//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

// Claim queue ownership uses the physical directory rather than a replaceable
// lock file. Every read and publication remains relative to that descriptor.
package miner

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// These platforms provide the required process-lock and relative I/O primitives.
func claimQueuePlatformSupported() error { return nil }

// A borrowed descriptor must still name a private directory owned by this user.
func claimQueuePrivateDirectory(directory *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("claim queue state directory must be private and owned")
	}
	return nil
}

// The returned descriptor takes lifetime ownership, including process death.
// Nonblocking acquisition rejects another owner before any queue read or write.
func claimQueueOpenDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), path)
	if err := claimQueuePrivateDirectory(directory); err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(fmt.Errorf("claim queue already has an owner: %w", err), directory.Close())
	}
	return directory, nil
}

// Read only a private regular entry; a fifo cannot block ownership admission.
// Unsafe entries may be replaced by an existing owner's atomic publication.
func claimQueueReadFile(directory *os.File, name string) ([]byte, os.FileMode, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			err = errors.Join(errClaimQueueUnsafeFile, err)
		}
		return nil, 0, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, statErr := file.Stat()
	var stat unix.Stat_t
	ownerErr := unix.Fstat(fd, &stat)
	if err := errors.Join(statErr, ownerErr); err != nil {
		return nil, 0, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return nil, 0, errors.Join(errClaimQueueUnsafeFile, file.Close())
	}
	raw, readErr := io.ReadAll(file)
	return raw, info.Mode(), errors.Join(readErr, file.Close())
}

// All temporary creation, replacement and sync use the locked directory, even
// when its original pathname is renamed or replaced during publication.
func claimQueuePublish(directory *os.File, name string, raw []byte) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".claim-queue-" + hex.EncodeToString(nonce[:])
	directoryFd := int(directory.Fd())
	fd, err := unix.Openat(directoryFd, temporary, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(directoryFd, temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	if err := file.Chmod(0o600); err != nil {
		return errors.Join(err, file.Close())
	}
	written, writeErr := file.Write(raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := unix.Renameat(directoryFd, temporary, directoryFd, name); err != nil {
		return err
	}
	return directory.Sync()
}
