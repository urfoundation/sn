package main

// These tests lock the accelerated fleet partitions, ABI shape and expiry
// preconditions without depending on wall-clock timing or a live RPC.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

func TestCoordinatorBatchCallsRespectThePublicEndpointLimit(t *testing.T) {
	type request struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      json.RawMessage   `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	type response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  string          `json:"result"`
	}
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		requestCount.Add(1)
		var batch []request
		if err := json.NewDecoder(httpRequest.Body).Decode(&batch); err != nil {
			t.Errorf("decode RPC batch: %v", err)
			return
		}
		if len(batch) == 0 || len(batch) > maximumEVMRPCBatchCalls {
			t.Errorf("RPC batch size=%d", len(batch))
		}
		responses := make([]response, len(batch))
		for index, call := range batch {
			if call.Method != "eth_call" || len(call.Params) != 2 || string(call.Params[1]) != `"0x7b"` {
				t.Errorf("RPC call %d method=%q params=%s", index, call.Method, call.Params)
			}
			responses[index] = response{JSONRPC: "2.0", ID: call.ID, Result: "0x0102"}
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(responses); err != nil {
			t.Errorf("encode RPC batch: %v", err)
		}
	}))
	defer server.Close()
	rpcClient, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcClient.Close()
	manager := &EVMTxManager{client: ethclient.NewClient(rpcClient)}
	calls := make([][]byte, maximumEVMRPCBatchCalls+1)
	for index := range calls {
		calls[index] = []byte{byte(index)}
	}
	outputs, err := rawCoordinatorBatchCallAt(context.Background(), manager, common.HexToAddress("0x1234"), calls, 123)
	if err != nil {
		t.Fatal(err)
	}
	if requestCount.Load() != 2 || len(outputs) != len(calls) {
		t.Fatalf("RPC requests/outputs=%d/%d, want 2/%d", requestCount.Load(), len(outputs), len(calls))
	}
	for index, output := range outputs {
		if string(output) != string([]byte{1, 2}) {
			t.Fatalf("RPC output %d=%x", index, output)
		}
	}
}

func TestFleetInstallAliasesDeriveReceiptsWithoutRPC(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	cfg.Config.Topology.ClientsPerHeadFleet = 1
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	clientRole := roles.Clients["miner-1"]
	clientID := [16]byte{1, 2, 3, 4}
	clientRole.ClientIDHex = hex.EncodeToString(clientID[:])
	roles.Clients["miner-1"] = clientRole
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	coordinatorAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")
	if err := saveContractDeployment(dir, ContractDeployment{CoordinatorProxy: coordinatorAddress}); err != nil {
		t.Fatal(err)
	}
	manifest, canonical, commitmentHash, err := fleetManifest(cfg, dir, roles, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "public", "fleet-1.json"), append(canonical, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	hexValue := func(value []byte) string { return "0x" + hex.EncodeToString(value) }
	commitmentEvidence := FleetCommitmentEvidence{
		Schema: fleetCommitmentEvidenceSchemaV2, ManifestURI: "fleet-1.json",
		CommitmentHash: hexValue(commitmentHash[:]), Hotkey: hexValue(manifest.Hotkey[:]),
		ExtrinsicHash: "0x" + strings.Repeat("11", 32), CommitmentBlock: 100, FinalizedBlock: 100,
		FinalizedBlockHash: "0x" + strings.Repeat("22", 32),
	}
	if err := writePublicJSON(filepath.Join(dir, "public", "fleet-1.commitment.json"), commitmentEvidence); err != nil {
		t.Fatal(err)
	}
	clientSeed, err := hex.DecodeString(clientRole.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := manifest.Binding(manifest.Members[0], 2, 33)
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
		Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: hexValue(binding.ClientID[:]), ClientKey: hexValue(binding.ClientKey[:]),
		FleetID: hexValue(binding.FleetID[:]), Hotkey: hexValue(binding.Hotkey[:]), Generation: 1,
		ValidFromEpoch: 2, ValidToEpoch: 33, CommitmentHash: hexValue(binding.CommitmentHash[:]), BindingDigest: hexValue(digest[:]),
		ClientSignature: hexValue(clientSignature), HotkeySignature: hexValue(hotkeySignature),
		TransactionHash: "0x" + strings.Repeat("33", 32), BlockNumber: 101, BlockHash: "0x" + strings.Repeat("44", 32), UID: 7,
	}
	if err := writePublicJSON(filepath.Join(dir, "public", "fleet-1-member-1.binding.json"), bindingEvidence); err != nil {
		t.Fatal(err)
	}
	memberEvidence := make([]string, 0, 10)
	installedFleets := make([]int, 0, 10)
	for fleet := 1; fleet <= 10; fleet++ {
		installedFleets = append(installedFleets, fleet)
		memberEvidence = append(memberEvidence, "fleet-"+strconv.Itoa(fleet)+"-member-1.binding.json")
	}
	transactionHash := "0x" + strings.Repeat("55", 32)
	batchEvidence := FleetInstallBatchEvidence{
		Schema: fleetInstallBatchEvidenceSchema, Batch: 1, FirstFleet: 1, LastFleet: 10, Generation: 1,
		EffectiveEpoch: 2, ValidToEpoch: 33, InstalledFleets: installedFleets, MemberEvidence: memberEvidence,
		TransactionHash: transactionHash, BlockNumber: 102, BlockHash: "0x" + strings.Repeat("66", 32),
	}
	if err := writePublicJSON(filepath.Join(dir, "public", "fleet-install-batch-1.json"), batchEvidence); err != nil {
		t.Fatal(err)
	}
	batchAction := Action{
		ID: "fleet.install.batch.1", Kind: "evm-transaction", Target: "batcher",
		Parameters: map[string]string{"first_fleet": "1", "last_fleet": "10", "generation": "1"},
	}
	mirrorAction := Action{ID: "fleet.mirror.1", Kind: "evm-read", Target: "head-fleet:1", Parameters: map[string]string{"batch_installed": "true"}, DependsOn: []string{batchAction.ID}}
	bindingAction := Action{ID: "fleet.bind.1.1", Kind: "evm-read", Target: "miner:1", Parameters: map[string]string{"batch_installed": "true"}, DependsOn: []string{mirrorAction.ID}}
	for _, action := range []*Action{&batchAction, &mirrorAction, &bindingAction} {
		intentHash, hashErr := actionIntentHash(*action)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		action.IntentHash = intentHash
	}
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("77", 32), Actions: []Action{batchAction, mirrorAction, bindingAction}}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	executor := &Executor{cfg: cfg, stateDir: dir, plan: plan, journal: journal, roles: roles}
	observed := map[string]any{
		"kind": batchAction.Kind, "target": batchAction.Target, "batch": 1, "first_fleet": 1, "last_fleet": 10,
		"generation": 1, "installed_fleets": 10, "carried_fleets": 0, "members": 10, "transaction_hash": transactionHash,
	}
	independentObserved, err := cloneObservedPostState(observed)
	if err != nil {
		t.Fatal(err)
	}
	source := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: plan.PlanHash, ActionID: batchAction.ID, IntentHash: batchAction.IntentHash,
		OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg),
		SubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("88", 32)},
		EVMFinalized:       ChainHead{Number: 102, Hash: batchEvidence.BlockHash}, EVMHashDomain: "evm-rpc",
		Observed:                      observed,
		IndependentSubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("88", 32)},
		IndependentEVMFinalized:       ChainHead{Number: 102, Hash: batchEvidence.BlockHash}, IndependentEVMHashDomain: "evm-rpc",
		IndependentObserved: independentObserved,
	}
	path, hash, err := executor.persistActionPostcondition(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: plan.PlanHash, ActionID: batchAction.ID, IntentHash: batchAction.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []Action{mirrorAction, bindingAction} {
		receipt, err := executor.verifyActionPostcondition(context.Background(), action)
		if err != nil {
			t.Fatalf("local alias %s required an unavailable RPC: %v", action.ID, err)
		}
		if receipt.Observed["source_action"] != batchAction.ID || receipt.Observed["source_postcondition_hash"] != hash {
			t.Fatalf("local alias %s lost source identity: %+v", action.ID, receipt.Observed)
		}
	}
	batchEvidence.TransactionHash = "0x" + strings.Repeat("99", 32)
	if err := writePublicJSON(filepath.Join(dir, "public", "fleet-install-batch-1.json"), batchEvidence); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.verifyActionPostcondition(context.Background(), mirrorAction); err == nil {
		t.Fatal("local mirror alias accepted a transaction hash that differed from its authenticated batch receipt")
	}
	batchEvidence.TransactionHash = transactionHash
	if err := writePublicJSON(filepath.Join(dir, "public", "fleet-install-batch-1.json"), batchEvidence); err != nil {
		t.Fatal(err)
	}
	bindingEvidence.ClientSignature = "0x" + strings.Repeat("00", ed25519.SignatureSize)
	if err := writePublicJSON(filepath.Join(dir, "public", "fleet-1-member-1.binding.json"), bindingEvidence); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.verifyActionPostcondition(context.Background(), bindingAction); err == nil {
		t.Fatal("local member alias accepted a binding with a forged client signature")
	}
}

// Cover the full and final partial batch boundaries and every adjacent range
// mutation that could overlap or omit a fleet.
func TestFleetInstallActionRangeRequiresTheCanonicalPartition(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 23
	action := Action{Parameters: map[string]string{"first_fleet": "11", "last_fleet": "20", "generation": "1"}}
	firstFleet, lastFleet, err := fleetInstallActionRange(cfg, action, 2)
	if err != nil || firstFleet != 11 || lastFleet != 20 {
		t.Fatalf("canonical install range = %d..%d: %v", firstFleet, lastFleet, err)
	}
	finalAction := Action{Parameters: map[string]string{"first_fleet": "21", "last_fleet": "23", "generation": "1"}}
	firstFleet, lastFleet, err = fleetInstallActionRange(cfg, finalAction, 3)
	if err != nil || firstFleet != 21 || lastFleet != 23 {
		t.Fatalf("final install range = %d..%d: %v", firstFleet, lastFleet, err)
	}
	mutations := []Action{
		{Parameters: map[string]string{"first_fleet": "10", "last_fleet": "20", "generation": "1"}},
		{Parameters: map[string]string{"first_fleet": "11", "last_fleet": "19", "generation": "1"}},
		{Parameters: map[string]string{"first_fleet": "11", "last_fleet": "20", "generation": "2"}},
	}
	for _, mutation := range mutations {
		if _, _, err := fleetInstallActionRange(cfg, mutation, 2); err == nil {
			t.Errorf("noncanonical install range accepted: %+v", mutation.Parameters)
		}
	}
}

// Reject malformed, mixed, cyclic, oversized, and non-contiguous commitment
// concurrency groups before any executor can submit a transaction.
func TestFleetCommitmentParallelGroupsAreBoundedIndependentAndContiguous(t *testing.T) {
	first := Action{ID: "fleet.commitment.1", Kind: "substrate-extrinsic", Parameters: map[string]string{fleetCommitmentParallelGroupParameter: "install-1"}, DependsOn: []string{"barrier"}}
	second := Action{ID: "fleet.commitment.2", Kind: "substrate-extrinsic", Parameters: map[string]string{fleetCommitmentParallelGroupParameter: "install-1"}, DependsOn: []string{"barrier"}}
	actions := []Action{first, second}
	end, grouped, err := fleetCommitmentParallelRange(actions, 0)
	if err != nil || !grouped || end != 2 || validateFleetCommitmentParallelGroups(actions) != nil {
		t.Fatalf("valid parallel group rejected: end=%d grouped=%t error=%v", end, grouped, err)
	}

	internalDependency := append([]Action(nil), actions...)
	internalDependency[1].DependsOn = []string{first.ID}
	if err := validateFleetCommitmentParallelGroups(internalDependency); err == nil {
		t.Error("internally dependent parallel group was accepted")
	}
	mixed := append([]Action(nil), actions...)
	mixed[1].ID = "fleet.refresh.commitment.2"
	if err := validateFleetCommitmentParallelGroups(mixed); err == nil {
		t.Error("mixed-generation parallel group was accepted")
	}
	wrongKind := append([]Action(nil), actions...)
	wrongKind[0].Kind = "evm-read"
	if err := validateFleetCommitmentParallelGroups(wrongKind); err == nil {
		t.Error("non-native parallel action was accepted")
	}
	nonContiguous := []Action{first, {ID: "barrier"}, second}
	if err := validateFleetCommitmentParallelGroups(nonContiguous); err == nil {
		t.Error("non-contiguous duplicate group was accepted")
	}
	oversized := make([]Action, fleetRefreshBatchSize+1)
	for index := range oversized {
		oversized[index] = Action{ID: "fleet.commitment." + strconv.Itoa(index+1), Kind: "substrate-extrinsic", Parameters: map[string]string{fleetCommitmentParallelGroupParameter: "install-oversized"}}
	}
	if err := validateFleetCommitmentParallelGroups(oversized); err == nil {
		t.Error("oversized parallel group was accepted")
	}
}

// Renewal uses the same disjoint partition but must never accept generation 1.
func TestFleetRefreshActionRangeRequiresTheCanonicalPartition(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 23
	action := Action{Parameters: map[string]string{"first_fleet": "21", "last_fleet": "23", "generation": "2"}}
	firstFleet, lastFleet, err := fleetRefreshActionRange(cfg, action, 3)
	if err != nil || firstFleet != 21 || lastFleet != 23 {
		t.Fatalf("canonical refresh range = %d..%d: %v", firstFleet, lastFleet, err)
	}
	action.Parameters["generation"] = "1"
	if _, _, err := fleetRefreshActionRange(cfg, action, 3); err == nil {
		t.Fatal("generation-1 refresh range was accepted")
	}
}

// Deterministically reproduce the expiry failure that long sequential setup
// could otherwise encounter, plus cleaned and non-future adjacent states.
func TestFleetRefreshPriorStateRejectsExpiredOrInexactGeneration(t *testing.T) {
	binding := protocol.FleetBinding{
		ChainID: 945, Netuid: 521, Generation: 1, ValidFromEpoch: 7, ValidToEpoch: 38,
		Coordinator: [20]byte{1}, FleetID: [32]byte{2}, Hotkey: [32]byte{3},
		ClientID: [16]byte{4}, ClientKey: [32]byte{5}, CommitmentHash: [32]byte{6},
	}
	evidence := FleetBindingEvidence{Generation: 1, ValidFromEpoch: 7, ValidToEpoch: 38, UID: 42}
	record := stabi.STCoordinatorBindingRecord{
		FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientKey: binding.ClientKey,
		CommitmentHash: binding.CommitmentHash, Generation: 1, ValidFromEpoch: 7,
		ValidToEpoch: 38, Uid: 42,
	}
	if err := validateFleetRefreshPriorState(20, 21, evidence, record, binding); err != nil {
		t.Fatalf("valid prior state rejected: %v", err)
	}
	if err := validateFleetRefreshPriorState(38, 39, evidence, record, binding); err == nil {
		t.Fatal("replacement after prior expiry was accepted")
	}
	if err := validateFleetRefreshPriorState(21, 21, evidence, record, binding); err == nil {
		t.Fatal("non-future replacement was accepted")
	}
	record.Cleaned = true
	if err := validateFleetRefreshPriorState(20, 21, evidence, record, binding); err == nil {
		t.Fatal("cleaned prior generation was accepted")
	}
}

// Prove the generated Go tuple can encode both helper entry points and that a
// selector swap cannot silently reinterpret an install as a refresh.
func TestFleetBatcherABIEncodesInstallAndRefreshTuples(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		t.Fatal(err)
	}
	binding := stabi.STCoordinatorFleetBinding{
		ChainId: 945, Netuid: 521, Coordinator: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		FleetId: [32]byte{1}, Hotkey: [32]byte{2}, ClientId: [16]byte{3}, ClientKey: [32]byte{4},
		Generation: 1, ValidFromEpoch: 2, ValidToEpoch: 33, CommitmentHash: [32]byte{5},
	}
	fleets := []fleetBatcherFleetRefresh{{
		Hotkey: [32]byte{2}, CommitmentHash: [32]byte{5}, FinalizedBlock: 100, FinalizedBlockHash: [32]byte{6},
		Members: []fleetBatcherMemberRefresh{{
			PriorGeneration: 0, Binding: binding, RevokeSignature: []byte{},
			ClientSignature: make([]byte, 64), HotkeySignature: make([]byte, 64),
		}},
	}}
	installData, err := parsed.Pack("install", fleets)
	if err != nil {
		t.Fatal(err)
	}
	refreshData, err := parsed.Pack("refresh", fleets)
	if err != nil {
		t.Fatal(err)
	}
	if string(installData[:4]) != string(parsed.Methods["install"].ID) || string(refreshData[:4]) != string(parsed.Methods["refresh"].ID) || string(installData[:4]) == string(refreshData[:4]) {
		t.Fatal("fleet batch method selectors are absent or aliased")
	}
	if _, err := parsed.Methods["install"].Inputs.Unpack(installData[4:]); err != nil {
		t.Fatalf("install tuple does not round-trip through ABI: %v", err)
	}
}

// A partition must cover every fleet exactly once, whether carried or newly
// installed.
func TestFleetInstallPartitionsRejectGapsDuplicatesAndOutOfRange(t *testing.T) {
	valid := FleetInstallBatchEvidence{FirstFleet: 11, LastFleet: 13, InstalledFleets: []int{12, 13}, CarriedFleets: []int{11}}
	if err := validateFleetInstallPartitions(valid); err != nil {
		t.Fatalf("valid partition rejected: %v", err)
	}
	duplicate := valid
	duplicate.CarriedFleets = []int{11, 12}
	if err := validateFleetInstallPartitions(duplicate); err == nil {
		t.Fatal("duplicate fleet partition was accepted")
	}
	gap := valid
	gap.InstalledFleets = []int{13}
	if err := validateFleetInstallPartitions(gap); err == nil {
		t.Fatal("gapped fleet partition was accepted")
	}
	outOfRange := valid
	outOfRange.InstalledFleets = []int{12, 14}
	if err := validateFleetInstallPartitions(outOfRange); err == nil {
		t.Fatal("out-of-range fleet partition was accepted")
	}
}

// Prepared calldata is an exact crash-recovery artifact; changing its bytes,
// plan identity or range must invalidate it before transaction recovery.
func TestFleetInstallPreparedEvidenceBindsCalldataAndPlanIdentity(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	action := Action{ID: "fleet.install.batch.1", IntentHash: "0x" + strings.Repeat("22", 32)}
	data := []byte{1, 2, 3, 4}
	prepared := &fleetInstallPreparedEvidence{
		Schema: fleetInstallPreparedSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		Batch: 1, FirstFleet: 1, LastFleet: 10, Generation: 1, EffectiveEpoch: 2, ValidToEpoch: 33,
		Calldata: "0x01020304", CalldataHash: crypto.Keccak256Hash(data).Hex(),
		Fleets: []fleetInstallPreparedFleet{{Fleet: 1}},
	}
	decoded, err := validateFleetInstallPrepared(prepared, cfg, plan, action, 1, 1, 10)
	if err != nil || string(decoded) != string(data) {
		t.Fatalf("valid prepared evidence rejected: %x %v", decoded, err)
	}
	tampered := *prepared
	tampered.Calldata = "0x01020305"
	if _, err := validateFleetInstallPrepared(&tampered, cfg, plan, action, 1, 1, 10); err == nil {
		t.Fatal("tampered prepared calldata was accepted")
	}
	tampered = *prepared
	tampered.PlanHash = "0x" + strings.Repeat("33", 32)
	if _, err := validateFleetInstallPrepared(&tampered, cfg, plan, action, 1, 1, 10); err == nil {
		t.Fatal("cross-plan prepared calldata was accepted")
	}
}

// A plan revision may carry a verified batch, but its durable prepared
// calldata remains authenticated by the exact archived plan/action which
// created it. The active plan must never be substituted into that check.
func TestCarriedFleetBatchPreparationUsesExactArchivedPlan(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	sourceHash := "0x" + strings.Repeat("aa", 32)
	currentHash := "0x" + strings.Repeat("bb", 32)
	sourceIntent := "0x" + strings.Repeat("11", 32)
	currentIntent := "0x" + strings.Repeat("22", 32)
	target := "0x1234567890123456789012345678901234567890"
	coordinator := common.HexToAddress("0x2345678901234567890123456789012345678901")
	deployment := ContractDeployment{
		Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		InitialNonce: 13, ReserveSink: common.HexToAddress("0x3456789012345678901234567890123456789012"),
		SettlementVault:           common.HexToAddress("0x4567890123456789012345678901234567890123"),
		CoordinatorImplementation: common.HexToAddress("0x5678901234567890123456789012345678901234"), CoordinatorProxy: coordinator,
		GovernanceDrillImplementation: common.HexToAddress("0x6789012345678901234567890123456789012345"),
		PrecompileProbe:               common.HexToAddress("0x7890123456789012345678901234567890123456"),
	}
	for _, test := range []struct {
		name       string
		id         string
		generation string
	}{
		{name: "install", id: "fleet.install.batch.1", generation: "1"},
		{name: "refresh", id: "fleet.refresh.batch.1", generation: "2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sourceAction := Action{
				ID: test.id, Kind: "evm-transaction", Target: target, IntentHash: sourceIntent,
				Parameters: map[string]string{"first_fleet": "1", "last_fleet": "10", "generation": test.generation},
			}
			currentAction := sourceAction
			currentAction.IntentHash = currentIntent
			currentAction.AcceptedPriorIntentHashes = []string{sourceIntent}
			sourcePlan := &SetupPlan{
				PlanHash: sourceHash, DeploymentID: cfg.Config.Deployment.DeploymentID,
				ChainID: cfg.ChainID, Netuid: cfg.Netuid, Deployment: deployment, Actions: []Action{sourceAction},
			}
			currentPlan := &SetupPlan{
				PlanHash: currentHash, PriorPlanHashes: []string{sourceHash}, DeploymentID: sourcePlan.DeploymentID,
				ChainID: sourcePlan.ChainID, Netuid: sourcePlan.Netuid, Deployment: deployment, Actions: []Action{currentAction},
			}
			verified := JournalEntry{PlanHash: sourceHash, ActionID: sourceAction.ID, IntentHash: sourceIntent, Stage: StageVerified}
			archivedAction, err := exactCarriedFleetBatchSourceAction(cfg, currentPlan, sourcePlan, currentAction, verified)
			if err != nil || archivedAction.IntentHash != sourceIntent {
				t.Fatalf("exact archived action rejected: %+v %v", archivedAction, err)
			}
			data := []byte{1, 2, 3, 4}
			switch test.name {
			case "install":
				prepared := &fleetInstallPreparedEvidence{
					Schema: fleetInstallPreparedSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
					PlanHash: sourceHash, ActionID: sourceAction.ID, IntentHash: sourceIntent,
					Batch: 1, FirstFleet: 1, LastFleet: 10, Generation: 1, EffectiveEpoch: 2, ValidToEpoch: 33,
					Calldata: "0x01020304", CalldataHash: crypto.Keccak256Hash(data).Hex(), Fleets: []fleetInstallPreparedFleet{{Fleet: 1}},
				}
				if _, err := validateFleetInstallPrepared(prepared, cfg, sourcePlan, archivedAction, 1, 1, 10); err != nil {
					t.Fatalf("source-bound install preparation rejected: %v", err)
				}
				if _, err := validateFleetInstallPrepared(prepared, cfg, currentPlan, currentAction, 1, 1, 10); err == nil {
					t.Fatal("install preparation accepted the active revision in place of its source plan")
				}
			case "refresh":
				fleets := make([]fleetRefreshPreparedFleet, 10)
				for index := range fleets {
					fleets[index].Fleet = index + 1
				}
				prepared := &fleetRefreshPreparedEvidence{
					Schema: fleetRefreshPreparedSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
					PlanHash: sourceHash, ActionID: sourceAction.ID, IntentHash: sourceIntent,
					Batch: 1, FirstFleet: 1, LastFleet: 10, Generation: 2, EffectiveEpoch: 2, ValidToEpoch: 33,
					Calldata: "0x01020304", CalldataHash: crypto.Keccak256Hash(data).Hex(), Fleets: fleets,
				}
				if _, err := validateFleetRefreshPrepared(prepared, cfg, sourcePlan, archivedAction, 1, 1, 10); err != nil {
					t.Fatalf("source-bound refresh preparation rejected: %v", err)
				}
				if _, err := validateFleetRefreshPrepared(prepared, cfg, currentPlan, currentAction, 1, 1, 10); err == nil {
					t.Fatal("refresh preparation accepted the active revision in place of its source plan")
				}
			}
		})
	}
}

func TestCarriedFleetBatchSourceRejectsLineageAndIdentityDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	sourceHash := "0x" + strings.Repeat("aa", 32)
	sourceIntent := "0x" + strings.Repeat("11", 32)
	target := "0x1234567890123456789012345678901234567890"
	newFixture := func() (*SetupPlan, *SetupPlan, Action, JournalEntry) {
		sourceAction := Action{
			ID: "fleet.install.batch.1", Kind: "evm-transaction", Target: target, IntentHash: sourceIntent,
			Parameters: map[string]string{"first_fleet": "1", "last_fleet": "10", "generation": "1"},
		}
		currentAction := sourceAction
		currentAction.IntentHash = "0x" + strings.Repeat("22", 32)
		currentAction.AcceptedPriorIntentHashes = []string{sourceIntent}
		deployment := ContractDeployment{
			Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, InitialNonce: 13,
			ReserveSink:                   common.HexToAddress("0x3456789012345678901234567890123456789012"),
			SettlementVault:               common.HexToAddress("0x4567890123456789012345678901234567890123"),
			CoordinatorImplementation:     common.HexToAddress("0x5678901234567890123456789012345678901234"),
			CoordinatorProxy:              common.HexToAddress("0x2345678901234567890123456789012345678901"),
			GovernanceDrillImplementation: common.HexToAddress("0x6789012345678901234567890123456789012345"),
			PrecompileProbe:               common.HexToAddress("0x7890123456789012345678901234567890123456"),
		}
		source := &SetupPlan{PlanHash: sourceHash, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, Netuid: cfg.Netuid, Deployment: deployment, Actions: []Action{sourceAction}}
		current := &SetupPlan{PlanHash: "0x" + strings.Repeat("bb", 32), PriorPlanHashes: []string{sourceHash}, DeploymentID: source.DeploymentID, ChainID: source.ChainID, Netuid: source.Netuid, Deployment: deployment, Actions: []Action{currentAction}}
		entry := JournalEntry{PlanHash: sourceHash, ActionID: sourceAction.ID, IntentHash: sourceIntent, Stage: StageVerified}
		return current, source, currentAction, entry
	}
	tests := []struct {
		name   string
		mutate func(*SetupPlan, *SetupPlan, *Action, *JournalEntry)
	}{
		{name: "outside lineage", mutate: func(current, _ *SetupPlan, _ *Action, _ *JournalEntry) { current.PriorPlanHashes = nil }},
		{name: "unaccepted intent", mutate: func(_ *SetupPlan, _ *SetupPlan, current *Action, _ *JournalEntry) {
			current.AcceptedPriorIntentHashes = nil
		}},
		{name: "wrong journal intent", mutate: func(_ *SetupPlan, _ *SetupPlan, _ *Action, entry *JournalEntry) {
			entry.IntentHash = "0x" + strings.Repeat("33", 32)
		}},
		{name: "different target", mutate: func(_ *SetupPlan, source *SetupPlan, _ *Action, _ *JournalEntry) {
			source.Actions[0].Target = "0x9999999999999999999999999999999999999999"
		}},
		{name: "zero target", mutate: func(_ *SetupPlan, source *SetupPlan, current *Action, _ *JournalEntry) {
			source.Actions[0].Target = common.Address{}.Hex()
			current.Target = common.Address{}.Hex()
		}},
		{name: "changed range", mutate: func(_ *SetupPlan, source *SetupPlan, _ *Action, _ *JournalEntry) {
			source.Actions[0].Parameters = map[string]string{"first_fleet": "1", "last_fleet": "9", "generation": "1"}
		}},
		{name: "different chain", mutate: func(_ *SetupPlan, source *SetupPlan, _ *Action, _ *JournalEntry) { source.ChainID++ }},
		{name: "different deployment", mutate: func(_ *SetupPlan, source *SetupPlan, _ *Action, _ *JournalEntry) {
			source.Deployment.SettlementVault = common.HexToAddress("0x9999999999999999999999999999999999999999")
		}},
		{name: "duplicate exact action", mutate: func(_ *SetupPlan, source *SetupPlan, _ *Action, _ *JournalEntry) {
			source.Actions = append(source.Actions, source.Actions[0])
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current, source, action, entry := newFixture()
			test.mutate(current, source, &action, &entry)
			if _, err := exactCarriedFleetBatchSourceAction(cfg, current, source, action, entry); err == nil {
				t.Fatal("drifted carried fleet batch source was accepted")
			}
		})
	}
}

func TestCarriedFleetBatchExecutorLoadsHashAuthenticatedArchivedPlan(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	var sourceAction Action
	for _, action := range source.Actions {
		if action.ID == "fleet.install.batch.1" {
			sourceAction = action
			break
		}
	}
	if sourceAction.ID == "" {
		t.Fatal("built plan has no first fleet install batch")
	}
	current := *source
	current.PlanHash = "0x" + strings.Repeat("ff", 32)
	current.PriorPlanHashes = append(append([]string(nil), source.PriorPlanHashes...), source.PlanHash)
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "plans", stringsTrim0x(source.PlanHash)+".json")
	wire, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: cfg, stateDir: stateDir, plan: &current}
	entry := JournalEntry{PlanHash: source.PlanHash, ActionID: sourceAction.ID, IntentHash: sourceAction.IntentHash, Stage: StageVerified}
	archivedExecutor, archivedAction, err := executor.carriedFleetBatchSourceExecutor(sourceAction, entry)
	if err != nil || archivedExecutor.plan.PlanHash != source.PlanHash || archivedAction.IntentHash != sourceAction.IntentHash {
		t.Fatalf("hash-authenticated archived plan rejected: executor=%+v action=%+v err=%v", archivedExecutor, archivedAction, err)
	}
	tampered := *source
	tampered.Actions = append([]Action(nil), source.Actions...)
	tampered.Actions[0].Description += " tampered"
	wire, err = json.MarshalIndent(&tampered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executor.carriedFleetBatchSourceExecutor(sourceAction, entry); err == nil {
		t.Fatal("carried executor accepted a modified archived plan under its old hash")
	}
}

func TestFleetInstallHistoricalReplayRequiresExactVerifiedRefresh(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	install := actionByID(t, plan, "fleet.install.batch.4")
	refresh := actionByID(t, plan, "fleet.refresh.batch.4")
	adjacent := actionByID(t, plan, "fleet.refresh.batch.3")
	executor := &Executor{cfg: cfg, plan: plan, journal: &Journal{entries: []JournalEntry{
		{PlanHash: plan.PlanHash, ActionID: adjacent.ID, IntentHash: adjacent.IntentHash, Stage: StageVerified},
	}}}
	superseded, err := executor.fleetInstallBatchSuperseded(install)
	if err != nil || superseded {
		t.Fatalf("adjacent refresh superseded install batch 4: superseded=%t err=%v", superseded, err)
	}
	executor.journal.entries = append(executor.journal.entries, JournalEntry{
		PlanHash: plan.PlanHash, ActionID: refresh.ID, IntentHash: refresh.IntentHash, Stage: StageVerified,
	})
	superseded, err = executor.fleetInstallBatchSuperseded(install)
	if err != nil || !superseded {
		t.Fatalf("exact refresh did not supersede install batch 4: superseded=%t err=%v", superseded, err)
	}

	drifted := *plan
	drifted.Actions = append([]Action(nil), plan.Actions...)
	for index := range drifted.Actions {
		if drifted.Actions[index].ID == refresh.ID {
			drifted.Actions[index].Parameters = cloneStrings(drifted.Actions[index].Parameters)
			drifted.Actions[index].Parameters["first_fleet"] = "32"
			break
		}
	}
	executor.plan = &drifted
	if _, err := executor.fleetInstallBatchSuperseded(install); err == nil || !strings.Contains(err.Error(), "range") {
		t.Fatalf("mismatched refresh range was accepted: %v", err)
	}
	if _, err := executor.fleetInstallBatchSuperseded(Action{ID: "fleet.install.batch.04"}); err == nil {
		t.Fatal("noncanonical install batch id was accepted")
	}
}

// The built release plan must deploy/activate once, use twenty atomic install
// and refresh batches, and leave all head per-member actions read-only.
func TestBuildPlanBatchesEveryHeadFleetBeforeTopologyLaunch(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]Action{}
	positions := map[string]int{}
	for position, action := range plan.Actions {
		actions[action.ID] = action
		positions[action.ID] = position
	}
	wantBatches := cfg.Config.Topology.HeadFleets / fleetRefreshBatchSize
	for batch := 1; batch <= wantBatches; batch++ {
		installID := "fleet.install.batch." + strconv.Itoa(batch)
		refreshID := "fleet.refresh.batch." + strconv.Itoa(batch)
		if actions[installID].Kind != "evm-transaction" || actions[refreshID].Kind != "evm-transaction" {
			t.Fatalf("batch %d is absent: install=%+v refresh=%+v", batch, actions[installID], actions[refreshID])
		}
		first := (batch-1)*fleetRefreshBatchSize + 1
		last := first + fleetRefreshBatchSize - 1
		for fleetIndex := first; fleetIndex <= last; fleetIndex++ {
			installCommitment := actions["fleet.commitment."+strconv.Itoa(fleetIndex)]
			refreshCommitment := actions["fleet.refresh.commitment."+strconv.Itoa(fleetIndex)]
			if installCommitment.Parameters[fleetCommitmentParallelGroupParameter] != "install-"+strconv.Itoa(batch) || refreshCommitment.Parameters[fleetCommitmentParallelGroupParameter] != "refresh-"+strconv.Itoa(batch) {
				t.Fatalf("fleet %d commitment groups are invalid: install=%+v refresh=%+v", fleetIndex, installCommitment, refreshCommitment)
			}
			if !slices.Contains(actions[installID].DependsOn, installCommitment.ID) || !slices.Contains(actions[refreshID].DependsOn, refreshCommitment.ID) {
				t.Fatalf("batch %d does not depend on fleet %d commitments", batch, fleetIndex)
			}
			for _, dependency := range installCommitment.DependsOn {
				if strings.HasPrefix(dependency, "fleet.commitment.") {
					t.Fatalf("parallel install commitment %s depends on group member %s", installCommitment.ID, dependency)
				}
			}
			for _, dependency := range refreshCommitment.DependsOn {
				if strings.HasPrefix(dependency, "fleet.refresh.commitment.") {
					t.Fatalf("parallel refresh commitment %s depends on group member %s", refreshCommitment.ID, dependency)
				}
			}
		}
	}
	for fleetIndex := 1; fleetIndex <= cfg.Config.Topology.HeadFleets; fleetIndex++ {
		mirror := actions["fleet.mirror."+strconv.Itoa(fleetIndex)]
		if mirror.Kind != "evm-read" || !mirror.Spend.EVMGasWei.IsZero() || mirror.Parameters["batch_installed"] != "true" {
			t.Fatalf("head fleet %d mirror is not a strict batch proof: %+v", fleetIndex, mirror)
		}
		for memberIndex := 1; memberIndex <= cfg.Config.Topology.ClientsPerHeadFleet; memberIndex++ {
			id := "fleet.bind." + strconv.Itoa(fleetIndex) + "." + strconv.Itoa(memberIndex)
			binding := actions[id]
			if binding.Kind != "evm-read" || !binding.Spend.EVMGasWei.IsZero() || binding.Parameters["batch_installed"] != "true" {
				t.Fatalf("%s is not a strict batch proof: %+v", id, binding)
			}
		}
	}
	if positions["fleet.refresh.deploy-batcher"] >= positions["fleet.commitment.1"] || positions["fleet.refresh.oracle-await-active"] >= positions["fleet.install.batch.1"] || positions["fleet.refresh.oracle-await-restored"] >= positions["topology.launch"] {
		t.Fatal("fleet batcher activation/install/restore topology order is invalid")
	}
}

// A repeated upgrade changes the helper CREATE nonce/address. Every reference
// must move together; retaining even one old batch target would strand the
// live plan after a valid formal revision.
func TestCoordinatorUpgradeRebindMovesEveryFleetBatcherReference(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, secrets, plan.Deployment.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	oldBatcher := payloads.FleetBatcherAddress
	if err := configureCoordinatorUpgradeNonce(payloads, payloads.CoordinatorUpgrade.DeployerNonce+7); err != nil {
		t.Fatal(err)
	}
	if payloads.FleetBatcherAddress == oldBatcher {
		t.Fatal("test nonce did not change the predicted fleet batcher")
	}
	if err := rebindPlanCoordinatorUpgrade(plan, payloads); err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		switch {
		case action.ID == "fleet.refresh.deploy-batcher":
			if common.HexToAddress(action.Target) != payloads.FleetBatcherAddress || common.HexToAddress(action.Parameters["expected_created_address"]) != payloads.FleetBatcherAddress {
				t.Fatalf("fleet batcher deployment was not rebound: %+v", action)
			}
		case action.ID == "fleet.refresh.oracle-activate":
			if common.HexToAddress(action.Parameters["oracle"]) != payloads.FleetBatcherAddress {
				t.Fatalf("fleet batcher activation was not rebound: %+v", action)
			}
		case action.ID == "fleet.refresh.oracle-await-active", strings.HasPrefix(action.ID, "fleet.install.batch."), strings.HasPrefix(action.ID, "fleet.refresh.batch."):
			if common.HexToAddress(action.Target) != payloads.FleetBatcherAddress {
				t.Fatalf("fleet batcher reference %s retained %s, want %s", action.ID, action.Target, payloads.FleetBatcherAddress)
			}
		}
	}
}
