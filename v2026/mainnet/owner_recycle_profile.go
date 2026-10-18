// Recycle authority observations retain exact finalized storage and authenticate
// the reviewed call, enum, per-netuid rate key and state-based admin window.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/chain"
)

const ownerRecycleObservationSchema = "urnetwork-mainnet-owner-recycle-observation-v1"

// A checksum binds public facts, never supplies independent approval/finality.
type ownerRecycleObservation struct {
	Schema          string             `json:"schema"`
	PolicyHash      string             `json:"policy_hash"`
	FinalizedNumber uint64             `json:"finalized_number"`
	FinalizedHash   string             `json:"finalized_hash"`
	Owner           string             `json:"owner_account_id"`
	Storage         []rootStorageValue `json:"storage"`
	ContentHash     string             `json:"content_hash"`
}

// Derived facts come only from complete metadata-authenticated storage bytes.
type ownerRecycleWindow struct {
	Owner             string
	Nonce             uint32
	FreeRao           uint64
	RegistrationBlock uint64
	Mode              byte
	Tempo             uint16
	LastEpoch         uint64
	PendingEpoch      uint64
	Freeze            uint16
	RateEpochs        uint16
	LastUpdate        uint64
}

// The source short-circuits tempo0, uses saturating subtraction and permits the
// first hyperparameter update. Planning requires every possible era block open;
// later privileged changes still require independent enforcement before effects.
func (self ownerRecycleWindow) permits(block uint64) bool {
	limit := uint64(self.Tempo) * uint64(self.RateEpochs)
	if self.LastUpdate != 0 && (block < self.LastUpdate || block-self.LastUpdate < limit) {
		return false
	}
	if self.Tempo == 0 {
		return true
	}
	if self.PendingEpoch > block {
		return false
	}
	next := self.LastEpoch + uint64(self.Tempo)
	if next < self.LastEpoch {
		next = ^uint64(0)
	}
	remaining := uint64(0)
	if next > block {
		remaining = next - block
	}
	return remaining >= uint64(self.Freeze)
}

// Reuse the already reviewed storage enum and require the call argument to have
// that exact type; duplicate indices, fields or a different call name fail closed.
func ownerRecycleCall(metadata *types.Metadata) ([2]byte, error) {
	var result [2]byte
	if _, _, err := recycleModeStorage(metadata, 25); err != nil {
		return result, err
	}
	var enumId types.Si1LookupTypeID
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name == "SubtensorModule" {
			for _, entry := range pallet.Storage.Items {
				if entry.Name == "RecycleOrBurn" {
					enumId = entry.Type.AsMap.Value
				}
			}
		}
	}
	indices := map[uint8]bool{}
	found, pallets := 0, 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if indices[uint8(pallet.Index)] {
			return result, errors.New("recycle metadata repeats pallet index")
		}
		indices[uint8(pallet.Index)] = true
		if pallet.Name != "AdminUtils" {
			continue
		}
		pallets++
		callType := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		if !pallet.HasCalls || callType == nil || !callType.Def.IsVariant {
			return result, errors.New("recycle admin call variants missing")
		}
		callIndices := map[uint8]bool{}
		for _, variant := range callType.Def.Variant.Variants {
			if callIndices[uint8(variant.Index)] {
				return result, errors.New("recycle admin call index duplicated")
			}
			callIndices[uint8(variant.Index)] = true
			if variant.Name != "sudo_set_recycle_or_burn" {
				continue
			}
			found++
			if variant.Index != 80 || len(variant.Fields) != 2 || !variant.Fields[0].HasName || variant.Fields[0].Name != "netuid" || !rootTypeMatches(metadata, variant.Fields[0].Type, "u16", 0) || !variant.Fields[1].HasName || variant.Fields[1].Name != "recycle_or_burn" || variant.Fields[1].Type.Int64() != enumId.Int64() {
				return result, errors.New("recycle call differs from reviewed netuid/enum encoding")
			}
			result = [2]byte{uint8(pallet.Index), uint8(variant.Index)}
		}
	}
	if found != 1 || pallets != 1 {
		return result, errors.New("recycle call missing or duplicated")
	}
	return result, nil
}

// An immutable expected key/default/width profile for one owner and netuid25.
type ownerRecycleStorageEntry struct {
	key      string
	fallback []byte
	width    int
	required bool
}

// Validate the nested RateLimitKey::OwnerHyperparamUpdate(1)/RecycleOrBurn(24)
// shape before encoding 01 1900 18. This rate is per subnet, not per owner key.
func ownerRecycleRateEntry(metadata *types.Metadata) (types.StorageEntryMetadataV14, error) {
	var selected types.StorageEntryMetadataV14
	count := 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name == "SubtensorModule" {
			for _, entry := range pallet.Storage.Items {
				if entry.Name == "LastRateLimitedBlock" {
					selected = entry
					count++
				}
			}
		}
	}
	if count != 1 || !selected.Modifier.IsDefault || selected.Modifier.IsOptional || !selected.Type.IsMap || len(selected.Type.AsMap.Hashers) != 1 || !selected.Type.AsMap.Hashers[0].IsIdentity || !rootTypeMatches(metadata, selected.Type.AsMap.Value, "u64", 0) || !bytes.Equal(selected.Fallback, make([]byte, 8)) {
		return selected, errors.New("recycle rate-limit map changed")
	}
	keyType := metadata.AsMetadataV14.EfficientLookup[selected.Type.AsMap.Key.Int64()]
	if keyType == nil || !keyType.Def.IsVariant {
		return selected, errors.New("recycle rate-limit key enum missing")
	}
	found := 0
	seen := map[uint8]bool{}
	for _, variant := range keyType.Def.Variant.Variants {
		if seen[uint8(variant.Index)] {
			return selected, errors.New("duplicate recycle rate-key variant")
		}
		seen[uint8(variant.Index)] = true
		if variant.Name != "OwnerHyperparamUpdate" {
			continue
		}
		found++
		if variant.Index != 1 || len(variant.Fields) != 2 || !rootTypeMatches(metadata, variant.Fields[0].Type, "u16", 0) {
			return selected, errors.New("recycle rate-limit owner key changed")
		}
		hyper := metadata.AsMetadataV14.EfficientLookup[variant.Fields[1].Type.Int64()]
		if hyper == nil || !hyper.Def.IsVariant {
			return selected, errors.New("recycle hyperparameter enum missing")
		}
		matches := 0
		seenHyper := map[uint8]bool{}
		for _, parameter := range hyper.Def.Variant.Variants {
			if seenHyper[uint8(parameter.Index)] {
				return selected, errors.New("duplicate recycle hyperparameter variant")
			}
			seenHyper[uint8(parameter.Index)] = true
			if parameter.Name == "RecycleOrBurn" {
				if parameter.Index != 24 || len(parameter.Fields) != 0 {
					return selected, errors.New("recycle hyperparameter encoding changed")
				}
				matches++
			}
		}
		if matches != 1 {
			return selected, errors.New("recycle hyperparameter absent or duplicated")
		}
	}
	if found != 1 {
		return selected, errors.New("recycle owner rate-key absent or duplicated")
	}
	return selected, nil
}

// All consumed defaults are authenticated before an absent row is interpreted.
func ownerRecycleEntries(metadata *types.Metadata, owner string) (map[string]ownerRecycleStorageEntry, error) {
	if !rootCanonicalHash(owner) {
		return nil, errors.New("recycle owner is not canonical AccountId32")
	}
	if _, err := ownerRecycleCall(metadata); err != nil {
		return nil, err
	}
	specs := []rootStorageSpec{
		{name: "NetworksAdded", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
		{name: "SubnetOwner", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
		{name: "NetworkRegisteredAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "Tempo", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "LastEpochBlock", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "PendingEpochAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "AdminFreezeWindow", value: "u16"}, {name: "OwnerHyperparamRateLimit", value: "u16"},
	}
	entries, err := observationStorageProfile(metadata, specs)
	if err != nil {
		return nil, err
	}
	result := map[string]ownerRecycleStorageEntry{}
	for _, spec := range specs {
		var args [][]byte
		if len(spec.keys) != 0 {
			args = [][]byte{chain.NetuidArg(25)}
		}
		key, err := types.CreateStorageKey(metadata, "SubtensorModule", spec.name, args...)
		if err != nil {
			return nil, err
		}
		width := map[string]int{"bool": 1, "account": 32, "u16": 2, "u64": 8}[spec.value]
		result[spec.name] = ownerRecycleStorageEntry{key: key.Hex(), fallback: entries[spec.name].Fallback, width: width, required: spec.name == "SubnetOwner" || spec.name == "NetworkRegisteredAt"}
	}
	rate, err := ownerRecycleRateEntry(metadata)
	if err != nil {
		return nil, err
	}
	rateKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "LastRateLimitedBlock", []byte{1, 25, 0, 24})
	if err != nil {
		return nil, err
	}
	result["LastRateLimitedBlock"] = ownerRecycleStorageEntry{key: rateKey.Hex(), fallback: rate.Fallback, width: 8}
	modeKey, fallback, err := recycleModeStorage(metadata, 25)
	if err != nil {
		return nil, err
	}
	result["RecycleOrBurn"] = ownerRecycleStorageEntry{key: modeKey.Hex(), fallback: fallback, width: 1}
	if err := rootAccountProfile(metadata); err != nil {
		return nil, err
	}
	account, _ := hex.DecodeString(owner[2:])
	accountKey, err := types.CreateStorageKey(metadata, "System", "Account", account)
	if err != nil {
		return nil, err
	}
	result["System.Account"] = ownerRecycleStorageEntry{key: accountKey.Hex(), width: 56, required: true}
	return result, nil
}

// Re-decode retained raw rows; attacker-authored derived fields cannot become
// an owner, nonce or successful mode simply by recomputing a JSON checksum.
func (self ownerRecycleObservation) window(policy recyclePolicy, metadata *types.Metadata) (ownerRecycleWindow, error) {
	var result ownerRecycleWindow
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != ownerRecycleObservationSchema || self.PolicyHash != rootObjectHash(policy) || claimed != rootObjectHash(self) || self.FinalizedNumber == 0 || !rootCanonicalHash(self.FinalizedHash) {
		return result, errors.New("recycle observation domain or seal changed")
	}
	entries, err := ownerRecycleEntries(metadata, self.Owner)
	if err != nil {
		return result, err
	}
	if len(self.Storage) != len(entries) {
		return result, errors.New("recycle observation storage is incomplete")
	}
	values := map[string][]byte{}
	for _, row := range self.Storage {
		entry, exists := entries[row.Name]
		if !exists || row.Key != entry.key || values[row.Name] != nil {
			return result, errors.New("recycle observation has wrong or duplicate storage key")
		}
		data, source := entry.fallback, "authenticated-metadata-fallback"
		if row.RawStorage != nil {
			data, err = rootReceiptHex(*row.RawStorage, 56)
			source = "finalized-storage"
		} else if entry.required {
			return result, errors.New("recycle owner/account/generation storage missing")
		}
		if err != nil || len(data) != entry.width || row.EffectiveScale != "0x"+hex.EncodeToString(data) || row.ValueSource != source {
			return result, errors.New("recycle raw storage and effective bytes disagree")
		}
		values[row.Name] = data
	}
	if values["NetworksAdded"][0] != 1 || values["RecycleOrBurn"][0] > 1 {
		return result, errors.New("recycle subnet absent or enum malformed")
	}
	result.Owner = "0x" + hex.EncodeToString(values["SubnetOwner"])
	if result.Owner != self.Owner {
		return result, errors.New("recycle observed account is not subnet owner")
	}
	result.Nonce = binary.LittleEndian.Uint32(values["System.Account"][:4])
	result.FreeRao = binary.LittleEndian.Uint64(values["System.Account"][16:24])
	result.RegistrationBlock = binary.LittleEndian.Uint64(values["NetworkRegisteredAt"])
	result.Mode = values["RecycleOrBurn"][0]
	result.Tempo = binary.LittleEndian.Uint16(values["Tempo"])
	result.LastEpoch = binary.LittleEndian.Uint64(values["LastEpochBlock"])
	result.PendingEpoch = binary.LittleEndian.Uint64(values["PendingEpochAt"])
	result.Freeze = binary.LittleEndian.Uint16(values["AdminFreezeWindow"])
	result.RateEpochs = binary.LittleEndian.Uint16(values["OwnerHyperparamRateLimit"])
	result.LastUpdate = binary.LittleEndian.Uint64(values["LastRateLimitedBlock"])
	if result.RegistrationBlock > self.FinalizedNumber || result.LastEpoch > self.FinalizedNumber || result.LastUpdate > self.FinalizedNumber {
		return result, errors.New("recycle storage claims future registration/epoch/update")
	}
	return result, nil
}

// One exact finalized hash supplies every row; the caller authenticates the
// network and canonical header/mapping before publishing this observation.
func (self *rootCanonicalChain) recycleObservationAt(ctx context.Context, policy recyclePolicy, owner, hash string, number uint64) (ownerRecycleObservation, error) {
	result := ownerRecycleObservation{Schema: ownerRecycleObservationSchema, PolicyHash: rootObjectHash(policy), FinalizedNumber: number, FinalizedHash: hash, Owner: owner}
	runtime, err := self.nativeRuntimeAt(ctx, hash)
	if err != nil {
		return ownerRecycleObservation{}, err
	}
	entries, err := ownerRecycleEntries(runtime.metadata, owner)
	if err != nil {
		return ownerRecycleObservation{}, err
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := entries[name]
		var raw *string
		if err := self.client.callWithStorageAbsence(ctx, "state_getStorage", []any{entry.key, hash}, &raw, true); err != nil {
			return ownerRecycleObservation{}, err
		}
		data, source := entry.fallback, "authenticated-metadata-fallback"
		if raw != nil {
			data, err = rootReceiptHex(*raw, 56)
			if err != nil {
				return ownerRecycleObservation{}, err
			}
			source = "finalized-storage"
		}
		result.Storage = append(result.Storage, rootStorageValue{Name: name, Key: entry.key, RawStorage: raw, EffectiveScale: "0x" + hex.EncodeToString(data), ValueSource: source})
	}
	result.ContentHash = rootObjectHash(result)
	if _, err := result.window(policy, runtime.metadata); err != nil {
		return ownerRecycleObservation{}, err
	}
	return result, nil
}
