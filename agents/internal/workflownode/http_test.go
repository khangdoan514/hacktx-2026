package workflownode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// newIndependentDouble is the Executor stand-in for HTTP tests. Each session
// is one node invocation and finishes on its own.
func newIndependentDouble(sessions map[string]*fakeSession) *fakeExec {
	if sessions == nil {
		sessions = map[string]*fakeSession{}
	}
	return &fakeExec{sessions: sessions}
}

func TestHTTPJoinWaitsForBothSchemaValidOutputs(t *testing.T) {
	gateA := make(chan struct{})
	gateB := make(chan struct{})
	releaseA := releaseGate(gateA)
	releaseB := releaseGate(gateB)
	defer releaseA()
	defer releaseB()

	sessionA := &fakeSession{runID: "agent-run-a", hold: gateA, actions: []ProposedAction{finishAction(leakOutput, []ArtifactRef{{ID: "art-a", ObjectKey: "tenant-a/out/a", ByteSize: 5}})}}
	sessionB := &fakeSession{runID: "agent-run-b", hold: gateB, actions: []ProposedAction{finishAction("beta", nil)}}
	exec := newIndependentDouble(map[string]*fakeSession{"node-a": sessionA, "node-b": sessionB})
	cat := fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract"), "summarize@1.0.0": testDef("summarize")}}
	svc := newService(t, cat, exec)
	srv := httptest.NewServer(NewHandler(svc))
	defer srv.Close()

	reqA := testRequest("extract", "node-a", "extract")
	reqB := testRequest("summarize", "node-b", "summarize")
	reqB.Input = json.RawMessage(`{"task":"summarize"}`)
	bodyA := mustJSON(t, reqA)
	bodyB := mustJSON(t, reqB)
	doneA := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", bodyA)
	doneB := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", bodyB)
	waitHTTPRunning(t, srv, "node-a", "node-b")

	status, raw := httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/join", mustJSON(t, joinRequest{NodeInvocationIDs: []string{"node-b", "node-a"}}))
	if status != http.StatusConflict || decodeErr(t, raw).Code != CodeJoinNotReady || strings.Contains(string(raw), `"nodes"`) {
		t.Fatalf("early join %d %s", status, raw)
	}

	releaseA()
	waitHTTPState(t, srv, "node-a", contracts.StateSucceeded)
	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/join", mustJSON(t, joinRequest{NodeInvocationIDs: []string{"node-a", "node-b"}}))
	if status != http.StatusConflict || decodeErr(t, raw).Code != CodeJoinNotReady {
		t.Fatalf("partial join %d %s", status, raw)
	}

	releaseB()
	resultA := awaitHTTP(t, doneA)
	resultB := awaitHTTP(t, doneB)
	invA := decodeInv(t, resultA.raw)
	invB := decodeInv(t, resultB.raw)
	if resultA.status != http.StatusOK || resultB.status != http.StatusOK || invA.State != contracts.StateSucceeded || invB.State != contracts.StateSucceeded {
		t.Fatalf("a %d %+v b %d %+v", resultA.status, invA, resultB.status, invB)
	}
	if !invA.OutputValidated || !invB.OutputValidated || invA.Correlation.AgentRunID != "agent-run-a" || invB.Correlation.AgentRunID != "agent-run-b" {
		t.Fatalf("validated runs %+v %+v", invA.Correlation, invB.Correlation)
	}
	if exec.openCount() != 2 {
		t.Fatalf("opens %d", exec.openCount())
	}

	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/join", mustJSON(t, joinRequest{NodeInvocationIDs: []string{"node-b", "node-a"}}))
	if status != http.StatusOK {
		t.Fatalf("join %d %s", status, raw)
	}
	var joined JoinedOutput
	if err := json.Unmarshal(raw, &joined); err != nil {
		t.Fatal(err)
	}
	if joined.WorkflowRunID != "wf-run-1" || joined.WorkflowVersion != "4" || joined.TraceID != "trace-1" || len(joined.Nodes) != 2 {
		t.Fatalf("%+v", joined)
	}
	if joined.Nodes[0].NodeID != "extract" || joined.Nodes[1].NodeID != "summarize" {
		t.Fatalf("order %+v", joined.Nodes)
	}
	if string(joined.Nodes[0].Output) != `{"result":"`+leakOutput+`"}` || string(joined.Nodes[1].Output) != `{"result":"beta"}` {
		t.Fatalf("outputs %s %s", joined.Nodes[0].Output, joined.Nodes[1].Output)
	}
	if joined.Nodes[0].AgentRunID != "agent-run-a" || len(joined.Nodes[0].Artifacts) != 1 {
		t.Fatalf("joined node %+v", joined.Nodes[0])
	}

	status, raw = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/events", nil)
	if status != http.StatusOK {
		t.Fatalf("events %d %s", status, raw)
	}
	var page eventsResponse
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if page.WorkflowRunID != "wf-run-1" || len(page.Events) < 2 {
		t.Fatalf("%+v", page)
	}
	blob := string(raw)
	if strings.Contains(blob, leakText) || strings.Contains(blob, leakOutput) || strings.Contains(blob, leakTool) {
		t.Fatalf("event payload leaked %s", blob)
	}
	seen := map[string]bool{}
	var previous uint64
	for _, event := range page.Events {
		if event.Sequence != previous+1 {
			t.Fatalf("sequence %d after %d", event.Sequence, previous)
		}
		previous = event.Sequence
		if event.Correlation.WorkflowRunID != "wf-run-1" || event.Correlation.TraceID != "trace-1" {
			t.Fatalf("correlation %+v", event.Correlation)
		}
		seen[event.Correlation.NodeID+"/"+string(event.Type)] = true
	}
	if !seen["extract/"+string(EventSucceeded)] || !seen["summarize/"+string(EventSucceeded)] {
		t.Fatalf("events %+v", page.Events)
	}
	status, raw = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/events?after="+strconv.FormatUint(page.Events[0].Sequence, 10), nil)
	if status != http.StatusOK {
		t.Fatalf("cursor %d %s", status, raw)
	}
	var rest eventsResponse
	if err := json.Unmarshal(raw, &rest); err != nil {
		t.Fatal(err)
	}
	if len(rest.Events) != len(page.Events)-1 || rest.Events[0].Sequence != page.Events[1].Sequence {
		t.Fatalf("cursor page %+v", rest.Events)
	}
}

func TestHTTPInvalidOutputIsExcludedFromJoin(t *testing.T) {
	sessionA := &fakeSession{runID: "agent-run-a", actions: []ProposedAction{finishAction("ok", nil)}}
	sessionB := &fakeSession{runID: "agent-run-b", actions: []ProposedAction{{Kind: ActionFinish, Output: json.RawMessage(`{"wrong":true}`)}}}
	exec := newIndependentDouble(map[string]*fakeSession{"node-a": sessionA, "node-b": sessionB})
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract"), "summarize@1.0.0": testDef("summarize")}}, exec)
	srv := httptest.NewServer(NewHandler(svc))
	defer srv.Close()

	doneA := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, testRequest("extract", "node-a", "extract")))
	reqB := testRequest("summarize", "node-b", "summarize")
	doneB := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, reqB))
	resultA := awaitHTTP(t, doneA)
	resultB := awaitHTTP(t, doneB)
	invA := decodeInv(t, resultA.raw)
	invB := decodeInv(t, resultB.raw)
	if resultA.status != http.StatusOK || invA.State != contracts.StateSucceeded || !invA.OutputValidated {
		t.Fatalf("a %d %+v", resultA.status, invA)
	}
	if resultB.status != http.StatusOK || invB.State != contracts.StateFailed || invB.Error == nil || invB.Error.Code != CodeOutputSchemaInvalid || invB.OutputValidated {
		t.Fatalf("b %d %+v", resultB.status, invB)
	}
	if len(sessionB.commits()) != 0 {
		t.Fatal("invalid output was committed")
	}
	status, raw := httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/join", mustJSON(t, joinRequest{NodeInvocationIDs: []string{"node-a", "node-b"}}))
	if status != http.StatusConflict || decodeErr(t, raw).Code != CodeJoinFailed || strings.Contains(string(raw), `"nodes"`) {
		t.Fatalf("join %d %s", status, raw)
	}
}

func TestHTTPCancelOneLeavesTheOtherRunning(t *testing.T) {
	gateB := make(chan struct{})
	releaseB := releaseGate(gateB)
	defer releaseB()

	entered := make(chan struct{})
	releaseCommit := make(chan struct{})
	defer close(releaseCommit)
	sessionA := &fakeSession{
		runID:       "agent-run-a",
		blockCommit: true,
		entered:     entered,
		release:     releaseCommit,
		actions: []ProposedAction{
			{Kind: ActionTool, Name: "lookup", Input: json.RawMessage(`{"q":"` + leakTool + `"}`)},
			{Kind: ActionTool, Name: "lookup-again", Input: json.RawMessage(`{"q":"next"}`)},
			finishAction("ok", nil),
		},
	}
	sessionB := &fakeSession{runID: "agent-run-b", hold: gateB, actions: []ProposedAction{finishAction("ok", nil)}}
	exec := newIndependentDouble(map[string]*fakeSession{"node-a": sessionA, "node-b": sessionB})
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract"), "summarize@1.0.0": testDef("summarize")}}, exec)
	srv := httptest.NewServer(NewHandler(svc))
	defer srv.Close()

	doneA := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, testRequest("extract", "node-a", "extract")))
	reqB := testRequest("summarize", "node-b", "summarize")
	doneB := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, reqB))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first action did not start")
	}
	waitHTTPState(t, srv, "node-b", contracts.StateRunning)

	status, raw := httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/invocations/node-a/cancel", nil)
	cancelled := decodeInv(t, raw)
	if status != http.StatusOK || cancelled.State != contracts.StateCancelled || cancelled.Error == nil || cancelled.Error.Code != CodeCancelled {
		t.Fatalf("cancel %d %+v", status, cancelled)
	}
	other := httpGetInv(t, srv, "node-b")
	if other.State != contracts.StateRunning {
		t.Fatalf("cancel marked the other invocation %s", other.State)
	}

	resultA := awaitHTTP(t, doneA)
	invA := decodeInv(t, resultA.raw)
	if resultA.status != http.StatusOK || invA.State != contracts.StateCancelled {
		t.Fatalf("invoke a %d %+v", resultA.status, invA)
	}
	commits := sessionA.commits()
	if len(commits) != 1 || commits[0].Name != "lookup" {
		t.Fatalf("commits %+v", commits)
	}
	if len(sessionB.commits()) != 0 {
		t.Fatalf("other commits %+v", sessionB.commits())
	}

	releaseB()
	resultB := awaitHTTP(t, doneB)
	invB := decodeInv(t, resultB.raw)
	if resultB.status != http.StatusOK || invB.State != contracts.StateSucceeded {
		t.Fatalf("b %d %+v", resultB.status, invB)
	}
	again := httpGetInv(t, srv, "node-a")
	if again.State != contracts.StateCancelled {
		t.Fatalf("cancelled invocation became %s", again.State)
	}
	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/join", mustJSON(t, joinRequest{NodeInvocationIDs: []string{"node-a", "node-b"}}))
	if status != http.StatusConflict || decodeErr(t, raw).Code != CodeJoinFailed {
		t.Fatalf("join %d %s", status, raw)
	}

	status, raw = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/events", nil)
	if status != http.StatusOK {
		t.Fatal(status)
	}
	var page eventsResponse
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	started := 0
	var sawCancel, sawASucceed bool
	for _, event := range page.Events {
		if event.Correlation.NodeID == "extract" && event.Type == EventActionStarted {
			started++
		}
		if event.Correlation.NodeID == "extract" && event.Type == EventCancelled {
			sawCancel = true
		}
		if event.Correlation.NodeID == "extract" && event.Type == EventSucceeded {
			sawASucceed = true
		}
	}
	if started != 1 || !sawCancel || sawASucceed {
		t.Fatalf("events %+v", page.Events)
	}
	if strings.Contains(string(raw), leakTool) || strings.Contains(string(raw), leakText) {
		t.Fatalf("event payload leaked %s", raw)
	}

	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/invocations/node-a/cancel", nil)
	second := decodeInv(t, raw)
	if status != http.StatusOK || second.State != contracts.StateCancelled {
		t.Fatalf("second cancel %d %+v", status, second)
	}
}

func TestHTTPUnapprovedProtectedToolDoesNotCommit(t *testing.T) {
	gate := make(chan struct{})
	release := releaseGate(gate)
	defer release()
	protected := &fakeSession{runID: "agent-run-p", actions: []ProposedAction{{
		Kind: ActionTool, Name: "write-record", Protected: true, Input: json.RawMessage(`{"q":"write"}`),
	}}}
	other := &fakeSession{runID: "agent-run-o", hold: gate, actions: []ProposedAction{finishAction("ok", nil)}}
	exec := newIndependentDouble(map[string]*fakeSession{"node-p": protected, "node-o": other})
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract"), "summarize@1.0.0": testDef("summarize")}}, exec)
	srv := httptest.NewServer(NewHandler(svc))
	defer srv.Close()

	reqP := testRequest("extract", "node-p", "extract")
	reqP.Approval = &ApprovalGate{ID: "gate-1", State: ApprovalPending}
	reqO := testRequest("summarize", "node-o", "summarize")
	doneO := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, reqO))
	waitHTTPState(t, srv, "node-o", contracts.StateRunning)

	status, raw := httpCall(t, srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, reqP))
	inv := decodeInv(t, raw)
	if status != http.StatusOK || inv.State != contracts.StateFailed || inv.Error == nil || inv.Error.Code != CodeApprovalRequired {
		t.Fatalf("protected %d %+v %s", status, inv, raw)
	}
	if inv.Approval == nil || inv.Approval.State != ApprovalPending || len(protected.commits()) != 0 {
		t.Fatalf("approval %+v commits %+v", inv.Approval, protected.commits())
	}
	sibling := httpGetInv(t, srv, "node-o")
	if sibling.State != contracts.StateRunning {
		t.Fatalf("sibling state %s", sibling.State)
	}

	release()
	resultO := awaitHTTP(t, doneO)
	invO := decodeInv(t, resultO.raw)
	if resultO.status != http.StatusOK || invO.State != contracts.StateSucceeded || len(protected.commits()) != 0 {
		t.Fatalf("other %d %+v commits %d", resultO.status, invO.State, len(protected.commits()))
	}
	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/join", mustJSON(t, joinRequest{NodeInvocationIDs: []string{"node-p", "node-o"}}))
	if status != http.StatusConflict || decodeErr(t, raw).Code != CodeJoinFailed {
		t.Fatalf("join %d %s", status, raw)
	}
	if httpGetInv(t, srv, "node-p").State != contracts.StateFailed {
		t.Fatal("protected invocation was marked succeeded")
	}
}

func TestHTTPReplayAndConflict(t *testing.T) {
	gate := make(chan struct{})
	release := releaseGate(gate)
	defer release()
	session := &fakeSession{runID: "agent-run-1", hold: gate, actions: []ProposedAction{finishAction("ok", nil)}}
	exec := newIndependentDouble(map[string]*fakeSession{"node-a": session})
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)
	srv := httptest.NewServer(NewHandler(svc))
	defer srv.Close()

	req := testRequest("extract", "node-a", "extract")
	body := mustJSON(t, req)
	done := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", body)
	waitHTTPState(t, srv, "node-a", contracts.StateRunning)

	req.Input = json.RawMessage("{\n  \"task\": \"review\"\n}")
	replayDone := httpAsync(srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, req))
	select {
	case result := <-replayDone:
		t.Fatalf("replay returned before the original finished: %d %s %v", result.status, result.raw, result.err)
	case <-time.After(200 * time.Millisecond):
	}
	if exec.openCount() != 1 {
		t.Fatalf("opens during replay %d", exec.openCount())
	}

	req.Input = json.RawMessage(`{"task":"other"}`)
	status, raw := httpCall(t, srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, req))
	if status != http.StatusConflict || decodeErr(t, raw).Code != CodeConflict {
		t.Fatalf("conflict %d %s", status, raw)
	}
	if strings.Contains(string(raw), "other") || exec.openCount() != 1 {
		t.Fatalf("conflict body %s opens %d", raw, exec.openCount())
	}
	current := httpGetInv(t, srv, "node-a")
	if current.State != contracts.StateRunning || string(current.Input) != `{"task":"review"}` {
		t.Fatalf("stored %+v input %s", current.State, current.Input)
	}

	release()
	first := awaitHTTP(t, done)
	replay := awaitHTTP(t, replayDone)
	inv := decodeInv(t, first.raw)
	again := decodeInv(t, replay.raw)
	if first.status != http.StatusOK || replay.status != http.StatusOK {
		t.Fatalf("first %d replay %d", first.status, replay.status)
	}
	if inv.Correlation.AgentRunID != "agent-run-1" || again.Correlation.AgentRunID != inv.Correlation.AgentRunID || string(again.Input) != `{"task":"review"}` {
		t.Fatalf("replay %+v %+v", inv.Correlation, again)
	}
	if exec.openCount() != 1 || len(session.commits()) != 1 {
		t.Fatalf("opens %d commits %+v", exec.openCount(), session.commits())
	}

	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/invocations/node-a/cancel", nil)
	terminal := decodeInv(t, raw)
	if status != http.StatusOK || terminal.State != contracts.StateSucceeded || !terminal.OutputValidated {
		t.Fatalf("terminal cancel %d %+v", status, terminal)
	}
}

func TestHTTPValidationAndMount(t *testing.T) {
	session := &fakeSession{runID: "agent-run-1", actions: []ProposedAction{finishAction("ok", nil)}}
	exec := newIndependentDouble(map[string]*fakeSession{"node-a": session})
	svc := newService(t, fakeCatalog{defs: map[string]contracts.AgentDefinition{"extract@1.0.0": testDef("extract")}}, exec)

	mux := http.NewServeMux()
	mux.Handle("/v1/workflow-node/", NewHandler(svc))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	status, raw := httpCall(t, srv, http.MethodGet, "/v1/agents", nil)
	if status != http.StatusNoContent {
		t.Fatalf("sibling %d %s", status, raw)
	}

	var extra map[string]any
	if err := json.Unmarshal(mustJSON(t, testRequest("extract", "node-a", "extract")), &extra); err != nil {
		t.Fatal(err)
	}
	extra["graph"] = map[string]any{"operation": "add"}
	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/invocations", mustJSON(t, extra))
	if status != http.StatusBadRequest || decodeErr(t, raw).Code != CodeInvalidInput || exec.openCount() != 0 {
		t.Fatalf("unknown field %d %s opens %d", status, raw, exec.openCount())
	}

	status, raw = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/invocations/missing", nil)
	if status != http.StatusNotFound || decodeErr(t, raw).Code != CodeNotFound {
		t.Fatalf("get %d %s", status, raw)
	}
	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/invocations/missing/cancel", nil)
	if status != http.StatusNotFound || decodeErr(t, raw).Code != CodeNotFound {
		t.Fatalf("cancel %d %s", status, raw)
	}
	status, raw = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/runs/missing-run/events", nil)
	if status != http.StatusOK {
		t.Fatalf("events %d %s", status, raw)
	}
	var empty eventsResponse
	if err := json.Unmarshal(raw, &empty); err != nil || empty.WorkflowRunID != "missing-run" || len(empty.Events) != 0 || strings.Contains(string(raw), "null") {
		t.Fatalf("empty events %s %v", raw, err)
	}
	status, raw = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/events?after=nope", nil)
	if status != http.StatusBadRequest || decodeErr(t, raw).Code != CodeInvalidInput {
		t.Fatalf("cursor %d %s", status, raw)
	}
	status, raw = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/events?after=1&after=2", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("duplicate cursor %d %s", status, raw)
	}
	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/join", mustJSON(t, joinRequest{}))
	if status != http.StatusBadRequest || decodeErr(t, raw).Code != CodeInvalidInput {
		t.Fatalf("empty join %d %s", status, raw)
	}
	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/join", []byte(`{"node_invocation_ids":["a"],"edges":[]}`))
	if status != http.StatusBadRequest || decodeErr(t, raw).Code != CodeInvalidInput {
		t.Fatalf("join field %d %s", status, raw)
	}
	status, raw = httpCall(t, srv, http.MethodPost, "/v1/workflow-node/runs/wf-run-1/invocations/node-a/cancel", []byte(`{}`))
	if status != http.StatusBadRequest || decodeErr(t, raw).Code != CodeInvalidInput {
		t.Fatalf("cancel body %d %s", status, raw)
	}
	status, _ = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/invocations", nil)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("method %d", status)
	}
	status, raw = httpCall(t, srv, http.MethodGet, "/v1/workflow-node/missing", nil)
	if status != http.StatusNotFound || decodeErr(t, raw).Code != CodeNotFound {
		t.Fatalf("missing route %d %s", status, raw)
	}

	nilSrv := httptest.NewServer(NewHandler(nil))
	defer nilSrv.Close()
	status, raw = httpCall(t, nilSrv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/events", nil)
	if status != http.StatusInternalServerError || decodeErr(t, raw).Code != CodeInternal {
		t.Fatalf("nil service %d %s", status, raw)
	}
}

type httpResult struct {
	status int
	raw    []byte
	err    error
}

func httpAsync(srv *httptest.Server, method, path string, body []byte) <-chan httpResult {
	ch := make(chan httpResult, 1)
	go func() {
		status, raw, err := httpDo(srv, method, path, body)
		ch <- httpResult{status: status, raw: raw, err: err}
	}()
	return ch
}

func awaitHTTP(t *testing.T, ch <-chan httpResult) httpResult {
	t.Helper()
	select {
	case result := <-ch:
		if result.err != nil {
			t.Fatal(result.err)
		}
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("http call timed out")
		return httpResult{}
	}
}

func httpCall(t *testing.T, srv *httptest.Server, method, path string, body []byte) (int, []byte) {
	t.Helper()
	status, raw, err := httpDo(srv, method, path, body)
	if err != nil {
		t.Fatal(err)
	}
	return status, raw
}

func httpDo(srv *httptest.Server, method, path string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, raw, err
	}
	return res.StatusCode, raw, nil
}

func httpGetInv(t *testing.T, srv *httptest.Server, nodeInvocationID string) Invocation {
	t.Helper()
	status, raw := httpCall(t, srv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/invocations/"+nodeInvocationID, nil)
	if status != http.StatusOK {
		t.Fatalf("get %s %d %s", nodeInvocationID, status, raw)
	}
	return decodeInv(t, raw)
}

func waitHTTPRunning(t *testing.T, srv *httptest.Server, ids ...string) {
	t.Helper()
	for _, id := range ids {
		waitHTTPState(t, srv, id, contracts.StateRunning)
	}
}

func waitHTTPState(t *testing.T, srv *httptest.Server, nodeInvocationID string, state contracts.State) {
	t.Helper()
	waitFor(t, func() bool {
		status, raw, err := httpDo(srv, http.MethodGet, "/v1/workflow-node/runs/wf-run-1/invocations/"+nodeInvocationID, nil)
		if err != nil || status != http.StatusOK {
			return false
		}
		var inv Invocation
		if json.Unmarshal(raw, &inv) != nil {
			return false
		}
		return inv.State == state
	})
}

func decodeInv(t *testing.T, raw []byte) Invocation {
	t.Helper()
	var inv Invocation
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatalf("invocation json: %v %s", err, raw)
	}
	return inv
}

func decodeErr(t *testing.T, raw []byte) Error {
	t.Helper()
	var body errorResponse
	if err := json.Unmarshal(raw, &body); err != nil || body.Error == nil {
		t.Fatalf("error json: %v %s", err, raw)
	}
	return *body.Error
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
