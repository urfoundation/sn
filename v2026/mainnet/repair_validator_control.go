// A fixed per-unit file lock excludes cooperating repair/activation owners
// across different incident journals. Privileged host administration stays trusted.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The marker is permanent and release-independent; it is never removed to
// replenish capacity. Each caller joins its commands before releasing this lock.
type repairValidatorControl struct {
	ctx    context.Context
	file   *os.File
	path   string
	marker string
	failed error
}

// Deriving the lock from the signed canonical fragment prevents per-incident
// paths from admitting simultaneous controllers for the same installed unit.
func (self *repairValidatorHost) control(ctx context.Context, unit repairValidatorUnit) (result *repairValidatorControl, resultErr error) {
	path := unit.File.Path + ".sn-control.lock"
	if ctx == nil {
		return nil, errors.New("validator unit control context unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := self.parents(path, self.rootUid); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	owner := &repairValidatorControl{ctx: ctx, file: os.NewFile(uintptr(fd), path), path: path, marker: "urnetwork-mainnet-validator-control-v1 " + unit.Name + "\n"}
	success := false
	defer func() {
		if !success {
			resultErr = errors.Join(resultErr, owner.close())
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
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("validator unit control ownership unavailable"), err)
	}
	if info.Size() == 0 {
		if n, err := owner.file.WriteString(owner.marker); err != nil {
			return nil, err
		} else if n != len(owner.marker) {
			return nil, io.ErrShortWrite
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
	if self.failed != nil {
		return self.failed
	}
	if self.ctx == nil {
		return errors.New("validator unit control context is absent")
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	opened, err := self.file.Stat()
	if err := repairValidatorObservation(self.ctx, "control-open-stat", err); err != nil {
		return self.observationError("cannot inspect validator unit control descriptor", err, false)
	}
	named, nameErr := os.Lstat(self.path)
	if err := repairValidatorObservation(self.ctx, "control-name-stat", nameErr); err != nil {
		return self.observationError("cannot inspect named validator unit control", err, true)
	}
	if !named.Mode().IsRegular() || named.Mode().Perm()&0077 != 0 || !os.SameFile(opened, named) {
		return self.observationError("validator unit control changed", durablevolume.ErrIdentity, false)
	}
	raw := make([]byte, len(self.marker)+1)
	n, err := self.file.ReadAt(raw, 0)
	if err == io.EOF {
		err = nil
	}
	if err := repairValidatorObservation(self.ctx, "control-marker-read", err); err != nil {
		return self.observationError("cannot read validator unit control marker", err, false)
	}
	if string(raw[:n]) != self.marker {
		return self.observationError("validator unit control marker differs", durablevolume.ErrIdentity, false)
	}
	return nil
}

// Failed observations preserve retryability; confirmed loss stays sticky until
// this owner closes. Replacing a name back cannot restore its action authority.
func (self *repairValidatorControl) observationError(message string, cause error, retainedName bool) error {
	err := repairValidatorObservationError(message, cause, retainedName)
	if errors.Is(err, durablevolume.ErrIdentity) {
		self.failed = err
	}
	return err
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
