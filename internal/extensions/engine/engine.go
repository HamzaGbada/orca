// Package engine defines the contract Orca needs from a container engine.
//
// It contains only interfaces and plain data types. Business modules depend
// on this package, never on a concrete SDK; the Docker implementation lives in
// internal/drivers/docker, the only package allowed to import the Moby SDK.
//
// The contract is read-only on purpose: Sprint 0-2 must not be able to delete,
// modify or write anything in Docker-managed storage.
package engine

import (
	"context"
	"strings"
	"time"
)

// Engine is the read-only view of a container engine used by Orca modules.
type Engine interface {
	// Info returns daemon identity, version and storage configuration.
	Info(ctx context.Context) (DaemonInfo, error)

	// Containers lists all containers (running and stopped). When withSize is
	// true the engine computes writable-layer sizes, which can be slow.
	Containers(ctx context.Context, withSize bool) ([]Container, error)

	// Images lists every image record, including untagged intermediate ones.
	Images(ctx context.Context) ([]Image, error)

	// ImageLayers returns the layer identity data of a single image.
	ImageLayers(ctx context.Context, imageID string) (ImageLayers, error)

	// ContainerDetails returns the runtime details of one container that
	// the container list does not include.
	ContainerDetails(ctx context.Context, containerID string) (ContainerDetails, error)

	Volumes(ctx context.Context) ([]Volume, error)

	// VolumeUsage returns the size and reference count of local volumes, by
	// volume name. Sizing walks every volume and can be slow.
	VolumeUsage(ctx context.Context) (map[string]VolumeUsage, error)

	Networks(ctx context.Context) ([]Network, error)

	// BuildCache lists the build cache records.
	BuildCache(ctx context.Context) ([]CacheRecord, error)

	// DiskUsage returns the engine's own accounting of used disk space.
	// It sizes every container, which can take seconds.
	DiskUsage(ctx context.Context) (DiskUsage, error)

	// ImagesSize returns the total size of all image layers, each layer
	// counted once. It is the image part of DiskUsage, without sizing
	// containers.
	ImagesSize(ctx context.Context) (int64, error)

	Close() error
}

// DaemonInfo describes the connected daemon.
type DaemonInfo struct {
	Host          string
	ServerVersion string
	APIVersion    string // negotiated API version used by the client
	MaxAPIVersion string // highest API version the daemon supports
	OS            string
	OSType        string
	Architecture  string
	KernelVersion string
	CPUs          int
	MemoryBytes   int64
	StorageDriver string
	DriverStatus  [][2]string
	DataRoot      string
	LoggingDriver string
	Containers    int
	Running       int
	Paused        int
	Stopped       int
	Images        int
}

// Local reports whether the daemon runs on this host (Unix socket), so its
// data root is on this host's filesystem.
func (d DaemonInfo) Local() bool {
	return strings.HasPrefix(d.Host, "unix://")
}

type Container struct {
	ID         string
	Name       string
	ImageID    string // resolved image ID ("sha256:...")
	ImageRef   string // reference used at creation time (tag, or an ID if untagged)
	CreatedAt  time.Time
	State      string // created|running|paused|restarting|removing|exited|dead
	Labels     map[string]string
	Mounts     []Mount
	Networks   []string
	SizeRw     int64 // writable-layer size; -1 when not computed
	SizeRootFs int64 // writable layer + image layers; -1 when not computed
}

// Mount types as reported by the engine.
const (
	MountBind   = "bind"
	MountVolume = "volume"
	MountTmpfs  = "tmpfs"
)

type Mount struct {
	Type        string
	Name        string // volume name, empty for bind mounts
	Source      string
	Destination string
	ReadOnly    bool
}

type Image struct {
	ID          string
	ParentID    string // classic builder parent, empty when not exposed
	RepoTags    []string
	RepoDigests []string
	CreatedAt   time.Time
	Size        int64
	// SharedSize is the part of Size in layers other images also use;
	// -1 when unknown.
	SharedSize int64
	Labels     map[string]string
}

// ContainerDetails are container attributes only available by inspection.
type ContainerDetails struct {
	StartedAt     time.Time // zero if never started
	FinishedAt    time.Time // zero if never stopped
	RestartPolicy string    // no|always|unless-stopped|on-failure
	LogDriver     string
	LogOptions    map[string]string
	// LogPath is the log file of the json-file driver; empty for other drivers.
	LogPath string
}

// ImageLayers holds the layer identities of one image, bottom layer first.
type ImageLayers struct {
	ImageID    string
	RootFSType string
	// DiffIDs are the uncompressed layer digests from the image config.
	DiffIDs []string
	// StorageDriver and StorageDirs come from the engine's GraphDriver data.
	// For overlay2, StorageDirs[i] is the overlay2 directory the image's
	// overlay stack uses for DiffIDs[i]. The top entry is the layer's
	// cache-id; lower entries come from the top directory's physical lower
	// chain and, for layers built by BuildKit, can be a BuildKit snapshot
	// directory rather than the layer's cache-id. StorageDirs is empty when
	// the mapping is not exposed.
	StorageDriver string
	StorageDirs   []string
}

type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	Scope      string
	CreatedAt  time.Time
	Labels     map[string]string
	// Options are the driver options, e.g. {"type": "none", "o": "bind",
	// "device": "/srv/data"} for a local volume backed by a host directory.
	Options map[string]string
}

// VolumeUsage is the engine-computed usage of a volume.
type VolumeUsage struct {
	Size     int64 // bytes; -1 when not available (non-local drivers)
	RefCount int64 // containers referencing the volume; -1 when not available
}

// CacheRecord is one build cache record.
type CacheRecord struct {
	ID          string
	Parents     []string
	Type        string // regular|source.local|exec.cachemount|frontend|...
	Description string
	InUse       bool
	Shared      bool // shares storage with images or other records
	Size        int64
	CreatedAt   time.Time
	LastUsedAt  time.Time // zero if never used
	UsageCount  int
}

type Network struct {
	ID       string
	Name     string
	Driver   string
	Scope    string
	Internal bool
	Labels   map[string]string
}

// DiskUsage is the engine's own view of used disk space (`docker system df`).
type DiskUsage struct {
	ImagesSize     int64 // unique image layer bytes
	ContainersSize int64 // sum of container writable layers
	BuildCacheSize int64
}
