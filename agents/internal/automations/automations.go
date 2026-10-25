// Package automations starts agent runs on a cron schedule. An automation
// stores a complete StartRunRequest; when its schedule comes due the
// scheduler hands that request to the same coordinator POST /v1/agent-runs
// uses, as the employee who created it, and records a firing. The run itself
// is the record of what the agent did; a firing links the schedule to it.
package automations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// ErrNotFound is returned for an automation outside the caller's tenant.
var ErrNotFound = errors.New("automation not found")

const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

type Automation struct {
	ID             string                    `json:"id"`
	TenantID       string                    `json:"-"`
	CreatedBy      string                    `json:"created_by"`
	CreatedByEmail string                    `json:"created_by_email,omitempty"`
	Name           string                    `json:"name"`
	Schedule       string                    `json:"schedule"`
	Timezone       string                    `json:"timezone"`
	Enabled        bool                      `json:"enabled"`
	Request        contracts.StartRunRequest `json:"request"`
	NextRunAt      *time.Time                `json:"next_run_at,omitempty"`
	LastFiredAt    *time.Time                `json:"last_fired_at,omitempty"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
	// LastFiring is filled on read so a list shows the latest outcome.
	LastFiring *Firing `json:"last_firing,omitempty"`
}

// Firing is one attempt to start a run. RunID is empty when the coordinator
// refused the request; Error then says why.
type Firing struct {
	ID           string    `json:"id"`
	AutomationID string    `json:"automation_id"`
	TenantID     string    `json:"-"`
	Trigger      string    `json:"trigger"`
	ScheduledFor time.Time `json:"scheduled_for"`
	FiredAt      time.Time `json:"fired_at"`
	RunID        string    `json:"run_id,omitempty"`
	Error        string    `json:"error,omitempty"`
	// The fields below are read from the run when a firing is listed.
	State    contracts.State  `json:"state,omitempty"`
	AgentID  string           `json:"agent_id,omitempty"`
	RunError string           `json:"run_error,omitempty"`
	Usage    *contracts.Usage `json:"usage,omitempty"`
	Name     string           `json:"automation_name,omitempty"`
}

type Store interface {
	Create(ctx context.Context, a Automation) error
	Update(ctx context.Context, a Automation) error
	Delete(ctx context.Context, tenant, id string) error
	Get(ctx context.Context, tenant, id string) (Automation, error)
	List(ctx context.Context, tenant string) ([]Automation, error)
	// Claim moves every enabled automation due at or before now to its next
	// time and returns the ones this caller won. next computes the following
	// slot. A second scheduler sharing the database does not fire them again.
	Claim(ctx context.Context, now time.Time, next func(Automation) *time.Time) ([]Automation, error)
	AddFiring(ctx context.Context, f Firing) error
	// Firings lists newest first. An empty automation ID lists the tenant.
	Firings(ctx context.Context, tenant, automationID string, limit int) ([]Firing, error)
}

func newID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
