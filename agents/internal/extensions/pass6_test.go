package extensions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

const (
	secretA = "cred-value-tenant-a"
	secretB = "cred-value-tenant-b"
)

func TestThreeAdaptersSharePermissionCheck(t *testing.T) {
	mcpHits := &atomic.Int32{}
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcpHits.Add(1)
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode mcp: %v", err)
			return
		}
		switch req.Method {
		case "initialize":
			writeRPC(w, req.ID, map[string]any{"protocolVersion": "2024-11-05"})
		case "tools/call":
			writeRPC(w, req.ID, map[string]any{"structuredContent": map[string]any{"source": "mcp"}})
		default:
			http.Error(w, "bad", http.StatusBadRequest)
		}
	}))
	defer mcp.Close()

	httpHits := &atomic.Int32{}
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"http"}`))
	}))
	defer httpSrv.Close()

	workerHits := &atomic.Int32{}
	workerAddr := serveTools(t, []ServiceTool{{
		ID: "extensions.worker.sample", Version: "1.0.0", SideEffect: SideEffectReadOnly,
		InputSchema: objectSchema(), OutputSchema: sourceSchema(),
		Handle: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			workerHits.Add(1)
			return json.RawMessage(`{"source":"worker"}`), nil
		},
	}})

	reg := NewRegistry()
	gw := NewGateway(reg, NewMemoryCredentials(), Options{})
	mcpTool := baseTool(ClassMCP, "extensions.mcp.sample", "1.0.0", mcp.URL, hostOf(mcp.URL))
	mcpTool.Endpoint.MCPTool = "sample"
	httpTool := baseTool(ClassHTTP, "extensions.http.internal", "1.0.0", httpSrv.URL, hostOf(httpSrv.URL))
	httpTool.Endpoint.Internal = true
	workerTool := baseTool(ClassWorker, "extensions.worker.sample", "1.0.0", "http://"+workerAddr, []string{workerAddr})
	publish(t, reg, mcpTool)
	publish(t, reg, httpTool)
	publish(t, reg, workerTool)
	if _, err := reg.Pin("run-1", "tenant-a", []string{mcpTool.ID, httpTool.ID, workerTool.ID}); err != nil {
		t.Fatal(err)
	}
	allowed := agent(mcpTool.ID, httpTool.ID, workerTool.ID)
	caller := principal()

	cases := []struct {
		id    string
		hits  *atomic.Int32
		want  string
		class Class
	}{
		{mcpTool.ID, mcpHits, "mcp", ClassMCP},
		{httpTool.ID, httpHits, "http", ClassHTTP},
		{workerTool.ID, workerHits, "worker", ClassWorker},
	}
	for _, tc := range cases {
		before := tc.hits.Load()
		inv := invoke(t, gw, "run-1", tc.id, allowed, caller, `{"query":"vendors"}`, nil)
		if inv.Error != nil {
			t.Fatalf("%s: %s", tc.id, inv.Error)
		}
		if !bytes.Contains(inv.Output, []byte(`"source":"`+tc.want+`"`)) && !bytes.Contains(inv.Output, []byte(`"source": "`+tc.want+`"`)) {
			t.Fatalf("%s output %s", tc.id, inv.Output)
		}
		if inv.ToolVersion != "1.0.0" || inv.Class != tc.class {
			t.Fatalf("%s recorded %+v", tc.id, inv)
		}
		if tc.hits.Load() <= before {
			t.Fatalf("%s did not reach its adapter", tc.id)
		}
		meta, _ := json.Marshal(inv.Metadata())
		if bytes.Contains(meta, []byte("vendors")) || bytes.Contains(meta, []byte("query")) {
			t.Fatalf("%s metadata leaked input %s", tc.id, meta)
		}
		if inv.V1().ToolVersion != "1.0.0" || inv.V1().Error != nil {
			t.Fatalf("%s v1 projection %+v", tc.id, inv.V1())
		}
	}
	if !regMust(t, reg, httpTool).Endpoint.Internal || invoke(t, gw, "run-1", httpTool.ID, allowed, caller, `{"query":"vendors"}`, nil).Internal != true {
		t.Fatal("internal HTTP tool did not stay on the HTTP adapter")
	}

	denies := []struct {
		name    string
		agent   contracts.AgentDefinition
		caller  Principal
		message string
	}{
		{"allowlist", agent(), caller, "tool is not allowed for this agent"},
		{"permission", allowed, Principal{ID: "user-1", TenantID: "tenant-a"}, "tool is not allowed for this agent"},
		{"tenant", allowed, Principal{ID: "user-2", TenantID: "tenant-b", Permissions: []string{"extensions.use"}}, "tool is not allowed for this tenant"},
	}
	for _, deny := range denies {
		t.Run(deny.name, func(t *testing.T) {
			snapshots := []int32{mcpHits.Load(), httpHits.Load(), workerHits.Load()}
			var codes []ErrorCode
			var messages []string
			for _, tc := range cases {
				inv := invoke(t, gw, "run-1", tc.id, deny.agent, deny.caller, `{"query":"vendors"}`, nil)
				if inv.Error == nil {
					t.Fatalf("%s was allowed", tc.id)
				}
				codes = append(codes, inv.Error.Code)
				messages = append(messages, inv.Error.Message)
			}
			after := []int32{mcpHits.Load(), httpHits.Load(), workerHits.Load()}
			if snapshots[0] != after[0] || snapshots[1] != after[1] || snapshots[2] != after[2] {
				t.Fatalf("denied call reached an adapter: before %v after %v", snapshots, after)
			}
			for i := range codes {
				if codes[i] != CodeToolNotAllowed || messages[i] != deny.message {
					t.Fatalf("adapter %d code %s message %s", i, codes[i], messages[i])
				}
			}
		})
	}

	before := []int32{mcpHits.Load(), httpHits.Load(), workerHits.Load()}
	for _, tc := range cases {
		inv := invoke(t, gw, "run-1", tc.id, allowed, caller, `{"query":1}`, nil)
		if inv.Error == nil || inv.Error.Code != CodeInvalidInput {
			t.Fatalf("%s invalid input: %+v", tc.id, inv.Error)
		}
	}
	after := []int32{mcpHits.Load(), httpHits.Load(), workerHits.Load()}
	if before[0] != after[0] || before[1] != after[1] || before[2] != after[2] {
		t.Fatal("invalid input reached an adapter")
	}
}

func TestPinnedVersionDoesNotChangeMidRun(t *testing.T) {
	start := func(source string) (*httptest.Server, *atomic.Int32) {
		hits := &atomic.Int32{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			var req rpcRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Method == "initialize" {
				writeRPC(w, req.ID, map[string]any{"protocolVersion": "2024-11-05"})
				return
			}
			writeRPC(w, req.ID, map[string]any{"structuredContent": map[string]any{"source": source}})
		}))
		return srv, hits
	}
	v1Srv, v1Hits := start("v1")
	defer v1Srv.Close()
	v2Srv, v2Hits := start("v2")
	defer v2Srv.Close()

	reg := NewRegistry()
	gw := NewGateway(reg, nil, Options{})
	v1 := baseTool(ClassMCP, "extensions.mcp.sample", "1.0.0", v1Srv.URL, hostOf(v1Srv.URL))
	v1.Endpoint.MCPTool = "sample"
	v1.Permissions = nil
	publish(t, reg, v1)
	if _, err := reg.Pin("run-old", "tenant-a", []string{v1.ID}); err != nil {
		t.Fatal(err)
	}
	v2 := v1
	v2.Version = "2.0.0"
	v2.Endpoint.URL = v2Srv.URL
	v2.Network = NetworkPolicy{Allow: hostOf(v2Srv.URL)}
	v2.Description = "replacement"
	publish(t, reg, v2)

	inv := invoke(t, gw, "run-old", v1.ID, agent(v1.ID), principal(), `{"query":"vendors"}`, nil)
	if inv.Error != nil || inv.ToolVersion != "1.0.0" || !bytes.Contains(inv.Output, []byte("v1")) {
		t.Fatalf("pinned invoke = version %s output %s err %v", inv.ToolVersion, inv.Output, inv.Error)
	}
	if v2Hits.Load() != 0 {
		t.Fatal("newer version received a pinned run")
	}
	if _, err := reg.Retire("tenant-a", v1.ID, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	inv = invoke(t, gw, "run-old", v1.ID, agent(v1.ID), principal(), `{"query":"vendors"}`, nil)
	if inv.Error != nil || inv.ToolVersion != "1.0.0" || !bytes.Contains(inv.Output, []byte("v1")) {
		t.Fatalf("retired pin changed: version %s output %s err %v", inv.ToolVersion, inv.Output, inv.Error)
	}
	mutated, err := reg.Pinned("run-old", v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	mutated.Description = "changed after pin"
	if err := reg.PutDraft(mutated); err == nil || asError(err).Code != CodeConflict {
		t.Fatalf("published version was editable: %v", err)
	}
	mutated.Endpoint.URL = v2Srv.URL
	mutated.InputSchema[0] = ' '
	again, err := reg.Pinned("run-old", v1.ID)
	if err != nil || again.Endpoint.URL != v1Srv.URL || again.Description == mutated.Description || again.InputSchema[0] == ' ' {
		t.Fatalf("pin copy aliased storage: %+v", again.Endpoint)
	}
	pin, err := reg.Pin("run-old", "tenant-a", []string{v1.ID})
	if err != nil || pin.Tools[v1.ID] != "1.0.0" {
		t.Fatalf("second pin = %+v %v", pin, err)
	}
	next, err := reg.Pin("run-new", "tenant-a", []string{v1.ID})
	if err != nil || next.Tools[v1.ID] != "2.0.0" {
		t.Fatalf("new run pin = %+v %v", next, err)
	}
	inv = invoke(t, gw, "run-new", v1.ID, agent(v1.ID), principal(), `{"query":"vendors"}`, nil)
	if inv.Error != nil || inv.ToolVersion != "2.0.0" || !bytes.Contains(inv.Output, []byte("v2")) {
		t.Fatalf("new run = version %s output %s err %v", inv.ToolVersion, inv.Output, inv.Error)
	}
	if v1Hits.Load() == 0 {
		t.Fatal("original version was never called")
	}
	if _, err := reg.Retire("tenant-a", v1.ID, "2.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Pin("run-dead", "tenant-a", []string{v1.ID}); asError(err).Code != CodeNotFound {
		t.Fatalf("retired catalog was pinned: %v", err)
	}
	if _, err := reg.Pin("run-old", "tenant-b", []string{v1.ID}); asError(err).Code != CodeToolNotAllowed {
		t.Fatalf("cross-tenant pin: %v", err)
	}
}

func TestWorkerCrashAndTimeoutAreRecorded(t *testing.T) {
	t.Run("process crash", func(t *testing.T) {
		addr := startWorkerProcess(t, "crash")
		reg := NewRegistry()
		gw := NewGateway(reg, nil, Options{})
		importWorker(t, gw, reg, addr, "tenant-a", nil)
		if _, err := reg.Publish("tenant-a", "extensions.worker.echo", "1.0.0"); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Pin("run-crash", "tenant-a", []string{"extensions.worker.echo"}); err != nil {
			t.Fatal(err)
		}
		inv := invoke(t, gw, "run-crash", "extensions.worker.echo", agent("extensions.worker.echo"), principal(), `{"query":"ping"}`, nil)
		if inv.Error == nil || inv.Error.Code != CodeWorkerCrash || !inv.Crash {
			t.Fatalf("crash record: %+v", inv.Error)
		}
		httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"source":"still-alive"}`))
		}))
		defer httpSrv.Close()
		follow := baseTool(ClassHTTP, "extensions.http.internal", "1.0.0", httpSrv.URL, hostOf(httpSrv.URL))
		follow.Permissions = nil
		publish(t, reg, follow)
		if _, err := reg.Pin("run-after", "tenant-a", []string{follow.ID}); err != nil {
			t.Fatal(err)
		}
		next := invoke(t, gw, "run-after", follow.ID, agent(follow.ID), principal(), `{"query":"vendors"}`, nil)
		if next.Error != nil || !bytes.Contains(next.Output, []byte("still-alive")) {
			t.Fatalf("caller did not continue after crash: %v %s", next.Error, next.Output)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		addr := serveTools(t, []ServiceTool{{
			ID: "extensions.worker.slow", Version: "1.0.0", SideEffect: SideEffectReadOnly,
			InputSchema: objectSchema(), OutputSchema: sourceSchema(),
			Handle: func(context.Context, json.RawMessage) (json.RawMessage, error) {
				time.Sleep(300 * time.Millisecond)
				return json.RawMessage(`{"source":"late"}`), nil
			},
		}})
		reg := NewRegistry()
		gw := NewGateway(reg, nil, Options{})
		tool := baseTool(ClassWorker, "extensions.worker.slow", "1.0.0", "http://"+addr, []string{addr})
		tool.Permissions = nil
		tool.TimeoutMS = 80
		publish(t, reg, tool)
		if _, err := reg.Pin("run-timeout", "tenant-a", []string{tool.ID}); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		inv := invoke(t, gw, "run-timeout", tool.ID, agent(tool.ID), principal(), `{"query":"vendors"}`, nil)
		if time.Since(started) > 250*time.Millisecond {
			t.Fatalf("timeout waited %s", time.Since(started))
		}
		if inv.Error == nil || inv.Error.Code != CodeTimeout || inv.Crash || inv.Uncertain {
			t.Fatalf("timeout record: crash %v uncertain %v err %v", inv.Crash, inv.Uncertain, inv.Error)
		}
	})

	t.Run("handler panic stays in the worker", func(t *testing.T) {
		var calls atomic.Int32
		addr := serveTools(t, []ServiceTool{{
			ID: "extensions.worker.panic", Version: "1.0.0", SideEffect: SideEffectReadOnly,
			InputSchema: objectSchema(), OutputSchema: sourceSchema(),
			Handle: func(context.Context, json.RawMessage) (json.RawMessage, error) {
				calls.Add(1)
				panic("boom")
			},
		}})
		reg := NewRegistry()
		gw := NewGateway(reg, nil, Options{})
		tool := baseTool(ClassWorker, "extensions.worker.panic", "1.0.0", "http://"+addr, []string{addr})
		tool.Permissions = nil
		publish(t, reg, tool)
		if _, err := reg.Pin("run-panic", "tenant-a", []string{tool.ID}); err != nil {
			t.Fatal(err)
		}
		inv := invoke(t, gw, "run-panic", tool.ID, agent(tool.ID), principal(), `{"query":"vendors"}`, nil)
		if inv.Error == nil || inv.Error.Code != CodeInternal || strings.Contains(inv.Error.Message, "boom") || inv.Crash {
			t.Fatalf("panic record: %+v", inv.Error)
		}
		if calls.Load() != 1 {
			t.Fatalf("panic handler calls = %d", calls.Load())
		}
	})
}

func TestReadOnlyRetriesAndSchemaRejection(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		_ = json.NewEncoder(w).Encode(wireResponse{Protocol: ProtocolVersion, Output: json.RawMessage(`{"source":"retried"}`)})
	}))
	defer srv.Close()
	reg := NewRegistry()
	gw := NewGateway(reg, nil, Options{})
	tool := baseTool(ClassWorker, "extensions.worker.flaky", "1.0.0", srv.URL, hostOf(srv.URL))
	tool.Permissions = nil
	tool.Retry.MaxAttempts = 2
	publish(t, reg, tool)
	if _, err := reg.Pin("run-retry", "tenant-a", []string{tool.ID}); err != nil {
		t.Fatal(err)
	}
	inv := invoke(t, gw, "run-retry", tool.ID, agent(tool.ID), principal(), `{"query":"vendors"}`, nil)
	if inv.Error != nil || inv.Attempts != 2 || !inv.History[0].Crash || inv.History[0].Code != CodeWorkerCrash {
		t.Fatalf("retry record: attempts %d history %+v err %v", inv.Attempts, inv.History, inv.Error)
	}
	if !bytes.Contains(inv.Output, []byte("retried")) || calls.Load() != 2 {
		t.Fatalf("calls %d output %s", calls.Load(), inv.Output)
	}

	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"source":"http-tool-output"}`))
	}))
	defer limited.Close()
	small := baseTool(ClassHTTP, "extensions.http.limited", "1.0.0", limited.URL, hostOf(limited.URL))
	small.Permissions = nil
	small.Resources.MaxOutputBytes = 8
	publish(t, reg, small)
	if _, err := reg.Pin("run-limit", "tenant-a", []string{small.ID}); err != nil {
		t.Fatal(err)
	}
	inv = invoke(t, gw, "run-limit", small.ID, agent(small.ID), principal(), `{"query":"vendors"}`, nil)
	if inv.Error == nil || inv.Error.Code != CodeResourceLimit || len(inv.Output) != 0 {
		t.Fatalf("limit record: %s err %v", inv.Output, inv.Error)
	}

	bad := baseTool(ClassHTTP, "extensions.http.bad-schema", "1.0.0", limited.URL, hostOf(limited.URL))
	bad.InputSchema = json.RawMessage(`{"pattern":"^x$"}`)
	if err := reg.PutDraft(bad); asError(err).Code != CodeInvalidInput {
		t.Fatalf("unsupported schema: %v", err)
	}
	named := baseTool(ClassHTTP, "extensions.http.named", "1.0.0", "http://example.invalid/tool", []string{"127.0.0.1:80"})
	if err := reg.PutDraft(named); asError(err).Code != CodeNetworkDenied {
		t.Fatalf("hostname endpoint: %v", err)
	}
	withUser := baseTool(ClassHTTP, "extensions.http.userinfo", "1.0.0", "http://user:pass@127.0.0.1:9/tool", []string{"127.0.0.1:9"})
	if err := reg.PutDraft(withUser); err == nil || strings.Contains(err.Error(), "pass") {
		t.Fatalf("userinfo endpoint: %v", err)
	}
}

func TestCredentialTenantIsolation(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(buf))
		_ = json.NewEncoder(w).Encode(wireResponse{Protocol: ProtocolVersion, Output: json.RawMessage(`{"ok":true}`)})
	}))
	defer srv.Close()
	reg := NewRegistry()
	creds := NewMemoryCredentials()
	if err := creds.Put(Credential{TenantID: "tenant-a", Ref: "vendor-api", Value: secretA}); err != nil {
		t.Fatal(err)
	}
	if err := creds.Put(Credential{TenantID: "tenant-b", Ref: "vendor-api", Value: secretB}); err != nil {
		t.Fatal(err)
	}
	if err := creds.Put(Credential{TenantID: "tenant-b", Ref: "other-api", Value: secretB}); err != nil {
		t.Fatal(err)
	}
	if err := creds.Put(Credential{TenantID: "tenant-a", Ref: "vendor-api", Value: "bad\nvalue"}); err == nil {
		t.Fatal("newline credential accepted")
	}
	gw := NewGateway(reg, creds, Options{})
	makeTool := func(tenant, id, ref string) ToolRecord {
		tool := baseTool(ClassWorker, id, "1.0.0", srv.URL, hostOf(srv.URL))
		tool.TenantID = tenant
		tool.AuthRef = ref
		tool.Permissions = nil
		tool.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
		tool.OutputSchema = json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`)
		return tool
	}
	toolA := makeTool("tenant-a", "extensions.worker.secret", "vendor-api")
	toolB := makeTool("tenant-b", "extensions.worker.secret", "vendor-api")
	missing := makeTool("tenant-a", "extensions.worker.missing", "other-api")
	publish(t, reg, toolA)
	publish(t, reg, toolB)
	publish(t, reg, missing)
	if _, err := reg.Pin("run-a", "tenant-a", []string{toolA.ID, missing.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Pin("run-b", "tenant-b", []string{toolB.ID}); err != nil {
		t.Fatal(err)
	}
	callerA := Principal{ID: "user-a", TenantID: "tenant-a"}
	inv := invoke(t, gw, "run-a", toolA.ID, agent(toolA.ID), callerA, `{}`, nil)
	if inv.Error != nil {
		t.Fatal(inv.Error)
	}
	if len(bodies) != 1 || !strings.Contains(bodies[0], secretA) || strings.Contains(bodies[0], secretB) {
		t.Fatalf("worker saw the wrong credential: %v", bodies)
	}
	raw, _ := json.Marshal(inv)
	meta, _ := json.Marshal(inv.Metadata())
	if bytes.Contains(raw, []byte(secretA)) || bytes.Contains(raw, []byte(secretB)) || bytes.Contains(meta, []byte(secretA)) {
		t.Fatalf("invocation stored a credential: %s", raw)
	}
	callerB := Principal{ID: "user-b", TenantID: "tenant-b"}
	inv = invoke(t, gw, "run-b", toolB.ID, agent(toolB.ID), callerB, `{}`, nil)
	if inv.Error != nil || len(bodies) != 2 || !strings.Contains(bodies[1], secretB) || strings.Contains(bodies[1], secretA) {
		t.Fatalf("tenant B body: %v err %v", bodies, inv.Error)
	}
	before := len(bodies)
	inv = invoke(t, gw, "run-b", toolB.ID, agent(toolB.ID), callerA, `{}`, nil)
	if inv.Error == nil || inv.Error.Code != CodeToolNotAllowed || len(bodies) != before || strings.Contains(inv.Error.Message, secretB) {
		t.Fatalf("cross-tenant invoke: %v bodies %d", inv.Error, len(bodies))
	}
	inv = invoke(t, gw, "run-a", missing.ID, agent(missing.ID), callerA, `{}`, nil)
	if inv.Error == nil || inv.Error.Code != CodeCredentialDenied || len(bodies) != before || strings.Contains(inv.Error.Message, secretB) {
		t.Fatalf("missing credential: %v", inv.Error)
	}

	var ran atomic.Int32
	addr := serveTools(t, []ServiceTool{{
		ID: "extensions.worker.leak", Version: "1.0.0", SideEffect: SideEffectReadOnly,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Handle: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			ran.Add(1)
			cred, ok := CredentialFromContext(ctx)
			if !ok {
				return nil, &Error{Code: CodeCredentialDenied, Message: "credential is not available"}
			}
			return json.Marshal(map[string]string{"leak": cred.Value})
		},
	}})
	leak := baseTool(ClassWorker, "extensions.worker.leak", "1.0.0", "http://"+addr, []string{addr})
	leak.AuthRef = "vendor-api"
	leak.Permissions = nil
	leak.InputSchema = json.RawMessage(`{"type":"object"}`)
	leak.OutputSchema = json.RawMessage(`{"type":"object"}`)
	publish(t, reg, leak)
	if _, err := reg.Pin("run-leak", "tenant-a", []string{leak.ID}); err != nil {
		t.Fatal(err)
	}
	inv = invoke(t, gw, "run-leak", leak.ID, agent(leak.ID), callerA, `{}`, nil)
	raw, _ = json.Marshal(inv)
	if ran.Load() != 1 || inv.Error == nil || inv.Error.Code != CodeCredentialDenied || bytes.Contains(raw, []byte(secretA)) || len(inv.Output) != 0 {
		t.Fatalf("leaked output: %s err %v", raw, inv.Error)
	}
}

func TestNetworkAndFilesystemRestrictions(t *testing.T) {
	allowedAddr, allowedHits := countingListener(t)
	forbiddenAddr, forbiddenHits := countingListener(t)
	root := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideMarker := "outside-secret-marker"
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte(outsideMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	addr := serveTools(t, []ServiceTool{{
		ID: "extensions.worker.guard", Version: "1.0.0", SideEffect: SideEffectReadOnly,
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"dial":{"type":"string"},"read":{"type":"string"}},"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"},"bytes":{"type":"integer"}},"required":["ok"],"additionalProperties":false}`),
		Handle: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			var body struct {
				Dial string `json:"dial"`
				Read string `json:"read"`
			}
			if err := json.Unmarshal(input, &body); err != nil {
				return nil, err
			}
			if body.Dial != "" {
				conn, err := Dial(ctx, "tcp", body.Dial)
				if err != nil {
					return nil, err
				}
				_ = conn.Close()
				return json.RawMessage(`{"ok":true,"bytes":0}`), nil
			}
			buf, err := ReadFile(ctx, body.Read)
			if err != nil {
				return nil, err
			}
			out, err := json.Marshal(map[string]any{"ok": true, "bytes": len(buf)})
			return out, err
		},
	}})
	reg := NewRegistry()
	gw := NewGateway(reg, nil, Options{})
	tool := baseTool(ClassWorker, "extensions.worker.guard", "1.0.0", "http://"+addr, []string{addr, allowedAddr})
	tool.Permissions = nil
	tool.InputSchema = json.RawMessage(`{"type":"object","properties":{"dial":{"type":"string"},"read":{"type":"string"}},"additionalProperties":false}`)
	tool.OutputSchema = json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"},"bytes":{"type":"integer"}},"required":["ok"],"additionalProperties":false}`)
	tool.Filesystem.Root = root
	publish(t, reg, tool)
	if _, err := reg.Pin("run-guard", "tenant-a", []string{tool.ID}); err != nil {
		t.Fatal(err)
	}
	caller := principal()
	caller.Permissions = nil
	allowedAgent := agent(tool.ID)
	inv := invoke(t, gw, "run-guard", tool.ID, allowedAgent, caller, `{"dial":"`+forbiddenAddr+`"}`, nil)
	if inv.Error == nil || inv.Error.Code != CodeNetworkDenied || forbiddenHits.Load() != 0 {
		t.Fatalf("forbidden dial: hits %d err %v", forbiddenHits.Load(), inv.Error)
	}
	inv = invoke(t, gw, "run-guard", tool.ID, allowedAgent, caller, `{"dial":"example.invalid:80"}`, nil)
	if inv.Error == nil || inv.Error.Code != CodeNetworkDenied || !strings.Contains(inv.Error.Message, "IP address") {
		t.Fatalf("hostname dial: %v", inv.Error)
	}
	inv = invoke(t, gw, "run-guard", tool.ID, allowedAgent, caller, `{"dial":"`+allowedAddr+`"}`, nil)
	if inv.Error != nil || allowedHits.Load() != 1 {
		t.Fatalf("allowed dial: hits %d err %v", allowedHits.Load(), inv.Error)
	}
	inv = invoke(t, gw, "run-guard", tool.ID, allowedAgent, caller, `{"read":"`+filepath.Join(root, "note.txt")+`"}`, nil)
	if inv.Error != nil || !bytes.Contains(inv.Output, []byte(`"bytes":6`)) {
		t.Fatalf("allowed read: %s err %v", inv.Output, inv.Error)
	}
	inv = invoke(t, gw, "run-guard", tool.ID, allowedAgent, caller, `{"read":"`+filepath.Join(outsideDir, "secret.txt")+`"}`, nil)
	raw, _ := json.Marshal(inv)
	if inv.Error == nil || inv.Error.Code != CodeFilesystemDenied || bytes.Contains(raw, []byte(outsideMarker)) {
		t.Fatalf("outside read: %s", raw)
	}
}

func TestWriteApprovalAndIdempotency(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	var hits atomic.Int32
	addr := serveTools(t, []ServiceTool{{
		ID: "extensions.worker.write", Version: "1.0.0", SideEffect: SideEffectWrite,
		InputSchema:  objectSchema(),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"saved":{"type":"boolean"}},"required":["saved"],"additionalProperties":false}`),
		Handle: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			hits.Add(1)
			return json.RawMessage(`{"saved":true}`), nil
		},
	}})
	reg := NewRegistry()
	gw := NewGateway(reg, nil, Options{Now: func() time.Time { return now }})
	tool := baseTool(ClassWorker, "extensions.worker.write", "1.0.0", "http://"+addr, []string{addr})
	tool.SideEffect = SideEffectWrite
	tool.OutputSchema = json.RawMessage(`{"type":"object","properties":{"saved":{"type":"boolean"}},"required":["saved"],"additionalProperties":false}`)
	publish(t, reg, tool)
	if _, err := reg.Pin("run-write", "tenant-a", []string{tool.ID}); err != nil {
		t.Fatal(err)
	}
	caller := principal()
	ag := agent(tool.ID)
	inv := invoke(t, gw, "run-write", tool.ID, ag, caller, `{"query":"save"}`, nil)
	if inv.Error == nil || inv.Error.Code != CodeApprovalRequired || hits.Load() != 0 {
		t.Fatalf("missing approval: %v hits %d", inv.Error, hits.Load())
	}
	denied := approvalFor(tool, now, "idem-key-1")
	denied.Approved = false
	inv = invoke(t, gw, "run-write", tool.ID, ag, caller, `{"query":"save"}`, denied)
	if inv.Error == nil || inv.Error.Code != CodeApprovalRequired || hits.Load() != 0 {
		t.Fatalf("denied approval: %v", inv.Error)
	}
	expired := approvalFor(tool, now, "idem-key-1")
	expired.ExpiresAt = now.Add(-time.Minute)
	inv = invoke(t, gw, "run-write", tool.ID, ag, caller, `{"query":"save"}`, expired)
	if inv.Error == nil || inv.Error.Code != CodeApprovalRequired || hits.Load() != 0 {
		t.Fatalf("expired approval: %v", inv.Error)
	}
	ok := approvalFor(tool, now, "idem-key-1")
	inv = invoke(t, gw, "run-write", tool.ID, ag, caller, `{"query":"save"}`, ok)
	if inv.Error != nil || hits.Load() != 1 || !bytes.Contains(inv.Output, []byte(`"saved":true`)) {
		t.Fatalf("approved write: %s err %v hits %d", inv.Output, inv.Error, hits.Load())
	}
	again := invoke(t, gw, "run-write", tool.ID, ag, caller, `{"query":"save"}`, ok)
	if again.Error != nil || hits.Load() != 1 || !bytes.Equal(again.Output, inv.Output) {
		t.Fatalf("idempotent replay dispatched again: hits %d err %v", hits.Load(), again.Error)
	}
	second := approvalFor(tool, now, "idem-key-2")
	if extra := invoke(t, gw, "run-write", tool.ID, ag, caller, `{"query":"save"}`, second); extra.Error != nil || hits.Load() != 2 {
		t.Fatalf("new key: hits %d err %v", hits.Load(), extra.Error)
	}

	var slowHits atomic.Int32
	slowAddr := serveTools(t, []ServiceTool{{
		ID: "extensions.worker.slow-write", Version: "1.0.0", SideEffect: SideEffectWrite,
		InputSchema:  objectSchema(),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"saved":{"type":"boolean"}},"required":["saved"],"additionalProperties":false}`),
		Handle: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			slowHits.Add(1)
			time.Sleep(300 * time.Millisecond)
			return json.RawMessage(`{"saved":true}`), nil
		},
	}})
	slow := tool
	slow.ID = "extensions.worker.slow-write"
	slow.Endpoint.URL = "http://" + slowAddr
	slow.Endpoint.WorkerID = slowAddr
	slow.Network = NetworkPolicy{Allow: []string{slowAddr}}
	slow.TimeoutMS = 80
	publish(t, reg, slow)
	if _, err := reg.Pin("run-uncertain", "tenant-a", []string{slow.ID}); err != nil {
		t.Fatal(err)
	}
	uncertainKey := approvalFor(slow, now, "idem-key-slow")
	inv = invoke(t, gw, "run-uncertain", slow.ID, agent(slow.ID), caller, `{"query":"save"}`, uncertainKey)
	if inv.Error == nil || inv.Error.Code != CodeTimeout || !inv.Uncertain || slowHits.Load() != 1 {
		t.Fatalf("uncertain write: hits %d uncertain %v err %v", slowHits.Load(), inv.Uncertain, inv.Error)
	}
	replayed := invoke(t, gw, "run-uncertain", slow.ID, agent(slow.ID), caller, `{"query":"save"}`, uncertainKey)
	if replayed.Error == nil || replayed.Error.Code != CodeTimeout || !replayed.Uncertain || slowHits.Load() != 1 {
		t.Fatalf("uncertain write retried: hits %d err %v", slowHits.Load(), replayed.Error)
	}
}

func TestDiscoveredWorkerRegistersAndRuns(t *testing.T) {
	addr := startWorkerProcess(t, "echo")
	reg := NewRegistry()
	gw := NewGateway(reg, nil, Options{})
	importWorker(t, gw, reg, addr, "tenant-a", nil)
	if _, err := reg.Publish("tenant-a", "extensions.worker.echo", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Pin("run-echo", "tenant-a", []string{"extensions.worker.echo"}); err != nil {
		t.Fatal(err)
	}
	inv := invoke(t, gw, "run-echo", "extensions.worker.echo", agent("extensions.worker.echo"), principal(), `{"query":"ping"}`, nil)
	if inv.Error != nil || !bytes.Contains(inv.Output, []byte(`"query":"ping"`)) {
		t.Fatalf("echo worker: %s err %v", inv.Output, inv.Error)
	}
}

func TestManagementAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"source":"api"}`))
	}))
	defer upstream.Close()
	reg := NewRegistry()
	gw := NewGateway(reg, nil, Options{})
	api := httptest.NewServer(NewAPI(reg, gw))
	defer api.Close()
	tool := baseTool(ClassHTTP, "extensions.http.internal", "1.0.0", upstream.URL, hostOf(upstream.URL))
	tool.Endpoint.Internal = true
	res := postJSON(t, api.URL+"/v1/extension-tools", tool)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("draft status %d", res.StatusCode)
	}
	res.Body.Close()
	rejected := postRaw(t, api.URL+"/v1/extension-tools", []byte(`{"tenant_id":"tenant-a","id":"extensions.http.other","version":"1.0.0","credential":"not-a-real-secret"}`))
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("credential field status %d", rejected.StatusCode)
	}
	rejected.Body.Close()
	res = postJSON(t, api.URL+"/v1/extension-tools/publish", map[string]string{"tenant_id": "tenant-a", "id": tool.ID, "version": "1.0.0"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("publish status %d", res.StatusCode)
	}
	res.Body.Close()
	res = postJSON(t, api.URL+"/v1/extension-tools/pin", map[string]any{"run_id": "run-api", "tenant_id": "tenant-a", "tool_ids": []string{tool.ID}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("pin status %d", res.StatusCode)
	}
	res.Body.Close()
	res = postJSON(t, api.URL+"/v1/extension-tools/invoke", map[string]any{
		"run_id": "run-api", "tool_id": tool.ID, "invocation_id": "inv-1",
		"agent_id": "orchestrator", "agent_version": "1.0.0", "allowed_tools": []string{tool.ID},
		"principal": principal(), "input": json.RawMessage(`{"query":"vendors"}`),
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("invoke status %d", res.StatusCode)
	}
	var inv Invocation
	if err := json.NewDecoder(res.Body).Decode(&inv); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if inv.Error != nil || !bytes.Contains(inv.Output, []byte("api")) {
		t.Fatalf("api invoke: %s err %v", inv.Output, inv.Error)
	}
	res = postJSON(t, api.URL+"/v1/extension-tools/invoke", map[string]any{
		"run_id": "run-api", "tool_id": tool.ID, "invocation_id": "inv-2",
		"agent_id": "orchestrator", "agent_version": "1.0.0", "allowed_tools": []string{},
		"principal": principal(), "input": json.RawMessage(`{"query":"vendors"}`),
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("denied invoke status %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&inv); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if inv.Error == nil || inv.Error.Code != CodeToolNotAllowed {
		t.Fatalf("api denial: %+v", inv.Error)
	}
	listRes, err := loopbackClient.Get(api.URL + "/v1/extension-tools?tenant_id=tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	defer listRes.Body.Close()
	if listRes.StatusCode != http.StatusOK {
		t.Fatalf("list status %d", listRes.StatusCode)
	}
}

func TestSchemaSubset(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"},"tag":{"type":"string","enum":["a","b"]}},"required":["n"],"additionalProperties":false}`)
	if err := ValidateSchemaDocument(schema); err != nil {
		t.Fatal(err)
	}
	if err := ValidateValue(schema, json.RawMessage(`{"n":2,"tag":"a"}`)); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`{"tag":"a"}`, `{"n":1.5}`, `{"n":1,"tag":"c"}`, `{"n":1,"extra":true}`, `[]`} {
		if err := ValidateValue(schema, json.RawMessage(value)); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	if err := ValidateSchemaDocument(json.RawMessage(`{"type":"object","properties":{"q":{"minLength":1}}}`)); err == nil {
		t.Fatal("nested unsupported keyword accepted")
	}
}

func TestSecretsStayOutOfSource(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	module := filepath.Join(filepath.Dir(file), "..", "..")
	roots := []string{
		filepath.Join(module, "internal", "extensions"),
		filepath.Join(module, "cmd", "extension-worker"),
	}
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || strings.HasSuffix(path, "_test.go") {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(body, []byte(secretA)) || bytes.Contains(body, []byte(secretB)) {
				t.Errorf("%s contains a credential value", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func objectSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`)
}

func sourceSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"}},"required":["source"],"additionalProperties":false}`)
}

func baseTool(class Class, id, version, endpoint string, allow []string) ToolRecord {
	return ToolRecord{
		TenantID:     "tenant-a",
		ID:           id,
		Version:      version,
		Description:  "pass 6 test tool",
		InputSchema:  objectSchema(),
		OutputSchema: sourceSchema(),
		SideEffect:   SideEffectReadOnly,
		TimeoutMS:    2000,
		Class:        class,
		Endpoint:     Endpoint{URL: endpoint, Method: "POST"},
		Permissions:  []string{"extensions.use"},
		Retry:        RetryPolicy{MaxAttempts: 1},
		Resources:    ResourceLimits{MaxOutputBytes: 1 << 16},
		Network:      NetworkPolicy{Allow: allow},
	}
}

func hostOf(raw string) []string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	return []string{parsed.Host}
}

func publish(t *testing.T, reg *Registry, tool ToolRecord) {
	t.Helper()
	if err := reg.PutDraft(tool); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Publish(tool.TenantID, tool.ID, tool.Version); err != nil {
		t.Fatal(err)
	}
}

func regMust(t *testing.T, reg *Registry, tool ToolRecord) ToolRecord {
	t.Helper()
	got, err := reg.Get(tool.TenantID, tool.ID, tool.Version)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func agent(ids ...string) contracts.AgentDefinition {
	return contracts.AgentDefinition{ID: "orchestrator", Version: "1.0.0", AllowedTools: ids}
}

func principal() Principal {
	return Principal{ID: "user-1", TenantID: "tenant-a", Permissions: []string{"extensions.use"}}
}

func invoke(t *testing.T, gw *Gateway, runID, toolID string, ag contracts.AgentDefinition, caller Principal, input string, approval *Approval) Invocation {
	t.Helper()
	inv, err := gw.Invoke(context.Background(), runID, InvokeRequest{
		Principal:  caller,
		Agent:      ag,
		Invocation: contracts.AgentInvocation{ID: "agent-inv", RunID: runID, AgentID: ag.ID, AgentVersion: ag.Version},
		ToolID:     toolID,
		Input:      json.RawMessage(input),
		Approval:   approval,
	})
	if inv.ID == "" {
		t.Fatalf("invoke %s: %v", toolID, err)
	}
	return inv
}

func approvalFor(tool ToolRecord, now time.Time, key string) *Approval {
	return &Approval{
		ID:             "apr-1",
		TenantID:       tool.TenantID,
		ToolID:         tool.ID,
		ToolVersion:    tool.Version,
		IdempotencyKey: key,
		Approved:       true,
		ExpiresAt:      now.Add(time.Minute),
	}
}

func writeRPC(w http.ResponseWriter, id int64, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func serveTools(t *testing.T, tools []ServiceTool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(ctx, ln, tools) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	waitReady(t, ln.Addr().String())
	return ln.Addr().String()
}

func countingListener(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hits := &atomic.Int32{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			hits.Add(1)
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String(), hits
}

var loopbackClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
	},
}

func waitReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		resp, err := loopbackClient.Get("http://" + addr + "/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		last = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker not ready: %v", last)
}

func importWorker(t *testing.T, gw *Gateway, reg *Registry, addr, tenant string, permissions []string) {
	t.Helper()
	found, err := gw.WorkerDiscovery().Discover(context.Background(), "http://"+addr, NetworkPolicy{Allow: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.ImportDiscovered(ImportOptions{
		TenantID: tenant, Endpoint: "http://" + addr, Network: NetworkPolicy{Allow: []string{addr}},
		Permissions: permissions, TimeoutMS: 2000,
	}, found); err != nil {
		t.Fatal(err)
	}
}

var (
	workerOnce  sync.Once
	workerPath  string
	workerBuild error
)

func extensionWorkerBin(t *testing.T) string {
	t.Helper()
	workerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "nmc3-extension-worker")
		if err != nil {
			workerBuild = err
			return
		}
		workerPath = filepath.Join(dir, "extension-worker")
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			workerBuild = errors.New("no caller")
			return
		}
		cmd := exec.Command("go", "build", "-o", workerPath, "./cmd/extension-worker")
		cmd.Dir = filepath.Join(filepath.Dir(file), "..", "..")
		out, err := cmd.CombinedOutput()
		if err != nil {
			workerBuild = fmt.Errorf("build extension worker: %v\n%s", err, out)
		}
	})
	if workerBuild != nil {
		t.Fatal(workerBuild)
	}
	return workerPath
}

func startWorkerProcess(t *testing.T, mode string) string {
	t.Helper()
	cmd := exec.Command(extensionWorkerBin(t), "-listen", "127.0.0.1:0", "-mode", mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("worker address: %v %s", err, stderr.String())
	}
	addr := strings.TrimSpace(line)
	waitReady(t, addr)
	return addr
}

func postJSON(t *testing.T, endpoint string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return postRaw(t, endpoint, buf)
}

func postRaw(t *testing.T, endpoint string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := loopbackClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
