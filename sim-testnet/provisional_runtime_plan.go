// Runtime readers retain the invocation's already-authenticated approval;
// strict release and final-acceptance readers continue to use the strict loader.
package main

import "errors"

// Revalidate the persisted wire, current operational configuration and budget
// on every reload. Only an admitted provisional invocation can retain its
// original release identity, and it cannot switch plans after admission.
func loadRuntimePersistedPlan(cfg *ResolvedConfig, stateDir string) (*SetupPlan, error) {
	if !provisionalResumeEnabled(cfg) {
		return loadPersistedPlan(cfg, stateDir)
	}
	plan, err := loadPersistedPlanIdentity(cfg, stateDir, true)
	if err != nil {
		return nil, err
	}
	record := cfg.provisionalResume.Record
	if !record.Provisional || record.FinalAcceptance || record.PlanHash != plan.PlanHash {
		return nil, errors.New("runtime plan reload differs from the admitted provisional non-accepting approval")
	}
	return plan, nil
}
