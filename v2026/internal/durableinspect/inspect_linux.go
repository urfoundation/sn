//go:build linux

// Package durableinspect supplies a read-only operational preflight. It returns
// no guard, writer, descriptor or activation capability to its caller.
package durableinspect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Inspect uses the process's actual credentials. A parent installer may launch
// this exact code as the approved service user; no UID waiver is available.
func Inspect(ctx context.Context, reference durablevolume.Reference, paths []string) (Report, error) {
	if ctx == nil || ctx.Err() != nil {
		return Report{}, errors.New("storage inspection context is unavailable")
	}
	paths, err := orderedPaths(paths)
	if err != nil {
		return Report{}, err
	}
	ctx = durablevolume.WithReference(ctx, reference)
	report := Report{Schema: Schema, Reference: reference, Uid: uint32(os.Geteuid()), Gid: uint32(os.Getegid()), Directories: make([]Directory, 0, len(paths))}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		owner, err := durablepath.Open(ctx, path, durablevolume.ReadOnly, false)
		if err != nil {
			return Report{}, err
		}
		info, statErr := owner.File().Stat()
		err = errors.Join(statErr, owner.CheckRead(), owner.Close(), ctx.Err())
		if err != nil {
			return Report{}, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() {
			return Report{}, errors.New("storage inspection directory identity is unavailable")
		}
		report.Directories = append(report.Directories, Directory{Path: path, Device: uint64(stat.Dev), Inode: uint64(stat.Ino), Uid: stat.Uid, Gid: stat.Gid})
	}
	return report, nil
}

func orderedPaths(paths []string) ([]string, error) {
	if len(paths) == 0 || len(paths) > 256 {
		return nil, errors.New("storage inspection requires one to 256 explicit directories")
	}
	result := slices.Clone(paths)
	slices.Sort(result)
	for index, path := range result {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || len(path) > 4096 || index != 0 && result[index-1] == path {
			return nil, errors.New("storage inspection directory paths are invalid or repeated")
		}
	}
	return result, nil
}
