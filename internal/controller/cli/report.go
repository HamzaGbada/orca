package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/HamzaGbada/orca/internal/modules/graph"
	"github.com/HamzaGbada/orca/internal/modules/report"
)

func inventoryCmd(_ *flag.FlagSet) func(context.Context, *env) error {
	return func(ctx context.Context, e *env) error {
		inv, err := e.svc.Inventory.Collect(ctx)
		if err != nil {
			return err
		}
		printWarnings(e, inv.Warnings)
		return writeJSON(e.stdout, inv)
	}
}

func graphCmd(_ *flag.FlagSet) func(context.Context, *env) error {
	return func(ctx context.Context, e *env) error {
		inv, err := e.svc.Inventory.Collect(ctx)
		if err != nil {
			return err
		}
		g, err := graph.FromInventory(inv)
		if err != nil {
			return err
		}
		printWarnings(e, inv.Warnings)
		if e.json {
			return g.WriteJSON(e.stdout)
		}
		return g.WriteDOT(e.stdout)
	}
}

func reportCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	summary := fs.Bool("summary", false, "print the short summary instead of the full explorer view")

	return func(ctx context.Context, e *env) error {
		inv, err := e.svc.Inventory.Collect(ctx)
		if err != nil {
			return err
		}
		r := report.Summarize(inv)
		printWarnings(e, r.Warnings)
		if e.json {
			return writeJSON(e.stdout, r)
		}

		var b strings.Builder
		if *summary {
			writeSummary(&b, r)
		} else {
			writeExplorer(&b, r, e.svc.ConfigSource)
		}
		_, err = io.WriteString(e.stdout, b.String())
		return err
	}
}

func printWarnings(e *env, ws []string) {
	for _, w := range ws {
		fmt.Fprintf(e.stderr, "orca: warning: %s\n", w)
	}
}

const rule = "──────────────────────────────────────────────"

func writeExplorer(w io.Writer, r report.Report, configSource string) {
	fmt.Fprintf(w, "ORCA\nDocker Storage Explorer\n%s\n", rule)

	section(w, "HOST")
	row(w, "Docker", "%s (API %s)", r.Host.ServerVersion, r.Host.APIVersion)
	row(w, "Driver", "%s", r.Host.StorageDriver)
	row(w, "Data root", "%s", r.Host.DataRoot)
	row(w, "Disk usage", "%s", diskLine(r))
	row(w, "Config", "%s", orDefault(configSource, "built-in defaults"))

	section(w, "IMAGES")
	row(w, "Total", "%d (%d tagged, %d dangling, %d intermediate)",
		r.Images.Total, r.Images.Tagged, r.Images.Dangling, r.Images.Intermediate)
	row(w, "Used", "%d", r.Images.Used)
	row(w, "Size", "%s", size(r.Images.Size))
	row(w, "Reclaimable", "at least %s", size(r.Images.ReclaimableAtLeast))

	section(w, "LAYERS")
	row(w, "Total", "%d", r.Layers.Total)
	row(w, "Shared", "%d", r.Layers.Shared)
	row(w, "Unique", "%d", r.Layers.Unique)

	section(w, "CONTAINERS")
	row(w, "Running", "%d", r.Containers.Running)
	row(w, "Stopped", "%d", r.Containers.Stopped)
	row(w, "Writable", "%s", size(r.Containers.Writable))
	row(w, "Reclaimable", "%s", size(r.Containers.Reclaimable))

	section(w, "VOLUMES")
	row(w, "Total", "%d", r.Volumes.Total)
	row(w, "Referenced", "%d", r.Volumes.Referenced)
	row(w, "Protected", "%d", r.Volumes.Protected)
	row(w, "Candidates", "%d", r.Volumes.Candidate)
	row(w, "Needs review", "%d (unused named volumes)", r.Volumes.Unreferenced)
	row(w, "Size", "%s", size(r.Volumes.Size))
	row(w, "Reclaimable", "%s (+%s needs review)", size(r.Volumes.CandidateSize), size(r.Volumes.ReviewSize))

	section(w, "BUILD CACHE")
	row(w, "Entries", "%d", r.BuildCache.Entries)
	row(w, "Size", "%s", size(r.BuildCache.Size))
	row(w, "Reclaimable", "%s", size(r.BuildCache.Reclaimable))

	section(w, "LOGS")
	row(w, "Total", "%s", logsLine(r.Logs))
	row(w, "Unlimited growth", "%d of %d containers", r.Logs.Unlimited, r.Logs.Count)
	row(w, "Large", "%d", r.Logs.Large)

	section(w, "POTENTIAL RECOVERY")
	writeRecovery(w, r.Recovery)
}

func writeSummary(w io.Writer, r report.Report) {
	fmt.Fprintf(w, "ORCA STORAGE REPORT\n%s\n", rule[:25*len("─")])

	section(w, "Filesystem")
	if d := r.Disk; d != nil {
		row(w, "Used", "%s / %s", size(d.UsedBytes), size(d.TotalBytes))
		row(w, "Usage", "%.1f%%", d.UsedPercent)
		row(w, "Pressure", "%s", d.Level)
	} else {
		row(w, "Usage", "not measured")
	}

	section(w, "Docker")
	row(w, "Images", "%s", size(r.Images.Size))
	row(w, "Containers", "%s", size(r.Containers.Writable))
	row(w, "Volumes", "%s", size(r.Volumes.Size))
	row(w, "Cache", "%s", size(r.BuildCache.Size))
	row(w, "Logs", "%s", logsLine(r.Logs))

	section(w, "Potential reclaim")
	writeRecovery(w, r.Recovery)
}

func writeRecovery(w io.Writer, rec report.Recovery) {
	row(w, "Images", "at least %s", size(rec.Images))
	row(w, "Containers", "%s", size(rec.Containers))
	row(w, "Volumes", "%s", size(rec.Volumes))
	row(w, "Build cache", "%s", size(rec.BuildCache))
	fmt.Fprintln(w)
	row(w, "TOTAL", "at least %s", size(rec.Total))
}

func section(w io.Writer, title string) {
	fmt.Fprintf(w, "\n%s\n", title)
}

func row(w io.Writer, label, format string, args ...any) {
	fmt.Fprintf(w, "  %-18s"+format+"\n", append([]any{label + ":"}, args...)...)
}

func diskLine(r report.Report) string {
	d := r.Disk
	if d == nil {
		return "not measured"
	}
	return fmt.Sprintf("%.1f%% of %s (%s free), pressure %s",
		d.UsedPercent, size(d.TotalBytes), size(d.AvailBytes), d.Level)
}

func logsLine(l report.Logs) string {
	switch {
	case l.Count == 0:
		return size(0)
	case l.Measured == 0:
		return "not measured (run as root)"
	case l.Measured < l.Count:
		return fmt.Sprintf("%s (%d of %d measured)", size(l.Total), l.Measured, l.Count)
	default:
		return size(l.Total)
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
