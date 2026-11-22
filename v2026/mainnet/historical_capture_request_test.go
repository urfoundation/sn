package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"
)

type historicalCaptureRpcFixture struct {
	server        *httptest.Server
	expected      identityExpectation
	parent        rootReceiptHeader
	child         rootReceiptHeader
	parentHash    string
	childHash     string
	code          []byte
	metadata      []byte
	body          []string
	artifactReads atomic.Int64
	bodyReads     atomic.Int64
	parentVersion atomic.Int64
	afterBody     atomic.Bool
	entered       chan struct{}
	joined        chan struct{}
}

func newHistoricalCaptureRpcFixture(t *testing.T, mode string) *historicalCaptureRpcFixture {
	t.Helper()
	f := &historicalCaptureRpcFixture{expected: identityExpectation{NativeChain: "Bittensor", GenesisHash: "0x" + strings.Repeat("21", 32), EvmChainId: 964}, code: []byte{0, 97, 115, 109, 1, 0, 0, 0}, metadata: []byte{'m', 'e', 't', 'a', 255, 0}, body: []string{"0x0c010203"}, entered: make(chan struct{}), joined: make(chan struct{})}
	f.parent, f.parentHash = rootReceiptHeaderFixture(t, "0x"+strings.Repeat("22", 32), 100, nil, false)
	f.child, f.childHash = rootReceiptHeaderFixture(t, f.parentHash, 101, [][]byte{{12, 1, 2, 3}}, true)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var call struct {
			JsonRpc string            `json:"jsonrpc"`
			Id      int               `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil || call.JsonRpc != "2.0" || call.Id != 1 {
			http.Error(w, "invalid fixture call", http.StatusBadRequest)
			return
		}
		stringArg := func(index int) string {
			var value string
			if len(call.Params) <= index || json.Unmarshal(call.Params[index], &value) != nil {
				t.Errorf("missing exact fixture hash argument: %s %v", call.Method, call.Params)
			}
			return value
		}
		var result any
		switch call.Method {
		case "system_chain":
			result = f.expected.NativeChain
		case "system_version":
			result = "synthetic capture node"
		case "eth_chainId":
			result = "0x3c4"
			if mode == "wrong-network" {
				result = "0x3b1"
			}
		case "chain_getFinalizedHead":
			result = f.childHash
		case "chain_getHeader":
			switch hash := stringArg(0); hash {
			case f.childHash:
				result = f.child
			case f.parentHash:
				result = f.parent
			default:
				t.Errorf("unexpected fixture header: %s", hash)
			}
		case "chain_getBlockHash":
			var number uint64
			if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &number) != nil {
				t.Errorf("invalid fixture block number: %v", call.Params)
			}
			switch number {
			case 0:
				result = f.expected.GenesisHash
			case 100:
				result = f.parentHash
			case 101:
				result = f.childHash
				if mode == "changed-canonical" && f.afterBody.Load() {
					result = f.expected.GenesisHash
				}
			default:
				t.Errorf("unexpected fixture number: %d", number)
			}
		case "state_getRuntimeVersion":
			version := 992
			if stringArg(0) == f.parentHash {
				version = 991
				if f.parentVersion.Add(1) > 1 && mode == "changed-parent-runtime" {
					version++
				}
			}
			result = map[string]any{"specName": "synthetic-capture-runtime", "specVersion": version, "transactionVersion": 1, "stateVersion": 1}
		case "state_getStorageHash", "state_getStorage":
			f.artifactReads.Add(1)
			if stringArg(0) != runtimeCodeStorageKey || stringArg(1) != f.parentHash {
				t.Errorf("capture read the upgrade child's code instead of parent execution code: %s %v", call.Method, call.Params)
			}
			if call.Method == "state_getStorageHash" {
				digest := blake2b.Sum256(f.code)
				result = "0x" + hex.EncodeToString(digest[:])
			} else {
				result = "0x" + hex.EncodeToString(f.code)
			}
		case "state_getMetadata":
			f.artifactReads.Add(1)
			if stringArg(0) != f.parentHash {
				t.Errorf("capture metadata came from the child runtime: %v", call.Params)
			}
			result = "0x" + hex.EncodeToString(f.metadata)
		case "chain_getBlock":
			if stringArg(0) != f.childHash {
				t.Errorf("capture body was retargeted: %v", call.Params)
			}
			reads := f.bodyReads.Add(1)
			if mode == "cancel-body" {
				close(f.entered)
				<-request.Context().Done()
				close(f.joined)
				return
			}
			if mode == "retry-body" && reads == 1 {
				http.Error(w, "temporary archive outage", http.StatusServiceUnavailable)
				return
			}
			body := f.body
			if mode == "changed-body" {
				body = []string{"0x0c010204"}
			}
			if mode == "missing-body" {
				body = nil
			}
			result = map[string]any{"block": map[string]any{"header": f.child, "extrinsics": body}}
			f.afterBody.Store(true)
		default:
			t.Errorf("unexpected or non-read capture RPC method %s", call.Method)
			http.Error(w, "unexpected method", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}); err != nil {
			t.Errorf("encode capture fixture: %v", err)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (self *historicalCaptureRpcFixture) client(t *testing.T) *rpcClient {
	t.Helper()
	client, err := newRpcClient(self.server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	return client
}

func TestHistoricalCaptureRequestPublicCommandUsesParentRuntimeAtUpgrade(t *testing.T) {
	f := newHistoricalCaptureRpcFixture(t, "success")
	var stdout, stderr bytes.Buffer
	exit := runMain(t.Context(), []string{"historical-capture-request", "--rpc", f.server.URL, "--block-hash", f.childHash, "--expected-chain", f.expected.NativeChain, "--expected-genesis", f.expected.GenesisHash, "--expected-evm-chain-id", "964"}, &stdout, &stderr)
	var envelope historicalCaptureObservationEnvelope
	if exit != 0 || decodePlanJson(stdout.Bytes(), &envelope) != nil {
		t.Fatal("public capture request failed", exit, stderr.String())
	}
	observation := envelope.Observation
	var input historicalCaptureInput
	if decodePlanJson([]byte(observation.RequestJSON), &input) != nil || input.validate() != nil {
		t.Fatal("public request is not the actual collector's bounded grammar", observation.RequestJSON)
	}
	if envelope.ContentHash != rootObjectHash(observation) || observation.RequestSha256 != monitorReadDigest([]byte(observation.RequestJSON)) || observation.Authority != "owned-rpc-assertion" || observation.RuntimeAdmitted || observation.FeeAdmitted || observation.Identity.FinalizedHash != f.childHash || observation.Identity.RuntimeSpec != 992 || observation.ParentRuntime.Identity.FinalizedHash != f.parentHash || observation.ParentRuntime.Version.SpecVersion != 991 || observation.ParentRuntime.Admission != "unapproved_observation" || f.artifactReads.Load() != 3 || f.bodyReads.Load() != 1 {
		t.Fatal("capture used current child code or promoted RPC observation authority", observation, f.artifactReads.Load(), f.bodyReads.Load())
	}
	if input.RuntimeCodeSha256 != historicalReplayDigest(sha256.Sum256(f.code)) || len(input.ExtrinsicsHex) != 1 || input.ExtrinsicsHex[0] != f.body[0] {
		t.Fatal("capture request changed original parent code or complete child body", input)
	}
}

func TestHistoricalCaptureRequestWrongNetworkPrecedesArtifactReads(t *testing.T) {
	f := newHistoricalCaptureRpcFixture(t, "wrong-network")
	result, err := f.client(t).readHistoricalCaptureRequest(t.Context(), f.expected, f.childHash, nil)
	if !errors.Is(err, errRpcIdentityMismatch) || result.RequestJSON != "" || f.artifactReads.Load() != 0 || f.bodyReads.Load() != 0 {
		t.Fatal("wrong network reached artifact capture or lost hard identity", result, err, f.artifactReads.Load())
	}
}

func TestHistoricalCaptureRequestChangedOrMissingCompleteBodyRefuses(t *testing.T) {
	for _, mode := range []string{"changed-body", "missing-body"} {
		f := newHistoricalCaptureRpcFixture(t, mode)
		result, err := f.client(t).readHistoricalCaptureRequest(t.Context(), f.expected, f.childHash, nil)
		if !errors.Is(err, errRpcIntegrity) || result.RequestJSON != "" || f.bodyReads.Load() != 1 {
			t.Fatal("incomplete or foreign body became an execution request", mode, result, err)
		}
	}
}

func TestHistoricalCaptureRequestClosingCanonicalAndRuntimeChangesRefuse(t *testing.T) {
	for _, mode := range []string{"changed-canonical", "changed-parent-runtime"} {
		f := newHistoricalCaptureRpcFixture(t, mode)
		result, err := f.client(t).readHistoricalCaptureRequest(t.Context(), f.expected, f.childHash, nil)
		if !errors.Is(err, errRpcIntegrity) || result.RequestJSON != "" {
			t.Fatal("changed finalized block or parent runtime became a capture request", mode, result, err)
		}
		if mode == "changed-canonical" && !f.afterBody.Load() {
			t.Fatal("closing canonical control failed before actual body read")
		}
	}
}

func TestHistoricalCaptureRequestCancellationJoinsActualBodyRead(t *testing.T) {
	f := newHistoricalCaptureRpcFixture(t, "cancel-body")
	client := f.client(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		result historicalCaptureObservation
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := client.readHistoricalCaptureRequest(ctx, f.expected, f.childHash, nil)
		done <- outcome{result, err}
	}()
	select {
	case <-f.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("actual capture body read did not start")
	}
	cancel()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || errors.Is(result.err, errRpcIntegrity) || result.result.RequestJSON != "" {
			t.Fatal("capture cancellation became authority or a changed-history contradiction", result)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("capture caller did not join after cancellation")
	}
	select {
	case <-f.joined:
	case <-time.After(30 * time.Second):
		t.Fatal("capture HTTP read retained a live request after cancellation")
	}
}

func TestHistoricalCaptureRequestTransientReadKeepsOneCallerBudget(t *testing.T) {
	f := newHistoricalCaptureRpcFixture(t, "retry-body")
	client := f.client(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	waits := 0
	client.retryWait = func(owner context.Context, _ time.Duration) error {
		waits++
		observed, ok := owner.Deadline()
		if !ok || !observed.Equal(deadline) {
			t.Error("nested capture read reset the caller's original budget", observed, deadline)
		}
		return owner.Err()
	}
	result, err := client.readHistoricalCaptureRequest(ctx, f.expected, f.childHash, nil)
	if err != nil || result.RequestJSON == "" || waits != 1 || f.bodyReads.Load() != 2 {
		t.Fatal("transient archive read did not recover within original caller budget", waits, f.bodyReads.Load(), result, err)
	}
}

func TestHistoricalCaptureRequestProfileBindsOriginalCodeAndMetadata(t *testing.T) {
	f := newHistoricalCaptureRpcFixture(t, "success")
	metadata := historicalReplayDigest(sha256.Sum256(f.metadata))
	good := historicalReplayObservationProfile{Schema: "urnetwork-original-wasm-hook-observation-v1", RuntimeCodeSha256: historicalReplayDigest(sha256.Sum256(f.code)), SourceReviewSha256: historicalReplayDigest{7}, MetadataSha256: &metadata, Rules: []historicalReplayHookRule{{Purpose: "fee-withdraw", FunctionIndex: 1, FunctionBodySha256: historicalReplayDigest{8}, OffsetStart: 1, OffsetEnd: 2}}}
	result, err := f.client(t).readHistoricalCaptureRequest(t.Context(), f.expected, f.childHash, &good)
	if err != nil || result.RequestJSON == "" || result.RuntimeAdmitted || result.FeeAdmitted {
		t.Fatal("exact unapproved parent profile was lost or promoted", result, err)
	}
	for _, change := range []string{"code", "metadata"} {
		profile := good
		if change == "code" {
			profile.RuntimeCodeSha256[0] ^= 1
		} else {
			wrong := *good.MetadataSha256
			wrong[0] ^= 1
			profile.MetadataSha256 = &wrong
		}
		result, err := f.client(t).readHistoricalCaptureRequest(t.Context(), f.expected, f.childHash, &profile)
		if err == nil || result.RequestJSON != "" {
			t.Fatal("foreign parent runtime/profile became a capture request", change, result, err)
		}
	}
}
