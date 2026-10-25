-- A repository trigger can carry its own task for the agent, written by a
-- project member (for example "label new issues" or "check PRs for missing
-- tests"). Several tasks may now run on the same event.
ALTER TABLE app.github_agent_rules
    ADD COLUMN instructions text NOT NULL DEFAULT ''
    CHECK (length(instructions) <= 2000);

DO $$
DECLARE
    name text;
BEGIN
    -- The generated unique constraint name is truncated by Postgres.
    SELECT conname INTO name FROM pg_constraint
    WHERE conrelid = 'app.github_agent_rules'::regclass AND contype = 'u';
    IF name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE app.github_agent_rules DROP CONSTRAINT %I', name);
    END IF;
END $$;
