package main

import (
	"maps"
	"math"
	"strconv"
	"strings"
	"testing"
)

// A repair converges locally when unrelated emissions or an operator repair
// have already restored the exact approved destination floor.
func TestAlphaRepairPrebroadcastSkipsSatisfiedNewIntent(t *testing.T) {
	skip, err := alphaRepairPrebroadcast(100, 100, 10, false)
	if err != nil || !skip {
		t.Fatalf("satisfied new repair was not skipped: skip=%t error=%v", skip, err)
	}
}

// A recorded intent must recover its immutable transaction instead of
// silently adopting current state after a crash.
func TestAlphaRepairPrebroadcastRecoversRecordedIntent(t *testing.T) {
	skip, err := alphaRepairPrebroadcast(100, 100, 10, true)
	if err != nil || skip {
		t.Fatalf("recorded repair did not require recovery: skip=%t error=%v", skip, err)
	}
}

// An undersized or overflowing repair is rejected before native signing.
func TestAlphaRepairPrebroadcastRejectsUnreachableAndInvalidBounds(t *testing.T) {
	for _, test := range []struct {
		current, minimum, credit uint64
	}{
		{current: 1, minimum: 100, credit: 98},
		{current: ^uint64(0) - 1, minimum: ^uint64(0), credit: 2},
		{current: 1, minimum: 0, credit: 1},
		{current: 1, minimum: 2, credit: 0},
	} {
		if _, err := alphaRepairPrebroadcast(test.current, test.minimum, test.credit, false); err == nil {
			t.Fatalf("invalid repair bound %+v was accepted", test)
		}
	}
}

func TestAlphaTransferMinimumMirrorsRuntimeFloorAtBoundary(t *testing.T) {
	// This is the finalized netuid-521 price which exposed the original 0.09
	// alpha validator plan as an AmountTooLow dispatch failure.
	const priceQ9 = uint64(568_309)
	minimum, err := minimumAlphaTransferRao(100_000, priceQ9, 0)
	if err != nil {
		t.Fatal(err)
	}
	if minimum != 175_960_613 {
		t.Fatalf("minimum alpha = %d, want 175960613", minimum)
	}
	atBoundary, err := alphaTransferTAOEquivalentRao(minimum, priceQ9)
	if err != nil {
		t.Fatal(err)
	}
	belowBoundary, err := alphaTransferTAOEquivalentRao(minimum-1, priceQ9)
	if err != nil {
		t.Fatal(err)
	}
	if atBoundary < 100_000 || belowBoundary >= 100_000 {
		t.Fatalf("runtime floor boundary = below:%d at:%d", belowBoundary, atBoundary)
	}
	failedAmount, err := alphaTransferTAOEquivalentRao(90_000_000, priceQ9)
	if err != nil {
		t.Fatal(err)
	}
	if failedAmount != 51_147 || failedAmount >= 100_000 {
		t.Fatalf("failed 0.09-alpha plan has TAO equivalent %d", failedAmount)
	}
}

func TestAlphaTransferMinimumAppliesCeilingMarginAndRejectsDrift(t *testing.T) {
	minimum, err := minimumAlphaTransferRao(100_000, 568_309, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	equivalent, err := alphaTransferTAOEquivalentRao(minimum, 568_309)
	if err != nil || equivalent < 110_000 {
		t.Fatalf("margin equivalent=%d error=%v", equivalent, err)
	}
	if lower, err := alphaTransferTAOEquivalentRao(minimum, 500_000); err != nil || lower >= 110_000 {
		t.Fatalf("downward price drift was not distinguishable: equivalent=%d error=%v", lower, err)
	}
	for _, input := range []struct {
		minimum, price uint64
		margin         uint16
	}{{0, 1, 0}, {1, 0, 0}, {1, 1, 5_001}, {math.MaxUint64, 1, 5_000}} {
		if _, err := minimumAlphaTransferRao(input.minimum, input.price, input.margin); err == nil {
			t.Fatalf("invalid/overflow input %+v was accepted", input)
		}
	}
}

func TestReserveValidatorTransferTargetsMajorityWithoutDepositCoupling(t *testing.T) {
	transfer, finalReserve, err := reserveValidatorTransferRao(26_000_000_000_000, 0, 3_900_000_000, 6_500)
	if err != nil {
		t.Fatal(err)
	}
	if transfer != 16_900_000_000_001 || finalReserve != transfer-1 || !alphaShareMeets(26_000_000_000_000, finalReserve, 6_500) {
		t.Fatalf("reserve transfer/final = %d/%d", transfer, finalReserve)
	}
	if alphaShareMeets(26_000_000_000_000, finalReserve-1, 6_500) {
		t.Fatal("one rao below the exact share boundary was accepted")
	}
	if _, _, err := reserveValidatorTransferRao(100, 90, 20, 6_500); err == nil {
		t.Fatal("reserve overfunding beyond registered alpha was accepted")
	}
}

func TestAlphaTransferDestinationRoundingEnvelopeIsExactAndBounded(t *testing.T) {
	const before, requested = uint64(17), uint64(245_000_000_025)
	for _, test := range []struct {
		name  string
		after uint64
		want  uint64
	}{
		{name: "exact", after: before + requested, want: requested},
		{name: "one-rao share floor", after: before + requested - 1, want: requested - 1},
	} {
		credited, err := alphaTransferCreditedRao(before, test.after, requested, 1)
		if err != nil || credited != test.want {
			t.Errorf("%s credited=%d error=%v, want %d", test.name, credited, err, test.want)
		}
	}
	for _, after := range []uint64{before - 1, before + requested - 2, before + requested + 1} {
		if _, err := alphaTransferCreditedRao(before, after, requested, 1); err == nil {
			t.Errorf("unsafe destination post-state %d was accepted", after)
		}
	}
	if _, err := alphaTransferCreditedRao(0, 0, 0, 1); err == nil {
		t.Fatal("zero/underflowing transfer envelope was accepted")
	}
}

func TestAlphaTransferActionRoundingParametersRejectAdjacentDrift(t *testing.T) {
	action := Action{ID: "alpha.transfer.operator-deposit.2", Parameters: map[string]string{
		"maximum_destination_rounding_shortfall_rao": "1",
		"minimum_destination_credit_rao":             "99",
	}, Spend: Spend{AlphaRao: 100}}
	if got, err := alphaTransferRoundingShortfall(action); err != nil || got != 1 {
		t.Fatalf("approved rounding envelope=%d error=%v", got, err)
	}
	for _, mutate := range []func(map[string]string){
		func(p map[string]string) { p["maximum_destination_rounding_shortfall_rao"] = "2" },
		func(p map[string]string) { p["minimum_destination_credit_rao"] = "98" },
	} {
		copy := Action{ID: action.ID, Parameters: maps.Clone(action.Parameters), Spend: action.Spend}
		mutate(copy.Parameters)
		if _, err := alphaTransferRoundingShortfall(copy); err == nil {
			t.Fatalf("drifted rounding envelope %v was accepted", copy.Parameters)
		}
	}
	legacy := Action{ID: action.ID, Spend: action.Spend, Parameters: map[string]string{}}
	if got, err := alphaTransferRoundingShortfall(legacy); err != nil || got != 1 {
		t.Fatalf("release-locked legacy envelope=%d error=%v", got, err)
	}
}

func TestAlphaTransferRepairIDsAcceptOnlyBoundedPositiveSequences(t *testing.T) {
	for _, id := range []string{"alpha.repair.operator-deposit.1", "alpha.repair.operator-deposit.1.2", "alpha.repair.validator.2.9"} {
		if _, _, err := alphaTransferTargetFromActionID(id); err != nil {
			t.Errorf("valid repair id %q was rejected: %v", id, err)
		}
	}
	for _, id := range []string{"alpha.repair.operator-deposit.1.0", "alpha.repair.operator-deposit.1.1", "alpha.repair.operator-deposit.1.2.3", "alpha.transfer.operator-deposit.1.2"} {
		if _, _, err := alphaTransferTargetFromActionID(id); err == nil {
			t.Errorf("invalid repair id %q was accepted", id)
		}
	}
}

func TestAlphaTransferCapacityAppliesEveryRuntimeRestriction(t *testing.T) {
	tests := []struct {
		name                                           string
		source, total, lock, positionBond, coldkeyBond uint64
		want                                           uint64
	}{
		{name: "source position", source: 9_000, total: 10_000, want: 9_000},
		{name: "position collateral", source: 9_000, total: 10_000, positionBond: 2_000, coldkeyBond: 2_000, want: 7_000},
		{name: "coldkey collateral", source: 9_000, total: 10_000, positionBond: 1_000, coldkeyBond: 4_000, want: 6_000},
		{name: "stored conviction lock", source: 9_000, total: 10_000, lock: 5_000, want: 5_000},
	}
	for _, test := range tests {
		got, err := alphaTransferCapacity(test.source, test.total, test.lock, test.positionBond, test.coldkeyBond)
		if err != nil || got != test.want {
			t.Errorf("%s capacity=%d error=%v, want %d", test.name, got, err, test.want)
		}
	}
	for _, invalid := range [][5]uint64{{0, 1, 0, 0, 0}, {2, 1, 0, 0, 0}, {1, 1, 2, 0, 0}, {1, 1, 0, 2, 2}, {1, 1, 0, 1, 0}} {
		if _, err := alphaTransferCapacity(invalid[0], invalid[1], invalid[2], invalid[3], invalid[4]); err == nil {
			t.Errorf("invalid capacity inputs %v were accepted", invalid)
		}
	}
}

func TestAlphaTransferPrebroadcastRejectsPriceDriftAndRegistrationDrift(t *testing.T) {
	var source, destination [32]byte
	source[0], destination[0] = 1, 2
	minimum, err := minimumAlphaTransferRao(100_000, 568_309, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	action := Action{ID: "alpha.transfer.validator.2", Parameters: map[string]string{
		"exact_amount_rao":                     strconv.FormatUint(minimum, 10),
		"runtime_default_min_transfer_tao_rao": "100000",
	}, Spend: Spend{AlphaRao: minimum}}
	live := liveAlphaTransferEconomics{
		DefaultMinTransferRao: 100_000, AlphaPriceQ9: 568_309,
		SourcePositionRao: 9_000_000_000, SourceColdkeyTotalRao: 9_000_000_000, SourceTransferableRao: 9_000_000_000,
		Snapshot: RegisteredAlphaSnapshot{TotalAlphaRao: 10_000_000_000, ByHotkey: map[[32]byte]uint64{source: 9_000_000_000, destination: 0}},
	}
	if err := validateAlphaTransferAtSnapshot(action, source, destination, live, 1_000, 6_000, 1_000_000_000, false); err != nil {
		t.Fatalf("approved transfer was rejected: %v", err)
	}
	driftedFloor := live
	driftedFloor.DefaultMinTransferRao++
	if err := validateAlphaTransferAtSnapshot(action, source, destination, driftedFloor, 1_000, 6_000, 1_000_000_000, false); err == nil || !strings.Contains(err.Error(), "DefaultMinTransfer changed") {
		t.Fatalf("runtime transfer-floor drift was accepted: %v", err)
	}
	drifted := live
	drifted.AlphaPriceQ9 = 500_000
	if err := validateAlphaTransferAtSnapshot(action, source, destination, drifted, 1_000, 6_000, 1_000_000_000, false); err == nil || !strings.Contains(err.Error(), "stopped before signing") {
		t.Fatalf("unsafe price drift was accepted: %v", err)
	}
	missingSource := live
	missingSource.Snapshot.ByHotkey = map[[32]byte]uint64{destination: 0}
	if err := validateAlphaTransferAtSnapshot(action, source, destination, missingSource, 1_000, 6_000, 1_000_000_000, false); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("unregistered source was accepted: %v", err)
	}
	missingDestination := live
	missingDestination.Snapshot.ByHotkey = map[[32]byte]uint64{source: 9_000_000_000}
	if err := validateAlphaTransferAtSnapshot(action, source, destination, missingDestination, 1_000, 6_000, 1_000_000_000, false); err == nil || !strings.Contains(err.Error(), "destination") {
		t.Fatalf("unregistered destination was accepted: %v", err)
	}
}

func TestAlphaTransferPrebroadcastRejectsLockedOrReservedSourceAlpha(t *testing.T) {
	var source, destination [32]byte
	source[0], destination[0] = 1, 2
	action := Action{ID: "alpha.transfer.validator.2", Parameters: map[string]string{
		"exact_amount_rao":                     "4000000000",
		"runtime_default_min_transfer_tao_rao": "100000",
	}, Spend: Spend{AlphaRao: 4_000_000_000}}
	live := liveAlphaTransferEconomics{
		DefaultMinTransferRao: 100_000, AlphaPriceQ9: 1_000_000_000,
		SourcePositionRao: 10_000_000_000, SourceColdkeyTotalRao: 10_000_000_000, SourceTransferableRao: 3_999_999_999,
		SourceStoredLockRao: 6_000_000_001,
		Snapshot:            RegisteredAlphaSnapshot{TotalAlphaRao: 10_000_000_000, ByHotkey: map[[32]byte]uint64{source: 10_000_000_000, destination: 0}},
	}
	if err := validateAlphaTransferAtSnapshot(action, source, destination, live, 0, 6_000, 2_000_000_000, false); err == nil || !strings.Contains(err.Error(), "transferable alpha") {
		t.Fatalf("locked source alpha was accepted: %v", err)
	}
	live.SourceTransferableRao = 10_000_000_000
	if err := validateAlphaTransferAtSnapshot(action, source, destination, live, 0, 6_000, 7_000_000_000, false); err == nil || !strings.Contains(err.Error(), "minimum remainder") {
		t.Fatalf("source remainder violation was accepted: %v", err)
	}
}

func TestReserveTransferPrebroadcastRequiresLiveMajority(t *testing.T) {
	var source, reserve [32]byte
	source[0], reserve[0] = 1, 2
	action := Action{ID: "alpha.transfer.validator.1", Parameters: map[string]string{
		"exact_amount_rao":                     "6500000000",
		"runtime_default_min_transfer_tao_rao": "100000",
	}, Spend: Spend{AlphaRao: 6_500_000_000}}
	live := liveAlphaTransferEconomics{
		DefaultMinTransferRao: 100_000, AlphaPriceQ9: 1_000_000_000,
		SourcePositionRao: 10_000_000_000, SourceColdkeyTotalRao: 10_000_000_000, SourceTransferableRao: 10_000_000_000,
		Snapshot: RegisteredAlphaSnapshot{TotalAlphaRao: 10_000_000_000, ByHotkey: map[[32]byte]uint64{source: 10_000_000_000, reserve: 0}},
	}
	if err := validateAlphaTransferAtSnapshot(action, source, reserve, live, 0, 6_000, 1_000_000_000, true); err != nil {
		t.Fatalf("reserve majority was rejected: %v", err)
	}
	action.Parameters["exact_amount_rao"] = "6000000000"
	action.Spend.AlphaRao = 6_000_000_000
	if err := validateAlphaTransferAtSnapshot(action, source, reserve, live, 0, 6_000, 1_000_000_000, true); err == nil || !strings.Contains(err.Error(), "reserve transfer stopped") {
		t.Fatalf("rounding-vulnerable exact-boundary reserve was accepted: %v", err)
	}
	action.Parameters["exact_amount_rao"] = "5500000000"
	action.Spend.AlphaRao = 5_500_000_000
	if err := validateAlphaTransferAtSnapshot(action, source, reserve, live, 0, 6_000, 1_000_000_000, true); err == nil || !strings.Contains(err.Error(), "reserve transfer stopped") {
		t.Fatalf("minority reserve transfer was accepted: %v", err)
	}
}
