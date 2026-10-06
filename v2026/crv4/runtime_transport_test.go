// Exact runtime/provisional readers share transport without sharing authority.
// Forced owner transitions reproduce replacement races without timing sleeps.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcclient "github.com/centrifuge/go-substrate-rpc-client/v4/client"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/gorilla/websocket"
)

// Reconnect expires the observation, not the original signed bytes. Fresh
// authentication can resume their validation without changing the signature.
func TestRuntimeTransportRetainedSourceReobservesWithoutResigning(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	client := newRuntimeTransportTestClient(fixture.chain.API.Client)
	fixture.chain.API.Client = client
	artifact := fixture.bind(t)
	prepared := fixture.prepared(t, nil)
	signed := prepared.ExtrinsicHex
	client.generation.Store(2)
	var temporary net.Error
	if err := fixture.chain.ValidatePreparedSource(prepared); !errors.As(err, &temporary) || !temporary.Temporary() {
		t.Fatalf("retained source reconnect was not a reobservation outcome: %v", err)
	}
	forged := artifact
	forged.CodeHash = types.Hash{0xf1}.Hex()
	temporary = nil
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), fixture.chain, forged); err == nil || errors.As(err, &temporary) {
		t.Fatalf("forged tuple inherited transient expiry: %v", err)
	}
	fixture.bind(t)
	if err := fixture.chain.ValidatePreparedSource(prepared); err != nil || prepared.ExtrinsicHex != signed {
		t.Fatalf("reobservation changed retained source bytes: %v", err)
	}
}

// The public stake reader makes a separate consumed-API read. Replacement at
// that read must invalidate its entire observation, then allow the same block.
func TestRuntimeTransportPublicStakeClosingReadRequiresReobservation(t *testing.T) {
	fixture := newValidatorStakeCapabilityTestFixture(t)
	identity := fixture.stake.identity
	client := newRuntimeTransportTestClient(identity.chain.API.Client)
	identity.chain.API.Client = client
	faulted := false
	fixture.beforeRead = func(count int) {
		if count == 2 {
			faulted = true
			client.generation.Store(2)
		}
	}
	waits := 0
	identity.chain.runtimeObservationRead.wait = func(context.Context, time.Duration) error { waits++; return nil }
	observed, err := fixture.stake.read()
	if err != nil || !faulted || waits != 1 || observed.Identity.BlockHash != identity.query.BlockHash || observed.TotalStakeRao != 150 {
		t.Fatalf("public stake did not automatically reobserve original block: faulted=%v waits=%d observed=%+v err=%v", faulted, waits, observed, err)
	}
}

// Actual WebSocket closure and reconnection use the same public API container.
// The pending read is the positive join barrier; no timer selects the race.
func TestRuntimeTransportActualWebsocketReconnectRevokesSameApiProof(t *testing.T) {
	encoded, metadataHash := runtimeIdentityTestMetadata(t)
	block := types.Hash{0x31}
	identity := RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: ReviewedRuntimeSpecVersion + 1, TransactionVersion: 1, StateVersion: 1},
		CodeHash: types.Hash{0x32}.Hex(), MetadataHash: metadataHash}
	var connections atomic.Int64
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		connections.Add(1)
		defer connection.Close()
		for {
			var input chainContextRPCRequest
			if err := connection.ReadJSON(&input); err != nil {
				return
			}
			if input.Method == "fixture_disconnect" {
				return
			}
			if len(input.Params) == 0 || string(input.Params[len(input.Params)-1]) != fmt.Sprintf("%q", block.Hex()) {
				t.Error("WebSocket runtime read lost its pinned block")
				return
			}
			var result any
			switch input.Method {
			case "state_getRuntimeVersion":
				result = identity.Version
			case "state_getStorageHash":
				result = identity.CodeHash
			case "state_getMetadata":
				result = encoded
			default:
				t.Errorf("unexpected WebSocket runtime method %s", input.Method)
				return
			}
			if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": result}); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	open := func() *Chain {
		client, err := dialContextSubstrateClient(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		return &Chain{API: &gsrpc.SubstrateAPI{Client: client}, GenesisHash: types.Hash{0x33}}
	}
	owner, peer := open(), open()
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), owner, block, identity)
	if err != nil {
		t.Fatal(err)
	}
	other, err := AuthenticateRuntimeArtifactAtContext(t.Context(), peer, block, identity)
	if err != nil {
		t.Fatal(err)
	}
	client := owner.API.Client.(*contextSubstrateClient)
	api, generation := owner.API, client.TransportGeneration()
	if err := client.Client.CallContext(t.Context(), nil, "fixture_disconnect"); err == nil {
		t.Fatal("closed actual WebSocket returned a complete read")
	}
	var temporary net.Error
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), owner, artifact); !errors.As(err, &temporary) || !temporary.Temporary() || client.TransportGeneration() <= generation {
		t.Fatalf("actual WebSocket closure retained same-API authority: %v", err)
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), owner, block, identity); !runtimeObservationMayRepeat(err) || connections.Load() != 3 {
		t.Fatalf("first direct attempt did not reject its actual reconnect: connections=%d err=%v", connections.Load(), err)
	}
	if err := client.Client.CallContext(t.Context(), nil, "fixture_disconnect"); err == nil {
		t.Fatal("second actual disconnect returned success")
	}
	waits := 0
	owner.runtimeObservationRead.wait = func(context.Context, time.Duration) error { waits++; return nil }
	fresh, err := ReadRuntimeArtifactAtContext(t.Context(), owner, block, identity)
	if err != nil || waits != 1 || owner.API != api || owner.API.Client != client || connections.Load() != 4 {
		t.Fatalf("same API failed to reobserve after actual reconnect: connections=%d err=%v", connections.Load(), err)
	}
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), owner, fresh); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), peer, other); err != nil {
		t.Fatalf("independent WebSocket peer lost its retained proof: %v", err)
	}
}

// The mutable generation models the same published atomic contract as the real
// RPC client; its actual disconnect/reconnect is tested in that package too.
type runtimeTransportTestClient struct {
	gsrpcclient.Client
	generation atomic.Uint64
	endpoint   string
}

// A real HTTP connection owner is replaced while its API/cache container stays
// alive. An independent second owner continues to serve the same exact bytes.
func TestRuntimeTransportActualHttpReplacementReobservesAndKeepsPeer(t *testing.T) {
	encoded, metadataHash := runtimeIdentityTestMetadata(t)
	block := types.Hash{0x45}
	identity := RuntimeArtifactIdentity{
		Version:  RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: ReviewedRuntimeSpecVersion + 1, TransactionVersion: 1, StateVersion: 1},
		CodeHash: types.Hash{0x61}.Hex(), MetadataHash: metadataHash,
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var input chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		requests.Add(1)
		if len(input.Params) == 0 || string(input.Params[len(input.Params)-1]) != fmt.Sprintf("%q", block.Hex()) {
			t.Error("HTTP runtime observation lost its exact block selector")
			return
		}
		var result any
		switch input.Method {
		case "state_getRuntimeVersion":
			result = identity.Version
		case "state_getStorageHash":
			result = identity.CodeHash
		case "state_getMetadata":
			result = encoded
		default:
			t.Errorf("unexpected HTTP runtime method %s", input.Method)
			return
		}
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	open := func() *Chain {
		client, err := dialContextSubstrateClient(t.Context(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		return &Chain{API: &gsrpc.SubstrateAPI{Client: client}, GenesisHash: types.Hash{0x62}}
	}
	owner, peer := open(), open()
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), owner, block, identity)
	if err != nil {
		t.Fatal(err)
	}
	other, err := AuthenticateRuntimeArtifactAtContext(t.Context(), peer, block, identity)
	if err != nil {
		t.Fatal(err)
	}
	owner.API.Client.Close()
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), owner, artifact); err == nil {
		t.Fatal("closed actual HTTP owner retained exact runtime authority")
	}
	owner.API.Client = open().API.Client
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), owner, artifact); err == nil {
		t.Fatal("replacement HTTP owner inherited its predecessor capability")
	}
	before := requests.Load()
	fresh, err := AuthenticateRuntimeArtifactAtContext(t.Context(), owner, block, identity)
	if err != nil || requests.Load()-before != 2 {
		t.Fatalf("replacement HTTP owner did not repeat exact state observation: err=%v requests=%d", err, requests.Load()-before)
	}
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), owner, fresh); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), peer, other); err != nil {
		t.Fatalf("unrelated HTTP peer lost its own capability: %v", err)
	}
}

// A redirect response is an observable endpoint refusal. It cannot silently
// delegate either a read or a retained request to an unapproved peer.
func TestRuntimeTransportActualHttpRedirectCannotDelegateAuthority(t *testing.T) {
	var forwarded atomic.Int64
	next := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		forwarded.Add(1)
		var input chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": "foreign"}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(next.Close)
	original := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, next.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(original.Close)
	client, err := dialContextSubstrateClient(t.Context(), original.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	var result string
	if err := client.CallContext(t.Context(), &result, "system_chain"); err == nil || forwarded.Load() != 0 || result != "" {
		t.Fatalf("approved HTTP owner delegated its read: result=%q forwarded=%d err=%v", result, forwarded.Load(), err)
	}
	if err := client.CallContext(t.Context(), &result, "author_submitExtrinsic", "0x0102"); err == nil || forwarded.Load() != 0 || result != "" {
		t.Fatalf("approved HTTP owner delegated retained request bytes: result=%q forwarded=%d err=%v", result, forwarded.Load(), err)
	}
}

// Only test-owned exclusive endpoint changes are permitted.
func (self *runtimeTransportTestClient) URL() string { return self.endpoint }

// Published generations can be observed concurrently with a forced transition.
func (self *runtimeTransportTestClient) TransportGeneration() uint64 {
	return self.generation.Load()
}

// Retain the real runtime read implementation while adding transport custody.
func newRuntimeTransportTestClient(client gsrpcclient.Client) *runtimeTransportTestClient {
	self := &runtimeTransportTestClient{Client: client, endpoint: "wss://runtime.example"}
	self.generation.Store(1)
	return self
}

// Reusing the API/cache container cannot carry an old proof to another client.
func TestRuntimeTransportStrictProofRejectsReplacedClient(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	artifact := fixture.bind(t)
	previous := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	fixture.chain.API.Client = &runtimeIdentityTestClient{callContext: previous.callContext}
	before := fixture.calls
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), fixture.chain, artifact); err == nil {
		t.Fatal("replaced client inherited the old exact runtime capability")
	}
	if err := fixture.chain.BindRuntimeArtifact(artifact); err == nil || fixture.calls != before {
		t.Fatal("old capability bound or performed a replacement read")
	}
	if err := fixture.chain.ValidatePreparedSource(fixture.prepared(t, nil)); err == nil {
		t.Fatal("replaced client authorized retained source use without reobservation")
	}
	fresh, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, fixture.block, fixture.identity)
	if err != nil || fixture.calls-before != 2 {
		t.Fatalf("replacement did not reobserve exact version and code: calls=%d err=%v", fixture.calls-before, err)
	}
	if err := fixture.chain.BindRuntimeArtifact(fresh); err != nil {
		t.Fatal(err)
	}
	if err := fixture.chain.ValidatePreparedSource(fixture.prepared(t, nil)); err != nil {
		t.Fatal(err)
	}
}

// The same pointer cannot transfer authority to a different configured route.
func TestRuntimeTransportStrictProofRejectsEndpointChange(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	client := newRuntimeTransportTestClient(fixture.chain.API.Client)
	fixture.chain.API.Client = client
	artifact := fixture.bind(t)
	client.endpoint = "wss://replacement.example"
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), fixture.chain, artifact); err == nil {
		t.Fatal("changed endpoint inherited the original runtime capability")
	}
	client.endpoint = "wss://runtime.example"
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal(err)
	}
	client.generation.Store(0)
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, fixture.block, fixture.identity); err == nil {
		t.Fatal("closed transport acquired an exact runtime capability")
	}
}

// Correct individual replies from different connection generations cannot be
// joined into an observation that no one transport actually completed.
func TestRuntimeTransportObservationCannotMixGenerations(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	original := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	client := newRuntimeTransportTestClient(original)
	fixture.chain.API.Client = client
	read := original.callContext
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := read(ctx, target, method, args...)
		if method == "state_getMetadata" {
			client.generation.Store(2)
		}
		return err
	}
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, fixture.block, fixture.identity)
	if err == nil || artifact.authenticationProof != nil {
		t.Fatal("mixed transport generations published exact runtime authority")
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, fixture.block, fixture.identity); err != nil {
		t.Fatalf("new generation could not independently reobserve: %v", err)
	}
}

// A synchronous handoff inside the last read reproduces API.Client replacement
// without a fixture data race or invented network result.
func TestRuntimeTransportClientChangeDuringObservationRefusesProof(t *testing.T) {
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	previous := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	read := previous.callContext
	next := &runtimeIdentityTestClient{callContext: read}
	previous.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := read(ctx, target, method, args...)
		if method == "state_getMetadata" {
			fixture.chain.API.Client = next
		}
		return err
	}
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, fixture.block, fixture.identity)
	if err == nil || artifact.authenticationProof != nil {
		t.Fatal("replacement during observation published a mixed-owner capability")
	}
}

// Producer purpose validation performs an additional API read after the exact
// artifact read; that closing boundary must preserve the same transport too.
func TestRuntimeTransportProducerPurposeRechecksAfterApiRead(t *testing.T) {
	fixture := validatorProducerRuntimeFixture(t)
	original := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	client := newRuntimeTransportTestClient(original)
	fixture.chain.API.Client = client
	artifact := fixture.bind(t)
	read := original.callContext
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := read(ctx, target, method, args...)
		client.generation.Store(2)
		return err
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err == nil {
		t.Fatal("producer purpose accepted replacement during its closing API read")
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("failed purpose observation acquired signing authority")
	}
}

// A simultaneous provisional reader keeps its own observer and policy. The
// strict reader authenticates an independently supplied complete tuple on the
// same transport and cannot promote the provisional result.
func TestRuntimeTransportPooledStrictAndProvisionalRemainIndependent(t *testing.T) {
	fixture := newRuntimeProofFixture(t)
	client := newRuntimeTransportTestClient(fixture.chain.API.Client)
	fixture.chain.API.Client = client
	provisional, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{1}, fixture.allowed)
	if err != nil {
		t.Fatal(err)
	}
	_, metadataHash, _ := provisionalRuntimeMetadataTest(t)
	approved := RuntimeArtifactIdentity{
		Version:  RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: ReviewedRuntimeSpecVersion + 1, TransactionVersion: 1, StateVersion: 1},
		CodeHash: types.Hash{0x61, 1}.Hex(), MetadataHash: metadataHash,
	}
	strict := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, GenesisHash: fixture.chain.GenesisHash}
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), strict, provisional); err == nil {
		t.Fatal("pooled provisional evidence became strict authority")
	}
	peerDone := make(chan error, 1)
	start := make(chan struct{})
	go func() {
		<-start
		for index := byte(2); index <= 5; index++ {
			artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{index}, fixture.allowed)
			if err != nil {
				peerDone <- err
				return
			}
			if !fixture.chain.RuntimeArtifactCompatible(artifact) {
				peerDone <- errors.New("provisional peer lost its separate authority")
				return
			}
		}
		peerDone <- nil
	}()
	close(start)
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), strict, types.Hash{1}, approved)
	peerErr := <-peerDone
	if err != nil || peerErr != nil {
		t.Fatalf("pooled readers failed independently: strict=%v peer=%v", err, peerErr)
	}
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), strict, artifact); err != nil || fixture.observations.Load() != 5 {
		t.Fatalf("strict observation affected provisional progress: audits=%d err=%v", fixture.observations.Load(), err)
	}
	changed := approved
	changed.MetadataHash = types.Hash{0x82}.Hex()
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), strict, types.Hash{1}, changed); err == nil {
		t.Fatal("changed strict metadata catalog inherited a pooled decision")
	}
	client.generation.Store(2)
	if err := ValidateRuntimeArtifactOwnerContext(t.Context(), strict, artifact); err == nil || fixture.chain.RuntimeArtifactCompatible(provisional) {
		t.Fatal("old transport generation survived pooled replacement")
	}
	fresh, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{1}, fixture.allowed)
	if err != nil || !fixture.chain.RuntimeArtifactCompatible(fresh) {
		t.Fatalf("provisional traffic could not reobserve after replacement: %v", err)
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), strict, types.Hash{1}, approved); err != nil {
		t.Fatal(err)
	}
}
