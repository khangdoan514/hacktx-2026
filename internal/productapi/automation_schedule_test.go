package productapi

import (
	"testing"
	"time"
)

func TestAutomationScheduleTimes(t *testing.T) {
	tests := []struct {
		name     string
		schedule automationSchedule
		after    string
		want     string
	}{
		{"once", automationSchedule{Kind: "once", RunAt: parseAutomationTime(t, "2026-10-26T14:00:00Z")}, "2026-10-25T14:00:00Z", "2026-10-26T14:00:00Z"},
		{"interval anchored after missed runs", automationSchedule{Kind: "interval", RunAt: parseAutomationTime(t, "2026-10-25T10:00:00Z"), IntervalSeconds: 300}, "2026-10-25T10:16:00Z", "2026-10-25T10:20:00Z"},
		{"daily local time", automationSchedule{Kind: "daily", LocalTime: "09:00", Timezone: "America/Chicago"}, "2026-10-25T14:01:00Z", "2026-10-26T14:00:00Z"},
		{"weekly local time", automationSchedule{Kind: "weekly", LocalTime: "09:00", Timezone: "America/Chicago", Weekday: dayPtr(1)}, "2026-10-26T14:01:00Z", "2026-11-02T15:00:00Z"},
		{"spring DST gap is skipped", automationSchedule{Kind: "daily", LocalTime: "02:30", Timezone: "America/Chicago"}, "2026-03-08T07:00:00Z", "2026-03-09T07:30:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nextAutomationTime(tc.schedule, *parseAutomationTime(t, tc.after))
			if err != nil || got == nil || got.Format(time.RFC3339) != tc.want {
				t.Fatalf("next=%v err=%v, want %s", got, err, tc.want)
			}
		})
	}
}

func TestOneTimeAutomationDoesNotRepeat(t *testing.T) {
	at := parseAutomationTime(t, "2026-10-25T10:00:00Z")
	next, err := nextAutomationTime(automationSchedule{Kind: "once", RunAt: at}, *at)
	if err != nil || next != nil {
		t.Fatalf("next=%v err=%v", next, err)
	}
}

func parseAutomationTime(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return &parsed
}

func dayPtr(day int) *int { return &day }
