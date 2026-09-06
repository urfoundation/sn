//go:build linux || darwin

package validator

// Explicit admission and persistence barriers verify that detached replay
// cannot lose concurrent events, overwrite a newer generation, or block readers.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urnetwork/connect"
)

// Each worker has one join boundary, reusable by assertions and deferred cleanup.
type statsWriteTestCall struct {
	done chan struct{}
	err  error
}

// No worker outlives its test-owned backend or temporary state directory.
func startStatsWriteTestCall(run func() error) *statsWriteTestCall {
	call := &statsWriteTestCall{done: make(chan struct{})}
	go func() {
		defer close(call.done)
		call.err = run()
	}()
	return call
}

// Reading the result only after completion avoids cross-goroutine result races.
func (self *statsWriteTestCall) join() error {
	<-self.done
	return self.err
}

// A genuine signed complete M8 trail is present but not yet applied to Stats.
func newStatsWriteReplayTest(t *testing.T) (*StatsEngine, *AttemptLedger, attemptRecordStoreTestFixture, string) {
	t.Helper()
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	stats.settlementEpoch, stats.settlementEpochKnown = 42, true
	stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = 1, 1
	if err := stats.Save(dir); err != nil {
		t.Fatal(err)
	}
	return stats, ledger, fixture, dir
}

// The first real Pending call waits at an explicit barrier. A second caller
// bypassing exclusive admission fails immediately instead of hiding in the
// same barrier and making a timeout the primary ownership witness.
func pauseStatsWriteReplayTest(ledger *AttemptLedger) (<-chan struct{}, func()) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls atomic.Uint32
	disk := ledger.disk
	ledger.disk = &attemptStatsReplayObservedBackend{attemptLedgerDiskBackend: disk, pending: func(ctx context.Context, visit func(AttemptRecord) error) error {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			select {
			case <-release:
			default:
				return errors.New("second replay bypassed exclusive admission")
			}
		}
		return disk.Pending(ctx, visit)
	}}
	return entered, func() { once.Do(func() { close(release) }) }
}

// Exact snapshots retain the full provider counters, not just a cursor marker.
func readStatsWriteSnapshotTest(t *testing.T, dir string) (statsSnapshot, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot statsSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot, raw
}

// Cancellation of a queued caller must complete while the first actual
// Pending owner is still blocked. The second caller never reaches the backend.
func TestStatsWriteCanceledReplayAdmissionJoinsBeforeCurrentOwner(t *testing.T) {
	stats, ledger, _, dir := newStatsWriteReplayTest(t)
	entered, release := pauseStatsWriteReplayTest(ledger)
	waiting := make(chan struct{})
	stats.writeHooks.step = func(operation, stage string) {
		if operation == "attach" && stage == "waiting" {
			close(waiting)
		}
	}
	first := startStatsWriteTestCall(func() error { return stats.AttachAttemptLedgerContext(context.Background(), ledger, dir) })
	defer func() { release(); _ = first.join() }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	second := startStatsWriteTestCall(func() error { return stats.AttachAttemptLedgerContext(ctx, ledger, dir) })
	defer func() { cancel(); _ = second.join() }()
	select {
	case <-waiting:
	case <-second.done:
		t.Fatalf("second replay bypassed the write reservation: %v", second.err)
	}
	cancel()
	if err := second.join(); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued replay did not join at cancellation: %v", err)
	}
	if !stats.mu.TryLock() {
		t.Fatal("blocked replay retained the public reader mutex")
	}
	stats.mu.Unlock()
	if len(stats.ProviderIDs()) != 0 {
		t.Fatal("unpublished replay became visible to a public reader")
	}
	release()
	if err := first.join(); err != nil || stats.attemptLastAppliedSequence != 8 {
		t.Fatalf("first real replay did not complete: %v", err)
	}
}

// A competing ledger is checked after admission against the published owner,
// before it can recover pending records or write any replacement snapshot.
func TestStatsWriteCompetingLedgerCannotReplaceCompletedReplay(t *testing.T) {
	stats, ledger, fixture, dir := newStatsWriteReplayTest(t)
	other := openAttemptLedgerDiskTest(t, newAttemptLedgerDiskTestStateDir(t), fixture, attemptLedgerDiskHooks{})
	entered, release := pauseStatsWriteReplayTest(ledger)
	waiting := make(chan struct{})
	writes := 0
	stats.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "attach" && stage == "waiting" {
			close(waiting)
		}
	}, writeSnapshot: func(path string, raw []byte) error {
		writes++
		return atomicStateWrite(path, raw, 0o600)
	}}
	first := startStatsWriteTestCall(func() error { return stats.AttachAttemptLedger(ledger, dir) })
	defer func() { release(); _ = first.join() }()
	<-entered
	second := startStatsWriteTestCall(func() error { return stats.AttachAttemptLedger(other, dir) })
	defer func() { release(); _ = second.join() }()
	select {
	case <-waiting:
	case <-second.done:
		t.Fatalf("different-ledger replay bypassed current ownership: %v", second.err)
	}
	release()
	if err := first.join(); err != nil {
		t.Fatal(err)
	}
	if err := second.join(); err == nil || !strings.Contains(err.Error(), "different attempt ledger") {
		t.Fatalf("competing ledger did not refuse the completed binding: %v", err)
	}
	snapshot, _ := readStatsWriteSnapshotTest(t, dir)
	if writes != 1 || stats.attemptLedger != ledger || snapshot.AttemptLastAppliedSequence != 8 {
		t.Fatalf("competing ledger replaced replay authority: writes=%d sequence=%d", writes, snapshot.AttemptLastAppliedSequence)
	}
	if head, err := other.Head(); err != nil || head.LastSequence != 0 {
		t.Fatalf("rejected second ledger was mutated: %+v/%v", head, err)
	}
}

// An event waiting behind replay is applied afterward and a subsequent Save
// includes it. Replay cannot publish its older seven-assignment clone later.
func TestStatsWriteReplayPreservesQueuedAssignmentAndSave(t *testing.T) {
	stats, ledger, fixture, dir := newStatsWriteReplayTest(t)
	entered, release := pauseStatsWriteReplayTest(ledger)
	waiting := make(chan struct{})
	writes := 0
	stats.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "record-assignment" && stage == "waiting" {
			close(waiting)
		}
	}, writeSnapshot: func(path string, raw []byte) error {
		writes++
		_ = stats.ProviderIDs()
		return atomicStateWrite(path, raw, 0o600)
	}}
	first := startStatsWriteTestCall(func() error { return stats.AttachAttemptLedger(ledger, dir) })
	defer func() { release(); _ = first.join() }()
	<-entered
	clientID := fixture.recordTs[7].Assignments[0].NextHop
	second := startStatsWriteTestCall(func() error {
		stats.RecordAssignment(clientID)
		return stats.Save(dir)
	})
	defer func() { release(); _ = second.join() }()
	select {
	case <-waiting:
	case <-second.done:
		t.Fatalf("queued event bypassed replay: %v", second.err)
	}
	release()
	if err := first.join(); err != nil {
		t.Fatal(err)
	}
	if err := second.join(); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := readStatsWriteSnapshotTest(t, dir)
	var assignments uint64
	for _, window := range snapshot.Window {
		assignments += window.Assignments
	}
	if writes != 2 || assignments != 8 || snapshot.AttemptLastAppliedSequence != 8 || stats.Exposure()[clientID] != 2 {
		t.Fatalf("replay lost a queued event or overwrote its save: writes=%d assignments=%d cursor=%d", writes, assignments, snapshot.AttemptLastAppliedSequence)
	}
}

// A valid native cut must acquire the replay's writer token before rotating;
// the old replay snapshot cannot overwrite its durable new egress generation.
func TestStatsWriteReplayCannotOverwriteQueuedNativeGeneration(t *testing.T) {
	stats, ledger, fixture, dir := newStatsWriteReplayTest(t)
	legacyDir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(legacyDir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := NewAttemptLedger(legacyDir, fixture.identity, fixture.validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	cut, err := legacy.BuildCut(fixture.recordTs[7].Boundary, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := pauseStatsWriteReplayTest(ledger)
	waiting := make(chan struct{})
	stats.writeHooks.step = func(operation, stage string) {
		if operation == "reconcile-native-cut" && stage == "waiting" {
			close(waiting)
		}
	}
	first := startStatsWriteTestCall(func() error { return stats.AttachAttemptLedger(ledger, dir) })
	defer func() { release(); _ = first.join() }()
	<-entered
	second := startStatsWriteTestCall(func() error { return stats.reconcileReleaseStatsCut(dir, 0, cut) })
	defer func() { release(); _ = second.join() }()
	select {
	case <-waiting:
	case <-second.done:
		t.Fatalf("new native generation bypassed replay ownership: %v", second.err)
	}
	release()
	if err := first.join(); err != nil {
		t.Fatal(err)
	}
	if err := second.join(); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := readStatsWriteSnapshotTest(t, dir)
	if snapshot.SettlementEpoch == nil || *snapshot.SettlementEpoch != 42 || snapshot.EgressGeneration != 1 || snapshot.AttemptLastAppliedSequence != 8 || snapshot.AttemptSettlementFirstSequence != 1 || snapshot.AttemptEgressFirstSequence != 9 {
		t.Fatalf("replay overwrote the later complete native generation: %+v", snapshot)
	}
	if stats.settlementEpoch != 42 || stats.egressGeneration != 1 {
		t.Fatal("memory and durable native generations differ")
	}
}

// Cancellation at the final pre-write boundary permits no snapshot or public
// binding. A fresh retry still replays the exact signed population once.
func TestStatsWriteReplayCancellationBeforeSnapshotDoesNotPublish(t *testing.T) {
	stats, ledger, _, dir := newStatsWriteReplayTest(t)
	_, before := readStatsWriteSnapshotTest(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := 0
	stats.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "attach" && stage == "before-snapshot" {
			cancel()
		}
	}, writeSnapshot: func(path string, raw []byte) error {
		writes++
		return atomicStateWrite(path, raw, 0o600)
	}}
	if err := stats.AttachAttemptLedgerContext(ctx, ledger, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-write cancellation was not returned: %v", err)
	}
	_, after := readStatsWriteSnapshotTest(t, dir)
	if writes != 0 || !bytes.Equal(before, after) || stats.attemptLedger != nil || stats.attemptLastAppliedSequence != 0 || len(stats.ProviderIDs()) != 0 {
		t.Fatal("canceled replay published partial statistics or a stale snapshot")
	}
	if err := stats.AttachAttemptLedgerContext(context.Background(), ledger, dir); err != nil {
		t.Fatal(err)
	}
	var assignments uint64
	for _, count := range stats.Exposure() {
		assignments += count
	}
	if writes != 1 || assignments != 7 || stats.attemptLastAppliedSequence != 8 {
		t.Fatalf("retry duplicated or omitted replay: writes=%d assignments=%d", writes, assignments)
	}
}

// Cancellation during an acknowledged writer cannot leave memory behind its
// already durable snapshot. The completed commit is returned successfully.
func TestStatsWriteReplayPublishesSuccessfulWriteDespiteLateCancellation(t *testing.T) {
	stats, ledger, _, dir := newStatsWriteReplayTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stats.writeHooks.writeSnapshot = func(path string, raw []byte) error {
		if err := atomicStateWrite(path, raw, 0o600); err != nil {
			return err
		}
		cancel()
		return nil
	}
	if err := stats.AttachAttemptLedgerContext(ctx, ledger, dir); err != nil || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("successful persistence was not the commit boundary: %v/%v", err, ctx.Err())
	}
	snapshot, _ := readStatsWriteSnapshotTest(t, dir)
	if stats.attemptLedger != ledger || stats.attemptLastAppliedSequence != 8 || snapshot.AttemptLastAppliedSequence != 8 {
		t.Fatal("late cancellation split memory from its durable replay")
	}
}

// The explicit defensive generation check occurs before any writer. A private
// fault simulates an unfenced state mutation; comparison after rename is too late.
func TestStatsWriteReplayGenerationMismatchRefusesBeforeSnapshot(t *testing.T) {
	stats, ledger, _, dir := newStatsWriteReplayTest(t)
	_, before := readStatsWriteSnapshotTest(t, dir)
	writes := 0
	stats.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "attach" && stage == "before-snapshot" {
			stats.mu.Lock()
			stats.egressGeneration++
			stats.mu.Unlock()
		}
	}, writeSnapshot: func(string, []byte) error { writes++; return nil }}
	if err := stats.AttachAttemptLedger(ledger, dir); err == nil || !strings.Contains(err.Error(), "replay generation changed") {
		t.Fatalf("stale replay was not rejected before persistence: %v", err)
	}
	_, after := readStatsWriteSnapshotTest(t, dir)
	if writes != 0 || !bytes.Equal(before, after) || stats.egressGeneration != 1 || stats.attemptLedger != nil || len(stats.ProviderIDs()) != 0 {
		t.Fatal("generation comparison occurred after a stale write or publication")
	}
}

// Every operational Stats mutator, including Save and the multi-engine owner,
// waits for the same reservation before inspecting or mutating state. Cases
// that refuse invalid application input still must not bypass that ordering.
func TestStatsWriteAllMutatorsRespectExclusiveAdmission(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	ledger := openAttemptLedgerDiskTest(t, newAttemptLedgerDiskTestStateDir(t), fixture, attemptLedgerDiskHooks{})
	clientID := fixture.recordTs[0].Assignments[0].NextHop
	cases := []struct {
		operation string
		invoke    func(*StatsEngine, string) error
	}{
		{operation: "record-assignment", invoke: func(stats *StatsEngine, _ string) error { stats.RecordAssignment(clientID); return nil }},
		{operation: "record-confirmation", invoke: func(stats *StatsEngine, _ string) error { stats.RecordConfirmation(clientID, 1); return nil }},
		{operation: "record-egress", invoke: func(stats *StatsEngine, _ string) error { stats.RecordEgressHash(clientID, [32]byte{1}); return nil }},
		{operation: "take-egress", invoke: func(stats *StatsEngine, _ string) error { _ = stats.TakeEgressIpHashes(); return nil }},
		{operation: "fold", invoke: func(stats *StatsEngine, _ string) error { stats.Fold(); return nil }},
		{operation: "save", invoke: func(stats *StatsEngine, dir string) error { return stats.Save(dir) }},
		{operation: "advance-epoch", invoke: func(stats *StatsEngine, dir string) error { return stats.AdvanceSettlementEpoch(42, dir) }},
		{operation: "load", invoke: func(stats *StatsEngine, dir string) error { return stats.Load(dir) }},
		{operation: "attach", invoke: func(stats *StatsEngine, dir string) error {
			return stats.AttachAttemptLedgerContext(context.Background(), ledger, dir)
		}},
		{operation: "checkpoint", invoke: func(stats *StatsEngine, _ string) error { return stats.checkpointAttempt(ledger, fixture.recordTs[0]) }},
		{operation: "begin-attempt", invoke: func(stats *StatsEngine, _ string) error { return stats.beginAttempt(42, ledger) }},
		{operation: "abort-attempt", invoke: func(stats *StatsEngine, _ string) error { stats.abortAttempt(); return nil }},
		{operation: "commit-attempt", invoke: func(stats *StatsEngine, _ string) error {
			_, err := stats.commitAttempt(ledger, nil, AttemptRecord{})
			return err
		}},
		{operation: "detach-native", invoke: func(stats *StatsEngine, dir string) error {
			_, err := stats.detachReleaseStatsMeasurement(dir, func(ReleaseStatsMeasurement, uint64) error { return nil })
			return err
		}},
		{operation: "detach-attempt-cut", invoke: func(stats *StatsEngine, dir string) error {
			_, err := stats.detachReleaseStatsMeasurementWithAttemptCut(dir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { return nil })
			return err
		}},
		{operation: "reconcile-native-cut", invoke: func(stats *StatsEngine, dir string) error { return stats.reconcileReleaseStatsCut(dir, 0) }},
		{operation: "settlement", invoke: func(stats *StatsEngine, dir string) error {
			return AdvanceAttemptSettlementEpoch(dir, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{{NoID: ledger.identity.NoID, StateDir: filepath.Dir(ledger.path), Stats: stats}})
		}},
	}
	for _, test := range cases {
		stats := NewStatsEngine(StatsConfig{})
		stats.attemptLedger, stats.activeAttemptCount = ledger, 1
		stats.settlementEpoch, stats.settlementEpochKnown = 42, true
		stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = 1, 1
		waiting := make(chan struct{})
		stats.writeHooks.step = func(operation, stage string) {
			if operation == test.operation && stage == "waiting" {
				close(waiting)
			}
		}
		reservation := stats.lockStatsWrite("test-startup")
		dir := t.TempDir()
		call := startStatsWriteTestCall(func() error { return test.invoke(stats, dir) })
		select {
		case <-waiting:
			if !stats.mu.TryLock() {
				reservation.release()
				_ = call.join()
				t.Fatalf("%s held state mutex while waiting for write ownership", test.operation)
			}
			stats.mu.Unlock()
		case <-call.done:
			reservation.release()
			t.Fatalf("%s bypassed exclusive Stats write admission: %v", test.operation, call.err)
		}
		reservation.release()
		_ = call.join()
	}
}

// Waiting for a later participant cannot retain an earlier participant's
// reader lock; cancellation releases all already acquired write reservations.
func TestStatsWriteCanceledMultiEngineAdmissionReleasesEarlierOwner(t *testing.T) {
	first := NewStatsEngine(StatsConfig{})
	firstDir, coordinator := t.TempDir(), t.TempDir()
	if err := first.Save(firstDir); err != nil {
		t.Fatal(err)
	}
	// Establish the intended earlier engine before creating the blocked one;
	// lock order is engine lifetime order, not caller-provided operator labels.
	second, ledger, _, secondDir := newStatsWriteReplayTest(t)
	_, before := readStatsWriteSnapshotTest(t, firstDir)
	entered, release := pauseStatsWriteReplayTest(ledger)
	waiting := make(chan struct{})
	second.writeHooks.step = func(operation, stage string) {
		if operation == "settlement" && stage == "waiting" {
			close(waiting)
		}
	}
	replay := startStatsWriteTestCall(func() error { return second.AttachAttemptLedger(ledger, secondDir) })
	defer func() { release(); _ = replay.join() }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := 0
	participants := []AttemptSettlementParticipant{{NoID: ledger.identity.NoID, StateDir: secondDir, Stats: second}, {NoID: 1, StateDir: firstDir, Stats: first}}
	settlement := startStatsWriteTestCall(func() error {
		return advanceAttemptSettlementEpochWithIOModeContext(ctx, coordinator, 43, attemptLedgerTestBoundary(), participants, func(string, []byte) error { writes++; return nil }, removeAttemptSettlementTransaction, true)
	})
	defer func() { cancel(); _ = settlement.join() }()
	select {
	case <-waiting:
	case <-settlement.done:
		t.Fatalf("multi-engine owner did not wait at its later participant: %v", settlement.err)
	}
	if !first.mu.TryLock() {
		cancel()
		_ = settlement.join()
		t.Fatal("queued multi-engine owner retained an earlier Stats reader mutex")
	}
	first.mu.Unlock()
	_ = first.ProviderIDs()
	cancel()
	if err := settlement.join(); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued multi-engine cancellation did not join: %v", err)
	}
	_, after := readStatsWriteSnapshotTest(t, firstDir)
	if writes != 0 || !bytes.Equal(before, after) || first.writeOwner != nil || first.attemptCutPending {
		t.Fatal("canceled multi-engine owner changed state or leaked its earlier token")
	}
	entries, err := os.ReadDir(coordinator)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled admission published a settlement journal: %v", err)
	}
	first.RecordAssignment(connect.NewId())
	if err := first.Save(firstDir); err != nil {
		t.Fatalf("earlier participant's write token was not released: %v", err)
	}
	release()
	if err := replay.join(); err != nil {
		t.Fatal(err)
	}
}

// Duplicate Stats pointers must fail before locking the same token twice.
func TestStatsWriteMultiEngineRejectsDuplicateOwnerBeforeIO(t *testing.T) {
	stats := NewStatsEngine(StatsConfig{})
	coordinator := t.TempDir()
	participants := []AttemptSettlementParticipant{{NoID: 1, StateDir: t.TempDir(), Stats: stats}, {NoID: 2, StateDir: t.TempDir(), Stats: stats}}
	writes := 0
	err := advanceAttemptSettlementEpochWithWrite(coordinator, 42, AttemptBoundary{}, participants, func(string, []byte) error { writes++; return nil })
	if err == nil || !strings.Contains(err.Error(), "share a statistics engine") || writes != 0 || stats.writeOwner != nil {
		t.Fatalf("duplicated owner was admitted: %d/%v", writes, err)
	}
	entries, err := os.ReadDir(coordinator)
	if err != nil || len(entries) != 0 {
		t.Fatalf("duplicate owner published filesystem state: %v", err)
	}
}

// Invalid late snapshot fields cannot publish an epoch or cursor before Load
// returns its error. A corrected snapshot can be retried on the same engine.
func TestStatsWriteLoadPublishesOnlyAfterCompleteValidation(t *testing.T) {
	stats := NewStatsEngine(StatsConfig{})
	dir := t.TempDir()
	epoch := uint64(42)
	clientID := connect.NewId()
	snapshot := statsSnapshot{Version: 4, SettlementEpoch: &epoch, EgressGeneration: 7, EmaPPM: map[string]uint32{clientID.String(): 1_000_001}}
	raw, err := encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(filepath.Join(dir, "stats.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stats.Load(dir); err == nil || stats.settlementEpochKnown || stats.egressGeneration != 0 || len(stats.ProviderIDs()) != 0 {
		t.Fatalf("late load failure published partial state: %v", err)
	}
	snapshot.EmaPPM[clientID.String()] = 500_000
	raw, err = encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(filepath.Join(dir, "stats.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stats.Load(dir); err != nil || !stats.settlementEpochKnown || stats.settlementEpoch != 42 || stats.egressGeneration != 7 || stats.emaPPM[clientID] != 500_000 {
		t.Fatalf("corrected full snapshot could not be retried: %v", err)
	}
}

// Invalid or already-canceled contexts cannot acquire startup ownership or
// enter even the first real backend callback.
func TestStatsWriteReplayRejectsContextBeforeBackendAdmission(t *testing.T) {
	stats, ledger, _, dir := newStatsWriteReplayTest(t)
	_, before := readStatsWriteSnapshotTest(t, dir)
	entered := 0
	disk := ledger.disk
	ledger.disk = &attemptStatsReplayObservedBackend{attemptLedgerDiskBackend: disk, pending: func(context.Context, func(AttemptRecord) error) error {
		entered++
		return errors.New("unexpected canceled replay backend admission")
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, input := range []context.Context{nil, ctx} {
		if err := stats.AttachAttemptLedgerContext(input, ledger, dir); err == nil {
			t.Fatal("invalid replay context was admitted")
		}
	}
	_, after := readStatsWriteSnapshotTest(t, dir)
	if entered != 0 || !bytes.Equal(before, after) || stats.writeOwner != nil || stats.attemptLedger != nil {
		t.Fatal("invalid replay context changed backend, snapshot or ownership")
	}
}

// A periodic Save's encoded older snapshot retains write ownership through
// its actual atomic write, so a later event and Save cannot be overwritten.
func TestStatsWriteSaveCannotOverwriteLaterEventSnapshot(t *testing.T) {
	stats := NewStatsEngine(StatsConfig{})
	clientID := connect.NewId()
	stats.RecordAssignment(clientID)
	dir := t.TempDir()
	if err := stats.Save(dir); err != nil {
		t.Fatal(err)
	}
	entered, release, waiting := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	writes := 0
	stats.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "record-assignment" && stage == "waiting" {
			close(waiting)
		}
	}, writeSnapshot: func(path string, raw []byte) error {
		writes++
		if !stats.mu.TryLock() {
			return errors.New("save writer retained the statistics state mutex")
		}
		stats.mu.Unlock()
		_ = stats.ProviderIDs()
		if writes == 1 {
			close(entered)
			<-release
		}
		return atomicStateWrite(path, raw, 0o600)
	}}
	first := startStatsWriteTestCall(func() error { return stats.Save(dir) })
	defer func() { unblock(); _ = first.join() }()
	select {
	case <-entered:
	case <-first.done:
		t.Fatalf("periodic save did not reach an unlocked writer: %v", first.err)
	}
	second := startStatsWriteTestCall(func() error { stats.RecordAssignment(clientID); return stats.Save(dir) })
	defer func() { unblock(); _ = second.join() }()
	select {
	case <-waiting:
	case <-second.done:
		t.Fatalf("event bypassed a still-unwritten save: %v", second.err)
	}
	if assignments, _ := stats.WindowCounts(clientID); assignments != 1 {
		t.Fatal("queued event mutated state before obtaining write ownership")
	}
	unblock()
	if err := first.join(); err != nil {
		t.Fatal(err)
	}
	if err := second.join(); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := readStatsWriteSnapshotTest(t, dir)
	if writes != 2 || snapshot.Window[clientID.String()] == nil || snapshot.Window[clientID.String()].Assignments != 2 {
		t.Fatal("older periodic save overwrote the later event snapshot")
	}
}

// The observed head still comes from the actual bounded backend.
type statsWriteHeadObservedBackend struct {
	attemptLedgerDiskBackend
	head func() (AttemptLedgerHead, error)
}

// A failure sentinel lets an old lock-holding path join before its assertion.
func (self *statsWriteHeadObservedBackend) Head() (AttemptLedgerHead, error) {
	return self.head()
}

// Startup, single-operator boundary checks and multi-operator preparation must all
// permit public Stats reads during the external durable-head lookup.
func TestStatsWriteDurableHeadCallbacksAllowPublicStatsReads(t *testing.T) {
	for _, operation := range []string{"attach", "advance", "settlement"} {
		stats, ledger, _, dir := newStatsWriteReplayTest(t)
		if operation != "attach" {
			if err := stats.AttachAttemptLedger(ledger, dir); err != nil {
				t.Fatal(err)
			}
		}
		entered, available := false, true
		escape := errors.New("durable head lookup retained Stats.mu")
		disk := ledger.disk
		ledger.disk = &statsWriteHeadObservedBackend{attemptLedgerDiskBackend: disk, head: func() (AttemptLedgerHead, error) {
			entered = true
			if !stats.mu.TryLock() {
				available = false
				return AttemptLedgerHead{}, escape
			}
			stats.mu.Unlock()
			_ = stats.ProviderIDs()
			return disk.Head()
		}}
		var err error
		switch operation {
		case "attach":
			err = stats.AttachAttemptLedger(ledger, dir)
		case "advance":
			err = stats.AdvanceSettlementEpoch(42, dir)
		default:
			err = AdvanceAttemptSettlementEpoch(t.TempDir(), 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{{NoID: ledger.identity.NoID, StateDir: dir, Stats: stats}})
		}
		if !entered || !available {
			t.Fatalf("%s did not permit public Stats reads at actual Head: %v", operation, err)
		}
		if operation == "settlement" {
			if !errors.Is(err, ErrAttemptLedgerStreamingRequired) {
				t.Fatalf("disk settlement lost its unactivated compact-cut guard: %v", err)
			}
		} else if err != nil {
			t.Fatalf("%s failed after its real head lookup: %v", operation, err)
		}
	}
}

// The public old generation may remain readable during an in-flight fold,
// but an unchanged refresh must obtain write ownership before treating that
// old epoch as current. Its fresh comparison cannot publish an obsolete view.
func TestStatsWriteUnchangedRefreshRechecksAfterInFlightEpochCommit(t *testing.T) {
	stats := NewStatsEngine(StatsConfig{})
	dir := t.TempDir()
	if err := stats.AdvanceSettlementEpoch(42, dir); err != nil {
		t.Fatal(err)
	}
	entered, release, waiting := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	stats.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "settlement" && stage == "waiting" {
			close(waiting)
		}
	}, writeSnapshot: func(path string, raw []byte) error {
		close(entered)
		<-release
		return atomicStateWrite(path, raw, 0o600)
	}}
	first := startStatsWriteTestCall(func() error { return stats.AdvanceSettlementEpoch(43, dir) })
	defer func() { unblock(); _ = first.join() }()
	<-entered
	participants := []AttemptSettlementParticipant{{NoID: 1, StateDir: dir, Stats: stats}}
	coordinator := t.TempDir()
	boundaryCalls := 0
	second := startStatsWriteTestCall(func() error {
		return advanceReleaseSettlementSnapshotWithMode(context.Background(), coordinator, &ReleaseSnapshot{Epoch: big.NewInt(42)}, participants, func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error) {
			boundaryCalls++
			return AttemptBoundary{}, errors.New("unchanged refresh unexpectedly resolved a boundary")
		}, false)
	})
	defer func() { unblock(); _ = second.join() }()
	select {
	case <-waiting:
	case <-second.done:
		t.Fatalf("unchanged refresh trusted the unpublished old epoch: %v", second.err)
	}
	unblock()
	if err := first.join(); err != nil {
		t.Fatal(err)
	}
	if err := second.join(); !errors.Is(err, errAttemptSettlementSnapshotStale) || boundaryCalls != 0 {
		t.Fatalf("unchanged refresh did not recheck committed epoch: %v calls=%d", err, boundaryCalls)
	}
	snapshot, _ := readStatsWriteSnapshotTest(t, dir)
	if snapshot.SettlementEpoch == nil || *snapshot.SettlementEpoch != 43 || stats.settlementEpoch != 43 {
		t.Fatal("unchanged refresh rewrote the later settlement generation")
	}
	entries, err := os.ReadDir(coordinator)
	if err != nil || len(entries) != 0 {
		t.Fatalf("stale unchanged refresh wrote a settlement artifact: %v", err)
	}
}
