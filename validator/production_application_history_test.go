//go:build linux || darwin

// A finalized row can be applied and then replaced before this process reads
// the head: on 2026-11-08 intent 26216's row was live on Finney from block
// 9461613 to 9461969, while the validator's retained-intent read was stalled,
// and the weight fallback's same-hotkey commit then replaced it for good. The
// validator waited in reveal_wait for the rest of the day. These tests drive
// the actual application reader against independent per-height chain rows.
package validator

import (
	"errors"
	"testing"
)

// Finalizes the fixture's actual signed intent at block 103, then lets the
// returned selector choose each height's applied row. The approval admits
// native blocks through the given height.
func finalizeProductionApplicationHistoryFixture(t *testing.T, through uint64) (*productionContinuationTestFixture, *productionContinuationNativeTestClient, *SteeringIntent) {
	t.Helper()
	fixture := newProductionContinuationTestFixtureThrough(t, through)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	native.receiptNumber, fixture.production.head = 103, 104
	fixture.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("the fixture intent did not finalize: %v", err)
	}
	finalized := fixture.restart(t)
	if finalized.Status != "finalized" || finalized.RevealBlock < 104 || finalized.RevealBlock > 800 {
		t.Fatalf("finalized fixture intent is outside the synthetic chain: status=%s reveal=%d", finalized.Status, finalized.RevealBlock)
	}
	native.weightReads = map[uint64]int{}
	return fixture, native, finalized
}

// The 11-08 shape: the fallback's earlier commit reveals first, the intent's
// own row follows, and the fallback's next commit replaces it before the head
// this process reads. The first height that carried the exact row is its
// application, exactly as if the head had been read then.
func TestProductionApplicationFindsRowReplacedBeforeObservation(t *testing.T) {
	fixture, native, finalized := finalizeProductionApplicationHistoryFixture(t, 200)
	reveal := finalized.RevealBlock
	native.appliedAt = func(number uint64) bool { return number >= reveal+2 && number <= reveal+4 }
	fixture.production.head = reveal + 8

	err := fixture.steerer.submitOnceV2(t.Context())
	var transition *productionSteeringTransition
	if !errors.As(err, &transition) || transition.revealWait || transition.nativeEpoch != finalized.SubnetEpoch {
		t.Fatalf("a row applied and then replaced was never observed: %v", err)
	}
	applied := fixture.restart(t)
	if applied.Status != "applied" || applied.ApplicationBlock != reveal+2 || applied.ApplicationBlockHash != fixture.production.block(reveal+2).Hex() {
		t.Fatalf("application is not the first height that carried the exact row: status=%s block=%d (reveal %d)", applied.Status, applied.ApplicationBlock, reveal)
	}
	if applied.Prepared.ExtrinsicHex != finalized.Prepared.ExtrinsicHex || applied.FinalizedBlock != finalized.FinalizedBlock || len(native.broadcasts) != 1 {
		t.Fatal("observing history changed the original signed work")
	}
	for number := range native.weightReads {
		if number < reveal {
			t.Fatalf("application read a row from before the reveal block: %d < %d", number, reveal)
		}
	}
}

// The row the intent commits can already be live before its own reveal, from
// an earlier identical commit. Heights before the reveal block are never
// evidence; a row replaced from the reveal block onward stays a reveal wait.
func TestProductionApplicationNeverCountsRowsBeforeReveal(t *testing.T) {
	fixture, native, finalized := finalizeProductionApplicationHistoryFixture(t, 200)
	reveal := finalized.RevealBlock
	native.appliedAt = func(number uint64) bool { return number < reveal }
	fixture.production.head = reveal + 8

	err := fixture.steerer.submitOnceV2(t.Context())
	var transition *productionSteeringTransition
	if !errors.As(err, &transition) || !transition.revealWait {
		t.Fatalf("a row only live before the reveal block ended the wait: %v", err)
	}
	if current := fixture.restart(t); current.Status != "finalized" || current.ApplicationBlock != 0 {
		t.Fatalf("a pre-reveal row was recorded as the application: status=%s block=%d", current.Status, current.ApplicationBlock)
	}
	for number := range native.weightReads {
		if number < reveal {
			t.Fatalf("application read a row from before the reveal block: %d < %d", number, reveal)
		}
	}
}

// The look-back only adds evidence. A history read that fails, even with a
// hard cause, leaves the head's own verdict, a reveal wait, and spends no
// steering failure; the next poll reads that height again.
func TestProductionApplicationHistoryFailureStaysRevealWait(t *testing.T) {
	fixture, native, finalized := finalizeProductionApplicationHistoryFixture(t, 200)
	reveal := finalized.RevealBlock
	native.appliedAt = func(number uint64) bool { return number >= reveal+2 && number <= reveal+4 }
	native.weightErrorKVs = map[uint64]error{reveal + 1: errors.New("synthetic malformed weight row")}
	fixture.production.head = reveal + 8

	err := fixture.steerer.submitOnceV2(t.Context())
	var transition *productionSteeringTransition
	if !errors.As(err, &transition) || !transition.revealWait {
		t.Fatalf("a failed history read replaced the head's reveal wait: %v", err)
	}
	if current := fixture.restart(t); current.Status != "finalized" || current.ApplicationBlock != 0 {
		t.Fatalf("a failed history read recorded an application: status=%s block=%d", current.Status, current.ApplicationBlock)
	}

	native.weightErrorKVs = nil
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("the next poll did not read the failed height again: %v", err)
	}
	if applied := fixture.restart(t); applied.Status != "applied" || applied.ApplicationBlock != reveal+2 {
		t.Fatalf("application after the retried read: status=%s block=%d", applied.Status, applied.ApplicationBlock)
	}
}
