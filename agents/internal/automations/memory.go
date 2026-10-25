package automations

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryStore keeps automations in process. It is for tests and for a local
// agent process without NMC3_DATABASE_URL; schedules do not survive a restart.
type MemoryStore struct {
	mu      sync.Mutex
	items   map[string]Automation
	firings []Firing
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{items: map[string]Automation{}} }

func (m *MemoryStore) Create(_ context.Context, a Automation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[a.ID] = a
	return nil
}

func (m *MemoryStore) Update(_ context.Context, a Automation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.items[a.ID]
	if !ok || old.TenantID != a.TenantID {
		return ErrNotFound
	}
	m.items[a.ID] = a
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, tenant, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.items[id]
	if !ok || a.TenantID != tenant {
		return ErrNotFound
	}
	delete(m.items, id)
	kept := m.firings[:0]
	for _, f := range m.firings {
		if f.AutomationID != id {
			kept = append(kept, f)
		}
	}
	m.firings = kept
	return nil
}

func (m *MemoryStore) Get(_ context.Context, tenant, id string) (Automation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.items[id]
	if !ok || a.TenantID != tenant {
		return Automation{}, ErrNotFound
	}
	return a, nil
}

func (m *MemoryStore) List(_ context.Context, tenant string) ([]Automation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Automation{}
	for _, a := range m.items {
		if a.TenantID == tenant {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryStore) Claim(_ context.Context, now time.Time, next func(Automation) *time.Time) ([]Automation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var due []Automation
	for id, a := range m.items {
		if !a.Enabled || a.NextRunAt == nil || a.NextRunAt.After(now) {
			continue
		}
		due = append(due, a)
		fired := now
		a.LastFiredAt = &fired
		a.NextRunAt = next(a)
		m.items[id] = a
	}
	return due, nil
}

func (m *MemoryStore) AddFiring(_ context.Context, f Firing) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.firings = append(m.firings, f)
	return nil
}

func (m *MemoryStore) Firings(_ context.Context, tenant, automationID string, limit int) ([]Firing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Firing{}
	for i := len(m.firings) - 1; i >= 0 && len(out) < limit; i-- {
		f := m.firings[i]
		if f.TenantID == tenant && (automationID == "" || f.AutomationID == automationID) {
			out = append(out, f)
		}
	}
	return out, nil
}
