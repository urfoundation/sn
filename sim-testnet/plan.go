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
	"strings"
	"time"

	"github.com/urfoundation/sn/ss58"
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
	Schema                       string      `json:"schema"`
	Release                      string      `json:"release"`
	ReleaseLockHash              string      `json:"release_lock_hash"`
	DeploymentID                 string      `json:"deployment_id"`
	ChainID                      uint64      `json:"chain_id"`
	GenesisHash                  string      `json:"genesis_hash"`
	Netuid                       uint16      `json:"netuid"`
	Owner                        string      `json:"owner"`
	LiveFacts                    SetupFacts  `json:"live_facts"`
	RegistrationBurnLimitRao     uint64      `json:"registration_burn_limit_rao"`
	NativeTransactionFeeLimitRao uint64      `json:"native_transaction_fee_limit_rao,omitempty"`
	BootstrapBurnHalfLifeBlocks  uint16      `json:"bootstrap_burn_half_life_blocks,omitempty"`
	ProductionBurnHalfLifeBlocks uint16      `json:"production_burn_half_life_blocks,omitempty"`
	PriorPlanHashes              []string    `json:"prior_plan_hashes,omitempty"`
	ConfigHash                   string      `json:"config_hash"`
	ResolvedInputsHash           string      `json:"resolved_inputs_hash"`
	PolicyHash                   string      `json:"policy_hash"`
	Roles                        PublicRoles `json:"roles"`
	Actions                      []Action    `json:"actions"`
	MaximumSpend                 Spend       `json:"maximum_spend"`
	Limits                       Spend       `json:"limits"`
	PlanHash                     string      `json:"plan_hash"`
	GeneratedAt                  string      `json:"generated_at,omitempty"`
}
type PublicRoles struct {
	Deployer, Owner, Guardian, CommitmentOracle string   `json:",omitempty"`
	OperatorDepositSigners                      []string `json:"operator_deposit_signers"`
	OperatorRootSigners                         []string `json:"operator_root_signers"`
	ClaimRelayers                               []string `json:"claim_relayers"`
	Keeper                                      string   `json:"keeper"`
}

// Project resolved campaign ceilings into the exact approval representation.
func configuredPlanLimits(cfg *ResolvedConfig) Spend {
	return Spend{
		TAORao: cfg.MaximumTAORao, AlphaRao: cfg.MaximumAlphaRao, EVMGasWei: cfg.MaximumEVMGasWei,
		Registrations: uint32(cfg.Config.Budgets.MaximumRegistrations), SubnetCreations: uint32(cfg.Config.Budgets.MaximumSubnetCreations),
	}
}

// Bind every executable field used to distinguish an action across plan
// revisions and journal recovery.
func actionIntentHash(action Action) (string, error) {
	return canonicalHashHex(struct {
		ID, Kind, Target, Description string
		Parameters                    map[string]string
		Spend                         Spend
		DependsOn                     []string
	}{
		ID:          action.ID,
		Kind:        action.Kind,
		Target:      action.Target,
		Description: action.Description,
		Parameters:  action.Parameters,
		Spend:       action.Spend,
		DependsOn:   action.DependsOn,
	})
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

func validateFreshSubnetTopology(cfg *ResolvedConfig, facts *SetupFacts) error {
	if cfg == nil || facts == nil {
		return errors.New("fresh subnet topology inputs are unavailable")
	}
	if facts.ExistingUIDCount == 0 || len(facts.ExistingUIDs) != int(facts.ExistingUIDCount) {
		return fmt.Errorf("fresh release plan has inconsistent existing UID facts: count=%d identities=%d", facts.ExistingUIDCount, len(facts.ExistingUIDs))
	}
	expected, err := ss58.DecodeWithPrefix(cfg.WalletHotkeyPublic, ss58.BittensorPrefix)
	if err != nil {
		return fmt.Errorf("configured subnet owner hotkey: %w", err)
	}
	expectedColdkey, err := ss58.DecodeWithPrefix(cfg.WalletPublic, ss58.BittensorPrefix)
	if err != nil {
		return fmt.Errorf("configured subnet owner coldkey: %w", err)
	}
	owner, err := decodeHex32("finalized subnet owner hotkey", facts.SubnetOwnerHotkey)
	if err != nil {
		return err
	}
	uidZero, err := decodeHex32("finalized UID zero hotkey", facts.UIDZeroHotkey)
	if err != nil {
		return err
	}
	if owner != expected || uidZero != expected {
		return fmt.Errorf("finalized owner and UID zero are not the configured subnet owner hotkey: owner=0x%x uid0=0x%x expected=0x%x", owner, uidZero, expected)
	}
	seen := map[[32]byte]bool{}
	for index, identity := range facts.ExistingUIDs {
		if int(identity.UID) != index || identity.RegistrationBlock == 0 {
			return fmt.Errorf("existing UID fact %d is not a complete contiguous finalized identity", index)
		}
		hotkey, err := decodeHex32(fmt.Sprintf("existing UID %d hotkey", identity.UID), identity.Hotkey)
		if err != nil {
			return err
		}
		coldkey, err := decodeHex32(fmt.Sprintf("existing UID %d coldkey", identity.UID), identity.Coldkey)
		if err != nil {
			return err
		}
		if seen[hotkey] {
			return fmt.Errorf("existing UID %d duplicates hotkey 0x%x", identity.UID, hotkey)
		}
		seen[hotkey] = true
		if identity.UID == 0 {
			if hotkey != expected || coldkey != expectedColdkey || !identity.SubnetOwner {
				return errors.New("UID zero is not the configured immortal subnet-owner hotkey")
			}
		} else if identity.SubnetOwner || coldkey == expectedColdkey {
			return fmt.Errorf("existing UID %d unexpectedly belongs to the subnet owner", identity.UID)
		}
	}
	maximum := hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["max_allowed_uids"])
	plannedInitial := uint64(2*cfg.Config.Topology.Operators + cfg.Config.Topology.Validators + cfg.Config.Topology.HeadFleets + cfg.Config.Topology.ChurnFloorUIDs + 1)
	if maximum == 0 || uint64(facts.ExistingUIDCount)+plannedInitial != maximum {
		return fmt.Errorf("existing UIDs %d plus %d planned initial registrations do not exactly fill max_allowed_uids %d", facts.ExistingUIDCount, plannedInitial, maximum)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return fmt.Errorf("derive planned registration identities: %w", err)
	}
	plannedLabels := []string{"escrow-hotkey"}
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		plannedLabels = append(plannedLabels, churnHotkeyLabel(churn))
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		plannedLabels = append(plannedLabels, fleetHotkeyLabel(fleet))
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		plannedLabels = append(plannedLabels, fmt.Sprintf("operator-%d-pool-hotkey", operator), fmt.Sprintf("operator-%d-deposit-hotkey", operator))
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		plannedLabels = append(plannedLabels, validatorHotkeyLabel(validator))
	}
	for _, label := range plannedLabels {
		role, ok := roles.Substrate[label]
		if !ok {
			return fmt.Errorf("planned registration role %s is unavailable", label)
		}
		hotkey, err := decodeHex32(label, role.PublicKeyHex)
		if err != nil {
			return err
		}
		if seen[hotkey] {
			return fmt.Errorf("planned registration role %s collides with an existing UID hotkey", label)
		}
	}
	return nil
}

func buildPlan(cfg *ResolvedConfig, facts *SetupFacts, roles PublicRoles, generatedAt time.Time) (*SetupPlan, error) {
	if facts == nil || facts.BurnRao == 0 || facts.AlphaSourceHotkey == "" || facts.ExistentialDepositRao == 0 || facts.ProbeTAORao == 0 {
		return nil, fmt.Errorf("finalized burn, alpha source, existential-deposit, and probe-value facts are required")
	}
	if err := validateFreshSubnetTopology(cfg, facts); err != nil {
		return nil, err
	}
	registrationBurnLimit := cfg.Config.Budgets.MaximumRegistrationBurnRao
	if registrationBurnLimit == 0 || facts.BurnRao > registrationBurnLimit {
		return nil, fmt.Errorf("finalized registration burn %d exceeds configured per-registration limit %d", facts.BurnRao, registrationBurnLimit)
	}
	if err := validateRegistrationEconomics(cfg, facts, registrationBurnLimit); err != nil {
		return nil, err
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
	nativeFeeLimit := cfg.Config.Budgets.MaximumNativeTransactionFeeRao
	if nativeFeeLimit == 0 {
		return nil, fmt.Errorf("native transaction fee limit is required")
	}
	bootstrapBurnHalfLife := uint16(hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["burn_half_life"]))
	productionBurnHalfLife := uint16(hyperparameterUint64(cfg.Hyperparameters.ProductionOwnerControlled["burn_half_life"]))
	p := &SetupPlan{Schema: "urnetwork-sim-plan-v2", Release: "1.0", ReleaseLockHash: releaseLockHash, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: testnetChainID, GenesisHash: testnetGenesis, Netuid: cfg.Netuid, Owner: cfg.WalletPublic, LiveFacts: *facts, RegistrationBurnLimitRao: registrationBurnLimit, NativeTransactionFeeLimitRao: nativeFeeLimit, BootstrapBurnHalfLifeBlocks: bootstrapBurnHalfLife, ProductionBurnHalfLifeBlocks: productionBurnHalfLife, ConfigHash: cfg.ConfigHash, ResolvedInputsHash: resolvedHash, PolicyHash: cfg.PolicyHash, Roles: roles, GeneratedAt: generatedAt.Format(time.RFC3339)}
	add := func(a Action) {
		h, _ := actionIntentHash(a)
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
	minimumCampaign, err := releaseCampaignDepositRequirement(cfg)
	if err != nil {
		return nil, err
	}
	if depositTotal < minimumCampaign {
		return nil, fmt.Errorf("campaign cap %d is below release requirement %d", depositTotal, minimumCampaign)
	}
	campaignRemainder := depositTotal - minimumCampaign
	acceleratedPerOperator, ok := checkedMul(cfg.Policy.Deposit.EpochCapRaoPerOperator, uint64(cfg.Config.Scenarios.ShortEpochs))
	if !ok {
		return nil, fmt.Errorf("per-operator accelerated campaign requirement overflow")
	}
	productionDepositEpochs := uint64(cfg.Config.Scenarios.ProductionEpochs) + 2
	operatorCampaign := make([]uint64, operatorCount)
	for index := range operatorCampaign {
		productionEpochs := productionDepositEpochs
		productionDust := uint64(0)
		if index == 1 {
			productionEpochs--
			productionDust = 1
		}
		production, mulOK := checkedMul(cfg.Policy.Deposit.EpochCapRaoPerOperator, productionEpochs)
		if !mulOK {
			return nil, fmt.Errorf("operator %d production campaign requirement overflow", index+1)
		}
		allocation, addOK := checkedAdd(acceleratedPerOperator, production)
		if !addOK {
			return nil, fmt.Errorf("operator %d campaign allocation overflow", index+1)
		}
		allocation, addOK = checkedAdd(allocation, productionDust)
		if !addOK {
			return nil, fmt.Errorf("operator %d dishonest campaign allocation overflow", index+1)
		}
		operatorCampaign[index] = allocation
	}
	operatorCampaign[0], ok = checkedAdd(operatorCampaign[0], cfg.Config.Scenarios.VoluntaryConvictionRao)
	if !ok {
		return nil, fmt.Errorf("operator 1 voluntary conviction allocation overflow")
	}
	operatorCampaign[operatorCount-1], ok = checkedAdd(operatorCampaign[operatorCount-1], campaignRemainder)
	if !ok {
		return nil, fmt.Errorf("operator %d campaign remainder overflow", operatorCount)
	}

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
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
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
			gasRole{label: fmt.Sprintf("operator-%d-claim-relayer", i+1), address: roles.ClaimRelayers[i], weight: 10},
		)
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
		usableTAORao, addOK := checkedAdd(gasRao, burnRao)
		if !addOK {
			return nil, fmt.Errorf("%s funding budget overflow", role.label)
		}
		if role.label == "deployer" {
			usableTAORao, addOK = checkedAdd(usableTAORao, facts.ProbeTAORao)
			if !addOK {
				return nil, fmt.Errorf("precompile probe funding budget overflow")
			}
		}
		maximumTransferRao, addOK := checkedAdd(usableTAORao, facts.ExistentialDepositRao)
		if !addOK {
			return nil, fmt.Errorf("%s existential-deposit funding budget overflow", role.label)
		}
		add(Action{
			ID: "evm.fund-" + role.label, Kind: "substrate-extrinsic", Target: role.address,
			Description: "fund the scoped EVM role with its exact usable campaign balance plus the runtime existential deposit",
			Parameters: map[string]string{
				"usable_evm_rao":          strconv.FormatUint(usableTAORao, 10),
				"existential_deposit_rao": strconv.FormatUint(facts.ExistentialDepositRao, 10),
			},
			Spend: Spend{TAORao: maximumTransferRao}, DependsOn: []string{"subnet.verify-owner"},
		})
	}

	keys := make([]string, 0, len(cfg.Hyperparameters.OwnerControlled))
	for k := range cfg.Hyperparameters.OwnerControlled {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	hyperparameterBarrier := "subnet.verify-owner"
	for _, k := range keys {
		id := "subnet.hyperparameter." + k
		add(Action{ID: id, Kind: "substrate-extrinsic", Target: fmt.Sprintf("netuid:%d", cfg.Netuid), Description: fmt.Sprintf("converge %s to %v and verify finalized state", k, cfg.Hyperparameters.OwnerControlled[k]), DependsOn: []string{hyperparameterBarrier}})
		hyperparameterBarrier = id
	}
	roleFunding, err := registrationRoleFunding(registrationBurnLimit, nativeFeeLimit, facts.ExistentialDepositRao)
	if err != nil {
		return nil, err
	}
	registrationFundingParameters := func() map[string]string {
		return map[string]string{
			"maximum_burn_rao":       fmt.Sprint(registrationBurnLimit),
			"maximum_fee_rao":        fmt.Sprint(nativeFeeLimit),
			"keep_alive_reserve_rao": fmt.Sprint(facts.ExistentialDepositRao),
		}
	}
	// Churn-floor identities must be the oldest non-owner registrations. Runtime
	// v451 breaks equal-emission prune ties by registration block and UID, even
	// inside immunity. Registering custody or pool identities first would let a
	// challenger evict a load-bearing role instead of the intended floor.
	lastChurn := hyperparameterBarrier
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		fundID := fmt.Sprintf("churn.fund.%d", churn)
		registerID := fmt.Sprintf("churn.register.%d", churn)
		add(Action{ID: fundID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("churn-coldkey:%d", churn), Description: "fund a deterministic churn-floor coldkey for one bounded registration while preserving its runtime keep-alive balance", Parameters: registrationFundingParameters(), Spend: Spend{TAORao: roleFunding}, DependsOn: []string{lastChurn}})
		add(Action{ID: registerID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("churn-hotkey:%d", churn), Description: "limit-register an unbound zero-weight churn-floor UID before every load-bearing identity", Parameters: registrationParameters(), Spend: Spend{Registrations: 1}, DependsOn: []string{fundID}})
		lastChurn = registerID
	}
	prev := "evm.fund-deployer"
	for entryIndex, entry := range []struct {
		id, desc     string
		registration bool
	}{{"reserve-sink", "deploy immutable one-way reserve sink", false}, {"settlement-vault", "deploy immutable settlement vault", false}, {"coordinator-implementation", "deploy coordinator implementation", false}, {"vault-register-escrow", "register the claims escrow under the immutable vault coldkey with the approved burn ceiling", true}, {"coordinator-proxy", "deploy and initialize ERC1967 coordinator proxy", false}, {"governance-drill-implementation", "deploy the locked testnet-only hostile coordinator implementation", false}, {"vault-fix-coordinator", "fix coordinator on settlement vault exactly once", false}, {"sink-fix-recorder", "fix coordinator on reserve sink exactly once", false}} {
		id := "evm." + entry.id
		registrations := uint32(0)
		if entry.registration {
			registrations = 1
		}
		dependencies := []string{prev}
		if entryIndex == 0 {
			dependencies = append(dependencies, lastChurn)
		}
		action := Action{ID: id, Kind: "evm-transaction", Target: entry.id, Description: entry.desc, Spend: Spend{EVMGasWei: gasCaps[id], Registrations: registrations}, DependsOn: dependencies}
		if entry.registration {
			action.Parameters = registrationParameters()
		}
		add(action)
		prev = id
	}
	setupDeps := []string{prev}
	for i := 0; i < operatorCount; i++ {
		depositRegistration := fmt.Sprintf("operator.deposit.register.%d", i+1)
		add(Action{ID: depositRegistration, Kind: "evm-transaction", Target: roles.OperatorDepositSigners[i], Description: "limit-register the operator-isolated deposit hotkey under its EVM mirror coldkey", Parameters: registrationParameters(), Spend: Spend{EVMGasWei: gasCaps[depositRegistration], Registrations: 1}, DependsOn: []string{fmt.Sprintf("evm.fund-operator-%d-deposit", i+1), lastChurn}})
		id := fmt.Sprintf("operator.register.%d", i+1)
		add(Action{ID: id, Kind: "evm-transaction", Target: fmt.Sprintf("no:%d", i+1), Description: "limit-register immutable pool hotkey and grant distinct deposit/root roles", Parameters: registrationParameters(), Spend: Spend{EVMGasWei: gasCaps[id], Registrations: 1}, DependsOn: []string{prev, "evm.fund-owner", depositRegistration}})
		alphaID := fmt.Sprintf("alpha.transfer.operator-deposit.%d", i+1)
		amount := operatorCampaign[i]
		add(Action{ID: alphaID, Kind: "substrate-extrinsic", Target: roles.OperatorDepositSigners[i], Description: "transfer exact existing subnet alpha into the coordinator-owned isolated deposit position", Spend: Spend{AlphaRao: amount}, DependsOn: []string{id}})
		setupDeps = append(setupDeps, alphaID)
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		fundID := fmt.Sprintf("fleet.fund.%d", fleet)
		registerID := fmt.Sprintf("fleet.register.%d", fleet)
		fundHotkeyID := fmt.Sprintf("fleet.fund-hotkey.%d", fleet)
		add(Action{ID: fundID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet-coldkey:%d", fleet), Description: "fund the independently keyed provider fleet coldkey for one burn, bounded fees, and runtime keep-alive balance", Parameters: registrationFundingParameters(), Spend: Spend{TAORao: roleFunding}, DependsOn: []string{"subnet.verify-owner", lastChurn}})
		add(Action{ID: registerID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet:%d", fleet), Description: "limit-register an independently keyed provider-owned head fleet hotkey", Parameters: registrationParameters(), Spend: Spend{Registrations: 1}, DependsOn: []string{fundID}})
		commitmentFees := nativeFeeLimit
		if fleet == 1 {
			// Canonical publish plus the M0B replace/restore pair.
			commitmentFees, ok = checkedMul(nativeFeeLimit, 3)
			if !ok {
				return nil, fmt.Errorf("head fleet commitment fee reserve overflow")
			}
		}
		commitmentFunding, fundingErr := registrationRoleFunding(0, commitmentFees, facts.ExistentialDepositRao)
		if fundingErr != nil {
			return nil, fmt.Errorf("head fleet %d commitment funding: %w", fleet, fundingErr)
		}
		add(Action{ID: fundHotkeyID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet-hotkey:%d", fleet), Description: "fund the fleet hotkey's exact bounded commitment-write fees and runtime keep-alive balance", Parameters: map[string]string{"maximum_fee_rao": fmt.Sprint(commitmentFees), "keep_alive_reserve_rao": fmt.Sprint(facts.ExistentialDepositRao)}, Spend: Spend{TAORao: commitmentFunding}, DependsOn: []string{registerID}})
		setupDeps = append(setupDeps, registerID, fundHotkeyID)
	}
	for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
		fleet := cfg.Config.Topology.HeadFleets + challenger
		fundID := fmt.Sprintf("fleet.fund.%d", fleet)
		fundHotkeyID := fmt.Sprintf("fleet.fund-hotkey.%d", fleet)
		add(Action{ID: fundID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("challenger-fleet-coldkey:%d", fleet), Description: "fund a challenger fleet coldkey for one bounded churn registration while preserving its runtime keep-alive balance", Parameters: registrationFundingParameters(), Spend: Spend{TAORao: roleFunding}, DependsOn: []string{"subnet.verify-owner", lastChurn}})
		commitmentFunding, fundingErr := registrationRoleFunding(0, nativeFeeLimit, facts.ExistentialDepositRao)
		if fundingErr != nil {
			return nil, fmt.Errorf("challenger fleet %d commitment funding: %w", fleet, fundingErr)
		}
		add(Action{ID: fundHotkeyID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("challenger-fleet-hotkey:%d", fleet), Description: "fund the challenger hotkey's bounded commitment-write fees and runtime keep-alive balance", Parameters: map[string]string{"maximum_fee_rao": fmt.Sprint(nativeFeeLimit), "keep_alive_reserve_rao": fmt.Sprint(facts.ExistentialDepositRao)}, Spend: Spend{TAORao: commitmentFunding}, DependsOn: []string{fundID}})
		setupDeps = append(setupDeps, fundID, fundHotkeyID)
	}
	setupDeps = append(setupDeps, "evm.vault-register-escrow")
	for i := 0; i < validatorCount; i++ {
		fundID := fmt.Sprintf("validator.fund.%d", i+1)
		add(Action{ID: fundID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("validator-coldkey:%d", i+1), Description: "fund the independent validator coldkey for one burn, bounded fees, and runtime keep-alive balance", Parameters: registrationFundingParameters(), Spend: Spend{TAORao: roleFunding}, DependsOn: []string{"subnet.verify-owner", lastChurn}})
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
	nativeWrites += uint64(cfg.Config.Topology.HeadFleets + 2*cfg.Config.Topology.ChallengerFleets + 3)
	feeReserve, ok := checkedMul(nativeWrites, nativeFeeLimit)
	if !ok {
		return nil, fmt.Errorf("native fee reserve overflow")
	}
	add(Action{ID: "wallet.native-fee-reserve", Kind: "budget-reserve", Target: cfg.WalletPublic, Description: "reserve the approved fee ceiling for every planned native extrinsic", Parameters: map[string]string{"maximum_fee_rao": fmt.Sprint(nativeFeeLimit), "native_writes": fmt.Sprint(nativeWrites)}, Spend: Spend{TAORao: feeReserve}, DependsOn: []string{"subnet.verify-owner"}})
	setupDeps = append(setupDeps, "wallet.native-fee-reserve")
	runtimeGas := cfg.MaximumEVMGasWei - allocatedSetupGas
	voluntaryGas := runtimeGas / 20
	productionGas := runtimeGas / 20
	retirementGas := runtimeGas / 20
	governanceGas := runtimeGas / 10
	precompileGas := runtimeGas / 8
	dishonestDepositGas := runtimeGas / 40
	if voluntaryGas == 0 || productionGas == 0 || retirementGas < uint64(operatorCount) || governanceGas < 10 || precompileGas < 10 || dishonestDepositGas == 0 {
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
	add(Action{ID: "campaign.evm-gas-reserve", Kind: "budget-reserve", Target: cfg.Config.Deployment.DeploymentID, Description: "reserve gas for deposits, payout roots, keepers, and claims during the live campaign", Spend: Spend{EVMGasWei: runtimeGas - voluntaryGas - productionGas - retirementGas - governanceGas - precompileGas - dishonestDepositGas}, DependsOn: setupDeps})
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
	lastChallenger := "topology.launch"
	for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
		fleet := cfg.Config.Topology.HeadFleets + challenger
		registerID := fmt.Sprintf("fleet.register.%d", fleet)
		commitID := fmt.Sprintf("fleet.commitment.%d", fleet)
		mirrorID := fmt.Sprintf("fleet.mirror.%d", fleet)
		add(Action{ID: registerID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("challenger-fleet:%d", fleet), Description: fmt.Sprintf("register the measured challenger into the full subnet and prove it replaces churn-floor UID %d", challenger), Parameters: registrationParameters(), Spend: Spend{Registrations: 1}, DependsOn: []string{lastChallenger, fmt.Sprintf("fleet.fund.%d", fleet), fmt.Sprintf("churn.register.%d", challenger)}})
		add(Action{ID: commitID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("challenger-fleet:%d", fleet), Description: "publish the challenger fleet manifest commitment and verify finalized storage", DependsOn: []string{registerID, fmt.Sprintf("fleet.fund-hotkey.%d", fleet)}})
		add(Action{ID: mirrorID, Kind: "evm-transaction", Target: fmt.Sprintf("challenger-fleet:%d", fleet), Description: "mirror the challenger commitment after its native registration finalizes", Spend: Spend{EVMGasWei: gasCaps[mirrorID]}, DependsOn: []string{commitID, "evm.fund-commitment-oracle"}})
		lastChallenger = mirrorID
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			bindID := fmt.Sprintf("fleet.bind.%d.%d", fleet, member)
			miner := fleetMemberMinerIndex(cfg, fleet, member)
			add(Action{ID: bindID, Kind: "evm-transaction", Target: fmt.Sprintf("miner:%d", miner), Description: "relay one challenger member binding effective next epoch", Spend: Spend{EVMGasWei: gasCaps[bindID]}, DependsOn: []string{lastChallenger, "evm.fund-keeper"}})
			lastChallenger = bindID
		}
	}
	add(Action{ID: "churn.tournament-complete", Kind: "local", Target: fmt.Sprintf("netuid:%d", cfg.Netuid), Description: "prove both live challengers replaced exactly the two oldest churn-floor UIDs while all 202 measured fleets remain registered", DependsOn: []string{lastChallenger}})
	add(Action{ID: "precompile.commitment-write", Kind: "substrate-extrinsic", Target: "head-fleet:1", Description: "write and finalized-read an exact one-field SHA-256 conformance commitment from the registered test hotkey", DependsOn: []string{"churn.tournament-complete"}})
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
	productionNames := make([]string, 0, len(cfg.Hyperparameters.ProductionOwnerControlled))
	for name := range cfg.Hyperparameters.ProductionOwnerControlled {
		productionNames = append(productionNames, name)
	}
	sort.Strings(productionNames)
	productionDependency := "production.schedule-policy"
	for _, name := range productionNames {
		id := "production.hyperparameter." + name
		value := cfg.Hyperparameters.ProductionOwnerControlled[name]
		add(Action{ID: id, Kind: "substrate-extrinsic", Target: fmt.Sprintf("netuid:%d", cfg.Netuid), Description: fmt.Sprintf("after M2, converge %s to its production value and verify finalized state", name), Parameters: map[string]string{"value": fmt.Sprint(value)}, DependsOn: []string{productionDependency}})
		productionDependency = id
	}
	add(Action{
		ID: "campaign.dishonest-deposit.2", Kind: "evm-transaction", Target: "no:2",
		Description: "post a deliberate one-rao demand deposit in the first fresh production-cadence epoch and prove live validators reject the signed-usage mismatch",
		Parameters:  map[string]string{"no_id": "2", "amount_rao": "1", "target_epoch": "next_fresh_production_epoch"},
		Spend:       Spend{EVMGasWei: dishonestDepositGas}, DependsOn: []string{"topology.launch", "campaign.evm-gas-reserve", productionDependency},
	})
	add(Action{ID: "retirement.evm-gas-reserve", Kind: "budget-reserve", Target: cfg.Config.Deployment.DeploymentID, Description: "reserve a separately approved gas ceiling for future-effective retirement of every operator", Parameters: map[string]string{"operators": fmt.Sprint(operatorCount)}, Spend: Spend{EVMGasWei: retirementGas}, DependsOn: []string{"topology.launch"}})
	p.MaximumSpend, err = maximumActionSpend(p.Actions)
	if err != nil {
		return nil, err
	}
	p.Limits = configuredPlanLimits(cfg)
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

// Sum the approval ceiling from its canonical action list without permitting
// any integer field to wrap.
func maximumActionSpend(actions []Action) (Spend, error) {
	var maximum Spend
	for _, action := range actions {
		var ok bool
		maximum.TAORao, ok = checkedAdd(maximum.TAORao, action.Spend.TAORao)
		if !ok {
			return Spend{}, errors.New("TAO plan overflow")
		}
		maximum.AlphaRao, ok = checkedAdd(maximum.AlphaRao, action.Spend.AlphaRao)
		if !ok {
			return Spend{}, errors.New("alpha plan overflow")
		}
		maximum.EVMGasWei, ok = checkedAdd(maximum.EVMGasWei, action.Spend.EVMGasWei)
		if !ok {
			return Spend{}, errors.New("gas plan overflow")
		}
		if math.MaxUint32-maximum.Registrations < action.Spend.Registrations {
			return Spend{}, errors.New("registration plan overflow")
		}
		maximum.Registrations += action.Spend.Registrations
		if math.MaxUint32-maximum.SubnetCreations < action.Spend.SubnetCreations {
			return Spend{}, errors.New("subnet-creation plan overflow")
		}
		maximum.SubnetCreations += action.Spend.SubnetCreations
	}
	return maximum, nil
}

// Runtime 451 bumps burn immediately after a registration and decays it on
// every following block. The bootstrap sets a one-block half-life, so an
// observed multiplier no greater than two guarantees every sequential
// registration returns to at most the same approved ceiling by the next block.
func validateRegistrationEconomics(cfg *ResolvedConfig, facts *SetupFacts, registrationBurnLimit uint64) error {
	if cfg == nil || cfg.Hyperparameters == nil || facts == nil {
		return errors.New("registration economics configuration is unavailable")
	}
	if facts.MinBurnRao == 0 || facts.MaxBurnRao < facts.MinBurnRao || facts.BurnRao < facts.MinBurnRao || facts.BurnRao > facts.MaxBurnRao || facts.BurnRao > registrationBurnLimit || facts.MinBurnRao > registrationBurnLimit {
		return fmt.Errorf("registration burn bounds current=%d min=%d max=%d are incompatible with limit %d", facts.BurnRao, facts.MinBurnRao, facts.MaxBurnRao, registrationBurnLimit)
	}
	bootstrapHalfLife := hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["burn_half_life"])
	productionHalfLife := hyperparameterUint64(cfg.Hyperparameters.ProductionOwnerControlled["burn_half_life"])
	if bootstrapHalfLife != 1 || productionHalfLife == 0 || (uint64(facts.BurnHalfLifeBlocks) != bootstrapHalfLife && uint64(facts.BurnHalfLifeBlocks) != productionHalfLife) {
		return fmt.Errorf("registration burn half-life current=%d bootstrap=%d production=%d does not provide the approved one-block bootstrap envelope", facts.BurnHalfLifeBlocks, bootstrapHalfLife, productionHalfLife)
	}
	multiplier, ok := new(big.Int).SetString(facts.BurnIncreaseMultQ64, 10)
	if !ok || multiplier.Sign() <= 0 {
		return fmt.Errorf("registration burn multiplier %q is not a positive Q64 integer", facts.BurnIncreaseMultQ64)
	}
	oneQ64 := new(big.Int).Lsh(big.NewInt(1), 64)
	twoQ64 := new(big.Int).Lsh(big.NewInt(2), 64)
	if multiplier.Cmp(oneQ64) < 0 || multiplier.Cmp(twoQ64) > 0 {
		return fmt.Errorf("registration burn multiplier Q64 %s is outside the one-through-two bootstrap envelope", multiplier)
	}
	return nil
}

// Bound a native registration coldkey for the runtime charge, the signed
// extrinsic fee, and the balance that Preservation::Preserve must leave alive.
func registrationRoleFunding(burnLimitRao, feeLimitRao, keepAliveRao uint64) (uint64, error) {
	chargedRao, ok := checkedAdd(burnLimitRao, feeLimitRao)
	if !ok {
		return 0, errors.New("native registration burn and fee funding overflow")
	}
	fundingRao, ok := checkedAdd(chargedRao, keepAliveRao)
	if !ok {
		return 0, errors.New("native registration keep-alive funding overflow")
	}
	return fundingRao, nil
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
	if p.Schema == "urnetwork-sim-plan-v2" {
		// Runtime 451 decays Burn on every block. V2 binds MinBurn, MaxBurn,
		// BurnIncreaseMult, the approved half-life lifecycle, and the hard
		// registration limit instead; apply rechecks the moving spot value.
		p.LiveFacts.BurnRao = 0
	}
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
	if p.Schema == "urnetwork-sim-plan-v2" && p.NativeTransactionFeeLimitRao == 0 {
		return errors.New("release plan has no per-transaction native fee limit")
	}
	if p.Schema == "urnetwork-sim-plan-v2" && (p.BootstrapBurnHalfLifeBlocks != 1 || p.ProductionBurnHalfLifeBlocks == 0) {
		return errors.New("release plan has no bounded bootstrap/production burn half-life lifecycle")
	}
	if p.Schema != "urnetwork-sim-plan-v1" && p.Schema != "urnetwork-sim-plan-v2" {
		return fmt.Errorf("unsupported setup plan schema %q", p.Schema)
	}
	seenPriorPlans := map[string]bool{}
	for _, hash := range p.PriorPlanHashes {
		if _, err := decodeHex32("prior plan hash", hash); err != nil {
			return err
		}
		if seenPriorPlans[hash] || hash == p.PlanHash {
			return fmt.Errorf("setup plan has a duplicate or self-referential prior plan hash %s", hash)
		}
		seenPriorPlans[hash] = true
	}
	seenActions := make(map[string]bool, len(p.Actions))
	for _, action := range p.Actions {
		if action.ID == "" || seenActions[action.ID] {
			return fmt.Errorf("setup plan has an empty or duplicate action id %q", action.ID)
		}
		intentHash, err := actionIntentHash(action)
		if err != nil {
			return fmt.Errorf("hash action %s intent: %w", action.ID, err)
		}
		if action.IntentHash == "" || action.IntentHash != intentHash {
			return fmt.Errorf("action %s intent hash does not bind its executable fields", action.ID)
		}
		for _, dependency := range action.DependsOn {
			if !seenActions[dependency] {
				return fmt.Errorf("action %s depends on missing or later action %s", action.ID, dependency)
			}
		}
		seenActions[action.ID] = true
		if strings.HasPrefix(action.ID, "evm.fund-") {
			if _, err := evmFundingTerms(action, p.LiveFacts.ExistentialDepositRao); err != nil {
				return err
			}
		}
		if action.Spend.Registrations == 0 {
			continue
		}
		limit, err := strconv.ParseUint(action.Parameters["maximum_burn_rao"], 10, 64)
		if err != nil || limit != p.RegistrationBurnLimitRao {
			return fmt.Errorf("registration action %s does not bind the plan burn limit", action.ID)
		}
	}
	maximumSpend, err := maximumActionSpend(p.Actions)
	if err != nil {
		return err
	}
	if maximumSpend != p.MaximumSpend {
		return fmt.Errorf("action spend total %+v does not equal plan maximum %+v", maximumSpend, p.MaximumSpend)
	}
	if p.MaximumSpend.SubnetCreations != 0 || p.Limits.SubnetCreations != 0 {
		return fmt.Errorf("subnet creation is forbidden")
	}
	return nil
}

// Return the exact plan lineage whose journaled action intents may be carried
// into this approved revision after their durable and live postconditions are
// revalidated.
func (self *SetupPlan) allowedPlanHashes() map[string]bool {
	result := map[string]bool{}
	if self == nil {
		return result
	}
	if self.PlanHash != "" {
		result[self.PlanHash] = true
	}
	for _, hash := range self.PriorPlanHashes {
		if hash != "" {
			result[hash] = true
		}
	}
	return result
}

// evmFundingTerms validates the approval-bound distinction between a role's
// usable EVM balance and the one-time native existential deposit needed to
// keep its mirror account alive.
func evmFundingTerms(action Action, expectedExistentialDepositRao uint64) (uint64, error) {
	if expectedExistentialDepositRao == 0 {
		return 0, errors.New("approved existential deposit is zero")
	}
	if len(action.Parameters) != 2 {
		return 0, fmt.Errorf("EVM funding action %s has %d parameters, want 2", action.ID, len(action.Parameters))
	}
	usable, err := strconv.ParseUint(action.Parameters["usable_evm_rao"], 10, 64)
	if err != nil || usable == 0 {
		return 0, fmt.Errorf("EVM funding action %s has invalid usable_evm_rao", action.ID)
	}
	deposit, err := strconv.ParseUint(action.Parameters["existential_deposit_rao"], 10, 64)
	if err != nil || deposit != expectedExistentialDepositRao {
		return 0, fmt.Errorf("EVM funding action %s existential deposit does not match approved %d rao", action.ID, expectedExistentialDepositRao)
	}
	maximumTransfer, ok := checkedAdd(usable, deposit)
	if !ok || action.Spend.TAORao != maximumTransfer || action.Spend.AlphaRao != 0 || action.Spend.EVMGasWei != 0 || action.Spend.Registrations != 0 || action.Spend.SubnetCreations != 0 {
		return 0, fmt.Errorf("EVM funding action %s maximum spend does not equal usable balance plus existential deposit", action.ID)
	}
	return usable, nil
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
		a, e = address(fmt.Sprintf("operator-%d-claim-relayer", i+1))
		if e != nil {
			return r, e
		}
		r.ClaimRelayers = append(r.ClaimRelayers, a)
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
