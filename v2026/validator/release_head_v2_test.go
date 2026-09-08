//go:build linux || darwin

package validator

// The collector uses a real ChainClient against a deterministic HTTP JSON-RPC
// fixture, real M8 ledgers and full typed replay. Test chain observations are
// independent fixtures, not claims about historical or live-chain eligibility.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// Boundaries and ABI values are immutable while ServeHTTP is running. Only
// request observations use the fixture lock; no callback runs under that lock.
type releaseHeadV2RPCFixture struct {
	stateLock  sync.Mutex
	artifact   *ReleaseMeasurementArtifact
	bindingKVs map[connect.Id]ReleaseBindingMeasurement
	failClient connect.Id
	requests   int
	batches    int
}

// Every response keeps the exact requested hash/canonical flag. A batch is
// deliberately reversed, so the real RPC client's id matching is exercised.
func (self *releaseHeadV2RPCFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(request.Body).Decode(&raw); err != nil {
		http.Error(writer, err.Error(), 400)
		return
	}
	reply := func(call chainBatchRPCRequest) map[string]any {
		func() { self.stateLock.Lock(); defer self.stateLock.Unlock(); self.requests++ }()
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
		refuse := func(message string) map[string]any {
			response["error"] = map[string]any{"code": -32000, "message": message}
			return response
		}
		if call.Method == "eth_getBlockByHash" {
			var hash common.Hash
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &hash) != nil || hash != common.HexToHash(self.artifact.EVMSnapshotHash) {
				return refuse("fixture head hash differs")
			}
			response["result"] = map[string]any{"number": hexutil.Uint64(self.artifact.EVMSnapshotBlock), "hash": hash}
			return response
		}
		if call.Method != "eth_call" || len(call.Params) != 2 {
			return refuse("unexpected fixture call")
		}
		var payload struct {
			To    common.Address
			Data  hexutil.Bytes
			Input hexutil.Bytes
		}
		var block gethrpc.BlockNumberOrHash
		if json.Unmarshal(call.Params[0], &payload) != nil || json.Unmarshal(call.Params[1], &block) != nil || block.BlockHash == nil || *block.BlockHash != common.HexToHash(self.artifact.EVMSnapshotHash) || !block.RequireCanonical || payload.To != common.HexToAddress(self.artifact.Coordinator) {
			return refuse("fixture binding is not at the pinned coordinator/hash")
		}
		calldata := payload.Data
		if len(calldata) == 0 {
			calldata = payload.Input
		}
		coordinator := stabi.NewSTCoordinator()
		selector := coordinator.PackBindingAt([16]byte{}, big.NewInt(1))
		if len(calldata) != 68 || !bytes.Equal(calldata[:4], selector[:4]) || new(big.Int).SetBytes(calldata[36:]).Cmp(new(big.Int).SetUint64(self.artifact.SettlementEpoch)) != 0 {
			return refuse("fixture binding selector/epoch differs")
		}
		var clientID connect.Id
		copy(clientID[:], calldata[4:20])
		if clientID == self.failClient && self.failClient != (connect.Id{}) {
			return refuse("test-owned exact binding failure")
		}
		binding, found := self.bindingKVs[clientID]
		if !found {
			return refuse("fixture client absent from independent binding census")
		}
		parsed, err := stabi.STCoordinatorMetaData.ParseABI()
		if err != nil {
			return refuse(err.Error())
		}
		record := stabi.STCoordinatorBindingRecord{
			FleetId: common.HexToHash(binding.FleetID), Hotkey: common.HexToHash(binding.Hotkey), ClientKey: common.HexToHash(binding.ClientKey),
			CommitmentHash: common.HexToHash(binding.CommitmentHash), Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch,
			ValidToEpoch: binding.ValidToEpoch, CleanedAtEpoch: binding.CleanedAtEpoch, Uid: binding.RecordUID, Cleaned: binding.Cleaned,
		}
		encoded, err := parsed.Methods["bindingAt"].Outputs.Pack(binding.Active, record)
		if err != nil {
			return refuse(err.Error())
		}
		response["result"] = hexutil.Encode(encoded)
		return response
	}
	writer.Header().Set("Content-Type", "application/json")
	if trimmed := bytes.TrimSpace(raw); len(trimmed) != 0 && trimmed[0] == '[' {
		var calls []chainBatchRPCRequest
		if err := json.Unmarshal(raw, &calls); err != nil {
			http.Error(writer, err.Error(), 400)
			return
		}
		func() { self.stateLock.Lock(); defer self.stateLock.Unlock(); self.batches++ }()
		results := make([]map[string]any, len(calls))
		for index, call := range calls {
			results[len(calls)-1-index] = reply(call)
		}
		_ = json.NewEncoder(writer).Encode(results)
		return
	}
	var call chainBatchRPCRequest
	if err := json.Unmarshal(raw, &call); err != nil {
		http.Error(writer, err.Error(), 400)
		return
	}
	_ = json.NewEncoder(writer).Encode(reply(call))
}

// Locking is confined to the request counters, not a test's expected verdict.
func (self *releaseHeadV2RPCFixture) counts() (int, int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.requests, self.batches
}

// One fixture keeps ordinary and legacy oracles separate and obtains new
// real replay scratch for each attempted collection.
type releaseHeadV2TestFixture struct {
	measurement *releaseMeasurementV2TestFixture
	steerer     *ReleaseSteerer
	snapshot    *ReleaseSnapshot
	hotkeys     map[[32]byte]uint16
	rpc         *releaseHeadV2RPCFixture
}

// Changing the native signer re-signs the real compact headers, never the
// genuine M8 rows, proof bytes, expected quality or independent legacy oracle.
func newReleaseHeadV2TestFixture(t *testing.T, completed int) *releaseHeadV2TestFixture {
	t.Helper()
	measurement := newReleaseMeasurementV2TestFixture(t, completed)
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x73})
	if err != nil {
		t.Fatal(err)
	}
	bindReleaseMeasurementEnvelopeV2TestHotkey(t, measurement, hotkey)
	artifact := measurement.artifact
	rpc := &releaseHeadV2RPCFixture{artifact: cloneReleaseMeasurementArtifact(t, artifact), bindingKVs: map[connect.Id]ReleaseBindingMeasurement{}}
	hotkeys := map[[32]byte]uint16{hotkey.PublicKey(): artifact.SelfUID}
	for _, binding := range artifact.Bindings {
		clientID, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		rpc.bindingKVs[clientID] = binding
		if binding.LiveUIDFound {
			hotkeys[common.HexToHash(binding.Hotkey)] = binding.LiveUID
		}
	}
	server := httptest.NewServer(rpc)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	chain := &ChainClient{client: client, coordinator: stabi.NewSTCoordinator(), chainId: new(big.Int).SetUint64(artifact.ChainID), contractAddr: common.HexToAddress(artifact.Coordinator), release: true}
	cfg := &ReleaseConfig{DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID, GenesisHash: artifact.GenesisHash, Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault, ValidatorID: artifact.ValidatorID, Netuid: artifact.Netuid, PolicyHash: artifact.PolicyHash, Policy: artifact.Policy, ControlledNOIDs: slices.Clone(artifact.ControlledNOIDs)}
	contexts := map[uint64]*ReleaseMeasurementContext{}
	for _, input := range artifact.Inputs {
		cfg.Operators = append(cfg.Operators, OperatorConfig{NoID: input.NoID})
		contexts[input.NoID] = &ReleaseMeasurementContext{NoID: input.NoID, Stats: measurement.operators[input.NoID].seal.engine.stats, ClientKey: func(connect.Id) ([32]byte, bool, error) { return [32]byte{0x31}, true, nil }}
	}
	store, err := newReleaseHeadV2EMAStore(t, newReleaseHeadV2TestStateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	policyHash, err := artifact.Policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &ReleaseSnapshot{BlockNumber: artifact.EVMSnapshotBlock, BlockHash: common.HexToHash(artifact.EVMSnapshotHash), Epoch: new(big.Int).SetUint64(artifact.SettlementEpoch)}
	snapshot.Policy.PolicyHash = policyHash
	return &releaseHeadV2TestFixture{measurement: measurement, steerer: &ReleaseSteerer{cfg: cfg, chain: chain, hotkey: hotkey, contexts: contexts, headEMA: store}, snapshot: snapshot, hotkeys: hotkeys, rpc: rpc}
}

// Current bindings are collected through the real RPC path, not supplied by
// the fixture's candidate artifact or borrowed as replay authority.
func (self *releaseHeadV2TestFixture) options(t *testing.T) ReleaseMeasurementV2Options {
	t.Helper()
	options := self.measurement.options(t)
	options.Bindings, options.Pools, options.DepositAudits = []ReleaseBindingMeasurement{}, []ReleasePoolMeasurement{}, []DepositAudit{}
	for noID, operator := range options.Operators {
		operator.Measurement.CurrentBindingKVs = nil
		options.Operators[noID] = operator
	}
	return options
}

// The public collector returns only after actual HTTP callbacks and replay
// resource owners have joined.
func (self *releaseHeadV2TestFixture) gather(ctx context.Context, options ReleaseMeasurementV2Options) (releaseHeadResult, error) {
	artifact := self.measurement.artifact
	return self.steerer.gatherHeadV2(ctx, self.snapshot, artifact.SubnetEpoch, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash, self.hotkeys, artifact.Inputs, options)
}

// Preview must never leave a durable or live EMA mutation, even on success;
// the actual intent lifecycle owns the eventual CommitForEpoch.
func (self *releaseHeadV2TestFixture) assertNoEMACommit(t *testing.T) {
	t.Helper()
	store := self.steerer.headEMA
	store.mu.Lock()
	empty := len(store.values) == 0 && store.lastSubnetEpoch == nil && store.lastAlpha == nil && len(store.lastFold) == 0
	store.mu.Unlock()
	if !empty {
		t.Fatal("head preview committed live EMA state before an intent")
	}
	if _, err := os.Lstat(store.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("head preview changed durable EMA state: %v", err)
	}
}

// Both positive-quality M8 operators are replayed and the result equals the
// separate v1 oracle; immutable observations come from reversed RPC batches.
func TestReleaseHeadV2CollectsRealM8WithPinnedBindingBatches(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 15)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	want := fixture.measurement.want
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Weights, want.SelectedHead) || !reflect.DeepEqual(result.Bound, want.BoundProviders) || !reflect.DeepEqual(result.Bindings, fixture.measurement.artifact.Bindings) || !reflect.DeepEqual(result.HeadEMA, fixture.measurement.artifact.HeadEMA) {
		t.Fatal("live compact head differs from the independent exact legacy oracle")
	}
	requests, batches := fixture.rpc.counts()
	if requests == 0 || batches != 2 || len(result.Inputs) != 2 {
		t.Fatalf("real pinned binding census was not consumed: requests=%d batches=%d", requests, batches)
	}
	fixture.assertNoEMACommit(t)
}

// An independently observed new generation cannot inherit the old signed
// generation's work, but its live registered providers still leave the pool.
func TestReleaseHeadV2RebindingKeepsZeroHeadAndPoolExclusion(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	for clientID, binding := range fixture.rpc.bindingKVs {
		if binding.Active {
			binding.Generation++
			fixture.rpc.bindingKVs[clientID] = binding
		}
	}
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil || len(result.Weights) != 0 {
		t.Fatalf("new generation inherited old head work: %v", err)
	}
	for _, binding := range fixture.rpc.bindingKVs {
		clientID, _ := connect.ParseId(binding.ClientID)
		if result.Bound[binding.NoID][clientID] != binding.Active {
			t.Fatal("live unselected binding did not retain pool exclusion")
		}
	}
	fixture.assertNoEMACommit(t)
}

// Deregistration is a stale binding, not proof of fresh head eligibility. Its
// actual completed/failed trail exposure remains in the raw pool measurement.
func TestReleaseHeadV2StaleUIDReturnsProviderToPool(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	for hotkey := range fixture.hotkeys {
		if hotkey != fixture.steerer.hotkey.PublicKey() {
			delete(fixture.hotkeys, hotkey)
		}
	}
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil || len(result.Weights) != 0 || len(result.StaleBindings) == 0 {
		t.Fatalf("stale native binding was not rejected: %v", err)
	}
	for _, providers := range result.Bound {
		if len(providers) != 0 {
			t.Fatal("deregistered provider remained excluded from pool")
		}
	}
	if !reflect.DeepEqual(result.Inputs, fixture.measurement.artifact.Inputs) {
		t.Fatal("stale head altered signed pool exposure")
	}
	fixture.assertNoEMACommit(t)
}

// A later operator's wrong authority is rejected before the first chain
// request, object reader or replay scratch creation.
func TestReleaseHeadV2AdmitsAllOperatorAuthorityBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	options := fixture.options(t)
	operator := options.Operators[10]
	operator.Expected.Activation.Domain.ActivationHash[0] ^= 1
	options.Operators[10] = operator
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, _ := fixture.rpc.counts()
	if err == nil || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("later authority reached I/O: requests=%d reads=%d err=%v", requests, *reads, err)
	}
	fixture.assertNoEMACommit(t)
}

// The second binding census must finish before either operator's public proof
// replay. A current-chain failure cannot produce partial scored head output.
func TestReleaseHeadV2BindingFailurePrecedesAllProofReaders(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	for clientID, binding := range fixture.rpc.bindingKVs {
		if binding.NoID == 10 {
			fixture.rpc.failClient = clientID
			break
		}
	}
	options := fixture.options(t)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, _ := fixture.rpc.counts()
	if err == nil || requests == 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("binding failure produced partial head: requests=%d reads=%d err=%v", requests, *reads, err)
	}
	fixture.assertNoEMACommit(t)
}

// Header validity alone is not enough: changing a byte in the second real
// proof stream invalidates the entire live result after the first replay.
func TestReleaseHeadV2ChangedProofDiscardsAllHeadOutput(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	options := fixture.options(t)
	operator := options.Operators[10]
	open := operator.Measurement.Replay.OpenData
	changed := false
	operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &releaseMeasurementEnvelopeV2ChangedReader{ReadCloser: reader, changed: &changed}, nil
	}
	options.Operators[10] = operator
	result, err := fixture.gather(t.Context(), options)
	if err == nil || !changed || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("changed real proof escaped complete replay: changed=%v error=%v", changed, err)
	}
	fixture.assertNoEMACommit(t)
}

// At the proof-reader boundary, owned candidate data and later replay options
// cannot be retargeted. The earlier client-key callback has its own control.
func TestReleaseHeadV2OwnsInputsAcrossProofCallbacks(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	options := fixture.options(t)
	wantInputs := cloneReleaseMeasurementArtifact(t, fixture.measurement.artifact).Inputs
	wantBindings := slices.Clone(fixture.measurement.artifact.Bindings)
	operator := options.Operators[9]
	read := operator.Measurement.Replay.ReadMetadata
	mutated := false
	operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !mutated {
			mutated = true
			fixture.measurement.artifact.Inputs[1].Stats.Providers[0].Assignments++
			later := options.Operators[10]
			for _, key := range later.Measurement.Replay.ServerKeys {
				key[0] ^= 1
				break
			}
			delete(fixture.hotkeys, common.HexToHash(fixture.measurement.artifact.Bindings[0].Hotkey))
			fixture.snapshot.BlockNumber++
		}
		return read(ctx, hash, size)
	}
	options.Operators[9] = operator
	result, err := fixture.gather(t.Context(), options)
	if err != nil || !mutated || !reflect.DeepEqual(result.Inputs, wantInputs) || !reflect.DeepEqual(result.Bindings, wantBindings) {
		t.Fatalf("callback changed owned head evidence: mutated=%v err=%v", mutated, err)
	}
	fixture.assertNoEMACommit(t)
}

// The first synchronous client-key lookup precedes the second operator's RPC
// and every proof read. Changing borrowed data there cannot retarget either.
func TestReleaseHeadV2OwnsInputsBeforeFirstClientKeyCallback(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	options := fixture.options(t)
	wantInputs := cloneReleaseMeasurementArtifact(t, fixture.measurement.artifact).Inputs
	wantBindings := slices.Clone(fixture.measurement.artifact.Bindings)
	lookup := fixture.steerer.contexts[9].ClientKey
	mutated := false
	fixture.steerer.contexts[9].ClientKey = func(clientID connect.Id) ([32]byte, bool, error) {
		if !mutated {
			mutated = true
			fixture.measurement.artifact.Inputs[1].Stats.Providers[0].Assignments++
			later := options.Operators[10]
			for _, key := range later.Measurement.Replay.ServerKeys {
				key[0] ^= 1
				break
			}
			for key := range fixture.hotkeys {
				delete(fixture.hotkeys, key)
			}
			fixture.snapshot.BlockNumber++
			fixture.snapshot.Epoch.Add(fixture.snapshot.Epoch, big.NewInt(1))
			fixture.steerer.contexts[10].ClientKey = func(connect.Id) ([32]byte, bool, error) {
				return [32]byte{}, false, errors.New("borrowed later lookup must not execute")
			}
		}
		return lookup(clientID)
	}
	result, err := fixture.gather(t.Context(), options)
	_, batches := fixture.rpc.counts()
	if err != nil || !mutated || batches != 2 || !reflect.DeepEqual(result.Inputs, wantInputs) || !reflect.DeepEqual(result.Bindings, wantBindings) {
		t.Fatalf("first client-key callback retargeted owned head evidence: mutated=%v batches=%d err=%v", mutated, batches, err)
	}
	fixture.assertNoEMACommit(t)
}

// An artifact-only control budget is insufficient for the native identities
// and generated binding census. Refusal precedes any binding/proof I/O.
func TestReleaseHeadV2RejectsCensusStorageBeforeBindingIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	options := fixture.options(t)
	draft := cloneReleaseMeasurementArtifact(t, fixture.measurement.artifact)
	draft.Bindings, draft.HeadEMA = []ReleaseBindingMeasurement{}, []HeadEMAMeasurement{}
	draft.Pools, draft.DepositAudits = []ReleasePoolMeasurement{}, []DepositAudit{}
	remaining := options.MaxControlBytes
	if err := releaseMeasurementV2ControlStorage(t.Context(), reflect.ValueOf(draft), &remaining); err != nil {
		t.Fatal(err)
	}
	options.MaxControlBytes -= remaining
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, _ := fixture.rpc.counts()
	if err == nil || !strings.Contains(err.Error(), "compact live head census exceeds its control bound") || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("generated census bypassed bounded pre-I/O admission: requests=%d reads=%d err=%v", requests, *reads, err)
	}
	fixture.assertNoEMACommit(t)
}

// Missing/private-key-less and valid but unauthorized signers are homogeneous
// admission refusals, never panics or invitations to query current bindings.
func TestReleaseHeadV2RejectsInvalidSignerBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	replacement, err := crv4.KeypairFromSeed([32]byte{0x74})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		hotkey *crv4.Keypair
	}{
		{name: "nil"}, {name: "zero", hotkey: &crv4.Keypair{}},
		{name: "ring without signer", hotkey: &crv4.Keypair{Ring: replacement.Ring}},
		{name: "different native identity", hotkey: replacement},
	} {
		fixture.steerer.hotkey = test.hotkey
		options := fixture.options(t)
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		result, err := fixture.gather(t.Context(), options)
		requests, _ := fixture.rpc.counts()
		if err == nil || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
			t.Fatalf("%s signer reached I/O or yielded head: requests=%d reads=%d err=%v", test.name, requests, *reads, err)
		}
	}
	fixture.assertNoEMACommit(t)
}

// The actual stream closes first; an injected late error and cancellation
// then test the caller's error/ownership contract, not a physical disk fault.
type releaseHeadV2LateReader struct {
	io.ReadCloser
	cancel  context.CancelFunc
	failure error
	closed  *int
}

func (self *releaseHeadV2LateReader) Close() error {
	err := self.ReadCloser.Close()
	(*self.closed)++
	self.cancel()
	return errors.Join(err, self.failure)
}

// Late errors must survive parent cancellation, and even a fully replayed
// first operator cannot leak a result or pre-intent EMA publication.
func TestReleaseHeadV2LateCloseCancellationRetainsFailure(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	options := fixture.options(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failure := errors.New("test-owned post-close error contract")
	operator := options.Operators[10]
	open := operator.Measurement.Replay.OpenData
	closed := 0
	operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &releaseHeadV2LateReader{ReadCloser: reader, cancel: cancel, failure: failure, closed: &closed}, nil
	}
	options.Operators[10] = operator
	result, err := fixture.gather(ctx, options)
	if !errors.Is(err, failure) || !errors.Is(err, context.Canceled) || closed == 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("late failure/cancellation escaped result clearing: closes=%d err=%v", closed, err)
	}
	fixture.assertNoEMACommit(t)
}

// This is an exact 1000-fleet scoring/pool-exclusion control, not a claim that
// 1000 genuine M8 streams ran. The separate collector tests above prove replay.
func TestReleaseHeadV2SharedSelectionKeepsTop200Of1000(t *testing.T) {
	t.Parallel()
	store, err := newReleaseHeadV2EMAStore(t, newReleaseHeadV2TestStateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	policy := exactPolicy(t)
	if policy.Steering.MaximumHeadFleets != 200 {
		t.Fatal("existing top-200 policy changed")
	}
	fleets := map[FleetScoreKey]map[[32]byte]bool{}
	members := map[uint16][]releaseHeadMember{}
	for uid := uint16(0); uid < 1000; uid++ {
		key := FleetScoreKey{FleetID: [32]byte{1, byte(uid >> 8), byte(uid)}, Hotkey: [32]byte{2, byte(uid >> 8), byte(uid)}, Generation: 1, UID: uid}
		fleets[key] = map[[32]byte]bool{{3, byte(uid >> 8), byte(uid)}: true}
		members[uid] = []releaseHeadMember{{NoID: 9 + uint64(uid%2), ClientID: connect.Id{1, byte(uid >> 8), byte(uid)}}}
	}
	original := make(map[FleetScoreKey]map[[32]byte]bool, len(fleets))
	for key, prefixes := range fleets {
		original[key] = maps.Clone(prefixes)
	}
	result, err := finishReleaseHead(store, 7, policy, fleets, map[uint64]map[connect.Id]bool{}, map[uint64]bool{10: true}, members, []StaleHeadBinding{}, []ReleaseMeasurementInput{}, []ReleaseBindingMeasurement{})
	if err != nil || len(result.SelectedUIDs) != 200 || len(result.RejectedUIDs) != 800 || len(result.EligibleUIDs) != 1000 {
		t.Fatalf("top-200 census differs: %v", err)
	}
	for index, uid := range result.SelectedUIDs {
		if uid != uint16(index) {
			t.Fatalf("exact tie selection is not by uid: %v", result.SelectedUIDs)
		}
	}
	for uid, providerMembers := range members {
		for _, member := range providerMembers {
			if !result.Bound[member.NoID][member.ClientID] {
				t.Fatalf("registered selected/rejected fleet %d re-entered pool", uid)
			}
			if result.Controlled[uid] != (member.NoID == 10) {
				t.Fatal("controlled operator masking changed")
			}
		}
	}
	if !reflect.DeepEqual(original, fleets) {
		t.Fatal("selection changed its input fleet/prefix sets")
	}
}

// A real committed/reloaded transcript is replayed exactly at the same epoch
// and previewed at the successor, without changing durable or live history.
func TestReleaseHeadV2BoundedEMAPreviewPreservesDurableSameEpoch(t *testing.T) {
	t.Parallel()
	stateDir := newReleaseHeadV2TestStateDir(t)
	store, err := newReleaseHeadV2EMAStore(t, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 3}
	alpha := exactPolicy(t).Steering.HeadScoreEMA
	raw := map[FleetScoreKey]*big.Rat{key: big.NewRat(5, 3)}
	want, transcript, err := store.PreviewForEpochV2(t.Context(), 7, raw, alpha)
	if err != nil || want[key.UID] == nil || want[key.UID].Cmp(big.NewRat(5, 3)) != 0 {
		t.Fatalf("initial exact EMA control differs: %v", err)
	}
	if err := store.CommitForEpochV2(t.Context(), 7, transcript, alpha); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	store, err = newReleaseHeadV2EMAStore(t, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	got, replay, err := store.previewForEpochV2(t.Context(), 7, raw, alpha, 8, 1024*1024)
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(replay, transcript) {
		t.Fatalf("bounded same-epoch preview changed committed values: %v", err)
	}
	successorRaw := map[FleetScoreKey]*big.Rat{key: big.NewRat(2, 3)}
	wantNext, wantTranscript, err := store.PreviewForEpochV2(t.Context(), 8, successorRaw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	gotNext, gotTranscript, err := store.previewForEpochV2(t.Context(), 8, successorRaw, alpha, 8, 1024*1024)
	if err != nil || !reflect.DeepEqual(gotNext, wantNext) || !reflect.DeepEqual(gotTranscript, wantTranscript) {
		t.Fatalf("bounded successor differs from exact ordinary preview: %v", err)
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("bounded preview rewrote durable history: %v", err)
	}
	if store.lastSubnetEpoch == nil || *store.lastSubnetEpoch != 7 || !reflect.DeepEqual(store.lastFold, transcript) {
		t.Fatal("bounded preview advanced live history")
	}
}

// These homogeneous owner-state admission controls force each count limit
// independently; they do not claim malformed state was admitted from disk.
func TestReleaseHeadV2BoundsEMAUnionAndCensuses(t *testing.T) {
	t.Parallel()
	first := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 3}
	second := FleetScoreKey{FleetID: [32]byte{4}, Hotkey: [32]byte{5}, Generation: 1, UID: 6}
	entry := func(key FleetScoreKey) headEMAEntry { return headEMAEntry{Key: key, Numerator: "1", Denominator: "1"} }
	for _, test := range []struct {
		name    string
		values  map[string]headEMAEntry
		fold    []HeadEMAMeasurement
		raw     map[FleetScoreKey]*big.Rat
		message string
	}{
		{name: "current", raw: map[FleetScoreKey]*big.Rat{first: big.NewRat(1, 1), second: big.NewRat(1, 1)}, message: "census exceeds"},
		{name: "retained", values: map[string]headEMAEntry{first.String(): entry(first), second.String(): entry(second)}, message: "census exceeds"},
		{name: "same epoch transcript", fold: []HeadEMAMeasurement{{Key: first}, {Key: second}}, message: "census exceeds"},
		{name: "history current union", values: map[string]headEMAEntry{first.String(): entry(first)}, raw: map[FleetScoreKey]*big.Rat{second: big.NewRat(1, 1)}, message: "current/history union exceeds"},
	} {
		store, err := newReleaseHeadV2EMAStore(t, newReleaseHeadV2TestStateDir(t))
		if err != nil {
			t.Fatal(err)
		}
		// This remains an explicit state-admission contract, not a claim
		// that malformed state was accepted by the real disk loader.
		store.values, store.lastFold = test.values, test.fold
		beforeValues, beforeFold := maps.Clone(test.values), slices.Clone(test.fold)
		got, transcript, err := store.previewForEpochV2(t.Context(), 7, test.raw, exactPolicy(t).Steering.HeadScoreEMA, 1, 1024*1024)
		if err == nil || !strings.Contains(err.Error(), test.message) || got != nil || transcript != nil {
			t.Fatalf("%s did not refuse exact census before preview: %v", test.name, err)
		}
		if !reflect.DeepEqual(store.values, beforeValues) || !reflect.DeepEqual(store.lastFold, beforeFold) {
			t.Fatalf("%s admission mutated history", test.name)
		}
	}
}

// Both cases come from real persisted and reloaded EMA commits. In the second,
// zero current weight removes the live entry but the same-epoch transcript
// retains its large prior rational, which must still be bounded before parsing.
func TestReleaseHeadV2BoundsRetainedEMARationalStorage(t *testing.T) {
	t.Parallel()
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 3}
	alpha := protocol.Rational{Numerator: 1, Denominator: 1}
	large, ok := new(big.Int).SetString(strings.Repeat("1", 1024), 10)
	if !ok {
		t.Fatal("deterministic large integer is invalid")
	}
	for _, transcriptOnly := range []bool{false, true} {
		stateDir := newReleaseHeadV2TestStateDir(t)
		store, err := newReleaseHeadV2EMAStore(t, stateDir)
		if err != nil {
			t.Fatal(err)
		}
		epoch := uint64(7)
		raw := map[FleetScoreKey]*big.Rat{key: new(big.Rat).SetInt(large)}
		_, transcript, err := store.PreviewForEpochV2(t.Context(), epoch, raw, alpha)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.CommitForEpochV2(t.Context(), epoch, transcript, alpha); err != nil {
			t.Fatal(err)
		}
		if transcriptOnly {
			epoch++
			raw = map[FleetScoreKey]*big.Rat{key: big.NewRat(0, 1)}
			_, transcript, err = store.PreviewForEpochV2(t.Context(), epoch, raw, alpha)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.CommitForEpochV2(t.Context(), epoch, transcript, alpha); err != nil {
				t.Fatal(err)
			}
		}
		store, err = newReleaseHeadV2EMAStore(t, stateDir)
		if err != nil {
			t.Fatal(err)
		}
		if transcriptOnly && (len(store.values) != 0 || len(store.lastFold) != 1 || len(store.lastFold[0].Prior.Numerator) != 1024) {
			t.Fatal("real zero-weight commit did not retain large prior in transcript only")
		}
		before, err := os.ReadFile(store.path)
		if err != nil {
			t.Fatal(err)
		}
		bound := releaseHeadV2TestOwnerControlBytes(store) + 512
		got, replay, err := store.previewForEpochV2(t.Context(), epoch, raw, alpha, 8, bound)
		if err == nil || !strings.Contains(err.Error(), "control storage exceeds its bound") || got != nil || replay != nil {
			t.Fatalf("retained rational bypassed bounded preview (transcript only %v): %v", transcriptOnly, err)
		}
		after, err := os.ReadFile(store.path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("refused retained rational changed disk (transcript only %v): %v", transcriptOnly, err)
		}
	}
}

// Current big.Rat payload is charged as integer words before copying/folding;
// it cannot bypass the separate retained-string and entry-count budgets.
func TestReleaseHeadV2BoundsCurrentEMARationalStorage(t *testing.T) {
	t.Parallel()
	store, err := newReleaseHeadV2EMAStore(t, newReleaseHeadV2TestStateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 3}
	large := new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 4096))
	bound := releaseHeadV2TestOwnerControlBytes(store) + 512
	got, transcript, err := store.previewForEpochV2(t.Context(), 7, map[FleetScoreKey]*big.Rat{key: large}, exactPolicy(t).Steering.HeadScoreEMA, 1, bound)
	if err == nil || !strings.Contains(err.Error(), "raw rational exceeds its control bound") || got != nil || transcript != nil {
		t.Fatalf("current rational bypassed word bound: %v", err)
	}
	if len(store.values) != 0 || store.lastSubnetEpoch != nil {
		t.Fatal("refused current rational changed live history")
	}
	if _, err := os.Lstat(store.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused current rational changed disk: %v", err)
	}
}
