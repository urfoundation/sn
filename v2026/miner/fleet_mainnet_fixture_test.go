// Real local Rpc transport exercises command wiring. Public protocol metadata
// is reused with synthetic runtime/network identities, storage and keys only.
package miner

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Mutation hooks are explicit Rpc barriers, never timing or scheduler probes.
type fleetMainnetTestFixture struct {
	stateLock             sync.Mutex
	authority             fleetMainnetRuntimeAuthority
	manifest              *protocol.FleetManifest
	opts                  docopt.Opts
	server                *httptest.Server
	genesis               types.Hash
	head                  types.Hash
	receiptBlock          types.Hash
	metadata              string
	code                  string
	version               crv4.RuntimeVersionIdentity
	calls                 map[string]int
	storage               map[string]string
	hook                  func(string)
	evmChainId            uint64
	evmTx                 common.Hash
	evmSigned             *ethtypes.Transaction
	evmReceipt            *ethtypes.Receipt
	nativeSigned          string
	finalizedNumber       uint64
	nativeBlocks          map[uint64]types.Hash
	nativeHeaders         map[uint64]types.Header
	nativeBodyOverrides   map[uint64]any
	nativeBodyReads       map[uint64]int
	nativeRuntimeUpdateAt uint64
	nativeReceiptNumber   uint64
	nativeThrough         uint64
	historicalVersions    map[string]crv4.RuntimeVersionIdentity
	nativeNonce           uint64
	evmNonce              uint64
	evmBlockNumber        uint64
	nativeBroadcast       bool
	nativeDropAck         bool
	nativeDispatchFailure bool
	nativeBlockMissing    bool
	missingMapping        bool
	mappingAtParent       bool
	revoked               bool
	after                 func(string)
	rpcFailure            func(string) error
	responseStatus        func(string, []json.RawMessage) int
}

// Owns synthetic public metadata, private test seeds, and local Rpc lifecycle.
func newFleetMainnetTestFixture(t *testing.T) *fleetMainnetTestFixture {
	t.Helper()
	_, _, _, raw := fleetRuntimeCurrentTestArtifactInputs(t)
	metadata, metadataHash, err := crv4.DecodeRuntimeMetadata(codec.HexEncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	self := &fleetMainnetTestFixture{genesis: types.Hash{0x51}, metadata: codec.HexEncodeToString(raw), code: (types.Hash{0x54}).Hex(), calls: map[string]int{}, storage: map[string]string{}, evmChainId: 964,
		nativeBodyOverrides: map[uint64]any{}, nativeBodyReads: map[uint64]int{}}
	self.finalizedNumber, self.evmNonce, self.evmBlockNumber = 100, 1, 102
	self.nativeReceiptNumber, self.nativeThrough = 102, 104
	self.stateLock.Lock()
	err = self.rebuildNativeBlocksWithLock()
	self.stateLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	self.historicalVersions = map[string]crv4.RuntimeVersionIdentity{}
	self.version = crv4.RuntimeVersionIdentity{SpecName: "synthetic-subtensor", SpecVersion: 8001, TransactionVersion: 1, StateVersion: 1}
	self.authority = fleetMainnetRuntimeAuthority{Schema: fleetMainnetRuntimeAuthoritySchema, NativeChain: "Synthetic Main Network", GenesisHash: self.genesis.Hex(), EvmChainId: 964, Netuid: 25, Coordinator: strings.ToLower(common.Address{0x55}.Hex()), RuntimeSourceCommit: strings.Repeat("ab", 20), RuntimeReviewScope: fleetMainnetRuntimeReviewScope, RuntimeReviewSha256: strings.Repeat("cd", 32), RuntimeVersion: self.version, RuntimeCodeHash: self.code, RuntimeMetadataHash: metadataHash}
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	seed := bytes.Repeat([]byte{0x56}, 32)
	hotkey, err := crv4.KeypairFromSeed([32]byte(seed))
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x57}, 32))
	var clientKey [32]byte
	copy(clientKey[:], clientPrivate.Public().(ed25519.PublicKey))
	self.manifest = &protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: 964, Netuid: 25, Coordinator: [20]byte{0x55}, FleetID: [32]byte{0x58}, Hotkey: hotkey.PublicKey(), Generation: 2, Members: []protocol.FleetMember{{ClientID: [16]byte{0x59}, ClientKey: clientKey}}}
	self.opts = docopt.Opts{"--manifest": filepath.Join(directory, "fleet.json"), "--mainnet-runtime-authority": filepath.Join(directory, "authority.json"), "--hotkey_seed_file": filepath.Join(directory, "hotkey.seed"), "--coldkey_seed_file": filepath.Join(directory, "coldkey.seed"), "--client_seed_file": filepath.Join(directory, "client.seed"), "--relayer_key_file": filepath.Join(directory, "relayer.seed"), "--client_id": hex.EncodeToString(self.manifest.Members[0].ClientID[:]), "--valid_from_epoch": "10", "--valid_to_epoch": "20", "--effective_epoch": "15", "--dry-run": true}
	manifestBytes, err := self.manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		option string
		data   []byte
	}{
		{option: "--manifest", data: manifestBytes}, {option: "--hotkey_seed_file", data: seed}, {option: "--coldkey_seed_file", data: bytes.Repeat([]byte{0x60}, 32)}, {option: "--client_seed_file", data: clientPrivate.Seed()}, {option: "--relayer_key_file", data: []byte(strings.Repeat("61", 32))},
	} {
		if err := os.WriteFile(fleetOpt(self.opts, item.option), item.data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	self.writeAuthority(t)
	coldkey, err := crv4.KeypairFromSeed([32]byte(bytes.Repeat([]byte{0x60}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	put := func(pallet, name string, value any, args ...[]byte) {
		key, err := types.CreateStorageKey(metadata, pallet, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := codec.EncodeToHex(value)
		if err != nil {
			t.Fatal(err)
		}
		self.storage[key.Hex()] = encoded
	}
	for _, name := range []string{"Burn", "MinBurn", "MaxBurn"} {
		put("SubtensorModule", name, types.U64(100), snchain.NetuidArg(25))
	}
	put("SubtensorModule", "BurnHalfLife", types.U16(10), snchain.NetuidArg(25))
	put("SubtensorModule", "BurnIncreaseMult", types.NewU128(*big.NewInt(100)), snchain.NetuidArg(25))
	account := snchain.AccountInfo{}
	account.Data.Free = 1000000
	account.Data.Flags = types.NewU128(*big.NewInt(0))
	coldkeyPublic := coldkey.PublicKey()
	put("System", "Account", account, coldkeyPublic[:])
	hash, err := self.manifest.CommitmentHash()
	if err != nil {
		t.Fatal(err)
	}
	info, err := crv4.EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	registration := binary.LittleEndian.AppendUint64(nil, 1)
	registration = binary.LittleEndian.AppendUint32(registration, 100)
	registration = append(registration, info...)
	key, err := types.CreateStorageKey(metadata, "Commitments", "CommitmentOf", snchain.NetuidArg(25), self.manifest.Hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	self.storage[key.Hex()] = codec.HexEncodeToString(registration)
	put("Commitments", "LastCommitment", types.U32(100), snchain.NetuidArg(25), self.manifest.Hotkey[:])
	self.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(io.LimitReader(request.Body, 8*1024*1024)).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.calls[call.Method]++
		if self.hook != nil {
			self.hook(call.Method)
		}
		var result any
		str := func(index int) string {
			var value string
			if index < len(call.Params) {
				_ = json.Unmarshal(call.Params[index], &value)
			}
			return value
		}
		switch call.Method {
		case "system_chain":
			result = self.authority.NativeChain
		case "chain_getBlockHash":
			var number uint64
			if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &number) != nil {
				t.Errorf("invalid native height: %s", call.Params)
				return
			}
			if number == 0 {
				result = self.genesis.Hex()
			} else if hash, ok := self.nativeBlocks[number]; ok {
				result = hash.Hex()
			} else {
				t.Errorf("unexpected native height %d", number)
			}
		case "chain_getFinalizedHead":
			result = self.nativeBlocks[self.finalizedNumber].Hex()
		case "state_getRuntimeVersion":
			result = self.version
			if historical, ok := self.historicalVersions[str(0)]; ok {
				result = historical
			}
		case "state_getMetadata":
			result = self.metadata
		case "state_getStorageHash":
			if str(0) != "0x3a636f6465" {
				t.Error("code key differs")
			}
			result = self.code
		case "state_getStorage":
			mappingKey, err := types.CreateStorageKey(metadata, "Ethereum", "BlockHash", append(binary.LittleEndian.AppendUint64(nil, self.evmBlockNumber), make([]byte, 24)...))
			if err != nil {
				t.Error(err)
				return
			}
			if value, ok := self.storage[str(0)]; ok {
				result = value
			}
			if str(0) == mappingKey.Hex() && !self.missingMapping && (str(1) == self.receiptBlock.Hex() || (self.mappingAtParent && (str(1) == self.nativeBlocks[101].Hex() || str(1) == self.head.Hex()))) {
				result = (common.Hash{0x62}).Hex()
			}
		case "chain_getHeader":
			for number, hash := range self.nativeBlocks {
				if str(0) == hash.Hex() {
					result = self.nativeHeaderWireWithLock(number)
				}
			}
		case "chain_getBlock":
			for number, hash := range self.nativeBlocks {
				if str(0) != hash.Hex() {
					continue
				}
				self.nativeBodyReads[number]++
				if override, exists := self.nativeBodyOverrides[number]; exists {
					result = override
				} else if !self.nativeBlockMissing {
					extrinsics := []string{}
					if self.nativeBroadcast && number == self.nativeReceiptNumber {
						extrinsics = append(extrinsics, self.nativeSigned)
					}
					result = map[string]any{"block": map[string]any{"header": self.nativeHeaderWireWithLock(number), "extrinsics": extrinsics}}
				}
			}
		case "system_accountNextIndex":
			result = self.nativeNonce
		case "payment_queryInfo":
			self.nativeSigned = str(0)
			result = map[string]any{"partialFee": "1"}
		case "eth_chainId":
			result = fmt.Sprintf("0x%x", self.evmChainId)
		case "eth_call":
			var invocation struct {
				To    string `json:"to"`
				Data  string `json:"data"`
				Input string `json:"input"`
			}
			if len(call.Params) == 0 || json.Unmarshal(call.Params[0], &invocation) != nil {
				t.Error("invalid eth_call")
				return
			}
			if !strings.EqualFold(invocation.To, self.authority.Coordinator) {
				t.Error("coordinator target changed")
			}
			data := invocation.Data
			if data == "" {
				data = invocation.Input
			}
			result = "0x"
			contractAbi, err := stabi.STCoordinatorMetaData.ParseABI()
			if err != nil {
				t.Error(err)
				return
			}
			if strings.HasPrefix(data, hexutilTest(stCoordinator.PackGetFleetBinding(self.manifest.Members[0].ClientID)[:4])) {
				to := uint64(20)
				if self.revoked {
					to = 14
				}
				encoded, err := contractAbi.Methods["getFleetBinding"].Outputs.Pack(stabi.STCoordinatorBindingRecord{FleetId: self.manifest.FleetID, Hotkey: self.manifest.Hotkey, ClientKey: clientKey, CommitmentHash: hash, Generation: 2, ValidFromEpoch: 10, ValidToEpoch: to, Uid: 7})
				if err != nil {
					t.Error(err)
					return
				}
				result = hexutilTest(encoded)
			} else if strings.HasPrefix(data, hexutilTest(stCoordinator.PackFleetRevokeDigest(self.manifest.Members[0].ClientID, 2, 15)[:4])) {
				digest, err := (protocol.FleetRevoke{ChainID: 964, Netuid: 25, Coordinator: self.manifest.Coordinator, ClientID: self.manifest.Members[0].ClientID, Generation: 2, EffectiveEpoch: 15}).Digest()
				if err != nil {
					t.Error(err)
					return
				}
				result = hexutilTest(digest[:])
			}
		case "eth_estimateGas":
			result = "0x5208"
		case "eth_getTransactionCount":
			result = fmt.Sprintf("0x%x", self.evmNonce)
		case "eth_gasPrice":
			result = "0x1"
		case "eth_sendRawTransaction":
			raw, err := hex.DecodeString(strings.TrimPrefix(str(0), "0x"))
			var tx ethtypes.Transaction
			if err != nil || tx.UnmarshalBinary(raw) != nil {
				t.Error("invalid signed EVM transaction")
				return
			}
			self.evmTx = tx.Hash()
			self.evmSigned = &tx
			self.evmReceipt = &ethtypes.Receipt{Status: 1, CumulativeGasUsed: 21000, TxHash: self.evmTx, GasUsed: 21000, BlockHash: common.Hash{0x62}, BlockNumber: new(big.Int).SetUint64(self.evmBlockNumber), Logs: []*ethtypes.Log{}}
			self.finalizedNumber = 102
			self.evmNonce = tx.Nonce() + 1
			contractAbi, err := stabi.STCoordinatorMetaData.ParseABI()
			if err != nil {
				t.Error(err)
				return
			}
			var clientTopic common.Hash
			copy(clientTopic[:], self.manifest.Members[0].ClientID[:])
			log := &ethtypes.Log{Address: common.Address(self.manifest.Coordinator), BlockNumber: self.evmBlockNumber, BlockHash: self.evmReceipt.BlockHash, TxHash: self.evmTx}
			if bytes.Equal(tx.Data()[:4], contractAbi.Methods["bindFleetMember"].ID) {
				event := contractAbi.Events["FleetBound"]
				log.Topics = []common.Hash{event.ID, clientTopic, common.Hash(self.manifest.FleetID), common.Hash(self.manifest.Hotkey)}
				log.Data, err = event.Inputs.NonIndexed().Pack(uint16(7), uint64(2), uint64(10), uint64(20))
			} else {
				event := contractAbi.Events["FleetBindingRevoked"]
				log.Topics = []common.Hash{event.ID, clientTopic}
				log.Data, err = event.Inputs.NonIndexed().Pack(uint64(2), uint64(15))
				self.revoked = true
			}
			if err != nil {
				t.Error(err)
				return
			}
			self.evmReceipt.Logs = []*ethtypes.Log{log}
			result = self.evmTx.Hex()
		case "eth_getTransactionReceipt":
			result = self.evmReceipt
		case "eth_getTransactionByBlockHashAndIndex":
			result = self.evmSigned
		case "eth_getBlockByNumber":
			result = map[string]any{"number": fmt.Sprintf("0x%x", self.evmBlockNumber), "hash": (common.Hash{0x62}).Hex()}
		default:
			t.Errorf("unexpected fleet mainnet Rpc %s", call.Method)
		}
		if self.after != nil {
			self.after(call.Method)
		}
		if self.responseStatus != nil {
			if status := self.responseStatus(call.Method, call.Params); status != 0 {
				http.Error(writer, "synthetic transient RPC response", status)
				return
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		if self.rpcFailure != nil {
			if err := self.rpcFailure(call.Method); err != nil {
				_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "error": map[string]any{"code": -32000, "message": err.Error()}})
				return
			}
		}
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(self.server.Close)
	self.opts["--rpc"] = []string{self.server.URL}
	self.opts["--substrate"] = []string{self.server.URL}
	t.Setenv("URNETWORK_STATE_DIR", filepath.Join(directory, "state"))
	return self
}

// Encodes synthetic bytes with the Rpc hex prefix.
func hexutilTest(raw []byte) string { return "0x" + hex.EncodeToString(raw) }

// Pins the exact synthetic approval bytes independently of Rpc responses.
func (self *fleetMainnetTestFixture) writeAuthority(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(self.authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fleetOpt(self.opts, "--mainnet-runtime-authority"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	self.opts["--mainnet-runtime-authority-sha256"] = hex.EncodeToString(hash[:])
}

// Reads observed Rpc counts after the deterministic operation boundary.
func (self *fleetMainnetTestFixture) count(method string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.calls[method]
}
