package validator

// Darwin's exclusive rename supplies the same no-replace storage contract.

import (
	"os"

	"golang.org/x/sys/unix"
)

// Both names stay relative to the retained private directory descriptor.
func attemptStoreRenameNoReplace(directory *os.File, from, to string) error {
	return unix.RenameatxNp(int(directory.Fd()), from, int(directory.Fd()), to, unix.RENAME_EXCL)
}
