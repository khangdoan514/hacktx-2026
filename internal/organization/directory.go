package organization

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

const (
	AdaID    = "20000000-0000-4000-8000-000000000001"
	BaoID    = "20000000-0000-4000-8000-000000000002"
	CamID    = "20000000-0000-4000-8000-000000000003"
	Password = "local-demo-pass"
)

type Team struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Division string `json:"division"`
}

type Profile struct {
	UserID string   `json:"user_id"`
	Email  string   `json:"email"`
	Name   string   `json:"display_name"`
	Roles  []string `json:"roles"`
	Teams  []Team   `json:"teams"`
}

func Load(ctx context.Context, tx pgx.Tx, userID, email string) (platform.Actor, error) {
	if _, err := tx.Exec(ctx, `SELECT app.provision_profile($1, $2)`, email, email); err != nil {
		return platform.Actor{}, platform.Fail(http.StatusForbidden, "forbidden", "This account is not an NMC2 employee")
	}
	var status, name string
	err := tx.QueryRow(ctx, `
		SELECT employee.status, employee.display_name
		FROM app.employees AS employee
		JOIN app.org_memberships AS membership
		  ON membership.user_id = employee.user_id
		 AND membership.organization_id = employee.organization_id
		 AND membership.status = 'active'
		WHERE employee.user_id = $1 AND employee.organization_id = $2
	`, userID, platform.OrgID).Scan(&status, &name)
	if platform.Missing(err) || status != "active" {
		return platform.Actor{}, platform.Fail(http.StatusForbidden, "forbidden", "This account is not an active NMC2 employee")
	}
	if err != nil {
		return platform.Actor{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT role_code FROM app.role_grants
		WHERE user_id = $1 AND organization_id = $2 AND status = 'active'
		ORDER BY role_code
	`, userID, platform.OrgID)
	if err != nil {
		return platform.Actor{}, err
	}
	defer rows.Close()
	roles := []string{}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return platform.Actor{}, err
		}
		roles = append(roles, role)
	}
	_ = name
	return platform.Actor{ID: userID, Email: email, OrgID: platform.OrgID, Roles: roles}, rows.Err()
}

func Require(actor platform.Actor, roles ...string) error {
	for _, role := range roles {
		if actor.Has(role) {
			return nil
		}
	}
	return platform.Fail(http.StatusForbidden, "forbidden", "You cannot do that")
}

func Me(ctx context.Context, tx pgx.Tx, actor platform.Actor) (Profile, error) {
	profile := Profile{UserID: actor.ID, Email: actor.Email, Roles: actor.Roles, Teams: []Team{}}
	if profile.Roles == nil {
		profile.Roles = []string{}
	}
	if err := tx.QueryRow(ctx, `SELECT display_name FROM app.employees WHERE user_id = $1`, actor.ID).Scan(&profile.Name); err != nil {
		return Profile{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT team.id, team.name, division.name
		FROM app.team_memberships AS membership
		JOIN app.teams AS team ON team.id = membership.team_id
		JOIN app.divisions AS division ON division.id = team.division_id
		WHERE membership.user_id = $1 AND membership.status = 'active'
		ORDER BY division.name, team.name
	`, actor.ID)
	if err != nil {
		return Profile{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var team Team
		if err := rows.Scan(&team.ID, &team.Name, &team.Division); err != nil {
			return Profile{}, err
		}
		profile.Teams = append(profile.Teams, team)
	}
	return profile, rows.Err()
}

func Directory(ctx context.Context, tx pgx.Tx) (map[string]any, error) {
	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM app.organizations WHERE id = $1`, platform.OrgID).Scan(&name); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT division.name, team.id, team.name
		FROM app.divisions AS division
		JOIN app.teams AS team ON team.division_id = division.id
		WHERE division.organization_id = $1
		ORDER BY division.name, team.name
	`, platform.OrgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	divisions := map[string][]Team{}
	order := []string{}
	for rows.Next() {
		var division string
		var team Team
		if err := rows.Scan(&division, &team.ID, &team.Name); err != nil {
			return nil, err
		}
		team.Division = division
		if _, ok := divisions[division]; !ok {
			order = append(order, division)
		}
		divisions[division] = append(divisions[division], team)
	}
	listed := []map[string]any{}
	for _, division := range order {
		listed = append(listed, map[string]any{"name": division, "teams": divisions[division]})
	}
	return map[string]any{"id": platform.OrgID, "name": name, "divisions": listed}, rows.Err()
}

func Invite(ctx context.Context, tx pgx.Tx, actor platform.Actor, email, teamID, role string) (string, string, error) {
	if err := Require(actor, "administrator"); err != nil {
		return "", "", err
	}
	plain, digest := platform.Token()
	var id string
	err := tx.QueryRow(ctx, `SELECT app.create_internal_invitation($1,$2,$3,$4)`, strings.ToLower(strings.TrimSpace(email)), teamID, role, digest).Scan(&id)
	return id, plain, err
}

func Accept(ctx context.Context, tx pgx.Tx, token, display string) error {
	sum := platform.HashBody([]byte(token))
	var org string
	return tx.QueryRow(ctx, `SELECT app.accept_internal_invitation($1,$2)`, sum, display).Scan(&org)
}

func RemoveMembership(ctx context.Context, tx pgx.Tx, actor platform.Actor, userID string) error {
	if err := Require(actor, "administrator"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT app.remove_org_membership($1)`, userID)
	return err
}

func Seed(ctx context.Context, db *platform.DB) error {
	hash, err := platform.HashPassword(Password)
	if err != nil {
		return err
	}
	people := []struct {
		id, email, name, team string
		roles                 []string
	}{
		{AdaID, "ada.hardware@nmc2.test", "Ada Hardware", platform.HardwareTeamID, []string{"requester"}},
		{BaoID, "bao.ai@nmc2.test", "Bao AI", platform.AITeamID, []string{"ai_intake", "ai_engineer"}},
		{CamID, "cam.review@nmc2.test", "Cam Review", platform.ReviewTeamID, []string{"release_approver", "administrator"}},
	}
	return db.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, person := range people {
			if _, err := tx.Exec(ctx, `INSERT INTO auth.users (id, email) VALUES ($1,$2) ON CONFLICT (id) DO NOTHING`, person.id, person.email); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO app.profiles (id, display_name) VALUES ($1,$2) ON CONFLICT (id) DO NOTHING`, person.id, person.name); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO app.local_credentials (user_id, email, password_hash) VALUES ($1,$2,$3)
				ON CONFLICT (user_id) DO NOTHING
			`, person.id, person.email, hash); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO app.employees (user_id, organization_id, email, display_name) VALUES ($1,$2,$3,$4)
				ON CONFLICT (user_id) DO UPDATE SET status = 'active', email = EXCLUDED.email, display_name = EXCLUDED.display_name
			`, person.id, platform.OrgID, person.email, person.name); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO app.org_memberships (organization_id, user_id) VALUES ($1,$2)
				ON CONFLICT (organization_id, user_id) DO UPDATE SET status = 'active', updated_at = now()
			`, platform.OrgID, person.id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO app.team_memberships (team_id, user_id) VALUES ($1,$2)
				ON CONFLICT (team_id, user_id) DO UPDATE SET status = 'active'
			`, person.team, person.id); err != nil {
				return err
			}
			for _, role := range person.roles {
				if _, err := tx.Exec(ctx, `
					INSERT INTO app.role_grants (organization_id, user_id, role_code, scope_type, scope_id)
					VALUES ($1,$2,$3,'team',$4)
					ON CONFLICT (user_id, role_code, scope_type, scope_id) DO UPDATE SET status = 'active'
				`, platform.OrgID, person.id, role, person.team); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func Login(ctx context.Context, db *platform.DB, cfg platform.Config, email, password string) (string, error) {
	if !cfg.AllowLocalAuth {
		return "", platform.Fail(http.StatusNotFound, "not_found", "Not found")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	var userID, hash string
	err := db.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT user_id, password_hash FROM app.local_credentials WHERE email = $1`, email).Scan(&userID, &hash)
	})
	if err != nil || !platform.CheckPassword(hash, password) {
		return "", platform.Fail(http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect")
	}
	return platform.MintToken(cfg, userID, email)
}
