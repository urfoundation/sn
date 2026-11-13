// Scripted JSON preserves malformed/missing wire values. Complete fixtures use
// independent SDK SCALE header encoding and independently generated trie vectors.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// Hashes real SDK headers, never a caller-supplied verified flag. Body roots
// receive separate independent Rust-vector coverage below.
func receiptTestHeader(t *testing.T, parent types.Hash, number uint64, body [][]byte, layout uint8) (types.Header, types.Hash) {
	t.Helper()
	root, err := receiptExtrinsicsRoot(body, layout)
	if err != nil {
		t.Fatal(err)
	}
	header := types.Header{ParentHash: parent, Number: types.BlockNumber(number), StateRoot: types.Hash{3}, ExtrinsicsRoot: root, Digest: types.Digest{}}
	raw, err := codec.Encode(header)
	if err != nil {
		t.Fatal(err)
	}
	return header, types.Hash(blake2b.Sum256(raw))
}

// Substrate serializes its number as a 0x-prefixed quantity. The pinned Go SDK
// hashes the correct SCALE bytes but its JSON marshaler omits that wire prefix.
func receiptTestHeaderWire(header types.Header) any {
	return struct {
		types.Header
		Number string `json:"number"`
	}{Header: header, Number: fmt.Sprintf("0x%x", uint64(header.Number))}
}

// The RPC quantity spelling is independent of SDK JSON marshaling. Preserve
// the SCALE hash while refusing omitted, unprefixed and overflowing numbers.
func TestReceiptHeaderRequiresNativeWireQuantity(t *testing.T) {
	header, hash := receiptTestHeader(t, types.Hash{4}, 5, nil, 0)
	var decoded receiptHeader
	if err := receiptTestAssign(&decoded, receiptTestHeaderWire(header)); err != nil {
		t.Fatal(err)
	}
	if decoded.Number != "0x5" {
		t.Fatalf("native RPC number spelling changed: %q", decoded.Number)
	}
	if number, err := decoded.authenticate(hash); err != nil || number != 5 {
		t.Fatalf("canonical wire quantity lost SDK header commitment: %d %v", number, err)
	}
	for _, number := range []string{"", "5", "0x", "0x100000000", "0x-5", "0xzz"} {
		decoded.Number = number
		if _, err := decoded.authenticate(hash); err == nil {
			t.Fatalf("invalid native wire quantity accepted: %q", number)
		}
	}
}

// Each client owns complete headers and independently replaceable body JSON.
type receiptScanTestFixture struct {
	chain   *Chain
	headers map[uint64]types.Header
	hashes  map[uint64]types.Hash
	bodies  map[uint64]json.RawMessage
	through uint64
	calls   []string
	fault   func(context.Context, any, string, ...any) (bool, error)
}

// Adjacent real headers bind each parent, while RPC hooks inject one concrete
// response defect at a time without substituting the receipt finder.
func newReceiptScanTestFixture(t *testing.T, bodies [][][]byte, layout uint8) *receiptScanTestFixture {
	t.Helper()
	self := &receiptScanTestFixture{headers: map[uint64]types.Header{}, hashes: map[uint64]types.Hash{}, bodies: map[uint64]json.RawMessage{}, through: uint64(len(bodies)) + 4}
	parent := types.Hash{4}
	for index, body := range bodies {
		number := uint64(index) + 5
		header, hash := receiptTestHeader(t, parent, number, body, layout)
		self.headers[number], self.hashes[number] = header, hash
		encoded := make([]string, len(body))
		for index, raw := range body {
			encoded[index] = codec.HexEncodeToString(raw)
		}
		raw, err := json.Marshal(map[string]any{"block": map[string]any{"header": receiptTestHeaderWire(header), "extrinsics": encoded}})
		if err != nil {
			t.Fatal(err)
		}
		self.bodies[number] = raw
		parent = hash
	}
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		self.calls = append(self.calls, method)
		if self.fault != nil {
			if handled, err := self.fault(ctx, target, method, args...); handled || err != nil {
				return err
			}
		}
		switch method {
		case "chain_getFinalizedHead":
			return receiptTestAssign(target, self.hashes[self.through].Hex())
		case "chain_getBlockHash":
			if len(args) == 1 {
				if number, ok := args[0].(uint64); ok {
					return receiptTestAssign(target, self.hashes[number].Hex())
				}
			}
		case "chain_getHeader", "chain_getBlock":
			for number, hash := range self.hashes {
				if len(args) == 1 && args[0] == hash.Hex() {
					if method == "chain_getHeader" {
						return receiptTestAssign(target, receiptTestHeaderWire(self.headers[number]))
					}
					return json.Unmarshal(self.bodies[number], target)
				}
			}
		}
		return fmt.Errorf("unexpected synthetic receipt RPC %s %v", method, args)
	}}
	self.chain = &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	return self
}

// Canonical empty bodies are valid absence, including across a layout change.
// A real byte hash is found at its exact later canonical block.
func TestReceiptScanAuthenticatesCompleteBodies(t *testing.T) {
	for layout := uint8(0); layout <= 1; layout++ {
		transaction := []byte(strings.Repeat("synthetic-extrinsic", 4))
		fixture := newReceiptScanTestFixture(t, [][][]byte{{}, {[]byte{1, 2}}, {transaction}}, layout)
		hash := types.Hash(blake2b.Sum256(transaction))
		receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(t.Context(), hash, 5)
		if err != nil || !found || receipt == nil || receipt.BlockNumber != 7 || receipt.BlockHash != fixture.hashes[7] || receipt.ExtrinsicHash != hash {
			t.Fatalf("layout %d complete canonical receipt: %+v %t %v", layout, receipt, found, err)
		}
		if receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(t.Context(), types.Hash{99}, 5); err != nil || found || receipt != nil {
			t.Fatalf("layout %d complete absence: %+v %t %v", layout, receipt, found, err)
		}
	}
}

// No null, omitted, partial, substituted or truncated body can prove absence.
// Even an early matching transaction cannot hide corrupt later vector entries.
func TestReceiptScanRejectsIncompleteBodyEvidence(t *testing.T) {
	for _, fault := range []string{"null", "empty", "null-block", "missing-block", "missing-header", "null-header", "missing-vector", "null-vector", "truncated-vector", "changed-extrinsic", "malformed-tail", "wrong-height", "wrong-hash", "missing-number", "missing-digest", "null-logs", "missing-state"} {
		transaction := []byte{1, 2, 3}
		fixture := newReceiptScanTestFixture(t, [][][]byte{{transaction, []byte{4, 5}}}, 0)
		var decoded map[string]any
		if err := json.Unmarshal(fixture.bodies[5], &decoded); err != nil {
			t.Fatal(err)
		}
		body := decoded["block"].(map[string]any)
		header := body["header"].(map[string]any)
		switch fault {
		case "null":
			decoded = nil
		case "empty", "missing-block":
			decoded = map[string]any{}
		case "null-block":
			decoded["block"] = nil
		case "missing-header":
			delete(body, "header")
		case "null-header":
			body["header"] = nil
		case "missing-vector":
			delete(body, "extrinsics")
		case "null-vector":
			body["extrinsics"] = nil
		case "truncated-vector":
			body["extrinsics"] = []string{}
		case "changed-extrinsic":
			body["extrinsics"] = []string{"0x01"}
		case "malformed-tail":
			body["extrinsics"] = []string{codec.HexEncodeToString(transaction), "not-hex"}
		case "wrong-height":
			header["number"] = "0x6"
		case "wrong-hash":
			header["stateRoot"] = types.Hash{9}.Hex()
		case "missing-number":
			delete(header, "number")
		case "missing-digest":
			delete(header, "digest")
		case "null-logs":
			header["digest"] = map[string]any{"logs": nil}
		case "missing-state":
			delete(header, "stateRoot")
		}
		var err error
		fixture.bodies[5], err = json.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		for _, hash := range []types.Hash{{99}, types.Hash(blake2b.Sum256(transaction))} {
			fixture.calls = nil
			if receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(t.Context(), hash, 5); err == nil || found || receipt != nil {
				t.Fatalf("%s became receipt or absence: %+v %t %v", fault, receipt, found, err)
			}
			if len(fixture.calls) == 0 || fixture.calls[len(fixture.calls)-1] != "chain_getBlock" {
				t.Fatalf("%s did not reach its body fault: %v", fault, fixture.calls)
			}
		}
		fixture.chain.Meta = types.NewMetadataV14()
		if err := fixture.chain.VerifyFinalizedExtrinsicContext(t.Context(), fixture.hashes[5], types.Hash(blake2b.Sum256(transaction))); err == nil || !strings.Contains(err.Error(), "receipt") {
			t.Fatalf("%s bypassed direct receipt validation: %v", fault, err)
		}
	}
}

// The independent SDK omits tag8; append its exact one-byte modern SCALE log
// to an SDK-encoded header and independently hash the complete result.
func TestReceiptHeaderSupportsRuntimeUpdateDigest(t *testing.T) {
	header, _ := receiptTestHeader(t, types.Hash{4}, 5, nil, 0)
	raw, err := codec.Encode(header)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], 4, 8)
	hash := types.Hash(blake2b.Sum256(raw))
	var decoded receiptHeader
	if err := receiptTestAssign(&decoded, receiptTestHeaderWire(header)); err != nil {
		t.Fatal(err)
	}
	decoded.Digest.Logs = []string{"0x08"}
	if number, err := decoded.authenticate(hash); err != nil || number != 5 {
		t.Fatalf("modern runtime-update digest rejected: %d %v", number, err)
	}
	for _, digest := range []string{"0x0800", "0x01", "0x00", "0x0004", "0x04010203", "0x040102030404", "0x000100"} {
		decoded.Digest.Logs = []string{digest}
		if _, err := decoded.authenticate(hash); err == nil {
			t.Fatalf("malformed modern digest accepted: %s", digest)
		}
	}
}

// A same-height canonical lookup cannot excuse a body whose actual number is
// different or whose authenticated parent skips the preceding scanned block.
func TestReceiptScanRejectsDisconnectedCanonicalBodies(t *testing.T) {
	for _, wrongHeight := range []bool{false, true} {
		fixture := newReceiptScanTestFixture(t, [][][]byte{{}, {}, {}}, 0)
		selected := uint64(6)
		number := uint64(6)
		if wrongHeight {
			selected = 5
		}
		header, hash := receiptTestHeader(t, types.Hash{9}, number, nil, 0)
		fixture.headers[selected], fixture.hashes[selected] = header, hash
		var err error
		fixture.bodies[selected], err = json.Marshal(map[string]any{"block": map[string]any{"header": receiptTestHeaderWire(header), "extrinsics": []string{}}})
		if err != nil {
			t.Fatal(err)
		}
		if receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(t.Context(), types.Hash{99}, 5); err == nil || found || receipt != nil {
			t.Fatalf("disconnected canonical body accepted: wrong height %t: %+v %t %v", wrongHeight, receipt, found, err)
		}
	}
}

// Finality's number comes only from a full hash-authenticated header. A corrupt
// or changed canonical boundary cannot turn a scan into early successful absence.
func TestReceiptScanRejectsUnboundFinality(t *testing.T) {
	for _, fault := range []string{"null-header", "wrong-header", "short-hash", "zero-hash", "noncanonical", "changed-height"} {
		fixture := newReceiptScanTestFixture(t, [][][]byte{{}}, 0)
		canonicalReads := 0
		fixture.fault = func(_ context.Context, target any, method string, _ ...any) (bool, error) {
			if method == "chain_getHeader" && fault == "null-header" {
				return true, json.Unmarshal([]byte("null"), target)
			}
			if method == "chain_getHeader" && fault == "wrong-header" {
				header := fixture.headers[5]
				header.Number = 4
				return true, receiptTestAssign(target, receiptTestHeaderWire(header))
			}
			if method == "chain_getFinalizedHead" && (fault == "short-hash" || fault == "zero-hash") {
				value := "0x01"
				if fault == "zero-hash" {
					value = types.Hash{}.Hex()
				}
				return true, receiptTestAssign(target, value)
			}
			if method == "chain_getBlockHash" {
				canonicalReads++
				if fault == "noncanonical" || fault == "changed-height" && canonicalReads == 2 {
					return true, receiptTestAssign(target, types.Hash{9}.Hex())
				}
			}
			return false, nil
		}
		if receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(t.Context(), types.Hash{99}, 5); err == nil || found || receipt != nil {
			t.Fatalf("%s became final absence: %+v %t %v", fault, receipt, found, err)
		}
	}
}

// Caller cancellation remains a transport/lifecycle error; it must neither
// advance a later retained scan boundary nor invoke another physical read.
func TestReceiptScanCancellationPreservesUnknown(t *testing.T) {
	fixture := newReceiptScanTestFixture(t, [][][]byte{{}, {}}, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.fault = func(_ context.Context, _ any, method string, _ ...any) (bool, error) {
		if method == "chain_getBlock" {
			cancel()
			return true, context.Canceled
		}
		return false, nil
	}
	if receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(ctx, types.Hash{99}, 5); !errors.Is(err, context.Canceled) || found || receipt != nil {
		t.Fatalf("cancelled receipt scan changed outcome: %+v %t %v", receipt, found, err)
	}
	if fixture.calls[len(fixture.calls)-1] != "chain_getBlock" {
		t.Fatal("cancelled scan admitted another physical read")
	}
}
