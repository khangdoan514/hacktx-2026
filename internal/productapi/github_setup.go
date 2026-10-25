package productapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

func randomGitHubState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Server) githubSetupConfig(_ http.ResponseWriter, _ *http.Request, _ context.Context, _ pgx.Tx, _ platform.Actor) (int, any, error) {
	_, err := configuredGitHubOAuth()
	slug := os.Getenv("GITHUB_APP_SLUG")
	installURL := ""
	if validGitHubName(slug) {
		installURL = "https://github.com/apps/" + slug + "/installations/new"
	}
	return http.StatusOK, map[string]any{"configured": err == nil, "install_url": installURL}, nil
}

func (s *Server) startGitHubSetup(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	projectID := r.PathValue("id")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	oauth, err := configuredGitHubOAuth()
	if err != nil {
		return 0, nil, platform.Fail(http.StatusServiceUnavailable, "unavailable", err.Error())
	}
	state, err := randomGitHubState()
	if err != nil {
		return 0, nil, err
	}
	verifier, err := randomGitHubState()
	if err != nil {
		return 0, nil, err
	}
	hash := sha256.Sum256([]byte(state))
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO app.github_setup_sessions
		(project_id,actor_id,state_hash,code_verifier,expires_at)
		VALUES ($1,$2,$3,$4,now()+interval '15 minutes') RETURNING id::text`,
		projectID, actor.ID, hash[:], verifier).Scan(&id)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, map[string]string{"session_id": id, "authorize_url": oauth.authorize(state, verifier)}, nil
}

func (s *Server) githubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if len(state) < 40 || len(state) > 128 {
		http.Error(w, "Invalid GitHub authorization state", http.StatusBadRequest)
		return
	}
	hash := sha256.Sum256([]byte(state))
	var id, projectID, verifier string
	err := s.DB.Admin(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE app.github_setup_sessions SET status='exchanging',updated_at=now()
			WHERE state_hash=$1 AND status='pending' AND expires_at>now()
			RETURNING id::text,project_id::text,code_verifier`, hash[:]).Scan(&id, &projectID, &verifier)
	})
	if err != nil {
		http.Error(w, "GitHub authorization was already used or expired", http.StatusBadRequest)
		return
	}
	result := "ready"
	failure := ""
	candidates := []githubCandidate{}
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		result, failure = "failed", "GitHub authorization was cancelled or denied"
	} else {
		oauth, configErr := configuredGitHubOAuth()
		if configErr != nil {
			result, failure = "failed", "GitHub authorization is not configured"
		} else {
			token, exchangeErr := oauth.exchange(r.Context(), r.URL.Query().Get("code"), verifier)
			if exchangeErr != nil {
				result, failure = "failed", exchangeErr.Error()
			} else {
				candidates, exchangeErr = oauth.candidateRepositories(r.Context(), token)
				if exchangeErr != nil {
					result, failure = "failed", "GitHub repositories could not be listed"
				}
			}
		}
	}
	data, _ := json.Marshal(candidates)
	if err := s.DB.Admin(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app.github_setup_sessions
			SET status=$2,candidates=$3::jsonb,error=NULLIF($4,''),code_verifier='',updated_at=now()
			WHERE id=$1 AND status='exchanging'`, id, result, string(data), failure)
		return err
	}); err != nil {
		http.Error(w, "GitHub setup could not be saved", http.StatusInternalServerError)
		return
	}
	returnURL := strings.TrimRight(os.Getenv("GITHUB_APP_CONSOLE_URL"), "/")
	parsed, parseErr := url.Parse(returnURL)
	if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
		writeJSON(w, http.StatusOK, map[string]string{"session_id": id, "result": result})
		return
	}
	query := url.Values{"project": {projectID}, "setup": {id}, "result": {result}}
	http.Redirect(w, r, returnURL+"/github?"+query.Encode(), http.StatusSeeOther)
}

func (s *Server) getGitHubSetup(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	projectID, id := r.PathValue("id"), r.PathValue("session")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	if !projectUUID.MatchString(id) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid setup session")
	}
	var status string
	var data json.RawMessage
	var failure *string
	var expires time.Time
	err := tx.QueryRow(ctx, `SELECT status,candidates,error,expires_at FROM app.github_setup_sessions
		WHERE id=$1 AND project_id=$2 AND actor_id=$3`, id, projectID, actor.ID).Scan(&status, &data, &failure, &expires)
	if err == pgx.ErrNoRows {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Setup session not found")
	}
	if err != nil {
		return 0, nil, err
	}
	if time.Now().After(expires) {
		status = "expired"
		data = json.RawMessage(`[]`)
	}
	return http.StatusOK, map[string]any{"status": status, "candidates": data, "error": failure, "expires_at": expires}, nil
}

func githubSetupCandidate(ctx context.Context, tx pgx.Tx, projectID, sessionID, actorID string, repositoryID int64) (*githubCandidate, error) {
	var data []byte
	err := tx.QueryRow(ctx, `SELECT candidates FROM app.github_setup_sessions
		WHERE id=$1 AND project_id=$2 AND actor_id=$3 AND status='ready' AND expires_at>now()`,
		sessionID, projectID, actorID).Scan(&data)
	if err == pgx.ErrNoRows {
		return nil, platform.Fail(http.StatusForbidden, "forbidden", "GitHub setup expired or belongs to another user")
	}
	if err != nil {
		return nil, err
	}
	var candidates []githubCandidate
	if err := json.Unmarshal(data, &candidates); err != nil {
		return nil, err
	}
	for i := range candidates {
		if candidates[i].RepositoryID == repositoryID {
			return &candidates[i], nil
		}
	}
	return nil, platform.Fail(http.StatusForbidden, "forbidden", "Repository was not authorized for this setup")
}

func (s *Server) listGitHubSetupBranches(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	projectID, sessionID := r.PathValue("id"), r.PathValue("session")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	repositoryID, err := strconv.ParseInt(r.PathValue("repository"), 10, 64)
	if err != nil || repositoryID <= 0 || !projectUUID.MatchString(sessionID) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid repository selection")
	}
	candidate, err := githubSetupCandidate(ctx, tx, projectID, sessionID, actor.ID, repositoryID)
	if err != nil {
		return 0, nil, err
	}
	app, err := configuredGitHubApp()
	if err != nil {
		return 0, nil, platform.Fail(http.StatusServiceUnavailable, "unavailable", "GitHub App is not configured")
	}
	verified, err := app.verifyRepository(ctx, candidate.InstallationID, candidate.RepositoryID, candidate.FullName)
	if err != nil || verified.AccountID != candidate.AccountID {
		return 0, nil, platform.Fail(http.StatusForbidden, "forbidden", "GitHub installation no longer grants this repository")
	}
	branches, err := app.listBranches(ctx, verified.InstallationID, verified.RepositoryID, verified.FullName)
	if err != nil {
		return 0, nil, platform.Fail(http.StatusBadGateway, "bad_gateway", "GitHub branches could not be listed")
	}
	return http.StatusOK, map[string]any{"branches": branches}, nil
}

func (s *Server) bindGitHubRepository(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	projectID := r.PathValue("id")
	orgID, err := projectAccess(ctx, tx, projectID)
	if err != nil {
		return 0, nil, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var input struct {
		SessionID    string `json:"session_id"`
		RepositoryID int64  `json:"repository_id"`
		Branch       string `json:"branch"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !projectUUID.MatchString(input.SessionID) || input.RepositoryID <= 0 || input.Branch == "" || len(input.Branch) > 255 {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Choose a repository and branch from a valid setup session")
	}
	selected, err := githubSetupCandidate(ctx, tx, projectID, input.SessionID, actor.ID, input.RepositoryID)
	if err != nil {
		return 0, nil, err
	}
	app, err := configuredGitHubApp()
	if err != nil {
		return 0, nil, platform.Fail(http.StatusServiceUnavailable, "unavailable", "GitHub App is not configured")
	}
	verified, err := app.verifyRepository(ctx, selected.InstallationID, selected.RepositoryID, selected.FullName)
	if err != nil || verified.AccountID != selected.AccountID {
		return 0, nil, platform.Fail(http.StatusForbidden, "forbidden", "GitHub installation no longer grants this repository")
	}
	branches, err := app.listBranches(ctx, verified.InstallationID, verified.RepositoryID, verified.FullName)
	if err != nil {
		return 0, nil, platform.Fail(http.StatusBadGateway, "bad_gateway", "GitHub branches could not be listed")
	}
	validBranch := false
	for _, branch := range branches {
		if branch == input.Branch {
			validBranch = true
			break
		}
	}
	if !validBranch {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Selected branch is no longer available")
	}
	var bindingID string
	err = s.DB.Admin(ctx, func(ctx context.Context, admin pgx.Tx) error {
		if _, err := admin.Exec(ctx, `INSERT INTO app.github_installations
			(installation_id,account_id,account_login,status) VALUES ($1,$2,$3,'active')
			ON CONFLICT (installation_id) DO UPDATE SET account_id=EXCLUDED.account_id,
			account_login=EXCLUDED.account_login,status='active',updated_at=now()`,
			verified.InstallationID, verified.AccountID, verified.AccountLogin); err != nil {
			return err
		}
		return admin.QueryRow(ctx, `INSERT INTO app.project_github_repositories
			(project_id,organization_id,repository_id,full_name,installation_id,connected_by,branch_name)
			SELECT $1,$2,$3,$4,$5,$6,$7 WHERE EXISTS (
				SELECT 1 FROM app.delivery_grants WHERE project_id=$1 AND user_id=$6 AND status='active')
			ON CONFLICT (project_id,repository_id) DO UPDATE SET full_name=EXCLUDED.full_name,
			installation_id=EXCLUDED.installation_id,branch_name=EXCLUDED.branch_name,enabled=true,connected_by=EXCLUDED.connected_by,updated_at=now()
			RETURNING id::text`, projectID, orgID, verified.RepositoryID, verified.FullName,
			verified.InstallationID, actor.ID, input.Branch).Scan(&bindingID)
	})
	if err == pgx.ErrNoRows {
		return 0, nil, platform.Fail(http.StatusForbidden, "forbidden", "Project access was revoked")
	}
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, map[string]any{"id": bindingID, "repository_id": verified.RepositoryID, "full_name": verified.FullName, "branch": input.Branch}, nil
}

func (s *Server) listGitHubRepositories(_ http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, _ platform.Actor) (int, any, error) {
	projectID := r.PathValue("id")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text,repository_id,full_name,branch_name,enabled,installation_id,created_at
		FROM app.project_github_repositories WHERE project_id=$1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name string
		var branch *string
		var repoID, installID int64
		var enabled bool
		var created time.Time
		if err := rows.Scan(&id, &repoID, &name, &branch, &enabled, &installID, &created); err != nil {
			return 0, nil, err
		}
		items = append(items, map[string]any{"id": id, "repository_id": repoID, "full_name": name, "branch": branch, "enabled": enabled, "installation_id": installID, "created_at": created})
	}
	return http.StatusOK, map[string]any{"items": items}, rows.Err()
}

func (s *Server) setGitHubRepositoryStatus(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := automationPermission(actor); err != nil {
		return 0, nil, err
	}
	projectID, bindingID := r.PathValue("id"), r.PathValue("binding")
	if _, err := projectAccess(ctx, tx, projectID); err != nil {
		return 0, nil, err
	}
	if !projectUUID.MatchString(bindingID) {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid binding")
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
		return admin.QueryRow(ctx, `UPDATE app.project_github_repositories SET enabled=$3,updated_at=now()
			WHERE id=$1 AND project_id=$2 AND EXISTS (
				SELECT 1 FROM app.delivery_grants WHERE project_id=$2 AND user_id=$4 AND status='active')
			RETURNING id::text`, bindingID, projectID, *input.Enabled, actor.ID).Scan(&updated)
	})
	if err == pgx.ErrNoRows {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Repository binding not found")
	}
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"id": updated, "enabled": *input.Enabled}, nil
}
