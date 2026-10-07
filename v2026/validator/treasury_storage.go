// The treasury reader consumes the reviewed native Owner and optional
// AutoStakeDestination mappings at the same authenticated historical artifact.
package validator

import (
	"errors"
	"reflect"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// A matching artifact hash does not replace the exact storage interface check.
func treasuryStorageProfile(metadata *types.Metadata, entries map[string]types.StorageEntryMetadataV14) error {
	owner, exists := entries["Owner"]
	if !exists || !owner.Type.IsMap || !owner.Modifier.IsDefault || owner.Modifier.IsOptional ||
		!reflect.DeepEqual(owner.Type.AsMap.Hashers, []types.StorageHasherV10{{IsBlake2_128Concat: true}}) ||
		!ownerRecycleStorageType(metadata, owner.Type.AsMap.Key, "account", 0) || !ownerRecycleStorageType(metadata, owner.Type.AsMap.Value, "account", 0) {
		return errors.New("treasury Owner storage differs from the reviewed native interface")
	}
	destination, exists := entries["AutoStakeDestination"]
	if !exists || !destination.Type.IsMap || destination.Modifier.IsDefault || !destination.Modifier.IsOptional ||
		!reflect.DeepEqual(destination.Type.AsMap.Hashers, []types.StorageHasherV10{{IsBlake2_128Concat: true}, {IsIdentity: true}}) ||
		!ownerRecycleStorageType(metadata, destination.Type.AsMap.Value, "account", 0) {
		return errors.New("treasury auto-stake storage differs from the reviewed native interface")
	}
	key := metadata.AsMetadataV14.EfficientLookup[destination.Type.AsMap.Key.Int64()]
	if key == nil || !key.Def.IsTuple || len(key.Def.Tuple) != 2 ||
		!ownerRecycleStorageType(metadata, key.Def.Tuple[0], "account", 0) || !ownerRecycleStorageType(metadata, key.Def.Tuple[1], "u16", 0) {
		return errors.New("treasury auto-stake storage key is not the exact coldkey/netuid tuple")
	}
	return nil
}
