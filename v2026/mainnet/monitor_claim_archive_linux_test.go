//go:build linux

// Linux-only observations: sealed memfd engines, inotify read census and
// native change times reported through FileInfo.
package main

import (
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMonitorClaimArchiveActualHistoryReadOnceAndCustodyAfterAdmission(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	_, args := f.plan(t)
	f.apply(t, args)
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, f.archive, unix.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	f.healthyPeer(t)
	f.advance(nil)
	run := f.start(t, monitorServiceHooks{})
	if !run.next(t).Current {
		t.Fatal("archive was not admitted")
	}
	buffer := make([]byte, 4096)
	if count, err := unix.Read(fd, buffer); err != nil || count <= 0 {
		t.Fatal("actual archive admission never read original bytes", count, err)
	}
	for range 3 {
		f.advance(nil)
		run.resume <- struct{}{}
		if !run.next(t).Current {
			t.Fatal("unchanged archive lost current role")
		}
	}
	if count, err := unix.Read(fd, buffer); count > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("hot claim samples reread complete archive payload", count, err)
	}
	raw, err := os.ReadFile(f.archive)
	if err != nil {
		t.Fatal(err)
	}
	replacement := f.archive + ".replacement"
	if err := os.WriteFile(replacement, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, f.archive); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := os.ReadFile(f.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reads := f.requests.Load()
	run.resume <- struct{}{}
	select {
	case event := <-run.sink.events:
		if event.Status != "identity" || event.Current {
			t.Fatal("changed archive custody admitted another source sample", event)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("changed archive custody did not stop affected role")
	}
	f.refusedWhilePeerContinues(t, run, reads, checkpoint)
}
