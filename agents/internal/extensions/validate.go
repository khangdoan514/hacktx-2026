package extensions

import (
	"net/url"
	"path/filepath"
	"strings"
)

func validToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func normalize(rec ToolRecord) (ToolRecord, error) {
	rec = cloneRecord(rec)
	rec.Lifecycle = LifecycleDraft
	if rec.Retry.MaxAttempts == 0 {
		rec.Retry.MaxAttempts = 1
	}
	if rec.Endpoint.Method == "" {
		rec.Endpoint.Method = "POST"
	}
	if rec.Class == ClassWorker && rec.Endpoint.WorkerID == "" {
		parsed, err := url.Parse(rec.Endpoint.URL)
		if err == nil {
			rec.Endpoint.WorkerID = parsed.Host
		}
	}
	if err := validateRecord(&rec); err != nil {
		return ToolRecord{}, err
	}
	return rec, nil
}

func validateRecord(rec *ToolRecord) error {
	if !validToken(rec.TenantID) || !validToken(rec.ID) || !validToken(rec.Version) {
		return fail(CodeInvalidInput, "tool identity is invalid")
	}
	if len(rec.Description) > 500 {
		return fail(CodeInvalidInput, "tool description is too long")
	}
	switch rec.SideEffect {
	case SideEffectReadOnly, SideEffectWrite:
	default:
		return fail(CodeInvalidInput, "tool side effect is invalid")
	}
	if rec.TimeoutMS <= 0 || rec.TimeoutMS > 300000 {
		return fail(CodeInvalidInput, "tool timeout is invalid")
	}
	if rec.Resources.MaxOutputBytes <= 0 || rec.Resources.MaxOutputBytes > 8<<20 {
		return fail(CodeInvalidInput, "tool output limit is invalid")
	}
	if rec.Retry.MaxAttempts < 1 || rec.Retry.MaxAttempts > 5 {
		return fail(CodeInvalidInput, "tool retry policy is invalid")
	}
	if rec.SideEffect == SideEffectWrite && rec.Retry.MaxAttempts != 1 {
		return fail(CodeInvalidInput, "write tools cannot set automatic retries")
	}
	if rec.AuthRef != "" && !validToken(rec.AuthRef) {
		return fail(CodeInvalidInput, "credential reference is invalid")
	}
	for _, permission := range rec.Permissions {
		if !validToken(permission) {
			return fail(CodeInvalidInput, "tool permission is invalid")
		}
	}
	if err := ValidateSchemaDocument(rec.InputSchema); err != nil {
		return err
	}
	if err := ValidateSchemaDocument(rec.OutputSchema); err != nil {
		return err
	}
	switch rec.Class {
	case ClassMCP:
		if !validToken(rec.Endpoint.MCPTool) {
			return fail(CodeInvalidInput, "MCP tool name is invalid")
		}
		if rec.Endpoint.Internal {
			return fail(CodeInvalidInput, "internal endpoints use the HTTP adapter")
		}
	case ClassHTTP:
	case ClassWorker:
		if rec.Endpoint.Internal {
			return fail(CodeInvalidInput, "internal endpoints use the HTTP adapter")
		}
	default:
		return fail(CodeInvalidInput, "tool class is invalid")
	}
	if rec.Endpoint.Method != "POST" {
		return fail(CodeInvalidInput, "tool method must be POST")
	}
	if err := AllowTarget(rec.Network, rec.Endpoint.URL); err != nil {
		return err
	}
	if rec.Filesystem.Root != "" {
		if !filepath.IsAbs(rec.Filesystem.Root) || strings.Contains(rec.Filesystem.Root, "..") {
			return fail(CodeInvalidInput, "filesystem root must be an absolute path")
		}
	}
	return nil
}
