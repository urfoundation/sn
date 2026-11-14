//go:build linux || darwin

package validator

// Batches expose only independently owned authenticated copies. Explicit
// decoder barriers force cancellation, close and custody loss before release.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Both resource dimensions stop a batch, without imposing a new record limit.
func TestAttemptRecordStoreBatchBoundsAndOwnedCopies(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs)
	first, err := json.Marshal(fixture.recordTs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, limits := range []struct {
		count, bytes uint64
		want         int
	}{{3, 4 * 1024 * 1024, 3}, {16, uint64(len(first)), 1}, {16, 1, 1}} {
		recordTs, err := store.readBatch(t.Context(), 1, 16, limits.count, limits.bytes)
		if err != nil || len(recordTs) != limits.want || !reflect.DeepEqual(recordTs, fixture.recordTs[:limits.want]) {
			t.Fatalf("bounded owned copies: count=%d bytes=%d records=%d: %v", limits.count, limits.bytes, len(recordTs), err)
		}
		recordTs[0].VPK[0] ^= 1
		if len(recordTs) > 1 && !reflect.DeepEqual(recordTs[1], fixture.recordTs[1]) {
			t.Fatal("records in a batch share mutable buffers")
		}
		fresh, err := store.Read(t.Context(), 1)
		if err != nil || !reflect.DeepEqual(fresh, fixture.recordTs[0]) {
			t.Fatalf("batch mutation changed the disk record: %v", err)
		}
	}
	for _, bounds := range [][4]uint64{{0, 16, 1, 1}, {2, 1, 1, 1}, {1, 17, 1, 1}, {1, 16, 0, 1}, {1, 16, 1, 0}} {
		if recordTs, err := store.readBatch(t.Context(), bounds[0], bounds[1], bounds[2], bounds[3]); err == nil || recordTs != nil {
			t.Fatalf("invalid batch bounds returned data: %+v: %v", bounds, err)
		}
	}
}

// A later corrupt record clears the entire batch; no valid-looking prefix is
// returned after failed authentication, and the normal store fault latches.
func TestAttemptRecordStoreBatchRejectsCorruptionBeforeRelease(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs)
	record, err := cloneAttemptRecord(fixture.recordTs[15])
	if err != nil {
		t.Fatal(err)
	}
	record.Signature[0] ^= 1
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Put(attemptStoreRecordKey(16), raw, nil); err != nil {
		t.Fatal(err)
	}
	recordTs, err := store.readBatch(t.Context(), 1, 16, 16, 4*1024*1024)
	if recordTs != nil || !errors.Is(err, errAttemptRecordStoreFaulted) {
		t.Fatalf("bad signature released a partial batch: %d: %v", len(recordTs), err)
	}
	if _, err := store.Read(t.Context(), 1); !errors.Is(err, errAttemptRecordStoreFaulted) {
		t.Fatalf("batch authentication did not retain the store fault: %v", err)
	}
}

// Loss after the last record has decoded still refuses all copied bytes at
// the closing physical observation, rather than caching an opening verdict.
func TestAttemptRecordStoreBatchRejectsClosingCustodyReplacement(t *testing.T) {
	fixture, ctx, host := newAttemptSealBatchFixture(t, 2)
	store := fixture.ledger.disk.(*attemptRecordStore)
	decoded := 0
	store.disk.hooks.Step = func(operation, _ string) error {
		if operation == "decode-record" {
			decoded++
			if decoded == 2 {
				host.Host.(*durablefixture.Host).ReplaceMount()
			}
		}
		return nil
	}
	recordTs, err := store.readBatch(ctx, 1, 2, 2, 4*1024*1024)
	if decoded != 2 || recordTs != nil || !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("closing generation change released data: decoded=%d records=%d: %v", decoded, len(recordTs), err)
	}
}

// Cancellation and Close join a batch while it owns the backend. Neither
// returns a partial batch, and caller cancellation leaves healthy siblings.
func TestAttemptRecordStoreBatchCancellationAndCloseJoin(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	for _, closeStore := range []bool{false, true} {
		store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
		appendAttemptRecordStoreTest(t, store, fixture.recordTs)
		entered, release := make(chan struct{}), make(chan struct{})
		var first atomic.Bool
		store.disk.hooks.Step = func(operation, _ string) error {
			if operation == "decode-record" && first.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			recordTs, err := store.readBatch(ctx, 1, 16, 16, 4*1024*1024)
			if recordTs != nil {
				err = errors.Join(err, errors.New("canceled batch returned records"))
			}
			done <- err
		}()
		releaseForegroundWait(t, entered, "batch decoder entry")
		var closed chan error
		if closeStore {
			closed = make(chan error, 1)
			go func() { closed <- store.Close() }()
			releaseForegroundWait(t, store.ctx.Done(), "batch store close admission")
		} else {
			cancel()
		}
		close(release)
		err := <-done
		cancel()
		if closeStore {
			if !errors.Is(err, errAttemptRecordStoreClosed) || <-closed != nil {
				t.Fatalf("batch close did not join/refuse: %v", err)
			}
		} else {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("batch caller cancellation changed: %v", err)
			}
			if _, err := store.Read(t.Context(), 1); err != nil || t.Context().Err() != nil {
				t.Fatalf("child cancellation poisoned a healthy sibling: %v", err)
			}
		}
	}
}

// Once copies have returned, closing storage is synchronous and cannot wait
// for the consumer; retained copies still own their original signed bytes.
func TestAttemptRecordStoreBatchReleasesStorageBeforeConsumer(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs)
	recordTs, err := store.readBatch(t.Context(), 1, 16, 16, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil || !reflect.DeepEqual(recordTs, fixture.recordTs) {
		t.Fatalf("released immutable copies changed or retained a storage owner: %v", err)
	}
}
