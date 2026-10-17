//go:build linux || darwin

// Continuation uses separately approved zero-price economics with genuine M8
// work, independent chain observations and real immutable input/intent files.
// It does not claim qualification of paid capture or final economic outcomes.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The histories originate at the actual Stats detach and immutable journal
// owner before the enclosing artifact and production sidecar are signed.
type productionContinuationTestFixture struct {
	production *ownerRecycleProductionTestFixture
	runtime    *releaseRuntimeV2
	steerer    *ReleaseSteerer
	inputKVs   map[uint64]*releaseStatsV2RuntimeTestFixture
	journalKVs map[uint64]*releaseMeasurementInputJournal
	contextKVs map[uint64]AttemptCutV2Context
	intent     *SteeringIntent
	envelope   []byte
	// Public attempt-object requests the replay owner has made.
	artifactReads atomic.Int64
}

// No current binding claims a local key; each real historical provider remains
// independently replayed into the operator pool under the signed zero-price policy.
func newProductionContinuationTestFixture(t *testing.T) *productionContinuationTestFixture {
	return newProductionContinuationTestFixtureWithStartup(t, nil, nil)
}

// Additional activation/configuration inputs are fixed before proof sealing
// and independent approval. Neither callback can bypass an authority reader.
func newProductionContinuationTestFixtureWithStartup(t *testing.T, anchor func(*recycleAdmissionFixture, *attemptCutV2SealTestFixture), configure func(*ownerRecycleProductionTestFixture, *productionContinuationTestFixture)) *productionContinuationTestFixture {
	return newProductionContinuationTestFixtureWithStartupThrough(t, 200, anchor, configure)
}

// Long receipt scans select their finite read window before the independent
// approval, source envelope and original native intent are signed.
func newProductionContinuationTestFixtureThrough(t *testing.T, through uint64) *productionContinuationTestFixture {
	return newProductionContinuationTestFixtureWithStartupThrough(t, through, nil, nil)
}

// Startup and long-scan fixtures compose their inputs before the same real
// independent approval. The ordinary startup window remains unchanged.
func newProductionContinuationTestFixtureWithStartupThrough(t *testing.T, through uint64, anchor func(*recycleAdmissionFixture, *attemptCutV2SealTestFixture), configure func(*ownerRecycleProductionTestFixture, *productionContinuationTestFixture)) *productionContinuationTestFixture {
	t.Helper()
	return newProductionContinuationTestFixtureWithWork(t, through, recycleQualityTestCompleted, anchor, configure)
}

// The genuine M8 work census is fixed before any proof is signed. Too little
// completed work leaves every pool below its quality minimum; with no current
// binding, the provider allocation is then empty.
func newProductionContinuationTestFixtureWithWork(t *testing.T, through uint64, completed int, anchor func(*recycleAdmissionFixture, *attemptCutV2SealTestFixture), configure func(*ownerRecycleProductionTestFixture, *productionContinuationTestFixture)) *productionContinuationTestFixture {
	t.Helper()
	self := &productionContinuationTestFixture{inputKVs: map[uint64]*releaseStatsV2RuntimeTestFixture{}, journalKVs: map[uint64]*releaseMeasurementInputJournal{}, contextKVs: map[uint64]AttemptCutV2Context{}}
	policy := productionContinuationTestPolicy(t)
	create := func(t *testing.T, hotkey [32]byte) *recycleOperatorFixture {
		sources := productionContinuationTestSourcesFor(t, completed)
		if hotkey != sources.hotkey {
			t.Fatal("continuation sources were signed for another validator hotkey")
		}
		return newRecycleOperatorFixtureWithMeasurement(t, func(admission *recycleAdmissionFixture, _ *releaseMeasurementV2TestFixture) {
			sources.install(t, admission)
			sources.use(self)
		}, func(setup func(*recycleAdmissionFixture, *releaseMeasurementV2TestFixture)) *recycleMeasurementFixture {
			measurement := newRecycleMeasurementFixtureWithProvider(t, newRecycleMeasurementTestAdmission(t, &policy), copyReleaseMeasurementTestProvider(t, sources.provider), setup, hotkey)
			if !bytes.Equal(measurement.encoded, sources.encoded) {
				t.Fatal("continuation artifact differs from its shared sources")
			}
			return measurement
		})
	}
	if anchor != nil {
		// An activation anchor belongs to this test's own startup records.
		create = func(t *testing.T, hotkey [32]byte) *recycleOperatorFixture {
			sources := newProductionContinuationTestSources()
			return newRecycleOperatorFixtureWithMeasurement(t, func(admission *recycleAdmissionFixture, provider *releaseMeasurementV2TestFixture) {
				sources.detach(t, admission, provider, completed)
				sources.use(self)
			}, func(setup func(*recycleAdmissionFixture, *releaseMeasurementV2TestFixture)) *recycleMeasurementFixture {
				admission := newRecycleMeasurementTestAdmission(t, &policy)
				return newRecycleMeasurementFixtureWithProvider(t, admission, newRecycleActivatedMeasurementTestProvider(t, admission, recycleQualityTestProviders, completed, hotkey, anchor), setup, hotkey)
			})
		}
	}
	self.production = newOwnerRecycleProductionTestFixtureWithInputs(t, create, func(production *ownerRecycleProductionTestFixture) {
		production.operator.measurement.admission.approval.ValidThroughNativeBlock = through
		cfg := production.cfg
		bounds := &cfg.EvidenceV2.Bounds
		bounds.MaxHistoryBytes, bounds.MaxInputJournalBytes = 32*1024*1024, 1024*1024
		bounds.MaxHeadEntries, bounds.MaxProviders, bounds.MaxEgressHashes, bounds.MaxFleetPrefixes = 128, 64, 1024, 64
		bounds.MaxParticipants = 2
		bounds.Cut, bounds.Replay = self.inputKVs[9].source.bounds, self.inputKVs[9].source.replay
		if configure != nil {
			configure(production, self)
		}
	})
	production, cfg := self.production, self.production.cfg
	history := &releaseEvidenceV2StartupHistory{cfg: *cfg, initial: map[uint64]ReleaseEvidenceV2ActivationContext{}, keys: map[uint64]map[byte]ed25519.PublicKey{},
		inputByEpoch: map[uint64]map[uint64]*releaseMeasurementInputJournal{production.epoch: self.journalKVs}, inputContextsByEpoch: map[uint64]map[uint64]AttemptCutV2Context{production.epoch: self.contextKVs}}
	for _, noId := range []uint64{9, 10} {
		physical := self.inputKVs[noId]
		history.initial[noId] = ReleaseEvidenceV2ActivationContext{InitialCut: physical.source.expected}
		history.keys[noId] = physical.source.server.serverPublicKeys()
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: noId, StateDir: physical.dir, Stats: physical.stats, Ledger: physical.ledger})
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		self.artifactReads.Add(1)
		kind, hash := request.URL.Query().Get("kind"), request.URL.Query().Get("hash")
		if request.Method != http.MethodGet || request.URL.Path != "/sn/attempt-artifact" || len(request.URL.Query()) != 2 || request.Header.Get("Authorization") != "" {
			t.Error("continuation replay changed the public object route")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var raw []byte
		for _, physical := range self.inputKVs {
			if kind == "metadata" {
				raw = physical.objects.metadataKVs[hash]
			} else {
				raw = physical.objects.dataKVs[kind][hash]
			}
			if raw != nil {
				break
			}
		}
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
	t.Cleanup(server.Close)
	reader, err := NewHTTPAttemptStreamV2Reader(server.URL, cfg.EvidenceV2.Bounds.Cut)
	if err != nil {
		t.Fatal(err)
	}
	history.readers = []*HTTPAttemptStreamV2Reader{reader}
	self.runtime = &releaseRuntimeV2{receiptCache: &productionReceiptCacheState{}, ctx: t.Context(), cfg: *cfg, native: production.operator.measurement.admission.chain, chain: production.operator.chain, hotkey: production.hotkey, history: history, gate: make(chan struct{}, 1)}
	store, err := newReleaseIntentStoreV2(self.runtime)
	if err != nil {
		t.Fatal(err)
	}
	self.steerer = &ReleaseSteerer{cfg: &self.runtime.cfg, runtimeV2: self.runtime, native: self.runtime.native, chain: self.runtime.chain, hotkey: self.runtime.hotkey, intents: store}
	stage, provider := production.stage(t)
	self.intent, self.envelope = production.intentAndEnvelope(t, stage, provider)
	measurement := production.operator.measurement
	self.intent.MeasurementArtifactPath, self.intent.MeasurementArtifactSize, err = persistReleaseMeasurementArtifact(cfg.StateDir, measurement.encoded, self.intent.MeasurementArtifactHash)
	if err != nil {
		t.Fatal(err)
	}
	self.intent.MeasurementEnvelopePath, self.intent.MeasurementEnvelopeSize, err = persistReleaseMeasurementEnvelope(cfg.StateDir, self.envelope, self.intent.MeasurementEnvelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	return self
}

// Continuation uses separately approved zero-price economics.
func productionContinuationTestPolicy(t testing.TB) protocol.Policy {
	t.Helper()
	policy := recycleTestInput(t).ParentPolicy
	policy.Deposit.Tiers = slices.Clone(policy.Deposit.Tiers)
	for index := range policy.Deposit.Tiers {
		policy.Deposit.Tiers[index].RateNumeratorRaoPerGiB = 0
		policy.Deposit.Tiers[index].RateNumeratorRaoPerUser = 0
	}
	policy.Deposit.ZeroRateAction = protocol.DepositZeroRateEqualDemand
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	return policy
}

// Both operators' genuine M8 work, its compact cuts and the input journals the
// actual Stats owner detached from it. Without an activation anchor these
// depend only on the completed census, so each census is built once per test
// process and shared read-only: every test still signs its own approval and
// owns its chain fixtures, intent store and validator state directory.
type productionContinuationTestSources struct {
	hotkey     [32]byte
	provider   *releaseMeasurementV2TestFixture
	encoded    []byte
	inputKVs   map[uint64]*releaseStatsV2RuntimeTestFixture
	journalKVs map[uint64]*releaseMeasurementInputJournal
	contextKVs map[uint64]AttemptCutV2Context
	rawKVs     map[uint64][]byte
	// What every sharing test leaves unchanged.
	headKVs     map[uint64]AttemptLedgerHead
	snapshotKVs map[uint64]statsSnapshot
	statsKVs    map[uint64][]byte
}

var productionContinuationTestSourcesCache struct {
	stateLock sync.Mutex
	censusKVs map[int]*productionContinuationTestSources
}

func newProductionContinuationTestSources() *productionContinuationTestSources {
	return &productionContinuationTestSources{inputKVs: map[uint64]*releaseStatsV2RuntimeTestFixture{}, journalKVs: map[uint64]*releaseMeasurementInputJournal{},
		contextKVs: map[uint64]AttemptCutV2Context{}, rawKVs: map[uint64][]byte{}, headKVs: map[uint64]AttemptLedgerHead{}, snapshotKVs: map[uint64]statsSnapshot{}, statsKVs: map[uint64][]byte{}}
}

// The first test to need a census builds it through the same admission, real
// sources and actual detach as a private fixture, under the process's owner.
func productionContinuationTestSourcesFor(t *testing.T, completed int) *productionContinuationTestSources {
	t.Helper()
	cache := &productionContinuationTestSourcesCache
	cache.stateLock.Lock()
	defer cache.stateLock.Unlock()
	if sources := cache.censusKVs[completed]; sources != nil {
		return sources
	}
	owner := sharedTestFixtureOwner{T: t}
	keypair, err := crv4.KeypairFromSeed([32]byte{0x6a, 0x41})
	if err != nil {
		t.Fatal(err)
	}
	policy := productionContinuationTestPolicy(owner)
	sources := newProductionContinuationTestSources()
	sources.hotkey = keypair.PublicKey()
	admission := newRecycleMeasurementTestAdmission(owner, &policy)
	measurement := newRecycleMeasurementFixtureWithProvider(owner, admission, newRecycleMeasurementTestProvider(owner, admission, recycleQualityTestProviders, completed, sources.hotkey), func(admission *recycleAdmissionFixture, provider *releaseMeasurementV2TestFixture) {
		setRecycleOperatorTestAuditWindows(provider.artifact)
		sources.detach(owner, admission, provider, completed)
		provider.rebuildLegacy(owner)
	}, sources.hotkey)
	sources.provider, sources.encoded = measurement.provider, measurement.encoded
	for noID, physical := range sources.inputKVs {
		head, err := physical.ledger.Head()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(physical.dir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
		sources.headKVs[noID], sources.snapshotKVs[noID], sources.statsKVs[noID] = head, physical.stats.snapshotStats(), raw
	}
	if cache.censusKVs == nil {
		cache.censusKVs = map[int]*productionContinuationTestSources{}
	}
	cache.censusKVs[completed] = sources
	return sources
}

// The actual Stats cursor is taken before publication, then the real detach
// writes each operator's immutable input journal into the admission's state.
// Every provider is at or above the quality minimum exactly when the census is.
func (self *productionContinuationTestSources) detach(t testing.TB, admission *recycleAdmissionFixture, provider *releaseMeasurementV2TestFixture, completed int) {
	t.Helper()
	artifact, cfg := provider.artifact, admission.cfg
	minimum := artifact.Policy.Verify.ReliabilityAMin
	for index := range artifact.Bindings {
		binding := &artifact.Bindings[index]
		binding.Active, binding.LiveUIDFound, binding.LiveUID = false, false, 0
		binding.LocalClientKey = releaseHex32([32]byte{})
	}
	for index, input := range artifact.Inputs {
		operator := provider.operators[input.NoID]
		source := operator.seal
		physical := releaseStatsV2RuntimeTestFixtureFor(t, source, source.engine.stats, source.ledger)
		// Standalone seal fixtures use a separately selected generation;
		// continuation takes the actual Stats cursor before publication.
		expected, err := physical.stats.releaseStatsV2Context(t.Context(), source.expected.Boundary, physical.options)
		if err != nil {
			t.Fatal(err)
		}
		source.expected, self.contextKVs[input.NoID] = expected, expected
		if err := physical.stats.Save(physical.dir); err != nil {
			t.Fatal(err)
		}
		for _, root := range []string{"replay", "seal"} {
			if err := os.Mkdir(filepath.Join(physical.dir, root), 0700); err != nil {
				t.Fatal(err)
			}
		}
		configureProductionContinuationTestOperator(cfg, index, input.NoID, physical.dir)
		steerer := &ReleaseSteerer{cfg: cfg, contexts: map[uint64]*ReleaseMeasurementContext{input.NoID: {NoID: input.NoID, Stats: physical.stats}}}
		hash, err := parseReleaseHex32("continuation EVM boundary", source.expected.Boundary.EVMBlockHash, false)
		if err != nil {
			t.Fatal(err)
		}
		options := releaseMeasurementInputV2Options{Stats: physical.fresh(t), MaxJournalBytes: 1024 * 1024}
		actual, err := steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), input.NoID, artifact.SubnetEpoch, 100, admission.finalized.Hex(), &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(source.expected.Boundary.SettlementEpoch), BlockNumber: source.expected.Boundary.EVMBlock, BlockHash: hash}, options)
		if err != nil {
			t.Fatalf("actual continuation input detach: %v", err)
		}
		if len(actual.Stats.Providers) != recycleQualityTestProviders-1 {
			t.Fatalf("continuation census assigned %d providers, want every provider but the seed", len(actual.Stats.Providers))
		}
		for _, measured := range actual.Stats.Providers {
			if completed >= int(minimum) && measured.Assignments < minimum || completed < int(minimum)-1 && measured.Assignments >= minimum {
				t.Fatalf("continuation census is on the wrong side of the quality minimum: %d assignments", measured.Assignments)
			}
		}
		path := releaseMeasurementInputV2Path(cfg.StateDir, artifact.SubnetEpoch, input.NoID)
		raw, err := readReleaseMeasurementInputV2Context(t.Context(), path, options.MaxJournalBytes, releaseMeasurementInputV2ReadHooks{})
		if err != nil {
			t.Fatal(err)
		}
		journal, err := decodeReleaseMeasurementInputV2(t.Context(), raw, options)
		if err != nil {
			t.Fatal(err)
		}
		self.inputKVs[input.NoID], self.journalKVs[input.NoID], self.rawKVs[input.NoID] = physical, journal, raw
		operator.metadata, operator.data = physical.options.Seal.ReadMetadata, physical.options.Seal.OpenData
		artifact.Inputs[index] = actual
	}
}

// Each operator's retained state directory and its scratch roots inside it.
func configureProductionContinuationTestOperator(cfg *ReleaseConfig, index int, noID uint64, dir string) {
	cfg.Operators[index].StateDir = dir
	cfg.EvidenceV2.Operators = append(cfg.EvidenceV2.Operators, ReleaseEvidenceV2OperatorConfig{NoID: noID, ReplayScratchRoot: filepath.Join(dir, "replay"), SealScratchRoot: filepath.Join(dir, "seal")})
}

// A test's own configuration declares the shared operator directories, and
// the actual immutable writer publishes the detached journal bytes into its
// own state directory. Its cleanup confirms the sources are as built.
func (self *productionContinuationTestSources) install(t *testing.T, admission *recycleAdmissionFixture) {
	t.Helper()
	artifact, cfg := self.provider.artifact, admission.cfg
	for index, input := range artifact.Inputs {
		configureProductionContinuationTestOperator(cfg, index, input.NoID, self.inputKVs[input.NoID].dir)
		if err := writeReleaseMeasurementInputV2(releaseMeasurementInputV2Path(cfg.StateDir, artifact.SubnetEpoch, input.NoID), self.rawKVs[input.NoID], 1024*1024); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { self.check(t) })
}

// The fixture replays the sources' operators, journals and cut contexts.
func (self *productionContinuationTestSources) use(fixture *productionContinuationTestFixture) {
	for noID, physical := range self.inputKVs {
		fixture.inputKVs[noID], fixture.journalKVs[noID], fixture.contextKVs[noID] = physical, self.journalKVs[noID], self.contextKVs[noID]
	}
}

// A test that changed a shared ledger, Stats owner or snapshot fails, and the
// next test builds its census again.
func (self *productionContinuationTestSources) check(t *testing.T) {
	t.Helper()
	for noID, physical := range self.inputKVs {
		head, headErr := physical.ledger.Head()
		raw, readErr := os.ReadFile(filepath.Join(physical.dir, "stats.json"))
		if headErr == nil && readErr == nil && head == self.headKVs[noID] && bytes.Equal(raw, self.statsKVs[noID]) && reflect.DeepEqual(physical.stats.snapshotStats(), self.snapshotKVs[noID]) {
			continue
		}
		t.Errorf("test changed operator %d's shared continuation sources: %v", noID, errors.Join(headErr, readErr))
		cache := &productionContinuationTestSourcesCache
		cache.stateLock.Lock()
		for completed, sources := range cache.censusKVs {
			if sources == self {
				delete(cache.censusKVs, completed)
			}
		}
		cache.stateLock.Unlock()
		return
	}
}

// Opening a new owner re-reads the actual canonical private intent and all
// signed evidence. No copied candidate or successful callback restores state.
func (self *productionContinuationTestFixture) restart(t *testing.T) *SteeringIntent {
	t.Helper()
	store, err := newReleaseIntentStoreV2(self.runtime)
	if err != nil {
		t.Fatal(err)
	}
	self.steerer.intents = store
	intent, err := store.currentV2(t.Context())
	if err != nil || intent == nil {
		t.Fatalf("actual nonempty V2 owner restart: %v", err)
	}
	return intent
}

// This is the fixture's first causal ownership check: the real begin publishes
// the exact signed vector, restart replays it, and a real update remains durable.
func TestProductionContinuationDurableIntentOwner(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	intent, err := fixture.steerer.intents.beginV2(t.Context(), *fixture.intent)
	if err != nil {
		t.Fatalf("actual nonempty V2 begin: %v", err)
	}
	before, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := fixture.restart(t)
	if restarted.VectorHash != intent.VectorHash || restarted.Prepared.ExtrinsicHex != intent.Prepared.ExtrinsicHex || restarted.CreatedAt != intent.CreatedAt {
		t.Fatal("durable restart changed the original signature or age")
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("read-only restart changed the stored intent: %v", err)
	}
	if err := fixture.steerer.intents.updateV2(t.Context(), intent.VectorHash, "pending", nil); err != nil {
		t.Fatalf("actual nonempty V2 update: %v", err)
	}
	restarted = fixture.restart(t)
	if restarted.Status != "pending" || restarted.Error != "" || restarted.Prepared.ExtrinsicHex != intent.Prepared.ExtrinsicHex || restarted.VectorHash != intent.VectorHash {
		t.Fatal("durable update lost the pending signature or invented failure")
	}
	raw, err := json.Marshal(restarted)
	if err != nil || !bytes.Contains(raw, []byte(fmt.Sprintf("\"subnet_epoch\":%d", intent.SubnetEpoch))) {
		t.Fatalf("durable intent has no original native epoch: %v", err)
	}
	if fixture.steerer.intents.v2.active.Load() {
		t.Fatal("durable owner remained locked after restart")
	}
}
