//go:build linux || darwin

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
)

// An actual HTTP native reader supplies SDK-encoded metadata, SCALE storage
// and the runtime's frozen selective-metagraph wire. No decoded observation
// replaces the mapping, registration, schedule or row verifier.
func TestFinalPublicValidatorSourcesV2NativeCheckpointAuthenticatesExactPublicState(t *testing.T) {
	t.Parallel()
	f := newFinalV2PublicRPCFixture(t)
	f.nativeHead.Number = 100
	f.reader.evidence.NativeTerminalHead = f.nativeHead
	f.reader.evidence.Netuid = 521
	f.reader.evidence.Window.BaselineHead = f.evmHead
	f.reader.runtimeVersion = crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1, StateVersion: 1}
	f.reader.runtimeCodeHash = common.Hash{0x61}.Hex()
	parent, finalized := common.Hash{0x23}.Hex(), common.Hash{0x24}.Hex()
	payout, payoutParent := common.Hash{0x29}.Hex(), common.Hash{0x28}.Hex()
	hotkey, coldkey := [32]byte{11}, [32]byte{12}
	metadata := gsrpctypes.NewMetadataV14()
	metadata.MagicNumber = gsrpctypes.MagicNumber
	identity := gsrpctypes.StorageHasherV10{IsIdentity: true}
	account := gsrpctypes.StorageHasherV10{IsBlake2_128Concat: true}
	entry := func(name string, hashers ...gsrpctypes.StorageHasherV10) gsrpctypes.StorageEntryMetadataV14 {
		return gsrpctypes.StorageEntryMetadataV14{Name: gsrpctypes.Text(name), Modifier: gsrpctypes.StorageFunctionModifierV0{IsDefault: true}, Type: gsrpctypes.StorageEntryTypeV14{IsMap: true, AsMap: gsrpctypes.MapTypeV14{Hashers: hashers}}}
	}
	metadata.AsMetadataV14.Pallets = []gsrpctypes.PalletMetadataV14{
		{Name: crv4.PalletName, HasStorage: true, Storage: gsrpctypes.StorageMetadataV14{Prefix: crv4.PalletName, Items: []gsrpctypes.StorageEntryMetadataV14{
			entry("SubnetworkN", identity), entry("Keys", identity, identity), entry("Uids", identity, account), entry("Owner", account), entry("TotalHotkeyAlpha", account, identity), entry("ValidatorPermit", identity),
			{Name: "StakeThreshold", Modifier: gsrpctypes.StorageFunctionModifierV0{IsDefault: true}, Type: gsrpctypes.StorageEntryTypeV14{IsPlainType: true}},
			entry("SubnetOwnerHotkey", identity), entry("SubnetEpochIndex", identity), entry("LastEpochBlock", identity), entry("PendingEpochAt", identity), entry("BlocksSinceLastStep", identity), entry("Tempo", identity), entry("RevealPeriodEpochs", identity), entry("Weights", identity, identity), entry("LastUpdate", identity),
		}}},
		{Name: "Ethereum", HasStorage: true, Storage: gsrpctypes.StorageMetadataV14{Prefix: "Ethereum", Items: []gsrpctypes.StorageEntryMetadataV14{entry("BlockHash", gsrpctypes.StorageHasherV10{IsTwox64Concat: true})}}},
	}
	metadataHex, err := codec.EncodeToHex(metadata)
	if err != nil { t.Fatal(err) }
	_, f.reader.runtimeMetadataHash, err = crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil { t.Fatal(err) }
	encode := func(value any) string { t.Helper(); raw, err := codec.EncodeToHex(value); if err != nil { t.Fatal(err) }; return raw }
	netuid, uid := binary.LittleEndian.AppendUint16(nil, 521), binary.LittleEndian.AppendUint16(nil, 1)
	storage := map[string]any{}
	add := func(name string, value any, args ...[]byte) string {
		t.Helper()
		key, err := gsrpctypes.CreateStorageKey(metadata, crv4.PalletName, name, args...)
		if err != nil { t.Fatal(err) }
		if value == nil { storage[key.Hex()] = nil } else { storage[key.Hex()] = encode(value) }
		return key.Hex()
	}
	add("SubnetworkN", uint16(3), netuid)
	add("Keys", hotkey, netuid, uid)
	add("Uids", uint16(1), netuid, hotkey[:])
	add("Owner", coldkey, hotkey[:])
	add("TotalHotkeyAlpha", uint64(200), hotkey[:], netuid)
	add("ValidatorPermit", []bool{false, true, false}, netuid)
	add("StakeThreshold", uint64(100))
	add("SubnetOwnerHotkey", nil, netuid)
	add("SubnetEpochIndex", uint64(10), netuid)
	add("LastEpochBlock", uint64(90), netuid)
	add("PendingEpochAt", uint64(0), netuid)
	add("BlocksSinceLastStep", uint64(10), netuid)
	add("Tempo", uint16(9), netuid)
	add("RevealPeriodEpochs", uint64(1), netuid)
	add("Weights", []crv4.WeightPair{{UID: 0, Value: 65535}, {UID: 2, Value: 100}}, netuid, uid)
	updatesKey := add("LastUpdate", []gsrpctypes.U64{1, 98, 1}, netuid)
	arg := make([]byte, 32)
	binary.LittleEndian.PutUint64(arg, f.evmHead.Number)
	mappingKey, err := gsrpctypes.CreateStorageKey(metadata, "Ethereum", "BlockHash", arg)
	if err != nil { t.Fatal(err) }
	compact := func(n uint64) []byte { t.Helper(); raw, err := codec.Encode(gsrpctypes.NewUCompactFromUInt(n)); if err != nil { t.Fatal(err) }; return raw }
	hotkeys := compact(3)
	for _, key := range [][32]byte{{31}, hotkey, {33}} { hotkeys = append(hotkeys, key[:]...) }
	stakes := compact(3)
	for _, stake := range []uint64{0, 150, 9} { stakes = append(stakes, compact(stake)...) }
	fields := map[int][]byte{30: compact(3), 52: hotkeys, 57: {12, 0, 1, 0}, 69: stakes}
	metagraph := append([]byte{1}, compact(521)...)
	for field := 1; field <= 76; field++ { if value, ok := fields[field]; ok { metagraph = append(metagraph, 1); metagraph = append(metagraph, value...) } else { metagraph = append(metagraph, 0) } }
	var mode atomic.Uint32
	var nativeCalls atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call finalV2RPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil { t.Error(err); http.Error(writer, "bad request", 400); return }
		nativeCalls.Add(1)
		var result any
		lastHash := ""
		if len(call.Params) != 0 { _ = json.Unmarshal(call.Params[len(call.Params)-1], &lastHash) }
		switch call.Method {
		case "chain_getBlockHash":
			number, err := finalV2RPCNumber(call.Params[0]); if err != nil { t.Error(err); return }
			result = map[uint64]string{0: testnetGenesis, 89: payoutParent, 90: payout, 99: parent, 100: f.nativeHead.Hash, 101: finalized}[number]
		case "chain_getHeader":
			number := map[string]uint64{payoutParent: 89, payout: 90, parent: 99, f.nativeHead.Hash: 100, finalized: 101}[lastHash]
			parentHash := parent; if number == 99 { parentHash = common.Hash{0x25}.Hex() }; if number == 101 { parentHash = f.nativeHead.Hash }
			if number == 90 { parentHash = payoutParent }
			if number == 89 { parentHash = common.Hash{0x27}.Hex() }
			result = map[string]any{"number": fmt.Sprintf("0x%x", number), "parentHash": parentHash, "stateRoot": common.Hash{}.Hex(), "extrinsicsRoot": common.Hash{}.Hex(), "digest": map[string]any{"logs": []string{}}}
		case "chain_getFinalizedHead": result = finalized
		case "state_getRuntimeVersion": result = f.reader.runtimeVersion
		case "state_getStorageHash": result = f.reader.runtimeCodeHash; if mode.Load() == 3 { result = common.Hash{0x62}.Hex() }
		case "state_getMetadata": result = metadataHex
		case "state_getStorage":
			var key string; if err := json.Unmarshal(call.Params[0], &key); err != nil { t.Error(err); return }
			if key == mappingKey.Hex() {
				if lastHash == parent && mode.Load() != 2 { result = nil } else { result = f.evmHead.Hash }
				if lastHash == f.nativeHead.Hash && mode.Load() == 1 { result = common.Hash{0x55}.Hex() }
			} else {
				var ok bool; result, ok = storage[key]
				if !ok || lastHash != f.nativeHead.Hash { t.Errorf("unexpected native storage %s at %s", key, lastHash); http.Error(writer, "wrong storage", 400); return }
				if key == updatesKey && mode.Load() == 4 { result = "0x00" }
			}
		case "state_call": result = fmt.Sprintf("0x%x", metagraph)
		default: t.Errorf("unexpected native read %s", call.Method); http.Error(writer, "unexpected method", 400); return
		}
		encoded, err := json.Marshal(result); if err != nil { t.Error(err); return }
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%s,"result":%s}`, call.ID, encoded)
	}))
	t.Cleanup(server.Close)
	client, err := gsrpcgeth.DialContext(t.Context(), server.URL)
	if err != nil { t.Fatal(err) }
	t.Cleanup(client.Close)
	f.reader.native = &gsrpc.SubstrateAPI{Client: &finalV2NativeReadTestClient{Client: client, endpoint: server.URL}}
	target, err := finalSemanticEvidenceDetachedCopy(f.reader.evidence)
	if err != nil { t.Fatal(err) }
	read := func(wantUID uint16) (FinalNativeCheckpointV2, []FinalRPCExchange, error) { return f.reader.NativeCheckpointV2(t.Context(), target, f.evmHead, wantUID, fmt.Sprintf("0x%x", hotkey)) }
	got, exchanges, err := read(1)
	if err != nil { t.Fatalf("actual public checkpoint failed: %v", err) }
	if got.Mapping.Query.NativeHash.Hex() != f.nativeHead.Hash || got.Mapping.Query.EVMHash.Hex() != f.evmHead.Hash || got.Mapping.Query.NativeHash == got.Mapping.Query.EVMHash || got.Schedule.SubnetEpochIndex != 10 || got.Schedule.LastEpochBlock != 90 || got.RevealPeriodEpochs != 1 || got.Identity.Stake.TotalStakeRao != 150 || got.Weights.LastUpdate != 98 || !reflect.DeepEqual(got.Weights.Values, []uint16{65535, 100}) {
		t.Fatalf("public checkpoint mixed clock/row/schedule authority: %+v", got)
	}
	if got.PayoutHead != (ChainHead{Number: 90, Hash: payout}) || got.PayoutParent != (ChainHead{Number: 89, Hash: payoutParent}) {
		t.Fatalf("public checkpoint lost the actual latest native payout and parent: %+v", got)
	}
	seenEVM, seenNative, seenRuntime, seenAbsent := false, false, false, false
	for _, exchange := range exchanges {
		if exchange.Chain == "evm" { seenEVM = true; if exchange.PinnedHead != f.evmHead { t.Fatal("EVM exchange inherited native identity") } }
		if exchange.Chain == "substrate" { seenNative = true; if exchange.PinnedHead.Hash == f.evmHead.Hash { t.Fatal("native exchange inherited EVM identity") } }
		if exchange.Method == "state_call" { seenRuntime = true }
		if exchange.Method == "state_getStorage" && exchange.PinnedHead.Hash == parent && string(exchange.Result) == "null" { seenAbsent = true }
	}
	if !seenEVM || !seenNative || !seenRuntime || !seenAbsent { t.Fatal("public checkpoint lost actual canonical/runtime/parent-absence reads") }
	for _, fault := range []uint32{1, 2, 3, 4} {
		mode.Store(fault)
		partial, raw, err := read(1)
		if err == nil || !reflect.DeepEqual(partial, FinalNativeCheckpointV2{}) || raw != nil { t.Fatalf("fault %d issued partial checkpoint authority: %v", fault, err) }
	}
	mode.Store(0)
	if partial, raw, err := read(2); err == nil || !reflect.DeepEqual(partial, FinalNativeCheckpointV2{}) || raw != nil { t.Fatal("wrong validator UID issued public authority") }
	before := nativeCalls.Load()
	got.Weights.Values[0] = 1
	fresh, _, err := read(1)
	if err != nil || fresh.Weights.Values[0] != 65535 || nativeCalls.Load() <= before { t.Fatalf("invocation reused another caller's mutable checkpoint: %v", err) }
}
