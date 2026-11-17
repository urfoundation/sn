//go:build windows

package miner

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// minerProcessAlive is true when a process with this pid is still running,
// even if it belongs to another user.
func minerProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// it exists; it just isn't ours to inspect
		return true
	}
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	var code uint32
	if err := windows.GetExitCodeProcess(process, &code); err != nil {
		return true
	}
	return code == 259 // STILL_ACTIVE
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
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if err != nil {
		file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errMinerUpdateLocked
		}
		return nil, err
	}
	return func() { file.Close() }, nil
}
