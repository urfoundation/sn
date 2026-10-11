//go:build linux || darwin

package validator

// Ordinary runtime controls use genuine M8 trails, real signed disk rows and
// actual snapshot/journal I/O. Barriers observe writer ownership; mutation
// controls change bytes or routing inputs, never cryptographic verdicts.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// Immutable object transport is shared with the full sealer/replay fixtures.
// Every operation receives fresh scratch while retaining the same real bytes.
type releaseStatsV2RuntimeTestFixture struct {
	source                  *attemptCutV2SealTestFixture
	stats                   *StatsEngine
	ledger                  *AttemptLedger
	dir                     string
	options                 releaseStatsV2Options
	objects                 *attemptCutV2SealTestObjects
	initialEgressGeneration uint64
}

// This test-only capture is also persisted before the real snapshot writer.
type releaseStatsV2RuntimeTestInput struct {
	Stats ReleaseStatsMeasurement `json:"stats"`
	Cut   AttemptCutV2            `json:"cut"`
}

// No reliability minimum, trail budget or disk capacity is changed.
func newReleaseStatsV2RuntimeTestFixture(t *testing.T, completed, failed int) *releaseStatsV2RuntimeTestFixture {
	t.Helper()
	source := newAttemptCutV2SealTestFixture(t, 8, completed, failed)
	fixture := releaseStatsV2RuntimeTestFixtureFor(t, source, source.engine.stats, source.ledger)
	if fixture.initialEgressGeneration != 0 {
		t.Fatalf("first settlement epoch before ledger attachment started at egress generation %d, want 0", fixture.initialEgressGeneration)
	}
	if err := fixture.stats.Save(fixture.dir); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// Census capacities are the genuine source's complete raw provider/hash set.
func releaseStatsV2RuntimeTestFixtureFor(t *testing.T, source *attemptCutV2SealTestFixture, stats *StatsEngine, ledger *AttemptLedger) *releaseStatsV2RuntimeTestFixture {
	t.Helper()
	seal, objects := newAttemptCutV2SealTestOptions(t, source)
	raw := source.engine.stats.currentReleaseStatsMeasurement()
	var hashes uint64
	for _, provider := range raw.Providers {
		hashes += uint64(len(provider.EgressIPHashHexes))
	}
	options := releaseStatsV2Options{
		Activation: source.expected.Activation, Policy: source.policy, Bounds: source.bounds, Seal: seal,
		Stats: AttemptCutV2StatsOptions{ExpectedConfig: raw.Config, MaxProviders: max(1, uint64(len(raw.Providers))), MaxEgressHashes: max(1, hashes),
			Replay: AttemptCutV2ReplayOptions{Bounds: source.replay, ScratchDirectory: filepath.Join(t.TempDir(), "stats-replay"), ServerKeys: source.server.serverPublicKeys(), ReadMetadata: seal.ReadMetadata, OpenData: seal.OpenData}},
	}
	return &releaseStatsV2RuntimeTestFixture{source: source, stats: stats, ledger: ledger, dir: filepath.Dir(ledger.path), options: options, objects: objects,
		initialEgressGeneration: stats.snapshotStats().EgressGeneration}
}

// Replays use new scratch, not a reused verified-result cache or fake reader.
func (self *releaseStatsV2RuntimeTestFixture) fresh(t *testing.T) releaseStatsV2Options {
	t.Helper()
	options := self.options
	options.Seal.ScratchDirectory = filepath.Join(t.TempDir(), "seal")
	options.Stats.Replay.ScratchDirectory = filepath.Join(t.TempDir(), "stats")
	return options
}

// Publishing this bounded file uses the same actual immutable journal writer.
func releaseStatsV2RuntimeTestCapture(path string, captured *releaseStatsV2RuntimeTestInput) func(ReleaseStatsMeasurement, AttemptCutV2) error {
	return func(stats ReleaseStatsMeasurement, cut AttemptCutV2) error {
		input := releaseStatsV2RuntimeTestInput{Stats: stats, Cut: cut}
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if err := writeReleaseMeasurementInputV2(path, encoded, 1024*1024); err != nil {
			return err
		}
		*captured = input
		return nil
	}
}

// Rows come from a second actual M8 trail with disjoint real provider IDs.
// The independent target attaches at the first terminal prefix, not a forged
// snapshot, and startup later replays the remaining authenticated rows.
func newReleaseStatsV2RuntimePrefixTestFixture(t *testing.T) *releaseStatsV2RuntimeTestFixture {
	t.Helper()
	source := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	source.server.mu.Lock()
	for index := range source.server.providers {
		source.server.providers[index] = connect.NewId()
	}
	source.server.mu.Unlock()
	proof, err := source.engine.RunTrail(t.Context())
	if err != nil || proof == nil || proof.M != 8 || len(proof.Hops) != 8 {
		t.Fatalf("second genuine M8 trail: %v", err)
	}
	if err := source.ledger.Walk(t.Context(), 9, 16, func(record AttemptRecord) error { source.recordTs = append(source.recordTs, record); return nil }); err != nil {
		t.Fatal(err)
	}
	dir := newAttemptLedgerDiskTestStateDir(t)
	stats := NewStatsEngine(source.engine.stats.cfg)
	if err := stats.AdvanceSettlementEpoch(42, dir); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewDiskAttemptLedger(t.Context(), dir, source.ledger.identity, attemptLedgerDiskTestCoordinator, source.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	if err := stats.AttachAttemptLedger(ledger, dir); err != nil {
		t.Fatal(err)
	}
	for _, record := range source.recordTs[:8] {
		appended, err := ledger.AppendContext(t.Context(), record)
		if err != nil || !reflect.DeepEqual(appended, &record) {
			t.Fatalf("real prefix append: %v", err)
		}
	}
	if err := stats.AttachAttemptLedger(ledger, dir); err != nil {
		t.Fatal(err)
	}
	return releaseStatsV2RuntimeTestFixtureFor(t, source, stats, ledger)
}

// A real directory at stats.json forces the production rename to
// fail. The previous regular snapshot is preserved and restored explicitly.
func obstructReleaseStatsV2RuntimeTestSnapshot(t *testing.T, dir string) func() {
	t.Helper()
	path, preserved := filepath.Join(dir, "stats.json"), filepath.Join(dir, "stats-before-compact.json")
	if err := os.Rename(path, preserved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(preserved, path); err != nil {
			t.Fatal(err)
		}
		restored = true
	}
	t.Cleanup(restore)
	return restore
}

// Actual policy replay precedes journal publication; only egress rotates.
func TestReleaseStatsV2RuntimeDetachesGenuineM8AndFailure(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeDetachesGenuineM8AndFailure")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 2, 1)
	before := fixture.stats.snapshotStats()
	raw := fixture.stats.currentReleaseStatsMeasurement()
	var captured releaseStatsV2RuntimeTestInput
	path := filepath.Join(ordinaryInputCommitTestDir(t), "input.json")
	measurement, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), func(stats ReleaseStatsMeasurement, cut AttemptCutV2) error {
		if current := fixture.stats.snapshotStats(); !reflect.DeepEqual(current, before) {
			return errors.New("public reader observed an uncommitted rotation")
		}
		return releaseStatsV2RuntimeTestCapture(path, &captured)(stats, cut)
	})
	if err != nil || cut == nil || cut.RecordCount != 18 || cut.CompleteCount != 2 || cut.FailedCount != 1 || !reflect.DeepEqual(measurement, raw) || !reflect.DeepEqual(captured.Cut, *cut) {
		t.Fatalf("genuine runtime detach cut=%+v: %v", cut, err)
	}
	after := fixture.stats.snapshotStats()
	if after.EgressGeneration != before.EgressGeneration+1 || after.AttemptEgressFirstSequence != 19 || len(after.Egress) != 0 || !reflect.DeepEqual(after.Window, before.Window) || !reflect.DeepEqual(after.EmaPPM, before.EmaPPM) || after.AttemptLastAppliedSequence != 18 {
		t.Fatalf("rotation changed quality or cursor: %+v", after)
	}
	if fixture.ledger.records != nil || fixture.ledger.pending != nil || fixture.ledger.terminal != nil || fixture.objects.reads == 0 || fixture.objects.closes == 0 {
		t.Fatal("runtime did not use genuine bounded disk/object replay")
	}
	loaded := NewStatsEngine(fixture.stats.cfg)
	if err := loaded.Load(fixture.dir); err != nil || !reflect.DeepEqual(loaded.snapshotStats(), after) {
		t.Fatalf("durable rotated snapshot differs: %v", err)
	}
}

// An admitted attempt drains under the existing barrier before object work.
func TestReleaseStatsV2RuntimeDrainsAndRetainsReservation(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeDrainsAndRetainsReservation")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	if err := fixture.stats.beginAttempt(42, fixture.ledger); err != nil {
		t.Fatal(err)
	}
	var captured releaseStatsV2RuntimeTestInput
	persist := releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured)
	_, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), persist)
	if !errors.Is(err, errAttemptCutPending) || cut != nil || fixture.objects.writes != 0 || fixture.stats.activeAttemptCount != 1 || !fixture.stats.attemptCutPending {
		t.Fatalf("active attempt admission was lost: %v", err)
	}
	if err := fixture.stats.beginAttempt(42, fixture.ledger); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("new attempt crossed pending cut: %v", err)
	}
	fixture.stats.abortAttempt()
	_, cut, err = fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), persist)
	if err != nil || cut == nil || fixture.stats.attemptCutPending || fixture.stats.egressGeneration != fixture.initialEgressGeneration+1 {
		t.Fatalf("drained retry: %v", err)
	}
}

// The journal callback retains the write token but never the state mutex.
// A queued cancellable writer cannot cross it; a later real Save sees rotation.
func TestReleaseStatsV2RuntimeOwnsWriterAcrossJournal(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeOwnsWriterAcrossJournal")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	entered, release, queued := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	fixture.stats.writeHooks.step = func(operation, stage string) {
		if operation == "detach-native-v2" && stage == "before-journal" {
			close(entered)
			<-release
		}
		if operation == "runtime-queue-control" && stage == "waiting" {
			close(queued)
		}
	}
	options := fixture.fresh(t)
	var captured releaseStatsV2RuntimeTestInput
	persist := releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured)
	done := make(chan error, 1)
	go func() {
		_, _, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, options, persist)
		done <- err
	}()
	<-entered
	before := fixture.stats.snapshotStats()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiter := make(chan error, 1)
	go func() {
		owner, err := fixture.stats.acquireStatsWrite(ctx, "runtime-queue-control")
		if owner != nil {
			owner.release()
		}
		waiter <- err
	}()
	<-queued
	cancel()
	waitErr := <-waiter
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(waitErr, context.Canceled) || before.EgressGeneration != fixture.initialEgressGeneration || fixture.stats.egressGeneration != fixture.initialEgressGeneration+1 {
		t.Fatalf("queued writer or public generation crossed retained owner: %v", waitErr)
	}
	if err := fixture.stats.Save(fixture.dir); err != nil {
		t.Fatal(err)
	}
}

// Cancellation after genuine full replay but before journal admission cannot
// publish a cut or clear the existing drain reservation.
func TestReleaseStatsV2RuntimeCancelBeforeJournal(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeCancelBeforeJournal")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.stats.writeHooks.step = func(operation, stage string) {
		if operation == "detach-native-v2" && stage == "before-journal" {
			cancel()
		}
	}
	calls := 0
	_, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(ctx, fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), func(ReleaseStatsMeasurement, AttemptCutV2) error { calls++; return nil })
	if !errors.Is(err, context.Canceled) || cut != nil || calls != 0 || fixture.objects.closes == 0 || fixture.stats.egressGeneration != fixture.initialEgressGeneration || !fixture.stats.attemptCutPending {
		t.Fatalf("canceled journal crossed boundary: %v", err)
	}
}

// The hook performs the actual disk write before canceling. A committed
// snapshot still publishes, matching the preexisting Stats commit boundary.
func TestReleaseStatsV2RuntimePublishesSuccessfulCanceledCommit(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimePublishesSuccessfulCanceledCommit")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writes := 0
	fixture.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		writes++
		err := writeStatsSnapshotOwned(directory, write)
		cancel()
		return err
	}
	var captured releaseStatsV2RuntimeTestInput
	_, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(ctx, fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured))
	if err != nil || cut == nil || writes != 1 || ctx.Err() == nil || fixture.stats.egressGeneration != fixture.initialEgressGeneration+1 || fixture.stats.attemptCutPending {
		t.Fatalf("committed cancellation lost publication: %v", err)
	}
	loaded := NewStatsEngine(fixture.stats.cfg)
	if err := loaded.Load(fixture.dir); err != nil || loaded.egressGeneration != fixture.initialEgressGeneration+1 {
		t.Fatalf("real committed snapshot: %v", err)
	}
}

// The sealer fetch-back succeeds on real bytes. Only the independent Stats
// replay sees corrupted bytes, and must reject them before journal publication.
func TestReleaseStatsV2RuntimeFullyReplaysBeforePublication(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeFullyReplaysBeforePublication")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	options := fixture.fresh(t)
	open := options.Stats.Replay.OpenData
	corrupted, closed, publications := 0, 0, 0
	options.Stats.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil {
			return reader, err
		}
		if kind != AttemptStreamV2Proofs {
			return reader, nil
		}
		raw, err := io.ReadAll(reader)
		if err = errors.Join(err, reader.Close()); err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			return nil, errors.New("genuine proof object is empty")
		}
		raw[len(raw)/2] ^= 1
		corrupted++
		return &attemptCutV2SealTestReader{Reader: bytes.NewReader(raw), close: func() error { closed++; return nil }}, nil
	}
	_, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, options, func(ReleaseStatsMeasurement, AttemptCutV2) error { publications++; return nil })
	if err == nil || cut != nil || corrupted == 0 || closed != corrupted || publications != 0 || fixture.stats.egressGeneration != fixture.initialEgressGeneration {
		t.Fatalf("independent full replay accepted altered proof: %v", err)
	}
}

// Standalone sealing may capture an earlier prefix. Runtime publication must
// additionally prove it is still the exact Stats-owned applied head.
func TestReleaseStatsV2RuntimeRejectsObjectCallbackHeadDrift(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeRejectsObjectCallbackHeadDrift")
		}
	})
	fixture := newReleaseStatsV2RuntimePrefixTestFixture(t)
	options := fixture.fresh(t)
	write := options.Seal.WriteRecords
	appended, publications := false, 0
	options.Seal.WriteRecords = func(ctx context.Context, hash string, raw []byte) error {
		if err := write(ctx, hash, raw); err != nil {
			return err
		}
		if !appended {
			for _, record := range fixture.source.recordTs[8:] {
				if _, err := fixture.ledger.AppendContext(ctx, record); err != nil {
					return err
				}
			}
			appended = true
		}
		return nil
	}
	_, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, options, func(ReleaseStatsMeasurement, AttemptCutV2) error { publications++; return nil })
	head, headErr := fixture.ledger.Head()
	if err == nil || cut != nil || !appended || publications != 0 || headErr != nil || head.LastSequence != 16 || fixture.stats.attemptLastAppliedSequence != 8 || fixture.stats.egressGeneration != fixture.initialEgressGeneration {
		t.Fatalf("runtime accepted unowned genuine suffix: %v; head=%+v", err, head)
	}
}

// A real rename failure leaves the journal and old snapshot. Reopen then
// replays a genuine later suffix; reconciliation retains only that suffix's
// egress and all quality counters, not a cleared or twice-applied window.
func TestReleaseStatsV2RuntimeReconcilesRealSuffixAfterRenameFailure(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeReconcilesRealSuffixAfterRenameFailure")
		}
	})
	fixture := newReleaseStatsV2RuntimePrefixTestFixture(t)
	before := fixture.stats.snapshotStats()
	restore := obstructReleaseStatsV2RuntimeTestSnapshot(t, fixture.dir)
	path := filepath.Join(ordinaryInputCommitTestDir(t), "input.json")
	var captured releaseStatsV2RuntimeTestInput
	_, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), releaseStatsV2RuntimeTestCapture(path, &captured))
	var rename *os.LinkError
	if !errors.As(err, &rename) || cut != nil || rename.Op != "rename" || captured.Cut.LastSequence != 8 || !reflect.DeepEqual(fixture.stats.snapshotStats(), before) || !fixture.stats.attemptCutPending {
		t.Fatalf("real journal-first rename failure: %v", err)
	}
	for _, record := range fixture.source.recordTs[8:] {
		if _, err := fixture.ledger.AppendContext(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	restore()
	if err := fixture.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewDiskAttemptLedger(t.Context(), fixture.dir, fixture.source.ledger.identity, attemptLedgerDiskTestCoordinator, fixture.source.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	stats := NewStatsEngine(fixture.stats.cfg)
	if err := stats.Load(fixture.dir); err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(reopened, fixture.dir); err != nil {
		t.Fatal(err)
	}
	encoded, err := readReleaseMeasurementInputV2(path, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	var journal releaseStatsV2RuntimeTestInput
	if err := json.Unmarshal(encoded, &journal); err != nil {
		t.Fatal(err)
	}
	if err := stats.reconcileReleaseStatsCutV2(t.Context(), fixture.dir, journal.Stats, journal.Cut, fixture.fresh(t)); err != nil {
		t.Fatal(err)
	}
	after := stats.snapshotStats()
	full := fixture.source.engine.stats.snapshotStats()
	expectedEgress := map[string][]string{}
	for _, hop := range fixture.source.recordTs[15].Proof.Hops[1:] {
		expectedEgress[hop.ClientId.String()] = []string{attemptHex32(hop.EgressIpHash)}
	}
	if after.EgressGeneration != before.EgressGeneration+1 || after.AttemptEgressFirstSequence != 9 || after.AttemptLastAppliedSequence != 16 || !reflect.DeepEqual(after.Egress, expectedEgress) || !reflect.DeepEqual(after.Window, full.Window) || !reflect.DeepEqual(after.EmaPPM, full.EmaPPM) || stats.attemptCutPending {
		t.Fatalf("same-generation suffix reconciliation differs: %+v", after)
	}
	loaded := NewStatsEngine(stats.cfg)
	if err := loaded.Load(fixture.dir); err != nil || !reflect.DeepEqual(loaded.snapshotStats(), after) {
		t.Fatalf("reconciled durable suffix differs: %v", err)
	}
}

// Reusing an earlier journal fully replays its evidence but cannot consume a
// newer generation's active attempt or its already-installed drain barrier.
func TestReleaseStatsV2RuntimeLaterGenerationPreservesNewPending(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeLaterGenerationPreservesNewPending")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	var captured releaseStatsV2RuntimeTestInput
	persist := releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured)
	if _, _, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), persist); err != nil {
		t.Fatal(err)
	}
	if err := fixture.stats.beginAttempt(42, fixture.ledger); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), persist); !errors.Is(err, errAttemptCutPending) {
		t.Fatal(err)
	}
	before := fixture.stats.snapshotStats()
	reads := fixture.objects.reads
	writes := 0
	fixture.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		writes++
		return writeStatsSnapshotOwned(directory, write)
	}
	if err := fixture.stats.reconcileReleaseStatsCutV2(t.Context(), fixture.dir, captured.Stats, captured.Cut, fixture.fresh(t)); err != nil {
		t.Fatal(err)
	}
	if writes != 0 || fixture.objects.reads <= reads || !reflect.DeepEqual(fixture.stats.snapshotStats(), before) || !fixture.stats.attemptCutPending || fixture.stats.activeAttemptCount != 1 {
		t.Fatal("old journal consumed newer native ownership")
	}
	fixture.stats.abortAttempt()
}

// The genuine all-operator advance installs terminal ownership while a real
// admitted attempt drains; ordinary recovery must not clear or borrow it.
func TestReleaseStatsV2RuntimePreservesTerminalPendingOwner(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimePreservesTerminalPendingOwner")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	var captured releaseStatsV2RuntimeTestInput
	if _, _, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.stats.beginAttempt(42, fixture.ledger); err != nil {
		t.Fatal(err)
	}
	err := AdvanceAttemptSettlementEpoch(newAttemptLedgerDiskTestStateDir(t), 43, fixture.source.expected.Boundary, []AttemptSettlementParticipant{{NoID: 9, StateDir: fixture.dir, Stats: fixture.stats}})
	if !errors.Is(err, errAttemptCutPending) || !fixture.stats.attemptSettlementCutPending || fixture.stats.attemptSettlementCutEpoch != 43 {
		t.Fatalf("actual terminal drain barrier: %v", err)
	}
	reads := fixture.objects.reads
	if err := fixture.stats.reconcileReleaseStatsCutV2(t.Context(), fixture.dir, captured.Stats, captured.Cut, fixture.fresh(t)); !errors.Is(err, errAttemptCutPending) {
		t.Fatal(err)
	}
	if fixture.objects.reads != reads || fixture.stats.egressGeneration != fixture.initialEgressGeneration+1 || !fixture.stats.attemptCutPending || !fixture.stats.attemptSettlementCutPending || fixture.stats.attemptSettlementCutEpoch != 43 {
		t.Fatal("ordinary journal stole terminal ownership")
	}
	fixture.stats.abortAttempt()
}

// A genuine VPK signature on another declared prefix does not make that
// prefix the authentic local ledger row. No object verdict is replaced.
func TestReleaseStatsV2RuntimeRejectsSignedFalsePrefix(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeRejectsSignedFalsePrefix")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	var captured releaseStatsV2RuntimeTestInput
	if _, _, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured)); err != nil {
		t.Fatal(err)
	}
	cut := captured.Cut
	cut.Root = attemptHex32([32]byte{0x71})
	var err error
	cut.Signature, err = cut.Sign(fixture.source.key, fixture.options.Bounds)
	if err != nil {
		t.Fatal(err)
	}
	before, reads := fixture.stats.snapshotStats(), fixture.objects.reads
	if err := fixture.stats.reconcileReleaseStatsCutV2(t.Context(), fixture.dir, captured.Stats, cut, fixture.fresh(t)); err == nil {
		t.Fatal("signed false local prefix was accepted")
	}
	if fixture.objects.reads != reads || !reflect.DeepEqual(before, fixture.stats.snapshotStats()) {
		t.Fatal("false prefix changed state or reached external replay")
	}
}

// V1 history is produced by real terminal advance, migrated unchanged to the
// disk ledger, then carried across an empty v2 native cut in the next epoch.
func TestReleaseStatsV2RuntimePreservesGenuineCarriedV1Transition(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimePreservesGenuineCarriedV1Transition")
		}
	})
	source := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	dir := newAttemptLedgerDiskTestStateDir(t)
	legacy, err := NewAttemptLedger(dir, source.ledger.identity, source.key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	stats := NewStatsEngine(source.engine.stats.cfg)
	if err := stats.AdvanceSettlementEpoch(42, dir); err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(legacy, dir); err != nil {
		t.Fatal(err)
	}
	for _, record := range source.recordTs {
		if _, err := legacy.AppendContext(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	if err := stats.AttachAttemptLedger(legacy, dir); err != nil {
		t.Fatal(err)
	}
	if err := AdvanceAttemptSettlementEpoch(newAttemptLedgerDiskTestStateDir(t), 43, source.expected.Boundary, []AttemptSettlementParticipant{{NoID: 9, StateDir: dir, Stats: stats}}); err != nil {
		t.Fatal(err)
	}
	transition := stats.settlementTransition
	if transition == nil || transition.PreFold.AttemptCut == nil || len(transition.PreFold.AttemptCut.Records) != 8 || VerifyAttemptSettlementTransition(transition) != nil {
		t.Fatal("carried control lacks genuine signed v1 terminal history")
	}
	legacyBytes, err := os.ReadFile(legacy.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := NewDiskAttemptLedger(t.Context(), dir, source.ledger.identity, attemptLedgerDiskTestCoordinator, source.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disk.Close() })
	restarted := NewStatsEngine(stats.cfg)
	if err := restarted.Load(dir); err != nil {
		t.Fatal(err)
	}
	if err := restarted.AttachAttemptLedger(disk, dir); err != nil {
		t.Fatal(err)
	}
	fixture := releaseStatsV2RuntimeTestFixtureFor(t, source, restarted, disk)
	fixture.options.Activation.FirstSequence, fixture.options.Activation.PriorRoot = 9, source.recordTs[7].RecordHash
	fixture.options.Activation.Domain.ActivationEpoch = 43
	boundary := AttemptBoundary{SettlementEpoch: 43, EVMBlock: 101, EVMBlockHash: attemptHex32([32]byte{9})}
	var captured releaseStatsV2RuntimeTestInput
	raw, cut, err := restarted.detachReleaseStatsMeasurementV2(t.Context(), dir, boundary, fixture.fresh(t), releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured))
	if err != nil || cut == nil || cut.RecordCount != 0 || cut.Context.FirstSequence != 9 || raw.SettlementTransition != nil || raw.AttemptCut != nil || !reflect.DeepEqual(restarted.settlementTransition, transition) {
		t.Fatalf("v2 cut changed carried v1 authority: %v", err)
	}
	preserved, err := os.ReadFile(legacy.path)
	if err != nil || !bytes.Equal(preserved, legacyBytes) {
		t.Fatalf("migration rewrote historical v1 ledger: %v", err)
	}
	loaded := NewStatsEngine(stats.cfg)
	if err := loaded.Load(dir); err != nil || !reflect.DeepEqual(loaded.settlementTransition, transition) {
		t.Fatalf("v1 transition not durably retained: %v", err)
	}
}

// Scratch aliasing and finite census refusal precede all object writes.
func TestReleaseStatsV2RuntimeRejectsAliasedScratchAndCensus(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimeRejectsAliasedScratchAndCensus")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	for _, edit := range []func(*releaseStatsV2Options){
		func(options *releaseStatsV2Options) {
			options.Stats.Replay.ScratchDirectory = options.Seal.ScratchDirectory
		},
		func(options *releaseStatsV2Options) {
			options.Stats.Replay.ScratchDirectory = filepath.Join(options.Seal.ScratchDirectory, "nested")
		},
		func(options *releaseStatsV2Options) {
			options.Seal.ScratchDirectory = filepath.Join(options.Stats.Replay.ScratchDirectory, "nested")
		},
		func(options *releaseStatsV2Options) { options.Stats.MaxProviders = 0 },
		func(options *releaseStatsV2Options) { options.Stats.MaxEgressHashes = 1 },
		func(options *releaseStatsV2Options) { options.Stats.ExpectedConfig.AMin++ },
	} {
		options := fixture.fresh(t)
		edit(&options)
		_, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, options, func(ReleaseStatsMeasurement, AttemptCutV2) error { return errors.New("unexpected publication") })
		if err == nil || cut != nil || fixture.objects.writes != 0 || fixture.stats.egressGeneration != fixture.initialEgressGeneration {
			t.Fatalf("scratch/census preflight accepted: %v", err)
		}
	}
}

// Callback mutation cannot rewrite the returned raw slices or signed header.
func TestReleaseStatsV2RuntimePublicationReceivesOwnedCopies(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseStatsV2RuntimePublicationReceivesOwnedCopies")
		}
	})
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	raw := fixture.stats.currentReleaseStatsMeasurement()
	var captured releaseStatsV2RuntimeTestInput
	path := filepath.Join(ordinaryInputCommitTestDir(t), "input.json")
	measurement, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), func(stats ReleaseStatsMeasurement, publication AttemptCutV2) error {
		if err := releaseStatsV2RuntimeTestCapture(path, &captured)(stats, publication); err != nil {
			return err
		}
		stats.Providers[0].LatencyBuckets[0]++
		stats.Providers[0].ClientID = "changed-by-recipient"
		publication.Signature[0] ^= 1
		return nil
	})
	if err != nil || cut == nil || !reflect.DeepEqual(measurement, raw) {
		t.Fatalf("callback mutated returned raw statistics: %v", err)
	}
	if err := cut.VerifyHeader(cut.Context, fixture.options.Bounds); err != nil {
		t.Fatal(err)
	}
	encoded, err := readReleaseMeasurementInputV2(path, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	var journal releaseStatsV2RuntimeTestInput
	if err := json.Unmarshal(encoded, &journal); err != nil || !reflect.DeepEqual(journal.Stats, raw) || !reflect.DeepEqual(journal.Cut, *cut) {
		t.Fatalf("immutable published bytes changed: %v", err)
	}
	if fixture.stats.egressGeneration != fixture.initialEgressGeneration+1 {
		t.Fatal("copy control did not complete a real rotation")
	}
}
