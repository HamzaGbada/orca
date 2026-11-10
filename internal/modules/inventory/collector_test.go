package inventory

import (
	"context"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/extensions/fsmeasure"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/layers"
	"github.com/HamzaGbada/orca/internal/modules/pressure"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
)

var t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

const (
	base = "sha256:aaaa000000000000000000000000000000000000000000000000000000000000"
	app  = "sha256:bbbb000000000000000000000000000000000000000000000000000000000000"
)

// fakeEngine serves a fixed Docker state. containerCalls counts
// Containers() calls so tests can change state between them.
type fakeEngine struct {
	engine.Engine
	host           string
	containers     []engine.Container
	details        map[string]engine.ContainerDetails
	images         []engine.Image
	volumes        []engine.Volume
	cache          []engine.CacheRecord
	containerCalls int
	// changeAfter, when set, is applied to the state after each
	// Containers() call.
	changeAfter func(f *fakeEngine, call int)
	// inspectErr is returned by the first ContainerDetails call.
	inspectErr   error
	historyCalls int
}

func (f *fakeEngine) Info(context.Context) (engine.DaemonInfo, error) {
	return engine.DaemonInfo{Host: f.host, ServerVersion: "29.2.1", StorageDriver: "overlay2", DataRoot: "/var/lib/docker"}, nil
}

func (f *fakeEngine) Containers(context.Context, bool) ([]engine.Container, error) {
	f.containerCalls++
	out := append([]engine.Container(nil), f.containers...)
	if f.changeAfter != nil {
		f.changeAfter(f, f.containerCalls)
	}
	return out, nil
}

func (f *fakeEngine) ContainerDetails(_ context.Context, id string) (engine.ContainerDetails, error) {
	if err := f.inspectErr; err != nil {
		f.inspectErr = nil
		return engine.ContainerDetails{}, err
	}
	return f.details[id], nil
}

func (f *fakeEngine) Images(context.Context) ([]engine.Image, error) { return f.images, nil }

func (f *fakeEngine) ImageLayers(_ context.Context, id string) (engine.ImageLayers, error) {
	if id == "sha256:app" {
		return engine.ImageLayers{ImageID: id, DiffIDs: []string{base, app}}, nil
	}
	return engine.ImageLayers{ImageID: id, DiffIDs: []string{base}}, nil
}

func (f *fakeEngine) ImageHistory(_ context.Context, id string) ([]int64, error) {
	f.historyCalls++
	// app = base (100, shared) + app layer (200): history maps one to one.
	if id == "sha256:app" {
		return []int64{100, 0, 200}, nil
	}
	return []int64{100}, nil
}

func (f *fakeEngine) Volumes(context.Context) ([]engine.Volume, error) { return f.volumes, nil }

func (f *fakeEngine) VolumeUsage(context.Context) (map[string]engine.VolumeUsage, error) {
	out := map[string]engine.VolumeUsage{}
	for i, v := range f.volumes {
		out[v.Name] = engine.VolumeUsage{Size: int64(1000 * (i + 1)), RefCount: -1}
	}
	return out, nil
}

func (f *fakeEngine) Networks(context.Context) ([]engine.Network, error) { return nil, nil }

func (f *fakeEngine) BuildCache(context.Context) ([]engine.CacheRecord, error) { return f.cache, nil }

func (f *fakeEngine) ImagesSize(context.Context) (int64, error) { return 5000, nil }

type fakeFS struct {
	fsmeasure.Measurer
	err error // returned by every measurement
}

func (m fakeFS) MeasureFiles(_ context.Context, path string) (fsmeasure.Usage, error) {
	return fsmeasure.Usage{Path: path, ApparentBytes: 200_000_000}, m.err
}

func (m fakeFS) Measure(_ context.Context, path string) (fsmeasure.Usage, error) {
	return fsmeasure.Usage{Path: path, Files: 7, Dirs: 3}, m.err
}

func (m fakeFS) Capacity(_ context.Context, path string) (fsmeasure.Capacity, error) {
	return fsmeasure.Capacity{Path: path, TotalBytes: 100, UsedBytes: 90, AvailBytes: 10}, nil
}

func newFake() *fakeEngine {
	return &fakeEngine{
		host: "unix:///var/run/docker.sock",
		containers: []engine.Container{
			{ID: "web", Name: "web", ImageID: "sha256:app", State: "running", SizeRw: 10,
				Labels: map[string]string{"com.docker.compose.project": "shop"},
				Mounts: []engine.Mount{
					{Type: engine.MountVolume, Name: "db", Destination: "/data"},
					{Type: engine.MountBind, Source: "/home/me/src", Destination: "/src", ReadOnly: true},
				}},
			{ID: "old", Name: "old", ImageID: "sha256:base", State: "exited", SizeRw: 20},
		},
		details: map[string]engine.ContainerDetails{
			"web": {StartedAt: t0.Add(-time.Hour), RestartPolicy: "always", LogDriver: "json-file",
				LogPath: "/var/lib/docker/containers/web/web-json.log"},
			"old": {StartedAt: t0.Add(-48 * time.Hour), FinishedAt: t0.Add(-24 * time.Hour), LogDriver: "json-file",
				LogOptions: map[string]string{"max-size": "10m"}, LogPath: "/var/lib/docker/containers/old/old-json.log"},
		},
		images: []engine.Image{
			{ID: "sha256:app", RepoTags: []string{"shop:1"}, Size: 300, SharedSize: 100},
			{ID: "sha256:base", Size: 100, SharedSize: 100, Labels: map[string]string{"orca.gc.protected": "true"}},
		},
		volumes: []engine.Volume{
			{Name: "db", Driver: "local", Mountpoint: "/var/lib/docker/volumes/db/_data"},
			{Name: "scratch", Driver: "local", Labels: map[string]string{"orca.gc.temporary": "true"}},
		},
		cache: []engine.CacheRecord{
			{ID: "c1", Size: 50},
			{ID: "c2", Size: 70, InUse: true, Parents: []string{"c1"}},
			{ID: "c3", Size: 90, Shared: true},
		},
	}
}

func collector(e engine.Engine, m fsmeasure.Measurer) *Collector {
	c := NewCollector(e, m, nil, Options{Disk: pressure.DefaultThresholds, LargeLogBytes: 100_000_000, Volumes: volumes.DefaultPolicy})
	c.now = func() time.Time { return t0 }
	return c
}

func TestCollect(t *testing.T) {
	inv, err := collector(newFake(), fakeFS{}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Consistent || inv.Attempts != 1 || len(inv.Warnings) != 0 {
		t.Errorf("consistent=%t attempts=%d warnings=%v", inv.Consistent, inv.Attempts, inv.Warnings)
	}

	web, old := inv.Containers[0], inv.Containers[1]
	if !web.Live || !web.LastUsed.Equal(t0) || web.Project.Name != "shop" || web.RestartPolicy != "always" {
		t.Errorf("web container: %+v", web)
	}
	if !web.Log.Unlimited || web.Log.Size != 200_000_000 || !web.Log.Large {
		t.Errorf("web log: %+v, want unlimited, 200MB, large", web.Log)
	}
	if old.Live || !old.LastUsed.Equal(t0.Add(-24*time.Hour)) || old.Log.Unlimited {
		t.Errorf("old container: live=%t lastUsed=%s log=%+v", old.Live, old.LastUsed, old.Log)
	}

	appImg, baseImg := inv.Images[0], inv.Images[1]
	if appImg.UniqueSize != 200 || len(appImg.Layers) != 2 || appImg.Kind != discovery.Tagged {
		t.Errorf("app image: %+v", appImg)
	}
	if !baseImg.Protected || !reflect.DeepEqual(baseImg.Containers, []string{"old"}) {
		t.Errorf("base image: protected=%t containers=%v", baseImg.Protected, baseImg.Containers)
	}
	if len(inv.Layers) != 2 || inv.ImagesSize != 5000 {
		t.Errorf("layers=%d imagesSize=%d", len(inv.Layers), inv.ImagesSize)
	}

	db, scratch := inv.Volumes[0], inv.Volumes[1]
	if db.State != volumes.Referenced || !db.Root || !db.LastUsed.Equal(t0) || db.Size != 1000 || db.Inodes != 10 {
		t.Errorf("db volume: %+v", db)
	}
	if scratch.State != volumes.Candidate || !scratch.LastUsed.IsZero() {
		t.Errorf("scratch volume: %+v", scratch)
	}

	if want := []BindMount{{ContainerID: "web", Source: "/home/me/src", Destination: "/src", ReadOnly: true}}; !reflect.DeepEqual(inv.BindMounts, want) {
		t.Errorf("bind mounts = %+v", inv.BindMounts)
	}

	var reclaimable []string
	for _, e := range inv.BuildCache {
		if e.Reclaimable {
			reclaimable = append(reclaimable, e.ID)
		}
	}
	if !reflect.DeepEqual(reclaimable, []string{"c1"}) {
		t.Errorf("reclaimable cache = %v, want only c1 (c2 in use, c3 shared)", reclaimable)
	}

	if inv.Disk == nil || inv.Disk.UsedPercent != 90 || inv.Disk.Level != pressure.Critical {
		t.Errorf("disk = %+v, want 90%% CRITICAL", inv.Disk)
	}
}

func TestCollectRetriesWhenStateChanges(t *testing.T) {
	f := newFake()
	// Between the first snapshot and its verification, a container stops.
	f.changeAfter = func(f *fakeEngine, call int) {
		if call == 1 {
			f.containers[0].State = "exited"
		}
	}
	inv, err := collector(f, fakeFS{}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Consistent || inv.Attempts != 2 || inv.Containers[0].State != "exited" {
		t.Errorf("consistent=%t attempts=%d state=%s, want a consistent second snapshot",
			inv.Consistent, inv.Attempts, inv.Containers[0].State)
	}
}

func TestCollectGivesUpWhenStateKeepsChanging(t *testing.T) {
	f := newFake()
	f.changeAfter = func(f *fakeEngine, call int) {
		f.containers = append(f.containers, engine.Container{ID: fmt.Sprint("new", call), State: "created"})
	}
	inv, err := collector(f, fakeFS{}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Consistent || inv.Attempts != maxAttempts {
		t.Errorf("consistent=%t attempts=%d, want inconsistent after %d", inv.Consistent, inv.Attempts, maxAttempts)
	}
	if !strings.Contains(strings.Join(inv.Warnings, "\n"), "inconsistent") {
		t.Errorf("warnings %v should flag the inconsistency", inv.Warnings)
	}
}

func TestCollectRetriesWhenResourceDisappears(t *testing.T) {
	f := newFake()
	f.inspectErr = fmt.Errorf("inspect container web: %w", engine.ErrNotFound)
	inv, err := collector(f, fakeFS{}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Consistent || inv.Attempts != 2 {
		t.Errorf("consistent=%t attempts=%d, want recovery on the second attempt", inv.Consistent, inv.Attempts)
	}
}

func TestCollectWithoutRoot(t *testing.T) {
	inv, err := collector(newFake(), fakeFS{err: &fs.PathError{Op: "open", Path: "/var/lib/docker", Err: fs.ErrPermission}}).
		Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Containers[0].Log.Size != -1 || inv.Volumes[0].Inodes != -1 {
		t.Errorf("unmeasurable sizes must be -1, got log=%d inodes=%d", inv.Containers[0].Log.Size, inv.Volumes[0].Inodes)
	}
	joined := strings.Join(inv.Warnings, "\n")
	if strings.Count(joined, "run as root") != 2 {
		t.Errorf("want one deduplicated warning each for logs and inodes, got %v", inv.Warnings)
	}
}

func TestCollectRemoteDaemon(t *testing.T) {
	f := newFake()
	f.host = "tcp://10.0.0.5:2376"
	inv, err := collector(f, fakeFS{}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Disk != nil || inv.Containers[0].Log.Size != -1 || inv.Volumes[0].Inodes != -1 {
		t.Error("the local filesystem must not be measured for a remote daemon")
	}
	if !strings.Contains(strings.Join(inv.Warnings, "\n"), "not on this host") {
		t.Errorf("warnings %v should explain why disk pressure is missing", inv.Warnings)
	}
}

type fakeLayerDB struct {
	sizes map[string]int64
	err   error
}

func (f fakeLayerDB) LayerSizes(_ context.Context, _, _ string, ids []string) (map[string]int64, error) {
	out := map[string]int64{}
	for _, id := range ids {
		if s, ok := f.sizes[id]; ok {
			out[id] = s
		}
	}
	return out, f.err
}

func layerSize(inv Inventory, chainID string) int64 {
	for _, l := range inv.Layers {
		if l.ChainID == chainID {
			return l.Size
		}
	}
	return -2
}

func TestCollectInfersLayerSizesWithoutRoot(t *testing.T) {
	inv, err := collector(newFake(), fakeFS{}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	appChain := layers.ChainIDs([]string{base, app})[1]
	if inv.LayerSizes != "inferred" || layerSize(inv, base) != 100 || layerSize(inv, appChain) != 200 {
		t.Errorf("source=%s base=%d app=%d, want inferred/100/200", inv.LayerSizes, layerSize(inv, base), layerSize(inv, appChain))
	}
}

func TestCollectReadsLayerDB(t *testing.T) {
	f := newFake()
	appChain := layers.ChainIDs([]string{base, app})[1]
	c := collector(f, fakeFS{})
	c.ldb = fakeLayerDB{sizes: map[string]int64{base: 111, appChain: 222}}

	inv, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.LayerSizes != "layerdb" || layerSize(inv, base) != 111 || layerSize(inv, appChain) != 222 {
		t.Errorf("source=%s sizes=%d/%d, want layerdb 111/222", inv.LayerSizes, layerSize(inv, base), layerSize(inv, appChain))
	}
	if f.historyCalls != 0 {
		t.Errorf("history fetched %d times although the layer database sized every layer", f.historyCalls)
	}
}

func TestCollectLayerDBUnreadableFallsBack(t *testing.T) {
	c := collector(newFake(), fakeFS{})
	c.ldb = fakeLayerDB{err: &fs.PathError{Op: "open", Path: "/var/lib/docker/image", Err: fs.ErrPermission}}
	inv, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.LayerSizes != "inferred" || layerSize(inv, base) != 100 {
		t.Errorf("source=%s base=%d, want inference after a permission error", inv.LayerSizes, layerSize(inv, base))
	}
}
