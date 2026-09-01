package main

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/stabi"
	"github.com/urnetwork/connect"
)

type scenarioAdversaryStub struct {
	evidence      *AdversaryCampaignEvidence
	started       bool
	startCalls    int
	stopCalls     int
	happyStarted  time.Time
	happyComplete time.Time
}

// shutdownCancellationAdversary emits one real failure, then waits for the
// campaign parent to cancel its second sample. The second result must not enter
// resilience evidence because it was manufactured by lifecycle teardown.
type shutdownCancellationAdversary struct {
	calls         atomic.Uint64
	secondEntered chan struct{}
}

// startCancellationAdversary exposes whether a rejected campaign start
// created any actor work.
type startCancellationAdversary struct {
	calls atomic.Uint64
}

// ID identifies the pre-canceled start probe.
func (self *startCancellationAdversary) ID() string { return "start-cancellation" }

// Sample records an actor launch which must never happen for a pre-canceled
// campaign parent.
func (self *startCancellationAdversary) Sample(context.Context, adversarySamplePhase, uint64) adversarySampleResult {
	self.calls.Add(1)
	return adversarySampleResult{Outcome: adversaryOutcomeSuccess}
}

// ID identifies the lifecycle probe independently of the release actor set.
func (self *shutdownCancellationAdversary) ID() string { return "shutdown-cancellation" }

// Sample separates an observable actor failure from the cancellation-only
// result with an explicit second-call barrier.
func (self *shutdownCancellationAdversary) Sample(ctx context.Context, _ adversarySamplePhase, _ uint64) adversarySampleResult {
	if self.calls.Add(1) == 1 {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "observable actor failure"}
	}
	close(self.secondEntered)
	<-ctx.Done()
	return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: ctx.Err().Error()}
}

func (self *scenarioAdversaryStub) Start(context.Context) error {
	self.started = true
	self.startCalls++
	return nil
}

func (self *scenarioAdversaryStub) MarkHappyPathStarted(at time.Time) {
	self.happyStarted = at
}

func (self *scenarioAdversaryStub) MarkHappyPathCompleted(at time.Time) {
	self.happyComplete = at
}

func (self *scenarioAdversaryStub) SetExpectedFaultTargets([]string) {}

func (self *scenarioAdversaryStub) Ready() bool { return self.started }

func (self *scenarioAdversaryStub) Stop(context.Context) (*AdversaryCampaignEvidence, error) {
	self.stopCalls++
	self.evidence.StartedBeforeHappyPath = !self.happyStarted.IsZero()
	self.evidence.StoppedAfterHappyPath = !self.happyComplete.IsZero()
	return self.evidence, nil
}

func (self *scenarioAdversaryStub) Snapshot() *AdversaryCampaignEvidence { return self.evidence }

func healthyAdversaryEvidence() *AdversaryCampaignEvidence {
	evidence := &AdversaryCampaignEvidence{
		Schema: "urnetwork-adversary-campaign-v1", Release: "1.0", MatrixHash: "0x" + strings.Repeat("ab", 32),
		StartedBeforeHappyPath: true, StoppedAfterHappyPath: true, MinimumSamplesPerActor: 10,
		MaximumActorErrorRatePPM: 10_000, MaximumP99Milliseconds: 15_000, MaximumAttackControlRatio: 20_000_000, Status: "stopped",
	}
	for _, id := range releaseAdversaryActorIDs {
		metrics := map[string]AdversaryMetricEvidence{}
		if id == "custody-boundary-emulation" {
			metrics["live_invalid_merkle_proof_rejections"] = AdversaryMetricEvidence{Samples: 1, Minimum: 2, Maximum: 2, Last: 2}
			metrics["live_merkle_state_mutations"] = AdversaryMetricEvidence{Samples: 1, Minimum: 0, Maximum: 0, Last: 0}
		}
		evidence.Actors = append(evidence.Actors, AdversaryActorEvidence{
			ID: id, VectorIDs: []string{"vector"}, Status: "stopped", Samples: 10, ControlSamples: 2, AttackSamples: 8,
			Successful: 10, P99LatencyMilliseconds: 10, Metrics: metrics,
		})
	}
	for _, id := range requiredAdversarialVectors {
		evidence.Vectors = append(evidence.Vectors, AdversaryVectorEvidence{
			ID: id, ExecutionMode: "bounded-emulation", ConcurrentCoverage: "continuous-emulation",
			ActorIDs: []string{releaseAdversaryActorIDs[0]}, LocalTests: []string{"sim-testnet/TestAdversarialMatrixCoversKnownBittensorVectors"},
			RequiredMetrics: []string{"sample_metric"}, MeasuredMetrics: []string{"sample_metric"}, SampleFloor: 10, Status: "pass",
		})
	}
	return evidence
}

func TestAdversarialMatrixCoversKnownBittensorVectors(t *testing.T) {
	cfg := testResolvedConfig(t)
	matrix, err := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		t.Fatal(err)
	}
	if len(matrix.Rows) != len(requiredAdversarialVectors) || matrix.Hash == "" {
		t.Fatalf("adversarial matrix rows=%d hash=%q", len(matrix.Rows), matrix.Hash)
	}
	if err := validateAdversarialActorCoverage(matrix, releaseAdversaryActorIDs); err != nil {
		t.Fatal(err)
	}
	if err := validateAdversarialMetricCoverage(matrix, releaseAdversaryMetricCatalog); err != nil {
		t.Fatal(err)
	}
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil || definition.AdversarialMatrixHash != matrix.Hash {
		t.Fatalf("release definition adversarial matrix=%q want=%q error=%v", definition.AdversarialMatrixHash, matrix.Hash, err)
	}
	production, err := scenarioDefinitionFor(cfg, "production-soak")
	if err != nil || production.AdversarialMatrixHash != matrix.Hash {
		t.Fatalf("production definition adversarial matrix=%q want=%q error=%v", production.AdversarialMatrixHash, matrix.Hash, err)
	}
}

func TestAdversarialMetricCoverageRejectsUnmeasuredRow(t *testing.T) {
	matrix := &AdversarialMatrix{Rows: []AdversarialMatrixRow{{
		ID: "unmeasured", ActorIDs: []string{"operator-api-pressure"}, Metrics: []string{"never_emitted"},
	}}}
	if err := validateAdversarialMetricCoverage(matrix, releaseAdversaryMetricCatalog); err == nil || !strings.Contains(err.Error(), "no metric emitted") {
		t.Fatalf("unmeasured matrix row error=%v", err)
	}
}

func TestAdversarialMatrixKeepsChainWideAttacksOffSharedTestnet(t *testing.T) {
	cfg := testResolvedConfig(t)
	matrix, err := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		t.Fatal(err)
	}
	wantLocal := map[string]bool{
		"weights-withhold-late-invalid-reveal":    true,
		"weights-fee-free-block-fill":             true,
		"hotkey-swap-reputation-reset":            true,
		"proxy-scope-alias-bypass":                true,
		"root-staking-index-state-bloat":          true,
		"root-claimed-swap-watermark-inflation":   true,
		"runtime-signed-precompile-foreign-frame": true,
	}
	for _, row := range matrix.Rows {
		if wantLocal[row.ID] && row.ExecutionMode != "local-runtime-only" {
			t.Errorf("chain-wide vector %s uses unsafe live mode %s", row.ID, row.ExecutionMode)
		}
		if row.ExecutionMode == "local-runtime-only" && len(row.ActorIDs) == 0 {
			t.Errorf("local-only vector %s has no concurrent live sentinel/emulator", row.ID)
		}
		delete(wantLocal, row.ID)
	}
	if len(wantLocal) != 0 {
		t.Fatalf("chain-wide vectors absent from matrix: %v", wantLocal)
	}
}

func TestAdversarialMatrixReferencesOnlyCheckedInTests(t *testing.T) {
	cfg := testResolvedConfig(t)
	matrix, err := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		t.Fatal(err)
	}
	references := map[string]bool{}
	discoverGoTestReferences(t, cfg.Repos.SN, "", references)
	discoverGoTestReferences(t, cfg.Repos.Server, "server", references)
	discoverSolidityTestReferences(t, filepath.Join(cfg.Repos.SN, "evm", "test"), references)
	for _, row := range matrix.Rows {
		for _, reference := range row.LocalTests {
			if !references[reference] {
				t.Errorf("adversarial matrix row %s references missing checked-in test %s", row.ID, reference)
			}
		}
	}
}

func TestAdversaryRequestGateCapsRate(t *testing.T) {
	gate, err := newAdversaryRequestGate(8)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	if delay := gate.reserve(at); delay != 0 {
		t.Fatalf("first reservation delay=%s", delay)
	}
	if delay := gate.reserve(at); delay != 125*time.Millisecond {
		t.Fatalf("second reservation delay=%s", delay)
	}
	if delay := gate.reserve(at.Add(time.Second)); delay != 0 {
		t.Fatalf("reservation after idle delay=%s", delay)
	}
	burstAt := at.Add(2 * time.Second)
	if delay := gate.reserveSlots(burstAt, 2); delay != 125*time.Millisecond {
		t.Fatalf("two-slot concurrent reservation delay=%s", delay)
	}
	if delay := gate.reserve(burstAt); delay != 250*time.Millisecond {
		t.Fatalf("reservation after concurrent pair delay=%s", delay)
	}
	if _, err := newAdversaryRequestGate(0); err == nil {
		t.Fatal("zero adversarial request rate was accepted")
	}
}

func TestAdversaryCampaignDoesNotCountShutdownCancellation(t *testing.T) {
	actor := &shutdownCancellationAdversary{secondEntered: make(chan struct{})}
	state := &adversaryActorState{evidence: AdversaryActorEvidence{ID: actor.ID()}}
	campaign := &liveAdversaryCampaign{
		cfg: AdversaryConfig{
			SampleIntervalMilliseconds: 1,
			RequestTimeoutMilliseconds: 30_000,
		},
		now:    time.Now,
		states: map[string]*adversaryActorState{actor.ID(): state},
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	var workers sync.WaitGroup
	workers.Add(1)
	go campaign.runActor(ctx, &workers, ready, actor)
	<-ready
	<-actor.secondEntered
	cancel()
	workers.Wait()
	if state.evidence.Samples != 1 || state.evidence.Errors != 1 || state.evidence.LastDetail != "observable actor failure" {
		t.Fatalf("shutdown cancellation changed actor evidence: %+v", state.evidence)
	}
}

func TestAdversaryCampaignRejectsCanceledParentBeforeActorLaunch(t *testing.T) {
	actor := &startCancellationAdversary{}
	campaign := &liveAdversaryCampaign{
		cfg: AdversaryConfig{
			SampleIntervalMilliseconds: 1,
			RequestTimeoutMilliseconds: 30_000,
		},
		actors: []adversaryActor{actor},
		now:    time.Now,
		states: map[string]*adversaryActorState{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := campaign.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled campaign start error=%v", err)
	}
	if actor.calls.Load() != 0 || campaign.started || len(campaign.states) != 0 {
		t.Fatalf("pre-canceled start launched actor calls=%d started=%t states=%d", actor.calls.Load(), campaign.started, len(campaign.states))
	}
}

func TestAdversaryFaultWindowAttributesOnlyExactTargetAndBoundedGrace(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	window := newAdversaryFaultWindow(10 * time.Second)
	window.now = func() time.Time { return now }
	window.Update([]string{"operator-1-api"})
	if !window.Expected("operator-1-api") || window.Expected("operator-2-api") {
		t.Fatal("fault window did not enforce exact target attribution")
	}
	window.Update(nil)
	now = now.Add(9 * time.Second)
	if !window.Expected("operator-1-api") {
		t.Fatal("in-flight request grace ended before one request timeout")
	}
	now = now.Add(2 * time.Second)
	if window.Expected("operator-1-api") {
		t.Fatal("restored target remained authorized after bounded grace")
	}
	actor := &verifyAdversary{faults: window}
	window.Update([]string{"operator-1-api"})
	if result := actor.sampleError(1, errors.New("connection reset"), 1, 1); result.Outcome != adversaryOutcomeExpectedRejection {
		t.Fatalf("scheduled target result=%+v", result)
	}
	if result := actor.sampleError(2, errors.New("connection reset"), 1, 1); result.Outcome != adversaryOutcomeError {
		t.Fatalf("unrelated target result=%+v", result)
	}
}

func TestAdversaryCampaignEnforcesErrorAndLatencyBounds(t *testing.T) {
	evidence := healthyAdversaryEvidence()
	assertions := adversaryAssertions(evidence, time.Now().Add(-time.Second), "observation")
	for _, assertion := range assertions {
		if !assertion.Passed {
			t.Fatalf("healthy adversarial evidence failed %s: %s", assertion.ID, assertion.Message)
		}
	}
	evidence.Actors[0].ErrorRatePPM = evidence.MaximumActorErrorRatePPM + 1
	evidence.Actors[0].P99LatencyMilliseconds = int64(evidence.MaximumP99Milliseconds + 1)
	evidence.Actors[0].AttackControlP95RatioPPM = evidence.MaximumAttackControlRatio + 1
	assertions = adversaryAssertions(evidence, time.Now().Add(-time.Second), "observation")
	wantID := "adversary_" + evidence.Actors[0].ID + "_resilience"
	foundFailure := false
	for _, assertion := range assertions {
		if assertion.ID == wantID && !assertion.Passed {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("adversarial error/latency breach did not fail %s", wantID)
	}
}

func TestAdversaryCampaignRequiresLiveInvalidMerkleProofEvidence(t *testing.T) {
	evidence := healthyAdversaryEvidence()
	for index := range evidence.Actors {
		if evidence.Actors[index].ID == "custody-boundary-emulation" {
			delete(evidence.Actors[index].Metrics, "live_invalid_merkle_proof_rejections")
			break
		}
	}
	foundFailure := false
	for _, assertion := range adversaryAssertions(evidence, time.Now().Add(-time.Second), "observation") {
		if assertion.ID == "adversary_live_invalid_merkle_proof" && !assertion.Passed {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatal("campaign without a live InvalidProof observation was accepted")
	}
}

func TestAdversaryMetricSnapshotOwnsCampaignHistory(t *testing.T) {
	state := &adversaryActorState{evidence: AdversaryActorEvidence{
		ID: "rpc-consistency-pressure",
		Metrics: map[string]AdversaryMetricEvidence{
			"subnet_uid_count": {Samples: 2, Minimum: 1, Maximum: 9, Last: 9},
		},
	}}
	snapshot := actorEvidenceSnapshot(state)
	state.evidence.Metrics["subnet_uid_count"] = AdversaryMetricEvidence{Samples: 3, Minimum: 0, Maximum: 9, Last: 0}
	if metric := snapshot.Metrics["subnet_uid_count"]; metric.Samples != 2 || metric.Minimum != 1 || metric.Last != 9 {
		t.Fatalf("snapshot metric changed with live campaign state: %+v", metric)
	}
}

func TestAdversaryLatencyRatioUsesOneMillisecondControlFloor(t *testing.T) {
	state := &adversaryActorState{
		evidence:         AdversaryActorEvidence{ID: "latency-floor", Samples: 2, ControlSamples: 1, AttackSamples: 1},
		latencies:        []int64{0, 100},
		controlLatencies: []int64{0},
		attackLatencies:  []int64{100},
	}
	evidence := actorEvidenceSnapshot(state)
	if evidence.ControlP95Milliseconds != 0 || evidence.AttackP95Milliseconds != 100 || evidence.AttackControlP95RatioPPM != 100_000_000 {
		t.Fatalf("sub-millisecond control bypassed the latency ratio: %+v", evidence)
	}
	evidence.Status = "stopped"
	evidence.P99LatencyMilliseconds = 100
	campaign := &AdversaryCampaignEvidence{
		StartedBeforeHappyPath: true, StoppedAfterHappyPath: true,
		MinimumSamplesPerActor: 1, MaximumP99Milliseconds: 1_000,
		MaximumAttackControlRatio: 20_000_000,
		Actors:                    []AdversaryActorEvidence{evidence},
	}
	assertions := adversaryAssertions(campaign, time.Now().Add(-time.Second), "observation")
	foundFailure := false
	for _, assertion := range assertions {
		if assertion.ID == "adversary_latency-floor_resilience" && !assertion.Passed {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatal("sub-millisecond control did not enforce the latency-ratio gate")
	}
}

func TestAdversaryVectorRequiresNamedMeasuredMetric(t *testing.T) {
	campaign := &liveAdversaryCampaign{
		cfg: AdversaryConfig{MinimumSamplesPerActor: 1, MaximumP99LatencyMilliseconds: 100, MaximumAttackControlP95Ratio: 2_000_000},
		matrix: &AdversarialMatrix{Rows: []AdversarialMatrixRow{{
			ID: "vector", Class: "test", ExecutionMode: "bounded-emulation", ActorIDs: []string{"actor"},
			Metrics: []string{"required_metric"}, LocalTests: []string{"test"}, Oracle: "sampled",
		}}},
		stopped: true,
	}
	actor := AdversaryActorEvidence{ID: "actor", Status: "stopped", Samples: 1, ControlSamples: 1, AttackSamples: 1}
	vectors := campaign.vectorEvidenceLocked(map[string]AdversaryActorEvidence{"actor": actor})
	if len(vectors) != 1 || vectors[0].Status != "fail" || len(vectors[0].MeasuredMetrics) != 0 {
		t.Fatalf("unmeasured vector evidence=%+v", vectors)
	}
	actor.Metrics = map[string]AdversaryMetricEvidence{"required_metric": {Samples: 1, Last: 7}}
	vectors = campaign.vectorEvidenceLocked(map[string]AdversaryActorEvidence{"actor": actor})
	if vectors[0].Status != "pass" || len(vectors[0].MeasuredMetrics) != 1 || vectors[0].MeasuredMetrics[0] != "required_metric" {
		t.Fatalf("measured vector evidence=%+v", vectors)
	}
}

func TestScenarioRunnerPersistsContinuousAdversariesAcrossHappyPath(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	evidence := healthyAdversaryEvidence()
	campaign := &scenarioAdversaryStub{evidence: evidence}
	definition := scenarioDefinition{
		Name: "unit-adversarial", AdversarialMatrixHash: evidence.MatrixHash,
		Checks: []scenarioCheck{{ID: "conservation", Check: func(evaluation *scenarioEvaluation) (bool, string) {
			return evaluation.Current.Status.Contracts.ConservationHolds, "exact"
		}}},
	}
	fixed := time.Date(2026, 8, 21, 1, 0, 0, 0, time.UTC)
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, &staticScenarioProbe{observations: []*ScenarioObservation{testScenarioObservation(cfg, 1)}}, scenarioRunOptions{
		Now: func() time.Time { return fixed }, Publish: false, Adversaries: campaign,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "pass" || result.Adversaries == nil || campaign.startCalls != 1 || campaign.stopCalls != 1 {
		t.Fatalf("result=%+v campaign=%+v", result, campaign)
	}
	path := filepath.Join(dir, "runs", result.RunID, "adversaries.json")
	b, err := os.ReadFile(path)
	if err != nil || !json.Valid(b) {
		t.Fatalf("adversarial evidence %s: %v %s", path, err, b)
	}
}

func TestConsensusCabalModelClipsMinoritySelfWeightAndStaleCopy(t *testing.T) {
	honest := map[uint16]uint64{1: 600_000, 2: 400_000}
	for _, cabalStake := range []uint64{100_000, 250_000, 400_000, 490_000} {
		selfWeighted := yumaConsensus([]yumaValidator{
			{stake: 1_000_000 - cabalStake, weights: honest},
			{stake: cabalStake, weights: map[uint16]uint64{3: 1_000_000}},
		}, 500_000)
		if selfWeighted[3] != 0 || selfWeighted[1] != honest[1] || selfWeighted[2] != honest[2] {
			t.Errorf("minority stake=%d consensus=%v", cabalStake, selfWeighted)
		}
		stale := yumaConsensus([]yumaValidator{
			{stake: 1_000_000 - cabalStake, weights: honest},
			{stake: cabalStake, weights: map[uint16]uint64{1: 400_000, 2: 600_000}},
		}, 500_000)
		if stale[1] != honest[1] || stale[2] != honest[2] {
			t.Errorf("stale copier stake=%d consensus=%v", cabalStake, stale)
		}
	}
}

func TestConsensusCabalModelReportsMajorityBoundary(t *testing.T) {
	below := yumaConsensus([]yumaValidator{{stake: 500_001, weights: map[uint16]uint64{1: 1_000_000}}, {stake: 499_999, weights: map[uint16]uint64{2: 1_000_000}}}, 500_000)
	if below[2] != 0 || below[1] != 1_000_000 {
		t.Fatalf("below-kappa cabal consensus=%v", below)
	}
	atBoundary := yumaConsensus([]yumaValidator{{stake: 500_000, weights: map[uint16]uint64{1: 1_000_000}}, {stake: 500_000, weights: map[uint16]uint64{2: 1_000_000}}}, 500_000)
	if atBoundary[1] != 1_000_000 || atBoundary[2] != 1_000_000 {
		t.Fatalf("kappa boundary was not explicit: %v", atBoundary)
	}
}

func TestLiquidAlphaBondEMARewardsEarlyMeasurementAndClearsPermitDropout(t *testing.T) {
	for sequence := uint64(0); sequence < 120; sequence++ {
		result, err := emulateLiquidAlphaCopyAndDropout(sequence)
		if err != nil {
			t.Fatalf("sequence=%d: %v", sequence, err)
		}
		if result.honestBondPPM <= 600_000 || result.copierBondPPM >= 400_000 {
			t.Fatalf("sequence=%d delayed copier escaped penalty: %+v", sequence, result)
		}
		if result.honestReentryBondPPM >= result.honestContinuousBondPPM {
			t.Fatalf("sequence=%d permit dropout retained bond history: %+v", sequence, result)
		}
	}
	if _, err := liquidAlphaValue(0, 0, 0, 0.9, 0.7, 1_000); err == nil {
		t.Fatal("inverted liquid-alpha bounds were accepted")
	}
}

func TestCustodyBoundaryEmulationSeparatesDomainsAndConservesRounding(t *testing.T) {
	cfg := testResolvedConfig(t)
	actor := &custodyAdversary{cfg: cfg}
	for _, phase := range []adversarySamplePhase{adversaryControlPhase, adversaryAttackPhase} {
		result := actor.Sample(context.Background(), phase, 7)
		if result.Outcome != adversaryOutcomeSuccess {
			t.Fatalf("phase=%s result=%+v", phase, result)
		}
	}
}

func TestLiveMerkleRetryOnlyMasksTheScheduledOperatorOutage(t *testing.T) {
	if !liveMerkleRetryable(fmt.Errorf("artifact pending: %w", errLiveMerkleEvidenceUnavailable), false) {
		t.Fatal("an unavailable entitlement was not retryable")
	}
	operatorUnavailable := fmt.Errorf("history: %w", errLiveMerkleOperatorUnavailable)
	if liveMerkleRetryable(operatorUnavailable, false) || !liveMerkleRetryable(operatorUnavailable, true) {
		t.Fatal("operator unavailability was not scoped to its scheduled fault")
	}
	for _, err := range []error{
		errors.New("invalid proof succeeded"),
		errors.New("artifact signature is invalid"),
		errors.New("entitlement mutated"),
	} {
		if liveMerkleRetryable(err, true) {
			t.Fatalf("scheduled operator outage masked a custody failure: %v", err)
		}
	}
}

func TestRegistrationBurnRaceModelRejectsSmallestPriceDrift(t *testing.T) {
	delta, capacity, rejected, err := registrationBurnRaceModel(250_000_000, 32, 7)
	if err != nil || delta != 1 || capacity != 25 || rejected != 1 {
		t.Fatalf("registration race delta=%d capacity=%d rejected=%d error=%v", delta, capacity, rejected, err)
	}
	if _, _, _, err := registrationBurnRaceModel(math.MaxUint64, 32, 0); err == nil {
		t.Fatal("unbounded registration burn race was accepted")
	}
}

func TestDepositBoundaryModelSeparatesReplayOperatorAndSnapshot(t *testing.T) {
	cfg := testResolvedConfig(t)
	replay, crossNO, rate, remaining, err := depositBoundaryModel(cfg.Policy.Deposit, 3)
	if err != nil || replay != 1 || crossNO != 1 || rate == 0 || remaining >= cfg.Policy.Deposit.EpochCapRaoPerOperator {
		t.Fatalf("deposit boundary replay=%d cross_no=%d rate=%d remaining=%d error=%v", replay, crossNO, rate, remaining, err)
	}
}

func TestImmutableCustodyAndSettlementLivenessModels(t *testing.T) {
	rejected, available, err := immutableCustodyProbeModel(9)
	if err != nil || rejected != 4 || available != 1 {
		t.Fatalf("custody probes rejected=%d available=%d error=%v", rejected, available, err)
	}
	delay, carry, duplicate, uncertain, err := settlementLivenessModel(9)
	if err != nil || delay != 0 || carry == 0 || duplicate != 1 || uncertain != 0 {
		t.Fatalf("settlement delay=%d carry=%d duplicate=%d uncertain=%d error=%v", delay, carry, duplicate, uncertain, err)
	}
}

func TestPatchedSubtensorRootIndexAndWatermarkModels(t *testing.T) {
	for sequence := uint64(0); sequence < 128; sequence++ {
		entries, err := patchedSubtensorStateModels(sequence)
		if err != nil || entries < 80 || entries > 128 {
			t.Fatalf("sequence=%d entries=%d error=%v", sequence, entries, err)
		}
	}
	if rootSwapDestinationClean(0, 0, 0, 0, 1) {
		t.Fatal("destination with a residual root-claimed watermark was accepted")
	}
	if owed := saturatingOwed(100, 101); owed != 0 {
		t.Fatalf("saturating owed=%d, want 0", owed)
	}
}

func TestRPCAdversaryEncodesSubnetPriceAndUIDCalls(t *testing.T) {
	cases := map[string]string{
		"getAlphaPrice(uint16)":       "0x69e38bc3",
		"getMovingAlphaPrice(uint16)": "0xa86b1037",
		"getTaoInPool(uint16)":        "0x2d9bfc71",
		"getAlphaInPool(uint16)":      "0xc2ba9e87",
		"getUidCount(uint16)":         "0x1f193572",
	}
	for signature, selector := range cases {
		encoded := abiUint16CallData(signature, 521)
		if len(encoded) != 2+8+64 || !strings.HasPrefix(encoded, selector) || !strings.HasSuffix(encoded, "0209") {
			t.Errorf("%s calldata=%s", signature, encoded)
		}
	}
	encoded := "\"0x" + strings.Repeat("0", 60) + "0209\""
	value, err := decodeABIUint256(rpcResponse{Result: json.RawMessage(encoded)})
	if err != nil || !value.IsUint64() || value.Uint64() != 521 {
		t.Fatalf("decoded uint256=%v error=%v", value, err)
	}
	if _, err := decodeABIUint256(rpcResponse{Result: json.RawMessage(`"0x01"`)}); err == nil {
		t.Fatal("short ABI uint256 was accepted")
	}
}

func TestRPCAdversaryRejectsZeroMovingPriceAndSentinelDrift(t *testing.T) {
	positive := func(value int64) *big.Int { return big.NewInt(value) }
	if err := validateSubnetPrecompileSentinels(positive(10), positive(10), positive(9), positive(9), positive(2), positive(2), positive(100), positive(200)); err != nil {
		t.Fatalf("positive matching sentinels were rejected: %v", err)
	}
	if err := validateSubnetPrecompileSentinels(positive(10), positive(10), positive(0), positive(0), positive(2), positive(2), positive(100), positive(200)); err == nil {
		t.Fatal("zero moving price was accepted despite the mainnet-readiness stop condition")
	}
	if err := validateSubnetPrecompileSentinels(positive(10), positive(11), positive(9), positive(9), positive(2), positive(2), positive(100), positive(200)); err == nil {
		t.Fatal("operational/public spot-price drift was accepted")
	}
}

func TestMEVShieldFinalityEraExpiryModel(t *testing.T) {
	for _, test := range []struct {
		name                 string
		finalized, best, era uint64
		wantLag              uint64
		wantExpired          bool
		wantError            bool
	}{
		{name: "no lag", finalized: 100, best: 100, era: 8},
		{name: "inside window", finalized: 100, best: 107, era: 8, wantLag: 7},
		{name: "window consumed", finalized: 100, best: 108, era: 8, wantLag: 8, wantExpired: true},
		{name: "far behind", finalized: 100, best: 120, era: 8, wantLag: 20, wantExpired: true},
		{name: "zero period", finalized: 100, best: 100, era: 0, wantError: true},
		{name: "invalid ordering", finalized: 101, best: 100, era: 8, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			lag, expired, err := mevShieldFinalityEraExpiryModel(test.finalized, test.best, test.era)
			if (err != nil) != test.wantError || lag != test.wantLag || expired != test.wantExpired {
				t.Fatalf("lag=%d expired=%t error=%v", lag, expired, err)
			}
		})
	}
}

func TestDepressedReserveFlowModelNeverRewardsNegativeFlow(t *testing.T) {
	for _, values := range [][3]uint64{
		{0, 0, 0},
		{1, 1, 0},
		{1, 2, 0},
		{math.MaxUint64 - 1, math.MaxUint64, 0},
		{math.MaxUint64, math.MaxUint64 - 1, 1},
	} {
		if got := safeProtocolFlowContribution(values[0], values[1]); got != values[2] {
			t.Errorf("flow contribution inflow=%d outflow=%d got=%d want=%d", values[0], values[1], got, values[2])
		}
	}
}

func TestRootBasketUnstakeSettlesProportionalHiddenReward(t *testing.T) {
	const principal = uint64(100_000_000_000)
	const reward = uint64(10_000_000_000)
	for _, test := range []struct {
		unstake, claimed, remaining uint64
	}{
		{1, 0, reward},
		{40_000_000_000, 4_000_000_000, 6_000_000_000},
		{principal, reward, 0},
	} {
		claimed, remaining, err := proportionalRootBasketClaim(principal, reward, test.unstake)
		if err != nil || claimed != test.claimed || remaining != test.remaining || claimed+remaining != reward {
			t.Errorf("unstake=%d claim=%d remaining=%d error=%v, want %d/%d", test.unstake, claimed, remaining, err, test.claimed, test.remaining)
		}
	}
	if _, _, err := proportionalRootBasketClaim(0, reward, 1); err == nil {
		t.Fatal("root basket claim without principal was accepted")
	}
	if _, _, err := proportionalRootBasketClaim(principal, reward, principal+1); err == nil {
		t.Fatal("root basket over-unstake was accepted")
	}
	claimed, remaining, err := proportionalRootBasketClaim(math.MaxUint64, math.MaxUint64, math.MaxUint64)
	if err != nil || claimed != math.MaxUint64 || remaining != 0 {
		t.Fatalf("maximum root basket settlement=%d/%d error=%v", claimed, remaining, err)
	}
}

func TestRuntime452RootBasketFailureIsolation(t *testing.T) {
	terminal, healthy, retryable, blocked, err := rootBasketFailureIsolationModel(1_000, 100)
	if err != nil || terminal != 1 || healthy != 1 || retryable != 1 || blocked {
		t.Fatalf("root basket isolation terminal=%d healthy=%d retryable=%d blocked=%t error=%v", terminal, healthy, retryable, blocked, err)
	}
	if _, _, _, _, err := rootBasketFailureIsolationModel(0, 100); err == nil {
		t.Fatal("zero pending-deposit control was accepted")
	}
}

func TestProxyStakeMEVBoundRejectsSandwichSlippage(t *testing.T) {
	control, err := emulateProxyStakeMEV(1_000_000_000_000, 2_000_000_000_000, 1_000_000_000, 0, 10_000)
	if err != nil || control.UnshieldedOut != control.BaselineOut || control.UnshieldedLossPPM != 0 || control.ProtectedWouldReject {
		t.Fatalf("control proxy stake=%+v error=%v", control, err)
	}
	attack, err := emulateProxyStakeMEV(1_000_000_000_000, 2_000_000_000_000, 1_000_000_000, 25_000_000_000, 10_000)
	if err != nil || attack.UnshieldedOut >= attack.BaselineOut || attack.UnshieldedLossPPM <= 10_000 || !attack.ProtectedWouldReject || attack.UnshieldedOut >= attack.MinimumOut {
		t.Fatalf("sandwiched proxy stake=%+v error=%v", attack, err)
	}
	if _, err := emulateProxyStakeMEV(1, 1, 1, 1, 1_000_000); err == nil {
		t.Fatal("unbounded staking slippage was accepted")
	}
}

func TestCommitmentParserTypeConfusionModelRejectsAlternateRuntimeFields(t *testing.T) {
	for sequence := uint64(0); sequence < 256; sequence++ {
		rejections, err := commitmentParserTypeConfusionModel(sequence)
		if err != nil || rejections != 5 {
			t.Fatalf("sequence=%d rejections=%d error=%v", sequence, rejections, err)
		}
	}
}

func TestRuntimeCompositeFailureModelRollsBackEveryWrite(t *testing.T) {
	for _, sequence := range []uint64{0, 1, 99, math.MaxUint16} {
		cases, err := runtimeCompositeRollbackModel(sequence)
		if err != nil || cases != 4 {
			t.Fatalf("sequence=%d cases=%d error=%v", sequence, cases, err)
		}
	}
	initial := runtimeEconomicState{UserTao: 100, PoolTao: 200, ClaimPaid: 7}
	got, err := applyRuntimeEconomicTransaction(initial, func(state *runtimeEconomicState) error {
		state.UserTao--
		state.PoolTao++
		state.ClaimPaid++
		return errors.New("late transfer failed")
	})
	if err == nil || got != initial {
		t.Fatalf("failed transaction state=%+v error=%v", got, err)
	}
}

func TestSettlementTransferFloorModelDefersDustAndAggregatesCredit(t *testing.T) {
	cases, err := settlementTransferFloorModel(100_000, 568_309)
	if err != nil || cases != 5 {
		t.Fatalf("settlement transfer-floor cases=%d error=%v", cases, err)
	}
}

func TestSettlementTransferFloorModelRejectsMissingRuntimeEconomics(t *testing.T) {
	for _, economics := range [][2]uint64{{0, 568_309}, {100_000, 0}} {
		if _, err := settlementTransferFloorModel(economics[0], economics[1]); err == nil {
			t.Fatalf("invalid settlement economics %v were accepted", economics)
		}
	}
}

func TestRuntimeIdentityMigrationPreservesEverySecurityField(t *testing.T) {
	if fields, err := runtimeIdentityMigrationModel(42); err != nil || fields != 8 {
		t.Fatalf("fields=%d error=%v", fields, err)
	}
	before := runtimeIdentitySecurityState{
		RootClaimable: 10, RootClaimed: 2, Conviction: 3, ChildkeyTake: 4,
		LockMass: 6, LockContributors: [3]uint64{1, 2, 3}, PerpetualLock: true,
	}
	missing := before
	missing.Conviction = 0
	if err := validateRuntimeIdentityMigration(before, runtimeIdentitySecurityState{}, missing); err == nil {
		t.Fatal("identity migration accepted a missing conviction field")
	}
	badLock := before
	badLock.LockContributors[0]++
	if err := validateRuntimeIdentityMigration(before, runtimeIdentitySecurityState{}, badLock); err == nil {
		t.Fatal("identity migration accepted an aggregate lock mismatch")
	}
	if err := validateRuntimeIdentityMigration(before, before, before); err == nil {
		t.Fatal("identity migration accepted residual old-identity state")
	}
}

func TestRuntimeOrderSettlementIsBoundedAndIdempotent(t *testing.T) {
	if cases, err := runtimeOrderModel(7); err != nil || cases != 4 {
		t.Fatalf("cases=%d error=%v", cases, err)
	}
	initial := runtimeOrderState{Remaining: 5}
	filled, err := settleRuntimeOrder(initial, 9, 10, 1, true)
	if err != nil || !filled.Closed || filled.Remaining != 0 || filled.BuyerDebited != 5 || filled.SellerPaid != 5 {
		t.Fatalf("bounded partial fill=%+v error=%v", filled, err)
	}
	replayed, err := settleRuntimeOrder(filled, 9, 10, 1, true)
	if err != nil || replayed != filled {
		t.Fatalf("replayed fill=%+v error=%v", replayed, err)
	}
}

func TestRuntimeIssuanceAndReserveMigrationStayConserved(t *testing.T) {
	for _, sequence := range []uint64{0, 1, 10_000, 1_000_000} {
		if cases, err := runtimeIssuanceMigrationModel(sequence); err != nil || cases != 5 {
			t.Fatalf("sequence=%d cases=%d error=%v", sequence, cases, err)
		}
	}
	bad := runtimeGlobalAccountingState{Sender: 1, Recipient: 1, TotalIssuance: 3}
	if runtimeGlobalAccountingValid(bad) {
		t.Fatal("global issuance accepted a balance-sum mismatch")
	}
}

func TestRuntimeWorkBoundsRejectUnmeteredCollections(t *testing.T) {
	if work, err := boundedRuntimeWork(255, 255, 10_000); err != nil || work != 2_550_000 {
		t.Fatalf("bounded work=%d error=%v", work, err)
	}
	if _, err := boundedRuntimeWork(256, 255, 10_000); err == nil {
		t.Fatal("over-limit runtime collection was accepted")
	}
	if _, err := boundedRuntimeWork(math.MaxUint64, math.MaxUint64, 2); err == nil {
		t.Fatal("runtime work overflow was accepted")
	}
}

func TestDrandRoundWatermarkRejectsUnprovenJumps(t *testing.T) {
	if round, err := advanceDrandRound(100, 101, 2); err != nil || round != 101 {
		t.Fatalf("valid round=%d error=%v", round, err)
	}
	for _, candidate := range []uint64{99, 100, 103, math.MaxUint64} {
		if round, err := advanceDrandRound(100, candidate, 2); err == nil || round != 100 {
			t.Errorf("candidate=%d round=%d error=%v", candidate, round, err)
		}
	}
}

func TestChildkeyGraphRejectsEmptySelfAndIndirectCycles(t *testing.T) {
	if err := validateChildkeyUpdate(map[uint64][]uint64{2: {4}}, 1, []uint64{2, 3}, 8); err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		graph    map[uint64][]uint64
		children []uint64
		maximum  int
	}{
		"empty":     {graph: map[uint64][]uint64{}, children: nil, maximum: 8},
		"self":      {graph: map[uint64][]uint64{}, children: []uint64{1}, maximum: 8},
		"indirect":  {graph: map[uint64][]uint64{2: {3}, 3: {1}}, children: []uint64{2}, maximum: 8},
		"unbounded": {graph: map[uint64][]uint64{2: {3}, 3: {4}}, children: []uint64{2}, maximum: 2},
	} {
		if err := validateChildkeyUpdate(test.graph, 1, test.children, test.maximum); err == nil {
			t.Errorf("%s childkey update was accepted", name)
		}
	}
}

func TestLeaseTerminationRepatriatesStakeLocksAndIndexes(t *testing.T) {
	initial := runtimeLeaseState{DerivedAlpha: 100, DerivedLock: 50, BeneficiaryAlpha: 20, BeneficiaryLock: 10, OwnerIndex: true, OwnedIndex: true, StakingIndex: true, ProxyInstalled: true}
	final, err := terminateRuntimeLease(initial)
	if err != nil || final.BeneficiaryAlpha != 120 || final.BeneficiaryLock != 60 || final.DerivedAlpha != 0 || final.DerivedLock != 0 || final.OwnerIndex || final.OwnedIndex || final.StakingIndex || final.ProxyInstalled {
		t.Fatalf("lease final=%+v error=%v", final, err)
	}
	overflow := initial
	overflow.BeneficiaryAlpha = math.MaxUint64
	if got, err := terminateRuntimeLease(overflow); err == nil || got != overflow {
		t.Fatalf("overflow lease state=%+v error=%v", got, err)
	}
}

func TestRegistrationEscrowBacksSumAndRejectsUnpricedOwnerAllocation(t *testing.T) {
	if liability, err := registrationAccountingModel([]uint64{100, 300}, 400, 0); err != nil || liability != 400 {
		t.Fatalf("liability=%d error=%v", liability, err)
	}
	if _, err := registrationAccountingModel([]uint64{100, 300}, 300, 0); err == nil {
		t.Fatal("max-only registration backing was accepted")
	}
	if _, err := registrationAccountingModel([]uint64{100}, 100, 1); err == nil {
		t.Fatal("unpriced owner alpha was accepted")
	}
	if _, err := registrationAccountingModel([]uint64{math.MaxUint64, 1}, math.MaxUint64, 0); err == nil {
		t.Fatal("registration liability overflow was accepted")
	}
}

func TestConcentratedLiquidityFailureIsAtomicAndRetryable(t *testing.T) {
	for _, sequence := range []uint64{0, 1, 99, 1_000} {
		if cases, err := concentratedLiquidityModel(sequence); err != nil || cases != 2 {
			t.Fatalf("sequence=%d cases=%d error=%v", sequence, cases, err)
		}
	}
	initial := runtimeLiquidityState{ReserveIn: 100, ReserveOut: 100, PendingEmissions: 7}
	if got, err := priceLimitedRuntimeSwap(initial, 10, 100); err == nil || got != initial {
		t.Fatalf("failed price-limited swap=%+v error=%v", got, err)
	}
}

func TestReleaseLockCoversEveryPublishedSubtensorAdvisory(t *testing.T) {
	cfg := testResolvedConfig(t)
	lock := new(ReleaseLock)
	if err := strictYAML(filepath.Join(cfg.Repos.SN, "deploy", "testnet", "release.lock.yml"), lock); err != nil {
		t.Fatal(err)
	}
	if lock.Runtime.SourceTag != "testnet" || lock.Runtime.SourceCommit != "da06f033663896ef2fdbbfc3ecc68ca908fba0f5" || lock.Runtime.SpecVersion != 452 || lock.Runtime.CodeHash == "" {
		t.Fatalf("release runtime does not postdate the published advisory fixes: %+v", lock.Runtime)
	}
	matrix, err := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range matrix.Rows {
		for _, source := range row.Sources {
			for advisory := range requiredSubtensorAdvisories {
				if strings.Contains(source, advisory) {
					seen[advisory] = true
				}
			}
		}
	}
	if len(seen) != len(requiredSubtensorAdvisories) {
		t.Fatalf("published Subtensor advisory coverage=%v, want all %d", seen, len(requiredSubtensorAdvisories))
	}
}

func TestAdversarialMatrixPinsReviewedSubtensorIssueHistory(t *testing.T) {
	cfg := testResolvedConfig(t)
	matrix, err := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range matrix.Rows {
		for _, source := range row.Sources {
			for issue, vectorID := range requiredSubtensorIssueVectors {
				if row.ID == vectorID && strings.Contains(source, "/issues/"+issue) {
					seen[issue] = true
				}
			}
		}
	}
	if len(seen) != len(requiredSubtensorIssueVectors) {
		t.Fatalf("reviewed issue coverage=%d want=%d missing=%v", len(seen), len(requiredSubtensorIssueVectors), func() []string {
			var missing []string
			for issue := range requiredSubtensorIssueVectors {
				if !seen[issue] {
					missing = append(missing, issue)
				}
			}
			sort.Strings(missing)
			return missing
		}())
	}
}

func TestAdversaryHTTPRejectsNonLoopbackSource(t *testing.T) {
	gate, err := newAdversaryRequestGate(20)
	if err != nil {
		t.Fatal(err)
	}
	client := &adversaryHTTP{gate: gate, timeout: time.Second}
	_, _, err = client.do(context.Background(), http.MethodGet, "http://127.0.0.1:1/status", "203.0.113.1", nil, 1)
	if err == nil || !strings.Contains(err.Error(), "not loopback") {
		t.Fatalf("external adversarial source was not rejected: %v", err)
	}
}

func TestAdversaryHTTPConcurrentPairActuallyOverlapsWithinReservedSlots(t *testing.T) {
	var current atomic.Int32
	var maximum atomic.Int32
	var releaseOnce sync.Once
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		inFlight := current.Add(1)
		defer current.Add(-1)
		for {
			observed := maximum.Load()
			if observed >= inFlight || maximum.CompareAndSwap(observed, inFlight) {
				break
			}
		}
		if inFlight == 2 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-time.After(time.Second):
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	gate, err := newAdversaryRequestGate(20)
	if err != nil {
		t.Fatal(err)
	}
	client := &adversaryHTTP{gate: gate, timeout: 2 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	responses, err := client.doConcurrentPair(ctx, http.MethodPost, server.URL, "", []byte(`{}`), 1024)
	if err != nil {
		t.Fatal(err)
	}
	for index, response := range responses {
		if response.Err != nil || response.Status != http.StatusOK || !json.Valid(response.Body) {
			t.Fatalf("response %d = %+v", index, response)
		}
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrent requests=%d, want 2", maximum.Load())
	}
}

func TestAdversarialRPCBlockDecoderRejectsMalformedAndErrors(t *testing.T) {
	valid := rpcResponse{Result: json.RawMessage(`{"number":"0x2a","hash":"0x` + strings.Repeat("ab", 32) + `"}`)}
	block, number, err := decodeRPCBlock(valid)
	if err != nil || number != 42 || block.Hash == "" {
		t.Fatalf("valid block=%+v number=%d error=%v", block, number, err)
	}
	invalid := rpcResponse{Result: json.RawMessage(`{"number":"42","hash":"short"}`)}
	if _, _, err := decodeRPCBlock(invalid); err == nil {
		t.Fatal("malformed RPC block was accepted")
	}
	withError := rpcResponse{Error: &rpcResponseError{Code: -32601, Message: "not found"}}
	if _, _, err := decodeRPCBlock(withError); err == nil {
		t.Fatal("RPC error envelope was accepted as a block")
	}
	quantity, err := decodeRPCQuantity(rpcResponse{Result: json.RawMessage(`"0X3B1"`)})
	if err != nil || quantity != 945 {
		t.Fatalf("normalized RPC quantity=%d error=%v", quantity, err)
	}
	if _, err := decodeRPCQuantity(rpcResponse{Result: json.RawMessage(`945`)}); err == nil {
		t.Fatal("non-hex RPC quantity was accepted")
	}
}

func TestInvalidMerkleProofResponseRequiresExactCustomError(t *testing.T) {
	selector := stabi.STSettlementVaultInvalidProofErrorID().Bytes()[:4]
	encoded := "0x" + hex.EncodeToString(selector)
	valid := []rpcResponse{
		{Error: &rpcResponseError{Code: 3, Message: "execution reverted", Data: json.RawMessage(fmt.Sprintf("%q", encoded))}},
		{Error: &rpcResponseError{Code: 3, Message: "execution reverted", Data: json.RawMessage(fmt.Sprintf(`{"data":%q}`, encoded))}},
	}
	for index, response := range valid {
		if err := requireInvalidProofResponse(response); err != nil {
			t.Fatalf("valid InvalidProof response %d rejected: %v", index, err)
		}
	}
	wrong := append([]byte(nil), selector...)
	wrong[0]++
	invalid := []rpcResponse{
		{},
		{Error: &rpcResponseError{Code: 3, Message: "execution reverted"}},
		{Error: &rpcResponseError{Code: 3, Message: "execution reverted", Data: json.RawMessage(`"reverted"`)}},
		{Error: &rpcResponseError{Code: 3, Message: "execution reverted", Data: json.RawMessage(fmt.Sprintf("%q", "0x"+hex.EncodeToString(wrong)))}},
		{Error: &rpcResponseError{Code: 3, Message: "execution reverted", Data: json.RawMessage(fmt.Sprintf("%q", encoded+"00"))}},
	}
	for index, response := range invalid {
		if err := requireInvalidProofResponse(response); err == nil {
			t.Fatalf("invalid revert response %d was accepted", index)
		}
	}
}

func TestLiveMerkleOperatorSelectionCoversBothAttackPhaseParities(t *testing.T) {
	passed := map[int]bool{}
	first := nextLiveMerkleOperator(passed, 2, 1)
	if first < 1 || first > 2 {
		t.Fatalf("first live Merkle operator=%d", first)
	}
	passed[first] = true
	second := nextLiveMerkleOperator(passed, 2, 3)
	if second < 1 || second > 2 || second == first {
		t.Fatalf("second live Merkle operator=%d after first=%d", second, first)
	}
	passed[second] = true
	if next := nextLiveMerkleOperator(passed, 2, 5); next != 0 {
		t.Fatalf("complete live Merkle operator set returned %d", next)
	}
}

func encodeLiveMerkleEntitlement(artifact *payoutArtifact, mutated bool) []byte {
	result := make([]byte, 7*32)
	copy(result[:32], artifact.PayoutRoot[:])
	artifactHash, _ := hex.DecodeString(strings.TrimPrefix(artifact.ContentHash, "sha256:"))
	copy(result[32:64], artifactHash)
	big.NewInt(1_000).FillBytes(result[64:96])
	big.NewInt(1_000).FillBytes(result[96:128])
	if mutated {
		big.NewInt(1).FillBytes(result[128:160])
	}
	binary.BigEndian.PutUint64(result[184:192], 500)
	result[len(result)-1] = 2
	return result
}

func TestLiveInvalidMerkleProofProbeUsesPinnedReadOnlyCalls(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	coordinator := common.HexToAddress("0x0000000000000000000000000000000000000100")
	vault := common.HexToAddress("0x0000000000000000000000000000000000000200")
	if err := saveContractDeployment(stateDir, ContractDeployment{CoordinatorProxy: coordinator, SettlementVault: vault}); err != nil {
		t.Fatal(err)
	}
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{
		DeploymentID:         cfg.Config.Deployment.DeploymentID,
		GenesisHash:          cfg.Public.Chain.GenesisHash,
		PolicyHash:           cfg.PolicyHash,
		ChainID:              cfg.ChainID,
		Netuid:               cfg.Netuid,
		Coordinator:          coordinator,
		SettlementVault:      vault,
		Epoch:                4,
		NoID:                 1,
		Start:                payoutartifact.Boundary{Number: 10, Hash: "0x" + strings.Repeat("01", 32)},
		End:                  payoutartifact.Boundary{Number: 20, Hash: "0x" + strings.Repeat("02", 32)},
		OperatorSnapshotHash: "sha256:" + strings.Repeat("10", 32),
		FleetSnapshotHash:    "sha256:" + strings.Repeat("20", 32),
		Providers: []payoutartifact.ProviderInput{{
			ClientID: [16]byte{1}, NetworkID: [16]byte{2}, Coldkey: [32]byte{3}, UsageBytes: 100,
			Assignments: 8, Confirmations: 8, Eligible: true,
		}},
		ReliabilityAMin: 8,
		CreatedAt:       time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ethcrypto.HexToECDSA(strings.Repeat("11", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	artifactBytes, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifactHash := strings.TrimPrefix(artifact.ContentHash, "sha256:")
	operatorServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/sn/artifacts":
			_, _ = writer.Write([]byte(fmt.Sprintf(`{"schema":"urnetwork-payout-artifact-history-v1","objects":[{"key":"blob/%s.json"}]}`, artifactHash)))
		case "/sn/artifact":
			if request.URL.Query().Get("hash") != artifact.ContentHash {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = writer.Write(artifactBytes)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer operatorServer.Close()

	var invalidMethod, invalidBlock atomic.Uint64
	var claimCalls atomic.Uint64
	var mutateAfterClaim atomic.Bool
	rpcServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope struct {
			JSONRPC string            `json:"jsonrpc"`
			ID      uint64            `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(request.Body).Decode(&envelope) != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": envelope.ID}
		switch envelope.Method {
		case "eth_getBlockByNumber":
			response["result"] = map[string]any{"number": "0x64", "hash": "0x" + strings.Repeat("ab", 32)}
		case "eth_call":
			if len(envelope.Params) != 2 {
				invalidBlock.Add(1)
				break
			}
			var call map[string]string
			var blockTag string
			if json.Unmarshal(envelope.Params[0], &call) != nil || json.Unmarshal(envelope.Params[1], &blockTag) != nil || blockTag != "0x64" || !strings.EqualFold(call["to"], vault.Hex()) {
				invalidBlock.Add(1)
			}
			data, decodeErr := hex.DecodeString(strings.TrimPrefix(call["data"], "0x"))
			if decodeErr != nil || len(data) < 4 {
				invalidMethod.Add(1)
				break
			}
			switch hex.EncodeToString(data[:4]) {
			case hex.EncodeToString(stabi.NewSTSettlementVault().PackEntitlement(big.NewInt(4), big.NewInt(1))[:4]):
				response["result"] = "0x" + hex.EncodeToString(encodeLiveMerkleEntitlement(artifact, mutateAfterClaim.Load() && claimCalls.Load() > 0))
			case hex.EncodeToString(stabi.NewSTSettlementVault().PackConservationHolds()[:4]):
				conservation := make([]byte, 32)
				conservation[31] = 1
				response["result"] = "0x" + hex.EncodeToString(conservation)
			case hex.EncodeToString(stabi.NewSTSettlementVault().PackClaim(big.NewInt(4), big.NewInt(1), artifact.Leaves[0].Coldkey, big.NewInt(9_999), artifact.Leaves[0].Proof)[:4]):
				claimCalls.Add(1)
				response["error"] = map[string]any{"code": 3, "message": "execution reverted", "data": "0x" + hex.EncodeToString(stabi.STSettlementVaultInvalidProofErrorID().Bytes()[:4])}
			default:
				invalidMethod.Add(1)
			}
		default:
			invalidMethod.Add(1)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	defer rpcServer.Close()
	gate := func() *adversaryHTTP {
		return &adversaryHTTP{gate: &adversaryRequestGate{interval: time.Nanosecond, now: time.Now}, timeout: time.Second}
	}
	evidence, err := liveInvalidMerkleProofProbe(context.Background(), cfg, stateDir, operatorServer.URL, rpcServer.URL, 1, gate(), gate(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Epoch != 4 || evidence.NoID != 1 || evidence.FinalizedBlock != 100 || evidence.Requests != 8 || claimCalls.Load() != 1 || invalidMethod.Load() != 0 || invalidBlock.Load() != 0 {
		t.Fatalf("live Merkle evidence=%+v claim_calls=%d invalid_method=%d invalid_block=%d", evidence, claimCalls.Load(), invalidMethod.Load(), invalidBlock.Load())
	}
	claimCalls.Store(0)
	mutateAfterClaim.Store(true)
	if _, err := liveInvalidMerkleProofProbe(context.Background(), cfg, stateDir, operatorServer.URL, rpcServer.URL, 1, gate(), gate(), 8); err == nil || !strings.Contains(err.Error(), "changed the pinned entitlement") {
		t.Fatalf("mutated post-rejection state error=%v", err)
	}
}

func TestAdversarialRPCRuntimeIdentityRejectsMalformedAndDriftingVersions(t *testing.T) {
	encode := func(name string, spec, transaction uint32) rpcResponse {
		result, err := json.Marshal(rpcRuntimeVersion{SpecName: name, SpecVersion: spec, TransactionVersion: transaction})
		if err != nil {
			t.Fatal(err)
		}
		return rpcResponse{Result: result}
	}
	valid, err := decodeRPCRuntimeVersion(encode("node-subtensor", 452, 1))
	if err != nil || validateRPCRuntimeIdentity(valid, valid, 452, 1) != nil {
		t.Fatalf("valid runtime identity rejected: %+v %v", valid, err)
	}
	cases := []rpcResponse{
		{Result: json.RawMessage(`{}`)},
		{Result: json.RawMessage(`null`)},
		{Error: &rpcResponseError{Code: -32000, Message: "unavailable"}},
	}
	for index, response := range cases {
		if _, decodeErr := decodeRPCRuntimeVersion(response); decodeErr == nil {
			t.Errorf("malformed runtime response %d was accepted", index)
		}
	}
	identities := []struct {
		private rpcRuntimeVersion
		public  rpcRuntimeVersion
	}{
		{private: valid, public: rpcRuntimeVersion{SpecName: "node-subtensor", SpecVersion: 453, TransactionVersion: 1}},
		{private: valid, public: rpcRuntimeVersion{SpecName: "other", SpecVersion: 452, TransactionVersion: 1}},
		{private: valid, public: rpcRuntimeVersion{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 2}},
	}
	for index, identity := range identities {
		if identityErr := validateRPCRuntimeIdentity(identity.private, identity.public, 452, 1); identityErr == nil {
			t.Errorf("drifting runtime identity %d was accepted", index)
		}
	}
}

func TestRPCAdversaryRejectsObservedRuntimeDrift(t *testing.T) {
	newServer := func(spec uint32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			defer request.Body.Close()
			var call struct {
				ID     uint64 `json:"id"`
				Method string `json:"method"`
			}
			if json.NewDecoder(request.Body).Decode(&call) != nil {
				http.Error(writer, "malformed request", http.StatusBadRequest)
				return
			}
			var result any
			switch call.Method {
			case "eth_getBlockByNumber":
				result = rpcBlock{Number: "0x64", Hash: "0x" + strings.Repeat("ab", 32)}
			case "state_getRuntimeVersion":
				result = rpcRuntimeVersion{SpecName: "node-subtensor", SpecVersion: spec, TransactionVersion: 1}
			default:
				_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "error": map[string]any{"code": -32601, "message": "unknown method"}})
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
		}))
	}
	private := newServer(452)
	defer private.Close()
	public := newServer(453)
	defer public.Close()
	cfg := testResolvedConfig(t)
	cfg.OperationalEVM = private.URL
	cfg.Public.Chain.EVMPublicReadEndpoint = public.URL
	cfg.Release.Runtime.SpecVersion = 452
	cfg.Release.Runtime.TransactionVersion = 1
	actor := &rpcAdversary{
		cfg: cfg,
		http: &adversaryHTTP{
			gate:    &adversaryRequestGate{now: time.Now},
			timeout: time.Second,
		},
	}
	result := actor.Sample(context.Background(), adversaryAttackPhase, 2)
	if result.Outcome != adversaryOutcomeError || result.Requests != 8 || !strings.Contains(result.Detail, "runtime specs operational=452 public=453 expected=452") {
		t.Fatalf("observed runtime drift result=%+v", result)
	}
}

func TestConsensusWeightComparisonIncludesUIDZero(t *testing.T) {
	left := map[uint16]uint64{0: 600_000, 1: 400_000}
	right := cloneWeights(left)
	if !equalWeights(left, right) {
		t.Fatal("identical vector containing UID zero was rejected")
	}
	right[0]--
	if equalWeights(left, right) {
		t.Fatal("UID-zero weight drift was ignored")
	}
}

func TestAdversarialAssignVerificationRejectsSignatureAndPathDrift(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 1
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	serverSeed := make([]byte, ed25519.SeedSize)
	serverSeed[0] = 2
	serverPrivate := ed25519.NewKeyFromSeed(serverSeed)
	serverPublic := serverPrivate.Public().(ed25519.PublicKey)
	trailID, _ := connect.IdFromBytes([]byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	confirmed, _ := connect.IdFromBytes([]byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2})
	next, _ := connect.IdFromBytes([]byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3})
	nonce := make([]byte, connect.VerifyNonceSize)
	message, err := connect.BuildVerifyAssignMessage(7, trailID, nonce, public, connect.VerifyMMin, []connect.Id{confirmed, next})
	if err != nil {
		t.Fatal(err)
	}
	assign := &connect.VerifyAssignResult{TrailId: trailID, ServerNonce: nonce, Trail: []connect.Id{confirmed}, NextHop: next, M: connect.VerifyMMin, ServerKeyId: 7, AssignSig: ed25519.Sign(serverPrivate, message)}
	if err := validateAdversaryAssign(assign, []connect.Id{confirmed}, public, map[byte]ed25519.PublicKey{7: serverPublic}); err != nil {
		t.Fatal(err)
	}
	tampered := *assign
	tampered.NextHop = confirmed
	if err := validateAdversaryAssign(&tampered, []connect.Id{confirmed}, public, map[byte]ed25519.PublicKey{7: serverPublic}); err == nil {
		t.Fatal("repeated adversarial ASSIGN hop was accepted")
	}
	tampered = *assign
	tampered.AssignSig = append([]byte(nil), assign.AssignSig...)
	tampered.AssignSig[0] ^= 1
	if err := validateAdversaryAssign(&tampered, []connect.Id{confirmed}, public, map[byte]ed25519.PublicKey{7: serverPublic}); err == nil {
		t.Fatal("tampered adversarial ASSIGN signature was accepted")
	}
}

func TestVerifyFinalIntegrityModelsRejectMITMMutations(t *testing.T) {
	validatorSeed := make([]byte, ed25519.SeedSize)
	validatorSeed[0] = 11
	validatorPrivate := ed25519.NewKeyFromSeed(validatorSeed)
	validatorPublic := validatorPrivate.Public().(ed25519.PublicKey)
	serverSeed := make([]byte, ed25519.SeedSize)
	serverSeed[0] = 22
	serverPrivate := ed25519.NewKeyFromSeed(serverSeed)
	serverPublic := serverPrivate.Public().(ed25519.PublicKey)
	id := func(first, last byte) connect.Id {
		value := make([]byte, 16)
		value[0], value[len(value)-1] = first, last
		result, err := connect.IdFromBytes(value)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	trailID := id(1, 1)
	trail := []connect.Id{id(2, 2), id(3, 3)}
	nonce := make([]byte, connect.VerifyNonceSize)
	nonce[0], nonce[len(nonce)-1] = 4, 5
	hops := []connect.VerifyProofHop{
		{ClientId: trail[0], TimeMs: 1_000, EgressIpHash: [32]byte{1}},
		{ClientId: trail[1], TimeMs: 2_000, EgressIpHash: [32]byte{2}},
	}
	const serverKeyID byte = 7
	finalMessage, err := connect.BuildVerifyFinalMessage(serverKeyID, trailID, nonce, validatorPublic, byte(len(trail)), hops)
	if err != nil {
		t.Fatal(err)
	}
	extendMessage, err := connect.BuildVerifyExtendMessage(trailID, nonce, validatorPublic, byte(len(trail)), trail)
	if err != nil {
		t.Fatal(err)
	}
	verifierSignature := ed25519.Sign(validatorPrivate, extendMessage)
	result := &connect.VerifyFinalResult{
		Status: connect.VerifyStatusComplete,
		Proof: &connect.VerifyProof{
			Header: connect.VerifyProofHeader{TrailId: trailID, ServerNonce: nonce, Vpk: validatorPublic, M: len(trail)},
			Hops:   hops, ServerKeyId: serverKeyID, Coverage: uint64(len(trail) - 1),
			FinalSig: ed25519.Sign(serverPrivate, finalMessage), VerifierSig: verifierSignature,
		},
	}
	evidence, err := verifyFinalIntegrityModels(result, trailID, nonce, len(trail), trail, verifierSignature, validatorPublic, map[byte]ed25519.PublicKey{serverKeyID: serverPublic})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.CanonicalBodyMutationRejections != 10 || evidence.SignedResponseTamperRejections != 12 || evidence.DuplicateHopRejections != 1 || evidence.SourceMismatchRejections != 1 {
		t.Fatalf("unexpected integrity evidence: %+v", evidence)
	}
	if err := validateAdversaryFinal(result, trailID, nonce, len(trail), trail, verifierSignature, validatorPublic, map[byte]ed25519.PublicKey{serverKeyID: serverPublic}); err != nil {
		t.Fatalf("integrity mutation model modified its real baseline: %v", err)
	}
}

func TestPoisonMetricEvidenceRequiresMeasuredDelta(t *testing.T) {
	metrics, err := poisonMetricEvidence("operator=1 stable_synthetic_source=true size_delta=17")
	if err != nil {
		t.Fatal(err)
	}
	if metrics["response_size_delta_bytes"] != 17 || metrics["route_distinguishability_ppm"] != 0 {
		t.Fatalf("unexpected poison metrics: %v", metrics)
	}
	if _, err := poisonMetricEvidence("stable_synthetic_source=true"); err == nil {
		t.Fatal("missing response size delta was accepted")
	}
}

func TestVerifyAdversaryMalformedSignatureSchedule(t *testing.T) {
	for _, test := range []struct {
		sequence uint64
		length   int
		metric   string
	}{
		{sequence: 2, length: 0, metric: "missing_signature_rejections"},
		{sequence: 6, length: ed25519.SignatureSize, metric: "invalid_signature_rejections"},
		{sequence: 18, length: 0, metric: "missing_signature_rejections"},
		{sequence: 22, length: ed25519.SignatureSize, metric: "invalid_signature_rejections"},
	} {
		signature, metric := adversaryMalformedSignature(test.sequence)
		if len(signature) != test.length || metric != test.metric {
			t.Errorf("sequence=%d length=%d metric=%s, want %d/%s", test.sequence, len(signature), metric, test.length, test.metric)
		}
	}
}

func TestAdversarialConfigIsMandatoryAndBounded(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Scenarios.Adversaries.Enabled = false
	if err := cfg.Config.Validate(); err == nil {
		t.Fatal("disabled adversarial release campaign was accepted")
	}
	cfg = testResolvedConfig(t)
	cfg.Config.Scenarios.Adversaries.MaximumOperatorRequestsPerSec = 21
	if err := cfg.Config.Validate(); err == nil {
		t.Fatal("operator adversarial QPS above the shared-testnet ceiling was accepted")
	}
	cfg = testResolvedConfig(t)
	cfg.Config.Scenarios.Adversaries.MaximumRPCRequestsPerSec = 6
	if err := cfg.Config.Validate(); err == nil {
		t.Fatal("RPC adversarial QPS above the shared-testnet ceiling was accepted")
	}
	cfg = testResolvedConfig(t)
	cfg.Config.Scenarios.Adversaries.MaximumActorErrorRatePPM = 1
	if err := cfg.Config.Validate(); err == nil {
		t.Fatal("nonzero adversarial error allowance was accepted for the release gate")
	}
	cfg = testResolvedConfig(t)
	cfg.Config.Scenarios.Adversaries.MinimumSamplesPerActor = 99
	if err := cfg.Config.Validate(); err == nil {
		t.Fatal("undersampled adversarial release campaign was accepted")
	}
	cfg = testResolvedConfig(t)
	cfg.Config.Scenarios.Adversaries.MaximumAttackControlP95Ratio = 0
	if err := cfg.Config.Validate(); err == nil {
		t.Fatal("missing adversarial attack/control resilience bound was accepted")
	}
}

func TestAdversarialMatrixPathRejectsTraversal(t *testing.T) {
	cfg := testResolvedConfig(t)
	if _, err := loadAdversarialMatrix(cfg.Repos.SN, "../outside.json"); err == nil {
		t.Fatal("adversarial matrix traversal was accepted")
	}
	if _, err := loadAdversarialMatrix(cfg.Repos.SN, filepath.Join(string(filepath.Separator), "tmp", "matrix.json")); err == nil {
		t.Fatal("absolute adversarial matrix path was accepted")
	}
}

func TestAdversarialScenarioRequiresCampaignImplementation(t *testing.T) {
	cfg := testResolvedConfig(t)
	definition := scenarioDefinition{Name: "missing-adversaries", AdversarialMatrixHash: "0x1", Checks: []scenarioCheck{{ID: "ready", Check: func(*scenarioEvaluation) (bool, string) { return true, "ready" }}}}
	_, err := runScenarioWithProbe(context.Background(), cfg, t.TempDir(), definition, &staticScenarioProbe{observations: []*ScenarioObservation{testScenarioObservation(cfg, 1)}}, scenarioRunOptions{Publish: false})
	if err == nil || !strings.Contains(err.Error(), "continuous adversarial campaign") {
		t.Fatalf("missing adversarial implementation was accepted: %v", err)
	}
}

func TestScenarioInitialFailureStopsAdversaries(t *testing.T) {
	cfg := testResolvedConfig(t)
	evidence := healthyAdversaryEvidence()
	campaign := &scenarioAdversaryStub{evidence: evidence}
	definition := scenarioDefinition{Name: "initial-adversarial-failure", AdversarialMatrixHash: evidence.MatrixHash, Checks: []scenarioCheck{{ID: "unused", Check: func(*scenarioEvaluation) (bool, string) { return true, "" }}}}
	result, err := runScenarioWithProbe(context.Background(), cfg, t.TempDir(), definition, &staticScenarioProbe{err: errors.New("rpc unavailable")}, scenarioRunOptions{Publish: false, Adversaries: campaign})
	if err == nil || result == nil || campaign.stopCalls != 1 || result.Adversaries == nil {
		t.Fatalf("result=%+v error=%v campaign=%+v", result, err, campaign)
	}
}
