//go:build linux || darwin

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func retainedRateStartupFixture(t *testing.T) (*Executor, *SetupPlan, []byte) {
	t.Helper()
	self, _ := provisionalAllowanceAdoptionFixture(t)
	source := self.plan
	previous := *self.cfg.Policy
	self.cfg.Policy = rateAmendmentTestPolicy(t, &previous)
	self.cfg.PolicyHash, _ = self.cfg.Policy.HashHex()
	current := clonePrecompileProbeSuccessorPlan(t, source)
	current.PolicyHash = self.cfg.PolicyHash
	current.PolicyRateAmendment = &PolicyRateAmendment{Schema: policyRateAmendmentSchema, PriorPlanHash: source.PlanHash, Previous: previous, Next: *self.cfg.Policy}
	current.PriorPlanHashes = append(current.PriorPlanHashes, source.PlanHash)
	for index := range current.Actions {
		action := &current.Actions[index]
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
	for _, id := range []string{"policy.schedule-bootstrap", "policy.await-bootstrap", "config.render", "topology.launch"} {
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
