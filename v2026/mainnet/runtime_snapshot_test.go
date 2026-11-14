// Snapshot tests use synthetic local RPC evidence and never contact a chain.
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

// Serves exact pinned artifact bytes, with a controlled identity or hash fault.
func runtimeSnapshotTestServer(t *testing.T, evmChainId string, wrongCodeHash, changedGenesis bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	code := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	metadata := []byte{109, 101, 116, 97, 255, 0}
	return runtimeSnapshotArtifactTestServer(t, evmChainId, wrongCodeHash, changedGenesis, code, metadata)
}

// Raw artifact sizes are independent of their JSON envelope and hex encoding.
func runtimeSnapshotArtifactTestServer(t *testing.T, evmChainId string, wrongCodeHash, changedGenesis bool, code, metadata []byte, runtimeVersions ...json.RawMessage) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	codeDigest := blake2b.Sum256(code)
	codeHash := "0x" + hex.EncodeToString(codeDigest[:])
	if wrongCodeHash {
		codeHash = testGenesisHash
	}
	artifactReads := &atomic.Int64{}
	genesisReads := &atomic.Int64{}
	versionReads := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			JsonRpc string `json:"jsonrpc"`
			Id      int    `json:"id"`
			Method  string `json:"method"`
			Params  []any  `json:"params"`
		}
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil || call.JsonRpc != "2.0" || call.Id != 1 {
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		var result any
		switch call.Method {
		case "system_chain":
			result = "Bittensor"
		case "system_version":
			result = "synthetic node"
		case "eth_chainId":
			result = evmChainId
		case "chain_getFinalizedHead":
			result = testFinalizedHash
		case "chain_getHeader":
			if len(call.Params) != 1 || call.Params[0] != testFinalizedHash {
				t.Errorf("header was not pinned to finalized hash: %v", call.Params)
			}
			result = identityTestHeader()
		case "chain_getBlockHash":
			if len(call.Params) != 1 {
				t.Errorf("block lookup has invalid params: %v", call.Params)
			}
			if call.Params[0] == float64(0) {
				result = testGenesisHash
				if changedGenesis && genesisReads.Add(1) > 1 {
					result = testFinalizedHash
				}
			} else if call.Params[0] == float64(100) {
				result = testFinalizedHash
			} else {
				t.Errorf("unexpected block lookup: %v", call.Params)
			}
		case "state_getRuntimeVersion":
			if len(call.Params) != 1 || call.Params[0] != testFinalizedHash {
				t.Errorf("runtime version was not pinned: %v", call.Params)
			}
			result = map[string]any{"specName": "synthetic-runtime", "specVersion": 991, "transactionVersion": 1, "stateVersion": 1}
			if len(runtimeVersions) != 0 {
				result = runtimeVersions[min(versionReads.Add(1)-1, int64(len(runtimeVersions)-1))]
			}
		case "state_getStorageHash":
			artifactReads.Add(1)
			if len(call.Params) != 2 || call.Params[0] != runtimeCodeStorageKey || call.Params[1] != testFinalizedHash {
				t.Errorf("code hash read was not pinned: %v", call.Params)
			}
			result = codeHash
		case "state_getStorage":
			artifactReads.Add(1)
			if len(call.Params) != 2 || call.Params[0] != runtimeCodeStorageKey || call.Params[1] != testFinalizedHash {
				t.Errorf("code read was not pinned: %v", call.Params)
			}
			result = "0x" + strings.ToUpper(hex.EncodeToString(code))
		case "state_getMetadata":
			artifactReads.Add(1)
			if len(call.Params) != 1 || call.Params[0] != testFinalizedHash {
				t.Errorf("metadata read was not pinned: %v", call.Params)
			}
			result = "0x" + strings.ToUpper(hex.EncodeToString(metadata))
		default:
			t.Errorf("unexpected RPC method %q", call.Method)
			http.Error(writer, "unexpected method", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}); err != nil {
			t.Errorf("encode synthetic RPC reply: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server, artifactReads
}

// Equal spec and transaction numbers cannot hide a different runtime name or
// trie state version at the same finalized block between two successful reads.
func TestRuntimeSnapshotRejectsFullVersionDriftAtOneBlock(t *testing.T) {
	initial := json.RawMessage(`{"specName":"synthetic-runtime","specVersion":991,"transactionVersion":1,"stateVersion":1}`)
	driftVersions := []json.RawMessage{
		json.RawMessage(`{"specName":"other-runtime","specVersion":991,"transactionVersion":1,"stateVersion":1}`),
		json.RawMessage(`{"specName":"synthetic-runtime","specVersion":991,"transactionVersion":1,"stateVersion":0}`),
	}
	for _, driftVersion := range driftVersions {
		server, reads := runtimeSnapshotArtifactTestServer(t, "0x3c4", false, false, []byte{1}, []byte{2}, initial, driftVersion)
		client, err := newRpcClient(server.URL, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := client.readRuntimeSnapshot(context.Background(), nil)
		if !errors.Is(err, errRpcIntegrity) || snapshot.Schema != "" || reads.Load() != 0 {
			t.Errorf("same-block runtime drift accepted: version=%s schema=%q reads=%d err=%v", driftVersion, snapshot.Schema, reads.Load(), err)
		}
	}
}

// Exact documented raw limits must survive hexadecimal expansion and JSON
// framing, including the trailing newline a normal server encoder writes.
func TestRuntimeSnapshotAcceptsExactRawArtifactLimits(t *testing.T) {
	code := bytes.Repeat([]byte{0xab}, maximumRuntimeSnapshotCodeBytes)
	metadata := bytes.Repeat([]byte{0xcd}, maximumRuntimeSnapshotMetadataBytes)
	server, reads := runtimeSnapshotArtifactTestServer(t, "0x3c4", false, false, code, metadata)
	client, err := newRpcClient(server.URL, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.readRuntimeSnapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	codeDigest := blake2b.Sum256(code)
	metadataDigest := blake2b.Sum256(metadata)
	if len(snapshot.CodeHex) != 2+2*len(code) || len(snapshot.MetadataHex) != 2+2*len(metadata) || snapshot.CodeHash != "0x"+hex.EncodeToString(codeDigest[:]) || snapshot.MetadataHash != "0x"+hex.EncodeToString(metadataDigest[:]) || reads.Load() != 3 {
		t.Fatalf("exact artifact limits changed byte identity: code=%d metadata=%d reads=%d", len(snapshot.CodeHex), len(snapshot.MetadataHex), reads.Load())
	}
}

// Extra wire framing headroom grants no extra raw artifact capacity. An
// artifact one byte over either bound must fail without publishing a snapshot.
func TestRuntimeSnapshotRejectsRawArtifactsAboveLimits(t *testing.T) {
	cases := []struct {
		label        string
		codeSize     int
		metadataSize int
		reads        int64
	}{
		{label: "code", codeSize: maximumRuntimeSnapshotCodeBytes + 1, metadataSize: 1, reads: 2},
		{label: "metadata", codeSize: 1, metadataSize: maximumRuntimeSnapshotMetadataBytes + 1, reads: 3},
	}
	for _, testCase := range cases {
		server, reads := runtimeSnapshotArtifactTestServer(t, "0x3c4", false, false, bytes.Repeat([]byte{0xab}, testCase.codeSize), bytes.Repeat([]byte{0xcd}, testCase.metadataSize))
		client, err := newRpcClient(server.URL, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := client.readRuntimeSnapshot(context.Background(), nil)
		if !errors.Is(err, errRpcIntegrity) || snapshot.Schema != "" || reads.Load() != testCase.reads {
			t.Errorf("oversized %s was not rejected: schema=%q reads=%d err=%v", testCase.label, snapshot.Schema, reads.Load(), err)
		}
	}
}

// The raw bytes, their independent hashes and the enclosing digest must agree.
func TestRuntimeSnapshotBindsOneFinalizedArtifact(t *testing.T) {
	server, artifactReads := runtimeSnapshotTestServer(t, "0x3c4", false, false)
	var stdout, stderr bytes.Buffer
	args := []string{"runtime-snapshot", "--rpc", server.URL, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964"}
	if code := runMain(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("runtime snapshot exit %d: %s", code, stderr.String())
	}
	var envelope runtimeSnapshotEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	snapshot := envelope.Snapshot
	if snapshot.Schema != runtimeSnapshotSchema || snapshot.Admission != "unapproved_observation" || snapshot.Identity.FinalizedHash != testFinalizedHash || snapshot.Identity.EvmChainId != 964 || snapshot.Version.StateVersion != 1 || artifactReads.Load() != 3 {
		t.Fatalf("runtime snapshot lost pinned scope: %+v reads=%d", snapshot, artifactReads.Load())
	}
	code, err := hex.DecodeString(snapshot.CodeHex[2:])
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := hex.DecodeString(snapshot.MetadataHex[2:])
	if err != nil {
		t.Fatal(err)
	}
	codeDigest := blake2b.Sum256(code)
	metadataDigest := blake2b.Sum256(metadata)
	if snapshot.CodeHash != "0x"+hex.EncodeToString(codeDigest[:]) || snapshot.MetadataHash != "0x"+hex.EncodeToString(metadataDigest[:]) {
		t.Fatal("snapshot hashes do not reproduce from retained artifact bytes")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append([]byte(runtimeSnapshotSchema+"\x00"), encoded...))
	if envelope.ContentHash != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatal("snapshot content hash does not reproduce")
	}
}

// A testnet route cannot spend time downloading artifacts under mainnet pins.
func TestRuntimeSnapshotRejectsWrongChainBeforeArtifactReads(t *testing.T) {
	server, artifactReads := runtimeSnapshotTestServer(t, "0x3b1", false, false)
	var stdout, stderr bytes.Buffer
	args := []string{"runtime-snapshot", "--rpc", server.URL, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964"}
	if code := runMain(context.Background(), args, &stdout, &stderr); code != 3 || stdout.Len() != 0 || artifactReads.Load() != 0 {
		t.Fatalf("wrong chain got exit %d, output %d bytes, artifact reads %d: %s", code, stdout.Len(), artifactReads.Load(), stderr.String())
	}
}

// A server-reported code hash cannot certify different raw Wasm bytes.
func TestRuntimeSnapshotRejectsCodeHashMismatch(t *testing.T) {
	server, _ := runtimeSnapshotTestServer(t, "0x3c4", true, false)
	var stdout, stderr bytes.Buffer
	if code := runMain(context.Background(), []string{"runtime-snapshot", "--rpc", server.URL}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "code bytes differ") {
		t.Fatalf("code mismatch got exit %d and output %q: %s", code, stdout.String(), stderr.String())
	}
}

// A proxy retarget after artifact reads cannot publish the old route's bytes.
func TestRuntimeSnapshotRejectsRetargetedGenesis(t *testing.T) {
	server, _ := runtimeSnapshotTestServer(t, "0x3c4", false, true)
	var stdout, stderr bytes.Buffer
	if code := runMain(context.Background(), []string{"runtime-snapshot", "--rpc", server.URL}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "canonical block changed") {
		t.Fatalf("route retarget got exit %d and output %q: %s", code, stdout.String(), stderr.String())
	}
}

// Observation retains unknown metadata for review instead of claiming that an
// old decoder can authorize a new runtime.
func TestRuntimeSnapshotRetainsUnknownMetadataForReview(t *testing.T) {
	server, _ := runtimeSnapshotTestServer(t, "0x3c4", false, false)
	var stdout, stderr bytes.Buffer
	if code := runMain(context.Background(), []string{"runtime-snapshot", "--rpc", server.URL}, &stdout, &stderr); code != 0 {
		t.Fatalf("unknown metadata was discarded: %d %s", code, stderr.String())
	}
	var envelope runtimeSnapshotEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.Snapshot.MetadataHex != "0x6d657461ff00" {
		t.Fatalf("unknown metadata did not survive raw capture: %v %q", err, envelope.Snapshot.MetadataHex)
	}
}
