package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareProjectDeniesShell(t *testing.T) {
	dir := t.TempDir()
	catalog := filepath.Join(dir, "src.json")
	if err := os.WriteFile(catalog, []byte(`[{"id":"vendor-a"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "project")
	defs := testDefs()
	if err := PrepareProject(project, catalog, defs, "opencode/beam-free"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(project, "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Permissions []Permission              `json:"permissions"`
		Agents      map[string]map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Permissions) != 1 || cfg.Permissions[0].Effect != "deny" {
		t.Fatalf("global permissions=%v", cfg.Permissions)
	}
	root, ok := cfg.Agents["root"]
	if !ok || root["mode"] != "primary" {
		t.Fatalf("root agent=%v", root)
	}
	one := cfg.Agents["one"]
	if one["mode"] != "subagent" {
		t.Fatalf("specialist=%v", one)
	}
	rules, _ := json.Marshal(one["permissions"])
	var perms []Permission
	if err := json.Unmarshal(rules, &perms); err != nil {
		t.Fatal(err)
	}
	sawRead := false
	for _, rule := range perms {
		if rule.Action == "shell" && rule.Effect == "allow" {
			t.Fatal("shell allowed")
		}
		if rule.Action == "read" && rule.Effect == "allow" {
			sawRead = true
		}
	}
	if !sawRead {
		t.Fatal("catalog read was not allowed")
	}
	if _, err := os.Stat(filepath.Join(project, "catalog", "vendors.json")); err != nil {
		t.Fatal(err)
	}
}
