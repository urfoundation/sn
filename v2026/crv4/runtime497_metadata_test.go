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
	"encoding/binary"
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
// state_getMetadata at block 9587045, the first runtime497 block.
const runtime497TestMetadataSize = 409264
const runtime497TestMetadataSha256 = "d54cab26911b8c2d840b5bc0534945e43d43cf7be23266d5100f9c0c64ecdd4e"
const runtime497TestMetadataHash = "0x9a042e5c8bb2864af87d97cfa6739ad4d3d1a852de901df51e6a8d843bc76451"

// The state_getStorageHash of :code at block 9587045 (subtensor v497's srtool
// Wasm, SHA-256 a335ac30).
const runtime497TestCodeHash = "0x1ef826aebed0d77300b7da09d4e3b23ad507317d72c8c01d827537643dbf2c15"

var runtime497TestIdentity = RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 497, TransactionVersion: 2, StateVersion: 1}

// One bounded gzip member must match both independent content digests.
func runtime497TestMetadataBytes(t *testing.T) []byte {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime497-metadata.scale.gz.base64")
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
	raw, readErr := io.ReadAll(io.LimitReader(reader, runtime497TestMetadataSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) != runtime497TestMetadataSize || input.Len() != 0 {
		t.Fatalf("bounded runtime497 metadata member differs: read=%v close=%v bytes=%d trailing=%d", readErr, closeErr, len(raw), input.Len())
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime497TestMetadataSha256 {
		t.Fatal("runtime497 fixture SHA256 differs")
	}
	return raw
}

func runtime497TestMetadata(t *testing.T) *types.Metadata {
	t.Helper()
	metadata, hash, err := DecodeRuntimeMetadata(fmt.Sprintf("0x%x", runtime497TestMetadataBytes(t)))
	if err != nil || hash != runtime497TestMetadataHash {
		t.Fatalf("runtime497 normal metadata decoder/pin: %v %s", err, hash)
	}
	return metadata
}

// The spec version inside an encoded System Version constant: spec_name and
// impl_name (compact-length text), then the authoring and spec version u32s.
func runtime497TestSpecVersionOffset(t *testing.T, value []byte) int {
	t.Helper()
	offset := 0
	for range 2 {
		if offset >= len(value) || value[offset]&3 != 0 {
			t.Fatal("System Version constant does not start with two short texts")
		}
		offset += 1 + int(value[offset]>>2)
	}
	offset += 4
	if offset+4 > len(value) {
		t.Fatal("System Version constant is too short for its spec version")
	}
	return offset
}

// The whole schema is runtime495's but for one byte: the spec version inside
// the System Version constant. Every type, call, event, error, storage item,
// constant and signed extension therefore keeps its 495 encoding.
func TestRuntime497MetadataDiffersFromRuntime495OnlyInSpecVersion(t *testing.T) {
	raw495, raw497 := runtime495TestMetadataBytes(t), runtime497TestMetadataBytes(t)
	if len(raw495) != len(raw497) {
		t.Fatalf("metadata sizes differ: %d %d", len(raw495), len(raw497))
	}
	var differing []int
	for index := range raw495 {
		if raw495[index] != raw497[index] {
			differing = append(differing, index)
		}
	}
	if len(differing) != 1 {
		t.Fatalf("metadata differs in %d bytes, not the one spec version byte: %v", len(differing), differing[:min(len(differing), 8)])
	}
	version495, err := runtime495TestMetadata(t).FindConstantValue("System", "Version")
	if err != nil {
		t.Fatal(err)
	}
	version497, err := runtime497TestMetadata(t).FindConstantValue("System", "Version")
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(raw495, version495)
	if at < 0 || bytes.Index(raw495[at+1:], version495) >= 0 || !bytes.Equal(raw497[at:at+len(version497)], version497) {
		t.Fatal("the System Version constant is not one exact span of both metadata blobs")
	}
	spec := runtime497TestSpecVersionOffset(t, version495)
	if differing[0] < at+spec || differing[0] >= at+spec+4 ||
		binary.LittleEndian.Uint32(version495[spec:]) != 495 || binary.LittleEndian.Uint32(version497[spec:]) != 497 {
		t.Fatalf("the one differing byte (offset %d) is not the System Version spec version (constant at %d, spec at +%d)", differing[0], at, spec)
	}
}

// All consumed producer and fleet shapes use their existing production checks,
// signed extensions included.
func TestRuntime497ConsumedMetadataInterfaces(t *testing.T) {
	metadata := runtime497TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime497 CRv4 interface differs", err)
	}
	if err := validateValidatorProducerMetadata(metadata); err != nil {
		t.Fatal("runtime497 producer interface differs", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatalf("runtime497 fleet purpose %v differs: %v", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime497TestVersionReply))
	if err != nil || version != runtime497TestIdentity {
		t.Fatalf("runtime497 exact protocol tuple differs: %v %+v", err, version)
	}
}

// The calls the producer signs keep runtime495's indices, so 497's unchanged
// transaction version 2 is the encoding 494 and 495 were reviewed at.
func TestRuntime497ConsumedCallIndicesMatchRuntime495(t *testing.T) {
	metadata495, metadata497 := runtime495TestMetadata(t), runtime497TestMetadata(t)
	for _, call := range []string{"Utility.batch_all", "Commitments.set_commitment", "SubtensorModule.commit_timelocked_weights", "SubtensorModule.commit_timelocked_mechanism_weights", "System.remark"} {
		index495, err495 := metadata495.FindCallIndex(call)
		index497, err497 := metadata497.FindCallIndex(call)
		if err495 != nil || err497 != nil || index495 != index497 {
			t.Fatalf("%s index differs: 495 %+v (%v), 497 %+v (%v)", call, index495, err495, index497, err497)
		}
	}
}

// Binds a synthetic exact authentication of the captured 497 schema and API
// declaration, at the transaction version Finney declares.
func runtime497TestSourceFixture(t *testing.T) (*sourceRuntimeCapabilityTestFixture, AuthenticatedRuntimeArtifact) {
	t.Helper()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.metadata = runtime497TestMetadata(t)
	fixture.identity.Version = runtime497TestIdentity
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := original(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			*result.(*json.RawMessage) = json.RawMessage(runtime497TestVersionReply)
		}
		return ctx.Err()
	}
	artifact := fixture.bind(t)
	if artifact.MetadataHash != runtime497TestMetadataHash || artifact.Version != runtime497TestIdentity {
		t.Fatal("exact authenticated runtime497 artifact differs from the captured schema")
	}
	return fixture, artifact
}

// Exact synthetic artifact authentication reaches the public staging capability
// check with the captured metadata and API3 declaration before signing can
// bind, and the bound signing view carries transaction version 2.
func TestRuntime497ValidatorProducerCapability(t *testing.T) {
	fixture, artifact := runtime497TestSourceFixture(t)
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("read-only artifact authentication granted producer signing capability")
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal("runtime497 staging capability refused the exact authenticated artifact", err)
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime497 producer capability could not bind", err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatal("runtime497 signing view lost its exact purpose proof", err)
	}
	if fixture.chain.Runtime.SpecVersion != 497 || fixture.chain.Runtime.TransactionVersion != 2 {
		t.Fatalf("runtime497 signing view binds %d/%d, not the authenticated 497/2", fixture.chain.Runtime.SpecVersion, fixture.chain.Runtime.TransactionVersion)
	}
}

// The production source write, end to end on the captured 497 schema: the live
// metadata builds the atomic batch, the real signer signs it with the bound
// runtime's versions, and the independent fixed encoding (the bytes the chain
// verifies: call, extra, then spec, transaction version, genesis, era birth and
// the absent metadata hash) reproduces the payload and verifies the signature.
// A signature made at transaction version 1 does not verify for runtime497, and
// the signed spec keeps a runtime497 submission from passing as runtime495's.
func TestRuntime497SourceSubmissionSignsTransactionVersion2(t *testing.T) {
	for _, mecid := range []*uint8{nil, new(uint8)} {
		fixture, _ := runtime497TestSourceFixture(t)
		prepared, key := sourcePreparedTest(t)
		prepared.Mecid = mecid
		prepared.SourceCommitment.RuntimeSpec, prepared.SourceCommitment.TransactionVersion = 497, 2
		prepared.SourceCommitment.GenesisHash = fixture.chain.GenesisHash.Hex()
		ciphertext, err := codec.HexDecodeString(prepared.CiphertextHex)
		if err != nil {
			t.Fatal(err)
		}
		call, err := fixture.chain.newSourceCommitmentBatchCall(prepared.Netuid, mecid, types.Hash{9}, ciphertext, prepared.RevealRound, prepared.CommitRevealVersion)
		if err != nil {
			t.Fatal("runtime497 source capability refused the exact authenticated view", err)
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
			t.Fatal("runtime497 metadata changes the fixed source encoding", err)
		}
		if _, err := prepared.Validate(); err != nil {
			t.Fatal("runtime497 signer did not sign the transaction-version-2 payload", err)
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
			!bytes.Equal(payload[versions:versions+8], []byte{0xf1, 0x01, 0, 0, 2, 0, 0, 0}) || !bytes.Equal(payload[versions+8:], oldPayload[versions+8:]) {
			t.Fatalf("runtime497 payload changes more than its signed versions: %x", payload[versions:versions+8])
		}
		sign(1)
		if _, err := prepared.Validate(); err == nil {
			t.Fatal("a transaction-version-1 signature verified as a runtime497 submission")
		}
		sign(2)
		for _, relabel := range []struct{ spec, transaction uint32 }{{497, 1}, {497, 3}, {498, 2}, {496, 2}, {495, 2}, {494, 2}} {
			changed := *prepared
			source := *prepared.SourceCommitment
			source.RuntimeSpec, source.TransactionVersion = relabel.spec, relabel.transaction
			changed.SourceCommitment = &source
			if _, err := changed.Validate(); err == nil {
				t.Fatalf("runtime497 submission was accepted as %d/%d", relabel.spec, relabel.transaction)
			}
		}
	}
}

// Only the exact reviewed identities' raised transaction version is a fixed
// source encoding; their neighbours stay unsupported before any signature check.
func TestRuntime497SourceEncodingAdmitsOnlyTheReviewedTransactionVersion(t *testing.T) {
	for _, test := range []struct {
		spec, transaction uint32
		ok                bool
	}{{497, 2, true}, {495, 2, true}, {494, 2, true}, {497, 1, true}, {467, 1, true}, {497, 3, false}, {496, 2, false}, {498, 2, false}, {0, 1, false}} {
		prepared, _ := sourcePreparedTest(t)
		prepared.SourceCommitment.RuntimeSpec, prepared.SourceCommitment.TransactionVersion = test.spec, test.transaction
		if _, _, _, err := preparedSourceEncoding(prepared); (err == nil) != test.ok {
			t.Fatalf("source encoding %d/%d: %v", test.spec, test.transaction, err)
		}
	}
}

// The real 473 anchor admits Finney's runtime497, whose only identity change
// besides the spec is the reviewed transaction version 2, as its own exact
// successor; the never-enacted 496, the next spec and identities with a raised
// version are not successors. Before this review the anchor refused 497 as
// "not a successor", which is what held steering on 2026-11-25.
func TestRuntime497IsAnAdmittedSuccessorAtTransactionVersion2(t *testing.T) {
	raw := runtime497TestMetadataBytes(t)
	successor := RuntimeArtifactIdentity{Version: runtime497TestIdentity, CodeHash: runtime497TestCodeHash, MetadataHash: runtime497TestMetadataHash}
	for _, test := range []struct {
		name    string
		version RuntimeVersionIdentity
		want    string
	}{
		{name: "runtime497", version: runtime497TestIdentity},
		{name: "spec496", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 496, TransactionVersion: 2, StateVersion: 1}, want: "not a successor"},
		{name: "spec498", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 498, TransactionVersion: 2, StateVersion: 1}, want: "not a successor"},
		{name: "transaction3", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 497, TransactionVersion: 3, StateVersion: 1}, want: "not a successor"},
		{name: "state2", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 497, TransactionVersion: 2, StateVersion: 2}, want: "not a successor"},
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
				t.Fatal("runtime497 is not an admitted successor of the approved anchor", err)
			}
			approved, ok := artifact.RuntimeSuccessorOf()
			if !ok || approved != runtimeSuccession473 || (RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}) != successor ||
				len(fixture.admitted) != 1 || fixture.admitted[0].Artifact != successor {
				t.Fatalf("runtime497 successor lost its exact identity or anchor: %+v", artifact)
			}
			view := *fixture.chain
			if err := view.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
				t.Fatal("runtime497 successor could not bind the producer signing view", err)
			}
			if err := view.ValidateValidatorProducerRuntimeSuccessor(runtimeSuccession473); err != nil {
				t.Fatal(err)
			}
			if view.Runtime.SpecVersion != 497 || view.Runtime.TransactionVersion != 2 {
				t.Fatalf("runtime497 successor view signs %d/%d", view.Runtime.SpecVersion, view.Runtime.TransactionVersion)
			}
		})
	}
}

// The stake and schedule readers accept the reviewed raised transaction
// version and nothing next to it.
func TestRuntime497StakeReadAdmitsOnlyTheReviewedTransactionVersion(t *testing.T) {
	for _, test := range []struct {
		version RuntimeVersionIdentity
		ok      bool
	}{
		{version: runtime497TestIdentity, ok: true},
		{version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 496, TransactionVersion: 2, StateVersion: 1}},
		{version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 498, TransactionVersion: 2, StateVersion: 1}},
		{version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 497, TransactionVersion: 3, StateVersion: 1}},
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
