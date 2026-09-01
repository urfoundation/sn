package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/ss58"
)

type Spend struct {
	TAORao          uint64      `json:"tao_rao"`
	AlphaRao        uint64      `json:"alpha_rao"`
	EVMGasWei       DecimalUint `json:"evm_gas_wei"`
	Registrations   uint32      `json:"registrations"`
	SubnetCreations uint32      `json:"subnet_creations"`
}
type Action struct {
	ID                        string            `json:"id"`
	Kind                      string            `json:"kind"`
	Target                    string            `json:"target"`
	Description               string            `json:"description"`
	Parameters                map[string]string `json:"parameters,omitempty"`
	Spend                     Spend             `json:"maximum_spend"`
	DependsOn                 []string          `json:"depends_on,omitempty"`
	AcceptedPriorIntentHashes []string          `json:"accepted_prior_intent_hashes,omitempty"`
	IntentHash                string            `json:"intent_hash"`
}

const (
	setupPlanSchemaV8                     = "urnetwork-sim-plan-v8"
	setupPlanSchemaV9                     = "urnetwork-sim-plan-v9"
	currentSetupPlanSchema                = "urnetwork-sim-plan-v10"
	evmMaximumGasUnitsParameter           = "maximum_gas_units"
	evmMaximumFeePerGasParameter          = "maximum_fee_per_gas_wei"
	deploymentManifestHashParameter       = "deployment_manifest_hash"
	fleetCommitmentStorageParameter       = "commitment_storage_schema"
	fleetCommitmentStorageV2              = "runtime-452-fixed-u32-exact-block-attestation-v2"
	fleetCommitmentParallelGroupParameter = "parallel_commitment_group"
	fleetCommitmentParallelWorkers        = 10
	fleetRefreshBatchSize                 = 10
)

type SetupPlan struct {
	Schema                       string                     `json:"schema"`
	Release                      string                     `json:"release"`
	ReleaseLockHash              string                     `json:"release_lock_hash"`
	DeploymentID                 string                     `json:"deployment_id"`
	ChainID                      uint64                     `json:"chain_id"`
	GenesisHash                  string                     `json:"genesis_hash"`
	Netuid                       uint16                     `json:"netuid"`
	Owner                        string                     `json:"owner"`
	LiveFacts                    SetupFacts                 `json:"live_facts"`
	RegistrationBurnLimitRao     uint64                     `json:"registration_burn_limit_rao"`
	NativeTransactionFeeLimitRao uint64                     `json:"native_transaction_fee_limit_rao,omitempty"`
	MaximumEVMFeePerGasWei       uint64                     `json:"maximum_evm_fee_per_gas_wei,omitempty"`
	AlphaTransferMarginBPS       uint16                     `json:"alpha_transfer_margin_bps,omitempty"`
	MinimumSourceRemainingRao    uint64                     `json:"minimum_source_remaining_alpha_rao,omitempty"`
	BootstrapBurnHalfLifeBlocks  uint16                     `json:"bootstrap_burn_half_life_blocks,omitempty"`
	ProductionBurnHalfLifeBlocks uint16                     `json:"production_burn_half_life_blocks,omitempty"`
	PriorPlanHashes              []string                   `json:"prior_plan_hashes,omitempty"`
	ConfigHash                   string                     `json:"config_hash"`
	ResolvedInputsHash           string                     `json:"resolved_inputs_hash"`
	PolicyHash                   string                     `json:"policy_hash"`
	Roles                        PublicRoles                `json:"roles"`
	Deployment                   ContractDeployment         `json:"deployment"`
	CoordinatorUpgrade           CoordinatorUpgrade         `json:"coordinator_upgrade"`
	CoordinatorUpgradeBaseline   CoordinatorUpgradeBaseline `json:"coordinator_upgrade_baseline,omitempty"`
	SupersededDeployments        []ContractDeployment       `json:"superseded_deployments,omitempty"`
	Actions                      []Action                   `json:"actions"`
	MaximumSpend                 Spend                      `json:"maximum_spend"`
	SupersededSpend              Spend                      `json:"superseded_spend,omitempty"`
	Limits                       Spend                      `json:"limits"`
	PlanHash                     string                     `json:"plan_hash"`
	GeneratedAt                  string                     `json:"generated_at,omitempty"`
}

// orderedJSONField retains the field order emitted by encoding/json. Plan
// hashes before schema v4 were computed from structs whose wire shape was
// smaller than the current SetupPlan, so re-marshalling them through the
// current type would add zero-valued fields and invalidate a valid approval.
type orderedJSONField struct {
	Name  string
	Value json.RawMessage
}

// Decode one JSON object without discarding its authenticated field order or
// accepting duplicate keys.
func decodeOrderedJSONObject(raw []byte) ([]orderedJSONField, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("ordered JSON value is not an object")
	}
	fields := []orderedJSONField{}
	seen := map[string]bool{}
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, tokenErr
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return nil, fmt.Errorf("ordered JSON object has an invalid or duplicate field %q", name)
		}
		var value json.RawMessage
		if decodeErr := decoder.Decode(&value); decodeErr != nil {
			return nil, decodeErr
		}
		seen[name] = true
		fields = append(fields, orderedJSONField{Name: name, Value: append(json.RawMessage(nil), value...)})
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return nil, errors.New("ordered JSON object has no closing delimiter")
	}
	if token, trailingErr := decoder.Token(); trailingErr != io.EOF {
		if trailingErr != nil {
			return nil, fmt.Errorf("decode ordered JSON trailing value: %w", trailingErr)
		}
		return nil, fmt.Errorf("ordered JSON object has trailing value %v", token)
	}
	return fields, nil
}

// Encode an ordered object in the same compact form used by json.Marshal.
func encodeOrderedJSONObject(fields []orderedJSONField) ([]byte, error) {
	var encoded bytes.Buffer
	encoded.WriteByte('{')
	for index, field := range fields {
		if index != 0 {
			encoded.WriteByte(',')
		}
		name, err := json.Marshal(field.Name)
		if err != nil {
			return nil, err
		}
		encoded.Write(name)
		encoded.WriteByte(':')
		if err := json.Compact(&encoded, field.Value); err != nil {
			return nil, fmt.Errorf("compact ordered JSON field %s: %w", field.Name, err)
		}
	}
	encoded.WriteByte('}')
	return encoded.Bytes(), nil
}

// Replace one required ordered-object field without changing any sibling
// encoding. This is used only for the observation fields that hash() has
// always normalized before an approval digest is computed.
func replaceOrderedJSONField(fields []orderedJSONField, name string, value json.RawMessage) error {
	for index := range fields {
		if fields[index].Name == name {
			fields[index].Value = append(json.RawMessage(nil), value...)
			return nil
		}
	}
	return fmt.Errorf("ordered JSON object is missing required field %s", name)
}

// Replace a field only when its schema revision emitted it. Observation fields
// added in a later revision must receive the same normalization as hash(), but
// their absence from an older authenticated wire representation must remain
// meaningful: adding a zero-valued field would change that historical digest.
func replaceOptionalOrderedJSONField(fields []orderedJSONField, name string, value json.RawMessage) {
	for index := range fields {
		if fields[index].Name == name {
			fields[index].Value = append(json.RawMessage(nil), value...)
			return
		}
	}
}

// Recompute a persisted plan's digest from its historical wire representation.
// The stored order is authenticated, while only review-time observations and
// generated_at receive the same normalization performed by SetupPlan.hash.
func persistedSetupPlanHash(raw []byte, schema string) (string, error) {
	fields, err := decodeOrderedJSONObject(raw)
	if err != nil {
		return "", fmt.Errorf("decode persisted plan object: %w", err)
	}
	if err := replaceOrderedJSONField(fields, "plan_hash", json.RawMessage(`""`)); err != nil {
		return "", err
	}
	filtered := fields[:0]
	var liveFactsIndex = -1
	var upgradeBaselineIndex = -1
	for _, field := range fields {
		if field.Name == "generated_at" {
			continue
		}
		if field.Name == "live_facts" {
			liveFactsIndex = len(filtered)
		}
		if field.Name == "coordinator_upgrade_baseline" {
			upgradeBaselineIndex = len(filtered)
		}
		filtered = append(filtered, field)
	}
	if liveFactsIndex < 0 {
		return "", errors.New("persisted plan has no live_facts object")
	}
	liveFacts, err := decodeOrderedJSONObject(filtered[liveFactsIndex].Value)
	if err != nil {
		return "", fmt.Errorf("decode persisted plan live facts: %w", err)
	}
	for _, name := range []string{"finalized_block", "alpha_available_rao", "wallet_free_tao_rao"} {
		if err := replaceOrderedJSONField(liveFacts, name, json.RawMessage(`0`)); err != nil {
			return "", err
		}
	}
	for _, name := range []string{
		"alpha_transferable_rao",
		"alpha_source_stored_lock_rao",
		"alpha_source_collateral_rao",
		"wallet_netuid_alpha_rao",
		"wallet_netuid_collateral_rao",
	} {
		replaceOptionalOrderedJSONField(liveFacts, name, json.RawMessage(`0`))
	}
	if err := replaceOrderedJSONField(liveFacts, "finalized_block_hash", json.RawMessage(`""`)); err != nil {
		return "", err
	}
	if planUsesRegistrationEnvelope(schema) {
		if err := replaceOrderedJSONField(liveFacts, "burn_rao", json.RawMessage(`0`)); err != nil {
			return "", err
		}
	}
	if planUsesContractDeploymentEnvelope(schema) {
		if err := replaceOrderedJSONField(liveFacts, "evm_finalized_block", json.RawMessage(`0`)); err != nil {
			return "", err
		}
		if err := replaceOrderedJSONField(liveFacts, "evm_finalized_block_hash", json.RawMessage(`""`)); err != nil {
			return "", err
		}
	}
	filtered[liveFactsIndex].Value, err = encodeOrderedJSONObject(liveFacts)
	if err != nil {
		return "", err
	}
	if upgradeBaselineIndex >= 0 {
		baseline, baselineErr := decodeOrderedJSONObject(filtered[upgradeBaselineIndex].Value)
		if baselineErr != nil {
			return "", fmt.Errorf("decode persisted coordinator upgrade baseline: %w", baselineErr)
		}
		if err := replaceOrderedJSONField(baseline, "finalized_block", json.RawMessage(`0`)); err != nil {
			return "", err
		}
		if err := replaceOrderedJSONField(baseline, "finalized_block_hash", json.RawMessage(`""`)); err != nil {
			return "", err
		}
		filtered[upgradeBaselineIndex].Value, err = encodeOrderedJSONObject(baseline)
		if err != nil {
			return "", err
		}
	}
	canonical, err := encodeOrderedJSONObject(filtered)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "0x" + hex.EncodeToString(digest[:]), nil
}

// Recover an older wire digest after an archived plan was accidentally
// re-marshaled by the immediately newer binary. The listed observation fields
// did not belong to those schemas; only the exact zero value emitted by Go may
// be removed, never an approved field, nonzero value, or unrelated mutation.
func legacyArchivedSetupPlanHash(raw []byte, schema string) (string, bool, error) {
	injectedFacts := map[string]string{}
	if !planUsesAlphaTransferEnvelope(schema) {
		for _, name := range []string{
			"alpha_transferable_rao",
			"alpha_source_stored_lock_rao",
			"alpha_source_collateral_rao",
			"wallet_netuid_alpha_rao",
			"wallet_netuid_collateral_rao",
			"initial_min_stake_rao",
			"alpha_price_tao_per_alpha_q9",
			"registered_alpha_rao",
			"reserve_validator_alpha_rao",
			"independent_validator_alpha_rao",
		} {
			injectedFacts[name] = "0"
		}
		injectedFacts["alpha_source_registered"] = "false"
	}
	if !planUsesDefaultMinTransferEnvelope(schema) {
		injectedFacts["default_min_transfer_rao"] = "0"
	}
	injectedTopLevel := map[string]string{}
	if !planUsesContractDeploymentEnvelope(schema) {
		injectedFacts["deployer_nonce"] = "0"
		injectedFacts["evm_finalized_block"] = "0"
		injectedFacts["evm_finalized_block_hash"] = `""`
		injectedTopLevel["deployment"] = `{"schema":"","deployment_id":"","initial_nonce":0,"reserve_sink":"0x0000000000000000000000000000000000000000","settlement_vault":"0x0000000000000000000000000000000000000000","coordinator_implementation":"0x0000000000000000000000000000000000000000","coordinator_proxy":"0x0000000000000000000000000000000000000000","governance_drill_implementation":"0x0000000000000000000000000000000000000000","precompile_probe":"0x0000000000000000000000000000000000000000"}`
		injectedTopLevel["superseded_spend"] = `{"tao_rao":0,"alpha_rao":0,"evm_gas_wei":0,"registrations":0,"subnet_creations":0}`
	}
	if !planUsesCoordinatorUpgradeEnvelope(schema) {
		injectedTopLevel["coordinator_upgrade"] = `{"schema":"","deployment_id":"","implementation":"0x0000000000000000000000000000000000000000","deployer_nonce":0,"runtime_code_hash":""}`
		injectedTopLevel["coordinator_upgrade_baseline"] = `{"schema":"","prior_deployment_hash":"","release_deployment_hash":"","rebound_deployment_hash":"","reserve_sink_executable_hash":"","settlement_vault_executable_hash":"","governance_drill_version":"","governance_proxiable_uuid":"","deployer_nonce":0,"probe_address_empty":false,"finalized_block":0,"finalized_block_hash":""}`
	}
	if len(injectedFacts) == 0 && len(injectedTopLevel) == 0 {
		return "", false, nil
	}
	fields, err := decodeOrderedJSONObject(raw)
	if err != nil {
		return "", false, err
	}
	liveFactsIndex := -1
	removed := false
	for index := range fields {
		if fields[index].Name != "live_facts" {
			continue
		}
		liveFactsIndex = index
		liveFacts, decodeErr := decodeOrderedJSONObject(fields[index].Value)
		if decodeErr != nil {
			return "", false, decodeErr
		}
		filtered := liveFacts[:0]
		for _, field := range liveFacts {
			if zero, known := injectedFacts[field.Name]; known {
				if string(bytes.TrimSpace(field.Value)) != zero {
					return "", false, nil
				}
				removed = true
				continue
			}
			filtered = append(filtered, field)
		}
		fields[index].Value, err = encodeOrderedJSONObject(filtered)
		if err != nil {
			return "", false, err
		}
		break
	}
	if liveFactsIndex < 0 {
		return "", false, errors.New("persisted plan has no live_facts object")
	}
	filtered := fields[:0]
	for _, field := range fields {
		expected, known := injectedTopLevel[field.Name]
		if !known {
			filtered = append(filtered, field)
			continue
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, field.Value); err != nil {
			return "", false, err
		}
		if compact.String() != expected {
			return "", false, nil
		}
		removed = true
	}
	if !removed {
		return "", false, nil
	}
	mutated, encodeErr := encodeOrderedJSONObject(filtered)
	if encodeErr != nil {
		return "", false, encodeErr
	}
	hash, hashErr := persistedSetupPlanHash(mutated, schema)
	return hash, true, hashErr
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

// Distinguish legacy plans from registration-envelope plans and the current
// EVM fee-envelope revision without parsing arbitrary schema strings.
func planUsesRegistrationEnvelope(schema string) bool {
	return schema == "urnetwork-sim-plan-v2" || schema == "urnetwork-sim-plan-v3" || planUsesEVMFeeEnvelope(schema)
}

// Identify plans whose EVM transactions bind gas units and fee price
// independently in addition to their aggregate wei ceiling.
func planUsesEVMFeeEnvelope(schema string) bool {
	return schema == "urnetwork-sim-plan-v3" || planUsesContractDeploymentEnvelope(schema)
}

func planUsesContractDeploymentEnvelope(schema string) bool {
	return schema == "urnetwork-sim-plan-v4" || planUsesAlphaTransferEnvelope(schema)
}

func planUsesAlphaTransferEnvelope(schema string) bool {
	return schema == "urnetwork-sim-plan-v5" || schema == "urnetwork-sim-plan-v6" || schema == "urnetwork-sim-plan-v7" || schema == setupPlanSchemaV8 || schema == setupPlanSchemaV9 || schema == currentSetupPlanSchema
}

func planUsesCoordinatorUpgradeEnvelope(schema string) bool {
	return schema == "urnetwork-sim-plan-v6" || schema == "urnetwork-sim-plan-v7" || schema == setupPlanSchemaV8 || schema == setupPlanSchemaV9 || schema == currentSetupPlanSchema
}

func planUsesDefaultMinTransferEnvelope(schema string) bool {
	return schema == "urnetwork-sim-plan-v7" || schema == setupPlanSchemaV8 || schema == setupPlanSchemaV9 || schema == currentSetupPlanSchema
}

// V8 introduced bounded destination-share floors. Later schemas retain that
// wire contract; v9 additionally binds both runtime share transitions.
func planUsesDestinationRoundingEnvelope(schema string) bool {
	return schema == setupPlanSchemaV8 || schema == setupPlanSchemaV9 || schema == currentSetupPlanSchema
}

func planUsesTwoTransitionReserveEnvelope(schema string) bool {
	return schema == setupPlanSchemaV9 || schema == currentSetupPlanSchema
}

func supportedSetupPlanSchema(schema string) bool {
	return schema == "urnetwork-sim-plan-v1" || schema == "urnetwork-sim-plan-v2" || schema == "urnetwork-sim-plan-v3" || schema == "urnetwork-sim-plan-v4" || planUsesAlphaTransferEnvelope(schema)
}

// Identify actions whose meaning or durable postcondition changes when an
// immutable contract generation is replaced. Native subnet mutations and
// independently keyed role funding remain carryable across that replacement.
func actionUsesContractDeployment(action Action) bool {
	id := action.ID
	if id == "subnet.verify-owner" || strings.HasPrefix(id, "subnet.hyperparameter.") || strings.HasPrefix(id, "production.hyperparameter.") || strings.HasPrefix(id, "evm.fund-") || strings.HasPrefix(id, "churn.") || strings.HasPrefix(id, "fleet.fund.") || strings.HasPrefix(id, "fleet.register.") || strings.HasPrefix(id, "fleet.fund-hotkey.") || strings.HasPrefix(id, "validator.fund.") || strings.HasPrefix(id, "validator.register.") || strings.HasPrefix(id, "validator.take-zero.") || strings.HasPrefix(id, "alpha.transfer.validator.") || strings.HasPrefix(id, "alpha.repair.validator.") || id == "validator.reserve-majority" || strings.HasPrefix(id, "operator.deposit.register.") || id == "wallet.native-fee-reserve" {
		return false
	}
	return true
}

func alphaTransferActionParameters(exactAmountRao, campaignRequirementRao, minimumAlphaRao uint64, facts *SetupFacts, marginBPS uint16) map[string]string {
	minimumCredit, _ := alphaTransferMinimumCreditRao(exactAmountRao)
	return map[string]string{
		"exact_amount_rao":                           strconv.FormatUint(exactAmountRao, 10),
		"campaign_requirement_rao":                   strconv.FormatUint(campaignRequirementRao, 10),
		"minimum_alpha_at_approved_price_rao":        strconv.FormatUint(minimumAlphaRao, 10),
		"approved_alpha_price_q9":                    strconv.FormatUint(facts.AlphaPriceQ9, 10),
		"runtime_default_min_transfer_tao_rao":       strconv.FormatUint(facts.DefaultMinTransferRao, 10),
		"minimum_tao_equivalent_margin_bps":          strconv.FormatUint(uint64(marginBPS), 10),
		"maximum_destination_rounding_shortfall_rao": strconv.FormatUint(alphaTransferDestinationRoundingAllowance, 10),
		"minimum_destination_credit_rao":             strconv.FormatUint(minimumCredit, 10),
	}
}

// Derive setup unit ceilings from the locked Foundry gas report with margin
// for live runtime/precompile accounting and the manager's padded estimate.
func setupEVMGasUnitLimits(cfg *ResolvedConfig) map[string]uint64 {
	limits := map[string]uint64{
		"evm.reserve-sink":                       600_000,
		"evm.settlement-vault":                   3_000_000,
		"evm.coordinator-implementation":         7_500_000,
		"evm.vault-register-escrow":              1_000_000,
		"evm.coordinator-proxy":                  1_500_000,
		"evm.governance-drill-implementation":    7_500_000,
		"evm.vault-fix-coordinator":              150_000,
		"evm.sink-fix-recorder":                  150_000,
		"precompile.probe-deploy":                3_000_000,
		"evm.coordinator-upgrade-implementation": 7_500_000,
		"evm.coordinator-upgrade-activate":       500_000,
		"policy.schedule-bootstrap":              500_000,
		"fleet.refresh.deploy-batcher":           1_500_000,
		"fleet.refresh.oracle-activate":          300_000,
		"fleet.refresh.oracle-restore":           300_000,
	}
	if cfg == nil || cfg.Config == nil {
		return limits
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		limits[fmt.Sprintf("operator.deposit.register.%d", operator)] = 1_000_000
		limits[fmt.Sprintf("operator.register.%d", operator)] = 750_000
	}
	for fleet := cfg.Config.Topology.HeadFleets + 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		limits[fmt.Sprintf("fleet.mirror.%d", fleet)] = 200_000
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			limits[fmt.Sprintf("fleet.bind.%d.%d", fleet, member)] = 400_000
		}
	}
	for batch := 1; batch <= (cfg.Config.Topology.HeadFleets+fleetRefreshBatchSize-1)/fleetRefreshBatchSize; batch++ {
		limits[fmt.Sprintf("fleet.install.batch.%d", batch)] = 18_000_000
		limits[fmt.Sprintf("fleet.refresh.batch.%d", batch)] = 24_000_000
	}
	return limits
}

// Bind every executable field used to distinguish an action across plan
// revisions and journal recovery.
func actionIntentHash(action Action) (string, error) {
	if len(action.AcceptedPriorIntentHashes) == 0 {
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
	return canonicalHashHex(struct {
		ID, Kind, Target, Description string
		Parameters                    map[string]string
		Spend                         Spend
		DependsOn                     []string
		AcceptedPriorIntentHashes     []string
	}{
		ID:                        action.ID,
		Kind:                      action.Kind,
		Target:                    action.Target,
		Description:               action.Description,
		Parameters:                action.Parameters,
		Spend:                     action.Spend,
		DependsOn:                 action.DependsOn,
		AcceptedPriorIntentHashes: action.AcceptedPriorIntentHashes,
	})
}

func actionAcceptsIntent(action Action, intentHash string) bool {
	if intentHash == action.IntentHash {
		return true
	}
	for _, accepted := range action.AcceptedPriorIntentHashes {
		if intentHash == accepted {
			return true
		}
	}
	return false
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
		MaximumEVMGasWei     DecimalUint
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

func validateFreshSubnetTopology(cfg *ResolvedConfig, facts *SetupFacts, generation uint64) error {
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
	plannedLabels := []string{escrowHotkeyLabelForGeneration(generation)}
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		plannedLabels = append(plannedLabels, churnHotkeyLabel(churn))
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		plannedLabels = append(plannedLabels, fleetHotkeyLabel(fleet))
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		plannedLabels = append(plannedLabels, operatorPoolHotkeyLabelForGeneration(operator, generation), fmt.Sprintf("operator-%d-deposit-hotkey", operator))
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
	return buildPlanWithRegistrationGeneration(cfg, facts, roles, generatedAt, 0)
}

func buildPlanWithRegistrationGeneration(cfg *ResolvedConfig, facts *SetupFacts, roles PublicRoles, generatedAt time.Time, generation uint64) (*SetupPlan, error) {
	if facts == nil || facts.BurnRao == 0 || facts.AlphaSourceHotkey == "" || facts.ExistentialDepositRao == 0 || facts.ProbeTAORao == 0 {
		return nil, fmt.Errorf("finalized burn, alpha source, existential-deposit, and probe-value facts are required")
	}
	if err := validateFreshSubnetTopology(cfg, facts, generation); err != nil {
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
		return nil, stateMismatchError(err, "release lock hash is empty")
	}
	resolvedHash, err := resolvedInputsHash(cfg)
	if err != nil {
		return nil, fmt.Errorf("hash resolved launch inputs: %w", err)
	}
	roleSecrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, fmt.Errorf("derive deployment roles: %w", err)
	}
	payloads, err := buildDeploymentPayloadsWithRegistrationGeneration(cfg, roleSecrets, facts.DeployerNonce, generation)
	if err != nil {
		return nil, fmt.Errorf("build approved deployment payloads: %w", err)
	}
	deploymentHash, err := contractDeploymentIdentityHash(payloads.Manifest)
	if err != nil {
		return nil, fmt.Errorf("hash approved deployment manifest: %w", err)
	}
	nativeFeeLimit := cfg.Config.Budgets.MaximumNativeTransactionFeeRao
	if nativeFeeLimit == 0 {
		return nil, fmt.Errorf("native transaction fee limit is required")
	}
	bootstrapBurnHalfLife := uint16(hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["burn_half_life"]))
	productionBurnHalfLife := uint16(hyperparameterUint64(cfg.Hyperparameters.ProductionOwnerControlled["burn_half_life"]))
	p := &SetupPlan{Schema: currentSetupPlanSchema, Release: "1.0", ReleaseLockHash: releaseLockHash, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: testnetChainID, GenesisHash: testnetGenesis, Netuid: cfg.Netuid, Owner: cfg.WalletPublic, LiveFacts: *facts, RegistrationBurnLimitRao: registrationBurnLimit, NativeTransactionFeeLimitRao: nativeFeeLimit, MaximumEVMFeePerGasWei: cfg.Config.Budgets.MaximumEVMFeePerGasWei, AlphaTransferMarginBPS: cfg.Config.AlphaTransfers.MinimumTAOEquivalentMarginBPS, MinimumSourceRemainingRao: cfg.Config.ValidatorBootstrap.MinimumSourceRemainingAlphaRao, BootstrapBurnHalfLifeBlocks: bootstrapBurnHalfLife, ProductionBurnHalfLifeBlocks: productionBurnHalfLife, ConfigHash: cfg.ConfigHash, ResolvedInputsHash: resolvedHash, PolicyHash: cfg.PolicyHash, Roles: roles, Deployment: payloads.Manifest, CoordinatorUpgrade: payloads.CoordinatorUpgrade, GeneratedAt: generatedAt.Format(time.RFC3339)}
	add := func(a Action) {
		if actionUsesContractDeployment(a) {
			parameters := make(map[string]string, len(a.Parameters)+1)
			for key, value := range a.Parameters {
				parameters[key] = value
			}
			parameters[deploymentManifestHashParameter] = deploymentHash
			a.Parameters = parameters
		}
		if a.Kind == "evm-transaction" {
			parameters := make(map[string]string, len(a.Parameters)+2)
			for key, value := range a.Parameters {
				parameters[key] = value
			}
			maximumGasUnits, _ := divideDecimalUint(a.Spend.EVMGasWei, p.MaximumEVMFeePerGasWei)
			parameters[evmMaximumGasUnitsParameter] = maximumGasUnits.String()
			parameters[evmMaximumFeePerGasParameter] = strconv.FormatUint(p.MaximumEVMFeePerGasWei, 10)
			a.Parameters = parameters
		}
		if envelope, ok := deploymentActionEnvelope(payloads, a.ID, registrationBurnLimit); ok {
			for key, value := range envelope {
				a.Parameters[key] = value
			}
		}
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
			productionDust = cfg.Config.Scenarios.DishonestDepositRao
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
	if facts.DefaultMinTransferRao == 0 || facts.AlphaPriceQ9 == 0 || facts.RegisteredAlphaRao == 0 || !facts.AlphaSourceRegistered || facts.AlphaTransferableRao == 0 || facts.WalletNetuidAlphaRao < facts.AlphaAvailableRao {
		return nil, errors.New("runtime alpha-transfer minimum, finalized price, registered stake, and a transferable registered source are required")
	}
	if facts.DefaultMinTransferRao != cfg.Public.Chain.ExpectedDefaultMinTransferRao {
		return nil, fmt.Errorf("runtime DefaultMinTransfer %d differs from public manifest %d", facts.DefaultMinTransferRao, cfg.Public.Chain.ExpectedDefaultMinTransferRao)
	}
	minimumAlphaTransfer, err := minimumAlphaTransferRao(facts.DefaultMinTransferRao, facts.AlphaPriceQ9, p.AlphaTransferMarginBPS)
	if err != nil {
		return nil, err
	}
	operatorTransfers := make([]uint64, operatorCount)
	for index, requirement := range operatorCampaign {
		reserveCalls := uint64(cfg.Config.Scenarios.ShortEpochs) + productionDepositEpochs
		if index == 0 {
			reserveCalls++ // voluntary conviction
		}
		reserveRoundingAllowance, multiplyOK := checkedMul(reserveCalls, reserveRoundingAllowancePerCallRao)
		if !multiplyOK {
			return nil, fmt.Errorf("operator %d reserve rounding allowance overflow", index+1)
		}
		withRoundingAllowance, addOK := checkedAdd(requirement, reserveRoundingAllowance)
		if !addOK {
			return nil, fmt.Errorf("operator %d reserve rounding allowance overflow", index+1)
		}
		withRoundingAllowance, addOK = checkedAdd(withRoundingAllowance, alphaTransferDestinationRoundingAllowance)
		if !addOK {
			return nil, fmt.Errorf("operator %d bootstrap transfer rounding allowance overflow", index+1)
		}
		operatorTransfers[index] = max64(withRoundingAllowance, minimumAlphaTransfer)
	}
	validatorTargets := make([]uint64, validatorCount)
	reserveTransfer, reserveFinal, err := reserveValidatorTransferRao(facts.RegisteredAlphaRao, facts.ReserveValidatorAlphaRao, minimumAlphaTransfer, cfg.Config.ValidatorBootstrap.ReserveTargetShareBPS)
	if err != nil {
		return nil, fmt.Errorf("reserve-validator bootstrap: %w", err)
	}
	validatorTargets[0] = reserveTransfer
	independentMinimum := max64(cfg.Config.ValidatorBootstrap.IndependentTargetAlphaRao, minimumAlphaTransfer)
	validatorTargets[1], ok = checkedAdd(independentMinimum, alphaTransferDestinationRoundingAllowance)
	if !ok {
		return nil, errors.New("independent-validator rounding allowance overflow")
	}
	if !alphaShareMeets(facts.RegisteredAlphaRao, reserveFinal, cfg.Config.ValidatorBootstrap.ReserveMinimumShareBPS) {
		return nil, fmt.Errorf("planned reserve stake %d does not meet %d bps of registered alpha %d", reserveFinal, cfg.Config.ValidatorBootstrap.ReserveMinimumShareBPS, facts.RegisteredAlphaRao)
	}
	alphaPlanned := uint64(0)
	for _, amount := range append(append([]uint64(nil), operatorTransfers...), validatorTargets...) {
		alphaPlanned, ok = checkedAdd(alphaPlanned, amount)
		if !ok {
			return nil, errors.New("release alpha requirement overflows uint64")
		}
	}
	withRemainder, ok := checkedAdd(alphaPlanned, cfg.Config.ValidatorBootstrap.MinimumSourceRemainingAlphaRao)
	if !ok || alphaPlanned > cfg.MaximumAlphaRao {
		return nil, fmt.Errorf("release alpha requirement %d exceeds configured limit %d", alphaPlanned, cfg.MaximumAlphaRao)
	}
	if facts.AlphaAvailableRao < withRemainder {
		return nil, fmt.Errorf("wallet alpha source %s has %d rao; release setup requires %d plus a %d-rao retained source position", facts.AlphaSourceHotkey, facts.AlphaAvailableRao, alphaPlanned, cfg.Config.ValidatorBootstrap.MinimumSourceRemainingAlphaRao)
	}
	if facts.AlphaTransferableRao < alphaPlanned {
		return nil, fmt.Errorf("wallet alpha source %s has %d transferable rao after lock/collateral restrictions; release setup requires %d", facts.AlphaSourceHotkey, facts.AlphaTransferableRao, alphaPlanned)
	}

	// Gas-report-derived unit ceilings prevent the many low-cost fleet calls
	// from diluting the fixed deployment caps. Multiplying by one reviewed fee
	// ceiling makes both dimensions explicit and leaves the exact remainder for
	// the live campaign.
	gasUnitLimits := setupEVMGasUnitLimits(cfg)
	gasCaps := map[string]DecimalUint{}
	allocatedSetupGas := decimalUint64(0)
	gasIDs := make([]string, 0, len(gasUnitLimits))
	for id := range gasUnitLimits {
		gasIDs = append(gasIDs, id)
	}
	sort.Strings(gasIDs)
	for _, id := range gasIDs {
		gasCaps[id] = multiplyUint64Decimal(gasUnitLimits[id], cfg.Config.Budgets.MaximumEVMFeePerGasWei)
		var addErr error
		allocatedSetupGas, addErr = addDecimalUint(allocatedSetupGas, gasCaps[id])
		if addErr != nil {
			return nil, fmt.Errorf("EVM setup gas total: %w", addErr)
		}
	}
	setupComparison, err := allocatedSetupGas.Cmp(cfg.MaximumEVMGasWei)
	if err != nil || allocatedSetupGas.IsZero() || setupComparison >= 0 {
		return nil, stateMismatchError(err, "EVM gas ceiling %s does not cover setup %s plus a live campaign reserve", cfg.MaximumEVMGasWei, allocatedSetupGas)
	}
	runtimeGas, err := subtractDecimalUint(cfg.MaximumEVMGasWei, allocatedSetupGas)
	if err != nil {
		return nil, fmt.Errorf("EVM runtime gas reserve: %w", err)
	}
	voluntaryGas, err := divideDecimalUint(runtimeGas, 20)
	if err != nil {
		return nil, err
	}
	productionGas, err := divideDecimalUint(runtimeGas, 20)
	if err != nil {
		return nil, err
	}
	retirementGas, err := divideDecimalUint(runtimeGas, 20)
	if err != nil {
		return nil, err
	}
	governanceGas, err := divideDecimalUint(runtimeGas, 10)
	if err != nil {
		return nil, err
	}
	precompileGas, err := multiplyDivideDecimalUint(runtimeGas, 1, 8)
	if err != nil {
		return nil, err
	}
	dishonestDepositGas, err := divideDecimalUint(runtimeGas, 40)
	if err != nil {
		return nil, err
	}
	minimumRetirement := decimalUint64(uint64(operatorCount))
	retirementComparison, comparisonErr := retirementGas.Cmp(minimumRetirement)
	if comparisonErr != nil || voluntaryGas.IsZero() || productionGas.IsZero() || retirementComparison < 0 || governanceGas.IsZero() || precompileGas.IsZero() || dishonestDepositGas.IsZero() {
		return nil, fmt.Errorf("EVM runtime gas ceiling is too small for conviction, production transition, and retirement")
	}
	campaignGas, err := subtractDecimalUints(runtimeGas, voluntaryGas, productionGas, retirementGas, governanceGas, precompileGas, dishonestDepositGas)
	if err != nil || campaignGas.IsZero() {
		return nil, stateMismatchError(err, "live campaign gas reserve is zero")
	}

	// Fund every signer for its exact explicit action ceilings first. Only the
	// live campaign reserve is weighted across deposit, root, claim, and keeper
	// roles, so many fleet calls cannot dilute a fixed deployment allowance.
	type gasRole struct {
		label, address string
		campaignWeight uint64
		burns          uint64
	}
	gasRoles := []gasRole{
		{label: "deployer", address: roles.Deployer, burns: 1},
		{label: "owner", address: roles.Owner, burns: uint64(operatorCount)},
		{label: "guardian", address: roles.Guardian},
		{label: "commitment-oracle", address: roles.CommitmentOracle},
		{label: "keeper", address: roles.Keeper, campaignWeight: 4},
	}
	for i := 0; i < operatorCount; i++ {
		gasRoles = append(gasRoles,
			gasRole{label: fmt.Sprintf("operator-%d-deposit", i+1), address: roles.OperatorDepositSigners[i], campaignWeight: 1, burns: 1},
			gasRole{label: fmt.Sprintf("operator-%d-root", i+1), address: roles.OperatorRootSigners[i], campaignWeight: 1},
			gasRole{label: fmt.Sprintf("operator-%d-claim-relayer", i+1), address: roles.ClaimRelayers[i], campaignWeight: 1},
		)
	}
	roleGas := map[string]DecimalUint{}
	for _, role := range gasRoles {
		roleGas[role.label] = decimalUint64(0)
	}
	addRoleGas := func(label string, amount DecimalUint) error {
		if _, ok := roleGas[label]; !ok {
			return fmt.Errorf("unknown EVM gas role %s", label)
		}
		updated, addErr := addDecimalUint(roleGas[label], amount)
		if addErr != nil {
			return fmt.Errorf("%s EVM gas allocation: %w", label, addErr)
		}
		roleGas[label] = updated
		return nil
	}
	for _, id := range []string{
		"evm.reserve-sink", "evm.settlement-vault", "evm.coordinator-implementation", "evm.vault-register-escrow",
		"evm.coordinator-proxy", "evm.governance-drill-implementation", "evm.vault-fix-coordinator", "evm.sink-fix-recorder",
		"precompile.probe-deploy", "evm.coordinator-upgrade-implementation", "fleet.refresh.deploy-batcher",
	} {
		if err := addRoleGas("deployer", gasCaps[id]); err != nil {
			return nil, err
		}
	}
	if err := addRoleGas("deployer", precompileGas); err != nil {
		return nil, err
	}
	if err := addRoleGas("owner", productionGas); err != nil {
		return nil, err
	}
	for _, id := range []string{"evm.coordinator-upgrade-activate", "policy.schedule-bootstrap", "fleet.refresh.oracle-activate", "fleet.refresh.oracle-restore"} {
		if err := addRoleGas("owner", gasCaps[id]); err != nil {
			return nil, err
		}
	}
	if err := addRoleGas("owner", retirementGas); err != nil {
		return nil, err
	}
	ownerGovernanceGas, err := multiplyDivideDecimalUint(governanceGas, 4, 5)
	if err != nil {
		return nil, err
	}
	guardianGovernanceGas, err := subtractDecimalUint(governanceGas, ownerGovernanceGas)
	if err != nil {
		return nil, err
	}
	if err := addRoleGas("owner", ownerGovernanceGas); err != nil {
		return nil, err
	}
	if err := addRoleGas("guardian", guardianGovernanceGas); err != nil {
		return nil, err
	}
	for operator := 1; operator <= operatorCount; operator++ {
		if err := addRoleGas("owner", gasCaps[fmt.Sprintf("operator.register.%d", operator)]); err != nil {
			return nil, err
		}
		if err := addRoleGas(fmt.Sprintf("operator-%d-deposit", operator), gasCaps[fmt.Sprintf("operator.deposit.register.%d", operator)]); err != nil {
			return nil, err
		}
	}
	if err := addRoleGas("operator-1-deposit", voluntaryGas); err != nil {
		return nil, err
	}
	if err := addRoleGas("operator-2-deposit", dishonestDepositGas); err != nil {
		return nil, err
	}
	for fleet := cfg.Config.Topology.HeadFleets + 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		if err := addRoleGas("commitment-oracle", gasCaps[fmt.Sprintf("fleet.mirror.%d", fleet)]); err != nil {
			return nil, err
		}
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			if err := addRoleGas("keeper", gasCaps[fmt.Sprintf("fleet.bind.%d.%d", fleet, member)]); err != nil {
				return nil, err
			}
		}
	}
	for batch := 1; batch <= (cfg.Config.Topology.HeadFleets+fleetRefreshBatchSize-1)/fleetRefreshBatchSize; batch++ {
		if err := addRoleGas("commitment-oracle", gasCaps[fmt.Sprintf("fleet.install.batch.%d", batch)]); err != nil {
			return nil, err
		}
		if err := addRoleGas("commitment-oracle", gasCaps[fmt.Sprintf("fleet.refresh.batch.%d", batch)]); err != nil {
			return nil, err
		}
	}
	var campaignWeight uint64
	remainingWeightedRoles := 0
	for _, role := range gasRoles {
		campaignWeight += role.campaignWeight
		if role.campaignWeight > 0 {
			remainingWeightedRoles++
		}
	}
	if campaignWeight == 0 {
		return nil, errors.New("EVM campaign gas has no funded role")
	}
	allocatedCampaignGas := decimalUint64(0)
	for _, role := range gasRoles {
		if role.campaignWeight == 0 {
			continue
		}
		share, shareErr := multiplyDivideDecimalUint(campaignGas, role.campaignWeight, campaignWeight)
		remainingWeightedRoles--
		if remainingWeightedRoles == 0 {
			share, shareErr = subtractDecimalUint(campaignGas, allocatedCampaignGas)
		}
		if shareErr != nil {
			return nil, fmt.Errorf("%s campaign gas share: %w", role.label, shareErr)
		}
		allocatedCampaignGas, shareErr = addDecimalUint(allocatedCampaignGas, share)
		if shareErr != nil {
			return nil, shareErr
		}
		if err := addRoleGas(role.label, share); err != nil {
			return nil, err
		}
	}
	fundedGas := decimalUint64(0)
	for _, role := range gasRoles {
		gas := roleGas[role.label]
		var gasErr error
		fundedGas, gasErr = addDecimalUint(fundedGas, gas)
		if gasErr != nil {
			return nil, fmt.Errorf("%s cumulative gas funding: %w", role.label, gasErr)
		}
		gasRao, gasErr := ceilDivideDecimalUintToUint64(gas, 1_000_000_000)
		if gasErr != nil {
			return nil, fmt.Errorf("%s gas funding rao: %w", role.label, gasErr)
		}
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
			Description: "fund the scoped EVM role with its exact explicit and campaign gas allowance plus registration value and the runtime existential deposit",
			Parameters: map[string]string{
				"usable_evm_rao":          strconv.FormatUint(usableTAORao, 10),
				"existential_deposit_rao": strconv.FormatUint(facts.ExistentialDepositRao, 10),
			},
			Spend: Spend{TAORao: maximumTransferRao}, DependsOn: []string{"subnet.verify-owner"},
		})
	}
	if fundedComparison, fundedErr := fundedGas.Cmp(cfg.MaximumEVMGasWei); fundedErr != nil || fundedComparison != 0 {
		return nil, stateMismatchError(fundedErr, "EVM role gas funding %s does not equal campaign ceiling %s", fundedGas, cfg.MaximumEVMGasWei)
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
	// v452 breaks equal-emission prune ties by registration block and UID, even
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
			action.Parameters["registration_role_generation"] = strconv.FormatUint(generation, 10)
			if generation > 0 {
				churn, churnErr := churnIndexForContractRegistration(cfg.Config.Topology, generation, 1)
				if churnErr != nil {
					return nil, churnErr
				}
				action.Parameters["expected_replaced_churn"] = strconv.Itoa(churn)
				action.DependsOn = append(action.DependsOn, fmt.Sprintf("churn.register.%d", churn))
			}
		}
		add(action)
		prev = id
	}
	add(Action{ID: "precompile.probe-deploy", Kind: "evm-transaction", Target: "disposable-precompile-probe", Description: "deploy the locked, owner-gated runtime-452 conformance probe at its precomputed nonce", Spend: Spend{EVMGasWei: gasCaps["precompile.probe-deploy"]}, DependsOn: []string{prev}})
	add(Action{ID: "evm.coordinator-upgrade-implementation", Kind: "evm-transaction", Target: payloads.CoordinatorUpgrade.Implementation.Hex(), Description: "deploy the exact release-1.0 coordinator implementation at the approval-bound deployer nonce", Parameters: map[string]string{"runtime_code_hash": payloads.CoordinatorUpgrade.RuntimeCodeHash}, Spend: Spend{EVMGasWei: gasCaps["evm.coordinator-upgrade-implementation"]}, DependsOn: []string{"precompile.probe-deploy"}})
	add(Action{ID: "evm.coordinator-upgrade-activate", Kind: "evm-transaction", Target: payloads.Manifest.CoordinatorProxy.Hex(), Description: "atomically activate the reviewed coordinator repair through its UUPS owner gate", Parameters: map[string]string{"implementation": payloads.CoordinatorUpgrade.Implementation.Hex(), "runtime_code_hash": payloads.CoordinatorUpgrade.RuntimeCodeHash}, Spend: Spend{EVMGasWei: gasCaps["evm.coordinator-upgrade-activate"]}, DependsOn: []string{"evm.coordinator-upgrade-implementation", "evm.fund-owner"}})
	add(Action{ID: "policy.schedule-bootstrap", Kind: "evm-transaction", Target: payloads.Manifest.CoordinatorProxy.Hex(), Description: "schedule the locked accelerated test policy for the next epoch when the live proxy still carries an earlier release policy", Parameters: map[string]string{"policy_hash": cfg.PolicyHash, "epoch_cap_rao_per_operator": fmt.Sprint(cfg.Policy.Deposit.EpochCapRaoPerOperator), "campaign_cap_rao": fmt.Sprint(cfg.Policy.Deposit.TotalTestCampaignCapRao)}, Spend: Spend{EVMGasWei: gasCaps["policy.schedule-bootstrap"]}, DependsOn: []string{"evm.coordinator-upgrade-activate", "evm.fund-owner"}})
	add(Action{ID: "policy.await-bootstrap", Kind: "evm-read", Target: payloads.Manifest.CoordinatorProxy.Hex(), Description: "wait until the locked accelerated policy is active before rendering or launching any workload", Parameters: map[string]string{"policy_hash": cfg.PolicyHash}, DependsOn: []string{"policy.schedule-bootstrap"}})
	setupDeps := []string{prev, "policy.await-bootstrap"}
	for i := 0; i < operatorCount; i++ {
		depositRegistration := fmt.Sprintf("operator.deposit.register.%d", i+1)
		add(Action{ID: depositRegistration, Kind: "evm-transaction", Target: roles.OperatorDepositSigners[i], Description: "limit-register the operator-isolated deposit hotkey under its EVM mirror coldkey", Parameters: registrationParameters(), Spend: Spend{EVMGasWei: gasCaps[depositRegistration], Registrations: 1}, DependsOn: []string{fmt.Sprintf("evm.fund-operator-%d-deposit", i+1), lastChurn}})
		id := fmt.Sprintf("operator.register.%d", i+1)
		operatorParameters := registrationParameters()
		operatorParameters["registration_role_generation"] = strconv.FormatUint(generation, 10)
		operatorDependencies := []string{prev, "evm.fund-owner", depositRegistration}
		if generation > 0 {
			churn, churnErr := churnIndexForContractRegistration(cfg.Config.Topology, generation, i+2)
			if churnErr != nil {
				return nil, churnErr
			}
			operatorParameters["expected_replaced_churn"] = strconv.Itoa(churn)
			operatorDependencies = append(operatorDependencies, fmt.Sprintf("churn.register.%d", churn))
		}
		add(Action{ID: id, Kind: "evm-transaction", Target: fmt.Sprintf("no:%d", i+1), Description: "limit-register immutable generation-scoped pool hotkey and grant distinct deposit/root roles", Parameters: operatorParameters, Spend: Spend{EVMGasWei: gasCaps[id], Registrations: 1}, DependsOn: operatorDependencies})
		alphaID := fmt.Sprintf("alpha.transfer.operator-deposit.%d", i+1)
		amount := operatorTransfers[i]
		parameters := alphaTransferActionParameters(amount, operatorCampaign[i], minimumAlphaTransfer, facts, p.AlphaTransferMarginBPS)
		parameters["campaign_policy_hash"] = cfg.PolicyHash
		reserveCalls := uint64(cfg.Config.Scenarios.ShortEpochs) + productionDepositEpochs
		if i == 0 {
			reserveCalls++
		}
		parameters["reserve_calls"] = strconv.FormatUint(reserveCalls, 10)
		parameters["reserve_rounding_allowance_per_call_rao"] = strconv.FormatUint(reserveRoundingAllowancePerCallRao, 10)
		add(Action{ID: alphaID, Kind: "substrate-extrinsic", Target: roles.OperatorDepositSigners[i], Description: "transfer an exact approved alpha amount into the coordinator-owned isolated deposit position", Parameters: parameters, Spend: Spend{AlphaRao: amount}, DependsOn: []string{id}})
		setupDeps = append(setupDeps, alphaID)
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		fundID := fmt.Sprintf("fleet.fund.%d", fleet)
		registerID := fmt.Sprintf("fleet.register.%d", fleet)
		fundHotkeyID := fmt.Sprintf("fleet.fund-hotkey.%d", fleet)
		add(Action{ID: fundID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet-coldkey:%d", fleet), Description: "fund the independently keyed provider fleet coldkey for one burn, bounded fees, and runtime keep-alive balance", Parameters: registrationFundingParameters(), Spend: Spend{TAORao: roleFunding}, DependsOn: []string{"subnet.verify-owner", lastChurn}})
		add(Action{ID: registerID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet:%d", fleet), Description: "limit-register an independently keyed provider-owned head fleet hotkey", Parameters: registrationParameters(), Spend: Spend{Registrations: 1}, DependsOn: []string{fundID}})
		commitmentFees, ok := checkedMul(nativeFeeLimit, 2)
		if !ok {
			return nil, fmt.Errorf("head fleet commitment fee reserve overflow")
		}
		if fleet == 1 {
			// Both generation publishes plus the M0B replace/restore pair.
			commitmentFees, ok = checkedMul(nativeFeeLimit, 4)
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
		alphaDescription := "transfer an exact approved alpha amount into the independent validator position"
		if i == 0 {
			alphaDescription = "transfer an exact approved alpha amount which gives the reserve validator a bootstrap majority"
		}
		parameters := alphaTransferActionParameters(validatorTargets[i], 0, minimumAlphaTransfer, facts, p.AlphaTransferMarginBPS)
		parameters["planned_existing_stake_rao"] = strconv.FormatUint([]uint64{facts.ReserveValidatorAlphaRao, facts.IndependentValidatorAlphaRao}[i], 10)
		if i == 0 {
			parameters["planned_final_stake_rao"] = strconv.FormatUint(reserveFinal, 10)
			parameters["registered_alpha_snapshot_rao"] = strconv.FormatUint(facts.RegisteredAlphaRao, 10)
			parameters["reserve_target_share_bps"] = strconv.FormatUint(uint64(cfg.Config.ValidatorBootstrap.ReserveTargetShareBPS), 10)
			parameters["reserve_minimum_share_bps"] = strconv.FormatUint(uint64(cfg.Config.ValidatorBootstrap.ReserveMinimumShareBPS), 10)
		}
		add(Action{ID: alphaID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("validator:%d", i+1), Description: alphaDescription, Parameters: parameters, Spend: Spend{AlphaRao: validatorTargets[i]}, DependsOn: []string{stakeDependency}})
		setupDeps = append(setupDeps, alphaID)
	}
	add(Action{ID: "validator.reserve-majority", Kind: "substrate-read", Target: "reserve-validator:1", Description: "verify the reserve validator retains the configured majority of all registered subnet alpha", Parameters: map[string]string{"minimum_share_bps": strconv.FormatUint(uint64(cfg.Config.ValidatorBootstrap.ReserveMinimumShareBPS), 10)}, DependsOn: []string{"alpha.transfer.validator.1", "alpha.transfer.validator.2"}})
	setupDeps = append(setupDeps, "validator.reserve-majority")
	// Fees paid directly by the source wallet are explicit and bounded rather
	// than hidden in transferred role balances.
	var nativeWrites uint64
	for _, planned := range p.Actions {
		if planned.Kind == "substrate-extrinsic" {
			nativeWrites++
		}
	}
	// Two production hyperparameter transitions, both head-fleet commitment
	// generations, every challenger registration/commitment pair, and the M0B
	// replace/restore pair are added after this reservation is constructed.
	nativeWrites += uint64(2*cfg.Config.Topology.HeadFleets + 2*cfg.Config.Topology.ChallengerFleets + 4)
	feeReserve, ok := checkedMul(nativeWrites, nativeFeeLimit)
	if !ok {
		return nil, fmt.Errorf("native fee reserve overflow")
	}
	add(Action{ID: "wallet.native-fee-reserve", Kind: "budget-reserve", Target: cfg.WalletPublic, Description: "reserve the approved fee ceiling for every planned native extrinsic", Parameters: map[string]string{"maximum_fee_rao": fmt.Sprint(nativeFeeLimit), "native_writes": fmt.Sprint(nativeWrites)}, Spend: Spend{TAORao: feeReserve}, DependsOn: []string{"subnet.verify-owner"}})
	setupDeps = append(setupDeps, "wallet.native-fee-reserve")
	probeWeights := map[string]uint64{
		"precompile.seed":         2,
		"precompile.move-forward": 1, "precompile.move-back": 1,
		"precompile.snapshot": 1, "precompile.transfer-out": 2,
	}
	probeGasCaps := map[string]DecimalUint{}
	allocatedProbeGas := decimalUint64(0)
	var probeWeightTotal uint64
	for _, weight := range probeWeights {
		probeWeightTotal += weight
	}
	probeIDs := make([]string, 0, len(probeWeights))
	for id := range probeWeights {
		probeIDs = append(probeIDs, id)
	}
	sort.Strings(probeIDs)
	for i, id := range probeIDs {
		cap, capErr := multiplyDivideDecimalUint(precompileGas, probeWeights[id], probeWeightTotal)
		if capErr != nil {
			return nil, fmt.Errorf("precompile gas cap for %s: %w", id, capErr)
		}
		if i == len(probeIDs)-1 {
			cap, capErr = subtractDecimalUint(precompileGas, allocatedProbeGas)
			if capErr != nil {
				return nil, fmt.Errorf("precompile final gas remainder: %w", capErr)
			}
		}
		probeGasCaps[id] = cap
		allocatedProbeGas, capErr = addDecimalUint(allocatedProbeGas, cap)
		if capErr != nil {
			return nil, fmt.Errorf("precompile cumulative gas: %w", capErr)
		}
	}
	add(Action{ID: "campaign.evm-gas-reserve", Kind: "budget-reserve", Target: cfg.Config.Deployment.DeploymentID, Description: "reserve gas for deposits, payout roots, keepers, and claims during the live campaign", Spend: Spend{EVMGasWei: campaignGas}, DependsOn: setupDeps})
	setupDeps = append(setupDeps, "campaign.evm-gas-reserve")
	add(Action{ID: "config.render", Kind: "local", Target: cfg.Config.Deployment.DeploymentID, Description: "atomically render isolated operator, miner, validator, and supervisor configs", Parameters: map[string]string{"operator_config_overlay": operatorConfigOverlayVersion}, DependsOn: setupDeps})
	add(Action{ID: "accounts.provision", Kind: "local", Target: cfg.Config.Deployment.DeploymentID, Description: "provision stable operator-scoped miner and validator identities", DependsOn: []string{"config.render"}})
	add(Action{ID: "campaign.voluntary-conviction.1", Kind: "evm-transaction", Target: "no:1", Description: "lock the exact first-tier boundary as voluntary conviction without recording current-epoch demand", Parameters: map[string]string{"no_id": "1", "amount_rao": fmt.Sprint(cfg.Config.Scenarios.VoluntaryConvictionRao), "reserve_runtime_share_transitions": strconv.FormatUint(reserveRuntimeShareTransitionCount, 10), "reserve_rounding_allowance_rao": strconv.FormatUint(reserveRoundingAllowancePerCallRao, 10)}, Spend: Spend{EVMGasWei: voluntaryGas}, DependsOn: []string{"accounts.provision", "campaign.evm-gas-reserve", "alpha.transfer.operator-deposit.1"}})
	batcherRuntimeHash := crypto.Keccak256Hash(payloads.FleetBatcherRuntime).Hex()
	add(Action{
		ID: "fleet.refresh.deploy-batcher", Kind: "evm-transaction", Target: payloads.FleetBatcherAddress.Hex(),
		Description: "deploy the bounded testnet-only fleet install and refresh helper at the approval-bound nonce",
		Parameters: map[string]string{
			"coordinator":       payloads.Manifest.CoordinatorProxy.Hex(),
			"commitment_oracle": payloads.CommitmentOracle.Hex(),
			"runtime_code_hash": batcherRuntimeHash,
		},
		Spend: Spend{EVMGasWei: gasCaps["fleet.refresh.deploy-batcher"]}, DependsOn: []string{"campaign.voluntary-conviction.1", "evm.fund-deployer"},
	})
	add(Action{
		ID: "fleet.refresh.oracle-activate", Kind: "evm-transaction", Target: payloads.Manifest.CoordinatorProxy.Hex(),
		Description: "schedule the bounded fleet refresh helper as commitment oracle for the next safe epoch",
		Parameters:  map[string]string{"oracle": payloads.FleetBatcherAddress.Hex()},
		Spend:       Spend{EVMGasWei: gasCaps["fleet.refresh.oracle-activate"]}, DependsOn: []string{"fleet.refresh.deploy-batcher", "evm.fund-owner"},
	})
	add(Action{ID: "fleet.refresh.oracle-await-active", Kind: "evm-read", Target: payloads.FleetBatcherAddress.Hex(), Description: "wait until the approved fleet refresh helper is the active commitment oracle", DependsOn: []string{"fleet.refresh.oracle-activate"}})
	lastFleet := "fleet.refresh.oracle-await-active"
	for batch := 1; batch <= (cfg.Config.Topology.HeadFleets+fleetRefreshBatchSize-1)/fleetRefreshBatchSize; batch++ {
		first := (batch-1)*fleetRefreshBatchSize + 1
		last := first + fleetRefreshBatchSize - 1
		if last > cfg.Config.Topology.HeadFleets {
			last = cfg.Config.Topology.HeadFleets
		}
		batchBarrier := lastFleet
		commitmentIDs := make([]string, 0, last-first+1)
		for fleet := first; fleet <= last; fleet++ {
			commitID := fmt.Sprintf("fleet.commitment.%d", fleet)
			add(Action{ID: commitID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet:%d", fleet), Description: "publish the canonical multi-client fleet manifest hash from its registered hotkey and verify finalized storage", Parameters: map[string]string{fleetCommitmentStorageParameter: fleetCommitmentStorageV2, fleetCommitmentParallelGroupParameter: fmt.Sprintf("install-%d", batch)}, DependsOn: []string{batchBarrier, fmt.Sprintf("fleet.fund-hotkey.%d", fleet)}})
			commitmentIDs = append(commitmentIDs, commitID)
		}
		installID := fmt.Sprintf("fleet.install.batch.%d", batch)
		installDependencies := append(append([]string(nil), commitmentIDs...), "evm.fund-commitment-oracle")
		add(Action{
			ID: installID, Kind: "evm-transaction", Target: payloads.FleetBatcherAddress.Hex(),
			Description: "atomically mirror finalized generation-1 commitments and install every dual-signed fleet member",
			Parameters:  map[string]string{"first_fleet": strconv.Itoa(first), "last_fleet": strconv.Itoa(last), "generation": "1"},
			Spend:       Spend{EVMGasWei: gasCaps[installID]}, DependsOn: installDependencies,
		})
		lastFleet = installID
		for fleet := first; fleet <= last; fleet++ {
			mirrorID := fmt.Sprintf("fleet.mirror.%d", fleet)
			add(Action{
				ID: mirrorID, Kind: "evm-read", Target: fmt.Sprintf("head-fleet:%d", fleet),
				Description: "verify the atomic installer mirrored the exact finalized native commitment",
				Parameters:  map[string]string{fleetCommitmentStorageParameter: fleetCommitmentStorageV2, "batch_installed": "true"},
				DependsOn:   []string{lastFleet},
			})
			lastFleet = mirrorID
			for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
				id := fmt.Sprintf("fleet.bind.%d.%d", fleet, member)
				miner := fleetMemberMinerIndex(cfg, fleet, member)
				add(Action{
					ID: id, Kind: "evm-read", Target: fmt.Sprintf("miner:%d", miner),
					Description: "verify the atomic installer recorded one exact dual-signed generation-1 member",
					Parameters:  map[string]string{"batch_installed": "true"}, DependsOn: []string{lastFleet},
				})
				lastFleet = id
			}
		}
	}
	lastRefresh := lastFleet
	for batch := 1; batch <= (cfg.Config.Topology.HeadFleets+fleetRefreshBatchSize-1)/fleetRefreshBatchSize; batch++ {
		first := (batch-1)*fleetRefreshBatchSize + 1
		last := first + fleetRefreshBatchSize - 1
		if last > cfg.Config.Topology.HeadFleets {
			last = cfg.Config.Topology.HeadFleets
		}
		batchBarrier := lastRefresh
		commitmentIDs := make([]string, 0, last-first+1)
		for fleet := first; fleet <= last; fleet++ {
			commitID := fmt.Sprintf("fleet.refresh.commitment.%d", fleet)
			add(Action{
				ID: commitID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet:%d", fleet),
				Description: "publish the generation-2 fleet manifest immediately before its bounded replacement batch",
				Parameters:  map[string]string{fleetCommitmentStorageParameter: fleetCommitmentStorageV2, "generation": "2", fleetCommitmentParallelGroupParameter: fmt.Sprintf("refresh-%d", batch)},
				DependsOn:   []string{batchBarrier, fmt.Sprintf("fleet.fund-hotkey.%d", fleet)},
			})
			commitmentIDs = append(commitmentIDs, commitID)
		}
		batchID := fmt.Sprintf("fleet.refresh.batch.%d", batch)
		batchDependencies := append(append([]string(nil), commitmentIDs...), "evm.fund-commitment-oracle")
		add(Action{
			ID: batchID, Kind: "evm-transaction", Target: payloads.FleetBatcherAddress.Hex(),
			Description: "atomically mirror finalized generation-2 commitments, client-revoke generation 1, and install dual-signed replacements",
			Parameters:  map[string]string{"first_fleet": strconv.Itoa(first), "last_fleet": strconv.Itoa(last), "generation": "2"},
			Spend:       Spend{EVMGasWei: gasCaps[batchID]}, DependsOn: batchDependencies,
		})
		lastRefresh = batchID
	}
	add(Action{
		ID: "fleet.refresh.oracle-restore", Kind: "evm-transaction", Target: payloads.Manifest.CoordinatorProxy.Hex(),
		Description: "restore the original commitment oracle for the next safe epoch after every fleet replacement verifies",
		Parameters:  map[string]string{"oracle": payloads.CommitmentOracle.Hex()},
		Spend:       Spend{EVMGasWei: gasCaps["fleet.refresh.oracle-restore"]}, DependsOn: []string{lastRefresh, "evm.fund-owner"},
	})
	add(Action{ID: "fleet.refresh.oracle-await-restored", Kind: "evm-read", Target: payloads.CommitmentOracle.Hex(), Description: "wait until the immutable original commitment oracle is active before topology launch", DependsOn: []string{"fleet.refresh.oracle-restore"}})
	add(Action{ID: "topology.launch", Kind: "local", Target: cfg.Config.Deployment.DeploymentID, Description: "start dependencies, two operators, miners, and two validators with readiness gates", DependsOn: []string{"fleet.refresh.oracle-await-restored"}})
	lastChallenger := "topology.launch"
	for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
		fleet := cfg.Config.Topology.HeadFleets + challenger
		churn, churnErr := churnIndexForChallenger(cfg.Config.Topology, generation, challenger)
		if churnErr != nil {
			return nil, churnErr
		}
		registerID := fmt.Sprintf("fleet.register.%d", fleet)
		commitID := fmt.Sprintf("fleet.commitment.%d", fleet)
		mirrorID := fmt.Sprintf("fleet.mirror.%d", fleet)
		challengerParameters := registrationParameters()
		challengerParameters["expected_replaced_churn"] = strconv.Itoa(churn)
		add(Action{ID: registerID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("challenger-fleet:%d", fleet), Description: fmt.Sprintf("register the measured challenger into the full subnet and prove it replaces oldest remaining churn-floor identity %d", churn), Parameters: challengerParameters, Spend: Spend{Registrations: 1}, DependsOn: []string{lastChallenger, fmt.Sprintf("fleet.fund.%d", fleet), fmt.Sprintf("churn.register.%d", churn)}})
		add(Action{ID: commitID, Kind: "substrate-extrinsic", Target: fmt.Sprintf("challenger-fleet:%d", fleet), Description: "publish the challenger fleet manifest commitment and verify finalized storage", Parameters: map[string]string{fleetCommitmentStorageParameter: fleetCommitmentStorageV2}, DependsOn: []string{registerID, fmt.Sprintf("fleet.fund-hotkey.%d", fleet)}})
		add(Action{ID: mirrorID, Kind: "evm-transaction", Target: fmt.Sprintf("challenger-fleet:%d", fleet), Description: "mirror the challenger commitment after its native registration finalizes", Parameters: map[string]string{fleetCommitmentStorageParameter: fleetCommitmentStorageV2}, Spend: Spend{EVMGasWei: gasCaps[mirrorID]}, DependsOn: []string{commitID, "evm.fund-commitment-oracle"}})
		lastChallenger = mirrorID
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			bindID := fmt.Sprintf("fleet.bind.%d.%d", fleet, member)
			miner := fleetMemberMinerIndex(cfg, fleet, member)
			add(Action{ID: bindID, Kind: "evm-transaction", Target: fmt.Sprintf("miner:%d", miner), Description: "relay one challenger member binding effective next epoch", Spend: Spend{EVMGasWei: gasCaps[bindID]}, DependsOn: []string{lastChallenger, "evm.fund-keeper"}})
			lastChallenger = bindID
		}
	}
	add(Action{ID: "churn.tournament-complete", Kind: "local", Target: fmt.Sprintf("netuid:%d", cfg.Netuid), Description: "prove both live challengers replaced exactly the two oldest eligible remaining churn-floor UIDs while all 202 measured fleets remain registered", DependsOn: []string{lastChallenger}})
	add(Action{ID: "precompile.commitment-write", Kind: "substrate-extrinsic", Target: "head-fleet:1", Description: "replace the exact generation-2 fleet commitment with a finalized one-field SHA-256 conformance commitment", Parameters: map[string]string{fleetCommitmentStorageParameter: fleetCommitmentStorageV2, "canonical_generation": strconv.FormatUint(precompileCanonicalFleetGeneration, 10)}, DependsOn: []string{"churn.tournament-complete"}})
	add(Action{ID: "precompile.commitment-restore", Kind: "substrate-extrinsic", Target: "head-fleet:1", Description: "restore the exact generation-2 fleet hash and prove the restored finalized bytes", Parameters: map[string]string{fleetCommitmentStorageParameter: fleetCommitmentStorageV2, "canonical_generation": strconv.FormatUint(precompileCanonicalFleetGeneration, 10)}, DependsOn: []string{"precompile.commitment-write"}})
	add(Action{ID: "precompile.read-battery", Kind: "evm-read", Target: "runtime-452-precompiles", Description: "prove Blake2, Ed25519, sr25519, metagraph, neuron, staking, live UID, absent UID, mirror custody, and minimum stake at one finalized head", DependsOn: []string{"precompile.commitment-restore", "precompile.probe-deploy"}})
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
	allocatedGovernance := decimalUint64(0)
	for i, drill := range governanceActions {
		cap, capErr := multiplyDivideDecimalUint(governanceGas, drill.weight, 10)
		if capErr != nil {
			return nil, fmt.Errorf("governance gas cap: %w", capErr)
		}
		if i == len(governanceActions)-1 {
			cap, capErr = subtractDecimalUint(governanceGas, allocatedGovernance)
			if capErr != nil {
				return nil, fmt.Errorf("governance final gas remainder: %w", capErr)
			}
		}
		allocatedGovernance, capErr = addDecimalUint(allocatedGovernance, cap)
		if capErr != nil {
			return nil, fmt.Errorf("governance cumulative gas: %w", capErr)
		}
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
		Description: "post a deliberate 50-percent demand underpayment in the first fresh production-cadence epoch and prove live validators reject the signed-usage mismatch",
		Parameters:  map[string]string{"no_id": "2", "amount_rao": fmt.Sprint(cfg.Config.Scenarios.DishonestDepositRao), "target_epoch": "next_fresh_production_epoch", "reserve_runtime_share_transitions": strconv.FormatUint(reserveRuntimeShareTransitionCount, 10), "reserve_rounding_allowance_rao": strconv.FormatUint(reserveRoundingAllowancePerCallRao, 10)},
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
		var gasErr error
		maximum.EVMGasWei, gasErr = addDecimalUint(maximum.EVMGasWei, action.Spend.EVMGasWei)
		if gasErr != nil {
			return Spend{}, fmt.Errorf("gas plan: %w", gasErr)
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

// Runtime 452 bumps burn immediately after a registration and decays it on
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
	p.LiveFacts.EVMFinalizedBlock = 0
	p.LiveFacts.EVMFinalizedBlockHash = ""
	p.LiveFacts.AlphaAvailableRao = 0
	p.LiveFacts.AlphaTransferableRao = 0
	p.LiveFacts.AlphaSourceStoredLockRao = 0
	p.LiveFacts.AlphaSourceCollateralRao = 0
	p.LiveFacts.WalletNetuidAlphaRao = 0
	p.LiveFacts.WalletNetuidCollateralRao = 0
	p.LiveFacts.WalletFreeTAORao = 0
	p.CoordinatorUpgradeBaseline.FinalizedBlock = 0
	p.CoordinatorUpgradeBaseline.FinalizedBlockHash = ""
	if planUsesRegistrationEnvelope(p.Schema) {
		// Runtime 452 decays Burn on every block. V2 binds MinBurn, MaxBurn,
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
	if err := validateFleetCommitmentParallelGroups(p.Actions); err != nil {
		return err
	}
	total, err := addSpends(p.MaximumSpend, p.SupersededSpend)
	if err != nil {
		return fmt.Errorf("combine active and superseded plan spend: %w", err)
	}
	if total.TAORao > p.Limits.TAORao {
		return fmt.Errorf("TAO plan maximum %d exceeds limit %d", total.TAORao, p.Limits.TAORao)
	}
	if total.AlphaRao > p.Limits.AlphaRao {
		return fmt.Errorf("alpha plan maximum %d exceeds limit %d", total.AlphaRao, p.Limits.AlphaRao)
	}
	gasComparison, err := total.EVMGasWei.Cmp(p.Limits.EVMGasWei)
	if err != nil {
		return fmt.Errorf("compare gas plan maximum and limit: %w", err)
	}
	if gasComparison > 0 {
		return fmt.Errorf("gas plan maximum %s exceeds limit %s", total.EVMGasWei, p.Limits.EVMGasWei)
	}
	if total.Registrations > p.Limits.Registrations {
		return fmt.Errorf("registration plan %d exceeds limit %d", total.Registrations, p.Limits.Registrations)
	}
	if p.MaximumSpend.Registrations > 0 && p.RegistrationBurnLimitRao == 0 {
		return errors.New("registration plan has no per-registration burn limit")
	}
	if planUsesRegistrationEnvelope(p.Schema) && p.NativeTransactionFeeLimitRao == 0 {
		return errors.New("release plan has no per-transaction native fee limit")
	}
	if planUsesRegistrationEnvelope(p.Schema) && (p.BootstrapBurnHalfLifeBlocks != 1 || p.ProductionBurnHalfLifeBlocks == 0) {
		return errors.New("release plan has no bounded bootstrap/production burn half-life lifecycle")
	}
	if planUsesEVMFeeEnvelope(p.Schema) && p.MaximumEVMFeePerGasWei == 0 {
		return errors.New("release plan has no per-gas EVM fee limit")
	}
	if !supportedSetupPlanSchema(p.Schema) {
		return fmt.Errorf("unsupported setup plan schema %q", p.Schema)
	}
	hasVoluntaryRecovery := false
	for _, action := range p.Actions {
		hasVoluntaryRecovery = hasVoluntaryRecovery || action.ID == voluntaryConvictionReconciliationActionID
	}
	minimumAlphaTransfer := uint64(0)
	alphaSourceCapacity := uint64(0)
	if planUsesAlphaTransferEnvelope(p.Schema) {
		runtimeMinimum := p.LiveFacts.InitialMinStakeRao
		if planUsesDefaultMinTransferEnvelope(p.Schema) {
			runtimeMinimum = p.LiveFacts.DefaultMinTransferRao
		}
		if p.AlphaTransferMarginBPS == 0 || p.MinimumSourceRemainingRao == 0 || runtimeMinimum == 0 || p.LiveFacts.AlphaPriceQ9 == 0 || p.LiveFacts.RegisteredAlphaRao == 0 || !p.LiveFacts.AlphaSourceRegistered || p.LiveFacts.AlphaTransferableRao == 0 {
			return errors.New("v5 plan has incomplete alpha-transfer economics")
		}
		minimumAlphaTransfer, err = minimumAlphaTransferRao(runtimeMinimum, p.LiveFacts.AlphaPriceQ9, p.AlphaTransferMarginBPS)
		if err != nil {
			return err
		}
		alphaSourceCapacity, err = alphaTransferCapacity(
			p.LiveFacts.AlphaAvailableRao,
			p.LiveFacts.WalletNetuidAlphaRao,
			p.LiveFacts.AlphaSourceStoredLockRao,
			p.LiveFacts.AlphaSourceCollateralRao,
			p.LiveFacts.WalletNetuidCollateralRao,
		)
		if err != nil || alphaSourceCapacity != p.LiveFacts.AlphaTransferableRao {
			return fmt.Errorf("v5 plan alpha source capacity is inconsistent: derived=%d recorded=%d error=%v", alphaSourceCapacity, p.LiveFacts.AlphaTransferableRao, err)
		}
	}
	deploymentHash := ""
	if planUsesContractDeploymentEnvelope(p.Schema) {
		if p.Deployment.Schema != "urnetwork-contract-deployment-v1" || p.Deployment.DeploymentID != p.DeploymentID || p.Deployment.DeployBlock != 0 || p.Deployment.DeployBlockHash != "" {
			return errors.New("v4 plan has an invalid mutable or foreign contract deployment")
		}
		if !common.IsHexAddress(p.Roles.Deployer) {
			return errors.New("v4 plan has an invalid contract deployer")
		}
		deployer := common.HexToAddress(p.Roles.Deployer)
		if err := validateContractDeploymentIdentity(p.Deployment, deployer); err != nil {
			return err
		}
		if planUsesCoordinatorUpgradeEnvelope(p.Schema) {
			if err := validateCoordinatorUpgradeIdentity(p.CoordinatorUpgrade, deployer, p.Deployment); err != nil {
				return err
			}
			if err := validateCoordinatorUpgradeBaseline(p.CoordinatorUpgradeBaseline, p.Deployment, p.CoordinatorUpgrade); err != nil {
				return err
			}
			if p.CoordinatorUpgrade.Schema == "urnetwork-coordinator-upgrade-v2" && !p.CoordinatorUpgradeBaseline.isRepeated() {
				return errors.New("repeated coordinator upgrade has no finalized compatibility baseline")
			}
			if !p.CoordinatorUpgradeBaseline.isZero() && len(p.PriorPlanHashes) == 0 {
				return errors.New("coordinator upgrade baseline has no authenticated prior plan")
			}
		}
		deploymentHash, err = contractDeploymentIdentityHash(p.Deployment)
		if err != nil {
			return fmt.Errorf("hash v4 contract deployment: %w", err)
		}
		if p.SupersededSpend.TAORao != 0 || p.SupersededSpend.SubnetCreations != 0 || !planUsesCoordinatorUpgradeEnvelope(p.Schema) && (p.SupersededSpend.AlphaRao != 0 || p.SupersededSpend.Registrations != 0) {
			return errors.New("plan recovery may supersede only verified EVM deployment spend, v6 policy-migration alpha spend, and v6 contract-owned registrations")
		}
		seenDeployments := map[string]bool{deploymentHash: true}
		previousNonce := uint64(0)
		for index, superseded := range p.SupersededDeployments {
			if superseded.Schema != "urnetwork-contract-deployment-v1" || superseded.DeploymentID != p.DeploymentID {
				return fmt.Errorf("v4 superseded deployment %d is foreign or invalid", index)
			}
			if err := validateContractDeploymentIdentity(superseded, deployer); err != nil {
				return fmt.Errorf("validate v4 superseded deployment %d: %w", index, err)
			}
			if (superseded.DeployBlock == 0) != (superseded.DeployBlockHash == "") {
				return fmt.Errorf("v4 superseded deployment %d has an incomplete deployment checkpoint", index)
			}
			if superseded.DeployBlockHash != "" {
				if _, err := decodeHex32("superseded deployment block hash", superseded.DeployBlockHash); err != nil {
					return err
				}
			}
			identityHash, hashErr := contractDeploymentIdentityHash(superseded)
			if hashErr != nil {
				return hashErr
			}
			if seenDeployments[identityHash] {
				return fmt.Errorf("v4 superseded deployment %d duplicates an active or prior deployment", index)
			}
			if superseded.InitialNonce >= p.Deployment.InitialNonce || (index > 0 && superseded.InitialNonce <= previousNonce) {
				return fmt.Errorf("v4 superseded deployment %d is not in increasing prior nonce order", index)
			}
			seenDeployments[identityHash] = true
			previousNonce = superseded.InitialNonce
		}
		if len(p.SupersededDeployments) == 0 && (p.SupersededSpend.Registrations != 0 || !p.SupersededSpend.EVMGasWei.IsZero() && !hasVoluntaryRecovery) {
			return errors.New("v4 plan has superseded deployment spend without a superseded deployment")
		}
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
	seenActionDetails := make(map[string]Action, len(p.Actions))
	alphaTransferActions := 0
	recoveryRepairID := ""
	recoveryCount := 0
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
		seenAcceptedIntents := map[string]bool{}
		for _, accepted := range action.AcceptedPriorIntentHashes {
			if action.ID != "evm.reserve-sink" || len(p.PriorPlanHashes) == 0 {
				return fmt.Errorf("action %s cannot accept an ancestor intent", action.ID)
			}
			if _, err := decodeHex32("accepted prior action intent", accepted); err != nil {
				return err
			}
			if accepted == action.IntentHash || seenAcceptedIntents[accepted] {
				return fmt.Errorf("action %s has a duplicate or self-referential accepted intent", action.ID)
			}
			seenAcceptedIntents[accepted] = true
		}
		if planUsesContractDeploymentEnvelope(p.Schema) && actionUsesContractDeployment(action) && action.Parameters[deploymentManifestHashParameter] != deploymentHash {
			return fmt.Errorf("action %s does not bind the approved contract deployment", action.ID)
		}
		if planUsesEVMFeeEnvelope(p.Schema) && action.Kind == "evm-transaction" {
			_, maximumFeePerGas, envelopeErr := evmActionFeeEnvelope(action)
			if envelopeErr != nil {
				return envelopeErr
			}
			if maximumFeePerGas != p.MaximumEVMFeePerGasWei {
				return fmt.Errorf("EVM action %s fee-per-gas limit %d differs from plan limit %d", action.ID, maximumFeePerGas, p.MaximumEVMFeePerGasWei)
			}
		}
		for _, dependency := range action.DependsOn {
			if !seenActions[dependency] {
				return fmt.Errorf("action %s depends on missing or later action %s", action.ID, dependency)
			}
		}
		seenActions[action.ID] = true
		seenActionDetails[action.ID] = action
		if action.ID == voluntaryConvictionReconciliationActionID {
			recoveryCount++
			var recoveryErr error
			recoveryRepairID, recoveryErr = validateVoluntaryConvictionReconciliationAction(p, action, seenPriorPlans)
			if recoveryErr != nil {
				return recoveryErr
			}
		}
		if strings.HasPrefix(action.ID, "evm.fund-") {
			if _, err := evmFundingTerms(action, p.LiveFacts.ExistentialDepositRao); err != nil {
				return err
			}
		}
		if planUsesAlphaTransferEnvelope(p.Schema) && strings.HasPrefix(action.ID, "alpha.transfer.") {
			alphaTransferActions++
			if strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit.") && action.Parameters["exact_amount_rao"] == "" && len(p.PriorPlanHashes) > 0 && action.Spend.AlphaRao > 0 {
				// An authenticated v4 ancestor may have already finalized this
				// exact operator-custody transfer. Revision construction carries
				// its original intent so journal replay cannot spend it twice.
				continue
			}
			exact, exactErr := strconv.ParseUint(action.Parameters["exact_amount_rao"], 10, 64)
			campaign, campaignErr := strconv.ParseUint(action.Parameters["campaign_requirement_rao"], 10, 64)
			minimum, minimumErr := strconv.ParseUint(action.Parameters["minimum_alpha_at_approved_price_rao"], 10, 64)
			price, priceErr := strconv.ParseUint(action.Parameters["approved_alpha_price_q9"], 10, 64)
			runtimeMinimumParameter := "runtime_initial_min_stake_tao_rao"
			approvedRuntimeMinimum := p.LiveFacts.InitialMinStakeRao
			expectedMinimum := minimumAlphaTransfer
			if planUsesDefaultMinTransferEnvelope(p.Schema) {
				hasDefaultFloor := action.Parameters["runtime_default_min_transfer_tao_rao"] != ""
				hasLegacyFloor := action.Parameters["runtime_initial_min_stake_tao_rao"] != ""
				if hasDefaultFloor == hasLegacyFloor {
					return fmt.Errorf("alpha transfer action %s has an ambiguous default-transfer runtime floor", action.ID)
				}
				if hasDefaultFloor {
					runtimeMinimumParameter = "runtime_default_min_transfer_tao_rao"
					approvedRuntimeMinimum = p.LiveFacts.DefaultMinTransferRao
				} else {
					if len(p.PriorPlanHashes) == 0 {
						return fmt.Errorf("alpha transfer action %s has an unauthenticated legacy runtime floor", action.ID)
					}
					legacyMinimum, legacyErr := minimumAlphaTransferRao(p.LiveFacts.InitialMinStakeRao, p.LiveFacts.AlphaPriceQ9, p.AlphaTransferMarginBPS)
					if legacyErr != nil {
						return legacyErr
					}
					expectedMinimum = legacyMinimum
				}
			}
			runtimeMinimum, runtimeErr := strconv.ParseUint(action.Parameters[runtimeMinimumParameter], 10, 64)
			margin, marginErr := strconv.ParseUint(action.Parameters["minimum_tao_equivalent_margin_bps"], 10, 16)
			if exactErr != nil || campaignErr != nil || minimumErr != nil || priceErr != nil || runtimeErr != nil || marginErr != nil || exact == 0 || exact != action.Spend.AlphaRao || campaign > exact || minimum != expectedMinimum || exact < minimum || price != p.LiveFacts.AlphaPriceQ9 || runtimeMinimum != approvedRuntimeMinimum || uint16(margin) != p.AlphaTransferMarginBPS {
				return fmt.Errorf("alpha transfer action %s does not bind the v5 runtime floor and exact spend", action.ID)
			}
			if planUsesDestinationRoundingEnvelope(p.Schema) {
				shortfall, shortfallErr := strconv.ParseUint(action.Parameters["maximum_destination_rounding_shortfall_rao"], 10, 64)
				minimumCredit, creditErr := strconv.ParseUint(action.Parameters["minimum_destination_credit_rao"], 10, 64)
				legacyCarried := action.Parameters["maximum_destination_rounding_shortfall_rao"] == "" && action.Parameters["minimum_destination_credit_rao"] == "" && len(p.PriorPlanHashes) > 0
				if !legacyCarried && (shortfallErr != nil || creditErr != nil || shortfall != alphaTransferDestinationRoundingAllowance || minimumCredit != exact-shortfall) {
					return fmt.Errorf("alpha transfer action %s does not bind the v8 destination-rounding envelope", action.ID)
				}
				if planUsesTwoTransitionReserveEnvelope(p.Schema) && strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit.") {
					reserveCalls, callsErr := strconv.ParseUint(action.Parameters["reserve_calls"], 10, 64)
					reserveAllowance, allowanceErr := strconv.ParseUint(action.Parameters["reserve_rounding_allowance_per_call_rao"], 10, 64)
					legacyReserve := len(p.PriorPlanHashes) > 0 && (action.Kind == "substrate-reconciliation" || (action.Parameters["reserve_calls"] == "" && action.Parameters["reserve_rounding_allowance_per_call_rao"] == ""))
					totalAllowance, multiplyOK := checkedMul(reserveCalls, reserveAllowance)
					minimumRequired, addOK := checkedAdd(campaign, totalAllowance)
					if !legacyReserve && (callsErr != nil || allowanceErr != nil || reserveCalls == 0 || reserveAllowance != reserveRoundingAllowancePerCallRao || !multiplyOK || !addOK || minimumCredit < minimumRequired) {
						return fmt.Errorf("operator alpha transfer %s does not bind the two-leg reserve rounding envelope", action.ID)
					}
				}
			}
			if planUsesCoordinatorUpgradeEnvelope(p.Schema) && strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit.") && !strings.EqualFold(action.Parameters["campaign_policy_hash"], p.PolicyHash) {
				return fmt.Errorf("operator alpha transfer %s does not bind the campaign policy", action.ID)
			}
			if action.Kind == "substrate-reconciliation" {
				recoveryBlock, blockErr := strconv.ParseUint(action.Parameters[alphaRecoveryBlockParameter], 10, 64)
				if !seenPriorPlans[action.Parameters[alphaRecoveryPlanHashParameter]] || action.Parameters[alphaRecoveryIntentHashParameter] == "" || action.Parameters[alphaRecoveryTransactionHashParameter] == "" || recoveryBlock == 0 || blockErr != nil {
					return fmt.Errorf("alpha reconciliation %s has an incomplete or foreign recovery envelope", action.ID)
				}
				for _, field := range []string{alphaRecoveryIntentHashParameter, alphaRecoveryTransactionHashParameter, alphaRecoveryBlockHashParameter} {
					if _, decodeErr := decodeHex32(field, action.Parameters[field]); decodeErr != nil {
						return decodeErr
					}
				}
			} else if action.Parameters[alphaRecoveryTransactionHashParameter] != "" {
				return fmt.Errorf("ordinary alpha transfer %s carries a recovery transaction", action.ID)
			}
		}
		if planUsesDestinationRoundingEnvelope(p.Schema) && strings.HasPrefix(action.ID, "alpha.repair.") {
			exact, exactErr := strconv.ParseUint(action.Parameters["exact_amount_rao"], 10, 64)
			minimum, minimumErr := strconv.ParseUint(action.Parameters["minimum_alpha_at_approved_price_rao"], 10, 64)
			price, priceErr := strconv.ParseUint(action.Parameters["approved_alpha_price_q9"], 10, 64)
			runtimeMinimum, runtimeErr := strconv.ParseUint(action.Parameters["runtime_default_min_transfer_tao_rao"], 10, 64)
			margin, marginErr := strconv.ParseUint(action.Parameters["minimum_tao_equivalent_margin_bps"], 10, 16)
			shortfall, shortfallErr := strconv.ParseUint(action.Parameters["maximum_destination_rounding_shortfall_rao"], 10, 64)
			minimumCredit, creditErr := strconv.ParseUint(action.Parameters["minimum_destination_credit_rao"], 10, 64)
			minimumIncrement, incrementErr := strconv.ParseUint(action.Parameters[alphaRepairMinimumIncrementParameter], 10, 64)
			minimumDestination, destinationErr := strconv.ParseUint(action.Parameters[alphaRepairMinimumDestinationParameter], 10, 64)
			absoluteTopUp := action.Parameters[alphaRepairMinimumDestinationParameter] != ""
			repairs := action.Parameters[alphaRepairForActionParameter]
			kind, _, targetErr := alphaTransferTargetFromActionID(action.ID)
			shareTarget, shareMinimum, reserveShareRepair, shareErr := reserveShareRepairTerms(action)
			if exactErr != nil || minimumErr != nil || priceErr != nil || runtimeErr != nil || marginErr != nil || shortfallErr != nil || creditErr != nil || targetErr != nil || shareErr != nil || exact != action.Spend.AlphaRao || exact < minimumAlphaTransfer || minimum != minimumAlphaTransfer || price != p.LiveFacts.AlphaPriceQ9 || runtimeMinimum != p.LiveFacts.DefaultMinTransferRao || uint16(margin) != p.AlphaTransferMarginBPS || shortfall != alphaTransferDestinationRoundingAllowance || minimumCredit != exact-shortfall || !seenActions[repairs] {
				return fmt.Errorf("alpha repair %s does not bind its v8 recovery and rounding envelope", action.ID)
			}
			if reserveShareRepair {
				before, beforeErr := strconv.ParseUint(action.Parameters[alphaRepairCumulativeBeforeParameter], 10, 64)
				limit, limitErr := strconv.ParseUint(action.Parameters[alphaRepairCumulativeLimitParameter], 10, 64)
				maximumTranche, trancheErr := strconv.ParseUint(action.Parameters[alphaRepairMaximumTrancheParameter], 10, 64)
				cumulative, addOK := checkedAdd(before, exact)
				linked := seenActionDetails[repairs]
				linkedTarget, linkedTargetErr := strconv.ParseUint(linked.Parameters["reserve_target_share_bps"], 10, 16)
				linkedMinimum, linkedMinimumErr := strconv.ParseUint(linked.Parameters["reserve_minimum_share_bps"], 10, 16)
				if beforeErr != nil || limitErr != nil || trancheErr != nil || maximumTranche == 0 || exact > maximumTranche || linkedTargetErr != nil || linkedMinimumErr != nil || !addOK || cumulative != limit || limit > p.Limits.AlphaRao || uint16(linkedTarget) != shareTarget || uint16(linkedMinimum) != shareMinimum || action.Parameters["planned_existing_stake_rao"] != "" || action.Parameters["planned_final_stake_rao"] != "" || action.Parameters["registered_alpha_snapshot_rao"] != "" {
					return fmt.Errorf("alpha repair %s does not bind its fixed cumulative reserve-share tranche", action.ID)
				}
			} else if absoluteTopUp {
				if !planUsesTwoTransitionReserveEnvelope(p.Schema) || destinationErr != nil || minimumDestination == 0 || action.Parameters[alphaRepairMinimumIncrementParameter] != "" {
					return fmt.Errorf("alpha repair %s has an invalid absolute destination target", action.ID)
				}
			} else if incrementErr != nil || minimumIncrement == 0 || exact != minimumAlphaTransfer {
				return fmt.Errorf("alpha repair %s has an invalid recovered-prestate target", action.ID)
			}
			if kind == "operator-deposit" && !strings.EqualFold(action.Parameters["campaign_policy_hash"], p.PolicyHash) {
				return fmt.Errorf("operator alpha repair %s does not bind the campaign policy", action.ID)
			}
		}
		if planUsesTwoTransitionReserveEnvelope(p.Schema) && (action.ID == "campaign.voluntary-conviction.1" || action.ID == dishonestDepositActionID) {
			transitions, transitionsErr := strconv.ParseUint(action.Parameters["reserve_runtime_share_transitions"], 10, 64)
			allowance, allowanceErr := strconv.ParseUint(action.Parameters["reserve_rounding_allowance_rao"], 10, 64)
			if transitionsErr != nil || allowanceErr != nil || transitions != reserveRuntimeShareTransitionCount || allowance != reserveRoundingAllowancePerCallRao {
				return fmt.Errorf("campaign reserve action %s does not bind both runtime share transitions", action.ID)
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
	if recoveryCount > 1 {
		return errors.New("plan has multiple voluntary-conviction reconciliations")
	}
	if recoveryCount == 1 {
		linked := false
		for _, action := range p.Actions {
			linked = linked || action.ID == recoveryRepairID && action.Parameters[alphaRepairForActionParameter] == voluntaryConvictionReconciliationActionID && slices.Contains(action.DependsOn, voluntaryConvictionReconciliationActionID)
		}
		if !linked {
			return errors.New("voluntary-conviction reconciliation has no exact alpha-repair barrier")
		}
	}
	if err := validateFleetCommitmentRecoveryBudget(p); err != nil {
		return err
	}
	if planUsesAlphaTransferEnvelope(p.Schema) && (alphaTransferActions != 4 || !seenActions["validator.reserve-majority"]) {
		return fmt.Errorf("v5 plan has %d alpha transfers or no reserve-majority barrier", alphaTransferActions)
	}
	maximumSpend, err := maximumActionSpend(p.Actions)
	if err != nil {
		return err
	}
	spendMatches, spendErr := equalSpend(maximumSpend, p.MaximumSpend)
	if spendErr != nil {
		return fmt.Errorf("compare action spend total to plan maximum: %w", spendErr)
	}
	if !spendMatches {
		return fmt.Errorf("action spend total %+v does not equal plan maximum %+v", maximumSpend, p.MaximumSpend)
	}
	if planUsesAlphaTransferEnvelope(p.Schema) {
		requiredPosition, addOK := checkedAdd(p.MaximumSpend.AlphaRao, p.MinimumSourceRemainingRao)
		if !addOK || p.LiveFacts.AlphaAvailableRao < requiredPosition || alphaSourceCapacity < p.MaximumSpend.AlphaRao {
			return fmt.Errorf("v5 plan alpha spend %d exceeds source position/capacity %d/%d with remainder %d", p.MaximumSpend.AlphaRao, p.LiveFacts.AlphaAvailableRao, alphaSourceCapacity, p.MinimumSourceRemainingRao)
		}
	}
	if p.MaximumSpend.SubnetCreations != 0 || p.Limits.SubnetCreations != 0 {
		return fmt.Errorf("subnet creation is forbidden")
	}
	return nil
}

// Add two spend vectors without allowing any budget dimension to wrap.
func addSpends(left, right Spend) (Spend, error) {
	result := Spend{}
	var ok bool
	result.TAORao, ok = checkedAdd(left.TAORao, right.TAORao)
	if !ok {
		return Spend{}, errors.New("TAO spend overflow")
	}
	result.AlphaRao, ok = checkedAdd(left.AlphaRao, right.AlphaRao)
	if !ok {
		return Spend{}, errors.New("alpha spend overflow")
	}
	var err error
	result.EVMGasWei, err = addDecimalUint(left.EVMGasWei, right.EVMGasWei)
	if err != nil {
		return Spend{}, fmt.Errorf("EVM gas spend: %w", err)
	}
	registrations := uint64(left.Registrations) + uint64(right.Registrations)
	subnetCreations := uint64(left.SubnetCreations) + uint64(right.SubnetCreations)
	if registrations > math.MaxUint32 || subnetCreations > math.MaxUint32 {
		return Spend{}, errors.New("counted spend overflow")
	}
	result.Registrations = uint32(registrations)
	result.SubnetCreations = uint32(subnetCreations)
	return result, nil
}

// Compare spend vectors while treating the zero value and canonical decimal
// zero as the same aggregate EVM amount.
func equalSpend(left, right Spend) (bool, error) {
	if left.TAORao != right.TAORao || left.AlphaRao != right.AlphaRao || left.Registrations != right.Registrations || left.SubnetCreations != right.SubnetCreations {
		return false, nil
	}
	comparison, err := left.EVMGasWei.Cmp(right.EVMGasWei)
	if err != nil {
		return false, err
	}
	return comparison == 0, nil
}

// Report semantic zero without relying on the in-memory representation of the
// arbitrary-precision EVM component. JSON round trips canonicalize its empty Go
// value to "0", and both representations must validate identically.
func spendIsZero(value Spend) bool {
	return value.TAORao == 0 && value.AlphaRao == 0 && value.EVMGasWei.IsZero() && value.Registrations == 0 && value.SubnetCreations == 0
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

// Decode the two-dimensional EVM approval and prove its product is the exact
// representable portion of the action's aggregate wei ceiling.
func evmActionFeeEnvelope(action Action) (uint64, uint64, error) {
	if action.Kind != "evm-transaction" || action.Spend.EVMGasWei.IsZero() {
		return 0, 0, fmt.Errorf("action %s has no EVM transaction gas ceiling", action.ID)
	}
	maximumGasUnits, err := strconv.ParseUint(action.Parameters[evmMaximumGasUnitsParameter], 10, 64)
	if err != nil || maximumGasUnits == 0 {
		return 0, 0, fmt.Errorf("EVM action %s has invalid %s", action.ID, evmMaximumGasUnitsParameter)
	}
	maximumFeePerGas, err := strconv.ParseUint(action.Parameters[evmMaximumFeePerGasParameter], 10, 64)
	if err != nil || maximumFeePerGas == 0 {
		return 0, 0, fmt.Errorf("EVM action %s has invalid %s", action.ID, evmMaximumFeePerGasParameter)
	}
	maximumCost := new(big.Int).Mul(new(big.Int).SetUint64(maximumGasUnits), new(big.Int).SetUint64(maximumFeePerGas))
	actionCeiling, err := action.Spend.EVMGasWei.Big()
	if err != nil {
		return 0, 0, fmt.Errorf("EVM action %s aggregate wei ceiling: %w", action.ID, err)
	}
	remainder := new(big.Int).Sub(new(big.Int).Set(actionCeiling), maximumCost)
	if remainder.Sign() < 0 || remainder.Cmp(new(big.Int).SetUint64(maximumFeePerGas)) >= 0 {
		return 0, 0, fmt.Errorf("EVM action %s gas-unit and fee ceilings do not match its aggregate wei ceiling", action.ID)
	}
	return maximumGasUnits, maximumFeePerGas, nil
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
	if !ok || action.Spend.TAORao != maximumTransfer || action.Spend.AlphaRao != 0 || !action.Spend.EVMGasWei.IsZero() || action.Spend.Registrations != 0 || action.Spend.SubnetCreations != 0 {
		return 0, fmt.Errorf("EVM funding action %s maximum spend does not equal usable balance plus existential deposit", action.ID)
	}
	return usable, nil
}

func derivePublicRoles(cfg *ResolvedConfig) (PublicRoles, error) {
	roles, err := buildEVMRoleSecrets(cfg)
	if err != nil {
		return PublicRoles{}, err
	}
	address := func(label string) (string, error) {
		role, ok := roles[label]
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
