package extensions

import "github.com/khangdoan514/hacktx-2026/agents/internal/contracts"

// Authorize is the permission check for MCP, HTTP, and worker tools.
// The gateway calls it before any adapter dials. A model, tool result, or
// worker response cannot satisfy this check.
func Authorize(principal Principal, agent contracts.AgentDefinition, tool ToolRecord) error {
	if principal.TenantID == "" || principal.TenantID != tool.TenantID {
		return fail(CodeToolNotAllowed, "tool is not allowed for this tenant")
	}
	if tool.Lifecycle != LifecyclePublished {
		return fail(CodeToolNotAllowed, "tool is not allowed for this agent")
	}
	allowed := false
	for _, id := range agent.AllowedTools {
		if id == tool.ID {
			allowed = true
			break
		}
	}
	if !allowed {
		return fail(CodeToolNotAllowed, "tool is not allowed for this agent")
	}
	granted := map[string]bool{}
	for _, permission := range principal.Permissions {
		granted[permission] = true
	}
	for _, needed := range tool.Permissions {
		if !granted[needed] {
			return fail(CodeToolNotAllowed, "tool is not allowed for this agent")
		}
	}
	switch tool.SideEffect {
	case SideEffectReadOnly, SideEffectWrite:
		return nil
	default:
		return fail(CodeInvalidInput, "tool side effect is invalid")
	}
}
