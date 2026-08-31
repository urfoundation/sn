// Finalized fleet-mirror recovery authenticates an interrupted convergent EVM
// write before a revised setup plan is allowed to verify it without rebroadcast.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Binds one successful ancestor transaction to its exact action and canonical
// native/EVM commitment identity. The revised action itself remains unchanged:
// its normal executor first reads this exact state and therefore broadcasts
// nothing before persisting the missing postcondition.
type finalizedFleetMirrorRecovery struct {
	Transaction        planRevisionTransaction
	Action             Action
	Fleet              int
	Hotkey             [32]byte
	CommitmentHash     [32]byte
	FinalizedBlock     uint64
	FinalizedBlockHash [32]byte
}

// Carries every narrowly reviewed successful-transaction exception discovered
// while checking a plan lineage. Generic successful EVM writes remain fatal.
type planRevisionRecoveries struct {
	VoluntaryConvictions []voluntaryConvictionDuplicateRecovery
	FleetMirrors         []finalizedFleetMirrorRecovery
	SubstrateFundings    []finalizedSubstrateFundingRecovery
}

// Parse only the canonical fleet mirror action namespace and reject aliases
// such as leading zeroes or out-of-range candidate indices.
func fleetMirrorRecoveryIndex(actionID string, maximum int) (int, error) {
	const prefix = "fleet.mirror."
	if maximum < 1 || !strings.HasPrefix(actionID, prefix) {
		return 0, fmt.Errorf("action %q is not a fleet mirror", actionID)
	}
	index, err := strconv.Atoi(strings.TrimPrefix(actionID, prefix))
	if err != nil || index < 1 || index > maximum || actionID != fmt.Sprintf("%s%d", prefix, index) {
		return 0, fmt.Errorf("fleet mirror action %q is out of range", actionID)
	}
	return index, nil
}

// Load one hash-authenticated plan only from the active approved ancestry.
func loadFleetMirrorLineagePlan(stateDir string, prior *SetupPlan, planHash string) (*SetupPlan, error) {
	if prior == nil || !prior.allowedPlanHashes()[planHash] {
		return nil, fmt.Errorf("plan %s is outside the approved fleet-mirror lineage", planHash)
	}
	if strings.EqualFold(prior.PlanHash, planHash) {
		return prior, nil
	}
	plan, err := readPersistedPlanFile(filepath.Join(stateDir, "plans", stringsTrim0x(planHash)+".json"))
	if err != nil {
		return nil, fmt.Errorf("read fleet-mirror ancestor plan %s: %w", planHash, err)
	}
	if !strings.EqualFold(plan.PlanHash, planHash) {
		return nil, fmt.Errorf("fleet-mirror ancestor identity %s differs from %s", plan.PlanHash, planHash)
	}
	return plan, nil
}

// Resolve one exact fleet-mirror intent without accepting a same-id sibling or
// another kind of EVM action.
func exactFleetMirrorPlanAction(plan *SetupPlan, actionID, intentHash string, headFleets, maximum int) (Action, int, error) {
	if plan == nil {
		return Action{}, 0, errors.New("fleet-mirror plan is unavailable")
	}
	if headFleets < 1 || headFleets > maximum {
		return Action{}, 0, errors.New("fleet-mirror topology bounds are invalid")
	}
	fleet, err := fleetMirrorRecoveryIndex(actionID, maximum)
	if err != nil {
		return Action{}, 0, err
	}
	var result *Action
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID != actionID || action.IntentHash != intentHash {
			continue
		}
		if result != nil {
			return Action{}, 0, errors.New("plan has duplicate exact fleet-mirror actions")
		}
		copy := *action
		result = &copy
	}
	if result == nil {
		return Action{}, 0, fmt.Errorf("plan %s has no fleet-mirror intent %s", plan.PlanHash, intentHash)
	}
	wantTarget := fmt.Sprintf("head-fleet:%d", fleet)
	if fleet > headFleets {
		wantTarget = fmt.Sprintf("challenger-fleet:%d", fleet)
	}
	if result.Kind != "evm-transaction" || result.Target != wantTarget || result.Parameters[fleetCommitmentStorageParameter] != fleetCommitmentStorageV2 {
		return Action{}, 0, errors.New("fleet-mirror action does not have the release transaction shape")
	}
	return *result, fleet, nil
}

// Require the exact native commitment transaction to have reached both a
// finalized journal checkpoint and durable postcondition verification. The
// source is the plan which authorized the mirror transaction: a later plan may
// legitimately rewire the already verified native action's dependencies and
// therefore has a different intent.
func fleetCommitmentEvidenceWasVerified(source *SetupPlan, entries []JournalEntry, fleet int, evidence *FleetCommitmentEvidence) bool {
	if source == nil || evidence == nil || fleet < 1 {
		return false
	}
	actionID := fmt.Sprintf("fleet.commitment.%d", fleet)
	var planned *Action
	for index := range source.Actions {
		action := &source.Actions[index]
		if action.ID != actionID {
			continue
		}
		if planned != nil {
			return false
		}
		planned = action
	}
	if planned == nil || planned.Kind != "substrate-extrinsic" {
		return false
	}
	allowedPlans := source.allowedPlanHashes()
	for _, finalized := range entries {
		if !allowedPlans[finalized.PlanHash] || finalized.ActionID != actionID || finalized.Stage != StageFinalized ||
			!actionAcceptsIntent(*planned, finalized.IntentHash) ||
			!strings.EqualFold(finalized.TransactionHash, evidence.ExtrinsicHash) || finalized.BlockNumber != evidence.FinalizedBlock ||
			!strings.EqualFold(finalized.BlockHash, evidence.FinalizedBlockHash) {
			continue
		}
		for _, verified := range entries {
			if verified.PlanHash == finalized.PlanHash && verified.ActionID == finalized.ActionID && verified.IntentHash == finalized.IntentHash && verified.Stage == StageVerified {
				return true
			}
		}
	}
	return false
}

// Load the exact canonical public preimage which the native chain authenticated.
// Client IDs are server-assigned state and therefore cannot be regenerated by
// BuildRoleSecrets during read-only revision; their committed hash is the
// authority. Deterministic client keys and every other manifest identity are
// still checked independently.
func loadFleetMirrorManifest(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, fleet int, coordinator common.Address) (protocol.FleetManifest, [32]byte, error) {
	if cfg == nil || roles == nil || fleet < 1 || fleet > cfg.Config.Topology.fleetCandidates() || coordinator == (common.Address{}) {
		return protocol.FleetManifest{}, [32]byte{}, errors.New("fleet-mirror manifest context is incomplete")
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.json", fleet)))
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, err
	}
	manifest, err := protocol.ParseFleetManifest(raw)
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, err
	}
	hotkey, err := roleBytes32(roles, fleetHotkeyLabel(fleet))
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, err
	}
	wantFleetID := derive32(cfg, fmt.Sprintf("fleet-id/%d", fleet))
	if manifest.ChainID != cfg.ChainID || manifest.Netuid != cfg.Netuid || common.BytesToAddress(manifest.Coordinator[:]) != coordinator || manifest.FleetID != wantFleetID || manifest.Hotkey != hotkey || manifest.Generation != 1 || len(manifest.Members) != cfg.Config.Topology.ClientsPerHeadFleet {
		return protocol.FleetManifest{}, [32]byte{}, errors.New("fleet-mirror manifest identity differs from the release topology")
	}
	expectedKeys := make(map[[32]byte]bool, cfg.Config.Topology.ClientsPerHeadFleet)
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner := fleetMemberMinerIndex(cfg, fleet, member)
		role, found := roles.Clients[fmt.Sprintf("miner-%d", miner)]
		keyBytes, decodeErr := hex.DecodeString(role.PublicKeyHex)
		if !found || decodeErr != nil || len(keyBytes) != 32 {
			return protocol.FleetManifest{}, [32]byte{}, fmt.Errorf("fleet-mirror miner-%d client key is unavailable", miner)
		}
		var key [32]byte
		copy(key[:], keyBytes)
		expectedKeys[key] = true
	}
	for _, member := range manifest.Members {
		if !expectedKeys[member.ClientKey] {
			return protocol.FleetManifest{}, [32]byte{}, errors.New("fleet-mirror manifest contains an unexpected client key")
		}
		delete(expectedKeys, member.ClientKey)
	}
	if len(expectedKeys) != 0 {
		return protocol.FleetManifest{}, [32]byte{}, errors.New("fleet-mirror manifest omits a deterministic client key")
	}
	commitmentHash, err := manifest.CommitmentHash()
	return *manifest, commitmentHash, err
}

// Authenticate signer, destination, value, chain, fee envelope, calldata, and
// the unique exact event from the signed transaction and finalized receipt.
func validateFinalizedFleetMirrorTransaction(cfg *ResolvedConfig, plan *SetupPlan, action Action, signed *ethTypes.Transaction, receipt *ethTypes.Receipt, coordinator common.Address, hotkey, commitmentHash [32]byte, finalizedBlock uint64, finalizedBlockHash [32]byte) error {
	if cfg == nil || plan == nil || signed == nil || receipt == nil || coordinator == (common.Address{}) || finalizedBlock == 0 {
		return errors.New("finalized fleet-mirror transaction context is incomplete")
	}
	if signed.To() == nil || *signed.To() != coordinator || signed.Value() == nil || signed.Value().Sign() != 0 || signed.ChainId().Cmp(new(big.Int).SetUint64(plan.ChainID)) != 0 || plan.ChainID != cfg.ChainID {
		return errors.New("fleet-mirror destination, value, or chain differs from approval")
	}
	if !common.IsHexAddress(plan.Roles.CommitmentOracle) {
		return errors.New("fleet-mirror plan has no valid commitment oracle")
	}
	signer := ethTypes.LatestSignerForChainID(signed.ChainId())
	from, err := ethTypes.Sender(signer, signed)
	if err != nil || from != common.HexToAddress(plan.Roles.CommitmentOracle) {
		return stateMismatchError(err, "fleet-mirror signer %s differs from approved %s", from, plan.Roles.CommitmentOracle)
	}
	maximumGasUnits, maximumFeePerGas, err := evmActionFeeEnvelope(action)
	if err != nil {
		return err
	}
	maximumFee := new(big.Int).SetUint64(maximumFeePerGas)
	if signed.Gas() > maximumGasUnits || signed.GasFeeCap().Cmp(maximumFee) > 0 || signed.GasTipCap().Cmp(maximumFee) > 0 {
		return fmt.Errorf("fleet-mirror signed fee envelope exceeds %d gas at %d wei", maximumGasUnits, maximumFeePerGas)
	}
	contract := stabi.NewSTCoordinator()
	wantData, err := contract.TryPackMirrorCommitment(hotkey, commitmentHash, finalizedBlock, finalizedBlockHash)
	if err != nil {
		return err
	}
	if !bytes.Equal(signed.Data(), wantData) {
		return errors.New("fleet-mirror calldata differs from the exact native commitment evidence")
	}
	if receipt.Status != ethTypes.ReceiptStatusSuccessful || receipt.TxHash != signed.Hash() || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Sign() <= 0 {
		return errors.New("fleet-mirror receipt is not the successful signed transaction")
	}
	matchingEvents := 0
	allMirrorEvents := 0
	eventTopic := crypto.Keccak256Hash([]byte("CommitmentMirrored(bytes32,bytes32,uint64,bytes32)"))
	for _, log := range receipt.Logs {
		if log == nil || log.Address != coordinator || len(log.Topics) == 0 || log.Topics[0] != eventTopic {
			continue
		}
		allMirrorEvents++
		event, unpackErr := contract.UnpackCommitmentMirroredEvent(log)
		if unpackErr != nil {
			continue
		}
		if event.Hotkey == hotkey && event.CommitmentHash == commitmentHash && event.FinalizedBlock == finalizedBlock && event.FinalizedBlockHash == finalizedBlockHash {
			matchingEvents++
		}
	}
	if allMirrorEvents != 1 || matchingEvents != 1 {
		return fmt.Errorf("fleet-mirror receipt has %d mirror events and %d exact matches", allMirrorEvents, matchingEvents)
	}
	return nil
}

// Read one mirrored commitment at an explicit EVM checkpoint.
func fleetMirrorAt(ctx context.Context, client *ethclient.Client, coordinator common.Address, hotkey [32]byte, block uint64) (stabi.MirroredCommitmentsOutput, error) {
	if client == nil || coordinator == (common.Address{}) || hotkey == ([32]byte{}) || block == 0 {
		return stabi.MirroredCommitmentsOutput{}, errors.New("fleet-mirror checkpoint query is incomplete")
	}
	contract := stabi.NewSTCoordinator()
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &coordinator, Data: contract.PackMirroredCommitments(hotkey)}, new(big.Int).SetUint64(block))
	if err != nil {
		return stabi.MirroredCommitmentsOutput{}, err
	}
	return contract.UnpackMirroredCommitments(out)
}

// Require both the transaction checkpoint and current finalized coordinator
// state to retain the exact native identity needed for executor convergence.
func validateFleetMirrorRecoveryState(historical, current stabi.MirroredCommitmentsOutput, commitmentHash [32]byte, finalizedBlock uint64, finalizedBlockHash [32]byte) error {
	if !fleetMirrorMatches(historical, commitmentHash, finalizedBlock, finalizedBlockHash) {
		return errors.New("fleet-mirror historical state differs from its transaction")
	}
	if !fleetMirrorMatches(current, commitmentHash, finalizedBlock, finalizedBlockHash) {
		return errors.New("fleet-mirror current state no longer converges without a transaction")
	}
	return nil
}

// Recognize a later exact action only when its hash-bound postcondition file
// proves both operational and comparison observers saw the recovered mirror.
// Once this exists, a later native rewrite of the same fleet commitment is not
// allowed to reopen the already closed ancestor transaction.
func verifiedFleetMirrorDescendant(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry, transaction planRevisionTransaction, action Action, fleet int, commitmentHash [32]byte, finalizedBlock uint64) (bool, error) {
	if cfg == nil || prior == nil || fleet < 1 || finalizedBlock == 0 {
		return false, errors.New("fleet-mirror descendant context is incomplete")
	}
	wantObserved := map[string]any{
		"fleet": fleet, "commitment_hash": common.BytesToHash(commitmentHash[:]).Hex(), "finalized_block": finalizedBlock,
		"kind": action.Kind, "target": action.Target,
	}
	executor := &Executor{cfg: cfg, plan: prior, stateDir: stateDir}
	for _, entry := range entries {
		if entry.PlanHash == transaction.PlanHash || !prior.allowedPlanHashes()[entry.PlanHash] || entry.ActionID != transaction.ActionID || entry.IntentHash != transaction.IntentHash || entry.Stage != StageVerified {
			continue
		}
		descendantPlan, err := loadFleetMirrorLineagePlan(stateDir, prior, entry.PlanHash)
		if err != nil {
			return false, err
		}
		descendantAction, descendantFleet, err := exactFleetMirrorPlanAction(descendantPlan, entry.ActionID, entry.IntentHash, cfg.Config.Topology.HeadFleets, cfg.Config.Topology.fleetCandidates())
		if err != nil || descendantFleet != fleet || descendantAction.IntentHash != action.IntentHash {
			return false, stateMismatchError(err, "verified descendant fleet mirror differs from recovered fleet %d", fleet)
		}
		record, err := executor.readPersistedPostcondition(entry)
		if err != nil {
			return false, fmt.Errorf("verified descendant fleet-mirror postcondition: %w", err)
		}
		if err := observedPostconditionMatches(record.Observed, wantObserved); err != nil {
			return false, fmt.Errorf("verified descendant operational fleet mirror: %w", err)
		}
		if err := observedPostconditionMatches(record.IndependentObserved, wantObserved); err != nil {
			return false, fmt.Errorf("verified descendant comparison fleet mirror: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// Prove the native evidence against canonical historical storage and the
// current finalized commitment before trusting its EVM mirror.
func validateFleetMirrorNativeEvidence(chain *crv4.Chain, cfg *ResolvedConfig, hotkey, commitmentHash [32]byte, evidence *FleetCommitmentEvidence, requireExactCurrent bool) error {
	if chain == nil || cfg == nil || evidence == nil {
		return errors.New("fleet-mirror native evidence context is incomplete")
	}
	blockHash, err := types.NewHashFromHexString(evidence.FinalizedBlockHash)
	if err != nil {
		return err
	}
	finalizedHash, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return err
	}
	finalizedHeader, err := chain.API.RPC.Chain.GetHeader(finalizedHash)
	if err != nil {
		return err
	}
	canonicalHash, err := chain.API.RPC.Chain.GetBlockHash(evidence.FinalizedBlock)
	if err != nil || uint64(finalizedHeader.Number) < evidence.FinalizedBlock || canonicalHash != blockHash {
		return stateMismatchError(err, "fleet-mirror native block %d is not canonical and finalized", evidence.FinalizedBlock)
	}
	historical, err := chain.FleetCommitmentAt(cfg.Netuid, hotkey, blockHash)
	if err != nil {
		return err
	}
	if err := crv4.ValidateFleetCommitmentWrite(commitmentHash, evidence.FinalizedBlock, historical); err != nil {
		return err
	}
	if requireExactCurrent {
		current, err := chain.FleetCommitmentFinalized(cfg.Netuid, hotkey)
		if err != nil || current.Hash != commitmentHash || current.CommitmentBlock != evidence.CommitmentBlock {
			return stateMismatchError(err, "fleet-mirror current native commitment differs from block %d", evidence.CommitmentBlock)
		}
	}
	return nil
}

// Authenticate the sole recoverable fleet-mirror shape from signed bytes,
// receipt finality, journal lineage, native storage, and EVM state.
func detectFinalizedFleetMirrorRecovery(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry, chain *crv4.Chain, client *ethclient.Client, signed *ethTypes.Transaction, receipt *ethTypes.Receipt, transaction planRevisionTransaction) (finalizedFleetMirrorRecovery, error) {
	if cfg == nil || prior == nil || chain == nil || client == nil || signed == nil || receipt == nil {
		return finalizedFleetMirrorRecovery{}, errors.New("successful unverified EVM transaction is not a recoverable fleet mirror")
	}
	sourcePlan, err := loadFleetMirrorLineagePlan(stateDir, prior, transaction.PlanHash)
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	action, fleet, err := exactFleetMirrorPlanAction(sourcePlan, transaction.ActionID, transaction.IntentHash, cfg.Config.Topology.HeadFleets, cfg.Config.Topology.fleetCandidates())
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	coordinator := sourcePlan.Deployment.CoordinatorProxy
	if coordinator != prior.Deployment.CoordinatorProxy {
		return finalizedFleetMirrorRecovery{}, errors.New("fleet-mirror coordinator differs across plan lineage and manifest")
	}
	manifest, commitmentHash, err := loadFleetMirrorManifest(cfg, stateDir, roles, fleet, coordinator)
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	evidence, err := loadFleetCommitmentEvidence(stateDir, fleet)
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	evidenceHotkey, err := decodeHex32("fleet-mirror evidence hotkey", evidence.Hotkey)
	if err != nil || evidenceHotkey != manifest.Hotkey {
		return finalizedFleetMirrorRecovery{}, stateMismatchError(err, "fleet-mirror evidence hotkey differs for fleet %d", fleet)
	}
	evidenceCommitment, err := decodeHex32("fleet-mirror evidence commitment", evidence.CommitmentHash)
	if err != nil || evidenceCommitment != commitmentHash {
		return finalizedFleetMirrorRecovery{}, stateMismatchError(err, "fleet-mirror evidence commitment differs for fleet %d", fleet)
	}
	finalizedBlockHash, err := decodeHex32("fleet-mirror evidence block", evidence.FinalizedBlockHash)
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	if !fleetCommitmentEvidenceWasVerified(sourcePlan, entries, fleet, evidence) {
		return finalizedFleetMirrorRecovery{}, errors.New("fleet-mirror native commitment lacks exact finalized and verified journal evidence")
	}
	descendantVerified, err := verifiedFleetMirrorDescendant(cfg, stateDir, prior, entries, transaction, action, fleet, commitmentHash, evidence.FinalizedBlock)
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	if err := validateFinalizedFleetMirrorTransaction(cfg, sourcePlan, action, signed, receipt, coordinator, manifest.Hotkey, commitmentHash, evidence.FinalizedBlock, finalizedBlockHash); err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	if err := validateFleetMirrorNativeEvidence(chain, cfg, manifest.Hotkey, commitmentHash, evidence, !descendantVerified); err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	if receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() {
		return finalizedFleetMirrorRecovery{}, errors.New("fleet-mirror receipt block is unavailable")
	}
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	historical, err := fleetMirrorAt(ctx, client, coordinator, manifest.Hotkey, receipt.BlockNumber.Uint64())
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	current, err := fleetMirrorAt(ctx, client, coordinator, manifest.Hotkey, head.Number)
	if err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	if err := validateFleetMirrorRecoveryState(historical, current, commitmentHash, evidence.FinalizedBlock, finalizedBlockHash); err != nil {
		return finalizedFleetMirrorRecovery{}, err
	}
	transaction.BlockNumber = receipt.BlockNumber.Uint64()
	transaction.BlockHash = receipt.BlockHash.Hex()
	return finalizedFleetMirrorRecovery{
		Transaction: transaction, Action: action, Fleet: fleet, Hotkey: manifest.Hotkey,
		CommitmentHash: commitmentHash, FinalizedBlock: evidence.FinalizedBlock, FinalizedBlockHash: finalizedBlockHash,
	}, nil
}

// Ensure every recovered mutation has an unchanged action in the fresh plan.
// This is the plan-hash boundary that prevents a successful old transaction
// from authorizing a changed target, gas envelope, dependency, or calldata.
func validateRevisedFleetMirrorRecoveries(revised *SetupPlan, recoveries []finalizedFleetMirrorRecovery) error {
	if len(recoveries) == 0 {
		return nil
	}
	if revised == nil {
		return errors.New("revised fleet-mirror plan is unavailable")
	}
	seenActions := map[string]bool{}
	seenTransactions := map[string]bool{}
	for _, recovery := range recoveries {
		if recovery.Fleet < 1 || recovery.Transaction.ActionID != recovery.Action.ID || recovery.Transaction.IntentHash != recovery.Action.IntentHash || recovery.Transaction.BlockNumber == 0 || recovery.Transaction.BlockHash == "" || recovery.Transaction.TransactionHash == "" {
			return errors.New("fleet-mirror recovery identity is incomplete")
		}
		if seenActions[recovery.Action.ID] || seenTransactions[strings.ToLower(recovery.Transaction.TransactionHash)] {
			return errors.New("fleet-mirror recovery is duplicated")
		}
		seenActions[recovery.Action.ID] = true
		seenTransactions[strings.ToLower(recovery.Transaction.TransactionHash)] = true
		var matched bool
		for _, action := range revised.Actions {
			if action.ID == recovery.Action.ID && action.IntentHash == recovery.Action.IntentHash {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("revised plan changed recovered fleet-mirror action %s", recovery.Action.ID)
		}
	}
	return nil
}
