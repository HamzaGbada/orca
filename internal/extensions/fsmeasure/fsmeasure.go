// Package fsmeasure defines the contract for read-only disk usage measurement.
//
// Measurement is diagnostic only: implementations must never create, modify
// or delete anything under the measured path (Safety Rule #3).
package fsmeasure

import "context"

// Usage is the measured disk usage of a directory tree.
type Usage struct {
	Path string
	// ApparentBytes is the sum of file sizes (what `du --apparent-size` reports).
	ApparentBytes int64
	// AllocatedBytes is the space actually allocated on disk (what `du` reports).
	AllocatedBytes int64
	Files          int64
	Dirs           int64
	// Unreadable counts entries that could not be read (e.g. permission
	// denied). When non-zero the totals are a lower bound.
	Unreadable int64
}

// Measurer measures disk usage of a directory tree without modifying it.
type Measurer interface {
	// Measure walks path without following symlinks or crossing into other
	// filesystems (mount points), counting hard-linked files once. It returns
	// an error wrapping fs.ErrPermission or fs.ErrNotExist when path itself
	// cannot be read.
	Measure(ctx context.Context, path string) (Usage, error)

	// MeasureFiles measures path and its siblings named path + suffix (e.g.
	// rotated logs "x.log.1", "x.log.2.gz"). It returns an error wrapping
	// fs.ErrPermission or fs.ErrNotExist when path cannot be read.
	MeasureFiles(ctx context.Context, path string) (Usage, error)

	// Capacity returns the size and free space of the filesystem holding path.
	Capacity(ctx context.Context, path string) (Capacity, error)
}

// Capacity describes a filesystem, with the same meaning as df(1).
type Capacity struct {
	Path       string
	TotalBytes int64
	UsedBytes  int64
	// AvailBytes is the space available to unprivileged users; it excludes
	// blocks reserved for root, so Used + Avail can be less than Total.
	AvailBytes int64
}

// UsedPercent is the used share of the space usable by unprivileged users,
// computed as df(1) does: Used / (Used + Avail) * 100.
func (c Capacity) UsedPercent() float64 {
	if c.UsedBytes+c.AvailBytes == 0 {
		return 0
	}
	return float64(c.UsedBytes) * 100 / float64(c.UsedBytes+c.AvailBytes)
}
