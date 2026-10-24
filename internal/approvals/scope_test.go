package approvals

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/platform/testdb"
)

func TestCrossScopeApprovalIsRejected(t *testing.T) {
	db := testdb.DB(t)
	ctx := context.Background()
	author := platform.UUID()
	reviewer := platform.UUID()
	requestA := platform.UUID()
	requestB := platform.UUID()
	specB := platform.UUID()
	orgID := platform.OrgID
	err := db.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, user := range []struct{ id, name string }{{author, "author"}, {reviewer, "reviewer"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO auth.users (id, email) VALUES ($1, $2)`, user.id, user.id+"@nmc2.test"); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO app.profiles (id, display_name) VALUES ($1, $2)`, user.id, user.name); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO app.employees (user_id, organization_id, email, display_name) VALUES ($1,$2,$3,$4)`, user.id, orgID, user.id+"@nmc2.test", user.name); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO app.org_memberships (organization_id, user_id) VALUES ($1,$2)`, orgID, user.id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.role_grants (organization_id, user_id, role_code, scope_type, scope_id) VALUES ($1,$2,'ai_engineer','organization',$1)`, orgID, author); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.role_grants (organization_id, user_id, role_code, scope_type, scope_id) VALUES ($1,$2,'release_approver','organization',$1)`, orgID, reviewer); err != nil {
			return err
		}
		for _, id := range []string{requestA, requestB} {
			if _, err := tx.Exec(ctx, `
				INSERT INTO app.capability_requests (id, organization_id, source_team_id, target_team_id, requester_id, title, state)
				VALUES ($1,$2,$3,$4,$5,'Request','accepted')
			`, id, orgID, platform.HardwareTeamID, platform.AITeamID, author); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO app.specifications (id, organization_id, request_id, state, current_revision, material_revision, created_by)
			VALUES ($1,$2,$3,'in_review',1,1,$4)
		`, specB, orgID, requestB, author)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithUser(ctx, author, author+"@nmc2.test", func(ctx context.Context, tx pgx.Tx) error {
		var raw []byte
		return tx.QueryRow(ctx, `SELECT app.create_scoped_approval($1,$2,'specification',$3,1,$4)`, orgID, requestA, specB, reviewer).Scan(&raw)
	})
	if platform.As(err) == nil || platform.As(err).Code != "cross_scope" {
		t.Fatalf("create accepted cross-scope target: %v", err)
	}
	badID := platform.UUID()
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO app.scoped_approvals (
			id, organization_id, scope_type, scope_id, target_kind, target_id, target_version, review_state, author_id, reviewer_id, state
		) VALUES ($1,$2,'request',$3,'specification',$4,1,'in_review',$5,$6,'pending')
	`, badID, orgID, requestA, specB, author, reviewer); err != nil {
		t.Fatal(err)
	}
	err = db.WithUser(ctx, reviewer, reviewer+"@nmc2.test", func(ctx context.Context, tx pgx.Tx) error {
		var raw []byte
		return tx.QueryRow(ctx, `SELECT app.decide_scoped_approval($1,'approved',1,'ok')`, badID).Scan(&raw)
	})
	if platform.As(err) == nil || platform.As(err).Code != "cross_scope" {
		t.Fatalf("decide accepted cross-scope target: %v", err)
	}
}
