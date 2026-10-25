-- Pass 3 durable attempts, checkpoints, leases, and cancellation.
-- Depends on 202610240001_nmc3_pass1.sql. The Go reference journal is
-- agents/internal/durable.Memory; these constraints are the database guard
-- for a later Postgres adapter. Protected writes are not dispatched here:
-- a WRITE attempt is recorded as UNCERTAIN and is not retried.

create table if not exists nmc3.run_idempotency (
  tenant_id text not null,
  idempotency_key text not null,
  run_id text not null unique references nmc3.runs(id) on delete cascade,
  request_hash text not null,
  created_at timestamptz not null,
  primary key (tenant_id, idempotency_key)
);

create table if not exists nmc3.attempts (
  id text primary key,
  run_id text not null references nmc3.runs(id) on delete cascade,
  operation_id text not null,
  attempt_number integer not null check (attempt_number > 0),
  worker_id text not null,
  state text not null check (state in ('CLAIMED','RUNNING','SUCCEEDED','FAILED','CANCELLED','UNCERTAIN','ABANDONED')),
  kind text not null check (kind in ('specialist','read_tool','finish','write')),
  side_effect text not null check (side_effect in ('NONE','READ_ONLY','WRITE')),
  step_id text not null,
  invocation_id text,
  fencing_token bigint not null check (fencing_token > 0),
  error jsonb,
  started_at timestamptz not null,
  finished_at timestamptz,
  lease_expires_at timestamptz,
  unique (operation_id, attempt_number),
  check (
    (state in ('CLAIMED','RUNNING') and finished_at is null)
    or
    (state not in ('CLAIMED','RUNNING') and finished_at is not null)
  )
);
create unique index if not exists attempts_one_active_idx
  on nmc3.attempts(operation_id) where state in ('CLAIMED','RUNNING');
create unique index if not exists attempts_one_uncertain_idx
  on nmc3.attempts(operation_id) where state = 'UNCERTAIN';
create index if not exists attempts_run_idx on nmc3.attempts(run_id, started_at);

create table if not exists nmc3.invocation_outcomes (
  invocation_id text primary key,
  run_id text not null references nmc3.runs(id) on delete cascade,
  state text not null check (state in ('SUCCEEDED','FAILED','CANCELLED')),
  output jsonb,
  error jsonb,
  attempt_id text not null references nmc3.attempts(id) on delete cascade,
  completed_at timestamptz not null
);
create index if not exists invocation_outcomes_run_idx on nmc3.invocation_outcomes(run_id, completed_at);

create table if not exists nmc3.checkpoints (
  run_id text not null references nmc3.runs(id) on delete cascade,
  sequence bigint not null check (sequence > 0),
  step_id text not null,
  invocation_id text,
  attempt_id text not null references nmc3.attempts(id) on delete cascade,
  payload jsonb not null,
  created_at timestamptz not null,
  primary key (run_id, sequence)
);

create table if not exists nmc3.worker_leases (
  run_id text primary key references nmc3.runs(id) on delete cascade,
  worker_id text not null,
  fencing_token bigint not null check (fencing_token > 0),
  expires_at timestamptz not null
);

create table if not exists nmc3.run_cancellations (
  run_id text primary key references nmc3.runs(id) on delete cascade,
  reason text not null,
  requested_at timestamptz not null
);

revoke all on nmc3.run_idempotency, nmc3.attempts, nmc3.invocation_outcomes,
  nmc3.checkpoints, nmc3.worker_leases, nmc3.run_cancellations
  from public, anon, authenticated;
grant select, insert, update, delete on nmc3.run_idempotency, nmc3.attempts,
  nmc3.invocation_outcomes, nmc3.checkpoints, nmc3.worker_leases, nmc3.run_cancellations
  to service_role;
