//go:build linux || darwin

// Actual original requests, physical checkpoint bytes and real dual HTTP
// stores exercise closure, restart, resource limits and publication ownership.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
)

// A single actual trail precedes 130 empty closed windows. Successful live
// admission reads each new interval, while cold reopen still proves all bytes.
func TestProviderRequestPublicationDoesNotReplayPriorWindowPrefixes(t *testing.T) {
	engine, journal, preparation, path := providerRequestTestOwner(t, 32)
	if _, err := engine.RunTrail(t.Context()); err != nil {
		t.Fatal(err)
	}
	reads := 0
	journal.step = func(phase string) error {
		if phase == "provider-request-window-record" {
			reads++
		}
		return nil
	}
	for index := uint64(0); index < 130; index++ {
		window := protocol.ValidatorEvidenceWindow{Epoch: preparation.Birth.SettlementEpoch + index, StartBlock: preparation.Birth.EVMBlock + index, EndBlock: preparation.Birth.EVMBlock + index + 1, FinalizedBlock: preparation.Birth.EVMBlock + index + 1}
		cut, err := journal.SealWindow(t.Context(), window, 1024*1024)
		if err != nil || cut == nil {
			t.Fatalf("actual request window %d: %v", index, err)
		}
		if index > 0 && len(cut.Records) != 0 {
			t.Fatal("idle window repeated old original requests")
		}
	}
	if reads != 8 {
		t.Fatalf("closed windows rescanned admitted original prefixes: %d", reads)
	}
	raw, err := os.ReadFile(filepath.Join(path, ProviderAttemptRequestJournalName))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := VerifyProviderAttemptRequestPrefix(t.Context(), *journal.checkpoint, preparation, bytes.NewReader(raw), nil); err != nil || result == nil {
		t.Fatal("final complete custody was omitted", err)
	}
}

// Build an older signed close over the first actual trail while the full
// original prefix contains a second trail in that same epoch.
func providerRequestStaleCloseTest(t *testing.T) (*ProviderAttemptRequestJournal, ProviderAttemptRequestPreparation, []byte, ProviderAttemptRequestClosedHead, []ProviderAttemptRequestRecord) {
	t.Helper()
	engine, journal, preparation, path := providerRequestTestOwner(t, 32)
	for index := 0; index < 2; index++ {
		if _, err := engine.RunTrail(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: preparation.Birth.SettlementEpoch, StartBlock: preparation.Birth.EVMBlock, EndBlock: preparation.Birth.EVMBlock + 1, FinalizedBlock: preparation.Birth.EVMBlock + 1}
	cut, err := journal.SealWindow(t.Context(), window, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(path, ProviderAttemptRequestJournalName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyProviderAttemptRequestPrefix(t.Context(), *journal.checkpoint, preparation, bytes.NewReader(raw), nil); err != nil {
		t.Fatal("complete positive prefix", err)
	}
	header := copyClosedProviderRequestTestHead(cut.Header)
	digest := sha256.New()
	var head ProviderAttemptRequestHead
	for _, record := range cut.Records[:8] {
		wire, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := record.Hash()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = digest.Write(wire)
		_, _ = digest.Write([]byte{'\n'})
		head = ProviderAttemptRequestHead{Sequence: record.Sequence, Hash: hash, Bytes: head.Bytes + uint64(len(wire)) + 1, LastBoundary: record.Boundary}
	}
	header.End = head
	copy(header.RecordsHash[:], digest.Sum(nil))
	signing, err := header.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	header.Signature = ed25519.Sign(journal.key, signing)
	return journal, preparation, raw, header, cut.Records
}

// A valid old signature cannot hide subsequent originals from cold admission.
func TestProviderRequestColdPrefixRejectsPostCloseSameEpochOriginals(t *testing.T) {
	journal, preparation, raw, closed, _ := providerRequestStaleCloseTest(t)
	checkpoint := *journal.checkpoint
	checkpoint.Closed = &closed
	if result, err := VerifyProviderAttemptRequestPrefix(t.Context(), checkpoint, preparation, bytes.NewReader(raw), nil); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("post-close original prefix admitted: %v", err)
	}
}

// Pending bytes must obey the same fence before recovery can publish them.
func TestProviderRequestColdPendingRejectsPostCloseSameEpochOriginal(t *testing.T) {
	journal, preparation, raw, closed, records := providerRequestStaleCloseTest(t)
	checkpoint := *journal.checkpoint
	checkpoint.Closed = &closed
	checkpoint.Committed = closed.End
	pending, err := json.Marshal(records[8])
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.Pending = &ProviderAttemptRequestPending{Bytes: uint64(len(pending)), Hash: sha256.Sum256(pending)}
	if result, err := VerifyProviderAttemptRequestPrefix(t.Context(), checkpoint, preparation, bytes.NewReader(raw[:closed.End.Bytes]), pending); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("post-close pending original admitted: %v", err)
	}
}

// The signer is valid; only the declared original payload hash is different.
func TestProviderRequestColdPrefixAuthenticatesClosedPayloadHash(t *testing.T) {
	journal, preparation, raw, _, _ := providerRequestStaleCloseTest(t)
	checkpoint := *journal.checkpoint
	closed := copyClosedProviderRequestTestHead(*checkpoint.Closed)
	closed.RecordsHash[0] ^= 1
	signing, err := closed.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	closed.Signature = ed25519.Sign(journal.key, signing)
	checkpoint.Closed = &closed
	if result, err := VerifyProviderAttemptRequestPrefix(t.Context(), checkpoint, preparation, bytes.NewReader(raw), nil); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("signed changed closed payload admitted: %v", err)
	}
}

// A caller cannot use a valid close header to sign a different actual request.
func TestProviderRequestCloseConsentRequiresExactClosedPayload(t *testing.T) {
	journal, _, _, _, records := providerRequestStaleCloseTest(t)
	cut := &ProviderAttemptRequestWindow{Header: copyClosedProviderRequestTestHead(*journal.checkpoint.Closed), Records: append([]ProviderAttemptRequestRecord(nil), records...)}
	cut.Records[0] = cut.Records[8]
	genesis, err := canonicalAttemptHex32("fixture", journal.identity.Ledger.GenesisHash, false)
	if err != nil {
		t.Fatal(err)
	}
	scope := protocol.ProviderAttemptReceiptScope{Profile: "synthetic", GenesisHash: genesis, DeploymentId: journal.identity.Ledger.DeploymentID, DeploymentKey: "synthetic", PolicyHash: journal.identity.PolicyHash, Netuid: uint64(journal.identity.Ledger.Netuid), NoId: journal.identity.Ledger.NoID}
	if err := journal.CloseRequests(t.Context(), cut, scope); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || cut.Closures != nil {
		t.Fatalf("close consent signed a substituted original: %v", err)
	}
}

// Exact immutable local bytes survive an uploaded-but-lost reply, and both
// actual public origins return those same bytes after retry.
func TestProviderRequestPublicationLostReplyRetainsExactOriginal(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, true)
	cut := fixture.response.Requests[0].Windows[0]
	raw, err := json.Marshal(cut)
	if err != nil {
		t.Fatal(err)
	}
	path, err := ProviderAttemptPublicationPath(newAttemptSettlementRuntimeV2TestStateDir(t), cut.Header.Window.Epoch, cut.Header.Preparation.Identity.Ledger.NoID)
	if err != nil {
		t.Fatal(err)
	}
	preparation := ProviderAttemptPublicationPreparation{StateDir: filepath.Dir(filepath.Dir(path)), MaxWindowBytes: 1024 * 1024, MaxHistoryBytes: 4 * 1024 * 1024, MaxFiles: 32, Operators: []ProviderAttemptPublicationOperator{{Preparation: cut.Header.Preparation, ReceiptScope: fixture.response.Authority.Validators[0].Operators[0].ReceiptScope}}}
	namespace := prepareProviderPublicationTestNamespace(t, preparation)
	if err := retainProviderAttemptPublication(t.Context(), path, raw, 1024*1024, 4*1024*1024, 32, namespace); err != nil {
		t.Fatal(err)
	}
	replicas := slices.Clone(fixture.base.owners[0].fixture.replicas)
	owned := replicas[1].WriteMetadata
	lost := errors.New("synthetic original upload reply lost")
	replicas[1].WriteMetadata = func(ctx context.Context, hash string, body []byte) error {
		if err := owned(ctx, hash, body); err != nil {
			return err
		}
		return lost
	}
	bounds := fixture.base.owners[0].fixture.operators[0].seal.bounds
	if err := replicateProviderAttemptPublication(t.Context(), raw, replicas, bounds, 1024*1024); !errors.Is(err, lost) {
		t.Fatal("lost original upload reply not retained", err)
	}
	replicas[1].WriteMetadata = owned
	retained, err := readReleaseMeasurementInputV2Context(t.Context(), path, 1024*1024, releaseMeasurementInputV2ReadHooks{})
	if err != nil || !bytes.Equal(retained, raw) {
		t.Fatal("original publication changed after lost reply", err)
	}
	if err := replicateProviderAttemptPublication(t.Context(), retained, replicas, bounds, 1024*1024); err != nil {
		t.Fatal(err)
	}
	if err := retainProviderAttemptPublication(t.Context(), path, append(bytes.Clone(raw), ' '), 1024*1024, 4*1024*1024, 32, namespace); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatal("immutable request publication was replaced", err)
	}
}

// The actual runtime closes a nonempty epoch and its idle successor, then its
// request publisher uses original protected source reservations at both APIs.
func TestProviderRequestProductionRuntimePublishesClosedAndIdleWindows(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	index := 0
	input := fixture.startup.inputs[index]
	engine := fixture.startup.engines[index]
	op := fixture.startup.cfg.Operators[index]
	policy, err := parseHash32("fixture", fixture.startup.cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	preparation := ProviderAttemptRequestPreparation{Identity: ProviderAttemptRequestIdentity{Ledger: engine.cfg.AttemptLedger.identity, Coordinator: fixture.startup.cfg.Coordinator, ClientId: engine.clientId, PolicyHash: policy}, Limits: ProviderAttemptRequestLimits{MaxRecords: 1000, MaxRecordBytes: 8192, MaxJournalBytes: 8 * 1024 * 1024}, Birth: fixture.startup.boundary}
	prepareProviderRequestTestOwner(t, op.StateDir, preparation)
	journal, err := OpenProviderAttemptRequestJournal(t.Context(), op.StateDir, preparation, input.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	engine.cfg.RequestJournal = journal
	engine.cfg.StepTimeout = time.Minute
	fixture.runtimes[index].engine = engine
	genesis, err := parseHash32("fixture", fixture.startup.cfg.GenesisHash)
	if err != nil {
		t.Fatal(err)
	}
	op.RequestReceiptScope = &protocol.ProviderAttemptReceiptScope{Profile: "synthetic", GenesisHash: genesis, DeploymentId: fixture.startup.cfg.DeploymentID, DeploymentKey: fmt.Sprintf("%d:%s", fixture.startup.cfg.ChainID, fixture.startup.cfg.Coordinator), PolicyHash: policy, Netuid: uint64(fixture.startup.cfg.Netuid), NoId: op.NoID}
	preparationRaw, err := json.Marshal(preparation)
	if err != nil {
		t.Fatal(err)
	}
	preparationPath := filepath.Join(t.TempDir(), "original-request-preparation.json")
	if err := os.WriteFile(preparationPath, preparationRaw, 0600); err != nil {
		t.Fatal(err)
	}
	op.RequestPreparation = &ReleaseEvidenceV2File{Path: preparationPath, Bytes: uint64(len(preparationRaw)), SHA256: attemptHex32(sha256.Sum256(preparationRaw))}
	fixture.runtime.cfg.Operators[index] = op
	publicationPreparation, err := providerAttemptPublicationPreparation(t.Context(), &fixture.runtime.cfg)
	if err != nil {
		t.Fatal(err)
	}
	prepareProviderPublicationTestNamespace(t, publicationPreparation)
	fixture.startup.trail(t, index)
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	cursor := &providerAttemptPublicationCursor{}
	for _, epoch := range []uint64{7, 8} {
		if err := fixture.runtime.publishProviderRequestLane(t.Context(), op, fixture.runtimes[index], cursor); err != nil {
			t.Fatalf("actual closed request publication %d: %v", epoch, err)
		}
		path, err := ProviderAttemptPublicationPath(fixture.startup.cfg.StateDir, epoch, op.NoID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cut ProviderAttemptRequestWindow
		if err := json.Unmarshal(raw, &cut); err != nil {
			t.Fatal(err)
		}
		if cut.Header.Window.Epoch != epoch || epoch == 7 && len(cut.Records) != 8 || epoch == 8 && len(cut.Records) != 0 {
			t.Fatal("production request census changed")
		}
		for _, store := range fixture.stores {
			store.stateLock.Lock()
			got := bytes.Clone(store.objects["metadata/"+attemptHex32(sha256.Sum256(raw))])
			store.stateLock.Unlock()
			if !bytes.Equal(got, raw) {
				t.Fatal("actual reserved public metadata missing")
			}
		}
	}
	restart := &providerAttemptPublicationCursor{}
	if err := fixture.runtime.publishProviderRequestLane(t.Context(), op, fixture.runtimes[index], restart); err != nil {
		t.Fatal("retained old publication restart", err)
	}
}

// A blocked optional publication worker cannot stop healthy work, and its
// cancellation/join completes before the same operator's resources close.
func TestProviderRequestPublicationWorkerIsIndependentAndJoined(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	entered := make(chan struct{})
	healthy := make(chan struct{})
	var joined atomic.Bool
	operations := releaseShutdownTestOperations(func() {})
	operations.providerRequests = func(owner context.Context) error {
		close(entered)
		<-owner.Done()
		joined.Store(true)
		return owner.Err()
	}
	operations.refresh = func(owner context.Context) error {
		select {
		case <-entered:
		case <-owner.Done():
			return owner.Err()
		}
		close(healthy)
		cancel()
		return nil
	}
	closeOwner := runtime.close
	runtime.close = func() error {
		if !joined.Load() {
			t.Error("request publisher escaped shutdown ownership")
		}
		return closeOwner()
	}
	if err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations); err != nil {
		t.Fatal(err)
	}
	select {
	case <-healthy:
	default:
		t.Fatal("blocked optional publication stalled healthy sibling")
	}
}

// Explicit offline fixture birth uses the same pure anchor generator as the
// complete-owner preparation adapter. Runtime never creates this directory.
func prepareProviderPublicationTestNamespace(t *testing.T, preparation ProviderAttemptPublicationPreparation) *ProviderAttemptPublicationNamespace {
	t.Helper()
	path := filepath.Join(preparation.StateDir, "provider-attempt-publications")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := directory.Stat()
	if err != nil {
		directory.Close()
		t.Fatal(err)
	}
	raw, err := FreshProviderAttemptPublicationNamespaceAttribute(preparation, info)
	if err != nil {
		directory.Close()
		t.Fatal(err)
	}
	if err := durablesys.SetAttribute(int(directory.Fd()), ProviderAttemptPublicationNamespaceAttribute, raw, unix.XATTR_CREATE); err != nil {
		directory.Close()
		t.Fatal(err)
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		t.Fatal(err)
	}
	namespace, err := OpenProviderAttemptPublicationNamespace(t.Context(), preparation)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := namespace.Close(); err != nil {
			t.Error(err)
		}
	})
	return namespace
}

// Optional publication cannot recreate a lost original birth even when the
// directory is otherwise private and a valid signed window is available.
func TestProviderRequestPublicationRefusesUnpreparedNamespaceWithoutWrites(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	cut := fixture.response.Requests[0].Windows[0]
	state := newAttemptSettlementRuntimeV2TestStateDir(t)
	preparation := ProviderAttemptPublicationPreparation{StateDir: state, MaxWindowBytes: 1024 * 1024, MaxHistoryBytes: 4 * 1024 * 1024, MaxFiles: 32, Operators: []ProviderAttemptPublicationOperator{{Preparation: cut.Header.Preparation, ReceiptScope: fixture.response.Authority.Validators[0].Operators[0].ReceiptScope}}}
	directory := filepath.Join(state, "provider-attempt-publications")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if namespace, err := OpenProviderAttemptPublicationNamespace(t.Context(), preparation); err == nil || namespace != nil {
		if namespace != nil {
			_ = namespace.Close()
		}
		t.Fatal("unprepared publication owner was recreated", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("unprepared owner wrote publication files", err)
	}
	namespace := func() *ProviderAttemptPublicationNamespace {
		if err := os.Remove(directory); err != nil {
			t.Fatal(err)
		}
		return prepareProviderPublicationTestNamespace(t, preparation)
	}()
	path, err := ProviderAttemptPublicationPath(state, cut.Header.Window.Epoch, cut.Header.Preparation.Identity.Ledger.NoID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cut)
	if err != nil {
		t.Fatal(err)
	}
	if err := retainProviderAttemptPublication(t.Context(), path, raw, 1024*1024, 4*1024*1024, 32, namespace); err != nil {
		t.Fatal("prepared counterpart", err)
	}
}

// A crash between the publisher's Linkat and temporary unlink is recovered
// under the same lease, retaining the authentic final inode and exact bytes.
func TestProviderRequestPublicationRecoversOnlyExactLinkedTemporary(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	cut := fixture.response.Requests[0].Windows[0]
	state := newAttemptSettlementRuntimeV2TestStateDir(t)
	preparation := ProviderAttemptPublicationPreparation{StateDir: state, MaxWindowBytes: 1024 * 1024, MaxHistoryBytes: 4 * 1024 * 1024, MaxFiles: 32, Operators: []ProviderAttemptPublicationOperator{{Preparation: cut.Header.Preparation, ReceiptScope: fixture.response.Authority.Validators[0].Operators[0].ReceiptScope}}}
	namespace := prepareProviderPublicationTestNamespace(t, preparation)
	path, err := ProviderAttemptPublicationPath(state, cut.Header.Window.Epoch, cut.Header.Preparation.Identity.Ledger.NoID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cut)
	if err != nil {
		t.Fatal(err)
	}
	if err := retainProviderAttemptPublication(t.Context(), path, raw, 1024*1024, 4*1024*1024, 32, namespace); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(filepath.Dir(path), ".compact-input-00000000000000000000000000000001")
	if err := os.Link(path, temporary); err != nil {
		t.Fatal(err)
	}
	if err := retainProviderAttemptPublication(t.Context(), path, raw, 1024*1024, 4*1024*1024, 32, namespace); err != nil {
		t.Fatal("actual linked publication recovery", err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(original, after) {
		t.Fatal("recovery replaced original final inode", err)
	}
	if _, err := os.Lstat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("original temporary link was not reconciled", err)
	}
	unrelated := filepath.Join(filepath.Dir(path), "unrelated-original.json")
	if err := os.Link(path, unrelated); err != nil {
		t.Fatal(err)
	}
	if err := retainProviderAttemptPublication(t.Context(), path, raw, 1024*1024, 4*1024*1024, 32, namespace); err == nil {
		t.Fatal("unrelated physical link admitted as publisher cleanup")
	}
	retained, err := os.ReadFile(unrelated)
	if err != nil || !bytes.Equal(retained, raw) {
		t.Fatal("unrelated original was removed", err)
	}
}
