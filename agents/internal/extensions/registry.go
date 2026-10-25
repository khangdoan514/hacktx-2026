package extensions

import (
	"net/url"
	"sync"
)

// Registry stores immutable published tool versions and run pins.
type Registry struct {
	mu    sync.Mutex
	seq   uint64
	tools map[string]ToolRecord
	order map[string]uint64
	pins  map[string]pinEntry
}

type pinEntry struct {
	meta  Pin
	tools map[string]ToolRecord
}

func NewRegistry() *Registry {
	return &Registry{
		tools: map[string]ToolRecord{},
		order: map[string]uint64{},
		pins:  map[string]pinEntry{},
	}
}

func recordKey(tenant, id, version string) string {
	return tenant + "\x00" + id + "\x00" + version
}

// PutDraft stores a new draft or replaces an existing draft. Published and
// retired versions are immutable.
func (r *Registry) PutDraft(rec ToolRecord) error {
	normalized, err := normalize(rec)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := recordKey(normalized.TenantID, normalized.ID, normalized.Version)
	if prev, ok := r.tools[key]; ok && prev.Lifecycle != LifecycleDraft {
		return fail(CodeConflict, "published tool versions are immutable")
	}
	r.tools[key] = normalized
	return nil
}

// Publish freezes a draft. Publishing the same version again returns the
// stored record. A retired version stays retired.
func (r *Registry) Publish(tenant, id, version string) (ToolRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := recordKey(tenant, id, version)
	rec, ok := r.tools[key]
	if !ok {
		return ToolRecord{}, fail(CodeNotFound, "tool version not found")
	}
	switch rec.Lifecycle {
	case LifecycleRetired:
		return ToolRecord{}, fail(CodeConflict, "retired tool versions stay retired")
	case LifecyclePublished:
		return cloneRecord(rec), nil
	}
	rec.Lifecycle = LifecyclePublished
	r.seq++
	r.order[key] = r.seq
	r.tools[key] = rec
	return cloneRecord(rec), nil
}

// Retire keeps the published bytes readable and stops new pins from selecting
// that version. An existing run pin is a separate copy and does not change.
func (r *Registry) Retire(tenant, id, version string) (ToolRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := recordKey(tenant, id, version)
	rec, ok := r.tools[key]
	if !ok {
		return ToolRecord{}, fail(CodeNotFound, "tool version not found")
	}
	if rec.Lifecycle == LifecycleDraft {
		return ToolRecord{}, fail(CodeConflict, "draft tools are discarded by not publishing")
	}
	rec.Lifecycle = LifecycleRetired
	r.tools[key] = rec
	return cloneRecord(rec), nil
}

func (r *Registry) Get(tenant, id, version string) (ToolRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.tools[recordKey(tenant, id, version)]
	if !ok {
		return ToolRecord{}, fail(CodeNotFound, "tool version not found")
	}
	return cloneRecord(rec), nil
}

func (r *Registry) List(tenant string) []ToolRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []ToolRecord{}
	for _, rec := range r.tools {
		if rec.TenantID == tenant {
			out = append(out, cloneRecord(rec))
		}
	}
	sortRecords(out)
	return out
}

func sortRecords(out []ToolRecord) {
	for i := 1; i < len(out); i++ {
		item := out[i]
		j := i
		for j > 0 && (out[j-1].ID > item.ID || (out[j-1].ID == item.ID && out[j-1].Version > item.Version)) {
			out[j] = out[j-1]
			j--
		}
		out[j] = item
	}
}

// Pin snapshots the latest published version of each tool for a run.
// The first successful pin for a run ID wins. A later publish does not
// change it.
func (r *Registry) Pin(runID, tenant string, toolIDs []string) (Pin, error) {
	if !validToken(runID) || !validToken(tenant) || len(toolIDs) == 0 {
		return Pin{}, fail(CodeInvalidInput, "pin request is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.pins[runID]; ok {
		if existing.meta.TenantID != tenant {
			return Pin{}, fail(CodeToolNotAllowed, "run pin belongs to another tenant")
		}
		if !sameIDs(existing.meta.Tools, toolIDs) {
			return Pin{}, fail(CodeConflict, "run tool versions are already pinned")
		}
		return clonePin(existing.meta), nil
	}
	entry := pinEntry{
		meta:  Pin{RunID: runID, TenantID: tenant, Tools: map[string]string{}},
		tools: map[string]ToolRecord{},
	}
	for _, id := range toolIDs {
		if _, seen := entry.meta.Tools[id]; seen {
			return Pin{}, fail(CodeInvalidInput, "pin request is invalid")
		}
		rec, ok := r.latestLocked(tenant, id)
		if !ok {
			return Pin{}, fail(CodeNotFound, "published tool not found")
		}
		entry.tools[id] = cloneRecord(rec)
		entry.meta.Tools[id] = rec.Version
	}
	r.pins[runID] = entry
	return clonePin(entry.meta), nil
}

func sameIDs(pinned map[string]string, ids []string) bool {
	if len(pinned) != len(ids) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if _, ok := pinned[id]; !ok || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func (r *Registry) LookupPin(runID, tenant string) (Pin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.pins[runID]
	if !ok {
		return Pin{}, fail(CodeNotFound, "run pin not found")
	}
	if entry.meta.TenantID != tenant {
		return Pin{}, fail(CodeToolNotAllowed, "run pin belongs to another tenant")
	}
	return clonePin(entry.meta), nil
}

// Resolve returns the pinned snapshot for a tenant. It does not read the
// mutable registry, so a newer publish cannot change the result.
func (r *Registry) Resolve(runID, tenant, toolID string) (ToolRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.pins[runID]
	if !ok {
		return ToolRecord{}, fail(CodeNotFound, "run pin not found")
	}
	if entry.meta.TenantID != tenant {
		return ToolRecord{}, fail(CodeToolNotAllowed, "tool is not allowed for this tenant")
	}
	rec, ok := entry.tools[toolID]
	if !ok {
		return ToolRecord{}, fail(CodeToolNotAllowed, "tool is not allowed for this agent")
	}
	return cloneRecord(rec), nil
}

func (r *Registry) latestLocked(tenant, id string) (ToolRecord, bool) {
	var best ToolRecord
	var bestSeq uint64
	found := false
	for key, rec := range r.tools {
		if rec.TenantID != tenant || rec.ID != id || rec.Lifecycle != LifecyclePublished {
			continue
		}
		seq := r.order[key]
		if !found || seq > bestSeq {
			best = rec
			bestSeq = seq
			found = true
		}
	}
	return best, found
}

// ImportDiscovered stores worker registration metadata as drafts. The caller
// publishes after reviewing permissions and the network policy.
func (r *Registry) ImportDiscovered(opts ImportOptions, items []DiscoveredTool) error {
	if len(items) == 0 {
		return fail(CodeInvalidInput, "worker exposed no tools")
	}
	if opts.TimeoutMS <= 0 {
		opts.TimeoutMS = 5000
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = 1 << 20
	}
	if opts.RetryMax <= 0 {
		opts.RetryMax = 1
	}
	if opts.WorkerID == "" {
		if parsed, err := url.Parse(opts.Endpoint); err == nil {
			opts.WorkerID = parsed.Host
		}
	}
	for _, item := range items {
		rec := ToolRecord{
			TenantID:     opts.TenantID,
			ID:           item.ID,
			Version:      item.Version,
			Description:  item.Description,
			InputSchema:  item.InputSchema,
			OutputSchema: item.OutputSchema,
			SideEffect:   item.SideEffect,
			TimeoutMS:    opts.TimeoutMS,
			Class:        ClassWorker,
			Endpoint:     Endpoint{URL: opts.Endpoint, WorkerID: opts.WorkerID},
			AuthRef:      opts.AuthRef,
			Permissions:  cloneStrings(opts.Permissions),
			Retry:        RetryPolicy{MaxAttempts: opts.RetryMax},
			Resources:    ResourceLimits{MaxOutputBytes: opts.MaxOutputBytes},
			Network:      NetworkPolicy{Allow: cloneStrings(opts.Network.Allow)},
			Filesystem:   opts.Filesystem,
		}
		if err := r.PutDraft(rec); err != nil {
			return err
		}
	}
	return nil
}

// Pinned returns a copy for inspection. Mutating it does not change the run.
func (r *Registry) Pinned(runID, toolID string) (ToolRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.pins[runID]
	if !ok {
		return ToolRecord{}, fail(CodeNotFound, "run pin not found")
	}
	rec, ok := entry.tools[toolID]
	if !ok {
		return ToolRecord{}, fail(CodeToolNotAllowed, "tool is not pinned for this run")
	}
	return cloneRecord(rec), nil
}
