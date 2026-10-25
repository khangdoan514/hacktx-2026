package workflownode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

const (
	inputSchema  = `{"type":"object","properties":{"task":{"type":"string"}},"required":["task"],"additionalProperties":false}`
	outputSchema = `{"type":"object","properties":{"result":{"type":"string"}},"required":["result"],"additionalProperties":false}`
	leakText     = "DO-NOT-LEAK-INSTRUCTION"
	leakTool     = "DO-NOT-LEAK-TOOL-INPUT"
	leakOutput   = "alpha-secret-output"
)

func testLimits() contracts.Limits {
	return contracts.Limits{
		MaxDelegations:      2,
		MaxConcurrentAgents: 2,
		MaxToolCalls:        4,
		MaxModelTurns:       4,
		MaxTokens:           100,
		MaxCostUSD:          1,
		TimeoutSeconds:      30,
	}
}

func testDef(id string) contracts.AgentDefinition {
	return contracts.AgentDefinition{
		ID:           id,
		Version:      "1.0.0",
		Name:         id,
		Role:         "worker",
		Instructions: leakText,
		InputSchema:  json.RawMessage(inputSchema),
		OutputSchema: json.RawMessage(outputSchema),
		Limits:       testLimits(),
	}
}

func testIdentity() PolicyIdentity {
	return PolicyIdentity{TenantID: "tenant-a", PrincipalID: "user-1", PolicyID: "policy-1", PolicyVersion: "3"}
}

func testRequest(nodeID, invocationID, agentID string) InvokeRequest {
	return InvokeRequest{
		Correlation: Correlation{
			WorkflowID:       "wf-review",
			WorkflowVersion:  "4",
			WorkflowRunID:    "wf-run-1",
			NodeID:           nodeID,
			NodeInvocationID: invocationID,
			TraceID:          "trace-1",
		},
		Identity:     testIdentity(),
		AgentID:      agentID,
		AgentVersion: "1.0.0",
		Input:        json.RawMessage(`{"task":"review"}`),
		Artifacts:    []ArtifactRef{{ID: "art-in", ObjectKey: "tenant-a/wf-run-1/art-in", ContentType: "text/plain", ByteSize: 3}},
		Limits:       testLimits(),
	}
}

type fakeCatalog struct {
	defs map[string]contracts.AgentDefinition
}

func (f fakeCatalog) Resolve(_ context.Context, id, version string) (contracts.AgentDefinition, error) {
	def, ok := f.defs[id+"@"+version]
	if !ok {
		return contracts.AgentDefinition{}, &Error{Code: CodeNotFound, Message: "agent version not found"}
	}
	return def, nil
}

type mismatchCatalog struct{}

func (mismatchCatalog) Resolve(context.Context, string, string) (contracts.AgentDefinition, error) {
	def := testDef("extract")
	def.Version = "2.0.0"
	return def, nil
}

type fakeExec struct {
	mu       sync.Mutex
	sessions map[string]*fakeSession
	opens    int
}

func (e *fakeExec) Open(ctx context.Context, req ExecuteRequest) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	session := e.sessions[req.Correlation.NodeInvocationID]
	if session == nil {
		return nil, &Error{Code: CodeInternal, Message: "no fake session"}
	}
	if session.opened {
		return nil, &Error{Code: CodeInternal, Message: "session opened twice"}
	}
	session.opened = true
	saved := req
	saved.Approval = cloneApproval(req.Approval)
	saved.Input = cloneRaw(req.Input)
	saved.Artifacts = cloneArtifacts(req.Artifacts)
	session.request = saved
	if session.mutate != nil {
		session.mutate(&req)
	}
	e.opens++
	return session, nil
}

func (e *fakeExec) openCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.opens
}

type fakeSession struct {
	mu          sync.Mutex
	runID       string
	actions     []ProposedAction
	next        int
	opened      bool
	request     ExecuteRequest
	mutate      func(*ExecuteRequest)
	committed   []ProposedAction
	hold        <-chan struct{}
	blockCommit bool
	entered     chan struct{}
	release     <-chan struct{}
	deny        error
}

func (s *fakeSession) RunID() string { return s.runID }

func (s *fakeSession) Next(ctx context.Context) (ProposedAction, error) {
	s.mu.Lock()
	hold := s.hold
	s.hold = nil
	s.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return ProposedAction{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return ProposedAction{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next >= len(s.actions) {
		return ProposedAction{}, &Error{Code: CodeInvalidInput, Message: "pinned agent produced no output"}
	}
	action := s.actions[s.next]
	s.next++
	return action, nil
}

func (s *fakeSession) Commit(ctx context.Context, action ProposedAction) error {
	s.mu.Lock()
	s.committed = append(s.committed, action)
	block := s.blockCommit && len(s.committed) == 1
	entered := s.entered
	release := s.release
	deny := s.deny
	s.mu.Unlock()
	if block {
		if entered != nil {
			close(entered)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deny != nil {
		return deny
	}
	return nil
}

func (s *fakeSession) commits() []ProposedAction {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ProposedAction(nil), s.committed...)
}

type callResult struct {
	inv Invocation
	err error
}

func startInvoke(svc *Service, ctx context.Context, req InvokeRequest) <-chan callResult {
	ch := make(chan callResult, 1)
	go func() {
		inv, err := svc.Invoke(ctx, req)
		ch <- callResult{inv: inv, err: err}
	}()
	return ch
}

func newService(t *testing.T, cat Catalog, exec Executor) *Service {
	t.Helper()
	svc, err := New(cat, exec, NewMemoryLog(), func() time.Time {
		return time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func codeOfErr(err error) Code {
	var nodeErr *Error
	if errors.As(err, &nodeErr) && nodeErr != nil {
		return nodeErr.Code
	}
	return ""
}

func releaseGate(ch chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() { close(ch) })
	}
}

func finishAction(result string, artifacts []ArtifactRef) ProposedAction {
	return ProposedAction{Kind: ActionFinish, Output: json.RawMessage(`{"result":"` + result + `"}`), Artifacts: artifacts}
}

func TestParallelJoinWaitsForBothValidatedOutputs(t *testing.T) {
	gateA := make(chan struct{})
	gateB := make(chan struct{})
	releaseA := releaseGate(gateA)
	releaseB := releaseGate(gateB)
	defer releaseA()
	defer releaseB()

	sessionA := &fakeSession{runID: "agent-run-a", hold: gateA, actions: []ProposedAction{finishAction(leakOutput, []ArtifactRef{{ID: "art-a", ObjectKey: "tenant-a/out/a", ByteSize: 5}})}}
	sessionB := &fakeSession{runID: "agent-run-b", hold: gateB, actions: []ProposedAction{finishAction("beta", nil)}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": sessionA, "node-b": sessionB}}
	cat := fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract"), "summarize@1.0.0": testDef("summarize")}}
	svc := newService(t, cat, exec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reqA := testRequest("extract", "node-a", "extract")
	reqB := testRequest("summarize", "node-b", "summarize")
	reqB.Input = json.RawMessage(`{"task":"summarize"}`)
	doneA := startInvoke(svc, ctx, reqA)
	doneB := startInvoke(svc, ctx, reqB)

	waitFor(t, func() bool {
		a, errA := svc.Get(ctx, "wf-run-1", "node-a")
		b, errB := svc.Get(ctx, "wf-run-1", "node-b")
		return errA == nil && errB == nil && a.State == contracts.StateRunning && b.State == contracts.StateRunning
	})
	if out, err := svc.Join(ctx, "wf-run-1", []string{"node-b", "node-a"}); codeOfErr(err) != CodeJoinNotReady || len(out.Nodes) != 0 {
		t.Fatalf("early join %+v %v", out, err)
	}

	releaseA()
	waitFor(t, func() bool {
		inv, err := svc.Get(ctx, "wf-run-1", "node-a")
		return err == nil && inv.State == contracts.StateSucceeded && inv.OutputValidated
	})
	if out, err := svc.Join(ctx, "wf-run-1", []string{"node-a", "node-b"}); codeOfErr(err) != CodeJoinNotReady || len(out.Nodes) != 0 {
		t.Fatalf("partial join %+v %v", out, err)
	}

	releaseB()
	resultA := <-doneA
	resultB := <-doneB
	if resultA.err != nil || resultB.err != nil {
		t.Fatalf("a %v b %v", resultA.err, resultB.err)
	}
	if resultA.inv.Correlation.AgentRunID != "agent-run-a" || resultB.inv.Correlation.AgentRunID != "agent-run-b" {
		t.Fatalf("run ids %s %s", resultA.inv.Correlation.AgentRunID, resultB.inv.Correlation.AgentRunID)
	}
	if resultA.inv.AgentVersion != "1.0.0" || resultB.inv.AgentVersion != "1.0.0" {
		t.Fatal("version pin lost")
	}
	if resultA.inv.Identity != testIdentity() || resultB.inv.Approval != nil {
		t.Fatalf("identity %+v approval %+v", resultA.inv.Identity, resultB.inv.Approval)
	}
	if sessionA.request.Correlation.AgentRunID != "" || sessionA.request.Definition.Version != "1.0.0" || sessionA.request.Identity.TenantID != "tenant-a" {
		t.Fatalf("executor request %+v", sessionA.request.Correlation)
	}
	if len(sessionA.request.Artifacts) != 1 || sessionA.request.Artifacts[0].ID != "art-in" {
		t.Fatalf("input artifacts %+v", sessionA.request.Artifacts)
	}

	out, err := svc.Join(ctx, "wf-run-1", []string{"node-b", "node-a"})
	if err != nil {
		t.Fatal(err)
	}
	if out.WorkflowRunID != "wf-run-1" || out.WorkflowVersion != "4" || out.TraceID != "trace-1" || len(out.Nodes) != 2 {
		t.Fatalf("%+v", out)
	}
	if out.Nodes[0].NodeID != "extract" || out.Nodes[1].NodeID != "summarize" {
		t.Fatalf("order %+v", out.Nodes)
	}
	if string(out.Nodes[0].Output) != `{"result":"`+leakOutput+`"}` || string(out.Nodes[1].Output) != `{"result":"beta"}` {
		t.Fatalf("outputs %s %s", out.Nodes[0].Output, out.Nodes[1].Output)
	}
	if out.Nodes[0].AgentRunID != "agent-run-a" || len(out.Nodes[0].Artifacts) != 1 || out.Nodes[0].Artifacts[0].ID != "art-a" {
		t.Fatalf("joined node %+v", out.Nodes[0])
	}

	events, err := svc.Events(ctx, "wf-run-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var previous uint64
	blob, _ := json.Marshal(events)
	if strings.Contains(string(blob), leakText) || strings.Contains(string(blob), leakOutput) {
		t.Fatalf("event payload leaked %s", blob)
	}
	for _, event := range events {
		if event.Correlation.WorkflowRunID != "wf-run-1" || event.Correlation.TraceID != "trace-1" || event.Correlation.WorkflowVersion != "4" {
			t.Fatalf("correlation %+v", event.Correlation)
		}
		if event.Sequence != previous+1 {
			t.Fatalf("sequence %d after %d", event.Sequence, previous)
		}
		previous = event.Sequence
		if event.Timestamp.Location() != time.UTC {
			t.Fatal("timestamp not UTC")
		}
		seen[event.Correlation.NodeID+"/"+string(event.Type)] = true
		if event.Type == EventStarted && event.Correlation.AgentRunID == "" {
			t.Fatal("started event missing agent run id")
		}
	}
	if !seen["extract/"+string(EventSucceeded)] || !seen["summarize/"+string(EventSucceeded)] {
		t.Fatalf("events %+v", events)
	}
}

func TestJoinRejectsInvalidOutput(t *testing.T) {
	gateA := make(chan struct{})
	gateB := make(chan struct{})
	releaseA := releaseGate(gateA)
	releaseB := releaseGate(gateB)
	defer releaseA()
	defer releaseB()
	sessionA := &fakeSession{runID: "agent-run-a", hold: gateA, actions: []ProposedAction{finishAction("ok", nil)}}
	sessionB := &fakeSession{runID: "agent-run-b", hold: gateB, actions: []ProposedAction{{Kind: ActionFinish, Output: json.RawMessage(`{"wrong":true}`)}}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": sessionA, "node-b": sessionB}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract"), "summarize@1.0.0": testDef("summarize")}}, exec)
	ctx := context.Background()
	doneA := startInvoke(svc, ctx, testRequest("extract", "node-a", "extract"))
	doneB := startInvoke(svc, ctx, testRequest("summarize", "node-b", "summarize"))
	waitFor(t, func() bool {
		a, _ := svc.Get(ctx, "wf-run-1", "node-a")
		b, _ := svc.Get(ctx, "wf-run-1", "node-b")
		return a.State == contracts.StateRunning && b.State == contracts.StateRunning
	})
	releaseA()
	releaseB()
	resultA := <-doneA
	resultB := <-doneB
	if resultA.err != nil || resultA.inv.State != contracts.StateSucceeded {
		t.Fatalf("a %+v %v", resultA.inv.State, resultA.err)
	}
	if codeOfErr(resultB.err) != CodeOutputSchemaInvalid || resultB.inv.State != contracts.StateFailed || resultB.inv.OutputValidated {
		t.Fatalf("b %+v %v", resultB.inv, resultB.err)
	}
	if len(sessionB.commits()) != 0 {
		t.Fatal("invalid output was committed")
	}
	out, err := svc.Join(ctx, "wf-run-1", []string{"node-a", "node-b"})
	if codeOfErr(err) != CodeJoinFailed || len(out.Nodes) != 0 {
		t.Fatalf("join %+v %v", out, err)
	}
}

func TestCancelStopsFurtherActions(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	session := &fakeSession{
		runID:       "agent-run-1",
		blockCommit: true,
		entered:     entered,
		release:     release,
		actions: []ProposedAction{
			{Kind: ActionTool, Name: "lookup", Input: json.RawMessage(`{"q":"` + leakTool + `"}`)},
			{Kind: ActionTool, Name: "lookup-again", Input: json.RawMessage(`{"q":"next"}`)},
			finishAction("ok", nil),
		},
	}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startInvoke(svc, ctx, testRequest("extract", "node-a", "extract"))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first action did not start")
	}
	cancelled, err := svc.Cancel(ctx, "wf-run-1", "node-a")
	if codeOfErr(err) != CodeCancelled || cancelled.State != contracts.StateCancelled {
		t.Fatalf("cancel %+v %v", cancelled, err)
	}
	result := <-done
	if result.inv.State != contracts.StateCancelled || codeOfErr(result.err) != CodeCancelled {
		t.Fatalf("invoke %+v %v", result.inv.State, result.err)
	}
	commits := session.commits()
	if len(commits) != 1 || commits[0].Name != "lookup" {
		t.Fatalf("commits %+v", commits)
	}
	events, err := svc.Events(ctx, "wf-run-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	started := 0
	for _, event := range events {
		if event.Type == EventActionStarted {
			started++
		}
		if event.Type == EventSucceeded {
			t.Fatal("cancelled invocation succeeded")
		}
	}
	if started != 1 || events[len(events)-1].Type != EventCancelled {
		t.Fatalf("events %+v", events)
	}
	blob, _ := json.Marshal(events)
	if strings.Contains(string(blob), leakTool) || strings.Contains(string(blob), leakText) {
		t.Fatalf("event payload leaked %s", blob)
	}
	again, err := svc.Cancel(ctx, "wf-run-1", "node-a")
	if err != nil || again.State != contracts.StateCancelled {
		t.Fatalf("second cancel %+v %v", again, err)
	}
}

func TestParallelCancelBlocksJoin(t *testing.T) {
	gateA := make(chan struct{})
	gateB := make(chan struct{})
	releaseA := releaseGate(gateA)
	releaseB := releaseGate(gateB)
	defer releaseA()
	defer releaseB()
	sessionA := &fakeSession{runID: "agent-run-a", hold: gateA, actions: []ProposedAction{finishAction("ok", nil)}}
	sessionB := &fakeSession{runID: "agent-run-b", hold: gateB, actions: []ProposedAction{finishAction("ok", nil)}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": sessionA, "node-b": sessionB}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract"), "summarize@1.0.0": testDef("summarize")}}, exec)
	ctx := context.Background()
	doneA := startInvoke(svc, ctx, testRequest("extract", "node-a", "extract"))
	doneB := startInvoke(svc, ctx, testRequest("summarize", "node-b", "summarize"))
	waitFor(t, func() bool {
		b, err := svc.Get(ctx, "wf-run-1", "node-b")
		return err == nil && b.State == contracts.StateRunning
	})
	if _, err := svc.Cancel(ctx, "wf-run-1", "node-b"); codeOfErr(err) != CodeCancelled {
		t.Fatal(err)
	}
	resultB := <-doneB
	if resultB.inv.State != contracts.StateCancelled || len(sessionB.commits()) != 0 {
		t.Fatalf("b %+v commits %+v", resultB.inv.State, sessionB.commits())
	}
	releaseA()
	resultA := <-doneA
	if resultA.err != nil {
		t.Fatal(resultA.err)
	}
	out, err := svc.Join(ctx, "wf-run-1", []string{"node-a", "node-b"})
	if codeOfErr(err) != CodeJoinFailed || len(out.Nodes) != 0 {
		t.Fatalf("join %+v %v", out, err)
	}
}

func TestGraphMutationAndApprovalBypass(t *testing.T) {
	graph := &fakeSession{runID: "agent-run-1", actions: []ProposedAction{{
		Kind:   ActionGraphMutation,
		Detail: json.RawMessage(`{"workflow_version":"99","operation":"REMOVE_NODE"}`),
	}}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": graph}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	inv, err := svc.Invoke(context.Background(), testRequest("extract", "node-a", "extract"))
	if codeOfErr(err) != CodeGraphImmutable || inv.State != contracts.StateFailed {
		t.Fatalf("%+v %v", inv, err)
	}
	if inv.Correlation.WorkflowVersion != "4" || len(graph.commits()) != 0 {
		t.Fatalf("version %s commits %+v", inv.Correlation.WorkflowVersion, graph.commits())
	}

	approval := &fakeSession{runID: "agent-run-2", actions: []ProposedAction{{
		Kind:   ActionApprovalDecision,
		Detail: json.RawMessage(`{"state":"APPROVED","decision_principal":"agent"}`),
	}}}
	exec = &fakeExec{sessions: map[string]*fakeSession{"node-b": approval}}
	svc = newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	req := testRequest("extract", "node-b", "extract")
	req.Approval = &ApprovalGate{ID: "gate-1", State: ApprovalPending}
	inv, err = svc.Invoke(context.Background(), req)
	if codeOfErr(err) != CodeApprovalBypass || inv.Approval.State != ApprovalPending || len(approval.commits()) != 0 {
		t.Fatalf("%+v %v commits %d", inv, err, len(approval.commits()))
	}

	override := &fakeSession{runID: "agent-run-3", actions: []ProposedAction{{Kind: ActionPolicyOverride, Detail: json.RawMessage(`{"policy_id":"other"}`)}}}
	exec = &fakeExec{sessions: map[string]*fakeSession{"node-c": override}}
	svc = newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	inv, err = svc.Invoke(context.Background(), testRequest("extract", "node-c", "extract"))
	if codeOfErr(err) != CodePolicyImmutable || inv.Identity != testIdentity() || len(override.commits()) != 0 {
		t.Fatalf("%+v %v", inv.Identity, err)
	}
}

func TestProtectedActionRequiresWorkflowApproval(t *testing.T) {
	gates := []*ApprovalGate{
		nil,
		{ID: "gate-1", State: ApprovalPending},
		{ID: "gate-1", State: ApprovalDenied, DecisionPrincipal: "approver-1"},
		{ID: "gate-1", State: ApprovalExpired, DecisionPrincipal: "approver-1"},
	}
	for i, gate := range gates {
		session := &fakeSession{runID: "agent-run", actions: []ProposedAction{{
			Kind: ActionTool, Name: "write-record", Protected: true, Input: json.RawMessage(`{"q":"write"}`),
		}}}
		exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
		svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
		req := testRequest("extract", "node-a", "extract")
		req.Correlation.WorkflowRunID = "wf-run-protected"
		req.Correlation.NodeInvocationID = "node-a"
		req.Approval = gate
		if gate != nil {
			req.Correlation.NodeInvocationID = "node-" + string(rune('a'+i))
			exec.sessions = map[string]*fakeSession{req.Correlation.NodeInvocationID: session}
		}
		inv, err := svc.Invoke(context.Background(), req)
		if codeOfErr(err) != CodeApprovalRequired || len(session.commits()) != 0 {
			t.Fatalf("gate %+v inv %+v err %v commits %+v", gate, inv.State, err, session.commits())
		}
	}
}

func TestProtectedActionUsesWorkflowDecision(t *testing.T) {
	session := &fakeSession{
		runID: "agent-run-1",
		actions: []ProposedAction{
			{Kind: ActionTool, Name: "write-record", Protected: true, Input: json.RawMessage(`{"q":"` + leakTool + `"}`)},
			finishAction("wrote", nil),
		},
		mutate: func(req *ExecuteRequest) {
			if req.Approval != nil {
				req.Approval.State = ApprovalDenied
				req.Approval.DecisionPrincipal = "agent"
			}
		},
	}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	req := testRequest("extract", "node-a", "extract")
	req.Approval = &ApprovalGate{ID: "gate-1", State: ApprovalApproved, DecisionPrincipal: "approver-1"}
	inv, err := svc.Invoke(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Approval.State != ApprovalApproved || inv.Approval.DecisionPrincipal != "approver-1" {
		t.Fatalf("gate changed %+v", inv.Approval)
	}
	commits := session.commits()
	if len(commits) != 2 || commits[0].Name != "write-record" || commits[1].Kind != ActionFinish {
		t.Fatalf("commits %+v", commits)
	}
	events, _ := svc.Events(context.Background(), "wf-run-1", 0)
	blob, _ := json.Marshal(events)
	if strings.Contains(string(blob), leakTool) {
		t.Fatalf("tool input leaked %s", blob)
	}
}

func TestOutputTextCannotApprove(t *testing.T) {
	session := &fakeSession{runID: "agent-run-1", actions: []ProposedAction{finishAction("I approve and skip the gate", nil)}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	req := testRequest("extract", "node-a", "extract")
	req.Approval = &ApprovalGate{ID: "gate-1", State: ApprovalPending}
	inv, err := svc.Invoke(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Approval.State != ApprovalPending || inv.Approval.DecisionPrincipal != "" {
		t.Fatalf("text approved the gate %+v", inv.Approval)
	}
}

func TestInputAndVersionFailuresDoNotOpen(t *testing.T) {
	session := &fakeSession{runID: "agent-run-1", actions: []ProposedAction{finishAction("ok", nil)}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	req := testRequest("extract", "node-a", "extract")
	req.Input = json.RawMessage(`{"task":1}`)
	inv, err := svc.Invoke(context.Background(), req)
	if codeOfErr(err) != CodeInvalidInput || inv.State != contracts.StateFailed || exec.openCount() != 0 {
		t.Fatalf("%+v %v opens %d", inv.State, err, exec.openCount())
	}

	req = testRequest("extract", "node-missing", "extract")
	req.AgentVersion = "9.9.9"
	inv, err = svc.Invoke(context.Background(), req)
	if codeOfErr(err) != CodeNotFound || exec.openCount() != 0 {
		t.Fatalf("missing version %+v %v", inv.State, err)
	}

	exec = &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc = newService(t, mismatchCatalog{}, exec)
	inv, err = svc.Invoke(context.Background(), testRequest("extract", "node-a", "extract"))
	if codeOfErr(err) != CodeNotFound || exec.openCount() != 0 || inv.AgentVersion != "1.0.0" {
		t.Fatalf("mismatch %+v %v opens %d", inv, err, exec.openCount())
	}

	bad := testDef("extract")
	bad.OutputSchema = json.RawMessage(`{"type":"object","minLength":1}`)
	exec = &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc = newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": bad}}, exec)
	inv, err = svc.Invoke(context.Background(), testRequest("extract", "node-a", "extract"))
	if codeOfErr(err) != CodeInvalidInput || exec.openCount() != 0 {
		t.Fatalf("bad schema %+v %v", inv.State, err)
	}
}

func TestDelegationDepthStaysWithExecutor(t *testing.T) {
	session := &fakeSession{
		runID: "agent-run-1",
		deny:  &contracts.RunError{Code: contracts.ErrDelegationDepthExceeded, Message: "depth 1 cannot delegate"},
		actions: []ProposedAction{{
			Kind: ActionDelegate, Name: "specialist", Input: json.RawMessage(`{"task":"look"}`),
		}},
	}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	inv, err := svc.Invoke(context.Background(), testRequest("extract", "node-a", "extract"))
	if codeOfErr(err) != CodeDelegationDepthExceeded || len(session.commits()) != 1 {
		t.Fatalf("%v commits %+v", err, session.commits())
	}
	if inv.State != contracts.StateFailed {
		t.Fatal(inv.State)
	}
}

func TestReplayDoesNotExecuteTwice(t *testing.T) {
	session := &fakeSession{runID: "agent-run-1", actions: []ProposedAction{finishAction("ok", nil)}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	req := testRequest("extract", "node-a", "extract")
	first, err := svc.Invoke(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Input = json.RawMessage("{\n  \"task\": \"review\"\n}")
	second, err := svc.Invoke(context.Background(), req)
	if err != nil || second.Correlation.AgentRunID != first.Correlation.AgentRunID || exec.openCount() != 1 {
		t.Fatalf("replay %+v %v opens %d", second.Correlation, err, exec.openCount())
	}
	req.Input = json.RawMessage(`{"task":"other"}`)
	_, err = svc.Invoke(context.Background(), req)
	if codeOfErr(err) != CodeConflict || exec.openCount() != 1 {
		t.Fatalf("conflict %v opens %d", err, exec.openCount())
	}
}

func TestCancelBeforeOpenAndAfterSuccess(t *testing.T) {
	session := &fakeSession{runID: "agent-run-1", actions: []ProposedAction{finishAction("ok", nil)}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inv, err := svc.Invoke(ctx, testRequest("extract", "node-a", "extract"))
	if codeOfErr(err) != CodeCancelled || inv.State != contracts.StateCancelled || exec.openCount() != 0 {
		t.Fatalf("%+v %v opens %d", inv.State, err, exec.openCount())
	}

	exec = &fakeExec{sessions: map[string]*fakeSession{"node-b": session}}
	svc = newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	req := testRequest("extract", "node-b", "extract")
	inv, err = svc.Invoke(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, _ := svc.Events(context.Background(), "wf-run-1", 0)
	again, err := svc.Cancel(context.Background(), "wf-run-1", "node-b")
	if err != nil || again.State != contracts.StateSucceeded || !again.OutputValidated {
		t.Fatalf("terminal cancel %+v %v", again, err)
	}
	eventsAfter, _ := svc.Events(context.Background(), "wf-run-1", 0)
	if len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("cancel rewrote history %d %d", len(eventsBefore), len(eventsAfter))
	}
}

func TestArtifactAndIdentityRejection(t *testing.T) {
	session := &fakeSession{runID: "agent-run-1", actions: []ProposedAction{finishAction("ok", []ArtifactRef{{ID: "", ObjectKey: "missing"}})}}
	exec := &fakeExec{sessions: map[string]*fakeSession{"node-a": session}}
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	inv, err := svc.Invoke(context.Background(), testRequest("extract", "node-a", "extract"))
	if codeOfErr(err) != CodeInvalidInput || inv.State != contracts.StateFailed || len(inv.OutputArtifacts) != 0 || len(session.commits()) != 0 {
		t.Fatalf("%+v %v commits %d", inv, err, len(session.commits()))
	}

	req := testRequest("extract", "node-b", "extract")
	req.Artifacts = []ArtifactRef{{ID: "art-1", ObjectKey: ""}}
	if _, err := svc.Invoke(context.Background(), req); codeOfErr(err) != CodeInvalidInput {
		t.Fatal(err)
	}
	req = testRequest("extract", "node-c", "extract")
	req.Identity.PolicyID = ""
	if _, err := svc.Invoke(context.Background(), req); codeOfErr(err) != CodeInvalidInput {
		t.Fatal(err)
	}
	req = testRequest("extract", "node-d", "extract")
	req.Correlation.AgentRunID = "caller-picked"
	if _, err := svc.Invoke(context.Background(), req); codeOfErr(err) != CodeInvalidInput {
		t.Fatal(err)
	}
	req = testRequest("extract", "node-e", "extract")
	req.Approval = &ApprovalGate{ID: "gate-1", State: ApprovalApproved}
	if _, err := svc.Invoke(context.Background(), req); codeOfErr(err) != CodeInvalidInput {
		t.Fatal(err)
	}
}

func TestJoinValidatedRefusesUnvalidatedSuccess(t *testing.T) {
	base := Invocation{
		State:        contracts.StateSucceeded,
		AgentID:      "extract",
		AgentVersion: "1.0.0",
		Correlation: Correlation{
			WorkflowID: "wf", WorkflowVersion: "4", WorkflowRunID: "run", NodeID: "extract", NodeInvocationID: "n1", TraceID: "trace", AgentRunID: "r1",
		},
		Output:          json.RawMessage(`{"result":"ok"}`),
		OutputSchema:    json.RawMessage(outputSchema),
		OutputValidated: false,
	}
	if _, err := JoinValidated("run", []Invocation{base}); codeOfErr(err) != CodeOutputSchemaInvalid {
		t.Fatal(err)
	}
	base.OutputValidated = true
	base.Output = json.RawMessage(`{"nope":true}`)
	if _, err := JoinValidated("run", []Invocation{base}); codeOfErr(err) != CodeOutputSchemaInvalid {
		t.Fatal(err)
	}
	running := base
	running.State = contracts.StateRunning
	running.Output = json.RawMessage(`{"result":"ok"}`)
	other := base
	other.Output = json.RawMessage(`{"result":"ok"}`)
	other.Correlation.NodeID = "summarize"
	if _, err := JoinValidated("run", []Invocation{other, running}); codeOfErr(err) != CodeJoinNotReady {
		t.Fatal(err)
	}
}
