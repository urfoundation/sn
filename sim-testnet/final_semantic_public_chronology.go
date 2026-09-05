package main

// final_semantic_public_chronology.go decodes the narrow archive-RPC calls
// that prove a carried mutation's historical dispatch target and the absence
// of an unrecorded coordinator upgrade during the signed campaign range.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/stabi"
)

// Holds the exact JSON-RPC filter shape used for an inclusive proxy log range.
// A struct rather than a map keeps its content and test projection explicit.
type finalCoordinatorUpgradeLogFilter struct {
	Address   string   `json:"address"`
	FromBlock string   `json:"fromBlock"`
	ToBlock   string   `json:"toBlock"`
	Topics    []string `json:"topics"`
}

// Names one inclusive provider-safe subrange of a signed coordinator range.
// The public endpoint accepts at most one thousand blocks per log query, so
// all chunks are derived rather than left to caller-selected pagination.
type finalCoordinatorUpgradeRangeChunk struct {
	From uint64
	To   uint64
}

// Encodes the only public call accepted as a temporary-window oracle proof.
// Both the target and EIP-1898 selector are reconstructed so a generic call
// transcript cannot be relabeled as an active-oracle observation.
func finalCoordinatorActiveOracleCallParams(proxy string, head ChainHead) (json.RawMessage, error) {
	canonical, err := finalCanonicalAddress(proxy)
	if err != nil || canonical != proxy {
		return nil, stateMismatchError(err, "historical fleet refresh oracle proxy is not canonical")
	}
	if err := verifyFinalHead("historical fleet refresh oracle checkpoint", head); err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	return json.Marshal([]any{map[string]string{"to": canonical, "data": hexutil.Encode(coordinator.PackActiveCommitmentOracle())}, finalEVMBlockSelector{BlockHash: head.Hash, RequireCanonical: true}})
}

// Encodes one ABI address result in the exact JSON-RPC shape returned by
// eth_call, allowing transcript replay to independently decode its value.
func finalCoordinatorActiveOracleCallResult(oracle string) (json.RawMessage, error) {
	canonical, err := finalCanonicalAddress(oracle)
	if err != nil || canonical != oracle || common.HexToAddress(canonical) == (common.Address{}) {
		return nil, stateMismatchError(err, "historical fleet refresh active oracle is not canonical")
	}
	encoded := common.LeftPadBytes(common.HexToAddress(canonical).Bytes(), common.HashLength)
	return json.Marshal(hexutil.Encode(encoded))
}

// Captures the implementation slot and both executable byte hashes at one
// canonical historical head. Callers use separate observations for execution
// and post-transaction state because an upgrade changes the slot mid-row.
type finalHistoricalCoordinatorRuntimeObservation struct {
	Implementation             string
	ObservedImplementationSlot string
	ProxyRuntimeHash           string
	ImplementationRuntimeHash  string
}

// Builds the one permitted Upgraded(address) selector. The terminal pinned
// head authenticates the response, while canonical endpoint headers bind both
// numeric range boundaries to the signed chain.
func finalCoordinatorUpgradeLogFilterForRange(value FinalCoordinatorUpgradeRangeEvidence) (finalCoordinatorUpgradeLogFilter, error) {
	if err := verifyFinalCoordinatorUpgradeRange(value); err != nil {
		return finalCoordinatorUpgradeLogFilter{}, err
	}
	return finalCoordinatorUpgradeLogFilterForChunk(value, finalCoordinatorUpgradeRangeChunk{From: value.From.Number, To: value.To.Number})
}

// Materializes exact contiguous query chunks without overlap or a hidden
// skipped endpoint. A range exactly one thousand blocks long remains one
// request; a one-thousand-and-one block range is split at its inclusive edge.
func finalCoordinatorUpgradeRangeChunks(value FinalCoordinatorUpgradeRangeEvidence) ([]finalCoordinatorUpgradeRangeChunk, error) {
	if err := verifyFinalCoordinatorUpgradeRange(value); err != nil {
		return nil, err
	}
	ranges, err := finalEVMLogQueryRanges(value.From.Number, value.To.Number)
	if err != nil {
		return nil, err
	}
	result := make([]finalCoordinatorUpgradeRangeChunk, len(ranges))
	for index := range ranges {
		result[index] = finalCoordinatorUpgradeRangeChunk{From: ranges[index].From, To: ranges[index].To}
	}
	return result, nil
}

// Builds one exact filter after checking the chunk belongs to its signed
// parent. Keeping the terminal head on the outer range lets every request be
// transcript-pinned to one immutable canonical campaign checkpoint.
func finalCoordinatorUpgradeLogFilterForChunk(value FinalCoordinatorUpgradeRangeEvidence, chunk finalCoordinatorUpgradeRangeChunk) (finalCoordinatorUpgradeLogFilter, error) {
	if err := verifyFinalCoordinatorUpgradeRange(value); err != nil {
		return finalCoordinatorUpgradeLogFilter{}, err
	}
	if chunk.From < value.From.Number || chunk.To > value.To.Number || chunk.To < chunk.From || chunk.To-chunk.From+1 > finalEVMLogQueryMaximumBlocks {
		return finalCoordinatorUpgradeLogFilter{}, errors.New("coordinator upgrade query chunk is outside its signed range")
	}
	return finalCoordinatorUpgradeLogFilter{
		Address: value.Proxy, FromBlock: hexutil.EncodeUint64(chunk.From), ToBlock: hexutil.EncodeUint64(chunk.To),
		Topics: []string{strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex())},
	}, nil
}

// Decodes and normalizes one public log response without accepting a topic,
// address, block, or indexed implementation outside the signed filter.
func finalCoordinatorUpgradeRangeStates(raw json.RawMessage, value FinalCoordinatorUpgradeRangeEvidence) ([]FinalCoordinatorUpgradeRangeState, error) {
	return finalCoordinatorUpgradeRangeChunkStates(raw, value, finalCoordinatorUpgradeRangeChunk{From: value.From.Number, To: value.To.Number})
}

// Decodes one exact provider-safe range response and keeps true EVM order.
// Transaction indexes, not transaction-hash lexical order, define which UUPS
// transition preceded another transition in the same block.
func finalCoordinatorUpgradeRangeChunkStates(raw json.RawMessage, value FinalCoordinatorUpgradeRangeEvidence, chunk finalCoordinatorUpgradeRangeChunk) ([]FinalCoordinatorUpgradeRangeState, error) {
	filter, err := finalCoordinatorUpgradeLogFilterForChunk(value, chunk)
	if err != nil {
		return nil, err
	}
	var wire []struct {
		Address          string   `json:"address"`
		Topics           []string `json:"topics"`
		Data             string   `json:"data"`
		BlockNumber      string   `json:"blockNumber"`
		BlockHash        string   `json:"blockHash"`
		TransactionHash  string   `json:"transactionHash"`
		TransactionIndex string   `json:"transactionIndex"`
		LogIndex         string   `json:"logIndex"`
		Removed          *bool    `json:"removed"`
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &wire) != nil {
		return nil, errors.New("coordinator upgrade range returned invalid logs")
	}
	result := make([]FinalCoordinatorUpgradeRangeState, len(wire))
	for index, log := range wire {
		if log.Removed != nil && *log.Removed || !common.IsHexAddress(log.Address) || !strings.EqualFold(log.Address, filter.Address) || len(log.Topics) != 2 || !strings.EqualFold(log.Topics[0], filter.Topics[0]) || log.Data != "0x" {
			return nil, errors.New("coordinator upgrade range returned a malformed upgrade log")
		}
		block, blockErr := finalDecodeBlockNumber("coordinator upgrade log", log.BlockNumber)
		transactionIndex, transactionErr := finalDecodeBlockNumber("coordinator upgrade log transaction index", log.TransactionIndex)
		logIndex, indexErr := finalDecodeBlockNumber("coordinator upgrade log index", log.LogIndex)
		if blockErr != nil || transactionErr != nil || indexErr != nil || block < chunk.From || block > chunk.To {
			return nil, stateMismatchError(errors.Join(blockErr, transactionErr, indexErr), "coordinator upgrade log is outside its range")
		}
		if err := requireFinalHex32("coordinator upgrade log block", log.BlockHash); err != nil {
			return nil, err
		}
		if err := requireFinalHex32("coordinator upgrade log transaction", log.TransactionHash); err != nil {
			return nil, err
		}
		implementationBytes, decodeErr := hexutil.Decode(log.Topics[1])
		if decodeErr != nil || len(implementationBytes) != common.HashLength {
			return nil, stateMismatchError(decodeErr, "coordinator upgrade log implementation topic is invalid")
		}
		implementation := common.BytesToAddress(implementationBytes[common.HashLength-common.AddressLength:])
		if implementation == (common.Address{}) {
			return nil, errors.New("coordinator upgrade log implementation is zero")
		}
		result[index] = FinalCoordinatorUpgradeRangeState{Event: FinalCoordinatorUpgradeEventEvidence{
			Proxy: value.Proxy, TransactionHash: strings.ToLower(log.TransactionHash), TransactionIndex: transactionIndex, LogIndex: logIndex,
			Block: ChainHead{Number: block, Hash: strings.ToLower(log.BlockHash)}, Implementation: strings.ToLower(implementation.Hex()),
		}}
		if index > 0 {
			previous := result[index-1].Event
			current := result[index].Event
			if current.Block.Number < previous.Block.Number || current.Block.Number == previous.Block.Number && (current.TransactionIndex < previous.TransactionIndex || current.TransactionIndex == previous.TransactionIndex && current.LogIndex <= previous.LogIndex) {
				return nil, errors.New("coordinator upgrade logs are not canonically ordered")
			}
		}
	}
	return result, nil
}

// Reads the constructor's sealed post-state at its own canonical head. The
// initialization transaction does not delegate through a prior implementation,
// so this explicit observation is the only archive-RPC proof of its runtime.
func (self *PublicFinalSemanticChainReader) HistoricalCoordinatorBaseline(ctx context.Context, timeline FinalHistoricalCoordinatorProxyTimelineEvidence) (FinalHistoricalCoordinatorBaselineState, []FinalRPCExchange, error) {
	if self == nil {
		return FinalHistoricalCoordinatorBaselineState{}, nil, errors.New("historical coordinator baseline reader is unavailable")
	}
	if err := verifyFinalHead("historical coordinator baseline", timeline.Baseline); err != nil {
		return FinalHistoricalCoordinatorBaselineState{}, nil, err
	}
	observation, exchanges, err := self.historicalCoordinatorRuntime(ctx, timeline.Baseline, timeline.Proxy, self.evidence.Deployment.ERC1967ImplementationSlot)
	if err != nil {
		return FinalHistoricalCoordinatorBaselineState{}, nil, err
	}
	return FinalHistoricalCoordinatorBaselineState{
		Proxy:                      timeline.Proxy,
		Implementation:             observation.Implementation,
		ObservedImplementationSlot: observation.ObservedImplementationSlot,
		ProxyRuntimeHash:           observation.ProxyRuntimeHash,
		ImplementationRuntimeHash:  observation.ImplementationRuntimeHash,
		Block:                      timeline.Baseline,
	}, exchanges, nil
}

// Reads a historical proxy's active commitment oracle at one canonical
// archive head. The explicit proxy avoids importing a terminal deployment
// address into a predecessor action's proof.
func (self *PublicFinalSemanticChainReader) CoordinatorActiveCommitmentOracle(ctx context.Context, proxy string, head ChainHead) (FinalCoordinatorActiveCommitmentOracleState, []FinalRPCExchange, error) {
	if self == nil {
		return FinalCoordinatorActiveCommitmentOracleState{}, nil, errors.New("historical fleet refresh oracle reader is unavailable")
	}
	canonicalProxy, proxyErr := finalCanonicalAddress(proxy)
	if proxyErr != nil || canonicalProxy != proxy {
		return FinalCoordinatorActiveCommitmentOracleState{}, nil, stateMismatchError(proxyErr, "historical fleet refresh oracle proxy is not canonical")
	}
	if err := verifyFinalHead("historical fleet refresh oracle checkpoint", head); err != nil {
		return FinalCoordinatorActiveCommitmentOracleState{}, nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	output, exchanges, err := self.evmCall(ctx, head, canonicalProxy, coordinator.PackActiveCommitmentOracle())
	if err != nil {
		return FinalCoordinatorActiveCommitmentOracleState{}, nil, err
	}
	oracle, err := coordinator.UnpackActiveCommitmentOracle(output)
	if err != nil || oracle == (common.Address{}) {
		return FinalCoordinatorActiveCommitmentOracleState{}, nil, stateMismatchError(err, "historical fleet refresh active oracle is invalid")
	}
	return FinalCoordinatorActiveCommitmentOracleState{CoordinatorProxy: canonicalProxy, Oracle: strings.ToLower(oracle.Hex()), Block: head}, exchanges, nil
}

// Reads a receipt, transaction, dispatch slot, and both exact byte streams at
// one archived receipt block. It never calls the generic receipt decoder,
// whose current-release address census would reject a valid predecessor.
func (self *PublicFinalSemanticChainReader) HistoricalCoordinatorReceipt(ctx context.Context, row FinalHistoricalCoordinatorReceiptEvidence) (FinalHistoricalCoordinatorReceiptState, []FinalRPCExchange, error) {
	if self == nil {
		return FinalHistoricalCoordinatorReceiptState{}, nil, errors.New("historical coordinator reader is unavailable")
	}
	receiptRaw, receiptExchange, err := self.evmRaw(ctx, row.Receipt.Block, "eth_getTransactionReceipt", row.Receipt.TransactionHash)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptState{}, nil, err
	}
	var receiptWire struct {
		TransactionHash  string          `json:"transactionHash"`
		BlockHash        string          `json:"blockHash"`
		BlockNumber      string          `json:"blockNumber"`
		TransactionIndex string          `json:"transactionIndex"`
		Status           string          `json:"status"`
		Logs             json.RawMessage `json:"logs"`
	}
	if bytes.Equal(bytes.TrimSpace(receiptRaw), []byte("null")) || json.Unmarshal(receiptRaw, &receiptWire) != nil || receiptWire.TransactionHash == "" || receiptWire.BlockHash == "" || receiptWire.BlockNumber == "" || receiptWire.TransactionIndex == "" || len(receiptWire.Logs) == 0 {
		return FinalHistoricalCoordinatorReceiptState{}, nil, errors.New("historical coordinator receipt is incomplete")
	}
	block, err := finalDecodeBlockNumber("historical coordinator receipt", receiptWire.BlockNumber)
	receiptIndex, indexErr := finalDecodeBlockNumber("historical coordinator receipt transaction index", receiptWire.TransactionIndex)
	if err != nil || indexErr != nil || !strings.EqualFold(receiptWire.TransactionHash, row.Receipt.TransactionHash) || block != row.Receipt.Block.Number || !strings.EqualFold(receiptWire.BlockHash, row.Receipt.Block.Hash) || receiptIndex != row.TransactionIndex || receiptWire.Status != "0x1" {
		return FinalHistoricalCoordinatorReceiptState{}, nil, stateMismatchError(errors.Join(err, indexErr), "historical coordinator receipt differs from sealed coordinates")
	}
	logs, err := finalCanonicalLogsFromRPC(receiptWire.Logs, nil)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptState{}, nil, err
	}
	emitters := make([]string, 0, len(logs))
	for _, log := range logs {
		emitters = append(emitters, log.Address)
	}
	sort.Strings(emitters)
	emitters = slices.Compact(emitters)
	if !slices.Equal(emitters, row.Emitters) {
		return FinalHistoricalCoordinatorReceiptState{}, nil, errors.New("historical coordinator receipt emitter graph differs from archived action")
	}
	logsHash, err := finalCanonicalReceiptLogsHash(logs)
	if err != nil || !strings.EqualFold(logsHash, row.Receipt.LogsHash) {
		return FinalHistoricalCoordinatorReceiptState{}, nil, stateMismatchError(err, "historical coordinator receipt logs differ from sealed coordinates")
	}
	transactionRaw, transactionExchange, err := self.evmRaw(ctx, row.Receipt.Block, "eth_getTransactionByHash", row.Receipt.TransactionHash)
	if err != nil {
		return FinalHistoricalCoordinatorReceiptState{}, nil, err
	}
	var transactionWire struct {
		Hash             string `json:"hash"`
		BlockHash        string `json:"blockHash"`
		BlockNumber      string `json:"blockNumber"`
		TransactionIndex string `json:"transactionIndex"`
		From             string `json:"from"`
		To               string `json:"to"`
		Input            string `json:"input"`
		Value            string `json:"value"`
	}
	if bytes.Equal(bytes.TrimSpace(transactionRaw), []byte("null")) || json.Unmarshal(transactionRaw, &transactionWire) != nil || transactionWire.Hash == "" || transactionWire.BlockHash == "" || transactionWire.BlockNumber == "" || transactionWire.TransactionIndex == "" || transactionWire.From == "" || transactionWire.To == "" || transactionWire.Input == "" || transactionWire.Value == "" {
		return FinalHistoricalCoordinatorReceiptState{}, nil, errors.New("historical coordinator transaction is incomplete")
	}
	transactionBlock, err := finalDecodeBlockNumber("historical coordinator transaction", transactionWire.BlockNumber)
	transactionIndex, indexErr := finalDecodeBlockNumber("historical coordinator transaction index", transactionWire.TransactionIndex)
	from, fromErr := finalCanonicalAddress(transactionWire.From)
	to, toErr := finalCanonicalAddress(transactionWire.To)
	input, inputErr := hexutil.Decode(transactionWire.Input)
	value, valueErr := hexutil.DecodeBig(transactionWire.Value)
	if err != nil || indexErr != nil || fromErr != nil || toErr != nil || inputErr != nil || valueErr != nil || len(input) < 4 || value.Sign() < 0 || !strings.EqualFold(transactionWire.Hash, row.Receipt.TransactionHash) || transactionBlock != row.Receipt.Block.Number || transactionIndex != receiptIndex || transactionIndex != row.TransactionIndex || !strings.EqualFold(transactionWire.BlockHash, row.Receipt.Block.Hash) || from != row.TransactionFrom || to != row.TransactionTo || to != row.CoordinatorProxy || hexutil.Encode(input) != row.TransactionInput || value.String() != row.TransactionValueWei {
		return FinalHistoricalCoordinatorReceiptState{}, nil, stateMismatchError(errors.Join(err, indexErr, fromErr, toErr, inputErr, valueErr), "historical coordinator transaction differs from archived action")
	}
	post, postExchanges, err := self.historicalCoordinatorRuntime(ctx, row.Receipt.Block, row.CoordinatorProxy, row.CoordinatorImplementationSlot)
	if err != nil || post.Implementation != row.CoordinatorImplementation {
		return FinalHistoricalCoordinatorReceiptState{}, nil, stateMismatchError(err, "historical coordinator post runtime differs from archived plan")
	}
	state := FinalHistoricalCoordinatorReceiptState{
		Receipt:          FinalEVMReceiptState{TransactionHash: strings.ToLower(receiptWire.TransactionHash), Block: ChainHead{Number: block, Hash: strings.ToLower(receiptWire.BlockHash)}, Status: "success", LogsHash: logsHash},
		TransactionIndex: receiptIndex, From: from, To: to, Input: hexutil.Encode(input), ValueWei: value.String(), Emitters: emitters, CoordinatorProxy: row.CoordinatorProxy,
		CoordinatorImplementation: post.Implementation, ObservedImplementationSlot: post.ObservedImplementationSlot,
		CoordinatorProxyRuntimeHash: post.ProxyRuntimeHash, CoordinatorImplementationRuntimeHash: post.ImplementationRuntimeHash,
	}
	exchanges := append([]FinalRPCExchange{receiptExchange, transactionExchange}, postExchanges...)
	if row.ActionID == "evm.coordinator-proxy" {
		return state, exchanges, nil
	}
	execution, executionExchanges, executionErr := self.historicalCoordinatorRuntime(ctx, row.ExecutionHead, row.CoordinatorProxy, row.CoordinatorImplementationSlot)
	if executionErr != nil || execution.Implementation != row.ExecutionImplementation {
		return FinalHistoricalCoordinatorReceiptState{}, nil, stateMismatchError(executionErr, "historical coordinator execution runtime differs from archived plan")
	}
	state.ExecutionImplementation = execution.Implementation
	state.ExecutionObservedImplementationSlot = execution.ObservedImplementationSlot
	state.ExecutionProxyRuntimeHash = execution.ProxyRuntimeHash
	state.ExecutionImplementationRuntimeHash = execution.ImplementationRuntimeHash
	exchanges = append(exchanges, executionExchanges...)
	return state, exchanges, nil
}

// Reads the proxy slot and executable code at one EIP-1898-pinned head. The
// caller compares the decoded identity to the signed row instead of trusting
// a preselected implementation address in the request itself.
func (self *PublicFinalSemanticChainReader) historicalCoordinatorRuntime(ctx context.Context, head ChainHead, proxy, slotAddress string) (finalHistoricalCoordinatorRuntimeObservation, []FinalRPCExchange, error) {
	if err := verifyFinalHead("historical coordinator runtime", head); err != nil {
		return finalHistoricalCoordinatorRuntimeObservation{}, nil, err
	}
	slotRaw, slotExchange, err := self.evmRaw(ctx, head, "eth_getStorageAt", proxy, slotAddress, finalEVMBlockSelector{BlockHash: head.Hash, RequireCanonical: true})
	if err != nil {
		return finalHistoricalCoordinatorRuntimeObservation{}, nil, err
	}
	slot, err := finalDecodeRPCString("historical coordinator implementation slot", slotRaw)
	if err != nil {
		return finalHistoricalCoordinatorRuntimeObservation{}, nil, err
	}
	slotBytes, err := hexutil.Decode(slot)
	if err != nil || len(slotBytes) != common.HashLength {
		return finalHistoricalCoordinatorRuntimeObservation{}, nil, stateMismatchError(err, "historical coordinator implementation slot is invalid")
	}
	implementation := common.BytesToAddress(slotBytes[common.HashLength-common.AddressLength:])
	if implementation == (common.Address{}) {
		return finalHistoricalCoordinatorRuntimeObservation{}, nil, errors.New("historical coordinator implementation slot is zero")
	}
	proxyCode, proxyExchanges, err := self.evmCode(ctx, head, proxy)
	if err != nil {
		return finalHistoricalCoordinatorRuntimeObservation{}, nil, err
	}
	implementationCode, implementationExchanges, err := self.evmCode(ctx, head, strings.ToLower(implementation.Hex()))
	if err != nil {
		return finalHistoricalCoordinatorRuntimeObservation{}, nil, err
	}
	return finalHistoricalCoordinatorRuntimeObservation{
		Implementation: strings.ToLower(implementation.Hex()), ObservedImplementationSlot: strings.ToLower(slot),
		ProxyRuntimeHash: strings.ToLower(crypto.Keccak256Hash(proxyCode).Hex()), ImplementationRuntimeHash: strings.ToLower(crypto.Keccak256Hash(implementationCode).Hex()),
	}, append([]FinalRPCExchange{slotExchange}, append(proxyExchanges, implementationExchanges...)...), nil
}

// Replays every provider-safe chunk of the signed interval and preserves each
// raw result under the terminal canonical head. The concatenated event stream
// must still be globally ordered, so a provider cannot move an event across a
// chunk boundary without failing the sealed projection.
func (self *PublicFinalSemanticChainReader) CoordinatorUpgradeRange(ctx context.Context, value FinalCoordinatorUpgradeRangeEvidence) ([]FinalCoordinatorUpgradeRangeState, []FinalRPCExchange, error) {
	chunks, err := finalCoordinatorUpgradeRangeChunks(value)
	if err != nil {
		return nil, nil, err
	}
	states := make([]FinalCoordinatorUpgradeRangeState, 0)
	exchanges := make([]FinalRPCExchange, 0, len(chunks))
	for _, chunk := range chunks {
		filter, filterErr := finalCoordinatorUpgradeLogFilterForChunk(value, chunk)
		if filterErr != nil {
			return nil, nil, filterErr
		}
		raw, exchange, rawErr := self.evmRaw(ctx, value.To, "eth_getLogs", filter)
		if rawErr != nil {
			return nil, nil, rawErr
		}
		decoded, decodeErr := finalCoordinatorUpgradeRangeChunkStates(raw, value, chunk)
		if decodeErr != nil {
			return nil, nil, decodeErr
		}
		states = append(states, decoded...)
		exchanges = append(exchanges, exchange)
	}
	for index := 1; index < len(states); index++ {
		previous := states[index-1].Event
		current := states[index].Event
		if current.Block.Number < previous.Block.Number || current.Block.Number == previous.Block.Number && (current.TransactionIndex < previous.TransactionIndex || current.TransactionIndex == previous.TransactionIndex && current.LogIndex <= previous.LogIndex) {
			return nil, nil, errors.New("coordinator upgrade chunks do not form one canonical event sequence")
		}
	}
	return states, exchanges, nil
}

// Locates and validates the exact range request inside a persisted transcript.
// This turns a generic raw RPC hash into a semantic proof that the full signed
// interval, proxy address, selector, and decoded events were actually queried.
func verifyFinalPublicChronologyTranscript(verification *FinalPublicChainVerification) error {
	if verification == nil {
		return errors.New("public coordinator chronology transcript is unavailable")
	}
	if err := finalVerifyPublicFleetRefreshOracleWindowTranscript(verification); err != nil {
		return err
	}
	if err := finalVerifyPublicCoordinatorUpgradeRangeTranscript(verification, verification.ChronologyAudit.UpgradeRange, verification.ChronologyAudit.AllowedUpgrades); err != nil {
		return err
	}
	for _, rangeEvidence := range verification.ChronologyAudit.HistoricalUpgradeRanges {
		expected := finalCoordinatorUpgradeEventsForRange(verification.ChronologyAudit.HistoricalUpgrades, rangeEvidence)
		if err := finalVerifyPublicCoordinatorUpgradeRangeTranscript(verification, rangeEvidence, expected); err != nil {
			return err
		}
	}
	return nil
}

// Replays the exact activeCommitmentOracle calls required by the signed
// temporary-batcher interval. Equal observer heads use one request; a second
// identical request is rejected rather than allowed to hide a replacement.
func finalVerifyPublicFleetRefreshOracleWindowTranscript(verification *FinalPublicChainVerification) error {
	if verification == nil {
		return errors.New("public fleet refresh oracle transcript is unavailable")
	}
	checkpoints := verification.ChronologyAudit.OracleWindowCheckpoints
	if err := verifyFinalFleetRefreshOracleWindowCheckpoints(checkpoints); err != nil {
		return err
	}
	proxy, proxyErr := finalCanonicalAddress(checkpoints.CoordinatorProxy)
	if proxyErr != nil || proxy != checkpoints.CoordinatorProxy {
		return stateMismatchError(proxyErr, "public fleet refresh oracle proxy is not canonical")
	}
	requests := make([]finalFleetRefreshOracleCheckpointRow, 0, 4)
	seenHeads := make(map[ChainHead]string)
	for _, row := range finalFleetRefreshOracleCheckpointRows(checkpoints) {
		if prior, found := seenHeads[row.value.Head]; found {
			if prior != row.value.Oracle {
				return errors.New("public fleet refresh oracle transcript has conflicting checkpoint aliases")
			}
			continue
		}
		seenHeads[row.value.Head] = row.value.Oracle
		requests = append(requests, row)
	}
	for _, request := range requests {
		wantParams, paramsErr := finalCoordinatorActiveOracleCallParams(proxy, request.value.Head)
		if paramsErr != nil {
			return paramsErr
		}
		var found *FinalRPCExchange
		for index := range verification.Exchanges {
			exchange := &verification.Exchanges[index]
			if exchange.Chain != "evm" || exchange.Method != "eth_call" || exchange.PinnedHead != request.value.Head || !bytes.Equal(exchange.Params, wantParams) {
				continue
			}
			if found != nil {
				return errors.New("public fleet refresh oracle transcript has duplicate checkpoint calls")
			}
			found = exchange
		}
		if found == nil {
			return fmt.Errorf("public fleet refresh oracle transcript lacks %s checkpoint", request.label)
		}
		encoded, decodeErr := finalDecodeRPCString("public fleet refresh active oracle", found.Result)
		if decodeErr != nil {
			return decodeErr
		}
		raw, hexErr := hexutil.Decode(encoded)
		oracle, unpackErr := stabi.NewSTCoordinator().UnpackActiveCommitmentOracle(raw)
		if hexErr != nil || unpackErr != nil || oracle == (common.Address{}) || !strings.EqualFold(oracle.Hex(), request.value.Oracle) {
			return stateMismatchError(errors.Join(hexErr, unpackErr), "public fleet refresh oracle %s result differs", request.label)
		}
	}
	return nil
}

// Replays every exact chunk for one signed proxy interval from transcript
// bytes. A missing, duplicate, overlapping, or out-of-order response fails
// before the broader public verifier trusts the resulting event projection.
func finalVerifyPublicCoordinatorUpgradeRangeTranscript(verification *FinalPublicChainVerification, rangeEvidence FinalCoordinatorUpgradeRangeEvidence, expected []FinalCoordinatorUpgradeEventEvidence) error {
	if verification == nil {
		return errors.New("public coordinator chronology transcript is unavailable")
	}
	chunks, err := finalCoordinatorUpgradeRangeChunks(rangeEvidence)
	if err != nil {
		return err
	}
	states := make([]FinalCoordinatorUpgradeRangeState, 0)
	matched := make(map[int]bool, len(verification.Exchanges))
	for _, chunk := range chunks {
		filter, filterErr := finalCoordinatorUpgradeLogFilterForChunk(rangeEvidence, chunk)
		if filterErr != nil {
			return filterErr
		}
		wantParams, paramsErr := json.Marshal([]any{filter})
		if paramsErr != nil {
			return paramsErr
		}
		var found *FinalRPCExchange
		for index := range verification.Exchanges {
			exchange := &verification.Exchanges[index]
			if exchange.Chain != "evm" || exchange.Method != "eth_getLogs" || exchange.PinnedHead != rangeEvidence.To || !bytes.Equal(exchange.Params, wantParams) {
				continue
			}
			if found != nil {
				return errors.New("public coordinator chronology transcript has duplicate upgrade range requests")
			}
			found = exchange
			matched[index] = true
		}
		if found == nil {
			return errors.New("public coordinator chronology transcript lacks a pinned upgrade range chunk")
		}
		decoded, decodeErr := finalCoordinatorUpgradeRangeChunkStates(found.Result, rangeEvidence, chunk)
		if decodeErr != nil {
			return decodeErr
		}
		states = append(states, decoded...)
	}
	for index := range verification.Exchanges {
		exchange := &verification.Exchanges[index]
		if exchange.Chain != "evm" || exchange.Method != "eth_getLogs" || exchange.PinnedHead != rangeEvidence.To || matched[index] {
			continue
		}
		var parameters []finalCoordinatorUpgradeLogFilter
		if json.Unmarshal(exchange.Params, &parameters) != nil || len(parameters) != 1 || !strings.EqualFold(parameters[0].Address, rangeEvidence.Proxy) || len(parameters[0].Topics) != 1 || !strings.EqualFold(parameters[0].Topics[0], strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex())) {
			continue
		}
		return errors.New("public coordinator chronology transcript has an unrecognized upgrade range chunk")
	}
	for index := 1; index < len(states); index++ {
		previous := states[index-1].Event
		current := states[index].Event
		if current.Block.Number < previous.Block.Number || current.Block.Number == previous.Block.Number && (current.TransactionIndex < previous.TransactionIndex || current.TransactionIndex == previous.TransactionIndex && current.LogIndex <= previous.LogIndex) {
			return errors.New("public coordinator chronology transcript upgrade chunks are not globally ordered")
		}
	}
	if len(states) != len(expected) {
		return errors.New("public coordinator chronology transcript event count differs from its projection")
	}
	for index := range states {
		if states[index].Event != expected[index] {
			return errors.New("public coordinator chronology transcript event differs from its projection")
		}
	}
	return nil
}
