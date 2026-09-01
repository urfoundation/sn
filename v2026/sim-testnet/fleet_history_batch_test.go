package main

// These tests force large historical fleet audits through bounded JSON-RPC
// batches and retain every per-action block/hash boundary without a live node.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/stabi"
)

type fleetHistoryBatchRPC struct {
	t                     *testing.T
	finalizedBlock        uint64
	tamperedBlock         uint64
	contractResult        string
	httpRequests          int
	blockBatchRequests    int
	contractBatchRequests int
	blockSelectors        []uint64
	contractSelectors     []uint64
}

type fleetHistoryBatchRPCRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

// Use a deterministic valid hash which exposes the selected block in its low
// bytes and therefore makes selector/hash swaps observable in assertions.
func fleetHistoryBatchBlockHash(block uint64) string {
	return fmt.Sprintf("0x%064x", block)
}

// Return either one finalized head or a bounded homogeneous batch. Production
// emits separate header and eth_call batches, so mixing methods is rejected.
func (self *fleetHistoryBatchRPC) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.t.Helper()
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		self.t.Errorf("read RPC body: %v", err)
		return
	}
	self.httpRequests++
	writer.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		var call fleetHistoryBatchRPCRequest
		if json.Unmarshal(body, &call) != nil || call.Method != "eth_getBlockByNumber" || len(call.Params) != 2 || string(call.Params[0]) != `"finalized"` {
			self.t.Errorf("unexpected single RPC request: %s", body)
			return
		}
		response := map[string]any{
			"jsonrpc": "2.0", "id": call.ID,
			"result": map[string]any{"number": hexutil.EncodeUint64(self.finalizedBlock), "hash": fleetHistoryBatchBlockHash(self.finalizedBlock)},
		}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			self.t.Errorf("encode finalized response: %v", err)
		}
		return
	}
	var calls []fleetHistoryBatchRPCRequest
	if err := json.Unmarshal(body, &calls); err != nil || len(calls) == 0 || len(calls) > maximumEVMRPCBatchCalls {
		self.t.Errorf("invalid RPC batch size=%d err=%v", len(calls), err)
		return
	}
	method := calls[0].Method
	for _, call := range calls {
		if call.Method != method {
			self.t.Errorf("mixed RPC batch methods %q and %q", method, call.Method)
			return
		}
	}
	responses := make([]map[string]any, len(calls))
	switch method {
	case "eth_getBlockByNumber":
		self.blockBatchRequests++
		for index, call := range calls {
			var selector string
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
				self.t.Errorf("invalid block request %d", index)
				return
			}
			block, decodeErr := hexutil.DecodeUint64(selector)
			if decodeErr != nil {
				self.t.Errorf("decode block selector %q: %v", selector, decodeErr)
				return
			}
			self.blockSelectors = append(self.blockSelectors, block)
			hash := fleetHistoryBatchBlockHash(block)
			if block == self.tamperedBlock {
				hash = "0x" + strings.Repeat("ff", 32)
			}
			responses[index] = map[string]any{
				"jsonrpc": "2.0", "id": call.ID,
				"result": map[string]any{"number": selector, "hash": hash},
			}
		}
	case "eth_call":
		self.contractBatchRequests++
		for index, call := range calls {
			var selector string
			if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil {
				self.t.Errorf("invalid contract request %d", index)
				return
			}
			block, decodeErr := hexutil.DecodeUint64(selector)
			if decodeErr != nil {
				self.t.Errorf("decode contract selector %q: %v", selector, decodeErr)
				return
			}
			self.contractSelectors = append(self.contractSelectors, block)
			result := self.contractResult
			if result == "" {
				result = "0x01"
			}
			responses[index] = map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}
		}
	default:
		self.t.Errorf("unexpected RPC batch method %q", method)
		return
	}
	if err := json.NewEncoder(writer).Encode(responses); err != nil {
		self.t.Errorf("encode batch response: %v", err)
	}
}

// The pre-fix verifier issued three HTTP requests per action and late workers
// expired behind the source-wide rate gate. Transport cost must instead grow
// with bounded batch count while every action keeps its own selector.
func TestHistoricalFleetGenerationOneAuditBatchesDistinctCheckpoints(t *testing.T) {
	fixture := &fleetHistoryBatchRPC{t: t, finalizedBlock: 1_000}
	server := httptest.NewServer(fixture)
	defer server.Close()
	rpcClient, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	client := ethclient.NewClient(rpcClient)
	calls := make([]historicalFleetGenerationOneCall, 120)
	for index := range calls {
		index := index
		block := uint64(100 + index)
		observed := map[string]any{"index": index}
		calls[index] = historicalFleetGenerationOneCall{
			action: Action{ID: fmt.Sprintf("fleet.bind.%d.1", index+1)},
			record: &ActionPostcondition{
				EVMFinalized: ChainHead{Number: block, Hash: fleetHistoryBatchBlockHash(block)}, Observed: observed,
			},
			address: common.HexToAddress("0x1234567890123456789012345678901234567890"), data: []byte{byte(index)},
			observe: func(output []byte) (map[string]any, error) {
				if len(output) != 1 || output[0] != 1 {
					return nil, fmt.Errorf("unexpected output %x", output)
				}
				return map[string]any{"index": index}, nil
			},
		}
	}
	if err := verifyHistoricalFleetGenerationOneClient(context.Background(), client, calls, false); err != nil {
		t.Fatal(err)
	}
	if fixture.httpRequests != 7 || fixture.blockBatchRequests != 3 || fixture.contractBatchRequests != 3 {
		t.Fatalf("HTTP/header/contract requests=%d/%d/%d, want 7/3/3", fixture.httpRequests, fixture.blockBatchRequests, fixture.contractBatchRequests)
	}
	if len(fixture.blockSelectors) != len(calls) || len(fixture.contractSelectors) != len(calls) {
		t.Fatalf("block/contract selectors=%d/%d, want %d/%d", len(fixture.blockSelectors), len(fixture.contractSelectors), len(calls), len(calls))
	}
	for index := range calls {
		want := uint64(100 + index)
		if fixture.blockSelectors[index] != want || fixture.contractSelectors[index] != want {
			t.Fatalf("selector %d=%d/%d, want %d", index, fixture.blockSelectors[index], fixture.contractSelectors[index], want)
		}
	}
}

// A canonical mismatch must stop the whole batch before any historical state
// result can be interpreted or cached.
func TestHistoricalFleetGenerationOneAuditRejectsTamperedCanonicalBlockBeforeStateReads(t *testing.T) {
	fixture := &fleetHistoryBatchRPC{t: t, finalizedBlock: 1_000, tamperedBlock: 101}
	server := httptest.NewServer(fixture)
	defer server.Close()
	rpcClient, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	client := ethclient.NewClient(rpcClient)
	calls := make([]historicalFleetGenerationOneCall, 2)
	for index := range calls {
		block := uint64(100 + index)
		calls[index] = historicalFleetGenerationOneCall{
			action:  Action{ID: fmt.Sprintf("fleet.mirror.%d", index+1)},
			record:  &ActionPostcondition{EVMFinalized: ChainHead{Number: block, Hash: fleetHistoryBatchBlockHash(block)}, Observed: map[string]any{}},
			address: common.HexToAddress("0x1234567890123456789012345678901234567890"), data: []byte{byte(index)},
			observe: func([]byte) (map[string]any, error) { return map[string]any{}, nil },
		}
	}
	err = verifyHistoricalFleetGenerationOneClient(context.Background(), client, calls, false)
	if err == nil || !strings.Contains(err.Error(), "canonical hash") {
		t.Fatalf("tampered canonical block error=%v", err)
	}
	if fixture.contractBatchRequests != 0 || len(fixture.contractSelectors) != 0 {
		t.Fatalf("state reads occurred after canonical failure: batches=%d selectors=%v", fixture.contractBatchRequests, fixture.contractSelectors)
	}
}

// The migration-era binding alias must prepare a real block-pinned bindingAt
// call. Treating it as a modern derived alias would silently replace its old
// observed-state schema with the batch receipt schema.
func TestHistoricalFleetAliasPreparationUsesRecordedBindingCall(t *testing.T) {
	supersession := newFleetGenerationOneSupersessionFixture(t, "historical-alias-binding")
	stateDir := t.TempDir()
	clientID := [16]byte{0x42}
	evidence := FleetBindingEvidence{
		Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: "0x" + common.Bytes2Hex(clientID[:]),
		Generation: 1, ValidFromEpoch: 2, ValidToEpoch: 33, UID: 7,
	}
	if err := os.MkdirAll(stateDir+"/public", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(stateDir+"/public/fleet-1-member-1.binding.json", evidence); err != nil {
		t.Fatal(err)
	}
	supersession.sourceRecord.Observed["client_id"] = evidence.ClientID
	supersession.sourceRecord.IndependentObserved["client_id"] = evidence.ClientID
	coordinatorAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")
	executor := &Executor{
		cfg: supersession.cfg, stateDir: stateDir,
		payloads: &DeploymentPayloads{Manifest: ContractDeployment{CoordinatorProxy: coordinatorAddress}},
	}
	audit := carriedActionAudit{action: supersession.sourceAction, entry: supersession.sourceEntry, record: supersession.sourceRecord}
	call, err := executor.prepareHistoricalFleetGenerationOneCall(context.Background(), audit, supersession.coordinates)
	if err != nil {
		t.Fatal(err)
	}
	if call.address != coordinatorAddress || len(call.data) == 0 || call.observe == nil {
		t.Fatalf("historical binding call is incomplete: address=%s calldata=%x", call.address, call.data)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	output, err := parsed.Methods["bindingAt"].Outputs.Pack(true, stabi.STCoordinatorBindingRecord{Generation: 1, Uid: 7})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := call.observe(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := observedPostconditionMatches(supersession.sourceRecord.Observed, observed); err != nil {
		t.Fatalf("historical binding observation: %v", err)
	}
}

// The carried-plan integration installs the cache only after the exact
// successor chain and historical RPC proof both pass. Its key includes the
// receipt hash, so adjacent evidence cannot inherit the optimization.
func TestCarriedFleetHistoryBatchCachesOnlyExactVerifiedAction(t *testing.T) {
	supersession := newFleetGenerationOneSupersessionFixture(t, "historical-alias-mirror")
	cfg := supersession.cfg
	cfg.OperationalRPCMode = rpcModePublicOverride
	for _, record := range []*ActionPostcondition{supersession.sourceRecord, supersession.installRecord, supersession.refreshRecord} {
		record.OperationalRPCMode = rpcModePublicOverride
		record.IndependentRPC = false
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner := fleetMemberMinerIndex(cfg, 1, member)
		label := fmt.Sprintf("miner-%d", miner)
		role := roles.Clients[label]
		role.ClientIDHex = fmt.Sprintf("%032x", member)
		roles.Clients[label] = role
	}
	stateDir := t.TempDir()
	if err := os.MkdirAll(stateDir+"/public", 0o755); err != nil {
		t.Fatal(err)
	}
	coordinatorAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")
	if err := saveContractDeployment(stateDir, ContractDeployment{CoordinatorProxy: coordinatorAddress}); err != nil {
		t.Fatal(err)
	}
	manifest, _, commitmentHash, err := fleetManifest(cfg, stateDir, roles, 1)
	if err != nil {
		t.Fatal(err)
	}
	finalizedBlockHash := [32]byte{0x42}
	evidence := FleetCommitmentEvidence{
		Schema: fleetCommitmentEvidenceSchemaV2, ManifestURI: "fleet-1.json",
		CommitmentHash: "0x" + common.Bytes2Hex(commitmentHash[:]), Hotkey: "0x" + common.Bytes2Hex(manifest.Hotkey[:]),
		ExtrinsicHash: "0x" + strings.Repeat("11", 32), CommitmentBlock: 9, FinalizedBlock: 9,
		FinalizedBlockHash: "0x" + common.Bytes2Hex(finalizedBlockHash[:]),
	}
	if err := writePublicJSON(stateDir+"/public/fleet-1.commitment.json", evidence); err != nil {
		t.Fatal(err)
	}
	supersession.sourceRecord.EVMFinalized = ChainHead{Number: 25, Hash: fleetHistoryBatchBlockHash(25)}
	supersession.sourceRecord.IndependentEVMFinalized = supersession.sourceRecord.EVMFinalized
	supersession.sourceRecord.Observed = map[string]any{
		"kind": supersession.sourceAction.Kind, "target": supersession.sourceAction.Target, "fleet": 1,
		"commitment_hash": "0x" + common.Bytes2Hex(commitmentHash[:]), "finalized_block": 9,
	}
	supersession.sourceRecord.IndependentObserved = map[string]any{
		"kind": supersession.sourceAction.Kind, "target": supersession.sourceAction.Target, "fleet": 1,
		"commitment_hash": "0x" + common.Bytes2Hex(commitmentHash[:]), "finalized_block": 9,
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	contractOutput, err := parsed.Methods["mirroredCommitments"].Outputs.Pack(commitmentHash, finalizedBlockHash, uint64(9))
	if err != nil {
		t.Fatal(err)
	}
	rpcFixture := &fleetHistoryBatchRPC{t: t, finalizedBlock: 1_000, contractResult: "0x" + common.Bytes2Hex(contractOutput)}
	server := httptest.NewServer(rpcFixture)
	defer server.Close()
	rpcClient, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	manager := &EVMTxManager{client: ethclient.NewClient(rpcClient)}
	currentPlanHash := "0x" + strings.Repeat("88", 32)
	plan := &SetupPlan{
		PlanHash: currentPlanHash, PriorPlanHashes: []string{supersession.sourceEntry.PlanHash},
		Actions: []Action{supersession.sourceAction, supersession.installAction, supersession.refreshAction},
	}
	executor := &Executor{
		cfg: cfg, stateDir: stateDir, plan: plan, roles: roles, deployer: manager, oracle: manager,
		payloads: &DeploymentPayloads{Manifest: ContractDeployment{CoordinatorProxy: coordinatorAddress}},
	}
	for _, evidence := range []struct {
		record *ActionPostcondition
		entry  *JournalEntry
	}{
		{record: supersession.sourceRecord, entry: &supersession.sourceEntry},
		{record: supersession.installRecord, entry: &supersession.installEntry},
		{record: supersession.refreshRecord, entry: &supersession.refreshEntry},
	} {
		path, hash, persistErr := executor.persistActionPostcondition(evidence.record)
		if persistErr != nil {
			t.Fatal(persistErr)
		}
		evidence.entry.PostconditionPath, evidence.entry.PostconditionHash = path, hash
	}
	executor.journal = &Journal{entries: []JournalEntry{supersession.sourceEntry, supersession.installEntry, supersession.refreshEntry}}
	audit := carriedActionAudit{action: supersession.sourceAction, entry: supersession.sourceEntry, record: supersession.sourceRecord}
	keys, err := executor.verifyCarriedFleetGenerationOneHistory(context.Background(), []carriedActionAudit{audit})
	if err != nil {
		t.Fatal(err)
	}
	key := carriedVerificationKey(supersession.sourceEntry)
	if !keys[key] || len(keys) != 1 {
		t.Fatalf("historical fleet cache keys=%v", keys)
	}
	if rpcFixture.httpRequests != 3 || rpcFixture.blockBatchRequests != 1 || rpcFixture.contractBatchRequests != 1 {
		t.Fatalf("integration HTTP/header/contract requests=%d/%d/%d, want 3/1/1", rpcFixture.httpRequests, rpcFixture.blockBatchRequests, rpcFixture.contractBatchRequests)
	}
	executor.carriedFleetHistoryKeys = keys
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := executor.verifyVerifiedActionStateWithRecord(cancelled, supersession.sourceAction, supersession.sourceEntry, supersession.sourceRecord, nil); err != nil {
		t.Fatalf("exact preverified action was replayed: %v", err)
	}
	tamperedRecord := *supersession.sourceRecord
	tamperedRecord.Observed, err = cloneObservedPostState(supersession.sourceRecord.Observed)
	if err != nil {
		t.Fatal(err)
	}
	tamperedRecord.Observed["finalized_block"] = 8
	if err := executor.verifyVerifiedActionStateWithRecord(cancelled, supersession.sourceAction, supersession.sourceEntry, &tamperedRecord, nil); err == nil {
		t.Fatal("tampered receipt body inherited the historical fleet cache")
	}
	adjacent := supersession.sourceEntry
	adjacent.PostconditionHash = "0x" + strings.Repeat("99", 32)
	if err := executor.verifyVerifiedActionStateWithRecord(cancelled, supersession.sourceAction, adjacent, supersession.sourceRecord, nil); err == nil {
		t.Fatal("adjacent postcondition hash inherited the historical fleet cache")
	}
}
