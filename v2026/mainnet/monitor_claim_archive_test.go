//go:build linux

// Real public monitor, plan/apply and snapshot owners surround synthetic claim
// publications. Sample barriers order every source change and cancellation.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

type monitorClaimArchiveFixture struct {
	services   *monitorServicesFixture
	ctx        context.Context
	url        string
	policy     monitorClaimPolicy
	value      atomic.Pointer[protocol.ClaimProgress]
	requests   atomic.Uint64
	checkpoint string
	archive    string
	metadata   string
	original   []byte
	request    monitorClaimArchiveRequest
}

type monitorClaimArchiveRun struct {
	cancel     context.CancelFunc
	done       chan struct{}
	resume     chan struct{}
	peerResume chan struct{}
	roleDone   chan int
	closes     atomic.Uint64
	sink       *monitorClaimTestSink
	diagnostic bytes.Buffer
	exit       int
}

func newMonitorClaimArchiveFixture(t *testing.T, catalog *monitorHistoryCatalogPolicy) *monitorClaimArchiveFixture {
	return newMonitorClaimArchiveCensusFixture(t, catalog, 2)
}

func newMonitorClaimArchiveCensusFixture(t *testing.T, catalog *monitorHistoryCatalogPolicy, census int) *monitorClaimArchiveFixture {
	return newMonitorClaimArchivePreparedFixture(t, catalog, census, nil)
}

// Restore fixtures prepare their real original logical roots before the first
// public sample. Ordinary archive tests retain their existing synthetic roots.
func newMonitorClaimArchivePreparedFixture(t *testing.T, catalog *monitorHistoryCatalogPolicy, census int, prepare func(*monitorClaimArchiveFixture) context.Context) *monitorClaimArchiveFixture {
	t.Helper()
	if census < 2 || census > 64 {
		t.Fatal("fixture census exceeds the actual bounded publication")
	}
	f := &monitorClaimArchiveFixture{services: newMonitorServicesFixture(t), metadata: t.TempDir()}
	if err := os.Chmod(f.metadata, 0700); err != nil {
		t.Fatal(err)
	}
	now := f.services.clock.now()
	value := monitorClaimTestPending(monitorClaimTestValue(now))
	for index := 1; index < census-1; index++ {
		entry := value.Entries[0]
		entry.Observation = cloneMonitorClaimObservation(entry.Observation)
		entry.Epoch, entry.Observation.Epoch = 7+int64(index), 7+int64(index)
		value.Entries = append(value.Entries, entry)
	}
	accepted := monitorClaimTestValue(now).Entries[0]
	accepted.Epoch, accepted.Observation.Epoch = 6+int64(census), 6+int64(census)
	value.Entries = append(value.Entries, accepted)
	value.TotalEntries, value.FinalizedEntries, value.UnresolvedEntries = uint64(census), 1, uint64(census-1)
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	f.value.Store(&value)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		f.requests.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/claim-progress" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(f.value.Load()); err != nil {
			return
		}
	}))
	t.Cleanup(source.Close)
	f.policy = monitorClaimTestPolicy(t, value, source.URL+"/claim-progress", now)
	for index := 1; index < census; index++ {
		f.policy.Epochs = append(f.policy.Epochs, monitorClaimEpochPolicy{Epoch: 7 + int64(index), ShareBps: 5000, AcceptBy: now.Add(-time.Minute).Format(time.RFC3339Nano)})
	}
	if census > maxMonitorClaimEpochs {
		f.policy.EpochCapacity = uint64(census)
	}
	f.policy.HistoryCatalog = catalog
	f.services.policy.Claims = []monitorClaimPolicy{f.policy}
	f.services.writePolicy(t)
	f.url, _, _ = monitorServicesBlockedChain(t)
	if prepare == nil {
		f.ctx = monitorTestStorageContext(t, t.Context(), f.services.args(f.url))
	} else {
		f.ctx = prepare(f)
	}
	reference, ok := durablevolume.ReferenceFromContext(f.ctx)
	if !ok {
		t.Fatal("fixture declaration absent")
	}
	configuration, err := durablevolume.Load(reference)
	if err != nil {
		t.Fatal(err)
	}
	for index := range configuration.Volumes {
		configuration.Volumes[index].MinAvailableBytes = max(configuration.Volumes[index].MinAvailableBytes, 32*1024*1024)
		configuration.Volumes[index].MinAvailableInodes = max(configuration.Volumes[index].MinAvailableInodes, 1024)
	}
	raw, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	reference = durablevolume.Reference{Path: filepath.Join(f.metadata, "volumes.json"), Sha256: monitorReadDigest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.ctx = durablevolume.WithReference(f.ctx, reference)
	f.checkpoint, _ = monitorClaimPaths(f.services.checkpointPath, f.services.metricsPath, f.policy.Role)
	if f.archive == "" {
		f.archive = filepath.Join(filepath.Dir(f.checkpoint), "claim-archive-001.json")
	}
	run := f.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || !event.CheckpointCurrent || event.State.MerkleProofs != census-1 || event.State.Deferred != 1 || event.State.Overdue != census-1 {
		t.Fatal("actual original monitor did not retain unresolved proof and deferred credit", event)
	}
	run.stop(t, 0)
	f.resetRequest(t)
	return f
}

func (self *monitorClaimArchiveFixture) start(t *testing.T, hooks monitorServiceHooks) *monitorClaimArchiveRun {
	t.Helper()
	self.services.policy.Claims = []monitorClaimPolicy{self.policy}
	self.services.writePolicy(t)
	ctx, cancel := context.WithCancel(self.ctx)
	run := &monitorClaimArchiveRun{cancel: cancel, done: make(chan struct{}), resume: make(chan struct{}, 1), peerResume: make(chan struct{}, 1), roleDone: make(chan int, 1), sink: &monitorClaimTestSink{events: make(chan monitorClaimTestEvent, 4), peers: make(chan monitorServiceEvent, 4)}}
	afterWorker, afterClose := hooks.afterWorker, hooks.afterClose
	hooks.afterWorker = func(role string, exit int) {
		if role == self.policy.Role {
			run.roleDone <- exit
		}
		if afterWorker != nil {
			afterWorker(role, exit)
		}
	}
	hooks.afterClose = func(role, kind string, file *os.File) error {
		var observed error
		if role == self.policy.Role {
			run.closes.Add(1)
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				observed = errors.New("Claim close observer preceded the actual descriptor close")
			}
		}
		if afterClose != nil {
			observed = errors.Join(observed, afterClose(role, kind, file))
		}
		return observed
	}
	if hooks.wait == nil {
		hooks.wait = func(ctx context.Context, role string, _ time.Duration) bool {
			var resume <-chan struct{}
			switch role {
			case self.policy.Role:
				resume = run.resume
			case "healthy":
				resume = run.peerResume
			}
			select {
			case <-ctx.Done():
				return false
			case <-resume:
				return true
			}
		}
	}
	go func() {
		defer close(run.done)
		run.exit = runMainWithMonitorHooks(ctx, self.services.args(self.url), run.sink, &run.diagnostic, self.services.clock.now, hooks)
	}()
	t.Cleanup(func() { cancel(); <-run.done })
	return run
}

func (self *monitorClaimArchiveRun) next(t *testing.T) monitorClaimTestEvent {
	t.Helper()
	select {
	case event := <-self.sink.events:
		return event
	case <-self.done:
		t.Fatal("public claim archive consumer exited before sample", self.exit, self.diagnostic.String())
	case <-time.After(20 * time.Second):
		t.Fatal("public claim archive consumer did not finish sample")
	}
	return monitorClaimTestEvent{}
}

func (self *monitorClaimArchiveRun) stop(t *testing.T, expected int) {
	t.Helper()
	self.cancel()
	select {
	case <-self.done:
	case <-time.After(20 * time.Second):
		t.Fatal("claim archive consumer did not join")
	}
	if self.exit != expected {
		t.Fatal("claim archive consumer exit differs", self.exit, expected, self.diagnostic.String())
	}
}

// A separately admitted validator proves actual continuation after Claim
// refusal; a still-running blocked chain alone is not useful progress evidence.
func (self *monitorClaimArchiveFixture) healthyPeer(t *testing.T) {
	t.Helper()
	value := monitorServicesTestRecord(self.services.clock.now(), 1)
	policy := monitorValidatorPolicy{Role: "healthy", ProgressFile: filepath.Join(self.services.directory, "healthy.json"), ExpectedSource: value.Source}
	self.services.policy.Validators = []monitorValidatorPolicy{policy}
	monitorServicesTestWrite(t, policy.ProgressFile, value)
	checkpoint, _ := monitorValidatorPaths(self.services.checkpointPath, self.services.metricsPath, policy.Role)
	provisionMonitorTestCustody(t, checkpoint)
}

// The affected owner must join before parent cancellation. Its original bytes
// and source-read count stay fixed while an independent role publishes twice.
func (self *monitorClaimArchiveFixture) refusedWhilePeerContinues(t *testing.T, run *monitorClaimArchiveRun, reads uint64, checkpoint []byte) {
	t.Helper()
	select {
	case exit := <-run.roleDone:
		if exit != 3 || run.closes.Load() != 2 {
			t.Fatal("Claim refusal lost its terminal cause or did not release both owners", exit, run.closes.Load())
		}
	case event := <-run.sink.events:
		t.Fatal("refused Claim custody emitted another source sample", event)
	case <-time.After(20 * time.Second):
		t.Fatal("refused Claim role did not join independently")
	}
	for index := range 2 {
		if index != 0 {
			run.peerResume <- struct{}{}
		}
		select {
		case event := <-run.sink.peers:
			if event.Role != "healthy" || event.Publication != "published" || event.State == nil || event.State.Record == nil || event.State.ReadStatus != "ok" || event.State.LastReadSuccessAt.IsZero() {
				t.Fatal("Claim refusal prevented an actual healthy peer sample", event)
			}
		case <-run.done:
			t.Fatal("Claim refusal stopped the independent monitor service", run.exit)
		case <-time.After(20 * time.Second):
			t.Fatal("healthy peer did not continue after Claim refusal")
		}
	}
	if self.requests.Load() != reads || len(run.sink.events) != 0 {
		t.Fatal("refused Claim custody read or published another source", reads, self.requests.Load(), len(run.sink.events))
	}
	retained, err := os.ReadFile(self.checkpoint)
	if err != nil || !bytes.Equal(retained, checkpoint) {
		t.Fatal("Claim refusal changed the original checkpoint evidence", err)
	}
	run.stop(t, 3)
	if self.requests.Load() != reads || len(run.sink.events) != 0 {
		t.Fatal("joined Claim refusal left a delayed read or fabricated sample")
	}
}

func (self *monitorClaimArchiveFixture) advance(change func(*protocol.ClaimProgress)) {
	value := cloneMonitorClaimProgress(self.value.Load())
	self.services.clock.seconds.Add(1)
	value.Sequence++
	value.PublishedAt = self.services.clock.now().Format(time.RFC3339Nano)
	if change != nil {
		change(value)
	}
	self.value.Store(value)
}

func (self *monitorClaimArchiveFixture) record(t *testing.T) monitorClaimCheckpointRecord {
	t.Helper()
	raw, err := os.ReadFile(self.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var record monitorClaimCheckpointRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func (self *monitorClaimArchiveFixture) resetRequest(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(self.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	self.original = raw
	provisionMonitorTestCustody(t, self.archive)
	self.request = monitorClaimArchiveRequest{Schema: monitorClaimArchiveRequestSchema, Policy: self.policy, Expected: identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, Original: monitorHistoryReference{Path: self.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, ArchivePath: self.archive, FutureSegments: 2}
	fence := monitorHistoryWriterFence{Schema: monitorHistoryWriterFenceSchema, Original: self.request.Original, PolicyHash: self.policy.archiveIdentityHash(), StoppedAndJoined: true}
	raw, err = json.Marshal(fence)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.metadata, "writer-fence.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	self.request.FormerWriterFence = planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
}

func (self *monitorClaimArchiveFixture) document(t *testing.T, name string, value any) (string, string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.metadata, name+".json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, monitorReadDigest(raw)
}

func (self *monitorClaimArchiveFixture) plan(t *testing.T) (monitorClaimArchivePlan, []string) {
	t.Helper()
	path, hash := self.document(t, "archive-request", self.request)
	before := mainnetNamespaceTest(t, filepath.Dir(self.checkpoint))
	var output, diagnostic bytes.Buffer
	if code := runMain(self.ctx, []string{"monitor-claim-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public claim archive plan refused", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(self.checkpoint))) {
		t.Fatal("claim archive planning wrote original state")
	}
	var plan monitorClaimArchivePlan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.RestartAuthorized || plan.RequiredSegments != 2*(uint64(plan.Segments)+self.request.FutureSegments) {
		t.Fatal("claim plan lost forecast or granted restart", plan)
	}
	path, hash = self.document(t, "archive-plan", plan)
	return plan, []string{"monitor-claim-archive", "apply", "--plan", path, "--plan-sha256", hash}
}

func (self *monitorClaimArchiveFixture) apply(t *testing.T, args []string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(self.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("public claim archive apply refused", code, diagnostic.String())
	}
	raw, err := os.ReadFile(self.archive)
	if err != nil || !bytes.Equal(raw, self.original) {
		t.Fatal("claim archive changed exact original bytes", err)
	}
}

func TestMonitorClaimArchivePublicRestartRetainsUnresolvedAndOriginalPayment(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	original := f.record(t)
	_, args := f.plan(t)
	f.apply(t, args)
	compacted := f.record(t)
	if compacted.Archive == nil || len(compacted.Archive.Epochs) != 2 || !compacted.State.Epochs[0].Archived || !compacted.State.Epochs[1].Archived || !reflect.DeepEqual(compacted.PolicyHistory, original.PolicyHistory) || !reflect.DeepEqual(compacted.State.Record, original.State.Record) {
		t.Fatal("claim compaction lost original progress, policy or unresolved evidence commitments", compacted)
	}
	f.advance(nil)
	run := f.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.MerkleProofs != 1 || event.State.Deferred != 1 || event.State.Overdue != 1 {
		t.Fatal("archive hydration hid unresolved proof or credit", event)
	}
	f.advance(func(value *protocol.ClaimProgress) {
		*value.Entries[0].Observation.LeafClaimed = true
		value.Entries[0].QueueStatus = "finalized"
		value.FinalizedEntries, value.UnresolvedEntries, value.OldestUnresolvedEpoch = 2, 0, nil
	})
	run.resume <- struct{}{}
	event = run.next(t)
	if !event.Current || event.State.ClaimedLeaves != 1 || event.State.MerkleProofs != 1 || event.State.Deferred != 1 || event.State.Overdue != 0 {
		t.Fatal("actual post-archive claim did not progress independently", event)
	}
	run.stop(t, 0)
	progressed := f.record(t)
	if progressed.State.Epochs[0].Archived || progressed.State.Epochs[0].Proof == nil || *progressed.State.Epochs[0].Proof.LeafClaimed || !progressed.State.Epochs[1].Archived {
		t.Fatal("changed claim lost first proof or unchanged receipt was expanded", progressed.State.Epochs)
	}
	_, metrics := monitorClaimPaths(f.services.checkpointPath, f.services.metricsPath, f.policy.Role)
	raw, err := os.ReadFile(metrics)
	if err != nil || !bytes.Contains(raw, []byte("per_epoch_payment_known{role=\"claim-a\"} 0")) {
		t.Fatal("archive became paid amount authority", err)
	}
	f.archive = filepath.Join(filepath.Dir(f.checkpoint), "claim-archive-002.json")
	f.resetRequest(t)
	_, args = f.plan(t)
	f.apply(t, args)
	f.advance(nil)
	run = f.start(t, monitorServiceHooks{})
	event = run.next(t)
	if event.State.ClaimedLeaves != 1 || event.State.Deferred != 1 || len(f.record(t).Archive.Segments) != 2 {
		t.Fatal("second archive lost predecessor obligations", event)
	}
	run.stop(t, 0)
}

func TestMonitorClaimArchiveLostAcknowledgmentResumesExactHeads(t *testing.T) {
	for _, phase := range []string{"archive", "checkpoint"} {
		func() {
			f := newMonitorClaimArchiveFixture(t, nil)
			plan, args := f.plan(t)
			calls := 0
			err := applyMonitorClaimArchive(f.ctx, plan, monitorServiceHooks{syncDirectory: func(_ string, kind string, _ *os.File) error {
				if kind == phase {
					calls++
					return syscall.EIO
				}
				return nil
			}})
			if !errors.Is(err, syscall.EIO) || calls != 1 {
				t.Fatal("real sync acknowledgment fault was not reached", err, calls)
			}
			archived, readErr := os.ReadFile(f.archive)
			if readErr != nil || !bytes.Equal(archived, f.original) {
				t.Fatal("original bytes were not durable before compaction", readErr)
			}
			f.apply(t, args)
			f.apply(t, args)
			f.advance(nil)
			run := f.start(t, monitorServiceHooks{})
			if event := run.next(t); !event.Current || event.State.Overdue != 1 || event.State.Deferred != 1 {
				t.Fatal("lost acknowledgment reset liabilities", event)
			}
			run.stop(t, 0)
		}()
	}
}

func TestMonitorClaimArchiveRefusesOmittedForgedAndRegressedEvidence(t *testing.T) {
	for _, fault := range []string{"missing", "digest", "repeat", "commitment", "erase-proof", "deferred-change", "incident-reset"} {
		func() {
			f := newMonitorClaimArchiveFixture(t, nil)
			original := f.record(t)
			_, args := f.plan(t)
			f.apply(t, args)
			candidate := f.record(t)
			switch fault {
			case "missing":
				candidate.Archive = nil
			case "digest":
				candidate.Archive.Segments[0].Sha256 = "sha256:" + strings.Repeat("f", 64)
			case "repeat":
				candidate.Archive.Segments = append(candidate.Archive.Segments, candidate.Archive.Segments[0])
			case "commitment":
				candidate.Archive.Epochs[0].StateSha256 = "sha256:" + strings.Repeat("e", 64)
			case "erase-proof":
				candidate.State.Epochs[0] = original.State.Epochs[0]
				candidate.State.Epochs[0].Proof = nil
			case "incident-reset":
				candidate.State.Incidents = 0
			case "deferred-change":
				candidate.State.Epochs[1] = cloneMonitorClaimEpoch(original.State.Epochs[1])
				candidate.State.Epochs[1].Observation.UnpaidCreditRao = "126"
			}
			raw, err := encodeMonitorClaimCheckpoint(candidate)
			if err != nil {
				t.Fatal(err)
			}
			writer, err := openMonitorHistorySnapshot(f.ctx, f.checkpoint, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(writer.publish(raw, nil), writer.close()); err != nil {
				t.Fatal(err)
			}
			f.healthyPeer(t)
			f.advance(nil)
			reads := f.requests.Load()
			run := f.start(t, monitorServiceHooks{})
			f.refusedWhilePeerContinues(t, run, reads, raw)
		}()
	}
}

func TestMonitorClaimArchiveActualHistoryReadOnceAndCustodyAfterAdmission(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	_, args := f.plan(t)
	f.apply(t, args)
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, f.archive, unix.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	f.healthyPeer(t)
	f.advance(nil)
	run := f.start(t, monitorServiceHooks{})
	if !run.next(t).Current {
		t.Fatal("archive was not admitted")
	}
	buffer := make([]byte, 4096)
	if count, err := unix.Read(fd, buffer); err != nil || count <= 0 {
		t.Fatal("actual archive admission never read original bytes", count, err)
	}
	for range 3 {
		f.advance(nil)
		run.resume <- struct{}{}
		if !run.next(t).Current {
			t.Fatal("unchanged archive lost current role")
		}
	}
	if count, err := unix.Read(fd, buffer); count > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("hot claim samples reread complete archive payload", count, err)
	}
	raw, err := os.ReadFile(f.archive)
	if err != nil {
		t.Fatal(err)
	}
	replacement := f.archive + ".replacement"
	if err := os.WriteFile(replacement, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, f.archive); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := os.ReadFile(f.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reads := f.requests.Load()
	run.resume <- struct{}{}
	select {
	case event := <-run.sink.events:
		if event.Status != "identity" || event.Current {
			t.Fatal("changed archive custody admitted another source sample", event)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("changed archive custody did not stop affected role")
	}
	f.refusedWhilePeerContinues(t, run, reads, checkpoint)
}

func TestMonitorClaimArchivePolicyRenewalPreservesUnresolvedPrefix(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	_, args := f.plan(t)
	f.apply(t, args)
	original := f.record(t)
	last := original.PolicyHistory.Entries[len(original.PolicyHistory.Entries)-1]
	f.policy.Epochs = append(f.policy.Epochs, monitorClaimEpochPolicy{Epoch: 9, ShareBps: 5000, AcceptBy: f.services.clock.now().Add(time.Hour).Format(time.RFC3339Nano)})
	f.policy.Renewal = &monitorProgressPolicyRenewal{Original: original.PolicyHistory.Entries[0].Resources, PreviousSha256: last.ContentHash, ReviewSha256: "sha256:" + strings.Repeat("d", 64)}
	f.advance(nil)
	run := f.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Overdue != 1 || event.State.Deferred != 1 {
		t.Fatal("reviewed new epoch erased old unknown or deferred liability", event)
	}
	run.stop(t, 0)
	renewed := f.record(t)
	if len(renewed.State.Epochs) != 3 || renewed.State.Epochs[2].Observation != nil || len(renewed.PolicyHistory.Entries) != 2 || !reflect.DeepEqual(original.Archive, renewed.Archive) || !reflect.DeepEqual(original.PolicyHistory.Entries, renewed.PolicyHistory.Entries[:1]) {
		t.Fatal("policy growth replaced archived expectations or invented new evidence", renewed)
	}
}

func TestMonitorClaimArchiveCancellationAndObservationErrorsDoNotInventConflict(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	plan, _ := f.plan(t)
	before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	called := false
	err := applyMonitorClaimArchive(ctx, plan, monitorServiceHooks{historyRead: func(_ string, step string) {
		if step == "archive-original" {
			called = true
			cancel()
		}
	}})
	if !called || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "disappeared") || strings.Contains(err.Error(), "differs") {
		t.Fatal("canceled real original read became observed conflict", called, err)
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
		t.Fatal("canceled archive preparation changed retained bytes")
	}
}

func TestMonitorClaimArchiveForecastRefusesBeforeEffects(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	request := f.request
	request.FutureSegments = maximumMonitorHistorySegments / 2
	if _, err := planMonitorClaimArchive(f.ctx, request); err == nil || !strings.Contains(err.Error(), "forecast") {
		t.Fatal("two-times segment forecast was not admitted before effects", err)
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
		t.Fatal("refused archive forecast mutated original owners")
	}
}

// Exact JSON fields/order from ad427 monitor_claim_state.go. These independent
// old types prevent the new owner from defining both sides of compatibility.
type monitorClaimArchivePriorEpochFixture struct {
	Epoch       int64                      `json:"epoch"`
	Observation *protocol.ClaimObservation `json:"observation,omitempty"`
	Proof       *protocol.ClaimObservation `json:"proof,omitempty"`
	FirstSeenAt time.Time                  `json:"first_seen_at"`
	ProgressAt  time.Time                  `json:"progress_at"`
}

type monitorClaimArchivePriorStateFixture struct {
	SampleAt           time.Time                              `json:"sample_at"`
	HighWaterAt        time.Time                              `json:"high_water_at"`
	LastAcceptedAt     time.Time                              `json:"last_accepted_at"`
	SemanticProgressAt time.Time                              `json:"semantic_progress_at"`
	OutageSince        time.Time                              `json:"outage_since"`
	LastIssueAt        time.Time                              `json:"last_issue_at"`
	LastIssue          string                                 `json:"last_issue"`
	Incidents          uint64                                 `json:"incidents"`
	Restarts           uint64                                 `json:"restarts"`
	Status             string                                 `json:"status"`
	Record             *protocol.ClaimProgress                `json:"record,omitempty"`
	Epochs             []monitorClaimArchivePriorEpochFixture `json:"epochs"`
}

func TestMonitorClaimArchiveOriginalCheckpointGrammarRemainsExact(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	record := f.record(t)
	// The literal old field order/omitempty grammar is independent of the new
	// serializer. Neither archive nor per-epoch false markers may change it.
	stateRaw, err := json.Marshal(record.State)
	if err != nil {
		t.Fatal(err)
	}
	var priorState monitorClaimArchivePriorStateFixture
	if err := json.Unmarshal(stateRaw, &priorState); err != nil {
		t.Fatal(err)
	}
	old := struct {
		Schema        string                               `json:"schema"`
		PolicyHash    string                               `json:"policy_hash"`
		State         monitorClaimArchivePriorStateFixture `json:"state"`
		PolicyHistory *monitorProgressPolicyHistory        `json:"policy_history,omitempty"`
		ContentHash   string                               `json:"content_hash"`
	}{Schema: record.Schema, PolicyHash: record.PolicyHash, State: priorState, PolicyHistory: record.PolicyHistory}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	old.ContentHash = hex.EncodeToString(digest[:])
	raw, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(raw, '\n'), f.original) {
		t.Fatal("absent archive fields rewrote literal prior checkpoint grammar")
	}
	decoded, err := decodeMonitorClaimCheckpoint(raw, f.policy)
	if err != nil || decoded.ContentHash != record.ContentHash {
		t.Fatal("new reader refused exact original checksum", err)
	}
}

func TestMonitorClaimCatalogPublicSignedGrowthKeepsOriginalEvidence(t *testing.T) {
	seed := bytes.Repeat([]byte{0x57}, ed25519.SeedSize)
	key := ed25519.NewKeyFromSeed(seed)
	policy := &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + strings.Repeat("b", 64), InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: 64 * 1024, HeldReaders: 128}}
	f := newMonitorClaimArchiveFixture(t, policy)
	_, args := f.plan(t)
	f.apply(t, args)
	f.resetRequest(t)
	request := monitorClaimCatalogRequest{Schema: monitorClaimCatalogRequestSchema, Expected: f.request.Expected, Policy: f.policy, Original: f.request.Original, FormerWriterFence: f.request.FormerWriterFence, FutureSegments: 2, Capacity: monitorHistoryCapacity{Segments: 256, CatalogBytes: 128 * 1024, HeldReaders: 256}}
	path, hash := f.document(t, "catalog-request", request)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, []string{"monitor-claim-catalog", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public claim catalog preview failed", code, diagnostic.String())
	}
	var plan monitorClaimCatalogPlan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	message, err := hex.DecodeString(plan.SigningBytes)
	if err != nil || len(message) == 0 {
		t.Fatal("public signing frame absent", err)
	}
	approval := monitorHistoryCatalogApproval{Schema: monitorHistoryCatalogApprovalSchema, Revision: plan.Revision, Signature: hex.EncodeToString(ed25519.Sign(key, message))}
	planPath, planHash := f.document(t, "catalog-plan", plan)
	approvalPath, approvalHash := f.document(t, "catalog-approval", approval)
	catalogArgs := []string{"monitor-claim-catalog", "apply", "--plan", planPath, "--plan-sha256", planHash, "--approval", approvalPath, "--approval-sha256", approvalHash}
	before := f.record(t)
	wrong := approval
	wrongKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x23}, ed25519.SeedSize))
	wrong.Signature = hex.EncodeToString(ed25519.Sign(wrongKey, message))
	beforeNamespace := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	if err := applyMonitorClaimCatalog(f.ctx, plan, wrong, monitorServiceHooks{}); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatal("foreign catalog signer acquired original custody", err)
	}
	if !reflect.DeepEqual(beforeNamespace, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
		t.Fatal("invalid capacity signature changed original claim bytes")
	}
	for range 2 {
		output.Reset()
		diagnostic.Reset()
		if code := runMain(f.ctx, catalogArgs, &output, &diagnostic); code != 0 {
			t.Fatal("public exact signed catalog adoption failed", code, diagnostic.String())
		}
	}
	after := f.record(t)
	if after.Catalog == nil || len(after.Catalog.Revisions) != 1 || !reflect.DeepEqual(before.State, after.State) || !reflect.DeepEqual(before.Archive, after.Archive) || !reflect.DeepEqual(before.PolicyHistory, after.PolicyHistory) {
		t.Fatal("capacity adoption changed original liability or policy provenance", after)
	}
	f.advance(nil)
	run := f.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Deferred != 1 || event.State.Overdue != 1 {
		t.Fatal("signed capacity discarded retained unresolved evidence", event)
	}
	run.stop(t, 0)
	// A later sample cannot be rewritten by replaying the old signed approval.
	if err := applyMonitorClaimCatalog(f.ctx, plan, approval, monitorServiceHooks{}); err == nil {
		t.Fatal("old signed capacity rewrote later claim progress")
	}
}

func TestMonitorClaimCatalogCannotEnrollLegacyOrReplaceOriginalKey(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	request := monitorClaimCatalogRequest{Schema: monitorClaimCatalogRequestSchema, Expected: f.request.Expected, Policy: f.policy, Original: f.request.Original, FormerWriterFence: f.request.FormerWriterFence, FutureSegments: 1, Capacity: monitorHistoryCapacity{Segments: 256, CatalogBytes: 128 * 1024, HeldReaders: 256}}
	if _, err := planMonitorClaimCatalog(f.ctx, request); err == nil {
		t.Fatal("legacy claim owner implicitly enrolled a catalog key")
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x37}, ed25519.SeedSize))
	f.policy.HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: fmt.Sprintf("0x%x", key.Public()), ReviewSha256: "sha256:" + strings.Repeat("c", 64), InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: 64 * 1024, HeldReaders: 128}}
	f.healthyPeer(t)
	f.advance(nil)
	checkpoint, err := os.ReadFile(f.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reads := f.requests.Load()
	run := f.start(t, monitorServiceHooks{})
	f.refusedWhilePeerContinues(t, run, reads, checkpoint)
}

func TestMonitorClaimArchivePublicInputReadErrorsPrecedeDigestRefusal(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	for _, command := range []string{"monitor-claim-archive", "monitor-claim-catalog"} {
		var output, diagnostic bytes.Buffer
		code := runMain(f.ctx, []string{command, "plan", "--request", filepath.Join(f.metadata, "absent.json"), "--request-sha256", "sha256:" + strings.Repeat("e", 64)}, &output, &diagnostic)
		if code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "read failed") || strings.Contains(diagnostic.String(), "differs") {
			t.Fatal("unobserved claim document became a digest contradiction", command, code, diagnostic.String())
		}
	}
}
