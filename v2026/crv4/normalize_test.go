package crv4

import (
	"math"
	"reflect"
	"testing"
)

func TestNormalizeToU16(t *testing.T) {
	cases := []struct {
		name     string
		uids     []uint16
		weights  []float64
		wantUids []uint16
		wantVals []uint16
	}{
		{
			// Max-upscale: max weight -> exactly 65535.
			name:     "basic",
			uids:     []uint16{1, 2, 3},
			weights:  []float64{1.0, 0.5, 0.25},
			wantUids: []uint16{1, 2, 3},
			wantVals: []uint16{65535, 32768, 16384},
		},
		{
			// Scale-invariant: same ratios, different magnitudes.
			name:     "scale_invariant",
			uids:     []uint16{1, 2, 3},
			weights:  []float64{400, 200, 100},
			wantUids: []uint16{1, 2, 3},
			wantVals: []uint16{65535, 32768, 16384},
		},
		{
			// Zeros are filtered out (uid dropped), like the SDK.
			name:     "zero_filtered",
			uids:     []uint16{0, 5, 9},
			weights:  []float64{0, 1, 0},
			wantUids: []uint16{5},
			wantVals: []uint16{65535},
		},
		{
			// Tiny weights that round to 0 are filtered.
			name:     "rounds_to_zero_filtered",
			uids:     []uint16{1, 2},
			weights:  []float64{1.0, 1e-9},
			wantUids: []uint16{1},
			wantVals: []uint16{65535},
		},
		{
			// All-zero -> empty vectors (nothing to set on chain).
			name:     "all_zero",
			uids:     []uint16{1, 2},
			weights:  []float64{0, 0},
			wantUids: []uint16{},
			wantVals: []uint16{},
		},
		{
			name:     "single",
			uids:     []uint16{200},
			weights:  []float64{0.123},
			wantUids: []uint16{200},
			wantVals: []uint16{65535},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotUids, gotVals, err := NormalizeToU16(tc.uids, tc.weights)
			if err != nil {
				t.Fatalf("NormalizeToU16: %v", err)
			}
			if !reflect.DeepEqual(gotUids, tc.wantUids) || !reflect.DeepEqual(gotVals, tc.wantVals) {
				t.Errorf("got (%v, %v), want (%v, %v)", gotUids, gotVals, tc.wantUids, tc.wantVals)
			}
		})
	}
}

func TestNormalizeToU16Errors(t *testing.T) {
	if _, _, err := NormalizeToU16([]uint16{1}, []float64{-0.1}); err == nil {
		t.Error("negative weight accepted")
	}
	if _, _, err := NormalizeToU16([]uint16{1, 2}, []float64{1}); err == nil {
		t.Error("length mismatch accepted")
	}
	if _, _, err := NormalizeToU16([]uint16{1}, []float64{math.NaN()}); err == nil {
		t.Error("NaN accepted")
	}
	if _, _, err := NormalizeToU16([]uint16{1}, []float64{math.Inf(1)}); err == nil {
		t.Error("Inf accepted")
	}
}

// chainMaxLimitCheck reproduces subtensor's check_vec_max_limited: after
// sum-normalization, max value must be <= limit/65535.
func chainMaxLimitCheck(weights []float64, limit uint16) bool {
	if limit == U16Max {
		return true
	}
	sum := 0.0
	maxW := 0.0
	for _, w := range weights {
		sum += w
		if w > maxW {
			maxW = w
		}
	}
	if sum == 0 {
		return true
	}
	return maxW/sum <= float64(limit)/U16Max+1e-12
}

func TestApplyMaxWeightLimit(t *testing.T) {
	t.Run("noop_when_unlimited", func(t *testing.T) {
		in := []float64{10, 1, 1}
		out, err := ApplyMaxWeightLimit(in, U16Max)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(out, in) {
			t.Errorf("changed weights under u16::MAX limit: %v", out)
		}
	})

	t.Run("noop_when_already_satisfied", func(t *testing.T) {
		in := []float64{1, 1, 1, 1}
		out, err := ApplyMaxWeightLimit(in, 32768) // limit 0.5, max share 0.25
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(out, in) {
			t.Errorf("changed already-satisfying weights: %v", out)
		}
	})

	t.Run("clips_to_limit", func(t *testing.T) {
		in := []float64{10, 1, 1}
		limit := uint16(32767) // ~0.5
		out, err := ApplyMaxWeightLimit(in, limit)
		if err != nil {
			t.Fatal(err)
		}
		if !chainMaxLimitCheck(out, limit) {
			t.Errorf("clipped weights still violate the chain check: %v", out)
		}
		// Untouched small weights, clipped large one.
		if out[1] != 1 || out[2] != 1 {
			t.Errorf("small weights modified: %v", out)
		}
		if out[0] >= 10 {
			t.Errorf("large weight not clipped: %v", out)
		}
		// The cap should be tight: max/sum ~ limit.
		if got := out[0] / (out[0] + 2); got < float64(limit)/U16Max-1e-6 {
			t.Errorf("clip not tight: max share %v", got)
		}
	})

	t.Run("uniform_when_unsatisfiable", func(t *testing.T) {
		// limit*n <= 1: no clipping can satisfy max/sum <= limit; SDK
		// falls back to uniform.
		in := []float64{5, 3}
		out, err := ApplyMaxWeightLimit(in, 16384) // 0.25 * 2 = 0.5 <= 1
		if err != nil {
			t.Fatal(err)
		}
		if out[0] != 0.5 || out[1] != 0.5 {
			t.Errorf("expected uniform, got %v", out)
		}
	})

	t.Run("zero_limit_rejected", func(t *testing.T) {
		if _, err := ApplyMaxWeightLimit([]float64{1}, 0); err == nil {
			t.Error("zero limit accepted")
		}
	})
}

// TestNormalizePipelineChainValid: the full cap+normalize pipeline yields
// vectors that pass the chain-side max-weight check.
func TestNormalizePipelineChainValid(t *testing.T) {
	uids := []uint16{0, 1, 2, 3, 4}
	scores := []float64{100, 10, 5, 1, 0.5}
	limit := uint16(21845) // ~1/3
	capped, err := ApplyMaxWeightLimit(scores, limit)
	if err != nil {
		t.Fatal(err)
	}
	_, vals, err := NormalizeToU16(uids, capped)
	if err != nil {
		t.Fatal(err)
	}
	asFloat := make([]float64, len(vals))
	for i, v := range vals {
		asFloat[i] = float64(v)
	}
	if !chainMaxLimitCheck(asFloat, limit) {
		t.Errorf("final u16 vector violates chain max-weight check: %v", vals)
	}
}
