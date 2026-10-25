package productapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

// saveAgentRunContext is called only by the private agent process after success.
// The project grant is checked again at save time so a revoked member cannot
// publish a late result.
func (s *Server) saveAgentRunContext(w http.ResponseWriter, r *http.Request) {
	token := platform.InternalToken()
	if token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get(platform.InternalHeader)), []byte(token)) != 1 {
		writeErr(w, platform.Fail(http.StatusUnauthorized, "unauthenticated", "Invalid internal token"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 70<<10)
	var input struct {
		ProjectID string `json:"project_id"`
		ActorID   string `json:"actor_id"`
		RunID     string `json:"run_id"`
		AgentID   string `json:"agent_id"`
		Body      string `json:"body"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || !projectUUID.MatchString(input.ProjectID) || !projectUUID.MatchString(input.ActorID) || len(input.RunID) == 0 || len(input.RunID) > 256 || len(input.AgentID) == 0 || len(input.AgentID) > 256 || len(input.Body) == 0 || len(input.Body) > 65536 {
		writeErr(w, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid run context"))
		return
	}
	err := s.DB.WithUser(r.Context(), input.ActorID, "", func(ctx context.Context, tx pgx.Tx) error {
		orgID, err := projectAccess(ctx, tx, input.ProjectID)
		if err != nil {
			return err
		}
		var shared bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app.project_agent_shares WHERE project_id=$1 AND agent_id=$2)`, input.ProjectID, input.AgentID).Scan(&shared); err != nil {
			return err
		}
		visibility := "private"
		if shared {
			visibility = "project"
		}
		_, err = tx.Exec(ctx, `INSERT INTO app.project_agent_context (project_id, organization_id, agent_id, kind, title, body, visibility, owner_id, source_run_id)
			VALUES ($1,$2,$3,'run_output',$4,$5,$6,$7,$8) ON CONFLICT (project_id, source_run_id) DO NOTHING`,
			input.ProjectID, orgID, input.AgentID, "Run "+input.RunID, input.Body, visibility, input.ActorID, input.RunID)
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "saved"})
}

var projectUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *Server) listAgentProjects(_ http.ResponseWriter, _ *http.Request, ctx context.Context, tx pgx.Tx, _ platform.Actor) (int, any, error) {
	rows, err := tx.Query(ctx, `SELECT id, name FROM app.delivery_projects ORDER BY name LIMIT 100`)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return 0, nil, err
		}
		items = append(items, map[string]string{"id": id, "name": name})
	}
	return http.StatusOK, map[string]any{"items": items}, rows.Err()
}

func (s *Server) createAgentProject(w http.ResponseWriter, r *http.Request, ctx context.Context, _ pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<10)
	var input struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid project request")
	}
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Name) < 2 || len(input.Name) > 120 {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Project name must be 2–120 characters")
	}
	id := platform.UUID()
	err := s.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO app.delivery_projects (id,organization_id,name)
			SELECT $1,$2,$3 WHERE EXISTS (SELECT 1 FROM app.employees e JOIN app.org_memberships m
			ON m.user_id=e.user_id AND m.organization_id=e.organization_id AND m.status='active'
			WHERE e.user_id=$4 AND e.organization_id=$2 AND e.status='active')`, id, actor.OrgID, input.Name, actor.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO app.delivery_grants (project_id,user_id) VALUES ($1,$2)`, id, actor.ID)
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, map[string]string{"id": id, "name": input.Name}, nil
}

type agentContextEntry struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	AgentID     string    `json:"agent_id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Visibility  string    `json:"visibility"`
	OwnerID     string    `json:"owner_id"`
	SourceRunID *string   `json:"source_run_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CanDelete   bool      `json:"can_delete"`
}

func projectAccess(ctx context.Context, tx pgx.Tx, projectID string) (string, error) {
	if !projectUUID.MatchString(projectID) {
		return "", platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid project ID")
	}
	var organizationID string
	err := tx.QueryRow(ctx, `SELECT organization_id FROM app.delivery_projects WHERE id=$1`, projectID).Scan(&organizationID)
	if err == pgx.ErrNoRows {
		return "", platform.Fail(http.StatusNotFound, "not_found", "Project not found")
	}
	return organizationID, err
}

func (s *Server) listProjectAgentContext(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	projectID := r.PathValue("id")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	agentID := r.URL.Query().Get("agent_id")
	rows, err := tx.Query(ctx, `SELECT id, project_id, agent_id, kind, title, body, visibility, owner_id, source_run_id, created_at
		FROM app.project_agent_context WHERE project_id=$1 AND deleted_at IS NULL AND ($2='' OR agent_id=$2)
		ORDER BY created_at DESC, id DESC LIMIT 100`, projectID, agentID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []agentContextEntry{}
	for rows.Next() {
		var item agentContextEntry
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.AgentID, &item.Kind, &item.Title, &item.Body, &item.Visibility, &item.OwnerID, &item.SourceRunID, &item.CreatedAt); err != nil {
			return 0, nil, err
		}
		item.CanDelete = item.OwnerID == actor.ID
		items = append(items, item)
	}
	return http.StatusOK, map[string]any{"items": items}, rows.Err()
}

func (s *Server) createProjectAgentContext(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	projectID := r.PathValue("id")
	orgID, err := projectAccess(ctx, tx, projectID)
	if err != nil {
		return 0, nil, err
	}
	var input struct {
		AgentID    string `json:"agent_id"`
		Kind       string `json:"kind"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		Visibility string `json:"visibility"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 70<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid context JSON")
	}
	input.AgentID = strings.TrimSpace(input.AgentID)
	input.Title = strings.TrimSpace(input.Title)
	if input.Visibility == "" {
		input.Visibility = "private"
	}
	if len(input.AgentID) == 0 || len(input.AgentID) > 256 || len(input.Title) == 0 || len(input.Title) > 200 || len(input.Body) == 0 || len(input.Body) > 65536 ||
		(input.Kind != "memory" && input.Kind != "report") || (input.Visibility != "private" && input.Visibility != "project") {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid context fields")
	}
	var item agentContextEntry
	err = tx.QueryRow(ctx, `INSERT INTO app.project_agent_context (project_id, organization_id, agent_id, kind, title, body, visibility, owner_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, project_id, agent_id, kind, title, body, visibility, owner_id, source_run_id, created_at`,
		projectID, orgID, input.AgentID, input.Kind, input.Title, input.Body, input.Visibility, actor.ID).
		Scan(&item.ID, &item.ProjectID, &item.AgentID, &item.Kind, &item.Title, &item.Body, &item.Visibility, &item.OwnerID, &item.SourceRunID, &item.CreatedAt)
	return http.StatusCreated, item, err
}

func (s *Server) deleteProjectAgentContext(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if _, err := projectAccess(ctx, tx, r.PathValue("id")); err != nil {
		return 0, nil, err
	}
	if !projectUUID.MatchString(r.PathValue("entry")) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid context ID")
	}
	tag, err := tx.Exec(ctx, `UPDATE app.project_agent_context SET deleted_at=now() WHERE project_id=$1 AND id=$2 AND owner_id=$3 AND deleted_at IS NULL`, r.PathValue("id"), r.PathValue("entry"), actor.ID)
	if err != nil {
		return 0, nil, err
	}
	if tag.RowsAffected() == 0 {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Context not found")
	}
	return http.StatusOK, map[string]string{"status": "deleted"}, nil
}

func (s *Server) listProjectAgents(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, _ platform.Actor) (int, any, error) {
	if _, err := projectAccess(ctx, tx, r.PathValue("id")); err != nil {
		return 0, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT agent_id, shared_by, created_at FROM app.project_agent_shares WHERE project_id=$1 ORDER BY agent_id`, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var agentID, sharedBy string
		var createdAt time.Time
		if err := rows.Scan(&agentID, &sharedBy, &createdAt); err != nil {
			return 0, nil, err
		}
		items = append(items, map[string]any{"agent_id": agentID, "shared_by": sharedBy, "created_at": createdAt})
	}
	return http.StatusOK, map[string]any{"agents": items}, rows.Err()
}

func (s *Server) shareProjectAgent(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if _, err := projectAccess(ctx, tx, r.PathValue("id")); err != nil {
		return 0, nil, err
	}
	var input struct {
		AgentID string `json:"agent_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid agent JSON")
	}
	input.AgentID = strings.TrimSpace(input.AgentID)
	if len(input.AgentID) == 0 || len(input.AgentID) > 256 || strings.Contains(input.AgentID, "/") {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid agent ID")
	}
	_, err := tx.Exec(ctx, `INSERT INTO app.project_agent_shares (project_id, agent_id, shared_by) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, r.PathValue("id"), input.AgentID, actor.ID)
	return http.StatusCreated, map[string]string{"agent_id": input.AgentID}, err
}

func (s *Server) unshareProjectAgent(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, _ platform.Actor) (int, any, error) {
	if _, err := projectAccess(ctx, tx, r.PathValue("id")); err != nil {
		return 0, nil, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM app.project_agent_shares WHERE project_id=$1 AND agent_id=$2`, r.PathValue("id"), r.PathValue("agent"))
	if err != nil {
		return 0, nil, err
	}
	if tag.RowsAffected() == 0 {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Shared agent not found")
	}
	return http.StatusOK, map[string]string{"status": "unshared"}, nil
}
