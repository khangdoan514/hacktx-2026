package productapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/platform/testdb"
)

func TestProjectAgentContextSharingAndRunHistory(t *testing.T) {
	db := testdb.DB(t)
	if err := organization.Seed(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	projectID := platform.UUID()
	_, err := db.Pool.Exec(context.Background(), `INSERT INTO app.delivery_projects (id, organization_id, name) VALUES ($1,$2,'Agent context test')`, projectID, platform.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{organization.BaoID, organization.CamID} {
		if _, err := db.Pool.Exec(context.Background(), `INSERT INTO app.delivery_grants (project_id, user_id) VALUES ($1,$2)`, projectID, user); err != nil {
			t.Fatal(err)
		}
	}
	var lastStart map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&lastStart); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"run_id":"run-1","state":"RUNNING"}`))
	}))
	defer upstream.Close()
	cfg := platform.Config{Environment: "test", AllowLocalAuth: true, AgentExecutionMode: "dummy", JWTSecret: "test-secret-test-secret-test-secret", JWTIssuer: "http://127.0.0.1/test-auth", JWTAudience: "authenticated", StorageDir: t.TempDir(), AgentAPIURL: upstream.URL}
	server, _, err := Wire(context.Background(), cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(server.Handler())
	defer app.Close()
	bao := login(t, app.URL, "bao.ai@nmc2.test")
	cam := login(t, app.URL, "cam.review@nmc2.test")
	path := "/api/v1/projects/" + projectID + "/agent-context"
	var private agentContextEntry
	must(t, app.URL, bao, http.MethodPost, path, map[string]string{"agent_id": "weekly-review", "kind": "memory", "title": "Private note", "body": "Bao only"}, &private)
	var listed struct {
		Items []agentContextEntry `json:"items"`
	}
	must(t, app.URL, cam, http.MethodGet, path, nil, &listed)
	if len(listed.Items) != 0 {
		t.Fatalf("private context leaked: %+v", listed.Items)
	}
	must(t, app.URL, bao, http.MethodPost, path, map[string]string{"agent_id": "weekly-review", "kind": "report", "title": "Last week", "body": "Shipped the API", "visibility": "project"}, nil)
	must(t, app.URL, cam, http.MethodGet, path, nil, &listed)
	if len(listed.Items) != 1 || listed.Items[0].Body != "Shipped the API" {
		t.Fatalf("shared context missing: %+v", listed.Items)
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/projects/"+projectID+"/agents", map[string]string{"agent_id": "weekly-review"}, nil)
	must(t, app.URL, cam, http.MethodGet, "/api/v1/projects/"+projectID+"/agents", nil, nil)
	start := map[string]any{"project_id": projectID, "agent_id": "weekly-review", "agent_version": "1.0.0", "input": map[string]any{"repository": "example"}, "limits": map[string]any{}, "shared_context": []any{map[string]any{"body": "injected"}}}
	status, _ := call(t, app.URL, bao, http.MethodPost, "/agent-api/agent-runs", start)
	if status != http.StatusAccepted {
		t.Fatalf("start status %d", status)
	}
	fragments, ok := lastStart["shared_context"].([]any)
	if !ok || len(fragments) != 2 {
		t.Fatalf("context not loaded: %#v", lastStart["shared_context"])
	}
	if lastStart["actor_id"] != organization.BaoID {
		t.Fatalf("actor not stamped: %#v", lastStart["actor_id"])
	}
	t.Setenv("NMC3_INTERNAL_TOKEN", "context-test-token")
	body, _ := json.Marshal(map[string]string{"project_id": projectID, "actor_id": organization.BaoID, "run_id": "run-1", "agent_id": "weekly-review", "body": `{"summary":"Progress this week"}`})
	req, _ := http.NewRequest(http.MethodPost, app.URL+"/api/internal/agent-context", bytes.NewReader(body))
	req.Header.Set(platform.InternalHeader, "context-test-token")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("save status %d", response.StatusCode)
	}
	must(t, app.URL, cam, http.MethodGet, path, nil, &listed)
	if len(listed.Items) != 2 || listed.Items[0].Kind != "run_output" {
		t.Fatalf("run history missing: %+v", listed.Items)
	}
	status, _ = call(t, app.URL, bao, http.MethodPost, "/agent-api/agent-runs", start)
	if status != http.StatusAccepted {
		t.Fatalf("next start status %d", status)
	}
	fragments, ok = lastStart["shared_context"].([]any)
	if !ok || len(fragments) != 3 {
		t.Fatalf("previous report not carried forward: %#v", lastStart["shared_context"])
	}
	status, _ = call(t, app.URL, cam, http.MethodDelete, path+"/"+private.ID, nil)
	if status != http.StatusNotFound {
		t.Fatalf("another member deleted private context: %d", status)
	}
	must(t, app.URL, bao, http.MethodDelete, path+"/"+private.ID, nil, nil)
	must(t, app.URL, bao, http.MethodGet, path, nil, &listed)
	if len(listed.Items) != 2 {
		t.Fatalf("deleted note remained visible: %+v", listed.Items)
	}
	if _, err := db.Pool.Exec(context.Background(), `UPDATE app.delivery_grants SET status='removed' WHERE project_id=$1 AND user_id=$2`, projectID, organization.CamID); err != nil {
		t.Fatal(err)
	}
	status, _ = call(t, app.URL, cam, http.MethodGet, path, nil)
	if status != http.StatusNotFound {
		t.Fatalf("revoked member status %d", status)
	}
}
