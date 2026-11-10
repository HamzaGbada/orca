package layers

// EmptyLayerDiffID is the DiffID of an empty layer (a tar archive of 1024
// zero bytes). Docker sizes such a layer as 0 bytes.
const EmptyLayerDiffID = "sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef"

// SizeFacts are the size facts about one image that the Docker API exposes.
type SizeFacts struct {
	// DiffIDs of the image, bottom layer first.
	DiffIDs []string
	// Size is the image's total size: the sum of its layers' sizes in the
	// classic image store.
	Size int64
	// History is the size of each build step, oldest first; nil if unknown.
	// Steps that created no layer have size 0.
	History []int64
}

// InferSizes returns the size of every layer (by ChainID) that follows
// exactly from known sizes (e.g. read from the layer database) and the
// facts of each image:
//
//  1. a layer with the empty DiffID has size 0;
//  2. when an image's non-zero history steps match its non-empty layers one
//     to one, each of those layers has its step's size and the others 0;
//  3. when an image has a single layer of unknown size, that size is the
//     image size minus its known layers (repeated until nothing changes).
//
// Known sizes are never overwritten, and an inference that contradicts them
// (or would be negative) is dropped, so every returned size is exact.
// These rules hold for the classic image store only.
func InferSizes(images []SizeFacts, known map[string]int64) map[string]int64 {
	sizes := make(map[string]int64, len(known))
	for id, s := range known {
		sizes[id] = s
	}
	chains := make([][]string, len(images))
	for i, img := range images {
		chains[i] = ChainIDs(img.DiffIDs)
	}

	// Rules 1 and 2.
	for i, img := range images {
		for j, d := range img.DiffIDs {
			if d == EmptyLayerDiffID {
				setIfUnknown(sizes, chains[i][j], 0)
			}
		}
		var steps []int64
		for _, s := range img.History {
			if s > 0 {
				steps = append(steps, s)
			}
		}
		var real []int
		for j, d := range img.DiffIDs {
			if d != EmptyLayerDiffID {
				real = append(real, j)
			}
		}
		if img.History == nil || len(steps) != len(real) {
			continue // ambiguous: some layers hold only directories (size 0)
		}
		proposed := make(map[string]int64, len(real))
		for k, j := range real {
			proposed[chains[i][j]] = steps[k]
		}
		if consistent(sizes, proposed) {
			for id, s := range proposed {
				setIfUnknown(sizes, id, s)
			}
		}
	}

	// Rule 3, to a fixpoint.
	for changed := true; changed; {
		changed = false
		for i, img := range images {
			var unknown string
			missing, sum := 0, int64(0)
			for _, id := range chains[i] {
				if s, ok := sizes[id]; ok {
					sum += s
				} else if id != unknown {
					missing++
					unknown = id
				}
			}
			if missing == 1 && img.Size-sum >= 0 {
				sizes[unknown] = img.Size - sum
				changed = true
			}
		}
	}
	return sizes
}

func setIfUnknown(sizes map[string]int64, id string, s int64) {
	if _, ok := sizes[id]; !ok {
		sizes[id] = s
	}
}

// consistent reports whether proposed sizes agree with the known ones.
func consistent(sizes, proposed map[string]int64) bool {
	for id, s := range proposed {
		if k, ok := sizes[id]; ok && k != s {
			return false
		}
	}
	return true
}
