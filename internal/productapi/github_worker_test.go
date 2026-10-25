package productapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/platform/testdb"
)

func TestGitHubDeliveryWorkerStartsScopedAgentOnce(t *testing.T) {
	ctx := context.Background()
	db := testdb.DB(t)
	if err := organization.Seed(ctx, db); err != nil {
		t.Fatal(err)
	}
	projectID, deliveryID := platform.UUID(), platform.UUID()
	installationID := time.Now().UnixNano()
	repositoryID := installationID + 1
	var bindingID string
	if _, err := db.Pool.Exec(ctx, `INSERT INTO app.delivery_projects (id,organization_id,name) VALUES ($1,$2,'GitHub worker test')`, projectID, platform.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO app.delivery_grants (project_id,user_id) VALUES ($1,$2)`, projectID, organization.BaoID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO app.github_installations (installation_id,account_id,account_login) VALUES ($1,20,'demo')`, installationID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `INSERT INTO app.project_github_repositories
		(project_id,organization_id,repository_id,full_name,installation_id,connected_by,branch_name)
		VALUES ($1,$2,$3,'demo/private',$4,$5,'main') RETURNING id::text`, projectID, platform.OrgID, repositoryID, installationID, organization.BaoID).Scan(&bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO app.github_agent_rules
		(binding_id,agent_id,agent_version,event_name,action_name,created_by)
		VALUES ($1,'github-repo-reviewer','1.0.0','issues','opened',$2)`, bindingID, organization.BaoID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO app.github_webhook_deliveries
		(delivery_id,installation_id,repository_id,event_name,action_name,payload)
		VALUES ($1,$2,$3,'issues','opened','{"issue":{"number":7}}')`, deliveryID, installationID, repositoryID); err != nil {
		t.Fatal(err)
	}
	starts := 0
	var sent map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-runs":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload["project_id"] == projectID {
				starts++
				sent = payload
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"run_id":"github-run-1","state":"RUNNING"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agent-runs/github-run-1":
			_, _ = w.Write([]byte(`{"run":{"state":"SUCCEEDED"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	t.Setenv("GITHUB_SCOPE_SIGNING_KEY", "test-github-scope-signing-key-123456789")
	t.Setenv("NMC3_INTERNAL_TOKEN", "github-worker-test-token")
	cfg := platform.Config{Environment: "test", AllowLocalAuth: true, AgentExecutionMode: "dummy", JWTSecret: "test-secret-test-secret-test-secret", JWTIssuer: "http://127.0.0.1/test-auth", JWTAudience: "authenticated", StorageDir: t.TempDir(), AgentAPIURL: upstream.URL}
	server, _, err := Wire(ctx, cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	worker := &GitHubDeliveryWorker{Server: server, Client: &http.Client{Timeout: 5 * time.Second}}
	for i := 0; i < 2; i++ {
		if err := worker.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if starts != 1 || sent["agent_id"] != "github-repo-reviewer" || sent["project_id"] != projectID {
		t.Fatalf("starts=%d payload=%+v", starts, sent)
	}
	input, ok := sent["input"].(map[string]any)
	if !ok || input["repository"] != "demo/private" || input["branch"] != "main" {
		t.Fatalf("input=%+v", sent["input"])
	}
	claims, err := parseGitHubScope(input["scope"].(string))
	if err != nil || claims.BindingID != bindingID || claims.RepositoryID != repositoryID || claims.DeliveryID != deliveryID {
		t.Fatalf("scope=%+v err=%v", claims, err)
	}
	var state string
	if err := db.Pool.QueryRow(ctx, `SELECT state FROM app.github_rule_runs WHERE delivery_id=$1`, deliveryID).Scan(&state); err != nil || state != "succeeded" {
		t.Fatalf("state=%s err=%v", state, err)
	}
}
