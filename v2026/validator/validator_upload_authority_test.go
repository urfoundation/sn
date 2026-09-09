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
}

// Both native clocks are real fixed SCALE responses. Current permit can
// differ from historical permit without modifying either query's identity.
func newValidatorUploadAuthorityTestFixture(t *testing.T) *validatorUploadAuthorityTestFixture {
	t.Helper()
	base := newReleaseActivationV2TestFixture(t, "")
	self := &validatorUploadAuthorityTestFixture{releaseActivationV2TestFixture: base, now: time.Now().Truncate(time.Second), currentHash: types.Hash{0x29}, currentPermit: true,
		records: make(map[[32]byte]ValidatorEvidenceActivationPublication), events: make([]ValidatorUploadActivationEvent, 0)}
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
	base.native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		base.native.ctx = ctx
		if err := ctx.Err(); err != nil {
			return err
		}
		if method == "chain_getFinalizedHead" {
			return setValidatorRuntimeIdentityTestResult(result, self.currentHash.Hex())
		}
		if method == "chain_getBlockHash" && len(args) == 1 && args[0] == uint64(101) {
			self.currentCanonicalCalls++
			if self.reorgAfterCurrentCanonical && self.currentCanonicalCalls > 1 {
				self.observerReorg.Store(true)
			}
			return setValidatorRuntimeIdentityTestResult(result, self.currentHash.Hex())
		}
		if method == "chain_getHeader" && len(args) == 1 && args[0] == self.currentHash.Hex() {
			header, ok := result.(*types.Header)
			if !ok {
				return errors.New("current fixture header target differs")
			}
			*header = types.Header{Number: 101}
			return nil
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
	}}
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
	if len(input) == 36 && bytes.Equal(input[:4], contract.PackActivation([32]byte{})[:4]) && common.BytesToAddress(call["to"]) == self.authority.Journal {
		if selector.BlockHash == nil || *selector.BlockHash != common.Hash(self.blockHash) || !selector.RequireCanonical || selector.BlockNumber != nil {
			return nil, errors.New("fixture activation getter lost observer identity")
		}
		var digest [32]byte
		copy(digest[:], input[4:])
		value := self.records[digest]
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
	if len(filter) != 4 {
		return nil, errors.New("fixture event filter census differs")
	}
	var address common.Address
	var from, to hexutil.Uint64
	var topics []common.Hash
	if err := errors.Join(json.Unmarshal(filter["address"], &address), json.Unmarshal(filter["fromBlock"], &from), json.Unmarshal(filter["toBlock"], &to), json.Unmarshal(filter["topics"], &topics)); err != nil {
		return nil, err
	}
	topic := crypto.Keccak256Hash([]byte("ActivationPublished(bytes32,bytes32,uint64,uint64)"))
	if address != self.authority.Journal || len(topics) != 1 || topics[0] != topic || from > to {
		return nil, errors.New("fixture event filter differs from independent deployment")
	}
	if self.fault == "null-events" {
		return nil, nil
	}
	rows := make([]map[string]any, 0)
	for _, event := range self.events {
		if event.Block < uint64(from) || event.Block > uint64(to) {
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
	events, err := fixture.chain.ValidatorUploadActivationEventsContext(t.Context(), fixture.deployment(), 1001, 1200, 200, 8, observer)
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
		got, err := fixture.chain.ValidatorUploadActivationEventsContext(t.Context(), fixture.deployment(), 1001, 1200, 200, 8, observer)
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
		result, err := fixture.chain.ValidatorUploadActivationEventsContext(ctx, fixture.deployment(), from, to, blocks, events, observer)
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
