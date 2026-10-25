package definitions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

func TestHTTPWebsiteFlow(t *testing.T) {
	exerciseWebsiteAndPin(t, mustRegistry(t))
}

func TestWebsiteClientUsesHandlerRoutes(t *testing.T) {
	api := string(readRepoFile(t, "../frontend/src/components/definitions/api.ts"))
	page := string(readRepoFile(t, "../frontend/src/app/(console)/agents/new/page.tsx"))
	editor := string(readRepoFile(t, "../frontend/src/components/builder/Builder.tsx"))
	if !strings.Contains(page, "Builder") {
		t.Fatal("new agent page does not render the builder")
	}
	for _, want := range []string{
		"/agent-api/definitions/draft",
		"/agent-api/definitions/test",
		"/agent-api/definitions/publish",
		"/agent-api/definitions/revise",
		"/agent-api/definitions/retire",
		"/agent-api/definitions/export",
		"X-NMC3-Actor",
		"specFromForm",
	} {
		if !strings.Contains(api, want) {
			t.Fatalf("definition client missing %s", want)
		}
	}
	for _, want := range []string{"definitionsApi.publish"} {
		if !strings.Contains(editor, want) {
			t.Fatalf("builder does not call %s", want)
		}
	}
}

func TestDefinitionsListen(t *testing.T) {
	addr := os.Getenv("NMC3_DEFINITIONS_ADDR")
	if addr == "" {
		t.Skip("set NMC3_DEFINITIONS_ADDR to serve an in-memory definition API")
	}
	reg, err := NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.Close)
	server := &http.Server{Addr: addr, Handler: Handler(reg), ReadHeaderTimeout: 10 * time.Second}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}

func exerciseWebsiteAndPin(t *testing.T, reg *Registry) {
	t.Helper()
	ctx := context.Background()
	mux := http.NewServeMux()
	Mount(mux, reg)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	if status, _ := apiDo(t, server, http.MethodGet, "/v1/agents", "", nil); status != http.StatusTeapot {
		t.Fatalf("run API route status %d", status)
	}
	if status, body := apiDo(t, server, http.MethodPost, pathDraft, "", []byte(`{}`)); status != http.StatusBadRequest || apiCode(t, body) != CodeInvalidInput {
		t.Fatalf("missing actor %d %s", status, body)
	}

	broken := childSpec()
	broken.ID = "broken-helper"
	broken.Name = ""
	broken.Limits.MaxTokens = 0
	status, body := apiDo(t, server, http.MethodPost, pathPublish, "ada", mustJSON(t, broken))
	if status != http.StatusBadRequest || apiCode(t, body) != CodeInvalidInput {
		t.Fatalf("invalid publish %d %s", status, body)
	}
	assertAPIField(t, body, "name", "required")
	assertAPIField(t, body, "limits.max_tokens", "greater than zero")
	if _, err := reg.Get(ctx, "broken-helper", "1.0.0"); registryErr(t, err).Code != CodeNotFound {
		t.Fatal("invalid publish wrote a version")
	}

	stable := specialist("stable-helper", "1.0.0", "Stable helper")
	status, body = apiDo(t, server, http.MethodPost, pathDraft, "ada", mustJSON(t, stable))
	if status != http.StatusOK || mustDefinition(t, body).State != StateDraft {
		t.Fatalf("stable draft %d %s", status, body)
	}
	invalid := stable
	invalid.Name = ""
	invalid.Limits.MaxCostUSD = 0
	status, body = apiDo(t, server, http.MethodPost, pathPublish, "ada", mustJSON(t, invalid))
	if status != http.StatusBadRequest || apiCode(t, body) != CodeInvalidInput {
		t.Fatalf("invalid publish of draft %d %s", status, body)
	}
	assertAPIField(t, body, "name", "required")
	assertAPIField(t, body, "limits.max_cost_usd", "greater than zero")
	kept, err := reg.Get(ctx, stable.ID, stable.Version)
	if err != nil || kept.State != StateDraft || kept.Spec.Name != stable.Name {
		t.Fatalf("draft was overwritten: %+v %v", kept, err)
	}

	parent := parentSpec("1.0.0", "version A")
	status, body = apiDo(t, server, http.MethodPost, pathPublish, "ada", mustJSON(t, parent))
	if status != http.StatusBadRequest || apiCode(t, body) != CodeInvalidInput {
		t.Fatalf("publish before delegate %d %s", status, body)
	}
	assertAPIField(t, body, "allowed_delegates[0]", "unknown delegate")
	if _, err := reg.Get(ctx, parent.ID, parent.Version); registryErr(t, err).Code != CodeNotFound {
		t.Fatal("rejected parent publish wrote a version")
	}

	child := childSpec()
	file := child
	file.InputSchema = json.RawMessage(`{"additionalProperties":false,"required":["question"],"properties":{"question":{"type":"string"}},"type":"object"}`)
	_, fileHash, err := Canonical(file)
	if err != nil {
		t.Fatal(err)
	}
	_, childHash, err := Canonical(child)
	if err != nil {
		t.Fatal(err)
	}
	if fileHash != childHash {
		t.Fatal("reordered schema changed the canonical hash")
	}
	status, body = apiDo(t, server, http.MethodPost, pathDraft, "ada", reversedJSON(t, child))
	if status != http.StatusOK {
		t.Fatalf("draft child %d %s", status, body)
	}
	status, body = apiDo(t, server, http.MethodPost, pathPublish, "ada", mustJSON(t, parent))
	if status != http.StatusBadRequest || apiCode(t, body) != CodeInvalidInput {
		t.Fatalf("publish before delegate is published %d %s", status, body)
	}
	assertAPIField(t, body, "allowed_delegates[0]", "published")
	if _, err := reg.Get(ctx, parent.ID, parent.Version); registryErr(t, err).Code != CodeNotFound {
		t.Fatal("unpublished delegate still stored the parent")
	}
	status, body = apiDo(t, server, http.MethodPost, pathTest, "ada", mustJSON(t, versionRef{ID: child.ID, Version: child.Version}))
	tested := mustDefinition(t, body)
	if status != http.StatusOK || tested.State != StateTesting || tested.TestedBy != "ada" {
		t.Fatalf("test child %d %s", status, body)
	}
	status, body = apiDo(t, server, http.MethodPost, pathPublish, "grace", reversedJSON(t, child))
	publishedChild := mustDefinition(t, body)
	if status != http.StatusOK || publishedChild.State != StatePublished || publishedChild.PublishedBy != "grace" || publishedChild.ContentHash != fileHash {
		t.Fatalf("publish child %d %s", status, body)
	}
	changed := child
	changed.Instructions = "mutated"
	status, body = apiDo(t, server, http.MethodPost, pathPublish, "grace", mustJSON(t, changed))
	if status != http.StatusConflict || apiCode(t, body) != CodeImmutable {
		t.Fatalf("mutate published %d %s", status, body)
	}
	storedChild, err := reg.Get(ctx, child.ID, child.Version)
	if err != nil || storedChild.Spec.Instructions != child.Instructions || storedChild.ContentHash != fileHash {
		t.Fatal("published child changed")
	}

	status, body = apiDo(t, server, http.MethodPost, pathDraft, "ada", mustJSON(t, parent))
	if status != http.StatusOK {
		t.Fatalf("draft parent %d %s", status, body)
	}
	status, body = apiDo(t, server, http.MethodPost, pathTest, "ada", mustJSON(t, versionRef{ID: parent.ID, Version: parent.Version}))
	if status != http.StatusOK || mustDefinition(t, body).State != StateTesting {
		t.Fatalf("test parent %d %s", status, body)
	}
	status, body = apiDo(t, server, http.MethodPost, pathPublish, "grace", mustJSON(t, parent))
	publishedParent := mustDefinition(t, body)
	if status != http.StatusOK || publishedParent.State != StatePublished {
		t.Fatalf("publish parent %d %s", status, body)
	}
	_, parentHash, err := Canonical(parent)
	if err != nil {
		t.Fatal(err)
	}
	if publishedParent.ContentHash != parentHash {
		t.Fatal("parent hash drifted from the posted spec")
	}

	pins, err := reg.ResolveAndPinRun(ctx, "run-a", parent.ID, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 2 || pins[0].AgentID != parent.ID || pins[1].AgentID != child.ID {
		t.Fatalf("pins %+v", pins)
	}
	if pins[0].Version != "1.0.0" || pins[0].ContentHash != parentHash || pins[1].Version != "1.0.0" || pins[1].ContentHash != fileHash {
		t.Fatalf("pin hashes %+v", pins)
	}
	again, err := reg.ResolveAndPinRun(ctx, "run-a", parent.ID, "1.0.0")
	if err != nil || len(again) != 2 || again[0].ContentHash != parentHash || again[1].ContentHash != fileHash {
		t.Fatalf("second pin %+v %v", again, err)
	}

	childB := child
	childB.Version = "1.1.0"
	childB.Instructions = "version B"
	status, body = apiDo(t, server, http.MethodPost, pathRevise, "ida", mustJSON(t, reviseRequest{ID: child.ID, FromVersion: "1.0.0", Spec: mustJSON(t, childB)}))
	if status != http.StatusOK || mustDefinition(t, body).State != StateDraft || mustDefinition(t, body).Spec.Version != "1.1.0" {
		t.Fatalf("revise child %d %s", status, body)
	}
	if got, err := reg.Get(ctx, child.ID, "1.0.0"); err != nil || got.Spec.Instructions != child.Instructions || got.State != StatePublished {
		t.Fatal("revise changed version A")
	}
	status, body = apiDo(t, server, http.MethodPost, pathPublish, "ida", mustJSON(t, childB))
	if status != http.StatusOK || mustDefinition(t, body).State != StatePublished {
		t.Fatalf("publish child B %d %s", status, body)
	}
	parentB := parentSpec("1.1.0", "version B")
	status, body = apiDo(t, server, http.MethodPost, pathRevise, "ida", mustJSON(t, reviseRequest{ID: parent.ID, FromVersion: "1.0.0", Spec: mustJSON(t, parentB)}))
	if status != http.StatusOK {
		t.Fatalf("revise parent %d %s", status, body)
	}
	status, body = apiDo(t, server, http.MethodPost, pathPublish, "ida", mustJSON(t, parentB))
	if status != http.StatusOK {
		t.Fatalf("publish parent B %d %s", status, body)
	}
	pinned, err := reg.Pinned(ctx, "run-a", child.ID)
	if err != nil || pinned.Version != "1.0.0" || pinned.ContentHash != fileHash {
		t.Fatalf("pin moved after publish B: %+v %v", pinned, err)
	}

	for _, id := range []string{child.ID, parent.ID} {
		status, body = apiDo(t, server, http.MethodPost, pathRetire, "ida", mustJSON(t, versionRef{ID: id, Version: "1.0.0"}))
		retired := mustDefinition(t, body)
		if status != http.StatusOK || retired.State != StateRetired || retired.ContentHash != map[string]string{child.ID: fileHash, parent.ID: parentHash}[id] {
			t.Fatalf("retire %s %d %s", id, status, body)
		}
	}
	pinned, err = reg.Pinned(ctx, "run-a", parent.ID)
	if err != nil || pinned.Version != "1.0.0" || pinned.ContentHash != parentHash || pinned.Spec.Instructions != "version A" {
		t.Fatalf("pin moved after retire: %+v %v", pinned, err)
	}
	if _, err := reg.ResolveForNewRun(ctx, parent.ID, "1.0.0"); registryErr(t, err).Code != CodeRetired {
		t.Fatalf("resolve retired: %v", err)
	}
	if _, err := reg.ResolveAndPinRun(ctx, "run-b", parent.ID, "1.0.0"); registryErr(t, err).Code != CodeRetired {
		t.Fatalf("pin retired: %v", err)
	}
	if leftover, err := reg.Pins(ctx, "run-b"); err != nil || len(leftover) != 0 {
		t.Fatalf("failed pin wrote rows: %+v %v", leftover, err)
	}
	nextPins, err := reg.ResolveAndPinRun(ctx, "run-c", parent.ID, "1.1.0")
	if err != nil || len(nextPins) != 2 || nextPins[0].Version != "1.1.0" || nextPins[1].Version != "1.1.0" {
		t.Fatalf("pin B %+v %v", nextPins, err)
	}

	status, body = apiDo(t, server, http.MethodGet, pathDefinitions+"?id="+child.ID+"&version=1.0.0", "", nil)
	if status != http.StatusOK || mustDefinition(t, body).State != StateRetired {
		t.Fatalf("get retired %d %s", status, body)
	}
	status, body = apiDo(t, server, http.MethodGet, pathExport, "", nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"schema_version":"1"`)) {
		t.Fatalf("export %d %s", status, body)
	}
	restored := mustRegistry(t)
	if _, err := restored.Import(ctx, "restore", body, ImportPublish); err != nil {
		t.Fatal(err)
	}
	got, err := restored.Get(ctx, child.ID, "1.0.0")
	if err != nil || got.ContentHash != fileHash || got.State != StateRetired {
		t.Fatalf("export round trip %+v %v", got, err)
	}
}

func childSpec() Spec {
	spec := specialist("catalog-helper", "1.0.0", "Catalog helper")
	spec.Instructions = "Answer from the catalog."
	spec.AllowedTools = []string{"catalog.search"}
	spec.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"question":{"type":"string"}},"required":["question"]}`)
	spec.OutputSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
	spec.Limits = contracts.Limits{
		MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 2,
		MaxModelTurns: 4, MaxTokens: 1000, MaxCostUSD: 1, TimeoutSeconds: 30,
	}
	return spec
}

func parentSpec(version, instructions string) Spec {
	spec := specialist("vendor-orchestrator", version, "Vendor orchestrator")
	spec.Role = "orchestrator"
	spec.Instructions = instructions
	spec.AllowedDelegates = []string{"catalog-helper"}
	return spec
}

func reversedJSON(t *testing.T, spec Spec) []byte {
	t.Helper()
	raw := mustJSON(t, spec)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(encoded)
		buf.WriteByte(':')
		buf.Write(obj[key])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func apiDo(t *testing.T, server *httptest.Server, method, path, actor string, body []byte) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if actor != "" {
		req.Header.Set(ActorHeader, actor)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func mustDefinition(t *testing.T, raw []byte) Version {
	t.Helper()
	var doc definitionResponse
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("definition json: %s", raw)
	}
	return doc.Definition
}

func apiCode(t *testing.T, raw []byte) string {
	t.Helper()
	var doc errorResponse
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("error json: %s", raw)
	}
	return doc.Error.Code
}

func assertAPIField(t *testing.T, raw []byte, field, message string) {
	t.Helper()
	var doc errorResponse
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("error json: %s", raw)
	}
	for _, item := range doc.Error.Fields {
		if item.Field == field && strings.Contains(item.Message, message) {
			return
		}
	}
	t.Fatalf("missing %s (%s) in %s", field, message, raw)
}
