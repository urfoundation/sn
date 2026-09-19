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
	for _, command := range []string{"setup", "resume", "scenario"} {
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

func TestOwnedRPCProvisionalSetupRequiresExactApprovalAndKeepsUnpacedRoute(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 1)
	self := fixture.executor
	options := cliOptions{Apply: true, ProvisionalResume: true, PlanHash: self.plan.PlanHash, OwnedRPCAuthority: self.cfg.ownedRPCAuthority}
	if err := validateOwnedRPCOptions("setup", options); err != nil {
		t.Fatal("approved provisional setup could not retain its owned route", err)
	}
	if _, _, err := parseCLI([]string{"setup", "--provisional-resume", "--apply", "--plan-hash", options.PlanHash, "--owned-rpc-authority", options.OwnedRPCAuthority}); err != nil {
		t.Fatal("the real setup command rejected its approved owned route", err)
	}
	if err := validateOwnedRPCPlan(self.cfg, self.plan); err != nil || configuredEVMRequestsPerMinute(self.cfg, self.cfg.OperationalEVM) != 0 ||
		verificationEVMEndpoint(self.cfg) != self.cfg.OperationalEVM || verificationSubstrateEndpoint(self.cfg) != self.cfg.OperationalSubstrate {
		t.Fatal("setup changed approved route identity or imposed a request limit", err)
	}
	for _, fault := range []string{"no-apply", "no-plan", "malformed-plan", "detach", "other-route", "manifest", "launch", "wrong-approved-route"} {
		changed := options
		command := "setup"
		switch fault {
		case "no-apply":
			changed.Apply = false
		case "no-plan":
			changed.PlanHash = ""
		case "malformed-plan":
			changed.PlanHash = "approximate"
		case "detach":
			changed.Detach = true
		case "other-route":
			changed.ProvisionalRPCAuthority = options.OwnedRPCAuthority
		case "manifest":
			changed.Manifest = "synthetic-manifest.json"
		case "launch":
			command = "launch"
		case "wrong-approved-route":
			plan := *self.plan
			plan.OwnedRPCAuthority = ""
			if validateOwnedRPCPlan(self.cfg, &plan) == nil {
				t.Fatal("setup accepted another plan's route")
			}
			continue
		}
		if err := validateOwnedRPCOptions(command, changed); err == nil {
			t.Fatal("owned setup accepted an unapproved invocation", fault)
		}
	}
}
