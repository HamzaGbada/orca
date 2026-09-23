package inventory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/extensions/fsmeasure"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/layers"
	"github.com/HamzaGbada/orca/internal/modules/pressure"
	"github.com/HamzaGbada/orca/internal/modules/project"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
	"github.com/HamzaGbada/orca/internal/shared/labels"
)

// maxAttempts bounds how often a snapshot is retaken when Docker state
// changes during collection.
const maxAttempts = 3

// Options configure the collector.
type Options struct {
	Disk          pressure.Thresholds
	LargeLogBytes int64
	Volumes       volumes.Policy
}

// Collector produces inventory snapshots.
type Collector struct {
	engine engine.Engine
	fs     fsmeasure.Measurer
	opts   Options
	now    func() time.Time
}

func NewCollector(e engine.Engine, m fsmeasure.Measurer, opts Options) *Collector {
	return &Collector{engine: e, fs: m, opts: opts, now: time.Now}
}

// Collect takes one snapshot. Docker has no transactional API, so the
// collector lists containers, images and volumes again at the end and
// retakes the snapshot when they changed. After maxAttempts it returns the
// last snapshot with Consistent=false and a warning.
func (c *Collector) Collect(ctx context.Context) (Inventory, error) {
	var inv Inventory
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var err error
		inv, err = c.collectOnce(ctx)
		if errors.Is(err, engine.ErrNotFound) && attempt < maxAttempts {
			continue // a resource disappeared mid-collection
		}
		if err != nil {
			return Inventory{}, err
		}
		inv.Attempts = attempt

		after, err := c.fingerprint(ctx)
		if err != nil {
			return Inventory{}, err
		}
		if after == fingerprintOf(inv) {
			inv.Consistent = true
			return inv, nil
		}
	}
	inv.Warnings = append(inv.Warnings,
		fmt.Sprintf("Docker state kept changing during %d collection attempts; the snapshot may be inconsistent", maxAttempts))
	return inv, nil
}

func (c *Collector) collectOnce(ctx context.Context) (Inventory, error) {
	inv := Inventory{CollectedAt: c.now()}
	w := &warnings{}

	info, err := c.engine.Info(ctx)
	if err != nil {
		return inv, err
	}
	inv.Host = Host{
		ServerVersion: info.ServerVersion,
		APIVersion:    info.APIVersion,
		StorageDriver: info.StorageDriver,
		DataRoot:      info.DataRoot,
		LoggingDriver: info.LoggingDriver,
	}

	rawContainers, err := c.engine.Containers(ctx, true)
	if err != nil {
		return inv, err
	}
	rawImages, err := c.engine.Images(ctx)
	if err != nil {
		return inv, err
	}
	rawVolumes, err := c.engine.Volumes(ctx)
	if err != nil {
		return inv, err
	}
	if inv.Networks, err = c.engine.Networks(ctx); err != nil {
		return inv, err
	}
	volumeUsage, err := c.engine.VolumeUsage(ctx)
	if err != nil {
		return inv, err
	}
	cache, err := c.engine.BuildCache(ctx)
	if err != nil {
		return inv, err
	}
	if inv.ImagesSize, err = c.engine.ImagesSize(ctx); err != nil {
		return inv, err
	}

	if inv.Containers, err = c.containers(ctx, rawContainers, info.Local(), inv.CollectedAt, w); err != nil {
		return inv, err
	}
	if inv.Images, inv.Layers, err = c.images(ctx, rawImages, rawContainers); err != nil {
		return inv, err
	}
	inv.Volumes = c.volumes(ctx, rawVolumes, volumeUsage, inv.Containers, info.Local(), w)
	inv.BindMounts = bindMounts(inv.Containers)
	for _, r := range cache {
		inv.BuildCache = append(inv.BuildCache, CacheEntry{CacheRecord: r, Reclaimable: !r.InUse && !r.Shared})
	}

	switch {
	case !info.Local():
		w.add(fmt.Sprintf("disk pressure not measured: daemon %s is not on this host", info.Host))
	default:
		capacity, err := c.fs.Capacity(ctx, info.DataRoot)
		if err != nil {
			w.add(fmt.Sprintf("disk pressure not measured: %v", err))
		} else {
			st := pressure.Evaluate(capacity, c.opts.Disk)
			inv.Disk = &st
		}
	}

	inv.Warnings = w.list
	return inv, nil
}

func (c *Collector) containers(ctx context.Context, raw []engine.Container, local bool, now time.Time, w *warnings) ([]Container, error) {
	out := make([]Container, 0, len(raw))
	for _, rc := range raw {
		d, err := c.engine.ContainerDetails(ctx, rc.ID)
		if err != nil {
			return nil, err
		}
		ct := Container{
			Container:     rc,
			Project:       project.Resolve(rc.Labels),
			Live:          discovery.IsLive(rc),
			Protected:     labels.Protective(rc.Labels, labels.Protected),
			StartedAt:     d.StartedAt,
			FinishedAt:    d.FinishedAt,
			RestartPolicy: d.RestartPolicy,
			Log: Log{
				Driver:    d.LogDriver,
				Options:   d.LogOptions,
				Path:      d.LogPath,
				Size:      -1,
				Unlimited: unlimitedLog(d.LogDriver, d.LogOptions),
			},
		}
		ct.LastUsed = latest(d.StartedAt, d.FinishedAt)
		if ct.Live {
			ct.LastUsed = now
		}
		c.measureLog(ctx, &ct.Log, local, w)
		out = append(out, ct)
	}
	return out, nil
}

// unlimitedLog reports whether Docker keeps the log on disk without a size
// cap: json-file without max-size (or max-size=-1). The local driver rotates
// by default; other drivers ship logs off the host.
func unlimitedLog(driver string, opts map[string]string) bool {
	if driver != "json-file" {
		return false
	}
	maxSize := opts["max-size"]
	return maxSize == "" || maxSize == "-1"
}

func (c *Collector) measureLog(ctx context.Context, l *Log, local bool, w *warnings) {
	if l.Path == "" || !local {
		return
	}
	u, err := c.fs.MeasureFiles(ctx, l.Path)
	switch {
	case err == nil:
		l.Size = u.ApparentBytes
		l.Large = l.Size >= c.opts.LargeLogBytes
	case errors.Is(err, fs.ErrNotExist):
		l.Size = 0 // nothing logged yet
	case errors.Is(err, fs.ErrPermission):
		w.add("container log sizes not measured: permission denied (run as root)")
	default:
		w.add(fmt.Sprintf("container log size not measured: %v", err))
	}
}

func (c *Collector) images(ctx context.Context, raw []engine.Image, containers []engine.Container) ([]Image, []*layers.Layer, error) {
	sets := make([]engine.ImageLayers, 0, len(raw))
	for _, img := range raw {
		set, err := c.engine.ImageLayers(ctx, img.ID)
		if err != nil {
			return nil, nil, err
		}
		sets = append(sets, set)
	}
	idx := layers.BuildIndex(sets)

	out := make([]Image, 0, len(raw))
	for _, img := range discovery.ClassifyImages(raw, containers) {
		unique := int64(-1)
		if img.SharedSize >= 0 {
			unique = img.Size - img.SharedSize
		}
		out = append(out, Image{
			Image:      img,
			Project:    project.Resolve(img.Labels),
			Protected:  labels.Protective(img.Labels, labels.Protected),
			Layers:     idx.ImageLayers[img.ID],
			UniqueSize: unique,
		})
	}
	return out, idx.All(), nil
}

func (c *Collector) volumes(ctx context.Context, raw []engine.Volume, usage map[string]engine.VolumeUsage,
	containers []Container, local bool, w *warnings) []Volume {
	mounts := make(map[string][]volumes.Mount)
	lastUsed := make(map[string]time.Time)
	for _, ct := range containers {
		for _, m := range ct.Mounts {
			if m.Type != engine.MountVolume {
				continue
			}
			mounts[m.Name] = append(mounts[m.Name], volumes.Mount{
				ContainerID: ct.ID, Destination: m.Destination, Live: ct.Live,
			})
			lastUsed[m.Name] = latest(lastUsed[m.Name], ct.LastUsed)
		}
	}

	out := make([]Volume, 0, len(raw))
	for _, v := range raw {
		vol := Volume{
			Volume:         v,
			Classification: volumes.Classify(v, mounts[v.Name], c.opts.Volumes),
			Size:           -1,
			Inodes:         -1,
			LastUsed:       lastUsed[v.Name],
		}
		if u, ok := usage[v.Name]; ok {
			vol.Size = u.Size
		}
		for _, m := range mounts[v.Name] {
			vol.Containers = append(vol.Containers, m.ContainerID)
		}
		sort.Strings(vol.Containers)
		if local && v.Driver == "local" && vol.Kind != volumes.External {
			c.measureInodes(ctx, &vol, w)
		}
		out = append(out, vol)
	}
	return out
}

func (c *Collector) measureInodes(ctx context.Context, v *Volume, w *warnings) {
	u, err := c.fs.Measure(ctx, v.Mountpoint)
	switch {
	case err == nil:
		v.Inodes = u.Files + u.Dirs
	case errors.Is(err, fs.ErrPermission):
		w.add("volume inode counts not measured: permission denied (run as root)")
	default:
		w.add(fmt.Sprintf("volume inode count not measured: %v", err))
	}
}

func bindMounts(containers []Container) []BindMount {
	var out []BindMount
	for _, ct := range containers {
		for _, m := range ct.Mounts {
			if m.Type == engine.MountBind {
				out = append(out, BindMount{
					ContainerID: ct.ID, Source: m.Source, Destination: m.Destination, ReadOnly: m.ReadOnly,
				})
			}
		}
	}
	return out
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// warnings collects distinct warning messages in order.
type warnings struct{ list []string }

func (w *warnings) add(msg string) {
	for _, existing := range w.list {
		if existing == msg {
			return
		}
	}
	w.list = append(w.list, msg)
}

// fingerprint identifies the set of containers (with state), images and
// volumes, to detect changes during collection.
func (c *Collector) fingerprint(ctx context.Context) (string, error) {
	cs, err := c.engine.Containers(ctx, false)
	if err != nil {
		return "", err
	}
	imgs, err := c.engine.Images(ctx)
	if err != nil {
		return "", err
	}
	vols, err := c.engine.Volumes(ctx)
	if err != nil {
		return "", err
	}

	var ids []string
	for _, ct := range cs {
		ids = append(ids, "c:"+ct.ID+":"+ct.State)
	}
	for _, img := range imgs {
		ids = append(ids, "i:"+img.ID)
	}
	for _, v := range vols {
		ids = append(ids, "v:"+v.Name)
	}
	return joinSorted(ids), nil
}

func fingerprintOf(inv Inventory) string {
	var ids []string
	for _, ct := range inv.Containers {
		ids = append(ids, "c:"+ct.ID+":"+ct.State)
	}
	for _, img := range inv.Images {
		ids = append(ids, "i:"+img.ID)
	}
	for _, v := range inv.Volumes {
		ids = append(ids, "v:"+v.Name)
	}
	return joinSorted(ids)
}

func joinSorted(ids []string) string {
	sort.Strings(ids)
	return strings.Join(ids, "\n")
}
