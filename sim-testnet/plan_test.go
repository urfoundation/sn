package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// historicalPlanFacts is the exact pre-v4 SetupFacts wire order. The v2 burn
// fields were optional so the same fixture can exercise the late-v1 shape.
type historicalPlanFacts struct {
	BurnRao               uint64            `json:"burn_rao"`
	MinBurnRao            uint64            `json:"min_burn_rao,omitempty"`
	MaxBurnRao            uint64            `json:"max_burn_rao,omitempty"`
	BurnHalfLifeBlocks    uint16            `json:"burn_half_life_blocks,omitempty"`
	BurnIncreaseMultQ64   string            `json:"burn_increase_mult_q64,omitempty"`
	AlphaSourceHotkey     string            `json:"alpha_source_hotkey"`
	AlphaAvailableRao     uint64            `json:"alpha_available_rao"`
	ExistingUIDCount      uint16            `json:"existing_uid_count"`
	SubnetOwnerHotkey     string            `json:"subnet_owner_hotkey"`
	UIDZeroHotkey         string            `json:"uid_zero_hotkey"`
	ExistingUIDs          []ExistingUIDFact `json:"existing_uids"`
	ExistentialDepositRao uint64            `json:"existential_deposit_rao"`
	NominatorMinimumRao   uint64            `json:"nominator_minimum_rao"`
	ProbeTAORao           uint64            `json:"probe_tao_rao"`
	WalletFreeTAORao      uint64            `json:"wallet_free_tao_rao"`
	FinalizedBlock        uint64            `json:"finalized_block"`
	FinalizedBlockHash    string            `json:"finalized_block_hash"`
}

// historicalPlanWire is the exact v3 plan field order before deployment
// identity and superseded-spend fields were introduced.
type historicalPlanWire struct {
	Schema                       string              `json:"schema"`
	Release                      string              `json:"release"`
	ReleaseLockHash              string              `json:"release_lock_hash"`
	DeploymentID                 string              `json:"deployment_id"`
	ChainID                      uint64              `json:"chain_id"`
	GenesisHash                  string              `json:"genesis_hash"`
	Netuid                       uint16              `json:"netuid"`
	Owner                        string              `json:"owner"`
	LiveFacts                    historicalPlanFacts `json:"live_facts"`
	RegistrationBurnLimitRao     uint64              `json:"registration_burn_limit_rao"`
	NativeTransactionFeeLimitRao uint64              `json:"native_transaction_fee_limit_rao,omitempty"`
	MaximumEVMFeePerGasWei       uint64              `json:"maximum_evm_fee_per_gas_wei,omitempty"`
	BootstrapBurnHalfLifeBlocks  uint16              `json:"bootstrap_burn_half_life_blocks,omitempty"`
	ProductionBurnHalfLifeBlocks uint16              `json:"production_burn_half_life_blocks,omitempty"`
	PriorPlanHashes              []string            `json:"prior_plan_hashes,omitempty"`
	ConfigHash                   string              `json:"config_hash"`
	ResolvedInputsHash           string              `json:"resolved_inputs_hash"`
	PolicyHash                   string              `json:"policy_hash"`
	Roles                        PublicRoles         `json:"roles"`
	Actions                      []Action            `json:"actions"`
	MaximumSpend                 Spend               `json:"maximum_spend"`
	Limits                       Spend               `json:"limits"`
	PlanHash                     string              `json:"plan_hash"`
	GeneratedAt                  string              `json:"generated_at,omitempty"`
}

// Project a current plan into the exact v3 wire shape used by an interrupted
// setup before the v4 recovery fields existed.
func historicalPlanFromCurrent(plan *SetupPlan, schema string) historicalPlanWire {
	facts := plan.LiveFacts
	return historicalPlanWire{
		Schema: schema, Release: plan.Release, ReleaseLockHash: plan.ReleaseLockHash,
		DeploymentID: plan.DeploymentID, ChainID: plan.ChainID, GenesisHash: plan.GenesisHash,
		Netuid: plan.Netuid, Owner: plan.Owner,
		LiveFacts: historicalPlanFacts{
			BurnRao: facts.BurnRao, MinBurnRao: facts.MinBurnRao, MaxBurnRao: facts.MaxBurnRao,
			BurnHalfLifeBlocks: facts.BurnHalfLifeBlocks, BurnIncreaseMultQ64: facts.BurnIncreaseMultQ64,
			AlphaSourceHotkey: facts.AlphaSourceHotkey, AlphaAvailableRao: facts.AlphaAvailableRao,
			ExistingUIDCount: facts.ExistingUIDCount, SubnetOwnerHotkey: facts.SubnetOwnerHotkey,
			UIDZeroHotkey: facts.UIDZeroHotkey, ExistingUIDs: append([]ExistingUIDFact(nil), facts.ExistingUIDs...),
			ExistentialDepositRao: facts.ExistentialDepositRao, NominatorMinimumRao: facts.NominatorMinimumRao,
			ProbeTAORao: facts.ProbeTAORao, WalletFreeTAORao: facts.WalletFreeTAORao,
			FinalizedBlock: facts.FinalizedBlock, FinalizedBlockHash: facts.FinalizedBlockHash,
		},
		RegistrationBurnLimitRao:     plan.RegistrationBurnLimitRao,
		NativeTransactionFeeLimitRao: plan.NativeTransactionFeeLimitRao,
		MaximumEVMFeePerGasWei:       plan.MaximumEVMFeePerGasWei,
		BootstrapBurnHalfLifeBlocks:  plan.BootstrapBurnHalfLifeBlocks,
		ProductionBurnHalfLifeBlocks: plan.ProductionBurnHalfLifeBlocks,
		PriorPlanHashes:              append([]string(nil), plan.PriorPlanHashes...), ConfigHash: plan.ConfigHash,
		ResolvedInputsHash: plan.ResolvedInputsHash, PolicyHash: plan.PolicyHash,
		Roles: plan.Roles, Actions: append([]Action(nil), plan.Actions...), MaximumSpend: plan.MaximumSpend,
		Limits: plan.Limits, GeneratedAt: plan.GeneratedAt,
	}
}

// Apply the historical hash normalization without using production recovery
// code, providing an independent expected digest for the regression fixture.
func historicalPlanHash(plan historicalPlanWire) (string, error) {
	plan.PlanHash = ""
	plan.GeneratedAt = ""
	plan.LiveFacts.FinalizedBlock = 0
	plan.LiveFacts.FinalizedBlockHash = ""
	plan.LiveFacts.AlphaAvailableRao = 0
	plan.LiveFacts.WalletFreeTAORao = 0
	if planUsesRegistrationEnvelope(plan.Schema) {
		plan.LiveFacts.BurnRao = 0
	}
	return canonicalHashHex(plan)
}

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
		t.Fatalf("gas plan = %s, want exact campaign ceiling %s", plan.MaximumSpend.EVMGasWei, cfg.MaximumEVMGasWei)
	}
	minimumTransfer, err := minimumAlphaTransferRao(plan.LiveFacts.DefaultMinTransferRao, plan.LiveFacts.AlphaPriceQ9, cfg.Config.AlphaTransfers.MinimumTAOEquivalentMarginBPS)
	if err != nil {
		t.Fatal(err)
	}
	reserveTransfer, _, err := reserveValidatorTransferRao(plan.LiveFacts.RegisteredAlphaRao, plan.LiveFacts.ReserveValidatorAlphaRao, minimumTransfer, cfg.Config.ValidatorBootstrap.ReserveTargetShareBPS)
	if err != nil {
		t.Fatal(err)
	}
	wantAlpha := actions["alpha.transfer.operator-deposit.1"].Spend.AlphaRao + actions["alpha.transfer.operator-deposit.2"].Spend.AlphaRao + reserveTransfer + actions["alpha.transfer.validator.2"].Spend.AlphaRao
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
	if plan.Schema != currentSetupPlanSchema || plan.NativeTransactionFeeLimitRao != cfg.Config.Budgets.MaximumNativeTransactionFeeRao || plan.MaximumEVMFeePerGasWei != cfg.Config.Budgets.MaximumEVMFeePerGasWei || plan.AlphaTransferMarginBPS != cfg.Config.AlphaTransfers.MinimumTAOEquivalentMarginBPS || plan.MinimumSourceRemainingRao != cfg.Config.ValidatorBootstrap.MinimumSourceRemainingAlphaRao {
		t.Fatalf("plan schema/native fee limit = %q/%d", plan.Schema, plan.NativeTransactionFeeLimitRao)
	}
	if plan.BootstrapBurnHalfLifeBlocks != 1 || plan.ProductionBurnHalfLifeBlocks != 360 || plan.LiveFacts.MinBurnRao != 500_000 || plan.LiveFacts.BurnIncreaseMultQ64 != "23058430092136939520" {
		t.Fatalf("registration economics are not approval-bound: %+v", plan)
	}
	for _, action := range plan.Actions {
		if action.Spend.Registrations > 0 && action.Parameters["maximum_burn_rao"] != fmt.Sprint(plan.RegistrationBurnLimitRao) {
			t.Fatalf("registration action %s does not bind the reviewed burn limit: %+v", action.ID, action)
		}
	}
	roleFunding := plan.RegistrationBurnLimitRao + plan.NativeTransactionFeeLimitRao + plan.LiveFacts.ExistentialDepositRao
	if actions["churn.fund.1"].Spend.TAORao != roleFunding || actions["fleet.fund.1"].Spend.TAORao != roleFunding || actions["validator.fund.1"].Spend.TAORao != roleFunding {
		t.Fatalf("native registration roles are not funded for burn, fee, and keep-alive: churn=%d fleet=%d validator=%d want=%d", actions["churn.fund.1"].Spend.TAORao, actions["fleet.fund.1"].Spend.TAORao, actions["validator.fund.1"].Spend.TAORao, roleFunding)
	}
	if actions["churn.fund.1"].Parameters["maximum_burn_rao"] != fmt.Sprint(plan.RegistrationBurnLimitRao) || actions["churn.fund.1"].Parameters["maximum_fee_rao"] != fmt.Sprint(plan.NativeTransactionFeeLimitRao) || actions["churn.fund.1"].Parameters["keep_alive_reserve_rao"] != fmt.Sprint(plan.LiveFacts.ExistentialDepositRao) {
		t.Fatalf("native registration funding components are not approval-bound: %+v", actions["churn.fund.1"])
	}
	if actions["fleet.fund-hotkey.1"].Spend.TAORao != 4*plan.NativeTransactionFeeLimitRao+plan.LiveFacts.ExistentialDepositRao || actions["fleet.fund-hotkey.2"].Spend.TAORao != 2*plan.NativeTransactionFeeLimitRao+plan.LiveFacts.ExistentialDepositRao || actions["fleet.fund-hotkey.1"].Parameters["keep_alive_reserve_rao"] != fmt.Sprint(plan.LiveFacts.ExistentialDepositRao) || actions["wallet.native-fee-reserve"].Parameters["maximum_fee_rao"] != fmt.Sprint(plan.NativeTransactionFeeLimitRao) {
		t.Fatalf("native commitment/global fee reserves are not approval-bound: fleet1=%d fleet2=%d wallet=%+v", actions["fleet.fund-hotkey.1"].Spend.TAORao, actions["fleet.fund-hotkey.2"].Spend.TAORao, actions["wallet.native-fee-reserve"])
	}
	nativeWrites := 0
	for _, action := range plan.Actions {
		if action.Kind == "substrate-extrinsic" {
			nativeWrites++
		}
	}
	if actions["wallet.native-fee-reserve"].Parameters["native_writes"] != strconv.Itoa(nativeWrites) {
		t.Fatalf("native fee reserve covers %s writes, want exact plan count %d", actions["wallet.native-fee-reserve"].Parameters["native_writes"], nativeWrites)
	}
	if !seen["campaign.evm-gas-reserve"] || !seen["campaign.voluntary-conviction.1"] || !seen[dishonestDepositActionID] || !seen["alpha.transfer.operator-deposit.1"] || !seen["alpha.transfer.validator.1"] || !seen["validator.reserve-majority"] || !seen["evm.vault-register-escrow"] || !seen["validator.take-zero.1"] || !seen["production.schedule-policy"] || !seen["production.hyperparameter.burn_half_life"] || !seen["production.hyperparameter.immunity_period"] || !seen["retirement.evm-gas-reserve"] || !seen["evm.fund-guardian"] || !seen["evm.governance-drill-implementation"] || !seen["precompile.transfer-out"] {
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
	for _, id := range []string{"precompile.commitment-write", "precompile.commitment-restore"} {
		if actions[id].Parameters["canonical_generation"] != strconv.FormatUint(precompileCanonicalFleetGeneration, 10) {
			t.Fatalf("precompile commitment action %s does not bind generation 2: %+v", id, actions[id])
		}
	}
	for _, id := range []string{"precompile.commitment-write", "precompile.commitment-restore", "precompile.probe-deploy", "precompile.read-battery", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out"} {
		if !seen[id] {
			t.Fatalf("precompile action %s is missing", id)
		}
		if actions[id].Kind == "evm-transaction" && actions[id].Spend.EVMGasWei.IsZero() {
			t.Fatalf("precompile transaction %s has no gas ceiling", id)
		}
	}
	if actions["precompile.seed"].Parameters["maximum_tao_rao"] != "1000" || actions["governance.guardian-pause"].DependsOn[0] != "precompile.transfer-out" {
		t.Fatalf("precompile economic gate/dependency is not exact: seed=%+v governance=%+v", actions["precompile.seed"], actions["governance.guardian-pause"])
	}
	for _, id := range []string{"governance.guardian-pause", "governance.upgrade-adversary", "governance.probe-custody", "governance.restore-coordinator", "governance.guardian-unpause"} {
		if !seen[id] || actions[id].Spend.EVMGasWei.IsZero() {
			t.Fatalf("governance drill action %s is missing or unbudgeted", id)
		}
	}
	production := actions["production.schedule-policy"]
	if production.Parameters["epoch_blocks"] != "360" || production.Parameters["after_accelerated_epochs"] != "5" || production.Spend.EVMGasWei.IsZero() || actions["retirement.evm-gas-reserve"].Spend.EVMGasWei.IsZero() {
		t.Fatalf("production/retirement reservations are incomplete: production=%+v retirement=%+v", production, actions["retirement.evm-gas-reserve"])
	}
	dishonest := actions[dishonestDepositActionID]
	if dishonest.Parameters["no_id"] != "2" || dishonest.Parameters["amount_rao"] != "5000000000" || dishonest.Parameters["target_epoch"] != "next_fresh_production_epoch" || dishonest.Spend.EVMGasWei.IsZero() || !slices.Contains(dishonest.DependsOn, "production.hyperparameter.immunity_period") {
		t.Fatalf("dishonest deposit action is not exact and production-fenced: %+v", dishonest)
	}
	if !slices.Contains(actions["production.hyperparameter.burn_half_life"].DependsOn, "production.schedule-policy") || !slices.Contains(actions["production.hyperparameter.immunity_period"].DependsOn, "production.hyperparameter.burn_half_life") {
		t.Fatalf("production hyperparameter transition is not an exact topological chain: burn=%+v immunity=%+v", actions["production.hyperparameter.burn_half_life"], actions["production.hyperparameter.immunity_period"])
	}
	voluntary := actions["campaign.voluntary-conviction.1"]
	op1Alpha, op2Alpha := actions["alpha.transfer.operator-deposit.1"], actions["alpha.transfer.operator-deposit.2"]
	if voluntary.Parameters["amount_rao"] != "1000000000" || voluntary.Parameters["reserve_runtime_share_transitions"] != "2" || voluntary.Parameters["reserve_rounding_allowance_rao"] != "2" || voluntary.Spend.EVMGasWei.IsZero() || op1Alpha.Parameters["campaign_requirement_rao"] != "101000000000" || op2Alpha.Parameters["campaign_requirement_rao"] != "95000000000" || op1Alpha.Spend.AlphaRao != 101000000023 || op2Alpha.Spend.AlphaRao != 95000000021 || op1Alpha.Parameters["exact_amount_rao"] != strconv.FormatUint(op1Alpha.Spend.AlphaRao, 10) || op2Alpha.Parameters["exact_amount_rao"] != strconv.FormatUint(op2Alpha.Spend.AlphaRao, 10) || op1Alpha.Parameters["minimum_destination_credit_rao"] != "101000000022" || op2Alpha.Parameters["minimum_destination_credit_rao"] != "95000000020" || op1Alpha.Parameters["reserve_calls"] != "11" || op2Alpha.Parameters["reserve_calls"] != "10" || op1Alpha.Parameters["reserve_rounding_allowance_per_call_rao"] != "2" || op2Alpha.Parameters["reserve_rounding_allowance_per_call_rao"] != "2" {
		t.Fatalf("campaign allocations are not exact: voluntary=%+v op1=%+v op2=%+v", voluntary, actions["alpha.transfer.operator-deposit.1"], actions["alpha.transfer.operator-deposit.2"])
	}
	reserveAlpha := actions["alpha.transfer.validator.1"]
	if reserveAlpha.Spend.AlphaRao != reserveTransfer || reserveAlpha.Parameters["reserve_target_share_bps"] != "6500" || reserveAlpha.Parameters["reserve_minimum_share_bps"] != "6000" || !slices.Contains(actions["validator.reserve-majority"].DependsOn, "alpha.transfer.validator.2") {
		t.Fatalf("reserve validator bootstrap is not majority-bound: transfer=%+v barrier=%+v", reserveAlpha, actions["validator.reserve-majority"])
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

func TestRegistrationEconomicsAcceptsRuntime452BoundedBootstrap(t *testing.T) {
	cfg := testResolvedConfig(t)
	facts := testSetupFacts()
	if err := validateRegistrationEconomics(cfg, facts, cfg.Config.Budgets.MaximumRegistrationBurnRao); err != nil {
		t.Fatalf("bounded runtime-452 registration economics were rejected: %v", err)
	}
	facts.BurnHalfLifeBlocks = 1
	if err := validateRegistrationEconomics(cfg, facts, cfg.Config.Budgets.MaximumRegistrationBurnRao); err != nil {
		t.Fatalf("already-bootstrapped registration economics were rejected: %v", err)
	}
}

func TestRegistrationEconomicsRejectsUnsafeBoundsAndMultiplier(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ResolvedConfig, *SetupFacts, *uint64)
		want   string
	}{
		{name: "zero minimum", mutate: func(_ *ResolvedConfig, facts *SetupFacts, _ *uint64) { facts.MinBurnRao = 0 }, want: "burn bounds"},
		{name: "inverted bounds", mutate: func(_ *ResolvedConfig, facts *SetupFacts, _ *uint64) { facts.MaxBurnRao = facts.MinBurnRao - 1 }, want: "burn bounds"},
		{name: "current below minimum", mutate: func(_ *ResolvedConfig, facts *SetupFacts, _ *uint64) { facts.BurnRao = facts.MinBurnRao - 1 }, want: "burn bounds"},
		{name: "current above maximum", mutate: func(_ *ResolvedConfig, facts *SetupFacts, _ *uint64) { facts.BurnRao = facts.MaxBurnRao + 1 }, want: "burn bounds"},
		{name: "minimum above cap", mutate: func(_ *ResolvedConfig, facts *SetupFacts, limit *uint64) { *limit = facts.MinBurnRao - 1 }, want: "burn bounds"},
		{name: "current above cap", mutate: func(_ *ResolvedConfig, facts *SetupFacts, limit *uint64) { *limit = facts.BurnRao - 1 }, want: "burn bounds"},
		{name: "bootstrap not one block", mutate: func(cfg *ResolvedConfig, _ *SetupFacts, _ *uint64) {
			cfg.Hyperparameters.OwnerControlled["burn_half_life"] = 2
		}, want: "half-life"},
		{name: "current outside lifecycle", mutate: func(_ *ResolvedConfig, facts *SetupFacts, _ *uint64) { facts.BurnHalfLifeBlocks = 17 }, want: "half-life"},
		{name: "missing production restore", mutate: func(cfg *ResolvedConfig, _ *SetupFacts, _ *uint64) {
			delete(cfg.Hyperparameters.ProductionOwnerControlled, "burn_half_life")
		}, want: "half-life"},
		{name: "malformed multiplier", mutate: func(_ *ResolvedConfig, facts *SetupFacts, _ *uint64) { facts.BurnIncreaseMultQ64 = "1.26" }, want: "positive Q64"},
		{name: "multiplier below one", mutate: func(_ *ResolvedConfig, facts *SetupFacts, _ *uint64) {
			facts.BurnIncreaseMultQ64 = "18446744073709551615"
		}, want: "one-through-two"},
		{name: "multiplier above two", mutate: func(_ *ResolvedConfig, facts *SetupFacts, _ *uint64) {
			facts.BurnIncreaseMultQ64 = "36893488147419103233"
		}, want: "one-through-two"},
	}
	for _, test := range tests {
		cfg := testResolvedConfig(t)
		facts := testSetupFacts()
		limit := cfg.Config.Budgets.MaximumRegistrationBurnRao
		test.mutate(cfg, facts, &limit)
		if err := validateRegistrationEconomics(cfg, facts, limit); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error=%v, want substring %q", test.name, err, test.want)
		}
	}
}

func TestRegistrationRoleFundingPreservesRuntimeKeepAliveBalance(t *testing.T) {
	const burnLimitRao, feeLimitRao, keepAliveRao = uint64(1_000_000), uint64(3_000_000), uint64(500)
	legacyFunding := burnLimitRao + feeLimitRao
	if legacyFunding-feeLimitRao-keepAliveRao >= burnLimitRao {
		t.Fatal("legacy burn-plus-fee funding unexpectedly satisfies the runtime preserve check")
	}
	funding, err := registrationRoleFunding(burnLimitRao, feeLimitRao, keepAliveRao)
	if err != nil || funding != 4_000_500 {
		t.Fatalf("registration funding=%d error=%v, want 4000500", funding, err)
	}
	if funding-feeLimitRao-keepAliveRao < burnLimitRao {
		t.Fatal("corrected funding does not leave the full burn spendable after the fee and keep-alive reserve")
	}
	if _, err := registrationRoleFunding(math.MaxUint64, 1, 0); err == nil || !strings.Contains(err.Error(), "burn and fee") {
		t.Fatalf("burn/fee overflow was accepted: %v", err)
	}
	if _, err := registrationRoleFunding(math.MaxUint64-1, 1, 1); err == nil || !strings.Contains(err.Error(), "keep-alive") {
		t.Fatalf("keep-alive overflow was accepted: %v", err)
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
	// Model a transfer from this wallet-owned source to a different
	// wallet-owned position. The source position and its transferable capacity
	// move together while the wallet-wide stake remains internally consistent.
	observationOnly.AlphaAvailableRao--
	observationOnly.AlphaTransferableRao--
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
	if a.PlanHash != c.PlanHash {
		t.Fatal("moving spot burn changed the v2 approval hash despite the runtime-enforced registration ceiling")
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
	roleCount := uint64(5 + 3*cfg.Config.Topology.Operators + cfg.Config.Topology.ChurnFloorUIDs + cfg.Config.Topology.Validators + 2*cfg.Config.Topology.fleetCandidates())
	if a.PlanHash == d.PlanHash || d.MaximumSpend.TAORao-a.MaximumSpend.TAORao != roleCount {
		t.Fatalf("existential-deposit drift was not bound exactly: hashes=%s/%s tao=%d/%d roles=%d", a.PlanHash, d.PlanHash, a.MaximumSpend.TAORao, d.MaximumSpend.TAORao, roleCount)
	}
}

func TestValidatePlanBudgetRejectsMismatchedActionIntent(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	for _, mutation := range []func(*SetupPlan){
		func(plan *SetupPlan) { plan.Actions[0].Description += " changed" },
		func(plan *SetupPlan) { plan.Actions[0].IntentHash = "0x" + strings.Repeat("12", 32) },
	} {
		plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		mutation(plan)
		if err := validatePlanBudget(plan); err == nil || !strings.Contains(err.Error(), "intent hash") {
			t.Errorf("mismatched action intent was accepted: %v", err)
		}
	}
}

func TestValidateV8PlanRejectsEveryAlphaTransferEnvelopeDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	mutations := []func(*Action){
		func(a *Action) { a.Parameters["exact_amount_rao"] = "1" },
		func(a *Action) { a.Parameters["campaign_requirement_rao"] = strconv.FormatUint(a.Spend.AlphaRao+1, 10) },
		func(a *Action) { a.Parameters["minimum_alpha_at_approved_price_rao"] = "1" },
		func(a *Action) { a.Parameters["approved_alpha_price_q9"] = "1" },
		func(a *Action) { a.Parameters["runtime_default_min_transfer_tao_rao"] = "1" },
		func(a *Action) { a.Parameters["minimum_tao_equivalent_margin_bps"] = "1" },
	}
	for index, mutate := range mutations {
		plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		for actionIndex := range plan.Actions {
			if plan.Actions[actionIndex].ID != "alpha.transfer.operator-deposit.1" {
				continue
			}
			mutate(&plan.Actions[actionIndex])
			plan.Actions[actionIndex].IntentHash, _ = actionIntentHash(plan.Actions[actionIndex])
			break
		}
		if err := validatePlanBudget(plan); err == nil || !strings.Contains(err.Error(), "runtime floor and exact spend") {
			t.Fatalf("alpha envelope mutation %d was accepted: %v", index, err)
		}
	}
}

func TestValidatePlanBudgetRejectsMismatchedMaximumSpend(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	for _, mutation := range []func(*Spend){
		func(spend *Spend) { spend.TAORao-- },
		func(spend *Spend) { spend.AlphaRao-- },
		func(spend *Spend) { spend.EVMGasWei = decimalUint64(1) },
		func(spend *Spend) { spend.Registrations-- },
	} {
		plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		mutation(&plan.MaximumSpend)
		if err := validatePlanBudget(plan); err == nil || !strings.Contains(err.Error(), "does not equal plan maximum") {
			t.Errorf("mismatched maximum spend was accepted: %v", err)
		}
	}
}

func TestMaximumActionSpendRejectsEveryIntegerOverflow(t *testing.T) {
	tests := []struct {
		name    string
		actions []Action
		want    string
	}{
		{name: "tao", actions: []Action{{Spend: Spend{TAORao: math.MaxUint64}}, {Spend: Spend{TAORao: 1}}}, want: "TAO"},
		{name: "alpha", actions: []Action{{Spend: Spend{AlphaRao: math.MaxUint64}}, {Spend: Spend{AlphaRao: 1}}}, want: "alpha"},
		{name: "registrations", actions: []Action{{Spend: Spend{Registrations: math.MaxUint32}}, {Spend: Spend{Registrations: 1}}}, want: "registration"},
		{name: "subnet creations", actions: []Action{{Spend: Spend{SubnetCreations: math.MaxUint32}}, {Spend: Spend{SubnetCreations: 1}}}, want: "subnet-creation"},
	}
	for _, test := range tests {
		if _, err := maximumActionSpend(test.actions); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s overflow error=%v, want substring %q", test.name, err, test.want)
		}
	}
}

func TestMaximumActionSpendSupportsAggregateEVMWeiBeyondUint64(t *testing.T) {
	actions := []Action{
		{Spend: Spend{EVMGasWei: decimalUint64(math.MaxUint64)}},
		{Spend: Spend{EVMGasWei: DecimalUint("100000000000000000000")}},
	}
	maximum, err := maximumActionSpend(actions)
	if err != nil || maximum.EVMGasWei != DecimalUint("118446744073709551615") {
		t.Fatalf("arbitrary-precision gas maximum = %s, %v", maximum.EVMGasWei, err)
	}
}

func TestPlanFundsEveryEVMSignerForItsExplicitWorstCaseBeforeCampaignShare(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]Action{}
	required := map[string]DecimalUint{
		"deployer": decimalUint64(0), "owner": decimalUint64(0), "guardian": decimalUint64(0),
		"commitment-oracle": decimalUint64(0), "keeper": decimalUint64(0),
		"operator-1-deposit": decimalUint64(0), "operator-2-deposit": decimalUint64(0),
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		required[fmt.Sprintf("operator-%d-root", operator)] = decimalUint64(0)
		required[fmt.Sprintf("operator-%d-claim-relayer", operator)] = decimalUint64(0)
	}
	addRequired := func(role string, amount DecimalUint) {
		updated, addErr := addDecimalUint(required[role], amount)
		if addErr != nil {
			t.Fatal(addErr)
		}
		required[role] = updated
	}
	for _, action := range plan.Actions {
		actions[action.ID] = action
		if action.Kind == "evm-transaction" {
			maximumGasUnits, maximumFeePerGas, envelopeErr := evmActionFeeEnvelope(action)
			if envelopeErr != nil || maximumGasUnits == 0 || maximumFeePerGas != plan.MaximumEVMFeePerGasWei {
				t.Fatalf("action %s EVM envelope = %d/%d, %v", action.ID, maximumGasUnits, maximumFeePerGas, envelopeErr)
			}
		}
		switch {
		case action.ID == "evm.coordinator-upgrade-activate" || action.ID == "policy.schedule-bootstrap":
			addRequired("owner", action.Spend.EVMGasWei)
		case action.Kind == "evm-transaction" && strings.HasPrefix(action.ID, "evm."):
			addRequired("deployer", action.Spend.EVMGasWei)
		case action.Kind == "evm-transaction" && strings.HasPrefix(action.ID, "precompile."):
			addRequired("deployer", action.Spend.EVMGasWei)
		case strings.HasPrefix(action.ID, "operator.register.") || action.ID == "production.schedule-policy":
			addRequired("owner", action.Spend.EVMGasWei)
		case strings.HasPrefix(action.ID, "operator.deposit.register."):
			addRequired(fmt.Sprintf("operator-%d-deposit", suffixInt(action.ID)), action.Spend.EVMGasWei)
		case strings.HasPrefix(action.ID, "fleet.mirror."):
			addRequired("commitment-oracle", action.Spend.EVMGasWei)
		case strings.HasPrefix(action.ID, "fleet.bind."):
			addRequired("keeper", action.Spend.EVMGasWei)
		case action.ID == "campaign.voluntary-conviction.1":
			addRequired("operator-1-deposit", action.Spend.EVMGasWei)
		case action.ID == dishonestDepositActionID:
			addRequired("operator-2-deposit", action.Spend.EVMGasWei)
		case action.ID == "governance.guardian-pause" || action.ID == "governance.guardian-unpause":
			addRequired("guardian", action.Spend.EVMGasWei)
		case strings.HasPrefix(action.ID, "governance."):
			addRequired("owner", action.Spend.EVMGasWei)
		}
	}
	addRequired("owner", actions["retirement.evm-gas-reserve"].Spend.EVMGasWei)
	if gasUnits, _, err := evmActionFeeEnvelope(actions["evm.reserve-sink"]); err != nil || gasUnits != 600_000 {
		t.Fatalf("reserve deployment gas units = %d, %v", gasUnits, err)
	}
	nonGasRao := map[string]uint64{
		"deployer":           cfg.Config.Budgets.MaximumRegistrationBurnRao + plan.LiveFacts.ProbeTAORao,
		"owner":              uint64(cfg.Config.Topology.Operators) * cfg.Config.Budgets.MaximumRegistrationBurnRao,
		"operator-1-deposit": cfg.Config.Budgets.MaximumRegistrationBurnRao,
		"operator-2-deposit": cfg.Config.Budgets.MaximumRegistrationBurnRao,
	}
	for role, requiredWei := range required {
		funding := actions["evm.fund-"+role]
		usableRao, parseErr := strconv.ParseUint(funding.Parameters["usable_evm_rao"], 10, 64)
		if parseErr != nil || usableRao < nonGasRao[role] {
			t.Fatalf("role %s usable funding = %d, %v", role, usableRao, parseErr)
		}
		availableWei := multiplyUint64Decimal(usableRao-nonGasRao[role], evmWeiPerRao)
		comparison, compareErr := availableWei.Cmp(requiredWei)
		if compareErr != nil || comparison < 0 || requiredWei.IsZero() && availableWei.IsZero() {
			t.Errorf("role %s gas funding %s is below explicit action ceilings %s: %v", role, availableWei, requiredWei, compareErr)
		}
	}
}

func TestSetupEVMGasUnitLimitsCoverLockedAndLiveEstimatesAfterManagerPadding(t *testing.T) {
	cfg := testResolvedConfig(t)
	limits := setupEVMGasUnitLimits(cfg)
	observations := []struct {
		id     string
		rawGas uint64
	}{
		{id: "evm.reserve-sink", rawGas: 418_811},
		{id: "evm.settlement-vault", rawGas: 2_253_684},
		{id: "evm.coordinator-implementation", rawGas: 4_577_466},
		{id: "evm.vault-register-escrow", rawGas: 186_405},
		{id: "evm.coordinator-proxy", rawGas: 510_239},
		{id: "evm.governance-drill-implementation", rawGas: 4_762_841},
		{id: "evm.vault-fix-coordinator", rawGas: 32_321},
		{id: "evm.sink-fix-recorder", rawGas: 49_638},
		{id: "operator.deposit.register.1", rawGas: 126_776},
		{id: "operator.register.1", rawGas: 515_196},
		// Maximum-size helper transactions replace the 1,000 serialized
		// head-fleet writes. These raw ceilings include the complete 10x4
		// install/refresh state transition before manager padding.
		{id: "fleet.refresh.deploy-batcher", rawGas: 818_937},
		{id: "fleet.install.batch.1", rawGas: 8_615_031},
		{id: "fleet.refresh.batch.1", rawGas: 8_160_111},
	}
	for _, observation := range observations {
		padded, err := paddedEVMGas(observation.rawGas)
		if err != nil || limits[observation.id] < padded {
			t.Errorf("%s unit limit %d is below padded observation %d from raw %d: %v", observation.id, limits[observation.id], padded, observation.rawGas, err)
		}
	}
}

func TestFleetCommitmentActionBindsFixedWidthStorageDecoder(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"fleet.commitment.1", "fleet.mirror.1"} {
		action := actionByID(t, plan, id)
		if action.Parameters[fleetCommitmentStorageParameter] != fleetCommitmentStorageV2 {
			t.Errorf("%s storage schema=%q, want %q", id, action.Parameters[fleetCommitmentStorageParameter], fleetCommitmentStorageV2)
			continue
		}

		legacy := action
		legacy.Parameters = cloneStrings(action.Parameters)
		legacy.Parameters[fleetCommitmentStorageParameter] = "runtime-452-registration-fixed-u32-v1"
		legacy.IntentHash, err = actionIntentHash(legacy)
		if err != nil {
			t.Fatal(err)
		}
		if legacy.IntentHash == action.IntentHash || actionAcceptsIntent(action, legacy.IntentHash) {
			t.Errorf("%s carried a legacy type-confused observation", id)
		}
	}
}

func TestLegacyPlanHashStillBindsSpotBurnForStoredV1Compatibility(t *testing.T) {
	plan := &SetupPlan{Schema: "urnetwork-sim-plan-v1", LiveFacts: SetupFacts{BurnRao: 500_000}}
	first, err := plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	plan.LiveFacts.BurnRao++
	second, err := plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("legacy v1 spot burn ceased to be hash-bound")
	}
}

func TestCoordinatorUpgradeBaselineCheckpointIsReviewTimeObservation(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	deploymentHash, err := contractDeploymentIdentityHash(plan.Deployment)
	if err != nil {
		t.Fatal(err)
	}
	plan.PriorPlanHashes = []string{"0x" + strings.Repeat("aa", 32)}
	plan.CoordinatorUpgradeBaseline = CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v1", PriorDeploymentHash: deploymentHash,
		ReleaseDeploymentHash: deploymentHash, ReboundDeploymentHash: deploymentHash,
		ReserveSinkExecutableHash: "0x" + strings.Repeat("11", 32), SettlementVaultExecutableHash: "0x" + strings.Repeat("22", 32),
		GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
		DeployerNonce: plan.Deployment.InitialNonce + 8, ProbeAddressEmpty: true,
		FinalizedBlock: 100, FinalizedBlockHash: "0x" + strings.Repeat("33", 32),
	}
	first, err := plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	plan.CoordinatorUpgradeBaseline.FinalizedBlock = 200
	plan.CoordinatorUpgradeBaseline.FinalizedBlockHash = "0x" + strings.Repeat("44", 32)
	second, err := plan.hash()
	if err != nil || first != second {
		t.Fatalf("advancing baseline checkpoint changed approval: %s %s %v", first, second, err)
	}
	plan.CoordinatorUpgradeBaseline.ReserveSinkExecutableHash = "0x" + strings.Repeat("55", 32)
	third, err := plan.hash()
	if err != nil || third == second {
		t.Fatalf("semantic baseline evidence was not approval-bound: %s %s %v", second, third, err)
	}
	plan.CoordinatorUpgradeBaseline.ReserveSinkExecutableHash = "0x" + strings.Repeat("11", 32)
	plan.PlanHash = first
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := persistedSetupPlanHash(encoded, plan.Schema)
	if err != nil || persisted != first {
		t.Fatalf("persisted baseline hash=%s want=%s: %v", persisted, first, err)
	}
}

func TestPersistedV3PlanHashSurvivesV4WireFields(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	historical := historicalPlanFromCurrent(current, "urnetwork-sim-plan-v3")
	historical.PlanHash, err = historicalPlanHash(historical)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(historical, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := persistedSetupPlanHash(encoded, historical.Schema)
	if err != nil || got != historical.PlanHash {
		t.Fatalf("persisted historical hash = %s, want %s: %v", got, historical.PlanHash, err)
	}
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "plan.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readPersistedPlan(stateDir)
	if err != nil {
		t.Fatalf("valid interrupted v3 plan was not loadable after v4: %v", err)
	}
	if loaded.Schema != historical.Schema || loaded.PlanHash != historical.PlanHash || loaded.Deployment.Schema != "" || loaded.LiveFacts.DeployerNonce != 0 {
		t.Fatalf("loaded historical plan gained v4 identity: %+v", loaded)
	}
}

func TestPersistedHistoricalPlanHashNormalizesOnlySchemaObservations(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"urnetwork-sim-plan-v1", "urnetwork-sim-plan-v2", "urnetwork-sim-plan-v3"} {
		historical := historicalPlanFromCurrent(current, schema)
		if schema == "urnetwork-sim-plan-v1" {
			historical.NativeTransactionFeeLimitRao = 0
			historical.MaximumEVMFeePerGasWei = 0
			historical.BootstrapBurnHalfLifeBlocks = 0
			historical.ProductionBurnHalfLifeBlocks = 0
			historical.LiveFacts.MinBurnRao = 0
			historical.LiveFacts.MaxBurnRao = 0
			historical.LiveFacts.BurnHalfLifeBlocks = 0
			historical.LiveFacts.BurnIncreaseMultQ64 = ""
		}
		if schema == "urnetwork-sim-plan-v2" {
			historical.MaximumEVMFeePerGasWei = 0
		}
		historical.PlanHash, err = historicalPlanHash(historical)
		if err != nil {
			t.Fatal(err)
		}
		encoded, marshalErr := json.MarshalIndent(historical, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		got, hashErr := persistedSetupPlanHash(encoded, schema)
		if hashErr != nil || got != historical.PlanHash {
			t.Errorf("%s historical hash = %s, want %s: %v", schema, got, historical.PlanHash, hashErr)
		}
		historical.Actions[0].Description += " tampered"
		tampered, marshalErr := json.Marshal(historical)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		got, hashErr = persistedSetupPlanHash(tampered, schema)
		if hashErr != nil {
			t.Errorf("%s tampered plan could not be hashed: %v", schema, hashErr)
		} else if got == historical.PlanHash {
			t.Errorf("%s executable mutation retained its historical hash", schema)
		}
	}
}

func TestPersistedV4HashPreservesPreV5WireShape(t *testing.T) {
	legacy := []byte(`{"schema":"urnetwork-sim-plan-v4","live_facts":{"burn_rao":5,"alpha_available_rao":9,"wallet_free_tao_rao":10,"finalized_block":11,"finalized_block_hash":"0xaa","evm_finalized_block":12,"evm_finalized_block_hash":"0xbb"},"plan_hash":"0xstored","bound":"yes"}`)
	normalized := []byte(`{"schema":"urnetwork-sim-plan-v4","live_facts":{"burn_rao":0,"alpha_available_rao":0,"wallet_free_tao_rao":0,"finalized_block":0,"finalized_block_hash":"","evm_finalized_block":0,"evm_finalized_block_hash":""},"plan_hash":"","bound":"yes"}`)
	digest := sha256.Sum256(normalized)
	want := fmt.Sprintf("0x%x", digest)
	got, err := persistedSetupPlanHash(legacy, "urnetwork-sim-plan-v4")
	if err != nil || got != want {
		t.Fatalf("pre-v5 persisted hash = %s, want %s: %v", got, want, err)
	}

	withCapacity := []byte(`{"schema":"urnetwork-sim-plan-v4","live_facts":{"burn_rao":5,"alpha_available_rao":9,"alpha_transferable_rao":123,"wallet_free_tao_rao":10,"finalized_block":11,"finalized_block_hash":"0xaa","evm_finalized_block":12,"evm_finalized_block_hash":"0xbb"},"plan_hash":"0xstored","bound":"yes"}`)
	withZeroCapacity := []byte(`{"schema":"urnetwork-sim-plan-v4","live_facts":{"burn_rao":5,"alpha_available_rao":9,"alpha_transferable_rao":0,"wallet_free_tao_rao":10,"finalized_block":11,"finalized_block_hash":"0xaa","evm_finalized_block":12,"evm_finalized_block_hash":"0xbb"},"plan_hash":"0xstored","bound":"yes"}`)
	added, err := persistedSetupPlanHash(withCapacity, "urnetwork-sim-plan-v4")
	if err != nil {
		t.Fatal(err)
	}
	addedZero, err := persistedSetupPlanHash(withZeroCapacity, "urnetwork-sim-plan-v4")
	if err != nil {
		t.Fatal(err)
	}
	if added != addedZero {
		t.Fatal("present capacity observation was not normalized")
	}
	if added == got {
		t.Fatal("adding a zero-valued post-v4 field did not change the authenticated historical wire shape")
	}
}

func TestLegacyArchivedPlanHashRepairsOnlyInjectedZeroDefaultMinimum(t *testing.T) {
	original := []byte(`{"schema":"urnetwork-sim-plan-v6","live_facts":{"burn_rao":5,"alpha_available_rao":9,"wallet_free_tao_rao":10,"finalized_block":11,"finalized_block_hash":"0xaa","evm_finalized_block":12,"evm_finalized_block_hash":"0xbb"},"plan_hash":"0xstored","bound":"yes"}`)
	want, err := persistedSetupPlanHash(original, "urnetwork-sim-plan-v6")
	if err != nil {
		t.Fatal(err)
	}
	archived := []byte(strings.Replace(string(original), `"wallet_free_tao_rao":10`, `"wallet_free_tao_rao":10,"default_min_transfer_rao":0`, 1))
	direct, err := persistedSetupPlanHash(archived, "urnetwork-sim-plan-v6")
	if err != nil {
		t.Fatal(err)
	}
	if direct == want {
		t.Fatal("injected historical field unexpectedly retained the original wire digest")
	}
	repaired, applicable, err := legacyArchivedSetupPlanHash(archived, "urnetwork-sim-plan-v6")
	if err != nil || !applicable || repaired != want {
		t.Fatalf("legacy archive repair = (%s, %t), want (%s, true): %v", repaired, applicable, want, err)
	}

	nonzero := []byte(strings.Replace(string(archived), `"default_min_transfer_rao":0`, `"default_min_transfer_rao":1`, 1))
	if got, ok, err := legacyArchivedSetupPlanHash(nonzero, "urnetwork-sim-plan-v6"); err != nil || ok || got != "" {
		t.Fatalf("nonzero historical field was repairable: (%s, %t, %v)", got, ok, err)
	}
	if got, ok, err := legacyArchivedSetupPlanHash(archived, "urnetwork-sim-plan-v7"); err != nil || ok || got != "" {
		t.Fatalf("current-envelope field was repairable: (%s, %t, %v)", got, ok, err)
	}
	tampered := []byte(strings.Replace(string(archived), `"bound":"yes"`, `"bound":"no"`, 1))
	if got, ok, err := legacyArchivedSetupPlanHash(tampered, "urnetwork-sim-plan-v6"); err != nil || !ok || got == want {
		t.Fatalf("semantic mutation repaired to the approved hash: (%s, %t, %v)", got, ok, err)
	}

	v4Original := []byte(`{"schema":"urnetwork-sim-plan-v4","live_facts":{"burn_rao":5,"alpha_available_rao":9,"wallet_free_tao_rao":10,"finalized_block":11,"finalized_block_hash":"0xaa","evm_finalized_block":12,"evm_finalized_block_hash":"0xbb"},"plan_hash":"0xstored","bound":"yes"}`)
	v4Want, err := persistedSetupPlanHash(v4Original, "urnetwork-sim-plan-v4")
	if err != nil {
		t.Fatal(err)
	}
	injectedAlphaFacts := `"alpha_transferable_rao":0,"alpha_source_stored_lock_rao":0,"alpha_source_collateral_rao":0,"wallet_netuid_alpha_rao":0,"wallet_netuid_collateral_rao":0,"initial_min_stake_rao":0,"alpha_price_tao_per_alpha_q9":0,"registered_alpha_rao":0,"reserve_validator_alpha_rao":0,"independent_validator_alpha_rao":0,"alpha_source_registered":false,`
	v4Archived := []byte(strings.Replace(string(v4Original), `"wallet_free_tao_rao":10,`, `"wallet_free_tao_rao":10,`+injectedAlphaFacts, 1))
	v4Repaired, applicable, err := legacyArchivedSetupPlanHash(v4Archived, "urnetwork-sim-plan-v4")
	if err != nil || !applicable || v4Repaired != v4Want {
		t.Fatalf("pre-v5 archive repair = (%s, %t), want (%s, true): %v", v4Repaired, applicable, v4Want, err)
	}
	for _, mutation := range []struct {
		name string
		from string
		to   string
	}{
		{name: "transferable", from: `"alpha_transferable_rao":0`, to: `"alpha_transferable_rao":1`},
		{name: "stored lock", from: `"alpha_source_stored_lock_rao":0`, to: `"alpha_source_stored_lock_rao":1`},
		{name: "collateral", from: `"alpha_source_collateral_rao":0`, to: `"alpha_source_collateral_rao":1`},
		{name: "wallet alpha", from: `"wallet_netuid_alpha_rao":0`, to: `"wallet_netuid_alpha_rao":1`},
		{name: "wallet collateral", from: `"wallet_netuid_collateral_rao":0`, to: `"wallet_netuid_collateral_rao":1`},
		{name: "initial minimum", from: `"initial_min_stake_rao":0`, to: `"initial_min_stake_rao":1`},
		{name: "price", from: `"alpha_price_tao_per_alpha_q9":0`, to: `"alpha_price_tao_per_alpha_q9":1`},
		{name: "registered alpha", from: `"registered_alpha_rao":0`, to: `"registered_alpha_rao":1`},
		{name: "reserve alpha", from: `"reserve_validator_alpha_rao":0`, to: `"reserve_validator_alpha_rao":1`},
		{name: "independent alpha", from: `"independent_validator_alpha_rao":0`, to: `"independent_validator_alpha_rao":1`},
		{name: "registered source", from: `"alpha_source_registered":false`, to: `"alpha_source_registered":true`},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			mutated := []byte(strings.Replace(string(v4Archived), mutation.from, mutation.to, 1))
			if got, ok, err := legacyArchivedSetupPlanHash(mutated, "urnetwork-sim-plan-v4"); err != nil || ok || got != "" {
				t.Fatalf("nonzero injected field was repairable: (%s, %t, %v)", got, ok, err)
			}
		})
	}

	v5Original := []byte(`{"schema":"urnetwork-sim-plan-v5","live_facts":{"burn_rao":5,"alpha_available_rao":9,"wallet_free_tao_rao":10,"finalized_block":11,"finalized_block_hash":"0xaa","evm_finalized_block":12,"evm_finalized_block_hash":"0xbb"},"plan_hash":"0xstored","bound":"yes"}`)
	v5Want, err := persistedSetupPlanHash(v5Original, "urnetwork-sim-plan-v5")
	if err != nil {
		t.Fatal(err)
	}
	zeroUpgrade := `{"schema":"","deployment_id":"","implementation":"0x0000000000000000000000000000000000000000","deployer_nonce":0,"runtime_code_hash":""}`
	zeroUpgradeBaseline := `{"schema":"","prior_deployment_hash":"","release_deployment_hash":"","rebound_deployment_hash":"","reserve_sink_executable_hash":"","settlement_vault_executable_hash":"","governance_drill_version":"","governance_proxiable_uuid":"","deployer_nonce":0,"probe_address_empty":false,"finalized_block":0,"finalized_block_hash":""}`
	v5Archived := strings.Replace(string(v5Original), `},"plan_hash"`, `},"coordinator_upgrade":`+zeroUpgrade+`,"coordinator_upgrade_baseline":`+zeroUpgradeBaseline+`,"plan_hash"`, 1)
	v5Repaired, applicable, err := legacyArchivedSetupPlanHash([]byte(v5Archived), "urnetwork-sim-plan-v5")
	if err != nil || !applicable || v5Repaired != v5Want {
		t.Fatalf("pre-v6 archive repair = (%s, %t), want (%s, true): %v", v5Repaired, applicable, v5Want, err)
	}
	for _, mutation := range []struct {
		name string
		from string
		to   string
	}{
		{name: "upgrade implementation", from: `"implementation":"0x0000000000000000000000000000000000000000"`, to: `"implementation":"0x0000000000000000000000000000000000000001"`},
		{name: "upgrade runtime", from: `"runtime_code_hash":""`, to: `"runtime_code_hash":"0x01"`},
		{name: "baseline deployment", from: `"prior_deployment_hash":""`, to: `"prior_deployment_hash":"0x01"`},
		{name: "baseline executable", from: `"reserve_sink_executable_hash":""`, to: `"reserve_sink_executable_hash":"0x01"`},
		{name: "baseline probe", from: `"probe_address_empty":false`, to: `"probe_address_empty":true`},
		{name: "baseline block", from: `"finalized_block":0`, to: `"finalized_block":1`},
		{name: "baseline block hash", from: `"finalized_block_hash":""`, to: `"finalized_block_hash":"0x01"`},
	} {
		t.Run("pre-v6 "+mutation.name, func(t *testing.T) {
			mutated := []byte(strings.Replace(v5Archived, mutation.from, mutation.to, 1))
			if got, ok, err := legacyArchivedSetupPlanHash(mutated, "urnetwork-sim-plan-v5"); err != nil || ok || got != "" {
				t.Fatalf("nonzero injected field was repairable: (%s, %t, %v)", got, ok, err)
			}
		})
	}

	v3Original := []byte(`{"schema":"urnetwork-sim-plan-v3","live_facts":{"burn_rao":5,"alpha_available_rao":9,"wallet_free_tao_rao":10,"finalized_block":11,"finalized_block_hash":"0xaa"},"plan_hash":"0xstored","bound":"yes"}`)
	v3Want, err := persistedSetupPlanHash(v3Original, "urnetwork-sim-plan-v3")
	if err != nil {
		t.Fatal(err)
	}
	zeroDeployment := `{"schema":"","deployment_id":"","initial_nonce":0,"reserve_sink":"0x0000000000000000000000000000000000000000","settlement_vault":"0x0000000000000000000000000000000000000000","coordinator_implementation":"0x0000000000000000000000000000000000000000","coordinator_proxy":"0x0000000000000000000000000000000000000000","governance_drill_implementation":"0x0000000000000000000000000000000000000000","precompile_probe":"0x0000000000000000000000000000000000000000"}`
	zeroSupersededSpend := `{"tao_rao":0,"alpha_rao":0,"evm_gas_wei":0,"registrations":0,"subnet_creations":0}`
	v3Archived := strings.Replace(string(v3Original), `"wallet_free_tao_rao":10,`, `"wallet_free_tao_rao":10,"deployer_nonce":0,"evm_finalized_block":0,"evm_finalized_block_hash":"",`, 1)
	v3Archived = strings.Replace(v3Archived, `},"plan_hash"`, `},"deployment":`+zeroDeployment+`,"superseded_spend":`+zeroSupersededSpend+`,"plan_hash"`, 1)
	v3Repaired, applicable, err := legacyArchivedSetupPlanHash([]byte(v3Archived), "urnetwork-sim-plan-v3")
	if err != nil || !applicable || v3Repaired != v3Want {
		t.Fatalf("pre-v4 archive repair = (%s, %t), want (%s, true): %v", v3Repaired, applicable, v3Want, err)
	}
	for _, mutation := range []struct {
		name string
		from string
		to   string
	}{
		{name: "deployer nonce", from: `"deployer_nonce":0`, to: `"deployer_nonce":1`},
		{name: "EVM block", from: `"evm_finalized_block":0`, to: `"evm_finalized_block":1`},
		{name: "EVM block hash", from: `"evm_finalized_block_hash":""`, to: `"evm_finalized_block_hash":"0x01"`},
		{name: "deployment", from: `"initial_nonce":0`, to: `"initial_nonce":1`},
		{name: "superseded spend", from: `"tao_rao":0`, to: `"tao_rao":1`},
	} {
		t.Run("pre-v4 "+mutation.name, func(t *testing.T) {
			mutated := []byte(strings.Replace(v3Archived, mutation.from, mutation.to, 1))
			if got, ok, err := legacyArchivedSetupPlanHash(mutated, "urnetwork-sim-plan-v3"); err != nil || ok || got != "" {
				t.Fatalf("nonzero injected field was repairable: (%s, %t, %v)", got, ok, err)
			}
		})
	}
}

func TestWriteRunInputsArchivesAuthenticatedPlanBytesExactly(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(prior, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	priorBytes := append([]byte("\n"), encoded...)
	priorBytes = append(priorBytes, '\n', '\n')
	stateDir := t.TempDir()
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), priorBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	next := *prior
	next.Actions = append([]Action(nil), prior.Actions...)
	next.Actions[0].Description += " with byte-preserving archival"
	next.Actions[0].IntentHash, err = actionIntentHash(next.Actions[0])
	if err != nil {
		t.Fatal(err)
	}
	next.PlanHash, err = next.hash()
	if err != nil {
		t.Fatal(err)
	}
	if next.PlanHash == prior.PlanHash {
		t.Fatal("test plan mutation retained its approval hash")
	}
	if err := writeRunInputs(cfg, stateDir, &next, roles); err != nil {
		t.Fatal(err)
	}
	archived, err := os.ReadFile(filepath.Join(stateDir, "plans", stringsTrim0x(prior.PlanHash)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archived, priorBytes) {
		t.Fatal("authenticated ancestor was re-serialized while being archived")
	}
	if _, err := decodePersistedPlanBytes(archived); err != nil {
		t.Fatalf("byte-exact archived ancestor no longer authenticates: %v", err)
	}
}

func TestPersistedPlanHashRejectsDuplicateAndMalformedAdjacentObjects(t *testing.T) {
	validFacts := `{"burn_rao":1,"alpha_available_rao":2,"wallet_free_tao_rao":3,"finalized_block":4,"finalized_block_hash":"0x01"}`
	tests := []string{
		`{"plan_hash":"x","plan_hash":"y","live_facts":` + validFacts + `}`,
		`{"plan_hash":"x","live_facts":{"burn_rao":1,"burn_rao":2,"alpha_available_rao":2,"wallet_free_tao_rao":3,"finalized_block":4,"finalized_block_hash":"0x01"}}`,
		`{"plan_hash":"x","live_facts":[]}`,
		`{"plan_hash":"x","live_facts":` + validFacts + `} true`,
	}
	for _, encoded := range tests {
		if _, err := persistedSetupPlanHash([]byte(encoded), "urnetwork-sim-plan-v3"); err == nil {
			t.Errorf("malformed persisted plan was accepted: %s", encoded)
		}
	}
	_, err := persistedSetupPlanHash([]byte(`{"plan_hash":"x","live_facts":`+validFacts+`} true`), "urnetwork-sim-plan-v3")
	if err == nil || !strings.Contains(err.Error(), "trailing value true") || strings.Contains(err.Error(), "%!") {
		t.Fatalf("trailing persisted value error is not actionable: %v", err)
	}
}

func TestPersistedV4HashMatchesTypedPlanHash(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := persistedSetupPlanHash(encoded, plan.Schema)
	if err != nil || got != plan.PlanHash {
		t.Fatalf("persisted v4 hash = %s, want %s: %v", got, plan.PlanHash, err)
	}
}

func TestPlanHashBindsTheRegistrationEconomicsEnvelope(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	baseline, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*SetupFacts)
	}{
		{name: "minimum", mutate: func(facts *SetupFacts) { facts.MinBurnRao++; facts.BurnRao++ }},
		{name: "maximum", mutate: func(facts *SetupFacts) { facts.MaxBurnRao-- }},
		{name: "half-life", mutate: func(facts *SetupFacts) { facts.BurnHalfLifeBlocks = 1 }},
		{name: "multiplier", mutate: func(facts *SetupFacts) { facts.BurnIncreaseMultQ64 = "23242897532874035200" }},
	}
	for _, test := range tests {
		facts := testSetupFacts()
		test.mutate(facts)
		changed, err := buildPlan(cfg, facts, roles, time.Unix(1, 0))
		if err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if changed.PlanHash == baseline.PlanHash {
			t.Errorf("%s drift did not change the v2 plan hash", test.name)
		}
	}
}

func TestPlanHashAndFundingBindNativeTransactionFeeLimit(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	first, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Budgets.MaximumNativeTransactionFeeRao++
	second, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanHash == second.PlanHash || second.NativeTransactionFeeLimitRao != first.NativeTransactionFeeLimitRao+1 {
		t.Fatalf("native fee drift did not change the plan: %s/%s limits=%d/%d", first.PlanHash, second.PlanHash, first.NativeTransactionFeeLimitRao, second.NativeTransactionFeeLimitRao)
	}
	wantIncrease := uint64(cfg.Config.Topology.ChurnFloorUIDs + cfg.Config.Topology.fleetCandidates() + cfg.Config.Topology.Validators)
	// Every registration coldkey and every source-wallet fee reservation grows
	// with the ceiling; commitment hotkeys add their own bounded writes too.
	if second.MaximumSpend.TAORao <= first.MaximumSpend.TAORao+wantIncrease {
		t.Fatalf("native fee drift was not propagated through adjacent reserves: %d -> %d", first.MaximumSpend.TAORao, second.MaximumSpend.TAORao)
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

func TestRuntimeConfigActionsBindTheApprovedConfigAndPolicy(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"config.render", "topology.launch"} {
		index := slices.IndexFunc(plan.Actions, func(action Action) bool { return action.ID == id })
		if index < 0 {
			t.Fatalf("action %s is missing", id)
		}
		action := plan.Actions[index]
		if action.Parameters["config_hash"] != plan.ConfigHash || action.Parameters["policy_hash"] != plan.PolicyHash {
			t.Fatalf("action %s lacks runtime identity: %+v", id, action.Parameters)
		}
		for _, field := range []string{"config_hash", "policy_hash"} {
			mutated := *plan
			mutated.Actions = append([]Action(nil), plan.Actions...)
			mutated.Actions[index] = action
			mutated.Actions[index].Parameters = make(map[string]string, len(action.Parameters))
			for key, value := range action.Parameters {
				mutated.Actions[index].Parameters[key] = value
			}
			delete(mutated.Actions[index].Parameters, field)
			mutated.Actions[index].IntentHash, err = actionIntentHash(mutated.Actions[index])
			if err != nil {
				t.Fatal(err)
			}
			if err := validatePlanBudget(&mutated); err == nil || !strings.Contains(err.Error(), "does not bind the approved runtime config and policy") {
				t.Fatalf("action %s without %s was accepted: %v", id, field, err)
			}
		}
	}
}

func TestHistoricalConvictionRepairRetainsOnlyItsAuthenticatedPolicy(t *testing.T) {
	oldPolicy := "0x" + strings.Repeat("11", 32)
	originalPlan := "0x" + strings.Repeat("22", 32)
	duplicatePlan := "0x" + strings.Repeat("33", 32)
	plan := &SetupPlan{Schema: currentSetupPlanSchema, PolicyHash: "0x" + strings.Repeat("44", 32)}
	priorPlans := map[string]bool{originalPlan: true, duplicatePlan: true}
	reconciliation := Action{ID: voluntaryConvictionReconciliationActionID, Kind: "evm-reconciliation", Parameters: map[string]string{
		voluntaryRecoveryRepairActionParameter:       "alpha.repair.operator-deposit.1.3",
		voluntaryRecoveryPolicyHashParameter:         oldPolicy,
		voluntaryRecoveryOriginalPlanHashParameter:   originalPlan,
		voluntaryRecoveryDuplicatePlanHashParameter:  duplicatePlan,
		voluntaryRecoveryOriginalIntentHashParameter: "0x" + strings.Repeat("66", 32),
		deploymentManifestHashParameter:              "0x" + strings.Repeat("77", 32),
	}}
	repair := Action{ID: "alpha.repair.operator-deposit.1.3", Parameters: map[string]string{
		alphaRepairForActionParameter:   voluntaryConvictionReconciliationActionID,
		"campaign_policy_hash":          oldPolicy,
		deploymentManifestHashParameter: "0x" + strings.Repeat("77", 32),
	}}
	seen := map[string]Action{reconciliation.ID: reconciliation}
	if !operatorRepairBindsApprovedCampaignPolicy(plan, repair, seen, priorPlans) {
		t.Fatal("authenticated historical conviction repair policy was rejected")
	}
	mutations := []func(*Action, map[string]Action, map[string]bool){
		func(action *Action, _ map[string]Action, _ map[string]bool) {
			action.Parameters["campaign_policy_hash"] = "0x" + strings.Repeat("55", 32)
		},
		func(_ *Action, linked map[string]Action, _ map[string]bool) {
			value := linked[reconciliation.ID]
			value.Kind = "local"
			linked[reconciliation.ID] = value
		},
		func(_ *Action, linked map[string]Action, _ map[string]bool) {
			value := linked[reconciliation.ID]
			value.Parameters[voluntaryRecoveryRepairActionParameter] = "other"
			linked[reconciliation.ID] = value
		},
		func(_ *Action, _ map[string]Action, ancestors map[string]bool) { delete(ancestors, originalPlan) },
	}
	for index, mutate := range mutations {
		candidate := repair
		candidate.Parameters = make(map[string]string, len(repair.Parameters))
		for key, value := range repair.Parameters {
			candidate.Parameters[key] = value
		}
		linkedReconciliation := reconciliation
		linkedReconciliation.Parameters = make(map[string]string, len(reconciliation.Parameters))
		for key, value := range reconciliation.Parameters {
			linkedReconciliation.Parameters[key] = value
		}
		linked := map[string]Action{reconciliation.ID: linkedReconciliation}
		ancestors := map[string]bool{originalPlan: true, duplicatePlan: true}
		mutate(&candidate, linked, ancestors)
		if operatorRepairBindsApprovedCampaignPolicy(plan, candidate, linked, ancestors) {
			t.Errorf("historical repair mutation %d was accepted", index)
		}
	}
	custodyRepair := Action{ID: "alpha.repair.operator-deposit.1.2", Parameters: map[string]string{
		alphaRepairForActionParameter:   "alpha.transfer.operator-deposit.1",
		"campaign_policy_hash":          oldPolicy,
		deploymentManifestHashParameter: "0x" + strings.Repeat("77", 32),
	}}
	plan.Actions = []Action{
		{ID: voluntaryConvictionActionID, IntentHash: reconciliation.Parameters[voluntaryRecoveryOriginalIntentHashParameter], DependsOn: []string{custodyRepair.ID}},
		reconciliation,
	}
	if !operatorRepairBindsApprovedCampaignPolicy(plan, custodyRepair, map[string]Action{}, priorPlans) {
		t.Fatal("verified historical custody prerequisite was not linked through the authenticated conviction")
	}
	plan.Actions[0].DependsOn = nil
	if operatorRepairBindsApprovedCampaignPolicy(plan, custodyRepair, map[string]Action{}, priorPlans) {
		t.Fatal("unreferenced historical custody repair was accepted")
	}
}

func TestPlanHashBindsFinalizedDeployerNonceAndEveryCREATEAddress(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	facts := testSetupFacts()
	facts.DeployerNonce = 17
	first, err := buildPlan(cfg, facts, roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	facts.DeployerNonce++
	second, err := buildPlan(cfg, facts, roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanHash == second.PlanHash || first.Deployment.InitialNonce != 17 || second.Deployment.InitialNonce != 18 || first.Deployment.ReserveSink == second.Deployment.ReserveSink || first.Deployment.CoordinatorProxy == second.Deployment.CoordinatorProxy {
		t.Fatalf("deployer nonce drift did not replace the approved deployment: first=%+v second=%+v", first.Deployment, second.Deployment)
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
		"transferable alpha":  func(_ *ResolvedConfig, f *SetupFacts) { f.AlphaTransferableRao = 1 },
		"coldkey alpha total": func(_ *ResolvedConfig, f *SetupFacts) { f.WalletNetuidAlphaRao = f.AlphaAvailableRao - 1 },
		"tao limit":           func(c *ResolvedConfig, _ *SetupFacts) { c.MaximumTAORao = 1 },
		"gas limit":           func(c *ResolvedConfig, _ *SetupFacts) { c.MaximumEVMGasWei = decimalUint64(1) },
		"registration burn":   func(c *ResolvedConfig, f *SetupFacts) { f.BurnRao = c.Config.Budgets.MaximumRegistrationBurnRao + 1 },
		"existential deposit": func(_ *ResolvedConfig, f *SetupFacts) { f.ExistentialDepositRao = 0 },
		"transfer minimum":    func(_ *ResolvedConfig, f *SetupFacts) { f.DefaultMinTransferRao = 0 },
		"transfer minimum manifest drift": func(_ *ResolvedConfig, f *SetupFacts) {
			f.DefaultMinTransferRao++
		},
		"alpha price":       func(_ *ResolvedConfig, f *SetupFacts) { f.AlphaPriceQ9 = 0 },
		"registered alpha":  func(_ *ResolvedConfig, f *SetupFacts) { f.RegisteredAlphaRao = 0 },
		"registered source": func(_ *ResolvedConfig, f *SetupFacts) { f.AlphaSourceRegistered = false },
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
		"cross-budget spend": func(a *Action) { a.Spend.EVMGasWei = decimalUint64(1) },
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
		Schema: "urnetwork-sim-plan-v1",
		Actions: []Action{
			{ID: "first"},
			{ID: "second", DependsOn: []string{"first"}},
		},
	}
	for index := range base.Actions {
		base.Actions[index].IntentHash, _ = actionIntentHash(base.Actions[index])
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
	forward.Actions[0].IntentHash, _ = actionIntentHash(forward.Actions[0])
	if err := validatePlanBudget(&forward); err == nil || !strings.Contains(err.Error(), "missing or later") {
		t.Fatalf("forward dependency was accepted: %v", err)
	}
}

func TestPlanValidationRejectsEveryMalformedSupersededDeployment(t *testing.T) {
	cfg := testResolvedConfig(t)
	facts := testSetupFacts()
	facts.DeployerNonce = 3
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base, err := buildPlan(cfg, facts, publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	obsolete, err := buildDeploymentPayloads(cfg, roles, 0)
	if err != nil {
		t.Fatal(err)
	}
	obsolete.Manifest.DeployBlock = 120
	obsolete.Manifest.DeployBlockHash = "0x" + strings.Repeat("12", 32)
	base.SupersededDeployments = []ContractDeployment{obsolete.Manifest}
	if err := applySupersededSpend(base, Spend{EVMGasWei: DecimalUint("1")}); err != nil {
		t.Fatal(err)
	}
	base.PlanHash, err = base.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(base); err != nil {
		t.Fatalf("valid superseded deployment history was rejected: %v", err)
	}
	clone := func() *SetupPlan {
		encoded, marshalErr := json.Marshal(base)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var plan SetupPlan
		if unmarshalErr := json.Unmarshal(encoded, &plan); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		return &plan
	}
	tests := []struct {
		name   string
		mutate func(*SetupPlan)
	}{
		{name: "foreign deployment", mutate: func(plan *SetupPlan) { plan.SupersededDeployments[0].DeploymentID = "foreign" }},
		{name: "incomplete checkpoint", mutate: func(plan *SetupPlan) { plan.SupersededDeployments[0].DeployBlockHash = "" }},
		{name: "malformed checkpoint", mutate: func(plan *SetupPlan) { plan.SupersededDeployments[0].DeployBlockHash = "0x12" }},
		{name: "active duplicate", mutate: func(plan *SetupPlan) { plan.SupersededDeployments[0] = plan.Deployment }},
		{name: "non-EVM superseded spend", mutate: func(plan *SetupPlan) { plan.SupersededSpend.TAORao = 1 }},
		{name: "spend without deployment", mutate: func(plan *SetupPlan) { plan.SupersededDeployments = nil }},
	}
	for _, test := range tests {
		plan := clone()
		test.mutate(plan)
		if err := validatePlanBudget(plan); err == nil {
			t.Errorf("%s was accepted", test.name)
		}
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
