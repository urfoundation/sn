package crv4

import (
	"math/big"
	"testing"
)

func rats(values ...int64) []*big.Rat {
	out := make([]*big.Rat, len(values))
	for i, value := range values {
		out[i] = big.NewRat(value, 1)
	}
	return out
}

func TestNormalizeRationalToU16Exact(t *testing.T) {
	uids, values, err := NormalizeRationalToU16([]uint16{3, 7, 9, 11}, rats(0, 1, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	wantUIDs := []uint16{7, 9, 11}
	wantValues := []uint16{21845, 43690, 65535}
	for i := range wantUIDs {
		if uids[i] != wantUIDs[i] || values[i] != wantValues[i] {
			t.Fatalf("got (%v,%v), want (%v,%v)", uids, values, wantUIDs, wantValues)
		}
	}
}

func TestApplyMaxWeightLimitRationalExact(t *testing.T) {
	// Limit 40%: [1,1,8] is clipped to [1,1,4/3], since
	// (4/3)/(1+1+4/3) = 2/5 exactly.
	limit := uint16((uint64(U16Max) * 2) / 5)
	capped, err := ApplyMaxWeightLimitRational(rats(1, 1, 8), limit)
	if err != nil {
		t.Fatal(err)
	}
	// The u16 policy limit is 26214/65535 == 2/5 exactly.
	want := big.NewRat(4, 3)
	if capped[2].Cmp(want) != 0 {
		t.Fatalf("cap = %s, want %s", capped[2], want)
	}
	total := new(big.Rat).Add(new(big.Rat).Add(capped[0], capped[1]), capped[2])
	ratio := new(big.Rat).Quo(capped[2], total)
	if ratio.Cmp(big.NewRat(2, 5)) != 0 {
		t.Fatalf("ratio = %s", ratio)
	}
}

func TestApplyMaxWeightLimitRationalRejectsInfeasible(t *testing.T) {
	if _, err := ApplyMaxWeightLimitRational(rats(1, 2), 100); err == nil {
		t.Fatal("expected infeasible cap rejection")
	}
}
