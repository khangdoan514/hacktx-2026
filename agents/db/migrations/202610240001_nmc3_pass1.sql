-- Pass 1 stores inspectable run history. It does not schedule or recover work.
create schema if not exists nmc3;
revoke all on schema nmc3 from public, anon, authenticated;

create table if not exists nmc3.runs (
  id text primary key,
  state text not null check (state in ('PENDING','RUNNING','SUCCEEDED','FAILED','CANCELLED')),
  agent_id text not null,
  agent_version text not null,
  snapshot jsonb not null,
  next_event_sequence bigint not null default 0 check (next_event_sequence >= 0),
  created_at timestamptz not null,
  updated_at timestamptz not null
);
create index if not exists runs_state_updated_idx on nmc3.runs(state,updated_at);

create table if not exists nmc3.agent_invocations (
  id text primary key,
  run_id text not null references nmc3.runs(id) on delete cascade,
  parent_id text references nmc3.agent_invocations(id),
  agent_id text not null,
  agent_version text not null,
  depth integer not null check (depth between 0 and 1),
  state text not null check (state in ('PENDING','RUNNING','SUCCEEDED','FAILED','CANCELLED')),
  snapshot jsonb not null
);
create index if not exists invocations_run_parent_idx on nmc3.agent_invocations(run_id,parent_id);

create table if not exists nmc3.model_turns (
  invocation_id text not null references nmc3.agent_invocations(id) on delete cascade,
  ordinal integer not null check (ordinal > 0),
  provider text,
  model text,
  action_type text,
  usage jsonb,
  error jsonb,
  occurred_at timestamptz not null default now(),
  primary key(invocation_id,ordinal)
);

create table if not exists nmc3.tool_invocations (
  id text primary key,
  invocation_id text not null references nmc3.agent_invocations(id) on delete cascade,
  tool_id text not null,
  tool_version text not null,
  state text not null check (state in ('RUNNING','SUCCEEDED','FAILED','CANCELLED')),
  snapshot jsonb not null,
  started_at timestamptz not null,
  finished_at timestamptz
);

create table if not exists nmc3.run_events (
  run_id text not null references nmc3.runs(id) on delete cascade,
  sequence bigint not null check (sequence > 0),
  invocation_id text,
  event_type text not null,
  occurred_at timestamptz not null,
  data jsonb,
  primary key(run_id,sequence)
);

-- The Go service connects with a dedicated private database role. These tables
-- are outside Supabase's exposed public schema; no browser role receives grants.
