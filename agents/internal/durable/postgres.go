package durable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// journalMem lends snapshot projection helpers. Those methods only edit the
// run value they are given; they do not read or write Memory maps.
var journalMem Memory

// Postgres is the durable journal on the existing nmc3 tables.
// Open it with OpenPostgres and pass it to Executor. It implements Store.
// It does not replace the Supabase run store.
type Postgres struct {
	pool *pgxpool.Pool
}

var (
	_ Store               = (*Postgres)(nil)
	_ contracts.RunStore  = (*Postgres)(nil)
	_ contracts.EventSink = (*Postgres)(nil)
)

// OpenPostgres opens the journal the HTTP process should pass to Executor.
// databaseURL is a direct Postgres connection string (DATABASE_URL). The
// database must already have migrations 202610240001, 202610240003, and
// 202610240005 applied. This constructor does not replace store.NewSupabase
// or store.Open.
func OpenPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, invalid("database URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("durable: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("durable: ping: %w", err)
	}
	var ready bool
	err = pool.QueryRow(ctx, `
		select to_regclass('nmc3.runs') is not null
		  and to_regclass('nmc3.agent_invocations') is not null
		  and to_regclass('nmc3.tool_invocations') is not null
		  and to_regclass('nmc3.run_events') is not null
		  and to_regclass('nmc3.attempts') is not null
		  and to_regclass('nmc3.invocation_outcomes') is not null
		  and to_regclass('nmc3.checkpoints') is not null
		  and to_regclass('nmc3.worker_leases') is not null
		  and to_regclass('nmc3.run_cancellations') is not null
		  and to_regclass('nmc3.run_idempotency') is not null
		  and exists (
		    select 1 from information_schema.columns
		    where table_schema = 'nmc3' and table_name = 'run_events' and column_name = 'source'
		  )`).Scan(&ready)
	if err != nil || !ready {
		pool.Close()
		if err != nil {
			return nil, fmt.Errorf("durable: schema check: %w", err)
		}
		return nil, invalid("nmc3 durable schema is missing; apply 202610240001, 202610240003, and 202610240005")
	}
	return &Postgres{pool: pool}, nil
}

// Close releases the connection pool.
func (s *Postgres) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Postgres) Admit(ctx context.Context, req AdmitRequest) (contracts.Run, bool, error) {
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
	now := pgTime(req.Now)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return contracts.Run{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1), hashtext($2))`, tenant, key); err != nil {
		return contracts.Run{}, false, mapWriteErr(err)
	}
	var existingRun, existingHash string
	err = tx.QueryRow(ctx, `select run_id, request_hash from nmc3.run_idempotency where tenant_id=$1 and idempotency_key=$2 for update`, tenant, key).Scan(&existingRun, &existingHash)
	if err == nil {
		if existingHash != hash {
			return contracts.Run{}, false, ErrIdempotencyConflict
		}
		run, err := lockRun(ctx, tx, existingRun)
		if err != nil {
			return contracts.Run{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return contracts.Run{}, false, err
		}
		return cloneRun(run), false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return contracts.Run{}, false, mapWriteErr(err)
	}
	run := req.Run
	run.State = contracts.StatePending
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	if err := s.insertRun(ctx, tx, &run); err != nil {
		return contracts.Run{}, false, err
	}
	if _, err := s.appendEvent(ctx, tx, run.ID, EventRunAdmitted, "", eventBody{State: string(contracts.StatePending)}, now); err != nil {
		return contracts.Run{}, false, err
	}
	_, err = tx.Exec(ctx, `insert into nmc3.run_idempotency (tenant_id, idempotency_key, run_id, request_hash, created_at) values ($1,$2,$3,$4,$5)`, tenant, key, run.ID, hash, now)
	if err != nil {
		return contracts.Run{}, false, mapWriteErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return contracts.Run{}, false, err
	}
	return cloneRun(run), true, nil
}

func (s *Postgres) Create(ctx context.Context, run contracts.Run) error {
	if run.State == "" {
		run.State = contracts.StatePending
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := s.insertRun(ctx, tx, &run); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Postgres) Get(ctx context.Context, runID string) (contracts.Run, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `select snapshot from nmc3.runs where id=$1`, runID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Run{}, notFoundRun()
	}
	if err != nil {
		return contracts.Run{}, mapWriteErr(err)
	}
	run, err := decodeRun(raw)
	if err != nil {
		return contracts.Run{}, err
	}
	return cloneRun(run), nil
}

func (s *Postgres) Update(ctx context.Context, run contracts.Run) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	current, raw, err := lockRunRaw(ctx, tx, run.ID)
	if err != nil {
		return err
	}
	var held bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from nmc3.worker_leases where run_id=$1)`, run.ID).Scan(&held); err != nil {
		return mapWriteErr(err)
	}
	if held {
		return ErrLeaseHeld
	}
	if !transition(current.State, run.State) {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid run state transition"}
	}
	if current.State == contracts.StateSucceeded || current.State == contracts.StateFailed || current.State == contracts.StateCancelled {
		next, err := json.Marshal(run)
		if err != nil {
			return err
		}
		if !jsonEqual(raw, next) {
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "terminal run cannot change"}
		}
	}
	if err := s.saveRun(ctx, tx, run); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Postgres) Append(ctx context.Context, event contracts.RunEvent) (contracts.RunEvent, error) {
	if event.RunID == "" || event.Type == "" {
		return contracts.RunEvent{}, invalid("event run ID and type are required")
	}
	if len(event.Data) > 0 && !json.Valid(event.Data) {
		return contracts.RunEvent{}, invalid("event data is not json")
	}
	now := pgTime(event.Timestamp)
	inv := ""
	if event.InvocationID != nil {
		inv = *event.InvocationID
	}
	var payload any
	if len(event.Data) > 0 {
		payload = cloneRaw(event.Data)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return contracts.RunEvent{}, err
	}
	defer tx.Rollback(ctx)
	stored, err := s.appendEvent(ctx, tx, event.RunID, event.Type, inv, payload, now)
	if err != nil {
		return contracts.RunEvent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return contracts.RunEvent{}, err
	}
	return stored, nil
}

func (s *Postgres) List(ctx context.Context, runID string, after uint64) ([]contracts.RunEvent, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `select exists(select 1 from nmc3.runs where id=$1)`, runID).Scan(&exists); err != nil {
		return nil, mapWriteErr(err)
	}
	if !exists {
		return nil, notFoundRun()
	}
	rows, err := s.pool.Query(ctx, `select sequence, invocation_id, event_type, occurred_at, data, source from nmc3.run_events where run_id=$1 and sequence>$2 order by sequence`, runID, int64(after))
	if err != nil {
		return nil, mapWriteErr(err)
	}
	defer rows.Close()
	return collectEvents(runID, rows)
}

func (s *Postgres) Cancel(ctx context.Context, runID, reason string, now time.Time) (Cancellation, error) {
	now = pgTime(now)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Cancellation{}, err
	}
	defer tx.Rollback(ctx)
	run, err := lockRun(ctx, tx, runID)
	if err != nil {
		return Cancellation{}, err
	}
	if run.State == contracts.StateSucceeded || run.State == contracts.StateFailed {
		return Cancellation{}, ErrRunTerminal
	}
	if existing, ok, err := loadCancellation(ctx, tx, runID); err != nil {
		return Cancellation{}, err
	} else if ok {
		if err := tx.Commit(ctx); err != nil {
			return Cancellation{}, err
		}
		return existing, nil
	}
	if strings.TrimSpace(reason) == "" {
		return Cancellation{}, invalid("cancellation reason is required")
	}
	record := Cancellation{RunID: runID, Reason: reason, RequestedAt: now}
	if _, err := tx.Exec(ctx, `insert into nmc3.run_cancellations (run_id, reason, requested_at) values ($1,$2,$3)`, runID, reason, now); err != nil {
		return Cancellation{}, mapWriteErr(err)
	}
	if run.State == contracts.StatePending || run.State == contracts.StateRunning {
		run.State = contracts.StateCancelled
		run.UpdatedAt = now
		run.Error = &contracts.RunError{Code: "CANCELLED", Message: reason}
		journalMem.cancelOpenInvocations(&run, now)
		if err := s.saveRun(ctx, tx, run); err != nil {
			return Cancellation{}, err
		}
	}
	if _, err := s.appendEvent(ctx, tx, runID, EventCancellationRequested, "", eventBody{Reason: reason, State: string(contracts.StateCancelled)}, now); err != nil {
		return Cancellation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Cancellation{}, err
	}
	return record, nil
}

func (s *Postgres) Cancellation(ctx context.Context, runID string) (Cancellation, bool, error) {
	var reason *string
	var at *time.Time
	err := s.pool.QueryRow(ctx, `
		select c.reason, c.requested_at
		from nmc3.runs r
		left join nmc3.run_cancellations c on c.run_id = r.id
		where r.id=$1`, runID).Scan(&reason, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cancellation{}, false, notFoundRun()
	}
	if err != nil {
		return Cancellation{}, false, mapWriteErr(err)
	}
	if reason == nil || at == nil {
		return Cancellation{}, false, nil
	}
	return Cancellation{RunID: runID, Reason: *reason, RequestedAt: at.UTC()}, true, nil
}

func (s *Postgres) ClaimRun(ctx context.Context, runID, workerID string, ttl time.Duration, now time.Time) (Lease, error) {
	now = pgTime(now)
	if err := requireWorker(workerID, ttl); err != nil {
		return Lease{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Lease{}, err
	}
	defer tx.Rollback(ctx)
	run, err := lockRun(ctx, tx, runID)
	if err != nil {
		return Lease{}, err
	}
	lease, err := s.claimLocked(ctx, tx, &run, workerID, ttl, now)
	if err != nil {
		return Lease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Lease{}, err
	}
	return lease, nil
}

func (s *Postgres) claimLocked(ctx context.Context, tx pgx.Tx, run *contracts.Run, workerID string, ttl time.Duration, now time.Time) (Lease, error) {
	switch run.State {
	case contracts.StateCancelled:
		return Lease{}, ErrCancelled
	case contracts.StateSucceeded, contracts.StateFailed:
		return Lease{}, ErrRunTerminal
	}
	if _, cancelled, err := loadCancellation(ctx, tx, run.ID); err != nil {
		return Lease{}, err
	} else if cancelled {
		return Lease{}, ErrCancelled
	}
	current, held, err := loadLease(ctx, tx, run.ID)
	if err != nil {
		return Lease{}, err
	}
	if held && current.ExpiresAt.After(now) {
		return Lease{}, ErrLeaseHeld
	}
	if held {
		return Lease{}, ErrNeedsRecovery
	}
	token, err := nextFence(ctx, tx, run.ID)
	if err != nil {
		return Lease{}, err
	}
	lease := Lease{RunID: run.ID, WorkerID: workerID, FencingToken: token, ExpiresAt: pgTime(now.Add(ttl))}
	if _, err := tx.Exec(ctx, `insert into nmc3.worker_leases (run_id, worker_id, fencing_token, expires_at) values ($1,$2,$3,$4)`, lease.RunID, lease.WorkerID, lease.FencingToken, lease.ExpiresAt); err != nil {
		return Lease{}, mapWriteErr(err)
	}
	if run.State == contracts.StatePending {
		run.State = contracts.StateRunning
		run.UpdatedAt = now
		journalMem.setInvocationState(run, RootInvocationID(run.ID), contracts.StateRunning, now, false)
		if err := s.saveRun(ctx, tx, *run); err != nil {
			return Lease{}, err
		}
	}
	return lease, nil
}

func (s *Postgres) RenewLease(ctx context.Context, lease Lease, ttl time.Duration, now time.Time) (Lease, error) {
	now = pgTime(now)
	if ttl <= 0 {
		return Lease{}, invalid("lease TTL must be positive")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Lease{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockRun(ctx, tx, lease.RunID); err != nil {
		return Lease{}, err
	}
	if err := checkLease(ctx, tx, lease, now); err != nil {
		return Lease{}, err
	}
	lease.ExpiresAt = pgTime(now.Add(ttl))
	if _, err := tx.Exec(ctx, `update nmc3.worker_leases set expires_at=$2 where run_id=$1 and worker_id=$3 and fencing_token=$4`, lease.RunID, lease.ExpiresAt, lease.WorkerID, lease.FencingToken); err != nil {
		return Lease{}, mapWriteErr(err)
	}
	if _, err := tx.Exec(ctx, `update nmc3.attempts set lease_expires_at=$4 where run_id=$1 and worker_id=$2 and fencing_token=$3 and state='RUNNING'`, lease.RunID, lease.WorkerID, lease.FencingToken, lease.ExpiresAt); err != nil {
		return Lease{}, mapWriteErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Lease{}, err
	}
	return lease, nil
}

func (s *Postgres) ReleaseLease(ctx context.Context, lease Lease, _ time.Time) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockRun(ctx, tx, lease.RunID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `delete from nmc3.worker_leases where run_id=$1 and worker_id=$2 and fencing_token=$3`, lease.RunID, lease.WorkerID, lease.FencingToken)
	if err != nil {
		return mapWriteErr(err)
	}
	if tag.RowsAffected() != 1 {
		return ErrStaleLease
	}
	return tx.Commit(ctx)
}

func (s *Postgres) RecoverClaim(ctx context.Context, runID, workerID string, ttl time.Duration, now time.Time) (Lease, []Attempt, error) {
	now = pgTime(now)
	if err := requireWorker(workerID, ttl); err != nil {
		return Lease{}, nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Lease{}, nil, err
	}
	defer tx.Rollback(ctx)
	run, err := lockRun(ctx, tx, runID)
	if err != nil {
		return Lease{}, nil, err
	}
	if run.State == contracts.StateSucceeded || run.State == contracts.StateFailed {
		return Lease{}, nil, ErrRunTerminal
	}
	current, held, err := loadLease(ctx, tx, runID)
	if err != nil {
		return Lease{}, nil, err
	}
	if held && current.ExpiresAt.After(now) {
		return Lease{}, nil, ErrLeaseHeld
	}
	cancel := run.State == contracts.StateCancelled
	if _, ok, err := loadCancellation(ctx, tx, runID); err != nil {
		return Lease{}, nil, err
	} else if ok {
		cancel = true
	}
	attempts, err := loadAttempts(ctx, tx, runID, run)
	if err != nil {
		return Lease{}, nil, err
	}
	for _, att := range attempts {
		if att.State != AttemptRunning && att.State != AttemptClaimed {
			continue
		}
		if att.LeaseExpiresAt.After(now) && !cancel {
			return Lease{}, nil, ErrLeaseHeld
		}
	}
	var abandoned []Attempt
	for _, att := range attempts {
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
		if _, err := tx.Exec(ctx, `update nmc3.attempts set state=$2, finished_at=$3 where id=$1`, att.ID, string(next), now); err != nil {
			return Lease{}, nil, mapWriteErr(err)
		}
		abandoned = append(abandoned, hydrateAttempt(run, att))
		if _, err := s.appendEvent(ctx, tx, runID, event, att.InvocationID, eventBody{
			AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID,
			StepID: att.StepID, WorkerID: att.WorkerID, State: string(next),
		}, now); err != nil {
			return Lease{}, nil, err
		}
	}
	if cancel {
		if _, err := tx.Exec(ctx, `delete from nmc3.worker_leases where run_id=$1`, runID); err != nil {
			return Lease{}, nil, mapWriteErr(err)
		}
		if run.State == contracts.StatePending || run.State == contracts.StateRunning {
			run.State = contracts.StateCancelled
			run.UpdatedAt = now
			journalMem.cancelOpenInvocations(&run, now)
			if err := s.saveRun(ctx, tx, run); err != nil {
				return Lease{}, nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return Lease{}, nil, err
		}
		return Lease{}, abandoned, nil
	}
	token, err := nextFence(ctx, tx, runID)
	if err != nil {
		return Lease{}, nil, err
	}
	lease := Lease{RunID: runID, WorkerID: workerID, FencingToken: token, ExpiresAt: pgTime(now.Add(ttl))}
	if _, err := tx.Exec(ctx, `
		insert into nmc3.worker_leases (run_id, worker_id, fencing_token, expires_at)
		values ($1,$2,$3,$4)
		on conflict (run_id) do update set worker_id=excluded.worker_id, fencing_token=excluded.fencing_token, expires_at=excluded.expires_at`,
		lease.RunID, lease.WorkerID, lease.FencingToken, lease.ExpiresAt); err != nil {
		return Lease{}, nil, mapWriteErr(err)
	}
	if run.State == contracts.StatePending {
		run.State = contracts.StateRunning
		run.UpdatedAt = now
		journalMem.setInvocationState(&run, RootInvocationID(runID), contracts.StateRunning, now, false)
		if err := s.saveRun(ctx, tx, run); err != nil {
			return Lease{}, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Lease{}, nil, err
	}
	return lease, abandoned, nil
}

func (s *Postgres) BeginAttempt(ctx context.Context, req BeginRequest) (Attempt, error) {
	now := pgTime(req.Now)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback(ctx)
	run, err := lockRun(ctx, tx, req.RunID)
	if err != nil {
		return Attempt{}, err
	}
	if _, cancelled, err := loadCancellation(ctx, tx, req.RunID); err != nil {
		return Attempt{}, err
	} else if cancelled || run.State == contracts.StateCancelled {
		return Attempt{}, ErrCancelled
	}
	if run.State == contracts.StateSucceeded || run.State == contracts.StateFailed {
		return Attempt{}, ErrRunTerminal
	}
	lease := Lease{RunID: req.RunID, WorkerID: req.WorkerID, FencingToken: req.FencingToken}
	if err := checkLease(ctx, tx, lease, now); err != nil {
		return Attempt{}, err
	}
	current, _, err := loadLease(ctx, tx, req.RunID)
	if err != nil {
		return Attempt{}, err
	}
	step := req.Step
	step.OperationID = step.operationID()
	if step.ID == "" || step.OperationID == "" {
		return Attempt{}, invalid("step id and operation id are required")
	}
	if err := validKind(step); err != nil {
		return Attempt{}, err
	}
	if step.InvocationID != "" {
		var state string
		err := tx.QueryRow(ctx, `select state from nmc3.invocation_outcomes where invocation_id=$1`, step.InvocationID).Scan(&state)
		if err == nil && contracts.State(state) == contracts.StateSucceeded {
			return Attempt{}, ErrAlreadyCompleted
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Attempt{}, mapWriteErr(err)
		}
	}
	var uncertain bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from nmc3.attempts where operation_id=$1 and state='UNCERTAIN')`, step.OperationID).Scan(&uncertain); err != nil {
		return Attempt{}, mapWriteErr(err)
	}
	if uncertain {
		return Attempt{}, ErrUncertain
	}
	var active bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from nmc3.attempts where operation_id=$1 and state in ('CLAIMED','RUNNING'))`, step.OperationID).Scan(&active); err != nil {
		return Attempt{}, mapWriteErr(err)
	}
	if active {
		return Attempt{}, ErrAttemptClaimed
	}
	var number int
	if err := tx.QueryRow(ctx, `select coalesce(max(attempt_number),0)+1 from nmc3.attempts where operation_id=$1`, step.OperationID).Scan(&number); err != nil {
		return Attempt{}, mapWriteErr(err)
	}
	id, err := newID("att")
	if err != nil {
		return Attempt{}, err
	}
	att := Attempt{
		ID: id, AttemptNumber: number, RunID: req.RunID, OperationID: step.OperationID,
		WorkerID: req.WorkerID, State: AttemptRunning, Kind: step.Kind, SideEffect: step.SideEffect,
		StepID: step.ID, InvocationID: step.InvocationID, ParentInvocationID: step.ParentInvocationID,
		FencingToken: req.FencingToken, StartedAt: now, LeaseExpiresAt: current.ExpiresAt,
	}
	if _, err := tx.Exec(ctx, `
		insert into nmc3.attempts (
		  id, run_id, operation_id, attempt_number, worker_id, state, kind, side_effect,
		  step_id, invocation_id, fencing_token, error, started_at, finished_at, lease_expires_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,null,$12,null,$13)`,
		att.ID, att.RunID, att.OperationID, att.AttemptNumber, att.WorkerID, string(att.State), string(att.Kind), string(att.SideEffect),
		att.StepID, nullString(att.InvocationID), att.FencingToken, att.StartedAt, att.LeaseExpiresAt); err != nil {
		return Attempt{}, mapWriteErr(err)
	}
	journalMem.projectBegin(&run, att, now)
	if err := s.saveRun(ctx, tx, run); err != nil {
		return Attempt{}, err
	}
	if _, err := s.appendEvent(ctx, tx, req.RunID, EventAttemptStarted, att.InvocationID, eventBody{
		AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID,
		StepID: att.StepID, WorkerID: att.WorkerID, State: string(att.State),
		Kind: string(att.Kind), SideEffect: string(att.SideEffect),
	}, now); err != nil {
		return Attempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Attempt{}, err
	}
	return cloneAttempt(att), nil
}

func (s *Postgres) FailAttempt(ctx context.Context, req FailRequest) (Attempt, error) {
	now := pgTime(req.Now)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback(ctx)
	att, run, err := s.lockOwned(ctx, tx, req.AttemptID, req.WorkerID, req.FencingToken, now)
	if err != nil {
		return Attempt{}, err
	}
	if att.State != AttemptRunning && att.State != AttemptClaimed {
		return Attempt{}, invalid("attempt is not active")
	}
	cause := req.Error
	att.State = AttemptFailed
	att.Error = &cause
	att.FinishedAt = timePtr(now)
	if err := updateAttemptEnd(ctx, tx, att); err != nil {
		return Attempt{}, err
	}
	if att.Kind == KindReadTool {
		journalMem.finishTool(&run, att, nil, &cause, now)
		if err := s.saveRun(ctx, tx, run); err != nil {
			return Attempt{}, err
		}
	}
	if _, err := s.appendEvent(ctx, tx, att.RunID, EventAttemptFailed, att.InvocationID, eventBody{
		AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID,
		StepID: att.StepID, WorkerID: att.WorkerID, State: string(att.State), Error: cause.Message,
	}, now); err != nil {
		return Attempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Attempt{}, err
	}
	return cloneAttempt(att), nil
}

func (s *Postgres) MarkUncertain(ctx context.Context, req UncertainRequest) (Attempt, error) {
	now := pgTime(req.Now)
	if strings.TrimSpace(req.Reason) == "" {
		return Attempt{}, invalid("uncertain reason is required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback(ctx)
	att, _, err := s.lockOwned(ctx, tx, req.AttemptID, req.WorkerID, req.FencingToken, now)
	if err != nil {
		return Attempt{}, err
	}
	if att.State == AttemptUncertain {
		if err := tx.Commit(ctx); err != nil {
			return Attempt{}, err
		}
		return cloneAttempt(att), nil
	}
	if att.State != AttemptRunning && att.State != AttemptClaimed {
		return Attempt{}, invalid("attempt is not active")
	}
	att.State = AttemptUncertain
	att.Error = &contracts.RunError{Code: contracts.ErrInternal, Message: req.Reason}
	att.FinishedAt = timePtr(now)
	if err := updateAttemptEnd(ctx, tx, att); err != nil {
		return Attempt{}, err
	}
	if _, err := s.appendEvent(ctx, tx, att.RunID, EventAttemptUncertain, att.InvocationID, eventBody{
		AttemptID: att.ID, AttemptNumber: att.AttemptNumber, OperationID: att.OperationID,
		StepID: att.StepID, WorkerID: att.WorkerID, State: string(att.State), Reason: req.Reason,
		SideEffect: string(att.SideEffect),
	}, now); err != nil {
		return Attempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Attempt{}, err
	}
	return cloneAttempt(att), nil
}

func (s *Postgres) CommitStep(ctx context.Context, req CommitRequest) (Checkpoint, error) {
	now := pgTime(req.Now)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Checkpoint{}, err
	}
	defer tx.Rollback(ctx)
	att, run, err := s.lockOwned(ctx, tx, req.AttemptID, req.WorkerID, req.FencingToken, now)
	if err != nil {
		return Checkpoint{}, err
	}
	if att.State == AttemptSucceeded {
		cp, _, err := latestCheckpoint(ctx, tx, att.RunID)
		if err != nil {
			return Checkpoint{}, err
		}
		return cp, ErrAlreadyCompleted
	}
	if att.State != AttemptRunning {
		return Checkpoint{}, invalid("attempt is not active")
	}
	if !json.Valid(req.Output) {
		return Checkpoint{}, invalid("step output is not json")
	}
	cancelled := run.State == contracts.StateCancelled
	if !cancelled {
		_, cancelled, err = loadCancellation(ctx, tx, att.RunID)
		if err != nil {
			return Checkpoint{}, err
		}
	}
	if att.Kind == KindFinish && cancelled {
		att.State = AttemptCancelled
		att.FinishedAt = timePtr(now)
		att.Error = &contracts.RunError{Code: "CANCELLED", Message: "run cancelled before finish"}
		if err := updateAttemptEnd(ctx, tx, att); err != nil {
			return Checkpoint{}, err
		}
		if _, err := s.appendEvent(ctx, tx, att.RunID, EventAttemptCancelled, att.InvocationID, eventBody{
			AttemptID: att.ID, State: string(AttemptCancelled), Reason: "run cancelled before finish",
		}, now); err != nil {
			return Checkpoint{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Checkpoint{}, err
		}
		return Checkpoint{}, ErrCancelled
	}
	if att.Kind == KindFinish && run.State != contracts.StateRunning && run.State != contracts.StatePending {
		return Checkpoint{}, ErrRunTerminal
	}
	if att.InvocationID != "" {
		var prev string
		err := tx.QueryRow(ctx, `select state from nmc3.invocation_outcomes where invocation_id=$1`, att.InvocationID).Scan(&prev)
		if err == nil && contracts.State(prev) == contracts.StateSucceeded {
			cause := contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invocation already completed"}
			att.State = AttemptFailed
			att.Error = &cause
			att.FinishedAt = timePtr(now)
			if err := updateAttemptEnd(ctx, tx, att); err != nil {
				return Checkpoint{}, err
			}
			if _, err := s.appendEvent(ctx, tx, att.RunID, EventAttemptFailed, att.InvocationID, eventBody{
				AttemptID: att.ID, AttemptNumber: att.AttemptNumber, State: string(AttemptFailed), Error: cause.Message,
			}, now); err != nil {
				return Checkpoint{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return Checkpoint{}, err
			}
			return Checkpoint{}, ErrAlreadyCompleted
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Checkpoint{}, mapWriteErr(err)
		}
	}
	progress, _, err := progressFromLatest(ctx, tx, att.RunID)
	if err != nil {
		return Checkpoint{}, err
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
	var seq int64
	if err := tx.QueryRow(ctx, `select coalesce(max(sequence),0)+1 from nmc3.checkpoints where run_id=$1`, att.RunID).Scan(&seq); err != nil {
		return Checkpoint{}, mapWriteErr(err)
	}
	cp := Checkpoint{
		RunID: att.RunID, Sequence: seq, StepID: att.StepID, InvocationID: att.InvocationID,
		AttemptID: att.ID, Payload: payload, CreatedAt: now,
	}
	if _, err := tx.Exec(ctx, `
		insert into nmc3.checkpoints (run_id, sequence, step_id, invocation_id, attempt_id, payload, created_at)
		values ($1,$2,$3,$4,$5,$6::jsonb,$7)`,
		cp.RunID, cp.Sequence, cp.StepID, nullString(cp.InvocationID), cp.AttemptID, string(payload), cp.CreatedAt); err != nil {
		return Checkpoint{}, mapWriteErr(err)
	}
	att.State = AttemptSucceeded
	att.FinishedAt = timePtr(now)
	att.Error = nil
	if err := updateAttemptEnd(ctx, tx, att); err != nil {
		return Checkpoint{}, err
	}
	if att.InvocationID != "" {
		if _, err := tx.Exec(ctx, `
			insert into nmc3.invocation_outcomes (invocation_id, run_id, state, output, error, attempt_id, completed_at)
			values ($1,$2,$3,$4::jsonb,null,$5,$6)`,
			att.InvocationID, att.RunID, string(contracts.StateSucceeded), string(req.Output), att.ID, now); err != nil {
			return Checkpoint{}, mapWriteErr(err)
		}
	}
	journalMem.projectCommit(&run, att, cloneRaw(req.Output), now)
	if att.Kind == KindFinish {
		run.State = contracts.StateSucceeded
		run.Output = cloneRaw(req.Output)
		run.Error = nil
		run.UpdatedAt = now
	}
	if err := s.saveRun(ctx, tx, run); err != nil {
		return Checkpoint{}, err
	}
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
		if _, err := s.appendEvent(ctx, tx, att.RunID, item.typ, att.InvocationID, item.body, now); err != nil {
			return Checkpoint{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Checkpoint{}, err
	}
	return cloneCheckpoint(cp), nil
}

func (s *Postgres) FailRun(ctx context.Context, runID string, cause contracts.RunError, lease Lease, now time.Time) error {
	now = pgTime(now)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	run, err := lockRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err := checkLease(ctx, tx, lease, now); err != nil {
		return err
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
	if err := s.saveRun(ctx, tx, run); err != nil {
		return err
	}
	if _, err := s.appendEvent(ctx, tx, runID, EventRunFailed, "", eventBody{State: string(contracts.StateFailed), Error: cause.Message, WorkerID: lease.WorkerID}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Postgres) LatestProgress(ctx context.Context, runID string) (Progress, Checkpoint, bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `select exists(select 1 from nmc3.runs where id=$1)`, runID).Scan(&exists); err != nil {
		return Progress{}, Checkpoint{}, false, mapWriteErr(err)
	}
	if !exists {
		return Progress{}, Checkpoint{}, false, notFoundRun()
	}
	cp, ok, err := latestCheckpoint(ctx, s.pool, runID)
	if err != nil || !ok {
		return emptyProgress(), Checkpoint{}, false, err
	}
	return decodeProgress(cp.Payload), cp, true, nil
}

func (s *Postgres) Invocation(ctx context.Context, invocationID string) (InvocationOutcome, bool, error) {
	rows, err := s.pool.Query(ctx, outcomeSelect+` where invocation_id=$1`, invocationID)
	if err != nil {
		return InvocationOutcome{}, false, mapWriteErr(err)
	}
	defer rows.Close()
	list, err := collectOutcomes(rows)
	if err != nil || len(list) == 0 {
		return InvocationOutcome{}, false, err
	}
	return list[0], true, nil
}

func (s *Postgres) Attempts(ctx context.Context, runID string) ([]Attempt, error) {
	run, err := s.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, attemptSelect+` where run_id=$1 order by started_at, attempt_number, id`, runID)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	defer rows.Close()
	return collectAttempts(run, rows)
}

func (s *Postgres) Checkpoints(ctx context.Context, runID string) ([]Checkpoint, error) {
	if _, err := s.Get(ctx, runID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, checkpointSelect+` where run_id=$1 order by sequence`, runID)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	defer rows.Close()
	return collectCheckpoints(rows)
}

func (s *Postgres) Outcomes(ctx context.Context, runID string) ([]InvocationOutcome, error) {
	if _, err := s.Get(ctx, runID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, outcomeSelect+` where run_id=$1 order by completed_at, invocation_id`, runID)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	defer rows.Close()
	return collectOutcomes(rows)
}

func (s *Postgres) CurrentLease(ctx context.Context, runID string) (Lease, bool, error) {
	var worker *string
	var token *int64
	var exp *time.Time
	err := s.pool.QueryRow(ctx, `
		select l.worker_id, l.fencing_token, l.expires_at
		from nmc3.runs r
		left join nmc3.worker_leases l on l.run_id = r.id
		where r.id=$1`, runID).Scan(&worker, &token, &exp)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, false, notFoundRun()
	}
	if err != nil {
		return Lease{}, false, mapWriteErr(err)
	}
	if worker == nil || token == nil || exp == nil {
		return Lease{}, false, nil
	}
	return Lease{RunID: runID, WorkerID: *worker, FencingToken: *token, ExpiresAt: exp.UTC()}, true, nil
}

func (s *Postgres) NoteRecovery(ctx context.Context, runID, workerID string, abandoned []Attempt, now time.Time) error {
	now = pgTime(now)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockRun(ctx, tx, runID); err != nil {
		return err
	}
	ids := make([]string, 0, len(abandoned))
	for _, att := range abandoned {
		ids = append(ids, att.ID)
	}
	if _, err := s.appendEvent(ctx, tx, runID, EventRecoveryStarted, "", eventBody{WorkerID: workerID, Abandoned: ids, State: "RECOVERING"}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Postgres) ApplyRetention(ctx context.Context, policy RetentionPolicy, now time.Time) (RetentionStats, error) {
	now = pgTime(now)
	if policy.TerminalHorizon <= 0 {
		return RetentionStats{}, invalid("retention horizon must be positive")
	}
	keep := policy.KeepLatestCheckpoints
	if keep < 1 {
		keep = 1
	}
	cutoff := now.Add(-policy.TerminalHorizon)
	tag, err := s.pool.Exec(ctx, `
		delete from nmc3.checkpoints
		where (run_id, sequence) in (
		  select run_id, sequence from (
		    select c.run_id, c.sequence,
		           row_number() over (partition by c.run_id order by c.sequence desc) as rn
		    from nmc3.checkpoints c
		    join nmc3.runs r on r.id = c.run_id
		    where r.state in ('SUCCEEDED','FAILED','CANCELLED')
		      and r.updated_at < $1
		  ) ranked
		  where rn > $2
		)`, cutoff, keep)
	if err != nil {
		return RetentionStats{}, mapWriteErr(err)
	}
	return RetentionStats{CheckpointsRemoved: int(tag.RowsAffected())}, nil
}

func (s *Postgres) Replay(ctx context.Context, runID string) (Replay, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Replay{}, err
	}
	defer tx.Rollback(ctx)
	run, err := fetchRun(ctx, tx, runID)
	if err != nil {
		return Replay{}, err
	}
	attempts, err := loadAttempts(ctx, tx, runID, run)
	if err != nil {
		return Replay{}, err
	}
	crows, err := tx.Query(ctx, checkpointSelect+` where run_id=$1 order by sequence`, runID)
	if err != nil {
		return Replay{}, mapWriteErr(err)
	}
	checkpoints, err := collectCheckpoints(crows)
	crows.Close()
	if err != nil {
		return Replay{}, err
	}
	orows, err := tx.Query(ctx, outcomeSelect+` where run_id=$1 order by completed_at, invocation_id`, runID)
	if err != nil {
		return Replay{}, mapWriteErr(err)
	}
	outcomes, err := collectOutcomes(orows)
	orows.Close()
	if err != nil {
		return Replay{}, err
	}
	erows, err := tx.Query(ctx, `select sequence, invocation_id, event_type, occurred_at, data, source from nmc3.run_events where run_id=$1 order by sequence`, runID)
	if err != nil {
		return Replay{}, mapWriteErr(err)
	}
	events, err := collectEvents(runID, erows)
	erows.Close()
	if err != nil {
		return Replay{}, err
	}
	replay := Replay{Run: cloneRun(run), Attempts: attempts, Checkpoints: checkpoints, Outcomes: outcomes, Events: events}
	if lease, ok, err := loadLease(ctx, tx, runID); err != nil {
		return Replay{}, err
	} else if ok {
		copied := lease
		replay.Lease = &copied
	}
	if cancel, ok, err := loadCancellation(ctx, tx, runID); err != nil {
		return Replay{}, err
	} else if ok {
		copied := cancel
		replay.Cancellation = &copied
	}
	if err := tx.Commit(ctx); err != nil {
		return Replay{}, err
	}
	return replay, nil
}

func (s *Postgres) insertRun(ctx context.Context, tx pgx.Tx, run *contracts.Run) error {
	if run.ID == "" {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "run ID is required"}
	}
	if run.State != contracts.StatePending {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "new runs start PENDING"}
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = pgTime(time.Time{})
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	run.CreatedAt = pgTime(run.CreatedAt)
	run.UpdatedAt = pgTime(run.UpdatedAt)
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	ensureRoot(run)
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into nmc3.runs (id, state, agent_id, agent_version, snapshot, created_at, updated_at) values ($1,$2,$3,$4,$5::jsonb,$6,$7)`,
		run.ID, string(run.State), run.AgentID, run.Version, string(raw), run.CreatedAt, run.UpdatedAt)
	if err != nil {
		return mapWriteErr(err)
	}
	return s.syncChildren(ctx, tx, *run)
}

func (s *Postgres) saveRun(ctx context.Context, tx pgx.Tx, run contracts.Run) error {
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	run.CreatedAt = pgTime(run.CreatedAt)
	run.UpdatedAt = pgTime(run.UpdatedAt)
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `update nmc3.runs set state=$2, agent_id=$3, agent_version=$4, snapshot=$5::jsonb, updated_at=$6 where id=$1`,
		run.ID, string(run.State), run.AgentID, run.Version, string(raw), run.UpdatedAt)
	if err != nil {
		return mapWriteErr(err)
	}
	if tag.RowsAffected() != 1 {
		return notFoundRun()
	}
	return s.syncChildren(ctx, tx, run)
}

func (s *Postgres) syncChildren(ctx context.Context, tx pgx.Tx, run contracts.Run) error {
	invocations := append([]contracts.AgentInvocation(nil), run.Invocations...)
	// Parents are depth 0. Insert them before depth-1 specialists so the parent FK holds.
	for pass := 0; pass < 2; pass++ {
		for _, inv := range invocations {
			if pass == 0 && inv.Depth != 0 {
				continue
			}
			if pass == 1 && inv.Depth == 0 {
				continue
			}
			payload, err := json.Marshal(inv)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				insert into nmc3.agent_invocations (id, run_id, parent_id, agent_id, agent_version, depth, state, snapshot)
				values ($1,$2,$3,$4,$5,$6,$7,$8::jsonb)
				on conflict (id) do update set
				  parent_id=excluded.parent_id,
				  agent_id=excluded.agent_id,
				  agent_version=excluded.agent_version,
				  depth=excluded.depth,
				  state=excluded.state,
				  snapshot=excluded.snapshot`,
				inv.ID, run.ID, parentArg(inv.ParentID), inv.AgentID, inv.AgentVersion, inv.Depth, string(inv.State), string(payload))
			if err != nil {
				return mapWriteErr(err)
			}
		}
	}
	for _, tool := range run.ToolInvocations {
		payload, err := json.Marshal(tool)
		if err != nil {
			return err
		}
		state := "SUCCEEDED"
		if tool.Error != nil {
			state = "FAILED"
		} else if tool.FinishedAt == nil {
			state = "RUNNING"
		}
		_, err = tx.Exec(ctx, `
			insert into nmc3.tool_invocations (id, invocation_id, tool_id, tool_version, state, snapshot, started_at, finished_at)
			values ($1,$2,$3,$4,$5,$6::jsonb,$7,$8)
			on conflict (id) do update set
			  invocation_id=excluded.invocation_id,
			  tool_id=excluded.tool_id,
			  tool_version=excluded.tool_version,
			  state=excluded.state,
			  snapshot=excluded.snapshot,
			  started_at=excluded.started_at,
			  finished_at=excluded.finished_at`,
			tool.ID, tool.AgentInvocationID, tool.ToolID, tool.ToolVersion, state, string(payload), pgTime(tool.StartedAt), tool.FinishedAt)
		if err != nil {
			return mapWriteErr(err)
		}
	}
	return nil
}

func (s *Postgres) appendEvent(ctx context.Context, tx pgx.Tx, runID, typ, invocationID string, data any, now time.Time) (contracts.RunEvent, error) {
	var raw []byte
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			return contracts.RunEvent{}, err
		}
		raw = encoded
	}
	var seq int64
	err := tx.QueryRow(ctx, `update nmc3.runs set next_event_sequence=next_event_sequence+1 where id=$1 returning next_event_sequence`, runID).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.RunEvent{}, notFoundRun()
	}
	if err != nil {
		return contracts.RunEvent{}, mapWriteErr(err)
	}
	var payload any
	if len(raw) > 0 {
		payload = string(raw)
	}
	if _, err := tx.Exec(ctx, `
		insert into nmc3.run_events (run_id, sequence, invocation_id, event_type, occurred_at, data, source)
		values ($1,$2,$3,$4,$5,$6::jsonb,'nmc3')`,
		runID, seq, nullString(invocationID), typ, now, payload); err != nil {
		return contracts.RunEvent{}, mapWriteErr(err)
	}
	event := contracts.RunEvent{
		Sequence:  uint64(seq),
		RunID:     runID,
		Type:      typ,
		Timestamp: now,
		Data:      cloneRaw(raw),
		Source:    "nmc3",
	}
	if invocationID != "" {
		id := invocationID
		event.InvocationID = &id
	}
	if len(raw) == 0 {
		event.Data = nil
	}
	return event, nil
}

func (s *Postgres) lockOwned(ctx context.Context, tx pgx.Tx, attemptID, workerID string, token int64, now time.Time) (Attempt, contracts.Run, error) {
	var runID string
	err := tx.QueryRow(ctx, `select run_id from nmc3.attempts where id=$1`, attemptID).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attempt{}, contracts.Run{}, ErrNotFound
	}
	if err != nil {
		return Attempt{}, contracts.Run{}, mapWriteErr(err)
	}
	run, err := lockRun(ctx, tx, runID)
	if err != nil {
		return Attempt{}, contracts.Run{}, err
	}
	if err := checkLease(ctx, tx, Lease{RunID: runID, WorkerID: workerID, FencingToken: token}, now); err != nil {
		return Attempt{}, contracts.Run{}, err
	}
	att, err := loadAttempt(ctx, tx, attemptID)
	if err != nil {
		return Attempt{}, contracts.Run{}, err
	}
	att = hydrateAttempt(run, att)
	if att.WorkerID != workerID || att.FencingToken != token {
		return Attempt{}, contracts.Run{}, ErrStaleLease
	}
	return att, run, nil
}

func ensureRoot(run *contracts.Run) {
	id := RootInvocationID(run.ID)
	for _, inv := range run.Invocations {
		if inv.ID == id {
			return
		}
	}
	run.Invocations = append(run.Invocations, contracts.AgentInvocation{
		ID: id, RunID: run.ID, AgentID: run.AgentID, AgentVersion: run.Version,
		Depth: 0, State: contracts.StatePending, Input: cloneRaw(run.Input),
	})
}

func validKind(step Step) error {
	switch step.Kind {
	case KindSpecialist, KindReadTool, KindFinish, KindWrite:
	default:
		return invalid("unknown step kind")
	}
	switch step.SideEffect {
	case SideEffectNone, SideEffectReadOnly, SideEffectWrite:
	default:
		return invalid("unknown side effect")
	}
	return nil
}

func checkLease(ctx context.Context, tx pgx.Tx, lease Lease, now time.Time) error {
	current, ok, err := loadLease(ctx, tx, lease.RunID)
	if err != nil {
		return err
	}
	if !ok || current.FencingToken != lease.FencingToken || current.WorkerID != lease.WorkerID {
		return ErrStaleLease
	}
	if !current.ExpiresAt.After(now) {
		return ErrStaleLease
	}
	return nil
}

func loadLease(ctx context.Context, tx pgx.Tx, runID string) (Lease, bool, error) {
	var lease Lease
	err := tx.QueryRow(ctx, `select worker_id, fencing_token, expires_at from nmc3.worker_leases where run_id=$1`, runID).Scan(&lease.WorkerID, &lease.FencingToken, &lease.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, false, nil
	}
	if err != nil {
		return Lease{}, false, mapWriteErr(err)
	}
	lease.RunID = runID
	lease.ExpiresAt = lease.ExpiresAt.UTC()
	return lease, true, nil
}

func loadCancellation(ctx context.Context, tx pgx.Tx, runID string) (Cancellation, bool, error) {
	var record Cancellation
	err := tx.QueryRow(ctx, `select reason, requested_at from nmc3.run_cancellations where run_id=$1`, runID).Scan(&record.Reason, &record.RequestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cancellation{}, false, nil
	}
	if err != nil {
		return Cancellation{}, false, mapWriteErr(err)
	}
	record.RunID = runID
	record.RequestedAt = record.RequestedAt.UTC()
	return record, true, nil
}

func nextFence(ctx context.Context, tx pgx.Tx, runID string) (int64, error) {
	var token int64
	err := tx.QueryRow(ctx, `
		select coalesce(max(fencing_token),0)+1 from (
		  select fencing_token from nmc3.worker_leases where run_id=$1
		  union all
		  select fencing_token from nmc3.attempts where run_id=$1
		) tokens`, runID).Scan(&token)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	if token < 1 {
		token = 1
	}
	return token, nil
}

func updateAttemptEnd(ctx context.Context, tx pgx.Tx, att Attempt) error {
	var errJSON any
	if att.Error != nil {
		raw, err := json.Marshal(att.Error)
		if err != nil {
			return err
		}
		errJSON = string(raw)
	}
	_, err := tx.Exec(ctx, `update nmc3.attempts set state=$2, finished_at=$3, error=$4::jsonb where id=$1`, att.ID, string(att.State), att.FinishedAt, errJSON)
	return mapWriteErr(err)
}

const attemptSelect = `
	select id, run_id, operation_id, attempt_number, worker_id, state, kind, side_effect,
	       step_id, invocation_id, fencing_token, error, started_at, finished_at, lease_expires_at
	from nmc3.attempts`

const checkpointSelect = `
	select run_id, sequence, step_id, invocation_id, attempt_id, payload, created_at
	from nmc3.checkpoints`

const outcomeSelect = `
	select invocation_id, run_id, state, output, error, attempt_id, completed_at
	from nmc3.invocation_outcomes`

type rowQuery interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func loadAttempts(ctx context.Context, q rowQuery, runID string, run contracts.Run) ([]Attempt, error) {
	rows, err := q.Query(ctx, attemptSelect+` where run_id=$1 order by started_at, attempt_number, id`, runID)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	defer rows.Close()
	return collectAttempts(run, rows)
}

func loadAttempt(ctx context.Context, tx pgx.Tx, id string) (Attempt, error) {
	rows, err := tx.Query(ctx, attemptSelect+` where id=$1 for update`, id)
	if err != nil {
		return Attempt{}, mapWriteErr(err)
	}
	defer rows.Close()
	list, err := collectAttempts(contracts.Run{}, rows)
	if err != nil {
		return Attempt{}, err
	}
	if len(list) == 0 {
		return Attempt{}, ErrNotFound
	}
	return list[0], nil
}

func collectAttempts(run contracts.Run, rows pgx.Rows) ([]Attempt, error) {
	out := []Attempt{}
	for rows.Next() {
		var att Attempt
		var state, kind, side string
		var inv *string
		var errRaw []byte
		var finished, leaseExp *time.Time
		if err := rows.Scan(&att.ID, &att.RunID, &att.OperationID, &att.AttemptNumber, &att.WorkerID, &state, &kind, &side, &att.StepID, &inv, &att.FencingToken, &errRaw, &att.StartedAt, &finished, &leaseExp); err != nil {
			return nil, mapWriteErr(err)
		}
		att.State = AttemptState(state)
		att.Kind = StepKind(kind)
		att.SideEffect = SideEffect(side)
		att.StartedAt = att.StartedAt.UTC()
		if inv != nil {
			att.InvocationID = *inv
		}
		if finished != nil {
			t := finished.UTC()
			att.FinishedAt = &t
		}
		if leaseExp != nil {
			att.LeaseExpiresAt = leaseExp.UTC()
		}
		if len(errRaw) > 0 {
			var cause contracts.RunError
			if err := json.Unmarshal(errRaw, &cause); err != nil {
				return nil, err
			}
			att.Error = &cause
		}
		out = append(out, cloneAttempt(hydrateAttempt(run, att)))
	}
	return out, rows.Err()
}

func hydrateAttempt(run contracts.Run, att Attempt) Attempt {
	if att.ParentInvocationID != "" {
		return att
	}
	switch att.Kind {
	case KindSpecialist, KindFinish:
		for _, inv := range run.Invocations {
			if inv.ID == att.InvocationID && inv.ParentID != nil {
				att.ParentInvocationID = *inv.ParentID
				return att
			}
		}
	case KindReadTool:
		for _, tool := range run.ToolInvocations {
			if tool.ID == att.ID {
				att.ParentInvocationID = tool.AgentInvocationID
				return att
			}
		}
	}
	return att
}

func latestCheckpoint(ctx context.Context, q rowQuery, runID string) (Checkpoint, bool, error) {
	rows, err := q.Query(ctx, checkpointSelect+` where run_id=$1 order by sequence desc limit 1`, runID)
	if err != nil {
		return Checkpoint{}, false, mapWriteErr(err)
	}
	defer rows.Close()
	list, err := collectCheckpoints(rows)
	if err != nil || len(list) == 0 {
		return Checkpoint{}, false, err
	}
	return list[0], true, nil
}

func progressFromLatest(ctx context.Context, q rowQuery, runID string) (Progress, Checkpoint, error) {
	cp, ok, err := latestCheckpoint(ctx, q, runID)
	if err != nil || !ok {
		return emptyProgress(), Checkpoint{}, err
	}
	return decodeProgress(cp.Payload), cp, nil
}

func collectCheckpoints(rows pgx.Rows) ([]Checkpoint, error) {
	out := []Checkpoint{}
	for rows.Next() {
		var cp Checkpoint
		var inv *string
		var payload []byte
		if err := rows.Scan(&cp.RunID, &cp.Sequence, &cp.StepID, &inv, &cp.AttemptID, &payload, &cp.CreatedAt); err != nil {
			return nil, mapWriteErr(err)
		}
		if inv != nil {
			cp.InvocationID = *inv
		}
		cp.Payload = payload
		cp.CreatedAt = cp.CreatedAt.UTC()
		out = append(out, cloneCheckpoint(cp))
	}
	return out, rows.Err()
}

func collectOutcomes(rows pgx.Rows) ([]InvocationOutcome, error) {
	out := []InvocationOutcome{}
	for rows.Next() {
		var outcome InvocationOutcome
		var state string
		var output, errRaw []byte
		if err := rows.Scan(&outcome.InvocationID, &outcome.RunID, &state, &output, &errRaw, &outcome.AttemptID, &outcome.CompletedAt); err != nil {
			return nil, mapWriteErr(err)
		}
		outcome.State = contracts.State(state)
		outcome.Output = output
		outcome.CompletedAt = outcome.CompletedAt.UTC()
		if len(errRaw) > 0 {
			var cause contracts.RunError
			if err := json.Unmarshal(errRaw, &cause); err != nil {
				return nil, err
			}
			outcome.Error = &cause
		}
		out = append(out, cloneOutcome(outcome))
	}
	return out, rows.Err()
}

func collectEvents(runID string, rows pgx.Rows) ([]contracts.RunEvent, error) {
	out := []contracts.RunEvent{}
	for rows.Next() {
		event := contracts.RunEvent{RunID: runID}
		var seq int64
		var inv, source *string
		var data []byte
		if err := rows.Scan(&seq, &inv, &event.Type, &event.Timestamp, &data, &source); err != nil {
			return nil, mapWriteErr(err)
		}
		event.Sequence = uint64(seq)
		event.Timestamp = event.Timestamp.UTC()
		event.Data = data
		if inv != nil {
			id := *inv
			event.InvocationID = &id
		}
		if source != nil {
			event.Source = *source
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func fetchRun(ctx context.Context, q rowQuery, runID string) (contracts.Run, error) {
	var raw []byte
	err := q.QueryRow(ctx, `select snapshot from nmc3.runs where id=$1`, runID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Run{}, notFoundRun()
	}
	if err != nil {
		return contracts.Run{}, mapWriteErr(err)
	}
	return decodeRun(raw)
}

func lockRun(ctx context.Context, tx pgx.Tx, runID string) (contracts.Run, error) {
	run, _, err := lockRunRaw(ctx, tx, runID)
	return run, err
}

func lockRunRaw(ctx context.Context, tx pgx.Tx, runID string) (contracts.Run, []byte, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `select snapshot from nmc3.runs where id=$1 for update`, runID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Run{}, nil, notFoundRun()
	}
	if err != nil {
		return contracts.Run{}, nil, mapWriteErr(err)
	}
	run, err := decodeRun(raw)
	if err != nil {
		return contracts.Run{}, nil, err
	}
	return run, raw, nil
}

func decodeRun(raw []byte) (contracts.Run, error) {
	var run contracts.Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return contracts.Run{}, fmt.Errorf("durable: run snapshot: %w", err)
	}
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	return run, nil
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	lb, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(lb) == string(rb)
}

func mapWriteErr(err error) error {
	if err == nil {
		return nil
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		switch pgerr.ConstraintName {
		case "attempts_one_active_idx":
			return ErrAttemptClaimed
		case "attempts_one_uncertain_idx":
			return ErrUncertain
		case "invocation_outcomes_pkey":
			return ErrAlreadyCompleted
		case "runs_pkey":
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "run ID already exists"}
		case "worker_leases_pkey":
			return ErrLeaseHeld
		case "run_idempotency_pkey":
			return ErrIdempotencyConflict
		}
	}
	if errors.As(err, &pgerr) {
		return fmt.Errorf("durable: postgres %s (%s): %w", pgerr.ConstraintName, pgerr.Code, err)
	}
	return fmt.Errorf("durable: postgres: %w", err)
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func parentArg(id *string) any {
	if id == nil || *id == "" {
		return nil
	}
	return *id
}

func pgTime(t time.Time) time.Time {
	return utc(t).Truncate(time.Microsecond)
}

func newID(prefix string) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(buf[:]), nil
}
