//go:build linux || darwin

// A genuine durable native input can outlive an interrupted Stats write. Its
// next runtime poll must adopt that exact signed cut before terminal closure.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"maps"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Genuine protected uploads and immutable journal publication precede the
// injected snapshot error. The runtime's independent cursor has not advanced.
func newReleaseRuntimeNativeRecoveryTestFixture(t *testing.T) (*releaseRuntimeV2TestFixture, *ReleaseSteerer, *ReleaseSnapshot, []byte) {
	t.Helper()
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	fixture.startup.trail(t, 0)
	participant := fixture.runtime.history.participants[0]
	originalCursor := fixture.runtime.history.current[participant.NoID]
	failure := errors.New("synthetic interrupted native Stats publication")
	participant.Stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
		if stage == "temporary-closed" {
			return failure
		}
		return nil
	}
	boundary := fixture.startup.boundary
	hash, err := parseReleaseHex32("native recovery fixture Evm hash", boundary.EVMBlockHash, false)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(boundary.SettlementEpoch), BlockNumber: boundary.EVMBlock, BlockHash: hash}
	native := fixture.startup.nativeFixture
	if _, _, err := fixture.runtime.collect(t.Context(), steerer, nil, snapshot, 1, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2}); !errors.Is(err, failure) {
		t.Fatalf("fixture did not interrupt after durable native cut: %v", err)
	}
	path := releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 1, participant.NoID)
	before, err := readReleaseMeasurementInputV2(path, fixture.runtime.cfg.EvidenceV2.Bounds.MaxInputJournalBytes)
	if err != nil || fixture.runtime.history.inputByEpoch[1][participant.NoID] != nil || !reflect.DeepEqual(fixture.runtime.history.current[participant.NoID], originalCursor) {
		t.Fatalf("fixture did not preserve an unadopted original cut: %v", err)
	}
	participant.Stats.writeHooks.snapshotIO.after = nil
	return fixture, steerer, snapshot, before
}

func TestReleaseRuntimeNativeRecoveryUsesPublishedCutAtNewerSnapshot(t *testing.T) {
	fixture, steerer, snapshot, before := newReleaseRuntimeNativeRecoveryTestFixture(t)
	participant := fixture.runtime.history.participants[0]
	originalCursor := fixture.runtime.history.current[participant.NoID]
	boundary := fixture.startup.boundary
	path := releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 1, participant.NoID)
	posts := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
	later := *snapshot
	later.BlockNumber++
	later.BlockHash = [32]byte{0x79, 0x21}
	fixture.startup.blocks[later.BlockNumber] = later.BlockHash
	native := fixture.startup.nativeFixture
	inputs, options, err := fixture.runtime.collect(t.Context(), steerer, nil, &later, 1, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2})
	if err != nil || len(inputs) != 2 || len(options.Operators) != 2 {
		t.Fatalf("newer finalized retry could not adopt original signed cut: inputs=%d operators=%d error=%v", len(inputs), len(options.Operators), err)
	}
	retained := fixture.runtime.history.inputByEpoch[1][participant.NoID]
	cursor := fixture.runtime.history.current[participant.NoID]
	after, readErr := readReleaseMeasurementInputV2(path, fixture.runtime.cfg.EvidenceV2.Bounds.MaxInputJournalBytes)
	if readErr != nil || !bytes.Equal(before, after) || retained == nil || cursor.generation != originalCursor.generation+1 || cursor.lastBoundary != boundary || options.Operators[participant.NoID].Expected.Boundary != boundary || len(fixture.runtime.nativeInputNoIdKVs) != 0 {
		t.Fatalf("recovery rewrote signed authority or rotated twice: cursor=%+v error=%v", cursor, readErr)
	}
	for index, store := range fixture.stores {
		if !maps.Equal(posts[index], store.counts()) {
			t.Fatal("recovery republished an already durable native cut")
		}
	}
	if current, err := steerer.intents.Current(); err != nil || current != nil {
		t.Fatalf("input recovery created a native intent: %v / %v", current, err)
	}
	if err := fixture.runtime.advance(t.Context(), &ReleaseSnapshot{Epoch: big.NewInt(8), BlockNumber: 1501, BlockHash: fixture.startup.blocks[1501]}); err != nil {
		t.Fatalf("recovered native cursor could not close actual terminal: %v", err)
	}
}

// Settlement refresh is a distinct caller from native collection. It must
// finish the retained native cut before using the cursor for a terminal cut.
func TestReleaseRuntimeNativeRecoveryPrecedesBackgroundTerminal(t *testing.T) {
	fixture, steerer, _, before := newReleaseRuntimeNativeRecoveryTestFixture(t)
	participant := fixture.runtime.history.participants[0]
	original := fixture.runtime.history.current[participant.NoID]
	if err := fixture.runtime.advance(t.Context(), &ReleaseSnapshot{Epoch: big.NewInt(8), BlockNumber: 1501, BlockHash: fixture.startup.blocks[1501]}); err != nil {
		t.Fatalf("background terminal could not reconcile published native cut: %v", err)
	}
	input := fixture.runtime.history.inputByEpoch[1][participant.NoID]
	closure := fixture.runtime.history.terminals[7]
	cursor := fixture.runtime.history.current[participant.NoID]
	if input == nil || closure == nil || cursor.epoch != 8 || cursor.generation != original.generation+2 || len(fixture.runtime.nativeInputNoIdKVs) != 0 {
		t.Fatalf("terminal omitted native recovery: input=%v closure=%v cursor=%+v", input != nil, closure != nil, cursor)
	}
	for _, transition := range closure.Transitions {
		if transition.Identity.NoID == participant.NoID && (transition.Cut.Context.EgressGeneration != original.generation+1 || transition.Cut.Context.EgressFirstSequence != input.MeasurementInput.AttemptCutV2.LastSequence+1) {
			t.Fatal("terminal cut did not follow the original native cut")
		}
	}
	after, err := readReleaseMeasurementInputV2(releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 1, participant.NoID), fixture.runtime.cfg.EvidenceV2.Bounds.MaxInputJournalBytes)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("terminal recovery rewrote the native journal: %v", err)
	}
	if current, err := steerer.intents.Current(); err != nil || current != nil {
		t.Fatalf("background recovery created a native intent: %v / %v", current, err)
	}
}

// The next collection may already be in a newer settlement. Original signed
// input recovery must precede unsigned cancellation and produce the existing
// authenticated deferral, never a replacement input for the same native epoch.
func TestReleaseRuntimeNativeRecoveryKeepsClosedSettlementDeferral(t *testing.T) {
	fixture, steerer, _, before := newReleaseRuntimeNativeRecoveryTestFixture(t)
	fixture.runtime.cfg.ProvisionalDeferClosedNativeInput = true
	fixture.runtime.history.cfg.ProvisionalDeferClosedNativeInput = true
	native := fixture.startup.nativeFixture
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(8), BlockNumber: 1501, BlockHash: fixture.startup.blocks[1501]}
	inputs, options, err := fixture.runtime.collect(t.Context(), steerer, nil, snapshot, 1, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2})
	if !errors.Is(err, errProvisionalClosedNativeInput) || inputs != nil || !reflect.DeepEqual(options, ReleaseMeasurementV2Options{}) {
		t.Fatalf("new settlement did not authenticate original cut deferral: %v", err)
	}
	participant := fixture.runtime.history.participants[0]
	path := releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 1, participant.NoID)
	after, readErr := readReleaseMeasurementInputV2(path, fixture.runtime.cfg.EvidenceV2.Bounds.MaxInputJournalBytes)
	if readErr != nil || !bytes.Equal(before, after) || fixture.runtime.history.current[participant.NoID].epoch != 8 || fixture.runtime.history.terminals[7] == nil || len(fixture.runtime.nativeInputNoIdKVs) != 0 {
		t.Fatalf("closed deferral lost exact recovered evidence: %v", readErr)
	}
	if current, err := steerer.intents.Current(); err != nil || current != nil {
		t.Fatalf("closed native retry created an intent: %v / %v", current, err)
	}
}

// A cancelled poll does not consume the pending witness or erase an original
// signed journal. A later live owner may reconcile it without a second upload.
func TestReleaseRuntimeNativeRecoveryPreservesCancelledOwner(t *testing.T) {
	fixture, steerer, snapshot, before := newReleaseRuntimeNativeRecoveryTestFixture(t)
	participant := fixture.runtime.history.participants[0]
	pending := fixture.runtime.nativeInputNoIdKVs[participant.NoID]
	original := fixture.runtime.history.current[participant.NoID]
	posts := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := fixture.runtime.advance(ctx, snapshot); !errors.Is(err, context.Canceled) || fixture.runtime.nativeInputNoIdKVs[participant.NoID] != pending || !reflect.DeepEqual(original, fixture.runtime.history.current[participant.NoID]) {
		t.Fatalf("cancelled recovery consumed original authority: %v", err)
	}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatalf("live owner could not reconcile after cancellation: %v", err)
	}
	after, err := readReleaseMeasurementInputV2(releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 1, participant.NoID), fixture.runtime.cfg.EvidenceV2.Bounds.MaxInputJournalBytes)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("cancelled retry changed signed bytes: %v", err)
	}
	for index, store := range fixture.stores {
		if !maps.Equal(posts[index], store.counts()) {
			t.Fatal("cancelled retry duplicated original publication")
		}
	}
	if current, err := steerer.intents.Current(); err != nil || current != nil {
		t.Fatalf("cancelled input recovery created an intent: %v / %v", current, err)
	}
}

// Re-signing candidate data cannot replace the originally chosen cursor or
// boundary. Even if publication acknowledgement lost its byte hash, the
// separately retained context refuses the change before the Stats write.
func TestReleaseRuntimeNativeRecoveryRejectsResignedAuthorityChanges(t *testing.T) {
	for _, field := range []string{"boundary", "generation", "egress-cursor", "native-label"} {
		fixture, _, snapshot, before := newReleaseRuntimeNativeRecoveryTestFixture(t)
		participant := fixture.runtime.history.participants[0]
		pending := fixture.runtime.nativeInputNoIdKVs[participant.NoID]
		bounds := fixture.runtime.cfg.EvidenceV2.Bounds
		operator, err := fixture.runtime.operator(t.Context(), pending.authority.expected, "test-forged-input")
		if err != nil {
			t.Fatal(err)
		}
		journal, err := decodeReleaseMeasurementInputV2(t.Context(), before, releaseMeasurementInputV2Options{MaxJournalBytes: bounds.MaxInputJournalBytes, Stats: releaseStatsV2Options{Policy: operator.Policy, Bounds: bounds.Cut, Stats: AttemptCutV2StatsOptions{ExpectedConfig: operator.Measurement.ExpectedConfig, MaxProviders: bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes}}})
		if err != nil {
			t.Fatal(err)
		}
		cut := journal.MeasurementInput.AttemptCutV2
		switch field {
		case "boundary":
			cut.Context.Boundary.EVMBlock++
			cut.Context.Boundary.EVMBlockHash = attemptHex32([32]byte{0x51, 0x73})
			journal.MeasurementInput.CutEVMSnapshotBlock = cut.Context.Boundary.EVMBlock
			journal.MeasurementInput.CutEVMSnapshotHash = cut.Context.Boundary.EVMBlockHash
		case "generation":
			cut.Context.EgressGeneration++
			journal.MeasurementInput.EgressGeneration++
		case "egress-cursor":
			cut.Context.EgressFirstSequence++
		case "native-label":
			journal.MeasurementInput.CutNativeBlock--
			journal.MeasurementInput.CutNativeBlockHash = attemptHex32([32]byte{0x62, 0x31})
		}
		cut.Signature, err = cut.Sign(ed25519.PrivateKey(fixture.runtime.sources[participant.NoID].privateKey[:]), bounds.Cut)
		if err != nil {
			t.Fatalf("%s did not produce a structurally valid signature: %v", field, err)
		}
		forged, err := canonicalReleaseMeasurementInputBytes(journal)
		if err != nil {
			t.Fatal(err)
		}
		path := releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 1, participant.NoID)
		if err := os.WriteFile(path, forged, 0o600); err != nil {
			t.Fatal(err)
		}
		if field != "native-label" {
			pending.authority.journalBytes, pending.authority.journalHash = 0, [32]byte{}
		}
		writes := 0
		participant.Stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
			writes++
			return writeStatsSnapshotOwned(directory, write)
		}
		original := fixture.runtime.history.current[participant.NoID]
		err = fixture.runtime.advance(t.Context(), snapshot)
		if err == nil || writes != 0 || !reflect.DeepEqual(original, fixture.runtime.history.current[participant.NoID]) || fixture.runtime.history.inputByEpoch[1][participant.NoID] != nil {
			t.Fatalf("%s candidate replaced original authority: writes=%d error=%v", field, writes, err)
		}
	}
}

// An altered current cursor, conflicting same-height snapshot, or lost signed
// file is a refusal. None grants permission to manufacture a replacement cut.
func TestReleaseRuntimeNativeRecoveryRejectsChangedOwnerOrLostJournal(t *testing.T) {
	for _, field := range []string{"cursor", "boundary", "journal"} {
		fixture, _, snapshot, _ := newReleaseRuntimeNativeRecoveryTestFixture(t)
		participant := fixture.runtime.history.participants[0]
		path := releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 1, participant.NoID)
		switch field {
		case "cursor":
			cursor := fixture.runtime.history.current[participant.NoID]
			cursor.generation++
			fixture.runtime.history.current[participant.NoID] = cursor
		case "boundary":
			snapshot.BlockHash = [32]byte{0x16, 0x28}
		case "journal":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		original := fixture.runtime.history.current[participant.NoID]
		posts := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
		if err := fixture.runtime.advance(t.Context(), snapshot); err == nil || !reflect.DeepEqual(original, fixture.runtime.history.current[participant.NoID]) {
			t.Fatalf("%s changed owner was silently adopted: %v", field, err)
		}
		if fixture.runtime.history.inputByEpoch[1][participant.NoID] != nil || fixture.runtime.nativeInputNoIdKVs[participant.NoID] == nil {
			t.Fatalf("%s refusal discarded pending ownership", field)
		}
		for index, store := range fixture.stores {
			if !maps.Equal(posts[index], store.counts()) {
				t.Fatalf("%s refusal published replacement evidence", field)
			}
		}
		if field == "journal" {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("lost journal was recreated: %v", err)
			}
		}
	}
}

// The private loader rechecks an already pinned journal before its Stats
// owner. Disappearance between the first read and reacquisition is not fresh
// initial absence, even when every required parent directory still exists.
func TestReleaseRuntimeNativeRecoveryLoaderDoesNotRecreateLostPublication(t *testing.T) {
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	input := fixture.detach(t)
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
	encoded, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	authority := &releaseMeasurementInputV2CutAuthority{expected: input.AttemptCutV2.Context}
	if err := authority.retain(encoded, *input.AttemptCutV2, fixture.options.Stats.Bounds); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	options := fixture.fresh(t)
	options.cutAuthority = authority
	writes, generation := fixture.runtime.objects.writes, fixture.runtime.stats.egressGeneration
	_, err = fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, options)
	if err == nil || !strings.Contains(err.Error(), "retained compact native journal disappeared") || fixture.runtime.objects.writes != writes || fixture.runtime.stats.egressGeneration != generation {
		t.Fatalf("pinned absent journal authorized a new detach: %v", err)
	}
}

// Cancellation after the actual rename and directory Sync leaves a durable
// postimage even when the caller cannot acknowledge it. Recovery joins that
// postimage exactly once, rather than trying to advance a stale live cursor.
func TestReleaseRuntimeNativeRecoveryAfterDurableSnapshotCancellation(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	participant := fixture.runtime.history.participants[0]
	original := fixture.runtime.history.current[participant.NoID]
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	committed := 0
	participant.Stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
		if stage == "directory-synced" {
			committed++
			cancel()
		}
		return nil
	}
	boundary := fixture.startup.boundary
	hash, err := parseReleaseHex32("cancelled native fixture Evm hash", boundary.EVMBlockHash, false)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(boundary.SettlementEpoch), BlockNumber: boundary.EVMBlock, BlockHash: hash}
	native := fixture.startup.nativeFixture
	if _, _, err := fixture.runtime.collect(ctx, steerer, nil, snapshot, 1, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2}); !errors.Is(err, context.Canceled) || committed != 1 {
		t.Fatalf("cancellation missed durable snapshot boundary: commits=%d error=%v", committed, err)
	}
	postimage, _ := readStatsWriteSnapshotTest(t, participant.StateDir)
	if postimage.EgressGeneration != original.generation+1 || fixture.runtime.history.inputByEpoch[1][participant.NoID] != nil {
		t.Fatal("fixture did not leave a committed but unacknowledged generation")
	}
	participant.Stats.writeHooks.snapshotIO.after = nil
	posts := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatalf("durable cancelled snapshot could not reconcile: %v", err)
	}
	cursor := fixture.runtime.history.current[participant.NoID]
	if cursor.generation != original.generation+1 || participant.Stats.egressGeneration != cursor.generation || fixture.runtime.history.inputByEpoch[1][participant.NoID] == nil {
		t.Fatal("cancelled postimage was lost or rotated twice")
	}
	for index, store := range fixture.stores {
		if !maps.Equal(posts[index], store.counts()) {
			t.Fatal("postimage recovery duplicated a protected upload")
		}
	}
}

// A truly unsigned drain can retry at a later boundary. The published sibling
// keeps its original cut; an absent journal does not become an invented cut.
func TestReleaseRuntimeNativeRecoveryDoesNotPinUnpublishedBoundary(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	participant := fixture.runtime.history.participants[0]
	setActive := func(count uint64) {
		t.Helper()
		owner, err := participant.Stats.acquireStatsWrite(t.Context(), "test-native-drain")
		if err != nil {
			t.Fatal(err)
		}
		defer owner.release()
		candidate := owner.clone()
		candidate.activeAttemptCount = count
		owner.publish(candidate)
	}
	setActive(1)
	boundary := fixture.startup.boundary
	hash, err := parseReleaseHex32("unsigned native fixture Evm hash", boundary.EVMBlockHash, false)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(boundary.SettlementEpoch), BlockNumber: boundary.EVMBlock, BlockHash: hash}
	native := fixture.startup.nativeFixture
	if _, _, err := fixture.runtime.collect(t.Context(), steerer, nil, snapshot, 1, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2}); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("fixture did not wait for actual native drain: %v", err)
	}
	path := releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 1, participant.NoID)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) || fixture.runtime.nativeInputNoIdKVs[participant.NoID].authority.journalBytes != 0 {
		t.Fatalf("draining fixture already published a native input: %v", err)
	}
	setActive(0)
	snapshot.BlockNumber++
	snapshot.BlockHash = [32]byte{0x59, 0x41}
	fixture.startup.blocks[snapshot.BlockNumber] = snapshot.BlockHash
	_, options, err := fixture.runtime.collect(t.Context(), steerer, nil, snapshot, 1, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2})
	if err != nil {
		t.Fatalf("drained unsigned owner could not choose fresh boundary: %v", err)
	}
	if options.Operators[participant.NoID].Expected.Boundary.EVMBlock != snapshot.BlockNumber || len(fixture.runtime.nativeInputNoIdKVs) != 0 {
		t.Fatal("unpublished boundary replaced the actual fresh snapshot")
	}
	sibling := fixture.runtime.history.participants[1].NoID
	if options.Operators[sibling].Expected.Boundary != boundary {
		t.Fatal("fresh unsigned retry changed the published sibling cut")
	}
}
