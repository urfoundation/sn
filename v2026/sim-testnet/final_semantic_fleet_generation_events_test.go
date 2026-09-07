package main

// final_semantic_fleet_generation_events_test.go proves that lineage receipt
// logs are decoded into exact semantic fields and cannot be replaced by a
// different permitted event or a partial receipt projection.

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

// Supplies controlled exact write replies for the narrow lineage surface.
// It deliberately has no generic receipt method: generation replay must use
// the dedicated full-log and transaction-input reader.
type finalFleetGenerationWriteTestReader struct {
	state   FinalFleetGenerationEVMWriteState
	runtime FinalFleetGenerationRuntimeState
}

// Returns the preconstructed result so negative tests can alter precisely one
// receipt property without changing the sealed evidence object.
func (self *finalFleetGenerationWriteTestReader) FleetGenerationEVMWrite(_ context.Context, _ FinalFleetGenerationWriteEvidence) (FinalFleetGenerationEVMWriteState, []FinalRPCExchange, error) {
	return self.state, nil, nil
}

// Is unused by direct write checks, but makes this fake satisfy the complete
// narrow lineage surface and protects that interface from accidental growth.
func (self *finalFleetGenerationWriteTestReader) NativeFleetCommitment(_ context.Context, _ uint16, _ string, _ ChainHead) (FinalNativeFleetCommitmentState, []FinalRPCExchange, error) {
	return FinalNativeFleetCommitmentState{}, nil, errors.New("native commitment is outside this write test")
}

// Returns the independent proxy and code projection for the write boundary.
func (self *finalFleetGenerationWriteTestReader) FleetGenerationRuntime(_ context.Context, _ FinalFleetGenerationWriteEvidence) (FinalFleetGenerationRuntimeState, []FinalRPCExchange, error) {
	return self.runtime, nil, nil
}

var _ finalFleetGenerationChainReader = (*finalFleetGenerationWriteTestReader)(nil)
var _ finalFleetGenerationRuntimeReader = (*finalFleetGenerationWriteTestReader)(nil)

// exercises each concrete event kind a carried or refresh write may retain.
func TestFinalFleetGenerationDecodesExactReceiptEventFields(t *testing.T) {
	t.Parallel()
	evidence := finalFleetGenerationEventFixture(t)
	values := []struct {
		action string
		name   string
	}{
		{action: "fleet.mirror.1", name: "CommitmentMirrored"},
		{action: "fleet.bind.1.1", name: "FleetBound"},
		{action: "fleet.install.batch.1", name: "CommitmentMirrored"},
		{action: "fleet.install.batch.1", name: "FleetBound"},
		{action: "fleet.install.batch.1", name: "FleetMemberBound"},
		{action: "fleet.install.batch.1", name: "FleetInstalled"},
		{action: "fleet.refresh.batch.1", name: "CommitmentMirrored"},
		{action: "fleet.refresh.batch.1", name: "FleetBindingRevoked"},
		{action: "fleet.refresh.batch.1", name: "FleetBound"},
		{action: "fleet.refresh.batch.1", name: "FleetMemberBound"},
		{action: "fleet.refresh.batch.1", name: "FleetRefreshed"},
	}
	for index, value := range values {
		log := finalFleetGenerationTestABIEventLog(t, evidence, value.action, value.name, uint64(index+1))
		decoded, err := finalFleetGenerationDecodeEvent(evidence, value.action, log)
		if err != nil {
			t.Fatalf("decode %s: %v", value.name, err)
		}
		if decoded.Name != value.name || decoded.Evidence.Contract != log.Address || decoded.Evidence.Kind != log.Topics[0] || !finalSemanticCanonicalLogEqual(decoded.Evidence.Log, log) {
			t.Fatalf("decoded %s identity differs: %+v", value.name, decoded.Evidence)
		}
		if decoded.Evidence.Hotkey == "" && decoded.Evidence.ClientID == "" {
			t.Fatalf("decoded %s has no bound identity", value.name)
		}
		if (value.name == "FleetInstalled" || value.name == "FleetRefreshed") && decoded.Evidence.MemberCount != 4 {
			t.Fatalf("%s member count=%d, want 4", value.name, decoded.Evidence.MemberCount)
		}
	}
}

// rejects a semantic field rewrite, an event topic rewrite, and a mismatched
// action class even when the underlying log otherwise remains ABI-decodable.
func TestFinalFleetGenerationRejectsReceiptEventFieldOrTopicSubstitution(t *testing.T) {
	t.Parallel()
	evidence := finalFleetGenerationEventFixture(t)
	log := finalFleetGenerationTestABIEventLog(t, evidence, "fleet.mirror.1", "CommitmentMirrored", 1)
	decoded, err := finalFleetGenerationDecodeEvent(evidence, "fleet.mirror.1", log)
	if err != nil {
		t.Fatal(err)
	}
	write := FinalFleetGenerationWriteEvidence{Action: FinalFleetGenerationActionEvidence{ActionID: "fleet.mirror.1"}}
	if err := verifyFinalFleetGenerationWriteEvent(evidence, write, decoded.Evidence); err != nil {
		t.Fatalf("verify valid event: %v", err)
	}
	corruptField := decoded.Evidence
	corruptField.CommitmentHash = finalFleetGenerationTestHash(99_001)
	if err := verifyFinalFleetGenerationWriteEvent(evidence, write, corruptField); err == nil {
		t.Fatal("accepted substituted CommitmentMirrored commitment")
	}
	wrongActionLog := finalFleetGenerationTestABIEventLog(t, evidence, "fleet.bind.1.1", "FleetBound", 2)
	if _, err := finalFleetGenerationDecodeEvent(evidence, "fleet.mirror.1", wrongActionLog); err == nil {
		t.Fatal("accepted FleetBound for a mirror action")
	}
	wrongTopic := decoded.Evidence
	wrongTopic.Log.Topics[0] = finalFleetGenerationTestHash(99_002)
	wrongTopic.Kind = wrongTopic.Log.Topics[0]
	if err := verifyFinalFleetGenerationWriteEvent(evidence, write, wrongTopic); err == nil {
		t.Fatal("accepted an unknown receipt event topic")
	}
}

// rejects a missing, extra, reordered, or wrong-target log before a reviewer
// could treat a partial public-provider receipt as a complete mutation proof.
func TestFinalFleetGenerationRejectsPartialOrSubstitutedPublicWrite(t *testing.T) {
	t.Parallel()
	evidence := finalFleetGenerationEventFixture(t)
	log := finalFleetGenerationTestABIEventLog(t, evidence, "fleet.mirror.1", "CommitmentMirrored", 1)
	decoded, err := finalFleetGenerationDecodeEvent(evidence, "fleet.mirror.1", log)
	if err != nil {
		t.Fatal(err)
	}
	calldata := "0x01020304"
	calldataBytes, err := decodeEvidenceHex(calldata)
	if err != nil {
		t.Fatal(err)
	}
	logsHash, err := finalCanonicalReceiptLogsHash([]finalCanonicalEVMLog{log})
	if err != nil {
		t.Fatal(err)
	}
	eventHash, err := canonicalHashHex([]FinalFleetGenerationEventEvidence{decoded.Evidence})
	if err != nil {
		t.Fatal(err)
	}
	write := FinalFleetGenerationWriteEvidence{
		Action:   FinalFleetGenerationActionEvidence{ActionID: "fleet.mirror.1", PlanHash: finalFleetGenerationTestHash(101), IntentHash: finalFleetGenerationTestHash(102)},
		Receipt:  FinalEVMReceipt{TransactionHash: log.TransactionHash, Block: ChainHead{Number: log.BlockNumber, Hash: log.BlockHash}, Status: "success", LogsHash: logsHash, Proof: finalFleetGenerationTestArtifact("evm-receipt", "public-write")},
		Calldata: calldata, CalldataHash: crypto.Keccak256Hash(calldataBytes).Hex(), EventHash: eventHash, Events: []FinalFleetGenerationEventEvidence{decoded.Evidence},
		CoordinatorProxy: evidence.Deployment.CoordinatorProxy, CoordinatorImplementation: evidence.Deployment.CoordinatorImplementation,
		CoordinatorImplementationSlot: evidence.Deployment.ObservedImplementationSlot, CoordinatorProxyRuntimeHash: evidence.Deployment.CoordinatorProxyCodeHash,
		CoordinatorRuntimeHash: evidence.Deployment.ImplementationCodeHash,
		Postcondition:          finalFleetGenerationTestArtifact("fleet-generation-postcondition", "public-write"),
		EVMHead:                ChainHead{Number: log.BlockNumber, Hash: log.BlockHash}, NativeHead: ChainHead{Number: log.BlockNumber, Hash: log.BlockHash},
	}
	state := FinalFleetGenerationEVMWriteState{TransactionHash: write.Receipt.TransactionHash, To: strings.ToLower(evidence.Deployment.CoordinatorProxy), Calldata: calldata, Block: write.Receipt.Block, Status: "success", Logs: []finalCanonicalEVMLog{log}}
	appendExchanges := func(string, ChainHead, []FinalRPCExchange) error { return nil }
	reader := &finalFleetGenerationWriteTestReader{state: state}
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err != nil {
		t.Fatalf("verify valid public write: %v", err)
	}
	reader.state.Logs = nil
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err == nil {
		t.Fatal("accepted a receipt missing its lineage event")
	}
	reader.state.Logs = []finalCanonicalEVMLog{log, log}
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err == nil {
		t.Fatal("accepted a receipt with an unrepresented duplicate event")
	}
	reader.state.Logs = []finalCanonicalEVMLog{log}
	reader.state.To = finalFleetGenerationTestBatcher
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err == nil {
		t.Fatal("accepted a historical write to another release contract")
	}
}

// Proves the raw ten-log install group cannot lose, duplicate, or relocate a
// coordinator/batcher event while preserving plausible fleet identities. The
// retained attempt-4 batch-three receipt
// 0x5fc320087783cc7ad9a1c319b9d25deadd0794237b3573b1f7aa327fd570876e
// at block 7900873 has five installed fleets and exactly fifty logs: one
// mirror, four coordinator/batcher member pairs, and one installed summary
// for each fleet.
func TestFinalFleetGenerationInstalledReceiptTopologyRejectsOmissionDuplicateAndWrongEmitter(t *testing.T) {
	t.Parallel()
	evidence := finalFleetGenerationEventFixture(t)
	lineage, batch, write := finalFleetGenerationInstalledTopologyFixture(t, evidence)
	if err := verifyFinalFleetGenerationInstallEvents(evidence, lineage, batch, write); err != nil {
		t.Fatalf("accept exact installed receipt topology: %v", err)
	}
	omitted := write
	omitted.Events = append([]FinalFleetGenerationEventEvidence(nil), write.Events[:len(write.Events)-1]...)
	if err := verifyFinalFleetGenerationInstallEvents(evidence, lineage, batch, omitted); err == nil {
		t.Fatal("accepted an installed receipt with an omitted FleetInstalled event")
	}
	duplicated := write
	duplicated.Events = append([]FinalFleetGenerationEventEvidence(nil), write.Events...)
	duplicated.Events[1] = duplicated.Events[len(duplicated.Events)-1]
	if err := verifyFinalFleetGenerationInstallEvents(evidence, lineage, batch, duplicated); err == nil {
		t.Fatal("accepted an installed receipt with a duplicate summary event")
	}
	wrongEmitter := write
	wrongEmitter.Events = append([]FinalFleetGenerationEventEvidence(nil), write.Events...)
	wrongEmitter.Events[len(wrongEmitter.Events)-1].Contract = evidence.Deployment.CoordinatorProxy
	wrongEmitter.Events[len(wrongEmitter.Events)-1].Log.Address = evidence.Deployment.CoordinatorProxy
	if err := verifyFinalFleetGenerationInstallEvents(evidence, lineage, batch, wrongEmitter); err == nil {
		t.Fatal("accepted a FleetInstalled event from the coordinator emitter")
	}
}

// checks the public receipt path for an installed batch separately from the
// source build, including exact batcher target, input bytes, and event emitter.
func TestFinalFleetGenerationInstalledPublicWriteRejectsWrongTargetCalldataOrEmitter(t *testing.T) {
	t.Parallel()
	evidence := finalFleetGenerationEventFixture(t)
	_, _, write := finalFleetGenerationInstalledTopologyFixture(t, evidence)
	state := FinalFleetGenerationEVMWriteState{
		TransactionHash: write.Receipt.TransactionHash, To: write.BatcherAddress, Calldata: write.Calldata,
		Block: write.Receipt.Block, Status: write.Receipt.Status,
	}
	for _, event := range write.Events {
		state.Logs = append(state.Logs, event.Log)
	}
	reader := &finalFleetGenerationWriteTestReader{state: state}
	appendExchanges := func(string, ChainHead, []FinalRPCExchange) error { return nil }
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err != nil {
		t.Fatalf("accept exact installed public write: %v", err)
	}
	reader.state.To = evidence.Deployment.CoordinatorProxy
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err == nil {
		t.Fatal("accepted an installed write with another target")
	}
	reader.state.To = write.BatcherAddress
	reader.state.Calldata = "0x01020305"
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err == nil {
		t.Fatal("accepted an installed write with substituted calldata")
	}
	reader.state.Calldata = write.Calldata
	reader.state.Logs = append([]finalCanonicalEVMLog(nil), state.Logs...)
	reader.state.Logs[len(reader.state.Logs)-1].Address = evidence.Deployment.CoordinatorProxy
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err == nil {
		t.Fatal("accepted an installed write with a substituted event emitter")
	}
}

// A sealed predecessor batch may target a batcher that the terminal release
// census has superseded. Receipt filtering must use only that write's approved
// coordinator/batcher graph, not the terminal helper or an arbitrary address.
func TestFinalFleetGenerationWriteEmitterGraphUsesDistinctHistoricalBatcher(t *testing.T) {
	t.Parallel()
	evidence := finalFleetGenerationEventFixture(t)
	current, err := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
	if err != nil {
		t.Fatal(err)
	}
	historical := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	if strings.EqualFold(historical.Hex(), current.Address) {
		t.Fatal("historical batcher fixture aliases the terminal batcher")
	}
	write := FinalFleetGenerationWriteEvidence{
		Action:           FinalFleetGenerationActionEvidence{ActionID: "fleet.install.batch.3"},
		CoordinatorProxy: evidence.Deployment.CoordinatorProxy,
		BatcherAddress:   historical.Hex(), BatcherRuntimeHash: finalFleetGenerationTestHash(32_500),
	}
	allowed, err := finalFleetGenerationWriteReleaseContractAddresses(write)
	if err != nil {
		t.Fatalf("approve distinct historical batcher: %v", err)
	}
	if !allowed[common.HexToAddress(evidence.Deployment.CoordinatorProxy)] || !allowed[historical] {
		t.Fatalf("write-time emitter graph omits approved address: %#v", allowed)
	}
	if allowed[common.HexToAddress(current.Address)] {
		t.Fatalf("write-time emitter graph admitted terminal batcher %s", current.Address)
	}
	write.Action.ActionID = "fleet.mirror.21"
	write.BatcherAddress, write.BatcherRuntimeHash = "", ""
	allowed, err = finalFleetGenerationWriteReleaseContractAddresses(write)
	if err != nil {
		t.Fatalf("approve carried coordinator write: %v", err)
	}
	if len(allowed) != 1 || !allowed[common.HexToAddress(evidence.Deployment.CoordinatorProxy)] {
		t.Fatalf("carried write emitter graph=%#v, want only coordinator", allowed)
	}

	// Exercise the actual public-reader filter rather than only its graph
	// constructor. The sealed write names the predecessor batcher while the
	// terminal runtime census names current; the historical receipt must still
	// replay, and the same response must fail if the sealed write is changed to
	// target the terminal batcher.
	write.Action = FinalFleetGenerationActionEvidence{ActionID: "fleet.install.batch.3", PlanHash: finalFleetGenerationTestHash(32_501), IntentHash: finalFleetGenerationTestHash(32_502)}
	write.BatcherAddress, write.BatcherRuntimeHash = historical.Hex(), finalFleetGenerationTestHash(32_503)
	head := finalFleetGenerationTestHead(1_234)
	log := finalFleetGenerationTestABIEventLog(t, evidence, write.Action.ActionID, "FleetMemberBound", 1)
	log.Address, log.BlockNumber, log.BlockHash, log.TransactionHash, log.LogIndex = strings.ToLower(historical.Hex()), head.Number, head.Hash, finalFleetGenerationTestHash(32_504), 0
	logsHash, err := finalCanonicalReceiptLogsHash([]finalCanonicalEVMLog{log})
	if err != nil {
		t.Fatal(err)
	}
	write.Receipt = FinalEVMReceipt{TransactionHash: log.TransactionHash, Block: head, Status: "success", LogsHash: logsHash, Proof: finalFleetGenerationTestArtifact("evm-receipt", "historical-batcher-public")}
	write.Calldata = "0x01020304"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Errorf("decode public fleet write RPC: %v", err)
			return
		}
		result := any(nil)
		switch call.Method {
		case "eth_getTransactionReceipt":
			result = map[string]any{
				"transactionHash": log.TransactionHash, "blockHash": head.Hash, "blockNumber": "0x4d2", "status": "0x1",
				"logs": []map[string]any{{"address": log.Address, "topics": log.Topics, "data": log.Data, "blockNumber": "0x4d2", "blockHash": head.Hash, "transactionHash": log.TransactionHash, "transactionIndex": "0x0", "logIndex": "0x0", "removed": false}},
			}
		case "eth_getTransactionByHash":
			result = map[string]any{"hash": log.TransactionHash, "blockHash": head.Hash, "blockNumber": "0x4d2", "to": historical.Hex(), "input": write.Calldata}
		default:
			t.Errorf("unexpected public fleet write RPC method %q", call.Method)
		}
		response.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(response).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}); err != nil {
			t.Errorf("encode public fleet write RPC: %v", err)
		}
	}))
	defer server.Close()
	client, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reader := &PublicFinalSemanticChainReader{evidence: evidence, evm: client, evmRetry: immediateFinalSemanticRetryPolicy()}
	state, exchanges, err := reader.FleetGenerationEVMWrite(context.Background(), write)
	if err != nil {
		t.Fatalf("replay historical batcher receipt: %v", err)
	}
	if !strings.EqualFold(state.To, historical.Hex()) || len(state.Logs) != 1 || len(exchanges) != 2 {
		t.Fatalf("historical batcher replay state=%+v exchanges=%d", state, len(exchanges))
	}
	wrongTarget := write
	wrongTarget.BatcherAddress = current.Address
	if _, _, err := reader.FleetGenerationEVMWrite(context.Background(), wrongTarget); err == nil {
		t.Fatal("accepted historical batcher receipt for a current-batcher write")
	}
}

// accepts an authenticated predecessor implementation before the current
// batcher existed, while requiring every generation-two refresh to use the
// current proxy, implementation, and batcher roots.
func TestFinalFleetGenerationAcceptsHistoricalRuntimeAndRequiresCurrentRefreshRoots(t *testing.T) {
	t.Parallel()
	evidence := finalFleetGenerationEventFixture(t)
	historical := FinalFleetGenerationWriteEvidence{
		Action:                      FinalFleetGenerationActionEvidence{ActionID: "fleet.mirror.1"},
		CoordinatorProxy:            evidence.Deployment.CoordinatorProxy,
		CoordinatorImplementation:   "0x00000000000000000000000000000000000000aa",
		CoordinatorProxyRuntimeHash: finalFleetGenerationTestHash(90_001),
		CoordinatorRuntimeHash:      finalFleetGenerationTestHash(90_002),
		EVMHead:                     ChainHead{Number: 7_896_921, Hash: finalFleetGenerationTestHash(90_003)},
	}
	historical.CoordinatorImplementationSlot = finalFleetGenerationImplementationSlot(historical.CoordinatorImplementation)
	reader := &finalFleetGenerationWriteTestReader{runtime: finalFleetGenerationRuntimeStateForWrite(historical)}
	appendExchanges := func(string, ChainHead, []FinalRPCExchange) error { return nil }
	if err := verifyFinalFleetGenerationRuntime(context.Background(), evidence, reader, historical, appendExchanges); err != nil {
		t.Fatalf("accept predecessor runtime: %v", err)
	}
	reader.runtime.CoordinatorRuntimeHash = finalFleetGenerationTestHash(90_004)
	if err := verifyFinalFleetGenerationRuntime(context.Background(), evidence, reader, historical, appendExchanges); err == nil {
		t.Fatal("accepted a substituted predecessor implementation code hash")
	}
	historicalInstall := historical
	historicalInstall.Action = FinalFleetGenerationActionEvidence{ActionID: "fleet.install.batch.3", PlanHash: finalFleetGenerationTestHash(90_007)}
	historicalInstall.BatcherAddress = finalFleetGenerationTestBatcher
	historicalInstall.BatcherRuntimeHash = finalFleetGenerationTestHash(90_008)
	reader.runtime = finalFleetGenerationRuntimeStateForWrite(historicalInstall)
	reader.runtime.BatcherAddress, reader.runtime.BatcherRuntimeHash = historicalInstall.BatcherAddress, historicalInstall.BatcherRuntimeHash
	if err := verifyFinalFleetGenerationRuntime(context.Background(), evidence, reader, historicalInstall, appendExchanges); err != nil {
		t.Fatalf("accept predecessor install batcher runtime: %v", err)
	}
	reader.runtime.BatcherRuntimeHash = finalFleetGenerationTestHash(90_009)
	if err := verifyFinalFleetGenerationRuntime(context.Background(), evidence, reader, historicalInstall, appendExchanges); err == nil {
		t.Fatal("accepted a substituted predecessor install batcher runtime")
	}
	proxy, err := finalReleaseRuntimeRootByName(evidence, "coordinator_proxy")
	if err != nil {
		t.Fatal(err)
	}
	implementation, err := finalReleaseRuntimeRootByName(evidence, "coordinator_upgrade_implementation")
	if err != nil {
		t.Fatal(err)
	}
	batcher, err := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
	if err != nil {
		t.Fatal(err)
	}
	refresh := FinalFleetGenerationWriteEvidence{
		Action: FinalFleetGenerationActionEvidence{ActionID: "fleet.refresh.batch.1", PlanHash: evidence.PlanHash}, CoordinatorProxy: proxy.Address,
		CoordinatorImplementation: implementation.Address, CoordinatorImplementationSlot: finalFleetGenerationImplementationSlot(implementation.Address),
		CoordinatorProxyRuntimeHash: proxy.RuntimeCodeHash, CoordinatorRuntimeHash: implementation.RuntimeCodeHash, BatcherAddress: batcher.Address, BatcherRuntimeHash: batcher.RuntimeCodeHash,
		EVMHead: ChainHead{Number: 7_900_646, Hash: finalFleetGenerationTestHash(90_005)},
	}
	reader.runtime = finalFleetGenerationRuntimeStateForWrite(refresh)
	reader.runtime.BatcherAddress, reader.runtime.BatcherRuntimeHash = batcher.Address, batcher.RuntimeCodeHash
	if err := verifyFinalFleetGenerationRuntime(context.Background(), evidence, reader, refresh, appendExchanges); err != nil {
		t.Fatalf("accept current refresh runtime: %v", err)
	}
	reader.runtime.BatcherRuntimeHash = finalFleetGenerationTestHash(90_006)
	if err := verifyFinalFleetGenerationRuntime(context.Background(), evidence, reader, refresh, appendExchanges); err == nil {
		t.Fatal("accepted a refresh with another batcher runtime")
	}
}

// Prevents a plan action for an atomic refresh from naming an address other
// than the reviewed batcher, while leaving predecessor logical targets to the
// separately authenticated historical-receipt path.
func TestFinalFleetGenerationRejectsCurrentBatchTargetSubstitution(t *testing.T) {
	batcher := common.HexToAddress(finalFleetGenerationTestBatcher)
	action := Action{Kind: "evm-transaction", Target: batcher.Hex()}
	if err := verifyFinalFleetGenerationCurrentBatchTarget(action, batcher); err != nil {
		t.Fatalf("accept exact current batcher target: %v", err)
	}
	action.Target = finalFleetGenerationTestCoordinator
	if err := verifyFinalFleetGenerationCurrentBatchTarget(action, batcher); err == nil {
		t.Fatal("accepted a substituted current batch action target")
	}
}

// builds a one-fleet install group with the complete Solidity receipt order.
// The helper intentionally uses one deterministic identity repeatedly because
// this focused topology test does not exercise the separate four-member
// uniqueness verifier.
func finalFleetGenerationInstalledTopologyFixture(t *testing.T, evidence *FinalSemanticEvidence) (*FinalFleetGenerationLineageEvidence, FinalFleetGenerationBatchEvidence, FinalFleetGenerationWriteEvidence) {
	t.Helper()
	batcher, err := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"CommitmentMirrored", "FleetBound", "FleetMemberBound", "FleetBound", "FleetMemberBound", "FleetBound", "FleetMemberBound", "FleetBound", "FleetMemberBound", "FleetInstalled"}
	events := make([]FinalFleetGenerationEventEvidence, 0, len(names))
	for index, name := range names {
		log := finalFleetGenerationTestABIEventLog(t, evidence, "fleet.install.batch.1", name, 1)
		log.LogIndex = uint64(index)
		decoded, err := finalFleetGenerationDecodeEventForBatcher(evidence, "fleet.install.batch.1", batcher.Address, log)
		if err != nil {
			t.Fatalf("decode installed %s: %v", name, err)
		}
		events = append(events, decoded.Evidence)
	}
	mirror := events[0]
	bound := events[1]
	initial := FinalFleetGenerationVersionEvidence{
		Generation: 1, Hotkey: mirror.Hotkey, CommitmentHash: mirror.CommitmentHash,
		NativeHead: ChainHead{Number: mirror.FinalizedBlock, Hash: mirror.FinalizedBlockHash},
	}
	for member := uint64(1); member <= 4; member++ {
		initial.Members = append(initial.Members, FinalFleetGenerationMemberEvidence{
			Member: member, ClientID: bound.ClientID, FleetKey: bound.FleetID, Hotkey: bound.Hotkey,
			CommitmentHash: mirror.CommitmentHash, Generation: bound.Generation, ValidFromEpoch: bound.ValidFromEpoch,
			ValidToEpoch: bound.ValidToEpoch, UID: bound.UID,
		})
	}
	logs := make([]finalCanonicalEVMLog, 0, len(events))
	for _, event := range events {
		logs = append(logs, event.Log)
	}
	logsHash, err := finalCanonicalReceiptLogsHash(logs)
	if err != nil {
		t.Fatal(err)
	}
	eventHash, err := canonicalHashHex(events)
	if err != nil {
		t.Fatal(err)
	}
	calldata := "0x01020304"
	calldataBytes, err := decodeEvidenceHex(calldata)
	if err != nil {
		t.Fatal(err)
	}
	head := ChainHead{Number: events[0].Log.BlockNumber, Hash: events[0].Log.BlockHash}
	write := FinalFleetGenerationWriteEvidence{
		Action:   FinalFleetGenerationActionEvidence{ActionID: "fleet.install.batch.1", PlanHash: finalFleetGenerationTestHash(32_001), IntentHash: finalFleetGenerationTestHash(32_002)},
		Receipt:  FinalEVMReceipt{TransactionHash: events[0].Log.TransactionHash, Block: head, Status: "success", LogsHash: logsHash, Proof: finalFleetGenerationTestArtifact("evm-receipt", "installed-topology")},
		Calldata: calldata, CalldataHash: crypto.Keccak256Hash(calldataBytes).Hex(), EventHash: eventHash, Events: events,
		CoordinatorProxy: evidence.Deployment.CoordinatorProxy, CoordinatorImplementation: evidence.Deployment.CoordinatorImplementation,
		CoordinatorImplementationSlot: evidence.Deployment.ObservedImplementationSlot, CoordinatorProxyRuntimeHash: evidence.Deployment.CoordinatorProxyCodeHash,
		CoordinatorRuntimeHash: evidence.Deployment.ImplementationCodeHash, BatcherAddress: batcher.Address, BatcherRuntimeHash: batcher.RuntimeCodeHash,
		Postcondition: finalFleetGenerationTestArtifact("fleet-generation-postcondition", "installed-topology"), EVMHead: head, NativeHead: head,
	}
	lineage := &FinalFleetGenerationLineageEvidence{SetupFleets: []FinalFleetGenerationFleetEvidence{{FleetID: 1, Initial: initial}}}
	batch := FinalFleetGenerationBatchEvidence{Batch: 1, Generation: 1, FirstFleet: 1, LastFleet: 10, InstalledFleets: []uint64{1}, BatchWrite: &write}
	return lineage, batch, write
}

// mirrors a successful public runtime response from a sealed write without
// reading terminal deployment fields into the predecessor branch.
func finalFleetGenerationRuntimeStateForWrite(write FinalFleetGenerationWriteEvidence) FinalFleetGenerationRuntimeState {
	return FinalFleetGenerationRuntimeState{
		CoordinatorProxy: write.CoordinatorProxy, CoordinatorImplementation: write.CoordinatorImplementation,
		CoordinatorImplementationSlot: write.CoordinatorImplementationSlot, CoordinatorProxyRuntimeHash: write.CoordinatorProxyRuntimeHash,
		CoordinatorRuntimeHash: write.CoordinatorRuntimeHash, Block: write.EVMHead,
	}
}

// Returns a complete anchor-bearing fixture because event decoding must use
// the same coordinator and batcher addresses as the release runtime census.
func finalFleetGenerationEventFixture(t *testing.T) *FinalSemanticEvidence {
	t.Helper()
	evidence, _ := finalSemanticFixture(t)
	if _, err := finalReleaseRuntimeRootByName(&evidence, "fleet_batcher"); err != nil {
		t.Fatalf("fixture lacks fleet batcher root: %v", err)
	}
	return &evidence
}

// builds a canonical raw EVM log through the generated ABI rather than hand
// writing topics. This makes substitutions exercise the real decoder shape.
func finalFleetGenerationTestABIEventLog(t *testing.T, evidence *FinalSemanticEvidence, actionID, name string, nonce uint64) finalCanonicalEVMLog {
	t.Helper()
	coordinator, batcher, err := finalFleetGenerationABIs()
	if err != nil {
		t.Fatal(err)
	}
	contract := coordinator
	address := strings.ToLower(evidence.Deployment.CoordinatorProxy)
	if name == "FleetMemberBound" || name == "FleetInstalled" || name == "FleetRefreshed" {
		root, rootErr := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
		if rootErr != nil {
			t.Fatal(rootErr)
		}
		contract, address = batcher, strings.ToLower(root.Address)
	}
	event, found := contract.Events[name]
	if !found {
		t.Fatalf("ABI lacks %s", name)
	}
	hotkey := finalFleetGenerationTestBytes32(10 + nonce)
	commitment := finalFleetGenerationTestBytes32(20 + nonce)
	fleet := finalFleetGenerationTestBytes32(30 + nonce)
	client := finalFleetGenerationTestBytes16(40 + nonce)
	indexed := make([]common.Hash, 0, 3)
	dataValues := make([]any, 0, len(event.Inputs))
	for _, input := range event.Inputs {
		if input.Indexed {
			switch input.Name {
			case "hotkey":
				indexed = append(indexed, common.BytesToHash(hotkey[:]))
			case "commitmentHash":
				indexed = append(indexed, common.BytesToHash(commitment[:]))
			case "fleetId":
				indexed = append(indexed, common.BytesToHash(fleet[:]))
			case "clientId":
				var topic [32]byte
				copy(topic[:], client[:])
				indexed = append(indexed, common.BytesToHash(topic[:]))
			default:
				t.Fatalf("unsupported indexed %s.%s", name, input.Name)
			}
			continue
		}
		switch input.Name {
		case "finalizedBlock":
			dataValues = append(dataValues, uint64(100+nonce))
		case "finalizedBlockHash":
			dataValues = append(dataValues, finalFleetGenerationTestBytes32(50+nonce))
		case "uid":
			dataValues = append(dataValues, uint16(7))
		case "generation":
			if name == "FleetBindingRevoked" || strings.HasPrefix(actionID, "fleet.install.batch.") {
				dataValues = append(dataValues, uint64(1))
			} else {
				dataValues = append(dataValues, uint64(2))
			}
		case "validFromEpoch", "effectiveEpoch":
			dataValues = append(dataValues, uint64(0))
		case "validToEpoch":
			dataValues = append(dataValues, uint64(90))
		case "members":
			dataValues = append(dataValues, big.NewInt(4))
		default:
			t.Fatalf("unsupported non-indexed %s.%s", name, input.Name)
		}
	}
	data, err := event.Inputs.NonIndexed().Pack(dataValues...)
	if err != nil {
		t.Fatal(err)
	}
	head := ChainHead{Number: 10_000 + nonce, Hash: finalFleetGenerationTestHash(60 + nonce)}
	topics := make([]string, 1, len(indexed)+1)
	topics[0] = strings.ToLower(event.ID.Hex())
	for _, topic := range indexed {
		topics = append(topics, strings.ToLower(topic.Hex()))
	}
	log := finalCanonicalEVMLog{Address: address, Topics: topics, Data: "0x" + common.Bytes2Hex(data), BlockNumber: head.Number, BlockHash: head.Hash, TransactionHash: finalFleetGenerationTestHash(70 + nonce), TransactionIndex: 0, LogIndex: uint64(nonce)}
	if _, err := finalFleetGenerationDecodeEvent(evidence, actionID, log); err != nil {
		t.Fatalf("build %s: %v", name, err)
	}
	return log
}

// creates a stable nonzero ABI bytes32 value without a dependence on map or
// cryptographic randomness in a test fixture.
func finalFleetGenerationTestBytes32(value uint64) [32]byte {
	var result [32]byte
	for index := range result[:24] {
		result[index] = byte(value + uint64(index))
	}
	result[31] = byte(value)
	return result
}

// creates a stable nonzero ABI bytes16 client identity for indexed topics.
func finalFleetGenerationTestBytes16(value uint64) [16]byte {
	var result [16]byte
	result[15] = byte(value)
	return result
}
