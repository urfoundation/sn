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

	"github.com/urfoundation/sn/crv4"
)

// Reads only artifact identity fields from the independently attested manifest.
type fleetRuntime458TestArtifact struct {
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
func decodeFleetRuntime458TestMetadata(encoded []byte, artifact fleetRuntime458TestArtifact) ([]byte, error) {
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
func fleetRuntime458TestArtifactInputs(t *testing.T) (crv4.RuntimeArtifactIdentity, fleetRuntime458TestArtifact, []byte, []byte) {
	t.Helper()
	return fleetRuntimeTestArtifactInputs(t, 458, "a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7")
}

// Current authority is pinned independently to the source reviewed in
// docs/spec/runtime-467-audit.md, never to a production current-version alias.
func fleetRuntimeCurrentTestArtifactInputs(t *testing.T) (crv4.RuntimeArtifactIdentity, fleetRuntime458TestArtifact, []byte, []byte) {
	t.Helper()
	return fleetRuntimeTestArtifactInputs(t, 467, "c6bcb4a7400764c94c1d1b1938514c6c2dd3d33b")
}

// Reads a reviewed version fixture while retaining its independent manifest pin.
func fleetRuntimeTestArtifactInputs(t *testing.T, spec uint32, commit string) (crv4.RuntimeArtifactIdentity, fleetRuntime458TestArtifact, []byte, []byte) {
	t.Helper()
	manifestBytes, err := os.ReadFile(filepath.Join("..", "docs", "spec", "runtime-metadata-artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		RuntimeSpecName    string                        `json:"runtime_spec_name"`
		TransactionVersion uint32                        `json:"transaction_version"`
		StateVersion       uint8                         `json:"state_version"`
		Artifacts          []fleetRuntime458TestArtifact `json:"artifacts"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	var selected fleetRuntime458TestArtifact
	count := 0
	for _, artifact := range manifest.Artifacts {
		if artifact.SpecVersion == spec {
			selected = artifact
			count++
		}
	}
	if count != 1 || selected.SourceRefKind != "commit" || selected.SourceRefName != commit || selected.SourceCommit != selected.SourceRefName {
		t.Fatal("runtime artifact has missing, duplicate or unreviewed source provenance")
	}
	metadataPath := filepath.Join("testdata", fmt.Sprintf("runtime%d-metadata.scale.gz.base64", spec))
	if spec == 467 {
		// Share the retained protocol bytes, but authenticate size, SHA-256
		// and BLAKE2b against the independently pinned manifest row below.
		metadataPath = filepath.Join("..", "crv4", "runtime-profile-v1.scale.gz.base64")
	}
	encoded, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := decodeFleetRuntime458TestMetadata(encoded, selected)
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
type fleetRuntime458HttpTestFixture struct {
	stateLock sync.Mutex
	version   crv4.RuntimeVersionIdentity
	calls     []string
	chain     *crv4.Chain
	stop      func()
}

// Serves the actual reviewed bytes and exact-block identity methods through
// the pinned native Rpc decoder, never through an authentication callback.
func newFleetRuntime458HttpTestFixture(t *testing.T, expected crv4.RuntimeArtifactIdentity, metadata []byte) *fleetRuntime458HttpTestFixture {
	t.Helper()
	self := &fleetRuntime458HttpTestFixture{version: expected.Version}
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
	expected, _, _, _ := fleetRuntimeCurrentTestArtifactInputs(t)
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

// Both the current-head and receipt-block authenticator bind real467 metadata;
// only its immutable decoding is reused, never either block's version or code.
func TestFleetFinalizedRuntime467AuthenticatesIndependentExactHeads(t *testing.T) {
	expected, _, _, metadata := fleetRuntimeCurrentTestArtifactInputs(t)
	fixture := newFleetRuntime458HttpTestFixture(t, expected, metadata)
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

// Current467 code and metadata cannot make the preceding461 version current;
// refusal precedes code/metadata reads and leaves the old binding untouched.
func TestFleetFinalizedRuntime467RejectsPrecedingSpecWithReviewedBytes(t *testing.T) {
	expected, _, _, metadata := fleetRuntimeCurrentTestArtifactInputs(t)
	fixture := newFleetRuntime458HttpTestFixture(t, expected, metadata)
	fixture.stateLock.Lock()
	fixture.version.SpecVersion = 461
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

// A previously cached467 artifact cannot bypass the independently observed
// receipt block's runtime version, even after a successful current-head bind.
func TestFleetRuntime467RejectsReceiptRuntimeDriftAfterCurrentAuthentication(t *testing.T) {
	expected, _, _, metadata := fleetRuntimeCurrentTestArtifactInputs(t)
	fixture := newFleetRuntime458HttpTestFixture(t, expected, metadata)
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
	_, artifact, encoded, _ := fleetRuntime458TestArtifactInputs(t)
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*fleetRuntime458TestArtifact){
		func(value *fleetRuntime458TestArtifact) { value.MetadataSize-- },
		func(value *fleetRuntime458TestArtifact) { value.MetadataSize++ },
		func(value *fleetRuntime458TestArtifact) { value.MetadataSize = 1024*1024 + 1 },
		func(value *fleetRuntime458TestArtifact) { value.MetadataSha256 = fmt.Sprintf("%064x", 1) },
		func(value *fleetRuntime458TestArtifact) { value.MetadataHash = "0x" + fmt.Sprintf("%064x", 2) },
	} {
		mutated := artifact
		change(&mutated)
		if _, err := decodeFleetRuntime458TestMetadata(encoded, mutated); err == nil {
			t.Fatalf("changed metadata authority was accepted: %+v", mutated)
		}
	}
	for _, invalid := range [][]byte{nil, bytes.Repeat([]byte{'A'}, 256*1024+1), []byte("not-base64"), []byte(base64.StdEncoding.EncodeToString(compressed[:len(compressed)-1])), []byte(base64.StdEncoding.EncodeToString(append(bytes.Clone(compressed), compressed...)))} {
		if _, err := decodeFleetRuntime458TestMetadata(invalid, artifact); err == nil {
			t.Fatal("invalid metadata encoding or framing was accepted")
		}
	}
}

// The old metadata remains exactly decodable, but cannot authorize a current
// fleet operation or replace an existing signing binding.
func TestFleetRuntime458Retains455EvidenceWithoutCurrentAuthority(t *testing.T) {
	expected, _, _, metadata := fleetRuntimeTestArtifactInputs(t, 455, "67dcf7f791dc495064c293f080a0702cb433e51e")
	fixture := newFleetRuntime458HttpTestFixture(t, expected, metadata)
	priorMetadata := types.NewMetadataV14()
	priorRuntime := &types.RuntimeVersion{SpecName: "synthetic-prior", SpecVersion: 9, TransactionVersion: 3}
	fixture.chain.Meta, fixture.chain.Runtime = priorMetadata, priorRuntime
	if _, err := authenticateAndBindFleetRuntimeFinalizedContext(context.Background(), fixture.chain); err == nil {
		t.Fatal("historical455 artifact acquired current fleet authority")
	}
	if fixture.chain.Meta != priorMetadata || fixture.chain.Runtime != priorRuntime {
		t.Fatal("historical artifact changed the current signing binding")
	}
	if _, err := crv4.AuthenticateRuntimeArtifactAtContext(context.Background(), fixture.chain, types.Hash{2}, expected); err != nil {
		t.Fatalf("explicit historical artifact lost exact metadata decoding: %v", err)
	}
}

// The old metadata remains exactly decodable, but cannot authorize a current
// fleet operation or replace an existing signing binding.
func TestFleetRuntime460Retains458EvidenceWithoutCurrentAuthority(t *testing.T) {
	expected, _, _, metadata := fleetRuntime458TestArtifactInputs(t)
	fixture := newFleetRuntime458HttpTestFixture(t, expected, metadata)
	priorMetadata := types.NewMetadataV14()
	priorRuntime := &types.RuntimeVersion{SpecName: "synthetic-prior", SpecVersion: 9, TransactionVersion: 3}
	fixture.chain.Meta, fixture.chain.Runtime = priorMetadata, priorRuntime
	if _, err := authenticateAndBindFleetRuntimeFinalizedContext(context.Background(), fixture.chain); err == nil {
		t.Fatal("historical458 artifact acquired current fleet authority")
	}
	if fixture.chain.Meta != priorMetadata || fixture.chain.Runtime != priorRuntime {
		t.Fatal("historical artifact changed the current signing binding")
	}
	if _, err := crv4.AuthenticateRuntimeArtifactAtContext(context.Background(), fixture.chain, types.Hash{2}, expected); err != nil {
		t.Fatalf("explicit historical artifact lost exact metadata decoding: %v", err)
	}
}

// The old metadata remains exactly decodable, but cannot authorize a current
// fleet operation or replace an existing signing binding.
func TestFleetRuntime467RetainsPrecedingEvidenceWithoutCurrentAuthority(t *testing.T) {
	cases := []struct {
		spec   uint32
		commit string
	}{
		{spec: 460, commit: "8d5f20ec1a5e5d90295d43046dacdefc54aaed06"},
		{spec: 461, commit: "7c9d45ebd423c7f6b0b477e11414fe2fe3a3794b"},
	}
	for _, test := range cases {
		expected, _, _, metadata := fleetRuntimeTestArtifactInputs(t, test.spec, test.commit)
		fixture := newFleetRuntime458HttpTestFixture(t, expected, metadata)
		priorMetadata := types.NewMetadataV14()
		priorRuntime := &types.RuntimeVersion{SpecName: "synthetic-prior", SpecVersion: 9, TransactionVersion: 3}
		fixture.chain.Meta, fixture.chain.Runtime = priorMetadata, priorRuntime
		if _, err := authenticateAndBindFleetRuntimeFinalizedContext(context.Background(), fixture.chain); err == nil {
			t.Fatalf("historical%d artifact acquired current fleet authority", test.spec)
		}
		if fixture.chain.Meta != priorMetadata || fixture.chain.Runtime != priorRuntime {
			t.Fatalf("historical%d artifact changed the current signing binding", test.spec)
		}
		if _, err := crv4.AuthenticateRuntimeArtifactAtContext(context.Background(), fixture.chain, types.Hash{2}, expected); err != nil {
			t.Fatalf("historical%d artifact lost exact metadata decoding: %v", test.spec, err)
		}
		fixture.stop()
	}
}
