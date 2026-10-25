package definitions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Canonical returns the canonical JSON for a definition spec and its
// SHA-256 hex digest. Object keys are sorted at every level, arrays keep
// their order, and there is no insignificant whitespace. Allowed model ids
// are sorted so equivalent policies hash the same; tool and delegate order
// stays significant.
func Canonical(spec Spec) ([]byte, string, error) {
	prepared, raw, hash, err := prepare(spec)
	if err != nil {
		return nil, "", err
	}
	_ = prepared
	return raw, hash, nil
}

func prepare(spec Spec) (Spec, []byte, string, error) {
	spec = sortPolicy(normalize(spec))
	raw, err := canonicalJSON(spec)
	if err != nil {
		return Spec{}, nil, "", err
	}
	sum := sha256.Sum256(raw)
	return spec, raw, hex.EncodeToString(sum[:]), nil
}

func normalize(spec Spec) Spec {
	if spec.AllowedTools == nil {
		spec.AllowedTools = []string{}
	}
	if spec.AllowedDelegates == nil {
		spec.AllowedDelegates = []string{}
	}
	if spec.ModelPolicy != nil {
		policy := *spec.ModelPolicy
		if len(policy.AllowedModels) == 0 && policy.DefaultModel == "" {
			spec.ModelPolicy = nil
		} else {
			if policy.AllowedModels == nil {
				policy.AllowedModels = []string{}
			} else {
				policy.AllowedModels = append([]string(nil), policy.AllowedModels...)
			}
			spec.ModelPolicy = &policy
		}
	}
	return spec
}

func sortPolicy(spec Spec) Spec {
	if spec.ModelPolicy == nil || len(spec.ModelPolicy.AllowedModels) < 2 {
		return spec
	}
	policy := *spec.ModelPolicy
	policy.AllowedModels = append([]string(nil), policy.AllowedModels...)
	sort.Strings(policy.AllowedModels)
	spec.ModelPolicy = &policy
	return spec
}

func canonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return canonicalBytes(raw)
}

func canonicalBytes(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("canonical JSON: %w", err)
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if typed {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		if _, err := typed.Float64(); err != nil {
			return fmt.Errorf("invalid number")
		}
		buf.WriteString(string(typed))
	case string:
		raw, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		buf.Write(raw)
	case []any:
		buf.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			raw, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buf.Write(raw)
			buf.WriteByte(':')
			if err := writeCanonical(buf, typed[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func hashTool(tool Tool) ([]byte, string, error) {
	raw, err := canonicalJSON(tool)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}
