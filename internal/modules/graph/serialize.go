package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteJSON writes {"Nodes": [...], "Edges": [...]}, sorted for stable
// output.
func (g *Graph) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	nodes := g.Nodes()
	edges := g.Edges()
	if edges == nil {
		edges = []Edge{}
	}
	return enc.Encode(struct {
		Nodes []*Node
		Edges []Edge
	}{nodes, edges})
}

var dotShapes = map[Kind]string{
	Project:    "folder",
	Container:  "box",
	Image:      "component",
	Layer:      "cylinder",
	Volume:     "cylinder",
	BindMount:  "tab",
	Log:        "note",
	BuildCache: "box3d",
}

// WriteDOT writes the graph in Graphviz DOT format, e.g. for
// `orca graph | dot -Tsvg > graph.svg`.
func (g *Graph) WriteDOT(w io.Writer) error {
	var b strings.Builder
	b.WriteString("digraph orca {\n  rankdir=LR;\n  node [fontsize=10];\n")
	for _, n := range g.Nodes() {
		fmt.Fprintf(&b, "  %q [label=%q, shape=%s];\n", n.ID, string(n.Kind)+"\n"+n.Label, dotShapes[n.Kind])
	}
	for _, e := range g.Edges() {
		style := ""
		if e.Kind == Member {
			style = ", style=dashed"
		}
		fmt.Fprintf(&b, "  %q -> %q [label=%q%s];\n", e.From, e.To, string(e.Kind), style)
	}
	b.WriteString("}\n")
	_, err := io.WriteString(w, b.String())
	return err
}
