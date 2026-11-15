//go:build darwin

// Darwin names the native change time Ctimespec.
package validator

import (
	"os"
	"syscall"
)

func sameProviderAttemptRequestFileStat(a, b os.FileInfo) bool {
	left, ok := a.Sys().(*syscall.Stat_t)
	right, other := b.Sys().(*syscall.Stat_t)
	return ok && other && left.Ctimespec == right.Ctimespec && left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid && left.Nlink == right.Nlink
}
