package gc

import (
	"time"

	"github.com/HamzaGbada/orca/internal/modules/graph"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/layers"
	"github.com/HamzaGbada/orca/internal/modules/project"
	"github.com/HamzaGbada/orca/internal/shared/labels"
)

// Type is the kind of a resource in a plan.
type Type string

const (
	Container  Type = "container"
	Image      Type = "image"
	Volume     Type = "volume"
	BuildCache Type = "buildcache"
	BindMount  Type = "bindmount"
	Layer      Type = "layer"
	Log        Type = "log"
	Project    Type = "project"
)

// resource is the planner's view of one graph node.
type resource struct {
	id       string // graph node ID, e.g. "image:sha256:..."
	typ      Type
	name     string
	dockerID string // container/image ID, volume name, cache ID, host path, ChainID
	created  time.Time
	lastUsed time.Time // zero when unknown or never used
	size     int64     // total size; -1 when unknown
	project  project.Ref
	labels   map[string]string

	container *inventory.Container
	image     *inventory.Image
	volume    *inventory.Volume
	cache     *inventory.CacheEntry
	layer     *layers.Layer
}

// index holds every resource of an inventory by graph node ID, plus the
// derived facts the planner needs.
type index struct {
	inv      inventory.Inventory
	now      time.Time
	g        *graph.Graph
	res      map[string]*resource
	ids      []string             // sorted, for deterministic iteration
	projects map[string]time.Time // project -> last activity (zero if never active)
}

func buildIndex(inv inventory.Inventory, g *graph.Graph) *index {
	ix := &index{inv: inv, now: inv.CollectedAt, g: g, res: map[string]*resource{}, projects: map[string]time.Time{}}

	for i := range inv.Containers {
		c := &inv.Containers[i]
		ix.add(&resource{
			id: graph.NodeID(graph.Container, c.ID), typ: Container, name: c.Name, dockerID: c.ID,
			created: c.CreatedAt, lastUsed: c.LastUsed, size: c.SizeRw, project: c.Project, labels: c.Labels,
			container: c,
		})
		if c.Project.Name != "" {
			if _, ok := ix.projects[c.Project.Name]; !ok || c.LastUsed.After(ix.projects[c.Project.Name]) {
				ix.projects[c.Project.Name] = c.LastUsed
			}
		}
	}
	lastUse := map[string]time.Time{} // image ID -> latest use by a container
	for _, c := range inv.Containers {
		if c.LastUsed.After(lastUse[c.ImageID]) {
			lastUse[c.ImageID] = c.LastUsed
		}
	}
	for i := range inv.Images {
		img := &inv.Images[i]
		ix.add(&resource{
			id: graph.NodeID(graph.Image, img.ID), typ: Image, name: imageName(img), dockerID: img.ID,
			created: img.CreatedAt, lastUsed: lastUse[img.ID], size: img.Size, project: img.Project, labels: img.Labels,
			image: img,
		})
		ix.notePresence(img.Project.Name)
	}
	for i := range inv.Volumes {
		v := &inv.Volumes[i]
		ix.add(&resource{
			id: graph.NodeID(graph.Volume, v.Name), typ: Volume, name: v.Name, dockerID: v.Name,
			created: v.CreatedAt, lastUsed: v.LastUsed, size: v.Size, project: v.Project, labels: v.Labels,
			volume: v,
		})
		ix.notePresence(v.Project.Name)
	}
	for i := range inv.BuildCache {
		r := &inv.BuildCache[i]
		ix.add(&resource{
			id: graph.NodeID(graph.BuildCache, r.ID), typ: BuildCache, name: r.Type + " " + shortID(r.ID), dockerID: r.ID,
			created: r.CreatedAt, lastUsed: r.LastUsedAt, size: r.Size, cache: r,
		})
	}
	for _, l := range inv.Layers {
		ix.add(&resource{
			id: graph.NodeID(graph.Layer, l.ChainID), typ: Layer, name: shortID(l.ChainID), dockerID: l.ChainID,
			size: l.Size, layer: l,
		})
	}
	for _, b := range inv.BindMounts {
		id := graph.NodeID(graph.BindMount, b.Source)
		if _, ok := ix.res[id]; !ok {
			ix.add(&resource{id: id, typ: BindMount, name: b.Source, dockerID: b.Source, size: -1})
		}
	}
	for _, c := range inv.Containers {
		if c.Log.Path != "" {
			ix.add(&resource{id: graph.NodeID(graph.Log, c.ID), typ: Log, name: "log of " + c.Name, dockerID: c.Log.Path, size: c.Log.Size})
		}
	}
	for name := range ix.projects {
		ix.add(&resource{id: graph.NodeID(graph.Project, name), typ: Project, name: name, dockerID: name, size: -1})
	}

	for _, n := range g.Nodes() { // sorted by ID
		if _, ok := ix.res[n.ID]; ok {
			ix.ids = append(ix.ids, n.ID)
		}
	}
	return ix
}

func (ix *index) add(r *resource) { ix.res[r.id] = r }

// notePresence registers a project known only from images or volumes.
func (ix *index) notePresence(name string) {
	if _, ok := ix.projects[name]; name != "" && !ok {
		ix.projects[name] = time.Time{}
	}
}

// ageRef is the time a resource was last relevant: last use when known,
// otherwise creation.
func (r *resource) ageRef() time.Time {
	if !r.lastUsed.IsZero() {
		return r.lastUsed
	}
	return r.created
}

// labelProtected reports an explicit orca.gc.protected label.
func (r *resource) labelProtected() bool {
	return labels.Protective(r.labels, labels.Protected)
}

func imageName(img *inventory.Image) string {
	if len(img.RepoTags) > 0 {
		return img.RepoTags[0]
	}
	return "<none> " + shortID(img.ID)
}

func shortID(id string) string {
	if len(id) > 7 && id[:7] == "sha256:" {
		id = id[7:]
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
