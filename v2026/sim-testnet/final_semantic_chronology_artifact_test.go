package main

// final_semantic_chronology_artifact_test.go exercises the closed temporary
// oracle proof independently from mutable source collection state.

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Holds one complete in-memory oracle-window graph for artifact-bound
// mutation checks. It deliberately retains raw receipt positions separately
// from the typed window so replacements cannot share mutable backing state.
type finalHistoricalCoordinatorOracleArtifactFixture struct {
	evidence    *FinalSemanticEvidence
	current     *SetupPlan
	plans       map[string]*SetupPlan
	entries     []JournalEntry
	rows        map[string]*FinalHistoricalCoordinatorReceiptEvidence
	receiptLogs map[string][]finalCanonicalEVMLog
	cache       map[string][]byte
	window      finalHistoricalCoordinatorOracleWindowArtifact
}

// Builds a strict v7-sized proof with twenty refresh writes between an active
// checkpoint and restoration. The compact fixture isolates chronology logic
// while retaining real v4 postcondition and journal identities.
func finalHistoricalCoordinatorOracleArtifactTestFixture(t *testing.T) *finalHistoricalCoordinatorOracleArtifactFixture {
	t.Helper()
	coordinator := strings.ToLower(common.HexToAddress("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed").Hex())
	batcher := strings.ToLower(common.HexToAddress("0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359").Hex())
	original := strings.ToLower(common.HexToAddress("0xdbf03b407c01e7cd3cbea99509d93f8dddc8c6fb").Hex())
	planHash := finalTestHex(0x10)
	deploymentID := "oracle-window-fixture"
	activate := Action{ID: "fleet.refresh.oracle-activate", Kind: "evm-transaction", Target: coordinator, Parameters: map[string]string{"oracle": batcher}, IntentHash: finalTestHex(0x11)}
	awaitActive := Action{ID: "fleet.refresh.oracle-await-active", Kind: "evm-read", Target: batcher, DependsOn: []string{activate.ID}, IntentHash: finalTestHex(0x12)}
	restore := Action{ID: "fleet.refresh.oracle-restore", Kind: "evm-transaction", Target: coordinator, Parameters: map[string]string{"oracle": original}, IntentHash: finalTestHex(0x13)}
	awaitRestored := Action{ID: "fleet.refresh.oracle-await-restored", Kind: "evm-read", Target: original, DependsOn: []string{restore.ID}, IntentHash: finalTestHex(0x14)}
	current := &SetupPlan{PlanHash: planHash, Deployment: ContractDeployment{CoordinatorProxy: common.HexToAddress(coordinator)}, Actions: []Action{activate, awaitActive, restore, awaitRestored}}
	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		current.Actions = append(current.Actions, Action{ID: "fleet.refresh.batch." + strconv.FormatUint(batch, 10), Kind: "evm-transaction", Target: batcher, IntentHash: finalTestHex(byte(0x60 + batch))})
	}
	entry := func(sequence uint64, action Action, stage JournalStage, transactionHash string, block uint64, blockHash string) JournalEntry {
		return JournalEntry{Schema: "urnetwork-sim-journal-v1", Sequence: sequence, DeploymentID: deploymentID, PlanHash: planHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: stage, TransactionHash: transactionHash, BlockNumber: block, BlockHash: blockHash}
	}
	activateVerified := entry(10, activate, StageVerified, "", 0, "")
	awaitActiveVerified := entry(12, awaitActive, StageVerified, "", 0, "")
	restoreVerified := entry(100, restore, StageVerified, "", 0, "")
	awaitRestoredVerified := entry(102, awaitRestored, StageVerified, "", 0, "")
	postcondition := func(action Action, journal *JournalEntry, block uint64, target, active string) ActionPostcondition {
		observed := map[string]any{"target_oracle": target, "active_oracle": active}
		record := ActionPostcondition{
			Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: deploymentID, PlanHash: planHash, ActionID: action.ID, IntentHash: action.IntentHash,
			OperationalRPCMode: rpcModePublicOverride, IndependentRPC: false,
			SubstrateFinalized: ChainHead{Number: block, Hash: finalTestHex(byte(block))}, EVMFinalized: ChainHead{Number: block, Hash: finalTestHex(byte(block + 1))}, EVMHashDomain: "evm-rpc", Observed: observed,
			IndependentSubstrateFinalized: ChainHead{Number: block, Hash: finalTestHex(byte(block))}, IndependentEVMFinalized: ChainHead{Number: block, Hash: finalTestHex(byte(block + 1))}, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: map[string]any{"target_oracle": target, "active_oracle": active},
		}
		hash, err := canonicalHashHex(record)
		if err != nil {
			t.Fatal(err)
		}
		journal.PostconditionHash = hash
		return record
	}
	activatePostcondition := postcondition(activate, &activateVerified, 10, batcher, batcher)
	awaitActivePostcondition := postcondition(awaitActive, &awaitActiveVerified, 20, batcher, batcher)
	restorePostcondition := postcondition(restore, &restoreVerified, 70, original, original)
	awaitRestoredPostcondition := postcondition(awaitRestored, &awaitRestoredVerified, 80, original, original)
	activateFinalized := entry(11, activate, StageFinalized, finalTestHex(0x21), 11, finalTestHex(0x22))
	restoreFinalized := entry(101, restore, StageFinalized, finalTestHex(0x23), 71, finalTestHex(0x24))
	entries := []JournalEntry{activateVerified, activateFinalized, awaitActiveVerified, restoreVerified, restoreFinalized, awaitRestoredVerified}
	rows := map[string]*FinalHistoricalCoordinatorReceiptEvidence{
		activateFinalized.TransactionHash: {Receipt: FinalEVMReceipt{TransactionHash: activateFinalized.TransactionHash, Block: ChainHead{Number: activateFinalized.BlockNumber, Hash: activateFinalized.BlockHash}, Status: "success"}, PlanHash: planHash, ActionID: activate.ID, IntentHash: activate.IntentHash, CoordinatorProxy: coordinator, TransactionTo: coordinator},
		restoreFinalized.TransactionHash:  {Receipt: FinalEVMReceipt{TransactionHash: restoreFinalized.TransactionHash, Block: ChainHead{Number: restoreFinalized.BlockNumber, Hash: restoreFinalized.BlockHash}, Status: "success"}, PlanHash: planHash, ActionID: restore.ID, IntentHash: restore.IntentHash, CoordinatorProxy: coordinator, TransactionTo: coordinator},
	}
	receiptLogs := map[string][]finalCanonicalEVMLog{
		activateFinalized.TransactionHash: {{TransactionHash: activateFinalized.TransactionHash, TransactionIndex: 1}},
		restoreFinalized.TransactionHash:  {{TransactionHash: restoreFinalized.TransactionHash, TransactionIndex: 1}},
	}
	lineage := &FinalFleetGenerationLineageEvidence{Batches: make([]FinalFleetGenerationBatchEvidence, 0, finalFleetGenerationBatchCount)}
	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		block := uint64(30) + batch
		transactionHash := finalTestHex(byte(0x40 + batch))
		head := ChainHead{Number: block, Hash: finalTestHex(byte(0x50 + batch))}
		log := finalCanonicalEVMLog{TransactionHash: transactionHash, BlockNumber: head.Number, BlockHash: head.Hash, TransactionIndex: 1}
		action := FinalFleetGenerationActionEvidence{ActionID: "fleet.refresh.batch." + strconv.FormatUint(batch, 10), PlanHash: planHash, IntentHash: finalTestHex(byte(0x60 + batch))}
		finalized := entry(12+batch, Action{ID: action.ActionID, Kind: "evm-transaction", Target: batcher, IntentHash: action.IntentHash}, StageFinalized, transactionHash, head.Number, head.Hash)
		entries = append(entries, finalized)
		write := FinalFleetGenerationWriteEvidence{
			Action: action, Receipt: FinalEVMReceipt{TransactionHash: transactionHash, Block: head, Status: "success"}, Events: []FinalFleetGenerationEventEvidence{{Log: log}}, BatcherAddress: batcher,
		}
		lineage.Batches = append(lineage.Batches, FinalFleetGenerationBatchEvidence{Batch: batch, Generation: 2, BatchWrite: &write})
	}
	evidence := &FinalSemanticEvidence{DeploymentID: deploymentID, EVMCampaignStartHead: ChainHead{Number: 100, Hash: finalTestHex(0x70)}, FleetGeneration: lineage}
	window := finalHistoricalCoordinatorOracleWindowArtifact{
		Schema:              finalHistoricalCoordinatorOracleWindowSchema,
		CoordinatorProxy:    coordinator,
		Activation:          finalHistoricalCoordinatorOracleWindowAction{PlanHash: planHash, ActionID: activate.ID, IntentHash: activate.IntentHash, Finalized: &activateFinalized, Verified: activateVerified, Postcondition: activatePostcondition, TransactionIndex: 1},
		AwaitActive:         finalHistoricalCoordinatorOracleWindowAction{PlanHash: planHash, ActionID: awaitActive.ID, IntentHash: awaitActive.IntentHash, Verified: awaitActiveVerified, Postcondition: awaitActivePostcondition},
		Restore:             finalHistoricalCoordinatorOracleWindowAction{PlanHash: planHash, ActionID: restore.ID, IntentHash: restore.IntentHash, Finalized: &restoreFinalized, Verified: restoreVerified, Postcondition: restorePostcondition, TransactionIndex: 1},
		AwaitRestored:       finalHistoricalCoordinatorOracleWindowAction{PlanHash: planHash, ActionID: awaitRestored.ID, IntentHash: awaitRestored.IntentHash, Verified: awaitRestoredVerified, Postcondition: awaitRestoredPostcondition},
		GenerationTwoWrites: make([]finalHistoricalCoordinatorOracleWindowWrite, 0, finalFleetGenerationBatchCount),
	}
	for _, batch := range lineage.Batches {
		write := *batch.BatchWrite
		var finalized JournalEntry
		for _, entry := range entries {
			if entry.Stage == StageFinalized && entry.TransactionHash == write.Receipt.TransactionHash {
				finalized = entry
				break
			}
		}
		if finalized.TransactionHash == "" {
			t.Fatalf("missing fixture finalized write %s", write.Action.ActionID)
		}
		window.GenerationTwoWrites = append(window.GenerationTwoWrites, finalHistoricalCoordinatorOracleWindowWrite{Action: write.Action, Receipt: write.Receipt, BatcherAddress: write.BatcherAddress, Finalized: finalized, TransactionIndex: 1})
	}
	data, err := json.Marshal(window)
	if err != nil {
		t.Fatal(err)
	}
	checkpoints, err := finalFleetRefreshOracleWindowCheckpointsFromArtifact(window)
	if err != nil {
		t.Fatal(err)
	}
	locator := FinalArtifactLocator{Kind: "historical-fleet-refresh-oracle-window", URI: "final-derived/historical-coordinator/oracle-window.json", ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	evidence.FleetRefreshOracleWindow = FinalFleetRefreshOracleWindowEvidence{Artifact: locator, Checkpoints: checkpoints}
	return &finalHistoricalCoordinatorOracleArtifactFixture{evidence: evidence, current: current, plans: map[string]*SetupPlan{planHash: current}, entries: entries, rows: rows, receiptLogs: receiptLogs, cache: map[string][]byte{locator.URI: data}, window: window}
}

// Rewrites the sealed proof bytes after one test-local field mutation. The
// locator intentionally remains fixed so the direct verifier must reject the
// semantic change even before the outer loader rejects its digest mismatch.
func (self *finalHistoricalCoordinatorOracleArtifactFixture) rewrite(t *testing.T) {
	t.Helper()
	data, err := json.Marshal(self.window)
	if err != nil {
		t.Fatal(err)
	}
	self.cache[self.evidence.FleetRefreshOracleWindow.Artifact.URI] = data
}

// Uses the same checksummed address spelling as the immutable plan builder,
// while retaining canonical lowercase addresses in the evidence artifacts.
func (self *finalHistoricalCoordinatorOracleArtifactFixture) checksumPlanTargets(t *testing.T) {
	t.Helper()
	for index := range self.current.Actions {
		action := &self.current.Actions[index]
		action.Target = common.HexToAddress(action.Target).Hex()
		if oracle, found := action.Parameters["oracle"]; found {
			action.Parameters["oracle"] = common.HexToAddress(oracle).Hex()
		}
	}
	if self.current.Actions[0].Target == self.window.CoordinatorProxy {
		t.Fatal("oracle fixture has no checksummed/lowercase spelling difference")
	}
}

// A normalized evidence address must authenticate the approved address bytes
// without demanding that the separately sealed plan be rewritten lowercase.
func TestFinalSemanticHistoricalOracleWindowPreservesChecksummedPlanTargets(t *testing.T) {
	value := finalHistoricalCoordinatorOracleArtifactTestFixture(t)
	value.checksumPlanTargets(t)
	before, err := json.Marshal(value.plans)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorOracleWindowArtifact(value.evidence, value.current, value.plans, value.entries, value.rows, value.receiptLogs, value.cache); err != nil {
		t.Fatalf("checksummed approved oracle actions were rejected: %v", err)
	}
	after, err := json.Marshal(value.plans)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("oracle artifact verification rewrote immutable plans: %v", err)
	}
}

// A carried handoff belongs to its predecessor proxy, even when the current
// release deploys another proxy and no longer contains the old oracle actions.
func TestFinalSemanticHistoricalOracleWindowPreservesChecksummedPredecessorTargets(t *testing.T) {
	value := finalHistoricalCoordinatorOracleArtifactTestFixture(t)
	value.checksumPlanTargets(t)
	predecessor := value.current
	current := *predecessor
	current.PlanHash = finalTestHex(0x91)
	current.PriorPlanHashes = []string{predecessor.PlanHash}
	current.Actions = nil
	current.Deployment.CoordinatorProxy = common.HexToAddress("0x4000000000000000000000000000000000000004")
	value.current = &current
	value.plans[current.PlanHash] = &current
	before, err := json.Marshal(value.plans)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorOracleWindowArtifact(value.evidence, value.current, value.plans, value.entries, value.rows, value.receiptLogs, value.cache); err != nil {
		t.Fatalf("checksummed predecessor oracle actions were rejected: %v", err)
	}
	after, err := json.Marshal(value.plans)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("carried oracle verification rewrote immutable plans: %v", err)
	}
}

// Rejects each field that would otherwise let an altered temporary-oracle
// interval look consistent with terminal state alone.
func TestFinalSemanticHistoricalOracleWindowArtifactRejectsBoundMutations(t *testing.T) {
	baseline := finalHistoricalCoordinatorOracleArtifactTestFixture(t)
	if err := verifyFinalHistoricalCoordinatorOracleWindowArtifact(baseline.evidence, baseline.current, baseline.plans, baseline.entries, baseline.rows, baseline.receiptLogs, baseline.cache); err != nil {
		t.Fatalf("exact oracle-window artifact rejected: %v", err)
	}
	mutations := []struct {
		name   string
		mutate func(*finalHistoricalCoordinatorOracleArtifactFixture)
	}{
		{name: "missing artifact", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			delete(value.cache, value.evidence.FleetRefreshOracleWindow.Artifact.URI)
		}},
		{name: "await-restored deployment", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.AwaitRestored.Postcondition.DeploymentID = "substituted-deployment"
			value.rewrite(t)
		}},
		{name: "activation await-active target", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.current.Actions[1].Target = "0x4000000000000000000000000000000000000004"
		}},
		{name: "window coordinator proxy", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.CoordinatorProxy = "0x4000000000000000000000000000000000000004"
			value.rewrite(t)
		}},
		{name: "window noncanonical address spelling", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.CoordinatorProxy = common.HexToAddress(value.window.CoordinatorProxy).Hex()
			value.rewrite(t)
		}},
		{name: "activation wrong plan target", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.current.Actions[0].Target = "0x4000000000000000000000000000000000000004"
		}},
		{name: "restore wrong plan target", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.current.Actions[2].Target = "0x4000000000000000000000000000000000000004"
		}},
		{name: "activation malformed plan target", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.current.Actions[0].Target = "not-an-address"
		}},
		{name: "restore zero plan target", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.current.Actions[2].Target = (common.Address{}).Hex()
		}},
		{name: "public checkpoint coordinator proxy", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.evidence.FleetRefreshOracleWindow.Checkpoints.CoordinatorProxy = "0x4000000000000000000000000000000000000004"
		}},
		{name: "public checkpoint noncanonical address spelling", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			checkpoints := &value.evidence.FleetRefreshOracleWindow.Checkpoints
			checkpoints.CoordinatorProxy = common.HexToAddress(checkpoints.CoordinatorProxy).Hex()
		}},
		{name: "activation receipt coordinator proxy", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.rows[value.window.Activation.Finalized.TransactionHash].TransactionTo = "0x4000000000000000000000000000000000000004"
		}},
		{name: "activation noncanonical receipt proxy", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			row := value.rows[value.window.Activation.Finalized.TransactionHash]
			row.CoordinatorProxy = common.HexToAddress(row.CoordinatorProxy).Hex()
		}},
		{name: "restore noncanonical receipt target", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			row := value.rows[value.window.Restore.Finalized.TransactionHash]
			row.TransactionTo = common.HexToAddress(row.TransactionTo).Hex()
		}},
		{name: "activation transaction index", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.Activation.TransactionIndex++
			value.rewrite(t)
		}},
		{name: "restore transaction index", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.Restore.TransactionIndex++
			value.rewrite(t)
		}},
		{name: "generation-two omission", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.GenerationTwoWrites = value.window.GenerationTwoWrites[:len(value.window.GenerationTwoWrites)-1]
			value.rewrite(t)
		}},
		{name: "generation-two coordinate", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.GenerationTwoWrites[0].TransactionIndex++
			value.rewrite(t)
		}},
		{name: "generation-two batcher", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.GenerationTwoWrites[0].BatcherAddress = "0x5000000000000000000000000000000000000005"
			value.rewrite(t)
		}},
		{name: "active checkpoint after write", mutate: func(value *finalHistoricalCoordinatorOracleArtifactFixture) {
			value.window.AwaitActive.Postcondition.EVMFinalized.Number = value.window.GenerationTwoWrites[0].Receipt.Block.Number
			value.window.AwaitActive.Postcondition.IndependentEVMFinalized.Number = value.window.GenerationTwoWrites[0].Receipt.Block.Number
			value.rewrite(t)
		}},
	}
	for _, mutation := range mutations {
		value := finalHistoricalCoordinatorOracleArtifactTestFixture(t)
		mutation.mutate(value)
		if err := verifyFinalHistoricalCoordinatorOracleWindowArtifact(value.evidence, value.current, value.plans, value.entries, value.rows, value.receiptLogs, value.cache); err == nil {
			t.Errorf("%s mutation was accepted", mutation.name)
		}
	}
}

// Binds the immutable oracle object into the public chronology projection so
// a regenerated transcript cannot replace its content address after sealing.
func TestFinalSemanticHistoricalOracleWindowDigestBindsPublicChronologyProjection(t *testing.T) {
	evidence := &FinalSemanticEvidence{
		Deployment:           FinalContractDeploymentEvidence{CoordinatorProxy: "0x1000000000000000000000000000000000000001"},
		EVMCampaignStartHead: ChainHead{Number: 10, Hash: finalTestHex(0x31)}, EVMTerminalHead: ChainHead{Number: 20, Hash: finalTestHex(0x32)},
		FleetGeneration: &FinalFleetGenerationLineageEvidence{},
		FleetRefreshOracleWindow: FinalFleetRefreshOracleWindowEvidence{
			Artifact: FinalArtifactLocator{Kind: "historical-fleet-refresh-oracle-window", URI: "final-derived/historical-coordinator/oracle-window.json", ContentHash: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1},
			Checkpoints: FinalFleetRefreshOracleWindowCheckpoints{
				CoordinatorProxy:         "0x1000000000000000000000000000000000000001",
				AwaitActiveOperational:   FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 10, Hash: finalTestHex(0x31)}, Oracle: "0x2000000000000000000000000000000000000002"},
				AwaitActiveIndependent:   FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 10, Hash: finalTestHex(0x31)}, Oracle: "0x2000000000000000000000000000000000000002"},
				AwaitRestoredOperational: FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 20, Hash: finalTestHex(0x32)}, Oracle: "0x3000000000000000000000000000000000000003"},
				AwaitRestoredIndependent: FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 20, Hash: finalTestHex(0x32)}, Oracle: "0x3000000000000000000000000000000000000003"},
			},
		},
	}
	head := evidence.EVMCampaignStartHead
	audit, err := finalPublicChronologyAuditForEvidence(evidence, []ChainHead{head, evidence.EVMTerminalHead})
	if err != nil {
		t.Fatal(err)
	}
	changed := audit
	changed.OracleWindowArtifactHash = "sha256:" + strings.Repeat("b", 64)
	changed.ProjectionHash = ""
	hash, err := canonicalHashHex(changed)
	if err != nil {
		t.Fatal(err)
	}
	changed.ProjectionHash = hash
	if err := verifyFinalPublicChronologyAudit(evidence, changed, []ChainHead{head, evidence.EVMTerminalHead}); err == nil {
		t.Fatal("rehashed public chronology projection accepted another oracle-window digest")
	}
}

// Rejects a bare namespace entry before source collection can silently skip
// unreviewed bytes beside otherwise valid predecessor-plan artifacts.
func TestFinalSemanticHistoricalPlanHistoryRejectsBareNamespace(t *testing.T) {
	if _, err := finalHistoricalCoordinatorPlanHistoryNames(map[string][]byte{"plan-history": []byte("surplus")}); err == nil {
		t.Fatal("bare historical plan namespace was accepted")
	}
}
