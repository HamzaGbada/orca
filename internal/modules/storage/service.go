// Package storage detects how the engine stores data on disk and measures
// it. Filesystem access is read-only and used for diagnostics only; Orca
// never deletes or modifies files under the data root (Safety Rule #3).
package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"orca/internal/extensions/engine"
	"orca/internal/extensions/fsmeasure"
)

// DiscrepancyThreshold is the relative difference between measured and
// API-reported usage above which the gap is reported as significant.
const DiscrepancyThreshold = 0.10

// Service inspects the engine's storage configuration and disk usage.
type Service struct {
	engine   engine.Engine
	measurer fsmeasure.Measurer
}

func NewService(e engine.Engine, m fsmeasure.Measurer) *Service {
	return &Service{engine: e, measurer: m}
}

// Report describes the engine's storage layout and usage.
type Report struct {
	Driver       string
	DriverStatus [][2]string
	DataRoot     string
	// Overlay2 is true for the classic overlay2 graph driver, whose layers
	// live in <DataRoot>/overlay2.
	Overlay2 bool
	// ContainerdSnapshotter is true when the containerd image store is used;
	// layers then live in containerd's snapshotter, not in <DataRoot>/overlay2.
	ContainerdSnapshotter bool
	Overlay2Dir           string

	// API is the engine's own accounting (`docker system df`).
	API engine.DiskUsage

	// Measured is set when Overlay2Dir was walked successfully.
	Measured *fsmeasure.Usage
	// MeasureSkipped explains why Overlay2Dir was not measured.
	MeasureSkipped string
}

// APITotal is the engine-reported usage expected to live in overlay2: image
// layers, container writable layers and build cache. It can overstate the
// real usage because build cache records may share storage with image layers.
func (r Report) APITotal() int64 {
	return r.API.ImagesSize + r.API.ContainersSize + r.API.BuildCacheSize
}

// Discrepancy returns measured apparent bytes minus APITotal, and that gap
// relative to APITotal. ok is false when nothing was measured.
func (r Report) Discrepancy() (diff int64, ratio float64, ok bool) {
	if r.Measured == nil {
		return 0, 0, false
	}
	diff = r.Measured.ApparentBytes - r.APITotal()
	if total := r.APITotal(); total > 0 {
		ratio = float64(diff) / float64(total)
	}
	return diff, ratio, true
}

// Inspect detects the storage driver and data root, then measures overlay2
// on disk when measure is true and the directory is readable on this host.
func (s *Service) Inspect(ctx context.Context, measure bool) (Report, error) {
	info, err := s.engine.Info(ctx)
	if err != nil {
		return Report{}, err
	}
	usage, err := s.engine.DiskUsage(ctx)
	if err != nil {
		return Report{}, err
	}

	r := Report{
		Driver:                info.StorageDriver,
		DriverStatus:          info.DriverStatus,
		DataRoot:              info.DataRoot,
		ContainerdSnapshotter: usesContainerdSnapshotter(info.DriverStatus),
		API:                   usage,
	}
	r.Overlay2 = r.Driver == "overlay2" && !r.ContainerdSnapshotter
	if r.DataRoot != "" {
		r.Overlay2Dir = filepath.Join(r.DataRoot, "overlay2")
	}

	switch {
	case !measure:
		r.MeasureSkipped = "measurement disabled"
	case !r.Overlay2:
		r.MeasureSkipped = fmt.Sprintf("storage driver %q is not the overlay2 graph driver", r.Driver)
	case !info.Local():
		r.MeasureSkipped = fmt.Sprintf("daemon %s is not local; its data root is not on this filesystem", info.Host)
	default:
		m, err := s.measurer.Measure(ctx, r.Overlay2Dir)
		switch {
		case err == nil:
			r.Measured = &m
		case errors.Is(err, fs.ErrPermission):
			r.MeasureSkipped = fmt.Sprintf("permission denied reading %s (run as root to measure)", r.Overlay2Dir)
		case errors.Is(err, fs.ErrNotExist):
			r.MeasureSkipped = fmt.Sprintf("%s does not exist on this host (daemon may run in a VM)", r.Overlay2Dir)
		default:
			return Report{}, fmt.Errorf("measure %s: %w", r.Overlay2Dir, err)
		}
	}
	return r, nil
}

// usesContainerdSnapshotter detects the containerd image store, which the
// daemon reports as DriverStatus ["driver-type", "io.containerd.snapshotter.v1"].
func usesContainerdSnapshotter(status [][2]string) bool {
	for _, kv := range status {
		if kv[0] == "driver-type" && strings.HasPrefix(kv[1], "io.containerd.snapshotter") {
			return true
		}
	}
	return false
}
