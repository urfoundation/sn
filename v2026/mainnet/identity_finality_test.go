// Finality tests change the reported head at an exact dependent read. All
// headers are independently encoded; canonical height alone is not finality.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// One synchronous transport owns every transition. The selected block stays
// fixed while the independently reported finalized head can advance or regress.
type identityFinalityFixture struct {
	client       *rpcClient
	mapping      *finalizedMappingFixture
	head         uint64
	headHashes   map[uint64]string
	headHeaders  map[string]rootReceiptHeader
	canonicalKVs map[uint64]string
	counts       map[string]int
	after        func(string, []any)
	reply        func(string, []any) (any, bool)
}

func newIdentityFinalityFixture(t *testing.T) *identityFinalityFixture {
	t.Helper()
	client, mapping := newFinalizedMappingFixture(t, 1, 0)
	self := &identityFinalityFixture{client: client, mapping: mapping, head: 150,
		headHashes: map[uint64]string{100: mapping.nativeHash}, headHeaders: map[string]rootReceiptHeader{mapping.nativeHash: mapping.nativeHeader},
		canonicalKVs: map[uint64]string{0: testGenesisHash, 100: mapping.nativeHash}, counts: map[string]int{}}
	for _, number := range []uint64{90, 150, 180} {
		header, hash := rootReceiptHeaderFixture(t, testGenesisHash, number, nil, false)
		self.headHashes[number], self.headHeaders[hash], self.canonicalKVs[number] = hash, header, hash
	}
	client.httpClient.Transport = roundTripFunc(self.roundTrip)
	// Tests that leave a lower head in place exhaust the real retry boundary
	// explicitly. Recovery tests replace this hook with their transition.
	client.retryWait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	return self
}

func (self *identityFinalityFixture) roundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	var call struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		return nil, err
	}
	self.counts[call.Method]++
	var result any
	handled := true
	switch call.Method {
	case "chain_getFinalizedHead":
		result = self.headHashes[self.head]
	case "chain_getHeader":
		if len(call.Params) != 1 {
			return nil, errors.New("finality header was not pinned")
		}
		result = self.headHeaders[call.Params[0].(string)]
	case "chain_getBlockHash":
		if len(call.Params) != 1 {
			return nil, errors.New("finality number was not pinned")
		}
		result = self.canonicalKVs[uint64(call.Params[0].(float64))]
	case "state_getRuntimeVersion":
		result = map[string]any{"specName": "synthetic-runtime", "specVersion": 991, "transactionVersion": 1, "stateVersion": 1}
	default:
		handled = false
	}
	if self.reply != nil {
		if replacement, ok := self.reply(call.Method, call.Params); ok {
			result, handled = replacement, true
		}
	}
	if self.after != nil {
		self.after(call.Method, call.Params)
	}
	if !handled {
		return self.mapping.roundTrip(request)
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
}

// A canonical block100 cannot remain finalized when the current finalized
// witness falls from150 to90 during the selected runtime read.
func TestIdentityHistoricalFinalityRejectsRegressedHead(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	f.after = func(method string, _ []any) {
		if method == "state_getRuntimeVersion" {
			f.head = 90
		}
	}
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err == nil || identity != (chainIdentity{}) {
		t.Fatalf("historical100 survived finalized150 ->90: identity=%+v err=%v", identity, err)
	}
}

// The original finalized witness must remain canonical even when both the
// selected block and a later finalized head still have consistent headers.
func TestIdentityHistoricalFinalityRetainsOpeningWitness(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	f.after = func(method string, _ []any) {
		if method == "state_getRuntimeVersion" {
			f.head, f.canonicalKVs[150] = 180, testGenesisHash
		}
	}
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if !errors.Is(err, errRpcIntegrity) || identity != (chainIdentity{}) {
		t.Fatalf("replaced finalized150 witness survived historical100: identity=%+v err=%v", identity, err)
	}
}

// Choosing current150 rather than historical100 must use the same closing
// head guard; no partial identity is published after the dependent read.
func TestIdentityCurrentFinalityRejectsRegressedHead(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	f.after = func(method string, _ []any) {
		if method == "state_getRuntimeVersion" {
			f.head = 90
		}
	}
	identity, err := f.client.readIdentity(t.Context())
	if err == nil || identity != (chainIdentity{}) {
		t.Fatalf("current150 survived finalized150 ->90: identity=%+v err=%v", identity, err)
	}
}

// The original selected snapshot remains stable through normal finality
// advancement. Requiring head equality would prevent useful archive reads.
func TestIdentityFinalityAdvancePreservesSelectedSnapshot(t *testing.T) {
	for _, historical := range []bool{false, true} {
		f := newIdentityFinalityFixture(t)
		selected, number := "", uint64(150)
		if historical {
			selected, number = f.mapping.nativeHash, 100
		}
		f.after = func(method string, _ []any) {
			if method == "state_getRuntimeVersion" {
				f.head = 180
			}
		}
		identity, err := f.client.readIdentityAt(t.Context(), selected)
		if err != nil || identity.FinalizedNumber != number || identity.FinalizedHash != f.headHashes[number] {
			t.Fatalf("historical=%t normal advance changed selected snapshot: %+v %v", historical, identity, err)
		}
	}
}

// Malformed finality is not repaired by the retained canonical number. The
// returned header must authenticate its whole body, including its height.
func TestIdentityFinalityRejectsMalformedClosingWitness(t *testing.T) {
	for _, fault := range []string{"null head", "short head", "zero head", "forged header", "null canonical"} {
		f := newIdentityFinalityFixture(t)
		closing := false
		f.after = func(method string, _ []any) { closing = closing || method == "state_getRuntimeVersion" }
		f.reply = func(method string, params []any) (any, bool) {
			if !closing {
				return nil, false
			}
			switch fault {
			case "null head":
				return nil, method == "chain_getFinalizedHead"
			case "short head":
				return "0x01", method == "chain_getFinalizedHead"
			case "zero head":
				return fmt.Sprintf("0x%064x", 0), method == "chain_getFinalizedHead"
			case "forged header":
				header := f.headHeaders[f.headHashes[150]]
				header.Number = "0x5a"
				return header, method == "chain_getHeader"
			case "null canonical":
				return nil, method == "chain_getBlockHash" && params[0] == float64(150)
			}
			return nil, false
		}
		identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
		if err == nil || identity != (chainIdentity{}) {
			t.Errorf("%s closing witness accepted: %+v %v", fault, identity, err)
		}
	}
}

// The mapping consumer must re-open and close finality even if its caller's
// selected identity was authenticated earlier and all mapped hashes stay exact.
func TestFinalizedMappingFinalityRejectsRegressionDuringEvmRead(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.after = func(method string, _ []any) {
		if method == "debug_getRawHeader" {
			f.head = 90
		}
	}
	mapping, err := f.client.readFinalizedMappingAtIdentity(t.Context(), identity)
	if err == nil || mapping.Schema != "" {
		t.Fatalf("mapping100 survived finalized150 ->90: mapping=%+v err=%v", mapping, err)
	}
}

// Even a still-covered selected block cannot hide replacement of the original
// finalized witness while the EVM read is in progress.
func TestFinalizedMappingFinalityRetainsOpeningWitness(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.after = func(method string, _ []any) {
		if method == "debug_getRawHeader" {
			f.head, f.canonicalKVs[150] = 180, testGenesisHash
		}
	}
	mapping, err := f.client.readFinalizedMappingAtIdentity(t.Context(), identity)
	if !errors.Is(err, errRpcIntegrity) || mapping.Schema != "" {
		t.Fatalf("mapping100 lost opening finalized150: mapping=%+v err=%v", mapping, err)
	}
}

// A historical in-memory identity is not permanent finalized-read authority.
// A fresh head below it must refuse before reading any EVM artifact.
func TestFinalizedMappingFinalityReopensBeforeEvmRead(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.head = 90
	mapping, err := f.client.readFinalizedMappingAtIdentity(t.Context(), identity)
	if err == nil || mapping.Schema != "" || f.counts["debug_getRawHeader"] != 0 {
		t.Fatalf("stale identity authorized EVM read: mapping=%+v reads=%v err=%v", mapping, f.counts, err)
	}
}

// Native150 ->180 never rewrites the selected native100/EVM37 mapping.
func TestFinalizedMappingFinalityAdvancePreservesBothClocks(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.after = func(method string, _ []any) {
		if method == "debug_getRawHeader" {
			f.head = 180
		}
	}
	mapping, err := f.client.readFinalizedMappingAtIdentity(t.Context(), identity)
	if err != nil || mapping.Identity != identity || mapping.EvmHeader.Number != 37 || mapping.EvmHeader.Hash != f.mapping.evmHash {
		t.Fatalf("normal advance replaced selected native/EVM point: %+v %v", mapping, err)
	}
}

// Cancellation during the last closing read must remove the complete candidate,
// even if a custom transport supplies a successful response after cancellation.
func TestIdentityFinalityCancellationCannotPublishSnapshot(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.after = func(method string, _ []any) {
		if method == "chain_getFinalizedHead" && f.counts[method] == 2 {
			cancel()
		}
	}
	identity, err := f.client.readIdentityAt(ctx, f.mapping.nativeHash)
	if !errors.Is(err, context.Canceled) || identity != (chainIdentity{}) {
		t.Fatalf("canceled closing finality published identity: %+v %v", identity, err)
	}
}

// Each closing RPC keeps its existing bounded retry cause. A single overload
// does not fabricate a changed canonical block or restart the sample deadline.
func TestIdentityFinalityRetriesTransientClosingReads(t *testing.T) {
	for _, method := range []string{"chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash"} {
		f := newIdentityFinalityFixture(t)
		closing, failed, retries := false, false, 0
		f.after = func(observed string, _ []any) { closing = closing || observed == "state_getRuntimeVersion" }
		transport := f.client.httpClient.Transport
		f.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
			var call struct{ Method string }
			if err := json.Unmarshal(raw, &call); err != nil {
				return nil, err
			}
			if closing && call.Method == method && !failed {
				failed = true
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: request}, nil
			}
			return transport.RoundTrip(request)
		})
		f.client.retryWait = func(ctx context.Context, _ time.Duration) error { retries++; return ctx.Err() }
		identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
		if err != nil || identity.FinalizedNumber != 100 || !failed || retries != 1 {
			t.Errorf("%s transient closing failure changed evidence/cause: %+v failed=%t retries=%d err=%v", method, identity, failed, retries, err)
		}
	}
}

// Cancellation at the retry barrier preserves the transport incident and
// leaves no identity; it is never promoted to a stable canonical mismatch.
func TestIdentityFinalityClosingOutageRetainsTransportCause(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closing := false
	f.after = func(method string, _ []any) { closing = closing || method == "state_getRuntimeVersion" }
	transport := f.client.httpClient.Transport
	outage := errors.New("synthetic finality route outage")
	f.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if closing {
			return nil, outage
		}
		return transport.RoundTrip(request)
	})
	f.client.retryWait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	identity, err := f.client.readIdentityAt(ctx, f.mapping.nativeHash)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, outage) || errors.Is(err, errRpcIntegrity) || identity != (chainIdentity{}) {
		t.Fatalf("closing transport outage was reclassified or published: %+v %v", identity, err)
	}
}

// Runtime artifact collection is independently callable, so its finality
// closure cannot depend on a later caller choosing the mapping reader.
func TestRuntimeSnapshotFinalityRejectsRegressionAfterMetadata(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.after = func(method string, _ []any) {
		if method == "state_getMetadata" {
			f.head = 90
		}
	}
	snapshot, err := f.client.readRuntimeSnapshotAtIdentity(t.Context(), identity)
	if !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || snapshot.Schema != "" || f.counts["state_getMetadata"] != 1 {
		t.Fatalf("unfinalized runtime bytes were published: %+v %v", snapshot, err)
	}
}

// Private finality state survives live composition but not JSON import. Its
// presence changes neither old identity wire bytes nor their retained seals.
func TestIdentityFinalityWitnessRemainsPrivateAndRequired(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	without := identity
	without.finalityWitness = nativeFinalityPoint{}
	if rootObjectHash(identity) != rootObjectHash(without) {
		t.Fatal("private finality changed original observation bytes")
	}
	if err := f.client.closeSnapshotFinality(t.Context(), without); !errors.Is(err, errRpcIntegrity) {
		t.Fatalf("missing original live witness was reconstructed as authority: %v", err)
	}
}

// A thread-safe transport barrier preserves the selected canonical100 while
// reporting independently encoded finalized90 after the chosen read completes.
func installNativeFinalityRegression(t *testing.T, client *rpcClient, trigger func(string, []any) bool) *atomic.Bool {
	t.Helper()
	client.retryWait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	header, hash := rootReceiptHeaderFixture(t, testGenesisHash, 90, nil, false)
	regressed := &atomic.Bool{}
	transport := client.httpClient.Transport
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		var call struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		if regressed.Load() {
			switch {
			case call.Method == "chain_getFinalizedHead":
				return ownerTrimTestReply(hash)
			case call.Method == "chain_getHeader" && len(call.Params) == 1 && call.Params[0] == hash:
				return ownerTrimTestReply(header)
			case call.Method == "chain_getBlockHash" && len(call.Params) == 1 && call.Params[0] == float64(90):
				return ownerTrimTestReply(hash)
			}
		}
		response, err := transport.RoundTrip(request)
		if err == nil && trigger(call.Method, call.Params) {
			regressed.Store(true)
		}
		return response, err
	})
	return regressed
}

// The passive-root and independent root preview consume this same production
// reader. Finality loss after storage cannot produce ready root prerequisites.
func TestRootPreviewFinalityClosesAfterStorage(t *testing.T) {
	client, fixture := newRootFixture(t)
	regressed := installNativeFinalityRegression(t, client, func(method string, _ []any) bool { return method == "state_getStorage" })
	preview, err := client.readRootPreview(t.Context(), fixture.policy, "synthetic-policy")
	if !regressed.Load() || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || preview.Schema != "" || preview.ReadOnlyReady {
		t.Fatalf("root storage outlived finality: %+v %v", preview, err)
	}
}

// The owner-trim/bootstrap census has no mandatory later EVM mapping call.
func TestSubnetPreviewFinalityClosesAfterStorage(t *testing.T) {
	client, _, policy := newSubnetFixture(t)
	regressed := installNativeFinalityRegression(t, client, func(method string, _ []any) bool { return method == "state_getStorage" })
	preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy")
	if !regressed.Load() || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || preview.Schema != "" || preview.CensusComplete {
		t.Fatalf("subnet storage outlived finality: %+v %v", preview, err)
	}
}

// Recycle/Burn state is another independently published bootstrap prerequisite.
func TestRecycleModeFinalityClosesAfterStorage(t *testing.T) {
	mode := "0x01"
	client, policy, _ := newRecycleTestClient(t, &mode, nil)
	regressed := installNativeFinalityRegression(t, client, func(method string, _ []any) bool { return method == "state_getStorage" })
	observation, err := client.readRecycleMode(t.Context(), policy, "synthetic-policy")
	if !regressed.Load() || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || observation.Schema != "" || observation.ModeGatePassed {
		t.Fatalf("Recycle mode outlived finality: %+v %v", observation, err)
	}
}

// Unapproved discovery still cannot claim a complete finalized membership
// after its original snapshot loses finality during the physical census.
func TestSubnetDiscoveryFinalityClosesAfterStorage(t *testing.T) {
	client, _, _, snapshot := newSubnetDiscoveryFixture(t)
	regressed := installNativeFinalityRegression(t, client, func(method string, _ []any) bool { return method == "state_queryStorageAt" })
	observation, err := client.readSubnetDiscovery(t.Context(), snapshot, rootObjectHash(snapshot))
	if !regressed.Load() || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || observation.Schema != "" || observation.MembershipComplete {
		t.Fatalf("discovery membership outlived finality: %+v %v", observation, err)
	}
}

// The additional original root checkpoint follows the completed subnet census.
// Lose finality only at that read, then require unresolved roles and exact custody.
func TestBootstrapChainReadinessFinalityClosesAfterRootCheckpoint(t *testing.T) {
	f := newBootstrapChainReadinessFixture(t)
	original := f.journals(t)
	heads, afterCensus := 0, 0
	regressed := installNativeFinalityRegression(t, f.client, func(method string, params []any) bool {
		if method == "chain_getFinalizedHead" {
			heads++
		}
		if heads == 4 && method == "chain_getBlockHash" && params[0] == float64(100) {
			afterCensus++
			// Two checks close the census, then the original root checkpoint.
			return afterCensus == 3
		}
		return false
	})
	result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
	if !regressed.Load() || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || result.ObservationComplete || result.Census != nil || result.Status != "unresolved" || !reflect.DeepEqual(original, f.journals(t)) {
		t.Fatalf("late finality loss published readiness or changed custody: heads=%d checks=%d result=%+v err=%v", heads, afterCensus, result, err)
	}
}

// The native-only admission command does not require a later EVM observation.
// Its final historical drain read must not preserve current eligibility after
// a finalized-head regression, even when both canonical anchors remain exact.
func TestValidatorActivationNativeFinalityClosesAfterCheckpointStorage(t *testing.T) {
	f := newValidatorActivationFixture(t)
	readiness := validatorActivationNativeTestReadiness(t, f)
	key, err := types.CreateStorageKey(f.chain.census.metadata, "SubtensorModule", "PendingServerEmission", []byte{25, 0})
	if err != nil {
		t.Fatal(err)
	}
	regressed := installNativeFinalityRegression(t, f.chain.client, func(method string, params []any) bool {
		return method == "state_getStorage" && params[0] == key.Hex()
	})
	result, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
	if !regressed.Load() || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || result != nil {
		t.Fatalf("native checkpoint storage outlived finality: %+v %v", result, err)
	}
}

// Stake-only admission likewise closes its own finality after both weighted
// census and the final activity vector, without relying on current-start code.
func TestValidatorActivationStakeFinalityClosesAfterActivityStorage(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	activation := f.activation
	readiness := validatorActivationNativeTestReadiness(t, activation)
	prior, err := activation.chain.client.observeValidatorActivationNative(t.Context(), activation.chain.preparation, readiness)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newOwnedSubmissionClient(activation.approval.Plan.Route)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	key, err := types.CreateStorageKey(activation.chain.census.metadata, "SubtensorModule", "LastUpdate", []byte{25, 0})
	if err != nil {
		t.Fatal(err)
	}
	regressed := installNativeFinalityRegression(t, client, func(method string, params []any) bool {
		return method == "state_getStorage" && params[0] == key.Hex()
	})
	result, err := client.observeValidatorActivationStake(t.Context(), activation.chain.preparation, readiness, prior)
	if !regressed.Load() || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || result != nil {
		t.Fatalf("stake activity storage outlived finality: %+v %v", result, err)
	}
}
