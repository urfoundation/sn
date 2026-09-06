//go:build linux || darwin

package validator

// Recovery controls begin with a complete genuine M8 stage interrupted only
// after its full record/proof replay and row Syncs. Actual deletion batches,
// iterator ownership and filesystem failures remain the observed boundaries.

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/syndtr/goleveldb/leveldb/opt"
)

// Leaves more than one batch of canonical, genuinely derived invisible rows
// in the real backend. No fake provider, claim or generation history is added.
func newStatsAggregateTestRecoveryRows(t *testing.T, hooks statsAggregateHooks) (*statsAggregateTestFixture, int) {
	t.Helper()
	failure := errors.New("complete aggregate stage interrupted before its header")
	var candidate statsAggregateHead
	hooks.BeforeCommit = func(head statsAggregateHead) error { candidate = head; return failure }
	fixture := newStatsAggregateLargeTestFixture(t, 20, 1, hooks)
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) || candidate.Generation != 1 || candidate.EgressClaimCount != 140 || len(fixture.source.recordTs) != 162 || fixture.source.policy.Verify.TrailDepth != 8 {
		t.Fatalf("full real interrupted stage was not reached: candidate=%+v result=%+v error=%v", candidate, result, err)
	}
	cursor := fixture.store.db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	rows := 0
	for cursor.Next() {
		if len(cursor.Key()) != 1 || cursor.Key()[0] != 'h' {
			rows++
		}
	}
	scanErr := cursor.Error()
	cursor.Release()
	if scanErr != nil || rows <= 128 || rows >= 256 || uint64(rows) != candidate.ProviderCount+candidate.EgressHashCount+candidate.EgressClaimCount {
		t.Fatalf("actual interrupted rows=%d candidate=%+v error=%v", rows, candidate, scanErr)
	}
	visible, err := fixture.store.Head(context.Background())
	if err != nil || visible.Generation != 0 || visible.ProviderCount != 0 || visible.EgressHashCount != 0 || visible.EgressClaimCount != 0 {
		t.Fatalf("interrupted stage already published authority: %v", err)
	}
	return fixture, rows
}

// The only surviving physical row after cleanup is the unchanged empty
// authority. This checks actual deletion, not merely filtered visibility.
func assertStatsAggregateTestRecoveredEmpty(t *testing.T, fixture *statsAggregateTestFixture) {
	t.Helper()
	head, err := fixture.store.Head(context.Background())
	if err != nil || head.Generation != 0 || head.ProviderCount != 0 || head.EgressHashCount != 0 || head.EgressClaimCount != 0 || len(statsAggregateTestRows(t, fixture.store, 0)) != 0 {
		t.Fatalf("recovery changed empty committed authority: %+v error=%v", head, err)
	}
	cursor := fixture.store.db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	onlyHeader := cursor.Next() && string(cursor.Key()) == "h" && !cursor.Next()
	scanErr := cursor.Error()
	cursor.Release()
	if scanErr != nil || !onlyHeader {
		t.Fatalf("recovery retained physical invisible rows: %v", scanErr)
	}
}

// Real recovery uses 128 bounded, copied deletion keys and one final partial
// batch. Each observed completion follows its own actual journal Sync.
func TestStatsAggregateRecoveryDeletesUseBoundedSyncBatches(t *testing.T) {
	t.Parallel()
	fixture, rows := newStatsAggregateTestRecoveryRows(t, statsAggregateHooks{})
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	var recovery atomic.Bool
	var syncs atomic.Int64
	var operations []int
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if operation == "aggregate-before-recovery" {
			recovery.Store(true)
		}
		if recovery.Load() && operation == "after-file-sync" && strings.HasSuffix(name, ".log") {
			syncs.Add(1)
		}
		return nil
	}, BatchSynced: func(kind string, count, size int) error {
		if kind != "recovery-deletes" {
			return nil
		}
		operations = append(operations, count)
		if !recovery.Load() || count < 1 || count > 128 || size <= 12 || size > 64*1024 || int(syncs.Load()) != len(operations) {
			return errors.New("recovery completion differs from bounded actual Syncs")
		}
		return nil
	}})
	if !reflect.DeepEqual(operations, []int{128, rows - 128}) || syncs.Load() != 2 {
		t.Fatalf("actual bounded recovery: rows=%d operations=%v syncs=%d", rows, operations, syncs.Load())
	}
	assertStatsAggregateTestRecoveredEmpty(t, fixture)
	head, err := store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// The fault is armed at real recoverRows entry, after backend Open. It cannot
// be satisfied by a service/startup error or by an unsynchronized Delete call.
func TestStatsAggregateRecoveryDeleteSyncFailureRefusesOwner(t *testing.T) {
	t.Parallel()
	fixture, _ := newStatsAggregateTestRecoveryRows(t, statsAggregateHooks{})
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("actual aggregate recovery deletion Sync failure")
	var recovery atomic.Bool
	var attempts, completions atomic.Int64
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if operation == "aggregate-before-recovery" {
			recovery.Store(true)
		}
		if recovery.Load() && operation == "before-file-sync" && strings.HasSuffix(name, ".log") {
			attempts.Add(1)
			return failure
		}
		return nil
	}, BatchSynced: func(kind string, _, _ int) error {
		if kind == "recovery-deletes" {
			completions.Add(1)
		}
		return nil
	}})
	if store != nil {
		_ = store.Close()
	}
	if !errors.Is(err, failure) || store != nil || !recovery.Load() || attempts.Load() != 1 || completions.Load() != 0 {
		t.Fatalf("actual recovery Sync refusal: store=%v attempts=%d completions=%d error=%v", store != nil, attempts.Load(), completions.Load(), err)
	}
	reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	assertStatsAggregateTestRecoveredEmpty(t, fixture)
}

// A failed post-Sync callback reports no usable owner, but the first 128
// deletions are already durable. Fresh recovery idempotently removes only
// the exact remaining rows, without replaying the completed batch.
func TestStatsAggregateRecoveryPostSyncFailureRestartsIdempotently(t *testing.T) {
	t.Parallel()
	fixture, rows := newStatsAggregateTestRecoveryRows(t, statsAggregateHooks{})
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("completed recovery deletion Sync failure")
	var recovery atomic.Bool
	var physicalSyncs, completions atomic.Int64
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if operation == "aggregate-before-recovery" {
			recovery.Store(true)
		}
		if recovery.Load() && operation == "after-file-sync" && strings.HasSuffix(name, ".log") {
			physicalSyncs.Add(1)
			return failure
		}
		return nil
	}, BatchSynced: func(kind string, _, _ int) error {
		if kind == "recovery-deletes" {
			completions.Add(1)
		}
		return nil
	}})
	if store != nil {
		_ = store.Close()
	}
	if !errors.Is(err, failure) || store != nil || physicalSyncs.Load() != 1 || completions.Load() != 0 {
		t.Fatalf("post-deletion-Sync failure: store=%v syncs=%d completions=%d error=%v", store != nil, physicalSyncs.Load(), completions.Load(), err)
	}
	var remaining []int
	reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{BatchSynced: func(kind string, count, size int) error {
		if kind != "recovery-deletes" || count > 128 || size > 64*1024 {
			return errors.New("restarted deletion batch differs")
		}
		remaining = append(remaining, count)
		return nil
	}})
	if !reflect.DeepEqual(remaining, []int{rows - 128}) {
		t.Fatalf("completed deletion batch was repeated or lost: original=%d remaining=%v", rows, remaining)
	}
	assertStatsAggregateTestRecoveredEmpty(t, fixture)
}

// A canceled recovery releases the actual pinned DB iterator before returning.
// The pinned backend's own live-iterator property observes that lifetime;
// successful reopening alone is not used as evidence of iterator release.
func TestStatsAggregateRecoveryCancellationJoinsIterator(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var armed atomic.Bool
	var fixture *statsAggregateTestFixture
	completed, iteratorAtSync := 0, ""
	fixture, rows := newStatsAggregateTestRecoveryRows(t, statsAggregateHooks{BatchSynced: func(kind string, count, size int) error {
		if !armed.Load() || kind != "recovery-deletes" {
			return nil
		}
		var err error
		iteratorAtSync, err = fixture.store.db.GetProperty("leveldb.aliveiters")
		if err != nil || count != 128 || size > 64*1024 || iteratorAtSync != "1" {
			return errors.Join(errors.New("cancellation did not reach one actual recovery iterator and full deletion batch"), err)
		}
		completed++
		cancel()
		return nil
	}})
	operationCtx, done, err := fixture.store.begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fixture.store.writeGate <- struct{}{}
	var finished atomic.Bool
	finish := func() {
		if finished.CompareAndSwap(false, true) {
			<-fixture.store.writeGate
			done()
		}
	}
	t.Cleanup(finish)
	head, err := fixture.store.readHead(fixture.store.db)
	if err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	err = fixture.store.recoverRows(operationCtx, head)
	iteratorAfter, propertyErr := fixture.store.db.GetProperty("leveldb.aliveiters")
	if !errors.Is(err, context.Canceled) || completed != 1 || iteratorAtSync != "1" || iteratorAfter != "0" || propertyErr != nil {
		t.Fatalf("canceled recovery lifetime: batches=%d iterators=%q/%q error=%v property=%v", completed, iteratorAtSync, iteratorAfter, err, propertyErr)
	}
	finish()
	if err := fixture.store.Close(); err != nil {
		t.Fatalf("operation cancellation latched a physical fault: %v", err)
	}
	var remaining []int
	reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{BatchSynced: func(kind string, count, size int) error {
		if kind != "recovery-deletes" || count > 128 || size > 64*1024 {
			return errors.New("canceled recovery restart exceeded its bounds")
		}
		remaining = append(remaining, count)
		return nil
	}})
	if !reflect.DeepEqual(remaining, []int{rows - 128}) {
		t.Fatalf("canceled recovery lost its completed durable batch: original=%d remaining=%v", rows, remaining)
	}
	assertStatsAggregateTestRecoveredEmpty(t, fixture)
}

// The public caller must retain the same distinction as the private batch:
// cancellation after deleting retired rows is not a physical-store fault.
// Native rotation supplies genuine retired claims without synthetic history.
func TestStatsAggregatePublicRecoveryCancellationPreservesOwner(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var armed atomic.Bool
	deleted := 0
	fixture := newStatsAggregateLargeTestFixture(t, 20, 1, statsAggregateHooks{BatchSynced: func(kind string, count, size int) error {
		if !armed.Load() || kind != "recovery-deletes" {
			return nil
		}
		if count != 128 || size > 64*1024 || deleted != 0 {
			return errors.New("public cleanup cancellation missed its first full bounded batch")
		}
		deleted += count
		cancel()
		return nil
	}})
	prior, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{RotateNative: true})
	if err != nil || prior.Generation != 1 || prior.EgressGeneration != 2 || prior.EgressFirstSequence != 163 || prior.EgressHashCount != 0 || prior.EgressClaimCount != 0 {
		t.Fatalf("real native rotation did not establish retired claims: %+v error=%v", prior, err)
	}
	priorRows := statsAggregateTestRows(t, fixture.store, prior.Generation)
	// This new cut is fully and independently sealed under the exact native
	// context just published by Replay; no candidate-selected clock is used.
	expected := prior.context()
	seal, objects := newAttemptCutV2SealTestOptions(t, fixture.source)
	cut, _, err := SealAttemptCutV2(context.Background(), fixture.source.ledger, expected, fixture.source.policy, fixture.source.key, fixture.source.bounds, seal)
	if err != nil || cut == nil || cut.RecordCount != 162 || cut.CompleteCount != 20 || cut.FailedCount != 1 {
		t.Fatalf("genuine next-native-context cut: %v", err)
	}
	reads := objects.reads
	options := AttemptCutV2ReplayOptions{Bounds: fixture.source.replay, ScratchDirectory: filepath.Join(t.TempDir(), "canceled-recovery-replay"), ServerKeys: seal.ServerKeys, ReadMetadata: seal.ReadMetadata, OpenData: seal.OpenData}
	armed.Store(true)
	result, err := fixture.store.Replay(ctx, *cut, expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{})
	if !errors.Is(err, context.Canceled) || result != (statsAggregateHead{}) || deleted != 128 || objects.reads != reads {
		t.Fatalf("public recovery cancellation: deleted=%d reads=%d/%d result=%+v error=%v", deleted, objects.reads, reads, result, err)
	}
	visible, err := fixture.store.Head(context.Background())
	if err != nil || visible != prior || !reflect.DeepEqual(priorRows, statsAggregateTestRows(t, fixture.store, prior.Generation)) {
		t.Fatalf("operation-local recovery cancellation became a physical fault or changed prior providers: %v", err)
	}
	iterators, err := fixture.store.db.GetProperty("leveldb.aliveiters")
	if err != nil || iterators != "0" {
		t.Fatalf("public canceled recovery retained its iterator: %q error=%v", iterators, err)
	}
	if _, err := fixture.store.Replay(context.Background(), *cut, expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{}); err == nil || !strings.Contains(err.Error(), "requires reopen") {
		t.Fatalf("incomplete cleanup was silently reused: %v", err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatalf("public recovery cancellation was falsely latched as physical Close failure: %v", err)
	}
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	visible, err = store.Head(context.Background())
	if err != nil || visible != prior || !reflect.DeepEqual(priorRows, statsAggregateTestRows(t, store, prior.Generation)) {
		t.Fatalf("reopen lost the native/settlement boundary or provider census: %v", err)
	}
}

// Operation-local cleanup cancellation must not soften persisted corruption.
// An unknown physical row is injected only after genuine full M8 admission;
// both the live owner and a fresh opener must refuse it without object I/O.
func TestStatsAggregateRecoveryCorruptionRemainsLatched(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
	if err := fixture.store.db.Put([]byte{'x'}, []byte{1}, &opt.WriteOptions{Sync: true, NoWriteMerge: true}); err != nil {
		t.Fatal(err)
	}
	reads := fixture.objects.reads
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err == nil || !strings.Contains(err.Error(), "unknown row") || result != (statsAggregateHead{}) || fixture.objects.reads != reads {
		t.Fatalf("persisted corruption was admitted or read candidate objects: result=%+v reads=%d/%d error=%v", result, fixture.objects.reads, reads, err)
	}
	if visible, err := fixture.store.Head(context.Background()); err == nil || !strings.Contains(err.Error(), "unknown row") || visible != (statsAggregateHead{}) {
		t.Fatalf("corrupt owner remained readable: %+v error=%v", visible, err)
	}
	iterators, err := fixture.store.db.GetProperty("leveldb.aliveiters")
	if err != nil || iterators != "0" {
		t.Fatalf("corrupt recovery retained its iterator: %q error=%v", iterators, err)
	}
	if err := fixture.store.Close(); err == nil || !strings.Contains(err.Error(), "unknown row") {
		t.Fatalf("stored corruption was not retained by Close: %v", err)
	}
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{})
	if store != nil {
		_ = store.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "unknown row") || store != nil {
		t.Fatalf("fresh owner accepted persisted corruption: store=%v error=%v", store != nil, err)
	}
}
