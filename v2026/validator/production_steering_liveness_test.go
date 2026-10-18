// The actual production loop publishes outcomes only after its owned call
// returns. A responsive file publisher cannot substitute for that boundary.
package validator

import (
	"context"
	"testing"
	"time"
)

// Channels force a completed first attempt, a wedged next call, continued
// publisher snapshots, and a later returned read wait without scheduler races.
func TestProductionSteeringProgressStaysAtOwnerBoundaryDuringBlockedSubmit(t *testing.T) {
	progress, clock := newReleaseProgressTest(t)
	initial := clock.now()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	attempts := 0
	go func() {
		done <- runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			attempts++
			if attempts == 1 {
				return ErrSteeringAlreadyFinal
			}
			close(entered)
			select {
			case <-release:
				return &productionSteeringReadWait{phase: productionReadReceipt, cause: context.DeadlineExceeded}
			case <-t.Context().Done():
				return t.Context().Err()
			}
		}, func() bool { return attempts == 1 }, progress)
	}()
	<-entered
	clock.set(initial.Add(10 * time.Minute))
	progress.observePublication(true)
	stalled := releaseProgressTestSnapshot(t, progress)
	// Join before assertions so even a causal failure releases the owned loop.
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if stalled.HeartbeatAt != clock.now().Format(time.RFC3339Nano) || stalled.Publisher.LastSuccessAt != stalled.HeartbeatAt ||
		stalled.Steering == nil || stalled.Steering.ObservedAt != initial.Format(time.RFC3339Nano) || stalled.Steering.Outcome != "complete" {
		t.Fatal("independent publisher refreshed the blocked steering owner", stalled)
	}
	recovered := releaseProgressTestSnapshot(t, progress)
	if recovered.Steering.ObservedAt != recovered.HeartbeatAt || recovered.Steering.Current || recovered.Steering.Outcome != "receipt_transport_wait" || recovered.Steering.LastSuccessAt != initial.Format(time.RFC3339Nano) {
		t.Fatal("returned retry did not retain responsiveness separately from success", recovered)
	}
}
