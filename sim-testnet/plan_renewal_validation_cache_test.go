package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotPlanRenewalCacheReusesAuthenticatedBytesAndInvalidatesChanges(t *testing.T) {
	_, stateDir, plan := fleetCensusPlanTestFixture(t)
	raw, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	cache, calls := &planRenewalValidationCache{}, 0
	verify := func(plan *SetupPlan) error {
		calls++
		return validateFleetRenewalPlan(plan)
	}
	for snapshot := 0; snapshot < 3; snapshot++ {
		got, err := cache.decode(raw, snapshot == 1, verify)
		if err != nil || got.PlanHash != plan.PlanHash || calls != 1 || got.validatorEvidenceHistorical != (snapshot == 1) {
			t.Fatalf("snapshot %d did not reuse pure verification: calls=%d err=%v", snapshot, calls, err)
		}
		// Decoded state remains detached; the cache has no pointer to mutate.
		got.Actions[0].IntentHash = "changed-local-copy"
	}
	plan.GeneratedAt += " changed exact wire"
	raw, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.decode(raw, false, verify); err != nil || calls != 2 {
		t.Fatalf("changed bytes inherited verification: calls=%d err=%v", calls, err)
	}
	plan.DeploymentID += "-corrupted"
	raw, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.decode(raw, false, verify); err == nil || !strings.Contains(err.Error(), "plan hash mismatch") || calls != 2 {
		t.Fatalf("changed canonical plan reused verification: calls=%d err=%v", calls, err)
	}
}

func TestSnapshotPlanRenewalCacheKeepsFailuresAndOtherValidationFresh(t *testing.T) {
	_, _, plan := fleetCensusPlanTestFixture(t)
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	cache, calls := &planRenewalValidationCache{}, 0
	failure := errors.New("invalid renewal signature")
	verify := func(plan *SetupPlan) error {
		calls++
		if calls <= 2 {
			return failure
		}
		return validateFleetRenewalPlan(plan)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := cache.decode(raw, false, verify); !errors.Is(err, failure) || calls != attempt {
			t.Fatalf("failed signature became a cache hit: calls=%d err=%v", calls, err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := cache.decode(raw, false, verify); err != nil || calls != 3 {
			t.Fatalf("recovered verification did not warm: calls=%d err=%v", calls, err)
		}
	}
	plan.MaximumSpend.AlphaRao = plan.Limits.AlphaRao + 1
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := cache.decode(raw, false, verify); err == nil || !strings.Contains(err.Error(), "alpha plan maximum") || calls != 4 {
			t.Fatalf("warm signature result bypassed budget: calls=%d err=%v", calls, err)
		}
	}
}

func TestSnapshotPlanRenewalCacheBoundsRetainedIdentities(t *testing.T) {
	cache, calls := &planRenewalValidationCache{}, 0
	verify := func(*SetupPlan) error { calls++; return nil }
	for index := 0; index <= maximumPlanRenewalValidationCacheEntries; index++ {
		if err := cache.verify([]byte{byte(index)}, nil, verify); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.successes) != maximumPlanRenewalValidationCacheEntries || len(cache.order) != maximumPlanRenewalValidationCacheEntries {
		t.Fatal("cache retained an unbounded set of prior approvals")
	}
	if err := cache.verify([]byte{0}, nil, verify); err != nil || calls != maximumPlanRenewalValidationCacheEntries+2 {
		t.Fatalf("evicted approval was not reverified: calls=%d err=%v", calls, err)
	}
}
