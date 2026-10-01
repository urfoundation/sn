// Archive output is private, complete, and create-only. Failure after rename
// preserves the candidate for inspection and never acknowledges uncertain sync.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Descriptor-relative publication cannot follow a final-name symlink or
// truncate earlier evidence. Only a full sealed capture may reach this boundary.
func writeSafeHistoryCapture(ctx context.Context, path string, envelope safeHistoryCaptureEnvelope) (resultErr error) {
	if ctx == nil || !bootstrapRootAbsolutePath(path) {
		return errors.New("Safe archive output requires a context and absolute canonical path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := bootstrapRootDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	checked, err := sealSafeHistoryCapture(envelope.Capture)
	if err != nil || checked.ContentHash != envelope.ContentHash || !envelope.Capture.ByteCommitmentsVerified {
		return errors.Join(errors.New("Safe archive output seal differs"), err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	fd, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), filepath.Dir(path))
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&0077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("Safe archive output directory is not private and owned")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	stage := ".safe-archive-" + hex.EncodeToString(random[:]) + ".tmp"
	stageFd, err := unix.Openat(fd, stage, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(stageFd), stage)
	defer func() {
		file.Close()
		if err := unix.Unlinkat(fd, stage, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	for offset := 0; offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Write(raw[offset:min(len(raw), offset+1024*1024)])
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		offset += n
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Renameat2(fd, stage, fd, filepath.Base(path), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return errors.Join(directory.Sync(), ctx.Err())
}
