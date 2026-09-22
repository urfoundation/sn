//go:build linux || darwin

// Setup discovery shares the real immutable reader's absence witness. Forced
// filesystem transitions keep late failures distinct from optional preflight.
package validator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Only an initially missing path, after all custody checks, is optional.
func TestReleaseEvidenceV2SetupFileInitialAbsence(t *testing.T) {
	t.Parallel()
	stateDir := newAttemptLedgerDiskTestStateDir(t)
	for _, name := range []string{"prepared.json", filepath.Join("unstarted", "completed.json")} {
		encoded, err := ReadReleaseEvidenceV2SetupFile(t.Context(), filepath.Join(stateDir, name), 1024)
		if encoded != nil || !ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
			t.Fatalf("clean missing %s lost its explicit optional state: %v", name, err)
		}
		if !ReleaseEvidenceV2SetupFileInitiallyMissing(fmt.Errorf("retained receipt: %w", err)) {
			t.Fatal("contextual wrapping lost verified initial absence")
		}
		for _, changed := range []error{nil, os.ErrNotExist, errors.Join(err, context.Canceled), errors.Join(err, errors.New("synthetic close failure"))} {
			if ReleaseEvidenceV2SetupFileInitiallyMissing(changed) {
				t.Fatalf("mixed or unwitnessed failure authorized fresh setup: %v", changed)
			}
		}
	}
}

// A receipt removed after observation must remain required evidence failure.
func TestReleaseEvidenceV2SetupFileLateDisappearance(t *testing.T) {
	t.Parallel()
	path := filepath.Join(newAttemptLedgerDiskTestStateDir(t), "completed.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed := 0
	encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, 1024, releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation != "leaf-observed" {
			return nil
		}
		observed++
		return os.Rename(path, path+"-preserved")
	}})
	if observed != 1 || encoded != nil || !errors.Is(err, os.ErrNotExist) || ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		t.Fatalf("late disappearance was accepted as unstarted setup: observed=%d error=%v", observed, err)
	}
}

// A real parent close precedes injected cancellation and failure. Neither
// absence nor an empty receipt can hide those failed ownership checks.
func TestReleaseEvidenceV2SetupFileCloseFailure(t *testing.T) {
	t.Parallel()
	for _, empty := range []bool{false, true} {
		path := filepath.Join(newAttemptLedgerDiskTestStateDir(t), "prepared.json")
		if empty {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		failure := errors.New("synthetic setup close failure")
		closed := 0
		encoded, err := readReleaseMeasurementInputV2Context(ctx, path, 1024, releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
			closed++
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Error("close observer received an open descriptor")
			}
			cancel()
			return failure
		}})
		cancel()
		if closed == 0 || encoded != nil || !errors.Is(err, failure) || !errors.Is(err, context.Canceled) || ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
			t.Fatalf("failed setup custody became optional: empty=%t closed=%d error=%v", empty, closed, err)
		}
	}
}
