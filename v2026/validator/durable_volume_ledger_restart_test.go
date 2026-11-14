//go:build linux || darwin

package validator

// A physical root generation alone cannot prove that its acknowledged ledger
// members survived the previous owner. These controls retain every original
// byte while presenting either absence or an older valid database at reopen.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// An empty replacement namespace must not become a fresh signing ledger after
// the original owner has acknowledged a record and joined its backend.
func TestDurableAttemptLedgerMissingMembersCannotReset(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	fixture.identity.ChainID = 964
	fixture.recordTs[0].Identity = fixture.identity
	fixture.recordTs[0] = resignAttemptRecordStoreTest(t, fixture.recordTs[0], fixture.validatorKey)
	root := newAttemptLedgerDiskTestStateDir(t)
	storage := durablefixture.New(t, t.Context(), root)
	prepareAttemptLedgerCustodyTest(t, storage.Context, root, fixture.identity, fixture.validatorKey)
	ledger, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	retained := newAttemptLedgerDiskTestStateDir(t)
	moveAttemptLedgerRestartMembers(t, root, retained)
	reopened, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if reopened != nil {
		head, headErr := reopened.Head()
		_ = reopened.Close()
		t.Fatalf("missing acknowledged ledger was reinitialized: head=%+v err=%v open=%v", head, headErr, err)
	}
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("missing acknowledged members were not classified as custody loss", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("refused reopen reconstructed missing ledger members", entries, err)
	}
}

// A database with a valid older prefix is also loss. Retain the original root
// and database directory inodes, so the record head itself must fence rollback.
func TestDurableAttemptLedgerPriorDatabaseCannotReset(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	fixture.identity.ChainID = 964
	fixture.recordTs[0].Identity = fixture.identity
	fixture.recordTs[0] = resignAttemptRecordStoreTest(t, fixture.recordTs[0], fixture.validatorKey)
	root := newAttemptLedgerDiskTestStateDir(t)
	storage := durablefixture.New(t, t.Context(), root)
	prepareAttemptLedgerCustodyTest(t, storage.Context, root, fixture.identity, fixture.validatorKey)
	ledger, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, attemptLedgerStoreName)
	prior := newAttemptLedgerDiskTestStateDir(t)
	copyAttemptLedgerRestartMembers(t, database, prior)
	ledger, err = NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	retained := newAttemptLedgerDiskTestStateDir(t)
	moveAttemptLedgerRestartMembers(t, database, retained)
	copyAttemptLedgerRestartMembers(t, prior, database)
	reopened, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if reopened != nil {
		head, headErr := reopened.Head()
		_ = reopened.Close()
		t.Fatalf("older valid database erased acknowledged head: head=%+v err=%v open=%v", head, headErr, err)
	}
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("older acknowledged prefix was not classified as custody loss", err)
	}
}

// The closed LevelDB fixture contains a finite flat set of regular files.
func copyAttemptLedgerRestartMembers(t *testing.T, source, destination string) {
	t.Helper()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("closed database fixture unexpectedly contains a directory")
		}
		raw, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, entry.Name()), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// Renaming preserves the exact original files for the recovery control.
func moveAttemptLedgerRestartMembers(t *testing.T, source, destination string) {
	t.Helper()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.Rename(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
}
