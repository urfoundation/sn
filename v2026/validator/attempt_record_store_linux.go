package validator

// Linux no-replace rename never truncates a conflicting backend descriptor.

import (
	"os"

	"golang.org/x/sys/unix"
)

// Both names stay relative to the retained private directory descriptor.
func attemptStoreRenameNoReplace(directory *os.File, from, to string) error {
	return unix.Renameat2(int(directory.Fd()), from, int(directory.Fd()), to, unix.RENAME_NOREPLACE)
}
