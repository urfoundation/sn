//go:build linux || darwin

package validator

// Deterministic runtime controls retain genuine M8 trails, signed typed
// streams, complete replay, physical snapshots and independent restart owners.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The initializer derives the physical empty prefix and leaves no transaction
// or fictitious closed settlement behind after every snapshot is durable.
func TestAttemptSettlementRuntimeV2InitializesExactEmptyPrefix(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	for index, participant := range fixture.participants {
		snapshot := participant.Stats.snapshotStats()
		head, err := participant.Ledger.Head()
		if err != nil || snapshot.Version != 6 || snapshot.EgressGeneration != 1 || fixture.fixtures[index].expected.EgressGeneration != 1 || snapshot.AttemptV2 == nil || snapshot.AttemptV2.Activation != fixture.fixtures[index].expected.Activation || snapshot.AttemptV2.Terminal != nil || snapshot.AttemptSettlementFirstSequence != head.LastSequence+1 || snapshot.AttemptV2.SettlementPriorRoot != head.Root || participant.Stats.attemptCutPending {
			t.Fatalf("actual activation prefix differs: %+v %v", snapshot, err)
		}
		snapshot.EgressGeneration = 0
		if _, err := encodeStatsSnapshot(snapshot); err == nil {
			t.Fatal("compact activation accepted an omitted generation")
		}
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed activation retained journal: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.coordinator, "settlement-closures-v2")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("activation invented a closed epoch: %v", err)
	}
}

// A real completed trail is existing nonempty current state, not an implicit
// activation migration. Refusal preserves all durable and live raw counters.
func TestAttemptSettlementRuntimeV2ActivationRefusesNonemptySignedWindow(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	fixture.trails(t, 0, 1, 0)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err == nil {
		t.Fatal("activation discarded a real signed window")
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("refused activation changed raw state")
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused activation wrote a journal: %v", err)
	}
}

// Independent expected activation cannot be replaced by the candidate prefix.
func TestAttemptSettlementRuntimeV2ActivationRejectsMismatchedPin(t *testing.T) {
	for _, pin := range []string{"prefix", "generation"} {
		fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
		before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
		options := fixture.options(t).Authority
		operator := options.Operators[11]
		if pin == "prefix" {
			operator.Expected.Activation.FirstSequence, operator.Expected.FirstSequence, operator.Expected.EgressFirstSequence = 2, 2, 2
			operator.Expected.Activation.PriorRoot, operator.Expected.PriorRoot = attemptHex32([32]byte{99}), attemptHex32([32]byte{99})
		} else {
			operator.Expected.EgressGeneration = 2
		}
		options.Operators[11] = operator
		physical := attemptSettlementV2PhysicalIO()
		writes := 0
		physical.writeJournal = func(*attemptPrivateDirectory, string, []byte) error {
			writes++
			return errors.New("unexpected activation write")
		}
		if err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, options, runtimeAttemptSettlementV2TestPersistence(), physical); err == nil || writes != 0 {
			t.Fatalf("%s candidate-selected activation entered persistence: %v/%d", pin, err, writes)
		}
		if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
			t.Fatalf("%s mismatched activation changed the actual live image", pin)
		}
	}
}

// Fifteen real complete trails plus one failed extension guarantee positive
// quality under a_min8 while retaining the original 122-record/16-trail cap.
func TestAttemptSettlementRuntimeV2PersistsCompleteRealM8Batch(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 15, 1)
	fixture.trails(t, 1, 1, 0)
	physical := attemptSettlementV2PhysicalIO()
	var transaction *attemptSettlementTransactionV2
	physical.writeJournal = func(root *attemptPrivateDirectory, name string, raw []byte) error {
		var err error
		transaction, _, _, err = decodeAttemptSettlementTransactionV2(t.Context(), raw, fixture.participants, runtimeAttemptSettlementV2TestPersistence())
		if err != nil {
			return err
		}
		return writeAttemptSettlementV2OwnedState(root, name, raw)
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err != nil || closure == nil || transaction == nil {
		t.Fatalf("actual complete batch: %v", err)
	}
	if closure.Transitions[0].Cut.RecordCount != 122 || closure.Transitions[0].Cut.CompleteCount != 15 || closure.Transitions[0].Cut.FailedCount != 1 || closure.Transitions[1].Cut.RecordCount != 8 || len(closure.Transitions[0].PostFold) == 0 {
		t.Fatal("real complete/failed M8 census or positive quality was lost")
	}
	for _, quality := range closure.Transitions[0].PostFold {
		if !quality.HasQuality || quality.QualityPPM == 0 {
			t.Fatal("guaranteed real score was not positive")
		}
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
	read, verified, err := ReadAttemptSettlementClosureV2(t.Context(), fixture.coordinator, 42, fixture.options(t).Authority)
	if err != nil || !reflect.DeepEqual(read, closure) || len(verified.Operators) != 2 || verified.Operators[9].Replay.Records.ItemCount != 122 || verified.Operators[11].Replay.Records.ItemCount != 8 {
		t.Fatalf("independent immutable full replay differs: %v", err)
	}
}

// A mutable public result must not alias the immutable Stats-owned history.
func TestAttemptSettlementRuntimeV2OwnsRetainedTerminalSeparately(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	closure.Transitions[0].Signature[0] ^= 1
	closure.Transitions[0].PreFold.Providers[0].Assignments++
	closure.Transitions[0].Batch[0].Digest = zeroAttemptHash()
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("public closure mutated retained terminal state")
	}
}

// Actual Save after partial physical advancement differs from the older disk
// checkpoint. Recovery must accept that exact live preimage, not arbitrary data.
func TestAttemptSettlementRuntimeV2PartialWriteSaveThenActualRestart(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	fixture.trails(t, 1, 1, 0)
	transaction := fixture.partialWrite(t)
	for index, participant := range fixture.participants {
		if transaction.Snapshots[index].OriginalHash == transaction.Snapshots[index].PreImageHash {
			t.Fatal("fixture did not exercise a newer live preimage")
		}
		if err := participant.Stats.Save(participant.StateDir); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil || !bytes.Equal(data, transaction.Snapshots[index].PreImageJSON) {
			t.Fatalf("actual shutdown Save differs from recorded preimage: %v", err)
		}
	}
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, restarted, transaction)
}

// A live retry has fresh verification scratch but cannot call any typed
// writer or private signer again; it publishes byte-identical journal output.
func TestAttemptSettlementRuntimeV2RetryFinishesWithoutResealing(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	transaction := fixture.partialWrite(t)
	options := fixture.options(t)
	writes := 0
	for noID, seal := range options.Seal {
		refuse := func(context.Context, string, []byte) error { writes++; return errors.New("retry attempted to reseal") }
		seal.WriteRecords, seal.WriteProofs, seal.WriteMetadata = refuse, refuse, refuse
		options.Seal[noID] = seal
	}
	closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, options)
	if err != nil || closure == nil || writes != 0 {
		t.Fatalf("journal retry resealed or failed: %v writes=%d", err, writes)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// Removal observes a coherently advanced whole batch with admission still
// closed. A retained removal error cannot publish half the operator generation.
func TestAttemptSettlementRuntimeV2PublishesAllBeforeJournalRemoval(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	physical := attemptSettlementV2PhysicalIO()
	cause := errors.New("actual removal boundary test refusal")
	physical.removeJournal = func(*attemptPrivateDirectory, string) error {
		for _, participant := range fixture.participants {
			if participant.Stats.snapshotStats().SettlementEpoch == nil || participant.Stats.settlementEpoch != 43 || !participant.Stats.attemptCutPending || !participant.Stats.attemptSettlementCutPending {
				t.Error("removal observed partial or admitted publication")
			}
		}
		return cause
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if !errors.Is(err, cause) || closure != nil {
		t.Fatalf("removal result escaped: %v", err)
	}
	fixture.assertReserved(t, 43)
	transaction := fixture.transaction(t)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// The first real removal may complete while its final acknowledgement fails.
// A current retry replays/resyncs immutable bytes and does not refold counters.
func TestAttemptSettlementRuntimeV2RetriesAfterRemovedJournalFailure(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	physical := attemptSettlementV2PhysicalIO()
	cause := errors.New("late journal removal acknowledgement")
	physical.removeJournal = func(root *attemptPrivateDirectory, name string) error {
		return errors.Join(removeAttemptSettlementV2OwnedTransaction(root, name), cause)
	}
	if closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true); !errors.Is(err, cause) || closure != nil {
		t.Fatalf("removed journal failure: %v", err)
	}
	fixture.assertReserved(t, 43)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("real removal did not happen: %v", err)
	}
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("no-journal retry refolded or changed exact snapshots")
	}
}

// An admitted actual snapshot may complete before cancellation. Preserve the
// journal and all gates, then finish the same bytes with a fresh owner context.
func TestAttemptSettlementRuntimeV2CancellationAfterFirstSnapshotRetainsJournal(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, raw []byte) error {
		writes++
		err := writeAttemptSettlementV2OwnedState(root, name, raw)
		cancel()
		return err
	}
	if closure, err := advanceAttemptSettlementEpochV2(ctx, fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true); !errors.Is(err, context.Canceled) || closure != nil || writes != 1 {
		t.Fatalf("canceled partial snapshot: %v writes=%d", err, writes)
	}
	fixture.assertReserved(t, 43)
	transaction := fixture.transaction(t)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// Mixed cancellation and a real atomic-write failure retain both causes.
func TestAttemptSettlementRuntimeV2RetainsCanceledPhysicalWriteCause(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	physical := attemptSettlementV2PhysicalIO()
	var physicalErr error
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, raw []byte) error {
		path := filepath.Join(root.path, name)
		if err := os.Rename(path, path+".preserved"); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return err
		}
		physicalErr = atomicStateWrite(path, raw, 0o600)
		cancel()
		return physicalErr
	}
	closure, err := advanceAttemptSettlementEpochV2(ctx, fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	var renameErr *os.LinkError
	if closure != nil || !errors.Is(err, context.Canceled) || physicalErr == nil || !errors.Is(err, physicalErr) || !errors.As(physicalErr, &renameErr) {
		t.Fatalf("cancellation hid the actual snapshot failure: %v", err)
	}
	fixture.assertReserved(t, 43)
	_ = fixture.transaction(t)
}

// All reservations are visible even if the first active operator cannot yet
// drain. The active count is a real admitted Stats attempt, not a timing guess.
func TestAttemptSettlementRuntimeV2ReservesEveryOperatorBeforeDrain(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	first := fixture.participants[0]
	if err := first.Stats.beginAttempt(42, first.Ledger); err != nil {
		t.Fatal(err)
	}
	defer first.Stats.abortAttempt()
	if closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); !errors.Is(err, errAttemptCutPending) || closure != nil {
		t.Fatalf("active drain result: %v", err)
	}
	fixture.assertReserved(t, 43)
	if err := fixture.participants[1].Stats.beginAttempt(42, fixture.participants[1].Ledger); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("later operator was not reserved: %v", err)
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("undrained batch journaled: %v", err)
	}
}

// Ordinary/native ownership is not a settlement reservation to clear.
func TestAttemptSettlementRuntimeV2RefusesNativeReservationWithoutMutation(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	stats := fixture.participants[1].Stats
	owner := stats.lockStatsWrite("native-test-reservation")
	stats.mu.Lock()
	stats.attemptCutPending = true
	stats.mu.Unlock()
	owner.release()
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); !errors.Is(err, errAttemptCutPending) || closure != nil {
		t.Fatalf("native reservation result: %v", err)
	}
	if !stats.attemptCutPending || stats.attemptSettlementCutPending || fixture.participants[0].Stats.attemptCutPending || !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("settlement stole or partially published a native reservation")
	}
}

// An unchanged refresh is genuinely inert even when the native owner is held.
func TestAttemptSettlementRuntimeV2UnchangedRefreshPreservesNativeReservation(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	stats := fixture.participants[0].Stats
	owner := stats.lockStatsWrite("native-test-reservation")
	stats.mu.Lock()
	stats.attemptCutPending = true
	stats.mu.Unlock()
	owner.release()
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.fixtures[0].expected.Boundary, fixture.options(t), attemptSettlementV2PhysicalIO(), false)
	if err != nil || closure != nil || !stats.attemptCutPending || stats.attemptSettlementCutPending || !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatalf("unchanged refresh stole native ownership: %v", err)
	}
}

// Fresh startup is an explicit ownership boundary, including repeated calls.
func TestAttemptSettlementRuntimeV2RecoveryRejectsLiveAndRepeatedAttachment(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err == nil {
		t.Fatal("recovery reset live owners")
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("refused live recovery mutated state")
	}
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err == nil {
		t.Fatal("repeated recovery reattached running owners")
	}
}

// Byte-exact known images are stronger than merely accepting the same epoch.
func TestAttemptSettlementRuntimeV2RecoveryRejectsUnknownSameEpochImage(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	transaction := fixture.partialWrite(t)
	var snapshot statsSnapshot
	if err := json.Unmarshal(transaction.Snapshots[1].PreImageJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.EgressGeneration++
	changed, err := encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(transaction.Snapshots[1].StatsPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := fixture.reopen(t)
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected recovery write")
	}
	if err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err == nil || writes != 0 || !strings.Contains(err.Error(), "exact known image") {
		t.Fatalf("unrelated generation entered recovery: %v writes=%d", err, writes)
	}
	for _, participant := range restarted {
		if participant.Stats.attemptLedger != nil || participant.Stats.attemptV2 != nil {
			t.Fatal("refused recovery attached a partial owner")
		}
	}
}

// A late proof-reader close is still part of complete authority verification;
// no recovery snapshot may be written from the otherwise valid partial result.
func TestAttemptSettlementRuntimeV2RecoveryReplaysBeforeAnySnapshotWrite(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	fixture.trails(t, 1, 1, 0)
	_ = fixture.partialWrite(t)
	restarted := fixture.reopen(t)
	options := fixture.options(t).Authority
	operator := options.Operators[11]
	open := operator.Measurement.Replay.OpenData
	cause := errors.New("actual reader late close boundary")
	closes := 0
	operator.Measurement.Replay.OpenData = runtimeAttemptSettlementV2TestLateClose(open, cause, &closes)
	options.Operators[11] = operator
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected recovery write")
	}
	err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, options, runtimeAttemptSettlementV2TestPersistence(), physical)
	if !errors.Is(err, cause) || writes != 0 || closes == 0 {
		t.Fatalf("late replay close published recovery: %v writes=%d closes=%d", err, writes, closes)
	}
}
