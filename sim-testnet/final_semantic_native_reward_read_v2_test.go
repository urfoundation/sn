//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
)

type finalNativeRewardHistoryFixtureV2 struct {
	native          *crv4.Chain
	head            ChainHead
	runtime         crv4.RuntimeArtifactIdentity
	mode            atomic.Uint32
	queries         atomic.Uint64
	started, joined chan struct{}
}

func newFinalNativeRewardHistoryFixtureV2(t *testing.T) *finalNativeRewardHistoryFixtureV2 {
	t.Helper()
	f := &finalNativeRewardHistoryFixtureV2{head: ChainHead{Number: 100, Hash: common.Hash{0x31}.Hex()}, started: make(chan struct{}), joined: make(chan struct{})}
	finalized := common.Hash{0x32}.Hex()
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	identity, account := types.StorageHasherV10{IsIdentity: true}, types.StorageHasherV10{IsBlake2_128Concat: true}
	entry := func(name string, hashers ...types.StorageHasherV10) types.StorageEntryMetadataV14 {
		return types.StorageEntryMetadataV14{Name: types.Text(name), Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: hashers}}}
	}
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{Name: crv4.PalletName, HasStorage: true, Storage: types.StorageMetadataV14{Prefix: crv4.PalletName, Items: []types.StorageEntryMetadataV14{
		entry("SubnetworkN", identity), entry("Emission", identity), entry("Incentive", identity), entry("Dividends", identity), entry("Keys", identity, identity), entry("Owner", account), entry("BlockAtRegistration", identity, identity), entry("TotalHotkeyAlpha", account, identity),
	}}}}
	metadataHex, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, metadataHash, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	f.runtime = crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1, StateVersion: 1}, CodeHash: common.Hash{0x33}.Hex(), MetadataHash: metadataHash}
	values := map[string]string{}
	add := func(name string, value any, args ...[]byte) string {
		t.Helper()
		key, err := types.CreateStorageKey(metadata, crv4.PalletName, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := codec.EncodeToHex(value)
		if err != nil {
			t.Fatal(err)
		}
		values[key.Hex()] = raw
		return key.Hex()
	}
	countKey := add("SubnetworkN", types.U16(2), netuidArg(521))
	emissionKey := add("Emission", []types.U64{3, 7}, netuidArg(521))
	add("Incentive", []types.U16{0, 65535}, netuidArg(521))
	add("Dividends", []types.U16{123, 456}, netuidArg(521))
	for uid := uint16(0); uid < 2; uid++ {
		hotkey, coldkey := [32]byte{byte(uid + 10)}, [32]byte{byte(uid + 20)}
		add("Keys", hotkey, netuidArg(521), netuidArg(uid))
		add("Owner", coldkey, hotkey[:])
		add("BlockAtRegistration", types.U64(10+uid), netuidArg(521), netuidArg(uid))
		add("TotalHotkeyAlpha", types.U64(100+100*uid), hotkey[:], netuidArg(521))
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call finalV2RPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		var result any
		mode := f.mode.Load()
		lastHash := ""
		if len(call.Params) > 0 {
			_ = json.Unmarshal(call.Params[len(call.Params)-1], &lastHash)
		}
		switch call.Method {
		case "chain_getBlockHash":
			number, err := finalV2RPCNumber(call.Params[0])
			if err != nil {
				t.Error(err)
				return
			}
			result = map[uint64]string{0: testnetGenesis, 100: f.head.Hash}[number]
			if mode == 1 && number == 100 {
				result = common.Hash{0x34}.Hex()
			}
		case "chain_getFinalizedHead":
			result = finalized
		case "chain_getHeader":
			number := uint64(100)
			if lastHash == finalized {
				number = 110
				if mode == 2 {
					number = 99
				}
			} else if lastHash != f.head.Hash {
				t.Errorf("unexpected historical header %s", lastHash)
				return
			}
			result = map[string]any{"number": fmt.Sprintf("0x%x", number), "parentHash": common.Hash{0x35}.Hex(), "stateRoot": common.Hash{}.Hex(), "extrinsicsRoot": common.Hash{}.Hex(), "digest": map[string]any{"logs": []string{}}}
		case "state_getRuntimeVersion":
			result = f.runtime.Version
		case "state_getStorageHash":
			result = f.runtime.CodeHash
			if mode == 3 {
				result = common.Hash{0x36}.Hex()
			}
		case "state_getMetadata":
			result = metadataHex
		case "state_getStorage":
			var key string
			if err := json.Unmarshal(call.Params[0], &key); err != nil {
				t.Error(err)
				return
			}
			value, ok := values[key]
			if !ok || lastHash != f.head.Hash {
				t.Errorf("unbound historical storage %s at %s", key, lastHash)
				return
			}
			result = value
			if mode == 4 && key == emissionKey {
				result = "0x00"
			}
			if mode == 7 && key == countKey {
				result = "0x0000"
			}
		case "state_queryStorageAt":
			f.queries.Add(1)
			if mode == 8 {
				close(f.started)
				<-request.Context().Done()
				close(f.joined)
				return
			}
			var keys []string
			if err := json.Unmarshal(call.Params[0], &keys); err != nil {
				t.Error(err)
				return
			}
			if lastHash != f.head.Hash {
				t.Error("UID batch lost its exact historical head")
				return
			}
			changes := make([][2]any, 0, len(keys))
			for _, key := range keys {
				value, ok := values[key]
				if !ok {
					t.Errorf("unexpected UID storage %s", key)
					return
				}
				changes = append(changes, [2]any{key, value})
			}
			block := f.head.Hash
			if mode == 5 {
				block = common.Hash{0x37}.Hex()
			}
			if mode == 6 {
				changes = changes[:len(changes)-1]
			}
			result = []any{map[string]any{"block": block, "changes": changes}}
		default:
			t.Errorf("unexpected historical native method %s", call.Method)
			return
		}
		if call.Method == "state_getRuntimeVersion" || call.Method == "state_getStorageHash" || call.Method == "state_getMetadata" {
			if lastHash != f.head.Hash {
				t.Error("historical native runtime read used unpinned/latest metadata")
				return
			}
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Error(err)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%s,"result":%s}`, call.ID, raw)
	}))
	t.Cleanup(server.Close)
	client, err := gsrpcgeth.DialContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	genesis, err := types.NewHashFromHexString(testnetGenesis)
	if err != nil {
		t.Fatal(err)
	}
	f.native = &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: &finalV2NativeReadTestClient{Client: client, endpoint: server.URL}}, GenesisHash: genesis, Meta: types.NewMetadataV14()}
	return f
}

func TestFinalNativeRewardV2ReadsExactHistoricalVectorsAndUIDStake(t *testing.T) {
	t.Parallel()
	f := newFinalNativeRewardHistoryFixtureV2(t)
	originalMetadata := f.native.Meta
	got, err := readFinalNativeRewardAtV2(t.Context(), f.native, f.head, 521, f.runtime)
	want := &NativeRewardObservation{FinalizedHead: f.head, EmissionRao: []string{"3", "7"}, Incentive: []uint16{0, 65535}, Dividends: []uint16{123, 456}, TotalHotkeyAlphaRao: []string{"100", "200"}}
	if err != nil || !reflect.DeepEqual(got, want) || f.queries.Load() != 2 || f.native.Meta != originalMetadata || f.native.API.RPC != nil {
		t.Fatalf("exact historical reward surfaces were not captured without mutating caller metadata: %+v %v", got, err)
	}
	for _, mode := range []uint32{1, 2, 3, 4, 5, 6, 7} {
		f.mode.Store(mode)
		if result, err := readFinalNativeRewardAtV2(t.Context(), f.native, f.head, 521, f.runtime); err == nil || result != nil {
			t.Fatalf("fault %d returned a historical reward observation: %+v %v", mode, result, err)
		}
	}
	f.mode.Store(0)
	got.TotalHotkeyAlphaRao[0] = "999"
	if fresh, err := readFinalNativeRewardAtV2(t.Context(), f.native, f.head, 521, f.runtime); err != nil || !reflect.DeepEqual(fresh, want) {
		t.Fatalf("historical read reused another caller's mutable result: %+v %v", fresh, err)
	}
}

func TestFinalNativeRewardV2CancellationJoinsHistoricalBatch(t *testing.T) {
	t.Parallel()
	f := newFinalNativeRewardHistoryFixtureV2(t)
	f.mode.Store(8)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := readFinalNativeRewardAtV2(ctx, f.native, f.head, 521, f.runtime)
		if result != nil {
			err = errors.New("canceled read returned a partial reward")
		}
		done <- err
	}()
	select {
	case <-f.started:
	case <-time.After(10 * time.Second):
		t.Fatal("historical UID batch did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("historical batch cancellation differs: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("historical reader failed to join canceled batch")
	}
	select {
	case <-f.joined:
	case <-time.After(10 * time.Second):
		t.Fatal("historical HTTP batch remained live after cancellation")
	}
}
