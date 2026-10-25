package automations

import (
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed five-field cron expression: minute, hour, day of
// month, month, and day of week. Each field is a bit set of allowed values.
// Day of month and day of week follow the usual cron rule: when both are
// restricted, a time matches if either one does.
type Schedule struct {
	minute, hour, dom, month, dow uint64
	domAny, dowAny                bool
	loc                           *time.Location
}

var shortcuts = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
var dayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// ParseSchedule reads a cron expression in the given IANA time zone. An empty
// zone means UTC.
func ParseSchedule(expr, zone string) (Schedule, error) {
	loc := time.UTC
	if zone != "" {
		l, err := time.LoadLocation(zone)
		if err != nil {
			return Schedule{}, fmt.Errorf("unknown time zone %q", zone)
		}
		loc = l
	}
	expr = strings.TrimSpace(strings.ToLower(expr))
	if full, ok := shortcuts[expr]; ok {
		expr = full
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return Schedule{}, fmt.Errorf("schedule needs five fields: minute hour day month weekday")
	}
	s := Schedule{loc: loc}
	var err error
	if s.minute, err = field(fields[0], 0, 59, nil); err != nil {
		return Schedule{}, fmt.Errorf("minute: %w", err)
	}
	if s.hour, err = field(fields[1], 0, 23, nil); err != nil {
		return Schedule{}, fmt.Errorf("hour: %w", err)
	}
	if s.dom, err = field(fields[2], 1, 31, nil); err != nil {
		return Schedule{}, fmt.Errorf("day of month: %w", err)
	}
	if s.month, err = field(fields[3], 1, 12, monthNames); err != nil {
		return Schedule{}, fmt.Errorf("month: %w", err)
	}
	if s.dow, err = field(fields[4], 0, 7, dayNames); err != nil {
		return Schedule{}, fmt.Errorf("day of week: %w", err)
	}
	// 7 is another name for Sunday.
	if s.dow&(1<<7) != 0 {
		s.dow = s.dow&^(1<<7) | 1
	}
	s.domAny = fields[2] == "*" || fields[2] == "?"
	s.dowAny = fields[4] == "*" || fields[4] == "?"
	return s, nil
}

func field(raw string, lo, hi int, names map[string]int) (uint64, error) {
	var set uint64
	for _, part := range strings.Split(raw, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return 0, fmt.Errorf("bad step %q", part)
			}
			step, part = n, part[:i]
		}
		start, end := lo, hi
		switch {
		case part == "*" || part == "?":
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			var err error
			if start, err = value(a, names); err != nil {
				return 0, err
			}
			if end, err = value(b, names); err != nil {
				return 0, err
			}
		default:
			v, err := value(part, names)
			if err != nil {
				return 0, err
			}
			start = v
			// "5/15" means from 5 to the end in steps of 15.
			if step == 1 {
				end = v
			}
		}
		if start < lo || end > hi || start > end {
			return 0, fmt.Errorf("%q is outside %d-%d", part, lo, hi)
		}
		for v := start; v <= end; v += step {
			set |= 1 << uint(v)
		}
	}
	if set == 0 {
		return 0, fmt.Errorf("no values")
	}
	return set, nil
}

func value(raw string, names map[string]int) (int, error) {
	if v, ok := names[raw]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("bad value %q", raw)
	}
	return v, nil
}

func has(set uint64, v int) bool { return set&(1<<uint(v)) != 0 }

func (s Schedule) dayMatches(t time.Time) bool {
	dom, dow := has(s.dom, t.Day()), has(s.dow, int(t.Weekday()))
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	default:
		return dom || dow
	}
}

// Next returns the first matching minute strictly after t, or the zero time
// when nothing matches within five years (for example "0 0 31 2 *").
func (s Schedule) Next(t time.Time) time.Time {
	t = t.In(s.loc).Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !has(s.month, int(t.Month())) {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, s.loc)
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, s.loc)
			continue
		}
		if !has(s.hour, t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, s.loc)
			continue
		}
		if !has(s.minute, t.Minute()) {
			// Jump to the next allowed minute in this hour, if any.
			rest := s.minute >> uint(t.Minute()+1) << uint(t.Minute()+1)
			if rest == 0 {
				t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, s.loc)
			} else {
				t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), bits.TrailingZeros64(rest), 0, 0, s.loc)
			}
			continue
		}
		return t.UTC()
	}
	return time.Time{}
}
