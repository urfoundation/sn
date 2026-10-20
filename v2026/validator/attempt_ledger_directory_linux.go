package validator

// Native write-state includes ctime, so restoring size and mtime cannot make
// a changed private projection look like the last acknowledged inode state.

import (
	"os"
	"syscall"
)

// The directory operation gate excludes other upgraded writers. Linux ctime
// and mtime detect an external rewrite during this owner's warm cursor; they
// are local change detection, never portable proof or signed evidence.
func attemptLedgerSameFileState(left, right os.FileInfo) bool {
	if !attemptLedgerPrivateFile(left) || !attemptLedgerPrivateFile(right) || !os.SameFile(left, right) || left.Size() != right.Size() {
		return false
	}
	leftStat, leftOK := left.Sys().(*syscall.Stat_t)
	rightStat, rightOK := right.Sys().(*syscall.Stat_t)
	return leftOK && rightOK && leftStat.Ctim == rightStat.Ctim && leftStat.Mtim == rightStat.Mtim
}
