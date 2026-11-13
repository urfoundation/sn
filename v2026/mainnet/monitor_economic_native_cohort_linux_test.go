//go:build linux

// Linux-only observations: sealed memfd engines, inotify read census and
// native change times reported through FileInfo.
package main

import (
	"reflect"
	"testing"
)

func TestMonitorNativeCohortRestoreAdmitsAll512SegmentsAndBothHeads(t *testing.T) {
	f := newMonitorNativeCohortFixture(t, 512)
	reference := f.plan(t)
	f.native.ctx = f.apply(t, reference, false)
	if record := f.native.record(t); !reflect.DeepEqual(record, f.record) {
		t.Fatal("full cross-root catalog changed")
	}
	wait := monitorNativeObserveAdmission(t, f.record.State.Archive.Segments)
	run := f.native.start(t, monitorServiceHooks{})
	wait(run)
	if event := run.next(t); !event.Current || event.State.ArchiveSegments != 512 || event.State.Cursor != f.record.State.Cursor || event.State.BatchCount != f.record.State.BatchCount {
		t.Fatal("full cross-root public continuation differs", event)
	}
	run.stop(t)
}
