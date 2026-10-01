//go:build linux || darwin

// Real ethclient requests and genuine signed responses exercise decision
// ownership. Barriers control transport races, never authentication verdicts.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// A single actual request is held until the test releases it or its real
// transport context is cancelled. No timeout or sleep proves the ordering.
type releaseClientKeyAuthorityV2RpcBarrier struct {
	method  string
	entered chan struct{}
	release chan struct{}
	left    chan struct{}
}

// Fixture state is owned under one lock; response generation and barriers run
// outside it. Each method is counted as one actual provider work item.
type releaseClientKeyAuthorityV2RpcFixture struct {
	stateLock     sync.Mutex
	artifacts     map[common.Hash]ReleaseMeasurementArtifact
	canonicalKVs  map[uint64]common.Hash
	finalized     common.Hash
	methodCounts  map[string]int
	failMethod    string
	barrier       *releaseClientKeyAuthorityV2RpcBarrier
	nativeGenesis any
}

// Actual batches retain each separately counted member and reverse result
// order to exercise the concrete Rpc client's identifier matching.
func (self *releaseClientKeyAuthorityV2RpcFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(request.Body).Decode(&raw); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	reply := func(call chainBatchRPCRequest) map[string]any {
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
		value, err := self.reply(request.Context(), call)
		if err != nil {
			response["error"] = map[string]any{"code": -32000, "message": err.Error()}
		} else {
			response["result"] = value
		}
		return response
	}
	writer.Header().Set("Content-Type", "application/json")
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
		var calls []chainBatchRPCRequest
		if json.Unmarshal(raw, &calls) != nil || len(calls) == 0 || len(calls) > stabi.MaxClientKeyAuthorityRpcBatchMembers {
			http.Error(writer, "invalid bounded batch", 400)
			return
		}
		responses := make([]map[string]any, len(calls))
		for index, call := range calls {
			responses[len(calls)-1-index] = reply(call)
		}
		_ = json.NewEncoder(writer).Encode(responses)
		return
	}
	var call chainBatchRPCRequest
	if err := json.Unmarshal(raw, &call); err != nil {
		http.Error(writer, err.Error(), 400)
		return
	}
	_ = json.NewEncoder(writer).Encode(reply(call))
}

// Independent ABI values come from the existing real key-history transport
// fixture, with two concrete canonical boundaries and a mutable failure source.
func (self *releaseClientKeyAuthorityV2RpcFixture) reply(ctx context.Context, call chainBatchRPCRequest) (any, error) {
	var artifact ReleaseMeasurementArtifact
	var canonical map[uint64]common.Hash
	var artifacts map[common.Hash]ReleaseMeasurementArtifact
	var barrier *releaseClientKeyAuthorityV2RpcBarrier
	var failed bool
	var genesis any
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.methodCounts[call.Method]++
		artifact = self.artifacts[self.finalized]
		artifacts = make(map[common.Hash]ReleaseMeasurementArtifact, len(self.artifacts))
		for hash, value := range self.artifacts {
			artifacts[hash] = value
		}
		canonical = make(map[uint64]common.Hash, len(self.canonicalKVs))
		for height, hash := range self.canonicalKVs {
			canonical[height] = hash
		}
		genesis = self.nativeGenesis
		if self.failMethod == call.Method {
			self.failMethod, failed = "", true
		}
		if self.barrier != nil && self.barrier.method == call.Method {
			barrier, self.barrier = self.barrier, nil
		}
	}()
	if barrier != nil {
		close(barrier.entered)
		select {
		case <-ctx.Done():
			close(barrier.left)
			return nil, ctx.Err()
		case <-barrier.release:
			close(barrier.left)
		}
	}
	if failed {
		return nil, errors.New("test-owned transient source failure")
	}
	switch call.Method {
	case "chain_getBlockHash":
		return genesis, nil
	case "eth_getBlockByNumber":
		var tag string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &tag) != nil {
			return nil, errors.New("incomplete exact block request")
		}
		if tag == "finalized" {
			return map[string]any{"number": hexutil.EncodeUint64(artifact.EVMSnapshotBlock), "hash": artifact.EVMSnapshotHash}, nil
		}
		number, err := hexutil.DecodeUint64(tag)
		if err != nil {
			return nil, err
		}
		hash, found := canonical[number]
		if !found {
			return nil, nil
		}
		return map[string]any{"number": hexutil.EncodeUint64(number), "hash": hash}, nil
	case "eth_getBlockByHash":
		var hash common.Hash
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &hash) != nil {
			return nil, errors.New("incomplete historical block request")
		}
		value, found := artifacts[hash]
		if !found {
			return nil, nil
		}
		return map[string]any{"number": hexutil.EncodeUint64(value.EVMSnapshotBlock), "hash": value.EVMSnapshotHash}, nil
	case "eth_call":
		var selector gethrpc.BlockNumberOrHash
		if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil || selector.BlockHash == nil || !selector.RequireCanonical {
			return nil, errors.New("authority call has no exact canonical selector")
		}
		var found bool
		artifact, found = artifacts[*selector.BlockHash]
		if !found || canonical[artifact.EVMSnapshotBlock] != *selector.BlockHash {
			return nil, errors.New("authority call is not canonical")
		}
	}
	value, handled, err := releaseHeadV2ClientKeyRPC(&artifact, call)
	if !handled {
		return nil, errors.New("unexpected authority fixture method")
	}
	return value, err
}

// Counter snapshots do not supply acceptance or replace any production read.
func (self *releaseClientKeyAuthorityV2RpcFixture) counts() map[string]int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	counts := make(map[string]int, len(self.methodCounts))
	for method, count := range self.methodCounts {
		counts[method] = count
	}
	return counts
}

// These tiny source identities need no M8 reconstruction. The separate actual
// gather regression below joins the same reader to full record/proof replay.
type releaseClientKeyAuthorityV2TestFixture struct {
	cfg      ReleaseConfig
	artifact ReleaseMeasurementArtifact
	hotkey   [32]byte
	domain   protocol.ClientKeyHistoryDomain
	request  protocol.ClientKeyObservationRequest
	prior    protocol.ClientKeyEffectiveBoundary
	rpc      *releaseClientKeyAuthorityV2RpcFixture
	chain    *ChainClient
}

// Current reviewed runtime values are read from the existing config fixture;
// the transport's two historical Evm boundaries are explicit test-owned facts.
func newReleaseClientKeyAuthorityV2TestFixture(t *testing.T) *releaseClientKeyAuthorityV2TestFixture {
	t.Helper()
	cfg := validReleaseConfig(t)
	artifact := ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchemaV2, DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID,
		GenesisHash: cfg.GenesisHash, Coordinator: cfg.Coordinator, SettlementVault: cfg.SettlementVault,
		ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, PolicyHash: cfg.PolicyHash, Policy: cfg.Policy,
		NativeSnapshotBlock: 100, NativeSnapshotHash: releaseHex32([32]byte{0x51}), SubnetEpoch: 9,
		EVMSnapshotBlock: 100, EVMSnapshotHash: releaseHex32([32]byte{0x61}), SettlementEpoch: 9, SelfUID: 7,
	}
	hotkey := [32]byte{0x71}
	domain, request, err := releaseClientKeyDecisionV2(&cfg, cfg.Operators[0].NoID, hotkey, &artifact, connect.Id{0x81})
	if err != nil {
		t.Fatal(err)
	}
	request.Nonce = [32]byte{0x91}
	prior := artifact
	prior.EVMSnapshotBlock, prior.EVMSnapshotHash, prior.SettlementEpoch = 99, releaseHex32([32]byte{0x62}), 8
	rpc := &releaseClientKeyAuthorityV2RpcFixture{
		artifacts:    map[common.Hash]ReleaseMeasurementArtifact{common.HexToHash(artifact.EVMSnapshotHash): artifact, common.HexToHash(prior.EVMSnapshotHash): prior},
		canonicalKVs: map[uint64]common.Hash{artifact.EVMSnapshotBlock: common.HexToHash(artifact.EVMSnapshotHash), prior.EVMSnapshotBlock: common.HexToHash(prior.EVMSnapshotHash)},
		finalized:    common.HexToHash(artifact.EVMSnapshotHash), methodCounts: map[string]int{}, nativeGenesis: cfg.GenesisHash,
	}
	server := httptest.NewServer(rpc)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	chain := &ChainClient{client: client, coordinator: stabi.NewSTCoordinator(), chainId: new(big.Int).SetUint64(cfg.ChainID), contractAddr: common.HexToAddress(cfg.Coordinator), release: true}
	return &releaseClientKeyAuthorityV2TestFixture{cfg: cfg, artifact: artifact, hotkey: hotkey, domain: domain, request: request, prior: protocol.ClientKeyEffectiveBoundary{Epoch: prior.SettlementEpoch, Block: prior.EVMSnapshotBlock, Hash: common.HexToHash(prior.EVMSnapshotHash)}, rpc: rpc, chain: chain}
}

// Every test owns one explicit control allowance and lifecycle.
func (self *releaseClientKeyAuthorityV2TestFixture) owner(t *testing.T, parent context.Context, maximum uint64) (context.Context, *releaseClientKeyAuthorityV2Reads) {
	t.Helper()
	budget := &releaseHeadV2Budget{limit: maximum}
	ctx, owner, err := newReleaseClientKeyAuthorityV2Reads(parent, self.chain, &self.cfg, &self.artifact, self.hotkey, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		owner.stateLock.Lock()
		closed := owner.closed
		owner.stateLock.Unlock()
		if !closed {
			_ = owner.finish(context.Canceled)
		}
	})
	return ctx, owner
}

// Every distinct client still arrives over the actual authenticated Http API
// and is independently signature checked, while one boundary needs 12 getters.
func TestReleaseClientKeyAuthorityV2SharesExactBoundaryWithIndependentSignatures(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
	for index := byte(1); index <= 3; index++ {
		request := fixture.request
		request.ClientID[1] = index
		encoded, err := reader.Read(ctx, request, releaseClientKeyHistoryTestResponseBytes)
		if err != nil {
			t.Fatal(err)
		}
		registration, err := verifyReleaseClientKeyCaptureV2(ctx, fixture.chain, encoded, releaseClientKeyHistoryTestResponseBytes, fixture.domain, request, false)
		if err != nil || registration.ClientID != request.ClientID || registration.PublicKey != ([32]byte{0x31}) {
			t.Fatalf("real signed client response was not independently authenticated: %v", err)
		}
	}
	if calls := fixture.rpc.counts()["eth_call"]; calls != 12 {
		t.Fatalf("same exact authority boundary caused %d getter calls, want 12", calls)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
	counts := fixture.rpc.counts()
	if counts["eth_chainId"] != 2 || counts["chain_getBlockHash"] != 2 || counts["eth_getBlockByNumber"] != 3 {
		t.Fatalf("final independent network/canonical witness was omitted: %v", counts)
	}
}

// A successful root lookup cannot admit a forged later client signature.
func TestReleaseClientKeyAuthorityV2CacheHitDoesNotAuthenticateForgedClient(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	encoded := releaseClientKeyTestResponse(t, fixture.cfg.DeploymentID, fixture.domain, fixture.request, [][32]byte{{0x31}})
	if _, err := verifyReleaseClientKeyCaptureV2(ctx, fixture.chain, encoded, releaseClientKeyHistoryTestResponseBytes, fixture.domain, fixture.request, false); err != nil {
		t.Fatal(err)
	}
	response, err := protocol.DecodeClientKeyHistoryResponse(encoded, releaseClientKeyHistoryTestResponseBytes)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := protocol.DecodeClientKeyEvidence(response.History[0], fixture.domain, protocol.ClientKeyRegistrationEvidenceKind)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := protocol.DecodeClientKeyRegistration(wrapper.Payload)
	if err != nil {
		t.Fatal(err)
	}
	registration.Signature[0] ^= 1
	payload, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	response.History[0] = releaseClientKeyTestEnvelope(t, fixture.domain, fixture.cfg.DeploymentID, protocol.ClientKeyRegistrationEvidenceKind, payload)
	changed, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifyReleaseClientKeyCaptureV2(ctx, fixture.chain, changed, releaseClientKeyHistoryTestResponseBytes, fixture.domain, fixture.request, false)
	if err == nil || got != (protocol.ClientKeyRegistration{}) || bytes.Equal(changed, encoded) || fixture.rpc.counts()["eth_call"] != 12 {
		t.Fatalf("cached root authenticated changed signed bytes: %v", err)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
}

// Each operator and historical boundary costs another complete real read.
func TestReleaseClientKeyAuthorityV2KeepsOperatorAndBoundaryDomainsSeparate(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	domains := []protocol.ClientKeyHistoryDomain{fixture.domain, fixture.domain}
	domains[1].NoID = fixture.cfg.Operators[1].NoID
	for _, domain := range domains {
		for _, boundary := range []protocol.ClientKeyEffectiveBoundary{fixture.prior, fixture.request.DecisionBoundary} {
			for index := 0; index < 2; index++ {
				signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, domain, boundary)
				if err != nil || signer == (common.Address{}) {
					t.Fatalf("independent boundary or operator lost actual authority: %v", err)
				}
			}
		}
	}
	if calls := fixture.rpc.counts()["eth_call"]; calls != 4*12 {
		t.Fatalf("unique operator/boundary work was hidden or duplicated: %d", calls)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
	if counts := fixture.rpc.counts(); counts["eth_getBlockByNumber"] != 7 || counts["eth_chainId"] != 5 {
		t.Fatalf("final witness failed to cover exactly two canonical boundaries: %v", counts)
	}
}

// Foreign configuration, future boundaries and concrete transport substitution
// are refused before a cache lookup can confer any authority.
func TestReleaseClientKeyAuthorityV2RejectsChangedDomainDecisionAndTransport(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	encoded := releaseClientKeyTestResponse(t, fixture.cfg.DeploymentID, fixture.domain, fixture.request, [][32]byte{{0x31}})
	if _, err := verifyReleaseClientKeyCaptureV2(ctx, fixture.chain, encoded, releaseClientKeyHistoryTestResponseBytes, fixture.domain, fixture.request, false); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*protocol.ClientKeyHistoryDomain){
		func(domain *protocol.ClientKeyHistoryDomain) { domain.ChainID++ },
		func(domain *protocol.ClientKeyHistoryDomain) { domain.GenesisHash[0] ^= 1 },
		func(domain *protocol.ClientKeyHistoryDomain) { domain.Coordinator[0] ^= 1 },
		func(domain *protocol.ClientKeyHistoryDomain) { domain.SettlementVault[0] ^= 1 },
		func(domain *protocol.ClientKeyHistoryDomain) { domain.PolicyHash[0] ^= 1 },
		func(domain *protocol.ClientKeyHistoryDomain) { domain.DeploymentIDHash[0] ^= 1 },
		func(domain *protocol.ClientKeyHistoryDomain) { domain.Netuid++ },
		func(domain *protocol.ClientKeyHistoryDomain) { domain.NoID = 99 },
	} {
		domain := fixture.domain
		mutate(&domain)
		if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, domain, fixture.request.DecisionBoundary); err == nil || signer != (common.Address{}) {
			t.Fatalf("changed domain acquired authority: %+v error=%v", domain, err)
		}
	}
	for _, mutate := range []func(*protocol.ClientKeyObservationRequest){
		func(request *protocol.ClientKeyObservationRequest) { request.NativeHash[0] ^= 1 },
		func(request *protocol.ClientKeyObservationRequest) { request.NativeBlock++ },
		func(request *protocol.ClientKeyObservationRequest) { request.NativeEpoch++ },
		func(request *protocol.ClientKeyObservationRequest) { request.ValidatorHotkey[0] ^= 1 },
		func(request *protocol.ClientKeyObservationRequest) { request.DecisionBoundary.Hash[0] ^= 1 },
	} {
		request := fixture.request
		mutate(&request)
		if got, err := verifyReleaseClientKeyCaptureV2(ctx, fixture.chain, encoded, releaseClientKeyHistoryTestResponseBytes, fixture.domain, request, false); err == nil || got != (protocol.ClientKeyRegistration{}) {
			t.Fatalf("changed native decision acquired authority: %v", err)
		}
	}
	foreign := &ChainClient{client: fixture.chain.client, coordinator: fixture.chain.coordinator, chainId: new(big.Int).Set(fixture.chain.chainId), contractAddr: fixture.chain.contractAddr, release: true}
	if signer, err := foreign.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary); err == nil || signer != (common.Address{}) {
		t.Fatalf("foreign concrete owner acquired cached authority: %v", err)
	}
	future := fixture.request.DecisionBoundary
	future.Block++
	if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, future); err == nil || signer != (common.Address{}) {
		t.Fatalf("future boundary acquired cached authority: %v", err)
	}
	if fixture.rpc.counts()["eth_call"] != 12 {
		t.Fatal("invalid routing reached the provider")
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
}

// No successful result or failure is reused by another runtime or decision
// owner. Unscoped callers retain their strict fresh-read behavior.
func TestReleaseClientKeyAuthorityV2NewRuntimeAndUnownedCallsReadFreshSources(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	for index := 0; index < 2; index++ {
		ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
		if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary); err != nil || signer == (common.Address{}) {
			t.Fatal(err)
		}
		if index == 0 {
			fixture.cfg.RuntimeCodeHash = releaseHex32([32]byte{0x39})
			fixture.cfg.RuntimeMetadataHash = releaseHex32([32]byte{0x49})
			fixture.cfg.RuntimeSpec++
		}
		if err := owner.finish(nil); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 2; index++ {
		if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(t.Context(), fixture.domain, fixture.request.DecisionBoundary); err != nil || signer == (common.Address{}) {
			t.Fatal(err)
		}
	}
	if calls := fixture.rpc.counts()["eth_call"]; calls != 4*12 {
		t.Fatalf("fresh runtime/strict caller inherited another owner's cached result: %d", calls)
	}
}

// An error has no retained authority. A subsequent attempt must issue all
// twelve actual getters; failed work still consumes its original byte budget.
func TestReleaseClientKeyAuthorityV2FailedReadCannotPoisonRetry(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	fixture.rpc.failMethod = "eth_call"
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary); err == nil || signer != (common.Address{}) {
		t.Fatalf("failed read returned authority: %v", err)
	}
	failedUsed := owner.budget.used
	if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary); err != nil || signer == (common.Address{}) {
		t.Fatalf("fresh actual retry inherited failed-read state: %v", err)
	}
	if calls := fixture.rpc.counts()["eth_call"]; calls != 13 || owner.budget.used <= failedUsed {
		t.Fatalf("failed attempt was cached, hidden or refunded: calls=%d used=%d failed=%d", calls, owner.budget.used, failedUsed)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
}

// A final immutable-height check observes a reorg even if every subsequent
// authority lookup itself hits the successful decision-local cache.
func TestReleaseClientKeyAuthorityV2FinalWitnessRejectsChangedCanonicalBoundary(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.prior); err != nil || signer == (common.Address{}) {
		t.Fatal(err)
	}
	fixture.rpc.stateLock.Lock()
	fixture.rpc.canonicalKVs[fixture.prior.Block] = common.Hash{0xee}
	fixture.rpc.stateLock.Unlock()
	if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.prior); err != nil || signer == (common.Address{}) {
		t.Fatal("exact cached value disappeared before its required final witness")
	}
	if err := owner.finish(nil); err == nil {
		t.Fatal("changed historical canonical height retained decision authority")
	}
	if fixture.rpc.counts()["eth_call"] != 12 {
		t.Fatal("final witness substituted another authority lookup for canonicality")
	}
	if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.prior); err == nil || signer != (common.Address{}) {
		t.Fatalf("closed owner continued serving authority: %v", err)
	}
}

// Null/unavailable native identity cannot fall back to the unrelated Evm
// genesis method, either on a miss or during the required final witness.
func TestReleaseClientKeyAuthorityV2RejectsUnavailableNativeIdentityAtBothBoundaries(t *testing.T) {
	t.Parallel()
	for _, final := range []bool{false, true} {
		for _, source := range []struct {
			genesis     any
			unavailable bool
		}{
			{genesis: nil},
			{genesis: releaseHex32([32]byte{0xee})},
			{unavailable: true},
		} {
			fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
			ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
			if final {
				if _, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary); err != nil {
					t.Fatal(err)
				}
			}
			fixture.rpc.stateLock.Lock()
			if source.unavailable {
				fixture.rpc.failMethod = "chain_getBlockHash"
			} else {
				fixture.rpc.nativeGenesis = source.genesis
			}
			fixture.rpc.stateLock.Unlock()
			if final {
				if err := owner.finish(nil); err == nil {
					t.Fatal("final native identity disappearance was accepted")
				}
			} else {
				if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary); err == nil || signer != (common.Address{}) {
					t.Fatalf("missing native identity acquired initial authority: %v", err)
				}
				_ = owner.finish(context.Canceled)
			}
		}
	}
}

// Context instrumentation reports entry to the actual wait select. It does
// not fake cancellation, any transport response, signature or verifier result.
type releaseClientKeyAuthorityV2WaitingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

// Each select sees the original context's actual cancellation channel.
func (self *releaseClientKeyAuthorityV2WaitingContext) Done() <-chan struct{} {
	self.once.Do(func() { close(self.waiting) })
	return self.Context.Done()
}

// One network barrier forces both callers onto the same unfinished authority.
func TestReleaseClientKeyAuthorityV2ConcurrentReadersShareOneActualRequestSet(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	barrier := &releaseClientKeyAuthorityV2RpcBarrier{method: "eth_call", entered: make(chan struct{}), release: make(chan struct{}), left: make(chan struct{})}
	fixture.rpc.barrier = barrier
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	results := make(chan error, 2)
	read := func(readCtx context.Context) {
		signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(readCtx, fixture.domain, fixture.request.DecisionBoundary)
		if err == nil && signer == (common.Address{}) {
			err = errors.New("successful read returned no signer")
		}
		results <- err
	}
	go read(ctx)
	<-barrier.entered
	waiting := &releaseClientKeyAuthorityV2WaitingContext{Context: ctx, waiting: make(chan struct{})}
	go read(waiting)
	<-waiting.waiting
	close(barrier.release)
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls := fixture.rpc.counts()["eth_call"]; calls != 12 {
		t.Fatalf("concurrent exact-boundary readers performed %d getters, want 12", calls)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
}

// Cancelling an admitted waiter cannot cancel the concrete leader's network
// work or grant the cancelled caller authority after the leader succeeds.
func TestReleaseClientKeyAuthorityV2CancelledWaiterDoesNotCancelLeader(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	barrier := &releaseClientKeyAuthorityV2RpcBarrier{method: "eth_call", entered: make(chan struct{}), release: make(chan struct{}), left: make(chan struct{})}
	fixture.rpc.barrier = barrier
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	leader := make(chan error, 1)
	go func() {
		signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary)
		if err == nil && signer == (common.Address{}) {
			err = errors.New("leader lost actual authority")
		}
		leader <- err
	}()
	<-barrier.entered
	waitCtx, cancel := context.WithCancel(ctx)
	waiting := &releaseClientKeyAuthorityV2WaitingContext{Context: waitCtx, waiting: make(chan struct{})}
	waiter := make(chan error, 1)
	go func() {
		signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(waiting, fixture.domain, fixture.request.DecisionBoundary)
		if signer != (common.Address{}) {
			err = errors.New("cancelled waiter acquired authority")
		}
		waiter <- err
	}()
	<-waiting.waiting
	cancel()
	if err := <-waiter; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter cancellation was not its terminal outcome: %v", err)
	}
	close(barrier.release)
	if err := <-leader; err != nil {
		t.Fatal(err)
	}
	if fixture.rpc.counts()["eth_call"] != 12 {
		t.Fatal("waiter cancellation restarted or duplicated concrete authority reads")
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
}

// Closing the owner cancels the real in-flight request and joins its reader
// before return. No caller can retain the unfinished root as an acceptance.
func TestReleaseClientKeyAuthorityV2OwnerCancellationJoinsRealTransport(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	barrier := &releaseClientKeyAuthorityV2RpcBarrier{method: "eth_call", entered: make(chan struct{}), release: make(chan struct{}), left: make(chan struct{})}
	fixture.rpc.barrier = barrier
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	result := make(chan error, 1)
	go func() {
		signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary)
		if signer != (common.Address{}) {
			err = errors.New("unfinished read acquired authority")
		}
		result <- err
	}()
	<-barrier.entered
	if err := owner.finish(nil); err == nil {
		t.Fatal("unfinished authority owner closed successfully")
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("actual authority reader was not joined at cancellation: %v", err)
	}
	<-barrier.left
	if fixture.rpc.counts()["eth_call"] != 1 {
		t.Fatal("cancelled owner continued its getter sequence")
	}
}

// Exact aggregate response and map-entry ownership share the caller's control
// limit; no response can consume a fresh implicit allowance on each client.
func TestReleaseClientKeyAuthorityV2UsesOneResponseAndEntryControlBudget(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	encoded := releaseClientKeyTestResponse(t, fixture.cfg.DeploymentID, fixture.domain, fixture.request, [][32]byte{{0x31}})
	remaining := uint64(8*len(encoded) - 1)
	owner.budget.limit = owner.budget.used + remaining
	if got, err := verifyReleaseClientKeyCaptureV2(ctx, fixture.chain, encoded, releaseClientKeyHistoryTestResponseBytes, fixture.domain, fixture.request, false); err == nil || got != (protocol.ClientKeyRegistration{}) {
		t.Fatalf("response exceeded its remaining shared control allowance: %v", err)
	}
	if len(fixture.rpc.counts()) != 0 {
		t.Fatal("over-budget response reached real authority reads")
	}
	maximum, err := releaseClientKeyCaptureV2Maximum(ctx, releaseClientKeyHistoryTestResponseBytes)
	if err != nil || maximum != uint64(len(encoded)-1) {
		t.Fatalf("live/retained reader ignored its remaining response bound: maximum=%d error=%v", maximum, err)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
	budget := &releaseHeadV2Budget{limit: 1}
	if _, _, err := newReleaseClientKeyAuthorityV2Reads(t.Context(), fixture.chain, &fixture.cfg, &fixture.artifact, fixture.hotkey, budget); err == nil {
		t.Fatal("owner allocated a map without its fixed control allowance")
	}
}

// A cached fixed value needs no new allocation, while a new authority key
// cannot bypass the same exhausted entry allowance to issue provider work.
func TestReleaseClientKeyAuthorityV2EntryBudgetRefusesNewBoundaryBeforeIo(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	if _, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary); err != nil {
		t.Fatal(err)
	}
	owner.budget.limit = owner.budget.used
	if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary); err != nil || signer == (common.Address{}) {
		t.Fatalf("completed immutable lookup allocated new control storage: %v", err)
	}
	if signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.prior); err == nil || signer != (common.Address{}) {
		t.Fatalf("new historical boundary ignored exhausted control storage: %v", err)
	}
	if calls := fixture.rpc.counts()["eth_call"]; calls != 12 {
		t.Fatalf("over-budget unique boundary reached the provider: %d", calls)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
}

// The capture consumer groups by the actual retained intent, not by a global
// history cache. A second independent pass must repeat the real source reads.
func TestReleaseClientKeyAuthorityV2CapturedIntentUsesScopedIndependentReads(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	var responses []releaseClientKeyCapturedV2Response
	for index := byte(1); index <= 3; index++ {
		request := fixture.request
		request.ClientID[1] = index
		encoded := releaseClientKeyTestResponse(t, fixture.cfg.DeploymentID, fixture.domain, request, [][32]byte{{0x31}})
		responses = append(responses, releaseClientKeyCapturedV2Response{encoded: encoded, maximum: releaseClientKeyHistoryTestResponseBytes, domain: fixture.domain, request: request})
	}
	for index := 0; index < 2; index++ {
		if err := verifyReleaseClientKeyCapturedResponsesV2(t.Context(), fixture.chain, &fixture.cfg, &fixture.artifact, fixture.hotkey, responses, 4*1024*1024); err != nil {
			t.Fatal(err)
		}
	}
	if calls := fixture.rpc.counts()["eth_call"]; calls != 2*12 {
		t.Fatalf("capture lost per-intent sharing or reused cross-pass authority: %d", calls)
	}
}

// Real private capture and startup descriptor custody preserve original
// response hashes; a new recovery owner reconstructs actual chain authority.
func TestReleaseClientKeyAuthorityV2RetainedRecoveryReusesOnlyExactCapturedBytes(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	fixture.cfg.StateDir = newReleaseHeadV2TestStateDir(t)
	ctx, owner := fixture.owner(t, t.Context(), 4*1024*1024)
	reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
	var requests []protocol.ClientKeyObservationRequest
	var hashes []string
	for index := byte(1); index <= 3; index++ {
		request := fixture.request
		request.ClientID[1] = index
		registration, hash, size, err := captureReleaseClientKeyV2(ctx, fixture.chain, reader, fixture.cfg.StateDir, fixture.domain, request, releaseClientKeyHistoryTestResponseBytes, 4*1024*1024)
		if err != nil || registration.ClientID != request.ClientID || hash == "" || size == 0 {
			t.Fatalf("actual private capture failed: hash=%q size=%d error=%v", hash, size, err)
		}
		requests, hashes = append(requests, request), append(hashes, hash)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
	recoveryCtx, recovery := fixture.owner(t, t.Context(), 4*1024*1024)
	custody := &releaseEvidenceV2StartupReferences{remaining: 4 * 1024 * 1024}
	t.Cleanup(func() { _ = custody.close() })
	for index, request := range requests {
		registration, err := readRetainedReleaseClientKeyV2(recoveryCtx, fixture.chain, custody, fixture.cfg.StateDir, fixture.domain, request, hashes[index], releaseClientKeyHistoryTestResponseBytes)
		if err != nil || registration.ClientID != request.ClientID || registration.PublicKey != ([32]byte{0x31}) {
			t.Fatalf("actual retained recovery failed: %v", err)
		}
	}
	if err := errors.Join(recovery.finish(nil), custody.check(), custody.close()); err != nil {
		t.Fatal(err)
	}
	if calls := fixture.rpc.counts()["eth_call"]; calls != 2*12 {
		t.Fatalf("recovery reused old authority or repeated one read per client: %d", calls)
	}
}

// Actual M8 head collection with retained Http source hashes has one complete
// authority read per operator, not one per independently signed client.
func TestReleaseClientKeyAuthorityV2ActualHeadDeduplicatesItsProviderCensus(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, binding := range result.Bindings {
		if binding.Active {
			active++
			if binding.ClientKeyObservationHash == "" {
				t.Fatal("real head omitted a signed client response")
			}
		}
	}
	if active <= len(fixture.steerer.cfg.Operators) {
		t.Fatal("fixture lacks repeated active clients within one authority domain")
	}
	requests, batches := fixture.rpc.counts()
	// Two binding batches plus two complete four-Http authority operations
	// and one four-member final witness. Logical work is counted separately.
	want := len(result.Bindings) + 2 + 2*20 + 4
	if requests != want || batches != 2+2*4+1 {
		t.Fatalf("actual head source request census differs: requests=%d want=%d batches=%d", requests, want, batches)
	}
	fixture.assertNoEMACommit(t)
}
