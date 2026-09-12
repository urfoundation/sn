package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/urfoundation/sn/protocol"
)

func finalFleetLifecycleRenewalApproval(plan *SetupPlan, files map[string][]byte) (*SetupPlan, error) {
	if err := validateFleetLifecycleRenewalPlan(plan); err != nil {
		return nil, err
	}
	if plan.FleetLifecycleRenewal == nil {
		return nil, nil
	}
	renewal := plan.FleetRenewals[plan.FleetLifecycleRenewal.Round-1]
	source, err := decodePersistedPlanBytes(files["plan-history/"+stringsTrim0x(renewal.SourcePlanHash)+".json"])
	if err != nil || source.PlanHash != renewal.SourcePlanHash {
		return nil, stateMismatchError(err, "lifecycle renewal original plan is unavailable")
	}
	approved, err := appendFleetRenewalPlan(source, renewal)
	if err != nil {
		return nil, err
	}
	if !plan.allowedPlanHashes()[approved.PlanHash] {
		return nil, errors.New("lifecycle renewal exact approval is not in current lineage")
	}
	path := "plan-history/" + stringsTrim0x(approved.PlanHash) + ".json"
	if approved.PlanHash == plan.PlanHash {
		path = "launch-foundation/plan.json"
	}
	retained, err := decodePersistedPlanBytes(files[path])
	if err != nil || retained.PlanHash != approved.PlanHash {
		return nil, stateMismatchError(err, "lifecycle renewal exact approved plan bytes are unavailable")
	}
	return retained, nil
}

// A lifecycle takeover borrows only its own completed approved renewal. The
// generation-lineage artifact independently closes all postconditions and
// exact public receipts; the final join below binds the two semantic views.
func finalFleetLifecycleRenewalTransaction(plan *SetupPlan, entries []JournalEntry, action Action) (JournalEntry, error) {
	var found *JournalEntry
	verified := false
	for _, entry := range entries {
		if entry.PlanHash != plan.PlanHash || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash {
			continue
		}
		if entry.Stage == StageVerified && entry.PostconditionHash != "" && entry.PostconditionPath != "" {
			verified = true
		}
		if entry.Stage != StageFinalized {
			continue
		}
		if found != nil || !validCanonicalHashHex(entry.TransactionHash) || !validCanonicalHashHex(entry.BlockHash) || entry.BlockNumber == 0 || entry.RecoveryBlock == 0 || entry.RecoveryBlock >= entry.BlockNumber || !validCanonicalHashHex(entry.RecoveryBlockHash) {
			return JournalEntry{}, errors.New("renewed lifecycle transaction has conflicting or incomplete finalized provenance")
		}
		copy := entry
		found = &copy
	}
	if found == nil || !verified {
		return JournalEntry{}, fmt.Errorf("renewed lifecycle action %s lacks its exact finalized and verified source", action.ID)
	}
	return *found, nil
}

func finalFleetLifecycleRenewedCommitment(plan *SetupPlan, entries []JournalEntry, variant fleetLifecycleVariant, commitment FleetCommitmentEvidence, manifestBytes []byte) (Action, JournalEntry, error) {
	renewal := plan.FleetRenewals[plan.FleetLifecycleRenewal.Round-1]
	fleet := renewal.Fleets[variant.Fleet-1]
	manifest, err := protocol.ParseFleetManifest(manifestBytes)
	if err != nil {
		return Action{}, JournalEntry{}, err
	}
	canonical, err := manifest.Canonical()
	if err != nil || !bytes.Equal(canonical, fleet.Manifest) {
		return Action{}, JournalEntry{}, stateMismatchError(err, "renewed lifecycle manifest differs from signed plan")
	}
	action, err := finalFleetLifecyclePlanAction(plan, fleetRenewalActionID(renewal.Round, variant.Fleet, "commitment", 0))
	if err != nil {
		return Action{}, JournalEntry{}, err
	}
	transaction, err := finalFleetLifecycleRenewalTransaction(plan, entries, action)
	if err != nil {
		return Action{}, JournalEntry{}, err
	}
	if commitment.DeploymentID != plan.DeploymentID || commitment.PlanHash != plan.PlanHash || commitment.ActionID != action.ID || commitment.IntentHash != action.IntentHash || commitment.ExtrinsicHash != transaction.TransactionHash || commitment.FinalizedBlock != transaction.BlockNumber || commitment.FinalizedBlockHash != transaction.BlockHash {
		return Action{}, JournalEntry{}, errors.New("renewed lifecycle commitment differs from its approved source transaction")
	}
	return action, transaction, nil
}

func finalFleetLifecycleRenewedMirror(plan *SetupPlan, entries []JournalEntry, files map[string][]byte, variant fleetLifecycleVariant, manifest protocol.FleetManifest, commitment FleetCommitmentEvidence) (FleetLifecycleMirrorEvidence, Action, error) {
	action, err := finalFleetLifecyclePlanAction(plan, fleetRenewalActionID(plan.FleetLifecycleRenewal.Round, variant.Fleet, "mirror", 0))
	if err != nil {
		return FleetLifecycleMirrorEvidence{}, action, err
	}
	transaction, err := finalFleetLifecycleRenewalTransaction(plan, entries, action)
	if err != nil {
		return FleetLifecycleMirrorEvidence{}, action, err
	}
	data, err := finalFleetRenewalMirrorCalldata(manifest, FinalFleetGenerationVersionEvidence{CommitmentHash: commitment.CommitmentHash, NativeHead: ChainHead{Number: commitment.FinalizedBlock, Hash: commitment.FinalizedBlockHash}})
	if err != nil {
		return FleetLifecycleMirrorEvidence{}, action, err
	}
	if err := verifyFinalFleetRenewalEnvelope(plan, action, transaction.TransactionHash, files["launch-foundation/transactions/"+stringsTrim0x(transaction.TransactionHash)+".rlp"], data); err != nil {
		return FleetLifecycleMirrorEvidence{}, action, err
	}
	return FleetLifecycleMirrorEvidence{Schema: "urnetwork-sim-fleet-commitment-mirror-v1", DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Hotkey: commitment.Hotkey, CommitmentHash: commitment.CommitmentHash, FinalizedBlock: commitment.FinalizedBlock, FinalizedBlockHash: commitment.FinalizedBlockHash, TransactionHash: transaction.TransactionHash, BlockNumber: transaction.BlockNumber, BlockHash: transaction.BlockHash}, action, nil
}

func verifyFinalFleetLifecycleRenewedBinding(plan *SetupPlan, entries []JournalEntry, files map[string][]byte, variant fleetLifecycleVariant, manifest protocol.FleetManifest, member int, binding FleetBindingEvidence) error {
	action, err := finalFleetLifecyclePlanAction(plan, fleetRenewalActionID(plan.FleetLifecycleRenewal.Round, variant.Fleet, "bind", member))
	if err != nil {
		return err
	}
	transaction, err := finalFleetLifecycleRenewalTransaction(plan, entries, action)
	if err != nil {
		return err
	}
	planned := plan.FleetRenewals[plan.FleetLifecycleRenewal.Round-1].Fleets[variant.Fleet-1].Members[member-1].Binding
	planned.DeploymentID, planned.PlanHash, planned.ActionID, planned.IntentHash = plan.DeploymentID, plan.PlanHash, action.ID, action.IntentHash
	planned.TransactionHash, planned.BlockHash, planned.BlockNumber = transaction.TransactionHash, transaction.BlockHash, transaction.BlockNumber
	if !finalJSONEqual(binding, planned) {
		return errors.New("renewed lifecycle binding differs from approved signed member and finalized transaction")
	}
	source := &finalFleetGenerationSource{archive: &finalSemanticArchive{files: files}, raw: map[string][]byte{}}
	_, data, err := source.renewalBinding("public/"+variant.BindingName(member), manifest, member, &planned)
	if err != nil {
		return err
	}
	return verifyFinalFleetRenewalEnvelope(plan, action, transaction.TransactionHash, files["launch-foundation/transactions/"+stringsTrim0x(transaction.TransactionHash)+".rlp"], data)
}

func verifyFinalFleetLifecycleRenewalJoin(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
	if evidence.FleetLifecycle == nil || evidence.FleetLifecycle.State.Renewal == nil {
		return nil
	}
	reference := evidence.FleetLifecycle.State.Renewal
	if reference.Round == 0 || reference.Round > uint64(len(lineage.Renewals)) {
		return errors.New("lifecycle renewed takeover has no full fleet generation proof")
	}
	round := lineage.Renewals[reference.Round-1]
	if round.Round != reference.Round || round.SourcePlanHash != reference.SourcePlanHash || round.ValidFromEpoch != reference.ValidFromEpoch || round.ValidToEpoch != reference.ValidToEpoch {
		return errors.New("lifecycle renewal authority differs from full fleet generation proof")
	}
	for _, item := range []struct {
		name  string
		fleet uint64
	}{{fleetLifecycleVariantTargetTakeover, 5}, {fleetLifecycleVariantCompanionTakeover, 6}} {
		variant, err := finalLifecycleVariantByName(evidence.FleetLifecycle, item.name)
		if err != nil {
			return err
		}
		fleet := round.Fleets[item.fleet-1]
		version := fleet.Version
		if variant.Generation != version.Generation || variant.Hotkey != version.Hotkey || variant.Commitment.CommitmentHash != version.CommitmentHash || variant.Commitment.ExtrinsicHash != version.CommitmentExtrinsicHash || variant.Commitment.FinalizedBlock != version.NativeHead.Number || variant.Commitment.FinalizedBlockHash != version.NativeHead.Hash || variant.Commitment.PlanHash != round.ApprovedPlanHash || variant.Commitment.ActionID != version.CommitmentAction.ActionID || variant.Commitment.IntentHash != version.CommitmentAction.IntentHash {
			return errors.New("lifecycle takeover borrowed another native renewal commitment")
		}
		if variant.Mirror.PlanHash != round.ApprovedPlanHash || variant.Mirror.ActionID != fleet.Mirror.Action.ActionID || variant.Mirror.IntentHash != fleet.Mirror.Action.IntentHash || variant.Mirror.TransactionHash != fleet.Mirror.Receipt.TransactionHash || variant.Mirror.BlockNumber != fleet.Mirror.Receipt.Block.Number || variant.Mirror.BlockHash != fleet.Mirror.Receipt.Block.Hash {
			return errors.New("lifecycle takeover borrowed another renewal mirror")
		}
		if len(variant.Bindings) != len(fleet.Members) {
			return errors.New("lifecycle renewed binding census differs")
		}
		for _, binding := range variant.Bindings {
			found := false
			for index, member := range version.Members {
				if !strings.EqualFold(member.ClientID, binding.ClientID) {
					continue
				}
				write := fleet.Members[index].Binding
				if !finalFleetRenewalMemberEqual(member, finalFleetRenewalMemberProjection(member.Member, binding)) || binding.PlanHash != round.ApprovedPlanHash || binding.ActionID != write.Action.ActionID || binding.IntentHash != write.Action.IntentHash || binding.TransactionHash != write.Receipt.TransactionHash || binding.BlockNumber != write.Receipt.Block.Number || binding.BlockHash != write.Receipt.Block.Hash {
					return errors.New("lifecycle binding differs from exact full renewal proof")
				}
				found = true
			}
			if !found {
				return errors.New("lifecycle member is absent from full renewal proof")
			}
		}
	}
	return nil
}
