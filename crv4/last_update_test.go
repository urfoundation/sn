// LastUpdate is one vector per subnet; the reader selects one UID from the
// exact block's value.
package crv4

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// The stored vector's entry for the UID, zero past its end or with no vector,
// always at the exact block, and never for a zero block hash.
func TestLastUpdateAtContextReadsOneUIDAtTheExactBlock(t *testing.T) {
	blockHash := types.Hash{21}
	stored := []byte{3 << 2}
	for _, written := range []uint64{9461000, 9461002, 9460900} {
		stored = binary.LittleEndian.AppendUint64(stored, written)
	}
	var value any = codec.HexEncodeToString(stored)
	reads := 0
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if method != "state_getStorage" || len(args) != 2 || args[1] != blockHash.Hex() {
			return errors.New("last update was not read at the exact block")
		}
		reads++
		return setRuntimeIdentityTestResult(result, value)
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: releaseContextStorageMetadata()}
	for uid, want := range map[uint16]uint64{0: 9461000, 1: 9461002, 2: 9460900, 3: 0, 400: 0} {
		if written, err := chain.LastUpdateAtContext(t.Context(), 25, uid, blockHash); err != nil || written != want {
			t.Fatalf("uid %d last update %d, want %d: %v", uid, written, want, err)
		}
	}
	value = nil
	if written, err := chain.LastUpdateAtContext(t.Context(), 25, 1, blockHash); err != nil || written != 0 {
		t.Fatalf("an absent vector read %d: %v", written, err)
	}
	if _, err := chain.LastUpdateAtContext(t.Context(), 25, 1, types.Hash{}); err == nil || reads != 6 {
		t.Fatalf("a zero block hash was read: reads=%d err=%v", reads, err)
	}
}
