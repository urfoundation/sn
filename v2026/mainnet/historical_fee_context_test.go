// Real signed-receipt tries and weighted GRANDPA certificates come from the
// pinned Server fixture exporter. Only the process protocol peer is synthetic;
// it tests the Go join, not Wasm execution or an admitted runtime.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/server/v2026/strecovery"
	"golang.org/x/crypto/blake2b"
)

type historicalFeeContextFixture struct {
	Archive      *strecovery.Archive                  `json:"archive"`
	Collection   *strecovery.ReceiptCollection        `json:"collection"`
	Checkpoint   *strecovery.NativeFinalityCheckpoint `json:"checkpoint"`
	Proof        *strecovery.ReceiptFinalityProof     `json:"proof"`
	Expected     *strecovery.ReceiptFeeContexts       `json:"expected"`
	ParentHeader string                               `json:"parent_header"`
	ChildHeader  string                               `json:"child_header"`
}

type historicalFeeContextPeerInput struct {
	Source      historicalReplayAddress `json:"source"`
	Transaction historicalReplayDigest  `json:"transaction"`
	ParentRoot  historicalReplayDigest  `json:"parent_root"`
	ChildRoot   historicalReplayDigest  `json:"child_root"`
	Mode        string                  `json:"mode"`
}

// This branch exists only in the Go test binary's existing synthetic engine.
// The production supervisor still invokes the separately pinned Rust image.
func historicalFeeContextSyntheticPeer(job historicalReplayJob, report *historicalReplayReport) error {
	if len(job.ProofNodesHex) != 1 {
		return errors.New("synthetic context peer requires one bounded input")
	}
	raw, err := historicalReplayHex(job.ProofNodesHex[0], 4096)
	if err != nil {
		return err
	}
	var input historicalFeeContextPeerInput
	if err := decodePlanJson(raw, &input); err != nil {
		return err
	}
	report.ParentStateRoot, report.ChildStateRoot = input.ParentRoot, input.ChildRoot
	if job.ObservationProfile == nil {
		return nil
	}
	traceJob := job
	switch input.Mode {
	case "missing":
		traceJob.RuntimeCodeHex = "0xf0"
	case "zero":
		traceJob.RuntimeCodeHex = "0xf1"
	}
	trace := historicalObservationTestTrace(traceJob)
	if input.Mode == "fractional-conflict" {
		// The same original signature has a contradictory complete native
		// pair. Its odd debit cannot map exactly under a signed 1/2 ratio.
		withdrawal, debit := "1001", "751"
		trace.FeeEvents.Candidates[0].WithdrawalRao = &withdrawal
		trace.FeeEvents.Candidates[0].DebitRao = &debit
		for index := range trace.FeeEvents.Events {
			if trace.FeeEvents.Events[index].Purpose == "fee-withdraw" {
				trace.FeeEvents.Events[index].AmountRao = &withdrawal
			}
		}
	}
	payer := historicalReplayDigest(blake2b.Sum256(append([]byte("evm:"), input.Source[:]...)))
	for index := range trace.FeeEvents.Events {
		event := &trace.FeeEvents.Events[index]
		if event.Source != nil {
			event.Source = &input.Source
			event.TransactionHash = &input.Transaction
		}
		if event.Payer != nil {
			event.Payer = &payer
		}
	}
	trace.FeeEvents.Candidates[0].Payer = payer
	trace.FeeEvents.Candidates[0].TransactionHash = input.Transaction
	report.HookObservations = trace
	return nil
}

func historicalFeeContextTestFixture(t *testing.T, name, mode string) (historicalFeeContextRequest, *historicalFeeContextFixture, historicalReplayJob) {
	t.Helper()
	return historicalFeeContextTestFixtureAt(t, filepath.Join("testdata", "historical-fee-context", name+".json"), name, mode)
}

func historicalFeeContextTestFixtureAt(t *testing.T, path, name, mode string) (historicalFeeContextRequest, *historicalFeeContextFixture, historicalReplayJob) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture historicalFeeContextFixture
	if err := decodePlanJson(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Archive == nil || fixture.Collection == nil || fixture.Checkpoint == nil || fixture.Proof == nil || fixture.Expected == nil || len(fixture.Expected.Blocks) != 1 || len(fixture.Expected.Blocks[0].NativeContexts) != 1 {
		t.Fatal("complete independently exported proof fixture is absent")
	}
	contexts, err := strecovery.VerifyReceiptFeeContexts(t.Context(), fixture.Archive, fixture.Collection, fixture.Checkpoint, fixture.Proof)
	if err != nil || contexts == nil || !reflect.DeepEqual(contexts, fixture.Expected) {
		t.Fatal("original synthetic cryptographic fixture no longer verifies", err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) planFileReference {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
	}
	engine := historicalObservationTestRequest(t, "0xe0")
	jobRaw, err := os.ReadFile(engine.Job.Path)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := decodePlanJson(jobRaw, &job); err != nil {
		t.Fatal(err)
	}
	parse := func(value string) historicalReplayDigest {
		t.Helper()
		digest, err := historicalCaptureHash(value)
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}
	native := contexts.Blocks[0].NativeContexts[0]
	if native.NativeParentStateRoot == nil {
		t.Fatal("fixture omitted exact native parent root")
	}
	var transaction *strecovery.ReceiptFeeTransactionContext
	for index := range contexts.Transactions {
		item := &contexts.Transactions[index]
		if item.Receipt != nil && (name != "reverted" || item.Receipt.Status == 0) {
			transaction = item
			break
		}
	}
	if transaction == nil {
		t.Fatal("fixture omitted the selected signed receipt")
	}
	address, err := rootReceiptHex(transaction.Sender, 20)
	if err != nil || len(address) != 20 {
		t.Fatal("fixture sender width differs", err)
	}
	input := historicalFeeContextPeerInput{Transaction: parse(transaction.Hash), ParentRoot: parse(*native.NativeParentStateRoot), ChildRoot: parse(native.NativeStateRoot), Mode: mode}
	copy(input.Source[:], address)
	if mode == "wrong-payer" {
		input.Source[0] ^= 1
	}
	if mode == "wrong-state" {
		input.ChildRoot[0] ^= 1
	}
	if mode == "unselected" {
		input.Transaction[0] ^= 1
	}
	inputRaw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	job.ParentHeaderHex, job.ChildHeaderHex = fixture.ParentHeader, fixture.ChildHeader
	job.ParentHash, job.ChildHash = parse(native.NativeParentHash), parse(native.NativeBlock.Hash)
	if mode == "wrong-parent" {
		job.ParentHash[0] ^= 1
	}
	job.RuntimeCodeHex = "0xfd"
	job.ExtrinsicsHex = []string{"0x00"}
	job.ProofNodesHex = []string{"0x" + hex.EncodeToString(inputRaw)}
	if mode == "no-profile" {
		job.ObservationProfile = nil
	}
	historicalObservationTestWrite(t, &engine, job)
	request := historicalFeeContextRequest{Schema: historicalFeeContextSchema, Genesis: fixture.Archive.Selection.Genesis, EvmChainId: fixture.Archive.Selection.ChainId,
		Archive: write("archive.json", fixture.Archive), Collection: write("collection.json", fixture.Collection), Checkpoint: write("checkpoint.json", fixture.Checkpoint), FinalityProof: write("proof.json", fixture.Proof), Engine: engine.Engine, Job: engine.Job}
	return request, &fixture, job
}

func historicalFeeContextTestRun(t *testing.T, ctx context.Context, request historicalFeeContextRequest) (int, []byte, string) {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(request.Archive.Path), "request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(ctx, []string{"verify-historical-fee-context", "--request", path, "--request-sha256", monitorReadDigest(raw)}, &stdout, &stderr)
	return code, stdout.Bytes(), stderr.String()
}

func TestHistoricalFeeContextPublicJoinRetainsSignedHistoryAndUnknownAuthority(t *testing.T) {
	request, fixture, _ := historicalFeeContextTestFixture(t, "success", "pair")
	code, raw, diagnostic := historicalFeeContextTestRun(t, t.Context(), request)
	var report historicalFeeContextReport
	if code != 0 || decodePlanJson(raw, &report) != nil || report.ContextProof == nil || report.Replay == nil {
		t.Fatal("actual receipt/native proof and owned process join failed", code, diagnostic)
	}
	if !reflect.DeepEqual(report.ContextProof, fixture.Expected) || len(report.Transactions) != len(fixture.Archive.Transactions) || report.JoinedCandidates != 1 || report.UnselectedCandidates != 0 ||
		report.AuthorityCheckpointAuthenticated || report.RuntimeSourceAuthenticated || report.FinalityAuthenticated || report.NativeFeesAuthenticated || report.SpendingAuthorized {
		t.Fatal("join discarded signatures or promoted relative proof into authority")
	}
	matched := 0
	for index, transaction := range report.Transactions {
		original := fixture.Archive.Transactions[index]
		if transaction.TransactionHash != original.Hash || transaction.Role != original.Role || transaction.Sender != original.Sender || transaction.Nonce != original.Nonce || !reflect.DeepEqual(transaction.Origins, original.Origins) ||
			transaction.ActualWithdrawalRao != nil || transaction.ActualRefundRao != nil || transaction.ActualDebitRao != nil || transaction.FeeAuthenticated {
			t.Fatal("original attempt provenance or unknown fee authority changed")
		}
		if transaction.Candidate != nil {
			matched++
			if transaction.Candidate.DebitRao == nil || *transaction.Candidate.DebitRao != "750" || transaction.State != "observed-pair-unadmitted" || transaction.NativeBlock == nil {
				t.Fatal("exact process candidate did not join the proved native context")
			}
		}
	}
	if matched != 1 {
		t.Fatal("candidate was lost or allocated to multiple signatures")
	}
}

func TestHistoricalFeeContextPublicMissingRefundIsNotZero(t *testing.T) {
	for _, mode := range []string{"missing", "zero", "no-profile"} {
		request, _, _ := historicalFeeContextTestFixture(t, "success", mode)
		code, raw, diagnostic := historicalFeeContextTestRun(t, t.Context(), request)
		var report historicalFeeContextReport
		if code != 0 || decodePlanJson(raw, &report) != nil {
			t.Fatal("valid unresolved/zero context did not return", mode, code, diagnostic)
		}
		found := false
		for _, transaction := range report.Transactions {
			candidate := transaction.Candidate
			if candidate == nil {
				continue
			}
			found = true
			if mode == "missing" && (candidate.RefundRao != nil || candidate.DebitRao != nil || transaction.State != "refund-unobserved") ||
				mode == "zero" && (candidate.RefundRao == nil || *candidate.RefundRao != "0" || candidate.DebitRao == nil || *candidate.DebitRao != "1000") {
				t.Fatal("missing refund and explicit zero were conflated", mode, candidate)
			}
		}
		if found != (mode != "no-profile") || report.NativeFeesAuthenticated {
			t.Fatal("absent observation profile invented a native fee", mode)
		}
	}
}

func TestHistoricalFeeContextPublicRevertedReceiptKeepsObservedCandidate(t *testing.T) {
	request, _, _ := historicalFeeContextTestFixture(t, "reverted", "pair")
	code, raw, diagnostic := historicalFeeContextTestRun(t, t.Context(), request)
	var report historicalFeeContextReport
	if code != 0 || decodePlanJson(raw, &report) != nil {
		t.Fatal("reverted receipt join failed", code, diagnostic)
	}
	for _, transaction := range report.Transactions {
		if transaction.Candidate != nil {
			if transaction.Receipt == nil || transaction.Receipt.Status != 0 || transaction.Candidate.DebitRao == nil || transaction.ActualDebitRao != nil {
				t.Fatal("reverted receipt silently erased or authenticated its candidate fee")
			}
			return
		}
	}
	t.Fatal("reverted receipt candidate disappeared")
}

func TestHistoricalFeeContextPublicRefusesChangedNativeContextAndPayer(t *testing.T) {
	for _, mode := range []string{"wrong-state", "wrong-parent", "wrong-payer"} {
		request, _, _ := historicalFeeContextTestFixture(t, "success", mode)
		code, raw, diagnostic := historicalFeeContextTestRun(t, t.Context(), request)
		if code == 0 || len(raw) != 0 || !strings.Contains(diagnostic, "contradicts") {
			t.Fatal("actual proved native context or signed payer was bypassed", mode, code, diagnostic)
		}
	}
}

func TestHistoricalFeeContextPublicKeepsForeignBlockCandidatesUnallocated(t *testing.T) {
	request, _, _ := historicalFeeContextTestFixture(t, "success", "unselected")
	code, raw, diagnostic := historicalFeeContextTestRun(t, t.Context(), request)
	var report historicalFeeContextReport
	if code != 0 || decodePlanJson(raw, &report) != nil || report.JoinedCandidates != 0 || report.UnselectedCandidates != 1 {
		t.Fatal("full block candidate was allocated to an unrelated archived signature", code, diagnostic)
	}
	for _, transaction := range report.Transactions {
		if transaction.Candidate != nil || transaction.ActualDebitRao != nil {
			t.Fatal("foreign fee became a selected account debit")
		}
	}
}

func TestHistoricalFeeContextActualCryptographicRefusalPrecedesProcess(t *testing.T) {
	for _, mode := range []string{"receipt", "certificate", "network"} {
		request, fixture, _ := historicalFeeContextTestFixture(t, "success", "pair")
		want := "independent network"
		switch mode {
		case "receipt":
			fixture.Collection.Commitments.Receipts[0].ReceiptNodes[0] = "0x00"
			// Re-seal the outer frames so the real receipt trie, rather than
			// the collection checksum, is the causal refusal boundary.
			fixture.Collection.ContentHash = ""
			fixture.Collection.ContentHash = rootObjectHash(fixture.Collection)
			fixture.Proof.CollectionHash = fixture.Collection.ContentHash
			want = "receipt commitment trie"
		case "certificate":
			raw, err := historicalReplayHex(fixture.Proof.Segments[0].JustificationScale, strecovery.MaximumReceiptFinalityBytes)
			if err != nil || len(raw) <= 81 {
				t.Fatal("fixture does not contain its original complete certificate", err)
			}
			raw[81] ^= 1
			fixture.Proof.Segments[0].JustificationScale = "0x" + hex.EncodeToString(raw)
			want = "signature"
		case "network":
			request.EvmChainId++
		}
		for _, file := range []struct {
			reference *planFileReference
			value     any
		}{{reference: &request.Collection, value: fixture.Collection}, {reference: &request.FinalityProof, value: fixture.Proof}} {
			raw, err := json.Marshal(file.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file.reference.Path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			file.reference.Sha256 = monitorReadDigest(raw)
		}
		started := false
		report, err := runHistoricalFeeContext(t.Context(), request, 300*time.Second, historicalReplayHooks{afterStart: func(context.Context, int) { started = true }})
		if report != nil || err == nil || !strings.Contains(err.Error(), want) || started {
			t.Fatal("invalid actual receipt/certificate/network reached process execution", mode, report, err, started)
		}
	}
}

func TestHistoricalFeeContextReadErrorsRemainObservationFailures(t *testing.T) {
	request, _, _ := historicalFeeContextTestFixture(t, "success", "pair")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := runHistoricalFeeContext(ctx, request, 300*time.Second, historicalReplayHooks{})
	if report != nil || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "differs") || strings.Contains(err.Error(), "contradicts") {
		t.Fatal("canceled input observation became a financial identity contradiction", report, err)
	}
	if err := os.Remove(request.Collection.Path); err != nil {
		t.Fatal(err)
	}
	report, err = runHistoricalFeeContext(t.Context(), request, 300*time.Second, historicalReplayHooks{})
	if report != nil || !errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "differs") {
		t.Fatal("unread evidence acquired a false cryptographic mismatch", report, err)
	}
}

func TestHistoricalFeeContextPublicStrictRequestAndInputPins(t *testing.T) {
	request, _, _ := historicalFeeContextTestFixture(t, "success", "pair")
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	unknown := append(bytes.Clone(encoded[:len(encoded)-1]), []byte(",\"fee_authenticated\":true}")...)
	path := filepath.Join(filepath.Dir(request.Archive.Path), "unknown-authority-request.json")
	if err := os.WriteFile(path, unknown, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runMain(t.Context(), []string{"verify-historical-fee-context", "--request", path, "--request-sha256", monitorReadDigest(unknown)}, &stdout, &stderr); code == 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unknown field") {
		t.Fatal("public fee request accepted an injected authority assertion", code, stderr.String())
	}
	original := request
	request.Archive.Sha256 = monitorReadDigest([]byte("synthetic different archive bytes"))
	if request.Archive.Sha256 == original.Archive.Sha256 {
		t.Fatal("negative fixture did not change the exact archive pin")
	}
	if err := request.validate(); err != nil {
		t.Fatal("negative fixture did not reach actual input reading", err)
	}
	code, raw, diagnostic := historicalFeeContextTestRun(t, t.Context(), request)
	if code == 0 || len(raw) != 0 || !strings.Contains(diagnostic, "input differs") {
		t.Fatal("public fee context bypassed exact source bytes", code, diagnostic)
	}
	request = original
	if err := request.validate(); err != nil {
		t.Fatal("alias control baseline is not a valid independent request", err)
	}
	request.Engine = request.Job
	if err := request.validate(); err == nil {
		t.Fatal("engine and job could alias one mutable input")
	}
}
