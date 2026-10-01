// Raw native StorageProof nodes authenticate a complete key prefix against a
// supplied root. This mathematical primitive grants no chain or send authority.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"

	"golang.org/x/crypto/blake2b"
)

const safeCurrentStorageCodec = "substrate-blake2-raw-storage-proof-complete-prefix-v1"
const safeCurrentStorageSdk = "cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a"
const maximumSafeCurrentProofNodes = 4096
const maximumSafeCurrentProofBytes = 10 * 1024 * 1024
const maximumSafeCurrentProofItemBytes = maximumRuntimeSnapshotCodeBytes + 1024
const maximumSafeCurrentStorageKeyBytes = 256
const maximumSafeCurrentPrefixEntries = 64

// One proof invocation owns its input, parsed nodes and traversal budget.
// Raw external values and nodes share a hash namespace, as in the pinned SDK.
type safeCurrentStorageTrie struct {
	blobKVs map[[32]byte][]byte
	nodeKVs map[[32]byte]*safeCurrentStorageNode
	work    int
}

// Packed partial keys borrow the proof buffer. Values may be inline or resolved
// through their hash; no missing child or missing value is inferred absent.
type safeCurrentStorageNode struct {
	empty       bool
	leaf        bool
	partial     []byte
	nibbles     int
	value       []byte
	hasValue    bool
	valueHashed bool
	children    [16][]byte
}

// Canonical lowercase hex and shared byte bounds precede allocation or hashing.
func newSafeCurrentStorageTrie(ctx context.Context, nodes []string) (*safeCurrentStorageTrie, error) {
	if ctx == nil || len(nodes) > maximumSafeCurrentProofNodes {
		return nil, errors.New("Safe current proof context or node count differs")
	}
	self := &safeCurrentStorageTrie{blobKVs: map[[32]byte][]byte{}, nodeKVs: map[[32]byte]*safeCurrentStorageNode{}}
	remaining := maximumSafeCurrentProofBytes
	for _, encoded := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count := (len(encoded) - 2) / 2
		if len(encoded) < 2 || len(encoded)%2 != 0 || encoded[:2] != "0x" || count > maximumSafeCurrentProofItemBytes || count > remaining {
			return nil, errors.New("Safe current proof byte bound differs")
		}
		for _, value := range encoded[2:] {
			if !(value >= '0' && value <= '9') && !(value >= 'a' && value <= 'f') {
				return nil, errors.New("Safe current proof hex is not canonical")
			}
		}
		raw, err := hex.DecodeString(encoded[2:])
		if err != nil {
			return nil, err
		}
		remaining -= len(raw)
		hash := blake2b.Sum256(raw)
		if _, exists := self.blobKVs[hash]; exists {
			return nil, errors.New("Safe current proof contains duplicate blobs")
		}
		self.blobKVs[hash] = raw
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return self, nil
}

// Both reviewed SDK layouts use this no-extension codec. CompactProof and
// generated_trie_proof encodings cannot substitute raw StorageProof blobs.
func decodeSafeCurrentStorageNode(raw []byte) (*safeCurrentStorageNode, error) {
	reader := &rootScaleReader{data: raw}
	first, err := reader.take(1)
	if err != nil {
		return nil, err
	}
	node, bits := &safeCurrentStorageNode{}, 0
	switch {
	case first[0] == 0:
		node.empty = true
	case first[0]&0xc0 == 0x40:
		node.leaf, node.hasValue, bits = true, true, 2
	case first[0]&0xc0 == 0x80:
		bits = 2
	case first[0]&0xc0 == 0xc0:
		node.hasValue, bits = true, 2
	case first[0]&0xe0 == 0x20:
		node.leaf, node.hasValue, node.valueHashed, bits = true, true, true, 3
	case first[0]&0xf0 == 0x10:
		node.hasValue, node.valueHashed, bits = true, true, 4
	default:
		return nil, errors.New("Safe current proof node header is unsupported")
	}
	if !node.empty {
		maximum := byte(255 >> bits)
		node.nibbles = int(first[0] & maximum)
		if node.nibbles == int(maximum) {
			for {
				next, err := reader.take(1)
				if err != nil {
					return nil, err
				}
				node.nibbles += int(next[0])
				if node.nibbles > 2*maximumSafeCurrentStorageKeyBytes {
					return nil, errors.New("Safe current proof partial key exceeds bound")
				}
				if next[0] != 255 {
					break
				}
			}
		}
		node.partial, err = reader.take((node.nibbles + 1) / 2)
		if err != nil || node.nibbles%2 != 0 && node.partial[0]&0xf0 != 0 {
			return nil, errors.New("Safe current proof partial key is truncated or padded")
		}
		bitmap := uint16(0)
		if !node.leaf {
			rawBitmap, err := reader.take(2)
			if err != nil {
				return nil, err
			}
			bitmap = binary.LittleEndian.Uint16(rawBitmap)
			if bitmap == 0 {
				return nil, errors.New("Safe current proof branch bitmap is empty")
			}
		}
		if node.hasValue {
			length := uint64(32)
			if !node.valueHashed {
				length, err = reader.compact()
				if err != nil || length > maximumSafeCurrentProofItemBytes {
					return nil, errors.New("Safe current proof value length differs")
				}
			}
			node.value, err = reader.take(int(length))
			if err != nil {
				return nil, err
			}
		}
		for index := range 16 {
			if bitmap&(1<<index) == 0 {
				continue
			}
			length, err := reader.compact()
			if err != nil || length == 0 || length > 32 {
				return nil, errors.New("Safe current proof child reference length differs")
			}
			node.children[index], err = reader.take(int(length))
			if err != nil {
				return nil, err
			}
		}
	}
	if reader.offset != len(raw) {
		return nil, errors.New("Safe current proof node has trailing bytes")
	}
	return node, nil
}

// Every traversal has bounded work even if a malicious DAG shares subtrees.
func (self *safeCurrentStorageTrie) node(ctx context.Context, reference []byte) (*safeCurrentStorageNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self.work++
	if self.work > 16*maximumSafeCurrentProofNodes || len(reference) == 0 || len(reference) > 32 {
		return nil, errors.New("Safe current proof traversal exceeds bound")
	}
	raw, hash := reference, [32]byte{}
	if len(reference) == 32 {
		copy(hash[:], reference)
		if hash == blake2b.Sum256([]byte{0}) {
			return &safeCurrentStorageNode{empty: true}, nil
		}
		var exists bool
		raw, exists = self.blobKVs[hash]
		if !exists {
			return nil, errors.New("Safe current proof is incomplete: missing node")
		}
	} else {
		hash = blake2b.Sum256(raw)
	}
	if node, exists := self.nodeKVs[hash]; exists {
		return node, nil
	}
	node, err := decodeSafeCurrentStorageNode(raw)
	if err == nil {
		self.nodeKVs[hash] = node
	}
	return node, err
}

// A value belongs to the proof buffer until copied by the caller.
func (self *safeCurrentStorageTrie) value(node *safeCurrentStorageNode) ([]byte, error) {
	if !node.hasValue {
		return nil, errors.New("Safe current proof node has no value")
	}
	if !node.valueHashed {
		return node.value, nil
	}
	var hash [32]byte
	copy(hash[:], node.value)
	value, exists := self.blobKVs[hash]
	if !exists {
		return nil, errors.New("Safe current proof is incomplete: missing external value")
	}
	return value, nil
}

// Nibble expansion is used only for bounded keys, never values or code.
func safeCurrentStorageNibbles(key []byte) []byte {
	nibbles := make([]byte, 2*len(key))
	for i, value := range key {
		nibbles[2*i], nibbles[2*i+1] = value>>4, value&15
	}
	return nibbles
}

// A partial's odd leading nibble uses the low half of its first byte.
func (self *safeCurrentStorageNode) path(parent []byte) []byte {
	path := make([]byte, len(parent)+self.nibbles)
	copy(path, parent)
	for i := range self.nibbles {
		packed := i + self.nibbles%2
		path[len(parent)+i] = self.partial[packed/2] >> (4 * (1 - packed%2)) & 15
	}
	return path
}

// Point reads authenticate account code and runtime bytes at the same root.
// A present empty value stays distinct from a proven absent key.
func (self *safeCurrentStorageTrie) read(ctx context.Context, root [32]byte, key []byte) ([]byte, bool, error) {
	if ctx == nil || len(key) > maximumSafeCurrentStorageKeyBytes {
		return nil, false, errors.New("Safe current proof key or context differs")
	}
	wanted, reference, offset := safeCurrentStorageNibbles(key), root[:], 0
	for {
		node, err := self.node(ctx, reference)
		if err != nil {
			return nil, false, err
		}
		path := node.path(nil)
		if node.empty || len(path) > len(wanted)-offset || !bytes.Equal(path, wanted[offset:offset+len(path)]) {
			return nil, false, nil
		}
		offset += len(path)
		if offset == len(wanted) {
			if !node.hasValue {
				return nil, false, nil
			}
			value, err := self.value(node)
			return slices.Clone(value), err == nil, err
		}
		if node.leaf || len(node.children[wanted[offset]]) == 0 {
			return nil, false, nil
		}
		reference = node.children[wanted[offset]]
		offset++
	}
}

// Complete coverage follows every child under the prefix. Sibling subtrees
// outside it need no proof; pagination EOF and requested-key lists play no role.
func (self *safeCurrentStorageTrie) prefix(ctx context.Context, root [32]byte, prefix []byte) (map[string][]byte, error) {
	if ctx == nil || len(prefix) > maximumSafeCurrentStorageKeyBytes {
		return nil, errors.New("Safe current proof prefix or context differs")
	}
	wanted, result := safeCurrentStorageNibbles(prefix), map[string][]byte{}
	remaining := maximumSafeCurrentProofBytes
	var visit func([]byte, []byte) error
	visit = func(reference, parent []byte) error {
		node, err := self.node(ctx, reference)
		if err != nil {
			return err
		}
		if node.empty {
			return nil
		}
		path := node.path(parent)
		if len(path) > 2*maximumSafeCurrentStorageKeyBytes {
			return errors.New("Safe current proof path exceeds bound")
		}
		shared := min(len(path), len(wanted))
		if !bytes.Equal(path[:shared], wanted[:shared]) {
			return nil
		}
		if len(path) >= len(wanted) && node.hasValue {
			if len(path)%2 != 0 || len(result) >= maximumSafeCurrentPrefixEntries {
				return errors.New("Safe current proof prefix key or entry bound differs")
			}
			key := make([]byte, len(path)/2)
			for i := range key {
				key[i] = path[2*i]<<4 | path[2*i+1]
			}
			if _, duplicate := result[string(key)]; duplicate {
				return errors.New("Safe current proof repeats a prefix key")
			}
			value, err := self.value(node)
			if err != nil {
				return err
			}
			if len(value) > remaining {
				return errors.New("Safe current proof expanded prefix values exceed bound")
			}
			remaining -= len(value)
			result[string(key)] = slices.Clone(value)
		}
		for i, child := range node.children {
			if len(child) == 0 || len(path) < len(wanted) && byte(i) != wanted[len(path)] {
				continue
			}
			if err := visit(child, append(slices.Clone(path), byte(i))); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root[:], nil); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
