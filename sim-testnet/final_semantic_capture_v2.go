//go:build linux || darwin

// Compact capture is a lossless live/offline boundary. Original signed bytes
// remain separate from the legacy materialized trail representation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	validatorpkg "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

const finalCollectedValidatorEvidenceV2Schema = "urnetwork-sim-validator-raw-evidence-v2"

// Every source is copied through the existing immutable campaign archive.
// Origin and private name are provenance; only Artifact locates retained bytes.
type FinalCollectedValidatorSourceV2 struct {
	Source   validatorpkg.ReleaseEvidenceV2CaptureSource `json:"source"`
	Artifact FinalArtifactLocator                        `json:"artifact"`
}

// Source-object counts are custody counts, never claimed trail/proof totals.
// The pending status cannot satisfy independent final semantic acceptance.
type FinalCollectedValidatorEvidenceV2 struct {
	Schema            string                            `json:"schema"`
	SemanticStatus    string                            `json:"semantic_status"`
	Hotkey            string                            `json:"hotkey"`
	Origins           [2]string                         `json:"origins"`
	Sources           []FinalCollectedValidatorSourceV2 `json:"sources"`
	Closures          []FinalCollectedSettlementClosure `json:"closures"`
	NativeCheckpoints []FinalNativeCheckpointV2         `json:"native_checkpoints,omitempty"`
}

// Absence is legacy configuration, not permission to fall back after a V2
// read, custody, signature or historical chain authentication failure.
func finalUsesEvidenceV2(cfg *ResolvedConfig) bool {
	return cfg != nil && cfg.Config != nil && len(cfg.Config.ValidatorEvidenceV2) != 0
}

// Reads the exact private rendered config and compares its source references
// to the independently reconstructed plan-owned setup. No seed is archived.
func finalReleaseCaptureConfigV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, validatorId uint64) (*validatorpkg.ReleaseConfig, []byte, error) {
	release, raw, _, err := finalReleaseCaptureConfigWithAdoptionV2(ctx, cfg, stateRoot, validatorId)
	return release, raw, err
}

func finalReleaseCaptureConfigWithAdoptionV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, validatorId uint64) (*validatorpkg.ReleaseConfig, []byte, []byte, error) {
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateRoot)
	if err != nil {
		return nil, nil, nil, err
	}
	var expected *validatorpkg.ReleaseEvidenceV2Config
	for index := range resolved.Config.ValidatorEvidenceV2 {
		if resolved.Config.ValidatorEvidenceV2[index].ValidatorID == validatorId {
			expected = &resolved.Config.ValidatorEvidenceV2[index].Evidence
			break
		}
	}
	if expected == nil {
		return nil, nil, nil, errors.New("compact capture configured validator is absent")
	}
	path := filepath.Join(stateRoot, "runtime", fmt.Sprintf("validator-%d", validatorId), "validator.yml")
	encoded, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, path, min(expected.Bounds.MaxControlBytes, uint64(maximumCampaignEvidenceRawFileBytes)))
	if err != nil {
		return nil, nil, nil, err
	}
	if err := validatorpkg.ValidateReleaseEvidenceV2ConfigYAML(encoded); err != nil {
		return nil, nil, nil, err
	}
	var release validatorpkg.ReleaseConfig
	decoder := yaml.NewDecoder(bytes.NewReader(encoded))
	decoder.KnownFields(true)
	if err := decoder.Decode(&release); err != nil {
		return nil, nil, nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, nil, nil, errors.New("compact runtime config has trailing YAML")
	}
	if err := release.Validate(); err != nil {
		return nil, nil, nil, err
	}
	deployment, err := loadContractDeployment(stateRoot)
	if err != nil {
		return nil, nil, nil, err
	}
	wantState := filepath.Join(stateRoot, "runtime", fmt.Sprintf("validator-%d", validatorId), "state")
	if release.ValidatorID != validatorId || release.StateDir != wantState || release.DeploymentID != cfg.Config.Deployment.DeploymentID || release.ChainID != cfg.ChainID || release.Netuid != cfg.Netuid || !strings.EqualFold(release.GenesisHash, cfg.Public.Chain.GenesisHash) || !strings.EqualFold(release.PolicyHash, cfg.PolicyHash) || common.HexToAddress(release.Coordinator) != deployment.CoordinatorProxy || common.HexToAddress(release.SettlementVault) != deployment.SettlementVault || !reflect.DeepEqual(release.EvidenceV2, *expected) || len(release.Operators) != len(cfg.OperatorAPIOrigins) {
		return nil, nil, nil, errors.New("compact capture runtime config differs from original setup/deployment")
	}
	for index, operator := range release.Operators {
		if operator.NoID != uint64(index+1) || operator.APIURL != cfg.OperatorAPIOrigins[index] {
			return nil, nil, nil, errors.New("compact capture runtime origins differ")
		}
	}
	adoption, adoptionBytes, err := finalCaptureHistoryAdoptionV2(ctx, cfg, stateRoot, &release, encoded)
	if err != nil {
		return nil, nil, nil, err
	}
	if adoption != nil {
		release.StateDir = adoption.CoordinatorStateDir
	}
	return &release, encoded, adoptionBytes, ctx.Err()
}

// One collector owns actual read clients and all exact source files. The
// complete archive remains pending analysis; no live Stats/ledger is reopened.
func collectFinalValidatorInputsV2(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, terminal *ScenarioObservation, phase string, window *ScenarioAcceptanceWindow, startedAt, completedAt time.Time, authority *finalOperatorPathAuthority) ([]FinalCollectedValidatorInputs, error) {
	if ctx == nil || cfg == nil || authority == nil || terminal == nil || window == nil || !finalUsesEvidenceV2(cfg) || len(cfg.OperatorAPIOrigins) != 2 {
		return nil, errors.New("compact final collector owner is incomplete")
	}
	requirements, err := finalLifecycleIntentRequirements(terminal, phase)
	if err != nil {
		return nil, err
	}
	var result []FinalCollectedValidatorInputs
	if window.EpochCount == 0 {
		return nil, errors.New("compact accepted epoch range is empty")
	}
	lastEpoch, ok := checkedAdd(window.FirstEpoch, window.EpochCount-1)
	if !ok {
		return nil, errors.New("compact accepted epoch range overflows")
	}
	archiveLimits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return nil, err
	}
	remainingSourceBytes := archiveLimits.maximumBytes
	authorityBytes, err := captureFinalValidatorAuthorityV2(ctx, cfg, stateRoot)
	if err != nil {
		return nil, err
	}
	contentLocators := map[string]FinalArtifactLocator{}
	for validatorId := uint64(1); validatorId <= uint64(cfg.Config.Topology.Validators); validatorId++ {
		release, configBytes, adoptionBytes, err := finalReleaseCaptureConfigWithAdoptionV2(ctx, cfg, stateRoot, validatorId)
		if err != nil {
			return nil, err
		}
		captureLimits, err := campaignValidatorLimitsV2(release.EvidenceV2.Bounds, uint64(len(release.EvidenceV2.Operators)), cfg.Config.ValidatorEvidenceRelay.MaxSlots)
		if err != nil {
			return nil, err
		}
		hotkey, err := decodeHex32("compact capture original validator hotkey", authority.identities.Substrate[validatorHotkeyLabel(int(validatorId))].PublicKey)
		if err != nil {
			return nil, err
		}
		paths := authority.pathsByValidator[validatorId]
		if len(paths) != cfg.Config.Topology.Operators {
			return nil, errors.New("compact capture original operator path census differs")
		}
		collected := FinalCollectedValidatorInputs{ValidatorID: validatorId, PathVPK: paths[0].PathVPK, OperatorPaths: append([]FinalOperatorPathIdentity(nil), paths...), EvidenceV2: &FinalCollectedValidatorEvidenceV2{Schema: finalCollectedValidatorEvidenceV2Schema, SemanticStatus: "pending_offline_verification", Hotkey: fmt.Sprintf("0x%x", hotkey), Origins: [2]string{cfg.OperatorAPIOrigins[0], cfg.OperatorAPIOrigins[1]}}}
		sourceLocators := map[validatorpkg.ReleaseEvidenceV2CaptureSource]FinalArtifactLocator{}
		retain := func(ctx context.Context, source validatorpkg.ReleaseEvidenceV2CaptureSource, raw []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			maximum := uint64(maximumCampaignEvidenceRawFileBytes)
			if source.Kind == "private" && source.Name == "steering-intents.json" {
				maximum = max(maximum, min(release.EvidenceV2.Bounds.MaxControlBytes, release.EvidenceV2.Bounds.MaxHistoryBytes))
			}
			if len(raw) == 0 || uint64(len(raw)) > maximum {
				return errors.New("compact source exceeds its raw or exact intent-control owner")
			}
			hash := bytesSHA256(raw)
			if previous, found := sourceLocators[source]; found {
				if previous.ContentHash != hash || previous.SizeBytes != uint64(len(raw)) {
					return errors.New("compact source provenance changed")
				}
				return nil
			}
			if uint64(len(sourceLocators)) >= captureLimits.maximumObjects {
				return errors.New("compact source census exceeds configured campaign archive limits")
			}
			locator, exists := contentLocators[hash]
			if !exists {
				if uint64(len(raw)) > remainingSourceBytes {
					return errors.New("compact unique source bytes exceed configured campaign archive limits")
				}
				name := fmt.Sprintf("final-inputs/validators/v2/%s.bin", strings.TrimPrefix(hash, "sha256:"))
				var err error
				locator, err = persistFinalCollectedArtifactForConfigV2(cfg, runRoot, "validator-evidence-v2-source", name, raw)
				if err != nil {
					return err
				}
				remainingSourceBytes -= uint64(len(raw))
				contentLocators[hash] = locator
			} else if locator.SizeBytes != uint64(len(raw)) {
				return errors.New("compact equal content hash has a different source length")
			}
			sourceLocators[source] = locator
			collected.EvidenceV2.Sources = append(collected.EvidenceV2.Sources, FinalCollectedValidatorSourceV2{Source: source, Artifact: locator})
			return nil
		}
		if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "runtime-config"}, configBytes); err != nil {
			return nil, err
		}
		if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "simulator-authority"}, authorityBytes); err != nil {
			return nil, err
		}
		if len(adoptionBytes) != 0 {
			if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "strict-history-adoption"}, adoptionBytes); err != nil {
				return nil, err
			}
		}
		chain, err := validatorpkg.DialReleaseChainContext(ctx, []string{cfg.OperationalEVM}, common.HexToAddress(release.Coordinator))
		if err != nil {
			return nil, err
		}
		native, err := crv4.DialChainContext(ctx, cfg.OperationalSubstrate)
		if err != nil {
			chain.Close()
			return nil, err
		}
		captured, captureErr := validatorpkg.CaptureReleaseEvidenceV2(ctx, release, chain, native, validatorpkg.ReleaseEvidenceV2CaptureOptions{Hotkey: hotkey, Origins: collected.EvidenceV2.Origins, MaximumBytes: captureLimits.dataBytes + captureLimits.controlBytes, MaximumObjects: captureLimits.maximumObjects, MaximumDataBytes: captureLimits.dataBytes, MaximumControlBytes: captureLimits.controlBytes, ThroughEpoch: lastEpoch}, retain)
		if captureErr == nil {
			collected.EvidenceV2.NativeCheckpoints, captureErr = collectFinalNativeCoverageV2(ctx, release, chain, native, hotkey, captured, *window, retain)
		}
		native.API.Client.Close()
		chain.Close()
		if captureErr != nil {
			return nil, captureErr
		}
		if err := captureFinalValidatorRelayV2(ctx, cfg, stateRoot, captured.Publications, retain); err != nil {
			return nil, err
		}
		collected.IntentStore, err = persistFinalCollectedArtifactForConfigV2(cfg, runRoot, "validator-steering-intent-store", fmt.Sprintf("final-inputs/validators/validator-%d-steering-intents.json", validatorId), captured.Store)
		if err != nil {
			return nil, err
		}
		selected, err := selectFinalCoverageIntentsV2(captured.Intents, collected.EvidenceV2.NativeCheckpoints)
		if err != nil {
			return nil, err
		}
		applied := map[uint64]bool{}
		matchedLifecycle := make([]bool, len(requirements[int(validatorId)]))
		dishonestMatches := 0
		for _, value := range captured.Intents {
			intent := &value.Intent
			inAcceptance := selected[value.Sequence]
			lifecycleIndex := -1
			for index, expected := range requirements[int(validatorId)] {
				if finalLifecycleIntentMatches(intent, expected) {
					if lifecycleIndex >= 0 || matchedLifecycle[index] {
						return nil, errors.New("compact lifecycle intent is duplicated")
					}
					lifecycleIndex = index
				}
			}
			dishonest := false
			if phase == "production-soak" && terminal.DishonestDeposit != nil && terminal.DishonestDepositValid {
				for _, expected := range terminal.DishonestDeposit.Validators {
					if uint64(expected.ValidatorID) != validatorId || intent.Status != "applied" || intent.SettlementEpoch != terminal.DishonestDeposit.Transaction.Epoch || intent.SubnetEpoch != expected.SubnetEpoch || intent.VectorHash != expected.VectorHash || intent.ApplicationBlock != expected.ApplicationBlock || !strings.EqualFold(intent.ApplicationBlockHash, expected.ApplicationBlockHash) {
						continue
					}
					for _, audit := range intent.DepositAudits {
						if finalJSONEqual(audit, expected.Audit) {
							dishonest = true
							dishonestMatches++
						}
					}
				}
			}
			if !inAcceptance && lifecycleIndex < 0 && !dishonest {
				continue
			}
			envelope, err := validatorpkg.DecodeReleaseMeasurementEnvelopeV2(ctx, value.Envelope, release.EvidenceV2.Bounds.MaxControlBytes)
			if err != nil {
				return nil, err
			}
			signedAt, err := time.Parse(time.RFC3339Nano, envelope.SignedAt)
			baseline := inAcceptance && intent.RevealBlock <= collected.EvidenceV2.NativeCheckpoints[0].Mapping.Query.NativeNumber
			if err != nil || signedAt.Before(startedAt) && !baseline || signedAt.After(completedAt) {
				return nil, errors.New("compact selected envelope is outside the original campaign time window")
			}
			item := FinalCollectedValidatorIntent{Sequence: value.Sequence, SettlementEpoch: intent.SettlementEpoch, SubnetEpoch: intent.SubnetEpoch, Status: intent.Status, VectorHash: intent.VectorHash}
			item.Artifact, err = persistFinalCollectedArtifact(runRoot, "steering-intent", fmt.Sprintf("final-inputs/validators/validator-%d-intent-%06d-%s.json", validatorId, value.Sequence, strings.TrimPrefix(intent.VectorHash, "0x")), value.Encoded)
			if err != nil {
				return nil, err
			}
			item.Measurement, err = persistFinalCollectedArtifact(runRoot, "validator-release-measurement", fmt.Sprintf("final-inputs/validators/validator-%d-measurement-%s.json", validatorId, strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:")), value.Measurement)
			if err != nil {
				return nil, err
			}
			item.Envelope, err = persistFinalCollectedArtifact(runRoot, "validator-release-measurement-envelope", fmt.Sprintf("final-inputs/validators/validator-%d-measurement-envelope-%s.json", validatorId, strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:")), value.Envelope)
			if err != nil {
				return nil, err
			}
			if inAcceptance {
				if intent.Status == "applied" {
					if applied[intent.SubnetEpoch] {
						return nil, errors.New("compact accepted native epoch has duplicate applied intents")
					}
					applied[intent.SubnetEpoch] = true
				}
				collected.Intents = append(collected.Intents, item)
			}
			if lifecycleIndex >= 0 {
				matchedLifecycle[lifecycleIndex] = true
				collected.LifecycleIntents = append(collected.LifecycleIntents, item)
			}
			if dishonest {
				collected.DishonestDepositIntent = &item
			}
		}
		if len(applied) != len(selected) {
			return nil, errors.New("compact native application interval census is incomplete")
		}
		for _, matched := range matchedLifecycle {
			if !matched {
				return nil, errors.New("compact required lifecycle intent is missing")
			}
		}
		if phase == "production-soak" && dishonestMatches != 1 {
			return nil, errors.New("compact exact dishonest-deposit intent is missing or duplicated")
		}
		for _, closure := range captured.Closures {
			name := filepath.ToSlash(filepath.Join("settlement-closures-v2", fmt.Sprintf("%d.json", closure.Epoch)))
			locator, found := sourceLocators[validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "private", Name: name}]
			if !found || len(closure.Transitions) == 0 {
				return nil, errors.New("compact terminal source is not retained")
			}
			boundary := closure.Transitions[0].FromBoundary
			collected.EvidenceV2.Closures = append(collected.EvidenceV2.Closures, FinalCollectedSettlementClosure{Epoch: closure.Epoch, Boundary: ChainHead{Number: boundary.EVMBlock, Hash: boundary.EVMBlockHash}, Artifact: locator})
		}
		sort.Slice(collected.EvidenceV2.Sources, func(i, j int) bool {
			return finalCaptureSourceV2Key(collected.EvidenceV2.Sources[i].Source) < finalCaptureSourceV2Key(collected.EvidenceV2.Sources[j].Source)
		})
		result = append(result, collected)
	}
	return result, ctx.Err()
}

// A total provenance order keeps repeated hashes from collapsing two origins.
func finalCaptureSourceV2Key(source validatorpkg.ReleaseEvidenceV2CaptureSource) string {
	return source.Kind + "\x00" + source.Origin + "\x00" + source.Name
}

// Structural closure authenticates only archive completeness and original
// byte linkage. Compact trail replay must still run before semantic success.
func verifyFinalCollectedValidatorEvidenceV2(cfg *ResolvedConfig, value *FinalSemanticCollectedInputs, collected FinalCollectedValidatorInputs) error {
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return err
	}
	v2 := collected.EvidenceV2
	if !finalUsesEvidenceV2(cfg) || value == nil || value.Window.EpochCount == 0 || value.Window.EpochBlocks == 0 || v2 == nil || v2.Schema != finalCollectedValidatorEvidenceV2Schema || v2.SemanticStatus != "pending_offline_verification" || len(collected.Attempts) != 0 || len(collected.PathProofs) != 0 || len(collected.SettlementClosures) != 0 || len(v2.Sources) == 0 || uint64(len(v2.Sources)) > limits.maximumObjects || len(cfg.OperatorAPIOrigins) != 2 || v2.Origins != [2]string{cfg.OperatorAPIOrigins[0], cfg.OperatorAPIOrigins[1]} {
		return errors.New("compact collected source shape or deferred status differs")
	}
	if _, ok := checkedAdd(value.Window.FirstEpoch, value.Window.EpochCount-1); !ok {
		return errors.New("compact captured accepted epochs overflow")
	}
	if _, err := decodeHex32("compact collected hotkey", v2.Hotkey); err != nil {
		return err
	}
	if _, err := finalOperatorPathKeys(collected.PathVPK, collected.OperatorPaths, cfg.Config.Topology.Operators); err != nil {
		return err
	}
	if err := verifyFinalArtifact("compact intent store", collected.IntentStore, "validator-steering-intent-store"); err != nil {
		return err
	}
	classes := map[string]bool{}
	for index, source := range v2.Sources {
		if source.Source.Kind == "" || source.Source.Name == "" || index > 0 && finalCaptureSourceV2Key(source.Source) <= finalCaptureSourceV2Key(v2.Sources[index-1].Source) {
			return errors.New("compact source provenance is not a complete canonical census")
		}
		if source.Source.Origin != "" && source.Source.Origin != v2.Origins[0] && source.Source.Origin != v2.Origins[1] {
			return errors.New("compact captured origin is not configured")
		}
		if err := verifyFinalArtifact("compact raw source", source.Artifact, "validator-evidence-v2-source"); err != nil {
			return err
		}
		classes[source.Source.Kind] = true
	}
	for _, kind := range []string{"setup", "private", "native-rpc", "closed-census", "signed-evidence", "terminal-payload", "relay-journal", "relay-request", "relay-result", "relay-winner"} {
		if !classes[kind] {
			return fmt.Errorf("compact capture lacks %s source bytes", kind)
		}
	}
	applied := map[uint64]bool{}
	for index, intent := range collected.Intents {
		outside := intent.SettlementEpoch < value.Window.FirstEpoch || intent.SettlementEpoch-value.Window.FirstEpoch >= value.Window.EpochCount
		if intent.Sequence == 0 || index > 0 && intent.Sequence <= collected.Intents[index-1].Sequence || outside && len(v2.NativeCheckpoints) == 0 {
			return errors.New("compact captured intent routing differs")
		}
		if err := verifyFinalArtifact("compact intent", intent.Artifact, "steering-intent"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("compact measurement", intent.Measurement, "validator-release-measurement"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("compact envelope", intent.Envelope, "validator-release-measurement-envelope"); err != nil {
			return err
		}
		if intent.Status == "applied" {
			key := intent.SettlementEpoch
			if len(v2.NativeCheckpoints) != 0 {
				key = intent.SubnetEpoch
			}
			if applied[key] {
				return errors.New("compact captured applied epoch is duplicated")
			}
			applied[key] = true
		}
	}
	closed := map[uint64]bool{}
	for index, closure := range v2.Closures {
		if index > 0 && closure.Epoch <= v2.Closures[index-1].Epoch || closure.Boundary.Number == 0 || verifyFinalArtifact("compact terminal source", closure.Artifact, "validator-evidence-v2-source") != nil {
			return errors.New("compact captured terminal routing differs")
		}
		closed[closure.Epoch] = true
		if closure.Epoch >= value.Window.FirstEpoch && closure.Epoch-value.Window.FirstEpoch < value.Window.EpochCount {
			span, ok := checkedMul(closure.Epoch-value.Window.FirstEpoch+1, value.Window.EpochBlocks)
			if !ok {
				return errors.New("compact terminal block span overflows")
			}
			end, ok := checkedAdd(value.Window.StartBlock, span)
			if !ok || end == 0 {
				return errors.New("compact terminal block range overflows")
			}
			if closure.Boundary.Number != end-1 {
				return errors.New("compact terminal source is not the actual accepted boundary")
			}
		}
	}
	for offset := uint64(0); offset < value.Window.EpochCount; offset++ {
		epoch := value.Window.FirstEpoch + offset
		if len(v2.NativeCheckpoints) == 0 && !applied[epoch] || !closed[epoch] {
			return errors.New("compact captured acceptance source coverage is incomplete")
		}
	}
	if len(v2.NativeCheckpoints) != 0 {
		blocks, err := finalNativeCoverageEVMBlocksV2(value.Window)
		if err != nil || len(blocks) != len(v2.NativeCheckpoints) || len(applied) == 0 {
			return errors.Join(errors.New("compact native coverage checkpoint census differs"), err)
		}
		for index, checkpoint := range v2.NativeCheckpoints {
			if checkpoint.Mapping.Query.EVMNumber != blocks[index] || checkpoint.Mapping.Query.NativeNumber == 0 || checkpoint.Weights.Block.Number != checkpoint.Mapping.Query.NativeNumber {
				return errors.New("compact native coverage checkpoint routing differs")
			}
		}
	}
	if value.Phase == "production-soak" && collected.DishonestDepositIntent == nil || value.Phase == "release-1.0" && collected.DishonestDepositIntent != nil {
		return errors.New("compact dishonest-deposit capture phase differs")
	}
	return nil
}

// Header/public-byte availability is only the live drain boundary. The
// existing relay then requires canonical on-chain slots; neither is scoring.
func waitFinalValidatorPublicationsV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, terminal *ScenarioObservation, window *ScenarioAcceptanceWindow, deadline time.Time, poll time.Duration, wait func(context.Context, time.Duration) error) error {
	if terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil || window == nil || window.EpochCount == 0 || window.EpochBlocks == 0 || len(cfg.OperatorAPIOrigins) != 2 {
		return errors.New("compact terminal wait authority is incomplete")
	}
	authority, err := loadFinalOperatorPathAuthority(cfg, stateRoot, finalConfiguredValidatorIDs(cfg))
	if err != nil {
		return err
	}
	var releases []*validatorpkg.ReleaseConfig
	activationCenses := map[uint64][]protocol.ValidatorEvidenceActivation{}
	for validatorId := uint64(1); validatorId <= uint64(cfg.Config.Topology.Validators); validatorId++ {
		release, _, err := finalReleaseCaptureConfigV2(ctx, cfg, stateRoot, validatorId)
		if err != nil {
			return err
		}
		hotkey, err := decodeHex32("compact terminal original hotkey", authority.identities.Substrate[validatorHotkeyLabel(int(validatorId))].PublicKey)
		if err != nil {
			return err
		}
		for _, operator := range release.EvidenceV2.Operators {
			raw, err := validatorpkg.ReadReleaseEvidenceV2File(ctx, operator.Activation, uint64(protocol.ValidatorEvidenceActivationPayloadSize))
			if err != nil {
				return err
			}
			activation, err := protocol.DecodeValidatorEvidenceActivationPayload(raw)
			if err != nil {
				return err
			}
			if activation.NoID != operator.NoID || activation.Hotkey != hotkey || !bytes.Equal(activation.VPK[:], authority.keysByValidator[validatorId][operator.NoID]) {
				return errors.New("compact terminal activation changes the original source identity")
			}
			activationCenses[validatorId] = append(activationCenses[validatorId], activation)
		}
		releases = append(releases, release)
	}
	return runFinalSettlementClosureWait(ctx, deadline, poll, func(ctx context.Context) (bool, error) {
		missing := false
		for _, release := range releases {
			for offset := uint64(0); offset < window.EpochCount; offset++ {
				epoch, ok := checkedAdd(window.FirstEpoch, offset)
				if !ok {
					return false, errors.New("compact terminal epoch range overflows")
				}
				span, ok := checkedMul(offset, window.EpochBlocks)
				if !ok {
					return false, errors.New("compact terminal block range overflows")
				}
				start, ok := checkedAdd(window.StartBlock, span)
				if !ok {
					return false, errors.New("compact terminal start overflows")
				}
				end, ok := checkedAdd(start, window.EpochBlocks)
				if !ok {
					return false, errors.New("compact terminal end overflows")
				}
				path, err := validatorpkg.ValidatorEvidencePublicationV2ManifestPath(release.StateDir, epoch)
				if err != nil {
					return false, err
				}
				manifest, err := validatorpkg.ReadValidatorEvidencePublicationV2Manifest(ctx, path, release.EvidenceV2.Bounds.MaxClosureBytes, release.EvidenceV2.Bounds.MaxParticipants)
				if errors.Is(err, os.ErrNotExist) {
					missing = true
					continue
				}
				if err != nil {
					return false, err
				}
				_, err = validatorpkg.ReadValidatorEvidencePublicationV2(ctx, manifest, validatorpkg.ValidatorEvidencePublicationV2ReadOptions{Activations: activationCenses[release.ValidatorID], Window: protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: start, EndBlock: end, FinalizedBlock: terminal.Status.Contracts.FinalizedHead.Number}, Origins: [2]string{cfg.OperatorAPIOrigins[0], cfg.OperatorAPIOrigins[1]}, Bounds: release.EvidenceV2.Bounds})
				if err != nil {
					return false, err
				}
			}
		}
		return !missing, nil
	}, wait)
}

// Older raw captures lack the original public renderer authority required for
// independent V2 replay. New captures enter only the complete archive consumer;
// neither a compact-format flag nor a pending status is a semantic verdict.
func requireFinalSemanticReplayV2(value *FinalSemanticCollectedInputs) error {
	if value == nil {
		return errors.New("final semantic source is absent")
	}
	if value.PriorPhase != nil && value.PriorPhase.SemanticStatus == finalSemanticCapturePendingStatus {
		return &finalSemanticAnalysisPendingError{}
	}
	for _, validator := range value.Validators {
		if validator.EvidenceV2 != nil {
			originalAuthority, decisions := false, false
			for _, source := range validator.EvidenceV2.Sources {
				originalAuthority = originalAuthority || source.Source == (validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "simulator-authority"})
				decisions = decisions || source.Source.Kind == "decision-observation"
			}
			if !originalAuthority || !decisions {
				return &finalSemanticAnalysisPendingError{}
			}
		}
	}
	return nil
}

// Read-back binds each selected original v6 intent to its original store,
// raw measurement, signed envelope and complete retained provenance inventory.
// It is intentionally not the independent compact head/terminal math replay.
func verifyFinalCapturedValidatorBytesV2(cfg *ResolvedConfig, value *FinalSemanticCollectedInputs, collected FinalCollectedValidatorInputs, authority *finalOperatorPathAuthority, loaded map[string][]byte) error {
	return verifyFinalCapturedValidatorBytesV2WithReader(context.Background(), cfg, value, collected, authority, loaded, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		raw, found := loaded[locator.URI]
		if !found {
			return nil, errors.New("compact captured source is absent")
		}
		return raw, nil
	})
}

// Source chunks are authenticated one at a time. Cross-object linkage retains
// only locator hashes/sizes, while selected small decision controls remain in
// the existing owner map. No record/proof bytes are cached for later scoring.
func verifyFinalCapturedValidatorBytesV2WithReader(ctx context.Context, cfg *ResolvedConfig, value *FinalSemanticCollectedInputs, collected FinalCollectedValidatorInputs, authority *finalOperatorPathAuthority, loaded map[string][]byte, read FinalArtifactLoader) error {
	if ctx == nil || read == nil {
		return errors.New("compact source reader owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyFinalCollectedValidatorEvidenceV2(cfg, value, collected); err != nil {
		return err
	}
	if authority == nil || authority.identities == nil {
		return errors.New("compact captured original identity authority is absent")
	}
	hotkey, err := decodeHex32("compact captured original hotkey", authority.identities.Substrate[validatorHotkeyLabel(int(collected.ValidatorID))].PublicKey)
	if err != nil || collected.EvidenceV2.Hotkey != fmt.Sprintf("0x%x", hotkey) {
		return errors.Join(errors.New("compact capture hotkey differs from original identity"), err)
	}
	storeBytes := loaded[collected.IntentStore.URI]
	intents, err := decodeValidatorIntentBytes(storeBytes, int(collected.ValidatorID))
	if err != nil {
		return err
	}
	var store struct {
		Schema  string            `json:"schema"`
		Current json.RawMessage   `json:"current"`
		History []json.RawMessage `json:"history"`
	}
	if err := decodeStrictJSONBytes(storeBytes, &store); err != nil {
		return err
	}
	rawIntents := append([]json.RawMessage(nil), store.History...)
	if len(store.Current) != 0 && !bytes.Equal(bytes.TrimSpace(store.Current), []byte("null")) {
		rawIntents = append(rawIntents, store.Current)
	}
	if len(intents) != len(rawIntents) {
		return errors.New("compact captured original intent count differs")
	}
	private := map[string]FinalArtifactLocator{}
	var nativeReads uint64
	for _, source := range collected.EvidenceV2.Sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := read(ctx, source.Artifact)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if uint64(len(raw)) != source.Artifact.SizeBytes || bytesSHA256(raw) != source.Artifact.ContentHash {
			return errors.New("compact captured source bytes are missing or changed")
		}
		if source.Source.Kind == "private" {
			private[source.Source.Name] = source.Artifact
		}
		if source.Source.Kind == "native-rpc" {
			var read validatorpkg.ReleaseEvidenceV2NativeRead
			if err := decodeStrictJSONBytes(raw, &read); err != nil {
				return err
			}
			if read.Schema != "urnetwork-validator-native-read-v2" || !json.Valid(read.Parameters) || !json.Valid(read.Result) {
				return errors.New("compact native read has no original parameters/result bytes")
			}
			switch read.Method {
			case "chain_getBlockHash", "chain_getHeader", "chain_getFinalizedHead", "chain_getBlock", "state_getRuntimeVersion", "state_getMetadata", "state_getStorage", "state_getStorageHash", "state_call":
			default:
				return errors.New("compact native capture contains a non-read method")
			}
			nativeReads++
			if source.Source.Name != fmt.Sprintf("read-%020d.json", nativeReads) {
				return errors.New("compact native capture read sequence differs")
			}
		}
	}
	storeSource, found := private["steering-intents.json"]
	if nativeReads == 0 || !found || storeSource.SizeBytes != uint64(len(storeBytes)) || storeSource.ContentHash != bytesSHA256(storeBytes) {
		return errors.New("compact native source/store original custody is incomplete")
	}
	selected := append([]FinalCollectedValidatorIntent(nil), collected.Intents...)
	selected = append(selected, collected.LifecycleIntents...)
	if collected.DishonestDepositIntent != nil {
		selected = append(selected, *collected.DishonestDepositIntent)
	}
	for _, item := range selected {
		if item.Sequence == 0 || item.Sequence > uint64(len(intents)) {
			return errors.New("compact captured intent is outside its original store")
		}
		intent := intents[item.Sequence-1]
		if intent.Prepared == nil || intent.Prepared.SourceCommitment == nil || !bytes.Equal(loaded[item.Artifact.URI], rawIntents[item.Sequence-1]) || item.SettlementEpoch != intent.SettlementEpoch || item.SubnetEpoch != intent.SubnetEpoch || item.Status != intent.Status || item.VectorHash != intent.VectorHash {
			return errors.New("compact captured intent differs from its exact original store/source")
		}
		measurement := loaded[item.Measurement.URI]
		envelope := loaded[item.Envelope.URI]
		measurementSource, haveMeasurement := private[intent.MeasurementArtifactPath]
		envelopeSource, haveEnvelope := private[intent.MeasurementEnvelopePath]
		if !haveMeasurement || !haveEnvelope || measurementSource.SizeBytes != uint64(len(measurement)) || envelopeSource.SizeBytes != uint64(len(envelope)) || measurementSource.ContentHash != bytesSHA256(measurement) || envelopeSource.ContentHash != bytesSHA256(envelope) || uint64(len(measurement)) != intent.MeasurementArtifactSize || uint64(len(envelope)) != intent.MeasurementEnvelopeSize || validatorpkg.ReleaseMeasurementContentHash(measurement) != intent.MeasurementArtifactHash || validatorpkg.ReleaseMeasurementEnvelopeContentHash(envelope) != intent.MeasurementEnvelopeHash {
			return errors.New("compact captured signed source hash/size differs")
		}
		var schema struct {
			Schema string `json:"schema"`
		}
		if err := json.Unmarshal(measurement, &schema); err != nil || schema.Schema != validatorpkg.ReleaseMeasurementSchemaV2 {
			return errors.New("compact capture cannot reinterpret a legacy measurement")
		}
		signed, err := validatorpkg.DecodeReleaseMeasurementEnvelopeV2(ctx, envelope, maximumCampaignEvidenceRawFileBytes)
		if err != nil || signed.ValidatorHotkey != collected.EvidenceV2.Hotkey || signed.PreparedExtrinsicHash != intent.Prepared.ExtrinsicHash || signed.MeasurementArtifactHash != intent.MeasurementArtifactHash || signed.ValidatorUID != intent.SelfUID {
			return errors.Join(errors.New("compact captured envelope source identity differs"), err)
		}
	}
	return ctx.Err()
}
