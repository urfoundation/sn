// Initial failure artifacts retain actual absence without inventing observed
// blocks, and authenticated recovery never rewrites their terminal result.
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initialObservationFailureFixture(t *testing.T) (*campaignSuccessionFixture, *scenarioCampaignAttempt, *ScenarioResult, string) {
	t.Helper()
	fixture := newCampaignSuccessionFixture(t)
	bindCampaignRecoveryFixture(t, fixture)
	prior, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := loadLiveProcessLogGate(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	gate.provisionalObservationOnly = true
	failure := errors.New("synthetic first observation unavailable")
	result, err := runScenarioWithProbe(t.Context(), fixture.cfg, fixture.stateDir, scenarioDefinition{Name: "release-1.0"}, &staticScenarioProbe{err: failure}, scenarioRunOptions{Attempt: prior, ProcessLogs: gate})
	if !errors.Is(err, failure) || result == nil || result.Result != "fail" || prior.payload.AcceptanceBoundary != nil {
		t.Fatalf("initial failure was not retained before acceptance: result=%v err=%v", result, err)
	}
	return fixture, prior, result, filepath.Join(fixture.stateDir, "runs", prior.payload.RunID)
}

// Exercise the real nil-snapshot failure producer, then validate the complete
// signed recovery chain using its explicit empty-observation marker.
func TestInitialObservationFailurePublishesRecoverableMarker(t *testing.T) {
	fixture, prior, result, runDir := initialObservationFailureFixture(t)
	marker, err := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
	if err != nil || string(marker) != preAcceptanceInterruptedObservationMarker {
		t.Fatalf("initial failure omitted its explicit absence marker: %q %v", marker, err)
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	completed, err := time.Parse(time.RFC3339Nano, result.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}
	next, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, completed.Add(time.Minute), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecovery(next); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(runDir, "result.json"))
	if err != nil || !bytes.Equal(after, raw) || next.payload.Recovery.PriorRunID != prior.payload.RunID || next.payload.Recovery.PriorResultSha256 != bytesSHA256(raw) || next.payload.Recovery.PriorObservationLogSha256 != bytesSHA256(marker) || next.payload.AcceptanceBoundary != nil {
		t.Fatalf("recovery changed terminal evidence or manufactured an interval: %v", err)
	}
}

// Reproduce the old binary's terminal result/process-log pair without a log.
// Only a new marker is added; the already hashed result remains byte-identical.
func TestInitialObservationFailureLegacyBackfillRetainsResult(t *testing.T) {
	fixture, prior, result, runDir := initialObservationFailureFixture(t)
	path := filepath.Join(runDir, "observations.jsonl")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	completed, err := time.Parse(time.RFC3339Nano, result.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}
	next, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, completed.Add(time.Minute), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecovery(next); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(runDir, "result.json"))
	if err != nil || !bytes.Equal(after, raw) || next.payload.Recovery.PriorRunID != prior.payload.RunID {
		t.Fatalf("legacy backfill changed original result: %v", err)
	}
	marker, err := os.ReadFile(path)
	if err != nil || string(marker) != preAcceptanceInterruptedObservationMarker {
		t.Fatal("legacy marker absent", err)
	}
	if err := os.WriteFile(path, append(marker, 'x'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecovery(next); err == nil {
		t.Fatal("signed recovery ignored substituted observation marker")
	}
}

// A terminal result containing any observed progress cannot authorize invented
// empty history; every remaining original artifact stays untouched.
func TestInitialObservationFailureBackfillRejectsObservedProgress(t *testing.T) {
	fixture, prior, result, runDir := initialObservationFailureFixture(t)
	path := filepath.Join(runDir, "observations.jsonl")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ScenarioResult){
		func(result *ScenarioResult) { result.StartHead.Number = 1 },
		func(result *ScenarioResult) { result.Assertions[0].ObservationHash = "0x" + strings.Repeat("ab", 32) },
	} {
		changed := *result
		changed.Assertions = append([]AssertionRecord(nil), result.Assertions...)
		mutate(&changed)
		var err error
		changed.EvidenceHash, err = canonicalScenarioResultHash(&changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := writePublicJSON(filepath.Join(runDir, "result.json"), &changed); err != nil {
			t.Fatal(err)
		}
		if err := materializeMissingPreAcceptanceObservationLog(fixture.stateDir, prior); err == nil {
			t.Fatal("observed predecessor acquired an empty-history marker")
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rejected backfill wrote observations", err)
		}
	}
}

// A failed append used to be silently dropped. Propagate both causes before a
// terminal result can claim that its complete observation artifacts are durable.
func TestInitialObservationFailurePropagatesAppendError(t *testing.T) {
	cfg := testResolvedConfig(t)
	runDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(runDir, "observations.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("synthetic observation failure")
	observation := testScenarioObservation(cfg, 2)
	result, err := writeInitialScenarioFailure(cfg, runDir, "synthetic-initial-failure", "synthetic-definition", scenarioDefinition{Name: "synthetic"}, time.Now().Add(-time.Minute), observation, nil, failure)
	var pathErr *os.PathError
	if !errors.Is(err, failure) || !errors.As(err, &pathErr) || result != nil || !strings.Contains(err.Error(), "persist initial scenario observation") {
		t.Fatalf("append failure was discarded: result=%v err=%v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(runDir, "result.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed observation publication left a terminal result", err)
	}
}

// Empty-history publication never replaces existing evidence or follows aliases.
func TestInitialObservationFailureMarkerPreservesExistingBytes(t *testing.T) {
	runDir := t.TempDir()
	path := filepath.Join(runDir, "observations.jsonl")
	raw := []byte("retained synthetic observation\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensurePreAcceptanceObservationLog(runDir); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("marker replaced existing history", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "absent-observation")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := ensurePreAcceptanceObservationLog(runDir); err == nil {
		t.Fatal("marker followed a symlink")
	}
	if _, err := os.Lstat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("marker changed symlink target", err)
	}
}
