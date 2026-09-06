//go:build linux || darwin

package validator

// The aggregate's larger genuine signed census must fit both its writer/replay
// and durable-ledger test configuration. Production limits are never inferred
// from requested work and the original small fixture remains explicit.

import (
	"context"
	"errors"
	"testing"
)

// Keep every byte/storage limit unchanged. Only these explicit larger test
// controls use the already-declared 256-record/32-trail replay ceiling.
func statsAggregateLargeTestLedgerLimits() AttemptLedgerDiskLimits {
	limits := attemptLedgerDiskTestLimits()
	limits.MaxRecordCount, limits.MaxTrailCount = 256, 32
	return limits
}

// This assertion inspects the actual constructor's ledger and real backend,
// not only the requested helper parameters or a replay declaration.
func assertStatsAggregateTestLedgerLimits(t *testing.T, source *attemptCutV2SealTestFixture, limits AttemptLedgerDiskLimits) {
	t.Helper()
	backend, ok := source.ledger.disk.(*attemptRecordStore)
	if !ok || source.ledger.diskLimits != limits || backend.bounds.MaxRecordCount != limits.MaxRecordCount || backend.bounds.MaxTrailCount != limits.MaxTrailCount || backend.bounds.MaxRecordBytes != limits.MaxRecordBytes || backend.bounds.MaxRawRecordBytes != limits.MaxRawRecordBytes || backend.bounds.MaxStorageBytes != limits.MaxStorageBytes || backend.bounds.MaxStorageFiles != limits.MaxStorageFiles {
		t.Fatalf("actual durable fixture bounds differ from explicit provisioning: ledger=%+v requested=%+v real_backend=%t", source.ledger.diskLimits, limits, ok)
	}
}

// The eight large cross-batch/recovery controls explicitly opt into this size.
// The small agreement control also checks its actual construction and replay.
// Full M8 trail generation, signing, disk appends, sealer and replay stay intact.
func newStatsAggregateLargeTestFixture(t *testing.T, complete, failed int, hooks statsAggregateHooks) *statsAggregateTestFixture {
	t.Helper()
	limits := statsAggregateLargeTestLedgerLimits()
	source := newAttemptCutV2SealTestFixtureWithLedgerLimits(t, 8, complete, failed, limits)
	assertStatsAggregateTestLedgerLimits(t, source, limits)
	if source.bounds.Records.MaxItems != 256 || source.replay.MaxTrails != 32 || source.policy.Verify.TrailDepth != 8 {
		t.Fatal("large aggregate ledger/replay ceiling or authenticated M8 policy differs")
	}
	return newStatsAggregateTestFixtureFromSource(t, source, hooks)
}

// An actual complete+failed M8 graph verifies the agreement after construction
// and full public sealing, without requiring another large census for a control.
func TestStatsAggregateLargeFixtureBindsExplicitLedgerAndReplayLimits(t *testing.T) {
	t.Parallel()
	before := attemptLedgerDiskTestLimits()
	if before.MaxRecordCount != 128 || before.MaxTrailCount != 16 {
		t.Fatalf("original small ledger ceiling changed: %+v", before)
	}
	fixture := newStatsAggregateLargeTestFixture(t, 1, 1, statsAggregateHooks{})
	large := fixture.source.ledger.diskLimits
	if large.MaxRecordCount != 256 || large.MaxTrailCount != 32 || fixture.cut.LastSequence != 10 || len(fixture.source.recordTs) != 10 {
		t.Fatalf("explicit larger fixture lost actual M8 complete+failed census: limits=%+v cut=%+v", large, fixture.cut)
	}
	large.MaxRecordCount, large.MaxTrailCount = before.MaxRecordCount, before.MaxTrailCount
	if large != before || attemptLedgerDiskTestLimits() != before {
		t.Fatal("larger fixture changed byte/storage bounds or the old default")
	}
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil || head.LastAppliedSequence != 10 || head.EgressClaimCount != 7 {
		t.Fatalf("explicit larger fixture failed full public aggregate replay: %+v error=%v", head, err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// The unchanged old constructor reaches its real 128-record/16-trail boundary.
// A seventeenth genuine SEED cannot append a checkpoint or move authority.
func TestStatsAggregateOriginalSmallLedgerStillRefusesCapacityOverflow(t *testing.T) {
	t.Parallel()
	source := newAttemptCutV2SealTestFixture(t, 8, 16, 0)
	limits := attemptLedgerDiskTestLimits()
	assertStatsAggregateTestLedgerLimits(t, source, limits)
	before, err := source.ledger.Head()
	if err != nil || limits.MaxRecordCount != 128 || limits.MaxTrailCount != 16 || before.LastSequence != 128 || before.TrailCount != 16 || len(source.recordTs) != 128 || source.policy.Verify.TrailDepth != 8 {
		t.Fatalf("unchanged small ledger did not reach its exact real M8 boundary: %+v error=%v", before, err)
	}
	proof, trailErr := source.engine.RunTrail(context.Background())
	var fatal *TrailFatalError
	if proof != nil || !errors.Is(trailErr, errAttemptRecordStoreLimit) || !errors.As(trailErr, &fatal) {
		t.Fatalf("old small ledger admitted a genuine overflow checkpoint: proof=%v error=%v", proof, trailErr)
	}
	after, err := source.ledger.Head()
	if err != nil || after != before {
		t.Fatalf("refused old-small checkpoint changed durable authority: before=%+v after=%+v error=%v", before, after, err)
	}
	pending := 0
	if err := source.ledger.disk.Pending(context.Background(), func(AttemptRecord) error { pending++; return nil }); err != nil || pending != 0 {
		t.Fatalf("refused old-small checkpoint retained pending history: pending=%d error=%v", pending, err)
	}
	active := func() uint64 {
		source.engine.stats.mu.Lock()
		defer source.engine.stats.mu.Unlock()
		return source.engine.stats.activeAttemptCount
	}()
	if active != 0 {
		t.Fatal("refused capacity checkpoint leaked its attempt reservation")
	}
}
