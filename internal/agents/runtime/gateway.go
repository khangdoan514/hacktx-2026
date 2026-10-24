package runtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
)

type Gateway struct {
	tools map[string]contracts.Tool
}

func NewGateway(tools ...contracts.Tool) *Gateway {
	registry := map[string]contracts.Tool{}
	for _, tool := range tools {
		registry[tool.Definition().ID] = tool
	}
	return &Gateway{tools: registry}
}

func (g *Gateway) Definitions() []contracts.ToolDefinition {
	out := make([]contracts.ToolDefinition, 0, len(g.tools))
	for _, tool := range g.tools {
		out = append(out, tool.Definition())
	}
	return out
}

func (g *Gateway) Invoke(ctx context.Context, agent contracts.AgentDefinition, invocation contracts.AgentInvocation, request contracts.ToolRequest) (contracts.ToolInvocation, error) {
	now := time.Now().UTC()
	call := contracts.ToolInvocation{
		ID:                newID("tool"),
		RunID:             invocation.RunID,
		AgentInvocationID: invocation.ID,
		ToolID:            request.ToolID,
		Input:             request.Input,
		StartedAt:         now,
	}
	allowed := false
	for _, id := range agent.AllowedTools {
		if id == request.ToolID {
			allowed = true
		}
	}
	tool, known := g.tools[request.ToolID]
	if !allowed || !known || tool.Definition().SideEffect != "READ_ONLY" {
		call.Error = &contracts.RunError{Code: contracts.ErrToolNotAllowed, Message: "That tool is not allowed for this agent"}
		finished := time.Now().UTC()
		call.FinishedAt = &finished
		return call, call.Error
	}
	call.ToolVersion = tool.Definition().Version
	if err := ValidateSchema(tool.Definition().InputSchema, request.Input); err != nil {
		call.Error = &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Tool input does not match the schema"}
		finished := time.Now().UTC()
		call.FinishedAt = &finished
		return call, call.Error
	}
	output, err := tool.Invoke(ctx, request.Input)
	finished := time.Now().UTC()
	call.FinishedAt = &finished
	if err != nil {
		call.Error = &contracts.RunError{Code: contracts.ErrInternal, Message: "Tool failed"}
		return call, call.Error
	}
	if err := ValidateSchema(tool.Definition().OutputSchema, output); err != nil {
		call.Error = &contracts.RunError{Code: contracts.ErrOutputSchemaInvalid, Message: "Tool output does not match the schema"}
		return call, call.Error
	}
	call.Output = output
	return call, nil
}

func definitionsFor(ids []string, catalog map[string]contracts.AgentDefinition) []contracts.AgentDefinition {
	out := []contracts.AgentDefinition{}
	for _, id := range ids {
		if definition, ok := catalog[id]; ok {
			out = append(out, definition)
		}
	}
	return out
}

func mustJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}
