// Causal receipt tests use synthetic finalized chains and an isolated HTTP
// server. Independent Rust trie vectors and GSRPC header encoding avoid a
// fixture that merely calls the implementation to invent its expected hashes.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Vectors were produced by sp-trie at the SDK revision locked by the reviewed
// Subtensor source: cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a. Inputs span compact
// index transitions, inline/hash child boundaries and hashed layout1 values.
func TestRootReceiptTrieMatchesPinnedRust(t *testing.T) {
	vectors := []struct {
		count int
		roots [2]string
	}{
		{count: 0, roots: [2]string{"03170a2e7597b7b7e3d84c05391d139a62b157e78786d8c082f29dcf4c111314", "03170a2e7597b7b7e3d84c05391d139a62b157e78786d8c082f29dcf4c111314"}},
		{count: 1, roots: [2]string{"27ca70a13c772e41e0f81ec9353f1b4f6a2b82b08d87a57e97e40036d16d997b", "27ca70a13c772e41e0f81ec9353f1b4f6a2b82b08d87a57e97e40036d16d997b"}},
		{count: 2, roots: [2]string{"04e3df244bca6dc77717cb84a3d91012dfc7f574993ce83ba128be6944d660d3", "04e3df244bca6dc77717cb84a3d91012dfc7f574993ce83ba128be6944d660d3"}},
		{count: 16, roots: [2]string{"075f18142c9494d8104529e0f9bf262a178b5ca0fa9506a774847a2bf5cfd10b", "075f18142c9494d8104529e0f9bf262a178b5ca0fa9506a774847a2bf5cfd10b"}},
		{count: 64, roots: [2]string{"df9b54b4042b261298d443a166fb41ab0f9426c32de85af8ccef370caf9fd4dd", "c0fc8bc715c9e59823411fbd5968f6ef51aa6bba02a480339cef92318f9d747c"}},
		{count: 65, roots: [2]string{"66079b4e40b6bb0f512c86e5e0a78b9bedfc290c0729ea471b01b4c4265ca82d", "159b2efbfeb273fd4a32de73c40cc82a8fe3f368af7e7c85aa3c781d1407efcf"}},
		{count: 256, roots: [2]string{"3a8f3fe205e0a07fe0514d56b5770035fd9218bc22c81b387616c8e6d94e3eee", "ded6205562f8f80a2eca6117d17e2aefeaf416ac0434e2b955e8697838d692d9"}},
		{count: 1024, roots: [2]string{"10763206d959aeb23bbc641f6bafca30148765ab835dae8102b77a1c2e33ed93", "4555cb3d445d8e5275c82c04b988d31538eed5a6e431b0d3b1d52ec3f21f4899"}},
	}
	for _, vector := range vectors {
		values := make([][]byte, vector.count)
		for index := range values {
			values[index] = make([]byte, index%67+1)
			for column := range values[index] {
				values[index][column] = byte(index*17 + column*31)
			}
		}
		for layout, expected := range vector.roots {
			actual, err := rootExtrinsicsRoot(values, uint8(layout))
			if err != nil || actual != "0x"+expected {
				t.Fatalf("Rust vector %d layout%d: %s %v", vector.count, layout, actual, err)
			}
		}
	}
}

// Pure profile tests exercise exact public metadata without synthetic identifier
// changes. The Rust wrapper's metadata() delegates to the inner payment type.
func TestRootReceiptRealPaymentMetadataAndNativeAccount(t *testing.T) {
	metadata, _, _ := rootTestMetadata(t)
	if _, err := rootSigningProfile(metadata); err != nil {
		t.Fatal(err)
	}
	if err := rootAccountProfile(metadata); err != nil {
		t.Fatal(err)
	}
	if _, err := rootReceiptEvents(metadata); err != nil {
		t.Fatal(err)
	}
	metadata.AsMetadataV14.Extrinsic.SignedExtensions[7].Identifier = "ChargeTransactionPaymentWrapper"
	if _, err := rootSigningProfile(metadata); err == nil {
		t.Fatal("fictional wrapper metadata identifier admitted")
	}
}

// Event fields not pertinent to an outcome receive a zero value according to
// the independent metadata fixture, without calling the production traversal.
func rootReceiptZero(t *testing.T, metadata *types.Metadata, id types.Si1LookupTypeID) []byte {
	t.Helper()
	def := metadata.AsMetadataV14.EfficientLookup[id.Int64()].Def
	var raw []byte
	switch {
	case def.IsComposite:
		for _, field := range def.Composite.Fields {
			raw = append(raw, rootReceiptZero(t, metadata, field.Type)...)
		}
	case def.IsTuple:
		for _, item := range def.Tuple {
			raw = append(raw, rootReceiptZero(t, metadata, item)...)
		}
	case def.IsVariant:
		variant := def.Variant.Variants[0]
		raw = append(raw, byte(variant.Index))
		for _, field := range variant.Fields {
			raw = append(raw, rootReceiptZero(t, metadata, field.Type)...)
		}
	case def.IsCompact, def.IsSequence:
		raw = []byte{0}
	case def.IsArray:
		for index := uint32(0); index < uint32(def.Array.Len); index++ {
			raw = append(raw, rootReceiptZero(t, metadata, def.Array.Type)...)
		}
	case def.IsPrimitive:
		width := map[types.Si0TypeDefPrimitive]int{types.IsBool: 1, types.IsU8: 1, types.IsU16: 2, types.IsU32: 4, types.IsU64: 8, types.IsU128: 16}[def.Primitive.Si0TypeDefPrimitive]
		if width == 0 {
			t.Fatalf("fixture primitive %d unsupported", def.Primitive.Si0TypeDefPrimitive)
		}
		raw = make([]byte, width)
	default:
		t.Fatal("fixture SCALE zero type unsupported")
	}
	return raw
}

// Construct one exact event record directly from the public pallet registry.
func rootReceiptEventFixture(t *testing.T, metadata *types.Metadata, name string, index uint32, fields ...[]byte) []byte {
	t.Helper()
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if !pallet.HasEvents {
			continue
		}
		for _, variant := range metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()].Def.Variant.Variants {
			if string(pallet.Name)+"."+string(variant.Name) != name {
				continue
			}
			raw := binary.LittleEndian.AppendUint32([]byte{0}, index)
			raw = append(raw, byte(pallet.Index), byte(variant.Index))
			for fieldIndex, field := range variant.Fields {
				value := rootReceiptZero(t, metadata, field.Type)
				if fieldIndex < len(fields) && fields[fieldIndex] != nil {
					value = fields[fieldIndex]
				}
				raw = append(raw, value...)
			}
			return append(raw, 0) // no topics
		}
	}
	t.Fatalf("fixture event %s missing", name)
	return nil
}

// Counters and optional read faults are synchronized even though the adapter
// itself serializes requests. All configured chain data is frozen before use.
type rootReceiptFixture struct {
	metadata    *types.Metadata
	metadataHex string
	action      rootAction
	signed      []byte
	profile     rootReceiptProfile
	headers     map[string]rootReceiptHeader
	byHeight    map[uint64]string
	bodies      map[string][]string
	storageKVs  map[string]string
	finalized   string
	evmHex      string
	runtimeKVs  map[string]rootReceiptProfile
	metadataKVs map[string]string
	stateLock   sync.Mutex
	counts      map[string]int
	fault       func(string, []json.RawMessage, int) (any, bool)
}

// Header expectations use the independent GSRPC SCALE encoder for all ordinary
// fields. The modern SDK unit digest8 is appended explicitly for upgrade tests.
func rootReceiptHeaderFixture(t *testing.T, parent string, number uint64, body [][]byte, upgrade bool) (rootReceiptHeader, string) {
	t.Helper()
	root, err := rootExtrinsicsRoot(body, 0)
	if err != nil {
		t.Fatal(err)
	}
	header := rootReceiptHeader{ParentHash: parent, Number: fmt.Sprintf("0x%x", number), StateRoot: "0x" + strings.Repeat("ab", 32), ExtrinsicsRoot: root}
	header.Digest.Logs = []string{}
	parentHash, _ := types.NewHashFromHexString(parent)
	stateHash, _ := types.NewHashFromHexString(header.StateRoot)
	bodyHash, _ := types.NewHashFromHexString(root)
	raw, err := codec.Encode(types.Header{ParentHash: parentHash, Number: types.BlockNumber(number), StateRoot: stateHash, ExtrinsicsRoot: bodyHash, Digest: types.Digest{}})
	if err != nil {
		t.Fatal(err)
	}
	if upgrade {
		header.Digest.Logs = []string{"0x08"}
		raw = append(raw[:len(raw)-1], 4, 8)
	}
	return header, rootExtrinsicHash(raw)
}

// Synthetic signatures and state belong only to this local disposable fixture.
func newRootReceiptFixture(t *testing.T, count int, included bool) (*rootCanonicalChain, *rootReceiptFixture) {
	t.Helper()
	action, pair, metadataHex := rootActionFixture(t)
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	profile := rootReceiptProfile{RuntimeSourceCommit: rootProfileSource, RuntimeVersion: action.Scope.RuntimeVersion, RuntimeCodeHash: action.Scope.RuntimeCodeHash, RuntimeMetadataHash: action.Scope.RuntimeMetadataHash}
	fixture := &rootReceiptFixture{metadata: metadata, metadataHex: metadataHex, action: action, profile: profile, headers: map[string]rootReceiptHeader{}, byHeight: map[uint64]string{}, bodies: map[string][]string{}, storageKVs: map[string]string{}, evmHex: "0x3c4", runtimeKVs: map[string]rootReceiptProfile{}, metadataKVs: map[string]string{}, counts: map[string]int{}}
	anchor, hash := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("bc", 32), action.BirthBlock, nil, false)
	fixture.headers[hash], fixture.byHeight[action.BirthBlock] = anchor, hash
	action.BirthHash = hash
	action, err = prepareRootAction(action, metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := hex.DecodeString(action.Payload[2:])
	signature, err := pair.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	fixture.action = action
	fixture.signed, err = action.signed(signature)
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= count; index++ {
		var body [][]byte
		if index == 1 && included {
			body = [][]byte{{8, 4, 0}, fixture.signed}
		}
		header, nextHash := rootReceiptHeaderFixture(t, hash, action.BirthBlock+uint64(index), body, index == 1)
		fixture.headers[nextHash], fixture.byHeight[action.BirthBlock+uint64(index)] = header, nextHash
		fixture.bodies[nextHash] = []string{}
		for _, raw := range body {
			fixture.bodies[nextHash] = append(fixture.bodies[nextHash], "0x"+hex.EncodeToString(raw))
		}
		hash = nextHash
	}
	fixture.finalized = hash
	hotkey, _ := hex.DecodeString(action.Scope.Hotkey[2:])
	set := func(pallet, name string, value []byte, args ...[]byte) {
		key, err := types.CreateStorageKey(metadata, pallet, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		fixture.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(value)
	}
	account := make([]byte, 56)
	nonce := action.Nonce
	if included {
		nonce++
	}
	binary.LittleEndian.PutUint32(account, nonce)
	set("System", "Account", account, hotkey)
	uid := binary.LittleEndian.AppendUint16(nil, action.Scope.Seat.Uid)
	set("SubtensorModule", "Uids", uid, []byte{0, 0}, hotkey)
	set("SubtensorModule", "Keys", hotkey, []byte{0, 0}, uid)
	coldkey, _ := hex.DecodeString(action.Scope.Coldkey[2:])
	set("SubtensorModule", "Owner", coldkey, hotkey)
	set("SubtensorModule", "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, action.Scope.Seat.RegistrationBlock), []byte{0, 0}, uid)
	events := []byte{12}
	events = append(events, rootReceiptEventFixture(t, metadata, "SubtensorModule.RootWeightsSet", 1, uid)...)
	events = append(events, rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 1, hotkey, binary.LittleEndian.AppendUint64(nil, 12), make([]byte, 8))...)
	events = append(events, rootReceiptEventFixture(t, metadata, "System.ExtrinsicSuccess", 1)...)
	set("System", "Events", events)
	weights := []byte{4}
	weights = binary.LittleEndian.AppendUint16(weights, 1)
	weights = binary.LittleEndian.AppendUint16(weights, 65535)
	set("SubtensorModule", "Weights", weights, []byte{0, 0}, uid)
	lastUpdates := rootCompact(uint64(action.Scope.Seat.Uid) + 1)
	for index := uint16(0); index <= action.Scope.Seat.Uid; index++ {
		lastUpdates = binary.LittleEndian.AppendUint64(lastUpdates, action.BirthBlock+1)
	}
	set("SubtensorModule", "LastUpdate", lastUpdates, []byte{0, 0})
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := newRootCanonicalChain(client, identityExpectation{NativeChain: action.Scope.NativeChain, GenesisHash: action.Scope.GenesisHash, EvmChainId: mainnetEvmChainId}, []rootReceiptProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	return chain, fixture
}

// Faults can replace one read response. No fixture accepts mutation methods.
func (self *rootReceiptFixture) serve(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	var call struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	data, _ := io.ReadAll(io.LimitReader(request.Body, 1024*1024))
	if err := json.Unmarshal(data, &call); err != nil {
		http.Error(writer, err.Error(), 400)
		return
	}
	self.counts[call.Method]++
	respond := func(result any) {
		json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}
	if self.fault != nil {
		if result, used := self.fault(call.Method, call.Params, self.counts[call.Method]); used {
			respond(result)
			return
		}
	}
	arg := func(index int) string {
		var value string
		if index < len(call.Params) {
			json.Unmarshal(call.Params[index], &value)
		}
		return value
	}
	switch call.Method {
	case "system_chain":
		respond(self.action.Scope.NativeChain)
	case "eth_chainId":
		respond(self.evmHex)
	case "chain_getBlockHash":
		var height uint64
		json.Unmarshal(call.Params[0], &height)
		if height == 0 {
			respond(self.action.Scope.GenesisHash)
		} else {
			respond(self.byHeight[height])
		}
	case "chain_getFinalizedHead":
		respond(self.finalized)
	case "chain_getHeader":
		respond(self.headers[arg(0)])
	case "chain_getBlock":
		respond(map[string]any{"block": map[string]any{"header": self.headers[arg(0)], "extrinsics": self.bodies[arg(0)]}})
	case "state_getRuntimeVersion", "state_getStorageHash", "state_getMetadata":
		block := arg(0)
		if call.Method == "state_getStorageHash" {
			block = arg(1)
		}
		profile, exists := self.runtimeKVs[block]
		if !exists {
			profile = self.profile
		}
		switch call.Method {
		case "state_getRuntimeVersion":
			respond(profile.RuntimeVersion)
		case "state_getStorageHash":
			respond(profile.RuntimeCodeHash)
		case "state_getMetadata":
			encoded, exists := self.metadataKVs[block]
			if !exists {
				encoded = self.metadataHex
			}
			respond(encoded)
		}
	case "state_getStorage":
		if value, exists := self.storageKVs[arg(0)]; exists {
			respond(value)
		} else {
			respond(nil)
		}
	default:
		json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32601, "message": "fixture refuses unknown or mutation method"}})
	}
}

// The happy path checks real signed bytes, full ancestry, phase index and fee;
// its header carries the upgrade digest missing from the old library codec.
func TestRootReceiptCanonicalInclusion(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 2, true)
	result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt == nil || !result.Receipt.Success || result.Receipt.ExtrinsicIndex != 1 || result.Receipt.ActualFeeRao != 12 || result.CheckedThrough != fixture.action.BirthBlock+2 || result.Observation.Seat != fixture.action.Scope.Seat {
		t.Fatalf("receipt/state differs: %+v", result)
	}
	if result.Receipt.PostState == nil || result.Receipt.PostState.Issue != "" || result.Receipt.PostState.LastUpdate != fixture.action.BirthBlock+1 {
		t.Fatalf("root post-state missing: %+v", result.Receipt.PostState)
	}
	if err := chain.submit(context.Background(), fixture.signed); err != errRootSubmissionUnavailable {
		t.Fatalf("submission unexpectedly enabled: %v", err)
	}
	if fixture.counts["author_submitExtrinsic"] != 0 {
		t.Fatal("read adapter performed a write")
	}
}

// Expiry requires every body, including the last mortal block; an explicit
// missing body or wrong root cannot be turned into an empty successful scan.
func TestRootReceiptCompleteMortalExpiry(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 64, false)
	result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil || rootTerminalPhase(fixture.action, result) != "expired" || fixture.counts["chain_getBlock"] != 63 {
		t.Fatalf("complete absence failed: %+v %v", result, err)
	}
	delete(fixture.bodies, fixture.byHeight[fixture.action.BirthBlock+63])
	if _, err := chain.reconcile(context.Background(), fixture.action, fixture.signed); err == nil {
		t.Fatal("missing last body became absence")
	}
}

// Current state is not required to interpret an older exact receipt. An
// unapproved upgrade blocks new effects and expiry instead of losing that fee.
func TestRootReceiptUpgradeUsesParentAndRetainsUnapprovedHead(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 3, true)
	upgraded := fixture.profile
	upgraded.RuntimeVersion.SpecVersion++
	upgraded.RuntimeCodeHash = "0x" + strings.Repeat("cd", 32)
	for height := fixture.action.BirthBlock + 1; height <= fixture.action.BirthBlock+3; height++ {
		fixture.runtimeKVs[fixture.byHeight[height]] = upgraded
	}
	result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil || result.Receipt == nil || result.Receipt.ExecutionRuntimeVersion != fixture.profile.RuntimeVersion || !result.Observation.StateUnavailable || result.Observation.RuntimeVersion != upgraded.RuntimeVersion {
		t.Fatalf("upgrade lost parent receipt: %+v %v", result, err)
	}
	if err := result.Observation.matches(fixture.action, true); err == nil {
		t.Fatal("unapproved current state admitted signing")
	}
	if rootTerminalPhase(fixture.action, result) != "finalized" {
		t.Fatal("old-runtime receipt misclassified as deviation")
	}
	result.Receipt = nil
	result.Observation.FinalizedNumber = fixture.action.BirthBlock + fixture.action.Period
	result.Observation.AccountNonce = fixture.action.Nonce
	if rootTerminalPhase(fixture.action, result) != "" {
		t.Fatal("unqualified nonce permitted expiry")
	}
}

// A later reaped account can reset a current nonce; it cannot erase a proven
// older inclusion. Only finalized absence still depends on a qualified nonce.
func TestRootReceiptHistoricalReceiptSurvivesReapedAccount(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 2, true)
	hotkey, _ := hex.DecodeString(fixture.action.Scope.Hotkey[2:])
	key, _ := types.CreateStorageKey(fixture.metadata, "System", "Account", hotkey)
	delete(fixture.storageKVs, key.Hex())
	result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil || result.Receipt == nil || result.Observation.AccountNonce != 0 {
		t.Fatalf("known receipt erased by later nonce: %+v %v", result, err)
	}
}

// Every corrupt response fails before successful coverage is returned. The
// cases isolate announced hash, parent continuity, body commitment and route.
func TestRootReceiptRejectsCorruptChainEvidence(t *testing.T) {
	for _, change := range []func(*rootReceiptFixture){
		func(f *rootReceiptFixture) { f.evmHex = "0x3b1" },
		func(f *rootReceiptFixture) {
			h := f.headers[f.finalized]
			h.StateRoot = "0x" + strings.Repeat("ee", 32)
			f.headers[f.finalized] = h
		},
		func(f *rootReceiptFixture) {
			h := f.headers[f.finalized]
			h.Digest.Logs = nil
			f.headers[f.finalized] = h
		},
		func(f *rootReceiptFixture) { f.bodies[f.byHeight[f.action.BirthBlock+1]][0] = "0x080401" },
		func(f *rootReceiptFixture) { f.bodies[f.byHeight[f.action.BirthBlock+1]] = []string{} },
		func(f *rootReceiptFixture) { f.byHeight[f.action.BirthBlock+2] = "0x" + strings.Repeat("ee", 32) },
		func(f *rootReceiptFixture) { delete(f.headers, f.action.BirthHash) },
	} {
		chain, fixture := newRootReceiptFixture(t, 2, true)
		change(fixture)
		result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
		if err == nil || result.Receipt != nil {
			t.Fatalf("corrupt chain became receipt: %+v %v", result, err)
		}
	}
}

// Dispatch failure is a real result with its exact SCALE error and charged fee.
func TestRootReceiptDispatchFailure(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 1, true)
	hotkey, _ := hex.DecodeString(fixture.action.Scope.Hotkey[2:])
	raw := []byte{8}
	raw = append(raw, rootReceiptEventFixture(t, fixture.metadata, "TransactionPayment.TransactionFeePaid", 1, hotkey, binary.LittleEndian.AppendUint64(nil, 31), make([]byte, 8))...)
	raw = append(raw, rootReceiptEventFixture(t, fixture.metadata, "System.ExtrinsicFailed", 1)...)
	key, _ := types.CreateStorageKey(fixture.metadata, "System", "Events")
	fixture.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(raw)
	result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil || result.Receipt == nil || result.Receipt.Success || result.Receipt.DispatchError == "" || result.Receipt.ActualFeeRao != 31 || rootTerminalPhase(fixture.action, result) != "dispatch-failed" {
		t.Fatalf("dispatch failure lost: %+v %v", result, err)
	}
}

// Corrupt event bundles exercise phase isolation, fee presence/payer/tip,
// duplicate terminal events and bounded lengths rather than checker-only mocks.
func TestRootReceiptRejectsAmbiguousEvents(t *testing.T) {
	action, _, metadataHex := rootActionFixture(t)
	metadata, _, _ := crv4.DecodeRuntimeMetadata(metadataHex)
	hotkey, _ := hex.DecodeString(action.Scope.Hotkey[2:])
	uid := binary.LittleEndian.AppendUint16(nil, action.Scope.Seat.Uid)
	weight := rootReceiptEventFixture(t, metadata, "SubtensorModule.RootWeightsSet", 1, uid)
	fee := rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 1, hotkey, make([]byte, 8), make([]byte, 8))
	success := rootReceiptEventFixture(t, metadata, "System.ExtrinsicSuccess", 1)
	bundle := append(append(append([]byte{12}, weight...), fee...), success...)
	wrongPhase := append([]byte(nil), bundle...)
	binary.LittleEndian.PutUint32(wrongPhase[2:6], 0)
	cases := [][]byte{
		append(append([]byte{8}, weight...), success...),
		append(append(append(append([]byte{16}, weight...), fee...), success...), success...),
		append(append([]byte(nil), bundle...), 0),
		wrongPhase,
		append(rootCompact(1<<32), bundle[1:]...),
		bundle[:len(bundle)-1],
	}
	for _, raw := range cases {
		if _, err := rootDecodeReceiptEvents(metadata, raw, 1, 2, action); err == nil {
			t.Fatal("ambiguous event bundle admitted")
		}
	}
	for _, feeVariant := range [][]byte{
		rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 1, bytes.Repeat([]byte{1}, 32), make([]byte, 8), make([]byte, 8)),
		rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 1, hotkey, make([]byte, 8), binary.LittleEndian.AppendUint64(nil, 1)),
		rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 0, hotkey, make([]byte, 8), make([]byte, 8)),
	} {
		raw := append(append(append([]byte{12}, weight...), feeVariant...), success...)
		if _, err := rootDecodeReceiptEvents(metadata, raw, 1, 2, action); err == nil {
			t.Fatal("wrong payer/tip/phase admitted")
		}
	}
}

// Malformed digest lengths are checked before any allocation; digest8 has no
// payload and cannot silently accept trailing data or a legacy unknown tag.
func TestRootReceiptDigestAndCompactBounds(t *testing.T) {
	for _, raw := range [][]byte{{8}, {0, 0}, {4, 1, 2, 3, 4, 4, 5}, {5, 1, 2, 3, 4, 0}, {6, 1, 2, 3, 4, 0}} {
		if err := rootReceiptDigest(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range [][]byte{{}, {8, 0}, {2}, {7}, {0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, {4, 1, 2, 3, 4, 1, 0}} {
		if err := rootReceiptDigest(raw); err == nil {
			t.Fatalf("bad digest accepted: %x", raw)
		}
	}
	reader := rootScaleReader{data: []byte{1, 0}}
	if _, err := reader.compact(); err == nil {
		t.Fatal("noncanonical compact zero admitted")
	}
}

// Explicit RPC timeouts retry within the same read budget. Archive pruning and
// mixed result/error replies fail once; writes are refused before HTTP delivery.
func TestRootReceiptRpcTimeoutAndReadOnlyBoundary(t *testing.T) {
	var lock sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		calls++
		if calls == 1 {
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"Request timeout"}}`)
			return
		}
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"ok"}`)
	}))
	defer server.Close()
	client, _ := newRpcClient(server.URL, 60*time.Second)
	var result string
	if err := client.call(context.Background(), "system_chain", []any{}, &result); err != nil || result != "ok" {
		t.Fatalf("ephemeral timeout failed: %q %v", result, err)
	}
	if err := client.call(context.Background(), "author_submitExtrinsic", []any{"0x00"}, &result); err == nil {
		t.Fatal("read port admitted mutation")
	}
	lock.Lock()
	observed := calls
	lock.Unlock()
	if observed != 2 {
		t.Fatalf("read retry/mutation count %d", observed)
	}
	for _, message := range []string{"State already discarded", "unknown method", "timeout while validating invalid proof"} {
		if rpcTransientReadError(-32000, message) {
			t.Fatal("permanent/unclassified error retried")
		}
	}
}

// Caller cancellation interrupts the archive operation and never produces an
// empty range, even after some successful headers have already been checked.
func TestRootReceiptCancellationPreservesUnresolvedAction(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 2, true)
	ctx, cancel := context.WithCancel(context.Background())
	fixture.fault = func(method string, _ []json.RawMessage, _ int) (any, bool) {
		if method == "chain_getBlock" {
			cancel()
		}
		return nil, false
	}
	result, err := chain.reconcile(ctx, fixture.action, fixture.signed)
	if err == nil || result.Receipt != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled archive became coverage: %+v %v", result, err)
	}
}

// Unknown execution bytes cannot be parsed under the action's old metadata,
// even when the spec/transaction versions were dishonestly left unchanged.
func TestRootReceiptRuntimeCodeDeviationAndUnknownExecution(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 1, true)
	deviating := fixture.profile
	deviating.RuntimeCodeHash = "0x" + strings.Repeat("de", 32)
	fixture.runtimeKVs[fixture.action.BirthHash] = deviating
	if _, err := chain.reconcile(context.Background(), fixture.action, fixture.signed); err != errRootReceiptProfileUnavailable {
		t.Fatalf("unapproved execution interpreted: %v", err)
	}
	chain, err := newRootCanonicalChain(chain.client, chain.expected, []rootReceiptProfile{fixture.profile, deviating})
	if err != nil {
		t.Fatal(err)
	}
	result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil || result.Receipt == nil || rootTerminalPhase(fixture.action, result) != "runtime-deviation" || result.Receipt.ActualFeeRao != 12 {
		t.Fatalf("qualified deviation discarded fee: %+v %v", result, err)
	}
}

// Byte matching is not enough: the adapter verifies that the signature really
// binds this retained action before issuing even its first observation RPC.
func TestRootReceiptRejectsForeignSignatureBeforeRpc(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 1, true)
	bad := append([]byte(nil), fixture.signed...)
	bad[len(bad)-1] ^= 1
	if _, err := chain.reconcile(context.Background(), fixture.action, bad); err == nil {
		t.Fatal("foreign signed bytes admitted")
	}
	if len(fixture.counts) != 0 {
		t.Fatal("invalid signature reached RPC")
	}
}

// A complete, independently hashed body may still contradict nonce uniqueness.
// The adapter scans past the first match and refuses duplicate exact inclusion.
func TestRootReceiptRejectsDuplicateInclusion(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 1, true)
	body := [][]byte{{8, 4, 0}, fixture.signed, fixture.signed}
	header, hash := rootReceiptHeaderFixture(t, fixture.action.BirthHash, fixture.action.BirthBlock+1, body, false)
	fixture.headers[hash], fixture.byHeight[fixture.action.BirthBlock+1], fixture.finalized = header, hash, hash
	fixture.bodies[hash] = []string{"0x080400", "0x" + hex.EncodeToString(fixture.signed), "0x" + hex.EncodeToString(fixture.signed)}
	if _, err := chain.reconcile(context.Background(), fixture.action, fixture.signed); err == nil {
		t.Fatal("duplicate exact action included twice")
	}
}

// Header hashes alone cannot turn a number gap or a different finalized anchor
// into continuity. Both bad chains are internally hash-consistent fixtures.
func TestRootReceiptRejectsHashedAncestryGapAndOldRecovery(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 1, true)
	header, hash := rootReceiptHeaderFixture(t, fixture.action.BirthHash, fixture.action.BirthBlock+2, nil, false)
	fixture.headers[hash], fixture.finalized = header, hash
	if _, err := chain.reconcile(context.Background(), fixture.action, fixture.signed); err == nil {
		t.Fatal("hash-consistent ancestry height gap accepted")
	}
	header, hash = rootReceiptHeaderFixture(t, fixture.action.BirthHash, fixture.action.BirthBlock+rootAncestryLimit+1, nil, false)
	fixture.headers[hash], fixture.finalized = header, hash
	if _, err := chain.reconcile(context.Background(), fixture.action, fixture.signed); err == nil {
		t.Fatal("unbounded historical recovery started")
	}
}

// Failed metadata authentication is never cached. Reopening the read adapter
// after a lost/canceled attempt rechecks full history and returns the same fee.
func TestRootReceiptRetryAndRestartKeepExactHistory(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 2, true)
	fixture.fault = func(method string, _ []json.RawMessage, count int) (any, bool) {
		if method == "state_getMetadata" && count == 1 {
			return "0x00", true
		}
		return nil, false
	}
	if _, err := chain.reconcile(context.Background(), fixture.action, fixture.signed); err == nil {
		t.Fatal("bad metadata admitted")
	}
	if len(chain.runtimeKVs) != 0 {
		t.Fatal("failed runtime metadata was cached")
	}
	result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := newRootCanonicalChain(chain.client, chain.expected, chain.profiles)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil || rootObjectHash(result) != rootObjectHash(recovered) {
		t.Fatalf("restart changed canonical evidence: %+v %v", recovered, err)
	}
}

// The port's admission rejects missing/ambiguous runtime authority and short
// retry budgets; a seat observation or testnet identity cannot enable it.
func TestRootReceiptConstructionAuthorityBounds(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 1, true)
	testnet := chain.expected
	testnet.EvmChainId = 945
	if _, err := newRootCanonicalChain(chain.client, testnet, chain.profiles); err == nil {
		t.Fatal("testnet admitted")
	}
	if _, err := newRootCanonicalChain(chain.client, chain.expected, []rootReceiptProfile{fixture.profile, fixture.profile}); err == nil {
		t.Fatal("ambiguous profile admitted")
	}
	unapproved := fixture.profile
	unapproved.RuntimeSourceCommit = strings.Repeat("0", 40)
	if _, err := newRootCanonicalChain(chain.client, chain.expected, []rootReceiptProfile{unapproved}); err == nil {
		t.Fatal("unknown source profile admitted")
	}
	short := *chain.client
	short.retryWindow = 30 * time.Second
	if _, err := newRootCanonicalChain(&short, chain.expected, chain.profiles); err == nil {
		t.Fatal("short retry budget admitted")
	}
}

// Queue cancellation must not wait for another action's whole archive budget.
func TestRootReceiptWaitingReconciliationCancels(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 1, true)
	chain.reconcileCh <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := chain.reconcile(ctx, fixture.action, fixture.signed)
	<-chain.reconcileCh
	if !errors.Is(err, context.Canceled) || len(fixture.counts) != 0 {
		t.Fatalf("canceled queue issued reads: %v", err)
	}
}

// A post-state gap is disclosed without erasing confirmed dispatch/fee. It may
// not be presented as matched root weights or as permission for a replacement.
func TestRootReceiptPostStateGapKeepsFinancialReceipt(t *testing.T) {
	chain, fixture := newRootReceiptFixture(t, 1, true)
	key, _ := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "LastUpdate", []byte{0, 0})
	delete(fixture.storageKVs, key.Hex())
	result, err := chain.reconcile(context.Background(), fixture.action, fixture.signed)
	if err != nil || result.Receipt == nil || result.Receipt.ActualFeeRao != 12 || result.Receipt.PostState == nil || result.Receipt.PostState.Issue == "" || result.Receipt.PostState.WeightsScale != "" {
		t.Fatalf("post-state gap erased or overclaimed receipt: %+v %v", result, err)
	}
}

// Equal byte counts do not authorize a different account/fee interpretation.
// Profile admission authenticates names, order and nested balance widths first.
func TestRootReceiptRejectsAdjacentAccountAndFeeWidthDrift(t *testing.T) {
	metadata, _, _ := rootTestMetadata(t)
	accountEntry, err := rootSystemEntry(metadata, "Account")
	if err != nil {
		t.Fatal(err)
	}
	account := metadata.AsMetadataV14.EfficientLookup[accountEntry.Type.AsMap.Value.Int64()]
	account.Def.Composite.Fields[0].Name = "not_nonce"
	if err := rootAccountProfile(metadata); err == nil {
		t.Fatal("renamed nonce layout admitted")
	}
	metadata, _, _ = rootTestMetadata(t)
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "TransactionPayment" {
			continue
		}
		variants := metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()].Def.Variant.Variants
		for index := range variants {
			if variants[index].Name != "TransactionFeePaid" {
				continue
			}
			// AccountId32 has a different width from actual_fee's native u64.
			variants[index].Fields[1].Type = variants[index].Fields[0].Type
		}
	}
	if _, err := rootReceiptEvents(metadata); err == nil {
		t.Fatal("wrong native fee width admitted")
	}
}

// A local transport observes the configured deadline without sleeping or using
// a wall-clock timeout as the test outcome. Thirty seconds of scheduler slack
// separates the corrected 60-second attempt from the old 15-second cutoff.
type rootReceiptDeadlineTransport struct{ remaining time.Duration }

func (self *rootReceiptDeadlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	deadline, _ := request.Context().Deadline()
	self.remaining = time.Until(deadline)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"ok"}`)), Header: http.Header{}}, nil
}

// A generous total budget is ineffective when every individual attempt is cut
// off before a normally slow archive read has a chance to return successfully.
func TestRootReceiptSlowReadGetsUsableAttemptBudget(t *testing.T) {
	client, err := newRpcClient("http://receipt-budget.example", 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	transport := &rootReceiptDeadlineTransport{}
	client.httpClient.Transport = transport
	var result string
	if err := client.call(context.Background(), "system_chain", []any{}, &result); err != nil {
		t.Fatal(err)
	}
	if transport.remaining <= 30*time.Second || transport.remaining > 60*time.Second {
		t.Fatalf("read attempt remains too short or unbounded: %s", transport.remaining)
	}
}
