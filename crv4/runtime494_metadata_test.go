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
// state_getMetadata at block 9570596, the first runtime494 block.
const runtime494TestMetadataSize = 406936
const runtime494TestMetadataSha256 = "a2f8737e3b8acdf7bffbd764c587c5696e7bec9b2453be8d69bcc990306bf093"
const runtime494TestMetadataHash = "0x5378d4cbeebda6a70798f94f6c7ba6a10551bb54ff3506d96902e8b0cac12d3a"

var runtime494TestIdentity = RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 2, StateVersion: 1}

// One bounded gzip member must match both independent content digests.
func runtime494TestMetadataBytes(t *testing.T) []byte {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime494-metadata.scale.gz.base64")
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
	raw, readErr := io.ReadAll(io.LimitReader(reader, runtime494TestMetadataSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) != runtime494TestMetadataSize || input.Len() != 0 {
		t.Fatalf("bounded runtime494 metadata member differs: read=%v close=%v bytes=%d trailing=%d", readErr, closeErr, len(raw), input.Len())
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime494TestMetadataSha256 {
		t.Fatal("runtime494 fixture SHA256 differs")
	}
	return raw
}

func runtime494TestMetadata(t *testing.T) *types.Metadata {
	t.Helper()
	metadata, hash, err := DecodeRuntimeMetadata(fmt.Sprintf("0x%x", runtime494TestMetadataBytes(t)))
	if err != nil || hash != runtime494TestMetadataHash {
		t.Fatalf("runtime494 normal metadata decoder/pin: %v %s", err, hash)
	}
	return metadata
}

// All consumed producer and fleet shapes use their existing production checks,
// signed extensions included.
func TestRuntime494ConsumedMetadataInterfaces(t *testing.T) {
	metadata := runtime494TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime494 CRv4 interface differs", err)
	}
	if err := validateValidatorProducerMetadata(metadata); err != nil {
		t.Fatal("runtime494 producer interface differs", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatalf("runtime494 fleet purpose %v differs: %v", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime494TestVersionReply))
	if err != nil || version != runtime494TestIdentity {
		t.Fatalf("runtime494 exact protocol tuple differs: %v %+v", err, version)
	}
}

// Binds a synthetic exact authentication of the captured 494 schema and API
// declaration, at the transaction version Finney declares.
func runtime494TestSourceFixture(t *testing.T) (*sourceRuntimeCapabilityTestFixture, AuthenticatedRuntimeArtifact) {
	t.Helper()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.metadata = runtime494TestMetadata(t)
	fixture.identity.Version = runtime494TestIdentity
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := original(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			*result.(*json.RawMessage) = json.RawMessage(runtime494TestVersionReply)
		}
		return ctx.Err()
	}
	artifact := fixture.bind(t)
	if artifact.MetadataHash != runtime494TestMetadataHash || artifact.Version != runtime494TestIdentity {
		t.Fatal("exact authenticated runtime494 artifact differs from the captured schema")
	}
	return fixture, artifact
}

// Exact synthetic artifact authentication reaches the public staging capability
// check with the captured metadata and API3 declaration before signing can
// bind, and the bound signing view carries transaction version 2.
func TestRuntime494ValidatorProducerCapability(t *testing.T) {
	fixture, artifact := runtime494TestSourceFixture(t)
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("read-only artifact authentication granted producer signing capability")
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal("runtime494 staging capability refused the exact authenticated artifact", err)
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime494 producer capability could not bind", err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatal("runtime494 signing view lost its exact purpose proof", err)
	}
	if fixture.chain.Runtime.SpecVersion != 494 || fixture.chain.Runtime.TransactionVersion != 2 {
		t.Fatalf("runtime494 signing view binds %d/%d, not the authenticated 494/2", fixture.chain.Runtime.SpecVersion, fixture.chain.Runtime.TransactionVersion)
	}
}

// The production source write, end to end on the captured 494 schema: the live
// metadata builds the atomic batch, the real signer signs it with the bound
// runtime's versions, and the independent fixed encoding (the bytes the chain
// verifies: call, extra, then spec, transaction version, genesis, era birth and
// the absent metadata hash) reproduces the payload and verifies the signature.
// A signature made at transaction version 1 does not verify for runtime494.
func TestRuntime494SourceSubmissionSignsTransactionVersion2(t *testing.T) {
	for _, mecid := range []*uint8{nil, new(uint8)} {
		fixture, _ := runtime494TestSourceFixture(t)
		prepared, key := sourcePreparedTest(t)
		prepared.Mecid = mecid
		prepared.SourceCommitment.RuntimeSpec, prepared.SourceCommitment.TransactionVersion = 494, 2
		prepared.SourceCommitment.GenesisHash = fixture.chain.GenesisHash.Hex()
		ciphertext, err := codec.HexDecodeString(prepared.CiphertextHex)
		if err != nil {
			t.Fatal(err)
		}
		call, err := fixture.chain.newSourceCommitmentBatchCall(prepared.Netuid, mecid, types.Hash{9}, ciphertext, prepared.RevealRound, prepared.CommitRevealVersion)
		if err != nil {
			t.Fatal("runtime494 source capability refused the exact authenticated view", err)
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
			t.Fatal("runtime494 metadata changes the fixed source encoding", err)
		}
		if _, err := prepared.Validate(); err != nil {
			t.Fatal("runtime494 signer did not sign the transaction-version-2 payload", err)
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
			!bytes.Equal(payload[versions:versions+8], []byte{0xee, 0x01, 0, 0, 2, 0, 0, 0}) || !bytes.Equal(payload[versions+8:], oldPayload[versions+8:]) {
			t.Fatalf("runtime494 payload changes more than its signed versions: %x", payload[versions:versions+8])
		}
		sign(1)
		if _, err := prepared.Validate(); err == nil {
			t.Fatal("a transaction-version-1 signature verified as a runtime494 submission")
		}
		sign(2)
		for _, relabel := range []struct{ spec, transaction uint32 }{{494, 1}, {494, 3}, {495, 2}, {493, 2}} {
			changed := *prepared
			source := *prepared.SourceCommitment
			source.RuntimeSpec, source.TransactionVersion = relabel.spec, relabel.transaction
			changed.SourceCommitment = &source
			if _, err := changed.Validate(); err == nil {
				t.Fatalf("runtime494 submission was accepted as %d/%d", relabel.spec, relabel.transaction)
			}
		}
	}
}

// Only the exact reviewed identity's raised transaction version is a fixed
// source encoding; its neighbours stay unsupported before any signature check.
func TestRuntime494SourceEncodingAdmitsOnlyTheReviewedTransactionVersion(t *testing.T) {
	for _, test := range []struct {
		spec, transaction uint32
		ok                bool
	}{{494, 2, true}, {494, 1, true}, {467, 1, true}, {494, 3, false}, {495, 2, false}, {493, 2, false}, {0, 1, false}} {
		prepared, _ := sourcePreparedTest(t)
		prepared.SourceCommitment.RuntimeSpec, prepared.SourceCommitment.TransactionVersion = test.spec, test.transaction
		if _, _, _, err := preparedSourceEncoding(prepared); (err == nil) != test.ok {
			t.Fatalf("source encoding %d/%d: %v", test.spec, test.transaction, err)
		}
	}
}

// The real 473 anchor admits Finney's runtime494, whose only identity change
// besides the spec is the reviewed transaction version 2, as its own exact
// successor; a neighbouring identity with a raised version is not a successor.
func TestRuntime494IsAnAdmittedSuccessorAtTransactionVersion2(t *testing.T) {
	raw := runtime494TestMetadataBytes(t)
	successor := RuntimeArtifactIdentity{Version: runtime494TestIdentity, CodeHash: "0x1735bc231789bead3ab3edfa583f3c9a95aaf7643adcc906ea224a7761a303ca", MetadataHash: runtime494TestMetadataHash}
	for _, test := range []struct {
		name    string
		version RuntimeVersionIdentity
		want    string
	}{
		{name: "runtime494", version: runtime494TestIdentity},
		{name: "spec495", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 2, StateVersion: 1}, want: "not a successor"},
		{name: "transaction3", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 3, StateVersion: 1}, want: "not a successor"},
		{name: "state2", version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 2, StateVersion: 2}, want: "not a successor"},
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
				t.Fatal("runtime494 is not an admitted successor of the approved anchor", err)
			}
			approved, ok := artifact.RuntimeSuccessorOf()
			if !ok || approved != runtimeSuccession473 || (RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}) != successor ||
				len(fixture.admitted) != 1 || fixture.admitted[0].Artifact != successor {
				t.Fatalf("runtime494 successor lost its exact identity or anchor: %+v", artifact)
			}
			view := *fixture.chain
			if err := view.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
				t.Fatal("runtime494 successor could not bind the producer signing view", err)
			}
			if err := view.ValidateValidatorProducerRuntimeSuccessor(runtimeSuccession473); err != nil {
				t.Fatal(err)
			}
			if view.Runtime.SpecVersion != 494 || view.Runtime.TransactionVersion != 2 {
				t.Fatalf("runtime494 successor view signs %d/%d", view.Runtime.SpecVersion, view.Runtime.TransactionVersion)
			}
		})
	}
}

// The stake and schedule readers accept the reviewed raised transaction
// version and nothing next to it.
func TestRuntime494StakeReadAdmitsOnlyTheReviewedTransactionVersion(t *testing.T) {
	for _, test := range []struct {
		version RuntimeVersionIdentity
		ok      bool
	}{
		{version: runtime494TestIdentity, ok: true},
		{version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 2, StateVersion: 1}},
		{version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 3, StateVersion: 1}},
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
