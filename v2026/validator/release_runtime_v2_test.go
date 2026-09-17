//go:build linux || darwin

// The actual root opens dormant disks, closes elapsed epochs, signs protected
// requests for every original source, and recovers exact public consents.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Epoch geometry is supplied by the actual hash-pinned contract transport,
// independently of candidate ledger roots or terminal payloads.
type releaseRuntimeV2ChainTestFixture struct{ *releaseStartupV2TestFixture }

func (self *releaseRuntimeV2ChainTestFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical || common.BytesToAddress(call["to"]) != common.HexToAddress(self.cfg.Coordinator) {
		return nil, errors.New("runtime fixture requires exact canonical hash selectors")
	}
	found := false
	for _, hash := range self.blocks {
		if common.Hash(hash) == *selector.BlockHash {
			found = true
		}
	}
	if !found {
		return nil, errors.New("runtime fixture view has no independent block")
	}
	coordinator := stabi.NewSTCoordinator()
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	for epoch := uint64(7); epoch <= 10; epoch++ {
		if bytes.Equal(call["input"], coordinator.PackEpochStartBlock(new(big.Int).SetUint64(epoch))) {
			return parsed.Methods["epochStartBlock"].Outputs.Pack(new(big.Int).SetUint64(1001 + 500*(epoch-7)))
		}
		if bytes.Equal(call["input"], coordinator.PackEpochEndBlock(new(big.Int).SetUint64(epoch))) {
			return parsed.Methods["epochEndBlock"].Outputs.Pack(new(big.Int).SetUint64(1501 + 500*(epoch-7)))
		}
	}
	return self.releaseStartupV2TestFixture.Call(ctx, call, selector)
}

// Each destination owns its current SDK session and records the original
// source proven by the actual reserved upload header, not a test callback.
type releaseRuntimeV2HttpTestStore struct {
	stateLock     sync.Mutex
	objects       map[string][]byte
	postsBySource map[uint64]int
}

func (self *releaseRuntimeV2HttpTestStore) counts() map[uint64]int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	result := map[uint64]int{}
	for noId, count := range self.postsBySource {
		result[noId] = count
	}
	return result
}

type releaseRuntimeV2TestFixture struct {
	startup  *releaseStartupV2TestFixture
	runtime  *releaseRuntimeV2
	runtimes []*releaseOperatorRuntime
	stores   [2]*releaseRuntimeV2HttpTestStore
	origins  [2]string
	hotkey   *crv4.Keypair
}

// Complete real source/destination owners are attached only after semantic
// startup authenticated the dormant disk census and native/Evm history.
func newReleaseRuntimeV2TestFixture(t *testing.T) *releaseRuntimeV2TestFixture {
	return newReleaseRuntimeV2TestFixtureWithBounds(t, nil)
}

// Bounds are independently supplied before startup, ledger and HTTP ownership.
func newReleaseRuntimeV2TestFixtureWithBounds(t *testing.T, bounds *ReleaseEvidenceV2Bounds) *releaseRuntimeV2TestFixture {
	t.Helper()
	startup := newReleaseStartupV2TestFixtureWithBounds(t, false, bounds)
	startup.cfg.EvidenceV2.UploadIntentSeconds = 300
	if bounds == nil {
		startup.cfg.EvidenceV2.Bounds.Cut.Records.MaxPageBytes = 256 * 1024
	}
	if err := startup.cfg.Validate(); err != nil {
		t.Fatalf("runtime fixture changed reviewed production configuration: %v", err)
	}
	for _, number := range []uint64{1500, 1501, 2000, 2001} {
		startup.blocks[number] = [32]byte{byte(number / 500), byte(number % 500), 0x75}
	}
	startup.finalized = 2001
	rpcServer := gethrpc.NewServer()
	if err := rpcServer.RegisterName("eth", &releaseRuntimeV2ChainTestFixture{releaseStartupV2TestFixture: startup}); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(rpcServer)
	t.Cleanup(func() { endpoint.Close(); rpcServer.Stop() })
	chain, err := DialReleaseChainContext(t.Context(), []string{endpoint.URL}, common.HexToAddress(startup.cfg.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.Close)
	startup.chain = chain
	self := &releaseRuntimeV2TestFixture{startup: startup}
	for index, input := range startup.inputs {
		store := &releaseRuntimeV2HttpTestStore{objects: map[string][]byte{}, postsBySource: map[uint64]int{}}
		credential := "runtime-v2-destination-" + strconv.Itoa(index+1)
		apiEndpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/verify/keys" && request.Method == http.MethodGet {
				value := sdk.VerifyKeysResult{}
				for version, key := range startup.keys[input.Config.NoID] {
					value.Keys = append(value.Keys, &sdk.VerifyServerKey{ServerKeyId: int32(version), PublicKey: key})
				}
				writer.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(writer).Encode(value); err != nil {
					t.Error(err)
				}
				return
			}
			kind, hash := request.URL.Query().Get("kind"), request.URL.Query().Get("hash")
			if request.URL.Path != "/sn/attempt-artifact" || len(request.URL.Query()) != 2 {
				t.Error("runtime used an unexpected object route")
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			key := kind + "/" + hash
			if request.Method == http.MethodPost {
				raw, err := io.ReadAll(request.Body)
				intent, signatureErr := protocol.VerifyValidatorAttemptUploadHeader(request.Header.Get(protocol.ValidatorAttemptUploadHeader))
				session, sessionErr := protocol.ValidatorAttemptUploadSessionHash(credential)
				source := uint64(0)
				for _, candidate := range startup.inputs {
					digest, digestErr := candidate.Context.Activation.Digest()
					if digestErr == nil && intent.ActivationHash == digest && intent.VPK == candidate.Context.Activation.VPK {
						source = candidate.Config.NoID
					}
				}
				if err != nil || signatureErr != nil || sessionErr != nil || source == 0 || intent.ReplicaNoID != input.Config.NoID || intent.SessionHash != session || request.Header.Get("Authorization") != "Bearer "+credential || intent.ContentHash != sha256.Sum256(raw) || intent.Size != uint64(len(raw)) || hash != attemptHex32(sha256.Sum256(raw)) {
					t.Errorf("runtime lost original protected source/destination binding: %v / %v", err, signatureErr)
					writer.WriteHeader(http.StatusForbidden)
					return
				}
				func() {
					store.stateLock.Lock()
					defer store.stateLock.Unlock()
					store.objects[key] = bytes.Clone(raw)
					store.postsBySource[source]++
				}()
				writer.Header().Set("ETag", "\""+hash+"\"")
				writer.WriteHeader(http.StatusNoContent)
				return
			}
			if request.Method != http.MethodGet || request.Header.Get("Authorization") != "" {
				t.Error("public replay leaked a credential or changed method")
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			raw := func() []byte {
				store.stateLock.Lock()
				defer store.stateLock.Unlock()
				return bytes.Clone(store.objects[key])
			}()
			if raw == nil {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			contentType := "application/x-ndjson"
			if kind == "metadata" {
				contentType = "application/json"
			}
			writer.Header().Set("Content-Type", contentType)
			writer.Header().Set("Content-Length", strconv.Itoa(len(raw)))
			_, _ = writer.Write(raw)
		}))
		t.Cleanup(apiEndpoint.Close)
		strategy := connect.NewClientStrategyWithDefaults(t.Context())
		api := sdk.NewApi(t.Context(), strategy, apiEndpoint.URL)
		api.SetByJwt(credential)
		t.Cleanup(func() {
			if err := api.CloseAndWait(context.Background()); err != nil {
				t.Error(err)
			}
			strategy.Close()
		})
		startup.cfg.Operators[index].APIURL = apiEndpoint.URL
		upload, err := newReleaseAttemptUploadV2(t.Context(), startup.cfg.Operators[index], startup.cfg.EvidenceV2.Bounds, api.GetByJwt)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(upload.close)
		self.runtimes = append(self.runtimes, &releaseOperatorRuntime{measurement: &ReleaseMeasurementContext{NoID: input.Config.NoID}, attemptUpload: upload})
		self.stores[index], self.origins[index] = store, apiEndpoint.URL
	}
	self.hotkey, err = crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	self.start(t)
	startup.prepareEngines(t)
	return self
}

func (self *releaseRuntimeV2TestFixture) start(t *testing.T) {
	t.Helper()
	runtime, err := newReleaseRuntimeV2WithRuntime(t.Context(), &self.startup.cfg, self.startup.chain, self.startup.nativeFixture.chain, self.hotkey, self.startup.inputs, self.startup.keys, self.origins, self.startup.disk, self.startup.nativeFixture.expected)
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range self.runtimes {
		noId := destination.measurement.NoID
		destination.measurement.Stats = self.startup.disk.states[noId].stats
		destination.attemptLedger = self.startup.disk.states[noId].ledger
		destination.attemptSource = runtime.sources[noId]
	}
	if err := runtime.attach(self.runtimes); err != nil {
		t.Fatal(err)
	}
	self.runtime = runtime
}

func TestReleaseRuntimeV2ClosesMissedEpochsAndPublishesProtectedSourceCensus(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	fixture.startup.trail(t, 0)
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	for _, participant := range fixture.runtime.history.participants {
		if fixture.runtime.history.current[participant.NoID].epoch != 9 {
			t.Fatal("missed epochs were not closed consecutively")
		}
	}
	for _, epoch := range []uint64{7, 8} {
		path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, epoch)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, fixture.startup.cfg.EvidenceV2.Bounds.MaxClosureBytes, 2)
		if err != nil {
			t.Fatal(err)
		}
		options := ValidatorEvidencePublicationV2ReadOptions{Origins: fixture.origins, Bounds: fixture.startup.cfg.EvidenceV2.Bounds, Window: protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: 1001 + 500*(epoch-7), EndBlock: 1501 + 500*(epoch-7), FinalizedBlock: 2001}}
		for _, input := range fixture.startup.inputs {
			options.Activations = append(options.Activations, input.Context.Activation)
		}
		publication, err := ReadValidatorEvidencePublicationV2(t.Context(), manifest, options)
		if err != nil || len(publication.Members) != 2 {
			t.Fatalf("actual closed public census: %v", err)
		}
	}
	for _, store := range fixture.stores {
		counts := store.counts()
		if len(counts) != len(fixture.startup.inputs) {
			t.Fatalf("destination source census differs from the original signed owners: %+v", counts)
		}
		for _, input := range fixture.startup.inputs {
			if input.Config.NoID != input.Context.Activation.NoID || counts[input.Context.Activation.NoID] == 0 {
				t.Fatalf("destination did not receive actual signed source %d: %+v", input.Context.Activation.NoID, counts)
			}
		}
	}
}

func TestReleaseRuntimeV2ActualDiskRestartReusesPublishedConsentsWithoutUploads(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	before := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
	fixture.startup.reopen(t)
	fixture.start(t)
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	for index, store := range fixture.stores {
		if !maps.Equal(before[index], store.counts()) {
			t.Fatal("actual disk restart changed the complete original published source census")
		}
	}
}

func TestReleaseRuntimeV2WaitingEpochOwnerCancelsWithoutPublicIo(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	release, err := fixture.runtime.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = fixture.runtime.advance(ctx, &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]})
	release()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting owner ignored cancellation: %v", err)
	}
	for _, store := range fixture.stores {
		if len(store.counts()) != 0 {
			t.Fatal("canceled owner started protected publication")
		}
	}
}

// The original root must keep refusing a tiny fixture hash in production
// configuration. It cannot publish state or start historical native reads.
func TestReleaseRuntimeV2RejectsUnreviewedConfiguredArtifact(t *testing.T) {
	fixture := newReleaseStartupV2TestFixture(t, false)
	fixture.cfg.EvidenceV2.UploadIntentSeconds = 300
	fixture.cfg.RuntimeCodeHash = fixture.nativeFixture.expected.CodeHash
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	beforeCalls := len(fixture.nativeFixture.calls)
	before := releaseStartupV2TestDiskImages(t, fixture.disk)
	runtime, err := newReleaseRuntimeV2(t.Context(), &fixture.cfg, fixture.chain, fixture.nativeFixture.chain, hotkey, fixture.inputs, fixture.keys, [2]string{fixture.replicas[0].Origin, fixture.replicas[1].Origin}, fixture.disk)
	if err == nil || runtime != nil || !strings.Contains(err.Error(), "not the reviewed") {
		t.Fatalf("production root admitted an unreviewed artifact: %v", err)
	}
	if len(fixture.nativeFixture.calls) != beforeCalls {
		t.Fatal("invalid production artifact started native history reads")
	}
	after := releaseStartupV2TestDiskImages(t, fixture.disk)
	for index := range before {
		if !bytes.Equal(before[index], after[index]) {
			t.Fatal("invalid production artifact published a partial disk batch")
		}
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// A real ordinary input forces native metadata/storage/schedule reads. An
// explicit fixture pin is independently checked, never an eligibility verdict.
func TestReleaseRuntimeV2ExplicitArtifactStillAuthenticatesNativeHistory(t *testing.T) {
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.cfg.EvidenceV2.UploadIntentSeconds = 300
	fixture.trail(t, 0)
	fixture.ordinary(t, 0, 1, false)
	fixture.reopen(t)
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	origins := [2]string{fixture.replicas[0].Origin, fixture.replicas[1].Origin}
	beforeCalls := len(fixture.nativeFixture.calls)
	runtime, err := newReleaseRuntimeV2WithRuntime(t.Context(), &fixture.cfg, fixture.chain, fixture.nativeFixture.chain, hotkey, fixture.inputs, fixture.keys, origins, fixture.disk, fixture.nativeFixture.expected)
	if err != nil || runtime == nil || len(fixture.nativeFixture.calls) <= beforeCalls {
		t.Fatalf("runtime did not consume genuine native history: %v", err)
	}
	if runtime.cfg.RuntimeCodeHash != releaseRuntimeCodeHash || runtime.cfg.RuntimeMetadataHash != releaseRuntimeMetadataHash {
		t.Fatal("explicit native fixture replaced production configuration pins")
	}
	fixture.reopen(t)
	wrong := fixture.nativeFixture.expected
	wrong.CodeHash = common.Hash{0x99}.Hex()
	beforeCalls = len(fixture.nativeFixture.calls)
	before := releaseStartupV2TestDiskImages(t, fixture.disk)
	runtime, err = newReleaseRuntimeV2WithRuntime(t.Context(), &fixture.cfg, fixture.chain, fixture.nativeFixture.chain, hotkey, fixture.inputs, fixture.keys, origins, fixture.disk, wrong)
	if err == nil || runtime != nil || len(fixture.nativeFixture.calls) <= beforeCalls {
		t.Fatalf("changed explicit artifact bypassed the real native reader: %v", err)
	}
	after := releaseStartupV2TestDiskImages(t, fixture.disk)
	for index := range before {
		if !bytes.Equal(before[index], after[index]) {
			t.Fatal("failed native authentication published a partial disk batch")
		}
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}
