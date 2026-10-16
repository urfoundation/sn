//go:build linux || darwin

package validator

// The private storage adapter uses native no-follow flags, single-link inode
// checks and a process-scoped file lock on its retained directory descriptor.

import (
	"os"
	"syscall"
)

// A private file belongs to this user and has no external hardlink alias.
func attemptStorePrivateFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1 && uint64(stat.Uid) == uint64(os.Geteuid())
}

// Directory permission and owner checks precede any namespace data access.
func attemptStorePrivateDirectory(info os.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(stat.Uid) == uint64(os.Geteuid())
}

// The descriptor-relative open itself rejects a last-component symlink swap.
func attemptStoreNoFollowFlag() int {
	return syscall.O_NOFOLLOW
}

// Closing the retained directory descriptor releases the process lock. A
// replaced or missing LOCK file cannot grant a second owner a different inode.
func attemptStoreLockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
