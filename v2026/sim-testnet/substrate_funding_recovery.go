// Finalized native-funding recovery proves an interrupted balance transfer
// before a revised plan is allowed to converge it without rebroadcast.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Binds one successful transfer to its source action, role, and exact balance
// delta. The unchanged revised action then observes the funded balance and
// persists the missing postcondition without sending another transaction.
type finalizedSubstrateFundingRecovery struct {
	Transaction        planRevisionTransaction
	Action             Action
	RoleLabel          string
	Account            [32]byte
	RecoveryBalanceRao uint64
	TransferRao        uint64
}

// Locate the sole randomized sr25519 field in a canonical signed-version-4
// extrinsic. Every surrounding byte remains deterministic and approval-bound;
// canonical successful dispatch independently proves this signature itself.
func signedSubstrateSignatureRange(raw []byte) (int, int, error) {
	if len(raw) < 2 {
		return 0, 0, errors.New("signed substrate transaction is truncated")
	}
	var declared uint64
	prefix := 0
	switch raw[0] & 3 {
	case 0:
		declared, prefix = uint64(raw[0]>>2), 1
	case 1:
		if len(raw) < 2 {
			return 0, 0, errors.New("signed substrate compact length is truncated")
		}
		declared, prefix = uint64(uint16(raw[0])|uint16(raw[1])<<8)>>2, 2
	case 2:
		if len(raw) < 4 {
			return 0, 0, errors.New("signed substrate compact length is truncated")
		}
		declared = uint64(uint32(raw[0])|uint32(raw[1])<<8|uint32(raw[2])<<16|uint32(raw[3])<<24) >> 2
		prefix = 4
	default:
		return 0, 0, errors.New("signed substrate transaction uses unsupported big-integer length")
	}
	if declared != uint64(len(raw)-prefix) || prefix >= len(raw) || raw[prefix] != 0x84 {
		return 0, 0, errors.New("signed substrate transaction has a noncanonical length or version")
	}
	offset := prefix + 1
	if offset+1+32+1+64 > len(raw) || raw[offset] != 0 {
		return 0, 0, errors.New("signed substrate transaction does not use an account-id signer")
	}
	offset += 1 + 32
	if raw[offset] != 1 {
		return 0, 0, errors.New("signed substrate transaction does not use sr25519")
	}
	start := offset + 1
	return start, start + 64, nil
}

// Compare the complete deterministic envelope while permitting only
// sr25519's randomized signature bytes to differ.
func validateSignedSubstrateCallMatches(actual, expected []byte) error {
	actualStart, actualEnd, err := signedSubstrateSignatureRange(actual)
	if err != nil {
		return err
	}
	expectedStart, expectedEnd, err := signedSubstrateSignatureRange(expected)
	if err != nil {
		return fmt.Errorf("reconstructed substrate transaction: %w", err)
	}
	if len(actual) != len(expected) || actualStart != expectedStart || actualEnd != expectedEnd || !bytes.Equal(actual[:actualStart], expected[:expectedStart]) || !bytes.Equal(actual[actualEnd:], expected[expectedEnd:]) {
		return errors.New("signed substrate transaction differs outside its randomized sr25519 signature")
	}
	return nil
}

// Load one hash-authenticated source plan from the approved revision lineage.
func loadSubstrateFundingLineagePlan(stateDir string, prior *SetupPlan, planHash string) (*SetupPlan, error) {
	if prior == nil || !prior.allowedPlanHashes()[planHash] {
		return nil, fmt.Errorf("plan %s is outside the approved substrate-funding lineage", planHash)
	}
	if strings.EqualFold(prior.PlanHash, planHash) {
		return prior, nil
	}
	plan, err := readPersistedPlanFile(filepath.Join(stateDir, "plans", stringsTrim0x(planHash)+".json"))
	if err != nil {
		return nil, fmt.Errorf("read substrate-funding ancestor plan %s: %w", planHash, err)
	}
	if !strings.EqualFold(plan.PlanHash, planHash) {
		return nil, fmt.Errorf("substrate-funding ancestor identity %s differs from %s", plan.PlanHash, planHash)
	}
	return plan, nil
}

// Resolve the deterministic destination label and target for every native
// funding family used by the real executor.
func substrateFundingRole(cfg *ResolvedConfig, actionID string) (string, string, error) {
	if cfg == nil || cfg.Config == nil {
		return "", "", errors.New("substrate-funding topology is unavailable")
	}
	type family struct {
		prefix  string
		maximum int
		label   func(int) string
		target  func(int) string
	}
	families := []family{
		{
			prefix: "churn.fund.", maximum: cfg.Config.Topology.ChurnFloorUIDs,
			label: churnColdkeyLabel, target: func(index int) string { return fmt.Sprintf("churn-coldkey:%d", index) },
		},
		{
			prefix: "fleet.fund.", maximum: cfg.Config.Topology.fleetCandidates(),
			label: fleetColdkeyLabel, target: func(index int) string {
				if index <= cfg.Config.Topology.HeadFleets {
					return fmt.Sprintf("head-fleet-coldkey:%d", index)
				}
				return fmt.Sprintf("challenger-fleet-coldkey:%d", index)
			},
		},
		{
			prefix: "fleet.fund-hotkey.", maximum: cfg.Config.Topology.fleetCandidates(),
			label: fleetHotkeyLabel, target: func(index int) string {
				if index <= cfg.Config.Topology.HeadFleets {
					return fmt.Sprintf("head-fleet-hotkey:%d", index)
				}
				return fmt.Sprintf("challenger-fleet-hotkey:%d", index)
			},
		},
		{
			prefix: "validator.fund.", maximum: cfg.Config.Topology.Validators,
			label:  func(index int) string { return fmt.Sprintf("validator-%d-coldkey", index) },
			target: func(index int) string { return fmt.Sprintf("validator-coldkey:%d", index) },
		},
	}
	for _, candidate := range families {
		if !strings.HasPrefix(actionID, candidate.prefix) {
			continue
		}
		suffix := strings.TrimPrefix(actionID, candidate.prefix)
		index, err := strconv.Atoi(suffix)
		if err != nil || index < 1 || index > candidate.maximum || strconv.Itoa(index) != suffix {
			return "", "", fmt.Errorf("substrate-funding action %q is out of range", actionID)
		}
		return candidate.label(index), candidate.target(index), nil
	}
	return "", "", fmt.Errorf("action %q is not recoverable substrate funding", actionID)
}

// Resolve only the exact source-plan intent and release-shaped funding action.
func exactSubstrateFundingPlanAction(cfg *ResolvedConfig, plan *SetupPlan, actionID, intentHash string) (Action, string, error) {
	if plan == nil {
		return Action{}, "", errors.New("substrate-funding plan is unavailable")
	}
	roleLabel, target, err := substrateFundingRole(cfg, actionID)
	if err != nil {
		return Action{}, "", err
	}
	var result *Action
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID != actionID || action.IntentHash != intentHash {
			continue
		}
		if result != nil {
			return Action{}, "", errors.New("plan has duplicate exact substrate-funding actions")
		}
		copy := *action
		result = &copy
	}
	if result == nil {
		return Action{}, "", fmt.Errorf("plan %s has no substrate-funding intent %s", plan.PlanHash, intentHash)
	}
	if result.Kind != "substrate-extrinsic" || result.Target != target || result.Spend.TAORao == 0 || result.Spend.AlphaRao != 0 || !result.Spend.EVMGasWei.IsZero() || result.Spend.Registrations != 0 || result.Spend.SubnetCreations != 0 {
		return Action{}, "", errors.New("substrate-funding action does not have the release transfer shape")
	}
	return *result, roleLabel, nil
}

// Prove the exact pre-transfer delta and both immediate and current
// no-broadcast postconditions without relying on arithmetic that can wrap.
func validateSubstrateFundingRecoveryBalances(action Action, recoveryBalance, inclusionBalance, currentBalance uint64) (uint64, error) {
	if action.Spend.TAORao == 0 || recoveryBalance >= action.Spend.TAORao {
		return 0, errors.New("substrate-funding recovery has no positive approved transfer delta")
	}
	transfer := action.Spend.TAORao - recoveryBalance
	if inclusionBalance < action.Spend.TAORao {
		return 0, fmt.Errorf("substrate-funding inclusion balance %d is below approved target %d", inclusionBalance, action.Spend.TAORao)
	}
	if currentBalance < action.Spend.TAORao {
		return 0, fmt.Errorf("substrate-funding current balance %d no longer converges at target %d", currentBalance, action.Spend.TAORao)
	}
	return transfer, nil
}

// Decode one JSON-origin integer without accepting a rounded float, sign,
// fractional value, or implementation-specific integer width.
func substrateFundingObservedRao(value any) (uint64, error) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed < 0 || typed >= float64(math.MaxUint64) || math.Trunc(typed) != typed {
			return 0, errors.New("substrate-funding observed balance is not an unsigned integer")
		}
		return uint64(typed), nil
	case json.Number:
		value, err := strconv.ParseUint(string(typed), 10, 64)
		return value, err
	case uint64:
		return typed, nil
	default:
		return 0, fmt.Errorf("substrate-funding observed balance has type %T", value)
	}
}

// Require the complete generic action envelope plus the funding observation
// and its conservative balance target. Action postconditions always add kind
// and target before the funding-specific fields; accepting a reduced synthetic
// shape would make revision recovery diverge from every durable v4 receipt.
func validateSubstrateFundingObservedPostcondition(observed map[string]any, action Action, roleLabel, account string) error {
	if len(observed) != 5 || observed["kind"] != action.Kind || observed["target"] != action.Target || observed["role"] != roleLabel {
		return errors.New("substrate-funding descendant observation has the wrong role or fields")
	}
	observedAccount, ok := observed["account"].(string)
	if !ok || !strings.EqualFold(observedAccount, account) {
		return errors.New("substrate-funding descendant observation has the wrong account")
	}
	balance, err := substrateFundingObservedRao(observed["free_balance_rao"])
	if err != nil || balance < action.Spend.TAORao {
		return stateMismatchError(err, "substrate-funding descendant balance %d is below target %d", balance, action.Spend.TAORao)
	}
	return nil
}

// Recognize a later exact no-broadcast verification only after its ordered,
// hash-bound operational and independent observations both prove the target.
func verifiedSubstrateFundingDescendant(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry, transaction planRevisionTransaction, action Action, roleLabel string, account [32]byte) (bool, error) {
	if cfg == nil || prior == nil || transaction.JournalSequence == 0 || roleLabel == "" || account == ([32]byte{}) {
		return false, errors.New("substrate-funding descendant context is incomplete")
	}
	accountHex := "0x" + hex.EncodeToString(account[:])
	executor := &Executor{cfg: cfg, plan: prior, stateDir: stateDir}
	for _, entry := range entries {
		if entry.Sequence <= transaction.JournalSequence || entry.PlanHash == transaction.PlanHash || !prior.allowedPlanHashes()[entry.PlanHash] || entry.ActionID != transaction.ActionID || entry.IntentHash != transaction.IntentHash || entry.Stage != StageVerified {
			continue
		}
		descendantPlan, err := loadSubstrateFundingLineagePlan(stateDir, prior, entry.PlanHash)
		if err != nil {
			return false, err
		}
		descendantAction, descendantRole, err := exactSubstrateFundingPlanAction(cfg, descendantPlan, entry.ActionID, entry.IntentHash)
		if err != nil || descendantRole != roleLabel || descendantAction.IntentHash != action.IntentHash {
			return false, stateMismatchError(err, "verified descendant substrate funding differs from %s", action.ID)
		}
		record, err := executor.readPersistedPostcondition(entry)
		if err != nil {
			return false, fmt.Errorf("verified descendant substrate-funding postcondition: %w", err)
		}
		if err := validateSubstrateFundingObservedPostcondition(record.Observed, action, roleLabel, accountHex); err != nil {
			return false, fmt.Errorf("verified descendant operational substrate funding: %w", err)
		}
		if err := validateSubstrateFundingObservedPostcondition(record.IndependentObserved, action, roleLabel, accountHex); err != nil {
			return false, fmt.Errorf("verified descendant comparison substrate funding: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// Authenticate signed bytes, canonical history, historical balance, and live
// convergence for the sole idempotent native-write recovery family.
func detectFinalizedSubstrateFundingRecovery(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry, chain *crv4.Chain, raw []byte, transaction planRevisionTransaction) (finalizedSubstrateFundingRecovery, error) {
	if cfg == nil || prior == nil || chain == nil || chain.API == nil || len(raw) == 0 || transaction.TransactionHash == "" || transaction.Signer == "" || transaction.Nonce == "" || transaction.RecoveryBlock == 0 || transaction.RecoveryBlockHash == "" || transaction.BlockNumber == 0 || transaction.BlockHash == "" {
		return finalizedSubstrateFundingRecovery{}, errors.New("successful native funding recovery context is incomplete")
	}
	sourcePlan, err := loadSubstrateFundingLineagePlan(stateDir, prior, transaction.PlanHash)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	action, roleLabel, err := exactSubstrateFundingPlanAction(cfg, sourcePlan, transaction.ActionID, transaction.IntentHash)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	if sourcePlan.Owner != cfg.WalletPublic {
		return finalizedSubstrateFundingRecovery{}, errors.New("substrate-funding source plan owner differs from the configured wallet")
	}
	signer, err := signature.KeyringPairFromSecret(cfg.WalletMaterial, 42)
	if err != nil || signer.Address != cfg.WalletPublic || signer.Address != transaction.Signer {
		return finalizedSubstrateFundingRecovery{}, stateMismatchError(err, "substrate-funding signer %s differs from wallet and journal %s", signer.Address, transaction.Signer)
	}
	nonce, err := strconv.ParseUint(transaction.Nonce, 10, 32)
	if err != nil || nonce > math.MaxUint32 {
		return finalizedSubstrateFundingRecovery{}, stateMismatchError(err, "substrate-funding nonce %q is invalid", transaction.Nonce)
	}
	transactionDigest := blake2b.Sum256(raw)
	if !strings.EqualFold(types.Hash(transactionDigest).Hex(), transaction.TransactionHash) {
		return finalizedSubstrateFundingRecovery{}, errors.New("substrate-funding transaction artifact hash differs from the journal")
	}
	transactionHash, err := types.NewHashFromHexString(transaction.TransactionHash)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	recoveryHash, err := types.NewHashFromHexString(transaction.RecoveryBlockHash)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	inclusionHash, err := types.NewHashFromHexString(transaction.BlockHash)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	finalizedHash, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	finalizedHeader, err := chain.API.RPC.Chain.GetHeader(finalizedHash)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	canonicalRecovery, recoveryErr := chain.API.RPC.Chain.GetBlockHash(transaction.RecoveryBlock)
	canonicalInclusion, inclusionErr := chain.API.RPC.Chain.GetBlockHash(transaction.BlockNumber)
	if recoveryErr != nil || inclusionErr != nil || uint64(finalizedHeader.Number) < transaction.BlockNumber || transaction.RecoveryBlock >= transaction.BlockNumber || canonicalRecovery != recoveryHash || canonicalInclusion != inclusionHash {
		return finalizedSubstrateFundingRecovery{}, stateMismatchError(errors.Join(recoveryErr, inclusionErr), "substrate-funding recovery and inclusion checkpoints are not canonical and ordered")
	}
	if err := chain.VerifyFinalizedExtrinsic(inclusionHash, transactionHash); err != nil {
		return finalizedSubstrateFundingRecovery{}, fmt.Errorf("substrate-funding finalized transaction: %w", err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	account, err := roleBytes32(roles, roleLabel)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	recoveryBalance, err := readFreeBalanceAtHash(chain, account, recoveryHash)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, fmt.Errorf("read substrate-funding recovery balance: %w", err)
	}
	inclusionBalance, err := readFreeBalanceAtHash(chain, account, inclusionHash)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, fmt.Errorf("read substrate-funding inclusion balance: %w", err)
	}
	descendantVerified, err := verifiedSubstrateFundingDescendant(cfg, stateDir, prior, entries, transaction, action, roleLabel, account)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	currentBalance := action.Spend.TAORao
	if !descendantVerified {
		currentBalance, err = readFreeBalanceAtHash(chain, account, finalizedHash)
		if err != nil {
			return finalizedSubstrateFundingRecovery{}, fmt.Errorf("read substrate-funding current balance: %w", err)
		}
	}
	transfer, err := validateSubstrateFundingRecoveryBalances(action, recoveryBalance, inclusionBalance, currentBalance)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	manager := &SubstrateManager{chain: chain, cfg: cfg}
	call, err := manager.FundCall(account, transfer)
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	expectedRaw, err := encodeSignedSubstrateCall(chain, signer, call, uint32(nonce))
	if err != nil {
		return finalizedSubstrateFundingRecovery{}, err
	}
	if err := validateSignedSubstrateCallMatches(raw, expectedRaw); err != nil {
		return finalizedSubstrateFundingRecovery{}, fmt.Errorf("substrate-funding signed bytes differ from the exact approved transfer: %w", err)
	}
	return finalizedSubstrateFundingRecovery{
		Transaction: transaction, Action: action, RoleLabel: roleLabel, Account: account,
		RecoveryBalanceRao: recoveryBalance, TransferRao: transfer,
	}, nil
}

// Ensure every recovered transfer remains an exact action in the fresh plan.
func validateRevisedSubstrateFundingRecoveries(revised *SetupPlan, recoveries []finalizedSubstrateFundingRecovery) error {
	if len(recoveries) == 0 {
		return nil
	}
	if revised == nil {
		return errors.New("revised substrate-funding plan is unavailable")
	}
	seenActions := map[string]bool{}
	seenTransactions := map[string]bool{}
	for _, recovery := range recoveries {
		if recovery.RoleLabel == "" || recovery.Account == ([32]byte{}) || recovery.TransferRao == 0 || recovery.Transaction.ActionID != recovery.Action.ID || recovery.Transaction.IntentHash != recovery.Action.IntentHash || recovery.Transaction.TransactionHash == "" || recovery.Transaction.BlockNumber == 0 || recovery.Transaction.BlockHash == "" {
			return errors.New("substrate-funding recovery identity is incomplete")
		}
		transactionHash := strings.ToLower(recovery.Transaction.TransactionHash)
		if seenActions[recovery.Action.ID] || seenTransactions[transactionHash] {
			return errors.New("substrate-funding recovery is duplicated")
		}
		seenActions[recovery.Action.ID] = true
		seenTransactions[transactionHash] = true
		matches := 0
		for _, action := range revised.Actions {
			if action.ID == recovery.Action.ID && action.IntentHash == recovery.Action.IntentHash {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("revised plan changed recovered substrate-funding action %s", recovery.Action.ID)
		}
	}
	return nil
}
