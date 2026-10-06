//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Paths are fixed before original enrollment/publication. The longest profile
// deliberately exercises JSON expansion as well as the runtime byte limit.
func monitorHistoryRestoreTestPath(t *testing.T, root, name, profile string) string {
	t.Helper()
	if profile == "nested" {
		return filepath.Join(root, "history", "epoch", name)
	}
	if profile != "deepest" && profile != "longest" {
		t.Fatal("unknown explicit namespace profile", profile)
	}
	parts := make([]string, 31)
	for index := range parts {
		parts[index] = "d"
	}
	if profile == "longest" {
		name += strings.Repeat("\x01", 155-len(name))
		remaining := maximumMonitorHistoryPath - len(root) - len(name) - 32 - len(parts)
		if remaining < 0 {
			t.Fatal("owned scratch root leaves no accepted longest-path fixture", len(root))
		}
		for index := range parts {
			additional := min(254, remaining)
			parts[index] += strings.Repeat("\x01", additional)
			remaining -= additional
		}
		if remaining != 0 {
			t.Fatal("longest namespace could not fit accepted components", remaining)
		}
	}
	path := filepath.Join(root, filepath.Join(parts...), name)
	if !monitorHistoryPath(path) || profile == "longest" && len(path) != maximumMonitorHistoryPath {
		t.Fatal("namespace fixture is outside original runtime admission", profile, len(path))
	}
	return path
}

func TestMonitorNativeRestoreDeepestInventoryNamespaceContinues(t *testing.T) {
	f := newMonitorNativeCohortFixtureNamespace(t, 1, "deepest")
	original := f.record.State.Archive.Segments[0]
	relative, err := filepath.Rel(f.sources[1].root, original.Path)
	if err != nil || len(strings.Split(relative, "/")) != 32 {
		t.Fatal("fixture did not reach exact inventory depth", err, relative)
	}
	f.native.ctx = f.apply(t, f.plan(t), true)
	if record := f.native.record(t); !reflect.DeepEqual(record, f.record) || record.State.Archive.Segments[0] != original {
		t.Fatal("deep restore changed original authority or economics")
	}
	run := f.native.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.BatchCount != 2 || event.State.PendingThrough != nil || event.State.ArchiveSegments != 1 || monitorEconomicTestFee(event) != "12" {
		t.Fatal("deep original pending outcome did not continue", event)
	}
	run.stop(t)
}

func TestMonitorEvmRestoreLongestEscapedNamespaceContinues(t *testing.T) {
	f := newMonitorEvmRestoreFixtureNamespace(t, 2, 1, "longest")
	original := f.record.State.Archive.Segments[0]
	relative, err := filepath.Rel(f.sources[1].root, original.Path)
	if err != nil || len(original.Path) != maximumMonitorHistoryPath || len(filepath.Base(original.Path)) != 155 || len(strings.Split(relative, "/")) != 32 {
		t.Fatal("fixture did not reach exact runtime and inventory limits", err, len(original.Path))
	}
	f.apply(t, f.plan(t), true)
	if f.evm.record(t).State.Archive.Segments[0] != original {
		t.Fatal("maximum path restore rewrote original history reference")
	}
	monitorEvmRestorePendingContinues(t, f)
}

// A valid original tree precedes each malformed request. Expanding any
// independent namespace dimension must refuse without reserving either root.
func TestMonitorHistoryRestoreNamespaceOverflowRefusesBeforeEffects(t *testing.T) {
	f := newMonitorEvmRestoreFixtureNamespace(t, 2, 1, "deepest")
	accepted := f.plan(t)
	valid, err := json.Marshal(f.request)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"depth", "original-path"} {
		var request monitorEvmRestoreCohortRequest
		if err := json.Unmarshal(valid, &request); err != nil {
			t.Fatal(err)
		}
		if fault == "depth" {
			request.Preparations[1].Limits.MaxDepth = 33
		} else {
			request.Original.Path = "/" + strings.Repeat("d", maximumMonitorHistoryPath)
		}
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.targets[0].target.metadata, "namespace-overflow-"+fault+".json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		var output, diagnostic bytes.Buffer
		code := runMain(f.evm.ctx, []string{"monitor-evm-archive", "restore-cohort-plan", "--request", path, "--request-sha256", monitorReadDigest(raw)}, &output, &diagnostic)
		if code == 0 || output.Len() != 0 {
			t.Fatal("namespace overflow acquired target authority", fault, code, diagnostic.String())
		}
		f.unmodifiedTargets(t)
	}
	// Refused requests do not poison the retained original exact plan.
	f.apply(t, accepted, false)
	monitorEvmRestorePendingContinues(t, f)
}
