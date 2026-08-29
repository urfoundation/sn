package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"time"
)

type Spend struct {
	TAORao          uint64 `json:"tao_rao"`
	AlphaRao        uint64 `json:"alpha_rao"`
	EVMGasWei       uint64 `json:"evm_gas_wei"`
	Registrations   uint32 `json:"registrations"`
	SubnetCreations uint32 `json:"subnet_creations"`
}
type Action struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Target      string            `json:"target"`
	Description string            `json:"description"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	Spend       Spend             `json:"maximum_spend"`
	DependsOn   []string          `json:"depends_on,omitempty"`
	IntentHash  string            `json:"intent_hash"`
}
type SetupPlan struct {
	Schema                   string      `json:"schema"`
	Release                  string      `json:"release"`
	ReleaseLockHash          string      `json:"release_lock_hash"`
	DeploymentID             string      `json:"deployment_id"`
	ChainID                  uint64      `json:"chain_id"`
	GenesisHash              string      `json:"genesis_hash"`
	Netuid                   uint16      `json:"netuid"`
	Owner                    string      `json:"owner"`
	LiveFacts                SetupFacts  `json:"live_facts"`
	RegistrationBurnLimitRao uint64      `json:"registration_burn_limit_rao"`
	ConfigHash               string      `json:"config_hash"`
	ResolvedInputsHash       string      `json:"resolved_inputs_hash"`
	PolicyHash               string      `json:"policy_hash"`
	Roles                    PublicRoles `json:"roles"`
	Actions                  []Action    `json:"actions"`
	MaximumSpend             Spend       `json:"maximum_spend"`
	Limits                   Spend       `json:"limits"`
	PlanHash                 string      `json:"plan_hash"`
	GeneratedAt              string      `json:"generated_at,omitempty"`
}
type PublicRoles struct {
	Deployer, Owner, Guardian, CommitmentOracle string   `json:",omitempty"`
	OperatorDepositSigners                      []string `json:"operator_deposit_signers"`
	OperatorRootSigners                         []string `json:"operator_root_signers"`
	MinerClaimRelayers                          []string `json:"miner_claim_relayers"`
	Keeper                                      string   `json:"keeper"`
}

func BuildPlan(ctx context.Context, cfg *ResolvedConfig) (*SetupPlan, error) {
	doc := RunDoctor(ctx, cfg)
	if err := doc.Error(); err != nil {
		return nil, fmt.Errorf("doctor must pass before planning: %w", err)
	}
	facts, err := ReadSetupFacts(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("read finalized setup facts: %w", err)
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		return nil, err
	}
	return buildPlan(cfg, facts, roles, time.Now().UTC())
}

// Bind resolved non-secret authority, identity, origin, and budget values so a
// vault edit invalidates approval even when its YAML reference stays constant.
func resolvedInputsHash(cfg *ResolvedConfig) (string, error) {
	if cfg == nil {
		return "", errors.New("resolved configuration is unavailable")
	}
	return canonicalHashHex(struct {
		ChainID              uint64
		Netuid               uint16
		PrivateAuthority     string
		OperationalRPCMode   string
		OperationalSubstrate string
		OperationalEVM       string
		ObjectStoreHost      string
		OperatorAPIOrigins   []string
		WalletPublic         string
		WalletHotkeyPublic   string
		MaximumTAORao        uint64
		MaximumAlphaRao      uint64
		MaximumEVMGasWei     uint64
	}{
		ChainID: cfg.ChainID, Netuid: cfg.Netuid, PrivateAuthority: cfg.Authority,
		OperationalRPCMode: cfg.OperationalRPCMode, OperationalSubstrate: cfg.OperationalSubstrate, OperationalEVM: cfg.OperationalEVM,
		ObjectStoreHost:    cfg.ObjectStoreHost,
		OperatorAPIOrigins: append([]string(nil), cfg.OperatorAPIOrigins...),
		WalletPublic:       cfg.WalletPublic, WalletHotkeyPublic: cfg.WalletHotkeyPublic,
		MaximumTAORao: cfg.MaximumTAORao, MaximumAlphaRao: cfg.MaximumAlphaRao,
		MaximumEVMGasWei: cfg.MaximumEVMGasWei,
	})
}

func buildPlan(cfg *ResolvedConfig, facts *SetupFacts, roles PublicRoles, generatedAt time.Time) (*SetupPlan, error) {
	if facts == nil || facts.BurnRao == 0 || facts.AlphaSourceHotkey == "" || facts.ProbeTAORao == 0 {
		return nil, fmt.Errorf("finalized burn, alpha source, and probe-value facts are required")
	}
	registrationBurnLimit := cfg.Config.Budgets.MaximumRegistrationBurnRao
	if registrationBurnLimit == 0 || facts.BurnRao > registrationBurnLimit {
		return nil, fmt.Errorf("finalized registration burn %d exceeds configured per-registration limit %d", facts.BurnRao, registrationBurnLimit)
	}
	if cfg.Release == nil {
		return nil, fmt.Errorf("release lock is required")
	}
	releaseLockHash, err := canonicalHashHex(cfg.Release)
	if err != nil || releaseLockHash == "" {
		return nil, fmt.Errorf("hash release lock: %w", err)
	}
	resolvedHash, err := resolvedInputsHash(cfg)
	if err != nil {
		return nil, fmt.Errorf("hash resolved launch inputs: %w", err)
	}
	p := &SetupPlan{Schema: "urnetwork-sim-plan-v1", Release: "1.0", ReleaseLockHash: releaseLockHash, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: testnetChainID, GenesisHash: testnetGenesis, Netuid: cfg.Netuid, Owner: cfg.WalletPublic, LiveFacts: *facts, RegistrationBurnLimitRao: registrationBurnLimit, ConfigHash: cfg.ConfigHash, ResolvedInputsHash: resolvedHash, PolicyHash: cfg.PolicyHash, Roles: roles, GeneratedAt: generatedAt.Format(time.RFC3339)}
	add := func(a Action) {
		h, _ := canonicalHashHex(struct {
			ID, Kind, Target, Description string
			Parameters                    map[string]string
			Spend                         Spend
			DependsOn                     []string
		}{
			ID:          a.ID,
			Kind:        a.Kind,
			Target:      a.Target,
			Description: a.Description,
			Parameters:  a.Parameters,
			Spend:       a.Spend,
			DependsOn:   a.DependsOn,
		})
		a.IntentHash = h
		p.Actions = append(p.Actions, a)
	}
	registrationParameters := func() map[string]string {
		return map[string]string{"maximum_burn_rao": fmt.Sprint(registrationBurnLimit)}
	}
	add(Action{ID: "subnet.verify-owner", Kind: "substrate-read", Target: fmt.Sprintf("netuid:%d", cfg.Netuid), Description: "verify existing subnet and owner; subnet creation is forbidden"})
	operatorCount := cfg.Config.Topology.Operators
	validatorCount := cfg.Config.Topology.Validators
	depositTotal := cfg.Policy.Deposit.TotalTestCampaignCapRao
	if depositTotal < uint64(operatorCount) {
		return nil, fmt.Errorf("campaign deposit cap is too small for %d operators", operatorCount)
	}
	validatorTarget := cfg.Policy.Deposit.EpochCapRaoPerOperator
	alphaPlanned, ok := checkedMul(uint64(validatorCount), validatorTarget)
	if !ok {
		return nil, fmt.Errorf("validator alpha plan overflow")
	}
	alphaPlanned, ok = checkedAdd(alphaPlanned, depositTotal)
	if !ok || alphaPlanned > cfg.MaximumAlphaRao {
		return nil, fmt.Errorf("release alpha requirement %d exceeds configured limit %d", alphaPlanned, cfg.MaximumAlphaRao)
	}
	if facts.AlphaAvailableRao < alphaPlanned {
		return nil, fmt.Errorf("wallet alpha source %s has %d rao, release setup requires %d", facts.AlphaSourceHotkey, facts.AlphaAvailableRao, alphaPlanned)
	}
	perOperatorCampaign, ok := checkedMul(cfg.Policy.Deposit.EpochCapRaoPerOperator, uint64(cfg.Config.Scenarios.ShortEpochs))
	if !ok {
		return nil, fmt.Errorf("per-operator campaign requirement overflow")
	}
	minimumCampaign, ok := checkedMul(perOperatorCampaign, uint64(operatorCount))
	if !ok {
		return nil, fmt.Errorf("campaign requirement overflow")
	}
	minimumCampaign, ok = checkedAdd(minimumCampaign, cfg.Config.Scenarios.VoluntaryConvictionRao)
	if !ok || depositTotal < minimumCampaign {
		return nil, fmt.Errorf("campaign cap %d is below release requirement %d", depositTotal, minimumCampaign)
	}
	campaignRemainder := depositTotal - minimumCampaign

	// The setup and live campaign share one gas ceiling. Every setup
	// transaction gets a weighted cap; the unspent remainder is reserved for
	// operator deposits/roots, keepers, and claims during the scenario.
	gasWeights := map[string]uint64{
		"evm.reserve-sink": 5, "evm.settlement-vault": 8,
		"evm.coordinator-implementation": 25, "evm.coordinator-proxy": 15,
		"evm.governance-drill-implementation": 25,
		"evm.vault-register-escrow":           5,
		"evm.vault-fix-coordinator":           2, "evm.sink-fix-recorder": 2,
	}
	for i := 1; i <= operatorCount; i++ {
		gasWeights[fmt.Sprintf("operator.deposit.register.%d", i)] = 3
		gasWeights[fmt.Sprintf("operator.register.%d", i)] = 8
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		gasWeights[fmt.Sprintf("fleet.mirror.%d", fleet)] = 3
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			gasWeights[fmt.Sprintf("fleet.bind.%d.%d", fleet, member)] = 4
		}
	}
	setupGasBudget, ok := mulDivFloor(cfg.MaximumEVMGasWei, 55, 100)
	if !ok {
		return nil, fmt.Errorf("EVM setup gas calculation overflow")
	}
	var totalWeight uint64
	for _, weight := range gasWeights {
		totalWeight += weight
	}
	gasCaps := map[string]uint64{}
	var allocatedSetupGas uint64
	gasIDs := make([]string, 0, len(gasWeights))
	for id := range gasWeights {
		gasIDs = append(gasIDs, id)
	}
	sort.Strings(gasIDs)
	for _, id := range gasIDs {
		gasCaps[id], ok = mulDivFloor(setupGasBudget, gasWeights[id], totalWeight)
		if !ok {
			return nil, fmt.Errorf("EVM gas cap calculation overflow for %s", id)
		}
		allocatedSetupGas += gasCaps[id]
	}
	if len(gasIDs) > 0 {
		gasCaps[gasIDs[0]] += setupGasBudget - allocatedSetupGas
		allocatedSetupGas = setupGasBudget
	}
	if setupGasBudget == 0 || allocatedSetupGas > cfg.MaximumEVMGasWei {
		return nil, fmt.Errorf("EVM gas ceiling is too small")
	}

	// Fund each online role once. Funding is counted as TAO outflow; gas is
	// counted only on the transaction actions, avoiding the old double count.
	type gasRole struct {
		label, address string
		weight         uint64
		burns          uint64
	}
	gasRoles := []gasRole{
		{label: "deployer", address: roles.Deployer, weight: 30, burns: 1},
		{label: "owner", address: roles.Owner, weight: 15, burns: uint64(operatorCount)},
		{label: "guardian", address: roles.Guardian, weight: 5},
		{label: "commitment-oracle", address: roles.CommitmentOracle, weight: 5},
		{label: "keeper", address: roles.Keeper, weight: 10},
	}
	for i := 0; i < operatorCount; i++ {
		gasRoles = append(gasRoles,
			gasRole{label: fmt.Sprintf("operator-%d-deposit", i+1), address: roles.OperatorDepositSigners[i], weight: 10, burns: 1},
			gasRole{label: fmt.Sprintf("operator-%d-root", i+1), address: roles.OperatorRootSigners[i], weight: 10},
		)
	}
	for i := 0; i < cfg.Config.Topology.Miners; i++ {
		gasRoles = append(gasRoles, gasRole{label: fmt.Sprintf("miner-%d-claim-relayer", i+1), address: roles.MinerClaimRelayers[i], weight: 4})
	}
	var roleWeight uint64
	for _, role := range gasRoles {
		roleWeight += role.weight
	}
	var fundedGas uint64
	for index, role := range gasRoles {
		gas, gasOK := mulDivFloor(cfg.MaximumEVMGasWei, role.weight, roleWeight)
		if !gasOK {
			return nil, fmt.Errorf("%s gas funding calculation overflow", role.label)
		}
		if index == len(gasRoles)-1 {
			gas = cfg.MaximumEVMGasWei - fundedGas
		}
		fundedGas += gas
		gasRao := ceilDiv(gas, 1_000_000_000)
		burnRao, mulOK := checkedMul(role.burns, registrationBurnLimit)
		if !mulOK {
			return nil, fmt.Errorf("%s burn budget overflow", role.label)
		}
		tao, addOK := checkedAdd(gasRao, burnRao)
		if !addOK {
			return nil, fmt.Errorf("%s funding budget overflow", role.label)
		}
		if role.label == "deployer" {
			tao, addOK = checkedAdd(tao, facts.ProbeTAORao)
			if !addOK {
				return nil, fmt.Errorf("precompile probe funding budget overflow")
			}
		}
		add(Action{ID: "evm.fund-" + role.label, Kind: "substrate-extrinsic", Target: role.address, Description: "fund the scoped EVM role with its exact campaign gas and registration ceiling", Spend: Spend{TAORao: tao}, DependsOn: []string{"subnet.verify-owner"}})
	}

	keys := make([]string, 0, len(cfg.Hyperparameters.OwnerControlled))
	for k := range cfg.Hyperparameters.OwnerControlled {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(Action{ID: "subnet.hyperparameter." + k, Kind: "substrate-extrinsic", Target: fmt.Sprintf("netuid:%d", cfg.Netuid), Description: fmt.Sprintf("converge %s to %v and verify finalized state", k, cfg.Hyperparameters.OwnerControlled[k]), DependsOn: []string{"subnet.verify-owner"}})
	}
	prev := "evm.fund-deployer"
	for _, entry := range []struct {
		id, desc     string
		registration bool
	}{{"reserve-sink", "deploy immutable one-way reserve sink", false}, {"settlement-vault", "deploy immutable settlement vault", false}, {"coordinator-implementation", "deploy coordinator implementation", false}, {"vault-register-escrow", "register the claims escrow under the immutable vault coldkey with the approved burn ceiling", true}, {"coordinator-proxy", "deploy and initialize ERC1967 coordinator proxy", false}, {"governance-drill-implementation", "deploy the locked testnet-only hostile coordinator implementation", false}, {"vault-fix-coordinator", "fix coordinator on settlement vault exactly once", false}, {"sink-fix-recorder", "fix coordinator on reserve sink exactly once", false}} {
		id := "evm." + entry.id
		registrations := uint32(0)
		if entry.registration {
			registrations = 1
		}
		action := Action{ID: id, Kind: "evm-transaction", Target: entry.id, Description: entry.desc, Spend: Spend{EVMGasWei: gasCaps[id], Registrations: registrations}, DependsOn: []string{prev}}
		if entry.registration {
			action.Parameters = registrationParameters()
		}
		add(action)
		prev = id
	}
	setupDeps := []string{prev}
	for i := 0; i < operatorCount; i++ {
		depositRegistration := fmt.Sprintf("operator.deposit.register.%d", i+1)
		add(Action{ID: depositRegistration, Kind: "evm-transaction", Target: roles.OperatorDepositSigners[i], Description: "limit-register the operator-isolated deposit hotkey under its EVM mirror coldkey", Parameters: registrationParameters(), Spend: Spend{EVMGasWei: gasCaps[depositRegistration], Registrations: 1}, DependsOn: []string{fmt.Sprintf("evm.fund-operator-%d-deposit", i+1)}})
		id := fmt.Sprintf("operator.register.%d", i+1)
		add(Action{ID: id, Kind: "evm-transaction", Target: fmt.Sprintf("no:%d", i+1), Description: "limit-register immutable pool hotkey and grant distinct deposit/root roles", Parameters: registrationParameters(), Spend: Spend{EVMGasWei: gasCaps[id], Registrations: 1}, DependsOn: []string{prev, "evm.fund-owner", depositRegistration}})
		alphaID := fmt.Sprintf("alpha.transfer.operator-deposit.%d", i+1)
		amount := perOperatorCampaign
		if i == 0 {
			amount, ok = checkedAdd(amount, cfg.Config.Scenarios.VoluntaryConvictionRao)
			if !ok {
				return nil, fmt.Errorf("operator 1 campaign allocation overflow")
			}
		}
		if i == operatorCount-1 {
			amount, ok = checkedAdd(amount, campaignRemainder)
			if !ok {
				return nil, fmt.Errorf("operator %d campaign allocation overflow", i+1)
			}
		}
		add(Action{ID: alphaID, Kind: "substrate-extrinsic", Target: roles.OperatorDepositSigners[i], Description: "transfer exact existing subnet alpha into the coordinator-owned isolated deposit position", Spend: Spend{AlphaRao: amount}, DependsOn: []string{id}})
		setupDeps = append(setupDeps, alphaID)
	}
	roleFunding, ok := checkedAdd(registrationBurnLimit, 2_000_000)
	if !ok {
		return nil, fmt.Errorf("native role funding overflow")
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		fundID := fmt.Sprintf("fleet.fund.%d", fleet)
		registerID := fmt.Sprintf("fleet.register.%d", fleet)
		fundHotkeyID := fmt.Sprintf("fleet.fund-hotkey.%d", fleet)
		add(Action{ID: fundID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet-coldkey:%d", fleet), Description: "fund the independently keyed provider fleet coldkey for one burn and bounded fees", Spend: Spend{TAORao: roleFunding}, DependsOn: []string{"subnet.verify-owner"}})
		add(Action{ID: registerID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet:%d", fleet), Description: "limit-register an independently keyed provider-owned head fleet hotkey", Parameters: registrationParameters(), Spend: Spend{Registrations: 1}, DependsOn: []string{fundID}})
		commitmentFees := uint64(1_000_000)
		if fleet == 1 {
			// Canonical publish plus the M0B replace/restore pair.
			commitmentFees = 3_000_000
		}
		add(Action{ID: fundHotkeyID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet-hotkey:%d", fleet), Description: "fund the fleet hotkey's exact bounded commitment-write fees", Spend: Spend{TAORao: commitmentFees}, DependsOn: []string{registerID}})
		setupDeps = append(setupDeps, registerID, fundHotkeyID)
	}
	setupDeps = append(setupDeps, "evm.vault-register-escrow")
	for i := 0; i < validatorCount; i++ {
		fundID := fmt.Sprintf("validator.fund.%d", i+1)
		add(Action{ID: fundID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("validator-coldkey:%d", i+1), Description: "fund the independent validator coldkey for one burn and bounded fees", Spend: Spend{TAORao: roleFunding}, DependsOn: []string{"subnet.verify-owner"}})
		registerID := fmt.Sprintf("validator.register.%d", i+1)
		description := "limit-register the independent validator hotkey and verify live UID"
		if i == 0 {
			description = "limit-register validator 1 as the reserve validator hotkey and verify live UID"
		}
		add(Action{ID: registerID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("validator:%d", i+1), Description: description, Parameters: registrationParameters(), Spend: Spend{Registrations: 1}, DependsOn: []string{fundID}})
		stakeDependency := registerID
		if i == 0 {
			add(Action{ID: "validator.take-zero.1", Kind: "substrate-extrinsic", Target: "reserve-validator:1", Description: "set and verify the reserve validator delegate take at exactly zero", DependsOn: []string{registerID}})
			stakeDependency = "validator.take-zero.1"
		}
		alphaID := fmt.Sprintf("alpha.transfer.validator.%d", i+1)
		add(Action{ID: alphaID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("validator:%d", i+1), Description: "transfer exact existing subnet alpha into the independent validator position", Spend: Spend{AlphaRao: validatorTarget}, DependsOn: []string{stakeDependency}})
		setupDeps = append(setupDeps, alphaID)
	}
	// Fees paid directly by the source wallet are explicit and bounded rather
	// than hidden in transferred role balances.
	var nativeWrites uint64
	for _, planned := range p.Actions {
		if planned.Kind == "substrate-extrinsic" {
			nativeWrites++
		}
	}
	// The production immunity transition is intentionally deferred until M3,
	// but its fee is part of the original campaign ceiling. The M0B
	// commitment replace/restore pair is also executed after this reservation
	// is constructed and must be included explicitly.
	nativeWrites += 3
	feeReserve, ok := checkedMul(nativeWrites, 1_000_000)
	if !ok {
		return nil, fmt.Errorf("native fee reserve overflow")
	}
	add(Action{ID: "wallet.native-fee-reserve", Kind: "budget-reserve", Target: cfg.WalletPublic, Description: "reserve a one-millirao maximum fee for every planned native extrinsic", Spend: Spend{TAORao: feeReserve}, DependsOn: []string{"subnet.verify-owner"}})
	setupDeps = append(setupDeps, "wallet.native-fee-reserve")
	runtimeGas := cfg.MaximumEVMGasWei - allocatedSetupGas
	voluntaryGas := runtimeGas / 20
	productionGas := runtimeGas / 20
	retirementGas := runtimeGas / 20
	governanceGas := runtimeGas / 10
	precompileGas := runtimeGas / 8
	if voluntaryGas == 0 || productionGas == 0 || retirementGas < uint64(operatorCount) || governanceGas < 10 || precompileGas < 10 {
		return nil, fmt.Errorf("EVM runtime gas ceiling is too small for conviction, production transition, and retirement")
	}
	probeWeights := map[string]uint64{
		"precompile.probe-deploy": 3, "precompile.seed": 2,
		"precompile.move-forward": 1, "precompile.move-back": 1,
		"precompile.snapshot": 1, "precompile.transfer-out": 2,
	}
	probeGasCaps := map[string]uint64{}
	var allocatedProbeGas uint64
	probeIDs := make([]string, 0, len(probeWeights))
	for id := range probeWeights {
		probeIDs = append(probeIDs, id)
	}
	sort.Strings(probeIDs)
	for i, id := range probeIDs {
		cap, capOK := mulDivFloor(precompileGas, probeWeights[id], 10)
		if !capOK {
			return nil, fmt.Errorf("precompile gas cap overflow for %s", id)
		}
		if i == len(probeIDs)-1 {
			cap = precompileGas - allocatedProbeGas
		}
		probeGasCaps[id] = cap
		allocatedProbeGas += cap
	}
	add(Action{ID: "campaign.evm-gas-reserve", Kind: "budget-reserve", Target: cfg.Config.Deployment.DeploymentID, Description: "reserve gas for deposits, payout roots, keepers, and claims during the live campaign", Spend: Spend{EVMGasWei: runtimeGas - voluntaryGas - productionGas - retirementGas - governanceGas - precompileGas}, DependsOn: setupDeps})
	setupDeps = append(setupDeps, "campaign.evm-gas-reserve")
	add(Action{ID: "config.render", Kind: "local", Target: cfg.Config.Deployment.DeploymentID, Description: "atomically render isolated operator, miner, validator, and supervisor configs", DependsOn: setupDeps})
	add(Action{ID: "accounts.provision", Kind: "local", Target: cfg.Config.Deployment.DeploymentID, Description: "provision stable operator-scoped miner and validator identities", DependsOn: []string{"config.render"}})
	add(Action{ID: "campaign.voluntary-conviction.1", Kind: "evm-transaction", Target: "no:1", Description: "lock the exact first-tier boundary as voluntary conviction without recording current-epoch demand", Parameters: map[string]string{"no_id": "1", "amount_rao": fmt.Sprint(cfg.Config.Scenarios.VoluntaryConvictionRao)}, Spend: Spend{EVMGasWei: voluntaryGas}, DependsOn: []string{"accounts.provision", "campaign.evm-gas-reserve", "alpha.transfer.operator-deposit.1"}})
	lastFleet := "campaign.voluntary-conviction.1"
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		commitID := fmt.Sprintf("fleet.commitment.%d", fleet)
		mirrorID := fmt.Sprintf("fleet.mirror.%d", fleet)
		add(Action{ID: commitID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet:%d", fleet), Description: "publish the canonical multi-client fleet manifest hash from its registered hotkey and verify finalized storage", DependsOn: []string{lastFleet, fmt.Sprintf("fleet.fund-hotkey.%d", fleet)}})
		add(Action{ID: mirrorID, Kind: "evm-transaction", Target: fmt.Sprintf("head-fleet:%d", fleet), Description: "mirror the independently verified finalized native commitment into the coordinator", Spend: Spend{EVMGasWei: gasCaps[mirrorID]}, DependsOn: []string{commitID, "evm.fund-commitment-oracle"}})
		lastFleet = mirrorID
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			id := fmt.Sprintf("fleet.bind.%d.%d", fleet, member)
			miner := fleetMemberMinerIndex(cfg, fleet, member)
			add(Action{ID: id, Kind: "evm-transaction", Target: fmt.Sprintf("miner:%d", miner), Description: "relay one dual-signed fleet member binding effective next epoch", Spend: Spend{EVMGasWei: gasCaps[id]}, DependsOn: []string{lastFleet, "evm.fund-keeper"}})
			lastFleet = id
		}
	}
	add(Action{ID: "topology.launch", Kind: "local", Target: cfg.Config.Deployment.DeploymentID, Description: "start dependencies, two operators, miners, and two validators with readiness gates", DependsOn: []string{lastFleet}})
	add(Action{ID: "precompile.commitment-write", Kind: "substrate-extrinsic", Target: "head-fleet:1", Description: "write and finalized-read an exact one-field SHA-256 conformance commitment from the registered test hotkey", DependsOn: []string{"topology.launch"}})
	add(Action{ID: "precompile.commitment-restore", Kind: "substrate-extrinsic", Target: "head-fleet:1", Description: "replace the conformance commitment with the canonical fleet hash and prove the restored finalized bytes", DependsOn: []string{"precompile.commitment-write"}})
	add(Action{ID: "precompile.probe-deploy", Kind: "evm-transaction", Target: "disposable-precompile-probe", Description: "deploy the locked, owner-gated runtime-451 conformance probe at its precomputed nonce", Spend: Spend{EVMGasWei: probeGasCaps["precompile.probe-deploy"]}, DependsOn: []string{"precompile.commitment-restore"}})
	add(Action{ID: "precompile.read-battery", Kind: "evm-read", Target: "runtime-451-precompiles", Description: "prove Blake2, Ed25519, sr25519, metagraph, neuron, staking, live UID, absent UID, mirror custody, and minimum stake at one finalized head", DependsOn: []string{"precompile.probe-deploy"}})
	add(Action{ID: "precompile.seed", Kind: "evm-transaction", Target: "validator:1", Description: "convert the approved TAO dust ceiling into probe-coldkey alpha and record exact live units", Parameters: map[string]string{"maximum_tao_rao": fmt.Sprint(facts.ProbeTAORao), "nominator_minimum_rao": fmt.Sprint(facts.NominatorMinimumRao)}, Spend: Spend{EVMGasWei: probeGasCaps["precompile.seed"]}, DependsOn: []string{"precompile.read-battery"}})
	add(Action{ID: "precompile.move-forward", Kind: "evm-transaction", Target: "validator:2", Description: "move half the observed probe alpha from validator 1 to validator 2 and prove exact slippage-free deltas", Spend: Spend{EVMGasWei: probeGasCaps["precompile.move-forward"]}, DependsOn: []string{"precompile.seed"}})
	add(Action{ID: "precompile.move-back", Kind: "evm-transaction", Target: "validator:1", Description: "move the same alpha back and prove exact round-trip custody", Spend: Spend{EVMGasWei: probeGasCaps["precompile.move-back"]}, DependsOn: []string{"precompile.move-forward"}})
	add(Action{ID: "precompile.snapshot", Kind: "evm-transaction", Target: "validator:1", Description: "snapshot probe stake before a native dividend cycle", Spend: Spend{EVMGasWei: probeGasCaps["precompile.snapshot"]}, DependsOn: []string{"precompile.move-back"}})
	add(Action{ID: "precompile.dividend", Kind: "evm-read", Target: "validator:1", Description: "wait through a bounded native cycle and prove take-zero dividend auto-compounding at finalized state", DependsOn: []string{"precompile.snapshot"}})
	add(Action{ID: "precompile.transfer-out", Kind: "evm-transaction", Target: "head-fleet-coldkey:1", Description: "transfer every probe-attributable alpha unit directly to a controlled provider coldkey and leave no probe dust", Spend: Spend{EVMGasWei: probeGasCaps["precompile.transfer-out"]}, DependsOn: []string{"precompile.dividend"}})
	governanceActions := []struct {
		id, target, description string
		weight                  uint64
	}{
		{"governance.guardian-pause", "guardian", "pause coordinator new-risk surfaces after at least one finalized entitlement", 1},
		{"governance.upgrade-adversary", "coordinator", "upgrade the coordinator proxy to the locked hostile testnet implementation", 3},
		{"governance.probe-custody", "coordinator", "attempt entitlement rewrite, custody reset, and reserve outflow and require every probe to fail", 2},
		{"governance.restore-coordinator", "coordinator", "restore the reviewed coordinator implementation and verify the ERC1967 slot", 3},
		{"governance.guardian-unpause", "guardian", "unpause new-risk surfaces after immutable claims and custody are reverified", 1},
	}
	governanceDependency := "precompile.transfer-out"
	var allocatedGovernance uint64
	for i, drill := range governanceActions {
		cap, capOK := mulDivFloor(governanceGas, drill.weight, 10)
		if !capOK {
			return nil, fmt.Errorf("governance gas cap overflow")
		}
		if i == len(governanceActions)-1 {
			cap = governanceGas - allocatedGovernance
		}
		allocatedGovernance += cap
		add(Action{ID: drill.id, Kind: "evm-transaction", Target: drill.target, Description: drill.description, Spend: Spend{EVMGasWei: cap}, DependsOn: []string{governanceDependency}})
		governanceDependency = drill.id
	}
	production := cfg.Policy.ProductionCadence
	add(Action{
		ID: "production.schedule-policy", Kind: "evm-transaction", Target: "coordinator",
		Description: "after M2, schedule the canonical production cadence for the next epoch without changing the active epoch",
		Parameters: map[string]string{
			"after_accelerated_epochs":  fmt.Sprint(production.AfterAcceleratedEpochs),
			"epoch_blocks":              fmt.Sprint(production.EpochBlocks),
			"root_commit_window_blocks": fmt.Sprint(production.RootCommitWindowBlocks),
			"finalize_offset_blocks":    fmt.Sprint(production.FinalizeOffsetBlocks),
			"close_grace_blocks":        fmt.Sprint(production.CloseGraceBlocks),
		},
		Spend: Spend{EVMGasWei: productionGas}, DependsOn: []string{governanceDependency, "evm.fund-owner"},
	})
	add(Action{ID: "production.hyperparameter.immunity_period", Kind: "substrate-extrinsic", Target: fmt.Sprintf("netuid:%d", cfg.Netuid), Description: "after M2, converge immunity_period to the production epoch length and verify finalized state", Parameters: map[string]string{"value": fmt.Sprint(production.EpochBlocks)}, DependsOn: []string{"production.schedule-policy"}})
	add(Action{ID: "retirement.evm-gas-reserve", Kind: "budget-reserve", Target: cfg.Config.Deployment.DeploymentID, Description: "reserve a separately approved gas ceiling for future-effective retirement of every operator", Parameters: map[string]string{"operators": fmt.Sprint(operatorCount)}, Spend: Spend{EVMGasWei: retirementGas}, DependsOn: []string{"topology.launch"}})
	for _, a := range p.Actions {
		var sumOK bool
		p.MaximumSpend.TAORao, sumOK = checkedAdd(p.MaximumSpend.TAORao, a.Spend.TAORao)
		if !sumOK {
			return nil, fmt.Errorf("TAO plan overflow")
		}
		p.MaximumSpend.AlphaRao, sumOK = checkedAdd(p.MaximumSpend.AlphaRao, a.Spend.AlphaRao)
		if !sumOK {
			return nil, fmt.Errorf("alpha plan overflow")
		}
		p.MaximumSpend.EVMGasWei, sumOK = checkedAdd(p.MaximumSpend.EVMGasWei, a.Spend.EVMGasWei)
		if !sumOK {
			return nil, fmt.Errorf("gas plan overflow")
		}
		p.MaximumSpend.Registrations += a.Spend.Registrations
		p.MaximumSpend.SubnetCreations += a.Spend.SubnetCreations
	}
	p.Limits = Spend{TAORao: cfg.MaximumTAORao, AlphaRao: cfg.MaximumAlphaRao, EVMGasWei: cfg.MaximumEVMGasWei, Registrations: uint32(cfg.Config.Budgets.MaximumRegistrations), SubnetCreations: uint32(cfg.Config.Budgets.MaximumSubnetCreations)}
	if err := validatePlanBudget(p); err != nil {
		return nil, err
	}
	planHash, err := p.hash()
	if err != nil {
		return nil, err
	}
	p.PlanHash = planHash
	return p, nil
}

func ceilDiv(value, divisor uint64) uint64 {
	if divisor == 0 {
		return math.MaxUint64
	}
	return value/divisor + boolUint(value%divisor != 0)
}
func boolUint(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}
func checkedAdd(a, b uint64) (uint64, bool) {
	if math.MaxUint64-a < b {
		return 0, false
	}
	return a + b, true
}
func checkedMul(a, b uint64) (uint64, bool) {
	if a != 0 && b > math.MaxUint64/a {
		return 0, false
	}
	return a * b, true
}
func mulDivFloor(a, b, divisor uint64) (uint64, bool) {
	if divisor == 0 {
		return 0, false
	}
	value := new(big.Int).Mul(new(big.Int).SetUint64(a), new(big.Int).SetUint64(b))
	value.Div(value, new(big.Int).SetUint64(divisor))
	if !value.IsUint64() {
		return 0, false
	}
	return value.Uint64(), true
}

func (p SetupPlan) hash() (string, error) {
	p.PlanHash = ""
	p.GeneratedAt = ""
	// The approval binds the economic inputs and intended mutations, not the
	// observation height or balances that naturally advance while an operator
	// reviews the plan. Apply re-reads and enforces these safety observations.
	p.LiveFacts.FinalizedBlock = 0
	p.LiveFacts.FinalizedBlockHash = ""
	p.LiveFacts.AlphaAvailableRao = 0
	p.LiveFacts.WalletFreeTAORao = 0
	return canonicalHashHex(p)
}
func validatePlanBudget(p *SetupPlan) error {
	if p == nil {
		return errors.New("setup plan is unavailable")
	}
	if p.MaximumSpend.TAORao > p.Limits.TAORao {
		return fmt.Errorf("TAO plan maximum %d exceeds limit %d", p.MaximumSpend.TAORao, p.Limits.TAORao)
	}
	if p.MaximumSpend.AlphaRao > p.Limits.AlphaRao {
		return fmt.Errorf("alpha plan maximum %d exceeds limit %d", p.MaximumSpend.AlphaRao, p.Limits.AlphaRao)
	}
	if p.MaximumSpend.EVMGasWei > p.Limits.EVMGasWei {
		return fmt.Errorf("gas plan maximum %d exceeds limit %d", p.MaximumSpend.EVMGasWei, p.Limits.EVMGasWei)
	}
	if p.MaximumSpend.Registrations > p.Limits.Registrations {
		return fmt.Errorf("registration plan %d exceeds limit %d", p.MaximumSpend.Registrations, p.Limits.Registrations)
	}
	if p.MaximumSpend.Registrations > 0 && p.RegistrationBurnLimitRao == 0 {
		return errors.New("registration plan has no per-registration burn limit")
	}
	seenActions := make(map[string]bool, len(p.Actions))
	var actionRegistrations uint64
	for _, action := range p.Actions {
		if action.ID == "" || seenActions[action.ID] {
			return fmt.Errorf("setup plan has an empty or duplicate action id %q", action.ID)
		}
		for _, dependency := range action.DependsOn {
			if !seenActions[dependency] {
				return fmt.Errorf("action %s depends on missing or later action %s", action.ID, dependency)
			}
		}
		seenActions[action.ID] = true
		if action.Spend.Registrations == 0 {
			continue
		}
		actionRegistrations += uint64(action.Spend.Registrations)
		limit, err := strconv.ParseUint(action.Parameters["maximum_burn_rao"], 10, 64)
		if err != nil || limit != p.RegistrationBurnLimitRao {
			return fmt.Errorf("registration action %s does not bind the plan burn limit", action.ID)
		}
	}
	if actionRegistrations != uint64(p.MaximumSpend.Registrations) {
		return fmt.Errorf("registration actions total %d, plan maximum is %d", actionRegistrations, p.MaximumSpend.Registrations)
	}
	if p.MaximumSpend.SubnetCreations != 0 || p.Limits.SubnetCreations != 0 {
		return fmt.Errorf("subnet creation is forbidden")
	}
	return nil
}

func derivePublicRoles(cfg *ResolvedConfig) (PublicRoles, error) {
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return PublicRoles{}, err
	}
	address := func(label string) (string, error) {
		role, ok := roles.EVM[label]
		if !ok || role.Address == "" {
			return "", fmt.Errorf("missing derived EVM role %q", label)
		}
		return role.Address, nil
	}
	var r PublicRoles
	if r.Deployer, err = address("deployer"); err != nil {
		return r, err
	}
	if r.Owner, err = address("testnet-owner"); err != nil {
		return r, err
	}
	if r.Guardian, err = address("guardian"); err != nil {
		return r, err
	}
	if r.CommitmentOracle, err = address("commitment-oracle"); err != nil {
		return r, err
	}
	if r.Keeper, err = address("keeper"); err != nil {
		return r, err
	}
	for i := 0; i < cfg.Config.Topology.Operators; i++ {
		a, e := address(fmt.Sprintf("operator-%d-deposit", i+1))
		if e != nil {
			return r, e
		}
		r.OperatorDepositSigners = append(r.OperatorDepositSigners, a)
		a, e = address(fmt.Sprintf("operator-%d-root", i+1))
		if e != nil {
			return r, e
		}
		r.OperatorRootSigners = append(r.OperatorRootSigners, a)
	}
	for i := 0; i < cfg.Config.Topology.Miners; i++ {
		a, e := address(fmt.Sprintf("miner-%d-claim-relayer", i+1))
		if e != nil {
			return r, e
		}
		r.MinerClaimRelayers = append(r.MinerClaimRelayers, a)
	}
	return r, nil
}

func (p SetupPlan) MarshalCanonical() ([]byte, error) {
	clone := p
	clone.GeneratedAt = ""
	return json.Marshal(clone)
}
func decodeHash(s string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(stringsTrim0x(s))
	if err != nil || len(b) != 32 {
		return out, fmt.Errorf("invalid hash %q", s)
	}
	copy(out[:], b)
	return out, nil
}
func stringsTrim0x(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}
