//go:build linux || darwin

package validator

// Rule R1 (VERIFY-COHORT-V2.md §2.1) with genuine M8 trails, signed disk
// records, the real producer, the real commit postimage, an independent replay
// and a restart. Only the mock server's provider list chooses who is drawn:
// its seed is always providers[0], so with eight providers every complete
// trail assigns each of the other seven exactly once.

import (
	"slices"
	"testing"

	"github.com/urnetwork/connect"
)

// The producer retires idle priors of a measured window; a verifier accepts
// that fold or the keep-all fold of the same signed statistics, and refuses
// any other set even under a genuine signature.
func TestAttemptSettlementV2RetiresIdlePriorsAfterMeasuredWindow(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementV2TestOperator(t, 9, 2, 0)
	assigned := ""
	for index := range fixture.measurement.Providers {
		provider := &fixture.measurement.Providers[index]
		if 0 < provider.Assignments && provider.Assignments < 8 {
			// an established provider measured sparsely this window
			provider.HasPriorQuality, provider.PriorQualityPPM = true, 432_100
			assigned = provider.ClientID
			break
		}
	}
	idleZero, idleScored := connect.Id{0xfe, 1}.String(), connect.Id{0xfe, 2}.String()
	fixture.measurement.Providers = append(fixture.measurement.Providers,
		ReleaseProviderMeasurement{ClientID: idleZero, HasPriorQuality: true, PriorQualityPPM: 0, LatencyBuckets: make([]uint64, statsLatencyBuckets)},
		ReleaseProviderMeasurement{ClientID: idleScored, HasPriorQuality: true, PriorQualityPPM: 765_432, LatencyBuckets: make([]uint64, statsLatencyBuckets)},
	)
	slices.SortFunc(fixture.measurement.Providers, func(a, b ReleaseProviderMeasurement) int {
		switch {
		case a.ClientID < b.ClientID:
			return -1
		case a.ClientID > b.ClientID:
			return 1
		}
		return 0
	})
	if assigned == "" {
		t.Fatal("fixture lacks a sparsely measured provider")
	}
	closure := sealAttemptSettlementV2Test(t, fixture)
	// every replay owns fresh scratch, so each verification takes new options
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, attemptSettlementV2TestOptions(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	postFold := closure.Transitions[0].PostFold
	ids := make([]string, len(postFold))
	for index, quality := range postFold {
		ids[index] = quality.ClientID
	}
	if slices.Contains(ids, idleZero) || slices.Contains(ids, idleScored) {
		t.Fatal("the signed post-fold kept a prior idle through a measured window")
	}
	position := slices.Index(ids, assigned)
	if position < 0 || postFold[position].QualityPPM != 432_100 {
		t.Fatal("a sparsely measured prior was retired or decayed")
	}

	// a validator rolled back to a keep-all producer stays verifiable
	keepAll := cloneAttemptSettlementV2Test(t, closure)
	keepAll.Transitions[0].PostFold = attemptSettlementV2KeepAllQualities(result.Operators[9].Stats)
	if len(keepAll.Transitions[0].PostFold) != len(postFold)+2 {
		t.Fatal("the keep-all fold did not keep exactly the two idle priors")
	}
	resignAttemptSettlementV2Test(t, keepAll, fixture)
	if _, err := VerifyAttemptSettlementClosureV2(t.Context(), keepAll, attemptSettlementV2TestOptions(t, fixture)); err != nil {
		t.Fatalf("a verifier refused the keep-all fold: %v", err)
	}
	// retiring one idle prior and keeping the other is neither rule
	partial := cloneAttemptSettlementV2Test(t, keepAll)
	partial.Transitions[0].PostFold = slices.DeleteFunc(slices.Clone(partial.Transitions[0].PostFold), func(quality AttemptSettlementQuality) bool { return quality.ClientID == idleZero })
	resignAttemptSettlementV2Test(t, partial, fixture)
	if _, err := VerifyAttemptSettlementClosureV2(t.Context(), partial, attemptSettlementV2TestOptions(t, fixture)); err == nil {
		t.Fatal("a verifier accepted a post-fold that neither rule produces")
	}
}

// Window 42 scores seven providers of each operator. In window 43 operator 9
// measures six of them and one new provider, so its seventh scored provider is
// idle and leaves; operator 11 measures nobody and keeps every prior. The real
// commit path folds the engine to the signed post-fold, an independent replay
// and the lineage verifier accept it, and a restart reproduces it. Before R1
// the idle provider stayed in operator 9's census forever.
func TestAttemptSettlementRuntimeV2RetiresIdlePriorAcrossRealWindows(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	all := make([][]connect.Id, len(fixture.fixtures))
	for index, operator := range fixture.fixtures {
		all[index] = slices.Clone(operator.server.providers)
		operator.server.providers = all[index][:8]
		fixture.trails(t, index, 8, 0)
		operator.server.providers = all[index]
	}
	first, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	for index := range fixture.fixtures {
		if len(first.Transitions[index].PostFold) != 7 {
			t.Fatalf("no_id %d scored %d providers in window 42, want the seven non-seed providers", fixture.participants[index].NoID, len(first.Transitions[index].PostFold))
		}
	}
	priorAuthority := fixture.options(t).Authority
	fixture.nextWindow(t, first)
	idle, fresh := all[0][7], all[0][8]
	measured := fixture.fixtures[0]
	measured.server.providers = append(slices.Clone(all[0][:7]), fresh)
	fixture.trails(t, 0, 2, 0)
	measured.server.providers = all[0]

	physical := attemptSettlementV2PhysicalIO()
	var transaction *attemptSettlementTransactionV2
	physical.writeJournal = func(root *attemptPrivateDirectory, name string, raw []byte) error {
		var err error
		transaction, _, _, err = decodeAttemptSettlementTransactionV2(t.Context(), raw, fixture.participants, runtimeAttemptSettlementV2TestPersistence())
		if err != nil {
			return err
		}
		return writeAttemptSettlementV2OwnedState(root, name, raw)
	}
	next, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 44, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err != nil || next == nil || transaction == nil {
		t.Fatalf("real second settlement: %v", err)
	}

	carried := map[string]uint32{}
	for _, quality := range first.Transitions[0].PostFold {
		carried[quality.ClientID] = quality.QualityPPM
	}
	retired := next.Transitions[0].PostFold
	if len(retired) != 6 {
		t.Fatalf("operator 9 kept %d priors, want the six it measured", len(retired))
	}
	for _, quality := range retired {
		if quality.ClientID == idle.String() || quality.ClientID == fresh.String() || carried[quality.ClientID] != quality.QualityPPM {
			t.Fatalf("operator 9's post-fold kept the idle provider, scored a sparse new one or changed a sparse prior: %s", quality.ClientID)
		}
	}
	if !slices.Equal(next.Transitions[1].PostFold, first.Transitions[1].PostFold) {
		t.Fatal("an unmeasured window retired operator 11's priors")
	}
	stats := fixture.participants[0].Stats
	stats.mu.Lock()
	_, idleExact := stats.emaPPM[idle]
	_, idleReporting := stats.ema[idle]
	census := len(stats.releaseStatsMeasurementWithLock().Providers)
	stats.mu.Unlock()
	if idleExact || idleReporting || census != 6 {
		t.Fatalf("operator 9's engine kept the idle provider (exact %t, reporting %t) or holds a census of %d", idleExact, idleReporting, census)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)

	read, _, err := ReadAttemptSettlementClosureV2(t.Context(), fixture.coordinator, 43, fixture.options(t).Authority)
	if err != nil || !slices.Equal(read.Transitions[0].PostFold, retired) {
		t.Fatalf("independent replay of the retiring settlement: %v", err)
	}
	if _, err := VerifyAttemptSettlementClosureV2Lineage(t.Context(), first, next, priorAuthority, fixture.options(t).Authority); err != nil {
		t.Fatalf("lineage across the retiring fold: %v", err)
	}
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatalf("restart after the retiring fold: %v", err)
	}
	after := runtimeAttemptSettlementV2TestImages(t, restarted)
	for index := range before {
		if !slices.Equal(before[index], after[index]) {
			t.Fatal("restart refolded or changed the retiring postimage")
		}
	}
}
