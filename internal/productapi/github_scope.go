package productapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type githubScopeClaims struct {
	BindingID       string `json:"binding_id"`
	ProjectID       string `json:"project_id"`
	ActorID         string `json:"actor_id"`
	RepositoryID    int64  `json:"repository_id"`
	InstallationID  int64  `json:"installation_id"`
	DeliveryID      string `json:"delivery_id"`
	AutomationRunID string `json:"automation_run_id,omitempty"`
	jwt.RegisteredClaims
}

func signGitHubScope(claims githubScopeClaims) (string, error) {
	secret := os.Getenv("GITHUB_SCOPE_SIGNING_KEY")
	if len(secret) < 32 {
		return "", platform.Fail(http.StatusServiceUnavailable, "unavailable", "GitHub scope signing is not configured")
	}
	now := time.Now().UTC()
	claims.Issuer = "nmc3-api"
	claims.Audience = jwt.ClaimStrings{"nmc3-github-tools"}
	claims.IssuedAt = jwt.NewNumericDate(now)
	claims.ExpiresAt = jwt.NewNumericDate(now.Add(30 * time.Minute))
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func parseGitHubScope(raw string) (*githubScopeClaims, error) {
	secret := os.Getenv("GITHUB_SCOPE_SIGNING_KEY")
	if len(secret) < 32 || len(raw) > 4096 {
		return nil, platform.Fail(http.StatusUnauthorized, "unauthenticated", "Invalid GitHub scope")
	}
	claims := &githubScopeClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return []byte(secret), nil
	}, jwt.WithAudience("nmc3-github-tools"), jwt.WithIssuer("nmc3-api"), jwt.WithExpirationRequired())
	if err != nil || !projectUUID.MatchString(claims.BindingID) || !projectUUID.MatchString(claims.ProjectID) ||
		!projectUUID.MatchString(claims.ActorID) ||
		(projectUUID.MatchString(claims.DeliveryID) == projectUUID.MatchString(claims.AutomationRunID)) ||
		claims.RepositoryID <= 0 || claims.InstallationID <= 0 {
		return nil, platform.Fail(http.StatusUnauthorized, "unauthenticated", "Invalid GitHub scope")
	}
	return claims, nil
}

// resolveGitHubScope is called by the private MCP tool broker. It rechecks the
// binding and actor's project grant before minting a one-repository token.
func (s *Server) resolveGitHubScope(w http.ResponseWriter, r *http.Request) {
	internal := platform.InternalToken()
	if internal == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get(platform.InternalHeader)), []byte(internal)) != 1 {
		writeErr(w, platform.Fail(http.StatusUnauthorized, "unauthenticated", "Invalid internal token"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var input struct {
		Scope string `json:"scope"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		writeErr(w, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid repository scope request"))
		return
	}
	claims, err := parseGitHubScope(input.Scope)
	if err != nil {
		writeErr(w, err)
		return
	}
	var fullName, branch string
	var deliveryID, automationRunID any
	if claims.DeliveryID != "" {
		deliveryID = claims.DeliveryID
	}
	if claims.AutomationRunID != "" {
		automationRunID = claims.AutomationRunID
	}
	err = s.DB.Admin(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT b.full_name,b.branch_name FROM app.project_github_repositories b
			JOIN app.github_installations i ON i.installation_id=b.installation_id
			JOIN app.employees e ON e.user_id=$6 AND e.organization_id=b.organization_id AND e.status='active'
			JOIN app.org_memberships m ON m.user_id=e.user_id AND m.organization_id=e.organization_id AND m.status='active'
			WHERE b.id=$1 AND b.project_id=$2 AND b.repository_id=$3 AND b.installation_id=$4
			AND b.enabled AND b.branch_name IS NOT NULL AND i.status='active' AND (
			EXISTS (SELECT 1 FROM app.github_webhook_deliveries d WHERE d.delivery_id=$5
				AND d.repository_id=b.repository_id AND d.installation_id=b.installation_id)
			OR EXISTS (SELECT 1 FROM app.agent_automation_runs x JOIN app.agent_automations a ON a.id=x.automation_id
				WHERE x.id=$7 AND a.github_binding_id=b.id AND a.project_id=b.project_id AND a.owner_id=$6
				AND a.status<>'paused' AND x.state IN ('dispatching','started')))`,
			claims.BindingID, claims.ProjectID, claims.RepositoryID, claims.InstallationID, deliveryID, claims.ActorID, automationRunID).Scan(&fullName, &branch)
	})
	if err != nil {
		writeErr(w, platform.Fail(http.StatusForbidden, "forbidden", "Repository scope is no longer available"))
		return
	}
	err = s.DB.WithUser(r.Context(), claims.ActorID, "", func(ctx context.Context, tx pgx.Tx) error {
		_, err := projectAccess(ctx, tx, claims.ProjectID)
		return err
	})
	if err != nil {
		writeErr(w, platform.Fail(http.StatusForbidden, "forbidden", "Project access was revoked"))
		return
	}
	app, err := configuredGitHubApp()
	if err != nil {
		writeErr(w, platform.Fail(http.StatusServiceUnavailable, "unavailable", "GitHub App is not configured"))
		return
	}
	appJWT, err := app.appJWT()
	if err != nil {
		writeErr(w, err)
		return
	}
	token, err := app.installationToken(r.Context(), claims.InstallationID, claims.RepositoryID, appJWT)
	if err != nil {
		writeErr(w, platform.Fail(http.StatusBadGateway, "bad_gateway", "GitHub installation token could not be obtained"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "full_name": fullName, "branch": branch})
}
