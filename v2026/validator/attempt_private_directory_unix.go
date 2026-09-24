//go:build linux || darwin

package validator

// These are real descriptor-relative syscalls. os.Root is confinement rather
// than no-follow admission and can retry an in-root symlink after ELOOP.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Intermediate descriptors are closed as the finite path is walked. The
// caller owns only the final directory; every failed cleanup is joined.
func openAttemptPrivateDirectoryFile(path string) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	current := os.NewFile(uintptr(fd), string(filepath.Separator))
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		next, openErr := unix.Openat(int(current.Fd()), component, flags, 0)
		closeErr := current.Close()
		if openErr != nil {
			return nil, errors.Join(&os.PathError{Op: "openat", Path: path, Err: openErr}, closeErr)
		}
		current = os.NewFile(uintptr(next), path)
		if closeErr != nil {
			return nil, errors.Join(closeErr, current.Close())
		}
	}
	return current, nil
}

// A no-follow name observation reports symlinks as symlinks without opening.
func (self *attemptPrivateDirectory) stat(name string) (attemptPrivateFileState, error) {
	if self == nil || self.file == nil || !validAttemptPrivateLeaf(name) {
		return attemptPrivateFileState{}, errors.New("private metadata stat has invalid owner or name")
	}
	var state unix.Stat_t
	if err := unix.Fstatat(int(self.file.Fd()), name, &state, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return attemptPrivateFileState{}, &os.PathError{Op: "fstatat", Path: filepath.Join(self.path, name), Err: err}
	}
	return attemptPrivateNativeFileState(&state), nil
}

// O_NONBLOCK is essential before fstat can refuse a replacement FIFO.
func (self *attemptPrivateDirectory) openFile(name string, flags int, mode uint32) (*os.File, error) {
	if self == nil || self.file == nil || !validAttemptPrivateLeaf(name) {
		return nil, errors.New("private metadata open has invalid owner or name")
	}
	path := filepath.Join(self.path, name)
	fd, err := unix.Openat(int(self.file.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, mode)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// Descriptor metadata uses the same native representation as name metadata.
func statAttemptPrivateFile(file *os.File) (attemptPrivateFileState, error) {
	if file == nil {
		return attemptPrivateFileState{}, errors.New("private metadata descriptor is nil")
	}
	var state unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &state); err != nil {
		return attemptPrivateFileState{}, &os.PathError{Op: "fstat", Path: file.Name(), Err: err}
	}
	return attemptPrivateNativeFileState(&state), nil
}
