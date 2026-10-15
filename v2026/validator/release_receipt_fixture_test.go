// Receipt fixtures commit to real empty or single-extrinsic bodies and hash
// complete SDK SCALE headers before signing any source that names those blocks.
package validator

import (
	"fmt"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// Preserve genuine SDK SCALE commitments while spelling the JSON quantity as
// Substrate does; the pinned SDK's marshaler omits the required 0x prefix.
func releaseReceiptTestHeaderWire(header types.Header) any {
	return struct {
		types.Header
		Number string `json:"number"`
	}{Header: header, Number: fmt.Sprintf("0x%x", uint64(header.Number))}
}

// The one-entry layout0 root is an independent exact leaf codec, not a call to
// the production trie implementation. Fixtures need only empty or one entry.
func releaseReceiptTestHeader(t *testing.T, parent types.Hash, number uint64, extrinsics ...string) (types.Header, types.Hash) {
	t.Helper()
	if len(extrinsics) > 1 {
		t.Fatal("receipt fixture supports only empty or single-extrinsic bodies")
	}
	trie := []byte{0}
	if len(extrinsics) == 1 {
		raw, err := codec.HexDecodeString(extrinsics[0])
		if err != nil || len(raw) == 0 {
			t.Fatalf("invalid receipt fixture extrinsic: %v", err)
		}
		// Leaf + two compact-index-zero nibbles, then SCALE Vec<u8> value.
		trie = append([]byte{0x42, 0}, releaseNativeValidatorTestCompact(t, uint64(len(raw)))...)
		trie = append(trie, raw...)
	}
	header := types.Header{ParentHash: parent, Number: types.BlockNumber(number), StateRoot: types.Hash{3}, ExtrinsicsRoot: types.Hash(blake2b.Sum256(trie)), Digest: types.Digest{}}
	raw, err := codec.Encode(header)
	if err != nil {
		t.Fatal(err)
	}
	return header, types.Hash(blake2b.Sum256(raw))
}
