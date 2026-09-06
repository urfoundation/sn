//go:build linux || darwin

package validator

// Batch controls use genuine full M8 inputs and the actual row/header Sync
// boundary. Small private no-op stages isolate persistence ownership without
// accepting a synthetic public record or replacing either replay verifier.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

// A private stage starts from a fully authenticated committed M8 generation.
// Hooks are fixed at construction and can be armed only after setup returns.
func newStatsAggregateTestBatchStage(t *testing.T, hooks statsAggregateHooks) *statsAggregateTestDurabilityStage {
	t.Helper()
	fixture := newStatsAggregateTestFixture(t, 1, 0, hooks)
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
	rows := statsAggregateTestRows(t, fixture.store, head.Generation)
	if len(rows) != 7 {
		t.Fatalf("genuine M8 provider census = %d, want 7", len(rows))
	}
	ctx, done, err := fixture.store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fixture.store.writeGate <- struct{}{}
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { <-fixture.store.writeGate; done() }) }
	t.Cleanup(finish)
	stage := statsAggregateStage{store: fixture.store, old: head, head: head, matchedPrefix: true}
	stage.head.Generation++
	fixture.store.stateLock.Lock()
	fixture.store.needsRecovery = true
	fixture.store.stateLock.Unlock()
	return &statsAggregateTestDurabilityStage{fixture: fixture, stage: stage, row: rows[0], ctx: ctx, finish: finish}
}

// Reopening is a semantic/recovery control, never a substitute for observing
// the exact pre-rotation WAL Sync in the preserved causal regression.
func reopenStatsAggregateTestFixture(t *testing.T, fixture *statsAggregateTestFixture, hooks statsAggregateHooks) *statsAggregateStore {
	t.Helper()
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, hooks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture.store = store
	return store
}

// Repeated writes must not be coalesced: these exact admitted operations reach
// three actual row Syncs, followed by one distinct authority-header Sync.
func TestStatsAggregateBatchBoundsEveryActualWrite(t *testing.T) {
	t.Parallel()
	var armed, publishing atomic.Bool
	var rowSyncs, headerSyncs atomic.Int64
	var owner *statsAggregateTestDurabilityStage
	var operations, sizes []int
	staged := 0
	owner = newStatsAggregateTestBatchStage(t, statsAggregateHooks{
		StorageStep: func(operation, name string) error {
			if !armed.Load() {
				return nil
			}
			if operation == "aggregate-before-publish" {
				publishing.Store(true)
			}
			if operation == "after-file-sync" && strings.HasSuffix(name, ".log") {
				if publishing.Load() {
					headerSyncs.Add(1)
				} else {
					rowSyncs.Add(1)
				}
			}
			return nil
		},
		PointStaged: func(kind byte, keyBytes, valueBytes int) error {
			if !armed.Load() {
				return nil
			}
			staged++
			points := owner.stage.points
			if kind != 'p' || keyBytes != 25 || valueBytes != 279 || points.batch.Len() != (staged-1)%128+1 || len(points.pendingKeyValues) != 1 || cap(points.batch.Dump()) > 64*1024 || len(points.batch.Dump())+12 > 64*1024 {
				return errors.New("actual admitted batch exceeded its operation, byte or ownership bound")
			}
			return nil
		},
		BatchSynced: func(kind string, count, size int) error {
			if !armed.Load() {
				return nil
			}
			operations, sizes = append(operations, count), append(sizes, size)
			if kind != "rows" || int(rowSyncs.Load()) != len(operations) || owner.stage.points.batch.Len() != 0 || len(owner.stage.points.pendingKeyValues) != 0 {
				return errors.New("batch completion did not follow its own physical Sync and release")
			}
			return nil
		},
		BeforeCommit: func(statsAggregateHead) error {
			if armed.Load() && (staged != 257 || len(operations) != 3 || owner.stage.points.batch.Len() != 0) {
				return errors.New("header admission preceded all row batches")
			}
			return nil
		},
	})
	armed.Store(true)
	for count := 0; count < 257; count++ {
		if err := owner.stage.putPoint(owner.ctx, statsAggregateProviderKey(owner.row.ClientID), owner.row.encode()); err != nil {
			t.Fatal(err)
		}
	}
	if rowSyncs.Load() != 2 || headerSyncs.Load() != 0 || owner.stage.points.batch.Len() != 1 {
		t.Fatal("row batching became per-point synchronization or lost its final pending row")
	}
	head, err := owner.stage.publish(owner.ctx)
	if err != nil || head != owner.stage.head || !reflect.DeepEqual(operations, []int{128, 128, 1}) || !reflect.DeepEqual(sizes, []int{39436, 39436, 320}) || rowSyncs.Load() != 3 || headerSyncs.Load() != 1 {
		t.Fatalf("actual batch sequence count=%v bytes=%v syncs=%d/%d error=%v", operations, sizes, rowSyncs.Load(), headerSyncs.Load(), err)
	}
	assertStatsAggregateTestParity(t, owner.fixture, head)
}

// Invalid shapes are rejected before copying or flushing; the legal fixed
// provider, empty-egress and complete-claim shapes remain admitted unchanged.
func TestStatsAggregateBatchRejectsOversizedRowsBeforeCopy(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	var writes, points atomic.Int64
	owner := newStatsAggregateTestBatchStage(t, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if armed.Load() && operation == "before-write" && strings.HasSuffix(name, ".log") {
			writes.Add(1)
		}
		return nil
	}, PointStaged: func(byte, int, int) error {
		if armed.Load() {
			points.Add(1)
		}
		return nil
	}})
	terminal := owner.fixture.source.recordTs[len(owner.fixture.source.recordTs)-1]
	binding := terminal.Assignments[0].Binding
	claim, err := encodeStatsAggregateClaim(binding)
	if err != nil {
		t.Fatal(err)
	}
	providerKey := statsAggregateVersionKey(statsAggregateProviderKey(owner.row.ClientID), owner.stage.head.Generation)
	egressKey := statsAggregateVersionKey(statsAggregateEgressKey(1, binding.ClientID, [32]byte{1}), owner.stage.head.Generation)
	claimKey := statsAggregateVersionKey(statsAggregateClaimKey(1, terminal.Sequence, binding.ClientID, [32]byte{1}), owner.stage.head.Generation)
	armed.Store(true)
	cases := []struct {
		name  string
		key   []byte
		value []byte
	}{
		{name: "empty-key", value: owner.row.encode()},
		{name: "unknown-kind", key: append([]byte{'x'}, providerKey[1:]...), value: owner.row.encode()},
		{name: "short-key", key: providerKey[:24], value: owner.row.encode()},
		{name: "oversized-key", key: bytes.Repeat([]byte{'p'}, 100*1024), value: owner.row.encode()},
		{name: "oversized-provider", key: providerKey, value: make([]byte, 100*1024)},
		{name: "nonempty-egress", key: egressKey, value: []byte{1}},
		{name: "short-claim", key: claimKey, value: claim[:len(claim)-1]},
	}
	for _, test := range cases {
		batch := newStatsAggregateBatch(owner.fixture.store, false)
		keyBefore, valueBefore := bytes.Clone(test.key), bytes.Clone(test.value)
		if err := batch.append(owner.ctx, test.key, test.value); err == nil || batch.batch.Len() != 0 || len(batch.pendingKeyValues) != 0 || cap(batch.batch.Dump()) > 64*1024 || !bytes.Equal(test.key, keyBefore) || !bytes.Equal(test.value, valueBefore) {
			t.Fatalf("%s was copied, mutated or admitted: %v", test.name, err)
		}
		if err := batch.flush(owner.ctx); err == nil {
			t.Fatalf("%s lost its first operation refusal", test.name)
		}
	}
	if writes.Load() != 0 || points.Load() != 0 {
		t.Fatal("invalid point shape reached staging observation or physical I/O")
	}
	batch := newStatsAggregateBatch(owner.fixture.store, false)
	for _, test := range []struct{ key, value []byte }{{key: providerKey, value: owner.row.encode()}, {key: egressKey}, {key: claimKey, value: claim}} {
		if err := batch.append(owner.ctx, test.key, test.value); err != nil {
			t.Fatal(err)
		}
	}
	if batch.batch.Len() != 3 || len(batch.pendingKeyValues) != 3 || points.Load() != 3 || writes.Load() != 0 {
		t.Fatal("legal fixed row controls were not retained as one pending bounded batch")
	}
}

// The complete signed census spans multiple batches without changing the
// explicit 256-record/32-trail replay ceiling or authenticated M8 policy. The
// larger test-only disk ledger is provisioned to that ceiling before startup.
func TestStatsAggregateCrossBatchReplayPreservesCompleteM8Census(t *testing.T) {
	t.Parallel()
	var batches, staged int
	var observedStage *statsAggregateStage
	maxPending := 0
	fixture := newStatsAggregateLargeTestFixture(t, 20, 1, statsAggregateHooks{PointStaged: func(byte, int, int) error {
		staged++
		if observedStage != nil {
			maxPending = max(maxPending, len(observedStage.points.pendingKeyValues))
			if maxPending > 128 || observedStage.points.batch.Len() > 128 || cap(observedStage.points.batch.Dump()) > 64*1024 {
				return errors.New("distinct complete claim points exceeded their copied pending bound")
			}
		}
		return nil
	}, BatchSynced: func(kind string, operations, size int) error {
		if kind != "rows" || operations < 1 || operations > 128 || size > 64*1024 || size <= 12 {
			return errors.New("full replay submitted an unbounded row batch")
		}
		batches++
		return nil
	}})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil || fixture.source.policy.Verify.TrailDepth != 8 || len(fixture.source.recordTs) != 162 || batches < 3 || staged <= 256 || head.EgressClaimCount != 140 {
		t.Fatalf("real cross-batch census: records=%d batches=%d staged=%d head=%+v error=%v", len(fixture.source.recordTs), batches, staged, head, err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
	claims, err := AttemptCutEgressClaims(&AttemptLedgerCut{EgressFirstSequence: fixture.source.expected.EgressFirstSequence, Records: fixture.source.recordTs})
	if err != nil || len(claims) != 140 {
		t.Fatalf("independent genuine claim census differs: %v", err)
	}
	ctx, done, err := fixture.store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	fixture.store.writeGate <- struct{}{}
	defer func() { <-fixture.store.writeGate }()
	stage := statsAggregateStage{store: fixture.store, old: head, head: head, matchedPrefix: true}
	stage.head.Generation++
	observedStage = &stage
	fixture.store.stateLock.Lock()
	fixture.store.needsRecovery = true
	fixture.store.stateLock.Unlock()
	for _, claim := range claims[:129] {
		hash, err := canonicalAttemptHex32("test complete claim", claim.EgressIPHash, false)
		if err != nil {
			t.Fatal(err)
		}
		value, err := encodeStatsAggregateClaim(claim.Binding)
		if err != nil {
			t.Fatal(err)
		}
		if err := stage.putPoint(ctx, statsAggregateClaimKey(head.EgressGeneration, claim.Sequence, claim.Binding.ClientID, hash), value); err != nil {
			t.Fatal(err)
		}
	}
	if maxPending != 128 || len(stage.points.pendingKeyValues) != 1 || stage.points.batch.Len() != 1 {
		t.Fatalf("distinct copied lookup did not reach and release its exact bound: peak=%d pending=%d operations=%d", maxPending, len(stage.points.pendingKeyValues), stage.points.batch.Len())
	}
	visible, err := fixture.store.Head(context.Background())
	if err != nil || visible != head {
		t.Fatalf("unpublished bounded claim stage changed the complete authority: %v", err)
	}
}

// Both empty presence and full binding bytes are read through the current
// batch. Caller buffers and returned buffers cannot rewrite a pending claim.
func TestStatsAggregatePendingClaimAndEmptyEgressOwnBytes(t *testing.T) {
	t.Parallel()
	owner := newStatsAggregateTestBatchStage(t, statsAggregateHooks{})
	terminal := owner.fixture.source.recordTs[len(owner.fixture.source.recordTs)-1]
	hop := terminal.Proof.Hops[1]
	var binding AttemptBinding
	for _, assignment := range terminal.Assignments {
		if assignment.NextHop == hop.ClientId {
			binding = assignment.Binding
		}
	}
	value, err := encodeStatsAggregateClaim(binding)
	if err != nil || !binding.Active || !binding.UIDFound {
		t.Fatalf("real complete claim is unavailable: %v", err)
	}
	egressKey := statsAggregateEgressKey(owner.stage.head.EgressGeneration, hop.ClientId, hop.EgressIpHash)
	claimKey := statsAggregateClaimKey(owner.stage.head.EgressGeneration, terminal.Sequence, hop.ClientId, hop.EgressIpHash)
	wantValue, wantKey := bytes.Clone(value), bytes.Clone(claimKey)
	if err := owner.stage.putPoint(owner.ctx, egressKey, nil); err != nil {
		t.Fatal(err)
	}
	if err := owner.stage.putPoint(owner.ctx, claimKey, value); err != nil {
		t.Fatal(err)
	}
	for index := range value {
		value[index] ^= 255
	}
	for index := range claimKey {
		claimKey[index] ^= 255
	}
	if value, exists, err := owner.stage.point(egressKey); err != nil || !exists || len(value) != 0 {
		t.Fatalf("pending empty egress became absent: %v", err)
	}
	got, exists, err := owner.stage.point(wantKey)
	if err != nil || !exists || !bytes.Equal(got, wantValue) {
		t.Fatalf("pending claim borrowed caller bytes: %v", err)
	}
	got[0] ^= 255
	got, exists, err = owner.stage.point(wantKey)
	if err != nil || !exists || !bytes.Equal(got, wantValue) {
		t.Fatalf("pending lookup exposed its owned claim bytes: %v", err)
	}
	if _, err := owner.fixture.store.db.Get(statsAggregateVersionKey(wantKey, owner.stage.head.Generation), nil); !errors.Is(err, leveldb.ErrNotFound) {
		t.Fatalf("pending ownership control was not pending: %v", err)
	}
	// A canonical but conflicting stored binding is fault injection at the
	// real record helper, not a newly accepted public record or cut.
	binding.UID++
	conflict, err := encodeStatsAggregateClaim(binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.stage.putPoint(owner.ctx, wantKey, conflict); err != nil {
		t.Fatal(err)
	}
	owner.stage.old.LastAppliedSequence = 0
	owner.stage.old.LastAppliedRoot = owner.fixture.source.expected.PriorRoot
	if err := owner.stage.record(owner.ctx, terminal); err == nil || !strings.Contains(err.Error(), "conflicting binding") {
		t.Fatalf("same pending claim key changed its complete binding: %v", err)
	}
	if owner.stage.head.EgressHashCount != owner.stage.old.EgressHashCount {
		t.Fatal("present empty pending egress was counted twice")
	}
	head, err := owner.fixture.store.Head(context.Background())
	if err != nil || head.Generation != owner.stage.head.Generation-1 {
		t.Fatalf("conflicting pending claim was published: %v", err)
	}
}

// The fold opens its canonical iterator only after all pending record rows
// have been synchronized. The independent existing Stats preview owns both
// exact integer and separately rounded legacy EMA expectations.
func TestStatsAggregateFoldFlushesPendingProviderRows(t *testing.T) {
	t.Parallel()
	var operations []int
	fixture := newStatsAggregateLargeTestFixture(t, 20, 0, statsAggregateHooks{BatchSynced: func(kind string, count, size int) error {
		if kind != "rows" || count > 128 || size > 64*1024 {
			return errors.New("fold submitted an unbounded row batch")
		}
		operations = append(operations, count)
		return nil
	}})
	ppm, ema := fixture.source.engine.stats.QualityPPM(), fixture.source.engine.stats.Quality()
	wantedProviders := fixture.source.engine.stats.currentReleaseStatsMeasurement().Providers
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{RotateNative: true, ToSettlementEpoch: 43})
	if err != nil || len(operations) != 4 || operations[0] != 128 || operations[1] != 128 || operations[2] >= 128 || operations[3] != len(wantedProviders) {
		t.Fatalf("fold did not flush the partial record batch before its complete provider census: batches=%v providers=%d error=%v", operations, len(wantedProviders), err)
	}
	rows := statsAggregateTestRows(t, fixture.store, head.Generation)
	if len(rows) != len(wantedProviders) || len(rows) < 7 || head.SettlementEpoch != 43 || head.SettlementFirstSequence != 161 || head.SettlementPriorRoot != fixture.cut.Root || head.EgressGeneration != 2 || head.EgressFirstSequence != 161 || head.EgressHashCount != 0 || head.EgressClaimCount != 0 {
		t.Fatalf("fold changed complete identities or independent clocks: head=%+v", head)
	}
	for index, row := range rows {
		wantPPM, hasPPM := ppm[row.ClientID]
		wantEMA, hasEMA := ema[row.ClientID]
		if row.ClientID.String() != wantedProviders[index].ClientID || row.WindowPresent || row.Window != (ProviderWindow{}) || row.PriorQualityPPM != wantPPM || row.HasPriorQuality != hasPPM || math.Float64bits(row.PriorEMA) != math.Float64bits(wantEMA) || row.HasPriorEMA != hasEMA {
			t.Fatalf("fold changed independent exact/legacy provider %s", row.ClientID)
		}
	}
}

// A completed row Sync followed by cancellation leaves durable but invisible
// rows. Cancellation is not a physical-store fault or a committed header.
func TestStatsAggregateRowSyncCancellationKeepsPriorAuthority(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var armed, publishing atomic.Bool
	var rowSyncs atomic.Int64
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if !armed.Load() {
			return nil
		}
		if operation == "aggregate-before-publish" {
			publishing.Store(true)
		}
		if !publishing.Load() && operation == "after-file-sync" && strings.HasSuffix(name, ".log") {
			rowSyncs.Add(1)
			cancel()
		}
		return nil
	}})
	armed.Store(true)
	result, err := fixture.store.Replay(ctx, fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if !errors.Is(err, context.Canceled) || result != (statsAggregateHead{}) || rowSyncs.Load() != 1 || publishing.Load() {
		t.Fatalf("post-row-Sync cancellation: syncs=%d publishing=%t result=%+v error=%v", rowSyncs.Load(), publishing.Load(), result, err)
	}
	head, err := fixture.store.Head(context.Background())
	if err != nil || head.Generation != 0 || head.ProviderCount != 0 {
		t.Fatalf("post-row-Sync cancellation changed prior authority: %v", err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatalf("operation cancellation became a physical-store fault: %v", err)
	}
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	head, err = store.Head(context.Background())
	if err != nil || head.Generation != 0 || len(statsAggregateTestRows(t, store, 0)) != 0 {
		t.Fatalf("canceled synchronized rows survived recovery as visible state: %v", err)
	}
}

// Successful header Sync is the commit point. A cancellation delivered after
// its file and directory Sync cannot turn that successful commit into absence.
func TestStatsAggregateHeaderSyncCancellationKeepsCommittedResult(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var armed, publishing atomic.Bool
	var headerSyncs atomic.Int64
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if armed.Load() && operation == "aggregate-before-publish" {
			publishing.Store(true)
		}
		if publishing.Load() && operation == "after-file-sync" && strings.HasSuffix(name, ".log") {
			headerSyncs.Add(1)
			cancel()
		}
		return nil
	}})
	armed.Store(true)
	head, err := fixture.store.Replay(ctx, fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil || ctx.Err() != context.Canceled || headerSyncs.Load() != 1 || head.Generation != 1 {
		t.Fatalf("successful header Sync was lost to later cancellation: syncs=%d head=%+v error=%v", headerSyncs.Load(), head, err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	reopened, err := store.Head(context.Background())
	if err != nil || reopened != head {
		t.Fatalf("canceled committed generation changed on reopen: %v", err)
	}
	assertStatsAggregateTestParity(t, fixture, reopened)
}

// A real row file+directory Sync may succeed before its observer reports a
// physical error. The failed owner returns no acceptance and recovery deletes
// its rows, without calling that deletion a rollback of the completed Sync.
func TestStatsAggregateRowPostSyncFailureRecoversInvisibleStage(t *testing.T) {
	t.Parallel()
	failure := errors.New("aggregate completed-row-Sync failure")
	var armed, publishing atomic.Bool
	var rowSyncs atomic.Int64
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if armed.Load() && operation == "aggregate-before-publish" {
			publishing.Store(true)
		}
		if armed.Load() && !publishing.Load() && operation == "after-file-sync" && strings.HasSuffix(name, ".log") {
			rowSyncs.Add(1)
			return failure
		}
		return nil
	}})
	armed.Store(true)
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) || rowSyncs.Load() != 1 || publishing.Load() {
		t.Fatalf("actual post-row-Sync fault: syncs=%d result=%+v error=%v", rowSyncs.Load(), result, err)
	}
	if _, err := fixture.store.Head(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("post-Sync physical fault was not latched: %v", err)
	}
	if err := fixture.store.Close(); !errors.Is(err, failure) {
		t.Fatalf("Close lost its completed-Sync error: %v", err)
	}
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	head, err := store.Head(context.Background())
	if err != nil || head.Generation != 0 || len(statsAggregateTestRows(t, store, 0)) != 0 {
		t.Fatalf("post-row-Sync fault exposed a partial generation: %v", err)
	}
	head, err = store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// A failed post-header-Sync call cannot report acceptance, although those
// already durable bytes reconstruct one complete committed generation later.
func TestStatsAggregateHeaderPostSyncFailureReopensCoherentGeneration(t *testing.T) {
	t.Parallel()
	failure := errors.New("aggregate completed-header-Sync failure")
	var armed, publishing atomic.Bool
	var headerSyncs atomic.Int64
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if armed.Load() && operation == "aggregate-before-publish" {
			publishing.Store(true)
		}
		if publishing.Load() && operation == "after-file-sync" && strings.HasSuffix(name, ".log") {
			headerSyncs.Add(1)
			return failure
		}
		return nil
	}})
	armed.Store(true)
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) || headerSyncs.Load() != 1 {
		t.Fatalf("post-header-Sync failure returned acceptance: syncs=%d result=%+v error=%v", headerSyncs.Load(), result, err)
	}
	if err := fixture.store.Close(); !errors.Is(err, failure) {
		t.Fatalf("Close lost its post-header-Sync failure: %v", err)
	}
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	head, err := store.Head(context.Background())
	if err != nil || head.Generation != 1 || head.LastAppliedRoot != fixture.cut.Root {
		t.Fatalf("completed header Sync did not reconstruct a coherent generation: %v", err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// Close cancels the admitted replay, then joins the real blocked row Sync
// before the active owner and its backend can be released.
func TestStatsAggregateCloseJoinsAdmittedBatchSync(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var armed, blocked, synced, publishing atomic.Bool
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if !armed.Load() {
			return nil
		}
		if operation == "aggregate-before-publish" {
			publishing.Store(true)
		}
		if operation == "before-file-sync" && strings.HasSuffix(name, ".log") && blocked.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		if operation == "after-file-sync" && strings.HasSuffix(name, ".log") {
			synced.Store(true)
		}
		return nil
	}})
	t.Cleanup(unblock)
	options := fixture.replayOptions(t)
	open := options.OpenData
	var replayCtx context.Context
	options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		replayCtx = ctx
		return open(ctx, kind, hash, size)
	}
	type replayResult struct {
		head statsAggregateHead
		err  error
	}
	replayed := make(chan replayResult, 1)
	armed.Store(true)
	go func() {
		head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{})
		replayed <- replayResult{head: head, err: err}
	}()
	<-entered
	type closeResult struct {
		err    error
		synced bool
	}
	closed := make(chan closeResult, 1)
	go func() {
		err := fixture.store.Close()
		closed <- closeResult{err: err, synced: synced.Load()}
	}()
	<-fixture.store.ctx.Done()
	<-replayCtx.Done()
	select {
	case result := <-closed:
		t.Fatalf("Close returned before the admitted filesystem Sync was released: %+v", result)
	default:
	}
	unblock()
	closeResultValue, replayResultValue := <-closed, <-replayed
	if closeResultValue.err != nil || !closeResultValue.synced || !errors.Is(replayResultValue.err, context.Canceled) || replayResultValue.head != (statsAggregateHead{}) || publishing.Load() {
		t.Fatalf("Close did not join its canceled row Sync: close=%+v replay=%+v publishing=%t", closeResultValue, replayResultValue, publishing.Load())
	}
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	head, err := store.Head(context.Background())
	if err != nil || head.Generation != 0 || len(statsAggregateTestRows(t, store, 0)) != 0 {
		t.Fatalf("closed synchronized stage exposed partial counters: %v", err)
	}
}

// Record processing can synchronize several complete batches before a later
// proof source fails. Every opened record reader closes; no header is exposed.
func TestStatsAggregateFlushedRowsLateProofFailurePublishesNothing(t *testing.T) {
	t.Parallel()
	var batches int
	fixture := newStatsAggregateLargeTestFixture(t, 20, 1, statsAggregateHooks{BatchSynced: func(kind string, count, size int) error {
		if kind != "rows" || count > 128 || size > 64*1024 {
			return errors.New("late-proof fixture exceeded a physical batch bound")
		}
		batches++
		return nil
	}})
	options := fixture.replayOptions(t)
	open := options.OpenData
	failure := errors.New("post-batch complete proof source refusal")
	opened, closed := 0, 0
	options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		if kind == AttemptStreamV2Proofs {
			if batches < 2 || opened == 0 || closed != opened {
				return nil, errors.New("proof refusal did not follow synchronized rows and closed record readers")
			}
			return nil, failure
		}
		reader, err := open(ctx, kind, hash, size)
		if err != nil {
			return reader, err
		}
		opened++
		return statsAggregateTestCloseReader{ReadCloser: reader, closed: func() error { closed++; return nil }}, nil
	}
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{})
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) || batches < 2 || opened != closed {
		t.Fatalf("late proof failure: batches=%d readers=%d/%d result=%+v error=%v", batches, opened, closed, result, err)
	}
	head, err := fixture.store.Head(context.Background())
	if err != nil || head.Generation != 0 || head.ProviderCount != 0 || len(statsAggregateTestRows(t, fixture.store, 0)) != 0 {
		t.Fatalf("already synchronized rows escaped before proof success: %v", err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	head, err = store.Head(context.Background())
	if err != nil || head.Generation != 0 || len(statsAggregateTestRows(t, store, 0)) != 0 {
		t.Fatalf("late-proof stage was not recovered: %v", err)
	}
}

// A pending maximum counter must be read before disk fallback. This is the
// real terminal helper with a fully verified M8 terminal and arithmetic fault
// injection, not a signed record altered to manufacture overflow.
func TestStatsAggregatePendingProviderOverflowDoesNotWrite(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	terminal := fixture.source.recordTs[len(fixture.source.recordTs)-1]
	if err := VerifyAttemptRecord(&terminal, fixture.source.ledger.identity, fixture.source.key.Public().(ed25519.PublicKey), fixture.source.server.serverPublicKeys()); err != nil {
		t.Fatal(err)
	}
	head, err := fixture.store.Head(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, done, err := fixture.store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	fixture.store.writeGate <- struct{}{}
	defer func() { <-fixture.store.writeGate }()
	stage := statsAggregateStage{store: fixture.store, old: head, head: head, matchedPrefix: true}
	stage.head.Generation++
	row := statsAggregateProvider{ClientID: terminal.Assignments[0].NextHop, WindowPresent: true, Window: ProviderWindow{Assignments: ^uint64(0)}}
	if err := row.validate(head.Config); err != nil {
		t.Fatal(err)
	}
	key, value := statsAggregateProviderKey(row.ClientID), row.encode()
	if err := stage.putPoint(ctx, key, value); err != nil {
		t.Fatal(err)
	}
	if err := stage.record(ctx, terminal); err == nil || !strings.Contains(err.Error(), "counter overflows") {
		t.Fatalf("pending counter overflow was missed: %v", err)
	}
	got, exists, err := stage.point(key)
	if err != nil || !exists || !bytes.Equal(got, value) || stage.points.batch.Len() != 1 || len(stage.points.pendingKeyValues) != 1 {
		t.Fatalf("overflow enqueued or changed its pending maximum row: %v", err)
	}
	if _, err := fixture.store.db.Get(statsAggregateVersionKey(key, stage.head.Generation), nil); !errors.Is(err, leveldb.ErrNotFound) {
		t.Fatalf("overflow reached physical row storage: %v", err)
	}
	visible, err := fixture.store.Head(context.Background())
	if err != nil || visible != head {
		t.Fatalf("overflow published a header: %v", err)
	}
}
