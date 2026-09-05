package validator

// Settlement closure has an independent finalized-EVM owner: a native submit
// may wait indefinitely for finality, but cannot suppress terminal exports.

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Advances one real settlement boundary. Missed epochs remain fail-closed;
// competing callers share the producer's sorted locks and journal.
func advanceReleaseSettlementSnapshot(ctx context.Context, stateDir string, snapshot *ReleaseSnapshot, participants []AttemptSettlementParticipant, boundary func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error)) error {
	return advanceReleaseSettlementSnapshotWithMode(ctx, stateDir, snapshot, participants, boundary, true)
}

// A read-only unchanged-epoch refresh must not clear the ordinary detach
// owner's active-attempt barrier. Native retries may finish their own journal.
func advanceReleaseSettlementSnapshotWithMode(ctx context.Context, stateDir string, snapshot *ReleaseSnapshot, participants []AttemptSettlementParticipant, boundary func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error), finishCurrent bool) error {
	if ctx == nil || snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() || boundary == nil {
		return errors.New("settlement refresh dependencies are incomplete")
	}
	ordered, err := validateAttemptSettlementParticipants(participants, true)
	if err != nil {
		return err
	}
	target := snapshot.Epoch.Uint64()
	{
		if err := ctx.Err(); err != nil {
			return err
		}
		epoch, known, err := func() (uint64, bool, error) {
			for _, participant := range ordered {
				participant.Stats.mu.Lock()
			}
			defer func() {
				for index := len(ordered) - 1; index >= 0; index-- {
					ordered[index].Stats.mu.Unlock()
				}
			}()
			first := ordered[0].Stats
			for _, participant := range ordered[1:] {
				if participant.Stats.settlementEpochKnown != first.settlementEpochKnown || participant.Stats.settlementEpoch != first.settlementEpoch {
					return 0, false, errors.New("settlement refresh participant ownership differs")
				}
			}
			return first.settlementEpoch, first.settlementEpochKnown, nil
		}()
		if err != nil {
			return err
		}
		if known && epoch > target {
			return errAttemptSettlementSnapshotStale
		}
		if known && epoch == target && !finishCurrent {
			return nil
		}
		if known && epoch < target && epoch+1 != target {
			return fmt.Errorf("cannot refresh settlement epoch %d from %d without consecutive terminal evidence", target, epoch)
		}
		var terminal AttemptBoundary
		if known && epoch < target {
			terminal, err = boundary(ctx, snapshot)
			if err != nil {
				return err
			}
		}
		return advanceAttemptSettlementEpochWithIOMode(stateDir, target, terminal, ordered, func(path string, payload []byte) error {
			return atomicStateWrite(path, payload, 0o600)
		}, removeAttemptSettlementTransaction, finishCurrent)
	}
}

// The existing poll cadence is retained. Active attempts keep the admission
// barrier closed until a later poll can drain it; permanent failures supervise
// the whole validator rather than becoming an endless log-and-retry loop.
func runReleaseSettlementRefresh(ctx context.Context, poll time.Duration, load releaseSnapshotLoader, advance func(context.Context, *ReleaseSnapshot) error, publish func(*ReleaseSnapshot), wait releaseSnapshotRetryWait) error {
	if ctx == nil || poll <= 0 || load == nil || advance == nil || publish == nil || wait == nil {
		return errors.New("settlement refresh owner dependencies are incomplete")
	}
	for {
		if err := wait(ctx, poll); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot, err := load(ctx)
		if err == nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			err = advance(ctx, snapshot)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, errAttemptCutPending) || errors.Is(err, errAttemptSettlementSnapshotStale) || transientReleaseSnapshotError(err) {
				continue
			}
			return fmt.Errorf("validator settlement refresh: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		publish(snapshot)
	}
}
