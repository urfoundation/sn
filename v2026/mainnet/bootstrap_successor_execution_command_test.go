// A full public v3 fixture reconstructs genuine eight-action custody before
// execution claim creation. It never exercises a live or synthetic send adapter.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Every original owner is real fixture code. New public signatures are synthetic
// and the resulting execution custody remains explicitly canonical-gated.
func TestBootstrapSuccessorExecutionCommandReconstructsAndRetainsV3Custody(t *testing.T) {
	f := newBootstrapSuccessorCommandFixture(t)
	bootstrapSuccessorCommandTestComplete(t, f)
	request := bootstrapSuccessorTestRequest(f.contracts.config, f.preparation.Plan.ContentHash)
	relayer, err := crypto.ToECDSA(crypto.Keccak256([]byte("synthetic public successor relayer")))
	if err != nil {
		t.Fatal(err)
	}
	request.IntendedRelayer, request.IntendedRelayerNonce = crypto.PubkeyToAddress(relayer.PublicKey), 42
	request.IntendedSafeNonce = "17"
	requestPath := filepath.Join(filepath.Dir(f.path), "synthetic-execution-original-request.json")
	bootstrapRootTestWrite(t, requestPath, request)
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "contract-successor-preview", &stdout, &stderr, "--request", requestPath); code != 0 {
		t.Fatal("preparation preview", code, stderr.String())
	}
	var preparationPreview bootstrapSuccessorPreparationPreview
	if err := decodePlanJson(stdout.Bytes(), &preparationPreview); err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("synthetic contract independent approval"))
	key := ed25519.NewKeyFromSeed(seed[:])
	preparationApproval := bootstrapSuccessorPreparationTestSign(t, preparationPreview.Plan, key)
	preparationRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "synthetic-execution-preparation-approval.json"), preparationApproval)
	stdout.Reset()
	stderr.Reset()
	if code := f.command(t.Context(), "contract-successor-prepare", &stdout, &stderr, "--request", requestPath, "--approval", preparationRef.Path,
		"--approval-sha256", preparationRef.Sha256, "--accept-successor-hash", preparationPreview.PlanHash); code != 0 {
		t.Fatal("preparation claim", code, stderr.String())
	}
	pin, archivePath, archiveMembers := safeReleaseTestInputs(t, "1.4.1", "Safe")
	last := preparationPreview.Plan.Proposal.AdoptedActions[7].Receipt
	safeRequest := bootstrapSuccessorSafeRequest{Schema: bootstrapSuccessorSafeRequestSchema, PreparationPlanHash: preparationPreview.PlanHash,
		PreparationRecordHash: bootstrapSuccessorSafeTestRecord(preparationApproval).ContentHash,
		Version:               "1.4.1", Variant: "Safe", Archive: planFileReference{Path: archivePath, Sha256: pin.ArchiveSha256}, RelayerGas: 100000,
		RelayerFeeCapWei: "10", RelayerTipCapWei: "1", StartNativeNumber: last.NativeNumber, StartNativeHash: last.NativeHash, ValidThroughNative: last.NativeNumber + 100}
	safePath := filepath.Join(filepath.Dir(f.path), "synthetic-execution-safe-request.json")
	bootstrapRootTestWrite(t, safePath, safeRequest)
	stdout.Reset()
	stderr.Reset()
	if code := f.command(t.Context(), "contract-successor-safe-review", &stdout, &stderr, "--request", requestPath, "--safe-request", safePath); code != 3 {
		t.Fatal("Safe review", code, stderr.String())
	}
	var review bootstrapSuccessorSafeReview
	if err := decodePlanJson(stdout.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	var profile *safeExecutionProfile
	for _, artifact := range pin.Artifacts {
		if artifact.Name == "Safe" {
			profile, err = newSafeExecutionProfile("1.4.1", "Safe", archiveMembers[artifact.ArchivePath])
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	oracle := newSafeExecutionFixture(t, "1.4.1", "Safe")
	var signatures []byte
	for _, owner := range oracle.keys[:2] {
		raw, err := crypto.Sign(review.Transaction.Digest[:], owner)
		if err != nil {
			t.Fatal(err)
		}
		raw[64] += 27
		signatures = append(signatures, raw...)
	}
	registry := bootstrapSuccessorExecutionTestDirectory(t)
	executionRequest := bootstrapSuccessorExecutionRequest{Schema: bootstrapSuccessorExecutionRequestSchema, SafeReviewHash: review.ContentHash,
		RegistryDirectory: registry, Owners: oracle.owners, Singleton: common.BytesToAddress(crypto.Keccak256([]byte("synthetic public singleton"))),
		SafeSignatures: bootstrapSuccessorExecutionTestRaw(t, "synthetic-public-safe-signatures.bin", signatures)}
	draft := bootstrapSuccessorExecutionPlan{Review: review, Request: executionRequest, SafeSignatures: "0x" + hex.EncodeToString(signatures)}
	outer, err := draft.outer(profile)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := outer.unsigned()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(mainnetEvmChainId)), relayer)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	executionRequest.RelayerTransaction = bootstrapSuccessorExecutionTestRaw(t, "synthetic-public-relayer.bin", signed)
	executionPath := filepath.Join(filepath.Dir(f.path), "synthetic-public-execution-request.json")
	bootstrapRootTestWrite(t, executionPath, executionRequest)
	invoke := func(command string, writer *bytes.Buffer, extra ...string) (int, string) {
		var diagnostic bytes.Buffer
		code := f.command(t.Context(), command, writer, &diagnostic,
			append([]string{"--request", requestPath, "--safe-request", safePath, "--execution-request", executionPath}, extra...)...)
		return code, diagnostic.String()
	}
	original := bootstrapSuccessorPreparationTestFiles(t, f.config.RunDirectory)
	f.contracts.stateLock.Lock()
	counts, writes := maps.Clone(f.contracts.counts), len(f.contracts.writes)
	f.contracts.stateLock.Unlock()
	f.census.stateLock.Lock()
	censusCounts := maps.Clone(f.census.methodCounts)
	f.census.stateLock.Unlock()
	stdout.Reset()
	if code, diagnostic := invoke("contract-successor-execution-preview", &stdout); code != 0 {
		t.Fatal("execution preview", code, diagnostic)
	}
	var preview struct {
		Schema       string                          `json:"schema"`
		Plan         bootstrapSuccessorExecutionPlan `json:"plan"`
		PlanHash     string                          `json:"execution_plan_hash"`
		SigningBytes string                          `json:"execution_signing_bytes"`
	}
	if err := decodePlanJson(stdout.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	approval := bootstrapSuccessorExecutionTestSign(t, preview.Plan, key, profile)
	approvalRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "synthetic-public-execution-approval.json"), approval)
	args := []string{"--approval", approvalRef.Path, "--approval-sha256", approvalRef.Sha256, "--accept-execution-hash", preview.PlanHash}
	for _, command := range []string{"contract-successor-execution-claim", "contract-successor-execution-resume", "contract-successor-execution-resume"} {
		stdout.Reset()
		if code, diagnostic := invoke(command, &stdout, args...); code != 0 {
			t.Fatal("public execution custody", command, code, diagnostic)
		}
		var result bootstrapSuccessorExecutionResult
		if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.ExecutionApprovalVerified || !result.LocalCustodyComplete || result.CanonicalAdoptionVerified || result.SubmissionAttempted || result.InstallationComplete || result.ActivationReady || result.CumulativeAttempts != 8 {
			t.Fatal("public execution custody manufactured chain authority")
		}
	}
	for name, raw := range original {
		retained, err := os.ReadFile(filepath.Join(f.config.RunDirectory, name))
		if err != nil || string(retained) != raw {
			t.Fatal("public execution rewrote original custody", name, err)
		}
	}
	for _, extra := range [][]string{{"--online"}, {"--submit"}, {"--rpc", "https://synthetic.example"}, {"--signer", approvalRef.Path}} {
		stdout.Reset()
		if code, _ := invoke("contract-successor-execution-resume", &stdout, append(args, extra...)...); code != 2 || stdout.Len() != 0 {
			t.Fatal("execution custody bypassed explicit canonical authorization", extra, code)
		}
	}
	path := filepath.Join(f.config.RunDirectory, bootstrapContractStateFile(7))
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	code, diagnostic := invoke("contract-successor-execution-resume", &stdout, args...)
	if err := os.WriteFile(path, saved, 0600); err != nil {
		t.Fatal(err)
	}
	if code != 1 || stdout.Len() != 0 {
		t.Fatal("public execution trusted a missing adopted receipt", code, diagnostic)
	}
	stderr.Reset()
	if code := f.command(t.Context(), "contract-successor-execution-resume", ioFailureWriter{}, &stderr,
		append([]string{"--request", requestPath, "--safe-request", safePath, "--execution-request", executionPath}, args...)...); code != 1 {
		t.Fatal("failed execution output reported success", code)
	}
	stdout.Reset()
	if code, diagnostic := invoke("contract-successor-execution-resume", &stdout, args...); code != 0 {
		t.Fatal("output failure lost execution custody", code, diagnostic)
	}
	f.contracts.stateLock.Lock()
	unchanged := maps.Equal(counts, f.contracts.counts) && writes == len(f.contracts.writes)
	f.contracts.stateLock.Unlock()
	f.census.stateLock.Lock()
	unchanged = unchanged && maps.Equal(censusCounts, f.census.methodCounts)
	f.census.stateLock.Unlock()
	if !unchanged {
		t.Fatal("public execution custody contacted the original network")
	}
}
