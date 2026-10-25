package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

const (
	EventRunAdmitted           = "DURABLE_RUN_ADMITTED"
	EventAttemptStarted        = "ATTEMPT_STARTED"
	EventAttemptSucceeded      = "ATTEMPT_SUCCEEDED"
	EventAttemptFailed         = "ATTEMPT_FAILED"
	EventAttemptAbandoned      = "ATTEMPT_ABANDONED"
	EventAttemptUncertain      = "ATTEMPT_UNCERTAIN"
	EventAttemptCancelled      = "ATTEMPT_CANCELLED"
	EventInvocationCompleted   = "INVOCATION_COMPLETED"
	EventCheckpointSaved       = "CHECKPOINT_SAVED"
	EventCancellationRequested = "CANCELLATION_REQUESTED"
	EventRecoveryStarted       = "RECOVERY_STARTED"
	EventRunCompleted          = "RUN_COMPLETED"
	EventRunFailed             = "RUN_FAILED"
)

type AttemptState string

const (
	AttemptClaimed   AttemptState = "CLAIMED"
	AttemptRunning   AttemptState = "RUNNING"
	AttemptSucceeded AttemptState = "SUCCEEDED"
	AttemptFailed    AttemptState = "FAILED"
	AttemptCancelled AttemptState = "CANCELLED"
	AttemptUncertain AttemptState = "UNCERTAIN"
	AttemptAbandoned AttemptState = "ABANDONED"
)

type SideEffect string

const (
	SideEffectNone     SideEffect = "NONE"
	SideEffectReadOnly SideEffect = "READ_ONLY"
	SideEffectWrite    SideEffect = "WRITE"
)

type StepKind string

const (
	KindSpecialist StepKind = "specialist"
	KindReadTool   StepKind = "read_tool"
	KindFinish     StepKind = "finish"
	KindWrite      StepKind = "write"
)

// Step is one logical operation in a durable plan. OperationID is the retry
// identity. InvocationID is the logical invocation that can complete once.
type Step struct {
	ID                 string
	Kind               StepKind
	InvocationID       string
	OperationID        string
	SideEffect         SideEffect
	ParentInvocationID string
}

func (s Step) operationID() string {
	if s.OperationID != "" {
		return s.OperationID
	}
	if s.InvocationID != "" {
		return s.InvocationID
	}
	return s.ID
}

// Plan is the ordered work a worker runs. Finish, when present, is last.
type Plan struct {
	Steps []Step
}

// Progress is the cumulative checkpoint payload. The latest checkpoint is
// enough to skip completed steps after a restart.
type Progress struct {
	CompletedSteps []string                   `json:"completed_steps"`
	Outputs        map[string]json.RawMessage `json:"outputs"`
}

// Attempt is one try of a logical operation. Retries get new attempt numbers.
type Attempt struct {
	ID                 string
	RunID              string
	OperationID        string
	AttemptNumber      int
	WorkerID           string
	State              AttemptState
	Kind               StepKind
	SideEffect         SideEffect
	StepID             string
	InvocationID       string
	ParentInvocationID string
	FencingToken       int64
	Error              *contracts.RunError
	StartedAt          time.Time
	FinishedAt         *time.Time
	LeaseExpiresAt     time.Time
}

// InvocationOutcome is the single terminal result of a logical invocation.
type InvocationOutcome struct {
	InvocationID string
	RunID        string
	State        contracts.State
	Output       json.RawMessage
	Error        *contracts.RunError
	AttemptID    string
	CompletedAt  time.Time
}

// Checkpoint is a durable resume point. Sequence is monotonic per run and is
// not reused when older rows are retained away.
type Checkpoint struct {
	RunID        string
	Sequence     int64
	StepID       string
	InvocationID string
	AttemptID    string
	Payload      json.RawMessage
	CreatedAt    time.Time
}

// Lease is a worker's exclusive ownership of a run. FencingToken increases on
// every new claim so a stopped worker cannot commit after a takeover.
type Lease struct {
	RunID        string
	WorkerID     string
	FencingToken int64
	ExpiresAt    time.Time
}

// Cancellation is durable. A restarted worker observes it and does not start
// new actions.
type Cancellation struct {
	RunID       string
	Reason      string
	RequestedAt time.Time
}

// AdmitRequest creates a run once per tenant idempotency key.
// The hash covers agent ID, version, model ID, and input.
type AdmitRequest struct {
	TenantID       string
	IdempotencyKey string
	Run            contracts.Run
	Now            time.Time
}

type BeginRequest struct {
	RunID        string
	Step         Step
	WorkerID     string
	FencingToken int64
	Now          time.Time
}

type FailRequest struct {
	AttemptID    string
	WorkerID     string
	FencingToken int64
	Error        contracts.RunError
	Now          time.Time
}

type UncertainRequest struct {
	AttemptID    string
	WorkerID     string
	FencingToken int64
	Reason       string
	Now          time.Time
}

type CommitRequest struct {
	AttemptID    string
	WorkerID     string
	FencingToken int64
	Output       json.RawMessage
	Now          time.Time
}

// RetentionPolicy compacts intermediate checkpoints for terminal runs.
// Attempt rows, outcomes, cancellations, and idempotency keys stay.
type RetentionPolicy struct {
	TerminalHorizon       time.Duration
	KeepLatestCheckpoints int
}

type RetentionStats struct {
	CheckpointsRemoved int
}

// Replay is the inspectable history of one run, in durable order.
type Replay struct {
	Run          contracts.Run
	Lease        *Lease
	Cancellation *Cancellation
	Attempts     []Attempt
	Checkpoints  []Checkpoint
	Outcomes     []InvocationOutcome
	Events       []contracts.RunEvent
}

// Handler executes a step that is not already complete. Executors call it at
// least once for an unfinished safe step and never for a completed invocation.
type Handler interface {
	Execute(ctx context.Context, step Step, progress Progress) (json.RawMessage, error)
}

type HandlerFunc func(context.Context, Step, Progress) (json.RawMessage, error)

func (f HandlerFunc) Execute(ctx context.Context, step Step, progress Progress) (json.RawMessage, error) {
	return f(ctx, step, progress)
}

// Store is the journal plus the Pass 1 run and event interfaces.
// *Memory is the reference implementation. It does not wrap store.Postgres;
// an integrator can project snapshots through the same interfaces.
type Store interface {
	contracts.RunStore
	contracts.EventSink

	Admit(ctx context.Context, req AdmitRequest) (contracts.Run, bool, error)
	Cancel(ctx context.Context, runID, reason string, now time.Time) (Cancellation, error)
	Cancellation(ctx context.Context, runID string) (Cancellation, bool, error)
	ClaimRun(ctx context.Context, runID, workerID string, ttl time.Duration, now time.Time) (Lease, error)
	RenewLease(ctx context.Context, lease Lease, ttl time.Duration, now time.Time) (Lease, error)
	ReleaseLease(ctx context.Context, lease Lease, now time.Time) error
	RecoverClaim(ctx context.Context, runID, workerID string, ttl time.Duration, now time.Time) (Lease, []Attempt, error)
	BeginAttempt(ctx context.Context, req BeginRequest) (Attempt, error)
	FailAttempt(ctx context.Context, req FailRequest) (Attempt, error)
	MarkUncertain(ctx context.Context, req UncertainRequest) (Attempt, error)
	CommitStep(ctx context.Context, req CommitRequest) (Checkpoint, error)
	FailRun(ctx context.Context, runID string, cause contracts.RunError, lease Lease, now time.Time) error
	LatestProgress(ctx context.Context, runID string) (Progress, Checkpoint, bool, error)
	Invocation(ctx context.Context, invocationID string) (InvocationOutcome, bool, error)
	Attempts(ctx context.Context, runID string) ([]Attempt, error)
	Checkpoints(ctx context.Context, runID string) ([]Checkpoint, error)
	Outcomes(ctx context.Context, runID string) ([]InvocationOutcome, error)
	CurrentLease(ctx context.Context, runID string) (Lease, bool, error)
	NoteRecovery(ctx context.Context, runID, workerID string, abandoned []Attempt, now time.Time) error
	ApplyRetention(ctx context.Context, policy RetentionPolicy, now time.Time) (RetentionStats, error)
	Replay(ctx context.Context, runID string) (Replay, error)
}

// RootInvocationID is the depth-0 invocation created for every admitted run.
func RootInvocationID(runID string) string { return runID + ":root" }

// TwoSpecialistPlan is the Pass 3 restart scenario: two specialists, then finish.
func TwoSpecialistPlan(runID string) Plan {
	root := RootInvocationID(runID)
	return Plan{Steps: []Step{
		{ID: "specialist-a", Kind: KindSpecialist, InvocationID: runID + ":specialist-a", SideEffect: SideEffectNone, ParentInvocationID: root},
		{ID: "specialist-b", Kind: KindSpecialist, InvocationID: runID + ":specialist-b", SideEffect: SideEffectNone, ParentInvocationID: root},
		{ID: "finish", Kind: KindFinish, InvocationID: root, SideEffect: SideEffectNone},
	}}
}

// ReadToolPlan is a safe read followed by finish. Lost responses retry on the
// read step only.
func ReadToolPlan(runID, toolID string) Plan {
	root := RootInvocationID(runID)
	return Plan{Steps: []Step{
		{ID: toolID, Kind: KindReadTool, InvocationID: runID + ":tool:" + toolID, SideEffect: SideEffectReadOnly, ParentInvocationID: root},
		{ID: "finish", Kind: KindFinish, InvocationID: root, SideEffect: SideEffectNone},
	}}
}

func validatePlan(plan Plan) error {
	if len(plan.Steps) == 0 {
		return invalid("plan has no steps")
	}
	seenStep := map[string]struct{}{}
	seenOp := map[string]struct{}{}
	seenInv := map[string]struct{}{}
	finish := 0
	for i, step := range plan.Steps {
		if step.ID == "" {
			return invalid("step id is required")
		}
		if _, ok := seenStep[step.ID]; ok {
			return invalid("duplicate step id " + step.ID)
		}
		seenStep[step.ID] = struct{}{}
		op := step.operationID()
		if _, ok := seenOp[op]; ok {
			return invalid("duplicate operation " + op)
		}
		seenOp[op] = struct{}{}
		switch step.Kind {
		case KindSpecialist:
			if step.SideEffect == SideEffectWrite {
				return invalid("specialist cannot be a protected write")
			}
			if step.InvocationID == "" {
				return invalid("specialist invocation id is required")
			}
		case KindReadTool:
			if step.SideEffect != SideEffectReadOnly {
				return invalid("read tool must be READ_ONLY")
			}
			if step.InvocationID == "" {
				return invalid("read invocation id is required")
			}
		case KindFinish:
			finish++
			if step.SideEffect == SideEffectWrite {
				return invalid("finish cannot be a protected write")
			}
			if step.InvocationID == "" {
				return invalid("finish invocation id is required")
			}
			if i != len(plan.Steps)-1 {
				return invalid("finish must be the last step")
			}
		case KindWrite:
			if step.SideEffect != SideEffectWrite {
				return invalid("write step must have side effect WRITE")
			}
			if step.operationID() == "" {
				return invalid("write operation id is required")
			}
		default:
			return invalid("unknown step kind")
		}
		if step.InvocationID != "" {
			if _, ok := seenInv[step.InvocationID]; ok {
				return invalid("duplicate invocation " + step.InvocationID)
			}
			seenInv[step.InvocationID] = struct{}{}
		}
	}
	if finish > 1 {
		return invalid("plan has more than one finish step")
	}
	return nil
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalid, message)
}

func containsStep(steps []string, id string) bool {
	for _, step := range steps {
		if step == id {
			return true
		}
	}
	return false
}
