package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func provisionalSetupRepairFixture(t *testing.T) (carriedPreparationTest, []Action) {
	t.Helper()
	fixture := provisionalPreparationCacheTest(t, 2)
	self := fixture.executor
	repairs := []Action{
		{ID: "alpha.repair.validator.1.8", Kind: "substrate-extrinsic", Target: "validator-1", Spend: Spend{AlphaRao: 100}, DependsOn: []string{self.plan.Actions[1].ID}},
		{ID: "alpha.repair.validator.1.9", Kind: "substrate-extrinsic", Target: "validator-1", Spend: Spend{AlphaRao: 100}, DependsOn: []string{"alpha.repair.validator.1.8"}},
	}
	for index := range repairs {
		var err error
		repairs[index].IntentHash, err = actionIntentHash(repairs[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	self.plan.Actions = append(self.plan.Actions, repairs...)
	self.plan.Actions = append(self.plan.Actions, Action{ID: "topology.launch", Kind: "local"}, Action{ID: "churn.tournament-complete", Kind: "local"})
	var err error
	self.plan.PlanHash, err = self.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.provisionalResume.Record.PlanHash = self.plan.PlanHash
	return fixture, repairs
}

// The execution boundary retains the real journal and content-addressed
// postcondition formats while replacing only chain observation/submission.
func persistProvisionalRepairTestReceipt(t *testing.T, fixture carriedPreparationTest, action Action) {
	t.Helper()
	self := fixture.executor
	record := *fixture.records[0]
	record.PlanHash, record.ActionID, record.IntentHash = self.plan.PlanHash, action.ID, action.IntentHash
	record.OperationalRPCMode = self.cfg.OperationalRPCMode
	path, hash, err := self.persistActionPostcondition(&record)
	if err != nil {
		t.Fatal(err)
	}
	if err := self.journal.Append(JournalEntry{DeploymentID: self.plan.DeploymentID, PlanHash: self.plan.PlanHash,
		ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}); err != nil {
		t.Fatal(err)
	}
}

// A failure after one repair and another broadcast preserves both checkpoints.
// Resume passes the exact remaining intent back to transaction recovery; a
// third invocation dispatches nothing and never changes supervisor files.
func TestProvisionalSetupRepairResumesOriginalIntentWithoutTopologyRestart(t *testing.T) {
	fixture, repairs := provisionalSetupRepairFixture(t)
	self := fixture.executor
	for _, name := range []string{"supervisor.json", "supervisor.state.json", "run-inputs.json"} {
		if err := os.WriteFile(filepath.Join(self.stateDir, name), []byte("retained-live-generation"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if need, err := provisionalLiveResumeNeedsDoctor(self); err != nil || !need {
		t.Fatalf("pending repair escaped doctor: %t %v", need, err)
	}
	interrupted := errors.New("synthetic interruption after broadcast")
	calls, broadcasts := map[string]int{}, map[string]int{}
	execute := func(_ context.Context, action Action) error {
		calls[action.ID]++
		matched := false
		for _, approved := range repairs {
			matched = matched || reflect.DeepEqual(approved, action)
		}
		if !matched {
			t.Fatal("repair changed its approved action")
		}
		if prior, found := self.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash); found {
			if action.ID != repairs[1].ID || prior.TransactionHash != "0x"+strings.Repeat("ab", 32) {
				t.Fatal("resume lost the exact earlier broadcast", prior)
			}
		} else {
			broadcasts[action.ID]++
			if err := self.journal.Append(JournalEntry{DeploymentID: self.plan.DeploymentID, PlanHash: self.plan.PlanHash,
				ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, TransactionHash: "0x" + strings.Repeat("ab", 32),
				Signer: "synthetic-signer", Nonce: "7", RecoveryBlock: 10, RecoveryBlockHash: "0x" + strings.Repeat("cd", 32)}); err != nil {
				t.Fatal(err)
			}
		}
		if action.ID == repairs[1].ID && calls[action.ID] == 1 {
			return interrupted
		}
		persistProvisionalRepairTestReceipt(t, fixture, action)
		return nil
	}
	if _, err := self.reconcileProvisionalSetupPrefix(t.Context(), self.plan, execute); !errors.Is(err, interrupted) {
		t.Fatal("repair interruption was hidden", err)
	}
	for range 2 {
		topology, err := self.reconcileProvisionalSetupPrefix(t.Context(), self.plan, execute)
		if err != nil || topology == nil || topology.ID != "topology.launch" {
			t.Fatalf("pending repair could not resume beside live topology: %+v %v", topology, err)
		}
	}
	if calls[repairs[0].ID] != 1 || calls[repairs[1].ID] != 2 || broadcasts[repairs[0].ID] != 1 || broadcasts[repairs[1].ID] != 1 {
		t.Fatalf("recovery replayed completed work: calls=%v broadcasts=%v", calls, broadcasts)
	}
	if !reflect.DeepEqual(self.journal.Entries()[:len(fixture.entries)], fixture.entries) {
		t.Fatal("repair recovery rewrote previous checkpoints")
	}
	for _, name := range []string{"supervisor.json", "supervisor.state.json", "run-inputs.json"} {
		wire, err := os.ReadFile(filepath.Join(self.stateDir, name))
		if err != nil || string(wire) != "retained-live-generation" {
			t.Fatalf("repair changed %s: %v", name, err)
		}
	}
}

func TestProvisionalSetupRepairAuthenticatesWholePrefixBeforeDispatch(t *testing.T) {
	for _, fault := range []string{"unverified-runtime-input", "corrupt-receipt", "changed-intent", "strict-mode", "missing-topology", "canceled"} {
		fixture, _ := provisionalSetupRepairFixture(t)
		self := fixture.executor
		ctx, cancel := context.WithCancel(t.Context())
		switch fault {
		case "unverified-runtime-input":
			self.plan.Actions[3] = Action{ID: "config.render", Kind: "local"}
		case "corrupt-receipt":
			if err := os.WriteFile(filepath.Join(self.stateDir, fixture.entries[1].PostconditionPath), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		case "changed-intent":
			self.plan.Actions[2].IntentHash = "different-approval"
		case "strict-mode":
			self.cfg.provisionalResume = nil
		case "missing-topology":
			self.plan.Actions = self.plan.Actions[:len(self.plan.Actions)-2]
		case "canceled":
			cancel()
		}
		before := self.journal.Entries()
		calls := 0
		_, err := self.reconcileProvisionalSetupPrefix(ctx, self.plan, func(context.Context, Action) error { calls++; return nil })
		cancel()
		if err == nil || calls != 0 || !reflect.DeepEqual(before, self.journal.Entries()) {
			t.Fatalf("%s dispatched before authenticating the complete prefix: calls=%d error=%v", fault, calls, err)
		}
	}
}

func TestProvisionalSetupRepairRequiresDurableAuthenticatedCompletion(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		fixture, _ := provisionalSetupRepairFixture(t)
		self := fixture.executor
		calls := 0
		_, err := self.reconcileProvisionalSetupPrefix(t.Context(), self.plan, func(_ context.Context, action Action) error {
			calls++
			if corrupt {
				persistProvisionalRepairTestReceipt(t, fixture, action)
				entry, _ := self.verifiedActionEntry(action)
				return os.WriteFile(filepath.Join(self.stateDir, entry.PostconditionPath), []byte("{}"), 0o600)
			}
			return nil
		})
		if err == nil || calls != 1 {
			t.Fatalf("repair advanced without durable authenticated completion: corrupt=%t calls=%d error=%v", corrupt, calls, err)
		}
	}
}

func TestProvisionalSetupReconciliationKeepsOriginalApprovalAndCheckpoint(t *testing.T) {
	fixture, _ := provisionalSetupRepairFixture(t)
	self := fixture.executor
	action := Action{ID: "alpha.transfer.operator-deposit.1", Kind: "substrate-reconciliation", Target: "retained-deposit"}
	var err error
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	self.plan.Actions = append(self.plan.Actions[:2:2], action, Action{ID: "topology.launch", Kind: "local"})
	self.plan.PlanHash, err = self.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.provisionalResume.Record.PlanHash = self.plan.PlanHash
	calls := 0
	for range 2 {
		_, err := self.reconcileProvisionalSetupPrefix(t.Context(), self.plan, func(_ context.Context, got Action) error {
			calls++
			if !reflect.DeepEqual(action, got) {
				t.Fatal("reconciliation changed its approved action")
			}
			persistProvisionalRepairTestReceipt(t, fixture, got)
			return nil
		})
		if err != nil {
			t.Fatal("retained alpha transfer could not reconcile beside live topology", err)
		}
	}
	if calls != 1 {
		t.Fatal("verified reconciliation was repeated", calls)
	}
}
