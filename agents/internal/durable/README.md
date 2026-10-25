# Durable runs and recovery

Pass 3 records enough state to resume a run after a worker stops, without completing the same logical invocation twice. This package is the durable execution adapter. It does not replace the agent loop, and it does not dispatch protected writes.

## Backend decision

The durable store is **Postgres in the existing `nmc3` schema**, not a separate workflow engine. Run snapshots and the ordered event log stay on the Pass 1 `RunStore` / `EventSink` interfaces. Attempts, checkpoints, leases, invocation outcomes, cancellation, and run-creation idempotency are new tables in [`agents/db/migrations/202610240005_nmc3_durable.sql`](../../db/migrations/202610240005_nmc3_durable.sql).

`Memory` is the reference journal those tables describe. Tests restart work by constructing a second `Executor` on the same `Memory` after the lease expires. That is the restart proof for this pass: the worker process is gone, the journal is not, and the new worker continues from recorded checkpoints. A live Supabase database was not attached, so the SQL migration has not been executed. Shipping a Postgres client before that proof would only duplicate the journal without testing it.

Temporal was not selected. Pass 3 needs attempt and checkpoint rows that operators can inspect next to the run, in the same database that already holds run history. A second engine would split the transaction that commits an outcome, a checkpoint, and the events for that step.

## What a worker does

1. `Admit` inserts a run once per tenant and idempotency key. The same key and the same agent, version, model, and input return the original run. A different payload is `ErrIdempotencyConflict`. The default tenant is `local` until Pass 4.
2. `Run` claims a live lease. An expired lease returns `ErrNeedsRecovery` and does not execute steps.
3. Each unfinished step starts an attempt. A handler result is committed with the lease's fencing token. The commit writes the attempt, the invocation outcome, the cumulative checkpoint, and the events under one lock.
4. `Recover` is the only takeover. It marks expired in-flight attempts `ABANDONED` (or `CANCELLED` when a cancellation record exists) and then resumes. Abandoning an attempt does not mark the run or the invocation successful.
5. A completed invocation rejects another completion. A second worker's commit with an old fencing token fails with `ErrStaleLease`.

`Run` on a run that is already succeeded, failed, or cancelled does not call the handler. `RunStore.Update` is rejected while a lease row exists, so a snapshot write cannot clobber the worker. A succeeded run rejects a later snapshot that differs from the stored one.

## Attempts, retries, and uncertain operations

| Situation | What is recorded | What happens next |
| --- | --- | --- |
| Safe step finishes | Attempt `SUCCEEDED`, one invocation outcome, one checkpoint | Later workers skip it |
| Worker stops mid-step | Attempt stays `RUNNING` until recovery, then `ABANDONED` | A new attempt may run. The abandoned attempt is not success |
| Read-only tool loses its response (`ErrLostResponse`) | Failed attempt, then another attempt up to `MaxReadAttempts` (default 2) | The successful try is the only invocation outcome |
| Read retries are exhausted | Run `FAILED` | Recovery does not try again |
| Outcome is unknown (`ErrOutcomeUnknown`) | Attempt `UNCERTAIN`. Run stays unfinished | No automatic retry |
| Step is `WRITE` | Attempt `UNCERTAIN` with side effect `WRITE`. The handler is not called | Pass 4 has to add approval and reconciliation before any dispatch |

`READ_ONLY` is the only side effect that is retried. Specialist steps are re-run only when they have no invocation outcome, which is the crash case above. They are not retried after a normal handler error.

## Cancellation

`Cancel` stores one cancellation row, sets the run to `CANCELLED`, and appends one `CANCELLATION_REQUESTED` event. Calling it again returns the original record. A worker checks the record before every new step. An action that already started may still commit; the next step does not start. After the lease expires, `Recover` sees the record, does not call the handler, and leaves the run cancelled.

## Checkpoints and retention

The checkpoint payload is cumulative: completed step IDs and their outputs. The latest row is enough to resume. `ApplyRetention` deletes older checkpoint rows for terminal runs past `TerminalHorizon`, keeping at least the latest row. It does not delete attempt history, invocation outcomes, cancellation records, idempotency keys, or anything for a run that is still pending or running. Uncertain attempts stay.

## Events

Events are append-only. `Memory` assigns the next per-run sequence under the same lock as the durable write, and readers order by that sequence. Recovery appends `ATTEMPT_ABANDONED` and `RECOVERY_STARTED`. It does not rewrite earlier events.

## Exported API

| Symbol | Role |
| --- | --- |
| `Memory`, `NewMemory` | Reference journal. Implements `contracts.RunStore`, `contracts.EventSink`, and `Store` |
| `Store` | Run/event interfaces plus admit, lease, attempt, checkpoint, cancel, retention, and replay |
| `Executor` | `Run`, `Recover`, `Cancel`, `Replay` |
| `AdmitRequest` | Idempotent run creation |
| `Plan`, `Step`, `TwoSpecialistPlan`, `ReadToolPlan`, `RootInvocationID` | Worker plans. Two specialists then finish is the restart scenario |
| `Handler`, `HandlerFunc` | Step body. Return `ErrLostResponse`, `ErrOutcomeUnknown`, or `ErrWorkerStopped` for those cases |
| `Attempt`, `Checkpoint`, `Lease`, `Cancellation`, `InvocationOutcome`, `Progress`, `Replay` | Inspectable records |
| `ApplyRetention`, `RetentionPolicy` | Checkpoint compaction |
| `ErrLeaseHeld`, `ErrNeedsRecovery`, `ErrStaleLease`, `ErrAttemptClaimed`, `ErrAlreadyCompleted`, `ErrUncertain`, `ErrProtectedWrite`, `ErrRetriesExhausted`, `ErrCancelled` | Control-flow errors |

`agents/internal/store` is unchanged. `Memory` is the stand-in that satisfies the same run and event interfaces. Wiring this executor into the HTTP process, and applying the migration, belongs to the integrator.

## Not in this pass

Protected writes, approvals, tenant enforcement, and the OpenCode session loop are out of scope. A `WRITE` step is refused and stored as uncertain. There is no Postgres driver here yet; the migration is the schema contract for that adapter.
