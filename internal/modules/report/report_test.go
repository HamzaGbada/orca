package report

import (
	"testing"

	"orca/internal/extensions/engine"
	"orca/internal/modules/discovery"
	"orca/internal/modules/inventory"
	"orca/internal/modules/layers"
	"orca/internal/modules/volumes"
)

func img(kind discovery.ImageKind, unique int64, containers []string, protected bool) inventory.Image {
	return inventory.Image{
		Image:      discovery.Image{Kind: kind, Containers: containers},
		UniqueSize: unique,
		Protected:  protected,
	}
}

func TestSummarize(t *testing.T) {
	inv := inventory.Inventory{
		ImagesSize: 10_000,
		Images: []inventory.Image{
			img(discovery.Tagged, 100, []string{"c1"}, false), // used: not reclaimable
			img(discovery.Tagged, 200, nil, false),            // reclaimable
			img(discovery.Dangling, 300, nil, true),           // protected: not reclaimable
			img(discovery.Dangling, 400, nil, false),          // reclaimable
			img(discovery.Intermediate, -1, nil, false),       // unknown size: skipped
		},
		Layers: []*layers.Layer{{Images: []string{"a", "b"}}, {Images: []string{"a"}}, {Images: []string{"b"}}},
		Containers: []inventory.Container{
			{Container: engine.Container{SizeRw: 10}, Live: true, Log: inventory.Log{Size: 5, Unlimited: true}},
			{Container: engine.Container{SizeRw: 20}, Log: inventory.Log{Size: -1}},
			{Container: engine.Container{SizeRw: 40}, Protected: true, Log: inventory.Log{Size: 7, Large: true}},
			{Container: engine.Container{SizeRw: -1}, Log: inventory.Log{Size: -1}},
		},
		Volumes: []inventory.Volume{
			{Classification: volumes.Classification{State: volumes.Protected}, Size: 1},
			{Classification: volumes.Classification{State: volumes.Referenced}, Size: 2},
			{Classification: volumes.Classification{State: volumes.Candidate}, Size: 4},
			{Classification: volumes.Classification{State: volumes.Unreferenced}, Size: 8},
			{Classification: volumes.Classification{State: volumes.Candidate}, Size: -1},
		},
		BuildCache: []inventory.CacheEntry{
			{CacheRecord: engine.CacheRecord{Size: 50}, Reclaimable: true},
			{CacheRecord: engine.CacheRecord{Size: 70}},
		},
	}

	r := Summarize(inv)

	if r.Images.Total != 5 || r.Images.Tagged != 2 || r.Images.Dangling != 2 || r.Images.Intermediate != 1 || r.Images.Used != 1 {
		t.Errorf("image counts = %+v", r.Images)
	}
	if r.Images.Size != 10_000 || r.Images.ReclaimableAtLeast != 600 {
		t.Errorf("image size/reclaim = %d/%d, want 10000/600", r.Images.Size, r.Images.ReclaimableAtLeast)
	}
	if r.Layers != (Layers{Total: 3, Shared: 1, Unique: 2}) {
		t.Errorf("layers = %+v", r.Layers)
	}
	if r.Containers != (Containers{Running: 1, Stopped: 3, Writable: 70, Reclaimable: 20}) {
		t.Errorf("containers = %+v, want only the stopped unprotected writable layer reclaimable", r.Containers)
	}
	if r.Logs != (Logs{Total: 12, Measured: 2, Count: 4, Unlimited: 1, Large: 1}) {
		t.Errorf("logs = %+v", r.Logs)
	}
	want := Volumes{Total: 5, Referenced: 1, Protected: 1, Candidate: 2, Unreferenced: 1, Size: 15, CandidateSize: 4, ReviewSize: 8}
	if r.Volumes != want {
		t.Errorf("volumes = %+v, want %+v", r.Volumes, want)
	}
	if r.BuildCache != (BuildCache{Entries: 2, Size: 120, Reclaimable: 50}) {
		t.Errorf("build cache = %+v", r.BuildCache)
	}
	if r.Recovery != (Recovery{Images: 600, Containers: 20, Volumes: 4, BuildCache: 50, Total: 674}) {
		t.Errorf("recovery = %+v; review volumes, protected and live resources must not count", r.Recovery)
	}
}
