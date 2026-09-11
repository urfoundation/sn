//go:build linux || darwin

package validator

// Real FIFO, symlink and directory replacements force native custody edges.
// No sleep, scheduler timing or fabricated replay verdict establishes a pass.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A pathname FIFO cannot block the native directory acquisition itself.
func TestAttemptSettlementRuntimeV2DirectoryFIFORefusedNatively(t *testing.T) {
	path := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "directory")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openAttemptSettlementV2Root(path)
	if root != nil || err == nil {
		t.Fatalf("FIFO acquired as directory: %v", err)
	}
}

// O_NONBLOCK is exercised by an actual regular-to-FIFO swap after admission.
func TestAttemptSettlementRuntimeV2LeafFIFOSwapRefusedNatively(t *testing.T) {
	dir := newAttemptSettlementRuntimeV2TestStateDir(t)
	path := filepath.Join(dir, "stats.json")
	if err := os.WriteFile(path, []byte("owned bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openAttemptSettlementV2Root(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.close()
	physical := attemptSettlementV2PhysicalIO()
	steps := 0
	physical.step = func(step string) error {
		if step != "before-open" {
			return nil
		}
		steps++
		if err := os.Rename(path, path+".preserved"); err != nil {
			return err
		}
		return unix.Mkfifo(path, 0o600)
	}
	data, exists, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	if err == nil || !exists || data != nil || steps != 1 {
		t.Fatalf("replacement FIFO was read or treated absent: %v/%t/%d", err, exists, steps)
	}
}

// The actual native open refuses even a symlink whose destination is inside
// the same directory and has byte-identical private contents.
func TestAttemptSettlementRuntimeV2InRootSymlinkSwapRefusedNatively(t *testing.T) {
	dir := newAttemptSettlementRuntimeV2TestStateDir(t)
	path := filepath.Join(dir, "stats.json")
	if err := os.WriteFile(path, []byte("owned bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openAttemptSettlementV2Root(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.close()
	physical := attemptSettlementV2PhysicalIO()
	physical.step = func(step string) error {
		if step != "before-open" {
			return nil
		}
		if err := os.Rename(path, path+".preserved"); err != nil {
			return err
		}
		return os.Symlink("stats.json.preserved", path)
	}
	data, exists, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	if !errors.Is(err, unix.ELOOP) || !exists || data != nil {
		t.Fatalf("in-root symlink was followed: %v/%t", err, exists)
	}
}

// Both before-open and after-read disappearance retain occupied ownership.
// The original bytes are preserved separately, not overwritten or recreated.
func TestAttemptSettlementRuntimeV2LateMissingLeafNeverBecomesAbsence(t *testing.T) {
	for _, boundary := range []string{"before-open", "after-read"} {
		dir := newAttemptSettlementRuntimeV2TestStateDir(t)
		path := filepath.Join(dir, "stats.json")
		if err := os.WriteFile(path, []byte("owned bytes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := openAttemptSettlementV2Root(dir)
		if err != nil {
			t.Fatal(err)
		}
		physical := attemptSettlementV2PhysicalIO()
		physical.step = func(step string) error {
			if step == boundary {
				return os.Rename(path, path+".preserved")
			}
			return nil
		}
		data, exists, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
		closeErr := root.close()
		if !errors.Is(err, os.ErrNotExist) || !exists || data != nil || closeErr != nil {
			t.Fatalf("%s missing occupied leaf lost custody: %v/%t close=%v", boundary, err, exists, closeErr)
		}
	}
}

// Native child O_DIRECTORY refuses a FIFO inserted after directory lstat.
func TestAttemptSettlementRuntimeV2ClosureDirectoryFIFOSwapRefused(t *testing.T) {
	coordinator := newAttemptSettlementRuntimeV2TestStateDir(t)
	path := filepath.Join(coordinator, "settlement-closures-v2")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	physical := attemptSettlementV2PhysicalIO()
	physical.step = func(step string) error {
		if step != "before-closure-directory-open" {
			return nil
		}
		if err := os.Rename(path, path+".preserved"); err != nil {
			return err
		}
		return unix.Mkfifo(path, 0o600)
	}
	root, err := openAttemptSettlementV2ClosureDirectory(coordinator, false, physical)
	if err == nil || root != nil {
		t.Fatalf("replaced child directory acquired: %v", err)
	}
}

// A snapshot callback cannot redirect the next operator's write into a new
// pathname occupant. Every public operator stays at the old reserved image.
func TestAttemptSettlementRuntimeV2CallbackDirectorySwapPreservesOwners(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	second := fixture.participants[1].StateDir
	oldDisk, err := os.ReadFile(filepath.Join(second, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, data []byte) error {
		writes++
		if err := writeAttemptSettlementV2OwnedState(root, name, data); err != nil {
			return err
		}
		if err := os.Rename(second, second+".preserved"); err != nil {
			return err
		}
		return os.Mkdir(second, 0o700)
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || closure != nil || writes != 1 || !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatalf("callback redirect partially published: %v/%d", err, writes)
	}
	fixture.assertReserved(t, 43)
	if _, err := os.Lstat(filepath.Join(second, "stats.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new occupant received old owner's snapshot: %v", err)
	}
	retained, err := os.ReadFile(filepath.Join(second+".preserved", "stats.json"))
	if err != nil || !bytes.Equal(retained, oldDisk) {
		t.Fatalf("retained second image changed: %v", err)
	}
	_ = fixture.transaction(t)
}

// This explicit error-contract control adds cancellation and a cause only
// after the real temporary closure file has completed its actual close.
func TestAttemptSettlementRuntimeV2LateClosureCloseRetainsCauseAndCancellation(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cause := errors.New("post-close compact file contract refusal")
	physical := attemptSettlementV2PhysicalIO()
	closes := 0
	physical.closeFile = func(file *os.File) error {
		err := file.Close()
		if strings.HasPrefix(filepath.Base(file.Name()), ".closure-") {
			closes++
			cancel()
			return errors.Join(err, cause)
		}
		return err
	}
	closure, err := advanceAttemptSettlementEpochV2(ctx, fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if closure != nil || !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || closes != 1 {
		t.Fatalf("late file close/cancel cause lost: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 43)
	transaction := fixture.transaction(t)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// A late directory-sync acknowledgement after the real immutable rename
// retains the journal; retry must finish exactly those already-present bytes.
func TestAttemptSettlementRuntimeV2ClosureDirectorySyncFailureIsRetryable(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	physical := attemptSettlementV2PhysicalIO()
	cause := errors.New("post-sync compact directory contract refusal")
	syncs := 0
	physical.syncDirectory = func(file *os.File) error {
		err := file.Sync()
		if filepath.Base(file.Name()) == "settlement-closures-v2" {
			syncs++
			return errors.Join(err, cause)
		}
		return err
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if closure != nil || !errors.Is(err, cause) || syncs != 1 {
		t.Fatalf("late directory sync lost: %v/%d", err, syncs)
	}
	fixture.assertReserved(t, 43)
	transaction := fixture.transaction(t)
	data, err := os.ReadFile(AttemptSettlementClosureV2Path(fixture.coordinator, 42))
	if err != nil || !bytes.Equal(data, transaction.ClosureJSON) {
		t.Fatalf("actual renamed immutable bytes were not retained: %v", err)
	}
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// Real descriptor double-close forces an actual os.PathError at the last
// owner cleanup, after journal removal. Admission stays closed until retry.
func TestAttemptSettlementRuntimeV2PhysicalOwnerCloseFailureKeepsAdmissionClosed(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	physical := attemptSettlementV2PhysicalIO()
	var physicalErr error
	closed := map[string]bool{}
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		if closed[path] {
			t.Error("owner was closed repeatedly")
		}
		closed[path] = true
		if path == fixture.participants[0].StateDir {
			if err := root.file.Close(); err != nil {
				return err
			}
			physicalErr = root.close()
			return physicalErr
		}
		return root.close()
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	var pathErr *os.PathError
	if closure != nil || physicalErr == nil || !errors.Is(err, physicalErr) || !errors.As(physicalErr, &pathErr) || len(closed) != 4 {
		t.Fatalf("actual owner close lost or owners abandoned: %v owners=%d", err, len(closed))
	}
	fixture.assertReserved(t, 43)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("post-close retry changed exact completed bytes")
	}
}

// Cancellation at the final actual directory close is observed before the
// coordinator releases its completed generation's admission reservation.
func TestAttemptSettlementRuntimeV2FinalOwnerCloseCancellationKeepsGate(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	physical := attemptSettlementV2PhysicalIO()
	closes := 0
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		err := root.close()
		if path == fixture.participants[0].StateDir {
			closes++
			cancel()
		}
		return err
	}
	closure, err := advanceAttemptSettlementEpochV2(ctx, fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if closure != nil || !errors.Is(err, context.Canceled) || closes != 1 {
		t.Fatalf("late owner cancellation was ignored: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 43)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("late-cancel retry changed exact completed state")
	}
}

// A real close callback can replace the occupied pathname before returning.
// Successful earlier reads do not turn that late disappearance into absence.
func TestAttemptSettlementRuntimeV2MetadataCloseReplacementIsRefused(t *testing.T) {
	dir := newAttemptSettlementRuntimeV2TestStateDir(t)
	path := filepath.Join(dir, "stats.json")
	if err := os.WriteFile(path, []byte("owned bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openAttemptSettlementV2Root(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.close()
	physical := attemptSettlementV2PhysicalIO()
	closes := 0
	physical.closeFile = func(file *os.File) error {
		closes++
		return errors.Join(file.Close(), os.Rename(path, path+".preserved"))
	}
	data, exists, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	if !errors.Is(err, os.ErrNotExist) || !exists || data != nil || closes != 1 {
		t.Fatalf("post-close replacement returned stale bytes or absence: %v/%t/%d", err, exists, closes)
	}
}
