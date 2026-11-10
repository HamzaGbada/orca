package gc

import "github.com/HamzaGbada/orca/internal/modules/graph"

// reach records how the mark phase reached a resource.
type reach struct {
	root Root   // the GC root the traversal started from
	prev string // the node it was reached from; "" for the root itself
}

// mark is the result of the mark phase: every reachable (marked) resource
// and how it was reached. Unmarked resources are GC candidates.
type mark struct {
	reached map[string]reach
	roots   map[string][]Root // node -> its own roots
}

// markReachable runs the mark phase: a transitive closure over dependency
// edges (container -> image -> layers, container -> volumes, image -> parent
// image, layer -> parent layer, cache -> parent cache, project -> members of
// a project root).
//
// Absolute roots are traversed before policy roots, so a resource kept by
// both is explained by its absolute root. Bind-mount targets are never
// entered: they are outside Docker's storage and are not part of marking.
//
// A layer is marked as soon as any image that contains it is marked, so a
// shared layer stays reachable while one image still needs it.
func markReachable(ix *index, roots []Root) mark {
	m := mark{reached: map[string]reach{}, roots: map[string][]Root{}}
	for _, r := range roots {
		m.roots[r.Node] = append(m.roots[r.Node], r)
	}
	for _, absolute := range []bool{true, false} {
		var queue []string
		for _, r := range roots {
			if r.Absolute != absolute {
				continue
			}
			if _, done := m.reached[r.Node]; !done {
				m.reached[r.Node] = reach{root: r}
				queue = append(queue, r.Node)
			}
		}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			for _, e := range ix.g.Out(id) {
				if n, ok := ix.g.Node(e.To); !ok || n.Kind == graph.BindMount {
					continue
				}
				if _, done := m.reached[e.To]; !done {
					m.reached[e.To] = reach{root: m.reached[id].root, prev: id}
					queue = append(queue, e.To)
				}
			}
		}
	}
	return m
}

// marked reports whether a resource is reachable from a GC root.
func (m mark) marked(id string) bool {
	_, ok := m.reached[id]
	return ok
}

// path returns the chain of nodes from the root to id.
func (m mark) path(id string) []string {
	var p []string
	for cur := id; cur != ""; cur = m.reached[cur].prev {
		p = append([]string{cur}, p...)
		if _, ok := m.reached[cur]; !ok {
			break
		}
	}
	return p
}
