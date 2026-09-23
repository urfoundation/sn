// Historical identity controls use real metadata encoding and scripted,
// block-pinned RPCs. They prove reader behavior, not live activation inclusion.
package crv4

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Each fixture owns its RPC transcript and response bytes. Concurrent controls
// join the single reader before inspecting or changing that owned transcript.
type validatorIdentityTestFixture struct {
	ctx          context.Context
	chain        *Chain
	query        ValidatorIdentityQuery
	allowed      []RuntimeArtifactIdentity
	version      RuntimeVersionIdentity
	codeHash     string
	metadata     *types.Metadata
	metadataHex  string
	blockHashes  map[uint64]types.Hash
	headers      map[string]types.Header
	finalized    types.Hash
	hotkey       [32]byte
	coldkey      [32]byte
	stake        uint64
	keyNames     map[string]string
	storage      map[string]json.RawMessage
	calls        []string
	storageCalls []string
	hook         func(context.Context, any, string, ...any) (bool, error)
	after        func(string, ...any)
}

// Hex results contain exactly one JSON string and no decoder-side re-encoding.
func validatorIdentityTestHex(value []byte) json.RawMessage {
	return json.RawMessage("\"0x" + hex.EncodeToString(value) + "\"")
}

// Uses the runtime-v454 map hashers and argument order, including Blake2
// account keys. The tiny portable metadata is encoded/decoded by the real SDK.
func newValidatorIdentityTestFixture(t *testing.T) *validatorIdentityTestFixture {
	t.Helper()
	identityHasher := types.StorageHasherV10{IsIdentity: true}
	accountHasher := types.StorageHasherV10{IsBlake2_128Concat: true}
	entry := func(name string, optional bool, hashers ...types.StorageHasherV10) types.StorageEntryMetadataV14 {
		return types.StorageEntryMetadataV14{
			Name:     types.Text(name),
			Modifier: types.StorageFunctionModifierV0{IsOptional: optional, IsDefault: !optional},
			Type:     types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: hashers}},
		}
	}
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{
		Name:       types.Text(PalletName),
		HasStorage: true,
		Storage: types.StorageMetadataV14{
			Prefix: types.Text(PalletName),
			Items: []types.StorageEntryMetadataV14{
				entry("SubnetworkN", false, identityHasher),
				entry("Keys", false, identityHasher, identityHasher),
				entry("Uids", true, identityHasher, accountHasher),
				entry("Owner", false, accountHasher),
				entry("TotalHotkeyAlpha", false, accountHasher, identityHasher),
				entry("ValidatorPermit", false, identityHasher),
			},
		},
	}}
	query := ValidatorIdentityQuery{
		GenesisHash: types.Hash{1}, BlockHash: types.Hash{2}, BlockNumber: 100,
		Netuid: 521, UID: 1, MaximumSubnetUIDs: 256,
	}
	fixture := &validatorIdentityTestFixture{
		ctx:         context.Background(),
		query:       query,
		version:     RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 1, StateVersion: 1},
		codeHash:    types.Hash{4}.Hex(),
		metadata:    metadata,
		blockHashes: map[uint64]types.Hash{0: query.GenesisHash, 100: query.BlockHash, 103: types.Hash{3}},
		headers: map[string]types.Header{
			query.BlockHash.Hex(): {Number: types.BlockNumber(100)},
			types.Hash{3}.Hex():   {Number: types.BlockNumber(103)},
		},
		finalized: types.Hash{3},
		hotkey:    [32]byte{11},
		coldkey:   [32]byte{12},
		stake:     9_123_456_789_012_345,
		keyNames:  map[string]string{},
		storage:   map[string]json.RawMessage{},
	}
	fixture.allowed = []RuntimeArtifactIdentity{{Version: fixture.version, CodeHash: fixture.codeHash}}
	fixture.publishMetadata(t)
	// Fixture keys deliberately use the SDK plus explicit little-endian bytes,
	// not the production reader's argument construction.
	netuid := binary.LittleEndian.AppendUint16(nil, query.Netuid)
	uid := binary.LittleEndian.AppendUint16(nil, query.UID)
	for _, field := range []struct {
		name  string
		args  [][]byte
		value []byte
	}{
		{name: "SubnetworkN", args: [][]byte{netuid}, value: []byte{3, 0}},
		{name: "Keys", args: [][]byte{netuid, uid}, value: fixture.hotkey[:]},
		{name: "Uids", args: [][]byte{netuid, fixture.hotkey[:]}, value: uid},
		{name: "Owner", args: [][]byte{fixture.hotkey[:]}, value: fixture.coldkey[:]},
		{name: "TotalHotkeyAlpha", args: [][]byte{fixture.hotkey[:], netuid}, value: binary.LittleEndian.AppendUint64(nil, fixture.stake)},
		{name: "ValidatorPermit", args: [][]byte{netuid}, value: []byte{12, 0, 1, 0}},
	} {
		key, err := types.CreateStorageKey(metadata, PalletName, field.name, field.args...)
		if err != nil {
			t.Fatal(err)
		}
		if _, duplicate := fixture.keyNames[key.Hex()]; duplicate {
			t.Fatal("fixture storage keys collide")
		}
		fixture.keyNames[key.Hex()] = field.name
		fixture.storage[field.name] = validatorIdentityTestHex(field.value)
	}
	// A deliberately unrelated dial-time metadata object cannot satisfy any
	// identity key lookup, even though the historical authenticated one can.
	currentMetadata := types.NewMetadataV14()
	currentMetadata.MagicNumber = types.MagicNumber
	fixture.chain = &Chain{
		API:         &gsrpc.SubstrateAPI{Client: &runtimeIdentityTestClient{callContext: fixture.call}},
		GenesisHash: query.GenesisHash,
		Meta:        currentMetadata,
		Runtime:     &types.RuntimeVersion{SpecName: "dial-time-only", SpecVersion: 999},
	}
	return fixture
}

// Refreshes fixture-authorized metadata bytes without any RPC or live chain.
func (self *validatorIdentityTestFixture) publishMetadata(t *testing.T) {
	t.Helper()
	encoded, err := codec.EncodeToHex(self.metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	self.metadataHex = encoded
	self.allowed[0].MetadataHash = digest
}

// Refuses all contextless calls, wrong block arguments, unknown keys and
// methods outside the reader's read-only transcript.
func (self *validatorIdentityTestFixture) call(ctx context.Context, result any, method string, args ...any) error {
	if ctx != self.ctx {
		return errors.New("validator identity RPC changed caller context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	label := method
	if method == "state_getStorage" && len(args) == 2 {
		key, ok := args[0].(string)
		if !ok || self.keyNames[key] == "" {
			return fmt.Errorf("unexpected validator identity storage key %v", args[0])
		}
		label += ":" + self.keyNames[key]
		self.storageCalls = append(self.storageCalls, self.keyNames[key])
	}
	self.calls = append(self.calls, label)
	if self.hook != nil {
		if handled, err := self.hook(ctx, result, method, args...); handled || err != nil {
			return err
		}
	}
	if self.after != nil {
		defer self.after(method, args...)
	}
	checkArgs := func(expected ...any) error {
		if !reflect.DeepEqual(args, expected) {
			return fmt.Errorf("%s args=%v, want %v", method, args, expected)
		}
		return nil
	}
	switch method {
	case "chain_getBlockHash":
		if len(args) != 1 {
			return errors.New("block hash requires one height")
		}
		number, ok := args[0].(uint64)
		if !ok {
			return errors.New("block hash height is not the pinned uint64")
		}
		value, found := self.blockHashes[number]
		if !found {
			return fmt.Errorf("unexpected block height %d", number)
		}
		return setRuntimeIdentityTestResult(result, value.Hex())
	case "chain_getHeader":
		if len(args) != 1 {
			return errors.New("header requires one hash")
		}
		hash, ok := args[0].(string)
		header, found := self.headers[hash]
		if !ok || !found {
			return fmt.Errorf("unexpected header hash %v", args[0])
		}
		target, ok := result.(*types.Header)
		if !ok {
			return fmt.Errorf("unexpected header result %T", result)
		}
		*target = header
		return nil
	case "chain_getFinalizedHead":
		if err := checkArgs(); err != nil {
			return err
		}
		return setRuntimeIdentityTestResult(result, self.finalized.Hex())
	case "state_getRuntimeVersion":
		if err := checkArgs(self.query.BlockHash.Hex()); err != nil {
			return err
		}
		return setRuntimeIdentityTestResult(result, self.version)
	case "state_getStorageHash":
		if err := checkArgs("0x3a636f6465", self.query.BlockHash.Hex()); err != nil {
			return err
		}
		return setRuntimeIdentityTestResult(result, self.codeHash)
	case "state_getMetadata":
		if err := checkArgs(self.query.BlockHash.Hex()); err != nil {
			return err
		}
		return setRuntimeIdentityTestResult(result, self.metadataHex)
	case "state_getStorage":
		if len(args) != 2 || args[1] != self.query.BlockHash.Hex() {
			return errors.New("validator identity storage used a different block")
		}
		target, ok := result.(*json.RawMessage)
		if !ok {
			return fmt.Errorf("unexpected storage result %T", result)
		}
		*target = append(json.RawMessage(nil), self.storage[self.keyNames[args[0].(string)]]...)
		return nil
	default:
		return fmt.Errorf("unexpected or mutating validator identity method %s", method)
	}
}

// The complete observed tuple comes from sixteen explicit historical reads,
// leaving the dial-time signing metadata/runtime untouched.
func TestRuntimeArtifactMetadataValidatorIdentityBindsHistoricalState(t *testing.T) {
	t.Parallel()
	fixture := newValidatorIdentityTestFixture(t)
	type callerKey struct{}
	fixture.ctx = context.WithValue(context.Background(), callerKey{}, "historical-validator")
	originalMetadata, originalRuntime := fixture.chain.Meta, fixture.chain.Runtime
	originalRuntimeValue := *originalRuntime
	observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
	expected := ValidatorIdentityObservation{
		GenesisHash: fixture.query.GenesisHash, BlockHash: fixture.query.BlockHash, BlockNumber: 100,
		FinalizedHash: fixture.finalized, FinalizedNumber: 103,
		Netuid: 521, UID: 1, SubnetUIDs: 3,
		Hotkey: fixture.hotkey, Coldkey: fixture.coldkey,
		StakeAlphaRao: fixture.stake, ValidatorPermit: true, Runtime: fixture.allowed[0],
	}
	if err != nil || observed != expected {
		t.Fatalf("historical identity=%+v error=%v, want %+v", observed, err, expected)
	}
	expectedCalls := []string{
		"chain_getBlockHash", "chain_getBlockHash", "chain_getHeader",
		"chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash",
		"state_getRuntimeVersion", "state_getStorageHash", "state_getMetadata",
		"state_getStorage:SubnetworkN", "state_getStorage:Keys", "state_getStorage:Uids",
		"state_getStorage:Owner", "state_getStorage:TotalHotkeyAlpha",
		"state_getStorage:ValidatorPermit", "chain_getBlockHash",
	}
	if !reflect.DeepEqual(fixture.calls, expectedCalls) {
		t.Fatalf("historical RPC transcript=%v, want %v", fixture.calls, expectedCalls)
	}
	if fixture.chain.Meta != originalMetadata || fixture.chain.Runtime != originalRuntime ||
		!reflect.DeepEqual(*fixture.chain.Runtime, originalRuntimeValue) || len(originalMetadata.AsMetadataV14.Pallets) != 0 {
		t.Fatal("historical read rebound or mutated dial-time signing state")
	}
}

// Invalid caller bounds and mismatched dialed genesis are rejected before
// any provider can supply authority or allocate a census-sized observation.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsInvalidQueriesBeforeRPC(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name   string
		change func(*validatorIdentityTestFixture)
	}{
		{name: "nil context", change: func(f *validatorIdentityTestFixture) { f.ctx = nil }},
		{name: "nil chain", change: func(f *validatorIdentityTestFixture) { f.chain = nil }},
		{name: "nil API", change: func(f *validatorIdentityTestFixture) { f.chain.API = nil }},
		{name: "nil client", change: func(f *validatorIdentityTestFixture) { f.chain.API.Client = nil }},
		{name: "zero genesis", change: func(f *validatorIdentityTestFixture) { f.query.GenesisHash = types.Hash{} }},
		{name: "dialed genesis", change: func(f *validatorIdentityTestFixture) { f.chain.GenesisHash = types.Hash{99} }},
		{name: "zero block", change: func(f *validatorIdentityTestFixture) { f.query.BlockHash = types.Hash{} }},
		{name: "zero height", change: func(f *validatorIdentityTestFixture) { f.query.BlockNumber = 0 }},
		{name: "height overflow", change: func(f *validatorIdentityTestFixture) { f.query.BlockNumber = uint64(math.MaxUint32) + 1 }},
		{name: "root subnet", change: func(f *validatorIdentityTestFixture) { f.query.Netuid = 0 }},
		{name: "zero capacity", change: func(f *validatorIdentityTestFixture) { f.query.MaximumSubnetUIDs = 0 }},
		{name: "capacity overflow", change: func(f *validatorIdentityTestFixture) { f.query.MaximumSubnetUIDs = uint32(math.MaxUint16) + 1 }},
		{name: "UID beyond capacity", change: func(f *validatorIdentityTestFixture) { f.query.UID = uint16(f.query.MaximumSubnetUIDs) }},
	} {
		fixture := newValidatorIdentityTestFixture(t)
		example.change(fixture)
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.calls) != 0 {
			t.Errorf("%s: result=%+v calls=%v error=%v", example.name, observed, fixture.calls, err)
		}
	}
}

// Caller allowlists must be complete, finite and unambiguous before any RPC;
// a provider's own tuple cannot repair missing independent authority.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsInvalidAuthorityBeforeRPC(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name   string
		change func(*validatorIdentityTestFixture)
	}{
		{name: "missing", change: func(f *validatorIdentityTestFixture) { f.allowed = nil }},
		{name: "duplicate", change: func(f *validatorIdentityTestFixture) { f.allowed = append(f.allowed, f.allowed[0]) }},
		{name: "too many", change: func(f *validatorIdentityTestFixture) {
			for i := 0; i < maximumRuntimeMetadataArtifactsPerChain; i++ {
				value := f.allowed[0]
				value.Version.SpecVersion += uint32(i + 1)
				f.allowed = append(f.allowed, value)
			}
		}},
		{name: "missing spec name", change: func(f *validatorIdentityTestFixture) { f.allowed[0].Version.SpecName = "" }},
		{name: "short code hash", change: func(f *validatorIdentityTestFixture) { f.allowed[0].CodeHash = "0x01" }},
		{name: "missing metadata hash", change: func(f *validatorIdentityTestFixture) { f.allowed[0].MetadataHash = "" }},
	} {
		fixture := newValidatorIdentityTestFixture(t)
		example.change(fixture)
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.calls) != 0 {
			t.Errorf("%s: result=%+v calls=%v error=%v", example.name, observed, fixture.calls, err)
		}
	}
}

// The exact configured authority bound remains usable with the selected
// historical artifact last. One additional distinct tuple fails before reads.
func TestRuntimeArtifactMetadataValidatorIdentityExactAuthorityBound(t *testing.T) {
	t.Parallel()
	for _, extra := range []bool{false, true} {
		fixture := newValidatorIdentityTestFixture(t)
		selected := fixture.allowed[0]
		fixture.allowed = nil
		count := maximumRuntimeMetadataArtifactsPerChain
		if extra {
			count++
		}
		for index := 1; index < count; index++ {
			identity := selected
			identity.Version.SpecVersion += uint32(index)
			fixture.allowed = append(fixture.allowed, identity)
		}
		fixture.allowed = append(fixture.allowed, selected)
		if len(fixture.allowed) != count {
			t.Fatal("authority boundary fixture has a different cardinality")
		}
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if extra {
			if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.calls) != 0 {
				t.Fatalf("one-over authority reached a provider: result=%+v calls=%v error=%v", observed, fixture.calls, err)
			}
		} else if err != nil || observed.Runtime != selected || observed.Hotkey != fixture.hotkey || observed.Coldkey != fixture.coldkey || observed.StakeAlphaRao != fixture.stake || !observed.ValidatorPermit || len(fixture.calls) != 16 || len(fixture.storageCalls) != 6 {
			t.Fatalf("exact authority bound did not authenticate its selected historical source: result=%+v calls=%v error=%v", observed, fixture.calls, err)
		}
	}
}

// A contradictory genesis, height, header or finalized checkpoint cannot
// reach runtime authentication or economic storage reads.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsNativeLineageMismatch(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name   string
		change func(*validatorIdentityTestFixture)
	}{
		{name: "RPC genesis", change: func(f *validatorIdentityTestFixture) { f.blockHashes[0] = types.Hash{9} }},
		{name: "selected canonical hash", change: func(f *validatorIdentityTestFixture) { f.blockHashes[100] = types.Hash{9} }},
		{name: "selected header number", change: func(f *validatorIdentityTestFixture) { f.headers[f.query.BlockHash.Hex()] = types.Header{Number: 99} }},
		{name: "not finalized", change: func(f *validatorIdentityTestFixture) { f.headers[f.finalized.Hex()] = types.Header{Number: 99} }},
		{name: "finalized canonical hash", change: func(f *validatorIdentityTestFixture) { f.blockHashes[103] = types.Hash{9} }},
		{name: "zero finalized hash", change: func(f *validatorIdentityTestFixture) { f.finalized = types.Hash{} }},
	} {
		fixture := newValidatorIdentityTestFixture(t)
		example.change(fixture)
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.storageCalls) != 0 {
			t.Errorf("%s: result=%+v storage=%v error=%v", example.name, observed, fixture.storageCalls, err)
		}
	}
}

// Version, Wasm and metadata are independent pins; a matching version does
// not authorize a different artifact or a fallback to dial-time metadata.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsUnauthenticatedRuntime(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name   string
		change func(*validatorIdentityTestFixture)
	}{
		{name: "spec version", change: func(f *validatorIdentityTestFixture) { f.version.SpecVersion++ }},
		{name: "transaction version", change: func(f *validatorIdentityTestFixture) { f.version.TransactionVersion++ }},
		{name: "state version", change: func(f *validatorIdentityTestFixture) { f.version.StateVersion++ }},
		{name: "Wasm", change: func(f *validatorIdentityTestFixture) { f.codeHash = types.Hash{9}.Hex() }},
		{name: "metadata digest", change: func(f *validatorIdentityTestFixture) { f.allowed[0].MetadataHash = types.Hash{9}.Hex() }},
		{name: "metadata bytes", change: func(f *validatorIdentityTestFixture) { f.metadataHex += "00" }},
	} {
		fixture := newValidatorIdentityTestFixture(t)
		example.change(fixture)
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.storageCalls) != 0 {
			t.Errorf("%s: result=%+v storage=%v error=%v", example.name, observed, fixture.storageCalls, err)
		}
	}
}

// The final height/hash check occurs after the last economic read. Returning
// plausible data before a provider switches canonical history is not success.
func TestRuntimeArtifactMetadataValidatorIdentityRechecksCanonicalityBeforePublish(t *testing.T) {
	t.Parallel()
	fixture := newValidatorIdentityTestFixture(t)
	fixture.after = func(method string, args ...any) {
		if method == "state_getStorage" && fixture.keyNames[args[0].(string)] == "ValidatorPermit" {
			fixture.blockHashes[100] = types.Hash{9}
		}
	}
	observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
	if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.storageCalls) != 6 || len(fixture.calls) != 16 {
		t.Fatalf("canonical switch: result=%+v storage=%v calls=%v error=%v", observed, fixture.storageCalls, fixture.calls, err)
	}
}

// A missing historical key must not fall back to a complete dial-time schema.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsMissingHistoricalStorage(t *testing.T) {
	t.Parallel()
	fixture := newValidatorIdentityTestFixture(t)
	currentMetadata, _, err := DecodeRuntimeMetadata(fixture.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	fixture.chain.Meta = currentMetadata
	items := fixture.metadata.AsMetadataV14.Pallets[0].Storage.Items
	fixture.metadata.AsMetadataV14.Pallets[0].Storage.Items = append(items[:3:3], items[4:]...)
	fixture.publishMetadata(t)
	observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
	if err == nil || observed != (ValidatorIdentityObservation{}) ||
		!reflect.DeepEqual(fixture.storageCalls, []string{"SubnetworkN", "Keys", "Uids"}) || fixture.chain.Meta != currentMetadata {
		t.Fatalf("missing historical Owner: result=%+v storage=%v error=%v", observed, fixture.storageCalls, err)
	}
}

// Census decoding is exact-width and bounded before any account-key lookup.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsInvalidCensus(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "missing", raw: json.RawMessage("null")},
		{name: "short", raw: validatorIdentityTestHex([]byte{3})},
		{name: "long", raw: validatorIdentityTestHex([]byte{3, 0, 0})},
		{name: "empty subnet", raw: validatorIdentityTestHex([]byte{0, 0})},
		{name: "over capacity", raw: validatorIdentityTestHex([]byte{1, 1})},
		{name: "UID equals count", raw: validatorIdentityTestHex([]byte{1, 0})},
	} {
		fixture := newValidatorIdentityTestFixture(t)
		fixture.storage["SubnetworkN"] = example.raw
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || observed != (ValidatorIdentityObservation{}) ||
			!reflect.DeepEqual(fixture.storageCalls, []string{"SubnetworkN"}) {
			t.Errorf("%s: result=%+v storage=%v error=%v", example.name, observed, fixture.storageCalls, err)
		}
	}
}

// Forward registration cannot authorize a recycled UID or a missing reverse key.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsReusedUID(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{
		json.RawMessage("null"),
		validatorIdentityTestHex([]byte{0, 0}),
		validatorIdentityTestHex([]byte{2, 0}),
		validatorIdentityTestHex([]byte{1}),
		validatorIdentityTestHex([]byte{1, 0, 0}),
	} {
		fixture := newValidatorIdentityTestFixture(t)
		fixture.storage["Uids"] = raw
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || observed != (ValidatorIdentityObservation{}) ||
			!reflect.DeepEqual(fixture.storageCalls, []string{"SubnetworkN", "Keys", "Uids"}) {
			t.Errorf("reverse registration %s: result=%+v storage=%v error=%v", raw, observed, fixture.storageCalls, err)
		}
	}
}

// Both identity keys require exact AccountId32 bytes, without JSON repair.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsMalformedAccounts(t *testing.T) {
	t.Parallel()
	for _, field := range []struct {
		name  string
		reads int
	}{
		{name: "Keys", reads: 2},
		{name: "Owner", reads: 4},
	} {
		for _, raw := range []json.RawMessage{
			json.RawMessage("null"),
			validatorIdentityTestHex(make([]byte, 31)),
			validatorIdentityTestHex(make([]byte, 33)),
			json.RawMessage(`"0x0"`),
			json.RawMessage(`"0x\u0030\u0031"`),
			json.RawMessage(`"0x01"false`),
			json.RawMessage(`1`),
		} {
			fixture := newValidatorIdentityTestFixture(t)
			fixture.storage[field.name] = raw
			observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
			if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.storageCalls) != field.reads {
				t.Errorf("%s %s: result=%+v storage=%v error=%v", field.name, raw, observed, fixture.storageCalls, err)
			}
		}
	}
}

// Native alpha storage is a full u64, not a float or compact integer. Only its
// ValueQuery null has a zero default; zero remains data, not an eligibility vote.
func TestRuntimeArtifactMetadataValidatorIdentityPreservesExactStake(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name  string
		raw   json.RawMessage
		stake uint64
	}{
		{name: "default zero", raw: json.RawMessage("null"), stake: 0},
		{name: "encoded zero", raw: validatorIdentityTestHex(make([]byte, 8)), stake: 0},
		{name: "above float precision", raw: validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 9_123_456_789_012_345)), stake: 9_123_456_789_012_345},
		{name: "full width", raw: validatorIdentityTestHex(bytes.Repeat([]byte{255}, 8)), stake: math.MaxUint64},
	} {
		fixture := newValidatorIdentityTestFixture(t)
		fixture.storage["TotalHotkeyAlpha"] = example.raw
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err != nil || observed.StakeAlphaRao != example.stake || observed.Hotkey != fixture.hotkey ||
			!observed.ValidatorPermit || len(fixture.calls) != 16 {
			t.Errorf("%s: result=%+v calls=%v error=%v", example.name, observed, fixture.calls, err)
		}
	}
}

// Missing/default stake is different from a present malformed encoding.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsMalformedStake(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{
		validatorIdentityTestHex(nil),
		validatorIdentityTestHex(make([]byte, 7)),
		validatorIdentityTestHex(make([]byte, 9)),
		json.RawMessage(`"null"`),
		json.RawMessage(`0`),
		json.RawMessage(` null`),
	} {
		fixture := newValidatorIdentityTestFixture(t)
		fixture.storage["TotalHotkeyAlpha"] = raw
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.storageCalls) != 5 {
			t.Errorf("stake %s: result=%+v storage=%v error=%v", raw, observed, fixture.storageCalls, err)
		}
	}
}

// A registered non-validator remains an observable identity, not a fabricated
// read error or an eligible-validator verdict.
func TestRuntimeArtifactMetadataValidatorIdentityPreservesFalsePermit(t *testing.T) {
	t.Parallel()
	fixture := newValidatorIdentityTestFixture(t)
	fixture.storage["ValidatorPermit"] = validatorIdentityTestHex([]byte{12, 1, 0, 1})
	observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
	if err != nil || observed.ValidatorPermit || observed.StakeAlphaRao != fixture.stake || observed.Hotkey != fixture.hotkey || len(fixture.calls) != 16 {
		t.Fatalf("non-validator observation=%+v calls=%v error=%v", observed, fixture.calls, err)
	}
}

// Every permit must be a canonical bool; a valid requested UID cannot conceal
// an invalid adjacent UID, noncanonical length, truncation or trailing data.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsMalformedPermits(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{
		json.RawMessage("null"),
		validatorIdentityTestHex(nil),
		validatorIdentityTestHex([]byte{13, 0, 0, 1, 0}),
		validatorIdentityTestHex([]byte{8, 0, 1}),
		validatorIdentityTestHex([]byte{16, 0, 1, 0, 0}),
		validatorIdentityTestHex([]byte{12, 0, 1}),
		validatorIdentityTestHex([]byte{12, 0, 1, 0, 0}),
		validatorIdentityTestHex([]byte{12, 0, 2, 0}),
		validatorIdentityTestHex([]byte{12, 2, 1, 0}),
		validatorIdentityTestHex([]byte{12, 0, 1, 2}),
		validatorIdentityTestHex([]byte{255, 255, 255, 255}),
	} {
		fixture := newValidatorIdentityTestFixture(t)
		fixture.storage["ValidatorPermit"] = raw
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if err == nil || observed != (ValidatorIdentityObservation{}) || len(fixture.storageCalls) != 6 || len(fixture.calls) != 15 {
			t.Errorf("permits %s: result=%+v calls=%v error=%v", raw, observed, fixture.calls, err)
		}
	}
}

// Explicit SCALE prefixes straddle every compact mode used by a u16 census.
// The fixtures never allocate from an untrusted encoded length.
func TestRuntimeArtifactMetadataValidatorIdentityPermitCompactBoundaries(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		count  uint16
		prefix []byte
	}{
		{count: 1, prefix: []byte{4}},
		{count: 63, prefix: []byte{252}},
		{count: 64, prefix: []byte{1, 1}},
		{count: 16383, prefix: []byte{253, 255}},
		{count: 16384, prefix: []byte{2, 0, 1, 0}},
		{count: 65535, prefix: []byte{254, 255, 3, 0}},
	} {
		raw := append(bytes.Clone(example.prefix), make([]byte, int(example.count))...)
		raw[len(raw)-1] = 1
		permit, err := decodeValidatorIdentityPermit(raw, example.count, example.count-1)
		if err != nil || !permit {
			t.Fatalf("count %d: permit=%t error=%v", example.count, permit, err)
		}
		if example.count > 1 {
			permit, err = decodeValidatorIdentityPermit(raw, example.count, 0)
			if err != nil || permit {
				t.Fatalf("first UID at count %d: permit=%t error=%v", example.count, permit, err)
			}
		}
		raw[len(example.prefix)] = 2
		if permit, err := decodeValidatorIdentityPermit(raw, example.count, example.count-1); err == nil || permit {
			t.Fatalf("invalid first bool at count %d: permit=%t error=%v", example.count, permit, err)
		}
	}
	for _, example := range []struct {
		count uint16
		uid   uint16
	}{
		{count: 0, uid: 0},
		{count: 1, uid: 1},
		{count: 1, uid: 65535},
	} {
		if permit, err := decodeValidatorIdentityPermit([]byte{4, 1}, example.count, example.uid); err == nil || permit {
			t.Errorf("invalid census/UID %+v: permit=%t error=%v", example, permit, err)
		}
	}
}

// Raw JSON must fit its caller's explicit bound and contain one unescaped
// even-length hex value. Only exact null gets an explicitly enabled default.
func TestRuntimeArtifactMetadataValidatorIdentityHexRejectsMalformedOrUnbounded(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name     string
		raw      json.RawMessage
		maximum  int
		optional bool
	}{
		{name: "negative bound", raw: json.RawMessage(`"0x01"`), maximum: -1},
		{name: "zero bound", raw: json.RawMessage(`"0x01"`)},
		{name: "excessive bound", raw: json.RawMessage(`"0x01"`), maximum: math.MaxUint16 + 5},
		{name: "null cannot bypass bound", raw: json.RawMessage(`null`), optional: true},
		{name: "required null", raw: json.RawMessage(`null`), maximum: 1},
		{name: "oversize", raw: json.RawMessage(`"0x0102"`), maximum: 1},
		{name: "odd hex", raw: json.RawMessage(`"0x1"`), maximum: 1},
		{name: "JSON escape", raw: json.RawMessage(`"0x\u0030\u0031"`), maximum: 16},
		{name: "invalid hex", raw: json.RawMessage(`"0xgg"`), maximum: 1},
		{name: "wrong prefix", raw: json.RawMessage(`"0X01"`), maximum: 1},
		{name: "trailing JSON", raw: json.RawMessage(`"0x01"null`), maximum: 16},
		{name: "quoted null", raw: json.RawMessage(`"null"`), maximum: 16, optional: true},
		{name: "whitespace null", raw: json.RawMessage(`null `), maximum: 16, optional: true},
		{name: "missing bytes", maximum: 1},
	} {
		decoded, err := decodeValidatorIdentityHexResult(example.raw, example.maximum, example.optional)
		if err == nil || decoded != nil {
			t.Errorf("%s: bytes=%x error=%v", example.name, decoded, err)
		}
	}
}

// Successful hex decoding owns its bytes; mixed-case hex does not change the
// native value and a typed caller, not this generic decoder, checks width.
func TestRuntimeArtifactMetadataValidatorIdentityHexOwnsExactBytes(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`"0xaB01"`)
	decoded, err := decodeValidatorIdentityHexResult(raw, 2, false)
	if err != nil || !bytes.Equal(decoded, []byte{171, 1}) {
		t.Fatalf("hex bytes=%x error=%v", decoded, err)
	}
	raw[3] = '0'
	if !bytes.Equal(decoded, []byte{171, 1}) {
		t.Fatal("decoded bytes alias the transport response")
	}
	decoded, err = decodeValidatorIdentityHexResult(json.RawMessage(`null`), 1, true)
	if err != nil || decoded != nil {
		t.Fatalf("optional null bytes=%x error=%v", decoded, err)
	}
	decoded, err = decodeValidatorIdentityHexResult(json.RawMessage(`"0x"`), 1, false)
	if err != nil || decoded == nil || len(decoded) != 0 {
		t.Fatalf("present empty bytes=%x error=%v", decoded, err)
	}
}

// Each of the sixteen actual RPC positions propagates its causal error and
// discards every partially collected identity, including final-check failure.
func TestRuntimeArtifactMetadataValidatorIdentityPreservesEveryRPCFailure(t *testing.T) {
	t.Parallel()
	for position := 0; position < 16; position++ {
		fixture := newValidatorIdentityTestFixture(t)
		cause := errors.New("injected historical RPC failure")
		fixture.hook = func(context.Context, any, string, ...any) (bool, error) {
			if len(fixture.calls) == position+1 {
				return true, cause
			}
			return false, nil
		}
		observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
		if !errors.Is(err, cause) || observed != (ValidatorIdentityObservation{}) || len(fixture.calls) != position+1 {
			t.Errorf("RPC position %d: result=%+v calls=%v error=%v", position, observed, fixture.calls, err)
		}
	}
}

// Cancellation crosses a real in-flight reader/RPC boundary. Channels force
// the ordering and join the sole writer before the transcript is inspected.
func TestRuntimeArtifactMetadataValidatorIdentityCancelsOutstandingRead(t *testing.T) {
	t.Parallel()
	fixture := newValidatorIdentityTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.ctx = ctx
	started := make(chan struct{})
	done := make(chan struct{})
	fixture.hook = func(ctx context.Context, _ any, method string, args ...any) (bool, error) {
		if method == "state_getStorage" && fixture.keyNames[args[0].(string)] == "Owner" {
			close(started)
			<-ctx.Done()
			return true, ctx.Err()
		}
		return false, nil
	}
	var observed ValidatorIdentityObservation
	var readErr error
	go func() {
		defer close(done)
		observed, readErr = ReadValidatorIdentityAtContext(ctx, fixture.chain, fixture.query, fixture.allowed...)
	}()
	select {
	case <-started:
	case <-done:
		t.Fatalf("reader did not reach owned blocking RPC: %v", readErr)
	}
	cancel()
	<-done
	if !errors.Is(readErr, context.Canceled) || observed != (ValidatorIdentityObservation{}) || len(fixture.storageCalls) != 4 {
		t.Fatalf("canceled read: result=%+v storage=%v error=%v", observed, fixture.storageCalls, readErr)
	}
}

// A transport can return its final value while cancellation wins. Publication
// must check the caller again, even when every RPC returned successfully.
func TestRuntimeArtifactMetadataValidatorIdentityCancelsBeforePublish(t *testing.T) {
	t.Parallel()
	fixture := newValidatorIdentityTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.ctx = ctx
	selectedReads := 0
	fixture.after = func(method string, args ...any) {
		if method == "chain_getBlockHash" && args[0] == fixture.query.BlockNumber {
			selectedReads++
			if selectedReads == 2 {
				cancel()
			}
		}
	}
	observed, err := ReadValidatorIdentityAtContext(ctx, fixture.chain, fixture.query, fixture.allowed...)
	if !errors.Is(err, context.Canceled) || observed != (ValidatorIdentityObservation{}) || selectedReads != 2 || len(fixture.calls) != 16 {
		t.Fatalf("canceled publication: result=%+v calls=%v error=%v", observed, fixture.calls, err)
	}
}

// Already-canceled operations cannot allocate provider work or fill a cache.
func TestRuntimeArtifactMetadataValidatorIdentityRejectsCanceledCallerBeforeRPC(t *testing.T) {
	t.Parallel()
	fixture := newValidatorIdentityTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fixture.ctx = ctx
	observed, err := ReadValidatorIdentityAtContext(ctx, fixture.chain, fixture.query, fixture.allowed...)
	if !errors.Is(err, context.Canceled) || observed != (ValidatorIdentityObservation{}) || len(fixture.calls) != 0 {
		t.Fatalf("already canceled: result=%+v calls=%v error=%v", observed, fixture.calls, err)
	}
}
