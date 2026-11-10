package duration

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	ok := map[string]time.Duration{
		"0": 0, "30d": 30 * Day, "1.5d": 36 * time.Hour, "2w": 14 * Day,
		"12h": 12 * time.Hour, "90m": 90 * time.Minute, " 7d ": 7 * Day,
	}
	for in, want := range ok {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "-1d", "d", "5x", "-3h"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestFormatAndAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0", 7 * Day: "7d", 36 * time.Hour: "36h", 90 * time.Minute: "90m"} {
		if got := Format(d); got != want {
			t.Errorf("Format(%v) = %s, want %s", d, got, want)
		}
	}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{30 * time.Second: "just now", time.Minute: "1 minute ago",
		5 * time.Hour: "5 hours ago", 47 * Day: "47 days ago", Day: "1 day ago"} {
		if got := Ago(now, now.Add(-d)); got != want {
			t.Errorf("Ago(-%v) = %s, want %s", d, got, want)
		}
	}
}
