package main

// final_semantic_deployment_anchor.go closes the release bytecode trust path:
// the approved plan, canonical release lock, captured deployment artifact, and
// pinned public runtime census must describe one exact executable graph.

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/ss58"
)

// Records one reviewed executable together with its concrete deployed hash.
// The first hash is the immutable release-build reference; the second is the
// fully linked runtime expected at the named address.
type FinalReleaseRuntimeRoot struct {
	Name               string `json:"name"`
	Address            string `json:"address"`
	RuntimeCodeHash    string `json:"runtime_code_hash"`
	ReleaseRuntimeHash string `json:"release_runtime_hash"`
}

// Describes how an approved plan field maps to a reviewed release-build key.
// The stable order is also the external evidence order, avoiding map-derived
// ambiguity in the signed final object.
type finalReleaseRuntimeRootSpec struct {
	name           string
	releaseLockKey string
}

// Mirrors the terminal observation wire without treating its runtime map as
// optional metadata. Keeping this named lets the lock/plan verifier consume
// the same decoded bytes as custody validation.
type finalContractDeploymentArtifact struct {
	Deployment                   ContractDeployment  `json:"deployment"`
	Upgrade                      CoordinatorUpgrade  `json:"upgrade"`
	Terminal                     ChainHead           `json:"terminal"`
	RuntimeCodeHashes            map[string]string   `json:"runtime_code_hashes"`
	Policy                       PolicyView          `json:"policy"`
	Custody                      ContractCustodyView `json:"custody"`
	PlanHash                     string              `json:"plan_hash"`
	PlanDefaultMinTransferTaoRao uint64              `json:"plan_default_min_transfer_rao"`
	ExpectedGuardian             string              `json:"expected_guardian"`
	ExpectedCommitmentOracle     string              `json:"expected_commitment_oracle"`
}

// Lists every executable role that the release review seals into final proof.
var finalReleaseRuntimeRootSpecs = []finalReleaseRuntimeRootSpec{
	{name: "coordinator_bootstrap_implementation", releaseLockKey: "coordinator_implementation_runtime_hash"},
	{name: "coordinator_proxy", releaseLockKey: "coordinator_proxy_runtime_hash"},
	{name: "coordinator_upgrade_implementation", releaseLockKey: "coordinator_implementation_runtime_hash"},
	{name: "fleet_batcher", releaseLockKey: "fleet_batcher_runtime_hash"},
	{name: "governance_drill_implementation", releaseLockKey: "governance_drill_implementation_runtime_hash"},
	{name: "precompile_probe", releaseLockKey: "precompile_probe_runtime_hash"},
	{name: "reserve_sink", releaseLockKey: "reserve_sink_runtime_hash"},
	{name: "settlement_vault", releaseLockKey: "settlement_vault_runtime_hash"},
}

// Finds one sealed executable after rejecting a malformed or duplicate root
// census. Consumers such as the generation replay use this rather than
// independently reconstructing an address from mutable presentation fields.
func finalReleaseRuntimeRootByName(evidence *FinalSemanticEvidence, name string) (FinalReleaseRuntimeRoot, error) {
	if evidence == nil {
		return FinalReleaseRuntimeRoot{}, errors.New("release runtime root evidence is unavailable")
	}
	if err := verifyFinalReleaseRuntimeRootsShape(evidence.Deployment); err != nil {
		return FinalReleaseRuntimeRoot{}, err
	}
	for _, root := range evidence.Deployment.RuntimeRoots {
		if root.Name == name {
			return root, nil
		}
	}
	return FinalReleaseRuntimeRoot{}, fmt.Errorf("release runtime root %q is absent", name)
}

// Builds the exact runtime census implied by a persisted plan and its
// authenticated release lock. This deliberately rejects a partial map: a
// missing root is indistinguishable from an ignored executable at review time.
func finalReleaseRuntimeRootsForPlan(plan *SetupPlan, lock *ReleaseLock) ([]FinalReleaseRuntimeRoot, error) {
	if plan == nil || lock == nil || plan.Schema != currentSetupPlanSchema {
		return nil, errors.New("approved release plan or lock is unavailable")
	}
	if err := validateReleaseLockStatic(lock); err != nil {
		return nil, fmt.Errorf("validate approved release lock: %w", err)
	}
	lockHash, err := canonicalHashHex(lock)
	if err != nil || !strings.EqualFold(plan.ReleaseLockHash, lockHash) {
		return nil, stateMismatchError(err, "approved plan does not bind the canonical release lock")
	}
	deploymentHashes, err := finalPlanDeploymentRuntimeHashes(plan.Deployment)
	if err != nil {
		return nil, err
	}
	if plan.CoordinatorUpgrade.Implementation == (common.Address{}) || plan.CoordinatorUpgrade.RuntimeCodeHash == "" {
		return nil, errors.New("approved plan has no coordinator upgrade executable")
	}
	if _, err := decodeHex32("approved coordinator upgrade runtime hash", plan.CoordinatorUpgrade.RuntimeCodeHash); err != nil {
		return nil, err
	}
	if err := finalPlanCoordinatorUpgradeActions(plan); err != nil {
		return nil, err
	}
	batcherAddress, batcherHash, err := finalPlanFleetBatcher(plan)
	if err != nil {
		return nil, err
	}
	probeAddress := effectivePrecompileProbe(plan.Deployment, plan.CoordinatorUpgradeBaseline)
	probeHash := deploymentHashes[plan.Deployment.PrecompileProbe]
	if probeAddress != plan.Deployment.PrecompileProbe {
		if plan.CoordinatorUpgradeBaseline.Schema != "urnetwork-coordinator-upgrade-baseline-v4" || plan.CoordinatorUpgradeBaseline.ReplacementPrecompileProbeHash == "" {
			return nil, errors.New("approved plan replacement probe has no reviewed runtime")
		}
		probeHash = plan.CoordinatorUpgradeBaseline.ReplacementPrecompileProbeHash
		if _, err := decodeHex32("approved replacement probe runtime hash", probeHash); err != nil {
			return nil, err
		}
	}
	values := map[string]struct {
		address common.Address
		hash    string
	}{
		"coordinator_bootstrap_implementation": {address: plan.Deployment.CoordinatorImplementation, hash: deploymentHashes[plan.Deployment.CoordinatorImplementation]},
		"coordinator_proxy":                    {address: plan.Deployment.CoordinatorProxy, hash: deploymentHashes[plan.Deployment.CoordinatorProxy]},
		"coordinator_upgrade_implementation":   {address: plan.CoordinatorUpgrade.Implementation, hash: plan.CoordinatorUpgrade.RuntimeCodeHash},
		"fleet_batcher":                        {address: batcherAddress, hash: batcherHash},
		"governance_drill_implementation":      {address: plan.Deployment.GovernanceDrillImplementation, hash: deploymentHashes[plan.Deployment.GovernanceDrillImplementation]},
		"precompile_probe":                     {address: probeAddress, hash: probeHash},
		"reserve_sink":                         {address: plan.Deployment.ReserveSink, hash: deploymentHashes[plan.Deployment.ReserveSink]},
		"settlement_vault":                     {address: plan.Deployment.SettlementVault, hash: deploymentHashes[plan.Deployment.SettlementVault]},
	}
	roots := make([]FinalReleaseRuntimeRoot, 0, len(finalReleaseRuntimeRootSpecs))
	seenAddresses := make(map[common.Address]bool, len(finalReleaseRuntimeRootSpecs))
	for _, spec := range finalReleaseRuntimeRootSpecs {
		value, ok := values[spec.name]
		if !ok || value.address == (common.Address{}) || value.hash == "" || seenAddresses[value.address] {
			return nil, fmt.Errorf("approved plan has an invalid or duplicate %s runtime root", spec.name)
		}
		seenAddresses[value.address] = true
		if _, err := decodeHex32("approved runtime code hash", value.hash); err != nil {
			return nil, fmt.Errorf("approved plan %s: %w", spec.name, err)
		}
		releaseHash, err := finalReleaseLockRuntimeHash(lock, spec.releaseLockKey)
		if err != nil {
			return nil, err
		}
		roots = append(roots, FinalReleaseRuntimeRoot{
			Name:               spec.name,
			Address:            strings.ToLower(value.address.Hex()),
			RuntimeCodeHash:    strings.ToLower(value.hash),
			ReleaseRuntimeHash: releaseHash,
		})
	}
	return roots, nil
}

// Enforces the six immutable manifest entries before the active upgrade and
// batcher are layered on top. Normalization rejects address aliases, and the
// exact cardinality makes unused map entries an error instead of dead data.
func finalPlanDeploymentRuntimeHashes(deployment ContractDeployment) (map[common.Address]string, error) {
	hashes, err := normalizedDeploymentRuntimeHashes(deployment)
	if err != nil {
		return nil, err
	}
	addresses := contractDeploymentAddresses(deployment)
	if len(hashes) != len(addresses) {
		return nil, fmt.Errorf("approved deployment runtime hash count=%d, want %d", len(hashes), len(addresses))
	}
	for _, address := range addresses {
		if address == (common.Address{}) || hashes[address] == "" {
			return nil, fmt.Errorf("approved deployment runtime hash for %s is absent", address.Hex())
		}
	}
	return hashes, nil
}

// Checks both the CREATE record and the activation call so a later evidence
// object cannot rename a different implementation as the approved upgrade.
func finalPlanCoordinatorUpgradeActions(plan *SetupPlan) error {
	if plan == nil {
		return errors.New("approved plan is unavailable")
	}
	var implementation *Action
	var activation *Action
	for index := range plan.Actions {
		action := &plan.Actions[index]
		switch action.ID {
		case "evm.coordinator-upgrade-implementation":
			if implementation != nil {
				return errors.New("approved plan duplicates the coordinator upgrade implementation action")
			}
			implementation = action
		case "evm.coordinator-upgrade-activate":
			if activation != nil {
				return errors.New("approved plan duplicates the coordinator upgrade activation action")
			}
			activation = action
		}
	}
	if implementation == nil || activation == nil || !common.IsHexAddress(implementation.Target) || !common.IsHexAddress(activation.Target) ||
		common.HexToAddress(implementation.Target) != plan.CoordinatorUpgrade.Implementation || common.HexToAddress(activation.Target) != plan.Deployment.CoordinatorProxy ||
		!strings.EqualFold(implementation.Parameters["runtime_code_hash"], plan.CoordinatorUpgrade.RuntimeCodeHash) ||
		!strings.EqualFold(activation.Parameters["implementation"], plan.CoordinatorUpgrade.Implementation.Hex()) ||
		!strings.EqualFold(activation.Parameters["runtime_code_hash"], plan.CoordinatorUpgrade.RuntimeCodeHash) {
		return errors.New("approved plan coordinator upgrade address or runtime hash differs from its exact actions")
	}
	return nil
}

// Extracts the one deployment helper that commits every fleet generation.
// Its exact three-field constructor binding is required even when ordinary
// lifecycle writes later happen through only a subset of its entry points.
func finalPlanFleetBatcher(plan *SetupPlan) (common.Address, string, error) {
	if plan == nil {
		return common.Address{}, "", errors.New("approved plan is unavailable")
	}
	var action *Action
	for index := range plan.Actions {
		if plan.Actions[index].ID == "fleet.refresh.deploy-batcher" {
			if action != nil {
				return common.Address{}, "", errors.New("approved plan duplicates the fleet batcher action")
			}
			action = &plan.Actions[index]
		}
	}
	if action == nil || !common.IsHexAddress(action.Target) ||
		!strings.EqualFold(action.Parameters["coordinator"], plan.Deployment.CoordinatorProxy.Hex()) ||
		!common.IsHexAddress(action.Parameters["commitment_oracle"]) || !strings.EqualFold(action.Parameters["commitment_oracle"], plan.Roles.CommitmentOracle) {
		return common.Address{}, "", errors.New("approved plan fleet batcher constructor binding is incomplete")
	}
	runtimeHash := action.Parameters["runtime_code_hash"]
	if _, err := decodeHex32("approved fleet batcher runtime hash", runtimeHash); err != nil {
		return common.Address{}, "", err
	}
	return common.HexToAddress(action.Target), strings.ToLower(runtimeHash), nil
}

// Reads one typed runtime digest from the static lock after rejecting aliases,
// unknown fields, and noncanonical digest encodings at the lock boundary.
func finalReleaseLockRuntimeHash(lock *ReleaseLock, key string) (string, error) {
	if lock == nil {
		return "", errors.New("approved release lock is unavailable")
	}
	value, err := lockString(lock.EVMBuild, key)
	if err != nil {
		return "", err
	}
	if _, err := decodeHex32("reviewed release runtime hash", value); err != nil {
		return "", err
	}
	return strings.ToLower(value), nil
}

// Locates the generated contract body for a named lock digest. The digest is
// an unlinked review reference; deployment-specific immutable words are
// reconstructed separately before any observed on-chain code is accepted.
func finalReleaseRuntimeArtifact(key string) (ContractArtifact, error) {
	switch key {
	case "coordinator_implementation_runtime_hash":
		return artifactByName("Coordinator"), nil
	case "coordinator_proxy_runtime_hash":
		return artifactByName("ERC1967Proxy"), nil
	case "fleet_batcher_runtime_hash":
		return TestnetFleetBatcherArtifact, nil
	case "governance_drill_implementation_runtime_hash":
		return TestnetGovernanceDrillArtifact, nil
	case "precompile_probe_runtime_hash":
		return TestnetPrecompileProbeArtifact, nil
	case "reserve_sink_runtime_hash":
		return artifactByName("ReserveSink"), nil
	case "settlement_vault_runtime_hash":
		return artifactByName("SettlementVault"), nil
	default:
		return ContractArtifact{}, fmt.Errorf("release runtime lock key %q has no generated artifact", key)
	}
}

// Recomputes every reviewed unlinked runtime hash from frozen generated
// contract bodies. Static lock syntax alone cannot prove that its digest names
// the executable generated by the release source.
func verifyFinalReleaseLockRuntimeBuild(lock *ReleaseLock) error {
	if lock == nil {
		return errors.New("approved release lock is unavailable")
	}
	seen := map[string]bool{}
	for _, spec := range finalReleaseRuntimeRootSpecs {
		if seen[spec.releaseLockKey] {
			continue
		}
		seen[spec.releaseLockKey] = true
		artifact, err := finalReleaseRuntimeArtifact(spec.releaseLockKey)
		if err != nil {
			return err
		}
		locked, err := finalReleaseLockRuntimeHash(lock, spec.releaseLockKey)
		if err != nil {
			return err
		}
		expected := strings.ToLower(crypto.Keccak256Hash(hexBytes(artifact.RuntimeBytecode)).Hex())
		if !strings.EqualFold(locked, expected) {
			return fmt.Errorf("approved release lock runtime %s differs from generated %s", spec.releaseLockKey, artifact.Name)
		}
	}
	return nil
}

// Recreates one fully linked runtime body and returns its exact EVM code hash.
// Wrapping the immutable substitution prevents a caller from validating a
// plan's self-reported hash without ever inspecting the reviewed bytecode.
func finalLinkedRuntimeHash(label string, artifact ContractArtifact, values map[string][]byte) (string, error) {
	runtime, err := runtimeWithImmutables(artifact, values)
	if err != nil {
		return "", fmt.Errorf("link %s runtime: %w", label, err)
	}
	return strings.ToLower(crypto.Keccak256Hash(runtime).Hex()), nil
}

// Derives the complete expected deployed census from the frozen source bodies
// and the exact plan/evidence immutable inputs. This is intentionally stronger
// than comparing self-consistent maps: replacing code in every evidence layer
// still fails unless it is the reviewed release executable linked at these
// exact addresses and roles.
func finalExpectedReleaseRuntimeRoots(plan *SetupPlan, evidence *FinalSemanticEvidence, lock *ReleaseLock) ([]FinalReleaseRuntimeRoot, error) {
	if plan == nil || evidence == nil || lock == nil || plan.Netuid != evidence.Netuid || !common.IsHexAddress(plan.Roles.Deployer) {
		return nil, errors.New("approved runtime reconstruction inputs are incomplete")
	}
	if err := verifyFinalReleaseLockRuntimeBuild(lock); err != nil {
		return nil, err
	}
	roots, err := finalReleaseRuntimeRootsForPlan(plan, lock)
	if err != nil {
		return nil, err
	}
	reserveHotkey, err := decodeHex32("signed reserve hotkey", evidence.Deployment.ReserveHotkey)
	if err != nil {
		return nil, err
	}
	escrowHotkey, err := decodeHex32("signed vault escrow hotkey", evidence.Deployment.VaultEscrowHotkey)
	if err != nil {
		return nil, err
	}
	deployer := common.HexToAddress(plan.Roles.Deployer)
	batcherAddress, _, err := finalPlanFleetBatcher(plan)
	if err != nil {
		return nil, err
	}
	reserveSelf := ss58.EvmMirrorPubkey(plan.Deployment.ReserveSink)
	vaultSelf := ss58.EvmMirrorPubkey(plan.Deployment.SettlementVault)
	hashes := map[string]string{}
	if hashes["coordinator_bootstrap_implementation"], err = finalLinkedRuntimeHash("coordinator bootstrap", artifactByName("Coordinator"), map[string][]byte{"__self": abiWordAddress(plan.Deployment.CoordinatorImplementation)}); err != nil {
		return nil, err
	}
	if hashes["coordinator_proxy"], err = finalLinkedRuntimeHash("coordinator proxy", artifactByName("ERC1967Proxy"), nil); err != nil {
		return nil, err
	}
	if hashes["coordinator_upgrade_implementation"], err = finalLinkedRuntimeHash("coordinator upgrade", artifactByName("Coordinator"), map[string][]byte{"__self": abiWordAddress(plan.CoordinatorUpgrade.Implementation)}); err != nil {
		return nil, err
	}
	if hashes["fleet_batcher"], err = finalLinkedRuntimeHash("fleet batcher", TestnetFleetBatcherArtifact, map[string][]byte{"coordinator": abiWordAddress(plan.Deployment.CoordinatorProxy), "oracle": abiWordAddress(common.HexToAddress(plan.Roles.CommitmentOracle))}); err != nil {
		return nil, err
	}
	if hashes["governance_drill_implementation"], err = finalLinkedRuntimeHash("governance drill", TestnetGovernanceDrillArtifact, map[string][]byte{"__self": abiWordAddress(plan.Deployment.GovernanceDrillImplementation)}); err != nil {
		return nil, err
	}
	if hashes["precompile_probe"], err = finalLinkedRuntimeHash("precompile probe", TestnetPrecompileProbeArtifact, map[string][]byte{"owner": abiWordAddress(deployer), "netuid": abiWordUint(uint64(plan.Netuid))}); err != nil {
		return nil, err
	}
	if hashes["reserve_sink"], err = finalLinkedRuntimeHash("reserve sink", artifactByName("ReserveSink"), map[string][]byte{"netuid": abiWordUint(uint64(plan.Netuid)), "reserveHotkey": reserveHotkey[:], "selfColdkey": reserveSelf[:], "bootstrap": abiWordAddress(deployer)}); err != nil {
		return nil, err
	}
	if hashes["settlement_vault"], err = finalLinkedRuntimeHash("settlement vault", artifactByName("SettlementVault"), map[string][]byte{"netuid": abiWordUint(uint64(plan.Netuid)), "escrowHotkey": escrowHotkey[:], "selfColdkey": vaultSelf[:], "minimumClaimTTLBlocks": abiWordUint(evidence.Deployment.VaultMinimumClaimTTLBlocks), "minimumTransferTaoRao": abiWordUint(plan.LiveFacts.DefaultMinTransferRao), "bootstrap": abiWordAddress(deployer)}); err != nil {
		return nil, err
	}
	for index := range roots {
		root := &roots[index]
		if root.Address == strings.ToLower(batcherAddress.Hex()) && root.Name != "fleet_batcher" || root.Name == "fleet_batcher" && root.Address != strings.ToLower(batcherAddress.Hex()) {
			return nil, errors.New("approved fleet batcher address differs from the runtime census")
		}
		got, ok := hashes[root.Name]
		if !ok || !strings.EqualFold(root.RuntimeCodeHash, got) {
			return nil, fmt.Errorf("approved runtime root %s differs from exact linked release bytecode", root.Name)
		}
	}
	return roots, nil
}

// Verifies the immutable YAML object itself, then ties its semantic hash to
// the plan's approval field. A locator alone is insufficient because a
// content-addressed but unrelated lock must not authorize an executable.
func verifyFinalReleaseLockArtifact(evidence *FinalSemanticEvidence, plan *SetupPlan, data []byte) (*ReleaseLock, error) {
	if evidence == nil || plan == nil || len(data) == 0 {
		return nil, errors.New("approved release lock artifact is unavailable")
	}
	lock, err := decodeReleaseLockBytes(data)
	if err != nil {
		return nil, fmt.Errorf("decode approved release lock artifact: %w", err)
	}
	canonical, err := canonicalReleaseLockBytes(lock)
	if err != nil || !bytes.Equal(canonical, data) {
		return nil, stateMismatchError(err, "approved release lock artifact is not canonical")
	}
	lockHash, err := canonicalHashHex(lock)
	if err != nil || !strings.EqualFold(plan.ReleaseLockHash, lockHash) {
		return nil, stateMismatchError(err, "approved release lock artifact differs from the decoded plan")
	}
	return lock, nil
}

// Validates the public evidence projection before any artifact bytes are
// loaded. The fixed name/order census makes missing, extra, swapped, and
// casing-only roots fail before a reader can accidentally ignore one.
func verifyFinalReleaseRuntimeRootsShape(deployment FinalContractDeploymentEvidence) error {
	if len(deployment.RuntimeRoots) != len(finalReleaseRuntimeRootSpecs) {
		return fmt.Errorf("release runtime root count=%d, want %d", len(deployment.RuntimeRoots), len(finalReleaseRuntimeRootSpecs))
	}
	seenAddresses := map[string]bool{}
	for index, spec := range finalReleaseRuntimeRootSpecs {
		root := deployment.RuntimeRoots[index]
		if root.Name != spec.name || root.Address != strings.ToLower(root.Address) || root.RuntimeCodeHash != strings.ToLower(root.RuntimeCodeHash) || root.ReleaseRuntimeHash != strings.ToLower(root.ReleaseRuntimeHash) {
			return fmt.Errorf("release runtime root %d is not canonical", index)
		}
		if err := requireFinalEVMAddress("release runtime root", root.Address); err != nil {
			return err
		}
		if err := requireFinalHex32("release runtime code hash", root.RuntimeCodeHash); err != nil {
			return err
		}
		if err := requireFinalHex32("reviewed release runtime hash", root.ReleaseRuntimeHash); err != nil {
			return err
		}
		if seenAddresses[root.Address] {
			return fmt.Errorf("release runtime root %s repeats address %s", root.Name, root.Address)
		}
		seenAddresses[root.Address] = true
	}
	legacy := map[string]struct {
		address string
		hash    string
	}{
		"coordinator_proxy":                  {address: deployment.CoordinatorProxy, hash: deployment.CoordinatorProxyCodeHash},
		"coordinator_upgrade_implementation": {address: deployment.CoordinatorImplementation, hash: deployment.ImplementationCodeHash},
		"settlement_vault":                   {address: deployment.SettlementVault, hash: deployment.SettlementVaultCodeHash},
		"reserve_sink":                       {address: deployment.ReserveSink, hash: deployment.ReserveSinkCodeHash},
	}
	for _, root := range deployment.RuntimeRoots {
		if expected, ok := legacy[root.Name]; ok && (root.Address != strings.ToLower(expected.address) || root.RuntimeCodeHash != strings.ToLower(expected.hash)) {
			return fmt.Errorf("release runtime root %s differs from the signed deployment projection", root.Name)
		}
	}
	return nil
}

// Compares the final evidence to both authenticated inputs and the raw
// terminal observation. Every runtime map is normalized before comparison so
// an address alias cannot hide an omitted or substituted executable.
func verifyFinalDeploymentRuntimeAnchors(evidence *FinalSemanticEvidence, plan *SetupPlan, lock *ReleaseLock, artifact finalContractDeploymentArtifact) error {
	if evidence == nil || plan == nil || lock == nil {
		return errors.New("release runtime anchor inputs are unavailable")
	}
	if err := verifyFinalReleaseRuntimeRootsShape(evidence.Deployment); err != nil {
		return err
	}
	expectedRoots, err := finalExpectedReleaseRuntimeRoots(plan, evidence, lock)
	if err != nil {
		return err
	}
	if !finalReleaseRuntimeRootsEqual(evidence.Deployment.RuntimeRoots, expectedRoots) {
		return errors.New("signed deployment runtime roots differ from the approved plan and release lock")
	}
	if artifact.Deployment.DeploymentID != plan.Deployment.DeploymentID || artifact.Deployment.InitialNonce != plan.Deployment.InitialNonce ||
		artifact.Deployment.RegistrationRoleGeneration != plan.Deployment.RegistrationRoleGeneration || !contractDeploymentAddressesEqual(artifact.Deployment, plan.Deployment) {
		return errors.New("contract deployment artifact differs from the approved deployment identity")
	}
	artifactDeploymentHashes, err := finalPlanDeploymentRuntimeHashes(artifact.Deployment)
	if err != nil {
		return fmt.Errorf("contract deployment artifact runtime map: %w", err)
	}
	planDeploymentHashes, err := finalPlanDeploymentRuntimeHashes(plan.Deployment)
	if err != nil {
		return err
	}
	if !finalRuntimeHashMapsEqual(artifactDeploymentHashes, planDeploymentHashes) || artifact.Upgrade != plan.CoordinatorUpgrade {
		return errors.New("contract deployment artifact runtime map or coordinator upgrade differs from the approved plan")
	}
	observedHashes, err := finalNormalizedRuntimeCodeHashes(artifact.RuntimeCodeHashes)
	if err != nil {
		return fmt.Errorf("contract deployment artifact terminal runtime map: %w", err)
	}
	expectedHashes, err := finalReleaseRuntimeHashMap(expectedRoots)
	if err != nil {
		return err
	}
	if !finalRuntimeHashMapsEqual(observedHashes, expectedHashes) {
		return errors.New("contract deployment artifact terminal runtime map omits, adds, or substitutes a reviewed executable")
	}
	return nil
}

// Converts a string-keyed observation without allowing duplicate case aliases
// for the same EVM address.
func finalNormalizedRuntimeCodeHashes(hashes map[string]string) (map[common.Address]string, error) {
	result := make(map[common.Address]string, len(hashes))
	for addressText, hash := range hashes {
		if !common.IsHexAddress(addressText) {
			return nil, fmt.Errorf("runtime observation has invalid address %q", addressText)
		}
		address := common.HexToAddress(addressText)
		if _, duplicate := result[address]; duplicate {
			return nil, fmt.Errorf("runtime observation duplicates address %s", address.Hex())
		}
		if _, err := decodeHex32("observed runtime code hash", hash); err != nil {
			return nil, err
		}
		result[address] = strings.ToLower(hash)
	}
	return result, nil
}

// Projects the sealed root list into the exact address-keyed form used by the
// raw terminal observation artifact.
func finalReleaseRuntimeHashMap(roots []FinalReleaseRuntimeRoot) (map[common.Address]string, error) {
	result := make(map[common.Address]string, len(roots))
	for _, root := range roots {
		if !common.IsHexAddress(root.Address) {
			return nil, fmt.Errorf("release runtime root %s has invalid address", root.Name)
		}
		address := common.HexToAddress(root.Address)
		if _, duplicate := result[address]; duplicate {
			return nil, fmt.Errorf("release runtime root %s repeats address", root.Name)
		}
		result[address] = strings.ToLower(root.RuntimeCodeHash)
	}
	return result, nil
}

// Renders the checked root census in the same lowercase-keyed wire shape as
// the captured contract-deployment artifact.
func finalReleaseRuntimeHashStrings(roots []FinalReleaseRuntimeRoot) (map[string]string, error) {
	addresses, err := finalReleaseRuntimeHashMap(roots)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(addresses))
	for address, hash := range addresses {
		result[strings.ToLower(address.Hex())] = hash
	}
	return result, nil
}

// Preserves exact address/hash membership without relying on map iteration.
func finalRuntimeHashMapsEqual(left, right map[common.Address]string) bool {
	if len(left) != len(right) {
		return false
	}
	for address, hash := range left {
		if !strings.EqualFold(hash, right[address]) {
			return false
		}
	}
	return true
}

// Uses the evidence's signed order as the comparison domain while preserving
// a small equality helper for tests that intentionally rebuild one census.
func finalReleaseRuntimeRootsEqual(left, right []FinalReleaseRuntimeRoot) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
