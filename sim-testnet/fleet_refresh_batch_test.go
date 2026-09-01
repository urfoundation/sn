package main

// These tests reproduce the public-RPC timeout caused by issuing one HTTP
// request per refresh member. They enforce bounded request partitions and all
// semantic checks at the exact block selected by the caller.

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Records exact HTTP batch geometry without relying on timing or scheduler
// behavior.
type fleetRefreshRPCRecorder struct {
	requestCount atomic.Int64
	stateLock    sync.Mutex
	batchSizes   []int
	blocks       []string
}

// Names the first record of each kind so negative tests can corrupt one field
// while leaving the remainder of a production-sized response exact.
type fleetRefreshTestTargets struct {
	Mirror      string
	Count       string
	Prior       string
	Successor   string
	MemberCount string
	PriorRecord stabi.STCoordinatorBindingRecord
	NextRecord  stabi.STCoordinatorBindingRecord
}

// Returns an immutable observation after the client call completes; explicit
// locking keeps the race detector independent of net/http scheduling details.
func (self *fleetRefreshRPCRecorder) snapshot() (int64, []int, []string) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.requestCount.Load(), append([]int(nil), self.batchSizes...), append([]string(nil), self.blocks...)
}

// Produces a canonical ABI return value for the in-process JSON-RPC fixture.
func fleetRefreshTestOutput(t *testing.T, parsed abi.ABI, method string, values ...any) string {
	t.Helper()
	output, err := parsed.Methods[method].Outputs.Pack(values...)
	if err != nil {
		t.Fatalf("pack %s output: %v", method, err)
	}
	return "0x" + hex.EncodeToString(output)
}

// Serves only explicitly named calldata. An unknown selector is a JSON-RPC
// element failure, allowing tests to prove transport errors are not relabeled
// as semantic state mismatches.
func newFleetRefreshRPCServer(t *testing.T, outputs map[string]string) (*httptest.Server, *fleetRefreshRPCRecorder) {
	t.Helper()
	type request struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      json.RawMessage   `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	type responseError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  string          `json:"result,omitempty"`
		Error   *responseError  `json:"error,omitempty"`
	}
	recorder := &fleetRefreshRPCRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		recorder.requestCount.Add(1)
		var batch []request
		if err := json.NewDecoder(httpRequest.Body).Decode(&batch); err != nil {
			t.Errorf("decode refresh RPC batch: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(batch) == 0 || len(batch) > maximumEVMRPCBatchCalls {
			t.Errorf("refresh RPC batch size=%d", len(batch))
		}
		responses := make([]response, len(batch))
		for index, call := range batch {
			responses[index] = response{JSONRPC: "2.0", ID: call.ID}
			if call.Method != "eth_call" || len(call.Params) != 2 {
				responses[index].Error = &responseError{Code: -32602, Message: "malformed test call"}
				continue
			}
			var envelope struct {
				Data string `json:"data"`
			}
			if err := json.Unmarshal(call.Params[0], &envelope); err != nil {
				responses[index].Error = &responseError{Code: -32602, Message: "malformed test calldata"}
				continue
			}
			var block string
			if err := json.Unmarshal(call.Params[1], &block); err != nil {
				responses[index].Error = &responseError{Code: -32602, Message: "malformed test block"}
				continue
			}
			recorder.stateLock.Lock()
			recorder.blocks = append(recorder.blocks, block)
			recorder.stateLock.Unlock()
			result, ok := outputs[strings.ToLower(envelope.Data)]
			if !ok {
				responses[index].Error = &responseError{Code: -32601, Message: "unmapped test calldata"}
				continue
			}
			responses[index].Result = result
		}
		recorder.stateLock.Lock()
		recorder.batchSizes = append(recorder.batchSizes, len(batch))
		recorder.stateLock.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(responses); err != nil {
			t.Errorf("encode refresh RPC batch: %v", err)
		}
	}))
	return server, recorder
}

// Dials one fixture server with the same client stack used by production
// coordinator reads.
func fleetRefreshTestManager(t *testing.T, server *httptest.Server) *EVMTxManager {
	t.Helper()
	rpcClient, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rpcClient.Close)
	return &EVMTxManager{client: ethclient.NewClient(rpcClient)}
}

// Builds ten four-member fleets, matching one production refresh action, and
// maps every unique calldata value to its exact ABI response.
func fleetRefreshStateFixture(t *testing.T) ([]fleetRefreshVerificationFleet, map[string]string, fleetRefreshTestTargets) {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")
	var protocolCoordinator [20]byte
	copy(protocolCoordinator[:], coordinatorAddress[:])
	outputs := map[string]string{}
	fleets := make([]fleetRefreshVerificationFleet, 0, fleetRefreshBatchSize)
	var targets fleetRefreshTestTargets
	bytes32 := func(value uint64) [32]byte {
		var result [32]byte
		binary.BigEndian.PutUint64(result[24:], value)
		return result
	}
	record := func(binding protocol.FleetBinding, validTo uint64, uid uint16) stabi.STCoordinatorBindingRecord {
		return stabi.STCoordinatorBindingRecord{
			FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientKey: binding.ClientKey,
			CommitmentHash: binding.CommitmentHash, Generation: binding.Generation,
			ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: validTo, Uid: uid,
		}
	}
	for fleetIndex := 1; fleetIndex <= fleetRefreshBatchSize; fleetIndex++ {
		fleetID := bytes32(uint64(1_000 + fleetIndex))
		hotkey := bytes32(uint64(2_000 + fleetIndex))
		commitmentHash := bytes32(uint64(3_000 + fleetIndex))
		finalizedBlockHash := bytes32(uint64(4_000 + fleetIndex))
		finalizedBlock := uint64(5_000 + fleetIndex)
		verificationFleet := fleetRefreshVerificationFleet{
			Fleet: fleetIndex, Hotkey: hotkey, CommitmentHash: commitmentHash,
			FinalizedBlock: finalizedBlock, FinalizedBlockHash: finalizedBlockHash,
			FleetID: fleetID, Members: make([]fleetRefreshVerificationMember, 0, 4),
		}
		mirrorCall := "0x" + hex.EncodeToString(coordinator.PackMirroredCommitments(hotkey))
		outputs[mirrorCall] = fleetRefreshTestOutput(t, parsed, "mirroredCommitments", commitmentHash, finalizedBlockHash, finalizedBlock)
		for memberIndex := 1; memberIndex <= 4; memberIndex++ {
			identity := uint64(fleetIndex*100 + memberIndex)
			var clientID [16]byte
			binary.BigEndian.PutUint64(clientID[8:], identity)
			binding := protocol.FleetBinding{
				ChainID: testnetChainID, Netuid: 521, Coordinator: protocolCoordinator,
				FleetID: fleetID, Hotkey: hotkey, ClientID: clientID, ClientKey: bytes32(6_000 + identity),
				Generation: 2, ValidFromEpoch: 10, ValidToEpoch: 41, CommitmentHash: commitmentHash,
			}
			priorBinding := binding
			priorBinding.Generation = 1
			priorBinding.ValidFromEpoch = 1
			priorBinding.ValidToEpoch = 32
			priorBinding.CommitmentHash = bytes32(7_000 + identity)
			uid := uint16(identity)
			priorRecord := record(priorBinding, binding.ValidFromEpoch-1, uid)
			nextRecord := record(binding, binding.ValidToEpoch, uid)
			countCall := "0x" + hex.EncodeToString(coordinator.PackBindingVersionCount(clientID))
			priorCall := "0x" + hex.EncodeToString(coordinator.PackBindingVersionAt(clientID, new(big.Int)))
			nextCall := "0x" + hex.EncodeToString(coordinator.PackBindingVersionAt(clientID, big.NewInt(1)))
			outputs[countCall] = fleetRefreshTestOutput(t, parsed, "bindingVersionCount", big.NewInt(2))
			outputs[priorCall] = fleetRefreshTestOutput(t, parsed, "bindingVersionAt", priorRecord)
			outputs[nextCall] = fleetRefreshTestOutput(t, parsed, "bindingVersionAt", nextRecord)
			verificationFleet.Members = append(verificationFleet.Members, fleetRefreshVerificationMember{
				Evidence: FleetRefreshBindingEvidence{Fleet: fleetIndex, Member: memberIndex, UID: uid},
				Binding:  binding, PriorBinding: priorBinding,
			})
			if fleetIndex == 1 && memberIndex == 1 {
				targets.Mirror = mirrorCall
				targets.Count = countCall
				targets.Prior = priorCall
				targets.Successor = nextCall
				targets.PriorRecord = priorRecord
				targets.NextRecord = nextRecord
			}
		}
		memberCountCall := "0x" + hex.EncodeToString(coordinator.PackFleetMemberCount(fleetID))
		outputs[memberCountCall] = fleetRefreshTestOutput(t, parsed, "fleetMemberCount", big.NewInt(4))
		if fleetIndex == 1 {
			targets.MemberCount = memberCountCall
		}
		fleets = append(fleets, verificationFleet)
	}
	return fleets, outputs, targets
}

// Reproduces the live 2,202/2,202 timeout without sleeps: a production action
// must fit in exactly three provider requests or the assertion fails.
func TestFleetRefreshStateAuditUsesThreeBoundedRequests(t *testing.T) {
	fleets, outputs, _ := fleetRefreshStateFixture(t)
	server, recorder := newFleetRefreshRPCServer(t, outputs)
	defer server.Close()
	manager := fleetRefreshTestManager(t, server)
	members, err := verifyFleetRefreshStateAt(context.Background(), manager, common.HexToAddress("0x1234567890123456789012345678901234567890"), stabi.NewSTCoordinator(), fleets, 123)
	if err != nil {
		t.Fatal(err)
	}
	requestCount, batchSizes, blocks := recorder.snapshot()
	if members != 40 || requestCount != 3 || !slices.Equal(batchSizes, []int{50, 50, 40}) {
		t.Fatalf("members/requests/batches=%d/%d/%v, want 40/3/[50 50 40]", members, requestCount, batchSizes)
	}
	for _, block := range blocks {
		if block != "0x7b" {
			t.Fatalf("state batch used block %q, want 0x7b", block)
		}
	}
}

// Every neighboring on-chain field remains independently fail-closed after
// transport batching; batching is never permission to accept partial state.
func TestFleetRefreshStateAuditRejectsAdjacentMutations(t *testing.T) {
	type mutation struct {
		Name     string
		Fragment string
		Apply    func(*testing.T, abi.ABI, map[string]string, fleetRefreshTestTargets)
	}
	mutations := []mutation{
		{Name: "mirror", Fragment: "refreshed mirror mismatch", Apply: func(t *testing.T, parsed abi.ABI, outputs map[string]string, targets fleetRefreshTestTargets) {
			outputs[targets.Mirror] = fleetRefreshTestOutput(t, parsed, "mirroredCommitments", [32]byte{1}, [32]byte{2}, uint64(3))
		}},
		{Name: "count", Fragment: "binding version count", Apply: func(t *testing.T, parsed abi.ABI, outputs map[string]string, targets fleetRefreshTestTargets) {
			outputs[targets.Count] = fleetRefreshTestOutput(t, parsed, "bindingVersionCount", big.NewInt(1))
		}},
		{Name: "predecessor", Fragment: "revoked generation mismatch", Apply: func(t *testing.T, parsed abi.ABI, outputs map[string]string, targets fleetRefreshTestTargets) {
			record := targets.PriorRecord
			record.ValidToEpoch++
			outputs[targets.Prior] = fleetRefreshTestOutput(t, parsed, "bindingVersionAt", record)
		}},
		{Name: "successor", Fragment: "replacement generation mismatch", Apply: func(t *testing.T, parsed abi.ABI, outputs map[string]string, targets fleetRefreshTestTargets) {
			record := targets.NextRecord
			record.Generation++
			outputs[targets.Successor] = fleetRefreshTestOutput(t, parsed, "bindingVersionAt", record)
		}},
		{Name: "member count", Fragment: "member count", Apply: func(t *testing.T, parsed abi.ABI, outputs map[string]string, targets fleetRefreshTestTargets) {
			outputs[targets.MemberCount] = fleetRefreshTestOutput(t, parsed, "fleetMemberCount", big.NewInt(3))
		}},
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range mutations {
		fleets, outputs, targets := fleetRefreshStateFixture(t)
		mutation.Apply(t, parsed, outputs, targets)
		server, _ := newFleetRefreshRPCServer(t, outputs)
		manager := fleetRefreshTestManager(t, server)
		_, verifyErr := verifyFleetRefreshStateAt(context.Background(), manager, common.HexToAddress("0x1234567890123456789012345678901234567890"), stabi.NewSTCoordinator(), fleets, 123)
		server.Close()
		if verifyErr == nil || !strings.Contains(verifyErr.Error(), mutation.Fragment) {
			t.Errorf("%s mutation error=%v, want %q", mutation.Name, verifyErr, mutation.Fragment)
		}
	}
}

// A provider or context failure is operational evidence, not proof that a
// successor record is dishonest. Preserve that distinction in diagnostics.
func TestFleetRefreshStateAuditDoesNotRelabelRPCFailure(t *testing.T) {
	fleets, outputs, targets := fleetRefreshStateFixture(t)
	delete(outputs, targets.Successor)
	server, _ := newFleetRefreshRPCServer(t, outputs)
	defer server.Close()
	manager := fleetRefreshTestManager(t, server)
	_, err := verifyFleetRefreshStateAt(context.Background(), manager, common.HexToAddress("0x1234567890123456789012345678901234567890"), stabi.NewSTCoordinator(), fleets, 123)
	if err == nil || !strings.Contains(err.Error(), "fleet refresh state batch") || !strings.Contains(err.Error(), "unmapped test calldata") || strings.Contains(err.Error(), "replacement generation mismatch") {
		t.Fatalf("RPC failure classification=%v", err)
	}
}

// Fresh generation-2 signing uses the same bounded transport: forty members
// require eighty calls but only two HTTP requests.
func TestFleetRefreshPreparationBatchesAllPriorBindings(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	outputs := map[string]string{}
	clientIDs := make([][16]byte, 40)
	for index := range clientIDs {
		binary.BigEndian.PutUint64(clientIDs[index][8:], uint64(index+1))
		record := stabi.STCoordinatorBindingRecord{Generation: 1, Uid: uint16(index + 1)}
		countCall := "0x" + hex.EncodeToString(coordinator.PackBindingVersionCount(clientIDs[index]))
		recordCall := "0x" + hex.EncodeToString(coordinator.PackBindingVersionAt(clientIDs[index], new(big.Int)))
		outputs[countCall] = fleetRefreshTestOutput(t, parsed, "bindingVersionCount", big.NewInt(1))
		outputs[recordCall] = fleetRefreshTestOutput(t, parsed, "bindingVersionAt", record)
	}
	server, recorder := newFleetRefreshRPCServer(t, outputs)
	defer server.Close()
	manager := fleetRefreshTestManager(t, server)
	reads, err := readFleetBindingVersionsAt(context.Background(), manager, common.HexToAddress("0x1234567890123456789012345678901234567890"), coordinator, clientIDs, 0, 123)
	if err != nil {
		t.Fatal(err)
	}
	requestCount, batchSizes, _ := recorder.snapshot()
	if len(reads) != 40 || requestCount != 2 || !slices.Equal(batchSizes, []int{50, 30}) {
		t.Fatalf("reads/requests/batches=%d/%d/%v, want 40/2/[50 30]", len(reads), requestCount, batchSizes)
	}
	for index, read := range reads {
		if read.Count == nil || read.Count.Uint64() != 1 || read.Record.Uid != uint16(index+1) {
			t.Fatalf("prior read %d=%+v", index, read)
		}
	}
}

// The five oracle routing fields must come from one HTTP request and one exact
// block, preventing both rate amplification and mixed-epoch observations.
func TestFleetRefreshOracleStateUsesOnePinnedBatch(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	immutable := common.HexToAddress("0x1111111111111111111111111111111111111111")
	active := common.HexToAddress("0x2222222222222222222222222222222222222222")
	pending := common.HexToAddress("0x3333333333333333333333333333333333333333")
	outputs := map[string]string{
		"0x" + hex.EncodeToString(coordinator.PackCurrentEpoch()):                 fleetRefreshTestOutput(t, parsed, "currentEpoch", big.NewInt(9)),
		"0x" + hex.EncodeToString(coordinator.PackCommitmentOracle()):             fleetRefreshTestOutput(t, parsed, "commitmentOracle", immutable),
		"0x" + hex.EncodeToString(coordinator.PackActiveCommitmentOracle()):       fleetRefreshTestOutput(t, parsed, "activeCommitmentOracle", active),
		"0x" + hex.EncodeToString(coordinator.PackPendingCommitmentOracle()):      fleetRefreshTestOutput(t, parsed, "pendingCommitmentOracle", pending),
		"0x" + hex.EncodeToString(coordinator.PackPendingCommitmentOracleEpoch()): fleetRefreshTestOutput(t, parsed, "pendingCommitmentOracleEpoch", uint64(10)),
	}
	server, recorder := newFleetRefreshRPCServer(t, outputs)
	defer server.Close()
	manager := fleetRefreshTestManager(t, server)
	state, err := readFleetRefreshOracleStateAt(context.Background(), manager, common.HexToAddress("0x1234567890123456789012345678901234567890"), coordinator, 123)
	if err != nil {
		t.Fatal(err)
	}
	requestCount, batchSizes, _ := recorder.snapshot()
	if requestCount != 1 || !slices.Equal(batchSizes, []int{5}) || state.CurrentEpoch != 9 || state.Immutable != immutable || state.Active != active || state.Pending != pending || state.PendingEpoch != 10 {
		t.Fatalf("oracle state/requests/batches=%+v/%d/%v", state, requestCount, batchSizes)
	}
}

// Invalid local coordinates must fail before a provider request is made.
func TestFleetRefreshStateAuditRejectsIncompleteIdentityBeforeRPC(t *testing.T) {
	fleets, outputs, _ := fleetRefreshStateFixture(t)
	fleets[0].Members[0].Binding.ValidFromEpoch = 0
	server, recorder := newFleetRefreshRPCServer(t, outputs)
	defer server.Close()
	manager := fleetRefreshTestManager(t, server)
	if _, err := verifyFleetRefreshStateAt(context.Background(), manager, common.HexToAddress("0x1234567890123456789012345678901234567890"), stabi.NewSTCoordinator(), fleets, 123); err == nil || !strings.Contains(err.Error(), "identity is incomplete") {
		t.Fatalf("incomplete identity error=%v", err)
	}
	requestCount, _, _ := recorder.snapshot()
	if requestCount != 0 {
		t.Fatalf("incomplete identity made %d RPC requests", requestCount)
	}
}

// A valid signature for a foreign member, hotkey or commitment must not be
// accepted as evidence for the deterministic fleet named by the batch.
func TestFleetRefreshReplacementRequiresExactManifestMember(t *testing.T) {
	coordinatorAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")
	var protocolCoordinator [20]byte
	copy(protocolCoordinator[:], coordinatorAddress[:])
	manifest := protocol.FleetManifest{
		Schema: protocol.FleetManifestSchema, ChainID: testnetChainID, Netuid: 521, Coordinator: protocolCoordinator,
		FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 2,
		Members: []protocol.FleetMember{{ClientID: [16]byte{3}, ClientKey: [32]byte{4}}},
	}
	commitmentHash := [32]byte{5}
	binding := protocol.FleetBinding{
		ChainID: manifest.ChainID, Netuid: manifest.Netuid, Coordinator: manifest.Coordinator,
		FleetID: manifest.FleetID, Hotkey: manifest.Hotkey, ClientID: manifest.Members[0].ClientID, ClientKey: manifest.Members[0].ClientKey,
		Generation: 2, ValidFromEpoch: 10, ValidToEpoch: 41, CommitmentHash: commitmentHash,
	}
	if !fleetRefreshReplacementMatchesManifest(binding, manifest, 1, commitmentHash, 10, 41) {
		t.Fatal("exact replacement was rejected")
	}
	mutations := []func(*protocol.FleetBinding){
		func(value *protocol.FleetBinding) { value.ChainID++ },
		func(value *protocol.FleetBinding) { value.Netuid++ },
		func(value *protocol.FleetBinding) { value.Coordinator[0]++ },
		func(value *protocol.FleetBinding) { value.FleetID[0]++ },
		func(value *protocol.FleetBinding) { value.Hotkey[0]++ },
		func(value *protocol.FleetBinding) { value.ClientID[0]++ },
		func(value *protocol.FleetBinding) { value.ClientKey[0]++ },
		func(value *protocol.FleetBinding) { value.Generation++ },
		func(value *protocol.FleetBinding) { value.ValidFromEpoch++ },
		func(value *protocol.FleetBinding) { value.ValidToEpoch++ },
		func(value *protocol.FleetBinding) { value.CommitmentHash[0]++ },
	}
	for index, mutate := range mutations {
		candidate := binding
		mutate(&candidate)
		if fleetRefreshReplacementMatchesManifest(candidate, manifest, 1, commitmentHash, 10, 41) {
			t.Errorf("replacement mutation %d was accepted", index)
		}
	}
	if fleetRefreshReplacementMatchesManifest(binding, manifest, 0, commitmentHash, 10, 41) || fleetRefreshReplacementMatchesManifest(binding, manifest, 2, commitmentHash, 10, 41) {
		t.Fatal("out-of-range manifest member was accepted")
	}
	wrongGeneration := manifest
	wrongGeneration.Generation = 1
	if fleetRefreshReplacementMatchesManifest(binding, wrongGeneration, 1, commitmentHash, 10, 41) {
		t.Fatal("generation-1 manifest was accepted as a replacement")
	}
}
