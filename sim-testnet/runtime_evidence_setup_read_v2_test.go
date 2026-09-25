//go:build linux || darwin

// Missing discovery receipts and incomplete immutable setup are different
// states. Required runtime, revision and final evidence keep failing closed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// Both retained receipt names classify empty and missing without accepting
// malformed, noncanonical or otherwise unreadable evidence as optional.
func TestRuntimeEvidenceSetupReadClassifiesMissingEmptyAndMalformed(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"prepared.json", "completed.json"} {
		for _, test := range []struct {
			name    string
			encoded []byte
			missing bool
			empty   bool
			valid   bool
		}{
			{name: "missing", missing: true},
			{name: "empty", empty: true},
			{name: "malformed", encoded: []byte("{broken\n")},
			{name: "unknown-field", encoded: []byte("{\"schema\":\"synthetic\",\"extra\":true}\n")},
			{name: "noncanonical", encoded: []byte("{ \"schema\":\"synthetic\"}\n")},
			{name: "present", encoded: []byte("{\"schema\":\"synthetic\"}\n"), valid: true},
		} {
			path := filepath.Join(t.TempDir(), name)
			if !test.missing {
				if err := os.WriteFile(path, test.encoded, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var value struct {
				Schema string `json:"schema"`
			}
			encoded, err := readRuntimeEvidenceSetupV2(t.Context(), path, 1024, &value)
			if (err == nil) != test.valid || errors.Is(err, errRuntimeEvidenceSetupEmpty) != test.empty || validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) != test.missing {
				t.Fatalf("%s %s has wrong setup state: %v", name, test.name, err)
			}
			if test.valid && !bytes.Equal(encoded, test.encoded) || !test.valid && encoded != nil {
				t.Fatalf("%s %s exposed unexpected receipt bytes", name, test.name)
			}
		}
	}
}

// Filesystem failures are never optional receipt absence. The check uses
// synthetic directories and links rather than permission behavior under sudo.
func TestRuntimeEvidenceSetupReadRejectsNonregularAndOversizedReceipts(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"prepared.json", "completed.json"} {
		for _, fault := range []string{"directory", "symlink", "dangling-symlink", "parent-file", "oversized"} {
			root := t.TempDir()
			path := filepath.Join(root, "setup", name)
			if fault == "parent-file" {
				if err := os.WriteFile(filepath.Dir(path), []byte("synthetic file"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "directory":
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
				case "symlink", "dangling-symlink":
					target := filepath.Join(root, "synthetic-target.json")
					if fault == "symlink" {
						if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				case "oversized":
					if err := os.WriteFile(path, bytes.Repeat([]byte{' '}, 1025), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			var value struct{}
			encoded, err := readRuntimeEvidenceSetupV2(t.Context(), path, 1024, &value)
			if err == nil || encoded != nil || errors.Is(err, errRuntimeEvidenceSetupEmpty) || validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
				t.Fatalf("%s %s became empty or optional evidence: bytes=%d error=%v", name, fault, len(encoded), err)
			}
		}
	}
}

// Cancellation of an optional initial lookup must stop preparation rather
// than making missing or already present data authorize a new namespace.
func TestRuntimeEvidenceSetupCanceledDiscoveryCannotClaimAbsence(t *testing.T) {
	t.Parallel()
	for _, present := range []bool{false, true} {
		root := t.TempDir()
		if present {
			path := filepath.Join(root, "evidence-v2-setup", "completed.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := requireRuntimeEvidenceSetupUnpreparedV2(ctx, root, 1024); !errors.Is(err, context.Canceled) || validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
			t.Fatalf("present=%t canceled discovery gained fresh authority: %v", present, err)
		}
	}
}

// Preflight can start before either receipt exists, but durable progress or
// an orphan completion prevents it from claiming a fresh activation namespace.
func TestRuntimeEvidenceSetupPreflightRequiresUnstartedReceipts(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	preparedPath := filepath.Join(fixture.stateDir, "evidence-v2-setup", "prepared.json")
	if err := os.Remove(preparedPath); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntimeEvidenceSetupRevisionV2(fixture.cfg, fixture.stateDir, fixture.plan, fixture.roles, nil); err != nil {
		t.Fatalf("never-started preflight requires retained setup: %v", err)
	}
	entries := []JournalEntry{{PlanHash: fixture.plan.PlanHash, ActionID: runtimeEvidenceActivationActionId(1, 1), Stage: StageIntent}}
	if err := validateRuntimeEvidenceSetupRevisionV2(fixture.cfg, fixture.stateDir, fixture.plan, fixture.roles, entries); err == nil {
		t.Fatal("prepared receipt absence hid durable activation progress")
	}
	completedBytes, err := json.Marshal(fixture.completed)
	if err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	for _, encoded := range [][]byte{nil, []byte("{broken\n"), append(completedBytes, '\n')} {
		if err := os.WriteFile(filepath.Join(fixture.stateDir, "evidence-v2-setup", "completed.json"), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validateRuntimeEvidenceSetupRevisionV2(fixture.cfg, fixture.stateDir, fixture.plan, fixture.roles, nil); err == nil {
			t.Fatal("orphan completion was accepted by revision preflight")
		}
		// The missing chain is unreachable: receipt ownership must reject first.
		if _, _, err := executor.prepareRuntimeEvidenceActivationsV2(t.Context(), nil); err == nil {
			t.Fatal("orphan completion was accepted by fresh preparation")
		}
	}
}

// Real signed setup proves required consumers reject unavailable or tampered
// receipts before rebuilding runtime inputs or producing final authority.
func TestRuntimeEvidenceSetupRequiredReadersRejectIncompleteReceipts(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntimeEvidenceSetupRevisionV2(fixture.cfg, fixture.stateDir, fixture.plan, fixture.roles, nil); err != nil {
		t.Fatalf("complete same-plan setup failed its revision preflight: %v", err)
	}
	if _, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir); err != nil {
		t.Fatal(err)
	}
	if _, err := captureFinalValidatorAuthorityV2(t.Context(), fixture.cfg, fixture.stateDir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"prepared.json", "completed.json"} {
		path := filepath.Join(fixture.stateDir, "evidence-v2-setup", name)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, fault := range []string{"missing", "empty", "malformed", "tampered"} {
			var changed []byte
			switch fault {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				changed = []byte("{broken\n")
			case "tampered":
				if name == "prepared.json" {
					value := *fixture.prepared
					value.Epoch++
					changed, err = json.Marshal(value)
				} else {
					value := *fixture.completed
					value.PreparedHash = fixture.cfg.PolicyHash
					changed, err = json.Marshal(value)
				}
				if err != nil {
					t.Fatal(err)
				}
				changed = append(changed, '\n')
			}
			if fault != "missing" {
				if err := os.WriteFile(path, changed, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := validateRuntimeEvidenceSetupRevisionV2(fixture.cfg, fixture.stateDir, fixture.plan, fixture.roles, nil); err == nil {
				t.Fatalf("revision accepted %s %s", fault, name)
			}
			if _, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir); err == nil {
				t.Fatalf("runtime resolution accepted %s %s", fault, name)
			}
			if _, err := captureFinalValidatorAuthorityV2(t.Context(), fixture.cfg, fixture.stateDir); err == nil {
				t.Fatalf("final authority accepted %s %s", fault, name)
			}
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
