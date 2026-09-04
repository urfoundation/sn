package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Serves one immutable finalized view and rejects every unplanned RPC method.
type replacementBoundaryRPC struct {
	t                    *testing.T
	nonce                uint64
	block                uint64
	codes                map[common.Address][]byte
	activeImplementation common.Address
}

func (self *replacementBoundaryRPC) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
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
	switch call.Method {
	case "eth_getBlockByNumber":
		response["result"] = map[string]any{"number": fmt.Sprintf("0x%x", self.block), "hash": "0x" + strings.Repeat("ab", 32)}
	case "eth_getTransactionCount":
		response["result"] = fmt.Sprintf("0x%x", self.nonce)
	case "eth_getCode":
		var address string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &address) != nil || !common.IsHexAddress(address) {
			response["error"] = map[string]any{"code": -32602, "message": "malformed code request"}
			break
		}
		response["result"] = "0x" + common.Bytes2Hex(self.codes[common.HexToAddress(address)])
	case "eth_getStorageAt":
		if len(call.Params) != 3 {
			response["error"] = map[string]any{"code": -32602, "message": "malformed storage request"}
			break
		}
		response["result"] = "0x" + common.Bytes2Hex(common.LeftPadBytes(self.activeImplementation.Bytes(), 32))
	default:
		response["error"] = map[string]any{"code": -32601, "message": "unexpected method"}
	}
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		self.t.Errorf("encode replacement-boundary RPC response: %v", err)
	}
}

func TestPersistedReplacementProbeObserverAuthenticatesEveryCrashBoundary(t *testing.T) {
	cfg, payloads, retained, baseline, _ := replacementPrecompileProbeFixture(t)
	secrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := buildDeploymentPayloads(cfg, secrets, payloads.Manifest.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	facts := *testSetupFacts()
	facts.DeployerNonce = payloads.Manifest.InitialNonce
	prior, err := buildPlan(cfg, &facts, publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := rebindPlanDeployment(prior, retained); err != nil {
		t.Fatal(err)
	}
	prior.CoordinatorUpgradeBaseline = baseline
	if err := rebindPlanCoordinatorUpgrade(prior, payloads); err != nil {
		t.Fatal(err)
	}
	ancestorHash := "0x" + strings.Repeat("71", 32)
	prior.PriorPlanHashes = []string{ancestorHash}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, prior.Deployment); err != nil {
		t.Fatal(err)
	}

	baseCodes := map[common.Address][]byte{}
	for _, address := range contractDeploymentAddresses(prior.Deployment) {
		baseCodes[address] = append([]byte(nil), initial.ExpectedRuntime[address]...)
	}
	baseCodes[prior.Deployment.PrecompileProbe] = []byte{1}
	baseCodes[common.HexToAddress(baseline.ActiveImplementation)] = append([]byte(nil), initial.ExpectedRuntime[common.HexToAddress(baseline.ActiveImplementation)]...)
	actionIDs := []string{"precompile.probe-deploy", "evm.coordinator-upgrade-implementation", "evm.coordinator-upgrade-activate", "fleet.refresh.deploy-batcher"}
	exactEntries := func(ids ...string) []JournalEntry {
		entries := make([]JournalEntry, 0, len(actionIDs)+len(ids))
		for index, id := range actionIDs {
			entries = append(entries, JournalEntry{PlanHash: ancestorHash, ActionID: id, IntentHash: fmt.Sprintf("0x%064x", index+1), Stage: StageVerified})
		}
		for _, id := range ids {
			action := actionByID(t, prior, id)
			entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified})
		}
		return entries
	}
	type boundary struct {
		name      string
		nonce     uint64
		verified  []string
		activated bool
		mutate    func(*replacementBoundaryRPC, *SetupPlan, *[]JournalEntry)
		wantError string
	}
	boundaries := []boundary{
		{name: "pre-probe", nonce: payloads.PrecompileProbeNonce},
		{name: "probe complete", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verified: actionIDs[:1]},
		{name: "implementation complete", nonce: payloads.FleetBatcherNonce, verified: actionIDs[:2]},
		{name: "implementation activated", nonce: payloads.FleetBatcherNonce, verified: actionIDs[:3], activated: true},
		{name: "batcher complete", nonce: payloads.FleetBatcherNonce + 1, verified: actionIDs, activated: true},
		{name: "pre-probe durable journal", nonce: payloads.PrecompileProbeNonce, mutate: func(_ *replacementBoundaryRPC, plan *SetupPlan, entries *[]JournalEntry) {
			action := actionByID(t, plan, "precompile.probe-deploy")
			*entries = append(*entries, JournalEntry{PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, TransactionHash: "0x" + strings.Repeat("75", 32)})
		}, wantError: "unconsumed replacement precompile probe"},
		{name: "advanced without probe proof", nonce: payloads.CoordinatorUpgrade.DeployerNonce, wantError: "verification"},
		{name: "wrong current intent", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verified: actionIDs[:1], mutate: func(_ *replacementBoundaryRPC, plan *SetupPlan, entries *[]JournalEntry) {
			(*entries)[len(*entries)-1].IntentHash = "0x" + strings.Repeat("73", 32)
			(*entries)[len(*entries)-1].PlanHash = plan.PlanHash
		}, wantError: "unauthenticated current-plan"},
		{name: "empty consumed probe", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verified: actionIDs[:1], mutate: func(rpc *replacementBoundaryRPC, _ *SetupPlan, _ *[]JournalEntry) {
			delete(rpc.codes, payloads.PrecompileProbeAddress)
		}, wantError: "replacement precompile probe runtime"},
		{name: "wrong consumed probe", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verified: actionIDs[:1], mutate: func(rpc *replacementBoundaryRPC, _ *SetupPlan, _ *[]JournalEntry) {
			rpc.codes[payloads.PrecompileProbeAddress] = []byte{1}
		}, wantError: "replacement precompile probe runtime"},
		{name: "future CREATE hole", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verified: actionIDs[:1], mutate: func(rpc *replacementBoundaryRPC, _ *SetupPlan, _ *[]JournalEntry) {
			rpc.codes[payloads.CoordinatorUpgrade.Implementation] = append([]byte(nil), payloads.ExpectedRuntime[payloads.CoordinatorUpgrade.Implementation]...)
		}, wantError: "unconsumed coordinator implementation"},
		{name: "activation before implementation", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verified: []string{"precompile.probe-deploy", "evm.coordinator-upgrade-activate"}, activated: true, wantError: "activation dependencies"},
		{name: "activation finalized but unverified", nonce: payloads.FleetBatcherNonce, verified: actionIDs[:2], mutate: func(_ *replacementBoundaryRPC, plan *SetupPlan, entries *[]JournalEntry) {
			action := actionByID(t, plan, "evm.coordinator-upgrade-activate")
			*entries = append(*entries, JournalEntry{PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: "0x" + strings.Repeat("76", 32)})
		}, wantError: "activation has no exact verified"},
		{name: "batcher without activation", nonce: payloads.FleetBatcherNonce + 1, verified: []string{"precompile.probe-deploy", "evm.coordinator-upgrade-implementation", "fleet.refresh.deploy-batcher"}, wantError: "activation dependencies"},
		{name: "changed release", nonce: payloads.PrecompileProbeNonce, mutate: func(_ *replacementBoundaryRPC, plan *SetupPlan, _ *[]JournalEntry) {
			plan.CoordinatorUpgradeBaseline.ReleaseDeploymentHash = "0x" + strings.Repeat("74", 32)
		}, wantError: "release deployment"},
	}
	for _, test := range boundaries {
		plan := *prior
		entries := exactEntries(test.verified...)
		codes := make(map[common.Address][]byte, len(baseCodes)+3)
		for address, code := range baseCodes {
			codes[address] = append([]byte(nil), code...)
		}
		for _, create := range []struct {
			nonce   uint64
			address common.Address
			code    []byte
		}{
			{payloads.PrecompileProbeNonce, payloads.PrecompileProbeAddress, payloads.ExpectedRuntime[payloads.PrecompileProbeAddress]},
			{payloads.CoordinatorUpgrade.DeployerNonce, payloads.CoordinatorUpgrade.Implementation, payloads.ExpectedRuntime[payloads.CoordinatorUpgrade.Implementation]},
			{payloads.FleetBatcherNonce, payloads.FleetBatcherAddress, payloads.FleetBatcherRuntime},
		} {
			if test.nonce > create.nonce {
				codes[create.address] = append([]byte(nil), create.code...)
			}
		}
		active := common.HexToAddress(baseline.ActiveImplementation)
		if test.activated {
			active = payloads.CoordinatorUpgrade.Implementation
		}
		rpcFixture := &replacementBoundaryRPC{t: t, nonce: test.nonce, block: 200, codes: codes, activeImplementation: active}
		if test.mutate != nil {
			test.mutate(rpcFixture, &plan, &entries)
		}
		server := httptest.NewServer(rpcFixture)
		cfg.OperationalRPCMode = rpcModePrivateAuthority
		cfg.OperationalEVM = server.URL
		current := *testSetupFacts()
		current.DeployerNonce = test.nonce
		current.EVMFinalizedBlock = 150
		migration, observeErr := observeCoordinatorUpgradeMigration(context.Background(), cfg, stateDir, &plan, &current, entries, secrets)
		server.Close()
		if test.wantError == "" {
			if observeErr != nil || migration == nil || migration.Baseline != baseline || migration.Upgrade != prior.CoordinatorUpgrade {
				t.Errorf("%s boundary rejected or changed: migration=%+v error=%v", test.name, migration, observeErr)
			}
			continue
		}
		if observeErr == nil || !strings.Contains(observeErr.Error(), test.wantError) {
			t.Errorf("%s boundary error=%v, want %q", test.name, observeErr, test.wantError)
		}
	}
}
