// Monitor files publish relative to one retained directory descriptor. A
// production owner additionally admits the exact declared durable filesystem.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The synchronous monitor joins its users before releasing these descriptors.
type monitorDirectory struct {
	path     string
	file     *os.File
	guard    *durablepath.Directory
	ctx      context.Context
	head     *durablehead.Owner
	headName string
}

// Pending ownership/capacity is retryable only while identity remains admitted.
func monitorStoragePending(err error) bool {
	var cleanup *monitorAdmissionCleanupError
	var refused *monitorAdmissionRefusalError
	return !errors.As(err, &cleanup) && !errors.As(err, &refused) && !errors.Is(err, durablevolume.ErrIdentity) && (errors.Is(err, durablevolume.ErrBusy) || errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablehead.ErrUncertain))
}

// A missing physical member is fresh only when no retained authority says it
// existed. Joined identity or observation errors must never default to empty.
func monitorCheckpointAbsent(err error) bool {
	return errors.Is(err, os.ErrNotExist) && !errors.Is(err, durablevolume.ErrIdentity) && !errors.Is(err, durablevolume.ErrUnavailable)
}

// A failed observation cannot establish a changed inode. Only a successfully
// observed mismatch or definite disappearance invalidates the retained owner.
func monitorNamedObservation(err error) error {
	if err == nil {
		return nil
	}
	// Cancellation is a caller outcome, not an unavailable physical fact.
	// Mixed independent causes still follow the ordinary hard-error rules.
	if monitorOnlyCancellationCauses(err, 0) {
		return err
	}
	if errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return monitorCustodyError(errors.Join(durablevolume.ErrIdentity, err))
	}
	return errors.Join(durablevolume.ErrUnavailable, err)
}

// Production contexts require a declaration; retained local-format fixtures
// use the descriptor-only constructor without selecting a production volume.
func openMonitorDirectory(path string, contexts []context.Context) (*monitorDirectory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("monitor directory must be canonical absolute")
	}
	self := &monitorDirectory{path: path}
	if len(contexts) != 0 {
		if len(contexts) != 1 {
			return nil, errors.New("monitor has ambiguous storage context")
		}
		guard, err := durablepath.Open(contexts[0], path, durablevolume.ReadWrite, false)
		if err != nil {
			return nil, err
		}
		self.guard, self.file, self.ctx = guard, guard.File(), contexts[0]
	} else {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return nil, errors.Join(errors.New("monitor directory traverses an alias"), err)
		}
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		self.file = os.NewFile(uintptr(fd), path)
	}
	if err := self.check(); err != nil {
		return nil, monitorAdmissionFailure(err, self.close())
	}
	return self, nil
}

// A replaced parent cannot redirect writes or preserve a stale lock's authority.
func (self *monitorDirectory) check() error {
	if self == nil || self.file == nil {
		return errors.New("monitor directory owner is closed")
	}
	if self.guard != nil {
		if err := self.guard.CheckRead(); err != nil {
			return monitorCustodyError(err)
		}
		if self.head != nil {
			return monitorCustodyError(self.head.Check())
		}
		return self.ctx.Err()
	}
	resolved, resolveErr := filepath.EvalSymlinks(self.path)
	opened, openErr := self.file.Stat()
	named, nameErr := os.Lstat(self.path)
	if err := errors.Join(resolveErr, openErr, nameErr); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return monitorCustodyError(errors.Join(durablevolume.ErrIdentity, err))
		}
		if errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF) {
			return err
		}
		return errors.Join(durablevolume.ErrUnavailable, err)
	}
	if resolved != self.path || !named.IsDir() || !os.SameFile(opened, named) || opened.Mode() != named.Mode() {
		return monitorCustodyError(errors.Join(durablevolume.ErrIdentity, errors.New("monitor directory changed after admission")))
	}
	return nil
}

// Proven custody loss is terminal for this owner. Observation outages remain
// retryable and must not be promoted into a fabricated identity change.
func monitorCustodyError(err error) error {
	if errors.Is(err, durablevolume.ErrIdentity) {
		return errors.Join(&monitorOutputOwnershipError{reason: "monitor durable custody identity changed"}, err)
	}
	return err
}

func (self *monitorDirectory) checkWrite() error {
	if err := self.check(); err != nil {
		return err
	}
	if self.head != nil {
		return monitorCustodyError(self.head.CheckWrite())
	}
	if self.guard != nil {
		return monitorCustodyError(self.guard.CheckWrite())
	}
	return nil
}

// Only one simple basename can be opened beneath the retained namespace.
func (self *monitorDirectory) open(name string, flags int, mode uint32) (*os.File, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return nil, errors.New("monitor file name is invalid")
	}
	check := self.check
	if flags&(syscall.O_WRONLY|syscall.O_RDWR|syscall.O_CREAT|syscall.O_TRUNC) != 0 {
		check = self.checkWrite
	}
	if err := check(); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(self.file.Fd()), name, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, mode)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return nil, errors.Join(&monitorOutputOwnershipError{reason: "monitor file became an alias or changed type"}, err)
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(self.path, name))
	if err := check(); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// Metadata reads use the same descriptor and never follow a final symlink.
func (self *monitorDirectory) stat(name string) (os.FileInfo, error) {
	file, err := self.open(name, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	return info, errors.Join(statErr, file.Close())
}

// Reads remain bounded and refuse special or aliased file generations.
func (self *monitorDirectory) read(name string, maximum int, private bool) ([]byte, error) {
	if self.head != nil && name == self.headName {
		raw, present, err := self.head.Read()
		if err != nil {
			return nil, monitorCustodyError(err)
		}
		if !present {
			return nil, os.ErrNotExist
		}
		if len(raw) > maximum {
			return nil, errors.New("monitor input exceeded its byte bound")
		}
		return raw, nil
	}
	file, err := self.open(name, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	var stat syscall.Stat_t
	fdErr := syscall.Fstat(int(file.Fd()), &stat)
	forbidden := os.FileMode(0022)
	if private {
		forbidden = 0077
	}
	if statErr != nil || fdErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&forbidden != 0 || stat.Nlink != 1 {
		return nil, errors.Join(errors.New("monitor input is not a protected regular file"), statErr, fdErr, file.Close())
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	after, afterErr := file.Stat()
	closeErr := file.Close()
	if len(raw) > maximum || afterErr != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, errors.Join(errors.New("monitor input changed or exceeded its byte bound"), readErr, afterErr, closeErr)
	}
	return raw, errors.Join(readErr, closeErr, self.check())
}

// Actual create, rename and sync operations never resolve a replacement parent.
func (self *monitorDirectory) publish(name string, raw []byte, mode os.FileMode, syncDirectory func(*os.File) error) error {
	if self.head != nil && name == self.headName {
		if mode != 0600 {
			return errors.New("monitor custody snapshot must stay private")
		}
		return monitorCustodyError(self.head.Publish(raw, syncDirectory))
	}
	if err := self.checkWrite(); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".sn-mainnet-monitor-" + hex.EncodeToString(nonce[:])
	file, err := self.open(temporary, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(int(self.file.Fd()), temporary, 0)
	if err := file.Chmod(mode); err != nil {
		return errors.Join(err, file.Close())
	}
	written, writeErr := file.Write(raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close(), self.checkWrite()); err != nil {
		return err
	}
	if err := unix.Renameat(int(self.file.Fd()), temporary, int(self.file.Fd()), name); err != nil {
		return err
	}
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(self.file), self.checkWrite())
}

// The guard owns its borrowed directory; descriptor-only owners close directly.
func (self *monitorDirectory) close() error {
	if self == nil || self.file == nil {
		return nil
	}
	file := self.file
	self.file = nil
	headErr := self.head.Close()
	self.head = nil
	if self.guard != nil {
		return errors.Join(headErr, self.guard.Close())
	}
	return errors.Join(headErr, file.Close())
}
