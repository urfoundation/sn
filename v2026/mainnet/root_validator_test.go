// Synthetic state and retained public metadata exercise the root observation
// boundary. Tests have no signing keys, live identities or chain endpoints.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Loads exact public runtime bytes already retained by the repository.
func rootTestMetadata(t *testing.T) (*types.Metadata, string, string) {
	t.Helper()
	encoded, err := os.ReadFile("../miner/testdata/runtime455-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxMetadataRpcReplyBytes))
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	metadataHex := "0x" + hex.EncodeToString(raw)
	metadata, hash, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	return metadata, metadataHex, hash
}

// Configuration is immutable after observation begins; only counters are shared.
type rootRpcFixture struct {
	metadata         *types.Metadata
	metadataHex      string
	policy           rootValidatorPolicy
	storageKVs       map[string]string
	evmChainHex      string
	version          crv4.RuntimeVersionIdentity
	stateLock        sync.Mutex
	methodCounts     map[string]int
	forkAfterStorage bool
	duplicatePage    bool
	omitStorage      bool
}

// Writes visibly synthetic storage through the same authenticated key builder.
func (self *rootRpcFixture) set(t *testing.T, name string, data []byte, args ...[]byte) string {
	t.Helper()
	key, err := types.CreateStorageKey(self.metadata, "SubtensorModule", name, args...)
	if err != nil {
		t.Fatal(err)
	}
	self.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(data)
	return key.Hex()
}

// Builds two fully reconciled seats with different stake and registration age.
func newRootFixture(t *testing.T) (*rpcClient, *rootRpcFixture) {
	t.Helper()
	metadata, metadataHex, metadataHash := rootTestMetadata(t)
	take := uint16(1234)
	policy := rootValidatorPolicy{
		Schema: rootPolicySchema, Role: "bittensor-root-validator", Netuid: 0, NativeChain: "synthetic-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964,
		StorageProfile: rootStorageProfileName, RuntimeSourceCommit: rootProfileSource,
		RuntimeVersion:  crv4.RuntimeVersionIdentity{SpecName: "synthetic-runtime", SpecVersion: 601, TransactionVersion: 2, StateVersion: 1},
		RuntimeCodeHash: "0x" + strings.Repeat("c", 64), RuntimeMetadataHash: metadataHash,
		Hotkey: "0x" + strings.Repeat("11", 32), Coldkey: "0x" + strings.Repeat("22", 32),
		ExpectedSeat: &rootSeatExpectation{Uid: 0, RegistrationBlock: 40}, MinimumStakeRao: "80", ExpectedDelegateTake: &take,
		BasketStrategy: "accumulate_in_place", DelegationStrategy: "none",
	}
	fixture := &rootRpcFixture{metadata: metadata, metadataHex: metadataHex, policy: policy, storageKVs: map[string]string{}, evmChainHex: "0x3c4", version: policy.RuntimeVersion, methodCounts: map[string]int{}}
	rootArg := []byte{0, 0}
	fixture.set(t, "NetworksAdded", []byte{1}, rootArg)
	fixture.set(t, "NetworksAdded", []byte{1}, []byte{25, 0})
	fixture.set(t, "SubnetworkN", []byte{2, 0}, rootArg)
	fixture.set(t, "MaxAllowedUids", []byte{2, 0}, rootArg)
	fixture.set(t, "ImmunityPeriod", []byte{10, 0}, rootArg)
	for index := 0; index < 2; index++ {
		uidArg := binary.LittleEndian.AppendUint16(nil, uint16(index))
		hotkey := bytes.Repeat([]byte{byte(0x11 + index)}, 32)
		coldkey := bytes.Repeat([]byte{byte(0x22 + index)}, 32)
		fixture.set(t, "Keys", hotkey, rootArg, uidArg)
		fixture.set(t, "Uids", uidArg, rootArg, hotkey)
		fixture.set(t, "Owner", coldkey, hotkey)
		fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, uint64(40+index)), rootArg, uidArg)
		fixture.set(t, "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, uint64(100-index*50)), hotkey, rootArg)
	}
	hotkey, _ := hex.DecodeString(policy.Hotkey[2:])
	fixture.set(t, "Delegates", binary.LittleEndian.AppendUint16(nil, take), hotkey)
	fixture.set(t, "AutoParentDelegationEnabled", []byte{0}, hotkey)
	lastUpdate := []byte{8}
	lastUpdate = binary.LittleEndian.AppendUint64(lastUpdate, 0)
	lastUpdate = binary.LittleEndian.AppendUint64(lastUpdate, 0)
	fixture.set(t, "LastUpdate", lastUpdate, rootArg)
	client, err := newRpcClient("http://root-rpc.example", 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = roundTripFunc(fixture.roundTrip)
	return client, fixture
}

// Refuses mutating and unpinned requests rather than silently serving a stub.
func (self *rootRpcFixture) roundTrip(request *http.Request) (*http.Response, error) {
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	var call struct {
		JsonRpc string            `json:"jsonrpc"`
		Id      int               `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil || call.JsonRpc != "2.0" || call.Id != 1 {
		return nil, errors.New("unexpected root fixture request")
	}
	self.stateLock.Lock()
	self.methodCounts[call.Method]++
	storageRead := self.methodCounts["state_getStorage"] != 0
	self.stateLock.Unlock()
	if strings.HasPrefix(call.Method, "state_") || call.Method == "chain_getHeader" {
		if len(call.Params) == 0 || string(call.Params[len(call.Params)-1]) != `"`+testFinalizedHash+`"` {
			return nil, fmt.Errorf("%s escaped the pinned root block", call.Method)
		}
	}
	var result any
	switch call.Method {
	case "system_chain":
		result = "synthetic-mainnet"
	case "system_version":
		result = "synthetic-node"
	case "eth_chainId":
		result = self.evmChainHex
	case "chain_getFinalizedHead":
		result = testFinalizedHash
	case "chain_getHeader":
		result = identityTestHeader()
	case "chain_getBlockHash":
		if len(call.Params) != 1 {
			return nil, errors.New("bad root fixture block query")
		}
		switch string(call.Params[0]) {
		case "0":
			result = testGenesisHash
		case "100":
			result = testFinalizedHash
			if self.forkAfterStorage && storageRead {
				result = "0x" + strings.Repeat("d", 64)
			}
		default:
			return nil, errors.New("unexpected root fixture height")
		}
	case "state_getRuntimeVersion":
		result = self.version
	case "state_getStorageHash":
		if string(call.Params[0]) != `"0x3a636f6465"` {
			return nil, errors.New("root code query escaped :code")
		}
		result = self.policy.RuntimeCodeHash
	case "state_getMetadata":
		result = self.metadataHex
	case "state_getStorage":
		var key string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &key) != nil {
			return nil, errors.New("bad root storage parameters")
		}
		if value, ok := self.storageKVs[key]; ok {
			result = value
		}
	case "state_queryStorageAt":
		var keys []string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &keys) != nil || len(keys) == 0 || len(keys) > subnetDiscoveryStorageBatchKeys {
			return nil, errors.New("unbounded discovery storage batch")
		}
		changes := make([][]any, len(keys))
		for index, key := range keys {
			var value any
			if raw, exists := self.storageKVs[key]; exists {
				value = raw
			}
			changes[index] = []any{key, value}
		}
		result = []any{map[string]any{"block": testFinalizedHash, "changes": changes}}
	case "state_getKeysPaged":
		var prefix string
		var start *string
		var count int
		if len(call.Params) != 4 || json.Unmarshal(call.Params[0], &prefix) != nil || json.Unmarshal(call.Params[1], &count) != nil || json.Unmarshal(call.Params[2], &start) != nil || count != 128 {
			return nil, errors.New("unbounded root key page")
		}
		keys := []string{}
		for key := range self.storageKVs {
			if strings.HasPrefix(key, prefix) && (start == nil || key > *start) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		if len(keys) > count {
			keys = keys[:count]
		}
		if self.duplicatePage && len(keys) > 0 {
			keys = append(keys, keys[len(keys)-1])
		}
		result = keys
	default:
		return nil, fmt.Errorf("unadmitted or mutating root RPC method %s", call.Method)
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
	if call.Method == "state_getStorage" && self.omitStorage {
		delete(reply, "result")
	}
	raw, err := json.Marshal(reply)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw))}, err
}

// Counter reads remain safe while parallel workers return from a response.
func (self *rootRpcFixture) count(method string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.methodCounts[method]
}

// Ready means only the bounded read-only adoption conditions were observed.
func TestRootPreviewReadsExactSeatAndDoesNotAuthorizeSigning(t *testing.T) {
	client, fixture := newRootFixture(t)
	preview, err := client.readRootPreview(context.Background(), fixture.policy, "sha256:synthetic-policy")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.ReadOnlyReady || preview.Status != "ready" || preview.ActivationReady || len(preview.ActivationBlockers) == 0 || len(preview.Seats) != 2 || preview.SelectedSeat == nil || preview.SelectedSeat.Uid != 0 || preview.SelectedSeat.StakeRao != "100" || preview.RetentionMarginRao != "50" || preview.LowestNonImmuneUid == nil || *preview.LowestNonImmuneUid != 1 || preview.StakeRank != 1 {
		t.Fatalf("incorrect root preview: %+v", preview)
	}
	if fixture.count("state_getKeysPaged") != 6 {
		t.Fatalf("each of three censuses needs a terminal empty page: %d", fixture.count("state_getKeysPaged"))
	}
	if preview.RootWeightSettingEnabled {
		t.Fatal("fixture should exercise accumulate strategy while custom weight writes are disabled")
	}
	sealed, err := sealRootPreview(preview)
	if err != nil || !strings.HasPrefix(sealed.ContentHash, "sha256:") {
		t.Fatal(err)
	}
	preview.Storage[0].ValueSource = "tampered"
	changed, _ := sealRootPreview(preview)
	if sealed.ContentHash == changed.ContentHash {
		t.Fatal("raw root evidence was not sealed")
	}
}

// Matching runtime numbers cannot authorize a wrong network or changed code.
func TestRootPreviewRejectsWrongIdentityBeforeStorage(t *testing.T) {
	for _, change := range []string{"testnet", "genesis", "runtime", "code", "metadata"} {
		client, fixture := newRootFixture(t)
		policy := fixture.policy
		switch change {
		case "testnet":
			fixture.evmChainHex = "0x3b1"
		case "genesis":
			policy.GenesisHash = "0x" + strings.Repeat("e", 64)
		case "runtime":
			policy.RuntimeVersion.TransactionVersion++
		case "code":
			policy.RuntimeCodeHash = "0x" + strings.Repeat("e", 64)
		case "metadata":
			policy.RuntimeMetadataHash = "0x" + strings.Repeat("e", 64)
		}
		preview, err := client.readRootPreview(context.Background(), policy, "policy")
		if !errors.Is(err, errRpcIntegrity) || preview.ReadOnlyReady || fixture.count("state_getStorage") != 0 {
			t.Fatalf("%s reached root storage: %+v %v", change, preview, err)
		}
	}
}

// A silent fallback cannot invent a seat or erase an existing custom strategy.
func TestRootPreviewDetectsSeatAndStrategyDrift(t *testing.T) {
	for _, change := range []string{"generation", "owner", "low-stake", "custom-weights", "auto-parent", "child", "pending-child", "take"} {
		client, fixture := newRootFixture(t)
		hotkey, _ := hex.DecodeString(fixture.policy.Hotkey[2:])
		rootArg := []byte{0, 0}
		want := ""
		switch change {
		case "generation":
			fixture.policy.ExpectedSeat = &rootSeatExpectation{Uid: 0, RegistrationBlock: 39}
			want = "ROOT_REGISTRATION_GENERATION_NOT_APPROVED"
		case "owner":
			fixture.policy.Coldkey = "0x" + strings.Repeat("33", 32)
			want = "ROOT_OWNER_MISMATCH"
		case "low-stake":
			fixture.policy.MinimumStakeRao = "101"
			want = "ROOT_STAKE_BELOW_POLICY_MINIMUM"
		case "custom-weights":
			fixture.set(t, "Weights", []byte{4, 25, 0, 255, 255}, rootArg, rootArg)
			want = "ROOT_EXISTING_CUSTOM_WEIGHTS_REQUIRE_EXPLICIT_TRANSITION"
		case "auto-parent":
			fixture.set(t, "AutoParentDelegationEnabled", []byte{1}, hotkey)
			want = "ROOT_AUTOMATIC_DELEGATION_ENABLED"
		case "child", "pending-child":
			value := append(binary.LittleEndian.AppendUint64([]byte{4}, 1), bytes.Repeat([]byte{77}, 32)...)
			if change == "child" {
				fixture.set(t, "ChildKeys", value, hotkey, []byte{25, 0})
			} else {
				value = binary.LittleEndian.AppendUint64(value, 120)
				fixture.set(t, "PendingChildKeys", value, []byte{25, 0}, hotkey)
			}
			want = "ROOT_EXISTING_OR_PENDING_DELEGATION_REQUIRES_EXPLICIT_TRANSITION"
		case "take":
			fixture.set(t, "Delegates", []byte{0, 0}, hotkey)
			want = "ROOT_DELEGATE_TAKE_NOT_APPROVED"
		}
		preview, err := client.readRootPreview(context.Background(), fixture.policy, "policy")
		if err != nil || preview.ReadOnlyReady || !strings.Contains(strings.Join(preview.Blockers, ","), want) {
			t.Fatalf("%s not exposed: %+v %v", change, preview, err)
		}
	}
}

// Absence is never decoded from a value-query default into a registered owner.
func TestRootPreviewRejectsIncompleteOrContradictoryCensus(t *testing.T) {
	for _, change := range []string{"missing-owner", "wrong-reverse", "extra-reverse", "future-registration", "hole", "duplicate-page", "missing-result", "fork", "overlong-value"} {
		client, fixture := newRootFixture(t)
		hotkey, _ := hex.DecodeString(fixture.policy.Hotkey[2:])
		rootArg := []byte{0, 0}
		switch change {
		case "missing-owner":
			key, _ := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "Owner", hotkey)
			delete(fixture.storageKVs, key.Hex())
		case "wrong-reverse":
			fixture.set(t, "Uids", []byte{1, 0}, rootArg, hotkey)
		case "extra-reverse":
			fixture.set(t, "Uids", []byte{0, 0}, rootArg, bytes.Repeat([]byte{88}, 32))
		case "future-registration":
			fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 101), rootArg, rootArg)
		case "hole":
			key, _ := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "Keys", rootArg, rootArg)
			value := fixture.storageKVs[key.Hex()]
			delete(fixture.storageKVs, key.Hex())
			newKey, _ := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "Keys", rootArg, []byte{2, 0})
			fixture.storageKVs[newKey.Hex()] = value
		case "duplicate-page":
			fixture.duplicatePage = true
		case "missing-result":
			fixture.omitStorage = true
		case "fork":
			fixture.forkAfterStorage = true
		case "overlong-value":
			fixture.set(t, "TotalHotkeyAlpha", make([]byte, 9), hotkey, rootArg)
		}
		preview, err := client.readRootPreview(context.Background(), fixture.policy, "policy")
		if !errors.Is(err, errRpcIntegrity) || preview.ReadOnlyReady {
			t.Fatalf("%s admitted: %+v %v", change, preview, err)
		}
	}
}

// Correct metadata hashing still requires the exact consumed storage shapes.
func TestRootStorageProfileRejectsTypeAndDefaultChanges(t *testing.T) {
	for _, change := range []string{"key", "value", "query", "duplicate", "default"} {
		metadata, _, _ := rootTestMetadata(t)
		for palletIndex := range metadata.AsMetadataV14.Pallets {
			pallet := &metadata.AsMetadataV14.Pallets[palletIndex]
			if pallet.Name != "SubtensorModule" {
				continue
			}
			for index := range pallet.Storage.Items {
				entry := &pallet.Storage.Items[index]
				if entry.Name != "RootWeightSettingEnabled" {
					continue
				}
				switch change {
				case "key":
					entry.Type = types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Key: entry.Type.AsPlainType, Value: entry.Type.AsPlainType, Hashers: []types.StorageHasherV10{{IsIdentity: true}}}}
				case "value":
					entry.Type.AsPlainType = types.NewSi1LookupTypeIDFromUInt(999999)
				case "query":
					entry.Modifier = types.StorageFunctionModifierV0{IsOptional: true}
				case "duplicate":
					pallet.Storage.Items = append(pallet.Storage.Items, *entry)
				case "default":
					entry.Fallback = types.Bytes{2}
				}
				break
			}
		}
		if _, err := rootStorageProfile(metadata); err == nil {
			t.Fatalf("%s profile mutation was admitted", change)
		}
	}
}

// CLI fixtures traverse the actual dispatch and JSON envelope through HTTP.
func rootFixtureServer(t *testing.T, fixture *rootRpcFixture) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response, err := fixture.roundTrip(request)
		if err != nil {
			t.Errorf("root fixture request: %v", err)
			http.Error(writer, err.Error(), 400)
			return
		}
		defer response.Body.Close()
		writer.WriteHeader(response.StatusCode)
		if _, err := io.Copy(writer, response.Body); err != nil {
			t.Errorf("root fixture response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// Policy files use the actual strict reader; no private material is produced.
func rootTestPolicyFile(t *testing.T, policy rootValidatorPolicy) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "root-policy.json")
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// One explicit CLI sample cannot accidentally start an unbounded service.
func TestRootPreviewCommandIsBoundedAndMachineReadable(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), []string{"root-preview", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy)}, &stdout, &stderr)
	var event rootMonitorEvent
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || code != 0 || event.Sample != 1 || event.Status != "ready" || event.Snapshot == nil || event.Snapshot.Observation.ActivationReady {
		t.Fatalf("preview command: %d %v %s %s", code, err, stdout.String(), stderr.String())
	}
}

// Retry starvation cannot be reintroduced by passing a ten-second GET budget.
func TestRootCommandRejectsMissingAuthorityAndShortBudgetBeforeRpc(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	for _, change := range []string{"budget", "chain-id", "source", "role", "strategy", "missing-hash"} {
		policy := fixture.policy
		args := []string{"root-preview", "--rpc", server.URL}
		switch change {
		case "budget":
			args = append(args, "--retry-window", "10s")
		case "chain-id":
			policy.EvmChainId = 945
		case "source":
			policy.RuntimeSourceCommit = strings.Repeat("1", 40)
		case "role":
			policy.Role = "ur-validator"
		case "strategy":
			policy.BasketStrategy = "custom"
		case "missing-hash":
			policy.GenesisHash = ""
		}
		args = append(args, "--policy", rootTestPolicyFile(t, policy))
		var stdout, stderr bytes.Buffer
		if code := runMain(context.Background(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || fixture.count("system_chain") != 0 {
			t.Fatalf("%s authority reached RPC: %d %s", change, code, stderr.String())
		}
	}
}

// Full-consumption checks are independent of the metadata decoder's permissive
// generic codec and reject malicious vector sizes before any allocation.
func TestRootScaleRejectsNoncanonicalAndOversizedVectors(t *testing.T) {
	for _, data := range [][]byte{nil, {1, 0}, {2, 0, 0, 0}, {3}, {0, 0}, {4, 0, 0, 0}, {1, 64}} {
		if err := rootValidateScale(data, "weights"); err == nil {
			t.Fatalf("invalid weights admitted: %x", data)
		}
	}
	if err := rootValidateScale([]byte{0}, "weights"); err != nil {
		t.Fatal(err)
	}
}

// Re-encoding metadata supports additional targeted wire-shape cases without
// changing which artifact the policy authenticates.
func TestRootPreviewAuthenticatesMetadataBeforeStorage(t *testing.T) {
	client, fixture := newRootFixture(t)
	fixture.metadata.MagicNumber = 0
	encoded, err := codec.EncodeToHex(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	fixture.metadataHex = encoded
	if _, err := client.readRootPreview(context.Background(), fixture.policy, "policy"); !errors.Is(err, errRpcIntegrity) || fixture.count("state_getStorage") != 0 {
		t.Fatalf("substituted metadata reached storage: %v", err)
	}
}

// Lost membership is a complete blocked observation, not a fabricated default
// UID zero and not a request to burn fees for automatic re-registration.
func TestRootPreviewRetainsMissingSeatAsBlocked(t *testing.T) {
	client, fixture := newRootFixture(t)
	fixture.policy.Hotkey = "0x" + strings.Repeat("44", 32)
	fixture.policy.ExpectedSeat = nil
	preview, err := client.readRootPreview(context.Background(), fixture.policy, "policy")
	if err != nil || preview.ReadOnlyReady || preview.SelectedSeat != nil || !strings.Contains(strings.Join(preview.Blockers, ","), "ROOT_SEAT_NOT_REGISTERED") {
		t.Fatalf("missing seat was not retained as blocked: %+v %v", preview, err)
	}
}

// Pruning ranks skip immune seats and break equal stakes by registration age,
// then UID. These boundaries come from the profile source, not a fixed seat cap.
func TestRootPreviewPruningUsesImmunityAndRegistrationGeneration(t *testing.T) {
	for _, scenario := range []string{"equal-stake", "immune", "immune-boundary"} {
		client, fixture := newRootFixture(t)
		rootArg := []byte{0, 0}
		hotkey, _ := hex.DecodeString(fixture.policy.Hotkey[2:])
		fixture.set(t, "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, 50), hotkey, rootArg)
		fixture.policy.MinimumStakeRao = "1"
		wantUid := uint16(0)
		if scenario != "equal-stake" {
			registration := uint64(91)
			if scenario == "immune-boundary" {
				registration = 90
			} else {
				wantUid = 1
			}
			fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, registration), rootArg, rootArg)
			fixture.policy.ExpectedSeat = &rootSeatExpectation{Uid: 0, RegistrationBlock: registration}
			// An equal-stake older peer wins after immunity expires.
			if scenario == "immune-boundary" {
				wantUid = 1
			}
		}
		preview, err := client.readRootPreview(context.Background(), fixture.policy, "policy")
		if err != nil || preview.LowestNonImmuneUid == nil || *preview.LowestNonImmuneUid != wantUid || preview.SelectedSeat.Immune != (scenario == "immune") {
			t.Fatalf("%s pruning rule: %+v %v", scenario, preview, err)
		}
		if scenario == "equal-stake" && !strings.Contains(strings.Join(preview.Blockers, ","), "ROOT_SEAT_IS_CURRENT_PRUNING_CANDIDATE") {
			t.Fatal("full-network pruning risk was omitted")
		}
	}
}

// A failed worker cancels and joins all siblings. Explicit barriers force the
// maximum active set, so scheduler luck or a short timeout cannot prove this.
func TestRootParallelReadCancelsAndJoinsWorkers(t *testing.T) {
	allEntered := make(chan struct{})
	var stateLock sync.Mutex
	entered, exited := 0, 0
	rootCause := errors.New("synthetic census failure")
	err := rootReadParallel(context.Background(), 32, func(ctx context.Context, index int) error {
		stateLock.Lock()
		entered++
		if entered == rootReadConcurrency {
			close(allEntered)
		}
		stateLock.Unlock()
		defer func() { stateLock.Lock(); exited++; stateLock.Unlock() }()
		if index == 0 {
			<-allEntered
			return rootCause
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, rootCause) || entered != exited || entered < rootReadConcurrency {
		t.Fatalf("parallel read lost failure or workers: entered=%d exited=%d err=%v", entered, exited, err)
	}
}

// Recoverable faults do not reset an admitted observation or stop the bounded
// service. A transient sample is explicitly unavailable, never stale ready.
func TestRootMonitorContinuesAfterUnavailableSample(t *testing.T) {
	_, fixture := newRootFixture(t)
	var stateLock sync.Mutex
	identityReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var call struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(raw, &call) != nil {
			t.Error("malformed request")
			return
		}
		if call.Method == "system_chain" {
			stateLock.Lock()
			identityReads++
			sample := identityReads
			stateLock.Unlock()
			if sample == 1 {
				fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"synthetic unavailable observation"}}`)
				return
			}
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		response, err := fixture.roundTrip(request)
		if err != nil {
			t.Error(err)
			http.Error(writer, err.Error(), 400)
			return
		}
		defer response.Body.Close()
		if _, err := io.Copy(writer, response.Body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	var stdout, stderr bytes.Buffer
	checkpoint := filepath.Join(t.TempDir(), "root-finalized.json")
	args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--samples", "2", "--interval", "1ns", "--checkpoint", checkpoint}
	ctx := monitorTestStorageContext(t, t.Context(), args)
	code := runRootMonitorTest(ctx, args, &stdout, &stderr)
	decoder := json.NewDecoder(&stdout)
	var first, second rootMonitorEvent
	if decoder.Decode(&first) != nil || decoder.Decode(&second) != nil || code != 0 || first.Status != "rpc-error" || first.Snapshot != nil || first.Observation != nil || first.ReadCause != "unavailable" || first.ReadPhase != "sample" || second.Status != "ready" || second.Sample != 2 || second.Observation == nil {
		t.Fatalf("bounded recovery failed: exit=%d first=%+v second=%+v stderr=%s", code, first, second, stderr.String())
	}
	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("finalized progress not retained: %v", err)
	}
}

// The command cannot convert a testnet route into mainnet authority or silently
// retry an integrity conflict as if it were an ephemeral GET failure.
func TestRootCommandRejectsTestnetAndStopsIntegritySamples(t *testing.T) {
	_, fixture := newRootFixture(t)
	fixture.evmChainHex = "0x3b1"
	server := rootFixtureServer(t, fixture)
	var stdout, stderr bytes.Buffer
	code := runRootMonitorTest(context.Background(), []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--samples", "3", "--interval", "1ns"}, &stdout, &stderr)
	var event rootMonitorEvent
	if json.Unmarshal(stdout.Bytes(), &event) != nil || code != 3 || event.Status != "rpc-integrity" || event.Snapshot != nil || fixture.count("system_chain") != 1 || fixture.count("state_getStorage") != 0 {
		t.Fatalf("testnet route was admitted/retried: %d %s %s", code, stdout.String(), stderr.String())
	}
}

// A storage timeout is retried at the exact key/hash, preserving already read
// seat rows. The deterministic first response causes the failure, not timing.
func TestRootPreviewRetriesTransientStorageWithoutChangingBlock(t *testing.T) {
	client, fixture := newRootFixture(t)
	var stateLock sync.Mutex
	failedKey := ""
	failedCount := 0
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		var call struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		if call.Method == "state_getStorage" {
			stateLock.Lock()
			first := failedKey == ""
			if first {
				failedKey = string(call.Params[0])
			}
			if string(call.Params[0]) == failedKey {
				failedCount++
			}
			stateLock.Unlock()
			if first {
				return &http.Response{StatusCode: http.StatusGatewayTimeout, Body: io.NopCloser(strings.NewReader("temporary"))}, nil
			}
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		return fixture.roundTrip(request)
	})
	preview, err := client.readRootPreview(context.Background(), fixture.policy, "policy")
	if err != nil || !preview.ReadOnlyReady || failedCount < 2 || fixture.count("chain_getFinalizedHead") != 4 {
		t.Fatalf("storage retry restarted/lost sample: ready=%v err=%v failed-count=%d", preview.ReadOnlyReady, err, failedCount)
	}
}

// A retained stale finalized clock overrides otherwise healthy seat evidence.
// This reproduces the important distinction without waiting for a stall timer.
func TestRootMonitorRetainedStallCannotReportReady(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	path := filepath.Join(t.TempDir(), "root-finalized.json")
	expected := identityExpectation{NativeChain: fixture.policy.NativeChain, GenesisHash: fixture.policy.GenesisHash, EvmChainId: fixture.policy.EvmChainId}
	args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--checkpoint", path}
	ctx := monitorTestStorageContext(t, t.Context(), args)
	store, err := openMonitorCheckpoint(path, expected, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.save(&monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runRootMonitorTest(ctx, args, &stdout, &stderr)
	var event rootMonitorEvent
	if json.Unmarshal(stdout.Bytes(), &event) != nil || code != 3 || event.Status != "finality-stalled" || event.Observation == nil || event.Observation.ReadOnlyReady {
		t.Fatalf("stalled finalized state looked ready: %d %s %s", code, stdout.String(), stderr.String())
	}
}

// A wall-clock rollback cannot buy more liveness time for an unchanged finalized
// head. Real finalized advancement establishes a new trustworthy local clock.
func TestRootMonitorFutureProgressClockIsConservativelyStalled(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: base.Add(time.Hour)}
	identity := chainIdentity{FinalizedHash: testFinalizedHash, FinalizedNumber: 100}
	if status, err := state.observe(base, identity, time.Minute); err != nil || status != "finality-stalled" {
		t.Fatalf("future progress clock hid finality uncertainty: status=%s error=%v", status, err)
	}
	identity.FinalizedHash, identity.FinalizedNumber = testGenesisHash, 101
	if status, err := state.observe(base, identity, time.Minute); err != nil || status != "ok" || !state.lastProgressAt.Equal(base) {
		t.Fatalf("real progress did not recover clock: status=%s state=%+v error=%v", status, state, err)
	}
}

// A reloaded future checkpoint traverses the actual root command and cannot
// publish a ready sample from otherwise complete, unchanged chain evidence.
func TestRootMonitorFutureCheckpointCannotReportReady(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	path := filepath.Join(t.TempDir(), "root-finalized.json")
	expected := identityExpectation{NativeChain: fixture.policy.NativeChain, GenesisHash: fixture.policy.GenesisHash, EvmChainId: fixture.policy.EvmChainId}
	args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--checkpoint", path}
	ctx := monitorTestStorageContext(t, t.Context(), args)
	store, err := openMonitorCheckpoint(path, expected, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.save(&monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runRootMonitorTest(ctx, args, &stdout, &stderr)
	var event rootMonitorEvent
	if json.Unmarshal(stdout.Bytes(), &event) != nil || code != 3 || event.Status != "finality-stalled" || event.Observation == nil || event.Observation.ReadOnlyReady {
		t.Fatalf("future retained clock looked ready: exit=%d status=%s stderr=%s", code, event.Status, stderr.String())
	}
}
