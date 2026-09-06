//go:build linux || darwin

package validator

// Admission lifecycle checks use genuine full M8 state and retained physical
// snapshots. Test hooks force real boundaries; timeouts are not the witness.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Missing LOCK does not grant permission to repair another signed namespace.
func TestStatsAggregateAdmissionMissingLockRefusesWithoutCreation(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	if err := os.Remove(filepath.Join(fixture.path, "LOCK")); err != nil {
		t.Fatal(err)
	}
	expected := fixture.source.expected
	expected.Identity.NoID++
	if observeStatsAggregateOpenTestRefusal(t, fixture, "missing-lock", expected, fixture.config) {
		t.Fatal("wrong namespace recreated LOCK or performed writable recovery")
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// Cancellation at each real read/promotion checkpoint precedes writable
// backend recovery, even after a complete physical metadata inspection.
func TestStatsAggregateAdmissionCancellationAtReadAndPromotion(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	for _, point := range []string{"admission-census", "admission-read-check", "admission-after-close", "admission-before-promote"} {
		before := snapshotStatsAggregateOpenTestStore(t, fixture)
		ctx, cancel := context.WithCancel(context.Background())
		observer := &statsAggregateOpenTestObserver{}
		var reached atomic.Bool
		store, err := openStatsAggregateStore(ctx, fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(operation, name string) error {
			if operation == point && reached.CompareAndSwap(false, true) {
				cancel()
			}
			return observer.step(operation, name)
		}})
		cancel()
		if store != nil {
			_ = store.Close()
		}
		if store != nil || !errors.Is(err, context.Canceled) || !reached.Load() {
			t.Fatalf("checkpoint %s missed exact cancellation: reached=%t err=%v", point, reached.Load(), err)
		}
		if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
			t.Fatalf("canceled %s mutated existing state: %v / %v", point, changes, observer.snapshot())
		}
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// The final inspection callback may cancel after every byte has validated.
func TestStatsAggregateAdmissionCheckedCancellationPrecedesNativeOpen(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer := &statsAggregateOpenTestObserver{}
	checked := false
	store, err := openStatsAggregateStore(ctx, fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step, AdmissionChecked: func(report statsAggregateAdmissionReport) error {
		checked = true
		cancel()
		return nil
	}})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || !checked || !errors.Is(err, context.Canceled) {
		t.Fatalf("fully checked canceled admission escaped: checked=%t err=%v", checked, err)
	}
	if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
		t.Fatalf("post-inspection cancellation mutated files: %v / %v", changes, observer.snapshot())
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// A read-side authority failure and a late close failure are independent.
// Neither may disappear into an otherwise valid authority result.
func TestStatsAggregateAdmissionJoinsAuthorityAndCloseFailures(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	closeFailure := errors.New("admission close sentinel")
	observer := &statsAggregateOpenTestObserver{}
	var closed atomic.Int64
	expected := fixture.source.expected
	expected.Identity.NoID++
	store, err := openStatsAggregateStore(context.Background(), fixture.path, expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if strings.HasSuffix(name, ".log") && operation == "admission-after-close" {
			closed.Add(1)
			return closeFailure
		}
		return observer.step(operation, name)
	}})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || !errors.Is(err, closeFailure) || !strings.Contains(err.Error(), "existing namespace or config differs") || closed.Load() != 1 {
		t.Fatalf("read/close outcomes were lost: closes=%d err=%v", closed.Load(), err)
	}
	if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
		t.Fatalf("failed close admitted writes: %v / %v", changes, observer.snapshot())
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// Same-inode/same-size rewrite plus restored mtime still changes native ctime.
// The final retained-descriptor census must catch it before any writer starts.
func TestStatsAggregateAdmissionRejectsRestoredSameInodeRewrite(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	observer := &statsAggregateOpenTestObserver{}
	changed := false
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step, AdmissionChecked: func(statsAggregateAdmissionReport) error {
		path := filepath.Join(fixture.path, "CURRENT")
		before, err := os.Stat(path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		n, writeErr := file.WriteAt(raw, 0)
		err = errors.Join(writeErr, file.Close(), os.Chtimes(path, before.ModTime(), before.ModTime()))
		after, statErr := os.Stat(path)
		if err != nil || statErr != nil || n != len(raw) || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || attemptLedgerSameFileState(before, after) {
			return errors.Join(errors.New("rewrite fixture did not establish changed native write state"), err, statErr)
		}
		changed = true
		return nil
	}})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || !changed || err == nil || !strings.Contains(err.Error(), "inspected file changed before promotion") || len(observer.snapshot()) != 0 {
		t.Fatalf("restored rewrite admitted recovery: changed=%t events=%v err=%v", changed, observer.snapshot(), err)
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// Two upgraded owners contend on the actual directory inode throughout
// inspection and promotion; a second owner cannot rewrite the first's WAL.
func TestStatsAggregateAdmissionRetainsGateThroughPromotion(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	reached, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finishRelease := func() { releaseOnce.Do(func() { close(release) }) }
	type outcome struct {
		store *statsAggregateStore
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		store, err := openStatsAggregateStore(ctx, fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{AdmissionChecked: func(statsAggregateAdmissionReport) error {
			close(reached)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}})
		done <- outcome{store: store, err: err}
	}()
	var result outcome
	joined := false
	t.Cleanup(func() {
		cancel()
		finishRelease()
		if !joined {
			result = <-done
		}
		if result.store != nil {
			_ = result.store.Close()
		}
	})
	select {
	case <-reached:
	case result = <-done:
		joined = true
		t.Fatalf("first owner missed inspection barrier: %v", result.err)
	}
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	observer := &statsAggregateOpenTestObserver{}
	second, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step})
	if second != nil {
		_ = second.Close()
	}
	if second != nil || err == nil {
		t.Fatal("second owner bypassed the retained inspection gate")
	}
	if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
		t.Fatalf("competing owner mutated admitted files: %v / %v", changes, observer.snapshot())
	}
	finishRelease()
	result = <-done
	joined = true
	if result.err != nil || result.store == nil {
		t.Fatalf("first retained owner did not promote: %v", result.err)
	}
	fixture.store = result.store
	actual, err := result.store.Head(context.Background())
	if err != nil || actual != head {
		t.Fatalf("promoted head differs: %v", err)
	}
	assertStatsAggregateTestParity(t, fixture, actual)
}

// Renaming the checked inode cannot redirect promotion into a fresh path.
// Both original bytes and the empty replacement remain free of backend writes.
func TestStatsAggregateAdmissionRejectsDirectoryReplacement(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	original := fixture.path
	retained := original + "-retained"
	observer := &statsAggregateOpenTestObserver{}
	changed := false
	store, err := openStatsAggregateStore(context.Background(), original, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step, AdmissionChecked: func(statsAggregateAdmissionReport) error {
		if err := os.Rename(original, retained); err != nil {
			return err
		}
		changed = true
		return os.Mkdir(original, 0o700)
	}})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || !changed || err == nil || !strings.Contains(err.Error(), "directory anchor changed") || len(observer.snapshot()) != 0 {
		t.Fatalf("replacement admitted backend writes: changed=%t events=%v err=%v", changed, observer.snapshot(), err)
	}
	entries, err := os.ReadDir(original)
	if err != nil || len(entries) != 0 {
		t.Fatalf("replacement path gained backend files: count=%d err=%v", len(entries), err)
	}
	fixture.path = retained
	// Rename may change the directory's own metadata. File bytes/identities are
	// checked independently; no production write is allowed to either inode.
	after := snapshotStatsAggregateOpenTestStore(t, fixture)
	if !os.SameFile(before.directory, after.directory) || before.directory.Mode() != after.directory.Mode() {
		t.Fatal("retained original directory identity or mode changed")
	}
	before.directory = after.directory
	if changes := before.changes(after); len(changes) != 0 {
		t.Fatalf("retained original file authority was modified: %v", changes)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, original); err != nil {
		t.Fatal(err)
	}
	fixture.path = original
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// Opening cancellation stops at successful publication, not at the lifetime
// of the returned real database or its next full M8 replay operation.
func TestStatsAggregateAdmissionCallerContextDoesNotOwnReturnedStore(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := openStatsAggregateStore(ctx, fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture.store = store
	cancel()
	got, err := store.Head(context.Background())
	if err != nil || got != head {
		t.Fatalf("returned store retained opening cancellation: %v", err)
	}
	next, err := store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil || next.Generation != head.Generation+1 {
		t.Fatalf("returned store lost its independently owned lifetime: %v", err)
	}
	assertStatsAggregateTestParity(t, fixture, next)
}

// Abnormal callback exit cannot bypass the constructor's private owner
// cleanup. The goroutine is explicitly joined before a real matching reopen.
func TestStatsAggregateAdmissionGoexitReleasesUnpublishedOwner(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	var reached, returned atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		store, _ := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{AdmissionChecked: func(statsAggregateAdmissionReport) error {
			reached.Store(true)
			runtime.Goexit()
			return nil
		}})
		returned.Store(true)
		if store != nil {
			_ = store.Close()
		}
	}()
	<-done
	if !reached.Load() || returned.Load() {
		t.Fatalf("abnormal exit did not reach admission callback: reached=%t returned=%t", reached.Load(), returned.Load())
	}
	if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 {
		t.Fatalf("abnormally exited admission mutated its namespace: %v", changes)
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}
