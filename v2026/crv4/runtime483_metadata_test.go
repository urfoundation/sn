// Runtime 483 metadata interfaces and its exact runtime version are independently pinned.
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

const runtime483TestMetadataSize = 367676
const runtime483TestMetadataSha256 = "37eef25143aaf16971597fbcc29164f8a74343d5743454fbd8b99f5d1ee78262"
const runtime483TestMetadataHash = "0xcdb8b463eb27b3157640e949fe06a242ae2f80a050f91bcd43d6b55932dda923"
const runtime483TestCodeHash = "0x99856ec64a61eb8011999a803bd336979512275214a173fea583abd5c89952d2"

// Exact runtime version is pinned in runtime483_api_test.go.

// Decompression is bounded; size and both digests are checked before decoding.
func runtime483TestMetadata(t *testing.T) (*types.Metadata, string) {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime483-metadata.scale.gz.base64")
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
	raw, err := io.ReadAll(io.LimitReader(reader, runtime483TestMetadataSize+1))
	if err != nil || len(raw) != runtime483TestMetadataSize {
		t.Fatalf("runtime483 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime483 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime483TestMetadataSha256 {
		t.Fatal("runtime483 metadata SHA-256 differs from its review pin")
	}
	wire := fmt.Sprintf("0x%x", raw)
	metadata, hash, err := DecodeRuntimeMetadata(wire)
	if err != nil || hash != runtime483TestMetadataHash {
		t.Fatal("runtime483 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return metadata, wire
}

func TestRuntime483ConsumedMetadataInterfaces(t *testing.T) {
	metadata, _ := runtime483TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime483 changed a CRv4 consumed metadata interface", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatal("runtime483 changed a fleet consumed metadata interface", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime483TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 483, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatal("runtime483 version reply decodes to another identity", version, err)
	}
}
