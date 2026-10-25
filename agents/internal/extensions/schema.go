package extensions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

var supportedKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "items": true,
	"enum": true, "additionalProperties": true,
}

func blank(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// ValidateSchemaDocument accepts the v1 keyword subset. Empty means unrestricted.
func ValidateSchemaDocument(schema json.RawMessage) error {
	if blank(schema) {
		return nil
	}
	var rules map[string]any
	if err := json.Unmarshal(schema, &rules); err != nil {
		return fail(CodeInvalidInput, "tool schema is invalid")
	}
	if err := checkDocument(rules); err != nil {
		return err
	}
	return nil
}

func checkDocument(rules map[string]any) error {
	for key := range rules {
		if !supportedKeywords[key] {
			return fail(CodeInvalidInput, "unsupported schema keyword "+key)
		}
	}
	if raw, ok := rules["type"]; ok {
		kind, ok := raw.(string)
		if !ok || !supportedType(kind) {
			return fail(CodeInvalidInput, "unsupported schema type")
		}
	}
	if raw, ok := rules["properties"]; ok {
		props, ok := raw.(map[string]any)
		if !ok {
			return fail(CodeInvalidInput, "tool schema is invalid")
		}
		for _, child := range props {
			nested, ok := child.(map[string]any)
			if !ok {
				return fail(CodeInvalidInput, "tool schema is invalid")
			}
			if err := checkDocument(nested); err != nil {
				return err
			}
		}
	}
	if raw, ok := rules["items"]; ok {
		nested, ok := raw.(map[string]any)
		if !ok {
			return fail(CodeInvalidInput, "tool schema is invalid")
		}
		if err := checkDocument(nested); err != nil {
			return err
		}
	}
	if raw, ok := rules["required"]; ok {
		items, ok := raw.([]any)
		if !ok {
			return fail(CodeInvalidInput, "tool schema is invalid")
		}
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return fail(CodeInvalidInput, "tool schema is invalid")
			}
		}
	}
	if raw, ok := rules["enum"]; ok {
		if _, ok := raw.([]any); !ok {
			return fail(CodeInvalidInput, "tool schema is invalid")
		}
	}
	if raw, ok := rules["additionalProperties"]; ok {
		switch nested := raw.(type) {
		case bool:
		case map[string]any:
			if err := checkDocument(nested); err != nil {
				return err
			}
		default:
			return fail(CodeInvalidInput, "tool schema is invalid")
		}
	}
	return nil
}

func supportedType(kind string) bool {
	switch kind {
	case "object", "array", "string", "number", "integer", "boolean", "null":
		return true
	default:
		return false
	}
}

// ValidateValue checks a JSON value against the v1 schema subset.
func ValidateValue(schema, value json.RawMessage) error {
	if len(bytes.TrimSpace(value)) == 0 || !json.Valid(value) {
		return fail(CodeInvalidInput, "value must be JSON")
	}
	if blank(schema) {
		return nil
	}
	var rules map[string]any
	if err := json.Unmarshal(schema, &rules); err != nil {
		return fail(CodeInvalidInput, "tool schema is invalid")
	}
	var parsed any
	if err := json.Unmarshal(value, &parsed); err != nil {
		return fail(CodeInvalidInput, "value must be JSON")
	}
	return checkValue(rules, parsed)
}

func checkValue(rules map[string]any, value any) error {
	for key := range rules {
		if !supportedKeywords[key] {
			return fail(CodeInvalidInput, "unsupported schema keyword "+key)
		}
	}
	if enum, ok := rules["enum"].([]any); ok {
		matched := false
		for _, candidate := range enum {
			if sameJSON(candidate, value) {
				matched = true
			}
		}
		if !matched {
			return fail(CodeInvalidInput, "value is outside the enum")
		}
	}
	kind, _ := rules["type"].(string)
	switch kind {
	case "", "null":
		if kind == "null" && value != nil {
			return fail(CodeInvalidInput, "expected null")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fail(CodeInvalidInput, "expected string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fail(CodeInvalidInput, "expected boolean")
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fail(CodeInvalidInput, "expected number")
		}
	case "integer":
		number, ok := value.(float64)
		if !ok || math.Trunc(number) != number {
			return fail(CodeInvalidInput, "expected integer")
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fail(CodeInvalidInput, "expected array")
		}
		if raw, ok := rules["items"].(map[string]any); ok {
			for _, item := range items {
				if err := checkValue(raw, item); err != nil {
					return err
				}
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fail(CodeInvalidInput, "expected object")
		}
		if required, ok := rules["required"].([]any); ok {
			for _, field := range required {
				name := fmt.Sprint(field)
				if _, present := object[name]; !present {
					return fail(CodeInvalidInput, "missing required property")
				}
			}
		}
		properties, _ := rules["properties"].(map[string]any)
		additional, hasAdditional := rules["additionalProperties"]
		for name, child := range object {
			raw, known := properties[name].(map[string]any)
			if known {
				if err := checkValue(raw, child); err != nil {
					return err
				}
				continue
			}
			if !hasAdditional {
				continue
			}
			switch typed := additional.(type) {
			case bool:
				if !typed {
					return fail(CodeInvalidInput, "unexpected property")
				}
			case map[string]any:
				if err := checkValue(typed, child); err != nil {
					return err
				}
			default:
				return fail(CodeInvalidInput, "tool schema is invalid")
			}
		}
	default:
		return fail(CodeInvalidInput, "unsupported schema type")
	}
	return nil
}

func sameJSON(left, right any) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}
