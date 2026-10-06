// Public CLI tests round-trip only synthetic approvals and public signatures.
// No production key, device, RPC write or service is admitted by these commands.
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
	"strings"
	"testing"
)

// A private fixture file returns its exact byte hash, independent of JSON seals.
func ownerRecycleTestFile(t *testing.T, directory, name string, data []byte) (string, string) {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return path, "sha256:" + hex.EncodeToString(digest[:])
}

// Public command output can be lost after export/import while the original
// journal remains authoritative; request verification needs no embedded paths.
func TestOwnerRecyclePublicCliRoundTrip(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	directory := filepath.Dir(f.config.Action.StatePath)
	inputRaw, _ := json.Marshal(f.input)
	inputPath, _ := ownerRecycleTestFile(t, directory, "input.json", inputRaw)
	var out, errOut bytes.Buffer
	if code := runOwnerRecycleCommand(f.storage.Context, []string{"plan", "--input", inputPath}, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	var unsigned ownerRecycleConfig
	if err := decodePlanJson(out.Bytes(), &unsigned); err != nil || unsigned.Signature != "" || unsigned.Action.RequestHash != f.config.Action.RequestHash {
		t.Fatal("plan inferred approval or changed action", err)
	}
	configRaw, _ := json.Marshal(f.config)
	configPath, _ := ownerRecycleTestFile(t, directory, "approved.json", configRaw)
	metadataPath, _ := ownerRecycleTestFile(t, directory, "metadata.hex", []byte(f.input.Metadata))
	ledgerPath, _ := ownerRecycleTestFile(t, directory, "metadata15.hex", []byte(f.input.LedgerMetadata))
	common := []string{"--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash}
	run := func(mode string, extra ...string) []byte {
		t.Helper()
		out.Reset()
		errOut.Reset()
		args := append([]string{mode}, common...)
		args = append(args, extra...)
		if code := runOwnerRecycleCommand(f.storage.Context, args, &out, &errOut); code != 0 {
			t.Fatal(mode, code, errOut.String())
		}
		return bytes.Clone(out.Bytes())
	}
	run("reserve")
	exported := run("export", "--metadata", metadataPath, "--ledger-metadata", ledgerPath)
	var request ownerRecycleSigningRequest
	if err := decodePlanJson(exported, &request); err != nil {
		t.Fatal(err)
	}
	requestPath, _ := ownerRecycleTestFile(t, directory, "portable.json", exported)
	trust := []string{"--request", requestPath, "--accept-request-hash", request.ContentHash, "--approval-key", f.key, "--owner-account-id", f.config.Action.Owner, "--expected-genesis", f.config.Action.Policy.GenesisHash}
	out.Reset()
	errOut.Reset()
	if code := runOwnerRecycleCommand(f.storage.Context, append([]string{"inspect-request"}, trust...), &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	proofPath, proofHash := ownerRecycleTestFile(t, directory, "synthetic-proof.bin", bytes.Repeat([]byte{29}, 330))
	out.Reset()
	errOut.Reset()
	args := append([]string{"ledger-plan"}, trust...)
	args = append(args, "--metadata-proof", proofPath, "--metadata-proof-sha256", proofHash)
	if code := runOwnerRecycleCommand(f.storage.Context, args, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	var transcript ownerLedgerTranscript
	if err := decodePlanJson(out.Bytes(), &transcript); err != nil || transcript.Signing || transcript.NetworkEffects || transcript.DeviceQualified {
		t.Fatal("offline transcript claimed execution", err)
	}
	signature := f.signature(t)
	response := append([]byte{0}, signature...)
	signaturePath, signatureHash := ownerRecycleTestFile(t, directory, "public-ledger-response.hex", []byte(hex.EncodeToString(response)+"\n"))
	importArgs := []string{"--signature", signaturePath, "--signature-file-sha256", signatureHash, "--accept-request-hash", request.ContentHash, "--ledger-response"}
	run("import", importArgs...)
	run("import", importArgs...)
	var record ownerRecycleRecord
	if err := decodePlanJson(run("status"), &record); err != nil || record.Phase != "signed" {
		t.Fatal("public signature not retained", err)
	}
	want, err := f.config.Action.signed(signature)
	if err != nil || record.RawExtrinsic != "0x"+hex.EncodeToString(want) {
		t.Fatal("CLI rewrote signed extrinsic", err)
	}
	// A portable request may be inspected after moving to an unrelated private
	// directory; only independently named inputs are opened on that computer.
	other := t.TempDir()
	os.Chmod(other, 0700)
	moved, _ := ownerRecycleTestFile(t, other, "request.json", exported)
	trust[1] = moved
	out.Reset()
	errOut.Reset()
	if code := runOwnerRecycleCommand(f.storage.Context, append([]string{"inspect-request"}, trust...), &out, &errOut); code != 0 {
		t.Fatal("portable inspection depended on host custody", errOut.String())
	}
}

// Ambiguous device output remains exported even past era death. Missing public
// bytes cannot be interpreted as an unsigned action safe to repeat or replace.
func TestOwnerRecycleUnreturnedRequestAndUnsafeStoreRefusals(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, false)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := custody.reconcile(context.Background(), nil); err == nil {
		t.Fatal("unreturned signature became expiry")
	}
	if _, err := custody.importSignature(rootObjectHash("foreign request"), f.signature(t)); err == nil {
		t.Fatal("foreign request signature imported")
	}
	if _, err := custody.importSignature(request.ContentHash, bytes.Repeat([]byte{7}, 64)); err == nil {
		t.Fatal("invalid owner signature imported")
	}
	record, err := custody.load()
	if err != nil || record.Phase != "exported" || record.Signature != "" {
		t.Fatal("refusal changed unresolved custody", err)
	}
	store.close()
	path := f.config.Action.StatePath
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, _ := ownerRecycleTestFile(t, filepath.Dir(path), "retained.json", raw)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(backup, path); err != nil {
		t.Fatal(err)
	}
	if _, err := openOwnerRecycleStore(f.config, f.key, false, f.storage.Context); err == nil {
		t.Fatal("journal symlink admitted")
	}
	os.Remove(path)
	os.WriteFile(path, raw, 0600)
	changed := f.config
	changed.Action.CustodyId = "synthetic-other-owner"
	changed.Action.RequestHash = ""
	changed.Action.RequestHash = rootObjectHash(changed.Action)
	changed.Signature = hex.EncodeToString(ed25519.Sign(f.approval, changed.signingBytes()))
	// The approved action is immutable even under a different independently
	// signed config; its existing marker cannot be reused as a new reservation.
	if _, err := openOwnerRecycleStore(changed, f.key, false, f.storage.Context); err == nil {
		t.Fatal("journal rebound to another approval")
	}
}

// Invalid Ledger framing is rejected without guessing away variant/status bytes.
func TestOwnerRecyclePublicImportRejectsLedgerFraming(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	store.close()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(f.config.Action.StatePath)
	configRaw, _ := json.Marshal(f.config)
	configPath, _ := ownerRecycleTestFile(t, directory, "config.json", configRaw)
	signature := f.signature(t)
	for _, response := range [][]byte{signature, append([]byte{1}, signature...), append(append([]byte{0}, signature...), 0x90, 0)} {
		path, digest := ownerRecycleTestFile(t, directory, "response.hex", []byte(hex.EncodeToString(response)))
		args := []string{"import", "--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash, "--accept-request-hash", request.ContentHash, "--signature", path, "--signature-file-sha256", digest, "--ledger-response"}
		var out, errOut bytes.Buffer
		if code := runOwnerRecycleCommand(f.storage.Context, args, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "MultiSignature") {
			t.Fatal("ambiguous Ledger response accepted", code, errOut.String())
		}
	}
}

// A chain adapter cannot rewrite an already retained fee/dispatch while repairing
// missing readback. Explicit continuations force the rejection deterministically.
type ownerRecycleTestReconciler struct {
	evidence ownerRecycleReconciliation
	err      error
}

// Only value copies escape the synthetic adapter.
func (self ownerRecycleTestReconciler) reconcile(context.Context, ownerRecycleSigningRequest, []byte) (ownerRecycleReconciliation, error) {
	return self.evidence, self.err
}

// Cancellation and changed financial evidence preserve original signed state.
func TestOwnerRecycleContinuationCannotReplaceReceipt(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	chain, _, request, signed := ownerRecycleTestChain(t, f, 2, true)
	evidence, err := chain.reconcile(context.Background(), request, signed)
	if err != nil {
		t.Fatal(err)
	}
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	custody.export(f.input.Metadata, f.input.LedgerMetadata)
	custody.importSignature(request.ContentHash, f.signature(t))
	gap := ownerTrimTestCopy(t, evidence)
	gap.Readback = nil
	gap.ReadbackIssue = "synthetic archive outage"
	if _, err := custody.reconcile(context.Background(), ownerRecycleTestReconciler{evidence: gap}); err != nil {
		t.Fatal(err)
	}
	changed := ownerTrimTestCopy(t, evidence)
	changed.Receipt.ActualFeeRao++
	if _, err := custody.reconcile(context.Background(), ownerRecycleTestReconciler{evidence: changed}); err == nil {
		t.Fatal("readback retry changed original fee")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := custody.reconcile(cancelled, ownerRecycleTestReconciler{evidence: evidence}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	record, err := custody.load()
	if err != nil || record.Phase != "finalized-readback-pending" || rootObjectHash(record.Reconciliation.Receipt) != rootObjectHash(evidence.Receipt) {
		t.Fatal("failed retry changed journal", err)
	}
}
