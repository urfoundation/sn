// Unsigned review exercises the actual dispatcher and signature admission.
// Public draft material can be reviewed without acquiring any custody owner.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Draft publication never signs or invokes the fixture's signed-plan loader.
func writeEvmPreviewConfig(t *testing.T, f *evmCreateFixture, config evmPhaseConfig) {
	t.Helper()
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// The returned bytes are exactly what an operator obtains from the CLI.
func runEvmPreview(t *testing.T, f *evmCreateFixture, extra ...string) (evmPhasePreview, int, string) {
	t.Helper()
	args := append([]string{"bootstrap-contracts", "preview", "--config", f.configPath}, extra...)
	prepared := mainnetNamespaceTest(t, f.config.Plan.RunDirectory)
	var stdout, stderr bytes.Buffer
	code := runMain(f.storageContext(context.Background()), args, &stdout, &stderr)
	if !reflect.DeepEqual(prepared, mainnetNamespaceTest(t, f.config.Plan.RunDirectory)) {
		t.Fatal("preview changed the prepared custody namespace")
	}
	var result evmPhasePreview
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	} else if stdout.Len() != 0 {
		t.Fatalf("rejected preview published partial signing material: %s", stdout.String())
	}
	return result, code, stderr.String()
}

// No mutable phase file or owned-RPC observation is a preview side effect.
func requireEvmPreviewNoAuthority(t *testing.T, f *evmCreateFixture, directory string) {
	t.Helper()
	path := filepath.Join(directory, evmCreateStateFile)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsigned preview retained a record: %v", err)
	}
	if directory == f.config.Plan.RunDirectory {
		owner, err := openBootstrapUnclaimedSnapshot(f.storageContext(t.Context()), path, "mainnet-evm-action", 512*1024)
		if err != nil {
			t.Fatal("unsigned preview changed explicitly fresh custody", err)
		}
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
	} else if _, err := os.Lstat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsigned preview enrolled a future custody path", err)
	}
	f.stateLock.Lock()
	untouched := len(f.counts) == 0 && len(f.writes) == 0
	f.stateLock.Unlock()
	if !untouched {
		t.Fatal("unsigned preview reached RPC")
	}
}

// The signing message is checked independently against the domain plus the
// typed JSON bytes, while a nonexistent custody directory remains nonexistent.
func TestEvmPhasePreviewExportsExactApprovalWithoutCustody(t *testing.T) {
	f := newEvmCreateFixture(t)
	config := copyEvmPhaseConfig(f.config)
	config.Signature = ""
	config.Plan.RunDirectory = filepath.Join(config.Plan.RunDirectory, "future-custody")
	writeEvmPreviewConfig(t, f, config)
	preview, code, diagnostic := runEvmPreview(t, f)
	if code != 0 {
		t.Fatalf("unsigned review required prior authority: %d %s", code, diagnostic)
	}
	planBytes, err := json.Marshal(config.Plan)
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]byte("urnetwork-mainnet-contract-phase-approval-v1\x00"), planBytes...)
	message, err := hex.DecodeString(preview.ApprovalSigningMessageHex)
	if err != nil || !bytes.Equal(message, expected) {
		t.Fatalf("preview changed exact approval signing bytes: %v", err)
	}
	digest := sha256.Sum256(expected)
	if preview.Schema != evmPhasePreviewSchema || preview.PlanHash != config.Plan.hash() || preview.ApprovalPublicKey != config.ApprovalPublicKey || preview.ApprovalSigningMessageSha256 != "sha256:"+hex.EncodeToString(digest[:]) || preview.ApprovalVerified || preview.InstallationComplete || preview.ExecutableAction != "reserve-create" || preview.ReserveAddress != f.plan.Address.Hex() || preview.ExpectedReserveRuntimeHash == "" {
		t.Fatalf("unsigned review misstated its authority or payload: %+v", preview)
	}
	if _, err := os.Lstat(config.Plan.RunDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsigned review touched future custody directory: %v", err)
	}
	requireEvmPreviewNoAuthority(t, f, config.Plan.RunDirectory)
}

// A real independent synthetic signature over the exported bytes admits the
// existing signed commands. Unsigned or subsequently changed plans still fail.
func TestEvmPhasePreviewSignatureAdmitsOnlyExactReviewedPlan(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.config.Signature = ""
	writeEvmPreviewConfig(t, f, f.config)
	preview, code, diagnostic := runEvmPreview(t, f)
	if code != 0 {
		t.Fatal(diagnostic)
	}
	for _, command := range []string{"plan", "apply", "resume"} {
		if _, code, diagnostic := f.command(command); code != 2 || !strings.Contains(diagnostic, "signature requires 128") {
			t.Fatalf("unsigned %s escaped independent approval: %d %s", command, code, diagnostic)
		}
	}
	requireEvmPreviewNoAuthority(t, f, f.config.Plan.RunDirectory)
	message, err := hex.DecodeString(preview.ApprovalSigningMessageHex)
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("synthetic contract independent approval"))
	key := ed25519.NewKeyFromSeed(seed[:])
	f.config.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	writeEvmPreviewConfig(t, f, f.config)
	if _, code, diagnostic := f.command("plan"); code != 0 {
		t.Fatalf("signature over exported bytes did not verify: %d %s", code, diagnostic)
	}
	if result, code, diagnostic := f.command("apply"); code != 0 || result.Status != "signature-awaiting-import" || result.PlanHash != preview.PlanHash {
		t.Fatalf("exact signed preview did not reach existing owner: %+v %d %s", result, code, diagnostic)
	}
	path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := copyEvmPhaseConfig(f.config)
	for _, change := range []string{"missing-signature", "wrong-signature", "changed-plan", "changed-key"} {
		candidate := copyEvmPhaseConfig(original)
		switch change {
		case "missing-signature":
			candidate.Signature = ""
		case "wrong-signature":
			candidate.Signature = strings.Repeat("00", 64)
		case "changed-plan":
			candidate.Plan.ValidThroughNative++
		case "changed-key":
			candidate.ApprovalPublicKey = "0x" + strings.Repeat("19", 32)
		}
		writeEvmPreviewConfig(t, f, candidate)
		if _, code, diagnostic := f.command("resume"); code != 2 || !strings.Contains(diagnostic, "signature") && !strings.Contains(diagnostic, "independent approval is invalid") {
			t.Fatalf("preview weakened signed resume for %s: %d %s", change, code, diagnostic)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("unapproved %s changed retained custody: %v", change, err)
		}
	}
	f.stateLock.Lock()
	untouched := len(f.counts) == 0 && len(f.writes) == 0
	f.stateLock.Unlock()
	if !untouched {
		t.Fatal("preview or offline approval admission reached RPC")
	}
}

// Unsigned review retains all structural/artifact and semantic CREATE checks;
// supplied approval bytes are rejected instead of being silently ignored.
func TestEvmPhasePreviewRejectsMalformedStructureAndArtifact(t *testing.T) {
	for _, change := range []string{"schema", "scope", "key", "empty-actions", "fee-budget", "route", "artifact", "constructor", "signature"} {
		f := newEvmCreateFixture(t)
		candidate := copyEvmPhaseConfig(f.config)
		candidate.Signature = ""
		switch change {
		case "schema":
			candidate.Schema = "unknown-contract-phase"
		case "scope":
			candidate.Plan.Network.EvmChainId = 945
		case "key":
			candidate.ApprovalPublicKey = ""
		case "empty-actions":
			candidate.Plan.Actions = nil
		case "fee-budget":
			candidate.Plan.MaximumTotalWei = "1"
		case "route":
			candidate.Plan.Route.RpcUrl = "http://unapproved.example:80"
		case "artifact":
			candidate.Plan.Artifacts.Sha256 = "sha256:" + strings.Repeat("01", 32)
		case "constructor":
			candidate.Plan.Actions[0].Data = "0x00"
		case "signature":
			candidate.Signature = f.config.Signature
		}
		writeEvmPreviewConfig(t, f, candidate)
		if _, code, diagnostic := runEvmPreview(t, f); code != 2 {
			t.Fatalf("unsigned preview accepted malformed %s: %d %s", change, code, diagnostic)
		}
		requireEvmPreviewNoAuthority(t, f, candidate.Plan.RunDirectory)
	}
}

// Strict JSON and command parsing prevent ambiguous drafts or an execution
// option from leaking into the read-only preparation command.
func TestEvmPhasePreviewRejectsAmbiguousJsonAndExecutionFlags(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.config.Signature = ""
	raw, err := json.Marshal(f.config)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{`,"schema":"duplicate"}`, `,"unexpected_authority":true}`} {
		ambiguous := append(append([]byte(nil), raw[:len(raw)-1]...), suffix...)
		if err := os.WriteFile(f.configPath, ambiguous, 0600); err != nil {
			t.Fatal(err)
		}
		if _, code, diagnostic := runEvmPreview(t, f); code != 2 {
			t.Fatalf("ambiguous preview JSON was admitted: %d %s", code, diagnostic)
		}
	}
	writeEvmPreviewConfig(t, f, f.config)
	for _, args := range [][]string{{"--online"}, {"--submit"}, {"--run-dir", f.config.Plan.RunDirectory}, {"--accept-plan-hash", f.config.Plan.hash()}, {"--signed-transaction", f.signedPath, "--signed-transaction-hash", f.signedHash}} {
		if _, code, diagnostic := runEvmPreview(t, f, args...); code != 2 {
			t.Fatalf("preview admitted execution flags %v: %d %s", args, code, diagnostic)
		}
	}
	requireEvmPreviewNoAuthority(t, f, f.config.Plan.RunDirectory)
}

// An active retained owner is irrelevant to public preview. Neither its lock
// nor even a deliberately undecodable journal is inspected or replaced.
func TestEvmPhasePreviewDoesNotInspectRetainedCustody(t *testing.T) {
	f := newEvmCreateFixture(t)
	store, err := openEvmActionStore(f.config, true, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	sentinel := []byte("synthetic opaque retained custody must not be read\n")
	if err := os.WriteFile(path, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	f.config.Signature = ""
	writeEvmPreviewConfig(t, f, f.config)
	if _, code, diagnostic := runEvmPreview(t, f); code != 0 {
		t.Fatalf("unsigned preview inspected retained owner state: %d %s", code, diagnostic)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(sentinel, after) {
		t.Fatalf("unsigned preview changed opaque retained custody: %v", err)
	}
	f.stateLock.Lock()
	untouched := len(f.counts) == 0 && len(f.writes) == 0
	f.stateLock.Unlock()
	if !untouched {
		t.Fatal("preview with retained custody reached RPC")
	}
}

// Failed output and an already canceled operation leave no persisted approval,
// journal, sender nonce ownership or online side effect to reconcile.
func TestEvmPhasePreviewCancellationAndOutputFailureLeaveNoAuthority(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.config.Signature = ""
	writeEvmPreviewConfig(t, f, f.config)
	args := []string{"bootstrap-contracts", "preview", "--config", f.configPath}
	prepared := mainnetNamespaceTest(t, f.config.Plan.RunDirectory)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(ctx), args, &stdout, &stderr); code == 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Fatalf("canceled preview emitted signing material: %d %s", code, stderr.String())
	}
	stderr.Reset()
	if code := runMain(f.storageContext(context.Background()), args, bootstrapRootFailedWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "unsigned preview output") {
		t.Fatalf("failed signing-material output was acknowledged: %d %s", code, stderr.String())
	}
	if !reflect.DeepEqual(prepared, mainnetNamespaceTest(t, f.config.Plan.RunDirectory)) {
		t.Fatal("canceled or failed preview changed prepared custody")
	}
	requireEvmPreviewNoAuthority(t, f, f.config.Plan.RunDirectory)
}
