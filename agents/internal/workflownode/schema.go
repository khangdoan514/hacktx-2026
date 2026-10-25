package workflownode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ValidateSchema checks value against the v1 JSON Schema subset: type,
// properties, required, items, enum, and additionalProperties. An empty schema
// imposes no shape rule. The value must still be JSON. Unsupported keywords
// are rejected. Error text names the rule that failed and does not echo the
// document.
func ValidateSchema(schema, value json.RawMessage) error {
	if len(bytes.TrimSpace(value)) == 0 || !json.Valid(value) {
		return fmt.Errorf("value must be JSON")
	}
	if len(bytes.TrimSpace(schema)) == 0 || string(bytes.TrimSpace(schema)) == "null" {
		return nil
	}
	if err := ValidateSchemaDocument(schema); err != nil {
		return err
	}
	var rules map[string]json.RawMessage
	if err := json.Unmarshal(schema, &rules); err != nil || rules == nil {
		return fmt.Errorf("invalid schema")
	}
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.UseNumber()
	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		return fmt.Errorf("value must be JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("value must be one JSON document")
	}
	return validateNode(rules, parsed)
}

// ValidateSchemaDocument rejects a schema that uses keywords outside the v1 subset.
func ValidateSchemaDocument(schema json.RawMessage) error {
	if len(bytes.TrimSpace(schema)) == 0 || string(bytes.TrimSpace(schema)) == "null" {
		return nil
	}
	var rules map[string]json.RawMessage
	if err := json.Unmarshal(schema, &rules); err != nil || rules == nil {
		return fmt.Errorf("invalid schema")
	}
	for key, raw := range rules {
		switch key {
		case "type":
			var kind string
			if json.Unmarshal(raw, &kind) != nil {
				return fmt.Errorf("invalid type")
			}
			switch kind {
			case "object", "array", "string", "number", "integer", "boolean", "null":
			default:
				return fmt.Errorf("unsupported type %s", kind)
			}
		case "properties":
			var props map[string]json.RawMessage
			if json.Unmarshal(raw, &props) != nil {
				return fmt.Errorf("invalid properties")
			}
			for _, sub := range props {
				if err := ValidateSchemaDocument(sub); err != nil {
					return err
				}
			}
		case "items":
			if err := ValidateSchemaDocument(raw); err != nil {
				return err
			}
		case "required":
			var keys []string
			if json.Unmarshal(raw, &keys) != nil {
				return fmt.Errorf("invalid required")
			}
		case "enum":
			var vals []json.RawMessage
			if json.Unmarshal(raw, &vals) != nil {
				return fmt.Errorf("invalid enum")
			}
		case "additionalProperties":
			var allowed bool
			if json.Unmarshal(raw, &allowed) != nil {
				return fmt.Errorf("unsupported additionalProperties")
			}
		default:
			return fmt.Errorf("unsupported schema keyword %s", key)
		}
	}
	return nil
}

func validateNode(rules map[string]json.RawMessage, value any) error {
	for key := range rules {
		switch key {
		case "type", "properties", "required", "items", "enum", "additionalProperties":
		default:
			return fmt.Errorf("unsupported schema keyword %s", key)
		}
	}
	if raw, ok := rules["type"]; ok {
		var kind string
		if json.Unmarshal(raw, &kind) != nil {
			return fmt.Errorf("invalid type")
		}
		if err := checkType(kind, value); err != nil {
			return err
		}
	}
	if raw, ok := rules["enum"]; ok {
		var vals []any
		if json.Unmarshal(raw, &vals) != nil {
			return fmt.Errorf("invalid enum")
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("value outside enum")
		}
		matched := false
		for _, candidate := range vals {
			got, err := json.Marshal(candidate)
			if err == nil && bytes.Equal(got, encoded) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("value outside enum")
		}
	}
	if raw, ok := rules["properties"]; ok {
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("properties requires an object")
		}
		var props map[string]json.RawMessage
		if json.Unmarshal(raw, &props) != nil {
			return fmt.Errorf("invalid properties")
		}
		for key, child := range object {
			sub, known := props[key]
			if !known {
				continue
			}
			var nested map[string]json.RawMessage
			if json.Unmarshal(sub, &nested) != nil {
				return fmt.Errorf("invalid property schema")
			}
			if err := validateNode(nested, child); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	if raw, ok := rules["additionalProperties"]; ok {
		var allowed bool
		if json.Unmarshal(raw, &allowed) != nil {
			return fmt.Errorf("unsupported additionalProperties")
		}
		if !allowed {
			object, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("additionalProperties requires an object")
			}
			var props map[string]json.RawMessage
			if praw, ok := rules["properties"]; ok {
				if json.Unmarshal(praw, &props) != nil {
					return fmt.Errorf("invalid properties")
				}
			}
			for key := range object {
				if _, known := props[key]; !known {
					return fmt.Errorf("unknown property %s", key)
				}
			}
		}
	}
	if raw, ok := rules["required"]; ok {
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("required requires an object")
		}
		var keys []string
		if json.Unmarshal(raw, &keys) != nil {
			return fmt.Errorf("invalid required")
		}
		for _, key := range keys {
			if _, exists := object[key]; !exists {
				return fmt.Errorf("missing %s", key)
			}
		}
	}
	if raw, ok := rules["items"]; ok {
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("items requires an array")
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) != nil {
			return fmt.Errorf("invalid items")
		}
		for i, item := range items {
			if err := validateNode(nested, item); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	}
	return nil
}

func checkType(kind string, value any) error {
	switch kind {
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("expected object")
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("expected array")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "number":
		if _, ok := value.(json.Number); !ok {
			return fmt.Errorf("expected number")
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok || strings.ContainsAny(string(number), ".eE") {
			return fmt.Errorf("expected integer")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "null":
		if value != nil {
			return fmt.Errorf("expected null")
		}
	default:
		return fmt.Errorf("unsupported type %s", kind)
	}
	return nil
}
