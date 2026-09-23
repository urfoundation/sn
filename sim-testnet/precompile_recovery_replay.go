// Extra probe nonces are admitted only through the bounded signed recovery
// authority and exact calldata, receipts and recorded operation order.
package main

import (
	"bytes"
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// This sequence inserts independently authorized repairs before the original
// sample transfer without changing any action in the signed setup plan.
func precompileProbeSuccessorActions(plan *SetupPlan, evidence *PrecompileConformanceEvidence) ([]Action, error) {
	var actions []Action
	for _, id := range precompileProbeSuccessorTransactionIds() {
		if id == "precompile.transfer-out" && evidence != nil && evidence.Recovery != nil {
			if err := validatePrecompileRecoveryPlan(plan, evidence, &evidence.Recovery.Authorization); err != nil {
				return nil, err
			}
			for index, step := range evidence.Recovery.Steps {
				if precompileRecoveryAfterTransfer(step) {
					continue
				}
				action, _, err := precompileRecoveryAction(&evidence.Recovery.Authorization, index, step)
				if err != nil || !precompileRecoveryActionEqual(action, step.Action) {
					return nil, errors.Join(errors.New("probe recovery action sequence changed"), err)
				}
				actions = append(actions, action)
			}
		}
		action, err := exactPlanActionByID(plan, id)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	if evidence != nil && evidence.Recovery != nil {
		for index, step := range evidence.Recovery.Steps {
			if !precompileRecoveryAfterTransfer(step) {
				continue
			}
			action, _, err := precompileRecoveryAction(&evidence.Recovery.Authorization, index, step)
			if err != nil || !precompileRecoveryActionEqual(action, step.Action) {
				return nil, errors.Join(errors.New("probe recovery post-transfer action changed"), err)
			}
			actions = append(actions, action)
		}
	}
	return actions, nil
}

// The verifier accepts a missing post-state only for the last already finalized
// operation; the event still authenticates the exact signed amount and custody.
func verifyPrecompileRecoveryCall(ctx context.Context, reader contractCreationReader, head ChainHead, plan *SetupPlan, evidence *PrecompileConformanceEvidence, entry JournalEntry, nonce uint64, allowPartial bool) error {
	if evidence == nil || evidence.Recovery == nil {
		return errors.New("probe recovery call has no evidence")
	}
	if err := validatePrecompileRecoveryPlan(plan, evidence, &evidence.Recovery.Authorization); err != nil {
		return err
	}
	index := -1
	for i, step := range evidence.Recovery.Steps {
		if step.Action.ID == entry.ActionID {
			index = i
			break
		}
	}
	if index < 0 {
		return errors.New("probe recovery call is outside its authorized sequence")
	}
	step := evidence.Recovery.Steps[index]
	action, data, err := precompileRecoveryAction(&evidence.Recovery.Authorization, index, step)
	if err != nil || !precompileRecoveryActionEqual(action, step.Action) || action.IntentHash != entry.IntentHash {
		return errors.Join(errors.New("probe recovery call changed its action intent"), err)
	}
	recordedTx, recordedBlock, recordedHash := precompileRecoveryReceipt(step)
	partial := recordedTx == "" && recordedBlock == 0 && recordedHash == ""
	if partial {
		if !allowPartial || index != len(evidence.Recovery.Steps)-1 {
			return errors.New("probe recovery lost a completed receipt")
		}
	} else if recordedTx != entry.TransactionHash || recordedBlock != entry.BlockNumber || recordedHash != entry.BlockHash {
		return errors.New("probe recovery changed its retained receipt identity")
	}
	if err := verifyEVMCheckpointFromReader(ctx, reader, head, ChainHead{Number: entry.BlockNumber, Hash: entry.BlockHash}); err != nil {
		return err
	}
	receipt, err := reader.TransactionReceipt(ctx, common.HexToHash(entry.TransactionHash))
	if err != nil || !receiptMatchesEvidence(head, receipt, entry.TransactionHash, entry.BlockNumber, entry.BlockHash) {
		return stateMismatchError(err, "probe recovery lacks a canonical successful receipt")
	}
	tx, pending, err := reader.TransactionByHash(ctx, receipt.TxHash)
	chain := new(big.Int).SetUint64(plan.ChainID)
	if err != nil || tx == nil || pending || tx.Hash() != receipt.TxHash || !tx.Protected() || tx.ChainId().Cmp(chain) != 0 || (nonce != 0 && tx.Nonce() != nonce) {
		return stateMismatchError(err, "probe recovery changed its signed chain or nonce")
	}
	if err := validatePrecompileRecoverySignedBounds(action, tx); err != nil {
		return err
	}
	signer, err := types.Sender(types.LatestSignerForChainID(chain), tx)
	if err != nil || !bytes.Equal(tx.Data(), data) {
		return stateMismatchError(err, "probe recovery changed its calldata or signature")
	}
	if err := validatePrecompileRecoveryTransactionFields(action, signer, tx.To(), tx.Value(), tx.Data()); err != nil {
		return err
	}
	return verifyPrecompileRecoveryEvent(evidence, step, receipt, partial)
}
