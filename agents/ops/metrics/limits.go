package metrics

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	ErrLimitInvalid = errors.New("metrics: concurrency limit must be positive")
	ErrOrgRequired  = errors.New("metrics: organization id is required")
	ErrNilContext   = errors.New("metrics: context is required")
)

// Limiter caps how many runs one organization may have in flight inside this
// process. Each process has its own counters. Two API processes do not share
// a limit.
type Limiter struct {
	mu           sync.Mutex
	sems         map[string]chan struct{}
	limits       map[string]int
	defaultLimit int
	admitted     atomic.Int64
	rejected     atomic.Int64
}

// NewLimiter sets the default in-flight cap and optional per-organization
// overrides. defaultLimit and every override must be positive. The override
// map is copied; later changes by the caller have no effect.
func NewLimiter(defaultLimit int, perTenant map[string]int) (*Limiter, error) {
	if defaultLimit <= 0 {
		return nil, ErrLimitInvalid
	}
	limits := make(map[string]int, len(perTenant))
	for tenant, limit := range perTenant {
		if tenant == "" || strings.TrimSpace(tenant) != tenant {
			return nil, ErrOrgRequired
		}
		if limit <= 0 {
			return nil, ErrLimitInvalid
		}
		limits[tenant] = limit
	}
	return &Limiter{
		sems:         make(map[string]chan struct{}),
		limits:       limits,
		defaultLimit: defaultLimit,
	}, nil
}

// Limit returns the configured cap for tenantID. Unknown organizations use
// the default. The limits map is immutable after NewLimiter returns.
func (l *Limiter) Limit(tenantID string) int {
	if limit, ok := l.limits[tenantID]; ok {
		return limit
	}
	return l.defaultLimit
}

// InFlight reports admitted work that has not been released. The count is
// exact when no Acquire or release is running for that organization.
func (l *Limiter) InFlight(tenantID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := l.sems[tenantID]
	if ch == nil {
		return 0
	}
	return len(ch)
}

// Stats returns process-local admission counts. Rejected counts acquires that
// waited for a free slot and then stopped because the context ended. It does
// not count calls rejected for a missing organization id.
func (l *Limiter) Stats() (admitted, rejected int64) {
	return l.admitted.Load(), l.rejected.Load()
}

// Acquire takes one in-flight slot for tenantID. The returned release function
// frees the slot. Extra release calls are no-ops. A cancelled context does not
// take a slot when the cancellation is already visible; if the caller is
// waiting because the organization is at its cap, cancellation returns
// ctx.Err() and counts as a rejection.
func (l *Limiter) Acquire(ctx context.Context, tenantID string) (func(), error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch, err := l.semaphore(tenantID)
	if err != nil {
		return nil, err
	}
	select {
	case ch <- struct{}{}:
		l.admitted.Add(1)
		var once sync.Once
		return func() {
			once.Do(func() { <-ch })
		}, nil
	case <-ctx.Done():
		l.rejected.Add(1)
		return nil, ctx.Err()
	}
}

func (l *Limiter) semaphore(tenantID string) (chan struct{}, error) {
	if tenantID == "" || strings.TrimSpace(tenantID) != tenantID {
		return nil, ErrOrgRequired
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if ch, ok := l.sems[tenantID]; ok {
		return ch, nil
	}
	ch := make(chan struct{}, l.Limit(tenantID))
	l.sems[tenantID] = ch
	return ch, nil
}
