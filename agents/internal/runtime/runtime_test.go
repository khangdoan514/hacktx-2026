package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

type memory struct {
	sync.Mutex
	runs   map[string]contracts.Run
	events map[string][]contracts.RunEvent
}

func newMemory() *memory {
	return &memory{runs: map[string]contracts.Run{}, events: map[string][]contracts.RunEvent{}}
}
func (m *memory) Create(_ context.Context, r contracts.Run) error {
	m.Lock()
	defer m.Unlock()
	m.runs[r.ID] = r
	return nil
}
func (m *memory) Get(_ context.Context, id string) (contracts.Run, error) {
	m.Lock()
	defer m.Unlock()
	return m.runs[id], nil
}
func (m *memory) Update(_ context.Context, r contracts.Run) error {
	m.Lock()
	defer m.Unlock()
	m.runs[r.ID] = r
	return nil
}
func (m *memory) Append(_ context.Context, e contracts.RunEvent) (contracts.RunEvent, error) {
	m.Lock()
	defer m.Unlock()
	e.Sequence = uint64(len(m.events[e.RunID]) + 1)
	m.events[e.RunID] = append(m.events[e.RunID], e)
	return e, nil
}
func (m *memory) List(_ context.Context, id string, after uint64) ([]contracts.RunEvent, error) {
	m.Lock()
	defer m.Unlock()
	var out []contracts.RunEvent
	for _, e := range m.events[id] {
		if e.Sequence > after {
			out = append(out, e)
		}
	}
	return out, nil
}

type noTools struct{}

func (noTools) Definitions() []contracts.ToolDefinition { return nil }

func (noTools) Invoke(context.Context, contracts.AgentDefinition, contracts.AgentInvocation, contracts.ToolRequest) (contracts.ToolInvocation, error) {
	return contracts.ToolInvocation{}, nil
}

type modelFunc func(context.Context, contracts.ModelInput) (contracts.ModelResult, error)

func (f modelFunc) Next(ctx context.Context, in contracts.ModelInput) (contracts.ModelResult, error) {
	return f(ctx, in)
}
func lim() contracts.Limits {
	return contracts.Limits{MaxDelegations: 2, MaxConcurrentAgents: 2, MaxToolCalls: 2, MaxModelTurns: 10, TimeoutSeconds: 3, MaxTokens: 100, MaxCostUSD: 1}
}
func defs() []contracts.AgentDefinition {
	return []contracts.AgentDefinition{{ID: "root", Version: "1.0.0", Name: "root", Limits: lim(), AllowedDelegates: []string{"one", "two"}, OutputSchema: json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}}}`)}, {ID: "one", Version: "1.0.0", Name: "one", Limits: lim()}, {ID: "two", Version: "1.0.0", Name: "two", Limits: lim()}}
}
func waitRun(t *testing.T, m *memory, id string) contracts.Run {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := m.Get(context.Background(), id)
		if r.State == contracts.StateSucceeded || r.State == contracts.StateFailed || r.State == contracts.StateCancelled {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("run did not finish")
	return contracts.Run{}
}
func start(t *testing.T, c *Coordinator, m *memory) contracts.Run {
	t.Helper()
	r, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: lim()})
	if err != nil {
		t.Fatal(err)
	}
	return waitRun(t, m, r.ID)
}

func TestParallelSpecialistsAndOrderedEvents(t *testing.T) {
	m := newMemory()
	entered := make(chan string, 2)
	release := make(chan struct{})
	f := modelFunc(func(ctx context.Context, in contracts.ModelInput) (contracts.ModelResult, error) {
		if in.Invocation.Depth == 1 {
			entered <- in.Invocation.AgentID
			select {
			case <-release:
			case <-ctx.Done():
				return contracts.ModelResult{}, ctx.Err()
			}
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{"source_ids":["x"]}`)}, Usage: contracts.Usage{InputTokens: 1}}, nil
		}
		switch len(in.History) {
		case 0:
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "one", Input: json.RawMessage(`{}`)}}}, nil
		case 1:
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "two", Input: json.RawMessage(`{}`)}}}, nil
		default:
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{"ok":true}`)}}, nil
		}
	})
	c, err := New(defs(), f, noTools{}, m, m)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: lim()})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first child did not enter")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("children did not run concurrently")
	}
	close(release)
	done := waitRun(t, m, r.ID)
	if done.State != contracts.StateSucceeded || len(done.Invocations) != 3 || done.Usage.InputTokens != 2 {
		t.Fatalf("unexpected run: %+v", done)
	}
	events, _ := m.List(context.Background(), r.ID, 0)
	for i, e := range events {
		if e.Sequence != uint64(i+1) {
			t.Fatal("unordered events")
		}
	}
}
func TestDepthDenial(t *testing.T) {
	m := newMemory()
	f := modelFunc(func(_ context.Context, in contracts.ModelInput) (contracts.ModelResult, error) {
		if in.Invocation.Depth == 0 && len(in.History) == 0 {
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "one", Input: json.RawMessage(`{}`)}}}, nil
		}
		if in.Invocation.Depth == 1 {
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "two", Input: json.RawMessage(`{}`)}}}, nil
		}
		return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{"ok":true}`)}}, nil
	})
	c, _ := New(defs(), f, noTools{}, m, m)
	r := start(t, c, m)
	if len(r.Invocations) != 2 {
		t.Fatalf("unexpected child count %d", len(r.Invocations))
	}
	var denied bool
	events, _ := m.List(context.Background(), r.ID, 0)
	for _, e := range events {
		if e.Type == "DELEGATION_REJECTED" {
			denied = true
		}
	}
	if !denied {
		t.Fatal("missing denial event")
	}
}
func TestInvalidActionAndOutput(t *testing.T) {
	for _, action := range []contracts.ModelAction{{Type: contracts.ActionFinish, ToolCall: &contracts.ToolRequest{ToolID: "x", Input: json.RawMessage(`{}`)}}, {Type: contracts.ActionFinish, Output: json.RawMessage(`{"wrong":true}`)}} {
		m := newMemory()
		c, _ := New(defs(), modelFunc(func(context.Context, contracts.ModelInput) (contracts.ModelResult, error) {
			return contracts.ModelResult{Action: action}, nil
		}), noTools{}, m, m)
		r := start(t, c, m)
		if r.State != contracts.StateFailed || r.Error == nil {
			t.Fatalf("invalid action accepted: %+v", r)
		}
	}
}
func TestTokenBudget(t *testing.T) {
	m := newMemory()
	c, _ := New(defs(), modelFunc(func(context.Context, contracts.ModelInput) (contracts.ModelResult, error) {
		return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{"ok":true}`)}, Usage: contracts.Usage{InputTokens: 101}}, nil
	}), noTools{}, m, m)
	r := start(t, c, m)
	if r.Error == nil || r.Error.Code != contracts.ErrBudgetExceeded || r.Usage.InputTokens != 101 {
		t.Fatalf("budget not enforced: %+v", r)
	}
}

func TestCancellationAndCostBudget(t *testing.T) {
	t.Run("cost", func(t *testing.T) {
		m := newMemory()
		c, _ := New(defs(), modelFunc(func(context.Context, contracts.ModelInput) (contracts.ModelResult, error) {
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{"ok":true}`)}, Usage: contracts.Usage{CostUSD: 2}}, nil
		}), noTools{}, m, m)
		r := start(t, c, m)
		if r.State != contracts.StateFailed || r.Error.Code != contracts.ErrBudgetExceeded {
			t.Fatalf("cost accepted: %+v", r)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		m := newMemory()
		entered := make(chan struct{})
		f := modelFunc(func(ctx context.Context, _ contracts.ModelInput) (contracts.ModelResult, error) {
			close(entered)
			<-ctx.Done()
			return contracts.ModelResult{}, ctx.Err()
		})
		c, _ := New(defs(), f, noTools{}, m, m)
		r, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: lim()})
		if err != nil {
			t.Fatal(err)
		}
		<-entered
		if err := c.Cancel(r.ID); err != nil {
			t.Fatal(err)
		}
		done := waitRun(t, m, r.ID)
		if done.State != contracts.StateCancelled {
			t.Fatalf("not cancelled: %+v", done)
		}
		events, _ := m.List(context.Background(), r.ID, 0)
		var agent, run bool
		for _, e := range events {
			agent = agent || e.Type == "AGENT_CANCELLED"
			run = run || e.Type == "RUN_CANCELLED"
		}
		if !agent || !run {
			t.Fatalf("missing cancellation events: %+v", events)
		}
	})
}

func TestSchedulingBudgets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  func(*contracts.Limits)
		action contracts.ModelAction
	}{
		{"turn", func(l *contracts.Limits) { l.MaxModelTurns = 1 }, contracts.ModelAction{Type: contracts.ActionTool, ToolCall: &contracts.ToolRequest{ToolID: "fake", Input: json.RawMessage(`{}`)}}},
		{"tool", func(l *contracts.Limits) { l.MaxToolCalls = 1 }, contracts.ModelAction{Type: contracts.ActionTool, ToolCall: &contracts.ToolRequest{ToolID: "fake", Input: json.RawMessage(`{}`)}}},
		{"delegation", func(l *contracts.Limits) { l.MaxDelegations = 1 }, contracts.ModelAction{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "one", Input: json.RawMessage(`{}`)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMemory()
			f := modelFunc(func(_ context.Context, in contracts.ModelInput) (contracts.ModelResult, error) {
				if in.Invocation.Depth == 1 {
					return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{}`)}}, nil
				}
				return contracts.ModelResult{Action: tc.action}, nil
			})
			c, err := New(defs(), f, noTools{}, m, m)
			if err != nil {
				t.Fatal(err)
			}
			limits := lim()
			tc.limit(&limits)
			started, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: limits})
			if err != nil {
				t.Fatal(err)
			}
			run := waitRun(t, m, started.ID)
			if run.State != contracts.StateFailed || run.Error == nil || run.Error.Code != contracts.ErrBudgetExceeded {
				t.Fatalf("budget failure missing: %+v", run)
			}
		})
	}
}

func TestDeadlineStopsModel(t *testing.T) {
	m := newMemory()
	f := modelFunc(func(ctx context.Context, _ contracts.ModelInput) (contracts.ModelResult, error) {
		<-ctx.Done()
		return contracts.ModelResult{}, ctx.Err()
	})
	c, err := New(defs(), f, noTools{}, m, m)
	if err != nil {
		t.Fatal(err)
	}
	limits := lim()
	limits.TimeoutSeconds = 1
	started, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	run := waitRun(t, m, started.ID)
	if run.State != contracts.StateFailed || run.Error == nil || run.Error.Code != contracts.ErrTimeout {
		t.Fatalf("deadline missing: %+v", run)
	}
}

func TestConcurrencyLimitSerializesSpecialists(t *testing.T) {
	m := newMemory()
	entered := make(chan string, 2)
	release := make(chan struct{})
	f := modelFunc(func(ctx context.Context, in contracts.ModelInput) (contracts.ModelResult, error) {
		if in.Invocation.Depth == 1 {
			entered <- in.Invocation.AgentID
			select {
			case <-release:
			case <-ctx.Done():
				return contracts.ModelResult{}, ctx.Err()
			}
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{}`)}}, nil
		}
		switch len(in.History) {
		case 0:
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "one", Input: json.RawMessage(`{}`)}}}, nil
		case 1:
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "two", Input: json.RawMessage(`{}`)}}}, nil
		default:
			return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{"ok":true}`)}}, nil
		}
	})
	c, err := New(defs(), f, noTools{}, m, m)
	if err != nil {
		t.Fatal(err)
	}
	limits := lim()
	limits.MaxConcurrentAgents = 1
	started, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first specialist did not start")
	}
	select {
	case <-entered:
		t.Fatal("second specialist exceeded concurrency limit")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second specialist never started")
	}
	run := waitRun(t, m, started.ID)
	if run.State != contracts.StateSucceeded {
		t.Fatalf("run failed: %+v", run)
	}
}

func TestPerRunModelIDPropagatesAndValidates(t *testing.T) {
	m := newMemory()
	var seen string
	f := modelFunc(func(_ context.Context, in contracts.ModelInput) (contracts.ModelResult, error) {
		seen = in.Context.ModelID
		return contracts.ModelResult{Action: contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{"ok":true}`)}, Usage: contracts.Usage{}}, nil
	})
	c, err := New(defs(), f, noTools{}, m, m)
	if err != nil {
		t.Fatal(err)
	}
	started, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", ModelID: "openai/gpt-4o-mini", Input: json.RawMessage(`{}`), Limits: lim()})
	if err != nil {
		t.Fatal(err)
	}
	run := waitRun(t, m, started.ID)
	if run.State != contracts.StateSucceeded || run.ModelID != "openai/gpt-4o-mini" || seen != "openai/gpt-4o-mini" {
		t.Fatalf("model propagation failed: run=%+v seen=%q", run, seen)
	}
	if _, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "root", AgentVersion: "1.0.0", ModelID: "bad model", Input: json.RawMessage(`{}`), Limits: lim()}); err == nil {
		t.Fatal("invalid model ID accepted")
	}
}
