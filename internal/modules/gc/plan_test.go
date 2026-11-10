package gc

import (
	"bytes"
	"strings"
	"testing"
)

func TestMatchTag(t *testing.T) {
	for _, tt := range []struct {
		pattern, ref string
		want         bool
	}{
		{"*:latest", "nginx:latest", true},
		{"*:latest", "quay.io/minio/mc:latest", true}, // * crosses "/"
		{"*:latest", "nginx:1.27", false},
		{"*:latest", "nginx:latest-alpine", false},
		{"postgres:1?", "postgres:16", true},
		{"postgres:1?", "postgres:9", false},
		{"registry.local/*", "registry.local/team/app:2", true},
		{"*", "anything:goes", true},
		{"app:*-prod", "app:2026-10-prod", true},
		{"app:*-prod", "app:2026-10-dev", false},
	} {
		if got := matchTag(tt.pattern, tt.ref); got != tt.want {
			t.Errorf("matchTag(%q, %q) = %t, want %t", tt.pattern, tt.ref, got, tt.want)
		}
	}
}

func TestScoring(t *testing.T) {
	f := newFixture().
		image("sha256:big", []string{"b"}, tags("big:1"), digest("b@sha256:1"), created(daysAgo(200)), sizes(5000, 0)).
		image("sha256:small", []string{"s"}, tags("small:1"), digest("s@sha256:1"), created(daysAgo(200)), sizes(50, 0)).
		image("sha256:young", []string{"y"}, tags("young:1"), digest("y@sha256:1"), created(daysAgo(10)), sizes(5000, 0)).
		image("sha256:active", []string{"a"}, tags("active:1"), digest("a@sha256:1"), created(daysAgo(200)),
			imgLabels("com.orca.project", "live"), sizes(5000, 0)).
		image("sha256:other", []string{"o"}, tags("other:1"), digest("o@sha256:1"), sizes(1, 0)).
		container("livec", "livec", "sha256:other", "running", now).labelContainer("livec", "com.orca.project", "live")

	a := analyze(t, f.build(), noReview())
	big, small, young, active := a.cand("image:sha256:big"), a.cand("image:sha256:small"), a.cand("image:sha256:young"), a.cand("image:sha256:active")
	if !(big.Score > small.Score) {
		t.Errorf("more reclaim must score higher: big %.1f, small %.1f", big.Score, small.Score)
	}
	if !(big.Score > young.Score) {
		t.Errorf("older must score higher: big %.1f, young %.1f", big.Score, young.Score)
	}
	if !(big.Score > active.Score) || active.ScoreBreakdown.Activity != 1 {
		t.Errorf("an active project must lower the score: big %.1f, active %.1f (%+v)", big.Score, active.Score, active.ScoreBreakdown)
	}
	// Breakdown for big: reclaim 1 (the largest), age 1 (200 days > 90).
	if b := big.ScoreBreakdown; b.Reclaim != 1 || b.Age != 1 || big.Score != 80 {
		t.Errorf("big breakdown %+v score %.1f, want reclaim 1, age 1, score 80", b, big.Score)
	}
	if big.Risk != 2 || big.Efficiency != 2500 {
		t.Errorf("pullable image risk/efficiency = %d/%.0f, want 2/2500", big.Risk, big.Efficiency)
	}

	// Weights change the order, never the class.
	p := noReview()
	p.Weights = Weights{Reclaim: 0, Age: 0, Activity: 5, Ambiguity: 5}
	b := analyze(t, f.build(), p)
	for _, c := range a.Plan.Candidates {
		if got := class(b, c.ID); got != c.Class {
			t.Errorf("%s: weights changed the class from %s to %s", c.ID, c.Class, got)
		}
	}
}

func TestExplainAndWhyNot(t *testing.T) {
	a := analyze(t, sharedBase().build(), noReview())

	// A kept layer: why is it not a candidate? Because the running web
	// container uses app, which contains it.
	id, err := a.Resolve(strings.TrimPrefix(layerID("base"), "layer:sha256:")[:12])
	if err != nil {
		t.Fatal(err)
	}
	e, err := a.Explain(id)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusKept || e.KeptBy == nil || e.KeptBy.Root.Category != RootRunning {
		t.Fatalf("base layer explanation = %+v", e)
	}
	var path []string
	for _, l := range e.KeptBy.Path {
		path = append(path, l.Name)
	}
	if got := strings.Join(path, " > "); got != "web > app:1 > "+shortID(strings.TrimPrefix(id, "layer:")) {
		t.Errorf("kept-by path = %q", got)
	}

	// A candidate, resolved by tag.
	id, err = a.Resolve("tool:1")
	if err != nil {
		t.Fatal(err)
	}
	e, _ = a.Explain(id)
	if e.Status != StatusCandidate || e.Candidate.Class != Safe || e.KeptBy != nil {
		t.Errorf("tool explanation = %+v", e)
	}

	// A layer only tool uses: freed with it.
	e, _ = a.Explain(layerID("base", "tool"))
	if e.Status != StatusFreedWith || !containsText(e.Notes, "freed when image tool:1 is removed") {
		t.Errorf("tool layer explanation = %+v", e)
	}

	// Resolution: container name, volume name, unknown, ambiguous.
	for ref, want := range map[string]string{"old": "container:old", "data": "volume:data", "/home/me/src": "bindmount:/home/me/src"} {
		if got, err := a.Resolve(ref); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %s", ref, got, err, want)
		}
	}
	if _, err := a.Resolve("nothing-here"); err == nil {
		t.Error("an unknown reference must fail")
	}
}

func TestResolveAmbiguous(t *testing.T) {
	f := newFixture().
		image("sha256:abc111", []string{"x"}, sizes(1, 0)).
		image("sha256:abc222", []string{"y"}, sizes(1, 0))
	a := analyze(t, f.build(), noReview())
	if _, err := a.Resolve("abc"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("err = %v, want ambiguity", err)
	}
	if id, err := a.Resolve("abc2"); err != nil || id != "image:sha256:abc222" {
		t.Errorf("Resolve(abc2) = %s, %v", id, err)
	}
}

func TestPlanRoundTripAndFormat(t *testing.T) {
	a := analyze(t, sharedBase().build(), DefaultPolicy())
	var buf bytes.Buffer
	if err := a.Plan.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"format": "orca.plan/v1"`) || !strings.Contains(buf.String(), `"reclaimable_size"`) {
		t.Fatalf("unexpected plan JSON:\n%s", buf.String()[:300])
	}
	p, err := ReadPlan(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if d := DiffPlans(a.Plan, p); !d.Empty() || !d.SameSnapshot || !d.SamePolicy {
		t.Errorf("a plan must equal itself after a round trip: %+v", d)
	}

	for _, bad := range []string{`{"format":"orca.plan/v9"}`, `{"format":"something"}`, `not json`} {
		if _, err := ReadPlan(strings.NewReader(bad)); err == nil {
			t.Errorf("ReadPlan(%s) should fail", bad)
		}
	}
}

func TestDiffPlans(t *testing.T) {
	oldPlan := analyze(t, sharedBase().build(), noReview()).Plan

	// Later: the old container is gone, a new dangling image appeared and
	// the tool image became REVIEW (the operator now reviews large ones).
	f := sharedBase().image("sha256:new", []string{"n"}, sizes(10, 0))
	f.containers = f.containers[:1]
	p := noReview()
	p.ReviewLarge = 1
	newPlan := analyze(t, f.build(), p).Plan

	d := DiffPlans(oldPlan, newPlan)
	if d.SameSnapshot || d.SamePolicy {
		t.Errorf("both snapshot and policy changed: %+v", d)
	}
	if len(d.Added) != 1 || d.Added[0].ID != "image:sha256:new" {
		t.Errorf("added = %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].ID != "container:old" {
		t.Errorf("removed = %+v", d.Removed)
	}
	if len(d.Changed) != 1 || d.Changed[0].ID != "image:sha256:tool" || d.Changed[0].OldClass != Safe || d.Changed[0].NewClass != Review {
		t.Errorf("changed = %+v", d.Changed)
	}
}
