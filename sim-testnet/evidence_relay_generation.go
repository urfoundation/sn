//go:build linux || darwin

// One activated policy generation appends authority to a retained continuation.
// Original source snapshots and aggregate funding never move into a new lane.
package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const evidenceRelayContinuationGenerationSchema = "urnetwork-sim-evidence-relay-continuation-v7"

// Frozen captures retain any real suffix after the original approval. Sources
// contain exactly one active generation; no third generation or new reserve.
type evidenceRelayActiveGeneration struct {
	Generation        uint64                                    `json:"generation"`
	RolloverPlanHash  string                                    `json:"rollover_plan_hash"`
	SourcePlanHash    string                                    `json:"source_plan_hash"`
	HandoffSha256     string                                    `json:"handoff_sha256"`
	CutoffEpoch       uint64                                    `json:"cutoff_epoch"`
	FirstFullEpoch    uint64                                    `json:"first_full_epoch"`
	SourceRoleOverlay *validatorcomponent.ReleaseEvidenceV2File `json:"source_role_overlay,omitempty"`
	Frozen            []EvidenceRelayContinuationSource         `json:"frozen"`
	Sources           []EvidenceRelayContinuationSource         `json:"sources"`
	Runtime           []evidenceRelayGenerationRuntime          `json:"runtime"`
}

// Actual selection authenticates the durable activation and source-role
// receipts again; appendix metadata cannot substitute a handoff.
func (self *evidenceRelayActiveGeneration) matchesHandoff(handoff *policyRolloverHandoffV2) error {
	if self == nil || handoff == nil || self.Generation != handoff.Generation || self.Generation == 0 || self.RolloverPlanHash != handoff.PlanHash || self.SourcePlanHash != handoff.SourcePlanHash || self.HandoffSha256 != handoff.sourceSHA256 || self.CutoffEpoch != handoff.CutoffEpoch || self.FirstFullEpoch != handoff.FirstFullEpoch || !reflect.DeepEqual(self.SourceRoleOverlay, handoff.SourceRoleOverlay) || len(self.Runtime) != 2 || len(self.Sources) != 4 || len(handoff.Members) != 4 || len(handoff.Validators) != 2 {
		return errors.New("relay generation differs from its authenticated activated handoff")
	}
	for index, source := range self.Sources {
		owner := handoff.Validators[index/2]
		if source.Activation != handoff.Members[index].Activation || source.ValidatorID != owner.ValidatorID || source.CoordinatorStateDir != owner.StateDir || self.Runtime[index/2].Original != owner.Config {
			return errors.New("relay generation replaced its activation or selected namespace")
		}
	}
	return nil
}

// Pure shape validation is repeated before any runtime reader. Signature,
// namespace-prefix and chain authentication remain stopped capture's job.
func (self *EvidenceRelayContinuation) validateActiveGeneration(plan *SetupPlan) error {
	if self.Schema != evidenceRelayContinuationGenerationSchema {
		if self.ActiveGeneration != nil {
			return errors.New("relay active generation requires its explicit v7 approval")
		}
		return nil
	}
	generation := self.ActiveGeneration
	if self.ProvisionalCapture != nil || generation == nil || generation.Generation == 0 || !validCanonicalHashHex(generation.RolloverPlanHash) || !plan.allowedPlanHashes()[generation.SourcePlanHash] || !validSHA256String(generation.HandoffSha256) || generation.CutoffEpoch == 0 || generation.CutoffEpoch >= self.EndSettlementEpoch || generation.FirstFullEpoch != generation.CutoffEpoch+1 || len(generation.Frozen) != 4 || len(generation.Sources) != 4 || len(generation.Runtime) != 2 || len(self.Sources) != 4 {
		return errors.New("relay active generation lost its bounded owner or census")
	}
	for index, source := range generation.Sources {
		original := self.Sources[index]
		if err := validateEvidenceRelaySourceAdvance(original, generation.Frozen[index]); err != nil {
			return err
		}
		if source.ValidatorID != original.ValidatorID || source.NoID != original.NoID || source.Activation.Hotkey != original.Activation.Hotkey || source.Activation.NoID != original.NoID || source.Activation.VPK == original.Activation.VPK || source.Activation.Domain.Epoch != generation.CutoffEpoch || source.CoordinatorStateDir == original.CoordinatorStateDir || !filepath.IsAbs(source.CoordinatorStateDir) || filepath.Clean(source.CoordinatorStateDir) != source.CoordinatorStateDir || filepath.Base(source.CoordinatorStateDir) != "coordinator-state-v2" || !validSHA256String(source.IntentPrefixSHA256) || source.Capacity.Identity.ValidatorID != source.ValidatorID || source.Capacity.Identity.NoID != source.NoID || source.Capacity.Identity.ValidatorVPK != fmt.Sprintf("0x%x", source.Activation.VPK) || source.Capacity.Identity.DeploymentID != plan.DeploymentID || original.Activation.Domain.Epoch >= generation.CutoffEpoch {
			return errors.New("relay active generation changed a source owner or cutoff")
		}
		runtime := generation.Runtime[index/2]
		if runtime.ValidatorId != source.ValidatorID || runtime.Content == "" || runtime.Config != policyRolloverFile(runtime.Config.Path, []byte(runtime.Content)) || filepath.Dir(runtime.Config.Path) != filepath.Dir(source.CoordinatorStateDir) || runtime.Config.Path == runtime.Original.Path {
			return errors.New("relay active generation runtime bytes or namespace differ")
		}
	}
	return nil
}

// Prefix growth preserves identity and the exact old root at unchanged length.
// Stopped capture separately replays the signed prefix at changed length.
func validateEvidenceRelaySourceAdvance(previous, next EvidenceRelayContinuationSource) error {
	if next.ValidatorID != previous.ValidatorID || next.NoID != previous.NoID || next.CoordinatorStateDir != previous.CoordinatorStateDir || next.Activation != previous.Activation || next.Capacity.Identity != previous.Capacity.Identity || next.Capacity.Coordinator != previous.Capacity.Coordinator || next.IntentPrefixCount < previous.IntentPrefixCount || next.LastNativeEpoch < previous.LastNativeEpoch || next.Capacity.Head.LastSequence < previous.Capacity.Head.LastSequence || next.Capacity.Head.TrailCount < previous.Capacity.Head.TrailCount || next.Capacity.Head.RecordBytes < previous.Capacity.Head.RecordBytes {
		return errors.New("relay refresh reset or replaced an authenticated source prefix")
	}
	if next.IntentPrefixCount == previous.IntentPrefixCount && (next.IntentPrefixSHA256 != previous.IntentPrefixSHA256 || next.LastNativeEpoch != previous.LastNativeEpoch || next.LastArtifactHash != previous.LastArtifactHash) {
		return errors.New("relay refresh changed unchanged native intent history")
	}
	if next.Capacity.Head.LastSequence == previous.Capacity.Head.LastSequence && next.Capacity.Head != previous.Capacity.Head {
		return errors.New("relay refresh changed unchanged signed ledger history")
	}
	return nil
}

// Closed subjects select their cutoff owner. Old audits can arrive after the
// cutoff but retain their original activation and consume the same reserve.
func (self *EvidenceRelayContinuation) acceptsActivation(activation protocol.ValidatorEvidenceActivation, header protocol.ValidatorEvidenceHeader) bool {
	if self == nil || self.ActiveGeneration != nil && len(self.ActiveGeneration.Sources) != len(self.Sources) {
		return false
	}
	for index, source := range self.Sources {
		if source.Activation == activation {
			return self.ActiveGeneration == nil || header.Kind != protocol.ValidatorEvidenceClosedCensus || header.Epoch < self.ActiveGeneration.CutoffEpoch
		}
		if self.ActiveGeneration != nil && self.ActiveGeneration.Sources[index].Activation == activation {
			return header.Kind != protocol.ValidatorEvidenceClosedCensus || header.Epoch >= self.ActiveGeneration.CutoffEpoch
		}
	}
	return false
}
