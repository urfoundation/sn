//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// A later collection invocation has no launch flags. Recover the immutable
// child request from the recorded process spec, and retain that exact request
// beside the unmodified rendered YAML. The request authorizes only its named
// historical prefix and coordinator namespace.
func finalCaptureHistoryAdoptionV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, release *validatorpkg.ReleaseConfig, configBytes []byte) (*validatorpkg.ReleaseHistoryAdoptionV2, []byte, error) {
	if ctx == nil || cfg == nil || release == nil {
		return nil, nil, errors.New("final history adoption owner is absent")
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateRoot, "supervisor.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, nil, err
	}
	var supervisor SupervisorFile
	if err := decodeStrictJSONBytes(raw, &supervisor); err != nil {
		return nil, nil, err
	}
	if supervisor.DeploymentID != release.DeploymentID {
		return nil, nil, errors.New("final history supervisor changes deployment")
	}
	id := fmt.Sprintf("validator-%d", release.ValidatorID)
	var path, hash string
	matched := 0
	for _, spec := range supervisor.Specs {
		if spec.ID != id {
			continue
		}
		matched++
		if spec.Role != "validator" {
			return nil, nil, errors.New("final history process role differs")
		}
		for _, arg := range spec.Args {
			switch {
			case strings.HasPrefix(arg, "--strict-history-adoption="):
				if path != "" {
					return nil, nil, errors.New("final history request path is duplicated")
				}
				path = strings.TrimPrefix(arg, "--strict-history-adoption=")
			case strings.HasPrefix(arg, "--strict-history-adoption-sha256="):
				if hash != "" {
					return nil, nil, errors.New("final history request hash is duplicated")
				}
				hash = strings.TrimPrefix(arg, "--strict-history-adoption-sha256=")
			case strings.HasPrefix(arg, "--strict-history-adoption"):
				return nil, nil, errors.New("final history process uses an unrecognized request argument")
			}
		}
	}
	if matched != 1 || (path == "") != (hash == "") {
		return nil, nil, errors.New("final history process census or request pair differs")
	}
	if path == "" {
		return nil, nil, ctx.Err()
	}
	raw, err = readStrictHistoryAdoptionFile(stateRoot, path, strictHistoryAdoptionMaximumBytes)
	if err != nil {
		return nil, nil, err
	}
	request, err := validatorpkg.DecodeReleaseHistoryAdoptionV2(raw, hash)
	if err != nil {
		return nil, nil, err
	}
	plan, err := loadPersistedPlan(cfg, stateRoot)
	if err != nil {
		return nil, nil, err
	}
	if err := verifyFinalHistoryAdoptionV2(plan, stateRoot, release, configBytes, request); err != nil {
		return nil, nil, err
	}
	return request, raw, ctx.Err()
}

func verifyFinalHistoryAdoptionV2(plan *SetupPlan, stateRoot string, release *validatorpkg.ReleaseConfig, configBytes []byte, request *validatorpkg.ReleaseHistoryAdoptionV2) error {
	if plan == nil || release == nil || request == nil || plan.DeploymentID != release.DeploymentID || request.DeploymentID != plan.DeploymentID || request.ValidatorID != release.ValidatorID || request.ApprovedPlanHash != plan.PlanHash || request.SourcePlanHash == plan.PlanHash || !plan.allowedPlanHashes()[request.SourcePlanHash] || request.ConfigSHA256 != bytesSHA256(configBytes) {
		return errors.New("final history request differs from its exact approved plan or rendered config")
	}
	want := filepath.Join(stateRoot, "runtime", fmt.Sprintf("validator-%d", release.ValidatorID), "coordinator-state-v2")
	if request.CoordinatorStateDir != want || release.StateDir != filepath.Join(filepath.Dir(want), "state") {
		return errors.New("final history request changes the retained coordinator namespace")
	}
	return nil
}
