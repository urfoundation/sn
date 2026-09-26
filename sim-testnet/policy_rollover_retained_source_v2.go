//go:build linux || darwin

// Activated generations keep their original approval after an independent
// fleet renewal. Only retained readers may resolve that archived authority.
package main

import (
	"context"
	"errors"
)

// Authenticate the named ancestor and unchanged evidence custody before
// validating retained bytes. Current configuration, policy and transport remain unchanged; there is no
// fingerprint substitution that could conceal a new configuration meaning.
// Fresh rollover and source-role mutation validators never call this helper.
func retainedPolicyRolloverSourceV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, current *SetupPlan, sourceHash string) (*SetupPlan, error) {
	_, source, err := retainedPolicyRolloverContextV2(ctx, cfg, stateDir, current, sourceHash)
	return source, err
}

// A signed exact-field migration resolves authentic old config bytes only for
// retained readers. Fresh rollover and source-role mutations retain current cfg.
func retainedPolicyRolloverContextV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, current *SetupPlan, sourceHash string) (*ResolvedConfig, *SetupPlan, error) {
	if ctx == nil || cfg == nil || current == nil || !validCanonicalHashHex(sourceHash) {
		return nil, nil, errors.New("retained rollover source authority is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if sourceHash == current.PlanHash {
		return cfg, current, nil
	}
	if !current.allowedPlanHashes()[sourceHash] || cfg.ConfigHash != current.ConfigHash {
		return nil, nil, errors.New("retained rollover source is outside the approved current lineage or configuration")
	}
	source, err := readValidatorEvidenceHistoricalPlan(stateDir, sourceHash)
	if err != nil {
		return nil, nil, err
	}
	if err := validatorEvidenceSourcePlanMatches(current, source); err != nil {
		return nil, nil, err
	}
	if source.ConfigHash != current.ConfigHash && current.CampaignConfigMigrationHash != "" {
		sourceCfg, source, _, err := authenticatedCampaignConfigMigrationSource(ctx, cfg, stateDir, current, sourceHash)
		return sourceCfg, source, err
	}
	if source.ConfigHash != current.ConfigHash || source.PolicyHash != current.PolicyHash || source.OwnedRPCAuthority != current.OwnedRPCAuthority {
		return nil, nil, errors.New("retained rollover successor changed configuration, policy or owned Rpc authority")
	}
	return cfg, source, ctx.Err()
}
