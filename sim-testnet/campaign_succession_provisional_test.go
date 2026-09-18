// Explicit provisional ownership enters the same signed successor path and
// cannot inherit failed work, change approval or promote final acceptance.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the actual durable-attempt entry point with a terminal signed
// ancestor, rather than bypassing its plan, receipt or custody admission.
func TestProvisionalCampaignSuccessionPreservesFailureAndFullWork(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	fixture.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: fixture.current.PlanHash, Provisional: true}}
	paths := []string{
		scenarioCampaignAttemptPath(fixture.stateDir, "release-1.0"),
		filepath.Join(fixture.stateDir, "runs", fixture.attempt.payload.RunID, "result.json"),
		filepath.Join(fixture.stateDir, "plan.json"),
		filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.prior.PlanHash)+".json"),
		filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.current.PlanHash)+".json"),
		filepath.Join(fixture.stateDir, "journal.jsonl"),
		filepath.Join(fixture.stateDir, "secrets", "roles.json"),
	}
	originalKVs := map[string][]byte{}
	for _, path := range paths {
		originalKVs[path] = readCampaignSuccessionFixtureBytes(t, path)
	}
	next, err := fixture.open()
	if err != nil {
		t.Fatalf("explicit provisional successor could not open durable full work: %v", err)
	}
	if next.payload.Succession == nil || next.payload.PlanHash != fixture.current.PlanHash || next.payload.RunID == fixture.attempt.payload.RunID ||
		next.payload.PreparationComplete || next.payload.HandoffAuthenticated || next.payload.AcceptanceBoundary != nil || next.payload.PriorRelease != nil {
		t.Fatal("provisional successor inherited failed work or changed its exact plan")
	}
	prepared := 0
	if err := beginScenarioCampaignPreparation(t.Context(), "release-1.0", next.payload.RunID, scenarioRunOptions{Attempt: next, Prepare: func(context.Context) error {
		prepared++
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	again, err := fixture.open()
	if err != nil || again.payload.RunID != next.payload.RunID || !again.payload.PreparationComplete || prepared != 1 {
		t.Fatalf("provisional successor did not reopen its own complete preparation: %v", err)
	}
	result := &ScenarioResult{RunID: next.payload.RunID, Name: "release-1.0", Result: "pass"}
	applyProvisionalScenarioProvenance(fixture.cfg, result)
	if !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance || result.ProvisionalDriver == nil {
		t.Fatal("provisional successor could claim final acceptance")
	}
	for path, original := range originalKVs {
		if !bytes.Equal(original, readCampaignSuccessionFixtureBytes(t, path)) {
			t.Fatalf("provisional succession changed original approval, failure or custody at %s", path)
		}
	}
}

// Provisional mode changes only driver acceptance. Exact approval, exclusive
// ownership, original signed history and private provider custody remain real.
func TestProvisionalCampaignSuccessionRejectsUnboundOrChangedAuthority(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"plan", "promoted", "missing-record", "closed-owner", "custody", "signed-source"} {
		fixture := newCampaignSuccessionFixture(t)
		fixture.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: fixture.current.PlanHash, Provisional: true}}
		switch fault {
		case "plan":
			fixture.cfg.provisionalResume.Record.PlanHash = fixture.prior.PlanHash
		case "promoted":
			fixture.cfg.provisionalResume.Record.FinalAcceptance = true
		case "missing-record":
			fixture.cfg.provisionalResume.Record = nil
		case "closed-owner":
			if err := fixture.journal.Close(); err != nil {
				t.Fatal(err)
			}
		case "custody":
			if err := os.Remove(filepath.Join(fixture.stateDir, "secrets", "roles.json")); err != nil {
				t.Fatal(err)
			}
		case "signed-source":
			path := scenarioCampaignAttemptPath(fixture.stateDir, "release-1.0")
			raw := readCampaignSuccessionFixtureBytes(t, path)
			if err := os.WriteFile(path, bytes.Replace(raw, []byte("release-1.0"), []byte("release-2.0"), 1), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := fixture.open()
		if err == nil {
			t.Fatalf("provisional succession admitted %s authority", fault)
		}
		if (fault == "plan" || fault == "promoted" || fault == "missing-record") && !strings.Contains(err.Error(), "exact non-accepting approval") {
			t.Fatalf("%s reached an unrelated refusal: %v", fault, err)
		}
		if _, err := os.Lstat(scenarioCampaignSuccessorPath(fixture.stateDir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refused provisional succession published a successor for %s: %v", fault, err)
		}
	}
}
