//go:build linux || darwin

package validator

// Clone-boundary controls observe actual owner acquisition and real counters.
// They do not substitute a clone, a replay result or an encoder allocation.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// Every callback is outside Stats.mu; the observer sees the real pre-copy
// boundary. A refusal must precede that boundary for every operator.
func runtimeAttemptSettlementV2BorrowedObserver(t *testing.T, fixture *attemptSettlementRuntimeV2TestFixture, copies, reads, writes *int) attemptSettlementV2IO {
	t.Helper()
	physical := attemptSettlementV2PhysicalIO()
	physical.step = func(stage string) error {
		if strings.HasPrefix(stage, "before-stats-clone-") {
			*copies = *copies+1
			for _, participant := range fixture.participants {
				if !participant.Stats.mu.TryLock() { t.Fatal("borrowed observer ran under a Stats state lock") }
				owned := participant.Stats.writeOwner != nil
				participant.Stats.mu.Unlock()
				if !owned { t.Fatal("clone boundary preceded complete writer ownership") }
			}
		}
		return nil
	}
	physical.closeFile = func(file *os.File) error { *reads = *reads+1; return file.Close() }
	physical.writeJournal = func(root *attemptPrivateDirectory, name string, data []byte) error { *writes = *writes+1; return writeAttemptSettlementV2OwnedState(root, name, data) }
	physical.writeSnapshot = physical.writeJournal
	return physical
}

// Generic counters are deliberately out of the signed window here: the
// assertion is resource admission before any clone, not acceptance as proofs.
func TestAttemptSettlementRuntimeV2BorrowedProviderCensusBeforeClone(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	for index := byte(1); index <= 33; index++ { fixture.participants[1].Stats.RecordAssignment(connect.Id{index}) }
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	copies, reads, writes := 0, 0, 0
	physical := runtimeAttemptSettlementV2BorrowedObserver(t, fixture, &copies, &reads, &writes)
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if err == nil || !strings.Contains(err.Error(), "borrowed provider census") || copies != 0 || reads != 0 || writes != 0 { t.Fatalf("provider census was copied before complete borrowed admission: copies=%d reads=%d writes=%d err=%v", copies, reads, writes, err) }
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) { t.Fatal("borrowed provider refusal changed live statistics") }
}

// Distinct real hash insertions exhaust the unchanged sixteen-hash allowance.
func TestAttemptSettlementRuntimeV2BorrowedEgressCensusBeforeClone(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	for index := byte(1); index <= 17; index++ { fixture.participants[1].Stats.RecordEgressHash(connect.Id{1}, [32]byte{index}) }
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	copies, reads, writes := 0, 0, 0
	physical := runtimeAttemptSettlementV2BorrowedObserver(t, fixture, &copies, &reads, &writes)
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if err == nil || !strings.Contains(err.Error(), "borrowed egress census") || copies != 0 || reads != 0 || writes != 0 { t.Fatalf("egress census was copied before complete borrowed admission: copies=%d reads=%d writes=%d err=%v", copies, reads, writes, err) }
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) { t.Fatal("borrowed egress refusal changed live statistics") }
}

// Each map individually fits 32; its exact union has 33. The prior maps are
// produced by the real compatibility Fold and overlap each other completely.
func TestAttemptSettlementRuntimeV2BorrowedUnionBeforeClone(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	stats := fixture.participants[1].Stats
	for index := byte(1); index <= 17; index++ { stats.RecordAssignment(connect.Id{index}) }
	if err := stats.Fold(); err != nil { t.Fatal(err) }
	for index := byte(18); index <= 33; index++ { stats.RecordAssignment(connect.Id{index}) }
	stats.RecordEgressHash(connect.Id{1}, [32]byte{1})
	if len(stats.ema) != 17 || len(stats.emaPPM) != 17 || len(stats.window) != 16 { t.Fatal("real union prerequisite differs") }
	copies, reads, writes := 0, 0, 0
	physical := runtimeAttemptSettlementV2BorrowedObserver(t, fixture, &copies, &reads, &writes)
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if err == nil || !strings.Contains(err.Error(), "borrowed provider union") || copies != 0 || reads != 0 || writes != 0 { t.Fatalf("overlapping provider maps escaped union admission before cloning: copies=%d reads=%d writes=%d err=%v", copies, reads, writes, err) }
}

// A real signed nonempty historical image is counted before copying its maps
// or the current wrapper. The immutable transition pointer is never cloned.
func TestAttemptSettlementRuntimeV2BorrowedSnapshotBytesBeforeClone(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2LegacyHistoryFixture(t, true)
	images := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	limit := uint64(len(images[0]))
	if uint64(len(images[1])) > limit { limit = uint64(len(images[1])) }
	bounds := runtimeAttemptSettlementV2TestPersistence(); bounds.MaxSnapshotBytes = limit-1
	copies, reads, writes := 0, 0, 0
	physical := runtimeAttemptSettlementV2BorrowedObserver(t, fixture, &copies, &reads, &writes)
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.options(t).Authority, bounds, physical)
	if err == nil || !strings.Contains(err.Error(), "borrowed snapshot") || copies != 0 || reads != 0 || writes != 0 { t.Fatalf("signed snapshot exceeded its bytes only after a collection copy: copies=%d reads=%d writes=%d err=%v", copies, reads, writes, err) }
	if !reflect.DeepEqual(images, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) { t.Fatal("borrowed byte refusal changed signed history") }
}

// This is an explicit synchronous boundary-mutation seam, not a concurrent
// writer. No borrowed iteration is active while the observer changes the map.
func TestAttemptSettlementRuntimeV2BorrowedBoundaryReadmission(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	copies, reads, writes := 0, 0, 0
	physical := runtimeAttemptSettlementV2BorrowedObserver(t, fixture, &copies, &reads, &writes)
	observe := physical.step
	physical.step = func(stage string) error {
		if err := observe(stage); err != nil { return err }
		if stage == "before-stats-clone-9" {
			stats := fixture.participants[1].Stats
			if !stats.mu.TryLock() { t.Fatal("borrowed mutation seam retained a state lock") }
			for index := byte(1); index <= 33; index++ { stats.window[connect.Id{index}] = &ProviderWindow{Assignments: 1} }
			stats.mu.Unlock()
		}
		return nil
	}
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if err == nil || !strings.Contains(err.Error(), "borrowed provider census") || copies != 2 || reads != 0 || writes != 0 { t.Fatalf("clone boundary change was not re-admitted before copying: observers=%d reads=%d writes=%d err=%v", copies, reads, writes, err) }
	for _, participant := range fixture.participants {
		if participant.Stats.attemptCutPending || participant.Stats.attemptV2 != nil { t.Fatal("borrowed boundary refusal published a candidate") }
		if _, err := os.Lstat(filepath.Join(participant.StateDir, "stats.json")); err != nil { t.Fatal(err) }
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) { t.Fatalf("borrowed boundary refusal wrote a journal: %v", err) }
}

// Valid empty activation still acquires every owner and persists real images.
func TestAttemptSettlementRuntimeV2BorrowedAllTokensBeforeCopy(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	copies, reads, writes := 0, 0, 0
	physical := runtimeAttemptSettlementV2BorrowedObserver(t, fixture, &copies, &reads, &writes)
	if err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err != nil { t.Fatal(err) }
	if copies != 2 || reads == 0 || writes != 3 { t.Fatalf("real admitted activation did not reach all owners and durable images: copies=%d reads=%d writes=%d", copies, reads, writes) }
}

// Exact comparisons use the unchanged actual codec for empty, legacy-signed,
// v6-carried and real current M8 statistics, including nil versus empty maps.
func TestAttemptSettlementRuntimeV2BorrowedExactRepresentation(t *testing.T) {
	compare := func(stats *StatsEngine) {
		owner := stats.lockStatsWrite("borrowed-size-test")
		defer owner.release()
		actual, err := encodeStatsSnapshot(stats.snapshotStats()); if err != nil { t.Fatal(err) }
		size, err := countAttemptSettlementV2Engine(t.Context(), stats, uint64(len(actual)))
		if err != nil || size != uint64(len(actual)) { t.Fatalf("borrowed counter differs from the real snapshot: size=%d actual=%d err=%v", size, len(actual), err) }
		if _, err := countAttemptSettlementV2Engine(t.Context(), stats, uint64(len(actual)-1)); err == nil { t.Fatal("borrowed counter accepted one-short allowance") }
		encoded, err := encodeAttemptSettlementV2Engine(t.Context(), stats, uint64(len(actual)))
		if err != nil || !bytes.Equal(encoded, actual) { t.Fatalf("borrowed encoder changed actual snapshot bytes: %v", err) }
	}
	compare(&StatsEngine{})
	compare(NewStatsEngine(StatsConfig{}))
	fixture := newAttemptSettlementRuntimeV2LegacyHistoryFixture(t, true)
	for _, participant := range fixture.participants { compare(participant.Stats) }
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil { t.Fatal(err) }
	for _, participant := range fixture.participants { compare(participant.Stats) }
	fixture.trails(t, 0, 2, 1)
	fixture.trails(t, 1, 1, 0)
	for _, participant := range fixture.participants { compare(participant.Stats) }
}
