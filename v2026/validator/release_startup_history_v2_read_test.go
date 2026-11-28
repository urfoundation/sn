//go:build linux || darwin

// These physical controls test bounded inventory only. Their opaque bytes
// deliberately confer no cut, cursor or eligibility authentication verdict.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The selected metadata limits are explicit local test authority, not new
// runtime defaults. Real immutable publication creates all input fixtures.
func releaseEvidenceV2HistoryReadTestBounds() ReleaseEvidenceV2Bounds {
	return ReleaseEvidenceV2Bounds{MaxHistoryBytes: 4096, MaxInputJournalBytes: 1024, MaxClosureBytes: 2048}
}

// The physical coordinator is separately owned; no test repairs a candidate
// symlink or opens a workspace root as a private history namespace.
func newReleaseEvidenceV2HistoryReadTestFixture(t *testing.T) (string, []byte) {
	t.Helper()
	coordinator := newAttemptSettlementRuntimeV2TestStateDir(t)
	encoded := []byte("{\"opaque_history_input\":true}\n")
	for _, value := range [][2]uint64{{7, 9}, {8, 11}} {
		if err := writeReleaseMeasurementInputV2(releaseMeasurementInputV2Path(coordinator, value[0], value[1]), encoded, 1024); err != nil {
			t.Fatal(err)
		}
	}
	return coordinator, encoded
}

// The latest independent input is included without any steering intent,
// snapshot generation or preselected native-epoch prefix.
func TestReleaseEvidenceV2HistoryReadIncludesCrashBeforeBegin(t *testing.T) {
	t.Parallel()
	coordinator, encoded := newReleaseEvidenceV2HistoryReadTestFixture(t)
	owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, releaseEvidenceV2HistoryReadTestBounds(), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.close(); err != nil {
			t.Error(err)
		}
	}()
	if len(owner.inputs) != 2 || len(owner.closures) != 0 || owner.inputs[0].epoch != 7 || owner.inputs[0].noID != 9 || owner.inputs[1].epoch != 8 || owner.inputs[1].noID != 11 || !bytes.Equal(owner.inputs[0].encoded, encoded) || !bytes.Equal(owner.inputs[1].encoded, encoded) {
		t.Fatal("complete independent ordinary-input census differs")
	}
	for _, path := range []string{filepath.Join(coordinator, "steering-intents.json"), filepath.Join(coordinator, "stats.json"), filepath.Join(coordinator, "settlement-closures-v2")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read-only history created an absent control path: %s: %v", path, err)
		}
	}
}

// Legacy names remain explicit raw inputs for the separate authenticated
// migration path. Neither codec can silently reinterpret the other schema.
func TestReleaseEvidenceV2HistoryReadSeparatesLegacyAndTerminalNamespaces(t *testing.T) {
	t.Parallel()
	coordinator, encoded := newReleaseEvidenceV2HistoryReadTestFixture(t)
	legacy := releaseMeasurementInputPath(coordinator, 6, 9)
	if err := writeReleaseMeasurementInputV2(legacy, encoded, 1024); err != nil {
		t.Fatal(err)
	}
	path := AttemptSettlementClosureV2Path(coordinator, 42)
	if err := ensurePrivateStateDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(path, encoded, 0o400); err != nil {
		t.Fatal(err)
	}
	owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, releaseEvidenceV2HistoryReadTestBounds(), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(owner.inputs) != 3 || !owner.inputs[0].legacy || owner.inputs[0].epoch != 6 || len(owner.closures) != 1 || owner.closures[0].epoch != 42 || !bytes.Equal(owner.closures[0].encoded, encoded) {
		t.Fatal("explicit historical namespace routing differs")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
}

// Initial absence is retained under a real parent. A later-created input
// namespace cannot be treated as an authenticated empty-history result.
func TestReleaseEvidenceV2HistoryReadRetainsMissingNamespace(t *testing.T) {
	t.Parallel()
	coordinator := newAttemptSettlementRuntimeV2TestStateDir(t)
	owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, releaseEvidenceV2HistoryReadTestBounds(), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeReleaseMeasurementInputV2(releaseMeasurementInputV2Path(coordinator, 7, 9), []byte("{}\n"), 1024); err != nil {
		t.Fatal(err)
	}
	if err := owner.check(); err == nil {
		t.Fatal("late input namespace retained an empty-history result")
	}
	if err := owner.close(); err == nil {
		t.Fatal("closing missing-namespace owner discarded its changed witness")
	}
}

// File bytes and complete name membership stay owned throughout later replay.
// A new same-sized member cannot hide behind unchanged directory identity.
func TestReleaseEvidenceV2HistoryReadRejectsLaterCensusExtension(t *testing.T) {
	t.Parallel()
	coordinator, encoded := newReleaseEvidenceV2HistoryReadTestFixture(t)
	owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, releaseEvidenceV2HistoryReadTestBounds(), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeReleaseMeasurementInputV2(releaseMeasurementInputV2Path(coordinator, 9, 9), encoded, 1024); err != nil {
		t.Fatal(err)
	}
	if err := owner.check(); err == nil {
		t.Fatal("later native cut was omitted from retained history census")
	}
	if err := owner.close(); err == nil {
		t.Fatal("final census witness omitted a later native cut")
	}
}

// A real read callback replaces an earlier immutable inode while the later
// operator is read; every already-acquired namespace still has to agree.
func TestReleaseEvidenceV2HistoryReadRejectsEarlierMemberReplacement(t *testing.T) {
	t.Parallel()
	coordinator, encoded := newReleaseEvidenceV2HistoryReadTestFixture(t)
	first := releaseMeasurementInputV2Path(coordinator, 7, 9)
	last := releaseMeasurementInputV2Path(coordinator, 8, 11)
	changed := false
	hooks := releaseMeasurementInputV2ReadHooks{step: func(phase string, file *os.File, _ int) error {
		if phase == "leaf-read" && file.Name() == last && !changed {
			changed = true
			return atomicStateWrite(first, encoded, 0o600)
		}
		return nil
	}}
	owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, releaseEvidenceV2HistoryReadTestBounds(), hooks)
	if err == nil || owner != nil || !changed {
		t.Fatalf("earlier history replacement escaped: changed=%t, %v", changed, err)
	}
}

// No member can borrow another member's allowance, and admitting all files
// independently cannot waive the separate complete-history byte bound.
func TestReleaseEvidenceV2HistoryReadEnforcesAggregateAndMemberBounds(t *testing.T) {
	t.Parallel()
	coordinator, encoded := newReleaseEvidenceV2HistoryReadTestFixture(t)
	for _, bound := range []ReleaseEvidenceV2Bounds{
		{MaxHistoryBytes: uint64(len(encoded)*2 - 1), MaxInputJournalBytes: 1024, MaxClosureBytes: 2048},
		{MaxHistoryBytes: 4096, MaxInputJournalBytes: uint64(len(encoded) - 1), MaxClosureBytes: 2048},
	} {
		owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, bound, releaseMeasurementInputV2ReadHooks{})
		if err == nil || owner != nil {
			t.Fatalf("bounded history admitted excess bytes: %v", err)
		}
	}
}

// Unknown aliases, public permissions and symlink leaves fail before semantic
// history admission; none is treated as a missing committed input.
func TestReleaseEvidenceV2HistoryReadRejectsUnownedOrAliasedMember(t *testing.T) {
	t.Parallel()
	for _, change := range []func(string, string) error{
		func(path, _ string) error { return os.Chmod(path, 0o644) },
		func(path, _ string) error { return os.Rename(path, path+".alias") },
		func(path, target string) error {
			if err := os.Rename(path, target); err != nil {
				return err
			}
			return os.Symlink(target, path)
		},
	} {
		coordinator, _ := newReleaseEvidenceV2HistoryReadTestFixture(t)
		if err := change(releaseMeasurementInputV2Path(coordinator, 7, 9), filepath.Join(coordinator, "preserved-input")); err != nil {
			t.Fatal(err)
		}
		owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, releaseEvidenceV2HistoryReadTestBounds(), releaseMeasurementInputV2ReadHooks{})
		if err == nil || owner != nil {
			t.Fatalf("unowned or aliased history member escaped: %v", err)
		}
	}
}

// Incomplete temporary publication is neither promoted nor deleted. It still
// consumes the complete directory census and byte allowance on every check.
func TestReleaseEvidenceV2HistoryReadRetainsCrashTemporaryWithoutPromotion(t *testing.T) {
	t.Parallel()
	coordinator, _ := newReleaseEvidenceV2HistoryReadTestFixture(t)
	path := filepath.Join(coordinator, "measurements", "inputs", ".compact-input-"+strings.Repeat("ab", 16))
	encoded := []byte("incomplete uncommitted bytes")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, releaseEvidenceV2HistoryReadTestBounds(), releaseMeasurementInputV2ReadHooks{})
	if err != nil || len(owner.inputs) != 2 {
		t.Fatalf("crash temporary was promoted or blocked inventory: %v", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, encoded) {
		t.Fatalf("read-only history removed or rewrote a crash temporary: %v", err)
	}
}

// A late real Close error is joined; returning successful opaque bytes would
// let a later semantic reader consume an incompletely released acquisition.
func TestReleaseEvidenceV2HistoryReadJoinsActualCloseFailure(t *testing.T) {
	t.Parallel()
	coordinator, _ := newReleaseEvidenceV2HistoryReadTestFixture(t)
	cause := errors.New("history actual close observer failure")
	closes := 0
	hooks := releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		closes++
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			return errors.New("history observer preceded actual descriptor Close")
		}
		return cause
	}}
	owner, err := readReleaseEvidenceV2HistoryFiles(t.Context(), coordinator, releaseEvidenceV2HistoryReadTestBounds(), hooks)
	if !errors.Is(err, cause) || owner != nil || closes < 2 {
		t.Fatalf("partial acquisition lost an actual Close failure: closes=%d, %v", closes, err)
	}
}

// Cancellation during the last real member read clears the complete result
// and joins every retained directory instead of leaking a partial inventory.
func TestReleaseEvidenceV2HistoryReadJoinsCancellationDuringLastInput(t *testing.T) {
	t.Parallel()
	coordinator, _ := newReleaseEvidenceV2HistoryReadTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	last := releaseMeasurementInputV2Path(coordinator, 8, 11)
	canceled := false
	hooks := releaseMeasurementInputV2ReadHooks{step: func(phase string, file *os.File, _ int) error {
		if phase == "leaf-read" && file.Name() == last {
			canceled = true
			cancel()
		}
		return nil
	}}
	owner, err := readReleaseEvidenceV2HistoryFiles(ctx, coordinator, releaseEvidenceV2HistoryReadTestBounds(), hooks)
	if !errors.Is(err, context.Canceled) || owner != nil || !canceled {
		t.Fatalf("canceled complete inventory escaped: canceled=%t, %v", canceled, err)
	}
}
