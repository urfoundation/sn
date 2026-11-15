// Incomplete historical fleet renewals retain their real writes. Only a later
// complete renewal may close them; an absent write never becomes a success.
package main

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Before any replacement binding, a partial wave can only shorten existing
// member leases. Its native/mirror writes are proven but do not advance a lease.
func verifyFinalIncompleteFleetRenewal(evidence *FinalSemanticEvidence, renewal FinalFleetRenewalRoundEvidence, fleet FinalFleetRenewalFleetEvidence, before FinalFleetGenerationVersionEvidence) (FinalFleetGenerationVersionEvidence, error) {
	if !fleet.Incomplete || len(fleet.Members) != len(before.Members) {
		return before, errors.New("incomplete fleet renewal omits predecessor membership")
	}
	if fleet.Version.CommitmentAction.ActionID != "" {
		version := fleet.Version
		if before.Generation == ^uint64(0) || version.Generation != before.Generation+1 || version.Hotkey != before.Hotkey || version.CommitmentHash == before.CommitmentHash || version.NativeHead.Number <= before.NativeHead.Number || version.CommitmentAction.PlanHash != renewal.ApprovedPlanHash {
			return before, errors.New("incomplete fleet renewal changed its approved native generation")
		}
		if err := verifyFinalFleetGenerationVersionForAction(evidence, fleet.FleetID, version, version.Generation, false, fleetRenewalActionID(renewal.Round, int(fleet.FleetID), "commitment", 0)); err != nil {
			return before, err
		}
		for index, member := range version.Members {
			prior := before.Members[index]
			if member.ClientID != prior.ClientID || member.ClientKey != prior.ClientKey || member.FleetKey != prior.FleetKey || member.UID != prior.UID || member.ValidFromEpoch != renewal.ValidFromEpoch || member.ValidToEpoch != renewal.ValidToEpoch {
				return before, errors.New("incomplete fleet renewal changed approved membership or validity")
			}
		}
	} else if !reflect.DeepEqual(fleet.Version, FinalFleetGenerationVersionEvidence{}) {
		return before, errors.New("incomplete fleet renewal has unproven native fields")
	}
	if fleet.Mirror.Action.ActionID != "" {
		mirror, err := finalFleetGenerationSingleDecodedEvent(evidence, fleet.Mirror)
		if err != nil || fleet.Version.CommitmentAction.ActionID == "" || mirror.Name != "CommitmentMirrored" || !finalFleetGenerationMirrorMatches(mirror.Evidence, fleet.Version) || fleet.Mirror.Action.ActionID != fleetRenewalActionID(renewal.Round, int(fleet.FleetID), "mirror", 0) || fleet.Mirror.Action.PlanHash != renewal.ApprovedPlanHash {
			return before, stateMismatchError(err, "incomplete fleet renewal mirror lacks its approved native checkpoint")
		}
	} else if !reflect.DeepEqual(fleet.Mirror, FinalFleetGenerationWriteEvidence{}) {
		return before, errors.New("incomplete fleet renewal has unproven mirror fields")
	}
	next := before
	next.Members = append([]FinalFleetGenerationMemberEvidence(nil), before.Members...)
	for index, member := range fleet.Members {
		prior := before.Members[index]
		if member.Member != uint64(index+1) || !finalFleetRenewalMemberEqual(member.Prior, prior) || !reflect.DeepEqual(member.Binding, FinalFleetGenerationWriteEvidence{}) {
			return before, errors.New("incomplete fleet renewal altered its predecessor or hid a binding")
		}
		if member.Revocation == nil {
			continue
		}
		revoked, err := finalFleetGenerationSingleDecodedEvent(evidence, *member.Revocation)
		if err != nil || fleet.Mirror.Action.ActionID == "" || prior.ValidToEpoch < renewal.ValidFromEpoch || revoked.Name != "FleetBindingRevoked" || revoked.Evidence.ClientID != prior.ClientID || revoked.Evidence.Generation != prior.Generation || revoked.Evidence.ValidFromEpoch != renewal.ValidFromEpoch || member.Revocation.Action.ActionID != fleetRenewalActionID(renewal.Round, int(fleet.FleetID), "revoke", index+1) || member.Revocation.Action.PlanHash != renewal.ApprovedPlanHash {
			return before, stateMismatchError(err, "incomplete fleet renewal changed its predecessor revocation")
		}
		next.Members[index].ValidToEpoch = renewal.ValidFromEpoch - 1
	}
	return next, nil
}

// Presence is based on the exact source journal, not an optional artifact path.
func (self *finalFleetGenerationSource) renewalHasFinalizedAction(actionId, planHash string) bool {
	for _, entry := range self.entries {
		if entry.Stage == StageFinalized && entry.ActionID == actionId && entry.PlanHash == planHash {
			return true
		}
	}
	return false
}

// Preserve native, mirror and revoke evidence for a wave that never bound any
// replacements. Mixed member generations require their own explicit recovery.
func (self *finalFleetGenerationSource) buildIncompleteFleetRenewal(renewal FleetRenewal, approved *SetupPlan, planned FleetRenewalFleet, fleet *FinalFleetRenewalFleetEvidence) error {
	fleet.Incomplete = true
	manifest, err := protocol.ParseFleetManifest(planned.Manifest)
	if err != nil {
		return err
	}
	commitmentId := fleetRenewalActionID(renewal.Round, planned.Fleet, "commitment", 0)
	if self.renewalHasFinalizedAction(commitmentId, approved.PlanHash) {
		fleet.Version, _, err = self.renewalNativeVersion(fleet.FleetID, manifest.Generation, fleetRenewalStem(renewal.Round, planned.Fleet), commitmentId, planned.Manifest)
		if err != nil {
			return err
		}
		for index, member := range planned.Members {
			fleet.Version.Members = append(fleet.Version.Members, finalFleetRenewalMemberProjection(uint64(index+1), member.Binding))
		}
	}
	mirrorId := fleetRenewalActionID(renewal.Round, planned.Fleet, "mirror", 0)
	if self.renewalHasFinalizedAction(mirrorId, approved.PlanHash) {
		data, err := finalFleetRenewalMirrorCalldata(*manifest, fleet.Version)
		if err != nil {
			return err
		}
		fleet.Mirror, err = self.renewalWrite(mirrorId, approved.PlanHash, data)
		if err != nil {
			return err
		}
	}
	for index, member := range planned.Members {
		number := index + 1
		row := FinalFleetRenewalMemberEvidence{Member: uint64(number), Prior: finalFleetRenewalMemberProjection(uint64(number), member.Prior)}
		row.Prior.ValidToEpoch, err = member.priorEffectiveValidTo()
		if err != nil {
			return err
		}
		revokeId := fleetRenewalActionID(renewal.Round, planned.Fleet, "revoke", number)
		if self.renewalHasFinalizedAction(revokeId, approved.PlanHash) {
			signature, ok := evidenceFixedHex(member.RevokeSignature, 64)
			if !ok {
				return errors.New("incomplete fleet renewal has no original client revocation signature")
			}
			data, err := stabi.NewSTCoordinator().TryPackRevokeFleetBinding(manifest.Members[index].ClientID, member.Prior.Generation, renewal.ValidFromEpoch, signature)
			if err != nil {
				return err
			}
			revoke, err := self.renewalWrite(revokeId, approved.PlanHash, data)
			if err != nil {
				return fmt.Errorf("incomplete fleet %d member %d: %w", planned.Fleet, number, err)
			}
			row.Revocation = &revoke
		}
		fleet.Members = append(fleet.Members, row)
	}
	return nil
}
