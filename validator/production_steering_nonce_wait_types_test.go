//go:build linux || darwin

// The nonce wait and the held epoch survive the single error observation as
// their own markers, are waits for the steering loop, and are no crash at a
// requested stop; a hard cause joined to either still counts.
package validator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/urfoundation/sn/crv4"
)

func productionNonceTestWaits() []error {
	refusal := &crv4.SubmissionUnresolvedError{Outcome: crv4.SubmissionNonceTaken, Code: 1010, Message: "Invalid Transaction", Data: "Transaction is outdated"}
	return []error{
		&productionPendingNonceWait{nativeEpoch: 26216, extrinsicHash: "0x01", prepared: 41, cause: refusal},
		&productionPendingNonceWait{nativeEpoch: 26216, extrinsicHash: "0x01", prepared: 41, gap: true, finalized: 40},
		&productionSteeringEpochHold{nativeEpoch: 26216, prepared: 9461000, written: 9461002},
	}
}

// Observation freezes each marker as itself, with its cause, so the loop's
// marker search finds it.
func TestProductionNonceWaitMarkersSurviveObservation(t *testing.T) {
	for _, wait := range productionNonceTestWaits() {
		observed := observeReleaseError(wait)
		switch wait.(type) {
		case *productionPendingNonceWait:
			marker := releaseErrorMarker[*productionPendingNonceWait](observed)
			if marker == nil || !releaseOnlyErrors(observed, marker) || marker.Error() != wait.Error() {
				t.Fatalf("observation lost the nonce wait: %v", observed)
			}
		case *productionSteeringEpochHold:
			marker := releaseErrorMarker[*productionSteeringEpochHold](observed)
			if marker == nil || !releaseOnlyErrors(observed, marker) || marker.Error() != wait.Error() {
				t.Fatalf("observation lost the held epoch: %v", observed)
			}
		}
	}
}

// Each is a wait: more of them in a row than the failure limit end nothing.
// A held epoch also restores the budget, like a completed epoch; a nonce wait
// keeps the hard failures before it.
func TestProductionNonceWaitsSpendNoFailures(t *testing.T) {
	hard := errors.New("synthetic hard cause")
	for _, wait := range productionNonceTestWaits() {
		polls := 0
		err := runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
			polls++
			if polls <= releaseSteeringFailureLimit-1 {
				return hard
			}
			if polls <= releaseSteeringFailureLimit+20 {
				return wait
			}
			return hard
		}, func() bool { return polls < releaseSteeringFailureLimit+21 }, nil, time.Now, releaseProductionCutWaitMinimumLimit)
		var limit *releaseSteeringFailureLimitError
		if _, held := wait.(*productionSteeringEpochHold); held {
			if errors.As(err, &limit) || !errors.Is(err, hard) || polls != releaseSteeringFailureLimit+21 {
				t.Fatalf("a held epoch did not restore the budget: polls=%d err=%v", polls, err)
			}
			continue
		}
		if !errors.As(err, &limit) || polls != releaseSteeringFailureLimit+21 {
			t.Fatalf("a nonce wait restored or spent the budget: polls=%d err=%v", polls, err)
		}
	}
}

// A requested stop during either wait exits cleanly; a hard cause joined to
// it still reaches the process verdict.
func TestReleaseStopDuringNonceWaitOrEpochHoldIsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, wait := range productionNonceTestWaits() {
		if err := releaseProcessError(ctx, errors.Join(wait, context.Canceled)); err != nil {
			t.Fatalf("a stop during %T read as a crash: %v", wait, err)
		}
		hard := errors.New("independent hard cause")
		if err := releaseProcessError(ctx, errors.Join(wait, hard, context.Canceled)); !errors.Is(err, hard) {
			t.Fatalf("a hard cause beside %T was hidden by the stop: %v", wait, err)
		}
	}
}

// Both submission owners, the pending rebroadcast and a fresh intent's first
// send, answer through one classifier over the real crv4 submit path: a pool
// answer that leaves the bytes to finality is a nonce wait carrying it, a
// genuinely hard refusal keeps its error, and a lost acknowledgement keeps its
// transport wait.
func TestProductionPendingSubmissionFailureClassifiesPoolAnswers(t *testing.T) {
	fixture, native, pending := newProductionNonceWaitFixture(t, 0)
	submit := func(answer error) error {
		t.Helper()
		native.submitAnswer = func(int) error { return answer }
		_, err := crv4.SubmitPrepared(t.Context(), fixture.steerer.native, pending.Prepared)
		return fixture.steerer.productionPendingSubmissionFailure(pending, pending.SubnetEpoch, err)
	}
	for _, refusal := range productionPoolTestNonceTaken(t) {
		var wait *productionPendingNonceWait
		var unresolved *crv4.SubmissionUnresolvedError
		if err := submit(refusal); !errors.As(err, &wait) || wait.gap || wait.nativeEpoch != pending.SubnetEpoch || wait.prepared != pending.Prepared.AccountNonce || !errors.As(wait.cause, &unresolved) {
			t.Fatalf("pool answer %v was not a nonce wait: %v", refusal, err)
		}
	}
	payment := productionPoolTestRefusal(t, map[string]any{"code": 1010, "message": "Invalid Transaction", "data": "Inability to pay some fees (e.g. account balance too low)"})
	err := submit(payment)
	var wait *productionPendingNonceWait
	var transport *productionSteeringReadWait
	if err == nil || errors.As(err, &wait) || errors.As(err, &transport) || retryableProductionSteeringRead(err) {
		t.Fatalf("a genuinely hard refusal became a wait: %v", err)
	}
	if err := submit(context.DeadlineExceeded); !errors.As(err, &transport) || transport.phase != productionReadReceipt {
		t.Fatalf("a lost acknowledgement lost its transport wait: %v", err)
	}
}
