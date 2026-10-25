package automations

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestScheduleNext(t *testing.T) {
	cases := []struct{ expr, zone, from, want string }{
		{"*/15 * * * *", "", "2026-10-25T10:07:30Z", "2026-10-25T10:15:00Z"},
		{"0 9 * * mon-fri", "", "2026-10-24T12:00:00Z", "2026-10-26T09:00:00Z"}, // Saturday -> Monday
		{"30 8 1 * *", "", "2026-10-25T00:00:00Z", "2026-11-01T08:30:00Z"},
		{"@hourly", "", "2026-10-25T10:00:00Z", "2026-10-25T11:00:00Z"},
		{"0 0 * * 7", "", "2026-10-25T01:00:00Z", "2026-11-01T00:00:00Z"}, // 7 is Sunday
		{"0 9 * * *", "America/Chicago", "2026-10-25T00:00:00Z", "2026-10-25T14:00:00Z"},
		{"5/20 * * * *", "", "2026-10-25T10:06:00Z", "2026-10-25T10:25:00Z"},
		{"0 12 13 * fri", "", "2026-10-25T00:00:00Z", "2026-10-30T12:00:00Z"}, // either day field matches
	}
	for _, c := range cases {
		s, err := ParseSchedule(c.expr, c.zone)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got := s.Next(mustTime(t, c.from)); !got.Equal(mustTime(t, c.want)) {
			t.Errorf("%s from %s = %s, want %s", c.expr, c.from, got, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "a * * * *", "5-1 * * * *"} {
		if _, err := ParseSchedule(bad, ""); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if _, err := ParseSchedule("* * * * *", "Mars/Olympus"); err == nil {
		t.Error("unknown zone parsed")
	}
	s, _ := ParseSchedule("0 0 31 2 *", "")
	if !s.Next(time.Now()).IsZero() {
		t.Error("February 31 matched")
	}
}

type fakeCoordinator struct {
	mu     sync.Mutex
	starts []contracts.StartRunRequest
	owners []string
	fail   error
}

func (f *fakeCoordinator) Start(ctx context.Context, req contracts.StartRunRequest) (contracts.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return contracts.Run{}, f.fail
	}
	f.starts = append(f.starts, req)
	f.owners = append(f.owners, toolpacks.ContextOwner(ctx))
	return contracts.Run{ID: "run-" + string(rune('a'+len(f.starts)-1)), State: contracts.StateRunning, AgentID: req.AgentID}, nil
}

type fakeRuns map[string]contracts.Run

func (f fakeRuns) Create(context.Context, contracts.Run) error { return nil }
func (f fakeRuns) Update(context.Context, contracts.Run) error { return nil }
func (f fakeRuns) Get(_ context.Context, id string) (contracts.Run, error) {
	run, ok := f[id]
	if !ok {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	return run, nil
}

func request() contracts.StartRunRequest {
	return contracts.StartRunRequest{AgentID: "vendor-orchestrator", AgentVersion: "1.0.0", Input: json.RawMessage(`{"task":"weekly digest"}`),
		Limits: contracts.Limits{MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 1, MaxModelTurns: 1, MaxTokens: 100, MaxCostUSD: 1, TimeoutSeconds: 30}}
}

func TestTickFiresDueOnceAndAdvances(t *testing.T) {
	clock := mustTime(t, "2026-10-25T10:00:00Z")
	store := NewMemoryStore()
	coord := &fakeCoordinator{}
	svc := &Service{Store: store, Coordinator: coord, Runs: fakeRuns{"run-a": {ID: "run-a", State: contracts.StateSucceeded, AgentID: "vendor-orchestrator", Usage: contracts.Usage{InputTokens: 3}}}, Now: func() time.Time { return clock }}
	due := clock.Add(-2 * time.Hour) // the process was down; fire once, not eight times
	later := clock.Add(time.Hour)
	_ = store.Create(context.Background(), Automation{ID: "auto-1", TenantID: "org", CreatedBy: "u1", Name: "Digest", Schedule: "*/15 * * * *", Timezone: "UTC", Enabled: true, Request: request(), NextRunAt: &due})
	_ = store.Create(context.Background(), Automation{ID: "auto-2", TenantID: "org", CreatedBy: "u1", Name: "Later", Schedule: "0 * * * *", Enabled: true, Request: request(), NextRunAt: &later})
	_ = store.Create(context.Background(), Automation{ID: "auto-3", TenantID: "org", CreatedBy: "u1", Name: "Off", Schedule: "* * * * *", Enabled: false, Request: request(), NextRunAt: &due})

	svc.Tick(context.Background())
	svc.Tick(context.Background())
	if len(coord.starts) != 1 {
		t.Fatalf("starts = %d, want 1", len(coord.starts))
	}
	if coord.owners[0] != "org:u1" {
		t.Fatalf("owner = %q", coord.owners[0])
	}
	a, _ := store.Get(context.Background(), "org", "auto-1")
	if want := mustTime(t, "2026-10-25T10:15:00Z"); a.NextRunAt == nil || !a.NextRunAt.Equal(want) {
		t.Fatalf("next = %v, want %s", a.NextRunAt, want)
	}
	firings, err := svc.Firings(context.Background(), "org", "", 10)
	if err != nil || len(firings) != 1 {
		t.Fatalf("firings = %v, %v", firings, err)
	}
	f := firings[0]
	if f.RunID != "run-a" || f.State != contracts.StateSucceeded || f.Trigger != TriggerSchedule || !f.ScheduledFor.Equal(due) || f.Name != "Digest" || f.Usage.InputTokens != 3 {
		t.Fatalf("firing = %+v", f)
	}
}

func TestFireRecordsRefusal(t *testing.T) {
	store := NewMemoryStore()
	coord := &fakeCoordinator{fail: &contracts.RunError{Code: contracts.ErrNotFound, Message: "agent version not found"}}
	svc := &Service{Store: store, Coordinator: coord}
	f := svc.Fire(context.Background(), Automation{ID: "auto-1", TenantID: "org", Request: request()}, TriggerManual, time.Now())
	if f.RunID != "" || f.Error != "agent version not found" {
		t.Fatalf("firing = %+v", f)
	}
}

func call(t *testing.T, h http.Handler, method, path, tenant string, body any) (int, map[string]json.RawMessage) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-NMC3-Tenant", tenant)
	req.Header.Set("X-NMC3-User", "u1")
	req.Header.Set("X-NMC3-Email", "bao.ai@nmc2.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]json.RawMessage{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestHTTPLifecycle(t *testing.T) {
	clock := mustTime(t, "2026-10-25T10:00:00Z")
	coord := &fakeCoordinator{}
	svc := &Service{Store: NewMemoryStore(), Coordinator: coord, Runs: fakeRuns{}, Now: func() time.Time { return clock }}
	mux := http.NewServeMux()
	(&API{Service: svc, Check: func(_ context.Context, req contracts.StartRunRequest) error {
		if req.AgentID != "vendor-orchestrator" {
			return &contracts.RunError{Code: contracts.ErrNotFound, Message: "agent version not found"}
		}
		return nil
	}}).Mount(mux)

	bad := request()
	bad.AgentID = "missing"
	if code, _ := call(t, mux, http.MethodPost, "/v1/automations", "org", map[string]any{"name": "x", "schedule": "* * * * *", "request": bad}); code != http.StatusBadRequest {
		t.Fatalf("unknown agent = %d", code)
	}
	if code, _ := call(t, mux, http.MethodPost, "/v1/automations", "org", map[string]any{"name": "x", "schedule": "nope", "request": request()}); code != http.StatusBadRequest {
		t.Fatalf("bad schedule = %d", code)
	}
	code, out := call(t, mux, http.MethodPost, "/v1/automations", "org", map[string]any{"name": "Digest", "schedule": "0 9 * * *", "timezone": "America/Chicago", "request": request()})
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, out["error"])
	}
	var created Automation
	_ = json.Unmarshal(out["automation"], &created)
	if created.NextRunAt == nil || !created.NextRunAt.Equal(mustTime(t, "2026-10-25T14:00:00Z")) || created.CreatedByEmail != "bao.ai@nmc2.test" {
		t.Fatalf("created = %+v", created)
	}
	if code, _ := call(t, mux, http.MethodGet, "/v1/automations/"+created.ID, "other-org", nil); code != http.StatusNotFound {
		t.Fatalf("other tenant read = %d", code)
	}
	code, out = call(t, mux, http.MethodPost, "/v1/automations/"+created.ID+"/run", "org", nil)
	if code != http.StatusAccepted || len(coord.starts) != 1 {
		t.Fatalf("run now = %d, starts %d", code, len(coord.starts))
	}
	code, out = call(t, mux, http.MethodPatch, "/v1/automations/"+created.ID, "org", map[string]any{"enabled": false})
	var paused Automation
	_ = json.Unmarshal(out["automation"], &paused)
	if code != http.StatusOK || paused.Enabled || paused.NextRunAt != nil {
		t.Fatalf("pause = %d %+v", code, paused)
	}
	code, out = call(t, mux, http.MethodGet, "/v1/automations", "org", nil)
	var list []Automation
	_ = json.Unmarshal(out["automations"], &list)
	if code != http.StatusOK || len(list) != 1 || list[0].LastFiring == nil || list[0].LastFiring.Trigger != TriggerManual {
		t.Fatalf("list = %d %+v", code, list)
	}
	code, out = call(t, mux, http.MethodPost, "/v1/automations/preview", "org", map[string]any{"schedule": "@daily"})
	var next []time.Time
	_ = json.Unmarshal(out["next"], &next)
	if code != http.StatusOK || len(next) != 5 || !next[0].Equal(mustTime(t, "2026-10-26T00:00:00Z")) {
		t.Fatalf("preview = %d %v", code, next)
	}
	code, out = call(t, mux, http.MethodGet, "/v1/automation-firings", "org", nil)
	var firings []Firing
	_ = json.Unmarshal(out["firings"], &firings)
	if code != http.StatusOK || len(firings) != 1 || firings[0].Name != "Digest" {
		t.Fatalf("firings = %d %+v", code, firings)
	}
	if code, _ := call(t, mux, http.MethodDelete, "/v1/automations/"+created.ID, "org", nil); code != http.StatusOK {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := call(t, mux, http.MethodGet, "/v1/automations/"+created.ID, "org", nil); code != http.StatusNotFound {
		t.Fatalf("after delete = %d", code)
	}
}
