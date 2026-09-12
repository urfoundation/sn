package main

// Approved renewals extend the sealed original lineage. Neither a terminal
// census nor a larger generation number can stand in for these actual writes.
import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type FinalFleetRenewalRoundEvidence struct {
	Round            uint64                           `json:"round"`
	SourcePlanHash   string                           `json:"source_plan_hash"`
	ApprovedPlanHash string                           `json:"approved_plan_hash"`
	MetadataHash     string                           `json:"metadata_hash"`
	Approval         FinalArtifactLocator             `json:"approval"`
	ValidFromEpoch   uint64                           `json:"valid_from_epoch"`
	ValidToEpoch     uint64                           `json:"valid_to_epoch"`
	Fleets           []FinalFleetRenewalFleetEvidence `json:"fleets"`
}

type FinalFleetRenewalFleetEvidence struct {
	FleetID        uint64                               `json:"fleet_id"`
	Previous       *FinalFleetGenerationVersionEvidence `json:"previous_lifecycle_generation,omitempty"`
	PreviousWrites []FinalFleetGenerationWriteEvidence  `json:"previous_lifecycle_writes,omitempty"`
	Version        FinalFleetGenerationVersionEvidence  `json:"version"`
	Mirror         FinalFleetGenerationWriteEvidence    `json:"mirror"`
	Members        []FinalFleetRenewalMemberEvidence    `json:"members"`
}

type FinalFleetRenewalMemberEvidence struct {
	Member     uint64                             `json:"member"`
	Prior      FinalFleetGenerationMemberEvidence `json:"prior"`
	Revocation *FinalFleetGenerationWriteEvidence `json:"revocation,omitempty"`
	Binding    FinalFleetGenerationWriteEvidence  `json:"binding"`
}

// Only canonical coordinates can expand the direct-write event/target scope.
func finalFleetRenewalActionCoordinates(id string) (round uint64, fleet, member int, operation string, ok bool) {
	parts := strings.Split(id, ".")
	if len(parts) != 5 && len(parts) != 6 || parts[0] != "fleet" || parts[1] != "renew" {
		return
	}
	var err error
	round, err = strconv.ParseUint(parts[2], 10, 64)
	if err != nil || round == 0 {
		return
	}
	fleet, err = strconv.Atoi(parts[3])
	if err != nil || fleet < 1 || fleet > int(finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount) {
		return
	}
	operation = parts[4]
	if len(parts) == 6 {
		member, err = strconv.Atoi(parts[5])
		if err != nil || member < 1 || member > int(finalFleetGenerationMembersPerFleet) {
			return
		}
	}
	switch operation {
	case "commitment", "mirror":
		if member != 0 {
			return
		}
	case "bind", "revoke":
		if member == 0 {
			return
		}
	default:
		return
	}
	ok = id == fleetRenewalActionID(round, fleet, operation, member)
	return
}

func finalFleetRenewalPreviousPrefix(fleet, generation uint64) string {
	if fleet == fleetLifecycleTargetFleet {
		if generation == 3 {
			return "lifecycle.prepare.target"
		}
		return "lifecycle.provider"
	}
	if fleet == fleetLifecycleCompanionFleet {
		if generation == 3 {
			return "lifecycle.prepare.companion"
		}
		return "lifecycle.terminal"
	}
	return ""
}

func finalFleetRenewalDirectOperation(id string) string {
	if _, _, _, operation, ok := finalFleetRenewalActionCoordinates(id); ok && operation != "commitment" {
		return operation
	}
	for _, prefix := range []string{"lifecycle.prepare.target", "lifecycle.prepare.companion", "lifecycle.provider", "lifecycle.terminal"} {
		if id == prefix+".mirror" {
			return "mirror"
		}
		for member := 1; member <= int(finalFleetGenerationMembersPerFleet); member++ {
			if id == fmt.Sprintf("%s.bind.%d", prefix, member) {
				return "bind"
			}
		}
	}
	return ""
}

func finalFleetRenewalWrites(fleet FinalFleetRenewalFleetEvidence) []FinalFleetGenerationWriteEvidence {
	writes := append([]FinalFleetGenerationWriteEvidence(nil), fleet.PreviousWrites...)
	writes = append(writes, fleet.Mirror)
	for _, member := range fleet.Members {
		if member.Revocation != nil {
			writes = append(writes, *member.Revocation)
		}
		writes = append(writes, member.Binding)
	}
	return writes
}

func finalFleetRenewalVersions(fleet FinalFleetRenewalFleetEvidence) []FinalFleetGenerationVersionEvidence {
	versions := []FinalFleetGenerationVersionEvidence{}
	if fleet.Previous != nil {
		versions = append(versions, *fleet.Previous)
	}
	return append(versions, fleet.Version)
}

func finalFleetRenewalMemberEqual(left, right FinalFleetGenerationMemberEvidence) bool {
	left.Member, right.Member = 0, 0
	return left == right
}

func verifyFinalFleetRenewalRounds(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
	latest := map[uint64]FinalFleetGenerationVersionEvidence{}
	for _, fleet := range lineage.SetupFleets {
		latest[fleet.FleetID] = fleet.Refresh
	}
	for _, fleet := range lineage.ChallengerFleets {
		latest[fleet.FleetID] = fleet.Initial
	}
	seenWrites := map[string]bool{}
	for index, renewal := range lineage.Renewals {
		if renewal.Round != uint64(index+1) || renewal.ValidFromEpoch == 0 || renewal.ValidToEpoch < renewal.ValidFromEpoch || len(renewal.Fleets) != int(finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount) {
			return errors.New("approved fleet renewal has incomplete round, window, or fleet coordinates")
		}
		for _, hash := range []string{renewal.SourcePlanHash, renewal.ApprovedPlanHash, renewal.MetadataHash} {
			if err := requireFinalHex32("fleet renewal approved lineage hash", hash); err != nil {
				return err
			}
		}
		if err := verifyFinalArtifact("fleet renewal exact approval", renewal.Approval, "fleet-renewal-approval"); err != nil {
			return err
		}
		for position, fleet := range renewal.Fleets {
			if fleet.FleetID != uint64(position+1) || len(fleet.Members) != int(finalFleetGenerationMembersPerFleet) {
				return errors.New("fleet renewal partition is missing, reordered, or duplicated")
			}
			before := latest[fleet.FleetID]
			if fleet.Previous != nil {
				prefix := finalFleetRenewalPreviousPrefix(fleet.FleetID, fleet.Previous.Generation)
				if prefix == "" || before.Generation == ^uint64(0) || fleet.Previous.Generation != before.Generation+1 || len(fleet.PreviousWrites) != 1+int(finalFleetGenerationMembersPerFleet) {
					return errors.New("fleet renewal has an unapproved lifecycle predecessor")
				}
				if err := verifyFinalFleetGenerationVersionForAction(evidence, fleet.FleetID, *fleet.Previous, fleet.Previous.Generation, false, prefix+".commitment"); err != nil {
					return err
				}
				if fleet.Previous.NativeHead.Number <= before.NativeHead.Number {
					return errors.New("fleet renewal lifecycle predecessor does not advance native history")
				}
				if err := verifyFinalFleetRenewalVersionWrites(evidence, *fleet.Previous, fleet.PreviousWrites, prefix); err != nil {
					return err
				}
				before = *fleet.Previous
			} else if len(fleet.PreviousWrites) != 0 {
				return errors.New("fleet renewal has orphan predecessor writes")
			}
			version := fleet.Version
			if before.Generation == ^uint64(0) || version.Generation != before.Generation+1 || version.Hotkey != before.Hotkey || version.CommitmentHash == before.CommitmentHash || version.NativeHead.Number <= before.NativeHead.Number || version.CommitmentAction.PlanHash != renewal.ApprovedPlanHash {
				return errors.New("fleet renewal changed identity, skipped a generation, or borrowed a native commitment")
			}
			if err := verifyFinalFleetGenerationVersionForAction(evidence, fleet.FleetID, version, version.Generation, false, fleetRenewalActionID(renewal.Round, int(fleet.FleetID), "commitment", 0)); err != nil {
				return err
			}
			mirror, err := finalFleetGenerationSingleDecodedEvent(evidence, fleet.Mirror)
			if err != nil || mirror.Name != "CommitmentMirrored" || !finalFleetGenerationMirrorMatches(mirror.Evidence, version) || fleet.Mirror.Action.ActionID != fleetRenewalActionID(renewal.Round, int(fleet.FleetID), "mirror", 0) || fleet.Mirror.Action.PlanHash != renewal.ApprovedPlanHash {
				return stateMismatchError(err, "fleet renewal mirror differs from its approved native generation")
			}
			oldMembers := map[string]FinalFleetGenerationMemberEvidence{}
			for _, member := range before.Members {
				oldMembers[member.ClientID] = member
			}
			for memberIndex, member := range fleet.Members {
				after := version.Members[memberIndex]
				prior, found := oldMembers[member.Prior.ClientID]
				if !found || member.Member != uint64(memberIndex+1) || !finalFleetRenewalMemberEqual(prior, member.Prior) || after.ClientID != prior.ClientID || after.ClientKey != prior.ClientKey || after.FleetKey != prior.FleetKey || after.UID != prior.UID || after.ValidFromEpoch != renewal.ValidFromEpoch || after.ValidToEpoch != renewal.ValidToEpoch {
					return errors.New("fleet renewal member does not preserve its exact predecessor identity")
				}
				delete(oldMembers, prior.ClientID)
				bound, err := finalFleetGenerationSingleDecodedEvent(evidence, member.Binding)
				if err != nil || bound.Name != "FleetBound" || !finalFleetGenerationBoundMatches(bound.Evidence, after) || member.Binding.Action.ActionID != fleetRenewalActionID(renewal.Round, int(fleet.FleetID), "bind", memberIndex+1) || member.Binding.Action.PlanHash != renewal.ApprovedPlanHash {
					return stateMismatchError(err, "fleet renewal binding differs from approved member")
				}
				if prior.ValidToEpoch >= renewal.ValidFromEpoch {
					if member.Revocation == nil {
						return errors.New("fleet renewal omitted its active predecessor revocation")
					}
					revoked, err := finalFleetGenerationSingleDecodedEvent(evidence, *member.Revocation)
					if err != nil || revoked.Name != "FleetBindingRevoked" || revoked.Evidence.ClientID != prior.ClientID || revoked.Evidence.Generation != prior.Generation || revoked.Evidence.ValidFromEpoch != renewal.ValidFromEpoch || member.Revocation.Action.ActionID != fleetRenewalActionID(renewal.Round, int(fleet.FleetID), "revoke", memberIndex+1) || member.Revocation.Action.PlanHash != renewal.ApprovedPlanHash {
						return stateMismatchError(err, "fleet renewal revocation changed its original consent")
					}
				} else if member.Revocation != nil {
					return errors.New("fleet renewal revoked an already expired predecessor")
				}
			}
			for _, write := range finalFleetRenewalWrites(fleet) {
				if seenWrites[write.Receipt.TransactionHash] {
					return errors.New("fleet renewal reused another generation's write")
				}
				seenWrites[write.Receipt.TransactionHash] = true
				if err := verifyFinalFleetGenerationWrite(evidence, write); err != nil {
					return err
				}
			}
			latest[fleet.FleetID] = version
		}
	}
	return nil
}

func verifyFinalFleetRenewalVersionWrites(evidence *FinalSemanticEvidence, version FinalFleetGenerationVersionEvidence, writes []FinalFleetGenerationWriteEvidence, prefix string) error {
	if len(writes) != 1+len(version.Members) {
		return errors.New("fleet renewal predecessor write set is incomplete")
	}
	mirror, err := finalFleetGenerationSingleDecodedEvent(evidence, writes[0])
	if err != nil || mirror.Name != "CommitmentMirrored" || !finalFleetGenerationMirrorMatches(mirror.Evidence, version) || writes[0].Action.ActionID != prefix+".mirror" {
		return stateMismatchError(err, "fleet renewal lifecycle predecessor mirror differs")
	}
	for index, member := range version.Members {
		bound, err := finalFleetGenerationSingleDecodedEvent(evidence, writes[index+1])
		if err != nil || bound.Name != "FleetBound" || !finalFleetGenerationBoundMatches(bound.Evidence, member) || writes[index+1].Action.ActionID != fmt.Sprintf("%s.bind.%d", prefix, index+1) {
			return stateMismatchError(err, "fleet renewal lifecycle predecessor binding differs")
		}
	}
	return nil
}
