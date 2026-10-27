//go:build windows

package miner

import (
	"log"
	"os"
	"runtime/debug"

	"golang.org/x/sys/windows"
)

// redirectStderrToFile points the process's standard error at the file
// (appending), so glog, the standard log and Go's own crash output land there.
// Windows has no dup2: the standard error handle, os.Stderr and the standard
// logger are each pointed at the file. It returns the terminal stderr was,
// which also gets a copy of any crash.
func redirectStderrToFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	terminal := os.Stderr
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(file.Fd())); err != nil {
		file.Close()
		return nil, err
	}
	os.Stderr = file
	log.SetOutput(file)
	_ = debug.SetCrashOutput(terminal, debug.CrashOptions{})
	return terminal, nil
}
