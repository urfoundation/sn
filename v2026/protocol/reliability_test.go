package protocol

import "testing"

func TestReliabilityPPMExactVectors(t *testing.T) {
	tests := []struct {
		confirmations, assignments, aMin uint64
		want                             uint32
	}{
		{0, 0, 8, 0},
		{8, 8, 8, 675584},
		{5, 10, 8, 236589},
		{1, 1, 8, 22416},
		{100, 100, 8, 963005},
	}
	for _, tc := range tests {
		got := ReliabilityPPM(tc.confirmations, tc.assignments, tc.aMin)
		if got != tc.want {
			t.Fatalf("ReliabilityPPM(%d,%d,%d)=%d want %d", tc.confirmations, tc.assignments, tc.aMin, got, tc.want)
		}
	}
}

func TestReliabilityPPMMonotoneAndDefensive(t *testing.T) {
	prior := uint32(0)
	for confirmations := uint64(0); confirmations <= 100; confirmations++ {
		got := ReliabilityPPM(confirmations, 100, 8)
		if got < prior {
			t.Fatalf("not monotone at %d: %d < %d", confirmations, got, prior)
		}
		prior = got
	}
	if got := ReliabilityPPM(20, 10, 8); got != ReliabilityPPM(10, 10, 8) {
		t.Fatalf("confirmations above assignments were not capped: %d", got)
	}
}
