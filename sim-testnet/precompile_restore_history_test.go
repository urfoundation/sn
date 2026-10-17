// Synthetic native state and phase receipts reproduce restore replay after a
// completed renewal without requiring a node or allowing current-state drift.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Records which native view the production restore verifier actually reads.
type precompileRestoreReaderFixture struct {
	hotkey          [32]byte
	blockHash       types.Hash
	historical      *crv4.FinalizedCommitment
	current         *crv4.FinalizedCommitment
	historicalErr   error
	currentErr      error
	historicalReads int
	currentReads    int
}

// The historical reader refuses any substituted hotkey or checkpoint.
func (self *precompileRestoreReaderFixture) fleetCommitmentAt(hotkey [32]byte, blockHash types.Hash) (*crv4.FinalizedCommitment, error) {
	self.historicalReads++
	if hotkey != self.hotkey || blockHash != self.blockHash {
		return nil, errors.New("synthetic restore request changed its hotkey or checkpoint")
	}
	return self.historical, self.historicalErr
}

// The current value models a later finalized fleet renewal.
func (self *precompileRestoreReaderFixture) fleetCommitmentFinalized(hotkey [32]byte) (*crv4.FinalizedCommitment, error) {
	self.currentReads++
	if hotkey != self.hotkey {
		return nil, errors.New("synthetic current restore request changed its hotkey")
	}
	return self.current, self.currentErr
}

// Uses a restored generation-two hash and a distinct successor at a later head.
func newPrecompileRestoreReaderFixture() (*precompileRestoreReaderFixture, PrecompileCommitmentEvidence) {
	blockHash := types.Hash{4}
	commitment := PrecompileCommitmentEvidence{
		CanonicalHash: common.Hash{2}.Hex(), CanonicalGeneration: precompileCanonicalFleetGeneration,
		WriteCommitmentBlock: 90, RestoreCommitmentBlock: 100,
		RestoreFinalizedHead: ChainHead{Number: 100, Hash: blockHash.Hex()}, Restored: true,
	}
	reader := &precompileRestoreReaderFixture{
		hotkey: [32]byte{1}, blockHash: blockHash,
		historical: &crv4.FinalizedCommitment{Hash: [32]byte{2}, CommitmentBlock: 100, FinalizedAt: 100, FinalizedHash: blockHash},
		current:    &crv4.FinalizedCommitment{Hash: [32]byte{3}, CommitmentBlock: 200, FinalizedAt: 201, FinalizedHash: types.Hash{5}},
	}
	return reader, commitment
}

// A valid historical restore survives the intentionally different successor.
func TestPrecompileRestoreHistoryReadsOriginalNativeWrite(t *testing.T) {
	reader, commitment := newPrecompileRestoreReaderFixture()
	if err := verifyPrecompileRestoreState(reader, reader.hotkey, commitment, false); err == nil || !strings.Contains(err.Error(), "not current finalized state") {
		t.Fatalf("ordinary restore accepted its successor: %v", err)
	}
	if err := verifyPrecompileRestoreState(reader, reader.hotkey, commitment, true); err != nil {
		t.Fatalf("historical restore rejected its exact native write after renewal: %v", err)
	}
	if reader.currentReads != 1 || reader.historicalReads != 1 {
		t.Fatalf("restore readers current=%d historical=%d, want 1/1", reader.currentReads, reader.historicalReads)
	}
}

// Historical hash, write block, header number, and header hash remain exact.
func TestPrecompileRestoreHistoryRejectsChangedNativeProof(t *testing.T) {
	for _, change := range []string{"hash", "write block", "header number", "header hash", "missing", "read error"} {
		reader, commitment := newPrecompileRestoreReaderFixture()
		switch change {
		case "hash":
			reader.historical.Hash = [32]byte{9}
		case "write block":
			reader.historical.CommitmentBlock++
		case "header number":
			reader.historical.FinalizedAt++
		case "header hash":
			reader.historical.FinalizedHash = types.Hash{9}
		case "missing":
			reader.historical = nil
		case "read error":
			reader.historicalErr = errors.New("synthetic historical storage refusal")
		}
		if err := verifyPrecompileRestoreState(reader, reader.hotkey, commitment, true); err == nil {
			t.Fatalf("changed %s was accepted", change)
		}
		if reader.currentReads != 0 || reader.historicalReads != 1 {
			t.Fatalf("changed %s changed reader selection: %d/%d", change, reader.currentReads, reader.historicalReads)
		}
	}
}

// Malformed or foreign anchors fail before any state read.
func TestPrecompileRestoreHistoryRejectsChangedRestoreAnchor(t *testing.T) {
	for _, change := range []string{"generation", "commitment hash", "zero header", "malformed header hash", "zero header hash", "write block"} {
		reader, commitment := newPrecompileRestoreReaderFixture()
		switch change {
		case "generation":
			commitment.CanonicalGeneration++
		case "commitment hash":
			commitment.CanonicalHash = "invalid"
		case "zero header":
			commitment.RestoreFinalizedHead.Number = 0
		case "malformed header hash":
			commitment.RestoreFinalizedHead.Hash = "invalid"
		case "zero header hash":
			commitment.RestoreFinalizedHead.Hash = common.Hash{}.Hex()
		case "write block":
			commitment.RestoreCommitmentBlock++
		}
		if err := verifyPrecompileRestoreState(reader, reader.hotkey, commitment, true); err == nil {
			t.Fatalf("changed %s was accepted", change)
		}
		if reader.currentReads != 0 || reader.historicalReads != 0 {
			t.Fatalf("changed %s reached storage: %d/%d", change, reader.currentReads, reader.historicalReads)
		}
	}
}

// Historical mode cannot weaken an ordinary restore's current-value check.
func TestPrecompileRestoreHistoryKeepsOrdinaryCurrentRequirement(t *testing.T) {
	reader, commitment := newPrecompileRestoreReaderFixture()
	reader.current = &crv4.FinalizedCommitment{Hash: [32]byte{2}, CommitmentBlock: 100, FinalizedAt: 200, FinalizedHash: types.Hash{5}}
	reader.historical = nil
	if err := verifyPrecompileRestoreState(reader, reader.hotkey, commitment, false); err != nil {
		t.Fatalf("exact current restore was rejected: %v", err)
	}
	reader.current.CommitmentBlock++
	if err := verifyPrecompileRestoreState(reader, reader.hotkey, commitment, false); err == nil {
		t.Fatal("changed current write block was accepted")
	}
	reader.current = nil
	if err := verifyPrecompileRestoreState(reader, reader.hotkey, commitment, false); err == nil {
		t.Fatal("missing current state was accepted")
	}
	if reader.currentReads != 3 || reader.historicalReads != 0 {
		t.Fatalf("ordinary restore consulted history: %d/%d", reader.currentReads, reader.historicalReads)
	}
}

// Constructs the original restore snapshot independently of phase projection.
func newPrecompileRestorePhaseFixture(t *testing.T) (Action, *PrecompileConformanceEvidence, *ActionPostcondition) {
	t.Helper()
	action := testFleetSupersessionAction(t, Action{
		ID: "precompile.commitment-restore", Kind: "substrate-extrinsic", Target: "head-fleet:1",
		Parameters: map[string]string{"canonical_generation": "2", fleetCommitmentStorageParameter: fleetCommitmentStorageV2},
	})
	complete := completePrecompileEvidence()
	complete.ProbeAddress = common.Address{7}.Hex()
	original := &PrecompileConformanceEvidence{ProbeAddress: complete.ProbeAddress, Commitment: complete.Commitment}
	var err error
	original.EvidenceHash, err = canonicalHashHex(original)
	if err != nil {
		t.Fatal(err)
	}
	complete.EvidenceHash, err = canonicalHashHex(complete)
	if err != nil {
		t.Fatal(err)
	}
	record := &ActionPostcondition{Observed: map[string]any{
		"kind": action.Kind, "target": action.Target, "probe": original.ProbeAddress,
		"evidence_hash": original.EvidenceHash, "complete": false, "canonical_chain_evidence": true,
	}}
	return action, complete, record
}

// Advancing all later phases preserves the original restore receipt verbatim.
func TestPrecompileRestoreHistoryPreservesRestorePhaseAfterLaterSteps(t *testing.T) {
	action, evidence, record := newPrecompileRestorePhaseFixture(t)
	before, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.EvidenceHash == record.Observed["evidence_hash"] || !evidence.Complete {
		t.Fatal("fixture did not advance beyond the restore phase")
	}
	state, err := precompileRestoreHistoricalObservation(action, evidence, record)
	if err != nil {
		t.Fatalf("later phases invalidated the original restore receipt: %v", err)
	}
	if err := observedPostconditionMatches(record.Observed, state); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(evidence)
	if err != nil || string(before) != string(after) {
		t.Fatal("restore replay rewrote cumulative evidence")
	}
}

// An already matching cumulative snapshot is preserved without projection.
func TestPrecompileRestoreHistoryPreservesMatchingCumulativeReceipt(t *testing.T) {
	action, evidence, record := newPrecompileRestorePhaseFixture(t)
	record.Observed["evidence_hash"], record.Observed["complete"] = evidence.EvidenceHash, evidence.Complete
	if _, err := precompileRestoreHistoricalObservation(action, evidence, record); err != nil {
		t.Fatalf("matching cumulative receipt was rejected: %v", err)
	}
}

// Projection strips only later phases, never original identity or proof fields.
func TestPrecompileRestoreHistoryRejectsChangedPhaseEvidence(t *testing.T) {
	for _, change := range []string{"canonical hash", "restore block", "restore transaction", "probe", "identity", "recorded complete", "recorded target", "additional observation"} {
		action, evidence, record := newPrecompileRestorePhaseFixture(t)
		switch change {
		case "canonical hash":
			evidence.Commitment.CanonicalHash = common.Hash{9}.Hex()
		case "restore block":
			evidence.Commitment.RestoreCommitmentBlock++
		case "restore transaction":
			evidence.Commitment.RestoreTransactionHash = common.Hash{9}.Hex()
		case "probe":
			evidence.ProbeAddress = common.Address{9}.Hex()
		case "identity":
			evidence.DeploymentID = "changed-synthetic-deployment"
		case "recorded complete":
			record.Observed["complete"] = true
		case "recorded target":
			record.Observed["target"] = "head-fleet:2"
		case "additional observation":
			record.Observed["unexpected"] = true
		}
		if _, err := precompileRestoreHistoricalObservation(action, evidence, record); err == nil {
			t.Fatalf("changed %s was accepted", change)
		}
	}
}

// Only the exact restore action joins fleet one's authenticated renewal scope.
func TestPrecompileRestoreHistoryRejectsAdjacentActionShapes(t *testing.T) {
	action, _, _ := newPrecompileRestorePhaseFixture(t)
	cfg := testResolvedConfig(t)
	for _, change := range []string{"kind", "fleet", "generation", "storage"} {
		changed := action
		changed.Parameters = map[string]string{"canonical_generation": "2", fleetCommitmentStorageParameter: fleetCommitmentStorageV2}
		switch change {
		case "kind":
			changed.Kind = "evm-read"
		case "fleet":
			changed.Target = "head-fleet:2"
		case "generation":
			changed.Parameters["canonical_generation"] = "3"
		case "storage":
			changed.Parameters[fleetCommitmentStorageParameter] = "different-synthetic-schema"
		}
		if fleets, err := fleetRenewalHistoricalActionFleets(cfg, changed); err == nil || len(fleets) != 0 {
			t.Fatalf("changed %s entered restore scope: %v %v", change, fleets, err)
		}
	}
	for _, actionId := range []string{"precompile.commitment-write", "precompile.read-battery", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out", "precompile.commitment-restore.extra"} {
		if fleets, err := fleetRenewalHistoricalActionFleets(cfg, Action{ID: actionId}); err != nil || len(fleets) != 0 {
			t.Fatalf("unrelated %s entered restore scope: %v %v", actionId, fleets, err)
		}
	}
}

// Exercises the production constructor with source proof and ordered renewal.
func TestPrecompileRestoreHistoryRequiresCompletedRenewal(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 1, 1)
	action := actionByID(t, fixture.plan, "precompile.commitment-restore")
	current := *fixture.plan
	current.PlanHash = common.Hash{3}.Hex()
	current.PriorPlanHashes = append(slices.Clone(current.PriorPlanHashes), fixture.plan.PlanHash)
	current.Actions = slices.Clone(current.Actions)
	current.FleetRenewals = []FleetRenewal{{Round: 1, Fleets: []FleetRenewalFleet{{Fleet: 1, Members: []FleetRenewalMember{{Miner: 1}}}}}}
	executor := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: &current, journal: &Journal{}}
	entry := JournalEntry{Sequence: 10, PlanHash: fixture.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified}
	record := testFleetSupersessionPostcondition(fixture.cfg, action, entry, 100, map[string]any{"restore": true})
	var err error
	entry.PostconditionPath, entry.PostconditionHash, err = executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	executor.journal.entries = []JournalEntry{entry}
	for index, operation := range []string{"commitment", "mirror", "bind"} {
		member := 0
		if operation == "bind" {
			member = 1
		}
		next := testFleetSupersessionAction(t, Action{ID: fleetRenewalActionID(1, 1, operation, member), Kind: "fixture", Target: "synthetic-renewal"})
		current.Actions = append(current.Actions, next)
		successor := JournalEntry{Sequence: uint64(20 + 2*index), PlanHash: current.PlanHash, ActionID: next.ID, IntentHash: next.IntentHash, Stage: StageFinalized,
			TransactionHash: common.Hash{byte(10 + index)}.Hex(), BlockNumber: 120, BlockHash: common.Hash{5}.Hex()}
		executor.journal.entries = append(executor.journal.entries, successor)
		successor.Sequence++
		successor.Stage = StageVerified
		successorRecord := testFleetSupersessionPostcondition(fixture.cfg, next, successor, 120, map[string]any{"complete": true})
		successor.PostconditionPath, successor.PostconditionHash, err = executor.persistActionPostcondition(successorRecord)
		if err != nil {
			t.Fatal(err)
		}
		executor.journal.entries = append(executor.journal.entries, successor)
	}
	completeEntries := slices.Clone(executor.journal.entries)
	for index, successor := range completeEntries {
		if successor.Stage != StageVerified || successor.Sequence < 20 {
			continue
		}
		executor.journal.entries = slices.Clone(completeEntries)
		executor.journal.entries[index].Stage = StageFailed
		if source, handled, err := executor.fleetRenewalHistoricalSource(action, entry, record); err != nil || handled || source != nil {
			t.Fatalf("incomplete %s granted restore history: %t %v", successor.ActionID, handled, err)
		}
	}
	executor.journal.entries = completeEntries
	source, handled, err := executor.fleetRenewalHistoricalSource(action, entry, record)
	if err != nil || !handled || source == nil {
		t.Fatalf("completed renewal did not admit the exact precompile restore: %t %v", handled, err)
	}
	if source.plan.PlanHash != fixture.plan.PlanHash || source.fleetCommitmentHistory == nil || !slices.Equal(source.fleetCommitmentHistory.fleets, []int{1}) || executor.fleetCommitmentHistory != nil {
		t.Fatal("restore history changed the original source or leaked its scope")
	}
	if hash, err := canonicalHashHex(record); err != nil || hash != entry.PostconditionHash {
		t.Fatal("restore history changed the original receipt hash")
	}
	late := entry
	late.Sequence = 30
	if source, handled, err := executor.fleetRenewalHistoricalSource(action, late, record); err != nil || handled || source != nil {
		t.Fatalf("renewal preceding the restore granted history: %t %v", handled, err)
	}
	unguarded := *source
	unguarded.fleetCommitmentHistory = nil
	if _, err := unguarded.historicalActionPostState(context.Background(), action, record, record.EVMFinalized); err == nil || !strings.Contains(err.Error(), "lacks approved completed-renewal history scope") {
		t.Fatalf("restore dispatch bypassed historical scope admission: %v", err)
	}
}
