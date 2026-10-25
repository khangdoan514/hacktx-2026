package productapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/platform/testdb"
)

func TestAutomationWorkerStartsDueRunWithProjectContext(t *testing.T) {
	db := testdb.DB(t)
	if err := organization.Seed(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	projectID := platform.UUID()
	agentID := "weekly-review-" + platform.UUID()
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO app.delivery_projects (id,organization_id,name) VALUES ($1,$2,'Automation test')`, projectID, platform.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO app.delivery_grants (project_id,user_id) VALUES ($1,$2)`, projectID, organization.BaoID); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	starts := 0
	var sent map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-runs":
			mu.Lock()
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload["agent_id"] == agentID {
				starts++
				sent = payload
			}
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"run_id":"scheduled-run-1","state":"RUNNING"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agent-runs/scheduled-run-1":
			_, _ = w.Write([]byte(`{"run":{"state":"SUCCEEDED"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	t.Setenv("NMC3_INTERNAL_TOKEN", "automation-test-token")
	cfg := platform.Config{Environment: "test", AllowLocalAuth: true, AgentExecutionMode: "dummy", JWTSecret: "test-secret-test-secret-test-secret", JWTIssuer: "http://127.0.0.1/test-auth", JWTAudience: "authenticated", StorageDir: t.TempDir(), AgentAPIURL: upstream.URL}
	server, _, err := Wire(context.Background(), cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(server.Handler())
	defer app.Close()
	bao := login(t, app.URL, "bao.ai@nmc2.test")
	must(t, app.URL, bao, http.MethodPost, "/api/v1/projects/"+projectID+"/agent-context", map[string]string{"agent_id": agentID, "kind": "report", "title": "Last week", "body": "Shipped API", "visibility": "project"}, nil)
	var created struct {
		ID string `json:"id"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/agent-automations", map[string]any{
		"name": "Weekly review", "project_id": projectID, "agent_id": agentID, "agent_version": "1.0.0",
		"input":    map[string]string{"task": "Summarize progress"},
		"limits":   map[string]any{"max_delegations": 1, "max_concurrent_agents": 1, "max_tool_calls": 4, "max_model_turns": 4, "max_tokens": 1000, "max_cost_usd": 1, "timeout_seconds": 30},
		"schedule": map[string]any{"kind": "once", "run_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339)},
	}, &created)
	if created.ID == "" {
		t.Fatal("missing automation id")
	}
	if err := db.Admin(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app.agent_automations SET next_run_at=now()-interval '1 second' WHERE id=$1`, created.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	worker := &AutomationWorker{Server: server, Client: &http.Client{Timeout: 5 * time.Second}}
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var runs struct {
		Items []automationOccurrence `json:"items"`
	}
	must(t, app.URL, bao, http.MethodGet, "/api/v1/agent-automations/"+created.ID+"/runs", nil, &runs)
	if len(runs.Items) != 1 || runs.Items[0].State != "succeeded" || runs.Items[0].AgentRunID == nil || *runs.Items[0].AgentRunID != "scheduled-run-1" {
		t.Fatalf("runs=%+v", runs.Items)
	}
	var listed struct {
		Items []agentAutomation `json:"items"`
	}
	must(t, app.URL, bao, http.MethodGet, "/api/v1/agent-automations", nil, &listed)
	var scheduled *agentAutomation
	for i := range listed.Items {
		if listed.Items[i].ID == created.ID {
			scheduled = &listed.Items[i]
			break
		}
	}
	if scheduled == nil || scheduled.Status != "completed" || scheduled.NextRunAt != nil {
		t.Fatalf("one-time schedule remained active: %+v", scheduled)
	}
	mu.Lock()
	defer mu.Unlock()
	if starts != 1 || sent["actor_id"] != organization.BaoID || sent["project_id"] != projectID {
		t.Fatalf("starts=%d payload=%+v", starts, sent)
	}
	fragments, ok := sent["shared_context"].([]any)
	if !ok || len(fragments) != 1 {
		t.Fatalf("project context=%#v", sent["shared_context"])
	}
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("one-time schedule started %d times", starts)
	}
}

func TestAutomationPauseResumeAndRunNow(t *testing.T) {
	db := testdb.DB(t)
	if err := organization.Seed(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	cfg := platform.Config{Environment: "test", AllowLocalAuth: true, AgentExecutionMode: "dummy", JWTSecret: "test-secret-test-secret-test-secret", JWTIssuer: "http://127.0.0.1/test-auth", JWTAudience: "authenticated", StorageDir: t.TempDir()}
	server, _, err := Wire(context.Background(), cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(server.Handler())
	defer app.Close()
	bao := login(t, app.URL, "bao.ai@nmc2.test")
	var created struct {
		ID string `json:"id"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/agent-automations", map[string]any{
		"name": "Interval review", "agent_id": "weekly-review", "agent_version": "1.0.0", "input": map[string]string{"task": "Review"},
		"limits":   map[string]any{"max_delegations": 1, "max_concurrent_agents": 1, "max_tool_calls": 4, "max_model_turns": 4, "max_tokens": 1000, "max_cost_usd": 1, "timeout_seconds": 30},
		"schedule": map[string]any{"kind": "interval", "interval_seconds": 60},
	}, &created)
	must(t, app.URL, bao, http.MethodPost, "/api/v1/agent-automations/"+created.ID+"/run-now", nil, nil)
	must(t, app.URL, bao, http.MethodPatch, "/api/v1/agent-automations/"+created.ID, map[string]string{"status": "paused"}, nil)
	var runs struct {
		Items []automationOccurrence `json:"items"`
	}
	must(t, app.URL, bao, http.MethodGet, "/api/v1/agent-automations/"+created.ID+"/runs", nil, &runs)
	if len(runs.Items) != 1 || runs.Items[0].State != "cancelled" {
		t.Fatalf("paused runs=%+v", runs.Items)
	}
	must(t, app.URL, bao, http.MethodPatch, "/api/v1/agent-automations/"+created.ID, map[string]string{"status": "active"}, nil)
	var listed struct {
		Items []agentAutomation `json:"items"`
	}
	must(t, app.URL, bao, http.MethodGet, "/api/v1/agent-automations", nil, &listed)
	var found *agentAutomation
	for i := range listed.Items {
		if listed.Items[i].ID == created.ID {
			found = &listed.Items[i]
			break
		}
	}
	if found == nil || found.Status != "active" || found.NextRunAt == nil || !found.NextRunAt.After(time.Now()) {
		t.Fatalf("resumed schedule=%+v", found)
	}
}
