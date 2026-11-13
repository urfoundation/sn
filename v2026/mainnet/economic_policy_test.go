// Synthetic finalized RPC fixtures exercise mode custody without any live chain.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/crv4"
)

// A minimal metadata14 artifact has the reviewed storage shape and synthetic
// portable ids. Tests may mutate metadata before its exact hash is approved.
func recycleTestMetadata(t *testing.T, mutate func(*types.Metadata)) (string, string) {
	t.Helper()
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	u16Id, netuidId, modeId := types.NewSi1LookupTypeIDFromUInt(0), types.NewSi1LookupTypeIDFromUInt(1), types.NewSi1LookupTypeIDFromUInt(2)
	metadata.AsMetadataV14.Lookup.Types = []types.PortableTypeV14{
		{ID: u16Id, Type: types.Si1Type{Def: types.Si1TypeDef{IsPrimitive: true, Primitive: types.Si1TypeDefPrimitive{Si0TypeDefPrimitive: types.IsU16}}}},
		{ID: netuidId, Type: types.Si1Type{Def: types.Si1TypeDef{IsComposite: true, Composite: types.Si1TypeDefComposite{Fields: []types.Si1Field{{Type: u16Id}}}}}},
		{ID: modeId, Type: types.Si1Type{Def: types.Si1TypeDef{IsVariant: true, Variant: types.Si1TypeDefVariant{Variants: []types.Si1Variant{
			{Name: "Burn", Index: 0}, {Name: "Recycle", Index: 1},
		}}}}},
	}
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{
		Name: "SubtensorModule", HasStorage: true, Storage: types.StorageMetadataV14{
			Prefix: "SubtensorModule", Items: []types.StorageEntryMetadataV14{{
				Name: "RecycleOrBurn", Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Fallback: types.Bytes{0},
				Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{
					Hashers: []types.StorageHasherV10{{IsIdentity: true}}, Key: netuidId, Value: modeId,
				}},
			}},
		},
	}}
	if mutate != nil {
		mutate(metadata)
	}
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, digest
}

// Configuration is set before the first request; only request counters mutate
// concurrently. Every state query must carry the original finalized hash.
type recycleRpcFixture struct {
	metadataHex       string
	version           crv4.RuntimeVersionIdentity
	codeHash          string
	storage           *string
	evmChainHex       string
	omitStorageResult bool
	forkAfterStorage  bool
	stateLock         sync.Mutex
	methodCounts      map[string]int
}

// Creates one owned local endpoint with no mutation or subscription methods.
func newRecycleTestClient(t *testing.T, storage *string, mutate func(*types.Metadata)) (*rpcClient, recyclePolicy, *recycleRpcFixture) {
	t.Helper()
	metadataHex, metadataHash := recycleTestMetadata(t, mutate)
	fixture := &recycleRpcFixture{
		metadataHex: metadataHex, version: crv4.RuntimeVersionIdentity{SpecName: "synthetic-runtime", SpecVersion: 471, TransactionVersion: 2, StateVersion: 1},
		codeHash: "0x" + strings.Repeat("c", 64), storage: storage, evmChainHex: "0x3c4", methodCounts: map[string]int{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			JsonRpc string            `json:"jsonrpc"`
			Id      int               `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil || call.JsonRpc != "2.0" || call.Id != 1 {
			t.Error("unexpected RPC request shape")
			http.Error(writer, "invalid fixture request", http.StatusBadRequest)
			return
		}
		fixture.stateLock.Lock()
		fixture.methodCounts[call.Method]++
		storageRead := fixture.methodCounts["state_getStorage"] != 0
		fixture.stateLock.Unlock()
		if call.Method == "state_getRuntimeVersion" || call.Method == "state_getStorageHash" || call.Method == "state_getMetadata" || call.Method == "state_getStorage" || call.Method == "chain_getHeader" {
			if len(call.Params) == 0 || string(call.Params[len(call.Params)-1]) != `"`+testFinalizedHash+`"` {
				t.Errorf("%s was not pinned to the finalized hash: %s", call.Method, call.Params)
				http.Error(writer, "unpinned state", http.StatusBadRequest)
				return
			}
		}
		var result any
		switch call.Method {
		case "system_chain":
			result = "synthetic-mainnet"
		case "system_version":
			result = "synthetic-node"
		case "eth_chainId":
			result = fixture.evmChainHex
		case "chain_getFinalizedHead":
			result = testFinalizedHash
		case "chain_getHeader":
			result = identityTestHeader()
		case "chain_getBlockHash":
			if len(call.Params) != 1 {
				t.Error("wrong block hash parameters")
				return
			}
			if string(call.Params[0]) == "0" {
				result = testGenesisHash
			} else if string(call.Params[0]) == "100" {
				result = testFinalizedHash
				if fixture.forkAfterStorage && storageRead {
					result = "0x" + strings.Repeat("d", 64)
				}
			} else {
				t.Errorf("unexpected block number %s", call.Params[0])
				return
			}
		case "state_getRuntimeVersion":
			result = fixture.version
		case "state_getStorageHash":
			if len(call.Params) != 2 || string(call.Params[0]) != `"0x3a636f6465"` {
				t.Error("runtime hash query did not select :code")
				return
			}
			result = fixture.codeHash
		case "state_getMetadata":
			result = fixture.metadataHex
		case "state_getStorage":
			var key string
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &key) != nil || len(key) != 70 || !strings.HasSuffix(key, "1900") {
				t.Errorf("wrong Identity/netuid25 storage key: %s", call.Params)
				return
			}
			result = fixture.storage
		default:
			t.Errorf("unexpected or mutating RPC method: %s", call.Method)
			http.Error(writer, "unadmitted RPC", http.StatusBadRequest)
			return
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
		if call.Method == "state_getStorage" && fixture.omitStorageResult {
			delete(reply, "result")
		}
		if err := json.NewEncoder(writer).Encode(reply); err != nil {
			t.Errorf("encode fixture reply: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	policy := recyclePolicy{
		Schema: recyclePolicySchema, NativeChain: "synthetic-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964, Netuid: 25,
		StorageProfile: recycleStorageProfile, RuntimeSourceCommit: strings.Repeat("1", 40),
		RuntimeVersion: fixture.version, RuntimeCodeHash: fixture.codeHash, RuntimeMetadataHash: metadataHash,
	}
	return client, policy, fixture
}

// Counter reads are safe while the local HTTP server is unwinding its reply.
func (self *recycleRpcFixture) count(method string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.methodCounts[method]
}

// Exact approved runtime471 can pass the mode precondition without inheriting
// runtime455 authority or claiming the economic target has been established.
func TestRecycleModeBindsApprovedRuntimeAndFinalizedStorage(t *testing.T) {
	mode := "0x01"
	client, policy, fixture := newRecycleTestClient(t, &mode, nil)
	observation, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash")
	if err != nil || !observation.ModeGatePassed || observation.Mode != "Recycle" || observation.ActivationReady || len(observation.Blockers) == 0 {
		t.Fatalf("mode observation: %+v error=%v", observation, err)
	}
	if observation.Identity.FinalizedHash != testFinalizedHash || observation.RuntimeVersion != policy.RuntimeVersion || observation.RuntimeMetadataHash != policy.RuntimeMetadataHash || observation.ValueSource != "finalized-storage" || observation.RawStorage == nil || *observation.RawStorage != mode || observation.EffectiveScale != mode || fixture.count("state_getStorage") != 1 {
		t.Fatalf("mode evidence lost its exact inputs: %+v", observation)
	}
	envelope, err := sealRecycleMode(observation)
	if err != nil || !strings.HasPrefix(envelope.ContentHash, "sha256:") {
		t.Fatalf("mode envelope: %+v %v", envelope, err)
	}
}

// A genuine null storage reply uses the authenticated Burn fallback and fails
// the recycle precondition; absence is never an implicit positive decision.
func TestRecycleModeMissingStorageUsesBurnFallback(t *testing.T) {
	client, policy, _ := newRecycleTestClient(t, nil, nil)
	observation, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash")
	if err != nil || observation.ModeGatePassed || observation.Mode != "Burn" || observation.ValueSource != "authenticated-metadata-fallback" || observation.RawStorage != nil || observation.EffectiveScale != "0x00" {
		t.Fatalf("absent mode was not explicit Burn: %+v %v", observation, err)
	}
}

// A stored Burn value remains distinguishable from an absent default.
func TestRecycleModeStoredBurnFailsPrecondition(t *testing.T) {
	mode := "0x00"
	client, policy, _ := newRecycleTestClient(t, &mode, nil)
	observation, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash")
	if err != nil || observation.ModeGatePassed || observation.Mode != "Burn" || observation.ValueSource != "finalized-storage" || observation.RawStorage == nil {
		t.Fatalf("stored Burn was admitted: %+v %v", observation, err)
	}
}

// A missing JSON result is a broken response, not a storage absence witness.
func TestRecycleModeMissingResultIsNotFallback(t *testing.T) {
	client, policy, fixture := newRecycleTestClient(t, nil, nil)
	fixture.omitStorageResult = true
	if _, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); !errors.Is(err, errRpcIntegrity) {
		t.Fatalf("missing result was admitted as storage absence: %v", err)
	}
}

// Unknown variants, malformed bytes and ignored trailing payloads all fail.
func TestRecycleModeRejectsNoncanonicalStorage(t *testing.T) {
	for _, mode := range []string{"0x", "0x02", "0x0100", "01", "0xgg", "0x1"} {
		client, policy, _ := newRecycleTestClient(t, &mode, nil)
		if _, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); !errors.Is(err, errRpcIntegrity) {
			t.Fatalf("mode %q was admitted: %v", mode, err)
		}
	}
}

// Reviewed bytes must match before any economic storage is requested.
func TestRecycleModeRejectsArtifactSubstitutionBeforeStorage(t *testing.T) {
	for _, mismatch := range []string{"version", "transaction", "state", "code", "metadata", "chain"} {
		mode := "0x01"
		client, policy, fixture := newRecycleTestClient(t, &mode, nil)
		switch mismatch {
		case "version":
			fixture.version.SpecVersion++
		case "transaction":
			fixture.version.TransactionVersion++
		case "state":
			fixture.version.StateVersion++
		case "code":
			fixture.codeHash = "0x" + strings.Repeat("d", 64)
		case "metadata":
			policy.RuntimeMetadataHash = "0x" + strings.Repeat("d", 64)
		case "chain":
			fixture.evmChainHex = "0x3b1"
		}
		if _, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); !errors.Is(err, errRpcIntegrity) || fixture.count("state_getStorage") != 0 {
			t.Fatalf("%s substitution reached storage: %v", mismatch, err)
		}
	}
}

// A caller-approved hash cannot make an unsupported metadata shape inherit the
// reviewed enum/default semantics merely because its storage byte is one.
func TestRecycleModeRejectsChangedMetadataSemantics(t *testing.T) {
	for _, change := range []string{"fallback", "optional", "prefix", "duplicate", "key-width", "hasher", "variant-index", "variant-fields"} {
		mode := "0x01"
		client, policy, fixture := newRecycleTestClient(t, &mode, func(metadata *types.Metadata) {
			storage := &metadata.AsMetadataV14.Pallets[0].Storage
			switch change {
			case "fallback":
				storage.Items[0].Fallback = types.Bytes{1}
			case "optional":
				storage.Items[0].Modifier = types.StorageFunctionModifierV0{IsOptional: true}
			case "prefix":
				storage.Prefix = "OtherModule"
			case "duplicate":
				storage.Items = append(storage.Items, storage.Items[0])
			case "key-width":
				metadata.AsMetadataV14.Lookup.Types[0].Type.Def.Primitive.Si0TypeDefPrimitive = types.IsU32
			case "hasher":
				storage.Items[0].Type.AsMap.Hashers = []types.StorageHasherV10{{IsBlake2_128Concat: true}}
			case "variant-index":
				metadata.AsMetadataV14.Lookup.Types[2].Type.Def.Variant.Variants[1].Index = 2
			case "variant-fields":
				metadata.AsMetadataV14.Lookup.Types[2].Type.Def.Variant.Variants[1].Fields = []types.Si1Field{{Type: types.NewSi1LookupTypeIDFromUInt(0)}}
			}
		})
		if _, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); !errors.Is(err, errRpcIntegrity) || fixture.count("state_getStorage") != 0 {
			t.Fatalf("%s metadata reached storage: %v", change, err)
		}
	}
}

// A contradictory finalized mapping cannot seal otherwise matching bytes.
func TestRecycleModeRejectsFinalizedMappingChange(t *testing.T) {
	mode := "0x01"
	client, policy, fixture := newRecycleTestClient(t, &mode, nil)
	fixture.forkAfterStorage = true
	if _, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); !errors.Is(err, errRpcIntegrity) {
		t.Fatalf("changed finalized mapping was accepted: %v", err)
	}
}

// Metadata can exceed the identity-response budget while retaining a separate
// finite allowance and exact byte-hash authentication.
func TestRecycleModeReadsMetadataBeyondIdentityReplyLimit(t *testing.T) {
	mode := "0x01"
	client, policy, _ := newRecycleTestClient(t, &mode, func(metadata *types.Metadata) {
		metadata.AsMetadataV14.Lookup.Types[0].Type.Docs = []types.Text{types.Text(strings.Repeat("m", 600*1024))}
	})
	if observation, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); err != nil || !observation.ModeGatePassed {
		t.Fatalf("bounded large metadata failed: %+v %v", observation, err)
	}
}

// Existing reviewed metadata artifacts exercise the profile against real
// portable type layouts without copying chain state into a new fixture.
func TestRecycleModeProfileMatchesRetainedMetadata(t *testing.T) {
	for _, path := range []string{"../miner/testdata/runtime455-metadata.scale.gz.base64", "../crv4/runtime-profile-v1.scale.gz.base64"} {
		encoded, err := os.ReadFile(path)
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
		metadata, _, err := crv4.DecodeRuntimeMetadata(fmt.Sprintf("0x%x", raw))
		if err != nil {
			t.Fatal(err)
		}
		key, fallback, err := recycleModeStorage(metadata, 25)
		if err != nil || len(key) != 34 || !bytes.Equal(fallback, []byte{0}) {
			t.Fatalf("retained metadata %s does not satisfy the storage profile: %x %x %v", path, key, fallback, err)
		}
	}
}

// Large but invalid SCALE cannot reach storage merely because metadata has a
// larger transport allowance than the small identity responses.
func TestRecycleModeRejectsInvalidLargeMetadata(t *testing.T) {
	mode := "0x01"
	client, policy, fixture := newRecycleTestClient(t, &mode, nil)
	fixture.metadataHex = "0x" + strings.Repeat("ff", 600*1024)
	if _, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); !errors.Is(err, errRpcIntegrity) || fixture.count("state_getStorage") != 0 {
		t.Fatalf("invalid large metadata reached storage: %v", err)
	}
}

// Metadata still has a hard response ceiling; an oversized body fails before
// JSON/SCALE decoding and cannot allocate according to its declared lengths.
func TestRecycleModeMetadataReplyLimitIsBounded(t *testing.T) {
	client, _, _ := newRecycleTestClient(t, nil, nil)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxMetadataRpcReplyBytes+1)))}, nil
	})
	var encoded string
	if err := client.call(context.Background(), "state_getMetadata", []any{testFinalizedHash}, &encoded); !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "reply exceeds 8 MiB") || encoded != "" {
		t.Fatalf("oversized metadata response was admitted: %v", err)
	}
}

// Allowing absent storage must not turn a null identity response into data.
func TestRecycleModeNullableReadDoesNotRelaxIdentity(t *testing.T) {
	client, _, _ := newRecycleTestClient(t, nil, nil)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":null}`))}, nil
	})
	var value *string
	if err := client.call(context.Background(), "system_chain", nil, &value); !errors.Is(err, errRpcIntegrity) {
		t.Fatalf("null identity was accepted: %v", err)
	}
	if err := client.callWithStorageAbsence(context.Background(), "system_chain", nil, &value, true); err == nil {
		t.Fatal("nullable non-storage method was accepted")
	}
}

// Every state read inherits one total observation deadline, including the
// identity preflight; repeated operations cannot restart the entire budget.
func TestRecycleModeUsesOneSampleDeadline(t *testing.T) {
	mode := "0x01"
	client, policy, _ := newRecycleTestClient(t, &mode, nil)
	transport := client.httpClient.Transport
	var deadline time.Time
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		current, ok := request.Context().Deadline()
		if !ok {
			return nil, errors.New("missing sample deadline")
		}
		if deadline.IsZero() {
			deadline = current
		} else if current != deadline {
			return nil, fmt.Errorf("sample deadline restarted: %v != %v", current, deadline)
		}
		return transport.RoundTrip(request)
	})
	if _, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); err != nil {
		t.Fatal(err)
	}
}

// Policy rejection happens without visiting an endpoint or printing a success.
func TestRecyclePolicyRejectsMissingAuthority(t *testing.T) {
	for _, change := range []string{"schema", "netuid", "chain-id", "genesis", "source", "profile", "state-version", "metadata"} {
		client, policy, fixture := newRecycleTestClient(t, nil, nil)
		switch change {
		case "schema":
			policy.Schema = ""
		case "netuid":
			policy.Netuid = 0
		case "chain-id":
			policy.EvmChainId = 945
		case "genesis":
			policy.GenesisHash = ""
		case "source":
			policy.RuntimeSourceCommit = "unreviewed"
		case "profile":
			policy.StorageProfile = "future-unreviewed-profile"
		case "state-version":
			policy.RuntimeVersion.StateVersion = 0
		case "metadata":
			policy.RuntimeMetadataHash = "0x" + strings.Repeat("0", 64)
		}
		if _, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash"); err == nil || fixture.count("system_chain") != 0 {
			t.Fatalf("%s missing authority reached RPC: %v", change, err)
		}
	}
}

// Check that content sealing changes with storage evidence, without granting
// any signature, policy approval or economic activation semantics to that hash.
func TestRecycleModeSealBindsRawAbsence(t *testing.T) {
	client, policy, _ := newRecycleTestClient(t, nil, nil)
	observation, err := client.readRecycleMode(context.Background(), policy, "synthetic-policy-hash")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := sealRecycleMode(observation)
	mode := "0x00"
	observation.RawStorage = &mode
	after, _ := sealRecycleMode(observation)
	if before.ContentHash == after.ContentHash {
		t.Fatal("raw absence was not bound by the content hash")
	}
}
