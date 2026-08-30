package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

type ContractDeployment struct {
	Schema                        string            `json:"schema"`
	DeploymentID                  string            `json:"deployment_id"`
	InitialNonce                  uint64            `json:"initial_nonce"`
	RegistrationRoleGeneration    uint64            `json:"registration_role_generation,omitempty"`
	ReserveSink                   common.Address    `json:"reserve_sink"`
	SettlementVault               common.Address    `json:"settlement_vault"`
	CoordinatorImplementation     common.Address    `json:"coordinator_implementation"`
	CoordinatorProxy              common.Address    `json:"coordinator_proxy"`
	GovernanceDrillImplementation common.Address    `json:"governance_drill_implementation"`
	PrecompileProbe               common.Address    `json:"precompile_probe"`
	DeployBlock                   uint64            `json:"deploy_block,omitempty"`
	DeployBlockHash               string            `json:"deploy_block_hash,omitempty"`
	RuntimeHashes                 map[string]string `json:"runtime_hashes,omitempty"`
}

// CoordinatorUpgrade is deliberately separate from the immutable deployment
// identity. Release revisions can therefore retain and audit an already-live
// sink, vault, proxy, and original implementation while approving one exact
// additive UUPS implementation at the next deterministic deployer nonce.
type CoordinatorUpgrade struct {
	Schema          string         `json:"schema"`
	DeploymentID    string         `json:"deployment_id"`
	Implementation  common.Address `json:"implementation"`
	DeployerNonce   uint64         `json:"deployer_nonce"`
	RuntimeCodeHash string         `json:"runtime_code_hash"`
}

// CoordinatorUpgradeBaseline records the exact, finalized compatibility proof
// used when an interrupted deployment is retained across a locked release
// change. The sink and vault executable hashes exclude constructor immutable
// words and Solidity metadata; every full runtime hash remains bound by the
// authenticated prior/rebound deployment manifests.
type CoordinatorUpgradeBaseline struct {
	Schema                        string `json:"schema"`
	PriorDeploymentHash           string `json:"prior_deployment_hash"`
	ReleaseDeploymentHash         string `json:"release_deployment_hash"`
	ReboundDeploymentHash         string `json:"rebound_deployment_hash"`
	ReserveSinkExecutableHash     string `json:"reserve_sink_executable_hash"`
	SettlementVaultExecutableHash string `json:"settlement_vault_executable_hash"`
	GovernanceDrillVersion        string `json:"governance_drill_version"`
	GovernanceProxiableUUID       string `json:"governance_proxiable_uuid"`
	DeployerNonce                 uint64 `json:"deployer_nonce"`
	ProbeAddressEmpty             bool   `json:"probe_address_empty"`
	ActiveImplementation          string `json:"active_implementation,omitempty"`
	ActiveImplementationHash      string `json:"active_implementation_runtime_hash,omitempty"`
	PrecompileProbeExecutableHash string `json:"precompile_probe_executable_hash,omitempty"`
	FinalizedBlock                uint64 `json:"finalized_block"`
	FinalizedBlockHash            string `json:"finalized_block_hash"`
}

type DeploymentPayloads struct {
	Deployer                                                                                                                          common.Address
	Manifest                                                                                                                          ContractDeployment
	CoordinatorUpgrade                                                                                                                CoordinatorUpgrade
	Reserve, Vault, Implementation, RegisterEscrow, Proxy, GovernanceDrill, FixVault, FixSink, PrecompileProbe, UpgradeImplementation []byte
	ExpectedRuntime                                                                                                                   map[common.Address][]byte
}

func validateCoordinatorUpgradeIdentity(upgrade CoordinatorUpgrade, deployer common.Address, deployment ContractDeployment) error {
	if (upgrade.Schema != "urnetwork-coordinator-upgrade-v1" && upgrade.Schema != "urnetwork-coordinator-upgrade-v2") || upgrade.DeploymentID != deployment.DeploymentID || deployer == (common.Address{}) || deployment.InitialNonce > ^uint64(0)-9 {
		return errors.New("coordinator upgrade identity is invalid")
	}
	minimumNonce := deployment.InitialNonce + 9
	if upgrade.Schema == "urnetwork-coordinator-upgrade-v1" && upgrade.DeployerNonce != minimumNonce || upgrade.Schema == "urnetwork-coordinator-upgrade-v2" && upgrade.DeployerNonce <= minimumNonce || upgrade.Implementation != crypto.CreateAddress(deployer, upgrade.DeployerNonce) {
		return errors.New("coordinator upgrade is not the approved deterministic CREATE")
	}
	if _, err := decodeHex32("coordinator upgrade runtime hash", upgrade.RuntimeCodeHash); err != nil {
		return err
	}
	return nil
}

func (baseline CoordinatorUpgradeBaseline) isZero() bool {
	return baseline == (CoordinatorUpgradeBaseline{})
}

func validateCoordinatorUpgradeBaseline(baseline CoordinatorUpgradeBaseline, deployment ContractDeployment, upgrade CoordinatorUpgrade) error {
	if baseline.isZero() {
		return nil
	}
	if baseline.Schema != "urnetwork-coordinator-upgrade-baseline-v1" && baseline.Schema != "urnetwork-coordinator-upgrade-baseline-v2" || baseline.FinalizedBlock == 0 || deployment.InitialNonce > ^uint64(0)-9 {
		return errors.New("coordinator upgrade baseline has an invalid identity, checkpoint, or nonce boundary")
	}
	if baseline.Schema == "urnetwork-coordinator-upgrade-baseline-v1" && (!baseline.ProbeAddressEmpty || baseline.DeployerNonce != deployment.InitialNonce+8 || upgrade.Schema != "urnetwork-coordinator-upgrade-v1" || upgrade.DeployerNonce != baseline.DeployerNonce+1) {
		return errors.New("coordinator upgrade baseline has an invalid v1 nonce boundary")
	}
	if baseline.Schema == "urnetwork-coordinator-upgrade-baseline-v2" && (baseline.ProbeAddressEmpty || upgrade.Schema != "urnetwork-coordinator-upgrade-v2" || baseline.DeployerNonce != upgrade.DeployerNonce || !common.IsHexAddress(baseline.ActiveImplementation)) {
		return errors.New("coordinator upgrade baseline has an invalid repeated-upgrade boundary")
	}
	for name, value := range map[string]string{
		"prior deployment hash":            baseline.PriorDeploymentHash,
		"release deployment hash":          baseline.ReleaseDeploymentHash,
		"rebound deployment hash":          baseline.ReboundDeploymentHash,
		"reserve sink executable hash":     baseline.ReserveSinkExecutableHash,
		"settlement vault executable hash": baseline.SettlementVaultExecutableHash,
		"governance drill version":         baseline.GovernanceDrillVersion,
		"governance proxiable UUID":        baseline.GovernanceProxiableUUID,
		"upgrade baseline finalized hash":  baseline.FinalizedBlockHash,
	} {
		if _, err := decodeHex32(name, value); err != nil {
			return err
		}
	}
	if baseline.Schema == "urnetwork-coordinator-upgrade-baseline-v2" {
		for name, value := range map[string]string{
			"active implementation runtime hash": baseline.ActiveImplementationHash,
			"precompile probe executable hash":   baseline.PrecompileProbeExecutableHash,
		} {
			if _, err := decodeHex32(name, value); err != nil {
				return err
			}
		}
	}
	wantVersion := crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex()
	if !strings.EqualFold(baseline.GovernanceDrillVersion, wantVersion) || !strings.EqualFold(baseline.GovernanceProxiableUUID, erc1967ImplementationSlot) {
		return errors.New("coordinator upgrade baseline has an incompatible governance drill implementation")
	}
	reboundHash, err := contractDeploymentIdentityHash(deployment)
	if err != nil || reboundHash != baseline.ReboundDeploymentHash {
		return errors.New("coordinator upgrade baseline does not authenticate the rebound deployment")
	}
	if baseline.Schema == "urnetwork-coordinator-upgrade-baseline-v2" && (baseline.PriorDeploymentHash != reboundHash || common.HexToAddress(baseline.ActiveImplementation) == upgrade.Implementation) {
		return errors.New("repeated coordinator upgrade baseline does not bind a distinct active implementation")
	}
	return nil
}

// Remove observation fields which advance after deployment while retaining
// every address, nonce, and expected runtime hash approved before execution.
func contractDeploymentIdentity(manifest ContractDeployment) ContractDeployment {
	manifest.DeployBlock = 0
	manifest.DeployBlockHash = ""
	return manifest
}

func contractDeploymentIdentityHash(manifest ContractDeployment) (string, error) {
	return canonicalHashHex(contractDeploymentIdentity(manifest))
}

func contractDeploymentAddressesEqual(left, right ContractDeployment) bool {
	return left.Schema == right.Schema && left.DeploymentID == right.DeploymentID && left.InitialNonce == right.InitialNonce && left.ReserveSink == right.ReserveSink && left.SettlementVault == right.SettlementVault && left.CoordinatorImplementation == right.CoordinatorImplementation && left.CoordinatorProxy == right.CoordinatorProxy && left.GovernanceDrillImplementation == right.GovernanceDrillImplementation && left.PrecompileProbe == right.PrecompileProbe
}

// Return contracts in their approved CREATE order, excluding intervening
// calls which consume a deployer nonce but do not create an address.
func contractDeploymentAddresses(manifest ContractDeployment) []common.Address {
	return []common.Address{
		manifest.ReserveSink,
		manifest.SettlementVault,
		manifest.CoordinatorImplementation,
		manifest.CoordinatorProxy,
		manifest.GovernanceDrillImplementation,
		manifest.PrecompileProbe,
	}
}

// Normalize runtime-hash keys to their address value so checksum case cannot
// hide a duplicate or make an otherwise exact observed subset look different.
func normalizedDeploymentRuntimeHashes(manifest ContractDeployment) (map[common.Address]string, error) {
	result := make(map[common.Address]string, len(manifest.RuntimeHashes))
	for addressText, hash := range manifest.RuntimeHashes {
		if !common.IsHexAddress(addressText) {
			return nil, fmt.Errorf("contract deployment has invalid runtime-hash address %q", addressText)
		}
		address := common.HexToAddress(addressText)
		if _, duplicate := result[address]; duplicate {
			return nil, fmt.Errorf("contract deployment has duplicate runtime-hash address %s", address)
		}
		if _, err := decodeHex32("contract runtime hash", hash); err != nil {
			return nil, fmt.Errorf("contract deployment address %s has no valid runtime hash: %w", address, err)
		}
		result[address] = hash
	}
	return result, nil
}

// Accept an observed subset only when every recorded runtime hash is exactly
// the release-planned value at that address.
func contractDeploymentRuntimeHashesCompatible(observed, planned ContractDeployment) bool {
	observedHashes, observedErr := normalizedDeploymentRuntimeHashes(observed)
	plannedHashes, plannedErr := normalizedDeploymentRuntimeHashes(planned)
	if observedErr != nil || plannedErr != nil || len(observedHashes) > len(plannedHashes) {
		return false
	}
	for address, hash := range observedHashes {
		if !strings.EqualFold(hash, plannedHashes[address]) {
			return false
		}
	}
	return true
}

// An in-place release repair may change only the original coordinator
// implementation artifact. All immutable custody contracts, the proxy, the
// hostile drill implementation, and the conformance probe remain byte exact.
func contractDeploymentUpgradeBaselineCompatible(planned, built ContractDeployment) bool {
	if !contractDeploymentAddressesEqual(planned, built) {
		return false
	}
	plannedHashes, plannedErr := normalizedDeploymentRuntimeHashes(planned)
	builtHashes, builtErr := normalizedDeploymentRuntimeHashes(built)
	if plannedErr != nil || builtErr != nil || len(plannedHashes) != len(builtHashes) {
		return false
	}
	for address, hash := range plannedHashes {
		if address == planned.CoordinatorImplementation {
			continue
		}
		if !strings.EqualFold(hash, builtHashes[address]) {
			return false
		}
	}
	return plannedHashes[planned.CoordinatorImplementation] != "" && builtHashes[built.CoordinatorImplementation] != ""
}

// Validate the release-facing half of a finalized legacy-baseline proof. The
// current probe and immutable proxy must be byte exact. Older sink, vault,
// original coordinator, and testnet-only hostile implementation hashes are
// retained by the rebound manifest only after the live observer has produced
// the stronger executable/conformance evidence recorded in baseline.
func validateCoordinatorUpgradeBaselineRelease(baseline CoordinatorUpgradeBaseline, planned, built ContractDeployment, upgrade CoordinatorUpgrade) error {
	if err := validateCoordinatorUpgradeBaseline(baseline, planned, upgrade); err != nil {
		return err
	}
	if baseline.isZero() || !contractDeploymentAddressesEqual(planned, built) {
		return errors.New("coordinator upgrade baseline does not match release deployment addresses")
	}
	builtHash, err := contractDeploymentIdentityHash(built)
	if err != nil || builtHash != baseline.ReleaseDeploymentHash {
		return errors.New("coordinator upgrade baseline does not authenticate the release deployment")
	}
	plannedHashes, plannedErr := normalizedDeploymentRuntimeHashes(planned)
	builtHashes, builtErr := normalizedDeploymentRuntimeHashes(built)
	if plannedErr != nil || builtErr != nil || len(plannedHashes) != len(builtHashes) {
		return errors.New("coordinator upgrade baseline runtime hashes are incomplete")
	}
	releaseExecuted := []common.Address{planned.CoordinatorProxy, planned.PrecompileProbe}
	if baseline.Schema == "urnetwork-coordinator-upgrade-baseline-v2" {
		// A repeated upgrade keeps the already-deployed probe. Its executable
		// body is bound separately after normalizing compiler metadata.
		releaseExecuted = []common.Address{planned.CoordinatorProxy}
	}
	for _, address := range releaseExecuted {
		if !strings.EqualFold(plannedHashes[address], builtHashes[address]) {
			return fmt.Errorf("coordinator upgrade baseline changed release-executed runtime %s", address)
		}
	}
	return nil
}

func validateCoordinatorUpgradePayloadBaseline(baseline CoordinatorUpgradeBaseline, payloads *DeploymentPayloads) error {
	if baseline.Schema != "urnetwork-coordinator-upgrade-baseline-v2" {
		return nil
	}
	if payloads == nil {
		return errors.New("repeated coordinator upgrade payload is unavailable")
	}
	for _, check := range []struct {
		name     string
		address  common.Address
		artifact ContractArtifact
		want     string
	}{
		{"reserve sink", payloads.Manifest.ReserveSink, artifactByName("ReserveSink"), baseline.ReserveSinkExecutableHash},
		{"settlement vault", payloads.Manifest.SettlementVault, artifactByName("SettlementVault"), baseline.SettlementVaultExecutableHash},
		{"precompile probe", payloads.Manifest.PrecompileProbe, TestnetPrecompileProbeArtifact, baseline.PrecompileProbeExecutableHash},
	} {
		got, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[check.address], check.artifact)
		if err != nil || got != check.want {
			return stateMismatchError(err, "repeated coordinator upgrade %s executable=%s want=%s", check.name, got, check.want)
		}
	}
	return nil
}

// Normalize a Solidity runtime for executable compatibility checks. Immutable
// words are constructor data, and the trailing CBOR section authenticates the
// build inputs without changing executable behavior; both remain protected by
// the full deployment hashes elsewhere in the approved plan.
func normalizedSolidityExecutable(code []byte, artifact ContractArtifact) ([]byte, error) {
	template := hexBytes(artifact.RuntimeBytecode)
	if len(code) != len(template) || len(code) < 3 {
		return nil, fmt.Errorf("%s runtime length=%d want=%d", artifact.Name, len(code), len(template))
	}
	normalized := append([]byte(nil), code...)
	for name, offsets := range artifact.ImmutableReferences {
		for _, offset := range offsets {
			if offset < 0 || offset+32 > len(normalized) {
				return nil, fmt.Errorf("%s immutable %s offset is out of range", artifact.Name, name)
			}
			clear(normalized[offset : offset+32])
		}
	}
	metadataLength := int(binary.BigEndian.Uint16(normalized[len(normalized)-2:]))
	metadataStart := len(normalized) - metadataLength - 2
	if metadataLength == 0 || metadataStart <= 0 || metadataStart >= len(normalized)-2 || normalized[metadataStart] < 0xa0 || normalized[metadataStart] > 0xbf {
		return nil, fmt.Errorf("%s runtime has no canonical Solidity CBOR trailer", artifact.Name)
	}
	return normalized[:metadataStart], nil
}

func normalizedSolidityExecutableHash(code []byte, artifact ContractArtifact) (string, error) {
	executable, err := normalizedSolidityExecutable(code, artifact)
	if err != nil {
		return "", err
	}
	return crypto.Keccak256Hash(executable).Hex(), nil
}

func validateContractDeploymentIdentity(manifest ContractDeployment, deployer common.Address) error {
	if deployer == (common.Address{}) || manifest.InitialNonce > ^uint64(0)-8 {
		return errors.New("contract deployment has an invalid deployer or nonce range")
	}
	nonceOffsets := []uint64{0, 1, 2, 4, 5, 8}
	expected := []common.Address{
		crypto.CreateAddress(deployer, manifest.InitialNonce),
		crypto.CreateAddress(deployer, manifest.InitialNonce+1),
		crypto.CreateAddress(deployer, manifest.InitialNonce+2),
		crypto.CreateAddress(deployer, manifest.InitialNonce+4),
		crypto.CreateAddress(deployer, manifest.InitialNonce+5),
		crypto.CreateAddress(deployer, manifest.InitialNonce+8),
	}
	actual := contractDeploymentAddresses(manifest)
	runtimeHashes, err := normalizedDeploymentRuntimeHashes(manifest)
	if err != nil {
		return err
	}
	seen := map[common.Address]bool{}
	for index, address := range actual {
		if address == (common.Address{}) || address != expected[index] || seen[address] {
			return fmt.Errorf("contract deployment address %d is not the unique CREATE address approved for nonce %d", index, manifest.InitialNonce+nonceOffsets[index])
		}
		seen[address] = true
		if runtimeHashes[address] == "" {
			return fmt.Errorf("contract deployment address %s has no runtime hash", address)
		}
	}
	if len(runtimeHashes) != len(actual) {
		return fmt.Errorf("contract deployment has %d runtime hashes, want %d", len(runtimeHashes), len(actual))
	}
	return nil
}

// Bind every deterministic deployer transaction to its exact nonce, target,
// value, and byte payload. The returned fields become part of the action intent
// and are checked again against signed transaction bytes before broadcast.
func deploymentActionEnvelope(payloads *DeploymentPayloads, actionID string, registrationBurnLimitRao uint64) (map[string]string, bool) {
	if payloads == nil {
		return nil, false
	}
	initialNonce := payloads.Manifest.InitialNonce
	nonce := initialNonce
	to := "create"
	created := common.Address{}
	value := new(big.Int)
	var data []byte
	switch actionID {
	case "evm.reserve-sink":
		data, created = payloads.Reserve, payloads.Manifest.ReserveSink
	case "evm.settlement-vault":
		nonce, data, created = initialNonce+1, payloads.Vault, payloads.Manifest.SettlementVault
	case "evm.coordinator-implementation":
		nonce, data, created = initialNonce+2, payloads.Implementation, payloads.Manifest.CoordinatorImplementation
	case "evm.vault-register-escrow":
		nonce, to, data = initialNonce+3, payloads.Manifest.SettlementVault.Hex(), payloads.RegisterEscrow
		value = registrationFundingWei(registrationBurnLimitRao)
	case "evm.coordinator-proxy":
		nonce, data, created = initialNonce+4, payloads.Proxy, payloads.Manifest.CoordinatorProxy
	case "evm.governance-drill-implementation":
		nonce, data, created = initialNonce+5, payloads.GovernanceDrill, payloads.Manifest.GovernanceDrillImplementation
	case "evm.vault-fix-coordinator":
		nonce, to, data = initialNonce+6, payloads.Manifest.SettlementVault.Hex(), payloads.FixVault
	case "evm.sink-fix-recorder":
		nonce, to, data = initialNonce+7, payloads.Manifest.ReserveSink.Hex(), payloads.FixSink
	case "precompile.probe-deploy":
		nonce, data, created = initialNonce+8, payloads.PrecompileProbe, payloads.Manifest.PrecompileProbe
	case "evm.coordinator-upgrade-implementation":
		nonce, data, created = payloads.CoordinatorUpgrade.DeployerNonce, payloads.UpgradeImplementation, payloads.CoordinatorUpgrade.Implementation
	default:
		return nil, false
	}
	result := map[string]string{
		"expected_signer":         payloads.Deployer.Hex(),
		"expected_nonce":          strconv.FormatUint(nonce, 10),
		"expected_transaction_to": to,
		"expected_value_wei":      value.String(),
		"expected_data_keccak256": crypto.Keccak256Hash(data).Hex(),
	}
	if created != (common.Address{}) {
		result["expected_created_address"] = created.Hex()
	}
	return result, true
}

func buildDeploymentPayloads(cfg *ResolvedConfig, roles *RoleSecrets, initialNonce uint64) (*DeploymentPayloads, error) {
	return buildDeploymentPayloadsWithRegistrationGeneration(cfg, roles, initialNonce, 0)
}

func configureCoordinatorUpgradeNonce(payloads *DeploymentPayloads, nonce uint64) error {
	if payloads == nil || payloads.Deployer == (common.Address{}) || payloads.Manifest.InitialNonce > ^uint64(0)-9 {
		return errors.New("coordinator upgrade payload context is invalid")
	}
	minimumNonce := payloads.Manifest.InitialNonce + 9
	if nonce < minimumNonce {
		return fmt.Errorf("coordinator upgrade nonce %d is below initial upgrade nonce %d", nonce, minimumNonce)
	}
	if payloads.CoordinatorUpgrade.Implementation != (common.Address{}) {
		delete(payloads.ExpectedRuntime, payloads.CoordinatorUpgrade.Implementation)
	}
	implementation := crypto.CreateAddress(payloads.Deployer, nonce)
	runtime, err := runtimeWithImmutables(artifactByName("Coordinator"), map[string][]byte{"__self": abiWordAddress(implementation)})
	if err != nil {
		return err
	}
	schema := "urnetwork-coordinator-upgrade-v1"
	if nonce > minimumNonce {
		schema = "urnetwork-coordinator-upgrade-v2"
	}
	payloads.ExpectedRuntime[implementation] = runtime
	payloads.CoordinatorUpgrade = CoordinatorUpgrade{Schema: schema, DeploymentID: payloads.Manifest.DeploymentID, Implementation: implementation, DeployerNonce: nonce, RuntimeCodeHash: crypto.Keccak256Hash(runtime).Hex()}
	return nil
}

func buildDeploymentPayloadsWithRegistrationGeneration(cfg *ResolvedConfig, roles *RoleSecrets, initialNonce, generation uint64) (*DeploymentPayloads, error) {
	if initialNonce > ^uint64(0)-9 {
		return nil, errors.New("deployment nonce range overflows uint64")
	}
	if err := validateContractRegistrationGeneration(cfg.Config.Topology, generation); err != nil {
		return nil, err
	}
	deployer, err := roles.EVMAddress("deployer")
	if err != nil {
		return nil, err
	}
	owner, err := roles.EVMAddress("testnet-owner")
	if err != nil {
		return nil, err
	}
	guardian, err := roles.EVMAddress("guardian")
	if err != nil {
		return nil, err
	}
	oracle, err := roles.EVMAddress("commitment-oracle")
	if err != nil {
		return nil, err
	}
	// The first three CREATEs are followed by the vault-owned escrow
	// registration, then the proxy and hostile implementation CREATEs and two
	// one-shot link calls. The conformance probe is therefore nonce+8.
	m := ContractDeployment{Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, InitialNonce: initialNonce, RegistrationRoleGeneration: generation, ReserveSink: crypto.CreateAddress(deployer, initialNonce), SettlementVault: crypto.CreateAddress(deployer, initialNonce+1), CoordinatorImplementation: crypto.CreateAddress(deployer, initialNonce+2), CoordinatorProxy: crypto.CreateAddress(deployer, initialNonce+4), GovernanceDrillImplementation: crypto.CreateAddress(deployer, initialNonce+5), PrecompileProbe: crypto.CreateAddress(deployer, initialNonce+8), RuntimeHashes: map[string]string{}}
	reserveHotkey, err := roleBytes32(roles, "reserve-hotkey")
	if err != nil {
		return nil, err
	}
	escrowHotkey, err := roleBytes32(roles, escrowHotkeyLabelForGeneration(generation))
	if err != nil {
		return nil, err
	}
	sinkSelf := ss58.EvmMirrorPubkey(m.ReserveSink)
	vaultSelf := ss58.EvmMirrorPubkey(m.SettlementVault)
	coordinatorSelf := ss58.EvmMirrorPubkey(m.CoordinatorProxy)
	reserveABI, err := abi.JSON(strings.NewReader(ReserveSinkABI))
	if err != nil {
		return nil, err
	}
	vaultABI, err := abi.JSON(strings.NewReader(SettlementVaultABI))
	if err != nil {
		return nil, err
	}
	coordABI, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return nil, err
	}
	proxyABI, err := abi.JSON(strings.NewReader(ERC1967ProxyABI))
	if err != nil {
		return nil, err
	}
	sinkArgs, err := reserveABI.Constructor.Inputs.Pack(cfg.Netuid, reserveHotkey, sinkSelf, deployer)
	if err != nil {
		return nil, fmt.Errorf("reserve constructor: %w", err)
	}
	minimumTTL := cfg.Policy.Settlement.EpochBlocks * cfg.Policy.Settlement.ClaimTTLEpochs
	minimumTransfer := cfg.Public.Chain.ExpectedDefaultMinTransferRao
	if minimumTransfer == 0 {
		return nil, errors.New("runtime DefaultMinTransfer is required for settlement-vault deployment")
	}
	vaultArgs, err := vaultABI.Constructor.Inputs.Pack(cfg.Netuid, escrowHotkey, vaultSelf, minimumTTL, minimumTransfer, deployer)
	if err != nil {
		return nil, fmt.Errorf("vault constructor: %w", err)
	}
	hash, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		return nil, err
	}
	policy := stabi.STCoordinatorPolicySnapshot{PolicyHash: hash, EffectiveEpoch: 0, EffectiveBlock: 0, EpochBlocks: cfg.Policy.Settlement.EpochBlocks, RootCommitWindowBlocks: cfg.Policy.Settlement.RootCommitWindowBlocks, FinalizeOffsetBlocks: cfg.Policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: cfg.Policy.Settlement.CloseGraceBlocks, ClaimTTLEpochs: cfg.Policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: cfg.Policy.Settlement.ClaimGraceEpochs, MaximumBindingValidityEpochs: cfg.Policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: cfg.Policy.Settlement.EpochBlocks * 2, EpochDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao)}
	initData, err := coordABI.Pack("initialize", cfg.Netuid, owner, guardian, coordinatorSelf, m.SettlementVault, m.ReserveSink, oracle, policy)
	if err != nil {
		return nil, fmt.Errorf("coordinator initialize: %w", err)
	}
	proxyArgs, err := proxyABI.Constructor.Inputs.Pack(m.CoordinatorImplementation, initData)
	if err != nil {
		return nil, fmt.Errorf("proxy constructor: %w", err)
	}
	fixVault, err := vaultABI.Pack("setCoordinatorOnce", m.CoordinatorProxy)
	if err != nil {
		return nil, err
	}
	fixSink, err := reserveABI.Pack("setRecorderOnce", m.CoordinatorProxy)
	if err != nil {
		return nil, err
	}
	probeABI, err := abi.JSON(strings.NewReader(SubnetProbeABI))
	if err != nil {
		return nil, err
	}
	probeArgs, err := probeABI.Constructor.Inputs.Pack(cfg.Netuid)
	if err != nil {
		return nil, fmt.Errorf("precompile probe constructor: %w", err)
	}
	registerEscrow, err := vaultABI.Pack("registerEscrow", cfg.Config.Budgets.MaximumRegistrationBurnRao)
	if err != nil {
		return nil, err
	}
	p := &DeploymentPayloads{Deployer: deployer, Manifest: m, Reserve: append(hexBytes(ReserveSinkCreationBytecode), sinkArgs...), Vault: append(hexBytes(SettlementVaultCreationBytecode), vaultArgs...), Implementation: hexBytes(CoordinatorCreationBytecode), RegisterEscrow: registerEscrow, Proxy: append(hexBytes(ERC1967ProxyCreationBytecode), proxyArgs...), GovernanceDrill: hexBytes(CoordinatorAdversaryCreationBytecode), FixVault: fixVault, FixSink: fixSink, PrecompileProbe: append(hexBytes(SubnetProbeCreationBytecode), probeArgs...), UpgradeImplementation: hexBytes(CoordinatorCreationBytecode), ExpectedRuntime: map[common.Address][]byte{}}
	p.ExpectedRuntime[m.ReserveSink], err = runtimeWithImmutables(artifactByName("ReserveSink"), map[string][]byte{"netuid": abiWordUint(uint64(cfg.Netuid)), "reserveHotkey": reserveHotkey[:], "selfColdkey": sinkSelf[:], "bootstrap": abiWordAddress(deployer)})
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.SettlementVault], err = runtimeWithImmutables(artifactByName("SettlementVault"), map[string][]byte{"netuid": abiWordUint(uint64(cfg.Netuid)), "escrowHotkey": escrowHotkey[:], "selfColdkey": vaultSelf[:], "minimumClaimTTLBlocks": abiWordUint(minimumTTL), "minimumTransferTaoRao": abiWordUint(minimumTransfer), "bootstrap": abiWordAddress(deployer)})
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.CoordinatorImplementation], err = runtimeWithImmutables(artifactByName("Coordinator"), map[string][]byte{"__self": abiWordAddress(m.CoordinatorImplementation)})
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.CoordinatorProxy], err = runtimeWithImmutables(artifactByName("ERC1967Proxy"), nil)
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.GovernanceDrillImplementation], err = runtimeWithImmutables(TestnetGovernanceDrillArtifact, map[string][]byte{"__self": abiWordAddress(m.GovernanceDrillImplementation)})
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.PrecompileProbe], err = runtimeWithImmutables(TestnetPrecompileProbeArtifact, map[string][]byte{"owner": abiWordAddress(deployer), "netuid": abiWordUint(uint64(cfg.Netuid))})
	if err != nil {
		return nil, err
	}
	if err := configureCoordinatorUpgradeNonce(p, initialNonce+9); err != nil {
		return nil, err
	}
	for addr, code := range p.ExpectedRuntime {
		if addr == p.CoordinatorUpgrade.Implementation {
			continue
		}
		p.Manifest.RuntimeHashes[addr.Hex()] = crypto.Keccak256Hash(code).Hex()
	}
	return p, nil
}

func roleBytes32(r *RoleSecrets, label string) ([32]byte, error) {
	var out [32]byte
	v, ok := r.Substrate[label]
	if !ok {
		return out, fmt.Errorf("missing substrate role %s", label)
	}
	b, err := hex.DecodeString(v.PublicKeyHex)
	if err != nil || len(b) != 32 {
		return out, fmt.Errorf("invalid role public key %s", label)
	}
	copy(out[:], b)
	return out, nil
}
func artifactByName(name string) ContractArtifact {
	for _, a := range ReleaseContractArtifacts {
		if a.Name == name {
			return a
		}
	}
	panic("unknown generated artifact " + name)
}
func runtimeWithImmutables(a ContractArtifact, values map[string][]byte) ([]byte, error) {
	code := hexBytes(a.RuntimeBytecode)
	if len(values) != len(a.ImmutableReferences) {
		return nil, fmt.Errorf("%s: got %d immutable values, want %d", a.Name, len(values), len(a.ImmutableReferences))
	}
	for name, offsets := range a.ImmutableReferences {
		v, ok := values[name]
		if !ok {
			return nil, fmt.Errorf("%s: missing immutable %s", a.Name, name)
		}
		if len(v) != 32 {
			return nil, fmt.Errorf("%s immutable %s is %d bytes", a.Name, name, len(v))
		}
		for _, off := range offsets {
			if off < 0 || off+32 > len(code) {
				return nil, fmt.Errorf("%s immutable offset out of range", a.Name)
			}
			copy(code[off:off+32], v)
		}
	}
	return code, nil
}
func hexBytes(s string) []byte {
	b, err := hex.DecodeString(stringsTrim0x(s))
	if err != nil {
		panic(err)
	}
	return b
}
func abiWordUint(v uint64) []byte {
	b := make([]byte, 32)
	new(big.Int).SetUint64(v).FillBytes(b)
	return b
}
func abiWordAddress(a common.Address) []byte { b := make([]byte, 32); copy(b[12:], a[:]); return b }
func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

type EVMTxManager struct {
	client       *ethclient.Client
	chainID      *big.Int
	deploymentID string
	stateDir     string
	journal      *Journal
	key          *ecdsa.PrivateKey
}

func DialEVMTxManager(ctx context.Context, cfg *ResolvedConfig, stateDir string, j *Journal, roles *RoleSecrets, roleLabel string) (*EVMTxManager, error) {
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, err
	}
	id, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, err
	}
	if id.Uint64() != testnetChainID {
		client.Close()
		return nil, fmt.Errorf("refusing EVM chain id %d", id.Uint64())
	}
	role, err := roles.EVMKey(roleLabel)
	if err != nil {
		client.Close()
		return nil, err
	}
	key, err := crypto.HexToECDSA(role.PrivateKeyHex)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &EVMTxManager{client: client, chainID: id, deploymentID: cfg.Config.Deployment.DeploymentID, stateDir: stateDir, journal: j, key: key}, nil
}
func (m *EVMTxManager) Close() { m.client.Close() }
func (m *EVMTxManager) PendingNonce(ctx context.Context) (uint64, error) {
	return m.client.PendingNonceAt(ctx, crypto.PubkeyToAddress(m.key.PublicKey))
}

// Apply the fixed live-estimate margin without allowing a hostile or malformed
// RPC result to wrap the uint64 gas limit.
func paddedEVMGas(estimatedGas uint64) (uint64, error) {
	padded, ok := checkedAdd(estimatedGas, estimatedGas/5)
	if !ok {
		return 0, errors.New("EVM gas estimate margin overflow")
	}
	padded, ok = checkedAdd(padded, 25_000)
	if !ok {
		return 0, errors.New("EVM gas estimate fixed margin overflow")
	}
	return padded, nil
}

// Enforce gas units, fee price, aggregate spend, value, and current signer
// balance together before any transaction bytes are persisted or broadcast.
func validateEVMTransactionEnvelope(action Action, estimatedGas uint64, feeCap, balance, value *big.Int) (uint64, *big.Int, error) {
	maximumGasUnits, maximumFeePerGas, err := evmActionFeeEnvelope(action)
	if err != nil {
		return 0, nil, err
	}
	if feeCap == nil || feeCap.Sign() < 0 || !feeCap.IsUint64() || feeCap.Uint64() > maximumFeePerGas {
		return 0, nil, fmt.Errorf("%s live fee cap %v exceeds approved fee-per-gas ceiling %d", action.ID, feeCap, maximumFeePerGas)
	}
	gas, err := paddedEVMGas(estimatedGas)
	if err != nil {
		return 0, nil, fmt.Errorf("%s: %w", action.ID, err)
	}
	if gas > maximumGasUnits {
		return 0, nil, fmt.Errorf("%s padded gas %d exceeds approved gas-unit ceiling %d", action.ID, gas, maximumGasUnits)
	}
	maximumCost := new(big.Int).Mul(new(big.Int).SetUint64(gas), feeCap)
	actionCeiling, ceilingErr := action.Spend.EVMGasWei.Big()
	if ceilingErr != nil {
		return 0, nil, fmt.Errorf("%s action ceiling: %w", action.ID, ceilingErr)
	}
	if maximumCost.Cmp(actionCeiling) > 0 {
		return 0, nil, fmt.Errorf("%s maximum gas cost %s exceeds action ceiling %s", action.ID, maximumCost, action.Spend.EVMGasWei)
	}
	if balance == nil || balance.Sign() < 0 || value == nil || value.Sign() < 0 {
		return 0, nil, fmt.Errorf("%s has invalid signer balance or transaction value", action.ID)
	}
	required := new(big.Int).Add(new(big.Int).Set(maximumCost), value)
	if balance.Cmp(required) < 0 {
		return 0, nil, fmt.Errorf("%s signer balance %s is below value-plus-maximum-gas requirement %s", action.ID, balance, required)
	}
	return gas, maximumCost, nil
}

// Verify optional exact transaction fields which are hash-bound into critical
// deployment actions. Either the complete field set is present or none is.
func validateApprovedEVMTransactionFields(action Action, signer common.Address, nonce uint64, to *common.Address, value *big.Int, data []byte) error {
	keys := []string{"expected_signer", "expected_nonce", "expected_transaction_to", "expected_value_wei", "expected_data_keccak256"}
	present := 0
	for _, key := range keys {
		if action.Parameters[key] != "" {
			present++
		}
	}
	if present == 0 {
		return nil
	}
	if present != len(keys) {
		return fmt.Errorf("action %s has an incomplete exact EVM transaction envelope", action.ID)
	}
	if !common.IsHexAddress(action.Parameters["expected_signer"]) || common.HexToAddress(action.Parameters["expected_signer"]) != signer {
		return fmt.Errorf("action %s signer %s differs from approved %s", action.ID, signer, action.Parameters["expected_signer"])
	}
	expectedNonce, err := strconv.ParseUint(action.Parameters["expected_nonce"], 10, 64)
	if err != nil || expectedNonce != nonce {
		return fmt.Errorf("action %s nonce %d differs from approved %s", action.ID, nonce, action.Parameters["expected_nonce"])
	}
	expectedTo := action.Parameters["expected_transaction_to"]
	if expectedTo == "create" {
		if to != nil {
			return fmt.Errorf("action %s expected contract creation, got target %s", action.ID, to.Hex())
		}
		created := action.Parameters["expected_created_address"]
		if !common.IsHexAddress(created) || crypto.CreateAddress(signer, nonce) != common.HexToAddress(created) {
			return fmt.Errorf("action %s CREATE address differs from approved %s", action.ID, created)
		}
	} else if !common.IsHexAddress(expectedTo) || to == nil || *to != common.HexToAddress(expectedTo) {
		return fmt.Errorf("action %s target differs from approved %s", action.ID, expectedTo)
	} else if action.Parameters["expected_created_address"] != "" {
		return fmt.Errorf("action %s call unexpectedly carries a CREATE address", action.ID)
	}
	expectedValue, ok := new(big.Int).SetString(action.Parameters["expected_value_wei"], 10)
	if !ok || expectedValue.Sign() < 0 || value == nil || expectedValue.Cmp(value) != 0 {
		return fmt.Errorf("action %s value differs from approved %s", action.ID, action.Parameters["expected_value_wei"])
	}
	if !strings.EqualFold(crypto.Keccak256Hash(data).Hex(), action.Parameters["expected_data_keccak256"]) {
		return fmt.Errorf("action %s data hash differs from approved %s", action.ID, action.Parameters["expected_data_keccak256"])
	}
	return nil
}

func (m *EVMTxManager) Send(ctx context.Context, planHash string, a Action, to *common.Address, value *big.Int, data []byte) (*types.Receipt, error) {
	if prior, ok := m.journal.LatestTransaction(planHash, a.ID, a.IntentHash); ok {
		rawPath := filepath.Join(m.stateDir, "transactions", stringsTrim0x(prior.TransactionHash)+".rlp")
		raw, err := os.ReadFile(rawPath)
		if err != nil {
			return nil, fmt.Errorf("resume exact EVM transaction %s: %w", prior.TransactionHash, err)
		}
		var tx types.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil {
			return nil, fmt.Errorf("decode persisted EVM transaction %s: %w", prior.TransactionHash, err)
		}
		if !strings.EqualFold(tx.Hash().Hex(), prior.TransactionHash) {
			return nil, fmt.Errorf("persisted EVM transaction hash mismatch: got %s want %s", tx.Hash(), prior.TransactionHash)
		}
		signer, err := types.Sender(types.LatestSignerForChainID(m.chainID), &tx)
		if err != nil {
			return nil, fmt.Errorf("recover persisted EVM transaction signer: %w", err)
		}
		if err := validateApprovedEVMTransactionFields(a, signer, tx.Nonce(), tx.To(), tx.Value(), tx.Data()); err != nil {
			return nil, fmt.Errorf("persisted EVM transaction approval: %w", err)
		}
		return m.waitExactTransaction(ctx, planHash, a, &tx)
	}
	from := crypto.PubkeyToAddress(m.key.PublicKey)
	nonce, err := m.client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, err
	}
	if err := validateApprovedEVMTransactionFields(a, from, nonce, to, value, data); err != nil {
		return nil, err
	}
	tip, err := m.client.SuggestGasTipCap(ctx)
	if err != nil {
		tip = big.NewInt(1_000_000_000)
	}
	header, err := m.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	feeCap := new(big.Int).Set(tip)
	if header.BaseFee != nil {
		feeCap.Add(new(big.Int).Mul(header.BaseFee, big.NewInt(2)), tip)
	}
	_, maximumFeePerGas, err := evmActionFeeEnvelope(a)
	if err != nil {
		return nil, err
	}
	if !feeCap.IsUint64() || feeCap.Uint64() > maximumFeePerGas {
		return nil, fmt.Errorf("%s live fee cap %s exceeds approved fee-per-gas ceiling %d", a.ID, feeCap, maximumFeePerGas)
	}
	msg := ethereum.CallMsg{From: from, To: to, Value: value, Data: data, GasTipCap: tip, GasFeeCap: feeCap}
	estimatedGas, err := m.client.EstimateGas(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("estimate %s: %w", a.ID, err)
	}
	balance, err := m.client.BalanceAt(ctx, from, nil)
	if err != nil {
		return nil, fmt.Errorf("read %s signer balance: %w", a.ID, err)
	}
	gas, _, err := validateEVMTransactionEnvelope(a, estimatedGas, feeCap, balance, value)
	if err != nil {
		return nil, err
	}
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: m.chainID, Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap, Gas: gas, To: to, Value: value, Data: data})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(m.chainID), m.key)
	if err != nil {
		return nil, err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, err
	}
	rawPath := filepath.Join(m.stateDir, "transactions", stringsTrim0x(signed.Hash().Hex())+".rlp")
	if err := atomicWrite(rawPath, raw, 0o600); err != nil {
		return nil, err
	}
	recovery, err := finalizedEVMHead(ctx, m.client)
	if err != nil {
		return nil, err
	}
	if err := m.journal.Append(JournalEntry{DeploymentID: m.deploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageBroadcast, Signer: from.Hex(), Nonce: strconv.FormatUint(nonce, 10), TransactionHash: signed.Hash().Hex(), RecoveryBlock: recovery.Number, RecoveryBlockHash: recovery.Hash}); err != nil {
		return nil, err
	}
	return m.waitExactTransaction(ctx, planHash, a, signed)
}

func knownEVMTxError(err error) bool {
	if err == nil {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "known transaction") || strings.Contains(message, "already known") || strings.Contains(message, "already imported") ||
		strings.Contains(message, "nonce too low") || strings.Contains(message, "replacement transaction underpriced")
}

func (m *EVMTxManager) waitExactTransaction(ctx context.Context, planHash string, a Action, signed *types.Transaction) (*types.Receipt, error) {
	if signed == nil {
		return nil, errors.New("nil persisted EVM transaction")
	}
	from, err := types.Sender(types.LatestSignerForChainID(m.chainID), signed)
	if err != nil {
		return nil, fmt.Errorf("recover persisted EVM signer: %w", err)
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		receipt, err := m.client.TransactionReceipt(ctx, signed.Hash())
		if err == nil {
			return m.finalizeReceipt(ctx, planHash, a, receipt)
		}
		if err != ethereum.NotFound {
			return nil, err
		}
		head, err := finalizedEVMHead(ctx, m.client)
		if err != nil {
			return nil, err
		}
		nonce, err := m.client.NonceAt(ctx, from, new(big.Int).SetUint64(head.Number))
		if err != nil {
			return nil, err
		}
		if nonce > signed.Nonce() {
			return nil, fmt.Errorf("EVM nonce %d was consumed by a different finalized transaction", signed.Nonce())
		}
		if err := m.client.SendTransaction(ctx, signed); !knownEVMTxError(err) {
			return nil, fmt.Errorf("rebroadcast exact EVM transaction %s: %w", signed.Hash(), err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *EVMTxManager) finalizeReceipt(ctx context.Context, planHash string, a Action, r *types.Receipt) (*types.Receipt, error) {
	if err := m.journal.Append(JournalEntry{DeploymentID: m.deploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageIncluded, TransactionHash: r.TxHash.Hex(), BlockNumber: r.BlockNumber.Uint64(), BlockHash: r.BlockHash.Hex()}); err != nil {
		return r, err
	}
	finalized, err := waitEVMReceiptFinality(ctx, m.client, r.TxHash)
	if err != nil {
		return r, err
	}
	if finalized.Status != types.ReceiptStatusSuccessful {
		return finalized, fmt.Errorf("EVM transaction %s reverted in its canonical inclusion", finalized.TxHash)
	}
	if err := m.journal.Append(JournalEntry{DeploymentID: m.deploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageFinalized, TransactionHash: finalized.TxHash.Hex(), BlockNumber: finalized.BlockNumber.Uint64(), BlockHash: finalized.BlockHash.Hex()}); err != nil {
		return finalized, err
	}
	return finalized, nil
}

type evmReceiptFinalityReader interface {
	evmBlockReader
	TransactionReceipt(context.Context, common.Hash) (*types.Receipt, error)
}

type ethEVMReceiptFinalityReader struct {
	client *ethclient.Client
}

func (self ethEVMReceiptFinalityReader) EVMBlockByNumber(ctx context.Context, number *big.Int) (ChainHead, error) {
	return (ethEVMBlockReader{client: self.client}).EVMBlockByNumber(ctx, number)
}

func (self ethEVMReceiptFinalityReader) TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return self.client.TransactionReceipt(ctx, hash)
}

// Read one receipt, the EVM finalized head, and the canonical EVM RPC
// header at the inclusion height. A canonical mismatch can be a transient
// reorg, so the caller retries it; malformed RPC data fails immediately.
func observeEVMReceiptFinality(ctx context.Context, reader evmReceiptFinalityReader, txHash common.Hash) (*types.Receipt, bool, error) {
	if reader == nil || txHash == (common.Hash{}) {
		return nil, false, errors.New("EVM finality observation is incomplete")
	}
	finalized, err := finalizedEVMHeadFromReader(ctx, reader)
	if err != nil {
		return nil, false, err
	}
	receipt, err := reader.TransactionReceipt(ctx, txHash)
	if errors.Is(err, ethereum.NotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if receipt == nil || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Sign() <= 0 {
		return nil, false, errors.New("EVM receipt has no valid inclusion block")
	}
	if finalized.Number < receipt.BlockNumber.Uint64() {
		return receipt, false, nil
	}
	canonicalHash, err := canonicalEVMBlockHash(ctx, reader, receipt.BlockNumber.Uint64())
	if err != nil {
		return nil, false, err
	}
	return receipt, receiptIsCanonicalAndFinalized(finalized.Number, receipt, canonicalHash), nil
}

func waitEVMReceiptFinalityWithInterval(ctx context.Context, reader evmReceiptFinalityReader, txHash common.Hash, interval time.Duration) (*types.Receipt, error) {
	if interval <= 0 {
		return nil, errors.New("EVM finality polling interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		receipt, ready, err := observeEVMReceiptFinality(ctx, reader, txHash)
		if err != nil {
			return nil, err
		}
		if ready {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitEVMReceiptFinality(ctx context.Context, client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	if client == nil {
		return nil, errors.New("EVM finality client is unavailable")
	}
	return waitEVMReceiptFinalityWithInterval(ctx, ethEVMReceiptFinalityReader{client: client}, txHash, 3*time.Second)
}

func receiptIsCanonicalAndFinalized(finalized uint64, receipt *types.Receipt, canonicalHash string) bool {
	return receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.IsUint64() &&
		receipt.BlockNumber.Uint64() <= finalized && canonicalHash != "" &&
		strings.EqualFold(canonicalHash, receipt.BlockHash.Hex())
}

func verifyRuntimeCode(ctx context.Context, c *ethclient.Client, expected map[common.Address][]byte) (map[string]string, error) {
	head, err := finalizedEVMHead(ctx, c)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for addr, want := range expected {
		got, err := c.CodeAt(ctx, addr, new(big.Int).SetUint64(head.Number))
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(got, want) {
			return nil, fmt.Errorf("runtime bytecode mismatch at %s: got %s want %s", addr, crypto.Keccak256Hash(got), crypto.Keccak256Hash(want))
		}
		out[addr.Hex()] = crypto.Keccak256Hash(got).Hex()
	}
	return out, nil
}

func saveContractDeployment(stateDir string, m ContractDeployment) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(stateDir, "public", "contracts.json"), append(b, '\n'), 0o644)
}
func loadContractDeployment(stateDir string) (*ContractDeployment, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", "contracts.json"))
	if err != nil {
		return nil, err
	}
	var m ContractDeployment
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
