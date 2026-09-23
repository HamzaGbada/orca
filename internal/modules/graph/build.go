package graph

import (
	"strings"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
)

// FromInventory builds the resource graph of one inventory snapshot.
func FromInventory(inv inventory.Inventory) (*Graph, error) {
	g := New()
	b := builder{g: g}

	for _, l := range inv.Layers {
		g.AddNode(Layer, l.ChainID, short(l.ChainID), -1)
	}
	for _, img := range inv.Images {
		name := short(img.ID)
		if len(img.RepoTags) > 0 {
			name = img.RepoTags[0]
		}
		g.AddNode(Image, img.ID, name, img.Size)
		for i, chainID := range img.Layers {
			b.edge(NodeID(Image, img.ID), NodeID(Layer, chainID), Contains)
			if i > 0 {
				b.edge(NodeID(Layer, chainID), NodeID(Layer, img.Layers[i-1]), Parent)
			}
		}
		b.member(img.Project.Name, NodeID(Image, img.ID))
	}
	for _, img := range inv.Images {
		if _, ok := g.Node(NodeID(Image, img.ParentID)); ok {
			b.edge(NodeID(Image, img.ID), NodeID(Image, img.ParentID), Parent)
		}
	}

	for _, v := range inv.Volumes {
		g.AddNode(Volume, v.Name, v.Name, v.Size)
		b.member(v.Project.Name, NodeID(Volume, v.Name))
	}

	for _, ct := range inv.Containers {
		cid := NodeID(Container, ct.ID)
		g.AddNode(Container, ct.ID, ct.Name, ct.SizeRw)
		if _, ok := g.Node(NodeID(Image, ct.ImageID)); ok {
			b.edge(cid, NodeID(Image, ct.ImageID), Uses)
		}
		for _, m := range ct.Mounts {
			switch m.Type {
			case engine.MountVolume:
				b.edge(cid, NodeID(Volume, m.Name), Mounts)
			case engine.MountBind:
				g.AddNode(BindMount, m.Source, m.Source, -1)
				b.edge(cid, NodeID(BindMount, m.Source), Mounts)
			}
		}
		if ct.Log.Path != "" {
			g.AddNode(Log, ct.ID, "log of "+ct.Name, ct.Log.Size)
			b.edge(cid, NodeID(Log, ct.ID), Writes)
		}
		b.member(ct.Project.Name, cid)
	}

	for _, r := range inv.BuildCache {
		g.AddNode(BuildCache, r.ID, r.Type+" "+r.ID, r.Size)
	}
	for _, r := range inv.BuildCache {
		for _, p := range r.Parents {
			if _, ok := g.Node(NodeID(BuildCache, p)); ok {
				b.edge(NodeID(BuildCache, r.ID), NodeID(BuildCache, p), Parent)
			}
		}
	}

	return g, b.err
}

// builder records the first edge error so FromInventory reads linearly.
type builder struct {
	g   *Graph
	err error
}

func (b *builder) edge(from, to string, k EdgeKind) {
	if err := b.g.AddEdge(from, to, k); err != nil && b.err == nil {
		b.err = err
	}
}

func (b *builder) member(project, id string) {
	if project == "" {
		return
	}
	b.g.AddNode(Project, project, project, -1)
	b.edge(NodeID(Project, project), id, Member)
}

func short(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
