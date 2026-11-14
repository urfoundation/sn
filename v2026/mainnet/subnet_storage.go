// The subnet observer consumes an explicit bounded subset of authenticated
// runtime storage. Enumeration proves both UID mapping directions are complete.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Capacity, immunity and trim inputs are read from this runtime, not defaults
// copied from documentation. The owner's trim interval is not in metadata.
var subnetStorageSpecs = []rootStorageSpec{
	{name: "NetworksAdded", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
	{name: "SubnetworkN", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "MinAllowedUids", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "MaxAllowedUids", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "ImmunityPeriod", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "ImmuneOwnerUidsLimit", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "Keys", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "account"},
	{name: "Uids", keys: []string{"u16", "account"}, hashers: []string{"identity", "blake128concat"}, value: "u16", optional: true},
	{name: "Owner", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "account"},
	{name: "BlockAtRegistration", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "u64"},
	{name: "IsNetworkMember", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "bool"},
	{name: "SubnetOwner", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
	{name: "SubnetOwnerHotkey", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
	{name: "OwnedHotkeys", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "accounts"},
	{name: "NetworkRegisteredAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "RegisteredSubnetCounter", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "NetworkRegistrationAllowed", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
	{name: "NetworkPowRegistrationAllowed", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
	{name: "Active", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bools"},
	{name: "ValidatorPermit", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bools"},
	{name: "Emission", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64s"},
	{name: "Tempo", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "LastEpochBlock", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "PendingEpochAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "AdminFreezeWindow", value: "u16"},
	{name: "TransactionKeyLastBlock", keys: []string{"account", "u16", "u16"}, hashers: []string{"blake128concat", "identity", "identity"}, value: "u64"},
}

// No row is admitted from a default identity, a partial page or a reused UID.
func (self *rootStorageReader) subnetRegistrations(ctx context.Context, netuid uint16, finalizedNumber uint64) ([]subnetRegistration, uint16, error) {
	netuidArg := binary.LittleEndian.AppendUint16(nil, netuid)
	exists, err := self.read(ctx, "NetworksAdded", netuidArg)
	if err != nil {
		return nil, 0, err
	}
	if exists.data[0] != 1 {
		return nil, 0, fmt.Errorf("%w: subnet %d does not exist", errRpcIntegrity, netuid)
	}
	n, err := self.read(ctx, "SubnetworkN", netuidArg)
	if err != nil {
		return nil, 0, err
	}
	count := int(binary.LittleEndian.Uint16(n.data))
	capacity, err := self.read(ctx, "MaxAllowedUids", netuidArg)
	if err != nil {
		return nil, 0, err
	}
	maximum := binary.LittleEndian.Uint16(capacity.data)
	if count > rootCensusLimit || count > int(maximum) || maximum == 0 {
		return nil, 0, fmt.Errorf("%w: subnet %d exceeds the observed capacity or local 4096-seat census bound", errRpcIntegrity, netuid)
	}
	probe, err := types.CreateStorageKey(self.metadata, "SubtensorModule", "Keys", netuidArg, []byte{0, 0})
	if err != nil || len(probe) != 36 {
		return nil, 0, fmt.Errorf("%w: subnet Keys prefix shape changed", errRpcIntegrity)
	}
	keys, err := self.keys(ctx, probe[:34])
	if err != nil {
		return nil, 0, err
	}
	if len(keys) != count {
		return nil, 0, fmt.Errorf("%w: subnet %d Keys cardinality differs from SubnetworkN", errRpcIntegrity, netuid)
	}
	keyKVs := map[string]bool{}
	for _, key := range keys {
		keyKVs[key] = true
	}
	if self.batchRegistrations {
		if err := self.prepareSubnetRegistrations(ctx, netuid, count, keyKVs); err != nil {
			return nil, 0, err
		}
	}
	registrations := make([]subnetRegistration, count)
	err = rootReadParallel(ctx, count, func(readCtx context.Context, index int) error {
		uidArg := binary.LittleEndian.AppendUint16(nil, uint16(index))
		key, keyErr := types.CreateStorageKey(self.metadata, "SubtensorModule", "Keys", netuidArg, uidArg)
		if keyErr != nil || !keyKVs[key.Hex()] {
			return fmt.Errorf("%w: subnet %d forward census has a hole or out-of-range UID", errRpcIntegrity, netuid)
		}
		hotkey, err := self.read(readCtx, "Keys", netuidArg, uidArg)
		if err != nil {
			return err
		}
		if hotkey.RawStorage == nil || !subnetAccountValid(hotkey.EffectiveScale) {
			return fmt.Errorf("%w: subnet %d has no recorded nonzero hotkey", errRpcIntegrity, netuid)
		}
		uid, err := self.read(readCtx, "Uids", netuidArg, hotkey.data)
		if err != nil {
			return err
		}
		if uid.RawStorage == nil || binary.LittleEndian.Uint16(uid.data) != uint16(index) {
			return fmt.Errorf("%w: subnet %d forward/reverse UID mapping disagrees", errRpcIntegrity, netuid)
		}
		owner, err := self.read(readCtx, "Owner", hotkey.data)
		if err != nil {
			return err
		}
		if owner.RawStorage == nil || !subnetAccountValid(owner.EffectiveScale) {
			return fmt.Errorf("%w: subnet %d hotkey has no recorded nonzero owner", errRpcIntegrity, netuid)
		}
		generation, err := self.read(readCtx, "BlockAtRegistration", netuidArg, uidArg)
		if err != nil {
			return err
		}
		if generation.RawStorage == nil || binary.LittleEndian.Uint64(generation.data) > finalizedNumber {
			return fmt.Errorf("%w: subnet %d registration generation is missing or in the future", errRpcIntegrity, netuid)
		}
		membership, err := self.read(readCtx, "IsNetworkMember", hotkey.data, netuidArg)
		if err != nil {
			return err
		}
		if membership.RawStorage == nil || membership.data[0] != 1 {
			return fmt.Errorf("%w: subnet %d membership flag contradicts UID mappings", errRpcIntegrity, netuid)
		}
		registrations[index] = subnetRegistration{Uid: uint16(index), Hotkey: hotkey.EffectiveScale, Coldkey: owner.EffectiveScale, RegistrationBlock: binary.LittleEndian.Uint64(generation.data)}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	reverseProbe, err := types.CreateStorageKey(self.metadata, "SubtensorModule", "Uids", netuidArg, make([]byte, 32))
	if err != nil || len(reverseProbe) != 82 {
		return nil, 0, fmt.Errorf("%w: subnet Uids prefix shape changed", errRpcIntegrity)
	}
	reverseKeys, err := self.keys(ctx, reverseProbe[:34])
	if err != nil {
		return nil, 0, err
	}
	if len(reverseKeys) != count {
		return nil, 0, fmt.Errorf("%w: subnet %d Uids cardinality differs from SubnetworkN", errRpcIntegrity, netuid)
	}
	expectedReverseKVs := map[string]bool{}
	for _, registration := range registrations {
		hotkey, _ := hex.DecodeString(registration.Hotkey[2:])
		key, keyErr := types.CreateStorageKey(self.metadata, "SubtensorModule", "Uids", netuidArg, hotkey)
		if keyErr != nil {
			return nil, 0, keyErr
		}
		expectedReverseKVs[key.Hex()] = true
	}
	for _, key := range reverseKeys {
		if !expectedReverseKVs[key] {
			return nil, 0, fmt.Errorf("%w: subnet %d reverse census has a foreign or malformed identity", errRpcIntegrity, netuid)
		}
	}
	return registrations, maximum, nil
}

// Authenticated metadata supplies the immunity threshold; no nominal 80% default.
func subnetMaximumImmunePercentage(metadata *types.Metadata) (uint8, error) {
	if metadata == nil || metadata.Version != 14 {
		return 0, errors.New("immune percentage requires authenticated metadata14")
	}
	var found *uint8
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "SubtensorModule" {
			continue
		}
		for _, constant := range pallet.Constants {
			if constant.Name == "MaxImmuneUidsPercentage" {
				if found != nil || !rootTypeMatches(metadata, constant.Type, "u8", 0) || len(constant.Value) != 1 || constant.Value[0] == 0 || constant.Value[0] > 100 {
					return 0, errors.New("unrecognized or duplicate immune percentage constant")
				}
				value := uint8(constant.Value[0])
				found = &value
			}
		}
	}
	if found == nil {
		return 0, errors.New("immune percentage constant is absent")
	}
	return *found, nil
}

// Only the reviewed two-u16 call shape is recognized; metadata does not prove
// origin semantics or source-to-Wasm provenance and supplies no signer.
func subnetOwnerTrimCall(metadata *types.Metadata) (*subnetTrimCall, error) {
	if metadata == nil || metadata.Version != 14 {
		return nil, errors.New("trim call requires authenticated metadata14")
	}
	var result *subnetTrimCall
	palletCount := 0
	palletIndices := map[uint8]bool{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if palletIndices[uint8(pallet.Index)] {
			return nil, errors.New("duplicate pallet index in trim capability metadata")
		}
		palletIndices[uint8(pallet.Index)] = true
		if pallet.Name != "AdminUtils" {
			continue
		}
		palletCount++
		callType := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		if !pallet.HasCalls || callType == nil || !callType.Def.IsVariant {
			return nil, errors.New("admin call variants are absent")
		}
		indices := map[uint8]bool{}
		for _, variant := range callType.Def.Variant.Variants {
			if indices[uint8(variant.Index)] {
				return nil, errors.New("duplicate admin call index")
			}
			indices[uint8(variant.Index)] = true
			if variant.Name != "sudo_trim_to_max_allowed_uids" {
				continue
			}
			if result != nil || len(variant.Fields) != 2 {
				return nil, errors.New("ambiguous or changed trim call")
			}
			for index, name := range []string{"netuid", "max_n"} {
				field := variant.Fields[index]
				if !field.HasName || string(field.Name) != name || !rootTypeMatches(metadata, field.Type, "u16", 0) {
					return nil, errors.New("trim call argument encoding changed")
				}
			}
			result = &subnetTrimCall{Pallet: "AdminUtils", Call: string(variant.Name), PalletIndex: uint8(pallet.Index), CallIndex: uint8(variant.Index), SourceOrigin: "subnet-owner-or-chain-root; owner rate limit and admin window apply under separately reviewed matching runtime source"}
		}
	}
	if palletCount != 1 || result == nil {
		return nil, errors.New("trim call is missing or admin pallet is duplicated")
	}
	return result, nil
}
