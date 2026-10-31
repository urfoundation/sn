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

// Fixture digests and byte count name immutable protocol evidence.
const runtime486TestMetadataSize = 367995
const runtime486TestMetadataSha256 = "142e2f2735d9585567fd71e080554a2a0ec10d4ad0bcedb4993e87e473ddd852"
const runtime486TestMetadataHash = "0x0c83083856fbfe771d449c5534001d91e1ce2c3032cc9ccb93c1ea356bc116af"

// One bounded gzip member must match both independent content digests.
func runtime486TestMetadata(t *testing.T) *types.Metadata {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime486-metadata.scale.gz.base64")
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
	raw, readErr := io.ReadAll(io.LimitReader(reader, runtime486TestMetadataSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) != runtime486TestMetadataSize || input.Len() != 0 {
		t.Fatalf("bounded runtime486 metadata member differs: read=%v close=%v bytes=%d trailing=%d", readErr, closeErr, len(raw), input.Len())
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime486TestMetadataSha256 {
		t.Fatal("runtime486 fixture SHA256 differs")
	}
	metadata, hash, err := DecodeRuntimeMetadata(fmt.Sprintf("0x%x", raw))
	if err != nil || hash != runtime486TestMetadataHash {
		t.Fatalf("runtime486 normal metadata decoder/pin: %v %s", err, hash)
	}
	return metadata
}

// All consumed producer and fleet shapes use their existing production checks.
func TestRuntime486ConsumedMetadataInterfaces(t *testing.T) {
	metadata := runtime486TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime486 CRv4 interface differs", err)
	}
	if err := validateValidatorProducerMetadata(metadata); err != nil {
		t.Fatal("runtime486 producer interface differs", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatalf("runtime486 fleet purpose %v differs: %v", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime486TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 486, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatalf("runtime486 exact protocol tuple differs: %v %+v", err, version)
	}
}

// Exact synthetic artifact authentication reaches the public staging capability
// check with the captured metadata and API3 declaration before signing can bind.
func TestRuntime486ValidatorProducerCapability(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.metadata = runtime486TestMetadata(t)
	fixture.identity.Version = RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 486, TransactionVersion: 1, StateVersion: 1}
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := original(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			*result.(*json.RawMessage) = json.RawMessage(runtime486TestVersionReply)
		}
		return ctx.Err()
	}
	artifact := fixture.bind(t)
	if artifact.MetadataHash != runtime486TestMetadataHash {
		t.Fatal("exact authenticated runtime486 metadata differs from the captured schema")
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("read-only artifact authentication granted producer signing capability")
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal("runtime486 staging capability refused the exact authenticated artifact", err)
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime486 producer capability could not bind", err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatal("runtime486 signing view lost its exact purpose proof", err)
	}
}
