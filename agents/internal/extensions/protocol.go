package extensions

import "encoding/json"

type wireCredential struct {
	Ref   string `json:"ref"`
	Value string `json:"value"`
}

type wireRequest struct {
	Protocol          int              `json:"protocol"`
	InvocationID      string           `json:"invocation_id"`
	RunID             string           `json:"run_id"`
	AgentInvocationID string           `json:"agent_invocation_id,omitempty"`
	PrincipalID       string           `json:"principal_id,omitempty"`
	TenantID          string           `json:"tenant_id"`
	ToolID            string           `json:"tool_id"`
	ToolVersion       string           `json:"tool_version"`
	Input             json.RawMessage  `json:"input"`
	Credential        *wireCredential  `json:"credential,omitempty"`
	Network           NetworkPolicy    `json:"network"`
	Filesystem        FilesystemPolicy `json:"filesystem"`
	MaxOutputBytes    int              `json:"max_output_bytes"`
}

type wireResponse struct {
	Protocol int             `json:"protocol"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    *Error          `json:"error,omitempty"`
}

type healthBody struct {
	Protocol int              `json:"protocol"`
	Tools    []DiscoveredTool `json:"tools"`
}
