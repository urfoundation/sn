// Shared recovery methods return facts only after complete byte admission.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
)

// Raw header ancestry and membership agree with independent fixture hashes.
// Truncation and caller cancellation cannot publish partial successful facts.
func TestReceiptSharedReadersRetainCompleteBodyAuthority(t *testing.T) {
	transaction := []byte{1, 2, 3}
	fixture := newReceiptScanTestFixture(t, [][][]byte{{transaction}}, 0)
	hash := types.Hash(blake2b.Sum256(transaction))
	number, parent, err := fixture.chain.ReceiptHeaderAtContext(t.Context(), fixture.hashes[5])
	if err != nil || number != 5 || parent != (types.Hash{4}) {
		t.Fatalf("shared authenticated header: %d %s %v", number, parent.Hex(), err)
	}
	for _, query := range []types.Hash{hash, {99}} {
		number, parent, found, err := fixture.chain.ReceiptBlockExtrinsicContext(t.Context(), fixture.hashes[5], query)
		if err != nil || number != 5 || parent != (types.Hash{4}) || found != (query == hash) {
			t.Fatalf("shared complete body membership: %d %s %t %v", number, parent.Hex(), found, err)
		}
	}
	raw, err := json.Marshal(map[string]any{"block": map[string]any{"header": receiptTestHeaderWire(fixture.headers[5]), "extrinsics": []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.bodies[5] = raw
	if number, parent, found, err := fixture.chain.ReceiptBlockExtrinsicContext(t.Context(), fixture.hashes[5], hash); err == nil || found || number != 0 || parent != (types.Hash{}) {
		t.Fatalf("shared truncated body supplied partial facts: %d %s %t %v", number, parent.Hex(), found, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reads := len(fixture.calls)
	if _, _, err := fixture.chain.ReceiptHeaderAtContext(ctx, fixture.hashes[5]); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, _, err := fixture.chain.ReceiptBlockExtrinsicContext(ctx, fixture.hashes[5], hash); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(fixture.calls) != reads {
		t.Fatal("cancelled shared receipt reader started another physical request")
	}
}
