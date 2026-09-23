// Standalone preparation borrows its authenticated command's RPC and journal
// ownership even when the retained campaign topology is deliberately stopped.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
)

// The real local HTTP client proves which route is used; no fixed live egress
// listener, native node, signer secret, or transaction broadcast is involved.
func precompilePreparationOwnerFixture(t *testing.T) (*ResolvedConfig, *Executor, *atomic.Int64) {
	t.Helper()
	cfg, plan, dir, _ := provisionalResumeTestContext(t)
	cfg.provisionalResume.Record = &provisionalResumeRecord{Provisional: true, Command: "scenario", Scenario: precompilePreparationScenario, PlanHash: plan.PlanHash}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	calls := new(atomic.Int64)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var request struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Method != "eth_chainId" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.Id, "result": "0x3b1"})
	}))
	t.Cleanup(endpoint.Close)
	client, err := ethclient.Dial(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	cfg.OperationalEVM = endpoint.URL
	executor := &Executor{cfg: cfg, auditAuthorizedConfig: cfg, stateDir: dir, journal: journal, plan: plan,
		roles: &RoleSecrets{}, substrate: &SubstrateManager{}, deployer: &EvmTxManager{client: client}, payloads: &DeploymentPayloads{}}
	return cfg, executor, calls
}

// A stopped proxy is not a prerequisite of this repair. The exact command
// manager remains usable after the preparation owner is returned to its caller.
func TestPrecompilePreparationRetainsDirectOwnerWithStoppedTopology(t *testing.T) {
	cfg, executor, calls := precompilePreparationOwnerFixture(t)
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", Processes: []ProcessState{{ID: publicEVMEgressProcessID, Role: "dependency-rpc-proxy", ExitError: "synthetic controlled stop"}}}
	if err := writePublicJSON(filepath.Join(executor.stateDir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	if active, err := supervisedCampaignEgressActive(t.Context(), executor.stateDir); err != nil || active {
		t.Fatalf("fixture topology is not stopped: %t %v", active, err)
	}
	for range 2 {
		prepared, err := precompilePreparationOwner(cfg, executor.stateDir, executor.journal, executor)
		if err != nil || prepared != executor || prepared.substrate != executor.substrate || prepared.deployer != executor.deployer || prepared.payloads != executor.payloads {
			t.Fatalf("repair replaced authenticated owners: %v", err)
		}
		chainId, err := prepared.deployer.client.ChainID(t.Context())
		if err != nil || chainId.Uint64() != testnetChainID {
			t.Fatalf("repair lost its original direct RPC: %v %v", chainId, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("direct manager calls=%d, want2", calls.Load())
	}
}

// Borrowing does not admit another journal, plan, route, or independently built
// approval owner even when it presents the same apparent runtime purpose.
func TestPrecompilePreparationRejectsOwnerAndRouteDrift(t *testing.T) {
	cfg, original, _ := precompilePreparationOwnerFixture(t)
	for _, fault := range []string{"journal", "directory", "approval", "plan", "route", "native", "evm"} {
		executor := *original
		switch fault {
		case "journal":
			executor.journal = &Journal{}
		case "directory":
			executor.stateDir = filepath.Join(original.stateDir, "other")
		case "approval":
			copied := *cfg
			executor.auditAuthorizedConfig = &copied
		case "plan":
			plan := *original.plan
			plan.PlanHash = "changed-plan"
			executor.plan = &plan
		case "route":
			runtime := *cfg
			runtime.OperationalEVM = "http://unapproved-rpc.example"
			executor.cfg = &runtime
		case "native":
			executor.substrate = nil
		case "evm":
			executor.deployer = nil
		}
		if _, err := precompilePreparationOwner(cfg, original.stateDir, original.journal, &executor); err == nil {
			t.Fatalf("changed %s owner passed", fault)
		}
	}
}

// Release still derives its non-faulted shared egress route and is explicitly
// eligible to restart retained topology; standalone preparation is neither.
func TestPrecompilePreparationDoesNotChangeReleaseEgressRequirement(t *testing.T) {
	cfg, executor, _ := precompilePreparationOwnerFixture(t)
	if provisionalRetainedStartupAllowed(cfg.provisionalResume.Record) {
		t.Fatal("standalone preparation acquired topology restart authority")
	}
	runtime, err := campaignRPCConfig(cfg)
	if err != nil || runtime.OperationalEVM != "http://"+campaignEVMAuthority() || runtime.OperationalEVM == cfg.OperationalEVM {
		t.Fatalf("release lost its campaign egress handoff: %v", err)
	}
	if err := validateCampaignRPCTransport(cfg, runtime); err != nil {
		t.Fatal(err)
	}
	release := *cfg.provisionalResume.Record
	release.Scenario = "release-1.0"
	if !provisionalRetainedStartupAllowed(&release) {
		t.Fatal("release lost exact retained topology restart scope")
	}
	if executor.cfg != cfg {
		t.Fatal("release transport derivative changed the preparation owner")
	}
}
