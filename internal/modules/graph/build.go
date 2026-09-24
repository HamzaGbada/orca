package graph

import (
	"fmt"
	"strings"
	"time"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
)

// FromInventory builds the resource graph of one inventory snapshot.
func FromInventory(inv inventory.Inventory) (*Graph, error) {
	g := New()
	b := builder{g: g}

	for _, l := range inv.Layers {
		g.AddNode(Layer, l.ChainID, short(l.ChainID), -1).Attrs = attrs(
			"chain id", l.ChainID,
			"diff id", l.DiffID,
			"depth", fmt.Sprint(l.Depth),
			"used by images", fmt.Sprint(l.RefCount()),
			"overlay2 dirs", strings.Join(l.StorageDirs, "\n"),
		)
	}
	for _, img := range inv.Images {
		name := short(img.ID)
		if len(img.RepoTags) > 0 {
			name = img.RepoTags[0]
		}
		g.AddNode(Image, img.ID, name, img.Size).Attrs = attrs(
			"id", img.ID,
			"kind", string(img.Kind),
			"tags", strings.Join(img.RepoTags, ", "),
			"created", timeAttr(img.CreatedAt),
			"unique size", sizeAttr(img.UniqueSize),
			"containers", fmt.Sprint(len(img.Containers)),
			"project", img.Project.Name,
			"protected", yes(img.Protected),
		)
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
		g.AddNode(Volume, v.Name, v.Name, v.Size).Attrs = attrs(
			"state", string(v.State),
			"kind", string(v.Kind),
			"why", strings.Join(v.Reasons, "; "),
			"driver", v.Driver,
			"mountpoint", v.Mountpoint,
			"created", timeAttr(v.CreatedAt),
			"last used", timeAttr(v.LastUsed),
			"project", v.Project.Name,
		)
		b.member(v.Project.Name, NodeID(Volume, v.Name))
	}

	for _, ct := range inv.Containers {
		cid := NodeID(Container, ct.ID)
		g.AddNode(Container, ct.ID, ct.Name, ct.SizeRw).Attrs = attrs(
			"id", ct.ID,
			"state", ct.State,
			"image", ct.ImageRef,
			"created", timeAttr(ct.CreatedAt),
			"last used", timeAttr(ct.LastUsed),
			"restart policy", ct.RestartPolicy,
			"log driver", ct.Log.Driver,
			"project", ct.Project.Name,
			"protected", yes(ct.Protected),
		)
		if _, ok := g.Node(NodeID(Image, ct.ImageID)); ok {
			b.edge(cid, NodeID(Image, ct.ImageID), Uses)
		}
		for _, m := range ct.Mounts {
			switch m.Type {
			case engine.MountVolume:
				b.edge(cid, NodeID(Volume, m.Name), Mounts)
			case engine.MountBind:
				g.AddNode(BindMount, m.Source, m.Source, -1).Attrs = attrs(
					"host path", m.Source,
					"owner", "host (never deleted by Orca)",
				)
				b.edge(cid, NodeID(BindMount, m.Source), Mounts)
			}
		}
		if ct.Log.Path != "" {
			g.AddNode(Log, ct.ID, "log of "+ct.Name, ct.Log.Size).Attrs = attrs(
				"path", ct.Log.Path,
				"driver", ct.Log.Driver,
				"unlimited growth", yes(ct.Log.Unlimited),
				"large", yes(ct.Log.Large),
			)
			b.edge(cid, NodeID(Log, ct.ID), Writes)
		}
		b.member(ct.Project.Name, cid)
	}

	for _, r := range inv.BuildCache {
		g.AddNode(BuildCache, r.ID, r.Type+" "+r.ID, r.Size).Attrs = attrs(
			"type", r.Type,
			"description", r.Description,
			"in use", yes(r.InUse),
			"shared", yes(r.Shared),
			"reclaimable", yes(r.Reclaimable),
			"created", timeAttr(r.CreatedAt),
			"last used", timeAttr(r.LastUsedAt),
			"usage count", fmt.Sprint(r.UsageCount),
		)
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

// attrs builds an attribute map from key/value pairs, skipping empty values.
func attrs(kv ...string) map[string]string {
	m := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			m[kv[i]] = kv[i+1]
		}
	}
	return m
}

func timeAttr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func sizeAttr(b int64) string {
	if b < 0 {
		return ""
	}
	return fmt.Sprint(b)
}

func yes(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

func short(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
