//go:build linux

// Linux-only observations: sealed memfd engines, inotify read census and
// native change times reported through FileInfo.
package main

import (
	"testing"
)

func TestMonitorNativeRestoreFull512HistoryAndBothActiveHeads(t *testing.T) {
	f := newMonitorNativeRestoreFixture(t, 512)
	path, hash := f.plan(t)
	f.apply(t, path, hash, false)
	admission := monitorNativeObserveAdmission(t, f.record.State.Archive.Segments)
	run := f.native.start(t, monitorServiceHooks{})
	admission(run)
	event := run.next(t)
	if !event.Current || event.State.ArchiveSegments != 512 || event.State.ArchivedEvents != 512 || event.State.BatchCount != 512 || event.State.Cursor.Number != 612 || event.State.ObservedAlpha != "5120" || event.State.ArchiveSegmentCapacity != 512 {
		t.Fatal("full accepted archive did not reopen through actual public monitor", event)
	}
	run.stop(t)
}
