-- Scheduled agent runs. An automation stores the StartRunRequest it submits;
-- each firing links the schedule to the run it started (or the reason it did
-- not start). Run snapshots stay in nmc3.runs.
create schema if not exists nmc3;
revoke all on schema nmc3 from public;

create table if not exists nmc3.automations (
  id text primary key check (id <> ''),
  tenant_id text not null,
  created_by text not null,
  created_by_email text not null default '',
  name text not null check (name <> ''),
  schedule text not null,
  timezone text not null default 'UTC',
  enabled boolean not null default true,
  request jsonb not null check (jsonb_typeof(request) = 'object'),
  next_run_at timestamptz,
  last_fired_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index if not exists automations_tenant_idx on nmc3.automations (tenant_id, created_at desc);
create index if not exists automations_due_idx on nmc3.automations (next_run_at) where enabled;

create table if not exists nmc3.automation_firings (
  id text primary key check (id <> ''),
  automation_id text not null references nmc3.automations(id) on delete cascade,
  tenant_id text not null,
  trigger text not null check (trigger in ('schedule', 'manual')),
  scheduled_for timestamptz not null,
  fired_at timestamptz not null default now(),
  run_id text,
  error text not null default ''
);
create index if not exists automation_firings_automation_idx on nmc3.automation_firings (automation_id, fired_at desc);
create index if not exists automation_firings_tenant_idx on nmc3.automation_firings (tenant_id, fired_at desc);

revoke all on table nmc3.automations from public;
revoke all on table nmc3.automation_firings from public;
do $$ begin
  if exists (select 1 from pg_roles where rolname = 'anon') then
    execute 'revoke all on table nmc3.automations from anon';
    execute 'revoke all on table nmc3.automation_firings from anon';
  end if;
  if exists (select 1 from pg_roles where rolname = 'authenticated') then
    execute 'revoke all on table nmc3.automations from authenticated';
    execute 'revoke all on table nmc3.automation_firings from authenticated';
  end if;
end $$;
