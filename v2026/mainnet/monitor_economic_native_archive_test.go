//go:build linux || darwin

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
)

// Test enrollment creates only explicitly fresh snapshot owners. The original
// native checkpoint has already been published by the actual public monitor.
type monitorNativeArchiveFixture struct {
	native     *monitorEconomicTestFixture
	checkpoint string
	archive    string
	metadata   string
	request    monitorNativeArchiveRequest
	original   []byte
}

func newMonitorNativeArchiveFixture(t *testing.T, peer bool) *monitorNativeArchiveFixture {
	t.Helper()
	return newMonitorNativeArchiveFixtureWithPolicy(t, peer, nil)
}

func newMonitorNativeArchiveFixtureWithPolicy(t *testing.T, peer bool, configure func(*monitorEconomicNativePolicy)) *monitorNativeArchiveFixture {
	t.Helper()
	native := newMonitorEconomicTestFixture(t, peer)
	native.policy.HistoryEntries = 1
	if configure != nil {
		configure(&native.policy)
	}
	run := native.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || event.State.Cursor.Number != 101 || event.State.PendingThrough == nil || event.State.PendingThrough.Number != 102 {
		t.Fatal("original public checkpoint did not retain the incomplete range", event)
	}
	run.resume <- struct{}{}
	if event := run.next(t); event.Current || event.Status != "capacity-held" || event.State.BatchCount != 1 {
		t.Fatal("original role did not hold its full history", event)
	}
	run.stop(t)
	checkpoint, _ := monitorEconomicNativePaths(native.services.checkpointPath, native.services.metricsPath, native.policy.Role)
	metadata := t.TempDir()
	if err := os.Chmod(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	self := &monitorNativeArchiveFixture{native: native, checkpoint: checkpoint, archive: filepath.Join(filepath.Dir(checkpoint), "native-archive-001.json"), metadata: metadata}
	// The reviewed operational declaration supplies the explicit two-times
	// physical forecast margin. No journal or physical generation is rewritten.
	ref, _ := durablevolume.ReferenceFromContext(native.ctx)
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
	native.ctx = durablevolume.WithReference(native.ctx, ref)
	self.resetRequest(t)
	return self
}

func (self *monitorNativeArchiveFixture) resetRequest(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(self.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	self.original = raw
	provisionMonitorTestCustody(t, self.archive)
	self.request = monitorNativeArchiveRequest{Schema: monitorNativeArchiveRequestSchema, Policy: self.native.policy,
		Expected: identityExpectation{NativeChain: self.native.policy.Observation.Network.NativeChain, GenesisHash: self.native.policy.Observation.Network.GenesisHash, EvmChainId: mainnetEvmChainId},
		Original: monitorHistoryReference{Path: self.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, ArchivePath: self.archive, FutureSegments: 2}
	fence := monitorHistoryWriterFence{Schema: monitorHistoryWriterFenceSchema, Original: self.request.Original, PolicyHash: self.native.policy.identityHash(), StoppedAndJoined: true}
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

func (self *monitorNativeArchiveFixture) plan(t *testing.T) (monitorNativeArchivePlan, []string) {
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
	args := []string{"monitor-native-archive", "plan", "--request", path, "--request-sha256", monitorReadDigest(raw)}
	if code := runMain(self.native.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("public native archive plan failed", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(self.checkpoint))) {
		t.Fatal("read-only archive planning changed original custody")
	}
	var plan monitorNativeArchivePlan
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
	return plan, []string{"monitor-native-archive", "apply", "--plan", path, "--plan-sha256", monitorReadDigest(output.Bytes())}
}

func (self *monitorNativeArchiveFixture) apply(t *testing.T, args []string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(self.native.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("public native archive apply failed", code, diagnostic.String())
	}
	raw, err := os.ReadFile(self.archive)
	if err != nil || !bytes.Equal(raw, self.original) {
		t.Fatal("archive did not retain exact original checkpoint bytes", err)
	}
}

func TestMonitorNativeArchivePublicRolloverContinuesOriginalPendingRange(t *testing.T) {
	f := newMonitorNativeArchiveFixture(t, false)
	original := f.native.record(t)
	_, args := f.plan(t)
	f.apply(t, args)
	compacted := f.native.record(t)
	if compacted.State.Cursor != original.State.Cursor || !reflect.DeepEqual(compacted.State.PendingThrough, original.State.PendingThrough) || compacted.State.BatchCount != original.State.BatchCount || compacted.State.BatchChainHash != original.State.BatchChainHash || compacted.State.ObservedAlpha != original.State.ObservedAlpha || len(compacted.State.History) != 0 || compacted.State.Status != "archive-ready" {
		t.Fatal("archive reset original progress", compacted)
	}
	run := f.native.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.BatchCount != 2 || event.State.PendingThrough != nil || event.State.ObservedAlpha != "10" || monitorEconomicTestFee(event) != "12" || event.State.ArchiveSegments != 1 || event.State.ArchivedEvents != 1 {
		t.Fatal("public archive adoption restarted or skipped its original pending range", event)
	}
	run.stop(t)
	firstArchive := f.archive
	f.archive = filepath.Join(filepath.Dir(f.checkpoint), "native-archive-002.json")
	f.resetRequest(t)
	_, args = f.plan(t)
	f.apply(t, args)
	run = f.native.start(t, monitorServiceHooks{})
	event = run.next(t)
	if !event.Current || event.State.BatchCount != 2 || event.State.ArchiveSegments != 2 || event.State.ArchivedEvents != 2 || event.State.ObservedAlpha != "10" || monitorEconomicTestFee(event) != "12" {
		t.Fatal("second archive forgot the complete original history", event)
	}
	run.stop(t)
	if _, err := os.Stat(firstArchive); err != nil {
		t.Fatal("rollover removed a previous segment", err)
	}
}

func TestMonitorNativeArchivePublicLostAcknowledgmentsReuseExactHeads(t *testing.T) {
	for _, stage := range []string{"archive", "checkpoint"} {
		f := newMonitorNativeArchiveFixture(t, false)
		_, args := f.plan(t)
		cause := errors.New("synthetic acknowledgment lost after real directory sync")
		calls := 0
		hooks := monitorServiceHooks{syncDirectory: func(role, kind string, _ *os.File) error {
			if role == f.native.policy.Role && kind == stage {
				calls++
				return cause
			}
			return nil
		}}
		var output, diagnostic bytes.Buffer
		if code := runMainWithMonitorHooks(f.native.ctx, args, &output, &diagnostic, time.Now, hooks); code == 0 || output.Len() != 0 || calls != 1 {
			t.Fatal("lost archive acknowledgment was silently retried", stage, code, calls, diagnostic.String())
		}
		f.apply(t, args)
		before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
		f.apply(t, args)
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
			t.Fatal("repeat reviewed apply republished a completed head", stage)
		}
		run := f.native.start(t, monitorServiceHooks{})
		if event := run.next(t); !event.Current || event.State.BatchCount != 2 || event.State.Cursor.Number != 102 {
			t.Fatal("lost acknowledgment replay lost original cursor", stage, event)
		}
		run.stop(t)
	}
}

func TestMonitorNativeArchivePublicRefusesActiveAndUnreviewedOwners(t *testing.T) {
	f := newMonitorNativeArchiveFixture(t, false)
	plan, args := f.plan(t)
	before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	owner, err := openMonitorHistorySnapshot(f.native.ctx, f.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMonitorNativeArchive(f.native.ctx, plan, monitorServiceHooks{}); !errors.Is(err, durablevolume.ErrBusy) {
		t.Fatal("archive did not refuse active original owner", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"missing-declaration", "canceled", "wrong-plan", "alias", "forecast", "unjoined"} {
		candidate := plan
		ctx := f.native.ctx
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

func TestMonitorNativeArchiveMissingRetainedSegmentStopsOnlyAffectedRole(t *testing.T) {
	f := newMonitorNativeArchiveFixture(t, true)
	_, args := f.plan(t)
	f.apply(t, args)
	terminal := make(chan int, 1)
	run := f.native.start(t, monitorServiceHooks{afterWorker: func(role string, exit int) {
		if role == f.native.policy.Role {
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

func TestMonitorNativeArchivePublicRefusesOmittedAndForgedPrefix(t *testing.T) {
	f := newMonitorNativeArchiveFixture(t, false)
	_, args := f.plan(t)
	f.apply(t, args)
	original := f.native.record(t)
	for _, fault := range []string{"amount", "event-count", "digest", "repeat", "missing"} {
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var candidate monitorEconomicNativeCheckpoint
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "amount":
			candidate.State.Archive.ObservedAlpha, candidate.State.ObservedAlpha = "11", "11"
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
		writer, err := openMonitorHistorySnapshot(f.native.ctx, f.checkpoint, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(writer.publish(append(raw, '\n'), nil), writer.close()); err != nil {
			t.Fatal(err)
		}
		before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
		run := f.native.start(t, monitorServiceHooks{wait: func(context.Context, string, time.Duration) bool { return false }})
		select {
		case <-run.done:
			if run.exit != 3 || !strings.Contains(run.diagnostic.String(), "monitor native economic admission") {
				t.Fatal("public reopen admitted unproved archive prefix", fault, run.exit, run.diagnostic.String())
			}
		case <-time.After(10 * time.Second):
			t.Fatal("refused archive owner did not join", fault)
		}
		run.stop(t)
		if len(run.sink.economics) != 0 {
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

func TestMonitorNativeArchiveForecastRefusesBeforePublication(t *testing.T) {
	f := newMonitorNativeArchiveFixture(t, false)
	before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	for _, fault := range []string{"segments", "bytes", "inodes"} {
		ctx, request := f.native.ctx, f.request
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
		args := []string{"monitor-native-archive", "plan", "--request", path, "--request-sha256", monitorReadDigest(raw)}
		if code := runMain(ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "forecast") {
			t.Fatal("insufficient forecast missed capacity admission", fault, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
			t.Fatal("insufficient forecast published a partial archive", fault)
		}
	}
}
