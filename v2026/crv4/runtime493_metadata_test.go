// Exact public schema evidence exercises normal consumed-interface decoders;
// it does not authenticate deployed Wasm source or grant runtime authority.
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
)

// Fixture digests and byte count name immutable protocol evidence: Finney's
// state_getMetadata at block 9549412, the first runtime493 block.
const runtime493TestMetadataSize = 395608
const runtime493TestMetadataSha256 = "4ed0c6997e668a995c9dc4ea4c8d587d1625c193fc3361a7781e49156a8db9df"
const runtime493TestMetadataHash = "0x25137f5e9ee03a4fbf1d6eade4bafb8e65a72a1f94a68526d495e3c944c1ed04"

// One bounded gzip member must match both independent content digests.
func runtime493TestMetadata(t *testing.T) *types.Metadata {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime493-metadata.scale.gz.base64")
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
	raw, readErr := io.ReadAll(io.LimitReader(reader, runtime493TestMetadataSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) != runtime493TestMetadataSize || input.Len() != 0 {
		t.Fatalf("bounded runtime493 metadata member differs: read=%v close=%v bytes=%d trailing=%d", readErr, closeErr, len(raw), input.Len())
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime493TestMetadataSha256 {
		t.Fatal("runtime493 fixture SHA256 differs")
	}
	metadata, hash, err := DecodeRuntimeMetadata(fmt.Sprintf("0x%x", raw))
	if err != nil || hash != runtime493TestMetadataHash {
		t.Fatalf("runtime493 normal metadata decoder/pin: %v %s", err, hash)
	}
	return metadata
}

// All consumed producer and fleet shapes use their existing production checks.
func TestRuntime493ConsumedMetadataInterfaces(t *testing.T) {
	metadata := runtime493TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime493 CRv4 interface differs", err)
	}
	if err := validateValidatorProducerMetadata(metadata); err != nil {
		t.Fatal("runtime493 producer interface differs", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatalf("runtime493 fleet purpose %v differs: %v", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime493TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 493, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatalf("runtime493 exact protocol tuple differs: %v %+v", err, version)
	}
}

// Exact synthetic artifact authentication reaches the public staging capability
// check with the captured metadata and API3 declaration before signing can bind.
func TestRuntime493ValidatorProducerCapability(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.metadata = runtime493TestMetadata(t)
	fixture.identity.Version = RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 493, TransactionVersion: 1, StateVersion: 1}
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := original(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			*result.(*json.RawMessage) = json.RawMessage(runtime493TestVersionReply)
		}
		return ctx.Err()
	}
	artifact := fixture.bind(t)
	if artifact.MetadataHash != runtime493TestMetadataHash {
		t.Fatal("exact authenticated runtime493 metadata differs from the captured schema")
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("read-only artifact authentication granted producer signing capability")
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal("runtime493 staging capability refused the exact authenticated artifact", err)
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime493 producer capability could not bind", err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatal("runtime493 signing view lost its exact purpose proof", err)
	}
}
