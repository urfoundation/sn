// Root observations decode only an authenticated, bounded storage profile.
// Metadata selects keys and defaults; absent identity rows never become seats.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const rootCensusLimit = 4096
const rootReadConcurrency = 8
const observationCensusKeyBytes = 32 + 2 + 16 + 32

// Shapes describe SCALE encoding, including transparent runtime newtypes.
type rootStorageSpec struct {
	name     string
	keys     []string
	hashers  []string
	value    string
	optional bool
}

// This profile covers observation only, not root registration or weight calls.
var rootStorageSpecs = []rootStorageSpec{
	{name: "NetworksAdded", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
	{name: "SubnetworkN", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "MaxAllowedUids", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "ImmunityPeriod", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "Keys", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "account"},
	{name: "Uids", keys: []string{"u16", "account"}, hashers: []string{"identity", "blake128concat"}, value: "u16", optional: true},
	{name: "Owner", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "account"},
	{name: "BlockAtRegistration", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "u64"},
	{name: "TotalHotkeyAlpha", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "u64"},
	{name: "Delegates", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "u16"},
	{name: "AutoParentDelegationEnabled", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "bool"},
	{name: "ChildKeys", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "links"},
	{name: "ParentKeys", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "links"},
	{name: "PendingChildKeys", keys: []string{"u16", "account"}, hashers: []string{"identity", "blake128concat"}, value: "pending-links"},
	{name: "Weights", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "weights"},
	{name: "LastUpdate", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64s"},
	{name: "RootWeightSettingEnabled", value: "bool"},
	{name: "RootWeightsCap", keys: []string{"u16"}, hashers: []string{"blake128concat"}, value: "u16"},
	{name: "StakeThreshold", value: "u64"},
	{name: "TaoWeight", value: "u64"},
	{name: "WeightsSetRateLimit", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "RootStakeUnlockInterval", value: "u64"},
	{name: "BasketShares", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "u64"},
	{name: "BasketRate", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "i128"},
	{name: "BasketClaimed", keys: []string{"account", "account"}, hashers: []string{"blake128concat", "blake128concat"}, value: "i128"},
}

// Compares wire shapes recursively with a hard depth bound on newtype chains.
func rootTypeMatches(metadata *types.Metadata, id types.Si1LookupTypeID, shape string, depth int) bool {
	if depth > 16 {
		return false
	}
	entry := metadata.AsMetadataV14.EfficientLookup[id.Int64()]
	if entry == nil {
		return false
	}
	def := entry.Def
	if def.IsComposite && len(def.Composite.Fields) == 1 {
		return rootTypeMatches(metadata, def.Composite.Fields[0].Type, shape, depth+1)
	}
	primitiveKVs := map[string]types.Si0TypeDefPrimitive{"bool": types.IsBool, "u8": types.IsU8, "u16": types.IsU16, "u32": types.IsU32, "u64": types.IsU64, "i128": types.IsI128}
	if primitive, ok := primitiveKVs[shape]; ok {
		return def.IsPrimitive && def.Primitive.Si0TypeDefPrimitive == primitive
	}
	switch shape {
	case "account":
		return def.IsArray && def.Array.Len == 32 && rootTypeMatches(metadata, def.Array.Type, "u8", depth+1)
	case "links", "weights", "u64s", "bools", "accounts":
		itemShape := map[string]string{"links": "link", "weights": "weight", "u64s": "u64", "bools": "bool", "accounts": "account"}[shape]
		return def.IsSequence && rootTypeMatches(metadata, def.Sequence.Type, itemShape, depth+1)
	case "link", "weight", "pending-links", "coldkey-announcement":
		parts := map[string][]string{"link": {"u64", "account"}, "weight": {"u16", "u16"}, "pending-links": {"links", "u64"}, "coldkey-announcement": {"u32", "account"}}[shape]
		if !def.IsTuple || len(def.Tuple) != len(parts) {
			return false
		}
		for index, part := range parts {
			if !rootTypeMatches(metadata, def.Tuple[index], part, depth+1) {
				return false
			}
		}
		return true
	}
	return false
}

// Rejects duplicate pallets/items and changes to key, query or value encoding.
func rootStorageProfile(metadata *types.Metadata) (map[string]types.StorageEntryMetadataV14, error) {
	return observationStorageProfile(metadata, rootStorageSpecs)
}

// Each observer supplies the complete set of wire shapes it is allowed to read.
func observationStorageProfile(metadata *types.Metadata, specs []rootStorageSpec) (map[string]types.StorageEntryMetadataV14, error) {
	if metadata == nil || metadata.Version != 14 {
		return nil, errors.New("root observation requires authenticated metadata14")
	}
	entryKVs := map[string]types.StorageEntryMetadataV14{}
	palletCount := 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "SubtensorModule" {
			continue
		}
		palletCount++
		if !pallet.HasStorage || pallet.Storage.Prefix != "SubtensorModule" {
			return nil, errors.New("root storage prefix changed")
		}
		for _, entry := range pallet.Storage.Items {
			if _, exists := entryKVs[string(entry.Name)]; exists {
				return nil, fmt.Errorf("duplicate root storage entry %s", entry.Name)
			}
			entryKVs[string(entry.Name)] = entry
		}
	}
	if palletCount != 1 {
		return nil, errors.New("root storage pallet is missing or duplicated")
	}
	for _, spec := range specs {
		entry, exists := entryKVs[spec.name]
		if !exists || entry.Modifier.IsOptional != spec.optional || entry.Modifier.IsDefault == spec.optional {
			return nil, fmt.Errorf("root %s storage query changed", spec.name)
		}
		valueId := entry.Type.AsPlainType
		if len(spec.keys) == 0 {
			if !entry.Type.IsPlainType || entry.Type.IsMap {
				return nil, fmt.Errorf("root %s is not a plain value", spec.name)
			}
		} else {
			if !entry.Type.IsMap || entry.Type.IsPlainType || len(entry.Type.AsMap.Hashers) != len(spec.keys) {
				return nil, fmt.Errorf("root %s map shape changed", spec.name)
			}
			mapType := entry.Type.AsMap
			keyIds := []types.Si1LookupTypeID{mapType.Key}
			if len(spec.keys) > 1 {
				keyType := metadata.AsMetadataV14.EfficientLookup[mapType.Key.Int64()]
				if keyType == nil || !keyType.Def.IsTuple {
					return nil, fmt.Errorf("root %s key tuple changed", spec.name)
				}
				keyIds = keyType.Def.Tuple
			}
			if len(keyIds) != len(spec.keys) {
				return nil, fmt.Errorf("root %s key count changed", spec.name)
			}
			for index, shape := range spec.keys {
				hasher := mapType.Hashers[index]
				matchesHasher := spec.hashers[index] == "identity" && hasher.IsIdentity ||
					spec.hashers[index] == "blake128concat" && hasher.IsBlake2_128Concat ||
					spec.hashers[index] == "twox64concat" && hasher.IsTwox64Concat
				if !rootTypeMatches(metadata, keyIds[index], shape, 0) || !matchesHasher {
					return nil, fmt.Errorf("root %s key encoding changed", spec.name)
				}
			}
			valueId = mapType.Value
		}
		if !rootTypeMatches(metadata, valueId, spec.value, 0) {
			return nil, fmt.Errorf("root %s value encoding changed", spec.name)
		}
		if !spec.optional {
			if err := rootValidateScale(entry.Fallback, spec.value); err != nil {
				return nil, fmt.Errorf("root %s metadata default: %w", spec.name, err)
			}
		}
	}
	return entryKVs, nil
}

// Decodes only canonical compact vector lengths within the local census bound.
func rootVector(data []byte, width, suffix int) ([]byte, int, error) {
	if len(data) == 0 {
		return nil, 0, errors.New("empty SCALE vector")
	}
	count, prefix := uint64(data[0]>>2), 1
	switch data[0] & 3 {
	case 1:
		if len(data) < 2 {
			return nil, 0, errors.New("truncated SCALE count")
		}
		count, prefix = uint64(binary.LittleEndian.Uint16(data[:2])>>2), 2
		if count < 64 {
			return nil, 0, errors.New("noncanonical SCALE count")
		}
	case 2:
		if len(data) < 4 {
			return nil, 0, errors.New("truncated SCALE count")
		}
		count, prefix = uint64(binary.LittleEndian.Uint32(data[:4])>>2), 4
		if count < 16384 {
			return nil, 0, errors.New("noncanonical SCALE count")
		}
	case 3:
		return nil, 0, errors.New("SCALE count exceeds root census bound")
	}
	if count > rootCensusLimit || len(data) != prefix+int(count)*width+suffix {
		return nil, 0, errors.New("root SCALE vector is truncated, overlong or over census bound")
	}
	return data[prefix : len(data)-suffix], int(count), nil
}

// Fixed widths and full consumption prevent trailing or malicious SCALE data.
func rootValidateScale(data []byte, shape string) error {
	widthKVs := map[string]int{"bool": 1, "u8": 1, "u16": 2, "u32": 4, "u64": 8, "i128": 16, "account": 32, "coldkey-announcement": 36}
	if width, ok := widthKVs[shape]; ok {
		if len(data) != width || shape == "bool" && data[0] > 1 {
			return fmt.Errorf("invalid exact %s", shape)
		}
		return nil
	}
	width, suffix := 0, 0
	switch shape {
	case "weights":
		width = 4
	case "u64s":
		width = 8
	case "bools":
		width = 1
	case "accounts":
		width = 32
	case "links":
		width = 40
	case "pending-links":
		width, suffix = 40, 8
	default:
		return errors.New("unsupported root SCALE shape")
	}
	body, _, err := rootVector(data, width, suffix)
	if err == nil && shape == "bools" {
		for _, value := range body {
			if value > 1 {
				return errors.New("invalid SCALE boolean vector member")
			}
		}
	}
	return err
}

// Raw absence and authenticated defaults remain separate reviewable evidence.
type rootStorageValue struct {
	Name           string  `json:"name"`
	Key            string  `json:"key"`
	RawStorage     *string `json:"raw_storage"`
	EffectiveScale string  `json:"effective_scale,omitempty"`
	ValueSource    string  `json:"value_source"`
	data           []byte
}

// One sample owns its immutable metadata and synchronized evidence map.
type rootStorageReader struct {
	client    *rpcClient
	metadata  *types.Metadata
	entries   map[string]types.StorageEntryMetadataV14
	specs     []rootStorageSpec
	block     string
	stateLock sync.Mutex
	valueKVs  map[string]rootStorageValue
	// Discovery alone stages bounded exact-key replies, consumed once so later
	// overlapping reads still compare independent observations at the same hash.
	batchRegistrations bool
	preparedKVs        map[string]*string
}

// A query never substitutes latest state or another route after a failed read.
func (self *rootStorageReader) read(ctx context.Context, name string, args ...[]byte) (rootStorageValue, error) {
	specs := self.specs
	if specs == nil {
		specs = rootStorageSpecs
	}
	shape := ""
	for _, spec := range specs {
		if spec.name == name {
			shape = spec.value
			break
		}
	}
	entry, exists := self.entries[name]
	if shape == "" || !exists {
		return rootStorageValue{}, fmt.Errorf("%w: %s is outside the authenticated observation profile", errRpcIntegrity, name)
	}
	key, err := types.CreateStorageKey(self.metadata, "SubtensorModule", name, args...)
	if err != nil {
		return rootStorageValue{}, err
	}
	var raw *string
	self.stateLock.Lock()
	raw, prepared := self.preparedKVs[key.Hex()]
	if prepared {
		delete(self.preparedKVs, key.Hex())
	}
	self.stateLock.Unlock()
	if !prepared {
		if err := self.client.callWithStorageAbsence(ctx, "state_getStorage", []any{key.Hex(), self.block}, &raw, true); err != nil {
			return rootStorageValue{}, fmt.Errorf("root %s: %w", name, err)
		}
	}
	value := rootStorageValue{Name: name, Key: key.Hex(), RawStorage: raw, ValueSource: "absent-optional"}
	if raw != nil {
		if !strings.HasPrefix(*raw, "0x") || len(*raw) > 2+2*(4+rootCensusLimit*40+8) {
			return value, fmt.Errorf("%w: root %s storage is not bounded hex", errRpcIntegrity, name)
		}
		value.data, err = hex.DecodeString((*raw)[2:])
		value.ValueSource = "finalized-storage"
	} else if entry.Modifier.IsDefault {
		value.data = append([]byte(nil), entry.Fallback...)
		value.ValueSource = "authenticated-metadata-fallback"
	}
	if err != nil {
		return value, fmt.Errorf("%w: root %s hex: %v", errRpcIntegrity, name, err)
	}
	if raw != nil || entry.Modifier.IsDefault {
		if err := rootValidateScale(value.data, shape); err != nil {
			return value, fmt.Errorf("%w: root %s: %v", errRpcIntegrity, name, err)
		}
		value.EffectiveScale = "0x" + hex.EncodeToString(value.data)
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if prior, exists := self.valueKVs[value.Key]; exists && (prior.EffectiveScale != value.EffectiveScale || (prior.RawStorage == nil) != (value.RawStorage == nil)) {
		return value, fmt.Errorf("%w: root storage changed at one finalized hash", errRpcIntegrity)
	}
	self.valueKVs[value.Key] = value
	return value, nil
}

// Paged enumeration is monotonic, prefix-bound and exact-block. Always request
// the terminal empty page rather than guessing completion from a short page.
func (self *rootStorageReader) keys(ctx context.Context, prefix []byte) ([]string, error) {
	keys := []string{}
	var start any
	for {
		var page []string
		if err := self.client.call(ctx, "state_getKeysPaged", []any{"0x" + hex.EncodeToString(prefix), 128, start, self.block}, &page); err != nil {
			return nil, err
		}
		if len(page) > 128 || len(keys)+len(page) > rootCensusLimit {
			return nil, fmt.Errorf("%w: root key census exceeds bounded page or total", errRpcIntegrity)
		}
		if len(page) == 0 {
			return keys, nil
		}
		for _, key := range page {
			if !strings.HasPrefix(key, "0x") || len(key) > 2+2*observationCensusKeyBytes {
				return nil, fmt.Errorf("%w: malformed root census key", errRpcIntegrity)
			}
			raw, err := hex.DecodeString(key[2:])
			if err != nil || !bytes.HasPrefix(raw, prefix) || (len(keys) > 0 && strings.ToLower(key) <= keys[len(keys)-1]) {
				return nil, fmt.Errorf("%w: duplicate, unordered or foreign root census key", errRpcIntegrity)
			}
			keys = append(keys, strings.ToLower(key))
		}
		start = keys[len(keys)-1]
	}
}

// Each sample has eight workers at most; every worker joins on cancellation or
// error. Independent reads share a deadline and never retry completed work.
func rootReadParallel(ctx context.Context, count int, read func(context.Context, int) error) error {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	work := make(chan int)
	errorsByIndex := make([]error, count)
	var workers sync.WaitGroup
	for worker := 0; worker < min(count, rootReadConcurrency); worker++ {
		workers.Go(func() {
			for index := range work {
				if err := read(readCtx, index); err != nil {
					errorsByIndex[index] = err
					cancel()
				}
			}
		})
	}
	send := func() {
		defer close(work)
		for index := 0; index < count; index++ {
			select {
			case <-readCtx.Done():
				return
			case work <- index:
			}
		}
	}
	send()
	workers.Wait()
	return errors.Join(append(errorsByIndex, readCtx.Err())...)
}

// Evidence order is deterministic and independent of the read completion order.
func (self *rootStorageReader) evidence() []rootStorageValue {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	values := make([]rootStorageValue, 0, len(self.valueKVs))
	for _, value := range self.valueKVs {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Key < values[j].Key })
	return values
}
