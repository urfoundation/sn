// Runtime 484 metadata interfaces and its exact runtime version are independently pinned.
package crv4

import (
	"bytes"
	"compress/gzip"
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

const runtime484TestMetadataSize = 367995
const runtime484TestMetadataSha256 = "6bb2f87cc01db758f9e8d05c8e221a696dd40cd0a7e853e56c5d41895ae3ba71"
const runtime484TestMetadataHash = "0x3cc23f9b95ae9d246e56c2f0d310a80d67e3a3b5bb0357c16182f85b8023a08b"
const runtime484TestCodeHash = "0x23041f50d3529b359e9be3b90f3ca40474678e834e97f49fb401d0c691097760"

// Exact runtime version is pinned in runtime484_api_test.go.

// Decompression is bounded; size and both digests are checked before decoding.
func runtime484TestMetadata(t *testing.T) (*types.Metadata, string) {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime484-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	reader.Multistream(false)
	raw, err := io.ReadAll(io.LimitReader(reader, runtime484TestMetadataSize+1))
	if err != nil || len(raw) != runtime484TestMetadataSize {
		t.Fatalf("runtime484 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime484 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime484TestMetadataSha256 {
		t.Fatal("runtime484 metadata SHA-256 differs from its review pin")
	}
	wire := fmt.Sprintf("0x%x", raw)
	metadata, hash, err := DecodeRuntimeMetadata(wire)
	if err != nil || hash != runtime484TestMetadataHash {
		t.Fatal("runtime484 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return metadata, wire
}

// Existing consumed-interface checks also admit the preserved runtime metadata.
func TestRuntime484ConsumedMetadataInterfaces(t *testing.T) {
	metadata, _ := runtime484TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime484 changed a CRv4 consumed metadata interface", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatal("runtime484 changed a fleet consumed metadata interface", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime484TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 484, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatal("runtime484 version reply decodes to another identity", version, err)
	}
}
