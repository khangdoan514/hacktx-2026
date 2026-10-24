package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/httpapi"
	"github.com/khangdoan514/hacktx-2026/agents/internal/model"
	"github.com/khangdoan514/hacktx-2026/agents/internal/runtime"
	"github.com/khangdoan514/hacktx-2026/agents/internal/tools"
)

type memoryHistory struct {
	mu     sync.Mutex
	runs   map[string]contracts.Run
	events map[string][]contracts.RunEvent
}

func newMemoryHistory() *memoryHistory {
	return &memoryHistory{runs: map[string]contracts.Run{}, events: map[string][]contracts.RunEvent{}}
}
func (m *memoryHistory) Create(_ context.Context, run contracts.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[run.ID]; ok {
		return fmt.Errorf("duplicate run")
	}
	m.runs[run.ID] = run
	return nil
}
func (m *memoryHistory) Get(_ context.Context, id string) (contracts.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	return run, nil
}
func (m *memoryHistory) Update(_ context.Context, run contracts.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.ID] = run
	return nil
}
func (m *memoryHistory) Append(_ context.Context, event contracts.RunEvent) (contracts.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	event.Sequence = uint64(len(m.events[event.RunID]) + 1)
	m.events[event.RunID] = append(m.events[event.RunID], event)
	return event, nil
}
func (m *memoryHistory) List(_ context.Context, id string, after uint64) ([]contracts.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[id]; !ok {
		return nil, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	out := []contracts.RunEvent{}
	for _, event := range m.events[id] {
		if event.Sequence > after {
			out = append(out, event)
		}
	}
	return out, nil
}

func TestJoinedAPIExamples(t *testing.T) {
	var definitions []contracts.AgentDefinition
	if err := readJSON("../../examples/definitions.json", &definitions); err != nil {
		t.Fatal(err)
	}
	steps := map[string][]contracts.ModelResult{}
	if err := readJSON("../../examples/deterministic-actions.json", &steps); err != nil {
		t.Fatal(err)
	}
	catalog, err := tools.NewCatalog()
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := tools.NewGateway(catalog)
	if err != nil {
		t.Fatal(err)
	}
	history := newMemoryHistory()
	coordinator, err := runtime.New(definitions, model.NewDeterministic(steps), gateway, history, history)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(coordinator, history, history, definitions))
	defer server.Close()
	var sample contracts.StartRunRequest
	if err := readJSON("../../examples/vendor-comparison.json", &sample); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ agent, wantState, wantEvent string }{
		{"vendor-orchestrator", "SUCCEEDED", "TOOL_SUCCEEDED"},
		{"denial-orchestrator", "SUCCEEDED", "DELEGATION_REJECTED"},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			sample.AgentID = tc.agent
			body, _ := json.Marshal(sample)
			response, err := http.Post(server.URL+"/v1/agent-runs", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("start status %d", response.StatusCode)
			}
			var started contracts.StartRunResponse
			if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			var run contracts.Run
			for time.Now().Before(deadline) {
				run, err = history.Get(context.Background(), started.RunID)
				if err != nil {
					t.Fatal(err)
				}
				if run.State == contracts.StateSucceeded || run.State == contracts.StateFailed {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if string(run.State) != tc.wantState {
				t.Fatalf("state %s error %+v", run.State, run.Error)
			}
			events, err := history.List(context.Background(), run.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for i, event := range events {
				if event.Sequence != uint64(i+1) {
					t.Fatal("unordered events")
				}
				if event.Type == tc.wantEvent {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s event", tc.wantEvent)
			}
			if tc.agent == "vendor-orchestrator" {
				if len(run.Invocations) != 3 || len(run.ToolInvocations) != 2 || run.Usage.InputTokens == 0 {
					t.Fatalf("incomplete run: %+v", run)
				}
			}
		})
	}
}
