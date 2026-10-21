package main

// Historical aliases retain their original receipt format and both pinned
// observations after renewal, even when current state has another generation.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Each observer serves one historical checkpoint and records exact reads.
// Its lock protects request accounting across the server and test goroutines.
type fleetHistoricalAliasRpc struct {
	stateLock           sync.Mutex
	t                   *testing.T
	checkpoint          uint64
	historicalOutputKVs map[string]string
	currentOutputKVs    map[string]string
	headerSelectors     []string
	contractBlocks      []uint64
}

// Reject writes and return changed contract state at every other block.
func (self *fleetHistoricalAliasRpc) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var call struct {
		Id     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		self.t.Errorf("decode historical alias request: %v", err)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": call.Id}
	switch call.Method {
	case "eth_getBlockByNumber":
		var selector string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
			self.t.Error("invalid historical alias block request")
			return
		}
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.headerSelectors = append(self.headerSelectors, selector)
		}()
		number := uint64(1_000)
		if selector != "finalized" {
			var err error
			number, err = hexutil.DecodeUint64(selector)
			if err != nil {
				self.t.Errorf("unnumbered historical alias checkpoint: %v", err)
				return
			}
		}
		response["result"] = map[string]any{"number": hexutil.EncodeUint64(number), "hash": fleetHistoryBatchBlockHash(number)}
	case "eth_call":
		var message struct {
			Input hexutil.Bytes `json:"input"`
		}
		var selector string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &message) != nil || json.Unmarshal(call.Params[1], &selector) != nil {
			self.t.Error("invalid historical alias contract request")
			return
		}
		block, err := hexutil.DecodeUint64(selector)
		if err != nil {
			self.t.Errorf("historical alias read was not pinned: %v", err)
			return
		}
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.contractBlocks = append(self.contractBlocks, block)
		}()
		outputs := self.currentOutputKVs
		if block == self.checkpoint {
			outputs = self.historicalOutputKVs
		}
		output, ok := outputs[hexutil.Encode(message.Input)]
		if !ok {
			self.t.Error("historical alias used unexpected contract calldata")
			return
		}
		response["result"] = output
	default:
		self.t.Errorf("unexpected historical alias method %q", call.Method)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		self.t.Errorf("encode historical alias response: %v", err)
	}
}

// Compare full request sequences after synchronous replay has returned.
func (self *fleetHistoricalAliasRpc) assertReads(t *testing.T, headers []string, blocks []uint64) {
	t.Helper()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if !slices.Equal(self.headerSelectors, headers) || !slices.Equal(self.contractBlocks, blocks) {
		t.Fatalf("historical reads headers=%v blocks=%v, want %v/%v", self.headerSelectors, self.contractBlocks, headers, blocks)
	}
}

// The serial client sends input; retain the full calldata and block selectors.
func TestFleetRenewalHistoricalAliasRpcDecodesInput(t *testing.T) {
	fixture := &fleetHistoricalAliasRpc{
		t: t, checkpoint: 110,
		historicalOutputKVs: map[string]string{"0x1122334401": "0xa1", "0x1122334402": "0xb1"},
		currentOutputKVs:    map[string]string{"0x1122334401": "0xa2", "0x1122334402": "0xb2"},
	}
	for index, call := range []struct {
		input  string
		block  uint64
		result string
	}{
		{input: "0x1122334401", block: 110, result: "0xa1"},
		{input: "0x1122334402", block: 110, result: "0xb1"},
		{input: "0x1122334401", block: 112, result: "0xa2"},
		{input: "0x1122334402", block: 112, result: "0xb2"},
	} {
		wire, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": index + 1, "method": "eth_call",
			"params": []any{map[string]string{"input": call.input}, hexutil.EncodeUint64(call.block)},
		})
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		fixture.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(wire))))
		var result struct {
			Id     int    `json:"id"`
			Result string `json:"result"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("input %s at block %d did not produce a response: %v", call.input, call.block, err)
		}
		if result.Id != index+1 || result.Result != call.result {
			t.Fatalf("input %s at block %d returned %+v, want %s", call.input, call.block, result, call.result)
		}
	}
	fixture.assertReads(t, nil, []uint64{110, 110, 112, 112})
}

// Holds authentic synthetic batch artifacts and both legacy alias receipts.
type fleetHistoricalAliasFixture struct {
	executor    *Executor
	actions     []Action
	records     []*ActionPostcondition
	operational *fleetHistoricalAliasRpc
	independent *fleetHistoricalAliasRpc
	batchHash   string
}

// Build signed generation-one local evidence and distinct historical readers.
func newFleetHistoricalAliasFixture(t *testing.T, independent bool) fleetHistoricalAliasFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	cfg.Config.Topology.ClientsPerHeadFleet = 1
	cfg.OperationalRPCMode = rpcModePublicOverride
	if independent {
		cfg.OperationalRPCMode = rpcModePrivateAuthority
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	clientRole := roles.Clients["miner-1"]
	clientRole.ClientIDHex = hex.EncodeToString([]byte{1, 2, 3, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	roles.Clients["miner-1"] = clientRole
	stateDir := t.TempDir()
	coordinatorAddress := common.Address{0x12}
	if err := saveContractDeployment(stateDir, ContractDeployment{CoordinatorProxy: coordinatorAddress}); err != nil {
		t.Fatal(err)
	}
	manifest, canonical, commitmentHash, err := fleetManifest(cfg, stateDir, roles, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "public", "fleet-1.json"), append(canonical, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	finalizedHash := [32]byte{0x42}
	commitment := FleetCommitmentEvidence{
		Schema: fleetCommitmentEvidenceSchemaV2, ManifestURI: "fleet-1.json",
		CommitmentHash: hexutil.Encode(commitmentHash[:]), Hotkey: hexutil.Encode(manifest.Hotkey[:]),
		ExtrinsicHash: common.Hash{0x11}.Hex(), CommitmentBlock: 9, FinalizedBlock: 9, FinalizedBlockHash: hexutil.Encode(finalizedHash[:]),
	}
	if err := writePublicJSON(filepath.Join(stateDir, "public", "fleet-1.commitment.json"), commitment); err != nil {
		t.Fatal(err)
	}
	binding, err := manifest.Binding(manifest.Members[0], 2, 33)
	if err != nil {
		t.Fatal(err)
	}
	clientSeed, err := hex.DecodeString(clientRole.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	clientSignature, err := binding.SignClient(ed25519.NewKeyFromSeed(clientSeed))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := binding.Digest()
	if err != nil {
		t.Fatal(err)
	}
	hotkey, err := crv4.KeypairFromSeedHex(roles.Substrate[fleetHotkeyLabel(1)].SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	bindingEvidence := FleetBindingEvidence{
		Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: hexutil.Encode(binding.ClientID[:]), ClientKey: hexutil.Encode(binding.ClientKey[:]),
		FleetID: hexutil.Encode(binding.FleetID[:]), Hotkey: hexutil.Encode(binding.Hotkey[:]), Generation: 1, ValidFromEpoch: 2, ValidToEpoch: 33,
		CommitmentHash: hexutil.Encode(binding.CommitmentHash[:]), BindingDigest: hexutil.Encode(digest[:]),
		ClientSignature: hexutil.Encode(clientSignature), HotkeySignature: hexutil.Encode(hotkeySignature), UID: 7,
		TransactionHash: common.Hash{0x33}.Hex(), BlockNumber: 99, BlockHash: fleetHistoryBatchBlockHash(99),
	}
	if err := writePublicJSON(filepath.Join(stateDir, "public", "fleet-1-member-1.binding.json"), bindingEvidence); err != nil {
		t.Fatal(err)
	}
	batch := FleetInstallBatchEvidence{Schema: fleetInstallBatchEvidenceSchema, Batch: 1, FirstFleet: 1, LastFleet: 10, Generation: 1}
	for fleet := 1; fleet <= 10; fleet++ {
		batch.CarriedFleets = append(batch.CarriedFleets, fleet)
		batch.MemberEvidence = append(batch.MemberEvidence, fmt.Sprintf("fleet-%d-member-1.binding.json", fleet))
	}
	if err := writePublicJSON(filepath.Join(stateDir, "public", "fleet-install-batch-1.json"), batch); err != nil {
		t.Fatal(err)
	}
	batchAction := testFleetSupersessionAction(t, Action{
		ID: "fleet.install.batch.1", Kind: "evm-transaction", Target: common.Address{0x34}.Hex(),
		Parameters: map[string]string{"first_fleet": "1", "last_fleet": "10", "generation": "1"},
	})
	actions := []Action{
		testFleetSupersessionAction(t, Action{ID: "fleet.mirror.1", Kind: "evm-read", Target: "head-fleet:1", Parameters: map[string]string{"batch_installed": "true"}}),
		testFleetSupersessionAction(t, Action{ID: "fleet.bind.1.1", Kind: "evm-read", Target: "miner:1", Parameters: map[string]string{"batch_installed": "true"}}),
	}
	plan := &SetupPlan{PlanHash: common.Hash{0x77}.Hex(), Actions: append([]Action{batchAction}, actions...)}
	executor := &Executor{cfg: cfg, stateDir: stateDir, roles: roles, plan: plan, payloads: &DeploymentPayloads{Manifest: ContractDeployment{CoordinatorProxy: coordinatorAddress}}}
	batchEntry := JournalEntry{Sequence: 20, PlanHash: plan.PlanHash, ActionID: batchAction.ID, IntentHash: batchAction.IntentHash, Stage: StageVerified}
	batchRecord := testFleetSupersessionPostcondition(cfg, batchAction, batchEntry, 100, map[string]any{
		"kind": batchAction.Kind, "target": batchAction.Target, "batch": 1, "first_fleet": 1, "last_fleet": 10,
		"generation": 1, "installed_fleets": 0, "carried_fleets": 10, "members": 10, "transaction_hash": "",
	})
	batchRecord.EVMFinalized.Hash = fleetHistoryBatchBlockHash(100)
	batchRecord.IndependentEVMFinalized = batchRecord.EVMFinalized
	batchEntry.PostconditionPath, batchEntry.PostconditionHash, err = executor.persistActionPostcondition(batchRecord)
	if err != nil {
		t.Fatal(err)
	}
	executor.journal = &Journal{entries: []JournalEntry{batchEntry}}
	fixture := fleetHistoricalAliasFixture{executor: executor, actions: actions, batchHash: batchEntry.PostconditionHash}
	observations := []map[string]any{
		{"kind": actions[0].Kind, "target": actions[0].Target, "fleet": 1, "commitment_hash": commitment.CommitmentHash, "finalized_block": 9},
		{"kind": actions[1].Kind, "target": actions[1].Target, "fleet": 1, "member": 1, "client_id": bindingEvidence.ClientID, "uid": 7},
	}
	for index, action := range actions {
		entry := JournalEntry{PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash}
		record := testFleetSupersessionPostcondition(cfg, action, entry, 110, observations[index])
		record.EVMFinalized.Hash = fleetHistoryBatchBlockHash(110)
		record.IndependentEVMFinalized = record.EVMFinalized
		if independent {
			record.IndependentEVMFinalized = ChainHead{Number: 111, Hash: fleetHistoryBatchBlockHash(111)}
		}
		fixture.records = append(fixture.records, record)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	for index := 0; index < 2; index++ {
		observer := &fleetHistoricalAliasRpc{t: t, checkpoint: uint64(110 + index), historicalOutputKVs: map[string]string{}, currentOutputKVs: map[string]string{}}
		for _, call := range []struct {
			data       []byte
			method     string
			historical []any
			current    []any
		}{
			{data: coordinator.PackMirroredCommitments(manifest.Hotkey), method: "mirroredCommitments", historical: []any{commitmentHash, finalizedHash, uint64(9)}, current: []any{[32]byte{0x91}, [32]byte{0x92}, uint64(90)}},
			{data: coordinator.PackBindingAt(binding.ClientID, big.NewInt(2)), method: "bindingAt", historical: []any{true, stabi.STCoordinatorBindingRecord{Generation: 1, Uid: 7}}, current: []any{true, stabi.STCoordinatorBindingRecord{Generation: 2, Uid: 7}}},
		} {
			for _, output := range []struct {
				values    []any
				outputKVs map[string]string
			}{{values: call.historical, outputKVs: observer.historicalOutputKVs}, {values: call.current, outputKVs: observer.currentOutputKVs}} {
				encoded, err := parsed.Methods[call.method].Outputs.Pack(output.values...)
				if err != nil {
					t.Fatal(err)
				}
				output.outputKVs[hexutil.Encode(call.data)] = hexutil.Encode(encoded)
			}
		}
		server := httptest.NewServer(observer)
		t.Cleanup(server.Close)
		client, err := rpc.DialHTTP(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		if index == 0 {
			fixture.operational = observer
			manager := &EvmTxManager{client: ethclient.NewClient(client)}
			executor.deployer, executor.oracle, executor.keeper = manager, manager, manager
		} else {
			fixture.independent = observer
			executor.independentEVM = ethclient.NewClient(client)
		}
	}
	return fixture
}

// Clone observations without changing the fixture's authenticated baseline.
func cloneFleetHistoricalAliasRecord(t *testing.T, source *ActionPostcondition) *ActionPostcondition {
	t.Helper()
	record := *source
	var err error
	record.Observed, err = cloneObservedPostState(source.Observed)
	if err != nil {
		t.Fatal(err)
	}
	record.IndependentObserved, err = cloneObservedPostState(source.IndependentObserved)
	if err != nil {
		t.Fatal(err)
	}
	return &record
}

// The old dispatcher adds exactly three fields and changes the receipt hash.
func TestFleetRenewalHistoricalAliasReplaysOriginalMirrorAndBinding(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, false)
	for index, action := range fixture.actions {
		record := fixture.records[index]
		derived, err := fixture.executor.actionPostState(t.Context(), action, record.EVMFinalized)
		if err != nil {
			t.Fatalf("%s derived control: %v", action.ID, err)
		}
		expected := cloneFleetHistoricalAliasRecord(t, record).Observed
		expected["source_action"], expected["source_postcondition_hash"], expected["batch"] = "fleet.install.batch.1", fixture.batchHash, 1
		if err := observedPostconditionMatches(expected, derived); err != nil {
			t.Fatalf("%s metadata-only causal control: %v", action.ID, err)
		}
		if err := observedPostconditionMatches(record.Observed, derived); err == nil {
			t.Fatalf("%s legacy and derived formats unexpectedly hash equally", action.ID)
		}
		before, err := canonicalHashHex(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, record); err != nil {
			t.Errorf("%s original-format replay: %v", action.ID, err)
		}
		after, err := canonicalHashHex(record)
		if err != nil || before != after {
			t.Fatalf("%s replay changed the stored receipt: %v", action.ID, err)
		}
	}
	fixture.operational.assertReads(t, []string{"finalized", "0x6e", "finalized", "0x6e"}, []uint64{110, 110})
	fixture.independent.assertReads(t, nil, nil)
}

// Both observers must replay their own checkpoint through the same adapter.
// A carried reconciliation authenticates its operational finalized head once.
// Historical receipts still replay their checkpoint and state at that head.
func TestFleetRenewalHistoricalAliasReusesAuthenticatedOperationalHead(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, false)
	head := &ChainHead{Number: 110, Hash: fleetHistoryBatchBlockHash(110)}
	for index, action := range fixture.actions {
		if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, fixture.records[index], head); err != nil {
			t.Errorf("%s cached-head replay: %v", action.ID, err)
		}
	}
	fixture.operational.assertReads(t, []string{"0x6e", "0x6e"}, []uint64{110, 110})
	fixture.independent.assertReads(t, nil, nil)
}

func TestFleetRenewalHistoricalAliasReplaysIndependentCheckpoint(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, true)
	for index, action := range fixture.actions {
		if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, fixture.records[index]); err != nil {
			t.Errorf("%s independent original-format replay: %v", action.ID, err)
		}
	}
	fixture.operational.assertReads(t, []string{"finalized", "0x6e", "finalized", "0x6e"}, []uint64{110, 110})
	fixture.independent.assertReads(t, []string{"finalized", "0x6f", "finalized", "0x6f"}, []uint64{111, 111})
}

// Current aliases retain all source metadata and perform only header reads.
func TestFleetRenewalHistoricalAliasPreservesDerivedReceipt(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, true)
	for _, action := range fixture.actions {
		record, err := fixture.executor.verifyFleetInstallAliasPostcondition(action)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, record); err != nil {
			t.Fatalf("%s derived replay: %v", action.ID, err)
		}
		for _, key := range []string{"source_action", "source_postcondition_hash", "batch"} {
			changed := cloneFleetHistoricalAliasRecord(t, record)
			changed.Observed[key] = "synthetic-changed-source"
			if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, changed); err == nil || !strings.Contains(err.Error(), "replay hash") {
				t.Fatalf("%s changed derived %s was not rejected by its exact hash: %v", action.ID, key, err)
			}
		}
	}
	fixture.independent.assertReads(t, []string{"finalized", "0x64", "finalized", "0x64"}, nil)
	fixture.operational.stateLock.Lock()
	defer fixture.operational.stateLock.Unlock()
	if len(fixture.operational.contractBlocks) != 0 {
		t.Fatalf("derived aliases performed contract reads: %v", fixture.operational.contractBlocks)
	}
}

// Partial metadata and disagreement between observer formats never downgrade.
func TestFleetRenewalHistoricalAliasRejectsPartialOrMixedMetadata(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, true)
	for index, action := range fixture.actions {
		for _, independent := range []bool{false, true} {
			for _, keys := range [][]string{{"batch"}, {"source_action", "source_postcondition_hash"}, {"source_action", "source_postcondition_hash", "batch"}} {
				record := cloneFleetHistoricalAliasRecord(t, fixture.records[index])
				observed := record.Observed
				if independent {
					observed = record.IndependentObserved
				}
				for _, key := range keys {
					observed[key] = "synthetic-source"
				}
				err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, record)
				if err == nil || !(strings.Contains(err.Error(), "partial source metadata") || strings.Contains(err.Error(), "different receipt formats")) {
					t.Fatalf("%s malformed observer=%t keys=%v was accepted: %v", action.ID, independent, keys, err)
				}
			}
		}
	}
}

// Every recorded field remains in the exact hash, including unknown fields.
func TestFleetRenewalHistoricalAliasRejectsChangedObservation(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, true)
	for index, action := range fixture.actions {
		for _, independent := range []bool{false, true} {
			for _, key := range []string{"fleet", "synthetic_extra"} {
				record := cloneFleetHistoricalAliasRecord(t, fixture.records[index])
				observed := record.Observed
				if independent {
					observed = record.IndependentObserved
				}
				observed[key] = 999
				if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, record); err == nil || !strings.Contains(err.Error(), "replay hash") {
					t.Fatalf("%s changed observer=%t field=%s was not hash-rejected: %v", action.ID, independent, key, err)
				}
			}
		}
	}
}

// Canonical hashes and fresh finality apply to both historical observers.
func TestFleetRenewalHistoricalAliasRejectsChangedCheckpoint(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, true)
	for index, action := range fixture.actions {
		for _, independent := range []bool{false, true} {
			for _, unfinalized := range []bool{false, true} {
				record := cloneFleetHistoricalAliasRecord(t, fixture.records[index])
				head := &record.EVMFinalized
				if independent {
					head = &record.IndependentEVMFinalized
				}
				head.Hash = common.Hash{0x99}.Hex()
				if unfinalized {
					*head = ChainHead{Number: 1_001, Hash: fleetHistoryBatchBlockHash(1_001)}
				}
				if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, record); err == nil || !strings.Contains(err.Error(), "historical EVM checkpoint") {
					t.Fatalf("%s invalid observer=%t unfinalized=%t checkpoint accepted: %v", action.ID, independent, unfinalized, err)
				}
			}
		}
	}
}

// A canonical block containing another generation must fail the actual read.
func TestFleetRenewalHistoricalAliasRejectsChangedContractState(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, true)
	for index, action := range fixture.actions {
		for _, independent := range []bool{false, true} {
			record := cloneFleetHistoricalAliasRecord(t, fixture.records[index])
			head := &record.EVMFinalized
			if independent {
				head = &record.IndependentEVMFinalized
			}
			*head = ChainHead{Number: 112, Hash: fleetHistoryBatchBlockHash(112)}
			if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, record); err == nil || !strings.Contains(err.Error(), "mismatch") || strings.Contains(err.Error(), "replay hash") {
				t.Fatalf("%s changed observer=%t contract state was not read-rejected: %v", action.ID, independent, err)
			}
		}
	}
}

// Single-provider receipts still require an exact cloned checkpoint and state.
func TestFleetRenewalHistoricalAliasRejectsChangedSharedClone(t *testing.T) {
	fixture := newFleetHistoricalAliasFixture(t, false)
	for index, action := range fixture.actions {
		for _, checkpoint := range []bool{false, true} {
			record := cloneFleetHistoricalAliasRecord(t, fixture.records[index])
			if checkpoint {
				record.IndependentEVMFinalized.Hash = common.Hash{0x99}.Hex()
			} else {
				record.IndependentObserved["fleet"] = 999
			}
			if err := fixture.executor.verifyHistoricalEVMPostcondition(t.Context(), action, record); err == nil || !strings.Contains(err.Error(), "shared-provider historical EVM") {
				t.Fatalf("%s changed shared clone checkpoint=%t was accepted: %v", action.ID, checkpoint, err)
			}
		}
	}
	fixture.independent.assertReads(t, nil, nil)
}
