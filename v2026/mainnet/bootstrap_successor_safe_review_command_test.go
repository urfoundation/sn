// The public Safe review command joins genuinely retained eight-action custody
// to published release bytes without sending a request or repairing a journal.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Every prerequisite is produced through its original public owner. The review
// must reload those files, retain exact seals and expose no executable boundary.
func TestBootstrapSuccessorSafeReviewCommandReconstructsPreparedV3Custody(t *testing.T) {
	f := newBootstrapSuccessorCommandFixture(t)
	bootstrapSuccessorCommandTestComplete(t, f)
	request := bootstrapSuccessorTestRequest(f.contracts.config, f.preparation.Plan.ContentHash)
	requestPath := filepath.Join(filepath.Dir(f.path), "synthetic-review-original-request.json")
	bootstrapRootTestWrite(t, requestPath, request)
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "contract-successor-preview", &stdout, &stderr, "--request", requestPath); code != 0 {
		t.Fatal("full-v3 preparation preview failed", code, stderr.String())
	}
	var preview bootstrapSuccessorPreparationPreview
	if err := decodePlanJson(stdout.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("synthetic contract independent approval"))
	approval := bootstrapSuccessorPreparationTestSign(t, preview.Plan, ed25519.NewKeyFromSeed(seed[:]))
	approvalRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "synthetic-review-approval.json"), approval)
	record := bootstrapSuccessorSafeTestRecord(approval)
	profile, archivePath, _ := safeReleaseTestInputs(t, "1.5.0", "SafeL2")
	lastReceipt := preview.Plan.Proposal.AdoptedActions[7].Receipt
	safeRequest := bootstrapSuccessorSafeRequest{Schema: bootstrapSuccessorSafeRequestSchema, PreparationPlanHash: preview.PlanHash, PreparationRecordHash: record.ContentHash,
		Version: "1.5.0", Variant: "SafeL2", Archive: planFileReference{Path: archivePath, Sha256: profile.ArchiveSha256}, RelayerGas: 100000,
		RelayerFeeCapWei: "10", RelayerTipCapWei: "1", StartNativeNumber: lastReceipt.NativeNumber, StartNativeHash: lastReceipt.NativeHash, ValidThroughNative: lastReceipt.NativeNumber + 100}
	safePath := filepath.Join(filepath.Dir(f.path), "synthetic-review-safe-request.json")
	safeRef := bootstrapRootTestWrite(t, safePath, safeRequest)
	invoke := func(ctx context.Context, extra ...string) ([]byte, int, string) {
		var out, diagnostic bytes.Buffer
		code := f.command(ctx, "contract-successor-safe-review", &out, &diagnostic, append([]string{"--request", requestPath, "--safe-request", safePath}, extra...)...)
		return out.Bytes(), code, diagnostic.String()
	}
	before := bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)
	if out, code, _ := invoke(t.Context()); code != 1 || len(out) != 0 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)) {
		t.Fatal("public Safe review created absent preparation", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := f.command(t.Context(), "contract-successor-prepare", &stdout, &stderr, "--request", requestPath, "--approval", approvalRef.Path,
		"--approval-sha256", approvalRef.Sha256, "--accept-successor-hash", preview.PlanHash); code != 0 {
		t.Fatal("full-v3 preparation failed", code, stderr.String())
	}
	before = bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)
	f.contracts.stateLock.Lock()
	counts, writes := maps.Clone(f.contracts.counts), len(f.contracts.writes)
	f.contracts.stateLock.Unlock()
	f.census.stateLock.Lock()
	censusCounts := maps.Clone(f.census.methodCounts)
	f.census.stateLock.Unlock()
	var first []byte
	for i := range 2 {
		raw, code, diagnostic := invoke(t.Context())
		if code != 3 || diagnostic != "" || len(raw) == 0 {
			t.Fatalf("full-v3 offline review failed: %d %s", code, diagnostic)
		}
		var result bootstrapSuccessorSafeReview
		if err := decodePlanJson(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "offline-review-authority-unresolved" || result.Preparation.ContentHash != record.ContentHash || result.RequestReference != safeRef ||
			rootObjectHash(result.Preparation.Approval) != rootObjectHash(approval) || result.Transaction.Safe != request.IntendedOwnerSafe || result.Transaction.Nonce != request.IntendedSafeNonce ||
			result.Relayer.Sender != request.IntendedRelayer || result.Relayer.Nonce != request.IntendedRelayerNonce || result.Transaction.Data != preview.Plan.Proposal.Anchor.CallData ||
			len(result.Preparation.Approval.Plan.Proposal.AdoptedActions) != 8 || result.Preparation.Approval.Plan.Proposal.Budget.RetainedAttempts != 8 || result.Relayer.CalldataComplete || result.SigningAuthorized || result.Executable ||
			result.CurrentChainVerified || result.SafeAuthorityVerified || result.NetworkEffects || result.InstallationComplete || result.ActivationReady {
			t.Fatal("public review lost retained identities or invented authority")
		}
		if i == 0 {
			first = raw
		} else if !bytes.Equal(first, raw) {
			t.Fatal("repeated read-only review changed its exact result")
		}
	}
	for _, extra := range [][]string{{"--online"}, {"--submit"}, {"--execute"}, {"--rpc", "https://synthetic.example"}, {"--signer", approvalRef.Path}, {"--signatures", approvalRef.Path}, {"--approval", approvalRef.Path}, {"--signed-transaction", approvalRef.Path}} {
		if raw, code, _ := invoke(t.Context(), extra...); code != 2 || len(raw) != 0 {
			t.Fatal("Safe review exposed execution or signature import", extra, code)
		}
	}
	for _, path := range []string{f.preparation.childPaths()[0], filepath.Join(f.config.RunDirectory, bootstrapContractStateFile(7)), filepath.Join(f.config.RunDirectory, bootstrapSuccessorPreparationFile)} {
		saved, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		raw, code, diagnostic := invoke(t.Context())
		if err := os.WriteFile(path, saved, 0600); err != nil {
			t.Fatal(err)
		}
		if code != 1 || len(raw) != 0 {
			t.Fatal("public review trusted missing original or prepared custody", filepath.Base(path), code, diagnostic)
		}
	}
	for _, path := range []string{f.preparation.childPaths()[0] + ".lock", filepath.Join(f.config.RunDirectory, bootstrapSuccessorPreparationFile+".lock")} {
		saved, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, saved[:len(saved)-1], 0600); err != nil {
			t.Fatal(err)
		}
		raw, code, diagnostic := invoke(t.Context())
		if err := os.WriteFile(path, saved, 0600); err != nil {
			t.Fatal(err)
		}
		if code != 1 || len(raw) != 0 {
			t.Fatal("public review repaired incomplete original or prepared claim", filepath.Base(path), code, diagnostic)
		}
	}
	lock, err := os.OpenFile(f.preparation.childPaths()[1]+".lock", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	raw, code, diagnostic := invoke(t.Context())
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if code != 1 || len(raw) != 0 || !strings.Contains(diagnostic, "active custody owner") {
		t.Fatal("public review escaped original owner", code, diagnostic)
	}
	for _, field := range []string{"plan", "record"} {
		changed := safeRequest
		if field == "plan" {
			changed.PreparationPlanHash = rootObjectHash("synthetic other plan")
		} else {
			changed.PreparationRecordHash = rootObjectHash("synthetic other record")
		}
		bootstrapRootTestWrite(t, safePath, changed)
		if raw, code, _ := invoke(t.Context()); code != 3 || len(raw) != 0 {
			t.Fatal("public review ignored exact preparation acceptance", field, code)
		}
	}
	bootstrapRootTestWrite(t, safePath, safeRequest)
	requestBytes, err := os.ReadFile(safePath)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(requestBytes, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"safe_authority_verified", "signatures", "to", "operation", "gas_price", "approval_signature_ed25519"} {
		fields[field] = true
		bootstrapRootTestWrite(t, safePath, fields)
		if raw, code, _ := invoke(t.Context()); code != 2 || len(raw) != 0 {
			t.Fatal("public review accepted undeclared authority", field, code)
		}
		delete(fields, field)
	}
	bootstrapRootTestWrite(t, safePath, safeRequest)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if raw, code, _ := invoke(ctx); code == 0 || len(raw) != 0 {
		t.Fatal("canceled public review completed")
	}
	stderr.Reset()
	if code := f.command(t.Context(), "contract-successor-safe-review", ioFailureWriter{}, &stderr, "--request", requestPath, "--safe-request", safePath); code != 1 {
		t.Fatal("public review output failure was accepted", code)
	}
	if raw, code, diagnostic := invoke(t.Context()); code != 3 || diagnostic != "" || !bytes.Equal(first, raw) {
		t.Fatal("failed output retained ownership or changed review", code, diagnostic)
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)) {
		t.Fatal("public review mutated custody")
	}
	f.contracts.stateLock.Lock()
	unchanged := maps.Equal(counts, f.contracts.counts) && writes == len(f.contracts.writes)
	f.contracts.stateLock.Unlock()
	f.census.stateLock.Lock()
	unchanged = unchanged && maps.Equal(censusCounts, f.census.methodCounts)
	f.census.stateLock.Unlock()
	if !unchanged {
		t.Fatal("offline Safe review called the native or EVM network")
	}
}
