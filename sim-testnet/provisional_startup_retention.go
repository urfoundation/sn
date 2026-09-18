package main

// Provisional detached startup may leave an authenticated supervisor running
// while one child repairs itself. Strict launch and any ambiguous generation
// still use the ordinary failed-launch rollback.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// Reads and authenticates the live generation recorded by one state directory.
type liveRecordedSupervisorReader func(string) (*SupervisorState, error)

// Accepts an installed child inventory without requiring current readiness.
// A stopped child must still prove that this generation started it at least
// once; its supervisor can then apply the independent restart policy.
func validateInstalledSupervisorInventory(state SupervisorState, manifestHash string, specs []ProcessSpec) error {
	if state.Schema != "urnetwork-sim-supervisor-state-v1" || state.UpdatedAt == "" || state.ContractCleanupCutoff == "" || state.ManifestHash != manifestHash || len(state.Processes) != len(specs) {
		return errors.New("provisional supervisor installation identity is incomplete")
	}
	want := make(map[string]ProcessSpec, len(specs))
	for _, spec := range specs {
		if spec.ID == "" || want[spec.ID].ID != "" {
			return errors.New("provisional supervisor manifest has a duplicate or empty process identity")
		}
		want[spec.ID] = spec
	}
	seen := make(map[string]bool, len(state.Processes))
	for _, process := range state.Processes {
		spec, ok := want[process.ID]
		if !ok || seen[process.ID] || process.Role != spec.Role || process.Identity != spec.Identity {
			return fmt.Errorf("provisional supervisor process %q differs from the installed manifest", process.ID)
		}
		if process.PID < 0 || process.PID == 1 || process.Restarts < 0 || process.StartedAt == "" {
			return fmt.Errorf("provisional supervisor process %s has an invalid installed generation", process.ID)
		}
		if process.PID == 0 && (process.Healthy || process.ExitError == "") {
			return fmt.Errorf("provisional supervisor stopped process %s has no recoverable exit", process.ID)
		}
		seen[process.ID] = true
	}
	return nil
}

// Authenticates the exact manifest, service metadata, supervisor executable,
// generation, and installed child inventory. Child health is intentionally not
// an input: this boundary exists so that the supervisor can recover it.
func authenticateInstalledProvisionalSupervisor(stateDir string, want SupervisorFile, service *SupervisorService, readLive liveRecordedSupervisorReader) (*SupervisorState, error) {
	if service == nil || readLive == nil {
		return nil, errors.New("provisional supervisor installation readers are incomplete")
	}
	expectedServiceName, err := persistentSupervisorServiceName(want.DeploymentID)
	if err != nil {
		return nil, err
	}
	if service.Schema != "urnetwork-sim-supervisor-service-v1" || service.Name != expectedServiceName || !filepath.IsAbs(service.Unit) || !filepath.IsAbs(service.Binary) || filepath.Clean(service.StateDir) != filepath.Clean(stateDir) {
		return nil, errors.New("provisional supervisor service identity differs from the installed launch")
	}
	var installedService SupervisorService
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.service.json"), &installedService); err != nil {
		return nil, fmt.Errorf("read installed provisional supervisor service: %w", err)
	}
	wantServiceHash, err := canonicalHashHex(*service)
	if err != nil {
		return nil, err
	}
	installedServiceHash, err := canonicalHashHex(installedService)
	if err != nil {
		return nil, err
	}
	if installedServiceHash != wantServiceHash {
		return nil, errors.New("provisional supervisor service metadata changed after installation")
	}
	var installedManifest SupervisorFile
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.json"), &installedManifest); err != nil {
		return nil, fmt.Errorf("read installed provisional supervisor manifest: %w", err)
	}
	wantManifestHash, err := canonicalHashHex(want)
	if err != nil {
		return nil, err
	}
	installedManifestHash, err := canonicalHashHex(installedManifest)
	if err != nil {
		return nil, err
	}
	if installedManifestHash != wantManifestHash {
		return nil, errors.New("provisional supervisor manifest changed after installation")
	}
	live, err := readLive(stateDir)
	if err != nil {
		return nil, fmt.Errorf("read installed provisional supervisor generation: %w", err)
	}
	if live == nil {
		return nil, errors.New("installed provisional supervisor is not live")
	}
	if err := validateSupervisorGeneration(*live); err != nil {
		return nil, err
	}
	if err := validateInstalledSupervisorInventory(*live, wantManifestHash, want.Specs); err != nil {
		return nil, err
	}
	binaryHash, err := fileSHA256(fmt.Sprintf("/proc/%d/exe", live.SupervisorPID))
	if err != nil {
		return nil, fmt.Errorf("hash installed provisional supervisor executable: %w", err)
	}
	if binaryHash != want.BinaryHash {
		return nil, errors.New("installed provisional supervisor executable differs from its manifest")
	}
	return live, nil
}

// Transfers ownership of a recoverable provisional generation from launch to
// the persistent supervisor. Cancellation and any post-start mutation retain
// failed-launch rollback semantics.
func preserveRecoverableProvisionalStartup(ctx context.Context, cfg *ResolvedConfig, stateDir string, want SupervisorFile, service *SupervisorService, launchErr error, rollbackRequired bool, readLive liveRecordedSupervisorReader, readService supervisorServiceStatusReader) (bool, error) {
	if launchErr == nil || !provisionalResumeEnabled(cfg) || rollbackRequired {
		return false, nil
	}
	if ctx == nil || ctx.Err() != nil || errors.Is(launchErr, context.Canceled) || errors.Is(launchErr, context.DeadlineExceeded) {
		return false, nil
	}
	if cfg == nil || cfg.Config == nil || cfg.provisionalResume == nil || cfg.provisionalResume.Record == nil || cfg.provisionalResume.Record.DeploymentID != cfg.Config.Deployment.DeploymentID || want.DeploymentID != cfg.Config.Deployment.DeploymentID || !validCanonicalHashHex(cfg.provisionalResume.Record.PlanHash) {
		return false, errors.New("provisional supervisor approval identity is incomplete")
	}
	if readService == nil {
		return false, errors.New("provisional supervisor service status reader is missing")
	}
	installed, err := authenticateInstalledProvisionalSupervisor(stateDir, want, service, readLive)
	if err != nil {
		return false, err
	}
	status, err := readService(ctx, *service)
	if err != nil {
		return false, fmt.Errorf("authenticate installed provisional supervisor service: %w", err)
	}
	if status.terminal() {
		return false, fmt.Errorf("installed provisional supervisor service %s is terminal: active=%s sub=%s", service.Name, status.ActiveState, status.SubState)
	}
	current, err := authenticateInstalledProvisionalSupervisor(stateDir, want, service, readLive)
	if err != nil {
		return false, err
	}
	if current.SupervisorPID != installed.SupervisorPID || current.SupervisorStartTimeTicks != installed.SupervisorStartTimeTicks {
		return false, errors.New("installed provisional supervisor generation changed during handoff")
	}
	if ctx.Err() != nil {
		return false, nil
	}
	return true, nil
}
