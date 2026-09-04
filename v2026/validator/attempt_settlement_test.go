package validator

// Settlement transaction tests force the active-trail and partial-filesystem
// boundaries with explicit hooks; no scheduler timing is involved.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

func newAttemptSettlementTestParticipant(t *testing.T, noID uint64) (AttemptSettlementParticipant, *AttemptLedger) {
	t.Helper()
	stateDir := t.TempDir()
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	if err := stats.AdvanceSettlementEpoch(42, stateDir); err != nil {
		t.Fatal(err)
	}
	_, validatorKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := NewAttemptLedger(stateDir, AttemptLedgerIdentity{
		DeploymentID: "settlement-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}),
		Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: noID,
	}, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
		t.Fatal(err)
	}
	return AttemptSettlementParticipant{NoID: noID, StateDir: stateDir, Stats: stats}, ledger
}

func TestAttemptSettlementAdvanceBlocksEveryOperatorBeforeFold(t *testing.T) {
	coordinatorStateDir := t.TempDir()
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	second, secondLedger := newAttemptSettlementTestParticipant(t, 2)
	if err := second.Stats.beginAttempt(42, secondLedger); err != nil {
		t.Fatal(err)
	}
	participants := []AttemptSettlementParticipant{second, first}
	if err := AdvanceAttemptSettlementEpoch(coordinatorStateDir, 43, attemptLedgerTestBoundary(), participants); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("active settlement advance = %v", err)
	}
	if err := first.Stats.beginAttempt(42, firstLedger); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("first operator admitted a trail after global cut request: %v", err)
	}
	second.Stats.abortAttempt()
	if err := AdvanceAttemptSettlementEpoch(coordinatorStateDir, 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatal(err)
	}
	for _, participant := range participants {
		if participant.Stats.settlementEpoch != 43 || participant.Stats.attemptCutPending {
			t.Fatalf("no_id %d settlement state = %d pending=%t", participant.NoID, participant.Stats.settlementEpoch, participant.Stats.attemptCutPending)
		}
		if participant.Stats.settlementTransition == nil || len(participant.Stats.settlementTransition.Batch) != 2 {
			t.Fatalf("no_id %d omitted its cross-operator transition", participant.NoID)
		}
		if err := VerifyAttemptSettlementTransition(participant.Stats.settlementTransition); err != nil {
			t.Fatalf("no_id %d settlement transition: %v", participant.NoID, err)
		}
	}
	if !equalAttemptSettlementBatch(first.Stats.settlementTransition.Batch, second.Stats.settlementTransition.Batch) {
		t.Fatal("operators published different settlement transaction membership")
	}
	transitions := []*AttemptSettlementTransition{first.Stats.settlementTransition, second.Stats.settlementTransition}
	if err := VerifyAttemptSettlementBatch(transitions); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptSettlementBatch(transitions[:1]); err == nil {
		t.Fatal("incomplete cross-operator settlement batch unexpectedly verified")
	}
}

func TestAttemptSettlementInitializesPristineOperatorsAtomically(t *testing.T) {
	stateDir := t.TempDir()
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	_, validatorKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := NewAttemptLedger(stateDir, AttemptLedgerIdentity{
		DeploymentID: "settlement-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}),
		Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: 1,
	}, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
		t.Fatal(err)
	}
	participant := AttemptSettlementParticipant{NoID: 1, StateDir: stateDir, Stats: stats}
	if err := AdvanceAttemptSettlementEpoch(t.TempDir(), 42, AttemptBoundary{}, []AttemptSettlementParticipant{participant}); err != nil {
		t.Fatal(err)
	}
	if stats.settlementEpoch != 42 || stats.settlementTransition != nil {
		t.Fatalf("pristine settlement initialization = epoch %d transition %v", stats.settlementEpoch, stats.settlementTransition)
	}
}

func TestAttemptSettlementRejectsUnsignedLegacyStateAndAcceptsFreshNamespace(t *testing.T) {
	legacyDir := t.TempDir()
	legacy := NewStatsEngine(StatsConfig{AMin: 1})
	if err := legacy.AdvanceSettlementEpoch(42, legacyDir); err != nil {
		t.Fatal(err)
	}
	legacy.RecordAssignment(connect.NewId())
	if err := legacy.Save(legacyDir); err != nil {
		t.Fatal(err)
	}
	loaded := NewStatsEngine(StatsConfig{AMin: 1})
	if err := loaded.Load(legacyDir); err != nil {
		t.Fatal(err)
	}
	_, validatorKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity := AttemptLedgerIdentity{
		DeploymentID: "settlement-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}),
		Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: 1,
	}
	ledger, err := NewAttemptLedger(legacyDir, identity, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.AttachAttemptLedger(ledger, legacyDir); err == nil {
		t.Fatal("unsigned legacy statistics were blessed by an empty attempt ledger")
	}

	freshDir := t.TempDir()
	fresh := NewStatsEngine(StatsConfig{AMin: 1})
	freshLedger, err := NewAttemptLedger(freshDir, identity, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.AttachAttemptLedger(freshLedger, freshDir); err != nil {
		t.Fatal(err)
	}
	participant := AttemptSettlementParticipant{NoID: 1, StateDir: freshDir, Stats: fresh}
	if err := AdvanceAttemptSettlementEpoch(t.TempDir(), 42, AttemptBoundary{}, []AttemptSettlementParticipant{participant}); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptSettlementTransitionProvesTerminalCountersAndEMA(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	participant := AttemptSettlementParticipant{NoID: ledger.identity.NoID, StateDir: stateDir, Stats: stats}
	if err := AdvanceAttemptSettlementEpoch(t.TempDir(), 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{participant}); err != nil {
		t.Fatal(err)
	}
	transition := stats.settlementTransition
	if transition == nil || transition.PreFold.AttemptCut == nil || transition.PreFold.AttemptCut.LastSequence != 4 || len(transition.PostFold) != 3 {
		t.Fatalf("terminal settlement transition = %+v", transition)
	}
	measurement := stats.currentReleaseStatsMeasurement()
	if _, err := VerifyReleaseStatsMeasurement(measurement); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(transition)
	if err != nil {
		t.Fatal(err)
	}
	var mutated AttemptSettlementTransition
	if err := json.Unmarshal(encoded, &mutated); err != nil {
		t.Fatal(err)
	}
	mutated.PostFold[0].QualityPPM++
	if err := VerifyAttemptSettlementTransition(&mutated); err == nil {
		t.Fatal("mutated post-fold quality unexpectedly verified")
	}
}

func TestAttemptSettlementRecoveryCompletesPartialFilesystemCommit(t *testing.T) {
	coordinatorStateDir := t.TempDir()
	first, _ := newAttemptSettlementTestParticipant(t, 1)
	second, _ := newAttemptSettlementTestParticipant(t, 2)
	participants := []AttemptSettlementParticipant{first, second}
	writes := 0
	expected := errors.New("synthetic second snapshot failure")
	err := advanceAttemptSettlementEpochWithWrite(coordinatorStateDir, 43, attemptLedgerTestBoundary(), participants, func(path string, payload []byte) error {
		writes++
		if writes == 2 {
			return expected
		}
		return atomicStateWrite(path, payload, 0o600)
	})
	if !errors.Is(err, expected) {
		t.Fatalf("partial commit error = %v", err)
	}
	if first.Stats.settlementEpoch != 42 || second.Stats.settlementEpoch != 42 {
		t.Fatalf("partial disk failure changed in-memory epochs: %d/%d", first.Stats.settlementEpoch, second.Stats.settlementEpoch)
	}
	transactionPath := attemptSettlementTransactionPath(coordinatorStateDir)
	if _, err := os.Stat(transactionPath); err != nil {
		t.Fatalf("recovery journal missing after partial commit: %v", err)
	}
	recoveryParticipants := []AttemptSettlementParticipant{
		{NoID: 1, StateDir: first.StateDir},
		{NoID: 2, StateDir: second.StateDir},
	}
	if err := RecoverAttemptSettlementEpoch(coordinatorStateDir, recoveryParticipants); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(transactionPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery journal survived successful replay: %v", err)
	}
	for _, participant := range recoveryParticipants {
		restarted := NewStatsEngine(StatsConfig{AMin: 1})
		if err := restarted.Load(participant.StateDir); err != nil {
			t.Fatal(err)
		}
		if restarted.settlementEpoch != 43 {
			t.Fatalf("recovered no_id %d epoch = %d, want 43", participant.NoID, restarted.settlementEpoch)
		}
		for _, provider := range restarted.currentReleaseStatsMeasurement().Providers {
			if provider.Assignments != 0 || provider.Confirmations != 0 {
				t.Fatalf("no_id %d recovered an unfolded window", participant.NoID)
			}
		}
	}
	if filepath.Dir(transactionPath) != coordinatorStateDir {
		t.Fatal("settlement transaction escaped coordinator state")
	}
}

func TestAttemptSettlementRemovalFailureRetainsRecoverableJournal(t *testing.T) {
	coordinatorStateDir := t.TempDir()
	first, _ := newAttemptSettlementTestParticipant(t, 1)
	second, _ := newAttemptSettlementTestParticipant(t, 2)
	participants := []AttemptSettlementParticipant{first, second}
	expected := errors.New("synthetic settlement journal removal failure")
	removeCalls := 0
	err := advanceAttemptSettlementEpochWithIO(coordinatorStateDir, 43, attemptLedgerTestBoundary(), participants, func(path string, payload []byte) error {
		return atomicStateWrite(path, payload, 0o600)
	}, func(string) error {
		removeCalls++
		return expected
	})
	if !errors.Is(err, expected) || removeCalls != 1 {
		t.Fatalf("settlement removal result = error %v calls %d", err, removeCalls)
	}
	transactionPath := attemptSettlementTransactionPath(coordinatorStateDir)
	if info, err := os.Lstat(transactionPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("failed removal did not retain recovery journal: info %v error %v", info, err)
	}
	for _, participant := range participants {
		if participant.Stats.settlementEpoch != 43 || !participant.Stats.attemptCutPending {
			t.Fatalf("no_id %d removal failure state = epoch %d cut_pending %t", participant.NoID, participant.Stats.settlementEpoch, participant.Stats.attemptCutPending)
		}
	}

	recoveryParticipants := []AttemptSettlementParticipant{
		{NoID: 1, StateDir: first.StateDir},
		{NoID: 2, StateDir: second.StateDir},
	}
	if err := RecoverAttemptSettlementEpoch(coordinatorStateDir, recoveryParticipants); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(transactionPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replayed removal left settlement journal: %v", err)
	}
	for _, participant := range recoveryParticipants {
		restarted := NewStatsEngine(StatsConfig{AMin: 1})
		if err := restarted.Load(participant.StateDir); err != nil {
			t.Fatal(err)
		}
		if restarted.settlementEpoch != 43 {
			t.Fatalf("recovered no_id %d epoch = %d, want 43", participant.NoID, restarted.settlementEpoch)
		}
	}
}

func TestAttemptSettlementRetryFinishesJournalAfterRemovalFailure(t *testing.T) {
	coordinatorStateDir := t.TempDir()
	participant, _ := newAttemptSettlementTestParticipant(t, 1)
	participants := []AttemptSettlementParticipant{participant}
	expected := errors.New("synthetic settlement journal removal failure")
	err := advanceAttemptSettlementEpochWithIO(coordinatorStateDir, 43, attemptLedgerTestBoundary(), participants, func(path string, payload []byte) error {
		return atomicStateWrite(path, payload, 0o600)
	}, func(string) error {
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("settlement removal error = %v", err)
	}
	if err := AdvanceAttemptSettlementEpoch(coordinatorStateDir, 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatalf("same-process settlement retry: %v", err)
	}
	if participant.Stats.settlementEpoch != 43 || participant.Stats.attemptCutPending {
		t.Fatalf("retried settlement state = epoch %d cut_pending %t", participant.Stats.settlementEpoch, participant.Stats.attemptCutPending)
	}
	if _, err := os.Stat(attemptSettlementTransactionPath(coordinatorStateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retried settlement retained journal: %v", err)
	}
}

func TestAttemptSettlementRecoveryRetriesJournalRemovalFailure(t *testing.T) {
	coordinatorStateDir := t.TempDir()
	first, _ := newAttemptSettlementTestParticipant(t, 1)
	second, _ := newAttemptSettlementTestParticipant(t, 2)
	participants := []AttemptSettlementParticipant{first, second}
	writes := 0
	expectedWrite := errors.New("synthetic second snapshot failure")
	err := advanceAttemptSettlementEpochWithWrite(coordinatorStateDir, 43, attemptLedgerTestBoundary(), participants, func(path string, payload []byte) error {
		writes++
		if writes == 2 {
			return expectedWrite
		}
		return atomicStateWrite(path, payload, 0o600)
	})
	if !errors.Is(err, expectedWrite) {
		t.Fatalf("partial settlement error = %v", err)
	}
	recoveryParticipants := []AttemptSettlementParticipant{
		{NoID: 1, StateDir: first.StateDir},
		{NoID: 2, StateDir: second.StateDir},
	}
	expectedRemove := errors.New("synthetic recovery journal removal failure")
	removeCalls := 0
	err = recoverAttemptSettlementEpochWithRemove(coordinatorStateDir, recoveryParticipants, func(string) error {
		removeCalls++
		return expectedRemove
	})
	if !errors.Is(err, expectedRemove) || removeCalls != 1 {
		t.Fatalf("recovery removal result = error %v calls %d", err, removeCalls)
	}
	transactionPath := attemptSettlementTransactionPath(coordinatorStateDir)
	if _, err := os.Stat(transactionPath); err != nil {
		t.Fatalf("failed recovery removal lost journal: %v", err)
	}
	if err := RecoverAttemptSettlementEpoch(coordinatorStateDir, recoveryParticipants); err != nil {
		t.Fatalf("retried settlement recovery: %v", err)
	}
	if _, err := os.Stat(transactionPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retried recovery retained journal: %v", err)
	}
}

func TestAttemptSettlementRotatesEgressWithItsSignedCursor(t *testing.T) {
	coordinatorStateDir := t.TempDir()
	participant, ledger := newAttemptSettlementTestParticipant(t, 1)
	priorGeneration := participant.Stats.egressGeneration

	if err := AdvanceAttemptSettlementEpoch(coordinatorStateDir, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{participant}); err != nil {
		t.Fatal(err)
	}
	if len(participant.Stats.EgressIpHashes()) != 0 {
		t.Fatal("settlement advance retained egress outside the new signed cursor")
	}
	if participant.Stats.egressGeneration != priorGeneration+1 || participant.Stats.attemptEgressFirstSequence != ledger.LastSequence()+1 {
		t.Fatalf("egress generation/cursor = %d/%d, want %d/%d", participant.Stats.egressGeneration, participant.Stats.attemptEgressFirstSequence, priorGeneration+1, ledger.LastSequence()+1)
	}

	restarted := NewStatsEngine(StatsConfig{AMin: 1})
	if err := restarted.Load(participant.StateDir); err != nil {
		t.Fatal(err)
	}
	if len(restarted.EgressIpHashes()) != 0 || restarted.egressGeneration != priorGeneration+1 || restarted.attemptEgressFirstSequence != ledger.LastSequence()+1 {
		t.Fatalf("persisted egress rotation = hashes=%v generation=%d cursor=%d", restarted.EgressIpHashes(), restarted.egressGeneration, restarted.attemptEgressFirstSequence)
	}
}
