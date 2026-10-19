//go:build linux || darwin || freebsd

// A configured complete launch requires the same process-owned directory
// custody supported by the Core capture worker's lifetime file lease.
package miner

import (
	"errors"
	"os"
	"syscall"
)

// Foreign private directories cannot become this provider's original outbox.
func validateProviderWorkCaptureDirectory(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("whole-work launch outbox is not owned by this process user")
	}
	return nil
}
