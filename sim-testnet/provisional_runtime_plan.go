// Runtime readers retain the invocation's already-authenticated approval;
// strict release and final-acceptance readers continue to use the strict loader.
package main

import "errors"

// Observe the exact persisted bytes and operational authority on every reload.
// Nested render readers may reuse a complete validation of those same inputs;
// only an admitted provisional invocation can retain its original release.
func loadRuntimePersistedPlan(cfg *ResolvedConfig, stateDir string) (*SetupPlan, error) {
	retainRelease := provisionalResumeEnabled(cfg)
	raw, err := readSetupPlanBytes(stateDir, "plan.json")
	if err != nil {
		return nil, err
	}
	var plan *SetupPlan
	if cfg != nil && cfg.runtimePlanReads != nil {
		plan, err = cfg.runtimePlanReads.load(cfg, stateDir, raw, retainRelease)
	} else {
		plan, err = loadPlanIdentityBytes(cfg, raw, retainRelease)
	}
	if err != nil {
		return nil, err
	}
	if !retainRelease {
		return plan, nil
	}
	record := cfg.provisionalResume.Record
	if !record.Provisional || record.FinalAcceptance || record.PlanHash != plan.PlanHash {
		return nil, errors.New("runtime plan reload differs from the admitted provisional non-accepting approval")
	}
	return plan, nil
}
