//go:build linux || darwin

// Real synthetic signed setup files pass through the renderer and the API's
// admission schema. Provisional staging never rewrites their authority source.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/server/controller"
	"gopkg.in/yaml.v3"
)

// Read the actual operator input consumed by reserved upload admission.
func readRuntimeReservedProvisionalTest(t *testing.T, stateDir string, operatorId int) controller.StReservedAttemptUploadConfig {
	t.Helper()
	path := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operatorId), "vault", "st.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Reserved *controller.StReservedAttemptUploadConfig `yaml:"testnet-reserved-attempt-upload"`
	}
	if err := yaml.Unmarshal(raw, &value); err != nil || value.Reserved == nil {
		t.Fatalf("read actual reserved staging input: %v", err)
	}
	return *value.Reserved
}

// The same authenticated setup must render a usable explicit local allowance,
// then return to ordinary complete discovery when provisional mode is removed.
func TestRuntimeProvisionalStagingRenderBindsRetainedAuthority(t *testing.T) {
	cfg, stateDir, roles := testRuntimeEvidenceLaunchTemplateRender(t, false)
	// The render fixture normally keeps roles in memory. This custody
	// preservation check also needs the real private store as its baseline.
	if err := saveRoleSecrets(filepath.Join(stateDir, "secrets", "roles.json"), roles); err != nil {
		t.Fatal(err)
	}
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	var contexts []validatorpkg.ReleaseEvidenceV2File
	paths := []string{"plan.json", "journal.jsonl", "secrets/roles.json", "evidence-v2-setup/prepared.json", "evidence-v2-setup/completed.json"}
	for _, source := range resolved.Config.ValidatorEvidenceV2 {
		for _, operator := range source.Evidence.Operators {
			contexts = append(contexts, operator.Context)
			for _, reference := range operator.Files() {
				paths = append(paths, reference.Path)
			}
		}
	}
	preservedKVs := map[string]string{}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(stateDir, filepath.FromSlash(path))
		}
		hash, err := fileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		preservedKVs[path] = hash
	}
	approved, err := canonicalHashHex(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	var ordinary []controller.StReservedAttemptUploadConfig
	for operatorId := 1; operatorId <= cfg.Config.Topology.Operators; operatorId++ {
		ordinary = append(ordinary, readRuntimeReservedProvisionalTest(t, stateDir, operatorId))
	}
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: plan.PlanHash, Provisional: true}}
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err != nil {
		t.Fatal(err)
	}
	for index, before := range ordinary {
		actual := readRuntimeReservedProvisionalTest(t, stateDir, index+1)
		if !actual.Admission.ProvisionalSeededDiscoveryOnly || !actual.Admission.ProvisionalRetainedContextAuthority || len(contexts) != 4 || !slices.Equal(actual.Admission.ActivationContexts, contexts) {
			t.Fatal("provisional renderer left retained validators behind complete discovery")
		}
		if err := actual.Admission.Validate(); err != nil {
			t.Fatalf("rendered retained staging cannot enter actual admission: %v", err)
		}
		actual.Admission.ProvisionalSeededDiscoveryOnly = before.Admission.ProvisionalSeededDiscoveryOnly
		actual.Admission.ProvisionalRetainedContextAuthority = before.Admission.ProvisionalRetainedContextAuthority
		actual.Admission.ActivationContexts = slices.Clone(before.Admission.ActivationContexts)
		actual.Admission.ProvisionalRuntimeCompatibility = before.Admission.ProvisionalRuntimeCompatibility
		actual.Admission.RuntimeObservationDir = before.Admission.RuntimeObservationDir
		if !reflect.DeepEqual(actual, before) {
			t.Fatal("provisional rendering changed capacity, deployment, runtime or routing")
		}
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatalf("provisional manifest rejected its exact rendered allowance: %v", err)
	}
	cfg.provisionalResume = nil
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "runtime reserved staging differs") {
		t.Fatalf("ordinary admission accepted provisional staging outputs: %v", err)
	}
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err != nil {
		t.Fatal(err)
	}
	for index, before := range ordinary {
		if actual := readRuntimeReservedProvisionalTest(t, stateDir, index+1); !reflect.DeepEqual(actual, before) {
			t.Fatal("ordinary rerender retained a provisional authority waiver")
		}
	}
	if actual, err := canonicalHashHex(cfg.Config); err != nil || actual != approved {
		t.Fatalf("rendering changed the approved static configuration: %v", err)
	}
	for path, before := range preservedKVs {
		if actual, err := fileSHA256(path); err != nil || actual != before {
			t.Fatalf("rendering changed plan, custody, receipt or signed history at %s: %v", path, err)
		}
	}
}

// Broken final-pair bytes or an unrelated approval cannot become a local
// allowance, and the API schema still rejects expanded or partial ownership.
func TestRuntimeProvisionalStagingRejectsChangedSourceAndAuthority(t *testing.T) {
	cfg, stateDir, roles := testRuntimeEvidenceLaunchTemplateRender(t, false)
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: plan.PlanHash, Provisional: true}}
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err != nil {
		t.Fatal(err)
	}
	actual := readRuntimeReservedProvisionalTest(t, stateDir, 1)
	for _, fault := range []string{"chain", "missing-context", "duplicate-context", "owners"} {
		candidate := actual.Admission
		candidate.ActivationContexts = slices.Clone(candidate.ActivationContexts)
		switch fault {
		case "chain":
			candidate.Deployment.ChainID++
		case "missing-context":
			candidate.ActivationContexts = candidate.ActivationContexts[:3]
		case "duplicate-context":
			candidate.ActivationContexts[3] = candidate.ActivationContexts[0]
		case "owners":
			candidate.MaximumOwners++
		}
		if err := candidate.Validate(); err == nil {
			t.Fatalf("retained staging admitted %s authority", fault)
		}
	}
	manifestBefore := readCampaignSuccessionFixtureBytes(t, runtimeConfigManifestPath(stateDir))
	cfg.provisionalResume.Record.PlanHash = "0x" + strings.Repeat("71", 32)
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err == nil || !strings.Contains(err.Error(), "exact non-accepting approval") {
		t.Fatalf("provisional render admitted a different approval: %v", err)
	}
	cfg.provisionalResume.Record.PlanHash = plan.PlanHash
	context := resolved.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].Context
	wire, err := os.ReadFile(context.Path)
	if err != nil || len(wire) == 0 {
		t.Fatalf("missing actual signed source: %v", err)
	}
	wire[0] ^= 1
	if err := os.WriteFile(context.Path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err == nil {
		t.Fatal("provisional renderer adopted changed retained context bytes")
	}
	if after := readCampaignSuccessionFixtureBytes(t, runtimeConfigManifestPath(stateDir)); !slices.Equal(after, manifestBefore) {
		t.Fatal("refused provisional rendering rewrote the existing manifest")
	}
}
