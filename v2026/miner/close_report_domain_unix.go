//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

// Optional public configuration cannot wait for a FIFO writer or follow a
// replacement symlink. Descriptor admission does not require secret-file modes.
package miner

import (
	"golang.org/x/sys/unix"
	"os"
)

// The caller owns the returned descriptor and checks its actual type and bound
// before reading; no pathname stat/open interval can admit a swapped special file.
func openProviderCloseReportDomain(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
