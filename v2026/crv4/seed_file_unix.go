//go:build linux || darwin

package crv4

// Seed custody accepts only owner-readable private, single-link regular files.
// Existing owned non-writable-by-others parents need not be silently chmodded.

import (
	"os"
	"syscall"
)

// These platforms provide descriptor-relative no-follow and no-replace I/O.
func seedFilesSupported() error { return nil }

// Nonblocking prevents a replaced FIFO from blocking before its type is checked.
func seedFileOpenFlags() int { return syscall.O_NOFOLLOW | syscall.O_NONBLOCK }

// Read-only private keys are valid; executable, shared and aliased keys are not.
func seedFilePrivate(info os.FileInfo) bool {
	if info == nil || info.Mode() != 0o600 && info.Mode() != 0o400 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1 && uint64(stat.Uid) == uint64(os.Geteuid())
}

// Parent listing is compatible with 0755; other users may not replace its keys.
func seedFileOwnedParent(info os.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(stat.Uid) == uint64(os.Geteuid())
}
