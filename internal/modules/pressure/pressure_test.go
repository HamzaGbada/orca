package pressure

import (
	"testing"

	"github.com/HamzaGbada/orca/internal/extensions/fsmeasure"
)

func TestLevel(t *testing.T) {
	th := DefaultThresholds
	tests := map[float64]Level{
		0: Normal, 69.99: Normal, 70: Warning, 84.9: Warning,
		85: Critical, 94.99: Critical, 95: Emergency, 100: Emergency,
	}
	for used, want := range tests {
		if got := th.Level(used); got != want {
			t.Errorf("Level(%v) = %s, want %s", used, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := DefaultThresholds.Validate(); err != nil {
		t.Errorf("defaults invalid: %v", err)
	}
	for _, bad := range []Thresholds{
		{Warning: 80, Critical: 70, Emergency: 95},
		{Warning: 70, Critical: 85, Emergency: 101},
		{Warning: 0, Critical: 85, Emergency: 95},
		{Warning: 70, Critical: 70, Emergency: 95},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v should be invalid", bad)
		}
	}
}

func TestEvaluateUsesDfFormula(t *testing.T) {
	// 100 total, 10 reserved for root, 72 used, 18 available to users:
	// df reports 72 / (72 + 18) = 80%, not 72 / 100.
	st := Evaluate(fsmeasure.Capacity{TotalBytes: 100, UsedBytes: 72, AvailBytes: 18}, DefaultThresholds)
	if st.UsedPercent != 80 || st.Level != Warning {
		t.Errorf("got %v%% %s, want 80%% WARNING", st.UsedPercent, st.Level)
	}
}
