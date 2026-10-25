package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type Engine struct {
	Store          contracts.RunStore
	Events         contracts.EventSink
	Adapter        contracts.ModelAdapter
	Gateway        contracts.ToolGateway
	Definitions    map[string]contracts.AgentDefinition
	Mode           string
	AdapterVersion string
	Queue          interface {
		Enqueue(ctx context.Context, runID string) error
	}
	now    func() time.Time
	mu     sync.Mutex
	limits map[string]contracts.Limits
}

type childResult struct {
	invocation contracts.AgentInvocation
	result     contracts.ActionResult
	err        error
}

func (e *Engine) clock() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now().UTC()
}

func (e *Engine) Start(ctx context.Context, request contracts.StartRunRequest) (contracts.Run, error) {
	if e.Mode != "dummy" {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "provider execution is not configured"}
	}
	if request.AgentID == "" || request.AgentVersion == "" || !json.Valid(request.Input) || !PositiveLimits(request.Limits) {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid run request"}
	}
	if request.ModelID != "" && !contracts.ValidModelID(request.ModelID) {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid model ID"}
	}
	definition, ok := e.Definitions[request.AgentID]
	if !ok || definition.Version != request.AgentVersion {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "agent version not found"}
	}
	if err := ValidateSchema(definition.InputSchema, request.Input); err != nil {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "input does not match the agent schema"}
	}
	actor, _ := platform.ActorFrom(ctx)
	now := e.clock()
	e.mu.Lock()
	if e.limits == nil {
		e.limits = map[string]contracts.Limits{}
	}
	e.mu.Unlock()
	run := contracts.Run{
		ID:              newID("run"),
		State:           contracts.StatePending,
		AgentID:         definition.ID,
		Version:         definition.Version,
		ModelID:         request.ModelID,
		Input:           request.Input,
		Usage:           contracts.Usage{},
		Invocations:     []contracts.AgentInvocation{},
		ToolInvocations: []contracts.ToolInvocation{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	meta := Meta{OrganizationID: actor.OrgID, ActorID: actor.ID, ExecutionMode: e.Mode, AdapterVersion: e.AdapterVersion, Limits: request.Limits}
	e.mu.Lock()
	e.limits[run.ID] = request.Limits
	e.mu.Unlock()
	if err := e.Store.Create(WithMeta(ctx, meta), run); err != nil {
		return contracts.Run{}, err
	}
	if err := e.append(ctx, run.ID, nil, "RUN_STARTED", map[string]string{"execution_mode": e.Mode, "adapter_version": e.AdapterVersion}); err != nil {
		return contracts.Run{}, err
	}
	if e.Queue != nil {
		if err := e.Queue.Enqueue(ctx, run.ID); err != nil {
			return contracts.Run{}, err
		}
	}
	return run, nil
}

func (e *Engine) Execute(ctx context.Context, runID string) (contracts.Run, error) {
	run, err := e.Store.Get(ctx, runID)
	if err != nil {
		return contracts.Run{}, err
	}
	if run.State != contracts.StatePending && run.State != contracts.StateRunning {
		return run, nil
	}
	definition := e.Definitions[run.AgentID]
	e.mu.Lock()
	saved := e.limits[run.ID]
	e.mu.Unlock()
	if saved.MaxModelTurns == 0 {
		if meta, ok := MetaFrom(ctx); ok {
			saved = meta.Limits
		}
	}
	limits := tighter(saved, definition.Limits)
	timeout := time.Duration(limits.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	run.State = contracts.StateRunning
	run.UpdatedAt = e.clock()
	if err := e.Store.Update(ctx, run); err != nil {
		return contracts.Run{}, err
	}
	counters := &budget{limits: limits}
	invocation, execErr := e.invoke(ctx, &run, definition, nil, 0, run.Input, counters)
	run.Invocations = append([]contracts.AgentInvocation{invocation}, childrenOf(run.Invocations)...)
	run.UpdatedAt = e.clock()
	run.Usage = contracts.Usage{}
	if execErr != nil {
		code := codeOf(execErr)
		run.Error = &contracts.RunError{Code: code, Message: safeMessage(code)}
		if code == contracts.ErrTimeout || errors.Is(execErr, context.DeadlineExceeded) {
			run.State = contracts.StateFailed
			run.Error.Code = contracts.ErrTimeout
		} else if code == contracts.ErrInternal && errors.Is(ctx.Err(), context.Canceled) {
			run.State = contracts.StateCancelled
			run.Error.Code = contracts.ErrInternal
		} else if errors.Is(ctx.Err(), context.Canceled) && code != contracts.ErrTimeout {
			run.State = contracts.StateCancelled
			run.Error = &contracts.RunError{Code: contracts.ErrInternal, Message: "The run was cancelled"}
			_ = e.append(ctx, run.ID, &invocation.ID, "RUN_CANCELLED", map[string]string{"code": "CANCELLED"})
		} else {
			run.State = contracts.StateFailed
		}
		if run.State == contracts.StateCancelled {
			_ = e.append(context.WithoutCancel(ctx), run.ID, nil, "RUN_CANCELLED", map[string]string{"message": run.Error.Message})
		} else {
			_ = e.append(context.WithoutCancel(ctx), run.ID, nil, "RUN_FAILED", map[string]string{"code": string(run.Error.Code), "message": run.Error.Message})
		}
	} else {
		run.State = contracts.StateSucceeded
		run.Output = invocation.Output
		_ = e.append(ctx, run.ID, nil, "RUN_SUCCEEDED", map[string]any{"agent_id": run.AgentID})
	}
	run.UpdatedAt = e.clock()
	if err := e.Store.Update(context.WithoutCancel(ctx), run); err != nil {
		return run, err
	}
	return run, execErr
}

func childrenOf(invocations []contracts.AgentInvocation) []contracts.AgentInvocation {
	if invocations == nil {
		return []contracts.AgentInvocation{}
	}
	return invocations
}

// MaxDelegationDepth is the deepest an invocation can sit and still delegate:
// depth 0 (root) and depth 1 may hand work on; an agent at depth 2 cannot.
const MaxDelegationDepth = 2

func (e *Engine) invoke(ctx context.Context, run *contracts.Run, definition contracts.AgentDefinition, parent *string, depth int, input json.RawMessage, counters *budget) (contracts.AgentInvocation, error) {
	now := e.clock()
	invocation := contracts.AgentInvocation{
		ID: newID("inv"), RunID: run.ID, ParentID: parent, AgentID: definition.ID, AgentVersion: definition.Version,
		Depth: depth, State: contracts.StateRunning, Input: input, StartedAt: &now,
	}
	if err := e.append(ctx, run.ID, &invocation.ID, "AGENT_STARTED", map[string]any{"agent_id": definition.ID, "depth": depth}); err != nil {
		return invocation, err
	}
	history := []contracts.ActionResult{}
	var inflight []<-chan childResult
	finish := func(state contracts.State, output json.RawMessage, runErr *contracts.RunError) (contracts.AgentInvocation, error) {
		e.waitAll(inflight)
		finished := e.clock()
		invocation.State = state
		invocation.Output = output
		invocation.Error = runErr
		invocation.FinishedAt = &finished
		event := "AGENT_SUCCEEDED"
		if state == contracts.StateFailed {
			event = "AGENT_FAILED"
		}
		if state == contracts.StateCancelled {
			event = "AGENT_CANCELLED"
		}
		_ = e.append(context.WithoutCancel(ctx), run.ID, &invocation.ID, event, map[string]string{"agent_id": definition.ID})
		if runErr != nil {
			return invocation, runErr
		}
		return invocation, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			code := contracts.ErrTimeout
			message := "The run timed out"
			state := contracts.StateFailed
			if errors.Is(err, context.Canceled) {
				code = contracts.ErrInternal
				message = "The run was cancelled"
				state = contracts.StateCancelled
			}
			return finish(state, nil, &contracts.RunError{Code: code, Message: message})
		}
		if meta, ok := MetaFrom(ctx); ok && meta.CancelRequested {
			return finish(contracts.StateCancelled, nil, &contracts.RunError{Code: contracts.ErrInternal, Message: "The run was cancelled"})
		}
		if !counters.turn() {
			return finish(contracts.StateFailed, nil, &contracts.RunError{Code: contracts.ErrBudgetExceeded, Message: "The run exceeded its model-turn budget"})
		}
		tools := []contracts.ToolDefinition{}
		if e.Gateway != nil {
			tools = filterTools(e.Gateway.Definitions(), definition.AllowedTools)
		}
		result, err := e.Adapter.Next(ctx, contracts.ModelInput{
			Definition: definition,
			Invocation: invocation,
			Context:    contracts.RunContext{RunID: run.ID, ModelID: run.ModelID, Request: run.Input, Limits: counters.limits},
			History:    history,
			Delegates:  definitionsFor(definition.AllowedDelegates, e.Definitions),
			Tools:      tools,
		})
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			return finish(contracts.StateFailed, nil, &contracts.RunError{Code: contracts.ErrInternal, Message: "The model adapter failed"})
		}
		result.Usage = contracts.Usage{}
		if err := ValidateAction(result.Action); err != nil {
			return finish(contracts.StateFailed, nil, asRunError(err))
		}
		switch result.Action.Type {
		case contracts.ActionDelegate:
			if depth >= MaxDelegationDepth {
				history = append(history, contracts.ActionResult{Action: result.Action, Error: &contracts.RunError{Code: contracts.ErrDelegationDepthExceeded, Message: "Agents at the maximum depth cannot delegate"}})
				_ = e.append(ctx, run.ID, &invocation.ID, "DELEGATION_REJECTED", map[string]string{"code": string(contracts.ErrDelegationDepthExceeded)})
				invocation.Error = history[len(history)-1].Error
				return finish(contracts.StateFailed, nil, invocation.Error)
			}
			target, ok := e.Definitions[result.Action.Delegation.AgentID]
			allowed := false
			for _, id := range definition.AllowedDelegates {
				if id == target.ID {
					allowed = true
				}
			}
			if !ok || !allowed {
				history = append(history, contracts.ActionResult{Action: result.Action, Error: &contracts.RunError{Code: contracts.ErrAgentNotAllowed, Message: "That specialist is not allowlisted"}})
				_ = e.append(ctx, run.ID, &invocation.ID, "DELEGATION_REJECTED", map[string]string{"code": string(contracts.ErrAgentNotAllowed)})
				return finish(contracts.StateFailed, nil, history[len(history)-1].Error)
			}
			if !counters.delegation(len(inflight)) {
				return finish(contracts.StateFailed, nil, &contracts.RunError{Code: contracts.ErrBudgetExceeded, Message: "The run exceeded its delegation budget"})
			}
			childInput := result.Action.Delegation.Input
			if err := ValidateSchema(target.InputSchema, childInput); err != nil {
				return finish(contracts.StateFailed, nil, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Delegation input does not match the specialist schema"})
			}
			done := make(chan childResult, 1)
			inflight = append(inflight, done)
			go func(target contracts.AgentDefinition, childInput json.RawMessage, done chan childResult) {
				child, err := e.invoke(ctx, run, target, &invocation.ID, depth+1, childInput, counters)
				e.recordInvocation(run, child)
				output := child.Output
				actionResult := contracts.ActionResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: output}, Output: output, Error: child.Error}
				done <- childResult{invocation: child, result: actionResult, err: err}
			}(target, childInput, done)
			history = append(history, contracts.ActionResult{Action: result.Action})
		case contracts.ActionTool:
			collected, fatal := e.collect(ctx, inflight)
			inflight = nil
			history = append(history, collected...)
			if fatal != nil {
				return finish(contracts.StateFailed, nil, asRunError(fatal))
			}
			if !counters.tool() {
				return finish(contracts.StateFailed, nil, &contracts.RunError{Code: contracts.ErrBudgetExceeded, Message: "The run exceeded its tool budget"})
			}
			_ = e.append(ctx, run.ID, &invocation.ID, "TOOL_STARTED", map[string]string{"tool_id": result.Action.ToolCall.ToolID})
			call, callErr := e.Gateway.Invoke(ctx, definition, invocation, *result.Action.ToolCall)
			e.recordTool(run, call)
			if callErr != nil {
				_ = e.append(ctx, run.ID, &invocation.ID, "TOOL_FAILED", map[string]string{"code": string(codeOf(callErr))})
				return finish(contracts.StateFailed, nil, asRunError(callErr))
			}
			_ = e.append(ctx, run.ID, &invocation.ID, "TOOL_SUCCEEDED", map[string]string{"tool_id": call.ToolID})
			history = append(history, contracts.ActionResult{Action: result.Action, Output: call.Output})
		case contracts.ActionFinish:
			collected, fatal := e.collect(ctx, inflight)
			inflight = nil
			history = append(history, collected...)
			if fatal != nil {
				return finish(contracts.StateFailed, nil, asRunError(fatal))
			}
			if err := ValidateSchema(definition.OutputSchema, result.Action.Output); err != nil {
				return finish(contracts.StateFailed, nil, &contracts.RunError{Code: contracts.ErrOutputSchemaInvalid, Message: "Output does not match the agent schema"})
			}
			if err := CitationsAllowed(input, result.Action.Output); err != nil {
				return finish(contracts.StateFailed, nil, &contracts.RunError{Code: contracts.ErrOutputSchemaInvalid, Message: "A finding cites a line that is not in the diff"})
			}
			return finish(contracts.StateSucceeded, result.Action.Output, nil)
		}
	}
}

func filterTools(all []contracts.ToolDefinition, allowed []string) []contracts.ToolDefinition {
	out := []contracts.ToolDefinition{}
	for _, tool := range all {
		for _, id := range allowed {
			if tool.ID == id {
				out = append(out, tool)
			}
		}
	}
	return out
}

func (e *Engine) waitAll(channels []<-chan childResult) {
	for _, channel := range channels {
		<-channel
	}
}

func (e *Engine) collect(ctx context.Context, channels []<-chan childResult) ([]contracts.ActionResult, error) {
	results := []contracts.ActionResult{}
	var fatal error
	for _, channel := range channels {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		case child := <-channel:
			results = append(results, child.result)
			if child.err != nil {
				code := codeOf(child.err)
				if code != contracts.ErrDelegationDepthExceeded && code != contracts.ErrAgentNotAllowed {
					fatal = child.err
				}
			}
		}
	}
	return results, fatal
}

func (e *Engine) recordInvocation(run *contracts.Run, invocation contracts.AgentInvocation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	run.Invocations = append(run.Invocations, invocation)
}

func (e *Engine) recordTool(run *contracts.Run, call contracts.ToolInvocation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	run.ToolInvocations = append(run.ToolInvocations, call)
}

func (e *Engine) append(ctx context.Context, runID string, invocationID *string, eventType string, data any) error {
	encoded, _ := json.Marshal(data)
	_, err := e.Events.Append(ctx, contracts.RunEvent{
		RunID: runID, InvocationID: invocationID, Type: eventType, Timestamp: e.clock(), Data: encoded,
	})
	return err
}

type budget struct {
	mu          sync.Mutex
	limits      contracts.Limits
	turns       int
	delegations int
	tools       int
	inflight    int
}

func (b *budget) turn() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.turns >= b.limits.MaxModelTurns {
		return false
	}
	b.turns++
	return true
}

func (b *budget) delegation(current int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.delegations >= b.limits.MaxDelegations || current >= b.limits.MaxConcurrentAgents {
		return false
	}
	b.delegations++
	return true
}

func (b *budget) tool() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tools >= b.limits.MaxToolCalls {
		return false
	}
	b.tools++
	return true
}

func codeOf(err error) contracts.ErrorCode {
	var runErr *contracts.RunError
	if errors.As(err, &runErr) {
		return runErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return contracts.ErrTimeout
	}
	if errors.Is(err, context.Canceled) {
		return contracts.ErrInternal
	}
	return contracts.ErrInternal
}

func asRunError(err error) *contracts.RunError {
	var runErr *contracts.RunError
	if errors.As(err, &runErr) {
		return runErr
	}
	return &contracts.RunError{Code: codeOf(err), Message: safeMessage(codeOf(err))}
}

func safeMessage(code contracts.ErrorCode) string {
	switch code {
	case contracts.ErrTimeout:
		return "The run timed out"
	case contracts.ErrBudgetExceeded:
		return "The run exceeded its budget"
	case contracts.ErrToolNotAllowed:
		return "That tool is not allowed for this agent"
	case contracts.ErrOutputSchemaInvalid:
		return "Output does not match the schema"
	case contracts.ErrDelegationDepthExceeded:
		return "Specialists cannot delegate"
	case contracts.ErrAgentNotAllowed:
		return "That specialist is not allowlisted"
	default:
		return "The run failed"
	}
}
