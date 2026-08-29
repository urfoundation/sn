// Release plan revisions preserve an immutable on-chain deployment baseline
// while allowing a newly locked binary/configuration to repair an interrupted
// campaign under a newly reviewed hash.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	substrateTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
)

// Holds one deterministic native registration ownership pair.
type plannedRegistrationIdentity struct {
	hotkey  [32]byte
	coldkey [32]byte
}

// Merges the recovery and inclusion evidence for one prior action intent.
type planRevisionTransaction struct {
	PlanHash, ActionID, IntentHash, TransactionHash string
	RecoveryBlock                                   uint64
	RecoveryBlockHash                               string
	BlockNumber                                     uint64
	BlockHash                                       string
}

// Find transaction-bearing ancestor intents that never reached durable
// postcondition verification. Each one needs a canonical failure proof before
// a revised plan may create another transaction for the same logical action.
func pendingPlanRevisionTransactions(prior *SetupPlan, entries []JournalEntry) ([]planRevisionTransaction, error) {
	if prior == nil {
		return nil, errors.New("prior plan is unavailable")
	}
	type transactionState struct {
		transaction planRevisionTransaction
		verified    bool
	}
	states := map[string]*transactionState{}
	allowedPlans := prior.allowedPlanHashes()
	for _, entry := range entries {
		if !allowedPlans[entry.PlanHash] {
			continue
		}
		key := entry.PlanHash + "\x00" + entry.ActionID + "\x00" + entry.IntentHash
		state := states[key]
		if state == nil {
			state = &transactionState{transaction: planRevisionTransaction{PlanHash: entry.PlanHash, ActionID: entry.ActionID, IntentHash: entry.IntentHash}}
			states[key] = state
		}
		if entry.Stage == StageVerified {
			state.verified = true
		}
		if entry.TransactionHash == "" {
			continue
		}
		if state.transaction.TransactionHash != "" && !strings.EqualFold(state.transaction.TransactionHash, entry.TransactionHash) {
			return nil, fmt.Errorf("plan %s action %s intent %s has multiple transactions", entry.PlanHash, entry.ActionID, entry.IntentHash)
		}
		state.transaction.TransactionHash = entry.TransactionHash
		if entry.RecoveryBlock != 0 {
			state.transaction.RecoveryBlock = entry.RecoveryBlock
			state.transaction.RecoveryBlockHash = entry.RecoveryBlockHash
		}
		if entry.BlockNumber != 0 {
			state.transaction.BlockNumber = entry.BlockNumber
			state.transaction.BlockHash = entry.BlockHash
		}
	}
	var pending []planRevisionTransaction
	for _, state := range states {
		if !state.verified && state.transaction.TransactionHash != "" {
			pending = append(pending, state.transaction)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].PlanHash != pending[j].PlanHash {
			return pending[i].PlanHash < pending[j].PlanHash
		}
		if pending[i].ActionID != pending[j].ActionID {
			return pending[i].ActionID < pending[j].ActionID
		}
		return pending[i].IntentHash < pending[j].IntentHash
	})
	return pending, nil
}

// Prove that one prior native transaction is finalized and dispatch-failed.
func validateFailedSubstrateRevisionTransaction(ctx context.Context, chain *crv4.Chain, transaction planRevisionTransaction) error {
	txHash, err := substrateTypes.NewHashFromHexString(transaction.TransactionHash)
	if err != nil {
		return err
	}
	proveFailure := func(blockHash substrateTypes.Hash) error {
		verificationErr := chain.VerifyFinalizedExtrinsic(blockHash, txHash)
		var dispatchError *crv4.FinalizedDispatchError
		if errors.As(verificationErr, &dispatchError) {
			return nil
		}
		if verificationErr == nil {
			return errors.New("prior native transaction succeeded without durable postcondition verification")
		}
		return fmt.Errorf("prior native transaction outcome is not a canonical dispatch failure: %w", verificationErr)
	}
	if transaction.BlockNumber != 0 && transaction.BlockHash != "" {
		blockHash, hashErr := substrateTypes.NewHashFromHexString(transaction.BlockHash)
		if hashErr != nil {
			return hashErr
		}
		finalizedHash, finalizedErr := chain.API.RPC.Chain.GetFinalizedHead()
		if finalizedErr != nil {
			return finalizedErr
		}
		finalizedHeader, finalizedErr := chain.API.RPC.Chain.GetHeader(finalizedHash)
		if finalizedErr != nil {
			return finalizedErr
		}
		canonicalHash, canonicalErr := chain.API.RPC.Chain.GetBlockHash(transaction.BlockNumber)
		if canonicalErr != nil {
			return canonicalErr
		}
		if uint64(finalizedHeader.Number) < transaction.BlockNumber || canonicalHash != blockHash {
			return errors.New("prior native transaction inclusion is not canonical and finalized")
		}
		return proveFailure(blockHash)
	}
	if transaction.RecoveryBlock == 0 {
		return errors.New("prior native transaction has no recovery checkpoint")
	}
	_, found, findErr := chain.FindFinalizedExtrinsic(ctx, txHash, transaction.RecoveryBlock)
	var dispatchError *crv4.FinalizedDispatchError
	if errors.As(findErr, &dispatchError) {
		return nil
	}
	if findErr != nil {
		return fmt.Errorf("scan prior native transaction: %w", findErr)
	}
	if found {
		return errors.New("prior native transaction succeeded without durable postcondition verification")
	}
	return errors.New("prior native transaction is absent from finalized history and may still be pending")
}

// Prove that one prior EVM transaction has a canonical finalized revert.
func validateFailedEVMRevisionTransaction(ctx context.Context, client *ethclient.Client, transaction planRevisionTransaction) error {
	receipt, err := client.TransactionReceipt(ctx, common.HexToHash(transaction.TransactionHash))
	if errors.Is(err, ethereum.NotFound) {
		return errors.New("prior EVM transaction has no canonical receipt and may still be pending")
	}
	if err != nil {
		return err
	}
	finalized, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return err
	}
	canonicalHeader, err := client.HeaderByNumber(ctx, receipt.BlockNumber)
	if err != nil {
		return err
	}
	if canonicalHeader == nil || !receiptIsCanonicalAndFinalized(finalized.Number, receipt, canonicalHeader.Hash().Hex()) {
		return errors.New("prior EVM transaction receipt is not canonical and finalized")
	}
	if transaction.BlockNumber != 0 && (receipt.BlockNumber.Uint64() != transaction.BlockNumber || !strings.EqualFold(receipt.BlockHash.Hex(), transaction.BlockHash)) {
		return errors.New("prior EVM receipt does not match its journaled inclusion")
	}
	if receipt.Status == ethTypes.ReceiptStatusSuccessful {
		return errors.New("prior EVM transaction succeeded without durable postcondition verification")
	}
	if receipt.Status != ethTypes.ReceiptStatusFailed {
		return fmt.Errorf("prior EVM transaction has unsupported receipt status %d", receipt.Status)
	}
	return nil
}

// Require a chain-proven revert for every unverified transaction in the plan
// lineage. A missing artifact, pending transaction, successful mutation, or
// observer error blocks revision rather than risking a duplicate side effect.
func validatePlanRevisionTransactions(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry) error {
	pending, err := pendingPlanRevisionTransactions(prior, entries)
	if err != nil || len(pending) == 0 {
		return err
	}
	var substrateChain *crv4.Chain
	var evmClient *ethclient.Client
	defer func() {
		if substrateChain != nil {
			substrateChain.API.Client.Close()
		}
		if evmClient != nil {
			evmClient.Close()
		}
	}()
	for _, transaction := range pending {
		stem := stringsTrim0x(transaction.TransactionHash)
		scalePath := filepath.Join(stateDir, "transactions", stem+".scale")
		rlpPath := filepath.Join(stateDir, "transactions", stem+".rlp")
		_, scaleErr := os.Stat(scalePath)
		_, rlpErr := os.Stat(rlpPath)
		hasScale := scaleErr == nil
		hasRLP := rlpErr == nil
		if (scaleErr != nil && !errors.Is(scaleErr, os.ErrNotExist)) || (rlpErr != nil && !errors.Is(rlpErr, os.ErrNotExist)) {
			return errors.Join(scaleErr, rlpErr)
		}
		if hasScale == hasRLP {
			return fmt.Errorf("prior transaction %s must have exactly one native SCALE or EVM RLP artifact", transaction.TransactionHash)
		}
		if hasScale {
			raw, readErr := os.ReadFile(scalePath)
			if readErr != nil {
				return readErr
			}
			digest := blake2b.Sum256(raw)
			if !strings.EqualFold(substrateTypes.Hash(digest).Hex(), transaction.TransactionHash) {
				return fmt.Errorf("prior native transaction artifact hash does not match %s", transaction.TransactionHash)
			}
			if substrateChain == nil {
				substrateChain, err = crv4.DialChain(cfg.OperationalSubstrate)
				if err != nil {
					return err
				}
			}
			if err := validateFailedSubstrateRevisionTransaction(ctx, substrateChain, transaction); err != nil {
				return fmt.Errorf("plan %s action %s: %w", transaction.PlanHash, transaction.ActionID, err)
			}
			continue
		}
		raw, readErr := os.ReadFile(rlpPath)
		if readErr != nil {
			return readErr
		}
		var signed ethTypes.Transaction
		if decodeErr := signed.UnmarshalBinary(raw); decodeErr != nil {
			return fmt.Errorf("decode prior EVM transaction artifact %s: %w", transaction.TransactionHash, decodeErr)
		}
		if !strings.EqualFold(signed.Hash().Hex(), transaction.TransactionHash) {
			return fmt.Errorf("prior EVM transaction artifact hash does not match %s", transaction.TransactionHash)
		}
		if evmClient == nil {
			evmClient, err = ethclient.DialContext(ctx, cfg.OperationalEVM)
			if err != nil {
				return err
			}
		}
		if err := validateFailedEVMRevisionTransaction(ctx, evmClient, transaction); err != nil {
			return fmt.Errorf("plan %s action %s: %w", transaction.PlanHash, transaction.ActionID, err)
		}
	}
	return nil
}

// Derive the exact hotkey/coldkey pairs for controlled registration roles.
func expectedRegistrationIdentities(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, labels []string) (map[string]plannedRegistrationIdentity, error) {
	result := make(map[string]plannedRegistrationIdentity, len(labels))
	nativeColdkeys := map[string]string{}
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		nativeColdkeys[churnHotkeyLabel(churn)] = churnColdkeyLabel(churn)
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		nativeColdkeys[fleetHotkeyLabel(fleet)] = fleetColdkeyLabel(fleet)
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		nativeColdkeys[validatorHotkeyLabel(validator)] = fmt.Sprintf("validator-%d-coldkey", validator)
	}
	var deployment *ContractDeployment
	for _, label := range labels {
		hotkey, err := roleBytes32(roles, label)
		if err != nil {
			return nil, err
		}
		var coldkey [32]byte
		if coldLabel := nativeColdkeys[label]; coldLabel != "" {
			coldkey, err = roleBytes32(roles, coldLabel)
		} else {
			for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
				if label == fmt.Sprintf("operator-%d-deposit-hotkey", operator) {
					address, addressErr := roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", operator))
					if addressErr != nil {
						return nil, addressErr
					}
					coldkey = ss58Mirror(address)
				}
			}
			if coldkey == ([32]byte{}) {
				if deployment == nil {
					deployment, err = loadContractDeployment(stateDir)
					if err != nil {
						return nil, fmt.Errorf("load contract deployment for registered role %s: %w", label, err)
					}
				}
				if label == "escrow-hotkey" {
					coldkey = ss58Mirror(deployment.SettlementVault)
				} else {
					matchedPool := false
					for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
						if label == fmt.Sprintf("operator-%d-pool-hotkey", operator) {
							coldkey = ss58Mirror(deployment.SettlementVault)
							matchedPool = true
							break
						}
					}
					if !matchedPool && label != "escrow-hotkey" {
						return nil, fmt.Errorf("registered role %s has no expected coldkey rule", label)
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		result[label] = plannedRegistrationIdentity{hotkey: hotkey, coldkey: coldkey}
	}
	return result, nil
}

// Compare one finalized UID fact to its deterministic role identity.
func registrationIdentityMatches(fact ExistingUIDFact, uid uint16, expected plannedRegistrationIdentity) bool {
	if fact.UID != uid || fact.RegistrationBlock == 0 || fact.SubnetOwner {
		return false
	}
	hotkey, hotkeyErr := decodeHex32("revision hotkey", fact.Hotkey)
	coldkey, coldkeyErr := decodeHex32("revision coldkey", fact.Coldkey)
	return hotkeyErr == nil && coldkeyErr == nil && hotkey == expected.hotkey && coldkey == expected.coldkey
}

// Require the current subnet to be either an exact prefix of the initial
// deterministic registration order or one of the bounded tournament states.
// Any external insertion, ownership change, gap, or unexpected prune aborts
// the revision before a new approval hash is produced.
func validatePlanRevisionTopology(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, roles *RoleSecrets) error {
	if cfg == nil || prior == nil || current == nil || roles == nil {
		return errors.New("plan revision topology inputs are incomplete")
	}
	baseline := prior.LiveFacts.ExistingUIDs
	if len(baseline) == 0 || int(prior.LiveFacts.ExistingUIDCount) != len(baseline) || int(current.ExistingUIDCount) != len(current.ExistingUIDs) || len(current.ExistingUIDs) < len(baseline) {
		return fmt.Errorf("plan revision has inconsistent baseline/current UID facts: baseline=%d/%d current=%d/%d", prior.LiveFacts.ExistingUIDCount, len(baseline), current.ExistingUIDCount, len(current.ExistingUIDs))
	}
	if current.SubnetOwnerHotkey != prior.LiveFacts.SubnetOwnerHotkey || current.UIDZeroHotkey != prior.LiveFacts.UIDZeroHotkey {
		return errors.New("plan revision changed the subnet owner or UID-zero hotkey")
	}
	for index := range baseline {
		if current.ExistingUIDs[index] != baseline[index] {
			return fmt.Errorf("plan revision bootstrap UID %d changed: current=%+v baseline=%+v", index, current.ExistingUIDs[index], baseline[index])
		}
	}
	initialLabels := initialTopologyRoleLabels(cfg.Config.Topology)
	progress := len(current.ExistingUIDs) - len(baseline)
	if progress > len(initialLabels) {
		return fmt.Errorf("plan revision current controlled UIDs %d exceed initial topology %d", progress, len(initialLabels))
	}
	labelsNeeded := append([]string(nil), initialLabels[:progress]...)
	if progress == len(initialLabels) {
		for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
			labelsNeeded = append(labelsNeeded, fleetHotkeyLabel(cfg.Config.Topology.HeadFleets+challenger))
		}
	}
	identities, err := expectedRegistrationIdentities(cfg, stateDir, roles, labelsNeeded)
	if err != nil {
		return err
	}
	if progress < len(initialLabels) {
		for index, label := range initialLabels[:progress] {
			uid := uint16(len(baseline) + index)
			if !registrationIdentityMatches(current.ExistingUIDs[len(baseline)+index], uid, identities[label]) {
				return fmt.Errorf("plan revision UID %d is not the expected registration prefix role %s", uid, label)
			}
		}
		return nil
	}
	for replacements := 0; replacements <= cfg.Config.Topology.ChallengerFleets; replacements++ {
		candidate := append([]string(nil), initialLabels...)
		for index := 0; index < replacements; index++ {
			candidate[index] = fleetHotkeyLabel(cfg.Config.Topology.HeadFleets + index + 1)
		}
		matched := true
		for index, label := range candidate {
			uid := uint16(len(baseline) + index)
			if !registrationIdentityMatches(current.ExistingUIDs[len(baseline)+index], uid, identities[label]) {
				matched = false
				break
			}
		}
		if matched {
			return nil
		}
	}
	return errors.New("plan revision full topology is neither the approved initial set nor a bounded challenger replacement state")
}

// Keep immutable deployment identity and deterministic roles across revisions.
func validatePlanRevisionIdentity(cfg *ResolvedConfig, prior *SetupPlan, roles PublicRoles) error {
	if cfg == nil || prior == nil {
		return errors.New("plan revision identity is unavailable")
	}
	if prior.Release != "1.0" || prior.DeploymentID != cfg.Config.Deployment.DeploymentID || prior.ChainID != testnetChainID || prior.GenesisHash != testnetGenesis || prior.Netuid != cfg.Netuid || prior.Owner != cfg.WalletPublic || prior.PolicyHash != cfg.PolicyHash {
		return errors.New("prior plan does not describe the same release deployment, chain, owner, netuid, and policy")
	}
	currentRoles, err := canonicalHashHex(roles)
	if err != nil {
		return err
	}
	priorRoles, err := canonicalHashHex(prior.Roles)
	if err != nil || currentRoles != priorRoles {
		return errors.New("prior plan deterministic public roles changed")
	}
	return validatePlanBudget(prior)
}

// Name the registration that terminally consumes one independently funded
// native coldkey. Other funding roles retain their current revised targets.
func registrationConsumerForFunding(actionID string) (string, bool) {
	for _, pair := range []struct {
		fundingPrefix, registrationPrefix string
	}{
		{fundingPrefix: "churn.fund.", registrationPrefix: "churn.register."},
		{fundingPrefix: "fleet.fund.", registrationPrefix: "fleet.register."},
		{fundingPrefix: "validator.fund.", registrationPrefix: "validator.register."},
	} {
		if !strings.HasPrefix(actionID, pair.fundingPrefix) {
			continue
		}
		suffix := strings.TrimPrefix(actionID, pair.fundingPrefix)
		index, err := strconv.Atoi(suffix)
		if err != nil || index <= 0 || strconv.Itoa(index) != suffix {
			return "", false
		}
		return pair.registrationPrefix + suffix, true
	}
	return "", false
}

// Retain an ancestor funding intent only after both its exact transfer and its
// unchanged consuming registration have durable proofs. An interrupted or
// changed registration keeps the newly reviewed repair funding instead.
func preserveConsumedRegistrationFunding(revised, prior *SetupPlan, entries []JournalEntry) error {
	if revised == nil || prior == nil {
		return errors.New("revised and prior plans are required to preserve consumed funding")
	}
	priorActions := make(map[string]Action, len(prior.Actions))
	revisedActions := make(map[string]Action, len(revised.Actions))
	for _, action := range prior.Actions {
		priorActions[action.ID] = action
	}
	for _, action := range revised.Actions {
		revisedActions[action.ID] = action
	}
	verifiedIntents := map[string]bool{}
	allowedPlans := prior.allowedPlanHashes()
	for _, entry := range entries {
		if allowedPlans[entry.PlanHash] && entry.Stage == StageVerified {
			verifiedIntents[entry.ActionID+"\x00"+entry.IntentHash] = true
		}
	}
	for index, revisedFunding := range revised.Actions {
		consumerID, ok := registrationConsumerForFunding(revisedFunding.ID)
		if !ok {
			continue
		}
		priorFunding, fundingExists := priorActions[revisedFunding.ID]
		priorConsumer, priorConsumerExists := priorActions[consumerID]
		revisedConsumer, revisedConsumerExists := revisedActions[consumerID]
		if !fundingExists || !priorConsumerExists || !revisedConsumerExists || priorConsumer.IntentHash != revisedConsumer.IntentHash {
			continue
		}
		if !verifiedIntents[priorFunding.ID+"\x00"+priorFunding.IntentHash] || !verifiedIntents[priorConsumer.ID+"\x00"+priorConsumer.IntentHash] {
			continue
		}
		revised.Actions[index] = priorFunding
	}
	maximumSpend, err := maximumActionSpend(revised.Actions)
	if err != nil {
		return err
	}
	revised.MaximumSpend = maximumSpend
	return nil
}

// Build a deterministic revision from already finalized, caller-supplied facts.
func buildPlanRevisionFromFacts(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, entries []JournalEntry, generatedAt time.Time) (*SetupPlan, error) {
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		return nil, err
	}
	if err := validatePlanRevisionIdentity(cfg, prior, roles); err != nil {
		return nil, err
	}
	roleSecrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, err
	}
	if err := validatePlanRevisionTopology(cfg, stateDir, prior, current, roleSecrets); err != nil {
		return nil, err
	}
	remaining, err := remainingPlanSpend(prior, entries)
	if err != nil {
		return nil, err
	}
	normalized := *current
	normalized.ExistingUIDCount = prior.LiveFacts.ExistingUIDCount
	normalized.ExistingUIDs = append([]ExistingUIDFact(nil), prior.LiveFacts.ExistingUIDs...)
	normalized.SubnetOwnerHotkey = prior.LiveFacts.SubnetOwnerHotkey
	normalized.UIDZeroHotkey = prior.LiveFacts.UIDZeroHotkey
	spentAlpha := prior.MaximumSpend.AlphaRao - remaining.AlphaRao
	if available, ok := checkedAdd(normalized.AlphaAvailableRao, spentAlpha); ok {
		normalized.AlphaAvailableRao = available
	} else {
		return nil, errors.New("plan revision alpha availability overflow")
	}
	revised, err := buildPlan(cfg, &normalized, roles, generatedAt)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, hash := range append(append([]string(nil), prior.PriorPlanHashes...), prior.PlanHash) {
		if !seen[hash] {
			revised.PriorPlanHashes = append(revised.PriorPlanHashes, hash)
			seen[hash] = true
		}
	}
	if err := preserveConsumedRegistrationFunding(revised, prior, entries); err != nil {
		return nil, fmt.Errorf("preserve consumed registration funding: %w", err)
	}
	revised.PlanHash, err = revised.hash()
	if err != nil {
		return nil, err
	}
	if err := validatePlanBudget(revised); err != nil {
		return nil, err
	}
	revisedRemaining, err := remainingPlanSpend(revised, entries)
	if err != nil {
		return nil, err
	}
	if err := validateApprovedSetupFacts(revised, current, revisedRemaining); err != nil {
		return nil, fmt.Errorf("revised plan is not affordable from current finalized state: %w", err)
	}
	return revised, nil
}

// Recheck live safety, transaction outcomes, and topology before revising.
func BuildPlanRevision(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry) (*SetupPlan, error) {
	remaining, err := remainingPlanSpend(prior, entries)
	if err != nil {
		return nil, err
	}
	doctor := runDoctor(ctx, cfg, &doctorPlanBudget{Plan: prior, Remaining: remaining})
	if err := doctor.Error(); err != nil {
		return nil, fmt.Errorf("doctor must pass before revising the plan: %w", err)
	}
	if err := validatePlanRevisionTransactions(ctx, cfg, stateDir, prior, entries); err != nil {
		return nil, fmt.Errorf("prior transaction revision safety: %w", err)
	}
	current, err := ReadSetupFacts(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("read finalized setup facts for plan revision: %w", err)
	}
	return buildPlanRevisionFromFacts(cfg, stateDir, prior, current, entries, time.Now().UTC())
}

// Resolve the active immutable plan, construct an initial plan, or construct a
// read-only revision when a valid stored ancestor no longer matches the locked
// release/configuration.
func BuildPlanForState(ctx context.Context, cfg *ResolvedConfig, stateDir string) (*SetupPlan, error) {
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err == nil {
		return plan, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return BuildPlan(ctx, cfg)
	}
	if !errors.Is(err, errPersistedPlanIdentityMismatch) {
		return nil, err
	}
	prior, err := readPersistedPlan(stateDir)
	if err != nil {
		return nil, err
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		return nil, err
	}
	return BuildPlanRevision(ctx, cfg, stateDir, prior, entries)
}
