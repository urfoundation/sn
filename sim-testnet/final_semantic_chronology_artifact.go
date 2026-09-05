package main

// final_semantic_chronology_artifact.go replays the closed source graph for
// every carried coordinator receipt. It deliberately verifies the archived
// predecessor action before a public RPC reader may compare historical state.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Mirrors the canonical proof object emitted when the closed live-chain
// snapshot first groups a receipt. Comparing this against the separately
// derived historical artifact prevents a matching aggregate log hash from
// concealing a substituted or omitted raw event.
type finalHistoricalCoordinatorReceiptProof struct {
	Status          string                 `json:"status"`
	TransactionHash string                 `json:"transaction_hash"`
	Block           ChainHead              `json:"block"`
	Logs            []finalCanonicalEVMLog `json:"logs"`
}

// Reconstructs the complete approved predecessor-plan namespace from the
// fleet-lineage artifact before historical coordinator rows are accepted. The
// fleet proof already seals every retained plan and journal byte stream, so
// chronology can reject an omitted coordinator-targeted setup call without
// reopening a mutable plan-history directory.
func finalHistoricalCoordinatorArtifactLineage(evidence *FinalSemanticEvidence, current *SetupPlan, cache map[string][]byte) (map[string]*SetupPlan, []JournalEntry, []byte, error) {
	if evidence == nil || current == nil || evidence.FleetGeneration == nil || len(cache) == 0 {
		return nil, nil, nil, errors.New("historical coordinator artifact lineage inputs are unavailable")
	}
	lineageData, found := cache[evidence.FleetGeneration.Artifact.URI]
	if !found {
		return nil, nil, nil, errors.New("historical coordinator fleet lineage artifact is not loaded")
	}
	files, err := finalFleetGenerationArtifactFiles(evidence, lineageData)
	if err != nil {
		return nil, nil, nil, err
	}
	currentData, found := files["launch-foundation/plan.json"]
	if !found || !bytes.Equal(currentData, cache[evidence.PlanArtifact.URI]) {
		return nil, nil, nil, errors.New("historical coordinator current plan differs from fleet lineage bytes")
	}
	plans := map[string]*SetupPlan{current.PlanHash: current}
	expectedPaths := make(map[string]string, len(current.PriorPlanHashes))
	for _, planHash := range current.PriorPlanHashes {
		if err := requireFinalHex32("historical coordinator predecessor plan", planHash); err != nil {
			return nil, nil, nil, err
		}
		path := filepath.ToSlash(filepath.Join("plan-history", stringsTrim0x(planHash)+".json"))
		if _, duplicate := expectedPaths[path]; duplicate {
			return nil, nil, nil, fmt.Errorf("historical coordinator predecessor plan path %s is duplicated", path)
		}
		expectedPaths[path] = planHash
	}
	for path := range files {
		if path != "plan-history" && !strings.HasPrefix(path, "plan-history/") {
			continue
		}
		if _, approved := expectedPaths[path]; !approved {
			return nil, nil, nil, fmt.Errorf("historical coordinator lineage plan-history entry %s is unapproved", path)
		}
	}
	for path, planHash := range expectedPaths {
		data, found := files[path]
		if !found {
			return nil, nil, nil, fmt.Errorf("historical coordinator predecessor plan %s is absent", planHash)
		}
		plan, decodeErr := decodePersistedPlanBytes(data)
		if decodeErr != nil || plan.PlanHash != planHash || plan.DeploymentID != evidence.DeploymentID || plan.ChainID != evidence.ChainID || plan.Netuid != evidence.Netuid {
			return nil, nil, nil, stateMismatchError(decodeErr, "historical coordinator predecessor plan %s differs from approved lineage", planHash)
		}
		plans[planHash] = plan
	}
	journal, found := files["launch-foundation/journal.jsonl"]
	if !found {
		return nil, nil, nil, errors.New("historical coordinator lineage journal is absent")
	}
	entries, err := decodeFinalSemanticJournalBytes(journal)
	if err != nil {
		return nil, nil, nil, err
	}
	return plans, entries, append([]byte(nil), journal...), nil
}

// Retrieves the exact retained plan bytes for one approved lineage member.
// The current plan comes from its dedicated artifact; predecessors come only
// from the sealed fleet-lineage namespace, never from an ambient run path.
func finalHistoricalCoordinatorArtifactPlanBytes(evidence *FinalSemanticEvidence, current, plan *SetupPlan, cache map[string][]byte) ([]byte, error) {
	if evidence == nil || current == nil || plan == nil || evidence.FleetGeneration == nil {
		return nil, errors.New("historical coordinator lineage plan is unavailable")
	}
	if plan.PlanHash == current.PlanHash {
		data, found := cache[evidence.PlanArtifact.URI]
		if !found {
			return nil, errors.New("historical coordinator current plan artifact is not loaded")
		}
		return append([]byte(nil), data...), nil
	}
	lineageData, found := cache[evidence.FleetGeneration.Artifact.URI]
	if !found {
		return nil, errors.New("historical coordinator fleet lineage artifact is not loaded")
	}
	files, err := finalFleetGenerationArtifactFiles(evidence, lineageData)
	if err != nil {
		return nil, err
	}
	path := filepath.ToSlash(filepath.Join("plan-history", stringsTrim0x(plan.PlanHash)+".json"))
	data, found := files[path]
	if !found {
		return nil, fmt.Errorf("historical coordinator plan %s is absent from fleet lineage", plan.PlanHash)
	}
	return append([]byte(nil), data...), nil
}

// Resolves the one approved action and verified journal record for a
// postcondition-only oracle proof. It intentionally searches all retained
// revisions and rejects ambiguity rather than choosing a latest entry.
func finalHistoricalCoordinatorArtifactVerifiedAction(evidence *FinalSemanticEvidence, plans map[string]*SetupPlan, entries []JournalEntry, actionID string) (*SetupPlan, Action, JournalEntry, error) {
	if evidence == nil || len(plans) == 0 || actionID == "" {
		return nil, Action{}, JournalEntry{}, errors.New("historical coordinator verified action inputs are incomplete")
	}
	planHashes := make([]string, 0, len(plans))
	for planHash := range plans {
		planHashes = append(planHashes, planHash)
	}
	sort.Strings(planHashes)
	var foundPlan *SetupPlan
	var foundAction Action
	var foundEntry *JournalEntry
	for _, planHash := range planHashes {
		plan := plans[planHash]
		action, actionErr := exactPlanActionByID(plan, actionID)
		if actionErr != nil {
			continue
		}
		for index := range entries {
			entry := &entries[index]
			if entry.Stage != StageVerified || entry.DeploymentID != evidence.DeploymentID || entry.PlanHash != plan.PlanHash || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash {
				continue
			}
			if foundEntry != nil {
				return nil, Action{}, JournalEntry{}, fmt.Errorf("historical coordinator action %s has multiple verified records", actionID)
			}
			foundPlan, foundAction, foundEntry = plan, action, entry
		}
	}
	if foundEntry == nil {
		return nil, Action{}, JournalEntry{}, fmt.Errorf("historical coordinator action %s has no verified record", actionID)
	}
	return foundPlan, foundAction, *foundEntry, nil
}

// Validates one embedded v4 receipt before its journal hash is trusted. The
// window artifact stores this full object so await-restored remains replayable
// even though it has no EVM transaction row of its own.
func finalHistoricalCoordinatorArtifactPostcondition(record ActionPostcondition, verified JournalEntry) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	decoded, err := decodeFinalActionPostconditionV4(data)
	if err != nil || decoded.DeploymentID == "" || decoded.DeploymentID != verified.DeploymentID || decoded.PlanHash != verified.PlanHash || decoded.ActionID != verified.ActionID || decoded.IntentHash != verified.IntentHash {
		return stateMismatchError(err, "historical coordinator oracle postcondition differs from verified journal")
	}
	hash, err := canonicalHashHex(decoded)
	if err != nil || !strings.EqualFold(hash, verified.PostconditionHash) {
		return stateMismatchError(err, "historical coordinator oracle postcondition hash differs from verified journal")
	}
	return nil
}

// Materializes the canonical generation-two position list from the already
// sealed fleet lineage. Every event in a batch receipt must agree on its
// transaction index; a cross-contract log cannot claim two EVM positions.
func finalHistoricalCoordinatorArtifactGenerationWrites(evidence *FinalSemanticEvidence, entries []JournalEntry) ([]finalHistoricalCoordinatorOracleWindowWrite, error) {
	if evidence == nil || evidence.FleetGeneration == nil || len(entries) == 0 {
		return nil, errors.New("historical fleet refresh generation evidence is unavailable")
	}
	result := make([]finalHistoricalCoordinatorOracleWindowWrite, 0, finalFleetGenerationBatchCount)
	seenActions := make(map[string]bool)
	seenTransactions := make(map[string]bool)
	for _, batch := range evidence.FleetGeneration.Batches {
		if batch.Generation != 2 {
			continue
		}
		if batch.BatchWrite == nil || len(batch.BatchWrite.Events) == 0 {
			return nil, fmt.Errorf("historical fleet refresh generation-two batch %d has no complete write", batch.Batch)
		}
		write := *batch.BatchWrite
		index := write.Events[0].Log.TransactionIndex
		for _, event := range write.Events {
			if event.Log.TransactionHash != write.Receipt.TransactionHash || event.Log.BlockNumber != write.Receipt.Block.Number || !strings.EqualFold(event.Log.BlockHash, write.Receipt.Block.Hash) || event.Log.TransactionIndex != index {
				return nil, fmt.Errorf("historical fleet refresh generation-two batch %d has inconsistent receipt coordinates", batch.Batch)
			}
		}
		key := write.Action.PlanHash + ":" + write.Action.ActionID + ":" + write.Action.IntentHash
		if seenActions[key] || seenTransactions[write.Receipt.TransactionHash] {
			return nil, errors.New("historical fleet refresh generation-two writes are duplicated")
		}
		seenActions[key] = true
		seenTransactions[write.Receipt.TransactionHash] = true
		var finalized *JournalEntry
		for entryIndex := range entries {
			entry := &entries[entryIndex]
			if entry.Stage != StageFinalized || entry.DeploymentID != evidence.DeploymentID || entry.PlanHash != write.Action.PlanHash || entry.ActionID != write.Action.ActionID || entry.IntentHash != write.Action.IntentHash || entry.TransactionHash != write.Receipt.TransactionHash {
				continue
			}
			if entry.BlockNumber != write.Receipt.Block.Number || !strings.EqualFold(entry.BlockHash, write.Receipt.Block.Hash) {
				return nil, fmt.Errorf("historical fleet refresh generation-two write %s finalized coordinates differ", write.Action.ActionID)
			}
			if finalized != nil {
				return nil, fmt.Errorf("historical fleet refresh generation-two write %s has multiple finalized records", write.Action.ActionID)
			}
			finalized = entry
		}
		if finalized == nil {
			return nil, fmt.Errorf("historical fleet refresh generation-two write %s has no finalized record", write.Action.ActionID)
		}
		result = append(result, finalHistoricalCoordinatorOracleWindowWrite{Action: write.Action, Receipt: write.Receipt, BatcherAddress: write.BatcherAddress, Finalized: *finalized, TransactionIndex: index})
	}
	if len(result) != int(finalFleetGenerationBatchCount) {
		return nil, fmt.Errorf("historical fleet refresh generation-two writes=%d, want %d", len(result), finalFleetGenerationBatchCount)
	}
	return result, nil
}

// Checks one embedded action against its exact approved plan/journal lineage
// and, for transaction actions, the separately sealed historical row. The
// two artifact channels must agree before their shared chronology is used.
func finalHistoricalCoordinatorArtifactOracleAction(evidence *FinalSemanticEvidence, current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, rows map[string]*FinalHistoricalCoordinatorReceiptEvidence, receiptLogs map[string][]finalCanonicalEVMLog, actionID string, value finalHistoricalCoordinatorOracleWindowAction) (Action, JournalEntry, error) {
	plan, action, verified, err := finalHistoricalCoordinatorArtifactVerifiedAction(evidence, plans, entries, actionID)
	if err != nil {
		return Action{}, JournalEntry{}, err
	}
	if value.PlanHash != plan.PlanHash || value.ActionID != action.ID || value.IntentHash != action.IntentHash || !finalJSONEqual(value.Verified, verified) {
		return Action{}, JournalEntry{}, fmt.Errorf("historical coordinator oracle %s verified identity differs", actionID)
	}
	if err := finalHistoricalCoordinatorArtifactPostcondition(value.Postcondition, verified); err != nil {
		return Action{}, JournalEntry{}, err
	}
	if actionID == "fleet.refresh.oracle-await-active" || actionID == "fleet.refresh.oracle-await-restored" {
		if value.Finalized != nil || value.TransactionIndex != 0 {
			return Action{}, JournalEntry{}, fmt.Errorf("historical coordinator oracle %s incorrectly names a transaction", actionID)
		}
		return action, verified, nil
	}
	targets, targetErr := finalHistoricalCoordinatorJournalActions(evidence, current, plans, entries)
	if targetErr != nil {
		return Action{}, JournalEntry{}, targetErr
	}
	if value.Finalized == nil {
		return Action{}, JournalEntry{}, fmt.Errorf("historical coordinator oracle %s finalized transaction is absent", actionID)
	}
	target, found := targets[value.Finalized.TransactionHash]
	if !found || target.action.ID != actionID || target.plan.PlanHash != plan.PlanHash || !finalJSONEqual(target.entry, *value.Finalized) {
		return Action{}, JournalEntry{}, fmt.Errorf("historical coordinator oracle %s finalized transaction differs", actionID)
	}
	row := rows[value.Finalized.TransactionHash]
	if row == nil || row.ActionID != actionID || row.PlanHash != plan.PlanHash || row.IntentHash != action.IntentHash || row.Receipt.Block.Number != value.Finalized.BlockNumber || !strings.EqualFold(row.Receipt.Block.Hash, value.Finalized.BlockHash) {
		return Action{}, JournalEntry{}, fmt.Errorf("historical coordinator oracle %s receipt row differs", actionID)
	}
	logs := receiptLogs[value.Finalized.TransactionHash]
	if len(logs) == 0 || logs[0].TransactionIndex != value.TransactionIndex {
		return Action{}, JournalEntry{}, fmt.Errorf("historical coordinator oracle %s transaction index differs from raw receipt", actionID)
	}
	for _, log := range logs {
		if log.TransactionIndex != value.TransactionIndex {
			return Action{}, JournalEntry{}, fmt.Errorf("historical coordinator oracle %s raw receipt has inconsistent transaction index", actionID)
		}
	}
	return action, target.entry, nil
}

// Joins immutable, possibly checksummed plan targets to canonical evidence
// addresses. Source collection and sealed replay share this check so neither
// requires rewriting approved plan bytes to match artifact serialization.
func finalHistoricalCoordinatorOracleWindowProxy(activation, restore Action, activationRow, restoreRow *FinalHistoricalCoordinatorReceiptEvidence) (string, error) {
	activationProxy, activationErr := finalCanonicalAddress(activation.Target)
	restoreProxy, restoreErr := finalCanonicalAddress(restore.Target)
	if activationErr != nil || restoreErr != nil || !strings.EqualFold(activationProxy, activation.Target) || !strings.EqualFold(restoreProxy, restore.Target) || activationProxy != restoreProxy || activationRow == nil || restoreRow == nil || activationRow.CoordinatorProxy != activationProxy || restoreRow.CoordinatorProxy != activationProxy || activationRow.TransactionTo != activationProxy || restoreRow.TransactionTo != activationProxy {
		return "", stateMismatchError(errors.Join(activationErr, restoreErr), "historical fleet refresh oracle proxy differs from its sealed actions or receipts")
	}
	return activationProxy, nil
}

// Replays the sealed temporary-oracle window and joins it to raw receipt,
// plan, journal, fleet-generation, and v4 postcondition artifacts. This is
// deliberately separate from source collection so a self-consistent mutation
// of final evidence cannot bypass the pre-campaign handoff proof.
func verifyFinalHistoricalCoordinatorOracleWindowArtifact(evidence *FinalSemanticEvidence, current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, rows map[string]*FinalHistoricalCoordinatorReceiptEvidence, receiptLogs map[string][]finalCanonicalEVMLog, cache map[string][]byte) error {
	if err := verifyFinalFleetRefreshOracleWindowEvidence(evidence); err != nil {
		return err
	}
	data, found := cache[evidence.FleetRefreshOracleWindow.Artifact.URI]
	if !found {
		return errors.New("historical fleet refresh oracle window artifact is not loaded")
	}
	var window finalHistoricalCoordinatorOracleWindowArtifact
	if err := decodeStrictJSONBytes(data, &window); err != nil {
		return err
	}
	if window.Schema != finalHistoricalCoordinatorOracleWindowSchema {
		return errors.New("historical fleet refresh oracle window schema is unsupported")
	}
	activation, activationFinalized, err := finalHistoricalCoordinatorArtifactOracleAction(evidence, current, plans, entries, rows, receiptLogs, "fleet.refresh.oracle-activate", window.Activation)
	if err != nil {
		return err
	}
	awaitActive, awaitActiveVerified, err := finalHistoricalCoordinatorArtifactOracleAction(evidence, current, plans, entries, rows, receiptLogs, "fleet.refresh.oracle-await-active", window.AwaitActive)
	if err != nil {
		return err
	}
	restore, restoreFinalized, err := finalHistoricalCoordinatorArtifactOracleAction(evidence, current, plans, entries, rows, receiptLogs, "fleet.refresh.oracle-restore", window.Restore)
	if err != nil {
		return err
	}
	awaitRestored, awaitVerified, err := finalHistoricalCoordinatorArtifactOracleAction(evidence, current, plans, entries, rows, receiptLogs, "fleet.refresh.oracle-await-restored", window.AwaitRestored)
	if err != nil {
		return err
	}
	activationRow := rows[activationFinalized.TransactionHash]
	restoreRow := rows[restoreFinalized.TransactionHash]
	coordinatorProxy, proxyErr := finalHistoricalCoordinatorOracleWindowProxy(activation, restore, activationRow, restoreRow)
	if proxyErr != nil || coordinatorProxy != window.CoordinatorProxy {
		return stateMismatchError(proxyErr, "historical fleet refresh oracle proxy differs from its sealed actions or receipts")
	}
	if err := validateFleetRefreshOraclePostconditionIdentity(activation, window.Activation.Verified, &window.Activation.Postcondition); err != nil {
		return fmt.Errorf("historical fleet refresh oracle artifact activation: %w", err)
	}
	if err := validateFleetRefreshOracleSupersession(awaitActive, awaitActiveVerified, &window.AwaitActive.Postcondition, restore, window.Restore.Verified, &window.Restore.Postcondition, awaitRestored, awaitVerified, &window.AwaitRestored.Postcondition); err != nil {
		return fmt.Errorf("historical fleet refresh oracle artifact supersession: %w", err)
	}
	if awaitActiveVerified.Sequence <= window.Activation.Verified.Sequence || window.AwaitActive.Postcondition.EVMFinalized.Number < window.Activation.Postcondition.EVMFinalized.Number || window.AwaitActive.Postcondition.IndependentEVMFinalized.Number < window.Activation.Postcondition.IndependentEVMFinalized.Number {
		return errors.New("historical fleet refresh oracle artifact active-oracle checkpoint does not follow activation")
	}
	activateOracle, activateOracleErr := plannedFleetRefreshOracleTarget(activation)
	awaitActiveOracle, awaitActiveOracleErr := plannedFleetRefreshOracleTarget(awaitActive)
	if activateOracleErr != nil || awaitActiveOracleErr != nil || activateOracle != awaitActiveOracle {
		return stateMismatchError(errors.Join(activateOracleErr, awaitActiveOracleErr), "historical fleet refresh oracle artifact activation and await-active oracle differ")
	}
	if current == nil || activationFinalized.PlanHash == "" || restoreFinalized.PlanHash == "" {
		return errors.New("historical fleet refresh oracle artifact current lineage is incomplete")
	}
	activationPosition := finalHistoricalCoordinatorTransactionPosition{block: ChainHead{Number: activationFinalized.BlockNumber, Hash: activationFinalized.BlockHash}, transactionIndex: window.Activation.TransactionIndex, transactionHash: activationFinalized.TransactionHash}
	restorePosition := finalHistoricalCoordinatorTransactionPosition{block: ChainHead{Number: restoreFinalized.BlockNumber, Hash: restoreFinalized.BlockHash}, transactionIndex: window.Restore.TransactionIndex, transactionHash: restoreFinalized.TransactionHash}
	before, orderErr := finalHistoricalCoordinatorPositionBefore(activationPosition, restorePosition)
	if orderErr != nil || !before {
		return errors.Join(orderErr, errors.New("historical fleet refresh oracle artifact restore is not after activation"))
	}
	wantWrites, err := finalHistoricalCoordinatorArtifactGenerationWrites(evidence, entries)
	if err != nil || !finalJSONEqual(window.GenerationTwoWrites, wantWrites) {
		return stateMismatchError(err, "historical fleet refresh oracle artifact generation-two writes differ from fleet lineage")
	}
	for _, write := range window.GenerationTwoWrites {
		batcher, batcherErr := finalCanonicalAddress(write.BatcherAddress)
		if batcherErr != nil || !strings.EqualFold(batcher, activateOracle.Hex()) {
			return stateMismatchError(batcherErr, "historical fleet refresh generation-two write %s targets another batcher", write.Action.ActionID)
		}
		position := finalHistoricalCoordinatorTransactionPosition{block: write.Receipt.Block, transactionIndex: write.TransactionIndex, transactionHash: write.Receipt.TransactionHash}
		afterActivation, activationErr := finalHistoricalCoordinatorPositionBefore(activationPosition, position)
		beforeRestore, restoreErr := finalHistoricalCoordinatorPositionBefore(position, restorePosition)
		if activationErr != nil || restoreErr != nil {
			return errors.Join(activationErr, restoreErr)
		}
		if !afterActivation || !beforeRestore {
			return fmt.Errorf("historical fleet refresh generation-two write %s is outside the sealed oracle window", write.Action.ActionID)
		}
		if position.block.Number <= window.AwaitActive.Postcondition.EVMFinalized.Number || position.block.Number <= window.AwaitActive.Postcondition.IndependentEVMFinalized.Number {
			return fmt.Errorf("historical fleet refresh generation-two write %s does not follow the active-oracle checkpoint", write.Action.ActionID)
		}
		if write.Finalized.Sequence <= awaitActiveVerified.Sequence || write.Finalized.Sequence >= window.Restore.Finalized.Sequence || write.Finalized.Sequence >= window.Restore.Verified.Sequence {
			return fmt.Errorf("historical fleet refresh generation-two write %s is outside the verified oracle journal interval", write.Action.ActionID)
		}
	}
	checkpoints, checkpointErr := finalFleetRefreshOracleWindowCheckpointsFromArtifact(window)
	if checkpointErr != nil || !finalJSONEqual(checkpoints, evidence.FleetRefreshOracleWindow.Checkpoints) {
		return stateMismatchError(checkpointErr, "historical fleet refresh oracle public checkpoints differ from the sealed window")
	}
	return nil
}

// Reconstructs each carried coordinator mutation from its independently
// content-addressed plan, journal, postcondition, and complete transaction
// receipt. No mutable archive path or self-described evidence field is used
// as authority during this offline review.
func verifyFinalHistoricalCoordinatorReceiptArtifacts(evidence *FinalSemanticEvidence, current *SetupPlan, cache map[string][]byte) error {
	if evidence == nil || current == nil || len(cache) == 0 {
		return errors.New("historical coordinator artifact inputs are unavailable")
	}
	policyData, found := cache[evidence.PolicyArtifact.URI]
	if !found {
		return errors.New("historical coordinator policy artifact is not loaded")
	}
	policy, err := verifyFinalPolicyArtifact(evidence, policyData)
	if err != nil {
		return err
	}
	plans, entries, lineageJournal, err := finalHistoricalCoordinatorArtifactLineage(evidence, current, cache)
	if err != nil {
		return err
	}
	ordinary, err := finalSemanticUniqueCarriedEVMReceipts(evidence)
	if err != nil {
		return err
	}
	targets, err := finalHistoricalCoordinatorJournalActions(evidence, current, plans, entries)
	if err != nil {
		return err
	}
	rows := make(map[string]*FinalHistoricalCoordinatorReceiptEvidence, len(evidence.HistoricalCoordinatorReceipts))
	var sharedJournal *FinalArtifactLocator
	for index := range evidence.HistoricalCoordinatorReceipts {
		row := &evidence.HistoricalCoordinatorReceipts[index]
		if _, duplicate := rows[row.Receipt.TransactionHash]; duplicate {
			return fmt.Errorf("historical coordinator receipt %s is duplicated", row.Receipt.TransactionHash)
		}
		if sharedJournal == nil {
			copy := row.JournalArtifact
			sharedJournal = &copy
		} else if !finalJSONEqual(*sharedJournal, row.JournalArtifact) {
			return errors.New("historical coordinator rows use different journal artifacts")
		}
		if data, found := cache[row.JournalArtifact.URI]; !found || !bytes.Equal(data, lineageJournal) {
			return fmt.Errorf("historical coordinator journal artifact %d differs from fleet-lineage journal", index)
		}
		if prior, found := ordinary[row.Receipt.TransactionHash]; found && prior != row.Receipt {
			return fmt.Errorf("historical coordinator receipt %s differs from ordinary semantic evidence", row.Receipt.TransactionHash)
		}
		if target, found := targets[row.Receipt.TransactionHash]; found {
			if row.PlanHash != target.plan.PlanHash || row.ActionID != target.action.ID || row.IntentHash != target.entry.IntentHash || row.Receipt.Block.Number != target.entry.BlockNumber || !strings.EqualFold(row.Receipt.Block.Hash, target.entry.BlockHash) {
				return fmt.Errorf("historical coordinator target receipt %s differs from finalized action", row.Receipt.TransactionHash)
			}
		} else if _, found := ordinary[row.Receipt.TransactionHash]; !found {
			return fmt.Errorf("historical coordinator receipt %s is not an approved ordinary or coordinator-targeted mutation", row.Receipt.TransactionHash)
		}
		rows[row.Receipt.TransactionHash] = row
	}
	for transactionHash := range ordinary {
		if rows[transactionHash] == nil {
			return fmt.Errorf("historical coordinator ordinary receipt %s is omitted", transactionHash)
		}
	}
	for transactionHash := range targets {
		if rows[transactionHash] == nil {
			return fmt.Errorf("historical coordinator target action receipt %s is omitted", transactionHash)
		}
	}
	receiptLogs := make(map[string][]finalCanonicalEVMLog, len(rows))
	for transactionHash, row := range rows {
		receiptData, found := cache[row.ReceiptArtifact.URI]
		if !found {
			return fmt.Errorf("historical coordinator receipt artifact %s is not loaded", transactionHash)
		}
		_, logs, receiptErr := finalHistoricalCoordinatorReceiptArtifactTransaction(row, receiptData)
		if receiptErr != nil {
			return fmt.Errorf("historical coordinator receipt artifact %s: %w", transactionHash, receiptErr)
		}
		receiptLogs[transactionHash] = logs
	}
	if finalHistoricalCoordinatorTimelineRequired(evidence) {
		if err := verifyFinalHistoricalCoordinatorTimelineArtifact(evidence, current, plans, entries, cache[evidence.HistoricalCoordinatorTimelineArtifact.URI]); err != nil {
			return err
		}
	}
	if err := verifyFinalHistoricalCoordinatorOracleWindowArtifact(evidence, current, plans, entries, rows, receiptLogs, cache); err != nil {
		return err
	}
	for index := range evidence.HistoricalCoordinatorReceipts {
		row := &evidence.HistoricalCoordinatorReceipts[index]
		planData, found := cache[row.PlanArtifact.URI]
		if !found {
			return fmt.Errorf("historical coordinator plan artifact %d is not loaded", index)
		}
		plan, err := decodePersistedPlanBytes(planData)
		lineagePlan := plans[row.PlanHash]
		expectedPlanData, lineageErr := finalHistoricalCoordinatorArtifactPlanBytes(evidence, current, lineagePlan, cache)
		if err != nil || lineageErr != nil || lineagePlan == nil || !bytes.Equal(planData, expectedPlanData) || !strings.EqualFold(plan.PlanHash, row.PlanHash) || !current.allowedPlanHashes()[plan.PlanHash] || plan.DeploymentID != evidence.DeploymentID || plan.ChainID != evidence.ChainID || plan.Netuid != evidence.Netuid {
			return stateMismatchError(errors.Join(err, lineageErr), "historical coordinator plan artifact %d differs from the approved lineage", index)
		}
		journalData, found := cache[row.JournalArtifact.URI]
		if !found {
			return fmt.Errorf("historical coordinator journal artifact %d is not loaded", index)
		}
		entries, err := decodeFinalSemanticJournalBytes(journalData)
		if err != nil {
			return fmt.Errorf("historical coordinator journal artifact %d: %w", index, err)
		}
		action, finalized, verified, err := finalHistoricalCoordinatorJournalArtifactAction(evidence, current, plan, entries, row)
		if err != nil {
			return fmt.Errorf("historical coordinator journal artifact %d: %w", index, err)
		}
		postconditionData, found := cache[row.PostconditionArtifact.URI]
		if !found {
			return fmt.Errorf("historical coordinator postcondition artifact %d is not loaded", index)
		}
		postcondition, err := decodeFinalActionPostconditionV4(postconditionData)
		if err != nil || postcondition.DeploymentID != evidence.DeploymentID || postcondition.PlanHash != finalized.PlanHash || postcondition.ActionID != finalized.ActionID || postcondition.IntentHash != finalized.IntentHash {
			return stateMismatchError(err, "historical coordinator postcondition artifact %d differs from its journal", index)
		}
		postconditionHash, err := canonicalHashHex(postcondition)
		if err != nil || !strings.EqualFold(postconditionHash, verified.PostconditionHash) {
			return stateMismatchError(err, "historical coordinator postcondition artifact %d hash differs from its journal", index)
		}
		receiptData, found := cache[row.ReceiptArtifact.URI]
		if !found {
			return fmt.Errorf("historical coordinator receipt artifact %d is not loaded", index)
		}
		transaction, logs, err := finalHistoricalCoordinatorReceiptArtifactTransaction(row, receiptData)
		if err != nil {
			return fmt.Errorf("historical coordinator receipt artifact %d: %w", index, err)
		}
		emitters, err := finalHistoricalCoordinatorEmitterGraph(logs)
		if err != nil || !finalJSONEqual(emitters, row.Emitters) {
			return stateMismatchError(err, "historical coordinator receipt artifact %d emitter graph differs", index)
		}
		proofData, found := cache[row.Receipt.Proof.URI]
		if !found {
			return fmt.Errorf("historical coordinator captured receipt proof %d is not loaded", index)
		}
		if err := verifyFinalHistoricalCoordinatorReceiptProof(row, proofData, logs); err != nil {
			return fmt.Errorf("historical coordinator captured receipt proof %d: %w", index, err)
		}
		if err := verifyFinalHistoricalCoordinatorActionWithPostcondition(evidence, policy, postcondition, plan, action, row.Receipt, transaction, logs, emitters); err != nil {
			return fmt.Errorf("historical coordinator receipt artifact %d action binding: %w", index, err)
		}
		if !strings.EqualFold(plan.Deployment.CoordinatorProxy.Hex(), row.CoordinatorProxy) {
			return errors.New("historical coordinator artifact proxy differs from archived plan")
		}
		if err := finalVerifyHistoricalCoordinatorReceiptTimeline(row, evidence.HistoricalCoordinatorTimeline); err != nil {
			return fmt.Errorf("historical coordinator artifact %d execution timeline: %w", index, err)
		}
	}
	return nil
}

// Enforces byte-for-byte semantic equality between the snapshot's original
// receipt proof and the independently content-addressed historical replay
// artifact. Both are required; neither can substitute for the other.
func verifyFinalHistoricalCoordinatorReceiptProof(row *FinalHistoricalCoordinatorReceiptEvidence, data []byte, logs []finalCanonicalEVMLog) error {
	if row == nil || len(data) == 0 || len(logs) == 0 {
		return errors.New("historical coordinator captured receipt proof is incomplete")
	}
	var proof finalHistoricalCoordinatorReceiptProof
	if err := decodeStrictJSONBytes(data, &proof); err != nil {
		return err
	}
	if proof.Status != row.Receipt.Status || proof.TransactionHash != row.Receipt.TransactionHash || proof.Block != row.Receipt.Block || len(proof.Logs) != len(logs) {
		return errors.New("historical coordinator captured receipt proof differs from its sealed row")
	}
	canonical, err := finalCanonicalizeLogs(proof.Logs)
	if err != nil || len(canonical) != len(proof.Logs) {
		return stateMismatchError(err, "historical coordinator captured receipt logs are not canonical")
	}
	for index := range canonical {
		if !finalSemanticCanonicalLogEqual(canonical[index], proof.Logs[index]) || !finalSemanticCanonicalLogEqual(canonical[index], logs[index]) {
			return errors.New("historical coordinator captured receipt raw log differs from replay artifact")
		}
	}
	return nil
}

// Locates exactly one finalized journal entry and one later verified
// postcondition for a sealed historical receipt. A sibling retry, different
// intent, or unapproved predecessor plan is never selected by iteration.
func finalHistoricalCoordinatorJournalArtifactAction(evidence *FinalSemanticEvidence, current, plan *SetupPlan, entries []JournalEntry, row *FinalHistoricalCoordinatorReceiptEvidence) (Action, JournalEntry, JournalEntry, error) {
	if evidence == nil || current == nil || plan == nil || row == nil {
		return Action{}, JournalEntry{}, JournalEntry{}, errors.New("historical coordinator journal identity is unavailable")
	}
	var finalized *JournalEntry
	for index := range entries {
		entry := &entries[index]
		if entry.Stage != StageFinalized || !strings.EqualFold(entry.TransactionHash, row.Receipt.TransactionHash) {
			continue
		}
		if entry.DeploymentID != evidence.DeploymentID || !strings.EqualFold(entry.PlanHash, row.PlanHash) || entry.ActionID != row.ActionID || !strings.EqualFold(entry.IntentHash, row.IntentHash) || entry.BlockNumber != row.Receipt.Block.Number || !strings.EqualFold(entry.BlockHash, row.Receipt.Block.Hash) {
			return Action{}, JournalEntry{}, JournalEntry{}, errors.New("finalized transaction coordinates differ from the sealed receipt")
		}
		if finalized != nil {
			return Action{}, JournalEntry{}, JournalEntry{}, errors.New("receipt has multiple finalized journal entries")
		}
		finalized = entry
	}
	if finalized == nil || !current.allowedPlanHashes()[finalized.PlanHash] || !strings.EqualFold(plan.PlanHash, finalized.PlanHash) {
		return Action{}, JournalEntry{}, JournalEntry{}, errors.New("receipt has no finalized action in the approved plan lineage")
	}
	action, err := exactPlanActionByID(plan, finalized.ActionID)
	if err != nil || action.Kind != "evm-transaction" || !actionAcceptsIntent(action, finalized.IntentHash) {
		return Action{}, JournalEntry{}, JournalEntry{}, stateMismatchError(err, "finalized historical coordinator action is not approved")
	}
	var verified *JournalEntry
	for index := range entries {
		entry := &entries[index]
		if entry.Stage != StageVerified || entry.Sequence <= finalized.Sequence || entry.DeploymentID != finalized.DeploymentID || entry.PlanHash != finalized.PlanHash || entry.ActionID != finalized.ActionID || entry.IntentHash != finalized.IntentHash {
			continue
		}
		if verified != nil {
			return Action{}, JournalEntry{}, JournalEntry{}, errors.New("finalized action has multiple verified postconditions")
		}
		verified = entry
	}
	if verified == nil {
		return Action{}, JournalEntry{}, JournalEntry{}, errors.New("finalized action has no verified postcondition")
	}
	path, err := postconditionRelativePath(verified.PlanHash, verified.ActionID)
	if err != nil || verified.PostconditionPath != path {
		return Action{}, JournalEntry{}, JournalEntry{}, stateMismatchError(err, "verified postcondition path is not canonical")
	}
	return action, *finalized, *verified, nil
}

// Decodes the complete transaction/receipt artifact and compares every field
// directly with the signed row. Canonical log ordering is part of the check,
// preventing an omitted secondary emitter from being masked by a log hash.
func finalHistoricalCoordinatorReceiptArtifactTransaction(row *FinalHistoricalCoordinatorReceiptEvidence, data []byte) (FinalCollectedEVMTransaction, []finalCanonicalEVMLog, error) {
	if row == nil || len(data) == 0 {
		return FinalCollectedEVMTransaction{}, nil, errors.New("historical coordinator receipt artifact is empty")
	}
	var artifact finalHistoricalCoordinatorReceiptArtifact
	if err := decodeStrictJSONBytes(data, &artifact); err != nil {
		return FinalCollectedEVMTransaction{}, nil, err
	}
	transaction := FinalCollectedEVMTransaction{
		TransactionHash: artifact.TransactionHash, Block: artifact.Block, From: artifact.From, To: artifact.To, Input: artifact.Input, ValueWei: artifact.ValueWei,
	}
	if artifact.Status != row.Receipt.Status || transaction.TransactionHash != row.Receipt.TransactionHash || transaction.Block != row.Receipt.Block || transaction.From != row.TransactionFrom || transaction.To != row.TransactionTo || transaction.Input != row.TransactionInput || transaction.ValueWei != row.TransactionValueWei {
		return FinalCollectedEVMTransaction{}, nil, errors.New("historical coordinator transaction artifact differs from its sealed row")
	}
	canonical, err := finalCanonicalizeLogs(artifact.Logs)
	if err != nil || len(canonical) != len(artifact.Logs) {
		return FinalCollectedEVMTransaction{}, nil, stateMismatchError(err, "historical coordinator receipt logs are not canonical")
	}
	for index := range canonical {
		if !finalSemanticCanonicalLogEqual(canonical[index], artifact.Logs[index]) || artifact.Logs[index].TransactionHash != row.Receipt.TransactionHash || artifact.Logs[index].BlockNumber != row.Receipt.Block.Number || artifact.Logs[index].BlockHash != row.Receipt.Block.Hash {
			return FinalCollectedEVMTransaction{}, nil, errors.New("historical coordinator receipt log differs from its sealed coordinates")
		}
	}
	logsHash, err := finalCanonicalReceiptLogsHash(canonical)
	if err != nil || !strings.EqualFold(logsHash, row.Receipt.LogsHash) {
		return FinalCollectedEVMTransaction{}, nil, stateMismatchError(err, "historical coordinator receipt artifact logs differ from its sealed hash")
	}
	return transaction, append([]finalCanonicalEVMLog(nil), canonical...), nil
}
