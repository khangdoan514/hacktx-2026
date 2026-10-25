package metrics

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

var (
	pemPattern        = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?-----END [A-Z0-9 ]+-----`)
	urlUserinfo       = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]+:[^/\s@]+@`)
	bearerPattern     = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]{8,}`)
	assignmentPattern = regexp.MustCompile(`(?i)\b(password|passwd|secret|api_key|authorization|credential|private_key|token)\b(\s*[=:]\s*)\S+`)
)

// RedactText removes credential material from a free-text operator message.
// A run ID in the same string is left in place. Callers still must not put
// prompts or retrieved documents in free text; pass those under a restricted
// attribute key so RedactMap drops them.
func RedactText(s string) string {
	if s == "" {
		return ""
	}
	s = pemPattern.ReplaceAllString(s, Redacted)
	s = urlUserinfo.ReplaceAllString(s, "${1}"+Redacted+"@")
	s = bearerPattern.ReplaceAllString(s, "${1} "+Redacted)
	s = assignmentPattern.ReplaceAllString(s, "${1}${2}"+Redacted)
	return s
}

// RedactMap returns a copy of attrs that is safe to attach to a log or span.
// Sensitive keys are replaced with Redacted. Restricted keys are omitted.
// The input map is not modified.
func RedactMap(attrs map[string]any) map[string]any {
	if attrs == nil {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for key, value := range attrs {
		if isRestrictedKey(key) {
			continue
		}
		if isSensitiveKey(key) {
			out[key] = Redacted
			continue
		}
		out[key] = redactValue(value)
	}
	return out
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return typed
	case string:
		return RedactText(typed)
	case map[string]any:
		return RedactMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = redactValue(typed[i])
		}
		return out
	case json.RawMessage:
		return redactRaw(typed)
	default:
		// Unknown structs can embed credentials. Do not stringify them.
		return Redacted
	}
}

func redactRaw(raw json.RawMessage) any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Redacted
	}
	switch raw[0] {
	case '{':
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil {
			return Redacted
		}
		return RedactMap(object)
	case '[':
		var items []any
		if err := json.Unmarshal(raw, &items); err != nil {
			return Redacted
		}
		return redactValue(items)
	default:
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return Redacted
		}
		return RedactText(text)
	}
}

func normalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, " ", "_")
	return key
}

func isSensitiveKey(key string) bool {
	switch normalizeKey(key) {
	case "password", "passwd", "secret", "token", "api_key", "apikey",
		"authorization", "auth", "credential", "credentials",
		"private_key", "database_url", "connection_string",
		"service_password", "supabase_secret_key", "supabase_secret",
		"access_token", "refresh_token", "session_token",
		"opencode_service_password", "bearer":
		return true
	default:
		normalized := normalizeKey(key)
		return strings.HasSuffix(normalized, "_token") ||
			strings.HasSuffix(normalized, "_secret") ||
			strings.HasSuffix(normalized, "_password") ||
			strings.HasSuffix(normalized, "_api_key") ||
			strings.HasSuffix(normalized, "_credential")
	}
}

func isRestrictedKey(key string) bool {
	switch normalizeKey(key) {
	case "prompt", "instructions", "raw_prompt", "system_prompt", "hidden_prompt",
		"retrieved_text", "retrieved_context", "document_body", "message_content",
		"raw_input":
		return true
	default:
		return false
	}
}
