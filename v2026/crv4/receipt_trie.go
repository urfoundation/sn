// The bounded ordered trie authenticates receipt body completeness. The codec
// follows the reviewed SDK and is checked against independent Rust vectors.
package crv4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sort"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
)

// Matches the reviewed native BlockLength, including operational calls.
const receiptBodyBytesLimit = 10 * 1024 * 1024
const receiptBodyCountLimit = 65536

// Compact-index nibbles own their bytes; values borrow the bounded body.
type receiptTrieEntry struct {
	key   []byte
	value []byte
}

// Layout0 stores values inline; layout1 hashes values of 33 bytes or more.
// Matching either exact commitment grants no execution or signing authority.
func receiptExtrinsicsRoot(extrinsics [][]byte, layout uint8) (types.Hash, error) {
	if layout > 1 || len(extrinsics) > receiptBodyCountLimit {
		return types.Hash{}, errors.New("crv4: unsupported receipt trie layout or count")
	}
	entries := make([]receiptTrieEntry, len(extrinsics))
	total := 0
	for index, raw := range extrinsics {
		if len(raw) == 0 || len(raw) > receiptBodyBytesLimit-total {
			return types.Hash{}, errors.New("crv4: receipt body exceeds its bound or has an empty extrinsic")
		}
		total += len(raw)
		compact := appendCompact(nil, uint64(index))
		nibbles := make([]byte, 0, 2*len(compact))
		for _, value := range compact {
			nibbles = append(nibbles, value>>4, value&15)
		}
		entries[index] = receiptTrieEntry{key: nibbles, value: raw}
	}
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].key, entries[j].key) < 0 })
	return types.Hash(blake2b.Sum256(receiptTrieNode(entries, 0, layout))), nil
}

// Compact indices have at most ten nibbles under the count bound, so recursion
// cannot depend on an untrusted arbitrary key or become unbounded.
func receiptTrieNode(entries []receiptTrieEntry, depth int, layout uint8) []byte {
	if len(entries) == 0 {
		return []byte{0}
	}
	shared := len(entries[0].key)
	last := entries[len(entries)-1].key
	for shared > depth && (shared > len(last) || !bytes.Equal(entries[0].key[depth:shared], last[depth:shared])) {
		shared--
	}
	partial := entries[0].key[depth:shared]
	leaf := len(entries) == 1
	var value []byte
	children := entries
	if len(entries[0].key) == shared {
		value = entries[0].value
		children = entries[1:]
	}
	prefix, mask := byte(0x80), byte(0x3f)
	if leaf {
		prefix = 0x40
	} else if value != nil {
		prefix = 0xc0
	}
	hashedValue := layout == 1 && len(value) >= 33
	if hashedValue {
		prefix, mask = 0x10, 0x0f
		if leaf {
			prefix, mask = 0x20, 0x1f
		}
	}
	result := receiptTriePartial(partial, prefix, mask)
	var groups [16][]receiptTrieEntry
	if !leaf {
		var bitmap uint16
		for _, entry := range children {
			nibble := entry.key[shared]
			groups[nibble] = append(groups[nibble], entry)
			bitmap |= 1 << nibble
		}
		result = binary.LittleEndian.AppendUint16(result, bitmap)
	}
	if value != nil {
		if hashedValue {
			digest := blake2b.Sum256(value)
			result = append(result, digest[:]...)
		} else {
			result = appendCompact(result, uint64(len(value)))
			result = append(result, value...)
		}
	}
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		child := receiptTrieNode(group, shared+1, layout)
		if len(child) >= 32 {
			digest := blake2b.Sum256(child)
			child = digest[:]
		}
		result = appendCompact(result, uint64(len(child)))
		result = append(result, child...)
	}
	return result
}

// The extension byte is required even exactly at the header mask bound.
func receiptTriePartial(nibbles []byte, prefix, mask byte) []byte {
	length := len(nibbles)
	result := []byte{prefix + byte(min(length, int(mask)))}
	if length >= int(mask) {
		remaining := length - int(mask)
		for remaining >= 255 {
			result = append(result, 255)
			remaining -= 255
		}
		result = append(result, byte(remaining))
	}
	index := 0
	if length%2 != 0 {
		result = append(result, nibbles[0])
		index++
	}
	for index < length {
		result = append(result, nibbles[index]<<4|nibbles[index+1])
		index += 2
	}
	return result
}
