// A mainnet ledger's actual public append/read surface retains its physical
// declaration across foreground writes, backend workers and verified reopen.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// These exported constructors must refuse before creating an absent mainnet
// directory. Legacy read grammars do not imply authority to create a writer.
func TestMainnetAttemptLedgerPublicConstructorsRequireCustody(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	fixture.identity.ChainID = 964
	root := filepath.Join(t.TempDir(), "unadmitted")
	if ledger, err := NewAttemptLedger(root, fixture.identity, fixture.validatorKey); ledger != nil || err == nil {
		t.Fatal("legacy public constructor admitted mainnet custody", err)
	}
	if ledger, err := NewDiskAttemptLedger(t.Context(), root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits()); ledger != nil || err == nil {
		t.Fatal("disk public constructor admitted mainnet custody", err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused public constructors created state", err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	storage := durablefixture.New(t, t.Context(), root)
	if ledger, err := NewAttemptLedgerContext(storage.Context, root, fixture.identity, fixture.validatorKey); ledger != nil || !errors.Is(err, ErrAttemptLedgerStreamingRequired) {
		t.Fatal("a declaration turned historical JSONL into an unanchored mainnet writer", ledger, err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("refused legacy writer created mainnet custody", entries, err)
	}
}

// Removing the private key after construction makes reaching ed25519.Sign
// observable as a panic. Capacity admission must refuse before using that key,
// and restoring capacity/key must continue the same original head once.
func TestDurableAttemptLedgerAdmissionPrecedesRecordSigning(t *testing.T) {
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
	defer ledger.Close()
	key := ledger.vsk
	ledger.vsk = nil
	storage.Host.SetReserve(0, 0)
	if record, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); record != nil || !errors.Is(err, durablevolume.ErrUnavailable) {
		t.Fatal("unavailable writer reached signing or publication", record, err)
	}
	ledger.vsk = key
	storage.Host.SetReserve(1024*1024*1024, 1024*1024)
	if record, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); err != nil || record.Sequence != 1 || record.RecordHash != fixture.recordTs[0].RecordHash {
		t.Fatal("same owner did not retain its original first record", record, err)
	}
}

// Capacity refusal before the database call cannot append a signed record or
// fault the current owner. Explicit restoration of reserve permits one append.
func TestDurableAttemptLedgerReserveKeepsOriginalPrefix(t *testing.T) {
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
	t.Cleanup(func() { _ = ledger.Close() })
	before, err := ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	storage.Host.SetReserve(0, 0)
	if record, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); record != nil || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, ErrDurablePublicationUncertain) {
		t.Fatal("reserve refusal appended or poisoned an uncertain record", record, err)
	}
	if head, err := ledger.Head(); err != nil || head != before {
		t.Fatal("full-media inspection hid or changed original prefix", head, err)
	}
	storage.Host.SetReserve(1024*1024*1024, 1024*1024)
	record, err := ledger.AppendContext(t.Context(), fixture.recordTs[0])
	if err != nil || record.RecordHash != fixture.recordTs[0].RecordHash || record.Sequence != 1 {
		t.Fatal("same owner failed to continue exact original record", record, err)
	}
}

// A real committed database batch with a lost acknowledgement is reopened and
// authenticated. No append or head can pretend the old prefix is authoritative.
func TestDurableAttemptLedgerUncertainAppendReconcilesOriginalBytes(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	fixture.identity.ChainID = 964
	fixture.recordTs[0].Identity = fixture.identity
	fixture.recordTs[0] = resignAttemptRecordStoreTest(t, fixture.recordTs[0], fixture.validatorKey)
	root := newAttemptLedgerDiskTestStateDir(t)
	storage := durablefixture.New(t, t.Context(), root)
	prepareAttemptLedgerCustodyTest(t, storage.Context, root, fixture.identity, fixture.validatorKey)
	var fail atomic.Bool
	lostAck := errors.New("synthetic committed WAL acknowledgement lost")
	ledger, err := newDiskAttemptLedgerWithHooks(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Store: attemptRecordStoreHooks{Step: func(operation, _ string) error {
		if operation == "after-batch" && fail.Load() {
			return lostAck
		}
		return nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	fail.Store(true)
	if record, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); record != nil || !errors.Is(err, lostAck) || !errors.Is(err, ErrDurablePublicationUncertain) || errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("lost database acknowledgement misclassified", record, err)
	}
	if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); !errors.Is(err, ErrDurablePublicationUncertain) {
		t.Fatal("uncertain public append retried without reopen", err)
	}
	if _, err := ledger.Head(); !errors.Is(err, ErrDurablePublicationUncertain) {
		t.Fatal("uncertain public head hid its missing acknowledgement", err)
	}
	if err := ledger.Close(); !errors.Is(err, ErrDurablePublicationUncertain) {
		t.Fatal("close erased uncertain append", err)
	}
	reopened, err := NewDiskAttemptLedger(storage.Context, root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	head, err := reopened.Head()
	if err != nil || head.LastSequence != 1 || head.Root != fixture.recordTs[0].RecordHash {
		t.Fatal("original committed prefix was reset", head, err)
	}
	if err := reopened.Walk(context.Background(), 1, 1, func(record AttemptRecord) error {
		if !bytes.Equal(record.Signature, fixture.recordTs[0].Signature) || record.RecordHash != fixture.recordTs[0].RecordHash {
			t.Error("reopened original signed bytes changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A replaced database parent must block cached head and actual backend writes,
// even if the original directory is subsequently restored.
func TestDurableAttemptLedgerReplacementBlocksCachedState(t *testing.T) {
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
	t.Cleanup(func() { _ = ledger.Close() })
	if err := os.Rename(root, root+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Head(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("cached public head accepted replacement root", err)
	}
	if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("public append accepted replacement root", err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root+"-original", root); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Head(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restoration revived lost database custody", err)
	}
}
