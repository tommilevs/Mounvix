package plugins

import (
	"testing"
	"time"
)

func TestParseScheduleBoundariesAndUTCMatching(t *testing.T) {
	tests := []struct {
		name string
		expr string
		at   time.Time
		want bool
	}{
		{"minute and hour lower boundary", "0 0 * * *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"minute and hour upper boundary", "59 23 * * *", time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), true},
		{"reject nonzero seconds", "0 0 * * *", time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), false},
		{"day-of-month match", "15 8 20 * *", time.Date(2026, 11, 20, 8, 15, 0, 0, time.UTC), true},
		{"day-of-week match", "15 8 * * 5", time.Date(2026, 11, 20, 8, 15, 0, 0, time.UTC), true},
		{"vixie OR day semantics", "15 8 1 * 5", time.Date(2026, 11, 20, 8, 15, 0, 0, time.UTC), true},
		{"must match all other fields", "15 8 * 11 4", time.Date(2026, 11, 20, 8, 15, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := ParseSchedule(tt.expr)
			if err != nil {
				t.Fatalf("ParseSchedule(%q): %v", tt.expr, err)
			}
			if got := schedule.Matches(tt.at); got != tt.want {
				t.Fatalf("Matches(%s) = %t, want %t", tt.at.Format(time.RFC3339), got, tt.want)
			}
		})
	}
}

func TestParseScheduleRejectsUnsupportedSyntaxAndRanges(t *testing.T) {
	for _, expr := range []string{
		"", "* * * *", "* * * * * extra", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 7",
		"*/5 * * * *", "1,2 * * * *", "1-5 * * * *", "1 2 3 4 5 trailing", "１ * * * *",
	} {
		t.Run(expr, func(t *testing.T) {
			if _, err := ParseSchedule(expr); err == nil {
				t.Fatalf("ParseSchedule(%q) unexpectedly succeeded", expr)
			}
		})
	}
}
