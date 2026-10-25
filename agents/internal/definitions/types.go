// Package definitions stores versioned agent definitions for Pass 2.
//
// A published version is immutable. Editing it means saving a new version.
// A run pin keeps the spec that was current when the run started, including
// after a later publish or retire. File arrays shaped like
// agents/examples/definitions.json import without lifecycle fields.
//
// This package does not start runs. Handler and Mount serve the lifecycle
// HTTP API. The agent service reads published definitions at run startup; run
// pins and policy enforcement are handled by later integration work.
package definitions

import (
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

const (
	// SchemaVersion is the canonical document version this package writes.
	// Importers reject any other value. The SQL column stays an open string
	// so a later pass can store another version beside it.
	SchemaVersion = "1"
	// DefaultTenant is the single internal tenant used until Pass 4.
	DefaultTenant = "local"

	StateDraft     = "draft"
	StateTesting   = "testing"
	StatePublished = "published"
	StateRetired   = "retired"

	// BuiltinCatalogToolID is the read-only tool referenced by the example
	// definitions. Version 1.0.0 is seeded and then immutable.
	BuiltinCatalogToolID      = "catalog.search"
	BuiltinCatalogToolVersion = "1.0.0"
)

const (
	CodeInvalidInput = string(contracts.ErrInvalidInput)
	CodeNotFound     = string(contracts.ErrNotFound)
	// CodeRetired, CodeConflict, and CodeImmutable are registry outcomes.
	// They are not v1 run error codes; do not copy them into RunError
	// without a contract change.
	CodeRetired   = "RETIRED"
	CodeConflict  = "CONFLICT"
	CodeImmutable = "IMMUTABLE"
)

// FieldError is one publish or import failure at a JSON path such as
// "limits.max_tokens" or "definitions[0].allowed_delegates[1]".
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error is a registry failure. Fields is set when a definition was rejected
// before any write.
type Error struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Fields) == 0 {
		return e.Code + ": " + e.Message
	}
	paths := make([]string, len(e.Fields))
	for i, field := range e.Fields {
		paths[i] = field.Field
	}
	return e.Code + ": " + e.Message + " (" + strings.Join(paths, ", ") + ")"
}

// ModelPolicy optionally restricts the per-run model. A nil policy preserves
// the v1 file shape: the service default and any syntactically valid model id
// are allowed. Secrets never belong in a policy or anywhere else in a spec.
type ModelPolicy struct {
	DefaultModel  string   `json:"default_model,omitempty"`
	AllowedModels []string `json:"allowed_models,omitempty"`
}

// Allows reports whether a start request may use modelID under this policy.
// An empty modelID means the service default. When an allow list exists and
// no default is set, the caller must pass an explicit model.
func (p *ModelPolicy) Allows(modelID string) error {
	if modelID == "" {
		if p == nil || len(p.AllowedModels) == 0 || p.DefaultModel != "" {
			return nil
		}
		return &Error{Code: CodeInvalidInput, Message: "model_id is required by model policy", Fields: []FieldError{{Field: "model_id", Message: "required by model policy"}}}
	}
	if !contracts.ValidModelID(modelID) {
		return &Error{Code: CodeInvalidInput, Message: "invalid model id", Fields: []FieldError{{Field: "model_id", Message: "invalid model id"}}}
	}
	if p == nil || len(p.AllowedModels) == 0 {
		return nil
	}
	for _, allowed := range p.AllowedModels {
		if allowed == modelID {
			return nil
		}
	}
	return &Error{Code: CodeInvalidInput, Message: "model is not allowed", Fields: []FieldError{{Field: "model_id", Message: "not included in model_policy.allowed_models"}}}
}

// Spec is the canonical definition body. It embeds the v1 agent definition so
// a file object unmarshals without lifecycle metadata. model_policy is omitted
// when absent, which is how examples/definitions.json stays valid.
type Spec struct {
	contracts.AgentDefinition
	ModelPolicy *ModelPolicy `json:"model_policy,omitempty"`
}

// Definition returns the v1 contract value stored in the spec.
func (s Spec) Definition() contracts.AgentDefinition {
	definition := s.AgentDefinition
	if s.ModelPolicy != nil {
		definition.PreferredModel = s.ModelPolicy.DefaultModel
	}
	return definition
}

// AllowsModel checks the spec's model policy.
func (s Spec) AllowsModel(modelID string) error { return s.ModelPolicy.Allows(modelID) }

// Version is one stored definition version plus lifecycle metadata.
// Metadata is not part of the content hash.
type Version struct {
	TenantID      string     `json:"-"`
	Spec          Spec       `json:"spec"`
	SchemaVersion string     `json:"schema_version"`
	ContentHash   string     `json:"content_hash"`
	State         string     `json:"state"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	TestedBy      string     `json:"tested_by,omitempty"`
	TestedAt      *time.Time `json:"tested_at,omitempty"`
	PublishedBy   string     `json:"published_by,omitempty"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	RetiredBy     string     `json:"retired_by,omitempty"`
	RetiredAt     *time.Time `json:"retired_at,omitempty"`
}

// AllowsModel checks the pinned spec's model policy.
func (v Version) AllowsModel(modelID string) error { return v.Spec.AllowsModel(modelID) }

// Pin is the immutable copy of a definition bound to one run and agent.
type Pin struct {
	RunID       string    `json:"run_id"`
	TenantID    string    `json:"-"`
	AgentID     string    `json:"agent_id"`
	Version     string    `json:"version"`
	ContentHash string    `json:"content_hash"`
	Spec        Spec      `json:"spec"`
	PinnedAt    time.Time `json:"pinned_at"`
}

// Tool is an immutable tool version used to satisfy allowed_tools.
// This pass registers read-only tools. Pass 6 owns execution and extra
// side-effect classes; it must add a new version instead of rewriting one.
type Tool struct {
	ID             string               `json:"id"`
	Version        string               `json:"version"`
	Description    string               `json:"description"`
	InputSchema    contracts.JSONSchema `json:"input_schema"`
	OutputSchema   contracts.JSONSchema `json:"output_schema"`
	SideEffect     string               `json:"side_effect"`
	TimeoutSeconds int                  `json:"timeout_seconds"`
}

// RefMode selects how delegate and tool references are checked.
type RefMode int

const (
	// RefNone checks the spec itself and skips the catalog.
	RefNone RefMode = iota
	// RefTest allows delegates in draft, testing, or published at the same version.
	RefTest
	// RefPublish requires delegates published at the same version.
	RefPublish
)

// Refs is the catalog used to validate tool and delegate references.
// Agents maps id and version to a lifecycle state. Tools is the set of
// registered tool ids; allowed_tools names an id, not a tool version.
type Refs struct {
	Agents map[string]string
	Tools  map[string]struct{}
}

// ImportMode selects how a file or definitions envelope is stored.
type ImportMode string

const (
	ImportDraft   ImportMode = "draft"
	ImportPublish ImportMode = "publish"
)

// ImportResult reports one all-or-nothing import.
type ImportResult struct {
	Versions  []Version `json:"versions"`
	Created   int       `json:"created"`
	Updated   int       `json:"updated"`
	Unchanged int       `json:"unchanged"`
}
