//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

// Descriptor-relative custody requires a private namespace and process-owned
// advisory lock. Unsupported platforms refuse this mainnet write path.
package miner

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// These platforms provide all required custody primitives.
func fleetRecoveryPlatformSupported() error { return nil }

// Checks ownership and exact private permissions on the opened object.
func fleetRecoveryPrivateFile(file *os.File, directory bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Uid != uint32(os.Geteuid()) || (directory && (!info.IsDir() || info.Mode().Perm() != 0700)) || (!directory && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600)) {
		return errors.New("fleet recovery object is not private and owned")
	}
	return nil
}

// Opens a private regular entry without following a symlink or changing roots.
func fleetRecoveryOpenFile(directory *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if err := fleetRecoveryPrivateFile(file, false); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

// Pins the owned namespace against leaf symlink substitution.
func fleetRecoveryOpenDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if err := fleetRecoveryPrivateFile(file, true); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

// Closing this descriptor releases the lock even after process death.
func fleetRecoveryLock(directory *os.File) error {
	return unix.Flock(int(directory.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

// The rename stays within the already locked directory descriptor.
func fleetRecoveryRename(directory *os.File, from, to string) error {
	fd := int(directory.Fd())
	return unix.Renameat(fd, from, fd, to)
}
