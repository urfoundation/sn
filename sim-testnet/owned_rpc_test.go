package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ownedRPCSourceConfigTest(t *testing.T) *ResolvedConfig {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.LaunchInputs.PublicSubstrateRPCOverride = "wss://independent-native.example"
	cfg.Config.LaunchInputs.PublicEVMRPCOverride = "https://independent-evm.example"
	cfg.Public.Chain.SubstratePublicReadEndpoint = cfg.Config.LaunchInputs.PublicSubstrateRPCOverride
	cfg.Public.Chain.EVMPublicReadEndpoint = cfg.Config.LaunchInputs.PublicEVMRPCOverride
	var err error
	cfg.OperationalSubstrate, cfg.OperationalEVM, cfg.OperationalRPCMode, err = resolveOperationalRPCs(cfg.Authority, cfg.Config.LaunchInputs.PublicSubstrateRPCOverride, cfg.Config.LaunchInputs.PublicEVMRPCOverride)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Use the real proxy/worker spec builder and endpoint quota selection. No
// retained supervisor or local listener is necessary for planning or setup.
func TestOwnedRPCStrictRoutePreservesConsentAndUnlimitsOwnedTransport(t *testing.T) {
	source := ownedRPCSourceConfigTest(t)
	before, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := prepareOwnedRPCConfiguration(source, "192.168.1.162:9944")
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(source)
	if err != nil || string(before) != string(after) || cfg.ConfigHash != source.ConfigHash || cfg.PolicyHash != source.PolicyHash || cfg.Authority != source.Authority || cfg.Config != source.Config || cfg.Public != source.Public {
		t.Fatal("strict owned selection changed authenticated configuration, public comparison, or custody")
	}
	if cfg.OperationalSubstrate != "ws://192.168.1.162:9944" || cfg.OperationalEVM != "http://192.168.1.162:9944" || !independentRPCRequired(cfg) || provisionalResumeEnabled(cfg) {
		t.Fatal("owned route did not retain strict independent admission")
	}
	if configuredEVMRequestsPerMinute(cfg, cfg.OperationalEVM) != 0 || configuredEVMRequestsPerMinute(cfg, cfg.Public.Chain.EVMPublicReadEndpoint) != 40 {
		t.Fatal("owned operational quota or independent public quota is incorrect")
	}
	stopped, err := selectReadOnlyRPCConfig(cfg, false)
	if err != nil || stopped != cfg || stopped.OperationalEVM != "http://192.168.1.162:9944" {
		t.Fatal("stopped topology was routed through its absent proxy")
	}
	runtime, err := campaignRPCConfig(cfg)
	if err != nil || validateCampaignRPCTransport(cfg, runtime) != nil || runtime.OperationalEVM != "http://"+campaignEVMAuthority() || runtime.OperationalSubstrate != cfg.OperationalSubstrate || runtime.Public.Chain.EVMPublicReadEndpoint != cfg.Public.Chain.EVMPublicReadEndpoint || configuredEVMRequestsPerMinute(runtime, runtime.OperationalEVM) != 0 {
		t.Fatalf("campaign derivative changed the owned route or independent reader: %v", err)
	}
	specs, err := buildServerSpecs(cfg, t.TempDir(), map[string]string{"sim-testnet": "/fixture/sim-testnet", connectServerBinaryName: "/fixture/connect"})
	if err != nil {
		t.Fatal(err)
	}
	wantUpstream := map[string]string{publicEVMEgressProcessID: "192.168.1.162:9944", workloadSubstrateProcessID: "192.168.1.162:9944", workloadRPCProxyProcessID: campaignEVMAuthority()}
	for _, spec := range specs {
		if want, ok := wantUpstream[spec.ID]; ok {
			found := false
			for _, arg := range spec.Args {
				found = found || arg == "--upstream="+want
				if strings.HasPrefix(arg, "--maximum-requests-per-minute=") || strings.HasPrefix(arg, "--tls-server-name=") {
					t.Fatalf("owned proxy %s retained a provider quota/TLS route: %v", spec.ID, spec.Args)
				}
			}
			if !found {
				t.Fatalf("owned proxy %s did not route to %s", spec.ID, want)
			}
			delete(wantUpstream, spec.ID)
		} else if spec.Env["BRINGYOUR_SUBTENSOR_HOSTNAME"] != workloadRPCAuthority() {
			t.Fatalf("worker %s bypassed the owned workload proxy", spec.ID)
		}
	}
	if len(wantUpstream) != 0 || validatorPollSeconds(cfg) >= 60 || claimPollSeconds(cfg) >= 60 {
		t.Fatal("strict owned runtime retained a public-provider proxy or polling policy")
	}
	for name, mutate := range map[string]func(*ResolvedConfig){
		"operational EVM":    func(c *ResolvedConfig) { c.OperationalEVM = source.OperationalEVM },
		"operational native": func(c *ResolvedConfig) { c.OperationalSubstrate = source.OperationalSubstrate },
		"assurance":          func(c *ResolvedConfig) { c.OperationalRPCMode = rpcModePublicOverride },
		"authority":          func(c *ResolvedConfig) { c.ownedRPCAuthority = "192.168.1.163:9944" },
	} {
		t.Run(name, func(t *testing.T) {
			tampered := *cfg
			mutate(&tampered)
			if err := validateExecutionRPCConfiguration(&tampered); err == nil {
				t.Fatal("strict owned endpoint substitution was accepted")
			}
		})
	}
}

// An invocation route preserves consent/config hashes but must replace plan
// approval. Reopening with an omitted or changed route is a revision request;
// a transaction manager also rejects it before making any connection.
func TestOwnedRPCPlanApprovalBindsExactRouteWithoutChangingCustody(t *testing.T) {
	source := ownedRPCSourceConfigTest(t)
	roles, err := derivePublicRoles(source)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(source, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := prepareOwnedRPCConfiguration(source, "192.168.1.162:9944")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if plan.OwnedRPCAuthority != cfg.ownedRPCAuthority || prior.ConfigHash != plan.ConfigHash || prior.PolicyHash != plan.PolicyHash || prior.PlanHash == plan.PlanHash || prior.ResolvedInputsHash == plan.ResolvedInputsHash || !reflect.DeepEqual(prior.Roles, plan.Roles) || !reflect.DeepEqual(prior.Actions, plan.Actions) || prior.MaximumSpend != plan.MaximumSpend || prior.Limits != plan.Limits {
		t.Fatal("owned route was unbound or changed source actions, custody, or economic limits")
	}
	if err := validateOwnedRPCPlan(cfg, plan); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []*ResolvedConfig{source, func() *ResolvedConfig {
		other, err := prepareOwnedRPCConfiguration(source, "192.168.1.163:9944")
		if err != nil {
			t.Fatal(err)
		}
		return other
	}()} {
		if err := validateOwnedRPCPlan(wrong, plan); err == nil {
			t.Fatal("an omitted or changed owned route reused the exact approval")
		}
		if _, err := NewExecutor(t.Context(), wrong, t.TempDir(), plan, nil, nil); err == nil || !strings.Contains(err.Error(), "owned RPC authority differs") {
			t.Fatalf("transaction manager did not reject route substitution before dialing: %v", err)
		}
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPersistedPlan(source, stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("omitted route was not an explicit plan revision: %v", err)
	}
	if reopened, err := loadPersistedPlan(cfg, stateDir); err != nil || reopened.PlanHash != plan.PlanHash {
		t.Fatalf("exact owned approval did not reopen: %v", err)
	}
}

func TestOwnedRPCCLIRejectsUnownedAndProvisionalRoutes(t *testing.T) {
	for _, authority := range []string{"127.0.0.1:9944", "0.0.0.0:9944", "8.8.8.8:9944", "node.example:9944", "192.168.1.162", "192.168.1.162:09944", "http://192.168.1.162:9944", "192.168.1.162:65536"} {
		if _, _, err := parseCLI([]string{"doctor", "--owned-rpc-authority", authority}); err == nil {
			t.Fatalf("unowned or ambiguous authority accepted: %s", authority)
		}
	}
	for _, args := range [][]string{
		{"resume", "--provisional-resume"}, {"release-lock"}, {"stop"}, {"inspect", "--manifest", "/fixture/public.json"},
	} {
		if _, _, err := parseCLI(append(args, "--owned-rpc-authority", "192.168.1.162:9944")); err == nil {
			t.Fatalf("unsupported strict route options accepted: %v", args)
		}
	}
	if _, options, err := parseCLI([]string{"fleet-renew", "--renewal-valid-from-epoch", "318", "--owned-rpc-authority", "192.168.1.162:9944"}); err != nil || options.OwnedRPCAuthority != "192.168.1.162:9944" {
		t.Fatalf("strict renewal could not select owned RPC: %v", err)
	}
}
