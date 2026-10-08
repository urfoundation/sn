//go:build linux

// Linux pins the approved ELF engine in a sealed memfd; the replay supervisor
// executes that descriptor under an owned subreaper.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Copy the independently hashed ELF into an immutable memfd before execution.
// Neither path replacement nor a write between hash and exec can select new
// code. No source file is mutated, and the descriptor closes after child join.
func historicalReplayEngine(ctx context.Context, reference planFileReference) (result *os.File, resultErr error) {
	if !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) {
		return nil, errors.New("historical replay engine requires exact absolute file pin")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := unix.Open(reference.Path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	source := os.NewFile(uintptr(fd), reference.Path)
	var copyFile *os.File
	defer func() {
		resultErr = errors.Join(resultErr, source.Close())
		if resultErr != nil {
			if copyFile != nil {
				resultErr = errors.Join(resultErr, copyFile.Close())
			}
			result = nil
		}
	}()
	before, err := source.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Mode().Perm()&0111 == 0 || before.Mode().Perm()&0022 != 0 || before.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || before.Size() < 4 || before.Size() > historicalReplayEngineLimit || stat.Nlink != 1 {
		return nil, errors.New("historical replay engine is not a bounded protected executable")
	}
	fd, err = unix.MemfdCreate("urnetwork-historical-replay", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	copyFile = os.NewFile(uintptr(fd), "historical-replay-engine")
	digest := sha256.New()
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := source.Read(buffer)
		if total == 0 && (n < 4 || !bytes.Equal(buffer[:4], []byte{0x7f, 'E', 'L', 'F'})) {
			return nil, errors.New("historical replay engine is not ELF")
		}
		total += int64(n)
		if total > before.Size() || total > historicalReplayEngineLimit {
			return nil, errors.New("historical replay engine grew during pinning")
		}
		if _, writeErr := io.MultiWriter(copyFile, digest).Write(buffer[:n]); writeErr != nil {
			return nil, writeErr
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	after, err := source.Stat()
	if err != nil {
		return nil, err
	}
	final, ok := after.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev != final.Dev || stat.Ino != final.Ino || stat.Mode != final.Mode || stat.Nlink != final.Nlink || stat.Mtim != final.Mtim || stat.Ctim != final.Ctim || total != before.Size() || after.Size() != total || "sha256:"+hex.EncodeToString(digest.Sum(nil)) != reference.Sha256 {
		return nil, errors.New("historical replay engine differs from its exact pin")
	}
	if err := copyFile.Chmod(0500); err != nil {
		return nil, err
	}
	if _, err := unix.FcntlInt(copyFile.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, err
	}
	return copyFile, ctx.Err()
}
