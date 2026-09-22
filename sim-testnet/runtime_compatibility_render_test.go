//go:build linux || darwin

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProvisionalRuntimeCompatibilityPreviewMatchesActualRender(t *testing.T) {
	cfg, stateDir, roles := runtimeConfigNativeRenderFixtureTest(t)
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: plan.PlanHash, Provisional: true}}
	before, err := os.ReadFile(runtimeConfigManifestPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	var previewReads atomic.Int64
	previewCfg := countedRuntimePlanReadScopeTest(cfg, &previewReads)
	preview, err := previewProvisionalRuntimeConfigs(previewCfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(runtimeConfigManifestPath(stateDir))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("preview changed retained manifest: %v", err)
	}
	if len(preview) != cfg.Config.Topology.Operators+cfg.Config.Topology.Validators+1 {
		t.Fatal("preview inventory differs")
	}
	if count := previewReads.Load(); count != 1 {
		t.Fatalf("preview authenticated the same immutable plan %d times, want 1", count)
	}
	var renderReads atomic.Int64
	if err := RenderRuntimeConfigs(countedRuntimePlanReadScopeTest(cfg, &renderReads), stateDir, roles); err != nil {
		t.Fatal(err)
	}
	if count := renderReads.Load(); count != 1 {
		t.Fatalf("render authenticated the same immutable plan %d times, want 1", count)
	}
	if cfg.runtimePlanReads != nil {
		t.Fatal("render proof scope escaped into its caller")
	}
	for relative, expected := range preview {
		actual, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(relative)))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("preview differs from actual render at %s: %v", relative, err)
		}
	}
}

// Opt-in qualification utility: read retained state, write only a NEW scratch
// directory. The ordinary suite skips this; it never contacts any RPC endpoint.
func TestLiveProvisionalRuntimeCompatibilityPreview(t *testing.T) {
	if os.Getenv("SIM_TESTNET_RUNTIME_PREVIEW") != "1" {
		t.Skip("read-only preview requires explicit environment")
	}
	stateDir, output := os.Getenv("SIM_TESTNET_STATE_DIR"), os.Getenv("SIM_TESTNET_PREVIEW_DIR")
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(output) {
		t.Fatal("absolute retained state and new preview directory required")
	}
	stateDir, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		t.Fatal(err)
	}
	output = filepath.Join(parent, filepath.Base(output))
	if output == stateDir || strings.HasPrefix(output, stateDir+string(os.PathSeparator)) || strings.HasPrefix(stateDir, output+string(os.PathSeparator)) {
		t.Fatal("preview must be outside retained state")
	}
	var options LoadOptions
	if err := json.Unmarshal([]byte(os.Getenv("SIM_TESTNET_PREVIEW_LOAD_OPTIONS")), &options); err != nil {
		t.Fatal(err)
	}
	options.RequireSecrets = true
	cfg, err := LoadResolved(options)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = prepareOwnedRPCConfiguration(cfg, os.Getenv("SIM_TESTNET_PREVIEW_RPC_AUTHORITY"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, ConfigHash: cfg.ConfigHash, DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: plan.PlanHash}}
	roles, err := loadExistingProvisionalRoles(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewProvisionalRuntimeConfigs(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for relative := range preview {
		hash, err := fileSHA256(filepath.Join(stateDir, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		before[relative] = hash
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for relative, wire := range preview {
		path := filepath.Join(output, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, wire, 0600); err != nil {
			t.Fatal(err)
		}
		hashes[relative] = bytesSHA256(wire)
	}
	for relative, expected := range before {
		actual, err := fileSHA256(filepath.Join(stateDir, filepath.FromSlash(relative)))
		if err != nil || actual != expected {
			t.Fatalf("retained input changed during preview: %s %v", relative, err)
		}
	}
	receipt := map[string]any{"schema": "urnetwork-provisional-runtime-config-preview-v1", "state_dir": stateDir, "plan_hash": plan.PlanHash, "config_hash": cfg.ConfigHash, "files": hashes, "retained_before_after": before, "provisional": true, "final_acceptance": false, "rpc_calls": 0, "retained_state_written": false}
	wire, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "RESULT.json"), append(wire, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("preview receipt: %s", filepath.Join(output, "RESULT.json"))
}
