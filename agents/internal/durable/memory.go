package durable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Memory is the reference journal. A restarted executor keeps the same Memory;
// the process that owns the journal is the durable store, not the worker.
// Admit, CommitStep, and RecoverClaim each hold one lock for the whole update.
type Memory struct {
	mu            sync.Mutex
	seq           int64
	fence         int64
	checkpointSeq map[string]int64
	runs          map[string]contracts.Run
	events        map[string][]contracts.RunEvent
	idem          map[string]idemRow
	idemByRun     map[string]string
	leases        map[string]Lease
	attemptOrder  map[string][]string
	attempts      map[string]Attempt
	active        map[string]string
	outcomes      map[string]InvocationOutcome
	checkpoints   map[string][]Checkpoint
	cancellations map[string]Cancellation
}

type idemRow struct {
	TenantID  string
	Key       string
	RunID     string
	Hash      string
	CreatedAt time.Time
}

type eventBody struct {
	AttemptID     string   `json:"attempt_id,omitempty"`
	AttemptNumber int      `json:"attempt_number,omitempty"`
	OperationID   string   `json:"operation_id,omitempty"`
	StepID        string   `json:"step_id,omitempty"`
	WorkerID      string   `json:"worker_id,omitempty"`
	State         string   `json:"state,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	SideEffect    string   `json:"side_effect,omitempty"`
	Sequence      int64    `json:"checkpoint_sequence,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Error         string   `json:"error,omitempty"`
	Abandoned     []string `json:"abandoned_attempt_ids,omitempty"`
}

func NewMemory() *Memory {
	return &Memory{
		checkpointSeq: map[string]int64{},
		runs:          map[string]contracts.Run{},
		events:        map[string][]contracts.RunEvent{},
		idem:          map[string]idemRow{},
		idemByRun:     map[string]string{},
		leases:        map[string]Lease{},
		attemptOrder:  map[string][]string{},
		attempts:      map[string]Attempt{},
		active:        map[string]string{},
		outcomes:      map[string]InvocationOutcome{},
		checkpoints:   map[string][]Checkpoint{},
		cancellations: map[string]Cancellation{},
	}
}

var (
	_ Store               = (*Memory)(nil)
	_ contracts.RunStore  = (*Memory)(nil)
	_ contracts.EventSink = (*Memory)(nil)
)

func (m *Memory) Admit(_ context.Context, req AdmitRequest) (contracts.Run, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		return contracts.Run{}, false, invalid("idempotency key is required")
	}
	if strings.TrimSpace(req.Run.ID) == "" {
		return contracts.Run{}, false, invalid("run ID is required")
	}
	if strings.TrimSpace(req.Run.AgentID) == "" || strings.TrimSpace(req.Run.Version) == "" {
		return contracts.Run{}, false, invalid("agent ID and version are required")
	}
	tenant := req.TenantID
	if strings.TrimSpace(tenant) == "" {
		tenant = "local"
	}
	hash := requestHash(req.Run)
	composite := tenant + "\x00" + key
	if row, ok := m.idem[composite]; ok {
		if row.Hash != hash {
			return contracts.Run{}, false, ErrIdempotencyConflict
		}
		run, ok := m.runs[row.RunID]
		if !ok {
			return contracts.Run{}, false, notFoundRun()
		}
		return cloneRun(run), false, nil
	}
	now := req.Now.UTC()
	if req.Now.IsZero() {
		now = time.Now().UTC()
	}
	req.Run.State = contracts.StatePending
	if req.Run.CreatedAt.IsZero() {
		req.Run.CreatedAt = now
	}
	if req.Run.UpdatedAt.IsZero() {
		req.Run.UpdatedAt = req.Run.CreatedAt
	}
	if err := m.createLocked(req.Run); err != nil {
		return contracts.Run{}, false, err
	}
	if err := m.appendLocked(req.Run.ID, EventRunAdmitted, "", eventBody{State: string(contracts.StatePending)}, now); err != nil {
		delete(m.runs, req.Run.ID)
		delete(m.events, req.Run.ID)
		return contracts.Run{}, false, err
	}
	m.idem[composite] = idemRow{TenantID: tenant, Key: key, RunID: req.Run.ID, Hash: hash, CreatedAt: now}
	m.idemByRun[req.Run.ID] = composite
	return cloneRun(m.runs[req.Run.ID]), true, nil
}

func (m *Memory) Create(_ context.Context, run contracts.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run.State == "" {
		run.State = contracts.StatePending
	}
	return m.createLocked(run)
}

func (m *Memory) createLocked(run contracts.Run) error {
	if run.ID == "" {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "run ID is required"}
	}
	if _, ok := m.runs[run.ID]; ok {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "run ID already exists"}
	}
	if run.State != contracts.StatePending {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "new runs start PENDING"}
	}
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	run.CreatedAt = run.CreatedAt.UTC()
	run.UpdatedAt = run.UpdatedAt.UTC()
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	m.runs[run.ID] = cloneRun(run)
	m.ensureRootLocked(run.ID, run.CreatedAt)
	return nil
}

func (m *Memory) Get(_ context.Context, runID string) (contracts.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return contracts.Run{}, notFoundRun()
	}
	return cloneRun(run), nil
}

func (m *Memory) Update(_ context.Context, run contracts.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.runs[run.ID]
	if !ok {
		return notFoundRun()
	}
	if _, held := m.leases[run.ID]; held {
		return ErrLeaseHeld
	}
	if !transition(current.State, run.State) {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid run state transition"}
	}
	if current.State == contracts.StateSucceeded || current.State == contracts.StateFailed || current.State == contracts.StateCancelled {
		oldRaw, _ := json.Marshal(current)
		newRaw, _ := json.Marshal(run)
		if string(oldRaw) != string(newRaw) {
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "terminal run cannot change"}
		}
	}
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	m.runs[run.ID] = cloneRun(run)
	return nil
}

func (m *Memory) Append(_ context.Context, event contracts.RunEvent) (contracts.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.RunID == "" || event.Type == "" {
		return contracts.RunEvent{}, invalid("event run ID and type are required")
	}
	if len(event.Data) > 0 && !json.Valid(event.Data) {
		return contracts.RunEvent{}, invalid("event data is not json")
	}
	now := event.Timestamp.UTC()
	if event.Timestamp.IsZero() {
		now = time.Now().UTC()
	}
	inv := ""
	if event.InvocationID != nil {
		inv = *event.InvocationID
	}
	var payload any
	if len(event.Data) > 0 {
		payload = cloneRaw(event.Data)
	}
	before := len(m.events[event.RunID])
	if err := m.appendLocked(event.RunID, event.Type, inv, payload, now); err != nil {
		return contracts.RunEvent{}, err
	}
	return cloneEvent(m.events[event.RunID][before]), nil
}

func (m *Memory) List(_ context.Context, runID string, after uint64) ([]contracts.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return nil, notFoundRun()
	}
	out := []contracts.RunEvent{}
	for _, event := range m.events[runID] {
		if event.Sequence > after {
			out = append(out, cloneEvent(event))
		}
	}
	return out, nil
}

func (m *Memory) Cancel(_ context.Context, runID, reason string, now time.Time) (Cancellation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return Cancellation{}, notFoundRun()
	}
	now = utc(now)
	if run.State == contracts.StateSucceeded || run.State == contracts.StateFailed {
		return Cancellation{}, ErrRunTerminal
	}
	if existing, ok := m.cancellations[runID]; ok {
		return existing, nil
	}
	if strings.TrimSpace(reason) == "" {
		return Cancellation{}, invalid("cancellation reason is required")
	}
	record := Cancellation{RunID: runID, Reason: reason, RequestedAt: now}
	m.cancellations[runID] = record
	if run.State == contracts.StatePending || run.State == contracts.StateRunning {
		run.State = contracts.StateCancelled
		run.UpdatedAt = now
		run.Error = &contracts.RunError{Code: "CANCELLED", Message: reason}
		m.cancelOpenInvocations(&run, now)
		m.runs[runID] = run
	}
	if err := m.appendLocked(runID, EventCancellationRequested, "", eventBody{Reason: reason, State: string(contracts.StateCancelled)}, now); err != nil {
		return Cancellation{}, err
	}
	return record, nil
}

func (m *Memory) Cancellation(_ context.Context, runID string) (Cancellation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return Cancellation{}, false, notFoundRun()
	}
	record, ok := m.cancellations[runID]
	return record, ok, nil
}

func (m *Memory) ClaimRun(_ context.Context, runID, workerID string, ttl time.Duration, now time.Time) (Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.claimLocked(runID, workerID, ttl, now)
}

func (m *Memory) claimLocked(runID, workerID string, ttl time.Duration, now time.Time) (Lease, error) {
	now = utc(now)
	if err := requireWorker(workerID, ttl); err != nil {
		return Lease{}, err
	}
	run, ok := m.runs[runID]
	if !ok {
		return Lease{}, notFoundRun()
	}
	switch run.State {
	case contracts.StateCancelled:
		return Lease{}, ErrCancelled
	case contracts.StateSucceeded, contracts.StateFailed:
		return Lease{}, ErrRunTerminal
	}
	if _, cancelled := m.cancellations[runID]; cancelled {
		return Lease{}, ErrCancelled
	}
	if lease, ok := m.leases[runID]; ok {
		if lease.ExpiresAt.After(now) {
			return Lease{}, ErrLeaseHeld
		}
		return Lease{}, ErrNeedsRecovery
	}
	m.fence++
	lease := Lease{RunID: runID, WorkerID: workerID, FencingToken: m.fence, ExpiresAt: now.Add(ttl)}
	m.leases[runID] = lease
	if run.State == contracts.StatePending {
		run.State = contracts.StateRunning
		run.UpdatedAt = now
		m.setInvocationState(&run, RootInvocationID(runID), contracts.StateRunning, now, false)
		m.runs[runID] = run
	}
	return lease, nil
}

func (m *Memory) RenewLease(_ context.Context, lease Lease, ttl time.Duration, now time.Time) (Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now = utc(now)
	if ttl <= 0 {
		return Lease{}, invalid("lease TTL must be positive")
	}
	if err := m.checkLease(lease, now); err != nil {
		return Lease{}, err
	}
	lease.ExpiresAt = now.Add(ttl)
	m.leases[lease.RunID] = lease
	for _, id := range m.attemptOrder[lease.RunID] {
		att := m.attempts[id]
		if att.State == AttemptRunning && att.WorkerID == lease.WorkerID && att.FencingToken == lease.FencingToken {
			att.LeaseExpiresAt = lease.ExpiresAt
			m.attempts[id] = att
		}
	}
	return lease, nil
}

func (m *Memory) ReleaseLease(_ context.Context, lease Lease, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.leases[lease.RunID]
	if !ok {
		return ErrStaleLease
	}
	if current.FencingToken != lease.FencingToken || current.WorkerID != lease.WorkerID {
		return ErrStaleLease
	}
	delete(m.leases, lease.RunID)
	_ = now
	return nil
}

func (m *Memory) RecoverClaim(_ context.Context, runID, workerID string, ttl time.Duration, now time.Time) (Lease, []Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now = utc(now)
	if err := requireWorker(workerID, ttl); err != nil {
		return Lease{}, nil, err
	}
	run, ok := m.runs[runID]
	if !ok {
		return Lease{}, nil, notFoundRun()
	}
	if run.State == contracts.StateSucceeded || run.State == contracts.StateFailed {
		return Lease{}, nil, ErrRunTerminal
	}
	if lease, ok := m.leases[runID]; ok && lease.ExpiresAt.After(now) {
		return Lease{}, nil, ErrLeaseHeld
	}
	cancel := run.State == contracts.StateCancelled
	if _, ok := m.cancellations[runID]; ok {
		cancel = true
	}
	for _, id := range m.attemptOrder[runID] {
		att := m.attempts[id]
		if att.State != AttemptRunning && att.State != AttemptClaimed {
			continue
		}
		if att.LeaseExpiresAt.After(now) && !cancel {
			return Lease{}, nil, ErrLeaseHeld
		}
	}
	var abandoned []Attempt
	for _, id := range append([]string(nil), m.attemptOrder[runID]...) {
		att := m.attempts[id]
		if att.State != AttemptRunning && att.State != AttemptClaimed {
			continue
		}
		next := AttemptAbandoned
		event := EventAttemptAbandoned
		if cancel {
			next = AttemptCancelled
			event = EventAttemptCancelled
		}
		att.State = next
		att.FinishedAt = timePtr(now)
		if m.active[att.OperationID] == att.ID {
			delete(m.active, att.OperationID)
		}
		m.attempts[att.ID] = att
		abandoned = append(abandoned, cloneAttempt(att))
		if err := m.appendLocked(runID, event, att.InvocationID, eventBody{
			AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID,
			StepID: att.StepID, WorkerID: att.WorkerID, State: string(next),
		}, now); err != nil {
			return Lease{}, nil, err
		}
	}
	// Abandoning an expired worker records the attempt. It does not finish the run.
	if run.State == contracts.StateSucceeded {
		return Lease{}, nil, ErrAlreadyCompleted
	}
	if cancel {
		delete(m.leases, runID)
		if run.State == contracts.StatePending || run.State == contracts.StateRunning {
			run.State = contracts.StateCancelled
			run.UpdatedAt = now
			m.cancelOpenInvocations(&run, now)
			m.runs[runID] = run
		}
		return Lease{}, abandoned, nil
	}
	m.fence++
	lease := Lease{RunID: runID, WorkerID: workerID, FencingToken: m.fence, ExpiresAt: now.Add(ttl)}
	m.leases[runID] = lease
	if run.State == contracts.StatePending {
		run.State = contracts.StateRunning
		run.UpdatedAt = now
		m.setInvocationState(&run, RootInvocationID(runID), contracts.StateRunning, now, false)
		m.runs[runID] = run
	}
	return lease, abandoned, nil
}

func (m *Memory) BeginAttempt(_ context.Context, req BeginRequest) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := utc(req.Now)
	run, ok := m.runs[req.RunID]
	if !ok {
		return Attempt{}, notFoundRun()
	}
	if _, cancelled := m.cancellations[req.RunID]; cancelled || run.State == contracts.StateCancelled {
		return Attempt{}, ErrCancelled
	}
	if run.State == contracts.StateSucceeded || run.State == contracts.StateFailed {
		return Attempt{}, ErrRunTerminal
	}
	if err := m.checkLease(Lease{RunID: req.RunID, WorkerID: req.WorkerID, FencingToken: req.FencingToken}, now); err != nil {
		return Attempt{}, err
	}
	step := req.Step
	step.OperationID = step.operationID()
	if step.ID == "" || step.OperationID == "" {
		return Attempt{}, invalid("step id and operation id are required")
	}
	if step.InvocationID != "" {
		if outcome, ok := m.outcomes[step.InvocationID]; ok && outcome.State == contracts.StateSucceeded {
			return Attempt{}, ErrAlreadyCompleted
		}
	}
	for _, id := range m.attemptOrder[req.RunID] {
		att := m.attempts[id]
		if att.OperationID != step.OperationID {
			continue
		}
		if att.State == AttemptUncertain {
			return Attempt{}, ErrUncertain
		}
	}
	if activeID, ok := m.active[step.OperationID]; ok {
		active := m.attempts[activeID]
		if active.State == AttemptRunning || active.State == AttemptClaimed {
			return Attempt{}, ErrAttemptClaimed
		}
	}
	number := 1
	for _, id := range m.attemptOrder[req.RunID] {
		att := m.attempts[id]
		if att.OperationID == step.OperationID && att.AttemptNumber >= number {
			number = att.AttemptNumber + 1
		}
	}
	lease := m.leases[req.RunID]
	att := Attempt{
		ID: m.nextID("att"), AttemptNumber: number, RunID: req.RunID, OperationID: step.OperationID,
		WorkerID: req.WorkerID, State: AttemptRunning, Kind: step.Kind, SideEffect: step.SideEffect,
		StepID: step.ID, InvocationID: step.InvocationID, ParentInvocationID: step.ParentInvocationID,
		FencingToken: req.FencingToken, StartedAt: now, LeaseExpiresAt: lease.ExpiresAt,
	}
	m.attempts[att.ID] = att
	m.attemptOrder[req.RunID] = append(m.attemptOrder[req.RunID], att.ID)
	m.active[step.OperationID] = att.ID
	m.projectBegin(&run, att, now)
	m.runs[req.RunID] = run
	if err := m.appendLocked(req.RunID, EventAttemptStarted, att.InvocationID, eventBody{
		AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID,
		StepID: att.StepID, WorkerID: att.WorkerID, State: string(att.State),
		Kind: string(att.Kind), SideEffect: string(att.SideEffect),
	}, now); err != nil {
		return Attempt{}, err
	}
	return cloneAttempt(att), nil
}

func (m *Memory) FailAttempt(_ context.Context, req FailRequest) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := utc(req.Now)
	att, err := m.ownedAttempt(req.AttemptID, req.WorkerID, req.FencingToken, now)
	if err != nil {
		return Attempt{}, err
	}
	if att.State != AttemptRunning && att.State != AttemptClaimed {
		return Attempt{}, invalid("attempt is not active")
	}
	att.State = AttemptFailed
	cause := req.Error
	att.Error = &cause
	att.FinishedAt = timePtr(now)
	if m.active[att.OperationID] == att.ID {
		delete(m.active, att.OperationID)
	}
	m.attempts[att.ID] = att
	if run, ok := m.runs[att.RunID]; ok && att.Kind == KindReadTool {
		m.finishTool(&run, att, nil, &cause, now)
		m.runs[att.RunID] = run
	}
	if err := m.appendLocked(att.RunID, EventAttemptFailed, att.InvocationID, eventBody{
		AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID,
		StepID: att.StepID, WorkerID: att.WorkerID, State: string(att.State), Error: cause.Message,
	}, now); err != nil {
		return Attempt{}, err
	}
	return cloneAttempt(att), nil
}

func (m *Memory) MarkUncertain(_ context.Context, req UncertainRequest) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := utc(req.Now)
	if strings.TrimSpace(req.Reason) == "" {
		return Attempt{}, invalid("uncertain reason is required")
	}
	att, err := m.ownedAttempt(req.AttemptID, req.WorkerID, req.FencingToken, now)
	if err != nil {
		return Attempt{}, err
	}
	if att.State == AttemptUncertain {
		return cloneAttempt(att), nil
	}
	if att.State != AttemptRunning && att.State != AttemptClaimed {
		return Attempt{}, invalid("attempt is not active")
	}
	att.State = AttemptUncertain
	att.Error = &contracts.RunError{Code: contracts.ErrInternal, Message: req.Reason}
	att.FinishedAt = timePtr(now)
	if m.active[att.OperationID] == att.ID {
		delete(m.active, att.OperationID)
	}
	m.attempts[att.ID] = att
	if err := m.appendLocked(att.RunID, EventAttemptUncertain, att.InvocationID, eventBody{
		AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID,
		StepID: att.StepID, WorkerID: att.WorkerID, State: string(att.State), Reason: req.Reason,
		SideEffect: string(att.SideEffect),
	}, now); err != nil {
		return Attempt{}, err
	}
	return cloneAttempt(att), nil
}

func (m *Memory) CommitStep(_ context.Context, req CommitRequest) (Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := utc(req.Now)
	att, err := m.ownedAttempt(req.AttemptID, req.WorkerID, req.FencingToken, now)
	if err != nil {
		return Checkpoint{}, err
	}
	if att.State == AttemptSucceeded {
		return m.latestCheckpoint(att.RunID), ErrAlreadyCompleted
	}
	if att.State != AttemptRunning {
		return Checkpoint{}, invalid("attempt is not active")
	}
	if !json.Valid(req.Output) {
		return Checkpoint{}, invalid("step output is not json")
	}
	run, ok := m.runs[att.RunID]
	if !ok {
		return Checkpoint{}, notFoundRun()
	}
	if _, cancelled := m.cancellations[att.RunID]; att.Kind == KindFinish && (run.State == contracts.StateCancelled || cancelled) {
		att.State = AttemptCancelled
		att.FinishedAt = timePtr(now)
		att.Error = &contracts.RunError{Code: "CANCELLED", Message: "run cancelled before finish"}
		if m.active[att.OperationID] == att.ID {
			delete(m.active, att.OperationID)
		}
		m.attempts[att.ID] = att
		_ = m.appendLocked(att.RunID, EventAttemptCancelled, att.InvocationID, eventBody{
			AttemptID: att.ID, State: string(AttemptCancelled), Reason: "run cancelled before finish",
		}, now)
		return Checkpoint{}, ErrCancelled
	}
	if att.Kind == KindFinish && run.State != contracts.StateRunning && run.State != contracts.StatePending {
		return Checkpoint{}, ErrRunTerminal
	}
	if previous, ok := m.outcomes[att.InvocationID]; ok && att.InvocationID != "" && previous.State == contracts.StateSucceeded {
		cause := contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invocation already completed"}
		att.State = AttemptFailed
		att.Error = &cause
		att.FinishedAt = timePtr(now)
		if m.active[att.OperationID] == att.ID {
			delete(m.active, att.OperationID)
		}
		m.attempts[att.ID] = att
		_ = m.appendLocked(att.RunID, EventAttemptFailed, att.InvocationID, eventBody{
			AttemptID: att.ID, AttemptNumber: att.AttemptNumber, State: string(AttemptFailed), Error: cause.Message,
		}, now)
		return Checkpoint{}, ErrAlreadyCompleted
	}
	progress := m.progressLocked(att.RunID)
	if progress.Outputs == nil {
		progress.Outputs = map[string]json.RawMessage{}
	}
	if att.InvocationID != "" {
		progress.Outputs[att.InvocationID] = cloneRaw(req.Output)
	}
	if !containsStep(progress.CompletedSteps, att.StepID) {
		progress.CompletedSteps = append(progress.CompletedSteps, att.StepID)
	}
	payload, err := json.Marshal(progress)
	if err != nil {
		return Checkpoint{}, err
	}
	m.checkpointSeq[att.RunID]++
	cp := Checkpoint{
		RunID: att.RunID, Sequence: m.checkpointSeq[att.RunID], StepID: att.StepID,
		InvocationID: att.InvocationID, AttemptID: att.ID, Payload: payload, CreatedAt: now,
	}
	m.checkpoints[att.RunID] = append(m.checkpoints[att.RunID], cp)
	att.State = AttemptSucceeded
	att.FinishedAt = timePtr(now)
	att.Error = nil
	if m.active[att.OperationID] == att.ID {
		delete(m.active, att.OperationID)
	}
	m.attempts[att.ID] = att
	if att.InvocationID != "" {
		m.outcomes[att.InvocationID] = InvocationOutcome{
			InvocationID: att.InvocationID, RunID: att.RunID, State: contracts.StateSucceeded,
			Output: cloneRaw(req.Output), AttemptID: att.ID, CompletedAt: now,
		}
	}
	m.projectCommit(&run, att, cloneRaw(req.Output), now)
	if att.Kind == KindFinish {
		run.State = contracts.StateSucceeded
		run.Output = cloneRaw(req.Output)
		run.Error = nil
		run.UpdatedAt = now
	}
	m.runs[att.RunID] = run
	bodies := []struct {
		typ  string
		body eventBody
	}{
		{EventAttemptSucceeded, eventBody{AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID, StepID: att.StepID, WorkerID: att.WorkerID, State: string(AttemptSucceeded)}},
		{EventInvocationCompleted, eventBody{AttemptID: att.ID, StepID: att.StepID, State: string(contracts.StateSucceeded)}},
		{EventCheckpointSaved, eventBody{AttemptID: att.ID, StepID: att.StepID, Sequence: cp.Sequence}},
	}
	if att.Kind == KindFinish {
		bodies = append(bodies, struct {
			typ  string
			body eventBody
		}{EventRunCompleted, eventBody{AttemptID: att.ID, State: string(contracts.StateSucceeded)}})
	}
	for _, item := range bodies {
		if err := m.appendLocked(att.RunID, item.typ, att.InvocationID, item.body, now); err != nil {
			return Checkpoint{}, err
		}
	}
	return cloneCheckpoint(cp), nil
}

func (m *Memory) FailRun(_ context.Context, runID string, cause contracts.RunError, lease Lease, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now = utc(now)
	if err := m.checkLease(lease, now); err != nil {
		return err
	}
	run, ok := m.runs[runID]
	if !ok {
		return notFoundRun()
	}
	if run.State == contracts.StateSucceeded {
		return ErrAlreadyCompleted
	}
	if run.State == contracts.StateFailed {
		return nil
	}
	if run.State == contracts.StateCancelled {
		return ErrCancelled
	}
	run.State = contracts.StateFailed
	copied := cause
	run.Error = &copied
	run.UpdatedAt = now
	for i := range run.Invocations {
		if run.Invocations[i].State == contracts.StateRunning || run.Invocations[i].State == contracts.StatePending {
			run.Invocations[i].State = contracts.StateFailed
			errCopy := cause
			run.Invocations[i].Error = &errCopy
			run.Invocations[i].FinishedAt = timePtr(now)
		}
	}
	m.runs[runID] = run
	return m.appendLocked(runID, EventRunFailed, "", eventBody{State: string(contracts.StateFailed), Error: cause.Message, WorkerID: lease.WorkerID}, now)
}

func (m *Memory) LatestProgress(_ context.Context, runID string) (Progress, Checkpoint, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return Progress{}, Checkpoint{}, false, notFoundRun()
	}
	list := m.checkpoints[runID]
	if len(list) == 0 {
		return emptyProgress(), Checkpoint{}, false, nil
	}
	cp := list[len(list)-1]
	return decodeProgress(cp.Payload), cloneCheckpoint(cp), true, nil
}

func (m *Memory) Invocation(_ context.Context, invocationID string) (InvocationOutcome, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	outcome, ok := m.outcomes[invocationID]
	if !ok {
		return InvocationOutcome{}, false, nil
	}
	return cloneOutcome(outcome), true, nil
}

func (m *Memory) Attempts(_ context.Context, runID string) ([]Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return nil, notFoundRun()
	}
	return m.attemptsLocked(runID), nil
}

func (m *Memory) Checkpoints(_ context.Context, runID string) ([]Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return nil, notFoundRun()
	}
	return m.checkpointsLocked(runID), nil
}

func (m *Memory) Outcomes(_ context.Context, runID string) ([]InvocationOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return nil, notFoundRun()
	}
	return m.outcomesLocked(runID), nil
}

func (m *Memory) CurrentLease(_ context.Context, runID string) (Lease, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return Lease{}, false, notFoundRun()
	}
	lease, ok := m.leases[runID]
	return lease, ok, nil
}

func (m *Memory) NoteRecovery(_ context.Context, runID, workerID string, abandoned []Attempt, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return notFoundRun()
	}
	ids := make([]string, 0, len(abandoned))
	for _, att := range abandoned {
		ids = append(ids, att.ID)
	}
	return m.appendLocked(runID, EventRecoveryStarted, "", eventBody{WorkerID: workerID, Abandoned: ids, State: "RECOVERING"}, utc(now))
}

func (m *Memory) ApplyRetention(_ context.Context, policy RetentionPolicy, now time.Time) (RetentionStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now = utc(now)
	if policy.TerminalHorizon <= 0 {
		return RetentionStats{}, invalid("retention horizon must be positive")
	}
	keep := policy.KeepLatestCheckpoints
	if keep < 1 {
		keep = 1
	}
	var stats RetentionStats
	cutoff := now.Add(-policy.TerminalHorizon)
	for id, run := range m.runs {
		if run.State != contracts.StateSucceeded && run.State != contracts.StateFailed && run.State != contracts.StateCancelled {
			continue
		}
		if !run.UpdatedAt.Before(cutoff) {
			continue
		}
		list := m.checkpoints[id]
		if len(list) <= keep {
			continue
		}
		removed := len(list) - keep
		m.checkpoints[id] = append([]Checkpoint(nil), list[len(list)-keep:]...)
		stats.CheckpointsRemoved += removed
	}
	return stats, nil
}

func (m *Memory) Replay(_ context.Context, runID string) (Replay, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return Replay{}, notFoundRun()
	}
	replay := Replay{
		Run:         cloneRun(run),
		Attempts:    m.attemptsLocked(runID),
		Checkpoints: m.checkpointsLocked(runID),
		Outcomes:    m.outcomesLocked(runID),
		Events:      make([]contracts.RunEvent, 0, len(m.events[runID])),
	}
	for _, event := range m.events[runID] {
		replay.Events = append(replay.Events, cloneEvent(event))
	}
	if lease, ok := m.leases[runID]; ok {
		copied := lease
		replay.Lease = &copied
	}
	if cancel, ok := m.cancellations[runID]; ok {
		copied := cancel
		replay.Cancellation = &copied
	}
	return replay, nil
}

func (m *Memory) attemptsLocked(runID string) []Attempt {
	out := make([]Attempt, 0, len(m.attemptOrder[runID]))
	for _, id := range m.attemptOrder[runID] {
		out = append(out, cloneAttempt(m.attempts[id]))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		if out[i].AttemptNumber != out[j].AttemptNumber {
			return out[i].AttemptNumber < out[j].AttemptNumber
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *Memory) checkpointsLocked(runID string) []Checkpoint {
	out := make([]Checkpoint, 0, len(m.checkpoints[runID]))
	for _, cp := range m.checkpoints[runID] {
		out = append(out, cloneCheckpoint(cp))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out
}

func (m *Memory) outcomesLocked(runID string) []InvocationOutcome {
	out := []InvocationOutcome{}
	for _, outcome := range m.outcomes {
		if outcome.RunID == runID {
			out = append(out, cloneOutcome(outcome))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CompletedAt.Equal(out[j].CompletedAt) {
			return out[i].CompletedAt.Before(out[j].CompletedAt)
		}
		return out[i].InvocationID < out[j].InvocationID
	})
	return out
}

func (m *Memory) progressLocked(runID string) Progress {
	list := m.checkpoints[runID]
	if len(list) == 0 {
		return emptyProgress()
	}
	return decodeProgress(list[len(list)-1].Payload)
}

func (m *Memory) latestCheckpoint(runID string) Checkpoint {
	list := m.checkpoints[runID]
	if len(list) == 0 {
		return Checkpoint{}
	}
	return cloneCheckpoint(list[len(list)-1])
}

func (m *Memory) ownedAttempt(id, workerID string, token int64, now time.Time) (Attempt, error) {
	att, ok := m.attempts[id]
	if !ok {
		return Attempt{}, ErrNotFound
	}
	if err := m.checkLease(Lease{RunID: att.RunID, WorkerID: workerID, FencingToken: token}, now); err != nil {
		return Attempt{}, err
	}
	if att.WorkerID != workerID || att.FencingToken != token {
		return Attempt{}, ErrStaleLease
	}
	return att, nil
}

func (m *Memory) checkLease(lease Lease, now time.Time) error {
	current, ok := m.leases[lease.RunID]
	if !ok || current.FencingToken != lease.FencingToken || current.WorkerID != lease.WorkerID {
		return ErrStaleLease
	}
	if !current.ExpiresAt.After(now) {
		return ErrStaleLease
	}
	return nil
}

func (m *Memory) appendLocked(runID, typ, invocationID string, data any, now time.Time) error {
	if _, ok := m.runs[runID]; !ok {
		return notFoundRun()
	}
	var raw json.RawMessage
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		raw = encoded
	}
	var inv *string
	if invocationID != "" {
		id := invocationID
		inv = &id
	}
	event := contracts.RunEvent{
		Sequence:     uint64(len(m.events[runID]) + 1),
		RunID:        runID,
		InvocationID: inv,
		Type:         typ,
		Timestamp:    now.UTC(),
		Data:         raw,
		Source:       "nmc3",
	}
	m.events[runID] = append(m.events[runID], event)
	return nil
}

func (m *Memory) ensureRootLocked(runID string, now time.Time) {
	run := m.runs[runID]
	id := RootInvocationID(runID)
	for _, inv := range run.Invocations {
		if inv.ID == id {
			return
		}
	}
	run.Invocations = append(run.Invocations, contracts.AgentInvocation{
		ID: id, RunID: runID, AgentID: run.AgentID, AgentVersion: run.Version,
		Depth: 0, State: contracts.StatePending, Input: cloneRaw(run.Input),
	})
	m.runs[runID] = run
	_ = now
}

func (m *Memory) projectBegin(run *contracts.Run, att Attempt, now time.Time) {
	switch att.Kind {
	case KindSpecialist, KindFinish:
		parent := att.ParentInvocationID
		var parentPtr *string
		if parent != "" {
			parentPtr = &parent
		}
		depth := 0
		if att.Kind == KindSpecialist {
			depth = 1
		}
		upsertInvocation(run, contracts.AgentInvocation{
			ID: att.InvocationID, RunID: att.RunID, ParentID: parentPtr, AgentID: run.AgentID,
			AgentVersion: run.Version, Depth: depth, State: contracts.StateRunning,
			Input: cloneRaw(run.Input), StartedAt: timePtr(now),
		})
	case KindReadTool:
		caller := att.ParentInvocationID
		if caller == "" {
			caller = RootInvocationID(att.RunID)
		}
		input, _ := json.Marshal(map[string]string{"operation_id": att.OperationID})
		upsertTool(run, contracts.ToolInvocation{
			ID: att.ID, RunID: att.RunID, AgentInvocationID: caller, ToolID: att.StepID,
			ToolVersion: "v1", Input: input, StartedAt: now,
		})
	}
}

func (m *Memory) projectCommit(run *contracts.Run, att Attempt, output json.RawMessage, now time.Time) {
	switch att.Kind {
	case KindSpecialist, KindFinish:
		parent := att.ParentInvocationID
		var parentPtr *string
		if parent != "" {
			parentPtr = &parent
		}
		depth := 0
		if att.Kind == KindSpecialist {
			depth = 1
		}
		existing := findInvocation(run, att.InvocationID)
		started := timePtr(now)
		if existing.StartedAt != nil {
			started = timePtr(*existing.StartedAt)
		}
		upsertInvocation(run, contracts.AgentInvocation{
			ID: att.InvocationID, RunID: att.RunID, ParentID: parentPtr, AgentID: run.AgentID,
			AgentVersion: run.Version, Depth: depth, State: contracts.StateSucceeded,
			Input: cloneRaw(run.Input), Output: output, StartedAt: started, FinishedAt: timePtr(now),
		})
	case KindReadTool:
		m.finishTool(run, att, output, nil, now)
	}
}

func (m *Memory) finishTool(run *contracts.Run, att Attempt, output json.RawMessage, cause *contracts.RunError, now time.Time) {
	caller := att.ParentInvocationID
	if caller == "" {
		caller = RootInvocationID(att.RunID)
	}
	var existing contracts.ToolInvocation
	for _, tool := range run.ToolInvocations {
		if tool.ID == att.ID {
			existing = tool
			break
		}
	}
	if len(existing.Input) == 0 {
		existing.Input, _ = json.Marshal(map[string]string{"operation_id": att.OperationID})
	}
	if existing.StartedAt.IsZero() {
		existing.StartedAt = now
	}
	var errCopy *contracts.RunError
	if cause != nil {
		copied := *cause
		errCopy = &copied
	}
	upsertTool(run, contracts.ToolInvocation{
		ID: att.ID, RunID: att.RunID, AgentInvocationID: caller, ToolID: att.StepID, ToolVersion: "v1",
		Input: existing.Input, Output: cloneRaw(output), Error: errCopy, StartedAt: existing.StartedAt, FinishedAt: timePtr(now),
	})
}

func (m *Memory) setInvocationState(run *contracts.Run, id string, state contracts.State, now time.Time, finish bool) {
	for i := range run.Invocations {
		if run.Invocations[i].ID != id {
			continue
		}
		if run.Invocations[i].State == contracts.StateSucceeded {
			return
		}
		run.Invocations[i].State = state
		if run.Invocations[i].StartedAt == nil && state == contracts.StateRunning {
			run.Invocations[i].StartedAt = timePtr(now)
		}
		if finish {
			run.Invocations[i].FinishedAt = timePtr(now)
		}
		return
	}
}

func (m *Memory) cancelOpenInvocations(run *contracts.Run, now time.Time) {
	for i := range run.Invocations {
		state := run.Invocations[i].State
		if state == contracts.StateSucceeded || state == contracts.StateFailed || state == contracts.StateCancelled {
			continue
		}
		run.Invocations[i].State = contracts.StateCancelled
		run.Invocations[i].FinishedAt = timePtr(now)
	}
}

func (m *Memory) nextID(prefix string) string {
	m.seq++
	return prefix + "_" + itoa(m.seq)
}

func requestHash(run contracts.Run) string {
	sum := sha256.New()
	sum.Write([]byte(run.AgentID))
	sum.Write([]byte{0})
	sum.Write([]byte(run.Version))
	sum.Write([]byte{0})
	sum.Write([]byte(run.ModelID))
	sum.Write([]byte{0})
	sum.Write(run.Input)
	return hex.EncodeToString(sum.Sum(nil))
}

func notFoundRun() error {
	return &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
}

func requireWorker(workerID string, ttl time.Duration) error {
	if strings.TrimSpace(workerID) == "" {
		return invalid("worker ID is required")
	}
	if ttl <= 0 {
		return invalid("lease TTL must be positive")
	}
	return nil
}

func utc(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func timePtr(t time.Time) *time.Time {
	t = t.UTC()
	return &t
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func transition(from, to contracts.State) bool {
	switch from {
	case contracts.StatePending:
		return to == contracts.StatePending || to == contracts.StateRunning || to == contracts.StateFailed || to == contracts.StateCancelled
	case contracts.StateRunning:
		return to == contracts.StateRunning || to == contracts.StateSucceeded || to == contracts.StateFailed || to == contracts.StateCancelled
	default:
		return from == to
	}
}

func emptyProgress() Progress {
	return Progress{CompletedSteps: []string{}, Outputs: map[string]json.RawMessage{}}
}

func decodeProgress(raw json.RawMessage) Progress {
	progress := emptyProgress()
	if len(raw) == 0 {
		return progress
	}
	_ = json.Unmarshal(raw, &progress)
	if progress.CompletedSteps == nil {
		progress.CompletedSteps = []string{}
	}
	if progress.Outputs == nil {
		progress.Outputs = map[string]json.RawMessage{}
	}
	return progress
}

func upsertInvocation(run *contracts.Run, inv contracts.AgentInvocation) {
	for i := range run.Invocations {
		if run.Invocations[i].ID == inv.ID {
			run.Invocations[i] = inv
			return
		}
	}
	run.Invocations = append(run.Invocations, inv)
}

func upsertTool(run *contracts.Run, tool contracts.ToolInvocation) {
	for i := range run.ToolInvocations {
		if run.ToolInvocations[i].ID == tool.ID {
			run.ToolInvocations[i] = tool
			return
		}
	}
	run.ToolInvocations = append(run.ToolInvocations, tool)
}

func findInvocation(run *contracts.Run, id string) contracts.AgentInvocation {
	for _, inv := range run.Invocations {
		if inv.ID == id {
			return inv
		}
	}
	return contracts.AgentInvocation{}
}

func cloneRaw(in json.RawMessage) json.RawMessage {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneTime(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	t := in.UTC()
	return &t
}

func cloneError(in *contracts.RunError) *contracts.RunError {
	if in == nil {
		return nil
	}
	copied := *in
	return &copied
}

func cloneRun(run contracts.Run) contracts.Run {
	run.Input = cloneRaw(run.Input)
	run.Output = cloneRaw(run.Output)
	run.Error = cloneError(run.Error)
	invocations := make([]contracts.AgentInvocation, len(run.Invocations))
	for i, inv := range run.Invocations {
		inv.Input = cloneRaw(inv.Input)
		inv.Output = cloneRaw(inv.Output)
		inv.Error = cloneError(inv.Error)
		inv.StartedAt = cloneTime(inv.StartedAt)
		inv.FinishedAt = cloneTime(inv.FinishedAt)
		if inv.ParentID != nil {
			parent := *inv.ParentID
			inv.ParentID = &parent
		}
		invocations[i] = inv
	}
	run.Invocations = invocations
	tools := make([]contracts.ToolInvocation, len(run.ToolInvocations))
	for i, tool := range run.ToolInvocations {
		tool.Input = cloneRaw(tool.Input)
		tool.Output = cloneRaw(tool.Output)
		tool.Error = cloneError(tool.Error)
		tool.FinishedAt = cloneTime(tool.FinishedAt)
		tools[i] = tool
	}
	run.ToolInvocations = tools
	return run
}

func cloneEvent(event contracts.RunEvent) contracts.RunEvent {
	event.Data = cloneRaw(event.Data)
	if event.InvocationID != nil {
		id := *event.InvocationID
		event.InvocationID = &id
	}
	return event
}

func cloneAttempt(att Attempt) Attempt {
	att.Error = cloneError(att.Error)
	att.FinishedAt = cloneTime(att.FinishedAt)
	return att
}

func cloneOutcome(outcome InvocationOutcome) InvocationOutcome {
	outcome.Output = cloneRaw(outcome.Output)
	outcome.Error = cloneError(outcome.Error)
	return outcome
}

func cloneCheckpoint(cp Checkpoint) Checkpoint {
	cp.Payload = cloneRaw(cp.Payload)
	return cp
}

func cloneProgress(progress Progress) Progress {
	out := Progress{CompletedSteps: append([]string(nil), progress.CompletedSteps...), Outputs: map[string]json.RawMessage{}}
	for key, value := range progress.Outputs {
		out.Outputs[key] = cloneRaw(value)
	}
	return out
}
