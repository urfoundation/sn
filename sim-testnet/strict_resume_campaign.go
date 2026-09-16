package main

// Strict retained-history startup can hand its existing writer directly to
// the full campaign. Standalone resume and scenario retain their own lifecycle.

import (
	"context"
	"errors"
	"fmt"
)

// Require an explicit combined operation before preparation can change local
// state. Detach applies to the supervisor; this command still owns the campaign.
func validateStrictResumeCampaignOptions(command string, options cliOptions) error {
	if !options.ThenReleaseCandidate {
		return nil
	}
	if command != "resume" || !options.Apply || !options.Detach || !validCanonicalHashHex(options.PlanHash) {
		return errors.New("--then-release-candidate requires resume --apply --detach and the exact approved --plan-hash")
	}
	if options.StrictHistoryAdoption == "" || !validSHA256ContentHash(options.StrictHistoryAdoptionSHA256) {
		return errors.New("--then-release-candidate requires the exact strict history adoption request and SHA256")
	}
	if options.PrepareOnly || options.ProvisionalResume || options.ProvisionalRPCAuthority != "" || options.ProvisionalObservationTimeout != 0 || options.Name != "" {
		return errors.New("--then-release-candidate is a full strict campaign; preparation-only, provisional options and --name are incompatible")
	}
	return nil
}

// Launch owns readiness and failed-generation cleanup. On success, the same
// approved executor and open journal enter the ordinary campaign runner, which
// retains archive, live phase, horizon and final semantic acceptance checks.
// No second mutation invocation or standalone-resume success is published.
func runStrictResumeCampaign(ctx context.Context, options cliOptions, executor *Executor, binaries map[string]string,
	launch func(context.Context, *ResolvedConfig, string, *SetupPlan, *RoleSecrets, *Executor, map[string]string, bool) error,
	campaign func(context.Context, *ResolvedConfig, string, *Journal, *Executor, *RoleSecrets, scenarioCampaignRunner) error,
) (map[string]any, error) {
	if err := validateStrictResumeCampaignOptions("resume", options); err != nil {
		return nil, err
	}
	if !options.ThenReleaseCandidate || ctx == nil || executor == nil || executor.cfg == nil || executor.cfg.Config == nil || executor.plan == nil || executor.roles == nil || executor.journal == nil || executor.preparationIncomplete || launch == nil || campaign == nil {
		return nil, errors.New("strict resume campaign requires its complete prepared writer")
	}
	cfg, plan, journal, roles, stateDir := executor.cfg, executor.plan, executor.journal, executor.roles, executor.stateDir
	adoption := cfg.strictHistoryAdoption
	if provisionalResumeEnabled(cfg) || adoption == nil || adoption.path != options.StrictHistoryAdoption || adoption.hash != options.StrictHistoryAdoptionSHA256 || adoption.bundle.ApprovedPlanHash != plan.PlanHash || options.PlanHash != plan.PlanHash {
		return nil, errors.New("strict resume campaign approval or authenticated history adoption changed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := launch(ctx, cfg, stateDir, plan, roles, executor, binaries, true); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if executor.cfg != cfg || executor.plan != plan || executor.journal != journal || executor.roles != roles || executor.stateDir != stateDir || executor.preparationIncomplete || cfg.strictHistoryAdoption != adoption || provisionalResumeEnabled(cfg) || plan.PlanHash != options.PlanHash || adoption.bundle.ApprovedPlanHash != plan.PlanHash || adoption.path != options.StrictHistoryAdoption || adoption.hash != options.StrictHistoryAdoptionSHA256 {
		return nil, errors.New("strict resume campaign writer or approval changed during startup")
	}
	if err := campaign(ctx, cfg, stateDir, journal, executor, roles, runScenarioCampaignAttempt); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return map[string]any{
		"schema": "urnetwork-sim-command-result-v1", "command": "resume", "scenario": releaseCandidateCampaignName, "campaign_complete": true,
		"deployment_id": cfg.Config.Deployment.DeploymentID, "plan_hash": plan.PlanHash, "state_dir": stateDir,
		"status_command": fmt.Sprintf("sim-testnet status --config %s --state-dir %s", cfg.ConfigPath, stateDir) + ownedRPCCommandOption(cfg),
	}, nil
}
