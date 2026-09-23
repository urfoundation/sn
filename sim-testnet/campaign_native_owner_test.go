// The real campaign handoff selects new reader ownership only for authenticated
// retained metadata; transport and process lifecycle stay independently scoped.
package main

import (
	"context"
	"errors"
	"maps"
	"testing"
)

// Represents the exact executor created by runMutation's retainedPlanResume
// branch, with a real journal and no node connections or transaction managers.
func retainedCampaignOwnerFixture(t *testing.T) (*ResolvedConfig, *Executor) {
	t.Helper()
	cfg, plan, dir, _ := provisionalResumeTestContext(t)
	cfg.provisionalResume.Record = &provisionalResumeRecord{Command: "scenario", Scenario: "release-1.0", Provisional: true, PlanHash: plan.PlanHash}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	return cfg, &Executor{cfg: cfg, stateDir: dir, plan: plan, journal: journal, roles: &RoleSecrets{Schema: "synthetic-retained-roles", DeploymentID: plan.DeploymentID, EVM: map[string]EVMRoleSecret{"synthetic-deployer": {Address: "synthetic-address"}}}}
}

// This is the actual constructor handoff, substituting only its final dial
// boundary. The newly owned reader retains the approved plan and proxy route.
func TestRetainedCampaignNativeOwnerAcquiresMissingReader(t *testing.T) {
	cfg, local := retainedCampaignOwnerFixture(t)
	reloadedRoles := *local.roles
	reloadedRoles.EVM = maps.Clone(local.roles.EVM)
	opened := 0
	factory := func(_ context.Context, authorized, runtime *ResolvedConfig, dir string, plan *SetupPlan, journal *Journal, roles *RoleSecrets, owner *Executor) (*Executor, error) {
		opened++
		if authorized != cfg || dir != local.stateDir || plan != local.plan || journal != local.journal || roles != &reloadedRoles || owner != nil {
			t.Fatal("reader construction lost its authenticated retained owner")
		}
		if runtime.OperationalEVM != "http://"+campaignEVMAuthority() || runtime.OperationalSubstrate != cfg.OperationalSubstrate || validateCampaignRPCTransport(cfg, runtime) != nil {
			t.Fatal("new reader bypassed the approved campaign transport")
		}
		return &Executor{cfg: runtime, plan: plan, journal: journal, roles: roles, substrate: &SubstrateManager{}}, nil
	}
	executor, runtime, err := openCampaignExecutorWithNativeOwner(t.Context(), cfg, local.stateDir, local.plan, local.journal, &reloadedRoles, local, factory)
	if err != nil || opened != 1 || executor == nil || executor.substrate == nil || executor.nativeOwner != nil || runtime == nil || local.substrate != nil {
		t.Fatalf("retained metadata failed reader handoff: opens%d executor%v error%v", opened, executor, err)
	}
}

// Existing live native ownership is borrowed exactly; the new handoff never
// opens a competing native reader merely because a release is provisional.
func TestRetainedCampaignNativeOwnerPreservesLiveReader(t *testing.T) {
	cfg, owner := retainedCampaignOwnerFixture(t)
	owner.substrate = &SubstrateManager{}
	opened := 0
	_, _, err := openCampaignExecutorWithNativeOwner(t.Context(), cfg, owner.stateDir, owner.plan, owner.journal, owner.roles, owner,
		func(_ context.Context, _, runtime *ResolvedConfig, _ string, _ *SetupPlan, _ *Journal, _ *RoleSecrets, selected *Executor) (*Executor, error) {
			opened++
			if selected != owner {
				t.Fatal("live native owner was replaced")
			}
			return &Executor{cfg: runtime, substrate: selected.substrate, nativeOwner: selected}, nil
		})
	if err != nil || opened != 1 {
		t.Fatalf("live owner handoff: %d %v", opened, err)
	}
}

// No factory is reached for a different approval, a partially built executor,
// or an observation-only command. Missing clients are not a generic fallback.
func TestRetainedCampaignNativeOwnerRejectsAmbiguousOwnership(t *testing.T) {
	cfg, original := retainedCampaignOwnerFixture(t)
	for _, fault := range []string{"plan", "journal", "roles", "nil roles", "changed credential", "directory", "config", "approval", "partial", "borrowed", "deployer", "independent", "payloads", "read only", "prepare", "wrong plan"} {
		owner := *original
		record := *cfg.provisionalResume.Record
		switch fault {
		case "plan":
			copy := *original.plan
			owner.plan = &copy
		case "journal":
			owner.journal = &Journal{}
		case "roles":
			owner.roles = &RoleSecrets{}
		case "nil roles":
			owner.roles = nil
		case "changed credential":
			roles := *original.roles
			roles.EVM = maps.Clone(original.roles.EVM)
			roles.EVM["synthetic-deployer"] = EVMRoleSecret{Address: "changed-synthetic-address"}
			owner.roles = &roles
		case "directory":
			owner.stateDir += "/different"
		case "config":
			copy := *cfg
			owner.cfg = &copy
		case "approval":
			copy := *cfg
			owner.auditAuthorizedConfig = &copy
		case "partial":
			owner.preparationIncomplete = true
		case "borrowed":
			owner.nativeOwner = original
		case "deployer":
			owner.deployer = &EvmTxManager{}
		case "independent":
			owner.independentSubstrate = &SubstrateManager{}
		case "payloads":
			owner.payloads = &DeploymentPayloads{}
		case "read only":
			record.ReadOnly = true
		case "prepare":
			record.Scenario = precompilePreparationScenario
		case "wrong plan":
			record.PlanHash = "other-plan"
		}
		cfg.provisionalResume.Record = &record
		opened := 0
		_, _, err := openCampaignExecutorWithNativeOwner(t.Context(), cfg, original.stateDir, original.plan, original.journal, original.roles, &owner,
			func(context.Context, *ResolvedConfig, *ResolvedConfig, string, *SetupPlan, *Journal, *RoleSecrets, *Executor) (*Executor, error) {
				opened++
				return nil, nil
			})
		if err == nil || opened != 0 {
			t.Fatalf("%s owner reached transport construction: opens%d err%v", fault, opened, err)
		}
		cfg.provisionalResume.Record = &provisionalResumeRecord{Command: "scenario", Scenario: "release-1.0", Provisional: true, PlanHash: original.plan.PlanHash}
	}
}

// A stopped egress remains a startup failure: the factory's error is retained
// without retrying through a direct route or silently taking an unrelated owner.
func TestRetainedCampaignNativeOwnerKeepsEgressFailureAndCancellation(t *testing.T) {
	cfg, local := retainedCampaignOwnerFixture(t)
	refused := errors.New("synthetic campaign proxy unavailable")
	opened := 0
	factory := func(context.Context, *ResolvedConfig, *ResolvedConfig, string, *SetupPlan, *Journal, *RoleSecrets, *Executor) (*Executor, error) {
		opened++
		return nil, refused
	}
	if _, _, err := openCampaignExecutorWithNativeOwner(t.Context(), cfg, local.stateDir, local.plan, local.journal, local.roles, local, factory); !errors.Is(err, refused) || opened != 1 {
		t.Fatalf("egress failure was hidden or rerouted: opens%d err%v", opened, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := openCampaignExecutorWithNativeOwner(ctx, cfg, local.stateDir, local.plan, local.journal, local.roles, local, factory); !errors.Is(err, context.Canceled) || opened != 1 {
		t.Fatalf("canceled acquisition opened new clients: opens%d err%v", opened, err)
	}
}
