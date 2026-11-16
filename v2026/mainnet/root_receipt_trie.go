// Native block bodies are checked against the SCALE ordered trie root. This
// small no-extension codec follows the pinned SDK's trie_stream/node_header;
// independent Rust sp-trie vectors qualify both layouts without another node.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"

	"golang.org/x/crypto/blake2b"
)

// The inspected runtime BlockLength permits 10 MiB, including operational calls.
const rootBodyBytesLimit = 10 * 1024 * 1024
const rootBodyCountLimit = 65536

// Entries own compact-index nibbles and reference immutable extrinsic bytes.
type rootTrieEntry struct {
	key   []byte
	value []byte
}

// The reviewed SDK uses layout0 for systemVersion 0/1, layout1 thereafter.
// The header commitment authenticates body bytes under either known layout;
// interpreting an included receipt still requires its exact execution profile.
func rootExtrinsicsRoot(extrinsics [][]byte, layout uint8) (string, error) {
	if layout > 1 || len(extrinsics) > rootBodyCountLimit {
		return "", errors.New("unsupported root body trie layout or count")
	}
	entries := make([]rootTrieEntry, len(extrinsics))
	total := 0
	for index, raw := range extrinsics {
		total += len(raw)
		if len(raw) == 0 || total > rootBodyBytesLimit {
			return "", errors.New("root block body exceeds byte bound or has an empty extrinsic")
		}
		compact := rootCompact(uint64(index))
		nibbles := make([]byte, 0, 2*len(compact))
		for _, value := range compact {
			nibbles = append(nibbles, value>>4, value&15)
		}
		entries[index] = rootTrieEntry{key: nibbles, value: raw}
	}
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].key, entries[j].key) < 0 })
	encoded := rootTrieNode(entries, 0, layout)
	digest := blake2b.Sum256(encoded)
	return "0x" + hex.EncodeToString(digest[:]), nil
}

// Compact-index keys have at most ten nibbles; recursion cannot be unbounded.
func rootTrieNode(entries []rootTrieEntry, depth int, layout uint8) []byte {
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
	result := rootTriePartial(partial, prefix, mask)
	var groups [16][]rootTrieEntry
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
			result = append(result, rootCompact(uint64(len(value)))...)
			result = append(result, value...)
		}
	}
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		child := rootTrieNode(group, shared+1, layout)
		if len(child) >= 32 {
			digest := blake2b.Sum256(child)
			child = digest[:]
		}
		result = append(result, rootCompact(uint64(len(child)))...)
		result = append(result, child...)
	}
	return result
}

// Prefix length encoding includes the extension byte at the exact mask bound.
func rootTriePartial(nibbles []byte, prefix, mask byte) []byte {
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
