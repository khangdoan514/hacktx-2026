package policy

import (
	"context"
	"time"
)

// Store persists policy revisions, resource bindings, approvals, and write
// attempts. Authorization decisions belong on Engine. The memory implementation
// is the tested store; the SQL migration is the durable constraint backstop.
type Store interface {
	InsertPolicy(ctx context.Context, policy Policy) error
	Policy(ctx context.Context, tenantID, id string, revision int) (Policy, error)
	LatestPolicy(ctx context.Context, tenantID, id string) (Policy, error)

	InsertDefinition(ctx context.Context, def Definition) error
	Definition(ctx context.Context, tenantID, agentID, version string) (Definition, error)
	DefinitionInOtherTenant(ctx context.Context, agentID, version, notTenant string) (ok bool, err error)

	InsertRun(ctx context.Context, run RunRecord) (stored RunRecord, created bool, err error)
	Run(ctx context.Context, id string) (RunRecord, error)

	InsertSecret(ctx context.Context, ref SecretRef) error
	Secret(ctx context.Context, tenantID, name string) (SecretRef, error)
	SecretInOtherTenant(ctx context.Context, name, notTenant string) (ok bool, err error)

	InsertContext(ctx context.Context, rec ContextRecord) error
	Context(ctx context.Context, id string) (ContextRecord, error)

	InsertEvents(ctx context.Context, runID string, events []EventRecord) error
	Events(ctx context.Context, runID string) ([]EventRecord, error)

	CreateProtected(ctx context.Context, approval ApprovalRequest, write WriteAttempt, now time.Time) (ApprovalRequest, WriteAttempt, bool, error)
	Approval(ctx context.Context, id string) (ApprovalRequest, error)
	ExpireApproval(ctx context.Context, id string, now time.Time) (ApprovalRequest, error)
	ListInbox(ctx context.Context, tenantID string, now time.Time) ([]ApprovalRequest, error)

	Write(ctx context.Context, tenantID, idempotencyKey string) (WriteAttempt, error)
	CommitDecision(ctx context.Context, decision ApprovalDecision, now time.Time) (ApprovalDecision, error)
	Dispatch(ctx context.Context, tenantID, idempotencyKey string, now time.Time) (WriteAttempt, bool, error)
	Timeout(ctx context.Context, tenantID, idempotencyKey string, now time.Time) (WriteAttempt, error)
	Result(ctx context.Context, tenantID, idempotencyKey, outcome string, now time.Time) (WriteAttempt, error)
	Reconcile(ctx context.Context, tenantID, idempotencyKey, outcome string, now time.Time) (WriteAttempt, error)

	AddAudit(ctx context.Context, event AuditEvent) error
	Audit(ctx context.Context, tenantID, resourceID string) ([]AuditEvent, error)
}
