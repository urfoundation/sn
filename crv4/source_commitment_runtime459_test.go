// Runtime459 uses the exact reviewed metadata with synthetic accounts and heads.
package crv4

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// Bounds and authenticates the actual459 metadata before any call construction.
func sourceRuntime459TestChain(t *testing.T) *Chain {
	t.Helper()
	encoded, err := os.ReadFile("../miner/testdata/runtime459-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(input)
	if err != nil {
		t.Fatal(err)
	}
	reader.Multistream(false)
	raw, err := io.ReadAll(io.LimitReader(reader, 336359))
	closeErr := reader.Close()
	sha, blake := sha256.Sum256(raw), blake2b.Sum256(raw)
	if err != nil || closeErr != nil || input.Len() != 0 || len(raw) != 336358 ||
		hex.EncodeToString(sha[:]) != "52256b0b4a5c682e94e1d68a7b7a5dc1ba4114cfde4fb39057be808a8443673d" ||
		hex.EncodeToString(blake[:]) != "cf97fac54fee756137f42e53deeeca828959a74c6d87274898db2c36a33c4fef" {
		t.Fatalf("runtime459 metadata fixture identity differs: read=%v close=%v", err, closeErr)
	}
	metadata := new(types.Metadata)
	if err := codec.Decode(raw, metadata); err != nil {
		t.Fatal(err)
	}
	return &Chain{Meta: metadata, GenesisHash: types.Hash{8}, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 459, TransactionVersion: 1}}
}

// Both CRv4 variants retain their calls while the signed runtime domain changes.
func TestSourceCommitmentRuntime459UsesActualMetadataAndDistinctSignedDomain(t *testing.T) {
	for _, mecid := range []*uint8{nil, new(uint8)} {
		previous := sourceRuntime455PreparedTest(t, 458, mecid)
		current := sourceRuntime455PreparedTest(t, 459, mecid)
		oldCall, oldFields, oldPayload, err := preparedSourceEncoding(previous)
		if err != nil {
			t.Fatal(err)
		}
		call, fields, payload, err := preparedSourceEncoding(current)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(call, oldCall) || !bytes.Equal(fields, oldFields) || bytes.Equal(payload, oldPayload) {
			t.Fatal("runtime459 changed atomic calls or lost its distinct signing domain")
		}
		if err := sourceRuntime459TestChain(t).ValidatePreparedSource(current); err != nil {
			t.Fatalf("actual runtime459 metadata refused exact signed submission: %v", err)
		}
	}
}

// An existing signature cannot be relabeled across any reviewed runtime pair.
func TestSourceCommitmentRuntime459RejectsRetainedSignatureRelabeling(t *testing.T) {
	for _, source := range []uint32{454, 455, 458, 459} {
		for _, target := range []uint32{454, 455, 458, 459} {
			if source == target {
				continue
			}
			prepared := sourceRuntime455PreparedTest(t, source, nil)
			prepared.SourceCommitment.RuntimeSpec = target
			if _, err := prepared.Validate(); err == nil {
				t.Fatalf("runtime%d signature acquired runtime%d authority", source, target)
			}
		}
	}
}

// Reviewed encoding never admits an intermediate/future version or changed
// metadata, genesis or transaction domain just because the source is similar.
func TestSourceCommitmentRuntime459RejectsAdjacentUnreviewedDomains(t *testing.T) {
	prepared := sourceRuntime455PreparedTest(t, 459, nil)
	for _, change := range []func(*Chain){
		func(chain *Chain) { chain.Runtime.SpecVersion = 455 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 456 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 457 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 460 },
		func(chain *Chain) { chain.Runtime.TransactionVersion = 2 },
		func(chain *Chain) { chain.GenesisHash = types.Hash{7} },
		func(chain *Chain) {
			extensions := chain.Meta.AsMetadataV14.Extrinsic.SignedExtensions
			for index, extension := range extensions {
				if extension.Identifier == "CheckSpecVersion" {
					chain.Meta.AsMetadataV14.Extrinsic.SignedExtensions = append(extensions[:index], extensions[index+1:]...)
					return
				}
			}
			t.Fatal("reviewed metadata omitted its spec signed extension")
		},
		func(chain *Chain) {
			for index := range chain.Meta.AsMetadataV14.Pallets {
				if chain.Meta.AsMetadataV14.Pallets[index].Name == "Utility" {
					chain.Meta.AsMetadataV14.Pallets[index].Index++
					return
				}
			}
			t.Fatal("reviewed metadata omitted Utility")
		},
	} {
		chain := sourceRuntime459TestChain(t)
		change(chain)
		if err := chain.ValidatePreparedSource(prepared); err == nil {
			t.Fatal("runtime459 submission accepted a changed independent domain")
		}
	}
	for _, spec := range []uint32{456, 457, 460} {
		changed := *prepared.SourceCommitment
		changed.RuntimeSpec = spec
		copyPrepared := *prepared
		copyPrepared.SourceCommitment = &changed
		if _, _, _, err := preparedSourceEncoding(&copyPrepared); err == nil {
			t.Fatalf("unreviewed runtime%d acquired offline encoding authority", spec)
		}
	}
}
