package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/nativefee"
)

// This test-only export lets the Server public model test consume an actual
// owned verifier outcome without exposing a production constructor. The nested
// runtime peer is synthetic; signed receipt/GRANDPA verification is real.
// Qualification selects a persistent output directory. An ordinary package
// test uses its own temporary output and performs the same proof checks.
func TestNativeFeeOutcomeExportSettlementFixtures(t *testing.T) {
	directory := os.Getenv("URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY")
	if directory == "" {
		directory = t.TempDir()
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		t.Fatal("set URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY to the selected private fixture output directory")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("native fee fixture output must already be a private directory", err)
	}
	var original []byte
	for _, mode := range []string{"pair", "missing", "fractional-conflict"} {
		exportNativeFeeSettlementFixture(t, directory, mode)
		raw, err := os.ReadFile(filepath.Join(directory, mode+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest nativeFeeSettlementFixtureManifest
		if err := decodePlanJson(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		if original == nil {
			original = bytes.Clone(manifest.RawTransaction)
		} else if !bytes.Equal(original, manifest.RawTransaction) {
			t.Fatal("exported outcomes changed the original signed transaction")
		}
		verified, err := nativefee.Invoke(t.Context(), manifest.Authority, manifest.Request, manifest.TransactionHash, time.Minute)
		if mode == "missing" {
			if err == nil || verified != nil {
				t.Fatal("exported missing refund acquired settlement authority")
			}
			continue
		}
		if err != nil {
			t.Fatal("exported original proof does not execute", err)
		}
		withdrawal, debit := "1000", "750"
		if mode == "fractional-conflict" {
			withdrawal, debit = "1001", "751"
		}
		facts := verified.Facts()
		if facts.TransactionHash != manifest.TransactionHash || facts.WithdrawalRao != withdrawal || facts.RefundRao != "250" || facts.DebitRao != debit || len(facts.Originals) != 7 {
			t.Fatal("exported original outcome differs")
		}
		retained := 0
		if err := verified.RetainOriginals(t.Context(), func(_ string, _ nativefee.Reference, reader io.Reader) error {
			retained++
			_, err := io.Copy(io.Discard, reader)
			return err
		}); err != nil || retained != 7 {
			t.Fatal("exported original proof closure is incomplete", err, retained)
		}
	}
}

type nativeFeeSettlementFixtureManifest struct {
	Schema            string              `json:"schema"`
	Authority         nativefee.Authority `json:"authority"`
	Request           nativefee.Reference `json:"request"`
	TransactionHash   string              `json:"transaction_hash"`
	RawTransaction    []byte              `json:"raw_transaction"`
	Sender            string              `json:"sender"`
	Nonce             uint64              `json:"nonce"`
	RuntimeCodeSha256 string              `json:"runtime_code_sha256"`
	NativeBlockNumber uint64              `json:"native_block_number"`
	ReceiptStatus     uint64              `json:"receipt_status"`
}

func exportNativeFeeSettlementFixture(t *testing.T, directory, mode string) {
	t.Helper()
	contextRequest, fixture, job := historicalFeeContextTestFixture(t, "success", mode)
	request, approval, key := economicNativeFeeTestRequestForContext(t, contextRequest, job)
	root := filepath.Join(directory, mode)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		name      string
		reference *planFileReference
	}{
		{name: "archive.json", reference: &request.Context.Archive}, {name: "collection.json", reference: &request.Context.Collection}, {name: "checkpoint.json", reference: &request.Context.Checkpoint}, {name: "finality.json", reference: &request.Context.FinalityProof}, {name: "job.json", reference: &request.Context.Job},
	} {
		raw, err := os.ReadFile(input.reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		if monitorReadDigest(raw) != input.reference.Sha256 {
			t.Fatal("fixture original changed before export")
		}
		input.reference.Path = filepath.Join(root, input.name)
		if err := os.WriteFile(input.reference.Path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	approval.ContextRequestHash = rootObjectHash(request.Context)
	economicNativeFeeTestSign(t, &request, &approval, key)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(root, "request.json")
	if err := os.WriteFile(requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := nativeFeeSettlementFixtureManifest{Schema: "urnetwork-native-fee-settlement-test-fixture-v1", Authority: nativefee.Authority{Verifier: nativefee.Reference(request.Context.Engine), NativePolicy: nativefee.NativePolicy(request.Policy)}, Request: nativefee.Reference{Path: requestPath, Sha256: monitorReadDigest(raw)}, RuntimeCodeSha256: economicNativeFeeSha(job.RuntimeCodeSha256), NativeBlockNumber: fixture.Expected.Blocks[0].NativeContexts[0].NativeBlock.Number}
	for _, transaction := range fixture.Expected.Transactions {
		if transaction.Receipt != nil {
			manifest.TransactionHash, manifest.Sender, manifest.Nonce, manifest.ReceiptStatus = transaction.Hash, transaction.Sender, transaction.Nonce, transaction.Receipt.Status
			for _, original := range fixture.Archive.Transactions {
				if original.Hash == transaction.Hash {
					manifest.RawTransaction = original.Raw
				}
			}
			break
		}
	}
	if len(manifest.RawTransaction) == 0 {
		t.Fatal("export lost original signed transaction")
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, mode+".json"), manifestRaw, 0600); err != nil {
		t.Fatal(err)
	}
}
