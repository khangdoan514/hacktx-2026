package opencode

// Context steps are the deterministic part of a definition: tool calls with
// fixed arguments that the coordinator makes itself before the model starts.
// Each result is added to the prompt, so the model begins with that data
// instead of deciding whether to fetch it. A delegate's steps run here too,
// and the orchestrator is told to pass their results on in the task.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

// ToolCaller runs one built-in pack tool as the employee who started the run.
// owner is empty for a caller without an employee identity.
type ToolCaller func(ctx context.Context, owner, toolID string, args json.RawMessage) (any, error)

// SetToolCaller enables context steps. Without one, a definition that has
// context steps is refused at Start.
func (c *Coordinator) SetToolCaller(call ToolCaller) { c.callTool = call }

// pendingPrompt is a prompt held back until context steps have run.
type pendingPrompt struct {
	defs   map[string]contracts.AgentDefinition
	agents []contracts.AgentDefinition
	owner  string
}

// promptWithContext runs the held-back context steps, then sends the prompt
// with their results.
func (c *Coordinator) promptWithContext(ctx context.Context, run contracts.Run, definition contracts.AgentDefinition, root *contracts.AgentInvocation, pending *pendingPrompt) ([]contracts.ToolInvocation, error) {
	results, calls, err := c.runContextSteps(ctx, run, root, pending.agents, pending.owner)
	if err != nil {
		return calls, err
	}
	if err := c.client.Prompt(ctx, run.OpenCodeSessionID, buildPrompt(definition, pending.defs, run.Input, results)); err != nil {
		log.Printf("NMC3 OpenCode Prompt failed: %v", err)
		return calls, &contracts.RunError{Code: contracts.ErrInternal, Message: "agent session could not start"}
	}
	return calls, nil
}

// maxContextResult caps one step's result in the prompt.
const maxContextResult = 16 << 10

// gathered is one finished context step, keyed to the agent that asked for it.
type gathered struct {
	agentID string
	step    contracts.ContextStep
	result  string
}

// contextAgents lists the definition and each delegate that has context steps,
// in prompt order.
func contextAgents(definition contracts.AgentDefinition, defs map[string]contracts.AgentDefinition) []contracts.AgentDefinition {
	var out []contracts.AgentDefinition
	if len(definition.ContextSteps) > 0 {
		out = append(out, definition)
	}
	if definition.Role == "specialist" {
		return out
	}
	for _, target := range definition.AllowedDelegates {
		if child, ok := defs[target+"@"+definition.Version]; ok && len(child.ContextSteps) > 0 {
			out = append(out, child)
		}
	}
	return out
}

// runContextSteps calls every step in order and records each as a tool
// invocation on the root agent. The first failure stops the run: the agent
// was built to start from this data, so it does not start without it.
func (c *Coordinator) runContextSteps(ctx context.Context, run contracts.Run, root *contracts.AgentInvocation, agents []contracts.AgentDefinition, owner string) ([]gathered, []contracts.ToolInvocation, error) {
	var results []gathered
	var calls []contracts.ToolInvocation
	for _, agent := range agents {
		for _, step := range agent.ContextSteps {
			args := fillArguments(step.Arguments, run.Input)
			call := contracts.ToolInvocation{ID: id("tool-"), RunID: run.ID, AgentInvocationID: root.ID, ToolID: step.Tool, ToolVersion: toolpacks.Version, Input: args, StartedAt: now()}
			c.emit(run.ID, &root.ID, "TOOL_STARTED", map[string]any{"tool_id": step.Tool, "context_step": step.ID, "agent_id": agent.ID})
			out, err := c.callTool(ctx, owner, step.Tool, args)
			finished := now()
			call.FinishedAt = &finished
			if err != nil {
				call.Error = &contracts.RunError{Code: contracts.ErrInternal, Message: err.Error()}
				calls = append(calls, call)
				c.emit(run.ID, &root.ID, "TOOL_FAILED", map[string]any{"tool_id": step.Tool, "context_step": step.ID, "code": call.Error.Code})
				return results, calls, &contracts.RunError{Code: contracts.ErrInternal, Message: fmt.Sprintf("context step %q (%s) failed: %v", step.Name, step.Tool, err)}
			}
			raw, _ := json.Marshal(out)
			call.Output = raw
			calls = append(calls, call)
			c.emit(run.ID, &root.ID, "TOOL_SUCCEEDED", map[string]any{"tool_id": step.Tool, "context_step": step.ID})
			text := string(raw)
			if len(text) > maxContextResult {
				text = text[:maxContextResult] + " …(truncated)"
			}
			results = append(results, gathered{agentID: agent.ID, step: step, result: text})
		}
	}
	return results, calls, nil
}

// contextSection renders gathered results for one agent's prompt line.
func contextSection(results []gathered, agentID string) string {
	var sb strings.Builder
	for _, r := range results {
		if r.agentID != agentID {
			continue
		}
		sb.WriteString("- " + r.step.Name + " (" + r.step.Tool + "): " + r.result + "\n")
	}
	return sb.String()
}

var inputRef = regexp.MustCompile(`\{\{\s*input\.([A-Za-z0-9_]+)\s*\}\}`)

// fillArguments replaces {{input.<field>}} in string arguments with that
// top-level field of the run input. A string field is inserted as is; any
// other value as JSON. An unknown field becomes empty.
func fillArguments(raw, input json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input, &fields)
	value := func(name string) string {
		v, ok := fields[name]
		if !ok {
			return ""
		}
		var s string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
		return string(v)
	}
	var args any
	if json.Unmarshal(raw, &args) != nil {
		return raw
	}
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case string:
			return inputRef.ReplaceAllStringFunc(t, func(m string) string { return value(inputRef.FindStringSubmatch(m)[1]) })
		case map[string]any:
			for k, item := range t {
				t[k] = walk(item)
			}
		case []any:
			for i, item := range t {
				t[i] = walk(item)
			}
		}
		return v
	}
	out, err := json.Marshal(walk(args))
	if err != nil {
		return raw
	}
	return out
}
