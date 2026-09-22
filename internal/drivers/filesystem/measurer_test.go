//go:build unix

package filesystem

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeasure(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a"), 1000)
	write(t, filepath.Join(root, "sub", "b"), 3000)
	// A hard link to "a" must be counted once.
	if err := os.Link(filepath.Join(root, "a"), filepath.Join(root, "sub", "a-link")); err != nil {
		t.Fatal(err)
	}
	// A symlink to a large file outside the tree must not be followed.
	outside := filepath.Join(t.TempDir(), "big")
	write(t, outside, 1<<20)
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	u, err := Measurer{}.Measure(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	linkInfo, err := os.Lstat(filepath.Join(root, "link"))
	if err != nil {
		t.Fatal(err)
	}
	var dirBytes int64
	for _, d := range []string{root, filepath.Join(root, "sub")} {
		info, err := os.Lstat(d)
		if err != nil {
			t.Fatal(err)
		}
		dirBytes += info.Size()
	}

	if want := 1000 + 3000 + linkInfo.Size() + dirBytes; u.ApparentBytes != want {
		t.Errorf("ApparentBytes = %d, want %d", u.ApparentBytes, want)
	}
	if u.Files != 3 || u.Dirs != 2 {
		t.Errorf("Files/Dirs = %d/%d, want 3/2 (a, sub/b, link / root, sub)", u.Files, u.Dirs)
	}
	if u.AllocatedBytes <= 0 {
		t.Errorf("AllocatedBytes = %d, want > 0", u.AllocatedBytes)
	}
	if u.Unreadable != 0 {
		t.Errorf("Unreadable = %d, want 0", u.Unreadable)
	}
}

func TestMeasureSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	write(t, filepath.Join(target, "a"), 4096)
	link := filepath.Join(t.TempDir(), "docker")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	u, err := Measurer{}.Measure(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Files != 1 || u.ApparentBytes < 4096 || u.Path != link {
		t.Errorf("symlinked root: Files=%d ApparentBytes=%d Path=%s, want 1 file of >= 4096 bytes at %s",
			u.Files, u.ApparentBytes, u.Path, link)
	}
}

func TestMeasurePermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	write(t, filepath.Join(locked, "secret"), 5000)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	u, err := Measurer{}.Measure(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if u.Unreadable != 1 {
		t.Errorf("Unreadable = %d, want 1", u.Unreadable)
	}

	if _, err := (Measurer{}).Measure(context.Background(), locked); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("unreadable root: err = %v, want fs.ErrPermission", err)
	}
}

func TestMeasureMissing(t *testing.T) {
	_, err := Measurer{}.Measure(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestMeasureCanceled(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a"), 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := (Measurer{}).Measure(ctx, root); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestMeasureFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "odd[dir]*")
	log := filepath.Join(dir, "c-json.log")
	write(t, log, 100)
	write(t, log+".1", 200)
	write(t, log+".2.gz", 50)
	write(t, filepath.Join(dir, "other.log"), 9999)
	if err := os.Mkdir(log+".dir", 0o755); err != nil {
		t.Fatal(err)
	}

	u, err := Measurer{}.MeasureFiles(context.Background(), log)
	if err != nil {
		t.Fatal(err)
	}
	if u.ApparentBytes != 350 || u.Files != 3 {
		t.Errorf("got %d bytes in %d files, want 350 in 3 (log + rotations, glob characters in the path escaped)",
			u.ApparentBytes, u.Files)
	}

	if _, err := (Measurer{}).MeasureFiles(context.Background(), filepath.Join(dir, "missing.log")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing log: err = %v, want fs.ErrNotExist", err)
	}
}

func TestMeasureFilesPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	write(t, filepath.Join(dir, "c-json.log"), 10)
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	_, err := Measurer{}.MeasureFiles(context.Background(), filepath.Join(dir, "c-json.log"))
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("err = %v, want fs.ErrPermission (never a silent zero)", err)
	}
}

func TestCapacity(t *testing.T) {
	c, err := Measurer{}.Capacity(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.TotalBytes <= 0 || c.UsedBytes < 0 || c.AvailBytes < 0 || c.UsedBytes+c.AvailBytes > c.TotalBytes {
		t.Errorf("implausible capacity %+v", c)
	}
	if p := c.UsedPercent(); p < 0 || p > 100 {
		t.Errorf("UsedPercent = %v", p)
	}

	if _, err := (Measurer{}).Capacity(context.Background(), filepath.Join(t.TempDir(), "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing path: err = %v, want fs.ErrNotExist", err)
	}
}
