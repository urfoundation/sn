// Offline launch preparation binds the retained trim scope, or v5's explicit
// no-trim census, and its UR roles (two, or v5's sole role) to the signed
// contract and root custody plans. It grants no chain effect.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/urfoundation/sn/v2026/validator"
)

const bootstrapChainConfigSchemaV1 = "urnetwork-mainnet-bootstrap-chain-config-v1"
const bootstrapChainPlanSchemaV1 = "urnetwork-mainnet-bootstrap-chain-preparation-v1"
const bootstrapChainConfigSchemaV2 = "urnetwork-mainnet-bootstrap-chain-config-v2"
const bootstrapChainPlanSchemaV2 = "urnetwork-mainnet-bootstrap-chain-preparation-v2"
const bootstrapChainConfigSchema = "urnetwork-mainnet-bootstrap-chain-config-v3"
const bootstrapChainPlanSchema = "urnetwork-mainnet-bootstrap-chain-preparation-v3"
const bootstrapChainConfigSchemaV4 = "urnetwork-mainnet-bootstrap-chain-config-v4"
const bootstrapChainPlanSchemaV4 = "urnetwork-mainnet-bootstrap-chain-preparation-v4"
const bootstrapChainConfigSchemaV5 = "urnetwork-mainnet-bootstrap-chain-config-v5"
const bootstrapChainPlanSchemaV5 = "urnetwork-mainnet-bootstrap-chain-preparation-v5"
const bootstrapChainStateFile = "bootstrap-chain.json"
const maximumBootstrapChainPlanBytes = 512 * 1024

// The intended majority and secondary roles, or v5's sole role, use the standard
// validator config grammar. Role labels do not prove live stake, key custody or
// service identity.
type bootstrapChainValidator struct {
	ValidatorId uint64 `json:"validator_id"`
	subnetIdentityExpectation
	Config            planFileReference `json:"config"`
	Role              string            `json:"role,omitempty"`
	Implementation    string            `json:"implementation,omitempty"`
	ApprovalPublicKey string            `json:"approval_public_key_ed25519,omitempty"`
}

// All network, deployment and custody coordinates are independently supplied.
// The two signed child plans retain their own authority and exact state paths.
// Only v5 may name an owner-trim mode; an absent mode keeps the retained trim.
type bootstrapChainConfig struct {
	Schema          string                       `json:"schema"`
	DeploymentId    string                       `json:"deployment_id"`
	Netuid          uint16                       `json:"netuid"`
	Network         planNetwork                  `json:"network"`
	RunDirectory    string                       `json:"run_directory"`
	OwnerTrimMode   string                       `json:"owner_trim_mode,omitempty"`
	OwnerTrimPolicy planFileReference            `json:"owner_trim_policy"`
	OwnerTrimPlan   planFileReference            `json:"owner_trim_plan"`
	Contracts       planFileReference            `json:"contracts"`
	Root            planFileReference            `json:"root"`
	Validators      []bootstrapChainValidator    `json:"ur_validators"`
	RootValidator   *bootstrapChainRootValidator `json:"root_validator,omitempty"`
}

// The domain and fixed false flags distinguish local custody preparation from
// an executable chain graph. Existing trim blockers remain part of its seal.
// A no-trim plan also seals the owner's decision beside the unchanged phases.
type bootstrapChainPlan struct {
	Schema               string                                    `json:"schema"`
	ConfigPath           string                                    `json:"config_path"`
	ConfigSha256         string                                    `json:"config_sha256"`
	Config               bootstrapChainConfig                      `json:"config"`
	OwnerTrimContentHash string                                    `json:"owner_trim_content_hash"`
	OwnerTrimBlockers    []string                                  `json:"owner_trim_execution_blockers"`
	ContractPlanHash     string                                    `json:"contract_plan_hash"`
	RootPlanHash         string                                    `json:"root_plan_hash"`
	ValidatorInspections []validator.ProductionBootstrapInspection `json:"ur_validator_config_inspections,omitempty"`
	RootInspection       *bootstrapChainRootInspection             `json:"root_validator_config_inspection,omitempty"`
	PendingChainPhases   []string                                  `json:"pending_chain_phases"`
	OwnerTrimPhase       string                                    `json:"owner_trim_phase,omitempty"`
	NetworkEffects       bool                                      `json:"network_effects"`
	NativeSigning        bool                                      `json:"native_signing"`
	ActivationReady      bool                                      `json:"activation_ready"`
	ContentHash          string                                    `json:"content_hash"`
}

// Only validated, in-memory child projections reach the offline owner. Their
// raw inputs are pinned before parsing and are reloaded on every invocation.
type bootstrapChainPreparation struct {
	Plan      bootstrapChainPlan
	Contracts evmCreatePlan
	Root      bootstrapRootPlan
}

// The preparation hash cannot accept a blocked review graph or child plan hash.
func bootstrapChainPlanHash(plan bootstrapChainPlan) string {
	plan.ContentHash = ""
	raw, _ := json.Marshal(plan)
	digest := sha256.Sum256(append([]byte(plan.Schema+"\x00"), raw...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Historical plans keep their original domain and omitted inspection fields.
// Only the current schema may create new preparation through the public command.
func bootstrapChainPlanSchemaForConfig(schema string) string {
	switch schema {
	case bootstrapChainConfigSchemaV1:
		return bootstrapChainPlanSchemaV1
	case bootstrapChainConfigSchemaV2:
		return bootstrapChainPlanSchemaV2
	case bootstrapChainConfigSchema:
		return bootstrapChainPlanSchema
	case bootstrapChainConfigSchemaV4:
		return bootstrapChainPlanSchemaV4
	case bootstrapChainConfigSchemaV5:
		return bootstrapChainPlanSchemaV5
	default:
		return ""
	}
}

// Every result preserves the complete unexecuted scope, including contracts
// whose payloads may not yet exist in the approved reserve CREATE prefix.
// These names are part of original plan seals; current treasury outcome reports
// must not rewrite them and invalidate retained preparation or child custody.
func bootstrapChainPendingPhases() []string {
	return []string{"owner-trim-execution-and-generation-reconciliation", "complete-contract-installation-and-evidence-binding", "two-ur-validator-production-admissions-and-healthy-operators", "root-current-authority-and-service-activation", "native-10-percent-allocation-and-90-percent-recycle-acceptance"}
}

// Only the new v5 schema seals a one-validator phase. Every two-role schema
// keeps the original names above, so no retained seal or child marker moves.
func bootstrapChainPendingPhasesForSchema(schema string) []string {
	phases := bootstrapChainPendingPhases()
	if schema == bootstrapChainConfigSchemaV5 {
		phases[2] = "one-ur-validator-production-admission-and-healthy-operators"
	}
	return phases
}

// Each schema fixes its ordered UR role labels. A one-role launch cannot borrow
// the majority or secondary label, and a two-role plan cannot select "sole".
func bootstrapChainValidatorRoles(schema string) []string {
	if schema == bootstrapChainConfigSchemaV5 {
		return []string{"sole"}
	}
	return []string{"majority", "secondary"}
}

// Every role's exact config input in sealed role order, for overlap checks.
func (self bootstrapChainConfig) validatorConfigPaths() []string {
	paths := make([]string, 0, len(self.Validators))
	for _, role := range self.Validators {
		paths = append(paths, role.Config.Path)
	}
	return paths
}

// The accepted plan owns only one local journal and never supplies a signing key.
func (self bootstrapChainPlan) validate() error {
	c := self.Config
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > maximumBootstrapChainPlanBytes {
		return errors.Join(errors.New("bootstrap chain preparation exceeds its retained plan bound"), err)
	}
	legacy := c.Schema == bootstrapChainConfigSchemaV1
	roles := bootstrapChainValidatorRoles(c.Schema)
	if self.Schema == "" || self.Schema != bootstrapChainPlanSchemaForConfig(c.Schema) || !planLabel(c.DeploymentId) || c.Netuid != 25 ||
		strings.TrimSpace(c.Network.NativeChain) == "" || !rootCanonicalHash(c.Network.GenesisHash) || c.Network.EvmChainId != mainnetEvmChainId ||
		!bootstrapRootAbsolutePath(c.RunDirectory) || !bootstrapRootAbsolutePath(self.ConfigPath) || !planSha256(self.ConfigSha256) ||
		!planSha256(self.OwnerTrimContentHash) || !planSha256(self.ContractPlanHash) || !planSha256(self.RootPlanHash) ||
		self.NetworkEffects || self.NativeSigning || self.ActivationReady || len(self.OwnerTrimBlockers) == 0 ||
		!slices.Equal(self.PendingChainPhases, bootstrapChainPendingPhasesForSchema(c.Schema)) || self.ContentHash != bootstrapChainPlanHash(self) || len(c.Validators) != len(roles) {
		return errors.New("bootstrap chain preparation schema, identity, bounds or seal differs")
	}
	if !c.validOwnerTrimMode() || self.OwnerTrimPhase != c.ownerTrimPhase() {
		return errors.New("bootstrap chain owner-trim mode or its sealed phase decision differs")
	}
	references := []planFileReference{c.OwnerTrimPolicy, c.OwnerTrimPlan, c.Contracts, c.Root}
	for _, role := range c.Validators {
		references = append(references, role.Config)
	}
	for _, reference := range references {
		if !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) {
			return errors.New("bootstrap chain preparation requires exact private input pins")
		}
	}
	for i, role := range c.Validators {
		if role.ValidatorId == 0 || !rootCanonicalHash(role.Hotkey) || !rootCanonicalHash(role.Coldkey) || role.RegistrationBlock == nil ||
			i != 0 && (role.ValidatorId == c.Validators[0].ValidatorId || role.Hotkey == c.Validators[0].Hotkey || role.Config.Path == c.Validators[0].Config.Path || role.Config.Sha256 == c.Validators[0].Config.Sha256) {
			return errors.New("bootstrap chain requires distinct explicit UR validator identities and config inputs")
		}
		if legacy {
			if role.Role != "" || role.Implementation != "" || role.ApprovalPublicKey != "" || len(self.ValidatorInspections) != 0 {
				return errors.New("bootstrap chain v1 cannot acquire production config inspection authority")
			}
		} else if role.Role != roles[i] || role.Implementation != "sn/validator" || !rootCanonicalHash(role.ApprovalPublicKey) {
			if len(roles) == 1 {
				return errors.New("bootstrap chain v5 requires one sole sn/validator role with an independent approval signer pin")
			}
			return errors.New("bootstrap chain requires ordered majority and secondary sn/validator roles with independent approval signer pins")
		}
	}
	if !legacy {
		if err := validateBootstrapChainValidatorInspections(c, self.ValidatorInspections); err != nil {
			return err
		}
	}
	if bootstrapChainHasRootRole(c.Schema) {
		if self.RootInspection == nil {
			return errors.New("bootstrap chain requires a verified root config inspection")
		}
		return self.RootInspection.validate(c, self.RootPlanHash)
	}
	if c.RootValidator != nil || self.RootInspection != nil {
		return errors.New("bootstrap chain v1/v2 cannot acquire root config inspection authority")
	}
	return nil
}

// All journals and lock markers must be distinct direct children. Separation
// also includes transitive artifact/service files before any owner can open.
func (self bootstrapChainPreparation) validate() error {
	if err := errors.Join(self.Plan.validate(), self.Contracts.Config.validate(), self.Root.validate()); err != nil {
		return err
	}
	c, contract, root := self.Plan.Config, self.Contracts.Config.Plan, self.Root
	if bootstrapChainPassiveRoot(c.Schema) != (root.PassiveService != nil) {
		return errors.New("bootstrap chain schema cannot convert legacy custody to passive observation")
	}
	if c.DeploymentId != contract.DeploymentId || c.DeploymentId != root.DeploymentId || c.Network != contract.Network || c.Network != root.Network ||
		c.RunDirectory != contract.RunDirectory || c.RunDirectory != root.RunDirectory || self.Plan.ContractPlanHash != contract.hash() || self.Plan.RootPlanHash != root.ContentHash {
		return errors.New("bootstrap chain child deployment, network, run directory or approved plan differs")
	}
	seen := map[string]bool{}
	for _, path := range self.protectedPaths() {
		passiveCheckpoint := root.PassiveService != nil && path == root.PassiveService.CheckpointPath && rootPassiveCheckpointWithin(path, c.RunDirectory)
		if !bootstrapRootAbsolutePath(path) || filepath.Dir(path) != c.RunDirectory && !passiveCheckpoint || seen[path] || seen[path+".lock"] {
			return errors.New("bootstrap chain journals or markers overlap or leave the approved run directory")
		}
		seen[path], seen[path+".lock"] = true, true
	}
	inputs := append([]string{self.Plan.ConfigPath, c.OwnerTrimPolicy.Path, c.OwnerTrimPlan.Path, c.Contracts.Path, c.Root.Path}, c.validatorConfigPaths()...)
	for _, path := range append(inputs, contract.Artifacts.Path, root.ServiceInput.Path) {
		if !bootstrapRootAbsolutePath(path) || seen[path] {
			return errors.New("bootstrap chain inputs overlap each other, a journal or a lock marker")
		}
		seen[path] = true
	}
	if bootstrapChainHasRootRole(c.Schema) {
		if rootObjectHash(self.Plan.RootInspection.Plan) != rootObjectHash(root) {
			return errors.New("bootstrap chain root inspection differs from the actual custody child")
		}
		path := c.RootValidator.Approval.Path
		if seen[path] {
			return errors.New("bootstrap chain root approval overlaps another input, journal or lock marker")
		}
		seen[path] = true
	}
	if c.Schema != bootstrapChainConfigSchemaV1 {
		return validateBootstrapChainValidatorPaths(self.Plan.ValidatorInspections, seen)
	}
	return nil
}

// The chain journal is included so initial claim and input separation share the
// exact same bounded destination set as the actual child owner construction.
func (self bootstrapChainPreparation) childPaths() []string {
	return append([]string{filepath.Join(self.Plan.Config.RunDirectory, bootstrapChainStateFile), filepath.Join(self.Contracts.Config.Plan.RunDirectory, evmCreateStateFile), filepath.Join(self.Root.RunDirectory, bootstrapRootProgressFile)}, self.Root.custodyChildPaths()...)
}

// Passive observation has no custody child, but its checkpoint must remain
// disjoint from every bootstrap input, journal, device and execution owner.
func (self bootstrapChainPreparation) protectedPaths() []string {
	paths := self.childPaths()
	if self.Root.PassiveService != nil {
		paths = append(paths, self.Root.PassiveService.CheckpointPath)
	}
	return paths
}

// These explicit schemas authenticate UR and root service roles; only v5 may
// name its sole UR hotkey as the root seat's hotkey. Older retained plans
// cannot gain this authority by adding fields or changing names.
func bootstrapChainHasRootRole(schema string) bool {
	return schema == bootstrapChainConfigSchema || schema == bootstrapChainConfigSchemaV4 || schema == bootstrapChainConfigSchemaV5
}

// v4 and v5 select passive root observation; v3 keeps legacy root custody.
func bootstrapChainPassiveRoot(schema string) bool {
	return schema == bootstrapChainConfigSchemaV4 || schema == bootstrapChainConfigSchemaV5
}

// Every bounded read checks the exact file bytes before its sole decode. No
// path reopen may substitute another approval between hash checking and use.
func readBootstrapChainInput(ctx context.Context, reference planFileReference, maximum int) ([]byte, error) {
	if !planSha256(reference.Sha256) {
		return nil, errors.New("bootstrap chain input pin is invalid")
	}
	raw, digest, err := readBootstrapRootFile(ctx, reference.Path, maximum)
	if err != nil || digest != reference.Sha256 {
		return nil, errors.Join(fmt.Errorf("bootstrap chain input %s differs from its exact pin", reference.Path), err)
	}
	return raw, nil
}

// Offline reconstruction checks internal consistency only. It cannot prove the
// retained census authentic, current, or safe at a future execution boundary.
func loadBootstrapChainPreparation(ctx context.Context, path string) (bootstrapChainPreparation, error) {
	var result bootstrapChainPreparation
	raw, digest, err := readBootstrapRootFile(ctx, path, rootServiceStoreLimit)
	if err != nil {
		return result, err
	}
	var config bootstrapChainConfig
	if err := decodePlanJson(raw, &config); err != nil {
		return result, err
	}
	if bootstrapChainPlanSchemaForConfig(config.Schema) == "" || len(config.Validators) != len(bootstrapChainValidatorRoles(config.Schema)) {
		return result, errors.New("bootstrap chain config requires its schema and exactly that schema's UR roles")
	}
	if !bootstrapChainHasRootRole(config.Schema) && config.RootValidator != nil {
		return result, errors.New("bootstrap chain v1/v2 cannot acquire root config inspection authority")
	}
	if !config.validOwnerTrimMode() {
		return result, fmt.Errorf("bootstrap chain owner_trim_mode is v5-only and its sole explicit value is %q", bootstrapChainOwnerTrimNone)
	}
	noTrim := config.noOwnerTrim()
	if err := bootstrapRootDirectory(config.RunDirectory); err != nil {
		return result, err
	}
	policyRaw, err := readBootstrapChainInput(ctx, config.OwnerTrimPolicy, maxRpcReplyBytes)
	if err != nil {
		return result, err
	}
	var policy subnetCensusPolicy
	if err := decodePlanJson(policyRaw, &policy); err != nil {
		return result, err
	}
	trimRaw, err := readBootstrapChainInput(ctx, config.OwnerTrimPlan, maximumOwnerTrimPlanBytes)
	if err != nil {
		return result, err
	}
	var trim ownerTrimPlan
	if err := decodePlanJson(trimRaw, &trim); err != nil {
		return result, err
	}
	if noTrim {
		err = validateBootstrapChainNoTrimReview(policy, config.OwnerTrimPolicy.Sha256, trim)
	} else {
		err = validateOwnerTrimGuardPlan(policy, config.OwnerTrimPolicy.Sha256, trim)
	}
	if err != nil {
		return result, err
	}
	for i := range trim.Census.Observation.Seats {
		seat := &trim.Census.Observation.Seats[i]
		seat.emission, err = strconv.ParseUint(seat.EmissionRao, 10, 64)
		if err != nil {
			return result, err
		}
	}
	rebuilt, err := buildOwnerTrimPlan(ctx, policy, trim.Census.Observation)
	if err != nil || rebuilt.ContentHash != trim.ContentHash {
		return result, errors.Join(errors.New("bootstrap chain trim selection differs from its retained census"), err)
	}
	contractRaw, err := readBootstrapChainInput(ctx, config.Contracts, 2*1024*1024)
	if err != nil {
		return result, err
	}
	var contracts evmPhaseConfig
	if err := decodePlanJson(contractRaw, &contracts); err != nil {
		return result, err
	}
	if err := contracts.validate(); err != nil {
		return result, err
	}
	result.Contracts, err = buildEvmCreatePlan(ctx, contracts, config.Contracts.Path)
	if err != nil {
		return result, err
	}
	rootRaw, err := readBootstrapChainInput(ctx, config.Root, rootServiceStoreLimit)
	if err != nil {
		return result, err
	}
	result.Root, err = decodeBootstrapRootPlan(ctx, config.Root.Path, rootRaw, config.Root.Sha256)
	if err != nil {
		return result, err
	}
	var rootInspection *bootstrapChainRootInspection
	if bootstrapChainHasRootRole(config.Schema) {
		rootInspection, err = loadBootstrapChainRootInspection(ctx, config, result.Root)
		if err != nil {
			return result, err
		}
	}
	scope := result.Root.identityScope()
	if config.Network != (planNetwork{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}) ||
		contracts.Plan.Runtime.RuntimeSourceCommit != policy.RuntimeSourceCommit || contracts.Plan.Runtime.RuntimeVersion != policy.RuntimeVersion ||
		contracts.Plan.Runtime.RuntimeCodeHash != policy.RuntimeCodeHash || contracts.Plan.Runtime.RuntimeMetadataHash != policy.RuntimeMetadataHash ||
		scope.RuntimeSourceCommit != policy.RuntimeSourceCommit || scope.RuntimeVersion != policy.RuntimeVersion || scope.RuntimeCodeHash != policy.RuntimeCodeHash || scope.RuntimeMetadataHash != policy.RuntimeMetadataHash {
		return result, errors.New("bootstrap chain trim, contract and root runtime/network expectations differ")
	}
	var inspections []validator.ProductionBootstrapInspection
	for _, role := range config.Validators {
		// Only v5's sole role may also hold the root seat, under the same owner.
		shared := config.Schema == bootstrapChainConfigSchemaV5 && role.Coldkey == scope.Coldkey
		if role.RegistrationBlock == nil || role.Hotkey == scope.Hotkey && !shared {
			return result, errors.New("bootstrap chain UR role is incomplete or reuses the separate root identity")
		}
		protected := slices.ContainsFunc(policy.Preserve, func(candidate subnetProtectedIdentity) bool {
			return candidate.Hotkey == role.Hotkey && candidate.Coldkey == role.Coldkey && *candidate.RegistrationBlock == *role.RegistrationBlock && slices.Contains(candidate.Roles, "ur-validator")
		})
		if noTrim {
			// No selection exists; the finalized census itself must hold the
			// exact generation as a preserved UR validator seat.
			if !protected || !bootstrapChainNoTrimCensusPreserves(trim.Census.Observation, role) {
				return result, errors.New("bootstrap chain UR validator generation is not explicitly protected and preserved in the retained no-trim census")
			}
		} else {
			survives := slices.ContainsFunc(trim.Best.Survivors, func(candidate subnetUidMapping) bool {
				return candidate.Hotkey == role.Hotkey && candidate.Coldkey == role.Coldkey && candidate.RegistrationBlock == *role.RegistrationBlock
			})
			if !protected || !survives {
				return result, errors.New("bootstrap chain UR validator generation is not explicitly protected and retained by the trim plan")
			}
		}
		configRaw, err := readBootstrapChainInput(ctx, role.Config, 2*1024*1024)
		if err != nil {
			return result, err
		}
		if config.Schema != bootstrapChainConfigSchemaV1 {
			// The sole launch config is signed before its coordinator and operator
			// exist, so only v5 admits its activation-pending evidence. Its producer
			// runs a separately re-approved rendered successor; v2-v4 stay strict.
			inspect := validator.InspectProductionBootstrapConfig
			if config.Schema == bootstrapChainConfigSchemaV5 {
				inspect = validator.InspectProductionBootstrapConfigPreActivation
			}
			inspection, err := inspect(ctx, role.Config.Path, configRaw)
			if err != nil {
				return result, fmt.Errorf("bootstrap chain validator %d production config: %w", role.ValidatorId, err)
			}
			pin := inspection.Approval.Proposal.Runtime
			if pin.Version != policy.RuntimeVersion || pin.SourceCommit != policy.RuntimeSourceCommit ||
				fmt.Sprintf("0x%x", pin.CodeHash) != policy.RuntimeCodeHash || fmt.Sprintf("0x%x", pin.MetadataHash) != policy.RuntimeMetadataHash ||
				fmt.Sprintf("0x%x", inspection.Approval.SubnetOwner) != policy.SubnetOwnerColdkey {
				return result, errors.New("bootstrap chain validator approved runtime/source or subnet owner differs from independent trim and child scope")
			}
			inspections = append(inspections, *inspection)
		}
	}
	if slices.ContainsFunc(policy.Remove, func(candidate subnetIdentityExpectation) bool {
		return strings.EqualFold(candidate.Hotkey, scope.Hotkey) || strings.EqualFold(candidate.Hotkey, contracts.Plan.ReserveHotkey)
	}) {
		return result, errors.New("bootstrap chain trim requests removal of the root or reserve custody identity")
	}
	if !slices.ContainsFunc(trim.Census.Observation.RootRegistrations, func(candidate subnetRegistration) bool {
		return candidate.Uid == scope.Seat.Uid && candidate.Hotkey == scope.Hotkey && candidate.Coldkey == scope.Coldkey && candidate.RegistrationBlock == scope.Seat.RegistrationBlock
	}) {
		return result, errors.New("bootstrap chain separate root generation differs from the retained excluded-root census")
	}
	result.Plan = bootstrapChainPlan{Schema: bootstrapChainPlanSchemaForConfig(config.Schema), ConfigPath: path, ConfigSha256: digest, Config: config,
		OwnerTrimContentHash: trim.ContentHash, OwnerTrimBlockers: trim.ExecutionBlockers, ContractPlanHash: contracts.Plan.hash(), RootPlanHash: result.Root.ContentHash,
		ValidatorInspections: inspections, RootInspection: rootInspection, PendingChainPhases: bootstrapChainPendingPhasesForSchema(config.Schema),
		OwnerTrimPhase: config.ownerTrimPhase()}
	result.Plan.ContentHash = bootstrapChainPlanHash(result.Plan)
	return result, errors.Join(result.validate(), ctx.Err())
}
