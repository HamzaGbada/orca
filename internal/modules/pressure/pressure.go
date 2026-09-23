// Package pressure classifies disk usage of the filesystem holding Docker's
// data root against configurable thresholds.
package pressure

import (
	"fmt"

	"github.com/HamzaGbada/orca/internal/extensions/fsmeasure"
)

// Level is the disk pressure level.
type Level string

const (
	Normal    Level = "NORMAL"
	Warning   Level = "WARNING"
	Critical  Level = "CRITICAL"
	Emergency Level = "EMERGENCY"
)

// Thresholds are used-space percentages at which each level starts.
type Thresholds struct {
	Warning   float64
	Critical  float64
	Emergency float64
}

// DefaultThresholds are used when the configuration sets none.
var DefaultThresholds = Thresholds{Warning: 70, Critical: 85, Emergency: 95}

// Validate checks 0 < Warning < Critical < Emergency <= 100.
func (t Thresholds) Validate() error {
	if !(0 < t.Warning && t.Warning < t.Critical && t.Critical < t.Emergency && t.Emergency <= 100) {
		return fmt.Errorf("disk thresholds must satisfy 0 < warning < critical < emergency <= 100, got %g/%g/%g",
			t.Warning, t.Critical, t.Emergency)
	}
	return nil
}

// Level returns the pressure level for a used percentage.
func (t Thresholds) Level(usedPercent float64) Level {
	switch {
	case usedPercent >= t.Emergency:
		return Emergency
	case usedPercent >= t.Critical:
		return Critical
	case usedPercent >= t.Warning:
		return Warning
	default:
		return Normal
	}
}

// Status is the disk pressure of the filesystem holding a path.
type Status struct {
	fsmeasure.Capacity
	UsedPercent float64
	Level       Level
}

// Evaluate classifies a filesystem capacity.
func Evaluate(c fsmeasure.Capacity, t Thresholds) Status {
	used := c.UsedPercent()
	return Status{Capacity: c, UsedPercent: used, Level: t.Level(used)}
}
