-- Scheduled repository reports reuse an enabled, project-owned GitHub binding.
ALTER TABLE app.agent_automations
    ADD COLUMN github_binding_id uuid REFERENCES app.project_github_repositories (id) ON DELETE SET NULL,
    ADD CONSTRAINT github_automation_project_required CHECK (github_binding_id IS NULL OR project_id IS NOT NULL);

CREATE INDEX agent_automations_github_binding_idx ON app.agent_automations (github_binding_id)
    WHERE github_binding_id IS NOT NULL;
