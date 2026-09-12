package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/urfoundation/sn/protocol"
)

// A stopped campaign can begin from one exact approved renewal. The original
// generation-three takeover remains in its source plan and public archive.
type FleetLifecycleRenewal struct {
	Schema                      string `json:"schema"`
	Round                       uint64 `json:"round"`
	SourcePlanHash              string `json:"source_plan_hash"`
	ValidFromEpoch              uint64 `json:"valid_from_epoch"`
	ValidToEpoch                uint64 `json:"valid_to_epoch"`
	TargetTakeoverGeneration    uint64 `json:"target_takeover_generation"`
	CompanionTakeoverGeneration uint64 `json:"companion_takeover_generation"`
	ProviderGeneration          uint64 `json:"provider_generation"`
	TerminalGeneration          uint64 `json:"terminal_generation"`
}

func cloneFleetLifecycleRenewal(source *FleetLifecycleRenewal) *FleetLifecycleRenewal {
	if source == nil {
		return nil
	}
	cloned := *source
	return &cloned
}

func fleetLifecycleRenewalReference(plan *SetupPlan, renewal *FleetRenewal) (*FleetLifecycleRenewal, error) {
	if plan == nil || renewal == nil || renewal.Round == 0 || renewal.ValidFromEpoch == 0 || renewal.ValidToEpoch < renewal.ValidFromEpoch || len(renewal.Fleets) < fleetLifecycleCompanionFleet {
		return nil, errors.New("lifecycle renewal source is incomplete")
	}
	reference := &FleetLifecycleRenewal{Schema: "urnetwork-fleet-lifecycle-renewal-v1", Round: renewal.Round, SourcePlanHash: renewal.SourcePlanHash, ValidFromEpoch: renewal.ValidFromEpoch, ValidToEpoch: renewal.ValidToEpoch}
	for _, item := range []struct {
		fleet, churn int
		uid          uint16
	}{{fleetLifecycleTargetFleet, fleetLifecycleTargetChurn, fleetLifecycleTargetExpectedUID}, {fleetLifecycleCompanionFleet, fleetLifecycleCompanionChurn, fleetLifecycleCompanionExpectedUID}} {
		fleet := &renewal.Fleets[item.fleet-1]
		manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
		if err != nil {
			return nil, err
		}
		if fleet.Fleet != item.fleet || fleet.HotkeyRole != churnHotkeyLabel(item.churn) || fleet.UID != item.uid || manifest.Generation <= fleetLifecycleTakeoverGeneration || manifest.Generation == ^uint64(0) || len(fleet.Members) != len(manifest.Members) || len(fleet.Members) == 0 {
			return nil, errors.New("lifecycle renewal changes the retained takeover identity or skips its generation")
		}
		for index, member := range fleet.Members {
			binding, err := fleetRenewalBinding(*manifest, manifest.Members[index], member.Binding)
			if err != nil || binding.ValidFromEpoch != renewal.ValidFromEpoch || binding.ValidToEpoch != renewal.ValidToEpoch || member.Prior.Generation+1 != binding.Generation || member.Prior.Hotkey != member.Binding.Hotkey || member.Prior.UID != item.uid || member.Binding.UID != item.uid {
				return nil, stateMismatchError(err, "lifecycle renewal member differs from its exact prior takeover")
			}
		}
		if item.fleet == fleetLifecycleTargetFleet {
			reference.TargetTakeoverGeneration, reference.ProviderGeneration = manifest.Generation, manifest.Generation+1
		} else {
			reference.CompanionTakeoverGeneration, reference.TerminalGeneration = manifest.Generation, manifest.Generation+1
		}
	}
	return reference, nil
}

func validateFleetLifecycleRenewalPlan(plan *SetupPlan) error {
	if plan == nil {
		return errors.New("lifecycle renewal plan is absent")
	}
	if plan.FleetLifecycleRenewal == nil {
		if len(plan.FleetRenewals) != 0 {
			return errors.New("approved renewal has no exact lifecycle successor binding")
		}
		return nil
	}
	reference := plan.FleetLifecycleRenewal
	if reference.Round == 0 || reference.Round != uint64(len(plan.FleetRenewals)) {
		return errors.New("lifecycle renewal must bind the last approved round")
	}
	want, err := fleetLifecycleRenewalReference(plan, &plan.FleetRenewals[reference.Round-1])
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(reference, want) || !plan.allowedPlanHashes()[reference.SourcePlanHash] {
		return errors.New("lifecycle renewal differs from its exact approved round")
	}
	commitments := 0
	for _, action := range plan.Actions {
		if !fleetLifecycleRenewalFutureAction(action.ID) {
			continue
		}
		if action.Parameters["lifecycle_renewal_round"] != strconv.FormatUint(reference.Round, 10) || action.Parameters["lifecycle_renewal_source_plan_hash"] != reference.SourcePlanHash {
			return errors.New("future lifecycle action is not bound to its renewal source")
		}
		if action.ID == "lifecycle.provider.commitment" && action.Parameters["generation"] != strconv.FormatUint(reference.ProviderGeneration, 10) || action.ID == "lifecycle.terminal.commitment" && action.Parameters["generation"] != strconv.FormatUint(reference.TerminalGeneration, 10) {
			return errors.New("future lifecycle commitment is not the exact successor generation")
		}
		if action.ID == "lifecycle.provider.commitment" || action.ID == "lifecycle.terminal.commitment" {
			commitments++
		}
	}
	if commitments != 2 {
		return errors.New("renewed lifecycle is missing its two exact successor commitments")
	}
	return nil
}

func fleetLifecycleRenewalFutureAction(id string) bool {
	return strings.HasPrefix(id, "lifecycle.provider.") || strings.HasPrefix(id, "lifecycle.terminal.")
}

func rebindFleetLifecycleRenewalPlan(plan *SetupPlan, renewal *FleetRenewal) error {
	reference, err := fleetLifecycleRenewalReference(plan, renewal)
	if err != nil {
		return err
	}
	plan.FleetLifecycleRenewal = reference
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if !fleetLifecycleRenewalFutureAction(action.ID) {
			continue
		}
		action.Parameters = cloneStrings(action.Parameters)
		action.Parameters["lifecycle_renewal_round"] = strconv.FormatUint(reference.Round, 10)
		action.Parameters["lifecycle_renewal_source_plan_hash"] = reference.SourcePlanHash
		if action.ID == "lifecycle.provider.commitment" {
			action.Parameters["generation"] = strconv.FormatUint(reference.ProviderGeneration, 10)
		}
		if action.ID == "lifecycle.terminal.commitment" {
			action.Parameters["generation"] = strconv.FormatUint(reference.TerminalGeneration, 10)
		}
		action.Description = strings.ReplaceAll(action.Description, "generation-4", "plan-approved successor generation")
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return err
		}
	}
	return nil
}

// A renewal cannot silently rewrite an already started lifecycle wave. This
// check runs during read-only planning and again against the approved source.
func validateFleetLifecycleRenewalPending(stateDir string, plan *SetupPlan, entries []JournalEntry) error {
	if plan == nil {
		return errors.New("lifecycle renewal source plan is absent")
	}
	if stateDir != "" {
		if _, err := os.Lstat(filepath.Join(stateDir, "public", "fleet-lifecycle.json")); err == nil {
			return errors.New("fleet renewal cannot replace an already recorded lifecycle run")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, entry := range entries {
		if plan.allowedPlanHashes()[entry.PlanHash] && (fleetLifecycleRenewalFutureAction(entry.ActionID) || strings.HasPrefix(entry.ActionID, "lifecycle.fallback.")) {
			return errors.New("fleet renewal cannot rebind a lifecycle action already in the journal")
		}
	}
	return nil
}

func fleetLifecycleVariantForPlan(plan *SetupPlan, name string) (fleetLifecycleVariant, error) {
	variant, err := fleetLifecycleVariantFor(name)
	if err != nil || plan == nil {
		return variant, err
	}
	if err := validateFleetLifecycleRenewalPlan(plan); err != nil {
		return fleetLifecycleVariant{}, err
	}
	if plan.FleetLifecycleRenewal == nil {
		return variant, nil
	}
	reference := plan.FleetLifecycleRenewal
	switch name {
	case fleetLifecycleVariantTargetTakeover, fleetLifecycleVariantCompanionTakeover:
		variant.Generation = reference.TargetTakeoverGeneration
		if name == fleetLifecycleVariantCompanionTakeover {
			variant.Generation = reference.CompanionTakeoverGeneration
		}
		stem := fleetRenewalStem(reference.Round, variant.Fleet)
		variant.ManifestName, variant.CommitmentName = stem+".json", stem+".commitment.json"
		variant.BindingName = func(member int) string { return fmt.Sprintf("%s-member-%d.binding.json", stem, member) }
	case fleetLifecycleVariantProvider, fleetLifecycleVariantTerminal:
		variant.Generation = reference.ProviderGeneration
		if name == fleetLifecycleVariantTerminal {
			variant.Generation = reference.TerminalGeneration
		}
		variant.ManifestName = fmt.Sprintf("fleet-%d.lifecycle-generation-%d.json", variant.Fleet, variant.Generation)
		variant.CommitmentName = fmt.Sprintf("fleet-%d.lifecycle-generation-%d.commitment.json", variant.Fleet, variant.Generation)
		variant.BindingName = func(member int) string {
			return fmt.Sprintf("fleet-%d-member-%d.lifecycle-generation-%d.binding.json", variant.Fleet, member, variant.Generation)
		}
	}
	return variant, nil
}

func fleetLifecycleRenewedTakeover(plan *SetupPlan, name string) bool {
	return plan != nil && plan.FleetLifecycleRenewal != nil && (name == fleetLifecycleVariantTargetTakeover || name == fleetLifecycleVariantCompanionTakeover)
}

func validateFleetLifecycleTakeoverWindow(plan *SetupPlan, evidence *FleetLifecycleEvidence, first, count uint64) error {
	if evidence == nil || first == 0 || count == 0 {
		return errors.New("lifecycle takeover window is absent")
	}
	if plan == nil || plan.FleetLifecycleRenewal == nil {
		if evidence.Renewal != nil || evidence.TakeoverEffectiveEpoch != first {
			return errors.New("fleet lifecycle takeover binding is not effective at the first release settlement epoch")
		}
		return nil
	}
	if err := validateFleetLifecycleRenewalPlan(plan); err != nil {
		return err
	}
	reference := plan.FleetLifecycleRenewal
	if !reflect.DeepEqual(evidence.Renewal, reference) || evidence.TakeoverEffectiveEpoch != reference.ValidFromEpoch || first < reference.ValidFromEpoch || count-1 > ^uint64(0)-first || first+count-1 > reference.ValidToEpoch {
		return errors.New("fleet lifecycle acceptance window is outside its exact approved renewal")
	}
	return nil
}

func (e *Executor) verifyFleetLifecycleRenewedBinding(ctx context.Context, name string, member int, head ChainHead) (*FleetBindingEvidence, error) {
	variant, err := fleetLifecycleVariantForPlan(e.plan, name)
	if err != nil {
		return nil, err
	}
	if !fleetLifecycleRenewedTakeover(e.plan, name) {
		return nil, errors.New("lifecycle variant is not an approved renewed takeover")
	}
	renewal := &e.plan.FleetRenewals[e.plan.FleetLifecycleRenewal.Round-1]
	fleet := &renewal.Fleets[variant.Fleet-1]
	if member < 1 || member > len(fleet.Members) || head.Number == 0 {
		return nil, errors.New("renewed lifecycle member or finalized head is outside its exact census")
	}
	action, err := e.planAction(fleetRenewalActionID(renewal.Round, fleet.Fleet, "bind", member))
	if err != nil {
		return nil, err
	}
	if err := e.verifyFleetLifecycleRenewalAction(ctx, action, head); err != nil {
		return nil, err
	}
	evidence, err := loadFleetLifecycleBindingEvidenceForPlan(e.plan, e.stateDir, name, member)
	if err != nil {
		return nil, err
	}
	transaction, err := e.fleetRenewalFinalized(action)
	if err != nil {
		return nil, err
	}
	approved := fleet.Members[member-1].Binding
	approved.DeploymentID = e.plan.DeploymentID
	approved.PlanHash, approved.ActionID, approved.IntentHash = transaction.PlanHash, action.ID, action.IntentHash
	approved.TransactionHash, approved.BlockNumber, approved.BlockHash = transaction.TransactionHash, transaction.BlockNumber, transaction.BlockHash
	if !reflect.DeepEqual(*evidence, approved) {
		return nil, errors.New("renewed lifecycle public binding differs from approved signed member and receipt")
	}
	return evidence, nil
}

func (e *Executor) verifyFleetLifecycleRenewalAction(ctx context.Context, action Action, head ChainHead) error {
	verified, ok := e.verifiedActionEntry(action)
	if !ok {
		return errors.New("renewed lifecycle predecessor is not postcondition-verified")
	}
	if _, err := e.readPersistedPostcondition(verified); err != nil {
		return err
	}
	if _, err := e.verifyFleetRenewalPostState(ctx, action, head, map[string]any{}); err != nil {
		return err
	}
	if independentRPCRequired(e.cfg) {
		if e.independentSubstrate == nil || e.independentEVM == nil {
			return errors.New("renewed lifecycle independent proof readers are absent")
		}
		nativeHash, nativeNumber, err := e.substrate.finalizedHead()
		if err != nil {
			return err
		}
		_, independentHead, err := e.waitIndependentCheckpoints(ctx, ChainHead{Number: nativeNumber, Hash: nativeHash.Hex()}, head)
		if err != nil {
			return err
		}
		if _, err := e.independentReadExecutor().verifyFleetRenewalPostState(ctx, action, independentHead, map[string]any{}); err != nil {
			return err
		}
	}
	return nil
}

func (self *liveFleetLifecycle) validateRenewedTakeoverLineage(ctx context.Context, name string, effective, blockStart, nativeEnd, evmEnd uint64) error {
	variant, err := fleetLifecycleVariantForPlan(self.executor.plan, name)
	if err != nil {
		return err
	}
	renewal := &self.executor.plan.FleetRenewals[self.executor.plan.FleetLifecycleRenewal.Round-1]
	fleet := &renewal.Fleets[variant.Fleet-1]
	if effective != renewal.ValidFromEpoch {
		return errors.New("renewed lifecycle effective epoch changed")
	}
	manifest, _, _, err := loadFleetLifecycleCommitment(self.stateDir, variant.ManifestName, variant.CommitmentName)
	if err != nil {
		return err
	}
	raw, err := manifest.Canonical()
	if err != nil || !bytes.Equal(raw, fleet.Manifest) {
		return stateMismatchError(err, "renewed lifecycle manifest differs from exact approved bytes")
	}
	commitment, err := self.executor.fleetRenewalCommitment(ctx, renewal, fleet)
	if err != nil {
		return err
	}
	if !fleetLifecycleBlockInRange(commitment.FinalizedBlock, blockStart, nativeEnd) {
		return errors.New("renewed lifecycle commitment is outside its native admission range")
	}
	head, err := finalizedEVMHead(ctx, self.executor.keeper.client)
	if err != nil {
		return err
	}
	for _, operation := range []string{"commitment", "mirror"} {
		action, err := self.executor.planAction(fleetRenewalActionID(renewal.Round, fleet.Fleet, operation, 0))
		if err != nil {
			return err
		}
		if err := self.executor.verifyFleetLifecycleRenewalAction(ctx, action, head); err != nil {
			return err
		}
		if operation == "mirror" {
			tx, err := self.executor.fleetRenewalFinalized(action)
			if err != nil {
				return err
			}
			if !fleetLifecycleBlockInRange(tx.BlockNumber, blockStart, evmEnd) {
				return errors.New("renewed lifecycle mirror is outside its EVM admission range")
			}
		}
	}
	for member := 1; member <= len(fleet.Members); member++ {
		binding, err := self.executor.verifyFleetLifecycleRenewedBinding(ctx, name, member, head)
		if err != nil {
			return err
		}
		if binding.ValidFromEpoch != effective || !fleetLifecycleBlockInRange(binding.BlockNumber, blockStart, evmEnd) {
			return errors.New("renewed lifecycle binding is outside its exact admission window")
		}
	}
	return nil
}
