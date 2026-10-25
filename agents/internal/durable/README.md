# Durable runs and recovery

Pass 3 records enough state to resume a run after a worker stops, without completing the same logical invocation twice. This package is the durable execution adapter. It does not replace the agent loop, and it does not dispatch protected writes.

## Backend decision

The durable store is **Postgres in the existing `nmc3` schema**, not a separate workflow engine. Run snapshots and the ordered event log stay on the Pass 1 `RunStore` / `EventSink` interfaces. Attempts, checkpoints, leases, invocation outcomes, cancellation, and run-creation idempotency are new tables in [`agents/db/migrations/202610240005_nmc3_durable.sql`](../../db/migrations/202610240005_nmc3_durable.sql).

`Memory` is the reference journal those tables describe. `Postgres`, opened with `OpenPostgres`, is the same journal on those tables. Tests restart work by constructing a second `Executor` on the same journal after the lease expires. The worker process is gone, the journal is not, and the new worker continues from recorded checkpoints. `TestPostgresDurableRestartProof` runs that proof against disposable Postgres 16. It applies migrations `202610240001`, `202610240003`, and `202610240005` in that order. Parent invocation links are read back from the run snapshot, so no migration after 005 is required. Hosted Supabase already has 005; this package does not connect to it.

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

Events are append-only. The journal assigns the next per-run sequence under the same lock as the durable write. Postgres uses `nmc3.runs.next_event_sequence`. Readers order by that sequence. Recovery appends `ATTEMPT_ABANDONED` and `RECOVERY_STARTED`. It does not rewrite earlier events.

## Exported API

| Symbol | Role |
| --- | --- |
| `Memory`, `NewMemory` | Reference journal. Implements `contracts.RunStore`, `contracts.EventSink`, and `Store` |
| `Postgres`, `OpenPostgres`, `Close` | Postgres journal on the existing `nmc3` tables. `OpenPostgres` is the constructor the HTTP process calls. It does not replace the Supabase run store |
| `Store` | Run/event interfaces plus admit, lease, attempt, checkpoint, cancel, retention, and replay |
| `Executor` | `Run`, `Recover`, `Cancel`, `Replay` |
| `AdmitRequest` | Idempotent run creation |
| `Plan`, `Step`, `TwoSpecialistPlan`, `ReadToolPlan`, `RootInvocationID` | Worker plans. Two specialists then finish is the restart scenario |
| `Handler`, `HandlerFunc` | Step body. Return `ErrLostResponse`, `ErrOutcomeUnknown`, or `ErrWorkerStopped` for those cases |
| `Attempt`, `Checkpoint`, `Lease`, `Cancellation`, `InvocationOutcome`, `Progress`, `Replay` | Inspectable records |
| `ApplyRetention`, `RetentionPolicy` | Checkpoint compaction |
| `ErrLeaseHeld`, `ErrNeedsRecovery`, `ErrStaleLease`, `ErrAttemptClaimed`, `ErrAlreadyCompleted`, `ErrUncertain`, `ErrProtectedWrite`, `ErrRetriesExhausted`, `ErrCancelled` | Control-flow errors |

`agents/internal/store` is unchanged. `Memory` remains the in-process stand-in. `OpenPostgres` is the journal constructor; wiring it into the HTTP process belongs to the integrator. The snippet below is not in `cmd/nmc3-agent/main.go`. Until that integrator wires it, the API process still will not resume in-flight OpenCode sessions.

## HTTP process wiring

Keep the existing `runs` / `events` selection. The default remains `store.NewSupabase`. `NMC3_STORE=postgres` still uses `store.Open`. Do not assign the durable journal to `runs` or `events`.

Add this beside that selection. `DATABASE_URL` is the direct Postgres connection that already has migrations 001, 003, and 005. It is not the Supabase secret key.

```go
if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
    journalCtx, stopJournal := context.WithTimeout(context.Background(), 15*time.Second)
    defer stopJournal()
    journal, err := durable.OpenPostgres(journalCtx, databaseURL)
    if err != nil {
        return fmt.Errorf("connect durable journal: %w", err)
    }
    defer journal.Close()
    executor := &durable.Executor{
        Store:           journal,
        WorkerID:        envDefault("NMC3_WORKER_ID", "nmc3-api"),
        LeaseTTL:        30 * time.Second,
        MaxReadAttempts: 2,
    }
    // executor.Recover(ctx, runID, plan, handler) resumes a durable plan
    // from this journal after a lease expires. It does not resume an
    // in-flight OpenCode session, and it does not replace runs or events.
    _ = executor
}
```

The import is `github.com/khangdoan514/hacktx-2026/agents/internal/durable`. `envDefault` is the helper already in `main.go`.

## Not in this pass

Protected writes, approvals, tenant enforcement, and the OpenCode session loop are out of scope. A `WRITE` step is refused and stored as uncertain. The HTTP process is not wired to this executor, so an API restart still does not resume an in-flight OpenCode session.
