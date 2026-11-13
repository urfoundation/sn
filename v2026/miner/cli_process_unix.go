//go:build linux || darwin

package miner

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// minerProcessAlive is true when a process with this pid exists, even if it
// belongs to another user.
func minerProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// lockMinerUpdate keeps two updates (the timer and a manual one) apart.
func lockMinerUpdate() (func(), error) {
	path, err := providerStatePath("miner-update.lock")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errMinerUpdateLocked
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}, nil
}
