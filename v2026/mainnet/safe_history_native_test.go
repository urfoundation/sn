// Deterministic native archive regressions exercise proof/canonical boundaries
// and the distinction between successful filtered traces and complete history.
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
	"golang.org/x/crypto/blake2b"
)

// Different child runtimes ensure the retained code is proven at the execution
// parent, while observed native writes remain distinct from committed effects.
func TestSafeHistoryNativeCaptureBindsParentRuntimeAndTrace(t *testing.T) {
	client, fixture := newSafeHistoryNativeFixture(t)
	capture, err := client.captureSafeHistoryNative(t.Context(), fixture.archive.expected, fixture.archive.scope)
	if err != nil || capture.Schema != safeHistoryNativeCaptureSchema || capture.NativeTrace == nil {
		t.Fatal("native archive capture failed", err)
	}
	native := capture.NativeTrace
	if !native.ParentRuntimeBytesVerified || !native.KeyedTraceScopeMatched || len(native.Blocks) != 2 || len(capture.Blocks) != 2 {
		t.Fatal("native archive lost parent proof or exact interval")
	}
	for i, block := range native.Blocks {
		original := fixture.archive.witnesses[i]
		parent := original.NativeHeader.ParentHash
		code := fixture.codes[parent]
		if !reflect.DeepEqual(block.ParentRuntime.Witness, fixture.parents[parent]) || block.ParentRuntime.Bytes != len(code) ||
			block.ParentRuntime.CodeHash != common.Hash(blake2b.Sum256(code)).Hex() || block.ParentRuntime.CodeHash == common.Hash(blake2b.Sum256(fixture.codes[original.Native.Hash])).Hex() ||
			block.Trace.BlockHash != original.Native.Hash[2:] || block.Trace.ParentHash != parent[2:] || len(block.Trace.Events) != 3 || block.Trace.Events[1].Data.StringValues["method"] != "Put" {
			t.Fatal("native trace or code proof selected another block or runtime", i)
		}
	}
	if fixture.counts["state_traceBlock"] != 2 || fixture.counts["state_getReadProof"] != 2 {
		t.Fatal("native capture did not independently read every selected parent and trace")
	}
	if _, err := sealSafeHistoryCapture(capture); err != nil {
		t.Fatal("native capture could not be sealed", err)
	}
}

// Both visible writes and an explicit empty successful trace lack keyless
// operations, rollback boundaries and full EVM history. None may grant authority.
func TestSafeHistoryNativeCaptureKeepsFilteredCoverageUnproven(t *testing.T) {
	for _, empty := range []bool{false, true} {
		client, fixture := newSafeHistoryNativeFixture(t)
		fixture.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
			if empty && method == "state_traceBlock" {
				trace := result.(map[string]any)["blockTrace"].(map[string]any)
				trace["events"], trace["spans"] = []any{}, []any{}
			}
			return result, nil
		}
		capture, err := client.captureSafeHistoryNative(t.Context(), fixture.archive.expected, fixture.archive.scope)
		if err != nil {
			t.Fatal("explicit filtered trace was refused", empty, err)
		}
		native := capture.NativeTrace
		if native.CompleteParentStateVerified || native.ClearPrefixCoverageVerified || native.RollbackCoverageVerified || native.StorageRootCoverageVerified ||
			native.InnerEvmCoverageVerified || native.TraceCompletenessVerified || capture.RuntimeSourceProven || capture.NativeExecutionVerified ||
			capture.InternalExecutionVerified || capture.DeploymentHistoryVerified || capture.CompletePendingVerified || capture.SendAuthorized {
			t.Fatal("successful filtered trace falsely proved complete execution or Safe authority", empty)
		}
		for _, widen := range []func(*safeHistoryNativeCapture){
			func(v *safeHistoryNativeCapture) { v.CompleteParentStateVerified = true },
			func(v *safeHistoryNativeCapture) { v.ClearPrefixCoverageVerified = true },
			func(v *safeHistoryNativeCapture) { v.RollbackCoverageVerified = true },
			func(v *safeHistoryNativeCapture) { v.StorageRootCoverageVerified = true },
			func(v *safeHistoryNativeCapture) { v.InnerEvmCoverageVerified = true },
			func(v *safeHistoryNativeCapture) { v.TraceCompletenessVerified = true },
		} {
			changed := *native
			widen(&changed)
			capture.NativeTrace = &changed
			if _, err := sealSafeHistoryCapture(capture); err == nil {
				t.Fatal("native archive sealed an unproved coverage claim")
			}
		}
	}
}

// Node-supplied proof roots and code claims cannot replace the parent's header
// commitment; every incomplete or substituted proof returns no partial capture.
func TestSafeHistoryNativeCaptureRefusesParentProofSubstitution(t *testing.T) {
	for _, fault := range []string{"at", "header", "nodes", "external-value"} {
		client, fixture := newSafeHistoryNativeFixture(t)
		fixture.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
			if fault == "header" && method == "chain_getHeader" {
				header := result.(rootReceiptHeader)
				header.StateRoot = testGenesisHash
				return header, nil
			}
			if method == "state_getReadProof" {
				proof := result.(map[string]any)
				switch fault {
				case "at":
					proof["at"] = fixture.archive.witnesses[0].Native.Hash
				case "nodes":
					proof["proof"] = []string{}
				case "external-value":
					code := "0x" + hex.EncodeToString(fixture.codes[proof["at"].(string)])
					nodes := []string{}
					for _, node := range proof["proof"].([]string) {
						if node != code {
							nodes = append(nodes, node)
						}
					}
					if len(nodes) == len(proof["proof"].([]string)) {
						t.Fatal("fixture did not contain the independent external code value")
					}
					proof["proof"] = nodes
				}
			}
			return result, nil
		}
		capture, err := client.captureSafeHistoryNative(t.Context(), fixture.archive.expected, fixture.archive.scope)
		if err == nil || capture.Schema != "" || capture.NativeTrace != nil || fixture.counts["state_traceBlock"] != 0 {
			t.Fatal("unproven parent authorized a trace or partial success", fault, err)
		}
	}
}

// Exact hash/filter echoes and explicit SDK vectors prevent defaults or a
// different account's trace from masquerading as this request's observation.
func TestSafeHistoryNativeTraceRefusesChangedScopeAndShape(t *testing.T) {
	_, fixture := newSafeHistoryNativeFixture(t)
	block, err := authenticateSafeHistoryBlock(t.Context(), fixture.archive.scope.Safe, fixture.archive.witnesses[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"blockHash", "parentHash", "tracingTargets", "storageKeys", "methods", "missing-methods", "null-events", "missing-events", "null-spans", "foreign-key", "missing-event-method", "duplicate-span", "unknown-field", "other-variant"} {
		response := fixture.trace(block.Witness)
		trace := response["blockTrace"].(map[string]any)
		switch fault {
		case "blockHash", "parentHash", "tracingTargets", "storageKeys", "methods":
			trace[fault] = "different"
		case "missing-methods":
			delete(trace, "methods")
		case "null-events":
			trace["events"] = nil
		case "missing-events":
			delete(trace, "events")
		case "null-spans":
			trace["spans"] = nil
		case "foreign-key", "missing-event-method":
			values := trace["events"].([]any)[0].(map[string]any)["data"].(map[string]any)["stringValues"].(map[string]string)
			if fault == "foreign-key" {
				values["key"] = "001122"
			} else {
				delete(values, "method")
			}
		case "duplicate-span":
			spans := trace["spans"].([]any)
			trace["spans"] = append(spans, spans[0])
		case "unknown-field":
			trace["complete"] = true
		case "other-variant":
			response["traceError"] = map[string]string{"error": "synthetic ambiguous response"}
		}
		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		result, err := decodeSafeHistoryNativeTrace(t.Context(), raw, fixture.archive.scope.Safe, block)
		if err == nil || result.BlockHash != "" {
			t.Fatal("native trace accepted changed request scope or omitted SDK fields", fault, err)
		}
	}
}

// Unsupported/disabled methods and SDK payload errors never become an empty
// successful trace. Read-only capture has no invocation route to a send method.
func TestSafeHistoryNativeCaptureRefusesUnavailableCapabilities(t *testing.T) {
	for _, unavailable := range []any{nil, mappingFixtureRpcError{code: -32601}, mappingFixtureRpcError{code: -32602}, map[string]any{"traceError": map[string]string{"error": "synthetic tracing unavailable"}}} {
		client, fixture := newSafeHistoryNativeFixture(t)
		fixture.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
			if method == "state_traceBlock" {
				return unavailable, nil
			}
			return result, nil
		}
		capture, err := client.captureSafeHistoryNative(t.Context(), fixture.archive.expected, fixture.archive.scope)
		if !errors.Is(err, errSafeHistoryArchiveUnavailable) || capture.Schema != "" {
			t.Fatal("unavailable native traces became successful empty evidence", err)
		}
	}
	client, fixture := newSafeHistoryNativeFixture(t)
	var result any
	if client.safeHistoryNativeRead(t.Context(), "eth_sendRawTransaction", []any{}, 1024, &result) == nil ||
		client.call(t.Context(), "state_traceBlock", []any{}, &result) == nil || len(fixture.counts) != 0 {
		t.Fatal("native trace capability escaped its narrow read-only profile")
	}
	if capture, err := client.captureSafeHistoryNative(nil, fixture.archive.expected, fixture.archive.scope); err == nil || capture.Schema != "" || len(fixture.counts) != 0 {
		t.Fatal("native trace admitted an absent lifecycle")
	}
}

// Each trace/proof owns a fresh finite read window, and cancellation at either
// exact boundary joins the operation without later requests or partial output.
func TestSafeHistoryNativeCaptureRenewsBudgetsAndJoinsCancellation(t *testing.T) {
	client, fixture := newSafeHistoryNativeFixture(t)
	client.retryWindow = time.Second
	var previous time.Time
	fixture.fault = func(request *http.Request, method string, _ int, result any) (any, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || !deadline.After(previous) {
			return nil, fmt.Errorf("native read did not renew deadline: %s", method)
		}
		previous = deadline
		return result, nil
	}
	if _, err := client.captureSafeHistoryNative(t.Context(), fixture.archive.expected, fixture.archive.scope); err != nil {
		t.Fatal("native trace reads shared an aggregate deadline", err)
	}
	for _, stopped := range []string{"state_getReadProof", "state_traceBlock"} {
		ctx, cancel := context.WithCancel(t.Context())
		client, fixture := newSafeHistoryNativeFixture(t)
		fixture.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
			if method == stopped {
				cancel()
			}
			return result, nil
		}
		capture, err := client.captureSafeHistoryNative(ctx, fixture.archive.expected, fixture.archive.scope)
		cancel()
		if !errors.Is(err, context.Canceled) || capture.Schema != "" || fixture.counts["state_getReadProof"] != 1 || fixture.counts["state_traceBlock"] > 1 {
			t.Fatal("native cancellation emitted partial success or continued reading", stopped, err)
		}
	}
}

// A later head may advance while each original proof stays pinned; replacing
// an interval block during native tracing must fail the final canonical pass.
func TestSafeHistoryNativeCaptureAdmitsAdvancementAndRefusesReorg(t *testing.T) {
	for _, reorg := range []bool{false, true} {
		client, fixture := newSafeHistoryNativeFixture(t)
		fixture.archive.advance = true
		fixture.archive.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
			if reorg && method == "chain_getBlockHash" && fixture.counts["state_traceBlock"] > 0 && result == fixture.archive.scope.From.Hash {
				return testGenesisHash, nil
			}
			return result, nil
		}
		capture, err := client.captureSafeHistoryNative(t.Context(), fixture.archive.expected, fixture.archive.scope)
		if reorg {
			if !errors.Is(err, errRpcIntegrity) || capture.Schema != "" {
				t.Fatal("native trace accepted a changed original block", err)
			}
		} else if err != nil || capture.LaterFinalized != fixture.archive.witnesses[2].Native || capture.NativeTrace.Blocks[0].Native != fixture.archive.scope.From {
			t.Fatal("native trace rejected a healthy advancing head or relabeled its proof", err)
		}
	}
}

// Count bounds are applied independently of reply size; malformed raw payloads
// cannot turn oversized vectors into a partially trusted history report.
func TestSafeHistoryNativeTraceBoundsVectorsAndReply(t *testing.T) {
	_, fixture := newSafeHistoryNativeFixture(t)
	block := safeHistoryBlock{Witness: fixture.archive.witnesses[0]}
	for _, fault := range []string{"spans", "events", "reply", "nil-context", "duplicate-json"} {
		response := fixture.trace(block.Witness)
		trace := response["blockTrace"].(map[string]any)
		if fault == "spans" {
			spans := make([]safeHistoryNativeSpan, maximumSafeHistoryNativeSpans+1)
			for i := range spans {
				spans[i] = safeHistoryNativeSpan{Id: uint64(i + 1), Name: "synthetic", Target: "state"}
			}
			trace["spans"] = spans
		} else if fault == "events" {
			events := make([]any, maximumSafeHistoryNativeEvents+1)
			for i := range events {
				events[i] = map[string]any{"target": "state", "parentId": nil, "data": map[string]any{"stringValues": map[string]string{"method": "Get", "key": "3a636f6465"}}}
			}
			trace["events"] = events
		}
		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		ctx := t.Context()
		switch fault {
		case "reply":
			raw = append(raw, bytes.Repeat([]byte{' '}, maximumSafeHistoryNativeReplyBytes+1-len(raw))...)
		case "nil-context":
			ctx = nil
		case "duplicate-json":
			raw = bytes.Replace(raw, []byte(`"methods":""`), []byte(`"methods":"","methods":""`), 1)
		}
		if result, err := decodeSafeHistoryNativeTrace(ctx, raw, fixture.archive.scope.Safe, block); err == nil || result.BlockHash != "" {
			t.Fatal("native trace bypassed bound or exact JSON decoding", fault, err)
		}
	}
}

// Each valid individual proof and trace fits its own bound. Their three-block
// combination exceeds the shared budget and must not return the earlier prefix.
func TestSafeHistoryNativeCaptureBoundsSharedWitnesses(t *testing.T) {
	client, fixture := newSafeHistoryNativeSizedFixture(t, 4*1024*1024)
	fixture.archive.scope.Through = fixture.archive.witnesses[2].Native
	fixture.archive.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
		if method == "chain_getFinalizedHead" {
			return fixture.archive.witnesses[2].Native.Hash, nil
		}
		return result, nil
	}
	fixture.fault = func(_ *http.Request, method string, _ int, result any) (any, error) {
		if method == "state_traceBlock" {
			trace := result.(map[string]any)["blockTrace"].(map[string]any)
			values := trace["events"].([]any)[0].(map[string]any)["data"].(map[string]any)["stringValues"].(map[string]string)
			values["value"] = strings.Repeat("a", 14*1024*1024)
		}
		return result, nil
	}
	capture, err := client.captureSafeHistoryNative(t.Context(), fixture.archive.expected, fixture.archive.scope)
	if err == nil || !strings.Contains(err.Error(), "shared encoded witness byte bound") || capture.Schema != "" || capture.NativeTrace != nil ||
		fixture.counts["state_getReadProof"] != 3 || fixture.counts["state_traceBlock"] != 3 {
		t.Fatal("native trace aggregate budget returned partial history or hit an unrelated bound", err)
	}
}

// The public flag retains the enriched schema with the same private create-only
// publication; a stdout failure cannot erase the successfully retained evidence.
func TestSafeHistoryNativeCommandRetainsEvidenceWithoutSubmission(t *testing.T) {
	_, fixture := newSafeHistoryNativeFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, err := fixture.roundTrip(r)
		if err != nil {
			t.Errorf("native archive command fixture: %v", err)
			w.WriteHeader(400)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		if _, err := io.Copy(w, response.Body); err != nil {
			t.Errorf("native archive response: %v", err)
		}
	}))
	defer server.Close()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "native-capture.json")
	args := []string{"safe-history-capture", "--rpc", server.URL, "--output", path, "--expected-chain", fixture.archive.expected.NativeChain,
		"--expected-genesis", fixture.archive.expected.GenesisHash, "--expected-evm-chain-id", "964", "--safe", fixture.archive.scope.Safe,
		"--from-number", "100", "--from-hash", fixture.archive.scope.From.Hash, "--through-number", "101", "--through-hash", fixture.archive.scope.Through.Hash, "--native-storage-trace"}
	var stdout, stderr bytes.Buffer
	if exit := runMain(t.Context(), args, &stdout, &stderr); exit != 0 {
		t.Fatal("public native archive command failed", exit, stderr.String())
	}
	raw, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	var envelope safeHistoryCaptureEnvelope
	if err != nil || statErr != nil || info.Mode().Perm() != 0600 || !bytes.Equal(raw, stdout.Bytes()) || decodePlanJson(raw, &envelope) != nil ||
		envelope.Capture.Schema != safeHistoryNativeCaptureSchema || envelope.Capture.NativeTrace == nil || envelope.Capture.SendAuthorized || envelope.Capture.DeploymentHistoryVerified {
		t.Fatal("native archive did not retain its exact private evidence and coverage limits", err, statErr)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runMain(t.Context(), args, &stdout, &stderr); exit != 1 || stdout.Len() != 0 {
		t.Fatal("native archive replaced existing evidence", exit, stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("native archive changed earlier evidence", err)
	}
	args[4] = filepath.Join(directory, "output-failed.json")
	if exit := runMain(t.Context(), args, ioFailureWriter{}, &stderr); exit != 1 {
		t.Fatal("native archive acknowledged failed stdout", exit)
	}
	if _, err := os.Stat(args[4]); err != nil {
		t.Fatal("stdout failure erased retained native evidence", err)
	}
	args = append(args, "--submit")
	if exit := runMain(t.Context(), args, &stdout, &stderr); exit != 2 || !strings.Contains(stderr.String(), "flag") {
		t.Fatal("native archive accepted a submit flag", exit, stderr.String())
	}
}
