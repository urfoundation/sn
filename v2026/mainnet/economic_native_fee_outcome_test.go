package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/nativefee"
)

// The owned approved test executable enters the real public command. Only its
// nested original-runtime peer remains synthetic, as in the retained fixtures.
func init() {
	if len(os.Args) > 1 && os.Args[0] == "urnetwork-native-fee-verifier" {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		code := runMain(ctx, os.Args[1:], os.Stdout, os.Stderr)
		cancel()
		os.Exit(code)
	}
}

// This reuses real signed receipt and GRANDPA fixtures. Its replay peer is
// synthetic and does not qualify the separately pinned original Wasm engine.
func nativeFeeOutcomeFixture(t *testing.T, name, mode string) (economicNativeFeeRequest, nativefee.NativePolicy, string) {
	t.Helper()
	contextRequest, fixture, job := historicalFeeContextTestFixture(t, name, mode)
	request, _, _ := economicNativeFeeTestRequestForContext(t, contextRequest, job)
	for _, transaction := range fixture.Expected.Transactions {
		if transaction.Receipt != nil && (name != "reverted" || transaction.Receipt.Status == 0) {
			return request, nativefee.NativePolicy(request.Policy), transaction.Hash
		}
	}
	t.Fatal("original signed target absent")
	return economicNativeFeeRequest{}, nativefee.NativePolicy{}, ""
}

func nativeFeeOutcomeRun(t *testing.T, request economicNativeFeeRequest, policy nativefee.NativePolicy, transaction string) (nativefee.Statement, int, string) {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(request.Context.Archive.Path), "owned-outcome-request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	policyRaw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMain(t.Context(), []string{"verify-native-fee-outcome", "--request", path, "--request-sha256", monitorReadDigest(raw), "--transaction", transaction, "--policy-json", string(policyRaw)}, &output, &diagnostic)
	var statement nativefee.Statement
	if code == 0 {
		if err := decodePlanJson(output.Bytes(), &statement); err != nil {
			t.Fatal(err)
		}
		if err := statement.Validate(policy, nativefee.Reference{Path: path, Sha256: monitorReadDigest(raw)}, transaction); err != nil {
			t.Fatal(err)
		}
	} else if output.Len() != 0 {
		t.Fatal("refused original proof published a partial outcome")
	}
	return statement, code, diagnostic.String()
}

func TestNativeFeeOutcomeCommandRetainsOriginalSignatureAndDebit(t *testing.T) {
	request, policy, transaction := nativeFeeOutcomeFixture(t, "success", "pair")
	statement, code, diagnostic := nativeFeeOutcomeRun(t, request, policy, transaction)
	if code != 0 || len(statement.RawTransaction) == 0 || statement.WithdrawalRao != "1000" || statement.RefundRao != "250" || statement.DebitRao != "750" || statement.ReceiptStatus != 1 || statement.ProofHash == "" {
		t.Fatal("complete original outcome was lost", code, diagnostic, statement)
	}
}

func TestNativeFeeOutcomeCommandRevertedCallRetainsDebit(t *testing.T) {
	request, policy, transaction := nativeFeeOutcomeFixture(t, "reverted", "pair")
	statement, code, diagnostic := nativeFeeOutcomeRun(t, request, policy, transaction)
	if code != 0 || statement.ReceiptStatus != 0 || statement.DebitRao != "750" {
		t.Fatal("reverted call erased original surviving fee", code, diagnostic)
	}
}

func TestNativeFeeOutcomeCommandUnknownRefundRefusesSettlement(t *testing.T) {
	request, policy, transaction := nativeFeeOutcomeFixture(t, "success", "missing")
	_, code, diagnostic := nativeFeeOutcomeRun(t, request, policy, transaction)
	if code == 0 || !strings.Contains(diagnostic, "remains unknown") {
		t.Fatal("unobserved refund became zero", code, diagnostic)
	}
}

func TestNativeFeeOutcomeCommandRequiresIndependentPolicy(t *testing.T) {
	request, policy, transaction := nativeFeeOutcomeFixture(t, "success", "pair")
	policy.ApprovalPublicKey = "0x" + strings.Repeat("12", 32)
	_, code, diagnostic := nativeFeeOutcomeRun(t, request, policy, transaction)
	if code == 0 || !strings.Contains(diagnostic, "cannot select its own") {
		t.Fatal("request selected its own authority", code, diagnostic)
	}
}

func TestNativeFeeOutcomeCommandRejectsWrongOriginalPayer(t *testing.T) {
	request, policy, transaction := nativeFeeOutcomeFixture(t, "success", "wrong-payer")
	_, code, diagnostic := nativeFeeOutcomeRun(t, request, policy, transaction)
	if code == 0 || !strings.Contains(diagnostic, "payer") {
		t.Fatal("original sender and native payer conflict was admitted", code, diagnostic)
	}
}

func TestNativeFeeOutcomeCommandDoesNotSubstituteAnotherTransaction(t *testing.T) {
	request, policy, _ := nativeFeeOutcomeFixture(t, "success", "pair")
	_, code, diagnostic := nativeFeeOutcomeRun(t, request, policy, "0x"+strings.Repeat("27", 32))
	if code == 0 || !strings.Contains(diagnostic, "remains unknown") {
		t.Fatal("unselected signature consumed another fee result", code, diagnostic)
	}
}

func nativeFeeOutcomeInvoke(t *testing.T, mode string) (*nativefee.Verified, nativefee.Authority, error) {
	t.Helper()
	request, policy, transaction := nativeFeeOutcomeFixture(t, "success", mode)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(request.Context.Archive.Path), "owned-invocation-request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	authority := nativefee.Authority{Verifier: nativefee.Reference{Path: request.Context.Engine.Path, Sha256: request.Context.Engine.Sha256}, NativePolicy: policy}
	verified, err := nativefee.Invoke(t.Context(), authority, nativefee.Reference{Path: path, Sha256: monitorReadDigest(raw)}, transaction, time.Minute)
	return verified, authority, err
}

func TestNativeFeeOutcomeOwnedInvocationExecutesOriginalProofs(t *testing.T) {
	verified, authority, err := nativeFeeOutcomeInvoke(t, "pair")
	if err != nil {
		t.Fatal(err)
	}
	if err := verified.Check(t.Context(), authority); err != nil {
		t.Fatal(err)
	}
	if verified.Facts().DebitRao != "750" || len(verified.Facts().RawTransaction) == 0 {
		t.Fatal("owned proof lost exact native debit or original signature")
	}
}

func TestNativeFeeOutcomeOwnedInvocationKeepsMissingRefundUnknown(t *testing.T) {
	verified, _, err := nativeFeeOutcomeInvoke(t, "missing")
	if err == nil || verified != nil {
		t.Fatal("missing original refund acquired an owned settlement result")
	}
}
