// Retained startup reconciles already-signed EVM outcomes before admitting
// transaction-capable children. It never signs, sends or verifies action effects.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Read-only by construction: no signing or transaction-submission operation.
type retainedProvisionalOutcomeReader interface {
	evmReceiptFinalityReader
	TransactionInBlock(context.Context, common.Hash, uint) (*types.Transaction, error)
}

type retainedProvisionalEVMOutcomeReader struct{ ethEVMReceiptFinalityReader }

func (self retainedProvisionalEVMOutcomeReader) TransactionInBlock(ctx context.Context, hash common.Hash, index uint) (*types.Transaction, error) {
	return self.client.TransactionInBlock(ctx, hash, index)
}

// Resolve only pending permitted-lineage EVM broadcasts. Native transactions
// keep their existing exact reconciliation requirement and cannot be guessed.
func (self *Executor) reconcileRetainedProvisionalTransactionOutcomes(ctx context.Context) error {
	if self == nil || self.journal == nil || self.plan == nil {
		return errors.New("retained startup reconciliation is incomplete")
	}
	var reader retainedProvisionalOutcomeReader
	if self.deployer != nil && self.deployer.client != nil {
		reader = retainedProvisionalEVMOutcomeReader{ethEVMReceiptFinalityReader: ethEVMReceiptFinalityReader{client: self.deployer.client}}
	}
	return reconcileRetainedProvisionalEVMOutcomes(ctx, self, reader, defaultFinalSemanticRPCRetryPolicy())
}

// Journal finality resolves a consumed nonce, not a postcondition. A finalized
// revert is retained as a known failed outcome; no StageVerified is fabricated.
func reconcileRetainedProvisionalEVMOutcomes(ctx context.Context, self *Executor, reader retainedProvisionalOutcomeReader, policy finalSemanticRPCRetryPolicy) error {
	if ctx == nil || self == nil || self.plan == nil || self.journal == nil {
		return errors.New("retained startup reconciliation is incomplete")
	}
	entries := self.journal.Entries()
	pending, err := unresolvedRetainedProvisionalTransactions(self.plan, entries)
	if err != nil {
		return err
	}
	for _, broadcast := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasPrefix(retainedProvisionalSignerKey(broadcast.Signer), "evm:") {
			continue
		}
		if reader == nil || self.plan.ChainID == 0 || broadcast.DeploymentID != self.plan.DeploymentID || broadcast.RecoveryBlock == 0 || !validCanonicalHashHex(broadcast.RecoveryBlockHash) {
			return errors.New("retained EVM outcome lacks its original deployment, reader or recovery checkpoint")
		}
		raw, err := readValidatorEvidenceHistoricalFile(self.stateDir, filepath.ToSlash(filepath.Join("transactions", strings.TrimPrefix(broadcast.TransactionHash, "0x")+".rlp")), maximumCampaignEvidenceRawFileBytes)
		if err != nil {
			return fmt.Errorf("retained EVM signed transaction %s: %w", broadcast.TransactionHash, err)
		}
		var signed types.Transaction
		if err := signed.UnmarshalBinary(raw); err != nil {
			return fmt.Errorf("retained EVM signed transaction: %w", err)
		}
		chainId := new(big.Int).SetUint64(self.plan.ChainID)
		signer, err := types.Sender(types.LatestSignerForChainID(chainId), &signed)
		if err != nil || !signed.Protected() || signed.ChainId().Cmp(chainId) != 0 || signed.Hash().Hex() != broadcast.TransactionHash || !strings.EqualFold(signer.Hex(), broadcast.Signer) || strconv.FormatUint(signed.Nonce(), 10) != broadcast.Nonce {
			return errors.Join(errors.New("retained EVM signed bytes differ from their original chain, hash, signer or nonce"), err)
		}
		var included *JournalEntry
		for index := range entries {
			entry := &entries[index]
			if entry.PlanHash != broadcast.PlanHash || entry.ActionID != broadcast.ActionID || entry.IntentHash != broadcast.IntentHash || entry.TransactionHash != broadcast.TransactionHash || entry.Stage != StageIncluded {
				continue
			}
			if entry.DeploymentID != broadcast.DeploymentID || entry.BlockNumber == 0 || !validCanonicalHashHex(entry.BlockHash) || entry.Signer != "" && !strings.EqualFold(entry.Signer, broadcast.Signer) || entry.Nonce != "" && entry.Nonce != broadcast.Nonce || entry.RecoveryBlock != 0 && entry.RecoveryBlock != broadcast.RecoveryBlock || entry.RecoveryBlockHash != "" && entry.RecoveryBlockHash != broadcast.RecoveryBlockHash || included != nil && (included.BlockNumber != entry.BlockNumber || included.BlockHash != entry.BlockHash) {
				return errors.New("retained EVM included outcome changed its original transaction coordinates")
			}
			included = entry
		}
		var receipt *types.Receipt
		if err := retryEvmReadRpcCall(ctx, "retained startup EVM outcome", policy, func(readCtx context.Context) error {
			finalized, err := finalizedEVMHeadFromReader(readCtx, reader)
			if err != nil {
				return err
			}
			actual, err := reader.TransactionReceipt(readCtx, signed.Hash())
			if err != nil {
				return err
			}
			if err := validateEVMReceiptIdentity(actual, signed.Hash()); err != nil {
				return err
			}
			if actual.Status != types.ReceiptStatusSuccessful && actual.Status != types.ReceiptStatusFailed || actual.BlockNumber.Uint64() < broadcast.RecoveryBlock {
				return errors.New("retained EVM receipt has an invalid outcome or predates its broadcast checkpoint")
			}
			if included != nil && (actual.BlockNumber.Uint64() != included.BlockNumber || actual.BlockHash.Hex() != included.BlockHash) {
				return errors.New("retained EVM receipt differs from its recorded inclusion")
			}
			checkpoint := ChainHead{Number: actual.BlockNumber.Uint64(), Hash: actual.BlockHash.Hex()}
			if err := verifyEVMCheckpointFromReader(readCtx, reader, finalized, checkpoint); err != nil {
				return fmt.Errorf("retained EVM inclusion is not canonical and finalized: %w", err)
			}
			if err := verifyEVMCheckpointFromReader(readCtx, reader, finalized, ChainHead{Number: broadcast.RecoveryBlock, Hash: broadcast.RecoveryBlockHash}); err != nil {
				return fmt.Errorf("retained EVM original broadcast checkpoint: %w", err)
			}
			transaction, err := reader.TransactionInBlock(readCtx, actual.BlockHash, actual.TransactionIndex)
			if err != nil {
				return err
			}
			if transaction == nil || transaction.Hash() != signed.Hash() {
				return errors.New("retained EVM inclusion contains another signed transaction")
			}
			body, err := transaction.MarshalBinary()
			if err != nil || !bytes.Equal(body, raw) {
				return errors.Join(errors.New("retained EVM inclusion differs from exact persisted signed bytes"), err)
			}
			receipt = actual
			return nil
		}); err != nil {
			return fmt.Errorf("reconcile retained transaction %s/%s: %w", broadcast.ActionID, broadcast.TransactionHash, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		finalized := broadcast
		finalized.Stage, finalized.BlockNumber, finalized.BlockHash = StageFinalized, receipt.BlockNumber.Uint64(), receipt.BlockHash.Hex()
		if receipt.Status == types.ReceiptStatusFailed {
			finalized.Error = "retained EVM transaction reverted in its canonical finalized inclusion; action effects remain unverified"
		}
		if err := self.journal.Append(finalized); err != nil {
			return fmt.Errorf("persist retained EVM outcome: %w", err)
		}
	}
	return nil
}
