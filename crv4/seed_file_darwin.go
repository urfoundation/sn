package crv4

// Darwin publication has the same non-replacing descriptor-relative contract.

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Both names are relative to the retained, checked directory descriptor.
func seedFilePublish(directory *os.File, from, to string) error {
	return unix.RenameatxNp(int(directory.Fd()), from, int(directory.Fd()), to, unix.RENAME_EXCL)
}

// Atime can change on read; size, mtime and ctime cannot change unnoticed.
func seedFileSameWriteState(before, after os.FileInfo) bool {
	a, aOK := before.Sys().(*syscall.Stat_t)
	b, bOK := after.Sys().(*syscall.Stat_t)
	return aOK && bOK && os.SameFile(before, after) && before.Mode() == after.Mode() &&
		a.Size == b.Size && a.Mtimespec == b.Mtimespec && a.Ctimespec == b.Ctimespec && a.Uid == b.Uid && a.Nlink == b.Nlink
}
