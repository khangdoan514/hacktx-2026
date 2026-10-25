// Package contracts contains the shared v1 first pass types. Changes to this package
// require updating the API fixtures and coordinating all package owners.
package contracts

import (
	"encoding/json"
	"time"
)

type State string

const (
	StatePending   State = "PENDING"
	StateRunning   State = "RUNNING"
	StateSucceeded State = "SUCCEEDED"
	StateFailed    State = "FAILED"
	StateCancelled State = "CANCELLED"
	// StateWaiting holds a run while a protected write has no decision.
	StateWaiting State = "WAITING"
)

type ErrorCode string

const (
	ErrInvalidInput            ErrorCode = "INVALID_INPUT"
	ErrNotFound                ErrorCode = "NOT_FOUND"
	ErrAgentNotAllowed         ErrorCode = "AGENT_NOT_ALLOWED"
	ErrDelegationDepthExceeded ErrorCode = "DELEGATION_DEPTH_EXCEEDED"
	ErrToolNotAllowed          ErrorCode = "TOOL_NOT_ALLOWED"
	ErrBudgetExceeded          ErrorCode = "BUDGET_EXCEEDED"
	ErrTimeout                 ErrorCode = "TIMEOUT"
	ErrOutputSchemaInvalid     ErrorCode = "OUTPUT_SCHEMA_INVALID"
	ErrInternal                ErrorCode = "INTERNAL"
)

type RunError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *RunError) Error() string { return string(e.Code) + ": " + e.Message }

// JSONSchema is a JSON Schema document. The first pass uses a documented JSON Schema
// subset; see docs/contracts-v1.md. A nil schema means no validation rule.
type JSONSchema = json.RawMessage

type Limits struct {
	MaxDelegations      int     `json:"max_delegations"`
	MaxConcurrentAgents int     `json:"max_concurrent_agents"`
	MaxToolCalls        int     `json:"max_tool_calls"`
	MaxModelTurns       int     `json:"max_model_turns"`
	MaxTokens           int     `json:"max_tokens"`
	MaxCostUSD          float64 `json:"max_cost_usd"`
	TimeoutSeconds      int     `json:"timeout_seconds"`
}

// Usage is cumulative for a run and per-turn for a model result.
type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

type AgentDefinition struct {
	ID               string     `json:"id"`
	Version          string     `json:"version"`
	Name             string     `json:"name"`
	Role             string     `json:"role"`
	Instructions     string     `json:"instructions"`
	InputSchema      JSONSchema `json:"input_schema"`
	OutputSchema     JSONSchema `json:"output_schema"`
	AllowedTools     []string   `json:"allowed_tools"`
	AllowedDelegates []string   `json:"allowed_delegates"`
	Limits           Limits     `json:"limits"`
}

type ToolDefinition struct {
	ID             string     `json:"id"`
	Version        string     `json:"version"`
	Description    string     `json:"description"`
	InputSchema    JSONSchema `json:"input_schema"`
	OutputSchema   JSONSchema `json:"output_schema"`
	SideEffect     string     `json:"side_effect"` // v1: READ_ONLY
	TimeoutSeconds int        `json:"timeout_seconds"`
}

type RunContext struct {
	RunID string `json:"run_id"`
	// ModelID is the resolved per-run model (request override or default).
	ModelID   string          `json:"model_id,omitempty"`
	Request   json.RawMessage `json:"request"`
	Artifacts json.RawMessage `json:"artifacts,omitempty"`
	Limits    Limits          `json:"limits"`
}

type AgentInvocation struct {
	ID           string          `json:"id"`
	RunID        string          `json:"run_id"`
	ParentID     *string         `json:"parent_id"`
	AgentID      string          `json:"agent_id"`
	AgentVersion string          `json:"agent_version"`
	Depth        int             `json:"depth"`
	State        State           `json:"state"`
	Input        json.RawMessage `json:"input"`
	Output       json.RawMessage `json:"output,omitempty"`
	Error        *RunError       `json:"error,omitempty"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
}

type ToolInvocation struct {
	ID                string          `json:"id"`
	RunID             string          `json:"run_id"`
	AgentInvocationID string          `json:"agent_invocation_id"`
	ToolID            string          `json:"tool_id"`
	ToolVersion       string          `json:"tool_version"`
	Input             json.RawMessage `json:"input"`
	Output            json.RawMessage `json:"output,omitempty"`
	Error             *RunError       `json:"error,omitempty"`
	StartedAt         time.Time       `json:"started_at"`
	FinishedAt        *time.Time      `json:"finished_at,omitempty"`
}

type RunEvent struct {
	Sequence     uint64          `json:"sequence"`
	RunID        string          `json:"run_id"`
	InvocationID *string         `json:"invocation_id,omitempty"`
	Type         string          `json:"type"`
	Timestamp    time.Time       `json:"timestamp"`
	Data         json.RawMessage `json:"data,omitempty"`
	// Source marks event provenance: "nmc3" for coordinator-owned events,
	// "opencode" for events mapped from an OpenCode session.
	Source string `json:"source,omitempty"`
}

type Run struct {
	ID      string `json:"id"`
	State   State  `json:"state"`
	AgentID string `json:"agent_id"`
	Version string `json:"agent_version"`
	// ModelID records the resolved model for inspection: the request override,
	// or the service default (OPENCODE_MODEL) when the request omits one.
	ModelID string `json:"model_id,omitempty"`
	// OpenCodeSessionID links the run to the executing OpenCode session.
	// Empty for runs that predate the OpenCode backend.
	OpenCodeSessionID string            `json:"opencode_session_id,omitempty"`
	Input             json.RawMessage   `json:"input"`
	Output            json.RawMessage   `json:"output,omitempty"`
	Error             *RunError         `json:"error,omitempty"`
	Usage             Usage             `json:"usage"`
	Invocations       []AgentInvocation `json:"invocations"`
	ToolInvocations   []ToolInvocation  `json:"tool_invocations"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// ValidModelID accepts OpenRouter-style "provider/name" IDs (with an optional
// ":variant" suffix such as ":free"). It is a syntax check only; the provider
// decides whether the model exists.
func ValidModelID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '/' || c == '.' || c == '_' || c == ':' || c == '-' {
			continue
		}
		return false
	}
	return true
}
