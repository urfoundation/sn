// Native command tests use a local websocket subscription and real signed
// extrinsic bytes. The synthetic chain emits one explicit finality event.
package miner

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	"github.com/centrifuge/go-substrate-rpc-client/v4/registry/parser"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/gorilla/websocket"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The fixture proves its constructed event against the same metadata decoder
// the production finality owner consumes, without weakening dispatch checks.
func (self *fleetMainnetTestFixture) nativeWebsocket(t *testing.T, register bool) string {
	t.Helper()
	metadata, _, err := crv4.DecodeRuntimeMetadata(self.metadata)
	if err != nil {
		t.Fatal(err)
	}
	var palletIndex, successIndex byte
	eventName := "ExtrinsicSuccess"
	if self.nativeDispatchFailure {
		eventName = "ExtrinsicFailed"
	}
	found := false
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "System" || !pallet.HasEvents {
			continue
		}
		event := metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()]
		for _, variant := range event.Def.Variant.Variants {
			if string(variant.Name) == eventName {
				palletIndex = byte(pallet.Index)
				successIndex = byte(variant.Index)
				found = true
			}
		}
	}
	if !found {
		t.Fatal("success event absent")
	}
	dispatch, err := codec.Encode(types.DispatchInfo{Weight: types.NewWeight(types.NewUCompactFromUInt(0), types.NewUCompactFromUInt(0)), Class: types.DispatchClass{IsNormal: true}, PaysFee: types.Pays{IsYes: true}})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{4, 0, 0, 0, 0, 0, palletIndex, successIndex}
	if self.nativeDispatchFailure {
		failed, err := codec.Encode(types.DispatchError{IsBadOrigin: true})
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, failed...)
	}
	raw = append(raw, dispatch...)
	raw = append(raw, 0)
	eventRegistry, err := registry.NewFactory().CreateEventRegistry(metadata)
	if err != nil {
		t.Fatal(err)
	}
	eventStorage := types.NewStorageDataRaw(raw)
	decoded, err := parser.NewEventParser().ParseEvents(eventRegistry, &eventStorage)
	if err != nil || len(decoded) != 1 || decoded[0].Name != "System."+eventName || !decoded[0].Phase.IsApplyExtrinsic || decoded[0].Phase.AsApplyExtrinsic != 0 {
		t.Fatalf("synthetic finalized event: %v %v", decoded, err)
	}
	eventsKey, err := types.CreateStorageKey(metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	commitmentKey, err := types.CreateStorageKey(metadata, "Commitments", "CommitmentOf", snchain.NetuidArg(25), self.manifest.Hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(metadata, "Commitments", "LastCommitment", snchain.NetuidArg(25), self.manifest.Hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	uidKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "Uids", snchain.NetuidArg(25), self.manifest.Hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "Owner", self.manifest.Hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	coldkey, err := crv4.KeypairFromSeed([32]byte(bytes.Repeat([]byte{0x60}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		for {
			_, rawCall, err := connection.ReadMessage()
			if err != nil {
				return
			}
			var call struct {
				Id     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(rawCall, &call); err != nil {
				t.Error(err)
				return
			}
			if call.Method == "author_submitAndWatchExtrinsic" {
				self.stateLock.Lock()
				self.calls[call.Method]++
				if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &self.nativeSigned) != nil {
					self.stateLock.Unlock()
					t.Error("malformed native submission")
					return
				}
				self.storage[eventsKey.Hex()] = codec.HexEncodeToString(raw)
				self.nativeBroadcast = true
				if err := self.rebuildNativeBlocksWithLock(); err != nil {
					self.stateLock.Unlock()
					t.Error(err)
					return
				}
				self.nativeNonce++
				self.finalizedNumber = self.nativeReceiptNumber
				signer := self.manifest.Hotkey
				if register {
					signer = coldkey.PublicKey()
				}
				accountKey, accountErr := types.CreateStorageKey(metadata, "System", "Account", signer[:])
				account := snchain.AccountInfo{Nonce: types.U32(self.nativeNonce)}
				account.Data.Free = 1000000
				account.Data.Flags = types.NewU128(*big.NewInt(0))
				accountRaw, encodeErr := codec.EncodeToHex(account)
				if accountErr != nil || encodeErr != nil {
					self.stateLock.Unlock()
					t.Error("synthetic account nonce failed")
					return
				}
				self.storage[accountKey.Hex()] = accountRaw
				if register {
					self.storage[uidKey.Hex()] = codec.HexEncodeToString(binary.LittleEndian.AppendUint16(nil, 7))
					owner := coldkey.PublicKey()
					self.storage[ownerKey.Hex()] = codec.HexEncodeToString(owner[:])
				} else {
					record, err := codec.HexDecodeString(self.storage[commitmentKey.Hex()])
					if err != nil || len(record) < 12 {
						self.stateLock.Unlock()
						t.Error("invalid synthetic commitment")
						return
					}
					binary.LittleEndian.PutUint32(record[8:12], uint32(self.nativeReceiptNumber))
					self.storage[commitmentKey.Hex()] = codec.HexEncodeToString(record)
					self.storage[lastKey.Hex()] = codec.HexEncodeToString(binary.LittleEndian.AppendUint32(nil, uint32(self.nativeReceiptNumber)))
				}
				if self.hook != nil {
					self.hook(call.Method)
				}
				block := self.receiptBlock.Hex()
				dropAck := self.nativeDropAck
				self.stateLock.Unlock()
				if dropAck {
					return
				}
				if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": "synthetic-native-subscription"}); err != nil {
					t.Error(err)
					return
				}
				if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "author_extrinsicUpdate", "params": map[string]any{"subscription": "synthetic-native-subscription", "result": map[string]any{"finalized": block}}}); err != nil {
					t.Error(err)
					return
				}
			} else if call.Method == "author_unwatchExtrinsic" {
				if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": true}); err != nil {
					return
				}
			} else {
				recorder := httptest.NewRecorder()
				self.server.Config.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(rawCall)))
				response := recorder.Result()
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Error(err)
					return
				}
				if err := connection.WriteMessage(websocket.TextMessage, body); err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

// The publication handler signs and verifies a real local subscription receipt.
func TestFleetMainnetPublishCommandSignsAndVerifiesApprovedReceipt(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	if err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("chain_getBlock") < 2 || fixture.count("state_getStorageHash") < 5 {
		t.Fatal("publication did not bind signing and exact finalized receipt")
	}
}

// Inclusion under new code cannot inherit the signing runtime's approval.
func TestFleetMainnetPublishCommandRejectsIncludedUpgradeWithoutResend(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method == "author_submitAndWatchExtrinsic" {
			fixture.version.SpecVersion++
		}
	}
	fixture.stateLock.Unlock()
	if err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("receipt upgrade: %v", err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("state_getStorage") != 1 {
		t.Fatal("receipt upgrade retried or decoded commitment under unapproved runtime")
	}
}

// Registration retains independent authority through signing and UID readback.
func TestFleetMainnetRegisterCommandAppliesAndChecksReceiptAuthority(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, true)}
	fixture.opts["--apply"] = true
	fixture.opts["--dry-run"] = false
	if err := fleetRegister(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("chain_getBlock") < 2 || fixture.count("state_getStorageHash") < 5 {
		t.Fatal("registration lost runtime admission at signing/broadcast/receipt")
	}
}

// A fee reply forces runtime drift before the native broadcast boundary.
func TestFleetMainnetRegisterCommandRejectsUpgradeBeforeBroadcast(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, true)}
	fixture.opts["--apply"] = true
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method == "payment_queryInfo" {
			fixture.version.SpecVersion++
		}
	}
	fixture.stateLock.Unlock()
	if err := fleetRegister(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("upgrade after fee quotation: %v", err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 0 {
		t.Fatal("registration broadcast with changed authority")
	}
}

// An included registration is never resent to repair receipt admission.
func TestFleetMainnetRegisterCommandRejectsIncludedUpgradeWithoutResend(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, true)}
	fixture.opts["--apply"] = true
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method == "author_submitAndWatchExtrinsic" {
			fixture.version.SpecVersion++
		}
	}
	fixture.stateLock.Unlock()
	if err := fleetRegister(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("included registration upgrade: %v", err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatal("registration resent after receipt refusal")
	}
}

// A nonce reply forces runtime drift immediately before native signing.
func TestFleetMainnetRegisterRejectsUpgradeDuringNonceReadBeforeSigning(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method == "system_accountNextIndex" {
			fixture.version.SpecVersion++
		}
	}
	fixture.stateLock.Unlock()
	if err := fleetRegister(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("upgrade before signing was not refused: %v", err)
	}
	if fixture.count("payment_queryInfo") != 0 {
		t.Fatal("upgrade during nonce read reached signed-byte quotation")
	}
}
