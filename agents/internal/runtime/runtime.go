package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

type Coordinator struct {
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
	defs     map[string]contracts.AgentDefinition
	model    contracts.ModelAdapter
	tools    contracts.ToolGateway
	store    contracts.RunStore
	events   contracts.EventSink
}

func New(definitions []contracts.AgentDefinition, model contracts.ModelAdapter, tools contracts.ToolGateway, store contracts.RunStore, events contracts.EventSink) (*Coordinator, error) {
	c := &Coordinator{defs: map[string]contracts.AgentDefinition{}, cancels: map[string]context.CancelFunc{}, model: model, tools: tools, store: store, events: events}
	if model == nil || tools == nil || store == nil || events == nil {
		return nil, fmt.Errorf("missing runtime dependency")
	}
	for _, d := range definitions {
		if d.ID == "" || d.Version == "" || d.Name == "" {
			return nil, fmt.Errorf("invalid definition identity")
		}
		if err := positive(d.Limits); err != nil {
			return nil, err
		}
		if err := validateSchema(d.InputSchema); err != nil {
			return nil, err
		}
		if err := validateSchema(d.OutputSchema); err != nil {
			return nil, err
		}
		key := d.ID + "@" + d.Version
		if _, ok := c.defs[key]; ok {
			return nil, fmt.Errorf("duplicate definition")
		}
		c.defs[key] = d
	}
	for _, d := range definitions {
		for _, target := range d.AllowedDelegates {
			if _, ok := c.defs[target+"@"+d.Version]; !ok {
				return nil, fmt.Errorf("unknown delegate %s", target)
			}
		}
	}
	return c, nil
}

func positive(l contracts.Limits) error {
	if l.MaxDelegations <= 0 || l.MaxConcurrentAgents <= 0 || l.MaxToolCalls <= 0 || l.MaxModelTurns <= 0 || l.TimeoutSeconds <= 0 || l.MaxTokens <= 0 || l.MaxCostUSD <= 0 || math.IsNaN(l.MaxCostUSD) || math.IsInf(l.MaxCostUSD, 0) {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "all limits must be positive"}
	}
	return nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func effective(a, b contracts.Limits) contracts.Limits {
	return contracts.Limits{MaxDelegations: min(a.MaxDelegations, b.MaxDelegations), MaxConcurrentAgents: min(a.MaxConcurrentAgents, b.MaxConcurrentAgents), MaxToolCalls: min(a.MaxToolCalls, b.MaxToolCalls), MaxModelTurns: min(a.MaxModelTurns, b.MaxModelTurns), TimeoutSeconds: min(a.TimeoutSeconds, b.TimeoutSeconds), MaxTokens: min(a.MaxTokens, b.MaxTokens), MaxCostUSD: math.Min(a.MaxCostUSD, b.MaxCostUSD)}
}
func id(prefix string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
func now() time.Time { return time.Now().UTC() }
func (c *Coordinator) Definitions() []contracts.AgentDefinition {
	out := make([]contracts.AgentDefinition, 0, len(c.defs))
	for _, d := range c.defs {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b contracts.AgentDefinition) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out
}

func (c *Coordinator) Start(ctx context.Context, req contracts.StartRunRequest) (contracts.Run, error) {
	d, ok := c.defs[req.AgentID+"@"+req.AgentVersion]
	if !ok {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "agent version not found"}
	}
	if err := positive(req.Limits); err != nil {
		return contracts.Run{}, err
	}
	if err := Validate(d.InputSchema, req.Input); err != nil {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "input does not match agent schema"}
	}
	t := now()
	r := contracts.Run{ID: id("run-"), State: contracts.StatePending, AgentID: d.ID, Version: d.Version, Input: req.Input, Invocations: []contracts.AgentInvocation{}, ToolInvocations: []contracts.ToolInvocation{}, CreatedAt: t, UpdatedAt: t}
	if err := c.store.Create(ctx, r); err != nil {
		return contracts.Run{}, err
	}
	go c.execute(r, d, effective(req.Limits, d.Limits))
	return r, nil
}

// Cancel stops an active run. The persistent snapshot and terminal events remain inspectable.
func (c *Coordinator) Cancel(runID string) error {
	c.cancelMu.Lock()
	cancel, ok := c.cancels[runID]
	c.cancelMu.Unlock()
	if !ok {
		return &contracts.RunError{Code: contracts.ErrNotFound, Message: "active run not found"}
	}
	cancel()
	return nil
}

type execution struct {
	mu                        sync.Mutex
	wg                        sync.WaitGroup
	run                       contracts.Run
	turns, tools, delegations int
	limits                    contracts.Limits
	active                    chan struct{}
	coord                     *Coordinator
	ctx                       context.Context
}

func (e *execution) persist() {
	e.run.UpdatedAt = now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.coord.store.Update(ctx, e.run); err != nil {
		log.Printf("NMC3 run %s snapshot update failed: %v", e.run.ID, err)
	}
}
func (e *execution) emit(typ string, inv *string, data any) {
	var raw json.RawMessage
	if data != nil {
		raw, _ = json.Marshal(data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.coord.events.Append(ctx, contracts.RunEvent{RunID: e.run.ID, InvocationID: inv, Type: typ, Timestamp: now(), Data: raw}); err != nil {
		log.Printf("NMC3 run %s event append failed: %v", e.run.ID, err)
	}
}
func (e *execution) setInvocation(v contracts.AgentInvocation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.run.Invocations {
		if e.run.Invocations[i].ID == v.ID {
			e.run.Invocations[i] = v
			e.persist()
			return
		}
	}
	e.run.Invocations = append(e.run.Invocations, v)
	e.persist()
}
func (e *execution) fail(err error) *contracts.RunError {
	var re *contracts.RunError
	if errors.As(err, &re) {
		return re
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &contracts.RunError{Code: contracts.ErrTimeout, Message: "run deadline exceeded"}
	}
	if errors.Is(err, context.Canceled) {
		return &contracts.RunError{Code: contracts.ErrTimeout, Message: "run cancelled"}
	}
	return &contracts.RunError{Code: contracts.ErrInternal, Message: "model or runtime operation failed"}
}
func (c *Coordinator) execute(r contracts.Run, d contracts.AgentDefinition, l contracts.Limits) {
	base, cancelRun := context.WithCancel(context.Background())
	ctx, cancelDeadline := context.WithTimeout(base, time.Duration(l.TimeoutSeconds)*time.Second)
	c.cancelMu.Lock()
	c.cancels[r.ID] = cancelRun
	c.cancelMu.Unlock()
	defer func() { cancelDeadline(); cancelRun(); c.cancelMu.Lock(); delete(c.cancels, r.ID); c.cancelMu.Unlock() }()
	e := &execution{run: r, limits: l, active: make(chan struct{}, l.MaxConcurrentAgents), coord: c, ctx: ctx}
	e.mu.Lock()
	e.run.State = contracts.StateRunning
	e.persist()
	e.mu.Unlock()
	e.emit("RUN_STARTED", nil, map[string]any{"agent_id": d.ID})
	root := contracts.AgentInvocation{ID: id("inv-"), RunID: r.ID, AgentID: d.ID, AgentVersion: d.Version, Depth: 0, State: contracts.StatePending, Input: r.Input}
	out, err := e.invoke(root, d)
	wasCancelled := errors.Is(ctx.Err(), context.Canceled)
	if err != nil {
		cancelRun()
	}
	e.wg.Wait()
	e.mu.Lock()
	if err == nil {
		e.run.State = contracts.StateSucceeded
		e.run.Output = out
	} else {
		e.run.Error = e.fail(err)
		if wasCancelled {
			e.run.State = contracts.StateCancelled
		} else {
			e.run.State = contracts.StateFailed
		}
	}
	e.persist()
	state := e.run.State
	e.mu.Unlock()
	if state == contracts.StateSucceeded {
		e.emit("RUN_SUCCEEDED", nil, nil)
	} else if state == contracts.StateCancelled {
		e.emit("RUN_CANCELLED", nil, nil)
	} else {
		e.emit("RUN_FAILED", nil, map[string]any{"code": e.run.Error.Code})
	}
}

func actionError(code contracts.ErrorCode, msg string) error {
	return &contracts.RunError{Code: code, Message: msg}
}
func validateAction(a contracts.ModelAction) error {
	switch a.Type {
	case contracts.ActionFinish:
		if len(a.Output) == 0 || a.ToolCall != nil || a.Delegation != nil || !json.Valid(a.Output) {
			return actionError(contracts.ErrInvalidInput, "invalid finish action")
		}
	case contracts.ActionTool:
		if a.ToolCall == nil || len(a.Output) > 0 || a.Delegation != nil || a.ToolCall.ToolID == "" || !json.Valid(a.ToolCall.Input) {
			return actionError(contracts.ErrInvalidInput, "invalid tool action")
		}
	case contracts.ActionDelegate:
		if a.Delegation == nil || len(a.Output) > 0 || a.ToolCall != nil || a.Delegation.AgentID == "" || !json.Valid(a.Delegation.Input) {
			return actionError(contracts.ErrInvalidInput, "invalid delegate action")
		}
	default:
		return actionError(contracts.ErrInvalidInput, "unknown model action")
	}
	return nil
}
func (e *execution) charge(u contracts.Usage) error {
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CostUSD < 0 || math.IsNaN(u.CostUSD) || math.IsInf(u.CostUSD, 0) {
		return actionError(contracts.ErrInvalidInput, "invalid model usage")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.run.Usage.InputTokens += u.InputTokens
	e.run.Usage.OutputTokens += u.OutputTokens
	e.run.Usage.CostUSD += u.CostUSD
	e.persist()
	if e.run.Usage.InputTokens+e.run.Usage.OutputTokens > e.limits.MaxTokens || e.run.Usage.CostUSD > e.limits.MaxCostUSD {
		return actionError(contracts.ErrBudgetExceeded, "model usage budget exceeded")
	}
	return nil
}
func (e *execution) takeTurn() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.turns >= e.limits.MaxModelTurns {
		return actionError(contracts.ErrBudgetExceeded, "model turn budget exceeded")
	}
	e.turns++
	return nil
}
func (e *execution) takeTool() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tools >= e.limits.MaxToolCalls {
		return actionError(contracts.ErrBudgetExceeded, "tool call budget exceeded")
	}
	e.tools++
	return nil
}
func (e *execution) takeDelegate() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.delegations >= e.limits.MaxDelegations {
		return actionError(contracts.ErrBudgetExceeded, "delegation budget exceeded")
	}
	e.delegations++
	return nil
}

type pendingResult struct {
	index int
	ch    chan contracts.ActionResult
}

func (e *execution) invoke(inv contracts.AgentInvocation, d contracts.AgentDefinition) (json.RawMessage, error) {
	if inv.Depth > 0 {
		select {
		case e.active <- struct{}{}:
		case <-e.ctx.Done():
			return nil, e.ctx.Err()
		}
		defer func() { <-e.active }()
	}
	start := now()
	inv.State = contracts.StateRunning
	inv.StartedAt = &start
	e.setInvocation(inv)
	e.emit("AGENT_STARTED", &inv.ID, map[string]any{"depth": inv.Depth})
	var output json.RawMessage
	var failure error
	defer func() {
		finish := now()
		inv.FinishedAt = &finish
		if failure != nil {
			inv.Error = e.fail(failure)
			if errors.Is(e.ctx.Err(), context.Canceled) {
				inv.State = contracts.StateCancelled
				e.emit("AGENT_CANCELLED", &inv.ID, nil)
			} else {
				inv.State = contracts.StateFailed
				e.emit("AGENT_FAILED", &inv.ID, map[string]any{"code": inv.Error.Code})
			}
		} else {
			inv.Output = output
			inv.State = contracts.StateSucceeded
			e.emit("AGENT_SUCCEEDED", &inv.ID, nil)
		}
		e.setInvocation(inv)
	}()
	history := []contracts.ActionResult{}
	pending := []pendingResult{}
	drain := func() {
		for _, p := range pending {
			history[p.index] = <-p.ch
		}
		pending = nil
	}
	for {
		if err := e.ctx.Err(); err != nil {
			failure = err
			return nil, err
		}
		if err := e.takeTurn(); err != nil {
			failure = err
			return nil, err
		}
		e.emit("MODEL_STARTED", &inv.ID, nil)
		result, err := e.coord.model.Next(e.ctx, contracts.ModelInput{Definition: d, Invocation: inv, Context: contracts.RunContext{RunID: e.run.ID, Request: inv.Input, Limits: e.limits}, History: history})
		if err != nil {
			e.emit("MODEL_FAILED", &inv.ID, map[string]any{"code": e.fail(err).Code})
			failure = err
			return nil, err
		}
		if err := e.charge(result.Usage); err != nil {
			e.emit("MODEL_FAILED", &inv.ID, map[string]any{"code": e.fail(err).Code})
			failure = err
			return nil, err
		}
		a := result.Action
		if err := validateAction(a); err != nil {
			e.emit("MODEL_FAILED", &inv.ID, map[string]any{"code": e.fail(err).Code})
			failure = err
			return nil, err
		}
		e.emit("MODEL_SUCCEEDED", &inv.ID, map[string]any{"action": a.Type, "usage": result.Usage})
		switch a.Type {
		case contracts.ActionFinish:
			if len(pending) > 0 {
				drain()
				continue
			}
			if err := Validate(d.OutputSchema, a.Output); err != nil {
				failure = actionError(contracts.ErrOutputSchemaInvalid, "agent output does not match schema")
				return nil, failure
			}
			output = a.Output
			return output, nil
		case contracts.ActionTool:
			if err := e.takeTool(); err != nil {
				failure = err
				return nil, err
			}
			e.emit("TOOL_STARTED", &inv.ID, map[string]any{"tool_id": a.ToolCall.ToolID})
			ti, err := e.coord.tools.Invoke(e.ctx, d, inv, *a.ToolCall)
			e.mu.Lock()
			e.run.ToolInvocations = append(e.run.ToolInvocations, ti)
			e.persist()
			e.mu.Unlock()
			ar := contracts.ActionResult{Action: a, Output: ti.Output}
			if err != nil {
				ar.Error = e.fail(err)
				e.emit("TOOL_FAILED", &inv.ID, map[string]any{"tool_id": a.ToolCall.ToolID, "tool_invocation_id": ti.ID, "code": ar.Error.Code})
			} else {
				e.emit("TOOL_SUCCEEDED", &inv.ID, map[string]any{"tool_id": a.ToolCall.ToolID, "tool_invocation_id": ti.ID})
			}
			history = append(history, ar)
		case contracts.ActionDelegate:
			if inv.Depth >= 1 {
				e.emit("DELEGATION_REJECTED", &inv.ID, map[string]any{"code": contracts.ErrDelegationDepthExceeded})
				failure = actionError(contracts.ErrDelegationDepthExceeded, "specialists cannot delegate")
				return nil, failure
			}
			if !slices.Contains(d.AllowedDelegates, a.Delegation.AgentID) {
				e.emit("DELEGATION_REJECTED", &inv.ID, map[string]any{"code": contracts.ErrAgentNotAllowed})
				failure = actionError(contracts.ErrAgentNotAllowed, "delegate is not allowed")
				return nil, failure
			}
			childDef, ok := e.coord.defs[a.Delegation.AgentID+"@"+d.Version]
			if !ok {
				failure = actionError(contracts.ErrNotFound, "delegate definition not found")
				return nil, failure
			}
			if err := Validate(childDef.InputSchema, a.Delegation.Input); err != nil {
				failure = actionError(contracts.ErrInvalidInput, "delegate input does not match schema")
				return nil, failure
			}
			if err := e.takeDelegate(); err != nil {
				failure = err
				return nil, err
			}
			child := contracts.AgentInvocation{ID: id("inv-"), RunID: e.run.ID, ParentID: &inv.ID, AgentID: childDef.ID, AgentVersion: childDef.Version, Depth: 1, State: contracts.StatePending, Input: a.Delegation.Input}
			e.setInvocation(child)
			ch := make(chan contracts.ActionResult, 1)
			index := len(history)
			history = append(history, contracts.ActionResult{Action: a})
			pending = append(pending, pendingResult{index: index, ch: ch})
			e.wg.Add(1)
			go func() {
				defer e.wg.Done()
				out, err := e.invoke(child, childDef)
				ar := contracts.ActionResult{Action: a, Output: out}
				if err != nil {
					ar.Error = e.fail(err)
				}
				ch <- ar
			}()
		}
	}
}
