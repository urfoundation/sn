// Liquidation preflight checks both the selected native position and the
// coldkey-wide lock. It does not classify that principal as earned income.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const treasuryPositionApi = "StakeInfoRuntimeApi_get_stake_info_for_hotkey_coldkey_netuid"
const treasuryAvailabilityApi = "StakeInfoRuntimeApi_get_stake_availability_for_coldkeys"

// Metadata selects the exact per-position collateral; a sibling's free stake
// cannot satisfy this guard. The runtime enforces the same guard at dispatch.
func treasuryCollateralKey(metadata *types.Metadata, hotkey, coldkey []byte) (types.StorageKey, error) {
	count := 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "SubtensorModule" {
			continue
		}
		for _, entry := range pallet.Storage.Items {
			if entry.Name != "MinerCollateral" {
				continue
			}
			count++
			if !entry.Modifier.IsOptional || !entry.Type.IsMap || len(entry.Type.AsMap.Hashers) != 3 || !entry.Type.AsMap.Hashers[0].IsIdentity || !entry.Type.AsMap.Hashers[1].IsBlake2_128Concat || !entry.Type.AsMap.Hashers[2].IsBlake2_128Concat {
				return nil, errors.New("treasury collateral key hashers changed")
			}
			key := metadata.AsMetadataV14.EfficientLookup[entry.Type.AsMap.Key.Int64()]
			if key == nil || !key.Def.IsTuple || len(key.Def.Tuple) != 3 {
				return nil, errors.New("treasury collateral key tuple changed")
			}
			for i, shape := range []string{"u16", "account", "account"} {
				if !rootTypeMatches(metadata, key.Def.Tuple[i], shape, 0) {
					return nil, errors.New("treasury collateral key field changed")
				}
			}
			value := metadata.AsMetadataV14.EfficientLookup[entry.Type.AsMap.Value.Int64()]
			if value == nil || !value.Def.IsComposite || len(value.Def.Composite.Fields) != 4 {
				return nil, errors.New("treasury collateral value shape changed")
			}
			for i, name := range []string{"locked", "drain_ratio", "min_locked", "earned"} {
				field := value.Def.Composite.Fields[i]
				shape := "u64"
				if i == 1 {
					shape = "u128"
				}
				if !field.HasName || string(field.Name) != name || !treasuryType(metadata, field.Type, shape, 0) {
					return nil, errors.New("treasury collateral value field changed")
				}
			}
		}
	}
	if count != 1 {
		return nil, errors.New("treasury collateral storage absent or duplicated")
	}
	return types.CreateStorageKey(metadata, "SubtensorModule", "MinerCollateral", []byte{25, 0}, hotkey, coldkey)
}

// Each API result is a retained exact SCALE response at the same original hash.
func treasuryLiquidationAvailable(ctx context.Context, a treasuryAction, metadata *types.Metadata, read func(context.Context, string, []byte) ([]byte, bool, error)) (*uint64, error) {
	hotkey, _ := hex.DecodeString(a.Inner.Hotkey[2:])
	coldkey, _ := hex.DecodeString(a.Descriptor.Multisig.AccountId[2:])
	input := append(append(append([]byte(nil), hotkey...), coldkey...), 25, 0)
	position, exists, err := read(ctx, "runtime-api:"+treasuryPositionApi+":0x"+hex.EncodeToString(input), nil)
	if err != nil {
		return nil, err
	}
	if exists && len(position) == 1 && position[0] == 0 {
		return nil, nil
	}
	if !exists || len(position) < 65 || position[0] != 1 || !bytes.Equal(position[1:33], hotkey) || !bytes.Equal(position[33:65], coldkey) {
		return nil, errors.New("treasury selected native stake position is unavailable")
	}
	r := rootScaleReader{data: position, offset: 65}
	values := [6]uint64{}
	for i := range values {
		values[i], err = r.compact()
		if err != nil {
			return nil, err
		}
	}
	registered, err := r.take(1)
	if err != nil || r.offset != len(position) || registered[0] > 1 || values[0] != 25 {
		return nil, errors.New("treasury native stake response differs from selected position")
	}
	// values[2] is StakeInfo.locked's source placeholder; never use it.
	input = append([]byte{4}, coldkey...)
	input = append(input, 1, 4, 25, 0)
	availability, exists, err := read(ctx, "runtime-api:"+treasuryAvailabilityApi+":0x"+hex.EncodeToString(input), nil)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("treasury coldkey availability is unknown")
	}
	r = rootScaleReader{data: availability}
	n, err := r.compact()
	if err != nil || n != 1 {
		return nil, errors.New("treasury availability coldkey count changed")
	}
	account, err := r.take(32)
	if err != nil || !bytes.Equal(account, coldkey) {
		return nil, errors.New("treasury availability coldkey differs")
	}
	n, err = r.compact()
	if err == nil && n == 0 && r.offset == len(availability) {
		return nil, nil
	}
	if err != nil || n != 1 {
		return nil, errors.New("treasury availability selected subnet is absent")
	}
	net, err := r.take(2)
	if err != nil || binary.LittleEndian.Uint16(net) != 25 {
		return nil, errors.New("treasury availability subnet differs")
	}
	available := [3]uint64{}
	for i := range available {
		available[i], err = r.compact()
		if err != nil {
			return nil, err
		}
	}
	free := uint64(0)
	if available[0] > available[1] {
		free = available[0] - available[1]
	}
	if r.offset != len(availability) || available[2] > free || values[1] > available[0] {
		return nil, errors.New("treasury availability result is contradictory")
	}
	key, err := treasuryCollateralKey(metadata, hotkey, coldkey)
	if err != nil {
		return nil, err
	}
	collateral, present, err := read(ctx, key.Hex(), nil)
	if err != nil {
		return nil, err
	}
	locked := uint64(0)
	if present {
		if len(collateral) != 40 {
			return nil, errors.New("treasury selected collateral value width changed")
		}
		locked = binary.LittleEndian.Uint64(collateral[:8])
	}
	positionFree := uint64(0)
	if values[1] > locked {
		positionFree = values[1] - locked
	}
	result := min(positionFree, available[2])
	return &result, nil
}
