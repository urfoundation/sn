package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestFleetLifecycleRenewalAdmitsOnlyApprovedSuccessor(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	sourceBytes, err := json.Marshal(fixture.base)
	if err != nil { t.Fatal(err) }
	plan, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil { t.Fatal(err) }
	if err := validateFleetLifecycleRenewalPlan(plan); err != nil { t.Fatal(err) }
	for _, test := range []struct { name string; generation uint64; manifest, binding string }{
		{fleetLifecycleVariantTargetTakeover, 4, "fleet-5.renewal-1.json", "fleet-5.renewal-1-member-1.binding.json"},
		{fleetLifecycleVariantCompanionTakeover, 4, "fleet-6.renewal-1.json", "fleet-6.renewal-1-member-1.binding.json"},
		{fleetLifecycleVariantProvider, 5, "fleet-5.lifecycle-generation-5.json", "fleet-5-member-1.lifecycle-generation-5.binding.json"},
		{fleetLifecycleVariantTerminal, 5, "fleet-6.lifecycle-generation-5.json", "fleet-6-member-1.lifecycle-generation-5.binding.json"},
	} {
		variant, err := fleetLifecycleVariantForPlan(plan, test.name)
		if err != nil { t.Fatal(err) }
		if variant.Generation != test.generation || variant.ManifestName != test.manifest || variant.BindingName(1) != test.binding { t.Fatalf("wrong approved lifecycle variant: %+v", variant) }
		prior, err := fleetLifecycleVariantForPlan(fixture.base, test.name)
		if err != nil || prior.Generation != test.generation-1 || prior.ManifestName == variant.ManifestName || prior.BindingName(1) == variant.BindingName(1) { t.Fatalf("source lifecycle identity was not retained: %+v %v", prior, err) }
	}
	for index, original := range fixture.base.Actions {
		current := plan.Actions[index]
		if fleetLifecycleRenewalFutureAction(original.ID) {
			if current.IntentHash == original.IntentHash || current.Spend != original.Spend || !reflect.DeepEqual(current.DependsOn, original.DependsOn) { t.Fatalf("future lifecycle %s changed its budget/dependencies or retained a stale intent", original.ID) }
		} else if original.ID != "campaign.evm-gas-reserve" && !reflect.DeepEqual(current, original) { t.Fatalf("renewal rewrote source action %s", original.ID) }
	}
	after, err := json.Marshal(fixture.base)
	if err != nil || !bytes.Equal(sourceBytes, after) { t.Fatal("renewal mutated source plan bytes") }
	if plan.Limits != fixture.base.Limits || plan.MaximumSpend != fixture.base.MaximumSpend { t.Fatal("lifecycle successor increased the campaign approval") }

	for _, fault := range []string{"missing-reference", "source", "round", "generation", "window", "action-generation", "action-source", "missing-commitment"} {
		t.Run(fault, func(t *testing.T) {
			raw, err := json.Marshal(plan)
			if err != nil { t.Fatal(err) }
			var changed SetupPlan
			if err := json.Unmarshal(raw, &changed); err != nil { t.Fatal(err) }
			switch fault {
			case "missing-reference": changed.FleetLifecycleRenewal = nil
			case "source": changed.FleetLifecycleRenewal.SourcePlanHash = common.Hash{0x99}.Hex()
			case "round": changed.FleetLifecycleRenewal.Round++
			case "generation": changed.FleetLifecycleRenewal.ProviderGeneration++
			case "window": changed.FleetLifecycleRenewal.ValidToEpoch++
			default:
				for index := range changed.Actions {
					action := &changed.Actions[index]
					if action.ID != "lifecycle.provider.commitment" { continue }
					if fault == "action-generation" { action.Parameters["generation"] = "6" }
					if fault == "action-source" { action.Parameters["lifecycle_renewal_source_plan_hash"] = common.Hash{0x99}.Hex() }
					if fault == "missing-commitment" { changed.Actions = append(changed.Actions[:index], changed.Actions[index+1:]...) }
					break
				}
			}
			if _, err := fleetLifecycleVariantForPlan(&changed, fleetLifecycleVariantProvider); err == nil { t.Fatal("unapproved lifecycle successor accepted") }
		})
	}

	evidence := &FleetLifecycleEvidence{TakeoverEffectiveEpoch: 318, Renewal: cloneFleetLifecycleRenewal(plan.FleetLifecycleRenewal)}
	for _, first := range []uint64{318, 320, 345} {
		if err := validateFleetLifecycleTakeoverWindow(plan, evidence, first, 5); err != nil { t.Fatalf("approved remaining window %d: %v", first, err) }
	}
	for _, window := range [][2]uint64{{317, 5}, {346, 5}, {318, 0}, {^uint64(0)-1, 5}} {
		if err := validateFleetLifecycleTakeoverWindow(plan, evidence, window[0], window[1]); err == nil { t.Fatalf("unapproved lifecycle window accepted: %v", window) }
	}
	evidence.TakeoverEffectiveEpoch++
	if err := validateFleetLifecycleTakeoverWindow(plan, evidence, 320, 5); err == nil { t.Fatal("rewritten takeover receipt epoch accepted") }
	evidence.TakeoverEffectiveEpoch = 318
	evidence.Renewal.SourcePlanHash = common.Hash{0x77}.Hex()
	if err := validateFleetLifecycleTakeoverWindow(plan, evidence, 320, 5); err == nil { t.Fatal("cross-source lifecycle evidence accepted") }
	legacy := &FleetLifecycleEvidence{TakeoverEffectiveEpoch: 294}
	if err := validateFleetLifecycleTakeoverWindow(fixture.base, legacy, 294, 5); err != nil { t.Fatal(err) }
	if err := validateFleetLifecycleTakeoverWindow(fixture.base, legacy, 318, 5); err == nil { t.Fatal("legacy plan acquired an unapproved window extension") }

	if err := validateFleetLifecycleRenewalPending(fixture.stateDir, fixture.base, nil); err != nil { t.Fatal(err) }
	for _, id := range []string{"lifecycle.provider.commitment", "lifecycle.terminal.bind.1", "lifecycle.fallback.register"} {
		if err := validateFleetLifecycleRenewalPending("", fixture.base, []JournalEntry{{PlanHash: fixture.base.PlanHash, ActionID: id, Stage: StageIntent}}); err == nil { t.Fatalf("already started lifecycle action %s was rebound", id) }
	}
	if err := os.MkdirAll(filepath.Join(fixture.stateDir,"public"), 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(fixture.stateDir,"public","fleet-lifecycle.json"), []byte("retained lifecycle proof"), 0o644); err != nil { t.Fatal(err) }
	if err := validateFleetLifecycleRenewalPending(fixture.stateDir, fixture.base, nil); err == nil { t.Fatal("retained lifecycle proof would be replaced") }
}

func TestFleetLifecycleRenewalDescriptorsKeepLaterWaves(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	plan, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil { t.Fatal(err) }
	if err := writePublicJSON(filepath.Join(fixture.stateDir, "plan.json"), plan); err != nil { t.Fatal(err) }
	journal, err := OpenJournal(fixture.stateDir)
	if err != nil { t.Fatal(err) }
	for _, action := range plan.Actions {
		if !isFleetRenewalAction(action) || action.Parameters["operation"] != "bind" { continue }
		path, err := postconditionRelativePath(plan.PlanHash, action.ID)
		if err != nil { t.Fatal(err) }
		if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionHash: common.Hash{0x12}.Hex(), PostconditionPath: path}); err != nil { t.Fatal(err) }
	}
	if err := journal.Close(); err != nil { t.Fatal(err) }
	// This exercises path selection only; the runtime admission separately
	// authenticates the signed members, canonical transactions and native slots.
	evidence := FleetLifecycleEvidence{Schema: fleetLifecycleEvidenceSchema, DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, RunID: "renewed-release", Stage: fleetLifecycleStageAwaitingDemotion, TakeoverEffectiveEpoch: 318, Renewal: cloneFleetLifecycleRenewal(plan.FleetLifecycleRenewal)}
	for _, test := range []struct { label string; fallback, provider, terminal uint64; names [2]string }{
		{"takeovers", 0, 0, 0, [2]string{fleetLifecycleVariantTargetTakeover, fleetLifecycleVariantCompanionTakeover}},
		{"fallback", 320, 0, 0, [2]string{fleetLifecycleVariantFallback, fleetLifecycleVariantCompanionTakeover}},
		{"provider", 320, 321, 0, [2]string{fleetLifecycleVariantProvider, fleetLifecycleVariantFallback}},
		{"terminal", 320, 321, 322, [2]string{fleetLifecycleVariantProvider, fleetLifecycleVariantTerminal}},
	} {
		t.Run(test.label, func(t *testing.T) {
			evidence.FallbackEffectiveEpoch, evidence.ProviderEffectiveEpoch, evidence.TerminalEffectiveEpoch = test.fallback, test.provider, test.terminal
			if err := writePublicJSON(filepath.Join(fixture.stateDir, "public", "fleet-lifecycle.json"), evidence); err != nil { t.Fatal(err) }
			descriptors, err := fleetLifecycleEvidenceDescriptors(fixture.cfg, fixture.stateDir, 323)
			if err != nil { t.Fatal(err) }
			if len(descriptors) != 202 { t.Fatal("renewal lost original fleet census") }
			for index, name := range test.names {
				want, err := fleetLifecycleVariantDescriptorForPlan(fixture.cfg, plan, name)
				if err != nil { t.Fatal(err) }
				if !reflect.DeepEqual(descriptors[4+index], want) { t.Fatalf("later %s was overwritten by renewal: %+v", name, descriptors[4+index]) }
			}
			for index, descriptor := range descriptors {
				if index == 4 || index == 5 { continue }
				if !strings.HasPrefix(descriptor.ManifestName, fmt.Sprintf("fleet-%d.renewal-1.", index+1)) || len(descriptor.BindingNames) != 4 { t.Fatalf("renewal dropped original fleet %d", index+1) }
			}
		})
	}
	evidence.Renewal.ProviderGeneration++
	if err := writePublicJSON(filepath.Join(fixture.stateDir, "public", "fleet-lifecycle.json"), evidence); err != nil { t.Fatal(err) }
	if _, err := fleetLifecycleEvidenceDescriptors(fixture.cfg, fixture.stateDir, 323); err == nil { t.Fatal("unapproved later lifecycle evidence changed descriptor selection") }
}
