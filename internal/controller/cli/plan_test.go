package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HamzaGbada/orca/internal/modules/gc"
)

func writePlanFile(t *testing.T, name string, cands ...gc.Candidate) string {
	t.Helper()
	p := gc.Plan{Format: gc.FormatVersion, GeneratedAt: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC), Candidates: cands}
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := p.WriteJSON(f); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPlanDiffIsOffline(t *testing.T) {
	old := writePlanFile(t, "old.json", gc.Candidate{ID: "image:a", Type: gc.Image, Name: "a:1", Class: gc.Safe, ReclaimableSize: 10})
	cur := writePlanFile(t, "new.json",
		gc.Candidate{ID: "image:a", Type: gc.Image, Name: "a:1", Class: gc.Review, ReclaimableSize: 10},
		gc.Candidate{ID: "volume:v", Type: gc.Volume, Name: "v", Class: gc.Safe, ReclaimableSize: 5})

	code, stdout, stderr := run(mustNotConnect(t), "plan", "diff", old, cur)
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"+ volume", "~ image", "SAFE → REVIEW"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("diff output missing %q:\n%s", want, stdout)
		}
	}

	// Flags may appear between the arguments.
	code, stdout, _ = run(mustNotConnect(t), "plan", "diff", old, "--json", cur)
	var d gc.Diff
	if code != exitOK || json.Unmarshal([]byte(stdout), &d) != nil || len(d.Added) != 1 || len(d.Changed) != 1 {
		t.Errorf("--json diff: exit %d, output %q", code, stdout)
	}
}

func TestPlanDiffErrors(t *testing.T) {
	old := writePlanFile(t, "old.json")
	if code, _, stderr := run(mustNotConnect(t), "plan", "diff", old); code != exitUsage || !strings.Contains(stderr, "expected <old.json> <new.json>") {
		t.Errorf("one argument: exit %d, stderr %q", code, stderr)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"format":"orca.plan/v9"}`), 0o644)
	if code, _, stderr := run(mustNotConnect(t), "plan", "diff", old, bad); code != exitError || !strings.Contains(stderr, "unsupported plan format") {
		t.Errorf("future format: exit %d, stderr %q", code, stderr)
	}
}

func TestGCRequiresDryRun(t *testing.T) {
	connect := func(context.Context, Options) (*Services, error) {
		return &Services{Close: func() error { return nil }}, nil // no GC service: planning would panic
	}
	code, _, stderr := run(connect, "gc")
	if code != exitUsage || !strings.Contains(stderr, "cannot delete anything") {
		t.Errorf("orca gc: exit %d, stderr %q", code, stderr)
	}
}

func TestExplainNeedsAResource(t *testing.T) {
	if code, _, stderr := run(mustNotConnect(t), "explain"); code != exitUsage || !strings.Contains(stderr, "expected <resource>") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if code, _, _ := run(mustNotConnect(t), "plan", "extra"); code != exitUsage {
		t.Errorf("plan with an argument: exit %d, want usage error", code)
	}
}
