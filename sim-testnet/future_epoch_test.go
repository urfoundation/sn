package main

import (
	"math"
	"testing"
)

// Reproduce the live epoch-8/9 failure at the exact public-testnet blocks.
func TestFutureEpochWindowRejectsTheObservedBindingBoundaryRace(t *testing.T) {
	window, ready, err := selectFutureEpochTransactionWindow(7_898_073, 8, 7_897_774, 7_898_074)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("last-block binding window was accepted")
	}
	if window.EffectiveEpoch != 9 {
		t.Fatalf("effective epoch = %d, want 9", window.EffectiveEpoch)
	}
}

// Prove that the same action becomes admissible after the boundary and targets
// the following epoch, rather than reusing the now-active epoch 9 payload.
func TestFutureEpochWindowReopensWithFreshEffectiveEpoch(t *testing.T) {
	window, ready, err := selectFutureEpochTransactionWindow(7_898_074, 9, 7_898_074, 7_898_374)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("fresh epoch window was not ready")
	}
	if window.EffectiveEpoch != 10 {
		t.Fatalf("effective epoch = %d, want 10", window.EffectiveEpoch)
	}
}

// Cover the adjacent threshold so the configured safety interval cannot be
// accidentally shortened by an inclusive/exclusive comparison change.
func TestFutureEpochWindowRequiresMoreThanTheSafetyInterval(t *testing.T) {
	end := uint64(10_000)
	head := end - futureEpochInclusionSafetyBlocks
	_, ready, err := selectFutureEpochTransactionWindow(head, 4, 9_700, end)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("exact safety-boundary window was accepted")
	}

	_, ready, err = selectFutureEpochTransactionWindow(head-1, 4, 9_700, end)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("window beyond the safety boundary was rejected")
	}
}

// Malformed RPC coordinates must fail closed instead of causing an endless
// boundary wait or unsigned arithmetic wraparound.
func TestFutureEpochWindowRejectsInconsistentCoordinates(t *testing.T) {
	if _, _, err := selectFutureEpochTransactionWindow(100, 1, 101, 200); err == nil {
		t.Fatal("epoch starting after the head was accepted")
	}
	if _, _, err := selectFutureEpochTransactionWindow(200, 1, 100, 200); err == nil {
		t.Fatal("head at the exclusive epoch end was accepted")
	}
	if _, _, err := selectFutureEpochTransactionWindow(100, 1, 100, 100+futureEpochInclusionSafetyBlocks); err == nil {
		t.Fatal("epoch shorter than the inclusion interval was accepted")
	}
}

// Future-effective policy, operator and binding actions all share this
// overflow boundary; none may silently wrap to epoch zero.
func TestFutureEpochWindowRejectsEffectiveEpochOverflow(t *testing.T) {
	if _, _, err := selectFutureEpochTransactionWindow(100, math.MaxUint64, 100, 400); err == nil {
		t.Fatal("effective epoch overflow was accepted")
	}
}

// Preserve the exact maximum-validity interpretation used by the coordinator.
func TestFleetBindingValidityUsesThePolicyLengthExactly(t *testing.T) {
	validTo, err := fleetBindingValidityEnd(10, 32)
	if err != nil {
		t.Fatal(err)
	}
	if validTo != 41 {
		t.Fatalf("valid-to epoch = %d, want 41", validTo)
	}
}

// Zero validity and addition overflow are adjacent policy/configuration faults,
// and both must fail before signatures or transactions are created.
func TestFleetBindingValidityRejectsInvalidPolicyArithmetic(t *testing.T) {
	if _, err := fleetBindingValidityEnd(10, 0); err == nil {
		t.Fatal("zero validity was accepted")
	}
	if _, err := fleetBindingValidityEnd(math.MaxUint64-1, 3); err == nil {
		t.Fatal("validity overflow was accepted")
	}
}

// Only the pre-broadcast race is recoverable. A same-epoch revert may describe
// a signature or identity fault, while a persisted transaction must be audited.
func TestFleetBindingRetryIsRestrictedToUnbroadcastEpochTransitions(t *testing.T) {
	if retryFleetBindingAfterEpochTransition(8, 9, false) {
		t.Fatal("same-epoch error was made retryable")
	}
	if !retryFleetBindingAfterEpochTransition(9, 9, false) {
		t.Fatal("unbroadcast epoch transition was not retryable")
	}
	if retryFleetBindingAfterEpochTransition(9, 9, true) {
		t.Fatal("persisted transaction was made retryable")
	}
}
