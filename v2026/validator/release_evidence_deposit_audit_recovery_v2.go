//go:build linux || darwin

// Original audit publications can be exported without opening any signer.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
)

// Returns only the original public publication bound by an independently
// pinned completed locator. Callers must
// authenticate the original activation, policy and epoch window independently.
// Missing prepared custody never creates a file or opens a signing path.
func ReadPreparedValidatorEvidenceDepositAuditV2(ctx context.Context, stateDir string, manifestHash [32]byte, manifestBytes uint64, options ValidatorEvidencePublicationV2ReadOptions) (*ValidatorEvidenceCensusV2Publication, error) {
	return readPreparedValidatorEvidenceDepositAuditV2(ctx, stateDir, manifestHash, manifestBytes, options, releaseMeasurementInputV2ReadHooks{})
}

// The per-call observer brackets genuine bounded descriptor operations only.
func readPreparedValidatorEvidenceDepositAuditV2(ctx context.Context, stateDir string, manifestHash [32]byte, manifestBytes uint64, options ValidatorEvidencePublicationV2ReadOptions, hooks releaseMeasurementInputV2ReadHooks) (publication *ValidatorEvidenceCensusV2Publication, resultErr error) {
	if ctx == nil {
		return nil, errors.New("prepared deposit audit read context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			publication = nil
		}
	}()
	if manifestHash == ([32]byte{}) || manifestBytes == 0 || manifestBytes > options.Bounds.MaxClosureBytes {
		return nil, errors.New("prepared deposit audit completed locator pin is absent or exceeds its bound")
	}
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(stateDir, options.Window.Epoch, options.Window.Subject)
	if err != nil {
		return nil, err
	}
	readManifest := func() (*ValidatorEvidenceDepositAuditV2Manifest, []byte, error) {
		manifest, err := ReadValidatorEvidenceDepositAuditV2Manifest(ctx, path, options.Bounds.MaxClosureBytes, options.Bounds.MaxParticipants)
		if err != nil {
			return nil, nil, err
		}
		raw, err := marshalAttemptSettlementV2JSON(ctx, manifest, options.Bounds.MaxClosureBytes, true, true)
		if err != nil {
			return nil, nil, err
		}
		if uint64(len(raw)) != manifestBytes || sha256.Sum256(raw) != manifestHash {
			return nil, nil, errors.New("prepared deposit audit completed locator differs from its independent pin")
		}
		return manifest, raw, nil
	}
	manifest, originalManifest, err := readManifest()
	if err != nil {
		return nil, err
	}
	if _, err := admitValidatorEvidenceDepositAuditV2(manifest, options); err != nil {
		return nil, err
	}
	maximum := min(options.Bounds.MaxArtifactBytes, options.Bounds.MaxControlBytes/8, options.Bounds.MaxHistoryBytes)
	if maximum == 0 {
		return nil, errors.New("prepared deposit audit has no finite byte allowance")
	}
	preparedPath := filepath.Join(stateDir, "evidence-deposit-audit-prepared", filepath.Base(path))
	raw, err := readReleaseMeasurementInputV2Context(ctx, preparedPath, maximum, hooks)
	if err != nil {
		return nil, err
	}
	var retained releaseDepositAuditPreparedV2
	if err := decodeValidatorEvidencePublicationV2Json(ctx, raw, maximum, &retained); err != nil {
		return nil, err
	}
	if retained.Schema != releaseDepositAuditPreparedV2Schema || retained.Manifest == nil || retained.Publication == nil {
		return nil, errors.New("prepared deposit audit original publication is absent")
	}
	retainedManifest, err := marshalAttemptSettlementV2JSON(ctx, retained.Manifest, options.Bounds.MaxClosureBytes, true, true)
	if err != nil || !bytes.Equal(retainedManifest, originalManifest) {
		return nil, errors.Join(errors.New("prepared deposit audit escaped its independently pinned completed locator"), err)
	}
	if err := validateValidatorEvidenceDepositAuditV2Publication(ctx, retained.Publication, manifest, options); err != nil {
		return nil, err
	}
	if _, _, err := readManifest(); err != nil {
		return nil, err
	}
	return retained.Publication, ctx.Err()
}
