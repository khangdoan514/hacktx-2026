package opencode

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

const searchJSON = `{"organic_results":[{"title":"Go context","link":"https://go.dev/blog/context","snippet":"Cancel requests with context."},{"title":"unsafe","link":"javascript:alert(1)"}],"search_metadata":{"provider_secret":"never-store-this"}}`

func searchState(payload any) map[string]any {
	data, _ := json.Marshal(payload)
	return map[string]any{
		"status":  "completed",
		"input":   map[string]any{"params": map[string]any{"engine": "google", "q": "Go context", "api_key": "never-store-this"}},
		"content": []any{map[string]any{"type": "text", "text": string(data)}},
	}
}

func TestRecordSearchMCPEnvelopes(t *testing.T) {
	var direct any
	_ = json.Unmarshal([]byte(searchJSON), &direct)
	for name, payload := range map[string]any{
		"direct JSON":            direct,
		"SerpApi result string":  map[string]any{"result": searchJSON},
		"MCP structured content": map[string]any{"structuredContent": map[string]any{"result": searchJSON}},
	} {
		t.Run(name, func(t *testing.T) {
			input, output, err := recordSearch(searchState(payload))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(input)+string(output), "never-store-this") {
				t.Fatal("provider metadata or credential argument was stored")
			}
			var result searchResult
			if json.Unmarshal(output, &result) != nil || len(result.Sources) != 1 || result.Sources[0].URL != "https://go.dev/blog/context" {
				t.Fatalf("output=%s", output)
			}
		})
	}
}

func TestRecordSearchRejectsErrors(t *testing.T) {
	for name, state := range map[string]map[string]any{
		"provider failure": searchState(map[string]any{"error": "Invalid key never-store-this"}),
		"MCP failure":      searchState(map[string]any{"isError": true, "structuredContent": map[string]any{"result": searchJSON}}),
		"missing key text": searchState("Error: Missing API key"),
		"malformed":        searchState("not JSON"),
		"OpenCode failure": {"status": "error", "error": map[string]any{"message": "never-store-this"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, output, err := recordSearch(state)
			if err == nil || len(output) != 0 || strings.Contains(err.Message, "never-store-this") {
				t.Fatalf("output=%s error=%v", output, err)
			}
		})
	}
}

func researchFixture(t *testing.T) ([]contracts.AgentDefinition, contracts.StartRunRequest) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "examples")
	var defs []contracts.AgentDefinition
	var req contracts.StartRunRequest
	for name, target := range map[string]any{"definitions.json": &defs, "web-research.json": &req} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatal(err)
		}
	}
	return defs, req
}

func TestResearchRunRecordsEvidenceAndFailures(t *testing.T) {
	validOutput := `{"recommendation":"Use context cancellation.","source_ids":["https://go.dev/blog/context"],"sources":[{"title":"invented title","url":"https://go.dev/blog/context","snippet":"invented snippet"}],"limitations":[]}`
	for _, tc := range []struct {
		name       string
		state      map[string]any
		output     string
		wantCode   contracts.ErrorCode
		toolFailed bool
	}{
		{name: "success", state: searchState(map[string]any{"result": searchJSON}), output: validOutput},
		{name: "no search", output: validOutput, wantCode: contracts.ErrInternal},
		{name: "quota failure", state: map[string]any{"status": "error", "error": map[string]any{"message": "quota exceeded"}}, output: validOutput, wantCode: contracts.ErrInternal, toolFailed: true},
		{name: "provider error in completed tool", state: searchState(map[string]any{"error": "Invalid API key"}), output: validOutput, wantCode: contracts.ErrInternal, toolFailed: true},
		{name: "invented URL", state: searchState(map[string]any{"result": searchJSON}), output: `{"recommendation":"Fake answer","source_ids":["https://fake.example"],"limitations":[]}`, wantCode: contracts.ErrOutputSchemaInvalid},
		{name: "uncited answer", state: searchState(map[string]any{"result": searchJSON}), output: `{"recommendation":"Uncited answer","source_ids":[],"limitations":[]}`, wantCode: contracts.ErrOutputSchemaInvalid},
		{name: "no results", state: searchState(map[string]any{"organic_results": []any{}}), output: `{"recommendation":"Pretend answer","source_ids":[],"limitations":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeBackend{outcome: "succeeded"}
			parts := []map[string]any{}
			if tc.state != nil {
				parts = append(parts, map[string]any{"type": "tool", "name": "serpapi_search", "state": tc.state})
			}
			parts = append(parts, map[string]any{"type": "text", "text": tc.output})
			backend.messages = []Message{assistantMessage(parts...)}
			server := httptest.NewServer(backend.handler())
			defer server.Close()
			client, _ := NewClient(server.URL, "secret", server.Client())
			mem := newMemory()
			defs, req := researchFixture(t)
			c, err := New(defs, client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
			if err != nil {
				t.Fatal(err)
			}
			started, err := c.Start(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			run := waitRun(t, mem, started.ID)
			if tc.wantCode != "" {
				if run.State != contracts.StateFailed || run.Error == nil || run.Error.Code != tc.wantCode {
					t.Fatalf("state=%s error=%v", run.State, run.Error)
				}
			} else if run.State != contracts.StateSucceeded {
				t.Fatalf("error=%v", run.Error)
			}
			if tc.state != nil {
				if len(run.ToolInvocations) != 1 || run.ToolInvocations[0].ToolID != "serpapi.search" || (run.ToolInvocations[0].Error != nil) != tc.toolFailed {
					t.Fatalf("tools=%+v", run.ToolInvocations)
				}
				events, _ := mem.List(context.Background(), run.ID, 0)
				if tc.toolFailed && !eventCode(t, events, "TOOL_FAILED", string(contracts.ErrInternal)) {
					t.Fatal("missing tool failure event")
				}
			}
			if tc.name == "success" {
				if !strings.Contains(string(run.Output), "Go context") || strings.Contains(string(run.Output), "invented") {
					t.Fatalf("sources not resolved from evidence: %s", run.Output)
				}
				if string(run.ToolInvocations[0].Input) != `{"params":{"engine":"google","q":"Go context"}}` {
					t.Fatalf("input=%s", run.ToolInvocations[0].Input)
				}
			}
			if tc.name == "no results" && strings.Contains(string(run.Output), "Pretend answer") {
				t.Fatalf("no-results answer was fabricated: %s", run.Output)
			}
		})
	}
}

func TestResearchToolPolicy(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	cfg, err := loadProjectPolicy(filepath.Join(filepath.Dir(file), "..", "..", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var server struct {
		Type     string            `json:"type"`
		URL      string            `json:"url"`
		OAuth    bool              `json:"oauth"`
		CodeMode bool              `json:"codemode"`
		Headers  map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(cfg.MCP.Servers["serpapi"], &server); err != nil {
		t.Fatal(err)
	}
	if server.Type != "remote" || server.URL != "https://mcp.serpapi.com/mcp" || server.OAuth || server.CodeMode || server.Headers["Authorization"] != "Bearer {env:SERPAPI_API_KEY}" {
		t.Fatalf("unexpected SerpApi configuration: %+v", server)
	}
	if cfg.Agents["web-researcher"].Mode != "primary" {
		t.Fatal("research agent must be directly runnable")
	}
	for id := range cfg.Agents {
		want := "deny"
		if id == "web-researcher" {
			want = "allow"
		}
		if got := permissionEffect(effectiveRules(cfg, id), "serpapi_search", "*"); got != want {
			t.Fatalf("%s search=%s, want %s", id, got, want)
		}
	}
	for _, action := range []string{"serpapi_search_table", "serpapi_search_dashboard", "shell", "read", "edit", "subagent", "webfetch", "websearch", "execute", "opencode_read_mcp_resource"} {
		if got := permissionEffect(effectiveRules(cfg, "web-researcher"), action, "*"); got != "deny" {
			t.Fatalf("researcher can use %s", action)
		}
	}
}
