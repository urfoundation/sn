package chain

import (
	"encoding/binary"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

func TestAccountInfoDecodesReviewedRuntimeU64Balances(t *testing.T) {
	raw := make([]byte, 56)
	binary.LittleEndian.PutUint32(raw[0:4], 7)
	binary.LittleEndian.PutUint32(raw[4:8], 2)
	binary.LittleEndian.PutUint32(raw[8:12], 1)
	binary.LittleEndian.PutUint32(raw[12:16], 0)
	binary.LittleEndian.PutUint64(raw[16:24], 12_345_678_901)
	binary.LittleEndian.PutUint64(raw[24:32], 22)
	binary.LittleEndian.PutUint64(raw[32:40], 33)
	raw[40] = 44 // little-endian u128 account flags

	var got AccountInfo
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

func TestStorageFallbackSuppliesAbsentValueQuery(t *testing.T) {
	entry := types.StorageEntryMetadataV14{
		Modifier: types.StorageFunctionModifierV0{IsDefault: true},
		Fallback: types.Bytes{0x33, 0xb3, 0x66, 0xe6},
	}
	var value struct {
		Low  types.U16
		High types.U16
	}
	present, err := DecodeStorageFallback(entry, &value)
	if err != nil {
		t.Fatal(err)
	}
	if !present || value.Low != 45_875 || value.High != 58_982 {
		t.Fatalf("runtime fallback decoded as present=%t value=%+v", present, value)
	}
}

func TestStorageFallbackKeepsAbsentOptionalQueryAbsent(t *testing.T) {
	entry := types.StorageEntryMetadataV14{
		Modifier: types.StorageFunctionModifierV0{IsOptional: true},
		Fallback: types.Bytes{0x01, 0x00},
	}
	value := types.U16(77)
	present, err := DecodeStorageFallback(entry, &value)
	if err != nil {
		t.Fatal(err)
	}
	if present || value != 77 {
		t.Fatalf("optional storage was invented as present=%t value=%d", present, value)
	}
}

func TestPoolPriceIsTaoRaoPerAlpha(t *testing.T) {
	for _, testCase := range []struct {
		name string
		pool PoolReserves
		want uint64
	}{
		{name: "parity", pool: PoolReserves{TaoRao: 5_000_000_000, AlphaInRao: 5_000_000_000}, want: 1_000_000_000},
		{name: "cheap alpha", pool: PoolReserves{TaoRao: 1_000_000_000, AlphaInRao: 4_000_000_000}, want: 250_000_000},
		{name: "empty pool", pool: PoolReserves{TaoRao: 1}, want: 0},
	} {
		if got := testCase.pool.PriceQ9(); got != testCase.want {
			t.Fatalf("%s: price = %d, want %d", testCase.name, got, testCase.want)
		}
	}
	if got := FormatRao(1_234_000_000_005); got != "1234.000000005" {
		t.Fatalf("FormatRao = %s", got)
	}
}
