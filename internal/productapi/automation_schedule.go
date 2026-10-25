package productapi

import (
	"fmt"
	"strings"
	"time"
)

type automationSchedule struct {
	Kind            string     `json:"kind"`
	RunAt           *time.Time `json:"run_at,omitempty"`
	IntervalSeconds int        `json:"interval_seconds,omitempty"`
	LocalTime       string     `json:"local_time,omitempty"`
	Weekday         *int       `json:"weekday,omitempty"`
	Timezone        string     `json:"timezone,omitempty"`
}

// nextAutomationTime returns the first occurrence strictly after the given
// instant. Recurring schedules skip missed intervals instead of replaying an
// unbounded backlog after an outage.
func nextAutomationTime(schedule automationSchedule, after time.Time) (*time.Time, error) {
	after = after.UTC()
	switch schedule.Kind {
	case "once":
		if schedule.RunAt == nil {
			return nil, fmt.Errorf("run_at is required")
		}
		at := schedule.RunAt.UTC()
		if !at.After(after) {
			return nil, nil
		}
		return &at, nil
	case "interval":
		if schedule.IntervalSeconds < 60 || schedule.IntervalSeconds > 30*24*60*60 {
			return nil, fmt.Errorf("interval_seconds must be between 60 and 2592000")
		}
		step := time.Duration(schedule.IntervalSeconds) * time.Second
		start := after.Add(step)
		if schedule.RunAt != nil {
			start = schedule.RunAt.UTC()
			if !start.After(after) {
				start = start.Add(time.Duration(int64(after.Sub(start)/step)+1) * step)
			}
		}
		return &start, nil
	case "daily", "weekly":
		zone := schedule.Timezone
		if zone == "" {
			zone = "UTC"
		}
		location, err := time.LoadLocation(zone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone")
		}
		clock, err := time.Parse("15:04", schedule.LocalTime)
		if err != nil || len(schedule.LocalTime) != 5 {
			return nil, fmt.Errorf("local_time must be HH:MM")
		}
		if schedule.Kind == "weekly" && (schedule.Weekday == nil || *schedule.Weekday < 0 || *schedule.Weekday > 6) {
			return nil, fmt.Errorf("weekday must be 0 (Sunday) through 6 (Saturday)")
		}
		local := after.In(location)
		for day := 0; day < 15; day++ {
			date := local.AddDate(0, 0, day)
			if schedule.Kind == "weekly" && int(date.Weekday()) != *schedule.Weekday {
				continue
			}
			candidate := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
			// A nonexistent wall time during a DST spring transition is skipped.
			if candidate.Hour() != clock.Hour() || candidate.Minute() != clock.Minute() {
				continue
			}
			if candidate.After(after) {
				utc := candidate.UTC()
				return &utc, nil
			}
		}
		return nil, fmt.Errorf("could not find the next schedule time")
	default:
		return nil, fmt.Errorf("kind must be once, interval, daily, or weekly")
	}
}

func validateAutomationSchedule(schedule automationSchedule, now time.Time) (time.Time, error) {
	if strings.TrimSpace(schedule.Timezone) == "" {
		schedule.Timezone = "UTC"
	}
	if schedule.RunAt != nil && schedule.RunAt.Before(now.Add(-time.Second)) {
		return time.Time{}, fmt.Errorf("run_at must be in the future")
	}
	if schedule.Kind == "once" && schedule.RunAt == nil {
		return time.Time{}, fmt.Errorf("run_at is required")
	}
	next, err := nextAutomationTime(schedule, now)
	if err != nil {
		return time.Time{}, err
	}
	if next == nil {
		return time.Time{}, fmt.Errorf("the schedule has no future occurrence")
	}
	return *next, nil
}
