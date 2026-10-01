//go:build linux || darwin

package validator

// Projection qualification forces exact prefix conflicts, durable replacement
// ambiguity, cancellation and incremental work through the real disk ledger.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// Proof bytes are independent fixture expectations, never read back from the
// projection whose completeness the tests are establishing.
func attemptProofStreamTestExpected(t *testing.T, records []AttemptRecord) []byte {
	t.Helper()
	var expected []byte
	for _, record := range records {
		if record.Disposition != AttemptDispositionComplete {
			continue
		}
		raw, err := json.Marshal(record.Proof)
		if err != nil {
			t.Fatal(err)
		}
		expected = append(expected, raw...)
		expected = append(expected, '\n')
	}
	return expected
}

// A matching partial final line is a repairable prefix; every conflicting,
// duplicate, reordered or orphan byte remains a visible failure.
func TestDiskAttemptProofProjectionExactPrefixAndTornSuffix(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	expected := attemptProofStreamTestExpected(t, fixture.recordTs)
	first := attemptProofStreamTestExpected(t, fixture.recordTs[:8])
	for _, variation := range []string{"missing", "empty", "first", "torn", "complete", "conflict", "duplicate", "reorder", "orphan"} {
		dir := newAttemptLedgerDiskTestStateDir(t)
		if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
			t.Fatal(err)
		}
		ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
		store, err := NewProofStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		var actual []byte
		valid := true
		switch variation {
		case "first":
			actual = first
		case "torn":
			actual = expected[:len(first)+(len(expected)-len(first))/2]
		case "complete":
			actual = expected
		case "conflict":
			actual = append([]byte(nil), expected...)
			actual[10] ^= 1
			valid = false
		case "duplicate":
			actual = append(append([]byte(nil), first...), first...)
			valid = false
		case "reorder":
			actual = append(append([]byte(nil), expected[len(first):]...), first...)
			valid = false
		case "orphan":
			actual = append(append([]byte(nil), expected...), 'x')
			valid = false
		}
		if variation != "missing" {
			if err := os.WriteFile(store.path, actual, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		err = store.ReconcileAttemptProofsContext(context.Background(), ledger)
		if valid && err != nil || !valid && err == nil {
			t.Fatalf("%s reconciliation: %v", variation, err)
		}
		got, readErr := os.ReadFile(store.path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if valid && !bytes.Equal(got, expected) || !valid && !bytes.Equal(got, actual) {
			t.Fatalf("%s projection bytes changed incorrectly", variation)
		}
		if !valid && store.projectionFault == nil {
			t.Fatalf("%s conflict did not fault the owner", variation)
		}
		if err := ledger.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// An error after rename is ambiguous even when the new complete bytes can be
// seen immediately. Only a fresh owner and full comparison may resume.
func TestDiskAttemptProofProjectionReplacementFaultRequiresNewOwner(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	store, err := NewProofStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	expectedError := errors.New("replacement directory sync failed")
	store.projectionStep = func(operation, name string) error {
		if operation == "directory-sync" && name == "proof-replace" {
			return expectedError
		}
		return nil
	}
	if err := store.ReconcileAttemptProofsContext(context.Background(), ledger); !errors.Is(err, expectedError) {
		t.Fatalf("replacement fault: %v", err)
	}
	expected := attemptProofStreamTestExpected(t, fixture.recordTs)
	actual, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("persisted replacement differs: %v", err)
	}
	store.projectionStep = nil
	if err := store.ReconcileAttemptProofsContext(context.Background(), ledger); !errors.Is(err, errAttemptProofProjectionFaulted) {
		t.Fatalf("faulted retry: %v", err)
	}
	reopened, err := NewProofStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.ReconcileAttemptProofsContext(context.Background(), ledger); err != nil {
		t.Fatalf("verified reopen: %v", err)
	}
}

// Cancellation after a spool record leaves the old destination untouched and
// can retry the disposable bounded scratch file without losing signed proofs.
func TestDiskAttemptProofProjectionCancellationPreservesOriginal(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	store, err := NewProofStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	expected := attemptProofStreamTestExpected(t, fixture.recordTs)
	prior := expected[:100]
	if err := os.WriteFile(store.path, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store.projectionStep = func(operation, _ string) error {
		if operation == "proof-spool-record" {
			cancel()
		}
		return nil
	}
	if err := store.ReconcileAttemptProofsContext(ctx, ledger); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled spool: %v", err)
	}
	actual, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(actual, prior) || store.projectionFault != nil {
		t.Fatalf("cancelled spool changed authority: %v/%v", err, store.projectionFault)
	}
	store.projectionStep = nil
	if err := store.ReconcileAttemptProofsContext(context.Background(), ledger); err != nil {
		t.Fatal(err)
	}
	actual, err = os.ReadFile(store.path)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("spool resume differs: %v", err)
	}
}

// An observed decode counter proves normal projection reads only the new
// range, without rebuilding earlier proof bytes or materializing ledger history.
func TestDiskAttemptProofProjectionIncrementalWorkUsesOnlyNewRecords(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs[:8]), 0o600); err != nil {
		t.Fatal(err)
	}
	var decodes atomic.Int64
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{Store: attemptRecordStoreHooks{Step: func(operation, _ string) error {
		if operation == "decode-record" {
			decodes.Add(1)
		}
		return nil
	}}})
	store, err := NewProofStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAttemptProofsContext(context.Background(), ledger); err != nil {
		t.Fatal(err)
	}
	for _, record := range fixture.recordTs[8:] {
		if _, err := ledger.Append(record); err != nil {
			t.Fatal(err)
		}
	}
	decodes.Store(0)
	if err := store.projectAttemptProof(ledger, fixture.recordTs[15].Proof); err != nil {
		t.Fatal(err)
	}
	if count := decodes.Load(); count != 8 {
		t.Fatalf("incremental projection decoded %d records, want exact new range 8", count)
	}
	actual, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(actual, attemptProofStreamTestExpected(t, fixture.recordTs)) {
		t.Fatalf("complete projection differs: %v", err)
	}
	decodes.Store(0)
	if err := store.projectAttemptProof(ledger, fixture.recordTs[15].Proof); err != nil {
		t.Fatal(err)
	}
	if count := decodes.Load(); count != 0 {
		t.Fatalf("same acknowledged head decoded %d records", count)
	}
}

// Storage exhaustion rejects the complete projection; it cannot drop the last
// proof or count a shortened census as successful recovery.
func TestDiskAttemptProofProjectionEnforcesWholeStreamByteBound(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	expected := attemptProofStreamTestExpected(t, fixture.recordTs)
	for _, exact := range []bool{false, true} {
		dir := newAttemptLedgerDiskTestStateDir(t)
		if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
			t.Fatal(err)
		}
		limits := attemptLedgerDiskTestLimits()
		limits.MaxProofBytes = uint64(len(expected))
		if !exact {
			limits.MaxProofBytes--
		}
		ledger, err := NewDiskAttemptLedger(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, limits)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ledger.Close() })
		store, err := NewProofStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		err = store.ReconcileAttemptProofsContext(context.Background(), ledger)
		if exact && err != nil || !exact && err == nil {
			t.Fatalf("exact=%t projection: %v", exact, err)
		}
		if !exact {
			if _, err := os.Stat(store.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("over-budget projection was published: %v", err)
			}
		}
		if err := ledger.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A scratch alias is never truncated. A renamed state directory cannot divert
// atomic replacement into a new directory installed at the old path.
func TestDiskAttemptProofProjectionRejectsScratchAliasAndDirectorySwap(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, variation := range []string{"hardlink", "symlink", "directory"} {
		dir := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
			t.Fatal(err)
		}
		ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
		store, err := NewProofStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside")
		original := []byte("preserved outside data")
		if err := os.WriteFile(outside, original, 0o600); err != nil {
			t.Fatal(err)
		}
		switch variation {
		case "hardlink":
			if err := os.Link(outside, store.path+".attempt-spool"); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Symlink(outside, store.path+".attempt-spool"); err != nil {
				t.Fatal(err)
			}
		case "directory":
			store.projectionStep = func(operation, _ string) error {
				if operation != "proof-rename" {
					return nil
				}
				if err := os.Rename(dir, dir+"-held"); err != nil {
					return err
				}
				if err := os.Mkdir(dir, 0o700); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(dir, "proofs.jsonl"), original, 0o600)
			}
		}
		if err := store.ReconcileAttemptProofsContext(context.Background(), ledger); err == nil {
			t.Fatalf("%s projection admitted", variation)
		}
		actual, err := os.ReadFile(outside)
		if err != nil || !bytes.Equal(actual, original) {
			t.Fatalf("%s alias changed: %v", variation, err)
		}
		if variation == "directory" {
			actual, err := os.ReadFile(filepath.Join(dir, "proofs.jsonl"))
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatalf("replacement path was redirected: %v", err)
			}
		}
	}
}

// Restoring both length and mtime does not restore the native change-time
// witness. Neither an unchanged head nor an incremental append may bless it.
func TestDiskAttemptProofProjectionWarmCursorRejectsSameLengthRewrite(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	for _, extend := range []bool{false, true} {
		dir := newAttemptLedgerDiskTestStateDir(t)
		if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs[:8]), 0o600); err != nil {
			t.Fatal(err)
		}
		ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
		store, err := NewProofStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.ReconcileAttemptProofsContext(context.Background(), ledger); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(store.path)
		if err != nil {
			t.Fatal(err)
		}
		corrupt := attemptProofStreamTestExpected(t, fixture.recordTs[:8])
		corrupt[len(corrupt)/2] ^= 1
		if err := os.WriteFile(store.path, corrupt, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(store.path, before.ModTime(), before.ModTime()); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(store.path)
		if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("rewrite did not preserve inode/size/mtime: %v", err)
		}
		if attemptLedgerSameFileState(before, after) {
			t.Fatal("filesystem did not establish changed native write-state")
		}
		if extend {
			for _, record := range fixture.recordTs[8:] {
				if _, err := ledger.Append(record); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := store.projectDiskAttemptProofs(context.Background(), ledger); err == nil {
			t.Fatalf("extend=%t accepted warm-cursor corruption", extend)
		}
		actual, err := os.ReadFile(store.path)
		if err != nil || !bytes.Equal(actual, corrupt) {
			t.Fatalf("extend=%t rewrote conflicting authority: %v", extend, err)
		}
	}
}

// A second caller cancels only after it observes real gate contention. The
// first projection stays blocked until the canceled caller has returned.
func TestDiskAttemptProofProjectionQueuedCallerCancelsWithoutJoiningOwner(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	store, err := NewProofStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	entered, release, contended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var first, waiter sync.Once
	store.projectionStep = func(operation, _ string) error {
		if operation == "proof-spool-record" {
			first.Do(func() { close(entered); <-release })
		}
		if operation == "proof-gate-contended" {
			waiter.Do(func() { close(contended) })
		}
		return nil
	}
	completed := make(chan error, 1)
	go func() { completed <- store.ReconcileAttemptProofsContext(context.Background(), ledger) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queued := make(chan error, 1)
	go func() { queued <- store.ReconcileAttemptProofsContext(ctx, ledger) }()
	<-contended
	cancel()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Errorf("queued cancellation: %v", err)
	}
	close(release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
}
