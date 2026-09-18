// Count immutable verifier invocations while exercising the real canonical
// plan reader and budget gate, including recovery and changed-file failures.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// No renewal is needed to test ownership of the verifier boundary. Full signed
// renewal semantics remain covered by the existing lifecycle descriptor test.
func fleetCensusPlanTestFixture(t *testing.T) (*ResolvedConfig, string, *SetupPlan) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFleetCensusTestPlan(t, stateDir, plan)
	return cfg, stateDir, plan
}

// Preserve canonical nested manifests and any intentionally stale plan hash.
func writeFleetCensusTestPlan(t *testing.T, stateDir string, plan *SetupPlan) {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFleetCensusPlanCacheReusesImmutableValidationAcrossRecovery(t *testing.T) {
	cfg, stateDir, plan := fleetCensusPlanTestFixture(t)
	verified := 0
	verify := func(plan *SetupPlan) error {
		verified++
		return validateFleetRenewalPlan(plan)
	}
	for attempt := 0; attempt < 3; attempt++ {
		got, err := readFleetCensusPlanWithVerifier(cfg, stateDir, verify)
		if err != nil || got.PlanHash != plan.PlanHash || verified != 1 {
			t.Fatalf("recovery %d repeated immutable validation: calls=%d err=%v", attempt, verified, err)
		}
		// Mutating one caller's decoded copy cannot poison later cache hits.
		got.Actions[0].IntentHash = "changed-local-copy"
	}
	plan.GeneratedAt = time.Unix(2, 0).UTC().Format(time.RFC3339Nano)
	writeFleetCensusTestPlan(t, stateDir, plan)
	if _, err := readFleetCensusPlanWithVerifier(cfg, stateDir, verify); err != nil || verified != 2 {
		t.Fatalf("changed exact bytes inherited validation: calls=%d err=%v", verified, err)
	}
}

func TestFleetCensusPlanCacheRejectsChangedCanonicalPlanBeforeReuse(t *testing.T) {
	cfg, stateDir, plan := fleetCensusPlanTestFixture(t)
	verified := 0
	verify := func(plan *SetupPlan) error {
		verified++
		return validateFleetRenewalPlan(plan)
	}
	if _, err := readFleetCensusPlanWithVerifier(cfg, stateDir, verify); err != nil {
		t.Fatal(err)
	}
	plan.DeploymentID += "-modified"
	writeFleetCensusTestPlan(t, stateDir, plan)
	if _, err := readFleetCensusPlanWithVerifier(cfg, stateDir, verify); err == nil || !strings.Contains(err.Error(), "plan hash mismatch") || verified != 1 {
		t.Fatalf("tampered canonical plan reused validation: calls=%d err=%v", verified, err)
	}
}

func TestFleetCensusPlanCacheDoesNotCacheFailedImmutableValidation(t *testing.T) {
	cfg, stateDir, _ := fleetCensusPlanTestFixture(t)
	verified := 0
	failure := errors.New("fixture invalid renewal signature")
	verify := func(*SetupPlan) error {
		verified++
		return failure
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := readFleetCensusPlanWithVerifier(cfg, stateDir, verify); !errors.Is(err, failure) || verified != attempt+1 {
			t.Fatalf("failed verifier became a warm success: calls=%d err=%v", verified, err)
		}
	}
}

func TestFleetCensusPlanCacheKeepsBudgetChecksFreshOnWarmSuccess(t *testing.T) {
	cfg, stateDir, plan := fleetCensusPlanTestFixture(t)
	plan.MaximumSpend.AlphaRao = plan.Limits.AlphaRao + 1
	var err error
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	writeFleetCensusTestPlan(t, stateDir, plan)
	verified := 0
	verify := func(plan *SetupPlan) error {
		verified++
		return validateFleetRenewalPlan(plan)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := readFleetCensusPlanWithVerifier(cfg, stateDir, verify); err == nil || !strings.Contains(err.Error(), "alpha plan maximum") || verified != 1 {
			t.Fatalf("warm renewal success bypassed current budget checks: calls=%d err=%v", verified, err)
		}
	}
}
