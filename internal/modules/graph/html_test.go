package graph

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteHTML(t *testing.T) {
	g := New()
	// A hostile container name must not be able to close the data element.
	evil := `</script><script>alert(1)</script><!--`
	g.AddNode(Container, "c1", evil, 10).Attrs = map[string]string{"state": "running", "note": "a & b <b>"}
	g.AddNode(Image, "i1", "app:1", 20)
	if err := g.AddEdge("container:c1", "image:i1", Uses); err != nil {
		t.Fatal(err)
	}

	var b bytes.Buffer
	if err := g.WriteHTML(&b, Meta{Host: "unix:///var/run/docker.sock", Orca: "0.1.0"}); err != nil {
		t.Fatal(err)
	}
	page := b.String()

	if strings.Contains(page, evil) {
		t.Error("untrusted labels must be escaped in the page")
	}
	if n := strings.Count(page, "</script>"); n != 3 {
		t.Errorf("page has %d </script> tags, want exactly 3 (data, library, app)", n)
	}
	for _, placeholder := range []string{"{{ORCA_DATA}}", "{{VIS_NETWORK}}"} {
		if strings.Contains(page, placeholder) {
			t.Errorf("placeholder %s was not replaced", placeholder)
		}
	}
	if !strings.Contains(page, "vis-network") || !strings.Contains(page, "@license") {
		t.Error("the page must inline vis-network with its license header")
	}

	// The embedded data round-trips.
	start := strings.Index(page, `type="application/json">`) + len(`type="application/json">`)
	end := strings.Index(page[start:], "</script>")
	if raw := page[start : start+end]; strings.ContainsAny(raw, "<>") {
		t.Errorf("the data block must contain no raw < or >: %q", raw)
	}
	var data struct {
		Meta  Meta
		Nodes []Node
		Edges []Edge
	}
	if err := json.Unmarshal([]byte(page[start:start+end]), &data); err != nil {
		t.Fatalf("embedded data is not valid JSON: %v", err)
	}
	if len(data.Nodes) != 2 || len(data.Edges) != 1 || data.Meta.Orca != "0.1.0" {
		t.Errorf("embedded data = %+v", data)
	}
	if data.Nodes[0].Label != evil || data.Nodes[0].Attrs["note"] != "a & b <b>" {
		t.Error("escaping must not alter the data itself")
	}
}

func TestVisNetworkCanBeInlined(t *testing.T) {
	if strings.Contains(strings.ToLower(visNetworkJS), "</script") {
		t.Fatal("vis-network.min.js contains </script and cannot be inlined")
	}
	if len(visNetworkJS) < 100_000 {
		t.Fatalf("vis-network.min.js looks truncated (%d bytes)", len(visNetworkJS))
	}
}
