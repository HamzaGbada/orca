// Package duration parses and formats the human durations used in Orca's
// configuration and labels, such as "30d" or "12h".
package duration

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	Day  = 24 * time.Hour
	Week = 7 * Day
)

// Parse accepts "<n>d" (days), "<n>w" (weeks), "0", or any Go duration
// ("36h", "90m", "1h30m"). Negative durations are rejected.
func Parse(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	var d time.Duration
	var err error
	switch {
	case s == "0":
		return 0, nil
	case strings.HasSuffix(s, "d"), strings.HasSuffix(s, "w"):
		unit := Day
		if strings.HasSuffix(s, "w") {
			unit = Week
		}
		var n float64
		n, err = strconv.ParseFloat(s[:len(s)-1], 64)
		d = time.Duration(n * float64(unit))
	default:
		d, err = time.ParseDuration(s)
	}
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 30d, 2w, 12h)", s)
	}
	return d, nil
}

// Format renders d compactly: "30d", "36h", "90m".
func Format(d time.Duration) string {
	switch {
	case d == 0:
		return "0"
	case d%Day == 0:
		return fmt.Sprintf("%dd", d/Day)
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

// Ago renders the time elapsed since t as a short phrase: "3 days ago",
// "5 hours ago", "just now". It is used in plan reasons, so it depends only
// on its inputs.
func Ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute") + " ago"
	case d < Day:
		return plural(int(d/time.Hour), "hour") + " ago"
	default:
		return plural(int(d/Day), "day") + " ago"
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
