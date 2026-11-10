//go:build linux || darwin

// The look-back that finds a replaced row is bounded per poll and reads each
// finalized height at most once per intent in one process.
package validator

import (
	"errors"
	"testing"
)

// A far-behind head is read back in bounded steps. Each poll reads at most
// productionApplicationHistoryBlocksPerPoll heights below the head, a process
// reads each finalized height at most once per intent, and the earliest
// height that carried the row is the application.
func TestProductionApplicationHistoryReadsBoundedStepsOnce(t *testing.T) {
	fixture, native, finalized := finalizeProductionApplicationHistoryFixture(t, 1000)
	fixture.steerer.applicationHistory = newProductionApplicationHistoryMemo()
	reveal := finalized.RevealBlock
	step := uint64(productionApplicationHistoryBlocksPerPoll)
	match := reveal + 2*step + 5
	native.appliedAt = func(number uint64) bool { return number == match || number == match+1 }
	head := match + 20
	fixture.production.head = head

	polls := 0
	for {
		polls++
		if polls > 4 {
			t.Fatal("the bounded look-back did not reach the applied row")
		}
		before := len(native.weightReads)
		err := fixture.steerer.submitOnceV2(t.Context())
		var transition *productionSteeringTransition
		if !errors.As(err, &transition) {
			t.Fatalf("poll %d: %v", polls, err)
		}
		// The head is read every poll; history adds at most one step.
		if added := len(native.weightReads) - before; added > int(step)+1 {
			t.Fatalf("poll %d read %d new heights, more than one step", polls, added)
		}
		if !transition.revealWait {
			break
		}
	}
	if polls != 3 {
		t.Fatalf("the look-back took %d polls, want 3", polls)
	}
	for number, reads := range native.weightReads {
		if number != head && reads > 2 {
			// A matching height is read once by the look-back and once more
			// under its own authenticated runtime.
			t.Fatalf("height %d was read %d times", number, reads)
		}
		if number < reveal {
			t.Fatalf("application read a row from before the reveal block: %d < %d", number, reveal)
		}
	}
	applied := fixture.restart(t)
	if applied.Status != "applied" || applied.ApplicationBlock != match || applied.ApplicationBlockHash != fixture.production.block(match).Hex() {
		t.Fatalf("application is not the earliest height that carried the row: status=%s block=%d want %d", applied.Status, applied.ApplicationBlock, match)
	}
}
