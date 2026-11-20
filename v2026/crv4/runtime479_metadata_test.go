// The exact runtime-479 metadata, captured read-only from Finney, is admitted
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

const runtime479TestMetadataSize = 359018
const runtime479TestMetadataSha256 = "075ee9614fee27fb83a44bda737f6803bd3f6642d57ab492321aa47dad359080"
const runtime479TestMetadataHash = "0x3ef17424a54b53d3630c2fde92ebff918364a2f4730c9c23e22ebdd7c21575d1"
const runtime479TestCodeHash = "0x44e952fd1cebe9bd15c0c55cee6e1d7c6c97b44d941b79127343118dbbd342bc"

// The node's exact state_getRuntimeVersion reply at finalized block 9,291,312.
const runtime479TestVersionReply = `{"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",2],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",1],["0x43580abff6baab45",5],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"authoringVersion":1,"implName":"node-subtensor","implVersion":1,"specName":"node-subtensor","specVersion":479,"stateVersion":1,"systemVersion":1,"transactionVersion":1}`

// Decompression is bounded; size and both digests are checked before decoding.
func runtime479TestMetadata(t *testing.T) (*types.Metadata, string) {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime479-metadata.scale.gz.base64")
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
	raw, err := io.ReadAll(io.LimitReader(reader, runtime479TestMetadataSize+1))
	if err != nil || len(raw) != runtime479TestMetadataSize {
		t.Fatalf("runtime479 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime479 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime479TestMetadataSha256 {
		t.Fatal("runtime479 metadata SHA-256 differs from its review pin")
	}
	wire := fmt.Sprintf("0x%x", raw)
	metadata, hash, err := DecodeRuntimeMetadata(wire)
	if err != nil || hash != runtime479TestMetadataHash {
		t.Fatal("runtime479 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return metadata, wire
}

// Producer, provisional and fleet profiles compare 479 with the reviewed 467
// baseline. Only the interfaces each operation consumes participate.
func TestRuntime479ConsumedInterfacesMatchReviewedBaseline(t *testing.T) {
	metadata, _ := runtime479TestMetadata(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal("runtime479 changed a CRv4 consumed interface", err)
	}
	for _, purpose := range []FleetRuntimePurpose{FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite} {
		if err := ValidateFleetRuntimeMetadata(metadata, purpose); err != nil {
			t.Fatal("runtime479 changed a fleet consumed interface", purpose, err)
		}
	}
	if err := validateProvisionalRuntimeApis(json.RawMessage(runtime479TestVersionReply)); err != nil {
		t.Fatal("runtime479 SubnetInfoRuntimeApi declaration changed", err)
	}
	version, err := DecodeRuntimeVersionIdentity(json.RawMessage(runtime479TestVersionReply))
	if err != nil || version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 479, TransactionVersion: 1, StateVersion: 1}) {
		t.Fatal("runtime479 version reply decodes to another identity", version, err)
	}
}

// The exact pinned artifact binds the production producer purpose on a
// read-only transcript; a one-byte metadata change cannot reuse the pin.
func TestRuntime479ValidatorProducerCapability(t *testing.T) {
	metadata, wire := runtime479TestMetadata(t)
	block := types.Hash{0x79}
	identity := RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 479, TransactionVersion: 1, StateVersion: 1}, CodeHash: runtime479TestCodeHash, MetadataHash: runtime479TestMetadataHash}
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
			return fmt.Errorf("runtime479 Rpc %s is not exact-block: %v", method, args)
		}
		switch method {
		case "state_getRuntimeVersion":
			*result.(*json.RawMessage) = json.RawMessage(runtime479TestVersionReply)
			return nil
		case "state_getStorageHash":
			return setRuntimeIdentityTestResult(result, identity.CodeHash)
		case "state_getMetadata":
			return setRuntimeIdentityTestResult(result, served)
		default:
			return fmt.Errorf("unexpected or mutating runtime479 Rpc %s", method)
		}
	}}}
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), chain, block, identity)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Version != identity.Version || artifact.MetadataHash != runtime479TestMetadataHash || !reflect.DeepEqual(artifact.Metadata.AsMetadataV14.Pallets, metadata.AsMetadataV14.Pallets) {
		t.Fatal("authenticated runtime479 artifact differs from its pins")
	}
	if err := chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal("runtime479 refused the producer capability", err)
	}
	if err := chain.ValidateValidatorProducerRuntime(identity); err != nil {
		t.Fatal(err)
	}
	served = wire[:len(wire)-2] + "00"
	if served == wire {
		served = wire[:len(wire)-2] + "01"
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), &Chain{GenesisHash: chain.GenesisHash, API: chain.API}, block, identity); err == nil {
		t.Fatal("changed metadata bytes reused the runtime479 pin")
	}
}

// The producer successor policy that production validators and the api opt
// into admits the real 479 upgrade from either approved anchor the launch has
// used, 475 and 478: every producer-consumed interface, the selective-metagraph
// API and the signing encoding versions are kept. The policy's own refusals are
// covered by the runtime succession tests.
func TestRuntime479IsAnAdmittedProducerSuccessor(t *testing.T) {
	_, wire := runtime479TestMetadata(t)
	runtime479 := RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 479, TransactionVersion: 1, StateVersion: 1}, CodeHash: runtime479TestCodeHash, MetadataHash: runtime479TestMetadataHash}
	runtime478 := RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 478, TransactionVersion: 1, StateVersion: 1}, CodeHash: runtime478TestCodeHash, MetadataHash: runtime478TestMetadataHash}
	block := types.Hash{0x79, 0x01}
	for _, anchor := range []RuntimeArtifactIdentity{runtimeSuccession475, runtime478} {
		chain := &Chain{GenesisHash: runtimeSuccessionTestGenesis}
		chain.API = &gsrpc.SubstrateAPI{Client: &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(args) == 0 || args[len(args)-1] != block.Hex() {
				return fmt.Errorf("runtime479 successor Rpc %s is not exact-block: %v", method, args)
			}
			switch method {
			case "state_getRuntimeVersion":
				return setRuntimeIdentityTestResult(result, json.RawMessage(runtime479TestVersionReply))
			case "state_getStorageHash":
				if len(args) != 2 || args[0] != "0x3a636f6465" {
					return fmt.Errorf("unexpected code-hash query %v", args)
				}
				return setRuntimeIdentityTestResult(result, runtime479.CodeHash)
			case "state_getMetadata":
				return setRuntimeIdentityTestResult(result, wire)
			default:
				return fmt.Errorf("unexpected or mutating runtime479 successor Rpc %s", method)
			}
		}}}
		var admittedSuccessors []RuntimeSuccessor
		if err := chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ValidatorProducerRuntimeProfile, []RuntimeArtifactIdentity{anchor}, func(successor RuntimeSuccessor) error {
			admittedSuccessors = append(admittedSuccessors, successor)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		successor, err := AuthenticateRuntimeArtifactAtContext(t.Context(), chain, block, anchor)
		if err != nil {
			t.Fatal("runtime479 refused as a successor of", anchor.Version.SpecVersion, err)
		}
		approved, ok := successor.RuntimeSuccessorOf()
		if !ok || approved != anchor || (RuntimeArtifactIdentity{Version: successor.Version, CodeHash: successor.CodeHash, MetadataHash: successor.MetadataHash}) != runtime479 ||
			len(admittedSuccessors) != 1 || admittedSuccessors[0].Artifact != runtime479 || admittedSuccessors[0].Approved != anchor {
			t.Fatalf("runtime479 successor lost its exact identity or anchor: %+v %+v", successor, admittedSuccessors)
		}
		if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), chain, successor); err != nil {
			t.Fatal(err)
		}
		view := *chain
		if err := view.BindValidatorProducerRuntimeArtifactContext(t.Context(), successor); err != nil {
			t.Fatal(err)
		}
		if uint32(view.Runtime.SpecVersion) != 479 || view.Meta != successor.Metadata {
			t.Fatal("signing view did not bind the runtime479 successor")
		}
		if err := view.ValidateValidatorProducerRuntimeSuccessor(anchor); err != nil {
			t.Fatal(err)
		}
		if err := view.ValidateValidatorProducerRuntime(anchor); err == nil {
			t.Fatal("runtime479 successor proof satisfied an exact approved pin")
		}
	}
}
