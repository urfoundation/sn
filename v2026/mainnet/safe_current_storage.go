// Complete finalized Safe storage is a narrower fact than deployment history.
// These readers produce observations only and never implement the send gate.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/blake2b"
)

const safeCurrentStorageSchema = "urnetwork-mainnet-safe-current-storage-observation-v1"

// The scope comes from a distinct independently signed policy proposal and the
// immutable execution plan. It cannot be inferred from observed storage.
type safeCurrentStorageScope struct {
	Safe                 common.Address
	Singleton            common.Address
	Owners               []common.Address
	Nonce                string
	Version              string
	Variant              string
	SafeProxyRuntimeHash common.Hash
	SingletonRuntimeHash common.Hash
	Runtime              rootReceiptProfile
}

// One raw proof binds code and all account storage to the same native header.
// The caller supplies the independently selected block identity, not a root.
type safeCurrentStorageWitness struct {
	At     string            `json:"at"`
	Header rootReceiptHeader `json:"header"`
	Nodes  []string          `json:"nodes"`
}

// Stable sorted words expose the exact proven census for independent review.
type safeCurrentStorageWord struct {
	Slot  common.Hash `json:"slot"`
	Value common.Hash `json:"value"`
}

// A successful proof establishes bytes at one root. History, finality trust,
// pending completeness and spending authority remain separate explicit gates.
type safeCurrentStorageObservation struct {
	Schema                    string                   `json:"schema"`
	Admission                 string                   `json:"admission"`
	NativeHash                string                   `json:"native_hash"`
	NativeNumber              uint64                   `json:"native_number"`
	StateRoot                 string                   `json:"state_root"`
	Codec                     string                   `json:"codec"`
	SdkSource                 string                   `json:"sdk_source"`
	StorageSource             string                   `json:"storage_source"`
	Safe                      common.Address           `json:"safe"`
	Singleton                 common.Address           `json:"singleton"`
	RuntimeCodeHash           string                   `json:"runtime_code_hash"`
	SafeProxyRuntimeHash      common.Hash              `json:"safe_proxy_runtime_hash"`
	SingletonRuntimeHash      common.Hash              `json:"singleton_runtime_hash"`
	Words                     []safeCurrentStorageWord `json:"words"`
	CompleteFinalizedStorage  bool                     `json:"complete_finalized_storage"`
	DeploymentHistoryVerified bool                     `json:"deployment_history_verified"`
	CompletePendingVerified   bool                     `json:"complete_pending_verified"`
	SendAuthorized            bool                     `json:"send_authorized"`
}

// The reviewed native map prefixes are Twox128(pallet/item), followed by
// Blake2_128Concat on each raw fixed-width H160/H256 SCALE key.
func safeCurrentNativeAccountKey(item string, address common.Address) []byte {
	key := append(xxhash.New128([]byte("EVM")).Sum(nil), xxhash.New128([]byte(item)).Sum(nil)...)
	hasher, _ := blake2b.New(16, nil)
	_, _ = hasher.Write(address[:])
	key = append(key, hasher.Sum(nil)...)
	return append(key, address[:]...)
}

// Each suffix includes its preimage, so it can be checked without reversing a
// Solidity mapping hash. Unknown nonzero slots are refused without guessing.
func safeCurrentNativeStorageKey(address common.Address, slot common.Hash) []byte {
	key := safeCurrentNativeAccountKey("AccountStorages", address)
	hasher, _ := blake2b.New(16, nil)
	_, _ = hasher.Write(slot[:])
	key = append(key, hasher.Sum(nil)...)
	return append(key, slot[:]...)
}

// Both pinned Safe layouts use mapping(address => address) in slots one/two.
func safeCurrentMappingSlot(address common.Address, slot byte) common.Hash {
	raw := make([]byte, 64)
	copy(raw[12:32], address[:])
	raw[63] = slot
	return crypto.Keccak256Hash(raw)
}

// Exact three-owner/two-signature, no-module semantics deliberately refuse all
// additional words, including approved hashes, signed messages and old baggage.
func (self safeCurrentStorageScope) validate() error {
	if self.Safe == (common.Address{}) || self.Singleton == (common.Address{}) || self.Safe == self.Singleton || len(self.Owners) != 3 ||
		!mainnetRuntimeCodecSource(self.Runtime.RuntimeSourceCommit) || self.Runtime.RuntimeVersion.SpecName == "" || self.Runtime.RuntimeVersion.SpecVersion == 0 || self.Runtime.RuntimeVersion.StateVersion > 1 ||
		!rootCanonicalHash(self.Runtime.RuntimeCodeHash) || !rootCanonicalHash(self.Runtime.RuntimeMetadataHash) {
		return errors.New("Safe current storage scope lacks exact account or runtime authority")
	}
	nonce, ok := new(big.Int).SetString(self.Nonce, 10)
	if !ok || nonce.Sign() < 0 || nonce.BitLen() > 256 || nonce.String() != self.Nonce {
		return errors.New("Safe current storage nonce is not canonical")
	}
	for i, owner := range self.Owners {
		if owner == (common.Address{}) || owner == common.HexToAddress("0x1") || owner == self.Safe || i > 0 && bytes.Compare(self.Owners[i-1][:], owner[:]) >= 0 {
			return errors.New("Safe current storage owners are not an exact sorted set")
		}
	}
	pin, err := loadSafeReleasePin(self.Version, self.Variant)
	if err != nil {
		return err
	}
	proxyMatched, singletonMatched := false, false
	for _, artifact := range pin.Artifacts {
		proxyMatched = proxyMatched || artifact.Name == "SafeProxy" && artifact.RuntimeKeccak256 == self.SafeProxyRuntimeHash.Hex()
		singletonMatched = singletonMatched || artifact.Name == self.Variant && artifact.RuntimeKeccak256 == self.SingletonRuntimeHash.Hex()
	}
	if !proxyMatched || !singletonMatched {
		return errors.New("Safe current storage code pins differ from the published profile")
	}
	return nil
}

// Request only permitted slots, code and runtime. Completeness is proved by
// traversing their union proof, never by trusting a returned census or EOF.
func (self safeCurrentStorageScope) keys() []string {
	keys := []string{runtimeCodeStorageKey}
	for _, address := range []common.Address{self.Safe, self.Singleton} {
		for _, item := range []string{"AccountCodes", "AccountCodesMetadata"} {
			keys = append(keys, "0x"+hex.EncodeToString(safeCurrentNativeAccountKey(item, address)))
		}
	}
	for _, slot := range self.slots() {
		keys = append(keys, "0x"+hex.EncodeToString(safeCurrentNativeStorageKey(self.Safe, slot)))
	}
	return keys
}

// Even nonce zero is explicitly requested, although its valid storage is absent.
func (self safeCurrentStorageScope) slots() []common.Hash {
	sentinel := common.HexToAddress("0x1")
	slots := []common.Hash{{}, common.BytesToHash([]byte{3}), common.BytesToHash([]byte{4}), common.BytesToHash([]byte{5}),
		safeCurrentMappingSlot(sentinel, 1), safeCurrentMappingSlot(sentinel, 2)}
	for _, owner := range self.Owners {
		slots = append(slots, safeCurrentMappingSlot(owner, 2))
	}
	return slots
}

// The complete census permits no unreachable owner/module entries or unrelated
// slots. Linked-list validation accepts any permutation of the exact owner set.
func (self safeCurrentStorageScope) words(entries map[string][]byte) ([]safeCurrentStorageWord, error) {
	allowed, values := map[common.Hash]bool{}, map[common.Hash]common.Hash{}
	for _, slot := range self.slots() {
		allowed[slot] = true
	}
	prefix := safeCurrentNativeAccountKey("AccountStorages", self.Safe)
	for key, raw := range entries {
		if len(key) != len(prefix)+48 || !bytes.HasPrefix([]byte(key), prefix) || len(raw) != 32 {
			return nil, errors.New("Safe current storage native key or value shape differs")
		}
		slot, value := common.BytesToHash([]byte(key)[len(key)-32:]), common.BytesToHash(raw)
		if !bytes.Equal([]byte(key), safeCurrentNativeStorageKey(self.Safe, slot)) || !allowed[slot] || value == (common.Hash{}) {
			return nil, errors.New("Safe current storage contains an unapproved, orphan or noncanonical word")
		}
		values[slot] = value
	}
	nonce, _ := new(big.Int).SetString(self.Nonce, 10)
	sentinel := common.HexToAddress("0x1")
	for slot, expected := range map[common.Hash]common.Hash{
		{}: common.BytesToHash(self.Singleton[:]), common.BytesToHash([]byte{3}): common.BytesToHash([]byte{3}),
		common.BytesToHash([]byte{4}): common.BytesToHash([]byte{2}), common.BytesToHash([]byte{5}): common.BigToHash(nonce),
		safeCurrentMappingSlot(sentinel, 1): common.BytesToHash(sentinel[:]),
	} {
		if values[slot] != expected {
			return nil, errors.New("Safe current storage singleton, nonce, threshold or module sentinel differs")
		}
	}
	remaining := map[common.Address]bool{}
	for _, owner := range self.Owners {
		remaining[owner] = true
	}
	current := sentinel
	for index := 0; index <= len(self.Owners); index++ {
		word := values[safeCurrentMappingSlot(current, 2)]
		if !bytes.Equal(word[:12], make([]byte, 12)) {
			return nil, errors.New("Safe current storage owner link has nonzero address padding")
		}
		next := common.BytesToAddress(word[12:])
		if index == len(self.Owners) {
			if next != sentinel || len(remaining) != 0 {
				return nil, errors.New("Safe current storage owner chain does not terminate")
			}
		} else if !remaining[next] {
			return nil, errors.New("Safe current storage owner chain omits or repeats authority")
		}
		delete(remaining, next)
		current = next
	}
	words := make([]safeCurrentStorageWord, 0, len(values))
	for slot, value := range values {
		words = append(words, safeCurrentStorageWord{Slot: slot, Value: value})
	}
	slices.SortFunc(words, func(a, b safeCurrentStorageWord) int { return bytes.Compare(a.Slot[:], b.Slot[:]) })
	return words, nil
}

// Header hashing binds all bytes to the independently selected native identity.
// Canonical finality and the signed policy are checked at the collection boundary.
func verifySafeCurrentStorage(ctx context.Context, scope safeCurrentStorageScope, hash string, number uint64, witness safeCurrentStorageWitness) (*safeCurrentStorageObservation, error) {
	if ctx == nil || witness.At != hash || !rootCanonicalHash(hash) {
		return nil, errors.New("Safe current storage witness changed the selected block")
	}
	if err := scope.validate(); err != nil {
		return nil, err
	}
	height, err := witness.Header.authenticate(hash)
	if err != nil || height != number {
		return nil, errors.Join(errors.New("Safe current storage native header differs"), err)
	}
	trie, err := newSafeCurrentStorageTrie(ctx, witness.Nodes)
	if err != nil {
		return nil, err
	}
	root := [32]byte(common.HexToHash(witness.Header.StateRoot))
	code, present, err := trie.read(ctx, root, []byte(":code"))
	if err != nil || !present || len(code) == 0 || len(code) > maximumRuntimeSnapshotCodeBytes || common.Hash(blake2b.Sum256(code)).Hex() != scope.Runtime.RuntimeCodeHash {
		return nil, errors.Join(errors.New("Safe current storage runtime artifact differs from the state root"), err)
	}
	for address, expected := range map[common.Address]common.Hash{scope.Safe: scope.SafeProxyRuntimeHash, scope.Singleton: scope.SingletonRuntimeHash} {
		raw, present, err := trie.read(ctx, root, safeCurrentNativeAccountKey("AccountCodes", address))
		if err != nil || !present {
			return nil, errors.Join(errors.New("Safe current storage account code is absent"), err)
		}
		reader := &rootScaleReader{data: raw}
		length, err := reader.compact()
		if err != nil || length == 0 || length > 64*1024 || int(length) != len(raw)-reader.offset || crypto.Keccak256Hash(raw[reader.offset:]) != expected {
			return nil, errors.New("Safe current storage code bytes differ from the published release")
		}
		metadata, present, err := trie.read(ctx, root, safeCurrentNativeAccountKey("AccountCodesMetadata", address))
		if err != nil || !present || len(metadata) != 40 || binary.LittleEndian.Uint64(metadata[:8]) != length || !bytes.Equal(metadata[8:], expected[:]) {
			return nil, errors.Join(errors.New("Safe current storage code metadata is absent or inconsistent"), err)
		}
	}
	entries, err := trie.prefix(ctx, root, safeCurrentNativeAccountKey("AccountStorages", scope.Safe))
	if err != nil {
		return nil, err
	}
	words, err := scope.words(entries)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &safeCurrentStorageObservation{Schema: safeCurrentStorageSchema, Admission: "unapproved_current_authority_observation",
		NativeHash: hash, NativeNumber: number, StateRoot: witness.Header.StateRoot, Codec: safeCurrentStorageCodec, SdkSource: safeCurrentStorageSdk,
		StorageSource: frontierMappingSourceCommit, Safe: scope.Safe, Singleton: scope.Singleton, RuntimeCodeHash: scope.Runtime.RuntimeCodeHash,
		SafeProxyRuntimeHash: scope.SafeProxyRuntimeHash, SingletonRuntimeHash: scope.SingletonRuntimeHash, Words: words, CompleteFinalizedStorage: true}, nil
}
