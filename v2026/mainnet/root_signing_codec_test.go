// Cross-checks the root encoder against GSRPC's typed payload/extrinsic codec,
// independent of the core's manual byte assembly and signature verification.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Both compact-vector lengths and Substrate's 256-byte signing boundary are
// checked against the external library using the exact same public signature.
func TestRootSigningMatchesTypedSubstrateCodec(t *testing.T) {
	action, pair, metadataHex := rootActionFixture(t)
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{8, 80} {
		action.Dests, action.Weights = make([]uint16, count), make([]uint16, count)
		for index := range action.Dests {
			action.Dests[index], action.Weights[index] = uint16(index), 10
		}
		prepared, err := prepareRootAction(action, metadataHex)
		if err != nil {
			t.Fatal(err)
		}
		call, err := types.NewCall(metadata, "SubtensorModule.set_root_weights", action.Dests, action.Weights)
		if err != nil {
			t.Fatal(err)
		}
		callRaw, err := codec.Encode(call)
		if err != nil || "0x"+hex.EncodeToString(callRaw) != prepared.Call {
			t.Fatalf("typed call differs: %v", err)
		}
		genesis, _ := types.NewHashFromHexString(action.Scope.GenesisHash)
		birthHash, _ := types.NewHashFromHexString(action.BirthHash)
		payload := extrinsic.Payload{
			EncodedCall: types.BytesBare(callRaw),
			SignedFields: []*extrinsic.SignedField{
				{Name: extrinsic.EraSignedField, Value: types.ExtrinsicEra{IsMortalEra: true, AsMortalEra: types.MortalEra{First: 5, Second: 0}}, Mutated: true},
				{Name: extrinsic.NonceSignedField, Value: types.NewUCompactFromUInt(uint64(action.Nonce)), Mutated: true},
				{Name: extrinsic.TipSignedField, Value: types.NewUCompactFromUInt(0), Mutated: true},
				{Name: extrinsic.CheckMetadataHashModeSignedField, Value: types.U8(0), Mutated: true},
			},
			SignedExtraFields: []*extrinsic.SignedField{
				{Name: extrinsic.SpecVersionSignedField, Value: types.U32(action.Scope.RuntimeVersion.SpecVersion), Mutated: true},
				{Name: extrinsic.TransactionVersionSignedField, Value: types.U32(action.Scope.RuntimeVersion.TransactionVersion), Mutated: true},
				{Name: extrinsic.GenesisHashSignedField, Value: genesis, Mutated: true},
				{Name: extrinsic.BlockHashSignedField, Value: birthHash, Mutated: true},
				{Name: extrinsic.CheckMetadataHashSignedField, Value: types.NewEmptyOption[types.Hash](), Mutated: true},
			},
		}
		payloadRaw, err := codec.Encode(&payload)
		if err != nil || "0x"+hex.EncodeToString(payloadRaw) != prepared.Payload {
			t.Fatalf("typed payload differs at %d destinations: %v", count, err)
		}
		signer := &rootSignerFixture{pair: pair}
		signature, err := signer.signOnce(context.Background(), prepared)
		if err != nil {
			t.Fatal(err)
		}
		address, err := types.NewMultiAddressFromAccountID(pair.Public())
		if err != nil {
			t.Fatal(err)
		}
		typed := extrinsic.Extrinsic{
			Version: extrinsic.Version4 | extrinsic.BitSigned, Method: call,
			Signature: &extrinsic.Signature{Signer: address, Signature: types.MultiSignature{IsSr25519: true, AsSr25519: types.NewSignature(signature)}, SignedFields: payload.SignedFields},
		}
		want, err := codec.Encode(typed)
		if err != nil {
			t.Fatal(err)
		}
		got, err := prepared.signed(signature)
		if err != nil || !bytes.Equal(want, got) {
			t.Fatalf("typed signed extrinsic differs at %d destinations: %v", count, err)
		}
	}
}
