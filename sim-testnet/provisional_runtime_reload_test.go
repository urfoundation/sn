// Runtime preparation must not reintroduce the driver/release equality gate
// after provisional admission has authenticated and retained the exact plan.
package main

import (
	"errors"
	"testing"
)

// Exercise the production admission, durable provenance and reload paths.
func TestProvisionalRuntimeReloadRetainsAdmittedPlanAcrossReleaseHotfix(t *testing.T) {
	cfg, plan, stateDir, options, _ := provisionalRuntimePlanFixture(t)
	if _, err := loadRuntimePersistedPlan(cfg, stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("unadmitted invocation bypassed strict release identity: %v", err)
	}
	if err := prepareProvisionalResume(t.Context(), cfg, stateDir, "resume", options, plan); err != nil {
		t.Fatal(err)
	}
	for repeat := 0; repeat < 3; repeat++ {
		retained, err := loadRuntimePersistedPlan(cfg, stateDir)
		if err != nil || retained.PlanHash != plan.PlanHash || retained.ReleaseLockHash != plan.ReleaseLockHash {
			t.Fatalf("runtime reader %d lost admitted plan: %v", repeat, err)
		}
	}
	if _, err := loadPersistedPlan(cfg, stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("strict acceptance inherited provisional release waiver: %v", err)
	}
}

// Hotfix compatibility never grants another approval, final acceptance or
// changed operating inputs; every subsequent runtime reader rechecks them.
func TestProvisionalRuntimeReloadRejectsApprovalAndOperationalDrift(t *testing.T) {
	for _, change := range []func(*ResolvedConfig){
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Record.PlanHash = "different-plan" },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Record.FinalAcceptance = true },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Record.Provisional = false },
		func(cfg *ResolvedConfig) { cfg.ConfigHash += "-changed" },
		func(cfg *ResolvedConfig) { cfg.OperationalEVM += "/changed" },
		func(cfg *ResolvedConfig) { cfg.MaximumTAORao++ },
	} {
		cfg, plan, stateDir, options, _ := provisionalRuntimePlanFixture(t)
		if err := prepareProvisionalResume(t.Context(), cfg, stateDir, "resume", options, plan); err != nil {
			t.Fatal(err)
		}
		change(cfg)
		if _, err := loadRuntimePersistedPlan(cfg, stateDir); err == nil {
			t.Fatal("runtime reload accepted changed approval or inputs")
		}
	}
}
