// Runtime-shape regressions protect the testnet setup path from generic
// Substrate assumptions and similarly named storage items.
package main

import (
	"encoding/binary"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

func TestSubtensorAccountInfoDecodesRuntime447U64Balances(t *testing.T) {
	raw := make([]byte, 56)
	binary.LittleEndian.PutUint32(raw[0:4], 7)
	binary.LittleEndian.PutUint32(raw[4:8], 2)
	binary.LittleEndian.PutUint32(raw[8:12], 1)
	binary.LittleEndian.PutUint32(raw[12:16], 0)
	binary.LittleEndian.PutUint64(raw[16:24], 12_345_678_901)
	binary.LittleEndian.PutUint64(raw[24:32], 22)
	binary.LittleEndian.PutUint64(raw[32:40], 33)
	raw[40] = 44 // little-endian u128 account flags

	var got subtensorAccountInfo
	if err := codec.Decode(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Nonce != 7 || got.Consumers != 2 || got.Providers != 1 || got.Sufficients != 0 {
		t.Fatalf("account references decoded incorrectly: %+v", got)
	}
	if got.Data.Free != 12_345_678_901 || got.Data.Reserved != 22 || got.Data.Frozen != 33 || got.Data.Flags.Int == nil || got.Data.Flags.Uint64() != 44 {
		t.Fatalf("account data decoded incorrectly: %+v", got.Data)
	}
}

func TestTransferHyperparameterUsesAtomicTransferStorage(t *testing.T) {
	shape, ok := hyperShapes["transfer_enabled"]
	if !ok {
		t.Fatal("transfer_enabled hyperparameter is missing")
	}
	if shape.Storage != "TransferToggle" || shape.Call != "sudo_set_toggle_transfer" || shape.Kind != "bool" {
		t.Fatalf("transfer_enabled shape = %+v", shape)
	}
	if shape.Storage == "SubtokenEnabled" {
		t.Fatal("atomic transfer setting was confused with one-time subnet activation")
	}
}

func TestRuntimeStorageFallbackSuppliesAbsentValueQuery(t *testing.T) {
	entry := types.StorageEntryMetadataV14{
		Modifier: types.StorageFunctionModifierV0{IsDefault: true},
		Fallback: types.Bytes{0x33, 0xb3, 0x66, 0xe6},
	}
	var value struct {
		Low  types.U16
		High types.U16
	}
	present, err := decodeStorageFallback(entry, &value)
	if err != nil {
		t.Fatal(err)
	}
	if !present || value.Low != 45_875 || value.High != 58_982 {
		t.Fatalf("runtime fallback decoded as present=%t value=%+v", present, value)
	}
}

func TestRuntimeStorageFallbackKeepsAbsentOptionalQueryAbsent(t *testing.T) {
	entry := types.StorageEntryMetadataV14{
		Modifier: types.StorageFunctionModifierV0{IsOptional: true},
		Fallback: types.Bytes{0x01, 0x00},
	}
	value := types.U16(77)
	present, err := decodeStorageFallback(entry, &value)
	if err != nil {
		t.Fatal(err)
	}
	if present || value != 77 {
		t.Fatalf("optional storage was invented as present=%t value=%d", present, value)
	}
}

func TestHotkeyOwnerMustMatchExpectedColdkey(t *testing.T) {
	expected := [32]byte{1, 2, 3}
	if err := validateHotkeyOwner("validator", expected, expected); err != nil {
		t.Fatalf("matching coldkey rejected: %v", err)
	}
	actual := expected
	actual[31] = 4
	if err := validateHotkeyOwner("validator", actual, expected); err == nil {
		t.Fatal("mismatched registration coldkey was accepted")
	}
}
