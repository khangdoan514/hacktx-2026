package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

// Permission is one OpenCode v2 rule. The last matching rule wins, so a broad
// deny comes before the specific allows.
type Permission struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

const catalogRel = "catalog/vendors.json"

// PrepareProject turns the session directory into the OpenCode project for
// these definitions. An empty directory makes OpenCode a general coding agent,
// which is not this API. The catalog file is the only data the specialists
// may read, and shell, edit, and web tools are denied.
func PrepareProject(dir, catalogSrc string, defs []contracts.AgentDefinition, model string) error {
	if dir == "" {
		return fmt.Errorf("opencode project directory is required")
	}
	if err := os.MkdirAll(filepath.Join(dir, "catalog"), 0o755); err != nil {
		return err
	}
	catalog, err := os.ReadFile(catalogSrc)
	if err != nil {
		return fmt.Errorf("read catalog: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, catalogRel), catalog, 0o644); err != nil {
		return err
	}
	agents := map[string]any{}
	for _, def := range defs {
		if def.ID == "" {
			continue
		}
		mode := "subagent"
		if def.Role != "specialist" && len(def.AllowedDelegates) > 0 {
			mode = "primary"
		}
		entry := map[string]any{
			"description": def.Name,
			"mode":        mode,
			"system":      systemPrompt(def),
			"permissions": PermissionsFor(def),
		}
		if def.Limits.MaxModelTurns > 0 {
			entry["steps"] = def.Limits.MaxModelTurns
		}
		agents[def.ID] = entry
	}
	body, err := json.MarshalIndent(map[string]any{
		"$schema":     "https://opencode.ai/config.json",
		"model":       model,
		"permissions": []Permission{{Action: "*", Resource: "*", Effect: "deny"}},
		"agents":      agents,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "opencode.jsonc"), append(body, '\n'), 0o644)
}

// PermissionsFor locks one agent. Specialists cannot delegate, even when the
// definition names a delegate in order to show that the call is denied.
// Orchestrators may call only their allowlisted specialists. A catalog tool
// is the read of catalog/vendors.json, not a shell command.
func PermissionsFor(def contracts.AgentDefinition) []Permission {
	rules := []Permission{{Action: "*", Resource: "*", Effect: "deny"}}
	if def.Role != "specialist" {
		for _, id := range def.AllowedDelegates {
			rules = append(rules, Permission{Action: "subagent", Resource: id, Effect: "allow"})
		}
	}
	if allowsCatalog(def) {
		rules = append(rules, Permission{Action: "read", Resource: catalogRel, Effect: "allow"})
		rules = append(rules, Permission{Action: "read", Resource: "**/vendors.json", Effect: "allow"})
	}
	for _, tool := range def.AllowedTools {
		if name, ok := toolpacks.OpenCodeNameFor(tool); ok {
			rules = append(rules, Permission{Action: name, Resource: "*", Effect: "allow"})
		}
	}
	return rules
}

func allowsCatalog(def contracts.AgentDefinition) bool {
	for _, tool := range def.AllowedTools {
		if tool == "catalog.search" || tool == "catalog.query" {
			return true
		}
	}
	return false
}

func systemPrompt(def contracts.AgentDefinition) string {
	var sb strings.Builder
	sb.WriteString(def.Name + ". " + def.Instructions + " ")
	if def.Role == "specialist" || len(def.AllowedDelegates) == 0 {
		sb.WriteString("Do not delegate. ")
	} else {
		sb.WriteString("Delegate only with the subagent tool to: " + strings.Join(def.AllowedDelegates, ", ") + ". ")
	}
	if allowsCatalog(def) {
		sb.WriteString("The only file you may read is " + catalogRel + ". ")
	} else {
		sb.WriteString("Do not read files; specialists read the catalog. ")
	}
	sb.WriteString("Do not use shell, glob, grep, edit, or web tools. Reply with only a JSON object matching " + string(def.OutputSchema))
	return sb.String()
}
