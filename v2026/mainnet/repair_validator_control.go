// A fixed per-unit file lock excludes cooperating repair/activation owners
// across different incident journals. Privileged host administration stays trusted.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
)

// The marker is permanent and release-independent; it is never removed to
// replenish capacity. Each caller joins its commands before releasing this lock.
type repairValidatorControl struct {
	file   *os.File
	path   string
	marker string
}

// Deriving the lock from the signed canonical fragment prevents per-incident
// paths from admitting simultaneous controllers for the same installed unit.
func (self *repairValidatorHost) control(ctx context.Context, unit repairValidatorUnit) (*repairValidatorControl, error) {
	path := unit.File.Path + ".sn-control.lock"
	if ctx == nil || ctx.Err() != nil {
		return nil, errors.New("validator unit control context unavailable")
	}
	if err := self.parents(path, self.rootUid); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	owner := &repairValidatorControl{file: os.NewFile(uintptr(fd), path), path: path, marker: "urnetwork-mainnet-validator-control-v1 " + unit.Name + "\n"}
	success := false
	defer func() {
		if !success {
			owner.close()
		}
	}()
	info, err := owner.file.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != self.rootUid || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
		return nil, errors.New("validator unit control file is unprotected")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.New("validator unit already has a control owner")
	}
	if info.Size() == 0 {
		if _, err := owner.file.WriteString(owner.marker); err != nil {
			return nil, err
		}
		if err := owner.file.Sync(); err != nil {
			return nil, err
		}
	}
	if err := owner.validate(); err != nil {
		return nil, err
	}
	success = true
	return owner, nil
}

// An unlink/replacement or changed marker cannot leave an action owner valid.
func (self *repairValidatorControl) validate() error {
	if self == nil || self.file == nil {
		return errors.New("validator unit control is closed")
	}
	opened, err := self.file.Stat()
	named, nameErr := os.Lstat(self.path)
	if err != nil || nameErr != nil || !named.Mode().IsRegular() || named.Mode().Perm()&0077 != 0 || !os.SameFile(opened, named) {
		return errors.New("validator unit control changed")
	}
	raw := make([]byte, len(self.marker)+1)
	n, err := self.file.ReadAt(raw, 0)
	if err != nil && !errors.Is(err, io.EOF) || string(raw[:n]) != self.marker {
		return errors.New("validator unit control marker differs")
	}
	return nil
}

// Lifetime cleanup closes the descriptor but preserves the permanent name.
func (self *repairValidatorControl) close() error {
	if self == nil || self.file == nil {
		return nil
	}
	err := self.file.Close()
	self.file = nil
	return err
}
