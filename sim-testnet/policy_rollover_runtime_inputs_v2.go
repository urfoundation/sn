//go:build linux || darwin

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// The sealed runtime manifest retains the activated generation's original
// inputs. Later role-only configs have separate signed authority and cannot
// move an original config, hotkey seed, or client key out of that inventory.
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
	return addPolicyRolloverGenerationRuntimeInputsV2(ctx, cfg, stateDir, base, paths, policyRolloverSourceRoleIO())
}

// Authenticate both owners before enumerating the immutable generation. The
// optional overlay must remain valid even though its files are independently
// sealed and are deliberately absent from the earlier runtime manifest.
func addPolicyRolloverGenerationRuntimeInputsV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, paths map[string]os.FileMode, sourceRoleIo policyRolloverSourceRoleIOV2) error {
	handoff, err := readBasePolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
	if err != nil {
		return err
	}
	if handoff == nil {
		return nil
	}
	if _, err := readPolicyRolloverSourceRoleOverlayWithV2(ctx, cfg, stateDir, base, handoff, sourceRoleIo); err != nil {
		return err
	}
	pointer := filepath.Join(policyRolloverRoot(stateDir), "handoff.json")
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
