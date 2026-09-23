// Outstanding, receipt-accounted credits are a durable pending state during a
// provisional interval, with zero signer access and unchanged final rejection.
package main

import (
	"context"
	"errors"
	"os"
	"testing"
)

// The real continuation entry point returns before calling Execute. Missing
// journal and signer owners intentionally make accidental dispatch impossible.
func TestPrecompileRecoveryPendingDefersTransferBeforeIntentOrSend(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	evidence, _, _ := precompileProbeSuccessorCallFixture(t, fixture)
	evidence.Back.FromBeforeRao += 17
	evidence.Back.FromAfterRao += 17
	if err := recordPrecompileRoundTripCredits(evidence); err != nil {
		t.Fatal(err)
	}
	if err := writePrecompileEvidence(fixture.stateDir, evidence); err != nil {
		t.Fatal(err)
	}
	owner := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: fixture.plan, payloads: fixture.payloads}
	action := actionByID(t, fixture.plan, "precompile.transfer-out")
	before, err := loadPrecompileEvidence(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		err := owner.executePrecompileContinuationAction(t.Context(), action)
		if !errors.Is(err, errPrecompileRecoveryPending) || !precompileContinuationMayWait(err) {
			t.Fatalf("pending recovery reached dispatch or became terminal: %v", err)
		}
	}
	after, err := loadPrecompileEvidence(fixture.stateDir)
	if err != nil || after.EvidenceHash != before.EvidenceHash || after.Transfer != before.Transfer || precompileEvidenceComplete(after) {
		t.Fatalf("pending recovery changed evidence or passed final acceptance: %v", err)
	}
}

// Ordinary observations continue while this exact liability waits for its own
// approved recovery, without converting integrity or filesystem errors to soft.
func TestPrecompileRecoveryPendingPreservesObservationAndHardErrors(t *testing.T) {
	evidence := completePrecompileEvidence()
	evidence.Back.FromBeforeRao += 17
	evidence.Back.FromAfterRao += 17
	if err := recordPrecompileRoundTripCredits(evidence); err != nil {
		t.Fatal(err)
	}
	pending := precompileTransferReadiness(evidence)
	want := &ScenarioObservation{}
	observed := 0
	got, err := precompileContinuationSnapshot(t.Context(), func(context.Context) error { return pending }, func(context.Context) (*ScenarioObservation, error) {
		observed++
		return want, nil
	})
	if err != nil || got != want || observed != 1 {
		t.Fatalf("pending recovery blocked ordinary observation: %v", err)
	}
	for _, hard := range []error{errors.New("changed receipt"), &os.PathError{Op: "read", Path: "synthetic-evidence.json", Err: os.ErrNotExist}} {
		if precompileContinuationMayWait(errors.Join(pending, hard)) {
			t.Fatalf("pending recovery masked hard error: %v", hard)
		}
	}
	evidence.RoundTripCredits.UnrecoveredMoveRao--
	if err := precompileTransferReadiness(evidence); err == nil || errors.Is(err, errPrecompileRecoveryPending) || precompileContinuationMayWait(err) {
		t.Fatalf("tampered recovery liability became pending: %v", err)
	}
}

// A round trip with no remaining move custody retains its prior transfer path.
func TestPrecompileRecoveryPendingLeavesClosedRoundTripsReady(t *testing.T) {
	if err := precompileTransferReadiness(completePrecompileEvidence()); err != nil {
		t.Fatalf("closed round trip was deferred: %v", err)
	}
}
