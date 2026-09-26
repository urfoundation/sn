package validator

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/protocol"
)

func exactPolicy(t *testing.T) protocol.Policy {
	t.Helper()
	p, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return *p
}

func TestBuildWeightVectorExactChannelsCedeAndMask(t *testing.T) {
	theta := protocol.Rational{Numerator: 3, Denominator: 10}
	uids, scores, err := BuildWeightVectorExact(
		[]ExactWeightInput{{UID: 1, Score: big.NewRat(1, 1)}, {UID: 2, Score: big.NewRat(3, 1)}},
		[]ExactWeightInput{{UID: 3, Score: big.NewRat(1, 1)}},
		theta,
		map[uint16]bool{2: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(uids) != 2 || uids[0] != 1 || uids[1] != 3 {
		t.Fatalf("uids = %v", uids)
	}
	if scores[0].Cmp(big.NewRat(7, 10)) != 0 || scores[1].Cmp(big.NewRat(3, 10)) != 0 {
		t.Fatalf("scores = %v", scores)
	}

	// Empty head cedes theta to the pool rather than stranding it.
	_, scores, err = BuildWeightVectorExact([]ExactWeightInput{{UID: 7, Score: big.NewRat(2, 1)}}, nil, theta, nil)
	if err != nil || len(scores) != 1 || scores[0].Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatalf("empty-channel result %v, %v", scores, err)
	}
}

func TestReleaseSubmitOptionsPinsSignedPolicyWeightCap(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.VersionKey = 7
	options := releaseSubmitOptions(&cfg)
	if options.VersionKey != 7 || options.MaxWeightLimit == nil || *options.MaxWeightLimit != cfg.Policy.Steering.MaxWeightLimitU16 {
		t.Fatalf("release submit options did not pin policy cap: %+v", options)
	}
}

func TestExactPoolWeightQualityInvertsEqualUsage(t *testing.T) {
	p := exactPolicy(t)
	usage := uint64(10) << 30
	conviction := big.NewInt(0)
	bad, err := impliedUsageQuality(usage, 0, conviction, 760_000, p)
	if err != nil {
		t.Fatal(err)
	}
	good, err := impliedUsageQuality(usage, 0, conviction, 980_000, p)
	if err != nil {
		t.Fatal(err)
	}
	if good.Cmp(bad) <= 0 {
		t.Fatalf("better isolated quality did not invert equal usage: bad=%s good=%s", bad, good)
	}
	// A conviction tier is a discount on what the operator pays, never on its
	// weight: the same audited usage scores the same from any tier.
	discounted, err := impliedUsageQuality(usage, 0, big.NewInt(1_000_000_000), 980_000, p)
	if err != nil {
		t.Fatal(err)
	}
	if discounted.Cmp(good) != 0 {
		t.Fatalf("lower tier rate changed the weight of the same usage: base=%s discounted=%s", good, discounted)
	}
}

// zeroPricePolicy is the testnet policy with every rate zeroed, as the launch
// price sheet publishes it.
func zeroPricePolicy(t *testing.T) protocol.Policy {
	t.Helper()
	p := exactPolicy(t)
	p.Deposit.ZeroRateAction = protocol.DepositZeroRateEqualDemand
	for index := range p.Deposit.Tiers {
		p.Deposit.Tiers[index].RateNumeratorRaoPerGiB = 0
		p.Deposit.Tiers[index].RateNumeratorRaoPerUser = 0
	}
	if err := p.Validate(); err != nil || !p.IsZeroPrice() {
		t.Fatalf("zero-price policy invalid: %v", err)
	}
	return p
}

// TestImpliedDemandPricesAuditedUsageAtBaselineTier pins the two-component
// demand signal: bytes and users priced at the conviction-zero tier, the same
// for every tier, zero without usage, and truncated with the deposit cap.
func TestImpliedDemandPricesAuditedUsageAtBaselineTier(t *testing.T) {
	p := exactPolicy(t)
	p.Deposit.Unit = protocol.DepositUnitRaoPerGiBAndUser
	for index, perUser := range []uint64{10, 8, 6} {
		p.Deposit.Tiers[index].RateNumeratorRaoPerUser = perUser
	}
	const gib = uint64(1) << 30
	capRao := p.Deposit.EpochCapRaoPerOperator
	for _, testCase := range []struct {
		name       string
		bytes      uint64
		users      uint64
		conviction int64
		want       *big.Rat
	}{
		{name: "no usage", want: new(big.Rat)},
		{name: "bytes only", bytes: 2 * gib, want: big.NewRat(2_000_000, 1)},
		{name: "users only", users: 3, want: big.NewRat(30, 1)},
		{name: "both", bytes: 2 * gib, users: 3, want: big.NewRat(2_000_030, 1)},
		{name: "fraction kept exact", bytes: 1, want: big.NewRat(15_625, 16_777_216)},
		{name: "discounted tier pays less, weighs the same", bytes: 2 * gib, users: 3, conviction: 1_000_000_000, want: big.NewRat(2_000_030, 1)},
		{name: "top tier", bytes: 2 * gib, users: 3, conviction: 10_000_000_000, want: big.NewRat(2_000_030, 1)},
		// 20,000 GiB owes 16e9 at the 800,000 tier, above the 1e10 cap: the
		// demand is truncated by the same factor (cap / owed), which is the old
		// cap / rate(tier) bound expressed at the baseline rate.
		{name: "cap truncates demand like the deposit", bytes: 20_000 * gib, conviction: 1_000_000_000, want: new(big.Rat).SetFrac(new(big.Int).Mul(new(big.Int).SetUint64(20_000*1_000_000), new(big.Int).SetUint64(capRao)), new(big.Int).SetUint64(20_000*800_000))},
	} {
		got, err := impliedDemand(testCase.bytes, testCase.users, big.NewInt(testCase.conviction), p.Deposit)
		if err != nil || got.Cmp(testCase.want) != 0 {
			t.Errorf("%s: implied demand = %v (%v), want %v", testCase.name, got, err, testCase.want)
		}
	}
	if _, err := impliedDemand(1, 1, big.NewInt(-1), p.Deposit); err == nil {
		t.Fatal("negative conviction was accepted")
	}
}

// TestZeroPriceImpliedDemandIsExactlyOneTimesQuality pins the launch mode:
// every pool's implied demand is exactly 1, so weights are proportional to
// clamped quality alone; usage, voluntary deposits and conviction do not move
// a pool, and the head split is untouched.
func TestZeroPriceImpliedDemandIsExactlyOneTimesQuality(t *testing.T) {
	p := zeroPricePolicy(t)
	one := big.NewRat(1, 1)
	for _, testCase := range []struct {
		bytes, users uint64
		conviction   int64
	}{{0, 0, 0}, {1 << 40, 250_000, 0}, {0, 0, 10_000_000_000}, {1 << 50, 1, 1_000_000_000}} {
		demand, err := impliedDemand(testCase.bytes, testCase.users, big.NewInt(testCase.conviction), p.Deposit)
		if err != nil || demand.Cmp(one) != 0 {
			t.Fatalf("zero-price demand for %+v = %v, %v", testCase, demand, err)
		}
	}
	low, err := impliedUsageQuality(0, 0, big.NewInt(0), 800_000, p)
	if err != nil {
		t.Fatal(err)
	}
	high, err := impliedUsageQuality(1<<40, 5, big.NewInt(1_000_000_000), 1_000_000, p)
	if err != nil {
		t.Fatal(err)
	}
	if low.Cmp(big.NewRat(800_000, 1_000_000)) != 0 || high.Cmp(one) != 0 {
		t.Fatalf("zero-price scores are not 1 × quality: low=%s high=%s", low, high)
	}
	// The clamp still applies and zero measured quality still scores zero.
	clamped, err := impliedUsageQuality(0, 0, big.NewInt(0), 1, p)
	if err != nil || clamped.Cmp(new(big.Rat).SetFrac(new(big.Int).SetUint64(uint64(p.Steering.QualityTransform.MinimumPPM)), big.NewInt(1_000_000))) != 0 {
		t.Fatalf("zero-price score did not clamp quality: %v %v", clamped, err)
	}
	if none, err := impliedUsageQuality(1, 1, big.NewInt(0), 0, p); err != nil || none.Sign() != 0 {
		t.Fatalf("zero quality scored %v under zero price (%v)", none, err)
	}

	pools := []ExactWeightInput{{UID: 1, Score: low}, {UID: 2, Score: high}}
	uids, scores, err := BuildWeightVectorExact(pools, nil, p.Steering.Theta, nil)
	if err != nil || len(uids) != 2 || scores[0].Cmp(big.NewRat(8, 18)) != 0 || scores[1].Cmp(big.NewRat(10, 18)) != 0 {
		t.Fatalf("quality-only pool split = %v %v (%v)", uids, scores, err)
	}
	uids, scores, err = BuildWeightVectorExact(pools, []ExactWeightInput{{UID: 3, Score: one}}, p.Steering.Theta, nil)
	if err != nil || len(uids) != 3 || scores[0].Cmp(big.NewRat(14, 45)) != 0 || scores[1].Cmp(big.NewRat(7, 18)) != 0 || scores[2].Cmp(big.NewRat(3, 10)) != 0 {
		t.Fatalf("head split changed under zero price: %v %v (%v)", uids, scores, err)
	}
}

func TestPoolQualityPPMIsPerNOAndExcludesHead(t *testing.T) {
	makeStats := func(confirm int) (*StatsEngine, connect.Id) {
		s := NewStatsEngine(StatsConfig{AMin: 8})
		id := connect.NewId()
		for i := 0; i < 8; i++ {
			s.RecordAssignment(id)
			if i < confirm {
				s.RecordConfirmation(id, 100)
			}
		}
		return s, id
	}
	good, goodID := makeStats(8)
	bad, _ := makeStats(2)
	if PoolQualityPPM(good, nil) <= PoolQualityPPM(bad, nil) {
		t.Fatal("isolated operator qualities did not differ")
	}
	if got := PoolQualityPPM(good, map[connect.Id]bool{goodID: true}); got != 0 {
		t.Fatalf("bound head provider leaked into pool quality: %d", got)
	}
}

func TestExactHeadScoresSplitSharedPrefixes(t *testing.T) {
	a, b, c := [32]byte{1}, [32]byte{2}, [32]byte{3}
	got := ExactHeadScores(map[uint16]map[[32]byte]bool{
		10: {a: true, b: true},
		11: {a: true, c: true},
	})
	if got[10].Cmp(big.NewRat(3, 2)) != 0 || got[11].Cmp(big.NewRat(3, 2)) != 0 {
		t.Fatalf("shared-prefix scores = %s, %s", got[10], got[11])
	}
}
