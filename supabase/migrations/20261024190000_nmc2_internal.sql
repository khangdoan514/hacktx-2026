-- NMC2 internal organization and capability workflow.
-- Does not alter dashboard columns or drop earlier tables.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SCHEMA IF NOT EXISTS nmc3;

CREATE OR REPLACE FUNCTION app.nmc_actor_active(p_org uuid)
RETURNS boolean
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
BEGIN
  RETURN EXISTS (
    SELECT 1
    FROM app.employees AS employee
    JOIN app.org_memberships AS membership
      ON membership.user_id = employee.user_id
     AND membership.organization_id = employee.organization_id
    JOIN app.profiles AS profile
      ON profile.id = employee.user_id
     AND profile.status = 'active'
    WHERE employee.user_id = app.current_user_id()
      AND employee.organization_id = p_org
      AND employee.status = 'active'
      AND membership.status = 'active'
  );
END;
$$;

CREATE TABLE app.organizations (
    id uuid PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.divisions (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    slug text NOT NULL,
    name text NOT NULL,
    UNIQUE (organization_id, slug)
);

CREATE TABLE app.teams (
    id uuid PRIMARY KEY,
    division_id uuid NOT NULL REFERENCES app.divisions (id),
    slug text NOT NULL,
    name text NOT NULL,
    UNIQUE (division_id, slug)
);

CREATE TABLE app.employees (
    user_id uuid PRIMARY KEY REFERENCES app.profiles (id),
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    email text NOT NULL,
    display_name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.org_memberships (
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    user_id uuid NOT NULL REFERENCES app.employees (user_id),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id)
);

CREATE TABLE app.team_memberships (
    team_id uuid NOT NULL REFERENCES app.teams (id),
    user_id uuid NOT NULL REFERENCES app.employees (user_id),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);

CREATE TABLE app.role_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    user_id uuid NOT NULL REFERENCES app.employees (user_id),
    role_code text NOT NULL,
    scope_type text NOT NULL CHECK (scope_type IN ('organization', 'division', 'team')),
    scope_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    UNIQUE (user_id, role_code, scope_type, scope_id)
);

CREATE TABLE app.internal_invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    email text NOT NULL,
    team_id uuid NOT NULL REFERENCES app.teams (id),
    role_code text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'revoked')),
    invited_by uuid NOT NULL REFERENCES app.profiles (id),
    accepted_by uuid REFERENCES app.profiles (id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.capability_requests (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    source_team_id uuid NOT NULL REFERENCES app.teams (id),
    target_team_id uuid NOT NULL REFERENCES app.teams (id),
    requester_id uuid NOT NULL REFERENCES app.employees (user_id),
    title text NOT NULL,
    outcome text NOT NULL DEFAULT '',
    state text NOT NULL,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    cancel_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state IN (
        'draft', 'submitted', 'triage', 'needs_information', 'accepted', 'rejected',
        'in_delivery', 'ready_for_acceptance', 'fulfilled', 'cancelled'
    ))
);

CREATE TABLE app.capability_clarifications (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL REFERENCES app.capability_requests (id),
    author_id uuid NOT NULL REFERENCES app.employees (user_id),
    question text NOT NULL,
    answer text,
    created_at timestamptz NOT NULL DEFAULT now(),
    answered_at timestamptz
);

CREATE TABLE app.specifications (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    request_id uuid NOT NULL UNIQUE REFERENCES app.capability_requests (id),
    state text NOT NULL,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    current_revision integer NOT NULL,
    material_revision integer NOT NULL,
    created_by uuid NOT NULL REFERENCES app.employees (user_id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state IN ('draft', 'in_review', 'changes_requested', 'approved', 'superseded'))
);

CREATE TABLE app.specification_revisions (
    specification_id uuid NOT NULL REFERENCES app.specifications (id),
    revision integer NOT NULL CHECK (revision > 0),
    document jsonb NOT NULL,
    material boolean NOT NULL,
    created_by uuid NOT NULL REFERENCES app.employees (user_id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (specification_id, revision)
);

CREATE TABLE app.scoped_approvals (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    target_kind text NOT NULL,
    target_id uuid NOT NULL,
    target_version integer NOT NULL,
    review_state text NOT NULL,
    author_id uuid NOT NULL REFERENCES app.employees (user_id),
    reviewer_id uuid NOT NULL REFERENCES app.employees (user_id),
    state text NOT NULL,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state IN (
        'pending', 'approved', 'rejected', 'changes_requested', 'expired', 'superseded', 'cancelled'
    ))
);

CREATE TABLE app.scoped_approval_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_id uuid NOT NULL REFERENCES app.scoped_approvals (id),
    actor_user_id uuid NOT NULL REFERENCES app.profiles (id),
    decision text NOT NULL,
    comment text NOT NULL DEFAULT '',
    target_version integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.capability_handovers (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    request_id uuid NOT NULL REFERENCES app.capability_requests (id),
    state text NOT NULL,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    recipient_id uuid REFERENCES app.employees (user_id),
    note text NOT NULL DEFAULT '',
    created_by uuid NOT NULL REFERENCES app.employees (user_id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state IN ('draft', 'sent', 'acknowledged', 'completed', 'returned', 'cancelled'))
);

CREATE TABLE app.delivery_tasks (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    request_id uuid NOT NULL REFERENCES app.capability_requests (id),
    title text NOT NULL,
    state text NOT NULL,
    version integer NOT NULL DEFAULT 1,
    evidence text NOT NULL DEFAULT '',
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state IN ('todo', 'done', 'cancelled'))
);

CREATE TABLE app.delivery_projects (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.delivery_grants (
    project_id uuid NOT NULL REFERENCES app.delivery_projects (id),
    user_id uuid NOT NULL REFERENCES app.employees (user_id),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE app.catalog_releases (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    specification_id uuid NOT NULL REFERENCES app.specifications (id),
    spec_revision integer NOT NULL,
    state text NOT NULL,
    version integer NOT NULL DEFAULT 1,
    published_at timestamptz,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state IN ('draft', 'testing', 'eligible', 'published', 'suspended', 'retired'))
);

CREATE TABLE app.evaluation_records (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    specification_id uuid NOT NULL REFERENCES app.specifications (id),
    catalog_id uuid REFERENCES app.catalog_releases (id),
    spec_revision integer NOT NULL,
    gate text NOT NULL,
    passed boolean NOT NULL,
    evidence text NOT NULL,
    invalidated_at timestamptz,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.resource_records (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    request_id uuid NOT NULL REFERENCES app.capability_requests (id),
    kind text NOT NULL CHECK (kind IN ('hardware', 'budget')),
    requested_amount numeric,
    approved_amount numeric,
    usage_amount numeric,
    unit text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (usage_amount IS NULL)
);

CREATE TABLE app.private_files (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    project_id uuid REFERENCES app.delivery_projects (id),
    request_id uuid REFERENCES app.capability_requests (id),
    owner_id uuid NOT NULL REFERENCES app.employees (user_id),
    filename text NOT NULL,
    storage_key text,
    checksum text,
    byte_size bigint,
    state text NOT NULL CHECK (state IN ('initiated', 'uploaded', 'ready')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.internal_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    actor_id uuid,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id uuid,
    data jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.employee_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id uuid,
    message text NOT NULL,
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.internal_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    topic text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);

CREATE TABLE app.command_idempotency (
    user_id uuid NOT NULL,
    scope text NOT NULL,
    idem_key text NOT NULL,
    body_hash text NOT NULL,
    status integer NOT NULL,
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, scope, idem_key)
);

CREATE TABLE nmc3.runs (
    id text PRIMARY KEY,
    organization_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    project_id uuid,
    state text NOT NULL,
    agent_id text NOT NULL,
    agent_version text NOT NULL,
    execution_mode text NOT NULL,
    adapter_version text NOT NULL,
    snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE nmc3.run_events (
    run_id text NOT NULL REFERENCES nmc3.runs (id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    snapshot jsonb NOT NULL,
    PRIMARY KEY (run_id, sequence)
);

CREATE TABLE nmc3.jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id text NOT NULL REFERENCES nmc3.runs (id),
    state text NOT NULL CHECK (state IN ('pending', 'leased', 'done', 'abandoned')),
    lease_owner text,
    lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO app.organizations (id, slug, name)
VALUES ('10000000-0000-4000-8000-000000000001', 'nmc2', 'NMC2');

INSERT INTO app.divisions (id, organization_id, slug, name) VALUES
    ('10000000-0000-4000-8000-000000000002', '10000000-0000-4000-8000-000000000001', 'hardware', 'Hardware'),
    ('10000000-0000-4000-8000-000000000003', '10000000-0000-4000-8000-000000000001', 'software', 'Software');

INSERT INTO app.teams (id, division_id, slug, name) VALUES
    ('10000000-0000-4000-8000-000000000004', '10000000-0000-4000-8000-000000000002', 'hardware', 'Hardware'),
    ('10000000-0000-4000-8000-000000000005', '10000000-0000-4000-8000-000000000003', 'ai', 'AI'),
    ('10000000-0000-4000-8000-000000000006', '10000000-0000-4000-8000-000000000003', 'review', 'Review');

CREATE OR REPLACE FUNCTION app.nmc_can_read_project(p_project uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1
    FROM app.delivery_projects AS project
    JOIN app.delivery_grants AS access_grant
      ON access_grant.project_id = project.id
     AND access_grant.user_id = app.current_user_id()
     AND access_grant.status = 'active'
    WHERE project.id = p_project
      AND app.nmc_actor_active(project.organization_id)
  );
$$;

CREATE OR REPLACE FUNCTION app.nmc_can_read_request(p_request uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1
    FROM app.capability_requests AS request
    WHERE request.id = p_request
      AND app.nmc_actor_active(request.organization_id)
      AND (
        request.requester_id = app.current_user_id()
        OR EXISTS (
          SELECT 1 FROM app.team_memberships AS membership
          WHERE membership.user_id = app.current_user_id()
            AND membership.status = 'active'
            AND membership.team_id IN (request.source_team_id, request.target_team_id)
        )
        OR EXISTS (
          SELECT 1 FROM app.role_grants AS role_grant
          WHERE role_grant.user_id = app.current_user_id()
            AND role_grant.organization_id = request.organization_id
            AND role_grant.status = 'active'
        )
      )
  );
$$;

CREATE OR REPLACE FUNCTION app.create_scoped_approval(
    p_org uuid,
    p_scope_id uuid,
    p_target_kind text,
    p_target_id uuid,
    p_target_version integer,
    p_reviewer uuid
) RETURNS jsonb
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
DECLARE
    spec app.specifications%ROWTYPE;
    created_row app.scoped_approvals%ROWTYPE;
BEGIN
    IF NOT app.nmc_actor_active(p_org) THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    IF p_reviewer = app.current_user_id() THEN
        RAISE EXCEPTION 'nmc_self_approval';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM app.role_grants
        WHERE user_id = app.current_user_id()
          AND organization_id = p_org
          AND role_code = 'ai_engineer'
          AND status = 'active'
    ) THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM app.employees
        WHERE user_id = p_reviewer AND organization_id = p_org AND status = 'active'
    ) OR NOT EXISTS (
        SELECT 1 FROM app.role_grants
        WHERE user_id = p_reviewer
          AND organization_id = p_org
          AND role_code = 'release_approver'
          AND status = 'active'
    ) THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    IF p_target_kind <> 'specification' THEN
        RAISE EXCEPTION 'nmc_invalid_target';
    END IF;
    SELECT * INTO spec FROM app.specifications WHERE id = p_target_id FOR UPDATE;
    IF spec.id IS NULL THEN
        RAISE EXCEPTION 'nmc_not_found';
    END IF;
    IF spec.organization_id IS DISTINCT FROM p_org
       OR spec.request_id IS DISTINCT FROM p_scope_id
       OR spec.material_revision IS DISTINCT FROM p_target_version
       OR spec.state IS DISTINCT FROM 'in_review' THEN
        RAISE EXCEPTION 'nmc_cross_scope';
    END IF;
    INSERT INTO app.scoped_approvals (
        id, organization_id, scope_type, scope_id, target_kind, target_id,
        target_version, review_state, author_id, reviewer_id, state
    ) VALUES (
        gen_random_uuid(), p_org, 'request', p_scope_id, p_target_kind, p_target_id,
        p_target_version, spec.state, app.current_user_id(), p_reviewer, 'pending'
    ) RETURNING * INTO created_row;
    RETURN jsonb_build_object(
        'id', created_row.id,
        'organization_id', created_row.organization_id,
        'scope_id', created_row.scope_id,
        'target_kind', created_row.target_kind,
        'target_id', created_row.target_id,
        'target_version', created_row.target_version,
        'review_state', created_row.review_state,
        'author_id', created_row.author_id,
        'reviewer_id', created_row.reviewer_id,
        'state', created_row.state,
        'version', created_row.version
    );
END;
$$;

CREATE OR REPLACE FUNCTION app.decide_scoped_approval(
    p_approval uuid,
    p_decision text,
    p_expected_version integer,
    p_comment text
) RETURNS jsonb
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
DECLARE
    rec app.scoped_approvals%ROWTYPE;
    spec app.specifications%ROWTYPE;
    updated_count integer;
    spec_state text;
BEGIN
    IF p_decision NOT IN ('approved', 'rejected', 'changes_requested') THEN
        RAISE EXCEPTION 'nmc_invalid_state';
    END IF;
    SELECT * INTO rec FROM app.scoped_approvals WHERE id = p_approval FOR UPDATE;
    IF rec.id IS NULL THEN
        RAISE EXCEPTION 'nmc_not_found';
    END IF;
    IF NOT app.nmc_actor_active(rec.organization_id) THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    IF rec.author_id = app.current_user_id() THEN
        RAISE EXCEPTION 'nmc_self_approval';
    END IF;
    IF rec.reviewer_id IS DISTINCT FROM app.current_user_id() THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    IF rec.state <> 'pending' OR rec.version <> p_expected_version THEN
        RAISE EXCEPTION 'nmc_conflict';
    END IF;
    SELECT * INTO spec FROM app.specifications WHERE id = rec.target_id FOR UPDATE;
    IF spec.id IS NULL
       OR spec.organization_id IS DISTINCT FROM rec.organization_id
       OR spec.request_id IS DISTINCT FROM rec.scope_id
       OR rec.target_kind IS DISTINCT FROM 'specification'
       OR spec.material_revision IS DISTINCT FROM rec.target_version
       OR spec.state IS DISTINCT FROM rec.review_state
       OR spec.state IS DISTINCT FROM 'in_review' THEN
        RAISE EXCEPTION 'nmc_cross_scope';
    END IF;
    spec_state := CASE p_decision
        WHEN 'approved' THEN 'approved'
        ELSE 'changes_requested'
    END;
    UPDATE app.specifications
    SET state = spec_state, version = version + 1, updated_at = now()
    WHERE id = spec.id
      AND organization_id = rec.organization_id
      AND request_id = rec.scope_id
      AND material_revision = rec.target_version
      AND state = 'in_review';
    GET DIAGNOSTICS updated_count = ROW_COUNT;
    IF updated_count <> 1 THEN
        RAISE EXCEPTION 'nmc_conflict';
    END IF;
    UPDATE app.scoped_approvals
    SET state = p_decision, version = version + 1, updated_at = now()
    WHERE id = rec.id AND state = 'pending' AND version = p_expected_version;
    GET DIAGNOSTICS updated_count = ROW_COUNT;
    IF updated_count <> 1 THEN
        RAISE EXCEPTION 'nmc_conflict';
    END IF;
    INSERT INTO app.scoped_approval_decisions (approval_id, actor_user_id, decision, comment, target_version)
    VALUES (rec.id, app.current_user_id(), p_decision, COALESCE(p_comment, ''), rec.target_version);
    RETURN jsonb_build_object(
        'id', rec.id,
        'state', p_decision,
        'version', rec.version + 1,
        'target_version', rec.target_version,
        'scope_id', rec.scope_id
    );
END;
$$;

CREATE OR REPLACE FUNCTION app.provision_profile(p_name text, p_email text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
BEGIN
    INSERT INTO app.profiles (id, display_name, status, timezone)
    VALUES (app.current_user_id(), COALESCE(NULLIF(p_name, ''), 'Employee'), 'active', 'UTC')
    ON CONFLICT (id) DO NOTHING;
END;
$$;

CREATE OR REPLACE FUNCTION nmc3.enqueue_job(p_run text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = nmc3, app, pg_temp
AS $$
DECLARE
    owner_id uuid;
    org_id uuid;
BEGIN
    SELECT actor_id, organization_id INTO owner_id, org_id FROM nmc3.runs WHERE id = p_run;
    IF owner_id IS NULL OR owner_id IS DISTINCT FROM app.current_user_id() OR NOT app.nmc_actor_active(org_id) THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    INSERT INTO nmc3.jobs (run_id, state) VALUES (p_run, 'pending');
END;
$$;

CREATE OR REPLACE FUNCTION nmc3.lease_next_job(p_worker text)
RETURNS TABLE(run_id text, actor_id uuid, organization_id uuid)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = nmc3, pg_temp
AS $$
DECLARE
    leased uuid;
BEGIN
    SELECT id INTO leased
    FROM nmc3.jobs
    WHERE state = 'pending'
    ORDER BY created_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT 1;
    IF leased IS NULL THEN
        RETURN;
    END IF;
    UPDATE nmc3.jobs
    SET state = 'leased', lease_owner = p_worker, lease_until = now() + interval '2 minutes'
    WHERE id = leased;
    RETURN QUERY
    SELECT job.run_id, run.actor_id, run.organization_id
    FROM nmc3.jobs AS job
    JOIN nmc3.runs AS run ON run.id = job.run_id
    WHERE job.id = leased;
END;
$$;

CREATE OR REPLACE FUNCTION nmc3.finish_job(p_run text, p_state text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = nmc3, pg_temp
AS $$
BEGIN
    UPDATE nmc3.jobs
    SET state = CASE WHEN p_state = 'abandoned' THEN 'abandoned' ELSE 'done' END,
        lease_until = NULL
    WHERE run_id = p_run AND state = 'leased';
END;
$$;

CREATE OR REPLACE FUNCTION nmc3.abandon_leased_jobs()
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = nmc3, pg_temp
AS $$
DECLARE
    affected integer;
BEGIN
    UPDATE nmc3.runs AS run
    SET state = 'FAILED',
        updated_at = now(),
        snapshot = jsonb_set(
            jsonb_set(run.snapshot, '{run,state}', '"FAILED"'::jsonb, true),
            '{run,error}',
            jsonb_build_object('code', 'INTERNAL', 'message', 'Abandoned in-flight work was marked failed'),
            true
        )
    WHERE run.state = 'RUNNING'
      AND EXISTS (
          SELECT 1 FROM nmc3.jobs AS job
          WHERE job.run_id = run.id AND job.state = 'leased'
      );
    GET DIAGNOSTICS affected = ROW_COUNT;
    UPDATE nmc3.jobs
    SET state = 'abandoned', lease_until = NULL
    WHERE state = 'leased';
    RETURN affected;
END;
$$;

REVOKE ALL ON FUNCTION app.nmc_actor_active(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION app.nmc_can_read_project(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION app.nmc_can_read_request(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION app.create_scoped_approval(uuid, uuid, text, uuid, integer, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION app.decide_scoped_approval(uuid, text, integer, text) FROM PUBLIC;
CREATE OR REPLACE FUNCTION app.note_material_revision(p_spec uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
DECLARE
    org_id uuid;
BEGIN
    SELECT organization_id INTO org_id FROM app.specifications WHERE id = p_spec;
    IF org_id IS NULL THEN
        RAISE EXCEPTION 'nmc_not_found';
    END IF;
    IF NOT app.nmc_actor_active(org_id) THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    UPDATE app.scoped_approvals
    SET state = 'superseded', version = version + 1, updated_at = now()
    WHERE target_id = p_spec
      AND target_kind = 'specification'
      AND state IN ('pending', 'approved');
    UPDATE app.evaluation_records
    SET invalidated_at = now()
    WHERE specification_id = p_spec AND invalidated_at IS NULL;
    UPDATE app.catalog_releases
    SET state = 'testing', version = version + 1, updated_at = now()
    WHERE specification_id = p_spec AND state IN ('draft', 'testing', 'eligible');
END;
$$;

CREATE OR REPLACE FUNCTION app.create_internal_invitation(
    p_email text, p_team uuid, p_role text, p_token_hash text
) RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
DECLARE
    org_id uuid := '10000000-0000-4000-8000-000000000001';
    created uuid;
BEGIN
    IF NOT app.nmc_actor_active(org_id) OR NOT EXISTS (
        SELECT 1 FROM app.role_grants
        WHERE user_id = app.current_user_id() AND organization_id = org_id
          AND role_code = 'administrator' AND status = 'active'
    ) THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    IF p_role NOT IN (
        'requester','team_lead','ai_intake','ai_engineer','hardware_owner','security_reviewer',
        'budget_owner','release_approver','operator','administrator','auditor'
    ) THEN
        RAISE EXCEPTION 'nmc_invalid_state';
    END IF;
    INSERT INTO app.internal_invitations (organization_id, email, team_id, role_code, token_hash, invited_by)
    VALUES (org_id, lower(p_email), p_team, p_role, p_token_hash, app.current_user_id())
    RETURNING id INTO created;
    RETURN created;
END;
$$;

CREATE OR REPLACE FUNCTION app.accept_internal_invitation(p_token_hash text, p_display text)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
DECLARE
    invite app.internal_invitations%ROWTYPE;
BEGIN
    SELECT * INTO invite FROM app.internal_invitations
    WHERE token_hash = p_token_hash AND status = 'pending';
    IF invite.id IS NULL OR lower(invite.email) IS DISTINCT FROM lower(app.current_user_email()) THEN
        RAISE EXCEPTION 'nmc_not_found';
    END IF;
    INSERT INTO app.employees (user_id, organization_id, email, display_name)
    VALUES (app.current_user_id(), invite.organization_id, lower(invite.email), COALESCE(NULLIF(p_display, ''), invite.email))
    ON CONFLICT (user_id) DO UPDATE SET status = 'active', email = EXCLUDED.email;
    INSERT INTO app.org_memberships (organization_id, user_id)
    VALUES (invite.organization_id, app.current_user_id())
    ON CONFLICT (organization_id, user_id) DO UPDATE SET status = 'active', updated_at = now();
    INSERT INTO app.team_memberships (team_id, user_id)
    VALUES (invite.team_id, app.current_user_id())
    ON CONFLICT (team_id, user_id) DO UPDATE SET status = 'active';
    INSERT INTO app.role_grants (organization_id, user_id, role_code, scope_type, scope_id)
    VALUES (invite.organization_id, app.current_user_id(), invite.role_code, 'team', invite.team_id)
    ON CONFLICT (user_id, role_code, scope_type, scope_id) DO UPDATE SET status = 'active';
    UPDATE app.internal_invitations
    SET status = 'accepted', accepted_by = app.current_user_id()
    WHERE id = invite.id AND status = 'pending';
    RETURN invite.organization_id;
END;
$$;

CREATE OR REPLACE FUNCTION app.remove_org_membership(p_user uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
DECLARE
    org_id uuid := '10000000-0000-4000-8000-000000000001';
BEGIN
    IF NOT app.nmc_actor_active(org_id) OR NOT EXISTS (
        SELECT 1 FROM app.role_grants
        WHERE user_id = app.current_user_id() AND organization_id = org_id
          AND role_code = 'administrator' AND status = 'active'
    ) OR p_user = app.current_user_id() THEN
        RAISE EXCEPTION 'nmc_forbidden';
    END IF;
    UPDATE app.org_memberships
    SET status = 'removed', updated_at = now()
    WHERE organization_id = org_id AND user_id = p_user AND status = 'active';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'nmc_not_found';
    END IF;
END;
$$;

REVOKE ALL ON FUNCTION app.note_material_revision(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION app.create_internal_invitation(text, uuid, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION app.accept_internal_invitation(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION app.remove_org_membership(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION app.provision_profile(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION nmc3.enqueue_job(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION nmc3.lease_next_job(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION nmc3.finish_job(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION nmc3.abandon_leased_jobs() FROM PUBLIC;

GRANT EXECUTE ON FUNCTION app.nmc_actor_active(uuid) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.nmc_can_read_project(uuid) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.nmc_can_read_request(uuid) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.create_scoped_approval(uuid, uuid, text, uuid, integer, uuid) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.decide_scoped_approval(uuid, text, integer, text) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.note_material_revision(uuid) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.create_internal_invitation(text, uuid, text, text) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.accept_internal_invitation(text, text) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.remove_org_membership(uuid) TO app_runtime;
GRANT EXECUTE ON FUNCTION app.provision_profile(text, text) TO app_runtime;
GRANT EXECUTE ON FUNCTION nmc3.enqueue_job(text) TO app_runtime;

GRANT USAGE ON SCHEMA nmc3 TO app_runtime;
GRANT SELECT, INSERT, UPDATE ON
    app.organizations,
    app.divisions,
    app.teams,
    app.employees,
    app.org_memberships,
    app.team_memberships,
    app.role_grants,
    app.internal_invitations,
    app.capability_requests,
    app.capability_clarifications,
    app.specifications,
    app.specification_revisions,
    app.scoped_approvals,
    app.scoped_approval_decisions,
    app.capability_handovers,
    app.delivery_tasks,
    app.delivery_projects,
    app.delivery_grants,
    app.catalog_releases,
    app.evaluation_records,
    app.resource_records,
    app.private_files,
    app.internal_audit,
    app.employee_notifications,
    app.internal_outbox,
    app.command_idempotency
TO app_runtime;
GRANT SELECT, INSERT, UPDATE ON nmc3.runs, nmc3.run_events, nmc3.jobs TO app_runtime;

ALTER TABLE app.organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.organizations FORCE ROW LEVEL SECURITY;
ALTER TABLE app.divisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.divisions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.teams ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.teams FORCE ROW LEVEL SECURITY;
ALTER TABLE app.employees ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.employees FORCE ROW LEVEL SECURITY;
ALTER TABLE app.org_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.org_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE app.team_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.team_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE app.role_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.role_grants FORCE ROW LEVEL SECURITY;
ALTER TABLE app.internal_invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.internal_invitations FORCE ROW LEVEL SECURITY;
ALTER TABLE app.capability_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.capability_requests FORCE ROW LEVEL SECURITY;
ALTER TABLE app.capability_clarifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.capability_clarifications FORCE ROW LEVEL SECURITY;
ALTER TABLE app.specifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.specifications FORCE ROW LEVEL SECURITY;
ALTER TABLE app.specification_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.specification_revisions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.scoped_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.scoped_approvals FORCE ROW LEVEL SECURITY;
ALTER TABLE app.scoped_approval_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.scoped_approval_decisions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.capability_handovers ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.capability_handovers FORCE ROW LEVEL SECURITY;
ALTER TABLE app.delivery_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.delivery_tasks FORCE ROW LEVEL SECURITY;
ALTER TABLE app.delivery_projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.delivery_projects FORCE ROW LEVEL SECURITY;
ALTER TABLE app.delivery_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.delivery_grants FORCE ROW LEVEL SECURITY;
ALTER TABLE app.catalog_releases ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.catalog_releases FORCE ROW LEVEL SECURITY;
ALTER TABLE app.evaluation_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.evaluation_records FORCE ROW LEVEL SECURITY;
ALTER TABLE app.resource_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.resource_records FORCE ROW LEVEL SECURITY;
ALTER TABLE app.private_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.private_files FORCE ROW LEVEL SECURITY;
ALTER TABLE app.internal_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.internal_audit FORCE ROW LEVEL SECURITY;
ALTER TABLE app.employee_notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.employee_notifications FORCE ROW LEVEL SECURITY;
ALTER TABLE app.internal_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.internal_outbox FORCE ROW LEVEL SECURITY;
ALTER TABLE app.command_idempotency ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.command_idempotency FORCE ROW LEVEL SECURITY;
ALTER TABLE nmc3.runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE nmc3.runs FORCE ROW LEVEL SECURITY;
ALTER TABLE nmc3.run_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE nmc3.run_events FORCE ROW LEVEL SECURITY;
ALTER TABLE nmc3.jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE nmc3.jobs FORCE ROW LEVEL SECURITY;

CREATE POLICY organizations_select ON app.organizations FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM app.org_memberships AS membership
        WHERE membership.organization_id = organizations.id
          AND membership.user_id = app.current_user_id()
          AND membership.status = 'active'
          AND app.nmc_actor_active(organizations.id)
    ));

CREATE POLICY divisions_select ON app.divisions FOR SELECT
    USING (app.nmc_actor_active(organization_id));
CREATE POLICY teams_select ON app.teams FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM app.divisions AS division
        WHERE division.id = teams.division_id
          AND app.nmc_actor_active(division.organization_id)
    ));
CREATE POLICY employees_select ON app.employees FOR SELECT
    USING (app.nmc_actor_active(organization_id) OR user_id = app.current_user_id());
CREATE POLICY org_memberships_select ON app.org_memberships FOR SELECT
    USING (user_id = app.current_user_id() OR app.nmc_actor_active(organization_id));
CREATE POLICY team_memberships_select ON app.team_memberships FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM app.teams AS team
        JOIN app.divisions AS division ON division.id = team.division_id
        WHERE team.id = team_memberships.team_id
          AND (team_memberships.user_id = app.current_user_id() OR app.nmc_actor_active(division.organization_id))
    ));
CREATE POLICY role_grants_select ON app.role_grants FOR SELECT
    USING (user_id = app.current_user_id() OR app.nmc_actor_active(organization_id));

CREATE POLICY requests_select ON app.capability_requests FOR SELECT
    USING (app.nmc_can_read_request(id));
CREATE POLICY requests_insert ON app.capability_requests FOR INSERT
    WITH CHECK (app.nmc_actor_active(organization_id) AND requester_id = app.current_user_id());
CREATE POLICY requests_update ON app.capability_requests FOR UPDATE
    USING (app.nmc_can_read_request(id))
    WITH CHECK (app.nmc_actor_active(organization_id));

CREATE POLICY clarifications_select ON app.capability_clarifications FOR SELECT
    USING (app.nmc_can_read_request(request_id));
CREATE POLICY clarifications_insert ON app.capability_clarifications FOR INSERT
    WITH CHECK (app.nmc_can_read_request(request_id) AND author_id = app.current_user_id());
CREATE POLICY clarifications_update ON app.capability_clarifications FOR UPDATE
    USING (app.nmc_can_read_request(request_id))
    WITH CHECK (app.nmc_can_read_request(request_id));

CREATE POLICY specifications_select ON app.specifications FOR SELECT
    USING (app.nmc_can_read_request(request_id));
CREATE POLICY specifications_insert ON app.specifications FOR INSERT
    WITH CHECK (app.nmc_actor_active(organization_id) AND app.nmc_can_read_request(request_id));
CREATE POLICY specifications_update ON app.specifications FOR UPDATE
    USING (app.nmc_can_read_request(request_id))
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY specification_revisions_select ON app.specification_revisions FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM app.specifications AS spec
        WHERE spec.id = specification_revisions.specification_id
          AND app.nmc_can_read_request(spec.request_id)
    ));
CREATE POLICY specification_revisions_insert ON app.specification_revisions FOR INSERT
    WITH CHECK (EXISTS (
        SELECT 1 FROM app.specifications AS spec
        WHERE spec.id = specification_revisions.specification_id
          AND app.nmc_actor_active(spec.organization_id)
    ));

CREATE POLICY approvals_select ON app.scoped_approvals FOR SELECT
    USING (app.nmc_actor_active(organization_id) AND app.nmc_can_read_request(scope_id));
CREATE POLICY approval_decisions_select ON app.scoped_approval_decisions FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM app.scoped_approvals AS approval
        WHERE approval.id = scoped_approval_decisions.approval_id
          AND app.nmc_actor_active(approval.organization_id)
    ));

CREATE POLICY handovers_all ON app.capability_handovers FOR ALL
    USING (app.nmc_can_read_request(request_id))
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY tasks_all ON app.delivery_tasks FOR ALL
    USING (app.nmc_can_read_request(request_id))
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY projects_select ON app.delivery_projects FOR SELECT
    USING (app.nmc_can_read_project(id));
CREATE POLICY grants_select ON app.delivery_grants FOR SELECT
    USING (app.nmc_can_read_project(project_id) OR user_id = app.current_user_id());
CREATE POLICY catalog_all ON app.catalog_releases FOR ALL
    USING (app.nmc_actor_active(organization_id))
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY evaluations_all ON app.evaluation_records FOR ALL
    USING (app.nmc_actor_active(organization_id))
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY resources_all ON app.resource_records FOR ALL
    USING (app.nmc_can_read_request(request_id))
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY files_select ON app.private_files FOR SELECT
    USING (
        app.nmc_actor_active(organization_id)
        AND (
            (project_id IS NOT NULL AND app.nmc_can_read_project(project_id))
            OR (request_id IS NOT NULL AND app.nmc_can_read_request(request_id))
        )
    );
CREATE POLICY files_insert ON app.private_files FOR INSERT
    WITH CHECK (
        app.nmc_actor_active(organization_id)
        AND owner_id = app.current_user_id()
        AND (
            (project_id IS NOT NULL AND app.nmc_can_read_project(project_id))
            OR (request_id IS NOT NULL AND app.nmc_can_read_request(request_id))
        )
    );
CREATE POLICY files_update ON app.private_files FOR UPDATE
    USING (
        app.nmc_actor_active(organization_id)
        AND (
            (project_id IS NOT NULL AND app.nmc_can_read_project(project_id))
            OR (request_id IS NOT NULL AND app.nmc_can_read_request(request_id))
        )
    )
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY audit_select ON app.internal_audit FOR SELECT
    USING (app.nmc_actor_active(organization_id));
CREATE POLICY audit_insert ON app.internal_audit FOR INSERT
    WITH CHECK (app.nmc_actor_active(organization_id) AND actor_id = app.current_user_id());
CREATE POLICY notifications_select ON app.employee_notifications FOR SELECT
    USING (user_id = app.current_user_id() AND app.nmc_actor_active(organization_id));
CREATE POLICY notifications_insert ON app.employee_notifications FOR INSERT
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY notifications_update ON app.employee_notifications FOR UPDATE
    USING (user_id = app.current_user_id() AND app.nmc_actor_active(organization_id))
    WITH CHECK (user_id = app.current_user_id());
CREATE POLICY outbox_insert ON app.internal_outbox FOR INSERT
    WITH CHECK (true);
CREATE POLICY idempotency_all ON app.command_idempotency FOR ALL
    USING (user_id = app.current_user_id())
    WITH CHECK (user_id = app.current_user_id());

CREATE POLICY runs_select ON nmc3.runs FOR SELECT
    USING (
        app.nmc_actor_active(organization_id)
        AND (project_id IS NULL OR app.nmc_can_read_project(project_id))
    );
CREATE POLICY runs_insert ON nmc3.runs FOR INSERT
    WITH CHECK (
        actor_id = app.current_user_id()
        AND app.nmc_actor_active(organization_id)
        AND (project_id IS NULL OR app.nmc_can_read_project(project_id))
    );
CREATE POLICY runs_update ON nmc3.runs FOR UPDATE
    USING (
        app.nmc_actor_active(organization_id)
        AND (project_id IS NULL OR app.nmc_can_read_project(project_id))
    )
    WITH CHECK (app.nmc_actor_active(organization_id));
CREATE POLICY events_select ON nmc3.run_events FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM nmc3.runs AS run
        WHERE run.id = run_events.run_id
          AND app.nmc_actor_active(run.organization_id)
          AND (run.project_id IS NULL OR app.nmc_can_read_project(run.project_id))
    ));
CREATE POLICY events_insert ON nmc3.run_events FOR INSERT
    WITH CHECK (EXISTS (
        SELECT 1 FROM nmc3.runs AS run
        WHERE run.id = run_events.run_id
          AND app.nmc_actor_active(run.organization_id)
    ));
