package docker

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/storage"
	"github.com/moby/moby/client"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
)

func (c *Client) Containers(ctx context.Context, withSize bool) ([]engine.Container, error) {
	res, err := c.api.ContainerList(ctx, client.ContainerListOptions{All: true, Size: withSize})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	out := make([]engine.Container, 0, len(res.Items))
	for _, s := range res.Items {
		ct := engine.Container{
			ID:         s.ID,
			ImageID:    s.ImageID,
			ImageRef:   s.Image,
			CreatedAt:  time.Unix(s.Created, 0),
			State:      string(s.State),
			Labels:     s.Labels,
			SizeRw:     -1,
			SizeRootFs: -1,
		}
		if len(s.Names) > 0 {
			ct.Name = strings.TrimPrefix(s.Names[0], "/")
		}
		if withSize {
			ct.SizeRw = s.SizeRw
			ct.SizeRootFs = s.SizeRootFs
		}
		for _, m := range s.Mounts {
			ct.Mounts = append(ct.Mounts, engine.Mount{
				Type:        string(m.Type),
				Name:        m.Name,
				Source:      m.Source,
				Destination: m.Destination,
				ReadOnly:    !m.RW,
			})
		}
		if s.NetworkSettings != nil {
			for name := range s.NetworkSettings.Networks {
				ct.Networks = append(ct.Networks, name)
			}
			sort.Strings(ct.Networks)
		}
		out = append(out, ct)
	}
	return out, nil
}

// Images lists all image records. All=true is required: with All=false the
// daemon hides untagged images that are the parent of another image, and
// those still hold layers on disk.
func (c *Client) Images(ctx context.Context) ([]engine.Image, error) {
	res, err := c.api.ImageList(ctx, client.ImageListOptions{All: true, SharedSize: true})
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	out := make([]engine.Image, 0, len(res.Items))
	for _, s := range res.Items {
		out = append(out, engine.Image{
			ID:          s.ID,
			ParentID:    s.ParentID,
			RepoTags:    realTags(s.RepoTags),
			RepoDigests: realDigests(s.RepoDigests),
			CreatedAt:   time.Unix(s.Created, 0),
			Size:        s.Size,
			SharedSize:  s.SharedSize,
			Labels:      s.Labels,
		})
	}
	return out, nil
}

// Older daemons report untagged images as "<none>:<none>" / "<none>@<none>".
func realTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		if t != "<none>:<none>" {
			out = append(out, t)
		}
	}
	return out
}

func realDigests(digests []string) []string {
	var out []string
	for _, d := range digests {
		if d != "<none>@<none>" {
			out = append(out, d)
		}
	}
	return out
}

func (c *Client) ImageLayers(ctx context.Context, imageID string) (engine.ImageLayers, error) {
	res, err := c.api.ImageInspect(ctx, imageID)
	if err != nil {
		return engine.ImageLayers{}, fmt.Errorf("inspect image %s: %w", imageID, notFound(err))
	}

	layers := engine.ImageLayers{
		ImageID:    res.ID,
		RootFSType: res.RootFS.Type,
		DiffIDs:    res.RootFS.Layers,
	}
	if res.GraphDriver != nil {
		layers.StorageDriver = res.GraphDriver.Name
		layers.StorageDirs = overlay2LayerDirs(res.GraphDriver, len(res.RootFS.Layers))
	}
	return layers, nil
}

// overlay2LayerDirs maps each layer (bottom first) to its overlay2/<cache-id>
// directory using the image's GraphDriver data:
//
//	UpperDir = <root>/overlay2/<cache-id of top layer>/diff
//	LowerDir = <dir of layer n-1>/diff:<dir of layer n-2>/diff:...:<dir of layer 1>/diff
//
// It returns nil when the driver is not overlay2 or the directory count does
// not match the layer count, so callers never get a wrong mapping.
func overlay2LayerDirs(gd *storage.DriverData, layerCount int) []string {
	if gd.Name != "overlay2" || layerCount == 0 {
		return nil
	}

	topDown := []string{gd.Data["UpperDir"]}
	if lower := gd.Data["LowerDir"]; lower != "" {
		topDown = append(topDown, strings.Split(lower, ":")...)
	}
	if len(topDown) != layerCount {
		return nil
	}

	dirs := make([]string, layerCount)
	for i, d := range topDown {
		if path.Base(d) != "diff" {
			return nil
		}
		dirs[layerCount-1-i] = path.Dir(d)
	}
	return dirs
}

func (c *Client) Volumes(ctx context.Context) ([]engine.Volume, error) {
	res, err := c.api.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}

	out := make([]engine.Volume, 0, len(res.Items))
	for _, v := range res.Items {
		// CreatedAt is RFC 3339; a missing or malformed value is left zero.
		created, _ := time.Parse(time.RFC3339, v.CreatedAt)
		out = append(out, engine.Volume{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Scope:      v.Scope,
			CreatedAt:  created,
			Labels:     v.Labels,
			Options:    v.Options,
		})
	}
	return out, nil
}

func (c *Client) Networks(ctx context.Context) ([]engine.Network, error) {
	res, err := c.api.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}

	out := make([]engine.Network, 0, len(res.Items))
	for _, n := range res.Items {
		out = append(out, engine.Network{
			ID:       n.ID,
			Name:     n.Name,
			Driver:   n.Driver,
			Scope:    n.Scope,
			Internal: n.Internal,
			Labels:   n.Labels,
		})
	}
	return out, nil
}

// notFound marks Docker "no such object" errors with engine.ErrNotFound.
func notFound(err error) error {
	if cerrdefs.IsNotFound(err) {
		return fmt.Errorf("%w: %v", engine.ErrNotFound, err)
	}
	return err
}

func (c *Client) ContainerDetails(ctx context.Context, containerID string) (engine.ContainerDetails, error) {
	res, err := c.api.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return engine.ContainerDetails{}, fmt.Errorf("inspect container %s: %w", containerID, notFound(err))
	}

	ct := res.Container
	d := engine.ContainerDetails{LogPath: ct.LogPath}
	if ct.State != nil {
		d.StartedAt = parseTime(ct.State.StartedAt)
		d.FinishedAt = parseTime(ct.State.FinishedAt)
	}
	if ct.HostConfig != nil {
		d.RestartPolicy = string(ct.HostConfig.RestartPolicy.Name)
		d.LogDriver = ct.HostConfig.LogConfig.Type
		d.LogOptions = ct.HostConfig.LogConfig.Config
	}
	return d, nil
}

// parseTime parses an RFC 3339 timestamp. Docker reports "never" as
// "0001-01-01T00:00:00Z", which parses to the zero time; malformed values
// are also left zero.
func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func (c *Client) VolumeUsage(ctx context.Context) (map[string]engine.VolumeUsage, error) {
	res, err := c.api.DiskUsage(ctx, client.DiskUsageOptions{Volumes: true, Verbose: true})
	if err != nil {
		return nil, fmt.Errorf("volume disk usage: %w", err)
	}

	out := make(map[string]engine.VolumeUsage, len(res.Volumes.Items))
	for _, v := range res.Volumes.Items {
		u := engine.VolumeUsage{Size: -1, RefCount: -1}
		if v.UsageData != nil {
			u = engine.VolumeUsage{Size: v.UsageData.Size, RefCount: v.UsageData.RefCount}
		}
		out[v.Name] = u
	}
	return out, nil
}

// BuildCache reads build cache records through the read-only disk usage
// endpoint. `docker builder prune` has no dry-run mode, so it is never used
// for inspection.
func (c *Client) BuildCache(ctx context.Context) ([]engine.CacheRecord, error) {
	res, err := c.api.DiskUsage(ctx, client.DiskUsageOptions{BuildCache: true, Verbose: true})
	if err != nil {
		return nil, fmt.Errorf("build cache disk usage: %w", err)
	}

	out := make([]engine.CacheRecord, 0, len(res.BuildCache.Items))
	for _, r := range res.BuildCache.Items {
		rec := engine.CacheRecord{
			ID:          r.ID,
			Parents:     r.Parents,
			Type:        r.Type,
			Description: r.Description,
			InUse:       r.InUse,
			Shared:      r.Shared,
			Size:        r.Size,
			CreatedAt:   r.CreatedAt,
			UsageCount:  r.UsageCount,
		}
		if r.LastUsedAt != nil {
			rec.LastUsedAt = *r.LastUsedAt
		}
		out = append(out, rec)
	}
	return out, nil
}
