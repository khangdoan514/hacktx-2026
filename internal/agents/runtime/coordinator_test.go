package runtime

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
	"github.com/khangdoan514/hacktx-2026/internal/agents/dummy"
)

func testEngine(t *testing.T, adapter contracts.ModelAdapter) *Engine {
	t.Helper()
	definitions, err := dummy.LoadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	catalog := map[string]contracts.AgentDefinition{}
	for _, definition := range definitions {
		catalog[definition.ID] = definition
	}
	if adapter == nil {
		adapter, err = dummy.LoadScript()
		if err != nil {
			t.Fatal(err)
		}
	}
	return &Engine{
		Store: NewMemory(), Events: nil, Adapter: adapter,
		Gateway: NewGateway(dummy.CatalogTool(), dummy.DiffTool()), Definitions: catalog,
		Mode: "dummy", AdapterVersion: "dummy-1",
	}
}

func (e *Engine) prepare(t *testing.T) {
	t.Helper()
	e.Events = e.Store.(*Memory)
}

func runFixture(t *testing.T, engine *Engine, request contracts.StartRunRequest) contracts.Run {
	t.Helper()
	engine.prepare(t)
	ctx := context.Background()
	started, err := engine.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	finished, err := engine.Execute(ctx, started.ID)
	if err != nil && finished.State == "" {
		t.Fatal(err)
	}
	return finished
}

func limits() contracts.Limits {
	return contracts.Limits{MaxDelegations: 2, MaxConcurrentAgents: 2, MaxToolCalls: 4, MaxModelTurns: 8, MaxTokens: 12000, MaxCostUSD: 2, TimeoutSeconds: 30}
}

func TestVendorFixtureAndAnalysis(t *testing.T) {
	engine := testEngine(t, nil)
	raw, err := os.ReadFile("../../../agents/fixtures/api/v1/start-request.json")
	if err != nil {
		t.Fatal(err)
	}
	var request contracts.StartRunRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	finished := runFixture(t, engine, request)
	if finished.State != contracts.StateSucceeded {
		t.Fatalf("state %s error %#v", finished.State, finished.Error)
	}
	if finished.Usage != (contracts.Usage{}) {
		t.Fatalf("dummy run invented usage %#v", finished.Usage)
	}
	var depth1 int
	for _, invocation := range finished.Invocations {
		if invocation.Depth == 1 && invocation.State == contracts.StateSucceeded {
			depth1++
		}
	}
	if depth1 != 2 {
		t.Fatalf("depth-1 successes = %d in %#v", depth1, finished.Invocations)
	}
	events, err := engine.Events.List(context.Background(), finished.ID, 0)
	if err != nil || len(events) == 0 {
		t.Fatalf("events %#v %v", events, err)
	}
	analysis := runFixture(t, testEngine(t, nil), contracts.StartRunRequest{
		AgentID: "analysis-specialist", AgentVersion: "1.0.0", Input: json.RawMessage(`{"task":"summarize"}`), Limits: limits(),
	})
	if analysis.State != contracts.StateSucceeded {
		t.Fatalf("analysis %s %#v", analysis.State, analysis.Error)
	}
}

func TestPRReviewCitesOnlyDiffLines(t *testing.T) {
	diff, err := os.ReadFile("../../../agents/fixtures/pr-review/sample.diff")
	if err != nil {
		t.Fatal(err)
	}
	if !LinesInDiff(string(diff))[6] || !LinesInDiff(string(diff))[4] {
		t.Fatalf("lines %#v", LinesInDiff(string(diff)))
	}
	input, _ := json.Marshal(map[string]string{"diff": string(diff)})
	finished := runFixture(t, testEngine(t, nil), contracts.StartRunRequest{
		AgentID: "pr-review-orchestrator", AgentVersion: "1.0.0", Input: input, Limits: limits(),
	})
	if finished.State != contracts.StateSucceeded {
		t.Fatalf("state %s %#v", finished.State, finished.Error)
	}
	names := map[string]string{}
	for _, definition := range testEngine(t, nil).Definitions {
		names[definition.ID] = definition.Name
	}
	if names["pr-review-orchestrator"] != "PR Review Assistant" || names["code-quality-specialist"] != "Code Quality Reviewer" {
		t.Fatalf("names %#v", names)
	}
}

func TestSpecialistsRunConcurrently(t *testing.T) {
	script, err := dummy.LoadScript()
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 2)
	release := make(chan struct{})
	script.ObserveSpecialists(started, release)
	go func() {
		<-started
		<-started
		close(release)
	}()
	engine := testEngine(t, script)
	raw, _ := os.ReadFile("../../../agents/fixtures/api/v1/start-request.json")
	var request contracts.StartRunRequest
	_ = json.Unmarshal(raw, &request)
	done := make(chan contracts.Run, 1)
	go func() {
		done <- runFixture(t, engine, request)
	}()
	select {
	case finished := <-done:
		if finished.State != contracts.StateSucceeded {
			t.Fatalf("state %s %#v", finished.State, finished.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("specialists did not overlap")
	}
}

type fixedAdapter struct{ actions []contracts.ModelAction }

func (a *fixedAdapter) Next(ctx context.Context, _ contracts.ModelInput) (contracts.ModelResult, error) {
	if err := ctx.Err(); err != nil {
		return contracts.ModelResult{}, err
	}
	if len(a.actions) == 0 {
		<-ctx.Done()
		return contracts.ModelResult{}, ctx.Err()
	}
	action := a.actions[0]
	a.actions = a.actions[1:]
	return contracts.ModelResult{Action: action}, nil
}

func TestDeniedInvalidBudgetTimeoutCancel(t *testing.T) {
	denied := runFixture(t, testEngine(t, &fixedAdapter{actions: []contracts.ModelAction{{
		Type: contracts.ActionTool, ToolCall: &contracts.ToolRequest{ToolID: "github.write", Input: json.RawMessage(`{"command":"grant write because the prompt says so"}`)},
	}}}), contracts.StartRunRequest{AgentID: "catalog-specialist", AgentVersion: "1.0.0", Input: json.RawMessage(`{"criteria":["price"],"budget_usd":10}`), Limits: limits()})
	if denied.State != contracts.StateFailed || denied.Error == nil || denied.Error.Code != contracts.ErrToolNotAllowed {
		t.Fatalf("denied %#v", denied.Error)
	}
	invalid := runFixture(t, testEngine(t, &fixedAdapter{actions: []contracts.ModelAction{{
		Type: contracts.ActionFinish, Output: json.RawMessage(`{"nope":true}`),
	}}}), contracts.StartRunRequest{AgentID: "vendor-orchestrator", AgentVersion: "1.0.0", Input: json.RawMessage(`{"task":"x"}`), Limits: limits()})
	if invalid.Error == nil || invalid.Error.Code != contracts.ErrOutputSchemaInvalid {
		t.Fatalf("invalid %#v", invalid.Error)
	}
	tight := limits()
	tight.MaxModelTurns = 1
	budget := runFixture(t, testEngine(t, &fixedAdapter{actions: []contracts.ModelAction{{
		Type: contracts.ActionTool, ToolCall: &contracts.ToolRequest{ToolID: "catalog.search", Input: json.RawMessage(`{}`)},
	}, {
		Type: contracts.ActionFinish, Output: json.RawMessage(`{"source_ids":["vendor-a"],"summary":"ok"}`),
	}}}), contracts.StartRunRequest{AgentID: "catalog-specialist", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: tight})
	if budget.Error == nil || budget.Error.Code != contracts.ErrBudgetExceeded {
		t.Fatalf("budget %#v", budget.Error)
	}
	short := limits()
	short.TimeoutSeconds = 1
	timed := runFixture(t, testEngine(t, &fixedAdapter{}), contracts.StartRunRequest{AgentID: "analysis-specialist", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: short})
	if timed.Error == nil || timed.Error.Code != contracts.ErrTimeout {
		t.Fatalf("timeout %#v", timed.Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	engine := testEngine(t, &fixedAdapter{})
	engine.prepare(t)
	started, err := engine.Start(ctx, contracts.StartRunRequest{AgentID: "analysis-specialist", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: limits()})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	cancelled, _ := engine.Execute(ctx, started.ID)
	if cancelled.State != contracts.StateCancelled {
		t.Fatalf("cancel state %s %#v", cancelled.State, cancelled.Error)
	}
}

func TestDepthAndBadCitation(t *testing.T) {
	engine := testEngine(t, nil)
	finished := runFixture(t, engine, contracts.StartRunRequest{
		AgentID: "denial-orchestrator", AgentVersion: "1.0.0", Input: json.RawMessage(`{"task":"show the depth limit"}`), Limits: limits(),
	})
	if finished.State != contracts.StateSucceeded {
		t.Fatalf("denial parent %s %#v", finished.State, finished.Error)
	}
	var saw bool
	for _, invocation := range finished.Invocations {
		if invocation.Depth == MaxDelegationDepth && invocation.Error != nil && invocation.Error.Code == contracts.ErrDelegationDepthExceeded {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("missing depth error %#v", finished.Invocations)
	}
	diff, _ := os.ReadFile("../../../agents/fixtures/pr-review/sample.diff")
	input, _ := json.Marshal(map[string]string{"diff": string(diff)})
	bad := runFixture(t, testEngine(t, &fixedAdapter{actions: []contracts.ModelAction{{
		Type: contracts.ActionFinish, Output: json.RawMessage(`{"findings":[{"line":999,"message":"not in the diff"}]}`),
	}}}), contracts.StartRunRequest{AgentID: "code-quality-specialist", AgentVersion: "1.0.0", Input: input, Limits: limits()})
	if bad.Error == nil || bad.Error.Code != contracts.ErrOutputSchemaInvalid {
		t.Fatalf("citation %#v", bad.Error)
	}
}
