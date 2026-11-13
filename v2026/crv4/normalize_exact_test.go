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

func TestApplyMaxWeightLimitRationalAllowsExactFeasibility(t *testing.T) {
	limit := uint16(U16Max / 3)
	capped, err := ApplyMaxWeightLimitRational(rats(1, 2, 8), limit)
	if err != nil {
		t.Fatal(err)
	}
	for index, weight := range capped {
		if weight.Cmp(big.NewRat(1, 1)) != 0 {
			t.Fatalf("weight %d = %s, want 1", index, weight)
		}
	}
}

func TestRepairMaxWeightLimitU16AfterRounding(t *testing.T) {
	limit := uint16((uint64(U16Max) * 2) / 5)
	capped, err := ApplyMaxWeightLimitRational(rats(1, 1, 8), limit)
	if err != nil {
		t.Fatal(err)
	}
	uids, values, err := NormalizeRationalToU16([]uint16{2, 1, 3}, capped)
	if err != nil {
		t.Fatal(err)
	}
	before := uint64(values[2]) * uint64(U16Max)
	beforeSum := uint64(values[0]) + uint64(values[1]) + uint64(values[2])
	beforeLimit := beforeSum * uint64(limit)
	if before <= beforeLimit {
		t.Fatalf("fixture did not reproduce rounding violation: %v", values)
	}
	if err := repairMaxWeightLimitU16(uids, values, limit); err != nil {
		t.Fatal(err)
	}
	if values[0] != 49151 || values[1] != 49152 {
		t.Fatalf("rounding unit was not assigned by ascending UID: uids=%v values=%v", uids, values)
	}
	var sum uint64
	var maximum uint16
	for _, value := range values {
		sum += uint64(value)
		if value > maximum {
			maximum = value
		}
	}
	if uint64(maximum)*uint64(U16Max) > sum*uint64(limit) {
		t.Fatalf("repaired vector still violates cap: %v", values)
	}
}

func TestRepairMaxWeightLimitU16RejectsInfeasibleEmittedBreadth(t *testing.T) {
	if err := repairMaxWeightLimitU16([]uint16{1, 2}, []uint16{U16Max, U16Max}, 32767); err == nil {
		t.Fatal("infeasible emitted vector was accepted")
	}
}

func TestRepairMaxWeightLimitU16RejectsMismatchedVectors(t *testing.T) {
	if err := repairMaxWeightLimitU16([]uint16{1}, []uint16{U16Max, U16Max}, 32768); err == nil {
		t.Fatal("mismatched UID/value vectors were accepted")
	}
}
