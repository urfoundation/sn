//go:build linux

package validator

// Occupied publication atomically preserves the displaced inode; it is not
// compare-and-swap. Witnessed absence uses the kernel's no-replace guarantee.

import (
	"os"

	"golang.org/x/sys/unix"
)

// Unsupported filesystem/kernel flags fail closed without Rename fallback.
func publishHeadEMAStoreV2File(directory *os.File, from, to string, occupied bool) error {
	flags := uint(unix.RENAME_NOREPLACE)
	if occupied {
		flags = unix.RENAME_EXCHANGE
	}
	return unix.Renameat2(int(directory.Fd()), from, int(directory.Fd()), to, flags)
}
