//go:build linux || darwin

package validator

// Real signed M8 fixtures exercise import, restart, exact projection and owner
// exclusion. Barriers observe the relevant lock/persistence boundary directly;
// no scheduler-negative timing is used as the correctness witness.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const attemptLedgerDiskTestCoordinator = "0x1111111111111111111111111111111111111111"

// TempDir's numbered directory uses the process umask, not a private mode.
// Create a dedicated owned state child without changing its existing parent.
func newAttemptLedgerDiskTestStateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !attemptLedgerPrivateDirectory(info) {
		t.Fatalf("private state fixture was not provisioned: %v", err)
	}
	return dir
}

// Small explicit qualification bounds do not activate production policy.
func attemptLedgerDiskTestLimits() AttemptLedgerDiskLimits {
	bounds := attemptRecordStoreTestBounds()
	return AttemptLedgerDiskLimits{MaxRecordBytes: bounds.MaxRecordBytes, MaxRecordCount: bounds.MaxRecordCount,
		MaxTrailCount: bounds.MaxTrailCount, MaxRawRecordBytes: bounds.MaxRawRecordBytes,
		MaxStorageBytes: bounds.MaxStorageBytes, MaxStorageFiles: bounds.MaxStorageFiles,
		MaxLegacyBytes: 8 * 1024 * 1024, MaxProofBytes: 8 * 1024 * 1024}
}

// Test JSONL bytes are derived directly from the real signed v1 fixture.
func attemptLedgerDiskTestJSONL(t *testing.T, records []AttemptRecord) []byte {
	t.Helper()
	var raw []byte
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, line...)
		raw = append(raw, '\n')
	}
	return raw
}

// Every constructor-owned backend is joined even when a later assertion fails.
func openAttemptLedgerDiskTest(t *testing.T, dir string, fixture attemptRecordStoreTestFixture, hooks attemptLedgerDiskHooks) *AttemptLedger {
	t.Helper()
	ledger, err := newDiskAttemptLedgerWithHooks(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), hooks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	return ledger
}

// Existing shared state is rejected without repair; an explicitly private
// child is admitted without chmodding its existing public parent or alias.
func TestDiskAttemptLedgerDirectoryRequiresExplicitPrivateState(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, disk := range []bool{false, true} {
		for _, variation := range []string{"private", "public", "symlink"} {
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			parentInfo, err := os.Lstat(parent)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(parent, "state")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			original := []byte("preserved state fixture")
			path := filepath.Join(dir, "sentinel")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			switch variation {
			case "public":
				if err := os.Chmod(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(dir, dir+"-target"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(dir+"-target", dir); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(dir)
			if err != nil {
				t.Fatal(err)
			}
			var ledger *AttemptLedger
			if disk {
				ledger, err = NewDiskAttemptLedger(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
			} else {
				ledger, err = NewAttemptLedger(dir, fixture.identity, fixture.validatorKey)
			}
			if ledger != nil {
				t.Cleanup(func() { _ = ledger.Close() })
			}
			if variation == "private" {
				if err != nil || ledger == nil {
					t.Fatalf("disk=%t private child refused: %v", disk, err)
				}
				if head, err := ledger.Head(); err != nil || head.LastSequence != 0 {
					t.Fatalf("disk=%t private head %+v/%v", disk, head, err)
				}
				if err := ledger.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if ledger != nil || err == nil || !strings.Contains(err.Error(), "state directory is not private and owned") {
					t.Fatalf("disk=%t %s state did not reach privacy refusal: %v/%v", disk, variation, ledger, err)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" {
					t.Fatalf("disk=%t rejected %s namespace was mutated: %v", disk, variation, err)
				}
			}
			after, err := os.Lstat(dir)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("disk=%t %s final component was changed: %v", disk, variation, err)
			}
			parentAfter, err := os.Lstat(parent)
			if err != nil || !os.SameFile(parentInfo, parentAfter) || parentInfo.Mode() != parentAfter.Mode() {
				t.Fatalf("disk=%t %s parent was changed: %v", disk, variation, err)
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatalf("disk=%t %s source was changed: %v", disk, variation, err)
			}
		}
	}
}

// The preserved source can include old empty lines without re-encoding them.
func TestDiskAttemptLedgerImportsExactV1BytesWithoutHistoryMaps(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	dir := newAttemptLedgerDiskTestStateDir(t)
	raw := append([]byte{'\n'}, attemptLedgerDiskTestJSONL(t, fixture.recordTs[:8])...)
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	if ledger.records != nil || ledger.pending != nil || ledger.terminal != nil {
		t.Fatal("disk ledger retained history or lifecycle maps")
	}
	visited := 0
	if err := ledger.Walk(context.Background(), 1, 8, func(record AttemptRecord) error {
		if !reflect.DeepEqual(record, fixture.recordTs[visited]) {
			return errors.New("import changed signed bytes")
		}
		visited++
		return nil
	}); err != nil || visited != 8 {
		t.Fatalf("walk %d: %v", visited, err)
	}
	for _, record := range fixture.recordTs[8:] {
		committed, err := ledger.AppendContext(context.Background(), record)
		if err != nil || !reflect.DeepEqual(*committed, record) {
			t.Fatalf("append %d differs: %v", record.Sequence, err)
		}
	}
	head, err := ledger.Head()
	if err != nil || head.LastSequence != 16 || head.Root != fixture.recordTs[15].RecordHash {
		t.Fatalf("head %+v: %v", head, err)
	}
	actual, err := os.ReadFile(filepath.Join(dir, attemptLedgerLegacyName))
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("original JSONL changed: %v", err)
	}
	if _, err := ledger.RecordsAfter(0); !errors.Is(err, ErrAttemptLedgerStreamingRequired) {
		t.Fatalf("slice API: %v", err)
	}
	if _, err := ledger.RecoverPending(); !errors.Is(err, ErrAttemptLedgerStreamingRequired) {
		t.Fatalf("slice recovery: %v", err)
	}
	if _, err := ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1); !errors.Is(err, ErrAttemptLedgerStreamingRequired) {
		t.Fatalf("v1 cut API: %v", err)
	}
}

// Cancellation follows an observed committed checkpoint, leaving resumable
// provenance and no falsely live constructor result.
func TestDiskAttemptLedgerCancelledImportResumesExactPrefix(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	raw := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ledger, err := newDiskAttemptLedgerWithHooks(ctx, dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: func(operation, name string) error {
		if operation == "import-record" && name == "3" {
			cancel()
		}
		return nil
	}})
	if ledger != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled constructor = %v/%v", ledger, err)
	}
	if _, err := os.Stat(filepath.Join(dir, attemptLedgerReadyName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial import has complete receipt: %v", err)
	}
	store := openAttemptRecordStoreTest(t, filepath.Join(dir, attemptLedgerStoreName), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	head, err := store.Head()
	if err != nil || head.LastSequence != 3 || head.Root != fixture.recordTs[2].RecordHash {
		t.Fatalf("cancelled prefix %+v/%v", head, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	ledger = openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	head, err = ledger.Head()
	if err != nil || head.LastSequence != 8 || head.Root != fixture.recordTs[7].RecordHash {
		t.Fatalf("resumed prefix %+v/%v", head, err)
	}
	actual, err := os.ReadFile(filepath.Join(dir, attemptLedgerLegacyName))
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("resume changed original: %v", err)
	}
}

// A matching donor byte stream is not the same local interrupted source inode.
func TestDiskAttemptLedgerInterruptedImportRejectsReplacedSourceInode(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	raw := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
	path := filepath.Join(dir, attemptLedgerLegacyName)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop after exact prefix")
	ledger, err := newDiskAttemptLedgerWithHooks(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: func(operation, name string) error {
		if operation == "import-record" && name == "2" {
			return stop
		}
		return nil
	}})
	if ledger != nil || !errors.Is(err, stop) {
		t.Fatalf("partial import %v/%v", ledger, err)
	}
	temporary := filepath.Join(dir, "replacement.jsonl")
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
	ledger, err = NewDiskAttemptLedger(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if ledger != nil || err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("substituted resume %v/%v", ledger, err)
	}
}

// Once complete, portability depends on exact bytes and signed prefix, not
// on the machine-local inode that happened to perform the original import.
func TestDiskAttemptLedgerCompletedImportAllowsPortableSourceIdentity(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	raw := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
	path := filepath.Join(dir, attemptLedgerLegacyName)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(dir, "portable-original.jsonl")
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
	ledger = openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	head, err := ledger.Head()
	if err != nil || head.LastSequence != 8 {
		t.Fatalf("portable completed prefix %+v/%v", head, err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger, err = NewDiskAttemptLedger(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if ledger != nil || err == nil {
		t.Fatalf("changed raw census admitted %v/%v", ledger, err)
	}
}

// Finite raw/line budgets and canonical framing reject without rewriting input.
func TestDiskAttemptLedgerImportRejectsOversizeTornAndNoncanonicalSource(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	raw := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
	for _, variation := range []string{"raw", "line", "torn", "noncanonical", "signature"} {
		dir := newAttemptLedgerDiskTestStateDir(t)
		input := append([]byte(nil), raw...)
		limits := attemptLedgerDiskTestLimits()
		var wantError string
		switch variation {
		case "raw":
			limits.MaxLegacyBytes = uint64(len(raw) - 1)
			wantError = "legacy file exceeds its explicit byte bound"
		case "line":
			limits.MaxRecordBytes = 128
			wantError = "legacy line exceeds its record bound"
		case "torn":
			input = input[:len(input)-1]
			wantError = "legacy source has a torn final line"
		case "noncanonical":
			input = append([]byte{' '}, input...)
			wantError = "not canonical"
		case "signature":
			records := append([]AttemptRecord(nil), fixture.recordTs...)
			records[0].Signature = append([]byte(nil), records[0].Signature...)
			records[0].Signature[0] ^= 1
			input = attemptLedgerDiskTestJSONL(t, records)
			wantError = "validator signature is invalid"
		}
		path := filepath.Join(dir, attemptLedgerLegacyName)
		if err := os.WriteFile(path, input, 0o600); err != nil {
			t.Fatal(err)
		}
		ledger, err := NewDiskAttemptLedger(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, limits)
		if ledger != nil || err == nil || !strings.Contains(err.Error(), wantError) {
			t.Fatalf("%s source did not reach %q refusal: %v/%v", variation, wantError, ledger, err)
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, input) {
			t.Fatalf("%s source was rewritten: %v", variation, err)
		}
	}
}

// Namespace binding survives both incomplete and complete importer restarts.
func TestDiskAttemptLedgerRejectsChangedNamespace(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	for _, variation := range []string{"coordinator", "identity", "validator"} {
		identity, coordinator, key := fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey
		switch variation {
		case "coordinator":
			coordinator = "0x2222222222222222222222222222222222222222"
		case "identity":
			identity.NoID++
		case "validator":
			key = newAttemptRecordStoreTestFixture(t, 1).validatorKey
		}
		other, err := NewDiskAttemptLedger(context.Background(), dir, identity, coordinator, key, attemptLedgerDiskTestLimits())
		if other != nil || err == nil {
			t.Fatalf("changed %s admitted: %v/%v", variation, other, err)
		}
	}
}

// A v1 object opened before migration cannot bypass the marker check, and a
// queued append cancels on an observed held directory lock.
func TestDiskAttemptLedgerMigrationGatesAlreadyOpenLegacyWriter(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	legacy, err := NewAttemptLedger(dir, fixture.identity, fixture.validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	type opening struct {
		ledger *AttemptLedger
		err    error
	}
	opened := make(chan opening, 1)
	go func() {
		ledger, err := newDiskAttemptLedgerWithHooks(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: func(operation, name string) error {
			if operation == "marker-sync" && name == attemptLedgerImportName {
				once.Do(func() { close(entered); <-release })
			}
			return nil
		}})
		opened <- opening{ledger: ledger, err: err}
	}()
	<-entered
	contended := make(chan struct{})
	var contention sync.Once
	legacy.legacyDirectoryStep = func(operation, _ string) error {
		if operation == "gate-contended" {
			contention.Do(func() { close(contended) })
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	appended := make(chan error, 1)
	go func() { _, err := legacy.AppendContext(ctx, fixture.recordTs[0]); appended <- err }()
	<-contended
	cancel()
	if err := <-appended; !errors.Is(err, context.Canceled) {
		t.Errorf("queued v1 append cancellation: %v", err)
	}
	close(release)
	result := <-opened
	if result.err != nil || result.ledger == nil {
		t.Fatalf("migration %v/%v", result.ledger, result.err)
	}
	defer result.ledger.Close()
	if _, err := legacy.Append(fixture.recordTs[0]); !errors.Is(err, ErrAttemptLedgerDiskMigration) {
		t.Fatalf("already-open v1 append = %v", err)
	}
	if _, err := NewAttemptLedger(dir, fixture.identity, fixture.validatorKey); !errors.Is(err, ErrAttemptLedgerDiskMigration) {
		t.Fatalf("v1 reopen = %v", err)
	}
	head, err := result.ledger.Head()
	if err != nil || head.LastSequence != 0 {
		t.Fatalf("migration was bypassed %+v/%v", head, err)
	}
}

// Two old cached heads are ordered by the same gate; the stale one refuses
// before append instead of producing a second sequence 1 in the JSONL file.
func TestDiskAttemptLedgerLegacyGateRejectsStaleSecondOwner(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := filepath.Join(t.TempDir(), "nested", "operator", "state")
	first, err := NewAttemptLedger(dir, fixture.identity, fixture.validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewAttemptLedger(dir, fixture.identity, fixture.validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.directory != nil || second.directory != nil {
		t.Fatal("legacy construction retained directory descriptors")
	}
	if _, err := first.Append(fixture.recordTs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Append(fixture.recordTs[0]); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale owner append: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, attemptLedgerLegacyName))
	if err != nil || !bytes.Equal(raw, attemptLedgerDiskTestJSONL(t, fixture.recordTs[:1])) {
		t.Fatalf("stale append changed bytes: %v", err)
	}
	if _, err := first.Append(fixture.recordTs[1]); err != nil {
		t.Fatalf("current owner failed to advance: %v", err)
	}
}

// Terminal recovery is streamed from exact pending state; a visitor failure
// preserves committed outcomes and a reopen resumes only the unfinished trail.
func TestDiskAttemptLedgerRecoveryStreamsAndResumesAfterVisitorFailure(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	second := fixture.recordTs[8]
	second.Sequence, second.PreviousHash = 2, fixture.recordTs[0].RecordHash
	second = resignAttemptRecordStoreTest(t, second, fixture.validatorKey)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, []AttemptRecord{fixture.recordTs[0], second}), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	stop := errors.New("visitor stops after one durable outcome")
	visited := 0
	if err := ledger.RecoverPendingContext(context.Background(), func(record AttemptRecord) error {
		visited++
		if record.Disposition != AttemptDispositionValidatorError || record.Sequence != 3 {
			return errors.New("unexpected recovery")
		}
		if _, err := ledger.Head(); err != nil {
			return err
		}
		return stop
	}); !errors.Is(err, stop) || visited != 1 {
		t.Fatalf("recovery %d/%v", visited, err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	ledger = openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	visited = 0
	if err := ledger.RecoverPendingContext(context.Background(), func(record AttemptRecord) error {
		visited++
		if record.Sequence != 4 {
			return fmt.Errorf("recovered sequence %d", record.Sequence)
		}
		return nil
	}); err != nil || visited != 1 {
		t.Fatalf("resumed recovery %d/%v", visited, err)
	}
	if err := ledger.RecoverPendingContext(context.Background(), func(AttemptRecord) error { return errors.New("terminal ID became pending again") }); err != nil {
		t.Fatal(err)
	}
}

// Close's cancellation is observed before the visitor is released. Returning
// from Close therefore proves both iterator cancellation and visitor joining.
func TestDiskAttemptLedgerCloseCancelsAndJoinsStreamingVisitor(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	entered, release := make(chan struct{}), make(chan struct{})
	walked := make(chan error, 1)
	go func() {
		walked <- ledger.Walk(context.Background(), 1, 8, func(record AttemptRecord) error {
			if record.Sequence != 1 {
				return errors.New("walk continued after close")
			}
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- ledger.Close() }()
	<-ledger.ctx.Done()
	close(release)
	if err := <-walked; !errors.Is(err, errAttemptRecordStoreClosed) && !errors.Is(err, errAttemptLedgerClosed) {
		t.Fatalf("closed walk: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Head(); !errors.Is(err, errAttemptLedgerClosed) {
		t.Fatalf("closed head: %v", err)
	}
}

// The checked head cannot hide an ambiguously committed signed batch.
func TestDiskAttemptLedgerAppendFaultRequiresVerifiedReopen(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	var fail atomic.Bool
	storeError := errors.New("batch persisted before synthetic failure")
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{Store: attemptRecordStoreHooks{Step: func(operation, _ string) error {
		if operation == "after-batch" && fail.Load() {
			return storeError
		}
		return nil
	}}})
	fail.Store(true)
	if _, err := ledger.AppendContext(context.Background(), fixture.recordTs[0]); !errors.Is(err, storeError) {
		t.Fatalf("ambiguous append: %v", err)
	}
	if _, err := ledger.Head(); !errors.Is(err, errAttemptRecordStoreFaulted) {
		t.Fatalf("faulted head: %v", err)
	}
	if _, err := ledger.Append(fixture.recordTs[0]); !errors.Is(err, errAttemptRecordStoreFaulted) {
		t.Fatalf("faulted retry: %v", err)
	}
	if err := ledger.Close(); !errors.Is(err, storeError) {
		t.Fatalf("faulted close: %v", err)
	}
	ledger = openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	head, err := ledger.Head()
	if err != nil || head.LastSequence != 1 || head.Root != fixture.recordTs[0].RecordHash {
		t.Fatalf("reconciled prefix %+v/%v", head, err)
	}
}

// A late replay error must leave the detached candidate, binding and saved
// snapshot unpublished, even after earlier records were valid and complete.
func TestDiskAttemptLedgerStatsLateReplayFailureLeavesOriginalState(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	records := append([]AttemptRecord(nil), fixture.recordTs[:9]...)
	records[8].Boundary.SettlementEpoch++
	records[8] = resignAttemptRecordStoreTest(t, records[8], fixture.validatorKey)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, records), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	stats := NewStatsEngine(StatsConfig{})
	stats.settlementEpoch, stats.settlementEpochKnown = 42, true
	stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = 1, 1
	if err := stats.Save(dir); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedgerContext(context.Background(), ledger, dir); err == nil {
		t.Fatal("late wrong-epoch replay succeeded")
	}
	if stats.attemptLedger != nil || stats.attemptLastAppliedSequence != 0 || len(stats.window) != 0 || len(stats.egress) != 0 {
		t.Fatal("failed replay published partial statistics")
	}
	after, err := os.ReadFile(filepath.Join(dir, "stats.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed replay changed snapshot: %v", err)
	}
}

// Even a final persistence error must not attach or advance the public state.
func TestDiskAttemptLedgerStatsSaveFailureCanRetryWithoutDoubleApplying(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	stats := NewStatsEngine(StatsConfig{})
	stats.settlementEpoch, stats.settlementEpochKnown = 42, true
	stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = 1, 1
	path := filepath.Join(dir, "stats.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedgerContext(context.Background(), ledger, dir); err == nil {
		t.Fatal("snapshot failure was hidden")
	}
	if stats.attemptLedger != nil || stats.attemptLastAppliedSequence != 0 || len(stats.window) != 0 {
		t.Fatal("failed save exposed replay state")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedgerContext(context.Background(), ledger, dir); err != nil {
		t.Fatal(err)
	}
	var assignments uint64
	for _, window := range stats.window {
		assignments += window.Assignments
	}
	if assignments != 7 || stats.attemptLastAppliedSequence != 8 {
		t.Fatalf("replayed assignment/sequence %d/%d", assignments, stats.attemptLastAppliedSequence)
	}
	if err := stats.AttachAttemptLedgerContext(context.Background(), ledger, dir); err != nil {
		t.Fatal(err)
	}
	var again uint64
	for _, window := range stats.window {
		again += window.Assignments
	}
	if again != assignments {
		t.Fatalf("replay applied twice %d/%d", assignments, again)
	}
}

// A failed first marker sync retains a bounded exact temporary prefix. The
// legacy writer refuses it, and a new importer must make it durable before use.
func TestDiskAttemptLedgerMarkerDurabilityFailureResumesWithoutLegacyDowngrade(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	raw := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	markerError := errors.New("marker sync failed")
	ledger, err := newDiskAttemptLedgerWithHooks(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: func(operation, name string) error {
		if operation == "marker-sync" && name == attemptLedgerImportName {
			return markerError
		}
		return nil
	}})
	if ledger != nil || !errors.Is(err, markerError) {
		t.Fatalf("marker failure: %v/%v", ledger, err)
	}
	if _, err := NewAttemptLedger(dir, fixture.identity, fixture.validatorKey); !errors.Is(err, ErrAttemptLedgerDiskMigration) {
		t.Fatalf("partial marker allowed downgrade: %v", err)
	}
	ledger = openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	head, err := ledger.Head()
	if err != nil || head.LastSequence != 8 {
		t.Fatalf("resumed marker prefix %+v/%v", head, err)
	}
	actual, err := os.ReadFile(filepath.Join(dir, attemptLedgerLegacyName))
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("marker retry changed source: %v", err)
	}
}

// A source swapped between the name check and no-follow open cannot grant the
// importer a different inode, even if its record bytes are otherwise identical.
func TestDiskAttemptLedgerSourceSwapBetweenCheckAndOpenFailsClosed(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	raw := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
	path, replacement := filepath.Join(dir, attemptLedgerLegacyName), filepath.Join(dir, "replacement.jsonl")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var swapped bool
	ledger, err := newDiskAttemptLedgerWithHooks(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: func(operation, name string) error {
		if operation == "before-open" && name == attemptLedgerLegacyName && !swapped {
			swapped = true
			if err := os.Rename(path, path+".held"); err != nil {
				return err
			}
			return os.Rename(replacement, path)
		}
		return nil
	}})
	if ledger != nil || err == nil || !swapped {
		t.Fatalf("swapped source admitted %v/%v/%t", ledger, err, swapped)
	}
	actual, err := os.ReadFile(path + ".held")
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("source inode was overwritten: %v", err)
	}
}

// Key consistency is established before creating an import marker or database.
func TestDiskAttemptLedgerRejectsInconsistentPrivateKeyBeforeMutation(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	key := append([]byte(nil), fixture.validatorKey...)
	key[len(key)-1] ^= 1
	dir := filepath.Join(t.TempDir(), "not-created")
	ledger, err := NewDiskAttemptLedger(context.Background(), dir, fixture.identity, attemptLedgerDiskTestCoordinator, key, attemptLedgerDiskTestLimits())
	if ledger != nil || err == nil || !strings.Contains(err.Error(), "validator private key is invalid") {
		t.Fatalf("inconsistent key admitted %v/%v", ledger, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid key mutated state: %v", err)
	}
	if _, err := NewAttemptLedger(dir, fixture.identity, key); err == nil || !strings.Contains(err.Error(), "validator private key is invalid") {
		t.Fatalf("legacy constructor did not reach inconsistent-key refusal: %v", err)
	}
}
