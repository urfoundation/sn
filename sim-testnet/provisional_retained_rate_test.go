//go:build linux || darwin

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func retainedRateStartupFixture(t *testing.T) (*Executor, *SetupPlan, []byte) {
	return retainedRateStartupRepairFixture(t, false)
}

// A fully archived source may contain an unused tranche replaced by an exact
// successor repair. Both current transfer and reserve-barrier receipts exist.
func retainedRateStartupRepairFixture(t *testing.T, retireUnused bool) (*Executor, *SetupPlan, []byte) {
	t.Helper()
	self, _ := provisionalAllowanceAdoptionFixture(t)
	source := self.plan
	if retireUnused {
		source = clonePrecompileProbeSuccessorPlan(t, source)
		source.PriorPlanHashes = append(source.PriorPlanHashes, source.PlanHash)
		minimum, err := minimumAlphaTransferRao(source.LiveFacts.DefaultMinTransferRao, source.LiveFacts.AlphaPriceQ9, source.AlphaTransferMarginBPS)
		if err != nil {
			t.Fatal(err)
		}
		committed, ok := checkedAdd(source.MaximumSpend.AlphaRao, source.SupersededSpend.AlphaRao)
		limit, addOk := checkedAdd(committed, minimum)
		if !ok || !addOk || limit > source.Limits.AlphaRao {
			t.Fatal("synthetic source has no reserve tranche capacity")
		}
		base := actionByID(t, source, "alpha.transfer.validator.1")
		parameters := alphaTransferActionParameters(minimum, 0, minimum, &source.LiveFacts, source.AlphaTransferMarginBPS)
		parameters[alphaRepairForActionParameter] = base.ID
		parameters[alphaRepairReserveShareParameter] = "true"
		parameters[alphaRepairCumulativeBeforeParameter] = strconv.FormatUint(committed, 10)
		parameters[alphaRepairCumulativeLimitParameter] = strconv.FormatUint(limit, 10)
		parameters[alphaRepairMaximumTrancheParameter] = strconv.FormatUint(minimum, 10)
		parameters["reserve_target_share_bps"] = base.Parameters["reserve_target_share_bps"]
		parameters["reserve_minimum_share_bps"] = base.Parameters["reserve_minimum_share_bps"]
		repair := Action{ID: "alpha.repair.validator.1.11", Kind: "substrate-extrinsic", Target: "validator:1", Description: "synthetic unused reserve tranche",
			Parameters: parameters,
			Spend:      Spend{AlphaRao: minimum, EVMGasWei: "0"}, DependsOn: []string{base.ID}}
		repair.IntentHash, _ = actionIntentHash(repair)
		index := slices.IndexFunc(source.Actions, func(a Action) bool { return a.ID == "validator.reserve-majority" })
		if index < 0 {
			t.Fatal("fixture has no reserve barrier")
		}
		source.Actions = slices.Insert(source.Actions, index, repair)
		barrier := &source.Actions[index+1]
		barrier.DependsOn = append(barrier.DependsOn, repair.ID)
		barrier.IntentHash, _ = actionIntentHash(*barrier)
		source.MaximumSpend, err = maximumActionSpend(source.Actions)
		if err != nil {
			t.Fatal(err)
		}
		source.PlanHash, err = source.hash()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := archiveReviewedSetupPlan(self.stateDir, source); err != nil {
			t.Fatal(err)
		}
	}
	previous := *self.cfg.Policy
	self.cfg.Policy = rateAmendmentTestPolicy(t, &previous)
	self.cfg.PolicyHash, _ = self.cfg.Policy.HashHex()
	current := clonePrecompileProbeSuccessorPlan(t, source)
	current.PolicyHash = self.cfg.PolicyHash
	current.PolicyRateAmendment = &PolicyRateAmendment{Schema: policyRateAmendmentSchema, PriorPlanHash: source.PlanHash, Previous: previous, Next: *self.cfg.Policy}
	current.PriorPlanHashes = append(current.PriorPlanHashes, source.PlanHash)
	for index := range current.Actions {
		action := &current.Actions[index]
		if retireUnused && action.ID == "alpha.repair.validator.1.11" {
			action.ID, action.Description, action.AcceptedPriorIntentHashes = "alpha.repair.validator.1.12", "synthetic completed successor tranche", nil
			action.IntentHash, _ = actionIntentHash(*action)
		}
		if retireUnused && action.ID == "validator.reserve-majority" {
			for i, dependency := range action.DependsOn {
				if dependency == "alpha.repair.validator.1.11" {
					action.DependsOn[i] = "alpha.repair.validator.1.12"
				}
			}
			action.IntentHash, _ = actionIntentHash(*action)
		}
		if action.ID == "policy.schedule-bootstrap" || action.ID == "policy.await-bootstrap" || action.ID == "config.render" || action.ID == "topology.launch" {
			action.Parameters["policy_hash"] = current.PolicyHash
			action.IntentHash, _ = actionIntentHash(*action)
		}
	}
	current.PlanHash, _ = current.hash()
	self.plan = current
	if _, err := archiveReviewedSetupPlan(self.stateDir, current); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(current)
	if err := atomicWrite(filepath.Join(self.stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareProvisionalResume(t.Context(), self.cfg, self.stateDir, "resume", cliOptions{Apply: true, ProvisionalResume: true, PlanHash: current.PlanHash}, current); err != nil {
		t.Fatal(err)
	}
	completedIds := []string{"policy.schedule-bootstrap", "policy.await-bootstrap", "config.render", "topology.launch"}
	if retireUnused {
		completedIds = append(completedIds, "alpha.repair.validator.1.12", "validator.reserve-majority")
	}
	for _, id := range completedIds {
		action := actionByID(t, current, id)
		head := testEVMHead(20, 0x35)
		record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: current.DeploymentID,
			PlanHash: current.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
			OperationalRPCMode: self.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(self.cfg),
			SubstrateFinalized: head, EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: map[string]any{"synthetic": true},
			IndependentSubstrateFinalized: head, IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: map[string]any{"synthetic": true}}
		path, hash, err := self.persistActionPostcondition(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := self.journal.Append(JournalEntry{DeploymentID: current.DeploymentID, PlanHash: current.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}); err != nil {
			t.Fatal(err)
		}
	}
	return self, source, raw
}

func TestRetainedRateStartupRetiresOnlyNeverStartedReserveRepair(t *testing.T) {
	self, source, raw := retainedRateStartupRepairFixture(t, true)
	before := self.journal.Entries()
	if ok, err := self.authenticateProvisionalRetainedPlan(t.Context(), raw); err != nil || !ok {
		t.Fatalf("unused source reserve tranche prevented authenticated successor startup: %v", err)
	}
	if !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("retirement fabricated a completed transfer")
	}
	retired := actionByID(t, source, "alpha.repair.validator.1.11")
	for _, stage := range []JournalStage{StageIntent, StageFailed, JournalStage("signed"), StageBroadcast, StageFinalized, StageVerified} {
		changed := *self
		changed.journal = &Journal{entries: append(append([]JournalEntry(nil), before...), JournalEntry{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: retired.ID, IntentHash: retired.IntentHash, Stage: stage})}
		if ok, err := changed.authenticateRetainedPolicyRateAmendment(t.Context(), source); err == nil || ok {
			t.Errorf("removed repair with %s evidence was admitted", stage)
		}
	}
	for _, entry := range []JournalEntry{
		{ActionID: retired.ID, IntentHash: "0x" + strings.Repeat("b2", 32), PlanHash: "0x" + strings.Repeat("c3", 32), Stage: StageIntent},
		{ActionID: "synthetic-other-action", IntentHash: retired.IntentHash, Stage: StageIntent},
	} {
		changed := *self
		changed.journal = &Journal{entries: append(append([]JournalEntry(nil), before...), entry)}
		if ok, err := changed.authenticateRetainedPolicyRateAmendment(t.Context(), source); err == nil || ok {
			t.Error("foreign action/approval or accepted-intent reference was ignored")
		}
	}
	for _, missing := range []string{"alpha.repair.validator.1.12", "validator.reserve-majority"} {
		changed := *self
		changed.journal = &Journal{entries: slices.DeleteFunc(append([]JournalEntry(nil), before...), func(entry JournalEntry) bool { return entry.ActionID == missing })}
		if ok, err := changed.authenticateRetainedPolicyRateAmendment(t.Context(), source); err == nil || ok {
			t.Errorf("unused source repair hid incomplete current action %s", missing)
		}
	}
}

func TestRetainedRateStartupAuthenticatesCompletedAmendmentOnly(t *testing.T) {
	self, source, raw := retainedRateStartupFixture(t)
	before := self.journal.Entries()
	if ok, err := self.authenticateProvisionalRetainedPlan(t.Context(), raw); err != nil || !ok {
		t.Fatal("completed rate amendment fell back to setup", ok, err)
	}
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), raw); err != nil || ok {
		t.Fatal("rate amendment acquired plan-only setup authority", ok, err)
	}
	if !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("retained rate admission changed the journal")
	}
	for _, mutation := range []string{"policy", "custody", "limit", "action", "pending", "receipt"} {
		t.Run(mutation, func(t *testing.T) {
			changed := *self
			changed.plan = clonePrecompileProbeSuccessorPlan(t, self.plan)
			changed.journal = &Journal{entries: append([]JournalEntry(nil), before...)}
			switch mutation {
			case "policy":
				changed.plan.PolicyRateAmendment.Previous.PolicyID++
			case "custody":
				changed.plan.Roles.Deployer = "changed"
			case "limit":
				changed.plan.Limits.TAORao++
			case "action":
				changed.plan.Actions[0].Description += " unrelated"
				changed.plan.Actions[0].IntentHash, _ = actionIntentHash(changed.plan.Actions[0])
			case "pending":
				changed.journal.entries = before[:len(before)-2]
			case "receipt":
				entry := before[len(before)-2]
				path := filepath.Join(self.stateDir, entry.PostconditionPath)
				original, _ := os.ReadFile(path)
				if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.WriteFile(path, original, 0o600) })
			}
			if ok, err := changed.authenticateRetainedPolicyRateAmendment(t.Context(), source); err == nil || ok {
				t.Fatal("changed completed rate authority accepted", ok, err)
			}
		})
	}
}
