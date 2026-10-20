//go:build linux || darwin

// Exact-image controls exercise genuine disk ledgers, V2 activation/recovery
// and bounded physical reads. Spies observe real writes without replacing any
// signature, statistics replay or recovery-acceptance decision.
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

// Captures actual durable images, not live-engine snapshots manufactured after
// a failure. The returned fence owns its own immutable byte copies.
func newAttemptSettlementV2StartupImagesTestFixture(t *testing.T) (*attemptSettlementRuntimeV2TestFixture, *attemptSettlementV2StartupImages, [][]byte) {
	t.Helper()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	images := make([][]byte, len(fixture.participants))
	present := make([]bool, len(fixture.participants))
	for index, participant := range fixture.participants {
		var err error
		images[index], err = os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
		present[index] = true
	}
	pins, err := newAttemptSettlementV2StartupImages(t.Context(), fixture.coordinator, fixture.participants, images, present, nil, false, runtimeAttemptSettlementV2TestPersistence())
	if err != nil {
		t.Fatal(err)
	}
	return fixture, pins, images
}

// Genuine fresh-engine recovery still succeeds for exactly the authenticated
// input bytes; neither the fence nor its constructor attaches public Stats.
func TestAttemptSettlementV2StartupImagesRecoverExactRealDiskBatch(t *testing.T) {
	t.Parallel()
	fixture, pins, images := newAttemptSettlementV2StartupImagesTestFixture(t)
	participants := fixture.reopen(t)
	physical := attemptSettlementV2PhysicalIO()
	physical.startupImages = pins
	if err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, participants, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err != nil {
		t.Fatal(err)
	}
	for index, participant := range participants {
		actual, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil || !bytes.Equal(actual, images[index]) || participant.Stats.attemptLedger != participant.Ledger || participant.Stats.egressGeneration != 1 {
			t.Fatalf("exact-image recovery changed genuine initial state: %v", err)
		}
	}
}

// The generic carried-state verifier does not know a later ordinary generation.
// The startup consumer must refuse bytes changed after independent replay,
// before even the first real snapshot writer or public Stats attachment.
func TestAttemptSettlementV2StartupImagesRejectChangedSnapshotBeforeRecoveryWrite(t *testing.T) {
	t.Parallel()
	fixture, pins, images := newAttemptSettlementV2StartupImagesTestFixture(t)
	participants := fixture.reopen(t)
	changed, err := decodeAttemptSettlementV2Stats(t.Context(), images[1], participants[1].Stats.cfg, runtimeAttemptSettlementV2TestPersistence().MaxSnapshotBytes)
	if err != nil {
		t.Fatal(err)
	}
	changed.egressGeneration++
	encoded, err := encodeAttemptSettlementV2Engine(t.Context(), changed, runtimeAttemptSettlementV2TestPersistence().MaxSnapshotBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(filepath.Join(participants[1].StateDir, "stats.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	physical := attemptSettlementV2PhysicalIO()
	physical.startupImages = pins
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, data []byte) error {
		writes++
		return writeAttemptSettlementV2OwnedState(root, name, data)
	}
	err = recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, participants, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if err == nil || !strings.Contains(err.Error(), "metadata differs from the independently replayed exact image") || writes != 0 {
		t.Fatalf("changed independently replayed snapshot reached recovery mutation: writes=%d, %v", writes, err)
	}
	for _, participant := range participants {
		if participant.Stats.attemptLedger != nil || participant.Stats.settlementEpochKnown {
			t.Fatal("changed exact image partially published startup Stats")
		}
	}
}

// Original absence is a pin too; a late metadata journal cannot choose a new
// transaction after the independent all-image/current-cursor preflight.
func TestAttemptSettlementV2StartupImagesRejectLateJournalAppearance(t *testing.T) {
	t.Parallel()
	fixture, pins, _ := newAttemptSettlementV2StartupImagesTestFixture(t)
	path := attemptSettlementTransactionV2Path(fixture.coordinator)
	if err := atomicStateWrite(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	physical := attemptSettlementV2PhysicalIO()
	physical.startupImages = pins
	actual, exists, err := readAttemptSettlementV2Path(path, runtimeAttemptSettlementV2TestPersistence().MaxJournalBytes, physical)
	if err == nil || actual != nil || exists || !strings.Contains(err.Error(), "metadata differs from the independently replayed exact image") {
		t.Fatalf("late coordinator journal escaped exact absence pin: %v", err)
	}
}

// An existing checkpoint disappearing cannot be silently reclassified as a
// fresh installation, even though absence is a valid bounded read result.
func TestAttemptSettlementV2StartupImagesRejectLostSnapshot(t *testing.T) {
	t.Parallel()
	fixture, pins, _ := newAttemptSettlementV2StartupImagesTestFixture(t)
	path := filepath.Join(fixture.participants[0].StateDir, "stats.json")
	if err := os.Rename(path, filepath.Join(fixture.participants[0].StateDir, "preserved-stats.json")); err != nil {
		t.Fatal(err)
	}
	physical := attemptSettlementV2PhysicalIO()
	physical.startupImages = pins
	actual, exists, err := readAttemptSettlementV2Path(path, runtimeAttemptSettlementV2TestPersistence().MaxSnapshotBytes, physical)
	if err == nil || actual != nil || exists || !strings.Contains(err.Error(), "metadata differs from the independently replayed exact image") {
		t.Fatalf("lost snapshot became a fresh-install image: %v", err)
	}
}

// Borrowed source bytes and presence flags may be changed by a caller after
// construction; the exact consumer still uses the fully detached input set.
func TestAttemptSettlementV2StartupImagesOwnBorrowedByteAndPresenceSlices(t *testing.T) {
	t.Parallel()
	fixture, _, images := newAttemptSettlementV2StartupImagesTestFixture(t)
	present := []bool{true, true}
	pins, err := newAttemptSettlementV2StartupImages(t.Context(), fixture.coordinator, fixture.participants, images, present, nil, false, runtimeAttemptSettlementV2TestPersistence())
	if err != nil {
		t.Fatal(err)
	}
	images[0][0], present[0] = '!', false
	physical := attemptSettlementV2PhysicalIO()
	physical.startupImages = pins
	path := filepath.Join(fixture.participants[0].StateDir, "stats.json")
	actual, exists, err := readAttemptSettlementV2Path(path, runtimeAttemptSettlementV2TestPersistence().MaxSnapshotBytes, physical)
	if err != nil || !exists || len(actual) == 0 || actual[0] == '!' {
		t.Fatalf("borrowed image mutation changed admitted custody: %v", err)
	}
}

// A complete startup namespace cannot silently consume an unlisted closure
// selected from a snapshot epoch. Its independent history must add that pin.
func TestAttemptSettlementV2StartupImagesRejectUnpinnedTerminalRead(t *testing.T) {
	t.Parallel()
	fixture, pins, _ := newAttemptSettlementV2StartupImagesTestFixture(t)
	path := AttemptSettlementClosureV2Path(fixture.coordinator, 42)
	if err := ensurePrivateStateDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(path, []byte("{}\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	physical := attemptSettlementV2PhysicalIO()
	physical.startupImages = pins
	actual, exists, err := readAttemptSettlementV2Path(path, 1024, physical)
	if err == nil || actual != nil || exists || !strings.Contains(err.Error(), "consumed an unpinned metadata image") {
		t.Fatalf("snapshot-selected unpinned terminal escaped custody: %v", err)
	}
}

// Adding a closure detaches its complete bytes and permits only that exact
// independently replayed terminal path, never a caller-mutated byte alias.
func TestAttemptSettlementV2StartupImagesOwnAddedClosureBytes(t *testing.T) {
	t.Parallel()
	fixture, pins, _ := newAttemptSettlementV2StartupImagesTestFixture(t)
	path, encoded := AttemptSettlementClosureV2Path(fixture.coordinator, 42), []byte("{}\n")
	if err := ensurePrivateStateDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(path, encoded, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := pins.addClosure(t.Context(), fixture.coordinator, 42, encoded, 1024); err != nil {
		t.Fatal(err)
	}
	encoded[0] = '!'
	physical := attemptSettlementV2PhysicalIO()
	physical.startupImages = pins
	actual, exists, err := readAttemptSettlementV2Path(path, 1024, physical)
	if err != nil || !exists || !bytes.Equal(actual, []byte("{}\n")) {
		t.Fatalf("borrowed closure bytes changed admitted terminal: %v", err)
	}
	if err := pins.addClosure(t.Context(), fixture.coordinator, 42, []byte("{}\n"), 1024); err == nil {
		t.Fatal("duplicate closure pin replaced independent bytes")
	}
}

// Empty bytes do not mean a present empty checkpoint, and no participant can
// borrow another's finite snapshot capacity during the complete admission.
func TestAttemptSettlementV2StartupImagesRejectMalformedCensusAndBounds(t *testing.T) {
	t.Parallel()
	fixture, _, images := newAttemptSettlementV2StartupImagesTestFixture(t)
	for _, value := range []struct {
		images  [][]byte
		present []bool
		bounds  AttemptSettlementRuntimeV2PersistenceBounds
	}{
		{images: images[:1], present: []bool{true}, bounds: runtimeAttemptSettlementV2TestPersistence()},
		{images: [][]byte{nil, images[1]}, present: []bool{true, true}, bounds: runtimeAttemptSettlementV2TestPersistence()},
		{images: images, present: []bool{false, true}, bounds: runtimeAttemptSettlementV2TestPersistence()},
		{images: images, present: []bool{true, true}, bounds: AttemptSettlementRuntimeV2PersistenceBounds{MaxSnapshotBytes: uint64(len(images[0]) - 1), MaxJournalBytes: runtimeAttemptSettlementV2TestPersistence().MaxJournalBytes}},
	} {
		pins, err := newAttemptSettlementV2StartupImages(t.Context(), fixture.coordinator, fixture.participants, value.images, value.present, nil, false, value.bounds)
		if err == nil || pins != nil {
			t.Fatalf("invalid exact-image admission escaped: %v", err)
		}
	}
}

// Caller cancellation is checked before cloning any image or exposing even an
// inert byte fence that a later startup stage might accidentally consume.
func TestAttemptSettlementV2StartupImagesRefuseCanceledAdmission(t *testing.T) {
	t.Parallel()
	fixture, _, images := newAttemptSettlementV2StartupImagesTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pins, err := newAttemptSettlementV2StartupImages(ctx, fixture.coordinator, fixture.participants, images, []bool{true, true}, nil, false, runtimeAttemptSettlementV2TestPersistence())
	if !errors.Is(err, context.Canceled) || pins != nil {
		t.Fatalf("canceled exact-image admission escaped: %v", err)
	}
}
