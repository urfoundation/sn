//go:build linux

// Linux-only observations: sealed memfd engines, inotify read census and
// native change times reported through FileInfo.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestHistoricalReplayExecutesSealedImageAfterSourceReplacement(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x00")
	// Replace only this test-owned copy, never the test runner or sealed input.
	input, err := os.Open(request.Engine.Path)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(request.Job.Path), "engine")
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0500)
	if err != nil {
		input.Close()
		t.Fatal(err)
	}
	_, err = io.Copy(output, input)
	if err := errors.Join(err, input.Close(), output.Close()); err != nil {
		t.Fatal(err)
	}
	request.Engine.Path = path
	checked := false
	report, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{beforeStart: func(_ context.Context, image *os.File) {
		checked = true
		if _, err := image.WriteAt([]byte{0}, 0); !errors.Is(err, syscall.EPERM) {
			t.Error("pinned executable accepted a write", err)
		}
		seals, err := unix.FcntlInt(image.Fd(), unix.F_GET_SEALS, 0)
		if err != nil || seals&(unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) != unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL {
			t.Error("pinned executable lacks immutable seals", seals, err)
		}
		if err := os.Rename(path, path+".retained"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("unexecutable replacement"), 0500); err != nil {
			t.Fatal(err)
		}
	}})
	if err != nil || report == nil || !checked {
		t.Fatal("source replacement redirected the sealed executable", err, checked)
	}
}
