// Tests drive the real read-only command/collector through complete synthetic
// archives and deterministic read-boundary faults; no live RPC is contacted.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// Both Frontier hash variants preserve foreign transactions, direct reverted
// calls, unknown Safe events, and their independently committed positions.
func TestSafeHistoryCaptureRetainsCompleteTrafficWithoutAuthority(t *testing.T) {
	client, fixture := newSafeHistoryFixture(t)
	capture, err := client.captureSafeHistory(t.Context(), fixture.expected, fixture.scope)
	if err != nil || len(capture.Blocks) != 2 || !capture.ByteCommitmentsVerified {
		t.Fatalf("complete archive census refused unrelated traffic: %v", err)
	}
	for index, block := range capture.Blocks {
		if block.NativeCount != 2 || block.TransactionCount != 3 || len(block.DirectCalls) != 1 || len(block.Logs) != 1 ||
			block.DirectCalls[0].TransactionIndex != 1 || block.DirectCalls[0].ReceiptStatus != 0 || block.Logs[0].TransactionIndex != 2 ||
			block.Evm.Number != uint64(37+index) || !reflect.DeepEqual(block.Witness, fixture.witnesses[index]) {
			t.Fatalf("archive census dropped committed traffic or changed its position: %d", index)
		}
		replayed, err := authenticateSafeHistoryBlock(t.Context(), fixture.scope.Safe, block.Witness)
		if err != nil || !reflect.DeepEqual(replayed, block) {
			t.Fatalf("retained archive witness did not replay exactly: %v", err)
		}
	}
	if capture.RuntimeSourceProven || capture.NativeExecutionVerified || capture.InternalExecutionVerified || capture.DeploymentHistoryVerified ||
		capture.CompletePendingVerified || capture.SendAuthorized || capture.FinalityAuthority != "owned-rpc-assertion" || len(capture.Unresolved) != 4 {
		t.Fatal("archive census acquired unresolved Safe history or send authority")
	}
	envelope, err := sealSafeHistoryCapture(capture)
	if err != nil || !planSha256(envelope.ContentHash) {
		t.Fatalf("archive census lost content seal: %v", err)
	}
}

// Complete native body commitments reject otherwise plausible omissions and
// reordering. The retained witness verifier applies exactly the same root check.
func TestSafeHistoryCensusRefusesNativeBodySubstitution(t *testing.T) {
	for _, fault := range []string{"omission", "reorder", "header", "length"} {
		_, fixture := newSafeHistoryFixture(t)
		witness := fixture.witnesses[0]
		switch fault {
		case "omission":
			witness.Extrinsics = witness.Extrinsics[:1]
		case "reorder":
			witness.Extrinsics[0], witness.Extrinsics[1] = witness.Extrinsics[1], witness.Extrinsics[0]
		case "header":
			witness.NativeHeader.StateRoot = testGenesisHash
		case "length":
			witness.Extrinsics[0] = "0x0804"
		}
		block, err := authenticateSafeHistoryBlock(t.Context(), fixture.scope.Safe, witness)
		if err == nil || block.Witness.Native.Hash != "" {
			t.Fatalf("archive census accepted substituted native body %s", fault)
		}
	}
}

// Hashing an independently resealed native header cannot authorize an EVM
// block with different receipt roots, raw bytes, or Frontier transaction vector.
func TestSafeHistoryCensusRefusesEvmCommitmentSubstitution(t *testing.T) {
	for _, fault := range []string{"receipt-omission", "receipt-root", "raw-block", "raw-header", "digest-hash", "digest-vector"} {
		_, fixture := newSafeHistoryFixture(t)
		witness := fixture.witnesses[0]
		switch fault {
		case "receipt-omission":
			witness.EvmReceipts = witness.EvmReceipts[:2]
		case "receipt-root":
			raw, _ := hex.DecodeString(witness.EvmReceipts[0][2:])
			var receipt types.Receipt
			if err := receipt.UnmarshalBinary(raw); err != nil {
				t.Fatal(err)
			}
			receipt.Status = 0
			raw, err := receipt.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			witness.EvmReceipts[0] = "0x" + hex.EncodeToString(raw)
		case "raw-block":
			witness.EvmBlock = fixture.witnesses[1].EvmBlock
		case "raw-header":
			witness.EvmHeader = fixture.witnesses[1].EvmHeader
		case "digest-hash":
			witness.NativeHeader.Digest.Logs = []string{mappingTestDigest(t, 3, testGenesisHash, nil)}
			safeHistoryTestSealNative(t, &witness)
		case "digest-vector":
			postLog, err := finalizedFrontierPostLog(witness.NativeHeader)
			if err != nil {
				t.Fatal(err)
			}
			postLog.TransactionHashes[0] = testGenesisHash
			witness.NativeHeader.Digest.Logs = []string{mappingTestDigest(t, 1, postLog.BlockHash, postLog.TransactionHashes)}
			safeHistoryTestSealNative(t, &witness)
		}
		block, err := authenticateSafeHistoryBlock(t.Context(), fixture.scope.Safe, witness)
		if err == nil || block.Witness.Native.Hash != "" {
			t.Fatalf("archive census accepted substituted EVM commitment %s", fault)
		}
	}
}

// A normal advancing head changes only the finality observation, never the
// retained interval or proof block identity, and requires no capture restart.
func TestSafeHistoryCaptureAdmitsAdvancingFinalizedHead(t *testing.T) {
	client, fixture := newSafeHistoryFixture(t)
	fixture.advance = true
	capture, err := client.captureSafeHistory(t.Context(), fixture.expected, fixture.scope)
	if err != nil || capture.InitialFinalized != fixture.witnesses[1].Native || capture.LaterFinalized != fixture.witnesses[2].Native ||
		capture.Scope != fixture.scope || len(capture.Blocks) != 2 || capture.Blocks[1].Witness.Native != fixture.scope.Through || fixture.counts["debug_getRawBlock"] != 2 {
		t.Fatalf("archive census rejected normal advancement or relabeled proof head: %v", err)
	}
}

// Owned-route canonicality and identity are rechecked after slow body reads.
// An outage or changed assertion discards the report instead of certifying gaps.
func TestSafeHistoryCaptureRefusesChangedCanonicalOrNetworkState(t *testing.T) {
	for _, fault := range []string{"reorg", "network", "start"} {
		client, fixture := newSafeHistoryFixture(t)
		fixture.fault = func(_ *http.Request, method string, count int, result any) (any, error) {
			if fault == "reorg" && method == "chain_getBlockHash" && count >= 5 {
				return testGenesisHash, nil
			}
			if fault == "network" && method == "system_chain" && count == 2 {
				return "other-chain", nil
			}
			return result, nil
		}
		if fault == "start" {
			fixture.scope.From.Hash = testGenesisHash
		}
		capture, err := client.captureSafeHistory(t.Context(), fixture.expected, fixture.scope)
		if err == nil || capture.Schema != "" {
			t.Fatalf("archive census accepted changed finality or scope %s", fault)
		}
	}
}

// A valid committed EVM block can still be the wrong parent for its native
// predecessor. Every raw commitment is resealed so this reaches the chain check.
func TestSafeHistoryCaptureRefusesEvmAncestryGap(t *testing.T) {
	client, fixture := newSafeHistoryFixture(t)
	witness := &fixture.witnesses[1]
	raw, _ := hex.DecodeString(witness.EvmBlock[2:])
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var header types.Header
	if err := rlp.DecodeBytes(fields[0], &header); err != nil {
		t.Fatal(err)
	}
	header.ParentHash = common.HexToHash(testGenesisHash)
	fields[0], _ = rlp.EncodeToBytes(&header)
	raw, _ = rlp.EncodeToBytes(fields)
	witness.EvmHeader, witness.EvmBlock = "0x"+hex.EncodeToString(fields[0]), "0x"+hex.EncodeToString(raw)
	witness.NativeHeader.Digest.Logs = []string{mappingTestDigest(t, 3, header.Hash().Hex(), nil)}
	safeHistoryTestSealNative(t, witness)
	fixture.scope.Through = witness.Native
	capture, err := client.captureSafeHistory(t.Context(), fixture.expected, fixture.scope)
	if err == nil || capture.Schema != "" || !strings.Contains(err.Error(), "EVM ancestry") {
		t.Fatalf("archive census accepted independently committed EVM ancestry gap: %v", err)
	}
}

// Each actual transport context has a renewed deadline. Cancellation during a
// raw receipt read propagates through the joined owner without partial success.
func TestSafeHistoryCaptureRenewsReadBudgetsAndJoinsCancellation(t *testing.T) {
	client, fixture := newSafeHistoryFixture(t)
	client.retryWindow = time.Second
	var previous time.Time
	fixture.fault = func(request *http.Request, method string, _ int, result any) (any, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || !deadline.After(previous) {
			return nil, fmt.Errorf("archive read did not renew its per-read deadline: %s", method)
		}
		previous = deadline
		return result, nil
	}
	if _, err := client.captureSafeHistory(t.Context(), fixture.expected, fixture.scope); err != nil {
		t.Fatalf("archive reads shared an aggregate retry deadline: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, fixture = newSafeHistoryFixture(t)
	fixture.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
		if method == "debug_getRawReceipts" {
			cancel()
		}
		return result, nil
	}
	capture, err := client.captureSafeHistory(ctx, fixture.expected, fixture.scope)
	if !errors.Is(err, context.Canceled) || capture.Schema != "" {
		t.Fatalf("cancelled archive read emitted partial success: %v", err)
	}
}

// Unsupported raw reads remain unavailable, and ordinary read or archive
// profiles cannot invoke submission. Invalid scopes are refused before any I/O.
func TestSafeHistoryCaptureBoundsCapabilitiesAndNoWriteRoute(t *testing.T) {
	client, fixture := newSafeHistoryFixture(t)
	for _, fault := range []string{"nil-context", "range", "safe", "identity"} {
		ctx, scope, expected := context.Background(), fixture.scope, fixture.expected
		switch fault {
		case "nil-context":
			ctx = nil
		case "range":
			scope.Through.Number = scope.From.Number + maximumSafeHistoryBlocks
		case "safe":
			scope.Safe = "0x0"
		case "identity":
			expected.EvmChainId = 1
		}
		if result, err := client.captureSafeHistory(ctx, expected, scope); err == nil || result.Schema != "" || len(fixture.counts) != 0 {
			t.Fatalf("archive admission escaped pre-read bound %s: %v", fault, err)
		}
	}
	var result string
	if client.callSafeHistoryRead(t.Context(), "eth_sendRawTransaction", testGenesisHash, &result) == nil || client.call(t.Context(), "debug_getRawBlock", nil, &result) == nil {
		t.Fatal("archive read capability escaped its narrow method boundary")
	}
	for _, required := range []string{"debug_getRawHeader", "debug_getRawBlock", "debug_getRawReceipts"} {
		for _, unavailable := range []any{nil, mappingFixtureRpcError{code: -32601}, mappingFixtureRpcError{code: -32602}} {
			client, fixture := newSafeHistoryFixture(t)
			fixture.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
				if method == required {
					return unavailable, nil
				}
				return result, nil
			}
			capture, err := client.captureSafeHistory(t.Context(), fixture.expected, fixture.scope)
			if !errors.Is(err, errSafeHistoryArchiveUnavailable) || capture.Schema != "" || fixture.counts["eth_getBlockByHash"] != 0 {
				t.Fatalf("unavailable %s became successful history: %v", required, err)
			}
		}
	}
}

// The public command retains exact fsynced bytes before acknowledging success;
// repeat publication cannot replace an earlier capture or symlink destination.
func TestSafeHistoryCommandRetainsPrivateCreateOnlyWitness(t *testing.T) {
	_, fixture := newSafeHistoryFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, err := fixture.roundTrip(r)
		if err != nil {
			t.Errorf("archive command fixture: %v", err)
			w.WriteHeader(400)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		if _, err := io.Copy(w, response.Body); err != nil {
			t.Errorf("archive response: %v", err)
		}
	}))
	defer server.Close()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "capture.json")
	args := []string{"safe-history-capture", "--rpc", server.URL, "--output", path, "--expected-chain", fixture.expected.NativeChain,
		"--expected-genesis", fixture.expected.GenesisHash, "--expected-evm-chain-id", "964", "--safe", fixture.scope.Safe,
		"--from-number", "100", "--from-hash", fixture.scope.From.Hash, "--through-number", "101", "--through-hash", fixture.scope.Through.Hash}
	var stdout, stderr bytes.Buffer
	if exit := runMain(t.Context(), args, &stdout, &stderr); exit != 0 {
		t.Fatalf("archive command refused complete fixture: %d %s", exit, stderr.String())
	}
	raw, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	var envelope safeHistoryCaptureEnvelope
	if err != nil || statErr != nil || info.Mode().Perm() != 0600 || !bytes.Equal(raw, stdout.Bytes()) || json.Unmarshal(raw, &envelope) != nil {
		t.Fatalf("archive command did not retain exact private witness bytes: %v %v", err, statErr)
	}
	checked, err := sealSafeHistoryCapture(envelope.Capture)
	if err != nil || checked.ContentHash != envelope.ContentHash || envelope.Capture.SendAuthorized || envelope.Capture.InternalExecutionVerified {
		t.Fatal("archive file lost digest or acquired history authority")
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runMain(t.Context(), args, &stdout, &stderr); exit != 1 || stdout.Len() != 0 {
		t.Fatalf("archive command replaced existing durable evidence: %d %s", exit, stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("archive conflict changed original durable evidence")
	}
	link := filepath.Join(directory, "linked.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := writeSafeHistoryCapture(t.Context(), link, envelope); err == nil {
		t.Fatal("archive publisher followed final-name symlink")
	}
}
