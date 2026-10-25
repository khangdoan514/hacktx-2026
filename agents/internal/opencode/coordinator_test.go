package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

type fakeBackend struct {
	mu          sync.Mutex
	auth        []string
	requests    []string
	model       map[string]string
	agent       string
	messages    []Message
	outcome     string
	tokens      struct{ in, out int }
	cost        float64
	holdWait    bool
	release     chan struct{}
	interrupted bool
}

func (f *fakeBackend) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		f.mu.Lock()
		f.auth = append(f.auth, user+":"+pass+":"+func() string {
			if ok {
				return "ok"
			}
			return "no"
		}())
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			if model, ok := body["model"].(map[string]any); ok {
				f.model = map[string]string{"providerID": stringOf(model["providerID"]), "id": stringOf(model["id"])}
			}
			f.agent = stringOf(body["agent"])
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"id":"ses-test","outcome":"","cost":0,"tokens":{"input":0,"output":0,"reasoning":0}}}`))
		case strings.HasSuffix(r.URL.Path, "/prompt"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"msg-1"}}`))
		case strings.HasSuffix(r.URL.Path, "/interrupt"):
			f.mu.Lock()
			if !f.interrupted {
				f.interrupted = true
				if f.release != nil {
					close(f.release)
				}
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"interrupted":true}`))
		case strings.HasSuffix(r.URL.Path, "/wait"):
			f.mu.Lock()
			hold, rel := f.holdWait, f.release
			f.mu.Unlock()
			if hold && rel != nil {
				select {
				case <-rel:
				case <-r.Context().Done():
				}
			}
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/message"):
			f.mu.Lock()
			msgs := f.messages
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"data": msgs})
		case strings.HasPrefix(r.URL.Path, "/api/session/"):
			f.mu.Lock()
			outcome, tokens, cost := f.outcome, f.tokens, f.cost
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"id": "ses-test", "outcome": outcome, "cost": cost,
				"tokens": map[string]int{"input": tokens.in, "output": tokens.out, "reasoning": 0},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

type memory struct {
	mu     sync.Mutex
	runs   map[string]contracts.Run
	events map[string][]contracts.RunEvent
}

func newMemory() *memory {
	return &memory{runs: map[string]contracts.Run{}, events: map[string][]contracts.RunEvent{}}
}

func (m *memory) Create(_ context.Context, r contracts.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.ID] = r
	return nil
}
func (m *memory) Update(_ context.Context, r contracts.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.ID] = r
	return nil
}
func (m *memory) Get(_ context.Context, id string) (contracts.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[id], nil
}
func (m *memory) Append(_ context.Context, e contracts.RunEvent) (contracts.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.Sequence = uint64(len(m.events[e.RunID]) + 1)
	m.events[e.RunID] = append(m.events[e.RunID], e)
	return e, nil
}
func (m *memory) List(_ context.Context, id string, after uint64) ([]contracts.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []contracts.RunEvent
	for _, e := range m.events[id] {
		if e.Sequence > after {
			out = append(out, e)
		}
	}
	return out, nil
}

func testDefs() []contracts.AgentDefinition {
	return []contracts.AgentDefinition{{
		ID: "root", Version: "1.0.0", Name: "root", Role: "orchestrator",
		InputSchema:      json.RawMessage(`{"type":"object"}`),
		OutputSchema:     json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}},"additionalProperties":false}`),
		AllowedDelegates: []string{"one"},
		Limits:           contracts.Limits{MaxDelegations: 2, MaxConcurrentAgents: 2, MaxToolCalls: 2, MaxModelTurns: 4, MaxTokens: 100, MaxCostUSD: 1, TimeoutSeconds: 5},
	}, {
		ID: "one", Version: "1.0.0", Name: "one", InputSchema: json.RawMessage(`{"type":"object"}`),
		Limits: contracts.Limits{MaxDelegations: 2, MaxConcurrentAgents: 2, MaxToolCalls: 2, MaxModelTurns: 4, MaxTokens: 100, MaxCostUSD: 1, TimeoutSeconds: 5},
	}}
}

func assistantMessage(parts ...map[string]any) Message {
	content := make([]struct {
		Type  string         `json:"type"`
		Text  string         `json:"text"`
		Name  string         `json:"name"`
		ID    string         `json:"id"`
		State map[string]any `json:"state"`
	}, len(parts))
	for i, p := range parts {
		content[i].Type, _ = p["type"].(string)
		content[i].Text, _ = p["text"].(string)
		content[i].Name, _ = p["name"].(string)
		if state, ok := p["state"].(map[string]any); ok {
			content[i].State = state
		}
	}
	return Message{ID: "msg-1", Type: "assistant", Agent: "build", Content: content}
}

func waitRun(t *testing.T, m *memory, id string) contracts.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := m.Get(context.Background(), id)
		if r.State == contracts.StateSucceeded || r.State == contracts.StateFailed {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not finish")
	return contracts.Run{}
}

func openLimits() contracts.Limits {
	return contracts.Limits{MaxDelegations: 2, MaxConcurrentAgents: 2, MaxToolCalls: 2, MaxModelTurns: 4, MaxTokens: 10000, MaxCostUSD: 10, TimeoutSeconds: 5}
}

func start(t *testing.T, c *Coordinator, model string) (contracts.Run, *memory) {
	t.Helper()
	return startLimits(t, c, model, openLimits()), nil
}

func startLimits(t *testing.T, c *Coordinator, model string, limits contracts.Limits) contracts.Run {
	t.Helper()
	r, err := c.Start(context.Background(), contracts.StartRunRequest{
		AgentID: "root", AgentVersion: "1.0.0", ModelID: model,
		Input:  json.RawMessage(`{}`),
		Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSuccessfulRunMapsSession(t *testing.T) {
	backend := &fakeBackend{outcome: "succeeded", cost: 0.001}
	backend.tokens.in, backend.tokens.out = 100, 20
	backend.messages = []Message{assistantMessage(
		map[string]any{"type": "tool", "name": "subagent", "state": map[string]any{"status": "completed", "input": map[string]any{"agent": "one"}}},
		map[string]any{"type": "text", "text": `{"ok":true}`},
	)}
	server := httptest.NewServer(backend.handler())
	defer server.Close()
	client, err := NewClient(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	mem := newMemory()
	c, err := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	if err != nil {
		t.Fatal(err)
	}
	started, _ := start(t, c, "openai/gpt-4o-mini")
	if started.OpenCodeSessionID != "ses-test" || started.ModelID != "openai/gpt-4o-mini" {
		t.Fatalf("run=%+v", started)
	}
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateSucceeded || string(run.Output) != `{"ok":true}` {
		t.Fatalf("run=%+v", run)
	}
	if run.Usage.InputTokens != 100 || run.Usage.OutputTokens != 20 || run.Usage.CostUSD != 0.001 {
		t.Fatalf("usage=%+v", run.Usage)
	}
	if len(run.Invocations) != 2 || run.Invocations[1].Depth != 1 || run.Invocations[1].AgentID != "one" {
		t.Fatalf("invocations=%+v", run.Invocations)
	}
	events, _ := mem.List(context.Background(), run.ID, 0)
	types := map[string]bool{}
	for i, e := range events {
		if e.Sequence != uint64(i+1) || e.Source != "opencode" {
			t.Fatalf("event order/source: %+v", e)
		}
		types[e.Type] = true
	}
	for _, want := range []string{"RUN_STARTED", "AGENT_STARTED", "MODEL_STARTED", "TOOL_STARTED", "TOOL_SUCCEEDED", "MODEL_SUCCEEDED", "AGENT_SUCCEEDED", "RUN_SUCCEEDED"} {
		if !types[want] {
			t.Fatalf("missing %s in %v", want, types)
		}
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.model["providerID"] != "openai" || backend.model["id"] != "gpt-4o-mini" {
		t.Fatalf("model mapping=%v", backend.model)
	}
	if backend.agent != "root" {
		t.Fatalf("agent=%s", backend.agent)
	}
	if len(backend.auth) == 0 || backend.auth[0] != "opencode:secret:ok" {
		t.Fatalf("auth=%v", backend.auth)
	}
}

func TestInvalidOutputFailsRun(t *testing.T) {
	backend := &fakeBackend{outcome: "succeeded"}
	backend.messages = []Message{assistantMessage(map[string]any{"type": "text", "text": `not json at all`})}
	server := httptest.NewServer(backend.handler())
	defer server.Close()
	client, _ := NewClient(server.URL, "secret", server.Client())
	mem := newMemory()
	c, _ := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	started, _ := start(t, c, "")
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateFailed || run.Error.Code != contracts.ErrOutputSchemaInvalid {
		t.Fatalf("run=%+v", run)
	}
}

func TestStartValidation(t *testing.T) {
	server := httptest.NewServer((&fakeBackend{}).handler())
	defer server.Close()
	client, _ := NewClient(server.URL, "secret", server.Client())
	mem := newMemory()
	c, _ := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	for _, tc := range []struct {
		name string
		req  contracts.StartRunRequest
		code contracts.ErrorCode
	}{
		{"unknown agent", contracts.StartRunRequest{AgentID: "missing", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: contracts.Limits{MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 1, MaxModelTurns: 1, MaxTokens: 1, MaxCostUSD: 1, TimeoutSeconds: 1}}, contracts.ErrNotFound},
		{"bad model", contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", ModelID: "bad model", Input: json.RawMessage(`{}`), Limits: contracts.Limits{MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 1, MaxModelTurns: 1, MaxTokens: 1, MaxCostUSD: 1, TimeoutSeconds: 1}}, contracts.ErrInvalidInput},
		{"no provider", contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", ModelID: "justamodel", Input: json.RawMessage(`{}`), Limits: contracts.Limits{MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 1, MaxModelTurns: 1, MaxTokens: 1, MaxCostUSD: 1, TimeoutSeconds: 1}}, contracts.ErrInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Start(context.Background(), tc.req)
			runErr, ok := err.(*contracts.RunError)
			if !ok || runErr.Code != tc.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestDeniedDelegationDoesNotCreateChild(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state map[string]any
		code  contracts.ErrorCode
	}{
		{"outside allowlist", map[string]any{"status": "completed", "input": map[string]any{"agent": "general"}}, contracts.ErrAgentNotAllowed},
		{"depth rejected", map[string]any{"status": "error", "error": "denied", "input": map[string]any{"agent": "one"}}, contracts.ErrDelegationDepthExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeBackend{outcome: "succeeded"}
			backend.messages = []Message{assistantMessage(
				map[string]any{"type": "tool", "name": "subagent", "state": tc.state},
				map[string]any{"type": "text", "text": `{"ok":true}`},
			)}
			server := httptest.NewServer(backend.handler())
			defer server.Close()
			client, _ := NewClient(server.URL, "secret", server.Client())
			mem := newMemory()
			c, _ := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
			started, _ := start(t, c, "")
			run := waitRun(t, mem, started.ID)
			if run.State != contracts.StateSucceeded || len(run.Invocations) != 1 {
				t.Fatalf("run=%+v", run)
			}
			if len(run.ToolInvocations) != 1 || run.ToolInvocations[0].Error == nil || run.ToolInvocations[0].Error.Code != tc.code {
				t.Fatalf("tools=%+v", run.ToolInvocations)
			}
			events, _ := mem.List(context.Background(), run.ID, 0)
			if !eventCode(t, events, "TOOL_FAILED", string(tc.code)) {
				t.Fatalf("events=%+v", events)
			}
		})
	}
}

func TestDisallowedToolIsDenied(t *testing.T) {
	backend := &fakeBackend{outcome: "succeeded"}
	backend.messages = []Message{assistantMessage(
		map[string]any{"type": "tool", "name": "shell", "state": map[string]any{"status": "completed"}},
		map[string]any{"type": "text", "text": `{"ok":true}`},
	)}
	server := httptest.NewServer(backend.handler())
	defer server.Close()
	client, _ := NewClient(server.URL, "secret", server.Client())
	mem := newMemory()
	c, _ := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	started, _ := start(t, c, "")
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateSucceeded || len(run.Invocations) != 1 {
		t.Fatalf("run=%+v", run)
	}
	if len(run.ToolInvocations) != 1 || run.ToolInvocations[0].Error == nil || run.ToolInvocations[0].Error.Code != contracts.ErrToolNotAllowed {
		t.Fatalf("tools=%+v", run.ToolInvocations)
	}
	events, _ := mem.List(context.Background(), run.ID, 0)
	if !eventCode(t, events, "TOOL_FAILED", string(contracts.ErrToolNotAllowed)) {
		t.Fatalf("events=%+v", events)
	}
	for _, e := range events {
		if e.Type == "TOOL_SUCCEEDED" {
			t.Fatal("disallowed tool was recorded as success")
		}
	}
}

func TestBuildPromptNamesCatalogFile(t *testing.T) {
	got := buildPrompt(contracts.AgentDefinition{
		Name: "Catalog specialist", Role: "specialist",
		Instructions: "Use only authorized actions and cite fictional source IDs.",
		AllowedTools: []string{"catalog.search"},
		OutputSchema: json.RawMessage(`{}`),
	}, map[string]contracts.AgentDefinition{}, json.RawMessage(`{}`))
	if !strings.Contains(got, "catalog_search") || strings.Contains(got, "fixtures/catalog/vendors.json") || !strings.Contains(got, "Use only authorized actions and cite fictional source IDs.") {
		t.Fatal(got)
	}
}

func TestCatalogSearchMapsToContractTool(t *testing.T) {
	backend := &fakeBackend{outcome: "succeeded"}
	backend.messages = []Message{assistantMessage(
		map[string]any{"type": "tool", "name": "catalog_search", "state": map[string]any{"status": "completed"}},
		map[string]any{"type": "text", "text": `{"ok":true}`},
	)}
	server := httptest.NewServer(backend.handler())
	defer server.Close()
	client, _ := NewClient(server.URL, "secret", server.Client())
	mem := newMemory()
	defs := testDefs()
	defs[0].AllowedTools = []string{"catalog.search"}
	c, _ := New(defs, client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	started, _ := start(t, c, "")
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateSucceeded || len(run.ToolInvocations) != 1 {
		t.Fatalf("run=%+v", run)
	}
	if run.ToolInvocations[0].Error != nil || run.ToolInvocations[0].ToolID != "catalog.search" {
		t.Fatalf("tools=%+v", run.ToolInvocations)
	}
}

func eventCode(t *testing.T, events []contracts.RunEvent, typ, code string) bool {
	t.Helper()
	for _, e := range events {
		if e.Type != typ {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(e.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data["code"] == code {
			return true
		}
	}
	return false
}

func TestNewestFirstMessageOrder(t *testing.T) {
	backend := &fakeBackend{outcome: "succeeded"}
	// The API returns newest-first; the final JSON must win over older chatter.
	backend.messages = []Message{
		assistantMessage(map[string]any{"type": "text", "text": `{"ok":true}`}),
		assistantMessage(map[string]any{"type": "text", "text": `working on it`}),
	}
	server := httptest.NewServer(backend.handler())
	defer server.Close()
	client, _ := NewClient(server.URL, "secret", server.Client())
	mem := newMemory()
	c, _ := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	started, _ := start(t, c, "")
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateSucceeded || string(run.Output) != `{"ok":true}` {
		t.Fatalf("run=%+v", run)
	}
}

func TestSubmittedLimitsStopTheRun(t *testing.T) {
	subagent := map[string]any{"type": "tool", "name": "subagent", "state": map[string]any{"status": "completed", "input": map[string]any{"agent": "one"}}}
	catalog := map[string]any{"type": "tool", "name": "catalog_search", "state": map[string]any{"status": "completed"}}
	cases := []struct {
		name     string
		limits   contracts.Limits
		messages []Message
		tokens   struct{ in, out int }
		cost     float64
		children int
		tools    int
		message  string
	}{
		{
			name:     "tokens",
			limits:   withLimit(openLimits(), func(l *contracts.Limits) { l.MaxTokens = 100 }),
			messages: []Message{assistantMessage(map[string]any{"type": "text", "text": `{"ok":true}`})},
			tokens:   struct{ in, out int }{80, 30},
			children: 0,
			message:  "token limit exceeded",
		},
		{
			name:     "cost",
			limits:   withLimit(openLimits(), func(l *contracts.Limits) { l.MaxCostUSD = 1 }),
			messages: []Message{assistantMessage(map[string]any{"type": "text", "text": `{"ok":true}`})},
			cost:     1.5,
			children: 0,
			message:  "cost limit exceeded",
		},
		{
			name:   "turns",
			limits: withLimit(openLimits(), func(l *contracts.Limits) { l.MaxModelTurns = 1 }),
			messages: []Message{
				assistantMessage(subagent, map[string]any{"type": "text", "text": `{"ok":true}`}),
				assistantMessage(map[string]any{"type": "text", "text": "working"}),
			},
			children: 0,
			message:  "model turn limit exceeded",
		},
		{
			name:     "tool calls",
			limits:   withLimit(openLimits(), func(l *contracts.Limits) { l.MaxToolCalls = 1 }),
			messages: []Message{assistantMessage(catalog, catalog, map[string]any{"type": "text", "text": `{"ok":true}`})},
			children: 0,
			tools:    1,
			message:  "tool-call limit exceeded",
		},
		{
			name:     "delegations",
			limits:   withLimit(openLimits(), func(l *contracts.Limits) { l.MaxDelegations = 1 }),
			messages: []Message{assistantMessage(subagent, subagent, map[string]any{"type": "text", "text": `{"ok":true}`})},
			children: 1,
			tools:    1,
			message:  "delegation limit exceeded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeBackend{outcome: "succeeded", cost: tc.cost, messages: tc.messages}
			backend.tokens.in, backend.tokens.out = tc.tokens.in, tc.tokens.out
			server := httptest.NewServer(backend.handler())
			defer server.Close()
			client, err := NewClient(server.URL, "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			mem := newMemory()
			defs := testDefs()
			defs[0].AllowedTools = []string{"catalog.search"}
			c, err := New(defs, client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
			if err != nil {
				t.Fatal(err)
			}
			started := startLimits(t, c, "", tc.limits)
			run := waitRun(t, mem, started.ID)
			if run.State != contracts.StateFailed || run.Error == nil || run.Error.Code != contracts.ErrBudgetExceeded || run.Error.Message != tc.message {
				t.Fatalf("run=%+v", run)
			}
			if run.Output != nil {
				t.Fatalf("output accepted after stop: %s", run.Output)
			}
			if len(run.Invocations) != 1+tc.children {
				t.Fatalf("invocations=%+v", run.Invocations)
			}
			if len(run.ToolInvocations) != tc.tools {
				t.Fatalf("tools=%+v", run.ToolInvocations)
			}
			events, _ := mem.List(context.Background(), run.ID, 0)
			if !eventCode(t, events, "RUN_FAILED", string(contracts.ErrBudgetExceeded)) {
				t.Fatalf("events=%+v", events)
			}
			if tc.name == "tokens" && (run.Usage.InputTokens != 80 || run.Usage.OutputTokens != 30) {
				t.Fatalf("usage=%+v", run.Usage)
			}
			if tc.name == "cost" && run.Usage.CostUSD != 1.5 {
				t.Fatalf("usage=%+v", run.Usage)
			}
		})
	}
}

func TestTokenLimitAllowsExactTotal(t *testing.T) {
	backend := &fakeBackend{outcome: "succeeded"}
	backend.tokens.in, backend.tokens.out = 60, 40
	backend.messages = []Message{assistantMessage(map[string]any{"type": "text", "text": `{"ok":true}`})}
	server := httptest.NewServer(backend.handler())
	defer server.Close()
	client, _ := NewClient(server.URL, "secret", server.Client())
	mem := newMemory()
	c, _ := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	limits := openLimits()
	limits.MaxTokens = 100
	started := startLimits(t, c, "", limits)
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateSucceeded || run.Usage.InputTokens+run.Usage.OutputTokens != 100 {
		t.Fatalf("run=%+v", run)
	}
}

func TestDelegationLimitInterruptsSession(t *testing.T) {
	subagent := map[string]any{"type": "tool", "name": "subagent", "state": map[string]any{"status": "completed", "input": map[string]any{"agent": "one"}}}
	backend := &fakeBackend{
		outcome:  "succeeded",
		holdWait: true,
		release:  make(chan struct{}),
		messages: []Message{assistantMessage(subagent, subagent, map[string]any{"type": "text", "text": `{"ok":true}`})},
	}
	previous := budgetPollInterval
	budgetPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { budgetPollInterval = previous })
	server := httptest.NewServer(backend.handler())
	defer server.Close()
	client, _ := NewClient(server.URL, "secret", server.Client())
	mem := newMemory()
	c, _ := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	limits := openLimits()
	limits.MaxDelegations = 1
	limits.TimeoutSeconds = 3
	started := startLimits(t, c, "", limits)
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateFailed || run.Error == nil || run.Error.Code != contracts.ErrBudgetExceeded {
		t.Fatalf("run=%+v", run)
	}
	if len(run.Invocations) != 2 {
		t.Fatalf("invocations=%+v", run.Invocations)
	}
	backend.mu.Lock()
	interrupted := backend.interrupted
	backend.mu.Unlock()
	if !interrupted {
		t.Fatal("in-flight session was not interrupted")
	}
}

func TestDeadlineRecordsTimeout(t *testing.T) {
	backend := &fakeBackend{holdWait: true, release: make(chan struct{})}
	server := httptest.NewServer(backend.handler())
	defer server.Close()
	client, _ := NewClient(server.URL, "secret", server.Client())
	mem := newMemory()
	c, _ := New(testDefs(), client, mem, mem, t.TempDir(), "meta/muse-spark-1.3-contributor")
	limits := openLimits()
	limits.TimeoutSeconds = 1
	started := startLimits(t, c, "", limits)
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateFailed || run.Error == nil || run.Error.Code != contracts.ErrTimeout {
		t.Fatalf("run=%+v", run)
	}
	backend.mu.Lock()
	interrupted := backend.interrupted
	backend.mu.Unlock()
	if interrupted {
		t.Fatal("deadline interrupted the session for budget")
	}
}

func withLimit(limits contracts.Limits, edit func(*contracts.Limits)) contracts.Limits {
	edit(&limits)
	return limits
}
