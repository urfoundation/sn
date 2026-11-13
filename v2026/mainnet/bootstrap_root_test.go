// Public command tests join real custody/service journals to synthetic signing
// and the existing local owned-http fixture. No live key or route is used.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Each fixture has separately private config and run directories. The native
// action is independently reapproved for its exact service/custody paths.
type bootstrapRootFixture struct {
	storage    *durablefixture.Fixture
	configPath string
	config     bootstrapRootConfig
	plan       bootstrapRootPlan
	offline    rootOfflineFixture
}

// File identity includes its final newline, independently of production hashes.
func bootstrapRootTestWrite(t *testing.T, path string, value any) planFileReference {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return planFileReference{Path: path, Sha256: "sha256:" + hex.EncodeToString(digest[:])}
}

// Existing fixture identities are synthetic; only their local approved paths
// change. The underlying native payload and independent approval stay checked.
func newBootstrapRootFixture(t *testing.T) *bootstrapRootFixture {
	t.Helper()
	return bootstrapRootFixtureFromOffline(t, newRootOfflineFixture(t))
}

// The same helper can start from the actual submission fixture's root action.
func bootstrapRootFixtureFromOffline(t *testing.T, offline rootOfflineFixture) *bootstrapRootFixture {
	t.Helper()
	runDirectory := filepath.Dir(offline.trust.StatePath)
	action := copyRootAction(offline.packet.Action)
	action.Scope.StatePath = filepath.Join(runDirectory, "root-service.json")
	offline.packet = rootOfflineApprove(t, offline.trust, action, offline.approvalKey)
	configDirectory := filepath.Join(t.TempDir(), "private-config")
	if err := os.Mkdir(configDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	service := rootServiceConfig{Schema: rootServiceConfigSchema, CustodyTrust: offline.trust, Packet: offline.packet, MaximumObservations: 3}
	fixture := &bootstrapRootFixture{storage: durablefixture.New(t, t.Context(), runDirectory), offline: offline, configPath: filepath.Join(configDirectory, "bootstrap.json"),
		config: bootstrapRootConfig{Schema: bootstrapRootConfigSchema, DeploymentId: "synthetic-mainnet-bootstrap", RunDirectory: runDirectory,
			Network:     planNetwork{NativeChain: action.Scope.NativeChain, GenesisHash: action.Scope.GenesisHash, EvmChainId: mainnetEvmChainId},
			RootService: bootstrapRootTestWrite(t, filepath.Join(configDirectory, "service.json"), service)}}
	bootstrapRootTestWrite(t, fixture.configPath, fixture.config)
	plan, err := loadBootstrapRootPlan(t.Context(), fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.plan = plan
	prepareMainnetSnapshotTest(t, filepath.Join(runDirectory, bootstrapRootProgressFile), "mainnet-bootstrap-root", 16*1024)
	prepareMainnetSnapshotTest(t, plan.Service.Packet.Action.Scope.StatePath, "mainnet-root-service", rootServiceStoreLimit)
	return fixture
}

// Calls the public dispatcher, preserving exact accepted plan/run identities.
func (self *bootstrapRootFixture) command(ctx context.Context, command string, stdout, stderr io.Writer, extra ...string) int {
	ctx = durablepath.WithHost(durablevolume.WithReference(ctx, self.storage.Reference), self.storage.Host)
	args := []string{"bootstrap", command, "--config", self.configPath}
	if command != "plan" {
		args = append(args, "--run-dir", self.config.RunDirectory, "--accept-plan-hash", self.plan.ContentHash)
	}
	return runMain(ctx, append(args, extra...), stdout, stderr)
}

// Public outputs are decoded strictly so local completion cannot hide a second
// undocumented authority document or a malformed partial result.
func (self *bootstrapRootFixture) result(t *testing.T, command string, extra ...string) bootstrapRootResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := self.command(t.Context(), command, &stdout, &stderr, extra...); code != 0 {
		t.Fatalf("bootstrap %s exit %d: %s", command, code, stderr.String())
	}
	var result bootstrapRootResult
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// A completed public handoff reopens directly in the existing service and owned
// http submitter. Only the test supplies current authority and the local chain.
func TestBootstrapRootCommandReachesExistingServiceAndHttp(t *testing.T) {
	submission := newRootSubmissionFixture(t)
	fixture := bootstrapRootFixtureFromOffline(t, submission.offline)
	submission.offline = fixture.offline
	submission.config.Service = fixture.plan.Service
	submission.config.Approval.ServiceConfigHash = rootObjectHash(fixture.plan.Service)
	submission.config.Approval.PacketHash = fixture.offline.packet.ContentHash
	submission.approve(t)
	prepared := mainnetNamespaceTest(t, fixture.config.RunDirectory)
	var stdout, stderr bytes.Buffer
	if code := fixture.command(t.Context(), "plan", &stdout, &stderr); code != 0 {
		t.Fatalf("plan exit %d: %s", code, stderr.String())
	}
	var plan bootstrapRootPlan
	if err := decodePlanJson(stdout.Bytes(), &plan); err != nil || !reflect.DeepEqual(plan, fixture.plan) || plan.NetworkEffects || plan.NativeSigning {
		t.Fatal("public plan changed immutable inputs or granted live effects", err)
	}
	if !reflect.DeepEqual(prepared, mainnetNamespaceTest(t, fixture.config.RunDirectory)) {
		t.Fatal("planning mutated prepared child custody")
	}
	applied := fixture.result(t, "apply")
	if !applied.LocalCustodyComplete || applied.SignatureStatus != "awaiting-import" || applied.NextPhase != "external-native-signature" ||
		applied.ActivationReady || !applied.ChainPhasesPending || applied.Packet.ContentHash != fixture.plan.Service.Packet.ContentHash || applied.Observations != 0 || applied.Broadcasts != 0 || len(submission.sent()) != 0 {
		t.Fatal("local request retention was not a completed, isolated custody phase")
	}
	receipt := fixture.offline.receipt(t)
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(fixture.configPath), "signature.json"), receipt)
	retained := fixture.result(t, "resume", "--signature-file", ref.Path, "--signature-sha256", ref.Sha256)
	if retained.SignatureStatus != "retained" || retained.ExtrinsicHash == "" || retained.ActivationReady || retained.Broadcasts != 0 || retained.ServicePhase != "observing" {
		t.Fatal("public signature import activated or lost its original local intent")
	}
	if repeated := fixture.result(t, "resume"); !reflect.DeepEqual(retained, repeated) {
		t.Fatal("signature recovery changed progress without another input")
	}
	submitter, submissionStore := submission.open(t, true)
	custody, custodyStore := fixture.offline.open(t, false)
	serviceStore, err := openRootServiceStore(fixture.plan.Service, false, fixture.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serviceStore.close() })
	position, err := submitter.reconcile(t.Context(), fixture.offline.packet.Action, nil)
	if err != nil {
		t.Fatal(err)
	}
	view := rootWeightObservation{Schema: rootWeightObservationSchema, Position: position.Observation, Enabled: true,
		ActiveNetworks: []uint16{0, 1, 2, 3, 4, 5, 6, 7}, StoredWeights: []rootStoredWeight{}, ConcentrationCap: 4096, StorageHash: rootObjectHash("synthetic bootstrap weight census")}
	service, err := newRootServiceOwner(fixture.plan.Service, serviceStore, rootServicePorts{Observer: &rootServiceObserverFixture{view: view}, Reconciler: submitter, Authority: submission.authority, Signer: custody, Submitter: submitter})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := service.step(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := submissionStore.load()
	if err != nil || len(submission.sent()) != 1 || actual.ExtrinsicHash != retained.ExtrinsicHash || actual.RawExtrinsic != submission.sent()[0] {
		t.Fatal("bootstrap artifacts did not progress through the actual owned submitter", err)
	}
	raw, _ := hex.DecodeString(actual.RawExtrinsic[2:])
	submission.finalize(t, raw)
	service.ports.Authority, service.ports.Signer, service.ports.Submitter, submitter.authority = nil, nil, nil, nil
	completed, err := service.step(t.Context())
	if err != nil || completed.Phase != "complete" || completed.Action.Phase != "finalized" {
		t.Fatal("composed original action lost its actual canonical receipt", err)
	}
	serviceStore.close()
	custodyStore.close()
	after := fixture.result(t, "resume")
	if after.ServicePhase != "complete" || after.ActionPhase != "finalized" || after.Observations != 1 || after.Broadcasts != 1 || after.NextPhase != "remaining-bootstrap-chain-phases" || after.ActivationReady || len(submission.sent()) != 1 {
		t.Fatal("local resume replaced the authoritative child's completed action or budget")
	}
}

// Mismatched confirmation, network or content pins fail before the run acquires
// a marker. Supplying a review-only graph hash never confirms this phase.
func TestBootstrapRootCommandRejectsUnapprovedInputsBeforeMutation(t *testing.T) {
	for _, change := range []string{"accepted-hash", "run-directory", "testnet", "genesis", "service-pin", "unsigned-config", "unknown-json", "duplicate-json", "missing-config"} {
		fixture := newBootstrapRootFixture(t)
		prepared := mainnetNamespaceTest(t, fixture.config.RunDirectory)
		args := []string{"bootstrap", "apply", "--config", fixture.configPath, "--run-dir", fixture.config.RunDirectory, "--accept-plan-hash", fixture.plan.ContentHash}
		switch change {
		case "accepted-hash":
			args[len(args)-1] = "sha256:" + strings.Repeat("a1", 32)
		case "run-directory":
			args[len(args)-3] = filepath.Dir(fixture.config.RunDirectory)
		case "testnet":
			fixture.config.Network.EvmChainId = 945
			bootstrapRootTestWrite(t, fixture.configPath, fixture.config)
		case "genesis":
			fixture.config.Network.GenesisHash = "0x" + strings.Repeat("a1", 32)
			bootstrapRootTestWrite(t, fixture.configPath, fixture.config)
		case "service-pin":
			fixture.config.RootService.Sha256 = "sha256:" + strings.Repeat("a1", 32)
			bootstrapRootTestWrite(t, fixture.configPath, fixture.config)
		case "unsigned-config":
			service := copyRootServiceConfig(fixture.plan.Service)
			service.Packet.Action.Scope.Coldkey = "0x" + strings.Repeat("a1", 32)
			fixture.config.RootService = bootstrapRootTestWrite(t, fixture.config.RootService.Path, service)
			bootstrapRootTestWrite(t, fixture.configPath, fixture.config)
		case "unknown-json", "duplicate-json":
			raw, _ := os.ReadFile(fixture.configPath)
			field := `"unapproved":true,`
			if change == "duplicate-json" {
				field = `"schema":"duplicate",`
			}
			if err := os.WriteFile(fixture.configPath, append([]byte("{"+field), raw[1:]...), 0600); err != nil {
				t.Fatal(err)
			}
		case "missing-config":
			if err := os.Remove(fixture.configPath); err != nil {
				t.Fatal(err)
			}
		}
		var stdout, stderr bytes.Buffer
		if code := runMain(fixture.storage.Context, args, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Fatalf("%s admitted or published partial authority: %d %s", change, code, stderr.String())
		}
		if !reflect.DeepEqual(prepared, mainnetNamespaceTest(t, fixture.config.RunDirectory)) {
			t.Fatalf("%s mutated prepared custody before approval", change)
		}
	}
}

// Output failure occurs after both real journals have been synced. A retry uses
// resume and retains their original bytes rather than repeating initialization.
func TestBootstrapRootOutputFailureResumesOriginalJournals(t *testing.T) {
	fixture := newBootstrapRootFixture(t)
	var stderr bytes.Buffer
	if code := fixture.command(t.Context(), "apply", bootstrapRootFailedWriter{}, &stderr); code != 1 {
		t.Fatal("output failure was not reported", code, stderr.String())
	}
	paths := []string{fixture.plan.Service.CustodyTrust.StatePath, fixture.plan.Service.Packet.Action.Scope.StatePath}
	before := make([][]byte, len(paths))
	for index, path := range paths {
		var err error
		before[index], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	result := fixture.result(t, "resume")
	if !result.LocalCustodyComplete || result.SignatureStatus != "awaiting-import" || result.Observations != 0 {
		t.Fatal("resume lost the completed local phase")
	}
	if code := fixture.command(t.Context(), "apply", io.Discard, &stderr); code == 0 {
		t.Fatal("apply reused a prior lifetime marker")
	}
	for index, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, before[index]) {
			t.Fatal("resume or repeated apply rewrote an authoritative child", err)
		}
	}
}

// A broken output sink cannot influence any child's durable state.
type bootstrapRootFailedWriter struct{}

// Deterministic failed publication has no scheduler or deadline dependency.
func (self bootstrapRootFailedWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic output failure")
}

// Injects a stopped progress write around real, already durable child effects.
// Barrier fields are configured before use and observed after joining.
type bootstrapRootProgressFailure struct {
	store   *bootstrapRootStore
	phase   string
	after   bool
	entered chan struct{}
	release chan struct{}
}

// Reads always use the actual strict private progress owner.
func (self *bootstrapRootProgressFailure) load() (bootstrapRootRecord, error) {
	return self.store.load()
}

// Neither side of a failed progress acknowledgement can undo a child journal.
func (self *bootstrapRootProgressFailure) save(record bootstrapRootRecord) error {
	if record.Phase != self.phase {
		return self.store.save(record)
	}
	if self.after {
		if err := self.store.save(record); err != nil {
			return err
		}
	}
	if self.entered != nil {
		close(self.entered)
		<-self.release
	}
	return errors.New("synthetic progress acknowledgement failure")
}

// The real custody/service owners remain authoritative on both sides of every
// completion write, including a signature imported before progress was saved.
func TestBootstrapRootReconcilesChildrenAfterAmbiguousProgress(t *testing.T) {
	for _, phase := range []string{"custody-retained", "service-retained", "signature-retained"} {
		for _, after := range []bool{false, true} {
			fixture := newBootstrapRootFixture(t)
			store, err := openBootstrapRootStore(fixture.plan, true, fixture.storage.Context)
			if err != nil {
				t.Fatal(err)
			}
			failure := &bootstrapRootProgressFailure{store: store, phase: phase, after: after}
			owner, err := newBootstrapRootOwner(fixture.plan, failure)
			if err != nil {
				t.Fatal(err)
			}
			receipt := fixture.offline.receipt(t)
			result, err := owner.advance(fixture.storage.Context, &receipt)
			if err == nil || result.Schema != "" || !owner.poisoned {
				t.Fatalf("%s after=%t published incomplete progress: %v", phase, after, err)
			}
			if _, err := owner.advance(fixture.storage.Context, nil); err == nil {
				t.Fatal("ambiguous owner continued before reopen")
			}
			store.close()
			resumed := fixture.result(t, "resume")
			wantSignature := "awaiting-import"
			if phase == "signature-retained" {
				wantSignature = "retained"
			}
			if !resumed.LocalCustodyComplete || resumed.SignatureStatus != wantSignature || resumed.Observations != 0 || resumed.Broadcasts != 0 {
				t.Fatalf("%s after=%t lost actual child state: %+v", phase, after, resumed)
			}
		}
	}
}

// Cancellation cannot overtake another phase owner or erase its completed
// child. Store closure is separately enforced even after another owner opens.
func TestBootstrapRootCancellationAndSingleOwnership(t *testing.T) {
	fixture := newBootstrapRootFixture(t)
	store, err := openBootstrapRootStore(fixture.plan, true, fixture.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if other, err := openBootstrapRootStore(fixture.plan, false, fixture.storage.Context); err == nil {
		other.close()
		t.Fatal("two process owners acquired the same plan")
	}
	failure := &bootstrapRootProgressFailure{store: store, phase: "custody-retained", entered: make(chan struct{}), release: make(chan struct{})}
	owner, err := newBootstrapRootOwner(fixture.plan, failure)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := owner.advance(fixture.storage.Context, nil); done <- err }()
	<-failure.entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := owner.advance(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled waiter entered the owned phase", err)
	}
	close(failure.release)
	if err := <-done; err == nil {
		t.Fatal("forced progress failure disappeared")
	}
	store.close()
	if _, err := store.load(); err == nil {
		t.Fatal("closed progress owner kept read capability")
	}
	resumed := fixture.result(t, "resume")
	if resumed.SignatureStatus != "awaiting-import" || resumed.Broadcasts != 0 {
		t.Fatal("interrupted progress invented native effects")
	}
	if err := store.save(bootstrapRootRecord{}); err == nil {
		t.Fatal("closed progress owner kept write capability")
	}
}

// Neither a missing child nor a partial original marker can replenish a
// completed phase. The remaining authoritative files must stay untouched.
func TestBootstrapRootMissingStateCannotCreateFreshAllowance(t *testing.T) {
	for _, target := range []string{"custody", "service", "progress"} {
		for _, both := range []bool{false, true} {
			fixture := newBootstrapRootFixture(t)
			fixture.result(t, "apply")
			path := filepath.Join(fixture.config.RunDirectory, bootstrapRootProgressFile)
			if target == "custody" {
				path = fixture.plan.Service.CustodyTrust.StatePath
			} else if target == "service" {
				path = fixture.plan.Service.Packet.Action.Scope.StatePath
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if both {
				if err := os.Remove(path + ".lock"); err != nil {
					t.Fatal(err)
				}
			}
			for _, command := range []string{"resume", "apply"} {
				var stdout, stderr bytes.Buffer
				if code := fixture.command(t.Context(), command, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
					t.Fatalf("%s %s both=%t recreated missing authority: %d", command, target, both, code)
				}
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing authoritative state was recreated", err)
			}
		}
	}
}

// Native verification and immutable custody reject forged, foreign and alternate
// valid public signatures. A restored unsigned child cannot be silently repaired.
func TestBootstrapRootSignatureImportPreservesOriginalBytes(t *testing.T) {
	fixture := newBootstrapRootFixture(t)
	fixture.result(t, "apply")
	unsigned, err := os.ReadFile(fixture.plan.Service.CustodyTrust.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	receipt := fixture.offline.receipt(t)
	path := filepath.Join(filepath.Dir(fixture.configPath), "signature.json")
	for _, change := range []string{"wrong-pin", "wrong-packet", "bad-signature", "unknown-field"} {
		candidate := receipt
		if change == "wrong-packet" {
			candidate.PacketHash = "sha256:" + strings.Repeat("a1", 32)
		}
		if change == "bad-signature" {
			candidate.Signature = strings.Repeat("a1", 64)
		}
		ref := bootstrapRootTestWrite(t, path, candidate)
		if change == "wrong-pin" {
			ref.Sha256 = "sha256:" + strings.Repeat("a1", 32)
		}
		if change == "unknown-field" {
			raw, _ := os.ReadFile(path)
			raw = append([]byte(`{"force":true,`), raw[1:]...)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			ref.Sha256 = "sha256:" + hex.EncodeToString(digest[:])
		}
		var stdout, stderr bytes.Buffer
		if code := fixture.command(t.Context(), "resume", &stdout, &stderr, "--signature-file", ref.Path, "--signature-sha256", ref.Sha256); code == 0 || stdout.Len() != 0 {
			t.Fatal("invalid public receipt was admitted", change)
		}
		if raw, err := os.ReadFile(fixture.plan.Service.CustodyTrust.StatePath); err != nil || !bytes.Equal(raw, unsigned) {
			t.Fatal("failed receipt import changed custody", change, err)
		}
	}
	ref := bootstrapRootTestWrite(t, path, receipt)
	retained := fixture.result(t, "resume", "--signature-file", ref.Path, "--signature-sha256", ref.Sha256)
	if repeated := fixture.result(t, "resume", "--signature-file", ref.Path, "--signature-sha256", ref.Sha256); !reflect.DeepEqual(retained, repeated) {
		t.Fatal("exact repeated signature changed progress")
	}
	alternate := fixture.offline.receipt(t)
	if alternate.Signature == receipt.Signature {
		t.Fatal("synthetic randomized native signer did not produce alternate bytes")
	}
	ref = bootstrapRootTestWrite(t, path, alternate)
	var stdout, stderr bytes.Buffer
	if code := fixture.command(t.Context(), "resume", &stdout, &stderr, "--signature-file", ref.Path, "--signature-sha256", ref.Sha256); code == 0 {
		t.Fatal("alternate valid signature replaced original bytes")
	}
	if err := os.WriteFile(fixture.plan.Service.CustodyTrust.StatePath, unsigned, 0600); err != nil {
		t.Fatal(err)
	}
	ref = bootstrapRootTestWrite(t, path, receipt)
	if code := fixture.command(t.Context(), "resume", io.Discard, &stderr, "--signature-file", ref.Path, "--signature-sha256", ref.Sha256); code == 0 {
		t.Fatal("progress silently repaired a regressed authoritative child")
	}
	if raw, _ := os.ReadFile(fixture.plan.Service.CustodyTrust.StatePath); !bytes.Equal(raw, unsigned) {
		t.Fatal("regressed child was mutated before continuity was checked")
	}
}

// Symlink aliases, public directories and marker/input collisions are rejected
// at the actual loading/owning boundary rather than a caller-selected label.
func TestBootstrapRootRejectsUnsafePathsAndState(t *testing.T) {
	for _, change := range []string{"public-run", "symlink-run", "symlink-config", "public-config", "journal-lock-alias", "input-journal-alias", "foreign-progress", "symlink-progress", "fifo-progress", "public-progress", "partial-marker"} {
		fixture := newBootstrapRootFixture(t)
		switch change {
		case "public-run":
			if err := os.Chmod(fixture.config.RunDirectory, 0755); err != nil {
				t.Fatal(err)
			}
		case "symlink-run":
			alias := filepath.Join(filepath.Dir(fixture.config.RunDirectory), "run-alias")
			if err := os.Symlink(fixture.config.RunDirectory, alias); err != nil {
				t.Fatal(err)
			}
			fixture.config.RunDirectory = alias
			bootstrapRootTestWrite(t, fixture.configPath, fixture.config)
		case "symlink-config":
			alias := fixture.configPath + ".alias"
			if err := os.Symlink(fixture.configPath, alias); err != nil {
				t.Fatal(err)
			}
			fixture.configPath = alias
		case "public-config":
			if err := os.Chmod(fixture.configPath, 0644); err != nil {
				t.Fatal(err)
			}
		case "journal-lock-alias", "input-journal-alias":
			plan := copyBootstrapRootPlan(fixture.plan)
			if change == "journal-lock-alias" {
				action := copyRootAction(plan.Service.Packet.Action)
				action.Scope.StatePath = filepath.Join(plan.RunDirectory, bootstrapRootProgressFile) + ".lock"
				plan.Service.Packet = rootOfflineApprove(t, fixture.offline.trust, action, fixture.offline.approvalKey)
			} else {
				plan.ConfigPath = plan.Service.CustodyTrust.StatePath
			}
			plan.ContentHash = bootstrapRootPlanHash(plan)
			if err := plan.validate(); err == nil {
				t.Fatal("mutable path alias passed", change)
			}
			continue
		case "foreign-progress", "symlink-progress", "fifo-progress", "public-progress", "partial-marker":
			fixture.result(t, "apply")
			path := filepath.Join(fixture.config.RunDirectory, bootstrapRootProgressFile)
			if change == "foreign-progress" {
				var record bootstrapRootRecord
				raw, _ := os.ReadFile(path)
				if err := json.Unmarshal(raw, &record); err != nil {
					t.Fatal(err)
				}
				record.PlanHash, record.ContentHash = "sha256:"+strings.Repeat("a1", 32), ""
				record.ContentHash = rootObjectHash(record)
				bootstrapRootTestWrite(t, path, record)
			} else if change == "symlink-progress" {
				copyPath := path + ".copy"
				if err := os.Rename(path, copyPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(copyPath, path); err != nil {
					t.Fatal(err)
				}
			} else if change == "fifo-progress" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			} else if change == "public-progress" {
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path+".lock", []byte("incomplete"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var stdout, stderr bytes.Buffer
		if code := fixture.command(t.Context(), "resume", &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Fatal("unsafe or foreign state passed", change, code)
		}
	}
}

// A post-rename directory-sync error is an actual ambiguous progress write;
// only reopening may recover it, and the child remains the source of truth.
func TestBootstrapRootDirectorySyncFailureReopensCompletedChild(t *testing.T) {
	fixture := newBootstrapRootFixture(t)
	store, err := openBootstrapRootStore(fixture.plan, true, fixture.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	store.syncDirectory = func(*os.File) error { return errors.New("synthetic directory sync failure") }
	owner, err := newBootstrapRootOwner(fixture.plan, store)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := owner.advance(fixture.storage.Context, nil); err == nil || result.LocalCustodyComplete || !owner.poisoned {
		t.Fatal("ambiguous actual publication did not stop the owner", err)
	}
	store.close()
	if result := fixture.result(t, "resume"); !result.LocalCustodyComplete || result.SignatureStatus != "awaiting-import" {
		t.Fatal("reopen lost the durable child after directory sync failure")
	}
}

// Both durable initial-claim boundaries can be interrupted before any child
// exists. Resume completes only that accepted claim and keeps child budgets.
func TestBootstrapRootInitialClaimRecovery(t *testing.T) {
	for _, stage := range []string{"marker-synced", "progress-synced"} {
		fixture := newBootstrapRootFixture(t)
		prepared := mainnetNamespaceTest(t, fixture.config.RunDirectory)
		failure := errors.New("synthetic initial claim interruption")
		store, err := openBootstrapRootStoreWithClaimHook(fixture.plan, true, func(boundary string) error {
			if boundary == stage {
				return failure
			}
			return nil
		}, fixture.storage.Context)
		if store != nil || !errors.Is(err, failure) {
			t.Fatalf("%s did not interrupt the initial claim: %v", stage, err)
		}
		path := filepath.Join(fixture.plan.RunDirectory, bootstrapRootProgressFile)
		marker, err := os.ReadFile(path + ".lock")
		if err != nil || string(marker) != fixture.plan.ContentHash+"\n" {
			t.Fatal("interrupted claim lost its exact accepted marker", err)
		}
		_, err = os.Lstat(path)
		if stage == "marker-synced" && !errors.Is(err, os.ErrNotExist) || stage == "progress-synced" && err != nil {
			t.Fatal("fault did not occur at the claimed progress boundary", stage, err)
		}
		afterClaim := mainnetNamespaceTest(t, fixture.config.RunDirectory)
		for _, child := range []string{fixture.plan.Service.CustodyTrust.StatePath, fixture.plan.Service.Packet.Action.Scope.StatePath} {
			if _, err := os.Lstat(child); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("initial claim created a child before completion", err)
			}
			name, err := filepath.Rel(fixture.config.RunDirectory, child+".lock")
			if err != nil || !reflect.DeepEqual(prepared[name], afterClaim[name]) {
				t.Fatal("initial claim changed a prepared child marker", child, err)
			}
		}
		// A second interruption during resume must leave the same recoverable
		// claim, rather than create a special one-use recovery allowance.
		store, err = openBootstrapRootStoreWithClaimHook(fixture.plan, false, func(boundary string) error {
			if boundary != "progress-synced" {
				t.Fatal("resume exposed an unexpected pre-child boundary", boundary)
			}
			return failure
		}, fixture.storage.Context)
		if store != nil || !errors.Is(err, failure) {
			t.Fatalf("accepted initial claim could not resume to its durable progress boundary: %v", err)
		}
		result := fixture.result(t, "resume")
		if !result.LocalCustodyComplete || result.SignatureStatus != "awaiting-import" || result.Observations != 0 || result.Broadcasts != 0 {
			t.Fatal("recovered initial claim renewed a child budget or lost local completion")
		}
		marker, err = os.ReadFile(path + ".lock")
		if err != nil || string(marker) != fixture.plan.ContentHash+"\n"+bootstrapRootClaimComplete {
			t.Fatal("child opened without durable claim-complete marker", err)
		}
		if again := fixture.result(t, "resume"); !reflect.DeepEqual(again, result) {
			t.Fatal("repeat resume changed recovered child state")
		}
	}
}

// Initial recovery never repairs corruption or assumes that an existing child
// was unused. Even a completed marker without children cannot lose its record.
func TestBootstrapRootInitialClaimRejectsAmbiguousState(t *testing.T) {
	for _, change := range []string{"empty-progress", "corrupt-progress", "advanced-progress", "custody-file", "custody-marker", "service-file", "service-marker", "partial-marker", "complete-without-progress"} {
		fixture := newBootstrapRootFixture(t)
		failure := errors.New("synthetic initial marker interruption")
		store, err := openBootstrapRootStoreWithClaimHook(fixture.plan, true, func(string) error { return failure }, fixture.storage.Context)
		if store != nil || !errors.Is(err, failure) {
			t.Fatal("failed to retain actual interrupted claim", err)
		}
		path := filepath.Join(fixture.plan.RunDirectory, bootstrapRootProgressFile)
		changedPath, changedBytes := path, []byte{}
		switch change {
		case "corrupt-progress":
			changedBytes = []byte(`{"schema":"unknown"}`)
		case "advanced-progress":
			record := bootstrapRootRecord{Schema: bootstrapRootStateSchema, PlanHash: fixture.plan.ContentHash, Phase: "custody-retained"}
			record.ContentHash = rootObjectHash(record)
			changedBytes, err = json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
		case "custody-file", "custody-marker":
			changedPath = fixture.plan.Service.CustodyTrust.StatePath
		case "service-file", "service-marker":
			changedPath = fixture.plan.Service.Packet.Action.Scope.StatePath
		case "partial-marker":
			changedPath, changedBytes = path+".lock", []byte(fixture.plan.ContentHash+"\nclaim")
		case "complete-without-progress":
			changedPath, changedBytes = path+".lock", []byte(fixture.plan.ContentHash+"\n"+bootstrapRootClaimComplete)
		}
		if change == "custody-marker" || change == "service-marker" {
			changedPath += ".lock"
			// The original empty precreated marker is approved fresh custody.
			// A foreign nonempty claim is the ambiguous state being rejected.
			changedBytes = []byte("synthetic unknown child claim\n")
		}
		if err := os.WriteFile(changedPath, changedBytes, 0600); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := fixture.command(t.Context(), "resume", &stdout, &stderr); code != 3 || stdout.Len() != 0 {
			t.Fatalf("%s ambiguous initial claim was repaired: exit=%d error=%s", change, code, stderr.String())
		}
		after, err := os.ReadFile(path + ".lock")
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rejected initial claim changed its marker", change, err)
		}
		retained, err := os.ReadFile(changedPath)
		if err != nil || !bytes.Equal(changedBytes, retained) {
			t.Fatal("rejected initial claim rewrote retained state", change, err)
		}
		if changedPath != path {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rejected initial claim created progress", change, err)
			}
		}
	}
}

// Produced facts can be consumed only after a producing ancestor. A future
// output cannot be made available by listing its digest in a review manifest.
func TestBootstrapDependenciesSeparateInputsAndProducedOutcomes(t *testing.T) {
	requirements, actions := bootstrapRequirements(), bootstrapActions()
	if err := validateBootstrapDependencies(requirements, actions); err != nil {
		t.Fatal("default graph recreates a circular bootstrap gate", err)
	}
	for _, action := range actions {
		for _, output := range action.Postconditions {
			for _, input := range action.Requirements {
				if input == output {
					t.Fatal("action requires its own outcome", action.Id, output)
				}
			}
		}
	}
	for _, gate := range []struct{ action, input string }{
		{action: "qualify-release", input: "deployed-contract-state"},
		{action: "start-ur-validators", input: "ur-validator-rows"},
		{action: "activate-native-miner-emissions", input: "native-emission-outcomes"},
	} {
		changed := bootstrapActions()
		for index := range changed {
			if changed[index].Id == gate.action {
				changed[index].Requirements = append(changed[index].Requirements, gate.input)
			}
		}
		if err := validateBootstrapDependencies(requirements, changed); err == nil {
			t.Fatalf("future %s outcome blocked its own bootstrap predecessor %s without rejection", gate.input, gate.action)
		}
	}
}
