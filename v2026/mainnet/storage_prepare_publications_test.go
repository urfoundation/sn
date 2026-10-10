//go:build linux || darwin

package main

// The original request journal creates and signs these windows and closures.
// The public storage command owns namespace birth and all subsequent copying.
import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

type storagePublicationFixture struct {
	source  *storagePreparationCommandFixture
	ctx     context.Context
	owner   durablevolume.PreparationOwner
	profile validator.ProviderAttemptPublicationPreparation
}

// Select the literal runtime child before granting any original root birth.
func newStoragePublicationPreparationFixture(t *testing.T) *storagePublicationFixture {
	t.Helper()
	f := &storagePublicationFixture{source: newStoragePreparationCommandFixture(t)}
	old := f.source.root
	f.source.root = filepath.Join(filepath.Dir(old), "provider-attempt-publications")
	if err := os.Rename(old, f.source.root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(f.source.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	request.RootPath = f.source.root
	raw, err = os.ReadFile(request.FormerWriterFence.Path)
	if err != nil {
		t.Fatal(err)
	}
	var fence durablevolume.PreparationFence
	if err := json.Unmarshal(raw, &fence); err != nil {
		t.Fatal(err)
	}
	fence.RootPath = f.source.root
	raw, err = json.Marshal(fence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.FormerWriterFence.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	request.FormerWriterFence.Sha256 = safeReleaseHash(raw)
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.source.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.source.requestHash = safeReleaseHash(raw)
	preparation := validator.ProviderAttemptRequestPreparation{Identity: validator.ProviderAttemptRequestIdentity{Ledger: f.source.identity, Coordinator: "0x" + strings.Repeat("23", 20), ClientId: connect.Id{151}, PolicyHash: [32]byte{152}}, Limits: validator.ProviderAttemptRequestLimits{MaxRecords: 32, MaxRecordBytes: 8192, MaxJournalBytes: 1024 * 1024}, Birth: validator.AttemptBoundary{SettlementEpoch: 23, EVMBlock: 101, EVMBlockHash: "0x" + strings.Repeat("71", 32)}}
	genesis, err := hex.DecodeString(strings.TrimPrefix(f.source.identity.GenesisHash, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	scope := protocol.ProviderAttemptReceiptScope{Profile: "synthetic-publication", DeploymentId: f.source.identity.DeploymentID, DeploymentKey: "964:" + preparation.Identity.Coordinator, PolicyHash: preparation.Identity.PolicyHash, Netuid: uint64(f.source.identity.Netuid), NoId: f.source.identity.NoID}
	copy(scope.GenesisHash[:], genesis)
	f.profile = validator.ProviderAttemptPublicationPreparation{StateDir: filepath.Dir(f.source.root), MaxWindowBytes: 1024 * 1024, MaxHistoryBytes: 4 * 1024 * 1024, MaxFiles: 32, Operators: []validator.ProviderAttemptPublicationOperator{{Preparation: preparation, ReceiptScope: scope}}}
	f.owner = storagePublicationProfileOwner(t, f.source, f.profile)
	storagePreparationOwnerRequest(t, f.source, "daemon", []durablevolume.PreparationOwner{f.owner})
	return f
}

// Ordinary fixtures publish only after the independent profile is fully set.
func newStoragePublicationFixture(t *testing.T) *storagePublicationFixture {
	t.Helper()
	f := newStoragePublicationPreparationFixture(t)
	f.ctx = storagePreparationApplyOwnerCommand(t, f.source, "storage-prepare")
	return f
}

func storagePublicationProfileOwner(t *testing.T, source *storagePreparationCommandFixture, profile validator.ProviderAttemptPublicationPreparation) durablevolume.PreparationOwner {
	t.Helper()
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(source.metadata, "publication-original-profile.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	inputs, err := json.Marshal(storageProviderPublicationPreparationScope{Schema: storageProviderPublicationPreparationSchema, PreparationProfile: durablevolume.Reference{Path: path, Sha256: safeReleaseHash(raw)}})
	if err != nil {
		t.Fatal(err)
	}
	return durablevolume.PreparationOwner{Kind: storageProviderPublicationKind, RelativePath: ".", Purpose: "fresh", Inputs: inputs}
}

// Explicit synthetic offline preparation is limited to the separate request
// journal fixture. Publication birth always comes from the public command.
func (self *storagePublicationFixture) originals(t *testing.T, count int) []validator.ProviderAttemptRequestWindow {
	t.Helper()
	directory := filepath.Join(filepath.Dir(self.source.root), "original-request-journal")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	storage := durablefixture.New(t, t.Context(), directory)
	root, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := os.OpenFile(filepath.Join(directory, validator.ProviderAttemptRequestJournalName), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	rootInfo, err := root.Stat()
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	fileInfo, err := file.Stat()
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	operator := self.profile.Operators[0]
	raw, err := validator.FreshProviderAttemptRequestCheckpoint(operator.Preparation, rootInfo, fileInfo)
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	if err := durablesys.SetAttribute(int(root.Fd()), validator.ProviderAttemptRequestAttribute, raw, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	if err := root.Sync(); err != nil {
		t.Fatal(err)
	}
	journal, err := validator.OpenProviderAttemptRequestJournal(storage.Context, directory, operator.Preparation, self.source.key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	var cuts []validator.ProviderAttemptRequestWindow
	for index := range count {
		boundary := operator.Preparation.Birth
		boundary.SettlementEpoch += uint64(index)
		boundary.EVMBlock += 100 * uint64(index)
		nonce := bytes.Repeat([]byte{byte(153 + index)}, 32)
		public := self.source.key.Public().(ed25519.PublicKey)
		message, err := connect.BuildVerifySeedMessage(public, nonce, 8)
		if err != nil {
			t.Fatal(err)
		}
		signature := ed25519.Sign(self.source.key, message)
		body, err := json.Marshal(connect.VerifySeedArgs{ClientId: operator.Preparation.Identity.ClientId, Vpk: public, ClientNonce: nonce, M: 8, SeedSig: signature})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := journal.Append(storage.Context, boundary, connect.Id{154}, body, message, signature); err != nil {
			t.Fatal("actual original request append failed", err)
		}
		window := protocol.ValidatorEvidenceWindow{Epoch: boundary.SettlementEpoch, StartBlock: boundary.EVMBlock, EndBlock: boundary.EVMBlock + 100, FinalizedBlock: boundary.EVMBlock + 100}
		cut, err := journal.SealWindow(storage.Context, window, self.profile.MaxWindowBytes)
		if err != nil {
			t.Fatal("actual original window did not close", err)
		}
		if err := journal.CloseRequests(storage.Context, cut, operator.ReceiptScope); err != nil {
			t.Fatal("actual original request closure did not seal", err)
		}
		if len(cut.Records) != 1 || len(cut.Closures) != 1 {
			t.Fatal("actual journal did not create one exact request and close per window")
		}
		cuts = append(cuts, *cut)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	return cuts
}

// Preserve a producer's original canonical payload in its fixed public name.
// Worker publication/replication barriers are independently owned by Integration.
func (self *storagePublicationFixture) retain(t *testing.T, cut validator.ProviderAttemptRequestWindow) string {
	t.Helper()
	path, err := validator.ProviderAttemptPublicationPath(self.profile.StateDir, cut.Header.Window.Epoch, cut.Header.Preparation.Identity.Ledger.NoID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cut)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	n, writeErr := file.Write(raw)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil || n != len(raw) {
		t.Fatal("fixture lost exact original producer payload", err)
	}
	return path
}

func TestStoragePreparationPublicationFreshAndEmptyRestoreAdmitActualNamespace(t *testing.T) {
	f := newStoragePublicationFixture(t)
	guard, err := validator.OpenProviderAttemptPublicationNamespace(f.ctx, f.profile)
	if err != nil {
		t.Fatal("public fresh preparation cannot open runtime namespace", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	ctx := applyStorageOriginalRestore(t, storage)
	guard, err = validator.OpenProviderAttemptPublicationNamespace(ctx, f.profile)
	if err != nil {
		t.Fatal("copied authentic empty namespace cannot reopen", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(f.source.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("empty restored namespace invented signed windows", err)
	}
}

func TestStoragePreparationPublicationRestoreKeepsActualWindowsClosuresAndCrashFiles(t *testing.T) {
	f := newStoragePublicationFixture(t)
	cuts := f.originals(t, 2)
	for _, cut := range cuts {
		f.retain(t, cut)
	}
	temp := filepath.Join(f.source.root, ".compact-input-"+strings.Repeat("ab", 16))
	partial := []byte(`{"header":`)
	if err := os.WriteFile(temp, partial, 0600); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	ctx := applyStorageOriginalRestore(t, storage)
	guard, err := validator.OpenProviderAttemptPublicationNamespace(ctx, f.profile)
	if err != nil {
		t.Fatal("complete restored original namespace cannot reopen", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(temp); err != nil || !bytes.Equal(raw, partial) {
		t.Fatal("restore erased or promoted interrupted temporary bytes", err)
	}
	var prior validator.ProviderAttemptRequestHead
	var previous [32]byte
	for _, original := range cuts {
		path, err := validator.ProviderAttemptPublicationPath(f.profile.StateDir, original.Header.Window.Epoch, original.Header.Preparation.Identity.Ledger.NoID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cut validator.ProviderAttemptRequestWindow
		if err := json.Unmarshal(raw, &cut); err != nil {
			t.Fatal(err)
		}
		operator := f.profile.Operators[0]
		if err := validator.VerifyProviderAttemptPublishedWindow(t.Context(), cut, operator.Preparation, operator.ReceiptScope, prior, previous, original.Header.Window, f.profile.MaxWindowBytes); err != nil {
			t.Fatal("restored full original closure cannot replay independently", err)
		}
		previous, err = cut.Header.Hash()
		if err != nil {
			t.Fatal(err)
		}
		prior = cut.Header.End
	}
}

func TestStoragePreparationPublicationRestoreRefusesMissingOriginalClose(t *testing.T) {
	f := newStoragePublicationFixture(t)
	cut := f.originals(t, 1)[0]
	cut.Closures = nil
	f.retain(t, cut)
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	path, hash := storagePreparationFreezeOwnerPlan(t, storage.target, storage.command)
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code == 0 {
		t.Fatal("request window without its original close acquired restored custody")
	}
	if _, err := unix.Getxattr(f.source.root, validator.ProviderAttemptPublicationNamespaceAttribute, make([]byte, 4096)); !errors.Is(err, durablesys.ErrNoAttribute) {
		t.Fatal("missing original close published a rebound birth", err)
	}
	if entries, err := os.ReadDir(storage.heldSource); err != nil || len(entries) != 1 {
		t.Fatal("failed verification discarded original source evidence", err)
	}
}

func TestStoragePreparationPublicationRestoreRefusesMissingOriginalWindow(t *testing.T) {
	f := newStoragePublicationFixture(t)
	cuts := f.originals(t, 2)
	f.retain(t, cuts[1])
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	path, hash := storagePreparationFreezeOwnerPlan(t, storage.target, storage.command)
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code == 0 {
		t.Fatal("later valid window hid the missing original birth prefix")
	}
	if _, err := unix.Getxattr(f.source.root, validator.ProviderAttemptPublicationNamespaceAttribute, make([]byte, 4096)); !errors.Is(err, durablesys.ErrNoAttribute) {
		t.Fatal("missing original prefix published a rebound birth", err)
	}
}

func TestStoragePreparationPublicationRestoreRejectsChangedApprovedScopeBeforeCopy(t *testing.T) {
	f := newStoragePublicationFixture(t)
	f.retain(t, f.originals(t, 1)[0])
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	f.profile.Operators[0].ReceiptScope.Profile += "-changed"
	owner := storagePublicationProfileOwner(t, storage.target, f.profile)
	owner.Purpose = "restore"
	storagePreparationOwnerRequest(t, storage.target, "daemon", []durablevolume.PreparationOwner{owner})
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "plan", "--request", storage.target.requestPath, "--request-sha256", storage.target.requestHash}, &output, &diagnostic); code == 0 {
		t.Fatal("new approval rebound old request publications to a different receipt domain")
	}
	if entries, err := os.ReadDir(f.source.root); err != nil || len(entries) != 0 {
		t.Fatal("foreign publication plan changed the empty replacement target", err)
	}
}

func TestStoragePreparationPublicationGuardKeepsFailedObservationCauseAndOwner(t *testing.T) {
	f := newStoragePublicationFixture(t)
	guard, err := validator.OpenProviderAttemptPublicationNamespace(f.ctx, f.profile)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	borrowed, err := os.Open(f.source.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(f.ctx, borrowed); !errors.Is(err, os.ErrClosed) || errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("failed read became false custody loss or lost its cause", err)
	}
	borrowed, err = os.Open(f.source.root)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	canceled, cancel := context.WithCancel(f.ctx)
	cancel()
	if err := guard.Check(canceled, borrowed); !errors.Is(err, context.Canceled) || errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("cancellation became false publication custody loss", err)
	}
	if err := guard.Check(f.ctx, borrowed); err != nil {
		t.Fatal("retryable failed observation poisoned the same actual owner", err)
	}
	if err := unix.Removexattr(f.source.root, validator.ProviderAttemptPublicationNamespaceAttribute); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(f.ctx, borrowed); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("actual missing retained birth remained authoritative", err)
	}
}
