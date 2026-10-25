package definitions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

func TestFileDefinitionsRemainV1(t *testing.T) {
	raw := readRepoFile(t, filepath.Join("examples", "definitions.json"))
	var defs []contracts.AgentDefinition
	if err := json.Unmarshal(raw, &defs); err != nil {
		t.Fatal(err)
	}
	if len(defs) != 10 {
		t.Fatalf("file definitions: %d", len(defs))
	}
	specs, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != len(defs) {
		t.Fatalf("parsed %d", len(specs))
	}
	for i := range defs {
		if specs[i].ID != defs[i].ID || specs[i].Version != defs[i].Version || specs[i].Name != defs[i].Name {
			t.Fatalf("spec %d does not match v1 definition", i)
		}
		if specs[i].ModelPolicy != nil {
			t.Fatalf("%s gained a model policy", specs[i].ID)
		}
	}
}

func TestImportExampleDefinitions(t *testing.T) {
	ctx := context.Background()
	reg := mustRegistry(t)
	raw := readRepoFile(t, filepath.Join("examples", "definitions.json"))
	first, err := reg.Import(ctx, "migration", raw, ImportPublish)
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 10 || first.Unchanged != 0 {
		t.Fatalf("created %d unchanged %d", first.Created, first.Unchanged)
	}
	second, err := reg.Import(ctx, "migration", raw, ImportPublish)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created != 0 || second.Unchanged != 10 {
		t.Fatalf("reimport created %d unchanged %d", second.Created, second.Unchanged)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Spec{}
	for _, spec := range parsed {
		byID[spec.ID] = spec
	}
	for _, version := range first.Versions {
		want, _, err := Canonical(byID[version.Spec.ID])
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := Canonical(version.Spec)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("%s canonical mismatch\n%s\n%s", version.Spec.ID, want, got)
		}
		if version.State != StatePublished || version.PublishedBy != "migration" || version.PublishedAt == nil {
			t.Fatalf("%s lifecycle %+v", version.Spec.ID, version)
		}
		var def contracts.AgentDefinition
		if err := json.Unmarshal(got, &def); err != nil {
			t.Fatal(err)
		}
		if def.ID != version.Spec.ID || def.Role != version.Spec.Role {
			t.Fatalf("v1 round trip %s", version.Spec.ID)
		}
	}
	denial, err := reg.Get(ctx, "denial-specialist", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(denial.Spec.AllowedDelegates, ",") != "catalog-specialist" {
		t.Fatalf("delegates %v", denial.Spec.AllowedDelegates)
	}
	summaries, err := reg.PublishedSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 10 {
		t.Fatalf("summaries %d", len(summaries))
	}
}

func TestCanonicalKeyOrderAndToolOrder(t *testing.T) {
	left := specialist("s", "1.0.0", "S")
	left.InputSchema = json.RawMessage(`{"additionalProperties":false,"type":"object"}`)
	right := left
	right.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
	a, _, err := Canonical(left)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := Canonical(right)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("schema key order changed hash\n%s\n%s", a, b)
	}
	left.AllowedTools = []string{"catalog.search", "catalog.other"}
	right.AllowedTools = []string{"catalog.other", "catalog.search"}
	_, leftHash, err := Canonical(left)
	if err != nil {
		t.Fatal(err)
	}
	_, rightHash, err := Canonical(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash == rightHash {
		t.Fatal("tool order was ignored")
	}
}

func TestPublishInvalidFieldErrors(t *testing.T) {
	ctx := context.Background()
	reg := mustRegistry(t)
	bad := specialist("broken", "1.0.0", "Broken")
	bad.Name = ""
	bad.Limits.MaxTokens = 0
	bad.Limits.MaxCostUSD = 0
	bad.InputSchema = json.RawMessage(`{"type":"object","format":"date","additionalProperties":{"type":"string"}}`)
	bad.AllowedTools = []string{"missing.tool"}
	bad.AllowedDelegates = []string{"no-such-agent", "broken"}
	_, err := reg.Publish(ctx, "ada", bad)
	regErr := registryErr(t, err)
	if regErr.Code != CodeInvalidInput {
		t.Fatalf("code %s", regErr.Code)
	}
	for _, want := range []struct{ field, message string }{
		{"name", "required"},
		{"limits.max_tokens", "greater than zero"},
		{"limits.max_cost_usd", "greater than zero"},
		{"input_schema.format", "unsupported"},
		{"input_schema.additionalProperties", "boolean"},
		{"allowed_tools[0]", "unknown tool"},
		{"allowed_delegates[0]", "unknown delegate"},
		{"allowed_delegates[1]", "itself"},
	} {
		if !hasField(err, want.field, want.message) {
			t.Fatalf("missing %s (%s) in %#v", want.field, want.message, regErr.Fields)
		}
	}
	if _, err := reg.Get(ctx, "broken", "1.0.0"); registryErr(t, err).Code != CodeNotFound {
		t.Fatal("invalid publish was stored")
	}
	_, err = reg.PublishJSON(ctx, "ada", []byte(`{"api_key":"secret","id":"broken","version":"1.0.0","name":"Broken","role":"specialist","instructions":"Use only authorized actions.","input_schema":{"type":"object"},"output_schema":{"type":"object"},"allowed_tools":[],"allowed_delegates":[],"limits":{"max_delegations":1,"max_concurrent_agents":1,"max_tool_calls":1,"max_model_turns":1,"max_tokens":"no","max_cost_usd":1,"timeout_seconds":1}}`))
	if !hasField(err, "api_key", "unknown") || !hasField(err, "limits.max_tokens", "number") {
		t.Fatalf("json fields %#v", registryErr(t, err).Fields)
	}
}

func TestLifecyclePinAndRetire(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)
	reg, err := NewMemory(WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.Close)

	child := specialist("catalog-specialist", "1.0.0", "Catalog specialist")
	parent := specialist("vendor-orchestrator", "1.0.0", "Vendor comparison orchestrator")
	parent.Role = "orchestrator"
	parent.AllowedDelegates = []string{"catalog-specialist"}
	parent.Instructions = "version A"
	if _, err := reg.SaveDraft(ctx, "ada", child); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SaveDraft(ctx, "ada", parent); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Publish(ctx, "ada", parent); !hasField(err, "allowed_delegates[0]", "published") {
		t.Fatalf("publish before delegate: %v", err)
	}
	tested, err := reg.Test(ctx, "ada", parent.ID, parent.Version)
	if err != nil {
		t.Fatal(err)
	}
	if tested.State != StateTesting || tested.TestedBy != "ada" || tested.TestedAt == nil || !tested.TestedAt.Equal(now) {
		t.Fatalf("test metadata %+v", tested)
	}
	if _, err := reg.Publish(ctx, "grace", child); err != nil {
		t.Fatal(err)
	}
	publishedAt := now
	published, err := reg.Publish(ctx, "grace", parent)
	if err != nil {
		t.Fatal(err)
	}
	if published.State != StatePublished || published.PublishedBy != "grace" || published.PublishedAt == nil || !published.PublishedAt.Equal(publishedAt) {
		t.Fatalf("publish metadata %+v", published)
	}
	if published.TestedBy != "ada" {
		t.Fatal("test record was cleared")
	}
	hashA := published.ContentHash
	now = now.Add(time.Minute)
	again, err := reg.Publish(ctx, "other", parent)
	if err != nil {
		t.Fatal(err)
	}
	if !again.PublishedAt.Equal(publishedAt) || again.PublishedBy != "grace" {
		t.Fatal("republish rewrote publisher")
	}
	if _, err := reg.SaveDraft(ctx, "ada", parent); registryErr(t, err).Code != CodeImmutable {
		t.Fatalf("in-place edit: %v", err)
	}
	if _, err := reg.Test(ctx, "ada", parent.ID, parent.Version); registryErr(t, err).Code != CodeInvalidInput {
		t.Fatalf("test published: %v", err)
	}
	revisedChild := child
	revisedChild.Version = "1.1.0"
	if _, err := reg.Revise(ctx, "ada", child.ID, child.Version, revisedChild); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Publish(ctx, "grace", revisedChild); err != nil {
		t.Fatal(err)
	}
	revised := parent
	revised.Version = "1.1.0"
	revised.Instructions = "version B"
	if _, err := reg.Revise(ctx, "ada", parent.ID, parent.Version, revised); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Publish(ctx, "grace", revised); err != nil {
		t.Fatal(err)
	}
	storedA, err := reg.Get(ctx, parent.ID, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if storedA.ContentHash != hashA || storedA.Spec.Instructions != "version A" {
		t.Fatal("version A changed after B was published")
	}
	pin, err := reg.PinRun(ctx, "run-1", parent.ID, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.PinRun(ctx, "run-1", child.ID, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	retireAt := now
	retired, err := reg.Retire(ctx, "ida", parent.ID, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if retired.State != StateRetired || retired.RetiredBy != "ida" || retired.RetiredAt == nil || !retired.RetiredAt.Equal(retireAt) {
		t.Fatalf("retire metadata %+v", retired)
	}
	if retired.ContentHash != hashA || retired.PublishedBy != "grace" {
		t.Fatal("retire rewrote the published spec")
	}
	now = now.Add(time.Minute)
	retiredAgain, err := reg.Retire(ctx, "ida", parent.ID, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !retiredAgain.RetiredAt.Equal(retireAt) {
		t.Fatal("second retire rewrote the timestamp")
	}
	pinned, err := reg.Pinned(ctx, "run-1", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.Version != "1.0.0" || pinned.ContentHash != hashA || pinned.Spec.Instructions != "version A" || pinned.ContentHash != pin.ContentHash {
		t.Fatalf("pin moved: %+v", pinned)
	}
	pins, err := reg.Pins(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 2 || pins[0].AgentID != child.ID || pins[1].ContentHash != hashA {
		t.Fatalf("pins %+v", pins)
	}
	if _, err := reg.ResolveForNewRun(ctx, parent.ID, "1.0.0"); registryErr(t, err).Code != CodeRetired {
		t.Fatalf("retired start: %v", err)
	}
	if _, err := reg.PinRun(ctx, "run-2", parent.ID, "1.0.0"); registryErr(t, err).Code != CodeRetired {
		t.Fatalf("pin retired: %v", err)
	}
	resolvedB, err := reg.ResolveForNewRun(ctx, parent.ID, "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if resolvedB.Spec.Instructions != "version B" || resolvedB.ContentHash == hashA {
		t.Fatal("version B was not the new run target")
	}
	summaries, err := reg.PublishedSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range summaries {
		if summary.ID == parent.ID && summary.Version == "1.0.0" {
			t.Fatal("retired version is still offered for new runs")
		}
	}
	if _, err := reg.Retire(ctx, "ida", revised.ID, revised.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Retire(ctx, "ida", child.ID, "9.9.9"); registryErr(t, err).Code != CodeNotFound {
		t.Fatalf("missing retire: %v", err)
	}
	draft := specialist("draft-only", "1.0.0", "Draft only")
	if _, err := reg.SaveDraft(ctx, "ada", draft); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Retire(ctx, "ida", draft.ID, draft.Version); registryErr(t, err).Code != CodeInvalidInput {
		t.Fatalf("retire draft: %v", err)
	}
}

func TestExportRoundTrip(t *testing.T) {
	ctx := context.Background()
	reg := mustRegistry(t)
	raw := readRepoFile(t, filepath.Join("examples", "definitions.json"))
	if _, err := reg.Import(ctx, "migration", raw, ImportPublish); err != nil {
		t.Fatal(err)
	}
	backup, err := reg.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(backup); err == nil {
		t.Fatal("backup was accepted by Parse")
	}
	other := mustRegistry(t)
	restored, err := other.Import(ctx, "ignored", backup, ImportDraft)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Created != 10 {
		t.Fatalf("restored %d", restored.Created)
	}
	again, err := other.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, again) {
		t.Fatalf("export was not stable\n%s\n%s", backup, again)
	}
	replay, err := other.Import(ctx, "ignored", backup, ImportPublish)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Unchanged != 10 || replay.Created != 0 {
		t.Fatalf("replay %+v", replay)
	}
	specs := make([]Spec, len(restored.Versions))
	for i, version := range restored.Versions {
		specs[i] = version.Spec
		if version.PublishedBy != "migration" {
			t.Fatalf("backup rewrote publisher to %s", version.PublishedBy)
		}
	}
	envelope, err := MarshalDefinitions(specs)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 10 {
		t.Fatalf("envelope %d", len(parsed))
	}
}

func TestImportConflictRollsBack(t *testing.T) {
	ctx := context.Background()
	reg := mustRegistry(t)
	original := specialist("alpha", "1.0.0", "Alpha")
	if _, err := reg.Publish(ctx, "ada", original); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Instructions = "changed instructions"
	extra := specialist("beta", "1.0.0", "Beta")
	body, err := json.Marshal([]Spec{extra, changed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Import(ctx, "ada", body, ImportPublish); err == nil {
		t.Fatal("conflict was accepted")
	}
	if _, err := reg.Get(ctx, "beta", "1.0.0"); registryErr(t, err).Code != CodeNotFound {
		t.Fatalf("beta survived rollback: %v", err)
	}
	stored, err := reg.Get(ctx, "alpha", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Spec.Instructions != original.Instructions {
		t.Fatal("alpha was partially updated")
	}
}

func TestModelPolicyNameAndToolRules(t *testing.T) {
	ctx := context.Background()
	reg := mustRegistry(t)
	spec := specialist("model-agent", "1.0.0", "Model agent")
	spec.ModelPolicy = &ModelPolicy{DefaultModel: "openai/gpt-test", AllowedModels: []string{"meta/muse", "openai/gpt-test"}}
	published, err := reg.Publish(ctx, "ada", spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := published.AllowsModel(""); err != nil {
		t.Fatal(err)
	}
	if err := published.AllowsModel("meta/muse"); err != nil {
		t.Fatal(err)
	}
	if err := published.AllowsModel("other/model"); err == nil {
		t.Fatal("disallowed model was accepted")
	}
	swapped := spec
	swapped.ModelPolicy = &ModelPolicy{DefaultModel: "openai/gpt-test", AllowedModels: []string{"openai/gpt-test", "meta/muse"}}
	_, hashA, err := Canonical(spec)
	if err != nil {
		t.Fatal(err)
	}
	_, hashB, err := Canonical(swapped)
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB {
		t.Fatal("allowed model order changed the hash")
	}
	bad := spec
	bad.Version = "1.0.1"
	bad.ModelPolicy = &ModelPolicy{DefaultModel: "openai/gpt-test", AllowedModels: []string{"meta/muse"}}
	if _, err := reg.Publish(ctx, "ada", bad); !hasField(err, "model_policy.default_model", "allowed_models") {
		t.Fatalf("default model: %v", err)
	}
	taken := specialist("other-agent", "1.0.0", "Model agent")
	if _, err := reg.SaveDraft(ctx, "ada", taken); !hasField(err, "name", "unique") {
		t.Fatalf("name conflict: %v", err)
	}
	replacement := builtinCatalogTool()
	replacement.Description = "changed"
	if err := reg.RegisterTool(ctx, replacement); registryErr(t, err).Code != CodeImmutable {
		t.Fatalf("tool rewrite: %v", err)
	}
	restricted := specialist("restricted", "1.0.0", "Restricted")
	restricted.ModelPolicy = &ModelPolicy{AllowedModels: []string{"meta/muse"}}
	stored, err := reg.Publish(ctx, "ada", restricted)
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.AllowsModel(""); err == nil {
		t.Fatal("empty model bypassed an allow list without a default")
	}
}

func TestBackupHashMismatch(t *testing.T) {
	ctx := context.Background()
	reg := mustRegistry(t)
	spec := specialist("alpha", "1.0.0", "Alpha")
	if _, err := reg.Publish(ctx, "ada", spec); err != nil {
		t.Fatal(err)
	}
	backup, err := reg.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(backup, &doc); err != nil {
		t.Fatal(err)
	}
	versions := doc["versions"].([]any)
	version := versions[0].(map[string]any)
	version["content_hash"] = strings.Repeat("ab", 32)
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	other := mustRegistry(t)
	if _, err := other.Import(ctx, "ada", body, ImportPublish); !hasField(err, "versions[0].content_hash", "match") {
		t.Fatalf("tampered backup: %v", err)
	}
}

func TestMigrationContract(t *testing.T) {
	sqlText := migrationSQL(t)
	for _, want := range []string{
		"nmc3.agent_definitions",
		"nmc3.agent_versions",
		"nmc3.tool_definitions",
		"nmc3.tool_versions",
		"nmc3.run_definition_pins",
		"prevent_definition_version_mutation",
		"prevent_row_mutation",
		"published definition version is immutable",
		"retired definition version is immutable",
		"'draft'",
		"'testing'",
		"'published'",
		"'retired'",
		"service_role",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("migration missing %s", want)
		}
	}
}

func mustRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.Close)
	return reg
}

func specialist(id, version, name string) Spec {
	return Spec{AgentDefinition: contracts.AgentDefinition{
		ID: id, Version: version, Name: name, Role: "specialist",
		Instructions:     "Use only authorized actions.",
		InputSchema:      json.RawMessage(`{"additionalProperties":false,"type":"object"}`),
		OutputSchema:     json.RawMessage(`{"additionalProperties":false,"type":"object"}`),
		AllowedTools:     []string{},
		AllowedDelegates: []string{},
		Limits: contracts.Limits{
			MaxDelegations: 2, MaxConcurrentAgents: 2, MaxToolCalls: 4,
			MaxModelTurns: 8, MaxTokens: 12000, MaxCostUSD: 2, TimeoutSeconds: 60,
		},
	}}
}

func registryErr(t *testing.T, err error) *Error {
	t.Helper()
	var reg *Error
	if !errors.As(err, &reg) {
		t.Fatalf("error %v", err)
	}
	return reg
}

func hasField(err error, field, message string) bool {
	var reg *Error
	if !errors.As(err, &reg) {
		return false
	}
	for _, item := range reg.Fields {
		if item.Field == field && strings.Contains(item.Message, message) {
			return true
		}
	}
	return false
}

func readRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", rel)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func migrationSQL(t *testing.T) string {
	t.Helper()
	return string(readRepoFile(t, filepath.Join("db", "migrations", "202610240006_nmc3_definitions.sql")))
}
