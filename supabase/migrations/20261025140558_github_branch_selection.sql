-- A project repository binding targets one Git branch for agent file reads.
-- Existing bindings have no branch until an administrator reconnects them
-- through the branch picker.
ALTER TABLE app.project_github_repositories
    ADD COLUMN branch_name text
    CHECK (length(branch_name) BETWEEN 1 AND 255);
