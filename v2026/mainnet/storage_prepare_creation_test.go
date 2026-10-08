//go:build linux || darwin

package main

// Public birth/export/restore is followed by the real SDK request and callback.
// All keys are synthetic; signed original bytes and filesystem custody are real.
import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/miner"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

type storageCreationFixture struct {
	source  *storagePreparationCommandFixture
	ctx     context.Context
	owner   durablevolume.PreparationOwner
	profile miner.ProviderContractCaptureProfile
	scope   connect.OriginalContractStoreScope
	seed    []byte
}

func newStorageCreationFixture(t *testing.T) *storageCreationFixture {
	t.Helper()
	f := &storageCreationFixture{source: newStoragePreparationCommandFixture(t), seed: bytes.Repeat([]byte{141}, ed25519.SeedSize)}
	key := ed25519.NewKeyFromSeed(f.seed)
	domain := protocol.ClientKeyHistoryDomain{ChainID: 31337, GenesisHash: [32]byte{142}, Netuid: 7, Coordinator: common.Address{143}, SettlementVault: common.Address{144}, DeploymentIDHash: [32]byte{145}, PolicyHash: [32]byte{146}, NoID: 3}
	digest, err := domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.scope = connect.OriginalContractStoreScope{DomainHash: digest, ClientId: [16]byte{147}, SourceGeneration: [16]byte{148}}
	copy(f.scope.PublicKey[:], key[ed25519.SeedSize:])
	f.profile = miner.ProviderContractCaptureProfile{Schema: miner.ProviderContractCaptureSchema, ApiUrl: "https://synthetic.invalid", Providers: []miner.ProviderContractCaptureOwner{{Slot: "direct", ClientId: f.scope.ClientId, PublicKey: f.scope.PublicKey, Domain: domain, Directory: f.source.root, SourceGeneration: f.scope.SourceGeneration}}}
	f.owner = storageCreationProfileOwner(t, f.source, f.profile)
	storagePreparationOwnerRequest(t, f.source, "daemon", []durablevolume.PreparationOwner{f.owner})
	f.ctx = storagePreparationApplyOwnerCommand(t, f.source, "storage-prepare")
	return f
}

// The capture authority remains independently protected outside its originals.
func storageCreationProfileOwner(t *testing.T, source *storagePreparationCommandFixture, profile miner.ProviderContractCaptureProfile) durablevolume.PreparationOwner {
	t.Helper()
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(source.metadata, "original-creation-profile.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	inputs, err := json.Marshal(storageOriginalContractPreparationScope{Schema: storageOriginalContractPreparationSchema, CaptureProfile: durablevolume.Reference{Path: path, Sha256: safeReleaseHash(raw)}, Slot: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	return durablevolume.PreparationOwner{Kind: storageOriginalContractKind, RelativePath: ".", Purpose: "fresh", Inputs: inputs}
}

type storageCreationOob struct {
	directory string
	ordinary  *connect.NoContractClientOob
	callback  connect.OobResultFunction
	request   *coreprotocol.CreateContract
	original  []byte
	err       error
}

// The actual transport admits only a request whose exact frame is already kept.
func (self *storageCreationOob) SendControl(frames []*coreprotocol.Frame, callback connect.OobResultFunction) {
	if len(frames) != 1 || frames[0].MessageType != coreprotocol.MessageType_TransferCreateContract {
		self.ordinary.SendControl(frames, callback)
		return
	}
	defer connect.MessagePoolReturn(frames[0].MessageBytes)
	self.callback, self.request = callback, &coreprotocol.CreateContract{}
	if self.err = connect.ProtoUnmarshal(frames[0].MessageBytes, self.request); self.err != nil {
		return
	}
	frame, err := proto.Marshal(frames[0])
	if err != nil {
		self.err = err
		return
	}
	entries, err := os.ReadDir(self.directory)
	if err != nil {
		self.err = err
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "request-") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(self.directory, entry.Name()))
		if err != nil {
			self.err = err
			return
		}
		original, err := coreprotocol.DecodeOriginalContractRequest(context.Background(), raw)
		if err != nil {
			self.err = err
			return
		}
		if bytes.Equal(original.RequestFrame, frame) {
			self.original = raw
			return
		}
	}
	self.err = errors.New("actual request transport preceded original custody")
}

// A complete real lifecycle is joined before export. A later lifecycle has a
// new signed SDK generation while keeping the approved source birth unchanged.
func (self *storageCreationFixture) capture(t *testing.T) coreprotocol.OriginalContractRequest {
	t.Helper()
	settings := connect.DefaultClientSettings()
	settings.ControlPingTimeout = 0
	settings.Log = connect.NewNoopLogger()
	settings.EncryptionSettings.Mode = connect.EncryptionModeOff
	settings.ClientKeySeed = self.seed
	settings.ContractManagerSettings = connect.DefaultContractManagerSettingsNoNetworkEvents()
	settings.ContractManagerSettings.CloseReportDomainHash = self.scope.DomainHash
	settings.ContractManagerSettings.OriginalContractCapture = &connect.OriginalContractCaptureSettings{Directory: self.source.root, PublicKey: self.scope.PublicKey, SourceGeneration: self.scope.SourceGeneration}
	oob := &storageCreationOob{directory: self.source.root, ordinary: connect.NewNoContractClientOob()}
	client := connect.NewClient(t.Context(), connect.Id(self.scope.ClientId), oob, settings)
	join := func() error {
		if oob.callback != nil {
			callback := oob.callback
			oob.callback = nil
			callback(nil, errors.New("synthetic request fixture cleanup"))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return client.CloseAndWait(ctx)
	}
	t.Cleanup(func() {
		if err := join(); err != nil {
			t.Error(err)
		}
	})
	destination, contractId := connect.NewId(), connect.NewId()
	client.ContractManager().CreateContract(connect.ContractKey{Destination: connect.DestinationId(destination)}, 0, 100)
	if oob.err != nil || oob.callback == nil || len(oob.original) == 0 {
		t.Fatal("actual request did not retain a pre-send original", oob.err)
	}
	request, err := coreprotocol.DecodeOriginalContractRequest(t.Context(), oob.original)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := proto.Marshal(&coreprotocol.StoredContract{ContractId: contractId.Bytes(), SourceId: client.ClientId().Bytes(), DestinationId: destination.Bytes(), TransferByteCount: 80})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := connect.ToFrame(&coreprotocol.CreateContractResult{CreateContract: oob.request, Contract: &coreprotocol.Contract{StoredContractBytes: stored, ProvideMode: coreprotocol.ProvideMode_Public}}, connect.DefaultProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	callback := oob.callback
	oob.callback = nil
	callback([]*coreprotocol.Frame{frame}, nil)
	connect.MessagePoolReturn(frame.MessageBytes)
	cut, err := client.ContractManager().OriginalWorkCut(t.Context(), 7, 100, [32]byte{149})
	if err != nil || !cut.Complete || len(cut.Contracts) != 1 || cut.Contracts[0].ContractId != [16]byte(contractId) {
		t.Fatal("actual callback did not admit its original contract", err)
	}
	admission, err := coreprotocol.DecodeOriginalContractAdmission(t.Context(), cut.Contracts[0].OriginalCreation)
	if err != nil || !bytes.Equal(admission.Request, oob.original) {
		t.Fatal("actual SDK publication lost its original request closure", err)
	}
	if err := join(); err != nil {
		t.Fatal(err)
	}
	if err := connect.ValidateOriginalContractStore(t.Context(), self.source.root, self.scope); err != nil {
		t.Fatal("actual retained creation store cannot reopen", err)
	}
	return request
}

// Shared command runner preserves every archived file and grants no restart.
func applyStorageOriginalRestore(t *testing.T, storage *storageSnapshotRestoreFixture) context.Context {
	t.Helper()
	path, hash := storagePreparationFreezeOwnerPlan(t, storage.target, storage.command)
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public original restore apply failed", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("original restore invented restart authority", err)
	}
	entries, err := os.ReadDir(storage.heldSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		original, err := os.ReadFile(filepath.Join(storage.heldSource, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		restored, err := os.ReadFile(filepath.Join(storage.target.root, entry.Name()))
		if err != nil || !bytes.Equal(original, restored) {
			t.Fatal("public restore rewrote or dropped an original", entry.Name(), err)
		}
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), storage.target.storage.Host)
}

func TestStoragePreparationCreationFreshAndEmptyRestoreKeepApprovedBirth(t *testing.T) {
	f := newStorageCreationFixture(t)
	if err := connect.ValidateOriginalContractStore(t.Context(), f.source.root, f.scope); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	applyStorageOriginalRestore(t, storage)
	if err := connect.ValidateOriginalContractStore(t.Context(), f.source.root, f.scope); err != nil {
		t.Fatal("empty original birth did not survive real restore", err)
	}
	entries, err := os.ReadDir(f.source.root)
	if err != nil || len(entries) != 1 || entries[0].Name() != connect.OriginalContractStoreLeaseName {
		t.Fatal("empty restore invented creation evidence", entries, err)
	}
}

func TestStoragePreparationCreationRestoreKeepsActualOriginalsAcrossLifecycles(t *testing.T) {
	f := newStorageCreationFixture(t)
	first := f.capture(t)
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	applyStorageOriginalRestore(t, storage)
	second := f.capture(t)
	if first.Generation == second.Generation || first.PublicKey != second.PublicKey || first.ClientId != second.ClientId || first.DomainHash != second.DomainHash {
		t.Fatal("restored custody rewrote SDK generation or original approved owner")
	}
	entries, err := os.ReadDir(f.source.root)
	if err != nil || len(entries) != 5 {
		t.Fatal("cold lifecycle lost either request/admission original closure", entries, err)
	}
}

func TestStoragePreparationCreationRestoreRejectsForeignApprovedBirthBeforeCopy(t *testing.T) {
	f := newStorageCreationFixture(t)
	f.capture(t)
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	f.profile.Providers[0].SourceGeneration[0] ^= 1
	owner := storageCreationProfileOwner(t, storage.target, f.profile)
	owner.Purpose = "restore"
	storagePreparationOwnerRequest(t, storage.target, "daemon", []durablevolume.PreparationOwner{owner})
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "plan", "--request", storage.target.requestPath, "--request-sha256", storage.target.requestHash}, &output, &diagnostic); code == 0 {
		t.Fatal("a newly approved different source birth adopted old originals")
	}
	if entries, err := os.ReadDir(f.source.root); err != nil || len(entries) != 0 {
		t.Fatal("foreign birth plan changed empty replacement target", err)
	}
}

func TestStoragePreparationCreationRestoreRefusesMissingRequestClosure(t *testing.T) {
	f := newStorageCreationFixture(t)
	f.capture(t)
	entries, err := os.ReadDir(f.source.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "request-") {
			if err := os.Remove(filepath.Join(f.source.root, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	path, hash := storagePreparationFreezeOwnerPlan(t, storage.target, storage.command)
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code == 0 {
		t.Fatal("an admission without its original separate request acquired restore authority")
	}
	if _, err := unix.Getxattr(f.source.root, connect.OriginalContractStoreAttribute, make([]byte, 4096)); !errors.Is(err, durablesys.ErrNoAttribute) {
		t.Fatal("failed closure verification published a new birth marker", err)
	}
	for _, root := range []string{storage.heldSource, storage.archive} {
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 2 {
			t.Fatal("failed original closure verification discarded retained evidence", root, err)
		}
	}
}
