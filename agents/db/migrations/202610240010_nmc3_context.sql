-- Pass 5 runtime context grants and retained snapshots.
-- Apply this file on its own. It does not alter runs, events, or definitions.
-- Artifact and memory bodies stay in the process object store. The snapshot
-- copies fragment text at resolve time so a later revocation stays explainable.
-- No pgvector extension, embedding column, or knowledge-chunk table.

create schema if not exists nmc3;
revoke all on schema nmc3 from public;

create table if not exists nmc3.context_grants (
  id text primary key check (id <> ''),
  tenant_id text not null check (tenant_id <> ''),
  principal_id text not null check (principal_id <> ''),
  agent_id text not null check (agent_id <> ''),
  scope text not null check (scope in ('step', 'run', 'agent_memory', 'organization')),
  resource_id text not null check (resource_id <> ''),
  purposes text[] not null check (
    cardinality(purposes) > 0
    and purposes <@ array[
      'resolve', 'retrieve', 'read_memory', 'write_memory', 'delete_memory',
      'write_artifact', 'delete_artifact', 'inspect'
    ]::text[]
  ),
  expires_at timestamptz,
  revoked_at timestamptz,
  revoked_by text not null default '',
  created_at timestamptz not null default now()
);

create index if not exists context_grants_lookup_idx
  on nmc3.context_grants (tenant_id, principal_id, agent_id, scope, resource_id);
create index if not exists context_grants_tenant_idx
  on nmc3.context_grants (tenant_id, id);

create table if not exists nmc3.context_snapshots (
  id text primary key check (id <> ''),
  version integer not null check (version in (0, 1)),
  tenant_id text not null check (tenant_id <> ''),
  run_id text not null check (run_id <> ''),
  invocation_id text not null default '',
  agent_id text not null default '',
  document jsonb not null check (jsonb_typeof(document) = 'object'),
  retain_until timestamptz not null,
  redacted_at timestamptz,
  created_at timestamptz not null
);

create index if not exists context_snapshots_run_idx
  on nmc3.context_snapshots (tenant_id, run_id, created_at, id);
create index if not exists context_snapshots_retention_idx
  on nmc3.context_snapshots (tenant_id, retain_until)
  where redacted_at is null;

revoke all on table nmc3.context_grants from public;
revoke all on table nmc3.context_snapshots from public;

do $$
begin
  if exists (select 1 from pg_roles where rolname = 'anon') then
    execute 'revoke all on table nmc3.context_grants from anon';
    execute 'revoke all on table nmc3.context_snapshots from anon';
  end if;
  if exists (select 1 from pg_roles where rolname = 'authenticated') then
    execute 'revoke all on table nmc3.context_grants from authenticated';
    execute 'revoke all on table nmc3.context_snapshots from authenticated';
  end if;
  if exists (select 1 from pg_roles where rolname = 'service_role') then
    execute 'grant usage on schema nmc3 to service_role';
    execute 'grant select, insert, update, delete on nmc3.context_grants, nmc3.context_snapshots to service_role';
  end if;
end
$$;
