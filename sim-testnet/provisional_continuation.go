// Continuation commands share one authority model: readers retain the active
// approval, while repairs may inspect an exact archived successor before apply.
// Neither authority promotes a recovery driver or runtime to final acceptance.
package main

import (
	"context"
	"errors"
	"fmt"
)

// Select the approval source independently of executable/runtime capability
// checks, so planning, repair and doctor cannot disagree about the same scope.
func provisionalReviewedPlan(command string, readOnly bool) (bool, error) {
	switch command {
	case "probe-recovery", "policy-rollover":
		return false, nil
	case "doctor", "terminal-diagnostics":
		if readOnly {
			return false, nil
		}
	case "fleet-renew":
		return !readOnly, nil
	case "setup":
		if !readOnly {
			return true, nil
		}
	case "resume", "scenario", "coordinator-repair":
		if !readOnly {
			return false, nil
		}
	}
	return false, errors.New("provisional continuation requires doctor or fleet-renew planning, or setup, resume, scenario, coordinator-repair or fleet-renew apply")
}

// Admit a read-only invocation using the same exact plan and runtime capability
// path as apply. Clone its authority so later callers cannot inherit permission
// from a diagnostic or plan generation; only immutable provenance is written.
func prepareProvisionalRetainedReader(ctx context.Context, cfg *ResolvedConfig, stateDir, command string, options cliOptions) (*ResolvedConfig, error) {
	if err := validateProvisionalResumeOptions(command, options); err != nil {
		return nil, err
	}
	if !options.ProvisionalResume {
		return cfg, nil
	}
	if ctx == nil || cfg == nil || cfg.provisionalResume == nil || options.Apply {
		return nil, errors.New("provisional reader requires read-only options and authenticated driver provenance")
	}
	plan, err := loadInvocationPlan(cfg, stateDir, command, options)
	if err != nil {
		return nil, fmt.Errorf("provisional %s requires the unchanged persisted plan: %w", command, err)
	}
	reader, provenance := *cfg, *cfg.provisionalResume
	reader.provisionalResume = &provenance
	reader.readOnlyAudit = true
	if err := prepareProvisionalResume(ctx, &reader, stateDir, command, options, plan); err != nil {
		return nil, err
	}
	return &reader, nil
}
