package contracts

import (
	"context"
	"encoding/json"
)

type ActionType string

const (
	ActionFinish   ActionType = "FINISH"
	ActionTool     ActionType = "TOOL"
	ActionDelegate ActionType = "DELEGATE"
)

// ModelAction contains exactly one of Output, ToolCall, or Delegation according
// to Type. The coordinator validates this before dispatch.
type ModelAction struct {
	Type       ActionType       `json:"type"`
	Output     json.RawMessage  `json:"output,omitempty"`
	ToolCall   *ToolRequest     `json:"tool_call,omitempty"`
	Delegation *DelegateRequest `json:"delegation,omitempty"`
}

type ToolRequest struct {
	ToolID string          `json:"tool_id"`
	Input  json.RawMessage `json:"input"`
}

type DelegateRequest struct {
	AgentID string          `json:"agent_id"`
	Input   json.RawMessage `json:"input"`
}

type ModelInput struct {
	Definition AgentDefinition `json:"definition"`
	Invocation AgentInvocation `json:"invocation"`
	Context    RunContext      `json:"context"`
	History    []ActionResult  `json:"history"`
	// Delegates carries the allowlisted delegate definitions (same version)
	// so a provider can shape delegation input to the delegate's schema.
	// Tools carries the agent's allowed tool definitions for the same reason.
	Delegates []AgentDefinition `json:"delegates,omitempty"`
	Tools     []ToolDefinition  `json:"tools,omitempty"`
}

type ActionResult struct {
	Action ModelAction     `json:"action"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  *RunError       `json:"error,omitempty"`
}

type ModelAdapter interface {
	Next(ctx context.Context, input ModelInput) (ModelResult, error)
}

type ModelResult struct {
	Action ModelAction `json:"action"`
	Usage  Usage       `json:"usage"`
}

type Tool interface {
	Definition() ToolDefinition
	Invoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

// ToolGateway is the only path from the coordinator to a tool. It must check
// the invoking agent's allowlist and validate input before calling a handler.
type ToolGateway interface {
	Invoke(ctx context.Context, agent AgentDefinition, invocation AgentInvocation, request ToolRequest) (ToolInvocation, error)
	// Definitions exposes the registered tool registry so the coordinator can
	// share an agent's allowed tool schemas with the model adapter.
	Definitions() []ToolDefinition
}

type RunStore interface {
	Create(ctx context.Context, run Run) error
	Get(ctx context.Context, runID string) (Run, error)
	Update(ctx context.Context, run Run) error
}

type EventSink interface {
	Append(ctx context.Context, event RunEvent) (RunEvent, error)
	List(ctx context.Context, runID string, after uint64) ([]RunEvent, error)
}

// Coordinator owns run creation and all agent invocation/delegation rules.
type Coordinator interface {
	Start(ctx context.Context, request StartRunRequest) (Run, error)
}
