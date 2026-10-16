// Archive census authenticates complete native bodies and Frontier receipt
// vectors. Committed direct calls/logs are not a proof of internal execution,
// reverted delegatecalls, native hooks, clean deployment, or Safe authority.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/server/v2026/strecovery"
)

const safeHistoryCensusSchema = "urnetwork-mainnet-safe-archive-census-v1"
const maximumSafeHistoryBlocks = 16
const maximumSafeHistoryEncodedBytes = 64 * 1024 * 1024

// Explicit endpoints prevent a later head from silently extending a capture.
// Adjacent captures may be joined by checking their native and EVM parents.
type safeHistoryBoundary struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

// Exact caller-selected scope carries no signature or authority delegation.
type safeHistoryScope struct {
	Safe    string              `json:"safe"`
	From    safeHistoryBoundary `json:"from"`
	Through safeHistoryBoundary `json:"through"`
}

// Raw native extrinsics include their SCALE length prefixes. The header fields
// retain every SCALE digest byte; EVM fields retain exact consensus RLP bytes.
type safeHistoryWitness struct {
	Native       safeHistoryBoundary `json:"native"`
	NativeHeader rootReceiptHeader   `json:"native_header"`
	Extrinsics   []string            `json:"native_extrinsics"`
	EvmHeader    string              `json:"evm_header_rlp"`
	EvmBlock     string              `json:"evm_block_rlp"`
	EvmReceipts  []string            `json:"evm_receipts_rlp"`
}

// Direct calls include failed top-level calls. Their input is committed, but
// receipt status alone says nothing about inner success or reverted side effects.
type safeHistoryDirectCall struct {
	TransactionIndex uint64 `json:"transaction_index"`
	TransactionHash  string `json:"transaction_hash"`
	Kind             string `json:"kind"`
	ReceiptStatus    uint64 `json:"receipt_status"`
	Nonce            uint64 `json:"nonce"`
	Input            string `json:"input"`
}

// Log positions are within the receipt, not untrusted RPC block-wide indices.
// Unknown event signatures are preserved without inferring Safe semantics.
type safeHistoryLog struct {
	TransactionIndex uint64   `json:"transaction_index"`
	TransactionHash  string   `json:"transaction_hash"`
	ReceiptLogIndex  uint64   `json:"receipt_log_index"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
}

// Counts include unrelated traffic. Empty Safe projections never certify that
// the Safe was untouched: reverted or internal calls can leave no committed log.
type safeHistoryBlock struct {
	Witness          safeHistoryWitness      `json:"witness"`
	PostLog          frontierPostLog         `json:"frontier_post_log"`
	Evm              safeHistoryBoundary     `json:"evm"`
	EvmParent        string                  `json:"evm_parent"`
	NativeCount      int                     `json:"native_extrinsic_count"`
	TransactionCount int                     `json:"evm_transaction_count"`
	DirectCalls      []safeHistoryDirectCall `json:"safe_direct_calls"`
	Logs             []safeHistoryLog        `json:"safe_committed_logs"`
}

// Reject absent/oversized intervals before starting any read. A bounded chunk
// is evidence of only its exact interval, never an implicit deployment origin.
func (self safeHistoryScope) validate() error {
	if !common.IsHexAddress(self.Safe) || self.Safe != strings.ToLower(self.Safe) || common.HexToAddress(self.Safe) == (common.Address{}) ||
		self.From.Number == 0 || self.Through.Number > math.MaxUint32 || self.Through.Number < self.From.Number || self.Through.Number-self.From.Number >= maximumSafeHistoryBlocks ||
		!rootCanonicalHash(self.From.Hash) || !rootCanonicalHash(self.Through.Hash) || self.From.Number == self.Through.Number && self.From.Hash != self.Through.Hash {
		return errors.New("Safe archive census requires an exact lowercase Safe and one through sixteen pinned native blocks")
	}
	return nil
}

// This verifier accepts unrelated native/EVM traffic. It composes the existing
// native ordered-trie checker and the server collector's complete EVM decoder;
// neither operation interprets native calls or certifies runtime execution.
func authenticateSafeHistoryBlock(ctx context.Context, safe string, witness safeHistoryWitness) (safeHistoryBlock, error) {
	if ctx == nil {
		return safeHistoryBlock{}, errors.New("Safe archive census context is absent")
	}
	if err := ctx.Err(); err != nil {
		return safeHistoryBlock{}, err
	}
	scope := safeHistoryScope{Safe: safe, From: witness.Native, Through: witness.Native}
	if err := scope.validate(); err != nil {
		return safeHistoryBlock{}, err
	}
	if number, err := witness.NativeHeader.authenticate(witness.Native.Hash); err != nil || number != witness.Native.Number {
		return safeHistoryBlock{}, fmt.Errorf("%w: Safe archive native header differs: %v", errRpcIntegrity, err)
	}
	body, err := authenticateRootReceiptBody(witness.NativeHeader, witness.Extrinsics)
	if err != nil {
		return safeHistoryBlock{}, err
	}
	postLog, err := finalizedFrontierPostLog(witness.NativeHeader)
	if err != nil {
		return safeHistoryBlock{}, err
	}
	mapped, err := authenticateMappedEvmHeader(witness.EvmHeader, postLog.BlockHash)
	if err != nil {
		return safeHistoryBlock{}, err
	}
	census, err := strecovery.AuthenticateReceiptBlockCensus(ctx, mapped.Hash, mapped.Number, witness.EvmHeader, witness.EvmBlock, witness.EvmReceipts)
	if err != nil {
		return safeHistoryBlock{}, err
	}
	if postLog.Variant == 1 && len(postLog.TransactionHashes) != len(census.Transactions) {
		return safeHistoryBlock{}, errors.New("Safe archive Frontier transaction vector count differs")
	}
	result := safeHistoryBlock{Witness: witness, PostLog: postLog, Evm: safeHistoryBoundary{Number: mapped.Number, Hash: mapped.Hash}, EvmParent: census.Header.ParentHash.Hex(),
		NativeCount: len(body), TransactionCount: len(census.Transactions), DirectCalls: []safeHistoryDirectCall{}, Logs: []safeHistoryLog{}}
	address := common.HexToAddress(safe)
	for index, transaction := range census.Transactions {
		if err := ctx.Err(); err != nil {
			return safeHistoryBlock{}, err
		}
		hash := transaction.Hash().Hex()
		if postLog.Variant == 1 && postLog.TransactionHashes[index] != hash {
			return safeHistoryBlock{}, errors.New("Safe archive Frontier transaction vector order or hash differs")
		}
		kind := ""
		if transaction.To() != nil && *transaction.To() == address {
			kind = "call"
		} else if transaction.To() == nil {
			from, err := types.Sender(types.LatestSignerForChainID(transaction.ChainId()), transaction)
			if err != nil {
				return safeHistoryBlock{}, errors.New("Safe archive cannot derive a committed top-level CREATE destination")
			}
			if crypto.CreateAddress(from, transaction.Nonce()) == address {
				kind = "create"
			}
		}
		receipt := census.Receipts[index]
		if kind != "" {
			result.DirectCalls = append(result.DirectCalls, safeHistoryDirectCall{TransactionIndex: uint64(index), TransactionHash: hash,
				Kind: kind, ReceiptStatus: receipt.Status, Nonce: transaction.Nonce(), Input: "0x" + hex.EncodeToString(transaction.Data())})
		}
		for logIndex, log := range receipt.Logs {
			if log.Address != address {
				continue
			}
			topics := make([]string, len(log.Topics))
			for topicIndex, topic := range log.Topics {
				topics[topicIndex] = topic.Hex()
			}
			result.Logs = append(result.Logs, safeHistoryLog{TransactionIndex: uint64(index), TransactionHash: hash, ReceiptLogIndex: uint64(logIndex), Topics: topics, Data: "0x" + hex.EncodeToString(log.Data)})
		}
	}
	if err := ctx.Err(); err != nil {
		return safeHistoryBlock{}, err
	}
	return result, nil
}
