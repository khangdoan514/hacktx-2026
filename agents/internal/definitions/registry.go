package definitions

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Registry is the definition lifecycle. Memory and Postgres implementations
// share these methods. A registry is bound to one tenant.
type Registry struct {
	store  store
	tenant string
	now    func() time.Time
}

// Option configures a registry.
type Option func(*Registry)

// WithClock overrides timestamps. The clock is stored in UTC truncated to
// microseconds so memory and Postgres agree.
func WithClock(now func() time.Time) Option {
	return func(r *Registry) {
		if now != nil {
			r.now = func() time.Time { return now().UTC().Truncate(time.Microsecond) }
		}
	}
}

// WithTenant selects the tenant stored on every row. Pass 4 replaces this
// with the caller's tenant. An empty id is rejected.
func WithTenant(id string) Option {
	return func(r *Registry) { r.tenant = id }
}

// NewMemory returns an in-memory registry seeded with catalog.search 1.0.0.
func NewMemory(opts ...Option) (*Registry, error) {
	r, err := newRegistry(opts)
	if err != nil {
		return nil, err
	}
	r.store = newMemoryStore()
	if err := r.seed(context.Background()); err != nil {
		return nil, err
	}
	return r, nil
}

// Open returns a Postgres registry. The pool must be able to read and write
// the nmc3 definition tables from migration 202610240004.
func Open(ctx context.Context, databaseURL string, opts ...Option) (*Registry, error) {
	if databaseURL == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "database URL is required"}
	}
	r, err := newRegistry(opts)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	r.store = &pgStore{pool: pool, tenant: r.tenant}
	if err := r.seed(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return r, nil
}

func newRegistry(opts []Option) (*Registry, error) {
	r := &Registry{
		tenant: DefaultTenant,
		now:    func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) },
	}
	for _, opt := range opts {
		opt(r)
	}
	if !validToken(r.tenant, maxIDLen) {
		return nil, &Error{Code: CodeInvalidInput, Message: "invalid tenant id"}
	}
	return r, nil
}

// Close releases the Postgres pool. It is safe on a memory registry.
func (r *Registry) Close() {
	if r != nil && r.store != nil {
		r.store.Close()
	}
}

func (r *Registry) seed(ctx context.Context) error {
	return r.RegisterTool(ctx, builtinCatalogTool())
}

func builtinCatalogTool() Tool {
	return Tool{
		ID:             BuiltinCatalogToolID,
		Version:        BuiltinCatalogToolVersion,
		Description:    "Read fictional support vendors.",
		InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":true}`),
		OutputSchema:   json.RawMessage(`{"type":"object","required":["vendors","source_ids"],"additionalProperties":false,"properties":{"vendors":{"type":"array"},"source_ids":{"type":"array","items":{"type":"string"}}}}`),
		SideEffect:     "READ_ONLY",
		TimeoutSeconds: 5,
	}
}

// SaveDraft creates a draft or replaces a draft/testing version in place.
// A published or retired version is left unchanged and returns CodeImmutable.
func (r *Registry) SaveDraft(ctx context.Context, actor string, spec Spec) (Version, error) {
	if err := checkActor(actor); err != nil {
		return Version{}, err
	}
	spec = normalize(spec)
	if fields := Validate(spec, nil, RefNone); len(fields) > 0 {
		return Version{}, validationError(fields)
	}
	prepared, _, hash, err := prepare(spec)
	if err != nil {
		return Version{}, err
	}
	var out Version
	err = r.store.Apply(ctx, func(s txStore) error {
		var saveErr error
		out, saveErr = r.saveDraft(ctx, s, actor, prepared, hash)
		return saveErr
	})
	if err != nil {
		return Version{}, mapMissing(err, "definition version not found")
	}
	return verify(out)
}

// SaveDraftJSON decodes one definition object, then saves it as a draft.
func (r *Registry) SaveDraftJSON(ctx context.Context, actor string, raw []byte) (Version, error) {
	spec, fields := decodeSpec(raw)
	if len(fields) > 0 {
		return Version{}, validationError(fields)
	}
	return r.SaveDraft(ctx, actor, spec)
}

// Revise copies work forward into a new draft version. The source version is
// not modified. spec.ID must match id and spec.Version must be different.
func (r *Registry) Revise(ctx context.Context, actor, id, fromVersion string, spec Spec) (Version, error) {
	if err := checkActor(actor); err != nil {
		return Version{}, err
	}
	spec = normalize(spec)
	if spec.ID != id {
		return Version{}, &Error{Code: CodeInvalidInput, Message: "revised definition id does not match the source", Fields: []FieldError{{Field: "id", Message: "must match the source definition"}}}
	}
	if spec.Version == "" || spec.Version == fromVersion {
		return Version{}, &Error{Code: CodeImmutable, Message: "editing a version requires a new version", Fields: []FieldError{{Field: "version", Message: "must be a new version"}}}
	}
	if fields := Validate(spec, nil, RefNone); len(fields) > 0 {
		return Version{}, validationError(fields)
	}
	prepared, _, hash, err := prepare(spec)
	if err != nil {
		return Version{}, err
	}
	var out Version
	err = r.store.Apply(ctx, func(s txStore) error {
		if _, err := s.GetVersion(ctx, id, fromVersion); err != nil {
			return err
		}
		var saveErr error
		out, saveErr = r.saveDraft(ctx, s, actor, prepared, hash)
		return saveErr
	})
	if err != nil {
		return Version{}, mapMissing(err, "definition version not found")
	}
	return verify(out)
}

func (r *Registry) saveDraft(ctx context.Context, s txStore, actor string, spec Spec, hash string) (Version, error) {
	if err := s.NameAvailable(ctx, spec.Name, spec.ID); err != nil {
		return Version{}, err
	}
	at := r.now()
	existing, err := s.GetVersion(ctx, spec.ID, spec.Version)
	if err != nil && !errors.Is(err, errNotFound) {
		return Version{}, err
	}
	if err == nil {
		if existing.State == StatePublished || existing.State == StateRetired {
			return Version{}, &Error{Code: CodeImmutable, Message: "published version cannot change; save a new version"}
		}
		existing.Spec = spec
		existing.ContentHash = hash
		existing.SchemaVersion = SchemaVersion
		existing.State = StateDraft
		existing.UpdatedAt = at
		existing.TestedBy = ""
		existing.TestedAt = nil
		existing.PublishedBy = ""
		existing.PublishedAt = nil
		existing.RetiredBy = ""
		existing.RetiredAt = nil
		if err := s.PutVersion(ctx, existing); err != nil {
			return Version{}, err
		}
	} else {
		created := Version{
			TenantID: r.tenant, Spec: spec, SchemaVersion: SchemaVersion, ContentHash: hash,
			State: StateDraft, CreatedBy: actor, CreatedAt: at, UpdatedAt: at,
		}
		if err := s.EnsureAgent(ctx, created); err != nil {
			return Version{}, err
		}
		if err := s.PutVersion(ctx, created); err != nil {
			return Version{}, err
		}
	}
	if err := s.RefreshAgent(ctx, spec.ID, spec.Name, at); err != nil {
		return Version{}, err
	}
	return s.GetVersion(ctx, spec.ID, spec.Version)
}

// Test validates a stored draft and moves it to testing. An invalid draft is
// left in place and returned as field errors. Published versions are not tested.
func (r *Registry) Test(ctx context.Context, actor, id, version string) (Version, error) {
	if err := checkActor(actor); err != nil {
		return Version{}, err
	}
	var out Version
	err := r.store.Apply(ctx, func(s txStore) error {
		existing, err := s.GetVersion(ctx, id, version)
		if err != nil {
			return err
		}
		if existing.State != StateDraft && existing.State != StateTesting {
			return &Error{Code: CodeInvalidInput, Message: "only a draft can be tested"}
		}
		refs, err := loadRefs(ctx, s)
		if err != nil {
			return err
		}
		if fields := Validate(existing.Spec, refs, RefTest); len(fields) > 0 {
			return validationError(fields)
		}
		at := r.now()
		existing.State = StateTesting
		existing.TestedBy = actor
		existing.TestedAt = timePtr(at)
		existing.UpdatedAt = at
		if err := s.PutVersion(ctx, existing); err != nil {
			return err
		}
		if err := s.RefreshAgent(ctx, id, existing.Spec.Name, at); err != nil {
			return err
		}
		out, err = s.GetVersion(ctx, id, version)
		return err
	})
	if err != nil {
		return Version{}, mapMissing(err, "definition version not found")
	}
	return verify(out)
}

// Publish validates spec and freezes that version. Invalid specs are rejected
// with field errors and are not stored. Republishing the same bytes is a no-op.
// A different body for a published or retired version is refused.
func (r *Registry) Publish(ctx context.Context, actor string, spec Spec) (Version, error) {
	if err := checkActor(actor); err != nil {
		return Version{}, err
	}
	spec = normalize(spec)
	var out Version
	err := r.store.Apply(ctx, func(s txStore) error {
		refs, err := loadRefs(ctx, s)
		if err != nil {
			return err
		}
		if fields := Validate(spec, refs, RefPublish); len(fields) > 0 {
			return validationError(fields)
		}
		prepared, _, hash, err := prepare(spec)
		if err != nil {
			return err
		}
		var pubErr error
		out, pubErr = r.publish(ctx, s, actor, prepared, hash)
		return pubErr
	})
	if err != nil {
		return Version{}, mapMissing(err, "definition version not found")
	}
	return verify(out)
}

// PublishJSON decodes one definition object and publishes it.
func (r *Registry) PublishJSON(ctx context.Context, actor string, raw []byte) (Version, error) {
	spec, fields := decodeSpec(raw)
	if len(fields) > 0 {
		return Version{}, validationError(fields)
	}
	return r.Publish(ctx, actor, spec)
}

func (r *Registry) publish(ctx context.Context, s txStore, actor string, spec Spec, hash string) (Version, error) {
	if err := s.NameAvailable(ctx, spec.Name, spec.ID); err != nil {
		return Version{}, err
	}
	at := r.now()
	existing, err := s.GetVersion(ctx, spec.ID, spec.Version)
	if errors.Is(err, errNotFound) {
		publishedAt := at
		created := Version{
			TenantID: r.tenant, Spec: spec, SchemaVersion: SchemaVersion, ContentHash: hash,
			State: StatePublished, CreatedBy: actor, CreatedAt: at, UpdatedAt: at,
			PublishedBy: actor, PublishedAt: &publishedAt,
		}
		if err := s.EnsureAgent(ctx, created); err != nil {
			return Version{}, err
		}
		if err := s.PutVersion(ctx, created); err != nil {
			return Version{}, err
		}
		if err := s.RefreshAgent(ctx, spec.ID, spec.Name, at); err != nil {
			return Version{}, err
		}
		return s.GetVersion(ctx, spec.ID, spec.Version)
	}
	if err != nil {
		return Version{}, err
	}
	if existing.ContentHash == hash && existing.State == StatePublished {
		return existing, nil
	}
	if existing.State == StatePublished || existing.State == StateRetired {
		return Version{}, &Error{Code: CodeImmutable, Message: "published version cannot change; save a new version"}
	}
	oldHash := existing.ContentHash
	publishedAt := at
	existing.Spec = spec
	existing.ContentHash = hash
	existing.SchemaVersion = SchemaVersion
	existing.State = StatePublished
	existing.UpdatedAt = at
	existing.PublishedBy = actor
	existing.PublishedAt = &publishedAt
	if oldHash != hash {
		existing.TestedBy = ""
		existing.TestedAt = nil
	}
	if err := s.PutVersion(ctx, existing); err != nil {
		return Version{}, err
	}
	if err := s.RefreshAgent(ctx, spec.ID, spec.Name, at); err != nil {
		return Version{}, err
	}
	return s.GetVersion(ctx, spec.ID, spec.Version)
}

// Retire blocks new runs of a published version. The spec, publisher, and
// existing run pins stay as they were. Retiring twice keeps the first timestamp.
func (r *Registry) Retire(ctx context.Context, actor, id, version string) (Version, error) {
	if err := checkActor(actor); err != nil {
		return Version{}, err
	}
	var out Version
	err := r.store.Apply(ctx, func(s txStore) error {
		existing, err := s.GetVersion(ctx, id, version)
		if err != nil {
			return err
		}
		if existing.State == StateRetired {
			out = existing
			return nil
		}
		if existing.State != StatePublished {
			return &Error{Code: CodeInvalidInput, Message: "only a published version can be retired"}
		}
		at := r.now()
		retired, err := s.RetireVersion(ctx, id, version, actor, at)
		if err != nil {
			return err
		}
		if err := s.RefreshAgent(ctx, id, existing.Spec.Name, at); err != nil {
			return err
		}
		out = retired
		return nil
	})
	if err != nil {
		return Version{}, mapMissing(err, "definition version not found")
	}
	return verify(out)
}

// Get returns one version in any lifecycle state, including retired.
func (r *Registry) Get(ctx context.Context, id, version string) (Version, error) {
	var out Version
	err := r.store.Apply(ctx, func(s txStore) error {
		var getErr error
		out, getErr = s.GetVersion(ctx, id, version)
		return getErr
	})
	if err != nil {
		return Version{}, mapMissing(err, "definition version not found")
	}
	return verify(out)
}

// List returns every version for the registry tenant, ordered by id then version.
func (r *Registry) List(ctx context.Context) ([]Version, error) {
	var out []Version
	err := r.store.Apply(ctx, func(s txStore) error {
		var listErr error
		out, listErr = s.ListVersions(ctx)
		return listErr
	})
	if err != nil {
		return nil, err
	}
	sortVersions(out)
	verified := make([]Version, 0, len(out))
	for _, v := range out {
		checked, err := verify(v)
		if err != nil {
			return nil, err
		}
		verified = append(verified, checked)
	}
	return verified, nil
}

// PublishedSummaries returns the v1 agent list: published versions only.
func (r *Registry) PublishedSummaries(ctx context.Context) ([]contracts.AgentSummary, error) {
	versions, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.AgentSummary, 0)
	for _, v := range versions {
		if v.State != StatePublished {
			continue
		}
		out = append(out, contracts.AgentSummary{ID: v.Spec.ID, Version: v.Spec.Version, Name: v.Spec.Name})
	}
	return out, nil
}

// ResolveForNewRun returns a published version. Retired, draft, and missing
// versions cannot start a run. Retired is CodeRetired; missing is CodeNotFound.
func (r *Registry) ResolveForNewRun(ctx context.Context, id, version string) (Version, error) {
	v, err := r.Get(ctx, id, version)
	if err != nil {
		return Version{}, err
	}
	switch v.State {
	case StatePublished:
		return v, nil
	case StateRetired:
		return Version{}, &Error{Code: CodeRetired, Message: "agent version is retired"}
	default:
		return Version{}, &Error{Code: CodeInvalidInput, Message: "agent version is not published"}
	}
}

// PinRun snapshots a published version onto a run. A second pin of the same
// bytes is a no-op. A different version or hash conflicts. Retired versions
// cannot be newly pinned; pins already stored remain readable through Pinned.
func (r *Registry) PinRun(ctx context.Context, runID, id, version string) (Pin, error) {
	if !validToken(runID, maxRunIDLen) {
		return Pin{}, &Error{Code: CodeInvalidInput, Message: "run id is required"}
	}
	var out Pin
	err := r.store.Apply(ctx, func(s txStore) error {
		existing, err := s.GetVersion(ctx, id, version)
		if err != nil {
			return err
		}
		if existing.State == StateRetired {
			return &Error{Code: CodeRetired, Message: "agent version is retired"}
		}
		if existing.State != StatePublished {
			return &Error{Code: CodeInvalidInput, Message: "agent version is not published"}
		}
		if _, _, hash, err := prepare(existing.Spec); err != nil {
			return err
		} else if hash != existing.ContentHash {
			return &Error{Code: CodeConflict, Message: "stored definition hash does not match its spec"}
		}
		pin, err := s.GetPin(ctx, runID, id)
		if err == nil {
			if pin.Version == version && pin.ContentHash == existing.ContentHash {
				out = pin
				return nil
			}
			return &Error{Code: CodeConflict, Message: "run is already pinned to a different definition version"}
		}
		if !errors.Is(err, errNotFound) {
			return err
		}
		created := Pin{
			RunID: runID, TenantID: r.tenant, AgentID: id, Version: version,
			ContentHash: existing.ContentHash, Spec: existing.Spec, PinnedAt: r.now(),
		}
		if err := s.InsertPin(ctx, created); err != nil {
			return err
		}
		out, err = s.GetPin(ctx, runID, id)
		return err
	})
	if err != nil {
		return Pin{}, mapMissing(err, "definition version not found")
	}
	return verifyPin(out)
}

// Pinned returns the spec snapshot stored for a run and agent.
func (r *Registry) Pinned(ctx context.Context, runID, agentID string) (Pin, error) {
	var out Pin
	err := r.store.Apply(ctx, func(s txStore) error {
		var getErr error
		out, getErr = s.GetPin(ctx, runID, agentID)
		return getErr
	})
	if err != nil {
		return Pin{}, mapMissing(err, "run pin not found")
	}
	return verifyPin(out)
}

// Pins lists every agent snapshot stored for a run, ordered by agent id.
func (r *Registry) Pins(ctx context.Context, runID string) ([]Pin, error) {
	var out []Pin
	err := r.store.Apply(ctx, func(s txStore) error {
		var listErr error
		out, listErr = s.ListPins(ctx, runID)
		return listErr
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	verified := make([]Pin, 0, len(out))
	for _, pin := range out {
		checked, err := verifyPin(pin)
		if err != nil {
			return nil, err
		}
		verified = append(verified, checked)
	}
	return verified, nil
}

// RegisterTool stores an immutable read-only tool version. The same bytes may
// be registered again. A different body for the same id and version conflicts.
func (r *Registry) RegisterTool(ctx context.Context, tool Tool) error {
	if fields := validateTool(tool); len(fields) > 0 {
		return validationError(fields)
	}
	_, hash, err := hashTool(tool)
	if err != nil {
		return err
	}
	return r.store.Apply(ctx, func(s txStore) error {
		return s.InsertTool(ctx, tool, hash, r.now())
	})
}

// ListTools returns registered tool versions ordered by id then version.
func (r *Registry) ListTools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	err := r.store.Apply(ctx, func(s txStore) error {
		var listErr error
		out, listErr = s.ListTools(ctx)
		return listErr
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Version < out[j].Version
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// References snapshots the catalog used for tool and delegate checks.
func (r *Registry) References(ctx context.Context) (*Refs, error) {
	var refs *Refs
	err := r.store.Apply(ctx, func(s txStore) error {
		var loadErr error
		refs, loadErr = loadRefs(ctx, s)
		return loadErr
	})
	return refs, err
}

func loadRefs(ctx context.Context, s txStore) (*Refs, error) {
	versions, err := s.ListVersions(ctx)
	if err != nil {
		return nil, err
	}
	tools, err := s.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	refs := &Refs{Agents: make(map[string]string, len(versions)), Tools: make(map[string]struct{}, len(tools))}
	for _, v := range versions {
		refs.Agents[refKey(v.Spec.ID, v.Spec.Version)] = v.State
	}
	for _, tool := range tools {
		refs.Tools[tool.ID] = struct{}{}
	}
	return refs, nil
}

func verify(v Version) (Version, error) {
	_, _, hash, err := prepare(v.Spec)
	if err != nil {
		return Version{}, err
	}
	if hash != v.ContentHash {
		return Version{}, &Error{Code: CodeConflict, Message: "stored definition hash does not match its spec"}
	}
	return v, nil
}

func verifyPin(pin Pin) (Pin, error) {
	_, _, hash, err := prepare(pin.Spec)
	if err != nil {
		return Pin{}, err
	}
	if hash != pin.ContentHash {
		return Pin{}, &Error{Code: CodeConflict, Message: "stored pin hash does not match its spec"}
	}
	return pin, nil
}

func mapMissing(err error, message string) error {
	if err == nil {
		return nil
	}
	var reg *Error
	if errors.As(err, &reg) {
		return err
	}
	if errors.Is(err, errNotFound) {
		return &Error{Code: CodeNotFound, Message: message}
	}
	return err
}

func sortVersions(versions []Version) {
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].Spec.ID == versions[j].Spec.ID {
			return versions[i].Spec.Version < versions[j].Spec.Version
		}
		return versions[i].Spec.ID < versions[j].Spec.ID
	})
}
