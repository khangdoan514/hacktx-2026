package definitions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

const (
	maxIDLen           = 256
	maxVersionLen      = 128
	maxNameLen         = 200
	maxInstructionsLen = 100000
	maxActorLen        = 200
	maxRunIDLen        = 256
	maxContextSteps    = 10
)

func validToken(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func validDisplay(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func checkActor(actor string) error {
	if !validDisplay(actor, maxActorLen) {
		return &Error{Code: CodeInvalidInput, Message: "actor is required"}
	}
	return nil
}

func refKey(id, version string) string { return id + "\x00" + version }

// Validate checks a definition and, when mode is not RefNone, its tool and
// delegate references. refs may be nil only for RefNone. A nil refs with
// RefTest or RefPublish treats every reference as unknown.
// Delegate targets must use the same version string as the agent being
// checked. A specialist may name a delegate; depth is enforced at runtime,
// because the shipped denial example does so.
func Validate(spec Spec, refs *Refs, mode RefMode) []FieldError {
	spec = normalize(spec)
	fields := make([]FieldError, 0)
	add := func(field, message string) {
		fields = append(fields, FieldError{Field: field, Message: message})
	}
	if spec.ID == "" {
		add("id", "is required")
	} else if !validToken(spec.ID, maxIDLen) {
		add("id", "must be 1-256 characters without spaces or control characters")
	}
	if spec.Version == "" {
		add("version", "is required")
	} else if !validToken(spec.Version, maxVersionLen) {
		add("version", "must be 1-128 characters without spaces or control characters")
	}
	if spec.Name == "" {
		add("name", "is required")
	} else if !validDisplay(spec.Name, maxNameLen) {
		add("name", "must be 1-200 characters without leading or trailing space")
	}
	switch spec.Role {
	case "orchestrator", "specialist":
	case "":
		add("role", "is required")
	default:
		add("role", "must be orchestrator or specialist")
	}
	if strings.TrimSpace(spec.Instructions) == "" {
		add("instructions", "is required")
	} else if len(spec.Instructions) > maxInstructionsLen {
		add("instructions", "is too long")
	}
	if len(bytes.TrimSpace(spec.InputSchema)) == 0 || string(bytes.TrimSpace(spec.InputSchema)) == "null" {
		add("input_schema", "is required")
	} else {
		fields = append(fields, validateSchema("input_schema", spec.InputSchema)...)
	}
	if len(bytes.TrimSpace(spec.OutputSchema)) == 0 || string(bytes.TrimSpace(spec.OutputSchema)) == "null" {
		add("output_schema", "is required")
	} else {
		fields = append(fields, validateSchema("output_schema", spec.OutputSchema)...)
	}
	fields = append(fields, validateLimits(spec.Limits)...)
	fields = append(fields, validateModelPolicy(spec.ModelPolicy)...)
	fields = append(fields, validateRefList("allowed_tools", spec.AllowedTools, spec, refs, mode, false)...)
	fields = append(fields, validateRefList("allowed_delegates", spec.AllowedDelegates, spec, refs, mode, true)...)
	fields = append(fields, validateContextSteps(spec.ContextSteps)...)
	return sortFields(fields)
}

// validateContextSteps checks each fixed tool call. Only built-in pack tools
// can run outside a model session, and their arguments are checked against
// the tool's own properties so a bad call fails at publish, not at run time.
func validateContextSteps(steps []contracts.ContextStep) []FieldError {
	fields := make([]FieldError, 0)
	if len(steps) > maxContextSteps {
		fields = append(fields, FieldError{Field: "context_steps", Message: fmt.Sprintf("at most %d steps", maxContextSteps)})
	}
	seen := map[string]bool{}
	for i, step := range steps {
		path := fmt.Sprintf("context_steps[%d]", i)
		add := func(field, message string) {
			fields = append(fields, FieldError{Field: path + "." + field, Message: message})
		}
		if !validToken(step.ID, maxIDLen) || seen[step.ID] {
			add("id", "must be a unique id without spaces")
		}
		seen[step.ID] = true
		if !validDisplay(step.Name, maxNameLen) {
			add("name", "must be 1-200 characters without leading or trailing space")
		}
		_, tool, ok := toolpacks.Lookup(step.Tool)
		if !ok {
			add("tool", "must be a built-in tool pack tool")
			continue
		}
		args := map[string]json.RawMessage{}
		if len(bytes.TrimSpace(step.Arguments)) > 0 && string(bytes.TrimSpace(step.Arguments)) != "null" {
			if json.Unmarshal(step.Arguments, &args) != nil || args == nil {
				add("arguments", "must be a JSON object")
				continue
			}
		}
		for name := range args {
			if _, known := tool.Properties[name]; !known {
				add("arguments."+name, "is not an argument of "+step.Tool)
			}
		}
		for _, name := range tool.Required {
			value, ok := args[name]
			if !ok || string(value) == `""` || string(value) == "null" {
				add("arguments."+name, "is required")
			}
		}
	}
	return fields
}

func validateLimits(limits contracts.Limits) []FieldError {
	fields := make([]FieldError, 0)
	check := func(name string, value int) {
		if value <= 0 {
			fields = append(fields, FieldError{Field: "limits." + name, Message: "must be greater than zero"})
		}
	}
	check("max_delegations", limits.MaxDelegations)
	check("max_concurrent_agents", limits.MaxConcurrentAgents)
	check("max_tool_calls", limits.MaxToolCalls)
	check("max_model_turns", limits.MaxModelTurns)
	check("max_tokens", limits.MaxTokens)
	check("timeout_seconds", limits.TimeoutSeconds)
	if limits.MaxCostUSD <= 0 || math.IsNaN(limits.MaxCostUSD) || math.IsInf(limits.MaxCostUSD, 0) {
		fields = append(fields, FieldError{Field: "limits.max_cost_usd", Message: "must be a finite number greater than zero"})
	}
	return fields
}

func validateModelPolicy(policy *ModelPolicy) []FieldError {
	if policy == nil {
		return nil
	}
	fields := make([]FieldError, 0)
	seen := map[string]bool{}
	for i, id := range policy.AllowedModels {
		path := fmt.Sprintf("model_policy.allowed_models[%d]", i)
		if !contracts.ValidModelID(id) {
			fields = append(fields, FieldError{Field: path, Message: "invalid model id"})
		} else if seen[id] {
			fields = append(fields, FieldError{Field: path, Message: "duplicate model id"})
		}
		seen[id] = true
	}
	if policy.DefaultModel != "" {
		if !contracts.ValidModelID(policy.DefaultModel) {
			fields = append(fields, FieldError{Field: "model_policy.default_model", Message: "invalid model id"})
		} else if len(policy.AllowedModels) > 0 && !seen[policy.DefaultModel] {
			fields = append(fields, FieldError{Field: "model_policy.default_model", Message: "must be included in allowed_models"})
		}
	}
	return fields
}

func validateRefList(name string, ids []string, spec Spec, refs *Refs, mode RefMode, delegates bool) []FieldError {
	fields := make([]FieldError, 0)
	seen := map[string]bool{}
	for i, id := range ids {
		path := fmt.Sprintf("%s[%d]", name, i)
		if id == "" || seen[id] {
			fields = append(fields, FieldError{Field: path, Message: "duplicate or empty reference"})
			continue
		}
		seen[id] = true
		if delegates && id == spec.ID {
			fields = append(fields, FieldError{Field: path, Message: "an agent cannot delegate to itself"})
			continue
		}
		if mode == RefNone {
			continue
		}
		if refs == nil {
			message := "unknown tool"
			if delegates {
				message = "unknown delegate version"
			}
			fields = append(fields, FieldError{Field: path, Message: message})
			continue
		}
		if !delegates {
			if _, ok := refs.Tools[id]; !ok {
				fields = append(fields, FieldError{Field: path, Message: "unknown tool"})
			}
			continue
		}
		state, ok := refs.Agents[refKey(id, spec.Version)]
		if !ok {
			fields = append(fields, FieldError{Field: path, Message: "unknown delegate version"})
			continue
		}
		switch mode {
		case RefPublish:
			if state != StatePublished {
				fields = append(fields, FieldError{Field: path, Message: "delegate must be published at the same version"})
			}
		case RefTest:
			if state == StateRetired {
				fields = append(fields, FieldError{Field: path, Message: "delegate is retired"})
			}
		}
	}
	return fields
}

func validateSchema(path string, raw json.RawMessage) []FieldError {
	if !json.Valid(raw) {
		return []FieldError{{Field: path, Message: "must be JSON"}}
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return []FieldError{{Field: path, Message: "must be a JSON object"}}
	}
	fields := make([]FieldError, 0)
	for key, value := range doc {
		switch key {
		case "type":
			var typ string
			if json.Unmarshal(value, &typ) != nil || !supportedSchemaTypes[typ] {
				fields = append(fields, FieldError{Field: path + ".type", Message: "unsupported schema type"})
			}
		case "properties":
			var props map[string]json.RawMessage
			if json.Unmarshal(value, &props) != nil || props == nil {
				fields = append(fields, FieldError{Field: path + ".properties", Message: "must be an object"})
				continue
			}
			names := make([]string, 0, len(props))
			for name := range props {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				fields = append(fields, validateSchema(path+".properties."+name, props[name])...)
			}
		case "items":
			fields = append(fields, validateSchema(path+".items", value)...)
		case "required":
			var keys []string
			if json.Unmarshal(value, &keys) != nil {
				fields = append(fields, FieldError{Field: path + ".required", Message: "must be an array of strings"})
				continue
			}
			seen := map[string]bool{}
			for i, key := range keys {
				if key == "" || seen[key] {
					fields = append(fields, FieldError{Field: fmt.Sprintf("%s.required[%d]", path, i), Message: "duplicate or empty required field"})
				}
				seen[key] = true
			}
		case "enum":
			var vals []json.RawMessage
			if json.Unmarshal(value, &vals) != nil {
				fields = append(fields, FieldError{Field: path + ".enum", Message: "must be an array"})
			}
		case "additionalProperties":
			var allowed bool
			if json.Unmarshal(value, &allowed) != nil {
				fields = append(fields, FieldError{Field: path + ".additionalProperties", Message: "must be a boolean"})
			}
		default:
			fields = append(fields, FieldError{Field: path + "." + key, Message: "unsupported schema keyword"})
		}
	}
	return fields
}

var supportedSchemaTypes = map[string]bool{
	"object": true, "array": true, "string": true, "number": true, "integer": true, "boolean": true, "null": true,
}

func validateTool(tool Tool) []FieldError {
	fields := make([]FieldError, 0)
	add := func(field, message string) {
		fields = append(fields, FieldError{Field: field, Message: message})
	}
	if !validToken(tool.ID, maxIDLen) {
		add("id", "is required")
	}
	if !validToken(tool.Version, maxVersionLen) {
		add("version", "is required")
	}
	if strings.TrimSpace(tool.Description) == "" {
		add("description", "is required")
	}
	if tool.SideEffect != "READ_ONLY" {
		add("side_effect", "must be READ_ONLY")
	}
	if tool.TimeoutSeconds <= 0 {
		add("timeout_seconds", "must be greater than zero")
	}
	if len(bytes.TrimSpace(tool.InputSchema)) == 0 {
		add("input_schema", "is required")
	} else {
		fields = append(fields, validateSchema("input_schema", tool.InputSchema)...)
	}
	if len(bytes.TrimSpace(tool.OutputSchema)) == 0 {
		add("output_schema", "is required")
	} else {
		fields = append(fields, validateSchema("output_schema", tool.OutputSchema)...)
	}
	return sortFields(fields)
}

func validationError(fields []FieldError) error {
	fields = sortFields(fields)
	if len(fields) == 0 {
		return nil
	}
	return &Error{Code: CodeInvalidInput, Message: "definition is invalid", Fields: fields}
}

func sortFields(fields []FieldError) []FieldError {
	if len(fields) == 0 {
		return nil
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Field == fields[j].Field {
			return fields[i].Message < fields[j].Message
		}
		return fields[i].Field < fields[j].Field
	})
	return fields
}

func prefixFields(prefix string, fields []FieldError) []FieldError {
	if len(fields) == 0 {
		return nil
	}
	out := make([]FieldError, len(fields))
	for i, field := range fields {
		if field.Field == "" || field.Field == "$" {
			out[i] = FieldError{Field: prefix, Message: field.Message}
			continue
		}
		out[i] = FieldError{Field: prefix + "." + field.Field, Message: field.Message}
	}
	return out
}

func nameConflict() error {
	return &Error{
		Code:    CodeConflict,
		Message: "agent name is already in use",
		Fields:  []FieldError{{Field: "name", Message: "must be unique"}},
	}
}
