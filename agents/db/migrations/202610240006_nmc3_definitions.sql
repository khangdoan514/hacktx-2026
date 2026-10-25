-- Pass 2 definition registry.
-- Published agent versions, tool versions, and run pins are immutable.
-- All rows belong to one internal tenant until Pass 4 adds enforcement.
-- Pass 6 may add tool columns and side-effect classes, but must not rewrite
-- an existing tool version, published spec, content hash, or run pin.

create schema if not exists nmc3;
revoke all on schema nmc3 from public;

create table if not exists nmc3.agent_definitions (
  tenant_id text not null check (tenant_id <> '' and char_length(tenant_id) <= 256),
  id text not null check (id <> '' and char_length(id) <= 256),
  name text not null check (name <> '' and char_length(name) <= 200),
  lifecycle_state text not null check (lifecycle_state in ('draft', 'testing', 'published', 'retired')),
  created_by text not null check (created_by <> ''),
  created_at timestamptz not null,
  updated_at timestamptz not null,
  primary key (tenant_id, id),
  unique (tenant_id, name)
);

create table if not exists nmc3.agent_versions (
  tenant_id text not null,
  agent_id text not null,
  version text not null check (version <> '' and char_length(version) <= 128),
  schema_version text not null check (schema_version <> ''),
  spec jsonb not null check (
    spec->>'id' = agent_id
    and spec->>'version' = version
    and jsonb_typeof(spec) = 'object'
  ),
  content_hash text not null check (content_hash ~ '^[0-9a-f]{64}$'),
  lifecycle_state text not null check (lifecycle_state in ('draft', 'testing', 'published', 'retired')),
  created_by text not null check (created_by <> ''),
  created_at timestamptz not null,
  updated_at timestamptz not null,
  tested_by text,
  tested_at timestamptz,
  published_by text,
  published_at timestamptz,
  retired_by text,
  retired_at timestamptz,
  primary key (tenant_id, agent_id, version),
  foreign key (tenant_id, agent_id) references nmc3.agent_definitions (tenant_id, id),
  check (
    (lifecycle_state = 'published' and published_at is not null and published_by is not null and retired_at is null and retired_by is null)
    or (lifecycle_state = 'retired' and published_at is not null and published_by is not null and retired_at is not null and retired_by is not null)
    or (lifecycle_state = 'testing' and tested_at is not null and tested_by is not null and published_at is null and published_by is null and retired_at is null and retired_by is null)
    or (lifecycle_state = 'draft' and tested_at is null and tested_by is null and published_at is null and published_by is null and retired_at is null and retired_by is null)
  )
);
create index if not exists agent_versions_state_idx
  on nmc3.agent_versions (tenant_id, lifecycle_state, agent_id);
create index if not exists agent_definitions_created_idx
  on nmc3.agent_definitions (tenant_id, created_at);

-- Tool identity is separate from immutable versions so Pass 6 can append a
-- version without altering catalog.search 1.0.0. side_effect is unconstrained
-- here; this pass only writes READ_ONLY.
create table if not exists nmc3.tool_definitions (
  tenant_id text not null,
  id text not null check (id <> '' and char_length(id) <= 256),
  created_at timestamptz not null,
  primary key (tenant_id, id)
);

create table if not exists nmc3.tool_versions (
  tenant_id text not null,
  tool_id text not null,
  version text not null check (version <> '' and char_length(version) <= 128),
  spec jsonb not null check (
    spec->>'id' = tool_id
    and spec->>'version' = version
    and spec->>'side_effect' = side_effect
    and (spec->>'timeout_seconds')::integer = timeout_seconds
    and jsonb_typeof(spec) = 'object'
  ),
  content_hash text not null check (content_hash ~ '^[0-9a-f]{64}$'),
  side_effect text not null check (side_effect <> ''),
  timeout_seconds integer not null check (timeout_seconds > 0),
  created_at timestamptz not null,
  primary key (tenant_id, tool_id, version),
  foreign key (tenant_id, tool_id) references nmc3.tool_definitions (tenant_id, id)
);

-- A pin is the spec a run actually used. It stays after a later publish or retire.
-- No cascade delete: versions are not removed.
create table if not exists nmc3.run_definition_pins (
  run_id text not null check (run_id <> '' and char_length(run_id) <= 256),
  tenant_id text not null,
  agent_id text not null,
  version text not null,
  content_hash text not null check (content_hash ~ '^[0-9a-f]{64}$'),
  spec jsonb not null check (
    spec->>'id' = agent_id
    and spec->>'version' = version
    and jsonb_typeof(spec) = 'object'
  ),
  pinned_at timestamptz not null,
  primary key (run_id, tenant_id, agent_id),
  foreign key (tenant_id, agent_id, version)
    references nmc3.agent_versions (tenant_id, agent_id, version)
);
create index if not exists run_definition_pins_version_idx
  on nmc3.run_definition_pins (tenant_id, agent_id, version);

create or replace function nmc3.prevent_definition_identity_mutation()
returns trigger
language plpgsql
set search_path = ''
as $$
begin
  if new.tenant_id is distinct from old.tenant_id
     or new.id is distinct from old.id
     or new.created_by is distinct from old.created_by
     or new.created_at is distinct from old.created_at
  then
    raise exception 'agent definition identity is immutable' using errcode = '55000';
  end if;
  return new;
end;
$$;

create or replace function nmc3.prevent_definition_version_mutation()
returns trigger
language plpgsql
set search_path = ''
as $$
begin
  if new.tenant_id is distinct from old.tenant_id
     or new.agent_id is distinct from old.agent_id
     or new.version is distinct from old.version
     or new.created_by is distinct from old.created_by
     or new.created_at is distinct from old.created_at
  then
    raise exception 'definition version identity is immutable' using errcode = '55000';
  end if;

  if old.lifecycle_state = 'retired' then
    raise exception 'retired definition version is immutable' using errcode = '55000';
  end if;

  if old.lifecycle_state = 'published' then
    if new.lifecycle_state = 'retired'
       and new.spec = old.spec
       and new.content_hash = old.content_hash
       and new.schema_version = old.schema_version
       and new.published_by is not distinct from old.published_by
       and new.published_at is not distinct from old.published_at
       and new.tested_by is not distinct from old.tested_by
       and new.tested_at is not distinct from old.tested_at
    then
      return new;
    end if;
    raise exception 'published definition version is immutable' using errcode = '55000';
  end if;

  if old.lifecycle_state in ('draft', 'testing')
     and new.lifecycle_state in ('draft', 'testing', 'published')
  then
    return new;
  end if;

  raise exception 'invalid definition lifecycle transition' using errcode = '22023';
end;
$$;

create or replace function nmc3.prevent_row_mutation()
returns trigger
language plpgsql
set search_path = ''
as $$
begin
  raise exception 'row is immutable' using errcode = '55000';
end;
$$;

drop trigger if exists agent_definitions_identity on nmc3.agent_definitions;
create trigger agent_definitions_identity
  before update on nmc3.agent_definitions
  for each row execute function nmc3.prevent_definition_identity_mutation();

drop trigger if exists agent_versions_immutable on nmc3.agent_versions;
create trigger agent_versions_immutable
  before update on nmc3.agent_versions
  for each row execute function nmc3.prevent_definition_version_mutation();

drop trigger if exists tool_definitions_immutable on nmc3.tool_definitions;
create trigger tool_definitions_immutable
  before update or delete on nmc3.tool_definitions
  for each row execute function nmc3.prevent_row_mutation();

drop trigger if exists tool_versions_immutable on nmc3.tool_versions;
create trigger tool_versions_immutable
  before update or delete on nmc3.tool_versions
  for each row execute function nmc3.prevent_row_mutation();

drop trigger if exists run_definition_pins_immutable on nmc3.run_definition_pins;
create trigger run_definition_pins_immutable
  before update or delete on nmc3.run_definition_pins
  for each row execute function nmc3.prevent_row_mutation();

revoke all on table nmc3.agent_definitions from public;
revoke all on table nmc3.agent_versions from public;
revoke all on table nmc3.tool_definitions from public;
revoke all on table nmc3.tool_versions from public;
revoke all on table nmc3.run_definition_pins from public;
revoke all on function nmc3.prevent_definition_identity_mutation() from public;
revoke all on function nmc3.prevent_definition_version_mutation() from public;
revoke all on function nmc3.prevent_row_mutation() from public;

do $$
begin
  if exists (select 1 from pg_roles where rolname = 'anon') then
    execute 'revoke all on table nmc3.agent_definitions from anon';
    execute 'revoke all on table nmc3.agent_versions from anon';
    execute 'revoke all on table nmc3.tool_definitions from anon';
    execute 'revoke all on table nmc3.tool_versions from anon';
    execute 'revoke all on table nmc3.run_definition_pins from anon';
    execute 'revoke all on function nmc3.prevent_definition_identity_mutation() from anon';
    execute 'revoke all on function nmc3.prevent_definition_version_mutation() from anon';
    execute 'revoke all on function nmc3.prevent_row_mutation() from anon';
  end if;
  if exists (select 1 from pg_roles where rolname = 'authenticated') then
    execute 'revoke all on table nmc3.agent_definitions from authenticated';
    execute 'revoke all on table nmc3.agent_versions from authenticated';
    execute 'revoke all on table nmc3.tool_definitions from authenticated';
    execute 'revoke all on table nmc3.tool_versions from authenticated';
    execute 'revoke all on table nmc3.run_definition_pins from authenticated';
    execute 'revoke all on function nmc3.prevent_definition_identity_mutation() from authenticated';
    execute 'revoke all on function nmc3.prevent_definition_version_mutation() from authenticated';
    execute 'revoke all on function nmc3.prevent_row_mutation() from authenticated';
  end if;
  if exists (select 1 from pg_roles where rolname = 'service_role') then
    execute 'grant select, insert, update on nmc3.agent_definitions, nmc3.agent_versions to service_role';
    execute 'grant select, insert on nmc3.tool_definitions, nmc3.tool_versions, nmc3.run_definition_pins to service_role';
  end if;
end;
$$;
