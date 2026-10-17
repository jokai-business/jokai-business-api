package notification

import (
	"testing"
	"time"
)

func TestReminderValidation(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		in    Input
		valid bool
	}{
		{"valid", Input{Title: " Follow up ", DueAt: now.Add(time.Hour)}, true},
		{"near-now", Input{Title: "Now", DueAt: now.Add(-30 * time.Second)}, true},
		{"old", Input{Title: "Old", DueAt: now.Add(-2 * time.Minute)}, false},
		{"too far", Input{Title: "Later", DueAt: now.Add(367 * 24 * time.Hour)}, false},
		{"blank", Input{Title: " ", DueAt: now}, false},
		{"missing date", Input{Title: "No date"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Validate(&tc.in, now)
			if (e == nil) != tc.valid {
				t.Fatalf("got %v", e)
			}
		})
	}
}
