-- GitHub App installations are server-managed. A project can bind only a
-- repository that GitHub reports as belonging to an installed account.
CREATE TABLE app.github_installations (
    installation_id bigint PRIMARY KEY CHECK (installation_id > 0),
    account_id bigint NOT NULL CHECK (account_id > 0),
    account_login text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleted')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app.project_github_repositories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES app.delivery_projects (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES app.organizations (id),
    repository_id bigint NOT NULL CHECK (repository_id > 0),
    full_name text NOT NULL CHECK (length(full_name) BETWEEN 3 AND 255),
    installation_id bigint NOT NULL REFERENCES app.github_installations (installation_id),
    enabled boolean NOT NULL DEFAULT true,
    connected_by uuid NOT NULL REFERENCES app.employees (user_id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, repository_id)
);
CREATE INDEX project_github_repositories_repo_idx ON app.project_github_repositories (repository_id, installation_id) WHERE enabled;

-- A one-time GitHub user authorization grants a short selection window. The
-- user token is exchanged and discarded in the callback; only repository IDs
-- returned by GitHub are retained here. No browser can forge candidates.
CREATE TABLE app.github_setup_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES app.delivery_projects (id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES app.employees (user_id) ON DELETE CASCADE,
    state_hash bytea NOT NULL UNIQUE,
    code_verifier text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'exchanging', 'ready', 'failed')),
    candidates jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(candidates) = 'array'),
    error text,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX github_setup_sessions_actor_idx ON app.github_setup_sessions (actor_id, project_id, expires_at);

CREATE TABLE app.github_agent_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    binding_id uuid NOT NULL REFERENCES app.project_github_repositories (id) ON DELETE CASCADE,
    agent_id text NOT NULL CHECK (length(agent_id) BETWEEN 1 AND 256),
    agent_version text NOT NULL CHECK (length(agent_version) BETWEEN 1 AND 80),
    event_name text NOT NULL CHECK (event_name IN ('issues', 'pull_request')),
    action_name text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_by uuid NOT NULL REFERENCES app.employees (user_id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (binding_id, agent_id, agent_version, event_name, action_name),
    CHECK ((event_name = 'issues' AND action_name = 'opened') OR
           (event_name = 'pull_request' AND action_name IN ('opened', 'synchronize')))
);

CREATE TABLE app.github_webhook_deliveries (
    delivery_id uuid PRIMARY KEY,
    installation_id bigint,
    repository_id bigint,
    event_name text NOT NULL,
    action_name text,
    payload jsonb NOT NULL,
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'ignored', 'processed', 'failed')),
    error text,
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz
);
CREATE INDEX github_webhook_deliveries_queued_idx ON app.github_webhook_deliveries (received_at) WHERE state = 'queued';

CREATE TABLE app.github_rule_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id uuid NOT NULL REFERENCES app.github_webhook_deliveries (delivery_id) ON DELETE CASCADE,
    rule_id uuid NOT NULL REFERENCES app.github_agent_rules (id) ON DELETE CASCADE,
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'dispatching', 'started', 'succeeded', 'failed', 'cancelled')),
    attempts integer NOT NULL DEFAULT 0,
    lease_until timestamptz,
    agent_run_id text,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (delivery_id, rule_id)
);
CREATE INDEX github_rule_runs_dispatch_idx ON app.github_rule_runs (lease_until, created_at) WHERE state IN ('queued', 'dispatching');
CREATE INDEX github_rule_runs_started_idx ON app.github_rule_runs (updated_at) WHERE state = 'started';

ALTER TABLE app.github_installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.github_installations FORCE ROW LEVEL SECURITY;
ALTER TABLE app.project_github_repositories ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.project_github_repositories FORCE ROW LEVEL SECURITY;
ALTER TABLE app.github_setup_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.github_setup_sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE app.github_agent_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.github_agent_rules FORCE ROW LEVEL SECURITY;
ALTER TABLE app.github_webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.github_webhook_deliveries FORCE ROW LEVEL SECURITY;
ALTER TABLE app.github_rule_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.github_rule_runs FORCE ROW LEVEL SECURITY;

CREATE POLICY project_github_repositories_select ON app.project_github_repositories FOR SELECT
    USING (app.nmc_can_read_project(project_id));
CREATE POLICY github_setup_sessions_owner ON app.github_setup_sessions FOR ALL
    USING (actor_id = app.current_user_id() AND app.nmc_can_read_project(project_id))
    WITH CHECK (actor_id = app.current_user_id() AND app.nmc_can_read_project(project_id));
CREATE POLICY github_agent_rules_select ON app.github_agent_rules FOR SELECT
    USING (EXISTS (SELECT 1 FROM app.project_github_repositories b
      WHERE b.id = binding_id AND app.nmc_can_read_project(b.project_id)));

-- The application service verifies the GitHub installation and project role
-- before writes. Browser/database clients cannot insert bindings or deliveries.
GRANT SELECT ON app.project_github_repositories, app.github_agent_rules TO app_runtime;
GRANT SELECT, INSERT ON app.github_setup_sessions TO app_runtime;
REVOKE ALL ON app.github_installations, app.github_webhook_deliveries, app.github_rule_runs FROM app_runtime;
REVOKE ALL ON app.github_installations, app.project_github_repositories,
    app.github_setup_sessions, app.github_agent_rules, app.github_webhook_deliveries, app.github_rule_runs FROM PUBLIC;
