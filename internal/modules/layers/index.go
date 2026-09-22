package layers

import (
	"sort"

	"orca/internal/extensions/engine"
)

// Layer is one layer identified by its ChainID.
type Layer struct {
	ChainID string
	DiffID  string
	// Depth is the 1-based position of the layer in its chain.
	Depth int
	// StorageDirs are the distinct overlay2 directories image overlay stacks
	// use for this layer. Usually one; more than one means the same content
	// is stored more than once, which happens when BuildKit built the layer
	// in its own snapshot directory. Empty when unknown.
	StorageDirs []string
	// Images are the IDs of every image record that includes this layer.
	Images []string
}

// RefCount is the number of image records that include the layer.
func (l Layer) RefCount() int {
	return len(l.Images)
}

// Shared reports whether more than one image record includes the layer.
func (l Layer) Shared() bool {
	return len(l.Images) > 1
}

// Index maps images to layers and layers to images.
type Index struct {
	// Layers by ChainID.
	Layers map[string]*Layer
	// ImageLayers holds the ChainIDs of each image, bottom layer first.
	ImageLayers map[string][]string
}

// BuildIndex builds both directions of the image/layer relationship.
func BuildIndex(sets []engine.ImageLayers) Index {
	idx := Index{
		Layers:      make(map[string]*Layer),
		ImageLayers: make(map[string][]string, len(sets)),
	}

	for _, set := range sets {
		chain := ChainIDs(set.DiffIDs)
		idx.ImageLayers[set.ImageID] = chain

		for i, chainID := range chain {
			l, ok := idx.Layers[chainID]
			if !ok {
				l = &Layer{ChainID: chainID, DiffID: set.DiffIDs[i], Depth: i + 1}
				idx.Layers[chainID] = l
			}
			if len(set.StorageDirs) == len(chain) {
				l.StorageDirs = appendUnique(l.StorageDirs, set.StorageDirs[i])
			}
			l.Images = appendUnique(l.Images, set.ImageID)
		}
	}

	for _, l := range idx.Layers {
		sort.Strings(l.StorageDirs)
		sort.Strings(l.Images)
	}
	return idx
}

func appendUnique(ss []string, s string) []string {
	for _, existing := range ss {
		if existing == s {
			return ss
		}
	}
	return append(ss, s)
}

// Shared returns layers included by more than one image, most referenced
// first.
func (idx Index) Shared() []*Layer {
	return idx.filter(func(l *Layer) bool { return l.Shared() })
}

// MultiDir returns layers stored in more than one overlay2 directory.
func (idx Index) MultiDir() []*Layer {
	return idx.filter(func(l *Layer) bool { return len(l.StorageDirs) > 1 })
}

// All returns every layer, most referenced first.
func (idx Index) All() []*Layer {
	return idx.filter(func(*Layer) bool { return true })
}

// DuplicatedDiffIDs returns DiffIDs stored under more than one ChainID: the
// same content extracted more than once because it sits on different parents.
func (idx Index) DuplicatedDiffIDs() map[string][]string {
	byDiff := make(map[string][]string)
	for _, l := range idx.Layers {
		byDiff[l.DiffID] = append(byDiff[l.DiffID], l.ChainID)
	}
	for diffID, chains := range byDiff {
		if len(chains) < 2 {
			delete(byDiff, diffID)
			continue
		}
		sort.Strings(chains)
	}
	return byDiff
}

func (idx Index) filter(keep func(*Layer) bool) []*Layer {
	var out []*Layer
	for _, l := range idx.Layers {
		if keep(l) {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RefCount() != out[j].RefCount() {
			return out[i].RefCount() > out[j].RefCount()
		}
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].ChainID < out[j].ChainID
	})
	return out
}
