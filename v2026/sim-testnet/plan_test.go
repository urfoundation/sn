package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestBuildPlanIsBoundedTopologicalAndUsesPersistedRoles(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if roles.Deployer != secrets.EVM["deployer"].Address || roles.Owner != secrets.EVM["testnet-owner"].Address {
		t.Fatal("plan roles do not match persisted role-secret derivation")
	}
	if len(roles.MinerClaimRelayers) != cfg.Config.Topology.Miners {
		t.Fatalf("claim relayers = %d, want %d", len(roles.MinerClaimRelayers), cfg.Config.Topology.Miners)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Actions[0].ID != "subnet.verify-owner" {
		t.Fatalf("first action = %s", plan.Actions[0].ID)
	}
	seen := map[string]bool{}
	actions := map[string]Action{}
	for _, action := range plan.Actions {
		if seen[action.ID] {
			t.Fatalf("duplicate action %s", action.ID)
		}
		for _, dependency := range action.DependsOn {
			if !seen[dependency] {
				t.Fatalf("action %s appears before dependency %s", action.ID, dependency)
			}
		}
		seen[action.ID] = true
		actions[action.ID] = action
		if strings.Contains(action.ID, "miner.register") || strings.Contains(action.ID, "validator.stake") {
			t.Fatalf("obsolete or unplanned registration/stake action %s", action.ID)
		}
	}
	if plan.MaximumSpend.EVMGasWei != cfg.MaximumEVMGasWei {
		t.Fatalf("gas plan = %d, want exact campaign ceiling %d", plan.MaximumSpend.EVMGasWei, cfg.MaximumEVMGasWei)
	}
	wantAlpha := cfg.Policy.Deposit.TotalTestCampaignCapRao + uint64(cfg.Config.Topology.Validators)*cfg.Policy.Deposit.EpochCapRaoPerOperator
	if plan.MaximumSpend.AlphaRao != wantAlpha {
		t.Fatalf("alpha plan = %d, want %d", plan.MaximumSpend.AlphaRao, wantAlpha)
	}
	wantRegistrations := uint32(2*cfg.Config.Topology.Operators + cfg.Config.Topology.Validators + cfg.Config.Topology.HeadFleets + 1)
	if plan.MaximumSpend.Registrations != wantRegistrations {
		t.Fatalf("registrations = %d, want %d", plan.MaximumSpend.Registrations, wantRegistrations)
	}
	if plan.RegistrationBurnLimitRao != cfg.Config.Budgets.MaximumRegistrationBurnRao {
		t.Fatalf("registration burn limit = %d, want %d", plan.RegistrationBurnLimitRao, cfg.Config.Budgets.MaximumRegistrationBurnRao)
	}
	for _, action := range plan.Actions {
		if action.Spend.Registrations > 0 && action.Parameters["maximum_burn_rao"] != fmt.Sprint(plan.RegistrationBurnLimitRao) {
			t.Fatalf("registration action %s does not bind the reviewed burn limit: %+v", action.ID, action)
		}
	}
	if !seen["campaign.evm-gas-reserve"] || !seen["campaign.voluntary-conviction.1"] || !seen["alpha.transfer.operator-deposit.1"] || !seen["alpha.transfer.validator.1"] || !seen["evm.vault-register-escrow"] || !seen["validator.take-zero.1"] || !seen["production.schedule-policy"] || !seen["production.hyperparameter.immunity_period"] || !seen["retirement.evm-gas-reserve"] || !seen["evm.fund-guardian"] || !seen["evm.governance-drill-implementation"] || !seen["precompile.transfer-out"] {
		t.Fatalf("release setup actions missing: %v", seen)
	}
	for _, id := range []string{"precompile.commitment-write", "precompile.commitment-restore", "precompile.probe-deploy", "precompile.read-battery", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out"} {
		if !seen[id] {
			t.Fatalf("precompile action %s is missing", id)
		}
		if actions[id].Kind == "evm-transaction" && actions[id].Spend.EVMGasWei == 0 {
			t.Fatalf("precompile transaction %s has no gas ceiling", id)
		}
	}
	if actions["precompile.seed"].Parameters["maximum_tao_rao"] != "1000" || actions["governance.guardian-pause"].DependsOn[0] != "precompile.transfer-out" {
		t.Fatalf("precompile economic gate/dependency is not exact: seed=%+v governance=%+v", actions["precompile.seed"], actions["governance.guardian-pause"])
	}
	for _, id := range []string{"governance.guardian-pause", "governance.upgrade-adversary", "governance.probe-custody", "governance.restore-coordinator", "governance.guardian-unpause"} {
		if !seen[id] || actions[id].Spend.EVMGasWei == 0 {
			t.Fatalf("governance drill action %s is missing or unbudgeted", id)
		}
	}
	production := actions["production.schedule-policy"]
	if production.Parameters["epoch_blocks"] != "50400" || production.Parameters["after_accelerated_epochs"] != "20" || production.Spend.EVMGasWei == 0 || actions["retirement.evm-gas-reserve"].Spend.EVMGasWei == 0 {
		t.Fatalf("production/retirement reservations are incomplete: production=%+v retirement=%+v", production, actions["retirement.evm-gas-reserve"])
	}
	voluntary := actions["campaign.voluntary-conviction.1"]
	if voluntary.Parameters["amount_rao"] != "1000000000" || voluntary.Spend.EVMGasWei == 0 || actions["alpha.transfer.operator-deposit.1"].Spend.AlphaRao != 3_000_000_000 || actions["alpha.transfer.operator-deposit.2"].Spend.AlphaRao != 2_000_000_000 {
		t.Fatalf("campaign allocations are not exact: voluntary=%+v op1=%+v op2=%+v", voluntary, actions["alpha.transfer.operator-deposit.1"], actions["alpha.transfer.operator-deposit.2"])
	}
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		if !seen[fmt.Sprintf("evm.fund-miner-%d-claim-relayer", i)] {
			t.Fatalf("miner %d claim relayer funding is missing", i)
		}
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		for _, id := range []string{fmt.Sprintf("fleet.fund.%d", fleet), fmt.Sprintf("fleet.register.%d", fleet), fmt.Sprintf("fleet.fund-hotkey.%d", fleet), fmt.Sprintf("fleet.commitment.%d", fleet), fmt.Sprintf("fleet.mirror.%d", fleet)} {
			if !seen[id] {
				t.Fatalf("fleet %d action %s is missing", fleet, id)
			}
		}
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			if !seen[fmt.Sprintf("fleet.bind.%d.%d", fleet, member)] {
				t.Fatalf("fleet %d member %d binding is missing", fleet, member)
			}
		}
	}
}

func TestPlanHashExcludesGenerationTimeButIncludesLiveFacts(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	facts := testSetupFacts()
	a, err := buildPlan(cfg, facts, roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildPlan(cfg, facts, roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if a.PlanHash != b.PlanHash {
		t.Fatalf("generation time changed plan hash: %s != %s", a.PlanHash, b.PlanHash)
	}
	observationOnly := *facts
	observationOnly.FinalizedBlock++
	observationOnly.FinalizedBlockHash = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	observationOnly.AlphaAvailableRao++
	observationOnly.WalletFreeTAORao++
	observed, err := buildPlan(cfg, &observationOnly, roles, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if a.PlanHash != observed.PlanHash {
		t.Fatalf("advancing finalized observations changed approval hash: %s != %s", a.PlanHash, observed.PlanHash)
	}
	changed := *facts
	changed.BurnRao++
	c, err := buildPlan(cfg, &changed, roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if a.PlanHash == c.PlanHash {
		t.Fatal("finalized burn did not change plan hash")
	}
	if a.MaximumSpend.TAORao != c.MaximumSpend.TAORao {
		t.Fatal("moving observed burn changed the reviewed registration ceiling")
	}
}

func TestPlanHashBindsTheCompleteReleaseLock(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	first, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	changed := *cfg.Release
	changed.Dependencies = map[string]string{}
	for name, value := range cfg.Release.Dependencies {
		changed.Dependencies[name] = value
	}
	changed.Dependencies["redis"] = "redis:8-alpine@sha256:" + strings.Repeat("3", 64)
	cfg.Release = &changed
	second, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if first.ReleaseLockHash == second.ReleaseLockHash || first.PlanHash == second.PlanHash {
		t.Fatal("release-lock drift did not invalidate the approved plan hash")
	}
}

func TestPlanHashBindsResolvedVaultLaunchInputs(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	first, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Authority = "private-testnet-rpc.example:9944"
	second, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if first.ResolvedInputsHash == second.ResolvedInputsHash || first.PlanHash == second.PlanHash {
		t.Fatal("resolved vault launch-input drift did not invalidate the approved plan hash")
	}
}

func TestBuildPlanFailsClosedOnEveryBudget(t *testing.T) {
	base := testResolvedConfig(t)
	roles, _ := derivePublicRoles(base)
	for name, mutate := range map[string]func(*ResolvedConfig, *SetupFacts){
		"alpha limit":        func(c *ResolvedConfig, _ *SetupFacts) { c.MaximumAlphaRao = 1 },
		"alpha availability": func(_ *ResolvedConfig, f *SetupFacts) { f.AlphaAvailableRao = 1 },
		"tao limit":          func(c *ResolvedConfig, _ *SetupFacts) { c.MaximumTAORao = 1 },
		"gas limit":          func(c *ResolvedConfig, _ *SetupFacts) { c.MaximumEVMGasWei = 1 },
		"registration burn":  func(c *ResolvedConfig, f *SetupFacts) { f.BurnRao = c.Config.Budgets.MaximumRegistrationBurnRao + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			copyCfg := *base
			facts := *testSetupFacts()
			mutate(&copyCfg, &facts)
			if _, err := buildPlan(&copyCfg, &facts, roles, time.Unix(1, 0)); err == nil {
				t.Fatalf("%s violation accepted", name)
			}
		})
	}
}

func TestRegistrationBurnLimitBindsPlanAndAction(t *testing.T) {
	plan := &SetupPlan{RegistrationBurnLimitRao: 100}
	action := Action{ID: "register", Parameters: map[string]string{"maximum_burn_rao": "100"}, Spend: Spend{Registrations: 1}}
	if limit, err := registrationBurnLimit(plan, action); err != nil || limit != 100 {
		t.Fatalf("registration limit = %d, %v", limit, err)
	}
	action.Parameters["maximum_burn_rao"] = "101"
	if _, err := registrationBurnLimit(plan, action); err == nil {
		t.Fatal("action-local registration limit widened its approved plan")
	}
	action.Parameters["maximum_burn_rao"] = "not-a-number"
	if _, err := registrationBurnLimit(plan, action); err == nil {
		t.Fatal("malformed registration limit was accepted")
	}
}

func TestPlanValidationRejectsDuplicateAndForwardActionDependencies(t *testing.T) {
	base := &SetupPlan{
		Actions: []Action{
			{ID: "first"},
			{ID: "second", DependsOn: []string{"first"}},
		},
	}
	if err := validatePlanBudget(base); err != nil {
		t.Fatalf("valid action graph rejected: %v", err)
	}
	duplicate := *base
	duplicate.Actions = append([]Action(nil), base.Actions...)
	duplicate.Actions[1].ID = "first"
	if err := validatePlanBudget(&duplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate action id was accepted: %v", err)
	}
	forward := *base
	forward.Actions = append([]Action(nil), base.Actions...)
	forward.Actions[0].DependsOn = []string{"second"}
	if err := validatePlanBudget(&forward); err == nil || !strings.Contains(err.Error(), "missing or later") {
		t.Fatalf("forward dependency was accepted: %v", err)
	}
}

func TestCheckedArithmeticAndMulDiv(t *testing.T) {
	if _, ok := checkedAdd(math.MaxUint64, 1); ok {
		t.Fatal("checkedAdd overflow accepted")
	}
	if _, ok := checkedMul(math.MaxUint64, 2); ok {
		t.Fatal("checkedMul overflow accepted")
	}
	if got, ok := mulDivFloor(math.MaxUint64, 55, 100); !ok || got != 10_145_709_240_540_253_388 {
		t.Fatalf("overflow-safe mulDiv = %d, %v", got, ok)
	}
}
