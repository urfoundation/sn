//go:build linux || darwin

// A permissionless publisher can win the immutable slot before our funded
// relay. Discover the exact on-chain winner, not a transaction guessed from
// our own nonce or one particular encoding of the hotkey consent signature.
package validator

import (
	"bytes"
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/stabi"
)

// The expected domain, header, complete public consents and bounded response
// sizes are independent inputs. Relayer, SignedTransaction and gas allowances
// describe an owned send and are deliberately not authority for a third party
// who pays its own gas. This method never broadcasts or reserves an account.
func (self *ChainClient) FindValidatorEvidenceSlotWinnerV2Context(ctx context.Context, supplied ValidatorEvidenceTransactionV2Expected) (result *ValidatorEvidenceTransactionV2Finalized, resultErr error) {
	if ctx == nil {
		return nil, errors.New("validator evidence winner context is absent")
	}
	ctx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected := supplied
	if expected.Journal == (common.Address{}) || expected.RuntimeHash == ([32]byte{}) || expected.MaxTransactionBytes == 0 || expected.MaxTransactionBytes > uint64(chainHTTPResponseLimit)/2 || expected.MaxReceiptLogs == 0 || expected.MaxReceiptLogs > uint64(chainHTTPResponseLimit)/128 {
		return nil, errors.New("validator evidence winner authority or finite response bounds are incomplete")
	}
	domain, err := expected.Activation.EvidenceDomain()
	if err != nil {
		return nil, err
	}
	if expected.Evidence.Schema != ValidatorEvidenceSignedV2Schema || expected.Evidence.Header.Hotkey != expected.Activation.Hotkey || expected.Evidence.Header.NoID != expected.Activation.NoID || expected.Evidence.Header.VPK != expected.Activation.VPK {
		return nil, errors.New("validator evidence winner differs from its independently expected activation")
	}
	expected.Evidence.VPKSignature = bytes.Clone(supplied.Evidence.VPKSignature)
	expected.Evidence.HotkeySignature = bytes.Clone(supplied.Evidence.HotkeySignature)
	if _, err := stabi.PackValidatorEvidenceCommitment(domain, expected.Window, expected.Evidence.Header, expected.Evidence.VPKSignature, expected.Evidence.HotkeySignature); err != nil {
		return nil, err
	}
	chain, err := self.ownReleaseDecisionChainV2Client(domain)
	if err != nil {
		return nil, err
	}
	block, blockHash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	if block < expected.Window.FinalizedBlock {
		return nil, ErrValidatorEvidenceTransactionPending
	}
	window := expected.Window
	window.FinalizedBlock = block
	if err := chain.validateValidatorEvidenceEpochGeometryV2Context(ctx, expected, block, blockHash); err != nil {
		return nil, err
	}
	publication, err := chain.ValidatorEvidenceAtHashContext(ctx, expected.Journal, expected.RuntimeHash, expected.Activation, expected.Evidence.Header, window, block, blockHash)
	if err != nil {
		return nil, err
	}
	includedHash, err := chain.BlockHashContext(ctx, publication.PublishedBlock)
	if err != nil {
		return nil, err
	}
	slot, err := expected.Evidence.Header.SlotKey()
	if err != nil {
		return nil, err
	}
	contractAbi, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	event := contractAbi.Events[stabi.STValidatorEvidenceEvidenceCommittedEventName]
	canonicalHash := common.Hash(includedHash)
	topics := [][]common.Hash{{event.ID}, {common.Hash(slot)}, {common.BigToHash(new(big.Int).SetUint64(expected.Evidence.Header.NoID))}, {common.BigToHash(new(big.Int).SetUint64(expected.Window.Epoch))}}
	logs, err := chain.client.FilterLogs(ctx, ethereum.FilterQuery{BlockHash: &canonicalHash, Addresses: []common.Address{expected.Journal}, Topics: topics})
	if err != nil {
		return nil, err
	}
	if len(logs) != 1 || logs[0].Removed || logs[0].Address != expected.Journal || logs[0].BlockHash != canonicalHash || logs[0].BlockNumber != publication.PublishedBlock || logs[0].TxHash == (common.Hash{}) {
		return nil, errors.New("validator evidence immutable slot has no unique canonical event")
	}
	digest, err := expected.Evidence.Header.Digest()
	if err != nil {
		return nil, err
	}
	header := expected.Evidence.Header
	eventData, err := event.Inputs.NonIndexed().Pack(digest, header.PayloadHash, header.CensusHash, header.PayloadBytes)
	if err != nil {
		return nil, err
	}
	if len(logs[0].Topics) != len(topics) || !bytes.Equal(logs[0].Data, eventData) {
		return nil, errors.New("validator evidence discovered event differs from the exact slot header")
	}
	for index, topic := range topics {
		if logs[0].Topics[index] != topic[0] {
			return nil, errors.New("validator evidence discovered event topics differ")
		}
	}
	transaction, pending, err := chain.client.TransactionByHash(ctx, logs[0].TxHash)
	if err != nil {
		return nil, err
	}
	if pending || transaction == nil || transaction.Hash() != logs[0].TxHash || !transaction.Protected() || transaction.ChainId().Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 {
		return nil, errors.New("validator evidence winner has no exact chain-bound signed transaction")
	}
	raw, err := transaction.MarshalBinary()
	if err != nil || len(raw) == 0 || uint64(len(raw)) > expected.MaxTransactionBytes {
		return nil, errors.Join(errors.New("validator evidence winner transaction exceeds its byte bound"), err)
	}
	signer, err := types.Sender(types.LatestSignerForChainID(transaction.ChainId()), transaction)
	if err != nil || signer == (common.Address{}) {
		return nil, errors.Join(errors.New("validator evidence winner transaction has no valid signer"), err)
	}
	expected.Relayer, expected.SignedTransaction = signer, raw
	confirmed, err := chain.confirmValidatorEvidenceTransactionV2(ctx, expected, transaction, true)
	if err != nil {
		return nil, err
	}
	if confirmed.Publication != publication || confirmed.Receipt.TxHash != logs[0].TxHash || confirmed.Receipt.TransactionIndex != logs[0].TxIndex || confirmed.Receipt.BlockHash != canonicalHash {
		return nil, errors.New("validator evidence discovery and confirmed winner differ")
	}
	return confirmed, nil
}
