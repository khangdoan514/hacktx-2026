package automations

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

// Service owns the schedule loop and the firing record. Runs are started
// through the coordinator, so a scheduled run is validated, limited, and
// recorded exactly like one an employee starts from the console.
type Service struct {
	Store       Store
	Coordinator contracts.Coordinator
	Runs        contracts.RunStore
	// Interval is how often the loop looks for due automations.
	Interval time.Duration
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// nextAfter is the following slot for a, or nil when the schedule no longer
// parses or never matches again. Missed slots are not replayed: a process
// that was down for a day fires once and then resumes the schedule.
func nextAfter(a Automation, t time.Time) *time.Time {
	sched, err := ParseSchedule(a.Schedule, a.Timezone)
	if err != nil {
		return nil
	}
	next := sched.Next(t)
	if next.IsZero() {
		return nil
	}
	return &next
}

// Run checks for due automations until ctx ends.
func (s *Service) Run(ctx context.Context) {
	interval := s.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick fires every automation that is due now.
func (s *Service) Tick(ctx context.Context) {
	now := s.now()
	due, err := s.Store.Claim(ctx, now, func(a Automation) *time.Time { return nextAfter(a, now) })
	if err != nil {
		log.Printf("NMC3 automations: claim due schedules: %v", err)
		return
	}
	for _, a := range due {
		scheduled := now
		if a.NextRunAt != nil {
			scheduled = *a.NextRunAt
		}
		s.Fire(ctx, a, TriggerSchedule, scheduled)
	}
}

// Fire starts one run for a as the employee who created it and records the
// outcome. A refused start is recorded, not retried; the next slot tries again.
func (s *Service) Fire(ctx context.Context, a Automation, trigger string, scheduled time.Time) Firing {
	f := Firing{ID: newID("fire-"), AutomationID: a.ID, TenantID: a.TenantID, Trigger: trigger, ScheduledFor: scheduled, FiredAt: s.now(), Name: a.Name, AgentID: a.Request.AgentID}
	startCtx, cancel := context.WithTimeout(toolpacks.WithOwner(ctx, a.TenantID+":"+a.CreatedBy), time.Minute)
	defer cancel()
	run, err := s.Coordinator.Start(startCtx, a.Request)
	if err != nil {
		f.Error = "the run could not start"
		var runErr *contracts.RunError
		if errors.As(err, &runErr) {
			f.Error = runErr.Message
		}
		log.Printf("NMC3 automations: %s (%s) did not start: %v", a.ID, trigger, err)
	} else {
		f.RunID, f.State = run.ID, run.State
	}
	if err := s.Store.AddFiring(context.WithoutCancel(ctx), f); err != nil {
		log.Printf("NMC3 automations: record firing for %s: %v", a.ID, err)
	}
	return f
}

// Firings lists firings with each run's current state, agent, and usage.
func (s *Service) Firings(ctx context.Context, tenant, automationID string, limit int) ([]Firing, error) {
	firings, err := s.Store.Firings(ctx, tenant, automationID, limit)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for i := range firings {
		f := &firings[i]
		if f.Name == "" {
			if _, ok := names[f.AutomationID]; !ok {
				if a, err := s.Store.Get(ctx, tenant, f.AutomationID); err == nil {
					names[f.AutomationID] = a.Name
				}
			}
			f.Name = names[f.AutomationID]
		}
		if f.RunID == "" || s.Runs == nil {
			continue
		}
		run, err := s.Runs.Get(ctx, f.RunID)
		if err != nil {
			continue
		}
		usage := run.Usage
		f.State, f.AgentID, f.Usage = run.State, run.AgentID, &usage
		if run.Error != nil {
			f.RunError = run.Error.Message
		}
	}
	return firings, nil
}
