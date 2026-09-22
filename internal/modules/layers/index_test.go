package layers

import (
	"reflect"
	"testing"

	"orca/internal/extensions/engine"
)

const (
	base = "sha256:aaaa000000000000000000000000000000000000000000000000000000000000"
	appA = "sha256:bbbb000000000000000000000000000000000000000000000000000000000000"
	appB = "sha256:cccc000000000000000000000000000000000000000000000000000000000000"
	lib  = "sha256:dddd000000000000000000000000000000000000000000000000000000000000"
)

func TestBuildIndexSharedLayers(t *testing.T) {
	idx := BuildIndex([]engine.ImageLayers{
		{ImageID: "img-a", DiffIDs: []string{base, appA}, StorageDirs: []string{"/o/base", "/o/a"}},
		{ImageID: "img-b", DiffIDs: []string{base, appB}, StorageDirs: []string{"/o/base", "/o/b"}},
	})

	baseChain := ChainIDs([]string{base})[0]
	aChain := ChainIDs([]string{base, appA})[1]

	if got := idx.ImageLayers["img-a"]; !reflect.DeepEqual(got, []string{baseChain, aChain}) {
		t.Errorf("image->layers for img-a = %v", got)
	}
	if len(idx.Layers) != 3 {
		t.Fatalf("got %d unique layers, want 3", len(idx.Layers))
	}

	shared := idx.Shared()
	if len(shared) != 1 || shared[0].ChainID != baseChain {
		t.Fatalf("shared layers = %v, want only the base layer", shared)
	}
	if got := shared[0].Images; !reflect.DeepEqual(got, []string{"img-a", "img-b"}) {
		t.Errorf("layer->images for base = %v", got)
	}
	if shared[0].RefCount() != 2 || shared[0].Depth != 1 {
		t.Errorf("base layer refcount/depth = %d/%d, want 2/1", shared[0].RefCount(), shared[0].Depth)
	}
	if got := idx.Layers[aChain]; got.RefCount() != 1 || got.Shared() {
		t.Errorf("app layer should be unique to img-a, got %v", got.Images)
	}
	if len(idx.MultiDir()) != 0 {
		t.Errorf("no layer should be stored twice, got %v", idx.MultiDir())
	}
}

func TestBuildIndexSameDiffIDDifferentParent(t *testing.T) {
	// The same content on top of different parents is two different layers.
	idx := BuildIndex([]engine.ImageLayers{
		{ImageID: "img-a", DiffIDs: []string{appA, lib}},
		{ImageID: "img-b", DiffIDs: []string{appB, lib}},
	})

	if len(idx.Layers) != 4 {
		t.Fatalf("got %d unique layers, want 4", len(idx.Layers))
	}
	if len(idx.Shared()) != 0 {
		t.Errorf("layers with the same DiffID but different parents must not be shared")
	}
	dups := idx.DuplicatedDiffIDs()
	if len(dups) != 1 || len(dups[lib]) != 2 {
		t.Errorf("duplicated DiffIDs = %v, want lib under 2 chain IDs", dups)
	}
}

func TestBuildIndexLayerStoredInTwoDirs(t *testing.T) {
	idx := BuildIndex([]engine.ImageLayers{
		{ImageID: "img-a", DiffIDs: []string{base, appA}, StorageDirs: []string{"/o/base", "/o/buildkit1"}},
		{ImageID: "img-b", DiffIDs: []string{base, appA, lib}, StorageDirs: []string{"/o/base", "/o/buildkit2", "/o/lib"}},
	})

	multi := idx.MultiDir()
	if len(multi) != 1 {
		t.Fatalf("got %d multi-directory layers, want 1", len(multi))
	}
	if want := []string{"/o/buildkit1", "/o/buildkit2"}; !reflect.DeepEqual(multi[0].StorageDirs, want) {
		t.Errorf("dirs = %v, want %v", multi[0].StorageDirs, want)
	}
}

func TestBuildIndexIgnoresMismatchedDirs(t *testing.T) {
	// A directory list that does not line up with the layers must not be
	// attributed to any layer.
	idx := BuildIndex([]engine.ImageLayers{
		{ImageID: "img-a", DiffIDs: []string{base, appA}, StorageDirs: []string{"/o/only-one"}},
	})
	for _, l := range idx.Layers {
		if len(l.StorageDirs) != 0 {
			t.Errorf("layer %s got dirs %v from a mismatched list", l.ChainID, l.StorageDirs)
		}
	}
}

func TestBuildIndexSameImageListedTwice(t *testing.T) {
	set := engine.ImageLayers{ImageID: "img-a", DiffIDs: []string{base}}
	idx := BuildIndex([]engine.ImageLayers{set, set})
	if got := idx.Layers[base].RefCount(); got != 1 {
		t.Errorf("refcount = %d, want 1", got)
	}
}
