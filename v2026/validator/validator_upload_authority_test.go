//go:build linux || darwin

// Existing real EVM and native-reader fixtures add genuine event ABI and
// independently hashed Timestamp/schedule metadata, not an admission verdict.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Mutable fields are changed only between joined calls in these tests.
type validatorUploadAuthorityTestFixture struct {
	*releaseActivationV2TestFixture
	now                        time.Time
	currentHash                types.Hash
	currentPermit              bool
	currentMillis              uint64
	records                    map[[32]byte]ValidatorEvidenceActivationPublication
	events                     []ValidatorUploadActivationEvent
	fault                      string
	currentCanonicalCalls      uint64
	reorgAfterCurrentCanonical bool
	observerReorg              atomic.Bool
	logCalls                   atomic.Uint64
	activationReads            atomic.Uint64
	absentActivationReads      atomic.Uint64
	// Operator IDs besides the expected record's that the coordinator lists as
	// active at its epoch, so one permitted hotkey can be several owners.
	extraOperators map[uint64]bool
	// The hotkey OR-lists and block ranges of the activation queries, in
	// call order, under logLock: a cold scan queries concurrently.
	logLock    sync.Mutex
	logHotkeys [][]common.Hash
	logRanges  [][2]uint64
	// Called for each activation query before it answers; an error fails it.
	logGate func(ctx context.Context, from, to uint64) error
	// Numbered block reads, and canonical hashes that replace an event
	// block's, under logLock.
	blockReads        map[uint64]int
	canonicalRewrites map[uint64]common.Hash
	// When set, the subnet owner's hotkey at the current native hash; the
	// permit census includes it.
	subnetOwnerHotkey [32]byte
	// Native storage round trips for the permit census: batches, and single
	// reads of a census entry made outside a batch.
	censusBatches atomic.Int64
	censusReads   atomic.Int64
}

// A batch answers each key through the fixture's own single reads.
type validatorUploadFixtureBatchKey struct{}

// Both native clocks are real fixed SCALE responses. Current permit can
// differ from historical permit without modifying either query's identity.
func newValidatorUploadAuthorityTestFixture(t *testing.T) *validatorUploadAuthorityTestFixture {
	t.Helper()
	base := newReleaseActivationV2TestFixture(t, "")
	self := &validatorUploadAuthorityTestFixture{releaseActivationV2TestFixture: base, now: time.Now().Truncate(time.Second), currentHash: types.Hash{0x29}, currentPermit: true,
		records: make(map[[32]byte]ValidatorEvidenceActivationPublication), events: make([]ValidatorUploadActivationEvent, 0)}
	currentHeader, currentHash := releaseReceiptTestHeader(t, base.native.block, 101)
	self.currentHash = currentHash
	self.currentMillis = uint64(self.now.UnixMilli())
	original := base.native.chain.API.Client
	var metadataHex string
	if err := original.CallContext(base.native.ctx, &metadataHex, "state_getMetadata", base.native.block.Hex()); err != nil {
		t.Fatal(err)
	}
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	metadata.AsMetadataV14.Pallets[0].Storage.Items = append(metadata.AsMetadataV14.Pallets[0].Storage.Items, types.StorageEntryMetadataV14{
		Name: "SubnetEpochIndex", Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{{IsIdentity: true}}}},
	})
	metadata.AsMetadataV14.Pallets = append(metadata.AsMetadataV14.Pallets, types.PalletMetadataV14{Name: "Timestamp", HasStorage: true, Storage: types.StorageMetadataV14{Prefix: "Timestamp", Items: []types.StorageEntryMetadataV14{{
		Name: "Now", Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Type: types.StorageEntryTypeV14{IsPlainType: true},
	}}}})
	metadataHex, err = codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, metadataHash, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	base.native.expected.MetadataHash = metadataHash
	base.authority.NativeRuntime = base.native.expected
	epochKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "SubnetEpochIndex", binary.LittleEndian.AppendUint16(nil, 521))
	if err != nil {
		t.Fatal(err)
	}
	timeKey, err := types.CreateStorageKey(metadata, "Timestamp", "Now")
	if err != nil {
		t.Fatal(err)
	}
	uidsKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "Uids", binary.LittleEndian.AppendUint16(nil, 521), base.authority.Expected.Hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "SubnetOwnerHotkey", binary.LittleEndian.AppendUint16(nil, 521))
	if err != nil {
		t.Fatal(err)
	}
	// The census entries' storage prefixes, and the permitted UID's Keys entry.
	var censusPrefixes []string
	for _, name := range []string{"SubnetworkN", "ValidatorPermit", "SubnetOwnerHotkey"} {
		key, err := types.CreateStorageKey(metadata, "SubtensorModule", name, binary.LittleEndian.AppendUint16(nil, 521))
		if err != nil {
			t.Fatal(err)
		}
		censusPrefixes = append(censusPrefixes, key.Hex()[:66])
	}
	permittedKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "Keys", binary.LittleEndian.AppendUint16(nil, 521), binary.LittleEndian.AppendUint16(nil, 1))
	if err != nil {
		t.Fatal(err)
	}
	censusPrefixes = append(censusPrefixes, permittedKey.Hex()[:66])
	var hook func(ctx context.Context, result any, method string, args ...any) error
	hook = func(ctx context.Context, result any, method string, args ...any) error {
		base.native.ctx = ctx
		if err := ctx.Err(); err != nil {
			return err
		}
		// As a node answers state_queryStorageAt at one hash: one change set,
		// every key with its value or null, each read as state_getStorage.
		if method == "state_queryStorageAt" && len(args) == 2 {
			self.censusBatches.Add(1)
			keys, _ := args[0].([]string)
			at, _ := args[1].(string)
			if self.fault == "census-batch-unavailable" {
				return errors.New("fixture storage batch is unavailable")
			}
			changes := make([][]any, 0, len(keys))
			for _, key := range keys {
				raw := json.RawMessage("null")
				if err := hook(context.WithValue(ctx, validatorUploadFixtureBatchKey{}, true), &raw, "state_getStorage", key, at); err != nil {
					return err
				}
				changes = append(changes, []any{key, raw})
			}
			if self.fault == "census-batch-partial" && len(changes) != 0 {
				changes = changes[:len(changes)-1]
			}
			block := at
			if self.fault == "census-batch-foreign-block" {
				block = types.Hash{0x66}.Hex()
			}
			encoded, err := json.Marshal([]map[string]any{{"block": block, "changes": changes}})
			if err != nil {
				return err
			}
			return json.Unmarshal(encoded, result)
		}
		if method == "state_getStorage" && len(args) == 2 && ctx.Value(validatorUploadFixtureBatchKey{}) == nil {
			if key, ok := args[0].(string); ok && slices.ContainsFunc(censusPrefixes, func(prefix string) bool { return strings.HasPrefix(key, prefix) }) {
				self.censusReads.Add(1)
			}
		}
		if self.fault == "census-keys-absent" && method == "state_getStorage" && len(args) == 2 && args[0] == permittedKey.Hex() && args[1] == self.currentHash.Hex() {
			return setValidatorRuntimeIdentityTestResult(result, nil)
		}
		if method == "chain_getFinalizedHead" {
			return setValidatorRuntimeIdentityTestResult(result, self.currentHash.Hex())
		}
		if self.fault == "native-anchor-changed" && method == "chain_getBlockHash" && len(args) == 1 && args[0] == base.authority.Expected.NativeBlock {
			return setValidatorRuntimeIdentityTestResult(result, types.Hash{0x77}.Hex())
		}
		if self.fault == "current-schedule-unavailable" && method == "state_getStorage" && len(args) == 2 && args[0] != timeKey.Hex() && args[1] == self.currentHash.Hex() {
			return errors.New("fixture current native storage is unavailable")
		}
		if self.fault == "current-hotkey-unregistered" && method == "state_getStorage" && len(args) == 2 && args[0] == uidsKey.Hex() && args[1] == self.currentHash.Hex() {
			// A deregistered hotkey has no reverse UID mapping at the current hash.
			return setValidatorRuntimeIdentityTestResult(result, nil)
		}
		if method == "chain_getBlockHash" && len(args) == 1 && args[0] == uint64(101) {
			self.currentCanonicalCalls++
			if self.reorgAfterCurrentCanonical && self.currentCanonicalCalls > 1 {
				self.observerReorg.Store(true)
			}
			return setValidatorRuntimeIdentityTestResult(result, self.currentHash.Hex())
		}
		if method == "chain_getHeader" && len(args) == 1 && args[0] == self.currentHash.Hex() {
			return setReleaseHistoricalTestResult(result, releaseReceiptTestHeaderWire(currentHeader))
		}
		if method == "state_getMetadata" {
			return setValidatorRuntimeIdentityTestResult(result, metadataHex)
		}
		if method == "state_getStorage" && len(args) == 2 {
			if args[0] == timeKey.Hex() {
				if args[1] != self.currentHash.Hex() {
					return errors.New("timestamp was not read at current finalized native hash")
				}
				return setValidatorRuntimeIdentityTestResult(result, "0x"+hex.EncodeToString(binary.LittleEndian.AppendUint64(nil, self.currentMillis)))
			}
			if args[0] == epochKey.Hex() {
				return setValidatorRuntimeIdentityTestResult(result, "0x"+hex.EncodeToString(binary.LittleEndian.AppendUint64(nil, 77)))
			}
			if self.subnetOwnerHotkey != ([32]byte{}) {
				if args[0] == ownerKey.Hex() && args[1] == self.currentHash.Hex() {
					return setValidatorRuntimeIdentityTestResult(result, "0x"+hex.EncodeToString(self.subnetOwnerHotkey[:]))
				}
				// The owner's hotkey holds no UID on the subnet.
				ownerUIDKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "Uids", binary.LittleEndian.AppendUint16(nil, 521), self.subnetOwnerHotkey[:])
				if err != nil {
					return err
				}
				if args[0] == ownerUIDKey.Hex() {
					return setValidatorRuntimeIdentityTestResult(result, nil)
				}
			}
		}
		args = append([]any(nil), args...)
		current := false
		for index, value := range args {
			if text, ok := value.(string); ok && text == self.currentHash.Hex() {
				args[index] = base.native.block.Hex()
				current = true
			}
		}
		permit := base.native.permit
		if current {
			base.native.permit = self.currentPermit
		}
		defer func() { base.native.permit = permit }()
		return original.CallContext(ctx, result, method, args...)
	}
	base.native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: hook}
	self.publish(t, base.authority.Expected, 1001, ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...)))
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", self); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	self.chain, err = DialReleaseChainContext(t.Context(), []string{httpServer.URL}, common.Address(base.authority.Expected.Domain.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(self.chain.Close)
	return self
}

// Some admission tests publish or hide activations after a refresh, in blocks
// that refresh already read as finalized, which a finalized chain cannot do.
// With the head inside the fixture's one discovery range, the range stays
// open and every refresh reads it again, as every refresh did before closed
// ranges were kept.
func (self *validatorUploadAuthorityTestFixture) keepDiscoveryRangeOpen() {
	self.block = 1199
}

// Each fixture publication first verifies genuine independently held consent
// keys. The scripted getter then models immutable compiler-qualified storage.
func (self *validatorUploadAuthorityTestFixture) publish(t *testing.T, record protocol.ValidatorEvidenceActivation, block uint64, privateKey ed25519.PrivateKey) {
	t.Helper()
	vpkSignature, err := record.SignVPK(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Verify(record, vpkSignature, hotkeySignature); err != nil {
		t.Fatalf("real activation consent prerequisite: %v", err)
	}
	if _, exists := self.records[digest]; exists {
		t.Fatal("fixture attempted to overwrite immutable activation")
	}
	self.records[digest] = ValidatorEvidenceActivationPublication{Record: record, PublishedBlock: block}
	self.events = append(self.events, ValidatorUploadActivationEvent{Digest: digest, Hotkey: record.Hotkey, NoID: record.NoID, Epoch: record.Domain.Epoch, Block: block, BlockHash: [32]byte(common.BigToHash(new(big.Int).SetUint64(block))), Index: uint64(len(self.events))})
}

// The exact immutable/static fields remain independent of any upload header.
func (self *validatorUploadAuthorityTestFixture) deployment() ValidatorUploadDeployment {
	domain := self.authority.Expected.Domain
	return ValidatorUploadDeployment{ChainID: domain.ChainID, GenesisHash: domain.GenesisHash, Netuid: domain.Netuid, Coordinator: domain.Coordinator, SettlementVault: domain.SettlementVault,
		DeploymentIDHash: domain.DeploymentIDHash, Journal: [20]byte(self.authority.Journal), RuntimeHash: self.authority.RuntimeHash, DeploymentBlock: 1001, NativeRuntime: self.native.expected, MaximumSubnetUIDs: 3}
}

// These are explicit tiny test capacities, not production defaults.
func (self *validatorUploadAuthorityTestFixture) config() ValidatorUploadAdmissionConfig {
	return ValidatorUploadAdmissionConfig{Deployment: self.deployment(), ReplicaNoID: 9, MaximumContextBytes: 64 * 1024, MaximumOwners: 4, BlocksPerRange: 200, MaximumRanges: 1, MaximumEventsPerRange: 8,
		RefreshSeconds: 10, MaximumRefreshSeconds: 20, MaximumHeadAgeSeconds: 60, MaximumIntentSeconds: 25, FreshActivePerOwner: 1, RetryActivePerOwner: 1}
}

// Header timestamp is a required actual field, not callback completion time.
func (self *validatorUploadAuthorityTestFixture) GetBlockByNumber(ctx context.Context, block gethrpc.BlockNumber, full bool) (map[string]any, error) {
	if self.fault == "canonical-unavailable" && block == gethrpc.BlockNumber(self.block) {
		return nil, context.DeadlineExceeded
	}
	if full {
		return nil, errors.New("staging fixture does not return full blocks")
	}
	var value map[string]any
	if block == gethrpc.FinalizedBlockNumber {
		var err error
		value, err = self.releaseActivationV2TestFixture.GetBlockByNumber(ctx, block, full)
		if err != nil {
			return nil, err
		}
	} else {
		self.calls.Add(1)
		number, hash := uint64(block), common.Hash{}
		switch number {
		case self.block:
			hash = common.Hash(self.blockHash)
			if self.observerReorg.Load() {
				hash[0] ^= 1
			}
		case self.authority.Expected.EVMBlock:
			hash = common.Hash(self.authority.Expected.EVMHash)
		}
		for _, event := range self.events {
			if number == event.Block {
				hash = common.Hash(event.BlockHash)
			}
		}
		self.logLock.Lock()
		if self.blockReads == nil {
			self.blockReads = make(map[uint64]int)
		}
		self.blockReads[number]++
		if rewritten, ok := self.canonicalRewrites[number]; ok {
			hash = rewritten
		}
		self.logLock.Unlock()
		if block < 0 || hash == (common.Hash{}) {
			return nil, errors.New("staging fixture has no such canonical numbered block")
		}
		value = map[string]any{"number": hexutil.EncodeUint64(number), "hash": hash}
	}
	if self.fault != "missing-timestamp" {
		value["timestamp"] = hexutil.EncodeUint64(uint64(self.now.Unix()))
	}
	return value, ctx.Err()
}

// Event blocks have their own canonical number/hash pairs.
func (self *validatorUploadAuthorityTestFixture) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (map[string]any, error) {
	if full {
		return nil, errors.New("staging fixture does not return full blocks")
	}
	for _, event := range self.events {
		if hash == common.Hash(event.BlockHash) {
			return map[string]any{"number": hexutil.EncodeUint64(event.Block), "hash": hash}, ctx.Err()
		}
	}
	return self.releaseActivationV2TestFixture.GetBlockByHash(ctx, hash, full)
}

// ABI getters cover both retained and rotated original records; no mutable
// runtime self-description is accepted as a substitute for the configured hash.
func (self *validatorUploadAuthorityTestFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	contract := stabi.NewSTValidatorEvidence()
	input := call["input"]
	coordinator := stabi.NewSTCoordinator()
	epoch := new(big.Int).SetUint64(self.authority.Expected.Domain.Epoch)
	for noID := range self.extraOperators {
		if !bytes.Equal(input, coordinator.PackOperatorAt(new(big.Int).SetUint64(noID), epoch)) || common.BytesToAddress(call["to"]) != common.Address(self.authority.Expected.Domain.Coordinator) {
			continue
		}
		parsed, err := stabi.STCoordinatorMetaData.ParseABI()
		if err != nil {
			return nil, err
		}
		encoded, err := parsed.Methods["operatorAt"].Outputs.Pack(stabi.STCoordinatorOperatorVersion{Active: true, EffectiveEpoch: self.authority.Expected.Domain.Epoch})
		return encoded, errors.Join(err, ctx.Err())
	}
	if len(input) == 36 && bytes.Equal(input[:4], contract.PackActivation([32]byte{})[:4]) && common.BytesToAddress(call["to"]) == self.authority.Journal {
		self.activationReads.Add(1)
		if selector.BlockHash == nil || *selector.BlockHash != common.Hash(self.blockHash) || !selector.RequireCanonical || selector.BlockNumber != nil {
			return nil, errors.New("fixture activation getter lost observer identity")
		}
		var digest [32]byte
		copy(digest[:], input[4:])
		value := self.records[digest]
		if value.PublishedBlock == 0 {
			self.absentActivationReads.Add(1)
		}
		stored := stabi.STValidatorEvidenceActivation{Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(value.Record), PublishedBlock: value.PublishedBlock}
		if self.fault == "wrong-digest" {
			stored.Record.NoId++
		}
		parsed, err := stabi.STValidatorEvidenceMetaData.ParseABI()
		if err != nil {
			return nil, err
		}
		encoded, err := parsed.Methods["activation"].Outputs.Pack(stored)
		if self.fault == "activation-trailing" {
			encoded = append(encoded, 0)
		}
		return encoded, err
	}
	return self.releaseActivationV2TestFixture.Call(ctx, call, selector)
}

// The real JSON-RPC method requires the full approved address/topic/range and
// returns canonical event words. Faults mutate actual transport responses.
func (self *validatorUploadAuthorityTestFixture) GetLogs(ctx context.Context, filter map[string]json.RawMessage) ([]map[string]any, error) {
	self.logCalls.Add(1)
	if self.fault == "logs-denied" {
		return nil, errors.New("Method not allowed")
	}
	if len(filter) != 4 {
		return nil, errors.New("fixture event filter census differs")
	}
	var address common.Address
	var from, to hexutil.Uint64
	var topics []json.RawMessage
	if err := errors.Join(json.Unmarshal(filter["address"], &address), json.Unmarshal(filter["fromBlock"], &from), json.Unmarshal(filter["toBlock"], &to), json.Unmarshal(filter["topics"], &topics)); err != nil {
		return nil, err
	}
	topic := crypto.Keccak256Hash([]byte("ActivationPublished(bytes32,bytes32,uint64,uint64)"))
	var first common.Hash
	if len(topics) == 0 || json.Unmarshal(topics[0], &first) != nil || address != self.authority.Journal || first != topic || from > to {
		return nil, errors.New("fixture event filter differs from independent deployment")
	}
	// As a node does: the third topic is an OR-list of hotkeys, the second
	// any digest. A filter without it reads every hotkey's activation.
	var hotkeys map[common.Hash]bool
	switch len(topics) {
	case 1:
	case 3:
		var list []common.Hash
		if string(topics[1]) != "null" || json.Unmarshal(topics[2], &list) != nil || len(list) == 0 {
			return nil, errors.New("fixture event filter topics are malformed")
		}
		self.logLock.Lock()
		self.logHotkeys = append(self.logHotkeys, list)
		self.logRanges = append(self.logRanges, [2]uint64{uint64(from), uint64(to)})
		self.logLock.Unlock()
		if self.logGate != nil {
			if err := self.logGate(ctx, uint64(from), uint64(to)); err != nil {
				return nil, err
			}
		}
		hotkeys = make(map[common.Hash]bool, len(list))
		for _, hotkey := range list {
			hotkeys[hotkey] = true
		}
	default:
		return nil, errors.New("fixture event filter topics are malformed")
	}
	if self.fault == "unfiltered-node" {
		hotkeys = nil
	}
	if self.fault == "null-events" {
		return nil, nil
	}
	rows := make([]map[string]any, 0)
	if self.fault == "empty-logs" {
		// A log-pruned provider returns a well-formed empty range.
		return rows, ctx.Err()
	}
	for _, event := range self.events {
		if event.Block < uint64(from) || event.Block > uint64(to) || hotkeys != nil && !hotkeys[common.Hash(event.Hotkey)] {
			continue
		}
		epoch := common.BigToHash(new(big.Int).SetUint64(event.Epoch))
		row := map[string]any{"address": address, "topics": []common.Hash{topic, common.Hash(event.Digest), common.Hash(event.Hotkey), common.BigToHash(new(big.Int).SetUint64(event.NoID))},
			"data": hexutil.Bytes(epoch[:]), "blockNumber": hexutil.EncodeUint64(event.Block), "blockHash": common.Hash(event.BlockHash), "logIndex": hexutil.EncodeUint64(event.Index), "removed": false}
		switch self.fault {
		case "removed-event":
			row["removed"] = true
		case "missing-index":
			delete(row, "logIndex")
		case "foreign-event":
			row["address"] = common.Address{0xff}
		case "short-event":
			row["data"] = "0x00"
		}
		rows = append(rows, row)
		if self.fault == "duplicate-event" {
			rows = append(rows, row)
		}
	}
	return rows, ctx.Err()
}

// A later external validator is discoverable without any startup reference.
func TestValidatorUploadAuthorityDiscoversAndAuthenticatesRealAnchor(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	observer, err := fixture.chain.ValidatorUploadObserverContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	events, err := fixture.chain.ValidatorUploadActivationEventsContext(t.Context(), fixture.deployment(), 1001, 1200, 200, 8, [][32]byte{fixture.authority.Expected.Hotkey}, observer)
	if err != nil || len(events) != 1 {
		t.Fatalf("real finalized event discovery: %v", err)
	}
	verified, err := fixture.chain.AuthenticateValidatorUploadActivationContext(t.Context(), fixture.native.chain, fixture.deployment(), events[0].Digest, observer)
	if err != nil || verified.Publication.Record != fixture.authority.Expected || verified.Native.Identity.Hotkey != events[0].Hotkey || verified.Native.Identity.UID != 1 || !verified.Native.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("real anchored historical staging authority: %+v / %v", verified, err)
	}
	current, err := ValidatorUploadNativeObserverContext(t.Context(), fixture.native.chain, fixture.deployment())
	if err != nil || current.Hash != fixture.currentHash || current.Number != 101 || current.TimestampMillis != uint64(fixture.now.UnixMilli()) {
		t.Fatalf("current native freshness came from historical or EVM clock: %+v / %v", current, err)
	}
}

// Errors are not absence, and event fields cannot authorize a neighboring
// digest/domain even when a real valid activation remains at the same anchor.
func TestValidatorUploadAuthorityRejectsMalformedDiscoveryAndRecords(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"missing-timestamp", "null-events", "removed-event", "missing-index", "foreign-event", "short-event", "duplicate-event", "wrong-digest", "activation-trailing"} {
		fixture := newValidatorUploadAuthorityTestFixture(t)
		observer, err := fixture.chain.ValidatorUploadObserverContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		fixture.fault = fault
		if fault == "missing-timestamp" {
			if got, err := fixture.chain.ValidatorUploadObserverContext(t.Context()); err == nil || got != (ValidatorUploadObserver{}) {
				t.Fatalf("%s observer escaped: %+v / %v", fault, got, err)
			}
			continue
		}
		if fault == "wrong-digest" || fault == "activation-trailing" {
			got, err := fixture.chain.AuthenticateValidatorUploadActivationContext(t.Context(), fixture.native.chain, fixture.deployment(), fixture.events[0].Digest, observer)
			if err == nil || got != (VerifiedReleaseActivationV2{}) {
				t.Fatalf("%s record escaped: %+v / %v", fault, got, err)
			}
			continue
		}
		got, err := fixture.chain.ValidatorUploadActivationEventsContext(t.Context(), fixture.deployment(), 1001, 1200, 200, 8, [][32]byte{fixture.authority.Expected.Hotkey}, observer)
		if err == nil || got != nil {
			t.Fatalf("%s event escaped: %v / %v", fault, got, err)
		}
	}
}

// The independent runtime, anchor and historical/native observations remain
// authoritative even though all candidate records carry valid real consent.
func TestValidatorUploadAuthorityRejectsNativeAndImmutableDrift(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"runtime", "genesis", "anchor", "permit", "stake", "policy"} {
		fixture := newValidatorUploadAuthorityTestFixture(t)
		observer, err := fixture.chain.ValidatorUploadObserverContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		deployment := fixture.deployment()
		switch fault {
		case "runtime":
			deployment.RuntimeHash[0] ^= 1
		case "genesis":
			deployment.GenesisHash[0] ^= 1
		case "anchor":
			fixture.releaseActivationV2TestFixture.fault = "foreign-anchor"
		case "permit":
			fixture.native.permit = false
		case "stake":
			fixture.native.threshold = 151
		case "policy":
			fixture.releaseActivationV2TestFixture.fault = "wrong-policy"
		}
		result, err := fixture.chain.AuthenticateValidatorUploadActivationContext(t.Context(), fixture.native.chain, deployment, fixture.events[0].Digest, observer)
		if err == nil || result != (VerifiedReleaseActivationV2{}) {
			t.Fatalf("%s authority drift escaped: %+v / %v", fault, result, err)
		}
	}
}

// Every invalid independent range/limit refuses before the actual RPC census.
func TestValidatorUploadAuthorityBoundsAndPreCancellationPrecedeRPC(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	observer := ValidatorUploadObserver{Number: 1200, Hash: fixture.blockHash, Timestamp: uint64(fixture.now.Unix())}
	for _, fault := range []string{"blocks", "events", "from", "to", "future", "cancel", "nil"} {
		from, to, blocks, events, ctx := uint64(1001), uint64(1200), uint64(200), uint64(8), t.Context()
		switch fault {
		case "blocks":
			blocks = 199
		case "events":
			events = 0
		case "from":
			from = 1000
		case "to":
			to = 1000
		case "future":
			to = 1201
		case "cancel":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		case "nil":
			ctx = nil
		}
		before := fixture.calls.Load()
		result, err := fixture.chain.ValidatorUploadActivationEventsContext(ctx, fixture.deployment(), from, to, blocks, events, [][32]byte{fixture.authority.Expected.Hotkey}, observer)
		if err == nil || result != nil || fixture.calls.Load() != before {
			t.Fatalf("%s bad bounds reached RPC: %v / %v", fault, result, err)
		}
	}
}

// Pretty names stay diagnostic only; tests do not derive domain authority
// from them or provide an eligibility callback to the production cache.
func (self *validatorUploadAuthorityTestFixture) String() string {
	return fmt.Sprintf("real staging fixture at native%d/EVM%d", self.native.blockNumber, self.block)
}
