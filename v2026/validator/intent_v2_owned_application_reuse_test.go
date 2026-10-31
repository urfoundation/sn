//go:build linux || darwin

// A normal owned application transition must retain its completed verification.
// The real persisted intent, canonical native row and replay readers stay in use.
package validator

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The unchanged public origin fails only after the normal applied write succeeds.
type ownedApplicationReplayBarrier struct {
	transport http.RoundTripper
	enabled   atomic.Bool
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
}

// An explicit barrier models an expired replay transport without a timed sleep.
func (self *ownedApplicationReplayBarrier) RoundTrip(request *http.Request) (*http.Response, error) {
	if self.enabled.Load() && request.URL.Path == "/sn/attempt-artifact" {
		self.once.Do(func() { close(self.entered) })
		select {
		case <-self.release:
			return nil, context.DeadlineExceeded
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	}
	return self.transport.RoundTrip(request)
}

// No new public replay is needed after the same owner verified and applied it.
func TestIntentV2OwnedApplicationRetainsVerifiedReplay(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	barrier := &ownedApplicationReplayBarrier{transport: fixture.runtime.history.readers[0].client.Transport, entered: make(chan struct{}), release: make(chan struct{})}
	fixture.runtime.history.readers[0].client.Transport = barrier
	releaseReplay := sync.OnceFunc(func() { close(barrier.release) })
	defer releaseReplay()
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	native.receiptNumber, fixture.production.head = 101, 102
	fixture.production.extrinsicsKVs = map[uint64][]string{101: {pending.Prepared.ExtrinsicHex}}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("original canonical receipt did not reconcile: %v", err)
	}
	native.applied = true
	fixture.production.head = max(uint64(102), pending.Prepared.RevealBlock)
	var transition *productionSteeringTransition
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("canonical row did not complete the owned applied write: %v", err)
	}
	store := fixture.steerer.intents
	raw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := decodeSteeringIntentFileV2(t.Context(), raw, fixture.runtime.cfg.EvidenceV2.Bounds.IntentFileLimit())
	if err != nil || stored.Current == nil || stored.Current.Status != "applied" || stored.Current.VectorHash != pending.VectorHash || stored.Current.ApplicationBlock < pending.Prepared.RevealBlock {
		t.Fatalf("owned application did not retain its original identity: %v", err)
	}
	requests := fixture.artifactReads.Load()
	broadcasts := len(native.broadcasts)
	barrier.enabled.Store(true)
	fixture.steerer.productionReadHooks.wait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	// The result joins the real bounded reader; no fabricated replay verdict passes.
	type readResult struct {
		intent *SteeringIntent
		err    error
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan readResult, 1)
	go func() {
		var intent *SteeringIntent
		err := fixture.steerer.productionRead(ctx, productionReadIntent, nil, func(readCtx context.Context) error {
			var err error
			intent, err = store.currentV2(readCtx)
			return err
		})
		done <- readResult{intent: intent, err: err}
	}()
	select {
	case result := <-done:
		if result.err != nil || result.intent == nil || result.intent.Status != "applied" || result.intent.VectorHash != pending.VectorHash || fixture.artifactReads.Load() != requests || len(native.broadcasts) != broadcasts {
			t.Fatalf("owned applied replay was not reusable: result=%v objects=%d sends=%d", result.err, fixture.artifactReads.Load()-requests, len(native.broadcasts)-broadcasts)
		}
	case <-barrier.entered:
		releaseReplay()
		select {
		case result := <-done:
			var wait *productionSteeringReadWait
			if !errors.As(result.err, &wait) || wait.phase != productionReadIntent {
				t.Fatalf("repeated replay did not reach its typed intent-read deadline: %v", result.err)
			}
			t.Fatalf("owned applied transition repeated unchanged public replay before current preparation: %v", result.err)
		case <-time.After(5 * time.Second):
			t.Fatal("refused replay did not join its owned context")
		}
	case <-time.After(10 * time.Second):
		cancel()
		<-done
		t.Fatal("owned applied read never reached replay or completed")
	}
}
