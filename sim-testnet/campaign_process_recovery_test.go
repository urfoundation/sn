// Process replacement tests retain real signed plans, observation checkpoints,
// and recovery ancestry. No chain or wall-clock sleep supplies correctness.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reproduce an owner exit after a durable acceptance boundary but before its
// deferred invalidation or terminal result could be written.
func openCampaignProcessRecoveryFixture(t *testing.T) (*campaignSuccessionFixture, *scenarioCampaignAttempt, string) {
	t.Helper()
	fixture := newCampaignSuccessionFixture(t)
	attempt, runDir := bindCampaignRecoveryFixture(t, fixture)
	attempt.payload.AcceptanceInvalidation, attempt.payload.AcceptanceInvalidatedAt = "", ""
	if err := writeScenarioCampaignAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(runDir, "result.json")); err != nil {
		t.Fatal(err)
	}
	return fixture, attempt, runDir
}

// Read exact bytes so the tests detect evidence or deployment rewrites, rather
// than accepting an equivalent decoded value with changed authentication.
func campaignProcessRecoveryBytes(t *testing.T, paths ...string) map[string][]byte {
	t.Helper()
	contents := map[string][]byte{}
	for _, path := range paths {
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		contents[path] = value
	}
	return contents
}

// Assert immutable sources survive both the retired interval and replacement.
func requireCampaignProcessRecoveryBytes(t *testing.T, contents map[string][]byte) {
	t.Helper()
	for path, expected := range contents {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("recovery changed %s: %v", path, err)
		}
	}
}

// A foreign process gets a new interval in one call; all historical work is
// retained and no old observation or process session enters acceptance.
func TestScenarioCampaignProcessRecoveryReplacesForeignSessionOnce(t *testing.T) {
	t.Parallel()
	fixture, prior, runDir := openCampaignProcessRecoveryFixture(t)
	before := campaignProcessRecoveryBytes(t,
		filepath.Join(fixture.stateDir, "plan.json"), filepath.Join(fixture.stateDir, "journal.jsonl"),
		filepath.Join(fixture.stateDir, "secrets", "roles.json"), filepath.Join(runDir, "observations.jsonl"),
		filepath.Join(runDir, scenarioCampaignStartFilename), filepath.Join(runDir, processLogEvidenceFilename))
	next, err := recoverScenarioCampaignProcessSession(t.Context(), prior, fixture.journal, "0x"+strings.Repeat("82", 32), fixture.now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if next.payload.RunID == prior.payload.RunID || next.payload.Recovery == nil || next.payload.Recovery.PriorRunID != prior.payload.RunID || next.payload.AcceptanceBoundary != nil || next.payload.AcceptanceInvalidation != "" || next.payload.PriorRelease != nil {
		t.Fatalf("replacement mixed interval identities: %+v", next.payload)
	}
	retired, _, err := readScenarioCampaignAttemptAt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", scenarioCampaignSuccessorPath(fixture.stateDir))
	if err != nil || retired.payload.AcceptanceInvalidation != "process-session-changed" {
		t.Fatalf("retired source=%+v err=%v", retired, err)
	}
	terminal, _, err := readScenarioCampaignRecoveryResult(fixture.cfg, fixture.stateDir, retired)
	if err != nil || terminal.Result != "fail" || terminal.FinalAcceptance == nil || *terminal.FinalAcceptance {
		t.Fatalf("retired terminal=%+v err=%v", terminal, err)
	}
	for _, source := range []*scenarioCampaignAttempt{prior, next} {
		again, err := recoverScenarioCampaignProcessSession(t.Context(), source, fixture.journal, "0x"+strings.Repeat("82", 32), time.Now().UTC())
		if err != nil || again.payload.RunID != next.payload.RunID {
			t.Fatalf("repeated recovery created another interval: next=%+v err=%v", again, err)
		}
	}
	files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("repeated recovery generations=%d err=%v", len(files), err)
	}
	requireCampaignProcessRecoveryBytes(t, before)
	// A delayed caller cannot overwrite a failure produced by the successor.
	nextRunDir := bindPreAcceptanceFailedRecoveryGeneration(t, fixture, next)
	terminalBytes := campaignProcessRecoveryBytes(t, filepath.Join(nextRunDir, "result.json"))
	if _, err := recoverScenarioCampaignProcessSession(t.Context(), prior, fixture.journal, "0x"+strings.Repeat("82", 32), time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "another terminal interval") {
		t.Fatalf("stale caller accepted a terminal successor: %v", err)
	}
	requireCampaignProcessRecoveryBytes(t, terminalBytes)
}

// Before acceptance, no process session owns the interval. Reuse the same
// preparation checkpoint even when its run directory has not been created.
func TestScenarioCampaignProcessRecoveryKeepsUnstartedPreparation(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	fixture.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: fixture.current.PlanHash, Provisional: true}}
	prior, err := fixture.open()
	if err != nil {
		t.Fatal(err)
	}
	if err := prior.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	before := campaignProcessRecoveryBytes(t, scenarioCampaignSuccessorPath(fixture.stateDir))
	next, err := recoverScenarioCampaignProcessSession(t.Context(), prior, fixture.journal, "0x"+strings.Repeat("82", 32), fixture.now.Add(time.Hour))
	if err != nil || next != prior || !next.payload.PreparationComplete || next.payload.AcceptanceBoundary != nil {
		t.Fatalf("unstarted preparation was reset: %+v %v", next, err)
	}
	requireCampaignProcessRecoveryBytes(t, before)
	if _, err := os.Lstat(filepath.Join(fixture.stateDir, "runs", prior.payload.RunID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("process recovery created an unstarted run: %v", err)
	}
}

// Two callers using the same exclusive deployment owner publish one successor
// under the phase lock; the second authenticates that exact new ancestry.
func TestScenarioCampaignProcessRecoverySerializesDuplicateCallers(t *testing.T) {
	t.Parallel()
	fixture, prior, _ := openCampaignProcessRecoveryFixture(t)
	type outcome struct {
		attempt *scenarioCampaignAttempt
		err     error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			next, err := recoverScenarioCampaignProcessSession(t.Context(), prior, fixture.journal, "0x"+strings.Repeat("82", 32), fixture.now.Add(time.Hour))
			results <- outcome{next, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatal(first.err, second.err)
	}
	if first.attempt.payload.RunID != second.attempt.payload.RunID {
		t.Fatal("duplicate callers published different intervals")
	}
	files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("duplicate caller generations=%d err=%v", len(files), err)
	}
}

// The next invocation completes a crash between signed invalidation and the
// terminal result. No additional invalidation or blank source is fabricated.
func TestScenarioCampaignProcessRecoveryFinishesInterruptedPublication(t *testing.T) {
	t.Parallel()
	fixture, prior, _ := openCampaignProcessRecoveryFixture(t)
	if err := prior.invalidateAcceptance("process-session-changed", fixture.now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	before := campaignProcessRecoveryBytes(t, scenarioCampaignSuccessorPath(fixture.stateDir))
	next, err := recoverScenarioCampaignProcessSession(t.Context(), prior, fixture.journal, "0x"+strings.Repeat("82", 32), fixture.now.Add(time.Hour))
	if err != nil || next == nil || next.payload.Recovery == nil || next.payload.Recovery.PriorRunID != prior.payload.RunID {
		t.Fatalf("interrupted publication=%+v err=%v", next, err)
	}
	requireCampaignProcessRecoveryBytes(t, before)
}

// Ownership, completion and corrupt terminal inputs reject before invalidation;
// an automatic retry must not turn missing authority into a destructive reset.
func TestScenarioCampaignProcessRecoveryProtectsIneligibleSources(t *testing.T) {
	for _, name := range []string{"same-session", "no-owner", "read-only", "complete", "production", "successful-result", "malformed-result", "canceled"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture, prior, runDir := openCampaignProcessRecoveryFixture(t)
			ctx := t.Context()
			journal := fixture.journal
			sessionId := "0x" + strings.Repeat("82", 32)
			switch name {
			case "same-session":
				sessionId = prior.payload.AcceptanceBoundary.ProcessSessionID
			case "no-owner":
				journal = nil
			case "read-only":
				fixture.cfg.readOnlyAudit = true
			case "complete", "production", "successful-result", "malformed-result":
				path, contents := filepath.Join(runDir, "complete.json"), "{}\n"
				if name == "production" {
					path = scenarioCampaignAttemptPath(fixture.stateDir, "production-soak")
				} else if name == "successful-result" || name == "malformed-result" {
					path, contents = filepath.Join(runDir, "result.json"), "{\"result\":\"pass\"}\n"
					if name == "malformed-result" {
						contents = "{"
					}
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			before := campaignProcessRecoveryBytes(t, scenarioCampaignSuccessorPath(fixture.stateDir))
			if next, err := recoverScenarioCampaignProcessSession(ctx, prior, journal, sessionId, fixture.now.Add(time.Hour)); err == nil || next != nil {
				t.Fatalf("ineligible recovery=%+v err=%v", next, err)
			}
			requireCampaignProcessRecoveryBytes(t, before)
			files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
			if err != nil || len(files) != 0 {
				t.Fatalf("ineligible recovery generations=%d err=%v", len(files), err)
			}
		})
	}
}

// The candidate driver must resolve a foreign session before its preflight and
// runner. Returning a new attempt from a helper is insufficient without this path.
func TestScenarioCampaignProcessRecoveryPrecedesCandidateStartup(t *testing.T) {
	t.Parallel()
	fixture, prior, _ := openCampaignProcessRecoveryFixture(t)
	preflightCalls, runnerCalls := 0, 0
	stop := errors.New("runner reached fresh interval")
	err := runReleaseCandidateCampaignWithAnalyzer(t.Context(), fixture.cfg, fixture.stateDir, fixture.journal, &Executor{plan: fixture.current}, fixture.roles,
		func(_ context.Context, _ *ResolvedConfig, _ string, _ string, _ *Journal, _ *Executor, attempt *scenarioCampaignAttempt) error {
			runnerCalls++
			if preflightCalls != 1 || attempt.payload.AcceptanceBoundary != nil || attempt.payload.Recovery == nil || attempt.payload.Recovery.PriorRunID != prior.payload.RunID {
				t.Fatalf("startup did not receive the new interval: %+v", attempt.payload)
			}
			return stop
		}, func(context.Context, *ResolvedConfig, string) error {
			preflightCalls++
			return nil
		}, func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
			t.Fatal("interrupted interval reached final analysis")
			return nil
		})
	if !errors.Is(err, stop) || runnerCalls != 1 || preflightCalls != 1 {
		t.Fatalf("one-invocation startup: runner=%d preflight=%d err=%v", runnerCalls, preflightCalls, err)
	}
}

// The existing fleet lifecycle belongs to an authenticated ancestor, not to
// the failed process. A fresh interval keeps it byte-for-byte without replay.
func TestScenarioCampaignProcessRecoveryRetainsFleetLifecycle(t *testing.T) {
	t.Parallel()
	lifecycle, evidence, prior, raw := fleetLifecycleProvisionalRecoveryFixture(t)
	fixture := &campaignSuccessionFixture{cfg: lifecycle.cfg, stateDir: lifecycle.stateDir, roles: lifecycle.executor.roles, current: lifecycle.executor.plan}
	journal := lifecycle.executor.journal
	fixture.journal = journal
	runDir := bindFailedRecoveryGeneration(t, fixture, prior, 12)
	prior.payload.AcceptanceInvalidation, prior.payload.AcceptanceInvalidatedAt = "", ""
	if err := writeScenarioCampaignAttempt(prior); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(runDir, "result.json")); err != nil {
		t.Fatal(err)
	}
	next, err := recoverScenarioCampaignProcessSession(t.Context(), prior, journal, "0x"+strings.Repeat("82", 32), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.attempt = next
	if err := lifecycle.ValidatePhaseResume("release-1.0", next.payload.RunID); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BeginPhase("release-1.0", next.payload.RunID); err != nil {
		t.Fatal(err)
	}
	if lifecycle.evidence.RunID != evidence.RunID || !lifecycle.retainedProvisionalRelease {
		t.Fatal("new interval replaced authenticated fleet lifecycle work")
	}
	requireCampaignProcessRecoveryBytes(t, map[string][]byte{filepath.Join(fixture.stateDir, "public", "fleet-lifecycle.json"): raw})
}
