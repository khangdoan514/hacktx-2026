package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

type fakeCoordinator struct{ run contracts.Run }

func (f fakeCoordinator) Start(_ context.Context, _ contracts.StartRunRequest) (contracts.Run, error) {
	return f.run, nil
}

type fakeStore struct{ run contracts.Run }

func (f fakeStore) Create(context.Context, contracts.Run) error { return nil }
func (f fakeStore) Update(context.Context, contracts.Run) error { return nil }
func (f fakeStore) Get(_ context.Context, id string) (contracts.Run, error) {
	if id != f.run.ID {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	return f.run, nil
}

type fakeEvents struct{}

func (fakeEvents) Append(_ context.Context, e contracts.RunEvent) (contracts.RunEvent, error) {
	return e, nil
}
func (fakeEvents) List(_ context.Context, _ string, after uint64) ([]contracts.RunEvent, error) {
	if after > 0 {
		return nil, nil
	}
	return []contracts.RunEvent{{Sequence: 1, Type: "RUN_STARTED"}}, nil
}

func testHandler() http.Handler {
	run := contracts.Run{ID: "run-1", State: contracts.StatePending, AgentID: "a", Version: "1.0.0", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return New(fakeCoordinator{run}, fakeStore{run}, fakeEvents{}, []contracts.AgentDefinition{{ID: "a", Version: "1.0.0", Name: "A"}})
}
func request(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, bytes.NewReader(b)))
	return rec
}
func validRequest() contracts.StartRunRequest {
	return contracts.StartRunRequest{AgentID: "a", AgentVersion: "1.0.0", Input: json.RawMessage(`{"task":"x"}`), Limits: contracts.Limits{MaxDelegations: 2, MaxConcurrentAgents: 2, MaxToolCalls: 2, MaxModelTurns: 4, MaxTokens: 100, MaxCostUSD: 1, TimeoutSeconds: 10}}
}

type listingStore struct {
	fakeStore
	runs []contracts.Run
}

func (s listingStore) ListRuns(context.Context, int) ([]contracts.Run, error) {
	return s.runs, nil
}

func TestListRuns(t *testing.T) {
	run := contracts.Run{ID: "run-1", State: contracts.StateSucceeded, AgentID: "a", Version: "1.0.0", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	h := New(fakeCoordinator{run}, listingStore{fakeStore: fakeStore{run: run}, runs: []contracts.Run{run}}, fakeEvents{}, []contracts.AgentDefinition{{ID: "a", Version: "1.0.0", Name: "A"}})
	got := request(t, h, "GET", "/v1/agent-runs", nil)
	if got.Code != 200 || !bytes.Contains(got.Body.Bytes(), []byte(`"id":"run-1"`)) {
		t.Fatalf("list %d %s", got.Code, got.Body.String())
	}
	plain := testHandler()
	if got := request(t, plain, "GET", "/v1/agent-runs", nil); got.Code != 200 || !bytes.Contains(got.Body.Bytes(), []byte(`"runs":[]`)) {
		t.Fatalf("empty list %d %s", got.Code, got.Body.String())
	}
}

func TestVersionedAPI(t *testing.T) {
	h := testHandler()
	if got := request(t, h, "POST", "/v1/agent-runs", validRequest()); got.Code != 202 {
		t.Fatalf("POST %d %s", got.Code, got.Body.String())
	}
	if got := request(t, h, "GET", "/v1/agent-runs/run-1", nil); got.Code != 200 {
		t.Fatalf("GET %d", got.Code)
	}
	if got := request(t, h, "GET", "/v1/agent-runs/run-1/events?after=1", nil); got.Code != 200 || !bytes.Contains(got.Body.Bytes(), []byte(`"events":[]`)) {
		t.Fatalf("events %d %s", got.Code, got.Body.String())
	}
	if got := request(t, h, "GET", "/v1/agents", nil); got.Code != 200 {
		t.Fatalf("agents %d", got.Code)
	}
}
func TestInvalidRequests(t *testing.T) {
	h := testHandler()
	req := validRequest()
	req.Limits.MaxTokens = 0
	cases := []struct {
		method, path string
		body         any
		status       int
	}{
		{"POST", "/v1/agent-runs", req, 400},
		{"POST", "/v1/agent-runs", map[string]any{"agent_id": "unknown", "agent_version": "1.0.0", "input": map[string]any{"x": 1}, "limits": validRequest().Limits}, 404},
		{"GET", "/v1/agent-runs/missing", nil, 404},
		{"GET", "/v1/agent-runs/run-1/events?after=-1", nil, 400},
		{"GET", "/v1/agent-runs/run-1/events?after=1&after=2", nil, 400},
	}
	badModel := validRequest()
	badModel.ModelID = "bad model"
	if got := request(t, h, "POST", "/v1/agent-runs", badModel); got.Code != 400 {
		t.Errorf("bad model_id: %d %s", got.Code, got.Body.String())
	}
	okModel := validRequest()
	okModel.ModelID = "meta-llama/llama-3.3-70b-instruct:free"
	if got := request(t, h, "POST", "/v1/agent-runs", okModel); got.Code != 202 {
		t.Errorf("valid model_id: %d %s", got.Code, got.Body.String())
	}
	for _, tc := range cases {
		got := request(t, h, tc.method, tc.path, tc.body)
		if got.Code != tc.status {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, got.Code, got.Body.String())
		}
		var response contracts.ErrorResponse
		if err := json.Unmarshal(got.Body.Bytes(), &response); err != nil || response.Error.Code == "" {
			t.Errorf("missing typed error: %s", got.Body.String())
		}
	}
}

func TestStartFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "api", "v1", "start-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request contracts.StartRunRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	request.AgentID = "a"
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/agent-runs", bytes.NewReader(mustJSON(request))))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("fixture request: %d %s", rec.Code, rec.Body.String())
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
