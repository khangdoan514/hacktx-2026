-- Project-scoped agent data. All reads and writes run under app_runtime RLS.
CREATE TABLE app.project_agent_context (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES app.delivery_projects (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    agent_id text NOT NULL CHECK (length(agent_id) BETWEEN 1 AND 256),
    kind text NOT NULL CHECK (kind IN ('memory', 'report', 'run_output')),
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    body text NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 65536),
    visibility text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'project')),
    owner_id uuid NOT NULL REFERENCES app.employees (user_id),
    source_run_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    UNIQUE (project_id, source_run_id)
);
CREATE INDEX project_agent_context_latest_idx ON app.project_agent_context
    (project_id, agent_id, kind, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE app.project_agent_shares (
    project_id uuid NOT NULL REFERENCES app.delivery_projects (id) ON DELETE CASCADE,
    agent_id text NOT NULL CHECK (length(agent_id) BETWEEN 1 AND 256),
    shared_by uuid NOT NULL REFERENCES app.employees (user_id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, agent_id)
);

ALTER TABLE app.project_agent_context ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.project_agent_context FORCE ROW LEVEL SECURITY;
ALTER TABLE app.project_agent_shares ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.project_agent_shares FORCE ROW LEVEL SECURITY;

CREATE POLICY project_agent_context_select ON app.project_agent_context FOR SELECT
    USING (app.nmc_can_read_project(project_id)
       AND (owner_id = app.current_user_id() OR (visibility = 'project' AND deleted_at IS NULL)));
CREATE POLICY project_agent_context_insert ON app.project_agent_context FOR INSERT
    WITH CHECK (owner_id = app.current_user_id()
       AND app.nmc_can_read_project(project_id)
       AND app.nmc_actor_active(organization_id)
       AND organization_id = (SELECT organization_id FROM app.delivery_projects WHERE id = project_id));
CREATE POLICY project_agent_context_update ON app.project_agent_context FOR UPDATE
    USING (owner_id = app.current_user_id() AND app.nmc_can_read_project(project_id))
    WITH CHECK (owner_id = app.current_user_id() AND app.nmc_can_read_project(project_id));

CREATE POLICY project_agent_shares_select ON app.project_agent_shares FOR SELECT
    USING (app.nmc_can_read_project(project_id));
CREATE POLICY project_agent_shares_insert ON app.project_agent_shares FOR INSERT
    WITH CHECK (shared_by = app.current_user_id() AND app.nmc_can_read_project(project_id));
CREATE POLICY project_agent_shares_delete ON app.project_agent_shares FOR DELETE
    USING (app.nmc_can_read_project(project_id));

GRANT SELECT, INSERT ON app.project_agent_context TO app_runtime;
GRANT UPDATE (deleted_at) ON app.project_agent_context TO app_runtime;
GRANT SELECT, INSERT, DELETE ON app.project_agent_shares TO app_runtime;
REVOKE ALL ON app.project_agent_context, app.project_agent_shares FROM PUBLIC;
