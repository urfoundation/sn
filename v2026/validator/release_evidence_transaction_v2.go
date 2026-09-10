//go:build linux || darwin

// Confirms the exact persisted relay transaction against canonical RPC, its
// emitted event and the independently expected immutable contract state.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Missing or not-yet-finalized inclusion is not a conflicting commitment and
// never authorizes allocating a replacement account nonce.
var ErrValidatorEvidenceTransactionPending = errors.New("validator evidence transaction is not finalized")

// Deployment, activation, closed window and gas allowances come from the
// release owner, not from the transaction or a receipt supplied by an endpoint.
// SignedTransaction is the exact durable pre-broadcast RLP; signatures are the
// already authorized public consents, never a relayer's private signing keys.
type ValidatorEvidenceTransactionV2Expected struct {
	Journal             common.Address
	RuntimeHash         [32]byte
	Activation          protocol.ValidatorEvidenceActivation
	Window              protocol.ValidatorEvidenceWindow
	Evidence            ValidatorEvidenceSignedV2
	Relayer             common.Address
	SignedTransaction   []byte
	MaxTransactionBytes uint64
	MaxReceiptLogs      uint64
	MaxGas              uint64
	MaxFeePerGas        uint64
}

// All returned bytes and the receipt belong to the caller. This proves the
// particular transaction/commitment, not native eligibility or proof truth;
// the publisher and independent replay still own those other obligations.
type ValidatorEvidenceTransactionV2Finalized struct {
	SignedTransaction []byte
	Receipt           *types.Receipt
	FinalizedBlock    uint64
	FinalizedHash     [32]byte
	Publication       ValidatorEvidencePublication
}

// Validates and owns exact persisted bytes before a funded caller can resume
// their broadcast. Inclusion and contract state still require confirmation.
func ValidateValidatorEvidenceTransactionV2(ctx context.Context, expected ValidatorEvidenceTransactionV2Expected) (*types.Transaction, error) {
	_, transaction, err := ownValidatorEvidenceTransactionV2(ctx, expected)
	return transaction, err
}

// Own and authenticate the complete transaction before any RPC callback. No
// constructor defaults, permissive ABI prefixes or another signer are allowed.
func ownValidatorEvidenceTransactionV2(ctx context.Context, expected ValidatorEvidenceTransactionV2Expected) (ValidatorEvidenceTransactionV2Expected, *types.Transaction, error) {
	if ctx == nil {
		return expected, nil, errors.New("validator evidence transaction context is absent")
	}
	if err := ctx.Err(); err != nil {
		return expected, nil, err
	}
	if expected.Journal == (common.Address{}) || expected.RuntimeHash == ([32]byte{}) || expected.Relayer == (common.Address{}) ||
		expected.Journal == common.Address(expected.Activation.Domain.Coordinator) || expected.Journal == common.Address(expected.Activation.Domain.SettlementVault) ||
		expected.MaxTransactionBytes == 0 || expected.MaxTransactionBytes > uint64(chainHTTPResponseLimit)/2 ||
		len(expected.SignedTransaction) == 0 || uint64(len(expected.SignedTransaction)) > expected.MaxTransactionBytes ||
		expected.MaxReceiptLogs == 0 || expected.MaxReceiptLogs > uint64(chainHTTPResponseLimit)/128 || expected.MaxGas < 21_000 || expected.MaxFeePerGas == 0 {
		return expected, nil, errors.New("validator evidence transaction ownership or bounds are incomplete")
	}
	domain, err := expected.Activation.EvidenceDomain()
	if err != nil {
		return expected, nil, err
	}
	if expected.Evidence.Schema != ValidatorEvidenceSignedV2Schema || expected.Evidence.Header.Hotkey != expected.Activation.Hotkey ||
		expected.Evidence.Header.NoID != expected.Activation.NoID || expected.Evidence.Header.VPK != expected.Activation.VPK {
		return expected, nil, errors.New("validator evidence transaction differs from its activation signer census")
	}
	if len(expected.Evidence.VPKSignature) != 64 || len(expected.Evidence.HotkeySignature) != 64 {
		return expected, nil, errors.New("validator evidence transaction consent lengths differ")
	}
	expected.SignedTransaction = bytes.Clone(expected.SignedTransaction)
	expected.Evidence.VPKSignature = bytes.Clone(expected.Evidence.VPKSignature)
	expected.Evidence.HotkeySignature = bytes.Clone(expected.Evidence.HotkeySignature)
	calldata, err := stabi.PackValidatorEvidenceCommitment(domain, expected.Window, expected.Evidence.Header, expected.Evidence.VPKSignature, expected.Evidence.HotkeySignature)
	if err != nil {
		return expected, nil, err
	}
	transaction := new(types.Transaction)
	if err := transaction.UnmarshalBinary(expected.SignedTransaction); err != nil {
		return expected, nil, fmt.Errorf("decode persisted validator evidence transaction: %w", err)
	}
	canonical, err := transaction.MarshalBinary()
	if err != nil || !bytes.Equal(canonical, expected.SignedTransaction) {
		return expected, nil, errors.Join(errors.New("validator evidence transaction RLP is noncanonical"), err)
	}
	chainID := new(big.Int).SetUint64(domain.ChainID)
	if (transaction.Type() != types.LegacyTxType && transaction.Type() != types.DynamicFeeTxType) || !transaction.Protected() || transaction.ChainId().Cmp(chainID) != 0 ||
		transaction.To() == nil || *transaction.To() != expected.Journal || transaction.Value().Sign() != 0 || !bytes.Equal(transaction.Data(), calldata) ||
		transaction.Gas() < 21_000 || transaction.Gas() > expected.MaxGas || transaction.GasFeeCap().Sign() <= 0 || !transaction.GasFeeCap().IsUint64() ||
		transaction.GasFeeCap().Uint64() > expected.MaxFeePerGas || transaction.GasTipCap().Sign() < 0 || transaction.GasTipCap().Cmp(transaction.GasFeeCap()) > 0 {
		return expected, nil, errors.New("validator evidence transaction escaped its exact zero-value envelope")
	}
	sender, err := types.Sender(types.LatestSignerForChainID(chainID), transaction)
	if err != nil || sender != expected.Relayer {
		return expected, nil, errors.Join(errors.New("validator evidence transaction signer differs from the funded relay"), err)
	}
	return expected, transaction, ctx.Err()
}

// Each success owns a real receipt lookup, exact signed transaction at its
// canonical block/index, complete fixed-ABI event and hash-pinned readback.
// Synthetic Subtensor EVM hashes are read from RPC, never Header.Hash().
func (self *ChainClient) ConfirmValidatorEvidenceTransactionV2Context(ctx context.Context, supplied ValidatorEvidenceTransactionV2Expected) (result *ValidatorEvidenceTransactionV2Finalized, resultErr error) {
	expected, transaction, err := ownValidatorEvidenceTransactionV2(ctx, supplied)
	if err != nil {
		return nil, err
	}
	return self.confirmValidatorEvidenceTransactionV2(ctx, expected, transaction, false)
}

// The shared inclusion/readback checks remain identical for an owned direct
// send and a permissionless winner. Only the winner may arrive through a
// forwarder or contract constructor and share its receipt with other slots.
func (self *ChainClient) confirmValidatorEvidenceTransactionV2(ctx context.Context, expected ValidatorEvidenceTransactionV2Expected, transaction *types.Transaction, permissionless bool) (result *ValidatorEvidenceTransactionV2Finalized, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	chain, err := self.ownReleaseDecisionChainV2Client(expected.Evidence.Header.Domain)
	if err != nil {
		return nil, err
	}
	finalizedBlock, finalizedHash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	if finalizedBlock < expected.Window.FinalizedBlock {
		return nil, ErrValidatorEvidenceTransactionPending
	}
	receipt, err := chain.client.TransactionReceipt(ctx, transaction.Hash())
	if errors.Is(err, ethereum.NotFound) {
		return nil, ErrValidatorEvidenceTransactionPending
	}
	if err != nil {
		return nil, fmt.Errorf("read exact validator evidence receipt: %w", err)
	}
	if receipt == nil || receipt.TxHash != transaction.Hash() || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Sign() <= 0 ||
		receipt.BlockHash == (common.Hash{}) || receipt.Type != transaction.Type() ||
		receipt.GasUsed > transaction.Gas() || receipt.CumulativeGasUsed < receipt.GasUsed || receipt.EffectiveGasPrice == nil || receipt.EffectiveGasPrice.Sign() < 0 ||
		receipt.EffectiveGasPrice.Cmp(transaction.GasFeeCap()) > 0 || uint64(len(receipt.Logs)) > expected.MaxReceiptLogs {
		return nil, errors.New("validator evidence receipt differs from its exact transaction or bounds")
	}
	if transaction.To() == nil {
		if !permissionless || receipt.ContractAddress != crypto.CreateAddress(expected.Relayer, transaction.Nonce()) {
			return nil, errors.New("validator evidence constructor receipt differs from its exact sender and nonce")
		}
	} else if receipt.ContractAddress != (common.Address{}) {
		return nil, errors.New("validator evidence call receipt claims contract creation")
	}
	includedBlock := receipt.BlockNumber.Uint64()
	if includedBlock > finalizedBlock {
		return nil, ErrValidatorEvidenceTransactionPending
	}
	includedHash, err := chain.BlockHashContext(ctx, includedBlock)
	if err != nil {
		return nil, err
	}
	if includedHash != [32]byte(receipt.BlockHash) {
		return nil, errors.New("validator evidence receipt inclusion is no longer canonical")
	}
	included, err := chain.client.TransactionInBlock(ctx, receipt.BlockHash, receipt.TransactionIndex)
	if err != nil {
		return nil, fmt.Errorf("read exact validator evidence transaction inclusion: %w", err)
	}
	if included == nil {
		return nil, errors.New("validator evidence transaction is absent from its receipt position")
	}
	raw, err := included.MarshalBinary()
	if err != nil || included.Hash() != transaction.Hash() || !bytes.Equal(raw, expected.SignedTransaction) {
		return nil, errors.Join(errors.New("validator evidence receipt position contains another signed transaction"), err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, errors.New("validator evidence transaction reverted in its canonical finalized inclusion")
	}
	if err := validateValidatorEvidenceTransactionV2Events(expected, receipt, permissionless); err != nil {
		return nil, err
	}
	if err := chain.validateValidatorEvidenceEpochGeometryV2Context(ctx, expected, finalizedBlock, finalizedHash); err != nil {
		return nil, err
	}
	window := expected.Window
	window.FinalizedBlock = finalizedBlock
	publication, err := chain.ValidatorEvidenceAtHashContext(ctx, expected.Journal, expected.RuntimeHash, expected.Activation, expected.Evidence.Header, window, finalizedBlock, finalizedHash)
	if err != nil {
		return nil, err
	}
	if publication.PublishedBlock != includedBlock {
		return nil, errors.New("validator evidence receipt and immutable publication heights differ")
	}
	for _, boundary := range []struct {
		number uint64
		hash   [32]byte
	}{
		{number: includedBlock, hash: includedHash},
		{number: finalizedBlock, hash: finalizedHash},
	} {
		actual, err := chain.BlockHashContext(ctx, boundary.number)
		if err != nil || actual != boundary.hash {
			return nil, errors.Join(errors.New("validator evidence canonical boundary changed through readback"), err)
		}
	}
	return &ValidatorEvidenceTransactionV2Finalized{SignedTransaction: expected.SignedTransaction, Receipt: receipt, FinalizedBlock: finalizedBlock, FinalizedHash: finalizedHash, Publication: publication}, nil
}

// Exact fixed-width words refuse padding and suffixes both before an absent
// slot authorizes a send and after a winner's receipt reaches readback.
func (self *ChainClient) validateValidatorEvidenceEpochGeometryV2Context(ctx context.Context, expected ValidatorEvidenceTransactionV2Expected, block uint64, blockHash [32]byte) error {
	epoch := new(big.Int).SetUint64(expected.Window.Epoch)
	outputs, err := self.batchCallsAtHashContext(ctx, block, blockHash, []chainBatchCall{
		{address: self.contractAddr, calldata: self.coordinator.PackEpochStartBlock(epoch)},
		{address: self.contractAddr, calldata: self.coordinator.PackEpochEndBlock(epoch)},
	})
	if err != nil {
		return err
	}
	start := common.BigToHash(new(big.Int).SetUint64(expected.Window.StartBlock))
	end := common.BigToHash(new(big.Int).SetUint64(expected.Window.EndBlock))
	if len(outputs) != 2 || !bytes.Equal(outputs[0], start[:]) || !bytes.Equal(outputs[1], end[:]) {
		return errors.New("validator evidence transaction window differs from canonical epoch geometry")
	}
	return ctx.Err()
}

// The fixed event is checked byte-for-byte before the generated decoder can
// ignore padding or suffixes. Duplicate or transplanted logs cannot attest it.
func validateValidatorEvidenceTransactionV2Event(expected ValidatorEvidenceTransactionV2Expected, receipt *types.Receipt) error {
	return validateValidatorEvidenceTransactionV2Events(expected, receipt, false)
}

// A permissionless forwarder may publish other immutable slots in one batch;
// exactly one event for this slot must still match every expected byte.
func validateValidatorEvidenceTransactionV2Events(expected ValidatorEvidenceTransactionV2Expected, receipt *types.Receipt, permissionless bool) error {
	header := expected.Evidence.Header
	slot, err := header.SlotKey()
	if err != nil {
		return err
	}
	digest, err := header.Digest()
	if err != nil {
		return err
	}
	contractABI, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		return err
	}
	event := contractABI.Events[stabi.STValidatorEvidenceEvidenceCommittedEventName]
	data, err := event.Inputs.NonIndexed().Pack(digest, header.PayloadHash, header.CensusHash, header.PayloadBytes)
	if err != nil {
		return err
	}
	topics := []common.Hash{event.ID, common.Hash(slot), common.BigToHash(new(big.Int).SetUint64(header.NoID)), common.BigToHash(new(big.Int).SetUint64(header.Epoch))}
	matched := false
	for _, log := range receipt.Logs {
		if log == nil || log.Removed || log.BlockHash != receipt.BlockHash || log.BlockNumber != receipt.BlockNumber.Uint64() || log.TxHash != receipt.TxHash || log.TxIndex != receipt.TransactionIndex {
			return errors.New("validator evidence receipt contains a transplanted or removed log")
		}
		if log.Address != expected.Journal {
			continue
		}
		if permissionless && (len(log.Topics) == 0 || log.Topics[0] != event.ID || len(log.Topics) > 1 && log.Topics[1] != common.Hash(slot)) {
			continue
		}
		if matched || len(log.Topics) != len(topics) || !bytes.Equal(log.Data, data) {
			return errors.New("validator evidence event is missing, duplicate or noncanonical")
		}
		for index := range topics {
			if log.Topics[index] != topics[index] {
				return errors.New("validator evidence event topics differ from the exact commitment")
			}
		}
		matched = true
	}
	if !matched {
		return errors.New("validator evidence receipt has no exact commitment event")
	}
	return nil
}
