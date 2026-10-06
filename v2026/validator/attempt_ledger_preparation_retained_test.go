//go:build linux

// Nonempty inspection uses the real signed ledger reader without constructing
// a writable owner. Original backend and migration bytes remain unchanged.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The legacy source is a synthetic signed trail. Its production import supplies
// real LevelDB files and exact ready/import receipts before the writer joins.
func attemptPreparationRetainedFixture(t *testing.T) (string, AttemptLedgerPreparationScope, attemptRecordStoreTestFixture) {
	t.Helper()
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "retained-ledger")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
	if err := os.WriteFile(filepath.Join(path, attemptLedgerLegacyName), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	limits := attemptLedgerDiskTestLimits()
	ledger, err := NewDiskAttemptLedger(t.Context(), path, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, limits)
	if err != nil {
		t.Fatal(err)
	}
	head, headErr := ledger.Head()
	if err := errors.Join(headErr, ledger.Close()); err != nil {
		t.Fatal(err)
	}
	scope := AttemptLedgerPreparationScope{Identity: fixture.identity, Coordinator: attemptLedgerDiskTestCoordinator, Limits: limits,
		ExpectedHead: head, Legacy: &AttemptLedgerPreparationLegacy{Bytes: uint64(len(legacy)), Sha256: attemptLedgerCustodyDigest(legacy)}}
	return path, scope, fixture
}

// Every private fixture member is retained before and after offline reads.
type attemptPreparationRetainedMember struct {
	info os.FileInfo
	raw  []byte
}

// This helper inspects only the test's small stopped tree, never live storage.
func attemptPreparationRetainedMembers(t *testing.T, path string) map[string]attemptPreparationRetainedMember {
	t.Helper()
	members := map[string]attemptPreparationRetainedMember{}
	err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		member := attemptPreparationRetainedMember{info: info}
		if !entry.IsDir() {
			member.raw, err = os.ReadFile(current)
			if err != nil {
				return err
			}
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		members[relative] = member
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return members
}

// Empty fresh databases never decode a record. This actual nonempty public
// inspection must authenticate the signed prefix without a writable disk hook.
func TestAttemptPreparationInspectsRetainedSignedPrefixWithoutWriter(t *testing.T) {
	path, scope, _ := attemptPreparationRetainedFixture(t)
	before := attemptPreparationRetainedMembers(t, path)
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	for index := 0; index < 2; index++ {
		census, err := InspectAttemptLedgerPreparation(t.Context(), directory, scope)
		if err != nil || census.Head != scope.ExpectedHead || census.Head.LastSequence == 0 || census.RestartAuthorized {
			t.Fatal("read-only preparation failed original signed prefix", census.Head, err)
		}
	}
	after := attemptPreparationRetainedMembers(t, path)
	if len(before) != len(after) {
		t.Fatal("inspection changed retained member census")
	}
	for name, prior := range before {
		current, present := after[name]
		if !present || !os.SameFile(prior.info, current.info) || prior.info.Mode() != current.info.Mode() || prior.info.Size() != current.info.Size() || !prior.info.ModTime().Equal(current.info.ModTime()) || !bytes.Equal(prior.raw, current.raw) {
			t.Fatal("inspection rewrote original ledger member", name)
		}
	}
}

// Optional hooks cannot waive canonical bytes, signature or per-record bounds.
func TestAttemptPreparationReadOnlyDecodeRetainsSignatureAndByteBounds(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	store := &attemptRecordStore{identity: attemptRecordStoreIdentity{Schema: attemptStoreSchema, Identity: fixture.identity, Coordinator: attemptLedgerDiskTestCoordinator}, vpk: fixture.validatorKey.Public().(ed25519.PublicKey), bounds: attemptRecordStoreTestBounds()}
	raw, err := json.Marshal(fixture.recordTs[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.decodeRecord(raw); err != nil {
		t.Fatal("read-only decode required a writable owner", err)
	}
	corrupt := fixture.recordTs[0]
	corrupt.Signature = append([]byte(nil), corrupt.Signature...)
	corrupt.Signature[0] ^= 1
	invalid, err := json.Marshal(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.decodeRecord(invalid); err == nil {
		t.Fatal("read-only decode accepted changed signature")
	}
	store.bounds.MaxRecordBytes = uint64(len(raw)) - 1
	if _, err := store.decodeRecord(raw); !errors.Is(err, errAttemptRecordStoreLimit) {
		t.Fatal("read-only decode bypassed the exact byte bound", err)
	}
}
