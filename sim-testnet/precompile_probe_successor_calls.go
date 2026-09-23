// Replacement resumes explain deployer nonces using only the original approved
// probe call sequence, canonical signed transactions, and phase evidence.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Read-only battery and dividend observations do not consume deployer nonces.
func precompileProbeSuccessorTransactionIds() []string {
	return []string{"precompile.probe-deploy", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.transfer-out"}
}

// Each consumed nonce needs one exact finalized action in chronological order.
func precompileProbeSuccessorPrefix(plan *SetupPlan, entries []JournalEntry, nonce uint64) ([]JournalEntry, error) {
	if err := validatePrecompileProbeSuccessorActions(plan); err != nil {
		return nil, err
	}
	successor := plan.PrecompileProbeSuccessor
	if successor == nil {
		return nil, errors.New("precompile probe successor is absent")
	}
	ids := precompileProbeSuccessorTransactionIds()
	if nonce < successor.DeployerNonce || nonce-successor.DeployerNonce > uint64(len(ids)) {
		return nil, errors.New("precompile probe successor nonce exceeds its approved call sequence")
	}
	consumed := int(nonce - successor.DeployerNonce)
	allowed := plan.allowedPlanHashes()
	prefix := make([]JournalEntry, 0, consumed)
	boundary := precompileProbeSuccessorJournalBoundary(successor)
	previous := boundary
	for index, actionId := range ids {
		action, err := exactPlanActionByID(plan, actionId)
		if err != nil {
			return nil, err
		}
		var matched *JournalEntry
		for _, entry := range entries {
			if entry.Sequence <= boundary || entry.ActionID != actionId || entry.Stage != StageFinalized {
				continue
			}
			if entry.DeploymentID != plan.DeploymentID || !allowed[entry.PlanHash] || entry.IntentHash != action.IntentHash {
				return nil, fmt.Errorf("precompile probe successor %s has an unapproved finalized identity", actionId)
			}
			if matched != nil {
				if index == 0 || entry.PlanHash != matched.PlanHash || entry.TransactionHash != matched.TransactionHash || entry.BlockNumber != matched.BlockNumber || entry.BlockHash != matched.BlockHash || entry.RecoveryBlock != matched.RecoveryBlock || entry.RecoveryBlockHash != matched.RecoveryBlockHash {
					return nil, fmt.Errorf("precompile probe successor %s has conflicting or repeated CREATE finalization", actionId)
				}
				continue
			}
			copy := entry
			matched = &copy
		}
		if index >= consumed {
			if matched != nil {
				return nil, errors.New("precompile probe successor finalized history exceeds observed nonce")
			}
			continue
		}
		if matched == nil || matched.Sequence <= previous || !validConformanceTransaction(matched.TransactionHash, matched.BlockHash, matched.BlockNumber) {
			return nil, fmt.Errorf("precompile probe successor %s has no exact contiguous finalized transaction", actionId)
		}
		previous = matched.Sequence
		prefix = append(prefix, *matched)
	}
	return prefix, nil
}

// Derives calldata from the fixed roles and previously recorded dynamic amounts.
func precompileProbeSuccessorCall(plan *SetupPlan, evidence *PrecompileConformanceEvidence, actionId string) ([]byte, *big.Int, JournalEntry, error) {
	parsed, err := abi.JSON(strings.NewReader(SubnetProbeABI))
	if err != nil {
		return nil, nil, JournalEntry{}, err
	}
	sample, err := decodeHex32("successor sample hotkey", evidence.SampleHotkey)
	if err != nil {
		return nil, nil, JournalEntry{}, err
	}
	move, err := decodeHex32("successor move hotkey", evidence.MoveHotkey)
	if err != nil {
		return nil, nil, JournalEntry{}, err
	}
	recovery, err := decodeHex32("successor recovery coldkey", evidence.RecoveryColdkey)
	if err != nil {
		return nil, nil, JournalEntry{}, err
	}
	value := new(big.Int)
	var data []byte
	var recorded JournalEntry
	switch actionId {
	case "precompile.seed":
		if evidence.Seed.TAORao == 0 || evidence.Seed.TAORao != plan.LiveFacts.ProbeTAORao || evidence.Seed.BeforeRao != 0 {
			return nil, nil, recorded, errors.New("successor seed differs from the approved unfunded TAO input")
		}
		value.Mul(new(big.Int).SetUint64(evidence.Seed.TAORao), big.NewInt(1_000_000_000))
		if value.String() != evidence.Seed.ValueWei {
			return nil, nil, recorded, errors.New("successor seed changed its approved value conversion")
		}
		data, err = parsed.Pack("seedFromTao", sample, new(big.Int).SetUint64(evidence.Seed.TAORao))
		recorded.TransactionHash, recorded.BlockNumber, recorded.BlockHash = evidence.Seed.TransactionHash, evidence.Seed.BlockNumber, evidence.Seed.BlockHash
	case "precompile.move-forward", "precompile.move-back":
		step, from, to, amount := evidence.Forward, sample, move, evidence.Seed.DeltaRao/2
		if actionId == "precompile.move-back" {
			amount, err = precompileMoveObservedAmount(evidence.Forward)
			if err != nil {
				return nil, nil, recorded, fmt.Errorf("successor reverse move has no conserved forward credit: %w", err)
			}
			step, from, to = evidence.Back, move, sample
		}
		if amount == 0 || step.AmountRao != amount || step.FromBeforeRao < amount {
			return nil, nil, recorded, errors.New("successor move differs from the approved round trip")
		}
		data, err = parsed.Pack("moveRoundTrip", from, to, new(big.Int).SetUint64(amount))
		recorded.TransactionHash, recorded.BlockNumber, recorded.BlockHash = step.TransactionHash, step.BlockNumber, step.BlockHash
	case "precompile.snapshot":
		if evidence.Snapshot.BaselineRao == 0 || evidence.Snapshot.BaselineRao < evidence.Seed.AfterRao {
			return nil, nil, recorded, errors.New("successor snapshot lost the completed round trip")
		}
		data, err = parsed.Pack("snapshot", sample)
		recorded.TransactionHash, recorded.BlockNumber, recorded.BlockHash = evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockNumber, evidence.Snapshot.BlockHash
	case "precompile.transfer-out":
		if evidence.Transfer.ProbeBeforeRao <= evidence.Seed.BeforeRao || evidence.Transfer.AmountRao != evidence.Transfer.ProbeBeforeRao-evidence.Seed.BeforeRao {
			return nil, nil, recorded, errors.New("successor transfer differs from the approved recovery")
		}
		data, err = parsed.Pack("transferOut", recovery, sample, new(big.Int).SetUint64(evidence.Transfer.AmountRao))
		recorded.TransactionHash, recorded.BlockNumber, recorded.BlockHash = evidence.Transfer.TransactionHash, evidence.Transfer.BlockNumber, evidence.Transfer.BlockHash
	default:
		return nil, nil, recorded, errors.New("successor nonce names an unapproved probe call")
	}
	return data, value, recorded, err
}

// Authenticates inclusion and the exact signed call, including dynamic value.
func verifyPrecompileProbeSuccessorCall(ctx context.Context, reader contractCreationReader, head ChainHead, plan *SetupPlan, evidence *PrecompileConformanceEvidence, entry JournalEntry, nonce uint64, allowPartial bool) error {
	data, value, recorded, err := precompileProbeSuccessorCall(plan, evidence, entry.ActionID)
	if err != nil {
		return err
	}
	if recorded.TransactionHash == "" && recorded.BlockNumber == 0 && recorded.BlockHash == "" {
		if !allowPartial {
			return errors.New("successor completed prefix lost its recorded phase receipt")
		}
	} else if recorded.TransactionHash != entry.TransactionHash || recorded.BlockNumber != entry.BlockNumber || recorded.BlockHash != entry.BlockHash {
		return errors.New("successor phase receipt differs from finalized journal evidence")
	}
	if err := verifyEVMCheckpointFromReader(ctx, reader, head, ChainHead{Number: entry.BlockNumber, Hash: entry.BlockHash}); err != nil {
		return err
	}
	receipt, err := reader.TransactionReceipt(ctx, common.HexToHash(entry.TransactionHash))
	if err != nil || !receiptMatchesEvidence(head, receipt, entry.TransactionHash, entry.BlockNumber, entry.BlockHash) {
		return stateMismatchError(err, "successor call has no exact canonical successful receipt")
	}
	transaction, pending, err := reader.TransactionByHash(ctx, receipt.TxHash)
	chainId := new(big.Int).SetUint64(plan.ChainID)
	if err != nil || transaction == nil || pending || transaction.Hash() != receipt.TxHash || !transaction.Protected() || transaction.ChainId().Cmp(chainId) != 0 {
		return stateMismatchError(err, "successor call changed its signed chain or inclusion")
	}
	signer, err := types.Sender(types.LatestSignerForChainID(chainId), transaction)
	if err != nil || signer != common.HexToAddress(plan.Roles.Deployer) || transaction.Nonce() != nonce || transaction.To() == nil || *transaction.To() != common.HexToAddress(plan.PrecompileProbeSuccessor.Probe) || transaction.Value().Cmp(value) != 0 || !bytes.Equal(transaction.Data(), data) {
		return stateMismatchError(err, "successor call differs from its exact approved signer, nonce, target, value or calldata")
	}
	return verifyPrecompileProbeSuccessorEvent(evidence, entry.ActionID, receipt, recorded.TransactionHash == "")
}

// Event fields authenticate earlier dynamic amounts before they derive a later call.
func verifyPrecompileProbeSuccessorEvent(evidence *PrecompileConformanceEvidence, actionId string, receipt *types.Receipt, partial bool) error {
	parsed, err := abi.JSON(strings.NewReader(SubnetProbeABI))
	if err != nil {
		return err
	}
	sample, err := decodeHex32("successor event sample", evidence.SampleHotkey)
	if err != nil {
		return err
	}
	move, err := decodeHex32("successor event move", evidence.MoveHotkey)
	if err != nil {
		return err
	}
	recovery, err := decodeHex32("successor event recovery", evidence.RecoveryColdkey)
	if err != nil {
		return err
	}
	filtered := *receipt
	filtered.Logs = nil
	for _, log := range receipt.Logs {
		if log != nil && log.Address == common.HexToAddress(evidence.ProbeAddress) {
			filtered.Logs = append(filtered.Logs, log)
		}
	}
	var fields map[string]uint64
	var values map[string]any
	switch actionId {
	case "precompile.seed":
		values, err = conformanceEventValues(parsed, "Seeded", &filtered, sample)
		fields = map[string]uint64{"amountArg": evidence.Seed.TAORao}
		value, ok := conformanceEventBigInt(values, "valueSent")
		if !ok || value.String() != evidence.Seed.ValueWei {
			return errors.New("successor seed event changed the approved value")
		}
		if !partial {
			fields["stakeAfter"] = evidence.Seed.AfterRao
			if evidence.Seed.DeltaRao == 0 || !exactIncrease(evidence.Seed.BeforeRao, evidence.Seed.AfterRao, evidence.Seed.DeltaRao) {
				return errors.New("successor seed event changed its exact delta")
			}
		}
	case "precompile.move-forward", "precompile.move-back":
		step, from, to := evidence.Forward, sample, move
		if actionId == "precompile.move-back" {
			step, from, to = evidence.Back, move, sample
		}
		values, err = conformanceEventValues(parsed, "MoveRoundTrip", &filtered, from, to)
		if err != nil {
			return err
		}
		observed, observeErr := reconcilePrecompileMoveEvent(step, values)
		if observeErr != nil {
			return observeErr
		}
		fields = map[string]uint64{"amount": step.AmountRao, "fromBefore": step.FromBeforeRao, "toBefore": step.ToBeforeRao}
		if !partial {
			if observed.NativeShareResidueRao != step.NativeShareResidueRao {
				return errors.New("successor move changed its native-share remainder")
			}
			fields["fromAfter"], fields["toAfter"] = step.FromAfterRao, step.ToAfterRao
		}
	case "precompile.snapshot":
		values, err = conformanceEventValues(parsed, "DividendSnapshot", &filtered, sample)
		fields = map[string]uint64{"baseline": evidence.Snapshot.BaselineRao}
		if !partial {
			fields["blockNumber"] = evidence.Snapshot.SinceBlock
		}
	case "precompile.transfer-out":
		values, err = conformanceEventValues(parsed, "TransferredOut", &filtered, recovery, sample)
		fields = map[string]uint64{"amount": evidence.Transfer.AmountRao, "sourceBefore": evidence.Transfer.ProbeBeforeRao, "destinationBefore": evidence.Transfer.ProviderBeforeRao}
		if !partial {
			fields["sourceAfter"], fields["destinationAfter"] = evidence.Transfer.ProbeAfterRao, evidence.Transfer.ProviderAfterRao
		}
	default:
		return errors.New("successor receipt names an unapproved event")
	}
	if err != nil {
		return err
	}
	for name, want := range fields {
		if got, ok := conformanceEventUint64(values, name); !ok || got != want {
			return fmt.Errorf("successor %s event changed %s", actionId, name)
		}
	}
	return nil
}

// A crash after the last finalization may retain only that phase's saved input.
func verifyPrecompileProbeSuccessorCalls(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, reader contractCreationReader, head ChainHead, nonce uint64) error {
	prefix, err := precompileProbeSuccessorPrefix(plan, entries, nonce)
	if err != nil || len(prefix) <= 1 {
		return err
	}
	evidence, err := loadPrecompileEvidence(stateDir)
	if err != nil {
		return err
	}
	owner := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, journal: &Journal{entries: entries}}
	if err := owner.validatePrecompileEvidence(common.HexToAddress(plan.PrecompileProbeSuccessor.Probe), evidence); err != nil {
		return err
	}
	if _, err := precompileProbeSuccessorEvidence(plan, evidence, evidence); err != nil {
		return err
	}
	old := plan.PrecompileProbeSuccessor.Evidence
	if evidence.Owner != old.Owner || evidence.SampleHotkey != old.SampleHotkey || evidence.SampleUID != old.SampleUID || evidence.AbsentHotkey != old.AbsentHotkey || evidence.MoveHotkey != old.MoveHotkey || evidence.RecoveryColdkey != old.RecoveryColdkey {
		return errors.New("successor call evidence changed the original approved roles")
	}
	for index, entry := range prefix[1:] {
		action, err := exactPlanActionByID(plan, entry.ActionID)
		if err != nil {
			return err
		}
		verified, complete := owner.verifiedActionEntry(action)
		if complete {
			if _, err := owner.readPersistedPostcondition(verified); err != nil {
				return err
			}
		}
		if err := verifyPrecompileProbeSuccessorCall(ctx, reader, head, plan, evidence, entry, plan.PrecompileProbeSuccessor.DeployerNonce+uint64(index)+1, index == len(prefix)-2 && !complete); err != nil {
			return err
		}
	}
	return nil
}
