package productapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/platform/testdb"
)

func TestGitHubSetupBindsOnlyUserAuthorizedRepository(t *testing.T) {
	ctx := context.Background()
	db := testdb.DB(t)
	if err := organization.Seed(ctx, db); err != nil {
		t.Fatal(err)
	}
	projectID := platform.UUID()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO app.delivery_projects (id,organization_id,name) VALUES ($1,$2,'Setup test')`, projectID, platform.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO app.delivery_grants (project_id,user_id) VALUES ($1,$2)`, projectID, organization.BaoID); err != nil {
		t.Fatal(err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "github.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}), 0600); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/oauth/access_token":
			if err := r.ParseForm(); err != nil || r.Form.Get("code_verifier") == "" {
				t.Errorf("OAuth verifier missing: %v", err)
			}
			_, _ = w.Write([]byte(`{"access_token":"user-token"}`))
		case "/user/installations":
			if r.Header.Get("Authorization") != "Bearer user-token" {
				t.Error("user token missing")
			}
			_, _ = w.Write([]byte(`{"installations":[{"id":123,"account":{"id":456,"login":"example"}}]}`))
		case "/user/installations/123/repositories":
			_, _ = w.Write([]byte(`{"repositories":[{"id":789,"full_name":"example/private"}]}`))
		case "/app/installations/123":
			_, _ = w.Write([]byte(`{"id":123,"account":{"id":456,"login":"example"},"suspended_at":null}`))
		case "/app/installations/123/access_tokens":
			var body struct {
				RepositoryIDs []int64 `json:"repository_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.RepositoryIDs) != 1 || body.RepositoryIDs[0] != 789 {
				t.Errorf("scoped token request=%+v err=%v", body, err)
			}
			_, _ = w.Write([]byte(`{"token":"installation-token"}`))
		case "/repos/example/private":
			_, _ = w.Write([]byte(`{"id":789,"full_name":"example/private"}`))
		case "/repos/example/private/branches":
			_, _ = w.Write([]byte(`[{"name":"main"},{"name":"release/demo"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	t.Setenv("GITHUB_APP_CLIENT_ID", "test-app")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "test-secret")
	t.Setenv("GITHUB_APP_PRIVATE_KEY_FILE", keyPath)
	t.Setenv("GITHUB_APP_CALLBACK_URL", "http://localhost:3010/api/v1/github/oauth/callback")
	t.Setenv("GITHUB_OAUTH_TOKEN_URL", upstream.URL+"/login/oauth/access_token")
	t.Setenv("GITHUB_API_URL", upstream.URL)
	t.Setenv("GITHUB_APP_CONSOLE_URL", "")
	cfg := platform.Config{Environment: "test", AllowLocalAuth: true, AgentExecutionMode: "dummy", JWTSecret: "test-secret-test-secret-test-secret", JWTIssuer: "http://127.0.0.1/test-auth", JWTAudience: "authenticated", StorageDir: t.TempDir()}
	server, _, err := Wire(ctx, cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(server.Handler())
	defer app.Close()
	bao := login(t, app.URL, "bao.ai@nmc2.test")
	var started struct {
		SessionID    string `json:"session_id"`
		AuthorizeURL string `json:"authorize_url"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/projects/"+projectID+"/github/setup", nil, &started)
	if started.SessionID == "" {
		t.Fatal("missing setup session")
	}
	authURL, err := url.Parse(started.AuthorizeURL)
	if err != nil || authURL.Query().Get("code_challenge") == "" || authURL.Query().Get("state") == "" {
		t.Fatalf("authorize URL=%s err=%v", started.AuthorizeURL, err)
	}
	callback := "/api/v1/github/oauth/callback?state=" + url.QueryEscape(authURL.Query().Get("state")) + "&code=one-time-code"
	res, err := http.Get(app.URL + callback)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("callback status=%d", res.StatusCode)
	}
	var selection struct {
		Status     string            `json:"status"`
		Candidates []githubCandidate `json:"candidates"`
	}
	must(t, app.URL, bao, http.MethodGet, "/api/v1/projects/"+projectID+"/github/setup/"+started.SessionID, nil, &selection)
	if selection.Status != "ready" || len(selection.Candidates) != 1 || selection.Candidates[0].RepositoryID != 789 {
		t.Fatalf("selection=%+v", selection)
	}
	var branchList struct {
		Branches []string `json:"branches"`
	}
	must(t, app.URL, bao, http.MethodGet, "/api/v1/projects/"+projectID+"/github/setup/"+started.SessionID+"/repositories/789/branches", nil, &branchList)
	if len(branchList.Branches) != 2 || branchList.Branches[1] != "release/demo" {
		t.Fatalf("branches=%+v", branchList.Branches)
	}
	path := "/api/v1/projects/" + projectID + "/github-repositories"
	status, _ := call(t, app.URL, bao, http.MethodPost, path, map[string]any{"session_id": started.SessionID, "repository_id": 999, "branch": "main"})
	if status != http.StatusForbidden {
		t.Fatalf("unlisted repo status=%d", status)
	}
	status, _ = call(t, app.URL, bao, http.MethodPost, path, map[string]any{"session_id": started.SessionID, "repository_id": 789, "branch": "other"})
	if status != http.StatusBadRequest {
		t.Fatalf("unlisted branch status=%d", status)
	}
	var bound struct {
		ID       string `json:"id"`
		FullName string `json:"full_name"`
		Branch   string `json:"branch"`
	}
	must(t, app.URL, bao, http.MethodPost, path, map[string]any{"session_id": started.SessionID, "repository_id": 789, "branch": "release/demo"}, &bound)
	if bound.ID == "" || bound.FullName != "example/private" || bound.Branch != "release/demo" {
		t.Fatalf("binding=%+v", bound)
	}
	var rules struct {
		Items []githubRuleView `json:"items"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/projects/"+projectID+"/github-rules",
		map[string]string{"binding_id": bound.ID, "event_name": "issues", "action_name": "opened"}, nil)
	must(t, app.URL, bao, http.MethodGet, "/api/v1/projects/"+projectID+"/github-rules", nil, &rules)
	if len(rules.Items) != 1 || !rules.Items[0].Enabled || rules.Items[0].FullName != "example/private" {
		t.Fatalf("rules=%+v", rules.Items)
	}
	// The OAuth state is single use and cannot be replayed to replace candidates.
	res, err = http.Get(app.URL + callback)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("replayed callback status=%d", res.StatusCode)
	}
	if strings.Contains(started.AuthorizeURL, "test-secret") {
		t.Fatal("OAuth client secret appeared in browser URL")
	}
}
