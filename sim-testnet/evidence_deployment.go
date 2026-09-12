// Binds the immutable evidence journal to an additive CREATE and the existing
// coordinator/vault identities. Proof publication remains a separate operation.
package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/stabi"
)

const (
	validatorEvidenceDeployActionID    = "evm.validator-evidence-deploy"
	validatorEvidenceAnchorActionID    = "evm.validator-evidence-anchor"
	validatorEvidenceManifestParameter = "validator_evidence_manifest_hash"
)

// The companion is additive: changing a release must not change or re-hash the
// historical six-contract custody graph. This complete identity is separately
// included in the approved plan and the public deployment manifest.
type ValidatorEvidenceDeployment struct {
	Schema           string         `json:"schema"`
	DeploymentID     string         `json:"deployment_id"`
	Address          common.Address `json:"address"`
	Deployer         common.Address `json:"deployer"`
	DeployerNonce    uint64         `json:"deployer_nonce"`
	Coordinator      common.Address `json:"coordinator"`
	SettlementVault  common.Address `json:"settlement_vault"`
	ChainID          uint64         `json:"chain_id"`
	Netuid           uint16         `json:"netuid"`
	GenesisHash      common.Hash    `json:"genesis_hash"`
	DeploymentIDHash common.Hash    `json:"deployment_id_hash"`
	RuntimeCodeHash  common.Hash    `json:"runtime_code_hash"`
}

// The caller owns both byte slices; construction never borrows generated
// bytecode or changes the older deployment's maps, nonces or addresses.
type validatorEvidenceDeploymentPayloads struct {
	Manifest ValidatorEvidenceDeployment
	Artifact ContractArtifact
	Creation []byte
	Runtime  []byte
	Anchor   []byte
}

// Uses the next deployer nonce after the existing fleet helper, leaving all
// earlier CREATE identities stable, including repeated coordinator upgrades.
func buildValidatorEvidenceDeployment(payloads *DeploymentPayloads, chainID uint64, genesis common.Hash, netuid uint16) (*validatorEvidenceDeploymentPayloads, error) {
	if payloads == nil || payloads.Deployer == (common.Address{}) || payloads.Manifest.DeploymentID == "" || strings.TrimSpace(payloads.Manifest.DeploymentID) != payloads.Manifest.DeploymentID ||
		payloads.Manifest.CoordinatorProxy == (common.Address{}) || payloads.Manifest.SettlementVault == (common.Address{}) || payloads.Manifest.CoordinatorProxy == payloads.Manifest.SettlementVault ||
		chainID == 0 || genesis == (common.Hash{}) || netuid == 0 {
		return nil, errors.New("validator evidence deployment domain is incomplete")
	}
	if payloads.CoordinatorUpgrade.DeployerNonce >= ^uint64(0)-1 || payloads.FleetBatcherNonce != payloads.CoordinatorUpgrade.DeployerNonce+1 ||
		payloads.CoordinatorUpgrade.Implementation != crypto.CreateAddress(payloads.Deployer, payloads.CoordinatorUpgrade.DeployerNonce) ||
		payloads.FleetBatcherAddress != crypto.CreateAddress(payloads.Deployer, payloads.FleetBatcherNonce) {
		return nil, errors.New("validator evidence deployment has an invalid additive nonce boundary")
	}
	artifact, err := currentValidatorEvidenceArtifact()
	if err != nil {
		return nil, err
	}
	return buildValidatorEvidenceDeploymentFromArtifact(payloads, chainID, genesis, netuid, payloads.FleetBatcherNonce+1, artifact)
}

// Fresh installs require exactly one generated artifact. Historical reader
// compatibility is checked separately against the actual public Go binding.
func currentValidatorEvidenceArtifact() (ContractArtifact, error) {
	artifact := ContractArtifact{}
	for _, candidate := range ReleaseContractArtifacts {
		if candidate.Name == "ValidatorEvidence" {
			if artifact.Name != "" {
				return ContractArtifact{}, errors.New("validator evidence release artifact is duplicated")
			}
			artifact = candidate
		}
	}
	if artifact.Name == "" || artifact.CreationBytecode == "" || artifact.RuntimeBytecode == "" {
		return ContractArtifact{}, errors.New("validator evidence release artifact is unavailable")
	}
	return artifact, nil
}

// Historical construction is used only after the source approval and artifact
// are authenticated. It retains the original CREATE nonce and executable.
func buildValidatorEvidenceDeploymentFromArtifact(payloads *DeploymentPayloads, chainID uint64, genesis common.Hash, netuid uint16, nonce uint64, artifact ContractArtifact) (*validatorEvidenceDeploymentPayloads, error) {
	if payloads == nil || payloads.Deployer == (common.Address{}) || payloads.Manifest.DeploymentID == "" || strings.TrimSpace(payloads.Manifest.DeploymentID) != payloads.Manifest.DeploymentID || payloads.Manifest.CoordinatorProxy == (common.Address{}) || payloads.Manifest.SettlementVault == (common.Address{}) || payloads.Manifest.CoordinatorProxy == payloads.Manifest.SettlementVault || chainID == 0 || genesis == (common.Hash{}) || netuid == 0 || nonce == ^uint64(0) {
		return nil, errors.New("validator evidence source deployment domain or nonce is incomplete")
	}
	if err := validateValidatorEvidenceSourceArtifact(artifact); err != nil {
		return nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(artifact.ABI))
	if err != nil {
		return nil, fmt.Errorf("validator evidence constructor ABI: %w", err)
	}
	deploymentHash := sha256.Sum256([]byte(payloads.Manifest.DeploymentID))
	arguments, err := parsed.Constructor.Inputs.Pack(payloads.Manifest.CoordinatorProxy, [32]byte(genesis), deploymentHash)
	if err != nil {
		return nil, fmt.Errorf("validator evidence constructor: %w", err)
	}
	runtime, err := runtimeWithImmutables(artifact, map[string][]byte{
		"coordinator":      abiWordAddress(payloads.Manifest.CoordinatorProxy),
		"settlementVault":  abiWordAddress(payloads.Manifest.SettlementVault),
		"chainId":          abiWordUint(chainID),
		"netuid":           abiWordUint(uint64(netuid)),
		"genesisHash":      genesis[:],
		"deploymentIdHash": deploymentHash[:],
	})
	if err != nil {
		return nil, err
	}
	if len(runtime) == 0 || len(runtime) > 24*1024 {
		return nil, errors.New("validator evidence runtime exceeds the EIP-170 deployment bound")
	}
	address := crypto.CreateAddress(payloads.Deployer, nonce)
	for _, existing := range append(contractDeploymentAddresses(payloads.Manifest), payloads.PrecompileProbeAddress, payloads.CoordinatorUpgrade.Implementation, payloads.FleetBatcherAddress) {
		if address == existing {
			return nil, errors.New("validator evidence deployment aliases an existing contract")
		}
	}
	return &validatorEvidenceDeploymentPayloads{
		Artifact: cloneValidatorEvidenceArtifact(artifact),
		Manifest: ValidatorEvidenceDeployment{
			Schema: "urnetwork-validator-evidence-deployment-v1", DeploymentID: payloads.Manifest.DeploymentID,
			Address: address, Deployer: payloads.Deployer, DeployerNonce: nonce,
			Coordinator: payloads.Manifest.CoordinatorProxy, SettlementVault: payloads.Manifest.SettlementVault,
			ChainID: chainID, Netuid: netuid, GenesisHash: genesis, DeploymentIDHash: deploymentHash,
			RuntimeCodeHash: crypto.Keccak256Hash(runtime),
		},
		Creation: append(hexBytes(artifact.CreationBytecode), arguments...), Runtime: runtime,
		Anchor: stabi.NewSTCoordinator().PackFixValidatorEvidence(address),
	}, nil
}

// A caller-provided identity is compared against independently rebuilt release
// payloads. It cannot choose the authoritative domain by describing itself.
func validateValidatorEvidenceDeployment(actual *ValidatorEvidenceDeployment, expected *validatorEvidenceDeploymentPayloads) error {
	if actual == nil || expected == nil || *actual != expected.Manifest {
		return errors.New("approved validator evidence deployment does not match the release payload")
	}
	return nil
}

// Both action intents carry the same immutable companion identity. Only the
// CREATE uses the deployer's exact reserved nonce; the owner link uses its own
// journalled transaction nonce without imposing a second account's sequence.
func (self *validatorEvidenceDeploymentPayloads) actionParameters(actionID string, owner common.Address) (map[string]string, error) {
	if self == nil || self.Manifest.Address == (common.Address{}) || owner == (common.Address{}) {
		return nil, errors.New("validator evidence action authority is incomplete")
	}
	hash, err := canonicalHashHex(self.Manifest)
	if err != nil {
		return nil, err
	}
	parameters := map[string]string{
		validatorEvidenceManifestParameter: hash,
		"validator_evidence":               self.Manifest.Address.Hex(),
		"coordinator":                      self.Manifest.Coordinator.Hex(),
		"runtime_code_hash":                self.Manifest.RuntimeCodeHash.Hex(),
	}
	parameters[validatorEvidenceArtifactParameter], err = canonicalHashHex(self.Artifact)
	if err != nil {
		return nil, err
	}
	switch actionID {
	case validatorEvidenceDeployActionID:
		parameters["expected_signer"] = self.Manifest.Deployer.Hex()
		parameters["expected_nonce"] = strconv.FormatUint(self.Manifest.DeployerNonce, 10)
		parameters["expected_transaction_to"] = "create"
		parameters["expected_created_address"] = self.Manifest.Address.Hex()
		parameters["expected_value_wei"] = "0"
		parameters["expected_data_keccak256"] = crypto.Keccak256Hash(self.Creation).Hex()
	case validatorEvidenceAnchorActionID:
		parameters["anchor_owner"] = owner.Hex()
		parameters["anchor_data_keccak256"] = crypto.Keccak256Hash(self.Anchor).Hex()
	default:
		return nil, fmt.Errorf("unknown validator evidence action %q", actionID)
	}
	return parameters, nil
}

// Recovery must authenticate the signed transaction, not merely the action
// label stored beside it. The owner's nonce is independent of the deployer's.
func validateValidatorEvidenceAnchorTransactionFields(action Action, signer common.Address, to *common.Address, value *big.Int, data []byte) error {
	ownerText, coordinatorText, evidenceText := action.Parameters["anchor_owner"], action.Parameters["coordinator"], action.Parameters["validator_evidence"]
	if action.ID != validatorEvidenceAnchorActionID || action.Kind != "evm-transaction" || !common.IsHexAddress(ownerText) || !common.IsHexAddress(coordinatorText) || !common.IsHexAddress(evidenceText) {
		return errors.New("validator evidence anchor transaction authority is incomplete")
	}
	owner, coordinator, evidence := common.HexToAddress(ownerText), common.HexToAddress(coordinatorText), common.HexToAddress(evidenceText)
	if owner == (common.Address{}) || signer != owner || coordinator == (common.Address{}) || evidence == (common.Address{}) || coordinator == evidence || action.Target != coordinator.Hex() || to == nil || *to != coordinator || value == nil || value.Sign() != 0 {
		return errors.New("validator evidence anchor signed transaction differs from approved owner, target or value")
	}
	if !bytes.Equal(data, stabi.NewSTCoordinator().PackFixValidatorEvidence(evidence)) || crypto.Keccak256Hash(data).Hex() != action.Parameters["anchor_data_keccak256"] {
		return errors.New("validator evidence anchor signed calldata differs from approval")
	}
	return nil
}

// The revision builder supplies the authenticated prior approval. A newly
// built candidate is not history. Relocation needs a separate retained-source
// proof path before an approval may authorize any new CREATE.
func validateValidatorEvidenceRevision(prior *SetupPlan, next *ValidatorEvidenceDeployment) error {
	if prior == nil {
		return errors.New("validator evidence revision prior approval is unavailable")
	}
	if !planUsesValidatorEvidenceEnvelope(prior.Schema) {
		return nil
	}
	if prior.ValidatorEvidence == nil || next == nil || *prior.ValidatorEvidence != *next {
		return errors.New("validator evidence revision requires authenticated existing-companion carry before relocation")
	}
	return nil
}

// Getter responses are fixed ABI words, compared byte-for-byte, so nonzero
// padding and trailing data cannot masquerade as the approved contract state.
func validatorEvidenceDeploymentGetters(manifest ValidatorEvidenceDeployment) []struct {
	address  common.Address
	data     []byte
	expected []byte
} {
	contract := stabi.NewSTValidatorEvidence()
	return []struct {
		address  common.Address
		data     []byte
		expected []byte
	}{
		{address: manifest.Address, data: contract.PackCoordinator(), expected: abiWordAddress(manifest.Coordinator)},
		{address: manifest.Address, data: contract.PackSettlementVault(), expected: abiWordAddress(manifest.SettlementVault)},
		{address: manifest.Address, data: contract.PackChainId(), expected: abiWordUint(manifest.ChainID)},
		{address: manifest.Address, data: contract.PackNetuid(), expected: abiWordUint(uint64(manifest.Netuid))},
		{address: manifest.Address, data: contract.PackGenesisHash(), expected: bytes.Clone(manifest.GenesisHash[:])},
		{address: manifest.Address, data: contract.PackDeploymentIdHash(), expected: bytes.Clone(manifest.DeploymentIDHash[:])},
	}
}

// A completed correction keeps the original batcher CREATE. Its signed old
// upgrade, together with the retained companion source, names that predecessor;
// the corrected implementation and the next free nonce are separate identities.
func validatorEvidencePredecessorNonce(plan *SetupPlan) (uint64, error) {
	if plan == nil {
		return 0, errors.New("validator evidence predecessor plan is unavailable")
	}
	upgrade := plan.CoordinatorUpgrade
	if plan.CoordinatorRepairCarry != nil {
		if plan.ValidatorEvidenceCarry == nil {
			return 0, errors.New("coordinator repair predecessor has no retained companion authority")
		}
		if err := validateCoordinatorRepairCarryPlan(plan); err != nil {
			return 0, err
		}
		upgrade = plan.CoordinatorRepairCarry.Request.Request.OldUpgrade
		if err := validateCoordinatorUpgradeIdentity(upgrade, common.HexToAddress(plan.Roles.Deployer), plan.Deployment); err != nil {
			return 0, err
		}
	}
	if upgrade.DeployerNonce >= ^uint64(0)-1 {
		return 0, errors.New("validator evidence plan nonce range overflows")
	}
	return upgrade.DeployerNonce + 1, nil
}

// Reconstructs from approved public authority, not from the companion's own
// claimed fields. It does not read wallets, contact RPC or allocate nonces.
func validatorEvidencePayloadsForPlan(plan *SetupPlan) (*validatorEvidenceDeploymentPayloads, error) {
	if plan == nil || !common.IsHexAddress(plan.Roles.Deployer) || !common.IsHexAddress(plan.Roles.Owner) {
		return nil, errors.New("validator evidence plan signer authority is unavailable")
	}
	historical := plan.validatorEvidenceHistorical || plan.ValidatorEvidenceCarry != nil
	if err := validateValidatorEvidenceSource(plan, historical); err != nil {
		return nil, err
	}
	deployer := common.HexToAddress(plan.Roles.Deployer)
	if err := validateCoordinatorUpgradeIdentity(plan.CoordinatorUpgrade, deployer, plan.Deployment); err != nil {
		return nil, err
	}
	fleetNonce, err := validatorEvidencePredecessorNonce(plan)
	if err != nil {
		return nil, err
	}
	genesis, err := decodeHex32("validator evidence plan genesis", plan.GenesisHash)
	if err != nil {
		return nil, err
	}
	payloads := &DeploymentPayloads{
		Deployer: deployer, Manifest: plan.Deployment, CoordinatorUpgrade: plan.CoordinatorUpgrade,
		PrecompileProbeAddress: effectivePrecompileProbe(plan.Deployment, plan.CoordinatorUpgradeBaseline),
		FleetBatcherNonce:      fleetNonce, FleetBatcherAddress: crypto.CreateAddress(deployer, fleetNonce),
	}
	nonce := fleetNonce + 1
	if plan.ValidatorEvidenceCarry != nil {
		if err := validateValidatorEvidenceCarryShape(plan); err != nil {
			return nil, err
		}
		if plan.ValidatorEvidence == nil {
			return nil, errors.New("validator evidence carry manifest is unavailable")
		}
		nonce = plan.ValidatorEvidence.DeployerNonce
	}
	return buildValidatorEvidenceDeploymentFromArtifact(payloads, plan.ChainID, genesis, plan.Netuid, nonce, plan.ValidatorEvidenceSource.Artifact)
}

// Historical approvals remain readable but cannot carry a half-upgraded
// companion. Current approvals require both exact actions and the startup
// dependency on the verified one-time anchor.
func validateValidatorEvidencePlan(plan *SetupPlan) error {
	if plan == nil {
		return errors.New("validator evidence plan is unavailable")
	}
	if !planUsesValidatorEvidenceEnvelope(plan.Schema) {
		if plan.ValidatorEvidence != nil || plan.ValidatorEvidenceSource != nil || plan.ValidatorEvidenceCarry != nil {
			return errors.New("legacy plan unexpectedly carries validator evidence")
		}
		for _, action := range plan.Actions {
			if action.ID == validatorEvidenceDeployActionID || action.ID == validatorEvidenceAnchorActionID {
				return errors.New("legacy plan unexpectedly carries validator evidence actions")
			}
		}
		return nil
	}
	expected, err := validatorEvidencePayloadsForPlan(plan)
	if err != nil {
		return err
	}
	if err := validateValidatorEvidenceDeployment(plan.ValidatorEvidence, expected); err != nil {
		return err
	}
	actions := map[string]Action{}
	for _, action := range plan.Actions {
		if _, duplicate := actions[action.ID]; duplicate {
			return errors.New("validator evidence plan has duplicate actions")
		}
		actions[action.ID] = action
	}
	for _, actionID := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		action, exists := actions[actionID]
		if !exists || action.Kind != "evm-transaction" || len(action.AcceptedPriorIntentHashes) != 0 {
			return fmt.Errorf("validator evidence plan is missing the exact %s transaction", actionID)
		}
		parameters, err := expected.actionParameters(actionID, common.HexToAddress(plan.Roles.Owner))
		if err != nil {
			return err
		}
		for key, want := range parameters {
			if action.Parameters[key] != want {
				return fmt.Errorf("validator evidence action %s does not bind %s", actionID, key)
			}
		}
		target := expected.Manifest.Address
		dependencies := []string{"fleet.refresh.deploy-batcher", "evm.coordinator-upgrade-activate"}
		if actionID == validatorEvidenceAnchorActionID {
			target = expected.Manifest.Coordinator
			dependencies = []string{validatorEvidenceDeployActionID, "evm.fund-owner"}
		}
		if action.Target != target.Hex() {
			return fmt.Errorf("validator evidence action %s target differs from the approved graph", actionID)
		}
		for _, dependency := range dependencies {
			if _, exists := actions[dependency]; !exists || !slices.Contains(action.DependsOn, dependency) {
				return fmt.Errorf("validator evidence action %s is missing dependency %s", actionID, dependency)
			}
		}
	}
	batcher, exists := actions["fleet.refresh.deploy-batcher"]
	batcherNonce, err := validatorEvidencePredecessorNonce(plan)
	if err != nil {
		return err
	}
	if !exists || batcher.Target != crypto.CreateAddress(expected.Manifest.Deployer, batcherNonce).Hex() ||
		batcher.Parameters["expected_nonce"] != strconv.FormatUint(batcherNonce, 10) ||
		batcher.Parameters["expected_signer"] != expected.Manifest.Deployer.Hex() || !slices.Contains(batcher.DependsOn, "evm.coordinator-upgrade-implementation") {
		return errors.New("validator evidence plan has a different predecessor CREATE")
	}
	render, exists := actions["config.render"]
	if !exists || !slices.Contains(render.DependsOn, validatorEvidenceAnchorActionID) {
		return errors.New("runtime rendering is not gated by the validator evidence anchor")
	}
	return nil
}

// Revisions before installation must carry the companion through the same
// explicitly approved nonce rebind. An already fixed foreign journal is never
// replaced: the live anchor precondition rejects that transition.
func rebindValidatorEvidencePlan(plan *SetupPlan) error {
	if plan == nil {
		return errors.New("validator evidence revision is unavailable")
	}
	if !planUsesValidatorEvidenceEnvelope(plan.Schema) {
		return nil
	}
	if plan.ValidatorEvidenceCarry != nil {
		// The approved original actions and their spend remain byte-identical.
		// Only surrounding upgrade/batcher actions receive new envelopes.
		return validateValidatorEvidencePlan(plan)
	}
	payloads, err := validatorEvidencePayloadsForPlan(plan)
	if err != nil {
		return err
	}
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID != validatorEvidenceDeployActionID && action.ID != validatorEvidenceAnchorActionID {
			continue
		}
		parameters, err := payloads.actionParameters(action.ID, common.HexToAddress(plan.Roles.Owner))
		if err != nil {
			return err
		}
		action.Parameters = cloneStrings(action.Parameters)
		for key, value := range parameters {
			action.Parameters[key] = value
		}
		action.Target = payloads.Manifest.Address.Hex()
		if action.ID == validatorEvidenceAnchorActionID {
			action.Target = payloads.Manifest.Coordinator.Hex()
		}
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return err
		}
	}
	manifest := payloads.Manifest
	plan.ValidatorEvidence = &manifest
	return validateValidatorEvidencePlan(plan)
}
