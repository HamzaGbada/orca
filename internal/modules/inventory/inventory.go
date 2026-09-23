// Package inventory collects a single snapshot of every resource Docker
// manages: containers, images, layers, volumes, bind mounts, networks, build
// cache and logs, plus the disk pressure of the data root. It is read-only.
package inventory

import (
	"time"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/layers"
	"github.com/HamzaGbada/orca/internal/modules/pressure"
	"github.com/HamzaGbada/orca/internal/modules/project"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
)

// Inventory is one snapshot of Docker's resources.
type Inventory struct {
	CollectedAt time.Time
	// Consistent is true when no container, image or volume was added,
	// removed or changed state while the snapshot was taken.
	Consistent bool
	Attempts   int

	Host Host
	// Disk is the pressure on the filesystem holding the data root; nil
	// when it could not be read (see Warnings).
	Disk *pressure.Status
	// ImagesSize is the engine's total of image layer bytes, each layer
	// counted once (the "Images" size of `docker system df`).
	ImagesSize int64

	Containers []Container
	Images     []Image
	Layers     []*layers.Layer
	Volumes    []Volume
	BindMounts []BindMount
	Networks   []engine.Network
	BuildCache []CacheEntry

	// Warnings list data that could not be collected, e.g. sizes that need
	// root access. The rest of the inventory is still accurate.
	Warnings []string
}

type Host struct {
	ServerVersion string
	APIVersion    string
	StorageDriver string
	DataRoot      string
	LoggingDriver string
}

type Container struct {
	engine.Container
	Project project.Ref
	// Live is true for running, paused and restarting containers.
	Live       bool
	Protected  bool
	StartedAt  time.Time
	FinishedAt time.Time
	// LastUsed is the collection time for live containers, otherwise the
	// last start or stop time; zero if the container never ran.
	LastUsed      time.Time
	RestartPolicy string
	Log           Log
}

// Log describes a container's log storage.
type Log struct {
	Driver  string
	Options map[string]string
	// Path is the log file of the json-file driver.
	Path string
	// Size includes rotated files; -1 when not measured.
	Size int64
	// Unlimited is true when Docker stores the log on disk with no size cap.
	Unlimited bool
	// Large is true when Size is at least the configured threshold.
	Large bool
}

type Image struct {
	discovery.Image
	Project   project.Ref
	Protected bool
	// Layers are ChainIDs, bottom layer first.
	Layers []string
	// UniqueSize is the part of Size no other image shares; -1 when unknown.
	UniqueSize int64
}

type Volume struct {
	engine.Volume
	volumes.Classification
	// Size is computed by the engine; -1 when not available.
	Size int64
	// Inodes counts files and directories; -1 when not measured.
	Inodes int64
	// Containers mount this volume (running or stopped).
	Containers []string
	// LastUsed is the latest LastUsed of the containers mounting the
	// volume; zero when unused. Filesystem access times are not used: they
	// are unreliable under relatime/noatime mounts.
	LastUsed time.Time
}

// BindMount is a host path mounted into a container. The data belongs to
// the host, never to Docker.
type BindMount struct {
	ContainerID string
	Source      string
	Destination string
	ReadOnly    bool
}

type CacheEntry struct {
	engine.CacheRecord
	// Reclaimable is true when the record is neither in use nor shares
	// storage with images or other records.
	Reclaimable bool
}
