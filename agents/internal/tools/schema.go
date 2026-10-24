package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

// ValidateJSON checks a value against the deliberately small v1 JSON Schema
// subset. Unknown schema keywords are rejected so constraints cannot vanish.
func ValidateJSON(schema, value json.RawMessage) error {
	if len(bytes.TrimSpace(value)) == 0 || !json.Valid(value) {
		return fmt.Errorf("invalid JSON value")
	}
	if len(bytes.TrimSpace(schema)) == 0 {
		return nil
	}
	var rule map[string]json.RawMessage
	if err := strictDecode(schema, &rule); err != nil || rule == nil {
		return fmt.Errorf("invalid JSON schema")
	}
	if err := validateSchemaRule(rule); err != nil {
		return err
	}
	var data any
	if err := strictDecode(value, &data); err != nil {
		return fmt.Errorf("invalid JSON value")
	}
	return validateRule(rule, data, "$")
}

// ValidateSchema checks the supported grammar without requiring a sample value.
func ValidateSchema(schema json.RawMessage) error {
	if len(bytes.TrimSpace(schema)) == 0 {
		return nil
	}
	var rule map[string]json.RawMessage
	if err := strictDecode(schema, &rule); err != nil || rule == nil {
		return fmt.Errorf("invalid JSON schema")
	}
	return validateSchemaRule(rule)
}

func validateSchemaRule(rule map[string]json.RawMessage) error {
	for key, raw := range rule {
		switch key {
		case "type":
			var typ string
			if err := json.Unmarshal(raw, &typ); err != nil {
				return fmt.Errorf("invalid type schema")
			}
			switch typ {
			case "object", "array", "string", "number", "integer", "boolean", "null":
			default:
				return fmt.Errorf("unsupported schema type %q", typ)
			}
		case "properties":
			var props map[string]json.RawMessage
			if err := strictDecode(raw, &props); err != nil || props == nil {
				return fmt.Errorf("invalid properties schema")
			}
			for _, nestedRaw := range props {
				var nested map[string]json.RawMessage
				if err := strictDecode(nestedRaw, &nested); err != nil || nested == nil {
					return fmt.Errorf("invalid property schema")
				}
				if err := validateSchemaRule(nested); err != nil {
					return err
				}
			}
		case "items":
			var nested map[string]json.RawMessage
			if err := strictDecode(raw, &nested); err != nil || nested == nil {
				return fmt.Errorf("invalid items schema")
			}
			if err := validateSchemaRule(nested); err != nil {
				return err
			}
		case "required":
			var names []string
			if err := strictDecode(raw, &names); err != nil || names == nil {
				return fmt.Errorf("invalid required schema")
			}
		case "enum":
			var values []any
			if err := strictDecode(raw, &values); err != nil || len(values) == 0 {
				return fmt.Errorf("invalid enum schema")
			}
		case "additionalProperties":
			var flag bool
			if err := json.Unmarshal(raw, &flag); err != nil {
				return fmt.Errorf("invalid additionalProperties schema")
			}
		default:
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	return nil
}

func strictDecode(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing or invalid JSON")
	}
	return nil
}

func validateRule(rule map[string]json.RawMessage, value any, path string) error {
	for key := range rule {
		switch key {
		case "type", "properties", "required", "items", "enum", "additionalProperties":
		default:
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	if raw, ok := rule["type"]; ok {
		var typ string
		if err := json.Unmarshal(raw, &typ); err != nil {
			return fmt.Errorf("invalid type schema")
		}
		valid := false
		switch typ {
		case "object":
			_, valid = value.(map[string]any)
		case "array":
			_, valid = value.([]any)
		case "string":
			_, valid = value.(string)
		case "number":
			_, valid = value.(json.Number)
		case "integer":
			if number, ok := value.(json.Number); ok {
				_, err := number.Int64()
				valid = err == nil
			}
		case "boolean":
			_, valid = value.(bool)
		case "null":
			valid = value == nil
		default:
			return fmt.Errorf("unsupported schema type %q", typ)
		}
		if !valid {
			return fmt.Errorf("%s must be %s", path, typ)
		}
	}
	if raw, ok := rule["enum"]; ok {
		var choices []any
		if err := strictDecode(raw, &choices); err != nil {
			return fmt.Errorf("invalid enum schema")
		}
		found := false
		for _, choice := range choices {
			if reflect.DeepEqual(choice, value) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s is outside enum", path)
		}
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		for _, key := range []string{"properties", "required", "additionalProperties"} {
			if _, ok := rule[key]; ok {
				return fmt.Errorf("%s must be object", path)
			}
		}
	} else {
		properties := map[string]json.RawMessage{}
		if raw, ok := rule["properties"]; ok {
			if err := strictDecode(raw, &properties); err != nil {
				return fmt.Errorf("invalid properties schema")
			}
		}
		if raw, ok := rule["required"]; ok {
			var names []string
			if err := strictDecode(raw, &names); err != nil {
				return fmt.Errorf("invalid required schema")
			}
			for _, name := range names {
				if _, exists := object[name]; !exists {
					return fmt.Errorf("%s.%s is required", path, name)
				}
			}
		}
		allowAdditional := true
		if raw, ok := rule["additionalProperties"]; ok {
			if err := json.Unmarshal(raw, &allowAdditional); err != nil {
				return fmt.Errorf("invalid additionalProperties schema")
			}
		}
		for name, item := range object {
			raw, exists := properties[name]
			if !exists {
				if !allowAdditional {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
				continue
			}
			var nested map[string]json.RawMessage
			if err := strictDecode(raw, &nested); err != nil {
				return fmt.Errorf("invalid property schema")
			}
			if err := validateRule(nested, item, path+"."+name); err != nil {
				return err
			}
		}
	}
	if raw, ok := rule["items"]; ok {
		items, valid := value.([]any)
		if !valid {
			return fmt.Errorf("%s must be array", path)
		}
		var nested map[string]json.RawMessage
		if err := strictDecode(raw, &nested); err != nil {
			return fmt.Errorf("invalid items schema")
		}
		for i, item := range items {
			if err := validateRule(nested, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
