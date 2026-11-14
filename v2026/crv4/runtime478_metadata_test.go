// The exact runtime-478 metadata, captured read-only from Finney, is admitted
// by the consumed-interface profiles. A read-only transcript replays the exact
// runtime version reply; no network, approval, seed or signing key is involved.
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
	"reflect"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const runtime478TestMetadataSize = 357499
const runtime478TestMetadataSha256 = "2513780705c9e9c1b9fb7e701670fee88cf8419b945100fc26ab87218b03e35a"
const runtime478TestMetadataHash = "0x4f96a9cb954ebe3ae0e364fe7e34ec8a0e902239832f68dfe90aaa1e4511c747"
const runtime478TestCodeHash = "0x7e722b3aa5c9192c98665a5d65fa91364bf9055e21fc90efce6d59bf4ef414e1"

// The node's exact state_getRuntimeVersion reply at finalized block 9,277,117.
const runtime478TestVersionReply = `{"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",2],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",1],["0x43580abff6baab45",5],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"authoringVersion":1,"implName":"node-subtensor","implVersion":1,"specName":"node-subtensor","specVersion":478,"stateVersion":1,"systemVersion":1,"transactionVersion":1}`

// Decompression is bounded; size and both digests are checked before decoding.
func runtime478TestMetadata(t *testing.T) (*types.Metadata, string) {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime478-metadata.scale.gz.base64")
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
	raw, err := io.ReadAll(io.LimitReader(reader, runtime478TestMetadataSize+1))
	if err != nil || len(raw) != runtime478TestMetadataSize {
		t.Fatalf("runtime478 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime478 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime478TestMetadataSha256 {
		t.Fatal("runtime478 metadata SHA-256 differs from its review pin")
	}
	wire := fmt.Sprintf("0x%x", raw)
	metadata, hash, err := DecodeRuntimeMetadata(wire)
	if err != nil || hash != runtime478TestMetadataHash {
		t.Fatal("runtime478 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return metadata, wire
}

// Producer, provisional and fleet profiles compare 478 with the reviewed 467
// baseline. Only the interfaces each operation consumes participate.
func TestRuntime478ConsumedInterfacesMatchReviewedBaseline(t *testing.T) {
	metadata, _ := runtime478TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime478 changed a CRv4 consumed interface", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatal("runtime478 changed a fleet consumed interface", purpose, err)
		}
	}
	if err := validateProvisionalRuntimeApis(json.RawMessage(runtime478TestVersionReply)); err != nil {
		t.Fatal("runtime478 SubnetInfoRuntimeApi declaration changed", err)
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime478TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 478, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatal("runtime478 version reply decodes to another identity", version, err)
	}
}

// The exact pinned artifact binds the production producer purpose on a
// read-only transcript; a one-byte metadata change cannot reuse the pin.
func TestRuntime478ValidatorProducerCapability(t *testing.T) {
	metadata, wire := runtime478TestMetadata(t)
	block := types.Hash{0x78}
	identity := RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 478, TransactionVersion: 1, StateVersion: 1}, CodeHash: runtime478TestCodeHash, MetadataHash: runtime478TestMetadataHash}
	served := wire
	chain := &Chain{GenesisHash: types.Hash{8}}
	chain.API = &gsrpc.SubstrateAPI{Client: &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		expected := []any{block.Hex()}
		if method == "state_getStorageHash" {
			expected = []any{"0x3a636f6465", block.Hex()}
		}
		if !reflect.DeepEqual(args, expected) {
			return fmt.Errorf("runtime478 Rpc %s is not exact-block: %v", method, args)
		}
		switch method {
		case "state_getRuntimeVersion":
			*result.(*json.RawMessage) = json.RawMessage(runtime478TestVersionReply)
			return nil
		case "state_getStorageHash":
			return setRuntimeIdentityTestResult(result, identity.CodeHash)
		case "state_getMetadata":
			return setRuntimeIdentityTestResult(result, served)
		default:
			return fmt.Errorf("unexpected or mutating runtime478 Rpc %s", method)
		}
	}}}
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), chain, block, identity)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Version != identity.Version || artifact.MetadataHash != runtime478TestMetadataHash || !reflect.DeepEqual(artifact.Metadata.AsMetadataV14.Pallets, metadata.AsMetadataV14.Pallets) {
		t.Fatal("authenticated runtime478 artifact differs from its pins")
	}
	if err := chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime478 refused the producer capability", err)
	}
	if err := chain.ValidateValidatorProducerRuntime(identity); err != nil {
		t.Fatal(err)
	}
	served = wire[:len(wire)-2] + "00"
	if served == wire {
		served = wire[:len(wire)-2] + "01"
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), &Chain{GenesisHash: chain.GenesisHash, API: chain.API}, block, identity); err == nil {
		t.Fatal("changed metadata bytes reused the runtime478 pin")
	}
}
