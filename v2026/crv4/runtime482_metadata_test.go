// Runtime 482 metadata interfaces and its exact runtime version are independently pinned.
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

const runtime482TestMetadataSize = 367676
const runtime482TestMetadataSha256 = "937fc71c0783462556cc26fab9da9d771e0c96b657b1cd749fb0d4e4b28d9281"
const runtime482TestMetadataHash = "0x512a7c0c9d87763428a4c927670f7a01f014ab9b1166d0709cfb08cef280be43"
const runtime482TestCodeHash = "0x37d83888085e2f1b7c9c36c1341cfb6c0d6689f8efd6e0c6128e3666eb5d8b62"

// Exact runtime version is pinned in runtime482_api_test.go.

// Decompression is bounded; size and both digests are checked before decoding.
func runtime482TestMetadata(t *testing.T) (*types.Metadata, string) {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime482-metadata.scale.gz.base64")
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
	raw, err := io.ReadAll(io.LimitReader(reader, runtime482TestMetadataSize+1))
	if err != nil || len(raw) != runtime482TestMetadataSize {
		t.Fatalf("runtime482 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime482 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime482TestMetadataSha256 {
		t.Fatal("runtime482 metadata SHA-256 differs from its review pin")
	}
	wire := fmt.Sprintf("0x%x", raw)
	metadata, hash, err := DecodeRuntimeMetadata(wire)
	if err != nil || hash != runtime482TestMetadataHash {
		t.Fatal("runtime482 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return metadata, wire
}

func TestRuntime482ConsumedMetadataInterfaces(t *testing.T) {
	metadata, _ := runtime482TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime482 changed a CRv4 consumed metadata interface", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatal("runtime482 changed a fleet consumed metadata interface", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime482TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 482, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatal("runtime482 version reply decodes to another identity", version, err)
	}
}
