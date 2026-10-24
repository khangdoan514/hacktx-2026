-- Application schema. Business tables stay in app so the Data API
-- (public/graphql_public) cannot mutate them.
-- auth.users must already exist (Supabase, or supabase/bootstrap_local.sql).

CREATE SCHEMA IF NOT EXISTS app;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app_runtime') THEN
    CREATE ROLE app_runtime NOLOGIN NOBYPASSRLS;
  END IF;
END
$$;

GRANT app_runtime TO CURRENT_USER;

CREATE OR REPLACE FUNCTION app.current_user_id() RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.user_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION app.current_user_email() RETURNS text
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.user_email', true), '')
$$;

CREATE OR REPLACE FUNCTION app.touch_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END
$$;

CREATE TABLE app.profiles (
    id uuid PRIMARY KEY REFERENCES auth.users (id) ON DELETE RESTRICT,
    display_name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    timezone text NOT NULL DEFAULT 'UTC',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.local_credentials (
    user_id uuid PRIMARY KEY REFERENCES app.profiles (id) ON DELETE CASCADE,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    timezone text NOT NULL DEFAULT 'UTC',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.workspace_memberships (
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    access_role text NOT NULL CHECK (access_role IN ('owner', 'administrator', 'member', 'guest')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX workspace_memberships_user_idx ON app.workspace_memberships (user_id, status);

CREATE TABLE app.departments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE app.functional_roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    code text NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, code)
);

CREATE TABLE app.member_functional_roles (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role_id uuid NOT NULL REFERENCES app.functional_roles (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id, role_id),
    FOREIGN KEY (workspace_id, user_id)
      REFERENCES app.workspace_memberships (workspace_id, user_id) ON DELETE CASCADE
);

CREATE TABLE app.partner_organizations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.partner_contacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES app.partner_organizations (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    name text NOT NULL,
    email text NOT NULL,
    user_id uuid REFERENCES app.profiles (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid,
    partner_id uuid REFERENCES app.partner_organizations (id) ON DELETE SET NULL,
    email text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    access_role text NOT NULL CHECK (access_role IN ('owner', 'administrator', 'member', 'guest')),
    project_role text CHECK (project_role IS NULL OR project_role IN ('owner', 'contributor', 'reviewer', 'viewer', 'partner')),
    invited_by uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'accepted', 'revoked', 'expired')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    consumed_by uuid REFERENCES app.profiles (id) ON DELETE SET NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    parent_id uuid REFERENCES app.projects (id) ON DELETE RESTRICT,
    partner_id uuid REFERENCES app.partner_organizations (id) ON DELETE SET NULL,
    name text NOT NULL,
    project_type text NOT NULL CHECK (project_type IN ('internal', 'partner')),
    objective text NOT NULL DEFAULT '',
    constraints text NOT NULL DEFAULT '',
    scope text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'archived', 'closed')),
    owner_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    starts_on date,
    ends_on date,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (project_type <> 'partner' OR partner_id IS NOT NULL)
);

CREATE TABLE app.project_memberships (
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    access_role text NOT NULL CHECK (access_role IN ('owner', 'contributor', 'reviewer', 'viewer', 'partner')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX project_memberships_user_idx ON app.project_memberships (user_id, status);

CREATE TABLE app.project_internal (
    project_id uuid PRIMARY KEY REFERENCES app.projects (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    internal_notes text NOT NULL DEFAULT '',
    internal_cost numeric
);

ALTER TABLE app.invitations
  ADD CONSTRAINT invitations_project_fk
  FOREIGN KEY (project_id) REFERENCES app.projects (id) ON DELETE CASCADE;

CREATE TABLE app.requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid REFERENCES app.projects (id) ON DELETE SET NULL,
    requester_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    title text NOT NULL,
    state text NOT NULL DEFAULT 'draft' CHECK (state IN (
      'draft', 'submitted', 'triaging', 'needs_information', 'accepted', 'rejected'
    )),
    converted_project_id uuid UNIQUE REFERENCES app.projects (id) ON DELETE SET NULL,
    converted_requirement_id uuid,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.request_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id uuid NOT NULL REFERENCES app.requests (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    revision integer NOT NULL CHECK (revision > 0),
    outcome text NOT NULL DEFAULT '',
    constraints text NOT NULL DEFAULT '',
    due_on date,
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (request_id, revision)
);

CREATE TABLE app.requirements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    request_id uuid REFERENCES app.requests (id) ON DELETE SET NULL,
    title text NOT NULL,
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'in_review', 'accepted', 'changes_requested')),
    accepted_revision integer,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE app.requests
  ADD CONSTRAINT requests_converted_requirement_fk
  FOREIGN KEY (converted_requirement_id) REFERENCES app.requirements (id) ON DELETE SET NULL;

CREATE TABLE app.requirement_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    requirement_id uuid NOT NULL REFERENCES app.requirements (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    revision integer NOT NULL CHECK (revision > 0),
    body text NOT NULL DEFAULT '',
    acceptance_criteria text NOT NULL DEFAULT '',
    priority text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high')),
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (requirement_id, revision)
);

CREATE TABLE app.tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    parent_id uuid REFERENCES app.tasks (id) ON DELETE RESTRICT,
    requirement_id uuid REFERENCES app.requirements (id) ON DELETE SET NULL,
    title text NOT NULL,
    brief text NOT NULL DEFAULT '',
    acceptance_criteria text NOT NULL DEFAULT '',
    assignee_id uuid REFERENCES app.profiles (id) ON DELETE SET NULL,
    priority integer NOT NULL DEFAULT 3 CHECK (priority BETWEEN 1 AND 5),
    due_on date,
    state text NOT NULL DEFAULT 'todo' CHECK (state IN (
      'todo', 'in_progress', 'in_review', 'changes_requested', 'done', 'cancelled'
    )),
    blocked_reason text NOT NULL DEFAULT '',
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tasks_assignee_idx ON app.tasks (assignee_id, state);
CREATE INDEX tasks_project_idx ON app.tasks (project_id, state, updated_at, id);

CREATE TABLE app.task_dependencies (
    task_id uuid NOT NULL REFERENCES app.tasks (id) ON DELETE CASCADE,
    depends_on uuid NOT NULL REFERENCES app.tasks (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, depends_on),
    CHECK (task_id <> depends_on)
);

CREATE TABLE app.handoffs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    state text NOT NULL DEFAULT 'draft' CHECK (state IN (
      'draft', 'sent', 'acknowledged', 'needs_information', 'completed', 'cancelled'
    )),
    recipient_id uuid REFERENCES app.profiles (id) ON DELETE RESTRICT,
    accountable_id uuid REFERENCES app.profiles (id) ON DELETE RESTRICT,
    due_on date,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    current_revision integer NOT NULL DEFAULT 0,
    created_by uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.handoff_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    handoff_id uuid NOT NULL REFERENCES app.handoffs (id) ON DELETE CASCADE,
    revision integer NOT NULL CHECK (revision > 0),
    snapshot jsonb NOT NULL,
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (handoff_id, revision)
);

CREATE TABLE app.handoff_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    handoff_id uuid NOT NULL REFERENCES app.handoffs (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    event_type text NOT NULL,
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.approval_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid REFERENCES app.projects (id) ON DELETE CASCADE,
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    target_revision integer NOT NULL,
    author_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    reviewer_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN (
      'pending', 'approved', 'rejected', 'changes_requested', 'cancelled', 'superseded'
    )),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX approval_reviewer_idx ON app.approval_requests (reviewer_id, state);

CREATE TABLE app.approval_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_id uuid NOT NULL REFERENCES app.approval_requests (id) ON DELETE CASCADE,
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('approved', 'rejected', 'changes_requested')),
    comment text NOT NULL DEFAULT '',
    target_revision integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    title text NOT NULL,
    visibility text NOT NULL DEFAULT 'internal' CHECK (visibility IN ('internal', 'shared')),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.document_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid NOT NULL REFERENCES app.documents (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    revision integer NOT NULL CHECK (revision > 0),
    storage_key text NOT NULL UNIQUE,
    filename text NOT NULL,
    mime_type text NOT NULL,
    byte_size bigint NOT NULL DEFAULT 0 CHECK (byte_size >= 0),
    checksum text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'ready', 'failed')),
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_id, revision)
);

CREATE TABLE app.comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    visibility text NOT NULL DEFAULT 'internal' CHECK (visibility IN ('internal', 'shared')),
    body text NOT NULL,
    author_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE app.notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    recipient_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE CASCADE,
    source_event_id uuid NOT NULL,
    kind text NOT NULL,
    target_type text NOT NULL DEFAULT '',
    target_id uuid,
    preview text NOT NULL DEFAULT '',
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_event_id, recipient_id)
);

CREATE INDEX notifications_recipient_idx ON app.notifications (recipient_id, read_at, created_at);

CREATE TABLE app.notification_preferences (
    user_id uuid PRIMARY KEY REFERENCES app.profiles (id) ON DELETE CASCADE,
    in_app boolean NOT NULL DEFAULT true
);

CREATE TABLE app.audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid REFERENCES app.projects (id) ON DELETE SET NULL,
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id uuid NOT NULL,
    revision integer,
    request_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_events_workspace_idx ON app.audit_events (workspace_id, created_at, id);

CREATE TABLE app.outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid REFERENCES app.projects (id) ON DELETE SET NULL,
    resource_type text NOT NULL,
    resource_id uuid NOT NULL,
    revision integer,
    kind text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    dispatched_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX outbox_pending_idx ON app.outbox_events (dispatched_at, created_at);

CREATE TABLE app.idempotency_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE CASCADE,
    scope text NOT NULL,
    key text NOT NULL,
    request_hash text NOT NULL,
    status_code integer NOT NULL,
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, scope, key)
);

CREATE TABLE app.datasets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    owner_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.dataset_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dataset_id uuid NOT NULL REFERENCES app.datasets (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    revision integer NOT NULL CHECK (revision > 0),
    state text NOT NULL CHECK (state IN ('validated', 'rejected')),
    summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (dataset_id, revision)
);

CREATE TABLE app.dataset_rows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id uuid NOT NULL REFERENCES app.dataset_versions (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    row_number integer NOT NULL CHECK (row_number > 0),
    partner_ref text NOT NULL DEFAULT '',
    values jsonb NOT NULL,
    quarantined boolean NOT NULL DEFAULT false,
    quarantine_reason text NOT NULL DEFAULT '',
    UNIQUE (version_id, row_number)
);

CREATE TABLE app.metric_definitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    name text NOT NULL,
    owner_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.metric_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    metric_id uuid NOT NULL REFERENCES app.metric_definitions (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    revision integer NOT NULL CHECK (revision > 0),
    formula text NOT NULL CHECK (formula IN ('gross_margin')),
    numerator text NOT NULL DEFAULT 'revenue - included_cost',
    denominator text NOT NULL DEFAULT 'revenue',
    unit text NOT NULL DEFAULT 'ratio',
    period text NOT NULL DEFAULT '',
    exclusions jsonb NOT NULL DEFAULT '[]'::jsonb,
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'accepted')),
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (metric_id, revision)
);

CREATE TABLE app.data_questions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    dataset_id uuid REFERENCES app.datasets (id) ON DELETE SET NULL,
    metric_id uuid REFERENCES app.metric_definitions (id) ON DELETE SET NULL,
    analysis_id uuid,
    requester_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    assignee_id uuid REFERENCES app.profiles (id) ON DELETE SET NULL,
    question text NOT NULL,
    state text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'answered', 'accepted')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.question_responses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id uuid NOT NULL REFERENCES app.data_questions (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    body text NOT NULL,
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    accepted boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.analyses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    title text NOT NULL,
    question text NOT NULL DEFAULT '',
    created_by uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE app.data_questions
  ADD CONSTRAINT data_questions_analysis_fk
  FOREIGN KEY (analysis_id) REFERENCES app.analyses (id) ON DELETE SET NULL;

CREATE TABLE app.analysis_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    analysis_id uuid NOT NULL REFERENCES app.analyses (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    revision integer NOT NULL CHECK (revision > 0),
    dataset_version_id uuid NOT NULL REFERENCES app.dataset_versions (id) ON DELETE RESTRICT,
    metric_version_id uuid NOT NULL REFERENCES app.metric_versions (id) ON DELETE RESTRICT,
    parameters jsonb NOT NULL,
    output jsonb NOT NULL,
    quality jsonb NOT NULL,
    stale boolean NOT NULL DEFAULT false,
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (analysis_id, revision)
);

CREATE TABLE app.dashboards (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    title text NOT NULL,
    created_by uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.dashboard_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id uuid NOT NULL REFERENCES app.dashboards (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES app.workspaces (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES app.projects (id) ON DELETE CASCADE,
    revision integer NOT NULL CHECK (revision > 0),
    base_revision integer,
    definition jsonb NOT NULL,
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'published')),
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (dashboard_id, revision)
);

-- Membership helpers are SECURITY DEFINER so policies can read grants
-- without recursive RLS. They return roles, not business payloads.

CREATE OR REPLACE FUNCTION app.workspace_role(wid uuid) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT m.access_role
  FROM app.workspace_memberships m
  WHERE m.workspace_id = wid
    AND m.user_id = app.current_user_id()
    AND m.status = 'active'
$$;

CREATE OR REPLACE FUNCTION app.is_workspace_member(wid uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT app.workspace_role(wid) IS NOT NULL
$$;

CREATE OR REPLACE FUNCTION app.workspace_has_active_owner(wid uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT EXISTS (
    SELECT 1 FROM app.workspace_memberships m
    WHERE m.workspace_id = wid AND m.access_role = 'owner' AND m.status = 'active'
  )
$$;

CREATE OR REPLACE FUNCTION app.project_role(pid uuid) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT m.access_role
  FROM app.project_memberships m
  WHERE m.project_id = pid
    AND m.user_id = app.current_user_id()
    AND m.status = 'active'
$$;

CREATE OR REPLACE FUNCTION app.can_read_project(pid uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT app.project_role(pid) IS NOT NULL
$$;

CREATE OR REPLACE FUNCTION app.can_write_project(pid uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT app.project_role(pid) IN ('owner', 'contributor')
$$;

CREATE OR REPLACE FUNCTION app.is_internal_project_member(pid uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT app.project_role(pid) IN ('owner', 'contributor', 'reviewer', 'viewer')
$$;

CREATE OR REPLACE FUNCTION app.project_has_active_owner(pid uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT EXISTS (
    SELECT 1 FROM app.project_memberships m
    WHERE m.project_id = pid AND m.access_role = 'owner' AND m.status = 'active'
  )
$$;

CREATE OR REPLACE FUNCTION app.project_workspace_id(pid uuid) RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT workspace_id FROM app.projects WHERE id = pid
$$;

CREATE OR REPLACE FUNCTION app.shares_workspace(other_user uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT EXISTS (
    SELECT 1
    FROM app.workspace_memberships me
    JOIN app.workspace_memberships them ON them.workspace_id = me.workspace_id
    WHERE me.user_id = app.current_user_id()
      AND me.status = 'active'
      AND them.user_id = other_user
      AND them.status = 'active'
  )
$$;

CREATE OR REPLACE FUNCTION app.lookup_local_credential(p_email text)
RETURNS TABLE(user_id uuid, password_hash text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT c.user_id, c.password_hash
  FROM app.local_credentials c
  WHERE lower(c.email) = lower(p_email)
$$;

CREATE OR REPLACE FUNCTION app.invitation_preview(p_token_hash text)
RETURNS TABLE(
  invitation_id uuid,
  workspace_id uuid,
  workspace_name text,
  project_id uuid,
  project_name text,
  email text,
  state text,
  expires_at timestamptz
)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = app, pg_temp AS $$
  SELECT i.id, i.workspace_id, w.name, i.project_id, p.name, i.email, i.state, i.expires_at
  FROM app.invitations i
  JOIN app.workspaces w ON w.id = i.workspace_id
  LEFT JOIN app.projects p ON p.id = i.project_id
  WHERE i.token_hash = p_token_hash
$$;

CREATE OR REPLACE FUNCTION app.prevent_last_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.access_role = 'owner' AND OLD.status = 'active' THEN
    IF TG_OP = 'DELETE'
       OR NEW.status <> 'active'
       OR NEW.access_role <> 'owner' THEN
      IF NOT EXISTS (
        SELECT 1 FROM app.workspace_memberships m
        WHERE m.workspace_id = OLD.workspace_id
          AND m.access_role = 'owner'
          AND m.status = 'active'
          AND m.user_id <> OLD.user_id
      ) THEN
        RAISE EXCEPTION 'last_owner' USING ERRCODE = '23514';
      END IF;
    END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END
$$;

CREATE TRIGGER workspace_memberships_last_owner
  BEFORE UPDATE OR DELETE ON app.workspace_memberships
  FOR EACH ROW EXECUTE FUNCTION app.prevent_last_owner();

CREATE OR REPLACE FUNCTION app.projects_validate_parent() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  parent_ws uuid;
  cycle boolean;
BEGIN
  IF NEW.parent_id IS NULL THEN
    RETURN NEW;
  END IF;
  SELECT workspace_id INTO parent_ws FROM app.projects WHERE id = NEW.parent_id;
  IF parent_ws IS NULL OR parent_ws <> NEW.workspace_id THEN
    RAISE EXCEPTION 'parent_workspace_mismatch' USING ERRCODE = '23514';
  END IF;
  WITH RECURSIVE chain AS (
    SELECT id, parent_id FROM app.projects WHERE id = NEW.parent_id
    UNION ALL
    SELECT p.id, p.parent_id
    FROM app.projects p
    JOIN chain c ON p.id = c.parent_id
    WHERE c.parent_id IS NOT NULL
  )
  SELECT EXISTS (SELECT 1 FROM chain WHERE id = NEW.id) INTO cycle;
  IF cycle THEN
    RAISE EXCEPTION 'project_cycle' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END
$$;

CREATE TRIGGER projects_parent_check
  BEFORE INSERT OR UPDATE OF parent_id, workspace_id ON app.projects
  FOR EACH ROW EXECUTE FUNCTION app.projects_validate_parent();

CREATE OR REPLACE FUNCTION app.sync_project_workspace() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  ws uuid;
BEGIN
  SELECT workspace_id INTO ws FROM app.projects WHERE id = NEW.project_id;
  IF ws IS NULL THEN
    RAISE EXCEPTION 'missing_project' USING ERRCODE = '23503';
  END IF;
  IF NEW.workspace_id IS DISTINCT FROM ws THEN
    RAISE EXCEPTION 'workspace_mismatch' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END
$$;

CREATE TRIGGER requests_workspace_sync
  BEFORE INSERT OR UPDATE OF project_id, workspace_id ON app.requests
  FOR EACH ROW WHEN (NEW.project_id IS NOT NULL)
  EXECUTE FUNCTION app.sync_project_workspace();

CREATE TRIGGER requirements_workspace_sync
  BEFORE INSERT OR UPDATE OF project_id, workspace_id ON app.requirements
  FOR EACH ROW EXECUTE FUNCTION app.sync_project_workspace();

CREATE TRIGGER tasks_workspace_sync
  BEFORE INSERT OR UPDATE OF project_id, workspace_id ON app.tasks
  FOR EACH ROW EXECUTE FUNCTION app.sync_project_workspace();

CREATE TRIGGER handoffs_workspace_sync
  BEFORE INSERT OR UPDATE OF project_id, workspace_id ON app.handoffs
  FOR EACH ROW EXECUTE FUNCTION app.sync_project_workspace();

CREATE TRIGGER documents_workspace_sync
  BEFORE INSERT OR UPDATE OF project_id, workspace_id ON app.documents
  FOR EACH ROW EXECUTE FUNCTION app.sync_project_workspace();

CREATE TRIGGER comments_workspace_sync
  BEFORE INSERT OR UPDATE OF project_id, workspace_id ON app.comments
  FOR EACH ROW EXECUTE FUNCTION app.sync_project_workspace();

CREATE TRIGGER analyses_workspace_sync
  BEFORE INSERT OR UPDATE OF project_id, workspace_id ON app.analyses
  FOR EACH ROW EXECUTE FUNCTION app.sync_project_workspace();

CREATE TRIGGER dashboards_workspace_sync
  BEFORE INSERT OR UPDATE OF project_id, workspace_id ON app.dashboards
  FOR EACH ROW EXECUTE FUNCTION app.sync_project_workspace();

CREATE OR REPLACE FUNCTION app.mark_analyses_stale() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = app, pg_temp AS $$
BEGIN
  UPDATE app.analysis_revisions
  SET stale = true
  WHERE dataset_version_id IN (
    SELECT id FROM app.dataset_versions WHERE dataset_id = NEW.dataset_id AND id <> NEW.id
  );
  RETURN NEW;
END
$$;

CREATE TRIGGER dataset_versions_mark_stale
  AFTER INSERT ON app.dataset_versions
  FOR EACH ROW EXECUTE FUNCTION app.mark_analyses_stale();

-- Row level security

ALTER TABLE app.profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.profiles FORCE ROW LEVEL SECURITY;
ALTER TABLE app.local_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.local_credentials FORCE ROW LEVEL SECURITY;
ALTER TABLE app.workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.workspaces FORCE ROW LEVEL SECURITY;
ALTER TABLE app.workspace_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.workspace_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE app.departments ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.departments FORCE ROW LEVEL SECURITY;
ALTER TABLE app.functional_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.functional_roles FORCE ROW LEVEL SECURITY;
ALTER TABLE app.member_functional_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.member_functional_roles FORCE ROW LEVEL SECURITY;
ALTER TABLE app.partner_organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.partner_organizations FORCE ROW LEVEL SECURITY;
ALTER TABLE app.partner_contacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.partner_contacts FORCE ROW LEVEL SECURITY;
ALTER TABLE app.invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.invitations FORCE ROW LEVEL SECURITY;
ALTER TABLE app.projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.projects FORCE ROW LEVEL SECURITY;
ALTER TABLE app.project_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.project_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE app.project_internal ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.project_internal FORCE ROW LEVEL SECURITY;
ALTER TABLE app.requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.requests FORCE ROW LEVEL SECURITY;
ALTER TABLE app.request_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.request_revisions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.requirements ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.requirements FORCE ROW LEVEL SECURITY;
ALTER TABLE app.requirement_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.requirement_revisions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.tasks FORCE ROW LEVEL SECURITY;
ALTER TABLE app.task_dependencies ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.task_dependencies FORCE ROW LEVEL SECURITY;
ALTER TABLE app.handoffs ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.handoffs FORCE ROW LEVEL SECURITY;
ALTER TABLE app.handoff_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.handoff_revisions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.handoff_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.handoff_events FORCE ROW LEVEL SECURITY;
ALTER TABLE app.approval_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.approval_requests FORCE ROW LEVEL SECURITY;
ALTER TABLE app.approval_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.approval_decisions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.documents FORCE ROW LEVEL SECURITY;
ALTER TABLE app.document_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.document_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.comments FORCE ROW LEVEL SECURITY;
ALTER TABLE app.notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.notifications FORCE ROW LEVEL SECURITY;
ALTER TABLE app.notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.notification_preferences FORCE ROW LEVEL SECURITY;
ALTER TABLE app.audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.audit_events FORCE ROW LEVEL SECURITY;
ALTER TABLE app.outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.outbox_events FORCE ROW LEVEL SECURITY;
ALTER TABLE app.idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.idempotency_keys FORCE ROW LEVEL SECURITY;
ALTER TABLE app.datasets ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.datasets FORCE ROW LEVEL SECURITY;
ALTER TABLE app.dataset_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.dataset_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.dataset_rows ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.dataset_rows FORCE ROW LEVEL SECURITY;
ALTER TABLE app.metric_definitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.metric_definitions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.metric_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.metric_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.data_questions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.data_questions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.question_responses ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.question_responses FORCE ROW LEVEL SECURITY;
ALTER TABLE app.analyses ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.analyses FORCE ROW LEVEL SECURITY;
ALTER TABLE app.analysis_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.analysis_revisions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.dashboards ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.dashboards FORCE ROW LEVEL SECURITY;
ALTER TABLE app.dashboard_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.dashboard_revisions FORCE ROW LEVEL SECURITY;

CREATE POLICY profiles_select ON app.profiles FOR SELECT
  USING (id = app.current_user_id() OR app.shares_workspace(id));
CREATE POLICY profiles_insert ON app.profiles FOR INSERT
  WITH CHECK (id = app.current_user_id());
CREATE POLICY profiles_update ON app.profiles FOR UPDATE
  USING (id = app.current_user_id())
  WITH CHECK (id = app.current_user_id());

CREATE POLICY workspaces_select ON app.workspaces FOR SELECT
  USING (app.is_workspace_member(id));
CREATE POLICY workspaces_insert ON app.workspaces FOR INSERT
  WITH CHECK (status = 'active');
CREATE POLICY workspaces_update ON app.workspaces FOR UPDATE
  USING (app.workspace_role(id) IN ('owner', 'administrator'))
  WITH CHECK (app.workspace_role(id) IN ('owner', 'administrator'));

CREATE POLICY workspace_memberships_select ON app.workspace_memberships FOR SELECT
  USING (app.is_workspace_member(workspace_id) OR user_id = app.current_user_id());
CREATE POLICY workspace_memberships_insert ON app.workspace_memberships FOR INSERT
  WITH CHECK (
    (
      user_id = app.current_user_id()
      AND access_role = 'owner'
      AND status = 'active'
      AND NOT app.workspace_has_active_owner(workspace_id)
    )
    OR app.workspace_role(workspace_id) = 'owner'
    OR (
      app.workspace_role(workspace_id) = 'administrator'
      AND access_role IN ('member', 'guest', 'administrator')
    )
    OR (
      user_id = app.current_user_id()
      AND status = 'active'
      AND EXISTS (
        SELECT 1 FROM app.invitations i
        WHERE i.workspace_id = workspace_memberships.workspace_id
          AND lower(i.email) = lower(coalesce(app.current_user_email(), ''))
          AND i.access_role = workspace_memberships.access_role
          AND i.state IN ('pending', 'accepted')
          AND i.revoked_at IS NULL
          AND (i.consumed_by IS NULL OR i.consumed_by = app.current_user_id())
      )
    )
  );
CREATE POLICY workspace_memberships_update ON app.workspace_memberships FOR UPDATE
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator'))
  WITH CHECK (
    app.workspace_role(workspace_id) = 'owner'
    OR (
      app.workspace_role(workspace_id) = 'administrator'
      AND access_role IN ('member', 'guest', 'administrator')
    )
  );

CREATE POLICY departments_select ON app.departments FOR SELECT
  USING (app.is_workspace_member(workspace_id));
CREATE POLICY departments_write ON app.departments FOR ALL
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator'));

CREATE POLICY functional_roles_select ON app.functional_roles FOR SELECT
  USING (app.is_workspace_member(workspace_id));
CREATE POLICY functional_roles_write ON app.functional_roles FOR ALL
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator'));

CREATE POLICY member_functions_select ON app.member_functional_roles FOR SELECT
  USING (app.is_workspace_member(workspace_id));
CREATE POLICY member_functions_write ON app.member_functional_roles FOR ALL
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator'));

CREATE POLICY partners_select ON app.partner_organizations FOR SELECT
  USING (
    app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
    OR EXISTS (
      SELECT 1 FROM app.partner_contacts c
      WHERE c.partner_id = partner_organizations.id
        AND c.user_id = app.current_user_id()
    )
  );
CREATE POLICY partners_write ON app.partner_organizations FOR ALL
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));

CREATE POLICY partner_contacts_select ON app.partner_contacts FOR SELECT
  USING (
    user_id = app.current_user_id()
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );
CREATE POLICY partner_contacts_write ON app.partner_contacts FOR ALL
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));

CREATE POLICY invitations_select ON app.invitations FOR SELECT
  USING (
    lower(email) = lower(coalesce(app.current_user_email(), ''))
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator')
  );
CREATE POLICY invitations_insert ON app.invitations FOR INSERT
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator'));
CREATE POLICY invitations_update ON app.invitations FOR UPDATE
  USING (
    lower(email) = lower(coalesce(app.current_user_email(), ''))
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator')
  )
  WITH CHECK (
    lower(email) = lower(coalesce(app.current_user_email(), ''))
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator')
  );

CREATE POLICY projects_select ON app.projects FOR SELECT
  USING (app.can_read_project(id));
CREATE POLICY projects_insert ON app.projects FOR INSERT
  WITH CHECK (
    app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
    AND owner_id = app.current_user_id()
  );
CREATE POLICY projects_update ON app.projects FOR UPDATE
  USING (app.project_role(id) IN ('owner', 'contributor'))
  WITH CHECK (app.project_role(id) IN ('owner', 'contributor'));

CREATE POLICY project_memberships_select ON app.project_memberships FOR SELECT
  USING (app.can_read_project(project_id) OR user_id = app.current_user_id());
CREATE POLICY project_memberships_insert ON app.project_memberships FOR INSERT
  WITH CHECK (
    (
      user_id = app.current_user_id()
      AND access_role = 'owner'
      AND status = 'active'
      AND NOT app.project_has_active_owner(project_id)
      AND app.is_workspace_member(app.project_workspace_id(project_id))
    )
    OR app.project_role(project_id) = 'owner'
    OR (
      user_id = app.current_user_id()
      AND status = 'active'
      AND EXISTS (
        SELECT 1 FROM app.invitations i
        WHERE i.project_id = project_memberships.project_id
          AND i.project_role = project_memberships.access_role
          AND lower(i.email) = lower(coalesce(app.current_user_email(), ''))
          AND i.state IN ('pending', 'accepted')
          AND i.revoked_at IS NULL
          AND (i.consumed_by IS NULL OR i.consumed_by = app.current_user_id())
      )
    )
  );
CREATE POLICY project_memberships_update ON app.project_memberships FOR UPDATE
  USING (app.project_role(project_id) = 'owner')
  WITH CHECK (app.project_role(project_id) = 'owner');

CREATE POLICY project_internal_select ON app.project_internal FOR SELECT
  USING (app.is_internal_project_member(project_id));
CREATE POLICY project_internal_write ON app.project_internal FOR ALL
  USING (app.project_role(project_id) IN ('owner', 'contributor'))
  WITH CHECK (app.project_role(project_id) IN ('owner', 'contributor'));

CREATE POLICY requests_select ON app.requests FOR SELECT
  USING (
    requester_id = app.current_user_id()
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );
CREATE POLICY requests_insert ON app.requests FOR INSERT
  WITH CHECK (
    requester_id = app.current_user_id()
    AND app.is_workspace_member(workspace_id)
  );
CREATE POLICY requests_update ON app.requests FOR UPDATE
  USING (
    requester_id = app.current_user_id()
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  )
  WITH CHECK (
    requester_id = app.current_user_id()
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );

CREATE POLICY request_revisions_all ON app.request_revisions FOR ALL
  USING (
    EXISTS (
      SELECT 1 FROM app.requests r
      WHERE r.id = request_revisions.request_id
    )
  )
  WITH CHECK (
    actor_user_id = app.current_user_id()
    AND EXISTS (
      SELECT 1 FROM app.requests r
      WHERE r.id = request_revisions.request_id
    )
  );

CREATE POLICY requirements_read ON app.requirements FOR SELECT
  USING (app.can_read_project(project_id) AND app.is_internal_project_member(project_id));
CREATE POLICY requirements_write ON app.requirements FOR ALL
  USING (app.can_write_project(project_id))
  WITH CHECK (app.can_write_project(project_id));

CREATE POLICY requirement_revisions_all ON app.requirement_revisions FOR ALL
  USING (app.is_internal_project_member(project_id))
  WITH CHECK (app.can_write_project(project_id) AND actor_user_id = app.current_user_id());

CREATE POLICY tasks_read ON app.tasks FOR SELECT
  USING (
    app.is_internal_project_member(project_id)
    OR assignee_id = app.current_user_id()
  );
CREATE POLICY tasks_write ON app.tasks FOR ALL
  USING (app.can_write_project(project_id) OR assignee_id = app.current_user_id())
  WITH CHECK (app.can_write_project(project_id) OR assignee_id = app.current_user_id());

CREATE POLICY task_dependencies_all ON app.task_dependencies FOR ALL
  USING (app.can_read_project(project_id))
  WITH CHECK (app.can_write_project(project_id));

CREATE POLICY handoffs_all ON app.handoffs FOR ALL
  USING (
    app.is_internal_project_member(project_id)
    OR recipient_id = app.current_user_id()
    OR accountable_id = app.current_user_id()
  )
  WITH CHECK (app.is_internal_project_member(project_id));

CREATE POLICY handoff_revisions_select ON app.handoff_revisions FOR SELECT
  USING (
    EXISTS (
      SELECT 1 FROM app.handoffs h
      WHERE h.id = handoff_revisions.handoff_id
    )
  );
CREATE POLICY handoff_revisions_insert ON app.handoff_revisions FOR INSERT
  WITH CHECK (
    actor_user_id = app.current_user_id()
    AND EXISTS (
      SELECT 1 FROM app.handoffs h
      WHERE h.id = handoff_revisions.handoff_id
        AND app.is_internal_project_member(h.project_id)
    )
  );

CREATE POLICY handoff_events_select ON app.handoff_events FOR SELECT
  USING (
    app.is_internal_project_member(project_id)
    OR EXISTS (
      SELECT 1 FROM app.handoffs h
      WHERE h.id = handoff_events.handoff_id
        AND (h.recipient_id = app.current_user_id() OR h.accountable_id = app.current_user_id())
    )
  );
CREATE POLICY handoff_events_insert ON app.handoff_events FOR INSERT
  WITH CHECK (
    actor_user_id = app.current_user_id()
    AND (
      app.is_internal_project_member(project_id)
      OR EXISTS (
        SELECT 1 FROM app.handoffs h
        WHERE h.id = handoff_events.handoff_id
          AND (h.recipient_id = app.current_user_id() OR h.accountable_id = app.current_user_id())
      )
    )
  );

CREATE POLICY approvals_select ON app.approval_requests FOR SELECT
  USING (
    author_id = app.current_user_id()
    OR reviewer_id = app.current_user_id()
    OR (project_id IS NOT NULL AND app.is_internal_project_member(project_id))
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );
CREATE POLICY approvals_insert ON app.approval_requests FOR INSERT
  WITH CHECK (
    author_id = app.current_user_id()
    AND app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );
CREATE POLICY approvals_update ON app.approval_requests FOR UPDATE
  USING (
    reviewer_id = app.current_user_id()
    OR author_id = app.current_user_id()
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator')
  )
  WITH CHECK (
    (
      reviewer_id = app.current_user_id()
      AND reviewer_id <> author_id
    )
    OR (
      author_id = app.current_user_id()
      AND state IN ('pending', 'cancelled')
    )
    OR app.workspace_role(workspace_id) IN ('owner', 'administrator')
  );

CREATE POLICY approval_decisions_select ON app.approval_decisions FOR SELECT
  USING (
    EXISTS (
      SELECT 1 FROM app.approval_requests a
      WHERE a.id = approval_decisions.approval_id
    )
  );
CREATE POLICY approval_decisions_insert ON app.approval_decisions FOR INSERT
  WITH CHECK (actor_user_id = app.current_user_id());

CREATE POLICY documents_select ON app.documents FOR SELECT
  USING (
    app.can_read_project(project_id)
    AND (visibility = 'shared' OR app.is_internal_project_member(project_id))
  );
CREATE POLICY documents_insert ON app.documents FOR INSERT
  WITH CHECK (
    created_by = app.current_user_id()
    AND app.can_read_project(project_id)
    AND (app.is_internal_project_member(project_id) OR visibility = 'shared')
  );
CREATE POLICY documents_update ON app.documents FOR UPDATE
  USING (app.can_write_project(project_id) OR created_by = app.current_user_id())
  WITH CHECK (
    (app.is_internal_project_member(project_id) OR visibility = 'shared')
    AND app.can_read_project(project_id)
  );

CREATE POLICY document_versions_select ON app.document_versions FOR SELECT
  USING (
    EXISTS (
      SELECT 1 FROM app.documents d
      WHERE d.id = document_versions.document_id
    )
  );
CREATE POLICY document_versions_insert ON app.document_versions FOR INSERT
  WITH CHECK (
    actor_user_id = app.current_user_id()
    AND EXISTS (
      SELECT 1 FROM app.documents d
      WHERE d.id = document_versions.document_id
    )
  );
CREATE POLICY document_versions_update ON app.document_versions FOR UPDATE
  USING (
    EXISTS (
      SELECT 1 FROM app.documents d
      WHERE d.id = document_versions.document_id
    )
  )
  WITH CHECK (actor_user_id = app.current_user_id());

CREATE POLICY comments_select ON app.comments FOR SELECT
  USING (
    deleted_at IS NULL
    AND app.can_read_project(project_id)
    AND (visibility = 'shared' OR app.is_internal_project_member(project_id))
  );
CREATE POLICY comments_insert ON app.comments FOR INSERT
  WITH CHECK (
    author_id = app.current_user_id()
    AND app.can_read_project(project_id)
    AND (app.is_internal_project_member(project_id) OR visibility = 'shared')
  );
CREATE POLICY comments_update ON app.comments FOR UPDATE
  USING (author_id = app.current_user_id())
  WITH CHECK (author_id = app.current_user_id());

CREATE POLICY notifications_select ON app.notifications FOR SELECT
  USING (recipient_id = app.current_user_id());
CREATE POLICY notifications_update ON app.notifications FOR UPDATE
  USING (recipient_id = app.current_user_id())
  WITH CHECK (recipient_id = app.current_user_id());
CREATE POLICY notifications_insert ON app.notifications FOR INSERT
  WITH CHECK (app.is_workspace_member(workspace_id));

CREATE POLICY notification_prefs_all ON app.notification_preferences FOR ALL
  USING (user_id = app.current_user_id())
  WITH CHECK (user_id = app.current_user_id());

CREATE POLICY audit_select ON app.audit_events FOR SELECT
  USING (
    app.workspace_role(workspace_id) IN ('owner', 'administrator')
    OR (project_id IS NOT NULL AND app.is_internal_project_member(project_id))
  );
CREATE POLICY audit_insert ON app.audit_events FOR INSERT
  WITH CHECK (
    actor_user_id = app.current_user_id()
    AND app.is_workspace_member(workspace_id)
  );

CREATE POLICY outbox_select ON app.outbox_events FOR SELECT
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));
CREATE POLICY outbox_insert ON app.outbox_events FOR INSERT
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member', 'guest'));
CREATE POLICY outbox_update ON app.outbox_events FOR UPDATE
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));

CREATE POLICY idempotency_all ON app.idempotency_keys FOR ALL
  USING (user_id = app.current_user_id())
  WITH CHECK (user_id = app.current_user_id());

CREATE POLICY datasets_select ON app.datasets FOR SELECT
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));
CREATE POLICY datasets_insert ON app.datasets FOR INSERT
  WITH CHECK (
    owner_id = app.current_user_id()
    AND app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );
CREATE POLICY datasets_update ON app.datasets FOR UPDATE
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));

CREATE POLICY dataset_versions_select ON app.dataset_versions FOR SELECT
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));
CREATE POLICY dataset_versions_insert ON app.dataset_versions FOR INSERT
  WITH CHECK (
    actor_user_id = app.current_user_id()
    AND app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );

CREATE POLICY dataset_rows_select ON app.dataset_rows FOR SELECT
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));
CREATE POLICY dataset_rows_insert ON app.dataset_rows FOR INSERT
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));

CREATE POLICY metrics_select ON app.metric_definitions FOR SELECT
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));
CREATE POLICY metrics_insert ON app.metric_definitions FOR INSERT
  WITH CHECK (
    owner_id = app.current_user_id()
    AND app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );
CREATE POLICY metrics_update ON app.metric_definitions FOR UPDATE
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));

CREATE POLICY metric_versions_select ON app.metric_versions FOR SELECT
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));
CREATE POLICY metric_versions_insert ON app.metric_versions FOR INSERT
  WITH CHECK (
    actor_user_id = app.current_user_id()
    AND app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );
CREATE POLICY metric_versions_update ON app.metric_versions FOR UPDATE
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));

CREATE POLICY questions_select ON app.data_questions FOR SELECT
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));
CREATE POLICY questions_insert ON app.data_questions FOR INSERT
  WITH CHECK (
    requester_id = app.current_user_id()
    AND app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );
CREATE POLICY questions_update ON app.data_questions FOR UPDATE
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'))
  WITH CHECK (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));

CREATE POLICY question_responses_select ON app.question_responses FOR SELECT
  USING (app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member'));
CREATE POLICY question_responses_insert ON app.question_responses FOR INSERT
  WITH CHECK (
    actor_user_id = app.current_user_id()
    AND app.workspace_role(workspace_id) IN ('owner', 'administrator', 'member')
  );

CREATE POLICY analyses_select ON app.analyses FOR SELECT
  USING (app.is_internal_project_member(project_id));
CREATE POLICY analyses_insert ON app.analyses FOR INSERT
  WITH CHECK (app.is_internal_project_member(project_id) AND created_by = app.current_user_id());
CREATE POLICY analyses_update ON app.analyses FOR UPDATE
  USING (app.is_internal_project_member(project_id))
  WITH CHECK (app.is_internal_project_member(project_id));

CREATE POLICY analysis_revisions_select ON app.analysis_revisions FOR SELECT
  USING (app.is_internal_project_member(project_id));
CREATE POLICY analysis_revisions_insert ON app.analysis_revisions FOR INSERT
  WITH CHECK (
    app.is_internal_project_member(project_id)
    AND actor_user_id = app.current_user_id()
  );
CREATE POLICY analysis_revisions_update ON app.analysis_revisions FOR UPDATE
  USING (app.is_internal_project_member(project_id))
  WITH CHECK (app.is_internal_project_member(project_id));

CREATE POLICY dashboards_select ON app.dashboards FOR SELECT
  USING (app.is_internal_project_member(project_id));
CREATE POLICY dashboards_insert ON app.dashboards FOR INSERT
  WITH CHECK (app.is_internal_project_member(project_id) AND created_by = app.current_user_id());
CREATE POLICY dashboards_update ON app.dashboards FOR UPDATE
  USING (app.is_internal_project_member(project_id))
  WITH CHECK (app.is_internal_project_member(project_id));

CREATE POLICY dashboard_revisions_select ON app.dashboard_revisions FOR SELECT
  USING (app.is_internal_project_member(project_id));
CREATE POLICY dashboard_revisions_insert ON app.dashboard_revisions FOR INSERT
  WITH CHECK (
    app.can_write_project(project_id)
    AND actor_user_id = app.current_user_id()
  );
CREATE POLICY dashboard_revisions_update ON app.dashboard_revisions FOR UPDATE
  USING (app.can_write_project(project_id))
  WITH CHECK (app.can_write_project(project_id));

-- Privileges. local_credentials and audit history stay narrower.

GRANT USAGE ON SCHEMA app TO app_runtime;

GRANT SELECT, INSERT, UPDATE ON app.profiles TO app_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON
  app.workspaces,
  app.workspace_memberships,
  app.departments,
  app.functional_roles,
  app.member_functional_roles,
  app.partner_organizations,
  app.partner_contacts,
  app.invitations,
  app.projects,
  app.project_memberships,
  app.project_internal,
  app.requests,
  app.request_revisions,
  app.requirements,
  app.requirement_revisions,
  app.tasks,
  app.task_dependencies,
  app.handoffs,
  app.handoff_revisions,
  app.handoff_events,
  app.approval_requests,
  app.approval_decisions,
  app.documents,
  app.document_versions,
  app.comments,
  app.notifications,
  app.notification_preferences,
  app.idempotency_keys,
  app.datasets,
  app.dataset_versions,
  app.dataset_rows,
  app.metric_definitions,
  app.metric_versions,
  app.data_questions,
  app.question_responses,
  app.analyses,
  app.analysis_revisions,
  app.dashboards,
  app.dashboard_revisions
TO app_runtime;

GRANT SELECT, INSERT ON app.audit_events TO app_runtime;
GRANT SELECT, INSERT, UPDATE ON app.outbox_events TO app_runtime;

CREATE OR REPLACE FUNCTION app.link_partner_contact(p_partner uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = app, pg_temp AS $$
BEGIN
  UPDATE app.partner_contacts
  SET user_id = app.current_user_id()
  WHERE partner_id = p_partner
    AND lower(email) = lower(coalesce(app.current_user_email(), ''))
    AND (user_id IS NULL OR user_id = app.current_user_id())
    AND app.is_workspace_member(workspace_id);
END
$$;

CREATE OR REPLACE FUNCTION app.suspend_partner(p_partner uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = app, pg_temp AS $$
DECLARE
  wid uuid;
BEGIN
  SELECT workspace_id INTO wid FROM app.partner_organizations WHERE id = p_partner;
  IF wid IS NULL THEN
    RAISE EXCEPTION 'not_found' USING ERRCODE = 'P0002';
  END IF;
  IF app.workspace_role(wid) NOT IN ('owner', 'administrator', 'member') THEN
    RAISE EXCEPTION 'forbidden' USING ERRCODE = '42501';
  END IF;
  UPDATE app.partner_organizations
  SET status = 'suspended', updated_at = now()
  WHERE id = p_partner;
  UPDATE app.project_memberships
  SET status = 'removed', updated_at = now()
  WHERE access_role = 'partner'
    AND user_id IN (
      SELECT user_id FROM app.partner_contacts
      WHERE partner_id = p_partner AND user_id IS NOT NULL
    );
END
$$;

GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA app TO app_runtime;
