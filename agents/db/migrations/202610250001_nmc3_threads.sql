-- Durable chat thread index and snapshots. OpenCode session IDs remain the
-- execution handle; this table lets the product list and recover transcripts.
create schema if not exists nmc3;
revoke all on schema nmc3 from public;

create table if not exists nmc3.threads (
  id text primary key check (id <> ''),
  tenant_id text not null default 'local',
  principal_id text not null default 'local',
  title text not null,
  transcript jsonb not null default '[]'::jsonb check (jsonb_typeof(transcript) = 'array'),
  busy boolean not null default false,
  archived_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index if not exists threads_owner_updated_idx
  on nmc3.threads (tenant_id, principal_id, updated_at desc) where archived_at is null;
create table if not exists nmc3.thread_files (
  id text primary key check (id <> ''),
  thread_id text not null references nmc3.threads(id) on delete cascade,
  tenant_id text not null,
  principal_id text not null,
  object_key text not null unique,
  file_name text not null,
  content_type text not null,
  byte_size bigint not null check (byte_size >= 0),
  checksum_sha256 text not null default '',
  created_at timestamptz not null default now()
);
create index if not exists thread_files_owner_idx on nmc3.thread_files(thread_id, tenant_id, principal_id);
revoke all on table nmc3.threads from public;
revoke all on table nmc3.thread_files from public;
do $$ begin
  if exists (select 1 from pg_roles where rolname = 'anon') then
    execute 'revoke all on table nmc3.threads from anon';
    execute 'revoke all on table nmc3.thread_files from anon';
  end if;
  if exists (select 1 from pg_roles where rolname = 'authenticated') then
    execute 'revoke all on table nmc3.threads from authenticated';
    execute 'revoke all on table nmc3.thread_files from authenticated';
  end if;
end $$;
