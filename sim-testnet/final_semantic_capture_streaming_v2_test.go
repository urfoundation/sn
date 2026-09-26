//go:build linux || darwin

// Source readback owns one actual archived file at a time. Incomplete shape
// fixtures intentionally never acquire a semantic acceptance verdict.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// The independently derived role key remains distinct from source payloads.
func finalCaptureStreamingAuthority(t *testing.T, cfg *ResolvedConfig, collected *FinalCollectedValidatorInputs) *finalOperatorPathAuthority {
	t.Helper()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	identities := roles.Public()
	collected.EvidenceV2.Hotkey = identities.Substrate[validatorHotkeyLabel(int(collected.ValidatorID))].PublicKey
	return &finalOperatorPathAuthority{identities: &identities}
}

// A loader returning original bytes and cancellation must not cause another
// source read, even when its own I/O succeeded.
func TestFinalCaptureV2StreamingCancellationAfterActualRead(t *testing.T) {
	cfg, value, root := newFinalCaptureV2ShapeTestFixture(t)
	collected := value.Validators[0]
	authority := finalCaptureStreamingAuthority(t, cfg, &collected)
	loaded := map[string][]byte{collected.IntentStore.URI: []byte("{\"schema\":\"" + validatorpkg.SteeringIntentSchema + "\",\"history\":[],\"current\":null}")}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reads := 0
	err := verifyFinalCapturedValidatorBytesV2WithReader(ctx, cfg, value, collected, authority, loaded, func(ctx context.Context, locator FinalArtifactLocator) ([]byte, error) {
		reads++
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(locator.URI)))
		cancel()
		return raw, err
	})
	if !errors.Is(err, context.Canceled) || reads != 1 {
		t.Fatalf("source cancellation did not stop exact owner: reads=%d err=%v", reads, err)
	}
	if len(loaded) != 1 {
		t.Fatal("source bodies escaped into the control cache")
	}
}

// Changed disk bytes cannot be hidden by a previously loaded map entry.
func TestFinalCaptureV2StreamingRejectsChangedActualSource(t *testing.T) {
	cfg, value, root := newFinalCaptureV2ShapeTestFixture(t)
	collected := value.Validators[0]
	authority := finalCaptureStreamingAuthority(t, cfg, &collected)
	first := collected.EvidenceV2.Sources[0].Artifact
	path := filepath.Join(root, filepath.FromSlash(first.URI))
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded := map[string][]byte{
		collected.IntentStore.URI: []byte("{\"schema\":\"" + validatorpkg.SteeringIntentSchema + "\",\"history\":[],\"current\":null}"),
		first.URI:                 original,
	}
	if err := os.WriteFile(path, []byte("changed source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	err = verifyFinalCapturedValidatorBytesV2WithReader(t.Context(), cfg, value, collected, authority, loaded, func(ctx context.Context, locator FinalArtifactLocator) ([]byte, error) {
		reads++
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(locator.URI)))
	})
	if err == nil || !strings.Contains(err.Error(), "missing or changed") || reads != 1 {
		t.Fatalf("old cached bytes hid changed source: reads=%d err=%v", reads, err)
	}
	if string(loaded[first.URI]) != string(original) {
		t.Fatal("readback mutated caller-owned original cache")
	}
}
