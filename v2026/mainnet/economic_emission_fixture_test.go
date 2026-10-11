// Local synthetic archive fixtures retain real envelope metadata and explicitly
// add the reviewed incentive/storage profile. They contain no live authority,
// runtime-to-Wasm assertion, key custody, or public RPC fallback.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Independently encode an augmented fixture so the policy approves its exact
// bytes, not the old artifact hash or a mutable in-memory decoder registry.
func economicEmissionTestMetadata(t *testing.T, mutate func(*types.Metadata)) (*types.Metadata, string, string) {
	t.Helper()
	metadata, _, _ := rootTestMetadata(t)
	next := uint64(0)
	for id := range metadata.AsMetadataV14.EfficientLookup {
		if next <= uint64(id) {
			next = uint64(id) + 1
		}
	}
	addType := func(def types.Si1TypeDef) types.Si1LookupTypeID {
		id := types.NewSi1LookupTypeIDFromUInt(next)
		next++
		value := types.Si1Type{Def: def}
		metadata.AsMetadataV14.EfficientLookup[id.Int64()] = &value
		metadata.AsMetadataV14.Lookup.Types = append(metadata.AsMetadataV14.Lookup.Types, types.PortableTypeV14{ID: id, Type: value})
		return id
	}
	idKVs := map[string]types.Si1LookupTypeID{}
	for _, primitive := range []struct {
		name string
		kind types.Si0TypeDefPrimitive
	}{{name: "u8", kind: types.IsU8}, {name: "u16", kind: types.IsU16}, {name: "u64", kind: types.IsU64}, {name: "bool", kind: types.IsBool}} {
		idKVs[primitive.name] = addType(types.Si1TypeDef{IsPrimitive: true, Primitive: types.Si1TypeDefPrimitive{Si0TypeDefPrimitive: primitive.kind}})
	}
	idKVs["u64s"] = addType(types.Si1TypeDef{IsSequence: true, Sequence: types.Si1TypeDefSequence{Type: idKVs["u64"]}})
	idKVs["account"] = addType(types.Si1TypeDef{IsArray: true, Array: types.Si1TypeDefArray{Len: 32, Type: idKVs["u8"]}})
	modeId := addType(types.Si1TypeDef{IsVariant: true, Variant: types.Si1TypeDefVariant{Variants: []types.Si1Variant{{Name: "Burn", Index: 0}, {Name: "Recycle", Index: 1}}}})
	for palletIndex := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[palletIndex]
		if pallet.Name != "SubtensorModule" {
			continue
		}
		upsert := func(entry types.StorageEntryMetadataV14) {
			for index := range pallet.Storage.Items {
				if pallet.Storage.Items[index].Name == entry.Name {
					pallet.Storage.Items[index] = entry
					return
				}
			}
			pallet.Storage.Items = append(pallet.Storage.Items, entry)
		}
		for _, spec := range economicEmissionStorageSpecs {
			width := map[string]int{"bool": 1, "u8": 1, "u16": 2, "u64": 8}[spec.value]
			entry := types.StorageEntryMetadataV14{Name: types.Text(spec.name), Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Fallback: make(types.Bytes, width)}
			if len(spec.keys) == 0 {
				entry.Type = types.StorageEntryTypeV14{IsPlainType: true, AsPlainType: idKVs[spec.value]}
			} else {
				hasher := types.StorageHasherV10{IsIdentity: true}
				if spec.hashers[0] == "twox64concat" {
					hasher = types.StorageHasherV10{IsTwox64Concat: true}
				}
				entry.Type = types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Key: idKVs["u16"], Value: idKVs[spec.value], Hashers: []types.StorageHasherV10{hasher}}}
			}
			upsert(entry)
		}
		upsert(types.StorageEntryMetadataV14{Name: "RecycleOrBurn", Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Fallback: types.Bytes{0}, Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Key: idKVs["u16"], Value: modeId, Hashers: []types.StorageHasherV10{{IsIdentity: true}}}}})
		eventType := metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()]
		kept := []types.Si1Variant{}
		used := map[byte]bool{}
		for _, variant := range eventType.Def.Variant.Variants {
			if variant.Name == "IncentiveAlphaEmittedToMiners" || variant.Name == "EpochDeferred" || variant.Name == "EpochSkipped" || variant.Name == "SubnetOwnerChanged" {
				continue
			}
			kept = append(kept, variant)
			used[byte(variant.Index)] = true
		}
		field := func(name, shape string) types.Si1Field {
			return types.Si1Field{HasName: true, Name: types.Text(name), Type: idKVs[shape]}
		}
		for _, variant := range []types.Si1Variant{
			{Name: "IncentiveAlphaEmittedToMiners", Fields: []types.Si1Field{field("netuid", "u16"), field("emissions", "u64s")}},
			{Name: "EpochDeferred", Fields: []types.Si1Field{field("netuid", "u16"), field("from_block", "u64"), field("to_block", "u64")}},
			{Name: "EpochSkipped", Fields: []types.Si1Field{field("netuid", "u16"), field("block", "u64")}},
			{Name: "SubnetOwnerChanged", Fields: []types.Si1Field{field("netuid", "u16"), field("old_coldkey", "account"), field("new_coldkey", "account")}},
		} {
			for index := 0; index <= 255; index++ {
				if !used[byte(index)] {
					variant.Index = types.U8(index)
					used[byte(index)] = true
					kept = append(kept, variant)
					break
				}
			}
		}
		eventType.Def.Variant.Variants = kept
	}
	if mutate != nil {
		mutate(metadata)
	}
	for index := range metadata.AsMetadataV14.Lookup.Types {
		entry := &metadata.AsMetadataV14.Lookup.Types[index]
		entry.Type = *metadata.AsMetadataV14.EfficientLookup[entry.ID.Int64()]
	}
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	decoded, hash, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return decoded, encoded, hash
}

// Only counters in the reused handler mutate during collection. Fixture fields
// and faults are configured before requests, including under the race detector.
type economicEmissionFixture struct {
	chain      *rootReceiptFixture
	client     *rpcClient
	policy     economicEmissionPolicy
	storageKVs map[string]map[string]*string
	eventsKey  string
	omitAt     string
}

// Event construction uses the independent GSRPC fixture rather than the
// production traversal; initialization has no extrinsic-index payload.
func economicEmissionTestEvent(t *testing.T, metadata *types.Metadata, name string, fields ...[]byte) []byte {
	t.Helper()
	raw := rootReceiptEventFixture(t, metadata, "SubtensorModule."+name, 0, fields...)
	return append([]byte{2}, raw[5:]...)
}

// The baseline has one incentive epoch and a later accumulating block. Pending
// server90 followed by emitted10 intentionally cannot establish denominator10.
func newEconomicEmissionFixture(t *testing.T) *economicEmissionFixture {
	t.Helper()
	metadata, metadataHex, metadataHash := economicEmissionTestMetadata(t, nil)
	profile := rootReceiptProfile{RuntimeSourceCommit: rootProfileSource, RuntimeVersion: crv4.RuntimeVersionIdentity{SpecName: "synthetic-mainnet", SpecVersion: 601, TransactionVersion: 2, StateVersion: 1}, RuntimeCodeHash: "0x" + strings.Repeat("c", 64), RuntimeMetadataHash: metadataHash}
	chain := &rootReceiptFixture{
		metadata: metadata, metadataHex: metadataHex, profile: profile, headers: map[string]rootReceiptHeader{}, byHeight: map[uint64]string{}, bodies: map[string][]string{}, storageKVs: map[string]string{},
		evmHex: "0x3c4", runtimeKVs: map[string]rootReceiptProfile{}, metadataKVs: map[string]string{}, counts: map[string]int{},
	}
	chain.action.Scope.NativeChain, chain.action.Scope.GenesisHash = "synthetic-mainnet", testGenesisHash
	fixture := &economicEmissionFixture{chain: chain, storageKVs: map[string]map[string]*string{}}
	parent := "0x" + strings.Repeat("bc", 32)
	for number := uint64(100); number <= 102; number++ {
		header, hash := rootReceiptHeaderFixture(t, parent, number, nil, false)
		chain.headers[hash], chain.byHeight[number], chain.bodies[hash] = header, hash, []string{}
		fixture.storageKVs[hash] = map[string]*string{}
		parent = hash
	}
	chain.finalized = parent
	birth, generation := uint64(10), uint64(3)
	fixture.policy = economicEmissionPolicy{Schema: economicEmissionPolicySchema, Network: planNetwork{NativeChain: "synthetic-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, Runtime: profile, Netuid: 25, SubnetRegistrationBlock: &birth, SubnetGeneration: &generation, From: economicEmissionBoundary{Number: 100, Hash: chain.byHeight[100]}, Through: economicEmissionBoundary{Number: 102, Hash: chain.byHeight[102]}, MaximumUids: 8}
	for number := uint64(100); number <= 102; number++ {
		for _, spec := range economicEmissionStorageSpecs {
			width := map[string]int{"bool": 1, "u8": 1, "u16": 2, "u64": 8}[spec.value]
			fixture.set(t, number, spec.name, make([]byte, width))
		}
		for name, value := range map[string]uint64{"NetworkRegisteredAt": 10, "RegisteredSubnetCounter": 3, "SubnetEpochIndex": 8, "LastEpochBlock": 101, "PendingServerEmission": 11} {
			fixture.set(t, number, name, binary.LittleEndian.AppendUint64(nil, value))
		}
		fixture.set(t, number, "NetworksAdded", []byte{1})
		fixture.set(t, number, "MechanismCountCurrent", []byte{1})
		fixture.set(t, number, "SubnetworkN", []byte{2, 0})
		fixture.set(t, number, "Tempo", []byte{100, 0})
		fixture.set(t, number, "RecycleOrBurn", []byte{1})
		fixture.set(t, number, "OwnerCutEnabled", []byte{1})
		fixture.set(t, number, "Events", []byte{0})
	}
	fixture.set(t, 100, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 7))
	fixture.set(t, 100, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 99))
	fixture.set(t, 100, "PendingServerEmission", binary.LittleEndian.AppendUint64(nil, 90))
	fixture.set(t, 101, "PendingServerEmission", make([]byte, 8))
	fixture.incentive(t, 101, 25, 9, 1)
	chain.fault = func(method string, params []json.RawMessage, _ int) (any, bool) {
		if method != "state_getStorage" || len(params) != 2 {
			return nil, false
		}
		var key, block string
		json.Unmarshal(params[0], &key)
		json.Unmarshal(params[1], &block)
		value, exists := fixture.storageKVs[block][key]
		return value, exists
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if fixture.omitAt != "" {
			raw, _ := io.ReadAll(io.LimitReader(request.Body, 1024*1024))
			var call struct {
				Method string   `json:"method"`
				Params []string `json:"params"`
			}
			json.Unmarshal(raw, &call)
			if call.Method == "state_getStorage" && len(call.Params) == 2 && call.Params[0] == fixture.eventsKey && call.Params[1] == fixture.omitAt {
				io.WriteString(writer, `{"jsonrpc":"2.0","id":1}`)
				return
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
		}
		chain.serve(writer, request)
	}))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	fixture.client = client
	return fixture
}

// Exact state keys are built from the fixture's separately encoded metadata.
func (self *economicEmissionFixture) set(t *testing.T, number uint64, name string, raw []byte) string {
	t.Helper()
	pallet := "SubtensorModule"
	args := [][]byte{{25, 0}}
	if name == "SubnetOwnerCut" {
		args = nil
	}
	if name == "Events" {
		pallet, args = "System", nil
	}
	key, err := types.CreateStorageKey(self.chain.metadata, pallet, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	if name == "Events" {
		self.eventsKey = key.Hex()
	}
	value := "0x" + hex.EncodeToString(raw)
	self.storageKVs[self.chain.byHeight[number]][key.Hex()] = &value
	return key.Hex()
}

// All supplied amounts are u64 atomic alpha; expected totals in assertions are
// literal big integers, not computed by the production aggregate function.
func (self *economicEmissionFixture) incentive(t *testing.T, number uint64, netuid uint16, amounts ...uint64) {
	t.Helper()
	vector := rootCompact(uint64(len(amounts)))
	for _, amount := range amounts {
		vector = binary.LittleEndian.AppendUint64(vector, amount)
	}
	event := economicEmissionTestEvent(t, self.chain.metadata, "IncentiveAlphaEmittedToMiners", binary.LittleEndian.AppendUint16(nil, netuid), vector)
	self.set(t, number, "Events", append([]byte{4}, event...))
}
