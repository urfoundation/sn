// Native recovery fixtures use independently encoded single-leaf bodies and
// real SCALE header commitments, including the modern one-byte update digest.
package miner

import (
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// This fixture needs only empty/single-entry layout0 roots. It does not call
// the shared production trie implementation when constructing expected bytes.
func fleetReceiptTestHeader(parent types.Hash, number uint64, extrinsics []string, update bool) (types.Header, types.Hash, error) {
	if len(extrinsics) > 1 {
		return types.Header{}, types.Hash{}, errors.New("synthetic receipt body exceeds one entry")
	}
	trie := []byte{0}
	if len(extrinsics) == 1 {
		raw, err := codec.HexDecodeString(extrinsics[0])
		if err != nil || len(raw) == 0 {
			return types.Header{}, types.Hash{}, errors.New("synthetic receipt extrinsic is malformed")
		}
		length, err := codec.Encode(types.NewUCompactFromUInt(uint64(len(raw))))
		if err != nil {
			return types.Header{}, types.Hash{}, err
		}
		trie = append(append([]byte{0x42, 0}, length...), raw...)
	}
	header := types.Header{ParentHash: parent, Number: types.BlockNumber(number), StateRoot: types.Hash{0x73}, ExtrinsicsRoot: types.Hash(blake2b.Sum256(trie)), Digest: types.Digest{}}
	raw, err := codec.Encode(header)
	if err != nil {
		return types.Header{}, types.Hash{}, err
	}
	if update {
		// Replace the empty digest with one RuntimeEnvironmentUpdated item.
		raw = append(raw[:len(raw)-1], 4, 8)
	}
	return header, types.Hash(blake2b.Sum256(raw)), nil
}

// Number is a native RPC quantity; SDK MarshalJSON incorrectly omits 0x.
// Raw logs preserve tag8, which that same pinned SDK cannot re-encode.
func (self *fleetMainnetTestFixture) nativeHeaderWireWithLock(number uint64) any {
	header := self.nativeHeaders[number]
	logs := []string{}
	if number == self.nativeRuntimeUpdateAt {
		logs = append(logs, "0x08")
	}
	return map[string]any{"parentHash": header.ParentHash.Hex(), "number": fmt.Sprintf("0x%x", number),
		"stateRoot": header.StateRoot.Hex(), "extrinsicsRoot": header.ExtrinsicsRoot.Hex(), "digest": map[string]any{"logs": logs}}
}

// Future synthetic blocks acquire their receipt commitment only when the
// submitted raw transaction is known. The original prepared block stays fixed.
func (self *fleetMainnetTestFixture) rebuildNativeBlocksWithLock() error {
	self.nativeBlocks, self.nativeHeaders = map[uint64]types.Hash{}, map[uint64]types.Header{}
	parent := types.Hash{0x50}
	for number := uint64(100); number <= self.nativeThrough; number++ {
		extrinsics := []string{}
		if number == self.nativeReceiptNumber && self.nativeBroadcast {
			extrinsics = append(extrinsics, self.nativeSigned)
		}
		header, hash, err := fleetReceiptTestHeader(parent, number, extrinsics, number == self.nativeRuntimeUpdateAt)
		if err != nil {
			return err
		}
		self.nativeHeaders[number], self.nativeBlocks[number] = header, hash
		parent = hash
	}
	self.head, self.receiptBlock = self.nativeBlocks[100], self.nativeBlocks[self.nativeReceiptNumber]
	return nil
}
