//go:build linux

// The request extension uses one small inode-bound xattr. All record bytes live
// in ordinary protected files and remain inside the complete owner inventory.
package validator

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func readProviderAttemptRequestAttribute(file *os.File) ([]byte, bool, error) {
	raw := make([]byte, 4096)
	n, err := unix.Fgetxattr(int(file.Fd()), ProviderAttemptRequestAttribute, raw)
	if errors.Is(err, unix.ENODATA) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return raw[:n], false, nil
}
func writeProviderAttemptRequestAttribute(file *os.File, raw []byte, create bool) error {
	flags := unix.XATTR_REPLACE
	if create {
		flags = unix.XATTR_CREATE
	}
	return unix.Fsetxattr(int(file.Fd()), ProviderAttemptRequestAttribute, raw, flags)
}
func sameProviderAttemptRequestFileStat(a, b os.FileInfo) bool {
	left, ok := a.Sys().(*syscall.Stat_t)
	right, other := b.Sys().(*syscall.Stat_t)
	return ok && other && left.Ctim == right.Ctim && left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid && left.Nlink == right.Nlink
}
