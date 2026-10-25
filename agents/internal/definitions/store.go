package definitions

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"sort"
	"sync"
	"time"
)

var errNotFound = errors.New("not found")

type store interface {
	Apply(ctx context.Context, fn func(txStore) error) error
	Close()
}

type txStore interface {
	GetVersion(ctx context.Context, id, version string) (Version, error)
	ListVersions(ctx context.Context) ([]Version, error)
	NameAvailable(ctx context.Context, name, selfID string) error
	EnsureAgent(ctx context.Context, v Version) error
	PutVersion(ctx context.Context, v Version) error
	RetireVersion(ctx context.Context, id, version, actor string, at time.Time) (Version, error)
	RefreshAgent(ctx context.Context, id, name string, at time.Time) error
	GetPin(ctx context.Context, runID, agentID string) (Pin, error)
	InsertPin(ctx context.Context, pin Pin) error
	ListPins(ctx context.Context, runID string) ([]Pin, error)
	InsertTool(ctx context.Context, tool Tool, hash string, at time.Time) error
	ListTools(ctx context.Context) ([]Tool, error)
}

type agentHead struct {
	ID        string
	Name      string
	Lifecycle string
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type storedTool struct {
	Tool Tool
	Hash string
}

type memorySnap struct {
	agents   map[string]agentHead
	versions map[string]Version
	names    map[string]string
	pins     map[string]Pin
	tools    map[string]storedTool
	toolIDs  map[string]time.Time
}

type memoryStore struct {
	mu       sync.Mutex
	agents   map[string]agentHead
	versions map[string]Version
	names    map[string]string
	pins     map[string]Pin
	tools    map[string]storedTool
	toolIDs  map[string]time.Time
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		agents:   map[string]agentHead{},
		versions: map[string]Version{},
		names:    map[string]string{},
		pins:     map[string]Pin{},
		tools:    map[string]storedTool{},
		toolIDs:  map[string]time.Time{},
	}
}

func (m *memoryStore) Close() {}

func (m *memoryStore) Apply(ctx context.Context, fn func(txStore) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := m.snapshot()
	if err := fn(m); err != nil {
		m.restore(snap)
		return err
	}
	return nil
}

func (m *memoryStore) snapshot() memorySnap {
	return memorySnap{
		agents:   maps.Clone(m.agents),
		versions: maps.Clone(m.versions),
		names:    maps.Clone(m.names),
		pins:     maps.Clone(m.pins),
		tools:    maps.Clone(m.tools),
		toolIDs:  maps.Clone(m.toolIDs),
	}
}

func (m *memoryStore) restore(snap memorySnap) {
	m.agents = snap.agents
	m.versions = snap.versions
	m.names = snap.names
	m.pins = snap.pins
	m.tools = snap.tools
	m.toolIDs = snap.toolIDs
}

func (m *memoryStore) GetVersion(_ context.Context, id, version string) (Version, error) {
	v, ok := m.versions[refKey(id, version)]
	if !ok {
		return Version{}, errNotFound
	}
	return cloneVersion(v), nil
}

func (m *memoryStore) ListVersions(context.Context) ([]Version, error) {
	out := make([]Version, 0, len(m.versions))
	for _, v := range m.versions {
		out = append(out, cloneVersion(v))
	}
	return out, nil
}

func (m *memoryStore) NameAvailable(_ context.Context, name, selfID string) error {
	if owner, ok := m.names[name]; ok && owner != selfID {
		return nameConflict()
	}
	return nil
}

func (m *memoryStore) EnsureAgent(_ context.Context, v Version) error {
	if _, ok := m.agents[v.Spec.ID]; ok {
		return nil
	}
	if owner, ok := m.names[v.Spec.Name]; ok && owner != v.Spec.ID {
		return nameConflict()
	}
	m.agents[v.Spec.ID] = agentHead{
		ID: v.Spec.ID, Name: v.Spec.Name, Lifecycle: v.State,
		CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
	m.names[v.Spec.Name] = v.Spec.ID
	return nil
}

func (m *memoryStore) PutVersion(_ context.Context, v Version) error {
	key := refKey(v.Spec.ID, v.Spec.Version)
	existing, ok := m.versions[key]
	if !ok {
		if _, headOK := m.agents[v.Spec.ID]; !headOK {
			return &Error{Code: CodeConflict, Message: "agent definition is missing"}
		}
		m.versions[key] = cloneVersion(v)
		return nil
	}
	if existing.State != StateDraft && existing.State != StateTesting {
		return &Error{Code: CodeImmutable, Message: "published version cannot change; save a new version"}
	}
	if existing.CreatedBy != v.CreatedBy || !existing.CreatedAt.Equal(v.CreatedAt) {
		return &Error{Code: CodeImmutable, Message: "definition version identity is immutable"}
	}
	m.versions[key] = cloneVersion(v)
	return nil
}

func (m *memoryStore) RetireVersion(_ context.Context, id, version, actor string, at time.Time) (Version, error) {
	key := refKey(id, version)
	existing, ok := m.versions[key]
	if !ok {
		return Version{}, errNotFound
	}
	if existing.State == StateRetired {
		return cloneVersion(existing), nil
	}
	if existing.State != StatePublished {
		return Version{}, &Error{Code: CodeInvalidInput, Message: "only a published version can be retired"}
	}
	existing.State = StateRetired
	existing.RetiredBy = actor
	existing.RetiredAt = timePtr(at)
	existing.UpdatedAt = at
	m.versions[key] = cloneVersion(existing)
	return cloneVersion(m.versions[key]), nil
}

func (m *memoryStore) RefreshAgent(_ context.Context, id, name string, at time.Time) error {
	head, ok := m.agents[id]
	if !ok {
		return errNotFound
	}
	if owner, taken := m.names[name]; taken && owner != id {
		return nameConflict()
	}
	if head.Name != name {
		if m.names[head.Name] == id {
			delete(m.names, head.Name)
		}
		m.names[name] = id
		head.Name = name
	}
	head.Lifecycle = summarizeLifecycle(m.versions, id)
	head.UpdatedAt = at
	m.agents[id] = head
	return nil
}

func summarizeLifecycle(versions map[string]Version, id string) string {
	published, testing, draft := false, false, false
	for _, v := range versions {
		if v.Spec.ID != id {
			continue
		}
		switch v.State {
		case StatePublished:
			published = true
		case StateTesting:
			testing = true
		case StateDraft:
			draft = true
		}
	}
	switch {
	case published:
		return StatePublished
	case testing:
		return StateTesting
	case draft:
		return StateDraft
	default:
		return StateRetired
	}
}

func (m *memoryStore) GetPin(_ context.Context, runID, agentID string) (Pin, error) {
	pin, ok := m.pins[refKey(runID, agentID)]
	if !ok {
		return Pin{}, errNotFound
	}
	return clonePin(pin), nil
}

func (m *memoryStore) InsertPin(_ context.Context, pin Pin) error {
	key := refKey(pin.RunID, pin.AgentID)
	if _, ok := m.pins[key]; ok {
		return &Error{Code: CodeConflict, Message: "run is already pinned to a different definition version"}
	}
	if _, ok := m.versions[refKey(pin.AgentID, pin.Version)]; !ok {
		return errNotFound
	}
	m.pins[key] = clonePin(pin)
	return nil
}

func (m *memoryStore) ListPins(_ context.Context, runID string) ([]Pin, error) {
	out := []Pin{}
	for _, pin := range m.pins {
		if pin.RunID == runID {
			out = append(out, clonePin(pin))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return out, nil
}

func (m *memoryStore) InsertTool(_ context.Context, tool Tool, hash string, at time.Time) error {
	key := refKey(tool.ID, tool.Version)
	if existing, ok := m.tools[key]; ok {
		if existing.Hash == hash {
			return nil
		}
		return &Error{Code: CodeImmutable, Message: "tool version cannot change; register a new version"}
	}
	if _, ok := m.toolIDs[tool.ID]; !ok {
		m.toolIDs[tool.ID] = at
	}
	copied := tool
	copied.InputSchema = cloneRaw(tool.InputSchema)
	copied.OutputSchema = cloneRaw(tool.OutputSchema)
	m.tools[key] = storedTool{Tool: copied, Hash: hash}
	return nil
}

func (m *memoryStore) ListTools(context.Context) ([]Tool, error) {
	out := make([]Tool, 0, len(m.tools))
	for _, stored := range m.tools {
		tool := stored.Tool
		tool.InputSchema = cloneRaw(stored.Tool.InputSchema)
		tool.OutputSchema = cloneRaw(stored.Tool.OutputSchema)
		out = append(out, tool)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Version < out[j].Version
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	out := make(json.RawMessage, len(raw))
	copy(out, raw)
	return out
}

func cloneSpec(spec Spec) Spec {
	spec.InputSchema = cloneRaw(spec.InputSchema)
	spec.OutputSchema = cloneRaw(spec.OutputSchema)
	if spec.AllowedTools != nil {
		spec.AllowedTools = append([]string(nil), spec.AllowedTools...)
	}
	if spec.AllowedDelegates != nil {
		spec.AllowedDelegates = append([]string(nil), spec.AllowedDelegates...)
	}
	if spec.ModelPolicy != nil {
		policy := *spec.ModelPolicy
		if policy.AllowedModels != nil {
			policy.AllowedModels = append([]string(nil), policy.AllowedModels...)
		}
		spec.ModelPolicy = &policy
	}
	return spec
}

func cloneVersion(v Version) Version {
	v.Spec = cloneSpec(v.Spec)
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	v.TestedAt = cloneTime(v.TestedAt)
	v.PublishedAt = cloneTime(v.PublishedAt)
	v.RetiredAt = cloneTime(v.RetiredAt)
	return v
}

func clonePin(pin Pin) Pin {
	pin.Spec = cloneSpec(pin.Spec)
	pin.PinnedAt = pin.PinnedAt.UTC()
	return pin
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	copied := t.UTC()
	return &copied
}

func timePtr(t time.Time) *time.Time {
	copied := t.UTC()
	return &copied
}
