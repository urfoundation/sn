package validator

// Linux native timestamps remain full-width rather than UnixNano conversions.

import "golang.org/x/sys/unix"

// One representation serves fstat and no-follow fstatat observations.
func attemptPrivateNativeFileState(state *unix.Stat_t) attemptPrivateFileState {
	return attemptPrivateFileState{dev: uint64(state.Dev), ino: uint64(state.Ino), mode: uint32(state.Mode), uid: uint32(state.Uid), links: uint64(state.Nlink), size: int64(state.Size), changeSeconds: int64(state.Ctim.Sec), changeNanoseconds: int64(state.Ctim.Nsec), modifySeconds: int64(state.Mtim.Sec), modifyNanoseconds: int64(state.Mtim.Nsec)}
}
