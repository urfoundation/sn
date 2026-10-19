// Synthetic account and storage rows exercise the source473 admission rules.
// The public call/event graph supplies wire layouts, never production authority.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// This explicit synthetic storage graph is independently spelled out rather
// than taking the implementation's declared storage profile as its oracle.
func rootRegisterTestMetadata(t *testing.T) (string, *types.Metadata) {
	t.Helper()
	_, metadata := ownerRecycleTestMetadata(t)
	next := uint64(0)
	for id := range metadata.AsMetadataV14.EfficientLookup {
		if next <= uint64(id) {
			next = uint64(id) + 1
		}
	}
	add := func(def types.Si1TypeDef) types.Si1LookupTypeID {
		id := types.NewSi1LookupTypeIDFromUInt(next)
		next++
		value := types.Si1Type{Def: def}
		metadata.AsMetadataV14.EfficientLookup[id.Int64()] = &value
		metadata.AsMetadataV14.Lookup.Types = append(metadata.AsMetadataV14.Lookup.Types, types.PortableTypeV14{ID: id, Type: value})
		return id
	}
	ids := map[string]types.Si1LookupTypeID{}
	for _, primitive := range []struct {
		name string
		kind types.Si0TypeDefPrimitive
	}{
		{name: "u8", kind: types.IsU8}, {name: "u16", kind: types.IsU16},
		{name: "u64", kind: types.IsU64}, {name: "bool", kind: types.IsBool},
	} {
		ids[primitive.name] = add(types.Si1TypeDef{IsPrimitive: true, Primitive: types.Si1TypeDefPrimitive{Si0TypeDefPrimitive: primitive.kind}})
	}
	ids["account"] = add(types.Si1TypeDef{IsArray: true, Array: types.Si1TypeDefArray{Len: 32, Type: ids["u8"]}})
	ids["accounts"] = add(types.Si1TypeDef{IsSequence: true, Sequence: types.Si1TypeDefSequence{Type: ids["account"]}})
	entry := func(name string, keys, hashers []string, value string, optional bool) types.StorageEntryMetadataV14 {
		key := ids[keys[0]]
		if len(keys) > 1 {
			parts := make([]types.Si1LookupTypeID, len(keys))
			for index, shape := range keys {
				parts[index] = ids[shape]
			}
			key = add(types.Si1TypeDef{IsTuple: true, Tuple: parts})
		}
		hashes := make([]types.StorageHasherV10, len(hashers))
		for index, hasher := range hashers {
			if hasher == "identity" {
				hashes[index].IsIdentity = true
			} else {
				hashes[index].IsBlake2_128Concat = true
			}
		}
		fallback := make(types.Bytes, map[string]int{"bool": 1, "u16": 2, "u64": 8, "account": 32, "accounts": 1}[value])
		if optional {
			fallback = nil
		}
		return types.StorageEntryMetadataV14{Name: types.Text(name), Modifier: types.StorageFunctionModifierV0{IsDefault: !optional, IsOptional: optional}, Fallback: fallback, Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Key: key, Value: ids[value], Hashers: hashes}}}
	}
	for index := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[index]
		if pallet.Name == "SubtensorModule" {
			pallet.Storage.Items = nil
			for _, spec := range []struct{ name, shape string }{
				{name: "NetworksAdded", shape: "bool"}, {name: "SubnetOwner", shape: "account"},
				{name: "SubnetworkN", shape: "u16"}, {name: "MaxAllowedUids", shape: "u16"},
				{name: "ImmunityPeriod", shape: "u16"}, {name: "Burn", shape: "u64"},
				{name: "RegistrationsThisBlock", shape: "u16"}, {name: "MaxRegistrationsPerBlock", shape: "u16"},
				{name: "RegistrationsThisInterval", shape: "u16"}, {name: "TargetRegistrationsPerInterval", shape: "u16"},
			} {
				pallet.Storage.Items = append(pallet.Storage.Items, entry(spec.name, []string{"u16"}, []string{"identity"}, spec.shape, false))
			}
			pallet.Storage.Items = append(pallet.Storage.Items,
				entry("Keys", []string{"u16", "u16"}, []string{"identity", "identity"}, "account", false),
				entry("Uids", []string{"u16", "account"}, []string{"identity", "blake"}, "u16", true),
				entry("Owner", []string{"account"}, []string{"blake"}, "account", false),
				entry("BlockAtRegistration", []string{"u16", "u16"}, []string{"identity", "identity"}, "u64", false),
				entry("TotalHotkeyAlpha", []string{"account", "u16"}, []string{"blake", "identity"}, "u64", false),
				entry("Delegates", []string{"account"}, []string{"blake"}, "u16", false),
				entry("AutoParentDelegationEnabled", []string{"account"}, []string{"blake"}, "bool", false),
				entry("OwnedHotkeys", []string{"account"}, []string{"blake"}, "accounts", false),
				entry("StakingHotkeys", []string{"account"}, []string{"blake"}, "accounts", false),
			)
		}
		if pallet.Name == "Balances" {
			pallet.Constants = []types.ConstantMetadataV14{{Name: "ExistentialDeposit", Type: ids["u64"], Value: binary.LittleEndian.AppendUint64(nil, 500)}}
		}
	}
	raw, err := codec.Encode(metadata)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "0x" + hex.EncodeToString(raw)
	decoded, _, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, decoded
}

// Callers update their policy's metadata hash from the returned encoded bytes.
// The helper binds the same updated hash before sealing the synthetic observation.
func rootRegisterTestObservation(t *testing.T, policy rootRegisterPolicy, birth uint64, hash string, nonce uint32) (rootRegisterObservation, string, *types.Metadata) {
	t.Helper()
	encoded, metadata := rootRegisterTestMetadata(t)
	raw, _ := hex.DecodeString(encoded[2:])
	policy.RuntimeMetadataHash = rootExtrinsicHash(raw)
	observation := rootRegisterObservation{Schema: rootRegisterObservationSchema, PolicyHash: rootObjectHash(policy), FinalizedNumber: birth, FinalizedHash: hash, Keys: []string{}, UidKeys: []string{}}
	root := []byte{0, 0}
	hotkey, _ := hex.DecodeString(policy.Hotkey[2:])
	operator, _ := hex.DecodeString(policy.Operator[2:])
	owner, _ := hex.DecodeString(policy.SubnetOwner[2:])
	for _, row := range []struct {
		name string
		data []byte
		args [][]byte
	}{
		{name: "NetworksAdded", data: []byte{1}, args: [][]byte{root}},
		{name: "NetworksAdded", data: []byte{1}, args: [][]byte{{25, 0}}},
		{name: "SubnetOwner", data: owner, args: [][]byte{{25, 0}}},
		{name: "SubnetworkN", data: []byte{0, 0}, args: [][]byte{root}},
		{name: "MaxAllowedUids", data: []byte{3, 0}, args: [][]byte{root}},
		{name: "ImmunityPeriod", data: []byte{5, 0}, args: [][]byte{root}},
		{name: "Burn", data: binary.LittleEndian.AppendUint64(nil, 10), args: [][]byte{root}},
		{name: "RegistrationsThisBlock", data: []byte{0, 0}, args: [][]byte{root}},
		{name: "MaxRegistrationsPerBlock", data: []byte{3, 0}, args: [][]byte{root}},
		{name: "RegistrationsThisInterval", data: []byte{0, 0}, args: [][]byte{root}},
		{name: "TargetRegistrationsPerInterval", data: []byte{10, 0}, args: [][]byte{root}},
		{name: "Uids", args: [][]byte{root, hotkey}}, {name: "Owner", args: [][]byte{hotkey}},
		{name: "TotalHotkeyAlpha", data: make([]byte, 8), args: [][]byte{hotkey, root}},
		{name: "Delegates", args: [][]byte{hotkey}},
		{name: "AutoParentDelegationEnabled", data: []byte{1}, args: [][]byte{hotkey}},
		{name: "OwnedHotkeys", data: []byte{0}, args: [][]byte{operator}},
		{name: "StakingHotkeys", data: []byte{0}, args: [][]byte{operator}},
	} {
		rootRegisterTestSet(t, &observation, metadata, "SubtensorModule", row.name, row.data, row.args...)
	}
	account := make([]byte, 56)
	binary.LittleEndian.PutUint32(account, nonce)
	binary.LittleEndian.PutUint64(account[16:24], 100000)
	rootRegisterTestSet(t, &observation, metadata, "System", "Account", account, operator)
	return observation, encoded, metadata
}

// Replacing a raw row updates only its own exact key and recomputes public bytes.
func rootRegisterTestSet(t *testing.T, observation *rootRegisterObservation, metadata *types.Metadata, palletName, name string, data []byte, args ...[]byte) {
	t.Helper()
	key, err := types.CreateStorageKey(metadata, palletName, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	var entry types.StorageEntryMetadataV14
	found := false
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if string(pallet.Name) == palletName {
			for _, item := range pallet.Storage.Items {
				if string(item.Name) == name {
					entry, found = item, true
				}
			}
		}
	}
	if !found {
		t.Fatalf("synthetic storage %s.%s missing", palletName, name)
	}
	row := rootStorageValue{Name: name, Key: key.Hex(), ValueSource: "absent-optional"}
	if palletName == "System" {
		row.Name = "System." + name
	}
	if data != nil {
		encoded := "0x" + hex.EncodeToString(data)
		row.RawStorage, row.EffectiveScale, row.ValueSource = &encoded, encoded, "finalized-storage"
	} else if entry.Modifier.IsDefault {
		row.EffectiveScale, row.ValueSource = "0x"+hex.EncodeToString(entry.Fallback), "authenticated-metadata-fallback"
	}
	replaced := false
	for index := range observation.Storage {
		if observation.Storage[index].Key == key.Hex() {
			observation.Storage[index], replaced = row, true
		}
	}
	if !replaced {
		observation.Storage = append(observation.Storage, row)
	}
	sort.Slice(observation.Storage, func(i, j int) bool { return observation.Storage[i].Key < observation.Storage[j].Key })
	observation.ContentHash = ""
	observation.ContentHash = rootObjectHash(*observation)
}

// Add one exact synthetic generation and its reverse key to the retained census.
func rootRegisterTestSeat(t *testing.T, observation *rootRegisterObservation, metadata *types.Metadata, uid uint16, hotkey, owner string, block, stake uint64) {
	t.Helper()
	root, uidArg := []byte{0, 0}, binary.LittleEndian.AppendUint16(nil, uid)
	hot, _ := hex.DecodeString(hotkey[2:])
	cold, _ := hex.DecodeString(owner[2:])
	rootRegisterTestSet(t, observation, metadata, "SubtensorModule", "Keys", hot, root, uidArg)
	rootRegisterTestSet(t, observation, metadata, "SubtensorModule", "Uids", uidArg, root, hot)
	rootRegisterTestSet(t, observation, metadata, "SubtensorModule", "Owner", cold, hot)
	rootRegisterTestSet(t, observation, metadata, "SubtensorModule", "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, block), root, uidArg)
	rootRegisterTestSet(t, observation, metadata, "SubtensorModule", "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, stake), hot, root)
	key, _ := types.CreateStorageKey(metadata, "SubtensorModule", "Keys", root, uidArg)
	reverse, _ := types.CreateStorageKey(metadata, "SubtensorModule", "Uids", root, hot)
	observation.Keys, observation.UidKeys = append(observation.Keys, key.Hex()), append(observation.UidKeys, reverse.Hex())
	sort.Strings(observation.Keys)
	sort.Strings(observation.UidKeys)
	rootRegisterTestSet(t, observation, metadata, "SubtensorModule", "SubnetworkN", binary.LittleEndian.AppendUint16(nil, uint16(len(observation.Keys))), root)
}

// A free slot accepts an unstaked applicant; equal root stake may replace the
// oldest nonimmune seat, and every immune seat blocks full-network admission.
func TestRootRegisterSnapshotFreeAndFullSeatRules(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	policy, metadata, observation := f.config.Action.Policy, f.metadata, f.input.Observation
	eligibility, err := observation.eligibility(policy, metadata)
	if err != nil || !eligibility.Eligible || eligibility.ExistingSeat != nil || eligibility.BurnRao != 10 || eligibility.ConservativeReducibleRao != 99500 {
		t.Fatalf("free root slot unavailable: %+v %v", eligibility, err)
	}
	for uid := uint16(0); uid < 3; uid++ {
		hotkey := "0x" + strings.Repeat([]string{"71", "72", "73"}[uid], 32)
		registered := uint64(80)
		if uid == 0 {
			registered = 90
		}
		rootRegisterTestSeat(t, &observation, metadata, uid, hotkey, "0x"+strings.Repeat("74", 32), registered, 20)
	}
	hotkey, _ := hex.DecodeString(policy.Hotkey[2:])
	rootRegisterTestSet(t, &observation, metadata, "SubtensorModule", "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, 20), hotkey, []byte{0, 0})
	eligibility, err = observation.eligibility(policy, metadata)
	if err != nil || !eligibility.Eligible || eligibility.Candidate == nil || eligibility.Candidate.Uid != 1 {
		t.Fatalf("equal stake/tie ordering differs: %+v %v", eligibility, err)
	}
	// A lowered max leaves the same full-network replacement rule in effect.
	rootRegisterTestSet(t, &observation, metadata, "SubtensorModule", "MaxAllowedUids", []byte{2, 0}, []byte{0, 0})
	eligibility, err = observation.eligibility(policy, metadata)
	if err != nil || !eligibility.Eligible || eligibility.Candidate == nil || eligibility.Candidate.Uid != 1 {
		t.Fatalf("oversubscribed root census lost source replacement semantics: %+v %v", eligibility, err)
	}
	rootRegisterTestSet(t, &observation, metadata, "SubtensorModule", "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, 19), hotkey, []byte{0, 0})
	eligibility, err = observation.eligibility(policy, metadata)
	if err != nil || eligibility.Eligible || eligibility.Reason != "root-stake-below-pruning-candidate" {
		t.Fatal("unstaked applicant can displace a staked root seat", err)
	}
	rootRegisterTestSet(t, &observation, metadata, "SubtensorModule", "ImmunityPeriod", []byte{30, 0}, []byte{0, 0})
	eligibility, err = observation.eligibility(policy, metadata)
	if err != nil || eligibility.Eligible || eligibility.Candidate != nil || eligibility.Reason != "root-full-without-nonimmune-seat" {
		t.Fatal("all-immune root fell back to an immune seat", err)
	}
}

// The official SDK rejects nonzero remaining bytes instead of treating every
// account with a similar prefix as a reserved subnet account.
func TestRootRegisterReservedSystemHotkeyExactGrammar(t *testing.T) {
	for _, netuid := range []uint16{0, 25, 65535} {
		raw := append([]byte("modlsubtensr"), binary.LittleEndian.AppendUint16(nil, netuid)...)
		raw = append(raw, make([]byte, 18)...)
		if !rootRegisterReservedHotkey("0x" + hex.EncodeToString(raw)) {
			t.Fatal("source reserved subnet account accepted as operator hotkey", netuid)
		}
		raw[31] = 1
		if rootRegisterReservedHotkey("0x" + hex.EncodeToString(raw)) {
			t.Fatal("nonzero trailing byte was ignored by account conversion")
		}
		raw[31], raw[4] = 0, 'x'
		if rootRegisterReservedHotkey("0x" + hex.EncodeToString(raw)) {
			t.Fatal("another pallet ID was classified as a subtensor account")
		}
	}
}

// Correctly resealing attacker-authored rows does not repair missing census,
// ownership, excluded subnet-owner identity or metadata query distinctions.
func TestRootRegisterObservationRejectsCensusAndOwnerForgery(t *testing.T) {
	for _, change := range []string{"foreign-owner", "excluded-owner", "missing-row", "false-absence", "duplicate-row", "wrong-key"} {
		f := newRootRegisterTestFixture(t, false)
		observation := f.input.Observation
		hotkey, _ := hex.DecodeString(f.config.Action.Policy.Hotkey[2:])
		switch change {
		case "foreign-owner":
			rootRegisterTestSet(t, &observation, f.metadata, "SubtensorModule", "Owner", make([]byte, 32), hotkey)
		case "excluded-owner":
			rootRegisterTestSet(t, &observation, f.metadata, "SubtensorModule", "SubnetOwner", make([]byte, 32), []byte{25, 0})
		case "missing-row":
			observation.Storage = observation.Storage[1:]
		case "false-absence":
			for index := range observation.Storage {
				if observation.Storage[index].Name == "Owner" {
					observation.Storage[index].EffectiveScale = f.config.Action.Policy.Operator
				}
			}
		case "duplicate-row":
			observation.Storage = append(observation.Storage, observation.Storage[0])
		case "wrong-key":
			observation.Storage[0].Key += "00"
		}
		observation.ContentHash = ""
		observation.ContentHash = rootObjectHash(observation)
		eligibility, err := observation.eligibility(f.config.Action.Policy, f.metadata)
		if change == "foreign-owner" {
			if err != nil || eligibility.Eligible || eligibility.Reason != "hotkey-owned-by-another-coldkey" {
				t.Fatal("foreign owner was not retained as ineligible", err)
			}
		} else if err == nil {
			t.Fatalf("%s observation forgery accepted", change)
		}
	}
}
