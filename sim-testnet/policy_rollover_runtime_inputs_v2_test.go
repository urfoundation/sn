//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// The signed role-only config is a second immutable owner, not a relocation
// of the generation already sealed in the runtime manifest. Descriptor proof
// cryptography is exercised separately by the validator's real native tests.
func TestPolicyRolloverRuntimeInputsRetainSealedGenerationAcrossRoleOverlay(t *testing.T) {
	g, handoff, sourceRoleIo := newPolicyRolloverSourceRoleTestV2(t)
	f := g.fixture
	before := map[string]os.FileMode{}
	if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, f.plan, before, sourceRoleIo); err != nil {
		t.Fatal(err)
	}
	if len(before) != 31 {
		t.Fatalf("original two-validator generation inventory = %d, want 31", len(before))
	}
	sealed := map[string]RuntimeConfigFile{}
	for relative, mode := range before {
		digest, observedMode, err := runtimeConfigFileDigest(filepath.Join(f.stateDir, filepath.FromSlash(relative)))
		if err != nil || observedMode != mode {
			t.Fatalf("original input %s unavailable: mode=%04o err=%v", relative, observedMode, err)
		}
		sealed[relative] = RuntimeConfigFile{Path: relative, SHA256: digest, Mode: fmt.Sprintf("%04o", mode)}
	}
	plan, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, handoff, sourceRoleIo)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, handoff, plan, sourceRoleIo, func(context.Context, *validatorcomponent.ReleaseConfig) error { return nil })
	if err != nil || selected.SourceRoleOverlay == nil {
		t.Fatalf("signed role-only activation failed: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		after := map[string]os.FileMode{}
		if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, f.plan, after, sourceRoleIo); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("role overlay moved sealed generation inputs: before=%v after=%v", before, after)
		}
		for relative, file := range sealed {
			if err := verifyRuntimeConfigManifestFile(f.cfg, f.stateDir, file, before[relative]); err != nil {
				t.Fatalf("retained generation input changed on retry %d: %v", attempt, err)
			}
		}
	}
	for index, owner := range handoff.Validators {
		root := policyRolloverGenerationRootV2(f.stateDir, owner.ValidatorID, handoff.Generation)
		for _, name := range []string{"hotkey.seed", "validator.yml", "state/operators/no-1/client.key", "state/operators/no-2/client.key"} {
			relative, err := filepath.Rel(f.stateDir, filepath.Join(root, filepath.FromSlash(name)))
			if err != nil || before[filepath.ToSlash(relative)] != 0o600 {
				t.Fatalf("generation lost private %s: %v", name, err)
			}
		}
		if selected.Validators[index].Config.Path == owner.Config.Path {
			t.Fatal("fixture failed to select a separately owned role config")
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(selected.Validators[index].Config.Path), "hotkey.seed")); !os.IsNotExist(err) {
			t.Fatalf("role overlay unexpectedly relocated native custody: %v", err)
		}
	}
	// The unchanged expected map still subjects original custody to the
	// ordinary manifest digest and mode check after role selection.
	seedPath := filepath.Join(policyRolloverGenerationRootV2(f.stateDir, 2, handoff.Generation), "hotkey.seed")
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(f.stateDir, seedPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func() error{
		func() error { return os.Remove(seedPath) },
		func() error { return os.WriteFile(seedPath, nil, 0o600) },
		func() error { return os.WriteFile(seedPath, []byte("changed synthetic seed"), 0o600) },
		func() error { return os.Chmod(seedPath, 0o644) },
	} {
		if err := change(); err != nil {
			t.Fatal(err)
		}
		if err := verifyRuntimeConfigManifestFile(f.cfg, f.stateDir, sealed[filepath.ToSlash(relative)], 0o600); err == nil {
			t.Fatal("role overlay bypassed original native custody authentication")
		}
		if err := atomicWrite(seedPath, seed, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Keeping the older manifest does not permit an invalid later owner to
	// hide behind it. Each selected reference must authenticate first.
	for _, path := range []string{selected.SourceRoleOverlay.Path, selected.Validators[1].Config.Path, selected.Validators[1].SourceRolePredecessorV2.Path} {
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(bytes.Clone(original), []byte("changed\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		paths := map[string]os.FileMode{"retained-sentinel": 0o600}
		if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, f.plan, paths, sourceRoleIo); err == nil {
			t.Fatalf("changed role-only reference accepted: %s", path)
		}
		if !reflect.DeepEqual(paths, map[string]os.FileMode{"retained-sentinel": 0o600}) {
			t.Fatal("failed overlay authentication partially changed the inventory")
		}
		if err := atomicWrite(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
