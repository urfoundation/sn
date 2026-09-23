// An initial read failure has no observation body, but still needs a durable
// explicit absence record so its authenticated successor can retain the run.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The scenario/recovery writer owns publication. Existing observation bytes
// remain untouched; a synced private marker is linked without replacing a file.
func ensurePreAcceptanceObservationLog(runDir string) (resultErr error) {
	path := filepath.Join(runDir, "observations.jsonl")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("pre-acceptance observation log is not a regular file")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info, err := os.Lstat(runDir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(errors.New("pre-acceptance observation directory is not an owned regular directory"), err)
	}
	file, err := os.CreateTemp(runDir, ".preacceptance-observations-")
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		resultErr = errors.Join(resultErr, os.Remove(file.Name()))
	}()
	if _, err := file.WriteString(preAcceptanceInterruptedObservationMarker); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(runDir)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// Only the authenticated recovery writer may fill this legacy absent artifact.
// No result, observed prefix or accepted interval can be reconstructed here.
func materializeMissingPreAcceptanceObservationLog(stateDir string, prior *scenarioCampaignAttempt) error {
	if prior == nil || prior.payload.AcceptanceBoundary != nil {
		return nil
	}
	runRelative := filepath.ToSlash(filepath.Join("runs", prior.payload.RunID))
	runDir := filepath.Join(stateDir, filepath.FromSlash(runRelative))
	if _, err := os.Lstat(filepath.Join(runDir, "observations.jsonl")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	result, _, err := readScenarioCampaignRecoveryResult(prior.cfg, stateDir, prior)
	if err != nil {
		return err
	}
	if result.CampaignStartHead != (ChainHead{}) || result.StartHead != (ChainHead{}) || result.EndHead != (ChainHead{}) || result.CampaignStartEpoch != 0 || result.StartEpoch != 0 || result.EndEpoch != 0 || result.AcceptanceWindow != nil || len(result.Faults) != 0 || result.LifecycleHandoff != nil {
		return errors.New("missing pre-acceptance observation log has recorded scenario progress")
	}
	initialFailure := false
	for _, assertion := range result.Assertions {
		if assertion.ObservationHash != "" {
			return errors.New("missing pre-acceptance observation log has a recorded observation hash")
		}
		initialFailure = initialFailure || assertion.ID == "initial_observation" && !assertion.Passed
	}
	if !initialFailure {
		return errors.New("missing pre-acceptance observation log has no terminal initial observation failure")
	}
	if _, err := os.Lstat(filepath.Join(runDir, scenarioCampaignStartFilename)); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errors.New("missing pre-acceptance observation log has a campaign-start marker"), err)
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, runRelative+"/"+processLogEvidenceFilename, maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	var logs processLogGateState
	if err := decodeStrictJSONBytes(raw, &logs); err != nil {
		return err
	}
	if logs.DeploymentID != prior.cfg.Config.Deployment.DeploymentID || logs.AcceptanceBoundary != nil {
		return errors.New("missing pre-acceptance observation log has a changed process-log identity or acceptance boundary")
	}
	if err := validatePersistedProcessLogGate(logs); err != nil {
		return fmt.Errorf("missing pre-acceptance observation log process evidence: %w", err)
	}
	return ensurePreAcceptanceObservationLog(runDir)
}
