//go:build linux || darwin

// Startup orchestration owns lifecycle and readiness. Long authenticated local
// history is not subject to the independent finite remote-read retry clock.
package validator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A concrete retained prefix survives the stage boundary. The caller's long
// deadline is preserved exactly; a new five-minute child would fail before the
// second local phase, causing the old whole-stage retry to discard this work.
func TestProductionStartupStageDoesNotDeadlineLocalHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*productionSteeringReadTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	prefix := filepath.Join(t.TempDir(), "verified-prefix")
	stages := 0
	err := awaitProductionStartupStage(ctx, &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion, PollSeconds: 1}, nil, "local authenticated history", func(stage context.Context) error {
		stages++
		if err := os.WriteFile(prefix, []byte("independently authenticated local prefix\n"), 0600); err != nil {
			return err
		}
		// The first completed physical phase precedes the clock assertion.
		// A finite I/O child must not become this local work's lifecycle.
		if observed, ok := stage.Deadline(); !ok || !observed.Equal(deadline) {
			return errors.New("local authenticated history acquired the remote-read deadline")
		}
		if err := stage.Err(); err != nil {
			return err
		}
		raw, err := os.ReadFile(prefix)
		if err != nil || string(raw) != "independently authenticated local prefix\n" {
			return errors.Join(errors.New("local reconstruction discarded its completed prefix"), err)
		}
		return nil
	})
	if err != nil || stages != 1 {
		t.Fatalf("startup restarted semantic work under an unrelated read budget: stages=%d error=%v", stages, err)
	}
}

// Only resolved original liabilities can request fresh preparation. Neither
// request nor monitor observation closes the current-eligibility/publication gate.
func TestProductionStartupPreparationRequiresResolvedOriginalAndReadyOwner(t *testing.T) {
	owner := &releaseProductionPreparation{requested: make(chan struct{}), ready: make(chan struct{})}
	steerer := &ReleaseSteerer{runtimeV2: &releaseRuntimeV2{preparation: owner}}
	for _, status := range []string{"pending", "finalized"} {
		if err := steerer.requestProductionPreparation(&SteeringIntent{Status: status, SubnetEpoch: 7}); err == nil {
			t.Fatalf("%s original liability requested replacement authority", status)
		}
		select {
		case <-owner.requested:
			t.Fatal("unresolved original work woke current preparation")
		default:
		}
	}
	resolved := &SteeringIntent{Status: "applied", SubnetEpoch: 7}
	var wait *productionPreparationPending
	if err := steerer.requestProductionPreparation(resolved); !errors.As(err, &wait) || !wait.epochKnown || wait.nativeEpoch != 7 {
		t.Fatalf("resolved owner omitted its observable preparation wait: %v", err)
	}
	select {
	case <-owner.requested:
	default:
		t.Fatal("resolved original outcome did not request current preparation")
	}
	if err := steerer.requestProductionPreparation(nil); !errors.As(err, &wait) {
		t.Fatal("fresh startup bypassed uncompleted current preparation")
	}
	owner.readyOnce.Do(func() { close(owner.ready) })
	if err := steerer.requestProductionPreparation(resolved); err != nil {
		t.Fatal(err)
	}
}

// A readiness wait stays visible, does not spend or reset the hard-error
// budget, and never launders a joined integrity defect into a wait.
func TestProductionStartupPreparationWaitPreservesHardFailureBudget(t *testing.T) {
	progress := &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
	wait := &productionPreparationPending{nativeEpoch: 7, epochKnown: true}
	count := 0
	if err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error { count++; return wait }, func() bool { return count < releaseSteeringFailureLimit+1 }, progress); err != nil || count != releaseSteeringFailureLimit+1 || progress.value.Steering == nil || progress.value.Steering.Current || progress.value.Steering.Outcome != "read_wait" {
		t.Fatalf("preparation wait stopped or claimed healthy startup: polls=%d error=%v", count, err)
	}
	hard := errors.New("synthetic original authority contradiction")
	count = 0
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error { count++; return errors.Join(wait, hard) }, func() bool { return true }, progress)
	if !errors.Is(err, hard) || count != releaseSteeringFailureLimit || progress.value.Steering.Outcome != "hard_error" {
		t.Fatalf("preparation wait concealed a genuine integrity cause: polls=%d error=%v", count, err)
	}
}
