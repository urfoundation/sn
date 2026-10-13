//go:build linux || darwin

// Real pending custody remains usable when optional acceleration fails or a
// genuine body prefix outlives one read attempt's logical deadline.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Two deterministic deadline barriers interrupt the actual shared scan after
// complete bodies. The production retry owner must preserve those bodies and
// continue, not repeatedly spend its 60-second attempt on the same prefix.
func TestProductionReceiptInterruptedBudgetRetainsActualPartialPrefix(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	original, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.production.head, native.receiptNumber = 104, 105
	fixture.production.epoch++
	native.bodyReads = map[uint64]int{}
	var attempt *productionNativeReadDeadline
	var attempts, operations, waits, interrupted int
	fixture.steerer.productionReadHooks.withTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		switch duration {
		case 300 * time.Second:
			operations++
			return context.WithTimeout(parent, duration)
		case 60 * time.Second:
			attempts++
			var cancel context.CancelFunc
			attempt, cancel = newProductionNativeReadDeadline(parent, duration)
			return attempt, cancel
		default:
			t.Fatalf("receipt retry changed the production read budget: %v", duration)
			return nil, nil
		}
	}
	fixture.steerer.productionReadHooks.wait = func(ctx context.Context, duration time.Duration) error {
		if ctx.Err() != nil || duration != releaseSnapshotStartupRetryDelay {
			t.Fatalf("partial-prefix retry lost its live operation owner: %v", ctx.Err())
		}
		waits++
		return nil
	}
	native.afterBody = func(number uint64) {
		if number != 100 && number != 102 {
			return
		}
		if native.bodyReads[number] != 1 {
			t.Fatalf("attempt deadline replayed an already admitted partial-prefix body %d", number)
		}
		if attempt == nil || attempt.Err() != nil {
			t.Fatal("actual body barrier lacks its live production attempt")
		}
		interrupted++
		attempt.finish(context.DeadlineExceeded)
	}
	var pendingWait *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &pendingWait) {
		t.Fatalf("slow complete bodies stopped original reconciliation: %v", err)
	}
	if interrupted != 2 || waits != 2 || operations == 0 || attempts <= operations || !slices.Equal(native.nonceReads, []uint64{104}) {
		t.Fatalf("partial retries borrowed an incomplete or different boundary: interrupted=%d waits=%d attempts=%d operations=%d nonces=%v", interrupted, waits, attempts, operations, native.nonceReads)
	}
	for number := uint64(100); number <= 104; number++ {
		if native.bodyReads[number] != 1 {
			t.Fatalf("budget recovery repeated complete body %d: %d reads", number, native.bodyReads[number])
		}
	}
	owner, err := newProductionReceiptCheckpointOwner(fixture.steerer, fixture.steerer.cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := owner.load(t.Context())
	if err != nil || checkpoint == nil || checkpoint.Through != 104 {
		t.Fatalf("successful partial recovery did not persist its real final boundary: %+v %v", checkpoint, err)
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(original, after) || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("partial deadline recovery mutated or replaced the signed liability")
	}
}

// A corrupt optional prefix is never used as evidence. Actual chain reads
// restore progress, while a closed observation reports cache degradation once.
func TestProductionReceiptCorruptCacheFallsBackToOriginalIntent(t *testing.T) {
	productionReceiptTestCacheDegradation(t, productionReceiptCacheRead)
}

// Failed optional persistence retains the proven memory boundary, without
// claiming durability or consuming the service's original-intent error budget.
func TestProductionReceiptUnwritableCachePreservesReconciliation(t *testing.T) {
	productionReceiptTestCacheDegradation(t, productionReceiptCacheWrite)
}

// The only fault is a real optional-path file, introduced before reading or
// after a body barrier. Source signatures, store replay and RPC admission run.
func productionReceiptTestCacheDegradation(t *testing.T, stage productionReceiptCacheStage) {
	t.Helper()
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	original, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.production.head, native.receiptNumber = 101, 103
	fixture.production.epoch++
	native.bodyReads = map[uint64]int{}
	owner, err := newProductionReceiptCheckpointOwner(fixture.steerer, fixture.steerer.cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	if stage == productionReceiptCacheRead {
		if err := os.Mkdir(owner.path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(owner.path, productionReceiptCheckpointName), []byte("{invalid cache"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else {
		native.afterBody = func(number uint64) {
			if number == 100 {
				if err := os.WriteFile(owner.path, []byte("optional namespace unavailable"), 0o600); err != nil {
					t.Fatal(err)
				}
				native.afterBody = nil
			}
		}
	}
	var observations []productionReceiptCacheObservation
	ctx := withProductionReceiptCacheDiagnostic(t.Context(), func(value productionReceiptCacheObservation) {
		if fixture.steerer.intents.v2.active.Load() {
			t.Fatal("cache diagnostic ran while original intent custody was active")
		}
		observations = append(observations, value)
	})
	var pendingWait *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(ctx); !errors.As(err, &pendingWait) {
		t.Fatalf("optional cache failure stopped original pending reconciliation: %v", err)
	}
	memory, disabled := fixture.runtime.receiptCache.snapshot(owner.scope)
	if !disabled || memory == nil || memory.Through != 101 || len(observations) != 1 || observations[0].nativeEpoch != pending.SubnetEpoch || observations[0].stage != stage {
		t.Fatalf("cache degradation lost verified progress or its bounded notice: disabled=%t memory=%+v observations=%+v", disabled, memory, observations)
	}
	// A new actual disk intent owner in this runtime must not reopen the bad
	// optional cache or repeat the already admitted prefix on its next poll.
	fixture.restart(t)
	fixture.production.head = 102
	if err := fixture.steerer.submitOnceV2(ctx); !errors.As(err, &pendingWait) {
		t.Fatalf("disabled cache stranded later original reconciliation: %v", err)
	}
	for number := uint64(100); number <= 102; number++ {
		if native.bodyReads[number] != 1 {
			t.Fatalf("disabled cache repeated authenticated body %d: %d reads", number, native.bodyReads[number])
		}
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(original, after) || len(observations) != 1 || !slices.Equal(native.nonceReads, []uint64{101, 102}) || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("cache failure erased, replaced or obscured original signed progress")
	}
}
