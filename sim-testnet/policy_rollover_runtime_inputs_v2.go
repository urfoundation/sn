//go:build linux || darwin

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// The normal runtime manifest includes the selected generation's exact inputs
// alongside preserved original inputs. Merely staging files changes nothing.
func addPolicyRolloverRuntimeInputsV2(cfg *ResolvedConfig, stateDir string, paths map[string]os.FileMode) error {
	ctx := context.Background()
	pointer := filepath.Join(policyRolloverRoot(stateDir), "handoff.json")
	_, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, pointer, 1)
	if validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return nil
	}
	base, err := loadRuntimePersistedPlan(cfg, stateDir)
	if err != nil {
		return err
	}
	handoff, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
	if err != nil {
		return err
	}
	if handoff == nil {
		return nil
	}
	files := []string{pointer, policyRolloverPlanPathV2(stateDir, handoff.Generation, handoff.CutoffEpoch), handoff.Identities.Path}
	for _, validator := range handoff.Validators {
		files = append(files, validator.Config.Path, filepath.Join(filepath.Dir(validator.Config.Path), "hotkey.seed"))
		for _, operator := range validator.Evidence.Operators {
			for _, file := range operator.Files() {
				files = append(files, file.Path)
			}
			files = append(files, filepath.Join(validator.ClientStateDir, "operators", fmt.Sprintf("no-%d", operator.NoID), "client.key"))
		}
	}
	for _, path := range files {
		if err := addRuntimeConfigPath(paths, stateDir, path, 0o600); err != nil {
			return err
		}
	}
	return nil
}
