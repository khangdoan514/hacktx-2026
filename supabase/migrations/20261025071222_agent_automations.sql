-- Durable schedules owned by an employee. Occurrences record every due time,
-- including dispatch failures, so a restart can resume work without losing it.
CREATE TABLE app.agent_automations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    owner_id uuid NOT NULL REFERENCES app.employees (user_id),
    project_id uuid REFERENCES app.delivery_projects (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    agent_id text NOT NULL CHECK (length(agent_id) BETWEEN 1 AND 256),
    agent_version text NOT NULL CHECK (length(agent_version) BETWEEN 1 AND 80),
    input jsonb NOT NULL,
    limits jsonb NOT NULL,
    schedule_kind text NOT NULL CHECK (schedule_kind IN ('once', 'interval', 'daily', 'weekly')),
    run_at timestamptz,
    interval_seconds integer,
    local_time text,
    weekday smallint,
    timezone text NOT NULL DEFAULT 'UTC',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'completed')),
    next_run_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (schedule_kind <> 'once' OR run_at IS NOT NULL),
    CHECK (schedule_kind <> 'interval' OR interval_seconds BETWEEN 60 AND 2592000),
    CHECK (schedule_kind NOT IN ('daily', 'weekly') OR local_time IS NOT NULL),
    CHECK (schedule_kind <> 'weekly' OR weekday BETWEEN 0 AND 6)
);
CREATE INDEX agent_automations_due_idx ON app.agent_automations (next_run_at, id)
    WHERE status = 'active' AND next_run_at IS NOT NULL;
CREATE INDEX agent_automations_owner_idx ON app.agent_automations (owner_id, created_at DESC);

CREATE TABLE app.agent_automation_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    automation_id uuid NOT NULL REFERENCES app.agent_automations (id) ON DELETE CASCADE,
    scheduled_for timestamptz NOT NULL,
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'dispatching', 'started', 'succeeded', 'failed', 'cancelled')),
    agent_run_id text,
    attempts integer NOT NULL DEFAULT 0,
    lease_until timestamptz,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (automation_id, scheduled_for)
);
CREATE INDEX agent_automation_runs_dispatch_idx ON app.agent_automation_runs (lease_until, created_at)
    WHERE state IN ('queued', 'dispatching');
CREATE INDEX agent_automation_runs_started_idx ON app.agent_automation_runs (updated_at)
    WHERE state = 'started';

ALTER TABLE app.agent_automations ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.agent_automations FORCE ROW LEVEL SECURITY;
ALTER TABLE app.agent_automation_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.agent_automation_runs FORCE ROW LEVEL SECURITY;

CREATE POLICY agent_automations_select ON app.agent_automations FOR SELECT
    USING (owner_id = app.current_user_id() AND app.nmc_actor_active(organization_id));
CREATE POLICY agent_automations_insert ON app.agent_automations FOR INSERT
    WITH CHECK (owner_id = app.current_user_id() AND app.nmc_actor_active(organization_id)
      AND (project_id IS NULL OR app.nmc_can_read_project(project_id)));
CREATE POLICY agent_automations_update ON app.agent_automations FOR UPDATE
    USING (owner_id = app.current_user_id() AND app.nmc_actor_active(organization_id))
    WITH CHECK (owner_id = app.current_user_id() AND app.nmc_actor_active(organization_id)
      AND (project_id IS NULL OR app.nmc_can_read_project(project_id)));
CREATE POLICY agent_automation_runs_select ON app.agent_automation_runs FOR SELECT
    USING (EXISTS (SELECT 1 FROM app.agent_automations a
      WHERE a.id = automation_id AND a.owner_id = app.current_user_id()
        AND app.nmc_actor_active(a.organization_id)));
CREATE POLICY agent_automation_runs_insert ON app.agent_automation_runs FOR INSERT
    WITH CHECK (state = 'queued' AND attempts = 0 AND agent_run_id IS NULL
      AND EXISTS (SELECT 1 FROM app.agent_automations a
        WHERE a.id = automation_id AND a.owner_id = app.current_user_id()
          AND app.nmc_actor_active(a.organization_id)));
CREATE POLICY agent_automation_runs_cancel ON app.agent_automation_runs FOR UPDATE
    USING (state = 'queued' AND EXISTS (SELECT 1 FROM app.agent_automations a
      WHERE a.id = automation_id AND a.owner_id = app.current_user_id()
        AND app.nmc_actor_active(a.organization_id)))
    WITH CHECK (state = 'cancelled' AND EXISTS (SELECT 1 FROM app.agent_automations a
      WHERE a.id = automation_id AND a.owner_id = app.current_user_id()
        AND app.nmc_actor_active(a.organization_id)));

GRANT SELECT, INSERT, UPDATE (status, next_run_at, updated_at) ON app.agent_automations TO app_runtime;
GRANT SELECT, INSERT ON app.agent_automation_runs TO app_runtime;
GRANT UPDATE (state, updated_at) ON app.agent_automation_runs TO app_runtime;
REVOKE ALL ON app.agent_automations, app.agent_automation_runs FROM PUBLIC;
