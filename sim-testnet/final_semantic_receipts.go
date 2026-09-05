package main

// Canonical EVM log projections make receipt evidence independent of JSON-RPC
// object field order and provider-specific formatting. Only logs emitted by
// the release contracts participate in semantic receipt hashes.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

type finalCanonicalEVMLog struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      uint64   `json:"block_number"`
	BlockHash        string   `json:"block_hash"`
	TransactionHash  string   `json:"transaction_hash"`
	TransactionIndex uint64   `json:"transaction_index"`
	LogIndex         uint64   `json:"log_index"`
}

type finalRPCLog struct {
	Address          common.Address `json:"address"`
	Topics           []common.Hash  `json:"topics"`
	Data             hexutil.Bytes  `json:"data"`
	BlockNumber      hexutil.Uint64 `json:"blockNumber"`
	BlockHash        common.Hash    `json:"blockHash"`
	TransactionHash  common.Hash    `json:"transactionHash"`
	TransactionIndex hexutil.Uint64 `json:"transactionIndex"`
	LogIndex         hexutil.Uint64 `json:"logIndex"`
	Removed          bool           `json:"removed"`
}

func finalCanonicalLogFromGeth(value ethTypes.Log) (finalCanonicalEVMLog, error) {
	if value.Removed {
		return finalCanonicalEVMLog{}, errors.New("canonical receipt contains a removed log")
	}
	if value.Address == (common.Address{}) || value.BlockNumber == 0 || value.BlockHash == (common.Hash{}) || value.TxHash == (common.Hash{}) {
		return finalCanonicalEVMLog{}, errors.New("canonical receipt log identity is incomplete")
	}
	topics := make([]string, len(value.Topics))
	for index, topic := range value.Topics {
		// The first topic is always an event signature. Indexed event values
		// may legitimately be zero, so do not reject their all-zero topics.
		if index == 0 && topic == (common.Hash{}) {
			return finalCanonicalEVMLog{}, errors.New("canonical receipt log has an empty topic")
		}
		topics[index] = strings.ToLower(topic.Hex())
	}
	return finalCanonicalEVMLog{
		Address: strings.ToLower(value.Address.Hex()), Topics: topics, Data: hexutil.Encode(value.Data),
		BlockNumber: value.BlockNumber, BlockHash: strings.ToLower(value.BlockHash.Hex()),
		TransactionHash: strings.ToLower(value.TxHash.Hex()), TransactionIndex: uint64(value.TxIndex), LogIndex: uint64(value.Index),
	}, nil
}

func finalCanonicalLogsFromRPC(raw json.RawMessage, allowed map[common.Address]bool) ([]finalCanonicalEVMLog, error) {
	var wire []finalRPCLog
	if len(raw) == 0 || json.Unmarshal(raw, &wire) != nil {
		return nil, errors.New("receipt logs are not valid JSON-RPC logs")
	}
	result := make([]finalCanonicalEVMLog, 0, len(wire))
	for _, value := range wire {
		if len(allowed) > 0 && !allowed[value.Address] {
			continue
		}
		log, err := finalCanonicalLogFromGeth(ethTypes.Log{
			Address: value.Address, Topics: value.Topics, Data: value.Data,
			BlockNumber: uint64(value.BlockNumber), BlockHash: value.BlockHash,
			TxHash: value.TransactionHash, TxIndex: uint(value.TransactionIndex), Index: uint(value.LogIndex), Removed: value.Removed,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, log)
	}
	canonical, err := finalCanonicalizeLogs(result)
	if err != nil {
		return nil, err
	}
	for index := range result {
		if !finalSemanticCanonicalLogEqual(result[index], canonical[index]) {
			return nil, errors.New("receipt logs are not in canonical order")
		}
	}
	return canonical, nil
}

func finalCanonicalizeLogs(values []finalCanonicalEVMLog) ([]finalCanonicalEVMLog, error) {
	result := append([]finalCanonicalEVMLog(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].BlockNumber != result[j].BlockNumber {
			return result[i].BlockNumber < result[j].BlockNumber
		}
		if result[i].TransactionIndex != result[j].TransactionIndex {
			return result[i].TransactionIndex < result[j].TransactionIndex
		}
		return result[i].LogIndex < result[j].LogIndex
	})
	for index, value := range result {
		if !common.IsHexAddress(value.Address) || common.HexToAddress(value.Address) == (common.Address{}) || !finalCanonicalHex(value.TransactionHash, common.HashLength) || !finalCanonicalHex(value.BlockHash, common.HashLength) || !strings.HasPrefix(value.Data, "0x") {
			return nil, errors.New("canonical receipt log is malformed")
		}
		if _, err := hexutil.Decode(value.Data); err != nil || len(value.Topics) == 0 || !finalCanonicalHex(value.Topics[0], common.HashLength) || common.HexToHash(value.Topics[0]) == (common.Hash{}) {
			return nil, errors.New("canonical receipt log event payload is malformed")
		}
		for _, topic := range value.Topics[1:] {
			if !finalCanonicalHex(topic, common.HashLength) {
				return nil, errors.New("canonical receipt log topic is malformed")
			}
		}
		if index > 0 {
			prior := result[index-1]
			if value.BlockNumber == prior.BlockNumber && value.TransactionIndex == prior.TransactionIndex && value.LogIndex == prior.LogIndex {
				return nil, errors.New("canonical receipt logs contain a duplicate position")
			}
		}
	}
	return result, nil
}

// Accepts only a fixed-width lowercase-prefix hexadecimal wire value.
func finalCanonicalHex(value string, size int) bool {
	if len(value) != 2+size*2 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hexutil.Decode(value)
	return err == nil
}

// Compares every canonical receipt-log field without normalizing substitutions.
func finalSemanticCanonicalLogEqual(left, right finalCanonicalEVMLog) bool {
	if left.Address != right.Address || left.Data != right.Data || left.BlockNumber != right.BlockNumber || left.BlockHash != right.BlockHash || left.TransactionHash != right.TransactionHash || left.TransactionIndex != right.TransactionIndex || left.LogIndex != right.LogIndex || len(left.Topics) != len(right.Topics) {
		return false
	}
	for index := range left.Topics {
		if left.Topics[index] != right.Topics[index] {
			return false
		}
	}
	return true
}

func finalCanonicalReceiptLogsHash(values []finalCanonicalEVMLog) (string, error) {
	canonical, err := finalCanonicalizeLogs(values)
	if err != nil {
		return "", err
	}
	if len(canonical) == 0 {
		return "", errors.New("semantic EVM receipt has no release-contract logs")
	}
	transaction := canonical[0].TransactionHash
	block := canonical[0].BlockHash
	for _, value := range canonical[1:] {
		if value.TransactionHash != transaction || value.BlockHash != block {
			return "", fmt.Errorf("semantic receipt log group spans multiple transactions or blocks")
		}
	}
	return canonicalHashHex(canonical)
}

func finalCanonicalRPCReceiptLogsHash(raw json.RawMessage, allowed map[common.Address]bool) (string, error) {
	logs, err := finalCanonicalLogsFromRPC(raw, allowed)
	if err != nil {
		return "", err
	}
	return finalCanonicalReceiptLogsHash(logs)
}
