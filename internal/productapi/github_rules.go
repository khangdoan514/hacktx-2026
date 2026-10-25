package productapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type githubRuleView struct {
	ID           string    `json:"id"`
	BindingID    string    `json:"binding_id"`
	FullName     string    `json:"full_name"`
	AgentID      string    `json:"agent_id"`
	AgentVersion string    `json:"agent_version"`
	EventName    string    `json:"event_name"`
	ActionName   string    `json:"action_name"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

func validGitHubRule(event, action string) bool {
	return event == "issues" && action == "opened" || event == "pull_request" && (action == "opened" || action == "synchronize")
}

func (s *Server) listGitHubRules(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, _ platform.Actor) (int, any, error) {
	projectID := r.PathValue("id")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.binding_id::text,b.full_name,r.agent_id,r.agent_version,
		r.event_name,r.action_name,r.enabled,r.created_at FROM app.github_agent_rules r
		JOIN app.project_github_repositories b ON b.id=r.binding_id
		WHERE b.project_id=$1 ORDER BY r.created_at DESC`, projectID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []githubRuleView{}
	for rows.Next() {
		var item githubRuleView
		if err := rows.Scan(&item.ID, &item.BindingID, &item.FullName, &item.AgentID, &item.AgentVersion,
			&item.EventName, &item.ActionName, &item.Enabled, &item.CreatedAt); err != nil {
			return 0, nil, err
		}
		items = append(items, item)
	}
	return http.StatusOK, map[string]any{"items": items}, rows.Err()
}

func (s *Server) createGitHubRule(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	projectID := r.PathValue("id")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<10)
	var input struct {
		BindingID  string `json:"binding_id"`
		EventName  string `json:"event_name"`
		ActionName string `json:"action_name"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !projectUUID.MatchString(input.BindingID) || !validGitHubRule(input.EventName, input.ActionName) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Choose a repository and supported GitHub event")
	}
	var id string
	err := s.DB.Admin(ctx, func(ctx context.Context, admin pgx.Tx) error {
		return admin.QueryRow(ctx, `INSERT INTO app.github_agent_rules
			(binding_id,agent_id,agent_version,event_name,action_name,created_by)
			SELECT b.id,'github-repo-reviewer','1.0.0',$3,$4,$5
			FROM app.project_github_repositories b WHERE b.id=$1 AND b.project_id=$2 AND b.enabled
			AND EXISTS (SELECT 1 FROM app.delivery_grants WHERE project_id=$2 AND user_id=$5 AND status='active')
			ON CONFLICT (binding_id,agent_id,agent_version,event_name,action_name)
			DO UPDATE SET enabled=true,updated_at=now() RETURNING id::text`,
			input.BindingID, projectID, input.EventName, input.ActionName, actor.ID).Scan(&id)
	})
	if err == pgx.ErrNoRows {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Enabled repository binding not found")
	}
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, map[string]string{"id": id}, nil
}

func (s *Server) setGitHubRuleStatus(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	projectID, ruleID := r.PathValue("id"), r.PathValue("rule")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	if !projectUUID.MatchString(ruleID) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid GitHub rule")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Enabled == nil {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Enabled is required")
	}
	var updated string
	err := s.DB.Admin(ctx, func(ctx context.Context, admin pgx.Tx) error {
		return admin.QueryRow(ctx, `UPDATE app.github_agent_rules r SET enabled=$3,updated_at=now()
			FROM app.project_github_repositories b WHERE r.id=$1 AND r.binding_id=b.id AND b.project_id=$2
			AND EXISTS (SELECT 1 FROM app.delivery_grants WHERE project_id=$2 AND user_id=$4 AND status='active')
			RETURNING r.id::text`, ruleID, projectID, *input.Enabled, actor.ID).Scan(&updated)
	})
	if err == pgx.ErrNoRows {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "GitHub rule not found")
	}
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"id": updated, "enabled": *input.Enabled}, nil
}

func (s *Server) listGitHubRuleRuns(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, _ platform.Actor) (int, any, error) {
	projectID := r.PathValue("id")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	items := []map[string]any{}
	err := s.DB.Admin(ctx, func(ctx context.Context, admin pgx.Tx) error {
		rows, err := admin.Query(ctx, `SELECT x.id::text,b.full_name,d.event_name,d.action_name,x.state,
			x.agent_run_id,x.error,x.created_at FROM app.github_rule_runs x
			JOIN app.github_agent_rules r ON r.id=x.rule_id
			JOIN app.project_github_repositories b ON b.id=r.binding_id
			JOIN app.github_webhook_deliveries d ON d.delivery_id=x.delivery_id
			WHERE b.project_id=$1 ORDER BY x.created_at DESC LIMIT 50`, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name, event, action, state string
			var runID, failure *string
			var created time.Time
			if err := rows.Scan(&id, &name, &event, &action, &state, &runID, &failure, &created); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "repository": name, "event_name": event, "action_name": action, "state": state, "agent_run_id": runID, "error": failure, "created_at": created})
		}
		return rows.Err()
	})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"items": items}, nil
}
