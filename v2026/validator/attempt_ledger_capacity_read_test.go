//go:build linux || darwin

package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb/opt"
)

func TestStoppedAttemptLedgerCapacityReplaysSignedLifetimeWithoutMutation(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, attemptLedgerStoreName)
	bounds := attemptRecordStoreTestBounds()
	store := openAttemptRecordStoreTest(t, path, fixture, bounds, attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs)
	head, err := store.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	limits := AttemptLedgerDiskLimits{MaxRecordBytes: bounds.MaxRecordBytes, MaxRecordCount: bounds.MaxRecordCount, MaxTrailCount: bounds.MaxTrailCount, MaxRawRecordBytes: bounds.MaxRawRecordBytes, MaxStorageBytes: bounds.MaxStorageBytes, MaxStorageFiles: bounds.MaxStorageFiles}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, used, err := stoppedAttemptFiles(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ReadStoppedAttemptLedgerCapacity(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits)
	if err != nil {
		t.Fatal(err)
	}
	if value.Head != head || value.Head.LastSequence != 16 || value.Head.TrailCount != 2 || value.StorageBytes != used || value.StorageFiles != uint64(len(before)) {
		t.Fatalf("stopped source lost lifetime head/counters: %+v", value)
	}
	after, _, err := stoppedAttemptFiles(root, limits)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("read-only replay modified source files", err)
	}
	changed := fixture.identity
	changed.NoID++
	if _, err := ReadStoppedAttemptLedgerCapacity(t.Context(), stateDir, changed, value.Coordinator, fixture.validatorKey.Public().(ed25519.PublicKey), limits); err == nil {
		t.Fatal("another activation identity acquired source capacity")
	}
	limited := limits
	limited.MaxRecordCount = 15
	limited.MaxTrailCount = 2
	if _, err := ReadStoppedAttemptLedgerCapacity(t.Context(), stateDir, fixture.identity, value.Coordinator, fixture.validatorKey.Public().(ed25519.PublicKey), limited); err == nil || !strings.Contains(err.Error(), "signed lifetime prefix") {
		t.Fatal("lifetime records were treated as a fresh empty allowance", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadStoppedAttemptLedgerCapacity(ctx, stateDir, fixture.identity, value.Coordinator, fixture.validatorKey.Public().(ed25519.PublicKey), limits); err == nil {
		t.Fatal("canceled capacity replay succeeded")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(stateDir, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadStoppedAttemptLedgerCapacity(t.Context(), alias, fixture.identity, value.Coordinator, fixture.validatorKey.Public().(ed25519.PublicKey), limits); err == nil {
		t.Fatal("aliased source store accepted")
	}
	missing := t.TempDir()
	if _, err := ReadStoppedAttemptLedgerCapacity(t.Context(), missing, fixture.identity, value.Coordinator, fixture.validatorKey.Public().(ed25519.PublicKey), limits); err == nil {
		t.Fatal("missing ledger acquired empty capacity")
	}
	if _, err := os.Lstat(filepath.Join(missing, attemptLedgerStoreName)); !os.IsNotExist(err) {
		t.Fatal("read-only reader created a ledger")
	}
}

func TestStoppedAttemptLedgerCapacityRejectsForgedHeadAndSignedRecord(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, fault := range []string{"head", "signature", "index"} {
		t.Run(fault, func(t *testing.T) {
			stateDir := t.TempDir()
			path := filepath.Join(stateDir, attemptLedgerStoreName)
			bounds := attemptRecordStoreTestBounds()
			store := openAttemptRecordStoreTest(t, path, fixture, bounds, attemptRecordStoreHooks{})
			appendAttemptRecordStoreTest(t, store, fixture.recordTs)
			db := store.db
			switch fault {
			case "head":
				raw, err := db.Get([]byte("head"), nil)
				if err != nil {
					t.Fatal(err)
				}
				var head AttemptLedgerHead
				if err := json.Unmarshal(raw, &head); err != nil {
					t.Fatal(err)
				}
				head.TrailCount = 0
				raw, err = json.Marshal(head)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Put([]byte("head"), raw, &opt.WriteOptions{Sync: true}); err != nil {
					t.Fatal(err)
				}
			case "signature":
				record := fixture.recordTs[0]
				record.Signature = append([]byte(nil), record.Signature...)
				record.Signature[0] ^= 1
				raw, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Put(attemptStoreRecordKey(1), raw, &opt.WriteOptions{Sync: true}); err != nil {
					t.Fatal(err)
				}
			case "index":
				if err := db.Delete(attemptStoreTrailRecordKey(fixture.recordTs[0].TrailID, 1), &opt.WriteOptions{Sync: true}); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			limits := AttemptLedgerDiskLimits{MaxRecordBytes: bounds.MaxRecordBytes, MaxRecordCount: bounds.MaxRecordCount, MaxTrailCount: bounds.MaxTrailCount, MaxRawRecordBytes: bounds.MaxRawRecordBytes, MaxStorageBytes: bounds.MaxStorageBytes, MaxStorageFiles: bounds.MaxStorageFiles}
			if _, err := ReadStoppedAttemptLedgerCapacity(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits); err == nil || !strings.Contains(err.Error(), "signed lifetime prefix") {
				t.Fatalf("%s forgery did not reach signed lifetime validation: %v", fault, err)
			}
		})
	}
}

func TestStoppedAttemptLedgerCapacityPreservesOriginalRootAcrossRealAppend(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, attemptLedgerStoreName)
	bounds := attemptRecordStoreTestBounds()
	store := openAttemptRecordStoreTest(t, path, fixture, bounds, attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:8])
	prefix, err := store.Head()
	if err != nil {
		t.Fatal(err)
	}
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[8:])
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	limits := AttemptLedgerDiskLimits{MaxRecordBytes: bounds.MaxRecordBytes, MaxRecordCount: bounds.MaxRecordCount, MaxTrailCount: bounds.MaxTrailCount, MaxRawRecordBytes: bounds.MaxRawRecordBytes, MaxStorageBytes: bounds.MaxStorageBytes, MaxStorageFiles: bounds.MaxStorageFiles}
	read := func(prior AttemptLedgerHead) (StoppedAttemptLedgerCapacity, error) {
		return ReadStoppedAttemptLedgerCapacity(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits, prior)
	}
	value, err := read(prefix)
	if err != nil || value.Head.LastSequence != 16 || value.Head.TrailCount != 2 || prefix.LastSequence != 8 {
		t.Fatal("real successor records lost original checkpoint", err)
	}
	changed := prefix
	changed.Root = fixture.recordTs[8].RecordHash
	if _, err := read(changed); err == nil {
		t.Fatal("signed but different original root acquired continuation capacity")
	}
	changed = prefix
	changed.LastSequence = value.Head.LastSequence + 1
	if _, err := read(changed); err == nil {
		t.Fatal("original lifetime checkpoint could move past the actual signed store")
	}
}
