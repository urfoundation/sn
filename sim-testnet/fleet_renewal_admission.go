// Fleet repair reuses continuation capability admission while retaining exact
// reviewed source, append-only actions and live signing checks. Archiving an
// approval does not activate it or grant final release acceptance.
package main

import (
	"context"
	"errors"
)

// Authenticate the source under the caller's journal lock before retaining a
// reviewed successor. Doctor reopens that immutable approval just as setup
// does, so the active predecessor need not be rewritten before readiness passes.
func prepareFleetRenewalInvocation(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions, base, plan *SetupPlan, entries []JournalEntry) error {
	if err := validateFleetRenewalSource(cfg, base, plan, entries); err != nil {
		return err
	}
	if !options.ProvisionalResume {
		return nil
	}
	if err := validateProvisionalResumeOptions("fleet-renew", options); err != nil {
		return err
	}
	if !options.Apply || cfg == nil || cfg.provisionalResume == nil || cfg.readOnlyAudit {
		return errors.New("provisional fleet repair requires an authenticated apply driver")
	}
	if err := requireApproved(true, options.PlanHash, plan.PlanHash); err != nil {
		return err
	}
	archived, err := archiveReviewedSetupPlan(stateDir, plan)
	if err != nil {
		return err
	}
	if !evidenceRelayContinuationSameJSON(archived, plan) {
		return errors.New("fleet repair differs from its immutable reviewed plan")
	}
	retained, err := loadInvocationPlan(cfg, stateDir, "fleet-renew", options)
	if err != nil {
		return err
	}
	return prepareProvisionalResume(ctx, cfg, stateDir, "fleet-renew", options, retained)
}
