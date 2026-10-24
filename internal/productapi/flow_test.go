package productapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/platform/testdb"
)

func TestEmployeeJourney(t *testing.T) {
	db := testdb.DB(t)
	cfg := platform.Config{
		Environment: "test", AllowLocalAuth: true, AgentExecutionMode: "dummy",
		JWTSecret: "test-secret-test-secret-test-secret", JWTIssuer: "http://127.0.0.1/test-auth",
		JWTAudience: "authenticated", StorageDir: t.TempDir(),
	}
	if err := organization.Seed(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	server, worker, err := Wire(context.Background(), cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(server.Handler())
	defer app.Close()
	ada := login(t, app.URL, "ada.hardware@nmc2.test")
	bao := login(t, app.URL, "bao.ai@nmc2.test")
	cam := login(t, app.URL, "cam.review@nmc2.test")

	status, _ := call(t, app.URL, ada, http.MethodGet, "/api/v1/partners", nil)
	if status != http.StatusNotFound {
		t.Fatalf("partner route status %d", status)
	}
	status, _ = call(t, app.URL, "", http.MethodGet, "/api/health", nil)
	if status != http.StatusOK {
		t.Fatalf("health %d", status)
	}
	status, _ = call(t, app.URL, "", http.MethodGet, "/v1/agents", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous agents %d", status)
	}

	var created struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	must(t, app.URL, ada, http.MethodPost, "/api/v1/requests", map[string]string{"title": "Review the billing change", "outcome": "A reviewed pull request"}, &created)
	var saved struct {
		Version int `json:"version"`
	}
	must(t, app.URL, ada, http.MethodPost, "/api/v1/requests/"+created.ID+"/save", map[string]any{"expected_version": created.Version, "title": "Review the billing change", "outcome": "Findings cite the diff"}, &saved)
	var submitted struct {
		Version int    `json:"version"`
		State   string `json:"state"`
	}
	must(t, app.URL, ada, http.MethodPost, "/api/v1/requests/"+created.ID+"/submit", map[string]any{"expected_version": saved.Version}, &submitted)
	var triaged struct {
		Version int `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/triage", map[string]any{"expected_version": submitted.Version}, &triaged)
	var clarified struct {
		Request struct {
			Version int `json:"version"`
		} `json:"request"`
		Clarification struct {
			ID string `json:"id"`
		} `json:"clarification"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/clarify", map[string]any{"expected_version": triaged.Version, "question": "Which repository is in scope?"}, &clarified)
	var answered struct {
		Version int `json:"version"`
	}
	must(t, app.URL, ada, http.MethodPost, "/api/v1/requests/"+created.ID+"/answer", map[string]any{"expected_version": clarified.Request.Version, "clarification_id": clarified.Clarification.ID, "answer": "The fictional billing repository."}, &answered)
	var triagedAgain struct {
		Version int `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/triage", map[string]any{"expected_version": answered.Version}, &triagedAgain)
	var accepted struct {
		Version int    `json:"version"`
		State   string `json:"state"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/accept", map[string]any{"expected_version": triagedAgain.Version}, &accepted)
	if accepted.State != "accepted" {
		t.Fatalf("state %s", accepted.State)
	}

	document := map[string]any{
		"identity": "pr-review", "purpose": "Review a pull request", "interface": map[string]string{"kind": "pull_request"},
		"instructions": "Cite only the diff.", "model_policy": map[string]string{"mode": "recorded"},
		"tools": []string{"diff.read"}, "context": map[string]string{"repository": "billing"},
		"resources": map[string]int{"gpu": 0}, "budget": map[string]int{"usd": 0},
		"guardrails": map[string]bool{"read_only": true}, "evaluations": map[string]string{"suite": "pr"},
		"operations": map[string]bool{"handover": true}, "output_action_types": []string{"comment"},
	}
	var spec struct {
		ID               string `json:"id"`
		Version          int    `json:"version"`
		MaterialRevision int    `json:"material_revision"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/specifications", map[string]any{"document": document}, &spec)
	var reviewing struct {
		Version int    `json:"version"`
		State   string `json:"state"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/specifications/"+spec.ID+"/submit-review", map[string]any{"expected_version": spec.Version}, &reviewing)
	status, _ = call(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/approvals", map[string]any{"reviewer_id": organization.BaoID})
	if status != http.StatusForbidden {
		t.Fatalf("self approval %d", status)
	}
	var approval struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/approvals", map[string]any{"reviewer_id": organization.CamID}, &approval)
	var decided struct {
		State   string `json:"state"`
		Version int    `json:"version"`
	}
	decisionBody := map[string]any{"decision": "approved", "expected_version": approval.Version, "comment": "Matches the request"}
	status, raw := callHeader(t, app.URL, cam, http.MethodPost, "/api/v1/approvals/"+approval.ID+"/decide", decisionBody, "idem-1")
	if status != http.StatusOK {
		t.Fatalf("decide %d %s", status, raw)
	}
	_ = json.Unmarshal(raw, &decided)
	if decided.State != "approved" {
		t.Fatalf("decision %s", decided.State)
	}
	status, replay := call(t, app.URL, cam, http.MethodPost, "/api/v1/approvals/"+approval.ID+"/decide", map[string]any{"decision": "approved", "expected_version": approval.Version + 1, "comment": "too late"})
	if status != http.StatusConflict {
		t.Fatalf("stale approval %d %s", status, replay)
	}
	status, body := callHeader(t, app.URL, cam, http.MethodPost, "/api/v1/approvals/"+approval.ID+"/decide", decisionBody, "idem-1")
	if status != http.StatusOK || !bytes.Contains(body, []byte("approved")) {
		t.Fatalf("idempotent replay %d %s", status, body)
	}
	status, _ = callHeader(t, app.URL, cam, http.MethodPost, "/api/v1/approvals/"+approval.ID+"/decide", map[string]any{"decision": "rejected", "expected_version": approval.Version, "comment": "different"}, "idem-1")
	if status != http.StatusConflict {
		t.Fatalf("idempotency mismatch %d", status)
	}

	var task struct {
		ID string `json:"id"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/tasks", map[string]string{"title": "Record the review"}, &task)
	must(t, app.URL, bao, http.MethodPost, "/api/v1/tasks/"+task.ID+"/complete", map[string]any{"expected_version": 1, "evidence": "notes"}, nil)
	var specStill struct {
		State string `json:"state"`
	}
	_ = db.Pool.QueryRow(context.Background(), `SELECT state FROM app.specifications WHERE id = $1`, spec.ID).Scan(&specStill.State)
	if specStill.State != "approved" {
		t.Fatalf("task completion changed the specification to %s", specStill.State)
	}

	var delivery struct {
		Version int `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/start-delivery", map[string]any{"expected_version": accepted.Version}, &delivery)
	var handover struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/handovers", nil, &handover)
	var sent struct {
		Version int `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/handovers/"+handover.ID+"/send", map[string]any{"expected_version": handover.Version}, &sent)
	var ready struct {
		Version int `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/requests/"+created.ID+"/ready", map[string]any{"expected_version": delivery.Version}, &ready)
	var acked struct {
		Version int    `json:"version"`
		State   string `json:"state"`
	}
	must(t, app.URL, ada, http.MethodPost, "/api/v1/handovers/"+handover.ID+"/acknowledge", map[string]any{"expected_version": sent.Version}, &acked)
	if acked.State != "acknowledged" {
		t.Fatalf("handover %s", acked.State)
	}
	must(t, app.URL, ada, http.MethodPost, "/api/v1/handovers/"+handover.ID+"/complete", map[string]any{"expected_version": acked.Version}, nil)
	var fulfilled struct {
		State string `json:"state"`
	}
	must(t, app.URL, ada, http.MethodPost, "/api/v1/requests/"+created.ID+"/fulfill", map[string]any{"expected_version": ready.Version}, &fulfilled)
	if fulfilled.State != "fulfilled" {
		t.Fatalf("fulfilled %s", fulfilled.State)
	}

	var catalog struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/specifications/"+spec.ID+"/catalog", nil, &catalog)
	var testing struct {
		Version int `json:"version"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/catalog/"+catalog.ID+"/test", map[string]any{"expected_version": catalog.Version}, &testing)
	status, _ = call(t, app.URL, cam, http.MethodPost, "/api/v1/catalog/"+catalog.ID+"/publish", map[string]any{"expected_version": testing.Version})
	if status != http.StatusConflict {
		t.Fatalf("publish without evaluation %d", status)
	}
	var eligible struct {
		Version int    `json:"version"`
		State   string `json:"state"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/catalog/"+catalog.ID+"/evaluations", map[string]any{"passed": true, "evidence": "The deterministic review completed."}, &eligible)
	var published struct {
		State   bool `json:"hardware_allocated"`
		Write   bool `json:"grants_write_tool"`
		Version int  `json:"version"`
	}
	must(t, app.URL, cam, http.MethodPost, "/api/v1/catalog/"+catalog.ID+"/publish", map[string]any{"expected_version": eligible.Version}, &published)
	if published.State || published.Write {
		t.Fatalf("publish allocated hardware or a write tool")
	}
	var budget struct {
		ID    string   `json:"id"`
		Usage *float64 `json:"usage_amount"`
	}
	must(t, app.URL, ada, http.MethodPost, "/api/v1/requests/"+created.ID+"/resources", map[string]any{"kind": "budget", "unit": "USD", "amount": 25}, &budget)
	if budget.Usage != nil {
		t.Fatalf("usage measured as %v", *budget.Usage)
	}
	var approvedBudget struct {
		Usage    *float64 `json:"usage_amount"`
		Approved *float64 `json:"approved_amount"`
	}
	must(t, app.URL, cam, http.MethodPost, "/api/v1/resources/"+budget.ID+"/approve", map[string]any{"amount": 20}, &approvedBudget)
	if approvedBudget.Usage != nil || approvedBudget.Approved == nil || *approvedBudget.Approved != 20 {
		t.Fatalf("budget record %#v", approvedBudget)
	}

	diff := "diff --git a/billing/invoice.go b/billing/invoice.go\n--- a/billing/invoice.go\n+++ b/billing/invoice.go\n@@ -1,4 +1,9 @@\n package billing\n \n func Charge(lines []Line) int {\n-    return 0\n+    total := 0\n+    for _, line := range lines {\n+        total += line.Amount\n+    }\n+    return total\n }\n"
	var started struct {
		RunID string `json:"run_id"`
	}
	must(t, app.URL, bao, http.MethodPost, "/v1/agent-runs", map[string]any{
		"agent_id": "pr-review-orchestrator", "agent_version": "1.0.0",
		"input":  map[string]string{"diff": diff},
		"limits": map[string]any{"max_delegations": 2, "max_concurrent_agents": 2, "max_tool_calls": 4, "max_model_turns": 8, "max_tokens": 12000, "max_cost_usd": 2, "timeout_seconds": 30},
	}, &started)
	if _, err := worker.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Run struct {
			State string `json:"state"`
			Usage struct {
				Input int `json:"input_tokens"`
			} `json:"usage"`
			Invocations []struct {
				AgentID string `json:"agent_id"`
				Depth   int    `json:"depth"`
			} `json:"invocations"`
		} `json:"run"`
		Mode string `json:"execution_mode"`
	}
	must(t, app.URL, bao, http.MethodGet, "/v1/agent-runs/"+started.RunID, nil, &snapshot)
	if snapshot.Run.State != "SUCCEEDED" || snapshot.Run.Usage.Input != 0 || snapshot.Mode != "dummy" {
		t.Fatalf("run %#v", snapshot)
	}
	var depth1 int
	for _, invocation := range snapshot.Run.Invocations {
		if invocation.Depth == 1 {
			depth1++
		}
	}
	if depth1 != 2 {
		t.Fatalf("invocations %#v", snapshot.Run.Invocations)
	}

	document["instructions"] = "Cite only the diff. Also check the new total."
	var revised struct {
		State            string `json:"state"`
		MaterialRevision int    `json:"material_revision"`
	}
	must(t, app.URL, bao, http.MethodPost, "/api/v1/specifications/"+spec.ID+"/revisions", map[string]any{"expected_version": reviewing.Version + 1, "document": document}, &revised)
	if revised.State != "draft" || revised.MaterialRevision == spec.MaterialRevision {
		t.Fatalf("material revision %#v", revised)
	}
	var superseded int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM app.scoped_approvals WHERE target_id = $1 AND state = 'superseded'`, spec.ID).Scan(&superseded); err != nil || superseded == 0 {
		t.Fatalf("approvals not superseded: %d %v", superseded, err)
	}

	status, _ = call(t, app.URL, cam, http.MethodPost, "/api/v1/members/"+organization.AdaID+"/remove", nil)
	if status != http.StatusOK {
		t.Fatalf("remove %d", status)
	}
	status, _ = call(t, app.URL, ada, http.MethodGet, "/api/v1/requests/"+created.ID, nil)
	if status != http.StatusForbidden {
		t.Fatalf("revoked read %d", status)
	}
	status, _ = call(t, app.URL, ada, http.MethodPost, "/v1/agent-runs", map[string]any{
		"agent_id": "analysis-specialist", "agent_version": "1.0.0", "input": map[string]string{"task": "summarize"},
		"limits": map[string]any{"max_delegations": 1, "max_concurrent_agents": 1, "max_tool_calls": 1, "max_model_turns": 2, "max_tokens": 1000, "max_cost_usd": 1, "timeout_seconds": 30},
	})
	if status != http.StatusForbidden {
		t.Fatalf("revoked run %d", status)
	}
}

func TestAbandonedWorkFailsAndCompletedHistoryRemains(t *testing.T) {
	db := testdb.DB(t)
	cfg := platform.Config{Environment: "test", AllowLocalAuth: true, AgentExecutionMode: "dummy", JWTSecret: "test-secret-test-secret-test-secret", JWTIssuer: "http://127.0.0.1/test-auth", JWTAudience: "authenticated"}
	if err := organization.Seed(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	_, worker, err := Wire(context.Background(), cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	doneID := "run_done_" + platform.UUID()
	lostID := "run_lost_" + platform.UUID()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO nmc3.runs (id, organization_id, actor_id, state, agent_id, agent_version, execution_mode, adapter_version, snapshot, created_at, updated_at)
		VALUES
		($1,$2,$3,'SUCCEEDED','analysis-specialist','1.0.0','dummy','dummy-1',$4, now(), now()),
		($5,$2,$3,'RUNNING','analysis-specialist','1.0.0','dummy','dummy-1',$6, now(), now())
	`, doneID, platform.OrgID, organization.BaoID, `{"run":{"id":"`+doneID+`","state":"SUCCEEDED"},"meta":{}}`, lostID, `{"run":{"id":"`+lostID+`","state":"RUNNING"},"meta":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO nmc3.jobs (run_id, state, lease_owner) VALUES ($1,'leased','old-worker')`, lostID); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Abandon(ctx); err != nil {
		t.Fatal(err)
	}
	var doneState, lostState, message string
	if err := db.Pool.QueryRow(ctx, `SELECT state FROM nmc3.runs WHERE id = $1`, doneID).Scan(&doneState); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT state, snapshot->'run'->'error'->>'message' FROM nmc3.runs WHERE id = $1`, lostID).Scan(&lostState, &message); err != nil {
		t.Fatal(err)
	}
	if doneState != "SUCCEEDED" || lostState != "FAILED" || message == "" {
		t.Fatalf("done=%s lost=%s message=%s", doneState, lostState, message)
	}
}

func login(t *testing.T, base, email string) string {
	t.Helper()
	status, body := call(t, base, "", http.MethodPost, "/api/v1/auth/local-session", map[string]string{"email": email, "password": organization.Password})
	if status != http.StatusOK {
		t.Fatalf("login %s %d %s", email, status, body)
	}
	var session struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(body, &session)
	return session.AccessToken
}

func must(t *testing.T, base, token, method, path string, payload any, dest any) {
	t.Helper()
	status, body := call(t, base, token, method, path, payload)
	if status >= 300 {
		t.Fatalf("%s %s -> %d %s", method, path, status, body)
	}
	if dest != nil && len(body) > 0 {
		if err := json.Unmarshal(body, dest); err != nil {
			t.Fatalf("decode %s: %v body %s", path, err, body)
		}
	}
}

func call(t *testing.T, base, token, method, path string, payload any) (int, []byte) {
	t.Helper()
	return callHeader(t, base, token, method, path, payload, "")
}

func callHeader(t *testing.T, base, token, method, path string, payload any, idem string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if payload != nil {
		encoded, _ := json.Marshal(payload)
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if idem != "" {
		request.Header.Set("Idempotency-Key", idem)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, body
}
