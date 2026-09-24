//go:build integration

// Package integration checks Orca against a real Docker daemon, comparing its
// results with the docker CLI. Run with:
//
//	go test -tags integration ./tests/integration/
package integration

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HamzaGbada/orca/internal/config"
	"github.com/HamzaGbada/orca/internal/drivers/docker"
	"github.com/HamzaGbada/orca/internal/drivers/filesystem"
	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/graph"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/layers"
)

func connect(t *testing.T) engine.Engine {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := docker.Connect(ctx, docker.Target{}, 5*time.Second)
	if err != nil {
		t.Skipf("no Docker daemon: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// dockerIDs runs a docker CLI listing command and returns the sorted, unique IDs.
func dockerIDs(t *testing.T, args ...string) []string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not installed")
	}
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		t.Fatalf("docker %v: %v", args, err)
	}
	return uniqueSorted(strings.Fields(string(out)))
}

func uniqueSorted(ids []string) []string {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: orca found %d, docker CLI %d", what, len(got), len(want))
	}
}

func TestInfo(t *testing.T) {
	info, err := connect(t).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.ServerVersion == "" || info.APIVersion == "" || info.StorageDriver == "" || info.DataRoot == "" {
		t.Errorf("incomplete daemon info: %+v", info)
	}
}

func TestContainersMatchCLI(t *testing.T) {
	svc := discovery.NewService(connect(t))
	ctx := context.Background()

	for _, tt := range []struct {
		state discovery.ContainerState
		args  []string
	}{
		{discovery.AllContainers, []string{"ps", "-aq", "--no-trunc"}},
		{discovery.RunningContainers, []string{"ps", "-q", "--no-trunc"}},
	} {
		cs, err := svc.Containers(ctx, tt.state, false)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, c := range cs {
			ids = append(ids, c.ID)
		}
		equal(t, "containers "+strings.Join(tt.args, " "), uniqueSorted(ids), dockerIDs(t, tt.args...))
	}
}

func TestImagesMatchCLI(t *testing.T) {
	imgs, err := discovery.NewService(connect(t)).Images(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var all, dangling []string
	for _, img := range imgs {
		all = append(all, img.ID)
		if img.Kind == discovery.Dangling {
			dangling = append(dangling, img.ID)
		}
	}
	equal(t, "all images", uniqueSorted(all), dockerIDs(t, "images", "-aq", "--no-trunc"))
	equal(t, "dangling images", uniqueSorted(dangling),
		dockerIDs(t, "images", "-q", "--no-trunc", "--filter", "dangling=true"))
}

func TestLayerIndex(t *testing.T) {
	snap, err := layers.NewService(connect(t)).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Images) == 0 {
		t.Skip("no images on this host")
	}
	for _, img := range snap.Images {
		if _, ok := snap.Index.ImageLayers[img.ID]; !ok {
			t.Errorf("image %s missing from the layer index", img.ID)
		}
	}
	t.Logf("%d images, %d unique layers, %d shared, %d stored in more than one directory",
		len(snap.Images), len(snap.Index.Layers), len(snap.Index.Shared()), len(snap.Index.MultiDir()))
}

func collect(t *testing.T) inventory.Inventory {
	t.Helper()
	cfg := config.Default()
	c := inventory.NewCollector(connect(t), filesystem.Measurer{}, inventory.Options{
		Disk: cfg.Disk, LargeLogBytes: cfg.LargeLogBytes, Volumes: cfg.Volumes,
	})
	inv, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestInventoryMatchesCLI(t *testing.T) {
	inv := collect(t)
	if !inv.Consistent {
		t.Logf("snapshot not consistent (Docker state changed during collection): %v", inv.Warnings)
	}

	var containers, images, vols []string
	for _, c := range inv.Containers {
		containers = append(containers, c.ID)
	}
	for _, i := range inv.Images {
		images = append(images, i.ID)
	}
	for _, v := range inv.Volumes {
		vols = append(vols, v.Name)
		if v.State == "" || len(v.Reasons) == 0 {
			t.Errorf("volume %s has no classification or reason", v.Name)
		}
	}
	equal(t, "inventory containers", uniqueSorted(containers), dockerIDs(t, "ps", "-aq", "--no-trunc"))
	equal(t, "inventory images", uniqueSorted(images), dockerIDs(t, "images", "-aq", "--no-trunc"))
	equal(t, "inventory volumes", uniqueSorted(vols), dockerIDs(t, "volume", "ls", "-q"))

	if _, err := graph.FromInventory(inv); err != nil {
		t.Errorf("graph: %v", err)
	}
}

// The disk pressure percentage must agree with df(1), which rounds up.
func TestDiskPressureMatchesDf(t *testing.T) {
	inv := collect(t)
	if inv.Disk == nil {
		t.Skipf("disk not measured: %v", inv.Warnings)
	}
	out, err := exec.Command("df", "--output=pcent", inv.Host.DataRoot).Output()
	if err != nil {
		t.Skipf("df unavailable: %v", err)
	}
	fields := strings.Fields(string(out))
	dfPct, err := strconv.Atoi(strings.TrimSuffix(fields[len(fields)-1], "%"))
	if err != nil {
		t.Fatalf("parse df output %q: %v", out, err)
	}
	if got := int(math.Ceil(inv.Disk.UsedPercent)); got < dfPct-1 || got > dfPct+1 {
		t.Errorf("orca %.2f%% (ceil %d), df %d%%", inv.Disk.UsedPercent, got, dfPct)
	}
}

// fakeSSH puts an `ssh` on PATH that records its arguments and then runs
// `docker system dial-stdio` locally, so the ssh:// transport is exercised
// end to end against the local daemon without a real remote host.
func fakeSSH(t *testing.T) (argsFile string) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not installed")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	// DOCKER_HOST is cleared so the local docker CLI does not dial back
	// through this fake ssh when a test sets DOCKER_HOST=ssh://.
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\nunset DOCKER_HOST\nexec docker system dial-stdio\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func TestSSHTransport(t *testing.T) {
	argsFile := fakeSSH(t)
	const target = "ssh://tester@fake-host:2222"

	c, err := docker.Connect(context.Background(), docker.Target{URL: target}, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	info, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.ServerVersion == "" || info.Host != target || info.Local() {
		t.Errorf("info over ssh: version=%q host=%q local=%t", info.ServerVersion, info.Host, info.Local())
	}
	if _, err := c.Images(context.Background()); err != nil {
		t.Errorf("list images over ssh: %v", err)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	for _, want := range []string{"-l\ntester\n", "-p\n2222\n", "--\nfake-host\n", "docker system dial-stdio"} {
		if !strings.Contains(args, want) {
			t.Errorf("ssh args %q missing %q", args, want)
		}
	}
}

func TestSSHFromDockerHostEnv(t *testing.T) {
	fakeSSH(t)
	t.Setenv("DOCKER_HOST", "ssh://fake-host")

	c, err := docker.Connect(context.Background(), docker.Target{}, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if info, err := c.Info(context.Background()); err != nil || info.Host != "ssh://fake-host" {
		t.Errorf("DOCKER_HOST=ssh://: host=%q err=%v", info.Host, err)
	}
}

func TestInventoryOverSSHSkipsLocalDisk(t *testing.T) {
	fakeSSH(t)
	c, err := docker.Connect(context.Background(), docker.Target{URL: "ssh://fake-host"}, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	cfg := config.Default()
	inv, err := inventory.NewCollector(c, filesystem.Measurer{}, inventory.Options{
		Disk: cfg.Disk, LargeLogBytes: cfg.LargeLogBytes, Volumes: cfg.Volumes,
	}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Disk != nil || inv.Host.Address != "ssh://fake-host" {
		t.Errorf("remote inventory: disk=%v address=%q; the local disk must not be measured", inv.Disk, inv.Host.Address)
	}
}
