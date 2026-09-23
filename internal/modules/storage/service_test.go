package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/extensions/fsmeasure"
)

type fakeEngine struct {
	engine.Engine
	info  engine.DaemonInfo
	usage engine.DiskUsage
}

func (f fakeEngine) Info(context.Context) (engine.DaemonInfo, error) { return f.info, nil }

func (f fakeEngine) DiskUsage(context.Context) (engine.DiskUsage, error) { return f.usage, nil }

type fakeMeasurer struct {
	fsmeasure.Measurer
	usage fsmeasure.Usage
	err   error
	calls *int
}

func (f fakeMeasurer) Measure(_ context.Context, path string) (fsmeasure.Usage, error) {
	*f.calls++
	u := f.usage
	u.Path = path
	return u, f.err
}

var overlay2Info = engine.DaemonInfo{
	Host:          "unix:///var/run/docker.sock",
	StorageDriver: "overlay2",
	DriverStatus:  [][2]string{{"Backing Filesystem", "extfs"}},
	DataRoot:      "/var/lib/docker",
}

func TestInspectMeasuresOverlay2(t *testing.T) {
	calls := 0
	svc := NewService(
		fakeEngine{info: overlay2Info, usage: engine.DiskUsage{ImagesSize: 800, ContainersSize: 100, BuildCacheSize: 100}},
		fakeMeasurer{usage: fsmeasure.Usage{ApparentBytes: 1200}, calls: &calls},
	)

	r, err := svc.Inspect(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Overlay2 || r.ContainerdSnapshotter {
		t.Errorf("Overlay2=%t ContainerdSnapshotter=%t, want true/false", r.Overlay2, r.ContainerdSnapshotter)
	}
	if r.Overlay2Dir != "/var/lib/docker/overlay2" || r.Measured == nil || r.Measured.Path != r.Overlay2Dir {
		t.Fatalf("overlay2 dir not measured: %+v", r)
	}
	diff, ratio, ok := r.Discrepancy()
	if !ok || r.APITotal() != 1000 || diff != 200 || ratio != 0.2 {
		t.Errorf("APITotal=%d diff=%d ratio=%v ok=%t, want 1000/200/0.2/true", r.APITotal(), diff, ratio, ok)
	}
}

func TestInspectSkipsMeasurement(t *testing.T) {
	containerd := overlay2Info
	containerd.StorageDriver = "overlayfs"
	containerd.DriverStatus = [][2]string{{"driver-type", "io.containerd.snapshotter.v1"}}

	remote := overlay2Info
	remote.Host = "tcp://10.0.0.5:2376"

	tests := []struct {
		name    string
		info    engine.DaemonInfo
		measure bool
		err     error
		want    string
	}{
		{"disabled", overlay2Info, false, nil, "disabled"},
		{"containerd snapshotter", containerd, true, nil, "not the overlay2 graph driver"},
		{"remote daemon", remote, true, nil, "not local"},
		{"not root", overlay2Info, true, fmt.Errorf("open: %w", fs.ErrPermission), "run as root"},
		{"daemon in a VM", overlay2Info, true, fmt.Errorf("lstat: %w", fs.ErrNotExist), "does not exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			svc := NewService(fakeEngine{info: tt.info}, fakeMeasurer{err: tt.err, calls: &calls})

			r, err := svc.Inspect(context.Background(), tt.measure)
			if err != nil {
				t.Fatal(err)
			}
			if r.Measured != nil {
				t.Error("Measured should be nil")
			}
			if !strings.Contains(r.MeasureSkipped, tt.want) {
				t.Errorf("MeasureSkipped = %q, want it to contain %q", r.MeasureSkipped, tt.want)
			}
			if _, _, ok := r.Discrepancy(); ok {
				t.Error("Discrepancy must not be reported without a measurement")
			}
			if tt.err == nil && calls != 0 {
				t.Errorf("measurer called %d times, want 0", calls)
			}
		})
	}
}

func TestInspectMeasurementError(t *testing.T) {
	calls := 0
	svc := NewService(fakeEngine{info: overlay2Info}, fakeMeasurer{err: errors.New("io error"), calls: &calls})
	if _, err := svc.Inspect(context.Background(), true); err == nil {
		t.Error("unexpected measurement errors must be returned")
	}
}
