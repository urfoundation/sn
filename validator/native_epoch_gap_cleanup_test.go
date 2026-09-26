//go:build linux || darwin

// A known steering gap must preserve cleanup needed by independent settlement
// publication, while refusing to produce a new signed native input.
package validator

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"reflect"
	"testing"
)

// Explicit Stats ownership creates a drained unsigned reservation in the prior
// settlement. No sleeps or active worker timing determine whether it is released.
func TestReleaseRuntimeNativeEpochGapReleasesExpiredUnsignedReservation(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	runtime := fixture.runtime
	if runtime.history.retainedStartup {
		t.Fatal("fixture unexpectedly grants retained-history gap authority")
	}
	runtime.cfg.ProvisionalDeferClosedNativeInput = true
	participant := runtime.history.participants[0]
	owner, err := participant.Stats.acquireStatsWrite(t.Context(), "synthetic-native-gap-reservation")
	if err != nil {
		t.Fatal(err)
	}
	candidate := owner.clone()
	candidate.attemptCutPending = true
	candidate.activeAttemptCount = 0
	owner.publish(candidate)
	owner.release()
	runtime.nativeReservations = map[uint64]uint64{participant.NoID: 41}
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(8), BlockNumber: 1501, BlockHash: fixture.startup.blocks[1501]}
	previous := &SteeringIntent{SubnetEpoch: 40, SettlementEpoch: 7, Status: "applied"}
	native := fixture.startup.nativeFixture
	inputs, options, err := runtime.collect(t.Context(), steerer, previous, snapshot, 42, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2})
	var gap *nativeEpochGapError
	if !errors.As(err, &gap) || gap.previousEpoch != 40 || gap.currentEpoch != 42 || inputs != nil || !reflect.DeepEqual(options, ReleaseMeasurementV2Options{}) {
		t.Fatalf("cleanup hid the native gap or admitted new input: %v", err)
	}
	if participant.Stats.attemptCutPending || len(runtime.nativeReservations) != 0 {
		t.Fatal("history gap stranded the drained unsigned reservation")
	}
	for _, participant := range runtime.history.participants {
		path := releaseMeasurementInputV2Path(runtime.cfg.StateDir, 42, participant.NoID)
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("gap published another native input: %v", err)
		}
	}
	if err := runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatalf("history gap blocked independent terminal closure: %v", err)
	}
	if runtime.history.terminals[7] == nil || runtime.history.current[participant.NoID].epoch != 8 {
		t.Fatal("gap cleanup did not permit the real terminal closure")
	}
}

// The original immutable native cut can outlive a failed Stats write. A later
// steering gap must finish that custody path without signing a replacement cut.
func TestReleaseRuntimeNativeEpochGapReconcilesPublishedInput(t *testing.T) {
	fixture, steerer, snapshot, before := newReleaseRuntimeNativeRecoveryTestFixture(t)
	runtime := fixture.runtime
	participant := runtime.history.participants[0]
	posts := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
	previous := &SteeringIntent{SubnetEpoch: 0, SettlementEpoch: snapshot.Epoch.Uint64(), Status: "applied"}
	native := fixture.startup.nativeFixture
	inputs, options, err := runtime.collect(t.Context(), steerer, previous, snapshot, 3, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2})
	var gap *nativeEpochGapError
	if !errors.As(err, &gap) || gap.previousEpoch != 0 || gap.currentEpoch != 3 || inputs != nil || !reflect.DeepEqual(options, ReleaseMeasurementV2Options{}) {
		t.Fatalf("signed input recovery hid the gap or created new input: %v", err)
	}
	if runtime.history.inputByEpoch[1][participant.NoID] == nil || len(runtime.nativeInputNoIdKVs) != 0 {
		t.Fatal("history gap bypassed the original signed input reconciliation")
	}
	after, err := readReleaseMeasurementInputV2(releaseMeasurementInputV2Path(runtime.cfg.StateDir, 1, participant.NoID), runtime.cfg.EvidenceV2.Bounds.MaxInputJournalBytes)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("gap recovery replaced the original signed input: %v", err)
	}
	for _, participant := range runtime.history.participants {
		if _, err := os.Lstat(releaseMeasurementInputV2Path(runtime.cfg.StateDir, 3, participant.NoID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("gap recovery signed a new input: %v", err)
		}
	}
	for index, store := range fixture.stores {
		if !reflect.DeepEqual(posts[index], store.counts()) {
			t.Fatal("gap recovery republished the original cut")
		}
	}
}
