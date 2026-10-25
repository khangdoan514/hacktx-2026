package context

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"time"
	"unicode/utf8"
)

// Resolver assembles scoped context and retains an explanation of each read.
type Resolver struct {
	grants    *grantIndex
	artifacts *artifactIndex
	memories  *memoryIndex
	snaps     *snapshotIndex
	objects   ObjectStore
	retriever Retriever
	nowFn     func() time.Time
	retention time.Duration
}

// Option configures a Resolver.
type Option func(*Resolver)

// WithClock sets the clock used for grant expiry, retention, and provenance.
func WithClock(now func() time.Time) Option {
	return func(r *Resolver) { r.nowFn = now }
}

// WithRetention sets how long snapshot text remains readable.
func WithRetention(d time.Duration) Option {
	return func(r *Resolver) {
		if d > 0 {
			r.retention = d
		}
	}
}

// WithRetriever sets the organization retrieval adapter.
func WithRetriever(ret Retriever) Option {
	return func(r *Resolver) { r.retriever = ret }
}

// WithObjectStore sets storage for large artifact and memory bodies.
func WithObjectStore(store ObjectStore) Option {
	return func(r *Resolver) { r.objects = store }
}

// New returns a resolver with in-memory grants, artifacts, memory, snapshots,
// and object storage. The default retriever is empty.
func New(opts ...Option) *Resolver {
	resolver := &Resolver{
		grants:    newGrantIndex(),
		artifacts: newArtifactIndex(),
		memories:  newMemoryIndex(),
		snaps:     newSnapshotIndex(),
		objects:   NewMemoryObjectStore(),
		retriever: NewStaticRetriever(),
		retention: DefaultRetention,
	}
	for _, opt := range opts {
		opt(resolver)
	}
	if resolver.objects == nil {
		resolver.objects = NewMemoryObjectStore()
	}
	if resolver.retriever == nil {
		resolver.retriever = NewStaticRetriever()
	}
	if resolver.retention <= 0 {
		resolver.retention = DefaultRetention
	}
	return resolver
}

func (r *Resolver) now() time.Time {
	if r.nowFn == nil {
		return time.Now().UTC()
	}
	return r.nowFn().UTC()
}

// ResolveRequest asks for one invocation's context.
// Depth 0 reads every run artifact the grant allows. Depth 1 reads only
// ArtifactIDs. Depth must be supplied by the coordinator, not by model text.
type ResolveRequest struct {
	Principal     Principal
	Grants        VersionedGrants
	AgentID       string
	RunID         string
	InvocationID  string
	Depth         int
	StepInput     string
	Query         string
	Collections   []string
	ArtifactIDs   []string
	AllowedTools  []string
	ForwardedFrom string
}

// ForwardRequest selects orchestrator context for one specialist.
// The specialist does not inherit the parent transcript, tool allowlist, or
// unselected artifacts. RunID is taken from the parent bundle.
type ForwardRequest struct {
	Principal    Principal
	Parent       Bundle
	Grants       VersionedGrants
	AgentID      string
	InvocationID string
	StepInput    string
	ArtifactIDs  []string
	AllowedTools []string
	Query        string
	Collections  []string
}

// ArtifactWrite stores a run artifact. Bodies larger than MaxInlineBytes are
// stored by object reference.
type ArtifactWrite struct {
	Principal      Principal
	Grants         VersionedGrants
	ID             string
	AgentID        string
	RunID          string
	InvocationID   string
	Name           string
	MediaType      string
	Classification string
	Body           []byte
	TTL            time.Duration
}

// ArtifactDelete removes an artifact from new reads.
type ArtifactDelete struct {
	Principal Principal
	Grants    VersionedGrants
	AgentID   string
	RunID     string
	ID        string
}

// MemoryWrite stores agent memory. The body is untrusted data.
type MemoryWrite struct {
	Principal Principal
	Grants    VersionedGrants
	ID        string
	AgentID   string
	Topic     string
	Text      string
	TTL       time.Duration
}

// MemoryDelete removes agent memory from new reads.
type MemoryDelete struct {
	Principal Principal
	Grants    VersionedGrants
	AgentID   string
	ID        string
}

// RetentionResult counts records expired by DeleteExpired.
type RetentionResult struct {
	SnapshotsRedacted int
	MemoriesDeleted   int
	ArtifactsDeleted  int
}

// Resolve builds the invocation bundle and stores a snapshot.
// Denied scopes contribute neither content nor retrieved metadata.
// AllowedTools is copied from the request before any retrieval and is not
// modified from fragment text.
func (r *Resolver) Resolve(ctx context.Context, req ResolveRequest) (Bundle, error) {
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	if err := validateResolve(req); err != nil {
		return Bundle{}, err
	}
	now := r.now()
	bundle := Bundle{
		Version:      BundleSchemaVersion,
		TenantID:     req.Principal.TenantID,
		RunID:        req.RunID,
		InvocationID: req.InvocationID,
		AgentID:      req.AgentID,
		Depth:        req.Depth,
		AllowedTools: cloneStrings(req.AllowedTools),
		ResolvedAt:   now,
	}
	allowlist := cloneStrings(bundle.AllowedTools)

	if req.StepInput != "" {
		r.attachStep(&bundle, req, now)
	}
	r.attachRun(ctx, &bundle, req, now)
	r.attachMemory(ctx, &bundle, req, now)
	if err := r.attachOrganization(ctx, &bundle, req, now); err != nil {
		return Bundle{}, err
	}
	if !sameStrings(allowlist, bundle.AllowedTools) {
		return Bundle{}, &Error{Code: CodeInternal, Message: codeMessage(CodeInternal)}
	}
	r.record(&bundle, now)
	return cloneBundle(bundle), nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// Forward copies the parent's selected run-artifact IDs into a depth-1 resolve.
// Fragment text is reloaded from the store under the specialist's grants.
// Parent step, memory, organization, and allowlist values are not copied.
func (r *Resolver) Forward(ctx context.Context, req ForwardRequest) (Bundle, error) {
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	if req.Parent.Depth != 0 {
		return Bundle{}, &Error{Code: CodeDenied, Message: "only an orchestrator bundle can be forwarded", Scope: ScopeRun}
	}
	if req.Parent.TenantID == "" || req.Principal.TenantID == "" || req.Parent.TenantID != req.Principal.TenantID {
		return Bundle{}, codeError(CodeDenied, ScopeRun, "", "")
	}
	if req.Parent.RunID == "" || req.Parent.InvocationID == "" {
		return Bundle{}, invalid("parent bundle is missing run identity")
	}
	return r.Resolve(ctx, ResolveRequest{
		Principal:     req.Principal,
		Grants:        req.Grants,
		AgentID:       req.AgentID,
		RunID:         req.Parent.RunID,
		InvocationID:  req.InvocationID,
		Depth:         1,
		StepInput:     req.StepInput,
		Query:         req.Query,
		Collections:   req.Collections,
		ArtifactIDs:   selectedArtifactIDs(req.Parent, req.ArtifactIDs),
		AllowedTools:  req.AllowedTools,
		ForwardedFrom: req.Parent.InvocationID,
	})
}

func selectedArtifactIDs(parent Bundle, requested []string) []string {
	have := map[string]struct{}{}
	for _, fragment := range parent.Fragments {
		if fragment.Scope != ScopeRun || fragment.Provenance.SourceID == "" {
			continue
		}
		have[fragment.Provenance.SourceID] = struct{}{}
	}
	var out []string
	seen := map[string]struct{}{}
	for _, id := range requested {
		if _, ok := have[id]; !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// PutGrant installs a runtime grant. The grant tenant is the admin's tenant.
func (r *Resolver) PutGrant(ctx context.Context, principal Principal, grant Grant) (Grant, error) {
	if err := ctx.Err(); err != nil {
		return Grant{}, err
	}
	if err := requireRole(principal, RoleAdmin); err != nil {
		return Grant{}, err
	}
	if grant.TenantID != "" && grant.TenantID != principal.TenantID {
		return Grant{}, codeError(CodeDenied, grant.Scope, "", "")
	}
	grant.TenantID = principal.TenantID
	if grant.PrincipalID == "" || grant.AgentID == "" || grant.ResourceID == "" {
		return Grant{}, invalid("grant identity and resource are required")
	}
	if !grant.Scope.Valid() {
		return Grant{}, invalid("grant scope is invalid")
	}
	if len(grant.Purposes) == 0 {
		return Grant{}, invalid("grant purpose is required")
	}
	for _, purpose := range grant.Purposes {
		if !purpose.Valid() {
			return Grant{}, invalid("grant purpose is invalid")
		}
	}
	if grant.ID == "" {
		grant.ID = newID("grant")
	}
	grant.ExpiresAt = cloneTime(grant.ExpiresAt)
	grant.RevokedAt = nil
	grant.RevokedBy = ""
	if err := r.grants.insert(grant); err != nil {
		return Grant{}, err
	}
	return cloneGrant(grant), nil
}

// RevokeGrant blocks new reads and writes that required this grant.
// Historical snapshots are left in place. Revoking an already revoked grant
// keeps the original timestamp.
func (r *Resolver) RevokeGrant(ctx context.Context, principal Principal, grantID string) (Grant, error) {
	if err := ctx.Err(); err != nil {
		return Grant{}, err
	}
	if err := requireRole(principal, RoleAdmin); err != nil {
		return Grant{}, err
	}
	if grantID == "" {
		return Grant{}, invalid("grant id is required")
	}
	grant, ok := r.grants.get(grantID)
	if !ok || grant.TenantID != principal.TenantID {
		return Grant{}, notFound("grant not found")
	}
	if grant.RevokedAt == nil {
		stamp := r.now()
		grant.RevokedAt = &stamp
		grant.RevokedBy = principal.ID
		r.grants.put(grant)
	}
	return cloneGrant(grant), nil
}

// GetGrant returns one grant in the caller's tenant.
func (r *Resolver) GetGrant(ctx context.Context, principal Principal, grantID string) (Grant, error) {
	if err := ctx.Err(); err != nil {
		return Grant{}, err
	}
	if !principal.HasRole(RoleAdmin) && !principal.HasRole(RoleInspector) {
		return Grant{}, codeError(CodeDenied, "", "", "")
	}
	grant, ok := r.grants.get(grantID)
	if !ok || grant.TenantID != principal.TenantID {
		return Grant{}, notFound("grant not found")
	}
	return grant, nil
}

// ListGrants returns the caller's tenant grants in ID order.
func (r *Resolver) ListGrants(ctx context.Context, principal Principal) ([]Grant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !principal.HasRole(RoleAdmin) && !principal.HasRole(RoleInspector) {
		return nil, codeError(CodeDenied, "", "", "")
	}
	if principal.TenantID == "" {
		return nil, invalid("principal is required")
	}
	return r.grants.list(principal.TenantID), nil
}

// PutArtifact stores a run artifact after a write grant check.
func (r *Resolver) PutArtifact(ctx context.Context, req ArtifactWrite) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	if err := validateActor(req.Principal, req.Grants, req.AgentID, req.RunID); err != nil {
		return Artifact{}, err
	}
	if len(req.Body) == 0 || req.Name == "" {
		return Artifact{}, invalid("artifact name and body are required")
	}
	now := r.now()
	if _, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeRun, req.RunID, PurposeWriteArtifact, now); code != "" {
		return Artifact{}, codeError(code, ScopeRun, req.RunID, PurposeWriteArtifact)
	}
	id := req.ID
	if id == "" {
		id = newID("art")
	}
	rec := artifactRec{Artifact: Artifact{
		ID:             id,
		TenantID:       req.Principal.TenantID,
		RunID:          req.RunID,
		InvocationID:   req.InvocationID,
		AgentID:        req.AgentID,
		Name:           req.Name,
		MediaType:      req.MediaType,
		Checksum:       hashBytes(req.Body),
		ByteSize:       len(req.Body),
		Classification: req.Classification,
		CreatedAt:      now,
	}}
	if req.TTL > 0 {
		expiry := now.Add(req.TTL)
		rec.RetainUntil = &expiry
	}
	if len(req.Body) > MaxInlineBytes {
		rec.ObjectKey = "art/" + id
		if err := r.objects.Put(ctx, rec.TenantID, rec.ObjectKey, req.Body); err != nil {
			return Artifact{}, &Error{Code: CodeInternal, Message: codeMessage(CodeInternal), Scope: ScopeRun, Resource: req.RunID}
		}
	} else {
		rec.inline = bytes.Clone(req.Body)
	}
	if _, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeRun, req.RunID, PurposeWriteArtifact, r.now()); code != "" {
		if rec.ObjectKey != "" {
			_ = r.objects.Delete(ctx, rec.TenantID, rec.ObjectKey)
		}
		return Artifact{}, codeError(code, ScopeRun, req.RunID, PurposeWriteArtifact)
	}
	if err := r.artifacts.insert(rec); err != nil {
		if rec.ObjectKey != "" {
			_ = r.objects.Delete(ctx, rec.TenantID, rec.ObjectKey)
		}
		return Artifact{}, err
	}
	return rec.Artifact, nil
}

// DeleteArtifact hides an artifact from new reads and deletes its object.
// Snapshots already taken keep their copied text until retention redacts them.
func (r *Resolver) DeleteArtifact(ctx context.Context, req ArtifactDelete) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateActor(req.Principal, req.Grants, req.AgentID, req.RunID); err != nil {
		return err
	}
	if req.ID == "" {
		return invalid("artifact id is required")
	}
	now := r.now()
	if _, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeRun, req.RunID, PurposeDeleteArtifact, now); code != "" {
		return codeError(code, ScopeRun, req.RunID, PurposeDeleteArtifact)
	}
	rec, ok := r.artifacts.markDeleted(req.Principal.TenantID, req.RunID, req.ID, now)
	if !ok {
		return notFound("artifact not found")
	}
	if rec.ObjectKey != "" {
		_ = r.objects.Delete(ctx, req.Principal.TenantID, rec.ObjectKey)
	}
	return nil
}

// WriteMemory stores agent memory after a write grant check.
func (r *Resolver) WriteMemory(ctx context.Context, req MemoryWrite) (Memory, error) {
	if err := ctx.Err(); err != nil {
		return Memory{}, err
	}
	if err := validateActor(req.Principal, req.Grants, req.AgentID, "memory"); err != nil {
		return Memory{}, err
	}
	if req.Text == "" {
		return Memory{}, invalid("memory text is required")
	}
	now := r.now()
	if _, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeAgentMemory, req.AgentID, PurposeWriteMemory, now); code != "" {
		return Memory{}, codeError(code, ScopeAgentMemory, req.AgentID, PurposeWriteMemory)
	}
	body := []byte(req.Text)
	id := req.ID
	if id == "" {
		id = newID("mem")
	}
	rec := memoryRec{Memory: Memory{
		ID:        id,
		TenantID:  req.Principal.TenantID,
		AgentID:   req.AgentID,
		Topic:     req.Topic,
		Checksum:  hashBytes(body),
		ByteSize:  len(body),
		CreatedAt: now,
	}}
	if req.TTL > 0 {
		expiry := now.Add(req.TTL)
		rec.ExpiresAt = &expiry
	}
	if len(body) > MaxInlineBytes {
		rec.ObjectKey = "mem/" + id
		if err := r.objects.Put(ctx, rec.TenantID, rec.ObjectKey, body); err != nil {
			return Memory{}, &Error{Code: CodeInternal, Message: codeMessage(CodeInternal), Scope: ScopeAgentMemory, Resource: req.AgentID}
		}
	} else {
		rec.inline = bytes.Clone(body)
	}
	if _, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeAgentMemory, req.AgentID, PurposeWriteMemory, r.now()); code != "" {
		if rec.ObjectKey != "" {
			_ = r.objects.Delete(ctx, rec.TenantID, rec.ObjectKey)
		}
		return Memory{}, codeError(code, ScopeAgentMemory, req.AgentID, PurposeWriteMemory)
	}
	if err := r.memories.insert(rec); err != nil {
		if rec.ObjectKey != "" {
			_ = r.objects.Delete(ctx, rec.TenantID, rec.ObjectKey)
		}
		return Memory{}, err
	}
	return rec.Memory, nil
}

// DeleteMemory hides memory from new reads.
func (r *Resolver) DeleteMemory(ctx context.Context, req MemoryDelete) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateActor(req.Principal, req.Grants, req.AgentID, "memory"); err != nil {
		return err
	}
	if req.ID == "" {
		return invalid("memory id is required")
	}
	now := r.now()
	if _, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeAgentMemory, req.AgentID, PurposeDeleteMemory, now); code != "" {
		return codeError(code, ScopeAgentMemory, req.AgentID, PurposeDeleteMemory)
	}
	rec, ok := r.memories.markDeleted(req.Principal.TenantID, req.AgentID, req.ID, now)
	if !ok {
		return notFound("memory not found")
	}
	if rec.ObjectKey != "" {
		_ = r.objects.Delete(ctx, req.Principal.TenantID, rec.ObjectKey)
	}
	return nil
}

// Inspect returns one snapshot for an authorized inspector.
// A revoked data grant does not hide a snapshot that is still inside retention.
// After redaction the text, title, and source URI are empty and provenance IDs remain.
func (r *Resolver) Inspect(ctx context.Context, principal Principal, snapshotID string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if principal.TenantID == "" || principal.ID == "" || snapshotID == "" {
		return Snapshot{}, invalid("principal and snapshot id are required")
	}
	snap, ok := r.snaps.get(snapshotID)
	if !ok || snap.TenantID != principal.TenantID {
		return Snapshot{}, notFound("snapshot not found")
	}
	if err := r.authorizeInspect(principal, snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// ListSnapshots returns the caller's snapshots for one run, oldest first.
func (r *Resolver) ListSnapshots(ctx context.Context, principal Principal, runID string) ([]Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if principal.TenantID == "" || principal.ID == "" || runID == "" {
		return nil, invalid("principal and run id are required")
	}
	if canInspectRole(principal) {
		return r.snaps.listRun(principal.TenantID, runID), nil
	}
	return r.listSnapshotsByGrant(principal, runID)
}

// PutSnapshot stores a snapshot recorded elsewhere, including a version 0
// snapshot that has no memory references.
func (r *Resolver) PutSnapshot(ctx context.Context, principal Principal, snap Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !principal.HasRole(RoleAdmin) && !principal.HasRole(RoleInspector) {
		return codeError(CodeDenied, "", "", PurposeInspect)
	}
	if principal.TenantID == "" || principal.ID == "" {
		return invalid("principal is required")
	}
	if snap.Version != 0 && snap.Version != BundleSchemaVersion {
		return invalid("unsupported snapshot version")
	}
	if snap.TenantID == "" {
		snap.TenantID = principal.TenantID
	}
	if snap.TenantID != principal.TenantID {
		return codeError(CodeDenied, "", "", PurposeInspect)
	}
	if snap.RunID == "" {
		return invalid("snapshot run id is required")
	}
	if snap.ID == "" {
		snap.ID = newID("snap")
	}
	now := r.now()
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = now
	}
	if snap.RetainUntil.IsZero() {
		snap.RetainUntil = snap.CreatedAt.Add(r.retention)
	}
	if snap.Bundle.TenantID == "" {
		snap.Bundle.TenantID = snap.TenantID
	}
	if snap.Bundle.TenantID != snap.TenantID {
		return codeError(CodeDenied, "", "", PurposeInspect)
	}
	r.snaps.put(snap)
	return nil
}

// DeleteExpired redacts snapshot text whose retention has elapsed and deletes
// expired memory and artifacts in the operator's tenant.
func (r *Resolver) DeleteExpired(ctx context.Context, principal Principal) (RetentionResult, error) {
	if err := ctx.Err(); err != nil {
		return RetentionResult{}, err
	}
	if !principal.HasRole(RoleAdmin) && !principal.HasRole(RoleOperator) {
		return RetentionResult{}, codeError(CodeDenied, "", "", "")
	}
	if principal.TenantID == "" {
		return RetentionResult{}, invalid("principal is required")
	}
	now := r.now()
	var result RetentionResult
	result.SnapshotsRedacted = r.snaps.redactExpired(principal.TenantID, now)
	for _, rec := range r.memories.listExpired(principal.TenantID, now) {
		deleted, ok := r.memories.markDeleted(rec.TenantID, rec.AgentID, rec.ID, now)
		if !ok {
			continue
		}
		if deleted.ObjectKey != "" {
			_ = r.objects.Delete(ctx, deleted.TenantID, deleted.ObjectKey)
		}
		result.MemoriesDeleted++
	}
	for _, rec := range r.artifacts.listExpired(principal.TenantID, now) {
		deleted, ok := r.artifacts.markDeleted(rec.TenantID, rec.RunID, rec.ID, now)
		if !ok {
			continue
		}
		if deleted.ObjectKey != "" {
			_ = r.objects.Delete(ctx, deleted.TenantID, deleted.ObjectKey)
		}
		result.ArtifactsDeleted++
	}
	return result, nil
}

func (r *Resolver) attachStep(bundle *Bundle, req ResolveRequest, now time.Time) {
	grant, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeStep, req.InvocationID, PurposeResolve, now)
	if code != "" {
		deny(bundle, code, ScopeStep, req.InvocationID, PurposeResolve)
		return
	}
	if _, code = r.authorize(req.Principal, req.Grants, req.AgentID, ScopeStep, req.InvocationID, PurposeResolve, r.now()); code != "" {
		deny(bundle, code, ScopeStep, req.InvocationID, PurposeResolve)
		return
	}
	bundle.Fragments = append(bundle.Fragments, r.fragment(req, ScopeStep, req.InvocationID, req.StepInput, SourceStep, req.InvocationID, grant.ID, "step", now, Provenance{}))
	noteGrant(bundle, grant, PurposeResolve)
}

func (r *Resolver) attachRun(ctx context.Context, bundle *Bundle, req ResolveRequest, now time.Time) {
	requested := req.Depth == 0 || len(req.ArtifactIDs) > 0
	if !requested {
		return
	}
	grant, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeRun, req.RunID, PurposeResolve, now)
	if code != "" {
		deny(bundle, code, ScopeRun, req.RunID, PurposeResolve)
		return
	}
	type loaded struct {
		rec  artifactRec
		text string
	}
	var got []loaded
	ids := req.ArtifactIDs
	if req.Depth == 0 {
		listed := r.artifacts.list(req.Principal.TenantID, req.RunID)
		ids = make([]string, 0, len(listed))
		for _, rec := range listed {
			ids = append(ids, rec.ID)
		}
	}
	for _, id := range uniqueStrings(ids) {
		rec, text, loadCode := r.loadArtifact(ctx, req.Principal.TenantID, req.RunID, id)
		if loadCode != "" {
			deny(bundle, loadCode, ScopeRun, id, PurposeResolve)
			continue
		}
		got = append(got, loaded{rec: rec, text: text})
	}
	grant, code = r.authorize(req.Principal, req.Grants, req.AgentID, ScopeRun, req.RunID, PurposeResolve, r.now())
	if code != "" {
		deny(bundle, code, ScopeRun, req.RunID, PurposeResolve)
		return
	}
	published := false
	for _, item := range got {
		if !r.artifacts.alive(req.Principal.TenantID, req.RunID, item.rec.ID) {
			deny(bundle, CodeNotFound, ScopeRun, item.rec.ID, PurposeResolve)
			continue
		}
		extra := Provenance{
			ObjectKey:     item.rec.ObjectKey,
			Checksum:      item.rec.Checksum,
			ByteSize:      item.rec.ByteSize,
			MediaType:     item.rec.MediaType,
			DocumentTitle: item.rec.Name,
		}
		if req.ForwardedFrom != "" {
			extra.ForwardedFrom = req.ForwardedFrom
			extra.Selected = true
		}
		bundle.Fragments = append(bundle.Fragments, r.fragment(req, ScopeRun, req.RunID, item.text, SourceArtifact, item.rec.ID, grant.ID, "artifact_store", now, extra))
		published = true
	}
	if published || req.Depth == 0 {
		noteGrant(bundle, grant, PurposeResolve)
	}
}

func (r *Resolver) attachMemory(ctx context.Context, bundle *Bundle, req ResolveRequest, now time.Time) {
	if !definitionAllows(req.Grants.Grants, ScopeAgentMemory, req.AgentID, PurposeReadMemory) {
		return
	}
	grant, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeAgentMemory, req.AgentID, PurposeReadMemory, now)
	if code != "" {
		deny(bundle, code, ScopeAgentMemory, req.AgentID, PurposeReadMemory)
		return
	}
	type loaded struct {
		rec  memoryRec
		text string
	}
	var got []loaded
	for _, rec := range r.memories.list(req.Principal.TenantID, req.AgentID, now) {
		text, loadCode := r.loadMemory(ctx, rec)
		if loadCode != "" {
			deny(bundle, loadCode, ScopeAgentMemory, rec.ID, PurposeReadMemory)
			continue
		}
		got = append(got, loaded{rec: rec, text: text})
	}
	grant, code = r.authorize(req.Principal, req.Grants, req.AgentID, ScopeAgentMemory, req.AgentID, PurposeReadMemory, r.now())
	if code != "" {
		deny(bundle, code, ScopeAgentMemory, req.AgentID, PurposeReadMemory)
		return
	}
	for _, item := range got {
		fresh, ok := r.memories.get(req.Principal.TenantID, req.AgentID, item.rec.ID)
		if !ok || (fresh.ExpiresAt != nil && !fresh.ExpiresAt.After(r.now())) {
			continue
		}
		extra := Provenance{ObjectKey: fresh.ObjectKey, Checksum: fresh.Checksum, ByteSize: fresh.ByteSize}
		bundle.Fragments = append(bundle.Fragments, r.fragment(req, ScopeAgentMemory, req.AgentID, item.text, SourceMemory, fresh.ID, grant.ID, "memory_store", now, extra))
	}
	noteGrant(bundle, grant, PurposeReadMemory)
}

func (r *Resolver) attachOrganization(ctx context.Context, bundle *Bundle, req ResolveRequest, now time.Time) error {
	collections := uniqueStrings(req.Collections)
	for _, collection := range collections {
		if err := ctx.Err(); err != nil {
			return err
		}
		grant, code := r.authorize(req.Principal, req.Grants, req.AgentID, ScopeOrganization, collection, PurposeRetrieve, now)
		if code != "" {
			deny(bundle, code, ScopeOrganization, collection, PurposeRetrieve)
			continue
		}
		// The grant lock is not held across Retrieve. Hits are discarded
		// unless the same grant still authorizes the collection afterward.
		hits, err := r.retriever.Retrieve(ctx, RetrievalQuery{
			TenantID:     req.Principal.TenantID,
			CollectionID: collection,
			Query:        req.Query,
			Limit:        DefaultRetrievalLimit,
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			deny(bundle, CodeInternal, ScopeOrganization, collection, PurposeRetrieve)
			continue
		}
		grant, code = r.authorize(req.Principal, req.Grants, req.AgentID, ScopeOrganization, collection, PurposeRetrieve, r.now())
		if code != "" {
			deny(bundle, code, ScopeOrganization, collection, PurposeRetrieve)
			continue
		}
		accepted := make([]Hit, 0, len(hits))
		for _, hit := range hits {
			if hit.TenantID != req.Principal.TenantID || hit.CollectionID != collection {
				continue
			}
			accepted = append(accepted, hit)
		}
		sort.SliceStable(accepted, func(i, j int) bool {
			if accepted[i].Ordinal == accepted[j].Ordinal {
				return accepted[i].DocumentID < accepted[j].DocumentID
			}
			return accepted[i].Ordinal < accepted[j].Ordinal
		})
		name := "retriever"
		if r.retriever != nil {
			name = r.retriever.Name()
		}
		for _, hit := range accepted {
			sourceID := hit.DocumentID
			if sourceID == "" {
				sourceID = collection
			}
			extra := Provenance{DocumentTitle: hit.Title, SourceURI: hit.SourceURI}
			bundle.Fragments = append(bundle.Fragments, r.fragment(req, ScopeOrganization, collection, hit.Text, SourceKnowledge, sourceID, grant.ID, name, now, extra))
		}
		noteGrant(bundle, grant, PurposeRetrieve)
	}
	return nil
}

func (r *Resolver) loadArtifact(ctx context.Context, tenantID, runID, id string) (artifactRec, string, Code) {
	rec, ok := r.artifacts.get(tenantID, runID, id)
	if !ok {
		return artifactRec{}, "", CodeNotFound
	}
	body, code := r.loadBody(ctx, tenantID, rec.ObjectKey, rec.inline, rec.Checksum)
	if code != "" {
		return artifactRec{}, "", code
	}
	if !utf8.Valid(body) {
		return rec, "", ""
	}
	return rec, string(body), ""
}

func (r *Resolver) loadMemory(ctx context.Context, rec memoryRec) (string, Code) {
	body, code := r.loadBody(ctx, rec.TenantID, rec.ObjectKey, rec.inline, rec.Checksum)
	if code != "" {
		return "", code
	}
	if !utf8.Valid(body) {
		return "", ""
	}
	return string(body), ""
}

func (r *Resolver) loadBody(ctx context.Context, tenantID, objectKey string, inline []byte, checksum string) ([]byte, Code) {
	var body []byte
	if objectKey != "" {
		loaded, err := r.objects.Get(ctx, tenantID, objectKey)
		if err != nil {
			return nil, CodeNotFound
		}
		body = loaded
	} else {
		body = inline
	}
	if checksum != "" && hashBytes(body) != checksum {
		return nil, CodeInternal
	}
	return body, ""
}

func (r *Resolver) fragment(req ResolveRequest, scope Scope, resource, text, sourceKind, sourceID, grantID, retriever string, now time.Time, extra Provenance) Fragment {
	extra.Scope = scope
	extra.ResourceID = resource
	extra.SourceKind = sourceKind
	extra.SourceID = sourceID
	extra.TenantID = req.Principal.TenantID
	extra.GrantID = grantID
	extra.Retriever = retriever
	extra.ContentHash = ContentHash(text)
	extra.CollectedAt = now
	return Fragment{
		ID:         newID("frag"),
		Scope:      scope,
		ResourceID: resource,
		Text:       text,
		Trust:      TrustUntrusted,
		Provenance: extra,
	}
}

func (r *Resolver) record(bundle *Bundle, now time.Time) {
	bundle.SnapshotID = newID("snap")
	refs := memoryRefs(*bundle)
	snap := Snapshot{
		Version:      bundle.Version,
		ID:           bundle.SnapshotID,
		TenantID:     bundle.TenantID,
		RunID:        bundle.RunID,
		InvocationID: bundle.InvocationID,
		AgentID:      bundle.AgentID,
		Bundle:       cloneBundle(*bundle),
		MemoryRefs:   refs,
		RetainUntil:  now.Add(r.retention),
		CreatedAt:    now,
	}
	r.snaps.put(snap)
}

func (r *Resolver) authorize(principal Principal, grants VersionedGrants, agentID string, scope Scope, resource string, purpose Purpose, now time.Time) (Grant, Code) {
	if !definitionAllows(grants.Grants, scope, resource, purpose) {
		return Grant{}, CodeDenied
	}
	return r.grants.decide(principal.TenantID, principal.ID, agentID, scope, resource, purpose, now)
}

func (r *Resolver) authorizeInspect(principal Principal, snap Snapshot) error {
	if canInspectRole(principal) {
		return nil
	}
	_, code := r.grants.decide(principal.TenantID, principal.ID, snap.AgentID, ScopeRun, snap.RunID, PurposeInspect, r.now())
	if code != "" {
		// A grant for another purpose is not inspection authority.
		if code == CodePurposeMismatch {
			code = CodeDenied
		}
		return codeError(code, ScopeRun, snap.RunID, PurposeInspect)
	}
	return nil
}

func (r *Resolver) listSnapshotsByGrant(principal Principal, runID string) ([]Snapshot, error) {
	all := r.snaps.listRun(principal.TenantID, runID)
	var out []Snapshot
	now := r.now()
	for _, snap := range all {
		grant, code := r.grants.decide(principal.TenantID, principal.ID, snap.AgentID, ScopeRun, runID, PurposeInspect, now)
		if code != "" || grant.ID == "" {
			continue
		}
		out = append(out, snap)
	}
	if len(out) == 0 && len(all) > 0 {
		return nil, codeError(CodeDenied, ScopeRun, runID, PurposeInspect)
	}
	return out, nil
}

func canInspectRole(principal Principal) bool {
	return principal.HasRole(RoleInspector) || principal.HasRole(RoleAdmin) || principal.HasRole(RoleOperator)
}

func requireRole(principal Principal, role string) error {
	if principal.TenantID == "" || principal.ID == "" {
		return invalid("principal is required")
	}
	if !principal.HasRole(role) {
		return codeError(CodeDenied, "", "", "")
	}
	return nil
}

func validateActor(principal Principal, grants VersionedGrants, agentID, runID string) error {
	if principal.TenantID == "" || principal.ID == "" {
		return invalid("principal is required")
	}
	if agentID == "" || runID == "" {
		return invalid("agent and run are required")
	}
	return validateGrants(grants)
}

func validateResolve(req ResolveRequest) error {
	if err := validateActor(req.Principal, req.Grants, req.AgentID, req.RunID); err != nil {
		return err
	}
	if req.InvocationID == "" {
		return invalid("invocation is required")
	}
	if req.Depth != 0 && req.Depth != 1 {
		return invalid("depth must be 0 or 1")
	}
	for _, collection := range req.Collections {
		if collection == "" {
			return invalid("collection id is required")
		}
	}
	return nil
}

func deny(bundle *Bundle, code Code, scope Scope, resource string, purpose Purpose) {
	bundle.Denials = append(bundle.Denials, Denial{
		Code:     code,
		Scope:    scope,
		Resource: resource,
		Purpose:  purpose,
	})
}

func noteGrant(bundle *Bundle, grant Grant, purpose Purpose) {
	for _, use := range bundle.GrantsUsed {
		if use.GrantID == grant.ID && use.Purpose == purpose {
			return
		}
	}
	bundle.GrantsUsed = append(bundle.GrantsUsed, GrantUse{
		GrantID:    grant.ID,
		Scope:      grant.Scope,
		ResourceID: grant.ResourceID,
		Purpose:    purpose,
	})
}

func memoryRefs(bundle Bundle) []string {
	var refs []string
	for _, fragment := range bundle.Fragments {
		if fragment.Scope == ScopeAgentMemory && fragment.Provenance.SourceID != "" {
			refs = append(refs, fragment.Provenance.SourceID)
		}
	}
	return refs
}

func newID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return prefix + "_" + hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return prefix + "_" + hex.EncodeToString(buf[:])
}
