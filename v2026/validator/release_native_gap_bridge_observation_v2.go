//go:build linux || darwin

package validator

// Read-only custody projects the applied predecessor needed for an explicit
// gap bridge without creating an owner, signer, transaction or state file.

import (
	"context"
	"errors"
	"path/filepath"
)

// This projection reports retained local receipts, never chain acceptance or
// permission to bypass the runtime's complete startup and replay admission.
type ReleaseNativeGapBridgePredecessorV2Observation struct {
	NativeEpoch           uint64 `json:"native_epoch"`
	Status                string `json:"status"`
	ArtifactHash          string `json:"artifact_hash"`
	ArtifactBytes         uint64 `json:"artifact_bytes"`
	VectorHash            string `json:"vector_hash"`
	ApplicationBlock      uint64 `json:"application_block"`
	RetainedIntents       int    `json:"retained_intents"`
	ExistingBridgeMatches bool   `json:"existing_bridge_matches"`
}

// Existing descriptor custody and typed transcript readers project only the
// exact independently named applied predecessor and its public artifact.
func ObserveReleaseNativeGapBridgePredecessorV2(ctx context.Context, cfg *ReleaseConfig, bridge *ReleaseNativeGapBridge, epoch uint64, artifactHash string, artifactBytes uint64) (result *ReleaseNativeGapBridgePredecessorV2Observation, resultErr error) {
	return observeReleaseNativeGapBridgePredecessorV2(ctx, cfg, bridge, epoch, artifactHash, artifactBytes, releaseMeasurementInputV2ReadHooks{})
}

func observeReleaseNativeGapBridgePredecessorV2(ctx context.Context, cfg *ReleaseConfig, bridge *ReleaseNativeGapBridge, epoch uint64, artifactHash string, artifactBytes uint64, hooks releaseMeasurementInputV2ReadHooks) (result *ReleaseNativeGapBridgePredecessorV2Observation, resultErr error) {
	if ctx == nil || cfg == nil || bridge == nil || epoch == 0 || artifactBytes == 0 || artifactBytes > cfg.EvidenceV2.Bounds.MaxArtifactBytes || bridge.ChainID != cfg.ChainID || bridge.DeploymentID != cfg.DeploymentID || bridge.ValidatorID != cfg.ValidatorID {
		return nil, errors.New("native bridge predecessor observation has no independently bound source")
	}
	if bridge.Schema != ReleaseNativeGapBridgeSchema && bridge.Schema != ReleaseNativeGapBridgeEdgesSchema || bridge.Schema == ReleaseNativeGapBridgeEdgesSchema && len(bridge.edges) == 0 {
		return nil, errors.New("native bridge predecessor observation requires a structurally admitted bridge")
	}
	if _, err := parseReleaseContentHash(artifactHash); err != nil {
		return nil, err
	}
	limit := cfg.EvidenceV2.Bounds.IntentFileLimit()
	custody := &releaseEvidenceV2StartupReferences{remaining: cfg.EvidenceV2.Bounds.MaxHistoryBytes, readHooks: hooks}
	defer func() {
		resultErr = errors.Join(resultErr, custody.check(), custody.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	encoded, err := custody.read(ctx, filepath.Join(cfg.StateDir, "steering-intents.json"), limit, false)
	if err != nil {
		return nil, err
	}
	checkPublication := func() error {
		for _, name := range []string{releaseIntentV2Marker, releaseIntentV2Candidate} {
			_, err := custody.owners[0].directory.stat(name)
			if !releaseMeasurementInputV2OnlyMissing(err) {
				return errors.Join(errors.New("native bridge predecessor observation found unresolved intent publication"), err)
			}
		}
		return nil
	}
	if err := checkPublication(); err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, checkPublication()) }()
	file, err := decodeSteeringIntentFileV2(ctx, encoded, limit)
	if err != nil {
		return nil, err
	}
	logical, err := readSteeringIntentHistoryV2(ctx, file, limit, func(name string, maximum uint64) ([]byte, error) {
		return custody.read(ctx, filepath.Join(cfg.StateDir, filepath.FromSlash(name)), maximum, false)
	})
	if err != nil {
		return nil, err
	}
	if file.Current == nil || len(logical.all) == 0 {
		return nil, errors.New("native bridge predecessor observation has no current retained intent")
	}
	for index, intent := range logical.all {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateSteeringIntentLifecycle(intent, index < logical.history); err != nil {
			return nil, err
		}
		if err := intent.VerifyVectorHash(); err != nil {
			return nil, err
		}
		if intent.ValidatorID != cfg.ValidatorID || intent.Netuid != cfg.Netuid {
			return nil, errors.New("native bridge predecessor intent identity differs")
		}
	}
	owner, err := bridge.matchHistory(ctx, logical.all)
	if err != nil {
		return nil, err
	}
	for index := 1; index < len(logical.all); index++ {
		previous, current := logical.all[index-1], logical.all[index]
		if !owner.allowsIntentEdge(previous, current) {
			if err := validateSteeringIntentSuccessor(previous, current); err != nil {
				return nil, err
			}
		}
	}
	current := file.Current
	if current.Status != "applied" || current.SubnetEpoch != epoch || current.MeasurementArtifactHash != artifactHash || current.MeasurementArtifactSize != artifactBytes {
		return nil, errors.New("retained applied predecessor differs from the independently named public artifact")
	}
	if current.MeasurementArtifactPath != filepath.ToSlash(filepath.Join("measurements", artifactHash[len("sha256:"):]+".json")) {
		return nil, errors.New("native bridge predecessor public artifact path differs")
	}
	publicBytes, err := custody.read(ctx, filepath.Join(cfg.StateDir, filepath.FromSlash(current.MeasurementArtifactPath)), artifactBytes, false)
	if err != nil {
		return nil, err
	}
	if uint64(len(publicBytes)) != artifactBytes || ReleaseMeasurementContentHash(publicBytes) != artifactHash {
		return nil, errors.New("native bridge predecessor public artifact bytes differ")
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(ctx, publicBytes, cfg.EvidenceV2.Bounds.MaxArtifactBytes, cfg.EvidenceV2.Bounds.MaxOperators)
	if err != nil {
		return nil, err
	}
	if artifact.SubnetEpoch != epoch || artifact.DeploymentID != cfg.DeploymentID || artifact.ValidatorID != cfg.ValidatorID || artifact.ChainID != cfg.ChainID || artifact.Netuid != cfg.Netuid || artifact.PolicyHash != current.PolicyHash || artifact.SettlementEpoch != current.SettlementEpoch {
		return nil, errors.New("native bridge predecessor typed public artifact identity differs")
	}
	return &ReleaseNativeGapBridgePredecessorV2Observation{NativeEpoch: epoch, Status: current.Status, ArtifactHash: artifactHash, ArtifactBytes: artifactBytes, VectorHash: current.VectorHash, ApplicationBlock: current.ApplicationBlock, RetainedIntents: len(logical.all), ExistingBridgeMatches: true}, nil
}
