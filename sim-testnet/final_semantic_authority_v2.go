//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	validatorpkg "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

const finalValidatorAuthorityV2Schema = "urnetwork-sim-validator-source-authority-v2"

// Only public configuration is captured. ResolvedConfig contains wallet
// material and must never be serialized. Prepared/completed remain byte arrays
// so nested JSON indentation cannot replace their original signed-file pins.
type finalValidatorAuthorityV2 struct {
	Schema          string                   `json:"schema"`
	StateRoot       string                   `json:"original_state_root"`
	Config          *HarnessConfig           `json:"config"`
	Public          *PublicManifest          `json:"public_manifest"`
	Hyperparameters *Hyperparameters         `json:"hyperparameters"`
	Resolved        resolvedPlanPublicInputs `json:"resolved_public_inputs"`
	Prepared        []byte                   `json:"prepared_bytes"`
	Completed       []byte                   `json:"completed_bytes"`
}

func captureFinalValidatorAuthorityV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string) ([]byte, error) {
	if cfg == nil || cfg.Config == nil || !cfg.Config.ProvisionValidatorEvidenceV2 {
		return nil, errors.New("final V2 authority requires the approved simulator provisioning template")
	}
	hash, err := releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil || hash != cfg.ConfigHash {
		return nil, errors.Join(errors.New("final V2 source template differs from its approved config hash"), err)
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	var prepared runtimeEvidenceActivationPreparedV2
	preparedBytes, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(stateRoot, "evidence-v2-setup", "prepared.json"), limit, &prepared)
	if err != nil {
		return nil, err
	}
	var completed runtimeEvidenceActivationCompletedV2
	completedBytes, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(stateRoot, "evidence-v2-setup", "completed.json"), limit, &completed)
	if err != nil {
		return nil, err
	}
	if completed.PreparedHash != fmt.Sprintf("0x%x", sha256.Sum256(preparedBytes)) || prepared.PlanHash != completed.PlanHash {
		return nil, errors.New("final V2 setup byte lineage differs")
	}
	return json.Marshal(finalValidatorAuthorityV2{Schema: finalValidatorAuthorityV2Schema, StateRoot: stateRoot, Config: cfg.Config, Public: cfg.Public, Hyperparameters: cfg.Hyperparameters, Resolved: resolvedPlanInputs(cfg), Prepared: preparedBytes, Completed: completedBytes})
}

// Reconstruct the actual renderer from approved public inputs. File refs,
// origins, bounds, policy and initial EMA authority are derived separately
// from the candidate runtime config; that config supplies no expected values.
func finalValidatorConfigAuthorityV2(ctx context.Context, evidence *FinalSemanticEvidence, current, source *SetupPlan, sourceBytes, runtimeBytes, manifestBytes, publicBytes, identityBytes, policyBytes, releaseBytes []byte, validatorID uint64) (*validatorpkg.ReleaseConfig, *finalValidatorAuthorityV2, error) {
	if ctx == nil || evidence == nil || current == nil || source == nil || validatorID == 0 || validatorID > uint64(evidence.ExpectedValidators) {
		return nil, nil, errors.New("final V2 source authority is incomplete")
	}
	var authority finalValidatorAuthorityV2
	decoder := json.NewDecoder(bytes.NewReader(sourceBytes))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&authority); err != nil {
		return nil, nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("final V2 authority has trailing bytes")
	}
	if authority.Schema != finalValidatorAuthorityV2Schema || authority.Config == nil || authority.Public == nil || authority.Hyperparameters == nil || !filepath.IsAbs(authority.StateRoot) || filepath.Clean(authority.StateRoot) != authority.StateRoot {
		return nil, nil, errors.New("final V2 authority shape differs")
	}
	configHash, err := releaseConfigHash(authority.Config, authority.Public, authority.Hyperparameters)
	if err != nil || configHash != evidence.ConfigHash || current.PlanHash != evidence.PlanHash || current.ConfigHash != configHash || source.ConfigHash != configHash || current.PolicyHash != evidence.PolicyHash || source.PolicyHash != evidence.PolicyHash || current.DeploymentID != evidence.DeploymentID || source.DeploymentID != evidence.DeploymentID || !current.allowedPlanHashes()[source.PlanHash] || current.ChainID != evidence.ChainID || source.ChainID != evidence.ChainID || current.Netuid != evidence.Netuid || source.Netuid != evidence.Netuid {
		return nil, nil, errors.Join(errors.New("final V2 public template or source plan differs from approval"), err)
	}
	if authority.Config.Deployment.DeploymentID != evidence.DeploymentID || authority.Public.Chain.ChainID != evidence.ChainID || !strings.EqualFold(authority.Public.Chain.GenesisHash, evidence.GenesisHash) || authority.Config.Topology.Validators != evidence.ExpectedValidators || authority.Config.Topology.Operators != evidence.ExpectedOperators || !authority.Config.ProvisionValidatorEvidenceV2 {
		return nil, nil, errors.New("final V2 template changes deployment or source census")
	}
	if err := validatorEvidenceSourcePlanMatches(current, source); err != nil {
		return nil, nil, err
	}
	var policy protocol.Policy
	if err := decodeStrictJSONBytes(policyBytes, &policy); err != nil {
		return nil, nil, err
	}
	policyHash, err := policy.HashHex()
	if err != nil || policyHash != evidence.PolicyHash {
		return nil, nil, errors.Join(errors.New("final V2 policy bytes differ"), err)
	}
	var releaseLock ReleaseLock
	releaseDecoder := yaml.NewDecoder(bytes.NewReader(releaseBytes))
	releaseDecoder.KnownFields(true)
	if err := releaseDecoder.Decode(&releaseLock); err != nil {
		return nil, nil, err
	}
	if err := releaseDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("final V2 release lock has trailing bytes")
	}
	releaseHash, err := canonicalHashHex(&releaseLock)
	if err != nil || releaseHash != current.ReleaseLockHash {
		return nil, nil, errors.Join(errors.New("final V2 release lock differs from its exact approved source"), err)
	}
	var public PublicDeploymentManifest
	if err := decodeStrictJSONBytes(publicBytes, &public); err != nil {
		return nil, nil, err
	}
	if public.DeploymentID != evidence.DeploymentID || public.PlanHash != evidence.PlanHash || public.ConfigHash != evidence.ConfigHash || public.PolicyHash != evidence.PolicyHash || public.ChainID != evidence.ChainID || public.Netuid != evidence.Netuid || public.Contracts == nil || public.Contracts.DeploymentID != evidence.DeploymentID || public.Contracts.CoordinatorProxy != common.HexToAddress(evidence.Deployment.CoordinatorProxy) || public.Contracts.SettlementVault != common.HexToAddress(evidence.Deployment.SettlementVault) || public.Contracts.ReserveSink != common.HexToAddress(evidence.Deployment.ReserveSink) {
		return nil, nil, errors.New("final V2 public deployment differs from sealed contract authority")
	}
	paths, err := decodeFinalOperatorPathAuthority(identityBytes, evidence.DeploymentID, evidence.ExpectedValidators, evidence.ExpectedOperators)
	if err != nil {
		return nil, nil, err
	}
	keys := func(id, noID uint64) ([32]byte, [32]byte, error) {
		hotkey, err := decodeHex32("final V2 original hotkey", paths.identities.Substrate[validatorHotkeyLabel(int(id))].PublicKey)
		if err != nil {
			return [32]byte{}, [32]byte{}, err
		}
		key := paths.keysByValidator[id][noID]
		if len(key) != 32 {
			return [32]byte{}, [32]byte{}, errors.New("final V2 original path identity is absent")
		}
		return hotkey, [32]byte(key), nil
	}
	var prepared runtimeEvidenceActivationPreparedV2
	var completed runtimeEvidenceActivationCompletedV2
	if err := decodeStrictJSONBytes(authority.Prepared, &prepared); err != nil {
		return nil, nil, err
	}
	if err := decodeStrictJSONBytes(authority.Completed, &completed); err != nil {
		return nil, nil, err
	}
	if prepared.PlanHash != source.PlanHash || completed.PlanHash != source.PlanHash || completed.PreparedHash != fmt.Sprintf("0x%x", sha256.Sum256(authority.Prepared)) {
		return nil, nil, errors.New("final V2 setup changed its original approval or completed byte pin")
	}
	resolvedHash, err := canonicalHashHex(authority.Resolved)
	if err != nil || resolvedHash != current.ResolvedInputsHash || authority.Resolved.ChainID != evidence.ChainID || authority.Resolved.Netuid != evidence.Netuid {
		return nil, nil, errors.Join(errors.New("final V2 resolved origins or public authority differ from approval"), err)
	}
	resolved := &ResolvedConfig{Config: authority.Config, Public: authority.Public, Hyperparameters: authority.Hyperparameters, Policy: &policy, Release: &releaseLock, ConfigHash: configHash, PolicyHash: policyHash, ChainID: evidence.ChainID, Netuid: evidence.Netuid, OperationalRPCMode: authority.Resolved.OperationalRPCMode}
	if current.OwnedRPCAuthority != "" {
		resolved.OperationalRPCMode = rpcModePrivateAuthority
	}
	for _, endpoint := range authority.Resolved.OperatorAPIOrigins {
		resolved.OperatorAPIOrigins = append(resolved.OperatorAPIOrigins, endpoint)
	}
	if len(resolved.OperatorAPIOrigins) != evidence.ExpectedOperators || len(public.Operators) != evidence.ExpectedOperators {
		return nil, nil, errors.New("final V2 source origin census differs")
	}
	for index, operator := range public.Operators {
		if operator.NoID != index+1 || operator.APIURL != resolved.OperatorAPIOrigins[index] {
			return nil, nil, errors.New("final V2 source origin differs from public deployment")
		}
	}
	// Preserve integers beyond JavaScript's precise range while checking the
	// approved hash, then provide exact uint64 values to the ordinary renderer.
	for key, value := range authority.Hyperparameters.OwnerControlled {
		if number, ok := value.(json.Number); ok {
			parsed, err := strconv.ParseUint(string(number), 10, 64)
			if err != nil {
				return nil, nil, errors.New("final V2 owner hyperparameter is not an exact unsigned integer")
			}
			authority.Hyperparameters.OwnerControlled[key] = parsed
		}
	}
	values, inputs, err := runtimeEvidenceFixedPublicInputsV2(resolved, source, authority.StateRoot, &prepared, &completed, keys)
	if err != nil {
		return nil, nil, err
	}
	copied := *resolved.Config
	copied.ValidatorEvidenceV2 = values
	resolved.Config = &copied
	roles := &RoleSecrets{EVM: map[string]EVMRoleSecret{}}
	for label, address := range paths.identities.EVM {
		roles.EVM[label] = EVMRoleSecret{Address: address}
	}
	start, err := contractDeploymentEventSyncBlock(public.Contracts)
	if err != nil {
		return nil, nil, err
	}
	expected, err := marshalRuntimeValidatorConfig(resolved, authority.StateRoot, roles, runtimeComponentConfigBase(resolved, public.Contracts, start), int(validatorID))
	if err != nil || !bytes.Equal(expected, runtimeBytes) {
		return nil, nil, errors.Join(errors.New("final V2 runtime config differs from deterministic approved rendering"), err)
	}
	var runtimeManifest RuntimeConfigManifest
	if err := decodeStrictJSONBytes(manifestBytes, &runtimeManifest); err != nil {
		return nil, nil, err
	}
	manifestHash, err := runtimeConfigManifestHash(runtimeManifest)
	if err != nil || runtimeManifest.ManifestHash != manifestHash || runtimeManifest.DeploymentID != evidence.DeploymentID || runtimeManifest.ConfigHash != evidence.ConfigHash || runtimeManifest.PolicyHash != evidence.PolicyHash {
		return nil, nil, errors.Join(errors.New("final V2 runtime inventory authority differs"), err)
	}
	configPath := filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("validator-%d", validatorID), "validator.yml"))
	matched := 0
	for _, file := range runtimeManifest.Files {
		if file.Path == configPath {
			if file.SHA256 != bytesSHA256(runtimeBytes) || file.Mode != "0600" {
				return nil, nil, errors.New("final V2 runtime config differs from captured rendered inventory")
			}
			matched++
		}
	}
	if matched != 1 {
		return nil, nil, errors.New("final V2 runtime inventory config is missing or duplicated")
	}
	var runtime validatorpkg.ReleaseConfig
	runtimeDecoder := yaml.NewDecoder(bytes.NewReader(runtimeBytes))
	runtimeDecoder.KnownFields(true)
	if err := runtimeDecoder.Decode(&runtime); err != nil {
		return nil, nil, err
	}
	if err := runtimeDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("final V2 runtime config has trailing YAML")
	}
	if !reflect.DeepEqual(runtime.EvidenceV2, values[validatorID-1].Evidence) {
		return nil, nil, errors.New("final V2 runtime changed fixed original references")
	}
	for _, operator := range runtime.EvidenceV2.Operators {
		for _, reference := range operator.Files() {
			if bytesSHA256(inputs[reference.Path]) != "sha256:"+strings.TrimPrefix(reference.SHA256, "0x") {
				return nil, nil, errors.New("final V2 reconstructed fixed source pin differs")
			}
		}
	}
	// The release loader normally canonicalizes address case before signed
	// measurement comparison. Keep the captured YAML unchanged above.
	runtime.Coordinator = strings.ToLower(runtime.Coordinator)
	runtime.SettlementVault = strings.ToLower(runtime.SettlementVault)
	if err := runtime.Validate(); err != nil {
		return nil, nil, err
	}
	return &runtime, &authority, ctx.Err()
}
