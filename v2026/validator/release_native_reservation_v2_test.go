//go:build linux || darwin

package validator

import (
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseRuntimeV2CancelsExpiredUnsignedNativeReservation(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	runtime := fixture.runtime
	participant := runtime.history.participants[0]
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(8), BlockNumber: 1501, BlockHash: fixture.startup.blocks[1501]}
	setReservation := func(active uint64) {
		t.Helper()
		owner, err := participant.Stats.acquireStatsWrite(t.Context(), "test-pending-native-drain")
		if err != nil {
			t.Fatal(err)
		}
		defer owner.release()
		candidate := owner.clone()
		candidate.attemptCutPending = true
		candidate.activeAttemptCount = active
		owner.publish(candidate)
	}
	cancel := func() error {
		t.Helper()
		release, err := runtime.acquire(t.Context())
		if err != nil {
			return err
		}
		defer release()
		return runtime.cancelExpiredNativeReservationOwned(t.Context(), nil, 2, snapshot)
	}
	setReservation(0)
	// A native drain has completed, but repeated real settlement advancement
	// still cannot acquire that native reservation. No external work owns it.
	for range 2 {
		if err := runtime.advance(t.Context(), snapshot); !errors.Is(err, errAttemptCutPending) {
			t.Fatalf("drained reservation did not reproduce advancement cycle: %v", err)
		}
	}
	if err := cancel(); err != nil || !participant.Stats.attemptCutPending {
		t.Fatalf("ordinary mode changed the reservation: %v", err)
	}
	runtime.cfg.ProvisionalDeferClosedNativeInput = true
	if err := cancel(); err == nil || !participant.Stats.attemptCutPending {
		t.Fatal("unowned native reservation was cancelled")
	}
	runtime.nativeReservations = map[uint64]uint64{participant.NoID: 1}
	setReservation(1)
	if err := cancel(); !errors.Is(err, errAttemptCutPending) || !participant.Stats.attemptCutPending {
		t.Fatalf("active attempt lost its reservation: %v", err)
	}
	setReservation(0)
	// The retry is in native epoch 2. Even an unreconciled epoch-1 journal
	// whose bytes cannot yet be authenticated must prevent cancellation.
	path := releaseMeasurementInputV2Path(runtime.cfg.StateDir, 1, participant.NoID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("retained journal requiring reconciliation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cancel(); err == nil || !participant.Stats.attemptCutPending {
		t.Fatal("earlier native journal lost reservation custody")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := cancel(); err != nil || participant.Stats.attemptCutPending {
		t.Fatalf("drained unsigned reservation did not release: %v", err)
	}
	if _, owned := runtime.nativeReservations[participant.NoID]; owned {
		t.Fatal("cancelled reservation retained an epoch owner")
	}
	if err := runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatalf("real terminal advancement remained blocked: %v", err)
	}
	if runtime.history.terminals[7] == nil || runtime.history.current[participant.NoID].epoch != 8 {
		t.Fatal("released reservation did not complete the real terminal closure")
	}
}
