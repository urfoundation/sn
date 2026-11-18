// Root receipt profiles bind independent runtime artifacts to the consumed
// native account, event and root-seat wire layouts. A node observation cannot
// add itself to this list; source-to-Wasm approval remains external authority.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Profiles have no implied mainnet values. The named source is the inspected
// codec semantics, not a claim that an observed Wasm was built from that source.
type rootReceiptProfile struct {
	RuntimeSourceCommit string                      `json:"runtime_source_commit"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
}

// Exactly one immutable runtime artifact supplies every interpretation at a hash.
type rootReceiptRuntime struct {
	profile  rootReceiptProfile
	metadata *types.Metadata
}

var errNativeMetadataPin = errors.New("native metadata differs from independently approved bounded canonical bytes")

// Untrusted SCALE lengths must never reach the decoder before authenticating
// the exact independently reviewed bytes. The hex allocation is bounded first.
func nativePinnedMetadata(encoded, expected string) (*types.Metadata, string, error) {
	raw, err := rootReceiptHex(encoded, maxMetadataRpcReplyBytes)
	if err != nil || !rootCanonicalHash(expected) || rootExtrinsicHash(raw) != expected {
		return nil, "", errNativeMetadataPin
	}
	return crv4.DecodeRuntimeMetadata(encoded)
}

// System storage keys must be selected from one exact pallet and item schema.
func rootSystemEntry(metadata *types.Metadata, name string) (types.StorageEntryMetadataV14, error) {
	var result types.StorageEntryMetadataV14
	palletCount, entryCount := 0, 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "System" {
			continue
		}
		palletCount++
		if !pallet.HasStorage || pallet.Storage.Prefix != "System" {
			return result, errors.New("root receipt System storage prefix changed")
		}
		for _, entry := range pallet.Storage.Items {
			if string(entry.Name) == name {
				result = entry
				entryCount++
			}
		}
	}
	if palletCount != 1 || entryCount != 1 {
		return result, fmt.Errorf("root receipt System.%s is missing or duplicated", name)
	}
	return result, nil
}

// Native Subtensor balance fields are u64, unlike generic Substrate AccountInfo.
// Validate field order/names and full shape before accepting its exact 56 bytes.
func rootAccountProfile(metadata *types.Metadata) error {
	entry, err := rootSystemEntry(metadata, "Account")
	if err != nil {
		return err
	}
	if !entry.Type.IsMap || !entry.Modifier.IsDefault || len(entry.Type.AsMap.Hashers) != 1 || !entry.Type.AsMap.Hashers[0].IsBlake2_128Concat || !rootTypeMatches(metadata, entry.Type.AsMap.Key, "account", 0) {
		return errors.New("root receipt System.Account key or query layout changed")
	}
	account := metadata.AsMetadataV14.EfficientLookup[entry.Type.AsMap.Value.Int64()]
	if account == nil || !account.Def.IsComposite || len(account.Def.Composite.Fields) != 5 {
		return errors.New("root receipt System.Account composite changed")
	}
	for index, name := range []string{"nonce", "consumers", "providers", "sufficients"} {
		field := account.Def.Composite.Fields[index]
		if string(field.Name) != name || !rootSigningType(metadata, field.Type, "u32", 0) {
			return errors.New("root receipt System.Account counters changed")
		}
	}
	dataField := account.Def.Composite.Fields[4]
	data := metadata.AsMetadataV14.EfficientLookup[dataField.Type.Int64()]
	if dataField.Name != "data" || data == nil || !data.Def.IsComposite || len(data.Def.Composite.Fields) != 4 {
		return errors.New("root receipt System.Account data changed")
	}
	for index, name := range []string{"free", "reserved", "frozen", "flags"} {
		field := data.Def.Composite.Fields[index]
		shape := rootTypeMatches(metadata, field.Type, "u64", 0)
		if index == 3 {
			typeEntry := metadata.AsMetadataV14.EfficientLookup[field.Type.Int64()]
			for depth := 0; typeEntry != nil && typeEntry.Def.IsComposite && len(typeEntry.Def.Composite.Fields) == 1 && depth < 16; depth++ {
				typeEntry = metadata.AsMetadataV14.EfficientLookup[typeEntry.Def.Composite.Fields[0].Type.Int64()]
			}
			shape = typeEntry != nil && typeEntry.Def.IsPrimitive && typeEntry.Def.Primitive.Si0TypeDefPrimitive == types.IsU128
		}
		if string(field.Name) != name || !shape {
			return errors.New("root receipt System.Account balance or flags layout changed")
		}
	}
	return nil
}

// Every historical state is addressed by hash. An execution runtime is read
// at its block's parent, including the block that itself installs new code.
func (self *rootCanonicalChain) runtimeAt(ctx context.Context, block string) (rootReceiptRuntime, error) {
	runtime, err := self.nativeRuntimeAt(ctx, block)
	if err != nil {
		return runtime, err
	}
	if _, err := rootSigningProfile(runtime.metadata); err != nil {
		return rootReceiptRuntime{}, err
	}
	if _, err := rootReceiptEvents(runtime.metadata); err != nil {
		return rootReceiptRuntime{}, err
	}
	return runtime, nil
}

// Authenticate the shared native envelope and financial events without granting
// a root or owner call profile. Cached artifact bytes never bypass these checks.
func (self *rootCanonicalChain) nativeRuntimeAt(ctx context.Context, block string) (rootReceiptRuntime, error) {
	runtime, err := self.authenticatedRuntimeAt(ctx, block)
	if err != nil {
		return rootReceiptRuntime{}, err
	}
	metadata := runtime.metadata
	if err := nativeSigningProfile(metadata); err != nil {
		return rootReceiptRuntime{}, err
	}
	if err := rootAccountProfile(metadata); err != nil {
		return rootReceiptRuntime{}, err
	}
	if _, err := nativeReceiptEvents(metadata, false); err != nil {
		return rootReceiptRuntime{}, err
	}
	entry, err := rootSystemEntry(metadata, "Events")
	if err != nil || !entry.Type.IsPlainType {
		return rootReceiptRuntime{}, errors.New("root receipt System.Events key schema changed")
	}
	return runtime, nil
}

// Only the full independently selected tuple authenticates immutable metadata.
// Consumers apply their own semantic purpose after this bounded raw-byte cache.
func (self *rootCanonicalChain) authenticatedRuntimeAt(ctx context.Context, block string) (rootReceiptRuntime, error) {
	var rawVersion json.RawMessage
	var codeHash string
	if err := self.client.call(ctx, "state_getRuntimeVersion", []any{block}, &rawVersion); err != nil {
		return rootReceiptRuntime{}, err
	}
	version, err := crv4.DecodeRuntimeVersionIdentity(rawVersion)
	if err != nil {
		return rootReceiptRuntime{}, err
	}
	var aliases struct {
		SystemVersion *uint8 `json:"systemVersion"`
	}
	if err := json.Unmarshal(rawVersion, &aliases); err != nil || aliases.SystemVersion != nil && *aliases.SystemVersion != version.StateVersion {
		return rootReceiptRuntime{}, errors.New("root receipt stateVersion/systemVersion aliases contradict")
	}
	if err := self.client.call(ctx, "state_getStorageHash", []any{"0x3a636f6465", block}, &codeHash); err != nil {
		return rootReceiptRuntime{}, err
	}
	for _, profile := range self.profiles {
		if profile.RuntimeVersion != version || profile.RuntimeCodeHash != codeHash {
			continue
		}
		// Cache immutable bytes by the entire approved tuple, never by spec alone.
		if runtime, exists := self.runtimeKVs[profile]; exists {
			index := slices.Index(self.runtimeOrder, profile)
			self.runtimeOrder = append(slices.Delete(self.runtimeOrder, index, index+1), profile)
			return runtime, nil
		}
		var encoded string
		if err := self.client.call(ctx, "state_getMetadata", []any{block}, &encoded); err != nil {
			return rootReceiptRuntime{}, err
		}
		metadata, digest, err := nativePinnedMetadata(encoded, profile.RuntimeMetadataHash)
		if err != nil || digest != profile.RuntimeMetadataHash {
			return rootReceiptRuntime{}, errors.Join(errors.New("root receipt metadata differs from independent artifact"), err)
		}
		runtime := rootReceiptRuntime{profile: profile, metadata: metadata}
		if len(self.runtimeOrder) >= self.runtimeCacheEntries {
			delete(self.runtimeKVs, self.runtimeOrder[0])
			self.runtimeOrder = self.runtimeOrder[1:]
		}
		self.runtimeKVs[profile] = runtime
		self.runtimeOrder = append(self.runtimeOrder, profile)
		return runtime, nil
	}
	return rootReceiptRuntime{}, errRootReceiptProfileUnavailable
}

// Hex reads are finite, exact-block and distinguish explicit null from errors.
func (self *rootCanonicalChain) storage(ctx context.Context, metadata *types.Metadata, pallet, name, block string, args ...[]byte) ([]byte, bool, error) {
	key, err := types.CreateStorageKey(metadata, pallet, name, args...)
	if err != nil {
		return nil, false, err
	}
	var raw *string
	if err := self.client.callBoundedRead(ctx, "state_getStorage", []any{key.Hex(), block}, &raw, true, 2*rootBodyBytesLimit+maxRpcReplyBytes); err != nil {
		return nil, false, err
	}
	if raw == nil {
		return nil, false, nil
	}
	data, err := rootReceiptHex(*raw, rootBodyBytesLimit)
	return data, true, err
}

// Canonical lowercase bytes prevent alternate spellings of retained evidence.
func rootReceiptHex(raw string, limit int) ([]byte, error) {
	if len(raw) < 2 || raw[:2] != "0x" || len(raw) > 2+2*limit {
		return nil, errors.New("root receipt hex exceeds bound or lacks prefix")
	}
	data, err := hex.DecodeString(raw[2:])
	if err != nil || raw != "0x"+hex.EncodeToString(data) {
		return nil, errors.New("root receipt hex is invalid or noncanonical")
	}
	return data, nil
}
