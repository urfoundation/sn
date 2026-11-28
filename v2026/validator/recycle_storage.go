// The exact approved metadata must also expose the consumed storage interface
// of the reviewed owner-recognition source; a hash alone cannot change meaning.
package validator

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Matches the fixed source interface, including query semantics and hashers.
func ownerRecycleStorageProfile(metadata *types.Metadata) (map[string]types.StorageEntryMetadataV14, error) {
	if metadata == nil || metadata.Version != 14 {
		return nil, errors.New("owner-recycle storage requires authenticated metadata14")
	}
	identity := types.StorageHasherV10{IsIdentity: true}
	blake := types.StorageHasherV10{IsBlake2_128Concat: true}
	twox := types.StorageHasherV10{IsTwox64Concat: true}
	definitions := []struct {
		name     string
		key      string
		value    string
		hashers  []types.StorageHasherV10
		optional bool
	}{
		{name: "SubnetOwner", key: "u16", value: "account", hashers: []types.StorageHasherV10{identity}},
		{name: "OwnedHotkeys", key: "account", value: "accounts", hashers: []types.StorageHasherV10{blake}},
		{name: "SubnetOwnerHotkey", key: "u16", value: "account", hashers: []types.StorageHasherV10{identity}},
		{name: "RecycleOrBurn", key: "u16", value: "mode", hashers: []types.StorageHasherV10{identity}},
		{name: "SubnetworkN", key: "u16", value: "u16", hashers: []types.StorageHasherV10{identity}},
		{name: "Keys", key: "netuid-uid", value: "account", hashers: []types.StorageHasherV10{identity, identity}},
		{name: "Uids", key: "netuid-account", value: "u16", hashers: []types.StorageHasherV10{identity, blake}, optional: true},
		{name: "BlockAtRegistration", key: "netuid-uid", value: "u64", hashers: []types.StorageHasherV10{identity, identity}},
		{name: "MechanismCountCurrent", key: "u16", value: "u8", hashers: []types.StorageHasherV10{twox}},
		{name: "SubnetEpochIndex", key: "u16", value: "u64", hashers: []types.StorageHasherV10{identity}},
		{name: "MinAllowedWeights", key: "u16", value: "u16", hashers: []types.StorageHasherV10{identity}},
		{name: "MaxWeightsLimit", key: "u16", value: "u16", hashers: []types.StorageHasherV10{identity}},
	}
	entries := map[string]types.StorageEntryMetadataV14{}
	palletCount := 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "SubtensorModule" {
			continue
		}
		palletCount++
		if !pallet.HasStorage || pallet.Storage.Prefix != "SubtensorModule" {
			return nil, errors.New("owner-recycle storage pallet prefix differs")
		}
		for _, candidate := range pallet.Storage.Items {
			name := string(candidate.Name)
			if _, duplicate := entries[name]; duplicate {
				return nil, errors.New("owner-recycle storage metadata contains duplicate entries")
			}
			entries[name] = candidate
		}
	}
	if palletCount != 1 {
		return nil, errors.New("owner-recycle storage pallet is absent or duplicated")
	}
	for _, definition := range definitions {
		entry, found := entries[definition.name]
		if !found || !entry.Type.IsMap || entry.Modifier.IsOptional != definition.optional || entry.Modifier.IsDefault == definition.optional ||
			!reflect.DeepEqual(entry.Type.AsMap.Hashers, definition.hashers) ||
			!ownerRecycleStorageType(metadata, entry.Type.AsMap.Key, definition.key, 0) || !ownerRecycleStorageType(metadata, entry.Type.AsMap.Value, definition.value, 0) {
			return nil, fmt.Errorf("owner-recycle storage %s differs from reviewed key/value/hashers/query semantics", definition.name)
		}
		if definition.name == "RecycleOrBurn" || definition.name == "OwnedHotkeys" {
			if !bytes.Equal(entry.Fallback, []byte{0}) {
				return nil, fmt.Errorf("owner-recycle storage %s has an unsupported default", definition.name)
			}
		}
	}
	return entries, nil
}

// Bounded single-field wrappers cover NetUid, MechId and AccountId32 without
// accepting arbitrary composites or cycling through hostile portable ids.
func ownerRecycleStorageType(metadata *types.Metadata, id types.Si1LookupTypeID, kind string, depth int) bool {
	if depth > 8 {
		return false
	}
	value := metadata.AsMetadataV14.EfficientLookup[id.Int64()]
	if value == nil {
		return false
	}
	definition := value.Def
	if definition.IsComposite && len(definition.Composite.Fields) == 1 {
		return ownerRecycleStorageType(metadata, definition.Composite.Fields[0].Type, kind, depth+1)
	}
	switch kind {
	case "u8":
		return definition.IsPrimitive && definition.Primitive.Si0TypeDefPrimitive == types.IsU8
	case "u16":
		return definition.IsPrimitive && definition.Primitive.Si0TypeDefPrimitive == types.IsU16
	case "u64":
		return definition.IsPrimitive && definition.Primitive.Si0TypeDefPrimitive == types.IsU64
	case "account":
		return definition.IsArray && definition.Array.Len == 32 && ownerRecycleStorageType(metadata, definition.Array.Type, "u8", depth+1)
	case "accounts":
		return definition.IsSequence && ownerRecycleStorageType(metadata, definition.Sequence.Type, "account", depth+1)
	case "u64s":
		return definition.IsSequence && ownerRecycleStorageType(metadata, definition.Sequence.Type, "u64", depth+1)
	case "netuid-uid", "netuid-account":
		if !definition.IsTuple || len(definition.Tuple) != 2 || !ownerRecycleStorageType(metadata, definition.Tuple[0], "u16", depth+1) {
			return false
		}
		last := "u16"
		if kind == "netuid-account" {
			last = "account"
		}
		return ownerRecycleStorageType(metadata, definition.Tuple[1], last, depth+1)
	case "mode":
		if !definition.IsVariant || len(definition.Variant.Variants) != 2 {
			return false
		}
		seen := map[string]bool{}
		for _, variant := range definition.Variant.Variants {
			name := string(variant.Name)
			if seen[name] || len(variant.Fields) != 0 || !(name == "Burn" && variant.Index == 0 || name == "Recycle" && variant.Index == 1) {
				return false
			}
			seen[name] = true
		}
		return true
	}
	return false
}
