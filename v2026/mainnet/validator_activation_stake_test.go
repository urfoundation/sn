// Original independently signed fixtures exercise real native RPC transport,
// complete runtime census decoding and the public bounded admission operation.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Mutable runtime bytes and fault hooks are owned by one fixture lock. All
// fixture state changes happen between joined command/observation invocations.
type validatorActivationStakeFixture struct {
	activation   *validatorActivationFixture
	stateLock    sync.Mutex
	fields       map[int][]byte
	runtimeCalls int
	fault        func(validatorActivationNativeTestCall, *http.Response) (*http.Response, error)
	barrier      func(context.Context)
}

// Fixture metadata and runtime family are selected before either independent
// approval. The five original preparation journals retain their signed bytes.
func newValidatorActivationStakeFixture(t *testing.T, metadataFault func(*types.Metadata)) *validatorActivationStakeFixture {
	t.Helper()
	mutate := func(metadata *types.Metadata) {
		ids := map[types.Si0TypeDefPrimitive]types.Si1LookupTypeID{}
		for _, entry := range metadata.AsMetadataV14.Lookup.Types {
			if entry.Type.Def.IsPrimitive {
				ids[entry.Type.Def.Primitive.Si0TypeDefPrimitive] = entry.ID
			}
		}
		for p := range metadata.AsMetadataV14.Pallets {
			pallet := &metadata.AsMetadataV14.Pallets[p]
			if pallet.Name != "SubtensorModule" {
				continue
			}
			for _, field := range []struct {
				name      string
				primitive types.Si0TypeDefPrimitive
				width     int
				plain     bool
			}{
				{name: "ActivityCutoffFactorMilli", primitive: types.IsU32, width: 4}, {name: "Kappa", primitive: types.IsU16, width: 2}, {name: "StakeThreshold", primitive: types.IsU64, width: 8, plain: true},
			} {
				entry := types.StorageEntryMetadataV14{Name: types.Text(field.name), Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Fallback: make(types.Bytes, field.width)}
				if field.plain {
					entry.Type = types.StorageEntryTypeV14{IsPlainType: true, AsPlainType: ids[field.primitive]}
				} else {
					entry.Type = types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Key: ids[types.IsU16], Value: ids[field.primitive], Hashers: []types.StorageHasherV10{{IsIdentity: true}}}}
				}
				found := false
				for i := range pallet.Storage.Items {
					if pallet.Storage.Items[i].Name == entry.Name {
						pallet.Storage.Items[i] = entry
						found = true
						break
					}
				}
				if !found {
					pallet.Storage.Items = append(pallet.Storage.Items, entry)
				}
			}
		}
		if metadataFault != nil {
			metadataFault(metadata)
		}
	}
	activation := newValidatorActivationFixtureWithNativeCensus(t, mutate, nil, func(census *rootRpcFixture, policy *subnetCensusPolicy) {
		policy.RuntimeVersion.SpecVersion = 455
		census.version = policy.RuntimeVersion
		census.policy.RuntimeVersion = policy.RuntimeVersion
		census.set(t, "StakeThreshold", binary.LittleEndian.AppendUint64(nil, 100))
		census.set(t, "ActivityCutoffFactorMilli", binary.LittleEndian.AppendUint32(nil, 100), []byte{25, 0})
		census.set(t, "Kappa", binary.LittleEndian.AppendUint16(nil, 32768), []byte{25, 0})
		census.set(t, "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, 1), bytes.Repeat([]byte{0x43}, 32), []byte{25, 0})
	})
	f := &validatorActivationStakeFixture{activation: activation, fields: map[int][]byte{}}
	compact := func(value uint64) []byte {
		raw, err := codec.Encode(types.NewUCompactFromUInt(value))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	f.fields[30] = compact(6)
	f.fields[52] = compact(6)
	for i := 0; i < 6; i++ {
		f.fields[52] = append(f.fields[52], bytes.Repeat([]byte{byte(0x41 + i)}, 32)...)
	}
	f.fields[57] = append(compact(6), []byte{0, 0, 1, 1, 0, 0}...)
	f.setStakes(t, []uint64{500, 0, 8000, 1000, 0, 0})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), 400)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		var call validatorActivationNativeTestCall
		if err := json.Unmarshal(raw, &call); err != nil {
			http.Error(writer, err.Error(), 400)
			return
		}
		var response *http.Response
		if call.Method == "state_call" {
			if len(call.Params) != 3 || string(call.Params[0]) != `"SubnetInfoRuntimeApi_get_selective_metagraph"` || string(call.Params[1]) != `"0x19001400001e00340039004500"` || string(call.Params[2]) != `"`+testFinalizedHash+`"` {
				http.Error(writer, "runtime census escaped its exact synthetic scope", 400)
				return
			}
			f.stateLock.Lock()
			f.runtimeCalls++
			barrier := f.barrier
			data := []byte{1, 25 * 4}
			for index := 1; index <= 76; index++ {
				if value, ok := f.fields[index]; ok {
					data = append(data, 1)
					data = append(data, value...)
				} else {
					data = append(data, 0)
				}
			}
			f.stateLock.Unlock()
			if barrier != nil {
				barrier(request.Context())
			}
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x" + hex.EncodeToString(data)})
			response = &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}
		} else {
			response, err = activation.chain.census.roundTrip(request)
		}
		if err != nil {
			http.Error(writer, err.Error(), 400)
			return
		}
		f.stateLock.Lock()
		if f.fault != nil {
			response, err = f.fault(call, response)
		}
		f.stateLock.Unlock()
		if err != nil {
			if response != nil {
				response.Body.Close()
			}
			http.Error(writer, err.Error(), 400)
			return
		}
		defer response.Body.Close()
		writer.WriteHeader(response.StatusCode)
		_, _ = io.Copy(writer, response.Body)
	}))
	t.Cleanup(server.Close)
	activation.approval.Plan.Route.RpcUrl = server.URL
	activation.sign()
	return f
}

// Independent SDK compact encoding is used only to construct runtime replies.
func (self *validatorActivationStakeFixture) setStakes(t *testing.T, stakes []uint64) {
	t.Helper()
	data, err := codec.Encode(types.NewUCompactFromUInt(uint64(len(stakes))))
	if err != nil {
		t.Fatal(err)
	}
	for _, stake := range stakes {
		raw, err := codec.Encode(types.NewUCompactFromUInt(stake))
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, raw...)
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.fields[69] = data
}

// Original census/native admission uses its real reader; only the extra stake
// observer is routed through the independently approved local HTTP endpoint.
func (self *validatorActivationStakeFixture) read(t *testing.T, ctx context.Context) (*validatorActivationStakeReadiness, error) {
	t.Helper()
	f := self.activation
	readiness, err := f.chain.client.observeBootstrapChainReadiness(f.chain.storageContext(ctx), f.chain.preparation)
	if err != nil || !readiness.ObservationComplete {
		return nil, errors.Join(errors.New("original fixture readiness unavailable"), err)
	}
	prior, err := f.chain.client.observeValidatorActivationNative(ctx, f.chain.preparation, readiness)
	if err != nil {
		return nil, err
	}
	client, err := newOwnedSubmissionClient(f.approval.Plan.Route)
	if err != nil {
		return nil, err
	}
	defer client.httpClient.CloseIdleConnections()
	return client.observeValidatorActivationStake(ctx, f.chain.preparation, readiness, prior)
}

// Capacity can be admitted for fresh registrations while actual runtime
// activity and applied-weight influence remain explicitly unproved.
func TestValidatorActivationStakeCommandAdmitsCapacityWithoutStarting(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	f.activation.installed()
	before := f.activation.chain.journals(t)
	result, code, detail := f.activation.command(t.Context(), "admit-stake", nil)
	if code != 0 || result.Status != "admitted-stake-capacity" || result.Readiness == nil || result.Readiness.Stake == nil {
		t.Fatal("public stake admission refused original scope", code, result, detail)
	}
	stake := result.Readiness.Stake
	if !stake.CapacityAdmitted || stake.ActiveMajorityBoundObserved || stake.AppliedWeightsInfluenceProven || stake.ActivityCutoffBlocks != 10 || stake.Roles[0].WeightedStakeFloorRao != 8000 || stake.Roles[1].WeightedStakeFloorRao != 1000 || stake.Roles[0].ActiveShareLowerQ32 != 0 || stake.Roles[0].LastUpdate != 0 {
		t.Fatal("capacity became exact stake, raw alpha or applied influence", stake)
	}
	if err := stake.validate(*result.Readiness); err != nil {
		t.Fatal(err)
	}
	operations := result.Operations
	status, code, detail := f.activation.command(t.Context(), "status", nil)
	if code != 0 || !reflect.DeepEqual(status.Readiness.Stake, stake) {
		t.Fatal("retained stake projection lost its source", code, detail)
	}
	result, code, detail = f.activation.command(t.Context(), "start", nil)
	if code != 3 || result.Status != "activation-authority-unavailable" || result.Operations != operations || f.activation.starts != [2]int{} || result.ActivationReady || !reflect.DeepEqual(before, f.activation.chain.journals(t)) {
		t.Fatal("capacity opened a start or altered original custody", code, result, detail)
	}
	if !slices.Contains(result.Readiness.Roles[0].ActivationBlockers, "EFFECTIVE_STAKE_MAJORITY_UNVERIFIED") || !slices.Contains(result.Readiness.Roles[0].ActivationBlockers, "SIGNING_DEVICE_AND_GLOBAL_CUSTODY_FENCE_UNVERIFIED") {
		t.Fatal("stake capacity erased unresolved current authority")
	}
}

// A recomputed projection hash cannot excuse a contradictory original role,
// activity boundary or retained disposition. These checks never grant authority.
func TestValidatorActivationStakeJournalRejectsChangedProjectionScope(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	f.activation.installed()
	result, code, detail := f.activation.command(t.Context(), "admit-stake", nil)
	if code != 0 || result.Readiness == nil || result.Readiness.Stake == nil {
		t.Fatal("original stake admission unavailable", code, detail)
	}
	for _, fault := range []string{"hash", "block", "last-update", "inactive-share", "threshold", "capacity", "applied-influence", "uid"} {
		readiness := *result.Readiness
		projection := *readiness.Stake
		readiness.Stake = &projection
		switch fault {
		case "hash":
			projection.EvidenceHash = "sha256:" + strings.Repeat("e", 64)
		case "block":
			projection.FinalizedNumber++
		case "last-update":
			projection.Roles[0].LastUpdate = projection.FinalizedNumber + 1
		case "inactive-share":
			projection.Roles[0].ActiveShareLowerQ32 = 1
		case "threshold":
			projection.StakeThresholdRao = projection.Roles[0].WeightedStakeFloorRao + 1
		case "capacity":
			projection.Roles[0].CapacityShareLowerQ32 = projection.RequiredMajorityShareQ32
		case "applied-influence":
			projection.AppliedWeightsInfluenceProven = true
		case "uid":
			projection.Roles[0].Registration.Uid++
		}
		if fault != "hash" {
			projection.ContentHash = ""
			projection.ContentHash = rootObjectHash(projection)
		}
		if err := readiness.validate(f.activation.approval.Plan); err == nil {
			t.Fatal("contradictory retained stake projection accepted", fault)
		}
	}
}

// The exact factor-derived boundary admits equality and rejects one older
// block even though stored Active remains true in both original censuses.
func TestValidatorActivationStakeActivityUsesFactorAndExactBoundary(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	for _, last := range []uint64{89, 90} {
		updates := make([]byte, 6*8)
		binary.LittleEndian.PutUint64(updates[2*8:], last)
		binary.LittleEndian.PutUint64(updates[3*8:], last)
		f.activation.chain.census.set(t, "LastUpdate", subnetTestVector(updates, 8), []byte{25, 0})
		observed, err := f.read(t, t.Context())
		if err != nil || observed == nil || observed.Roles[0].RuntimeActivityEligible != (last == 90) || observed.ActiveMajorityBoundObserved != (last == 90) || observed.AppliedWeightsInfluenceProven {
			t.Fatal("factor-derived activity boundary differs", last, observed, err)
		}
	}
}

// A non-permitted peer cannot hide impossible future activity even while both
// original UR roles still pass their signed native freshness prerequisites.
func TestValidatorActivationStakeRejectsFuturePeerActivity(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	updates := make([]byte, 6*8)
	binary.LittleEndian.PutUint64(updates[5*8:], 101)
	f.activation.chain.census.set(t, "LastUpdate", subnetTestVector(updates, 8), []byte{25, 0})
	observed, err := f.read(t, t.Context())
	f.stateLock.Lock()
	reads := f.runtimeCalls
	f.stateLock.Unlock()
	if err == nil || observed != nil || reads != 1 || !strings.Contains(err.Error(), "activity is in the future") {
		t.Fatal("impossible peer activity escaped full-census validation", observed, err, reads)
	}
}

// Role labels and integer ratios cannot replace weighted stake/permit. The
// same independently signed plan is retried after each real runtime fault.
func TestValidatorActivationStakeRejectsInsufficientAndAmbiguousCapacity(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	for _, stakes := range [][]uint64{{500, 0, 100, 99, 0, 0}, {500, 0, 8000, 0, 0, 0}, {500, 0, 8000, 1000, uint64(1) << 62, 0}} {
		f.setStakes(t, stakes)
		if observed, err := f.read(t, t.Context()); err == nil || observed != nil {
			t.Fatal("unproved weighted capacity accepted", stakes, observed, err)
		}
	}
	f.setStakes(t, []uint64{500, 0, 8000, 1000, 0, 0})
	f.activation.chain.census.set(t, "Kappa", []byte{0, 0}, []byte{25, 0})
	if observed, err := f.read(t, t.Context()); err == nil || observed != nil {
		t.Fatal("extreme kappa was treated as ordinary majority", observed, err)
	}
}

// All peers are cross-checked against the original complete census, including
// peers that currently have no validator permit and contribute no active row.
func TestValidatorActivationStakeRejectsRuntimePeerSubstitution(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	for _, fault := range []string{"peer-hotkey", "peer-permit", "extra-field", "truncated-stake"} {
		f.stateLock.Lock()
		original := map[int][]byte{}
		for index, value := range f.fields {
			original[index] = bytes.Clone(value)
		}
		switch fault {
		case "peer-hotkey":
			f.fields[52][1+5*32] ^= 1
		case "peer-permit":
			f.fields[57][1+5] = 1
		case "extra-field":
			f.fields[72] = []byte{0}
		case "truncated-stake":
			f.fields[69] = f.fields[69][:len(f.fields[69])-1]
		}
		before := f.runtimeCalls
		f.stateLock.Unlock()
		observed, err := f.read(t, t.Context())
		f.stateLock.Lock()
		reached := f.runtimeCalls == before+1
		f.fields = original
		f.stateLock.Unlock()
		if err == nil || observed != nil || !reached {
			t.Fatal("runtime substitution escaped or did not reach its source", fault, observed, err, reached)
		}
	}
}

// The final numeric native recheck occurs after the activity response, beyond
// both the original readiness and the stake reader's own canonical fences.
func TestValidatorActivationStakeRejectsClosingCanonicalChange(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	key, _ := types.CreateStorageKey(f.activation.chain.census.metadata, "SubtensorModule", "LastUpdate", []byte{25, 0})
	activity, reached := false, false
	f.fault = func(call validatorActivationNativeTestCall, response *http.Response) (*http.Response, error) {
		if f.runtimeCalls > 0 && call.Method == "state_getStorage" && string(call.Params[0]) == `"`+key.Hex()+`"` {
			activity = true
		}
		if activity && call.Method == "chain_getBlockHash" && string(call.Params[0]) == "100" {
			reached = true
			return validatorActivationNativeTestReply(response, "0x"+strings.Repeat("d", 64))
		}
		return response, nil
	}
	observed, err := f.read(t, t.Context())
	f.stateLock.Lock()
	actualReached := reached
	f.stateLock.Unlock()
	if err == nil || observed != nil || !actualReached {
		t.Fatal("closing native contradiction retained stake", observed, err, actualReached)
	}
}

// Cancellation is forced at the actual local HTTP runtime request and every
// handler/reader joins before the fixture is released; no sleep orders it.
func TestValidatorActivationStakeCancellationDiscardsAndJoins(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	f.barrier = func(ctx context.Context) {
		close(entered)
		select {
		case <-ctx.Done():
		case <-release:
		}
	}
	var observed *validatorActivationStakeReadiness
	var err error
	t.Cleanup(func() { cancel(); releaseOnce.Do(func() { close(release) }); <-done })
	go func() { defer close(done); observed, err = f.read(t, ctx) }()
	select {
	case <-entered:
	case <-done:
		t.Fatal("runtime transport barrier not reached", err)
	}
	cancel()
	releaseOnce.Do(func() { close(release) })
	<-done
	if !errors.Is(err, context.Canceled) || observed != nil {
		t.Fatal("canceled runtime read retained capacity", observed, err)
	}
}

// Unsupported current activity codecs are independently approved synthetic
// inputs, not modifications to a plan after original custody was acquired.
func TestValidatorActivationStakeRejectsApprovedActivityCodecChange(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, func(metadata *types.Metadata) {
		for p := range metadata.AsMetadataV14.Pallets {
			for i := range metadata.AsMetadataV14.Pallets[p].Storage.Items {
				entry := &metadata.AsMetadataV14.Pallets[p].Storage.Items[i]
				if entry.Name == "ActivityCutoffFactorMilli" {
					entry.Type.AsMap.Hashers[0] = types.StorageHasherV10{IsTwox64Concat: true}
				}
			}
		}
	})
	observed, err := f.read(t, t.Context())
	f.stateLock.Lock()
	reads := f.runtimeCalls
	f.stateLock.Unlock()
	if err == nil || observed != nil || reads != 0 || !strings.Contains(err.Error(), "ActivityCutoffFactorMilli") {
		t.Fatal("approved unsupported activity codec reached census or was decoded", observed, err, reads)
	}
}

// Only the single fixed runtime API query extends this owned read profile;
// unpinned blocks, other runtime methods, writes and subscriptions stay closed.
func TestValidatorActivationStakeRpcProfileRejectsUnscopedMethods(t *testing.T) {
	client, err := newRpcClient("http://stake-rpc.example", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		count++
		return nil, errors.New("unexpected transport access")
	})
	bridge := &validatorActivationStakeReadClient{evmNativeReadClient: &evmNativeReadClient{client: client}, input: "0x19001400001e00340039004500", block: testFinalizedHash}
	for _, args := range [][]any{{}, {"other_method", bridge.input, bridge.block}, {"SubnetInfoRuntimeApi_get_selective_metagraph", "0x00", bridge.block}, {"SubnetInfoRuntimeApi_get_selective_metagraph", bridge.input, testGenesisHash}, {"SubnetInfoRuntimeApi_get_selective_metagraph", bridge.input, bridge.block, "extra"}} {
		if err := bridge.CallContext(t.Context(), new(json.RawMessage), "state_call", args...); err == nil {
			t.Fatal("unscoped runtime query admitted", args)
		}
	}
	if err := client.call(t.Context(), "state_call", []any{}, new(json.RawMessage)); err == nil {
		t.Fatal("runtime method leaked into generic profile")
	}
	if err := bridge.CallContext(t.Context(), new(string), "author_submitExtrinsic", "0x00"); err == nil {
		t.Fatal("native bridge admitted submission")
	}
	if err := bridge.Call(new(string), "state_call"); err == nil {
		t.Fatal("native bridge admitted a contextless call")
	}
	if _, err := bridge.Subscribe(t.Context(), "", "", "", "", nil); err == nil {
		t.Fatal("native bridge acquired a subscription")
	}
	if count != 0 {
		t.Fatal("rejected scope reached owned transport", count)
	}
}
