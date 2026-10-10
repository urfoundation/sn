//go:build linux

// Linux-only observations: sealed memfd engines, inotify read census and
// native change times reported through FileInfo.
package main

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Inotify delivers actual access events synchronously with the filesystem
// operations. Nonblocking reads test an empty queue, never elapsed time.
func TestMonitorEvmArchiveUnchangedChecksDoNotRereadPayload(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, false)
	plan, args := f.plan(t)
	f.apply(t, args)
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, f.archive, unix.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	owner, raw, err := openMonitorHistoryReader(f.evm.ctx, plan.Archive)
	if err != nil || !bytes.Equal(raw, f.original) {
		t.Fatal("archive admission did not read original payload", err)
	}
	defer owner.close()
	buffer := make([]byte, 4096)
	if n, err := unix.Read(fd, buffer); err != nil || n == 0 {
		t.Fatal("actual admission access was not observed", n, err)
	}
	for index := 0; index < 16; index++ {
		if err := owner.check(); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := unix.Read(fd, buffer); n > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("unchanged archive reread its payload", n, err)
	}
	file, err := os.OpenFile(f.archive, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{'!'}, 0); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	if err := owner.check(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("changed immutable payload retained cache admission", err)
	}
	if n, err := unix.Read(fd, buffer); n > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("invalid immutable member was reread before refusal", n, err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if replacement, _, err := openMonitorHistoryReader(f.evm.ctx, plan.Archive); err == nil || replacement != nil {
		t.Fatal("reopen admitted altered original archive", err)
	}
}
