package organization

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/platform/testdb"
)

func TestRevokedMembershipRemovesProjectFileAndRunAccess(t *testing.T) {
	db := testdb.DB(t)
	ctx := context.Background()
	userID := platform.UUID()
	projectID := platform.UUID()
	fileID := platform.UUID()
	runID := "run_" + platform.UUID()
	orgID := platform.OrgID
	err := db.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO auth.users (id, email) VALUES ($1, $2)`, userID, userID+"@nmc2.test"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.profiles (id, display_name) VALUES ($1, 'Ada')`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.employees (user_id, organization_id, email, display_name) VALUES ($1,$2,$3,'Ada')`, userID, orgID, userID+"@nmc2.test"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.org_memberships (organization_id, user_id) VALUES ($1,$2)`, orgID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.delivery_projects (id, organization_id, name) VALUES ($1,$2,'GPU slice')`, projectID, orgID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.delivery_grants (project_id, user_id) VALUES ($1,$2)`, projectID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO app.private_files (id, organization_id, project_id, owner_id, filename, state)
			VALUES ($1,$2,$3,$4,'notes.txt','ready')
		`, fileID, orgID, projectID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO nmc3.runs (id, organization_id, actor_id, project_id, state, agent_id, agent_version, execution_mode, adapter_version, snapshot, created_at, updated_at)
			VALUES ($1,$2,$3,$4,'SUCCEEDED','pr-review-orchestrator','1.0.0','dummy','dummy-1','{}'::jsonb, now(), now())
		`, runID, orgID, userID, projectID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var bypass bool
	if err := db.Pool.QueryRow(ctx, `SELECT rolbypassrls OR rolsuper FROM pg_roles WHERE rolname = 'app_runtime'`).Scan(&bypass); err != nil || bypass {
		t.Fatalf("app_runtime must not bypass RLS: %v bypass=%v", err, bypass)
	}
	count := func() (int, int, int) {
		t.Helper()
		var projects, files, runs int
		err := db.WithUser(ctx, userID, userID+"@nmc2.test", func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM app.delivery_projects WHERE id = $1`, projectID).Scan(&projects); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM app.private_files WHERE id = $1`, fileID).Scan(&files); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM nmc3.runs WHERE id = $1`, runID).Scan(&runs)
		})
		if err != nil {
			t.Fatal(err)
		}
		return projects, files, runs
	}
	if projects, files, runs := count(); projects != 1 || files != 1 || runs != 1 {
		t.Fatalf("active member could not read project=%d file=%d run=%d", projects, files, runs)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE app.org_memberships SET status = 'removed' WHERE user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if projects, files, runs := count(); projects != 0 || files != 0 || runs != 0 {
		t.Fatalf("project grant survived membership removal project=%d file=%d run=%d", projects, files, runs)
	}
}
