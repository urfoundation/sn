// Fixed-point controls compare conservative bounds with independently encoded
// I64F64/I32F32 arithmetic, including unknown fractional stake and both masks.
package main

import (
	"math"
	"math/big"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Enumerated fractions exercise zero, half and the largest representable
// fraction. The reference retains 128-bit fixed values and performs both actual
// normalization truncations, independently of the production interval formula.
func TestValidatorStakeBoundsNeverExceedExactFixedPointShares(t *testing.T) {
	for _, floors := range [][4]uint64{{100, 99, 0, 0}, {8000, 1000, 500, 20}, {1, 1, 1, 1}, {9000, 1000, 1_000_000_000_000, 0}} {
		entries := make([]crv4.ValidatorStakeCensusEntry, 4)
		for i, floor := range floors {
			entries[i] = crv4.ValidatorStakeCensusEntry{TotalStakeFloorRao: floor, ValidatorPermit: i < 2}
		}
		owner := uint16(3)
		for _, activity := range [][4]bool{{true, true, true, true}, {true, false, false, true}, {false, true, true, true}} {
			bounds, _, _, err := validatorActivationStakeBounds(entries, 1, &owner, activity[:], [2]uint16{0, 1})
			if err != nil {
				t.Fatal(err)
			}
			for combination := 0; combination < 81; combination++ {
				remaining := combination
				stakes, sum := make([]*big.Int, 4), new(big.Int)
				for i, floor := range floors {
					stakes[i] = new(big.Int).Lsh(new(big.Int).SetUint64(floor), 64)
					fraction := remaining % 3
					remaining /= 3
					if fraction == 1 {
						stakes[i].Add(stakes[i], new(big.Int).Lsh(big.NewInt(1), 63))
					}
					if fraction == 2 {
						stakes[i].Add(stakes[i], new(big.Int).SetUint64(math.MaxUint64))
					}
					if floor < 1 && i != int(owner) {
						stakes[i].SetInt64(0)
					}
					sum.Add(sum, stakes[i])
				}
				first := make([]*big.Int, 4)
				capacitySum, activeSum := new(big.Int), new(big.Int)
				for i, stake := range stakes {
					first[i] = new(big.Int).Lsh(new(big.Int).Set(stake), 64)
					first[i].Quo(first[i], sum)
					first[i].Rsh(first[i], 32)
					if entries[i].ValidatorPermit || i == int(owner) {
						capacitySum.Add(capacitySum, first[i])
						if activity[i] {
							activeSum.Add(activeSum, first[i])
						}
					}
				}
				for role := 0; role < 2; role++ {
					exact := [3]uint64{first[role].Uint64(), 0, 0}
					if capacitySum.Sign() != 0 {
						exact[1] = new(big.Int).Quo(new(big.Int).Lsh(new(big.Int).Set(first[role]), 32), capacitySum).Uint64()
					}
					if activity[role] && activeSum.Sign() != 0 {
						exact[2] = new(big.Int).Quo(new(big.Int).Lsh(new(big.Int).Set(first[role]), 32), activeSum).Uint64()
					}
					for stage := range exact {
						if bounds[role][stage] > exact[stage] {
							t.Fatal("interval bound overstated exact runtime arithmetic", floors, activity, combination, role, stage, bounds[role], exact)
						}
					}
				}
			}
		}
	}
}

// Rounded integer majority and a large ineligible peer are distinct hazards:
// the first loses fractional margin, the second erases stake before masking.
func TestValidatorStakeBoundsRefuseFloorMajorityAndQuantizedZero(t *testing.T) {
	for _, floors := range [][3]uint64{{100, 99, 0}, {8000, 1, uint64(1) << 62}} {
		entries := []crv4.ValidatorStakeCensusEntry{{TotalStakeFloorRao: floors[0], ValidatorPermit: true}, {TotalStakeFloorRao: floors[1], ValidatorPermit: true}, {TotalStakeFloorRao: floors[2]}}
		bounds, _, _, err := validatorActivationStakeBounds(entries, 1, nil, []bool{true, true, false}, [2]uint16{0, 1})
		if err != nil || bounds[0][1] > validatorStakeRequiredShare(32768) {
			t.Fatal("floor ratio or pre-mask quantization claimed majority", floors, bounds, err)
		}
		if floors[2] != 0 && (bounds[0][0] != 0 || bounds[1][0] != 0) {
			t.Fatal("non-permitted peer disappeared before first normalization", bounds)
		}
	}
}

// Owner eligibility changes both threshold filtering and permission. Unknown
// fractions may not overflow a signed fixed-point runtime sum or be ignored.
func TestValidatorStakeBoundsPreserveOwnerExceptionAndRange(t *testing.T) {
	entries := []crv4.ValidatorStakeCensusEntry{{TotalStakeFloorRao: 8000, ValidatorPermit: true}, {TotalStakeFloorRao: 1000, ValidatorPermit: true}, {TotalStakeFloorRao: 99}}
	owner := uint16(2)
	without, lower, _, err := validatorActivationStakeBounds(entries, 100, nil, []bool{true, true, true}, [2]uint16{0, 1})
	if err != nil || lower != 9000 {
		t.Fatal(without, lower, err)
	}
	with, lower, _, err := validatorActivationStakeBounds(entries, 100, &owner, []bool{true, true, true}, [2]uint16{0, 1})
	if err != nil || lower != 9099 || with[0][1] >= without[0][1] {
		t.Fatal("registered owner disappeared from consensus stake", with, lower, err)
	}
	for _, value := range []uint64{math.MaxInt64, math.MaxUint64} {
		entries[2].TotalStakeFloorRao = value
		if _, _, _, err := validatorActivationStakeBounds(entries, 100, nil, []bool{true, true, true}, [2]uint16{0, 1}); err == nil {
			t.Fatal("unbounded fixed-point sum admitted", value)
		}
	}
	if validatorStakeRequiredShare(0) != validatorStakeQ32One || validatorStakeRequiredShare(math.MaxUint16) != validatorStakeQ32One || validatorStakeRequiredShare(10000) <= validatorStakeQ32One/2 {
		t.Fatal("noncentral kappa collapsed to a simple half")
	}
}
