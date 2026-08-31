package main

// These tests force the legacy/atomic generation-1 to generation-2 transition
// without a live chain, including every ordering and identity boundary that
// permits historical replay during carried-plan preflight.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/stabi"
)

// Holds one complete synthetic source/install/refresh evidence chain.
type fleetGenerationOneSupersessionFixture struct {
	cfg                         *ResolvedConfig
	coordinates                 fleetGenerationOneActionCoordinates
	sourceAction, installAction Action
	refreshAction               Action
	sourceEntry, installEntry   JournalEntry
	refreshEntry                JournalEntry
	sourceRecord, installRecord *ActionPostcondition
	refreshRecord               *ActionPostcondition
}

// Bind a deterministic action intent for the synthetic approved plan.
func testFleetSupersessionAction(t *testing.T, action Action) Action {
	t.Helper()
	intentHash, err := actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	action.IntentHash = intentHash
	return action
}

// Build one dual-observer receipt at a fixed finalized checkpoint.
func testFleetSupersessionPostcondition(cfg *ResolvedConfig, action Action, entry JournalEntry, block uint64, observed map[string]any) *ActionPostcondition {
	comparison := make(map[string]any, len(observed))
	for key, value := range observed {
		comparison[key] = value
	}
	blockHash := "0x" + strings.Repeat("ab", 32)
	substrateHash := "0x" + strings.Repeat("cd", 32)
	return &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: entry.PlanHash, ActionID: action.ID, IntentHash: entry.IntentHash,
		OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg),
		SubstrateFinalized: ChainHead{Number: block, Hash: substrateHash}, EVMFinalized: ChainHead{Number: block, Hash: blockHash}, EVMHashDomain: "evm-rpc",
		Observed: observed, IndependentSubstrateFinalized: ChainHead{Number: block, Hash: substrateHash},
		IndependentEVMFinalized: ChainHead{Number: block, Hash: blockHash}, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: comparison,
	}
}

// Construct the exact source/install/refresh chain for a legacy write, atomic
// alias, or install action itself.
func newFleetGenerationOneSupersessionFixture(t *testing.T, sourceKind string) fleetGenerationOneSupersessionFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	cfg.Config.Topology.ClientsPerHeadFleet = 4
	planHash := "0x" + strings.Repeat("77", 32)
	manifestHash := "0x" + strings.Repeat("11", 32)
	batcher := "0x1234567890123456789012345678901234567890"
	installAction := testFleetSupersessionAction(t, Action{
		ID: "fleet.install.batch.1", Kind: "evm-transaction", Target: batcher,
		Parameters: map[string]string{
			deploymentManifestHashParameter: manifestHash, "first_fleet": "1", "last_fleet": "10", "generation": "1",
			evmMaximumGasUnitsParameter: "18000000", evmMaximumFeePerGasParameter: "100000000000",
		},
		Spend: Spend{EVMGasWei: "1800000000000000000"},
	})
	refreshAction := testFleetSupersessionAction(t, Action{
		ID: "fleet.refresh.batch.1", Kind: "evm-transaction", Target: batcher,
		Parameters: map[string]string{
			deploymentManifestHashParameter: manifestHash, "first_fleet": "1", "last_fleet": "10", "generation": "2",
			evmMaximumGasUnitsParameter: "24000000", evmMaximumFeePerGasParameter: "100000000000",
		},
		Spend: Spend{EVMGasWei: "2400000000000000000"},
	})
	installEntry := JournalEntry{
		Sequence: 20, PlanHash: planHash, ActionID: installAction.ID, IntentHash: installAction.IntentHash,
		Stage: StageVerified, PostconditionHash: "0x" + strings.Repeat("22", 32),
	}
	refreshEntry := JournalEntry{
		Sequence: 30, PlanHash: planHash, ActionID: refreshAction.ID, IntentHash: refreshAction.IntentHash,
		Stage: StageVerified, PostconditionHash: "0x" + strings.Repeat("33", 32),
	}
	installObserved := map[string]any{
		"kind": installAction.Kind, "target": installAction.Target, "batch": 1, "first_fleet": 1, "last_fleet": 10,
		"generation": 1, "installed_fleets": 0, "carried_fleets": 10, "members": 40, "transaction_hash": "",
	}
	refreshObserved := map[string]any{
		"kind": refreshAction.Kind, "target": refreshAction.Target, "batch": 1, "first_fleet": 1, "last_fleet": 10,
		"generation": 2, "fleets": 10, "members": 40, "transaction_hash": "0x" + strings.Repeat("44", 32),
		"calldata_hash": "0x" + strings.Repeat("55", 32), "effective_epoch": 8, "valid_to_epoch": 39,
	}
	installRecord := testFleetSupersessionPostcondition(cfg, installAction, installEntry, 20, installObserved)
	refreshRecord := testFleetSupersessionPostcondition(cfg, refreshAction, refreshEntry, 30, refreshObserved)

	sourceAction := Action{}
	sourceEntry := JournalEntry{}
	var sourceRecord *ActionPostcondition
	switch sourceKind {
	case "install":
		sourceAction, sourceEntry, sourceRecord = installAction, installEntry, installRecord
	case "legacy-mirror":
		sourceAction = testFleetSupersessionAction(t, Action{
			ID: "fleet.mirror.1", Kind: "evm-transaction", Target: "head-fleet:1",
			Parameters: map[string]string{
				deploymentManifestHashParameter: manifestHash, fleetCommitmentStorageParameter: fleetCommitmentStorageV2,
				evmMaximumGasUnitsParameter: "200000", evmMaximumFeePerGasParameter: "100000000000",
			}, Spend: Spend{EVMGasWei: "20000000000000000"},
		})
		sourceEntry = JournalEntry{Sequence: 10, PlanHash: planHash, ActionID: sourceAction.ID, IntentHash: sourceAction.IntentHash, Stage: StageVerified}
		sourceRecord = testFleetSupersessionPostcondition(cfg, sourceAction, sourceEntry, 10, map[string]any{
			"kind": sourceAction.Kind, "target": sourceAction.Target, "fleet": 1,
			"commitment_hash": "0x" + strings.Repeat("66", 32), "finalized_block": 9,
		})
	case "legacy-binding":
		sourceAction = testFleetSupersessionAction(t, Action{
			ID: "fleet.bind.1.1", Kind: "evm-transaction", Target: "miner:1",
			Parameters: map[string]string{
				deploymentManifestHashParameter: manifestHash,
				evmMaximumGasUnitsParameter:     "400000", evmMaximumFeePerGasParameter: "100000000000",
			}, Spend: Spend{EVMGasWei: "40000000000000000"},
		})
		sourceEntry = JournalEntry{Sequence: 10, PlanHash: planHash, ActionID: sourceAction.ID, IntentHash: sourceAction.IntentHash, Stage: StageVerified}
		sourceRecord = testFleetSupersessionPostcondition(cfg, sourceAction, sourceEntry, 10, map[string]any{
			"kind": sourceAction.Kind, "target": sourceAction.Target, "fleet": 1, "member": 1,
			"client_id": "0x" + strings.Repeat("77", 16), "uid": 7,
		})
	case "alias-mirror":
		sourceAction = testFleetSupersessionAction(t, Action{
			ID: "fleet.mirror.1", Kind: "evm-read", Target: "head-fleet:1",
			Parameters: map[string]string{
				deploymentManifestHashParameter: manifestHash, fleetCommitmentStorageParameter: fleetCommitmentStorageV2, "batch_installed": "true",
			},
		})
		sourceEntry = JournalEntry{Sequence: 25, PlanHash: planHash, ActionID: sourceAction.ID, IntentHash: sourceAction.IntentHash, Stage: StageVerified}
		sourceRecord = testFleetSupersessionPostcondition(cfg, sourceAction, sourceEntry, 20, map[string]any{
			"kind": sourceAction.Kind, "target": sourceAction.Target, "batch": 1, "fleet": 1,
			"source_action": installAction.ID, "source_postcondition_hash": installEntry.PostconditionHash,
			"commitment_hash": "0x" + strings.Repeat("66", 32), "finalized_block": 9,
		})
	case "alias-binding":
		sourceAction = testFleetSupersessionAction(t, Action{
			ID: "fleet.bind.1.1", Kind: "evm-read", Target: "miner:1",
			Parameters: map[string]string{deploymentManifestHashParameter: manifestHash, "batch_installed": "true"},
		})
		sourceEntry = JournalEntry{Sequence: 25, PlanHash: planHash, ActionID: sourceAction.ID, IntentHash: sourceAction.IntentHash, Stage: StageVerified}
		sourceRecord = testFleetSupersessionPostcondition(cfg, sourceAction, sourceEntry, 20, map[string]any{
			"kind": sourceAction.Kind, "target": sourceAction.Target, "batch": 1, "fleet": 1, "member": 1,
			"source_action": installAction.ID, "source_postcondition_hash": installEntry.PostconditionHash,
			"client_id": "0x" + strings.Repeat("77", 16), "uid": 7,
		})
	default:
		t.Fatalf("unknown fleet supersession source kind %q", sourceKind)
	}
	coordinates, applicable, err := fleetGenerationOneCoordinates(cfg, sourceAction)
	if err != nil || !applicable {
		t.Fatalf("source coordinates: applicable=%t coordinates=%+v err=%v", applicable, coordinates, err)
	}
	return fleetGenerationOneSupersessionFixture{
		cfg: cfg, coordinates: coordinates, sourceAction: sourceAction, installAction: installAction, refreshAction: refreshAction,
		sourceEntry: sourceEntry, installEntry: installEntry, refreshEntry: refreshEntry,
		sourceRecord: sourceRecord, installRecord: installRecord, refreshRecord: refreshRecord,
	}
}

// Every supported generation-1 source must converge through the same exact
// install and refresh range before its live state may be considered consumed.
func TestFleetGenerationOneHistoricalReplayAcceptsEveryExactSourceShape(t *testing.T) {
	for _, sourceKind := range []string{"install", "legacy-mirror", "legacy-binding", "alias-mirror", "alias-binding"} {
		fixture := newFleetGenerationOneSupersessionFixture(t, sourceKind)
		if err := validateFleetGenerationOneSupersession(
			fixture.cfg, fixture.coordinates,
			fixture.sourceAction, fixture.sourceEntry, fixture.sourceRecord,
			fixture.installAction, fixture.installEntry, fixture.installRecord,
			fixture.refreshAction, fixture.refreshEntry, fixture.refreshRecord,
		); err != nil {
			t.Errorf("exact %s chain rejected: %v", sourceKind, err)
		}
	}
}

// Mutate each adjacent identity/order boundary independently; none may turn a
// stale generation-1 current state into an accepted historical assertion.
func TestFleetGenerationOneHistoricalReplayRejectsAdjacentSuccessors(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*fleetGenerationOneSupersessionFixture)
	}{
		{name: "source not verified", mutate: func(value *fleetGenerationOneSupersessionFixture) { value.sourceEntry.Stage = StageFinalized }},
		{name: "install not later", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.installEntry.Sequence = value.sourceEntry.Sequence
		}},
		{name: "refresh not later", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.refreshEntry.Sequence = value.installEntry.Sequence
		}},
		{name: "install checkpoint older", mutate: func(value *fleetGenerationOneSupersessionFixture) { value.installRecord.EVMFinalized.Number = 9 }},
		{name: "refresh comparison checkpoint older", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.refreshRecord.IndependentEVMFinalized.Number = 19
		}},
		{name: "adjacent refresh range", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.refreshAction.Parameters["first_fleet"] = "11"
		}},
		{name: "refresh generation", mutate: func(value *fleetGenerationOneSupersessionFixture) { value.refreshAction.Parameters["generation"] = "3" }},
		{name: "different batcher", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.refreshAction.Target = "0x9999999999999999999999999999999999999999"
		}},
		{name: "different source deployment", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.sourceAction.Parameters[deploymentManifestHashParameter] = "0xother"
		}},
		{name: "different refresh deployment", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.refreshAction.Parameters[deploymentManifestHashParameter] = "0xother"
		}},
		{name: "install partition gap", mutate: func(value *fleetGenerationOneSupersessionFixture) { value.installRecord.Observed["carried_fleets"] = 9 }},
		{name: "refresh member gap", mutate: func(value *fleetGenerationOneSupersessionFixture) { value.refreshRecord.Observed["members"] = 39 }},
		{name: "comparison observation drift", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.refreshRecord.IndependentObserved["members"] = 39
		}},
		{name: "record plan drift", mutate: func(value *fleetGenerationOneSupersessionFixture) { value.installRecord.PlanHash = "0xother" }},
		{name: "empty refresh transaction", mutate: func(value *fleetGenerationOneSupersessionFixture) {
			value.refreshRecord.Observed["transaction_hash"] = ""
		}},
	}
	for _, mutation := range mutations {
		fixture := newFleetGenerationOneSupersessionFixture(t, "legacy-mirror")
		mutation.mutate(&fixture)
		if err := validateFleetGenerationOneSupersession(
			fixture.cfg, fixture.coordinates,
			fixture.sourceAction, fixture.sourceEntry, fixture.sourceRecord,
			fixture.installAction, fixture.installEntry, fixture.installRecord,
			fixture.refreshAction, fixture.refreshEntry, fixture.refreshRecord,
		); err == nil {
			t.Errorf("%s mutation was accepted", mutation.name)
		}
	}
}

// Alias replay additionally requires its receipt to name the exact source
// batch receipt and to follow that batch in journal order.
func TestFleetGenerationOneAliasRequiresExactInstallReceipt(t *testing.T) {
	for _, mutation := range []func(*fleetGenerationOneSupersessionFixture){
		func(value *fleetGenerationOneSupersessionFixture) {
			value.sourceEntry.Sequence = value.installEntry.Sequence
		},
		func(value *fleetGenerationOneSupersessionFixture) { value.sourceRecord.EVMFinalized.Number++ },
		func(value *fleetGenerationOneSupersessionFixture) {
			value.sourceRecord.Observed["source_action"] = "fleet.install.batch.2"
		},
		func(value *fleetGenerationOneSupersessionFixture) {
			value.sourceRecord.Observed["source_postcondition_hash"] = "0xother"
		},
		func(value *fleetGenerationOneSupersessionFixture) { value.sourceRecord.Observed["batch"] = 2 },
	} {
		fixture := newFleetGenerationOneSupersessionFixture(t, "alias-mirror")
		mutation(&fixture)
		if err := validateFleetGenerationOneSupersession(
			fixture.cfg, fixture.coordinates,
			fixture.sourceAction, fixture.sourceEntry, fixture.sourceRecord,
			fixture.installAction, fixture.installEntry, fixture.installRecord,
			fixture.refreshAction, fixture.refreshEntry, fixture.refreshRecord,
		); err == nil {
			t.Error("malformed alias successor was accepted")
		}
	}
}

// Classification itself rejects challenger, noncanonical, partially marked,
// and spend-bearing aliases before inspecting any journal evidence.
func TestFleetGenerationOneSupersessionClassifiesOnlyCanonicalHeadActions(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	cfg.Config.Topology.ClientsPerHeadFleet = 4
	ordinary := Action{ID: "operator.register.1"}
	if _, applicable, err := fleetGenerationOneCoordinates(cfg, ordinary); err != nil || applicable {
		t.Fatalf("ordinary action classified as fleet generation 1: applicable=%t err=%v", applicable, err)
	}
	baseline := newFleetGenerationOneSupersessionFixture(t, "legacy-mirror").sourceAction
	challenger := baseline
	challenger.ID, challenger.Target = "fleet.mirror.11", "challenger-fleet:11"
	if _, applicable, err := fleetGenerationOneCoordinates(cfg, challenger); err != nil || applicable {
		t.Fatalf("challenger action did not retain live verification: applicable=%t err=%v", applicable, err)
	}
	invalid := []Action{baseline, baseline, baseline, baseline}
	invalid[0].ID = "fleet.mirror.01"
	invalid[1].ID, invalid[1].Target = "fleet.mirror.999", "challenger-fleet:999"
	invalid[2].Parameters = cloneStrings(invalid[2].Parameters)
	invalid[2].Parameters["batch_installed"] = "false"
	invalid[3].Kind = "evm-read"
	invalid[3].Parameters = cloneStrings(invalid[3].Parameters)
	invalid[3].Parameters["batch_installed"] = "true"
	for index, action := range invalid {
		if _, applicable, err := fleetGenerationOneCoordinates(cfg, action); err == nil || !applicable {
			t.Errorf("invalid fleet action %d classification: applicable=%t err=%v", index, applicable, err)
		}
	}
}

// Exercise the production resolver: an exact complete chain is historical,
// no refresh remains live-checked, and a refresh without its install fails.
func TestFleetGenerationOneSupersessionResolverRequiresCompleteVerifiedChain(t *testing.T) {
	fixture := newFleetGenerationOneSupersessionFixture(t, "legacy-mirror")
	stateDir := t.TempDir()
	plan := &SetupPlan{
		PlanHash: fixture.sourceEntry.PlanHash, Actions: []Action{fixture.sourceAction, fixture.installAction, fixture.refreshAction},
	}
	executor := &Executor{cfg: fixture.cfg, stateDir: stateDir, plan: plan}
	installPath, installHash, err := executor.persistActionPostcondition(fixture.installRecord)
	if err != nil {
		t.Fatal(err)
	}
	refreshPath, refreshHash, err := executor.persistActionPostcondition(fixture.refreshRecord)
	if err != nil {
		t.Fatal(err)
	}
	fixture.installEntry.PostconditionPath, fixture.installEntry.PostconditionHash = installPath, installHash
	fixture.refreshEntry.PostconditionPath, fixture.refreshEntry.PostconditionHash = refreshPath, refreshHash
	executor.journal = &Journal{entries: []JournalEntry{fixture.sourceEntry, fixture.installEntry, fixture.refreshEntry}}
	if superseded, err := executor.fleetGenerationOneActionSuperseded(fixture.sourceAction, fixture.sourceEntry, fixture.sourceRecord); err != nil || !superseded {
		t.Fatalf("complete successor chain rejected: superseded=%t err=%v", superseded, err)
	}
	executor.journal = &Journal{entries: []JournalEntry{fixture.sourceEntry, fixture.installEntry}}
	if superseded, err := executor.fleetGenerationOneActionSuperseded(fixture.sourceAction, fixture.sourceEntry, fixture.sourceRecord); err != nil || superseded {
		t.Fatalf("absent refresh bypassed live checking: superseded=%t err=%v", superseded, err)
	}
	executor.journal = &Journal{entries: []JournalEntry{fixture.sourceEntry, fixture.refreshEntry}}
	if superseded, err := executor.fleetGenerationOneActionSuperseded(fixture.sourceAction, fixture.sourceEntry, fixture.sourceRecord); err == nil || superseded {
		t.Fatalf("partial successor chain was accepted: superseded=%t err=%v", superseded, err)
	}
}

// Serve exact generation-1 results only at the recorded checkpoint and a
// generation-2 mismatch everywhere else. The requested selectors are retained
// so the test proves the verifier did not silently resolve a new finalized head.
type fleetHistoricalPostStateRPC struct {
	t                           *testing.T
	historicalBlock             uint64
	mirrorMethod, bindingMethod [4]byte
	historicalMirror            []byte
	currentMirror               []byte
	historicalBinding           []byte
	currentBinding              []byte
	selectors                   []string
}

// Return ABI output selected by both method and explicit block number.
func (self *fleetHistoricalPostStateRPC) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.t.Helper()
	defer request.Body.Close()
	var call struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
	if call.Method != "eth_call" || len(call.Params) != 2 {
		response["error"] = map[string]any{"code": -32601, "message": "unexpected method"}
		_ = json.NewEncoder(writer).Encode(response)
		return
	}
	var invocation struct {
		Data  string `json:"data"`
		Input string `json:"input"`
	}
	var selector string
	if json.Unmarshal(call.Params[0], &invocation) != nil || json.Unmarshal(call.Params[1], &selector) != nil {
		response["error"] = map[string]any{"code": -32602, "message": "malformed call"}
		_ = json.NewEncoder(writer).Encode(response)
		return
	}
	self.selectors = append(self.selectors, selector)
	encodedCalldata := invocation.Data
	if encodedCalldata == "" {
		encodedCalldata = invocation.Input
	}
	calldata, err := hex.DecodeString(stringsTrim0x(encodedCalldata))
	if err != nil || len(calldata) < 4 {
		response["error"] = map[string]any{"code": -32602, "message": "malformed calldata"}
		_ = json.NewEncoder(writer).Encode(response)
		return
	}
	var method [4]byte
	copy(method[:], calldata[:4])
	historicalSelector := "0x" + new(big.Int).SetUint64(self.historicalBlock).Text(16)
	result := []byte(nil)
	switch method {
	case self.mirrorMethod:
		result = self.currentMirror
		if selector == historicalSelector {
			result = self.historicalMirror
		}
	case self.bindingMethod:
		result = self.currentBinding
		if selector == historicalSelector {
			result = self.historicalBinding
		}
	default:
		response["error"] = map[string]any{"code": -32602, "message": "unexpected selector"}
		_ = json.NewEncoder(writer).Encode(response)
		return
	}
	response["result"] = "0x" + hex.EncodeToString(result)
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		self.t.Errorf("encode RPC response: %v", err)
	}
}

// The production post-state functions must use the caller's authenticated
// block for both mirror and binding reads. This deterministically reproduces
// the resume bug: latest state is generation 2 while recorded state is 1.
func TestFleetGenerationOneHistoricalPostStateUsesRecordedEVMBlock(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	cfg.Config.Topology.ClientsPerHeadFleet = 4
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner := fleetMemberMinerIndex(cfg, 1, member)
		label := "miner-" + new(big.Int).SetInt64(int64(miner)).String()
		role := roles.Clients[label]
		clientID := [16]byte{byte(member)}
		role.ClientIDHex = hex.EncodeToString(clientID[:])
		roles.Clients[label] = role
	}
	stateDir := t.TempDir()
	coordinatorAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")
	if err := saveContractDeployment(stateDir, ContractDeployment{CoordinatorProxy: coordinatorAddress}); err != nil {
		t.Fatal(err)
	}
	manifest, canonical, commitmentHash, err := fleetManifest(cfg, stateDir, roles, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(stateDir+"/public/fleet-1.json", append(canonical, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	finalizedBlockHash := [32]byte{0x42}
	commitmentEvidence := FleetCommitmentEvidence{
		Schema: fleetCommitmentEvidenceSchemaV2, ManifestURI: "fleet-1.json",
		CommitmentHash: "0x" + hex.EncodeToString(commitmentHash[:]), Hotkey: "0x" + hex.EncodeToString(manifest.Hotkey[:]),
		ExtrinsicHash: "0x" + strings.Repeat("11", 32), CommitmentBlock: 99, FinalizedBlock: 99,
		FinalizedBlockHash: "0x" + hex.EncodeToString(finalizedBlockHash[:]),
	}
	if err := writePublicJSON(stateDir+"/public/fleet-1.commitment.json", commitmentEvidence); err != nil {
		t.Fatal(err)
	}
	clientID := manifest.Members[0].ClientID
	bindingEvidence := FleetBindingEvidence{
		Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: "0x" + hex.EncodeToString(clientID[:]),
		Generation: 1, ValidFromEpoch: 2, ValidToEpoch: 33, UID: 7,
	}
	if err := writePublicJSON(stateDir+"/public/fleet-1-member-1.binding.json", bindingEvidence); err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	historicalMirror, err := parsed.Methods["mirroredCommitments"].Outputs.Pack(commitmentHash, finalizedBlockHash, uint64(99))
	if err != nil {
		t.Fatal(err)
	}
	currentMirror, err := parsed.Methods["mirroredCommitments"].Outputs.Pack([32]byte{0x99}, [32]byte{0x98}, uint64(199))
	if err != nil {
		t.Fatal(err)
	}
	historicalRecord := stabi.STCoordinatorBindingRecord{Generation: 1, Uid: 7}
	currentRecord := stabi.STCoordinatorBindingRecord{Generation: 2, Uid: 7}
	historicalBinding, err := parsed.Methods["bindingAt"].Outputs.Pack(true, historicalRecord)
	if err != nil {
		t.Fatal(err)
	}
	currentBinding, err := parsed.Methods["bindingAt"].Outputs.Pack(true, currentRecord)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	mirrorCall := coordinator.PackMirroredCommitments(manifest.Hotkey)
	bindingCall := coordinator.PackBindingAt(clientID, big.NewInt(2))
	fixture := &fleetHistoricalPostStateRPC{
		t: t, historicalBlock: 100,
		historicalMirror: historicalMirror, currentMirror: currentMirror,
		historicalBinding: historicalBinding, currentBinding: currentBinding,
	}
	copy(fixture.mirrorMethod[:], mirrorCall[:4])
	copy(fixture.bindingMethod[:], bindingCall[:4])
	server := httptest.NewServer(fixture)
	defer server.Close()
	rpcClient, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	manager := &EVMTxManager{client: ethclient.NewClient(rpcClient)}
	executor := &Executor{
		cfg: cfg, stateDir: stateDir, roles: roles,
		payloads: &DeploymentPayloads{Manifest: ContractDeployment{CoordinatorProxy: coordinatorAddress}},
		oracle:   manager, keeper: manager,
	}
	historicalHead := ChainHead{Number: 100, Hash: "0x" + strings.Repeat("aa", 32)}
	if _, err := executor.verifyFleetMirrorPostState(context.Background(), 1, historicalHead, map[string]any{}); err != nil {
		t.Fatalf("historical mirror was not replayed at block 100: %v", err)
	}
	if _, err := executor.verifyFleetBindingPostState(context.Background(), 1, 1, historicalHead, map[string]any{}); err != nil {
		t.Fatalf("historical binding was not replayed at block 100: %v", err)
	}
	currentHead := ChainHead{Number: 101, Hash: "0x" + strings.Repeat("bb", 32)}
	if _, err := executor.verifyFleetMirrorPostState(context.Background(), 1, currentHead, map[string]any{}); err == nil {
		t.Fatal("generation-2 mirror was accepted as the generation-1 postcondition")
	}
	if _, err := executor.verifyFleetBindingPostState(context.Background(), 1, 1, currentHead, map[string]any{}); err == nil {
		t.Fatal("generation-2 binding was accepted as the generation-1 postcondition")
	}
	if len(fixture.selectors) != 4 || fixture.selectors[0] != "0x64" || fixture.selectors[1] != "0x64" || fixture.selectors[2] != "0x65" || fixture.selectors[3] != "0x65" {
		t.Fatalf("fleet post-state block selectors = %v", fixture.selectors)
	}
}
