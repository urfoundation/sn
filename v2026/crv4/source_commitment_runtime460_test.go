// Runtime460 uses the exact reviewed metadata with synthetic accounts and heads.
package crv4

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// The actual decoded metadata changes only System.Version's spec constant;
// calls, storage layouts, signed extensions and all other constants retain459.
func TestSourceCommitmentRuntime460MetadataChangesOnlySpecConstant(t *testing.T) {
	// SDK cacb431 encodes this exact field order, including the final system
	// version byte. The RPC client's RuntimeVersion has a different SCALE layout.
	type runtimeVersion struct {
		SpecName         string
		ImplName         string
		AuthoringVersion uint32
		SpecVersion      uint32
		ImplVersion      uint32
		Apis             []struct {
			Id      [8]byte
			Version uint32
		}
		TransactionVersion uint32
		SystemVersion      uint8
	}
	decodeVersion := func(raw []byte) runtimeVersion {
		t.Helper()
		input := bytes.NewReader(raw)
		var version runtimeVersion
		if err := scale.NewDecoder(input).Decode(&version); err != nil {
			t.Fatal(err)
		}
		if input.Len() != 0 {
			t.Fatal("runtime version constant has trailing bytes")
		}
		return version
	}
	prior, current := sourceRuntime459TestChain(t), sourceRuntime460TestChain(t)
	var priorValue []byte
	for _, pallet := range prior.Meta.AsMetadataV14.Pallets {
		if pallet.Name == "System" {
			for _, constant := range pallet.Constants {
				if constant.Name == "Version" {
					priorValue = append([]byte(nil), constant.Value...)
				}
			}
		}
	}
	if len(priorValue) == 0 {
		t.Fatal("prior metadata omitted System.Version")
	}
	found := false
	for palletIndex := range current.Meta.AsMetadataV14.Pallets {
		pallet := &current.Meta.AsMetadataV14.Pallets[palletIndex]
		if pallet.Name != "System" {
			continue
		}
		for constantIndex := range pallet.Constants {
			constant := &pallet.Constants[constantIndex]
			if constant.Name != "Version" {
				continue
			}
			if len(constant.Value) != len(priorValue) {
				t.Fatal("runtime version constant length changed")
			}
			differences := 0
			for index, value := range constant.Value {
				if value != priorValue[index] {
					differences++
					if priorValue[index] != 0xcb || value != 0xcc {
						t.Fatal("runtime version raw constant changed outside459-to460")
					}
				}
			}
			if differences != 1 {
				t.Fatal("runtime version constant did not retain every non-spec byte")
			}
			oldVersion, newVersion := decodeVersion(priorValue), decodeVersion(constant.Value)
			if oldVersion.SpecVersion != 459 || newVersion.SpecVersion != 460 {
				t.Fatal("metadata spec constant differs")
			}
			oldVersion.SpecVersion = 460
			if !reflect.DeepEqual(oldVersion, newVersion) {
				t.Fatal("runtime metadata version changed outside spec")
			}
			constant.Value = append([]byte(nil), priorValue...)
			found = true
		}
	}
	if !found || !reflect.DeepEqual(prior.Meta, current.Meta) {
		t.Fatal("runtime460 metadata changed outside System.Version")
	}
}

// Bounds and authenticates the actual460 metadata before any call construction.
func sourceRuntime460TestChain(t *testing.T) *Chain {
	t.Helper()
	encoded, err := os.ReadFile("../miner/testdata/runtime460-metadata.scale.gz.base64")
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
		hex.EncodeToString(sha[:]) != "0e18eed4701255a567411bdc646c76eba355cf41bcb5fcbed8f673a458118e1a" ||
		hex.EncodeToString(blake[:]) != "98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c" {
		t.Fatalf("runtime460 metadata fixture identity differs: read=%v close=%v", err, closeErr)
	}
	metadata := new(types.Metadata)
	if err := codec.Decode(raw, metadata); err != nil {
		t.Fatal(err)
	}
	return &Chain{Meta: metadata, GenesisHash: types.Hash{8}, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 460, TransactionVersion: 1}}
}

// Both CRv4 variants retain their calls while the signed runtime domain changes.
func TestSourceCommitmentRuntime460UsesActualMetadataAndDistinctSignedDomain(t *testing.T) {
	for _, mecid := range []*uint8{nil, new(uint8)} {
		previous := sourceRuntime455PreparedTest(t, 459, mecid)
		current := sourceRuntime455PreparedTest(t, 460, mecid)
		oldCall, oldFields, oldPayload, err := preparedSourceEncoding(previous)
		if err != nil {
			t.Fatal(err)
		}
		call, fields, payload, err := preparedSourceEncoding(current)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(call, oldCall) || !bytes.Equal(fields, oldFields) || bytes.Equal(payload, oldPayload) {
			t.Fatal("runtime460 changed atomic calls or lost its distinct signing domain")
		}
		if err := sourceRuntime460TestChain(t).ValidatePreparedSource(current); err != nil {
			t.Fatalf("actual runtime460 metadata refused exact signed submission: %v", err)
		}
	}
}

// An existing signature cannot be relabeled across any reviewed runtime pair.
func TestSourceCommitmentRuntime460RejectsRetainedSignatureRelabeling(t *testing.T) {
	for _, source := range []uint32{454, 455, 458, 459, 460} {
		for _, target := range []uint32{454, 455, 458, 459, 460} {
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
func TestSourceCommitmentRuntime460RejectsAdjacentUnreviewedDomains(t *testing.T) {
	prepared := sourceRuntime455PreparedTest(t, 460, nil)
	for _, change := range []func(*Chain){
		func(chain *Chain) { chain.Runtime.SpecVersion = 455 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 456 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 457 },
		func(chain *Chain) { chain.Runtime.SpecVersion = 461 },
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
		chain := sourceRuntime460TestChain(t)
		change(chain)
		if err := chain.ValidatePreparedSource(prepared); err == nil {
			t.Fatal("runtime460 submission accepted a changed independent domain")
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
