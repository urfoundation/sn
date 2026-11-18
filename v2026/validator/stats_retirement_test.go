package validator

// Rule R1 (VERIFY-COHORT-V2.md §2.1): the settlement fold retires the carried
// quality of a provider with no assignment in a measured window, so the
// validator's census is set by its last two windows and never by history.
// These are pure engine controls; the signed producer, the commit postimage
// and restart are exercised with real trails in attempt_settlement_v2_retirement_test.go.

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

func retirementTestId(n int) connect.Id {
	return connect.Id{0x52, byte(n >> 8), byte(n)}
}

// A window with a assignments and c confirmations, latencies in the 2-4 ms bucket.
func retirementTestWindow(a, c uint64) *ProviderWindow {
	w := &ProviderWindow{Assignments: a, Confirmations: c}
	w.LatencyBuckets[latencyBucket(3)] = c
	return w
}

// One engine with three carried priors and a measured window: a scored prior,
// a sparse prior, an idle prior, and a newly scored provider.
func retirementTestEngine() (*StatsEngine, connect.Id, connect.Id, connect.Id, connect.Id) {
	scored, sparse, idle, fresh := retirementTestId(1), retirementTestId(2), retirementTestId(3), retirementTestId(4)
	engine := NewStatsEngine(StatsConfig{AMin: 8})
	engine.emaPPM = map[connect.Id]uint32{scored: 500_000, sparse: 400_000, idle: 765_432}
	engine.ema = map[connect.Id]float64{scored: 0.5, sparse: 0.4, idle: 0.765432}
	engine.window = map[connect.Id]*ProviderWindow{
		scored: retirementTestWindow(10, 10),
		sparse: retirementTestWindow(3, 3),
		fresh:  retirementTestWindow(9, 9),
	}
	return engine, scored, sparse, idle, fresh
}

func retirementTestCensus(engine *StatsEngine) []connect.Id {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	ids := []connect.Id{}
	for _, provider := range engine.releaseStatsMeasurementWithLock().Providers {
		id, err := connect.ParseId(provider.ClientID)
		if err != nil {
			panic(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// The idle prior leaves with both of its EMAs; the sparse prior carries its
// EMA untouched; the scored prior folds at alpha; the new provider enters.
// Before R1 the idle prior stayed in the census forever.
func TestFoldRetiresPriorAfterIdleSettlementWindow(t *testing.T) {
	engine, scored, sparse, idle, fresh := retirementTestEngine()
	engine.foldStatsOwnedRule(true)
	if _, ok := engine.emaPPM[idle]; ok {
		t.Fatal("an idle prior survived a measured window")
	}
	if _, ok := engine.ema[idle]; ok {
		t.Fatal("an idle prior's reporting EMA survived its exact EMA")
	}
	if engine.emaPPM[sparse] != 400_000 || engine.ema[sparse] != 0.4 {
		t.Fatal("a sparse window decayed or retired an established prior")
	}
	if value, ok := engine.emaPPM[scored]; !ok || value == 500_000 {
		t.Fatal("a scored prior was not folded")
	}
	if _, ok := engine.emaPPM[fresh]; !ok {
		t.Fatal("a newly scored provider gained no quality")
	}
	if len(engine.emaPPM) != len(engine.ema) || len(engine.window) != 0 {
		t.Fatal("the fold left unequal EMA censuses or an open window")
	}
	if census := retirementTestCensus(engine); slices.Contains(census, idle) || len(census) != 3 {
		t.Fatalf("the census after the fold holds %d providers, idle included %t", len(census), slices.Contains(census, idle))
	}

	keepAll, _, _, idleKept, _ := retirementTestEngine()
	keepAll.foldStatsOwned()
	if keepAll.emaPPM[idleKept] != 765_432 || len(keepAll.emaPPM) != 4 {
		t.Fatal("the legacy fold no longer keeps every prior")
	}
}

// A validator that measured nobody for a whole window keeps every prior, so a
// long outage can't wipe the pool's quality to zero.
func TestFoldWithEmptyWindowRetiresNothing(t *testing.T) {
	engine, scored, sparse, idle, _ := retirementTestEngine()
	engine.window = map[connect.Id]*ProviderWindow{}
	engine.foldStatsOwnedRule(true)
	if engine.emaPPM[scored] != 500_000 || engine.emaPPM[sparse] != 400_000 || engine.emaPPM[idle] != 765_432 || len(engine.emaPPM) != 3 || len(engine.ema) != 3 {
		t.Fatal("an unmeasured window retired or changed a prior")
	}
	quality, err := ExactPoolQualityFromReleaseStats(verifiedRetirementTestStats(t, engine), nil)
	if err != nil || quality == 0 {
		t.Fatalf("pool quality after an empty window = %d: %v", quality, err)
	}
	// a window whose providers all have zero assignments measured nobody too
	zero, _, _, zeroIdle, _ := retirementTestEngine()
	zero.window = map[connect.Id]*ProviderWindow{retirementTestId(9): {}}
	zero.foldStatsOwnedRule(true)
	if _, ok := zero.emaPPM[zeroIdle]; !ok {
		t.Fatal("a window with no assignment retired a prior")
	}
}

func verifiedRetirementTestStats(t *testing.T, engine *StatsEngine) VerifiedReleaseStats {
	t.Helper()
	engine.mu.Lock()
	measurement := engine.releaseStatsMeasurementWithLock()
	engine.mu.Unlock()
	measurement.SettlementTransition = nil
	verified, err := VerifyReleaseStatsMeasurement(measurement)
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

// The derived post-fold is R1; verification accepts R1 or the release-1.0
// keep-all fold of the same signed statistics and nothing else, and the engine
// folds to whichever the signed terminal carries.
func TestPostFoldVerifierAcceptsKeepAllOrR1Only(t *testing.T) {
	engine, _, _, idle, _ := retirementTestEngine()
	stats := verifiedRetirementTestStats(t, engine)
	r1, keepAll := attemptSettlementV2Qualities(stats), attemptSettlementV2KeepAllQualities(stats)
	if len(keepAll) != len(r1)+1 || slices.ContainsFunc(r1, func(q AttemptSettlementQuality) bool { return q.ClientID == idle.String() }) {
		t.Fatalf("R1 kept the idle prior or dropped more: %d against %d", len(r1), len(keepAll))
	}
	if !attemptSettlementV2PostFoldAccepted(stats, r1) || !attemptSettlementV2PostFoldAccepted(stats, keepAll) {
		t.Fatal("a verifier refused the R1 or the keep-all fold")
	}
	dropped := slices.DeleteFunc(slices.Clone(r1), func(q AttemptSettlementQuality) bool { return q.QualityPPM == 400_000 })
	for _, other := range [][]AttemptSettlementQuality{dropped, {}, append(slices.Clone(keepAll), AttemptSettlementQuality{ClientID: retirementTestId(99).String(), HasQuality: true, QualityPPM: 1})} {
		if attemptSettlementV2PostFoldAccepted(stats, other) {
			t.Fatalf("a verifier accepted a post-fold of %d providers that neither rule produces", len(other))
		}
		if _, err := attemptSettlementV2FoldMatching(engine, other); err == nil {
			t.Fatal("the engine folded to a post-fold that neither rule produces")
		}
	}
	for _, postFold := range [][]AttemptSettlementQuality{r1, keepAll} {
		before := sortedAttemptEMAQualities(engine.emaPPM)
		post, err := attemptSettlementV2FoldMatching(engine, postFold)
		if err != nil || !slices.Equal(sortedAttemptEMAQualities(post.emaPPM), postFold) || len(post.ema) != len(post.emaPPM) {
			t.Fatalf("the engine did not fold to the signed post-fold: %v", err)
		}
		if !slices.Equal(sortedAttemptEMAQualities(engine.emaPPM), before) || len(engine.window) == 0 {
			t.Fatal("matching a fold changed the owned candidate")
		}
	}
}

// The census bound the server's rolling census relies on (§2.5), with the real
// fold: after any number of measured windows, in which assignments land one
// window either side of the server's label near each boundary, the census
// holds only providers the server assigned in the last three of its epochs.
// A fixed seed keeps it deterministic.
func TestValidatorCensusWithinRollingCount(t *testing.T) {
	random := rand.New(rand.NewPCG(1117, 2026))
	engine := NewStatsEngine(StatsConfig{AMin: 8})
	last := map[connect.Id]int{}
	maxCensus := 0
	for epoch := 1; epoch <= 40; epoch++ {
		// the server's epoch e cohort: a carried core plus fresh draws
		cohort := map[connect.Id]bool{}
		for n := 0; n < 300; n++ {
			cohort[retirementTestId(random.IntN(1200))] = true
		}
		engine.window = map[connect.Id]*ProviderWindow{}
		for id := range cohort {
			a := uint64(1 + random.IntN(20))
			// Near a boundary the server labels some of this validator
			// window's assignments one epoch early (the validator saw the
			// boundary first) or late (an attempt straddled it). Early labels
			// are the tight case of the bound.
			label := epoch
			switch random.IntN(25) {
			case 0:
				label = epoch - 1
			case 1:
				label = epoch + 1
			}
			engine.window[id] = retirementTestWindow(a, uint64(random.IntN(int(a)+1)))
			last[id] = max(last[id], label)
		}
		census := retirementTestCensus(engine)
		maxCensus = max(maxCensus, len(census))
		for _, id := range census {
			if last[id] < epoch-2 {
				t.Fatalf("epoch %d: %s was last assigned in epoch %d but is still in the census", epoch, id, last[id])
			}
		}
		engine.foldStatsOwnedRule(true)
	}
	if maxCensus > 2*300 {
		t.Fatalf("the census reached %d, more than two windows of 300", maxCensus)
	}
}

// ProviderIDs and the raw census agree after a retiring fold: binding-tier
// exclusion never sees a provider the census dropped.
func TestRetiredPriorLeavesProviderIDs(t *testing.T) {
	engine, _, _, idle, _ := retirementTestEngine()
	engine.foldStatsOwnedRule(true)
	if slices.Contains(engine.ProviderIDs(), idle) {
		t.Fatal("a retired prior is still a provider id")
	}
}
