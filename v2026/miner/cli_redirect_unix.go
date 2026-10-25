//go:build linux || darwin

package miner

import (
	"os"
	"runtime/debug"

	"golang.org/x/sys/unix"
)

// redirectStderrToFile points file descriptor 2 at the file (appending), so
// glog, the standard log and Go's own crash output land there. It returns
// the terminal stderr was, which also gets a copy of any crash.
func redirectStderrToFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	saved, err := unix.Dup(int(os.Stderr.Fd()))
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(saved)
	if err := unix.Dup2(int(file.Fd()), int(os.Stderr.Fd())); err != nil {
		unix.Close(saved)
		return nil, err
	}
	terminal := os.NewFile(uintptr(saved), "/dev/tty")
	_ = debug.SetCrashOutput(terminal, debug.CrashOptions{})
	return terminal, nil
}
