// Read-only provisional diagnostics use the same retained approval and runtime
// capability admission as continuation. Their observations grant no acceptance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Admit one exact retained plan before observing a successor runtime. Only
// immutable invocation provenance is written; deployment inputs and journals
// remain untouched, and the caller keeps its original strict configuration.
func prepareProvisionalDoctor(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) (*ResolvedConfig, error) {
	return prepareProvisionalRetainedReader(ctx, cfg, stateDir, "doctor", options)
}

// A recovery driver is authenticated separately from the release which
// installed the retained state. Source qualification remains a final gate;
// invoking the broad doctor must not silently restore it as a startup gate.
func checkDoctorReleaseLock(ctx context.Context, report *DoctorReport, cfg *ResolvedConfig, approved *doctorPlanBudget, validateCurrent func(*ResolvedConfig) error, authenticate releaseExecutableAuthenticator) {
	if !provisionalResumeEnabled(cfg) {
		if validateCurrent == nil {
			report.add("release-lock", true, errors.New("current release source validator is unavailable"), "")
			return
		}
		report.add("release-lock", true, validateCurrent(cfg), cfg.Release.Release)
		return
	}
	err := validateProvisionalDoctorReleaseLock(ctx, cfg, approved, authenticate)
	detail := "retained approval and actual recovery executable; provisional=true final_acceptance=false"
	report.add("release-lock", true, err, detail)
	if err == nil {
		report.add("release-lock/current-source", false, errors.New("deferred: current source qualification remains required for final acceptance"), "retained release identity was not replaced or promoted")
	}
}

// Retain the exact approved lock, operational inputs and durable invocation
// record. Reobserve the running executable so a copied record, changed binary,
// or newly edited lock cannot authorize continuation. Generated contract bytes
// stay bound even though source-only hotfix qualification is deferred.
func validateProvisionalDoctorReleaseLock(ctx context.Context, cfg *ResolvedConfig, approved *doctorPlanBudget, authenticate releaseExecutableAuthenticator) error {
	if ctx == nil || cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Release == nil || approved == nil || approved.Plan == nil || !filepath.IsAbs(approved.StateDir) || authenticate == nil || !provisionalResumeEnabled(cfg) {
		return errors.New("provisional doctor retained release authority is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.Config.Deployment.Network != "bittensor-testnet" || cfg.Config.Deployment.Subnet != "existing" || cfg.ChainID != testnetChainID || cfg.Public.Chain.ChainID != testnetChainID || !strings.EqualFold(cfg.Public.Chain.GenesisHash, testnetGenesis) {
		return errors.New("provisional doctor retained release is restricted to the existing testnet")
	}
	invocation, record := cfg.provisionalResume, cfg.provisionalResume.Record
	readOnly := record.ReadOnly
	_, authorityErr := provisionalReviewedPlan(record.Command, readOnly)
	if authorityErr != nil || cfg.readOnlyAudit != readOnly || record.Schema != "urnetwork-sim-provisional-resume-v1" || !record.Provisional || record.FinalAcceptance || record.ConfigHash != cfg.ConfigHash || record.DeploymentID != cfg.Config.Deployment.DeploymentID || record.RetainedSNRepo != cfg.Repos.SN || record.Driver != invocation.Driver || !releaseSHA256.MatchString(invocation.RecordHash) {
		return errors.New("provisional doctor lost its exact non-accepting invocation authority")
	}
	options := cliOptions{
		Apply: !readOnly, ProvisionalResume: true, PlanHash: record.PlanHash,
		Name: record.Scenario, OwnedRPCAuthority: cfg.ownedRPCAuthority,
	}
	plan, err := loadInvocationPlan(cfg, approved.StateDir, record.Command, options)
	if err != nil {
		return err
	}
	if !evidenceRelayContinuationSameJSON(plan, approved.Plan) || plan.ReleaseLockHash != record.ReleaseLockHash {
		return errors.New("provisional doctor approval differs from the retained plan")
	}
	if err := validateReleaseLockStatic(cfg.Release); err != nil {
		return err
	}
	lockHash, err := canonicalHashHex(cfg.Release)
	if err != nil || lockHash != record.ReleaseLockHash {
		return errors.Join(errors.New("provisional doctor release lock differs from the exact retained approval"), err)
	}
	if err := verifyFinalReleaseLockRuntimeBuild(cfg.Release); err != nil {
		return err
	}
	if err := validateGeneratedReleaseBuild(cfg.Release); err != nil {
		return err
	}
	if !filepath.IsAbs(invocation.RecordPath) || filepath.Clean(invocation.RecordPath) != invocation.RecordPath {
		return errors.New("provisional doctor provenance path is not canonical")
	}
	relative, err := filepath.Rel(approved.StateDir, invocation.RecordPath)
	if err != nil || !strings.HasPrefix(relative, "provisional-resumes"+string(filepath.Separator)) || filepath.Base(relative) != "provenance.json" {
		return errors.Join(errors.New("provisional doctor provenance is outside retained invocation history"), err)
	}
	raw, err := readValidatorEvidenceHistoricalFile(approved.StateDir, filepath.ToSlash(relative), maximumCampaignEvidenceRawFileBytes)
	if err != nil || bytesSHA256(raw) != invocation.RecordHash {
		return errors.Join(errors.New("provisional doctor provenance bytes changed"), err)
	}
	var persisted provisionalResumeRecord
	if err := json.Unmarshal(raw, &persisted); err != nil || persisted != *record {
		return errors.Join(errors.New("provisional doctor provenance identity changed"), err)
	}
	actual := *cfg
	actual.provisionalResume = nil
	if err := authenticate(ctx, &actual, executableAttestationProvisionalResume); err != nil {
		return fmt.Errorf("reauthenticate provisional doctor executable: %w", err)
	}
	if actual.provisionalResume == nil || actual.provisionalResume.Driver != record.Driver {
		return errors.New("provisional doctor running executable differs from the retained invocation")
	}
	return ctx.Err()
}
