package policy

import (
	"context"
	"sort"
	"sync"
	"time"
)

type pair struct{ a, b string }
type triple struct{ a, b, c string }

// Memory is the in-process store used by tests and by a process that has not
// wired the SQL migration. It enforces one decision, expiry, and one dispatch
// under a single lock.
type Memory struct {
	mu sync.Mutex

	policies    map[triple]Policy
	latest      map[pair]int
	definitions map[triple]Definition
	runs        map[string]RunRecord
	runKey      map[pair]string
	secrets     map[pair]SecretRef
	contexts    map[string]ContextRecord
	events      map[string][]EventRecord

	approvals       map[string]ApprovalRequest
	approvalKey     map[pair]string
	decisions       map[string]ApprovalDecision
	writes          map[string]WriteAttempt
	writeKey        map[pair]string
	writeByApproval map[string]string
	audits          []AuditEvent
}

// NewMemory returns an empty store.
func NewMemory() *Memory {
	return &Memory{
		policies:        map[triple]Policy{},
		latest:          map[pair]int{},
		definitions:     map[triple]Definition{},
		runs:            map[string]RunRecord{},
		runKey:          map[pair]string{},
		secrets:         map[pair]SecretRef{},
		contexts:        map[string]ContextRecord{},
		events:          map[string][]EventRecord{},
		approvals:       map[string]ApprovalRequest{},
		approvalKey:     map[pair]string{},
		decisions:       map[string]ApprovalDecision{},
		writes:          map[string]WriteAttempt{},
		writeKey:        map[pair]string{},
		writeByApproval: map[string]string{},
	}
}

func (m *Memory) InsertPolicy(_ context.Context, policy Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	policy = copyPolicy(policy)
	key := triple{policy.TenantID, policy.ID, itoa(policy.Revision)}
	if prev, ok := m.policies[key]; ok {
		if samePolicy(prev, policy) {
			return nil
		}
		return errCode(CodeConflict, "policy revision already exists")
	}
	m.policies[key] = policy
	latestKey := pair{policy.TenantID, policy.ID}
	if policy.Revision > m.latest[latestKey] {
		m.latest[latestKey] = policy.Revision
	}
	return nil
}

func (m *Memory) Policy(_ context.Context, tenantID, id string, revision int) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	policy, ok := m.policies[triple{tenantID, id, itoa(revision)}]
	if !ok {
		return Policy{}, errCode(CodeNotFound, "policy revision not found")
	}
	return copyPolicy(policy), nil
}

func (m *Memory) LatestPolicy(_ context.Context, tenantID, id string) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	revision, ok := m.latest[pair{tenantID, id}]
	if !ok {
		return Policy{}, errCode(CodeNotFound, "policy not found")
	}
	return copyPolicy(m.policies[triple{tenantID, id, itoa(revision)}]), nil
}

func (m *Memory) InsertDefinition(_ context.Context, def Definition) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	def.AllowedTools = cloneStrings(def.AllowedTools)
	def.AllowedDelegates = cloneStrings(def.AllowedDelegates)
	key := triple{def.TenantID, def.AgentID, def.Version}
	if prev, ok := m.definitions[key]; ok {
		if sameDefinition(prev, def) {
			return nil
		}
		return errCode(CodeConflict, "definition version already exists")
	}
	m.definitions[key] = def
	return nil
}

func (m *Memory) Definition(_ context.Context, tenantID, agentID, version string) (Definition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	def, ok := m.definitions[triple{tenantID, agentID, version}]
	if !ok {
		return Definition{}, errCode(CodeNotFound, "definition not found")
	}
	def.AllowedTools = cloneStrings(def.AllowedTools)
	def.AllowedDelegates = cloneStrings(def.AllowedDelegates)
	return def, nil
}

func (m *Memory) DefinitionInOtherTenant(_ context.Context, agentID, version, notTenant string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.definitions {
		if key.b == agentID && key.c == version && key.a != notTenant {
			return true, nil
		}
	}
	return false, nil
}

func (m *Memory) InsertRun(_ context.Context, run RunRecord) (RunRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.runs[run.ID]; ok {
		if equivalentRun(prev, run) {
			return prev, false, nil
		}
		return RunRecord{}, false, errCode(CodeConflict, "run id already exists")
	}
	if run.IdempotencyKey != "" {
		key := pair{run.TenantID, run.IdempotencyKey}
		if id, ok := m.runKey[key]; ok {
			prev := m.runs[id]
			if equivalentRun(prev, run) {
				return prev, false, nil
			}
			return RunRecord{}, false, errCode(CodeConflict, "run idempotency key was already used")
		}
		m.runKey[key] = run.ID
	}
	m.runs[run.ID] = run
	return run, true, nil
}

func (m *Memory) Run(_ context.Context, id string) (RunRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return RunRecord{}, errCode(CodeNotFound, "run not found")
	}
	return run, nil
}

func (m *Memory) InsertSecret(_ context.Context, ref SecretRef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := pair{ref.TenantID, ref.Name}
	if prev, ok := m.secrets[key]; ok {
		if prev.RunID == ref.RunID {
			return nil
		}
		return errCode(CodeConflict, "secret reference already exists")
	}
	m.secrets[key] = ref
	return nil
}

func (m *Memory) Secret(_ context.Context, tenantID, name string) (SecretRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref, ok := m.secrets[pair{tenantID, name}]
	if !ok {
		return SecretRef{}, errCode(CodeNotFound, "secret reference not found")
	}
	return ref, nil
}

func (m *Memory) SecretInOtherTenant(_ context.Context, name, notTenant string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.secrets {
		if key.b == name && key.a != notTenant {
			return true, nil
		}
	}
	return false, nil
}

func (m *Memory) InsertContext(_ context.Context, rec ContextRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.contexts[rec.ID]; ok {
		if prev.TenantID == rec.TenantID && prev.RunID == rec.RunID && prev.Summary == rec.Summary {
			return nil
		}
		return errCode(CodeConflict, "context id already exists")
	}
	m.contexts[rec.ID] = rec
	return nil
}

func (m *Memory) Context(_ context.Context, id string) (ContextRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.contexts[id]
	if !ok {
		return ContextRecord{}, errCode(CodeNotFound, "context not found")
	}
	return rec, nil
}

func (m *Memory) InsertEvents(_ context.Context, runID string, events []EventRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]EventRecord, len(events))
	copy(copied, events)
	m.events[runID] = append(m.events[runID], copied...)
	return nil
}

func (m *Memory) Events(_ context.Context, runID string) ([]EventRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := m.events[runID]
	out := make([]EventRecord, len(events))
	copy(out, events)
	return out, nil
}

func (m *Memory) CreateProtected(_ context.Context, approval ApprovalRequest, write WriteAttempt, now time.Time) (ApprovalRequest, WriteAttempt, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if approval.TenantID != write.TenantID || approval.IdempotencyKey != write.IdempotencyKey || approval.RunID != write.RunID {
		return ApprovalRequest{}, WriteAttempt{}, false, errCode(CodeInvalidInput, "approval and write attempt do not match")
	}
	key := pair{approval.TenantID, approval.IdempotencyKey}
	if id, ok := m.approvalKey[key]; ok {
		current, attempt := m.expireLocked(id, now)
		return current, copyWrite(attempt), false, nil
	}
	if _, ok := m.writeKey[key]; ok {
		return ApprovalRequest{}, WriteAttempt{}, false, errCode(CodeConflict, "write idempotency key already exists")
	}
	approval.State = ApprovalWaiting
	write.State = WriteWaiting
	write.ApprovalID = approval.ID
	write.DispatchCount = 0
	write.DispatchedAt = nil
	write.Outcome = ""
	m.approvals[approval.ID] = approval
	m.approvalKey[key] = approval.ID
	m.writes[write.ID] = write
	m.writeKey[key] = write.ID
	m.writeByApproval[approval.ID] = write.ID
	m.holdRun(approval.RunID)
	return approval, copyWrite(write), true, nil
}

func (m *Memory) Approval(_ context.Context, id string) (ApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	approval, ok := m.approvals[id]
	if !ok {
		return ApprovalRequest{}, errCode(CodeNotFound, "approval request not found")
	}
	return approval, nil
}

func (m *Memory) ExpireApproval(_ context.Context, id string, now time.Time) (ApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.approvals[id]; !ok {
		return ApprovalRequest{}, errCode(CodeNotFound, "approval request not found")
	}
	approval, _ := m.expireLocked(id, now)
	return approval, nil
}

func (m *Memory) ListInbox(_ context.Context, tenantID string, now time.Time) ([]ApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []ApprovalRequest{}
	for id, approval := range m.approvals {
		if approval.TenantID != tenantID {
			continue
		}
		current, _ := m.expireLocked(id, now)
		if current.State == ApprovalWaiting {
			out = append(out, current)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) Write(_ context.Context, tenantID, idempotencyKey string) (WriteAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	write, err := m.writeForKey(tenantID, idempotencyKey)
	if err != nil {
		return WriteAttempt{}, err
	}
	return copyWrite(write), nil
}

func (m *Memory) CommitDecision(_ context.Context, decision ApprovalDecision, now time.Time) (ApprovalDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	approval, ok := m.approvals[decision.RequestID]
	if !ok {
		return ApprovalDecision{}, errCode(CodeNotFound, "approval request not found")
	}
	if decision.Outcome != OutcomeApproved && decision.Outcome != OutcomeDenied {
		return ApprovalDecision{}, errCode(CodeInvalidInput, "decision outcome must be APPROVED or DENIED")
	}
	if decision.ApproverID == "" {
		return ApprovalDecision{}, errCode(CodeInvalidInput, "approver is required")
	}
	if _, decided := m.decisions[approval.ID]; decided || (approval.State != ApprovalWaiting && approval.State != ApprovalExpired) {
		return ApprovalDecision{}, errCode(CodeAlreadyDecided, "approval request already has a decision")
	}
	current, _ := m.expireLocked(approval.ID, now)
	if current.State == ApprovalExpired {
		return ApprovalDecision{}, errCode(CodeExpired, "approval request has expired")
	}
	if decision.ApproverID == current.RequesterID {
		return ApprovalDecision{}, errCode(CodeSelfApproval, "requester cannot approve their own protected write")
	}
	if current.State != ApprovalWaiting {
		return ApprovalDecision{}, errCode(CodeAlreadyDecided, "approval request already has a decision")
	}
	decision.TenantID = current.TenantID
	decision.RequesterID = current.RequesterID
	decision.DecidedAt = now
	m.decisions[current.ID] = decision
	write := m.writeLocked(current.ID)
	if decision.Outcome == OutcomeApproved {
		current.State = ApprovalApproved
		write.State = WriteReady
	} else {
		current.State = ApprovalDenied
		write.State = WriteDenied
	}
	write.UpdatedAt = now
	m.approvals[current.ID] = current
	m.writes[write.ID] = write
	m.releaseRun(current.RunID, string(current.State))
	return decision, nil
}

func (m *Memory) Dispatch(_ context.Context, tenantID, idempotencyKey string, now time.Time) (WriteAttempt, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	write, err := m.writeForKey(tenantID, idempotencyKey)
	if err != nil {
		return WriteAttempt{}, false, err
	}
	approval, _ := m.expireLocked(write.ApprovalID, now)
	write = m.writeLocked(approval.ID)
	switch write.State {
	case WriteUncertain, WriteReconciled:
		return copyWrite(write), false, errCode(CodeUncertain, "uncertain write requires reconciliation, not a second dispatch")
	case WriteDispatched, WriteSucceeded, WriteFailed:
		return copyWrite(write), false, errCode(CodeDuplicateWrite, "protected write was already dispatched")
	case WriteDenied:
		return copyWrite(write), false, errCode(CodeNotDispatchable, "denied approval cannot dispatch")
	case WriteExpired:
		return copyWrite(write), false, errCode(CodeExpired, "approval request has expired")
	case WriteWaiting:
		return copyWrite(write), false, errCode(CodeNotDispatchable, "protected write is waiting for approval")
	case WriteReady:
		if approval.State != ApprovalApproved || write.DispatchCount != 0 {
			return copyWrite(write), false, errCode(CodeNotDispatchable, "protected write is not ready to dispatch")
		}
		at := now
		write.State = WriteDispatched
		write.DispatchCount = 1
		write.DispatchedAt = &at
		write.UpdatedAt = now
		m.writes[write.ID] = write
		return copyWrite(write), true, nil
	default:
		return copyWrite(write), false, errCode(CodeNotDispatchable, "protected write cannot be dispatched")
	}
}

func (m *Memory) Timeout(_ context.Context, tenantID, idempotencyKey string, now time.Time) (WriteAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	write, err := m.writeForKey(tenantID, idempotencyKey)
	if err != nil {
		return WriteAttempt{}, err
	}
	switch write.State {
	case WriteUncertain:
		return copyWrite(write), nil
	case WriteDispatched:
		write.State = WriteUncertain
		write.Outcome = ""
		write.UpdatedAt = now
		m.writes[write.ID] = write
		return copyWrite(write), nil
	default:
		return copyWrite(write), errCode(CodeNotDispatchable, "only a dispatched write can time out")
	}
}

func (m *Memory) Result(_ context.Context, tenantID, idempotencyKey, outcome string, now time.Time) (WriteAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	write, err := m.writeForKey(tenantID, idempotencyKey)
	if err != nil {
		return WriteAttempt{}, err
	}
	if outcome != OutcomeSucceeded && outcome != OutcomeFailed {
		return WriteAttempt{}, errCode(CodeInvalidInput, "write result must be SUCCEEDED or FAILED")
	}
	switch write.State {
	case WriteUncertain, WriteReconciled:
		return copyWrite(write), errCode(CodeUncertain, "uncertain write requires explicit reconciliation")
	case WriteSucceeded, WriteFailed:
		if write.Outcome == outcome {
			return copyWrite(write), nil
		}
		return copyWrite(write), errCode(CodeConflict, "write result was already recorded")
	case WriteDispatched:
		write.State = WriteState(outcome)
		write.Outcome = outcome
		write.UpdatedAt = now
		m.writes[write.ID] = write
		return copyWrite(write), nil
	default:
		return copyWrite(write), errCode(CodeNotDispatchable, "write has not been dispatched")
	}
}

func (m *Memory) Reconcile(_ context.Context, tenantID, idempotencyKey, outcome string, now time.Time) (WriteAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	write, err := m.writeForKey(tenantID, idempotencyKey)
	if err != nil {
		return WriteAttempt{}, err
	}
	if outcome != OutcomeSucceeded && outcome != OutcomeFailed {
		return WriteAttempt{}, errCode(CodeInvalidInput, "reconciliation outcome must be SUCCEEDED or FAILED")
	}
	switch write.State {
	case WriteReconciled:
		if write.Outcome == outcome {
			return copyWrite(write), nil
		}
		return copyWrite(write), errCode(CodeConflict, "write was already reconciled")
	case WriteUncertain:
		if write.DispatchCount != 1 {
			return copyWrite(write), errCode(CodeNotDispatchable, "uncertain write is missing its dispatch")
		}
		write.State = WriteReconciled
		write.Outcome = outcome
		write.UpdatedAt = now
		m.writes[write.ID] = write
		return copyWrite(write), nil
	default:
		return copyWrite(write), errCode(CodeNotDispatchable, "only an uncertain write can be reconciled")
	}
}

func (m *Memory) AddAudit(_ context.Context, event AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audits = append(m.audits, event)
	return nil
}

func (m *Memory) Audit(_ context.Context, tenantID, resourceID string) ([]AuditEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []AuditEvent{}
	for _, event := range m.audits {
		if event.TenantID != tenantID {
			continue
		}
		if resourceID != "" && event.ResourceID != resourceID {
			continue
		}
		out = append(out, event)
	}
	return out, nil
}

func (m *Memory) expireLocked(approvalID string, now time.Time) (ApprovalRequest, WriteAttempt) {
	approval := m.approvals[approvalID]
	write := m.writeLocked(approvalID)
	nextApproval, nextWrite, changed := expirePair(approval, write, now)
	if !changed {
		return approval, write
	}
	m.approvals[nextApproval.ID] = nextApproval
	if nextWrite.ID != "" && nextWrite.State != write.State {
		m.writes[nextWrite.ID] = nextWrite
	}
	m.releaseRun(nextApproval.RunID, string(ApprovalExpired))
	return nextApproval, nextWrite
}

func (m *Memory) writeLocked(approvalID string) WriteAttempt {
	id := m.writeByApproval[approvalID]
	return m.writes[id]
}

func (m *Memory) writeForKey(tenantID, idempotencyKey string) (WriteAttempt, error) {
	id, ok := m.writeKey[pair{tenantID, idempotencyKey}]
	if !ok {
		return WriteAttempt{}, errCode(CodeNotFound, "protected write not found")
	}
	return m.writes[id], nil
}

func (m *Memory) holdRun(id string) {
	run, ok := m.runs[id]
	if !ok {
		return
	}
	if run.State != StateWaiting {
		run.ResumeState = run.State
		run.State = StateWaiting
	}
	run.Hold = string(ApprovalWaiting)
	m.runs[id] = run
}

func (m *Memory) releaseRun(id, hold string) {
	run, ok := m.runs[id]
	if !ok {
		return
	}
	if run.State == StateWaiting {
		if run.ResumeState != "" {
			run.State = run.ResumeState
		} else {
			run.State = "RUNNING"
		}
	}
	run.Hold = hold
	m.runs[id] = run
}

func copyWrite(write WriteAttempt) WriteAttempt {
	if write.DispatchedAt != nil {
		at := *write.DispatchedAt
		write.DispatchedAt = &at
	}
	return write
}

func copyPolicy(policy Policy) Policy {
	policy.AllowedModels = cloneStrings(policy.AllowedModels)
	policy.AllowedTools = cloneStrings(policy.AllowedTools)
	policy.AllowedDelegates = cloneStrings(policy.AllowedDelegates)
	policy.ProtectedTools = cloneStrings(policy.ProtectedTools)
	policy.ApproverRoles = cloneStrings(policy.ApproverRoles)
	return policy
}

func samePolicy(a, b Policy) bool {
	return a.TenantID == b.TenantID && a.ID == b.ID && a.Revision == b.Revision &&
		sameStrings(a.AllowedModels, b.AllowedModels) && sameStrings(a.AllowedTools, b.AllowedTools) &&
		sameStrings(a.AllowedDelegates, b.AllowedDelegates) && sameStrings(a.ProtectedTools, b.ProtectedTools) &&
		sameStrings(a.ApproverRoles, b.ApproverRoles) && a.MaxTokens == b.MaxTokens &&
		a.MaxCostUSD == b.MaxCostUSD && a.MaxModelTurns == b.MaxModelTurns &&
		a.ApprovalTTL == b.ApprovalTTL && a.EscalationRole == b.EscalationRole &&
		a.AllowServiceApproval == b.AllowServiceApproval
}

func sameDefinition(a, b Definition) bool {
	return a.TenantID == b.TenantID && a.AgentID == b.AgentID && a.Version == b.Version &&
		sameStrings(a.AllowedTools, b.AllowedTools) && sameStrings(a.AllowedDelegates, b.AllowedDelegates)
}

func equivalentRun(a, b RunRecord) bool {
	return a.TenantID == b.TenantID && a.RequesterID == b.RequesterID && a.AgentID == b.AgentID &&
		a.Version == b.Version && a.ModelID == b.ModelID && a.PolicyID == b.PolicyID &&
		a.IdempotencyKey == b.IdempotencyKey && a.Budget == b.Budget
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
