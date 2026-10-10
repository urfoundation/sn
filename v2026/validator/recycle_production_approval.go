// Production authority is a separately signed finite successor, never an
// observation flag. Its detached capsule binds the complete resolved config.
package validator

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/urfoundation/sn/v2026/crv4"
)

const ownerRecycleProductionApprovalSchema = "urnetwork-owner-recycle-approval-v2"
const ownerRecycleProductionScope = "urnetwork-owner-recycle-production-v1"

// Independent operator approval defines the finite production epoch window
// and the exact validator census whose eligibility is observed per decision.
type OwnerRecycleProductionApproval struct {
	Schema                  string     `json:"schema"`
	RuntimeCapability       string     `json:"runtime_capability"`
	EpochScheduleProfile    string     `json:"epoch_schedule_profile,omitempty"`
	ValidatorHotkeys        [][32]byte `json:"validator_hotkeys"`
	MaximumLastUpdateAge    uint64     `json:"maximum_last_update_age"`
	ValidThroughNativeEpoch uint64     `json:"valid_through_native_epoch"`
	ActivationNativeHash    [32]byte   `json:"activation_native_hash"`
	ActivationNativeBlock   uint64     `json:"activation_native_block,omitempty"`
}

// Only the signature loader creates this owned authority. Exported config
// fields alone cannot recreate it, and later mutation invalidates its seal.
type ownerRecycleProductionAuthority struct {
	encoded        []byte
	configHash     [32]byte
	selection      ReleaseOwnerRecycleApprovalConfig
	prepared       *ownerRecyclePreparedAuthorization
	historicalOnly bool
}

// Initial schema-3 approvals retain their original wire meaning. A compatible
// successor explicitly preserves that first economic block across runtime windows.
func ownerRecycleActivationBlock(approval *OwnerRecycleApproval) uint64 {
	if approval.Production.ActivationNativeBlock != 0 {
		return approval.Production.ActivationNativeBlock
	}
	return approval.ValidFromNativeBlock
}

// Schema selection is only routing; it is never an authority check.
func isOwnerRecycleProductionConfig(cfg *ReleaseConfig) bool {
	return cfg != nil && (cfg.SchemaVersion == ReleaseMainnetProductionSchemaVersion || cfg.ownerRecycleProduction != nil ||
		cfg.productionRuntimeHistory != nil || len(cfg.ProductionRuntimeApprovals) != 0 ||
		cfg.productionAuthorityHistory != nil || len(cfg.ProductionAuthorityHistory) != 0)
}

// Old observer approvals permit only their first decision. Production has an
// independently signed finite interval, without inventing late activation.
func ownerRecycleDecisionEpochApproved(approval *OwnerRecycleApproval, epoch uint64) bool {
	if approval.Production == nil {
		return epoch == approval.FirstNativeEpoch
	}
	return epoch >= approval.FirstNativeEpoch && epoch <= approval.Production.ValidThroughNativeEpoch
}

// A continuing finite window does not invent a late first activation. Its
// nonempty predecessor has already been authenticated by the V2 intent owner.
func requireOwnerRecycleProductionFirstIntent(cfg *ReleaseConfig, previous *SteeringIntent, epoch uint64) error {
	if !isOwnerRecycleProductionConfig(cfg) {
		return nil
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return err
	}
	if previous == nil {
		if epoch != approved.Approval.FirstNativeEpoch {
			return errors.New("owner-recycle initial intent missed its signed drained activation epoch")
		}
	} else if productionEconomicIntent(previous) == nil {
		return errors.New("owner-recycle production cannot reinterpret a parent intent as its successor predecessor")
	} else if cfg.TreasuryApproval != nil && previous.Treasury == nil {
		if previous.Status != "applied" || epoch != approved.Approval.FirstNativeEpoch || previous.SubnetEpoch >= epoch {
			return errors.New("treasury transition requires an applied original intent and its exact new drained first epoch")
		}
	} else if cfg.TreasuryApproval == nil && previous.Treasury != nil {
		return errors.New("owner-recycle configuration cannot continue a treasury intent")
	}
	return nil
}

// Old admission signatures remain read-only and cannot be relabeled as a
// production approval, even when every other tuple happens to match.
func validateOwnerRecycleProductionApproval(cfg *ReleaseConfig, approval *OwnerRecycleApproval) error {
	if approval == nil {
		return errors.New("owner-recycle approval is absent")
	}
	if !isOwnerRecycleProductionConfig(cfg) {
		if approval.Production != nil || approval.Schema != ownerRecycleApprovalSchema {
			return errors.New("owner-recycle observation config cannot admit a production approval")
		}
		return nil
	}
	p := approval.Production
	approvalSchema, scope, proposalSchema := ownerRecycleProductionApprovalSchema, ownerRecycleProductionScope, ownerRecycleProposalSchema
	if cfg.TreasuryApproval != nil {
		approvalSchema, scope, proposalSchema = TreasuryApprovalSchema, TreasuryProductionScope, TreasuryProposalSchema
	}
	if approval.Schema != approvalSchema || approval.Proposal.Schema != proposalSchema ||
		(cfg.TreasuryApproval != nil) != (approval.Proposal.Treasury != nil) || p == nil || p.Schema != scope ||
		p.RuntimeCapability != crv4.ValidatorProducerRuntimeProfile || p.MaximumLastUpdateAge == 0 || p.ActivationNativeHash == ([32]byte{}) ||
		p.ValidThroughNativeEpoch < approval.FirstNativeEpoch || len(p.ValidatorHotkeys) < cfg.Policy.Safety.MinimumLiveValidatorCount ||
		len(p.ValidatorHotkeys) > maximumOwnerRecycleApprovedHotkeys {
		return errors.New("owner-recycle production approval lacks its exact purpose, finite epoch window or validator census")
	}
	if err := crv4.ValidateEpochScheduleProfile(p.EpochScheduleProfile); err != nil {
		return err
	}
	if ownerRecycleActivationBlock(approval) > approval.ValidFromNativeBlock ||
		ownerRecycleActivationBlock(approval) < approval.ValidFromNativeBlock && len(cfg.ProductionAuthorityHistory) == 0 {
		return errors.New("owner-recycle earlier economic activation requires original production authority history")
	}
	foundSelf := false
	for index, hotkey := range p.ValidatorHotkeys {
		if hotkey == ([32]byte{}) || index > 0 && bytes.Compare(p.ValidatorHotkeys[index-1][:], hotkey[:]) >= 0 {
			return errors.New("owner-recycle production validators must be ordered, unique and nonzero")
		}
		foundSelf = foundSelf || hotkey == approval.ValidatorHotkey
		if treasuryRecipientHotkey(approval.Proposal.Treasury, hotkey) {
			return errors.New("treasury recipient cannot also be an approved validator")
		}
		// Only a treasury approval's own validator may also be an owner.
		for _, owner := range approval.OwnerHotkeys {
			if owner == hotkey && (hotkey != approval.ValidatorHotkey || approval.Proposal.Treasury == nil) {
				return errors.New("owner-recycle production validator cannot be an owner recipient")
			}
		}
	}
	if !foundSelf {
		return errors.New("owner-recycle production census omits the actual signer")
	}
	return nil
}

// Loads signed bytes before keys, journals or workers are opened. Restart may
// use the same immutable retained bytes after the source file is unavailable.
func loadOwnerRecycleProductionConfig(cfg *ReleaseConfig) error {
	if cfg == nil || cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion {
		return errors.New("owner-recycle production requires schema 3")
	}
	if err := validateOwnerRecycleApprovalSelection(cfg); err != nil {
		return err
	}
	raw, err := readRetainedProductionFile(cfg, productionEconomicSelection(cfg).Approval, maximumOwnerRecycleApprovalBytes,
		retainedProductionApprovalPath(cfg), filepath.Join(cfg.StateDir, retainedOwnerRecycleApprovalName))
	if err != nil {
		return err
	}
	return loadOwnerRecycleProductionConfigBytes(cfg, raw)
}

// A selected immutable history bundle can supply these original bytes after
// their source path disappears. The original independent signature still gates.
func loadOwnerRecycleProductionConfigBytes(cfg *ReleaseConfig, raw []byte) error {
	if cfg == nil || cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion {
		return errors.New("production authority bytes require schema 3")
	}
	if err := validateOwnerRecycleApprovalSelection(cfg); err != nil {
		return err
	}
	if err := matchProductionAuthorityBytes(productionEconomicSelection(cfg).Approval, raw, maximumOwnerRecycleApprovalBytes); err != nil {
		return err
	}
	if _, err := decodeOwnerRecycleApproval(cfg, raw); err != nil {
		return err
	}
	hash, err := OwnerRecycleConfigHash(cfg)
	if err != nil {
		return err
	}
	cfg.ownerRecycleProduction = &ownerRecycleProductionAuthority{encoded: bytes.Clone(raw), configHash: hash, selection: *productionEconomicSelection(cfg)}
	return nil
}

// Revalidates the signed immutable input instead of trusting cached booleans.
// It deliberately does not call ReleaseConfig.Validate and cannot recurse.
func validateOwnerRecycleProductionConfig(cfg *ReleaseConfig) error {
	_, err := ownerRecycleProductionApproval(cfg)
	return err
}

// Returns a newly decoded value; callers cannot mutate the retained capsule.
func ownerRecycleProductionApproval(cfg *ReleaseConfig) (*OwnerRecycleApprovalEnvelope, error) {
	if cfg == nil || cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion || cfg.ownerRecycleProduction == nil || productionEconomicSelection(cfg) == nil {
		return nil, errors.New("owner-recycle production configuration lacks independently loaded authority")
	}
	owner := cfg.ownerRecycleProduction
	hash, err := OwnerRecycleConfigHash(cfg)
	if err != nil || hash != owner.configHash || !reflect.DeepEqual(owner.selection, *productionEconomicSelection(cfg)) {
		return nil, errors.Join(errors.New("owner-recycle production configuration changed after approval"), err)
	}
	approval, err := decodeOwnerRecycleApproval(cfg, owner.encoded)
	if err != nil {
		return nil, fmt.Errorf("owner-recycle production authority: %w", err)
	}
	return approval, nil
}
