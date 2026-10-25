package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "", true},
		{"*", "a/b", true},
		{"*.env", ".env", true},
		{"*.env", "agents/.env", true},
		{"*.env", ".env.local", false},
		{"*.env.*", ".env.local", true},
		{"*.env.*", ".env", false},
		{"fixtures/catalog/*", "fixtures/catalog/vendors.json", true},
		{"fixtures/catalog/*", "fixtures/other/vendors.json", false},
		{"*/fixtures/catalog/*", "/tmp/proj/agents/fixtures/catalog/vendors.json", true},
		{"?", "a", true},
		{"?", "ab", false},
		{"catalog-specialist", "catalog-specialist", true},
		{"catalog-specialist", "catalog-specialist-evil", false},
	}
	for _, tc := range cases {
		if got := globMatch(tc.pattern, tc.value); got != tc.want {
			t.Fatalf("glob %q %q = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

func TestDepthEscalationDenied(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	cfg, err := loadProjectPolicy(filepath.Join(filepath.Dir(file), "..", "..", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Experimental.SubagentDepth == nil || *cfg.Experimental.SubagentDepth != 1 {
		t.Fatalf("subagent_depth=%v, want 1", cfg.Experimental.SubagentDepth)
	}
	if cfg.DefaultAgent != "vendor-orchestrator" {
		t.Fatalf("default_agent=%s", cfg.DefaultAgent)
	}
	if cfg.MCP == nil || len(cfg.MCP.Servers) != 2 {
		t.Fatalf("catalog mcp missing: %+v", cfg.MCP)
	}
	raw, ok := cfg.MCP.Servers["catalog"]
	if !ok {
		t.Fatalf("catalog mcp missing: %+v", cfg.MCP)
	}
	var catalogServer struct {
		Type     string   `json:"type"`
		Command  []string `json:"command"`
		CodeMode *bool    `json:"codemode"`
	}
	if err := json.Unmarshal(raw, &catalogServer); err != nil {
		t.Fatal(err)
	}
	if catalogServer.Type != "local" || catalogServer.CodeMode == nil || *catalogServer.CodeMode {
		t.Fatalf("catalog server=%+v", catalogServer)
	}
	joined := strings.Join(catalogServer.Command, " ")
	if !strings.Contains(joined, "catalog-mcp") {
		t.Fatalf("command=%v", catalogServer.Command)
	}
	for _, id := range []string{"catalog-specialist", "accessibility-specialist", "denial-specialist", "interval-reporter", "project-span-reporter"} {
		agent := cfg.Agents[id]
		if agent.Mode != "subagent" {
			t.Fatalf("%s mode=%s", id, agent.Mode)
		}
		if !nestedSubagentBlocked(*cfg.Experimental.SubagentDepth, agent.Mode) {
			t.Fatalf("%s can nest a subagent at depth %d", id, *cfg.Experimental.SubagentDepth)
		}
	}
	// A permission allow would still not open a second level. The harness
	// blocks it because the caller is already a subagent at depth 1.
	if !nestedSubagentBlocked(1, "subagent") || nestedSubagentBlocked(1, "primary") {
		t.Fatal("depth gate mismatch")
	}

	for _, id := range []string{"vendor-orchestrator", "denial-orchestrator", "workspace-assistant", "partner-analytics", "internal-analytics", "catalog-specialist", "accessibility-specialist", "denial-specialist", "interval-reporter", "project-span-reporter", "build", "plan", "general", "explore"} {
		for _, rule := range cfg.Agents[id].Permissions {
			if rule.Effect == "ask" {
				t.Fatalf("%s uses ask: %+v", id, rule)
			}
		}
	}

	catalog := "/tmp/proj/agents/fixtures/catalog/vendors.json"
	envFile := "/tmp/proj/agents/.env"
	escape := "/tmp/proj/agents/fixtures/catalog/../../.env"
	checks := []struct {
		agent    string
		action   string
		resource string
		want     string
	}{
		// Depth-1 specialist cannot launch the delegate its definition names.
		{"denial-specialist", "subagent", "catalog-specialist", "deny"},
		{"denial-specialist", "subagent", "general", "deny"},
		{"denial-specialist", "shell", "sh -c opencode", "deny"},
		{"denial-specialist", "edit", "fixtures/catalog/vendors.json", "deny"},
		{"denial-specialist", "webfetch", "https://example.test", "deny"},
		{"denial-specialist", "websearch", "vendors", "deny"},
		{"denial-specialist", "skill", "escape", "deny"},
		{"denial-specialist", "external_directory", "/tmp", "deny"},
		{"denial-specialist", "read", ".env", "deny"},
		{"denial-specialist", "read", envFile, "deny"},
		{"denial-specialist", "read", "fixtures/catalog/vendors.json", "deny"},
		{"denial-specialist", "catalog_search", "*", "deny"},
		// Catalog specialists may call the MCP tool and may not read the file.
		{"catalog-specialist", "catalog_search", "*", "allow"},
		{"catalog-specialist", "read", "fixtures/catalog/vendors.json", "deny"},
		{"catalog-specialist", "read", catalog, "deny"},
		{"catalog-specialist", "glob", "fixtures/catalog/vendors.json", "deny"},
		{"catalog-specialist", "grep", catalog, "deny"},
		{"catalog-specialist", "execute", "*", "deny"},
		{"catalog-specialist", "opencode_list_mcp_resources", "catalog", "deny"},
		{"catalog-specialist", "opencode_read_mcp_resource", "catalog:vendors", "deny"},
		{"catalog-specialist", "read", escape, "deny"},
		{"catalog-specialist", "read", ".env", "deny"},
		{"catalog-specialist", "read", ".env.local", "deny"},
		{"catalog-specialist", "read", "README.md", "deny"},
		{"catalog-specialist", "subagent", "catalog-specialist", "deny"},
		{"catalog-specialist", "subagent", "accessibility-specialist", "deny"},
		{"catalog-specialist", "shell", "cat fixtures/catalog/vendors.json", "deny"},
		{"catalog-specialist", "edit", catalog, "deny"},
		{"catalog-specialist", "webfetch", "https://example.test/vendors", "deny"},
		{"catalog-specialist", "external_directory", "/etc", "deny"},
		{"accessibility-specialist", "catalog_search", "*", "allow"},
		{"accessibility-specialist", "read", catalog, "deny"},
		{"accessibility-specialist", "subagent", "catalog-specialist", "deny"},
		{"accessibility-specialist", "shell", "bash", "deny"},
		// Orchestrators may launch only their allowlisted specialists.
		{"vendor-orchestrator", "subagent", "catalog-specialist", "allow"},
		{"vendor-orchestrator", "subagent", "accessibility-specialist", "allow"},
		{"vendor-orchestrator", "subagent", "denial-specialist", "deny"},
		{"vendor-orchestrator", "subagent", "general", "deny"},
		{"vendor-orchestrator", "subagent", "build", "deny"},
		{"vendor-orchestrator", "shell", "ls", "deny"},
		{"vendor-orchestrator", "edit", "opencode.jsonc", "deny"},
		{"vendor-orchestrator", "catalog_search", "*", "deny"},
		{"vendor-orchestrator", "read", catalog, "deny"},
		{"vendor-orchestrator", "read", ".env", "deny"},
		{"denial-orchestrator", "subagent", "denial-specialist", "allow"},
		{"denial-orchestrator", "subagent", "catalog-specialist", "deny"},
		{"denial-orchestrator", "catalog_search", "*", "deny"},
		{"denial-orchestrator", "shell", "sh", "deny"},
		// Built-in agents cannot be used as an escalation path.
		{"build", "shell", "sh", "deny"},
		{"build", "edit", "x", "deny"},
		{"build", "subagent", "general", "deny"},
		{"general", "shell", "sh", "deny"},
		{"explore", "webfetch", "https://example.test", "deny"},
		{"plan", "edit", "x", "deny"},
		{"compaction", "read", "README.md", "allow"},
		{"compaction", "read", ".env", "deny"},
		{"compaction", "shell", "sh", "deny"},
		{"compaction", "edit", "x", "deny"},
		{"compaction", "subagent", "general", "deny"},
	}
	for _, tc := range checks {
		got := permissionEffect(effectiveRules(cfg, tc.agent), tc.action, tc.resource)
		if got != tc.want {
			t.Fatalf("%s %s %s: got %s want %s", tc.agent, tc.action, tc.resource, got, tc.want)
		}
	}
}

func TestThreadToolsRequireApprovalForOperations(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	cfg, err := loadProjectPolicy(filepath.Join(filepath.Dir(file), "..", "..", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	rules := effectiveRules(cfg, "thread")
	checks := []struct{ action, resource, want string }{
		{"read", "README.md", "allow"},
		{"read", ".env", "deny"},
		{"glob", "*.go", "allow"},
		{"grep", "API_KEY", "ask"},
		{"shell", "pwd", "ask"},
		{"edit", "README.md", "ask"},
		{"catalog_search", "*", "allow"},
		{"tools_github_get_file", "*", "allow"},
		{"websearch", "OpenCode", "allow"},
		{"subagent", "general", "deny"},
	}
	for _, check := range checks {
		if got := permissionEffect(rules, check.action, check.resource); got != check.want {
			t.Errorf("%s %q: got %q, want %q", check.action, check.resource, got, check.want)
		}
	}
}

// Every pack tool a definition lists must be allowed for that agent in
// opencode.jsonc, and no agent may hold a pack tool its definition omits.
func TestPackToolsMatchDefinitions(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	cfg, err := loadProjectPolicy(filepath.Join(root, "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.MCP.Servers["tools"]; !ok {
		t.Fatal("tools mcp server missing")
	}
	raw, err := os.ReadFile(filepath.Join(root, "examples", "definitions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var defs []contracts.AgentDefinition
	if err := json.Unmarshal(raw, &defs); err != nil {
		t.Fatal(err)
	}
	for _, def := range defs {
		rules := effectiveRules(cfg, def.ID)
		for _, pack := range toolpacks.Packs() {
			for _, tool := range pack.Tools {
				want := "deny"
				if listed(def.AllowedTools, pack.ToolID(tool)) {
					want = "allow"
				}
				if got := permissionEffect(rules, pack.OpenCodeName(tool), "*"); got != want {
					t.Fatalf("%s %s = %s, want %s", def.ID, pack.OpenCodeName(tool), got, want)
				}
			}
		}
		// The checked-in block must equal what PermissionsFor generates for pack tools.
		for _, rule := range PermissionsFor(def) {
			if strings.HasPrefix(rule.Action, "tools_") && permissionEffect(rules, rule.Action, "*") != "allow" {
				t.Fatalf("%s: generated rule %+v is not in opencode.jsonc", def.ID, rule)
			}
		}
	}
}

func TestContractIDMapping(t *testing.T) {
	if got := canonicalToolID("tools_github_search_code"); got != "github.search_code" {
		t.Fatal(got)
	}
	if got := canonicalToolID("tools_gdrive_read_file"); got != "gdrive.read_file" {
		t.Fatal(got)
	}
	if got := canonicalToolID("catalog_search"); got != "catalog.search" {
		t.Fatal(got)
	}
}
