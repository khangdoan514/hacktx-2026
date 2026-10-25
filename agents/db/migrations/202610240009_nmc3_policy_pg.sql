-- Pass 4 Postgres store tables that are not approval rows.
-- Approvals, decisions, write attempts, agent_policies, and policy_audit
-- stay in 202610240004_nmc3_approvals.sql. Secret values are not columns.

create schema if not exists nmc3;

create table if not exists nmc3.policy_definitions (
  tenant_id text not null,
  agent_id text not null,
  version text not null,
  allowed_tools text[] not null default '{}',
  allowed_delegates text[] not null default '{}',
  primary key (tenant_id, agent_id, version)
);

create table if not exists nmc3.policy_runs (
  id text primary key,
  tenant_id text not null,
  requester_id text not null,
  agent_id text not null,
  version text not null,
  model_id text not null,
  policy_id text not null,
  policy_revision integer not null check (policy_revision > 0),
  idempotency_key text not null default '',
  state text not null check (state in (
    'PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'WAITING'
  )),
  resume_state text not null default '' check (resume_state in (
    '', 'PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'WAITING'
  )),
  hold text not null default '' check (hold in (
    '', 'WAITING', 'APPROVED', 'DENIED', 'EXPIRED'
  )),
  max_tokens integer not null default 0 check (max_tokens >= 0),
  max_cost_usd double precision not null default 0 check (
    max_cost_usd >= 0
    and max_cost_usd < 'Infinity'::double precision
    and max_cost_usd <> 'NaN'::double precision
  ),
  max_model_turns integer not null default 0 check (max_model_turns >= 0),
  created_at timestamptz not null
);

create unique index if not exists policy_runs_idempotency_idx
  on nmc3.policy_runs (tenant_id, idempotency_key)
  where idempotency_key <> '';

create table if not exists nmc3.policy_secret_refs (
  tenant_id text not null,
  name text not null,
  run_id text not null default '',
  primary key (tenant_id, name)
);

create table if not exists nmc3.policy_contexts (
  id text primary key,
  tenant_id text not null,
  run_id text not null,
  summary text not null
);

create table if not exists nmc3.policy_events (
  id bigint generated always as identity primary key,
  run_id text not null,
  sequence bigint not null check (sequence >= 0),
  event_type text not null,
  data text not null default ''
);

create index if not exists policy_events_run_idx
  on nmc3.policy_events (run_id, id);

create index if not exists policy_write_approval_idx
  on nmc3.write_attempts (approval_id);

revoke all on nmc3.policy_definitions, nmc3.policy_runs, nmc3.policy_secret_refs,
  nmc3.policy_contexts, nmc3.policy_events from public, anon, authenticated;
grant select, insert, update on nmc3.policy_definitions to service_role;
grant select, insert, update on nmc3.policy_runs to service_role;
grant select, insert, update on nmc3.policy_secret_refs to service_role;
grant select, insert, update on nmc3.policy_contexts to service_role;
grant select, insert on nmc3.policy_events to service_role;
grant usage, select on sequence nmc3.policy_events_id_seq to service_role;
