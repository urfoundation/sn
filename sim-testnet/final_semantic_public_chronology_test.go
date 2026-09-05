package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Creates one canonical coordinator range with enough blocks to exercise the
// public endpoint's inclusive one-thousand-block pagination boundary.
func finalCoordinatorUpgradeRangeTestEvidence() FinalCoordinatorUpgradeRangeEvidence {
	return FinalCoordinatorUpgradeRangeEvidence{
		From: ChainHead{Number: 10, Hash: finalTestHex(0x11)}, To: ChainHead{Number: 1010, Hash: finalTestHex(0x12)},
		Proxy: strings.ToLower(common.HexToAddress("0x1000000000000000000000000000000000000001").Hex()),
	}
}

// Supplies the two distinct oracle states used by the transcript-focused
// tests. Each observer pair intentionally shares a head to exercise the
// deduplicated public request path used by the public override profile.
func finalOracleWindowTestCheckpoints(proxy string) FinalFleetRefreshOracleWindowCheckpoints {
	return FinalFleetRefreshOracleWindowCheckpoints{
		CoordinatorProxy:         proxy,
		AwaitActiveOperational:   FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 20, Hash: finalTestHex(0x21)}, Oracle: strings.ToLower(common.HexToAddress("0x2000000000000000000000000000000000000002").Hex())},
		AwaitActiveIndependent:   FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 20, Hash: finalTestHex(0x21)}, Oracle: strings.ToLower(common.HexToAddress("0x2000000000000000000000000000000000000002").Hex())},
		AwaitRestoredOperational: FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 40, Hash: finalTestHex(0x22)}, Oracle: strings.ToLower(common.HexToAddress("0x3000000000000000000000000000000000000003").Hex())},
		AwaitRestoredIndependent: FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 40, Hash: finalTestHex(0x22)}, Oracle: strings.ToLower(common.HexToAddress("0x3000000000000000000000000000000000000003").Hex())},
	}
}

// Assembles every exact RPC exchange consumed by a narrow chronology replay.
// The oracle requests precede log chunks only to make selective test mutation
// deterministic; verification itself matches semantic request content.
func finalPublicChronologyTranscriptTestFixture(t *testing.T, rangeEvidence FinalCoordinatorUpgradeRangeEvidence) *FinalPublicChainVerification {
	t.Helper()
	checkpoints := finalOracleWindowTestCheckpoints(rangeEvidence.Proxy)
	exchanges := make([]FinalRPCExchange, 0, 4)
	seen := map[ChainHead]bool{}
	for _, row := range finalFleetRefreshOracleCheckpointRows(checkpoints) {
		if seen[row.value.Head] {
			continue
		}
		params, paramsErr := finalCoordinatorActiveOracleCallParams(checkpoints.CoordinatorProxy, row.value.Head)
		result, resultErr := finalCoordinatorActiveOracleCallResult(row.value.Oracle)
		if paramsErr != nil || resultErr != nil {
			t.Fatal(errors.Join(paramsErr, resultErr))
		}
		exchanges = append(exchanges, FinalRPCExchange{Chain: "evm", Method: "eth_call", Params: params, PinnedHead: row.value.Head, Result: result})
		seen[row.value.Head] = true
	}
	chunks, err := finalCoordinatorUpgradeRangeChunks(rangeEvidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunks {
		filter, filterErr := finalCoordinatorUpgradeLogFilterForChunk(rangeEvidence, chunk)
		if filterErr != nil {
			t.Fatal(filterErr)
		}
		params, paramsErr := json.Marshal([]any{filter})
		if paramsErr != nil {
			t.Fatal(paramsErr)
		}
		exchanges = append(exchanges, FinalRPCExchange{Chain: "evm", Method: "eth_getLogs", Params: params, PinnedHead: rangeEvidence.To, Result: json.RawMessage("[]")})
	}
	return &FinalPublicChainVerification{ChronologyAudit: FinalPublicChronologyAudit{UpgradeRange: rangeEvidence, OracleWindowCheckpoints: checkpoints}, Exchanges: exchanges}
}

// Clones only mutable transcript fields so each mutation begins from the
// exact accepted request set rather than carrying a prior failure forward.
func finalPublicChronologyTranscriptTestClone(value *FinalPublicChainVerification) *FinalPublicChainVerification {
	result := *value
	result.Exchanges = append([]FinalRPCExchange(nil), value.Exchanges...)
	return &result
}

// Models the narrow live reader surface without inheriting a mutable terminal
// deployment object. It records each requested proxy so tests can prove the
// historical action target reaches the archive call unchanged.
type finalFleetRefreshOracleWindowTestReader struct {
	checkpoints   FinalFleetRefreshOracleWindowCheckpoints
	reportedProxy string
	requested     []string
}

// Emits one pinned active-oracle response for an explicitly requested proxy.
func (self *finalFleetRefreshOracleWindowTestReader) CoordinatorActiveCommitmentOracle(_ context.Context, proxy string, head ChainHead) (FinalCoordinatorActiveCommitmentOracleState, []FinalRPCExchange, error) {
	if self == nil || proxy != self.checkpoints.CoordinatorProxy {
		return FinalCoordinatorActiveCommitmentOracleState{}, nil, errors.New("historical oracle reader received another coordinator proxy")
	}
	self.requested = append(self.requested, proxy)
	var oracle string
	for _, row := range finalFleetRefreshOracleCheckpointRows(self.checkpoints) {
		if row.value.Head != head {
			continue
		}
		if oracle != "" && oracle != row.value.Oracle {
			return FinalCoordinatorActiveCommitmentOracleState{}, nil, errors.New("historical oracle reader received conflicting checkpoint aliases")
		}
		oracle = row.value.Oracle
	}
	if oracle == "" {
		return FinalCoordinatorActiveCommitmentOracleState{}, nil, errors.New("historical oracle reader received an unknown checkpoint")
	}
	params, paramsErr := finalCoordinatorActiveOracleCallParams(proxy, head)
	result, resultErr := finalCoordinatorActiveOracleCallResult(oracle)
	if paramsErr != nil || resultErr != nil {
		return FinalCoordinatorActiveCommitmentOracleState{}, nil, errors.Join(paramsErr, resultErr)
	}
	reportedProxy := self.reportedProxy
	if reportedProxy == "" {
		reportedProxy = proxy
	}
	return FinalCoordinatorActiveCommitmentOracleState{CoordinatorProxy: reportedProxy, Oracle: oracle, Block: head}, []FinalRPCExchange{{Chain: "evm", Method: "eth_call", Params: params, PinnedHead: head, Result: result}}, nil
}

// Confirms the public projection splits a 1001-block interval into exactly
// adjacent inclusive chunks, preserving both endpoints without provider-size
// violations.
func TestFinalSemanticCoordinatorUpgradeRangeChunksCoverOfficialLimit(t *testing.T) {
	rangeEvidence := finalCoordinatorUpgradeRangeTestEvidence()
	chunks, err := finalCoordinatorUpgradeRangeChunks(rangeEvidence)
	if err != nil || len(chunks) != 2 || chunks[0] != (finalCoordinatorUpgradeRangeChunk{From: 10, To: 1009}) || chunks[1] != (finalCoordinatorUpgradeRangeChunk{From: 1010, To: 1010}) {
		t.Fatalf("chunks=%+v err=%v, want [10,1009],[1010,1010]", chunks, err)
	}
	for index := range chunks {
		if chunks[index].To-chunks[index].From+1 > finalEVMLogQueryMaximumBlocks || index > 0 && chunks[index].From != chunks[index-1].To+1 {
			t.Fatalf("chunk %d is oversized or noncontiguous: %+v", index, chunks)
		}
	}
}

// Decodes true EVM transaction order from a raw UUPS log instead of using the
// transaction hash as an accidental lexical ordering surrogate.
func TestFinalSemanticCoordinatorUpgradeChunkBindsTransactionIndex(t *testing.T) {
	rangeEvidence := finalCoordinatorUpgradeRangeTestEvidence()
	chunk := finalCoordinatorUpgradeRangeChunk{From: 10, To: 1009}
	implementation := strings.ToLower(common.HexToAddress("0x2000000000000000000000000000000000000002").Hex())
	topic := strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex())
	raw, err := json.Marshal([]map[string]any{{
		"address": rangeEvidence.Proxy, "topics": []string{topic, common.BytesToHash(common.HexToAddress(implementation).Bytes()).Hex()}, "data": "0x",
		"blockNumber": "0x14", "blockHash": finalTestHex(0x13), "transactionHash": finalTestHex(0x14), "transactionIndex": "0x2", "logIndex": "0x1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	states, err := finalCoordinatorUpgradeRangeChunkStates(raw, rangeEvidence, chunk)
	if err != nil || len(states) != 1 || states[0].Event.Proxy != rangeEvidence.Proxy || states[0].Event.TransactionIndex != 2 || states[0].Event.LogIndex != 1 || states[0].Event.Implementation != implementation {
		t.Fatalf("states=%+v err=%v, want transaction-indexed event", states, err)
	}
}

// Requires every derived chunk exactly once in the sealed transcript; an
// omitted, duplicated, or foreign overlapping query cannot stand in for the
// full historical interval.
func TestFinalSemanticCoordinatorUpgradeTranscriptRejectsIncompleteChunks(t *testing.T) {
	rangeEvidence := finalCoordinatorUpgradeRangeTestEvidence()
	verification := finalPublicChronologyTranscriptTestFixture(t, rangeEvidence)
	if err := verifyFinalPublicChronologyTranscript(verification); err != nil {
		t.Fatalf("complete chunk transcript rejected: %v", err)
	}
	missing := finalPublicChronologyTranscriptTestClone(verification)
	missing.Exchanges = missing.Exchanges[:len(missing.Exchanges)-1]
	if err := verifyFinalPublicChronologyTranscript(missing); err == nil {
		t.Fatal("accepted a transcript with a missing log-range chunk")
	}
}

// Rejects substitutions, duplicates, and false oracle values before a public
// transcript can claim that a historical temporary batcher was active.
func TestFinalSemanticHistoricalOracleWindowTranscriptRejectsMutations(t *testing.T) {
	baseline := finalPublicChronologyTranscriptTestFixture(t, finalCoordinatorUpgradeRangeTestEvidence())
	if err := verifyFinalPublicChronologyTranscript(baseline); err != nil {
		t.Fatalf("exact oracle transcript rejected: %v", err)
	}
	duplicate := finalPublicChronologyTranscriptTestClone(baseline)
	duplicate.Exchanges = append(duplicate.Exchanges, duplicate.Exchanges[0])
	if err := verifyFinalPublicChronologyTranscript(duplicate); err == nil {
		t.Fatal("accepted duplicate historical oracle call")
	}
	wrongProxy := finalPublicChronologyTranscriptTestClone(baseline)
	params, paramsErr := finalCoordinatorActiveOracleCallParams("0x4000000000000000000000000000000000000004", wrongProxy.Exchanges[0].PinnedHead)
	if paramsErr != nil {
		t.Fatal(paramsErr)
	}
	wrongProxy.Exchanges[0].Params = params
	if err := verifyFinalPublicChronologyTranscript(wrongProxy); err == nil {
		t.Fatal("accepted terminal or substituted oracle proxy")
	}
	wrongOracle := finalPublicChronologyTranscriptTestClone(baseline)
	result, resultErr := finalCoordinatorActiveOracleCallResult("0x5000000000000000000000000000000000000005")
	if resultErr != nil {
		t.Fatal(resultErr)
	}
	wrongOracle.Exchanges[0].Result = result
	if err := verifyFinalPublicChronologyTranscript(wrongOracle); err == nil {
		t.Fatal("accepted false historical active oracle")
	}
}

// Requires the live archive call and its returned state to retain the
// historical schedule proxy instead of silently reading terminal deployment.
func TestFinalSemanticHistoricalOracleWindowOnChainBindsCoordinatorProxy(t *testing.T) {
	checkpoints := finalOracleWindowTestCheckpoints(finalCoordinatorUpgradeRangeTestEvidence().Proxy)
	reader := &finalFleetRefreshOracleWindowTestReader{checkpoints: checkpoints}
	var exchanges []FinalRPCExchange
	appendExchanges := func(chain string, head ChainHead, values []FinalRPCExchange) error {
		if chain != "evm" || len(values) != 1 || values[0].PinnedHead != head {
			return errors.New("historical oracle append received an unexpected transcript")
		}
		exchanges = append(exchanges, values...)
		return nil
	}
	if err := verifyFinalSemanticFleetRefreshOracleWindowOnChain(context.Background(), checkpoints, reader, appendExchanges); err != nil {
		t.Fatalf("exact historical oracle reader rejected: %v", err)
	}
	if len(reader.requested) != 2 || len(exchanges) != 2 {
		t.Fatalf("oracle calls/exchanges=%d/%d, want 2/2", len(reader.requested), len(exchanges))
	}
	for _, proxy := range reader.requested {
		if proxy != checkpoints.CoordinatorProxy {
			t.Fatalf("oracle reader used %s, want %s", proxy, checkpoints.CoordinatorProxy)
		}
	}
	wrongState := &finalFleetRefreshOracleWindowTestReader{checkpoints: checkpoints, reportedProxy: "0x4000000000000000000000000000000000000004"}
	if err := verifyFinalSemanticFleetRefreshOracleWindowOnChain(context.Background(), checkpoints, wrongState, appendExchanges); err == nil {
		t.Fatal("accepted a live oracle state attributed to another proxy")
	}
}
