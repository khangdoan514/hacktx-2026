package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
)

var supportedKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "items": true, "enum": true, "additionalProperties": true,
}

func ValidateSchema(schema, value json.RawMessage) error {
	if len(bytes.TrimSpace(value)) == 0 || !json.Valid(value) {
		return fmt.Errorf("value must be JSON")
	}
	if len(bytes.TrimSpace(schema)) == 0 || string(bytes.TrimSpace(schema)) == "null" {
		return nil
	}
	var rules map[string]any
	if err := json.Unmarshal(schema, &rules); err != nil {
		return err
	}
	var parsed any
	if err := json.Unmarshal(value, &parsed); err != nil {
		return err
	}
	return check(rules, parsed)
}

func check(rules map[string]any, value any) error {
	for key := range rules {
		if !supportedKeywords[key] {
			return fmt.Errorf("unsupported schema keyword %s", key)
		}
	}
	if enum, ok := rules["enum"].([]any); ok {
		matched := false
		for _, candidate := range enum {
			if fmt.Sprint(candidate) == fmt.Sprint(value) {
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("value is outside the enum")
		}
	}
	kind, _ := rules["type"].(string)
	switch kind {
	case "", "null":
		if kind == "null" && value != nil {
			return fmt.Errorf("expected null")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("expected number")
		}
	case "integer":
		number, ok := value.(float64)
		if !ok || math.Trunc(number) != number {
			return fmt.Errorf("expected integer")
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array")
		}
		if raw, ok := rules["items"].(map[string]any); ok {
			for _, item := range items {
				if err := check(raw, item); err != nil {
					return err
				}
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object")
		}
		if required, ok := rules["required"].([]any); ok {
			for _, field := range required {
				name := fmt.Sprint(field)
				if _, present := object[name]; !present {
					return fmt.Errorf("missing %s", name)
				}
			}
		}
		properties, _ := rules["properties"].(map[string]any)
		additional, hasAdditional := rules["additionalProperties"]
		for name, child := range object {
			raw, known := properties[name].(map[string]any)
			if known {
				if err := check(raw, child); err != nil {
					return err
				}
				continue
			}
			if hasAdditional {
				switch typed := additional.(type) {
				case bool:
					if !typed {
						return fmt.Errorf("unexpected property %s", name)
					}
				case map[string]any:
					if err := check(typed, child); err != nil {
						return err
					}
				default:
					return fmt.Errorf("invalid additionalProperties")
				}
			}
		}
	default:
		return fmt.Errorf("unsupported schema type %s", kind)
	}
	return nil
}

func ValidateAction(action contracts.ModelAction) error {
	payloads := 0
	if len(bytes.TrimSpace(action.Output)) > 0 && string(bytes.TrimSpace(action.Output)) != "null" {
		payloads++
	}
	if action.ToolCall != nil {
		payloads++
	}
	if action.Delegation != nil {
		payloads++
	}
	if payloads != 1 {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "an action needs exactly one payload"}
	}
	switch action.Type {
	case contracts.ActionFinish:
		if action.ToolCall != nil || action.Delegation != nil || !json.Valid(action.Output) {
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "FINISH requires output JSON"}
		}
	case contracts.ActionTool:
		if action.ToolCall == nil || action.ToolCall.ToolID == "" || !json.Valid(action.ToolCall.Input) {
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "TOOL requires a tool call"}
		}
	case contracts.ActionDelegate:
		if action.Delegation == nil || action.Delegation.AgentID == "" || !json.Valid(action.Delegation.Input) {
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "DELEGATE requires a delegation"}
		}
	default:
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "unknown action"}
	}
	return nil
}

func PositiveLimits(limits contracts.Limits) bool {
	return limits.MaxDelegations > 0 && limits.MaxConcurrentAgents > 0 && limits.MaxToolCalls > 0 &&
		limits.MaxModelTurns > 0 && limits.MaxTokens > 0 && limits.MaxCostUSD > 0 &&
		!math.IsNaN(limits.MaxCostUSD) && !math.IsInf(limits.MaxCostUSD, 0) && limits.TimeoutSeconds > 0
}

func tighter(request, definition contracts.Limits) contracts.Limits {
	limit := request
	if definition.MaxDelegations > 0 && definition.MaxDelegations < limit.MaxDelegations {
		limit.MaxDelegations = definition.MaxDelegations
	}
	if definition.MaxConcurrentAgents > 0 && definition.MaxConcurrentAgents < limit.MaxConcurrentAgents {
		limit.MaxConcurrentAgents = definition.MaxConcurrentAgents
	}
	if definition.MaxToolCalls > 0 && definition.MaxToolCalls < limit.MaxToolCalls {
		limit.MaxToolCalls = definition.MaxToolCalls
	}
	if definition.MaxModelTurns > 0 && definition.MaxModelTurns < limit.MaxModelTurns {
		limit.MaxModelTurns = definition.MaxModelTurns
	}
	if definition.TimeoutSeconds > 0 && definition.TimeoutSeconds < limit.TimeoutSeconds {
		limit.TimeoutSeconds = definition.TimeoutSeconds
	}
	return limit
}

func LinesInDiff(diff string) map[int]bool {
	lines := map[int]bool{}
	newLine := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "@@") {
			var oldStart, oldCount, nextStart, nextCount int
			if _, err := fmt.Sscanf(line, "@@ -%d,%d +%d,%d @@", &oldStart, &oldCount, &nextStart, &nextCount); err != nil {
				_, _ = fmt.Sscanf(line, "@@ -%d +%d,%d @@", &oldStart, &nextStart, &nextCount)
			}
			newLine = nextStart
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			lines[newLine] = true
			newLine++
			continue
		}
		if strings.HasPrefix(line, "-") {
			continue
		}
		if newLine > 0 {
			lines[newLine] = true
			newLine++
		}
	}
	return lines
}

func CitationsAllowed(input, output json.RawMessage) error {
	var body map[string]any
	if json.Unmarshal(output, &body) != nil {
		return fmt.Errorf("output is not an object")
	}
	findings, ok := body["findings"].([]any)
	if !ok {
		return nil
	}
	var source map[string]any
	_ = json.Unmarshal(input, &source)
	diff, _ := source["diff"].(string)
	if diff == "" {
		return fmt.Errorf("findings require the diff they cite")
	}
	allowed := LinesInDiff(diff)
	for _, item := range findings {
		finding, _ := item.(map[string]any)
		line, _ := finding["line"].(float64)
		if !allowed[int(line)] {
			return fmt.Errorf("finding cites line %d, which is not in the diff", int(line))
		}
	}
	return nil
}
