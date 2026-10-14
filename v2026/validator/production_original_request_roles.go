// Review-only original-request role output borrows exact approved config bytes.
// It cannot reuse the old ReleaseConfig signature for these additional fields.
package validator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

// This independent selection supplies original custody, client and VPK identity.
// No current login, SQL row, retained leaf or config approval key supplies it.
type ProductionOriginalRequestSelection struct {
	NoId        uint64                `json:"no_id"`
	Preparation ReleaseEvidenceV2File `json:"request_preparation"`
}

// OperatorConfig is the exact runtime consumer type, copied from the original
// config with the two reviewed additions. Publication preparation binds the same
// complete roster and existing evidence bounds for later stopped-writer setup.
type ProductionOriginalRequestRoleConfig struct {
	Operators              []OperatorConfig                      `json:"operators"`
	PublicationPreparation ProviderAttemptPublicationPreparation `json:"publication_preparation"`
	PublicationSha256      string                                `json:"publication_sha256"`
}

// Every original validator/operator is required. Profile is an explicit Server
// installation selection; policy network defaults cannot manufacture that fact.
func InspectProductionOriginalRequestRoles(ctx context.Context, path string, raw []byte, profile string, selections []ProductionOriginalRequestSelection) (ProductionOriginalRequestRoleConfig, error) {
	var result ProductionOriginalRequestRoleConfig
	if profile != "testnet" && profile != "mainnet" {
		return result, errors.New("original receipt scope requires explicit testnet or mainnet Server profile")
	}
	inspection, err := InspectProductionBootstrapConfig(ctx, path, raw)
	if err != nil {
		return result, err
	}
	cfg, err := decodeReleaseConfigDocument(path, raw)
	if err != nil {
		return result, err
	}
	coordinator := common.HexToAddress(cfg.Coordinator)
	if cfg.ChainID == 0 || coordinator == (common.Address{}) || !common.IsHexAddress(cfg.Coordinator) {
		return result, errors.New("original receipt scope has no admitted deployment key")
	}
	genesis, err := canonicalAttemptHex32("original receipt genesis", cfg.GenesisHash, false)
	if err != nil {
		return result, err
	}
	policy, err := canonicalAttemptHex32("original receipt policy", cfg.PolicyHash, false)
	if err != nil {
		return result, err
	}
	if len(selections) == 0 || len(selections) != len(cfg.Operators) || len(selections) > 64 {
		return result, errors.New("original request export requires the complete original operator census")
	}
	selected := map[uint64]ReleaseEvidenceV2File{}
	for _, selection := range selections {
		if _, exists := selected[selection.NoId]; exists {
			return result, errors.New("original request export repeats an operator selection")
		}
		if err := selection.Preparation.Validate(4096); err != nil {
			return result, err
		}
		for _, reserved := range append([]string{path, inspection.ApprovalReference.Path}, inspection.DeclaredPaths...) {
			if productionOriginalRequestPathsOverlap(selection.Preparation.Path, reserved) {
				return result, errors.New("request preparation overlaps original configuration, credentials or mutable custody")
			}
		}
		for _, prior := range selected {
			if productionOriginalRequestPathsOverlap(selection.Preparation.Path, prior.Path) {
				return result, errors.New("original request preparation files overlap across owners")
			}
		}
		selected[selection.NoId] = selection.Preparation
	}
	for _, operator := range cfg.Operators {
		reference, ok := selected[operator.NoID]
		if !ok || operator.AllowClientRegistration {
			return result, errors.New("original request role requires its selected retained operator identity")
		}
		encoded, err := ReadReleaseEvidenceV2File(ctx, reference, 4096)
		if err != nil {
			return result, err
		}
		var original ProviderAttemptRequestPreparation
		if err := attemptStoreDecode(encoded, &original); err != nil {
			return result, err
		}
		if err := original.Validate(); err != nil {
			return result, err
		}
		identity := original.Identity
		ledger := identity.Ledger
		if ledger.DeploymentID != cfg.DeploymentID || ledger.ChainID != cfg.ChainID || ledger.GenesisHash != cfg.GenesisHash || ledger.Netuid != cfg.Netuid || ledger.ValidatorID != cfg.ValidatorID || ledger.NoID != operator.NoID || identity.PolicyHash != policy || identity.Coordinator != strings.ToLower(coordinator.Hex()) {
			return result, errors.New("request preparation differs from the signed original validator/operator deployment")
		}
		if err := ValidateProviderAttemptRequestRootCapacity(cfg.EvidenceV2.Bounds.Disk, original.Limits); err != nil {
			return result, err
		}
		scope := protocol.ProviderAttemptReceiptScope{Profile: profile, GenesisHash: genesis, DeploymentId: cfg.DeploymentID,
			DeploymentKey: fmt.Sprintf("%d:%s", cfg.ChainID, strings.ToLower(coordinator.Hex())), PolicyHash: policy, Netuid: uint64(cfg.Netuid), NoId: operator.NoID}
		if operator.RequestPreparation != nil && *operator.RequestPreparation != reference || operator.RequestReceiptScope != nil && *operator.RequestReceiptScope != scope {
			return result, errors.New("request role export cannot replace previously approved original source custody")
		}
		operator.RequestPreparation, operator.RequestReceiptScope = &reference, &scope
		result.Operators = append(result.Operators, operator)
	}
	// This copy is used only for the same pure profile computation as runtime.
	// No ReleaseConfig, approval envelope or signing capability is returned.
	candidate := *cfg
	candidate.Operators = result.Operators
	result.PublicationPreparation, err = providerAttemptPublicationPreparation(ctx, &candidate)
	if err != nil {
		return ProductionOriginalRequestRoleConfig{}, err
	}
	result.PublicationSha256, err = result.PublicationPreparation.Digest()
	if err != nil {
		return ProductionOriginalRequestRoleConfig{}, err
	}
	if err := ctx.Err(); err != nil {
		return ProductionOriginalRequestRoleConfig{}, err
	}
	return result, nil
}

// Both containment directions matter: an approved input is never selected from
// within a mutable root or used as an ancestor of another original owner.
func productionOriginalRequestPathsOverlap(first, second string) bool {
	for _, pair := range [][2]string{{first, second}, {second, first}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
