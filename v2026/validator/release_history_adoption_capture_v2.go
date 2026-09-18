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
	file, raw, closeOwner, err := readHistoryAdoptionPrefixV2(ctx, cfg, request.CoordinatorStateDir)
	defer func() {
		resultErr = errors.Join(resultErr, closeOwner(), ctx.Err())
		if resultErr != nil {
			encoded = nil
		}
	}()
	if err != nil {
		return nil, err
	}
	request.IntentPrefixSHA256 = ReleaseMeasurementContentHash(raw)
	request.IntentPrefixCount = uint64(len(file.History))
	if file.Current != nil {
		request.IntentPrefixCount++
		request.LastNativeEpoch, request.LastArtifactHash = file.Current.SubnetEpoch, file.Current.MeasurementArtifactHash
	}
	if _, err := request.matchPrefix(ctx, file, raw, cfg.EvidenceV2.Bounds.IntentFileLimit()); err != nil {
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
	if cfg.ProvisionalDeferClosedNativeInput || cfg.ChainID != 945 || cfg.Policy.NetworkProfile != "testnet" || cfg.GenesisHash != "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105" || cfg.DeploymentID != request.DeploymentID || cfg.ValidatorID != request.ValidatorID {
		return errors.New("strict history source changes the testnet owner")
	}
	file, raw, closeOwner, err := readHistoryAdoptionPrefixV2(ctx, cfg, request.CoordinatorStateDir)
	defer func() { resultErr = errors.Join(resultErr, closeOwner(), ctx.Err()) }()
	if err != nil {
		return err
	}
	_, err = request.matchPrefix(ctx, file, raw, cfg.EvidenceV2.Bounds.IntentFileLimit())
	return err
}

func readHistoryAdoptionPrefixV2(ctx context.Context, cfg *ReleaseConfig, root string) (*steeringIntentFile, []byte, func() error, error) {
	owned := &releaseEvidenceV2StartupReferences{remaining: cfg.EvidenceV2.Bounds.IntentFileLimit()}
	finish := func() error { return errors.Join(owned.check(), owned.close()) }
	raw, err := owned.read(ctx, filepath.Join(root, "steering-intents.json"), cfg.EvidenceV2.Bounds.IntentFileLimit(), true)
	if err != nil {
		return nil, nil, finish, err
	}
	file := &steeringIntentFile{Schema: steeringIntentSchema}
	if raw == nil {
		return file, nil, finish, nil
	}
	if err := json.Unmarshal(raw, file); err != nil {
		return nil, nil, finish, err
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, file, cfg.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil || file.Schema != steeringIntentSchema || !bytes.Equal(raw, canonical) {
		return nil, nil, finish, errors.Join(errors.New("strict history capture requires the original canonical intent file"), err)
	}
	return file, raw, finish, nil
}
