//go:build linux

package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Each boundary has a durable exact predecessor. Reopening never invokes the
// signing path; the retained original bytes either commit or remain blocked.
func TestDurableAttemptLedgerPendingCrashReplaysOriginalRecord(t *testing.T) {
	for _, stage := range []string{"custody-record-synced", "custody-committed-synced", "custody-cleanup"} {
		t.Run(stage, func(t *testing.T) {
			fixture, root, storage := newDurableAttemptPendingFixture(t)
			lostAck := errors.New("synthetic ledger custody barrier acknowledgement lost")
			armed := false
			ledger, err := newDiskAttemptLedgerWithHooks(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: func(operation, name string) error {
				if armed && (operation == stage || operation == "directory-sync" && name == stage) {
					return lostAck
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			armed = true
			if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); !errors.Is(err, ErrDurablePublicationUncertain) || !errors.Is(err, lostAck) || errors.Is(err, durablevolume.ErrIdentity) {
				t.Fatal("barrier ambiguity was not retained as publication uncertainty", err)
			}
			if _, err := ledger.Head(); !errors.Is(err, ErrDurablePublicationUncertain) {
				t.Fatal("uncertain instance offered authoritative cached head", err)
			}
			if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); !errors.Is(err, ErrDurablePublicationUncertain) {
				t.Fatal("uncertain instance retried its mutation", err)
			}
			_ = ledger.Close()
			assertDurableAttemptOriginalReopen(t, root, storage, fixture)
		})
	}
}

// Missing bytes after a header-only checkpoint are an unresolved publication,
// not a new empty ledger. Explicit restoration must match the original hash.
func TestDurableAttemptLedgerIncompletePendingKeepsExactExpectation(t *testing.T) {
	fixture, root, storage := newDurableAttemptPendingFixture(t)
	stopped := errors.New("synthetic stop after pending checkpoint")
	armed := false
	ledger, err := newDiskAttemptLedgerWithHooks(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: func(operation, _ string) error {
		if armed && operation == "custody-pending-synced" {
			return stopped
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	armed = true
	if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); !errors.Is(err, stopped) || !errors.Is(err, ErrDurablePublicationUncertain) {
		t.Fatal(err)
	}
	_ = ledger.Close()
	for attempt := 0; attempt < 2; attempt++ {
		reopened, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
		if reopened != nil || !errors.Is(err, ErrDurablePublicationUncertain) || errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("incomplete publication was reset or falsely classified as changed custody", reopened, err)
		}
		if _, err := os.Lstat(filepath.Join(root, attemptLedgerPendingName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("reopen invented missing pending bytes", err)
		}
	}
	raw, err := json.Marshal(fixture.recordTs[0])
	if err != nil {
		t.Fatal(err)
	}
	// This test-owned restoration supplies the original serialized signature;
	// the constructor must authenticate and replay it without a signing call.
	if err := os.WriteFile(filepath.Join(root, attemptLedgerPendingName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	assertDurableAttemptOriginalReopen(t, root, storage, fixture)
}

// Once the pending member's inode and complete bytes were acknowledged, its
// subsequent absence is physical loss, even if the database has not committed.
func TestDurableAttemptLedgerLostPendingRecordCannotRetry(t *testing.T) {
	fixture, root, storage := newDurableAttemptPendingFixture(t)
	armed := false
	ledger, err := newDiskAttemptLedgerWithHooks(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: func(operation, _ string) error {
		if armed && operation == "custody-record-synced" {
			return errors.New("synthetic process exit before database call")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	armed = true
	if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); !errors.Is(err, ErrDurablePublicationUncertain) {
		t.Fatal(err)
	}
	_ = ledger.Close()
	path := filepath.Join(root, attemptLedgerPendingName)
	retained := filepath.Join(newAttemptLedgerDiskTestStateDir(t), "original")
	if err := os.Rename(path, retained); err != nil {
		t.Fatal(err)
	}
	if reopened, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits()); reopened != nil || !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("lost acknowledged pending record was recreated or downgraded to retry", reopened, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused opener recreated lost signed bytes", err)
	}
	if err := os.Rename(retained, path); err != nil {
		t.Fatal(err)
	}
	assertDurableAttemptOriginalReopen(t, root, storage, fixture)
}

// A deleted mandatory checkpoint cannot turn a retained database into runtime
// enrollment. Replacing checkpoint bytes also poisons the already opened owner.
func TestDurableAttemptLedgerAnchorLossIsSticky(t *testing.T) {
	fixture, root, storage := newDurableAttemptPendingFixture(t)
	ledger, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	file, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	raw, err := readAttemptLedgerCustodyAttribute(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Fremovexattr(int(file.Fd()), attemptLedgerCustodyAttribute); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Head(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("cached head ignored lost mandatory anchor", err)
	}
	if err := unix.Fsetxattr(int(file.Fd()), attemptLedgerCustodyAttribute, raw, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Head(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restoring anchor revived invalidated owner", err)
	}
}

// Keep the original signed input and all physical setup explicit in fixtures.
func newDurableAttemptPendingFixture(t *testing.T) (attemptRecordStoreTestFixture, string, *durablefixture.Fixture) {
	t.Helper()
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	fixture.identity.ChainID = 964
	fixture.recordTs[0].Identity = fixture.identity
	fixture.recordTs[0] = resignAttemptRecordStoreTest(t, fixture.recordTs[0], fixture.validatorKey)
	root := newAttemptLedgerDiskTestStateDir(t)
	storage := durablefixture.New(t, t.Context(), root)
	prepareAttemptLedgerCustodyTest(t, storage.Context, root, fixture.identity, fixture.validatorKey)
	return fixture, root, storage
}

func assertDurableAttemptOriginalReopen(t *testing.T, root string, storage *durablefixture.Fixture, fixture attemptRecordStoreTestFixture) {
	t.Helper()
	reopened, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	head, err := reopened.Head()
	if err != nil || head.LastSequence != 1 || head.Root != fixture.recordTs[0].RecordHash {
		t.Fatal("reconciliation changed original acknowledged prefix", head, err)
	}
	if err := reopened.Walk(t.Context(), 1, 1, func(record AttemptRecord) error {
		original, err := json.Marshal(fixture.recordTs[0])
		actual, actualErr := json.Marshal(record)
		if err != nil || actualErr != nil || !bytes.Equal(original, actual) {
			t.Error("reconciliation changed original signed bytes", err, actualErr)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, attemptLedgerPendingName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed reconciliation retained an unresolved pending member", err)
	}
}
