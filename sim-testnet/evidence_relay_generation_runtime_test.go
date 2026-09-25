//go:build linux || darwin

// Real activated generation files exercise exact runtime projection and
// detached current authority. No live configuration or private input is used.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	validatorcomponent "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

// A source-role config lives outside the active namespace. The derived strict
// file must select the original active history while changing only runtime pins.
func TestEvidenceRelayGenerationRuntimePreservesActiveNamespace(t *testing.T) {
	g, _, handoff, _ := newPolicyRolloverHandoffTestV2(t)
	f := g.fixture
	plan := *f.plan
	plan.PolicyHash = f.cfg.PolicyHash
	var err error
	plan.ReleaseLockHash, err = canonicalHashHex(f.cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	owner := handoff.Validators[0]
	before, err := os.ReadFile(owner.Config.Path)
	if err != nil {
		t.Fatal(err)
	}
	var source validatorcomponent.ReleaseConfig
	if err := yaml.Unmarshal(before, &source); err != nil {
		t.Fatal(err)
	}
	source.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
	source.ProvisionalDeferClosedNativeInput = true
	raw, err := yaml.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(f.stateDir, "synthetic-role-overlay", "validator.yml")
	if err := atomicWrite(sourcePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	owner.Config = policyRolloverFile(sourcePath, raw)
	overlay, err := captureEvidenceRelayGenerationRuntime(t.Context(), f.cfg, &plan, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayGenerationRuntime(t.Context(), f.cfg, &plan, owner, *overlay); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(overlay.Config.Path); !os.IsNotExist(err) {
		t.Fatal("capture wrote its selecting runtime file", err)
	}
	var selected validatorcomponent.ReleaseConfig
	if err := yaml.Unmarshal([]byte(overlay.Content), &selected); err != nil {
		t.Fatal(err)
	}
	if selected.StateDir != owner.StateDir || selected.ProvisionalRuntimeCompatibility != "" || selected.ProvisionalDeferClosedNativeInput || filepath.Join(filepath.Dir(overlay.Config.Path), "coordinator-state-v2") != owner.StateDir {
		t.Fatal("strict projection replaced the active namespace or kept provisional authority")
	}
	selected.RuntimeSpec, selected.TransactionVersion, selected.StateVersion = source.RuntimeSpec, source.TransactionVersion, source.StateVersion
	selected.RuntimeCodeHash, selected.RuntimeMetadataHash = source.RuntimeCodeHash, source.RuntimeMetadataHash
	selected.ProvisionalRuntimeCompatibility, selected.ProvisionalDeferClosedNativeInput = source.ProvisionalRuntimeCompatibility, source.ProvisionalDeferClosedNativeInput
	if !reflect.DeepEqual(selected, source) {
		t.Fatal("runtime projection changed policy, custody, bounds or inputs")
	}
	if err := ensurePrivateDir(owner.StateDir); err != nil {
		t.Fatal(err)
	}
	request, err := validatorcomponent.CaptureReleaseHistoryAdoptionV2(t.Context(), overlay.Config.Path, []byte(overlay.Content), common.Hash{0xcd}.Hex(), plan.PlanHash, 90)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatorcomponent.CheckReleaseHistoryAdoptionV2Source(t.Context(), overlay.Config.Path, []byte(overlay.Content), request, bytesSHA256(request)); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string][]byte{handoff.Validators[0].Config.Path: before, sourcePath: raw} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("strict projection changed retained source bytes", err)
		}
	}
}

// An attacker can rehash the proposed payload, but cannot change the detached
// reviewed runtime, original source reference or any non-runtime config field.
func TestEvidenceRelayGenerationRuntimeRejectsJointSubstitution(t *testing.T) {
	g, _, handoff, _ := newPolicyRolloverHandoffTestV2(t)
	f := g.fixture
	plan := *f.plan
	plan.PolicyHash = f.cfg.PolicyHash
	plan.ReleaseLockHash, _ = canonicalHashHex(f.cfg.Release)
	owner := handoff.Validators[0]
	original, err := captureEvidenceRelayGenerationRuntime(t.Context(), f.cfg, &plan, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*evidenceRelayGenerationRuntime)
	}{
		{name: "runtime", change: func(value *evidenceRelayGenerationRuntime) {
			value.Content += "# replaced\n"
			value.Config = policyRolloverFile(value.Config.Path, []byte(value.Content))
		}},
		{name: "original", change: func(value *evidenceRelayGenerationRuntime) { value.Original.Path += ".other" }},
		{name: "namespace", change: func(value *evidenceRelayGenerationRuntime) {
			value.Config.Path = filepath.Join(f.stateDir, "other", "validator.yml")
		}},
		{name: "owner", change: func(value *evidenceRelayGenerationRuntime) { value.ValidatorId++ }},
	} {
		changed := *original
		test.change(&changed)
		if err := validateEvidenceRelayGenerationRuntime(t.Context(), f.cfg, &plan, owner, changed); err == nil {
			t.Fatalf("accepted %s replacement", test.name)
		}
	}
	for _, test := range []struct {
		name   string
		change func(*validatorcomponent.ReleaseConfig)
	}{
		{name: "hotkey", change: func(value *validatorcomponent.ReleaseConfig) { value.HotkeySeedFile += ".other" }},
		{name: "coordinator", change: func(value *validatorcomponent.ReleaseConfig) { value.StateDir += ".other" }},
		{name: "capacity", change: func(value *validatorcomponent.ReleaseConfig) { value.EvidenceV2.Bounds.Disk.MaxRecordCount++ }},
		{name: "provisional", change: func(value *validatorcomponent.ReleaseConfig) {
			value.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
		}},
		{name: "code", change: func(value *validatorcomponent.ReleaseConfig) { value.RuntimeCodeHash = common.Hash{0xde}.Hex() }},
	} {
		var value validatorcomponent.ReleaseConfig
		if err := yaml.Unmarshal([]byte(original.Content), &value); err != nil {
			t.Fatal(err)
		}
		test.change(&value)
		raw, err := yaml.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		changed := *original
		changed.Content, changed.Config = string(raw), policyRolloverFile(changed.Config.Path, raw)
		if err := validateEvidenceRelayGenerationRuntime(t.Context(), f.cfg, &plan, owner, changed); err == nil {
			t.Fatalf("accepted jointly rehashed %s", test.name)
		}
	}
	changed := plan
	changed.ReleaseLockHash = "0x" + string(bytes.Repeat([]byte("11"), 32))
	if _, err := captureEvidenceRelayGenerationRuntime(t.Context(), f.cfg, &changed, owner); err == nil {
		t.Fatal("overlay accepted a foreign current release")
	}
	if _, err := os.Stat(original.Config.Path); !os.IsNotExist(err) {
		t.Fatal("failed review wrote a runtime config")
	}
}

// Interrupted import leaves only inert immutable config files. Selection
// requires every file and retries publish exactly the same approved bytes.
func TestEvidenceRelayGenerationRuntimeImportSelectsOnlyCompleteInputs(t *testing.T) {
	g, rollover, handoff, journal := newPolicyRolloverHandoffTestV2(t)
	f := g.fixture
	activatePolicyRolloverHandoffTestV2(t, g, rollover, handoff, journal)
	handoff, err := readPolicyRolloverSourceHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	plan := *f.plan
	plan.PolicyHash = f.cfg.PolicyHash
	plan.ReleaseLockHash, _ = canonicalHashHex(f.cfg.Release)
	generation := &evidenceRelayActiveGeneration{Generation: handoff.Generation, RolloverPlanHash: handoff.PlanHash, SourcePlanHash: handoff.SourcePlanHash, HandoffSha256: handoff.sourceSHA256, CutoffEpoch: handoff.CutoffEpoch, FirstFullEpoch: handoff.FirstFullEpoch, SourceRoleOverlay: handoff.SourceRoleOverlay}
	plan.EvidenceRelayContinuation = &EvidenceRelayContinuation{ActiveGeneration: generation}
	for index, owner := range handoff.Validators {
		runtime, err := captureEvidenceRelayGenerationRuntime(t.Context(), f.cfg, &plan, owner)
		if err != nil {
			t.Fatal(err)
		}
		generation.Runtime = append(generation.Runtime, *runtime)
		for member := 0; member < 2; member++ {
			generation.Sources = append(generation.Sources, EvidenceRelayContinuationSource{ValidatorID: owner.ValidatorID, CoordinatorStateDir: owner.StateDir, Activation: handoff.Members[index*2+member].Activation})
		}
	}
	if _, err := readEvidenceRelayGenerationRuntime(t.Context(), f.cfg, f.stateDir, &plan, handoff); err == nil {
		t.Fatal("uninstalled review selected runtime files")
	}
	first := generation.Runtime[0]
	if _, err := validatorcomponent.WriteReleaseEvidenceV2File(t.Context(), first.Config.Path, []byte(first.Content), first.Config.Bytes); err != nil {
		t.Fatal(err)
	}
	if _, err := readEvidenceRelayGenerationRuntime(t.Context(), f.cfg, f.stateDir, &plan, handoff); err == nil {
		t.Fatal("partial import selected a generation")
	}
	if err := installEvidenceRelayGenerationRuntime(t.Context(), &plan); err != nil {
		t.Fatal(err)
	}
	if err := installEvidenceRelayGenerationRuntime(t.Context(), &plan); err != nil {
		t.Fatal("exact interrupted import was not idempotent", err)
	}
	selected, err := readEvidenceRelayGenerationRuntime(t.Context(), f.cfg, f.stateDir, &plan, handoff)
	if err != nil {
		t.Fatal(err)
	}
	for index, owner := range selected.Validators {
		if owner.Config != generation.Runtime[index].Config || owner.StateDir != handoff.Validators[index].StateDir || !reflect.DeepEqual(owner.Evidence, handoff.Validators[index].Evidence) {
			t.Fatal("runtime selection replaced active evidence or namespace")
		}
	}
	changed := *handoff
	changed.CutoffEpoch++
	if _, err := readEvidenceRelayGenerationRuntime(t.Context(), f.cfg, f.stateDir, &plan, &changed); err == nil {
		t.Fatal("same runtime accepted another cutoff")
	}
	for path, before := range g.original {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("runtime import changed a frozen input", err)
		}
	}
}
