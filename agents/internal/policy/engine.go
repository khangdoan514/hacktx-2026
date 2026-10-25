package policy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Engine checks tenant, allowlist, budget, approval, and protected-write rules.
// It does not call tools or models. A Granted dispatch is the only signal that
// the gateway may perform the external write once.
type Engine struct {
	store   Store
	now     func() time.Time
	ids     func() (string, error)
	secrets SecretResolver
}

// Option configures an Engine.
type Option func(*Engine)

// WithClock overrides the clock. Tests use it for expiry.
func WithClock(now func() time.Time) Option {
	return func(e *Engine) { e.now = now }
}

// WithIDs overrides identifier generation.
func WithIDs(ids func() (string, error)) Option {
	return func(e *Engine) { e.ids = ids }
}

// WithSecrets sets the just-in-time resolver. ResolveSecret fails closed when
// this is unset.
func WithSecrets(secrets SecretResolver) Option {
	return func(e *Engine) { e.secrets = secrets }
}

// New returns an engine over store.
func New(store Store, opts ...Option) *Engine {
	engine := &Engine{store: store, now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(engine)
	}
	return engine
}

func (e *Engine) ready(ctx context.Context, principal Principal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e == nil || e.store == nil {
		return errCode(CodeInternal, "policy engine is not configured")
	}
	return principal.validate()
}

func (e *Engine) nowUTC() time.Time {
	if e.now == nil {
		return time.Now().UTC()
	}
	return e.now().UTC()
}

func (e *Engine) newID() (string, error) {
	if e.ids != nil {
		id, err := e.ids()
		if err != nil {
			return "", err
		}
		if !validID(id) && !validKey(id) {
			return "", errCode(CodeInternal, "id generator returned an empty id")
		}
		return id, nil
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func (e *Engine) record(ctx context.Context, principal Principal, action, kind, id, outcome, code, detail string) {
	eventID, err := e.newID()
	if err != nil {
		return
	}
	_ = e.store.AddAudit(ctx, AuditEvent{
		ID:           eventID,
		TenantID:     principal.TenantID,
		ActorID:      principal.UserID,
		Action:       action,
		ResourceKind: kind,
		ResourceID:   id,
		Outcome:      outcome,
		Code:         code,
		Detail:       detail,
		At:           e.nowUTC(),
	})
}

// PublishPolicy stores the next immutable revision. Only a human administrator
// of the tenant can publish. The caller's tenant, author, and revision win
// over values on the submitted policy.
func (e *Engine) PublishPolicy(ctx context.Context, principal Principal, policy Policy) (Policy, error) {
	if err := e.ready(ctx, principal); err != nil {
		return Policy{}, err
	}
	if principal.Kind != KindUser || !principal.Has(RoleAdministrator) {
		e.record(ctx, principal, "publish_policy", "policy", policy.ID, auditDeny, CodeDenied, "principal cannot publish policy")
		return Policy{}, errCode(CodeDenied, "principal cannot publish policy")
	}
	if !validID(policy.ID) {
		return Policy{}, errCode(CodeInvalidInput, "policy id is required")
	}
	if err := validatePolicy(policy); err != nil {
		return Policy{}, err
	}
	policy.TenantID = principal.TenantID
	policy.CreatedBy = principal.UserID
	for attempt := 0; attempt < 5; attempt++ {
		next := 1
		latest, err := e.store.LatestPolicy(ctx, principal.TenantID, policy.ID)
		if err == nil {
			next = latest.Revision + 1
		} else if !Is(err, CodeNotFound) {
			return Policy{}, err
		}
		policy.Revision = next
		policy.CreatedAt = e.nowUTC()
		if err := e.store.InsertPolicy(ctx, policy); err != nil {
			if Is(err, CodeConflict) {
				continue
			}
			return Policy{}, err
		}
		e.record(ctx, principal, "publish_policy", "policy", policy.ID, auditAllow, "", "policy revision published")
		return policy, nil
	}
	return Policy{}, errCode(CodeConflict, "policy revision could not be published")
}

// Policy returns one revision from the caller's tenant.
func (e *Engine) Policy(ctx context.Context, principal Principal, id string, revision int) (Policy, error) {
	if err := e.ready(ctx, principal); err != nil {
		return Policy{}, err
	}
	if len(principal.Roles) == 0 {
		e.record(ctx, principal, "read_policy", "policy", id, auditDeny, CodeDenied, "principal cannot read policy")
		return Policy{}, errCode(CodeDenied, "principal cannot read policy")
	}
	policy, err := e.store.Policy(ctx, principal.TenantID, id, revision)
	if err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// PutDefinition registers the allowlists for an agent version in the caller's tenant.
func (e *Engine) PutDefinition(ctx context.Context, principal Principal, def Definition) error {
	if err := e.ready(ctx, principal); err != nil {
		return err
	}
	if principal.Kind != KindUser || !principal.Has(RoleAdministrator) {
		e.record(ctx, principal, "put_definition", "definition", def.AgentID, auditDeny, CodeDenied, "principal cannot register a definition")
		return errCode(CodeDenied, "principal cannot register a definition")
	}
	if def.TenantID != "" && def.TenantID != principal.TenantID {
		e.record(ctx, principal, "put_definition", "definition", def.AgentID, auditDeny, CodeCrossTenant, "cross-tenant definition write")
		return errCode(CodeCrossTenant, "cannot register a definition for another tenant")
	}
	def.TenantID = principal.TenantID
	if !validID(def.AgentID) || !validID(def.Version) || !validList(def.AllowedTools) || !validList(def.AllowedDelegates) {
		return errCode(CodeInvalidInput, "definition id, version, and allowlists must be valid ids")
	}
	if err := e.store.InsertDefinition(ctx, def); err != nil {
		return err
	}
	e.record(ctx, principal, "put_definition", "definition", def.AgentID, auditAllow, "", "definition registered")
	return nil
}

// PutSecretRef registers a secret name. It does not accept a secret value.
func (e *Engine) PutSecretRef(ctx context.Context, principal Principal, ref SecretRef) error {
	if err := e.ready(ctx, principal); err != nil {
		return err
	}
	if principal.Kind != KindUser || !principal.Has(RoleAdministrator) {
		e.record(ctx, principal, "put_secret_ref", "secret", ref.Name, auditDeny, CodeDenied, "principal cannot register a secret reference")
		return errCode(CodeDenied, "principal cannot register a secret reference")
	}
	if ref.TenantID != "" && ref.TenantID != principal.TenantID {
		e.record(ctx, principal, "put_secret_ref", "secret", ref.Name, auditDeny, CodeCrossTenant, "cross-tenant secret reference write")
		return errCode(CodeCrossTenant, "cannot register a secret reference for another tenant")
	}
	ref.TenantID = principal.TenantID
	if !validID(ref.Name) || (ref.RunID != "" && !validID(ref.RunID)) {
		return errCode(CodeInvalidInput, "secret reference name is required")
	}
	if err := e.store.InsertSecret(ctx, ref); err != nil {
		return err
	}
	e.record(ctx, principal, "put_secret_ref", "secret", ref.Name, auditAllow, "", "secret reference registered")
	return nil
}

// PutContext stores a context summary for a run in the caller's tenant.
func (e *Engine) PutContext(ctx context.Context, principal Principal, rec ContextRecord) error {
	if err := e.ready(ctx, principal); err != nil {
		return err
	}
	if rec.TenantID != "" && rec.TenantID != principal.TenantID {
		e.record(ctx, principal, "put_context", "context", rec.ID, auditDeny, CodeCrossTenant, "cross-tenant context write")
		return errCode(CodeCrossTenant, "cannot write context for another tenant")
	}
	run, err := e.visibleRun(ctx, principal, rec.RunID, "put_context", "context")
	if err != nil {
		return err
	}
	if !principal.canStart() && principal.UserID != run.RequesterID {
		e.record(ctx, principal, "put_context", "context", rec.ID, auditDeny, CodeDenied, "principal cannot write context")
		return errCode(CodeDenied, "principal cannot write context")
	}
	if !validID(rec.ID) {
		return errCode(CodeInvalidInput, "context id is required")
	}
	rec.TenantID = principal.TenantID
	rec.Summary = RedactJSON(rec.Summary)
	if err := e.store.InsertContext(ctx, rec); err != nil {
		return err
	}
	e.record(ctx, principal, "put_context", "context", rec.ID, auditAllow, "", "context stored")
	return nil
}

// PutEvents appends safe inspection events for a run in the caller's tenant.
func (e *Engine) PutEvents(ctx context.Context, principal Principal, runID string, events []EventRecord) error {
	if err := e.ready(ctx, principal); err != nil {
		return err
	}
	run, err := e.visibleRun(ctx, principal, runID, "put_events", "event")
	if err != nil {
		return err
	}
	if !principal.canStart() && principal.UserID != run.RequesterID {
		e.record(ctx, principal, "put_events", "event", runID, auditDeny, CodeDenied, "principal cannot write events")
		return errCode(CodeDenied, "principal cannot write events")
	}
	stored := make([]EventRecord, len(events))
	for i, event := range events {
		event.RunID = runID
		event.Data = RedactJSON(event.Data)
		stored[i] = event
	}
	if err := e.store.InsertEvents(ctx, runID, stored); err != nil {
		return err
	}
	e.record(ctx, principal, "put_events", "event", runID, auditAllow, "", "events stored")
	return nil
}

// StartRun pins a run to the latest policy revision when the caller, model,
// budget, and definition tenant all pass. The same idempotency key returns
// the original pin.
func (e *Engine) StartRun(ctx context.Context, principal Principal, req StartRequest) (RunRecord, error) {
	if err := e.ready(ctx, principal); err != nil {
		return RunRecord{}, err
	}
	if !principal.canStart() {
		e.record(ctx, principal, "start_run", "run", req.RunID, auditDeny, CodeDenied, "principal cannot start runs")
		return RunRecord{}, errCode(CodeDenied, "principal cannot start runs")
	}
	if !validID(req.RunID) || !validID(req.AgentID) || !validID(req.Version) || !validID(req.PolicyID) || !validKey(req.IdempotencyKey) {
		return RunRecord{}, errCode(CodeInvalidInput, "run, agent, policy, and idempotency key are required")
	}
	if req.ModelID == "" || !contracts.ValidModelID(req.ModelID) {
		return RunRecord{}, errCode(CodeInvalidInput, "model id is invalid")
	}
	if err := finiteBudget(req.Budget); err != nil {
		return RunRecord{}, err
	}
	policy, err := e.store.LatestPolicy(ctx, principal.TenantID, req.PolicyID)
	if err != nil {
		if Is(err, CodeNotFound) {
			e.record(ctx, principal, "start_run", "run", req.RunID, auditDeny, CodeNotFound, "policy not found")
		}
		return RunRecord{}, err
	}
	if !contains(policy.AllowedModels, req.ModelID) {
		e.record(ctx, principal, "start_run", "run", req.RunID, auditDeny, CodeModelNotAllowed, "model is not on the policy allowlist")
		return RunRecord{}, errCode(CodeModelNotAllowed, "model is not on the policy allowlist")
	}
	if err := withinBudget(policy, req.Budget); err != nil {
		e.record(ctx, principal, "start_run", "run", req.RunID, auditDeny, codeOf(err), "budget exceeds policy")
		return RunRecord{}, err
	}
	if _, err := e.lookupDefinition(ctx, principal, req.AgentID, req.Version, "start_run"); err != nil {
		return RunRecord{}, err
	}
	run := RunRecord{
		ID:             req.RunID,
		TenantID:       principal.TenantID,
		RequesterID:    principal.UserID,
		AgentID:        req.AgentID,
		Version:        req.Version,
		ModelID:        req.ModelID,
		PolicyID:       policy.ID,
		PolicyRevision: policy.Revision,
		IdempotencyKey: req.IdempotencyKey,
		State:          "PENDING",
		ResumeState:    "PENDING",
		Budget:         req.Budget,
		CreatedAt:      e.nowUTC(),
	}
	stored, _, err := e.store.InsertRun(ctx, run)
	if err != nil {
		e.record(ctx, principal, "start_run", "run", req.RunID, auditDeny, codeOf(err), "run was not started")
		return RunRecord{}, err
	}
	e.record(ctx, principal, "start_run", "run", stored.ID, auditAllow, "", "run started")
	return stored, nil
}

// CheckBudget denies a projected cumulative budget that passes the pinned ceiling.
func (e *Engine) CheckBudget(ctx context.Context, principal Principal, runID string, projected Budget) error {
	if err := e.ready(ctx, principal); err != nil {
		return err
	}
	if err := finiteBudget(projected); err != nil {
		return err
	}
	run, err := e.visibleRun(ctx, principal, runID, "check_budget", "run")
	if err != nil {
		return err
	}
	policy, err := e.store.Policy(ctx, run.TenantID, run.PolicyID, run.PolicyRevision)
	if err != nil {
		return err
	}
	if err := withinBudget(policy, projected); err != nil {
		e.record(ctx, principal, "check_budget", "run", runID, auditDeny, CodeBudgetExceeded, "projected budget exceeds pinned policy")
		return err
	}
	e.record(ctx, principal, "check_budget", "run", runID, auditAllow, "", "budget within pinned policy")
	return nil
}

// ReadDefinition returns a definition in the caller's tenant.
func (e *Engine) ReadDefinition(ctx context.Context, principal Principal, agentID, version string) (Definition, error) {
	if err := e.ready(ctx, principal); err != nil {
		return Definition{}, err
	}
	if len(principal.Roles) == 0 {
		e.record(ctx, principal, "read_definition", "definition", agentID, auditDeny, CodeDenied, "principal cannot read definitions")
		return Definition{}, errCode(CodeDenied, "principal cannot read definitions")
	}
	def, err := e.lookupDefinition(ctx, principal, agentID, version, "read_definition")
	if err != nil {
		return Definition{}, err
	}
	e.record(ctx, principal, "read_definition", "definition", agentID, auditAllow, "", "definition read")
	return def, nil
}

// ReadRun returns a run when the caller may inspect it.
func (e *Engine) ReadRun(ctx context.Context, principal Principal, runID string) (RunRecord, error) {
	if err := e.ready(ctx, principal); err != nil {
		return RunRecord{}, err
	}
	run, err := e.store.Run(ctx, runID)
	if err != nil {
		return RunRecord{}, err
	}
	if run.TenantID != principal.TenantID {
		e.record(ctx, principal, "inspect", "run", runID, auditDeny, CodeCrossTenant, "cross-tenant run read")
		return RunRecord{}, errCode(CodeCrossTenant, "cannot read a run for another tenant")
	}
	if !principal.canInspect(run) {
		e.record(ctx, principal, "inspect", "run", runID, auditDeny, CodeDenied, "principal cannot inspect this run")
		return RunRecord{}, errCode(CodeDenied, "principal cannot inspect this run")
	}
	e.record(ctx, principal, "inspect", "run", runID, auditAllow, "", "run read")
	return run, nil
}

// ReadEvents returns events only after the run's tenant and inspection checks.
func (e *Engine) ReadEvents(ctx context.Context, principal Principal, runID string) ([]EventRecord, error) {
	if err := e.ready(ctx, principal); err != nil {
		return nil, err
	}
	if _, err := e.readableRun(ctx, principal, runID, "inspect", "event"); err != nil {
		return nil, err
	}
	events, err := e.store.Events(ctx, runID)
	if err != nil {
		return nil, err
	}
	e.record(ctx, principal, "inspect", "event", runID, auditAllow, "", "events read")
	return events, nil
}

// ReadContext returns one context record after tenant and inspection checks.
func (e *Engine) ReadContext(ctx context.Context, principal Principal, contextID string) (ContextRecord, error) {
	if err := e.ready(ctx, principal); err != nil {
		return ContextRecord{}, err
	}
	rec, err := e.store.Context(ctx, contextID)
	if err != nil {
		return ContextRecord{}, err
	}
	if rec.TenantID != principal.TenantID {
		e.record(ctx, principal, "inspect", "context", contextID, auditDeny, CodeCrossTenant, "cross-tenant context read")
		return ContextRecord{}, errCode(CodeCrossTenant, "cannot read context for another tenant")
	}
	if _, err := e.readableRun(ctx, principal, rec.RunID, "inspect", "context"); err != nil {
		return ContextRecord{}, err
	}
	e.record(ctx, principal, "inspect", "context", contextID, auditAllow, "", "context read")
	return rec, nil
}

// ResolveSecret returns the secret value for a reference in the caller's tenant.
// Auditors can inspect runs and cannot resolve secrets. The value is not audited.
func (e *Engine) ResolveSecret(ctx context.Context, principal Principal, name string) (string, error) {
	if err := e.ready(ctx, principal); err != nil {
		return "", err
	}
	ref, err := e.store.Secret(ctx, principal.TenantID, name)
	if err != nil {
		if Is(err, CodeNotFound) {
			other, otherErr := e.store.SecretInOtherTenant(ctx, name, principal.TenantID)
			if otherErr != nil {
				return "", otherErr
			}
			if other {
				e.record(ctx, principal, "resolve_secret", "secret", name, auditDeny, CodeCrossTenant, "cross-tenant secret read")
				return "", errCode(CodeCrossTenant, "cannot read a secret for another tenant")
			}
		}
		return "", err
	}
	if !principal.canStart() {
		e.record(ctx, principal, "resolve_secret", "secret", name, auditDeny, CodeDenied, "principal cannot resolve secrets")
		return "", errCode(CodeDenied, "principal cannot resolve secrets")
	}
	if ref.RunID != "" {
		if _, err := e.readableRun(ctx, principal, ref.RunID, "resolve_secret", "secret"); err != nil {
			return "", err
		}
	}
	if e.secrets == nil {
		return "", errCode(CodeInternal, "secret resolver is not configured")
	}
	value, err := e.secrets.Resolve(ctx, ref)
	if err != nil {
		e.record(ctx, principal, "resolve_secret", "secret", name, auditDeny, CodeInternal, "secret could not be resolved")
		return "", errCode(CodeInternal, "secret could not be resolved")
	}
	e.record(ctx, principal, "resolve_secret", "secret", name, auditAllow, "", Redact("secret ref resolved", value))
	return value, nil
}

// AuthorizeDelegate allows a delegate only when the pinned policy and the
// caller definition both list it and the target belongs to the same tenant.
// untrusted is model or retrieval text and cannot add a delegate.
func (e *Engine) AuthorizeDelegate(ctx context.Context, principal Principal, runID, targetID, targetVersion, untrusted string) error {
	if err := e.ready(ctx, principal); err != nil {
		return err
	}
	run, err := e.actingRun(ctx, principal, runID, "delegate", "delegate")
	if err != nil {
		return err
	}
	def, err := e.store.Definition(ctx, run.TenantID, run.AgentID, run.Version)
	if err != nil {
		return err
	}
	policy, err := e.store.Policy(ctx, run.TenantID, run.PolicyID, run.PolicyRevision)
	if err != nil {
		return err
	}
	detail := "delegate is not on the allowlist"
	if untrusted != "" {
		detail = "untrusted input ignored"
	}
	if !validID(targetID) || !validID(targetVersion) || !delegatePermitted(policy, def, targetID) {
		e.record(ctx, principal, "delegate", "delegate", targetID, auditDeny, CodeDelegateNotAllowed, detail)
		return errCode(CodeDelegateNotAllowed, "delegate is not on the allowlist")
	}
	if _, err := e.lookupDefinition(ctx, principal, targetID, targetVersion, "delegate"); err != nil {
		return err
	}
	e.record(ctx, principal, "delegate", "delegate", targetID, auditAllow, "", "delegate permitted")
	return nil
}

// CheckTool reports whether a tool is on the pinned allowlists and whether it
// is a protected write. untrusted cannot grant the tool or clear the write class.
func (e *Engine) CheckTool(ctx context.Context, principal Principal, runID, toolID, sideEffect, untrusted string) (ToolDecision, error) {
	if err := e.ready(ctx, principal); err != nil {
		return ToolDecision{}, err
	}
	decision, _, _, err := e.evaluateTool(ctx, principal, runID, toolID, sideEffect, untrusted)
	return decision, err
}

// BeginProtectedWrite opens a WAITING approval, or returns the existing attempt
// for the same idempotency key. A read-only tool and a disallowed tool do not
// create an attempt.
func (e *Engine) BeginProtectedWrite(ctx context.Context, principal Principal, req WriteRequest) (BeginResult, error) {
	if err := e.ready(ctx, principal); err != nil {
		return BeginResult{}, err
	}
	decision, run, policy, err := e.evaluateTool(ctx, principal, req.RunID, req.ToolID, req.SideEffect, req.UntrustedInput)
	if err != nil {
		return BeginResult{}, err
	}
	if !decision.Protected {
		return BeginResult{}, errCode(CodeInvalidInput, "tool is not a protected write")
	}
	if !validKey(req.IdempotencyKey) || !validID(req.ToolVersion) || strings.TrimSpace(req.InputDigest) == "" {
		return BeginResult{}, errCode(CodeInvalidInput, "idempotency key, tool version, and input digest are required")
	}
	if req.InvocationID != "" && !validID(req.InvocationID) {
		return BeginResult{}, errCode(CodeInvalidInput, "invocation id is invalid")
	}
	approvalID, err := e.newID()
	if err != nil {
		return BeginResult{}, err
	}
	writeID, err := e.newID()
	if err != nil {
		return BeginResult{}, err
	}
	now := e.nowUTC()
	approval := ApprovalRequest{
		ID:             approvalID,
		TenantID:       run.TenantID,
		RunID:          run.ID,
		InvocationID:   req.InvocationID,
		ToolID:         req.ToolID,
		ToolVersion:    req.ToolVersion,
		IdempotencyKey: req.IdempotencyKey,
		PolicyID:       policy.ID,
		PolicyRevision: policy.Revision,
		RequesterID:    run.RequesterID,
		State:          ApprovalWaiting,
		InputDigest:    req.InputDigest,
		ExpiresAt:      now.Add(policy.approvalTTL()),
		CreatedAt:      now,
	}
	write := WriteAttempt{
		ID:             writeID,
		TenantID:       run.TenantID,
		IdempotencyKey: req.IdempotencyKey,
		RunID:          run.ID,
		InvocationID:   req.InvocationID,
		ToolID:         req.ToolID,
		ToolVersion:    req.ToolVersion,
		State:          WriteWaiting,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	storedApproval, storedWrite, created, err := e.store.CreateProtected(ctx, approval, write, now)
	if err != nil {
		return BeginResult{}, err
	}
	if storedApproval.State == ApprovalExpired {
		e.auditExpired(ctx, principal, policy, storedApproval.ID)
	}
	return BeginResult{Attempt: storedWrite, Approval: storedApproval, Created: created}, nil
}

// Decide records the single decision for a WAITING request. The requester
// cannot decide it. A service cannot decide it unless the pinned policy allows
// service approval. Expired, denied, and unauthorized decisions do not dispatch.
func (e *Engine) Decide(ctx context.Context, principal Principal, requestID, outcome string) (ApprovalDecision, error) {
	if err := e.ready(ctx, principal); err != nil {
		return ApprovalDecision{}, err
	}
	if !validID(requestID) {
		return ApprovalDecision{}, errCode(CodeInvalidInput, "approval id is required")
	}
	approval, err := e.store.Approval(ctx, requestID)
	if err != nil {
		return ApprovalDecision{}, err
	}
	if approval.TenantID != principal.TenantID {
		e.record(ctx, principal, "decide", "approval", requestID, auditDeny, CodeCrossTenant, "cross-tenant approval decision")
		return ApprovalDecision{}, errCode(CodeCrossTenant, "cannot decide an approval in another tenant")
	}
	policy, err := e.store.Policy(ctx, approval.TenantID, approval.PolicyID, approval.PolicyRevision)
	if err != nil {
		return ApprovalDecision{}, err
	}
	if !principal.canApprove(policy) {
		e.record(ctx, principal, "decide", "approval", requestID, auditDeny, CodeUnauthorizedApprover, "approver is not authorized for this policy")
		return ApprovalDecision{}, errCode(CodeUnauthorizedApprover, "approver is not authorized for this policy")
	}
	if principal.UserID == approval.RequesterID {
		e.record(ctx, principal, "decide", "approval", requestID, auditDeny, CodeSelfApproval, "requester cannot approve their own protected write")
		return ApprovalDecision{}, errCode(CodeSelfApproval, "you cannot approve your own protected write")
	}
	decisionID, err := e.newID()
	if err != nil {
		return ApprovalDecision{}, err
	}
	stored, err := e.store.CommitDecision(ctx, ApprovalDecision{
		ID:         decisionID,
		RequestID:  requestID,
		ApproverID: principal.UserID,
		Outcome:    outcome,
	}, e.nowUTC())
	if err != nil {
		if Is(err, CodeExpired) {
			e.auditExpired(ctx, principal, policy, requestID)
		} else {
			e.record(ctx, principal, "decide", "approval", requestID, auditDeny, codeOf(err), "approval decision rejected")
		}
		return ApprovalDecision{}, err
	}
	e.record(ctx, principal, "decide", "approval", requestID, auditAllow, stored.Outcome, "approval decision recorded")
	return stored, nil
}

// Dispatch grants the external write at most once, and only from READY.
func (e *Engine) Dispatch(ctx context.Context, principal Principal, idempotencyKey string) (DispatchResult, error) {
	attempt, err := e.prepareWrite(ctx, principal, idempotencyKey, "dispatch")
	if err != nil {
		return DispatchResult{}, err
	}
	if !principal.canStart() {
		e.record(ctx, principal, "dispatch", "write", attempt.ID, auditDeny, CodeDenied, "principal cannot dispatch a protected write")
		return DispatchResult{Attempt: attempt}, errCode(CodeDenied, "principal cannot dispatch a protected write")
	}
	stored, granted, err := e.store.Dispatch(ctx, principal.TenantID, idempotencyKey, e.nowUTC())
	if err != nil {
		if Is(err, CodeExpired) {
			e.record(ctx, principal, "dispatch", "write", stored.ID, auditDeny, CodeExpired, "expired approval cannot dispatch")
		} else {
			e.record(ctx, principal, "dispatch", "write", stored.ID, auditDeny, codeOf(err), "dispatch was not granted")
		}
		return DispatchResult{Attempt: stored, Granted: false}, err
	}
	e.record(ctx, principal, "dispatch", "write", stored.ID, auditAllow, "", "protected write dispatched once")
	return DispatchResult{Attempt: stored, Granted: granted}, nil
}

// ReportTimeout marks a dispatched write uncertain. It does not create or
// dispatch another attempt. A second report is idempotent.
func (e *Engine) ReportTimeout(ctx context.Context, principal Principal, idempotencyKey string) (WriteAttempt, error) {
	if _, err := e.prepareWrite(ctx, principal, idempotencyKey, "timeout"); err != nil {
		return WriteAttempt{}, err
	}
	if !principal.canStart() {
		return WriteAttempt{}, errCode(CodeDenied, "principal cannot report a write timeout")
	}
	attempt, err := e.store.Timeout(ctx, principal.TenantID, idempotencyKey, e.nowUTC())
	if err != nil {
		e.record(ctx, principal, "timeout", "write", attempt.ID, auditDeny, codeOf(err), "timeout was not recorded")
		return attempt, err
	}
	e.record(ctx, principal, "timeout", "write", attempt.ID, auditAllow, string(attempt.State), "dispatch timeout recorded")
	return attempt, nil
}

// ReportResult records a definite tool response for a dispatched write.
// An uncertain write stays uncertain until Reconcile.
func (e *Engine) ReportResult(ctx context.Context, principal Principal, idempotencyKey, outcome string) (WriteAttempt, error) {
	if _, err := e.prepareWrite(ctx, principal, idempotencyKey, "result"); err != nil {
		return WriteAttempt{}, err
	}
	if !principal.canStart() {
		return WriteAttempt{}, errCode(CodeDenied, "principal cannot record a write result")
	}
	attempt, err := e.store.Result(ctx, principal.TenantID, idempotencyKey, outcome, e.nowUTC())
	if err != nil {
		e.record(ctx, principal, "result", "write", attempt.ID, auditDeny, codeOf(err), "write result rejected")
		return attempt, err
	}
	e.record(ctx, principal, "result", "write", attempt.ID, auditAllow, outcome, "write result recorded")
	return attempt, nil
}

// Reconcile resolves an uncertain write by explicit operator intervention.
// It does not dispatch the tool again.
func (e *Engine) Reconcile(ctx context.Context, principal Principal, idempotencyKey, outcome string) (WriteAttempt, error) {
	attempt, err := e.prepareWrite(ctx, principal, idempotencyKey, "reconcile")
	if err != nil {
		return WriteAttempt{}, err
	}
	if !principal.canReconcile() {
		e.record(ctx, principal, "reconcile", "write", attempt.ID, auditDeny, CodeDenied, "principal cannot reconcile a write")
		return WriteAttempt{}, errCode(CodeDenied, "principal cannot reconcile a write")
	}
	stored, err := e.store.Reconcile(ctx, principal.TenantID, idempotencyKey, outcome, e.nowUTC())
	if err != nil {
		e.record(ctx, principal, "reconcile", "write", attempt.ID, auditDeny, codeOf(err), "reconciliation rejected")
		return stored, err
	}
	e.record(ctx, principal, "reconcile", "write", stored.ID, auditAllow, outcome, "uncertain write reconciled")
	return stored, nil
}

// Inbox lists WAITING approvals in the caller's tenant for an approver.
// Expired requests are moved to EXPIRED and omitted.
func (e *Engine) Inbox(ctx context.Context, principal Principal) ([]ApprovalRequest, error) {
	if err := e.ready(ctx, principal); err != nil {
		return nil, err
	}
	if principal.Kind == KindService || !principal.HasAny(RoleReleaseApprover, RoleSecurityReviewer, RoleAdministrator, RoleOperator) {
		e.record(ctx, principal, "inbox", "approval", principal.TenantID, auditDeny, CodeDenied, "principal cannot list approvals")
		return nil, errCode(CodeDenied, "principal cannot list approvals")
	}
	return e.store.ListInbox(ctx, principal.TenantID, e.nowUTC())
}

// Approval returns one request after a tenant check. An authorized caller also
// applies expiry.
func (e *Engine) Approval(ctx context.Context, principal Principal, requestID string) (ApprovalRequest, error) {
	if err := e.ready(ctx, principal); err != nil {
		return ApprovalRequest{}, err
	}
	approval, err := e.store.Approval(ctx, requestID)
	if err != nil {
		return ApprovalRequest{}, err
	}
	if approval.TenantID != principal.TenantID {
		e.record(ctx, principal, "read_approval", "approval", requestID, auditDeny, CodeCrossTenant, "cross-tenant approval read")
		return ApprovalRequest{}, errCode(CodeCrossTenant, "cannot read an approval for another tenant")
	}
	run, err := e.store.Run(ctx, approval.RunID)
	if err != nil {
		return ApprovalRequest{}, err
	}
	policy, err := e.store.Policy(ctx, approval.TenantID, approval.PolicyID, approval.PolicyRevision)
	if err != nil {
		return ApprovalRequest{}, err
	}
	if !principal.canInspect(run) && !principal.canApprove(policy) {
		e.record(ctx, principal, "read_approval", "approval", requestID, auditDeny, CodeDenied, "principal cannot read this approval")
		return ApprovalRequest{}, errCode(CodeDenied, "principal cannot read this approval")
	}
	current, err := e.store.ExpireApproval(ctx, requestID, e.nowUTC())
	if err != nil {
		return ApprovalRequest{}, err
	}
	if current.State == ApprovalExpired && approval.State != ApprovalExpired {
		e.auditExpired(ctx, principal, policy, requestID)
	}
	return current, nil
}

// Audit returns redacted decision records for the caller's tenant.
func (e *Engine) Audit(ctx context.Context, principal Principal, resourceID string) ([]AuditEvent, error) {
	if err := e.ready(ctx, principal); err != nil {
		return nil, err
	}
	if !principal.HasAny(RoleAuditor, RoleAdministrator, RoleSecurityReviewer) {
		e.record(ctx, principal, "audit", "audit", resourceID, auditDeny, CodeDenied, "principal cannot read the audit")
		return nil, errCode(CodeDenied, "principal cannot read the audit")
	}
	return e.store.Audit(ctx, principal.TenantID, resourceID)
}

func (e *Engine) evaluateTool(ctx context.Context, principal Principal, runID, toolID, sideEffect, untrusted string) (ToolDecision, RunRecord, Policy, error) {
	run, err := e.actingRun(ctx, principal, runID, "use_tool", "tool")
	if err != nil {
		return ToolDecision{}, RunRecord{}, Policy{}, err
	}
	def, err := e.store.Definition(ctx, run.TenantID, run.AgentID, run.Version)
	if err != nil {
		return ToolDecision{}, RunRecord{}, Policy{}, err
	}
	policy, err := e.store.Policy(ctx, run.TenantID, run.PolicyID, run.PolicyRevision)
	if err != nil {
		return ToolDecision{}, RunRecord{}, Policy{}, err
	}
	detail := "tool is not on the allowlist"
	if untrusted != "" {
		detail = "untrusted input ignored"
	}
	if !validID(toolID) || !toolPermitted(policy, def, toolID) {
		e.record(ctx, principal, "use_tool", "tool", toolID, auditDeny, CodeToolNotAllowed, detail)
		return ToolDecision{}, run, policy, errCode(CodeToolNotAllowed, "tool is not on the allowlist")
	}
	protected := isProtected(sideEffect, toolID, policy)
	e.record(ctx, principal, "use_tool", "tool", toolID, auditAllow, "", "tool permitted")
	return ToolDecision{Permitted: true, Protected: protected}, run, policy, nil
}

func (e *Engine) actingRun(ctx context.Context, principal Principal, runID, action, kind string) (RunRecord, error) {
	run, err := e.visibleRun(ctx, principal, runID, action, kind)
	if err != nil {
		return RunRecord{}, err
	}
	if !principal.canStart() && principal.UserID != run.RequesterID {
		e.record(ctx, principal, action, kind, runID, auditDeny, CodeDenied, "principal cannot act on this run")
		return RunRecord{}, errCode(CodeDenied, "principal cannot act on this run")
	}
	return run, nil
}

func (e *Engine) readableRun(ctx context.Context, principal Principal, runID, action, kind string) (RunRecord, error) {
	run, err := e.visibleRun(ctx, principal, runID, action, kind)
	if err != nil {
		return RunRecord{}, err
	}
	if !principal.canInspect(run) {
		e.record(ctx, principal, action, kind, runID, auditDeny, CodeDenied, "principal cannot inspect this run")
		return RunRecord{}, errCode(CodeDenied, "principal cannot inspect this run")
	}
	return run, nil
}

func (e *Engine) visibleRun(ctx context.Context, principal Principal, runID, action, kind string) (RunRecord, error) {
	run, err := e.store.Run(ctx, runID)
	if err != nil {
		return RunRecord{}, err
	}
	if run.TenantID != principal.TenantID {
		e.record(ctx, principal, action, kind, runID, auditDeny, CodeCrossTenant, "cross-tenant run read")
		return RunRecord{}, errCode(CodeCrossTenant, "cannot read a run for another tenant")
	}
	return run, nil
}

func (e *Engine) prepareWrite(ctx context.Context, principal Principal, idempotencyKey, action string) (WriteAttempt, error) {
	if err := e.ready(ctx, principal); err != nil {
		return WriteAttempt{}, err
	}
	if !validKey(idempotencyKey) {
		return WriteAttempt{}, errCode(CodeInvalidInput, "idempotency key is required")
	}
	attempt, err := e.store.Write(ctx, principal.TenantID, idempotencyKey)
	if err != nil {
		if Is(err, CodeNotFound) {
			e.record(ctx, principal, action, "write", idempotencyKey, auditDeny, CodeNotFound, "protected write not found")
		}
		return WriteAttempt{}, err
	}
	if attempt.TenantID != principal.TenantID {
		e.record(ctx, principal, action, "write", attempt.ID, auditDeny, CodeCrossTenant, "cross-tenant write read")
		return WriteAttempt{}, errCode(CodeCrossTenant, "cannot read a write for another tenant")
	}
	return attempt, nil
}

func (e *Engine) lookupDefinition(ctx context.Context, principal Principal, agentID, version, action string) (Definition, error) {
	def, err := e.store.Definition(ctx, principal.TenantID, agentID, version)
	if err == nil {
		return def, nil
	}
	if !Is(err, CodeNotFound) {
		return Definition{}, err
	}
	other, otherErr := e.store.DefinitionInOtherTenant(ctx, agentID, version, principal.TenantID)
	if otherErr != nil {
		return Definition{}, otherErr
	}
	if other {
		e.record(ctx, principal, action, "definition", agentID, auditDeny, CodeCrossTenant, "cross-tenant definition read")
		return Definition{}, errCode(CodeCrossTenant, "cannot read a definition for another tenant")
	}
	e.record(ctx, principal, action, "definition", agentID, auditDeny, CodeNotFound, "definition not found")
	return Definition{}, errCode(CodeNotFound, "definition not found")
}

func (e *Engine) auditExpired(ctx context.Context, principal Principal, policy Policy, requestID string) {
	role := policy.EscalationRole
	if role == "" {
		role = RoleAdministrator
	}
	e.record(ctx, principal, "expire", "approval", requestID, auditDeny, CodeExpired, "approval expired; escalation="+role)
}

func (p Policy) approvalTTL() time.Duration {
	if p.ApprovalTTL <= 0 {
		return DefaultApprovalTTL
	}
	return p.ApprovalTTL
}

func validatePolicy(policy Policy) error {
	if policy.MaxTokens < 0 || policy.MaxModelTurns < 0 || policy.ApprovalTTL < 0 {
		return errCode(CodeInvalidInput, "policy limits must be non-negative")
	}
	if math.IsNaN(policy.MaxCostUSD) || math.IsInf(policy.MaxCostUSD, 0) || policy.MaxCostUSD < 0 {
		return errCode(CodeInvalidInput, "policy cost cap must be a finite non-negative number")
	}
	if !validList(policy.AllowedModels) || !validList(policy.AllowedTools) || !validList(policy.AllowedDelegates) || !validList(policy.ProtectedTools) || !validList(policy.ApproverRoles) {
		return errCode(CodeInvalidInput, "policy allowlists must contain valid ids")
	}
	for _, model := range policy.AllowedModels {
		if !contracts.ValidModelID(model) {
			return errCode(CodeInvalidInput, "policy model id is invalid")
		}
	}
	if policy.EscalationRole != "" && !validID(policy.EscalationRole) {
		return errCode(CodeInvalidInput, "escalation role is invalid")
	}
	return nil
}

func finiteBudget(budget Budget) error {
	if budget.MaxTokens < 0 || budget.MaxModelTurns < 0 || math.IsNaN(budget.MaxCostUSD) || math.IsInf(budget.MaxCostUSD, 0) || budget.MaxCostUSD < 0 {
		return errCode(CodeInvalidInput, "budget values must be finite and non-negative")
	}
	return nil
}

func withinBudget(policy Policy, budget Budget) error {
	if policy.MaxTokens > 0 && budget.MaxTokens > policy.MaxTokens {
		return errCode(CodeBudgetExceeded, "token budget exceeds policy")
	}
	if policy.MaxModelTurns > 0 && budget.MaxModelTurns > policy.MaxModelTurns {
		return errCode(CodeBudgetExceeded, "model turn budget exceeds policy")
	}
	if policy.MaxCostUSD > 0 && budget.MaxCostUSD > policy.MaxCostUSD {
		return errCode(CodeBudgetExceeded, "cost budget exceeds policy")
	}
	return nil
}

func toolPermitted(policy Policy, def Definition, toolID string) bool {
	return contains(policy.AllowedTools, toolID) && contains(def.AllowedTools, toolID)
}

func delegatePermitted(policy Policy, def Definition, targetID string) bool {
	return contains(policy.AllowedDelegates, targetID) && contains(def.AllowedDelegates, targetID)
}

func isProtected(sideEffect, toolID string, policy Policy) bool {
	if contains(policy.ProtectedTools, toolID) {
		return true
	}
	return sideEffect != SideEffectReadOnly
}

func validList(ids []string) bool {
	for _, id := range ids {
		if !validID(id) {
			return false
		}
	}
	return true
}

func codeOf(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return CodeInternal
}
