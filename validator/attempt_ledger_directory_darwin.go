package validator

// Darwin records local proof write-state with both change and modify times.

import (
	"os"
	"syscall"
)

// Inode, ownership, length and native write times qualify the incremental
// cursor. A fresh owner still verifies every existing byte from signed records.
func attemptLedgerSameFileState(left, right os.FileInfo) bool {
	if !attemptLedgerPrivateFile(left) || !attemptLedgerPrivateFile(right) || !os.SameFile(left, right) || left.Size() != right.Size() {
		return false
	}
	leftStat, leftOK := left.Sys().(*syscall.Stat_t)
	rightStat, rightOK := right.Sys().(*syscall.Stat_t)
	return leftOK && rightOK && leftStat.Ctimespec == rightStat.Ctimespec && leftStat.Mtimespec == rightStat.Mtimespec
}
