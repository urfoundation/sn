// Synthetic signed cleanup histories keep their original approval when a
// later configuration opens a new campaign recovery.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Seal the real cleanup checkpoints before changing the approved configuration.
func newHistoricalCleanupRecoveryFixture(t *testing.T) (*cleanupHistoryFixture, time.Time) {
	t.Helper()
	f := newCleanupHistoryFixture(t)
	prior := f.cleanup.history.attempt
	f.result.Assertions = []AssertionRecord{{ID: "fleet_lifecycle_complete", Passed: false, Message: "synthetic cleanup does not prove skipped lifecycle mutations"}}
	f.result.AssertionCount, f.result.FailedAssertionCount = 1, 1
	var err error
	f.result.EvidenceHash, err = canonicalScenarioResultHash(f.result)
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(prior.stateDir, "runs", prior.payload.RunID)
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), f.result); err != nil {
		t.Fatal(err)
	}
	completed, err := time.Parse(time.RFC3339Nano, f.result.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := prior.invalidateAcceptance("execution-exited-before-completion", completed.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	advanceCampaignLineageFixture(t, f.cleanup.history.campaign)
	return f, completed.Add(time.Hour)
}

// The actual recovery entry point authenticates the old cleanup under its
// own signed approval while publishing only a fresh, non-accepting attempt.
func TestScenarioCleanupHistoricalRecoveryKeepsOriginalApproval(t *testing.T) {
	t.Parallel()
	f, now := newHistoricalCleanupRecoveryFixture(t)
	c := f.cleanup.history.campaign
	prior := f.cleanup.history.attempt
	retainedKVs := retainedCampaignLineageBytes(t, c.stateDir)
	admission := *c.cfg.provisionalResume.Record
	planBefore := readCampaignSuccessionFixtureBytes(t, filepath.Join(c.stateDir, "plan.json"))
	journalBefore := readCampaignSuccessionFixtureBytes(t, filepath.Join(c.stateDir, "journal.jsonl"))
	attempt, err := loadOrCreateScenarioCampaignAttempt(c.cfg, c.stateDir, c.roles, c.current.PlanHash, "release-1.0", nil, now, c.journal)
	if err != nil {
		t.Fatal("authenticated historical cleanup blocked a fresh recovery", err)
	}
	if attempt.cfg != c.cfg || attempt.cfg.readOnlyAudit || attempt.payload.PlanHash != c.current.PlanHash || attempt.payload.ConfigHash != c.cfg.ConfigHash || attempt.payload.AcceptanceBoundary != nil || attempt.payload.PreparationComplete || attempt.payload.Recovery == nil || attempt.payload.Recovery.PriorRunID != prior.payload.RunID {
		t.Fatal("historical cleanup changed current campaign authority", attempt.payload)
	}
	for range 2 {
		if err := validateScenarioCampaignRecoveryAncestor(attempt, prior.payload.RunID); err != nil {
			t.Fatal("cold or warm ancestor validation lost historical approval", err)
		}
	}
	if !reflect.DeepEqual(admission, *c.cfg.provisionalResume.Record) || f.result.Result != "fail" || *f.result.FinalAcceptance {
		t.Fatal("historical inspection changed invocation provenance or acceptance")
	}
	for path, before := range retainedKVs {
		if after := readCampaignSuccessionFixtureBytes(t, path); !bytes.Equal(before, after) {
			t.Fatalf("historical cleanup recovery rewrote %s", path)
		}
	}
	if !bytes.Equal(planBefore, readCampaignSuccessionFixtureBytes(t, filepath.Join(c.stateDir, "plan.json"))) || !bytes.Equal(journalBefore, readCampaignSuccessionFixtureBytes(t, filepath.Join(c.stateDir, "journal.jsonl"))) {
		t.Fatal("historical cleanup recovery changed the active plan or transaction journal")
	}
}

// Historical provenance cannot authorize another run, another approval, or
// strict/current use of the same otherwise authentic lifecycle bytes.
func TestScenarioCleanupHistoricalApprovalRejectsForeignContext(t *testing.T) {
	t.Parallel()
	f, _ := newHistoricalCleanupRecoveryFixture(t)
	c := f.cleanup.history.campaign
	reader, err := newScenarioCampaignLineageReader(c.cfg, c.stateDir, c.roles, c.current.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	prior, _, err := reader.read(f.cleanup.history.attempt.path())
	if err != nil {
		t.Fatal(err)
	}
	binding := *f.result.LifecycleHandoff
	raw, err := os.ReadFile(filepath.Join(c.stateDir, "runs", prior.payload.RunID, scenarioLifecycleHandoffFilename))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioLifecycleHandoffBinding(prior.cfg, binding, raw); err != nil {
		t.Fatal("authenticated historical view lost its original handoff", err)
	}
	for _, mutate := range []func(*ScenarioLifecycleHandoff){
		func(value *ScenarioLifecycleHandoff) { value.PlanHash = c.current.PlanHash },
		func(value *ScenarioLifecycleHandoff) { value.ConfigHash = c.cfg.ConfigHash },
		func(value *ScenarioLifecycleHandoff) {
			value.CurrentRunID, value.ReleaseRunID = "synthetic-foreign-run", "synthetic-foreign-run"
		},
		func(value *ScenarioLifecycleHandoff) { value.InheritedPlanHash = c.current.PlanHash },
	} {
		changed := binding
		mutate(&changed)
		if err := validateScenarioLifecycleHandoffBinding(prior.cfg, changed, raw); err == nil {
			t.Fatalf("historical scope admitted a foreign handoff: %+v", changed)
		}
	}
	for _, mutate := range []func(*ResolvedConfig){
		func(value *ResolvedConfig) { value.readOnlyAudit = false },
		func(value *ResolvedConfig) { value.provisionalResume = nil },
		func(value *ResolvedConfig) { value.ConfigHash = c.cfg.ConfigHash },
		func(value *ResolvedConfig) {
			state, record := *value.provisionalResume, *value.provisionalResume.Record
			record.PlanHash = binding.PlanHash
			state.Record, value.provisionalResume = &record, &state
		},
	} {
		changed := *prior.cfg
		mutate(&changed)
		if err := validateScenarioLifecycleHandoffBinding(&changed, binding, raw); err == nil {
			t.Fatal("historical handoff survived a changed approval context")
		}
	}
	for _, readOnly := range []bool{false, true} {
		current := *c.cfg
		current.readOnlyAudit = readOnly
		if err := validateScenarioLifecycleHandoffBinding(&current, binding, raw); err == nil {
			t.Fatal("current invocation accepted historical provenance without authenticated scope")
		}
	}
	if err := reader.plans.check(); err != nil {
		t.Fatal(err)
	}
}

// A provenance rejection must retain its real cause instead of claiming that
// unchanged canonical lifecycle bytes differ from their handoff.
func TestScenarioCleanupHistoryReportsBindingFailure(t *testing.T) {
	t.Parallel()
	f := newCleanupHistoryFixture(t)
	cfg := *f.cleanup.history.campaign.cfg
	state, record := *cfg.provisionalResume, *cfg.provisionalResume.Record
	record.PlanHash = "0x" + strings.Repeat("b9", 32)
	state.Record, cfg.provisionalResume = &record, &state
	err := validateProvisionalLifecycleCleanupHistory(&cfg, f.result, f.history)
	if err == nil || !strings.Contains(err.Error(), "inherited release lifecycle handoff provenance is incomplete or inconsistent") {
		t.Fatal("cleanup wrapper hid the actual binding failure", err)
	}
}
