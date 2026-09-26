//go:build linux || darwin

// A separately approved provisional recovery adopts one real native edge in
// an existing evidence generation. It cannot admit a final historical archive.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/urfoundation/sn/crv4"
)

const ReleaseNativeHistoryRecoveryV2Schema = "urnetwork-validator-native-history-recovery-v2"
const ReleaseNativeHistoryRecoveryV2MaximumBytes = 128 * 1024

// The deployment launcher authenticates the owner-signed parent plan before
// pinning these canonical child bytes. This wire is not itself a receipt.
type ReleaseNativeHistoryRecoveryV2 struct {
	Schema               string                       `json:"schema"`
	Provisional          bool                         `json:"provisional"`
	FinalAcceptance      bool                         `json:"final_acceptance"`
	StateDir             string                       `json:"state_dir"`
	ConfigPath           string                       `json:"config_path"`
	Generation           uint64                       `json:"generation"`
	Runtime              crv4.RuntimeArtifactIdentity `json:"runtime"`
	CompatibilityProfile string                       `json:"compatibility_profile"`
	HeadEmaSha256        string                       `json:"head_ema_sha256"`
	Adoption             ReleaseHistoryAdoptionV2     `json:"adoption"`
}

// DecodeReleaseNativeHistoryRecoveryV2 admits only the exact finite wire; the
// source namespace and full normal startup still require independent checks.
func DecodeReleaseNativeHistoryRecoveryV2(encoded []byte, expectedSha256 string) (*ReleaseNativeHistoryRecoveryV2, error) {
	if len(encoded) == 0 || len(encoded) > ReleaseNativeHistoryRecoveryV2MaximumBytes || ReleaseMeasurementContentHash(encoded) != expectedSha256 {
		return nil, errors.New("native history recovery differs from its approved byte pin")
	}
	var request ReleaseNativeHistoryRecoveryV2
	if err := json.Unmarshal(encoded, &request); err != nil {
		return nil, err
	}
	wire, err := json.MarshalIndent(request, "", "  ")
	if err != nil || !bytes.Equal(encoded, append(wire, '\n')) {
		return nil, errors.New("native history recovery is not canonical")
	}
	adoption, err := json.MarshalIndent(request.Adoption, "", "  ")
	if err != nil {
		return nil, err
	}
	adoption = append(adoption, '\n')
	if _, err := DecodeReleaseHistoryAdoptionV2(adoption, ReleaseMeasurementContentHash(adoption)); err != nil {
		return nil, err
	}
	if request.Schema != ReleaseNativeHistoryRecoveryV2Schema || !request.Provisional || request.FinalAcceptance || request.Generation != 2 || request.Adoption.IntentPrefixCount == 0 || request.Adoption.FirstNativeEpoch <= request.Adoption.LastNativeEpoch+1 || request.CompatibilityProfile != crv4.ProvisionalRuntimeCompatibilityProfile || request.Runtime.Version.SpecName != "node-subtensor" || request.Runtime.Version.SpecVersion <= crv4.ReviewedRuntimeSpecVersion || request.Runtime.Version.TransactionVersion != 1 || request.Runtime.Version.StateVersion != 1 {
		return nil, errors.New("native history recovery is outside its generation-2 provisional one-edge scope")
	}
	for _, path := range []string{request.StateDir, request.ConfigPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, errors.New("native recovery owner path is not absolute and clean")
		}
	}
	want := filepath.Join(request.StateDir, "runtime", fmt.Sprintf("validator-%d", request.Adoption.ValidatorID), "evidence-generations", fmt.Sprintf("generation-%020d", request.Generation), "coordinator-state-v2")
	if request.Adoption.CoordinatorStateDir != want {
		return nil, errors.New("native history recovery changes its generation namespace")
	}
	if _, err := parseReleaseContentHash(request.HeadEmaSha256); err != nil {
		return nil, err
	}
	for _, hash := range []string{request.Runtime.CodeHash, request.Runtime.MetadataHash} {
		if _, err := canonicalAttemptHex32("native recovery runtime", hash, false); err != nil {
			return nil, err
		}
	}
	return &request, nil
}

// matchConfig permits the selected source-role config to live outside the
// generation directory while retaining that directory's exact coordinator.
func (self *ReleaseNativeHistoryRecoveryV2) matchConfig(cfg *ReleaseConfig, path string, encoded []byte) error {
	if cfg == nil || path != self.ConfigPath || ReleaseMeasurementContentHash(encoded) != self.Adoption.ConfigSHA256 || cfg.StateDir != self.Adoption.CoordinatorStateDir || cfg.DeploymentID != self.Adoption.DeploymentID || cfg.ValidatorID != self.Adoption.ValidatorID || cfg.ChainID != 945 || cfg.GenesisHash != provisionalRuntimeTestnetGenesis || cfg.Policy.NetworkProfile != "testnet" || cfg.ProvisionalDeferClosedNativeInput || cfg.ProvisionalRuntimeCompatibility != self.CompatibilityProfile {
		return errors.New("native history recovery changes its pinned testnet config or coordinator owner")
	}
	return cfg.Validate()
}

// configure retains ordinary semantic startup and the exact historical prefix.
// It never selects retained setup or enables closed-native-input deferral.
func (self *ReleaseNativeHistoryRecoveryV2) configure(ctx context.Context, cfg *ReleaseConfig, path string) error {
	encoded, err := ReadReleaseEvidenceV2SetupFile(ctx, path, ReleaseNativeHistoryRecoveryV2MaximumBytes)
	if err != nil {
		return err
	}
	owned, err := decodeReleaseConfigBytes(path, encoded)
	if err != nil {
		return err
	}
	if err := self.matchConfig(owned, path, encoded); err != nil {
		return err
	}
	if err := self.checkSource(ctx, owned, false); err != nil {
		return err
	}
	copy := *self
	owned.historyAdoptionV2, owned.nativeHistoryRecoveryV2 = &copy.Adoption, &copy
	*cfg = *owned
	return nil
}

// checkSource pins the compact EMA until the first real intent is appended.
// Later restarts reauthenticate the immutable prefix and normal complete replay
// proves every appended intent and EMA fold. Unresolved publications fail closed.
func (self *ReleaseNativeHistoryRecoveryV2) checkSource(ctx context.Context, cfg *ReleaseConfig, exact bool) (resultErr error) {
	owned := &releaseEvidenceV2StartupReferences{remaining: cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	defer func() { resultErr = errors.Join(resultErr, owned.check(), owned.close(), ctx.Err()) }()
	raw, err := owned.read(ctx, filepath.Join(cfg.StateDir, "steering-intents.json"), cfg.EvidenceV2.Bounds.IntentFileLimit(), false)
	if err != nil {
		return err
	}
	var file steeringIntentFile
	if err := decodeAttemptStreamV2JSON(raw, cfg.EvidenceV2.Bounds.IntentFileLimit(), &file); err != nil {
		return err
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, &file, cfg.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil || !bytes.Equal(canonical, raw) {
		return errors.Join(errors.New("native recovery source intents are not canonical"), err)
	}
	if _, err := self.Adoption.matchPrefix(ctx, &file, raw, cfg.EvidenceV2.Bounds.IntentFileLimit()); err != nil {
		return err
	}
	for _, name := range []string{releaseIntentV2Marker, releaseIntentV2Candidate, headEMAStoreV2Marker, headEMAStoreV2Candidate} {
		if _, err := owned.read(ctx, filepath.Join(cfg.StateDir, name), 1, true); err != nil {
			return err
		}
		if !owned.owners[len(owned.owners)-1].initialMissing {
			return errors.New("native recovery source has an unresolved intent or EMA publication")
		}
	}
	count := uint64(len(file.History))
	if file.Current != nil {
		count++
	}
	if exact && count != self.Adoption.IntentPrefixCount {
		return errors.New("native recovery source progressed after review")
	}
	if count == self.Adoption.IntentPrefixCount {
		ema, err := owned.read(ctx, filepath.Join(cfg.StateDir, "head-ema.json"), cfg.EvidenceV2.Bounds.MaxControlBytes, false)
		if err != nil {
			return err
		}
		var compact headEMAFile
		if err := decodeAttemptStreamV2JSON(ema, cfg.EvidenceV2.Bounds.MaxControlBytes, &compact); err != nil {
			return err
		}
		if ReleaseMeasurementContentHash(ema) != self.HeadEmaSha256 || compact.Schema != headEMASchemaV2 || compact.LastSubnetEpoch == nil || *compact.LastSubnetEpoch != self.Adoption.LastNativeEpoch {
			return errors.New("native recovery compact EMA changed or does not end at its retained intent")
		}
	}
	return nil
}

// CaptureReleaseNativeHistoryRecoveryV2 only reads the selected existing files.
// Its caller excludes writers and independently authenticates native sources.
func CaptureReleaseNativeHistoryRecoveryV2(ctx context.Context, stateDir, path string, encoded []byte, approvedPlanHash, sourcePlanHash string, first uint64, runtime crv4.RuntimeArtifactIdentity) (result []byte, resultErr error) {
	if ctx == nil {
		return nil, errors.New("native recovery capture has no context")
	}
	cfg, err := decodeReleaseConfigBytes(path, bytes.Clone(encoded))
	if err != nil {
		return nil, err
	}
	file, raw, closeOwner, err := readHistoryAdoptionPrefixV2(ctx, cfg, cfg.StateDir)
	defer func() {
		resultErr = errors.Join(resultErr, closeOwner(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err != nil {
		return nil, err
	}
	if file.Current == nil {
		return nil, errors.New("native recovery requires an actual retained terminal intent")
	}
	ema, err := ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(cfg.StateDir, "head-ema.json"), cfg.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil {
		return nil, err
	}
	request := ReleaseNativeHistoryRecoveryV2{Schema: ReleaseNativeHistoryRecoveryV2Schema, Provisional: true, StateDir: stateDir, ConfigPath: path, Generation: 2, Runtime: runtime, CompatibilityProfile: cfg.ProvisionalRuntimeCompatibility, HeadEmaSha256: ReleaseMeasurementContentHash(ema), Adoption: ReleaseHistoryAdoptionV2{Schema: ReleaseHistoryAdoptionV2Schema, DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID, ApprovedPlanHash: approvedPlanHash, SourcePlanHash: sourcePlanHash, ConfigSHA256: ReleaseMeasurementContentHash(encoded), CoordinatorStateDir: cfg.StateDir, IntentPrefixSHA256: ReleaseMeasurementContentHash(raw), IntentPrefixCount: uint64(len(file.History)) + 1, LastNativeEpoch: file.Current.SubnetEpoch, LastArtifactHash: file.Current.MeasurementArtifactHash, FirstNativeEpoch: first}}
	result, err = json.MarshalIndent(request, "", "  ")
	if err != nil {
		return nil, err
	}
	result = append(result, '\n')
	if _, err := DecodeReleaseNativeHistoryRecoveryV2(result, ReleaseMeasurementContentHash(result)); err != nil {
		return nil, err
	}
	if err := request.matchConfig(cfg, path, encoded); err != nil {
		return nil, err
	}
	if err := request.checkSource(ctx, cfg, true); err != nil {
		return nil, err
	}
	return result, nil
}

// CheckReleaseNativeHistoryRecoveryV2Source is a local immutable-source check;
// exact=false permits only append-only progress for a later normal restart.
func CheckReleaseNativeHistoryRecoveryV2Source(ctx context.Context, encoded []byte, expectedSha256 string, exact bool) error {
	request, err := DecodeReleaseNativeHistoryRecoveryV2(encoded, expectedSha256)
	if err != nil {
		return err
	}
	config, err := ReadReleaseEvidenceV2SetupFile(ctx, request.ConfigPath, ReleaseNativeHistoryRecoveryV2MaximumBytes)
	if err != nil {
		return err
	}
	cfg, err := decodeReleaseConfigBytes(request.ConfigPath, config)
	if err != nil {
		return err
	}
	if err := request.matchConfig(cfg, request.ConfigPath, config); err != nil {
		return err
	}
	return request.checkSource(ctx, cfg, exact)
}

// RunReleaseWithNativeHistoryRecoveryV2 is entered only with the launcher's
// immutable child pin. Final archive admission remains a separate authority.
func RunReleaseWithNativeHistoryRecoveryV2(ctx context.Context, configPath string, encoded []byte, expectedSha256 string) error {
	request, err := DecodeReleaseNativeHistoryRecoveryV2(encoded, expectedSha256)
	if err != nil {
		return err
	}
	return runReleaseWithRecoveryStartupV2(ctx, configPath, nil, nil, request)
}
