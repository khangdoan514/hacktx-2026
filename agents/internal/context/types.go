package context

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"
)

const (
	// BundleSchemaVersion is the snapshot/bundle shape this package writes.
	// Version 0 snapshots predate agent-memory references and remain readable.
	BundleSchemaVersion = 1
	// GrantSchemaVersion is the definition-grant document version.
	// Version 0 allows only step and run scopes.
	GrantSchemaVersion = 1
	// MaxInlineBytes is the largest artifact or memory body kept beside its
	// record. Larger bodies are stored by object reference.
	MaxInlineBytes = 4096
	// DefaultRetrievalLimit bounds one organization retrieval.
	DefaultRetrievalLimit = 8
	// DefaultRetention is how long a context snapshot keeps fragment text.
	DefaultRetention = 30 * 24 * time.Hour
)

// Scope is one of the four context scopes from the product specification.
type Scope string

const (
	ScopeStep         Scope = "step"
	ScopeRun          Scope = "run"
	ScopeAgentMemory  Scope = "agent_memory"
	ScopeOrganization Scope = "organization"
)

// Valid reports whether s is one of the four known scopes.
func (s Scope) Valid() bool {
	switch s {
	case ScopeStep, ScopeRun, ScopeAgentMemory, ScopeOrganization:
		return true
	default:
		return false
	}
}

// Purpose is the reason a grant allows a read or write.
type Purpose string

const (
	PurposeResolve        Purpose = "resolve"
	PurposeRetrieve       Purpose = "retrieve"
	PurposeReadMemory     Purpose = "read_memory"
	PurposeWriteMemory    Purpose = "write_memory"
	PurposeDeleteMemory   Purpose = "delete_memory"
	PurposeWriteArtifact  Purpose = "write_artifact"
	PurposeDeleteArtifact Purpose = "delete_artifact"
	PurposeInspect        Purpose = "inspect"
)

// Valid reports whether p is a known purpose.
func (p Purpose) Valid() bool {
	switch p {
	case PurposeResolve, PurposeRetrieve, PurposeReadMemory, PurposeWriteMemory,
		PurposeDeleteMemory, PurposeWriteArtifact, PurposeDeleteArtifact, PurposeInspect:
		return true
	default:
		return false
	}
}

const (
	// RoleAdmin may install and revoke grants.
	RoleAdmin = "admin"
	// RoleInspector may read snapshots and provenance.
	RoleInspector = "inspector"
	// RoleOperator may run retention deletion.
	RoleOperator = "operator"
)

// Trust classifies fragment text. Every fragment this package publishes is
// untrusted: the text cannot grant tools, scopes, or further data.
type Trust string

const TrustUntrusted Trust = "untrusted"

const (
	SourceStep      = "step_input"
	SourceArtifact  = "run_artifact"
	SourceMemory    = "agent_memory"
	SourceKnowledge = "knowledge_chunk"
)

// Code is a stable authorization or validation failure.
type Code string

const (
	CodeDenied          Code = "CONTEXT_DENIED"
	CodeGrantRevoked    Code = "GRANT_REVOKED"
	CodeGrantExpired    Code = "GRANT_EXPIRED"
	CodePurposeMismatch Code = "PURPOSE_MISMATCH"
	CodeNotFound        Code = "NOT_FOUND"
	CodeInvalid         Code = "INVALID_INPUT"
	CodeInternal        Code = "INTERNAL"
)

// Error is a context failure whose message never includes stored content.
type Error struct {
	Code     Code    `json:"code"`
	Message  string  `json:"message"`
	Scope    Scope   `json:"scope,omitempty"`
	Resource string  `json:"resource,omitempty"`
	Purpose  Purpose `json:"purpose,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

// Principal is the caller the coordinator presents. This package does not
// authenticate that claim; it enforces grants for the identity it is given.
type Principal struct {
	TenantID string   `json:"tenant_id"`
	ID       string   `json:"id"`
	Roles    []string `json:"roles,omitempty"`
}

// HasRole reports whether the principal carries role.
func (p Principal) HasRole(role string) bool {
	return slices.Contains(p.Roles, role)
}

// Grant is a revocable runtime authorization for one agent, scope, and resource.
type Grant struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	PrincipalID string     `json:"principal_id"`
	AgentID     string     `json:"agent_id"`
	Scope       Scope      `json:"scope"`
	ResourceID  string     `json:"resource_id"`
	Purposes    []Purpose  `json:"purposes"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	RevokedBy   string     `json:"revoked_by,omitempty"`
}

// Active reports whether the grant can authorize a new read or write at now.
func (g Grant) Active(now time.Time) bool {
	if g.RevokedAt != nil {
		return false
	}
	if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
		return false
	}
	return true
}

// DefinitionGrant is the grant pinned on an agent definition version.
// An empty Resource matches any runtime resource of that scope. A set Resource
// pins the definition to one invocation, run, agent, or collection.
type DefinitionGrant struct {
	Scope    Scope     `json:"scope"`
	Resource string    `json:"resource,omitempty"`
	Purposes []Purpose `json:"purposes"`
}

// VersionedGrants is the definition grant document.
// Version 0 is the legacy step/run document. Version 1 adds agent memory and
// organization retrieval.
type VersionedGrants struct {
	Version int               `json:"version"`
	Grants  []DefinitionGrant `json:"grants,omitempty"`
}

// Provenance explains where a fragment came from. Inspectors use it after the
// text has been redacted. It is not a permission.
type Provenance struct {
	Scope         Scope     `json:"scope"`
	ResourceID    string    `json:"resource_id"`
	SourceKind    string    `json:"source_kind"`
	SourceID      string    `json:"source_id"`
	TenantID      string    `json:"tenant_id"`
	GrantID       string    `json:"grant_id"`
	Retriever     string    `json:"retriever,omitempty"`
	ContentHash   string    `json:"content_hash"`
	CollectedAt   time.Time `json:"collected_at"`
	ObjectKey     string    `json:"object_key,omitempty"`
	Checksum      string    `json:"checksum,omitempty"`
	ByteSize      int       `json:"byte_size,omitempty"`
	MediaType     string    `json:"media_type,omitempty"`
	DocumentTitle string    `json:"document_title,omitempty"`
	SourceURI     string    `json:"source_uri,omitempty"`
	ForwardedFrom string    `json:"forwarded_from,omitempty"`
	Selected      bool      `json:"selected,omitempty"`
}

// Fragment is model-facing text plus the provenance an inspector needs.
// Text is untrusted data. Trust is always TrustUntrusted.
type Fragment struct {
	ID         string     `json:"id"`
	Scope      Scope      `json:"scope"`
	ResourceID string     `json:"resource_id"`
	Text       string     `json:"text"`
	Trust      Trust      `json:"trust"`
	Redacted   bool       `json:"redacted,omitempty"`
	Provenance Provenance `json:"provenance"`
}

// Denial records a refused scope without retrieved content or metadata.
// Resource is an identifier the caller already supplied.
type Denial struct {
	Code     Code    `json:"code"`
	Scope    Scope   `json:"scope"`
	Resource string  `json:"resource,omitempty"`
	Purpose  Purpose `json:"purpose,omitempty"`
}

// GrantUse records the grant that authorized a published scope.
type GrantUse struct {
	GrantID    string  `json:"grant_id"`
	Scope      Scope   `json:"scope"`
	ResourceID string  `json:"resource_id"`
	Purpose    Purpose `json:"purpose"`
}

// Bundle is the context for one invocation.
//
// Fragments are the only model-facing payload. AllowedTools is the policy
// allowlist supplied by the coordinator. Denials and GrantsUsed are for the
// runtime and inspectors. None of those audit fields are permissions derived
// from fragment text.
type Bundle struct {
	Version      int        `json:"version"`
	SnapshotID   string     `json:"snapshot_id,omitempty"`
	TenantID     string     `json:"tenant_id"`
	RunID        string     `json:"run_id"`
	InvocationID string     `json:"invocation_id"`
	AgentID      string     `json:"agent_id"`
	Depth        int        `json:"depth"`
	Fragments    []Fragment `json:"fragments,omitempty"`
	AllowedTools []string   `json:"allowed_tools,omitempty"`
	Denials      []Denial   `json:"denials,omitempty"`
	GrantsUsed   []GrantUse `json:"grants_used,omitempty"`
	ResolvedAt   time.Time  `json:"resolved_at"`
}

// ToolAllowed reports whether toolID is on the policy allowlist.
// Fragment text is not consulted.
func (b Bundle) ToolAllowed(toolID string) bool {
	return slices.Contains(b.AllowedTools, toolID)
}

// Snapshot is the retained explanation of one resolve or forward.
type Snapshot struct {
	Version      int    `json:"version"`
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id"`
	RunID        string `json:"run_id"`
	InvocationID string `json:"invocation_id"`
	AgentID      string `json:"agent_id"`
	Bundle       Bundle `json:"bundle"`
	// MemoryRefs lists agent-memory source IDs included at resolve time.
	// Version 0 snapshots leave this empty.
	MemoryRefs  []string   `json:"memory_refs,omitempty"`
	RetainUntil time.Time  `json:"retain_until"`
	RedactedAt  *time.Time `json:"redacted_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Artifact is a run-scoped object reference. The body is not part of the
// reference; readers load it through an authorized resolve.
type Artifact struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	RunID          string     `json:"run_id"`
	InvocationID   string     `json:"invocation_id,omitempty"`
	AgentID        string     `json:"agent_id,omitempty"`
	Name           string     `json:"name,omitempty"`
	MediaType      string     `json:"media_type,omitempty"`
	ObjectKey      string     `json:"object_key,omitempty"`
	Checksum       string     `json:"checksum,omitempty"`
	ByteSize       int        `json:"byte_size"`
	Classification string     `json:"classification,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	RetainUntil    *time.Time `json:"retain_until,omitempty"`
}

// Memory is agent-scoped memory metadata. The body is loaded only by an
// authorized resolve.
type Memory struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	AgentID   string     `json:"agent_id"`
	Topic     string     `json:"topic,omitempty"`
	ObjectKey string     `json:"object_key,omitempty"`
	Checksum  string     `json:"checksum,omitempty"`
	ByteSize  int        `json:"byte_size"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// ContentHash is the SHA-256 hex digest of fragment text.
func ContentHash(text string) string {
	return hashBytes([]byte(text))
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func codeMessage(code Code) string {
	switch code {
	case CodeDenied:
		return "access denied"
	case CodeGrantRevoked:
		return "grant revoked"
	case CodeGrantExpired:
		return "grant expired"
	case CodePurposeMismatch:
		return "purpose not granted"
	case CodeNotFound:
		return "not found"
	case CodeInvalid:
		return "invalid input"
	default:
		return "internal error"
	}
}

func codeError(code Code, scope Scope, resource string, purpose Purpose) error {
	return &Error{Code: code, Message: codeMessage(code), Scope: scope, Resource: resource, Purpose: purpose}
}

func invalid(message string) error {
	return &Error{Code: CodeInvalid, Message: message}
}

func notFound(message string) error {
	return &Error{Code: CodeNotFound, Message: message}
}

func definitionAllows(grants []DefinitionGrant, scope Scope, resource string, purpose Purpose) bool {
	for _, grant := range grants {
		if grant.Scope != scope || !slices.Contains(grant.Purposes, purpose) {
			continue
		}
		if grant.Resource == "" || grant.Resource == resource {
			return true
		}
	}
	return false
}

func validateGrants(set VersionedGrants) error {
	switch set.Version {
	case 0, GrantSchemaVersion:
	default:
		return invalid("unsupported definition grant version")
	}
	for _, grant := range set.Grants {
		if !grant.Scope.Valid() {
			return invalid("definition grant scope is invalid")
		}
		if set.Version < GrantSchemaVersion && (grant.Scope == ScopeAgentMemory || grant.Scope == ScopeOrganization) {
			return invalid("definition grant version does not allow memory or organization scope")
		}
		if len(grant.Purposes) == 0 {
			return invalid("definition grant purpose is required")
		}
		for _, purpose := range grant.Purposes {
			if !purpose.Valid() {
				return invalid("definition grant purpose is invalid")
			}
		}
	}
	return nil
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneTime(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	value := in.UTC()
	return &value
}

func clonePurposes(in []Purpose) []Purpose {
	if in == nil {
		return nil
	}
	out := make([]Purpose, len(in))
	copy(out, in)
	return out
}

func cloneGrant(in Grant) Grant {
	in.Purposes = clonePurposes(in.Purposes)
	in.ExpiresAt = cloneTime(in.ExpiresAt)
	in.RevokedAt = cloneTime(in.RevokedAt)
	return in
}

func cloneFragment(in Fragment) Fragment {
	return in
}

func cloneBundle(in Bundle) Bundle {
	in.AllowedTools = cloneStrings(in.AllowedTools)
	if in.Fragments != nil {
		fragments := make([]Fragment, len(in.Fragments))
		for i := range in.Fragments {
			fragments[i] = cloneFragment(in.Fragments[i])
		}
		in.Fragments = fragments
	}
	if in.Denials != nil {
		denials := make([]Denial, len(in.Denials))
		copy(denials, in.Denials)
		in.Denials = denials
	}
	if in.GrantsUsed != nil {
		uses := make([]GrantUse, len(in.GrantsUsed))
		copy(uses, in.GrantsUsed)
		in.GrantsUsed = uses
	}
	return in
}

func cloneSnapshot(in Snapshot) Snapshot {
	in.Bundle = cloneBundle(in.Bundle)
	if in.MemoryRefs != nil {
		in.MemoryRefs = cloneStrings(in.MemoryRefs)
	}
	in.RedactedAt = cloneTime(in.RedactedAt)
	return in
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, value := range in {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
