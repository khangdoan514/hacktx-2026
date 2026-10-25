package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// StateWaiting is the run hold while a protected write has no decision.
// It is the same value as contracts.StateWaiting. The coordinator applies it
// while ApprovalWaiting is current.
const StateWaiting = string(contracts.StateWaiting)

const DefaultApprovalTTL = 24 * time.Hour

// NMC2 role codes. Authorization reads these from platform.Actor, which
// organization.Load fills from app.role_grants. They are not a second directory.
const (
	RoleRequester        = "requester"
	RoleAIEngineer       = "ai_engineer"
	RoleSecurityReviewer = "security_reviewer"
	RoleReleaseApprover  = "release_approver"
	RoleOperator         = "operator"
	RoleAdministrator    = "administrator"
	RoleAuditor          = "auditor"
)

const (
	SideEffectReadOnly = "READ_ONLY"
	SideEffectWrite    = "WRITE"
)

type Kind string

const (
	KindUser    Kind = "user"
	KindService Kind = "service"
)

type ApprovalState string

const (
	ApprovalWaiting  ApprovalState = "WAITING"
	ApprovalApproved ApprovalState = "APPROVED"
	ApprovalDenied   ApprovalState = "DENIED"
	ApprovalExpired  ApprovalState = "EXPIRED"
)

type WriteState string

const (
	WriteWaiting    WriteState = "WAITING"
	WriteReady      WriteState = "READY"
	WriteDispatched WriteState = "DISPATCHED"
	WriteSucceeded  WriteState = "SUCCEEDED"
	WriteFailed     WriteState = "FAILED"
	WriteUncertain  WriteState = "UNCERTAIN"
	WriteReconciled WriteState = "RECONCILED"
	WriteDenied     WriteState = "DENIED"
	WriteExpired    WriteState = "EXPIRED"
)

const (
	OutcomeApproved  = "APPROVED"
	OutcomeDenied    = "DENIED"
	OutcomeSucceeded = "SUCCEEDED"
	OutcomeFailed    = "FAILED"
)

const (
	CodeDenied               = "DENIED"
	CodeCrossTenant          = "CROSS_TENANT"
	CodeToolNotAllowed       = "TOOL_NOT_ALLOWED"
	CodeDelegateNotAllowed   = "DELEGATE_NOT_ALLOWED"
	CodeModelNotAllowed      = "MODEL_NOT_ALLOWED"
	CodeBudgetExceeded       = "BUDGET_EXCEEDED"
	CodeSelfApproval         = "SELF_APPROVAL"
	CodeAlreadyDecided       = "ALREADY_DECIDED"
	CodeExpired              = "EXPIRED"
	CodeUnauthorizedApprover = "UNAUTHORIZED_APPROVER"
	CodeNotDispatchable      = "NOT_DISPATCHABLE"
	CodeDuplicateWrite       = "DUPLICATE_WRITE"
	CodeUncertain            = "UNCERTAIN_WRITE"
	CodeNotFound             = "NOT_FOUND"
	CodeInvalidInput         = "INVALID_INPUT"
	CodeConflict             = "CONFLICT"
	CodeInternal             = "INTERNAL"
)

const (
	auditAllow = "allow"
	auditDeny  = "deny"
)

// Error is a policy decision the caller can show without secret material.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func errCode(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Is reports whether err is a policy Error with the given code.
func Is(err error, code string) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

// Principal is the caller for one policy check. UserID and TenantID come from
// the authenticated employee or from an already-authenticated service identity.
// Email and display fields are not authorization claims.
type Principal struct {
	UserID   string
	Email    string
	TenantID string
	Roles    []string
	Kind     Kind
}

// Budget is a ceiling. Zero means that dimension was not requested or not capped.
type Budget struct {
	MaxTokens     int
	MaxCostUSD    float64
	MaxModelTurns int
}

// Policy is one immutable revision for a tenant. A run pins ID and Revision.
type Policy struct {
	ID                   string
	TenantID             string
	Revision             int
	AllowedModels        []string
	AllowedTools         []string
	AllowedDelegates     []string
	ProtectedTools       []string
	ApproverRoles        []string
	MaxTokens            int
	MaxCostUSD           float64
	MaxModelTurns        int
	ApprovalTTL          time.Duration
	EscalationRole       string
	AllowServiceApproval bool
	CreatedBy            string
	CreatedAt            time.Time
}

// Definition is the tenant binding and allowlists the policy layer was given.
// The definition registry remains the source of the published agent text.
type Definition struct {
	TenantID         string
	AgentID          string
	Version          string
	AllowedTools     []string
	AllowedDelegates []string
}

// RunRecord is the policy pin for one run. It is not the run snapshot store.
type RunRecord struct {
	ID             string
	TenantID       string
	RequesterID    string
	AgentID        string
	Version        string
	ModelID        string
	PolicyID       string
	PolicyRevision int
	IdempotencyKey string
	State          string
	ResumeState    string
	Hold           string
	Budget         Budget
	CreatedAt      time.Time
}

// SecretRef names a credential. The secret value is resolved just in time and
// is never stored on this struct.
type SecretRef struct {
	TenantID string
	Name     string
	RunID    string
}

// ContextRecord is scoped run context. Summary is returned only after a tenant
// and inspection check.
type ContextRecord struct {
	ID       string
	TenantID string
	RunID    string
	Summary  string
}

// EventRecord is a safe inspection event. Sequence order belongs to the run store.
type EventRecord struct {
	RunID    string
	Sequence uint64
	Type     string
	Data     string
}

// StartRequest asks to pin a run to a policy revision and an agent version.
type StartRequest struct {
	RunID          string
	IdempotencyKey string
	AgentID        string
	Version        string
	ModelID        string
	PolicyID       string
	Budget         Budget
}

// ApprovalRequest is one protected write waiting for a single human decision.
type ApprovalRequest struct {
	ID             string
	TenantID       string
	RunID          string
	InvocationID   string
	ToolID         string
	ToolVersion    string
	IdempotencyKey string
	PolicyID       string
	PolicyRevision int
	RequesterID    string
	State          ApprovalState
	InputDigest    string
	ExpiresAt      time.Time
	CreatedAt      time.Time
}

// ApprovalDecision is the one recorded outcome for an approval request.
type ApprovalDecision struct {
	ID          string
	RequestID   string
	TenantID    string
	ApproverID  string
	RequesterID string
	Outcome     string
	DecidedAt   time.Time
}

// WriteAttempt is one logical protected write. IdempotencyKey is stable across
// the dispatch and any timeout so a retry cannot create a second attempt.
type WriteAttempt struct {
	ID             string
	TenantID       string
	IdempotencyKey string
	RunID          string
	InvocationID   string
	ToolID         string
	ToolVersion    string
	ApprovalID     string
	State          WriteState
	DispatchCount  int
	DispatchedAt   *time.Time
	Outcome        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ToolDecision is the allowlist result. Permitted does not dispatch a write.
// Untrusted model or retrieval text cannot change Permitted or Protected.
type ToolDecision struct {
	Permitted bool
	Protected bool
	Code      string
	Message   string
}

// WriteRequest is a gateway proposal. UntrustedInput is recorded only as
// "present" and cannot grant a tool, change its side effect, or skip approval.
type WriteRequest struct {
	RunID          string
	InvocationID   string
	ToolID         string
	ToolVersion    string
	SideEffect     string
	InputDigest    string
	IdempotencyKey string
	UntrustedInput string
}

// BeginResult is the approval created for a protected write, or the existing
// attempt when the same idempotency key is presented again.
type BeginResult struct {
	Attempt  WriteAttempt
	Approval ApprovalRequest
	Created  bool
}

// DispatchResult grants the tool call only when Granted is true.
// A false grant is not a retryable failure of the external write.
type DispatchResult struct {
	Attempt WriteAttempt
	Granted bool
}

// AuditEvent is a redacted decision record. Detail must not contain secret
// values, prompts, or untrusted tool text.
type AuditEvent struct {
	ID           string
	TenantID     string
	ActorID      string
	Action       string
	ResourceKind string
	ResourceID   string
	Outcome      string
	Code         string
	Detail       string
	At           time.Time
}

// SecretResolver returns secret material after the engine has authorized the
// reference. Implementations must not log the value.
type SecretResolver interface {
	Resolve(ctx context.Context, ref SecretRef) (string, error)
}

// IdempotencyKey is the stable key for one logical protected write.
// A timeout retry must pass the same parts. A new action digest is a new write.
func IdempotencyKey(tenantID, runID, invocationID, toolID, actionDigest string) (string, error) {
	if !validID(tenantID) || !validID(runID) || !validID(toolID) || strings.TrimSpace(actionDigest) == "" {
		return "", errCode(CodeInvalidInput, "idempotency key parts are required")
	}
	if invocationID != "" && !validID(invocationID) {
		return "", errCode(CodeInvalidInput, "invocation id is invalid")
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{tenantID, runID, invocationID, toolID, actionDigest}, "\x00")))
	return hex.EncodeToString(sum[:]), nil
}

// Redact replaces known secret material with a placeholder.
func Redact(detail string, secrets ...string) string {
	out := detail
	for _, secret := range secrets {
		if len(secret) < 6 {
			continue
		}
		out = strings.ReplaceAll(out, secret, "[redacted]")
	}
	return out
}

// RedactJSON replaces values of sensitive object keys. Non-JSON text is returned unchanged.
func RedactJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	encoded, err := json.Marshal(redactValue(value))
	if err != nil {
		return raw
	}
	return string(encoded)
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if sensitiveKey(key) {
				out[key] = "[redacted]"
				continue
			}
			out[key] = redactValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactValue(item)
		}
		return out
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
	case "secret", "secrets", "token", "password", "authorization", "api_key", "apikey", "access_token", "refresh_token", "credential":
		return true
	default:
		return false
	}
}

func validID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validKey(key string) bool {
	if key == "" || len(key) > 256 {
		return false
	}
	for _, r := range key {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func contains(list []string, needle string) bool {
	for _, item := range list {
		if item == needle {
			return true
		}
	}
	return false
}

func cloneStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}
