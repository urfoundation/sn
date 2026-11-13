// Complete header bytes and canonical closing checks protect every retained
// state coordinate, including readers that do not select a runtime themselves.
package crv4

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/gorilla/websocket"
)

// Actual schedule, nonce and weight readers keep their different zero results;
// none may label them with a forged header or changed canonical checkpoint.
func TestAdjacentHeaderStateReadersRejectUncommittedCoordinates(t *testing.T) {
	for _, reader := range []string{"nonce", "weights", "schedule"} {
		for _, fault := range []string{"none", "number", "digest", "closing"} {
			fixture := newAccountNonceTestFixture(t, t.Context(), nil)
			client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
			original := client.callContext
			storageRead := false
			client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
				if method == "state_getStorage" {
					storageRead = true
					return setRuntimeIdentityTestResult(result, nil)
				}
				if method == "chain_getHeader" && (fault == "number" || fault == "digest") {
					header, _ := receiptTestHeader(t, types.Hash{32}, 42, nil, 1)
					if fault == "number" {
						header.Number = 41
					}
					if target, ok := result.(*types.Header); ok {
						*target = header
						return nil
					}
					encoded, err := json.Marshal(receiptTestHeaderWire(header))
					if err != nil {
						return err
					}
					var wire map[string]any
					if err := json.Unmarshal(encoded, &wire); err != nil {
						return err
					}
					if fault == "digest" {
						wire["digest"] = map[string]any{"logs": []string{"0x08"}}
					}
					return setRuntimeIdentityTestResult(result, wire)
				}
				if method == "chain_getBlockHash" && fault == "closing" && storageRead {
					return setRuntimeIdentityTestResult(result, types.Hash{0xff}.Hex())
				}
				return original(ctx, result, method, args...)
			}
			var err error
			switch reader {
			case "nonce":
				nonce, hash, number, readErr := fixture.chain.FinalizedAccountNonceContext(t.Context(), fixture.publicKey)
				err = readErr
				if err != nil && (nonce != 0 || hash != (types.Hash{}) || number != 0) {
					t.Fatal("failed nonce read retained partial evidence")
				}
			case "weights":
				row, number, hash, readErr := fixture.chain.WeightsAtFinalizedContext(t.Context(), 7, 3)
				err = readErr
				if err != nil && (row != nil || hash != (types.Hash{}) || number != 0) {
					t.Fatal("failed weights read retained partial evidence")
				}
			case "schedule":
				state, readErr := fixture.chain.EpochScheduleStateAtContext(t.Context(), 7, fixture.blockHash)
				err = readErr
				if err != nil && state != nil {
					t.Fatal("failed schedule retained partial evidence")
				}
			}
			if (err == nil) != (fault == "none") {
				t.Fatalf("%s retained uncommitted evidence under %s: %v", reader, fault, err)
			}
			if fault == "number" || fault == "digest" {
				if storageRead || !strings.Contains(err.Error(), "header SCALE hash") {
					t.Fatalf("%s reached storage before authenticating %s: %v", reader, fault, err)
				}
			}
		}
	}
}

// The exact same current and finalized heights do not authenticate changed
// state roots in the lower reader used by both upload and startup eligibility.
func TestAdjacentHeaderIdentityRejectsSubstitutedRoots(t *testing.T) {
	for _, finalized := range []bool{false, true} {
		fixture := newValidatorIdentityTestFixture(t)
		hash := fixture.query.BlockHash
		if finalized {
			hash = fixture.finalized
		}
		header := fixture.headers[hash.Hex()]
		header.StateRoot = types.Hash{0xaa}
		fixture.headers[hash.Hex()] = header
		got, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || got != (ValidatorIdentityObservation{}) || !strings.Contains(err.Error(), "header SCALE hash") || len(fixture.storageCalls) != 0 {
			t.Fatalf("identity retained changed root finalized=%t: %+v %v", finalized, got, err)
		}
	}
}

// Parent execution and inclusion post-state remain separate; authenticating
// their runtime hashes cannot replace authenticating the parent commitment.
func TestAdjacentHeaderCheckpointRejectsSubstitutedParent(t *testing.T) {
	fixture, query, _ := newEVMCheckpointTestFixture(t)
	parent := fixture.blockHashes[99]
	header := fixture.headers[parent.Hex()]
	header.StateRoot = types.Hash{0xaa}
	fixture.headers[parent.Hex()] = header
	got, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
	if err == nil || got != (EVMCheckpointObservation{}) || !strings.Contains(err.Error(), "header SCALE hash") || len(fixture.storageCalls) != 0 {
		t.Fatalf("checkpoint retained substituted parent commitment: %+v %v", got, err)
	}
}

// Every boundary remains canonical after the first-insertion storage reads;
// changing only one boundary never yields a partially valid checkpoint.
func TestAdjacentHeaderCheckpointRejectsClosingReplacement(t *testing.T) {
	for _, height := range []uint64{99, 100, 103} {
		fixture, query, _ := newEVMCheckpointTestFixture(t)
		fixture.after = func(method string, args ...any) {
			if method == "state_getStorage" {
				fixture.blockHashes[height] = types.Hash{0xcc}
			}
		}
		got, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
		if err == nil || got != (EVMCheckpointObservation{}) {
			t.Fatalf("checkpoint retained closing replacement at%d: %+v %v", height, got, err)
		}
	}
}

// The ordinary fleet status path must authenticate coordinates too, including
// calls that have no prior source receipt from which to borrow a known number.
func TestAdjacentHeaderFleetCommitmentRejectsSubstitution(t *testing.T) {
	for _, fault := range []string{"none", "number", "closing"} {
		header, block := receiptTestHeader(t, types.Hash{9}, 42, nil, 1)
		registration, last := commitmentContextTestStorage(t, [32]byte{7}, 42)
		reads := 0
		client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
			switch method {
			case "state_getStorage":
				reads++
				if reads == 1 {
					return receiptTestAssign(result, registration)
				}
				return receiptTestAssign(result, last)
			case "chain_getHeader":
				supplied := header
				if fault == "number" {
					supplied.Number = 41
				}
				return receiptTestAssign(result, receiptTestHeaderWire(supplied))
			case "chain_getBlockHash":
				if fault == "closing" {
					return receiptTestAssign(result, types.Hash{0xee}.Hex())
				}
				return receiptTestAssign(result, block.Hex())
			}
			return fmt.Errorf("unexpected commitment request %s", method)
		}}
		native := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: commitmentContextTestMetadata()}
		got, err := native.FleetCommitmentAtContext(t.Context(), 7, [32]byte{1}, block)
		if fault == "none" {
			if err != nil || got == nil || got.FinalizedAt != 42 {
				t.Fatalf("actual commitment failed: %+v %v", got, err)
			}
		} else if err == nil || got != nil {
			t.Fatalf("fleet commitment accepted %s evidence: %+v %v", fault, got, err)
		}
	}
}

// A real local websocket sends one finality notification and actual body/event
// bytes. Receipt coordinates come from that body, never a second header reply.
func TestAdjacentHeaderWatchUsesCommittedReceiptNumber(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		raw := []byte{1, 2}
		header, block := receiptTestHeader(t, types.Hash{4}, 5, [][]byte{raw}, 0)
		metadata := types.NewMetadataV14()
		metadata.MagicNumber = types.MagicNumber
		id := types.NewSi1LookupTypeIDFromUInt(0)
		metadata.AsMetadataV14.Lookup.Types = []types.PortableTypeV14{{ID: id, Type: types.Si1Type{Def: types.Si1TypeDef{IsVariant: true, Variant: types.Si1TypeDefVariant{Variants: []types.Si1Variant{{Name: "ExtrinsicSuccess", Index: 0}}}}}}}
		metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{Name: "System", Index: 0, HasEvents: true, Events: types.EventMetadataV14{Type: id}, HasStorage: true, Storage: types.StorageMetadataV14{Prefix: "System", Items: []types.StorageEntryMetadataV14{{Name: "Events", Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Type: types.StorageEntryTypeV14{IsPlainType: true, AsPlainType: id}}}}}}
		encodedMetadata, err := codec.EncodeToHex(metadata)
		if err != nil {
			t.Fatal(err)
		}
		metadata, _, err = DecodeRuntimeMetadata(encodedMetadata)
		if err != nil {
			t.Fatal(err)
		}
		peerDone := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
			if err != nil {
				t.Error(err)
				close(peerDone)
				return
			}
			defer close(peerDone)
			defer connection.Close()
			for {
				var call chainContextRPCRequest
				if err := connection.ReadJSON(&call); err != nil {
					return
				}
				var result any
				switch call.Method {
				case "author_submitAndWatchExtrinsic":
					result = "1"
				case "author_unwatchExtrinsic":
					result = true
				case "chain_getHeader":
					changed := header
					changed.Number = 99
					result = receiptTestHeaderWire(changed)
				case "chain_getBlock":
					result = map[string]any{"block": map[string]any{"header": receiptTestHeaderWire(header), "extrinsics": []string{"0x0102"}}}
				case "state_getStorage":
					result = "0x040000000000000000"
				case "chain_getBlockHash":
					result = block.Hex()
					if replaced {
						result = types.Hash{0xea}.Hex()
					}
				default:
					t.Errorf("unexpected watch Rpc %s", call.Method)
					return
				}
				if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}); err != nil {
					return
				}
				if call.Method == "author_submitAndWatchExtrinsic" {
					if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "author_extrinsicUpdate", "params": map[string]any{"subscription": "1", "result": map[string]any{"finalized": block.Hex()}}}); err != nil {
						return
					}
				}
			}
		}))
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		transport, err := gsrpcgeth.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"))
		if err != nil {
			cancel()
			server.Close()
			t.Fatal(err)
		}
		native := &Chain{API: &gsrpc.SubstrateAPI{Client: &contextSubstrateClient{Client: transport, url: "ws://synthetic-watch"}}, Meta: metadata}
		got, err := native.SubmitRawAndWatchFinalized(ctx, "0x0102")
		cancel()
		transport.Close()
		<-peerDone
		server.Close()
		if replaced {
			if err == nil || got != nil {
				t.Fatalf("watch retained replaced canonical receipt: %+v %v", got, err)
			}
		} else if err != nil || got == nil || got.BlockNumber != 5 || got.BlockHash != block {
			t.Fatalf("watch retained uncommitted convenience height instead of body5: %+v %v", got, err)
		}
	}
}
