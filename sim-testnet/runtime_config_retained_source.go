//go:build linux || darwin

// Retained V6 startup authenticates original rendered bytes separately from
// the successor capacity delivered by its hash-pinned validator handoff.
package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
)

// Resolve the signed setup before deriving the sole permitted predecessor
// identity. A caller cannot select an unrelated manifest or archived plan.
func authenticatedRetainedRuntimeConfigManifest(cfg *ResolvedConfig, stateDir string, plan *SetupPlan) (*RuntimeConfigManifest, map[string]os.FileMode, error) {
	if cfg == nil || cfg.Config == nil || plan == nil {
		return nil, nil, errors.New("retained runtime manifest has no approved deployment")
	}
	if plan.CampaignConfigMigrationHash == "" && (plan.EvidenceRelayContinuation == nil || len(plan.EvidenceRelayContinuation.SourceBounds) == 0) {
		return authenticatedRuntimeConfigManifest(cfg, stateDir)
	}
	current, err := loadRuntimePersistedPlan(cfg, stateDir)
	if err != nil || current.PlanHash != plan.PlanHash {
		return nil, nil, errors.Join(errors.New("retained runtime manifest differs from the active approved plan"), err)
	}
	retainedConfigHash := ""
	if current.CampaignConfigMigrationHash != "" {
		sourceCfg, _, _, err := authenticatedCampaignConfigMigrationSource(context.Background(), cfg, stateDir, current, "")
		if err != nil {
			return nil, nil, err
		}
		retainedConfigHash = sourceCfg.ConfigHash
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		return nil, nil, err
	}
	retainedEvidenceHash := ""
	continuation := current.EvidenceRelayContinuation
	if continuation != nil && len(continuation.SourceBounds) != 0 {
		if !resolved.Config.ProvisionValidatorEvidenceV2 {
			return nil, nil, errors.New("retained source lifetime requires its authenticated original setup")
		}
		if err := continuation.validateSourceBounds(); err != nil {
			return nil, nil, err
		}
		values := slices.Clone(resolved.Config.ValidatorEvidenceV2)
		if len(values) != len(continuation.SourceBounds) {
			return nil, nil, errors.New("retained source lifetime lost an original validator")
		}
		for index, source := range continuation.SourceBounds {
			if values[index].ValidatorID != source.ValidatorId || !reflect.DeepEqual(values[index].Evidence.Bounds, source.Approved) {
				return nil, nil, errors.New("retained source lifetime differs from its approved resolution")
			}
			values[index].Evidence.Bounds = source.Original
		}
		original, config := *resolved, *resolved.Config
		config.ValidatorEvidenceV2, original.Config = values, &config
		retainedEvidenceHash, err = runtimeEvidenceV2Identity(&original)
		if err != nil {
			return nil, nil, err
		}
	}
	return authenticatedRuntimeConfigManifestWithRetainedIdentity(resolved, stateDir, retainedEvidenceHash, retainedConfigHash)
}

// Only the explicit non-accepting resume may retain an older render identity.
func verifyRetainedProvisionalRuntimeConfigManifest(cfg *ResolvedConfig, stateDir string, plan *SetupPlan) (runtimeConfigVerification, error) {
	if !provisionalResumeEnabled(cfg) || plan == nil || cfg.provisionalResume.Record.PlanHash != plan.PlanHash || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance {
		return runtimeConfigVerification{}, errors.New("retained runtime inputs require the exact provisional successor")
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		return runtimeConfigVerification{}, err
	}
	cfg = resolved
	manifest, expected, err := authenticatedRetainedRuntimeConfigManifest(cfg, stateDir, plan)
	if err != nil {
		return runtimeConfigVerification{}, err
	}
	return verifyResolvedRuntimeConfigManifest(cfg, stateDir, manifest, expected)
}

// Provisional scenario observation may inspect the same retained store bytes.
// Ordinary completion continues to require the current rendered identity.
func authenticatedRuntimeBlobConfigManifest(cfg *ResolvedConfig, stateDir string) (*RuntimeConfigManifest, map[string]os.FileMode, error) {
	if !provisionalResumeEnabled(cfg) {
		return authenticatedRuntimeConfigManifest(cfg, stateDir)
	}
	plan, err := loadRuntimePersistedPlan(cfg, stateDir)
	if err != nil {
		return nil, nil, err
	}
	if cfg.provisionalResume.Record.PlanHash != plan.PlanHash || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance {
		return nil, nil, errors.New("retained runtime store inputs require the exact provisional successor")
	}
	return authenticatedRetainedRuntimeConfigManifest(cfg, stateDir, plan)
}
