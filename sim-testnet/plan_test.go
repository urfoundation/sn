package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
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
	if len(roles.ClaimRelayers) != cfg.Config.Topology.Operators {
		t.Fatalf("claim relayers = %d, want %d", len(roles.ClaimRelayers), cfg.Config.Topology.Operators)
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
	positions := map[string]int{}
	evmFundingActions := 0
	for position, action := range plan.Actions {
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
		positions[action.ID] = position
		if strings.HasPrefix(action.ID, "evm.fund-") {
			evmFundingActions++
			usable, parseErr := strconv.ParseUint(action.Parameters["usable_evm_rao"], 10, 64)
			if parseErr != nil || action.Parameters["existential_deposit_rao"] != strconv.FormatUint(plan.LiveFacts.ExistentialDepositRao, 10) || action.Spend.TAORao != usable+plan.LiveFacts.ExistentialDepositRao {
				t.Fatalf("EVM funding action does not bind usable balance and existential deposit: %+v", action)
			}
		}
		if strings.Contains(action.ID, "miner.register") || strings.Contains(action.ID, "validator.stake") {
			t.Fatalf("obsolete or unplanned registration/stake action %s", action.ID)
		}
	}
	wantEVMFundingActions := 5 + 3*cfg.Config.Topology.Operators
	if evmFundingActions != wantEVMFundingActions {
		t.Fatalf("EVM funding actions = %d, want %d", evmFundingActions, wantEVMFundingActions)
	}
	if plan.MaximumSpend.EVMGasWei != cfg.MaximumEVMGasWei {
		t.Fatalf("gas plan = %d, want exact campaign ceiling %d", plan.MaximumSpend.EVMGasWei, cfg.MaximumEVMGasWei)
	}
	wantAlpha := cfg.Policy.Deposit.TotalTestCampaignCapRao + uint64(cfg.Config.Topology.Validators)*cfg.Policy.Deposit.EpochCapRaoPerOperator
	if plan.MaximumSpend.AlphaRao != wantAlpha {
		t.Fatalf("alpha plan = %d, want %d", plan.MaximumSpend.AlphaRao, wantAlpha)
	}
	wantRegistrations := uint32(2*cfg.Config.Topology.Operators + cfg.Config.Topology.Validators + cfg.Config.Topology.fleetCandidates() + cfg.Config.Topology.ChurnFloorUIDs + 1)
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
	if !seen["campaign.evm-gas-reserve"] || !seen["campaign.voluntary-conviction.1"] || !seen[dishonestDepositActionID] || !seen["alpha.transfer.operator-deposit.1"] || !seen["alpha.transfer.validator.1"] || !seen["evm.vault-register-escrow"] || !seen["validator.take-zero.1"] || !seen["production.schedule-policy"] || !seen["production.hyperparameter.immunity_period"] || !seen["retirement.evm-gas-reserve"] || !seen["evm.fund-guardian"] || !seen["evm.governance-drill-implementation"] || !seen["precompile.transfer-out"] {
		t.Fatalf("release setup actions missing: %v", seen)
	}
	lastChurn := fmt.Sprintf("churn.register.%d", cfg.Config.Topology.ChurnFloorUIDs)
	for _, loadBearing := range []string{"evm.vault-register-escrow", "operator.deposit.register.1", "operator.register.1", "fleet.register.1", "validator.register.1"} {
		if positions[lastChurn] >= positions[loadBearing] {
			t.Fatalf("load-bearing registration %s precedes churn-floor barrier %s", loadBearing, lastChurn)
		}
	}
	for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
		fleet := cfg.Config.Topology.HeadFleets + challenger
		registration := actions[fmt.Sprintf("fleet.register.%d", fleet)]
		if !slices.Contains(registration.DependsOn, fmt.Sprintf("churn.register.%d", challenger)) {
			t.Fatalf("challenger fleet %d does not bind its intended churn registration: %+v", fleet, registration)
		}
	}
	if !slices.Contains(actions["churn.tournament-complete"].DependsOn, fmt.Sprintf("fleet.bind.%d.%d", cfg.Config.Topology.fleetCandidates(), cfg.Config.Topology.ClientsPerHeadFleet)) || !slices.Contains(actions["precompile.commitment-write"].DependsOn, "churn.tournament-complete") {
		t.Fatalf("churn tournament barrier is not exact: barrier=%+v precompile=%+v", actions["churn.tournament-complete"], actions["precompile.commitment-write"])
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
	if production.Parameters["epoch_blocks"] != "2400" || production.Parameters["after_accelerated_epochs"] != "20" || production.Spend.EVMGasWei == 0 || actions["retirement.evm-gas-reserve"].Spend.EVMGasWei == 0 {
		t.Fatalf("production/retirement reservations are incomplete: production=%+v retirement=%+v", production, actions["retirement.evm-gas-reserve"])
	}
	dishonest := actions[dishonestDepositActionID]
	if dishonest.Parameters["no_id"] != "2" || dishonest.Parameters["amount_rao"] != "1" || dishonest.Parameters["target_epoch"] != "next_fresh_production_epoch" || dishonest.Spend.EVMGasWei == 0 || !slices.Contains(dishonest.DependsOn, "production.schedule-policy") {
		t.Fatalf("dishonest deposit action is not exact and production-fenced: %+v", dishonest)
	}
	voluntary := actions["campaign.voluntary-conviction.1"]
	if voluntary.Parameters["amount_rao"] != "1000000000" || voluntary.Spend.EVMGasWei == 0 || actions["alpha.transfer.operator-deposit.1"].Spend.AlphaRao != 3_250_000_000 || actions["alpha.transfer.operator-deposit.2"].Spend.AlphaRao != 2_250_000_000 {
		t.Fatalf("campaign allocations are not exact: voluntary=%+v op1=%+v op2=%+v", voluntary, actions["alpha.transfer.operator-deposit.1"], actions["alpha.transfer.operator-deposit.2"])
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		if !seen[fmt.Sprintf("evm.fund-operator-%d-claim-relayer", i)] {
			t.Fatalf("operator %d shared claim relayer funding is missing", i)
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

func TestBuildPlanRequiresUntouchedSingleOwnerUIDTopology(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*SetupFacts)
		want   string
	}{
		{name: "canonical", mutate: func(*SetupFacts) {}},
		{name: "empty", mutate: func(f *SetupFacts) { f.ExistingUIDCount = 0; f.ExistingUIDs = nil }, want: "inconsistent"},
		{name: "missing bootstrap identity", mutate: func(f *SetupFacts) { f.ExistingUIDCount = 1; f.ExistingUIDs = f.ExistingUIDs[:1] }, want: "do not exactly fill"},
		{name: "additional external identity", mutate: func(f *SetupFacts) {
			f.ExistingUIDCount = 3
			f.ExistingUIDs = append(f.ExistingUIDs, ExistingUIDFact{UID: 2, Hotkey: "0x" + strings.Repeat("46", 32), Coldkey: "0x" + strings.Repeat("47", 32), RegistrationBlock: 70})
		}, want: "do not exactly fill"},
		{name: "different owner", mutate: func(f *SetupFacts) { f.SubnetOwnerHotkey = "0x" + strings.Repeat("43", 32) }, want: "not the configured"},
		{name: "different uid zero", mutate: func(f *SetupFacts) { f.UIDZeroHotkey = "0x" + strings.Repeat("44", 32) }, want: "not the configured"},
		{name: "malformed owner", mutate: func(f *SetupFacts) { f.SubnetOwnerHotkey = "0x01" }, want: "32-byte"},
	}
	for _, test := range tests {
		facts := *testSetupFacts()
		test.mutate(&facts)
		_, err := buildPlan(cfg, &facts, roles, time.Unix(1, 0))
		if test.want == "" && err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
			t.Fatalf("%s: error=%v, want substring %q", test.name, err, test.want)
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
	depositChanged := *facts
	depositChanged.ExistentialDepositRao++
	d, err := buildPlan(cfg, &depositChanged, roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	roleCount := uint64(5 + 3*cfg.Config.Topology.Operators)
	if a.PlanHash == d.PlanHash || d.MaximumSpend.TAORao-a.MaximumSpend.TAORao != roleCount {
		t.Fatalf("existential-deposit drift was not bound exactly: hashes=%s/%s tao=%d/%d roles=%d", a.PlanHash, d.PlanHash, a.MaximumSpend.TAORao, d.MaximumSpend.TAORao, roleCount)
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
		"alpha limit":         func(c *ResolvedConfig, _ *SetupFacts) { c.MaximumAlphaRao = 1 },
		"alpha availability":  func(_ *ResolvedConfig, f *SetupFacts) { f.AlphaAvailableRao = 1 },
		"tao limit":           func(c *ResolvedConfig, _ *SetupFacts) { c.MaximumTAORao = 1 },
		"gas limit":           func(c *ResolvedConfig, _ *SetupFacts) { c.MaximumEVMGasWei = 1 },
		"registration burn":   func(c *ResolvedConfig, f *SetupFacts) { f.BurnRao = c.Config.Budgets.MaximumRegistrationBurnRao + 1 },
		"existential deposit": func(_ *ResolvedConfig, f *SetupFacts) { f.ExistentialDepositRao = 0 },
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

func TestEVMFundingTermsRejectEveryApprovalDrift(t *testing.T) {
	base := Action{
		ID: "evm.fund-owner", Kind: "substrate-extrinsic",
		Parameters: map[string]string{"usable_evm_rao": "1000", "existential_deposit_rao": "500"},
		Spend:      Spend{TAORao: 1500},
	}
	if usable, err := evmFundingTerms(base, 500); err != nil || usable != 1_000 {
		t.Fatalf("valid funding terms = %d, %v", usable, err)
	}
	for name, mutate := range map[string]func(*Action){
		"missing usable":     func(a *Action) { delete(a.Parameters, "usable_evm_rao") },
		"extra parameter":    func(a *Action) { a.Parameters["extra"] = "1" },
		"malformed usable":   func(a *Action) { a.Parameters["usable_evm_rao"] = "x" },
		"zero usable":        func(a *Action) { a.Parameters["usable_evm_rao"] = "0" },
		"deposit drift":      func(a *Action) { a.Parameters["existential_deposit_rao"] = "501" },
		"spend below":        func(a *Action) { a.Spend.TAORao-- },
		"spend above":        func(a *Action) { a.Spend.TAORao++ },
		"cross-budget spend": func(a *Action) { a.Spend.EVMGasWei = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			action := base
			action.Parameters = map[string]string{}
			for key, value := range base.Parameters {
				action.Parameters[key] = value
			}
			mutate(&action)
			if _, err := evmFundingTerms(action, 500); err == nil {
				t.Fatalf("%s funding approval drift was accepted", name)
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
