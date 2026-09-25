//go:build linux || darwin

// A strict history bundle keeps frozen witnesses separate from the two active
// writers. Only the latter receive permission for one fresh native bridge.
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Exact config bytes, path and source approval jointly select one namespace.
type strictHistoryConfigInput struct {
	path, source string
	content      []byte
}

// The original two-validator path stays byte-compatible. An approved active
// generation adds frozen capture receipts and selects its strict runtime files.
func strictHistoryConfigInputs(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, resolved *ResolvedConfig, roles *RoleSecrets, base map[string]any, source string) ([]strictHistoryConfigInput, []strictHistoryConfigInput, error) {
	var original []strictHistoryConfigInput
	for id := 1; id <= cfg.Config.Topology.Validators; id++ {
		raw, err := marshalRuntimeValidatorConfig(resolved, stateDir, roles, base, id)
		if err != nil {
			return nil, nil, err
		}
		original = append(original, strictHistoryConfigInput{path: filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", id), "validator.yml"), source: source, content: raw})
	}
	if plan.EvidenceRelayContinuation == nil || plan.EvidenceRelayContinuation.ActiveGeneration == nil {
		return original, nil, nil
	}
	handoff, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, plan)
	if err != nil || handoff == nil {
		return nil, nil, errors.Join(errors.New("strict history active generation is absent"), err)
	}
	var active []strictHistoryConfigInput
	for _, owner := range handoff.Validators {
		raw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, owner.Config, owner.Config.Bytes)
		if err != nil {
			return nil, nil, err
		}
		active = append(active, strictHistoryConfigInput{path: owner.Config.Path, source: handoff.SourcePlanHash, content: raw})
	}
	return active, original, nil
}
