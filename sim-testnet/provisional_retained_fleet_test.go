//go:build linux

// Retained startup authenticates an applied fleet successor and the subsequent
// journal without mistaking a process restart for another fleet submission.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Use a real generated fleet plan, its exact archived source, and a verified
// topology receipt. A later ordinary receipt reproduces preparation after the
// renewal checkpoint; every unverified renewal action deliberately stays pending.
func retainedFleetStartupFixture(t *testing.T) (*Executor, []byte) {
	t.Helper()
	fixture, _, _, _ := provisionalFleetRenewalFixture(t)
	self := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: fixture.base, roles: fixture.roles, journal: openCampaignTestJournal(t, fixture.stateDir)}
	if _, err := archiveReviewedSetupPlan(self.stateDir, fixture.base); err != nil {
		t.Fatal(err)
	}
	persistProvisionalAdoptionTopologyTest(t, self)
	entries := self.journal.Entries()
	fixture.renewal.JournalHash = entries[len(entries)-1].EntryHash
	plan, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	self.plan, err = bindFleetRenewalRuntimeIdentity(fixture.cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archiveReviewedSetupPlan(self.stateDir, self.plan); err != nil {
		t.Fatal(err)
	}
	raw, err := readSetupPlanBytes(self.stateDir, "plans/"+stringsTrim0x(self.plan.PlanHash)+".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareProvisionalResume(t.Context(), self.cfg, self.stateDir, "scenario", cliOptions{Apply: true, ProvisionalResume: true, PlanHash: self.plan.PlanHash, Name: "release-1.0"}, self.plan); err != nil {
		t.Fatal(err)
	}
	persistProvisionalAdoptionTopologyTest(t, self)
	return self, raw
}

// Execute the real stopped-scenario orchestration. An action-bearing fleet
// successor must prepare and start processes without re-running pending setup.
func TestRetainedFleetStartupContinuesAfterPreparation(t *testing.T) {
	self, active := retainedFleetStartupFixture(t)
	renewal := self.plan.FleetRenewals[len(self.plan.FleetRenewals)-1]
	source, err := readValidatorEvidenceHistoricalPlan(self.stateDir, renewal.SourcePlanHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFleetRenewalSource(self.cfg, source, self.plan, self.journal.Entries()); err == nil {
		t.Fatal("fixture does not reproduce legitimate preparation after the renewal checkpoint")
	}
	if admitted, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), active); err != nil || admitted {
		t.Fatal("fleet successor unexpectedly qualified as a local-only transform", admitted, err)
	}
	self, stopped, _, _, _ := stoppedScenarioTopologyFixture(t, self)
	if admitted, err := self.authenticateProvisionalRetainedPlan(t.Context(), active); err != nil || !admitted {
		t.Fatal("preflight failed to select retained reader, binary and readiness preparation", admitted, err)
	}
	before := self.journal.Entries()
	var order []string
	err = runScenarioAfterRetainedStartup(t.Context(), self, stopped, nil,
		func(context.Context, *Executor, *provisionalStoppedTopology, map[string]string) error {
			order = append(order, "start")
			return nil
		}, func() error {
			order = append(order, "scenario")
			return nil
		})
	if err != nil || !reflect.DeepEqual(order, []string{"start", "scenario"}) || !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("fleet continuation replayed setup, lost evidence, or failed to enter the scenario", order, err)
	}
	if after, err := readSetupPlanBytes(self.stateDir, "plan.json"); err != nil || !bytes.Equal(active, after) {
		t.Fatal("process-only restart rewrote approved actions", err)
	}
}

// An exact approval and checkpoint do not authorize changed predecessor bytes,
// corrupted later receipts, a missing archive, or an unrelated action delta.
func TestRetainedFleetStartupRejectsChangedEvidence(t *testing.T) {
	self, active := retainedFleetStartupFixture(t)
	renewal := self.plan.FleetRenewals[len(self.plan.FleetRenewals)-1]
	entries := self.journal.Entries()
	for _, relative := range []string{
		"plans/" + stringsTrim0x(renewal.SourcePlanHash) + ".json",
		"plans/" + stringsTrim0x(self.plan.PlanHash) + ".json",
		entries[len(entries)-1].PostconditionPath,
	} {
		path := filepath.Join(self.stateDir, relative)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if admitted, err := self.authenticateProvisionalRetainedPlan(t.Context(), active); admitted || err == nil {
			t.Fatal("changed retained evidence was admitted", relative, admitted, err)
		}
		if err := atomicWrite(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Rehashing and archiving an unrelated action change still cannot satisfy
	// the existing exact fleet append validator.
	changed := *self.plan
	changed.Actions = append([]Action(nil), changed.Actions...)
	changed.Actions[0].Description += " unrelated synthetic action change"
	var err error
	changed.Actions[0].IntentHash, err = actionIntentHash(changed.Actions[0])
	if err != nil {
		t.Fatal(err)
	}
	changed.PlanHash, err = changed.hash()
	if err != nil {
		t.Fatal(err)
	}
	self.plan = &changed
	if _, err := archiveReviewedSetupPlan(self.stateDir, self.plan); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(self.plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "plan.json"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareProvisionalResume(t.Context(), self.cfg, self.stateDir, "scenario", cliOptions{Apply: true, ProvisionalResume: true, PlanHash: self.plan.PlanHash, Name: "release-1.0"}, self.plan); err != nil {
		t.Fatal(err)
	}
	if admitted, err := self.authenticateProvisionalRetainedPlan(t.Context(), wire); admitted || err == nil {
		t.Fatal("arbitrary action change was treated as a retained fleet successor", admitted, err)
	}
}

// Immutable provenance and cancellation constrain the fleet path just as they
// constrain local approvals. Neither can fall back to ordinary setup dispatch.
func TestRetainedFleetStartupKeepsInvocationScope(t *testing.T) {
	self, active := retainedFleetStartupFixture(t)
	original := *self.cfg.provisionalResume.Record
	for _, fault := range []string{"setup", "observation", "acceptance", "driver", "plan"} {
		record := original
		switch fault {
		case "setup":
			record.Command, record.Scenario = "setup", ""
		case "observation":
			record.ReadOnly = true
		case "acceptance":
			record.FinalAcceptance = true
		case "driver":
			record.Driver.ExecutableSHA256 = "changed-synthetic-driver"
		case "plan":
			record.PlanHash = "changed-synthetic-plan"
		}
		self.cfg.provisionalResume.Record = &record
		if admitted, err := self.authenticateProvisionalRetainedPlan(t.Context(), active); admitted || err == nil {
			t.Fatal("changed invocation reached retained startup", fault, admitted, err)
		}
	}
	self.cfg.provisionalResume.Record = &original
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if admitted, err := self.authenticateProvisionalRetainedPlan(ctx, active); admitted || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled retained startup continued", admitted, err)
	}
}
