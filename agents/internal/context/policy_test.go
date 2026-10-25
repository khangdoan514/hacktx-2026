package context_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	agentctx "github.com/khangdoan514/hacktx-2026/agents/internal/context"
)

func TestMemoryWriteReadDelete(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	ctx := context.Background()
	readOnly := v1(agentctx.DefinitionGrant{Scope: agentctx.ScopeAgentMemory, Purposes: []agentctx.Purpose{agentctx.PurposeReadMemory}})
	readWrite := v1(agentctx.DefinitionGrant{Scope: agentctx.ScopeAgentMemory, Purposes: []agentctx.Purpose{
		agentctx.PurposeReadMemory, agentctx.PurposeWriteMemory, agentctx.PurposeDeleteMemory,
	}})
	writeOnly := v1(agentctx.DefinitionGrant{Scope: agentctx.ScopeAgentMemory, Purposes: []agentctx.Purpose{agentctx.PurposeWriteMemory}})
	e.grant(t, "g-write", "researcher", "researcher", agentctx.ScopeAgentMemory, agentctx.PurposeWriteMemory)
	if _, err := e.r.WriteMemory(ctx, agentctx.MemoryWrite{
		Principal: e.user, Grants: readOnly, ID: "mem-1", AgentID: "researcher", Text: "KEEP_MEMORY",
	}); codeOf(t, err) != agentctx.CodeDenied {
		t.Fatal(err)
	}
	if _, err := e.r.WriteMemory(ctx, agentctx.MemoryWrite{
		Principal: e.user, Grants: writeOnly, ID: "mem-1", AgentID: "researcher", Text: "KEEP_MEMORY",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.WriteMemory(ctx, agentctx.MemoryWrite{
		Principal: e.user, Grants: writeOnly, AgentID: "other-agent", Text: "LEAK_OTHER_MEMORY",
	}); codeOf(t, err) != agentctx.CodeDenied {
		t.Fatal(err)
	}

	noRead := mustBundle(t)(e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, Grants: writeOnly, AgentID: "researcher", RunID: "run-1", InvocationID: "inv-1",
	}))
	assertNoLeak(t, noRead, "KEEP_MEMORY")
	if code, ok := denialCode(noRead, agentctx.ScopeAgentMemory, "researcher"); ok {
		t.Fatalf("write-only definition should not request a read, denials %+v code %s", noRead.Denials, code)
	}

	e.grant(t, "g-read", "researcher", "researcher", agentctx.ScopeAgentMemory, agentctx.PurposeReadMemory)
	first := mustBundle(t)(e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, Grants: readWrite, AgentID: "researcher", RunID: "run-1", InvocationID: "inv-1",
	}))
	if !hasText(first, "KEEP_MEMORY") {
		t.Fatal("granted memory was not readable")
	}
	if err := e.r.DeleteMemory(ctx, agentctx.MemoryDelete{
		Principal: e.user, Grants: writeOnly, AgentID: "researcher", ID: "mem-1",
	}); codeOf(t, err) != agentctx.CodeDenied {
		t.Fatal(err)
	}
	e.grant(t, "g-delete", "researcher", "researcher", agentctx.ScopeAgentMemory, agentctx.PurposeDeleteMemory)
	if err := e.r.DeleteMemory(ctx, agentctx.MemoryDelete{
		Principal: e.user, Grants: readWrite, AgentID: "researcher", ID: "mem-1",
	}); err != nil {
		t.Fatal(err)
	}
	second := mustBundle(t)(e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, Grants: readWrite, AgentID: "researcher", RunID: "run-2", InvocationID: "inv-2",
	}))
	assertNoLeak(t, second, "KEEP_MEMORY")
	snap, err := e.r.Inspect(ctx, e.inspector, first.SnapshotID)
	if err != nil || !hasText(snap.Bundle, "KEEP_MEMORY") {
		t.Fatalf("deleted memory erased history: %+v err %v", snap, err)
	}
}

func TestGrantExpiryBlocksNewReads(t *testing.T) {
	t.Parallel()
	ret := agentctx.NewStaticRetriever(agentctx.Hit{
		TenantID: "tenant-a", CollectionID: "vendors", DocumentID: "doc-1", Title: "LEAK_EXPIRED_TITLE", Text: "VISIBLE_UNTIL_EXPIRY",
	})
	e := newEnv(t, ret)
	expiry := e.clock.Now().Add(time.Minute)
	if _, err := e.r.PutGrant(context.Background(), e.admin, agentctx.Grant{
		ID: "g-org", PrincipalID: e.user.ID, AgentID: "orchestrator", Scope: agentctx.ScopeOrganization,
		ResourceID: "vendors", Purposes: []agentctx.Purpose{agentctx.PurposeRetrieve}, ExpiresAt: &expiry,
	}); err != nil {
		t.Fatal(err)
	}
	def := v1(agentctx.DefinitionGrant{Scope: agentctx.ScopeOrganization, Resource: "vendors", Purposes: []agentctx.Purpose{agentctx.PurposeRetrieve}})
	req := agentctx.ResolveRequest{
		Principal: e.user, Grants: def, AgentID: "orchestrator", RunID: "run-1", InvocationID: "inv-1",
		Collections: []string{"vendors"},
	}
	first := mustBundle(t)(e.r.Resolve(context.Background(), req))
	if !hasText(first, "VISIBLE_UNTIL_EXPIRY") {
		t.Fatal("expected text before expiry")
	}
	e.clock.Advance(time.Minute)
	calls := len(ret.Calls())
	second := mustBundle(t)(e.r.Resolve(context.Background(), req))
	code, ok := denialCode(second, agentctx.ScopeOrganization, "vendors")
	if !ok || code != agentctx.CodeGrantExpired || len(ret.Calls()) != calls {
		t.Fatalf("expiry denials %+v calls %d", second.Denials, len(ret.Calls()))
	}
	assertNoLeak(t, second, "VISIBLE_UNTIL_EXPIRY", "LEAK_EXPIRED_TITLE")
	snap, err := e.r.Inspect(context.Background(), e.inspector, first.SnapshotID)
	if err != nil || !hasText(snap.Bundle, "VISIBLE_UNTIL_EXPIRY") {
		t.Fatal("expired grant hid a retained snapshot")
	}
}

func TestLargeArtifactChecksumAndDeletion(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	ctx := context.Background()
	def := v1(agentctx.DefinitionGrant{Scope: agentctx.ScopeRun, Purposes: []agentctx.Purpose{
		agentctx.PurposeResolve, agentctx.PurposeWriteArtifact, agentctx.PurposeDeleteArtifact,
	}})
	e.grant(t, "g-run", "orchestrator", "run-1", agentctx.ScopeRun, agentctx.PurposeResolve, agentctx.PurposeWriteArtifact, agentctx.PurposeDeleteArtifact)
	body := []byte(strings.Repeat("A", agentctx.MaxInlineBytes+1))
	ref, err := e.r.PutArtifact(ctx, agentctx.ArtifactWrite{
		Principal: e.user, Grants: def, ID: "artifact-large", AgentID: "orchestrator", RunID: "run-1",
		Name: "large-notes", MediaType: "text/plain", Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ObjectKey == "" || ref.ByteSize != len(body) || ref.Checksum != agentctx.ContentHash(string(body)) {
		t.Fatalf("reference = %+v", ref)
	}
	specDef := v1(
		agentctx.DefinitionGrant{Scope: agentctx.ScopeStep, Purposes: []agentctx.Purpose{agentctx.PurposeResolve}},
		agentctx.DefinitionGrant{Scope: agentctx.ScopeRun, Purposes: []agentctx.Purpose{agentctx.PurposeResolve}},
	)
	e.grant(t, "g-spec-step", "specialist", "inv-spec", agentctx.ScopeStep, agentctx.PurposeResolve)
	e.grant(t, "g-spec-run", "specialist", "run-1", agentctx.ScopeRun, agentctx.PurposeResolve)
	parent := mustBundle(t)(e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, Grants: def, AgentID: "orchestrator", RunID: "run-1", InvocationID: "inv-orch",
		AllowedTools: []string{"catalog.read"},
	}))
	if !hasText(parent, string(body)) || parent.Fragments[0].Provenance.ObjectKey != ref.ObjectKey {
		t.Fatal("large artifact was not loaded through its reference")
	}

	if err := e.objects.Put(ctx, "tenant-a", ref.ObjectKey, []byte("CORRUPTED_SECRET_BODY")); err != nil {
		t.Fatal(err)
	}
	corrupted := mustBundle(t)(e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, Grants: def, AgentID: "orchestrator", RunID: "run-1", InvocationID: "inv-orch",
	}))
	code, ok := denialCode(corrupted, agentctx.ScopeRun, "artifact-large")
	if !ok || code != agentctx.CodeInternal {
		t.Fatalf("denials %+v", corrupted.Denials)
	}
	assertNoLeak(t, corrupted, "CORRUPTED_SECRET_BODY", strings.Repeat("A", 64))
	original, err := e.r.Inspect(ctx, e.inspector, parent.SnapshotID)
	if err != nil || !hasText(original.Bundle, string(body)) {
		t.Fatal("checksum failure rewrote history")
	}

	if err := e.objects.Put(ctx, "tenant-a", ref.ObjectKey, body); err != nil {
		t.Fatal(err)
	}
	restored := mustBundle(t)(e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, Grants: def, AgentID: "orchestrator", RunID: "run-1", InvocationID: "inv-orch-2",
	}))
	if err := e.r.DeleteArtifact(ctx, agentctx.ArtifactDelete{
		Principal: e.user, Grants: def, AgentID: "orchestrator", RunID: "run-1", ID: "artifact-large",
	}); err != nil {
		t.Fatal(err)
	}
	child := mustBundle(t)(e.r.Forward(ctx, agentctx.ForwardRequest{
		Principal: e.user, Parent: restored, Grants: specDef, AgentID: "specialist", InvocationID: "inv-spec",
		StepInput: "task", ArtifactIDs: []string{"artifact-large"}, AllowedTools: []string{"catalog.read"},
	}))
	assertNoLeak(t, child, strings.Repeat("A", 64), "large-notes")
	if hasText(restored, string(body)) == false {
		t.Fatal("parent bundle value lost its copy")
	}
	snap, err := e.r.Inspect(ctx, e.inspector, restored.SnapshotID)
	if err != nil || !hasText(snap.Bundle, string(body)) {
		t.Fatal("artifact deletion erased the retained snapshot")
	}
}

func TestLegacySnapshotAndDefinition(t *testing.T) {
	t.Parallel()
	e := newEnv(t, agentctx.NewStaticRetriever(agentctx.Hit{
		TenantID: "tenant-a", CollectionID: "vendors", DocumentID: "doc-legacy", Title: "LEAK_LEGACY_TITLE", Text: "LEAK_LEGACY_BODY",
	}))
	ctx := context.Background()
	legacyGrants := agentctx.VersionedGrants{Version: 0, Grants: []agentctx.DefinitionGrant{
		{Scope: agentctx.ScopeStep, Purposes: []agentctx.Purpose{agentctx.PurposeResolve}},
		{Scope: agentctx.ScopeOrganization, Resource: "vendors", Purposes: []agentctx.Purpose{agentctx.PurposeRetrieve}},
	}}
	if _, err := e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, Grants: legacyGrants, AgentID: "orchestrator", RunID: "run-1", InvocationID: "inv-1",
	}); codeOf(t, err) != agentctx.CodeInvalid {
		t.Fatal(err)
	}
	e.grant(t, "g-step", "orchestrator", "inv-legacy", agentctx.ScopeStep, agentctx.PurposeResolve)
	bundle := mustBundle(t)(e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, Grants: agentctx.VersionedGrants{Version: 0, Grants: []agentctx.DefinitionGrant{
			{Scope: agentctx.ScopeStep, Purposes: []agentctx.Purpose{agentctx.PurposeResolve}},
		}},
		AgentID: "orchestrator", RunID: "run-legacy", InvocationID: "inv-legacy", StepInput: "legacy step",
		Collections: []string{"vendors"},
	}))
	if textsOf(bundle) == nil || textsOf(bundle)[0] != "legacy step" {
		t.Fatalf("legacy step = %q", textsOf(bundle))
	}
	assertNoLeak(t, bundle, "LEAK_LEGACY_BODY", "LEAK_LEGACY_TITLE")

	now := e.clock.Now()
	imported := agentctx.Snapshot{
		Version: 0, ID: "snap-legacy", TenantID: "tenant-a", RunID: "run-old", InvocationID: "inv-old", AgentID: "orchestrator",
		CreatedAt: now, RetainUntil: now.Add(time.Hour),
		Bundle: agentctx.Bundle{
			Version: 0, TenantID: "tenant-a", RunID: "run-old", InvocationID: "inv-old", AgentID: "orchestrator",
			Fragments: []agentctx.Fragment{{
				ID: "frag-old", Scope: agentctx.ScopeStep, Text: "old step without memory", Trust: agentctx.TrustUntrusted,
				Provenance: agentctx.Provenance{Scope: agentctx.ScopeStep, SourceKind: agentctx.SourceStep, SourceID: "inv-old", ContentHash: agentctx.ContentHash("old step without memory")},
			}},
		},
	}
	if err := e.r.PutSnapshot(ctx, e.inspector, imported); err != nil {
		t.Fatal(err)
	}
	snap, err := e.r.Inspect(ctx, e.inspector, "snap-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Version != 0 || snap.MemoryRefs != nil || !hasText(snap.Bundle, "old step without memory") {
		t.Fatalf("legacy snapshot = %+v refs %#v", snap.Version, snap.MemoryRefs)
	}
}

type revokeRetriever struct {
	inner  *agentctx.StaticRetriever
	revoke func()
}

func (r revokeRetriever) Name() string { return r.inner.Name() }

func (r revokeRetriever) Retrieve(ctx context.Context, query agentctx.RetrievalQuery) ([]agentctx.Hit, error) {
	hits, err := r.inner.Retrieve(ctx, query)
	if r.revoke != nil {
		r.revoke()
	}
	return hits, err
}

func TestRevokeDuringRetrieveDropsHits(t *testing.T) {
	t.Parallel()
	inner := agentctx.NewStaticRetriever(agentctx.Hit{
		TenantID: "tenant-a", CollectionID: "vendors", DocumentID: "doc-1", Title: "LEAK_RACE_TITLE", Text: "LEAK_RACE_BODY",
	})
	e := newEnv(t, nil)
	ret := revokeRetriever{inner: inner, revoke: func() {
		if _, err := e.r.RevokeGrant(context.Background(), e.admin, "g-org"); err != nil {
			t.Errorf("revoke: %v", err)
		}
	}}
	e.r = agentctx.New(
		agentctx.WithClock(e.clock.Now),
		agentctx.WithRetention(time.Hour),
		agentctx.WithObjectStore(e.objects),
		agentctx.WithRetriever(ret),
	)
	def := v1(agentctx.DefinitionGrant{Scope: agentctx.ScopeOrganization, Resource: "vendors", Purposes: []agentctx.Purpose{agentctx.PurposeRetrieve}})
	e.grant(t, "g-org", "orchestrator", "vendors", agentctx.ScopeOrganization, agentctx.PurposeRetrieve)
	bundle := mustBundle(t)(e.r.Resolve(context.Background(), agentctx.ResolveRequest{
		Principal: e.user, Grants: def, AgentID: "orchestrator", RunID: "run-1", InvocationID: "inv-1",
		Collections: []string{"vendors"}, AllowedTools: []string{"catalog.read"},
	}))
	code, ok := denialCode(bundle, agentctx.ScopeOrganization, "vendors")
	if !ok || code != agentctx.CodeGrantRevoked || len(bundle.Fragments) != 0 {
		t.Fatalf("in-flight revoke published hits: %+v texts %q", bundle.Denials, textsOf(bundle))
	}
	assertNoLeak(t, bundle, "LEAK_RACE_BODY", "LEAK_RACE_TITLE")
	if bundle.ToolAllowed("secret.export") {
		t.Fatal("tool permission changed")
	}
}

func TestConcurrentResolveAndRevoke(t *testing.T) {
	t.Parallel()
	ret := agentctx.NewStaticRetriever(agentctx.Hit{
		TenantID: "tenant-a", CollectionID: "vendors", DocumentID: "doc-1", Text: "catalog note",
	})
	e := newEnv(t, ret)
	def := v1(agentctx.DefinitionGrant{Scope: agentctx.ScopeOrganization, Resource: "vendors", Purposes: []agentctx.Purpose{agentctx.PurposeRetrieve}})
	e.grant(t, "g-org", "orchestrator", "vendors", agentctx.ScopeOrganization, agentctx.PurposeRetrieve)
	req := agentctx.ResolveRequest{
		Principal: e.user, Grants: def, AgentID: "orchestrator", RunID: "run-1", InvocationID: "inv-1",
		Collections: []string{"vendors"}, AllowedTools: []string{"catalog.read"},
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < 20; n++ {
				bundle, err := e.r.Resolve(context.Background(), req)
				if err != nil {
					t.Errorf("resolve: %v", err)
					return
				}
				if bundle.ToolAllowed("secret.export") {
					t.Error("tool granted during concurrent resolve")
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		if _, err := e.r.RevokeGrant(context.Background(), e.admin, "g-org"); err != nil {
			t.Errorf("revoke: %v", err)
		}
	}()
	close(start)
	wg.Wait()
	final := mustBundle(t)(e.r.Resolve(context.Background(), req))
	if hasText(final, "catalog note") {
		t.Fatal("revoked grant still returned collection text")
	}
	assertNoLeak(t, final, "catalog note")
}

func TestResolveHonorsCancellation(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bundle, err := e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: e.user, AgentID: "orchestrator", RunID: "run-1", InvocationID: "inv-1",
		StepInput: "LEAK_CANCELED_STEP",
	})
	if !errorsIsCanceled(err) || bundle.SnapshotID != "" || len(bundle.Fragments) != 0 {
		t.Fatalf("canceled resolve = %+v err %v", bundle, err)
	}
	assertNoLeak(t, bundle, "LEAK_CANCELED_STEP")
}

func errorsIsCanceled(err error) bool {
	return err == context.Canceled
}

func TestRetentionDoesNotCrossTenants(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	ctx := context.Background()
	adminB := agentctx.Principal{TenantID: "tenant-b", ID: "admin-b", Roles: []string{agentctx.RoleAdmin}}
	userB := agentctx.Principal{TenantID: "tenant-b", ID: "user-b"}
	inspectorB := agentctx.Principal{TenantID: "tenant-b", ID: "inspector-b", Roles: []string{agentctx.RoleInspector}}
	if _, err := e.r.PutGrant(ctx, adminB, agentctx.Grant{
		ID: "g-b", PrincipalID: userB.ID, AgentID: "orchestrator", Scope: agentctx.ScopeStep,
		ResourceID: "inv-b", Purposes: []agentctx.Purpose{agentctx.PurposeResolve},
	}); err != nil {
		t.Fatal(err)
	}
	def := v1(agentctx.DefinitionGrant{Scope: agentctx.ScopeStep, Purposes: []agentctx.Purpose{agentctx.PurposeResolve}})
	bundle := mustBundle(t)(e.r.Resolve(ctx, agentctx.ResolveRequest{
		Principal: userB, Grants: def, AgentID: "orchestrator", RunID: "run-b", InvocationID: "inv-b",
		StepInput: "TENANT_B_STEP",
	}))
	e.clock.Advance(2 * time.Hour)
	if _, err := e.r.DeleteExpired(ctx, e.operator); err != nil {
		t.Fatal(err)
	}
	snap, err := e.r.Inspect(ctx, inspectorB, bundle.SnapshotID)
	if err != nil || !hasText(snap.Bundle, "TENANT_B_STEP") || snap.RedactedAt != nil {
		t.Fatalf("tenant A retention changed tenant B snapshot: %+v err %v", snap, err)
	}
}
