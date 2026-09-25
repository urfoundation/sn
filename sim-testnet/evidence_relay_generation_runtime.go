//go:build linux || darwin

// A terminal continuation selects strict runtime bytes without replacing an
// activated generation's config, policy, keys, namespace or signed history.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	validatorcomponent "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

// The setup plan approves both the retained source reference and the exact
// derived bytes. Import creates this new immutable file before selecting it.
type evidenceRelayGenerationRuntime struct {
	ValidatorId uint64                                   `json:"validator_id"`
	Original    validatorcomponent.ReleaseEvidenceV2File `json:"original"`
	Config      validatorcomponent.ReleaseEvidenceV2File `json:"config"`
	Content     string                                   `json:"content"`
}

// Runtime identity comes from the current reviewed release, never from the
// submitted overlay. Clearing provisional flags grants no historical credit.
func captureEvidenceRelayGenerationRuntime(ctx context.Context, cfg *ResolvedConfig, plan *SetupPlan, owner policyRolloverValidatorHandoffV2) (*evidenceRelayGenerationRuntime, error) {
	if cfg == nil || cfg.Release == nil || plan == nil || provisionalResumeEnabled(cfg) || owner.ValidatorID == 0 {
		return nil, errors.New("strict generation runtime requires a current strict approval")
	}
	releaseHash, err := canonicalHashHex(cfg.Release)
	if err != nil || releaseHash != plan.ReleaseLockHash || cfg.ConfigHash != plan.ConfigHash || cfg.PolicyHash != plan.PolicyHash {
		return nil, errors.Join(errors.New("strict generation runtime changed its current release or config"), err)
	}
	if err := validateReviewedRuntimeIdentity(cfg.Release); err != nil {
		return nil, err
	}
	if err := validateRuntimeConfigIdentityPlan(cfg, plan); err != nil {
		return nil, err
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	selected, err := policyRolloverSourceRoleConfigV2(ctx, owner.Config, limit)
	if err != nil {
		return nil, err
	}
	if selected.ValidatorID != owner.ValidatorID || selected.DeploymentID != plan.DeploymentID || selected.StateDir != owner.StateDir || !reflect.DeepEqual(selected.EvidenceV2, owner.Evidence) || !reflect.DeepEqual(selected.SourceRolePredecessorV2, owner.SourceRolePredecessorV2) {
		return nil, errors.New("strict generation runtime lost its authenticated source owner")
	}
	selected.RuntimeSpec = cfg.Release.Runtime.SpecVersion
	selected.TransactionVersion = cfg.Release.Runtime.TransactionVersion
	selected.StateVersion = cfg.Release.Runtime.StateVersion
	selected.RuntimeCodeHash = cfg.Release.Runtime.CodeHash
	selected.RuntimeMetadataHash = cfg.Release.Runtime.MetadataHash
	selected.ProvisionalRuntimeCompatibility = ""
	selected.ProvisionalDeferClosedNativeInput = false
	if err := selected.Validate(); err != nil {
		return nil, err
	}
	raw, err := yaml.Marshal(selected)
	if err != nil {
		return nil, err
	}
	// History admission deliberately binds the sibling coordinator directory.
	// The original source-role config can live elsewhere and remains untouched.
	path := filepath.Join(filepath.Dir(owner.StateDir), "validator-strict-"+stringsTrim0x(releaseHash)+".yml")
	return &evidenceRelayGenerationRuntime{ValidatorId: owner.ValidatorID, Original: owner.Config, Config: policyRolloverFile(path, raw), Content: string(raw)}, ctx.Err()
}

// Re-derive every byte against detached current authority and the unchanged
// owner-signed source. Joint changes to a reference and payload still fail.
func validateEvidenceRelayGenerationRuntime(ctx context.Context, cfg *ResolvedConfig, plan *SetupPlan, owner policyRolloverValidatorHandoffV2, overlay evidenceRelayGenerationRuntime) error {
	expected, err := captureEvidenceRelayGenerationRuntime(ctx, cfg, plan, owner)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(*expected, overlay) {
		return fmt.Errorf("validator %d strict runtime differs from its exact reviewed projection", owner.ValidatorID)
	}
	return nil
}

// Selecting a continuation never writes a config. Only exact stopped import
// installs its bytes; readers require those same immutable bytes to exist.
func readEvidenceRelayGenerationRuntime(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, handoff *policyRolloverHandoffV2) (*policyRolloverHandoffV2, error) {
	if plan == nil || plan.EvidenceRelayContinuation == nil || plan.EvidenceRelayContinuation.ActiveGeneration == nil {
		return handoff, nil
	}
	generation := plan.EvidenceRelayContinuation.ActiveGeneration
	if err := generation.matchesHandoff(handoff); err != nil {
		return nil, err
	}
	selected := *handoff
	selected.Validators = append([]policyRolloverValidatorHandoffV2(nil), handoff.Validators...)
	for index, owner := range handoff.Validators {
		overlay := generation.Runtime[index]
		if err := validateEvidenceRelayGenerationRuntime(ctx, cfg, plan, owner, overlay); err != nil {
			return nil, err
		}
		raw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, overlay.Config, uint64(len(overlay.Content)))
		if err != nil || !bytes.Equal(raw, []byte(overlay.Content)) {
			return nil, errors.Join(errors.New("strict generation runtime file differs from the approved bytes"), err)
		}
		selected.Validators[index].Config = overlay.Config
	}
	return &selected, ctx.Err()
}
