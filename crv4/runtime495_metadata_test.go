// Exact public schema evidence exercises normal consumed-interface decoders and
// the real signer; it does not authenticate deployed Wasm source or grant
// runtime authority.
package crv4

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// Fixture digests and byte count name immutable protocol evidence: Finney's
// state_getMetadata at block 9578500, the first runtime495 block.
const runtime495TestMetadataSize = 409264
const runtime495TestMetadataSha256 = "cfebbb78baf01a47a9b0bbd97888b46d07a593594f65ca7df231fa9b1b097343"
const runtime495TestMetadataHash = "0x15e58ecd5d479b0aff4e811f8e108db7b97715c288fe67c7d6fe659708f40ed6"

// The state_getStorageHash of :code at block 9578500 (subtensor v495's srtool
// Wasm, SHA-256 22fd1702).
const runtime495TestCodeHash = "0x7f4f60f27f1cbdb92a8413d8280217789229f54189e48a501ada2d25f4c7edf7"

var runtime495TestIdentity = RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 2, StateVersion: 1}

// One bounded gzip member must match both independent content digests.
func runtime495TestMetadataBytes(t *testing.T) []byte {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime495-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(input)
	if err != nil {
		t.Fatal(err)
	}
	reader.Multistream(false)
	raw, readErr := io.ReadAll(io.LimitReader(reader, runtime495TestMetadataSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) != runtime495TestMetadataSize || input.Len() != 0 {
		t.Fatalf("bounded runtime495 metadata member differs: read=%v close=%v bytes=%d trailing=%d", readErr, closeErr, len(raw), input.Len())
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime495TestMetadataSha256 {
		t.Fatal("runtime495 fixture SHA256 differs")
	}
	return raw
}

func runtime495TestMetadata(t *testing.T) *types.Metadata {
	t.Helper()
	metadata, hash, err := DecodeRuntimeMetadata(fmt.Sprintf("0x%x", runtime495TestMetadataBytes(t)))
	if err != nil || hash != runtime495TestMetadataHash {
		t.Fatalf("runtime495 normal metadata decoder/pin: %v %s", err, hash)
	}
	return metadata
}

// All consumed producer and fleet shapes use their existing production checks,
// signed extensions included.
func TestRuntime495ConsumedMetadataInterfaces(t *testing.T) {
	metadata := runtime495TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime495 CRv4 interface differs", err)
	}
	if err := validateValidatorProducerMetadata(metadata); err != nil {
		t.Fatal("runtime495 producer interface differs", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatalf("runtime495 fleet purpose %v differs: %v", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime495TestVersionReply))
	if err != nil || version != runtime495TestIdentity {
		t.Fatalf("runtime495 exact protocol tuple differs: %v %+v", err, version)
	}
}

// The calls the producer signs keep runtime494's indices: 495 appends no call,
// so its unchanged transaction version 2 is the encoding 494 was reviewed at.
func TestRuntime495ConsumedCallIndicesMatchRuntime494(t *testing.T) {
	metadata494, metadata495 := runtime494TestMetadata(t), runtime495TestMetadata(t)
	for _, call := range []string{"Utility.batch_all", "Commitments.set_commitment", "SubtensorModule.commit_timelocked_weights", "SubtensorModule.commit_timelocked_mechanism_weights", "System.remark"} {
		index494, err494 := metadata494.FindCallIndex(call)
		index495, err495 := metadata495.FindCallIndex(call)
		if err494 != nil || err495 != nil || index494 != index495 {
			t.Fatalf("%s index differs: 494 %+v (%v), 495 %+v (%v)", call, index494, err494, index495, err495)
		}
	}
}

// Binds a synthetic exact authentication of the captured 495 schema and API
// declaration, at the transaction version Finney declares.
func runtime495TestSourceFixture(t *testing.T) (*sourceRuntimeCapabilityTestFixture, AuthenticatedRuntimeArtifact) {
	t.Helper()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.metadata = runtime495TestMetadata(t)
	fixture.identity.Version = runtime495TestIdentity
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := original(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			*result.(*json.RawMessage) = json.RawMessage(runtime495TestVersionReply)
		}
		return ctx.Err()
	}
	artifact := fixture.bind(t)
	if artifact.MetadataHash != runtime495TestMetadataHash || artifact.Version != runtime495TestIdentity {
		t.Fatal("exact authenticated runtime495 artifact differs from the captured schema")
	}
	return fixture, artifact
}

// Exact synthetic artifact authentication reaches the public staging capability
// check with the captured metadata and API3 declaration before signing can
// bind, and the bound signing view carries transaction version 2.
func TestRuntime495ValidatorProducerCapability(t *testing.T) {
	fixture, artifact := runtime495TestSourceFixture(t)
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("read-only artifact authentication granted producer signing capability")
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal("runtime495 staging capability refused the exact authenticated artifact", err)
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime495 producer capability could not bind", err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatal("runtime495 signing view lost its exact purpose proof", err)
	}
	if fixture.chain.Runtime.SpecVersion != 495 || fixture.chain.Runtime.TransactionVersion != 2 {
		t.Fatalf("runtime495 signing view binds %d/%d, not the authenticated 495/2", fixture.chain.Runtime.SpecVersion, fixture.chain.Runtime.TransactionVersion)
	}
}

// The production source write, end to end on the captured 495 schema: the live
// metadata builds the atomic batch, the real signer signs it with the bound
// runtime's versions, and the independent fixed encoding (the bytes the chain
// verifies: call, extra, then spec, transaction version, genesis, era birth and
// the absent metadata hash) reproduces the payload and verifies the signature.
// A signature made at transaction version 1 does not verify for runtime495, and
// the signed spec keeps a runtime495 submission from passing as runtime494's.
func TestRuntime495SourceSubmissionSignsTransactionVersion2(t *testing.T) {
	for _, mecid := range []*uint8{nil, new(uint8)} {
		fixture, _ := runtime495TestSourceFixture(t)
		prepared, key := sourcePreparedTest(t)
		prepared.Mecid = mecid
		prepared.SourceCommitment.RuntimeSpec, prepared.SourceCommitment.TransactionVersion = 495, 2
		prepared.SourceCommitment.GenesisHash = fixture.chain.GenesisHash.Hex()
		ciphertext, err := codec.HexDecodeString(prepared.CiphertextHex)
		if err != nil {
			t.Fatal(err)
		}
		call, err := fixture.chain.newSourceCommitmentBatchCall(prepared.Netuid, mecid, types.Hash{9}, ciphertext, prepared.RevealRound, prepared.CommitRevealVersion)
		if err != nil {
			t.Fatal("runtime495 source capability refused the exact authenticated view", err)
		}
		sign := func(transactionVersion types.U32) {
			t.Helper()
			view := *fixture.chain
			runtime := *fixture.chain.Runtime
			runtime.TransactionVersion = transactionVersion
			view.Runtime = &runtime
			ext, err := view.NewSignedExtrinsic(key, call, prepared.AccountNonce)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := codec.Encode(*ext)
			if err != nil {
				t.Fatal(err)
			}
			hash := blake2b.Sum256(raw)
			prepared.ExtrinsicHex, prepared.ExtrinsicHash = codec.HexEncodeToString(raw), types.Hash(hash).Hex()
		}
		sign(2)
		if err := fixture.chain.ValidatePreparedSource(prepared); err != nil {
			t.Fatal("runtime495 metadata changes the fixed source encoding", err)
		}
		if _, err := prepared.Validate(); err != nil {
			t.Fatal("runtime495 signer did not sign the transaction-version-2 payload", err)
		}
		_, _, payload, err := preparedSourceEncoding(prepared)
		if err != nil {
			t.Fatal(err)
		}
		_, _, oldPayload, err := preparedSourceEncoding(sourceRuntime455PreparedTest(t, 461, mecid))
		if err != nil {
			t.Fatal(err)
		}
		// Only the signed spec and transaction version u32s differ from a reviewed
		// transaction-version-1 payload of the same attempt.
		versions := len(payload) - 4*2 - 32*2 - 1
		if len(payload) != len(oldPayload) || !bytes.Equal(payload[:versions], oldPayload[:versions]) ||
			!bytes.Equal(payload[versions:versions+8], []byte{0xef, 0x01, 0, 0, 2, 0, 0, 0}) || !bytes.Equal(payload[versions+8:], oldPayload[versions+8:]) {
			t.Fatalf("runtime495 payload changes more than its signed versions: %x", payload[versions:versions+8])
		}
		sign(1)
		if _, err := prepared.Validate(); err == nil {
			t.Fatal("a transaction-version-1 signature verified as a runtime495 submission")
		}
		sign(2)
		for _, relabel := range []struct{ spec, transaction uint32 }{{495, 1}, {495, 3}, {496, 2}, {494, 2}, {493, 2}} {
			changed := *prepared
			source := *prepared.SourceCommitment
			source.RuntimeSpec, source.TransactionVersion = relabel.spec, relabel.transaction
			changed.SourceCommitment = &source
			if _, err := changed.Validate(); err == nil {
				t.Fatalf("runtime495 submission was accepted as %d/%d", relabel.spec, relabel.transaction)
			}
		}
	}
}

// Only the exact reviewed identities' raised transaction version is a fixed
// source encoding; their neighbours stay unsupported before any signature check.
func TestRuntime495SourceEncodingAdmitsOnlyTheReviewedTransactionVersion(t *testing.T) {
	for _, test := range []struct {
		spec, transaction uint32
		ok                bool
	}{{495, 2, true}, {494, 2, true}, {495, 1, true}, {467, 1, true}, {495, 3, false}, {496, 2, false}, {493, 2, false}, {0, 1, false}} {
		prepared, _ := sourcePreparedTest(t)
		prepared.SourceCommitment.RuntimeSpec, prepared.SourceCommitment.TransactionVersion = test.spec, test.transaction
		if _, _, _, err := preparedSourceEncoding(prepared); (err == nil) != test.ok {
			t.Fatalf("source encoding %d/%d: %v", test.spec, test.transaction, err)
		}
	}
}

// The real 473 anchor admits Finney's runtime495, whose only identity change
// besides the spec is the reviewed transaction version 2, as its own exact
// successor; a neighbouring identity with a raised version is not a successor.
func TestRuntime495IsAnAdmittedSuccessorAtTransactionVersion2(t *testing.T) {
	raw := runtime495TestMetadataBytes(t)
	successor := RuntimeArtifactIdentity{Version: runtime495TestIdentity, CodeHash: runtime495TestCodeHash, MetadataHash: runtime495TestMetadataHash}
	for _, test := range []struct {
		name    string
		version RuntimeVersionIdentity
		want    string
	}{
		{name: "runtime495", version: runtime495TestIdentity},
		{name: "spec496", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 496, TransactionVersion: 2, StateVersion: 1}, want: "not a successor"},
		{name: "transaction3", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 3, StateVersion: 1}, want: "not a successor"},
		{name: "state2", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 2, StateVersion: 2}, want: "not a successor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimeSuccessionTestFixture(t)
			identity := successor
			identity.Version = test.version
			fixture.blocks[runtimeSuccessionNewBlock] = &runtimeSuccessionTestBlock{version: runtimeSuccessionTestVersion(identity, 3), code: successor.CodeHash, metadata: fmt.Sprintf("0x%x", raw)}
			fixture.enable(t, runtimeSuccession473)
			artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, runtimeSuccessionNewBlock, runtimeSuccession473)
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) || len(fixture.admitted) != 0 {
					t.Fatalf("%s was not refused with %q: %v", test.name, test.want, err)
				}
				return
			}
			if err != nil {
				t.Fatal("runtime495 is not an admitted successor of the approved anchor", err)
			}
			approved, ok := artifact.RuntimeSuccessorOf()
			if !ok || approved != runtimeSuccession473 || (RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}) != successor ||
				len(fixture.admitted) != 1 || fixture.admitted[0].Artifact != successor {
				t.Fatalf("runtime495 successor lost its exact identity or anchor: %+v", artifact)
			}
			view := *fixture.chain
			if err := view.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
				t.Fatal("runtime495 successor could not bind the producer signing view", err)
			}
			if err := view.ValidateValidatorProducerRuntimeSuccessor(runtimeSuccession473); err != nil {
				t.Fatal(err)
			}
			if view.Runtime.SpecVersion != 495 || view.Runtime.TransactionVersion != 2 {
				t.Fatalf("runtime495 successor view signs %d/%d", view.Runtime.SpecVersion, view.Runtime.TransactionVersion)
			}
		})
	}
}

// The stake and schedule readers accept the reviewed raised transaction
// version and nothing next to it.
func TestRuntime495StakeReadAdmitsOnlyTheReviewedTransactionVersion(t *testing.T) {
	for _, test := range []struct {
		version RuntimeVersionIdentity
		ok      bool
	}{
		{version: runtime495TestIdentity, ok: true},
		{version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 496, TransactionVersion: 2, StateVersion: 1}},
		{version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 3, StateVersion: 1}},
	} {
		fixture := newValidatorStakeCapabilityTestFixture(t)
		identity := fixture.stake.identity
		identity.version = test.version
		identity.allowed[0].Version = test.version
		fixture.apiVersions = json.RawMessage(`[["0x8375104b299b74c5",3]]`)
		fixture.publish(t)
		_, err := fixture.stake.read()
		if (err == nil) != test.ok {
			t.Fatalf("stake read at %+v: %v", test.version, err)
		}
		if !test.ok && !strings.Contains(err.Error(), "encoding versions are unsupported") {
			t.Fatalf("stake read at %+v refused for another reason: %v", test.version, err)
		}
	}
}
