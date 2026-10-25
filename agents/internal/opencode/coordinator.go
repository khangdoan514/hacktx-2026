package opencode

// Coordinator implements contracts.Coordinator on top of the OpenCode
// service. Start validates the request against NMC3 schemas, creates an
// OpenCode session, and returns immediately; a background goroutine waits
// for completion, maps session messages to ordered NMC3 events and an
// invocation tree, validates the structured result, and persists the run.
// OpenCode configuration owns live tool and subagent policy. This layer
// validates requests and structured results, records denials and session
// usage, and enforces submitted limits. The wait deadline records TIMEOUT.
// max_tokens, max_cost_usd, max_model_turns, max_tool_calls, and
// max_delegations record BUDGET_EXCEEDED, interrupt an in-flight session,
// and do not accept further tool calls or child invocations.
// max_concurrent_agents is not a stop condition in this layer.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

func id(prefix string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func now() time.Time { return time.Now().UTC() }

type Coordinator struct {
	client       *Client
	defs         map[string]contracts.AgentDefinition
	store        contracts.RunStore
	events       contracts.EventSink
	workdir      string
	defaultModel string
}

func New(definitions []contracts.AgentDefinition, client *Client, store contracts.RunStore, events contracts.EventSink, workdir, defaultModel string) (*Coordinator, error) {
	if client == nil || store == nil || events == nil {
		return nil, fmt.Errorf("opencode coordinator is missing a dependency")
	}
	if workdir == "" || defaultModel == "" {
		return nil, fmt.Errorf("opencode coordinator needs a workdir and default model")
	}
	c := &Coordinator{client: client, defs: map[string]contracts.AgentDefinition{}, store: store, events: events, workdir: workdir, defaultModel: defaultModel}
	for _, d := range definitions {
		if d.ID == "" || d.Version == "" || d.Name == "" {
			return nil, fmt.Errorf("invalid definition identity")
		}
		if err := Validate(d.InputSchema, nil); err != nil {
			_ = err
		}
		key := d.ID + "@" + d.Version
		if _, ok := c.defs[key]; ok {
			return nil, fmt.Errorf("duplicate definition")
		}
		c.defs[key] = d
	}
	return c, nil
}

func (c *Coordinator) Definitions() []contracts.AgentDefinition {
	out := make([]contracts.AgentDefinition, 0, len(c.defs))
	for _, d := range c.defs {
		out = append(out, d)
	}
	return out
}

// splitModel maps "provider/model" to OpenCode's Model.Ref parts. The syntax
// was already checked by ValidModelID; a missing provider prefix is rejected
// here because OpenCode cannot route it.
func splitModel(model string) (providerID, modelID string, err error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "model must be provider/model"}
	}
	return parts[0], parts[1], nil
}

func positive(l contracts.Limits) error {
	if l.MaxDelegations <= 0 || l.MaxConcurrentAgents <= 0 || l.MaxToolCalls <= 0 || l.MaxModelTurns <= 0 || l.TimeoutSeconds <= 0 || l.MaxTokens <= 0 || l.MaxCostUSD <= 0 {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "all limits must be positive"}
	}
	return nil
}

func (c *Coordinator) Start(ctx context.Context, req contracts.StartRunRequest) (contracts.Run, error) {
	definition, ok := c.defs[req.AgentID+"@"+req.AgentVersion]
	if !ok {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "agent version not found"}
	}
	if err := positive(req.Limits); err != nil {
		return contracts.Run{}, err
	}
	model := req.ModelID
	if model == "" {
		model = c.defaultModel
	}
	if !contracts.ValidModelID(model) {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid model ID"}
	}
	providerID, modelID, err := splitModel(model)
	if err != nil {
		return contracts.Run{}, err
	}
	if err := Validate(definition.InputSchema, req.Input); err != nil {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "input does not match agent schema: " + err.Error()}
	}
	session, err := c.client.CreateSession(ctx, c.workdir, definition.Name+" run", definition.ID, providerID, modelID)
	if err != nil {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "agent session could not start"}
	}
	if err := c.client.Prompt(ctx, session.ID, buildPrompt(definition, c.defs, req.Input)); err != nil {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "agent session could not start"}
	}
	t := now()
	run := contracts.Run{ID: id("run-"), State: contracts.StateRunning, AgentID: definition.ID, Version: definition.Version, ModelID: model, OpenCodeSessionID: session.ID, Input: req.Input, Invocations: []contracts.AgentInvocation{}, ToolInvocations: []contracts.ToolInvocation{}, CreatedAt: t, UpdatedAt: t}
	if err := c.store.Create(ctx, run); err != nil {
		return contracts.Run{}, err
	}
	go c.execute(run, definition, req.Limits)
	return run, nil
}

// budgetPollInterval is how often an in-flight session is checked against
// submitted limits. Tests shorten it. The wait deadline stays on ctx.
var budgetPollInterval = 200 * time.Millisecond

// buildPrompt translates the NMC3 definition into session instructions. The
// output schema travels with the prompt so the model returns a valid final
// object; the coordinator still validates it before accepting the run.
func buildPrompt(definition contracts.AgentDefinition, defs map[string]contracts.AgentDefinition, input json.RawMessage) string {
	var sb strings.Builder
	sb.WriteString("You are " + definition.Name + " (" + definition.Role + "). " + definition.Instructions + "\n")
	sb.WriteString("Input (JSON): " + string(input) + "\n")
	if len(definition.AllowedDelegates) > 0 && definition.Role != "specialist" {
		sb.WriteString("Call the subagent tool once for each of these agents, in parallel:\n")
		for _, target := range definition.AllowedDelegates {
			if child, ok := defs[target+"@"+definition.Version]; ok {
				sb.WriteString("- agent " + child.ID + " input " + string(child.InputSchema) + "\n")
			}
		}
		sb.WriteString("Do not read the catalog yourself. Specialists read catalog/vendors.json and must cite only its vendor ids.\n")
	}
	if listed(definition.AllowedTools, "catalog.search") {
		sb.WriteString("Search the fictional catalog with the catalog_search tool. Do not read catalog files.\n")
	}
	sb.WriteString("Rules: do not write, edit, or delete any files; read-only analysis. Cite source IDs for every claim. If a tool or delegation is denied, do not retry it through the shell, the network, or another agent.\n")
	sb.WriteString("Reply with ONLY a single JSON object matching this output schema, no other text: " + string(definition.OutputSchema))
	return sb.String()
}

// canonicalToolID maps OpenCode's MCP tool name back to the NMC3 contract
// id. OpenCode names a local server tool "<server>_<tool>".
func canonicalToolID(name string) string {
	if name == "catalog_search" {
		return "catalog.search"
	}
	return name
}

func listed(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func delegatedAgent(state map[string]any) string {
	if input, ok := state["input"].(map[string]any); ok {
		if name, ok := input["agent"].(string); ok && name != "" {
			return name
		}
	}
	return "subagent"
}

func callFailed(state map[string]any) bool {
	status, _ := state["status"].(string)
	if status != "" && status != "completed" {
		return true
	}
	failure, _ := state["error"].(string)
	return failure != ""
}

// delegationDenial refuses a child when the target is outside the allowlist
// or OpenCode rejected the call. A rejected allowlisted call is the depth
// gate (a specialist cannot launch another agent). No child is created.
func delegationDenial(definition contracts.AgentDefinition, agent string, state map[string]any) (contracts.ErrorCode, bool) {
	if !listed(definition.AllowedDelegates, agent) {
		return contracts.ErrAgentNotAllowed, true
	}
	if callFailed(state) {
		return contracts.ErrDelegationDepthExceeded, true
	}
	return "", false
}

func toolDenial(definition contracts.AgentDefinition, name string, state map[string]any) (contracts.ErrorCode, bool) {
	if !listed(definition.AllowedTools, name) {
		return contracts.ErrToolNotAllowed, true
	}
	if callFailed(state) {
		return contracts.ErrInternal, true
	}
	return "", false
}

func (c *Coordinator) emit(runID string, invocationID *string, typ string, data any) {
	var raw json.RawMessage
	if data != nil {
		raw, _ = json.Marshal(data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = c.events.Append(ctx, contracts.RunEvent{RunID: runID, InvocationID: invocationID, Type: typ, Timestamp: now(), Data: raw, Source: "opencode"})
}

func (c *Coordinator) persist(run contracts.Run) {
	run.UpdatedAt = now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.store.Update(ctx, run)
}

func fail(err error) *contracts.RunError {
	if re, ok := err.(*contracts.RunError); ok {
		return re
	}
	return &contracts.RunError{Code: contracts.ErrInternal, Message: "agent session failed"}
}

func (c *Coordinator) execute(run contracts.Run, definition contracts.AgentDefinition, limits contracts.Limits) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(limits.TimeoutSeconds)*time.Second)
	defer cancel()
	c.emit(run.ID, nil, "RUN_STARTED", map[string]any{"agent_id": definition.ID, "opencode_session_id": run.OpenCodeSessionID})
	root := contracts.AgentInvocation{ID: id("inv-"), RunID: run.ID, AgentID: definition.ID, AgentVersion: definition.Version, Depth: 0, State: contracts.StateRunning, Input: run.Input}
	start := now()
	root.StartedAt = &start
	run.Invocations = append(run.Invocations, root)
	c.persist(run)
	c.emit(run.ID, &root.ID, "AGENT_STARTED", map[string]any{"depth": 0})
	c.emit(run.ID, &root.ID, "MODEL_STARTED", nil)

	output, usage, invocations, tools, runErr := c.collect(ctx, run, definition, &root, limits)
	finish := now()
	if runErr != nil {
		log.Printf("NMC3 run %s failed: %v", run.ID, runErr)
		root.Error = fail(runErr)
		root.State = contracts.StateFailed
		c.emit(run.ID, &root.ID, "MODEL_FAILED", map[string]any{"code": root.Error.Code})
		c.emit(run.ID, &root.ID, "AGENT_FAILED", map[string]any{"code": root.Error.Code})
	} else {
		c.emit(run.ID, &root.ID, "MODEL_SUCCEEDED", map[string]any{"action": "FINISH", "usage": usage})
		root.Output = output
		root.State = contracts.StateSucceeded
		c.emit(run.ID, &root.ID, "AGENT_SUCCEEDED", nil)
	}
	root.FinishedAt = &finish
	run.Invocations[0] = root
	run.Invocations = append(run.Invocations, invocations...)
	run.ToolInvocations = append(run.ToolInvocations, tools...)
	run.Usage = usage
	if runErr != nil {
		run.Error = fail(runErr)
		run.State = contracts.StateFailed
	} else {
		run.State = contracts.StateSucceeded
		run.Output = output
	}
	c.persist(run)
	if run.State == contracts.StateSucceeded {
		c.emit(run.ID, nil, "RUN_SUCCEEDED", nil)
	} else {
		c.emit(run.ID, nil, "RUN_FAILED", map[string]any{"code": run.Error.Code})
	}
}

func budgetError(message string) *contracts.RunError {
	return &contracts.RunError{Code: contracts.ErrBudgetExceeded, Message: message}
}

func usageFrom(session Session) contracts.Usage {
	return contracts.Usage{InputTokens: session.Tokens.Input, OutputTokens: session.Tokens.Output, CostUSD: session.Cost}
}

// usageStop applies the submitted token and cost caps to cumulative session
// usage. MaxTokens is input plus output. Equal to the cap is still inside it.
func usageStop(limits contracts.Limits, usage contracts.Usage) *contracts.RunError {
	if int64(usage.InputTokens)+int64(usage.OutputTokens) > int64(limits.MaxTokens) {
		return budgetError("token limit exceeded")
	}
	if usage.CostUSD > limits.MaxCostUSD {
		return budgetError("cost limit exceeded")
	}
	return nil
}

// countStop reports the first turn, tool call, or delegation past its cap.
// A subagent part consumes a delegation, not a tool call. The walk matches
// the mapper: newest-first messages are counted oldest-first.
func countStop(limits contracts.Limits, messages []Message) *contracts.RunError {
	turns, toolCalls, delegations := 0, 0, 0
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Type != "assistant" {
			continue
		}
		turns++
		if turns > limits.MaxModelTurns {
			return budgetError("model turn limit exceeded")
		}
		for _, part := range message.Content {
			if part.Type != "tool" {
				continue
			}
			if part.Name == "subagent" {
				delegations++
				if delegations > limits.MaxDelegations {
					return budgetError("delegation limit exceeded")
				}
				continue
			}
			toolCalls++
			if toolCalls > limits.MaxToolCalls {
				return budgetError("tool-call limit exceeded")
			}
		}
	}
	return nil
}

func limitStop(limits contracts.Limits, usage contracts.Usage, messages []Message) *contracts.RunError {
	if stop := usageStop(limits, usage); stop != nil {
		return stop
	}
	return countStop(limits, messages)
}

// peekBudget reads the live session without recording events. A read failure
// leaves the wait in place; the next successful read or the final mapping
// still enforces the same limits.
func (c *Coordinator) peekBudget(ctx context.Context, sessionID string, limits contracts.Limits) *contracts.RunError {
	session, err := c.client.GetSession(ctx, sessionID)
	if err != nil {
		return nil
	}
	messages, err := c.client.Messages(ctx, sessionID)
	if err != nil {
		return nil
	}
	return limitStop(limits, usageFrom(session), messages)
}

// collect waits for the session, polling submitted limits while it runs.
// Crossing one interrupts the session. The transcript is then mapped only
// through the last in-budget action: a later tool call is not recorded and
// a later delegation does not create a child.
func (c *Coordinator) collect(ctx context.Context, run contracts.Run, definition contracts.AgentDefinition, root *contracts.AgentInvocation, limits contracts.Limits) (json.RawMessage, contracts.Usage, []contracts.AgentInvocation, []contracts.ToolInvocation, error) {
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- c.client.Wait(ctx, run.OpenCodeSessionID)
	}()
	var premature *contracts.RunError
	ticker := time.NewTicker(budgetPollInterval)
	defer ticker.Stop()
	var waitErr error
waitLoop:
	for {
		select {
		case waitErr = <-waitDone:
			break waitLoop
		case <-ticker.C:
			if premature != nil {
				continue
			}
			stop := c.peekBudget(ctx, run.OpenCodeSessionID, limits)
			if stop == nil {
				continue
			}
			premature = stop
			interruptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = c.client.Interrupt(interruptCtx, run.OpenCodeSessionID)
			cancel()
		}
	}
	if premature == nil && ctx.Err() != nil {
		return nil, contracts.Usage{}, nil, nil, &contracts.RunError{Code: contracts.ErrTimeout, Message: "run deadline exceeded"}
	}
	if waitErr != nil && premature == nil {
		return nil, contracts.Usage{}, nil, nil, waitErr
	}
	readCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		readCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	session, err := c.client.GetSession(readCtx, run.OpenCodeSessionID)
	if err != nil {
		if premature != nil {
			return nil, contracts.Usage{}, nil, nil, premature
		}
		return nil, contracts.Usage{}, nil, nil, err
	}
	messages, err := c.client.Messages(readCtx, run.OpenCodeSessionID)
	if err != nil {
		if premature != nil {
			return nil, usageFrom(session), nil, nil, premature
		}
		return nil, contracts.Usage{}, nil, nil, err
	}
	output, usage, invocations, tools, mapErr := c.mapSession(run, definition, root, limits, session, messages)
	// A limit observed while the session was still running wins over a later
	// non-success outcome from the interrupt. A budget error in the final
	// transcript names the cap that the recorded actions actually crossed.
	if re, ok := mapErr.(*contracts.RunError); ok && re.Code == contracts.ErrBudgetExceeded {
		return output, usage, invocations, tools, mapErr
	}
	if premature != nil {
		return output, usage, invocations, tools, premature
	}
	return output, usage, invocations, tools, mapErr
}

func (c *Coordinator) mapSession(run contracts.Run, definition contracts.AgentDefinition, root *contracts.AgentInvocation, limits contracts.Limits, session Session, messages []Message) (json.RawMessage, contracts.Usage, []contracts.AgentInvocation, []contracts.ToolInvocation, error) {
	usage := usageFrom(session)
	var invocations []contracts.AgentInvocation
	var tools []contracts.ToolInvocation
	var lastText string
	turns, toolCalls, delegations := 0, 0, 0
	// The message list is newest-first; walk chronologically so progress
	// events stay ordered and the final text is the newest one.
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Type != "assistant" {
			continue
		}
		turns++
		if turns > limits.MaxModelTurns {
			return nil, usage, invocations, tools, budgetError("model turn limit exceeded")
		}
		for _, part := range message.Content {
			switch part.Type {
			case "tool":
				if part.Name == "subagent" {
					delegations++
					if delegations > limits.MaxDelegations {
						return nil, usage, invocations, tools, budgetError("delegation limit exceeded")
					}
				} else {
					toolCalls++
					if toolCalls > limits.MaxToolCalls {
						return nil, usage, invocations, tools, budgetError("tool-call limit exceeded")
					}
				}
				toolID := canonicalToolID(part.Name)
				toolInvocation := contracts.ToolInvocation{ID: id("tool-"), RunID: run.ID, AgentInvocationID: root.ID, ToolID: toolID, ToolVersion: "opencode", StartedAt: now()}
				finished := now()
				toolInvocation.FinishedAt = &finished
				c.emit(run.ID, &root.ID, "TOOL_STARTED", map[string]any{"tool_id": toolID})
				if part.Name == "subagent" {
					agent := delegatedAgent(part.State)
					code, denied := delegationDenial(definition, agent, part.State)
					if denied {
						toolInvocation.Error = &contracts.RunError{Code: code, Message: "delegation denied"}
						c.emit(run.ID, &root.ID, "TOOL_FAILED", map[string]any{"tool_id": part.Name, "code": code, "agent_id": agent})
						tools = append(tools, toolInvocation)
						break
					}
					c.emit(run.ID, &root.ID, "TOOL_SUCCEEDED", map[string]any{"tool_id": part.Name})
					tools = append(tools, toolInvocation)
					child := contracts.AgentInvocation{ID: id("inv-"), RunID: run.ID, ParentID: &root.ID, AgentID: agent, AgentVersion: definition.Version, Depth: 1, State: contracts.StateSucceeded, Input: json.RawMessage(`{}`), StartedAt: &toolInvocation.StartedAt, FinishedAt: &finished}
					invocations = append(invocations, child)
					c.emit(run.ID, &child.ID, "AGENT_STARTED", map[string]any{"depth": 1})
					c.emit(run.ID, &child.ID, "AGENT_SUCCEEDED", nil)
					break
				}
				if code, denied := toolDenial(definition, toolID, part.State); denied {
					toolInvocation.Error = &contracts.RunError{Code: code, Message: "tool call denied"}
					c.emit(run.ID, &root.ID, "TOOL_FAILED", map[string]any{"tool_id": toolID, "code": code})
				} else {
					c.emit(run.ID, &root.ID, "TOOL_SUCCEEDED", map[string]any{"tool_id": toolID})
				}
				tools = append(tools, toolInvocation)
			case "text":
				if part.Text != "" {
					lastText = part.Text
				}
			}
		}
	}
	if stop := usageStop(limits, usage); stop != nil {
		return nil, usage, invocations, tools, stop
	}
	if session.Outcome != "" && session.Outcome != "succeeded" {
		message := "agent session ended as " + session.Outcome
		for _, item := range messages {
			if item.Error != nil && item.Error.Message != "" {
				message = item.Error.Message
			}
		}
		return nil, usage, invocations, tools, &contracts.RunError{Code: contracts.ErrInternal, Message: message}
	}
	extracted := extractFirstObject(lastText)
	if err := Validate(definition.OutputSchema, json.RawMessage(extracted)); err != nil {
		return nil, usage, invocations, tools, &contracts.RunError{Code: contracts.ErrOutputSchemaInvalid, Message: "agent output does not match schema: " + err.Error()}
	}
	return json.RawMessage(extracted), usage, invocations, tools, nil
}
