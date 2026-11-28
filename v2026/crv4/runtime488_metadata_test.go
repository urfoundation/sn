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
const runtime488TestMetadataSize = 392151
const runtime488TestMetadataSha256 = "66804d7b7f326f3285f900a1533df30c17ffa1d2855a5c3f3559ae05471446b7"
const runtime488TestMetadataHash = "0x9ae276aa9166d8d4c72deb8d14e802df2171df3c3232198bbff65d5fe205e0ca"

// One bounded gzip member must match both independent content digests.
func runtime488TestMetadata(t *testing.T) *types.Metadata {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime488-metadata.scale.gz.base64")
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
	raw, readErr := io.ReadAll(io.LimitReader(reader, runtime488TestMetadataSize+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) != runtime488TestMetadataSize || input.Len() != 0 {
		t.Fatalf("bounded runtime488 metadata member differs: read=%v close=%v bytes=%d trailing=%d", readErr, closeErr, len(raw), input.Len())
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime488TestMetadataSha256 {
		t.Fatal("runtime488 fixture SHA256 differs")
	}
	metadata, hash, err := DecodeRuntimeMetadata(fmt.Sprintf("0x%x", raw))
	if err != nil || hash != runtime488TestMetadataHash {
		t.Fatalf("runtime488 normal metadata decoder/pin: %v %s", err, hash)
	}
	return metadata
}

// All consumed producer and fleet shapes use their existing production checks.
func TestRuntime488ConsumedMetadataInterfaces(t *testing.T) {
	metadata := runtime488TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime488 CRv4 interface differs", err)
	}
	if err := validateValidatorProducerMetadata(metadata); err != nil {
		t.Fatal("runtime488 producer interface differs", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatalf("runtime488 fleet purpose %v differs: %v", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime488TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 488, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatalf("runtime488 exact protocol tuple differs: %v %+v", err, version)
	}
}

// Exact synthetic artifact authentication reaches the public staging capability
// check with the captured metadata and API3 declaration before signing can bind.
func TestRuntime488ValidatorProducerCapability(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.metadata = runtime488TestMetadata(t)
	fixture.identity.Version = RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 488, TransactionVersion: 1, StateVersion: 1}
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := original(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			*result.(*json.RawMessage) = json.RawMessage(runtime488TestVersionReply)
		}
		return ctx.Err()
	}
	artifact := fixture.bind(t)
	if artifact.MetadataHash != runtime488TestMetadataHash {
		t.Fatal("exact authenticated runtime488 metadata differs from the captured schema")
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("read-only artifact authentication granted producer signing capability")
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal("runtime488 staging capability refused the exact authenticated artifact", err)
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime488 producer capability could not bind", err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatal("runtime488 signing view lost its exact purpose proof", err)
	}
}
