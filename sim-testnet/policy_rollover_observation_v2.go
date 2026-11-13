//go:build linux || darwin

// A live observer selects only the activated, receipt-authenticated generation.
// Staged inputs never redirect a reader and malformed active inputs never fall back.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/urfoundation/sn/v2026/crv4"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

func readPolicyRolloverObservationV2(ctx context.Context, cfg *ResolvedConfig, stateDir string) (*policyRolloverHandoffV2, error) {
	_, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(policyRolloverRoot(stateDir), "handoff.json"), 1)
	if validatorpkg.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return nil, nil
	}
	plan, err := loadRuntimePersistedPlan(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	handoff, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, plan)
	if err != nil || handoff == nil {
		return nil, errors.Join(errors.New("active observation generation is unavailable"), err)
	}
	return handoff, nil
}

func policyRolloverObservationValidatorV2(handoff *policyRolloverHandoffV2, validatorID int) (*policyRolloverValidatorHandoffV2, error) {
	if handoff == nil {
		return nil, nil
	}
	if validatorID < 1 || validatorID > len(handoff.Validators) || handoff.Validators[validatorID-1].ValidatorID != uint64(validatorID) {
		return nil, errors.New("active observation generation lacks its exact validator")
	}
	return &handoff.Validators[validatorID-1], nil
}

// The authority and its disk namespace move together. Old proof counts and
// old seeds never contribute to a fresh VPK's completed-work census.
func loadScenarioPathAuthorityV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, validatorID int) (*finalOperatorPathAuthority, *policyRolloverValidatorHandoffV2, error) {
	handoff, err := readPolicyRolloverObservationV2(ctx, cfg, stateDir)
	if err != nil {
		return nil, nil, err
	}
	selected, err := policyRolloverObservationValidatorV2(handoff, validatorID)
	if err != nil {
		return nil, nil, err
	}
	if selected == nil {
		authority, err := loadFinalOperatorPathAuthority(cfg, stateDir, []uint64{uint64(validatorID)})
		return authority, nil, err
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, nil, err
	}
	public, err := validatorpkg.ReadReleaseEvidenceV2File(ctx, handoff.Identities, limit)
	if err != nil {
		return nil, nil, err
	}
	authority, err := decodeFinalOperatorPathAuthority(public, cfg.Config.Deployment.DeploymentID, cfg.Config.Topology.Validators, cfg.Config.Topology.Operators)
	if err != nil {
		return nil, nil, err
	}
	for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
		member := handoff.Members[(validatorID-1)*cfg.Config.Topology.Operators+noID-1]
		key := authority.keysByValidator[uint64(validatorID)][uint64(noID)]
		if member.ValidatorId != uint64(validatorID) || member.NoId != uint64(noID) || !bytes.Equal(key, member.Activation.VPK[:]) {
			return nil, nil, errors.New("active public path authority differs from signed activation")
		}
		seed, err := crv4.LoadRawSeedFile(filepath.Join(selected.ClientStateDir, "operators", fmt.Sprintf("no-%d", noID), "client.key"))
		if err != nil {
			return nil, nil, err
		}
		if !bytes.Equal(ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey), key) {
			return nil, nil, errors.New("active path seed differs from its approved public role")
		}
	}
	return authority, selected, nil
}

// Terminal readers authenticate every configured seed under the same public
// authority. The archived semantic verifier still owns independent acceptance.
func loadFinalOperatorPathAuthorityV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, validatorIDs []uint64) (*finalOperatorPathAuthority, error) {
	if cfg == nil || cfg.Config == nil || len(validatorIDs) == 0 {
		return nil, errors.New("terminal path authority has no validator census")
	}
	var authority *finalOperatorPathAuthority
	seen := map[uint64]bool{}
	for _, id := range validatorIDs {
		if id == 0 || id > uint64(cfg.Config.Topology.Validators) || seen[id] {
			return nil, errors.New("terminal path authority has an invalid validator census")
		}
		seen[id] = true
		next, _, err := loadScenarioPathAuthorityV2(ctx, cfg, stateDir, int(id))
		if err != nil {
			return nil, err
		}
		if authority != nil && !bytes.Equal(authority.publicBytes, next.publicBytes) {
			return nil, errors.New("terminal path authority changed during its complete census")
		}
		authority = next
	}
	return authority, nil
}
