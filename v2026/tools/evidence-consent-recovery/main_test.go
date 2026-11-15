//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

func recoveryTestArguments(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	pin := fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic pinned public input")))
	return []string{"--recover-same-header-consent", "--config", filepath.Join(root, "config.yml"), "--config-sha256", pin, "--artifact", filepath.Join(root, "artifact.json"), "--artifact-sha256", pin, "--expected-header", filepath.Join(root, "header.json"), "--expected-header-sha256", pin, "--activation-context", filepath.Join(root, "activation.json"), "--activation-context-sha256", pin, "--server-keys", filepath.Join(root, "keys.json"), "--server-keys-sha256", pin, "--expected-manifest-sha256", pin, "--expected-manifest-bytes", "123", "--scratch", filepath.Join(root, "scratch")}
}

func TestConsentRecoveryCommandRequiresExplicitModeBeforeManagedLoading(t *testing.T) {
	arguments := recoveryTestArguments(t)
	if _, err := parseRecoveryCommand(arguments[1:], defaultRecoveryCommandSettings()); err == nil {
		t.Fatal("missing explicit recovery flag was accepted")
	}
	var output bytes.Buffer
	if err := runRecoveryCommand(t.Context(), recoveryCommand{}, &output, defaultRecoveryCommandSettings()); err == nil || !strings.Contains(err.Error(), "explicit") || output.Len() != 0 {
		t.Fatalf("unacknowledged command reached input/configuration loading: %v output=%q", err, output.String())
	}
}

func TestConsentRecoveryCommandRetainsEveryIndependentPin(t *testing.T) {
	arguments := recoveryTestArguments(t)
	command, err := parseRecoveryCommand(arguments, defaultRecoveryCommandSettings())
	if err != nil || !command.RecoverSameHeaderConsent || command.ExpectedManifestBytes != 123 || command.ConfigHash == "" || command.ArtifactHash == "" || command.HeaderHash == "" || command.ActivationHash == "" || command.ServerKeysHash == "" || command.ExpectedManifestHash == "" {
		t.Fatalf("complete independently pinned command was not admitted: %+v %v", command, err)
	}
}

func TestConsentRecoveryCommandRejectsMissingPinsAliasesAndUnboundedDeadline(t *testing.T) {
	for _, field := range []string{"--config-sha256", "--artifact-sha256", "--expected-header-sha256", "--activation-context-sha256", "--server-keys-sha256", "--expected-manifest-sha256", "--expected-manifest-bytes", "--scratch"} {
		arguments := recoveryTestArguments(t)
		for index := range arguments {
			if arguments[index] == field {
				arguments[index+1] = ""
				break
			}
		}
		if _, err := parseRecoveryCommand(arguments, defaultRecoveryCommandSettings()); err == nil {
			t.Fatalf("missing %s was accepted", field)
		}
	}
	for _, extra := range [][]string{{"--timeout=0s"}, {"--timeout=-1s"}, {"unexpected"}, {"--private-key=synthetic"}, {"--relay=true"}, {"--send=true"}} {
		if _, err := parseRecoveryCommand(append(recoveryTestArguments(t), extra...), defaultRecoveryCommandSettings()); err == nil {
			t.Fatalf("invalid options were accepted: %v", extra)
		}
	}
	for _, pin := range []string{"", strings.Repeat("0", 64), strings.Repeat("a", 63), strings.Repeat("A", 64), "0x" + strings.Repeat("a", 64), "../synthetic"} {
		if _, err := decodeRecoveryHash(pin); err == nil {
			t.Fatalf("noncanonical independent pin was accepted: %q", pin)
		}
	}
}

func TestConsentRecoveryPublicInputPinCorruptionAndBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public.json")
	raw := []byte("{\"synthetic\":true}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	pin := fmt.Sprintf("%x", sha256.Sum256(raw))
	actual, err := readRecoveryInput(t.Context(), path, pin, 1024)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("exact public input failed: %v", err)
	}
	if result, err := readRecoveryInput(t.Context(), path, pin, 1); err == nil || result != nil {
		t.Fatalf("oversized public input escaped: %q %v", result, err)
	}
	raw[0] ^= 1
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := readRecoveryInput(t.Context(), path, pin, 1024); err == nil || result != nil {
		t.Fatalf("changed public input escaped: %q %v", result, err)
	}
}

func TestConsentRecoveryPublicInputRejectsSymlinkAndHardlinkAliases(t *testing.T) {
	for _, hardlink := range []bool{false, true} {
		root := t.TempDir()
		original, alias := filepath.Join(root, "original.json"), filepath.Join(root, "alias.json")
		raw := []byte("synthetic public metadata\n")
		if err := os.WriteFile(original, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		if hardlink {
			err = os.Link(original, alias)
		} else {
			err = os.Symlink(original, alias)
		}
		if err != nil {
			t.Fatal(err)
		}
		if result, err := readRecoveryInput(t.Context(), alias, fmt.Sprintf("%x", sha256.Sum256(raw)), 1024); err == nil || result != nil {
			t.Fatalf("aliased input escaped: hardlink=%t result=%q err=%v", hardlink, result, err)
		}
	}
}

func TestConsentRecoveryStrictPublicHeaderRejectsUnknownAndTrailingFields(t *testing.T) {
	for _, raw := range [][]byte{[]byte("{\"unexpected\":true}\n"), []byte("{}\n{}\n"), []byte("{\"epoch\":\"synthetic\"}\n")} {
		var header protocol.ValidatorEvidenceHeader
		if err := decodeRecoveryJson(raw, &header); err == nil {
			t.Fatalf("malformed public header was accepted: %q", raw)
		}
	}
	var header protocol.ValidatorEvidenceHeader
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeRecoveryJson(raw, &header); err != nil {
		t.Fatalf("typed public JSON failed syntax admission: %v", err)
	}
}

func TestConsentRecoveryPublicProjectionClosesBeforeSuccessfulReceipt(t *testing.T) {
	path := filepath.Join(privateRecoveryProjectionTestDirectory(t), "public-consent.json")
	raw := []byte("synthetic original-header public consent\n")
	var output bytes.Buffer
	if err := writeRecoveryProjection(t.Context(), path, raw, &output); err != nil {
		t.Fatalf("durable public projection returned a late error: %v", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("public projection changed bytes: %q %v", actual, err)
	}
	var receipt struct {
		PublicProjection  bool   `json:"public_projection"`
		RecoveryCompleted bool   `json:"recovery_completed"`
		NewSignatures     bool   `json:"new_signatures"`
		Hash              string `json:"metadata_hash"`
		Bytes             uint64 `json:"metadata_bytes"`
	}
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil || !receipt.PublicProjection || receipt.RecoveryCompleted || receipt.NewSignatures || receipt.Bytes != uint64(len(raw)) || receipt.Hash != fmt.Sprintf("0x%x", sha256.Sum256(raw)) {
		t.Fatalf("public export falsely claimed repair or signing: %q %v", output.Bytes(), err)
	}
}

func TestConsentRecoveryPublicProjectionNeverOverwritesExistingPath(t *testing.T) {
	path := filepath.Join(privateRecoveryProjectionTestDirectory(t), "public-consent.json")
	original := []byte("synthetic retained public consent\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := writeRecoveryProjection(t.Context(), path, []byte("synthetic different bytes\n"), &output); err == nil || output.Len() != 0 {
		t.Fatalf("occupied export path returned success: %v %q", err, output.Bytes())
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("occupied export path changed: %q %v", actual, err)
	}
}

func TestConsentRecoveryPublicProjectionCancellationHasNoReceipt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(privateRecoveryProjectionTestDirectory(t), "public-consent.json")
	var output bytes.Buffer
	if err := writeRecoveryProjection(ctx, path, []byte("synthetic public consent\n"), &output); !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("cancelled export emitted success: %v %q", err, output.Bytes())
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled export created a file: %v", err)
	}
}

func TestConsentRecoveryPublicProjectionLateCloseFailureHasNoReceipt(t *testing.T) {
	path := filepath.Join(privateRecoveryProjectionTestDirectory(t), "public-consent.json")
	var output bytes.Buffer
	late := errors.New("synthetic late directory-close observation failure")
	err := writeRecoveryProjectionWithHooks(t.Context(), path, []byte("synthetic public consent\n"), &output, recoveryProjectionHooks{afterDirectoryClose: func() error { return late }})
	if !errors.Is(err, late) || output.Len() != 0 {
		t.Fatalf("late closed-descriptor failure emitted success: %v %q", err, output.Bytes())
	}
}

func TestConsentRecoveryPublicProjectionParentReplacementCannotRedirectOrProduceReceipt(t *testing.T) {
	directory := privateRecoveryProjectionTestDirectory(t)
	displaced := directory + ".displaced"
	path := filepath.Join(directory, "public-consent.json")
	raw := []byte("synthetic public consent\n")
	var output bytes.Buffer
	err := writeRecoveryProjectionWithHooks(t.Context(), path, raw, &output, recoveryProjectionHooks{afterDirectoryOpen: func() error {
		if err := os.Rename(directory, displaced); err != nil {
			return err
		}
		if err := os.Mkdir(directory, 0o700); err != nil {
			return err
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(displaced); err != nil {
				t.Error(err)
			}
		})
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), "changed before receipt") || output.Len() != 0 {
		t.Fatalf("replaced parent emitted a misleading export receipt: %v %q", err, output.Bytes())
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("export followed replaced parent: %v", err)
	}
	actual, err := os.ReadFile(filepath.Join(displaced, filepath.Base(path)))
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("descriptor-owned public output was redirected: %q %v", actual, err)
	}
}

func privateRecoveryProjectionTestDirectory(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
