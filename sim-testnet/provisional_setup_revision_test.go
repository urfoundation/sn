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

func provisionalSetupActivationFixture(t *testing.T) (carriedPreparationTest, []Action, []byte) {
	t.Helper()
	fixture, repairs := provisionalSetupRepairFixture(t)
	self := fixture.executor
	for index := range self.plan.Actions {
		action := &self.plan.Actions[index]
		var err error
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			t.Fatal(err)
		}
	}
	var err error
	self.plan.MaximumSpend, err = maximumActionSpend(self.plan.Actions)
	if err != nil {
		t.Fatal(err)
	}
	self.plan.PlanHash, err = self.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.provisionalResume.Record.PlanHash = self.plan.PlanHash
	self.cfg.provisionalResume.Record.Command = "setup"
	self.cfg.provisionalResume.RecordPath = filepath.Join(self.stateDir, "provisional-resumes", "synthetic-invocation", "provenance.json")
	source, err := json.MarshalIndent(fixture.source, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	source = append(source, '\n')
	if err := atomicWrite(filepath.Join(self.stateDir, "plan.json"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture, repairs, source
}

// Real persisted approvals, authenticated receipts and journal transitions
// prove activation does not call archive RPCs or repeat completed actions.
func TestProvisionalSetupRevisionActivatesBeforeRepairAndResumesPartialFailure(t *testing.T) {
	fixture, repairs, source := provisionalSetupActivationFixture(t)
	self := fixture.executor
	retained := []string{"supervisor.json", "supervisor.state.json", "runtime-config-manifest.json", "config.redacted.yml", "public/identities.json"}
	for _, name := range retained {
		if err := atomicWrite(filepath.Join(self.stateDir, name), []byte("retained-runtime-bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	interrupted := errors.New("synthetic repair interruption")
	calls := map[string]int{}
	execute := func(_ context.Context, action Action) error {
		calls[action.ID]++
		active, err := readPersistedPlan(self.stateDir)
		if err != nil || active.PlanHash != self.plan.PlanHash {
			t.Fatal("transaction reached dispatch before durable plan activation", err)
		}
		if action.ID == repairs[1].ID && calls[action.ID] == 1 {
			return interrupted
		}
		persistProvisionalRepairTestReceipt(t, fixture, action)
		return nil
	}
	if err := self.activateProvisionalSetupRevision(t.Context(), source, execute); !errors.Is(err, interrupted) {
		t.Fatal("approved activation did not preserve the repair failure", err)
	}
	active, err := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := self.activateProvisionalSetupRevision(t.Context(), active, execute); err != nil {
			t.Fatal("durable repair progress could not resume", err)
		}
	}
	if calls[repairs[0].ID] != 1 || calls[repairs[1].ID] != 2 || len(calls) != 2 {
		t.Fatal("activation replayed completed work or executed topology actions", calls)
	}
	archived, err := os.ReadFile(filepath.Join(self.stateDir, "plans", stringsTrim0x(fixture.source.PlanHash)+".json"))
	if err != nil || !bytes.Equal(archived, source) || !reflect.DeepEqual(self.journal.Entries()[:len(fixture.entries)], fixture.entries) {
		t.Fatal("activation changed original approval or receipt bytes", err)
	}
	for _, name := range retained {
		wire, err := os.ReadFile(filepath.Join(self.stateDir, name))
		if err != nil || string(wire) != "retained-runtime-bytes" {
			t.Fatal("provisional setup rewrote runtime inputs", name, err)
		}
	}
	var record struct {
		Provisional             bool `json:"provisional"`
		FinalAcceptance         bool `json:"final_acceptance"`
		HistoricalAuditDeferred bool `json:"historical_audit_deferred"`
	}
	if err := readJSONFile(filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "setup-activation.json"), &record); err != nil ||
		!record.Provisional || record.FinalAcceptance || !record.HistoricalAuditDeferred {
		t.Fatal("activation lost the explicit pending historical audit", err)
	}
}

func TestProvisionalSetupRevisionRejectsCorruptionAllowanceAndTopologyChangesBeforeActivation(t *testing.T) {
	for _, fault := range []string{"receipt", "source-bytes", "approval-hash", "lineage", "allowance", "pending-contract", "strict", "canceled"} {
		fixture, _, source := provisionalSetupActivationFixture(t)
		self := fixture.executor
		ctx, cancel := context.WithCancel(t.Context())
		switch fault {
		case "receipt":
			if err := atomicWrite(filepath.Join(self.stateDir, fixture.entries[1].PostconditionPath), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		case "source-bytes":
			if err := atomicWrite(filepath.Join(self.stateDir, "plan.json"), append(source, ' '), 0o600); err != nil {
				t.Fatal(err)
			}
		case "approval-hash":
			self.plan.MaximumSpend.AlphaRao++
		case "lineage":
			self.plan.PriorPlanHashes = nil
		case "allowance":
			self.plan.MaximumSpend.AlphaRao = self.plan.Limits.AlphaRao + 1
		case "pending-contract":
			self.plan.Actions[2].ID = "evm.synthetic-new-contract"
			self.plan.Actions[2].Kind = "evm-transaction"
			var err error
			self.plan.Actions[2].IntentHash, err = actionIntentHash(self.plan.Actions[2])
			if err != nil {
				t.Fatal(err)
			}
		case "strict":
			self.cfg.provisionalResume.Record.FinalAcceptance = true
		case "canceled":
			cancel()
		}
		if fault == "lineage" || fault == "allowance" || fault == "pending-contract" {
			var err error
			self.plan.PlanHash, err = self.plan.hash()
			if err != nil {
				t.Fatal(err)
			}
			self.cfg.provisionalResume.Record.PlanHash = self.plan.PlanHash
		}
		before, err := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		err = self.activateProvisionalSetupRevision(ctx, source, func(context.Context, Action) error { calls++; return nil })
		cancel()
		after, readErr := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
		if err == nil || readErr != nil || calls != 0 || !bytes.Equal(before, after) || !reflect.DeepEqual(fixture.entries, self.journal.Entries()) {
			t.Fatalf("%s changed approval or dispatched before full authentication: calls=%d err=%v read=%v", fault, calls, err, readErr)
		}
	}
}

func TestProvisionalSetupRevisionLeavesRuntimeRenderingAndAcceptanceDeferred(t *testing.T) {
	fixture, _, _ := provisionalSetupActivationFixture(t)
	report := &launchPreparationReport{Ready: true}
	// No runtime files or chain readers exist. Activation must not demand a
	// current config render that is explicitly deferred for the live topology.
	collectLaunchRuntimePreparation(report, "setup", fixture.executor)
	if !report.Ready || len(report.Checks) != 1 || report.Checks[0].Hard {
		t.Fatal("repair activation demanded launch-only runtime work", report)
	}
	options := cliOptions{Apply: true, ProvisionalResume: true, PlanHash: fixture.executor.plan.PlanHash}
	if _, _, err := parseCLI([]string{"setup", "--provisional-resume", "--apply", "--plan-hash", options.PlanHash}); err != nil {
		t.Fatal("explicit repair revision command is unavailable", err)
	}
	options.Detach = true
	if err := validateProvisionalResumeOptions("setup", options); err == nil {
		t.Fatal("provisional activation admitted process launch")
	}
}
