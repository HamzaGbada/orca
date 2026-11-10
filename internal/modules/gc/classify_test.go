package gc

import (
	"bytes"
	"math/rand"
	"testing"
	"time"
)

func class(a *Analysis, id string) Class {
	if c := a.cand(id); c != nil {
		return c.Class
	}
	return ""
}

func TestAbsoluteRoots(t *testing.T) {
	f := newFixture().
		image("sha256:keep", []string{"k"}, tags("keep:1"), digest("k@sha256:1"), imgLabels("orca.gc.protected", "true"), sizes(10, 0)).
		image("sha256:typo", []string{"t"}, tags("typo:1"), digest("t@sha256:1"), imgLabels("orca.gc.protected", "yes"), sizes(10, 0)).
		image("sha256:off", []string{"o"}, tags("off:1"), digest("o@sha256:1"), imgLabels("orca.gc.protected", "false"), sizes(10, 0)).
		volume("pg_production").
		volume("db").
		container("dbc", "dbc", "sha256:off", "exited", daysAgo(40), volMount("db", "/var/lib/mysql")).
		container("paused", "paused", "sha256:off", "paused", now)

	a := analyze(t, f.build(), noReview())
	for id, want := range map[string]Class{
		"image:sha256:keep":    Protected, // orca.gc.protected=true
		"image:sha256:typo":    Protected, // a typo still protects (fail safe)
		"volume:pg_production": Protected, // protected name pattern
		"volume:db":            Protected, // mounted at a protected path
	} {
		if got := class(a, id); got != want {
			t.Errorf("%s: class %q, want %s", id, got, want)
		}
	}
	// "false" does not protect, but the paused container is live: it keeps
	// the image (in use, not protected).
	if c := a.cand("image:sha256:off"); c != nil || !a.mark.marked("image:sha256:off") {
		t.Errorf("image used by a paused container must be kept, got %+v", c)
	}
	if c := a.cand("container:paused"); c != nil {
		t.Errorf("a paused container is live, never a candidate: %+v", c)
	}
}

func TestProtectedIsNeverSafe(t *testing.T) {
	// Unused, huge, ancient, untagged: everything says "delete", but the
	// label protects it.
	f := newFixture().image("sha256:p", []string{"p"}, imgLabels("orca.gc.protected", "true"), created(daysAgo(900)), sizes(1<<40, 0))
	a := analyze(t, f.build(), DefaultPolicy())
	c := a.cand("image:sha256:p")
	if c == nil || c.Class != Protected || c.ReclaimableSize != 0 || c.Score != 0 {
		t.Fatalf("protected image = %+v, want PROTECTED with no reclaim and no score", c)
	}
}

func TestPolicyRootsAndOperatorOverrides(t *testing.T) {
	build := func() *fixture {
		return newFixture().
			image("sha256:latest", []string{"l"}, tags("quay.io/team/app:latest"), digest("l@sha256:1"), sizes(10, 0)).
			image("sha256:pinned", []string{"p"}, tags("app:1.2"), digest("p@sha256:2"), sizes(10, 0)).
			container("recent", "recent", "sha256:pinned", "exited", daysAgo(2)).
			buildCache("child", 5, daysAgo(1), "parent").
			buildCache("parent", 7, daysAgo(30)).
			buildCache("stale", 9, daysAgo(30)).
			image("sha256:proj", []string{"q"}, tags("proj:1"), digest("q@sha256:3"), imgLabels("com.orca.project", "med", "com.orca.retention", "30d"), sizes(10, 0)).
			container("projc", "projc", "sha256:proj", "exited", daysAgo(4)).
			labelContainer("projc", "com.orca.project", "med")
	}

	a := analyze(t, build().build(), noReview())
	for id, want := range map[string]Class{
		"image:sha256:latest": Protected, // *:latest matches across registry paths
		"container:recent":    Protected, // stopped 2 days ago, 7d retention
		"buildcache:child":    Protected, // used yesterday, 7d retention
		"buildcache:stale":    Safe,
		"image:sha256:proj":   Protected, // project med: retention 30d, activity 4 days ago
		"container:projc":     Protected,
	} {
		if got := class(a, id); got != want {
			t.Errorf("%s: class %q, want %s", id, got, want)
		}
	}
	// The kept cache record keeps its parent; the retained container keeps
	// its image.
	if !a.mark.marked("buildcache:parent") || !a.mark.marked("image:sha256:pinned") {
		t.Error("dependencies of policy roots must be kept")
	}
	if c := a.cand("image:sha256:proj"); !containsText(c.Reasons, "project med has com.orca.retention=30d, last activity 4 days ago") {
		t.Errorf("project retention reason missing: %v", c.Reasons)
	}

	// The operator disables every policy root category.
	p := noReview()
	p.ProtectedTags, p.StoppedContainerRetention, p.BuildCacheRetention, p.ProjectRetention = nil, 0, 0, false
	a = analyze(t, build().build(), p)
	for _, id := range []string{"image:sha256:latest", "container:recent", "buildcache:child", "buildcache:parent", "image:sha256:proj", "container:projc"} {
		if got := class(a, id); got != Safe {
			t.Errorf("with policy roots disabled, %s: class %q, want SAFE", id, got)
		}
	}
}

func TestProjectRetentionExpiredAndFailSafe(t *testing.T) {
	f := newFixture().
		image("sha256:old", []string{"o"}, tags("old:1"), digest("o@sha256:1"), imgLabels("com.orca.project", "legacy", "com.orca.retention", "7d"), sizes(10, 0)).
		container("legacyc", "legacyc", "sha256:old", "exited", daysAgo(20)).labelContainer("legacyc", "com.orca.project", "legacy").
		image("sha256:bad", []string{"b"}, tags("bad:1"), digest("b@sha256:1"), imgLabels("com.orca.project", "odd", "com.orca.retention", "forever"), sizes(10, 0))

	a := analyze(t, f.build(), noReview())
	if got := class(a, "image:sha256:old"); got != Safe {
		t.Errorf("retention elapsed 13 days ago: class %q, want SAFE", got)
	}
	c := a.cand("image:sha256:bad")
	if c == nil || c.Class != Protected || !containsText(c.Reasons, "not a duration: kept to be safe") {
		t.Errorf("unreadable retention must protect: %+v", c)
	}
}

func TestReviewHeuristics(t *testing.T) {
	f := newFixture().
		image("sha256:fresh", []string{"f"}, tags("fresh:1"), digest("f@sha256:1"), created(now.Add(-3*time.Hour)), sizes(10, 0)).
		image("sha256:huge", []string{"h"}, tags("huge:1"), digest("h@sha256:1"), sizes(20e9, 0)).
		image("sha256:local", []string{"lb"}, tags("mine:dev"), sizes(10, 0)).
		image("sha256:dangling", []string{"d"}, sizes(10, 0)).
		image("sha256:unknown", []string{"u"}, tags("u:1"), digest("u@sha256:1"), sizes(10, 11)).
		image("sha256:ambig", []string{"am"}, tags("am:1"), digest("am@sha256:1"), imgLabels("com.orca.project", "a", "com.docker.compose.project", "b"), sizes(10, 0)).
		volume("named").
		volume("scratch", "orca.gc.temporary", "true").
		container("zero", "zero", "sha256:dangling", "exited", daysAgo(30))

	inv := f.build()
	// The zero container has a known, empty writable layer and an unknown
	// (unmeasured) log: that must not count as "size unknown".
	for i := range inv.Containers {
		if inv.Containers[i].ID == "zero" {
			inv.Containers[i].SizeRw = 0
			inv.Containers[i].Log.Path = "/var/lib/docker/containers/zero/zero-json.log"
		}
	}
	a := analyze(t, inv, DefaultPolicy())

	for id, want := range map[string]string{
		"image:sha256:fresh":   "created or used 3 hours ago",
		"image:sha256:huge":    "large: would free 20GB",
		"image:sha256:local":   "built locally and never pushed",
		"image:sha256:unknown": "size unknown",
		"image:sha256:ambig":   "ambiguous project ownership",
		"volume:named":         "named volume: it may hold persistent data",
	} {
		c := a.cand(id)
		if c == nil || c.Class != Review || !containsText(c.Reasons, want) {
			t.Errorf("%s = %+v, want REVIEW because %q", id, c, want)
		}
	}
	for _, id := range []string{"image:sha256:dangling", "volume:scratch", "container:zero"} {
		if got := class(a, id); got != Safe {
			t.Errorf("%s: class %q, want SAFE (reasons: %v)", id, got, a.cand(id))
		}
	}
	if z := a.cand("container:zero"); z.ReclaimExact {
		t.Error("an unmeasured log makes the container's reclaim a lower bound")
	}
}

func TestDependencyPropagation(t *testing.T) {
	// The stopped container has a huge writable layer (REVIEW); its image
	// is SAFE on its own but can only go after the container, so it needs
	// review too.
	f := newFixture().
		image("sha256:img", []string{"i"}, tags("img:1"), digest("i@sha256:1"), sizes(10, 0)).
		container("c", "c", "sha256:img", "exited", daysAgo(40))
	inv := f.build()
	inv.Containers[0].SizeRw = 20e9

	a := analyze(t, inv, DefaultPolicy())
	img := a.cand("image:sha256:img")
	if class(a, "container:c") != Review || img.Class != Review {
		t.Fatalf("container %s, image %s; want both REVIEW", class(a, "container:c"), img.Class)
	}
	if len(img.RemoveFirst) != 1 || img.RemoveFirst[0] != "container:c" || !containsText(img.Reasons, "can only be removed after container c") {
		t.Errorf("image dependency not explained: RemoveFirst=%v reasons=%v", img.RemoveFirst, img.Reasons)
	}
}

func TestExternalVolume(t *testing.T) {
	a := analyze(t, newFixture().externalVolume("nfs-share").build(), noReview())
	if got := class(a, "volume:nfs-share"); got != External {
		t.Errorf("unused external volume: class %q, want EXTERNAL", got)
	}
}

func TestDeterministic(t *testing.T) {
	inv := sharedBase().build()
	var first bytes.Buffer
	if err := analyze(t, inv, DefaultPolicy()).Plan.WriteJSON(&first); err != nil {
		t.Fatal(err)
	}
	// Same inventory, any input order: byte-identical plan.
	for i := 0; i < 20; i++ {
		inv := sharedBase().build()
		r := rand.New(rand.NewSource(int64(i)))
		r.Shuffle(len(inv.Images), func(a, b int) { inv.Images[a], inv.Images[b] = inv.Images[b], inv.Images[a] })
		r.Shuffle(len(inv.Containers), func(a, b int) { inv.Containers[a], inv.Containers[b] = inv.Containers[b], inv.Containers[a] })
		r.Shuffle(len(inv.Layers), func(a, b int) { inv.Layers[a], inv.Layers[b] = inv.Layers[b], inv.Layers[a] })
		var again bytes.Buffer
		if err := analyze(t, inv, DefaultPolicy()).Plan.WriteJSON(&again); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Bytes(), again.Bytes()) {
			t.Fatalf("plan changed with input order (seed %d)", i)
		}
	}
}

func TestEveryCandidateIsExplained(t *testing.T) {
	a := analyze(t, sharedBase().build(), DefaultPolicy())
	if len(a.Plan.Candidates) == 0 {
		t.Fatal("expected candidates")
	}
	for _, c := range a.Plan.Candidates {
		if len(c.Reasons) == 0 || c.Summary == "" {
			t.Errorf("%s has no reason", c.ID)
		}
	}
}
