package context

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrObjectNotFound is returned when an object key is absent for the tenant.
// The error text does not include the key or the object body.
var ErrObjectNotFound = errors.New("context: object not found")

// ObjectStore keeps large artifact and memory bodies.
// Implementations must isolate tenants and must not echo body bytes in errors.
type ObjectStore interface {
	Put(ctx context.Context, tenantID, key string, body []byte) error
	Get(ctx context.Context, tenantID, key string) ([]byte, error)
	Delete(ctx context.Context, tenantID, key string) error
}

// MemoryObjectStore is an in-memory object store for tests and local runs.
type MemoryObjectStore struct {
	mu     sync.Mutex
	bodies map[string][]byte
}

// NewMemoryObjectStore returns an empty object store.
func NewMemoryObjectStore() *MemoryObjectStore {
	return &MemoryObjectStore{bodies: map[string][]byte{}}
}

func objectRef(tenantID, key string) string {
	return tenantID + "\x00" + key
}

// Put stores a copy of body at tenantID/key.
func (s *MemoryObjectStore) Put(ctx context.Context, tenantID, key string, body []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" || key == "" {
		return invalid("object identity is required")
	}
	copied := make([]byte, len(body))
	copy(copied, body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies[objectRef(tenantID, key)] = copied
	return nil
}

// Get returns a copy of the object body.
func (s *MemoryObjectStore) Get(ctx context.Context, tenantID, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" || key == "" {
		return nil, ErrObjectNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.bodies[objectRef(tenantID, key)]
	if !ok {
		return nil, ErrObjectNotFound
	}
	out := make([]byte, len(body))
	copy(out, body)
	return out, nil
}

// Delete removes the object. A missing object is not an error.
func (s *MemoryObjectStore) Delete(ctx context.Context, tenantID, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" || key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bodies, objectRef(tenantID, key))
	return nil
}

type grantIndex struct {
	mu   sync.Mutex
	byID map[string]Grant
}

func newGrantIndex() *grantIndex {
	return &grantIndex{byID: map[string]Grant{}}
}

// GrantStore persists runtime grants. An active purpose match authorizes the
// call. A revoked grant for that purpose outranks an active grant for a
// different purpose. Implementations must not include stored text in errors.
type GrantStore interface {
	Insert(ctx context.Context, grant Grant) error
	Put(ctx context.Context, grant Grant) error
	Get(ctx context.Context, id string) (Grant, bool, error)
	List(ctx context.Context, tenantID string) ([]Grant, error)
	Decide(ctx context.Context, tenantID, principalID, agentID string, scope Scope, resource string, purpose Purpose, now time.Time) (Grant, Code, error)
}

// SnapshotStore persists context snapshots, including version 0 records that
// have no memory references. RedactExpired clears fragment text in that
// tenant only.
type SnapshotStore interface {
	Put(ctx context.Context, snap Snapshot) error
	Get(ctx context.Context, id string) (Snapshot, bool, error)
	ListRun(ctx context.Context, tenantID, runID string) ([]Snapshot, error)
	RedactExpired(ctx context.Context, tenantID string, now time.Time) (int, error)
}

func (s *grantIndex) Insert(ctx context.Context, grant Grant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.insert(grant)
}

func (s *grantIndex) Put(ctx context.Context, grant Grant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.put(grant)
	return nil
}

func (s *grantIndex) Get(ctx context.Context, id string) (Grant, bool, error) {
	if err := ctx.Err(); err != nil {
		return Grant{}, false, err
	}
	grant, ok := s.get(id)
	return grant, ok, nil
}

func (s *grantIndex) List(ctx context.Context, tenantID string) ([]Grant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.list(tenantID), nil
}

func (s *grantIndex) Decide(ctx context.Context, tenantID, principalID, agentID string, scope Scope, resource string, purpose Purpose, now time.Time) (Grant, Code, error) {
	if err := ctx.Err(); err != nil {
		return Grant{}, "", err
	}
	grant, code := s.decide(tenantID, principalID, agentID, scope, resource, purpose, now)
	return grant, code, nil
}

func (s *grantIndex) insert(grant Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[grant.ID]; ok {
		return invalid("grant already exists")
	}
	s.byID[grant.ID] = cloneGrant(grant)
	return nil
}

func (s *grantIndex) put(grant Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[grant.ID] = cloneGrant(grant)
}

func (s *grantIndex) get(id string) (Grant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.byID[id]
	if !ok {
		return Grant{}, false
	}
	return cloneGrant(grant), true
}

func (s *grantIndex) list(tenantID string) []Grant {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Grant, 0, len(s.byID))
	for _, grant := range s.byID {
		if grant.TenantID == tenantID {
			out = append(out, cloneGrant(grant))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// decide returns an active matching grant or a failure code.
// Revocation of a same-purpose grant wins over an active grant for a
// different purpose. No stored text is examined.
func (s *grantIndex) decide(tenantID, principalID, agentID string, scope Scope, resource string, purpose Purpose, now time.Time) (Grant, Code) {
	s.mu.Lock()
	defer s.mu.Unlock()
	matched := make([]Grant, 0)
	for _, grant := range s.byID {
		if grant.TenantID != tenantID || grant.PrincipalID != principalID || grant.AgentID != agentID {
			continue
		}
		if grant.Scope != scope || grant.ResourceID != resource {
			continue
		}
		matched = append(matched, grant)
	}
	return decideGrants(matched, purpose, now)
}

func decideGrants(grants []Grant, purpose Purpose, now time.Time) (Grant, Code) {
	revoked := false
	expired := false
	otherPurpose := false
	for _, grant := range grants {
		matchesPurpose := false
		for _, candidate := range grant.Purposes {
			if candidate == purpose {
				matchesPurpose = true
				break
			}
		}
		if !matchesPurpose {
			if grant.Active(now) {
				otherPurpose = true
			}
			continue
		}
		if grant.Active(now) {
			return cloneGrant(grant), ""
		}
		if grant.RevokedAt != nil {
			revoked = true
			continue
		}
		expired = true
	}
	switch {
	case revoked:
		return Grant{}, CodeGrantRevoked
	case expired:
		return Grant{}, CodeGrantExpired
	case otherPurpose:
		return Grant{}, CodePurposeMismatch
	default:
		return Grant{}, CodeDenied
	}
}

type artifactRec struct {
	Artifact
	inline    []byte
	deletedAt *time.Time
}

type artifactIndex struct {
	mu   sync.Mutex
	byID map[string]artifactRec
}

func newArtifactIndex() *artifactIndex {
	return &artifactIndex{byID: map[string]artifactRec{}}
}

func (s *artifactIndex) insert(rec artifactRec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[rec.ID]; ok {
		return invalid("artifact already exists")
	}
	s.byID[rec.ID] = copyArtifactRec(rec)
	return nil
}

func (s *artifactIndex) get(tenantID, runID, id string) (artifactRec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok || rec.TenantID != tenantID || rec.RunID != runID || rec.deletedAt != nil {
		return artifactRec{}, false
	}
	return copyArtifactRec(rec), true
}

func (s *artifactIndex) alive(tenantID, runID, id string) bool {
	_, ok := s.get(tenantID, runID, id)
	return ok
}

func (s *artifactIndex) list(tenantID, runID string) []artifactRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []artifactRec
	for _, rec := range s.byID {
		if rec.TenantID == tenantID && rec.RunID == runID && rec.deletedAt == nil {
			out = append(out, copyArtifactRec(rec))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *artifactIndex) listExpired(tenantID string, now time.Time) []artifactRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []artifactRec
	for _, rec := range s.byID {
		if rec.TenantID != tenantID || rec.deletedAt != nil || rec.RetainUntil == nil {
			continue
		}
		if rec.RetainUntil.After(now) {
			continue
		}
		out = append(out, copyArtifactRec(rec))
	}
	return out
}

func (s *artifactIndex) markDeleted(tenantID, runID, id string, now time.Time) (artifactRec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok || rec.TenantID != tenantID || rec.RunID != runID {
		return artifactRec{}, false
	}
	if rec.deletedAt != nil {
		return copyArtifactRec(rec), true
	}
	stamp := now.UTC()
	rec.deletedAt = &stamp
	rec.Name = ""
	rec.Classification = ""
	rec.inline = nil
	s.byID[id] = rec
	return copyArtifactRec(rec), true
}

func copyArtifactRec(rec artifactRec) artifactRec {
	rec.RetainUntil = cloneTime(rec.RetainUntil)
	rec.deletedAt = cloneTime(rec.deletedAt)
	if rec.inline != nil {
		rec.inline = bytes.Clone(rec.inline)
	}
	return rec
}

type memoryRec struct {
	Memory
	inline []byte
}

type memoryIndex struct {
	mu   sync.Mutex
	byID map[string]memoryRec
}

func newMemoryIndex() *memoryIndex {
	return &memoryIndex{byID: map[string]memoryRec{}}
}

func (s *memoryIndex) insert(rec memoryRec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[rec.ID]; ok {
		return invalid("memory already exists")
	}
	s.byID[rec.ID] = copyMemoryRec(rec)
	return nil
}

func (s *memoryIndex) get(tenantID, agentID, id string) (memoryRec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok || rec.TenantID != tenantID || rec.AgentID != agentID || rec.DeletedAt != nil {
		return memoryRec{}, false
	}
	return copyMemoryRec(rec), true
}

func (s *memoryIndex) list(tenantID, agentID string, now time.Time) []memoryRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []memoryRec
	for _, rec := range s.byID {
		if rec.TenantID != tenantID || rec.AgentID != agentID || rec.DeletedAt != nil {
			continue
		}
		if rec.ExpiresAt != nil && !rec.ExpiresAt.After(now) {
			continue
		}
		out = append(out, copyMemoryRec(rec))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *memoryIndex) listExpired(tenantID string, now time.Time) []memoryRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []memoryRec
	for _, rec := range s.byID {
		if rec.TenantID != tenantID || rec.DeletedAt != nil || rec.ExpiresAt == nil {
			continue
		}
		if rec.ExpiresAt.After(now) {
			continue
		}
		out = append(out, copyMemoryRec(rec))
	}
	return out
}

func (s *memoryIndex) markDeleted(tenantID, agentID, id string, now time.Time) (memoryRec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok || rec.TenantID != tenantID || rec.AgentID != agentID {
		return memoryRec{}, false
	}
	if rec.DeletedAt != nil {
		return copyMemoryRec(rec), true
	}
	stamp := now.UTC()
	rec.DeletedAt = &stamp
	rec.Topic = ""
	rec.inline = nil
	s.byID[id] = rec
	return copyMemoryRec(rec), true
}

func copyMemoryRec(rec memoryRec) memoryRec {
	rec.ExpiresAt = cloneTime(rec.ExpiresAt)
	rec.DeletedAt = cloneTime(rec.DeletedAt)
	if rec.inline != nil {
		rec.inline = bytes.Clone(rec.inline)
	}
	return rec
}

type snapshotIndex struct {
	mu   sync.Mutex
	byID map[string]Snapshot
}

func newSnapshotIndex() *snapshotIndex {
	return &snapshotIndex{byID: map[string]Snapshot{}}
}

func (s *snapshotIndex) Put(ctx context.Context, snap Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.put(snap)
	return nil
}

func (s *snapshotIndex) Get(ctx context.Context, id string) (Snapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	snap, ok := s.get(id)
	return snap, ok, nil
}

func (s *snapshotIndex) ListRun(ctx context.Context, tenantID, runID string) ([]Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.listRun(tenantID, runID), nil
}

func (s *snapshotIndex) RedactExpired(ctx context.Context, tenantID string, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.redactExpired(tenantID, now), nil
}

func (s *snapshotIndex) put(snap Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[snap.ID] = cloneSnapshot(snap)
}

func (s *snapshotIndex) get(id string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.byID[id]
	if !ok {
		return Snapshot{}, false
	}
	return cloneSnapshot(snap), true
}

func (s *snapshotIndex) listRun(tenantID, runID string) []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Snapshot
	for _, snap := range s.byID {
		if snap.TenantID == tenantID && snap.RunID == runID {
			out = append(out, cloneSnapshot(snap))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt) || (out[i].CreatedAt.Equal(out[j].CreatedAt) && out[i].ID < out[j].ID)
	})
	return out
}

func (s *snapshotIndex) redactExpired(tenantID string, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for id, snap := range s.byID {
		if snap.TenantID != tenantID || snap.RedactedAt != nil || snap.RetainUntil.After(now) {
			continue
		}
		redactSnapshot(&snap, now)
		s.byID[id] = snap
		count++
	}
	return count
}

func redactSnapshot(snap *Snapshot, now time.Time) {
	stamp := now.UTC()
	snap.RedactedAt = &stamp
	for i := range snap.Bundle.Fragments {
		snap.Bundle.Fragments[i].Text = ""
		snap.Bundle.Fragments[i].Redacted = true
		snap.Bundle.Fragments[i].Provenance.DocumentTitle = ""
		snap.Bundle.Fragments[i].Provenance.SourceURI = ""
	}
}
