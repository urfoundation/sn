//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
)

// CaptureReleaseHistoryAdoptionV2 produces an approval request from stopped
// local history and the exact planned config bytes. It neither changes disk
// nor authenticates history on behalf of the later strict startup owner.
func CaptureReleaseHistoryAdoptionV2(ctx context.Context, configPath string, configBytes []byte, approvedPlanHash, sourcePlanHash string, firstNativeEpoch uint64) (encoded []byte, resultErr error) {
	if ctx == nil || !filepath.IsAbs(configPath) || filepath.Clean(configPath) != configPath {
		return nil, errors.New("strict history capture has no exact config owner")
	}
	cfg, err := decodeReleaseConfigBytes(configPath, bytes.Clone(configBytes))
	if err != nil {
		return nil, err
	}
	request := ReleaseHistoryAdoptionV2{Schema: ReleaseHistoryAdoptionV2Schema, DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID,
		ApprovedPlanHash: approvedPlanHash, SourcePlanHash: sourcePlanHash, ConfigSHA256: ReleaseMeasurementContentHash(configBytes),
		CoordinatorStateDir: filepath.Join(filepath.Dir(configPath), "coordinator-state-v2"), FirstNativeEpoch: firstNativeEpoch}
	all, current, prefix, closeOwner, err := readHistoryAdoptionPrefixV2(ctx, cfg, request.CoordinatorStateDir)
	defer func() {
		resultErr = errors.Join(resultErr, closeOwner(), ctx.Err())
		if resultErr != nil {
			encoded = nil
		}
	}()
	if err != nil {
		return nil, err
	}
	request.IntentPrefixSHA256 = ReleaseMeasurementContentHash(prefix)
	request.IntentPrefixCount = uint64(len(all))
	if current != nil {
		request.LastNativeEpoch, request.LastArtifactHash = current.SubnetEpoch, current.MeasurementArtifactHash
	}
	if _, err := request.matchPrefix(ctx, all, prefix != nil, cfg.EvidenceV2.Bounds.MaxHistoryBytes); err != nil {
		return nil, err
	}
	encoded, err = json.MarshalIndent(request, "", "  ")
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if _, err := DecodeReleaseHistoryAdoptionV2(encoded, ReleaseMeasurementContentHash(encoded)); err != nil {
		return nil, err
	}
	return encoded, nil
}

// CheckReleaseHistoryAdoptionV2Source is only a local preflight. Its caller must
// still approve the plan, preserve writer exclusion and run normal full startup.
func CheckReleaseHistoryAdoptionV2Source(ctx context.Context, configPath string, configBytes, encoded []byte, expectedSHA256 string) (resultErr error) {
	request, err := DecodeReleaseHistoryAdoptionV2(encoded, expectedSHA256)
	if err != nil {
		return err
	}
	if ctx == nil || !filepath.IsAbs(configPath) || filepath.Clean(configPath) != configPath || request.ConfigSHA256 != ReleaseMeasurementContentHash(configBytes) || request.CoordinatorStateDir != filepath.Join(filepath.Dir(configPath), "coordinator-state-v2") {
		return errors.New("strict history source differs from its planned config or namespace")
	}
	cfg, err := decodeReleaseConfigBytes(configPath, bytes.Clone(configBytes))
	if err != nil {
		return err
	}
	if cfg.ProvisionalDeferClosedNativeInput || cfg.ProvisionalRuntimeCompatibility != "" || cfg.ChainID != 945 || cfg.Policy.NetworkProfile != "testnet" || cfg.GenesisHash != "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105" || cfg.DeploymentID != request.DeploymentID || cfg.ValidatorID != request.ValidatorID {
		return errors.New("strict history source changes the testnet owner")
	}
	all, _, prefix, closeOwner, err := readHistoryAdoptionPrefixV2(ctx, cfg, request.CoordinatorStateDir)
	defer func() { resultErr = errors.Join(resultErr, closeOwner(), ctx.Err()) }()
	if err != nil {
		return err
	}
	_, err = request.matchPrefix(ctx, all, prefix != nil, cfg.EvidenceV2.Bounds.MaxHistoryBytes)
	return err
}

// The complete logical history under root, sealed segments included, its
// current intent, and the canonical unsealed file of all of it, the bytes an
// adoption pins. An unsealed source pins its own file; an absent one, none.
func readHistoryAdoptionPrefixV2(ctx context.Context, cfg *ReleaseConfig, root string) ([]*SteeringIntent, *SteeringIntent, []byte, func() error, error) {
	bounds := cfg.EvidenceV2.Bounds
	owned := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}
	finish := func() error { return errors.Join(owned.check(), owned.close()) }
	raw, err := owned.read(ctx, filepath.Join(root, "steering-intents.json"), bounds.IntentFileLimit(), true)
	if err != nil || raw == nil {
		return nil, nil, nil, finish, err
	}
	file, err := decodeSteeringIntentFileV2(ctx, raw, bounds.IntentFileLimit())
	if err != nil {
		return nil, nil, nil, finish, errors.Join(errors.New("strict history capture requires the original canonical intent file"), err)
	}
	logical, err := readSteeringIntentHistoryV2(ctx, file, bounds.IntentFileLimit(), func(name string, maximum uint64) ([]byte, error) {
		return owned.read(ctx, filepath.Join(root, filepath.FromSlash(name)), maximum, false)
	})
	if err != nil {
		return nil, nil, nil, finish, err
	}
	if len(file.Segments) == 0 {
		return logical.all, file.Current, raw, finish, nil
	}
	unsealed := steeringIntentFile{Schema: steeringIntentSchema, Current: file.Current, History: make([]SteeringIntent, 0, logical.history)}
	for _, intent := range logical.all[:logical.history] {
		unsealed.History = append(unsealed.History, *intent)
	}
	prefix, err := marshalAttemptSettlementV2JSON(ctx, &unsealed, bounds.MaxHistoryBytes, true, true)
	return logical.all, file.Current, prefix, finish, err
}
