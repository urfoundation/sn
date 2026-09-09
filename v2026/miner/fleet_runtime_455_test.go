// Uses the reviewed protocol artifact through real local Http responses. No
// endpoint, account, observed chain boundary or Rpc capture is fixture data.
package miner

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	"sync"
	"testing"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Reads only artifact identity fields from the independently attested manifest.
type fleetRuntime455TestArtifact struct {
	SpecVersion    uint32 `json:"spec_version"`
	SourceRefKind  string `json:"source_ref_kind"`
	SourceRefName  string `json:"source_ref_name"`
	SourceCommit   string `json:"source_commit"`
	CodeHash       string `json:"code_blake2b_256"`
	MetadataSize   int64  `json:"metadata_size"`
	MetadataSha256 string `json:"metadata_sha256"`
	MetadataHash   string `json:"metadata_blake2b_256"`
}

// Decompression is limited before hashing; extra members and trailing bytes
// are refused. The manifest cannot authorize an unbounded fixture allocation.
func decodeFleetRuntime455TestMetadata(encoded []byte, artifact fleetRuntime455TestArtifact) ([]byte, error) {
	if len(encoded) == 0 || len(encoded) > 256*1024 || artifact.MetadataSize <= 0 || artifact.MetadataSize > 1024*1024 {
		return nil, errors.New("runtime metadata fixture bounds are invalid")
	}
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		return nil, err
	}
	input := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(input)
	if err != nil {
		return nil, err
	}
	reader.Multistream(false)
	metadata, readErr := io.ReadAll(io.LimitReader(reader, artifact.MetadataSize+1))
	if err := errors.Join(readErr, reader.Close()); err != nil {
		return nil, err
	}
	if int64(len(metadata)) != artifact.MetadataSize || input.Len() != 0 {
		return nil, errors.New("runtime metadata fixture length or framing differs")
	}
	sha := sha256.Sum256(metadata)
	blake := blake2b.Sum256(metadata)
	if hex.EncodeToString(sha[:]) != artifact.MetadataSha256 || "0x"+hex.EncodeToString(blake[:]) != artifact.MetadataHash {
		return nil, errors.New("runtime metadata fixture digest differs")
	}
	return metadata, nil
}

// Selects the exact reviewed commit artifact without consulting production
// constants or the deliberately not-yet-regenerated release lock.
func fleetRuntime455TestArtifactInputs(t *testing.T) (crv4.RuntimeArtifactIdentity, fleetRuntime455TestArtifact, []byte, []byte) {
	t.Helper()
	manifestBytes, err := os.ReadFile(filepath.Join("..", "docs", "spec", "runtime-metadata-artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		RuntimeSpecName    string                        `json:"runtime_spec_name"`
		TransactionVersion uint32                        `json:"transaction_version"`
		StateVersion       uint8                         `json:"state_version"`
		Artifacts          []fleetRuntime455TestArtifact `json:"artifacts"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	var selected fleetRuntime455TestArtifact
	count := 0
	for _, artifact := range manifest.Artifacts {
		if artifact.SpecVersion == 455 {
			selected = artifact
			count++
		}
	}
	if count != 1 || selected.SourceRefKind != "commit" || selected.SourceRefName != "67dcf7f791dc495064c293f080a0702cb433e51e" || selected.SourceCommit != selected.SourceRefName {
		t.Fatal("runtime455 artifact has missing, duplicate or unreviewed source provenance")
	}
	encoded, err := os.ReadFile(filepath.Join("testdata", "runtime455-metadata.scale.gz.base64"))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := decodeFleetRuntime455TestMetadata(encoded, selected)
	if err != nil {
		t.Fatal(err)
	}
	expected := crv4.RuntimeArtifactIdentity{
		Version:  crv4.RuntimeVersionIdentity{SpecName: manifest.RuntimeSpecName, SpecVersion: selected.SpecVersion, TransactionVersion: manifest.TransactionVersion, StateVersion: manifest.StateVersion},
		CodeHash: selected.CodeHash, MetadataHash: selected.MetadataHash,
	}
	return expected, selected, encoded, metadata
}

// Owns the real transport and synchronized response state. Synthetic heads
// are the only admitted state roots; tests cannot accidentally request latest.
type fleetRuntime455HttpTestFixture struct {
	stateLock sync.Mutex
	version   crv4.RuntimeVersionIdentity
	calls     []string
	chain     *crv4.Chain
	stop      func()
}

// Serves the actual reviewed bytes and exact-block identity methods through
// the pinned native Rpc decoder, never through an authentication callback.
func newFleetRuntime455HttpTestFixture(t *testing.T, expected crv4.RuntimeArtifactIdentity, metadata []byte) *fleetRuntime455HttpTestFixture {
	t.Helper()
	self := &fleetRuntime455HttpTestFixture{version: expected.Version}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params []string        `json:"params"`
		}
		decoder := json.NewDecoder(io.LimitReader(request.Body, 4096))
		if err := decoder.Decode(&call); err != nil {
			t.Errorf("decode fleet Http request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			t.Error("fleet Http request has trailing bytes")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var result any
		callLabel := call.Method
		if call.Method == "chain_getFinalizedHead" {
			if len(call.Params) != 0 {
				t.Error("finalized head request has parameters")
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			result = (types.Hash{1}).Hex()
		} else {
			wantCount := 1
			if call.Method == "state_getStorageHash" {
				wantCount = 2
			}
			if len(call.Params) != wantCount || (wantCount == 2 && call.Params[0] != "0x3a636f6465") {
				t.Errorf("fleet %s parameters differ: %v", call.Method, call.Params)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			blockHash := call.Params[len(call.Params)-1]
			if blockHash != (types.Hash{1}).Hex() && blockHash != (types.Hash{2}).Hex() {
				t.Errorf("fleet %s lost its synthetic exact state root", call.Method)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			callLabel += "@" + blockHash
			switch call.Method {
			case "state_getRuntimeVersion":
				self.stateLock.Lock()
				result = self.version
				self.stateLock.Unlock()
			case "state_getStorageHash":
				result = expected.CodeHash
			case "state_getMetadata":
				result = "0x" + hex.EncodeToString(metadata)
			default:
				t.Errorf("unexpected fleet Http method %s", call.Method)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		self.stateLock.Lock()
		self.calls = append(self.calls, callLabel)
		self.stateLock.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}); err != nil {
			t.Errorf("write fleet Http response: %v", err)
		}
	}))
	client, err := gsrpcgeth.DialContext(context.Background(), server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	self.chain = fleetRuntimeTestChain(&fleetRuntimeTestClient{callContext: client.CallContext, close: client.Close})
	var once sync.Once
	self.stop = func() { once.Do(func() { client.Close(); server.Close() }) }
	t.Cleanup(self.stop)
	return self
}

// A truthful current source tuple is independently joined to both the actual
// artifact and public profile. The separate release-lock assertion still runs.
func TestFleetRuntimeArtifactMatchesReviewedMetadataManifest(t *testing.T) {
	expected, _, _, _ := fleetRuntime455TestArtifactInputs(t)
	if observed := fleetReleaseRuntimeArtifact(); observed != expected {
		t.Fatalf("fleet artifact %+v does not match reviewed metadata manifest %+v", observed, expected)
	}
	var public struct {
		Chain struct {
			ExpectedRuntimeSpec        uint32 `yaml:"expected_runtime_spec"`
			ExpectedTransactionVersion uint32 `yaml:"expected_transaction_version"`
			ExpectedStateVersion       uint8  `yaml:"expected_state_version"`
		} `yaml:"chain"`
	}
	encoded, err := os.ReadFile(filepath.Join("..", "deploy", "testnet", "public.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	if expected.Version.SpecVersion != public.Chain.ExpectedRuntimeSpec || expected.Version.TransactionVersion != public.Chain.ExpectedTransactionVersion || expected.Version.StateVersion != public.Chain.ExpectedStateVersion {
		t.Fatal("reviewed artifact and public current runtime profile differ")
	}
}

// Both the current-head and receipt-block authenticator bind real455 metadata;
// only its immutable decoding is reused, never either block's version or code.
func TestFleetFinalizedRuntime455AuthenticatesIndependentExactHeads(t *testing.T) {
	expected, _, _, metadata := fleetRuntime455TestArtifactInputs(t)
	fixture := newFleetRuntime455HttpTestFixture(t, expected, metadata)
	oldMetadata := types.NewMetadataV14()
	fixture.chain.Meta = oldMetadata
	fixture.chain.Runtime = &types.RuntimeVersion{SpecName: expected.Version.SpecName, SpecVersion: 454, TransactionVersion: 1}
	finalized, err := authenticateAndBindFleetRuntimeFinalizedContext(context.Background(), fixture.chain)
	if err != nil || finalized != (types.Hash{1}) || fixture.chain.Meta == oldMetadata || fixture.chain.Meta == nil || fixture.chain.Runtime == nil || uint32(fixture.chain.Runtime.SpecVersion) != expected.Version.SpecVersion {
		t.Fatalf("real finalized runtime authentication failed: head=%s error=%v", finalized.Hex(), err)
	}
	callIndex, err := fixture.chain.Meta.FindCallIndex("Commitments.set_commitment")
	if err != nil || callIndex.SectionIndex != 18 || callIndex.MethodIndex != 0 {
		t.Fatalf("reviewed metadata commitment call differs: %+v error=%v", callIndex, err)
	}
	boundMetadata := fixture.chain.Meta
	if err := authenticateAndBindFleetRuntimeAtContext(context.Background(), fixture.chain, types.Hash{2}); err != nil {
		t.Fatal(err)
	}
	fixture.stop()
	want := []string{"chain_getFinalizedHead", "state_getRuntimeVersion@" + (types.Hash{1}).Hex(), "state_getStorageHash@" + (types.Hash{1}).Hex(), "state_getMetadata@" + (types.Hash{1}).Hex(), "state_getRuntimeVersion@" + (types.Hash{2}).Hex(), "state_getStorageHash@" + (types.Hash{2}).Hex()}
	if !reflect.DeepEqual(fixture.calls, want) || fixture.chain.Meta != boundMetadata {
		t.Fatalf("exact heads did not preserve independent identity reads and one metadata decode: %v", fixture.calls)
	}
}

// Current455 code and metadata cannot make a historical454 version current;
// refusal precedes code/metadata reads and leaves the old binding untouched.
func TestFleetFinalizedRuntime455RejectsPrecedingSpecWithReviewedBytes(t *testing.T) {
	expected, _, _, metadata := fleetRuntime455TestArtifactInputs(t)
	fixture := newFleetRuntime455HttpTestFixture(t, expected, metadata)
	fixture.stateLock.Lock()
	fixture.version.SpecVersion--
	fixture.stateLock.Unlock()
	oldMetadata := types.NewMetadataV14()
	oldRuntime := &types.RuntimeVersion{SpecName: "retained-test-runtime", SpecVersion: 7, TransactionVersion: 8}
	fixture.chain.Meta, fixture.chain.Runtime = oldMetadata, oldRuntime
	finalized, err := authenticateAndBindFleetRuntimeFinalizedContext(context.Background(), fixture.chain)
	fixture.stop()
	if err == nil || finalized != (types.Hash{}) || fixture.chain.Meta != oldMetadata || fixture.chain.Runtime != oldRuntime || !reflect.DeepEqual(fixture.calls, []string{"chain_getFinalizedHead", "state_getRuntimeVersion@" + (types.Hash{1}).Hex()}) {
		t.Fatalf("preceding runtime acquired current authority: calls=%v error=%v", fixture.calls, err)
	}
}

// A previously cached455 artifact cannot bypass the independently observed
// receipt block's runtime version, even after a successful current-head bind.
func TestFleetRuntime455RejectsReceiptRuntimeDriftAfterCurrentAuthentication(t *testing.T) {
	expected, _, _, metadata := fleetRuntime455TestArtifactInputs(t)
	fixture := newFleetRuntime455HttpTestFixture(t, expected, metadata)
	if _, err := authenticateAndBindFleetRuntimeFinalizedContext(context.Background(), fixture.chain); err != nil {
		t.Fatal(err)
	}
	oldMetadata, oldRuntime := fixture.chain.Meta, fixture.chain.Runtime
	fixture.stateLock.Lock()
	fixture.version.SpecVersion++
	fixture.stateLock.Unlock()
	receipt := &crv4.FinalizedCommitment{FinalizedHash: types.Hash{2}, FinalizedAt: 2}
	_, err := verifyPinnedFleetCommitmentWriteContext(context.Background(), fixture.chain, 42, [32]byte{3}, [32]byte{4}, receipt)
	fixture.stop()
	if err == nil || fixture.chain.Meta != oldMetadata || fixture.chain.Runtime != oldRuntime || len(fixture.calls) != 5 || fixture.calls[4] != "state_getRuntimeVersion@"+(types.Hash{2}).Hex() {
		t.Fatalf("cached current artifact bypassed receipt identity: calls=%v error=%v", fixture.calls, err)
	}
}

// Bounds, framing and both independent digests are checked separately, so a
// metadata decompression success cannot conceal another authority mismatch.
func TestFleetRuntimeMetadataFixtureRejectsBoundFramingAndDigestDrift(t *testing.T) {
	_, artifact, encoded, _ := fleetRuntime455TestArtifactInputs(t)
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*fleetRuntime455TestArtifact){
		func(value *fleetRuntime455TestArtifact) { value.MetadataSize-- },
		func(value *fleetRuntime455TestArtifact) { value.MetadataSize++ },
		func(value *fleetRuntime455TestArtifact) { value.MetadataSize = 1024*1024 + 1 },
		func(value *fleetRuntime455TestArtifact) { value.MetadataSha256 = fmt.Sprintf("%064x", 1) },
		func(value *fleetRuntime455TestArtifact) { value.MetadataHash = "0x" + fmt.Sprintf("%064x", 2) },
	} {
		mutated := artifact
		change(&mutated)
		if _, err := decodeFleetRuntime455TestMetadata(encoded, mutated); err == nil {
			t.Fatalf("changed metadata authority was accepted: %+v", mutated)
		}
	}
	for _, invalid := range [][]byte{nil, bytes.Repeat([]byte{'A'}, 256*1024+1), []byte("not-base64"), []byte(base64.StdEncoding.EncodeToString(compressed[:len(compressed)-1])), []byte(base64.StdEncoding.EncodeToString(append(bytes.Clone(compressed), compressed...)))} {
		if _, err := decodeFleetRuntime455TestMetadata(invalid, artifact); err == nil {
			t.Fatal("invalid metadata encoding or framing was accepted")
		}
	}
}
