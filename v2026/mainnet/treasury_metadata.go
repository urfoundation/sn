// The pinned runtime metadata must describe the exact source-reviewed native
// multisig, registration, liquidation and balance-transfer encodings.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Newtype wrappers are bounded; semantic field names are checked where encoded.
func treasuryType(metadata *types.Metadata, id types.Si1LookupTypeID, shape string, depth int) bool {
	if depth > 16 {
		return false
	}
	entry := metadata.AsMetadataV14.EfficientLookup[id.Int64()]
	if entry == nil {
		return false
	}
	def := entry.Def
	if def.IsComposite && len(def.Composite.Fields) == 1 {
		return treasuryType(metadata, def.Composite.Fields[0].Type, shape, depth+1)
	}
	switch shape {
	case "u128":
		return def.IsPrimitive && def.Primitive.Si0TypeDefPrimitive == types.IsU128
	case "timepoint", "weight", "pending":
		names := map[string][]string{"timepoint": {"height", "index"}, "weight": {"ref_time", "proof_size"}, "pending": {"when", "deposit", "depositor", "approvals"}}[shape]
		shapes := map[string][]string{"timepoint": {"u32", "u32"}, "weight": {"compact64", "compact64"}, "pending": {"timepoint", "u64", "account", "accounts"}}[shape]
		if !def.IsComposite || len(def.Composite.Fields) != len(names) {
			return false
		}
		for i, field := range def.Composite.Fields {
			if !field.HasName || string(field.Name) != names[i] || !treasuryType(metadata, field.Type, shapes[i], depth+1) {
				return false
			}
		}
		return true
	case "optional-timepoint":
		if !def.IsVariant || len(def.Variant.Variants) != 2 {
			return false
		}
		a, b := def.Variant.Variants[0], def.Variant.Variants[1]
		return a.Name == "None" && a.Index == 0 && len(a.Fields) == 0 && b.Name == "Some" && b.Index == 1 && len(b.Fields) == 1 && treasuryType(metadata, b.Fields[0].Type, "timepoint", depth+1)
	case "address":
		if !def.IsVariant {
			return false
		}
		matches := 0
		for _, v := range def.Variant.Variants {
			if v.Name == "Id" {
				if v.Index != 0 || len(v.Fields) != 1 || !rootTypeMatches(metadata, v.Fields[0].Type, "account", 0) {
					return false
				}
				matches++
			}
		}
		return matches == 1
	case "runtime-call":
		if !def.IsVariant {
			return false
		}
		for _, pallet := range metadata.AsMetadataV14.Pallets {
			if pallet.Name == "SubtensorModule" {
				for _, v := range def.Variant.Variants {
					if v.Name == pallet.Name && v.Index == pallet.Index && len(v.Fields) == 1 && v.Fields[0].Type.Int64() == pallet.Calls.Type.Int64() {
						return true
					}
				}
			}
		}
		return false
	}
	return rootSigningType(metadata, id, shape, 0)
}

// Pallet, call index, names and argument layouts must all match pinned source.
func treasuryCallProfile(metadata *types.Metadata, palletName, callName string, palletIndex, callIndex byte, names, shapes []string) error {
	pallets, calls := 0, 0
	indices := map[byte]bool{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if indices[byte(pallet.Index)] {
			return errors.New("treasury metadata has duplicate pallet index")
		}
		indices[byte(pallet.Index)] = true
		if string(pallet.Name) != palletName {
			continue
		}
		pallets++
		entry := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		if byte(pallet.Index) != palletIndex || !pallet.HasCalls || entry == nil || !entry.Def.IsVariant {
			return errors.New("treasury call pallet profile changed")
		}
		seen := map[byte]bool{}
		for _, variant := range entry.Def.Variant.Variants {
			if seen[byte(variant.Index)] {
				return errors.New("treasury duplicate call index")
			}
			seen[byte(variant.Index)] = true
			if string(variant.Name) != callName {
				continue
			}
			calls++
			if byte(variant.Index) != callIndex || len(variant.Fields) != len(names) {
				return errors.New("treasury call argument count or index changed")
			}
			for i, field := range variant.Fields {
				if !field.HasName || string(field.Name) != names[i] || !treasuryType(metadata, field.Type, shapes[i], 0) {
					return fmt.Errorf("treasury %s.%s field %s changed", palletName, callName, names[i])
				}
			}
		}
	}
	if pallets != 1 || calls != 1 {
		return errors.New("treasury call is absent or duplicated")
	}
	return nil
}

// Multisig storage and deposit constants are interpreted only after metadata
// authentication; a self-reported pending count supplies no threshold authority.
func treasuryMetadataProfile(metadata *types.Metadata, action treasuryAction) (uint64, error) {
	if err := action.Descriptor.validate(); err != nil {
		return 0, err
	}
	if err := nativeSigningProfileForSignature(metadata, "Ed25519", 0); err != nil {
		return 0, err
	}
	name, idx := action.Operation, byte(1)
	names := []string{"threshold", "other_signatories", "maybe_timepoint", "call", "max_weight"}
	shapes := []string{"u16", "accounts", "optional-timepoint", "runtime-call", "weight"}
	if name == "approve_as_multi" {
		idx = 2
		names[3] = "call_hash"
		shapes[3] = "account"
	}
	if name == "cancel_as_multi" {
		idx = 3
		names = []string{"threshold", "other_signatories", "timepoint", "call_hash"}
		shapes = []string{"u16", "accounts", "timepoint", "account"}
	}
	if err := treasuryCallProfile(metadata, "Multisig", name, 13, idx, names, shapes); err != nil {
		return 0, err
	}
	switch action.Inner.Kind {
	case "register_limit":
		name = "register_limit"
		idx = 134
		names = []string{"netuid", "hotkey", "limit_price"}
		shapes = []string{"u16", "account", "u64"}
	case "remove_stake_limit":
		name = "remove_stake_limit"
		idx = 89
		names = []string{"hotkey", "netuid", "amount_unstaked", "limit_price", "allow_partial"}
		shapes = []string{"account", "u16", "u64", "u64", "bool"}
	case "transfer_keep_alive":
		if err := treasuryCallProfile(metadata, "Balances", "transfer_keep_alive", 5, 3, []string{"dest", "value"}, []string{"address", "compact64"}); err != nil {
			return 0, err
		}
		name = ""
	default:
		return 0, errors.New("treasury inner codec is unsupported")
	}
	if name != "" {
		if err := treasuryCallProfile(metadata, "SubtensorModule", name, 7, idx, names, shapes); err != nil {
			return 0, err
		}
	}
	entries := 0
	constants := map[string]uint64{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "Multisig" {
			continue
		}
		if !pallet.HasStorage || pallet.Storage.Prefix != "Multisig" {
			return 0, errors.New("treasury multisig storage prefix changed")
		}
		for _, entry := range pallet.Storage.Items {
			if entry.Name != "Multisigs" {
				continue
			}
			entries++
			if !entry.Modifier.IsOptional || entry.Modifier.IsDefault || !entry.Type.IsMap || len(entry.Type.AsMap.Hashers) != 2 || !entry.Type.AsMap.Hashers[0].IsTwox64Concat || !entry.Type.AsMap.Hashers[1].IsBlake2_128Concat || !treasuryType(metadata, entry.Type.AsMap.Value, "pending", 0) {
				return 0, errors.New("treasury pending multisig storage changed")
			}
			key := metadata.AsMetadataV14.EfficientLookup[entry.Type.AsMap.Key.Int64()]
			if key == nil || !key.Def.IsTuple || len(key.Def.Tuple) != 2 || !rootTypeMatches(metadata, key.Def.Tuple[0], "account", 0) || !rootTypeMatches(metadata, key.Def.Tuple[1], "account", 0) {
				return 0, errors.New("treasury multisig key tuple changed")
			}
		}
		for _, constant := range pallet.Constants {
			n := string(constant.Name)
			if n != "DepositBase" && n != "DepositFactor" && n != "MaxSignatories" {
				continue
			}
			if _, ok := constants[n]; ok {
				return 0, errors.New("treasury duplicate deposit constant")
			}
			shape, width := "u64", 8
			if n == "MaxSignatories" {
				shape, width = "u32", 4
			}
			if !rootTypeMatches(metadata, constant.Type, shape, 0) || len(constant.Value) != width {
				return 0, errors.New("treasury deposit constant shape changed")
			}
			if width == 8 {
				constants[n] = binary.LittleEndian.Uint64(constant.Value)
			} else {
				constants[n] = uint64(binary.LittleEndian.Uint32(constant.Value))
			}
		}
	}
	if entries != 1 || len(constants) != 3 || constants["MaxSignatories"] != 100 || constants["DepositFactor"] > (math.MaxUint64-constants["DepositBase"])/uint64(action.Descriptor.Multisig.Threshold) {
		return 0, errors.New("treasury deposit or pending storage profile is incomplete")
	}
	return constants["DepositBase"] + uint64(action.Descriptor.Multisig.Threshold)*constants["DepositFactor"], nil
}
