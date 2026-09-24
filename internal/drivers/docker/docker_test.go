package docker

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/moby/moby/api/types/storage"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
)

func TestOverlay2LayerDirs(t *testing.T) {
	const root = "/var/lib/docker/overlay2/"
	tests := []struct {
		name   string
		gd     storage.DriverData
		layers int
		want   []string
	}{
		{
			name: "three layers, bottom first",
			gd: storage.DriverData{Name: "overlay2", Data: map[string]string{
				"UpperDir": root + "top/diff",
				"LowerDir": root + "mid/diff:" + root + "bottom/diff",
			}},
			layers: 3,
			want:   []string{root + "bottom", root + "mid", root + "top"},
		},
		{
			name:   "single layer has no LowerDir",
			gd:     storage.DriverData{Name: "overlay2", Data: map[string]string{"UpperDir": root + "only/diff"}},
			layers: 1,
			want:   []string{root + "only"},
		},
		{
			name: "count mismatch yields no mapping",
			gd: storage.DriverData{Name: "overlay2", Data: map[string]string{
				"UpperDir": root + "top/diff",
				"LowerDir": root + "bottom/diff",
			}},
			layers: 3,
		},
		{
			name:   "other driver yields no mapping",
			gd:     storage.DriverData{Name: "btrfs", Data: map[string]string{"UpperDir": root + "x/diff"}},
			layers: 1,
		},
		{
			name:   "unexpected path layout yields no mapping",
			gd:     storage.DriverData{Name: "overlay2", Data: map[string]string{"UpperDir": root + "top/merged"}},
			layers: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := overlay2LayerDirs(&tt.gd, tt.layers); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConnectDaemonUnavailable(t *testing.T) {
	host := "unix://" + filepath.Join(t.TempDir(), "missing.sock")

	_, err := Connect(context.Background(), Target{URL: host}, time.Second)
	if !errors.Is(err, engine.ErrUnavailable) {
		t.Errorf("err = %v, want engine.ErrUnavailable", err)
	}
}

func TestConnectTimeout(t *testing.T) {
	// A socket that accepts connections but never answers.
	sock := filepath.Join(t.TempDir(), "silent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	start := time.Now()
	_, err = Connect(context.Background(), Target{URL: "unix://" + sock}, 200*time.Millisecond)
	if !errors.Is(err, engine.ErrUnavailable) {
		t.Errorf("err = %v, want engine.ErrUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Connect took %s, want it bounded by the timeout", elapsed)
	}
}

func TestConnectCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Connect(ctx, Target{URL: "unix://" + filepath.Join(t.TempDir(), "missing.sock")}, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
