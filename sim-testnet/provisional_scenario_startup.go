// Provisional release scenarios may restart an exactly retained stopped
// successor before opening the new generation's live process-log gate.
package main

import (
	"context"
	"errors"
)

// Observation-only epoch commands have a read-only journal and cannot own a
// restart. Release scenarios share the existing explicit resume authority.
func provisionalRetainedStartupAllowed(record *provisionalResumeRecord) bool {
	if record == nil || !record.Provisional || record.FinalAcceptance || record.ReadOnly {
		return false
	}
	if record.Command == "resume" {
		return true
	}
	return record.Command == "scenario" && (record.Scenario == "release-1.0" || record.Scenario == "production-soak" || record.Scenario == releaseCandidateCampaignName)
}

// Complete the authenticated process-only restart before the scenario reads
// its gate. A failed start never enters traffic; an existing live topology
// passes through without a restart and keeps its normal exact-generation gate.
func runScenarioAfterRetainedStartup(ctx context.Context, executor *Executor, stopped *provisionalStoppedTopology, bins map[string]string, start retainedProvisionalStarter, run func() error) error {
	if ctx == nil || run == nil {
		return errors.New("scenario startup continuation is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if stopped != nil {
		if executor == nil || !provisionalResumeEnabled(executor.cfg) || executor.cfg.provisionalResume.Record.Command != "scenario" {
			return errors.New("stopped scenario startup requires explicit provisional scenario authority")
		}
		if err := executeRetainedProvisionalResume(ctx, executor, stopped, bins, start); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return run()
}
