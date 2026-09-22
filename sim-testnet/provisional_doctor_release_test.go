// Retained approval and the actual recovery executable are separate
// authorities; a checkout hotfix must neither reset progress nor forge release
// qualification for source which differs from the original lock.
package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Use a real persisted plan and provenance record while observing a synthetic
// executable through the same authentication seam as public command dispatch.
func provisionalDoctorReleaseFixture(t *testing.T) (*ResolvedConfig, *doctorPlanBudget, releaseExecutableAuthenticator) {
	t.Helper()
	var retained *ReleaseLock
	cfg, plan, stateDir, options, _ := provisionalRuntimePlanFixtureWithConfig(t, "", func(cfg *ResolvedConfig) {
		retained = testReleaseLockFixture(t)
		cfg.Release = retained
	})
	cfg.Release = retained
	if err := prepareProvisionalResume(t.Context(), cfg, stateDir, "scenario", options, plan); err != nil {
		t.Fatal(err)
	}
	driver := cfg.provisionalResume.Driver
	authenticate := func(ctx context.Context, observed *ResolvedConfig, mode executableAttestationMode) error {
		if mode != executableAttestationProvisionalResume {
			return errors.New("unexpected executable assurance")
		}
		observed.provisionalResume = &provisionalResumeState{Driver: driver}
		return ctx.Err()
	}
	return cfg, &doctorPlanBudget{Plan: plan, StateDir: stateDir}, authenticate
}

// Reproduce the late doctor failure after an admitted recovery hotfix. The
// strict source mismatch stays explicit, while exact retained state passes.
func TestProvisionalDoctorRetainsSourceApprovalAcrossHotfix(t *testing.T) {
	cfg, approved, authenticate := provisionalDoctorReleaseFixture(t)
	before, err := os.ReadFile(filepath.Join(approved.StateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	completedPath := filepath.Join(approved.StateDir, "completed.json")
	completed := []byte("retained authenticated completed-work witness\n")
	if err := os.WriteFile(completedPath, completed, 0o600); err != nil {
		t.Fatal(err)
	}
	currentSourceCalls := 0
	validateCurrent := func(*ResolvedConfig) error {
		currentSourceCalls++
		return errors.New("release lock repositories.protocol_source_hash mismatch after producer-gate hotfix")
	}
	var report DoctorReport
	checkDoctorReleaseLock(t.Context(), &report, cfg, approved, validateCurrent, authenticate)
	if currentSourceCalls != 0 || len(report.Checks) != 2 || !report.Checks[0].Hard || !report.Checks[0].OK || report.Checks[1].Hard || report.Checks[1].OK || report.Checks[1].Name != "release-lock/current-source" || !strings.Contains(report.Checks[1].Detail, "deferred") {
		t.Fatalf("retained doctor restored strict qualification or hid its deferral: calls=%d checks=%+v", currentSourceCalls, report.Checks)
	}
	after, err := os.ReadFile(filepath.Join(approved.StateDir, "plan.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("source recovery changed the approved plan", err)
	}
	retained, err := os.ReadFile(completedPath)
	if err != nil || !bytes.Equal(completed, retained) {
		t.Fatal("source recovery lost completed work", err)
	}
	if cfg.provisionalResume.Record == nil || cfg.provisionalResume.Record.Driver != cfg.provisionalResume.Driver {
		t.Fatal("executable reauthentication replaced admitted provenance")
	}
}

// Setup checks its reviewed successor before activating it. The active plan
// cannot substitute for that archive or be rewritten by this readiness check.
func TestProvisionalDoctorSetupUsesExactReviewedSuccessor(t *testing.T) {
	cfg, approved, authenticate := provisionalDoctorReleaseFixture(t)
	options := cliOptions{Apply: true, ProvisionalResume: true, PlanHash: approved.Plan.PlanHash}
	if err := prepareProvisionalResume(t.Context(), cfg, approved.StateDir, "setup", options, approved.Plan); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(approved.StateDir, "plan.json")
	active, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(approved.StateDir, "plans", stringsTrim0x(approved.Plan.PlanHash)+".json")
	if err := os.Remove(archivePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := validateProvisionalDoctorReleaseLock(t.Context(), cfg, approved, authenticate); err == nil {
		t.Fatal("active plan substituted for the missing reviewed successor")
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, active, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateProvisionalDoctorReleaseLock(t.Context(), cfg, approved, authenticate); err != nil {
		t.Fatal("exact reviewed successor failed retained approval", err)
	}
	after, err := os.ReadFile(activePath)
	if err != nil || !bytes.Equal(active, after) {
		t.Fatal("readiness check activated or changed the reviewed successor", err)
	}
}

// Every bound input remains hard. Deferring current source qualification
// cannot turn a different lock, plan, record, network or driver into history.
func TestProvisionalDoctorRejectsRetainedAuthoritySubstitution(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ResolvedConfig, *doctorPlanBudget) error
	}{
		{name: "lock", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			cfg.Release.Repositories["protocol_source_hash"] = "sha256:" + strings.Repeat("db", 32)
			return nil
		}},
		{name: "approval", change: func(_ *ResolvedConfig, approved *doctorPlanBudget) error {
			approved.Plan.PlanHash = "0x" + strings.Repeat("eb", 32)
			return nil
		}},
		{name: "record plan", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			cfg.provisionalResume.Record.PlanHash = "0x" + strings.Repeat("ea", 32)
			return nil
		}},
		{name: "final acceptance", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			cfg.provisionalResume.Record.FinalAcceptance = true
			return nil
		}},
		{name: "read-only substitution", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			cfg.provisionalResume.Record.ReadOnly = true
			return nil
		}},
		{name: "driver", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			cfg.provisionalResume.Driver.ExecutableSHA256 = "sha256:" + strings.Repeat("fa", 32)
			return nil
		}},
		{name: "network", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			cfg.ChainID = 1
			return nil
		}},
		{name: "missing record", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			return os.Remove(cfg.provisionalResume.RecordPath)
		}},
		{name: "empty record", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			return os.WriteFile(cfg.provisionalResume.RecordPath, nil, 0o600)
		}},
		{name: "different record", change: func(cfg *ResolvedConfig, _ *doctorPlanBudget) error {
			return os.WriteFile(cfg.provisionalResume.RecordPath, []byte("{}\n"), 0o600)
		}},
		{name: "missing plan", change: func(_ *ResolvedConfig, approved *doctorPlanBudget) error {
			return os.Remove(filepath.Join(approved.StateDir, "plan.json"))
		}},
	}
	for _, test := range tests {
		cfg, approved, authenticate := provisionalDoctorReleaseFixture(t)
		if err := test.change(cfg, approved); err != nil {
			t.Fatal(err)
		}
		var report DoctorReport
		checkDoctorReleaseLock(t.Context(), &report, cfg, approved, nil, authenticate)
		if len(report.Checks) != 1 || !report.Checks[0].Hard || report.Checks[0].OK {
			t.Errorf("%s escaped the retained authority gate: %+v", test.name, report.Checks)
		}
	}
}

// Re-reading bytes and build identity is mandatory even when the original
// in-memory and durable provenance records still agree with each other.
func TestProvisionalDoctorRequiresCurrentExecutableObservation(t *testing.T) {
	for _, failure := range []string{"observer error", "different executable", "different revision", "absent observation"} {
		cfg, approved, _ := provisionalDoctorReleaseFixture(t)
		called := false
		authenticate := func(_ context.Context, actual *ResolvedConfig, _ executableAttestationMode) error {
			called = true
			if failure == "observer error" {
				return errors.New("synthetic executable read failure")
			}
			if failure == "absent observation" {
				return nil
			}
			driver := cfg.provisionalResume.Driver
			if failure == "different executable" {
				driver.ExecutableSHA256 = "sha256:" + strings.Repeat("ba", 32)
			} else {
				driver.Build.Revision = strings.Repeat("cb", 20)
			}
			actual.provisionalResume = &provisionalResumeState{Driver: driver}
			return nil
		}
		if err := validateProvisionalDoctorReleaseLock(t.Context(), cfg, approved, authenticate); err == nil || !called {
			t.Errorf("%s escaped executable authentication: called=%t err=%v", failure, called, err)
		}
	}
}

// Ordinary doctor continues to reject changed source before release. The
// provisional driver observer cannot be used implicitly to avoid that result.
func TestDoctorStrictReleaseSourceFailureRemainsBlocking(t *testing.T) {
	cfg, approved, _ := provisionalDoctorReleaseFixture(t)
	cfg.provisionalResume = nil
	calls := 0
	validateCurrent := func(*ResolvedConfig) error {
		calls++
		return errors.New("synthetic current protocol source mismatch")
	}
	var report DoctorReport
	checkDoctorReleaseLock(t.Context(), &report, cfg, approved, validateCurrent, nil)
	if calls != 1 || len(report.Checks) != 1 || !report.Checks[0].Hard || report.Checks[0].OK || !strings.Contains(report.Checks[0].Detail, "source mismatch") {
		t.Fatalf("strict source qualification was weakened: calls=%d checks=%+v", calls, report.Checks)
	}
}

// An explicit provisional flag alone is insufficient; no retained approval
// means a hard rejection with no qualification waiver recorded.
func TestProvisionalDoctorRequiresRetainedPlanForSourceDeferral(t *testing.T) {
	cfg, _, authenticate := provisionalDoctorReleaseFixture(t)
	var report DoctorReport
	checkDoctorReleaseLock(t.Context(), &report, cfg, nil, nil, authenticate)
	if len(report.Checks) != 1 || !report.Checks[0].Hard || report.Checks[0].OK {
		t.Fatalf("unbound provisional doctor deferred source qualification: %+v", report.Checks)
	}
}

// Source-only hotfixes cannot substitute generated ABI, runtime, compiler
// artifact or storage-layout identities consumed by fresh transaction paths.
func TestGeneratedReleaseBuildRejectsAdjacentArtifactDrift(t *testing.T) {
	lock := testReleaseLockFixture(t)
	if err := validateGeneratedReleaseBuild(lock); err != nil {
		t.Fatal("reviewed generated build failed", err)
	}
	for key := range generatedReleaseBuildObservation() {
		changed := *lock
		changed.EVMBuild = maps.Clone(lock.EVMBuild)
		changed.EVMBuild[key] = "changed-generated-artifact"
		if err := validateGeneratedReleaseBuild(&changed); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("changed generated build %s was admitted: %v", key, err)
		}
	}
}
