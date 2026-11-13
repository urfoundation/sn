//go:build linux || darwin

// Recovery tests lose every public object, retain the real signed disk ledger,
// then require the original graph and full replay without source mutation.
package validator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb/opt"
)

// Adapts the real sealer's bounded object transport to the unsigned recovery.
func attemptCutV2RecoveryTestOptions(fixture *attemptCutV2SealTestFixture, options AttemptCutV2SealOptions) AttemptCutV2RecoveryOptions {
	return AttemptCutV2RecoveryOptions{LedgerLimits: attemptLedgerDiskTestLimits(), ReplayBounds: options.ReplayBounds, ScratchDirectory: options.ScratchDirectory,
		ServerKeys: options.ServerKeys, WriteRecords: options.WriteRecords, WriteProofs: options.WriteProofs, WriteMetadata: options.WriteMetadata, ReadMetadata: options.ReadMetadata, OpenData: options.OpenData}
}

// A later real signed append cannot extend an already signed historical cut.
func TestAttemptCutV2RecoveryRestoresLostGraphFromOriginalPrefix(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 1)
	originalOptions, originalObjects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, originalReplay, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, originalOptions)
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.ledger.disk.(*attemptRecordStore).disk.path
	stateDir := filepath.Dir(path)
	if _, err := fixture.engine.RunTrail(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, _, err := stoppedAttemptFiles(root, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	recoveryTransport, recoveredObjects := newAttemptCutV2SealTestOptions(t, fixture)
	missingReplay := AttemptCutV2ReplayOptions{Bounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "missing-replay"), ServerKeys: recoveryTransport.ServerKeys, ReadMetadata: recoveryTransport.ReadMetadata, OpenData: recoveryTransport.OpenData}
	if _, err := ReplayAttemptCutV2WithPolicy(t.Context(), *cut, fixture.expected, fixture.policy, fixture.bounds, missingReplay); err == nil {
		t.Fatal("the original cut replayed after its complete public graph was lost")
	}
	result, err := RecoverStoppedAttemptCutV2Objects(t.Context(), stateDir, *cut, fixture.expected, fixture.policy, fixture.bounds, attemptCutV2RecoveryTestOptions(fixture, recoveryTransport))
	if err != nil {
		t.Fatal(err)
	}
	if result != originalReplay || !reflect.DeepEqual(originalObjects.metadataKVs, recoveredObjects.metadataKVs) || !reflect.DeepEqual(originalObjects.dataKVs, recoveredObjects.dataKVs) {
		t.Fatal("recovery changed original object bytes, references or replay census")
	}
	after, _, err := stoppedAttemptFiles(root, attemptLedgerDiskTestLimits())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("recovery changed the original source", err)
	}
}

// Even a valid new signature cannot replace the original stream identity.
func TestAttemptCutV2RecoveryRejectsDifferentSignedReference(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Dir(fixture.ledger.disk.(*attemptRecordStore).disk.path)
	if err := fixture.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	cut.Records.ManifestHash = attemptHex32([32]byte{99})
	cut.Signature, err = cut.Sign(fixture.key, fixture.bounds)
	if err != nil {
		t.Fatal(err)
	}
	recoveryTransport, _ := newAttemptCutV2SealTestOptions(t, fixture)
	result, err := RecoverStoppedAttemptCutV2Objects(t.Context(), stateDir, *cut, fixture.expected, fixture.policy, fixture.bounds, attemptCutV2RecoveryTestOptions(fixture, recoveryTransport))
	if err == nil || !strings.Contains(err.Error(), "original signed reference") || result != (AttemptCutV2ReplayResult{}) {
		t.Fatal("a replacement reference escaped original-byte recovery", err)
	}
}

// Full source authentication precedes the first public staging callback.
func TestAttemptCutV2RecoveryRejectsCorruptLifetimeBeforeStaging(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	store := fixture.ledger.disk.(*attemptRecordStore)
	stateDir := filepath.Dir(store.disk.path)
	if err := store.db.Delete(attemptStoreTrailRecordKey(fixture.recordTs[0].TrailID, 1), &opt.WriteOptions{Sync: true}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	recoveryTransport, recoveredObjects := newAttemptCutV2SealTestOptions(t, fixture)
	result, err := RecoverStoppedAttemptCutV2Objects(t.Context(), stateDir, *cut, fixture.expected, fixture.policy, fixture.bounds, attemptCutV2RecoveryTestOptions(fixture, recoveryTransport))
	if err == nil || recoveredObjects.writes != 0 || result != (AttemptCutV2ReplayResult{}) {
		t.Fatal("corrupt source emitted public objects", err)
	}
}

// A live owner retains its actual directory lock, even when read-only leveldb opens.
func TestAttemptCutV2RecoveryRefusesLiveWriter(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Dir(fixture.ledger.disk.(*attemptRecordStore).disk.path)
	recoveryTransport, recoveredObjects := newAttemptCutV2SealTestOptions(t, fixture)
	if _, err := RecoverStoppedAttemptCutV2Objects(t.Context(), stateDir, *cut, fixture.expected, fixture.policy, fixture.bounds, attemptCutV2RecoveryTestOptions(fixture, recoveryTransport)); err == nil || recoveredObjects.writes != 0 {
		t.Fatal("recovery acquired a second owner beside the live writer", err)
	}
}

// Already canceled work cannot acquire or stage any original source bytes.
func TestAttemptCutV2RecoveryRefusesCancellation(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Dir(fixture.ledger.disk.(*attemptRecordStore).disk.path)
	recoveryTransport, recoveredObjects := newAttemptCutV2SealTestOptions(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RecoverStoppedAttemptCutV2Objects(ctx, stateDir, *cut, fixture.expected, fixture.policy, fixture.bounds, attemptCutV2RecoveryTestOptions(fixture, recoveryTransport)); !errors.Is(err, context.Canceled) || recoveredObjects.writes != 0 {
		t.Fatal("canceled recovery emitted public objects", err)
	}
}

// A staging callback cancels at an explicit boundary after the first object.
func TestAttemptCutV2RecoveryJoinsLateCancellationWithoutSourceMutation(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.ledger.disk.(*attemptRecordStore).disk.path
	if err := fixture.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, _, err := stoppedAttemptFiles(root, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	recoveryTransport, recoveredObjects := newAttemptCutV2SealTestOptions(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	recoveryOptions := attemptCutV2RecoveryTestOptions(fixture, recoveryTransport)
	recoveryOptions.WriteRecords = func(ctx context.Context, hash string, raw []byte) error {
		if err := recoveryTransport.WriteRecords(ctx, hash, raw); err != nil {
			return err
		}
		cancel()
		return nil
	}
	result, err := RecoverStoppedAttemptCutV2Objects(ctx, filepath.Dir(path), *cut, fixture.expected, fixture.policy, fixture.bounds, recoveryOptions)
	if !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) || recoveredObjects.writes != 1 {
		t.Fatal("late cancellation published a recovery result", err)
	}
	after, _, err := stoppedAttemptFiles(root, attemptLedgerDiskTestLimits())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("canceled recovery changed the source", err)
	}
}
