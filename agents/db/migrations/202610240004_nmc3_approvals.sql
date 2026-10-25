-- Pass 4 approval requests, decisions, and protected-write attempts.
-- Employee identity stays in app.employees / app.role_grants. These tables do
-- not copy profiles or secrets. The Go policy package enforces the same
-- states in process; these constraints are the durable backstop.

create table if not exists nmc3.agent_policies (
  id text not null,
  tenant_id text not null,
  revision integer not null check (revision > 0),
  allowed_models text[] not null default '{}',
  allowed_tools text[] not null default '{}',
  allowed_delegates text[] not null default '{}',
  protected_tools text[] not null default '{}',
  approver_roles text[] not null default '{}',
  max_tokens integer not null default 0 check (max_tokens >= 0),
  max_cost_usd double precision not null default 0 check (
    max_cost_usd >= 0
    and max_cost_usd < 'Infinity'::double precision
    and max_cost_usd <> 'NaN'::double precision
  ),
  max_model_turns integer not null default 0 check (max_model_turns >= 0),
  approval_ttl_seconds integer not null default 0 check (approval_ttl_seconds >= 0),
  escalation_role text not null default '',
  allow_service_approval boolean not null default false,
  created_by text not null,
  created_at timestamptz not null,
  primary key (tenant_id, id, revision)
);

create table if not exists nmc3.approval_requests (
  id text primary key,
  tenant_id text not null,
  run_id text not null,
  invocation_id text not null default '',
  tool_id text not null,
  tool_version text not null,
  idempotency_key text not null,
  policy_id text not null,
  policy_revision integer not null check (policy_revision > 0),
  requester_id text not null,
  state text not null check (state in ('WAITING', 'APPROVED', 'DENIED', 'EXPIRED')),
  input_digest text not null,
  expires_at timestamptz not null,
  created_at timestamptz not null,
  unique (tenant_id, idempotency_key),
  unique (id, tenant_id, requester_id),
  unique (id, tenant_id, run_id, idempotency_key),
  foreign key (tenant_id, policy_id, policy_revision)
    references nmc3.agent_policies (tenant_id, id, revision)
);

create index if not exists approval_inbox_idx
  on nmc3.approval_requests (tenant_id, state, expires_at);

-- One decision row per request. approver_id cannot equal the request's requester.
create table if not exists nmc3.approval_decisions (
  id text primary key,
  request_id text not null,
  tenant_id text not null,
  approver_id text not null,
  requester_id text not null,
  outcome text not null check (outcome in ('APPROVED', 'DENIED')),
  decided_at timestamptz not null,
  unique (request_id),
  check (approver_id <> requester_id),
  foreign key (request_id, tenant_id, requester_id)
    references nmc3.approval_requests (id, tenant_id, requester_id)
);

create index if not exists approval_decider_idx
  on nmc3.approval_decisions (tenant_id, approver_id, decided_at);

-- dispatch_count stays 0 until the single transition to DISPATCHED, then stays 1.
create table if not exists nmc3.write_attempts (
  id text primary key,
  tenant_id text not null,
  idempotency_key text not null,
  run_id text not null,
  invocation_id text not null default '',
  tool_id text not null,
  tool_version text not null,
  approval_id text not null,
  state text not null check (state in (
    'WAITING', 'READY', 'DISPATCHED', 'SUCCEEDED', 'FAILED',
    'UNCERTAIN', 'RECONCILED', 'DENIED', 'EXPIRED'
  )),
  dispatch_count integer not null default 0 check (dispatch_count >= 0),
  dispatched_at timestamptz,
  outcome text not null default '',
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (tenant_id, idempotency_key),
  foreign key (approval_id, tenant_id, run_id, idempotency_key)
    references nmc3.approval_requests (id, tenant_id, run_id, idempotency_key),
  check (
    (
      state in ('WAITING', 'READY', 'DENIED', 'EXPIRED')
      and dispatch_count = 0
      and dispatched_at is null
    )
    or (
      state in ('DISPATCHED', 'SUCCEEDED', 'FAILED', 'UNCERTAIN', 'RECONCILED')
      and dispatch_count = 1
      and dispatched_at is not null
    )
  )
);

create index if not exists write_attempts_state_idx
  on nmc3.write_attempts (tenant_id, state, updated_at);

create table if not exists nmc3.policy_audit (
  id text primary key,
  tenant_id text not null,
  actor_id text not null,
  action text not null,
  resource_kind text not null,
  resource_id text not null,
  outcome text not null,
  code text not null default '',
  detail text not null default '',
  occurred_at timestamptz not null
);

create index if not exists policy_audit_tenant_idx
  on nmc3.policy_audit (tenant_id, occurred_at);
create index if not exists policy_audit_resource_idx
  on nmc3.policy_audit (tenant_id, resource_id);

revoke all on nmc3.agent_policies, nmc3.approval_requests, nmc3.approval_decisions,
  nmc3.write_attempts, nmc3.policy_audit from public, anon, authenticated;
grant select, insert, update on nmc3.agent_policies to service_role;
grant select, insert, update on nmc3.approval_requests to service_role;
grant select, insert on nmc3.approval_decisions to service_role;
grant select, insert, update on nmc3.write_attempts to service_role;
grant select, insert on nmc3.policy_audit to service_role;
