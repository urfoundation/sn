// Provisional runtime resumes retain the exact approval across a driver hotfix;
// current release auditing remains strict and operational identity cannot drift.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Persist a fully budget-validated real plan before changing only release source.
func provisionalRuntimePlanFixture(t *testing.T) (*ResolvedConfig, *SetupPlan, string, cliOptions, []byte) {
	t.Helper()
	cfg, _, stateDir, options := provisionalResumeTestContext(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	options.PlanHash = plan.PlanHash
	changed := *cfg.Release
	changed.Repositories = maps.Clone(cfg.Release.Repositories)
	if changed.Repositories == nil {
		changed.Repositories = map[string]any{}
	}
	changed.Repositories["sn"] = map[string]any{"commit": strings.Repeat("ab", 20)}
	cfg.Release = &changed
	return cfg, plan, stateDir, options, raw
}

func TestProvisionalRuntimePlanRetainsApprovalAcrossReleaseHotfix(t *testing.T) {
	cfg, plan, stateDir, options, before := provisionalRuntimePlanFixture(t)
	if _, err := loadPersistedPlan(cfg, stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("strict release audit did not detect drift: %v", err)
	}
	for _, command := range []string{"resume", "scenario"} {
		retained, err := loadInvocationPlan(cfg, stateDir, command, options)
		if err != nil {
			t.Fatalf("%s lost retained approval after hotfix: %v", command, err)
		}
		if retained.PlanHash != plan.PlanHash || retained.ReleaseLockHash != plan.ReleaseLockHash || retained.ConfigHash != plan.ConfigHash || retained.ResolvedInputsHash != plan.ResolvedInputsHash {
			t.Fatal("runtime substituted the original plan/config/release identity")
		}
		if err := prepareProvisionalResume(t.Context(), cfg, stateDir, command, options, retained); err != nil {
			t.Fatal(err)
		}
		if cfg.provisionalResume.Record.ReleaseLockHash != plan.ReleaseLockHash || cfg.provisionalResume.Record.Driver != cfg.provisionalResume.Driver {
			t.Fatal("provenance did not distinguish retained release from actual driver")
		}
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retained plan was rewritten", err)
	}
	for _, name := range []string{"journal.jsonl", "deployment.lock", "supervisor.state.json"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime plan admission created %s", name)
		}
	}
}

func TestProvisionalRuntimePlanRejectsOperationalAndApprovalDrift(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ResolvedConfig, *cliOptions)
	}{
		{name: "config", change: func(cfg *ResolvedConfig, _ *cliOptions) { cfg.ConfigHash += "-changed" }},
		{name: "policy", change: func(cfg *ResolvedConfig, _ *cliOptions) { cfg.PolicyHash += "-changed" }},
		{name: "endpoint", change: func(cfg *ResolvedConfig, _ *cliOptions) { cfg.OperationalEVM += "/other" }},
		{name: "allowance", change: func(cfg *ResolvedConfig, _ *cliOptions) { cfg.MaximumTAORao++ }},
		{name: "approval", change: func(_ *ResolvedConfig, options *cliOptions) { options.PlanHash = "0x" + strings.Repeat("ff", 32) }},
		{name: "apply", change: func(_ *ResolvedConfig, options *cliOptions) { options.Apply = false }},
		{name: "provenance", change: func(cfg *ResolvedConfig, _ *cliOptions) { cfg.provisionalResume = nil }},
	}
	for _, test := range tests {
		cfg, _, stateDir, options, before := provisionalRuntimePlanFixture(t)
		test.change(cfg, &options)
		if _, err := loadInvocationPlan(cfg, stateDir, "resume", options); err == nil {
			t.Errorf("%s: changed identity was admitted", test.name)
		}
		after, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s: rejected input changed retained bytes", test.name)
		}
	}
}

func TestProvisionalRuntimePlanRejectsTamperedPersistedApproval(t *testing.T) {
	cfg, plan, stateDir, options, _ := provisionalRuntimePlanFixture(t)
	plan.Actions[0].IntentHash = "0x" + strings.Repeat("ef", 32)
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadInvocationPlan(cfg, stateDir, "resume", options); err == nil {
		t.Fatal("provisional loader accepted tampered wire")
	}
}
