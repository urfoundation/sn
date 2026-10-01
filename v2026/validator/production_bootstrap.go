// Offline bootstrap inspection reuses production admission without returning
// a producer configuration, reading credentials or granting runtime authority.
package validator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// These public facts are a read-only projection of a fully checked initial
// schema-3 config and signed approval. They cannot be passed to a producer.
// Declared paths reserve distinct custody namespaces; their contents, including
// operator activation evidence and credentials, remain unverified here.
type ProductionBootstrapInspection struct {
	DeploymentId      string                `json:"deployment_id"`
	ValidatorId       uint64                `json:"validator_id"`
	EvmChainId        uint64                `json:"evm_chain_id"`
	Netuid            uint16                `json:"netuid"`
	Coordinator       string                `json:"coordinator"`
	SettlementVault   string                `json:"settlement_vault"`
	DeployBlock       uint64                `json:"deploy_block"`
	PolicyHash        string                `json:"policy_hash"`
	ApprovalSigner    string                `json:"approval_signer_ed25519"`
	ApprovalReference ReleaseEvidenceV2File `json:"approval_reference"`
	Approval          OwnerRecycleApproval  `json:"approval"`
	DeclaredPaths     []string              `json:"declared_paths"`
}

// Borrows the exact config bytes for this synchronous call. The caller pins
// them independently; the pathname supplies context only and is never reopened.
// Only the declared content-addressed approval is read. Initial bootstrap has
// no inherited production history and cannot use retained-state fallbacks.
// Physical path checks inspect metadata, never credential/evidence contents.
func InspectProductionBootstrapConfig(ctx context.Context, path string, raw []byte) (*ProductionBootstrapInspection, error) {
	if ctx == nil {
		return nil, errors.New("production bootstrap context is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateReleaseEvidenceV2Path(path); err != nil {
		return nil, err
	}
	cfg, err := decodeReleaseConfigDocument(path, raw)
	if err != nil {
		return nil, err
	}
	if cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion || len(cfg.ProductionRuntimeApprovals) != 0 ||
		len(cfg.ProductionAuthorityHistory) != 0 || len(cfg.MainnetRuntimeApprovals) != 0 {
		return nil, errors.New("production bootstrap requires initial schema 3 without inherited runtime or authority history")
	}
	cfg.Coordinator, cfg.SettlementVault = strings.ToLower(cfg.Coordinator), strings.ToLower(cfg.SettlementVault)
	if err := validateOwnerRecycleApprovalSelection(cfg); err != nil {
		return nil, err
	}
	paths := productionBootstrapDeclaredPaths(cfg)
	for _, reserved := range append([]string{path}, paths...) {
		approvalPath := cfg.OwnerRecycleApproval.Approval.Path
		if approvalPath == reserved || strings.HasPrefix(approvalPath, reserved+string(filepath.Separator)) || strings.HasPrefix(reserved, approvalPath+string(filepath.Separator)) {
			return nil, errors.New("production bootstrap approval overlaps declared config, credentials, state or evidence")
		}
	}
	approvalRaw, err := ReadReleaseEvidenceV2File(ctx, cfg.OwnerRecycleApproval.Approval, maximumOwnerRecycleApprovalBytes)
	if err != nil {
		return nil, err
	}
	if err := loadOwnerRecycleProductionConfigBytes(cfg, approvalRaw); err != nil {
		return nil, err
	}
	if err := loadReleaseProductionRuntimeHistoryBytes(cfg, nil); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &ProductionBootstrapInspection{DeploymentId: cfg.DeploymentID, ValidatorId: cfg.ValidatorID, EvmChainId: cfg.ChainID, Netuid: cfg.Netuid,
		Coordinator: cfg.Coordinator, SettlementVault: cfg.SettlementVault, DeployBlock: cfg.DeployBlock, PolicyHash: cfg.PolicyHash,
		ApprovalSigner: cfg.OwnerRecycleApproval.Signer, ApprovalReference: cfg.OwnerRecycleApproval.Approval,
		Approval: approved.Approval, DeclaredPaths: paths}, nil
}

// The normalized path inventory contains declarations only; gathering it must
// precede approval I/O so a declared credential cannot become an input source.
func productionBootstrapDeclaredPaths(cfg *ReleaseConfig) []string {
	paths := []string{cfg.StateDir, cfg.HotkeySeedFile}
	for _, operator := range cfg.Operators {
		paths = append(paths, operator.StateDir, operator.NetworkJWTFile, operator.ClientJWTFile, operator.ClientKeySeedFile)
	}
	for _, operator := range cfg.EvidenceV2.Operators {
		for _, file := range operator.Files() {
			paths = append(paths, file.Path)
		}
		paths = append(paths, operator.ReplayScratchRoot, operator.SealScratchRoot)
	}
	return paths
}
