// Historical monitor policy retains its original timing interpretation;
// explicit new profiles are bounded and cannot be inferred from unknown text.
package main

import (
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The same input keeps its legacy forecast unless an exact profile is chosen.
// Unknown choices fail policy validation and cannot produce a known boundary.
func TestMonitorTempoDriftProfileRetainsLegacyAndRejectsUnknown(t *testing.T) {
	native := protocol.ValidatorNativeObservation{Block: 180, LastEpochBlock: 180, Tempo: 100, BlocksSinceLastStep: 100}
	if block, known := monitorNativeEpochBoundary(native); !known || block != 280 {
		t.Fatalf("legacy monitor forecast changed: %d %t", block, known)
	}
	for _, count := range []uint64{99, 100, 101} {
		native.BlocksSinceLastStep = count
		want := uint64(181)
		if count == 99 {
			want = 182
		}
		if block, known := monitorNativeEpochBoundaryWithProfile(native, crv4.TempoDriftEpochScheduleProfile); !known || block != want {
			t.Fatalf("tempo drift count=%d forecast=%d known=%t", count, block, known)
		}
	}
	native = protocol.ValidatorNativeObservation{Block: 70000, LastEpochBlock: 70000, Tempo: 65535, BlocksSinceLastStep: 65535}
	if block, known := monitorNativeEpochBoundaryWithProfile(native, crv4.TempoDriftEpochScheduleProfile); !known || block != 70001 {
		t.Fatalf("reviewed root tempo was confused with the owner cap: %d %t", block, known)
	}
	policy := monitorNativeDeadlinePolicy{CompletionMarginBlocks: 5, EpochScheduleProfile: "unknown"}
	if policy.validate() == nil {
		t.Fatal("unknown monitor scheduler profile passed policy validation")
	}
	if _, known := monitorNativeEpochBoundaryWithProfile(native, "unknown"); known {
		t.Fatal("unknown monitor scheduler profile produced a boundary")
	}
}
