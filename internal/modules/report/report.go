// Package report summarizes an inventory into the storage report: what
// Docker uses and how much could potentially be reclaimed.
//
// Reclaim figures are first approximations and deliberately conservative:
// they never count protected resources, running containers or volumes that
// need review, and image figures are lower bounds (see Images.ReclaimableAtLeast).
package report

import (
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/pressure"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
)

type Report struct {
	Host       inventory.Host
	Disk       *pressure.Status
	Consistent bool
	Images     Images
	Layers     Layers
	Containers Containers
	Volumes    Volumes
	BuildCache BuildCache
	Logs       Logs
	Recovery   Recovery
	Warnings   []string
}

type Images struct {
	Total, Tagged, Dangling, Intermediate int
	// Used images have at least one container created from them.
	Used int
	// Size is the engine's total of unique layer bytes.
	Size int64
	// ReclaimableAtLeast sums the unique size of unused, unprotected images.
	// It is a lower bound: layers shared only between unused images are not
	// counted. Exact figures need per-layer sizes (Sprint 2).
	ReclaimableAtLeast int64
}

type Layers struct {
	Total, Shared, Unique int
}

type Containers struct {
	Running, Stopped int
	// Writable is the total size of writable layers.
	Writable int64
	// Reclaimable is the writable size of stopped, unprotected containers.
	Reclaimable int64
}

type Volumes struct {
	Total, Referenced, Protected, Candidate, Unreferenced int
	// Size is the total size of volumes whose size is known.
	Size int64
	// CandidateSize is reclaimable; ReviewSize belongs to unreferenced named
	// volumes that need human review and is not counted as reclaimable.
	CandidateSize, ReviewSize int64
}

type BuildCache struct {
	Entries     int
	Size        int64
	Reclaimable int64
}

type Logs struct {
	// Total is the size of the logs that could be measured.
	Total           int64
	Measured, Count int
	Unlimited       int
	Large           int
}

type Recovery struct {
	Images, Containers, Volumes, BuildCache, Total int64
}

// Summarize builds the report of one inventory.
func Summarize(inv inventory.Inventory) Report {
	r := Report{Host: inv.Host, Disk: inv.Disk, Consistent: inv.Consistent, Warnings: inv.Warnings}

	r.Images.Total = len(inv.Images)
	r.Images.Size = inv.ImagesSize
	for _, img := range inv.Images {
		switch img.Kind {
		case discovery.Tagged:
			r.Images.Tagged++
		case discovery.Dangling:
			r.Images.Dangling++
		case discovery.Intermediate:
			r.Images.Intermediate++
		}
		if img.Referenced() {
			r.Images.Used++
		} else if !img.Protected && img.UniqueSize > 0 {
			r.Images.ReclaimableAtLeast += img.UniqueSize
		}
	}

	r.Layers.Total = len(inv.Layers)
	for _, l := range inv.Layers {
		if l.Shared() {
			r.Layers.Shared++
		}
	}
	r.Layers.Unique = r.Layers.Total - r.Layers.Shared

	for _, ct := range inv.Containers {
		if ct.Live {
			r.Containers.Running++
		} else {
			r.Containers.Stopped++
		}
		if ct.SizeRw > 0 {
			r.Containers.Writable += ct.SizeRw
			if !ct.Live && !ct.Protected {
				r.Containers.Reclaimable += ct.SizeRw
			}
		}

		r.Logs.Count++
		if ct.Log.Size >= 0 {
			r.Logs.Measured++
			r.Logs.Total += ct.Log.Size
		}
		if ct.Log.Unlimited {
			r.Logs.Unlimited++
		}
		if ct.Log.Large {
			r.Logs.Large++
		}
	}

	r.Volumes.Total = len(inv.Volumes)
	for _, v := range inv.Volumes {
		size := max(v.Size, 0)
		r.Volumes.Size += size
		switch v.State {
		case volumes.Protected:
			r.Volumes.Protected++
		case volumes.Referenced:
			r.Volumes.Referenced++
		case volumes.Candidate:
			r.Volumes.Candidate++
			r.Volumes.CandidateSize += size
		case volumes.Unreferenced:
			r.Volumes.Unreferenced++
			r.Volumes.ReviewSize += size
		}
	}

	r.BuildCache.Entries = len(inv.BuildCache)
	for _, e := range inv.BuildCache {
		r.BuildCache.Size += e.Size
		if e.Reclaimable {
			r.BuildCache.Reclaimable += e.Size
		}
	}

	r.Recovery = Recovery{
		Images:     r.Images.ReclaimableAtLeast,
		Containers: r.Containers.Reclaimable,
		Volumes:    r.Volumes.CandidateSize,
		BuildCache: r.BuildCache.Reclaimable,
	}
	r.Recovery.Total = r.Recovery.Images + r.Recovery.Containers + r.Recovery.Volumes + r.Recovery.BuildCache
	return r
}
