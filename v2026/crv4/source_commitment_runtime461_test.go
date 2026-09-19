// Runtime461 uses the exact reviewed metadata with synthetic accounts and heads.
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

// Bounds and authenticates the actual461 metadata before any call construction.
func sourceRuntime461TestChain(t *testing.T) *Chain {
	t.Helper()
	encoded, err := os.ReadFile("../miner/testdata/runtime461-metadata.scale.gz.base64")
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
	raw, err := io.ReadAll(io.LimitReader(reader, 344268))
	closeErr := reader.Close()
	sha, blake := sha256.Sum256(raw), blake2b.Sum256(raw)
	if err != nil || closeErr != nil || input.Len() != 0 || len(raw) != 344267 ||
		hex.EncodeToString(sha[:]) != "ddeffac09b36b85f584ad08b11441f7eae184728a67fa286c0c9b919d27f0405" ||
		hex.EncodeToString(blake[:]) != "98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68" {
		t.Fatalf("runtime461 metadata fixture identity differs: read=%v close=%v", err, closeErr)
	}
	metadata := new(types.Metadata)
	if err := codec.Decode(raw, metadata); err != nil {
		t.Fatal(err)
	}
	return &Chain{Meta: metadata, GenesisHash: types.Hash{8}, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 461, TransactionVersion: 1}}
}

// Both CRv4 variants retain their calls while the signed runtime domain changes.
func TestSourceCommitmentRuntime461UsesActualMetadataAndDistinctSignedDomain(t *testing.T) {
	for _, mecid := range []*uint8{nil, new(uint8)} {
		previous := sourceRuntime455PreparedTest(t, 460, mecid)
		current := sourceRuntime455PreparedTest(t, 461, mecid)
		oldCall, oldFields, oldPayload, err := preparedSourceEncoding(previous)
		if err != nil {
			t.Fatal(err)
		}
		call, fields, payload, err := preparedSourceEncoding(current)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(call, oldCall) || !bytes.Equal(fields, oldFields) || bytes.Equal(payload, oldPayload) {
			t.Fatal("runtime461 changed atomic calls or lost its distinct signing domain")
		}
		if err := sourceRuntime461TestChain(t).ValidatePreparedSource(current); err != nil {
			t.Fatalf("actual runtime461 metadata refused exact signed submission: %v", err)
		}
	}
}

// An existing signature cannot be relabeled across any reviewed runtime pair.
func TestSourceCommitmentRuntime461RejectsRetainedSignatureRelabeling(t *testing.T) {
	for _, source := range []uint32{454, 455, 458, 459, 460, 461} {
		for _, target := range []uint32{454, 455, 458, 459, 460, 461} {
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
func TestSourceCommitmentRuntime461RejectsAdjacentUnreviewedDomains(t *testing.T) {
	prepared := sourceRuntime455PreparedTest(t, 461, nil)
	for _, change := range []func(*Chain){
		func(chain *Chain) { chain.Runtime.SpecVersion = 455 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 456 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 457 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 460 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 462 },
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
		chain := sourceRuntime461TestChain(t)
		change(chain)
		if err := chain.ValidatePreparedSource(prepared); err == nil {
			t.Fatal("runtime461 submission accepted a changed independent domain")
		}
	}
	for _, spec := range []uint32{456, 457, 462} {
		changed := *prepared.SourceCommitment
		changed.RuntimeSpec = spec
		copyPrepared := *prepared
		copyPrepared.SourceCommitment = &changed
		if _, _, _, err := preparedSourceEncoding(&copyPrepared); err == nil {
			t.Fatalf("unreviewed runtime%d acquired offline encoding authority", spec)
		}
	}
}
