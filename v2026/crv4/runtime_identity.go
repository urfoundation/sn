// Runtime identity helpers deliberately use raw JSON for fields which the
// pinned GSRPC RuntimeVersion type does not expose. Callers supply the expected
// release values; this package preserves authoritative presence and bytes.
package crv4

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
)

// RuntimeVersionIdentity is the complete release-bound portion of an
// authoritative state_getRuntimeVersion response.
type RuntimeVersionIdentity struct {
	SpecName           string `json:"specName"`
	SpecVersion        uint32 `json:"specVersion"`
	TransactionVersion uint32 `json:"transactionVersion"`
	StateVersion       uint8  `json:"stateVersion"`
}

// Names every immutable input needed to select metadata for one reviewed
// runtime. A spec version alone must never be used as a cache key.
type RuntimeArtifactIdentity struct {
	Version      RuntimeVersionIdentity
	CodeHash     string
	MetadataHash string
}

// Carries metadata selected only after the requested block matched one exact
// allowed version and :code hash. Each connection fetches and hashes the large
// bytes once per exact artifact.
type AuthenticatedRuntimeArtifact struct {
	BlockHash    types.Hash
	Version      RuntimeVersionIdentity
	CodeHash     string
	MetadataHash string
	Metadata     *types.Metadata
}

// Coordinates one in-flight or successfully published immutable metadata load.
type runtimeMetadataArtifactCacheEntry struct {
	loadDone chan struct{}
	metadata *types.Metadata
	err      error
}

// Coalesces loads per independently dialed provider. Methods are safe for
// concurrent use and failures are never retained.
type runtimeMetadataArtifactCache struct {
	stateLock       sync.Mutex
	identityEntries map[RuntimeArtifactIdentity]*runtimeMetadataArtifactCacheEntry
}

const maximumRuntimeMetadataArtifactsPerChain = 3

// Creates an empty, hard-bounded per-provider artifact store.
func newRuntimeMetadataArtifactCache() *runtimeMetadataArtifactCache {
	return &runtimeMetadataArtifactCache{identityEntries: map[RuntimeArtifactIdentity]*runtimeMetadataArtifactCacheEntry{}}
}

// Manually assembled Chains are used by deterministic tests. Production
// chains initialize the cache in DialChain; this narrow lock makes lazy test
// initialization race-free without putting a copyable mutex in Chain.
var runtimeMetadataArtifactCacheInitialization = struct {
	stateLock sync.Mutex
}{}

// Lazily supports manually assembled test chains; production initializes the
// pointer while dialing, before the connection becomes visible to callers.
func (self *Chain) runtimeMetadataArtifactCache() *runtimeMetadataArtifactCache {
	var runtimeArtifacts *runtimeMetadataArtifactCache
	func() {
		runtimeMetadataArtifactCacheInitialization.stateLock.Lock()
		defer runtimeMetadataArtifactCacheInitialization.stateLock.Unlock()
		if self.runtimeArtifacts == nil {
			self.runtimeArtifacts = newRuntimeMetadataArtifactCache()
		}
		runtimeArtifacts = self.runtimeArtifacts
	}()
	return runtimeArtifacts
}

// Normalizes one exact digest before it participates in comparison or a key.
func canonicalRuntimeArtifactHash(label, value string) (string, error) {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return "", fmt.Errorf("%s is not a 32-byte 0x hash", label)
	}
	if _, err := hex.DecodeString(value[2:]); err != nil {
		return "", fmt.Errorf("%s is not a 32-byte 0x hash: %w", label, err)
	}
	return strings.ToLower(value), nil
}

// Rejects incomplete tuples and makes case-equivalent hashes one cache key.
func canonicalRuntimeArtifactIdentity(identity RuntimeArtifactIdentity) (RuntimeArtifactIdentity, error) {
	if identity.Version.SpecName == "" {
		return RuntimeArtifactIdentity{}, errors.New("runtime artifact spec name is empty")
	}
	codeHash, err := canonicalRuntimeArtifactHash("runtime artifact code hash", identity.CodeHash)
	if err != nil {
		return RuntimeArtifactIdentity{}, err
	}
	metadataHash, err := canonicalRuntimeArtifactHash("runtime artifact metadata hash", identity.MetadataHash)
	if err != nil {
		return RuntimeArtifactIdentity{}, err
	}
	identity.CodeHash = codeHash
	identity.MetadataHash = metadataHash
	return identity, nil
}

// Publishes only a successful exact object. Followers wait outside the lock, a
// canceled follower cannot cancel the leader, and failures leave no entry.
func (self *runtimeMetadataArtifactCache) load(ctx context.Context, identity RuntimeArtifactIdentity, fetch func(context.Context) (*types.Metadata, string, error)) (*types.Metadata, string, error) {
	if ctx == nil || self == nil || fetch == nil {
		return nil, "", errors.New("runtime metadata artifact cache dependencies are unavailable")
	}
	var entry *runtimeMetadataArtifactCacheEntry
	var existing bool
	var capacityErr error
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		entry = self.identityEntries[identity]
		if entry != nil {
			existing = true
			return
		}
		if len(self.identityEntries) >= maximumRuntimeMetadataArtifactsPerChain {
			capacityErr = fmt.Errorf("runtime metadata artifact cache already contains its maximum %d identities", maximumRuntimeMetadataArtifactsPerChain)
			return
		}
		entry = &runtimeMetadataArtifactCacheEntry{loadDone: make(chan struct{})}
		self.identityEntries[identity] = entry
	}()
	if capacityErr != nil {
		return nil, "", capacityErr
	}
	if existing {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-entry.loadDone:
			return entry.metadata, identity.MetadataHash, entry.err
		}
	}

	metadata, metadataHash, err := fetch(ctx)
	if err == nil && metadata == nil {
		err = errors.New("runtime metadata artifact is nil")
	}
	if err == nil {
		metadataHash, err = canonicalRuntimeArtifactHash("observed runtime metadata hash", metadataHash)
	}
	if err == nil && metadataHash != identity.MetadataHash {
		err = fmt.Errorf("observed runtime metadata hash %s, want %s", metadataHash, identity.MetadataHash)
	}

	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if err == nil {
			entry.metadata = metadata
		} else {
			entry.err = err
			delete(self.identityEntries, identity)
		}
		close(entry.loadDone)
	}()
	return metadata, metadataHash, err
}

// Decode one required unsigned JSON integer without accepting strings,
// fractions, exponents, signs, null, leading zeroes, or width truncation.
func decodeRuntimeVersionUint(name string, raw json.RawMessage, bits int) (uint64, error) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 {
		return 0, fmt.Errorf("runtime version field %s is empty", name)
	}
	for index, character := range value {
		if character < '0' || character > '9' || (index == 0 && character == '0' && len(value) != 1) {
			return 0, fmt.Errorf("runtime version field %s has invalid unsigned integer encoding %q", name, value)
		}
	}
	decoded, err := strconv.ParseUint(string(value), 10, bits)
	if err != nil {
		return 0, fmt.Errorf("runtime version field %s is outside uint%d: %w", name, bits, err)
	}
	return decoded, nil
}

// DecodeRuntimeVersionIdentity retains required-field presence. Unknown
// forward-compatible fields are ignored, while duplicate release-bound fields
// and trailing JSON are rejected.
func DecodeRuntimeVersionIdentity(raw json.RawMessage) (RuntimeVersionIdentity, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return RuntimeVersionIdentity{}, fmt.Errorf("decode runtime version object: %w", err)
	}
	delimiter, ok := opening.(json.Delim)
	if !ok || delimiter != '{' {
		return RuntimeVersionIdentity{}, errors.New("runtime version result is not an object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		keyToken, keyErr := decoder.Token()
		if keyErr != nil {
			return RuntimeVersionIdentity{}, fmt.Errorf("decode runtime version field name: %w", keyErr)
		}
		key, ok := keyToken.(string)
		if !ok {
			return RuntimeVersionIdentity{}, errors.New("runtime version field name is not a string")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return RuntimeVersionIdentity{}, fmt.Errorf("decode runtime version field %s: %w", key, err)
		}
		switch key {
		case "specName", "specVersion", "transactionVersion", "stateVersion":
			if _, exists := fields[key]; exists {
				return RuntimeVersionIdentity{}, fmt.Errorf("runtime version field %s is duplicated", key)
			}
			fields[key] = value
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return RuntimeVersionIdentity{}, fmt.Errorf("close runtime version object: %w", err)
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return RuntimeVersionIdentity{}, errors.New("runtime version object is not closed")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			return RuntimeVersionIdentity{}, errors.New("runtime version result has trailing JSON")
		}
		return RuntimeVersionIdentity{}, fmt.Errorf("decode runtime version trailing JSON: %w", err)
	}
	for _, name := range []string{"specName", "specVersion", "transactionVersion", "stateVersion"} {
		if _, exists := fields[name]; !exists {
			return RuntimeVersionIdentity{}, fmt.Errorf("runtime version field %s is missing", name)
		}
	}

	var version RuntimeVersionIdentity
	if err := json.Unmarshal(fields["specName"], &version.SpecName); err != nil || version.SpecName == "" {
		return RuntimeVersionIdentity{}, errors.New("runtime version field specName is not a nonempty string")
	}
	specVersion, err := decodeRuntimeVersionUint("specVersion", fields["specVersion"], 32)
	if err != nil {
		return RuntimeVersionIdentity{}, err
	}
	transactionVersion, err := decodeRuntimeVersionUint("transactionVersion", fields["transactionVersion"], 32)
	if err != nil {
		return RuntimeVersionIdentity{}, err
	}
	stateVersion, err := decodeRuntimeVersionUint("stateVersion", fields["stateVersion"], 8)
	if err != nil {
		return RuntimeVersionIdentity{}, err
	}
	version.SpecVersion = uint32(specVersion)
	version.TransactionVersion = uint32(transactionVersion)
	version.StateVersion = uint8(stateVersion)
	return version, nil
}

// RuntimeVersionAtContext reads the complete runtime version at one
// caller-selected block rather than silently dropping stateVersion through the
// GSRPC type. The caller's cancellation reaches the underlying RPC.
func RuntimeVersionAtContext(ctx context.Context, chain *Chain, blockHash types.Hash) (RuntimeVersionIdentity, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || blockHash == (types.Hash{}) {
		return RuntimeVersionIdentity{}, errors.New("runtime version read dependencies are unavailable")
	}
	var raw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &raw, "state_getRuntimeVersion", blockHash.Hex()); err != nil {
		return RuntimeVersionIdentity{}, fmt.Errorf("read state_getRuntimeVersion at %s: %w", blockHash.Hex(), err)
	}
	version, err := DecodeRuntimeVersionIdentity(raw)
	if err != nil {
		return RuntimeVersionIdentity{}, fmt.Errorf("decode state_getRuntimeVersion at %s: %w", blockHash.Hex(), err)
	}
	return version, nil
}

// Preserves the contextless compatibility surface for existing callers.
func RuntimeVersionAt(chain *Chain, blockHash types.Hash) (RuntimeVersionIdentity, error) {
	return RuntimeVersionAtContext(context.Background(), chain, blockHash)
}

// RuntimeCodeHashAtContext reads the authoritative System.Code storage hash at
// one explicit block. Shape validation belongs to the release caller.
func RuntimeCodeHashAtContext(ctx context.Context, chain *Chain, blockHash types.Hash) (string, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || blockHash == (types.Hash{}) {
		return "", errors.New("runtime code-hash read dependencies are unavailable")
	}
	var codeHash string
	if err := chain.API.Client.CallContext(ctx, &codeHash, "state_getStorageHash", "0x3a636f6465", blockHash.Hex()); err != nil {
		return "", err
	}
	return strings.ToLower(codeHash), nil
}

// Preserves the contextless compatibility surface for existing callers.
func RuntimeCodeHashAt(chain *Chain, blockHash types.Hash) (string, error) {
	return RuntimeCodeHashAtContext(context.Background(), chain, blockHash)
}

// DecodeRuntimeMetadata returns decoded metadata plus BLAKE2b-256 of the exact
// SCALE bytes in one authoritative state_getMetadata result. It never hashes a
// re-encoding.
func DecodeRuntimeMetadata(encoded string) (*types.Metadata, string, error) {
	if len(encoded) <= 2 || !strings.HasPrefix(encoded, "0x") || len(encoded)%2 != 0 {
		return nil, "", errors.New("runtime metadata is not canonical even-length 0x hex")
	}
	raw, err := hex.DecodeString(encoded[2:])
	if err != nil {
		return nil, "", fmt.Errorf("decode runtime metadata hex: %w", err)
	}
	metadata := new(types.Metadata)
	reader := bytes.NewReader(raw)
	if err := scale.NewDecoder(reader).Decode(metadata); err != nil {
		return nil, "", fmt.Errorf("decode runtime metadata SCALE: %w", err)
	}
	if reader.Len() != 0 {
		return nil, "", fmt.Errorf("runtime metadata has %d trailing SCALE bytes", reader.Len())
	}
	digest := blake2b.Sum256(raw)
	return metadata, "0x" + hex.EncodeToString(digest[:]), nil
}

// RuntimeMetadataAtContext reads and authenticates metadata at one explicit
// block. It does not cache: release callers decide which complete artifact
// identities are admissible before reusing immutable decoded metadata.
func RuntimeMetadataAtContext(ctx context.Context, chain *Chain, blockHash types.Hash) (*types.Metadata, string, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || blockHash == (types.Hash{}) {
		return nil, "", errors.New("runtime metadata read dependencies are unavailable")
	}
	var encoded string
	if err := chain.API.Client.CallContext(ctx, &encoded, "state_getMetadata", blockHash.Hex()); err != nil {
		return nil, "", err
	}
	return DecodeRuntimeMetadata(encoded)
}

// Preserves the contextless compatibility surface for existing callers.
func RuntimeMetadataAt(chain *Chain, blockHash types.Hash) (*types.Metadata, string, error) {
	return RuntimeMetadataAtContext(context.Background(), chain, blockHash)
}

// Checks the complete runtime version and :code hash at every requested block.
// Only then may it reuse bytes authenticated for that same immutable artifact
// on this connection. The release gate separately executes every admitted exact
// Wasm and proves Metadata_metadata reaches its pinned bytes without a stateful
// host call.
func AuthenticateRuntimeArtifactAtContext(ctx context.Context, chain *Chain, blockHash types.Hash, allowedIdentities ...RuntimeArtifactIdentity) (AuthenticatedRuntimeArtifact, error) {
	result := AuthenticatedRuntimeArtifact{BlockHash: blockHash}
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || blockHash == (types.Hash{}) || len(allowedIdentities) == 0 {
		return result, errors.New("runtime artifact authentication dependencies are unavailable")
	}
	if len(allowedIdentities) > maximumRuntimeMetadataArtifactsPerChain {
		return result, fmt.Errorf("runtime artifact allowlist has %d identities, maximum %d", len(allowedIdentities), maximumRuntimeMetadataArtifactsPerChain)
	}
	canonicalRuntimeArtifactIdentities := make([]RuntimeArtifactIdentity, len(allowedIdentities))
	allowedVersionIdentityBools := make(map[RuntimeVersionIdentity]bool, len(allowedIdentities))
	for index, identity := range allowedIdentities {
		var err error
		canonicalRuntimeArtifactIdentities[index], err = canonicalRuntimeArtifactIdentity(identity)
		if err != nil {
			return result, fmt.Errorf("allowed runtime artifact %d: %w", index, err)
		}
		if allowedVersionIdentityBools[canonicalRuntimeArtifactIdentities[index].Version] {
			return result, fmt.Errorf("allowed runtime artifact %d duplicates version identity %+v", index, canonicalRuntimeArtifactIdentities[index].Version)
		}
		allowedVersionIdentityBools[canonicalRuntimeArtifactIdentities[index].Version] = true
	}

	version, err := RuntimeVersionAtContext(ctx, chain, blockHash)
	if err != nil {
		return result, err
	}
	var selectedIdentity *RuntimeArtifactIdentity
	for index := range canonicalRuntimeArtifactIdentities {
		if canonicalRuntimeArtifactIdentities[index].Version == version {
			selectedIdentity = &canonicalRuntimeArtifactIdentities[index]
			break
		}
	}
	if selectedIdentity == nil {
		return result, fmt.Errorf("runtime at %s has unreviewed identity %s/%d/%d/%d", blockHash.Hex(), version.SpecName, version.SpecVersion, version.TransactionVersion, version.StateVersion)
	}
	codeHash, err := RuntimeCodeHashAtContext(ctx, chain, blockHash)
	if err != nil {
		return result, fmt.Errorf("read runtime code hash at %s: %w", blockHash.Hex(), err)
	}
	codeHash, err = canonicalRuntimeArtifactHash("observed runtime code hash", codeHash)
	if err != nil {
		return result, err
	}
	if codeHash != selectedIdentity.CodeHash {
		return result, fmt.Errorf("observed runtime code hash %s, want %s", codeHash, selectedIdentity.CodeHash)
	}
	metadata, metadataHash, err := chain.runtimeMetadataArtifactCache().load(ctx, *selectedIdentity, func(fetchCtx context.Context) (*types.Metadata, string, error) {
		return RuntimeMetadataAtContext(fetchCtx, chain, blockHash)
	})
	if err != nil {
		return result, err
	}
	result.Version = version
	result.CodeHash = codeHash
	result.MetadataHash = metadataHash
	result.Metadata = metadata
	return result, nil
}
