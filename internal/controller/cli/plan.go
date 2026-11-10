package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/HamzaGbada/orca/internal/modules/gc"
)

func planCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	details := fs.Bool("details", false, "list every candidate with its score and main reason")
	return func(ctx context.Context, e *env) error {
		return runPlan(ctx, e, *details)
	}
}

// gcCmd is the future home of garbage collection. This version can only
// plan, so it requires --dry-run and never deletes anything.
func gcCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	dryRun := fs.Bool("dry-run", false, "show the GC plan without changing anything (same as orca plan)")
	details := fs.Bool("details", false, "list every candidate with its score and main reason")
	return func(ctx context.Context, e *env) error {
		if !*dryRun {
			return usageError{"this version of orca cannot delete anything: use orca gc --dry-run (or orca plan)"}
		}
		return runPlan(ctx, e, *details)
	}
}

func runPlan(ctx context.Context, e *env, details bool) error {
	a, err := e.svc.GC.Analyze(ctx)
	if err != nil {
		return err
	}
	p := a.Plan
	printWarnings(e, p.Warnings)
	if e.json {
		return p.WriteJSON(e.stdout)
	}
	var b strings.Builder
	writePlan(&b, p)
	if details {
		writePlanDetails(&b, p)
	}
	_, err = io.WriteString(e.stdout, b.String())
	return err
}

var typeOrder = []gc.Type{gc.Image, gc.Container, gc.Volume, gc.BuildCache, gc.BindMount}

var typeNames = map[gc.Type][2]string{
	gc.Image:      {"image", "images"},
	gc.Container:  {"container", "containers"},
	gc.Volume:     {"volume", "volumes"},
	gc.BuildCache: {"build cache entry", "build cache entries"},
	gc.BindMount:  {"bind mount", "bind mounts"},
	gc.Layer:      {"layer", "layers"},
}

func typeCount(t gc.Type, n int) string {
	name := typeNames[t]
	if n == 1 {
		return fmt.Sprintf("%d %s", n, name[0])
	}
	return fmt.Sprintf("%d %s", n, name[1])
}

// group is a report line: candidates of one type and summary.
type group struct {
	typ     gc.Type
	summary string
	count   int
	bytes   int64
}

func groups(p gc.Plan, class gc.Class, bySummary bool) []group {
	idx := map[string]*group{}
	var out []*group
	for _, c := range p.Candidates {
		if c.Class != class {
			continue
		}
		key := string(c.Type)
		if bySummary {
			key += "|" + c.Summary
		}
		g := idx[key]
		if g == nil {
			g = &group{typ: c.Type}
			if bySummary {
				g.summary = c.Summary
			}
			idx[key] = g
			out = append(out, g)
		}
		g.count++
		g.bytes += c.ReclaimableSize
	}
	rank := map[gc.Type]int{}
	for i, t := range typeOrder {
		rank[t] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].typ != out[j].typ {
			return rank[out[i].typ] < rank[out[j].typ]
		}
		return out[i].count > out[j].count
	})
	res := make([]group, len(out))
	for i, g := range out {
		res[i] = *g
	}
	return res
}

func atLeast(b int64, exact bool) string {
	if exact {
		return size(b)
	}
	return "at least " + size(b)
}

func writePlan(w io.Writer, p gc.Plan) {
	fmt.Fprintf(w, "ORCA GC PLAN (dry-run)\n%s\n", rule)
	fmt.Fprintf(w, "Docker %s at %s · snapshot %s · policy %s\n", p.Daemon.Version, p.Daemon.Address, p.Inventory.Snapshot, p.Policy.Version)

	fmt.Fprintf(w, "\nSAFE to reclaim (would run without confirmation):\n")
	safe := groups(p, gc.Safe, false)
	if len(safe) == 0 {
		fmt.Fprintf(w, "  nothing\n")
	}
	for _, g := range safe {
		fmt.Fprintf(w, "  %-30s %s\n", typeCount(g.typ, g.count), size(g.bytes))
	}
	if p.Totals.SharedLayersSafe > 0 {
		fmt.Fprintf(w, "  %-30s %s\n", "+ layers shared between them", size(p.Totals.SharedLayersSafe))
	}

	fmt.Fprintf(w, "\nREVIEW (would require --include-review or confirmation):\n")
	review := groups(p, gc.Review, true)
	if len(review) == 0 {
		fmt.Fprintf(w, "  nothing\n")
	}
	for _, g := range review {
		fmt.Fprintf(w, "  %-30s %-10s — %s\n", typeCount(g.typ, g.count), size(g.bytes), g.summary)
	}

	fmt.Fprintf(w, "\nPROTECTED (will never be touched by this plan):\n")
	protected := groups(p, gc.Protected, true)
	if len(protected) == 0 {
		fmt.Fprintf(w, "  nothing\n")
	}
	for _, g := range protected {
		fmt.Fprintf(w, "  %-30s (%s)\n", typeCount(g.typ, g.count), g.summary)
	}

	if external := groups(p, gc.External, true); len(external) > 0 {
		fmt.Fprintf(w, "\nEXTERNAL (not Docker-managed, never touched):\n")
		for _, g := range external {
			fmt.Fprintf(w, "  %-30s (%s)\n", typeCount(g.typ, g.count), g.summary)
		}
	}

	if len(p.Kept) > 0 {
		var parts []string
		for _, k := range p.Kept {
			parts = append(parts, typeCount(k.Type, k.Count))
		}
		fmt.Fprintf(w, "\nKEPT (in use, or needed by what is kept):\n  %s\n", strings.Join(parts, ", "))
	}

	fmt.Fprintf(w, "\nProjected total reclaim (SAFE only): %s\n", atLeast(p.Totals.Safe, p.Totals.SafeExact))
	fmt.Fprintf(w, "Projected total reclaim (SAFE + REVIEW): %s\n", atLeast(p.Totals.SafeAndReview, p.Totals.SafeAndReviewExact))
	if !p.Totals.SafeExact || !p.Totals.SafeAndReviewExact {
		fmt.Fprintf(w, "(\"at least\": some sizes need root to measure; run with sudo for exact figures)\n")
	}
	fmt.Fprintf(w, "\nNothing was changed. Details: orca plan --details · one resource: orca explain <name>\n")
}

func writePlanDetails(w io.Writer, p gc.Plan) {
	fmt.Fprintf(w, "\n%-9s %-10s %-44s %-12s %6s  %s\n", "CLASS", "TYPE", "NAME", "RECLAIM", "SCORE", "WHY")
	for _, c := range p.Candidates {
		score := "-"
		if c.Class == gc.Safe || c.Class == gc.Review {
			score = fmt.Sprintf("%.1f", c.Score)
		}
		reclaim := sizeOrDash(c.ReclaimableSize)
		if c.Class == gc.Protected || c.Class == gc.External {
			reclaim = "-"
		} else if !c.ReclaimExact {
			reclaim = "≥" + reclaim
		}
		fmt.Fprintf(w, "%-9s %-10s %-44s %-12s %6s  %s\n", c.Class, c.Type, truncate(c.Name, 44), reclaim, score, c.Summary)
	}
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func planDiffCmd(_ *flag.FlagSet) func(context.Context, *env) error {
	return func(_ context.Context, e *env) error {
		old, err := readPlanFile(e.args[0])
		if err != nil {
			return err
		}
		cur, err := readPlanFile(e.args[1])
		if err != nil {
			return err
		}
		d := gc.DiffPlans(old, cur)
		if e.json {
			return writeJSON(e.stdout, d)
		}
		var b strings.Builder
		writeDiff(&b, d)
		_, err = io.WriteString(e.stdout, b.String())
		return err
	}
}

func readPlanFile(path string) (gc.Plan, error) {
	f, err := os.Open(path)
	if err != nil {
		return gc.Plan{}, err
	}
	defer f.Close()
	p, err := gc.ReadPlan(f)
	if err != nil {
		return gc.Plan{}, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

func writeDiff(w io.Writer, d gc.Diff) {
	fmt.Fprintf(w, "PLAN DIFF\n")
	fmt.Fprintf(w, "  old: %s  snapshot %s  policy %s\n", d.Old.GeneratedAt.Format("2006-01-02 15:04"), d.Old.Snapshot, d.Old.PolicyVersion)
	fmt.Fprintf(w, "  new: %s  snapshot %s  policy %s\n", d.New.GeneratedAt.Format("2006-01-02 15:04"), d.New.Snapshot, d.New.PolicyVersion)
	switch {
	case d.SameSnapshot && d.SamePolicy:
		fmt.Fprintf(w, "  same Docker state and same policy\n")
	case d.SameSnapshot:
		fmt.Fprintf(w, "  same Docker state, different policy: differences come from the policy\n")
	case d.SamePolicy:
		fmt.Fprintf(w, "  same policy, different Docker state\n")
	}
	fmt.Fprintln(w)
	if d.Empty() {
		fmt.Fprintf(w, "No changes.\n")
	}
	for _, c := range d.Added {
		fmt.Fprintf(w, "+ %-10s %-44s %-9s %s\n", c.Type, truncate(c.Name, 44), c.Class, size(c.ReclaimableSize))
	}
	for _, c := range d.Removed {
		fmt.Fprintf(w, "- %-10s %-44s %-9s %s\n", c.Type, truncate(c.Name, 44), c.Class, size(c.ReclaimableSize))
	}
	for _, c := range d.Changed {
		line := fmt.Sprintf("~ %-10s %-44s %s → %s", c.Type, truncate(c.Name, 44), c.OldClass, c.NewClass)
		if c.OldClass == c.NewClass {
			line = fmt.Sprintf("~ %-10s %-44s %s", c.Type, truncate(c.Name, 44), c.NewClass)
		}
		if c.OldReclaim != c.NewReclaim {
			line += fmt.Sprintf("  (reclaim %s → %s)", size(c.OldReclaim), size(c.NewReclaim))
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "\nSAFE total:          %s → %s\n", atLeast(d.Old.Totals.Safe, d.Old.Totals.SafeExact), atLeast(d.New.Totals.Safe, d.New.Totals.SafeExact))
	fmt.Fprintf(w, "SAFE + REVIEW total: %s → %s\n", atLeast(d.Old.Totals.SafeAndReview, d.Old.Totals.SafeAndReviewExact),
		atLeast(d.New.Totals.SafeAndReview, d.New.Totals.SafeAndReviewExact))
}

func explainCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	whyNot := fs.Bool("why-not", false, "only say why the resource is not a GC candidate")
	return func(ctx context.Context, e *env) error {
		a, err := e.svc.GC.Analyze(ctx)
		if err != nil {
			return err
		}
		id, err := a.Resolve(e.args[0])
		if err != nil {
			return err
		}
		x, err := a.Explain(id)
		if err != nil {
			return err
		}
		if e.json {
			return writeJSON(e.stdout, x)
		}
		var b strings.Builder
		writeExplanation(&b, x, *whyNot)
		_, err = io.WriteString(e.stdout, b.String())
		return err
	}
}

func writeExplanation(w io.Writer, x gc.Explanation, whyNot bool) {
	fmt.Fprintf(w, "%s %s\n", x.Type, x.Name)
	if x.ResourceID != x.Name {
		fmt.Fprintf(w, "  id: %s\n", x.ResourceID)
	}
	if x.Size >= 0 {
		fmt.Fprintf(w, "  size: %s\n", size(x.Size))
	}

	c := x.Candidate
	switch x.Status {
	case gc.StatusCandidate:
		fmt.Fprintf(w, "\n%s candidate: removing it would free %s\n", c.Class, atLeast(c.ReclaimableSize, c.ReclaimExact))
		if whyNot {
			fmt.Fprintf(w, "It IS a candidate, because:\n")
		}
		bullets(w, c.Reasons)
	case gc.StatusProtected:
		fmt.Fprintf(w, "\nPROTECTED: never a candidate, because:\n")
		bullets(w, c.Reasons)
	case gc.StatusExternal:
		fmt.Fprintf(w, "\nEXTERNAL: not Docker-managed, never a candidate:\n")
		bullets(w, c.Reasons)
	case gc.StatusKept:
		fmt.Fprintf(w, "\nKEPT: not a candidate, because it is reachable from a GC root:\n")
	case gc.StatusFreedWith:
		fmt.Fprintf(w, "\nNot a separate candidate:\n")
		bullets(w, x.Notes)
	}
	// The reachability path matters when the resource is kept through
	// another one; a root on its own was explained above.
	if x.KeptBy != nil && x.Status != gc.StatusCandidate && (len(x.KeptBy.Path) > 1 || x.Status == gc.StatusKept) {
		kind := "policy"
		if x.KeptBy.Root.Absolute {
			kind = "absolute"
		}
		fmt.Fprintf(w, "  root: %s %s (%s root: %s)\n", x.KeptBy.Path[0].Type, x.KeptBy.Path[0].Name, kind, x.KeptBy.Root.Reason)
		var chain []string
		for i, l := range x.KeptBy.Path {
			step := string(l.Type) + " " + l.Name
			if i > 0 {
				step = "─" + l.Edge + "→ " + step
			}
			chain = append(chain, step)
		}
		fmt.Fprintf(w, "  path: %s\n", strings.Join(chain, " "))
	}
	if whyNot {
		return
	}

	if c != nil && c.ScoreBreakdown != nil {
		b := c.ScoreBreakdown
		fmt.Fprintf(w, "\nScore %.1f (reclaim %.3f, age %.3f, project activity %.3f, ambiguity %.0f) · risk %d\n",
			c.Score, b.Reclaim, b.Age, b.Activity, b.Ambiguity, c.Risk)
	}
	if c != nil && len(c.RemoveFirst) > 0 {
		fmt.Fprintf(w, "Must be removed after: %s\n", strings.Join(c.RemoveFirst, ", "))
	}
	writeLinks(w, "Depends on", x.DependsOn)
	writeLinks(w, "Used by", x.UsedBy)
}

func bullets(w io.Writer, lines []string) {
	for _, l := range lines {
		fmt.Fprintf(w, "  - %s\n", l)
	}
}

func writeLinks(w io.Writer, title string, links []gc.Link) {
	if len(links) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s (%d):\n", title, len(links))
	for i, l := range links {
		if i == 12 {
			fmt.Fprintf(w, "  … %d more (orca explain --json)\n", len(links)-12)
			break
		}
		fmt.Fprintf(w, "  %-9s %-10s %-44s %s\n", l.Edge, l.Type, truncate(l.Name, 44), l.Status)
	}
}
