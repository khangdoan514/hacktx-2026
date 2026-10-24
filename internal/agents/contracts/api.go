package contracts

import "encoding/json"

type StartRunRequest struct {
	AgentID      string `json:"agent_id"`
	AgentVersion string `json:"agent_version"`
	// ModelID is an optional per-run OpenRouter model override. When empty,
	// the service default (OPENROUTER_MODEL) applies. When set it must pass
	// ValidModelID; unknown providers fail at the provider, not the gateway.
	ModelID string          `json:"model_id,omitempty"`
	Input   json.RawMessage `json:"input"`
	Limits  Limits          `json:"limits"`
}

type StartRunResponse struct {
	RunID string `json:"run_id"`
	State State  `json:"state"`
}

type RunResponse struct {
	Run Run `json:"run"`
}

type EventsResponse struct {
	RunID  string     `json:"run_id"`
	Events []RunEvent `json:"events"`
}

type AgentsResponse struct {
	Agents []AgentSummary `json:"agents"`
}

type AgentSummary struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Name    string `json:"name"`
}

type ErrorResponse struct {
	Error RunError `json:"error"`
}
