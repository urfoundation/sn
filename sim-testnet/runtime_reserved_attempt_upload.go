package main

// Rendering copies explicit reserved capacity and independently approved
// deployment pins; it never treats an activation list as staging authority.
import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/server/v2026/controller"
	"gopkg.in/yaml.v3"
)

// Planning may omit the whole owner; any supplied census must be complete,
// sorted and finite. Known owners are a minimum, not derived quota defaults.
func validateRuntimeReservedAttemptUploadCensus(cfg *HarnessConfig) error {
	if cfg == nil || cfg.Topology.Operators < 2 || cfg.Topology.Validators < 1 || len(cfg.Artifacts.ReservedAttemptUploads) != cfg.Topology.Operators {
		return errors.New("runtime reserved staging requires explicit capacity for every operator")
	}
	for index, value := range cfg.Artifacts.ReservedAttemptUploads {
		domain := value.Admission.Deployment
		if value.Admission.ReplicaNoID != uint64(index+1) || uint64(cfg.Topology.Validators) > value.Admission.MaximumOwners/uint64(cfg.Topology.Operators) {
			return errors.New("runtime reserved staging census omits a configured original/destination owner")
		}
		if runtimeReservedAttemptUploadIsTemplate(value) {
			if !cfg.ProvisionValidatorEvidenceV2 || domain.MaximumSubnetUIDs == 0 || domain.MaximumSubnetUIDs > 65535 || value.Admission.ActivationContexts == nil || len(value.Admission.ActivationContexts) != 0 {
				return errors.New("runtime reserved staging template requires explicit fresh provisioning, native census and empty discovery")
			}
			if err := value.ValidateCapacity(); err != nil {
				return err
			}
			continue
		}
		profile := &controller.StConfig{Enabled: true, Profile: "testnet", DeploymentId: cfg.Deployment.DeploymentID, ChainId: testnetChainID,
			GenesisHash: [32]byte(common.HexToHash(testnetGenesis)), Netuid: uint64(domain.Netuid), NoId: uint64(index + 1), ContractAddress: common.Address(domain.Coordinator), SettlementVault: common.Address(domain.SettlementVault)}
		if err := value.Validate(profile); err != nil {
			return err
		}
	}
	return nil
}

// Only the independently approved native census is a static capacity field;
// every generated identity must be absent together, never partially supplied.
func runtimeReservedAttemptUploadIsTemplate(value controller.StReservedAttemptUploadConfig) bool {
	domain := value.Admission.Deployment
	domain.MaximumSubnetUIDs = 0
	return domain == (validatorpkg.ValidatorUploadDeployment{})
}

// The canonical approved setup and authenticated journal locate the original
// companion CREATE. A later start could silently hide external activations.
func runtimeReservedAttemptUploads(cfg *ResolvedConfig, stateDir string, contracts *ContractDeployment) ([]controller.StReservedAttemptUploadConfig, error) {
	if cfg == nil || cfg.Public == nil || cfg.Release == nil || contracts == nil {
		return nil, errors.New("runtime reserved staging deployment is absent")
	}
	if err := validateRuntimeReservedAttemptUploadCensus(cfg.Config); err != nil {
		return nil, err
	}
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	if plan.ValidatorEvidence == nil {
		return nil, errors.New("runtime reserved staging companion is not in the approved plan")
	}
	creation, err := runtimeReservedAttemptUploadCreation(cfg, stateDir, plan)
	if err != nil {
		return nil, fmt.Errorf("runtime reserved staging companion CREATE: %w", err)
	}
	result := slices.Clone(cfg.Config.Artifacts.ReservedAttemptUploads)
	expectedRuntime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.Public.Chain.ExpectedRuntimeSpec,
		TransactionVersion: cfg.Public.Chain.ExpectedTransactionVersion, StateVersion: cfg.Public.Chain.ExpectedStateVersion}, CodeHash: cfg.Release.Runtime.CodeHash, MetadataHash: cfg.Release.Runtime.MetadataHash}
	companion := plan.ValidatorEvidence
	for index := range result {
		value := &result[index]
		if runtimeReservedAttemptUploadIsTemplate(*value) {
			genesis, err := decodeHex32("runtime reserved staging native genesis", cfg.Public.Chain.GenesisHash)
			if err != nil {
				return nil, err
			}
			value.Admission.Deployment = validatorpkg.ValidatorUploadDeployment{ChainID: plan.ChainID, GenesisHash: genesis, Netuid: plan.Netuid,
				Coordinator: [20]byte(companion.Coordinator), SettlementVault: [20]byte(companion.SettlementVault), DeploymentIDHash: [32]byte(companion.DeploymentIDHash),
				Journal: [20]byte(companion.Address), RuntimeHash: [32]byte(companion.RuntimeCodeHash), DeploymentBlock: creation.BlockNumber,
				NativeRuntime: expectedRuntime, MaximumSubnetUIDs: value.Admission.Deployment.MaximumSubnetUIDs}
		}
		domain := value.Admission.Deployment
		if domain.Netuid != cfg.Netuid || common.Address(domain.Coordinator) != contracts.CoordinatorProxy || common.Address(domain.SettlementVault) != contracts.SettlementVault ||
			common.Address(domain.Journal) != companion.Address || common.Hash(domain.RuntimeHash) != companion.RuntimeCodeHash || common.Hash(domain.DeploymentIDHash) != companion.DeploymentIDHash ||
			types.Hash(domain.GenesisHash).Hex() != cfg.Public.Chain.GenesisHash || domain.NativeRuntime != expectedRuntime || domain.DeploymentBlock > creation.BlockNumber {
			return nil, errors.New("runtime reserved staging pins differ from approved immutable deployment/runtime or complete discovery start")
		}
		profile := &controller.StConfig{Enabled: true, Profile: "testnet", DeploymentId: plan.DeploymentID, ChainId: plan.ChainID,
			GenesisHash: domain.GenesisHash, Netuid: uint64(plan.Netuid), NoId: uint64(index + 1), ContractAddress: contracts.CoordinatorProxy, SettlementVault: contracts.SettlementVault}
		if err := value.Validate(profile); err != nil {
			return nil, fmt.Errorf("runtime reserved staging resolved deployment: %w", err)
		}
		for _, source := range cfg.Config.ValidatorEvidenceV2 {
			if source.Evidence.UploadIntentSeconds == 0 || source.Evidence.UploadIntentSeconds > value.Admission.MaximumIntentSeconds {
				return nil, errors.New("runtime reserved staging intent lifetime is absent or exceeds a destination")
			}
		}
		value.NativeRPCURLs = slices.Clone(value.NativeRPCURLs)
		value.Admission.ActivationContexts = slices.Clone(value.Admission.ActivationContexts)
	}
	return result, nil
}

// Rendering reopens the original approved transaction and its verified source
// observation. This is retained setup evidence, not a new online finality check;
// the executor and staging discovery still authenticate their live chain reads.
func runtimeReservedAttemptUploadCreation(cfg *ResolvedConfig, stateDir string, plan *SetupPlan) (ValidatorEvidenceCarryReceipt, error) {
	zero := ValidatorEvidenceCarryReceipt{}
	if cfg == nil || cfg.Config == nil || plan == nil || plan.ValidatorEvidence == nil {
		return zero, errors.New("approved companion source is absent")
	}
	action, err := exactPlanActionByID(plan, validatorEvidenceDeployActionID)
	if err != nil {
		return zero, err
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		return zero, err
	}
	creation, broadcast, verified, err := validatorEvidenceHistoryReceipt(plan, entries, action)
	if err != nil {
		return zero, err
	}
	if err := verifyFinalHead("companion CREATE", ChainHead{Number: creation.BlockNumber, Hash: creation.BlockHash}); err != nil {
		return zero, err
	}
	source := plan
	if carry := plan.ValidatorEvidenceCarry; carry != nil {
		if err := validateValidatorEvidenceCarryShape(plan); err != nil {
			return zero, err
		}
		if carry.Creation != creation {
			return zero, errors.New("companion CREATE differs from its approved original carry receipt")
		}
		source, err = readValidatorEvidenceHistoricalPlan(stateDir, carry.SourcePlanHash)
		if err != nil {
			return zero, err
		}
		if source.ValidatorEvidenceCarry != nil || source.ReleaseLockHash != carry.SourceReleaseLockHash {
			return zero, errors.New("companion CREATE does not belong to the original approved source release")
		}
	} else if creation.PlanHash != plan.PlanHash {
		return zero, errors.New("companion CREATE predecessor lacks an approved carry")
	}
	if err := validatorEvidenceSourcePlanMatches(plan, source); err != nil {
		return zero, err
	}
	if _, err := readValidatorEvidenceSourceTransaction(stateDir, source, action, creation, broadcast); err != nil {
		return zero, err
	}
	record, err := readValidatorEvidenceSourcePostcondition(stateDir, cfg, source, verified)
	if err != nil {
		return zero, err
	}
	manifest := source.ValidatorEvidence
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		return zero, err
	}
	for _, observation := range []struct {
		head   ChainHead
		values map[string]any
	}{
		{head: record.EVMFinalized, values: record.Observed},
		{head: record.IndependentEVMFinalized, values: record.IndependentObserved},
	} {
		if observation.head.Number < creation.BlockNumber || observation.head.Number == creation.BlockNumber && observation.head.Hash != creation.BlockHash {
			return zero, errors.New("companion CREATE conflicts with its verified finalized checkpoint")
		}
		want := map[string]any{"kind": action.Kind, "target": action.Target, "address": manifest.Address.Hex(), "runtime_hash": manifest.RuntimeCodeHash.Hex(),
			"coordinator": manifest.Coordinator.Hex(), validatorEvidenceManifestParameter: manifestHash}
		anchor, ok := observation.values["coordinator_validator_evidence"].(string)
		if !ok || anchor != (common.Address{}).Hex() && anchor != manifest.Address.Hex() || len(observation.values) != len(want)+1 {
			return zero, errors.New("companion CREATE observation has another coordinator anchor or source shape")
		}
		for name, value := range want {
			actual, ok := observation.values[name].(string)
			if !ok || actual != value {
				return zero, errors.New("companion CREATE observation differs from the approved immutable source")
			}
		}
	}
	return creation, nil
}

// The ordinary and protected policies are decoded and hashed from the SAME
// owned st.yml bytes. Rehashing a modified file never changes approved capacity.
func validateRuntimeReservedAttemptUploadNode(cfg *ResolvedConfig, stateDir, relative string, root *yaml.Node) error {
	var node *yaml.Node
	for index := 0; index < len(root.Content); index += 2 {
		if root.Content[index].Value == "testnet-reserved-attempt-upload" {
			node = root.Content[index+1]
		}
	}
	if cfg == nil || cfg.Config == nil {
		return errors.New("runtime reserved staging configuration is missing")
	}
	if len(cfg.Config.Artifacts.ReservedAttemptUploads) == 0 {
		if node != nil {
			return errors.New("runtime reserved staging was not approved")
		}
		return nil
	}
	contracts, err := loadContractDeployment(stateDir)
	if err != nil {
		return err
	}
	values, err := runtimeReservedAttemptUploads(cfg, stateDir, contracts)
	if err != nil {
		return err
	}
	var expected *controller.StReservedAttemptUploadConfig
	for index := range values {
		path := filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("operator-%d", index+1), "vault", "st.yml"))
		if relative == path {
			expected = &values[index]
			break
		}
	}
	if expected == nil || node == nil {
		return errors.New("runtime reserved staging route or explicit value is missing")
	}
	var actual controller.StReservedAttemptUploadConfig
	if err := node.Decode(&actual); err != nil {
		return err
	}
	want, err := canonicalHashHex(expected)
	if err != nil {
		return err
	}
	observed, err := canonicalHashHex(actual)
	if err != nil || observed != want {
		return errors.New("runtime reserved staging differs from approved capacity or authority pins")
	}
	return nil
}
