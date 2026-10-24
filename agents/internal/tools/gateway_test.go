package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

type fakeTool struct {
	calls  atomic.Int32
	action func(context.Context) (json.RawMessage, error)
}

func (f *fakeTool) Definition() contracts.ToolDefinition {
	return contracts.ToolDefinition{ID: "fake", Version: "1.0.0", SideEffect: "READ_ONLY", TimeoutSeconds: 1, InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`), OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`)}
}
func (f *fakeTool) Invoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	f.calls.Add(1)
	return f.action(ctx)
}

func TestGatewayAuthorizationAndSchemas(t *testing.T) {
	f := &fakeTool{action: func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"ok":true}`), nil }}
	g, err := NewGateway(f)
	if err != nil {
		t.Fatal(err)
	}
	inv := contracts.AgentInvocation{ID: "inv", RunID: "run"}
	agent := contracts.AgentDefinition{AllowedTools: []string{"fake"}}
	for _, tc := range []struct {
		name   string
		agent  contracts.AgentDefinition
		input  string
		code   contracts.ErrorCode
		called bool
	}{
		{"denied", contracts.AgentDefinition{}, `{"id":"x"}`, contracts.ErrToolNotAllowed, false},
		{"malformed", agent, `{"id":42}`, contracts.ErrInvalidInput, false},
		{"extra", agent, `{"id":"x","secret":"x"}`, contracts.ErrInvalidInput, false},
		{"secret key", agent, `{"id":"x","secret=never-show":"x"}`, contracts.ErrInvalidInput, false},
		{"success", agent, `{"id":"x"}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := f.calls.Load()
			got, err := g.Invoke(context.Background(), tc.agent, inv, contracts.ToolRequest{ToolID: "fake", Input: json.RawMessage(tc.input)})
			if tc.code == "" {
				if err != nil || got.Error != nil || got.FinishedAt == nil || string(got.Output) != `{"ok":true}` {
					t.Fatalf("got %+v %v", got, err)
				}
			} else {
				var re *contracts.RunError
				if !errors.As(err, &re) || re.Code != tc.code || got.Error == nil || got.FinishedAt == nil {
					t.Fatalf("got %+v %v", got, err)
				}
				if tc.name == "secret key" && got.Error.Message != "invalid tool input" {
					t.Fatalf("input key leaked: %s", got.Error.Message)
				}
			}
			if (f.calls.Load() > before) != tc.called {
				t.Fatal("handler call mismatch")
			}
		})
	}
}

func TestGatewayTimeoutAndFailureAreSafe(t *testing.T) {
	f := &fakeTool{action: func(ctx context.Context) (json.RawMessage, error) { <-ctx.Done(); return nil, ctx.Err() }}
	g, _ := NewGateway(f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	got, err := g.Invoke(ctx, contracts.AgentDefinition{AllowedTools: []string{"fake"}}, contracts.AgentInvocation{ID: "i", RunID: "r"}, contracts.ToolRequest{ToolID: "fake", Input: json.RawMessage(`{"id":"x"}`)})
	var re *contracts.RunError
	if !errors.As(err, &re) || re.Code != contracts.ErrTimeout || got.Error == nil {
		t.Fatalf("got %+v %v", got, err)
	}
	failureTool := &fakeTool{action: func(context.Context) (json.RawMessage, error) { return nil, errors.New("secret=never-show") }}
	failureGateway, err := NewGateway(failureTool)
	if err != nil {
		t.Fatal(err)
	}
	got, err = failureGateway.Invoke(context.Background(), contracts.AgentDefinition{AllowedTools: []string{"fake"}}, contracts.AgentInvocation{ID: "i", RunID: "r"}, contracts.ToolRequest{ToolID: "fake", Input: json.RawMessage(`{"id":"x"}`)})
	if err == nil || got.Error == nil || got.Error.Message != "tool failed" {
		t.Fatalf("unsafe failure %+v %v", got, err)
	}
}

func TestCatalogFixtureAndOutput(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	fixture, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "catalog", "vendors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(fixture) != string(vendorFixture) {
		t.Fatal("embedded catalog differs from checked-in fixture")
	}
	c, err := NewCatalog()
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewGateway(c)
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.Invoke(context.Background(), contracts.AgentDefinition{AllowedTools: []string{"catalog.search"}}, contracts.AgentInvocation{ID: "i", RunID: "r"}, contracts.ToolRequest{ToolID: "catalog.search", Input: json.RawMessage(`{"criteria":["accessibility"],"budget_usd":700}`)})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Vendors   []Vendor `json:"vendors"`
		SourceIDs []string `json:"source_ids"`
	}
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Vendors) != 1 || output.Vendors[0].ID != "vendor-a" || len(output.SourceIDs) != 1 || output.SourceIDs[0] != "vendor-a" {
		t.Fatalf("bad catalog result %+v", output)
	}
}

func TestSchemaRejectsUnsupportedKeywords(t *testing.T) {
	if err := ValidateJSON(json.RawMessage(`{"type":"string","pattern":"x"}`), json.RawMessage(`"x"`)); err == nil {
		t.Fatal("unsupported keyword accepted")
	}
}
