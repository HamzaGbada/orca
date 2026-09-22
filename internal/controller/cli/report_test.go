package cli

import (
	"strings"
	"testing"

	"orca/internal/extensions/fsmeasure"
	"orca/internal/modules/pressure"
	"orca/internal/modules/report"
)

func TestReportRendering(t *testing.T) {
	r := report.Report{
		Disk: &pressure.Status{
			Capacity:    fsmeasure.Capacity{TotalBytes: 300e9, UsedBytes: 213e9, AvailBytes: 87e9},
			UsedPercent: 71.0, Level: pressure.Warning,
		},
		Images:   report.Images{Total: 3, ReclaimableAtLeast: 2e9},
		Logs:     report.Logs{Count: 2},
		Recovery: report.Recovery{Images: 2e9, Total: 2e9},
	}

	var full, summary strings.Builder
	writeExplorer(&full, r, "")
	writeSummary(&summary, r)

	for _, want := range []string{
		"Docker Storage Explorer", "Disk usage:       71.0% of 300GB (87GB free), pressure WARNING",
		"Config:           built-in defaults", "Reclaimable:      at least 2GB",
		"Total:            not measured (run as root)", "TOTAL:            at least 2GB",
	} {
		if !strings.Contains(full.String(), want) {
			t.Errorf("explorer view missing %q:\n%s", want, full.String())
		}
	}
	for _, want := range []string{"ORCA STORAGE REPORT", "Pressure:         WARNING", "Potential reclaim"} {
		if !strings.Contains(summary.String(), want) {
			t.Errorf("summary view missing %q:\n%s", want, summary.String())
		}
	}
	for _, out := range []string{full.String(), summary.String()} {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == "" && line != "" {
				t.Errorf("whitespace-only line in output: %q", line)
			}
		}
	}

	r.Disk = nil
	full.Reset()
	writeExplorer(&full, r, "/etc/orca.yaml")
	if !strings.Contains(full.String(), "Disk usage:       not measured") || !strings.Contains(full.String(), "/etc/orca.yaml") {
		t.Errorf("unmeasured disk / config source not rendered:\n%s", full.String())
	}
}
