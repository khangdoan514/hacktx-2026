package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Validate checks the deliberately small v1 JSON Schema subset.
func Validate(schema, value json.RawMessage) error {
	if len(value) == 0 || !json.Valid(value) {
		return fmt.Errorf("invalid JSON")
	}
	if len(schema) == 0 {
		return nil
	}
	var s map[string]json.RawMessage
	if err := json.Unmarshal(schema, &s); err != nil || s == nil {
		return fmt.Errorf("invalid schema")
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(value))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return err
	}
	return validateNode(s, v)
}

func validateSchema(schema json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	var s map[string]json.RawMessage
	if json.Unmarshal(schema, &s) != nil || s == nil {
		return fmt.Errorf("invalid schema")
	}
	for k, raw := range s {
		switch k {
		case "type":
			var typ string
			if json.Unmarshal(raw, &typ) != nil {
				return fmt.Errorf("invalid type")
			}
			switch typ {
			case "object", "array", "string", "number", "integer", "boolean", "null":
			default:
				return fmt.Errorf("unsupported type %s", typ)
			}
		case "properties":
			var props map[string]json.RawMessage
			if json.Unmarshal(raw, &props) != nil {
				return fmt.Errorf("invalid properties")
			}
			for _, sub := range props {
				if err := validateSchema(sub); err != nil {
					return err
				}
			}
		case "items":
			if err := validateSchema(raw); err != nil {
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
			return fmt.Errorf("unsupported schema keyword %s", k)
		}
	}
	return nil
}

func validateNode(s map[string]json.RawMessage, v any) error {
	for k := range s {
		switch k {
		case "type", "properties", "required", "items", "enum", "additionalProperties":
		default:
			return fmt.Errorf("unsupported schema keyword %s", k)
		}
	}
	if raw, ok := s["type"]; ok {
		var typ string
		if json.Unmarshal(raw, &typ) != nil {
			return fmt.Errorf("invalid type")
		}
		good := false
		switch typ {
		case "object":
			_, good = v.(map[string]any)
		case "array":
			_, good = v.([]any)
		case "string":
			_, good = v.(string)
		case "number":
			_, good = v.(json.Number)
		case "integer":
			n, ok := v.(json.Number)
			good = ok && !strings.ContainsAny(string(n), ".eE")
		case "boolean":
			_, good = v.(bool)
		case "null":
			good = v == nil
		default:
			return fmt.Errorf("unsupported type %s", typ)
		}
		if !good {
			return fmt.Errorf("expected %s", typ)
		}
	}
	if raw, ok := s["enum"]; ok {
		var vals []any
		if json.Unmarshal(raw, &vals) != nil {
			return fmt.Errorf("invalid enum")
		}
		good := false
		want, _ := json.Marshal(v)
		for _, x := range vals {
			got, _ := json.Marshal(x)
			if bytes.Equal(got, want) {
				good = true
				break
			}
		}
		if !good {
			return fmt.Errorf("value outside enum")
		}
	}
	if raw, ok := s["properties"]; ok {
		obj, yes := v.(map[string]any)
		if !yes {
			return fmt.Errorf("properties requires object")
		}
		var props map[string]json.RawMessage
		if json.Unmarshal(raw, &props) != nil {
			return fmt.Errorf("invalid properties")
		}
		for key, sch := range props {
			if child, exists := obj[key]; exists {
				var sub map[string]json.RawMessage
				if json.Unmarshal(sch, &sub) != nil {
					return fmt.Errorf("invalid property schema")
				}
				if err := validateNode(sub, child); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			}
		}
		if extra, ok := s["additionalProperties"]; ok {
			var allowed bool
			if json.Unmarshal(extra, &allowed) != nil {
				return fmt.Errorf("unsupported additionalProperties")
			}
			if !allowed {
				for key := range obj {
					if _, known := props[key]; !known {
						return fmt.Errorf("unknown property %s", key)
					}
				}
			}
		}
	}
	if raw, ok := s["required"]; ok {
		obj, yes := v.(map[string]any)
		if !yes {
			return fmt.Errorf("required requires object")
		}
		var keys []string
		if json.Unmarshal(raw, &keys) != nil {
			return fmt.Errorf("invalid required")
		}
		for _, key := range keys {
			if _, exists := obj[key]; !exists {
				return fmt.Errorf("missing %s", key)
			}
		}
	}
	if raw, ok := s["items"]; ok {
		arr, yes := v.([]any)
		if !yes {
			return fmt.Errorf("items requires array")
		}
		var sub map[string]json.RawMessage
		if json.Unmarshal(raw, &sub) != nil {
			return fmt.Errorf("invalid items")
		}
		for i, item := range arr {
			if err := validateNode(sub, item); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	}
	return nil
}

// extractFirstObject returns the first balanced {...} span so surrounding
// chatter or code fences from chatty providers do not fail an otherwise
// valid action. The extracted object is still strictly validated.
func extractFirstObject(content string) string {
	start := strings.Index(content, "{")
	if start < 0 {
		return content
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		c := content[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		} else if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return content[start : i+1]
			}
		}
	}
	return content
}
