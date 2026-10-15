package commitmentoracle

// The crv4-backed reader against a local JSON-RPC server: CommitmentOf keys
// are listed under the netuid prefix at the requested block, hotkeys are taken
// from the key tail only when the metadata rebuilds the exact key, and values
// come from state_queryStorageAt at that same block.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcclient "github.com/centrifuge/go-substrate-rpc-client/v4/client"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/crv4"
)

type nativeRpcRequest struct {
	Id     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

// Commitments metadata with the given second hasher for both maps.
func commitmentsMetadata(second types.StorageHasherV10) *types.Metadata {
	entry := func(name string) types.StorageEntryMetadataV14 {
		return types.StorageEntryMetadataV14{
			Name: types.Text(name),
			Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{{IsIdentity: true}, second}}},
		}
	}
	metadata := types.NewMetadataV14()
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{
		Name:       types.Text(crv4.CommitmentsPalletName),
		HasStorage: true,
		Storage: types.StorageMetadataV14{
			Prefix: types.Text(crv4.CommitmentsPalletName),
			Items:  []types.StorageEntryMetadataV14{entry("CommitmentOf"), entry("LastCommitment")},
		},
	}}
	return metadata
}

// Serves storage values by key at one block hash and records the block of
// every storage RPC.
func newNativeRpcServer(t *testing.T, at [32]byte, values map[string][]byte, otherNetuidKeys []string) (*httptest.Server, *[]string) {
	blocks := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpcRequest nativeRpcRequest
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var result any
		switch rpcRequest.Method {
		case "state_getKeysPaged":
			var prefix string
			var block string
			json.Unmarshal(rpcRequest.Params[0], &prefix)
			json.Unmarshal(rpcRequest.Params[3], &block)
			blocks = append(blocks, block)
			keys := []string{}
			for key := range values {
				if strings.HasPrefix(key, prefix) {
					keys = append(keys, key)
				}
			}
			for _, key := range otherNetuidKeys {
				if strings.HasPrefix(key, prefix) {
					keys = append(keys, key)
				}
			}
			slices.Sort(keys)
			result = keys
		case "state_queryStorageAt":
			var keys []string
			var block string
			json.Unmarshal(rpcRequest.Params[0], &keys)
			json.Unmarshal(rpcRequest.Params[1], &block)
			blocks = append(blocks, block)
			changes := [][2]any{}
			for _, key := range keys {
				changes = append(changes, [2]any{key, codec.HexEncodeToString(values[key])})
			}
			result = []map[string]any{{"block": block, "changes": changes}}
		default:
			t.Errorf("unexpected RPC %s", rpcRequest.Method)
		}
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.Id, "result": result})
	}))
	return server, &blocks
}

func newTestSubstrateReader(t *testing.T, url string, metadata *types.Metadata) *substrateReader {
	client, err := gsrpcclient.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return &substrateReader{endpoint: "test", chain: &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: metadata}}
}

func TestSubstrateReaderListsNetuidCommitmentsAtBlock(t *testing.T) {
	metadata := commitmentsMetadata(types.StorageHasherV10{IsTwox64Concat: true})
	at := [32]byte{0xaa, 0x01}
	keyOf := func(netuid uint16, hotkey [32]byte) string {
		key, err := types.CreateStorageKey(metadata, crv4.CommitmentsPalletName, "CommitmentOf", encodeNetuid(netuid), hotkey[:])
		if err != nil {
			t.Fatal(err)
		}
		return codec.HexEncodeToString(key)
	}
	values := map[string][]byte{}
	hotkeys := [][32]byte{}
	for index := 0; index < 70; index += 1 {
		hotkey := [32]byte{byte(index + 1), 0x25}
		hotkeys = append(hotkeys, hotkey)
		values[keyOf(25, hotkey)] = []byte{byte(index)}
	}
	// Netuid 26 shares no prefix with netuid 25 and is never listed.
	server, blocks := newNativeRpcServer(t, at, values, []string{keyOf(26, [32]byte{0x99})})
	defer server.Close()
	reader := newTestSubstrateReader(t, server.URL, metadata)

	entries, err := reader.commitments(context.Background(), 25, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 70 {
		t.Fatalf("entries %d, want 70", len(entries))
	}
	for _, entry := range entries {
		index := slices.Index(hotkeys, entry.hotkey)
		if index < 0 || len(entry.raw) != 1 || entry.raw[0] != byte(index) {
			t.Fatalf("entry hotkey 0x%x raw %x", entry.hotkey, entry.raw)
		}
	}
	for _, block := range *blocks {
		if !strings.EqualFold(block, types.Hash(at).Hex()) {
			t.Fatalf("storage read at %s, not the requested block", block)
		}
	}
}

// A hasher that does not end in the hotkey cannot name the entry's hotkey, so
// the listing fails instead of guessing.
func TestSubstrateReaderRefusesNonConcatHasher(t *testing.T) {
	metadata := commitmentsMetadata(types.StorageHasherV10{IsBlake2_128: true})
	at := [32]byte{0xaa, 0x02}
	key, err := types.CreateStorageKey(metadata, crv4.CommitmentsPalletName, "CommitmentOf", encodeNetuid(25), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	// Pad the key so its length could hold a hotkey tail.
	padded := codec.HexEncodeToString(append(key, make([]byte, 32)...))
	server, _ := newNativeRpcServer(t, at, map[string][]byte{padded: {1}}, nil)
	defer server.Close()
	reader := newTestSubstrateReader(t, server.URL, metadata)
	if _, err := reader.commitments(context.Background(), 25, at); err == nil {
		t.Fatal("listed commitments under a non-concat hasher")
	}
}

// SubtensorModule metadata for the role reads: SubnetworkN and
// ValidatorPermit by netuid, Uids by (netuid, hotkey), Keys by (netuid, uid).
func roleMetadata() *types.Metadata {
	identity := types.StorageHasherV10{IsIdentity: true}
	entry := func(name string, hashers ...types.StorageHasherV10) types.StorageEntryMetadataV14 {
		return types.StorageEntryMetadataV14{
			Name: types.Text(name),
			Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: hashers}},
		}
	}
	metadata := types.NewMetadataV14()
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{
		Name:       types.Text(crv4.PalletName),
		HasStorage: true,
		Storage: types.StorageMetadataV14{
			Prefix: types.Text(crv4.PalletName),
			Items: []types.StorageEntryMetadataV14{
				entry("SubnetworkN", identity),
				entry("ValidatorPermit", identity),
				entry("Uids", identity, types.StorageHasherV10{IsBlake2_128Concat: true}),
				entry("Keys", identity, identity),
			},
		},
	}}
	return metadata
}

// The reader builds every role key from the metadata, reads them at the given
// block, and decodes the validator's permit.
func TestSubstrateReaderReadsRoleAtBlock(t *testing.T) {
	metadata := roleMetadata()
	at := [32]byte{0xab}
	validator := [32]byte{0x28, 0x30}
	key := func(item string, args ...[]byte) string {
		built, err := types.CreateStorageKey(metadata, crv4.PalletName, item, args...)
		if err != nil {
			t.Fatal(err)
		}
		return built.Hex()
	}
	netuid := encodeNetuid(25)
	values := map[string][]byte{
		key("SubnetworkN", netuid):           {0x03, 0x00},
		key("ValidatorPermit", netuid):       {0x0c, 0x00, 0x01, 0x00},
		key("Uids", netuid, validator[:]):    {0x01, 0x00},
		key("Keys", netuid, encodeNetuid(1)): validator[:],
	}
	blocks := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpcRequest nativeRpcRequest
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil || rpcRequest.Method != "state_getStorage" {
			t.Errorf("unexpected request %s", rpcRequest.Method)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var storageKey, block string
		json.Unmarshal(rpcRequest.Params[0], &storageKey)
		json.Unmarshal(rpcRequest.Params[1], &block)
		blocks = append(blocks, block)
		var result any
		if value, ok := values[storageKey]; ok {
			result = codec.HexEncodeToString(value)
		}
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.Id, "result": result})
	}))
	defer server.Close()
	reader := newTestSubstrateReader(t, server.URL, metadata)
	role, err := reader.role(context.Background(), 25, validator, at)
	if err != nil || role != (neuronRole{registered: true, uid: 1, validatorPermit: true}) {
		t.Fatalf("role %+v err %v", role, err)
	}
	for _, block := range blocks {
		if block != types.Hash(at).Hex() {
			t.Fatalf("read at %s", block)
		}
	}
	// An unregistered hotkey has no UID and no permit.
	role, err = reader.role(context.Background(), 25, [32]byte{0x77}, at)
	if err != nil || role.registered || role.validatorPermit {
		t.Fatalf("unregistered role %+v err %v", role, err)
	}
	// A missing permit census is an error, not "no permit".
	delete(values, key("ValidatorPermit", netuid))
	if _, err := reader.role(context.Background(), 25, validator, at); err == nil {
		t.Fatal("read a role without ValidatorPermit")
	}
}
