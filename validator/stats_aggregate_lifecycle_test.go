//go:build linux || darwin

package validator

// Late stream failures and cancellation leave only invisible staged rows.
// Real storage restart, reader ownership and fixed-width writes are observed
// directly; no test replaces signature verification or public replay.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/syndtr/goleveldb/leveldb/opt"
)

// A transport wrapper preserves actual read bytes and actual Close first.
type statsAggregateTestCloseReader struct {
	io.ReadCloser
	closed func() error
}

// Real reader closure is never skipped by an injected adjacent close error.
func (self statsAggregateTestCloseReader) Close() error {
	err := self.ReadCloser.Close()
	if self.closed != nil {
		err = errors.Join(err, self.closed())
	}
	return err
}

// A proof-opening failure occurs after every record has reached the private
// stage. Only old committed rows remain visible, and replay requires recovery.
func TestStatsAggregateLateProofFailureDoesNotPublish(t *testing.T) {
	t.Parallel()
	pointWrites := 0
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{PointStaged: func(byte, int, int) error { pointWrites++; return nil }})
	options := fixture.replayOptions(t)
	open := options.OpenData
	failure := errors.New("late aggregate proof source failure")
	options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		if kind == AttemptStreamV2Proofs {
			return nil, failure
		}
		return open(ctx, kind, hash, size)
	}
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{})
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) || pointWrites == 0 {
		t.Fatalf("late proof failure did not reach real staged updates: head=%+v writes=%d error=%v", result, pointWrites, err)
	}
	head, err := fixture.store.Head(context.Background())
	if err != nil || head.Generation != 0 || head.ProviderCount != 0 || len(statsAggregateTestRows(t, fixture.store, 0)) != 0 {
		t.Fatalf("late proof failure exposed a staged provider: %v", err)
	}
	if _, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{}); err == nil || !strings.Contains(err.Error(), "requires reopen") {
		t.Fatalf("failed stage was silently reused: %v", err)
	}
}

// The same failure is recovered by closing/reopening the real backend. Recovery
// uses bounded point deletion; no failed generation becomes a hidden parent.
func TestStatsAggregateFailedGenerationRecoveryReplaysExactCensus(t *testing.T) {
	t.Parallel()
	failure := errors.New("aggregate precommit interruption")
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{BeforeCommit: func(statsAggregateHead) error { return failure }})
	if _, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{}); !errors.Is(err, failure) {
		t.Fatalf("precommit interruption: %v", err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture.store = store
	head, err := store.Head(context.Background())
	if err != nil || head.Generation != 0 || len(statsAggregateTestRows(t, store, 0)) != 0 {
		t.Fatalf("failed generation survived recovery: %v", err)
	}
	iterator := store.db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll})
	rows := 0
	for iterator.Next() {
		rows++
	}
	scanErr := iterator.Error()
	iterator.Release()
	if scanErr != nil || rows != 1 {
		t.Fatalf("recovery left %d logical rows, want only header: %v", rows, scanErr)
	}
	head, err = store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// A real proof reader can fail on Close only after its bytes and EOF succeeded;
// that failure still precedes every visible aggregate header mutation.
func TestStatsAggregateProofReaderCloseFailureDoesNotPublish(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	options := fixture.replayOptions(t)
	open := options.OpenData
	failure := errors.New("aggregate proof reader close failure")
	proofCloses := 0
	options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return statsAggregateTestCloseReader{ReadCloser: reader, closed: func() error { proofCloses++; return failure }}, nil
	}
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{})
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) || proofCloses != 1 {
		t.Fatalf("proof close failure=%v, closes=%d, head=%+v", err, proofCloses, result)
	}
	head, err := fixture.store.Head(context.Background())
	if err != nil || head.Generation != 0 || head.ProviderCount != 0 {
		t.Fatalf("proof close failure published partial state: %v", err)
	}
}

// Cancellation at the actual post-replay/precommit boundary cannot publish
// even though all records, proof bytes and reader closes already succeeded.
func TestStatsAggregatePrecommitCancellationPreservesVisibleGeneration(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var fixture *statsAggregateTestFixture
	observed := false
	fixture = newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{BeforeCommit: func(candidate statsAggregateHead) error {
		old, err := fixture.store.Head(context.Background())
		if err != nil || old.Generation != 0 || candidate.ProviderCount == 0 || fixture.objects.closes == 0 {
			return errors.New("precommit hook missed full replay or old-generation visibility")
		}
		observed = true
		cancel()
		return nil
	}})
	result, err := fixture.store.Replay(ctx, fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if !errors.Is(err, context.Canceled) || !observed || result != (statsAggregateHead{}) {
		t.Fatalf("precommit cancellation=%v observed=%t head=%+v", err, observed, result)
	}
	head, err := fixture.store.Head(context.Background())
	if err != nil || head.Generation != 0 || head.ProviderCount != 0 {
		t.Fatalf("canceled stage was published: %v", err)
	}
}

// Already-canceled callers perform neither object I/O nor scratch creation.
func TestStatsAggregateCanceledAdmissionDoesNotMutateScratch(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options := fixture.replayOptions(t)
	reads := fixture.objects.reads
	if _, err := fixture.store.Replay(ctx, fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled admission: %v", err)
	}
	if fixture.objects.reads != reads {
		t.Fatal("canceled caller fetched public objects")
	}
	if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled caller created scratch: %v", err)
	}
}

// Actual data reads wait on the operation context. Store closure must cancel
// the read and join its real Close before releasing the aggregate backend.
type statsAggregateTestCancelReader struct {
	ctx     context.Context
	entered chan struct{}
	closed  *atomic.Int64
}

// A single attempted read exposes the real public chunk-reading boundary.
func (self *statsAggregateTestCancelReader) Read([]byte) (int, error) {
	select {
	case self.entered <- struct{}{}:
	default:
	}
	<-self.ctx.Done()
	return 0, self.ctx.Err()
}

// Resource completion is recorded before replay can release its active owner.
func (self *statsAggregateTestCancelReader) Close() error {
	self.closed.Add(1)
	return nil
}

// No external reader survives Close, and no canceled reader publishes a head.
func TestStatsAggregateCloseCancelsAndJoinsPublicReader(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	options := fixture.replayOptions(t)
	entered := make(chan struct{}, 1)
	var closed atomic.Int64
	options.OpenData = func(ctx context.Context, _, _ string, _ uint64) (io.ReadCloser, error) {
		return &statsAggregateTestCancelReader{ctx: ctx, entered: entered, closed: &closed}, nil
	}
	replayDone := make(chan error, 1)
	go func() {
		_, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{})
		replayDone <- err
	}()
	<-entered
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 1 {
		t.Fatal("aggregate Close returned before its public reader closed")
	}
	if err := <-replayDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("closed replay returned %v", err)
	}
	if _, err := fixture.store.Head(context.Background()); err == nil {
		t.Fatal("closed aggregate continued serving state")
	}
}

// A physical write fault latches the live store and releases its owner only
// through Close. Reopen accepts either a complete committed header or the old
// one, never a partial row census; this failure precedes the header write.
func TestStatsAggregateStorageFaultCannotPublishPartialCounters(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	failure := errors.New("aggregate real storage write failure")
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{StorageStep: func(operation, _ string) error {
		if operation == "before-write" && armed.Load() {
			return failure
		}
		return nil
	}})
	armed.Store(true)
	result, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if !errors.Is(err, failure) || result != (statsAggregateHead{}) {
		t.Fatalf("physical point write failure=%v head=%+v", err, result)
	}
	if _, err := fixture.store.Head(context.Background()); err == nil {
		t.Fatal("physically faulted aggregate remained readable")
	}
	if err := fixture.store.Close(); !errors.Is(err, failure) {
		t.Fatalf("close forgot the latched physical fault: %v", err)
	}
	reopened, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	head, err := reopened.Head(context.Background())
	if err != nil || head.Generation != 0 || head.ProviderCount != 0 {
		t.Fatalf("failed physical write reopened partial counters: %v", err)
	}
}

// Replacing the owned directory cannot redirect a live aggregate into a new
// namespace, even when both directory names have otherwise private modes.
func TestStatsAggregateDirectoryReplacementRefusesBeforeReplay(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	preserved := fixture.path + "-preserved"
	if err := os.Rename(fixture.path, preserved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fixture.path, 0o700); err != nil {
		t.Fatal(err)
	}
	options := fixture.replayOptions(t)
	reads := fixture.objects.reads
	if _, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{}); err == nil || !strings.Contains(err.Error(), "anchor changed") {
		t.Fatalf("replaced directory entered replay: %v", err)
	}
	if fixture.objects.reads != reads {
		t.Fatal("replaced aggregate reached public I/O")
	}
	if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replaced aggregate created scratch: %v", err)
	}
	entries, err := os.ReadDir(fixture.path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("replacement namespace was mutated: %v", err)
	}
}

// Every retired version is pruned before a new stage, so point lookup and
// physical row census never form an ever-growing ancestry chain.
func TestStatsAggregateRepeatedGenerationsRetainBoundedVersionDepth(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	var head statsAggregateHead
	for index := 0; index < 5; index++ {
		var err error
		head, err = fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
		if err != nil {
			t.Fatal(err)
		}
	}
	cursor := fixture.store.db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll})
	rows := 0
	for cursor.Next() {
		rows++
	}
	err := cursor.Error()
	cursor.Release()
	want := 1 + int(head.ProviderCount+head.EgressHashCount+head.EgressClaimCount)
	if err != nil || rows != want {
		t.Fatalf("generation ancestry retained %d rows, want %d: %v", rows, want, err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// Logical resource refusal retains every prior identity; it cannot publish a
// truncated provider census or silently replace the caller's explicit limit.
func TestStatsAggregateProviderBoundRejectsWholeGeneration(t *testing.T) {
	t.Parallel()
	source := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	seal, _ := newAttemptCutV2SealTestOptions(t, source)
	cut, _, err := SealAttemptCutV2(context.Background(), source.ledger, source.expected, source.policy, source.key, source.bounds, seal)
	if err != nil {
		t.Fatal(err)
	}
	bounds := statsAggregateTestBounds()
	bounds.MaxProviders = 6
	pointWrites := 0
	store, err := openStatsAggregateStore(context.Background(), filepath.Join(newAttemptLedgerDiskTestStateDir(t), "aggregate"), source.expected, source.policy, source.engine.stats.cfg, bounds, statsAggregateHooks{PointStaged: func(kind byte, _, _ int) error {
		if kind == 'p' {
			pointWrites++
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	options := AttemptCutV2ReplayOptions{Bounds: source.replay, ScratchDirectory: filepath.Join(t.TempDir(), "replay"), ServerKeys: seal.ServerKeys, ReadMetadata: seal.ReadMetadata, OpenData: seal.OpenData}
	if _, err := store.Replay(context.Background(), *cut, source.expected, source.policy, source.bounds, options, statsAggregateAdvance{}); err == nil || !strings.Contains(err.Error(), "provider count") || pointWrites != 6 {
		t.Fatalf("bounded complete-census refusal: writes=%d error=%v", pointWrites, err)
	}
	head, err := store.Head(context.Background())
	if err != nil || head.ProviderCount != 0 || head.Generation != 0 {
		t.Fatalf("resource refusal published truncated providers: %v", err)
	}
}

// Integer overflow is observed at the real terminal point-update helper using
// an independently verified M8 terminal and a fixed-size maximum counter row.
// This is arithmetic fault injection, not a proof or historical-import fixture.
func TestStatsAggregateTerminalPointUpdateRejectsCounterOverflow(t *testing.T) {
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
	row := statsAggregateProvider{ClientID: terminal.Assignments[0].NextHop, WindowPresent: true, Window: ProviderWindow{Assignments: ^uint64(0)}}
	if err := row.validate(head.Config); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.db.Put(statsAggregateVersionKey(statsAggregateProviderKey(row.ClientID), 1), row.encode(), nil); err != nil {
		t.Fatal(err)
	}
	stage := statsAggregateStage{store: fixture.store, old: head, head: head, matchedPrefix: true}
	stage.head.Generation = 1
	before := bytes.Clone(row.encode())
	if err := stage.record(context.Background(), terminal); err == nil || !strings.Contains(err.Error(), "counter overflows") {
		t.Fatalf("overflowing terminal point update: %v", err)
	}
	after, exists, err := statsAggregatePoint(fixture.store.db, statsAggregateProviderKey(row.ClientID), 1)
	if err != nil || !exists || !bytes.Equal(before, after) {
		t.Fatalf("overflow changed its fixed-size row: %v", err)
	}
	visible, err := fixture.store.Head(context.Background())
	if err != nil || !reflect.DeepEqual(head, visible) {
		t.Fatalf("overflow fault published a header: %v", err)
	}
}
