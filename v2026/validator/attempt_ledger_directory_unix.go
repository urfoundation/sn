//go:build linux || darwin

package validator

// Native ownership checks and cancellable flock admission share the private
// record store's inode contract on supported deployment platforms.

import (
	"errors"
	"os"
	"syscall"
)

// No permission relaxation is implied by using the compatibility constructor.
func attemptLedgerPrivateDirectory(info os.FileInfo) bool { return attemptStorePrivateDirectory(info) }

// Evidence has one private inode owner.
func attemptLedgerPrivateFile(info os.FileInfo) bool { return attemptStorePrivateFile(info) }

// The final component cannot be replaced with a symlink at open time.
func attemptLedgerNoFollowFlag() int { return syscall.O_NOFOLLOW }

// Each failed acquisition distinguishes ordinary contention from I/O failure.
func attemptLedgerTryLock(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

// Closing also unlocks, but scoped legacy appends retain their descriptors.
func attemptLedgerUnlock(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }

// Immutable migration receipts never replace a destination.
func attemptLedgerRenameNoReplace(directory *os.File, from, to string) error {
	return attemptStoreRenameNoReplace(directory, from, to)
}

// Device/inode identify a local interrupted import; portable completed copies
// are instead compared using namespace, exact bytes and the signed prefix.
func attemptLedgerLocalFileID(info os.FileInfo) (uint64, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("attempt ledger local file identity is unavailable")
	}
	return uint64(stat.Dev), uint64(stat.Ino), nil
}
