//go:build linux || darwin

package validator

// The explicit compatibility path closes a real complete v1 batch first,
// migrates those exact signed bytes through NewDiskAttemptLedger, and only then
// activates an empty v2 window. No prior EMA is pasted or manufactured.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// Both versions use the same genuine M8 records and scoring config. The
// legacy materializing API is exercised only on its actual legacy backend.
func newAttemptSettlementRuntimeV2LegacyHistoryFixture(t *testing.T, positive bool) *attemptSettlementRuntimeV2TestFixture {
	t.Helper()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	completed := 2
	if positive {
		completed = 15
	}
	fixture.trails(t, 0, completed, 1)
	fixture.trails(t, 1, 1, 0)
	legacy := make([]AttemptSettlementParticipant, len(fixture.participants))
	legacyLedgers := make([]*AttemptLedger, len(fixture.participants))
	nativeHeads := make([]AttemptLedgerHead, len(fixture.participants))
	for index, participant := range fixture.participants {
		operator := fixture.fixtures[index]
		head, err := participant.Ledger.Head()
		if err != nil {
			t.Fatal(err)
		}
		nativeHeads[index] = head
		var records []AttemptRecord
		if err := participant.Ledger.Walk(t.Context(), 1, head.LastSequence, func(record AttemptRecord) error { records = append(records, record); return nil }); err != nil {
			t.Fatal(err)
		}
		image, err := encodeStatsSnapshot(participant.Stats.snapshotStats())
		if err != nil {
			t.Fatal(err)
		}
		if err := participant.Ledger.Close(); err != nil {
			t.Fatal(err)
		}
		dir := newAttemptSettlementRuntimeV2TestStateDir(t)
		if err := os.WriteFile(filepath.Join(dir, "attempt-ledger.jsonl"), attemptLedgerDiskTestJSONL(t, records), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stats.json"), image, 0o600); err != nil {
			t.Fatal(err)
		}
		ledger, err := NewAttemptLedger(dir, operator.expected.Identity, operator.key)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ledger.Close() })
		stats := NewStatsEngine(participant.Stats.cfg)
		if err := stats.Load(dir); err != nil {
			t.Fatal(err)
		}
		if err := stats.AttachAttemptLedger(ledger, dir); err != nil {
			t.Fatal(err)
		}
		legacy[index] = AttemptSettlementParticipant{NoID: participant.NoID, StateDir: dir, Stats: stats}
		legacyLedgers[index] = ledger
	}
	if err := AdvanceAttemptSettlementEpoch(fixture.coordinator, 43, fixture.fixtures[0].expected.Boundary, legacy); err != nil {
		t.Fatalf("actual complete v1 close: %v", err)
	}
	for index, participant := range legacy {
		operator := fixture.fixtures[index]
		head, err := legacyLedgers[index].Head()
		if err != nil {
			t.Fatal(err)
		}
		if participant.Stats.settlementTransition == nil || participant.Stats.settlementTransition.PreFold.AttemptCut.LastSequence != head.LastSequence || len(participant.Stats.window) != 0 || len(participant.Stats.egress) != 0 {
			t.Fatal("real legacy close did not empty its exact signed window")
		}
		// Legacy Head reports only the signed prefix. Keep the original native
		// counters for the native-to-legacy-to-native round trip below.
		if head.LastSequence != nativeHeads[index].LastSequence || head.Root != nativeHeads[index].Root {
			t.Fatal("real legacy close changed the original signed native prefix")
		}
		if err := legacyLedgers[index].Close(); err != nil {
			t.Fatal(err)
		}
		disk, err := NewDiskAttemptLedger(t.Context(), participant.StateDir, operator.expected.Identity, attemptLedgerDiskTestCoordinator, operator.key, attemptLedgerDiskTestLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = disk.Close() })
		imported, err := disk.Head()
		if err != nil || imported != nativeHeads[index] {
			t.Fatalf("actual legacy migration changed signed prefix: %v", err)
		}
		stats := NewStatsEngine(participant.Stats.cfg)
		if err := stats.Load(participant.StateDir); err != nil {
			t.Fatal(err)
		}
		if err := stats.AttachAttemptLedger(disk, participant.StateDir); err != nil {
			t.Fatal(err)
		}
		store, err := NewProofStore(participant.StateDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.ReconcileAttemptProofsContext(t.Context(), disk); err != nil {
			t.Fatal(err)
		}
		expected := operator.expected
		expected.Activation.Domain.ActivationEpoch, expected.Activation.Domain.ActivationHash = 43, [32]byte{0x33}
		expected.Activation.FirstSequence, expected.Activation.PriorRoot = head.LastSequence+1, head.Root
		expected.FirstSequence, expected.EgressFirstSequence, expected.PriorRoot = head.LastSequence+1, head.LastSequence+1, head.Root
		expected.EgressGeneration = stats.egressGeneration
		expected.Boundary = AttemptBoundary{SettlementEpoch: 43, EVMBlock: operator.expected.Boundary.EVMBlock + 1, EVMBlockHash: attemptHex32([32]byte{43, 77})}
		config := operator.engine.cfg
		config.AttemptLedger = disk
		config.AttemptBoundaryResolver = func(_ context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			if pinned != nil && *pinned != expected.Boundary {
				return AttemptBoundary{}, nil, errors.New("legacy migration fixture changed finalized pin")
			}
			bindings := make([]AttemptBinding, len(ids))
			for index, id := range ids {
				bindings[index] = attemptLedgerTestBinding(id, 1)
			}
			return expected.Boundary, bindings, nil
		}
		operator.engine = NewTrailEngine(operator.engine.clientId, operator.key, operator.server, NewStaticServerKeyRing(operator.server.serverPublicKeys()), operator.engine.pickSeed, stats, store, func() uint64 { return 43 }, config)
		operator.ledger, operator.expected = disk, expected
		fixture.participants[index] = AttemptSettlementRuntimeV2Participant{NoID: participant.NoID, StateDir: participant.StateDir, Stats: stats, Ledger: disk}
	}
	return fixture
}

// Real positive exact prior is authenticated in a complete legacy batch and
// preserved byte-for-byte except for the explicit v6 activation wrapper.
func TestAttemptSettlementRuntimeV2ActivationPreservesRealLegacyPositiveHistory(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2LegacyHistoryFixture(t, true)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if len(fixture.participants[0].Stats.emaPPM) == 0 {
		t.Fatal("real a_min8 legacy close did not establish positive prior")
	}
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	for index, participant := range fixture.participants {
		snapshot := participant.Stats.snapshotStats()
		if snapshot.Version != 6 || snapshot.AttemptV2 == nil || snapshot.SettlementTransition == nil {
			t.Fatal("activation omitted the actual carried legacy history")
		}
		snapshot.Version, snapshot.AttemptV2 = 5, nil
		legacy, err := encodeStatsSnapshot(snapshot)
		if err != nil || !bytes.Equal(legacy, before[index]) {
			t.Fatalf("activation rewrote v1 history or manufactured prior: %v", err)
		}
	}
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeAttemptSettlementV2TestImages(t, fixture.participants), runtimeAttemptSettlementV2TestImages(t, restarted)) {
		t.Fatal("restart changed genuine legacy-derived prior")
	}
}

// A compact nonempty current window after genuine signed legacy history is
// sealed normally; the old transition bytes remain nonrecursive carried data.
func TestAttemptSettlementRuntimeV2TerminalPreservesLegacyHistoryWithRealNewRows(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2LegacyHistoryFixture(t, false)
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	legacy := make([][]byte, len(fixture.participants))
	for index, participant := range fixture.participants {
		var err error
		legacy[index], err = json.Marshal(participant.Stats.settlementTransition)
		if err != nil {
			t.Fatal(err)
		}
	}
	fixture.trails(t, 0, 1, 0)
	fixture.trails(t, 1, 1, 0)
	closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 44, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	for index, participant := range fixture.participants {
		if closure.Transitions[index].Cut.RecordCount != 8 || closure.Transitions[index].PreFold.SettlementTransition != nil || closure.Transitions[index].PreFold.AttemptCut != nil {
			t.Fatal("compact terminal did not use exactly the actual new M8 window")
		}
		retained, err := json.Marshal(participant.Stats.settlementTransition)
		if err != nil || !bytes.Equal(retained, legacy[index]) {
			t.Fatalf("compact terminal rewrote original legacy bytes: %v", err)
		}
	}
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
}

// A mutated legacy signature cannot become authority merely by being stored
// alongside a locally well-shaped v6 activation marker.
func TestAttemptSettlementRuntimeV2ActivationRejectsMutatedLegacyHistory(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2LegacyHistoryFixture(t, false)
	participant := fixture.participants[1]
	owner := participant.Stats.lockStatsWrite("legacy-history-refusal-input")
	participant.Stats.mu.Lock()
	encoded, err := json.Marshal(participant.Stats.settlementTransition)
	var changed AttemptSettlementTransition
	if err == nil {
		err = json.Unmarshal(encoded, &changed)
	}
	if err == nil && len(changed.Signature) == 0 {
		err = errors.New("real legacy terminal signature prerequisite is missing")
	}
	if err == nil {
		changed.Signature[0] ^= 1
		participant.Stats.settlementTransition = &changed
	}
	participant.Stats.mu.Unlock()
	owner.release()
	if err != nil {
		t.Fatal(err)
	}
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeJournal = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected legacy mutation journal")
	}
	if err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err == nil || writes != 0 {
		t.Fatalf("mutated signed legacy history entered activation: %v/%d", err, writes)
	}
}
