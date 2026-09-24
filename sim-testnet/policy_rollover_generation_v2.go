//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/clientauth"
	validatorcomponent "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
	"gopkg.in/yaml.v3"
)

// A generation starts a different signed ledger. Its keys and paths can never
// imply a continuation of a retained VPK's sequence, roots, or EMA history.
func policyRolloverGenerationRolesV2(cfg *ResolvedConfig, originalRoles *RoleSecrets, generation uint64) (*RoleSecrets, error) {
	if err := validatePolicyRolloverGenerationOwnerV2(cfg, originalRoles, generation); err != nil {
		return nil, err
	}
	roles := cloneRoleSecrets(originalRoles)
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			if _, _, err := runtimeEvidenceActivationKeysV2(originalRoles, uint64(validatorID), uint64(noID)); err != nil {
				return nil, err
			}
			label := fmt.Sprintf("validator-%d-no-%d", validatorID, noID)
			seed := derive32(cfg, fmt.Sprintf("client/%s/evidence-generation/%d", label, generation))
			key := ed25519.NewKeyFromSeed(seed[:])
			role := ClientRoleSecret{Label: label, SeedHex: hex.EncodeToString(seed[:]), PublicKeyHex: hex.EncodeToString(key[ed25519.SeedSize:])}
			if role.PublicKeyHex == originalRoles.Clients[label].PublicKeyHex {
				return nil, errors.New("evidence generation cannot reuse the prior validator public key")
			}
			roles.Clients[label] = role
		}
	}
	return roles, nil
}

func validatePolicyRolloverGenerationOwnerV2(cfg *ResolvedConfig, roles *RoleSecrets, generation uint64) error {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Release == nil || cfg.WalletMaterial == "" || generation == 0 || roles == nil ||
		roles.Schema != "urnetwork-sim-role-secrets-v1" || roles.DeploymentID == "" || roles.DeploymentID != cfg.Config.Deployment.DeploymentID {
		return errors.New("policy rollover generation requires its configured deployment, nonzero generation and retained roles")
	}
	return validateSimulatorEvidenceV2Census(cfg.Config)
}

func validatePolicyRolloverGenerationRolesV2(cfg *ResolvedConfig, roles *RoleSecrets, generation uint64) error {
	if err := validatePolicyRolloverGenerationOwnerV2(cfg, roles, generation); err != nil {
		return err
	}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			label := fmt.Sprintf("validator-%d-no-%d", validatorID, noID)
			seed := derive32(cfg, fmt.Sprintf("client/%s/evidence-generation/%d", label, generation))
			role := roles.Clients[label]
			if role.Label != label || role.SeedHex != hex.EncodeToString(seed[:]) {
				return errors.New("policy rollover client key does not belong to the requested generation")
			}
			if _, _, err := runtimeEvidenceActivationKeysV2(roles, uint64(validatorID), uint64(noID)); err != nil {
				return err
			}
		}
	}
	return nil
}

func policyRolloverGenerationRootV2(stateDir string, validatorID, generation uint64) string {
	return filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "evidence-generations", fmt.Sprintf("generation-%020d", generation))
}

func policyRolloverGenerationCommonRootV2(stateDir string, generation uint64) string {
	return filepath.Join(stateDir, "evidence-generations", fmt.Sprintf("generation-%020d", generation))
}

func policyRolloverGenerationStateDirV2(stateDir string, validatorID, generation uint64) string {
	return filepath.Join(policyRolloverGenerationRootV2(stateDir, validatorID, generation), "coordinator-state-v2")
}

func policyRolloverGenerationOperatorDirV2(stateDir string, validatorID, noID, generation uint64) string {
	return filepath.Join(policyRolloverGenerationRootV2(stateDir, validatorID, generation), "state", "operators", fmt.Sprintf("no-%d", noID))
}

type policyRolloverGenerationClientsV2 struct {
	Schema       string                      `json:"schema"`
	DeploymentID string                      `json:"deployment_id"`
	Generation   uint64                      `json:"generation"`
	Clients      map[string]ClientRoleSecret `json:"clients"`
}

func policyRolloverGenerationClientsBytesV2(roles *RoleSecrets, validators, operators int, generation uint64) ([]byte, error) {
	manifest := policyRolloverGenerationClientsV2{Schema: "urnetwork-sim-evidence-generation-clients-v2", DeploymentID: roles.DeploymentID, Generation: generation, Clients: map[string]ClientRoleSecret{}}
	for validatorID := 1; validatorID <= validators; validatorID++ {
		for noID := 1; noID <= operators; noID++ {
			label := fmt.Sprintf("validator-%d-no-%d", validatorID, noID)
			manifest.Clients[label] = roles.Clients[label]
		}
	}
	wire, err := json.Marshal(manifest)
	return append(wire, '\n'), err
}

// This explicit lifecycle mutation creates independent API client identities.
// The parent rollover journal authorizes invocation. A crash retries each
// durable client JWT; neither original role secrets nor original JWTs change.
func provisionPolicyRolloverGenerationClientsV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, generation uint64, roles *RoleSecrets) error {
	return provisionPolicyRolloverGenerationClientsWithV2(ctx, cfg, stateDir, generation, roles, provisionClientJWT)
}

func provisionPolicyRolloverGenerationClientsWithV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, generation uint64, roles *RoleSecrets, provision func(context.Context, string, string, string, string) (connect.Id, error)) error {
	if ctx == nil || provision == nil {
		return errors.New("policy rollover client provisioning owner is absent")
	}
	if err := validatePolicyRolloverGenerationRolesV2(cfg, roles, generation); err != nil {
		return err
	}
	if err := validatorcomponent.ValidateReleaseEvidenceV2Path(stateDir); err != nil {
		return err
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return err
	}
	type clientInput struct {
		label, root   string
		priorClientID string
		network, seed []byte
		noID          int
	}
	inputs := []clientInput{}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			label := fmt.Sprintf("validator-%d-no-%d", validatorID, noID)
			original := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "operators", fmt.Sprintf("no-%d", noID), "network.jwt")
			network, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, original, limit)
			if err != nil || len(bytes.TrimSpace(network)) == 0 {
				return errors.Join(errors.New("policy rollover requires the retained operator network credential"), err)
			}
			seed, _ := hex.DecodeString(roles.Clients[label].SeedHex)
			root := policyRolloverGenerationOperatorDirV2(stateDir, uint64(validatorID), uint64(noID), generation)
			priorClientID := roles.Clients[label].ClientIDHex
			storedToken, tokenErr := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(root, "client.jwt"), limit)
			if tokenErr == nil {
				storedID, err := clientauth.ClientIdFromJwt(strings.TrimSpace(string(storedToken)))
				if err != nil || storedID == (connect.Id{}) {
					return errors.Join(errors.New("policy rollover retained client JWT is invalid"), err)
				}
				storedHex := hex.EncodeToString(storedID[:])
				if priorClientID != "" && priorClientID != storedHex {
					return errors.New("policy rollover client role differs from its durable JWT")
				}
				priorClientID = storedHex
			} else if !validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(tokenErr) {
				return tokenErr
			}
			for path, data := range map[string][]byte{filepath.Join(root, "network.jwt"): network, filepath.Join(root, "client.key"): seed} {
				if err := preflightPolicyRolloverGenerationFileV2(ctx, path, data, limit); err != nil {
					return err
				}
			}
			inputs = append(inputs, clientInput{label: label, root: root, priorClientID: priorClientID, network: network, seed: seed, noID: noID})
		}
	}
	for _, input := range inputs {
		for path, data := range map[string][]byte{filepath.Join(input.root, "network.jwt"): input.network, filepath.Join(input.root, "client.key"): input.seed} {
			if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, path, data, limit); err != nil {
				return err
			}
		}
		id, err := provision(ctx, fmt.Sprintf("http://127.0.0.1:%d", 18080+input.noID), filepath.Join(input.root, "network.jwt"), filepath.Join(input.root, "client.jwt"), fmt.Sprintf("sim-testnet %s evidence generation %d", input.label, generation))
		if err != nil {
			return err
		}
		role := roles.Clients[input.label]
		if id == (connect.Id{}) || input.priorClientID != "" && input.priorClientID != hex.EncodeToString(id[:]) {
			return errors.New("policy rollover API client identity changed during provisioning")
		}
		role.ClientIDHex = hex.EncodeToString(id[:])
		roles.Clients[input.label] = role
	}
	wire, err := policyRolloverGenerationClientsBytesV2(roles, cfg.Config.Topology.Validators, cfg.Config.Topology.Operators, generation)
	if err != nil {
		return err
	}
	_, err = validatorcomponent.WriteReleaseEvidenceV2File(ctx, filepath.Join(policyRolloverGenerationCommonRootV2(stateDir, generation), "clients.json"), wire, limit)
	return err
}

func preflightPolicyRolloverGenerationFileV2(ctx context.Context, path string, want []byte, limit uint64) error {
	if len(want) == 0 || uint64(len(want)) > limit {
		return errors.New("policy rollover generation input exceeds its explicit bound")
	}
	have, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, path, limit)
	if validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(have, want) {
		return errors.New("policy rollover generation file already contains different bytes")
	}
	return nil
}

// Stage only fixed inputs for a complete freshly activated census. Finalized
// native eligibility, policy snapshots and on-chain activation receipts are
// authenticated by the caller and again by normal validator startup. No state
// migration, EMA import, process selection or active pointer is performed here.
func stagePolicyRolloverGenerationV2(ctx context.Context, cfg *ResolvedConfig, basePlan *SetupPlan, stateDir string, generation uint64, roles *RoleSecrets, members []runtimeEvidenceActivationMemberV2, boundary ChainHead) ([]policyRolloverValidatorHandoffV2, error) {
	if ctx == nil {
		return nil, errors.New("policy rollover generation context is absent")
	}
	if err := validatePolicyRolloverGenerationRolesV2(cfg, roles, generation); err != nil {
		return nil, err
	}
	if err := validatorcomponent.ValidateReleaseEvidenceV2Path(stateDir); err != nil {
		return nil, err
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	if basePlan == nil || basePlan.ValidatorEvidence == nil || basePlan.DeploymentID != roles.DeploymentID || basePlan.ChainID != testnetChainID || basePlan.Netuid != cfg.Netuid || len(members) != cfg.Config.Topology.Validators*cfg.Config.Topology.Operators {
		return nil, errors.New("policy rollover generation requires the exact deployment and complete activation census")
	}
	cfg, err = evidenceRelaySourceCapacityConfig(cfg, basePlan)
	if err != nil {
		return nil, err
	}
	first := members[0].Activation
	prepared := &runtimeEvidenceActivationPreparedV2{Schema: "urnetwork-sim-evidence-activation-prepared-v2", PlanHash: basePlan.PlanHash, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Epoch: first.Domain.Epoch,
		Native: ChainHead{Number: first.NativeBlock, Hash: common.Hash(first.NativeHash).Hex()}, Evm: ChainHead{Number: first.EVMBlock, Hash: common.Hash(first.EVMHash).Hex()}, Members: members}
	completed := &runtimeEvidenceActivationCompletedV2{Schema: "urnetwork-sim-evidence-activation-completed-v2", PlanHash: basePlan.PlanHash, Boundary: boundary}
	evidence, originalInputs, err := runtimeEvidenceFixedInputsV2(cfg, basePlan, stateDir, roles, prepared, completed)
	if err != nil {
		return nil, err
	}
	for index, member := range members {
		if index%cfg.Config.Topology.Operators != 0 && member.ValidatorUid != members[index-index%cfg.Config.Topology.Operators].ValidatorUid {
			return nil, errors.New("policy rollover generation members disagree on validator UID")
		}
	}
	contracts, err := loadContractDeployment(stateDir)
	if err != nil {
		return nil, err
	}
	if contracts.CoordinatorProxy != basePlan.ValidatorEvidence.Coordinator || contracts.SettlementVault != basePlan.ValidatorEvidence.SettlementVault {
		return nil, errors.New("policy rollover generation deployment differs from its evidence journal domain")
	}
	eventSyncBlock, err := contractDeploymentEventSyncBlock(contracts)
	if err != nil {
		return nil, err
	}
	if err := validateRuntimeOperatorApiOrigins(cfg); err != nil {
		return nil, err
	}
	clientsBytes, err := policyRolloverGenerationClientsBytesV2(roles, cfg.Config.Topology.Validators, cfg.Config.Topology.Operators, generation)
	if err != nil {
		return nil, err
	}
	retainedClients, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(policyRolloverGenerationCommonRootV2(stateDir, generation), "clients.json"), limit)
	if err != nil || !bytes.Equal(clientsBytes, retainedClients) {
		return nil, errors.Join(errors.New("policy rollover generation client provisioning is incomplete or changed"), err)
	}
	publicBytes, err := json.Marshal(roles.Public())
	if err != nil {
		return nil, err
	}
	publicBytes = append(publicBytes, '\n')
	identitiesPath := filepath.Join(policyRolloverGenerationCommonRootV2(stateDir, generation), "public", "identities.json")
	identities := policyRolloverGenerationReferenceV2(identitiesPath, publicBytes)
	type stagedFile struct {
		path  string
		data  []byte
		limit uint64
	}
	files := []stagedFile{{identitiesPath, publicBytes, limit}}
	handoffs := []policyRolloverValidatorHandoffV2{}
	base := runtimeComponentConfigBase(cfg, contracts, eventSyncBlock)
	for index := range evidence {
		validatorID := uint64(index + 1)
		root := policyRolloverGenerationRootV2(stateDir, validatorID, generation)
		for operatorIndex := range evidence[index].Evidence.Operators {
			operator := &evidence[index].Evidence.Operators[operatorIndex]
			oldFiles := operator.Files()
			newFiles := make([]validatorcomponent.ReleaseEvidenceV2File, len(oldFiles))
			for fileIndex, old := range oldFiles {
				path := filepath.Join(root, "evidence-v2", fmt.Sprintf("no-%d", operator.NoID), filepath.Base(old.Path))
				data := slices.Clone(originalInputs[old.Path])
				newFiles[fileIndex] = policyRolloverGenerationReferenceV2(path, data)
				files = append(files, stagedFile{path, data, runtimeEvidenceV2ReferenceLimit(evidence[index].Evidence.Bounds, fileIndex)})
			}
			operator.Activation, operator.VPKSignature, operator.HotkeySignature, operator.Context, operator.History = newFiles[0], newFiles[1], newFiles[2], newFiles[3], newFiles[4]
			operator.ReplayScratchRoot = filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", operator.NoID), "replay")
			operator.SealScratchRoot = filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", operator.NoID), "seal")
			label := fmt.Sprintf("validator-%d-no-%d", validatorID, operator.NoID)
			role := roles.Clients[label]
			clientRoot := policyRolloverGenerationOperatorDirV2(stateDir, validatorID, operator.NoID, generation)
			token, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(clientRoot, "client.jwt"), limit)
			if err != nil {
				return nil, err
			}
			id, err := clientauth.ClientIdFromJwt(strings.TrimSpace(string(token)))
			if err != nil || id == (connect.Id{}) || hex.EncodeToString(id[:]) != role.ClientIDHex {
				return nil, errors.Join(errors.New("policy rollover generation client JWT differs from provisioned identity"), err)
			}
			seed, _ := hex.DecodeString(role.SeedHex)
			if err := preflightPolicyRolloverGenerationFileV2(ctx, filepath.Join(clientRoot, "client.key"), seed, ed25519.SeedSize); err != nil {
				return nil, err
			}
			files = append(files, stagedFile{filepath.Join(clientRoot, "client.key"), seed, ed25519.SeedSize})
		}
		renderConfig := *cfg
		// This independent ledger starts under the current policy and carries
		// no predecessor policy or replay authority from the retained source.
		renderConfig.previousPolicy = nil
		renderHarness := *cfg.Config
		renderHarness.ValidatorEvidenceV2 = evidence
		renderConfig.Config = &renderHarness
		wire, err := marshalRuntimeValidatorConfig(&renderConfig, stateDir, roles, base, int(validatorID))
		if err != nil {
			return nil, err
		}
		var release validatorcomponent.ReleaseConfig
		if err := yaml.Unmarshal(wire, &release); err != nil {
			return nil, err
		}
		release.StateDir = policyRolloverGenerationStateDirV2(stateDir, validatorID, generation)
		release.HotkeySeedFile = filepath.Join(root, "hotkey.seed")
		for opIndex := range release.Operators {
			op := &release.Operators[opIndex]
			op.StateDir = policyRolloverGenerationOperatorDirV2(stateDir, validatorID, op.NoID, generation)
			op.NetworkJWTFile, op.ClientJWTFile, op.ClientKeySeedFile = filepath.Join(op.StateDir, "network.jwt"), filepath.Join(op.StateDir, "client.jwt"), filepath.Join(op.StateDir, "client.key")
		}
		if err := release.Validate(); err != nil {
			return nil, err
		}
		wire, err = yaml.Marshal(release)
		if err != nil {
			return nil, err
		}
		configPath := filepath.Join(root, "validator.yml")
		if err := preflightPolicyRolloverGenerationNamespaceV2(ctx, configPath, wire, release.StateDir, filepath.Join(root, "state"), cfg.Config.Topology.Operators, limit); err != nil {
			return nil, err
		}
		files = append(files, stagedFile{release.HotkeySeedFile, []byte("0x" + roles.Substrate[validatorHotkeyLabel(int(validatorID))].SeedHex), 66}, stagedFile{configPath, wire, limit})
		handoffs = append(handoffs, policyRolloverValidatorHandoffV2{ValidatorID: validatorID, PreviousStateDir: filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "coordinator-state-v2"), StateDir: release.StateDir, ClientStateDir: filepath.Join(root, "state"), Evidence: evidence[index].Evidence, Config: policyRolloverGenerationReferenceV2(configPath, wire), Identities: identities})
	}
	for _, file := range files {
		if err := preflightPolicyRolloverGenerationFileV2(ctx, file.path, file.data, file.limit); err != nil {
			return nil, err
		}
	}
	for _, file := range files {
		if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, file.path, file.data, file.limit); err != nil {
			return nil, err
		}
	}
	return handoffs, nil
}

func policyRolloverGenerationReferenceV2(path string, data []byte) validatorcomponent.ReleaseEvidenceV2File {
	return validatorcomponent.ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(data)), SHA256: fmt.Sprintf("0x%x", sha256.Sum256(data))}
}

// An exact static retry may coexist with that generation's later signed state.
// Before its first config is published, any dynamic state is an unexplained
// authority and blocks adoption. Nothing is deleted or relabelled in either case.
func preflightPolicyRolloverGenerationNamespaceV2(ctx context.Context, configPath string, wire []byte, coordinator, clients string, operators int, limit uint64) error {
	prior, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, configPath, limit)
	if err == nil {
		if !bytes.Equal(prior, wire) {
			return errors.New("policy rollover generation config already differs")
		}
		return nil
	}
	if !validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return err
	}
	legacy, signed, err := classifyValidatorAttemptState(clients, operators)
	if err != nil || legacy || signed {
		return errors.Join(errors.New("policy rollover generation has unexplained preexisting measurement state"), err)
	}
	entries, err := os.ReadDir(coordinator)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(entries) != 0 {
		return errors.New("policy rollover generation has unexplained preexisting coordinator state")
	}
	return nil
}
