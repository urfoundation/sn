//go:build linux || darwin

package validator

// Durability controls observe the real journal and table writers. Rotation is
// forced by bounded no-op rewrites of a genuinely replayed M8 provider row;
// no fake proof, changed counter or orderly-close durability assumption is used.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

// The observer is private to one real backend. Its table barrier is released
// explicitly, including on assertion failure, before the backend owner closes.
type statsAggregateTestWALObserver struct {
	stateLock    sync.Mutex
	armed        bool
	publishing   bool
	disk         *attemptRecordStoreStorage
	dirtyKVs     map[string]bool
	sizeKVs      map[string]int64
	emptyWrites  int
	closedWAL    string
	closedDirty  bool
	rowSyncs     int
	headerSyncs  int
	tableEntered bool
	tableExited  bool
	tableSynced  bool
	rotated      chan struct{}
	tableEntry   chan struct{}
	tableRelease chan struct{}
	tableDurable chan struct{}
	releaseOnce  sync.Once
}

// Only bounded fixture journals and the first compaction table are observed.
func newStatsAggregateTestWALObserver() *statsAggregateTestWALObserver {
	return &statsAggregateTestWALObserver{dirtyKVs: map[string]bool{}, sizeKVs: map[string]int64{}, rotated: make(chan struct{}), tableEntry: make(chan struct{}), tableRelease: make(chan struct{}), tableDurable: make(chan struct{})}
}

// A hook observes physical writes, completed fsyncs and the exact table-create
// boundary; it never changes bytes or reports a configured worker as completed.
func (self *statsAggregateTestWALObserver) step(operation, name string) error {
	var size int64
	knownSize := false
	if operation == "after-write" && strings.HasSuffix(name, ".log") {
		self.stateLock.Lock()
		disk := self.disk
		self.stateLock.Unlock()
		if disk != nil {
			info, err := disk.root.Lstat(name)
			if err != nil {
				return err
			}
			size, knownSize = info.Size(), true
		}
	}
	self.stateLock.Lock()
	if operation == "aggregate-before-publish" && self.armed {
		self.publishing = true
	}
	if strings.HasSuffix(name, ".log") {
		switch operation {
		case "after-write":
			// journal.Reset/Close can issue a real zero-byte Write. Only
			// actual descriptor-relative file growth introduces dirty bytes.
			if knownSize {
				if size < self.sizeKVs[name] {
					self.stateLock.Unlock()
					return errors.New("observed aggregate journal unexpectedly shrank")
				}
				if size > self.sizeKVs[name] {
					self.dirtyKVs[name] = true
				} else if self.armed {
					self.emptyWrites++
				}
				self.sizeKVs[name] = size
			}
		case "after-file-sync":
			self.dirtyKVs[name] = false
			if self.armed {
				if self.publishing {
					self.headerSyncs++
				} else {
					self.rowSyncs++
				}
			}
		case "after-file-close":
			if self.armed && self.closedWAL == "" {
				self.closedWAL, self.closedDirty = name, self.dirtyKVs[name]
				close(self.rotated)
			}
		}
	}
	table := strings.HasSuffix(name, ".ldb") || strings.HasSuffix(name, ".sst")
	if self.armed && table && operation == "before-create" && !self.tableEntered {
		self.tableEntered = true
		close(self.tableEntry)
		self.stateLock.Unlock()
		<-self.tableRelease
		self.stateLock.Lock()
		self.tableExited = true
	}
	if self.armed && table && operation == "after-file-sync" && !self.tableSynced {
		self.tableSynced = true
		close(self.tableDurable)
	}
	self.stateLock.Unlock()
	return nil
}

// Arm only after the independent full signed replay has produced a valid base.
func (self *statsAggregateTestWALObserver) arm() {
	self.stateLock.Lock()
	self.armed = true
	self.publishing = false
	self.stateLock.Unlock()
}

// Cleanup and the intended interleaving share one idempotent barrier release.
func (self *statsAggregateTestWALObserver) release() {
	self.releaseOnce.Do(func() { close(self.tableRelease) })
}

// A low-level no-op generation owns the same admission/write gate as Replay.
// Its independent base has already passed the full real M8 public workflow.
type statsAggregateTestDurabilityStage struct {
	fixture *statsAggregateTestFixture
	stage   statsAggregateStage
	row     statsAggregateProvider
	ctx     context.Context
	finish  func()
}

// The rewritten row and every header field except generation remain identical
// to verified committed state. This tests persistence, not an importer API.
func newStatsAggregateTestDurabilityStage(t *testing.T, observer *statsAggregateTestWALObserver) *statsAggregateTestDurabilityStage {
	t.Helper()
	hooks := statsAggregateHooks{}
	if observer != nil {
		hooks.StorageStep = observer.step
	}
	fixture := newStatsAggregateTestFixture(t, 1, 0, hooks)
	if observer != nil {
		observer.stateLock.Lock()
		observer.disk = fixture.store.disk
		observer.stateLock.Unlock()
		t.Cleanup(observer.release)
	}
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
	rows := statsAggregateTestRows(t, fixture.store, head.Generation)
	if len(rows) != 7 {
		t.Fatalf("genuine M8 provider census = %d, want 7", len(rows))
	}
	operationCtx, done, err := fixture.store.begin(context.Background())
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
	return &statsAggregateTestDurabilityStage{fixture: fixture, stage: stage, row: rows[0], ctx: operationCtx, finish: finish}
}

// At most 20,000 fixed 279-byte genuine values fill the unchanged 4 MiB
// backend buffer. Only actual old-journal closure ends this bounded loop.
func (self *statsAggregateTestDurabilityStage) rotate(t *testing.T, observer *statsAggregateTestWALObserver) int {
	t.Helper()
	observer.arm()
	key, value := statsAggregateProviderKey(self.row.ClientID), self.row.encode()
	for count := 1; count <= 20000; count++ {
		if err := self.stage.putPoint(self.ctx, key, value); err != nil {
			t.Fatal(err)
		}
		select {
		case <-observer.rotated:
			<-observer.tableEntry
			if count < 1000 {
				t.Fatalf("rotation did not exercise the unchanged 4 MiB buffer: %d points", count)
			}
			return count
		default:
		}
	}
	t.Fatal("bounded genuine-row rewrites did not reach actual journal rotation")
	return 0
}

// The previous WAL can be closed while its frozen table is still blocked.
// A final-header fsync on the new WAL must not stand in for old-row durability.
func TestStatsAggregatePublicationWaitsForRotatedRowDurability(t *testing.T) {
	t.Parallel()
	observer := newStatsAggregateTestWALObserver()
	owner := newStatsAggregateTestDurabilityStage(t, observer)
	points := owner.rotate(t, observer)
	result, err := owner.stage.publish(owner.ctx)
	if err != nil || result != owner.stage.head {
		t.Fatalf("real persistence publication: %v", err)
	}
	visible, err := owner.fixture.store.Head(context.Background())
	if err != nil || visible != result {
		t.Fatalf("published header was not the observed generation: %v", err)
	}
	assertStatsAggregateTestParity(t, owner.fixture, visible)
	observer.stateLock.Lock()
	closedWAL, closedDirty, rowSyncs, headerSyncs := observer.closedWAL, observer.closedDirty, observer.rowSyncs, observer.headerSyncs
	entered, exited, synced := observer.tableEntered, observer.tableExited, observer.tableSynced
	observer.stateLock.Unlock()
	if closedWAL == "" || !entered || exited || synced || headerSyncs != 1 {
		t.Fatalf("causal WAL/table boundary not reached: wal=%q table=%t/%t/%t header_syncs=%d", closedWAL, entered, exited, synced, headerSyncs)
	}
	t.Logf("real rotated WAL %s after %d fixed rows; row_syncs=%d header_syncs=%d blocked_table=true", closedWAL, points, rowSyncs, headerSyncs)
	if closedDirty {
		t.Fatal("published aggregate header before rotated row WAL was durable")
	}
	if rowSyncs == 0 {
		t.Fatal("rotation had no completed staged-row sync")
	}
}

// Full public replay must encounter and propagate a staged-row fsync failure
// before reaching the header writer, including for a real failed terminal.
func TestStatsAggregateReplayRejectsStagedRowSyncFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("aggregate staged row sync sentinel")
	var armed, publishing atomic.Bool
	var attempts atomic.Int64
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if armed.Load() && operation == "aggregate-before-publish" {
			publishing.Store(true)
		}
		if armed.Load() && !publishing.Load() && operation == "before-file-sync" && strings.HasSuffix(name, ".log") {
			attempts.Add(1)
			return failure
		}
		return nil
	}})
	armed.Store(true)
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err == nil && publishing.Load() && result.Generation == 1 && attempts.Load() == 0 {
		t.Fatal("staged row Sync failure was skipped before header publication")
	}
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) || attempts.Load() != 1 || publishing.Load() {
		t.Fatalf("staged-row failure boundary: attempts=%d publishing=%t result=%+v error=%v", attempts.Load(), publishing.Load(), result, err)
	}
	rawHead, readErr := fixture.store.readHead(fixture.store.db)
	if readErr != nil || rawHead.Generation != 0 {
		t.Fatalf("row sync failure advanced the stored authority: %v", readErr)
	}
	if _, err := fixture.store.Head(context.Background()); err == nil {
		t.Fatal("row sync failure did not latch physical-store refusal")
	}
	if err := fixture.store.Close(); !errors.Is(err, failure) {
		t.Fatalf("Close forgot staged-row sync failure: %v", err)
	}
}

// The final header has its own actual fsync failure boundary. A failed return
// is not a rollback promise; the live owner faults rather than claiming success.
func TestStatsAggregateReplayRejectsHeaderSyncFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("aggregate final header sync sentinel")
	var armed, publishing atomic.Bool
	var attempts atomic.Int64
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if armed.Load() && operation == "aggregate-before-publish" {
			publishing.Store(true)
		}
		if armed.Load() && publishing.Load() && operation == "before-file-sync" && strings.HasSuffix(name, ".log") {
			attempts.Add(1)
			return failure
		}
		return nil
	}})
	beforeCloses := fixture.objects.closes
	armed.Store(true)
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) || attempts.Load() != 1 || !publishing.Load() || fixture.objects.closes <= beforeCloses {
		t.Fatalf("full-replay final header failure: attempts=%d publishing=%t result=%+v error=%v", attempts.Load(), publishing.Load(), result, err)
	}
	if _, err := fixture.store.Head(context.Background()); err == nil {
		t.Fatal("header sync failure left a readable successful owner")
	}
	if err := fixture.store.Close(); !errors.Is(err, failure) {
		t.Fatalf("Close forgot header sync failure: %v", err)
	}
}

// Reset/Close physically invokes Write with no new bytes after a completed
// flush. The causal observer must not mislabel that real call as dirty data.
func TestStatsAggregateWALObserverIgnoresEmptyPhysicalWrite(t *testing.T) {
	t.Parallel()
	observer := newStatsAggregateTestWALObserver()
	owner := newStatsAggregateTestDurabilityStage(t, observer)
	observer.arm()
	owner.finish()
	if err := owner.fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	observer.stateLock.Lock()
	closedWAL, closedDirty, emptyWrites := observer.closedWAL, observer.closedDirty, observer.emptyWrites
	observer.stateLock.Unlock()
	if closedWAL == "" || emptyWrites == 0 || closedDirty {
		t.Fatalf("actual empty journal Write was not distinguished from dirty bytes: wal=%q empty_writes=%d dirty=%t", closedWAL, emptyWrites, closedDirty)
	}
}

// Cancellation after actual rotation still leaves the prior header and exact
// provider census visible while the uncommitted table remains blocked.
func TestStatsAggregateRotatedStageCancellationPreventsPublication(t *testing.T) {
	t.Parallel()
	observer := newStatsAggregateTestWALObserver()
	owner := newStatsAggregateTestDurabilityStage(t, observer)
	owner.rotate(t, observer)
	ctx, cancel := context.WithCancel(owner.ctx)
	cancel()
	result, err := owner.stage.publish(ctx)
	if !errors.Is(err, context.Canceled) || result != (statsAggregateHead{}) {
		t.Fatalf("canceled rotated generation published: result=%+v error=%v", result, err)
	}
	head, err := owner.fixture.store.Head(context.Background())
	if err != nil || head != owner.stage.old {
		t.Fatalf("canceled rotated generation replaced prior authority: %v", err)
	}
	assertStatsAggregateTestParity(t, owner.fixture, head)
	observer.stateLock.Lock()
	headerSyncs, tableExited := observer.headerSyncs, observer.tableExited
	observer.stateLock.Unlock()
	if headerSyncs != 0 || tableExited {
		t.Fatal("cancellation missed the blocked-table/pre-header boundary")
	}
}

// Close joins the actual backend compaction owner, not only admitted callers.
// The table callback must have returned before Close can release the database.
func TestStatsAggregateCloseJoinsBlockedTableCreation(t *testing.T) {
	t.Parallel()
	observer := newStatsAggregateTestWALObserver()
	owner := newStatsAggregateTestDurabilityStage(t, observer)
	owner.rotate(t, observer)
	owner.finish()
	// Capture callback completion at Close return, not at a later observation.
	type closeResult struct {
		err         error
		tableExited bool
	}
	closed := make(chan closeResult, 1)
	go func() {
		err := owner.fixture.store.Close()
		observer.stateLock.Lock()
		tableExited := observer.tableExited
		observer.stateLock.Unlock()
		closed <- closeResult{err: err, tableExited: tableExited}
	}()
	<-owner.fixture.store.ctx.Done()
	select {
	case result := <-closed:
		t.Fatalf("Close returned with table creation still blocked: %+v", result)
	default:
	}
	observer.release()
	result := <-closed
	if result.err != nil || !result.tableExited {
		t.Fatalf("Close did not join real compaction callback: %+v", result)
	}
	if _, err := owner.fixture.store.Head(context.Background()); err == nil {
		t.Fatal("closed rotated store remained readable")
	}
}

// If the old frozen table genuinely completed its fsync before publication,
// the same no-op generation preserves every canonical value after reopening.
// Reopen is a semantic control, not the crash-durability proof above.
func TestStatsAggregateCompactedRotationReopensExactCensus(t *testing.T) {
	t.Parallel()
	observer := newStatsAggregateTestWALObserver()
	owner := newStatsAggregateTestDurabilityStage(t, observer)
	owner.rotate(t, observer)
	observer.release()
	<-observer.tableDurable
	result, err := owner.stage.publish(owner.ctx)
	if err != nil || result != owner.stage.head {
		t.Fatalf("already-compacted generation publication: %v", err)
	}
	owner.finish()
	if err := owner.fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	fixture := owner.fixture
	reopened, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	fixture.store = reopened
	head, err := reopened.Head(context.Background())
	if err != nil || head != result {
		t.Fatalf("reopened committed generation differs: %v", err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// A point writer must own both its key and value even if future bounded
// staging delays the physical write until another point or publication.
func TestStatsAggregateNoOpStageOwnsCanonicalProviderBytes(t *testing.T) {
	t.Parallel()
	owner := newStatsAggregateTestDurabilityStage(t, nil)
	key, value := statsAggregateProviderKey(owner.row.ClientID), owner.row.encode()
	wantedKey, wantedValue := bytes.Clone(key), bytes.Clone(value)
	if err := owner.stage.putPoint(owner.ctx, key, value); err != nil {
		t.Fatal(err)
	}
	for index := range key {
		key[index] ^= 0xff
	}
	for index := range value {
		value[index] ^= 0xff
	}
	result, err := owner.stage.publish(owner.ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, exists, err := statsAggregatePoint(owner.fixture.store.db, wantedKey, result.Generation)
	if err != nil || !exists || !bytes.Equal(stored, wantedValue) {
		t.Fatalf("owned point bytes changed after admission: %v", err)
	}
	assertStatsAggregateTestParity(t, owner.fixture, result)
}

// An already-canceled point cannot write a row, emit a point observation or
// change the generation. This uses the actual stage method used by Replay.
func TestStatsAggregateCanceledStageDoesNotTouchPhysicalRows(t *testing.T) {
	t.Parallel()
	owner := newStatsAggregateTestDurabilityStage(t, nil)
	ctx, cancel := context.WithCancel(owner.ctx)
	cancel()
	key := statsAggregateProviderKey(owner.row.ClientID)
	if err := owner.stage.putPoint(ctx, key, owner.row.encode()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled point returned %v", err)
	}
	raw, err := owner.fixture.store.db.Get(statsAggregateVersionKey(key, owner.stage.head.Generation), nil)
	if !errors.Is(err, leveldb.ErrNotFound) || raw != nil {
		t.Fatalf("canceled stage changed its absent physical row: length=%d error=%v", len(raw), err)
	}
	head, err := owner.fixture.store.Head(context.Background())
	if err != nil || head != owner.stage.old {
		t.Fatalf("canceled point changed committed authority: %v", err)
	}
}
