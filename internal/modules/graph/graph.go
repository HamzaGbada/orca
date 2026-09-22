// Package graph is Orca's resource dependency graph: which resources need
// which others. An edge A -> B means "A depends on B": B cannot be removed
// while A exists. Sprint 2's mark phase walks these edges from GC roots.
//
// Project membership is recorded with Member edges, which group resources
// but are not dependencies; traversals for reachability skip them.
package graph

import (
	"fmt"
	"sort"
)

// Kind is the type of a resource node.
type Kind string

const (
	Project    Kind = "project"
	Container  Kind = "container"
	Image      Kind = "image"
	Layer      Kind = "layer"
	Volume     Kind = "volume"
	BindMount  Kind = "bindmount"
	Log        Kind = "log"
	BuildCache Kind = "buildcache"
)

// EdgeKind is the relationship an edge represents.
type EdgeKind string

const (
	// Uses: container -> image it was created from.
	Uses EdgeKind = "uses"
	// Mounts: container -> volume or bind mount.
	Mounts EdgeKind = "mounts"
	// Writes: container -> its log.
	Writes EdgeKind = "writes"
	// Contains: image -> each of its layers.
	Contains EdgeKind = "contains"
	// Parent: layer -> parent layer, image -> parent image,
	// build cache record -> parent record.
	Parent EdgeKind = "parent"
	// Member: project -> resource. Grouping only, not a dependency.
	Member EdgeKind = "member"
)

// Node is one resource. ID is unique across kinds: "<kind>:<resource id>".
type Node struct {
	ID    string
	Kind  Kind
	Label string
	// Size in bytes; -1 when unknown.
	Size int64
}

// Edge is a directed relationship From -> To.
type Edge struct {
	From string
	To   string
	Kind EdgeKind
}

// Graph is a directed graph of resources.
type Graph struct {
	nodes map[string]*Node
	out   map[string][]Edge
	in    map[string][]Edge
}

func New() *Graph {
	return &Graph{
		nodes: make(map[string]*Node),
		out:   make(map[string][]Edge),
		in:    make(map[string][]Edge),
	}
}

// NodeID builds the graph ID of a resource.
func NodeID(k Kind, id string) string {
	return string(k) + ":" + id
}

// AddNode adds a node, or returns the existing node with the same ID.
func (g *Graph) AddNode(k Kind, id, label string, size int64) *Node {
	nid := NodeID(k, id)
	if n, ok := g.nodes[nid]; ok {
		return n
	}
	n := &Node{ID: nid, Kind: k, Label: label, Size: size}
	g.nodes[nid] = n
	return n
}

// AddEdge adds from -> to. Both nodes must exist; duplicate edges are
// ignored.
func (g *Graph) AddEdge(from, to string, k EdgeKind) error {
	if _, ok := g.nodes[from]; !ok {
		return fmt.Errorf("edge %s -> %s: unknown node %s", from, to, from)
	}
	if _, ok := g.nodes[to]; !ok {
		return fmt.Errorf("edge %s -> %s: unknown node %s", from, to, to)
	}
	e := Edge{From: from, To: to, Kind: k}
	for _, existing := range g.out[from] {
		if existing == e {
			return nil
		}
	}
	g.out[from] = append(g.out[from], e)
	g.in[to] = append(g.in[to], e)
	return nil
}

// Node returns a node by ID.
func (g *Graph) Node(id string) (*Node, bool) {
	n, ok := g.nodes[id]
	return n, ok
}

// Nodes returns all nodes sorted by ID.
func (g *Graph) Nodes() []*Node {
	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Edges returns all edges sorted by From, To, Kind.
func (g *Graph) Edges() []Edge {
	var out []Edge
	for _, es := range g.out {
		out = append(out, es...)
	}
	sortEdges(out)
	return out
}

// Out returns the edges leaving a node (what it depends on).
func (g *Graph) Out(id string) []Edge { return g.out[id] }

// In returns the edges entering a node: its reverse references.
func (g *Graph) In(id string) []Edge { return g.in[id] }

// RefCount is the number of resources depending on a node, not counting
// project membership.
func (g *Graph) RefCount(id string) int {
	n := 0
	for _, e := range g.in[id] {
		if e.Kind != Member {
			n++
		}
	}
	return n
}

// Dependencies follows dependency edges (all kinds except Member).
func Dependencies(e Edge) bool { return e.Kind != Member }

// Reachable returns every node reachable from the roots by edges accepted
// by follow, including the roots themselves. Unknown roots are ignored.
func (g *Graph) Reachable(roots []string, follow func(Edge) bool) map[string]bool {
	seen := make(map[string]bool)
	stack := make([]string, 0, len(roots))
	for _, r := range roots {
		if _, ok := g.nodes[r]; ok && !seen[r] {
			seen[r] = true
			stack = append(stack, r)
		}
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range g.out[id] {
			if follow(e) && !seen[e.To] {
				seen[e.To] = true
				stack = append(stack, e.To)
			}
		}
	}
	return seen
}

// Dependents returns every node that transitively depends on id, following
// edges backwards, excluding id itself.
func (g *Graph) Dependents(id string, follow func(Edge) bool) map[string]bool {
	seen := map[string]bool{id: true}
	stack := []string{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range g.in[cur] {
			if follow(e) && !seen[e.From] {
				seen[e.From] = true
				stack = append(stack, e.From)
			}
		}
	}
	delete(seen, id)
	return seen
}

func sortEdges(es []Edge) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].From != es[j].From {
			return es[i].From < es[j].From
		}
		if es[i].To != es[j].To {
			return es[i].To < es[j].To
		}
		return es[i].Kind < es[j].Kind
	})
}
