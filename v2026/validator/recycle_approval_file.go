//go:build linux || darwin

// Approval installation uses the existing immutable, no-replace descriptor
// custody owner. It authenticates provided signatures and never generates one.
package validator

import (
	"context"
	"path/filepath"
)

// Retains one exact independently signed successor, with file and directory
// fsync. Production successors retain content-addressed originals; observation
// approvals retain their unchanged fixed record. Neither path replaces bytes.
// The configuration is borrowed and must remain immutable during the call.
func RetainOwnerRecycleApproval(ctx context.Context, cfg *ReleaseConfig) (ReleaseEvidenceV2File, error) {
	if err := validateOwnerRecycleApprovalSelection(cfg); err != nil {
		return ReleaseEvidenceV2File{}, err
	}
	if isOwnerRecycleProductionConfig(cfg) {
		if err := validateOwnerRecycleProductionConfig(cfg); err != nil {
			return ReleaseEvidenceV2File{}, err
		}
		reference, err := WriteReleaseEvidenceV2File(ctx, retainedProductionApprovalPath(cfg), cfg.ownerRecycleProduction.encoded, maximumOwnerRecycleApprovalBytes)
		if err != nil {
			return ReleaseEvidenceV2File{}, err
		}
		if _, err := RetainOwnerRecycleProductionAuthority(ctx, cfg); err != nil {
			return ReleaseEvidenceV2File{}, err
		}
		return reference, nil
	}
	raw, err := ReadReleaseEvidenceV2File(ctx, cfg.OwnerRecycleApproval.Approval, maximumOwnerRecycleApprovalBytes)
	if err != nil {
		return ReleaseEvidenceV2File{}, err
	}
	if _, err := decodeOwnerRecycleApproval(cfg, raw); err != nil {
		return ReleaseEvidenceV2File{}, err
	}
	return WriteReleaseEvidenceV2File(ctx, filepath.Join(cfg.StateDir, retainedOwnerRecycleApprovalName), raw, maximumOwnerRecycleApprovalBytes)
}

// All prior bundles remain unchanged. Exact retries are idempotent, including
// after a partial write; the existing immutable writer owns file/directory fsync.
func RetainOwnerRecycleProductionAuthority(ctx context.Context, cfg *ReleaseConfig) (ReleaseEvidenceV2File, error) {
	raw, err := BuildOwnerRecycleProductionAuthority(ctx, cfg)
	if err != nil {
		return ReleaseEvidenceV2File{}, err
	}
	for index, encoded := range cfg.productionRuntimeHistory.encoded {
		path := retainedProductionRuntimePath(cfg, cfg.ProductionRuntimeApprovals[index].SHA256)
		if _, err := WriteReleaseEvidenceV2File(ctx, path, encoded, maximumReleaseMainnetRuntimeApprovalBytes); err != nil {
			return ReleaseEvidenceV2File{}, err
		}
	}
	if cfg.productionAuthorityHistory != nil {
		for index, entry := range cfg.productionAuthorityHistory.entries {
			path := retainedProductionAuthorityPath(cfg, cfg.ProductionAuthorityHistory[index].SHA256)
			if _, err := WriteReleaseEvidenceV2File(ctx, path, entry.encoded, maximumProductionAuthorityBundleBytes); err != nil {
				return ReleaseEvidenceV2File{}, err
			}
		}
	}
	return WriteReleaseEvidenceV2File(ctx, retainedProductionAuthorityPath(cfg, ReleaseMeasurementContentHash(raw)), raw, maximumProductionAuthorityBundleBytes)
}
