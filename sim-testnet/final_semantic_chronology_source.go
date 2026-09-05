package main

// This unit projects carried coordinator mutations only from the sealed
// archive. It joins a receipt to an authenticated predecessor plan, journal
// finalization, postcondition, and captured transaction envelope before any
// public RPC request is allowed to replay it.

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Holds the authenticated input indexes needed to turn a carried receipt into
// a closed predecessor-plan proof. Its maps are immutable after construction,
// keeping source projection independent of archive directory traversal order.
type finalHistoricalCoordinatorSource struct {
	archive       *finalSemanticArchive
	evidence      *FinalSemanticEvidence
	chain         *FinalCollectedChainSnapshot
	events        *finalSemanticEventIndex
	current       *SetupPlan
	deployment    *ContractDeployment
	policy        *protocol.Policy
	plans         map[string]*SetupPlan
	planBytes     map[string][]byte
	entries       []JournalEntry
	journalBytes  []byte
	transactions  map[string]FinalCollectedEVMTransaction
	planArtifacts map[string]FinalArtifactLocator
	postArtifacts map[string]FinalArtifactLocator
}

// Identifies a finalized predecessor-plan call whose approved target is the
// coordinator proxy. This narrow record keeps target discovery independent of
// semantic receipt families, so setup mutations cannot disappear merely
// because no later business-domain projection happened to reference them.
type finalHistoricalCoordinatorJournalAction struct {
	plan   *SetupPlan
	action Action
	entry  JournalEntry
}

// Retains the entire finalized transaction boundary for a carried mutation.
// The source cannot reuse the narrower ordinary receipt proof because this
// replay must bind sender, calldata, value, and every receipt emitter.
type finalHistoricalCoordinatorReceiptArtifact struct {
	Status          string                 `json:"status"`
	TransactionHash string                 `json:"transaction_hash"`
	Block           ChainHead              `json:"block"`
	From            string                 `json:"from"`
	To              string                 `json:"to"`
	Input           string                 `json:"input"`
	ValueWei        string                 `json:"value_wei"`
	Logs            []finalCanonicalEVMLog `json:"logs"`
}

// Builds a strict predecessor-plan and journal index. Historical plans are
// accepted only when the current approved plan explicitly retains their hash.
func newFinalHistoricalCoordinatorSource(archive *finalSemanticArchive, evidence *FinalSemanticEvidence, chain *FinalCollectedChainSnapshot, events *finalSemanticEventIndex) (*finalHistoricalCoordinatorSource, error) {
	if archive == nil || evidence == nil || chain == nil || events == nil {
		return nil, errors.New("historical coordinator source inputs are incomplete")
	}
	currentBytes, _, err := archive.file("launch-foundation/plan.json")
	if err != nil {
		return nil, err
	}
	current, err := decodePersistedPlanBytes(currentBytes)
	if err != nil || current.Schema != currentSetupPlanSchema || current.PlanHash != evidence.PlanHash || current.DeploymentID != evidence.DeploymentID || current.ChainID != evidence.ChainID || current.Netuid != evidence.Netuid {
		return nil, stateMismatchError(err, "historical coordinator current plan differs from semantic identity")
	}
	deployment, err := finalHistoricalCoordinatorLiveDeployment(archive, evidence, chain, current)
	if err != nil {
		return nil, err
	}
	journalBytes, _, err := archive.file("launch-foundation/journal.jsonl")
	if err != nil {
		return nil, err
	}
	entries, err := decodeFinalSemanticJournalBytes(journalBytes)
	if err != nil {
		return nil, err
	}
	policyBytes, _, err := archive.file(archive.collected.Policy.URI)
	if err != nil {
		return nil, err
	}
	policy, err := verifyFinalPolicyArtifact(evidence, policyBytes)
	if err != nil {
		return nil, err
	}
	result := &finalHistoricalCoordinatorSource{
		archive: archive, evidence: evidence, chain: chain, events: events, current: current, deployment: deployment,
		policy: policy,
		plans:  map[string]*SetupPlan{current.PlanHash: current}, planBytes: map[string][]byte{current.PlanHash: append([]byte(nil), currentBytes...)},
		entries: entries, journalBytes: append([]byte(nil), journalBytes...), transactions: make(map[string]FinalCollectedEVMTransaction),
		planArtifacts: make(map[string]FinalArtifactLocator), postArtifacts: make(map[string]FinalArtifactLocator),
	}
	names, err := finalHistoricalCoordinatorPlanHistoryNames(archive.files)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		data := archive.files[name]
		plan, decodeErr := decodePersistedPlanBytes(data)
		if decodeErr != nil {
			return nil, fmt.Errorf("decode historical coordinator plan %s: %w", name, decodeErr)
		}
		planHash := strings.ToLower(plan.PlanHash)
		wantName := filepath.ToSlash(filepath.Join("plan-history", stringsTrim0x(planHash)+".json"))
		if name != wantName || !current.allowedPlanHashes()[plan.PlanHash] {
			return nil, fmt.Errorf("historical coordinator plan %s is outside the approved lineage", name)
		}
		if prior := result.plans[plan.PlanHash]; prior != nil {
			if !finalJSONEqual(prior, plan) || !bytes.Equal(result.planBytes[plan.PlanHash], data) {
				return nil, fmt.Errorf("historical coordinator plan %s has conflicting archived copies", plan.PlanHash)
			}
			continue
		}
		result.plans[plan.PlanHash] = plan
		result.planBytes[plan.PlanHash] = append([]byte(nil), data...)
	}
	for _, hash := range current.PriorPlanHashes {
		if result.plans[hash] == nil {
			return nil, fmt.Errorf("historical coordinator predecessor plan %s is absent", hash)
		}
	}
	for _, transaction := range chain.EVMTransactions {
		if prior, found := result.transactions[transaction.TransactionHash]; found && prior != transaction {
			return nil, fmt.Errorf("historical coordinator transaction %s is duplicated", transaction.TransactionHash)
		}
		result.transactions[transaction.TransactionHash] = transaction
	}
	if err := result.verifyReleaseCaptureCensus(); err != nil {
		return nil, err
	}
	return result, nil
}

// Takes the deployment checkpoint from the closed public manifest, rather
// than from a persisted plan. Plans deliberately retain deploy_block zero so
// a revision can be prepared before any transaction is broadcast; the public
// manifest is the authenticated source of the live block boundary.
func finalHistoricalCoordinatorLiveDeployment(archive *finalSemanticArchive, evidence *FinalSemanticEvidence, chain *FinalCollectedChainSnapshot, plan *SetupPlan) (*ContractDeployment, error) {
	if archive == nil || evidence == nil || chain == nil || plan == nil {
		return nil, errors.New("historical coordinator live deployment inputs are incomplete")
	}
	data, _, err := archive.file("launch-foundation/public.json")
	if err != nil {
		return nil, err
	}
	var public PublicDeploymentManifest
	if err := decodeStrictJSONBytes(data, &public); err != nil {
		return nil, fmt.Errorf("decode historical coordinator public deployment: %w", err)
	}
	if public.Schema != "urnetwork-sim-public-deployment-v1" || public.Contracts == nil || public.DeploymentID != evidence.DeploymentID || public.Contracts.DeploymentID != evidence.DeploymentID || public.ChainID != evidence.ChainID || public.Netuid != evidence.Netuid || public.PlanHash != evidence.PlanHash || public.Contracts.DeployBlock == 0 || public.Contracts.DeployBlock != chain.CurrentReleaseFromBlock || chain.DeploymentID != evidence.DeploymentID {
		return nil, errors.New("historical coordinator public deployment differs from the closed campaign")
	}
	deployment := *public.Contracts
	for label, values := range map[string][3]common.Address{
		"coordinator proxy": {deployment.CoordinatorProxy, plan.Deployment.CoordinatorProxy, common.HexToAddress(evidence.Deployment.CoordinatorProxy)},
		"settlement vault":  {deployment.SettlementVault, plan.Deployment.SettlementVault, common.HexToAddress(evidence.Deployment.SettlementVault)},
		"reserve sink":      {deployment.ReserveSink, plan.Deployment.ReserveSink, common.HexToAddress(evidence.Deployment.ReserveSink)},
	} {
		if values[0] == (common.Address{}) || values[0] != values[1] || values[0] != values[2] {
			return nil, fmt.Errorf("historical coordinator public %s differs from approved plan/evidence", label)
		}
	}
	return &deployment, nil
}

// Accepts only direct canonical plan files under the closed plan-history
// bundle. Ignoring an auxiliary, nested, or malformed entry would leave an
// unreviewed predecessor input beside the exact lineage selected below.
func finalHistoricalCoordinatorPlanHistoryNames(files map[string][]byte) ([]string, error) {
	names := make([]string, 0)
	for name := range files {
		if name == "plan-history" {
			return nil, errors.New("historical coordinator plan-history root is not a plan artifact")
		}
		if !strings.HasPrefix(name, "plan-history/") {
			continue
		}
		relative := strings.TrimPrefix(name, "plan-history/")
		if relative == "" || strings.Contains(relative, "/") || !strings.HasSuffix(relative, ".json") {
			return nil, fmt.Errorf("historical coordinator plan-history path %q is not canonical", name)
		}
		if err := requireFinalHex32("historical coordinator plan-history filename", "0x"+strings.TrimSuffix(relative, ".json")); err != nil {
			return nil, stateMismatchError(err, "historical coordinator plan-history path %q is invalid", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Selects every successful predecessor-plan transaction which dispatches to
// its own coordinator proxy before the signed campaign begins. The selector
// intentionally discovers the reviewed target graph rather than maintaining
// a guessed whitelist of action IDs; an unsupported new proxy method fails
// later during exact ABI replay instead of being silently omitted.
func finalHistoricalCoordinatorJournalActions(evidence *FinalSemanticEvidence, current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry) (map[string]finalHistoricalCoordinatorJournalAction, error) {
	if evidence == nil || current == nil || len(plans) == 0 || evidence.EVMCampaignStartHead.Number < 2 {
		return nil, errors.New("historical coordinator journal action inputs are incomplete")
	}
	allowed := current.allowedPlanHashes()
	result := make(map[string]finalHistoricalCoordinatorJournalAction)
	for index := range entries {
		entry := entries[index]
		if entry.Stage != StageFinalized || entry.BlockNumber == 0 || entry.BlockNumber >= evidence.EVMCampaignStartHead.Number {
			continue
		}
		planHash := strings.ToLower(entry.PlanHash)
		if !allowed[entry.PlanHash] || planHash != entry.PlanHash {
			continue
		}
		plan := plans[planHash]
		if plan == nil || plan.PlanHash != entry.PlanHash {
			return nil, fmt.Errorf("historical coordinator finalized plan %s is absent or noncanonical", entry.PlanHash)
		}
		action, err := exactPlanActionByID(plan, entry.ActionID)
		if err != nil || action.Kind != "evm-transaction" || !actionAcceptsIntent(action, entry.IntentHash) {
			return nil, stateMismatchError(err, "historical coordinator finalized action %s is not approved", entry.ActionID)
		}
		if !common.IsHexAddress(action.Target) || common.HexToAddress(action.Target) != plan.Deployment.CoordinatorProxy {
			continue
		}
		if entry.DeploymentID != evidence.DeploymentID || entry.TransactionHash != strings.ToLower(entry.TransactionHash) || entry.BlockHash != strings.ToLower(entry.BlockHash) || requireFinalHex32("historical coordinator journal transaction", entry.TransactionHash) != nil || requireFinalHex32("historical coordinator journal block", entry.BlockHash) != nil {
			return nil, fmt.Errorf("historical coordinator finalized action %s has noncanonical transaction coordinates", entry.ActionID)
		}
		key := entry.TransactionHash
		candidate := finalHistoricalCoordinatorJournalAction{plan: plan, action: action, entry: entry}
		if prior, found := result[key]; found {
			if prior.plan.PlanHash != candidate.plan.PlanHash || !finalJSONEqual(prior.action, candidate.action) || !finalJSONEqual(prior.entry, candidate.entry) {
				return nil, fmt.Errorf("historical coordinator transaction %s has conflicting finalized actions", entry.TransactionHash)
			}
			continue
		}
		result[key] = candidate
	}
	return result, nil
}

// Recomputes the live EVM query boundary from the authenticated active plan,
// every explicitly retained predecessor, and the sealed journal. The captured
// address set cannot contain an injected foreign emitter, omit a retired
// custody contract, or silently start after a carried action.
func (self *finalHistoricalCoordinatorSource) verifyReleaseCaptureCensus() error {
	if self == nil || self.current == nil || self.deployment == nil || self.chain == nil {
		return errors.New("historical coordinator release census inputs are incomplete")
	}
	batcher, err := finalCanonicalAddress(self.chain.FleetBatcher)
	if err != nil || batcher != self.chain.FleetBatcher {
		return stateMismatchError(err, "historical coordinator fleet batcher is not canonical")
	}
	census, err := finalCaptureReleaseContractCensusForLineage(self.current, self.deployment, common.HexToAddress(batcher), self.plans, self.entries)
	if err != nil {
		return err
	}
	if self.chain.EVMFromBlock != census.fromBlock || self.chain.CurrentReleaseFromBlock != self.deployment.DeployBlock || !slices.Equal(self.chain.CurrentReleaseAddresses, census.currentAddresses) || !slices.Equal(self.chain.ReleaseContractAddresses, census.releaseAddresses) {
		return errors.New("historical coordinator release census differs from authenticated plan lineage")
	}
	return nil
}

// Unions ordinary economic receipts with all discovered coordinator-targeted
// setup mutations. An ordinary receipt keeps its existing proof locator;
// a setup-only receipt receives a fresh exact raw-log proof before the row is
// materialized, preventing a target-only action from depending on a loose
// journal coordinate.
func (self *finalHistoricalCoordinatorSource) historicalReceipts() (map[string]FinalEVMReceipt, error) {
	if self == nil || self.archive == nil || self.evidence == nil || self.events == nil {
		return nil, errors.New("historical coordinator receipt source is unavailable")
	}
	result, err := finalSemanticUniqueCarriedEVMReceipts(self.evidence)
	if err != nil {
		return nil, err
	}
	targets, err := finalHistoricalCoordinatorJournalActions(self.evidence, self.current, self.plans, self.entries)
	if err != nil {
		return nil, err
	}
	for transactionHash, target := range targets {
		logs := self.events.byTx[transactionHash]
		if len(logs) == 0 {
			return nil, fmt.Errorf("historical coordinator target action %s has no captured logs", target.action.ID)
		}
		if existing, found := result[transactionHash]; found {
			if existing.Status != "success" || existing.Block.Number != target.entry.BlockNumber || !strings.EqualFold(existing.Block.Hash, target.entry.BlockHash) {
				return nil, fmt.Errorf("historical coordinator target action %s conflicts with its semantic receipt", target.action.ID)
			}
			continue
		}
		receipt, receiptErr := self.archive.receiptFromIndex(self.events, finalSemanticEvent{Log: logs[0]}, "historical-coordinator-"+stringsTrim0x(transactionHash))
		if receiptErr != nil || receipt.Status != "success" || receipt.Block.Number != target.entry.BlockNumber || !strings.EqualFold(receipt.Block.Hash, target.entry.BlockHash) {
			return nil, stateMismatchError(receiptErr, "historical coordinator target action %s receipt differs from finalized journal", target.action.ID)
		}
		result[transactionHash] = receipt
	}
	return result, nil
}

// Couples an approved action to the sole verified journal record and durable
// postcondition that close its execution. It is used for oracle handoff
// ordering, where the await action intentionally has no transaction receipt.
type finalHistoricalCoordinatorVerifiedAction struct {
	plan   *SetupPlan
	action Action
	entry  JournalEntry
	record *ActionPostcondition
}

// Resolves one unique verified action across the authenticated current and
// predecessor lineage. Ambiguous revisions are refused because choosing the
// latest matching action would make chronology depend on archive iteration.
func (self *finalHistoricalCoordinatorSource) verifiedAction(actionID string) (finalHistoricalCoordinatorVerifiedAction, error) {
	if self == nil || self.archive == nil || actionID == "" {
		return finalHistoricalCoordinatorVerifiedAction{}, errors.New("historical coordinator verified action source is unavailable")
	}
	planHashes := make([]string, 0, len(self.plans))
	for planHash := range self.plans {
		planHashes = append(planHashes, planHash)
	}
	sort.Strings(planHashes)
	var result *finalHistoricalCoordinatorVerifiedAction
	for _, planHash := range planHashes {
		plan := self.plans[planHash]
		action, actionErr := exactPlanActionByID(plan, actionID)
		if actionErr != nil {
			continue
		}
		var verified *JournalEntry
		for index := range self.entries {
			entry := &self.entries[index]
			if entry.Stage != StageVerified || entry.DeploymentID != self.evidence.DeploymentID || entry.PlanHash != plan.PlanHash || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash {
				continue
			}
			if verified != nil {
				return finalHistoricalCoordinatorVerifiedAction{}, fmt.Errorf("historical coordinator action %s has multiple verified records", actionID)
			}
			verified = entry
		}
		if verified == nil {
			continue
		}
		path, pathErr := postconditionRelativePath(verified.PlanHash, verified.ActionID)
		if pathErr != nil || verified.PostconditionPath != path {
			return finalHistoricalCoordinatorVerifiedAction{}, stateMismatchError(pathErr, "historical coordinator action %s postcondition path is invalid", actionID)
		}
		data, _, dataErr := self.archive.file(path)
		if dataErr != nil {
			return finalHistoricalCoordinatorVerifiedAction{}, dataErr
		}
		record, decodeErr := decodeFinalActionPostconditionV4(data)
		if decodeErr != nil || record.DeploymentID != self.evidence.DeploymentID || record.PlanHash != verified.PlanHash || record.ActionID != verified.ActionID || record.IntentHash != verified.IntentHash {
			return finalHistoricalCoordinatorVerifiedAction{}, stateMismatchError(decodeErr, "historical coordinator action %s postcondition identity differs", actionID)
		}
		hash, hashErr := canonicalHashHex(record)
		if hashErr != nil || !strings.EqualFold(hash, verified.PostconditionHash) {
			return finalHistoricalCoordinatorVerifiedAction{}, stateMismatchError(hashErr, "historical coordinator action %s postcondition hash differs", actionID)
		}
		candidate := &finalHistoricalCoordinatorVerifiedAction{plan: plan, action: action, entry: *verified, record: record}
		if result != nil {
			return finalHistoricalCoordinatorVerifiedAction{}, fmt.Errorf("historical coordinator action %s is ambiguous across approved revisions", actionID)
		}
		result = candidate
	}
	if result == nil {
		return finalHistoricalCoordinatorVerifiedAction{}, fmt.Errorf("historical coordinator action %s has no verified record", actionID)
	}
	return *result, nil
}

// Captures a finalized EVM transaction's canonical chain order. Receipt
// blocks alone are insufficient when an activation and a fleet batch share a
// block, so the immutable transaction index is retained as the tie breaker.
type finalHistoricalCoordinatorTransactionPosition struct {
	block            ChainHead
	transactionIndex uint64
	transactionHash  string
}

const finalHistoricalCoordinatorOracleWindowSchema = "urnetwork-final-historical-coordinator-oracle-window-v2"

// Records one immutable oracle-handoff action together with its finalized
// transaction where applicable and the exact verified postcondition. The
// await-restored read has no transaction but remains mandatory because it is
// the only proof that the temporary oracle was actually restored.
type finalHistoricalCoordinatorOracleWindowAction struct {
	PlanHash         string              `json:"plan_hash"`
	ActionID         string              `json:"action_id"`
	IntentHash       string              `json:"intent_hash"`
	Finalized        *JournalEntry       `json:"finalized,omitempty"`
	Verified         JournalEntry        `json:"verified"`
	Postcondition    ActionPostcondition `json:"postcondition"`
	TransactionIndex uint64              `json:"transaction_index,omitempty"`
}

// Retains the narrow ordering coordinate for one generation-two batch. The
// detailed fleet evidence owns its calldata and events; this projection only
// binds that already-proven write into the temporary-oracle interval.
type finalHistoricalCoordinatorOracleWindowWrite struct {
	Action           FinalFleetGenerationActionEvidence `json:"action"`
	Receipt          FinalEVMReceipt                    `json:"receipt"`
	BatcherAddress   string                             `json:"batcher_address"`
	Finalized        JournalEntry                       `json:"finalized"`
	TransactionIndex uint64                             `json:"transaction_index"`
}

// Seals the temporary fleet refresh window in one content-addressed object.
// This keeps the postcondition-only await-restored action reviewable after
// source collection and makes ordering tamper evident without duplicating
// every batch's full receipt payload.
type finalHistoricalCoordinatorOracleWindowArtifact struct {
	Schema              string                                        `json:"schema"`
	CoordinatorProxy    string                                        `json:"coordinator_proxy"`
	Activation          finalHistoricalCoordinatorOracleWindowAction  `json:"activation"`
	AwaitActive         finalHistoricalCoordinatorOracleWindowAction  `json:"await_active"`
	Restore             finalHistoricalCoordinatorOracleWindowAction  `json:"restore"`
	AwaitRestored       finalHistoricalCoordinatorOracleWindowAction  `json:"await_restored"`
	GenerationTwoWrites []finalHistoricalCoordinatorOracleWindowWrite `json:"generation_two_writes"`
}

// Extracts one observer-pair value only when both retained maps report the
// same canonical active oracle. This is intentionally separate from the
// planned target check: the public verifier must prove the live value itself.
func finalFleetRefreshOracleCheckpointOracle(record ActionPostcondition) (string, error) {
	operational, operationalOK := record.Observed["active_oracle"].(string)
	independent, independentOK := record.IndependentObserved["active_oracle"].(string)
	operationalCanonical, operationalErr := finalCanonicalAddress(operational)
	independentCanonical, independentErr := finalCanonicalAddress(independent)
	if !operationalOK || !independentOK || operationalErr != nil || independentErr != nil || operationalCanonical != operational || independentCanonical != independent || operationalCanonical == "0x0000000000000000000000000000000000000000" || operationalCanonical != independentCanonical {
		return "", stateMismatchError(errors.Join(operationalErr, independentErr), "historical fleet refresh active-oracle observations disagree")
	}
	return operationalCanonical, nil
}

// Derives the four immutable public-query obligations from the sealed action
// postconditions. The artifact already binds action targets and observer maps;
// this compact projection lets public replay prove those observations directly.
func finalFleetRefreshOracleWindowCheckpointsFromArtifact(value finalHistoricalCoordinatorOracleWindowArtifact) (FinalFleetRefreshOracleWindowCheckpoints, error) {
	activeOracle, activeErr := finalFleetRefreshOracleCheckpointOracle(value.AwaitActive.Postcondition)
	restoredOracle, restoredErr := finalFleetRefreshOracleCheckpointOracle(value.AwaitRestored.Postcondition)
	if activeErr != nil || restoredErr != nil {
		return FinalFleetRefreshOracleWindowCheckpoints{}, errors.Join(activeErr, restoredErr)
	}
	result := FinalFleetRefreshOracleWindowCheckpoints{
		CoordinatorProxy:         value.CoordinatorProxy,
		AwaitActiveOperational:   FinalFleetRefreshOracleCheckpointEvidence{Head: value.AwaitActive.Postcondition.EVMFinalized, Oracle: activeOracle},
		AwaitActiveIndependent:   FinalFleetRefreshOracleCheckpointEvidence{Head: value.AwaitActive.Postcondition.IndependentEVMFinalized, Oracle: activeOracle},
		AwaitRestoredOperational: FinalFleetRefreshOracleCheckpointEvidence{Head: value.AwaitRestored.Postcondition.EVMFinalized, Oracle: restoredOracle},
		AwaitRestoredIndependent: FinalFleetRefreshOracleCheckpointEvidence{Head: value.AwaitRestored.Postcondition.IndependentEVMFinalized, Oracle: restoredOracle},
	}
	if err := verifyFinalFleetRefreshOracleWindowCheckpoints(result); err != nil {
		return FinalFleetRefreshOracleWindowCheckpoints{}, err
	}
	return result, nil
}

// Computes the canonical position from all captured logs for one transaction.
// A receipt whose logs disagree on block or transaction index cannot provide
// an ordering witness for a multi-contract coordinator side effect.
func (self *finalHistoricalCoordinatorSource) transactionPosition(entry JournalEntry) (finalHistoricalCoordinatorTransactionPosition, error) {
	if self == nil || self.events == nil || entry.Stage != StageFinalized || entry.TransactionHash == "" || entry.BlockNumber == 0 {
		return finalHistoricalCoordinatorTransactionPosition{}, errors.New("historical coordinator transaction position is incomplete")
	}
	logs := self.events.byTx[entry.TransactionHash]
	if len(logs) == 0 {
		return finalHistoricalCoordinatorTransactionPosition{}, fmt.Errorf("historical coordinator transaction %s has no captured logs", entry.TransactionHash)
	}
	position := finalHistoricalCoordinatorTransactionPosition{
		block:            ChainHead{Number: entry.BlockNumber, Hash: entry.BlockHash},
		transactionIndex: logs[0].TransactionIndex,
		transactionHash:  entry.TransactionHash,
	}
	for _, log := range logs {
		if log.BlockNumber != position.block.Number || !strings.EqualFold(log.BlockHash, position.block.Hash) || log.TransactionHash != position.transactionHash || log.TransactionIndex != position.transactionIndex {
			return finalHistoricalCoordinatorTransactionPosition{}, fmt.Errorf("historical coordinator transaction %s logs disagree on canonical position", entry.TransactionHash)
		}
	}
	return position, nil
}

// Resolves the one finalized transaction that the verified action closes.
// Verified journal rows deliberately carry only the postcondition locator, so
// their empty transaction fields must never be used as an EVM census key.
func (self *finalHistoricalCoordinatorSource) finalizedAction(verified finalHistoricalCoordinatorVerifiedAction) (JournalEntry, error) {
	if self == nil || verified.plan == nil || verified.action.ID == "" || verified.entry.Stage != StageVerified {
		return JournalEntry{}, errors.New("historical coordinator verified action is incomplete")
	}
	var result *JournalEntry
	for index := range self.entries {
		entry := &self.entries[index]
		if entry.Stage != StageFinalized || entry.DeploymentID != self.evidence.DeploymentID || entry.PlanHash != verified.plan.PlanHash || entry.ActionID != verified.action.ID || entry.IntentHash != verified.action.IntentHash || entry.Sequence >= verified.entry.Sequence {
			continue
		}
		if entry.TransactionHash == "" || entry.BlockNumber == 0 || entry.BlockHash == "" {
			return JournalEntry{}, fmt.Errorf("historical coordinator action %s has an incomplete finalized transaction", verified.action.ID)
		}
		if result != nil {
			return JournalEntry{}, fmt.Errorf("historical coordinator action %s has multiple finalized transactions", verified.action.ID)
		}
		result = entry
	}
	if result == nil {
		return JournalEntry{}, fmt.Errorf("historical coordinator action %s has no finalized transaction", verified.action.ID)
	}
	return *result, nil
}

// Orders two immutable EVM transaction coordinates while refusing a shared
// block/index slot. A lexical transaction-hash tie breaker would manufacture
// an order for two incompatible receipts which claim the same chain position.
func finalHistoricalCoordinatorPositionBefore(left, right finalHistoricalCoordinatorTransactionPosition) (bool, error) {
	if left.block.Number != right.block.Number {
		return left.block.Number < right.block.Number, nil
	}
	if left.transactionIndex != right.transactionIndex {
		return left.transactionIndex < right.transactionIndex, nil
	}
	return false, fmt.Errorf("historical coordinator transactions %s and %s share block %d transaction index %d", left.transactionHash, right.transactionHash, left.block.Number, left.transactionIndex)
}

// Finds the sole finalized journal record for an already sealed generation
// write. The fleet lineage owns the detailed batch event proof; chronology
// only consumes its immutable ordering coordinate.
func (self *finalHistoricalCoordinatorSource) finalizedFleetGenerationWrite(write FinalFleetGenerationWriteEvidence) (JournalEntry, error) {
	if self == nil || write.Action.ActionID == "" || write.Action.PlanHash == "" || write.Action.IntentHash == "" || write.Receipt.TransactionHash == "" {
		return JournalEntry{}, errors.New("historical fleet generation write identity is incomplete")
	}
	var result *JournalEntry
	for index := range self.entries {
		entry := &self.entries[index]
		if entry.Stage != StageFinalized || entry.DeploymentID != self.evidence.DeploymentID || entry.PlanHash != write.Action.PlanHash || entry.ActionID != write.Action.ActionID || entry.IntentHash != write.Action.IntentHash || entry.TransactionHash != write.Receipt.TransactionHash {
			continue
		}
		if entry.BlockNumber != write.Receipt.Block.Number || !strings.EqualFold(entry.BlockHash, write.Receipt.Block.Hash) {
			return JournalEntry{}, fmt.Errorf("historical fleet generation write %s finalized coordinate differs", write.Action.ActionID)
		}
		if result != nil {
			return JournalEntry{}, fmt.Errorf("historical fleet generation write %s has multiple finalized records", write.Action.ActionID)
		}
		result = entry
	}
	if result == nil {
		return JournalEntry{}, fmt.Errorf("historical fleet generation write %s has no finalized record", write.Action.ActionID)
	}
	return *result, nil
}

// Proves the exact temporary-oracle window around every generation-two batch.
// The shared supersession validator binds both independent postconditions;
// this source-level join additionally proves all generation-two writes are
// ordered after activation and before restoration at canonical transaction
// positions without duplicating the batcher's full receipt evidence.
func (self *finalHistoricalCoordinatorSource) fleetRefreshOracleWindow(rows []FinalHistoricalCoordinatorReceiptEvidence) (finalHistoricalCoordinatorOracleWindowArtifact, error) {
	if self == nil || self.evidence == nil || self.evidence.FleetGeneration == nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, errors.New("historical fleet refresh oracle evidence is unavailable")
	}
	activate, err := self.verifiedAction("fleet.refresh.oracle-activate")
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	awaitActive, err := self.verifiedAction("fleet.refresh.oracle-await-active")
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	restore, err := self.verifiedAction("fleet.refresh.oracle-restore")
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	awaitRestored, err := self.verifiedAction("fleet.refresh.oracle-await-restored")
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	if err := validateFleetRefreshOraclePostconditionIdentity(activate.action, activate.entry, activate.record); err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, fmt.Errorf("historical fleet refresh activation: %w", err)
	}
	if err := validateFleetRefreshOracleSupersession(awaitActive.action, awaitActive.entry, awaitActive.record, restore.action, restore.entry, restore.record, awaitRestored.action, awaitRestored.entry, awaitRestored.record); err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, fmt.Errorf("historical fleet refresh oracle supersession: %w", err)
	}
	if awaitActive.entry.Sequence <= activate.entry.Sequence || awaitActive.record.EVMFinalized.Number < activate.record.EVMFinalized.Number || awaitActive.record.IndependentEVMFinalized.Number < activate.record.IndependentEVMFinalized.Number {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, errors.New("historical fleet refresh active-oracle checkpoint does not follow activation")
	}
	activateOracle, activateOracleErr := plannedFleetRefreshOracleTarget(activate.action)
	awaitActiveOracle, awaitActiveOracleErr := plannedFleetRefreshOracleTarget(awaitActive.action)
	if activateOracleErr != nil || awaitActiveOracleErr != nil || activateOracle != awaitActiveOracle {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, stateMismatchError(errors.Join(activateOracleErr, awaitActiveOracleErr), "historical fleet refresh activation and await-active oracle differ")
	}
	activateFinalized, err := self.finalizedAction(activate)
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	restoreFinalized, err := self.finalizedAction(restore)
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	targets, err := finalHistoricalCoordinatorJournalActions(self.evidence, self.current, self.plans, self.entries)
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	rowByTransaction := make(map[string]FinalHistoricalCoordinatorReceiptEvidence, len(rows))
	for _, row := range rows {
		rowByTransaction[row.Receipt.TransactionHash] = row
	}
	activateTarget, activateFound := targets[activateFinalized.TransactionHash]
	restoreTarget, restoreFound := targets[restoreFinalized.TransactionHash]
	if !activateFound || !restoreFound || activateTarget.action.ID != activate.action.ID || restoreTarget.action.ID != restore.action.ID || rowByTransaction[activateFinalized.TransactionHash].ActionID != activate.action.ID || rowByTransaction[restoreFinalized.TransactionHash].ActionID != restore.action.ID {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, fmt.Errorf("historical fleet refresh oracle receipts are absent from the target-derived census: activate=%s found=%t action=%q row=%q restore=%s found=%t action=%q row=%q", activateFinalized.TransactionHash, activateFound, activateTarget.action.ID, rowByTransaction[activateFinalized.TransactionHash].ActionID, restoreFinalized.TransactionHash, restoreFound, restoreTarget.action.ID, rowByTransaction[restoreFinalized.TransactionHash].ActionID)
	}
	activateRow := rowByTransaction[activateFinalized.TransactionHash]
	restoreRow := rowByTransaction[restoreFinalized.TransactionHash]
	coordinatorProxy, proxyErr := finalHistoricalCoordinatorOracleWindowProxy(activateTarget.action, restoreTarget.action, &activateRow, &restoreRow)
	if proxyErr != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, proxyErr
	}
	if activateTarget.entry.TransactionHash != activateFinalized.TransactionHash || restoreTarget.entry.TransactionHash != restoreFinalized.TransactionHash {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, errors.New("historical fleet refresh oracle target census differs from finalized journal")
	}
	activatePosition, err := self.transactionPosition(activateFinalized)
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	restorePosition, err := self.transactionPosition(restoreFinalized)
	if err != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, err
	}
	activationBeforeRestore, orderErr := finalHistoricalCoordinatorPositionBefore(activatePosition, restorePosition)
	if orderErr != nil {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, orderErr
	}
	if !activationBeforeRestore {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, errors.New("historical fleet refresh oracle restore is not after activation")
	}
	result := finalHistoricalCoordinatorOracleWindowArtifact{
		Schema:           finalHistoricalCoordinatorOracleWindowSchema,
		CoordinatorProxy: coordinatorProxy,
		Activation: finalHistoricalCoordinatorOracleWindowAction{
			PlanHash: activate.plan.PlanHash, ActionID: activate.action.ID, IntentHash: activate.action.IntentHash,
			Finalized: &activateFinalized, Verified: activate.entry, Postcondition: *activate.record, TransactionIndex: activatePosition.transactionIndex,
		},
		AwaitActive: finalHistoricalCoordinatorOracleWindowAction{
			PlanHash: awaitActive.plan.PlanHash, ActionID: awaitActive.action.ID, IntentHash: awaitActive.action.IntentHash,
			Verified: awaitActive.entry, Postcondition: *awaitActive.record,
		},
		Restore: finalHistoricalCoordinatorOracleWindowAction{
			PlanHash: restore.plan.PlanHash, ActionID: restore.action.ID, IntentHash: restore.action.IntentHash,
			Finalized: &restoreFinalized, Verified: restore.entry, Postcondition: *restore.record, TransactionIndex: restorePosition.transactionIndex,
		},
		AwaitRestored: finalHistoricalCoordinatorOracleWindowAction{
			PlanHash: awaitRestored.plan.PlanHash, ActionID: awaitRestored.action.ID, IntentHash: awaitRestored.action.IntentHash,
			Verified: awaitRestored.entry, Postcondition: *awaitRestored.record,
		},
		GenerationTwoWrites: make([]finalHistoricalCoordinatorOracleWindowWrite, 0, finalFleetGenerationBatchCount),
	}
	writeCount := 0
	for _, batch := range self.evidence.FleetGeneration.Batches {
		if batch.Generation != 2 {
			continue
		}
		if batch.BatchWrite == nil {
			return finalHistoricalCoordinatorOracleWindowArtifact{}, fmt.Errorf("historical fleet refresh generation-two batch %d has no write", batch.Batch)
		}
		batcher, batcherErr := finalCanonicalAddress(batch.BatchWrite.BatcherAddress)
		if batcherErr != nil || !strings.EqualFold(batcher, activateOracle.Hex()) {
			return finalHistoricalCoordinatorOracleWindowArtifact{}, stateMismatchError(batcherErr, "historical fleet refresh generation-two batch %d targets another batcher", batch.Batch)
		}
		entry, entryErr := self.finalizedFleetGenerationWrite(*batch.BatchWrite)
		if entryErr != nil {
			return finalHistoricalCoordinatorOracleWindowArtifact{}, entryErr
		}
		position, positionErr := self.transactionPosition(entry)
		if positionErr != nil {
			return finalHistoricalCoordinatorOracleWindowArtifact{}, positionErr
		}
		activationBeforeWrite, activationErr := finalHistoricalCoordinatorPositionBefore(activatePosition, position)
		writeBeforeRestore, restoreErr := finalHistoricalCoordinatorPositionBefore(position, restorePosition)
		if activationErr != nil || restoreErr != nil {
			return finalHistoricalCoordinatorOracleWindowArtifact{}, errors.Join(activationErr, restoreErr)
		}
		if !activationBeforeWrite || !writeBeforeRestore {
			return finalHistoricalCoordinatorOracleWindowArtifact{}, fmt.Errorf("historical fleet refresh generation-two batch %d is outside the oracle window", batch.Batch)
		}
		if position.block.Number <= awaitActive.record.EVMFinalized.Number || position.block.Number <= awaitActive.record.IndependentEVMFinalized.Number {
			return finalHistoricalCoordinatorOracleWindowArtifact{}, fmt.Errorf("historical fleet refresh generation-two batch %d does not follow the active-oracle checkpoint", batch.Batch)
		}
		if entry.Sequence <= awaitActive.entry.Sequence || entry.Sequence >= restoreFinalized.Sequence || entry.Sequence >= restore.entry.Sequence {
			return finalHistoricalCoordinatorOracleWindowArtifact{}, fmt.Errorf("historical fleet refresh generation-two batch %d is outside the verified oracle journal interval", batch.Batch)
		}
		result.GenerationTwoWrites = append(result.GenerationTwoWrites, finalHistoricalCoordinatorOracleWindowWrite{
			Action: batch.BatchWrite.Action, Receipt: batch.BatchWrite.Receipt, BatcherAddress: batch.BatchWrite.BatcherAddress, Finalized: entry, TransactionIndex: position.transactionIndex,
		})
		writeCount++
	}
	if writeCount != int(finalFleetGenerationBatchCount) {
		return finalHistoricalCoordinatorOracleWindowArtifact{}, fmt.Errorf("historical fleet refresh generation-two writes=%d, want %d", writeCount, finalFleetGenerationBatchCount)
	}
	return result, nil
}

// Rechecks the source-only form before the content-addressed window is
// emitted. The artifact verifier reconstructs the identical object later.
func (self *finalHistoricalCoordinatorSource) verifyFleetRefreshOracleWindow(rows []FinalHistoricalCoordinatorReceiptEvidence) error {
	_, err := self.fleetRefreshOracleWindow(rows)
	return err
}

// Builds all required carried-receipt rows after the ordinary semantic graph
// has been constructed. An empty set remains explicit so older source code
// cannot silently omit the new trust domain.
func (self *finalSemanticArchive) buildHistoricalCoordinatorReceipts(evidence *FinalSemanticEvidence, chain *FinalCollectedChainSnapshot, events *finalSemanticEventIndex) error {
	context, err := newFinalHistoricalCoordinatorSource(self, evidence, chain, events)
	if err != nil {
		return err
	}
	expected, err := context.historicalReceipts()
	if err != nil {
		return err
	}
	timeline, err := finalHistoricalCoordinatorBuildTimeline(evidence, context.current, context.plans, context.entries, context.events.byTx, context.chain.CoordinatorBaselines)
	if err != nil {
		return err
	}
	evidence.HistoricalCoordinatorTimeline = timeline.evidence()
	timelineValue, err := finalHistoricalCoordinatorTimelineArtifactFromSource(timeline, context.chain.CoordinatorBaselines, context.events.byTx)
	if err != nil {
		return err
	}
	timelineArtifact, err := self.derived("historical-coordinator-timeline", "historical-coordinator/timeline.json", timelineValue)
	if err != nil {
		return err
	}
	evidence.HistoricalCoordinatorTimelineArtifact = timelineArtifact
	journalArtifact, err := self.derivedBytes("historical-journal", "historical-coordinator/journal.jsonl", context.journalBytes)
	if err != nil {
		return err
	}
	rows := make([]FinalHistoricalCoordinatorReceiptEvidence, 0, len(expected))
	for _, receipt := range expected {
		row, buildErr := context.row(receipt, journalArtifact, timeline)
		if buildErr != nil {
			return buildErr
		}
		rows = append(rows, row)
	}
	canonicalizeFinalHistoricalCoordinatorReceipts(rows)
	evidence.HistoricalCoordinatorReceipts = rows
	window, err := context.fleetRefreshOracleWindow(rows)
	if err != nil {
		return err
	}
	checkpoints, err := finalFleetRefreshOracleWindowCheckpointsFromArtifact(window)
	if err != nil {
		return err
	}
	windowArtifact, err := self.derived("historical-fleet-refresh-oracle-window", "historical-coordinator/oracle-window.json", window)
	if err != nil {
		return err
	}
	evidence.FleetRefreshOracleWindow = FinalFleetRefreshOracleWindowEvidence{Artifact: windowArtifact, Checkpoints: checkpoints}
	return verifyFinalHistoricalCoordinatorReceipts(evidence)
}

// Projects one carried mutation from one exact finalized/verified journal pair
// and its sealed transaction/log envelope. Multiple retries or postconditions
// are refused rather than selected by order.
func (self *finalHistoricalCoordinatorSource) row(receipt FinalEVMReceipt, journalArtifact FinalArtifactLocator, timeline *finalHistoricalCoordinatorTimeline) (FinalHistoricalCoordinatorReceiptEvidence, error) {
	if self == nil || self.current == nil {
		return FinalHistoricalCoordinatorReceiptEvidence{}, errors.New("historical coordinator source is unavailable")
	}
	transaction, found := self.transactions[receipt.TransactionHash]
	if !found || transaction.Block != receipt.Block {
		return FinalHistoricalCoordinatorReceiptEvidence{}, fmt.Errorf("historical coordinator receipt %s has no captured transaction envelope", receipt.TransactionHash)
	}
	finalized, plan, action, verified, postcondition, postconditionBytes, err := self.journalMutation(receipt)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptEvidence{}, err
	}
	logs := self.events.byTx[receipt.TransactionHash]
	if len(logs) == 0 {
		return FinalHistoricalCoordinatorReceiptEvidence{}, fmt.Errorf("historical coordinator receipt %s has no captured logs", receipt.TransactionHash)
	}
	logsHash, err := finalCanonicalReceiptLogsHash(logs)
	if err != nil || !strings.EqualFold(logsHash, receipt.LogsHash) {
		return FinalHistoricalCoordinatorReceiptEvidence{}, stateMismatchError(err, "historical coordinator receipt %s logs differ from semantic evidence", receipt.TransactionHash)
	}
	emitters, err := finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptEvidence{}, err
	}
	if postcondition == nil || postcondition.PlanHash != finalized.PlanHash || postcondition.ActionID != finalized.ActionID || postcondition.IntentHash != finalized.IntentHash {
		return FinalHistoricalCoordinatorReceiptEvidence{}, errors.New("historical coordinator postcondition differs from finalized journal")
	}
	if err := verifyFinalHistoricalCoordinatorActionWithPostcondition(self.evidence, self.policy, postcondition, plan, action, receipt, transaction, logs, emitters); err != nil {
		return FinalHistoricalCoordinatorReceiptEvidence{}, err
	}
	receiptArtifact, err := self.receiptArtifact(receipt, transaction, logs)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptEvidence{}, err
	}
	planArtifact, err := self.planArtifact(plan.PlanHash)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptEvidence{}, err
	}
	postconditionArtifact, err := self.postconditionArtifact(verified, postconditionBytes)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptEvidence{}, err
	}
	runtime, err := timeline.receiptRuntime(plan, action, finalized, logs)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptEvidence{}, err
	}
	return FinalHistoricalCoordinatorReceiptEvidence{
		Receipt: receipt, ReceiptArtifact: receiptArtifact, PlanHash: strings.ToLower(plan.PlanHash), PlanArtifact: planArtifact, JournalArtifact: journalArtifact, PostconditionArtifact: postconditionArtifact,
		ActionID: action.ID, IntentHash: strings.ToLower(finalized.IntentHash), TransactionFrom: transaction.From, TransactionTo: transaction.To,
		TransactionInput: transaction.Input, TransactionValueWei: transaction.ValueWei, TransactionIndex: runtime.transactionIndex, Emitters: emitters,
		CoordinatorProxy: strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()), ExecutionHead: runtime.executionHead, ExecutionImplementation: runtime.execution.Implementation, ExecutionImplementationRuntimeHash: runtime.execution.RuntimeHash,
		CoordinatorImplementation: runtime.post.Implementation, CoordinatorImplementationSlot: erc1967ImplementationSlot,
		CoordinatorProxyRuntimeHash: runtime.proxyRuntimeHash, CoordinatorImplementationRuntimeHash: runtime.post.RuntimeHash,
	}, nil
}

// Materializes the full native JSON-RPC transaction/receipt projection used
// by a carried action. A content-addressed copy lets an offline reviewer
// reject a self-consistent but substituted row without contacting a node.
func (self *finalHistoricalCoordinatorSource) receiptArtifact(receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, logs []finalCanonicalEVMLog) (FinalArtifactLocator, error) {
	if self == nil || self.archive == nil || len(logs) == 0 || transaction.TransactionHash != receipt.TransactionHash || transaction.Block != receipt.Block {
		return FinalArtifactLocator{}, errors.New("historical coordinator receipt artifact inputs are incomplete")
	}
	value := finalHistoricalCoordinatorReceiptArtifact{
		Status: receipt.Status, TransactionHash: transaction.TransactionHash, Block: transaction.Block,
		From: transaction.From, To: transaction.To, Input: transaction.Input, ValueWei: transaction.ValueWei,
		Logs: append([]finalCanonicalEVMLog(nil), logs...),
	}
	return self.archive.derived("historical-coordinator-receipt", filepath.ToSlash(filepath.Join("historical-coordinator", "receipts", stringsTrim0x(receipt.TransactionHash)+".json")), value)
}

// Finds one source action with exact finalized coordinates and one subsequent
// verified postcondition. The journal hash chain was validated before this
// search, so sequence order cannot be synthesized by a loose JSONL scan.
func (self *finalHistoricalCoordinatorSource) journalMutation(receipt FinalEVMReceipt) (JournalEntry, *SetupPlan, Action, JournalEntry, *ActionPostcondition, []byte, error) {
	var finalized *JournalEntry
	for index := range self.entries {
		entry := &self.entries[index]
		if entry.Stage != StageFinalized || !strings.EqualFold(entry.TransactionHash, receipt.TransactionHash) {
			continue
		}
		if entry.BlockNumber != receipt.Block.Number || !strings.EqualFold(entry.BlockHash, receipt.Block.Hash) || entry.DeploymentID != self.evidence.DeploymentID {
			return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, errors.New("historical coordinator finalized journal coordinates differ from receipt")
		}
		if finalized != nil {
			return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, errors.New("historical coordinator receipt has multiple finalized journal entries")
		}
		finalized = entry
	}
	if finalized == nil || !self.current.allowedPlanHashes()[finalized.PlanHash] {
		return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, errors.New("historical coordinator receipt has no approved finalized journal action")
	}
	plan := self.plans[finalized.PlanHash]
	if plan == nil {
		return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, errors.New("historical coordinator finalized plan is absent")
	}
	action, err := exactPlanActionByID(plan, finalized.ActionID)
	if err != nil || action.Kind != "evm-transaction" || !actionAcceptsIntent(action, finalized.IntentHash) {
		return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, stateMismatchError(err, "historical coordinator finalized action is not approved")
	}
	var verified *JournalEntry
	for index := range self.entries {
		entry := &self.entries[index]
		if entry.Stage != StageVerified || entry.Sequence <= finalized.Sequence || entry.PlanHash != finalized.PlanHash || entry.ActionID != finalized.ActionID || entry.IntentHash != finalized.IntentHash {
			continue
		}
		if verified != nil {
			return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, errors.New("historical coordinator action has multiple verified postconditions")
		}
		verified = entry
	}
	if verified == nil {
		return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, errors.New("historical coordinator action has no verified postcondition")
	}
	path, pathErr := postconditionRelativePath(verified.PlanHash, verified.ActionID)
	if pathErr != nil || verified.PostconditionPath != path {
		return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, stateMismatchError(pathErr, "historical coordinator postcondition path is not canonical")
	}
	data, _, dataErr := self.archive.file(path)
	if dataErr != nil {
		return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, dataErr
	}
	postcondition, decodeErr := decodeFinalActionPostconditionV4(data)
	if decodeErr != nil || postcondition.DeploymentID != self.evidence.DeploymentID || postcondition.PlanHash != finalized.PlanHash || postcondition.ActionID != finalized.ActionID || postcondition.IntentHash != finalized.IntentHash {
		return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, stateMismatchError(decodeErr, "historical coordinator postcondition identity differs from journal")
	}
	postconditionHash, hashErr := canonicalHashHex(postcondition)
	if hashErr != nil || !strings.EqualFold(postconditionHash, verified.PostconditionHash) {
		return JournalEntry{}, nil, Action{}, JournalEntry{}, nil, nil, stateMismatchError(hashErr, "historical coordinator postcondition hash differs from journal")
	}
	return *finalized, plan, action, *verified, postcondition, data, nil
}

// Materializes one archived plan once, preserving the source bytes which were
// authenticated by that plan's own persisted hash rather than re-marshaling a
// possibly compatible Go struct.
func (self *finalHistoricalCoordinatorSource) planArtifact(planHash string) (FinalArtifactLocator, error) {
	if locator, found := self.planArtifacts[planHash]; found {
		return locator, nil
	}
	data := self.planBytes[planHash]
	if len(data) == 0 {
		return FinalArtifactLocator{}, errors.New("historical coordinator plan bytes are absent")
	}
	locator, err := self.archive.derivedBytes("historical-setup-plan", filepath.ToSlash(filepath.Join("historical-coordinator", "plans", stringsTrim0x(planHash)+".json")), data)
	if err != nil {
		return FinalArtifactLocator{}, err
	}
	self.planArtifacts[planHash] = locator
	return locator, nil
}

// Materializes one journal-authenticated postcondition once. The path itself
// stays in the signed journal artifact, while this locator seals exact bytes.
func (self *finalHistoricalCoordinatorSource) postconditionArtifact(entry JournalEntry, data []byte) (FinalArtifactLocator, error) {
	if locator, found := self.postArtifacts[entry.PostconditionPath]; found {
		return locator, nil
	}
	locator, err := self.archive.derivedBytes("historical-action-postcondition", filepath.ToSlash(filepath.Join("historical-coordinator", "postconditions", stringsTrim0x(entry.PlanHash), entry.ActionID+".json")), data)
	if err != nil {
		return FinalArtifactLocator{}, err
	}
	self.postArtifacts[entry.PostconditionPath] = locator
	return locator, nil
}

// Extracts the two executable hashes required at a carried proxy dispatch.
// They come from the archived deployment map, not from the current release
// roots which may intentionally describe a later coordinator implementation.
func finalHistoricalCoordinatorRuntimeHashes(plan *SetupPlan) (string, string, error) {
	if plan == nil || plan.Deployment.CoordinatorProxy == (common.Address{}) || plan.Deployment.CoordinatorImplementation == (common.Address{}) {
		return "", "", errors.New("historical coordinator deployment is incomplete")
	}
	proxy := strings.ToLower(plan.Deployment.RuntimeHashes[plan.Deployment.CoordinatorProxy.Hex()])
	implementation := strings.ToLower(plan.Deployment.RuntimeHashes[plan.Deployment.CoordinatorImplementation.Hex()])
	if err := requireFinalHex32("historical coordinator proxy runtime", proxy); err != nil {
		return "", "", err
	}
	if err := requireFinalHex32("historical coordinator implementation runtime", implementation); err != nil {
		return "", "", err
	}
	return proxy, implementation, nil
}

// Derives the complete release-emitter graph from every captured log in the
// transaction. A set rather than first-event lookup catches a missing vault or
// reserve side effect even when the coordinator event itself is intact.
func finalHistoricalCoordinatorEmitterGraph(logs []finalCanonicalEVMLog) ([]string, error) {
	if len(logs) == 0 {
		return nil, errors.New("historical coordinator receipt has no logs")
	}
	result := make([]string, 0, len(logs))
	for _, log := range logs {
		canonical, err := finalCanonicalAddress(log.Address)
		if err != nil || canonical != log.Address {
			return nil, stateMismatchError(err, "historical coordinator receipt emitter is invalid")
		}
		result = append(result, canonical)
	}
	sort.Strings(result)
	return slices.Compact(result), nil
}

// Validates a carried action without a policy artifact. Direct unit callers
// exercise registration/deposit/upgrade branches through this wrapper; the
// archive builder and artifact verifier always use the policy-aware variant.
func verifyFinalHistoricalCoordinatorAction(evidence *FinalSemanticEvidence, plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, logs []finalCanonicalEVMLog, emitters []string) error {
	return verifyFinalHistoricalCoordinatorActionWithPolicy(evidence, nil, plan, action, receipt, transaction, logs, emitters)
}

// Validates the transaction ABI boundary for every carried economic mutation
// that can be emitted before the signed campaign. Each branch names the full
// predecessor-plan graph, sender, value, action payload, and complete log set.
func verifyFinalHistoricalCoordinatorActionWithPolicy(evidence *FinalSemanticEvidence, policy *protocol.Policy, plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, logs []finalCanonicalEVMLog, emitters []string) error {
	return verifyFinalHistoricalCoordinatorActionWithPostcondition(evidence, policy, nil, plan, action, receipt, transaction, logs, emitters)
}

// Adds the finalized postcondition to an otherwise byte-exact historical
// action replay. It is mandatory for archived source and artifact review,
// while the nil-capable wrapper keeps isolated ABI unit tests focused on the
// event and calldata surface they construct.
func verifyFinalHistoricalCoordinatorActionWithPostcondition(evidence *FinalSemanticEvidence, policy *protocol.Policy, postcondition *ActionPostcondition, plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, logs []finalCanonicalEVMLog, emitters []string) error {
	if evidence == nil || plan == nil || action.Kind != "evm-transaction" || receipt.Status != "success" || transaction.Block != receipt.Block || transaction.To != strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()) {
		return errors.New("historical coordinator action target differs from archived plan")
	}
	if err := finalVerifyHistoricalCoordinatorTransaction(receipt, transaction); err != nil {
		return err
	}
	canonicalLogs, err := finalCanonicalizeLogs(logs)
	if err != nil || len(canonicalLogs) != len(logs) {
		return stateMismatchError(err, "historical coordinator action logs are not canonical")
	}
	for index := range canonicalLogs {
		if !finalSemanticCanonicalLogEqual(canonicalLogs[index], logs[index]) || canonicalLogs[index].TransactionHash != receipt.TransactionHash || canonicalLogs[index].BlockNumber != receipt.Block.Number || canonicalLogs[index].BlockHash != receipt.Block.Hash {
			return errors.New("historical coordinator action logs differ from its receipt")
		}
	}
	logsHash, hashErr := finalCanonicalReceiptLogsHash(canonicalLogs)
	if hashErr != nil || !strings.EqualFold(logsHash, receipt.LogsHash) {
		return stateMismatchError(hashErr, "historical coordinator action logs hash differs from its receipt")
	}
	canonicalEmitters, err := finalHistoricalCoordinatorEmitterGraph(canonicalLogs)
	if err != nil || !slices.Equal(canonicalEmitters, emitters) {
		return stateMismatchError(err, "historical coordinator action emitter graph is not canonical")
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	input, err := decodeEvidenceHex(transaction.Input)
	if err != nil || len(input) < 4 {
		return stateMismatchError(err, "historical coordinator action calldata is invalid")
	}
	method, err := finalHistoricalCoordinatorMethod(parsed, input)
	if err != nil {
		return err
	}
	values, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return fmt.Errorf("decode historical coordinator %s calldata: %w", method.Name, err)
	}
	canonicalArguments, packErr := method.Inputs.Pack(values...)
	canonicalInput := append(append([]byte(nil), method.ID...), canonicalArguments...)
	if packErr != nil || !bytes.Equal(canonicalInput, input) {
		return stateMismatchError(packErr, "historical coordinator %s calldata is not an exact canonical ABI tuple", method.Name)
	}
	expectedEmitters := []string{}
	switch method.Name {
	case "registerOperator":
		expectedEmitters, err = verifyFinalHistoricalRegisterOperator(evidence, plan, action, receipt, transaction, values, canonicalLogs)
	case "addConviction":
		expectedEmitters, err = verifyFinalHistoricalConviction(evidence, plan, action, receipt, transaction, values, canonicalLogs)
	case "deposit":
		expectedEmitters, err = verifyFinalHistoricalDeposit(evidence, plan, action, receipt, transaction, values, canonicalLogs)
	case "upgradeToAndCall":
		expectedEmitters, err = verifyFinalHistoricalUpgrade(plan, action, receipt, transaction, values, canonicalLogs)
	case "scheduleCommitmentOracle":
		expectedEmitters, err = verifyFinalHistoricalCommitmentOracleSchedule(postcondition, plan, action, receipt, transaction, values, canonicalLogs)
	case "schedulePolicy":
		expectedEmitters, err = verifyFinalHistoricalPolicySchedule(evidence, policy, postcondition, plan, action, receipt, transaction, values, canonicalLogs)
	default:
		return fmt.Errorf("historical coordinator action %s uses unsupported calldata selector %s", action.ID, method.Name)
	}
	if err != nil || !slices.Equal(emitters, expectedEmitters) {
		return stateMismatchError(err, "historical coordinator action %s emitter graph differs from archived plan", action.ID)
	}
	return nil
}

// Rejects aliases in the envelope before an ABI decoder can normalize them.
// The journal binds an action intent; this routine binds that intent to the
// exact finalized transaction bytes represented by the collected envelope.
func finalVerifyHistoricalCoordinatorTransaction(receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction) error {
	if err := requireFinalHex32("historical coordinator receipt transaction", receipt.TransactionHash); err != nil || receipt.TransactionHash != strings.ToLower(receipt.TransactionHash) || transaction.TransactionHash != receipt.TransactionHash {
		return stateMismatchError(err, "historical coordinator transaction hash differs from receipt")
	}
	if err := verifyFinalHead("historical coordinator receipt", receipt.Block); err != nil {
		return err
	}
	from, fromErr := finalCanonicalAddress(transaction.From)
	to, toErr := finalCanonicalAddress(transaction.To)
	if fromErr != nil || toErr != nil || from != transaction.From || to != transaction.To {
		return stateMismatchError(errors.Join(fromErr, toErr), "historical coordinator transaction addresses are not canonical")
	}
	input, inputErr := hexutil.Decode(transaction.Input)
	if inputErr != nil || len(input) < 4 || hexutil.Encode(input) != transaction.Input {
		return stateMismatchError(inputErr, "historical coordinator transaction input is not canonical")
	}
	value, valueOK := new(big.Int).SetString(transaction.ValueWei, 10)
	if !valueOK || value.Sign() < 0 || value.String() != transaction.ValueWei {
		return errors.New("historical coordinator transaction value is not canonical")
	}
	return nil
}

// Resolves a selector only when it names exactly one coordinator ABI method.
func finalHistoricalCoordinatorMethod(parsed abi.ABI, input []byte) (abi.Method, error) {
	if len(input) < 4 {
		return abi.Method{}, errors.New("historical coordinator calldata lacks a selector")
	}
	for _, method := range parsed.Methods {
		if bytes.Equal(method.ID, input[:4]) {
			return method, nil
		}
	}
	return abi.Method{}, errors.New("historical coordinator calldata selector is unknown")
}

// Verifies an operator registration against its plan role and the same
// receipt's initial authority events. The post-registration terminal pool row
// may have rotated authority, so the event is the only correct historic join.
func verifyFinalHistoricalRegisterOperator(evidence *FinalSemanticEvidence, plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, values []any, logs []finalCanonicalEVMLog) ([]string, error) {
	noID, err := finalHistoricalActionNO(action.ID, "operator.register.")
	if err != nil || action.Target != fmt.Sprintf("no:%d", noID) || len(values) != 8 || transaction.From != strings.ToLower(plan.Roles.Owner) {
		return nil, stateMismatchError(err, "historical operator registration action is invalid")
	}
	limit, err := registrationBurnLimit(plan, action)
	value, valueOK := new(big.Int).SetString(transaction.ValueWei, 10)
	if err != nil || !valueOK || value.Cmp(registrationFundingWei(limit)) != 0 {
		return nil, stateMismatchError(err, "historical operator registration value differs from archived plan")
	}
	callNoID, ok := values[0].(*big.Int)
	if !ok || !callNoID.IsUint64() || callNoID.Uint64() != noID {
		return nil, errors.New("historical operator registration calldata operator differs")
	}
	pool := finalHistoricalCoordinatorPool(evidence, noID)
	if pool == nil {
		return nil, errors.New("historical operator registration has no semantic pool")
	}
	expectedKeys := []string{pool.OperatorColdkey, pool.Hotkey, pool.DepositHotkey}
	for index, expected := range expectedKeys {
		key, keyErr := finalSemanticReceiptSS58Hex(expected)
		if keyErr != nil || !finalHistoricalBytes32Equal(values[index+1], key) {
			return nil, stateMismatchError(keyErr, "historical operator registration key %d differs", index+1)
		}
	}
	depositSigner, signerOK := values[4].(common.Address)
	rootSigner, rootOK := values[5].(common.Address)
	effectiveEpoch, epochOK := values[6].(uint64)
	maximumBurn, burnOK := values[7].(uint64)
	if !signerOK || !rootOK || !epochOK || effectiveEpoch == 0 || !burnOK || maximumBurn != limit || depositSigner == (common.Address{}) || rootSigner == (common.Address{}) {
		return nil, errors.New("historical operator registration calldata authority is invalid")
	}
	events, err := finalHistoricalCoordinatorDecodeLogs(plan, logs)
	if err != nil {
		return nil, err
	}
	if err := finalHistoricalRegisterEvents(events, pool, noID, effectiveEpoch, depositSigner, rootSigner); err != nil {
		return nil, err
	}
	return finalHistoricalCoordinatorEmitterSet(plan.Deployment.CoordinatorProxy, plan.Deployment.SettlementVault), nil
}

// Verifies the one planned voluntary-conviction transaction, including its
// dynamic nonce/deadline as recorded by the exact ConvictionAdded event.
func verifyFinalHistoricalConviction(evidence *FinalSemanticEvidence, plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, values []any, logs []finalCanonicalEVMLog) ([]string, error) {
	if action.ID != voluntaryConvictionActionID || action.Target != "no:1" || len(plan.Roles.OperatorDepositSigners) == 0 || transaction.From != strings.ToLower(plan.Roles.OperatorDepositSigners[0]) || transaction.ValueWei != "0" || len(values) != 4 {
		return nil, errors.New("historical voluntary conviction action is invalid")
	}
	noID, noIDOK := values[0].(*big.Int)
	amount, amountOK := values[1].(*big.Int)
	nonce, nonceOK := values[2].(*big.Int)
	deadline, deadlineOK := values[3].(uint64)
	wantAmount, parsed := new(big.Int).SetString(action.Parameters["amount_rao"], 10)
	if !noIDOK || !amountOK || !nonceOK || !deadlineOK || !parsed || noID.Cmp(big.NewInt(1)) != 0 || amount.Cmp(wantAmount) != 0 || nonce.Sign() < 0 || deadline < receipt.Block.Number {
		return nil, errors.New("historical voluntary conviction calldata differs from archived action")
	}
	events, err := finalHistoricalCoordinatorDecodeLogs(plan, logs)
	if err != nil {
		return nil, err
	}
	deposit, err := finalHistoricalDepositEvent(events, "ConvictionAdded", 1, amount, nonce, transaction.From, plan.PolicyHash)
	if err != nil {
		return nil, err
	}
	if err := finalHistoricalReservePrincipalAddition(evidence, receipt, events, deposit); err != nil {
		return nil, err
	}
	return finalHistoricalCoordinatorEmitterSet(plan.Deployment.CoordinatorProxy, plan.Deployment.ReserveSink), nil
}

// Verifies the planned dishonest-deposit call under the exact archived
// operator-deposit signer. This branch remains strict even when a test run
// places the fault before the signed campaign boundary.
func verifyFinalHistoricalDeposit(evidence *FinalSemanticEvidence, plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, values []any, logs []finalCanonicalEVMLog) ([]string, error) {
	noID, amount, err := dishonestDepositParameters(action)
	if err != nil || action.ID != dishonestDepositActionID || int(noID) > len(plan.Roles.OperatorDepositSigners) || transaction.From != strings.ToLower(plan.Roles.OperatorDepositSigners[noID-1]) || transaction.ValueWei != "0" || len(values) != 4 {
		return nil, stateMismatchError(err, "historical deposit action is invalid")
	}
	callNoID, noIDOK := values[0].(*big.Int)
	callAmount, amountOK := values[1].(*big.Int)
	nonce, nonceOK := values[2].(*big.Int)
	deadline, deadlineOK := values[3].(uint64)
	if !noIDOK || !amountOK || !nonceOK || !deadlineOK || !callNoID.IsUint64() || callNoID.Uint64() != noID || callAmount.Cmp(amount) != 0 || nonce.Sign() < 0 || deadline < receipt.Block.Number {
		return nil, errors.New("historical deposit calldata differs from archived action")
	}
	events, err := finalHistoricalCoordinatorDecodeLogs(plan, logs)
	if err != nil {
		return nil, err
	}
	deposit, err := finalHistoricalDepositEvent(events, "Deposit", noID, amount, nonce, transaction.From, plan.PolicyHash)
	if err != nil {
		return nil, err
	}
	if err := finalHistoricalReservePrincipalAddition(evidence, receipt, events, deposit); err != nil {
		return nil, err
	}
	return finalHistoricalCoordinatorEmitterSet(plan.Deployment.CoordinatorProxy, plan.Deployment.ReserveSink), nil
}

// Verifies a UUPS activation against its exact archived implementation, empty
// upgrade calldata, owner, and sole Upgraded event. No current runtime graph
// is consulted for a carried predecessor transition.
func verifyFinalHistoricalUpgrade(plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, values []any, logs []finalCanonicalEVMLog) ([]string, error) {
	if action.ID != "evm.coordinator-upgrade-activate" || action.Target != plan.Deployment.CoordinatorProxy.Hex() || transaction.From != strings.ToLower(plan.Roles.Owner) || transaction.ValueWei != "0" || len(values) != 2 || action.Parameters["implementation"] != plan.CoordinatorUpgrade.Implementation.Hex() || !strings.EqualFold(action.Parameters["runtime_code_hash"], plan.CoordinatorUpgrade.RuntimeCodeHash) {
		return nil, errors.New("historical coordinator upgrade action is invalid")
	}
	implementation, ok := values[0].(common.Address)
	data, dataOK := values[1].([]byte)
	if !ok || !dataOK || len(data) != 0 || implementation != plan.CoordinatorUpgrade.Implementation {
		return nil, errors.New("historical coordinator upgrade implementation differs from archived plan")
	}
	events, err := finalHistoricalCoordinatorDecodeLogs(plan, logs)
	if err != nil {
		return nil, err
	}
	if len(events) != 1 || events[0].Name != "Upgraded" || events[0].Log.Address != strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()) {
		return nil, errors.New("historical coordinator upgrade has an unexpected event graph")
	}
	observed, observedOK := finalSemanticAddress(events[0].Args, "implementation")
	if !observedOK || !strings.EqualFold(observed, implementation.Hex()) || events[0].Log.BlockNumber != receipt.Block.Number || !strings.EqualFold(events[0].Log.BlockHash, receipt.Block.Hash) {
		return nil, errors.New("historical coordinator Upgraded event differs from activation")
	}
	return finalHistoricalCoordinatorEmitterSet(plan.Deployment.CoordinatorProxy), nil
}

// Replays the temporary fleet-oracle handoff byte-for-byte. A terminal oracle
// value alone cannot prove that the batcher was active during refresh, so the
// exact schedule call, event, and both persisted observations are all bound.
func verifyFinalHistoricalCommitmentOracleSchedule(postcondition *ActionPostcondition, plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, values []any, logs []finalCanonicalEVMLog) ([]string, error) {
	if plan == nil || postcondition == nil || (action.ID != "fleet.refresh.oracle-activate" && action.ID != "fleet.refresh.oracle-restore") || action.Target != plan.Deployment.CoordinatorProxy.Hex() || transaction.From != strings.ToLower(plan.Roles.Owner) || transaction.ValueWei != "0" || len(values) != 2 {
		return nil, errors.New("historical commitment oracle schedule action is invalid")
	}
	wantOracle, targetErr := plannedFleetRefreshOracleTarget(action)
	oracle, oracleOK := values[0].(common.Address)
	effectiveEpoch, epochOK := values[1].(uint64)
	if targetErr != nil || !oracleOK || !epochOK || oracle == (common.Address{}) || oracle != wantOracle || effectiveEpoch == 0 {
		return nil, stateMismatchError(targetErr, "historical commitment oracle schedule calldata differs from approved action")
	}
	events, err := finalHistoricalCoordinatorDecodeLogs(plan, logs)
	if err != nil {
		return nil, err
	}
	if len(events) != 1 || events[0].Name != "CommitmentOracleScheduled" || events[0].Log.Address != strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()) || events[0].Log.BlockNumber != receipt.Block.Number || !strings.EqualFold(events[0].Log.BlockHash, receipt.Block.Hash) {
		return nil, errors.New("historical commitment oracle schedule has an unexpected event graph")
	}
	emittedOracle, emittedOracleOK := finalSemanticAddress(events[0].Args, "oracle")
	emittedEpoch, emittedEpochOK := finalSemanticUint(events[0].Args, "effectiveEpoch")
	if !emittedOracleOK || !emittedEpochOK || !strings.EqualFold(emittedOracle, oracle.Hex()) || emittedEpoch != effectiveEpoch {
		return nil, errors.New("historical CommitmentOracleScheduled event differs from calldata")
	}
	if err := finalHistoricalCommitmentOraclePostcondition(postcondition, action, receipt, oracle, effectiveEpoch); err != nil {
		return nil, err
	}
	return finalHistoricalCoordinatorEmitterSet(plan.Deployment.CoordinatorProxy), nil
}

// Validates the operational and independent oracle checkpoints for one
// schedule transaction. Each observer must show either the future pending
// handoff or the already activated target; a generic successful receipt is
// not treated as a temporal state proof.
func finalHistoricalCommitmentOraclePostcondition(postcondition *ActionPostcondition, action Action, receipt FinalEVMReceipt, target common.Address, effectiveEpoch uint64) error {
	if postcondition == nil || postcondition.ActionID != action.ID || postcondition.EVMFinalized.Number < receipt.Block.Number || postcondition.IndependentEVMFinalized.Number < receipt.Block.Number || len(postcondition.Observed) == 0 || len(postcondition.IndependentObserved) == 0 {
		return errors.New("historical commitment oracle postcondition is incomplete")
	}
	for _, observed := range []map[string]any{postcondition.Observed, postcondition.IndependentObserved} {
		currentEpoch, currentOK := finalSemanticObservedUint(observed, "current_epoch")
		pendingEpoch, pendingEpochOK := finalSemanticObservedUint(observed, "pending_epoch")
		immutable, immutableOK := observed["immutable_oracle"].(string)
		active, activeOK := observed["active_oracle"].(string)
		pending, pendingOK := observed["pending_oracle"].(string)
		observedTarget, targetOK := observed["target_oracle"].(string)
		if !currentOK || !pendingEpochOK || !immutableOK || !activeOK || !pendingOK || !targetOK || !common.IsHexAddress(immutable) || !common.IsHexAddress(active) || !common.IsHexAddress(pending) || !common.IsHexAddress(observedTarget) || common.HexToAddress(immutable) == (common.Address{}) || !strings.EqualFold(observedTarget, target.Hex()) {
			return errors.New("historical commitment oracle postcondition has malformed oracle state")
		}
		if strings.EqualFold(active, target.Hex()) {
			if currentEpoch < effectiveEpoch {
				return errors.New("historical commitment oracle postcondition activates before its effective epoch")
			}
			continue
		}
		if !strings.EqualFold(pending, target.Hex()) || pendingEpoch != effectiveEpoch || pendingEpoch <= currentEpoch {
			return errors.New("historical commitment oracle postcondition does not retain the scheduled future handoff")
		}
	}
	if action.ID == "fleet.refresh.oracle-restore" {
		immutable, _ := postcondition.Observed["immutable_oracle"].(string)
		if !strings.EqualFold(immutable, target.Hex()) {
			return errors.New("historical commitment oracle restore does not target the immutable oracle")
		}
	}
	return nil
}

// Reconstructs a bootstrap policy call from the captured canonical policy and
// validates the dynamic event separately: the contract fills effectiveBlock,
// whereas the approved calldata intentionally supplies zero for that field.
func verifyFinalHistoricalPolicySchedule(evidence *FinalSemanticEvidence, policy *protocol.Policy, postcondition *ActionPostcondition, plan *SetupPlan, action Action, receipt FinalEVMReceipt, transaction FinalCollectedEVMTransaction, values []any, logs []finalCanonicalEVMLog) ([]string, error) {
	if evidence == nil || policy == nil || action.ID != "policy.schedule-bootstrap" || action.Target != plan.Deployment.CoordinatorProxy.Hex() || transaction.From != strings.ToLower(plan.Roles.Owner) || transaction.ValueWei != "0" || len(values) != 1 || !strings.EqualFold(action.Parameters["policy_hash"], plan.PolicyHash) {
		return nil, errors.New("historical coordinator policy action is invalid")
	}
	events, err := finalHistoricalCoordinatorDecodeLogs(plan, logs)
	if err != nil {
		return nil, err
	}
	if len(events) != 1 || events[0].Name != "PolicyScheduled" || events[0].Log.Address != strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()) {
		return nil, errors.New("historical coordinator policy has an unexpected event graph")
	}
	index, indexOK := finalSemanticUint(events[0].Args, "index")
	policyHash, hashOK := finalSemanticHex32(events[0].Args, "policyHash")
	effectiveEpoch, epochOK := finalSemanticUint(events[0].Args, "effectiveEpoch")
	effectiveBlock, blockOK := finalSemanticUint(events[0].Args, "effectiveBlock")
	if !indexOK || index == 0 || !hashOK || !epochOK || effectiveEpoch == 0 || !blockOK || effectiveBlock <= receipt.Block.Number || !strings.EqualFold(policyHash, plan.PolicyHash) {
		return nil, errors.New("historical coordinator PolicyScheduled event differs from archived policy")
	}
	if err := finalHistoricalPolicyPostcondition(postcondition, action, receipt, index, policyHash, effectiveEpoch, effectiveBlock); err != nil {
		return nil, err
	}
	wantPolicyHash, hashErr := policy.HashHex()
	if hashErr != nil || !strings.EqualFold(policyHash, wantPolicyHash) {
		return nil, errors.New("historical coordinator policy artifact differs from archived action")
	}
	hash, err := decodeHex32("historical bootstrap policy hash", policyHash)
	if err != nil || policy.Settlement.EpochBlocks > ^uint64(0)/2 {
		return nil, stateMismatchError(err, "historical coordinator policy tuple overflows")
	}
	next := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: effectiveEpoch, EffectiveBlock: 0,
		EpochBlocks: policy.Settlement.EpochBlocks, RootCommitWindowBlocks: policy.Settlement.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: policy.Settlement.CloseGraceBlocks,
		ClaimTTLEpochs: policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: policy.Settlement.EpochBlocks * 2,
		EpochDepositCapRao: new(big.Int).SetUint64(policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(policy.Deposit.TotalTestCampaignCapRao),
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return nil, err
	}
	wantInput, err := parsed.Pack("schedulePolicy", next)
	observedInput, inputErr := hexutil.Decode(transaction.Input)
	if err != nil || inputErr != nil || !bytes.Equal(wantInput, observedInput) {
		return nil, stateMismatchError(errors.Join(err, inputErr), "historical coordinator policy calldata differs from archived tuple")
	}
	return finalHistoricalCoordinatorEmitterSet(plan.Deployment.CoordinatorProxy), nil
}

// Joins the event's dynamic policy coordinates to the verified post-state.
// The calldata deliberately contains a zero effective block; only the event
// reflects the contract-computed boundary, while the postcondition proves the
// index, hash, and future epoch entered the coordinator's canonical history.
func finalHistoricalPolicyPostcondition(postcondition *ActionPostcondition, action Action, receipt FinalEVMReceipt, index uint64, policyHash string, effectiveEpoch, effectiveBlock uint64) error {
	if postcondition == nil || postcondition.ActionID != action.ID || postcondition.Observed == nil || postcondition.EVMFinalized.Number < receipt.Block.Number {
		return errors.New("historical coordinator policy postcondition is incomplete")
	}
	policyCount, countOK := finalSemanticObservedUint(postcondition.Observed, "policy_count")
	currentEpoch, epochOK := finalSemanticObservedUint(postcondition.Observed, "current_epoch")
	observedHash, hashOK := postcondition.Observed["policy_hash"].(string)
	scheduledIndex, indexOK := finalSemanticObservedUint(postcondition.Observed, "scheduled_policy_index")
	scheduledEpoch, scheduledEpochOK := finalSemanticObservedUint(postcondition.Observed, "scheduled_policy_effective_epoch")
	scheduledBlock, scheduledBlockOK := finalSemanticObservedUint(postcondition.Observed, "scheduled_policy_effective_block")
	scheduledHash, scheduledHashOK := postcondition.Observed["scheduled_policy_hash"].(string)
	if !countOK || policyCount == 0 || index != policyCount-1 || !epochOK || effectiveEpoch <= currentEpoch || !hashOK || requireFinalHex32("historical coordinator postcondition policy hash", observedHash) != nil || !strings.EqualFold(observedHash, policyHash) || !indexOK || scheduledIndex != index || !scheduledEpochOK || scheduledEpoch != effectiveEpoch || !scheduledBlockOK || scheduledBlock != effectiveBlock || !scheduledHashOK || requireFinalHex32("historical coordinator scheduled policy hash", scheduledHash) != nil || !strings.EqualFold(scheduledHash, policyHash) {
		return errors.New("historical coordinator PolicyScheduled event differs from its verified post-state")
	}
	return nil
}

// Decodes all logs against the archived coordinator/vault/reserve graph.
// This avoids treating a later deployment's address map as authority for a
// carried predecessor receipt.
func finalHistoricalCoordinatorDecodeLogs(plan *SetupPlan, logs []finalCanonicalEVMLog) ([]finalSemanticEvent, error) {
	if plan == nil || len(logs) == 0 {
		return nil, errors.New("historical coordinator log decode inputs are incomplete")
	}
	coordinator, vault, reserve, err := finalSemanticReceiptContractABIs()
	if err != nil {
		return nil, err
	}
	contracts := map[string]abi.ABI{
		strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()): coordinator,
		strings.ToLower(plan.Deployment.SettlementVault.Hex()):  vault,
		strings.ToLower(plan.Deployment.ReserveSink.Hex()):      reserve,
	}
	result := make([]finalSemanticEvent, 0, len(logs))
	for _, log := range logs {
		contract, found := contracts[log.Address]
		if !found || len(log.Topics) == 0 {
			return nil, errors.New("historical coordinator receipt emitted from an unapproved contract")
		}
		event, found := finalSemanticReceiptABIEvent(contract, log.Topics[0])
		if !found {
			return nil, errors.New("historical coordinator receipt has an unknown event")
		}
		data, decodeErr := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
		args := map[string]any{}
		if decodeErr == nil {
			decodeErr = event.Inputs.NonIndexed().UnpackIntoMap(args, data)
		}
		if decodeErr == nil {
			topics := make([]common.Hash, len(log.Topics)-1)
			for index := 1; index < len(log.Topics); index++ {
				topics[index-1] = common.HexToHash(log.Topics[index])
			}
			decodeErr = abi.ParseTopicsIntoMap(args, indexedABIArguments(event.Inputs), topics)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode historical coordinator %s event: %w", event.Name, decodeErr)
		}
		result = append(result, finalSemanticEvent{Name: event.Name, Log: log, Args: args})
	}
	return result, nil
}

// Requires exactly the initial vault/proxy registration graph. Each event is
// fully bound to calldata and immutable pool identity, rather than merely to
// its operator number or currently active authority.
func finalHistoricalRegisterEvents(events []finalSemanticEvent, pool *FinalPoolUIDEvidence, noID, epoch uint64, depositSigner, rootSigner common.Address) error {
	if pool == nil || len(events) != 2 {
		return errors.New("historical operator registration has an unexpected event count")
	}
	coldkey, coldkeyErr := finalSemanticReceiptSS58Hex(pool.OperatorColdkey)
	poolHotkey, poolHotkeyErr := finalSemanticReceiptSS58Hex(pool.Hotkey)
	depositHotkey, depositHotkeyErr := finalSemanticReceiptSS58Hex(pool.DepositHotkey)
	if coldkeyErr != nil || poolHotkeyErr != nil || depositHotkeyErr != nil {
		return stateMismatchError(errors.Join(coldkeyErr, poolHotkeyErr, depositHotkeyErr), "historical operator registration pool keys are invalid")
	}
	poolRegistered := false
	scheduled := false
	for _, event := range events {
		switch event.Name {
		case "PoolRegistered":
			value, noIDOK := finalSemanticUint(event.Args, "noId")
			hotkey, hotkeyOK := finalSemanticHex32(event.Args, "hotkey")
			uid, uidOK := finalSemanticUint(event.Args, "uid")
			if !noIDOK || !hotkeyOK || !uidOK || value != noID || !strings.EqualFold(hotkey, poolHotkey) || uid != uint64(pool.UID) || poolRegistered {
				return errors.New("historical operator registration PoolRegistered event differs")
			}
			poolRegistered = true
		case "OperatorScheduled":
			value, noIDOK := finalSemanticUint(event.Args, "noId")
			effective, epochOK := finalSemanticUint(event.Args, "effectiveEpoch")
			eventColdkey, coldkeyOK := finalSemanticHex32(event.Args, "coldkey")
			eventPoolHotkey, poolHotkeyOK := finalSemanticHex32(event.Args, "poolHotkey")
			eventDepositHotkey, depositHotkeyOK := finalSemanticHex32(event.Args, "depositHotkey")
			deposit, depositOK := finalSemanticAddress(event.Args, "depositSigner")
			root, rootOK := finalSemanticAddress(event.Args, "rootSigner")
			active, activeOK := event.Args["active"].(bool)
			if !noIDOK || !epochOK || !coldkeyOK || !poolHotkeyOK || !depositHotkeyOK || !depositOK || !rootOK || !activeOK || value != noID || effective != epoch || !strings.EqualFold(eventColdkey, coldkey) || !strings.EqualFold(eventPoolHotkey, poolHotkey) || !strings.EqualFold(eventDepositHotkey, depositHotkey) || !strings.EqualFold(deposit, depositSigner.Hex()) || !strings.EqualFold(root, rootSigner.Hex()) || !active || scheduled {
				return errors.New("historical operator registration OperatorScheduled event differs")
			}
			scheduled = true
		default:
			return fmt.Errorf("historical operator registration has unexpected %s event", event.Name)
		}
	}
	if !poolRegistered || !scheduled {
		return errors.New("historical operator registration lacks its full multi-emitter event graph")
	}
	return nil
}

// Requires one exact deposit-like coordinator event and returns its dynamic
// epoch for the reserve-side ledger join. Policy hash is part of the event
// authority: a matching amount under a different policy is not equivalent.
func finalHistoricalDepositEvent(events []finalSemanticEvent, name string, noID uint64, amount, nonce *big.Int, from, policyHash string) (finalSemanticReceiptDeposit, error) {
	if err := requireFinalHex32("historical coordinator policy hash", policyHash); err != nil {
		return finalSemanticReceiptDeposit{}, err
	}
	if len(events) != 2 {
		return finalSemanticReceiptDeposit{}, errors.New("historical coordinator deposit receipt has an unexpected event count")
	}
	var result finalSemanticReceiptDeposit
	found := false
	for _, event := range events {
		if event.Name == "ReservePrincipalAdded" {
			continue
		}
		if event.Name != name {
			return finalSemanticReceiptDeposit{}, fmt.Errorf("historical coordinator %s receipt has unexpected %s event", name, event.Name)
		}
		decoded, err := finalSemanticReceiptDepositEvent(event)
		if err != nil || decoded.NoID != noID || decoded.Amount.Cmp(amount) != 0 || decoded.Nonce.Cmp(nonce) != 0 || !strings.EqualFold(decoded.Funder, from) || !strings.EqualFold(decoded.PolicyHash, policyHash) || found {
			return finalSemanticReceiptDeposit{}, stateMismatchError(err, "historical coordinator %s event differs from calldata", name)
		}
		result = decoded
		found = true
	}
	if !found {
		return finalSemanticReceiptDeposit{}, fmt.Errorf("historical coordinator %s event is absent", name)
	}
	return result, nil
}

// Joins a dynamic deposit event to exactly one signed reserve ledger row and
// compares every emitted principal field. This makes the proxy call and the
// reserve custody side effect one inseparable historical operation.
func finalHistoricalReservePrincipalAddition(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, events []finalSemanticEvent, deposit finalSemanticReceiptDeposit) error {
	if evidence == nil || deposit.Amount == nil || deposit.Amount.Sign() <= 0 {
		return errors.New("historical reserve principal inputs are incomplete")
	}
	var addition *FinalReservePrincipalAddedEvidence
	for index := range evidence.Reserve.PrincipalAdditions {
		candidate := &evidence.Reserve.PrincipalAdditions[index]
		if !finalSemanticReceiptMatches(candidate.Receipt, receipt) {
			continue
		}
		if addition != nil {
			return errors.New("historical reserve receipt has multiple principal additions")
		}
		addition = candidate
	}
	if addition == nil || addition.NoID != deposit.NoID || addition.Epoch != deposit.Epoch || addition.AmountRao != deposit.Amount.String() {
		return errors.New("historical reserve principal row differs from coordinator deposit")
	}
	if len(events) != 2 {
		return errors.New("historical reserve receipt has an unexpected event count")
	}
	found := false
	for _, event := range events {
		if event.Name != "ReservePrincipalAdded" {
			continue
		}
		if found {
			return errors.New("historical reserve receipt duplicates ReservePrincipalAdded")
		}
		for _, expected := range []struct {
			field string
			value string
		}{
			{field: "amount", value: addition.AmountRao},
			{field: "operatorPrincipal", value: addition.OperatorPrincipalRao},
			{field: "totalPrincipal", value: addition.TotalPrincipalRao},
			{field: "liveStake", value: addition.LiveStakeRao},
		} {
			if err := finalSemanticReceiptRequireDecimal(event, expected.field, expected.value); err != nil {
				return err
			}
		}
		if err := finalSemanticReceiptRequireUint(event, "epoch", addition.Epoch); err != nil {
			return err
		}
		if err := finalSemanticReceiptRequireUint(event, "noId", addition.NoID); err != nil {
			return err
		}
		operatorPrincipal, operatorOK := finalSemanticInteger(event.Args, "operatorPrincipal")
		totalPrincipal, totalOK := finalSemanticInteger(event.Args, "totalPrincipal")
		liveStake, liveOK := finalSemanticInteger(event.Args, "liveStake")
		if !operatorOK || !totalOK || !liveOK || operatorPrincipal.Sign() <= 0 || totalPrincipal.Sign() <= 0 || operatorPrincipal.Cmp(deposit.Amount) < 0 || operatorPrincipal.Cmp(totalPrincipal) > 0 || liveStake.Cmp(totalPrincipal) < 0 {
			return errors.New("historical ReservePrincipalAdded accounting is incoherent")
		}
		found = true
	}
	if !found {
		return errors.New("historical reserve receipt lacks ReservePrincipalAdded")
	}
	return nil
}

// Returns a strict, canonical unique address graph derived from one archived
// deployment rather than from the current terminal deployment.
func finalHistoricalCoordinatorEmitterSet(addresses ...common.Address) []string {
	result := make([]string, len(addresses))
	for index, address := range addresses {
		result[index] = strings.ToLower(address.Hex())
	}
	sort.Strings(result)
	return slices.Compact(result)
}

// Parses one operator action suffix while rejecting a leading-zero alias or
// an accidental action-family prefix match.
func finalHistoricalActionNO(actionID, prefix string) (uint64, error) {
	if !strings.HasPrefix(actionID, prefix) {
		return 0, errors.New("historical coordinator action ID has an unexpected family")
	}
	value := strings.TrimPrefix(actionID, prefix)
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != value {
		return 0, errors.New("historical coordinator action ID has an invalid operator number")
	}
	return parsed, nil
}

// Resolves one pool identity without selecting a duplicate row by order.
func finalHistoricalCoordinatorPool(evidence *FinalSemanticEvidence, noID uint64) *FinalPoolUIDEvidence {
	if evidence == nil {
		return nil
	}
	var result *FinalPoolUIDEvidence
	for index := range evidence.Pools {
		if evidence.Pools[index].NoID != noID {
			continue
		}
		if result != nil {
			return nil
		}
		result = &evidence.Pools[index]
	}
	return result
}

// Checks one ABI bytes32 argument against a canonical lower-case hexadecimal
// source field without accepting an all-zero or short representation.
func finalHistoricalBytes32Equal(value any, expected string) bool {
	decoded, err := hexutil.Decode(expected)
	if err != nil || len(decoded) != common.HashLength {
		return false
	}
	actual, ok := value.([32]byte)
	return ok && bytes.Equal(actual[:], decoded)
}
