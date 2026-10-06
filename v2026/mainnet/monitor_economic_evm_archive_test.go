//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Test enrollment creates only explicitly fresh snapshot owners. The original
// evm checkpoint has already been published by the actual public monitor.
type monitorEvmArchiveFixture struct {
	evm        *monitorEvmFixture
	checkpoint string
	archive    string
	metadata   string
	request    monitorEvmArchiveRequest
	original   []byte
}

func newMonitorEvmArchiveFixture(t *testing.T, peer bool) *monitorEvmArchiveFixture {
	t.Helper()
	return newMonitorEvmArchiveFixtureWithPolicy(t, peer, nil)
}

func newMonitorEvmArchiveFixtureWithPolicy(t *testing.T, peer bool, configure func(*monitorEconomicEvmPolicy)) *monitorEvmArchiveFixture {
	t.Helper()
	evm := newMonitorEvmFixture(t, "settlement-vault", peer)
	evm.policy.HistoryEntries = 5
	if configure != nil {
		configure(&evm.policy)
	}
	run := evm.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || event.State.Cursor.Number != 11 || event.State.PendingThrough == nil || event.State.PendingThrough.Number != 13 {
		t.Fatal("original public checkpoint did not retain the incomplete range", event)
	}
	run.resume <- struct{}{}
	if event := run.next(t); event.Current || event.Status != "capacity-held" || event.State.BatchCount != 1 {
		t.Fatal("original role did not hold its full history", event)
	}
	run.stop(t)
	checkpoint, _ := monitorEconomicEvmPaths(evm.services.checkpointPath, evm.services.metricsPath, evm.policy.Role)
	metadata := t.TempDir()
	if err := os.Chmod(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	self := &monitorEvmArchiveFixture{evm: evm, checkpoint: checkpoint, archive: filepath.Join(filepath.Dir(checkpoint), "evm-archive-001.json"), metadata: metadata}
	// The reviewed operational declaration supplies the explicit two-times
	// physical forecast margin. No journal or physical generation is rewritten.
	ref, _ := durablevolume.ReferenceFromContext(evm.ctx)
	config, err := durablevolume.Load(ref)
	if err != nil {
		t.Fatal(err)
	}
	for index := range config.Volumes {
		config.Volumes[index].MinAvailableBytes = 16 * 1024 * 1024
		config.Volumes[index].MinAvailableInodes = 1024
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	ref = durablevolume.Reference{Path: filepath.Join(metadata, "archive-volumes.json"), Sha256: monitorReadDigest(raw)}
	if err := os.WriteFile(ref.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	evm.ctx = durablevolume.WithReference(evm.ctx, ref)
	self.resetRequest(t)
	return self
}

func (self *monitorEvmArchiveFixture) resetRequest(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(self.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	self.original = raw
	provisionMonitorTestCustody(t, self.archive)
	self.request = monitorEvmArchiveRequest{Schema: monitorEvmArchiveRequestSchema, Policy: self.evm.policy,
		Expected: identityExpectation{NativeChain: self.evm.policy.Network.NativeChain, GenesisHash: self.evm.policy.Network.GenesisHash, EvmChainId: mainnetEvmChainId},
		Original: monitorHistoryReference{Path: self.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, ArchivePath: self.archive, FutureSegments: 2}
	fence := monitorHistoryWriterFence{Schema: monitorHistoryWriterFenceSchema, Original: self.request.Original, PolicyHash: self.evm.policy.identityHash(), StoppedAndJoined: true}
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

func (self *monitorEvmArchiveFixture) plan(t *testing.T) (monitorEvmArchivePlan, []string) {
	t.Helper()
	raw, err := json.Marshal(self.request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.metadata, "request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, filepath.Dir(self.checkpoint))
	var output, diagnostic bytes.Buffer
	args := []string{"monitor-evm-archive", "plan", "--request", path, "--request-sha256", monitorReadDigest(raw)}
	if code := runMain(self.evm.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("public evm archive plan failed", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(self.checkpoint))) {
		t.Fatal("read-only archive planning changed original custody")
	}
	var plan monitorEvmArchivePlan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.RestartAuthorized || plan.RequiredSegments != 2*(uint64(plan.Segments)+self.request.FutureSegments) || plan.RequiredBytes <= 2*self.request.Original.Bytes {
		t.Fatal("archive plan lost explicit physical forecast or granted restart", plan)
	}
	path = filepath.Join(self.metadata, "plan.json")
	if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return plan, []string{"monitor-evm-archive", "apply", "--plan", path, "--plan-sha256", monitorReadDigest(output.Bytes())}
}

func (self *monitorEvmArchiveFixture) apply(t *testing.T, args []string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(self.evm.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("public evm archive apply failed", code, diagnostic.String())
	}
	raw, err := os.ReadFile(self.archive)
	if err != nil || !bytes.Equal(raw, self.original) {
		t.Fatal("archive did not retain exact original checkpoint bytes", err)
	}
}

func TestMonitorEvmArchivePublicRolloverContinuesCreditCarryAndCosts(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, false)
	original := f.evm.record(t)
	_, args := f.plan(t)
	f.apply(t, args)
	compacted := f.evm.record(t)
	if compacted.State.Archive == nil || compacted.State.Cursor != original.State.Cursor || !reflect.DeepEqual(compacted.State.PendingThrough, original.State.PendingThrough) || compacted.State.BatchCount != original.State.BatchCount || compacted.State.BatchChainHash != original.State.BatchChainHash || !reflect.DeepEqual(compacted.State.Snapshot, original.State.Snapshot) || !reflect.DeepEqual(compacted.ResourceHistory, original.ResourceHistory) || len(compacted.State.History)+len(compacted.State.Fees) != 0 || compacted.State.Status != "archive-ready" {
		t.Fatal("archive reset original credit, progress or review provenance", compacted)
	}
	if compacted.State.Archive.FeeWei != "42000" || compacted.State.Snapshot.Credits[f.evm.policy.Coldkeys[0]] != "12" || compacted.State.Snapshot.Pools["1"] != "50" {
		t.Fatal("compacted head discarded original deferred credit or carry", compacted)
	}
	run := f.evm.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 13 || event.State.BatchCount != 2 || event.State.PendingThrough != nil || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "84000" || event.State.ArchiveSegments != 1 || event.State.ArchivedEvents != 4 || event.State.ArchivedTransactionFees != 1 || event.State.ContractState == nil || event.State.ContractState.Counters["totalPaid"] != "15" || event.State.ContractState.Credits[f.evm.policy.Coldkeys[0]] != "0" || event.State.ContractState.Pools["1"] != "0" {
		t.Fatal("public archive adoption lost original carry, aggregate payment or cost", event)
	}
	if event.State.NativeFeeDebitRao != nil || event.State.NativeFeeRefundRao != nil || event.State.NativeFeeExecutionVerified || event.State.IndependentFinalityVerified {
		t.Fatal("archive promoted receipt assertions to independent fee authority", event)
	}
	run.stop(t)
	firstArchive := f.archive
	f.archive = filepath.Join(filepath.Dir(f.checkpoint), "evm-archive-002.json")
	f.resetRequest(t)
	_, args = f.plan(t)
	f.apply(t, args)
	run = f.evm.start(t, monitorServiceHooks{})
	event = run.next(t)
	if !event.Current || event.State.BatchCount != 2 || event.State.ArchiveSegments != 2 || event.State.ArchivedEvents != 7 || event.State.ArchivedTransactionFees != 2 || event.State.HistoryEntries != 0 || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "84000" {
		t.Fatal("second archive forgot or replayed original financial history", event)
	}
	run.stop(t)
	if _, err := os.Stat(firstArchive); err != nil {
		t.Fatal("rollover removed a prior segment", err)
	}
}

func TestMonitorEvmArchivePublicLostAcknowledgmentsReuseExactHeads(t *testing.T) {
	for _, stage := range []string{"archive", "checkpoint"} {
		f := newMonitorEvmArchiveFixture(t, false)
		_, args := f.plan(t)
		cause := errors.New("synthetic acknowledgment lost after real directory sync")
		calls := 0
		hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			if role == f.evm.policy.Role && kind == stage {
				calls++
				return cause
			}
			return nil
		}}
		var output, diagnostic bytes.Buffer
		if code := runMainWithMonitorHooks(f.evm.ctx, args, &output, &diagnostic, time.Now, hooks); code == 0 || output.Len() != 0 || calls != 1 {
			t.Fatal("lost archive acknowledgment was silently retried", stage, code, calls, diagnostic.String())
		}
		f.apply(t, args)
		before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
		f.apply(t, args)
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
			t.Fatal("repeat reviewed apply republished a completed head", stage)
		}
		run := f.evm.start(t, monitorServiceHooks{})
		if event := run.next(t); !event.Current || event.State.BatchCount != 2 || event.State.Cursor.Number != 13 {
			t.Fatal("lost acknowledgment replay lost original cursor", stage, event)
		}
		run.stop(t)
	}
}

func TestMonitorEvmArchivePublicRefusesActiveAndUnreviewedOwners(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, false)
	plan, args := f.plan(t)
	before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	owner, err := openMonitorHistorySnapshot(f.evm.ctx, f.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMonitorEvmArchive(f.evm.ctx, plan, monitorServiceHooks{}); !errors.Is(err, durablevolume.ErrBusy) {
		t.Fatal("archive did not refuse active original owner", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"missing-declaration", "canceled", "wrong-plan", "alias", "forecast", "unjoined"} {
		candidate := plan
		ctx := f.evm.ctx
		switch fault {
		case "missing-declaration":
			ctx = t.Context()
		case "canceled":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		case "wrong-plan":
			candidate.Next.Sha256 = fmt.Sprintf("sha256:%064x", 1)
		case "alias":
			candidate.Request.ArchivePath = candidate.Request.Original.Path
		case "forecast":
			candidate.Request.FutureSegments = 0
		case "unjoined":
			candidate.Request.FormerWriterFence.Sha256 = fmt.Sprintf("sha256:%064x", 1)
		}
		candidate.PlanHash = candidate.hash()
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.metadata, "refused-plan.json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		bad := append([]string(nil), args...)
		bad[3], bad[5] = path, monitorReadDigest(raw)
		var output, diagnostic bytes.Buffer
		if code := runMain(ctx, bad, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("archive granted unreviewed mutation", fault, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
			t.Fatal("refused archive changed original custody", fault)
		}
	}
}

func TestMonitorEvmArchiveMissingRetainedSegmentStopsOnlyAffectedRole(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, true)
	_, args := f.plan(t)
	f.apply(t, args)
	terminal := make(chan int, 1)
	run := f.evm.start(t, monitorServiceHooks{afterWorker: func(role string, exit int) {
		if role == f.evm.policy.Role {
			terminal <- exit
		}
	}})
	if event := run.next(t); !event.Current || event.State.BatchCount != 2 {
		t.Fatal(event)
	}
	select {
	case <-run.sink.peers:
	case <-time.After(10 * time.Second):
		t.Fatal("healthy role did not publish")
	}
	if err := os.Remove(f.archive); err != nil {
		t.Fatal(err)
	}
	run.resume <- struct{}{}
	if event := run.next(t); event.Current || event.Status != "identity-conflict" || event.State.BatchCount != 2 {
		t.Fatal("archive loss became a fresh or successful history", event)
	}
	select {
	case exit := <-terminal:
		if exit != 3 {
			t.Fatal("affected archive loss did not stop", exit)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("affected archive owner did not join")
	}
	run.peerResume <- struct{}{}
	select {
	case <-run.sink.peers:
	case <-run.done:
		t.Fatal("archive loss stopped unrelated role")
	case <-time.After(10 * time.Second):
		t.Fatal("healthy peer did not continue")
	}
	run.stop(t)
}

// Inotify delivers actual access events synchronously with the filesystem
// operations. Nonblocking reads test an empty queue, never elapsed time.
func TestMonitorEvmArchiveUnchangedChecksDoNotRereadPayload(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, false)
	plan, args := f.plan(t)
	f.apply(t, args)
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, f.archive, unix.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	owner, raw, err := openMonitorHistoryReader(f.evm.ctx, plan.Archive)
	if err != nil || !bytes.Equal(raw, f.original) {
		t.Fatal("archive admission did not read original payload", err)
	}
	defer owner.close()
	buffer := make([]byte, 4096)
	if n, err := unix.Read(fd, buffer); err != nil || n == 0 {
		t.Fatal("actual admission access was not observed", n, err)
	}
	for index := 0; index < 16; index++ {
		if err := owner.check(); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := unix.Read(fd, buffer); n > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("unchanged archive reread its payload", n, err)
	}
	file, err := os.OpenFile(f.archive, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{'!'}, 0); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	if err := owner.check(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("changed immutable payload retained cache admission", err)
	}
	if n, err := unix.Read(fd, buffer); n > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("invalid immutable member was reread before refusal", n, err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if replacement, _, err := openMonitorHistoryReader(f.evm.ctx, plan.Archive); err == nil || replacement != nil {
		t.Fatal("reopen admitted altered original archive", err)
	}
}

func TestMonitorEvmArchivePublicRefusesOmittedAndForgedPrefix(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, false)
	_, args := f.plan(t)
	f.apply(t, args)
	original := f.evm.record(t)
	for _, fault := range []string{"amount", "event-count", "digest", "repeat", "missing"} {
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var candidate monitorEconomicEvmCheckpoint
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "amount":
			candidate.State.Archive.FeeWei = "42001"
		case "event-count":
			candidate.State.Archive.Events++
		case "digest":
			candidate.State.Archive.Segments[0].Sha256 = fmt.Sprintf("sha256:%064x", 1)
		case "repeat":
			candidate.State.Archive.Segments = append(candidate.State.Archive.Segments, candidate.State.Archive.Segments[0])
		case "missing":
			candidate.State.Archive = nil
		}
		candidate.ContentHash = candidate.hash()
		raw, err = json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		writer, err := openMonitorHistorySnapshot(f.evm.ctx, f.checkpoint, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(writer.publish(append(raw, '\n'), nil), writer.close()); err != nil {
			t.Fatal(err)
		}
		before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
		run := f.evm.start(t, monitorServiceHooks{wait: func(context.Context, string, time.Duration) bool { return false }})
		select {
		case <-run.done:
			if run.exit != 3 || !strings.Contains(run.diagnostic.String(), "monitor EVM economic admission") {
				t.Fatal("public reopen admitted unproved archive prefix", fault, run.exit, run.diagnostic.String())
			}
		case <-time.After(10 * time.Second):
			t.Fatal("refused archive owner did not join", fault)
		}
		run.stop(t)
		if len(run.sink.events) != 0 {
			t.Fatal("unproved archive prefix emitted an observation", fault)
		}
		// The independent chain role may publish its own sample before joining.
		// Only this refused owner and its original segment must stay byte-exact.
		after := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
		for _, path := range []string{f.checkpoint, f.checkpoint + ".lock", f.archive, f.archive + ".lock"} {
			name := filepath.Base(path)
			if !reflect.DeepEqual(before[name], after[name]) {
				t.Fatal("refused archive reopened or rewrote original progress", fault, name)
			}
		}
	}
}

func TestMonitorEvmArchiveForecastRefusesBeforePublication(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, false)
	before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	for _, fault := range []string{"segments", "bytes", "inodes"} {
		ctx, request := f.evm.ctx, f.request
		if fault == "segments" {
			request.FutureSegments = maximumMonitorHistorySegments / 2
		} else {
			ref, _ := durablevolume.ReferenceFromContext(ctx)
			config, err := durablevolume.Load(ref)
			if err != nil {
				t.Fatal(err)
			}
			for index := range config.Volumes {
				if fault == "bytes" {
					config.Volumes[index].MinAvailableBytes = 1
				} else {
					config.Volumes[index].MinAvailableInodes = 1
				}
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(f.metadata, "insufficient-volumes.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			ctx = durablevolume.WithReference(ctx, durablevolume.Reference{Path: path, Sha256: monitorReadDigest(raw)})
		}
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.metadata, "insufficient-request.json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		var output, diagnostic bytes.Buffer
		args := []string{"monitor-evm-archive", "plan", "--request", path, "--request-sha256", monitorReadDigest(raw)}
		if code := runMain(ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "forecast") {
			t.Fatal("insufficient forecast missed capacity admission", fault, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
			t.Fatal("insufficient forecast published a partial archive", fault)
		}
	}
}
