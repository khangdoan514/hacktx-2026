package opencode

// Permission evaluation mirrors OpenCode V2's documented order so the
// checked-in opencode.jsonc can be tested without a live model call.
// See https://opencode.ai/v2/docs/permissions/ and agents/docs/opencode-adoption.md.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// basePolicy is the ordered policy OpenCode applies before project rules.
// A later deny must override the .env ask, or a headless --auto client could
// approve the read.
var basePolicy = []permissionRule{
	{Action: "*", Resource: "*", Effect: "allow"},
	{Action: "read", Resource: "*.env", Effect: "ask"},
	{Action: "read", Resource: "*.env.*", Effect: "ask"},
	{Action: "read", Resource: "*.env.example", Effect: "allow"},
}

type permissionRule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

type agentPolicy struct {
	Mode        string           `json:"mode"`
	Permissions []permissionRule `json:"permissions"`
}

type projectPolicy struct {
	DefaultAgent string                 `json:"default_agent"`
	Permissions  []permissionRule       `json:"permissions"`
	Agents       map[string]agentPolicy `json:"agents"`
	Experimental struct {
		SubagentDepth *int `json:"subagent_depth"`
	} `json:"experimental"`
	MCP *struct {
		Servers map[string]json.RawMessage `json:"servers"`
	} `json:"mcp"`
}

func loadProjectPolicy(path string) (projectPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return projectPolicy{}, err
	}
	var cfg projectPolicy
	if err := json.Unmarshal([]byte(stripJSONC(string(data))), &cfg); err != nil {
		return projectPolicy{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// effectiveRules is base policy, then project rules, then that agent's rules.
// Shipped agent extras sit before project rules; config rules are last.
func effectiveRules(cfg projectPolicy, agent string) []permissionRule {
	rules := append([]permissionRule{}, basePolicy...)
	rules = append(rules, cfg.Permissions...)
	if entry, ok := cfg.Agents[agent]; ok {
		rules = append(rules, entry.Permissions...)
	}
	return rules
}

// permissionEffect returns the last matching effect. OpenCode asks when no
// rule matches, which is unsafe for an unattended service.
func permissionEffect(rules []permissionRule, action, resource string) string {
	effect := "ask"
	for _, rule := range rules {
		if globMatch(rule.Action, action) && globMatch(rule.Resource, resource) {
			effect = rule.Effect
		}
	}
	return effect
}

// nestedSubagentBlocked reports the harness depth gate. subagent_depth 1
// lets a primary session launch one child and stops that child from
// launching another, even when a permission rule would allow it.
func nestedSubagentBlocked(depth int, mode string) bool {
	return mode == "subagent" && depth <= 1
}

func globMatch(pattern, value string) bool {
	return globMatchAt(pattern, value)
}

func globMatchAt(pattern, value string) bool {
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i == len(pattern)-1 {
				return true
			}
			rest := pattern[i+1:]
			for j := 0; j <= len(value); j++ {
				if globMatchAt(rest, value[j:]) {
					return true
				}
			}
			return false
		case '?':
			if value == "" {
				return false
			}
			value = value[1:]
		default:
			if value == "" || value[0] != pattern[i] {
				return false
			}
			value = value[1:]
		}
	}
	return value == ""
}

func stripJSONC(in string) string {
	var b strings.Builder
	inString := false
	escape := false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if inString {
			b.WriteByte(c)
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(in) && in[i+1] == '/' {
			for i < len(in) && in[i] != '\n' {
				i++
			}
			if i < len(in) {
				b.WriteByte('\n')
			}
			continue
		}
		if c == '/' && i+1 < len(in) && in[i+1] == '*' {
			i += 2
			for i+1 < len(in) && !(in[i] == '*' && in[i+1] == '/') {
				i++
			}
			if i+1 < len(in) {
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	return stripTrailingCommas(b.String())
}

func stripTrailingCommas(in string) string {
	var b strings.Builder
	inString := false
	escape := false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if inString {
			b.WriteByte(c)
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(in) && (in[j] == ' ' || in[j] == '\n' || in[j] == '\r' || in[j] == '\t') {
				j++
			}
			if j < len(in) && (in[j] == '}' || in[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
