//go:build linux || darwin

// Synthetic signed ledgers pass through physical export, the public fixed
// adapter, real plan/apply publication and the original guarded runtime owner.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Original source and archive stay present after the old logical path receives
// a separately declared physical generation. No target head is test-enrolled.
type attemptLedgerRestoreFixture struct {
	root       string
	archive    string
	metadata   string
	storage    *durablefixture.Fixture
	fixture    attemptRecordStoreTestFixture
	scope      AttemptLedgerPreparationScope
	report     durablevolume.Inventory
	request    durablevolume.Reference
	checkpoint attemptLedgerCustodyCheckpoint
}

// Every JSON authority is a separate immutable private file.
func (self *attemptLedgerRestoreFixture) write(t *testing.T, name string, value any) durablevolume.Reference {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.metadata, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write(raw)
	if err := errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	return durablevolume.Reference{Path: path, Sha256: durablefixture.Digest(raw)}
}

// The source uses the real original append barriers. Imported legacy bytes
// and pending record bytes are all produced by the existing signing fixture.
func newAttemptLedgerRestoreFixture(t *testing.T, fixture attemptRecordStoreTestFixture, legacy bool, stage string, configure ...func(*attemptLedgerRestoreFixture)) *attemptLedgerRestoreFixture {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	self := &attemptLedgerRestoreFixture{root: filepath.Join(parent, "ledger"), archive: filepath.Join(parent, "archive"), metadata: filepath.Join(parent, "metadata"), fixture: fixture}
	staging := filepath.Join(parent, "staging")
	for _, path := range []string{self.root, self.archive, self.metadata, staging} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	self.storage = durablefixture.New(t, t.Context(), self.root)
	if legacy {
		raw := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
		if err := os.WriteFile(filepath.Join(self.root, attemptLedgerLegacyName), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	prepareAttemptLedgerCustodyTest(t, self.storage.Context, self.root, fixture.identity, fixture.validatorKey)
	armed := false
	lost := errors.New("synthetic retained ledger publication acknowledgement lost")
	step := func(operation, _ string) error {
		if armed && operation == stage {
			return lost
		}
		return nil
	}
	ledger, err := newDiskAttemptLedgerWithHooks(self.storage.Context, self.root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, attemptLedgerDiskTestLimits(), attemptLedgerDiskHooks{Step: step, Store: attemptRecordStoreHooks{Step: step}})
	if err != nil {
		t.Fatal(err)
	}
	if stage != "" {
		armed = true
		if _, err := ledger.AppendContext(t.Context(), fixture.recordTs[0]); !errors.Is(err, lost) || !errors.Is(err, ErrDurablePublicationUncertain) {
			_ = ledger.Close()
			t.Fatal("source did not stop at exact pending barrier", stage, err)
		}
	}
	if err := ledger.Close(); stage == "" && err != nil || stage != "" && !errors.Is(err, ErrDurablePublicationUncertain) {
		t.Fatal("source writer did not join with original outcome", stage, err)
	}
	root, err := os.Open(self.root)
	if err != nil {
		t.Fatal(err)
	}
	anchor, readErr := readAttemptLedgerCustodyAttribute(root)
	if err := errors.Join(readErr, root.Close(), attemptStoreDecode(anchor, &self.checkpoint)); err != nil {
		t.Fatal(err)
	}
	self.scope = AttemptLedgerPreparationScope{Identity: fixture.identity, Coordinator: attemptLedgerDiskTestCoordinator, Limits: attemptLedgerDiskTestLimits(), ExpectedHead: self.checkpoint.Committed}
	if self.checkpoint.Legacy != nil {
		self.scope.Legacy = &AttemptLedgerPreparationLegacy{Bytes: self.checkpoint.Legacy.Bytes, Sha256: self.checkpoint.Legacy.Sha256}
	}
	for _, change := range configure {
		change(self)
	}
	config, err := durablevolume.Load(self.storage.Reference)
	if err != nil {
		t.Fatal(err)
	}
	volumeSpec := config.Volumes[0]
	fence := self.write(t, "source-fence.json", durablevolume.FormerWriterFence{Schema: durablevolume.FormerWriterFenceSchema, RootPath: self.root, DeclarationSha256: self.storage.Reference.Sha256, LeaseSha256: volumeSpec.StateRoots[0].LeaseSha256, FormerWritersStopped: true, Evidence: "synthetic original ledger writer explicitly closed and joined"})
	volume, err := durablevolume.OpenWithHost(self.storage.Reference, self.root, durablevolume.Snapshot, self.storage.Host)
	if err != nil {
		t.Fatal(err)
	}
	self.report, err = volume.InventoryPhysical(t.Context(), fence, durablevolume.InventoryLimits{MaxEntries: 300, MaxBytes: 128 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 8, MaxOwnerAttributeBytes: 32 * 1024})
	if err := errors.Join(err, volume.Close()); err != nil {
		t.Fatal(err)
	}
	reportRef := self.write(t, "original-inventory.json", self.report)
	for _, entry := range self.report.Entries {
		path := filepath.Join(self.archive, entry.Path)
		if entry.Path != "" {
			if entry.Kind == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				raw, err := os.ReadFile(filepath.Join(self.root, entry.Path))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, attribute := range entry.OwnerAttributes {
			if err := unix.Setxattr(path, attribute.Name, attribute.Value, unix.XATTR_CREATE); err != nil {
				t.Fatal(err)
			}
		}
	}
	nonce, err := hex.DecodeString(self.report.RootGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(self.archive, durablevolume.RootGenerationAttribute, nonce, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(self.root, self.root+".original-held"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(self.root, 0700); err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(self.root, &stat); err != nil {
		t.Fatal(err)
	}
	targetFence := self.write(t, "target-fence.json", durablevolume.PreparationFence{Schema: durablevolume.PreparationFenceSchema, RootPath: self.root, RootInode: stat.Ino, Purpose: "restore", FormerWritersStopped: true, NoPreviousTargetState: true, Evidence: "synthetic empty new physical root preserves the original logical path"})
	inputs, err := json.Marshal(self.scope)
	if err != nil {
		t.Fatal(err)
	}
	self.request = self.write(t, "request.json", durablevolume.PreparationRequest{Schema: durablevolume.PreparationRequestSchema, Purpose: "restore", Scope: "daemon", MountPath: volumeSpec.MountPath, FilesystemUuid: volumeSpec.FilesystemUuid, FilesystemType: volumeSpec.FilesystemType,
		MinAvailableBytes: 1024 * 1024, MinAvailableInodes: 64, RootPath: self.root, MarkerPath: filepath.Join(self.metadata, "identity"), LeasePath: filepath.Join(self.metadata, "lease"), DeclarationPath: filepath.Join(self.metadata, "declaration.json"), ControlPath: filepath.Join(self.metadata, "control.jsonl"), StagingDirectory: staging, FormerWriterFence: targetFence,
		RestoreSource: &durablevolume.PreparationRestoreSource{Directory: self.archive, Inventory: reportRef, FormerWriterFence: fence}, Limits: durablevolume.PreparationLimits{MaxEntries: 300, MaxBytes: 128 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 8, MaxOwnerAttributeBytes: 32 * 1024, MaxPlanBytes: 1024 * 1024},
		Owners: []durablevolume.PreparationOwner{{Kind: AttemptLedgerPreparationKind, RelativePath: ".", Purpose: "restore", Inputs: inputs}}})
	return self
}

// The same fixed adapter shape used by the command cannot enroll any other kind.
func attemptLedgerRestoreTestAdapter() durablevolume.PreparationAdapter {
	return durablevolume.PreparationAdapter{Restore: PlanAttemptLedgerRestore, InspectRestore: InspectAttemptLedgerRestore}
}

// Only reviewed production plan/apply publishes new physical coordinates.
func (self *attemptLedgerRestoreFixture) apply(t *testing.T) context.Context {
	t.Helper()
	plan, err := durablevolume.PlanPreparationWithHost(t.Context(), self.request, attemptLedgerRestoreTestAdapter(), self.storage.Host)
	if err != nil {
		t.Fatal("ledger restore planning refused retained original", err)
	}
	result, err := durablevolume.ApplyPreparationWithHost(t.Context(), self.write(t, "accepted-plan.json", plan), attemptLedgerRestoreTestAdapter(), self.storage.Host)
	if err != nil || result.RestartAuthorized {
		t.Fatal("ledger restore failed or authorized restart", err)
	}
	for _, entry := range self.report.Entries {
		if entry.Kind != "file" {
			continue
		}
		before, err := os.ReadFile(filepath.Join(self.archive, entry.Path))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(self.root, entry.Path))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("restore changed original signed or database bytes", entry.Path, err)
		}
	}
	root, err := os.Open(self.root)
	if err != nil {
		t.Fatal(err)
	}
	raw, readErr := readAttemptLedgerCustodyAttribute(root)
	var actual attemptLedgerCustodyCheckpoint
	if err := errors.Join(readErr, root.Close(), attemptStoreDecode(raw, &actual)); err != nil {
		t.Fatal(err)
	}
	if actual.Committed != self.checkpoint.Committed || (actual.Pending == nil) != (self.checkpoint.Pending == nil) || actual.DirectoryInode == self.checkpoint.DirectoryInode || actual.DatabaseInode == self.checkpoint.DatabaseInode {
		t.Fatal("restore changed original logical head or failed physical rebinding")
	}
	if actual.Pending != nil && (actual.Pending.Head != self.checkpoint.Pending.Head || actual.Pending.Record.Bytes != self.checkpoint.Pending.Record.Bytes || actual.Pending.Record.Sha256 != self.checkpoint.Pending.Record.Sha256 || (actual.Pending.Record.Inode == 0) != (self.checkpoint.Pending.Record.Inode == 0)) {
		t.Fatal("restore relabeled original pending authority")
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), self.storage.Host)
}

// Exact signed legacy receipts survive migration to new physical files. The
// actual runtime admits them without rewriting their old import coordinates.
func TestAttemptLedgerRestoreKeepsOriginalSignedLegacyPrefix(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	f := newAttemptLedgerRestoreFixture(t, fixture, true, "")
	ctx := f.apply(t)
	ledger, err := NewDiskAttemptLedger(ctx, f.root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, f.scope.Limits)
	if err != nil {
		t.Fatal("restored signed ledger cannot reopen", err)
	}
	defer ledger.Close()
	head, err := ledger.Head()
	if err != nil || head != f.scope.ExpectedHead {
		t.Fatal("runtime reset restored signed prefix", head, err)
	}
	if err := ledger.Walk(t.Context(), 1, head.LastSequence, func(record AttemptRecord) error {
		want, err := json.Marshal(fixture.recordTs[record.Sequence-1])
		if err != nil {
			return err
		}
		actual, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if !bytes.Equal(want, actual) {
			return errors.New("restored record differs from original signed bytes")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{attemptLedgerImportName, attemptLedgerReadyName, attemptLedgerLegacyName} {
		before, err := os.ReadFile(filepath.Join(f.archive, name))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(f.root, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("runtime rewrote retained migration authority", name, err)
		}
	}
}

// Before batch, after batch and committed-before-cleanup preserve distinct
// original phases; constructor reconciliation only replays their exact record.
func TestAttemptLedgerRestoreRetainsPendingAndCleanupPhases(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, stage := range []string{"custody-record-synced", "after-batch", "custody-committed-synced"} {
		func() {
			f := newAttemptLedgerRestoreFixture(t, fixture, false, stage)
			ctx := f.apply(t)
			ledger, err := NewDiskAttemptLedger(ctx, f.root, fixture.identity, attemptLedgerDiskTestCoordinator, fixture.validatorKey, f.scope.Limits)
			if err != nil {
				t.Fatal("restored original pending phase cannot reconcile", stage, err)
			}
			defer ledger.Close()
			head, err := ledger.Head()
			if err != nil || head.LastSequence != 1 || head.Root != fixture.recordTs[0].RecordHash {
				t.Fatal("runtime lost original pending record", stage, head, err)
			}
			if err := ledger.Walk(t.Context(), 1, 1, func(record AttemptRecord) error {
				raw, err := json.Marshal(record)
				if err != nil {
					return err
				}
				want, err := json.Marshal(fixture.recordTs[0])
				if err != nil {
					return err
				}
				if !bytes.Equal(raw, want) {
					return errors.New("pending signature or record was replaced")
				}
				return nil
			}); err != nil {
				t.Fatal(stage, err)
			}
			if _, err := os.Stat(filepath.Join(f.root, attemptLedgerPendingName)); !os.IsNotExist(err) {
				t.Fatal("actual runtime did not finish exact original cleanup", stage, err)
			}
		}()
	}
}

// Header-only and changed signed pending members remain incomplete; planning
// cannot reinterpret either as fresh custody or create a replacement record.
func TestAttemptLedgerRestoreRefusesMissingOrPartialPendingIntent(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, stage := range []string{"custody-pending-synced", "custody-record-synced"} {
		func() {
			f := newAttemptLedgerRestoreFixture(t, fixture, false, stage)
			if stage == "custody-record-synced" {
				for index, entry := range f.report.Entries {
					if entry.Path == attemptLedgerPendingName {
						f.report.Entries[index].Size--
					}
				}
			}
			var request durablevolume.PreparationRequest
			raw, err := os.ReadFile(f.request.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Fatal(err)
			}
			_, err = PlanAttemptLedgerRestore(t.Context(), "exact-stage", request.Owners[0], f.report)
			if !errors.Is(err, ErrDurablePublicationUncertain) {
				t.Fatal("partial original intent was reset or accepted", stage, err)
			}
			entries, err := os.ReadDir(f.root)
			if err != nil || len(entries) != 0 {
				t.Fatal("refused restore created target members", stage, err)
			}
		}()
	}
}

// An explicitly reviewed head never authorizes rollback, and an unrecognized
// namespace member cannot disappear from a plausible complete restore report.
func TestAttemptLedgerRestoreRejectsWrongHeadAndUnknownMembers(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	f := newAttemptLedgerRestoreFixture(t, fixture, true, "")
	var request durablevolume.PreparationRequest
	raw, err := os.ReadFile(f.request.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	owner := request.Owners[0]
	scope := f.scope
	scope.ExpectedHead = AttemptLedgerHead{Root: zeroAttemptHash()}
	owner.Inputs, err = json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PlanAttemptLedgerRestore(t.Context(), "exact-stage", owner, f.report); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restore accepted an older reviewed head", err)
	}
	report := f.report
	report.Entries = append([]durablevolume.InventoryEntry(nil), f.report.Entries...)
	for index, entry := range report.Entries {
		if entry.Path == attemptLedgerReadyName {
			report.Entries[index].Path = "another-owner.json"
		}
	}
	if _, err := PlanAttemptLedgerRestore(t.Context(), "exact-stage", request.Owners[0], report); err == nil {
		t.Fatal("restore omitted another owner's member")
	}
	entries, err := os.ReadDir(f.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed original authority changed target", err)
	}
}
