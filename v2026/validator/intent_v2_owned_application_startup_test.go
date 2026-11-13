//go:build linux || darwin

// The real startup root can authenticate an already-applied synthetic original
// after a bounded live read failed. No persisted verification verdict is supplied.
package validator

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// A complete normal startup authenticates the retained applied entry and reaches
// fresh preparation. It neither edits that entry nor signs a replacement.
func TestIntentV2OwnedApplicationNormalStartupRecoversAppliedEntry(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	continuation := fixture.continuation
	pending := continuation.beginAndLoseAcknowledgement(t, fixture.native)
	fixture.native.receiptNumber, continuation.production.head = 103, 104
	continuation.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	if err := continuation.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("original canonical finalization: %v", err)
	}
	fixture.native.applied = true
	continuation.production.head = max(uint64(104), pending.Prepared.RevealBlock)
	var transition *productionSteeringTransition
	if err := continuation.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("original canonical application: %v", err)
	}
	store := continuation.steerer.intents
	stored := ownedApplicationSafetyStored(t, store)
	if stored.Current.Status != "applied" || stored.Current.VectorHash != pending.VectorHash {
		t.Fatal("test did not begin with a genuine retained applied original")
	}
	original := fixture.storedIntentBytes(t)
	fresh, err := newReleaseIntentStoreV2(continuation.runtime)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh owner has no lifecycle handoff. Its real public replay reaches
	// an explicit transport deadline; no elapsed-time race produces this wait.
	released := make(chan struct{})
	close(released)
	reader := continuation.runtime.history.readers[0]
	barrier := &ownedApplicationReplayBarrier{transport: reader.client.Transport, entered: make(chan struct{}), release: released}
	reader.client.Transport = barrier
	barrier.enabled.Store(true)
	err = continuation.steerer.productionRead(t.Context(), productionReadIntent, nil, func(ctx context.Context) error {
		_, err := fresh.currentV2(ctx)
		return err
	})
	var wait *productionSteeringReadWait
	if !errors.As(err, &wait) || wait.phase != productionReadIntent || fresh.v2.active.Load() {
		t.Fatalf("fresh bounded owner did not release its typed replay wait: %v", err)
	}
	select {
	case <-barrier.entered:
	default:
		t.Fatal("bounded read never attempted the original public replay")
	}
	barrier.enabled.Store(false)
	if !bytes.Equal(original, fixture.storedIntentBytes(t)) {
		t.Fatal("refused bounded read changed the retained applied entry")
	}
	// The existing current-only EVM barrier stops the fully reconstructed root
	// before any fresh signature or trail work. Historical authority stays live.
	continuation.production.epoch++
	continuation.production.operator.blocks[110] = [32]byte{0xa2}
	continuation.production.operator.finalized = 110
	fixture.closePreparation(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	var result error
	select {
	case result = <-done:
		t.Fatalf("normal startup stopped before preparation: %v", result)
	case <-fixture.latestRead:
		cancel()
		result = <-done
		t.Fatalf("retained startup used unrelated latest metadata: %v", result)
	case <-fixture.evm.freshRead:
		cancel()
		select {
		case result = <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("canceled normal startup did not join its owners")
		}
	case <-time.After(180 * time.Second):
		cancel()
		select {
		case result = <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("startup watchdog could not join its canceled owner")
		}
		t.Fatalf("normal startup never reached its preparation barrier: %v", result)
	}
	if result != nil && !errors.Is(result, context.Canceled) {
		t.Fatalf("normal startup failed while joining cancellation: %v", result)
	}
	if !bytes.Equal(original, fixture.storedIntentBytes(t)) || len(fixture.native.broadcasts) != 1 || fixture.native.broadcasts[0] != pending.Prepared.ExtrinsicHex {
		t.Fatal("normal startup changed the applied original or broadcast a replacement")
	}
	fixture.stateLock.Lock()
	latestReads, writeRequests := fixture.latestReads, fixture.writeRequests
	fixture.stateLock.Unlock()
	if latestReads != 0 || writeRequests != 0 {
		t.Fatal("retained recovery used latest metadata or wrote a native request")
	}
	for _, origin := range fixture.origins {
		select {
		case <-origin.seedRead:
			t.Fatal("unready preparation started fresh trail work")
		default:
		}
	}
}
