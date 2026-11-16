// Actual signed receipt tries and GRANDPA ancestry exercise the public producer.
// The Go protocol peer remains explicitly synthetic; the separately selected
// original-Wasm roots qualify its engine and rollback observations.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func economicNativeFeeTestRequest(t *testing.T, fixture, mode string) (economicNativeFeeRequest, economicNativeFeeApproval, ed25519.PrivateKey) {
	t.Helper()
	input, _, job := historicalFeeContextTestFixture(t, fixture, mode)
	return economicNativeFeeTestRequestForContext(t, input, job)
}

func economicNativeFeeTestRequestForContext(t *testing.T, input historicalFeeContextRequest, job historicalReplayJob) (economicNativeFeeRequest, economicNativeFeeApproval, ed25519.PrivateKey) {
	t.Helper()
	profileRaw, err := json.Marshal(job.ObservationProfile)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x67}, ed25519.SeedSize))
	policy := economicNativeFeePolicy{ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), Genesis: input.Genesis, EvmChainId: input.EvmChainId, EngineSha256: input.Engine.Sha256, CheckpointSha256: input.Checkpoint.Sha256, ReviewSha256: economicNativeFeeSha(job.ObservationProfile.SourceReviewSha256), ProfileSha256: monitorReadDigest(profileRaw)}
	request := economicNativeFeeRequest{Schema: economicNativeFeeEvidenceSchema, Policy: policy, Context: input}
	approval := economicNativeFeeApproval{Schema: economicNativeFeeApprovalSchema, PolicyHash: rootObjectHash(policy), ContextRequestHash: rootObjectHash(input), RuntimeCodeSha256: economicNativeFeeSha(job.RuntimeCodeSha256), MetadataSha256: economicNativeFeeSha(*job.ObservationProfile.MetadataSha256), ParentHash: economicNativeFeeHash(job.ParentHash), ChildHash: economicNativeFeeHash(job.ChildHash), Semantics: "original-native-withdrawal-refund-and-ethereum-context-with-rollback-v1"}
	economicNativeFeeTestSign(t, &request, &approval, key)
	return request, approval, key
}

func economicNativeFeeTestSign(t *testing.T, request *economicNativeFeeRequest, approval *economicNativeFeeApproval, key ed25519.PrivateKey) {
	t.Helper()
	message, err := approval.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	approval.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(request.Context.Archive.Path), "fee-approval.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	request.Approval = planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
}

func economicNativeFeeTestRun(t *testing.T, ctx context.Context, request economicNativeFeeRequest) (economicNativeFeeEvidence, int, string) {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(request.Context.Archive.Path), "admitted-fee-request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	code := runMain(ctx, []string{"verify-admitted-native-fees", "--request", path, "--request-sha256", monitorReadDigest(raw)}, &out, &diagnostic)
	var result economicNativeFeeEvidence
	if code == 0 {
		if err := decodePlanJson(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	} else if out.Len() != 0 {
		t.Fatal("refused native fee producer published partial evidence")
	}
	return result, code, diagnostic.String()
}

func TestEconomicNativeFeePublicAdmitsOriginalWithdrawalRefundOnly(t *testing.T) {
	request, _, _ := economicNativeFeeTestRequest(t, "success", "pair")
	result, code, diagnostic := economicNativeFeeTestRun(t, t.Context(), request)
	if code != 0 || result.Context == nil || result.AuthenticatedTransactions != 1 || result.WholeBlockCensusComplete || result.SpendingAuthorized {
		t.Fatal("approved native fee pair was not retained in its original scope", code, diagnostic)
	}
	found := 0
	for _, transaction := range result.Context.Transactions {
		if transaction.FeeAuthenticated {
			found++
			if transaction.ActualWithdrawalRao == nil || *transaction.ActualWithdrawalRao != "1000" || transaction.ActualRefundRao == nil || *transaction.ActualRefundRao != "250" || transaction.ActualDebitRao == nil || *transaction.ActualDebitRao != "750" || transaction.Receipt == nil {
				t.Fatal("native fee producer substituted receipt cost for exact W/R", transaction)
			}
		}
	}
	if found != 1 {
		t.Fatal("original signed fee recipient was repeated or lost")
	}
}

func TestEconomicNativeFeePublicMissingRefundRemainsUnknown(t *testing.T) {
	request, _, _ := economicNativeFeeTestRequest(t, "success", "missing")
	result, code, diagnostic := economicNativeFeeTestRun(t, t.Context(), request)
	if code != 0 || result.SelectedCensusComplete || result.WithdrawalRao != nil || result.RefundRao != nil || result.DebitRao != nil || result.AuthenticatedTransactions != 0 {
		t.Fatal("fee approval invented a missing native refund", code, diagnostic)
	}
	for _, item := range result.Context.Transactions {
		if item.FeeAuthenticated || item.ActualRefundRao != nil {
			t.Fatal("missing refund became an authenticated zero")
		}
	}
}

func TestEconomicNativeFeePublicObservedZeroRefundStaysExplicit(t *testing.T) {
	request, _, _ := economicNativeFeeTestRequest(t, "success", "zero")
	result, code, diagnostic := economicNativeFeeTestRun(t, t.Context(), request)
	if code != 0 || result.AuthenticatedTransactions != 1 {
		t.Fatal("explicit native zero refund was lost", code, diagnostic)
	}
	for _, item := range result.Context.Transactions {
		if item.FeeAuthenticated && (item.ActualRefundRao == nil || *item.ActualRefundRao != "0" || item.ActualDebitRao == nil || *item.ActualDebitRao != "1000") {
			t.Fatal("observed native zero refund changed", item)
		}
	}
}

func TestEconomicNativeFeePublicRevertedCallRetainsSurvivingFee(t *testing.T) {
	request, _, _ := economicNativeFeeTestRequest(t, "reverted", "pair")
	result, code, diagnostic := economicNativeFeeTestRun(t, t.Context(), request)
	if code != 0 || result.AuthenticatedTransactions != 1 {
		t.Fatal("reverted call lost surviving original fee", code, diagnostic)
	}
	for _, item := range result.Context.Transactions {
		if item.FeeAuthenticated && (item.Receipt == nil || item.Receipt.Status != 0 || item.ActualDebitRao == nil || *item.ActualDebitRao != "750") {
			t.Fatal("reverted receipt fabricated zero fee", item)
		}
	}
}

func TestEconomicNativeFeePublicRefusesChangedSemanticAuthority(t *testing.T) {
	for _, change := range []string{"signature", "profile", "checkpoint", "engine", "semantics", "parent", "request"} {
		request, approval, key := economicNativeFeeTestRequest(t, "success", "pair")
		switch change {
		case "signature":
			key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, ed25519.SeedSize))
		case "profile":
			request.Policy.ProfileSha256 = "sha256:" + strings.Repeat("13", 32)
		case "checkpoint":
			request.Policy.CheckpointSha256 = "sha256:" + strings.Repeat("13", 32)
		case "engine":
			request.Policy.EngineSha256 = "sha256:" + strings.Repeat("13", 32)
		case "semantics":
			approval.Semantics = "receipt-gas-estimate"
		case "parent":
			approval.ParentHash = "0x" + strings.Repeat("13", 32)
		case "request":
			approval.ContextRequestHash = "sha256:" + strings.Repeat("13", 32)
		}
		economicNativeFeeTestSign(t, &request, &approval, key)
		if _, code, diagnostic := economicNativeFeeTestRun(t, t.Context(), request); code == 0 {
			t.Fatal("changed independent native fee authority was accepted", change, diagnostic)
		}
	}
}

func TestEconomicNativeFeePublicKeepsPayerAndNativeProofJoins(t *testing.T) {
	for _, mode := range []string{"wrong-payer", "wrong-state", "wrong-parent"} {
		request, _, _ := economicNativeFeeTestRequest(t, "success", mode)
		if _, code, diagnostic := economicNativeFeeTestRun(t, t.Context(), request); code == 0 {
			t.Fatal("native fee authority waived original cryptographic context", mode, diagnostic)
		}
	}
}

func TestEconomicNativeFeePublicCancellationPreservesCause(t *testing.T) {
	request, _, _ := economicNativeFeeTestRequest(t, "success", "pair")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	value, err := runEconomicNativeFeeEvidence(ctx, request, time.Minute, historicalReplayHooks{})
	if value != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("native fee producer lost cancellation or published partial authority", err)
	}
}
