//go:build unix

// Package filesystem implements fsmeasure.Measurer with a read-only walk.
package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/HamzaGbada/orca/internal/extensions/fsmeasure"
)

// Measurer walks directory trees using only stat/readdir calls.
type Measurer struct{}

var _ fsmeasure.Measurer = Measurer{}

type inodeKey struct{ dev, ino uint64 }

func (Measurer) Measure(ctx context.Context, path string) (fsmeasure.Usage, error) {
	// Data roots are often symlinks to another disk; WalkDir would not
	// descend into a symlinked root, so resolve it first.
	root, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fsmeasure.Usage{}, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fsmeasure.Usage{}, err
	}
	rootStat, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fsmeasure.Usage{}, fmt.Errorf("stat %s: no unix stat data", root)
	}
	// Fail early with fs.ErrPermission rather than reporting an empty tree.
	dir, err := os.Open(root)
	if err != nil {
		return fsmeasure.Usage{}, err
	}
	dir.Close()

	usage := fsmeasure.Usage{Path: path}
	seen := make(map[inodeKey]struct{})
	rootDev := uint64(rootStat.Dev)

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			usage.Unreadable++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			usage.Unreadable++
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			usage.Unreadable++
			return nil
		}

		// Do not descend into other filesystems, e.g. the overlay mounts of
		// running containers at overlay2/<id>/merged, which would count
		// their lower layers a second time.
		if uint64(st.Dev) != rootDev {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if st.Nlink > 1 && !d.IsDir() {
			key := inodeKey{dev: uint64(st.Dev), ino: uint64(st.Ino)}
			if _, dup := seen[key]; dup {
				return nil
			}
			seen[key] = struct{}{}
		}

		if d.IsDir() {
			usage.Dirs++
		} else {
			usage.Files++
		}
		usage.ApparentBytes += info.Size()
		usage.AllocatedBytes += int64(st.Blocks) * 512
		return nil
	})
	if err != nil {
		return usage, err
	}
	return usage, nil
}

func (Measurer) MeasureFiles(ctx context.Context, path string) (fsmeasure.Usage, error) {
	// Lstat first: Glob silently returns no matches for an unreadable
	// directory, which would report an unknown size as zero.
	if _, err := os.Lstat(path); err != nil {
		return fsmeasure.Usage{}, err
	}
	matches, err := filepath.Glob(escapeGlob(path) + "*")
	if err != nil {
		return fsmeasure.Usage{}, err
	}

	usage := fsmeasure.Usage{Path: path}
	for _, m := range matches {
		if err := ctx.Err(); err != nil {
			return usage, err
		}
		info, err := os.Lstat(m)
		if err != nil {
			usage.Unreadable++
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		usage.Files++
		usage.ApparentBytes += info.Size()
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			usage.AllocatedBytes += int64(st.Blocks) * 512
		}
	}
	return usage, nil
}

// escapeGlob escapes glob metacharacters so path matches only itself.
func escapeGlob(path string) string {
	var b strings.Builder
	for _, r := range path {
		if strings.ContainsRune(`*?[\\`, r) {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (Measurer) Capacity(_ context.Context, path string) (fsmeasure.Capacity, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return fsmeasure.Capacity{}, &fs.PathError{Op: "statfs", Path: path, Err: err}
	}
	bs := int64(st.Bsize)
	return fsmeasure.Capacity{
		Path:       path,
		TotalBytes: int64(st.Blocks) * bs,
		UsedBytes:  int64(st.Blocks-st.Bfree) * bs,
		AvailBytes: int64(st.Bavail) * bs,
	}, nil
}
