package gc

import (
	"testing"

	"github.com/HamzaGbada/orca/internal/modules/graph"
	"github.com/HamzaGbada/orca/internal/modules/layers"
)

func layerID(names ...string) string {
	var diffs []string
	for _, n := range names {
		diffs = append(diffs, diff(n))
	}
	c := layers.ChainIDs(diffs)
	return graph.NodeID(graph.Layer, c[len(c)-1])
}

// noReview disables the time and size review heuristics so a test sees the
// mark phase and classification alone.
func noReview() Policy {
	p := DefaultPolicy()
	p.ReviewRecent, p.ReviewLarge = 0, 0
	return p
}

// web (running) uses app and the "data" volume; tool shares app's base
// layer but nothing uses it; old is a long-stopped container.
func sharedBase() *fixture {
	return newFixture().
		image("sha256:app", []string{"base", "app"}, tags("app:1"), digest("app@sha256:1"), sizes(300, 100)).
		image("sha256:tool", []string{"base", "tool"}, tags("tool:1"), digest("tool@sha256:2"), sizes(150, 100)).
		size([]string{"base"}, 100).size([]string{"base", "app"}, 200).size([]string{"base", "tool"}, 50).
		volume("data").
		container("web", "web", "sha256:app", "running", now, volMount("data", "/data"), bindMount("/home/me/src", "/src")).
		container("old", "old", "sha256:tool", "exited", daysAgo(40))
}

func TestMarkRunningContainerChain(t *testing.T) {
	a := analyze(t, sharedBase().build(), noReview())

	for _, id := range []string{"container:web", "image:sha256:app", layerID("base"), layerID("base", "app"), "volume:data"} {
		if !a.mark.marked(id) {
			t.Errorf("%s must be marked (reachable from the running container)", id)
		}
		if a.cand(id) != nil {
			t.Errorf("%s is in use and must not be a candidate", id)
		}
	}
	for _, id := range []string{"container:old", "image:sha256:tool", layerID("base", "tool")} {
		if a.mark.marked(id) {
			t.Errorf("%s must be unmarked", id)
		}
	}
	if c := a.cand("container:old"); c == nil || c.Class != Safe {
		t.Errorf("old stopped container: %+v, want SAFE", c)
	}
}

func TestSharedLayerStaysMarked(t *testing.T) {
	a := analyze(t, sharedBase().build(), noReview())

	// The base layer is shared by app (kept) and tool (candidate): it is
	// reachable through app and must never be counted as reclaimable.
	if !a.mark.marked(layerID("base")) {
		t.Fatal("shared base layer must stay marked while app is reachable")
	}
	tool := a.cand("image:sha256:tool")
	if tool == nil || tool.Class != Safe || tool.ReclaimableSize != 50 || !tool.ReclaimExact {
		t.Fatalf("tool = %+v, want SAFE reclaiming exactly its own 50 B", tool)
	}
	if !containsText(tool.Reasons, "1 of its 2 layers is shared with kept images") {
		t.Errorf("tool reasons should mention the kept shared layer: %v", tool.Reasons)
	}
	// Plan total: the tool image (50) + old container (1000 writable).
	if got := a.Plan.Totals.Safe; got != 1050 {
		t.Errorf("SAFE total = %d, want 1050 (base layer excluded)", got)
	}
}

func TestBindMountsAreExternalAndNeverTraversed(t *testing.T) {
	a := analyze(t, sharedBase().build(), noReview())
	id := "bindmount:/home/me/src"
	if a.mark.marked(id) {
		t.Error("bind-mount targets must never be marked")
	}
	if c := a.cand(id); c == nil || c.Class != External || c.ReclaimableSize != 0 {
		t.Errorf("bind mount = %+v, want EXTERNAL reclaiming nothing", c)
	}
}

func TestMultiParentChain(t *testing.T) {
	// Classic builder: parent P (intermediate, untagged) and child C.
	f := newFixture().
		image("sha256:P", []string{"l1"}, sizes(100, 100)).
		image("sha256:C", []string{"l1", "l2"}, tags("c:1"), digest("c@sha256:3"), parent("sha256:P"), sizes(130, 100)).
		size([]string{"l1"}, 100).size([]string{"l1", "l2"}, 30)

	a := analyze(t, f.build(), noReview())
	p, c := a.cand("image:sha256:P"), a.cand("image:sha256:C")
	if p == nil || c == nil || p.Class != Safe || c.Class != Safe {
		t.Fatalf("P=%+v C=%+v, want both SAFE", p, c)
	}
	if len(p.RemoveFirst) != 1 || p.RemoveFirst[0] != "image:sha256:C" {
		t.Errorf("parent must be removed after its child: RemoveFirst=%v", p.RemoveFirst)
	}
	// l1 is shared by P and C, both candidates: counted once in the total.
	if a.Plan.Totals.Safe != 130 || !a.Plan.Totals.SafeExact || a.Plan.Totals.SharedLayersSafe != 100 {
		t.Errorf("totals = %+v, want 130 exact with 100 shared", a.Plan.Totals)
	}

	// Once the child is used by a running container, the whole chain is
	// reachable through the parent edge.
	f.container("run", "run", "sha256:C", "running", now)
	a = analyze(t, f.build(), noReview())
	if a.cand("image:sha256:P") != nil || !a.mark.marked("image:sha256:P") || !a.mark.marked(layerID("l1")) {
		t.Error("the parent of a used image and its layers must be kept")
	}
}

func TestOrphanedSubtreeCountedOnce(t *testing.T) {
	// x and y share base b2; nothing uses either. b2's size is unknown,
	// but each image's total pins it down exactly.
	f := newFixture().
		image("sha256:x", []string{"b2", "x"}, tags("x:1"), digest("x@sha256:4"), sizes(1010, 1000)).
		image("sha256:y", []string{"b2", "y"}, tags("y:1"), digest("y@sha256:5"), sizes(1020, 1000)).
		size([]string{"b2", "x"}, 10).size([]string{"b2", "y"}, 20)

	a := analyze(t, f.build(), noReview())
	tot := a.Plan.Totals
	if tot.Safe != 1030 || !tot.SafeExact || tot.SharedLayersSafe != 1000 {
		t.Errorf("totals = %+v, want 1030 exact (10 + 20 + base 1000 once)", tot)
	}
	if x := a.cand("image:sha256:x"); x.ReclaimableSize != 10 {
		t.Errorf("x alone frees only its own layer, got %d", x.ReclaimableSize)
	}

	// With the containerd image store, image totals don't pin layer sizes:
	// the shared base is unknown and the total becomes a lower bound.
	f.containerd = true
	a = analyze(t, f.build(), noReview())
	if tot := a.Plan.Totals; tot.Safe != 30 || tot.SafeExact {
		t.Errorf("containerd totals = %+v, want lower bound 30, not exact", tot)
	}
}

func TestTotalsNeverExceedMeasuredUsage(t *testing.T) {
	a := analyze(t, sharedBase().build(), noReview())
	if containsText(a.Plan.Warnings, "BUG") {
		t.Fatalf("consistent inventory produced a warning: %v", a.Plan.Warnings)
	}
	// Corrupt the inventory: Docker reports less image usage than one
	// image's exclusive layers. The plan must flag it, not hide it.
	inv := sharedBase().build()
	inv.ImagesSize = 10
	a = analyze(t, inv, noReview())
	if !containsText(a.Plan.Warnings, "BUG: projected image reclaim") {
		t.Errorf("inconsistent sizes must be flagged, warnings: %v", a.Plan.Warnings)
	}
}
