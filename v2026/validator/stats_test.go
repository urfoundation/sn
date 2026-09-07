package validator

// Statistics math tests (VALIDATOR.md §7): Wilson bound reference values,
// EMA folding, a_min gating, latency buckets, persistence.

import (
	"crypto/ed25519"
	"errors"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// Reference values computed independently (python, the closed-form Wilson
// lower bound at z = 1.96).
func TestWilsonLowerReferenceValues(t *testing.T) {
	cases := []struct {
		c, a uint64
		want float64
	}{
		{45, 50, 0.786395096652},
		{0, 10, 0.0},
		{10, 10, 0.722459831233},
		{1, 1, 0.206543291474},
		{30, 40, 0.598057409330},
	}
	for _, tc := range cases {
		got := WilsonLower(tc.c, tc.a, 1.96)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("WilsonLower(%d, %d): got %.12f want %.12f", tc.c, tc.a, got, tc.want)
		}
	}
	if WilsonLower(5, 0, 1.96) != 0 {
		t.Fatal("a=0 must score 0")
	}
}

func TestWilsonLowerMonotonicity(t *testing.T) {
	// More successes at fixed trials → higher bound; more trials at a fixed
	// rate → higher bound (confidence tightens).
	if !(WilsonLower(9, 10, 1.96) > WilsonLower(8, 10, 1.96)) {
		t.Fatal("not monotone in successes")
	}
	if !(WilsonLower(90, 100, 1.96) > WilsonLower(9, 10, 1.96)) {
		t.Fatal("not monotone in trials at fixed rate")
	}
}

func TestLatencyBucketsAndPercentiles(t *testing.T) {
	w := &ProviderWindow{}
	// 90 samples at ~100ms (bucket covering 64..128 → upper 128),
	// 10 samples at ~5000ms (4096..8192 → upper 8192).
	for i := 0; i < 90; i++ {
		w.LatencyBuckets[latencyBucket(100)]++
	}
	for i := 0; i < 10; i++ {
		w.LatencyBuckets[latencyBucket(5000)]++
	}
	if p50 := w.Percentile(0.50); p50 != 128 {
		t.Fatalf("p50 = %f, want 128 (upper edge of the 100ms bucket)", p50)
	}
	if p95 := w.Percentile(0.95); p95 != 8192 {
		t.Fatalf("p95 = %f, want 8192 (upper edge of the 5000ms bucket)", p95)
	}
	// Sub-millisecond and huge values stay in range.
	if latencyBucket(0.2) != 0 {
		t.Fatal("sub-ms bucket")
	}
	if latencyBucket(math.MaxUint32) >= statsLatencyBuckets {
		t.Fatal("bucket overflow")
	}
}

func TestAMinGate(t *testing.T) {
	stats := NewStatsEngine(StatsConfig{AMin: 5})
	sparse := connect.NewId()
	dense := connect.NewId()
	for i := 0; i < 4; i++ {
		stats.RecordAssignment(sparse)
		stats.RecordConfirmation(sparse, 50)
	}
	for i := 0; i < 5; i++ {
		stats.RecordAssignment(dense)
		stats.RecordConfirmation(dense, 50)
	}
	quality := stats.Quality()
	if _, ok := quality[sparse]; ok {
		t.Fatal("provider below a_min was scored")
	}
	if _, ok := quality[dense]; !ok {
		t.Fatal("provider at a_min was not scored")
	}
	// Folding drops the sparse window without creating an EMA for it.
	stats.Fold()
	if _, ok := stats.Quality()[sparse]; ok {
		t.Fatal("sparse provider gained an EMA at fold")
	}
	if _, ok := stats.Quality()[dense]; !ok {
		t.Fatal("dense provider lost its EMA at fold")
	}
}

func TestEmaFold(t *testing.T) {
	cfg := StatsConfig{AMin: 1, Alpha: 0.1, LatRefMs: 4000}
	stats := NewStatsEngine(cfg)
	hop := connect.NewId()

	// Epoch 1: perfect liveness at 100ms → q1 = wilson(10,10) × 4000/(4000+128).
	for i := 0; i < 10; i++ {
		stats.RecordAssignment(hop)
		stats.RecordConfirmation(hop, 100)
	}
	q1 := WilsonLower(10, 10, 1.96) * (4000.0 / (4000.0 + 128.0))
	quality := stats.Quality()
	if math.Abs(quality[hop]-q1) > 1e-12 {
		t.Fatalf("first-window quality %.12f, want raw %.12f (no EMA history)", quality[hop], q1)
	}
	stats.Fold()
	if got := stats.Quality()[hop]; math.Abs(got-q1) > 1e-12 {
		t.Fatalf("ema after first fold %.12f, want %.12f", got, q1)
	}

	// Epoch 2: total failure → q2raw = 0; ema = 0.1×0 + 0.9×q1.
	for i := 0; i < 10; i++ {
		stats.RecordAssignment(hop)
	}
	stats.Fold()
	want := 0.9 * q1
	if got := stats.Quality()[hop]; math.Abs(got-want) > 1e-12 {
		t.Fatalf("ema after failure epoch %.12f, want %.12f", got, want)
	}
}

func TestQualityPreviewBlend(t *testing.T) {
	cfg := StatsConfig{AMin: 1, Alpha: 0.1, LatRefMs: 4000}
	stats := NewStatsEngine(cfg)
	hop := connect.NewId()
	for i := 0; i < 10; i++ {
		stats.RecordAssignment(hop)
		stats.RecordConfirmation(hop, 100)
	}
	stats.Fold()
	ema := stats.Quality()[hop]

	// Mid-epoch: fresh failures blend as α·raw + (1−α)·ema without folding.
	for i := 0; i < 10; i++ {
		stats.RecordAssignment(hop)
	}
	want := 0.1*0 + 0.9*ema
	if got := stats.Quality()[hop]; math.Abs(got-want) > 1e-12 {
		t.Fatalf("preview blend %.12f, want %.12f", got, want)
	}
}

// TestEgressIpHashAccumulation: the engine accumulates a per-provider set of
// distinct routable egress-IP-hashes (§11.1, D27), dedupes, ignores the zero
// hash, returns a defensive copy, remains independent of the settlement Fold,
// and rotates atomically at the native tempo.
func TestEgressIpHashAccumulation(t *testing.T) {
	stats := NewStatsEngine(StatsConfig{})
	p := connect.NewId()
	h1, h2 := iphash(1), iphash(2)

	stats.RecordEgressHash(p, h1)
	stats.RecordEgressHash(p, h2)
	stats.RecordEgressHash(p, h1)         // duplicate → still one entry (a set)
	stats.RecordEgressHash(p, [32]byte{}) // the zero hash (unstamped hop) is ignored

	got := stats.EgressIpHashes()
	if len(got[p]) != 2 || !got[p][h1] || !got[p][h2] {
		t.Fatalf("egress set %v, want {h1,h2} (deduped, zero ignored)", got[p])
	}

	// The returned map is a copy — mutating it must not affect the engine.
	got[p][iphash(3)] = true
	if stats.EgressIpHashes()[p][iphash(3)] {
		t.Fatal("EgressIpHashes must return a defensive copy")
	}

	// Settlement-quality folding does not erase the independent head window.
	stats.Fold()
	if len(stats.EgressIpHashes()[p]) != 2 {
		t.Fatal("settlement fold erased the native-tempo egress window")
	}

	closed := stats.TakeEgressIpHashes()
	if len(closed[p]) != 2 || len(stats.EgressIpHashes()) != 0 {
		t.Fatalf("native-tempo rotation closed=%v current=%v", closed, stats.EgressIpHashes())
	}
	stats.RecordEgressHash(p, iphash(3))
	if closed[p][iphash(3)] || !stats.EgressIpHashes()[p][iphash(3)] {
		t.Fatal("post-rotation proof leaked into the detached tempo window")
	}
}

// A failed submission may retry in the same native epoch. It must reuse the
// exact detached evidence window instead of consuming a sparse retry window;
// the following epoch then receives evidence recorded after the first cut.
func TestReleaseHeadEvidenceRotatesOncePerNativeEpoch(t *testing.T) {
	first, second := NewStatsEngine(StatsConfig{}), NewStatsEngine(StatsConfig{})
	stateDir := t.TempDir()
	firstState := filepath.Join(stateDir, "no-3")
	secondState := filepath.Join(stateDir, "no-9")
	cfg := &ReleaseConfig{
		StateDir: stateDir, DeploymentID: "measurement-restart", ChainID: 945,
		GenesisHash: releaseHex32([32]byte{1}), Coordinator: "0x0000000000000000000000000000000000000001",
		ValidatorID: 1, Netuid: 521, PolicyHash: releaseHex32([32]byte{2}),
		Operators: []OperatorConfig{{NoID: 3, StateDir: firstState}, {NoID: 9, StateDir: secondState}},
	}
	validatorKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	for _, setup := range []struct {
		noID  uint64
		stats *StatsEngine
		dir   string
	}{{noID: 3, stats: first, dir: firstState}, {noID: 9, stats: second, dir: secondState}} {
		if err := setup.stats.AdvanceSettlementEpoch(4, setup.dir); err != nil {
			t.Fatal(err)
		}
		ledger, err := NewAttemptLedger(setup.dir, AttemptLedgerIdentity{
			DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.GenesisHash,
			Netuid: cfg.Netuid, ValidatorID: cfg.ValidatorID, NoID: setup.noID,
		}, validatorKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := setup.stats.AttachAttemptLedger(ledger, setup.dir); err != nil {
			t.Fatal(err)
		}
	}
	contexts := map[uint64]*ReleaseMeasurementContext{
		9: {NoID: 9, Stats: second},
		3: {NoID: 3, Stats: first},
	}
	steerer := &ReleaseSteerer{cfg: cfg, contexts: contexts}
	snapshot := &ReleaseSnapshot{BlockNumber: 98, BlockHash: [32]byte{7}, Epoch: big.NewInt(4)}

	initial, err := steerer.takeHeadEvidence(41, 100, releaseHex32([32]byte{3}), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	firstInitial, err := VerifyReleaseStatsMeasurement(initial[3].Stats)
	if err != nil {
		t.Fatal(err)
	}
	secondInitial, err := VerifyReleaseStatsMeasurement(initial[9].Stats)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstInitial.Providers) != 0 || len(secondInitial.Providers) != 0 || initial[3].EgressGeneration != 0 || initial[9].EgressGeneration != 0 {
		t.Fatalf("initial native window evidence=%v", initial)
	}
	// Reconstructing the steerer forces the same-epoch retry through durable
	// journals rather than relying on its in-memory cache.
	steerer = &ReleaseSteerer{cfg: cfg, contexts: contexts}
	retry, err := steerer.takeHeadEvidence(41, 101, releaseHex32([32]byte{4}), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	retryStats, err := VerifyReleaseStatsMeasurement(retry[3].Stats)
	if err != nil {
		t.Fatal(err)
	}
	if len(retryStats.Providers) != 0 || retry[3].EgressGeneration != initial[3].EgressGeneration || retry[3].Stats.AttemptCut.Root != initial[3].Stats.AttemptCut.Root {
		t.Fatalf("same-epoch retry changed detached evidence: %v", retry[3])
	}
	next, err := steerer.takeHeadEvidence(42, 102, releaseHex32([32]byte{5}), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	nextStats, err := VerifyReleaseStatsMeasurement(next[3].Stats)
	if err != nil {
		t.Fatal(err)
	}
	if len(nextStats.Providers) != 0 || next[3].EgressGeneration != initial[3].EgressGeneration+1 {
		t.Fatalf("next native epoch evidence=%v, want exactly one generation advance", next[3])
	}
	crossedSettlement := *snapshot
	crossedSettlement.Epoch = big.NewInt(5)
	if _, err := steerer.takeHeadEvidence(42, 103, releaseHex32([32]byte{6}), &crossedSettlement); err == nil {
		t.Fatal("same native window crossed into a different settlement epoch")
	}
	if _, err := steerer.takeHeadEvidence(41, 103, releaseHex32([32]byte{6}), snapshot); err == nil {
		t.Fatal("native epoch regression was accepted")
	}
}

// A failed write-ahead journal must leave the native egress window intact;
// only a successfully persisted atomic snapshot owns and rotates the evidence.
func TestReleaseStatsDetachRotatesOnlyAfterPersistence(t *testing.T) {
	dir := t.TempDir()
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	clientID := connect.NewId()
	hash := iphash(9)
	stats.RecordAssignment(clientID)
	stats.RecordConfirmation(clientID, 100)
	stats.RecordEgressHash(clientID, hash)
	expectedFailure := errors.New("journal unavailable")
	if _, err := stats.detachReleaseStatsMeasurement(dir, func(ReleaseStatsMeasurement, uint64) error { return expectedFailure }); !errors.Is(err, expectedFailure) {
		t.Fatalf("failed journal result = %v", err)
	}
	if !stats.EgressIpHashes()[clientID][hash] {
		t.Fatal("failed journal write destroyed native egress evidence")
	}
	measurement, err := stats.detachReleaseStatsMeasurement(dir, func(ReleaseStatsMeasurement, uint64) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyReleaseStatsMeasurement(measurement)
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Providers[clientID].EgressIPHashes[hash] || verified.Providers[clientID].Exposure != 1 || !verified.Providers[clientID].HasQuality || len(stats.EgressIpHashes()) != 0 {
		t.Fatalf("persisted atomic measurement did not own the complete cut: %+v", verified.Providers[clientID])
	}
}

// Journal-first recovery clears exactly the old durable generation and never
// erases a repeated prefix recorded in the following native window.
func TestReleaseStatsCutReconcilesCrashWithoutDuplicateOrLoss(t *testing.T) {
	dir := t.TempDir()
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	clientID := connect.NewId()
	hash := iphash(7)
	stats.RecordEgressHash(clientID, hash)
	if err := stats.Save(dir); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cutGeneration := uint64(99)
	_, err := stats.detachReleaseStatsMeasurement(blocker, func(_ ReleaseStatsMeasurement, generation uint64) error {
		cutGeneration = generation
		return nil
	})
	if err == nil || cutGeneration != 0 {
		t.Fatalf("simulated post-journal crash generation=%d err=%v", cutGeneration, err)
	}
	restarted := NewStatsEngine(StatsConfig{AMin: 1})
	if err := restarted.Load(dir); err != nil {
		t.Fatal(err)
	}
	if err := restarted.reconcileReleaseStatsCut(dir, cutGeneration); err != nil {
		t.Fatal(err)
	}
	if len(restarted.EgressIpHashes()) != 0 || restarted.egressGeneration != 1 {
		t.Fatalf("recovered cut egress=%v generation=%d", restarted.EgressIpHashes(), restarted.egressGeneration)
	}
	restarted.RecordEgressHash(clientID, hash)
	if err := restarted.Save(dir); err != nil {
		t.Fatal(err)
	}
	if err := restarted.reconcileReleaseStatsCut(dir, cutGeneration); err != nil {
		t.Fatal(err)
	}
	if !restarted.EgressIpHashes()[clientID][hash] {
		t.Fatal("old journal erased a repeated prefix from the next generation")
	}
}

// Settlement ownership is persisted with the quality state, so restarts and a
// failed atomic write cannot skip or duplicate an epoch fold.
func TestStatsSettlementAdvanceIsDurableAndTransactional(t *testing.T) {
	dir := t.TempDir()
	stats := NewStatsEngine(StatsConfig{AMin: 1, AlphaNumerator: 1, AlphaDenominator: 2})
	if err := stats.AdvanceSettlementEpoch(4, dir); err != nil {
		t.Fatal(err)
	}
	clientID := connect.NewId()
	stats.RecordAssignment(clientID)
	stats.RecordConfirmation(clientID, 100)
	before := stats.QualityPPM()[clientID]

	badStateRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badStateRoot, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stats.AdvanceSettlementEpoch(5, badStateRoot); err == nil {
		t.Fatal("settlement fold unexpectedly persisted beneath a regular file")
	}
	if assignments, confirmations := stats.WindowCounts(clientID); assignments != 1 || confirmations != 1 {
		t.Fatalf("failed boundary write mutated the live window: %d/%d", assignments, confirmations)
	}
	if err := stats.AdvanceSettlementEpoch(5, dir); err != nil {
		t.Fatal(err)
	}
	if assignments, _ := stats.WindowCounts(clientID); assignments != 0 {
		t.Fatal("successful boundary did not clear the folded window")
	}
	if got := stats.QualityPPM()[clientID]; got != before {
		t.Fatalf("first durable fold quality %d, want %d", got, before)
	}

	restarted := NewStatsEngine(StatsConfig{AMin: 1, AlphaNumerator: 1, AlphaDenominator: 2})
	if err := restarted.Load(dir); err != nil {
		t.Fatal(err)
	}
	if err := restarted.AdvanceSettlementEpoch(5, dir); err != nil {
		t.Fatal(err)
	}
	if got := restarted.QualityPPM()[clientID]; got != before {
		t.Fatalf("same-epoch restart applied the fold twice: %d, want %d", got, before)
	}
	if err := restarted.AdvanceSettlementEpoch(7, dir); err == nil {
		t.Fatal("unobservable skipped settlement epoch was accepted")
	}
}

func TestStatsSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	hop := connect.NewId()
	for i := 0; i < 8; i++ {
		stats.RecordAssignment(hop)
		stats.RecordConfirmation(hop, 200)
	}
	stats.Fold()
	stats.RecordAssignment(hop) // partial window survives the roundtrip too
	if err := stats.Save(dir); err != nil {
		t.Fatal(err)
	}

	restored := NewStatsEngine(StatsConfig{AMin: 1})
	if err := restored.Load(dir); err != nil {
		t.Fatal(err)
	}
	wantQ := stats.Quality()[hop]
	gotQ := restored.Quality()[hop]
	if math.Abs(wantQ-gotQ) > 1e-12 {
		t.Fatalf("quality after roundtrip %.12f, want %.12f", gotQ, wantQ)
	}
	if a, _ := restored.WindowCounts(hop); a != 1 {
		t.Fatalf("window did not survive roundtrip: a=%d", a)
	}
	if got, want := restored.QualityPPM()[hop], stats.QualityPPM()[hop]; got != want {
		t.Fatalf("exact quality after roundtrip %d, want %d", got, want)
	}
}

func TestQualityPPMExactLatencyAndEMA(t *testing.T) {
	stats := NewStatsEngine(StatsConfig{
		AMin:             10,
		AlphaNumerator:   1,
		AlphaDenominator: 10,
		LatRefMillis:     4000,
	})
	hop := connect.NewId()
	for i := 0; i < 10; i++ {
		stats.RecordAssignment(hop)
		stats.RecordConfirmation(hop, 100) // conservative bucket upper = 128
	}
	wilson := uint64(722459) // exact floor from protocol.ReliabilityPPM(10,10,10)
	wantFirst := uint32(wilson * 4000 / (4000 + 128))
	if got := stats.QualityPPM()[hop]; got != wantFirst {
		t.Fatalf("quality ppm = %d, want %d", got, wantFirst)
	}
	stats.Fold()
	for i := 0; i < 10; i++ {
		stats.RecordAssignment(hop) // all fail, raw = 0
	}
	wantPreview := uint32(9 * uint64(wantFirst) / 10)
	if got := stats.QualityPPM()[hop]; got != wantPreview {
		t.Fatalf("preview ppm = %d, want %d", got, wantPreview)
	}
	stats.Fold()
	if got := stats.QualityPPM()[hop]; got != wantPreview {
		t.Fatalf("folded ppm = %d, want %d", got, wantPreview)
	}
}
