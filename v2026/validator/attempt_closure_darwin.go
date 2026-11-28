package validator

// Darwin retains the same immutable closure and alias invariants using its
// exclusive descriptor-relative rename and native timestamp field names.

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Keeps nanoseconds separate to avoid narrowing the native timestamp range.
func attemptSettlementClosureFileMetadata(info os.FileInfo) (attemptSettlementClosureFileState, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return attemptSettlementClosureFileState{}, false
	}
	return attemptSettlementClosureFileState{
		links:         uint64(stat.Nlink),
		changeSeconds: stat.Ctimespec.Sec, changeNanoseconds: stat.Ctimespec.Nsec,
		modifySeconds: stat.Mtimespec.Sec, modifyNanoseconds: stat.Mtimespec.Nsec,
	}, true
}

// Never falls back to an overwriting rename when exclusive publication fails.
func renameAttemptSettlementClosure(directoryFD int, from, to string) error {
	return unix.RenameatxNp(directoryFD, from, directoryFD, to, unix.RENAME_EXCL)
}
