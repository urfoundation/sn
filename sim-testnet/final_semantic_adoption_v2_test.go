//go:build linux || darwin

package main

import (
	"path/filepath"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
)

func TestFinalCaptureV2AdoptionRequiresApprovedOriginalConfigAndNamespace(t *testing.T) {
	root := t.TempDir()
	raw := []byte("the exact original rendered configuration\n")
	plan := &SetupPlan{DeploymentID: "retained", PlanHash: finalFleetGenerationTestHash(81), PriorPlanHashes: []string{finalFleetGenerationTestHash(80)}}
	release := &validatorpkg.ReleaseConfig{DeploymentID: plan.DeploymentID, ValidatorID: 1, StateDir: filepath.Join(root, "runtime", "validator-1", "state")}
	request := &validatorpkg.ReleaseHistoryAdoptionV2{DeploymentID: plan.DeploymentID, ValidatorID: 1, ApprovedPlanHash: plan.PlanHash, SourcePlanHash: plan.PriorPlanHashes[0], ConfigSHA256: bytesSHA256(raw), CoordinatorStateDir: filepath.Join(root, "runtime", "validator-1", "coordinator-state-v2")}
	if err := verifyFinalHistoryAdoptionV2(plan, root, release, raw, request); err != nil {
		t.Fatal(err)
	}
	original := *release
	for _, test := range []struct {
		name   string
		change func(*validatorpkg.ReleaseHistoryAdoptionV2)
	}{
		{"unapproved source", func(v *validatorpkg.ReleaseHistoryAdoptionV2) { v.SourcePlanHash = finalFleetGenerationTestHash(79) }},
		{"previous launch approval", func(v *validatorpkg.ReleaseHistoryAdoptionV2) { v.ApprovedPlanHash = plan.PriorPlanHashes[0] }},
		{"replacement config", func(v *validatorpkg.ReleaseHistoryAdoptionV2) {
			v.ConfigSHA256 = bytesSHA256(append(append([]byte(nil), raw...), '\n'))
		}},
		{"other validator namespace", func(v *validatorpkg.ReleaseHistoryAdoptionV2) {
			v.CoordinatorStateDir = filepath.Join(root, "runtime", "validator-2", "coordinator-state-v2")
		}},
		{"fresh namespace", func(v *validatorpkg.ReleaseHistoryAdoptionV2) { v.CoordinatorStateDir = release.StateDir }},
		{"other identity", func(v *validatorpkg.ReleaseHistoryAdoptionV2) { v.ValidatorID++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := *request
			test.change(&changed)
			if err := verifyFinalHistoryAdoptionV2(plan, root, release, raw, &changed); err == nil {
				t.Fatal("accepted changed original adoption authority")
			}
			if release.StateDir != original.StateDir || release.ValidatorID != original.ValidatorID {
				t.Fatal("admission mutated the original rendered config")
			}
		})
	}
}
