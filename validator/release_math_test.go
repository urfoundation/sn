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

func TestExactPoolWeightQualityInvertsEqualDeposits(t *testing.T) {
	p := exactPolicy(t)
	deposit := big.NewInt(10_000_000)
	conviction := big.NewInt(0)
	bad, err := impliedUsageQuality(deposit, conviction, 760_000, p)
	if err != nil {
		t.Fatal(err)
	}
	good, err := impliedUsageQuality(deposit, conviction, 980_000, p)
	if err != nil {
		t.Fatal(err)
	}
	if good.Cmp(bad) <= 0 {
		t.Fatalf("better isolated quality did not invert equal deposits: bad=%s good=%s", bad, good)
	}
	// Conviction tier is an exact rate discount.
	discounted, err := impliedUsageQuality(deposit, big.NewInt(1_000_000_000), 980_000, p)
	if err != nil {
		t.Fatal(err)
	}
	if discounted.Cmp(good) <= 0 {
		t.Fatalf("lower tier rate did not increase implied usage: base=%s discounted=%s", good, discounted)
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
