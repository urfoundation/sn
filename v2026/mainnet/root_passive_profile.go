// Current root funds accumulate dividends without a root weight vector. The
// passive observer validates that distinct capability before reading any state.
package main

import (
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// These fields belong only to the retired root-weight strategy. Non-root
// weights still exist, but cannot establish root eligibility or basket targets.
func rootRetiredWeightStorage(name string) bool {
	switch name {
	case "RootWeightSettingEnabled", "RootWeightsCap", "Weights", "LastUpdate", "WeightsSetRateLimit":
		return true
	default:
		return false
	}
}

// The new profile requires the retired call and gates to be absent. A changed
// runtime cannot silently regain mutable basket policy under passive approval.
func rootPassiveStorageMetadata(metadata *types.Metadata) (map[string]types.StorageEntryMetadataV14, error) {
	if metadata == nil || metadata.Version != 14 {
		return nil, errors.New("passive root requires authenticated metadata14")
	}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "SubtensorModule" {
			continue
		}
		if !pallet.HasCalls {
			return nil, errors.New("passive root call metadata is absent")
		}
		entry := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		if entry == nil || !entry.Def.IsVariant {
			return nil, errors.New("passive root call variants are absent")
		}
		for _, variant := range entry.Def.Variant.Variants {
			if variant.Name == "set_root_weights" {
				return nil, errors.New("passive root cannot adopt a runtime with a root-weight call")
			}
		}
		for _, item := range pallet.Storage.Items {
			if item.Name == "RootWeightSettingEnabled" || item.Name == "RootWeightsCap" {
				return nil, errors.New("passive root cannot adopt retired root-weight storage")
			}
		}
	}
	var specs []rootStorageSpec
	for _, spec := range rootStorageSpecs {
		if !rootRetiredWeightStorage(spec.name) {
			specs = append(specs, spec)
		}
	}
	return observationStorageProfile(metadata, specs)
}
