//go:build linux || darwin

// Semantic startup controls consume the same real chain, signed streams and
// physical mutation owners as production. Refusal never substitutes a verdict.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func releaseStartupV2TestDiskImages(t *testing.T, disk *releaseEvidenceV2DiskState) [][]byte {
	t.Helper()
	images := make([][]byte, len(disk.participants))
	for index, participant := range disk.participants {
		encoded, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		images[index] = encoded
	}
	return images
}

func requireReleaseStartupV2Dormant(t *testing.T, disk *releaseEvidenceV2DiskState) {
	t.Helper()
	for _, participant := range disk.participants {
		state := disk.states[participant.NoID]
		if state.store != nil || state.stats != participant.Stats || participant.Stats.attemptLedger != nil || participant.Stats.attemptV2 != nil || participant.Stats.settlementEpochKnown {
			t.Fatal("failed semantic startup exposed a partial active operator")
		}
	}
}

// The complete pristine census acquires genuine activation snapshots and proof
// projections, while no terminal, steering intent or worker is manufactured.
func TestReleaseStartupV2InitializesActualCompletePristineBatch(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, false)
	before := slices.Clone(fixture.disk.participants)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	for index, participant := range fixture.disk.participants {
		stats := participant.Stats
		if participant.Stats != before[index].Stats || stats.attemptLedger != participant.Ledger || stats.attemptV2 == nil || stats.attemptV2.Activation != fixture.inputs[index].Context.InitialCut.Activation || stats.attemptV2.Terminal != nil || stats.settlementEpoch != 7 || stats.egressGeneration != 1 || stats.attemptCutPending || fixture.disk.states[participant.NoID].store == nil {
			t.Fatal("actual initial batch did not publish its exact complete ownership")
		}
	}
	for _, path := range []string{attemptSettlementTransactionV2Path(fixture.cfg.StateDir), filepath.Join(fixture.cfg.StateDir, "settlement-closures-v2"), filepath.Join(fixture.cfg.StateDir, "steering-intents.json")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("initialization invented terminal or intent state: %v", err)
		}
	}
}

// The native journal exists before Begin and before the snapshot rename. Its
// real M8 rows must survive, with exactly one egress rotation after recovery.
func TestReleaseStartupV2RecoversRealM8CutBeforeBegin(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	journal := fixture.ordinary(t, 0, 1, true)
	if journal.MeasurementInput.AttemptCutV2.RecordCount != 8 || fixture.disk.participants[0].Stats.egressGeneration != 1 {
		t.Fatal("fixture did not interrupt a genuine pre-rotation M8 cut")
	}
	fixture.reopen(t)
	var before [2]int
	for index, store := range fixture.stores {
		_, _, before[index] = store.snapshot()
	}
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	stats := fixture.disk.participants[0].Stats
	if stats.egressGeneration != 2 || stats.attemptLastAppliedSequence != 8 || stats.attemptEgressFirstSequence != 9 || stats.attemptSettlementFirstSequence != 1 || len(stats.window) == 0 || len(stats.egress) != 0 || stats.attemptCutPending {
		t.Fatal("real crash-before-Begin recovery lost rows or repeated the native rotation")
	}
	for index, store := range fixture.stores {
		_, _, reads := store.snapshot()
		if reads <= before[index] {
			t.Fatal("startup skipped one actual public replay origin")
		}
	}
	if fixture.disk.participants[0].Ledger.identity.ValidatorUID != 1 || fixture.nativeFixture.uid != 2 {
		t.Fatal("current native registration replaced historical ledger identity")
	}
	fixture.prepareEngines(t)
	fixture.trail(t, 0)
	if stats.attemptLastAppliedSequence != 16 {
		t.Fatal("recovered real operator did not continue its retained ledger")
	}
}

// A completed cut followed by real observations retains their exact current
// raw state. Startup does not clear later egress or fold a native window twice.
func TestReleaseStartupV2PreservesCompletedCutAndRealSuffix(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	fixture.ordinary(t, 0, 1, false)
	fixture.trail(t, 0)
	if err := fixture.disk.participants[0].Stats.Save(fixture.disk.participants[0].StateDir); err != nil {
		t.Fatal(err)
	}
	want := releaseStartupV2TestDiskImages(t, fixture.disk)
	fixture.reopen(t)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	actual := releaseStartupV2TestDiskImages(t, fixture.disk)
	for index := range want {
		if !bytes.Equal(want[index], actual[index]) {
			t.Fatal("startup changed a completed native generation or exact suffix counters")
		}
	}
	if fixture.disk.participants[0].Stats.egressGeneration != 2 || len(fixture.disk.participants[0].Stats.egress) == 0 {
		t.Fatal("actual suffix egress was absent or rotated twice")
	}
}

// Empty signed cuts still advance generation. An old canonical activation
// snapshot passes local zero-counter checks but cannot choose a rollback prefix.
func TestReleaseStartupV2RejectsSnapshotSelectedNativeRollback(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	old := releaseStartupV2TestDiskImages(t, fixture.disk)[0]
	fixture.ordinary(t, 0, 1, false)
	fixture.ordinary(t, 0, 2, false)
	path := filepath.Join(fixture.disk.participants[0].StateDir, "stats.json")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	writes := 0
	physical := attemptSettlementV2PhysicalIO()
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected rollback recovery write")
	}
	err := fixture.start(t.Context(), physical)
	if err == nil || !strings.Contains(err.Error(), "independently replayed current cursor") || writes != 0 {
		t.Fatalf("snapshot-selected rollback entered recovery: %v/%d", err, writes)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// A skipped generation cannot be justified by a real valid cut that does not
// describe it. The range and all zero counters deliberately remain plausible.
func TestReleaseStartupV2RejectsInventedCurrentGeneration(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.ordinary(t, 0, 1, false)
	stats := fixture.disk.participants[0].Stats
	snapshot := stats.snapshotStats()
	snapshot.EgressGeneration += 7
	encoded, err := encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.disk.participants[0].StateDir, "stats.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err == nil || !strings.Contains(err.Error(), "independently replayed current cursor") {
		t.Fatalf("invented current generation was accepted: %v", err)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// Native epoch is observed through the actual metadata/stake reader, not
// accepted from an unsigned filename or an otherwise valid signed EVM cut.
func TestReleaseStartupV2RejectsNativeFilenameClockSubstitution(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	journal := fixture.ordinary(t, 0, 1, false)
	old := releaseMeasurementInputV2Path(fixture.cfg.StateDir, 1, journal.MeasurementInput.NoID)
	journal.SubnetEpoch = 2
	encoded, err := canonicalReleaseMeasurementInputBytes(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, releaseMeasurementInputV2Path(fixture.cfg.StateDir, 2, journal.MeasurementInput.NoID)); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err == nil || !strings.Contains(err.Error(), "native epoch") {
		t.Fatalf("unsigned native clock substitution was accepted: %v", err)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// Altering a real second-origin proof exercises complete stream replay, not
// merely a header/signature check or a trusted first-replica success flag.
func TestReleaseStartupV2RequiresActualSecondOriginProofReplay(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	fixture.ordinary(t, 0, 1, false)
	fixture.reopen(t)
	fixture.stores[1].readBytes = func(kind string, encoded []byte) []byte {
		if kind == AttemptStreamV2Proofs {
			encoded[len(encoded)/2] ^= 1
		}
		return encoded
	}
	err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO())
	if err == nil || !strings.Contains(err.Error(), "origin 1 ordinary replay") {
		t.Fatalf("altered real replica proof was accepted: %v", err)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// Actual terminal folds establish positive a_min8 EMA, then a genuinely empty
// next settlement preserves that exact prior through full history and restart.
func TestReleaseStartupV2RecoversConsecutiveRealTerminalsWithPositiveEMA(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	for range 15 {
		fixture.trail(t, 0)
	}
	first := fixture.terminal(t, false)
	if len(first.Transitions[0].PostFold) == 0 {
		t.Fatal("real M8 terminal did not exercise positive EMA")
	}
	second := fixture.terminal(t, false)
	if second.Transitions[0].Cut.RecordCount != 0 || !slices.Equal(first.Transitions[0].PostFold, second.Transitions[0].PostFold) {
		t.Fatal("real empty successor did not retain the first positive fold")
	}
	want := releaseStartupV2TestDiskImages(t, fixture.disk)
	fixture.reopen(t)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	for index, participant := range fixture.disk.participants {
		actual, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil || !bytes.Equal(want[index], actual) || participant.Stats.settlementEpoch != 9 || participant.Stats.egressGeneration != 3 || participant.Stats.attemptV2.Terminal == nil {
			t.Fatalf("complete real terminal recovery changed its exact carried state: %v", err)
		}
	}
}

// A durable all-operator journal can precede immutable closure publication and
// contain mixed physical pre/post snapshots. Real recovery must finish it once.
func TestReleaseStartupV2FinishesActualPartialTerminalJournal(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	closure := fixture.terminal(t, true)
	if _, err := os.Lstat(AttemptSettlementClosureV2Path(fixture.cfg.StateDir, 7)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial fixture already published terminal: %v", err)
	}
	fixture.reopen(t)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(AttemptSettlementClosureV2Path(fixture.cfg.StateDir, 7))
	if err != nil {
		t.Fatal(err)
	}
	want, err := marshalAttemptSettlementV2JSON(t.Context(), closure, fixture.cfg.EvidenceV2.Bounds.MaxClosureBytes, false, true)
	if err != nil || !bytes.Equal(want, encoded) {
		t.Fatalf("recovery resealed or changed the actual journal closure: %v", err)
	}
	for _, participant := range fixture.disk.participants {
		if participant.Stats.settlementEpoch != 8 || participant.Stats.egressGeneration != 2 || fixture.disk.states[participant.NoID].store == nil {
			t.Fatal("partial terminal recovery did not publish the whole exact successor")
		}
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.cfg.StateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed journal was not removed durably: %v", err)
	}
}

// Even a latest valid closure cannot select the beginning of history. Missing
// an earlier terminal is detected before replay can adopt the current snapshot.
func TestReleaseStartupV2RejectsMissingEarlierTerminal(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.terminal(t, false)
	fixture.terminal(t, false)
	path := AttemptSettlementClosureV2Path(fixture.cfg.StateDir, 7)
	if err := os.Rename(path, filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "saved-first-closure.json")); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err == nil || !strings.Contains(err.Error(), "history has a gap") {
		t.Fatalf("snapshot-selected terminal prefix was accepted: %v", err)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// Replay and all image checks can succeed while the derived proof view is
// corrupt. That late refusal still cannot expose any public Stats or store.
func TestReleaseStartupV2ProjectionFailureKeepsWholeBatchDormant(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	fixture.ordinary(t, 0, 1, false)
	path := filepath.Join(fixture.disk.participants[0].StateDir, "proofs.jsonl")
	if err := os.WriteFile(path, []byte("foreign projection\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err == nil {
		t.Fatal("conflicting actual proof projection was accepted")
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != "foreign projection\n" {
		t.Fatalf("failed projection replaced conflicting evidence: %v", err)
	}
}

// Real physical admission is after semantic replay. Changing a canonical
// image there must fail at the actual byte consumer, before its first write.
func TestReleaseStartupV2RechecksExactSnapshotAtMutationOwner(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.reopen(t)
	changed, writes := false, 0
	physical := attemptSettlementV2PhysicalIO()
	physical.step = func(phase string) error {
		if !changed && strings.HasPrefix(phase, "before-stats-clone-") {
			changed = true
			candidate := fixture.disk.snapshots[1].snapshotStats()
			candidate.EgressGeneration++
			encoded, err := encodeStatsSnapshot(candidate)
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(fixture.disk.participants[1].StateDir, "stats.json"), encoded, 0o600)
		}
		return nil
	}
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected changed-image write")
	}
	err := fixture.start(t.Context(), physical)
	if err == nil || !strings.Contains(err.Error(), "independently replayed exact image") || !changed || writes != 0 {
		t.Fatalf("changed image crossed the real consumer: %v/%v/%d", err, changed, writes)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// A transport callback can retarget input bytes after their first read. The
// retained complete census rejects this before any recovery image is written.
func TestReleaseStartupV2RetainsInputCustodyAcrossPublicReplay(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	journal := fixture.ordinary(t, 0, 1, false)
	fixture.reopen(t)
	var once sync.Once
	var mutateErr error
	fixture.stores[1].readBytes = func(kind string, encoded []byte) []byte {
		if kind == AttemptStreamV2Proofs {
			once.Do(func() {
				mutateErr = os.WriteFile(releaseMeasurementInputV2Path(fixture.cfg.StateDir, 1, journal.MeasurementInput.NoID), []byte("changed native journal\n"), 0o600)
			})
		}
		return encoded
	}
	err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO())
	// HTTP completion precedes return, so the actual mutation callback is joined.
	if mutateErr != nil {
		t.Fatal(mutateErr)
	}
	if err == nil || !strings.Contains(err.Error(), "file census changed") {
		t.Fatalf("changed input survived public replay custody: %v", err)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// Cancellation after an actual durable snapshot does not lend authority to
// the original public batch. Durable retry evidence remains for the next open.
func TestReleaseStartupV2LateCancellationKeepsPublicBatchDormant(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.reopen(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	physical := attemptSettlementV2PhysicalIO()
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, encoded []byte) error {
		err := writeAttemptSettlementV2OwnedState(root, name, encoded)
		cancel()
		return err
	}
	if err := fixture.start(ctx, physical); !errors.Is(err, context.Canceled) {
		t.Fatalf("late startup cancellation escaped: %v", err)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// Observers cannot shorten the retained destination map while all real I/O
// succeeds. Publication uses the original complete destination ownership.
func TestReleaseStartupV2RejectsLateDestinationCensusMutation(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.reopen(t)
	states := []*releaseAttemptState{fixture.disk.states[fixture.disk.participants[0].NoID], fixture.disk.states[fixture.disk.participants[1].NoID]}
	physical := attemptSettlementV2PhysicalIO()
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, encoded []byte) error {
		delete(fixture.disk.states, fixture.disk.participants[1].NoID)
		return writeAttemptSettlementV2OwnedState(root, name, encoded)
	}
	if err := fixture.start(t.Context(), physical); err == nil || !strings.Contains(err.Error(), "publication census changed") {
		t.Fatalf("changed public destinations were accepted: %v", err)
	}
	for _, state := range states {
		if state.store != nil || state.stats.attemptLedger != nil {
			t.Fatal("changed destination map exposed an active original")
		}
	}
}

func TestReleaseStartupV2PrecancelHasNoSnapshotMutation(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := fixture.start(ctx, attemptSettlementV2PhysicalIO()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancellation was ignored: %v", err)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
	for _, image := range releaseStartupV2TestDiskImages(t, fixture.disk) {
		if len(image) != 0 {
			t.Fatal("pre-canceled startup wrote a snapshot")
		}
	}
}
