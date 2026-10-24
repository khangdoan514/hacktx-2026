-- Provenance is nullable so rows created before this migration still load.
-- Application inserts always write attribution for new revisions.

ALTER TABLE app.dashboard_revisions
    ADD COLUMN provenance jsonb,
    ADD COLUMN applied_by uuid REFERENCES app.profiles (id) ON DELETE RESTRICT,
    ADD COLUMN published_by uuid REFERENCES app.profiles (id) ON DELETE RESTRICT;

UPDATE app.dashboard_revisions
SET provenance = jsonb_build_object(
        'actor_type', 'human',
        'actor_id', actor_user_id,
        'source_versions', '[]'::jsonb
    ),
    applied_by = actor_user_id
WHERE provenance IS NULL;

-- Reviewers can decide without owning the target row. The function still
-- requires the caller to be the assigned reviewer and exactly one matching row.
CREATE OR REPLACE FUNCTION app.apply_approval_target(p_approval uuid, p_decision text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = app, pg_temp
AS $$
DECLARE
    rec app.approval_requests%ROWTYPE;
    new_state text;
    updated_count integer;
BEGIN
    SELECT * INTO rec FROM app.approval_requests WHERE id = p_approval;
    IF rec.id IS NULL THEN
        RAISE EXCEPTION 'not_found' USING ERRCODE = 'P0002';
    END IF;
    IF rec.reviewer_id IS DISTINCT FROM app.current_user_id() THEN
        RAISE EXCEPTION 'forbidden' USING ERRCODE = '42501';
    END IF;
    IF rec.target_type = 'task' THEN
        new_state := CASE p_decision
            WHEN 'approved' THEN 'done'
            WHEN 'changes_requested' THEN 'changes_requested'
            WHEN 'rejected' THEN 'changes_requested'
            ELSE NULL
        END;
        UPDATE app.tasks
        SET state = new_state, version = version + 1, updated_at = now()
        WHERE id = rec.target_id AND state = 'in_review' AND version = rec.target_revision;
        GET DIAGNOSTICS updated_count = ROW_COUNT;
    ELSIF rec.target_type = 'requirement' THEN
        new_state := CASE p_decision
            WHEN 'approved' THEN 'accepted'
            WHEN 'changes_requested' THEN 'changes_requested'
            WHEN 'rejected' THEN 'changes_requested'
            ELSE NULL
        END;
        UPDATE app.requirements AS r
        SET state = new_state,
            accepted_revision = CASE WHEN new_state = 'accepted' THEN rec.target_revision ELSE r.accepted_revision END,
            version = r.version + 1,
            updated_at = now()
        WHERE r.id = rec.target_id
          AND r.state = 'in_review'
          AND (
            SELECT MAX(rr.revision)
            FROM app.requirement_revisions rr
            WHERE rr.requirement_id = r.id
          ) = rec.target_revision;
        GET DIAGNOSTICS updated_count = ROW_COUNT;
    ELSE
        RAISE EXCEPTION 'invalid_target' USING ERRCODE = '23514';
    END IF;
    IF new_state IS NULL OR updated_count <> 1 THEN
        RAISE EXCEPTION 'conflict' USING ERRCODE = '40001';
    END IF;
END
$$;

GRANT EXECUTE ON FUNCTION app.apply_approval_target(uuid, text) TO app_runtime;
