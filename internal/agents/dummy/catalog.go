package dummy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func ReadRepo(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(repoRoot(), rel))
}

type catalogTool struct {
	vendors []map[string]any
}

func CatalogTool() contracts.Tool {
	raw, err := ReadRepo("agents/fixtures/catalog/vendors.json")
	if err != nil {
		panic(err)
	}
	var vendors []map[string]any
	if err := json.Unmarshal(raw, &vendors); err != nil {
		panic(err)
	}
	return catalogTool{vendors: vendors}
}

func (c catalogTool) Definition() contracts.ToolDefinition {
	return contracts.ToolDefinition{
		ID: "catalog.search", Version: "1.0.0",
		Description:    "Read fictional support vendors.",
		InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":true}`),
		OutputSchema:   json.RawMessage(`{"type":"object","required":["vendors","source_ids"],"additionalProperties":false,"properties":{"vendors":{"type":"array"},"source_ids":{"type":"array","items":{"type":"string"}}}}`),
		SideEffect:     "READ_ONLY",
		TimeoutSeconds: 5,
	}
}

func (c catalogTool) Invoke(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	ids := []string{}
	for _, vendor := range c.vendors {
		if id, _ := vendor["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return json.Marshal(map[string]any{"vendors": c.vendors, "source_ids": ids})
}

type diffTool struct{}

func DiffTool() contracts.Tool { return diffTool{} }

func (diffTool) Definition() contracts.ToolDefinition {
	return contracts.ToolDefinition{
		ID: "diff.read", Version: "1.0.0",
		Description:    "Read the fictional pull request diff already attached to the run.",
		InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":true}`),
		OutputSchema:   json.RawMessage(`{"type":"object","required":["diff"],"properties":{"diff":{"type":"string"}}}`),
		SideEffect:     "READ_ONLY",
		TimeoutSeconds: 5,
	}
}

func (diffTool) Invoke(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var body map[string]any
	_ = json.Unmarshal(input, &body)
	diff, _ := body["diff"].(string)
	return json.Marshal(map[string]string{"diff": diff})
}

func LoadDefinitions() ([]contracts.AgentDefinition, error) {
	raw, err := ReadRepo("agents/examples/definitions.json")
	if err != nil {
		return nil, err
	}
	var definitions []contracts.AgentDefinition
	if err := json.Unmarshal(raw, &definitions); err != nil {
		return nil, err
	}
	definitions = append(definitions, analysisDefinition(), prOrchestrator(), codeQuality(), testSecurity())
	return definitions, nil
}

func analysisDefinition() contracts.AgentDefinition {
	return contracts.AgentDefinition{
		ID: "analysis-specialist", Version: "1.0.0", Name: "Analysis Specialist", Role: "specialist",
		Instructions:     "Summarize the fictional vendor set without inventing sources.",
		InputSchema:      json.RawMessage(`{"type":"object","additionalProperties":true}`),
		OutputSchema:     json.RawMessage(`{"type":"object","required":["summary"],"additionalProperties":false,"properties":{"summary":{"type":"string"}}}`),
		AllowedTools:     []string{},
		AllowedDelegates: []string{},
		Limits:           contracts.Limits{MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 1, MaxModelTurns: 4, MaxTokens: 12000, MaxCostUSD: 2, TimeoutSeconds: 60},
	}
}

func prOrchestrator() contracts.AgentDefinition {
	return contracts.AgentDefinition{
		ID: "pr-review-orchestrator", Version: "1.0.0", Name: "PR Review Assistant", Role: "orchestrator",
		Instructions:     "Review the fictional diff by asking the code quality reviewer and the test and security reviewer.",
		InputSchema:      json.RawMessage(`{"type":"object","required":["diff"],"additionalProperties":true,"properties":{"diff":{"type":"string"}}}`),
		OutputSchema:     json.RawMessage(`{"type":"object","required":["summary","finding_ids"],"additionalProperties":false,"properties":{"summary":{"type":"string"},"finding_ids":{"type":"array","items":{"type":"string"}}}}`),
		AllowedDelegates: []string{"code-quality-specialist", "test-security-specialist"},
		Limits:           contracts.Limits{MaxDelegations: 2, MaxConcurrentAgents: 2, MaxToolCalls: 4, MaxModelTurns: 8, MaxTokens: 12000, MaxCostUSD: 2, TimeoutSeconds: 60},
	}
}

func codeQuality() contracts.AgentDefinition {
	return contracts.AgentDefinition{
		ID: "code-quality-specialist", Version: "1.0.0", Name: "Code Quality Reviewer", Role: "specialist",
		Instructions: "Cite only lines present in the diff.",
		InputSchema:  json.RawMessage(`{"type":"object","required":["diff"],"additionalProperties":true,"properties":{"diff":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["findings"],"additionalProperties":false,"properties":{"findings":{"type":"array","items":{"type":"object","required":["line","message"],"properties":{"line":{"type":"integer"},"message":{"type":"string"}}}}}}`),
		AllowedTools: []string{"diff.read"},
		Limits:       contracts.Limits{MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 2, MaxModelTurns: 4, MaxTokens: 12000, MaxCostUSD: 2, TimeoutSeconds: 60},
	}
}

func testSecurity() contracts.AgentDefinition {
	return contracts.AgentDefinition{
		ID: "test-security-specialist", Version: "1.0.0", Name: "Test and Security Reviewer", Role: "specialist",
		Instructions: "Cite only lines present in the diff.",
		InputSchema:  json.RawMessage(`{"type":"object","required":["diff"],"additionalProperties":true,"properties":{"diff":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["findings"],"additionalProperties":false,"properties":{"findings":{"type":"array","items":{"type":"object","required":["line","message"],"properties":{"line":{"type":"integer"},"message":{"type":"string"}}}}}}`),
		AllowedTools: []string{"diff.read"},
		Limits:       contracts.Limits{MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 2, MaxModelTurns: 4, MaxTokens: 12000, MaxCostUSD: 2, TimeoutSeconds: 60},
	}
}
