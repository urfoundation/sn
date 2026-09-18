package main

// Provisional assurance does not change the exact operational route already
// approved by a stopped deployment. All transport choices remain private.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The real plan, provenance writer and proxy builder need no live supervisor
// to retain a previously approved owned route during provisional startup.
func TestOwnedRpcProvisionalResumePreservesStoppedPlanAndPrivateRoutes(t *testing.T) {
	source := ownedRPCSourceConfigTest(t)
	cfg, err := prepareOwnedRPCConfiguration(source, "192.168.50.20:9944")
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	provisional, _, stateDir, _ := provisionalResumeTestContext(t)
	if err := writeRunInputs(cfg, stateDir, plan, roles); err != nil {
		t.Fatal(err)
	}
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	planBytes, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	source.provisionalResume = provisional.provisionalResume
	resumed, err := prepareOwnedRPCConfiguration(source, cfg.ownedRPCAuthority)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"resume", "scenario"} {
		args := []string{command, "--provisional-resume", "--apply", "--plan-hash", plan.PlanHash, "--owned-rpc-authority", cfg.ownedRPCAuthority}
		if command == "scenario" {
			args = append(args, "--name", "release-candidate")
		}
		_, options, err := parseCLI(args)
		if err != nil {
			t.Fatalf("exact provisional %s could not retain its owned route: %v", command, err)
		}
		if err := prepareProvisionalResume(t.Context(), resumed, stateDir, command, options, plan); err != nil {
			t.Fatal(err)
		}
		if err := validateOwnedRPCPlan(resumed, plan); err != nil {
			t.Fatalf("provisional provenance invalidated approved routing: %v", err)
		}
	}
	loaded, err := loadPersistedPlan(resumed, stateDir)
	if err != nil || loaded.PlanHash != plan.PlanHash {
		t.Fatalf("provisional routing needed a replacement plan: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || !bytes.Equal(planBytes, after) {
		t.Fatal("provisional startup rewrote its approved plan", err)
	}
	afterTree := validatorNamespaceTreeSnapshot(t, stateDir)
	for path, value := range before {
		if current := afterTree[path]; value != current {
			t.Fatalf("provenance changed retained input %s", path)
		}
	}
	if resumed.Config != source.Config || resumed.Public != source.Public || resumed.ConfigHash != source.ConfigHash || resumed.PolicyHash != source.PolicyHash || resumed.Authority != source.Authority || !ownedRPCOnly(resumed) || independentRPCRequired(resumed) {
		t.Fatal("provisional route changed canonical identity, custody or assurance")
	}
	if verificationEVMEndpoint(resumed) != cfg.OperationalEVM || verificationSubstrateEndpoint(resumed) != cfg.OperationalSubstrate || configuredEVMRequestsPerMinute(resumed, resumed.OperationalEVM) != 0 || effectiveAdversaryConfig(resumed).MaximumRPCRequestsPerSec != 0 {
		t.Fatal("provisional owned route gained a public endpoint or request ceiling")
	}
	for _, endpoint := range []string{source.Public.Chain.EVMPublicReadEndpoint, source.Public.Chain.SubstratePublicReadEndpoint, "http://192.168.50.21:9944"} {
		if validateOwnedRPCDialEndpoint(resumed, endpoint) == nil {
			t.Fatalf("provisional route admitted an unapproved endpoint %s", endpoint)
		}
	}
	runtime, err := campaignRPCConfig(resumed)
	if err != nil || validateCampaignRPCTransport(resumed, runtime) != nil || runtime.OperationalSubstrate != cfg.OperationalSubstrate || runtime.OperationalEVM != "http://"+campaignEVMAuthority() {
		t.Fatal("provisional campaign lost its exact owned egress", err)
	}
	specs, err := buildServerSpecs(resumed, stateDir, map[string]string{"sim-testnet": "/fixture/sim-testnet", connectServerBinaryName: "/fixture/connect"})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]string{publicEVMEgressProcessID: cfg.ownedRPCAuthority, workloadSubstrateProcessID: cfg.ownedRPCAuthority, workloadRPCProxyProcessID: campaignEVMAuthority()}
	for _, spec := range specs {
		upstream, found := wanted[spec.ID]
		if !found {
			continue
		}
		matched := false
		for _, arg := range spec.Args {
			matched = matched || arg == "--upstream="+upstream
			if strings.HasPrefix(arg, "--tls-server-name=") || strings.HasPrefix(arg, "--maximum-requests-per-minute=") {
				t.Fatalf("provisional proxy restored public routing or pacing: %v", spec.Args)
			}
		}
		if !matched {
			t.Fatalf("proxy %s lost its exact owned upstream", spec.ID)
		}
		delete(wanted, spec.ID)
	}
	if len(wanted) != 0 {
		t.Fatal("provisional startup omitted an owned proxy")
	}
	result := &ScenarioResult{Result: "pass"}
	applyProvisionalScenarioProvenance(resumed, result)
	if !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance {
		t.Fatal("owned transport promoted provisional execution into final acceptance")
	}
	for _, mutate := range []func(*ResolvedConfig){
		func(value *ResolvedConfig) { value.ownedRPCAuthority = "192.168.50.21:9944" },
		func(value *ResolvedConfig) { value.OperationalEVM = source.Public.Chain.EVMPublicReadEndpoint },
		func(value *ResolvedConfig) { value.provisionalRPCAuthority = cfg.ownedRPCAuthority },
	} {
		changed := *resumed
		mutate(&changed)
		if validateOwnedRPCPlan(&changed, plan) == nil {
			t.Fatal("provisional execution accepted changed route authority")
		}
	}
	encoded, err := json.Marshal(plan)
	if err != nil || !bytes.Equal(approval, encoded) {
		t.Fatal("route checks changed in-memory approval", err)
	}
}
