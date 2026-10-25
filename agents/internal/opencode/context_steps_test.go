package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

func contextCoordinator(t *testing.T, backend *fakeBackend, steps []contracts.ContextStep) (*Coordinator, *memory) {
	t.Helper()
	server := httptest.NewServer(backend.handler())
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	mem := newMemory()
	source := func(context.Context) ([]contracts.AgentDefinition, error) {
		return []contracts.AgentDefinition{{ID: "issue-triage", Version: "1.0.0", Name: "Issue triage", Role: "specialist", Instructions: "Rank the open issues.", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Limits: openLimits(), ContextSteps: steps}}, nil
	}
	// A run started by an employee opens a per-employee project, which is
	// derived from the workspace config.
	workdir := t.TempDir()
	t.Setenv("NMC3_TOOLS_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(workdir, "opencode.jsonc"), []byte(`{"mcp":{"servers":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := New(testDefs(), client, mem, mem, workdir, "meta/muse-spark-1.3-contributor", source)
	if err != nil {
		t.Fatal(err)
	}
	return c, mem
}

func TestContextStepsRunBeforeThePrompt(t *testing.T) {
	backend := &fakeBackend{outcome: "succeeded", messages: []Message{assistantMessage(map[string]any{"type": "text", "text": `{"ok":true}`})}}
	steps := []contracts.ContextStep{{ID: "issues", Name: "Open issues", Tool: "github.list_issues", Arguments: json.RawMessage(`{"owner":"acme","repo":"{{input.repo}}"}`)}}
	c, mem := contextCoordinator(t, backend, steps)
	var gotOwner, gotTool, gotArgs string
	c.SetToolCaller(func(_ context.Context, owner, toolID string, args json.RawMessage) (any, error) {
		gotOwner, gotTool, gotArgs = owner, toolID, string(args)
		return map[string]any{"issues": []string{"#12 login fails"}}, nil
	})
	ctx := toolpacks.WithOwner(context.Background(), "emp-1")
	started, err := c.Start(ctx, contracts.StartRunRequest{AgentID: "issue-triage", AgentVersion: "1.0.0", Input: json.RawMessage(`{"repo":"web"}`), Limits: openLimits()})
	if err != nil {
		t.Fatal(err)
	}
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateSucceeded {
		t.Fatalf("run=%+v", run)
	}
	if gotOwner != "emp-1" || gotTool != "github.list_issues" || gotArgs != `{"owner":"acme","repo":"web"}` {
		t.Fatalf("owner=%q tool=%q args=%s", gotOwner, gotTool, gotArgs)
	}
	if len(run.ToolInvocations) != 1 || run.ToolInvocations[0].ToolID != "github.list_issues" || run.ToolInvocations[0].Error != nil {
		t.Fatalf("tools=%+v", run.ToolInvocations)
	}
	backend.mu.Lock()
	prompts := backend.prompts
	backend.mu.Unlock()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Open issues (github.list_issues)") || !strings.Contains(prompts[0], "#12 login fails") {
		t.Fatalf("prompts=%v", prompts)
	}
}

func TestFailedContextStepFailsTheRunWithoutPrompting(t *testing.T) {
	backend := &fakeBackend{outcome: "succeeded"}
	c, mem := contextCoordinator(t, backend, []contracts.ContextStep{{ID: "issues", Name: "Open issues", Tool: "github.list_issues", Arguments: json.RawMessage(`{"owner":"acme","repo":"web"}`)}})
	c.SetToolCaller(func(context.Context, string, string, json.RawMessage) (any, error) {
		return nil, errors.New("github is not connected")
	})
	started, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "issue-triage", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: openLimits()})
	if err != nil {
		t.Fatal(err)
	}
	run := waitRun(t, mem, started.ID)
	if run.State != contracts.StateFailed || run.Error == nil || !strings.Contains(run.Error.Message, "not connected") {
		t.Fatalf("run=%+v", run)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.prompts) != 0 {
		t.Fatalf("prompted after a failed context step: %v", backend.prompts)
	}
}

func TestContextStepsNeedAToolCaller(t *testing.T) {
	c, _ := contextCoordinator(t, &fakeBackend{}, []contracts.ContextStep{{ID: "issues", Name: "Open issues", Tool: "github.list_issues"}})
	_, err := c.Start(context.Background(), contracts.StartRunRequest{AgentID: "issue-triage", AgentVersion: "1.0.0", Input: json.RawMessage(`{}`), Limits: openLimits()})
	var runErr *contracts.RunError
	if !errors.As(err, &runErr) || runErr.Code != contracts.ErrInvalidInput {
		t.Fatalf("err=%v", err)
	}
}
