package chain

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/crv4"
)

// NetuidArg encodes a NetUid storage-map argument (little-endian u16).
func NetuidArg(netuid uint16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], netuid)
	return b[:]
}

// DecodeStorageFallback decodes a runtime-declared ValueQuery fallback when no
// raw key exists. A missing OptionalQuery remains absent; inventing a zero
// value there would hide missing identities and subnet state.
func DecodeStorageFallback(entry types.StorageEntryMetadata, value any) (bool, error) {
	entryV14, ok := entry.(types.StorageEntryMetadataV14)
	if !ok {
		return false, fmt.Errorf("runtime storage entry is not metadata v14")
	}
	if !entryV14.Modifier.IsDefault {
		return false, nil
	}
	if err := codec.Decode(entryV14.Fallback, value); err != nil {
		return false, fmt.Errorf("decode runtime storage fallback: %w", err)
	}
	return true, nil
}

// ReadStorageAtContext reads one exact-block value with the same absent-key
// semantics the runtime applies. The chain's bound metadata derives the
// fallback, so callers authenticate that metadata for the block first.
func ReadStorageAtContext(ctx context.Context, chain *crv4.Chain, key types.StorageKey, pallet, storage string, value any, blockHash types.Hash) (bool, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || chain.Meta == nil || blockHash == (types.Hash{}) {
		return false, errors.New("storage read dependencies are unavailable")
	}
	var encoded *string
	if err := chain.API.Client.CallContext(ctx, &encoded, "state_getStorage", key.Hex(), blockHash.Hex()); err != nil {
		return false, fmt.Errorf("read %s.%s: %w", pallet, storage, err)
	}
	if encoded != nil {
		raw, err := codec.HexDecodeString(*encoded)
		if err != nil {
			return false, fmt.Errorf("decode %s.%s: %w", pallet, storage, err)
		}
		if err := codec.Decode(raw, value); err != nil {
			return false, fmt.Errorf("decode %s.%s: %w", pallet, storage, err)
		}
		return true, nil
	}
	entry, err := chain.Meta.FindStorageEntryMetadata(pallet, storage)
	if err != nil {
		return false, err
	}
	return DecodeStorageFallback(entry, value)
}

// ReadStorageAt preserves the contextless surface used by the harness.
func ReadStorageAt(chain *crv4.Chain, key types.StorageKey, pallet, storage string, value any, blockHash types.Hash) (bool, error) {
	return ReadStorageAtContext(context.Background(), chain, key, pallet, storage, value, blockHash)
}

// ReadRequiredStorageAtContext requires a concrete value or a runtime-declared
// ValueQuery fallback. Absent optional storage is never a real zero.
func ReadRequiredStorageAtContext(ctx context.Context, chain *crv4.Chain, key types.StorageKey, pallet, storage string, value any, blockHash types.Hash) error {
	present, err := ReadStorageAtContext(ctx, chain, key, pallet, storage, value, blockHash)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("%s.%s storage is absent", pallet, storage)
	}
	return nil
}

// ReadRequiredStorageAt preserves the contextless surface used by the harness.
func ReadRequiredStorageAt(chain *crv4.Chain, key types.StorageKey, pallet, storage string, value any, blockHash types.Hash) error {
	return ReadRequiredStorageAtContext(context.Background(), chain, key, pallet, storage, value, blockHash)
}

// AccountInfo matches the reviewed runtime's System.Account value. Subtensor's
// Balance is u64 (rao); the generic GSRPC AccountInfo assumes u128 and cannot
// decode this runtime's AccountData.
type AccountInfo struct {
	Nonce       types.U32
	Consumers   types.U32
	Providers   types.U32
	Sufficients types.U32
	Data        struct {
		Free     types.U64
		Reserved types.U64
		Frozen   types.U64
		Flags    types.U128
	}
}

// readSubtensorStorage derives a SubtensorModule key and reads it at one block.
func readSubtensorStorage(ctx context.Context, chain *crv4.Chain, storage string, value any, blockHash types.Hash, args ...[]byte) (bool, error) {
	if chain == nil || chain.Meta == nil {
		return false, errors.New("storage metadata is unavailable")
	}
	key, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, storage, args...)
	if err != nil {
		return false, fmt.Errorf("storage key %s.%s: %w", crv4.PalletName, storage, err)
	}
	return ReadStorageAtContext(ctx, chain, key, crv4.PalletName, storage, value, blockHash)
}

// FreeBalanceAtContext reads an account's free balance in rao. An absent
// account has a zero balance.
func FreeBalanceAtContext(ctx context.Context, chain *crv4.Chain, account [32]byte, blockHash types.Hash) (uint64, error) {
	if chain == nil || chain.Meta == nil {
		return 0, errors.New("storage metadata is unavailable")
	}
	key, err := types.CreateStorageKey(chain.Meta, "System", "Account", account[:])
	if err != nil {
		return 0, fmt.Errorf("storage key System.Account: %w", err)
	}
	var info AccountInfo
	present, err := ReadStorageAtContext(ctx, chain, key, "System", "Account", &info, blockHash)
	if err != nil || !present {
		return 0, err
	}
	return uint64(info.Data.Free), nil
}

// UIDAtContext resolves a hotkey's UID on the subnet at one block.
func UIDAtContext(ctx context.Context, chain *crv4.Chain, netuid uint16, hotkey [32]byte, blockHash types.Hash) (uint16, bool, error) {
	var uid types.U16
	present, err := readSubtensorStorage(ctx, chain, "Uids", &uid, blockHash, NetuidArg(netuid), hotkey[:])
	return uint16(uid), present, err
}

// HotkeyOwnerAtContext reads the coldkey that owns a hotkey. Owner is a
// ValueQuery; an unowned hotkey resolves to the runtime's zero-account
// fallback, which callers must treat as unowned.
func HotkeyOwnerAtContext(ctx context.Context, chain *crv4.Chain, hotkey [32]byte, blockHash types.Hash) ([32]byte, error) {
	var owner types.AccountID
	if _, err := readSubtensorStorage(ctx, chain, "Owner", &owner, blockHash, hotkey[:]); err != nil {
		return [32]byte{}, err
	}
	return [32]byte(owner), nil
}

// TotalHotkeyAlphaAtContext reads the alpha (rao) staked on a hotkey for one
// subnet across every coldkey. The runtime's default is zero.
func TotalHotkeyAlphaAtContext(ctx context.Context, chain *crv4.Chain, hotkey [32]byte, netuid uint16, blockHash types.Hash) (uint64, error) {
	var alpha types.U64
	if _, err := readSubtensorStorage(ctx, chain, "TotalHotkeyAlpha", &alpha, blockHash, hotkey[:], NetuidArg(netuid)); err != nil {
		return 0, err
	}
	return uint64(alpha), nil
}

// SubnetworkNAtContext reads the subnet's registered UID count.
func SubnetworkNAtContext(ctx context.Context, chain *crv4.Chain, netuid uint16, blockHash types.Hash) (uint16, error) {
	var count types.U16
	present, err := readSubtensorStorage(ctx, chain, "SubnetworkN", &count, blockHash, NetuidArg(netuid))
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fmt.Errorf("SubnetworkN is absent for netuid %d", netuid)
	}
	return uint16(count), nil
}

// MaxAllowedUidsAtContext reads the subnet's UID capacity.
func MaxAllowedUidsAtContext(ctx context.Context, chain *crv4.Chain, netuid uint16, blockHash types.Hash) (uint16, error) {
	var maximum types.U16
	present, err := readSubtensorStorage(ctx, chain, "MaxAllowedUids", &maximum, blockHash, NetuidArg(netuid))
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fmt.Errorf("MaxAllowedUids is absent for netuid %d", netuid)
	}
	return uint16(maximum), nil
}

// ValidatorPermitAtContext reads one UID's validator permit from the subnet's
// complete permit vector.
func ValidatorPermitAtContext(ctx context.Context, chain *crv4.Chain, netuid uint16, uid uint16, blockHash types.Hash) (bool, error) {
	var permits []types.Bool
	present, err := readSubtensorStorage(ctx, chain, "ValidatorPermit", &permits, blockHash, NetuidArg(netuid))
	if err != nil {
		return false, err
	}
	if !present || int(uid) >= len(permits) {
		return false, nil
	}
	return bool(permits[uid]), nil
}

// PoolReserves is the subnet's Dynamic TAO pool at one block.
type PoolReserves struct {
	TaoRao     uint64
	AlphaInRao uint64
}

// PriceQ9 returns the pool price in TAO rao per alpha (1 alpha = 1e9 rao),
// the unit add_stake_limit's limit_price uses. Zero when the pool is empty.
func (self PoolReserves) PriceQ9() uint64 {
	if self.AlphaInRao == 0 {
		return 0
	}
	price := new(big.Int).Mul(new(big.Int).SetUint64(self.TaoRao), big.NewInt(1_000_000_000))
	price.Quo(price, new(big.Int).SetUint64(self.AlphaInRao))
	if !price.IsUint64() {
		return ^uint64(0)
	}
	return price.Uint64()
}

// PoolReservesAtContext reads SubnetTAO and SubnetAlphaIn at one block.
func PoolReservesAtContext(ctx context.Context, chain *crv4.Chain, netuid uint16, blockHash types.Hash) (PoolReserves, error) {
	var tao, alphaIn types.U64
	if _, err := readSubtensorStorage(ctx, chain, "SubnetTAO", &tao, blockHash, NetuidArg(netuid)); err != nil {
		return PoolReserves{}, err
	}
	if _, err := readSubtensorStorage(ctx, chain, "SubnetAlphaIn", &alphaIn, blockHash, NetuidArg(netuid)); err != nil {
		return PoolReserves{}, err
	}
	return PoolReserves{TaoRao: uint64(tao), AlphaInRao: uint64(alphaIn)}, nil
}

// FormatRao renders a rao amount in whole units (1 unit = 1e9 rao).
func FormatRao(rao uint64) string {
	return fmt.Sprintf("%d.%09d", rao/1_000_000_000, rao%1_000_000_000)
}
