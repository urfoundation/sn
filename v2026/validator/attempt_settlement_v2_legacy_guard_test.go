//go:build linux || darwin

package validator

// Legacy entrypoints cannot clear compact ownership or replay a v1 journal
// over v6 files. All admitted control counters come from genuine M8 trails.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// One-member activation is an independently declared complete operator set,
// not a v6 marker pasted into the earlier operator's live candidate.
func activateAttemptSettlementRuntimeV2TestSecond(t *testing.T, fixture *attemptSettlementRuntimeV2TestFixture) {
	t.Helper()
	authority := fixture.options(t).Authority
	delete(authority.Operators, fixture.participants[0].NoID)
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants[1:], 42, authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
}

// Produces canonical v1 journal bytes from actual owner snapshots. A newer
// live first image differs from its older disk checkpoint in refusal controls.
func legacyAttemptSettlementRuntimeV2TestJournal(t *testing.T, fixture *attemptSettlementRuntimeV2TestFixture, participants []AttemptSettlementRuntimeV2Participant) (*attemptSettlementTransaction, []AttemptSettlementParticipant, []byte) {
	t.Helper()
	transaction := &attemptSettlementTransaction{Schema: attemptSettlementTransactionSchema, Epoch: 42}
	legacy := make([]AttemptSettlementParticipant, len(participants))
	for index, participant := range participants {
		encoded, err := encodeStatsSnapshot(participant.Stats.snapshotStats())
		if err != nil {
			t.Fatal(err)
		}
		transaction.Snapshots = append(transaction.Snapshots, attemptSettlementSnapshot{NoID: participant.NoID, StatsPath: filepath.Join(participant.StateDir, "stats.json"), StatsJSON: encoded})
		legacy[index] = AttemptSettlementParticipant{NoID: participant.NoID, StateDir: participant.StateDir}
	}
	encoded, err := canonicalAttemptSettlementTransaction(transaction)
	if err != nil {
		t.Fatal(err)
	}
	return transaction, legacy, encoded
}

// Journal injection is explicit after setup activation; activation itself may
// not silently pass an already-existing legacy transaction.
func publishLegacyAttemptSettlementRuntimeV2TestJournal(t *testing.T, fixture *attemptSettlementRuntimeV2TestFixture, encoded []byte) {
	t.Helper()
	if err := atomicStateWrite(attemptSettlementTransactionPath(fixture.coordinator), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Root and every target byte/hash must remain unchanged on refusal, even when
// the first v1 target would otherwise be physically replaced before the last.
func assertLegacyAttemptSettlementRuntimeV2TestUnchanged(t *testing.T, coordinator string, transaction *attemptSettlementTransaction, journal []byte, before [][]byte) {
	t.Helper()
	actual, err := os.ReadFile(attemptSettlementTransactionPath(coordinator))
	if err != nil || !bytes.Equal(actual, journal) || attemptSettlementV2ImageHash(actual) != attemptSettlementV2ImageHash(journal) {
		t.Fatalf("refusal changed original journal: %v", err)
	}
	for index, snapshot := range transaction.Snapshots {
		actual, err := os.ReadFile(snapshot.StatsPath)
		if err != nil || !bytes.Equal(actual, before[index]) || attemptSettlementV2ImageHash(actual) != attemptSettlementV2ImageHash(before[index]) {
			t.Fatalf("refusal changed no_id %d target: %v", snapshot.NoID, err)
		}
	}
}

// The legacy free batch API must refuse before its all-current finish branch
// can clear either a v6 gate or an earlier v1 participant's native reservation.
func TestAttemptSettlementRuntimeV2LegacyBatchCurrentCannotClearOwnership(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		fixture := newAttemptSettlementRuntimeV2TestFixture(t, !mixed)
		if mixed {
			activateAttemptSettlementRuntimeV2TestSecond(t, fixture)
		}
		legacy := make([]AttemptSettlementParticipant, len(fixture.participants))
		for index, participant := range fixture.participants {
			legacy[index] = AttemptSettlementParticipant{NoID: participant.NoID, StateDir: participant.StateDir, Stats: participant.Stats}
			owner := participant.Stats.lockStatsWrite("legacy-guard-reservation")
			participant.Stats.mu.Lock()
			participant.Stats.attemptCutPending = true
			participant.Stats.mu.Unlock()
			owner.release()
		}
		before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
		if err := AdvanceAttemptSettlementEpoch(fixture.coordinator, 42, fixture.fixtures[0].expected.Boundary, legacy); err == nil || !strings.Contains(err.Error(), "v2 settlement coordinator") {
			t.Fatalf("mixed=%t legacy current bypass: %v", mixed, err)
		}
		if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
			t.Fatal("legacy current changed exact cursor/epoch/EMA/history")
		}
		for _, participant := range fixture.participants {
			if !participant.Stats.attemptCutPending || participant.Stats.attemptSettlementCutPending {
				t.Fatal("legacy current changed native reservation")
			}
		}
		if _, err := os.Lstat(attemptSettlementTransactionPath(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy current created journal: %v", err)
		}
	}
}

// A later v6 participant is admitted before any earlier v1 reservation/fold.
func TestAttemptSettlementRuntimeV2LegacyBatchAdvanceCannotPartiallyMutate(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		fixture := newAttemptSettlementRuntimeV2TestFixture(t, !mixed)
		if mixed {
			activateAttemptSettlementRuntimeV2TestSecond(t, fixture)
		}
		fixture.trails(t, 0, 1, 0)
		legacy := make([]AttemptSettlementParticipant, len(fixture.participants))
		for index, participant := range fixture.participants {
			legacy[index] = AttemptSettlementParticipant{NoID: participant.NoID, StateDir: participant.StateDir, Stats: participant.Stats}
		}
		before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
		if err := AdvanceAttemptSettlementEpoch(fixture.coordinator, 43, fixture.fixtures[0].expected.Boundary, legacy); err == nil || !strings.Contains(err.Error(), "v2 settlement coordinator") {
			t.Fatalf("mixed=%t legacy advance bypass: %v", mixed, err)
		}
		if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
			t.Fatal("legacy advance changed exact cursor/epoch/EMA/history")
		}
		for _, participant := range fixture.participants {
			if participant.Stats.attemptCutPending || participant.Stats.attemptSettlementCutPending {
				t.Fatal("legacy advance partially reserved participants")
			}
		}
	}
}

// The adjacent per-engine v1 API also refuses both same-epoch and next-epoch
// calls without resetting v6 state or touching its durable checkpoint.
func TestAttemptSettlementRuntimeV2LegacyPerEngineAdvanceRefusesDowngrade(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	for _, epoch := range []uint64{42, 43} {
		participant := fixture.participants[0]
		if err := participant.Stats.AdvanceSettlementEpoch(epoch, participant.StateDir); err == nil {
			t.Fatalf("legacy engine accepted epoch%d", epoch)
		}
		if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
			t.Fatal("legacy engine mutated compact state")
		}
		disk, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil || !bytes.Equal(disk, before[0]) {
			t.Fatalf("legacy engine changed v6 disk: %v", err)
		}
	}
}

// The earlier v1 target has a genuinely different valid journal postimage.
// A later actual v6 target must stop the entire recovery before that write.
func TestAttemptSettlementRuntimeV2LegacyRecoveryMixedTargetsStayExact(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	fixture.trails(t, 0, 1, 0)
	transaction, participants, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, fixture.participants)
	activateAttemptSettlementRuntimeV2TestSecond(t, fixture)
	publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
	before := make([][]byte, len(participants))
	for index, snapshot := range transaction.Snapshots {
		var err error
		before[index], err = os.ReadFile(snapshot.StatsPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	if bytes.Equal(before[0], transaction.Snapshots[0].StatsJSON) {
		t.Fatal("earlier v1 target would not prove a partial write")
	}
	if err := RecoverAttemptSettlementEpoch(fixture.coordinator, participants); err == nil {
		t.Fatal("v1 journal overwrote a later v6 target")
	}
	assertLegacyAttemptSettlementRuntimeV2TestUnchanged(t, fixture.coordinator, transaction, journal, before)
}

// A single existing v6 target is equally ineligible for legacy repair.
func TestAttemptSettlementRuntimeV2LegacyRecoverySingleTargetStaysExact(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	transaction, participants, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, fixture.participants[1:])
	activateAttemptSettlementRuntimeV2TestSecond(t, fixture)
	publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
	before, err := os.ReadFile(transaction.Snapshots[0].StatsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecoverAttemptSettlementEpoch(fixture.coordinator, participants); err == nil {
		t.Fatal("single v6 target was overwritten by v1 recovery")
	}
	assertLegacyAttemptSettlementRuntimeV2TestUnchanged(t, fixture.coordinator, transaction, journal, [][]byte{before})
}

// A v6 postimage is invalid inside the old journal even when every actual
// target is v1. This exercises decoder admission, not target-file refusal.
func TestAttemptSettlementRuntimeV2LegacyRecoveryRejectsV6JournalInjection(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	fixture.trails(t, 0, 1, 0)
	original := make([][]byte, len(fixture.participants))
	for index, participant := range fixture.participants {
		var err error
		original[index], err = os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	activateAttemptSettlementRuntimeV2TestSecond(t, fixture)
	transaction, participants, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, fixture.participants)
	publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
	if err := atomicStateWrite(transaction.Snapshots[1].StatsPath, original[1], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverAttemptSettlementEpoch(fixture.coordinator, participants); err == nil || !strings.Contains(err.Error(), "legacy settlement snapshot") {
		t.Fatalf("v6 journal admission was bypassed: %v", err)
	}
	assertLegacyAttemptSettlementRuntimeV2TestUnchanged(t, fixture.coordinator, transaction, journal, original)
}

// Invalid JSON, unknown versions and nonregular targets are all occupied
// refusals. None may be classified as a missing recoverable legacy target.
func TestAttemptSettlementRuntimeV2LegacyRecoveryRejectsUnknownTargets(t *testing.T) {
	for _, variant := range []string{"invalid-json", "unknown-version", "fifo", "symlink"} {
		fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
		fixture.trails(t, 0, 1, 0)
		transaction, participants, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, fixture.participants)
		publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
		first, err := os.ReadFile(transaction.Snapshots[0].StatsPath)
		if err != nil {
			t.Fatal(err)
		}
		path := transaction.Snapshots[1].StatsPath
		if err := os.Rename(path, path+".preserved"); err != nil {
			t.Fatal(err)
		}
		switch variant {
		case "invalid-json":
			err = os.WriteFile(path, []byte("{"), 0o600)
		case "unknown-version":
			err = os.WriteFile(path, []byte("{\"v\":999}\n"), 0o600)
		case "fifo":
			err = unix.Mkfifo(path, 0o600)
		case "symlink":
			err = os.Symlink("stats.json.preserved", path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := RecoverAttemptSettlementEpoch(fixture.coordinator, participants); err == nil {
			t.Fatalf("%s occupied target was repaired as absent", variant)
		}
		actual, err := os.ReadFile(transaction.Snapshots[0].StatsPath)
		if err != nil || !bytes.Equal(actual, first) {
			t.Fatalf("%s wrote earlier valid target: %v", variant, err)
		}
		actual, err = os.ReadFile(attemptSettlementTransactionPath(fixture.coordinator))
		if err != nil || !bytes.Equal(actual, journal) {
			t.Fatalf("%s consumed original journal: %v", variant, err)
		}
	}
}

// The new guard preserves all-v1 recovery's exact serialized output.
func TestAttemptSettlementRuntimeV2LegacyRecoveryKeepsValidV1Behavior(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	fixture.trails(t, 0, 1, 0)
	fixture.trails(t, 1, 1, 0)
	transaction, participants, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, fixture.participants)
	publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
	if err := RecoverAttemptSettlementEpoch(fixture.coordinator, participants); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range transaction.Snapshots {
		actual, err := os.ReadFile(snapshot.StatsPath)
		if err != nil || !bytes.Equal(actual, snapshot.StatsJSON) {
			t.Fatalf("valid v1 recovery output changed: %v", err)
		}
	}
	if _, err := os.Lstat(attemptSettlementTransactionPath(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("valid v1 recovery retained journal: %v", err)
	}
}
