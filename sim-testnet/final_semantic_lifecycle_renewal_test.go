package main

import (
	"strings"
	"testing"
)

func TestFinalFleetRenewalLifecycleJoinsExactGenerationAndSource(t *testing.T) {
	evidence, lineage := finalFleetRenewalLineageFixture(t)
	round := lineage.Renewals[0]
	evidence.FleetLifecycle = &FinalFleetLifecycleEvidence{State: FleetLifecycleEvidence{Renewal: &FleetLifecycleRenewal{Round: round.Round, SourcePlanHash: round.SourcePlanHash, ValidFromEpoch: round.ValidFromEpoch, ValidToEpoch: round.ValidToEpoch}}}
	for _, item := range []struct {
		name  string
		fleet int
	}{{fleetLifecycleVariantTargetTakeover, 5}, {fleetLifecycleVariantCompanionTakeover, 6}} {
		fleet := round.Fleets[item.fleet-1]
		version := fleet.Version
		variant := FinalFleetLifecycleVariantEvidence{Name: item.name, Generation: version.Generation, Hotkey: version.Hotkey, Commitment: FleetCommitmentEvidence{PlanHash: round.ApprovedPlanHash, ActionID: version.CommitmentAction.ActionID, IntentHash: version.CommitmentAction.IntentHash, CommitmentHash: version.CommitmentHash, ExtrinsicHash: version.CommitmentExtrinsicHash, FinalizedBlock: version.NativeHead.Number, FinalizedBlockHash: version.NativeHead.Hash}, Mirror: FleetLifecycleMirrorEvidence{PlanHash: round.ApprovedPlanHash, ActionID: fleet.Mirror.Action.ActionID, IntentHash: fleet.Mirror.Action.IntentHash, TransactionHash: fleet.Mirror.Receipt.TransactionHash, BlockNumber: fleet.Mirror.Receipt.Block.Number, BlockHash: fleet.Mirror.Receipt.Block.Hash}}
		for index, member := range version.Members {
			write := fleet.Members[index].Binding
			variant.Bindings = append(variant.Bindings, FleetBindingEvidence{ClientID: member.ClientID, ClientKey: member.ClientKey, FleetID: member.FleetKey, Hotkey: member.Hotkey, CommitmentHash: member.CommitmentHash, Generation: member.Generation, ValidFromEpoch: member.ValidFromEpoch, ValidToEpoch: member.ValidToEpoch, UID: member.UID, PlanHash: round.ApprovedPlanHash, ActionID: write.Action.ActionID, IntentHash: write.Action.IntentHash, TransactionHash: write.Receipt.TransactionHash, BlockNumber: write.Receipt.Block.Number, BlockHash: write.Receipt.Block.Hash})
		}
		evidence.FleetLifecycle.Variants = append(evidence.FleetLifecycle.Variants, variant)
	}
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err != nil {
		t.Fatalf("exact renewed lifecycle join: %v", err)
	}
	evidence.FleetLifecycle.Variants[0].Mirror.TransactionHash = finalFleetGenerationTestHash(123)
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err == nil {
		t.Fatal("accepted another lifecycle mirror with a complete unrelated renewal")
	}
	evidence.FleetLifecycle.Variants[0].Mirror.TransactionHash = round.Fleets[4].Mirror.Receipt.TransactionHash
	evidence.FleetLifecycle.State.Renewal.SourcePlanHash = finalFleetGenerationTestHash(456)
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err == nil {
		t.Fatal("accepted another lifecycle renewal approval source")
	}
}

func TestFinalFleetRenewalLifecyclePathsUseApprovedTakeoverAndSuccessor(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	plan, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	entries := []JournalEntry{}
	for _, action := range plan.Actions {
		_, fleet, _, operation, ok := finalFleetRenewalActionCoordinates(action.ID)
		if !ok || fleet != 5 && fleet != 6 || operation != "mirror" && operation != "bind" {
			continue
		}
		entries = append(entries, JournalEntry{PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: finalFleetGenerationTestHash(uint64(len(entries) + 1))})
	}
	paths, err := finalFleetLifecycleExpectedPathsForPlan(plan, 4, entries)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			t.Fatalf("duplicate path %s", path)
		}
		seen[path] = true
	}
	for _, path := range []string{"public/fleet-5.renewal-1.json", "public/fleet-6.renewal-1-member-4.binding.json", "public/fleet-5.lifecycle-generation-5.json", "public/fleet-6-member-4.lifecycle-generation-5.binding.json", "plan-history/" + stringsTrim0x(fixture.base.PlanHash) + ".json"} {
		if !seen[path] {
			t.Fatalf("missing approved lifecycle source %s", path)
		}
	}
	for _, path := range paths {
		if strings.Contains(path, "lifecycle-generation-3") || path == "public/"+fleetLifecycleMirrorEvidenceName(fleetLifecycleVariantTargetTakeover) {
			t.Fatalf("renewed lifecycle substituted an original takeover file %s", path)
		}
	}
	if _, err := finalFleetLifecycleExpectedPathsForPlan(plan, 4, entries[:len(entries)-1]); err == nil {
		t.Fatal("accepted missing renewed lifecycle envelope")
	}
	if _, err := finalFleetLifecycleExpectedPathsForPlan(plan, 4, append(entries, entries[0])); err == nil {
		t.Fatal("accepted duplicated renewed lifecycle transaction")
	}
	legacy, err := finalFleetLifecycleExpectedPaths(4)
	if err != nil {
		t.Fatal(err)
	}
	foundLegacy := false
	for _, path := range legacy {
		if path == "public/fleet-5.lifecycle-generation-3.json" {
			foundLegacy = true
		}
	}
	if !foundLegacy {
		t.Fatal("rewrote the original archived lifecycle namespace")
	}
}
