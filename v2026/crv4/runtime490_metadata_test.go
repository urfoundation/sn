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
// state_getMetadata at block 9478217, the first runtime490 block.
const runtime490TestMetadataSize = 395984
const runtime490TestMetadataSha256 = "6c1f9314e97e88d9ec4ffbdc1e2f7335e86c2b41bf27e726bef5bd3fa5305401"
const runtime490TestMetadataHash = "0x58fd4d5d2a68ee7d6491c98979127b3ecedd54c7fbc0fd6a04794bade14458d3"

// One bounded gzip member must match both independent content digests.
func runtime490TestMetadata(t *testing.T) *types.Metadata {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime490-metadata.scale.gz.base64")
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
	raw, readErr := io.ReadAll(io.LimitReader(reader, runtime490TestMetadataSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) != runtime490TestMetadataSize || input.Len() != 0 {
		t.Fatalf("bounded runtime490 metadata member differs: read=%v close=%v bytes=%d trailing=%d", readErr, closeErr, len(raw), input.Len())
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime490TestMetadataSha256 {
		t.Fatal("runtime490 fixture SHA256 differs")
	}
	metadata, hash, err := DecodeRuntimeMetadata(fmt.Sprintf("0x%x", raw))
	if err != nil || hash != runtime490TestMetadataHash {
		t.Fatalf("runtime490 normal metadata decoder/pin: %v %s", err, hash)
	}
	return metadata
}

// All consumed producer and fleet shapes use their existing production checks.
func TestRuntime490ConsumedMetadataInterfaces(t *testing.T) {
	metadata := runtime490TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime490 CRv4 interface differs", err)
	}
	if err := validateValidatorProducerMetadata(metadata); err != nil {
		t.Fatal("runtime490 producer interface differs", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatalf("runtime490 fleet purpose %v differs: %v", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime490TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 490, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatalf("runtime490 exact protocol tuple differs: %v %+v", err, version)
	}
}

// Exact synthetic artifact authentication reaches the public staging capability
// check with the captured metadata and API3 declaration before signing can bind.
func TestRuntime490ValidatorProducerCapability(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.metadata = runtime490TestMetadata(t)
	fixture.identity.Version = RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 490, TransactionVersion: 1, StateVersion: 1}
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := original(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			*result.(*json.RawMessage) = json.RawMessage(runtime490TestVersionReply)
		}
		return ctx.Err()
	}
	artifact := fixture.bind(t)
	if artifact.MetadataHash != runtime490TestMetadataHash {
		t.Fatal("exact authenticated runtime490 metadata differs from the captured schema")
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("read-only artifact authentication granted producer signing capability")
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal("runtime490 staging capability refused the exact authenticated artifact", err)
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime490 producer capability could not bind", err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatal("runtime490 signing view lost its exact purpose proof", err)
	}
}
