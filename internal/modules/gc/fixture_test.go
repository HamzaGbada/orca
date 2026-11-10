package gc

import (
	"time"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/layers"
	"github.com/HamzaGbada/orca/internal/modules/project"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func daysAgo(n float64) time.Time { return now.Add(-time.Duration(n * 24 * float64(time.Hour))) }

// diff returns a fake DiffID.
func diff(name string) string {
	h := "sha256:" + name
	for len(h) < 71 {
		h += "0"
	}
	return h
}

// fixture builds inventories through the same functions the collector uses.
type fixture struct {
	images     []engine.Image
	sets       []engine.ImageLayers
	containers []inventory.Container
	volumes    []engine.Volume
	cache      []inventory.CacheEntry
	sizes      map[string]int64 // by ChainID
	containerd bool
}

func newFixture() *fixture {
	return &fixture{sizes: map[string]int64{}}
}

type imgOpt func(*engine.Image)

func tags(t ...string) imgOpt       { return func(i *engine.Image) { i.RepoTags = t } }
func digest(d string) imgOpt        { return func(i *engine.Image) { i.RepoDigests = []string{d} } }
func created(t time.Time) imgOpt    { return func(i *engine.Image) { i.CreatedAt = t } }
func parent(id string) imgOpt       { return func(i *engine.Image) { i.ParentID = id } }
func imgLabels(kv ...string) imgOpt { return func(i *engine.Image) { i.Labels = kvMap(kv) } }
func sizes(size, shared int64) imgOpt {
	return func(i *engine.Image) { i.Size, i.SharedSize = size, shared }
}

// image adds an image made of the given layers (bottom first).
func (f *fixture) image(id string, layerNames []string, opts ...imgOpt) *fixture {
	img := engine.Image{ID: id, CreatedAt: daysAgo(60), Size: 0, SharedSize: 0}
	for _, o := range opts {
		o(&img)
	}
	var diffs []string
	for _, n := range layerNames {
		diffs = append(diffs, diff(n))
	}
	f.images = append(f.images, img)
	f.sets = append(f.sets, engine.ImageLayers{ImageID: id, DiffIDs: diffs})
	return f
}

// size records the size of the layer at the top of the given chain.
func (f *fixture) size(layerNames []string, n int64) *fixture {
	var diffs []string
	for _, l := range layerNames {
		diffs = append(diffs, diff(l))
	}
	c := layers.ChainIDs(diffs)
	f.sizes[c[len(c)-1]] = n
	return f
}

func (f *fixture) container(id, name, imageID, state string, lastUsed time.Time, mounts ...engine.Mount) *fixture {
	c := inventory.Container{
		Container: engine.Container{ID: id, Name: name, ImageID: imageID, ImageRef: imageID, State: state,
			CreatedAt: daysAgo(90), Mounts: mounts, SizeRw: 1000},
		Live:     state == "running" || state == "paused" || state == "restarting",
		LastUsed: lastUsed,
		Log:      inventory.Log{Size: -1},
	}
	if c.Live {
		c.LastUsed = now
	}
	f.containers = append(f.containers, c)
	return f
}

func (f *fixture) labelContainer(id string, kv ...string) *fixture {
	for i := range f.containers {
		if f.containers[i].ID == id {
			f.containers[i].Labels = kvMap(kv)
		}
	}
	return f
}

func (f *fixture) volume(name string, kv ...string) *fixture {
	f.volumes = append(f.volumes, engine.Volume{Name: name, Driver: "local", Mountpoint: "/var/lib/docker/volumes/" + name + "/_data",
		CreatedAt: daysAgo(30), Labels: kvMap(kv)})
	return f
}

func (f *fixture) externalVolume(name string) *fixture {
	f.volumes = append(f.volumes, engine.Volume{Name: name, Driver: "nfs", CreatedAt: daysAgo(30)})
	return f
}

func (f *fixture) buildCache(id string, size int64, lastUsed time.Time, parents ...string) *fixture {
	f.cache = append(f.cache, inventory.CacheEntry{
		CacheRecord: engine.CacheRecord{ID: id, Type: "regular", Size: size, CreatedAt: daysAgo(40), LastUsedAt: lastUsed, Parents: parents},
		Reclaimable: true,
	})
	return f
}

func volMount(name, dest string) engine.Mount {
	return engine.Mount{Type: engine.MountVolume, Name: name, Destination: dest}
}

func bindMount(src, dest string) engine.Mount {
	return engine.Mount{Type: engine.MountBind, Source: src, Destination: dest}
}

// build assembles the inventory like the collector does.
func (f *fixture) build() inventory.Inventory {
	idx := layers.BuildIndex(f.sets)
	for _, l := range idx.Layers {
		if s, ok := f.sizes[l.ChainID]; ok {
			l.Size = s
		}
	}
	var raw []engine.Container
	for i := range f.containers {
		f.containers[i].Project = project.Resolve(f.containers[i].Labels)
		raw = append(raw, f.containers[i].Container)
	}
	inv := inventory.Inventory{
		CollectedAt: now, Consistent: true, LayerSizes: "layerdb",
		Host:       inventory.Host{Address: "unix:///var/run/docker.sock", ServerVersion: "29.2.1", DataRoot: "/var/lib/docker", StorageDriver: "overlay2", ContainerdStore: f.containerd},
		Containers: f.containers,
		Layers:     idx.All(),
		BuildCache: f.cache,
	}
	for _, img := range discovery.ClassifyImages(f.images, raw) {
		inv.Images = append(inv.Images, inventory.Image{
			Image: img, Project: project.Resolve(img.Labels), Layers: idx.ImageLayers[img.ID], UniqueSize: img.Size - img.SharedSize,
		})
		inv.ImagesSize += img.Size - img.SharedSize
	}
	for _, l := range idx.All() {
		if l.Size > 0 && l.RefCount() > 1 {
			inv.ImagesSize += l.Size
		}
	}
	for _, v := range f.volumes {
		var mounts []volumes.Mount
		var users []string
		for _, c := range f.containers {
			for _, m := range c.Mounts {
				if m.Type == engine.MountVolume && m.Name == v.Name {
					mounts = append(mounts, volumes.Mount{ContainerID: c.ID, Destination: m.Destination, Live: c.Live})
					users = append(users, c.ID)
				}
			}
		}
		inv.Volumes = append(inv.Volumes, inventory.Volume{
			Volume: v, Classification: volumes.Classify(v, mounts, volumes.DefaultPolicy), Size: 500, Inodes: -1, Containers: users,
		})
	}
	for _, c := range f.containers {
		for _, m := range c.Mounts {
			if m.Type == engine.MountBind {
				inv.BindMounts = append(inv.BindMounts, inventory.BindMount{ContainerID: c.ID, Source: m.Source, Destination: m.Destination})
			}
		}
	}
	return inv
}

func kvMap(kv []string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// analyze runs the planner with a policy that disables the time-based
// review heuristic unless a test sets it.
func analyze(t interface{ Fatal(...any) }, inv inventory.Inventory, p Policy) *Analysis {
	a, err := Analyze(inv, p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (a *Analysis) cand(id string) *Candidate { return a.cands[id] }
