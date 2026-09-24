package graph

import (
	_ "embed"
	"encoding/json"
	"io"
	"strings"
)

//go:embed assets/graph.html
var htmlTemplate string

//go:embed assets/vis-network.min.js
var visNetworkJS string

// Meta describes where and when a graph was produced; the HTML view shows it.
type Meta struct {
	Host      string `json:",omitempty"` // daemon address
	Docker    string `json:",omitempty"` // Docker server version
	Generated string `json:",omitempty"` // RFC 3339 timestamp
	Orca      string `json:",omitempty"` // Orca version
}

// WriteHTML writes a self-contained interactive page (no network access
// needed) showing the graph.
//
// The graph is embedded as JSON inside a <script type="application/json">
// element. encoding/json escapes <, > and & as <, > and &, so
// resource names and labels cannot close the element or inject markup; the
// page inserts them into the DOM as text only.
func (g *Graph) WriteHTML(w io.Writer, m Meta) error {
	edges := g.Edges()
	if edges == nil {
		edges = []Edge{}
	}
	data, err := json.Marshal(struct {
		Meta  Meta
		Nodes []*Node
		Edges []Edge
	}{m, g.Nodes(), edges})
	if err != nil {
		return err
	}
	_, err = strings.NewReplacer(
		"{{ORCA_DATA}}", string(data),
		"{{VIS_NETWORK}}", visNetworkJS,
	).WriteString(w, htmlTemplate)
	return err
}
