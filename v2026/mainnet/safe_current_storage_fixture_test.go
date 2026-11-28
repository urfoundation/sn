// Actual published Safe execution supplies storage/code facts. A separate test
// encoder commits those facts to a native trie and independently encoded header.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"slices"
	"testing"

	native "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/blake2b"
)

// One independent encoder owns complete synthetic native storage and proof data.
type safeCurrentProofFixture struct {
	oracle  *safeExecutionFixture
	scope   safeCurrentStorageScope
	entries map[string][]byte
}

// This fixture encoder never calls the production key or mapping-slot helpers.
func safeCurrentTestNativeKey(item string, address common.Address, slot *common.Hash) []byte {
	key := append(xxhash.New128([]byte("EVM")).Sum(nil), xxhash.New128([]byte(item)).Sum(nil)...)
	parts := [][]byte{address[:]}
	if slot != nil {
		parts = append(parts, slot[:])
	}
	for _, part := range parts {
		hasher, err := blake2b.New(16, nil)
		if err != nil {
			panic(err)
		}
		_, _ = hasher.Write(part)
		key = append(append(key, hasher.Sum(nil)...), part...)
	}
	return key
}

// Real setup chooses the owner link order; facts are read from its EVM state.
func newSafeCurrentProofFixture(t *testing.T, version, variant string) *safeCurrentProofFixture {
	t.Helper()
	return safeCurrentProofFixtureFromOracle(t, newSafeExecutionFixture(t, version, variant), version, variant)
}

// Sharing the actual EVM with an HTTP fixture exercises final pending rechecks.
func safeCurrentProofFixtureFromOracle(t *testing.T, oracle *safeExecutionFixture, version, variant string) *safeCurrentProofFixture {
	t.Helper()
	safe := oracle.transaction.Safe
	singletonWord := oracle.state.GetState(safe, common.Hash{})
	singleton := common.BytesToAddress(singletonWord[12:])
	profile := bootstrapSuccessorCanonicalTestRuntime()
	runtimeCode := []byte("synthetic independently approved native runtime")
	profile.RuntimeCodeHash = common.Hash(blake2b.Sum256(runtimeCode)).Hex()
	f := &safeCurrentProofFixture{oracle: oracle, entries: map[string][]byte{":code": runtimeCode}, scope: safeCurrentStorageScope{
		Safe: safe, Singleton: singleton, Owners: slices.Clone(oracle.owners), Nonce: oracle.transaction.Nonce.String(), Version: version, Variant: variant,
		SafeProxyRuntimeHash: crypto.Keccak256Hash(oracle.state.GetCode(safe)), SingletonRuntimeHash: crypto.Keccak256Hash(oracle.state.GetCode(singleton)), Runtime: profile}}
	for _, address := range []common.Address{safe, singleton} {
		code := oracle.state.GetCode(address)
		encoded, err := codec.Encode(code)
		if err != nil {
			t.Fatal(err)
		}
		f.entries[string(safeCurrentTestNativeKey("AccountCodes", address, nil))] = encoded
		metadata := make([]byte, 40)
		binary.LittleEndian.PutUint64(metadata, uint64(len(code)))
		copy(metadata[8:], crypto.Keccak256(code))
		f.entries[string(safeCurrentTestNativeKey("AccountCodesMetadata", address, nil))] = metadata
	}
	slots := []common.Hash{{}, common.BigToHash(big.NewInt(3)), common.BigToHash(big.NewInt(4)), common.BigToHash(big.NewInt(5))}
	sentinel := common.HexToAddress("0x1")
	for _, owner := range append([]common.Address{sentinel}, oracle.owners...) {
		slots = append(slots, crypto.Keccak256Hash(common.LeftPadBytes(owner[:], 32), common.LeftPadBytes([]byte{2}, 32)))
	}
	slots = append(slots, crypto.Keccak256Hash(common.LeftPadBytes(sentinel[:], 32), common.LeftPadBytes([]byte{1}, 32)))
	for _, slot := range slots {
		value := oracle.state.GetState(safe, slot)
		if value == (common.Hash{}) {
			t.Fatal("published Safe fixture has an absent expected word", slot)
		}
		f.entries[string(safeCurrentTestNativeKey("AccountStorages", safe, &slot))] = slices.Clone(value[:])
	}
	return f
}

// Mutable scenarios always rebuild the commitment, distinguishing a true bad
// state from a proof/hash corruption. No production decoder supplies this root.
func safeCurrentTestWitness(t *testing.T, entries map[string][]byte) safeCurrentStorageWitness {
	t.Helper()
	root, nodes := safeCurrentTestTrie(t, entries)
	header := native.Header{ParentHash: native.Hash{1}, Number: 703, StateRoot: native.Hash(root), ExtrinsicsRoot: native.Hash{2}}
	raw, err := codec.Encode(header)
	if err != nil {
		t.Fatal(err)
	}
	witness := safeCurrentStorageWitness{At: common.Hash(blake2b.Sum256(raw)).Hex(), Nodes: nodes,
		Header: rootReceiptHeader{ParentHash: header.ParentHash.Hex(), Number: "0x2bf", StateRoot: common.Hash(root).Hex(), ExtrinsicsRoot: header.ExtrinsicsRoot.Hex()}}
	witness.Header.Digest.Logs = []string{}
	return witness
}

// A test-only radix builder uses the pinned SDK's documented layout-one
// encoding. Independent SDK vectors cross-check every relevant decoder shape.
func safeCurrentTestTrie(t *testing.T, entries map[string][]byte) ([32]byte, []string) {
	t.Helper()
	type entry struct {
		key   []byte
		value []byte
	}
	items := []entry{}
	for key, value := range entries {
		nibbles := []byte{}
		for _, part := range []byte(key) {
			nibbles = append(nibbles, part/16, part%16)
		}
		items = append(items, entry{key: nibbles, value: value})
	}
	slices.SortFunc(items, func(a, b entry) int { return bytes.Compare(a.key, b.key) })
	blobs := map[[32]byte][]byte{}
	compact := func(count int) []byte {
		raw, err := codec.Encode(native.NewUCompactFromUInt(uint64(count)))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var encode func([]entry, int) []byte
	encode = func(items []entry, offset int) []byte {
		if len(items) == 0 {
			return []byte{0}
		}
		length := 0
		for offset+length < len(items[0].key) && offset+length < len(items[len(items)-1].key) && items[0].key[offset+length] == items[len(items)-1].key[offset+length] {
			length++
		}
		path := items[0].key[offset : offset+length]
		offset += length
		leaf := len(items) == 1
		var value []byte
		hasValue := len(items[0].key) == offset
		if hasValue {
			value = items[0].value
			items = items[1:]
		}
		header, maximum := byte(0x80), 63
		if leaf {
			header = 0x40
		} else if hasValue {
			header = 0xc0
		}
		if hasValue && len(value) >= 33 {
			header, maximum = 0x10, 15
			if leaf {
				header, maximum = 0x20, 31
			}
		}
		raw := []byte{header | byte(min(length, maximum))}
		if length >= maximum {
			remaining := length - maximum
			for remaining >= 255 {
				raw = append(raw, 255)
				remaining -= 255
			}
			raw = append(raw, byte(remaining))
		}
		if len(path)%2 != 0 {
			raw, path = append(raw, path[0]), path[1:]
		}
		for i := 0; i < len(path); i += 2 {
			raw = append(raw, path[i]*16+path[i+1])
		}
		groups := [16][]entry{}
		bitmap := uint16(0)
		for _, item := range items {
			groups[item.key[offset]] = append(groups[item.key[offset]], item)
			bitmap |= 1 << item.key[offset]
		}
		if !leaf {
			raw = append(raw, byte(bitmap), byte(bitmap>>8))
		}
		if hasValue {
			if len(value) >= 33 {
				hash := blake2b.Sum256(value)
				blobs[hash] = slices.Clone(value)
				raw = append(raw, hash[:]...)
			} else {
				raw = append(append(raw, compact(len(value))...), value...)
			}
		}
		for _, group := range groups {
			if len(group) == 0 {
				continue
			}
			child := encode(group, offset+1)
			if len(child) >= 32 {
				hash := blake2b.Sum256(child)
				blobs[hash] = child
				child = hash[:]
			}
			raw = append(append(raw, compact(len(child))...), child...)
		}
		return raw
	}
	raw := encode(items, 0)
	root := blake2b.Sum256(raw)
	blobs[root] = raw
	nodes := []string{}
	for _, blob := range blobs {
		nodes = append(nodes, "0x"+hex.EncodeToString(blob))
	}
	slices.Sort(nodes)
	return root, nodes
}
