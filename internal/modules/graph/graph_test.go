package graph

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/layers"
	"github.com/HamzaGbada/orca/internal/modules/project"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
)

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestAddEdge(t *testing.T) {
	g := New()
	a := g.AddNode(Container, "a", "a", -1)
	b := g.AddNode(Image, "b", "b", -1)
	if g.AddNode(Container, "a", "renamed", 5) != a {
		t.Error("AddNode must return the existing node")
	}
	if err := g.AddEdge(a.ID, "image:missing", Uses); err == nil {
		t.Error("edge to an unknown node must fail")
	}
	for i := 0; i < 2; i++ {
		if err := g.AddEdge(a.ID, b.ID, Uses); err != nil {
			t.Fatal(err)
		}
	}
	if len(g.Edges()) != 1 || g.RefCount(b.ID) != 1 || len(g.In(b.ID)) != 1 {
		t.Errorf("duplicate edges must be ignored: edges=%v", g.Edges())
	}
}

func TestTraversal(t *testing.T) {
	g := New()
	for _, n := range []struct {
		k  Kind
		id string
	}{{Project, "p"}, {Container, "c"}, {Image, "i"}, {Layer, "l1"}, {Layer, "l2"}, {Volume, "v"}, {Volume, "orphan"}} {
		g.AddNode(n.k, n.id, n.id, -1)
	}
	edges := []Edge{
		{"container:c", "image:i", Uses},
		{"image:i", "layer:l2", Contains},
		{"image:i", "layer:l1", Contains},
		{"layer:l2", "layer:l1", Parent},
		{"container:c", "volume:v", Mounts},
		{"project:p", "container:c", Member},
		{"project:p", "volume:orphan", Member},
	}
	for _, e := range edges {
		if err := g.AddEdge(e.From, e.To, e.Kind); err != nil {
			t.Fatal(err)
		}
	}

	got := keys(g.Reachable([]string{"container:c"}, Dependencies))
	want := []string{"container:c", "image:i", "layer:l1", "layer:l2", "volume:v"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Reachable = %v, want %v", got, want)
	}

	// Membership is not a dependency: reaching from the project with
	// Dependencies stays at the project.
	if got := keys(g.Reachable([]string{"project:p"}, Dependencies)); !reflect.DeepEqual(got, []string{"project:p"}) {
		t.Errorf("Reachable(project) = %v, membership must not be followed", got)
	}

	if got := keys(g.Dependents("layer:l1", Dependencies)); !reflect.DeepEqual(got, []string{"container:c", "image:i", "layer:l2"}) {
		t.Errorf("Dependents(l1) = %v", got)
	}
	if g.RefCount("container:c") != 0 || g.RefCount("layer:l1") != 2 {
		t.Errorf("RefCount(c)=%d RefCount(l1)=%d, want 0 and 2 (member edges excluded)",
			g.RefCount("container:c"), g.RefCount("layer:l1"))
	}
}

func sampleInventory() inventory.Inventory {
	const l1, l2 = "sha256:l1", "sha256:l2"
	return inventory.Inventory{
		Layers: []*layers.Layer{{ChainID: l1}, {ChainID: l2}},
		Images: []inventory.Image{
			{Image: discovery.Image{Image: engine.Image{ID: "sha256:app", RepoTags: []string{"shop:1"}, ParentID: "sha256:base", Size: 30}},
				Layers: []string{l1, l2}, Project: project.Ref{Name: "shop"}},
			{Image: discovery.Image{Image: engine.Image{ID: "sha256:base", Size: 10}}, Layers: []string{l1}},
		},
		Volumes: []inventory.Volume{{Volume: engine.Volume{Name: "db"}, Size: 7,
			Classification: volumes.Classification{Project: project.Ref{Name: "shop"}}}},
		Containers: []inventory.Container{{
			Container: engine.Container{ID: "web", Name: "web", ImageID: "sha256:app", SizeRw: 3, Mounts: []engine.Mount{
				{Type: engine.MountVolume, Name: "db"},
				{Type: engine.MountBind, Source: "/home/me/src"},
			}},
			Project: project.Ref{Name: "shop"},
			Log:     inventory.Log{Path: "/x-json.log", Size: 9},
		}},
		BuildCache: []inventory.CacheEntry{
			{CacheRecord: engine.CacheRecord{ID: "c1"}},
			{CacheRecord: engine.CacheRecord{ID: "c2", Parents: []string{"c1", "gone"}}},
		},
	}
}

func TestFromInventory(t *testing.T) {
	g, err := FromInventory(sampleInventory())
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, e := range g.Edges() {
		got = append(got, e.From+" -"+string(e.Kind)+"-> "+e.To)
	}
	want := []string{
		"buildcache:c2 -parent-> buildcache:c1",
		"container:web -mounts-> bindmount:/home/me/src",
		"container:web -writes-> log:web",
		"container:web -mounts-> volume:db",
		"container:web -uses-> image:sha256:app",
		"image:sha256:app -parent-> image:sha256:base",
		"image:sha256:app -contains-> layer:sha256:l1",
		"image:sha256:app -contains-> layer:sha256:l2",
		"image:sha256:base -contains-> layer:sha256:l1",
		"layer:sha256:l2 -parent-> layer:sha256:l1",
		"project:shop -member-> container:web",
		"project:shop -member-> image:sha256:app",
		"project:shop -member-> volume:db",
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("edges:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if n, _ := g.Node("log:web"); n == nil || n.Size != 9 {
		t.Errorf("log node = %+v", n)
	}
	reach := g.Reachable([]string{NodeID(Container, "web")}, Dependencies)
	if !reach["layer:sha256:l1"] || !reach["image:sha256:base"] || reach["buildcache:c1"] {
		t.Errorf("reachable from web = %v", keys(reach))
	}
}

func TestSerialization(t *testing.T) {
	g, err := FromInventory(sampleInventory())
	if err != nil {
		t.Fatal(err)
	}

	var a, b bytes.Buffer
	if err := g.WriteJSON(&a); err != nil {
		t.Fatal(err)
	}
	if err := g.WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Error("JSON output must be deterministic")
	}
	var decoded struct {
		Nodes []Node
		Edges []Edge
	}
	if err := json.Unmarshal(a.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Nodes) != len(g.Nodes()) || len(decoded.Edges) != len(g.Edges()) {
		t.Errorf("JSON round trip lost data")
	}

	var dot bytes.Buffer
	if err := g.WriteDOT(&dot); err != nil {
		t.Fatal(err)
	}
	out := dot.String()
	for _, want := range []string{"digraph orca {", `"container:web" -> "image:sha256:app" [label="uses"]`, "style=dashed"} {
		if !strings.Contains(out, want) {
			t.Errorf("DOT output missing %q", want)
		}
	}
}

func TestEmptyGraphJSON(t *testing.T) {
	var b bytes.Buffer
	if err := New().WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"Edges": []`) {
		t.Errorf("empty graph should serialize edges as [], got %s", b.String())
	}
}
