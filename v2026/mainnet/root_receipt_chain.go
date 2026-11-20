// The production root chain port is read-only. It authenticates headers/body
// commitments and ancestry from the explicitly approved owned RPC's finalized
// head. It does not implement GRANDPA or storage proofs, custody, or submission.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

const rootAncestryLimit = 4096

var errRootReceiptProfileUnavailable = errors.New("root receipt runtime/code has no independently approved decoding profile")
var errRootSubmissionUnavailable = errors.New("root submission is disabled until independent authority, custody and exposure adapters are qualified")

// One adapter serializes reconciliation/cache ownership, like its action owner.
// Profiles are copied; no mutable observer list can authorize a later runtime.
type rootCanonicalChain struct {
	client              *rpcClient
	expected            identityExpectation
	profiles            []rootReceiptProfile
	reconcileCh         chan struct{}
	runtimeKVs          map[rootReceiptProfile]rootReceiptRuntime
	runtimeOrder        []rootReceiptProfile
	runtimeCacheEntries int
}

// Construction accepts no inferred genesis, testnet route, signer or raw secret.
// These read profiles are not signing authority or source-to-Wasm provenance.
func newRootCanonicalChain(client *rpcClient, expected identityExpectation, profiles []rootReceiptProfile) (*rootCanonicalChain, error) {
	return newRootCanonicalChainBounded(client, expected, profiles, 8, 8)
}

// Only the economic observer selects a larger artifact catalog. Its decoded
// cache remains smaller than that retained catalog; eviction removes no review.
func newRootCanonicalChainBounded(client *rpcClient, expected identityExpectation, profiles []rootReceiptProfile, profileLimit, cacheEntries int) (*rootCanonicalChain, error) {
	if profileLimit < 8 || profileLimit > 64 || cacheEntries < 1 || cacheEntries > profileLimit || client == nil || client.retryWindow < 60*time.Second || client.retryWindow > 15*time.Minute || expected.NativeChain == "" || expected.EvmChainId != mainnetEvmChainId || !rootCanonicalHash(expected.GenesisHash) || len(profiles) == 0 || len(profiles) > profileLimit {
		return nil, errors.New("root receipt adapter requires independent mainnet identity, bounded 60–900s reads and its declared finite runtime capacity")
	}
	seen := map[string]bool{}
	for _, profile := range profiles {
		version := profile.RuntimeVersion
		if !mainnetRuntimeCodecSource(profile.RuntimeSourceCommit) || version.SpecName == "" || version.SpecVersion == 0 || version.TransactionVersion == 0 || version.StateVersion != 1 || !rootCanonicalHash(profile.RuntimeCodeHash) || !rootCanonicalHash(profile.RuntimeMetadataHash) {
			return nil, errors.New("root receipt artifact lacks the reviewed runtime/system-version profile")
		}
		key := rootObjectHash(struct {
			Version crv4.RuntimeVersionIdentity
			Code    string
		}{Version: version, Code: profile.RuntimeCodeHash})
		if seen[key] {
			return nil, errors.New("root receipt runtime/code profile is duplicated or ambiguous")
		}
		seen[key] = true
	}
	return &rootCanonicalChain{client: client, expected: expected, profiles: append([]rootReceiptProfile(nil), profiles...), runtimeKVs: map[rootReceiptProfile]rootReceiptRuntime{}, runtimeCacheEntries: cacheEntries, reconcileCh: make(chan struct{}, 1)}, nil
}

// No production mutation route is exposed by this increment, even if called
// directly without the owner. Read retries are never reused for an RPC write.
func (self *rootCanonicalChain) submit(context.Context, []byte) error {
	return errRootSubmissionUnavailable
}

// Header fields retain exact digest bytes rather than accepting an announced
// hash or GSRPC zero-values for omitted fields as authentication.
type rootReceiptHeader struct {
	ParentHash     string `json:"parentHash"`
	Number         string `json:"number"`
	StateRoot      string `json:"stateRoot"`
	ExtrinsicsRoot string `json:"extrinsicsRoot"`
	Digest         struct {
		Logs []string `json:"logs"`
	} `json:"digest"`
}

// Normalizes hex spelling on one caller-owned decoded header without changing
// byte identity or converting absent digest logs into an empty vector.
func (self *rootReceiptHeader) normalizeHashes() {
	self.ParentHash = strings.ToLower(self.ParentHash)
	self.StateRoot = strings.ToLower(self.StateRoot)
	self.ExtrinsicsRoot = strings.ToLower(self.ExtrinsicsRoot)
	for index := range self.Digest.Logs {
		self.Digest.Logs[index] = strings.ToLower(self.Digest.Logs[index])
	}
}

// SCALE header hashing authenticates its parent, roots, height and digest.
func (self rootReceiptHeader) authenticate(expectedHash string) (uint64, error) {
	number, err := parseHexNumber(self.Number)
	if err != nil || number > math.MaxUint32 {
		return 0, errors.New("root finalized header number exceeds native u32")
	}
	// Genesis has no parent; later headers must name a real predecessor.
	validParent := rootCanonicalHash(self.ParentHash) || number == 0 && self.ParentHash == "0x"+strings.Repeat("0", 64)
	if !validParent || !rootCanonicalHash(self.StateRoot) || !rootCanonicalHash(self.ExtrinsicsRoot) || self.Digest.Logs == nil || len(self.Digest.Logs) > 256 {
		return 0, errors.New("root finalized header fields are missing or invalid")
	}
	parent, _ := hex.DecodeString(self.ParentHash[2:])
	state, _ := hex.DecodeString(self.StateRoot[2:])
	extrinsics, _ := hex.DecodeString(self.ExtrinsicsRoot[2:])
	raw := append(parent, rootCompact(number)...)
	raw = append(raw, state...)
	raw = append(raw, extrinsics...)
	raw = append(raw, rootCompact(uint64(len(self.Digest.Logs)))...)
	for _, encoded := range self.Digest.Logs {
		log, err := rootReceiptHex(encoded, 65536)
		if err != nil || len(raw)+len(log) > 256*1024 {
			return 0, errors.New("root finalized header digest exceeds bound")
		}
		if err := rootReceiptDigest(log); err != nil {
			return 0, err
		}
		raw = append(raw, log...)
	}
	if rootExtrinsicHash(raw) != expectedHash {
		return 0, errors.New("root finalized header hash differs from its SCALE bytes")
	}
	return number, nil
}

// Every parent is requested by the child-authenticated hash, never by latest.
func (self *rootCanonicalChain) header(ctx context.Context, hash string) (rootReceiptHeader, uint64, error) {
	var header rootReceiptHeader
	if !rootCanonicalHash(hash) {
		return header, 0, errors.New("invalid root finalized header hash")
	}
	if err := self.client.call(ctx, "chain_getHeader", []any{hash}, &header); err != nil {
		return header, 0, err
	}
	header.normalizeHashes()
	number, err := header.authenticate(hash)
	return header, number, err
}

// Explicit network reads are repeated for every reconciliation. A previously
// cached artifact never bypasses a wrong-chain route after failover.
func (self *rootCanonicalChain) network(ctx context.Context) error {
	identity := chainIdentity{}
	var evmHex string
	for _, read := range []struct {
		method string
		params []any
		result any
	}{
		{method: "system_chain", params: []any{}, result: &identity.NativeChain},
		{method: "chain_getBlockHash", params: []any{0}, result: &identity.GenesisHash},
		{method: "eth_chainId", params: []any{}, result: &evmHex},
	} {
		if err := self.client.call(ctx, read.method, read.params, read.result); err != nil {
			return err
		}
	}
	var err error
	identity.EvmChainId, err = parseHexNumber(evmHex)
	if err != nil {
		return err
	}
	return self.expected.match(identity)
}

// Exact current state may be unavailable after an unapproved upgrade. That
// blocks signing and absence expiry but must not erase an older verified fee.
func (self *rootCanonicalChain) observation(ctx context.Context, action rootAction, hash string, number uint64) (rootActionObservation, error) {
	observation := rootActionObservation{NativeChain: self.expected.NativeChain, GenesisHash: self.expected.GenesisHash, EvmChainId: self.expected.EvmChainId, FinalizedHash: hash, FinalizedNumber: number, Hotkey: action.Scope.Hotkey}
	runtime, err := self.runtimeAt(ctx, hash)
	if err == errRootReceiptProfileUnavailable {
		var rawVersion json.RawMessage
		var metadataHex string
		if err := self.client.call(ctx, "state_getRuntimeVersion", []any{hash}, &rawVersion); err != nil {
			return observation, err
		}
		version, err := crv4.DecodeRuntimeVersionIdentity(rawVersion)
		if err != nil {
			return observation, err
		}
		if err := self.client.call(ctx, "state_getStorageHash", []any{"0x3a636f6465", hash}, &observation.RuntimeCodeHash); err != nil {
			return observation, err
		}
		if err := self.client.call(ctx, "state_getMetadata", []any{hash}, &metadataHex); err != nil {
			return observation, err
		}
		metadataRaw, err := rootReceiptHex(metadataHex, maxMetadataRpcReplyBytes)
		if err != nil || !rootCanonicalHash(observation.RuntimeCodeHash) {
			return observation, errors.New("root current runtime identity is malformed")
		}
		observation.RuntimeVersion, observation.RuntimeMetadataHash = version, rootExtrinsicHash(metadataRaw)
		observation.StateUnavailable = true
		return observation, nil
	}
	if err != nil {
		return observation, err
	}
	observation.RuntimeVersion, observation.RuntimeCodeHash, observation.RuntimeMetadataHash = runtime.profile.RuntimeVersion, runtime.profile.RuntimeCodeHash, runtime.profile.RuntimeMetadataHash
	hotkey, _ := hex.DecodeString(action.Scope.Hotkey[2:])
	account, exists, err := self.storage(ctx, runtime.metadata, "System", "Account", hash, hotkey)
	if err != nil {
		return observation, err
	}
	if exists {
		if len(account) != 56 {
			return observation, errors.New("root native account is not the reviewed 56-byte row")
		}
		observation.AccountNonce = binary.LittleEndian.Uint32(account[:4])
	}
	var specs []rootStorageSpec
	for _, spec := range rootStorageSpecs {
		if spec.name == "Keys" || spec.name == "Uids" || spec.name == "Owner" || spec.name == "BlockAtRegistration" {
			specs = append(specs, spec)
		}
	}
	entries, err := observationStorageProfile(runtime.metadata, specs)
	if err != nil {
		return observation, err
	}
	reader := rootStorageReader{client: self.client, metadata: runtime.metadata, entries: entries, specs: specs, block: hash, valueKVs: map[string]rootStorageValue{}}
	uid, err := reader.read(ctx, "Uids", []byte{0, 0}, hotkey)
	if err != nil {
		return observation, err
	}
	if uid.RawStorage == nil {
		return observation, nil
	}
	key, err := reader.read(ctx, "Keys", []byte{0, 0}, uid.data)
	if err != nil {
		return observation, err
	}
	owner, err := reader.read(ctx, "Owner", hotkey)
	if err != nil {
		return observation, err
	}
	registration, err := reader.read(ctx, "BlockAtRegistration", []byte{0, 0}, uid.data)
	if err != nil {
		return observation, err
	}
	if key.RawStorage == nil || key.EffectiveScale != action.Scope.Hotkey || owner.RawStorage == nil || registration.RawStorage == nil || binary.LittleEndian.Uint64(registration.data) > number {
		return observation, errors.New("root current seat forward/reverse/owner/generation rows disagree")
	}
	observation.Coldkey = owner.EffectiveScale
	observation.Seat = rootSeatExpectation{Uid: binary.LittleEndian.Uint16(uid.data), RegistrationBlock: binary.LittleEndian.Uint64(registration.data)}
	return observation, nil
}

// A complete body must hash to the authenticated header before any matching
// byte string is called an inclusion or the absence scan advances a block.
func (self *rootCanonicalChain) body(ctx context.Context, hash string, header rootReceiptHeader) ([][]byte, error) {
	var reply struct {
		Block *struct {
			Header     rootReceiptHeader `json:"header"`
			Extrinsics *[]string         `json:"extrinsics"`
		} `json:"block"`
	}
	if err := self.client.callBoundedRead(ctx, "chain_getBlock", []any{hash}, &reply, false, 2*rootBodyBytesLimit+maxRpcReplyBytes); err != nil {
		return nil, err
	}
	if reply.Block == nil || reply.Block.Extrinsics == nil || len(*reply.Block.Extrinsics) > rootBodyCountLimit {
		return nil, errors.New("root block body is missing or exceeds count bound")
	}
	if _, err := reply.Block.Header.authenticate(hash); err != nil {
		return nil, err
	}
	return authenticateRootReceiptBody(header, *reply.Block.Extrinsics)
}

// The same complete ordered-trie check serves archive reads and retained
// witnesses. It authenticates bytes without interpreting runtime call semantics.
func authenticateRootReceiptBody(header rootReceiptHeader, extrinsics []string) ([][]byte, error) {
	if extrinsics == nil || len(extrinsics) > rootBodyCountLimit {
		return nil, errors.New("root block body is missing or exceeds count bound")
	}
	body := make([][]byte, 0, len(extrinsics))
	total := 0
	for _, encoded := range extrinsics {
		raw, err := rootReceiptHex(encoded, rootBodyBytesLimit)
		if err != nil {
			return nil, err
		}
		total += len(raw)
		if total > rootBodyBytesLimit {
			return nil, errors.New("root block body exceeds byte bound")
		}
		lengthReader := rootScaleReader{data: raw}
		length, err := lengthReader.compact()
		if err != nil || length != uint64(len(raw)-lengthReader.offset) || length == 0 {
			return nil, errors.New("root block extrinsic length prefix differs from bytes")
		}
		body = append(body, raw)
	}
	// Both native ordered-trie layouts are independently implemented. Matching
	// the committed root authenticates complete bytes without interpreting an
	// unknown intervening runtime. An actual receipt still requires its exact
	// parent execution profile and that source's layout below.
	root, err := rootExtrinsicsRoot(body, 0)
	if err != nil {
		return nil, err
	}
	if root != header.ExtrinsicsRoot {
		root, err = rootExtrinsicsRoot(body, 1)
		if err != nil || root != header.ExtrinsicsRoot {
			return nil, errors.Join(errors.New("root complete body differs from finalized extrinsics root"), err)
		}
	}
	return body, nil
}

// Reconciliation is finite and never skips a failed archive read. Long offline
// gaps above the ancestry bound require an explicitly reviewed archive recovery;
// they cannot silently become unsigned expiry or a renewed allowance.
func (self *rootCanonicalChain) reconcile(ctx context.Context, action rootAction, signed []byte) (rootActionReconciliation, error) {
	var result rootActionReconciliation
	if ctx == nil {
		return result, errors.New("root receipt context is absent")
	}
	select {
	case self.reconcileCh <- struct{}{}:
		defer func() { <-self.reconcileCh }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	if err := action.validate(); err != nil {
		return result, err
	}
	if err := rootReceiptSignedAction(action, signed); err != nil {
		return result, err
	}
	if action.Scope.NativeChain != self.expected.NativeChain || action.Scope.GenesisHash != self.expected.GenesisHash || action.Scope.EvmChainId != self.expected.EvmChainId {
		return result, errors.New("root receipt action has another approved network")
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := self.network(operationCtx); err != nil {
		return result, err
	}
	var finalized string
	if err := self.client.call(operationCtx, "chain_getFinalizedHead", []any{}, &finalized); err != nil {
		return result, err
	}
	header, number, err := self.header(operationCtx, finalized)
	if err != nil {
		return result, err
	}
	if number < action.BirthBlock || number-action.BirthBlock > rootAncestryLimit {
		return result, errors.New("root finalized head is before anchor or beyond bounded ancestry recovery")
	}
	headers := map[uint64]rootReceiptHeader{number: header}
	hashes := map[uint64]string{number: finalized}
	for height := number; height > action.BirthBlock; height-- {
		parent := headers[height].ParentHash
		parentHeader, parentNumber, err := self.header(operationCtx, parent)
		if err != nil {
			return result, err
		}
		if parentNumber != height-1 {
			return result, errors.New("root finalized ancestry skips a block number")
		}
		headers[parentNumber], hashes[parentNumber] = parentHeader, parent
	}
	if hashes[action.BirthBlock] != action.BirthHash {
		return result, errors.New("root finalized ancestry has another mortal anchor")
	}
	result.AnchorHash, result.CheckedFrom, result.CheckedThrough = action.BirthHash, action.BirthBlock+1, min(number, action.BirthBlock+action.Period-1)
	for height := result.CheckedFrom; height <= result.CheckedThrough; height++ {
		body, err := self.body(operationCtx, hashes[height], headers[height])
		if err != nil {
			return rootActionReconciliation{}, err
		}
		for index, raw := range body {
			if len(signed) == 0 || !bytes.Equal(raw, signed) {
				continue
			}
			if result.Receipt != nil {
				return rootActionReconciliation{}, errors.New("root signed action appears more than once in finalized interval")
			}
			runtime, err := self.runtimeAt(operationCtx, hashes[height-1])
			if err != nil {
				return rootActionReconciliation{}, err
			}
			// The admitted source uses systemVersion1: storage layout1 but
			// extrinsics layout0. Never infer the latter from the former.
			root, err := rootExtrinsicsRoot(body, 0)
			if err != nil || root != headers[height].ExtrinsicsRoot {
				return rootActionReconciliation{}, errors.New("root receipt execution profile/body layout disagree")
			}
			events, exists, err := self.storage(operationCtx, runtime.metadata, "System", "Events", hashes[height])
			if err != nil {
				return rootActionReconciliation{}, err
			}
			if !exists {
				return rootActionReconciliation{}, errors.New("root included action has no finalized event storage")
			}
			receipt, err := rootDecodeReceiptEvents(runtime.metadata, events, uint32(index), len(body), action)
			if err != nil {
				return rootActionReconciliation{}, err
			}
			receipt.BlockNumber, receipt.BlockHash, receipt.ExtrinsicIndex, receipt.RawExtrinsic = height, hashes[height], uint32(index), "0x"+hex.EncodeToString(raw)
			receipt.ExecutionRuntimeVersion, receipt.ExecutionCodeHash, receipt.ExecutionMetadataHash = runtime.profile.RuntimeVersion, runtime.profile.RuntimeCodeHash, runtime.profile.RuntimeMetadataHash
			receipt.PostState = self.rootPostState(operationCtx, action, hashes[height])
			result.Receipt = &receipt
		}
	}
	result.Observation, err = self.observation(operationCtx, action, finalized, number)
	if err != nil {
		return rootActionReconciliation{}, err
	}
	var canonical string
	if err := self.client.call(operationCtx, "chain_getBlockHash", []any{number}, &canonical); err != nil {
		return rootActionReconciliation{}, err
	}
	if canonical != finalized {
		return rootActionReconciliation{}, errors.New("root finalized snapshot changed during reconciliation")
	}
	if err := self.network(operationCtx); err != nil {
		return rootActionReconciliation{}, err
	}
	if err := result.validate(action, signed); err != nil {
		return rootActionReconciliation{}, err
	}
	return result, nil
}

// The pinned SDK includes RuntimeEnvironmentUpdated (8), omitted by the older
// GSRPC digest enum. Decode exact modern variants with bounded vector lengths;
// otherwise a legitimate upgrade header could look corrupt or exhaust memory.
func rootReceiptDigest(raw []byte) error {
	reader := rootScaleReader{data: raw}
	tag, err := reader.take(1)
	if err != nil {
		return err
	}
	switch tag[0] {
	case 8:
	case 0, 4, 5, 6:
		if tag[0] != 0 {
			if _, err := reader.take(4); err != nil {
				return err
			}
		}
		length, err := reader.compact()
		if err != nil || length > uint64(len(raw)-reader.offset) {
			return errors.New("root finalized digest vector is truncated")
		}
		if _, err := reader.take(int(length)); err != nil {
			return err
		}
	default:
		return errors.New("root finalized digest variant is not in reviewed SDK")
	}
	if reader.offset != len(raw) {
		return errors.New("root finalized digest has trailing bytes")
	}
	return nil
}

// The receipt port independently verifies the retained sr25519 signature and
// reconstructs every signed field, rather than trusting a caller's byte label.
func rootReceiptSignedAction(action rootAction, signed []byte) error {
	if len(signed) == 0 {
		return nil
	}
	reader := rootScaleReader{data: signed}
	length, err := reader.compact()
	if err != nil || length != uint64(len(signed)-reader.offset) {
		return errors.New("root receipt signed action length differs")
	}
	body := signed[reader.offset:]
	if len(body) < 99 || body[0] != 0x84 || body[1] != 0 || body[34] != 1 {
		return errors.New("root receipt signed action address/signature variant differs")
	}
	expected, err := action.signed(body[35:99])
	if err != nil || !bytes.Equal(expected, signed) {
		return errors.Join(errors.New("root receipt bytes do not authenticate this action"), err)
	}
	return nil
}

// Readback is the block's final state, which may include a later transaction in
// the same block. Its absence is retained separately from an already proven
// dispatch/fee and cannot erase that financial outcome or authorize another send.
func (self *rootCanonicalChain) rootPostState(ctx context.Context, action rootAction, block string) *rootReceiptPostState {
	result := &rootReceiptPostState{}
	err := func() error {
		runtime, err := self.runtimeAt(ctx, block)
		if err != nil {
			return err
		}
		var specs []rootStorageSpec
		for _, spec := range rootStorageSpecs {
			if spec.name == "Weights" || spec.name == "LastUpdate" {
				specs = append(specs, spec)
			}
		}
		entries, err := observationStorageProfile(runtime.metadata, specs)
		if err != nil {
			return err
		}
		reader := rootStorageReader{client: self.client, metadata: runtime.metadata, entries: entries, specs: specs, block: block, valueKVs: map[string]rootStorageValue{}}
		weights, err := reader.read(ctx, "Weights", []byte{0, 0}, binary.LittleEndian.AppendUint16(nil, action.Scope.Seat.Uid))
		if err != nil {
			return err
		}
		lastUpdate, err := reader.read(ctx, "LastUpdate", []byte{0, 0})
		if err != nil {
			return err
		}
		if weights.RawStorage == nil || lastUpdate.RawStorage == nil {
			return errors.New("root receipt post-state rows are absent")
		}
		values := rootScaleReader{data: lastUpdate.data}
		count, err := values.compact()
		if err != nil || count <= uint64(action.Scope.Seat.Uid) {
			return errors.New("root receipt post-state has no selected seat's last update")
		}
		result.WeightsScale = weights.EffectiveScale
		result.LastUpdate = binary.LittleEndian.Uint64(lastUpdate.data[values.offset+int(action.Scope.Seat.Uid)*8:])
		result.LastUpdateStorageHash = rootExtrinsicHash(lastUpdate.data)
		return nil
	}()
	if err != nil {
		result.Issue = err.Error()
		if len(result.Issue) > 1024 {
			result.Issue = result.Issue[:1024]
		}
	}
	return result
}
