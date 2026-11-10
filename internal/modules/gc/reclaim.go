package gc

import (
	"sort"

	"github.com/HamzaGbada/orca/internal/modules/graph"
)

// Reclaimability.
//
// A candidate's own reclaimable size is what removing it alone frees:
//
//	container: its writable layer plus its log files
//	image:     its exclusive layers (Docker's Size - SharedSize): layers no
//	           other image uses, so none of them can be reachable
//	volume:    its size
//	cache:     its size (records sharing storage with images are roots)
//
// Layers shared only between candidate images are freed only when all of
// those images go. They belong to no single candidate and are counted once,
// at plan level, by sharedLayers.

// reclaimOf returns a candidate's own reclaimable size and whether it is
// exact (false when part of it is unknown; the size is then a lower bound).
func reclaimOf(r *resource) (int64, bool) {
	switch r.typ {
	case Container:
		c := r.container
		n, exact := int64(0), true
		if c.SizeRw >= 0 {
			n += c.SizeRw
		} else {
			exact = false
		}
		if c.Log.Path != "" {
			if c.Log.Size >= 0 {
				n += c.Log.Size
			} else {
				exact = false
			}
		}
		return n, exact
	case Image:
		if r.image.UniqueSize >= 0 {
			return r.image.UniqueSize, true
		}
		return 0, false
	case Volume, BuildCache:
		if r.size >= 0 {
			return r.size, true
		}
		return 0, false
	}
	return 0, true
}

// fact says that the unknown layers in a set add up to a known total.
type fact struct {
	image   string
	unknown []string
	total   int64
}

// sizer sums layer sizes. Unknown sizes are bounded from below with facts
// derived from image sizes; the result is never more than the real size.
type sizer struct {
	size  map[string]int64 // known layer sizes by ChainID
	facts []fact
}

func newSizer(ix *index) *sizer {
	s := &sizer{size: map[string]int64{}}
	for _, l := range ix.inv.Layers {
		if l.Size >= 0 {
			s.size[l.ChainID] = l.Size
		}
	}
	refs := map[string]int{}
	for _, l := range ix.inv.Layers {
		refs[l.ChainID] = l.RefCount()
	}
	// Image totals are sums of layer sizes only in the classic image store.
	if ix.inv.Host.ContainerdStore {
		return s
	}
	for _, img := range ix.inv.Images {
		var all, shared []string
		knownAll, knownExcl := int64(0), int64(0)
		for _, id := range img.Layers {
			sz, ok := s.size[id]
			if ok {
				knownAll += sz
				if refs[id] == 1 {
					knownExcl += sz
				}
				continue
			}
			all = append(all, id)
			if refs[id] > 1 {
				shared = append(shared, id)
			}
		}
		if len(all) == 0 || img.Size < 0 {
			continue
		}
		restAll := img.Size - knownAll // sum of the unknown layers
		if restAll >= 0 {
			s.facts = append(s.facts, fact{img.ID, all, restAll})
		}
		// The unknown shared layers add up to the unknown total minus the
		// unknown exclusive part (Size - SharedSize covers exclusive layers).
		if img.UniqueSize >= 0 && len(shared) > 0 {
			restShared := restAll - (img.UniqueSize - knownExcl)
			if restShared >= 0 && restAll >= 0 {
				s.facts = append(s.facts, fact{img.ID, shared, restShared})
			}
		}
	}
	// Largest first gives the best greedy bound; ties by image for
	// determinism.
	sort.SliceStable(s.facts, func(i, j int) bool {
		if s.facts[i].total != s.facts[j].total {
			return s.facts[i].total > s.facts[j].total
		}
		return s.facts[i].image < s.facts[j].image
	})
	return s
}

// sum adds up the sizes of the given layers. exact is false when some
// sizes are unknown; sum is then a lower bound: known sizes plus totals of
// facts whose unknown layers all lie in the set, each layer used once.
func (s *sizer) sum(ids []string) (sum int64, exact bool) {
	unknown := map[string]bool{}
	for _, id := range ids {
		if sz, ok := s.size[id]; ok {
			sum += sz
		} else {
			unknown[id] = true
		}
	}
	for _, f := range s.facts {
		if len(unknown) == 0 {
			break
		}
		fits := true
		for _, id := range f.unknown {
			if !unknown[id] {
				fits = false
				break
			}
		}
		if fits {
			sum += f.total
			for _, id := range f.unknown {
				delete(unknown, id)
			}
		}
	}
	return sum, len(unknown) == 0
}

// sharedLayers returns the layers referenced by two or more images, all of
// which are in the given set: they are freed only when the whole set goes.
// Exclusive layers are already counted in each image's own reclaim.
func sharedLayers(ix *index, m mark, images map[string]bool) []string {
	var out []string
	for _, l := range ix.inv.Layers {
		if l.RefCount() < 2 {
			continue
		}
		if m.marked(graph.NodeID(graph.Layer, l.ChainID)) {
			continue
		}
		all := true
		for _, img := range l.Images {
			if !images[graph.NodeID(graph.Image, img)] {
				all = false
				break
			}
		}
		if all {
			out = append(out, l.ChainID)
		}
	}
	sort.Strings(out)
	return out
}
