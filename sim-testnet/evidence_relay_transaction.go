//go:build linux || darwin

// Evidence uses the existing funded role, exact action envelope, persisted
// transaction bytes and hash-chained journal. It does not own another wallet
// or nonce store. The campaign owner separately admits each dynamic action
// against its finite, approved aggregate relay allowance.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/stabi"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// A successful third-party race retains our actual reverted receipt and cost
// for the final report; it never disguises that transaction as a successful
// publication or allocates a second transaction for the immutable slot.
type evidenceRelayTransactionResult struct {
	Winner              *validatorcomponent.ValidatorEvidenceTransactionV2Finalized
	OwnReceipt          *types.Receipt
	LostPublicationRace bool
}

// Inclusion/finality rows deliberately omit signer and nonce. Only the first
// exact broadcast owns that recovery identity, even after later stage rows.
func evidenceRelayOriginalBroadcast(entries []JournalEntry, deploymentId, planHash string, action Action) (original JournalEntry, broadcast, hasIntent bool, resultErr error) {
	for _, entry := range entries {
		if entry.PlanHash != planHash || entry.ActionID != action.ID {
			continue
		}
		if entry.DeploymentID != deploymentId || entry.IntentHash != action.IntentHash {
			return original, false, false, errors.New("evidence relay slot has another durable deployment or action")
		}
		if entry.Stage == StageIntent {
			hasIntent = true
		}
		if entry.Stage == StageBroadcast {
			nonce, err := strconv.ParseUint(entry.Nonce, 10, 64)
			if !hasIntent || err != nil || strconv.FormatUint(nonce, 10) != entry.Nonce || !common.IsHexAddress(entry.Signer) || !validCanonicalHashHex(entry.TransactionHash) || entry.RecoveryBlock == 0 || !validCanonicalHashHex(entry.RecoveryBlockHash) {
				return original, false, false, errors.New("evidence relay original broadcast identity is incomplete or noncanonical")
			}
			if !broadcast {
				original, broadcast = entry, true
			} else if entry.TransactionHash != original.TransactionHash || entry.Signer != original.Signer || entry.Nonce != original.Nonce || entry.RecoveryBlock != original.RecoveryBlock || entry.RecoveryBlockHash != original.RecoveryBlockHash {
				return original, false, false, errors.New("evidence relay replay changed the original broadcast identity")
			}
		}
		if entry.TransactionHash != "" && (!broadcast || entry.TransactionHash != original.TransactionHash) {
			return original, false, false, errors.New("evidence relay transaction stage has no matching original broadcast")
		}
	}
	return original, broadcast, hasIntent, nil
}

// The caller's action is already admitted and retained by the campaign owner.
// Header and slot parameters bind semantic authority, while the normal exact
// transaction envelope and this validator bind any recovered raw calldata.
func (self *EvmTxManager) relayValidatorEvidenceTransaction(ctx context.Context, chain *validatorcomponent.ChainClient, planHash string, action Action, supplied validatorcomponent.ValidatorEvidenceTransactionV2Expected) (result *evidenceRelayTransactionResult, resultErr error) {
	if ctx == nil {
		return nil, errors.New("evidence relay transaction context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if self == nil || self.client == nil || self.key == nil || self.chainID == nil || self.journal == nil || chain == nil || !validCanonicalHashHex(planHash) {
		return nil, errors.New("evidence relay has no funded manager, chain or approved plan")
	}
	expected := supplied
	expected.SignedTransaction = nil
	expected.Evidence.VPKSignature = bytes.Clone(supplied.Evidence.VPKSignature)
	expected.Evidence.HotkeySignature = bytes.Clone(supplied.Evidence.HotkeySignature)
	action.Parameters, action.DependsOn = maps.Clone(action.Parameters), slices.Clone(action.DependsOn)
	action.AcceptedPriorIntentHashes = slices.Clone(action.AcceptedPriorIntentHashes)
	domain, err := expected.Activation.EvidenceDomain()
	if err != nil {
		return nil, err
	}
	calldata, err := stabi.PackValidatorEvidenceCommitment(domain, expected.Window, expected.Evidence.Header, expected.Evidence.VPKSignature, expected.Evidence.HotkeySignature)
	if err != nil {
		return nil, err
	}
	slot, err := expected.Evidence.Header.SlotKey()
	if err != nil {
		return nil, err
	}
	digest, err := expected.Evidence.Header.Digest()
	if err != nil {
		return nil, err
	}
	if action.ID != fmt.Sprintf("evidence.relay.%x", slot) || action.Kind != "evm-transaction" || action.Target != expected.Journal.Hex() ||
		action.Parameters["validator_evidence_slot"] != fmt.Sprintf("0x%x", slot) || action.Parameters["validator_evidence_header_hash"] != fmt.Sprintf("0x%x", digest) ||
		action.Spend.TAORao != 0 || action.Spend.AlphaRao != 0 || action.Spend.Registrations != 0 || action.Spend.SubnetCreations != 0 || self.chainID.Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 {
		return nil, errors.New("evidence relay action escaped its exact zero-value slot and header")
	}
	intentHash, err := actionIntentHash(action)
	if err != nil || intentHash != action.IntentHash {
		return nil, errors.Join(errors.New("evidence relay action hash differs"), err)
	}
	expected.MaxGas, expected.MaxFeePerGas, err = evmActionFeeEnvelope(action)
	if err != nil {
		return nil, err
	}
	expected.Relayer = crypto.PubkeyToAddress(self.key.PublicKey)
	release, err := self.acquireNonceTurn(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	// Recovery is authenticated before the existing sender can rebroadcast.
	// No amount of on-chain winner evidence makes foreign stored bytes safe.
	readBroadcast := func() (JournalEntry, *types.Transaction, bool, error) {
		prior, broadcast, hasIntent, err := evidenceRelayOriginalBroadcast(self.journal.Entries(), self.deploymentID, planHash, action)
		if err != nil || !broadcast {
			return prior, nil, hasIntent, err
		}
		expected.SignedTransaction, err = readValidatorEvidenceHistoricalFile(self.stateDir, "transactions/"+stringsTrim0x(prior.TransactionHash)+".rlp", 64*1024)
		if err != nil {
			return prior, nil, hasIntent, err
		}
		transaction, err := validatorcomponent.ValidateValidatorEvidenceTransactionV2(ctx, expected)
		if err != nil || transaction == nil || transaction.Hash().Hex() != prior.TransactionHash || !strings.EqualFold(prior.Signer, expected.Relayer.Hex()) || strconv.FormatUint(transaction.Nonce(), 10) != prior.Nonce {
			return prior, nil, hasIntent, errors.Join(errors.New("evidence relay recovery differs from its original exact broadcast"), err)
		}
		if err := validateApprovedEVMTransactionFields(action, expected.Relayer, transaction.Nonce(), transaction.To(), transaction.Value(), transaction.Data()); err != nil {
			return prior, nil, hasIntent, err
		}
		return prior, transaction, hasIntent, nil
	}
	prior, transaction, hasIntent, err := readBroadcast()
	if err != nil {
		return nil, err
	}
	broadcast := transaction != nil
	winner, winnerErr := chain.FindValidatorEvidenceSlotWinnerV2Context(ctx, expected)
	if winnerErr != nil && !errors.Is(winnerErr, validatorcomponent.ErrValidatorEvidenceAbsent) {
		return nil, winnerErr
	}
	if winner != nil && !broadcast {
		return &evidenceRelayTransactionResult{Winner: winner}, nil
	}
	if winner != nil && winner.Receipt.TxHash.Hex() == prior.TransactionHash {
		confirmed, err := chain.ConfirmValidatorEvidenceTransactionV2Context(ctx, expected)
		if err != nil {
			return nil, err
		}
		return &evidenceRelayTransactionResult{Winner: confirmed, OwnReceipt: confirmed.Receipt}, nil
	}
	if !broadcast {
		if !hasIntent {
			if err := self.journal.Append(JournalEntry{DeploymentID: self.deploymentID, PlanHash: planHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
				return nil, err
			}
		}
	}
	receipt, sendErr := self.sendOwnedNonce(ctx, planHash, action, &expected.Journal, new(big.Int), calldata)
	if sendErr != nil {
		if receipt == nil || receipt.Status != types.ReceiptStatusFailed || ctx.Err() != nil {
			return nil, sendErr
		}
		prior, transaction, _, err = readBroadcast()
		if err != nil || transaction == nil || receipt.TxHash != transaction.Hash() {
			return nil, errors.Join(sendErr, errors.New("evidence relay failed receipt differs from its original broadcast"), err)
		}
		// An initial failed receipt can precede a finality transport error.
		// Re-read canonical inclusion and exact body before claiming a race.
		actual, finalized, err := observeEVMReceiptFinality(ctx, ethEVMReceiptFinalityReader{client: self.client}, transaction.Hash())
		if err != nil || !finalized || actual == nil || actual.Status != types.ReceiptStatusFailed || actual.Type != transaction.Type() || actual.ContractAddress != (common.Address{}) || actual.GasUsed > transaction.Gas() || actual.CumulativeGasUsed < actual.GasUsed || actual.EffectiveGasPrice == nil || actual.EffectiveGasPrice.Sign() < 0 || actual.EffectiveGasPrice.Cmp(transaction.GasFeeCap()) > 0 || len(actual.Logs) != 0 {
			return nil, errors.Join(sendErr, errors.New("evidence relay failed receipt lacks exact canonical transaction/cost identity"), err)
		}
		included, err := self.client.TransactionInBlock(ctx, actual.BlockHash, actual.TransactionIndex)
		if err != nil || included == nil || included.Hash() != transaction.Hash() {
			return nil, errors.Join(sendErr, errors.New("evidence relay failed inclusion contains another transaction"), err)
		}
		raw, err := included.MarshalBinary()
		if err != nil || !bytes.Equal(raw, expected.SignedTransaction) {
			return nil, errors.Join(sendErr, errors.New("evidence relay failed inclusion changed the saved signed bytes"), err)
		}
		receipt = actual
		// Only the sender's real canonical failed receipt plus an independently
		// confirmed matching winner explains a publication race.
		winner, winnerErr = chain.FindValidatorEvidenceSlotWinnerV2Context(ctx, expected)
		if winnerErr != nil || winner == nil || winner.Receipt.TxHash == receipt.TxHash {
			return nil, errors.Join(sendErr, winnerErr)
		}
		canonical, err := canonicalEVMBlockHash(ctx, ethEVMBlockReader{client: self.client}, receipt.BlockNumber.Uint64())
		if err != nil || canonical != receipt.BlockHash.Hex() {
			return nil, errors.Join(sendErr, errors.New("evidence relay reverted inclusion changed through winner readback"), err)
		}
		if err := self.journal.Append(JournalEntry{DeploymentID: self.deploymentID, PlanHash: planHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFailed, TransactionHash: receipt.TxHash.Hex(), BlockNumber: receipt.BlockNumber.Uint64(), BlockHash: receipt.BlockHash.Hex(), Error: "canonical publication race: exact immutable slot was committed by " + winner.Receipt.TxHash.Hex()}); err != nil {
			return nil, errors.Join(sendErr, err)
		}
		return &evidenceRelayTransactionResult{Winner: winner, OwnReceipt: receipt, LostPublicationRace: true}, nil
	}
	if receipt == nil {
		return nil, errors.New("evidence relay sender returned no canonical receipt")
	}
	prior, transaction, _, err = readBroadcast()
	if err != nil || transaction == nil || prior.TransactionHash != receipt.TxHash.Hex() {
		return nil, errors.Join(errors.New("evidence relay successful receipt differs from its original durable transaction"), err)
	}
	winner, err = chain.ConfirmValidatorEvidenceTransactionV2Context(ctx, expected)
	if err != nil {
		return nil, err
	}
	return &evidenceRelayTransactionResult{Winner: winner, OwnReceipt: receipt}, nil
}
