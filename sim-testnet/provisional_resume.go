package main

// Explicit provisional testnet resume retains the approved plan and its
// locally authenticated verified receipts. It does not qualify the new driver
// as the locked release, or make a release acceptance claim.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type provisionalDriverProvenance struct {
	ExecutablePath   string                         `json:"executable_path"`
	ExecutableSHA256 string                         `json:"executable_sha256"`
	Build            releaseExecutableBuildIdentity `json:"build"`
}

type provisionalResumeRecord struct {
	Schema          string                      `json:"schema"`
	Provisional     bool                        `json:"provisional"`
	FinalAcceptance bool                        `json:"final_acceptance"`
	StartedAt       string                      `json:"started_at"`
	Command         string                      `json:"command"`
	Scenario        string                      `json:"scenario,omitempty"`
	DeploymentID    string                      `json:"deployment_id"`
	PlanHash        string                      `json:"plan_hash"`
	ConfigHash      string                      `json:"config_hash"`
	ReleaseLockHash string                      `json:"retained_release_lock_hash"`
	RetainedSNRepo  string                      `json:"retained_configuration_sn_repository"`
	Driver          provisionalDriverProvenance `json:"actual_driver"`
}

type provisionalResumeState struct {
	Driver     provisionalDriverProvenance
	Record     *provisionalResumeRecord
	RecordPath string
	RecordHash string
}

type provisionalScenarioProvenance struct {
	ResumeRecordHash string                         `json:"resume_record_hash"`
	ExecutableSHA256 string                         `json:"executable_sha256"`
	Build            releaseExecutableBuildIdentity `json:"build"`
}

func validateProvisionalResumeOptions(command string, options cliOptions) error {
	if !options.ProvisionalResume {
		return nil
	}
	if command != "resume" && command != "scenario" && command != "coordinator-repair" {
		return errors.New("--provisional-resume is valid only for resume, scenario or coordinator-repair")
	}
	if !options.Apply || !validCanonicalHashHex(options.PlanHash) {
		return errors.New("--provisional-resume requires --apply and the exact persisted --plan-hash")
	}
	return nil
}

// Persist unique provenance before the journal, host services, or transaction
// managers are opened. No approved plan, release input or used evidence is
// rewritten. A later failed attempt leaves an honest record of its invocation.
func prepareProvisionalResume(ctx context.Context, cfg *ResolvedConfig, stateDir, command string, options cliOptions, plan *SetupPlan) error {
	if err := validateProvisionalResumeOptions(command, options); err != nil {
		return err
	}
	if !options.ProvisionalResume {
		return nil
	}
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Release == nil || plan == nil || cfg.provisionalResume == nil {
		return errors.New("provisional resume requires authenticated executable provenance and the persisted plan")
	}
	if cfg.Config.Deployment.Network != "bittensor-testnet" || cfg.Config.Deployment.Subnet != "existing" || cfg.ChainID != testnetChainID || cfg.Public.Chain.ChainID != testnetChainID || !strings.EqualFold(cfg.Public.Chain.GenesisHash, testnetGenesis) || plan.ChainID != testnetChainID || !strings.EqualFold(plan.GenesisHash, testnetGenesis) {
		return errors.New("provisional resume is restricted to the existing pinned Bittensor testnet")
	}
	if err := requireApproved(true, options.PlanHash, plan.PlanHash); err != nil {
		return err
	}
	driver := cfg.provisionalResume.Driver
	if !releaseSHA256.MatchString(driver.ExecutableSHA256) || !releaseGitCommit.MatchString(driver.Build.Revision) || driver.Build.PackagePath != "github.com/urfoundation/sn/sim-testnet" || driver.Build.ModulePath != "github.com/urfoundation/sn" || !filepath.IsAbs(driver.ExecutablePath) {
		return errors.New("provisional resume actual driver provenance is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	record := &provisionalResumeRecord{
		Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, FinalAcceptance: false,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Command: command, Scenario: options.Name,
		DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: plan.PlanHash, ConfigHash: cfg.ConfigHash,
		ReleaseLockHash: plan.ReleaseLockHash, RetainedSNRepo: cfg.Repos.SN, Driver: driver,
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	root := filepath.Join(stateDir, "provisional-resumes")
	if err := ensurePrivateDir(root); err != nil {
		return err
	}
	if err := rejectFinalArtifactSymlinkComponents(stateDir, root); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, "invocation-")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "provenance.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(encoded)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr, ctx.Err()); err != nil {
		return err
	}
	// Flush the directory entry as well as the bytes before enabling reuse.
	for _, syncPath := range []string{dir, root, stateDir} {
		directory, err := os.Open(syncPath)
		if err != nil {
			return err
		}
		syncErr = directory.Sync()
		closeErr = directory.Close()
		if err := errors.Join(syncErr, closeErr, ctx.Err()); err != nil {
			return err
		}
	}
	cfg.provisionalResume.Record = record
	cfg.provisionalResume.RecordPath = path
	cfg.provisionalResume.RecordHash = bytesSHA256(encoded)
	fmt.Fprintf(os.Stderr, "sim-testnet: provisional testnet resume; final_acceptance=false; retained plan %s; actual driver SHA256 %s; provenance %s\n", plan.PlanHash, driver.ExecutableSHA256, path)
	return nil
}

func provisionalResumeEnabled(cfg *ResolvedConfig) bool {
	return cfg != nil && cfg.provisionalResume != nil && cfg.provisionalResume.Record != nil
}

func (e *Executor) authenticateProvisionalReceipt(action Action, entry JournalEntry) error {
	if !provisionalResumeEnabled(e.cfg) || e.plan == nil || e.cfg.provisionalResume.Record.PlanHash != e.plan.PlanHash || entry.Stage != StageVerified || entry.ActionID != action.ID || !actionAcceptsIntent(action, entry.IntentHash) {
		return errors.New("provisional receipt does not match the exact approved verified intent")
	}
	if _, err := e.readPersistedPostcondition(entry); err != nil {
		return fmt.Errorf("provisional resume persisted postcondition: %w", err)
	}
	return nil
}

func (e *Executor) verifyProvisionalActionHistory(ctx context.Context) error {
	count := 0
	for _, action := range e.plan.Actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, ok := e.verifiedActionEntryForScope(action, true)
		if !ok {
			continue
		}
		if err := e.authenticateProvisionalReceipt(action, entry); err != nil {
			return fmt.Errorf("action %s: %w", action.ID, err)
		}
		count++
	}
	fmt.Fprintf(os.Stderr, "sim-testnet: provisional resume authenticated %d verified local receipts; current topology readiness remains required\n", count)
	return nil
}

func applyProvisionalScenarioProvenance(cfg *ResolvedConfig, result *ScenarioResult) {
	if !provisionalResumeEnabled(cfg) || result == nil {
		return
	}
	finalAcceptance := false
	result.Provisional = true
	result.FinalAcceptance = &finalAcceptance
	result.ProvisionalDriver = &provisionalScenarioProvenance{
		ResumeRecordHash: cfg.provisionalResume.RecordHash,
		ExecutableSHA256: cfg.provisionalResume.Driver.ExecutableSHA256,
		Build:            cfg.provisionalResume.Driver.Build,
	}
}
