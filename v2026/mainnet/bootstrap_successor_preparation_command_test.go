// One genuine eight-action v3 fixture exercises the public preparation command.
// All signatures and RPC state here are synthetic; production remains offline.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A public claim requires original authentic preparation, eight receipt seals,
// a separate exact approval and one root. None of these calls observes a chain.
func TestBootstrapSuccessorPreparationCommandRetainsActualV3Prefix(t *testing.T) {
	f := newBootstrapSuccessorCommandFixture(t)
	retained := bootstrapSuccessorCommandTestComplete(t, f)
	before := bootstrapContractTestJournals(t, f.config.RunDirectory)
	maps.Copy(before, f.journals(t))
	namesBefore := bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)
	f.contracts.stateLock.Lock()
	counts, writes := maps.Clone(f.contracts.counts), len(f.contracts.writes)
	f.contracts.stateLock.Unlock()
	f.census.stateLock.Lock()
	censusCounts := maps.Clone(f.census.methodCounts)
	f.census.stateLock.Unlock()
	request := bootstrapSuccessorTestRequest(f.contracts.config, f.preparation.Plan.ContentHash)
	requestPath := filepath.Join(filepath.Dir(f.path), "signed-preparation-request.json")
	requestRef := bootstrapRootTestWrite(t, requestPath, request)
	invoke := func(command string, extra ...string) ([]byte, int, string) {
		var stdout, stderr bytes.Buffer
		code := f.command(t.Context(), command, &stdout, &stderr, append([]string{"--request", requestPath}, extra...)...)
		return stdout.Bytes(), code, stderr.String()
	}
	raw, code, diagnostic := invoke("contract-successor-preview")
	if code != 0 || diagnostic != "" {
		t.Fatalf("genuine v3 preparation preview failed: %d %s", code, diagnostic)
	}
	var preview bootstrapSuccessorPreparationPreview
	if err := decodePlanJson(raw, &preview); err != nil {
		t.Fatal(err)
	}
	message, err := preview.Plan.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	if preview.PlanHash != preview.Plan.hash() || preview.SigningBytes != "0x"+hex.EncodeToString(message) || preview.Plan.Request.Sha256 != requestRef.Sha256 ||
		preview.Plan.Proposal.LocalPreparation.ContractTransactionHash != retained.Contracts.TransactionHash || len(preview.Plan.Proposal.AdoptedActions) != 8 ||
		preview.Plan.ApprovalPublicKey != f.contracts.config.ApprovalPublicKey || !maps.Equal(namesBefore, bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)) {
		t.Fatal("public preparation preview lost original authority or created custody")
	}
	for i, adopted := range preview.Plan.Proposal.AdoptedActions {
		var original evmActionRecord
		if err := decodePlanJson([]byte(before[filepath.Join(f.config.RunDirectory, bootstrapContractStateFile(i))]), &original); err != nil {
			t.Fatal(err)
		}
		if adopted.JournalHash != original.ContentHash || adopted.CustodyHash != rootObjectHash(original) || adopted.TransactionHash != original.TransactionHash ||
			adopted.Attempts != original.Attempts || adopted.Receipt == nil || original.Receipt == nil || *adopted.Receipt != *original.Receipt {
			t.Fatalf("public preparation changed genuine retained prefix action %d", i)
		}
	}
	seed := sha256.Sum256([]byte("synthetic contract independent approval"))
	approval := bootstrapSuccessorPreparationTestSign(t, preview.Plan, ed25519.NewKeyFromSeed(seed[:]))
	approvalPath := filepath.Join(filepath.Dir(f.path), "signed-preparation-approval.json")
	approvalRef := bootstrapRootTestWrite(t, approvalPath, approval)
	approvalArgs := []string{"--approval", approvalPath, "--approval-sha256", approvalRef.Sha256, "--accept-successor-hash", preview.PlanHash}
	for _, extra := range [][]string{{"--online"}, {"--submit"}, {"--execute"}, {"--rpc", "https://synthetic.example"}, {"--signed-transaction", approvalPath}, {"--signer", approvalPath}} {
		if _, code, _ := invoke("contract-successor-prepare", append(append([]string(nil), approvalArgs...), extra...)...); code != 2 {
			t.Fatal("offline preparation exposed execution or signing flags", extra, code)
		}
	}
	if _, code, _ := invoke("contract-successor-resume", approvalArgs...); code == 0 {
		t.Fatal("public resume created an absent successor claim")
	}
	for _, path := range []string{f.preparation.childPaths()[0], filepath.Join(f.config.RunDirectory, bootstrapContractStateFile(7))} {
		saved := before[path]
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		_, code, diagnostic := invoke("contract-successor-prepare", approvalArgs...)
		if err := os.WriteFile(path, []byte(saved), 0600); err != nil {
			t.Fatal(err)
		}
		if code == 0 || !strings.Contains(diagnostic, "original scope unresolved") {
			t.Fatalf("public preparation trusted missing original retained custody %s: %d %s", filepath.Base(path), code, diagnostic)
		}
	}
	markerPath := f.preparation.childPaths()[0] + ".lock"
	if err := os.WriteFile(markerPath, []byte(f.preparation.Plan.ContentHash+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, code, diagnostic = invoke("contract-successor-prepare", approvalArgs...)
	if err := os.WriteFile(markerPath, []byte(before[markerPath]), 0600); err != nil {
		t.Fatal(err)
	}
	if code == 0 || !strings.Contains(diagnostic, "original scope unresolved") {
		t.Fatal("public preparation accepted a partial original marker", code, diagnostic)
	}
	lock, err := os.OpenFile(f.preparation.childPaths()[1]+".lock", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	_, code, diagnostic = invoke("contract-successor-prepare", approvalArgs...)
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if code == 0 || !strings.Contains(diagnostic, "active custody owner") {
		t.Fatal("public preparation escaped the original action owner", code, diagnostic)
	}
	bad := copyBootstrapSuccessorPreparationTestApproval(t, approval)
	bad.Signature = strings.Repeat("00", ed25519.SignatureSize)
	badRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "invalid-preparation-approval.json"), bad)
	if _, code, _ := invoke("contract-successor-prepare", "--approval", badRef.Path, "--approval-sha256", badRef.Sha256, "--accept-successor-hash", preview.PlanHash); code != 2 {
		t.Fatal("public preparation accepted a forged independent approval", code)
	}
	if !maps.Equal(namesBefore, bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)) {
		t.Fatal("refused preparation altered original custody or reserved a new claimant")
	}
	raw, code, diagnostic = invoke("contract-successor-prepare", approvalArgs...)
	if code != 0 || diagnostic != "" {
		t.Fatalf("public signed preparation failed exact original prefix: %d %s", code, diagnostic)
	}
	var result bootstrapSuccessorPreparationResult
	if err := decodePlanJson(raw, &result); err != nil {
		t.Fatal(err)
	}
	if !result.LocalPreparationComplete || !result.PreparationApprovalVerified || result.RetainedOriginalActions != 8 || result.PlanHash != preview.PlanHash ||
		result.ProposedBudget.RetainedAttempts != 8 || result.ProposedBudget.ProposedMaximumCumulativeAttempts != 10 || result.ProposedBudget.ProposedRemainingAttempts != 2 ||
		result.ProposedBudget.RetryMarginAttempts != 1 || result.ProposedBudget.ProposedMaximumLifetimeWei != "2244000000" || result.PhysicalRoot != preview.Plan.Root ||
		result.ExecutionApprovalVerified || result.CurrentChainVerified || result.SafeAuthorityVerified || result.GlobalSigningCustodyVerified || result.Executable ||
		result.Signing || result.NetworkEffects || result.InstallationComplete || result.ActivationReady {
		t.Fatalf("public preparation lost additive floors or inferred executable authority: %+v", result)
	}
	after := bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)
	if len(after) != len(namesBefore)+3 {
		t.Fatal("public preparation created caller-selected or extra custody")
	}
	var record bootstrapSuccessorPreparationRecord
	if err := decodePlanJson([]byte(after[bootstrapSuccessorPreparationFile]), &record); err != nil {
		t.Fatal(err)
	}
	if err := record.validate(approval); err != nil {
		t.Fatal(err)
	}
	if result.RecordHash != record.ContentHash || result.ApprovalHash != rootObjectHash(approval) {
		t.Fatal("public preparation output differs from its actual durable claim")
	}
	if _, code, _ := invoke("contract-successor-prepare", approvalArgs...); code == 0 {
		t.Fatal("public duplicate prepare replaced its fixed claimant")
	}
	var stderr bytes.Buffer
	if code := f.command(t.Context(), "contract-successor-resume", ioFailureWriter{}, &stderr, append([]string{"--request", requestPath}, approvalArgs...)...); code != 1 {
		t.Fatal("public preparation lost its output failure", code)
	}
	raw, code, diagnostic = invoke("contract-successor-resume", approvalArgs...)
	if code != 0 || diagnostic != "" {
		t.Fatal("public resume failed after output loss", code, diagnostic)
	}
	var resumed bootstrapSuccessorPreparationResult
	if err := decodePlanJson(raw, &resumed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, resumed) || !maps.Equal(after, bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)) {
		t.Fatal("public resume changed immutable preparation or renewed budget")
	}
	if original := f.result(t, "resume"); !reflect.DeepEqual(original, retained) {
		t.Fatal("successor preparation prevented unchanged original recovery")
	}
	for path, original := range before {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != original {
			t.Fatalf("signed preparation altered original signature, receipt or marker %s: %v", filepath.Base(path), err)
		}
	}
	f.contracts.stateLock.Lock()
	unchanged := maps.Equal(counts, f.contracts.counts) && len(f.contracts.writes) == writes
	f.contracts.stateLock.Unlock()
	f.census.stateLock.Lock()
	unchanged = unchanged && maps.Equal(censusCounts, f.census.methodCounts)
	f.census.stateLock.Unlock()
	if !unchanged || writes != 8 {
		t.Fatal("signed local preparation reobserved or replayed the original actions")
	}
}

// Namespace checks include original inputs, transitive validator claims, both
// fixed filenames and arbitrary hash stage names before an owner can write.
func TestBootstrapSuccessorPreparationSeparatesOriginalInputsAndOwners(t *testing.T) {
	f := newBootstrapChainFixture(t)
	_, plans, err := prepareBootstrapContractReadiness(t.Context(), f.preparation.Contracts, f.config.Contracts.Path)
	if err != nil {
		t.Fatal(err)
	}
	requestPath, approvalPath := filepath.Join(filepath.Dir(f.path), "preparation-request.json"), filepath.Join(filepath.Dir(f.path), "preparation-approval.json")
	if err := validateBootstrapSuccessorPreparationPaths(f.preparation, plans, requestPath, approvalPath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{f.path, f.config.Contracts.Path, requestPath, filepath.Join(f.config.RunDirectory, bootstrapSuccessorPreparationFile), filepath.Join(f.config.RunDirectory, bootstrapSuccessorPreparationFile+".lock"), filepath.Join(f.config.RunDirectory, bootstrapSuccessorStagePrefix+"synthetic.claim")} {
		if err := validateBootstrapSuccessorPreparationPaths(f.preparation, plans, requestPath, path); err == nil {
			t.Fatal("preparation approval aliases input or reserved custody", path)
		}
		if err := validateBootstrapSuccessorPreparationPaths(f.preparation, plans, requestPath, approvalPath, path); err == nil {
			t.Fatal("additional review input aliases original or prepared custody", path)
		}
	}
	archivePath := filepath.Join(filepath.Dir(f.path), "synthetic-reviewed-release.tgz")
	if err := validateBootstrapSuccessorPreparationPaths(f.preparation, plans, requestPath, approvalPath, archivePath); err != nil {
		t.Fatal("independent review archive refused", err)
	}
	if err := validateBootstrapSuccessorPreparationPaths(f.preparation, plans, requestPath, approvalPath, archivePath, archivePath); err == nil {
		t.Fatal("additional review inputs alias each other")
	}
	for _, path := range []string{filepath.Join(f.config.RunDirectory, bootstrapSuccessorPreparationFile), filepath.Join(f.config.RunDirectory, bootstrapSuccessorStagePrefix+"synthetic.claim")} {
		changed := f.preparation
		changed.Plan.Config.Contracts.Path = path
		if err := validateBootstrapSuccessorPreparationPaths(changed, plans, requestPath, approvalPath); err == nil {
			t.Fatal("new custody overlaps an original approved input", path)
		}
	}
}
