package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Executor drives a plan against a Store. Run refuses an expired lease.
// Recover is the explicit takeover: it abandons expired attempts and then
// continues from the latest checkpoint. Neither path marks a run successful
// just because the previous worker stopped.
type Executor struct {
	Store           Store
	WorkerID        string
	LeaseTTL        time.Duration
	MaxReadAttempts int
	Now             func() time.Time
	// OnCheckpoint runs after a checkpoint is durable. A non-nil error stops
	// this worker. The run stays as recorded; a stop is not success.
	OnCheckpoint func(Checkpoint) error
}

func (e *Executor) Run(ctx context.Context, runID string, plan Plan, handler Handler) (contracts.Run, error) {
	if err := e.prepare(plan, handler); err != nil {
		return contracts.Run{}, err
	}
	run, err := e.Store.Get(ctx, runID)
	if err != nil {
		return contracts.Run{}, err
	}
	if terminal(run.State) {
		return run, nil
	}
	if blocked, err := e.blocked(ctx, runID, plan); err != nil {
		return run, err
	} else if blocked {
		return run, ErrUncertain
	}
	if cancelled, err := e.isCancelled(ctx, runID); err != nil {
		return run, err
	} else if cancelled {
		return run, ErrCancelled
	}
	lease, err := e.Store.ClaimRun(ctx, runID, e.WorkerID, e.leaseTTL(), e.now())
	if err != nil {
		return run, err
	}
	return e.drive(ctx, runID, plan, handler, lease)
}

// Recover abandons expired in-flight attempts, then resumes. A cancelled run
// is cleaned up and no new step runs. A successful or failed run is returned
// unchanged.
func (e *Executor) Recover(ctx context.Context, runID string, plan Plan, handler Handler) (contracts.Run, error) {
	if err := e.prepare(plan, handler); err != nil {
		return contracts.Run{}, err
	}
	run, err := e.Store.Get(ctx, runID)
	if err != nil {
		return contracts.Run{}, err
	}
	if run.State == contracts.StateSucceeded || run.State == contracts.StateFailed {
		return run, nil
	}
	lease, abandoned, err := e.Store.RecoverClaim(ctx, runID, e.WorkerID, e.leaseTTL(), e.now())
	if err != nil {
		return run, err
	}
	if err := e.Store.NoteRecovery(ctx, runID, e.WorkerID, abandoned, e.now()); err != nil {
		return run, err
	}
	run, err = e.Store.Get(ctx, runID)
	if err != nil {
		return contracts.Run{}, err
	}
	if run.State == contracts.StateCancelled {
		e.release(lease)
		return run, nil
	}
	if cancelled, err := e.isCancelled(ctx, runID); err != nil {
		return run, err
	} else if cancelled {
		e.release(lease)
		return run, nil
	}
	if blocked, err := e.blocked(ctx, runID, plan); err != nil {
		return run, err
	} else if blocked {
		e.release(lease)
		return run, ErrUncertain
	}
	return e.drive(ctx, runID, plan, handler, lease)
}

func (e *Executor) Cancel(ctx context.Context, runID, reason string) (Cancellation, error) {
	if e.Store == nil {
		return Cancellation{}, invalid("store is required")
	}
	return e.Store.Cancel(ctx, runID, reason, e.now())
}

func (e *Executor) Replay(ctx context.Context, runID string) (Replay, error) {
	if e.Store == nil {
		return Replay{}, invalid("store is required")
	}
	return e.Store.Replay(ctx, runID)
}

func (e *Executor) drive(ctx context.Context, runID string, plan Plan, handler Handler, lease Lease) (contracts.Run, error) {
	keep := false
	defer func() {
		if !keep {
			e.release(lease)
		}
	}()
	progress, _, _, err := e.Store.LatestProgress(ctx, runID)
	if err != nil {
		return contracts.Run{}, err
	}
	if progress.Outputs == nil {
		progress.Outputs = map[string]json.RawMessage{}
	}
	for _, step := range plan.Steps {
		step = normalizeStep(runID, step)
		if err := ctx.Err(); err != nil {
			keep = true
			return e.current(ctx, runID, err)
		}
		if cancelled, err := e.isCancelled(ctx, runID); err != nil {
			return contracts.Run{}, err
		} else if cancelled {
			return e.current(ctx, runID, ErrCancelled)
		}
		renewed, err := e.Store.RenewLease(ctx, lease, e.leaseTTL(), e.now())
		if err != nil {
			keep = true
			return e.current(ctx, runID, err)
		}
		lease = renewed
		if containsStep(progress.CompletedSteps, step.ID) {
			continue
		}
		if step.InvocationID != "" {
			outcome, ok, err := e.Store.Invocation(ctx, step.InvocationID)
			if err != nil {
				return contracts.Run{}, err
			}
			if ok && outcome.State == contracts.StateSucceeded {
				progress.Outputs[step.InvocationID] = cloneRaw(outcome.Output)
				if !containsStep(progress.CompletedSteps, step.ID) {
					progress.CompletedSteps = append(progress.CompletedSteps, step.ID)
				}
				continue
			}
		}
		var cp Checkpoint
		progress, cp, err = e.runStep(ctx, lease, step, progress, handler)
		if err != nil {
			if isStop(err) || errors.Is(err, ErrStaleLease) {
				keep = true
			}
			return e.current(ctx, runID, err)
		}
		if e.OnCheckpoint != nil && cp.Sequence != 0 {
			if hookErr := e.OnCheckpoint(cp); hookErr != nil {
				run, getErr := e.Store.Get(ctx, runID)
				if getErr != nil {
					keep = true
					return contracts.Run{}, hookErr
				}
				if run.State == contracts.StatePending || run.State == contracts.StateRunning {
					keep = true
				}
				return run, fmt.Errorf("%w: %v", ErrWorkerStopped, hookErr)
			}
		}
	}
	return e.Store.Get(ctx, runID)
}

func (e *Executor) runStep(ctx context.Context, lease Lease, step Step, progress Progress, handler Handler) (Progress, Checkpoint, error) {
	if step.Kind == KindWrite || step.SideEffect == SideEffectWrite {
		if err := e.markProtected(ctx, lease, step); err != nil {
			return progress, Checkpoint{}, err
		}
		return progress, Checkpoint{}, ErrProtectedWrite
	}
	for n := 1; ; n++ {
		now := e.now()
		att, err := e.Store.BeginAttempt(ctx, BeginRequest{
			RunID: lease.RunID, Step: step, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Now: now,
		})
		if err != nil {
			return progress, Checkpoint{}, err
		}
		out, err := handler.Execute(ctx, step, cloneProgress(progress))
		now = e.now()
		if err == nil {
			if !json.Valid(out) {
				cause := contracts.RunError{Code: contracts.ErrOutputSchemaInvalid, Message: "handler output is not json"}
				if _, ferr := e.Store.FailAttempt(ctx, FailRequest{AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Error: cause, Now: now}); ferr != nil {
					return progress, Checkpoint{}, ferr
				}
				if rerr := e.Store.FailRun(ctx, lease.RunID, cause, lease, now); rerr != nil {
					return progress, Checkpoint{}, rerr
				}
				return progress, Checkpoint{}, invalid("handler output is not json")
			}
			cp, cerr := e.Store.CommitStep(ctx, CommitRequest{
				AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Output: out, Now: now,
			})
			if errors.Is(cerr, ErrAlreadyCompleted) {
				if outcome, ok, oerr := e.Store.Invocation(ctx, step.InvocationID); oerr != nil {
					return progress, Checkpoint{}, oerr
				} else if ok {
					progress.Outputs[step.InvocationID] = cloneRaw(outcome.Output)
				}
				if !containsStep(progress.CompletedSteps, step.ID) {
					progress.CompletedSteps = append(progress.CompletedSteps, step.ID)
				}
				return progress, Checkpoint{}, nil
			}
			if cerr != nil {
				return progress, Checkpoint{}, cerr
			}
			return decodeProgress(cp.Payload), cp, nil
		}
		if isStop(err) {
			return progress, Checkpoint{}, err
		}
		if errors.Is(err, ErrOutcomeUnknown) {
			if _, uerr := e.Store.MarkUncertain(ctx, UncertainRequest{
				AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken,
				Reason: "operation outcome is unknown", Now: now,
			}); uerr != nil {
				return progress, Checkpoint{}, uerr
			}
			return progress, Checkpoint{}, ErrUncertain
		}
		cause := contracts.RunError{Code: contracts.ErrInternal, Message: err.Error()}
		if errors.Is(err, ErrLostResponse) {
			cause = contracts.RunError{Code: contracts.ErrTimeout, Message: "lost response"}
		}
		if _, ferr := e.Store.FailAttempt(ctx, FailRequest{
			AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Error: cause, Now: now,
		}); ferr != nil {
			return progress, Checkpoint{}, ferr
		}
		if errors.Is(err, ErrLostResponse) && step.SideEffect == SideEffectReadOnly && n < e.maxReads() {
			continue
		}
		if errors.Is(err, ErrLostResponse) && step.SideEffect == SideEffectReadOnly {
			cause = contracts.RunError{Code: contracts.ErrTimeout, Message: "read retries exhausted"}
			if rerr := e.Store.FailRun(ctx, lease.RunID, cause, lease, now); rerr != nil {
				return progress, Checkpoint{}, rerr
			}
			return progress, Checkpoint{}, ErrRetriesExhausted
		}
		if rerr := e.Store.FailRun(ctx, lease.RunID, cause, lease, now); rerr != nil {
			return progress, Checkpoint{}, rerr
		}
		return progress, Checkpoint{}, err
	}
}

func (e *Executor) markProtected(ctx context.Context, lease Lease, step Step) error {
	now := e.now()
	att, err := e.Store.BeginAttempt(ctx, BeginRequest{
		RunID: lease.RunID, Step: step, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Now: now,
	})
	if err != nil {
		return err
	}
	_, err = e.Store.MarkUncertain(ctx, UncertainRequest{
		AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken,
		Reason: "protected writes are not dispatched before Pass 4", Now: now,
	})
	return err
}

func (e *Executor) prepare(plan Plan, handler Handler) error {
	if e.Store == nil {
		return invalid("store is required")
	}
	if strings.TrimSpace(e.WorkerID) == "" {
		return invalid("worker ID is required")
	}
	if handler == nil {
		return invalid("handler is required")
	}
	return validatePlan(plan)
}

func (e *Executor) blocked(ctx context.Context, runID string, plan Plan) (bool, error) {
	attempts, err := e.Store.Attempts(ctx, runID)
	if err != nil {
		return false, err
	}
	for _, step := range plan.Steps {
		op := normalizeStep(runID, step).OperationID
		for _, att := range attempts {
			if att.OperationID == op && att.State == AttemptUncertain {
				return true, nil
			}
		}
	}
	return false, nil
}

func (e *Executor) isCancelled(ctx context.Context, runID string) (bool, error) {
	if _, ok, err := e.Store.Cancellation(ctx, runID); err != nil || ok {
		return ok, err
	}
	run, err := e.Store.Get(ctx, runID)
	if err != nil {
		return false, err
	}
	return run.State == contracts.StateCancelled, nil
}

func (e *Executor) current(ctx context.Context, runID string, cause error) (contracts.Run, error) {
	run, err := e.Store.Get(ctx, runID)
	if err != nil {
		return contracts.Run{}, cause
	}
	return run, cause
}

func (e *Executor) release(lease Lease) {
	if lease.FencingToken == 0 {
		return
	}
	_ = e.Store.ReleaseLease(context.Background(), lease, e.now())
}

func (e *Executor) now() time.Time {
	if e.Now == nil {
		return time.Now().UTC()
	}
	return e.Now().UTC()
}

func (e *Executor) leaseTTL() time.Duration {
	if e.LeaseTTL <= 0 {
		return 30 * time.Second
	}
	return e.LeaseTTL
}

func (e *Executor) maxReads() int {
	if e.MaxReadAttempts <= 0 {
		return 2
	}
	return e.MaxReadAttempts
}

func normalizeStep(runID string, step Step) Step {
	if step.ParentInvocationID == "" && (step.Kind == KindSpecialist || step.Kind == KindReadTool) {
		step.ParentInvocationID = RootInvocationID(runID)
	}
	if step.Kind == KindFinish && step.InvocationID == "" {
		step.InvocationID = RootInvocationID(runID)
	}
	step.OperationID = step.operationID()
	return step
}

func terminal(state contracts.State) bool {
	return state == contracts.StateSucceeded || state == contracts.StateFailed || state == contracts.StateCancelled
}

func isStop(err error) bool {
	return errors.Is(err, ErrWorkerStopped) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
