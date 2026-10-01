// Runtime455 retains the audited atomic call layout while changing the actual
// signed payload domain. These tests use real metadata encoders and signatures.
package crv4

import (
	"bytes"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// The tiny metadata describes call indices and signed extensions, not an
// approved artifact. Release admission tests independently enforce code pins.
func sourceRuntime455TestChain() *Chain {
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	metadata.AsMetadataV14.EfficientLookup = map[int64]*types.Si1Type{}
	for index, pallet := range []struct {
		name     string
		index    byte
		variants []types.Si1Variant
	}{
		{name: "Commitments", index: 18, variants: []types.Si1Variant{{Name: "set_commitment", Index: 0}}},
		{name: "SubtensorModule", index: 7, variants: []types.Si1Variant{{Name: "commit_timelocked_weights", Index: 113}, {Name: "commit_timelocked_mechanism_weights", Index: 118}}},
		{name: "Utility", index: 11, variants: []types.Si1Variant{{Name: "batch_all", Index: 2}}},
	} {
		lookup := types.NewSi1LookupTypeIDFromUInt(uint64(index))
		metadata.AsMetadataV14.Pallets = append(metadata.AsMetadataV14.Pallets, types.PalletMetadataV14{Name: types.Text(pallet.name), Index: types.U8(pallet.index), HasCalls: true, Calls: types.FunctionMetadataV14{Type: lookup}})
		metadata.AsMetadataV14.EfficientLookup[int64(index)] = &types.Si1Type{Def: types.Si1TypeDef{IsVariant: true, Variant: types.Si1TypeDefVariant{Variants: pallet.variants}}}
	}
	metadata.AsMetadataV14.Extrinsic.Version = 4
	for index, name := range []string{"CheckSpecVersion", "CheckTxVersion", "CheckGenesis", "CheckMortality", "CheckNonce", "ChargeTransactionPaymentWrapper", "CheckMetadataHash"} {
		lookup := types.NewSi1LookupTypeIDFromUInt(uint64(index + 10))
		metadata.AsMetadataV14.EfficientLookup[int64(index+10)] = &types.Si1Type{Path: types.Si1Path{types.Text(name)}}
		metadata.AsMetadataV14.Extrinsic.SignedExtensions = append(metadata.AsMetadataV14.Extrinsic.SignedExtensions, types.SignedExtensionMetadataV14{Identifier: types.Text(name), Type: lookup})
	}
	return &Chain{Meta: metadata, GenesisHash: types.Hash{8}, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1}}
}

// Both historical454 and current455 use real sr25519 bytes. Re-signing is
// intentional fixture construction, never production recovery behavior.
func sourceRuntime455PreparedTest(t *testing.T, spec uint32, mecid *uint8) *PreparedSubmission {
	t.Helper()
	prepared, key := sourcePreparedTest(t)
	prepared.SourceCommitment.RuntimeSpec = spec
	prepared.Mecid = mecid
	signSourcePreparedTest(t, prepared, key, nil)
	return prepared
}

// The runtime upgrade changes signed-extra spec bytes, not the atomic calls.
func TestSourceCommitmentRuntime455PreservesReviewed454CallShape(t *testing.T) {
	for _, mecid := range []*uint8{nil, new(uint8)} {
		old := sourceRuntime455PreparedTest(t, 454, mecid)
		current := sourceRuntime455PreparedTest(t, 455, mecid)
		oldCall, oldFields, oldPayload, err := preparedSourceEncoding(old)
		if err != nil {
			t.Fatal(err)
		}
		currentCall, currentFields, currentPayload, err := preparedSourceEncoding(current)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(oldCall, currentCall) || !bytes.Equal(oldFields, currentFields) || bytes.Equal(oldPayload, currentPayload) {
			t.Fatal("runtime455 did not preserve exact calls with a distinct signed runtime domain")
		}
		for _, prepared := range []*PreparedSubmission{old, current} {
			chain := sourceRuntime455TestChain()
			chain.Runtime.SpecVersion = types.U32(prepared.SourceCommitment.RuntimeSpec)
			if err := chain.ValidatePreparedSource(prepared); err != nil {
				t.Fatalf("runtime%d actual metadata/signature validation: %v", prepared.SourceCommitment.RuntimeSpec, err)
			}
		}
	}
}

// Durable signed bytes cannot be relabeled in either direction across455.
func TestSourceCommitmentRuntime455RejectsCrossVersionSignatureReuse(t *testing.T) {
	for _, spec := range []uint32{454, 455} {
		prepared := sourceRuntime455PreparedTest(t, spec, nil)
		prepared.SourceCommitment.RuntimeSpec = 909 - spec
		if _, err := prepared.Validate(); err == nil {
			t.Fatalf("runtime%d signed bytes were accepted under runtime%d", spec, prepared.SourceCommitment.RuntimeSpec)
		}
	}
}

// Neither source encoding nor actual call construction admits future specs
// or transaction versions merely because the layout might still look alike.
func TestSourceCommitmentRuntime455RejectsUnreviewedVersionDomains(t *testing.T) {
	for _, version := range []struct {
		spec        uint32
		transaction uint32
	}{
		{spec: 0, transaction: 1}, {spec: 453, transaction: 1}, {spec: 456, transaction: 1},
		{spec: 454, transaction: 0}, {spec: 454, transaction: 2}, {spec: 455, transaction: 0}, {spec: 455, transaction: 2},
	} {
		prepared := sourceRuntime455PreparedTest(t, 455, nil)
		prepared.SourceCommitment.RuntimeSpec, prepared.SourceCommitment.TransactionVersion = version.spec, version.transaction
		if _, _, _, err := preparedSourceEncoding(prepared); err == nil {
			t.Fatalf("unreviewed encoding version %+v", version)
		}
		chain := sourceRuntime455TestChain()
		chain.Runtime.SpecVersion, chain.Runtime.TransactionVersion = types.U32(version.spec), types.U32(version.transaction)
		if _, err := chain.newSourceCommitmentBatchCall(521, nil, [32]byte{9}, []byte{1}, 22, 4); err == nil {
			t.Fatalf("unreviewed live call version %+v", version)
		}
	}
}

// Metadata is an independent authority: signed candidate fields cannot choose
// a replacement utility/commitments/subtensor dispatch under the same spec.
func TestSourceCommitmentRuntime455RejectsMetadataCallIndexDrift(t *testing.T) {
	prepared := sourceRuntime455PreparedTest(t, 455, nil)
	for _, index := range []int{0, 1, 2} {
		chain := sourceRuntime455TestChain()
		chain.Meta.AsMetadataV14.Pallets[index].Index++
		if err := chain.ValidatePreparedSource(prepared); err == nil {
			t.Fatalf("metadata pallet index drift %d was accepted", index)
		}
		chain = sourceRuntime455TestChain()
		chain.Meta.AsMetadataV14.EfficientLookup[int64(index)].Def.Variant.Variants[0].Index++
		if err := chain.ValidatePreparedSource(prepared); err == nil {
			t.Fatalf("metadata call index drift %d was accepted", index)
		}
	}
}

// Missing or reordered signed extensions are not accepted as compatible.
func TestSourceCommitmentRuntime455RejectsMetadataSignedExtensionDrift(t *testing.T) {
	prepared := sourceRuntime455PreparedTest(t, 455, nil)
	chain := sourceRuntime455TestChain()
	chain.Meta.AsMetadataV14.Extrinsic.SignedExtensions = chain.Meta.AsMetadataV14.Extrinsic.SignedExtensions[1:]
	if err := chain.ValidatePreparedSource(prepared); err == nil {
		t.Fatal("missing spec signed extension was accepted")
	}
	chain = sourceRuntime455TestChain()
	extensions := chain.Meta.AsMetadataV14.Extrinsic.SignedExtensions
	extensions[0], extensions[1] = extensions[1], extensions[0]
	if err := chain.ValidatePreparedSource(prepared); err == nil {
		t.Fatal("reordered runtime signed extensions were accepted")
	}
}

// The independent chain and candidate signer domains must agree even when
// exact candidate signatures and metadata call indices are individually valid.
func TestSourceCommitmentRuntime455RejectsIndependentChainDomainDrift(t *testing.T) {
	prepared := sourceRuntime455PreparedTest(t, 455, nil)
	for _, mutate := range []func(*Chain){
		func(chain *Chain) { chain.GenesisHash = types.Hash{7} },
		func(chain *Chain) { chain.Runtime.SpecVersion = 454 },
		func(chain *Chain) { chain.Runtime.TransactionVersion = 2 },
	} {
		chain := sourceRuntime455TestChain()
		mutate(chain)
		if err := chain.ValidatePreparedSource(prepared); err == nil {
			t.Fatal("independent chain/domain mismatch was accepted")
		}
	}
	if raw, err := codec.HexDecodeString(prepared.ExtrinsicHex); err != nil || len(raw) == 0 {
		t.Fatal("fixture has no original signed bytes")
	}
}
