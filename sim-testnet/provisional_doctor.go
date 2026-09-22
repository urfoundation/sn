// Read-only provisional diagnostics use the same retained approval and runtime
// capability admission as continuation. Their observations grant no acceptance.
package main

import (
	"context"
	"errors"
	"fmt"
)

// Admit one exact retained plan before observing a successor runtime. Only
// immutable invocation provenance is written; deployment inputs and journals
// remain untouched, and the caller keeps its original strict configuration.
func prepareProvisionalDoctor(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) (*ResolvedConfig, error) {
	if err := validateProvisionalResumeOptions("doctor", options); err != nil {
		return nil, err
	}
	if !options.ProvisionalResume {
		return cfg, nil
	}
	if ctx == nil || cfg == nil || cfg.provisionalResume == nil {
		return nil, errors.New("provisional doctor requires authenticated driver provenance")
	}
	plan, err := loadInvocationPlan(cfg, stateDir, "doctor", options)
	if err != nil {
		return nil, fmt.Errorf("provisional doctor requires the unchanged persisted plan: %w", err)
	}
	reader, provenance := *cfg, *cfg.provisionalResume
	reader.provisionalResume = &provenance
	reader.readOnlyAudit = true
	if err := prepareProvisionalResume(ctx, &reader, stateDir, "doctor", options, plan); err != nil {
		return nil, err
	}
	return &reader, nil
}
