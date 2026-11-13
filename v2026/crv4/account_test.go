package crv4

// Exercises the exact Subtensor account row used before replaying a retained
// transaction. The wire bytes are independent of the production decoder.

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Builds the reviewed four-u32, three-u64, one-u128 account layout directly.
func accountNonceTestBytes(nonce uint32) []byte {
	raw := make([]byte, 56)
	binary.LittleEndian.PutUint32(raw[0:4], nonce)
	binary.LittleEndian.PutUint32(raw[4:8], 2)
	binary.LittleEndian.PutUint32(raw[8:12], 3)
	binary.LittleEndian.PutUint32(raw[12:16], 4)
	binary.LittleEndian.PutUint64(raw[16:24], 12_345_678_901)
	binary.LittleEndian.PutUint64(raw[24:32], 22)
	binary.LittleEndian.PutUint64(raw[32:40], 33)
	raw[40], raw[55] = 1, 128
	return raw
}

// Holds one synthetic canonical block and the exact scripted storage value.
type accountNonceTestFixture struct {
	chain     *Chain
	ctx       context.Context
	publicKey [32]byte
	blockHash types.Hash
	storage   any
	calls     []string
}

// Requires callers to preserve both their context and metadata-derived key.
func newAccountNonceTestFixture(t *testing.T, ctx context.Context, storage any) *accountNonceTestFixture {
	t.Helper()
	fixture := &accountNonceTestFixture{ctx: ctx, publicKey: [32]byte{31}, blockHash: types.Hash{32}, storage: storage}
	header, hash := receiptTestHeader(t, types.Hash{32}, 42, nil, 1)
	fixture.blockHash = hash
	metadata := releaseContextStorageMetadata()
	key, err := types.CreateStorageKey(metadata, "System", "Account", fixture.publicKey[:])
	if err != nil {
		t.Fatal(err)
	}
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if ctx != fixture.ctx {
			return fmt.Errorf("account read replaced the caller context")
		}
		fixture.calls = append(fixture.calls, method)
		switch method {
		case "chain_getFinalizedHead":
			if len(args) != 0 {
				return fmt.Errorf("unexpected finalized-head arguments: %v", args)
			}
			return setRuntimeIdentityTestResult(result, fixture.blockHash.Hex())
		case "chain_getHeader":
			if len(args) != 1 || args[0] != fixture.blockHash.Hex() {
				return fmt.Errorf("header did not retain exact finalized hash: %v", args)
			}
			if target, ok := result.(*types.Header); ok {
				*target = header
				return nil
			}
			return setRuntimeIdentityTestResult(result, receiptTestHeaderWire(header))
		case "chain_getBlockHash":
			if len(args) != 1 || args[0] != uint64(42) {
				return fmt.Errorf("canonical account height changed: %v", args)
			}
			return setRuntimeIdentityTestResult(result, fixture.blockHash.Hex())
		case "state_getStorage":
			if len(args) != 2 || args[0] != key.Hex() || args[1] != fixture.blockHash.Hex() {
				return fmt.Errorf("account did not retain exact storage key and hash: %v", args)
			}
			return setRuntimeIdentityTestResult(result, fixture.storage)
		default:
			return fmt.Errorf("unexpected account read method: %s", method)
		}
	}}
	fixture.chain = &Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: metadata}
	return fixture
}

// A funded Subtensor account must produce its actual nonce rather than the
// generic u128-balance decoder's unexpected end-of-input failure.
func TestAccountNonceAtContextDecodesReviewedSubtensorRow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, expected := range []uint32{0, 7, ^uint32(0)} {
		fixture := newAccountNonceTestFixture(t, ctx, "0x"+hex.EncodeToString(accountNonceTestBytes(expected)))
		nonce, err := fixture.chain.AccountNonceAtContext(ctx, fixture.publicKey, fixture.blockHash)
		if err != nil || nonce != expected {
			t.Errorf("nonce=%d: read=%d error=%v", expected, nonce, err)
		}
		if !reflect.DeepEqual(fixture.calls, []string{"state_getStorage"}) {
			t.Errorf("nonce=%d: unexpected calls %v", expected, fixture.calls)
		}
	}
}

// Every compatibility wrapper must reach the same exact-block decoder.
func TestAccountNonceWrappersDecodeReviewedSubtensorRow(t *testing.T) {
	for _, finalized := range []bool{false, true} {
		fixture := newAccountNonceTestFixture(t, context.Background(), "0x"+hex.EncodeToString(accountNonceTestBytes(19)))
		var nonce uint32
		var err error
		expectedCalls := []string{"state_getStorage"}
		if finalized {
			var blockHash types.Hash
			var blockNumber uint64
			nonce, blockHash, blockNumber, err = fixture.chain.FinalizedAccountNonce(fixture.publicKey)
			if blockHash != fixture.blockHash || blockNumber != 42 {
				t.Errorf("finalized account returned wrong checkpoint: %s/%d", blockHash.Hex(), blockNumber)
			}
			expectedCalls = []string{"chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash", "state_getStorage", "chain_getBlockHash"}
		} else {
			nonce, err = fixture.chain.AccountNonceAt(fixture.publicKey, fixture.blockHash)
		}
		if err != nil || nonce != 19 || !reflect.DeepEqual(fixture.calls, expectedCalls) {
			t.Errorf("finalized=%v: nonce=%d error=%v calls=%v", finalized, nonce, err, fixture.calls)
		}
	}
}

// The finalized wrapper may not replace caller cancellation or mix latest
// account storage with the authenticated finalized checkpoint.
func TestFinalizedAccountNonceContextDecodesReviewedSubtensorRow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture := newAccountNonceTestFixture(t, ctx, "0x"+hex.EncodeToString(accountNonceTestBytes(23)))
	nonce, blockHash, blockNumber, err := fixture.chain.FinalizedAccountNonceContext(ctx, fixture.publicKey)
	if err != nil || nonce != 23 || blockHash != fixture.blockHash || blockNumber != 42 {
		t.Fatalf("nonce=%d checkpoint=%s/%d error=%v", nonce, blockHash.Hex(), blockNumber, err)
	}
	if !reflect.DeepEqual(fixture.calls, []string{"chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash", "state_getStorage", "chain_getBlockHash"}) {
		t.Fatalf("unexpected finalized account calls: %v", fixture.calls)
	}
}

// Only a JSON null represents an absent account; malformed present values
// cannot supply a zero or prefix nonce that might authorize a replay.
func TestAccountNonceAtContextRejectsWrongAccountLayout(t *testing.T) {
	for _, encoded := range []string{
		"0x",
		"0x" + hex.EncodeToString(accountNonceTestBytes(7)[:55]),
		"0x" + hex.EncodeToString(append(accountNonceTestBytes(7), 0)),
		"0x" + hex.EncodeToString(append(accountNonceTestBytes(7), make([]byte, 24)...)),
		"0xnot-hex",
	} {
		fixture := newAccountNonceTestFixture(t, context.Background(), encoded)
		if nonce, err := fixture.chain.AccountNonceAtContext(fixture.ctx, fixture.publicKey, fixture.blockHash); err == nil {
			t.Errorf("accepted malformed account of %d encoded bytes as nonce %d", len(encoded), nonce)
		}
	}
}

// Canonical absence remains a valid zero and is distinct from an empty value.
func TestAccountNonceAtContextPreservesAbsentAccount(t *testing.T) {
	fixture := newAccountNonceTestFixture(t, context.Background(), nil)
	nonce, err := fixture.chain.AccountNonceAtContext(fixture.ctx, fixture.publicKey, fixture.blockHash)
	if err != nil || nonce != 0 || !reflect.DeepEqual(fixture.calls, []string{"state_getStorage"}) {
		t.Fatalf("absent account nonce=%d error=%v calls=%v", nonce, err, fixture.calls)
	}
}
