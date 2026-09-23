package validator

// Linux preserves immutable closure publication with descriptor-relative
// no-replace rename and exact native file mutation metadata.

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
		changeSeconds: stat.Ctim.Sec, changeNanoseconds: stat.Ctim.Nsec,
		modifySeconds: stat.Mtim.Sec, modifyNanoseconds: stat.Mtim.Nsec,
	}, true
}

// Never replaces an existing name or exposes a temporary hardlink alias.
func renameAttemptSettlementClosure(directoryFD int, from, to string) error {
	return unix.Renameat2(directoryFD, from, directoryFD, to, unix.RENAME_NOREPLACE)
}
