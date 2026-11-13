//go:build linux || darwin

package validator

// A production start verified every attempt record in full again, so starts
// grew with the lifetime ledger. Under a startup verified owner, a later start
// authenticates only records after the remembered byte-exact prefix, and still
// refuses every change the full verification refuses.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

func openAttemptRecordStorePrefixTest(t *testing.T, ctx context.Context, path string, fixture attemptRecordStoreTestFixture) (*attemptRecordStore, error) {
	t.Helper()
	return openAttemptRecordStoreWithHooks(ctx, path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
}

// Opens, reports how many records the open authenticated in full, and closes.
func authenticatedAttemptRecordStoreOpen(t *testing.T, ctx context.Context, path string, fixture attemptRecordStoreTestFixture, appendRecords []AttemptRecord) uint64 {
	t.Helper()
	store, err := openAttemptRecordStorePrefixTest(t, ctx, path, fixture)
	if err != nil {
		t.Fatal(err)
	}
	authenticated := store.recordsAuthenticated
	appendAttemptRecordStoreTest(t, store, appendRecords)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return authenticated
}

func TestAttemptRecordStoreStartupAuthenticatesOnlyRecordsAfterARememberedPrefix(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 3)
	path := filepath.Join(t.TempDir(), "store")
	authenticatedAttemptRecordStoreOpen(t, t.Context(), path, fixture, fixture.recordTs[:16])
	owner := newReleaseStartupVerified()
	ctx := withReleaseStartupVerified(t.Context(), owner)
	steps := []struct {
		name   string
		ctx    context.Context
		want   uint64
		append []AttemptRecord
	}{
		{name: "first start, nothing remembered", ctx: ctx, want: 16},
		{name: "second start", ctx: ctx, want: 0},
		{name: "a start without an owner", ctx: t.Context(), want: 16, append: fixture.recordTs[16:]},
		{name: "after eight more records", ctx: ctx, want: 8},
		{name: "after those passed too", ctx: ctx, want: 0},
	}
	for _, step := range steps {
		if got := authenticatedAttemptRecordStoreOpen(t, step.ctx, path, fixture, step.append); got != step.want {
			t.Fatalf("%s: authenticated %d records in full, want %d", step.name, got, step.want)
		}
	}
}

// Each change is refused with and without a remembered prefix: changed bytes
// inside the prefix are authenticated in full again, and every structural
// check still runs.
func TestAttemptRecordStoreRememberedPrefixStillRefusesChanges(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	changes := []struct {
		name   string
		change func(*testing.T, *leveldb.DB, *leveldb.Batch)
	}{
		{name: "signature bytes inside the prefix", change: func(t *testing.T, db *leveldb.DB, batch *leveldb.Batch) {
			raw, err := db.Get(attemptStoreRecordKey(3), nil)
			if err != nil {
				t.Fatal(err)
			}
			var record AttemptRecord
			if err := json.Unmarshal(raw, &record); err != nil || len(record.Signature) == 0 {
				t.Fatalf("record 3: %v", err)
			}
			record.Signature[0] ^= 0xff
			changed, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			batch.Put(attemptStoreRecordKey(3), changed)
		}},
		{name: "marker hash inside the prefix", change: func(t *testing.T, _ *leveldb.DB, batch *leveldb.Batch) {
			batch.Put(attemptStoreTrailRecordKey(fixture.recordTs[0].TrailID, 1), []byte(zeroAttemptHash()))
		}},
		{name: "final trail state", change: func(t *testing.T, _ *leveldb.DB, batch *leveldb.Batch) {
			batch.Delete(attemptStoreTrailKey(fixture.recordTs[0].TrailID))
		}},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "store")
			authenticatedAttemptRecordStoreOpen(t, t.Context(), path, fixture, fixture.recordTs)
			owner := newReleaseStartupVerified()
			ctx := withReleaseStartupVerified(t.Context(), owner)
			if got := authenticatedAttemptRecordStoreOpen(t, ctx, path, fixture, nil); got != uint64(len(fixture.recordTs)) {
				t.Fatalf("first start authenticated %d records", got)
			}
			if got := authenticatedAttemptRecordStoreOpen(t, ctx, path, fixture, nil); got != 0 {
				t.Fatalf("the prefix was not remembered: %d", got)
			}
			disk, err := openAttemptRecordStoreStorage(path, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{}, func(error) {})
			if err != nil {
				t.Fatal(err)
			}
			db, err := leveldb.Open(disk, &opt.Options{Strict: opt.StrictAll})
			if err != nil {
				t.Fatal(err)
			}
			batch := new(leveldb.Batch)
			change.change(t, db, batch)
			if err := db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			for _, opened := range []context.Context{ctx, t.Context()} {
				store, err := openAttemptRecordStorePrefixTest(t, opened, path, fixture)
				if err == nil {
					_ = store.Close()
					t.Fatalf("a changed store opened (owner=%t)", opened == ctx)
				}
			}
		})
	}
}
