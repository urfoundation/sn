//go:build linux

package main

// Public preparation creates birth, an actual SDK lifecycle captures originals,
// and public export/restore returns those same bytes to the actual SDK worker.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/miner"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"golang.org/x/sys/unix"
)

type storageSdkWorkFixture struct {
	source  *storagePreparationCommandFixture
	ctx     context.Context
	owner   durablevolume.PreparationOwner
	profile miner.ProviderWorkCaptureProfile
	scope   connect.OriginalWorkOutboxScope
	seed    []byte
	server  *httptest.Server
	cuts    chan []byte
	errors  chan error
	request atomic.Bool
}

func newStorageSdkWorkFixture(t *testing.T) *storageSdkWorkFixture {
	t.Helper()
	fixture := &storageSdkWorkFixture{source: newStoragePreparationCommandFixture(t), seed: bytes.Repeat([]byte{91}, ed25519.SeedSize), cuts: make(chan []byte, 4), errors: make(chan error, 4)}
	providerKey := ed25519.NewKeyFromSeed(fixture.seed)
	requestKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{92}, ed25519.SeedSize))
	domain := protocol.ClientKeyHistoryDomain{ChainID: 31337, GenesisHash: [32]byte{93}, Netuid: 7, Coordinator: common.Address{94}, SettlementVault: common.Address{95}, DeploymentIDHash: [32]byte{96}, PolicyHash: [32]byte{97}, NoID: 3}
	domainHash, err := domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	fixture.scope = connect.OriginalWorkOutboxScope{DomainHash: domainHash, ClientId: [16]byte{98}}
	copy(fixture.scope.PublicKey[:], providerKey[ed25519.SeedSize:])
	copy(fixture.scope.RequestPublicKey[:], requestKey[ed25519.SeedSize:])
	fail := func(writer http.ResponseWriter, err error) {
		select {
		case fixture.errors <- err:
		default:
		}
		http.Error(writer, "synthetic original custody failure", http.StatusBadRequest)
	}
	fixture.server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/provider-work/v1/owners":
			raw, err := io.ReadAll(io.LimitReader(request.Body, coreprotocol.MaximumOriginalWorkOwnerBytes+1))
			if err != nil {
				fail(writer, err)
				return
			}
			owner, err := coreprotocol.DecodeOriginalWorkOwnerEnrollment(request.Context(), raw)
			if err != nil {
				fail(writer, err)
				return
			}
			if owner.DomainHash != fixture.scope.DomainHash || owner.ClientId != fixture.scope.ClientId || owner.PublicKey != fixture.scope.PublicKey {
				fail(writer, errors.New("actual SDK enrollment changed independent owner scope"))
				return
			}
			_ = json.NewEncoder(writer).Encode(coreprotocol.OriginalWorkOwnerReceipt{Schema: coreprotocol.OriginalWorkOwnerReceiptSchema, OwnerHash: sha256.Sum256(raw)})
		case "/provider-work/v1/requests":
			response := coreprotocol.OriginalWorkRequests{Schema: coreprotocol.OriginalWorkRequestsSchema, Requests: [][]byte{}}
			if fixture.request.Load() {
				generation, err := hex.DecodeString(request.URL.Query().Get("generation"))
				if err != nil || len(generation) != 16 {
					fail(writer, errors.Join(err, errors.New("actual SDK generation query is absent")))
					return
				}
				now := time.Now().Unix()
				value := coreprotocol.OriginalWorkRequest{RequestId: [16]byte{99}, DomainHash: fixture.scope.DomainHash, ClientId: fixture.scope.ClientId, PublicKey: fixture.scope.PublicKey, Epoch: 11, Kind: "start", Block: 151, BlockHash: [32]byte{100}, IssuedAtUnix: now - 10, ExpiresAtUnix: now + 600}
				copy(value.Generation[:], generation)
				value, err = coreprotocol.SignOriginalWorkRequest(value, requestKey)
				if err != nil {
					fail(writer, err)
					return
				}
				raw, err := value.Bytes()
				if err != nil {
					fail(writer, err)
					return
				}
				response.Requests = [][]byte{raw}
			}
			_ = json.NewEncoder(writer).Encode(response)
		case "/provider-work/v1/cuts":
			raw, err := io.ReadAll(io.LimitReader(request.Body, coreprotocol.MaximumOriginalWorkSubmissionBytes+1))
			if err != nil {
				fail(writer, err)
				return
			}
			var submission coreprotocol.OriginalWorkCutSubmission
			if err := json.Unmarshal(raw, &submission); err != nil {
				fail(writer, err)
				return
			}
			receipt, err := coreprotocol.VerifyOriginalWorkSubmission(request.Context(), submission, fixture.scope.RequestPublicKey)
			if err != nil {
				fail(writer, err)
				return
			}
			fixture.request.Store(false)
			_ = json.NewEncoder(writer).Encode(receipt)
			select {
			case fixture.cuts <- bytes.Clone(raw):
			default:
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(fixture.server.Close)
	fixture.profile = miner.ProviderWorkCaptureProfile{Schema: miner.ProviderWorkCaptureSchema, ApiUrl: fixture.server.URL, RequestPublicKey: fixture.scope.RequestPublicKey, Providers: []miner.ProviderWorkCaptureOwner{{Slot: "direct", ClientId: fixture.scope.ClientId, PublicKey: fixture.scope.PublicKey, Domain: domain, OutboxDirectory: fixture.source.root}}}
	profileRaw, err := json.Marshal(fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(fixture.source.metadata, "sdk-capture-profile.json")
	if err := os.WriteFile(profilePath, profileRaw, 0600); err != nil {
		t.Fatal(err)
	}
	inputs, err := json.Marshal(storageSdkWorkPreparationScope{Schema: storageSdkWorkPreparationSchema, CaptureProfile: durablevolume.Reference{Path: profilePath, Sha256: safeReleaseHash(profileRaw)}, Slot: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	fixture.owner = durablevolume.PreparationOwner{Kind: storageSdkWorkKind, RelativePath: ".", Purpose: "fresh", Inputs: inputs}
	storagePreparationOwnerRequest(t, fixture.source, "daemon", []durablevolume.PreparationOwner{fixture.owner})
	fixture.ctx = storagePreparationApplyOwnerCommand(t, fixture.source, "storage-prepare")
	return fixture
}

// The real SDK owns capture/delivery. A returned submission is observed only
// after durable retention; CloseAndWait joins its exact filesystem lease owner.
func (self *storageSdkWorkFixture) deliver(t *testing.T, requestNew bool) []byte {
	t.Helper()
	self.request.Store(requestNew)
	settings := connect.DefaultClientSettings()
	settings.ControlPingTimeout = 0
	settings.Log = connect.NewNoopLogger()
	settings.EncryptionSettings.Mode = connect.EncryptionModeOff
	settings.ClientKeySeed = self.seed
	settings.ContractManagerSettings = connect.DefaultContractManagerSettingsNoNetworkEvents()
	settings.ContractManagerSettings.CloseReportDomainHash = self.scope.DomainHash
	settings.ContractManagerSettings.OriginalWorkCapture = &connect.OriginalWorkCaptureSettings{ApiUrl: self.server.URL, OutboxDirectory: self.source.root, RequestPublicKey: self.scope.RequestPublicKey, PublicKey: self.scope.PublicKey, HttpClient: self.server.Client()}
	client := connect.NewClient(t.Context(), connect.Id(self.scope.ClientId), connect.NewNoContractClientOob(), settings)
	join := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return client.CloseAndWait(ctx)
	}
	t.Cleanup(func() {
		if err := join(); err != nil {
			t.Error(err)
		}
	})
	owner, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var raw []byte
	select {
	case raw = <-self.cuts:
	case err := <-self.errors:
		t.Fatal(err)
	case <-owner.Done():
		t.Fatal("actual SDK original capture/delivery did not complete", owner.Err())
	}
	if err := join(); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStoragePreparationSdkFreshBirthAdmitsActualReadOnlyOwner(t *testing.T) {
	f := newStorageSdkWorkFixture(t)
	if err := connect.ValidateOriginalWorkOutbox(t.Context(), f.source.root, f.scope); err != nil {
		t.Fatal("explicit preparation cannot admit actual empty SDK owner", err)
	}
	entries, err := os.ReadDir(f.source.root)
	if err != nil || len(entries) != 1 || entries[0].Name() != connect.OriginalWorkOutboxIndexName {
		t.Fatal("fresh preparation invented captured work", entries, err)
	}
	other := t.TempDir()
	if err := os.Chmod(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := connect.ValidateOriginalWorkOutbox(t.Context(), other, f.scope); !errors.Is(err, connect.ErrOriginalWorkOutboxIdentity) {
		t.Fatal("empty unprepared root became authentic SDK birth", err)
	}
	if entries, err := os.ReadDir(other); err != nil || len(entries) != 0 {
		t.Fatal("runtime admission created missing custody", entries, err)
	}
}

// Empty original birth survives copying without being interpreted as no owner.
func TestStoragePreparationSdkEmptyRestoreKeepsOriginalBirth(t *testing.T) {
	f := newStorageSdkWorkFixture(t)
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	path, digest := storagePreparationFreezeOwnerPlan(t, storage.target, storage.command)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan durablevolume.PreparationPlan
	if err := json.Unmarshal(raw, &plan); err != nil || len(plan.Derivations) != 0 || len(plan.Sources) != 1 || plan.Sources[0].File.Path != connect.OriginalWorkOutboxIndexName || plan.Sources[0].File.Bytes != 0 {
		t.Fatal("empty original birth acquired invented metadata or work", err)
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "apply", "--plan", path, "--plan-sha256", digest}, &output, &diagnostic); code != 0 {
		t.Fatal("empty original SDK birth did not restore", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("empty SDK restore invented restart approval", err)
	}
	if err := connect.ValidateOriginalWorkOutbox(t.Context(), f.source.root, f.scope); err != nil {
		t.Fatal("copied empty birth cannot admit its actual SDK owner", err)
	}
	entries, err := os.ReadDir(f.source.root)
	if err != nil || len(entries) != 1 || entries[0].Name() != connect.OriginalWorkOutboxIndexName {
		t.Fatal("empty restore invented a captured original", entries, err)
	}
	for _, root := range []string{storage.heldSource, f.source.root} {
		original, err := os.ReadFile(filepath.Join(root, connect.OriginalWorkOutboxIndexName))
		if err != nil || len(original) != 0 {
			t.Fatal("restore changed empty original index bytes", root, err)
		}
	}
}

// The public command keeps signed cut bytes while rebinding only inode custody.
func TestStoragePreparationSdkRestoreRetainsActualRuntimeOriginals(t *testing.T) {
	f := newStorageSdkWorkFixture(t)
	original := f.deliver(t, true)
	if err := connect.ValidateOriginalWorkOutbox(t.Context(), f.source.root, f.scope); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	path, digest := storagePreparationFreezeOwnerPlan(t, storage.target, storage.command)
	planRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan durablevolume.PreparationPlan
	if err := json.Unmarshal(planRaw, &plan); err != nil || len(plan.Derivations) != 1 || plan.Derivations[0].Original.File.Path != connect.OriginalWorkOutboxIndexName {
		t.Fatal("SDK restore lost original physical index ancestry", err)
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "apply", "--plan", path, "--plan-sha256", digest}, &output, &diagnostic); code != 0 {
		t.Fatal("public SDK restore did not apply", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("SDK restore invented restart approval", err)
	}
	if err := connect.ValidateOriginalWorkOutbox(t.Context(), f.source.root, f.scope); err != nil {
		t.Fatal("restored originals failed actual SDK owner admission", err)
	}
	if restored := f.deliver(t, false); !bytes.Equal(restored, original) {
		t.Fatal("actual SDK restart re-signed retained original")
	}
	retained, err := os.ReadFile(plan.Derivations[0].Original.Path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(storage.heldSource, connect.OriginalWorkOutboxIndexName))
	if err != nil || !bytes.Equal(retained, before) {
		t.Fatal("restore rewrote original index source", err)
	}
	for _, source := range plan.Sources {
		if source.File.Path == connect.OriginalWorkOutboxIndexName {
			continue
		}
		before, err := os.ReadFile(filepath.Join(storage.heldSource, source.File.Path))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(f.source.root, source.File.Path))
		if err != nil || !bytes.Equal(before, after) || !bytes.Equal(after, original) {
			t.Fatal("restore changed signed request or cut bytes", err)
		}
		info, err := os.Stat(filepath.Join(f.source.root, source.File.Path))
		if err != nil || info.Mode().Perm() != 0400 {
			t.Fatal("restore broadened immutable original protection", err)
		}
	}
	output.Reset()
	diagnostic.Reset()
	if code := runMain(storage.target.ctx, []string{storage.command, "apply", "--plan", path, "--plan-sha256", digest}, &output, &diagnostic); code != 0 {
		t.Fatal("completed SDK restore did not continue exactly", code, diagnostic.String())
	}
}

// Independent approval and the complete original census precede target writes.
func TestStoragePreparationSdkRestoreRefusesMissingOriginalAndForeignScope(t *testing.T) {
	for _, change := range []string{"deleted-leaf", "missing-birth", "foreign-client", "foreign-key", "foreign-domain", "foreign-approver"} {
		func() {
			f := newStorageSdkWorkFixture(t)
			f.deliver(t, true)
			switch change {
			case "deleted-leaf":
				entries, err := os.ReadDir(f.source.root)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry.Name() != connect.OriginalWorkOutboxIndexName {
						if err := os.Remove(filepath.Join(f.source.root, entry.Name())); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "missing-birth":
				if err := unix.Removexattr(f.source.root, connect.OriginalWorkOutboxAttribute); err != nil {
					t.Fatal(err)
				}
			case "foreign-client", "foreign-key", "foreign-domain", "foreign-approver":
				var inputs storageSdkWorkPreparationScope
				if err := json.Unmarshal(f.owner.Inputs, &inputs); err != nil {
					t.Fatal(err)
				}
				switch change {
				case "foreign-client":
					f.profile.Providers[0].ClientId[0]++
				case "foreign-key":
					f.profile.Providers[0].PublicKey[0]++
				case "foreign-domain":
					f.profile.Providers[0].Domain.NoID++
				case "foreign-approver":
					f.profile.RequestPublicKey[0]++
				}
				raw, err := json.Marshal(f.profile)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(inputs.CaptureProfile.Path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				inputs.CaptureProfile.Sha256 = safeReleaseHash(raw)
				f.owner.Inputs, err = json.Marshal(inputs)
				if err != nil {
					t.Fatal(err)
				}
			}
			storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
			var output, diagnostic bytes.Buffer
			if code := runMain(storage.target.ctx, []string{storage.command, "plan", "--request", storage.target.requestPath, "--request-sha256", storage.target.requestHash}, &output, &diagnostic); code == 0 || output.Len() != 0 {
				t.Fatal("invalid SDK original became restorable custody", change, code, diagnostic.String())
			}
			if entries, err := os.ReadDir(storage.target.root); err != nil || len(entries) != 0 {
				t.Fatal("refused SDK restore mutated target", change, entries, err)
			}
		}()
	}
}
