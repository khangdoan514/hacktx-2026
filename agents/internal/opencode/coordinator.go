package opencode

// Coordinator implements contracts.Coordinator on top of the OpenCode
// service. Start validates the request against NMC3 schemas, creates an
// OpenCode session, and returns immediately; a background goroutine waits
// for completion, maps session messages to ordered NMC3 events and an
// invocation tree, validates the structured result, and persists the run.
// All execution rules the old hand-built runtime enforced (delegation,
// tools, budgets) are now owned by OpenCode configuration and permissions;
// this layer owns validation, provenance, and history.

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
	session, err := c.client.CreateSession(ctx, c.workdir, definition.Name+" run", definition.ID, providerID, modelID, PermissionsFor(definition))
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
	sb.WriteString("Do not use shell, glob, grep, edit, or web tools.\n")
	sb.WriteString("Reply with ONLY a single JSON object matching this output schema, no other text: " + string(definition.OutputSchema))
	return sb.String()
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

	output, usage, invocations, tools, runErr := c.collect(ctx, run, definition, &root)
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

// collect waits for the session, then maps messages to NMC3 events, one
// depth-1 invocation per completed subagent call, tool outcomes, cumulative
// usage, and the validated final output.
func (c *Coordinator) collect(ctx context.Context, run contracts.Run, definition contracts.AgentDefinition, root *contracts.AgentInvocation) (json.RawMessage, contracts.Usage, []contracts.AgentInvocation, []contracts.ToolInvocation, error) {
	if err := c.client.Wait(ctx, run.OpenCodeSessionID); err != nil {
		if ctx.Err() != nil {
			return nil, contracts.Usage{}, nil, nil, &contracts.RunError{Code: contracts.ErrTimeout, Message: "run deadline exceeded"}
		}
		return nil, contracts.Usage{}, nil, nil, err
	}
	session, err := c.client.GetSession(ctx, run.OpenCodeSessionID)
	if err != nil {
		return nil, contracts.Usage{}, nil, nil, err
	}
	messages, err := c.client.Messages(ctx, run.OpenCodeSessionID)
	if err != nil {
		return nil, contracts.Usage{}, nil, nil, err
	}
	usage := contracts.Usage{InputTokens: session.Tokens.Input, OutputTokens: session.Tokens.Output, CostUSD: session.Cost}
	var invocations []contracts.AgentInvocation
	children := map[string]string{}
	var tools []contracts.ToolInvocation
	var lastText string
	ensureChild := func(agent string, at time.Time) string {
		if agent == "" || agent == definition.ID || agent == "build" {
			return ""
		}
		if childID, ok := children[agent]; ok {
			return childID
		}
		finished := at
		child := contracts.AgentInvocation{ID: id("inv-"), RunID: run.ID, ParentID: &root.ID, AgentID: agent, AgentVersion: definition.Version, Depth: 1, State: contracts.StateSucceeded, Input: json.RawMessage(`{}`), StartedAt: &at, FinishedAt: &finished}
		invocations = append(invocations, child)
		children[agent] = child.ID
		c.emit(run.ID, &child.ID, "AGENT_STARTED", map[string]any{"depth": 1})
		c.emit(run.ID, &child.ID, "AGENT_SUCCEEDED", nil)
		return child.ID
	}
	// The message list is newest-first; walk chronologically so progress
	// events stay ordered and the final text is the newest one.
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Type != "assistant" {
			continue
		}
		for _, part := range message.Content {
			switch part.Type {
			case "tool":
				at := now()
				if part.Name == "subagent" {
					agent := "subagent"
					if input, ok := part.State["input"].(map[string]any); ok {
						if name, ok := input["agent"].(string); ok && name != "" {
							agent = name
						}
					}
					ensureChild(agent, at)
					continue
				}
				ownerID := root.ID
				ownerAgent := definition.ID
				if childID := ensureChild(message.Agent, at); childID != "" {
					ownerID = childID
					ownerAgent = message.Agent
				}
				toolID := catalogToolID(part.Name, ownerAgent, part.State, c.defs, definition.Version)
				toolInvocation := contracts.ToolInvocation{ID: id("tool-"), RunID: run.ID, AgentInvocationID: ownerID, ToolID: toolID, ToolVersion: "opencode", StartedAt: at}
				finished := at
				toolInvocation.FinishedAt = &finished
				c.emit(run.ID, &ownerID, "TOOL_STARTED", map[string]any{"tool_id": toolID})
				failure, _ := part.State["error"].(string)
				if failure != "" {
					toolInvocation.Error = &contracts.RunError{Code: contracts.ErrInternal, Message: "tool call failed"}
					c.emit(run.ID, &ownerID, "TOOL_FAILED", map[string]any{"tool_id": toolID})
				} else {
					c.emit(run.ID, &ownerID, "TOOL_SUCCEEDED", map[string]any{"tool_id": toolID})
				}
				tools = append(tools, toolInvocation)
			case "text":
				if part.Text != "" {
					lastText = part.Text
				}
			}
		}
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

// catalogToolID reports a catalog read as the agent's catalog tool. Other
// OpenCode tools keep their own names.
func catalogToolID(toolName, agentID string, state map[string]any, defs map[string]contracts.AgentDefinition, version string) string {
	if toolName == "read" && strings.Contains(resourcePath(state), "vendors.json") {
		if def, ok := defs[agentID+"@"+version]; ok {
			for _, tool := range def.AllowedTools {
				if tool == "catalog.search" || tool == "catalog.query" {
					return tool
				}
			}
		}
		return "catalog.search"
	}
	return toolName
}

func resourcePath(state map[string]any) string {
	if state == nil {
		return ""
	}
	if input, ok := state["input"].(map[string]any); ok {
		for _, key := range []string{"path", "filePath", "file", "target"} {
			if value, ok := input[key].(string); ok {
				return value
			}
		}
	}
	if title, ok := state["title"].(string); ok {
		return title
	}
	return ""
}
