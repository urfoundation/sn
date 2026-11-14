// Runtime 481 metadata interfaces and its exact runtime version are independently pinned.
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

const runtime481TestMetadataSize = 364065
const runtime481TestMetadataSha256 = "4dc0d25bc11d1ea15118b433ba34e78f40ae2bfbaa188757e6f67f0a3240266a"
const runtime481TestMetadataHash = "0x40db5b7feac3a8e9ab2ab000234257428826d9827ab326034a3329fdd0d50088"
const runtime481TestCodeHash = "0x5fb504c1c793a3ddb5456f189ddef98b5146853aea6ff998501f1b6c6eb720bc"

// The node's exact state_getRuntimeVersion reply at finalized block 9,320,004.
const runtime481TestVersionReply = `{"specName":"node-subtensor","implName":"node-subtensor","authoringVersion":1,"specVersion":481,"implVersion":1,"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",3],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",1],["0x43580abff6baab45",5],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"transactionVersion":1,"systemVersion":1,"stateVersion":1}`

// Decompression is bounded; size and both digests are checked before decoding.
func runtime481TestMetadata(t *testing.T) (*types.Metadata, string) {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime481-metadata.scale.gz.base64")
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
	raw, err := io.ReadAll(io.LimitReader(reader, runtime481TestMetadataSize+1))
	if err != nil || len(raw) != runtime481TestMetadataSize {
		t.Fatalf("runtime481 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime481 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime481TestMetadataSha256 {
		t.Fatal("runtime481 metadata SHA-256 differs from its review pin")
	}
	wire := fmt.Sprintf("0x%x", raw)
	metadata, hash, err := DecodeRuntimeMetadata(wire)
	if err != nil || hash != runtime481TestMetadataHash {
		t.Fatal("runtime481 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return metadata, wire
}

func TestRuntime481ConsumedMetadataInterfaces(t *testing.T) {
	metadata, _ := runtime481TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime481 changed a CRv4 consumed metadata interface", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatal("runtime481 changed a fleet consumed metadata interface", purpose, err)
		}
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime481TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 481, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatal("runtime481 version reply decodes to another identity", version, err)
	}
}
