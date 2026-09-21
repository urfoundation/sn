// Synthetic approvals reproduce a retained no-mutation handoff whose plan
// predates the current campaign. Original files stay byte-identical throughout.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type historicalLifecycleFixture struct {
	campaign  *campaignSuccessionFixture
	lifecycle *liveFleetLifecycle
	evidence  *FleetLifecycleEvidence
	attempt   *scenarioCampaignAttempt
	original  []byte
}

func newHistoricalLifecycleFixture(t *testing.T) *historicalLifecycleFixture {
	t.Helper()
	campaign := newCampaignSuccessionFixture(t)
	root, _ := bindCampaignRecoveryFixture(t, campaign)
	sourcePlan := campaign.current
	lifecycle, evidence := fleetLifecycleProvisionalBypassFixture(t)
	evidence.DeploymentID = campaign.cfg.Config.Deployment.DeploymentID
	evidence.PlanHash, evidence.RunID = sourcePlan.PlanHash, root.payload.RunID
	evidence.Renewal = cloneFleetLifecycleRenewal(sourcePlan.FleetLifecycleRenewal)
	for _, identity := range []struct {
		uid   uint16
		churn int
	}{
		{uid: fleetLifecycleTargetExpectedUID, churn: fleetLifecycleTargetChurn},
		{uid: fleetLifecycleCompanionExpectedUID, churn: fleetLifecycleCompanionChurn},
		{uid: fleetLifecycleTerminalVictimUID, churn: fleetLifecycleTerminalVictimChurn},
	} {
		hotkey, hotkeyErr := roleBytes32(campaign.roles, churnHotkeyLabel(identity.churn))
		coldkey, coldkeyErr := roleBytes32(campaign.roles, churnColdkeyLabel(identity.churn))
		if hotkeyErr != nil || coldkeyErr != nil {
			t.Fatal(hotkeyErr, coldkeyErr)
		}
		evidence.LaunchPrune.Inputs[identity.uid].Hotkey = fleetLifecycleHex(hotkey)
		evidence.LaunchPrune.Inputs[identity.uid].Coldkey = fleetLifecycleHex(coldkey)
	}
	evidence.FirstAcceptedEpoch, evidence.AcceptanceStartBlock = 101, 1_000
	evidence.AcceptanceEndBlock, evidence.AcceptanceTerminalBlock = 2_500, 2_650
	advanceCampaignLineageFixture(t, campaign)
	accepted, err := provisionalAcceptedPlanHashes(campaign.current)
	if err != nil {
		t.Fatal(err)
	}
	campaign.cfg.provisionalResume = &provisionalResumeState{
		RecordPath: filepath.Join(campaign.stateDir, "synthetic-provenance.json"), AcceptedPlanHashes: accepted,
		Record: &provisionalResumeRecord{
			Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, FinalAcceptance: false,
			ConfigHash: campaign.cfg.ConfigHash, DeploymentID: campaign.cfg.Config.Deployment.DeploymentID, PlanHash: campaign.current.PlanHash,
		},
	}
	attempt, err := loadOrCreateScenarioCampaignAttempt(campaign.cfg, campaign.stateDir, campaign.roles, campaign.current.PlanHash, "release-1.0", nil, campaign.now.Add(time.Hour), campaign.journal)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.cfg, lifecycle.stateDir, lifecycle.attempt = campaign.cfg, campaign.stateDir, attempt
	lifecycle.executor = &Executor{cfg: campaign.cfg, stateDir: campaign.stateDir, plan: campaign.current, roles: campaign.roles, journal: campaign.journal}
	evidence.ProvisionalBypass, err = fleetLifecycleProvisionalBypass(campaign.cfg, campaign.current, campaign.roles, *evidence.LaunchPrune)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(campaign.stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(campaign.stateDir, "public", "fleet-lifecycle.json")
	if err := writePublicJSON(path, evidence); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return &historicalLifecycleFixture{campaign: campaign, lifecycle: lifecycle, evidence: evidence, attempt: attempt, original: raw}
}

func TestFleetLifecycleHistoricalApprovalRetainsBytesThroughCurrentAcceptance(t *testing.T) {
	f := newHistoricalLifecycleFixture(t)
	planPath := filepath.Join(f.campaign.stateDir, "plan.json")
	journalPath := filepath.Join(f.campaign.stateDir, "journal.jsonl")
	planBefore := readCampaignSuccessionFixtureBytes(t, planPath)
	journalBefore := readCampaignSuccessionFixtureBytes(t, journalPath)
	if f.attempt.payload.PreparationComplete || f.attempt.payload.AcceptanceBoundary != nil {
		t.Fatal("new approval inherited preparation or acceptance")
	}
	if err := f.lifecycle.ValidatePhaseResume("release-1.0", f.attempt.payload.RunID); err != nil {
		t.Fatal(err)
	}
	if err := f.lifecycle.BeginPhase("release-1.0", f.attempt.payload.RunID); err != nil {
		t.Fatal(err)
	}
	if f.attempt.payload.PreparationComplete || f.attempt.payload.AcceptanceBoundary != nil || !f.lifecycle.retainedProvisionalRelease || f.lifecycle.evidence.PlanHash != f.evidence.PlanHash {
		t.Fatal("lifecycle admission changed current campaign authority or original approval")
	}
	window, present := f.lifecycle.RetainedAcceptanceWindowForPhase("release-1.0")
	if !present || window.StartBlock != f.evidence.AcceptanceStartBlock {
		t.Fatal("original lifecycle window lost")
	}
	if err := f.lifecycle.BindAcceptanceWindowForPhase("release-1.0", window); err != nil {
		t.Fatal(err)
	}
	// The real acceptance writer owns a fresh current window. Archive capture
	// must still work after that signed boundary exists (also after invalidation).
	runDir := bindFailedRecoveryGeneration(t, f.campaign, f.attempt, 200)
	if f.attempt.payload.AcceptanceBoundary.AcceptanceWindow.StartBlock == window.StartBlock {
		t.Fatal("current campaign reused old acceptance window")
	}
	if err := f.lifecycle.Advance(t.Context(), testScenarioObservation(f.campaign.cfg, 202), nil); err != nil {
		t.Fatal(err)
	}
	binding, err := captureScenarioLifecycleHandoff(f.campaign.cfg, f.campaign.stateDir, runDir, f.attempt.payload.RunID, f.attempt)
	if err != nil {
		t.Fatal(err)
	}
	if binding.PlanHash != f.campaign.current.PlanHash || binding.InheritedPlanHash != f.evidence.PlanHash || binding.InheritedReleaseRunID != f.evidence.RunID || binding.CurrentRunID != f.attempt.payload.RunID {
		t.Fatalf("archive lost either approval/run identity: %+v", binding)
	}
	if err := validateScenarioLifecycleHandoffBinding(f.campaign.cfg, *binding, f.original); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(f.campaign.stateDir, "public", "fleet-lifecycle.json"), filepath.Join(runDir, scenarioLifecycleHandoffFilename)} {
		if !bytes.Equal(readCampaignSuccessionFixtureBytes(t, path), f.original) {
			t.Fatalf("retained lifecycle bytes changed: %s", path)
		}
	}
	if !bytes.Equal(planBefore, readCampaignSuccessionFixtureBytes(t, planPath)) || !bytes.Equal(journalBefore, readCampaignSuccessionFixtureBytes(t, journalPath)) {
		t.Fatal("lifecycle recovery changed approval or transaction journal")
	}
	strict := *f.campaign.cfg
	strict.provisionalResume = nil
	if err := validateScenarioLifecycleHandoffBinding(&strict, *binding, f.original); err == nil {
		t.Fatal("strict acceptance admitted historical provisional handoff")
	}
	changed := *binding
	changed.InheritedPlanHash = ""
	if err := validateScenarioLifecycleHandoffBinding(f.campaign.cfg, changed, f.original); err == nil {
		t.Fatal("archive omitted the historical approval")
	}
	if err := f.lifecycle.BeginPhase("release-1.0", f.attempt.payload.RunID); err == nil {
		t.Fatal("post-acceptance archival access reopened lifecycle adoption")
	}
}

func TestFleetLifecycleHistoricalApprovalRejectsChangedIdentityAfterWarmAdmission(t *testing.T) {
	f := newHistoricalLifecycleFixture(t)
	if err := f.lifecycle.BeginPhase("release-1.0", f.attempt.payload.RunID); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name string
		edit func(*liveFleetLifecycle, *FleetLifecycleEvidence)
		want string
	}{
		{name: "strict", edit: func(l *liveFleetLifecycle, _ *FleetLifecycleEvidence) { l.cfg.provisionalResume = nil }},
		{name: "acceptance", edit: func(l *liveFleetLifecycle, _ *FleetLifecycleEvidence) {
			l.cfg.provisionalResume.Record.FinalAcceptance = true
		}},
		{name: "provisional", edit: func(l *liveFleetLifecycle, _ *FleetLifecycleEvidence) {
			l.cfg.provisionalResume.Record.Provisional = false
		}},
		{name: "approval", edit: func(l *liveFleetLifecycle, _ *FleetLifecycleEvidence) {
			l.cfg.provisionalResume.Record.PlanHash = "0x" + strings.Repeat("87", 32)
		}},
		{name: "policy", edit: func(l *liveFleetLifecycle, _ *FleetLifecycleEvidence) {
			l.executor.plan.PolicyHash = "0x" + strings.Repeat("88", 32)
		}},
		{name: "custody", edit: func(l *liveFleetLifecycle, _ *FleetLifecycleEvidence) {
			l.executor.plan.Roles.Owner += "-synthetic-other"
		}},
		{name: "run", edit: func(_ *liveFleetLifecycle, e *FleetLifecycleEvidence) { e.RunID = "synthetic-foreign-run" }},
		{name: "source-approval", edit: func(l *liveFleetLifecycle, e *FleetLifecycleEvidence) { e.PlanHash = l.executor.plan.PlanHash }},
		{name: "stage", edit: func(_ *liveFleetLifecycle, e *FleetLifecycleEvidence) { e.Stage = fleetLifecycleStageComplete }, want: "handoff stage"},
		{name: "production", edit: func(_ *liveFleetLifecycle, e *FleetLifecycleEvidence) { e.ProductionRunID = "synthetic-production-run" }, want: "production successor state"},
		{name: "mutation", edit: func(_ *liveFleetLifecycle, e *FleetLifecycleEvidence) { e.ProviderEffectiveEpoch = 1 }},
	} {
		lifecycle, cfg, executor, plan := *f.lifecycle, *f.lifecycle.cfg, *f.lifecycle.executor, *f.lifecycle.executor.plan
		invocation, record := *cfg.provisionalResume, *cfg.provisionalResume.Record
		cfg.provisionalResume, invocation.Record = &invocation, &record
		lifecycle.cfg, lifecycle.executor, executor.plan = &cfg, &executor, &plan
		evidence := *f.evidence
		mutation.edit(&lifecycle, &evidence)
		if err := lifecycle.validateProvisionalBypassState("release-1.0", evidence.RunID, &evidence); err == nil || mutation.want != "" && !strings.Contains(err.Error(), mutation.want) {
			t.Fatalf("warm historical admission changed %s rejection: %v", mutation.name, err)
		}
	}
	if err := writePublicJSON(scenarioCampaignAttemptPath(f.campaign.stateDir, "production-soak"), map[string]any{"synthetic": true}); err != nil {
		t.Fatal(err)
	}
	if err := f.lifecycle.Advance(t.Context(), testScenarioObservation(f.campaign.cfg, 202), nil); err == nil {
		t.Fatal("warm release history ignored a production descendant")
	}
}

func TestFleetLifecycleHistoricalApprovalReopensArchivedPlanOnNewOwner(t *testing.T) {
	f := newHistoricalLifecycleFixture(t)
	if err := f.lifecycle.ValidatePhaseResume("release-1.0", f.attempt.payload.RunID); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.campaign.stateDir, "plans", stringsTrim0x(f.evidence.PlanHash)+".json")
	if err := os.WriteFile(path, []byte("changed synthetic archived approval"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.lifecycle.BeginPhase("release-1.0", f.attempt.payload.RunID); err == nil {
		t.Fatal("changed archived approval retained cached adoption authority")
	}
	if !bytes.Equal(readCampaignSuccessionFixtureBytes(t, filepath.Join(f.campaign.stateDir, "public", "fleet-lifecycle.json")), f.original) {
		t.Fatal("failed adoption changed lifecycle evidence")
	}
}

func TestFleetLifecycleHistoricalApprovalArchiveRechecksCurrentBypassBytes(t *testing.T) {
	f := newHistoricalLifecycleFixture(t)
	if err := f.lifecycle.BeginPhase("release-1.0", f.attempt.payload.RunID); err != nil {
		t.Fatal(err)
	}
	runDir := bindFailedRecoveryGeneration(t, f.campaign, f.attempt, 200)
	path := filepath.Join(f.campaign.stateDir, "public", "fleet-lifecycle.json")
	for _, mutation := range []struct {
		name string
		edit func(*FleetLifecycleEvidence)
	}{
		{name: "acceptance", edit: func(e *FleetLifecycleEvidence) { e.ProvisionalBypass.FinalAcceptance = true }},
		{name: "provisional", edit: func(e *FleetLifecycleEvidence) { e.ProvisionalBypass.Provisional = false }},
		{name: "census", edit: func(e *FleetLifecycleEvidence) { e.ProvisionalBypass.RuntimePruneUid++ }},
		{name: "mutation", edit: func(e *FleetLifecycleEvidence) { e.ProviderEffectiveEpoch = 1 }},
	} {
		evidence, bypass := *f.evidence, *f.evidence.ProvisionalBypass
		evidence.ProvisionalBypass = &bypass
		mutation.edit(&evidence)
		if err := writePublicJSON(path, &evidence); err != nil {
			t.Fatal(err)
		}
		if _, err := captureScenarioLifecycleHandoff(f.campaign.cfg, f.campaign.stateDir, runDir, f.attempt.payload.RunID, f.attempt); err == nil {
			t.Fatalf("archive accepted changed %s after warm startup", mutation.name)
		}
		if _, err := os.Stat(filepath.Join(runDir, scenarioLifecycleHandoffFilename)); !os.IsNotExist(err) {
			t.Fatalf("rejected %s published lifecycle evidence: %v", mutation.name, err)
		}
	}
	if err := os.WriteFile(path, f.original, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureScenarioLifecycleHandoff(f.campaign.cfg, f.campaign.stateDir, runDir, f.attempt.payload.RunID, f.attempt); err != nil {
		t.Fatalf("original authenticated bytes did not remain usable: %v", err)
	}
}
