//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type monitorStartupArchiveFixture struct {
	services                  *monitorServicesFixture
	ctx                       context.Context
	role, checkpoint, archive string
}

// The original checkpoint and archive are produced by the real public owner
// and offline archive commands before any selected admission fault is enabled.
func newMonitorStartupArchiveFixture(t *testing.T, kind string) monitorStartupArchiveFixture {
	t.Helper()
	var result monitorStartupArchiveFixture
	switch kind {
	case "native":
		f := newMonitorNativeArchiveFixture(t, true)
		_, args := f.plan(t)
		f.apply(t, args)
		result = monitorStartupArchiveFixture{f.native.services, f.native.ctx, f.native.policy.Role, f.checkpoint, f.archive}
	case "evm":
		f := newMonitorEvmArchiveFixture(t, true)
		_, args := f.plan(t)
		f.apply(t, args)
		result = monitorStartupArchiveFixture{f.evm.services, f.evm.ctx, f.evm.policy.Role, f.checkpoint, f.archive}
	case "claim":
		f := newMonitorClaimArchiveFixture(t, nil)
		_, args := f.plan(t)
		f.apply(t, args)
		result = monitorStartupArchiveFixture{f.services, f.ctx, f.policy.Role, f.checkpoint, f.archive}
		value := monitorServicesTestRecord(f.services.clock.now(), 1)
		peer := monitorValidatorPolicy{Role: "validator-a", ProgressFile: filepath.Join(f.services.directory, "validator-a.json"), ExpectedSource: value.Source}
		f.services.policy.Validators = []monitorValidatorPolicy{peer}
		monitorServicesTestWrite(t, peer.ProgressFile, value)
		checkpoint, _ := monitorValidatorPaths(f.services.checkpointPath, f.services.metricsPath, peer.Role)
		provisionMonitorTestCustody(t, checkpoint)
		f.services.writePolicy(t)
	default:
		t.Fatal("unknown archived owner", kind)
	}
	return result
}

func monitorStartupArchiveCleanup(t *testing.T, kind string) {
	t.Helper()
	f := newMonitorStartupArchiveFixture(t, kind)
	before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	url, _, _ := monitorServicesBlockedChain(t)
	var reads, releases atomic.Int32
	var released atomic.Pointer[os.File]
	ctx := context.WithValue(f.ctx, monitorHistoryAdmissionReadKey{}, func(path string, file *os.File) error {
		if path != f.archive {
			return nil
		}
		reads.Add(1)
		// The actual read completed, but its outcome becomes unavailable.
		// Closing this same file makes the deferred close report ErrClosed;
		// no callback manufactures a successful read or a close result.
		if err := file.Close(); err != nil {
			return err
		}
		releases.Add(1)
		released.Store(file)
		return syscall.EIO
	})
	ctx, cancel := context.WithCancel(ctx)
	run := &monitorServicesTestRun{cancel: cancel, done: make(chan struct{}), events: make(chan monitorServiceEvent, 8), resume: map[string]chan struct{}{"validator-a": make(chan struct{})}}
	sink := &monitorStartupStatusSink{samples: run.events, admissions: make(chan monitorAdmissionEvent, 8)}
	terminal := make(chan int, 1)
	hooks := monitorServiceHooks{wait: func(ctx context.Context, role string, _ time.Duration) bool {
		select {
		case <-run.resume[role]:
			return true
		case <-ctx.Done():
			return false
		}
	}, afterWorker: func(role string, exit int) {
		if role == f.role {
			terminal <- exit
		}
	}}
	go func() {
		defer close(run.done)
		run.exit = runMonitorStorageTestWithHooks(t, ctx, f.services.args(url), sink, &run.stderr, f.services.clock.now, hooks)
	}()
	t.Cleanup(func() { run.cancel(); <-run.done })
	var admission monitorAdmissionEvent
	select {
	case admission = <-sink.admissions:
	case <-run.done:
		t.Fatal("archived owner exited before its admission status", kind, run.exit, run.stderr.String())
	case <-time.After(time.Minute):
		t.Fatal("archived owner admission status did not arrive", kind)
	}
	if admission.Role != f.role || admission.Status != "quarantined" || admission.Reason != "cleanup-unresolved" || admission.RetryAfterSeconds != 0 || admission.CheckpointCurrent || admission.MetricsCurrent {
		t.Fatal("nested archive release failure became another ownership attempt", kind, admission)
	}
	if exit := <-terminal; exit != 3 || reads.Load() != 1 || releases.Load() != 1 {
		t.Fatal("nested owner lifecycle was not retained", kind, exit, reads.Load(), releases.Load())
	}
	if file := released.Load(); file == nil {
		t.Fatal("actual archive reader was not owned", kind)
	} else if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("nested reader remained open", kind, err)
	}
	if event := monitorStartupNext(t, run); event.Role != "validator-a" || event.Publication != "published" {
		t.Fatal("archive cleanup stopped healthy peer", kind, event)
	}
	run.again(t, "validator-a")
	if event := monitorStartupNext(t, run); event.Role != "validator-a" || event.Publication != "published" || reads.Load() != 1 {
		t.Fatal("quarantined archive was reacquired or peer stalled", kind, event, reads.Load())
	}
	run.cancel()
	<-run.done
	if run.exit != 3 {
		t.Fatal("nested cleanup outcome was discarded", kind, run.exit, run.stderr.String())
	}
	after := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
	for _, path := range []string{f.checkpoint, f.archive, f.checkpoint + ".lock", f.archive + ".lock"} {
		name := filepath.Base(path)
		if !reflect.DeepEqual(before[name], after[name]) {
			t.Fatal("refused archive admission rewrote original custody", kind, name)
		}
	}
}

func TestMonitorStartupNativeNestedCleanupCannotRetry(t *testing.T) {
	monitorStartupArchiveCleanup(t, "native")
}
func TestMonitorStartupEvmNestedCleanupCannotRetry(t *testing.T) {
	monitorStartupArchiveCleanup(t, "evm")
}
func TestMonitorStartupClaimNestedCleanupCannotRetry(t *testing.T) {
	monitorStartupArchiveCleanup(t, "claim")
}

// An ordinary failed read releases its exact reader and remains retryable;
// closure uncertainty is not inferred from the error alone.
func TestMonitorStartupArchiveReadWithoutCleanupFailureCanResume(t *testing.T) {
	f := newMonitorNativeArchiveFixture(t, false)
	plan, args := f.plan(t)
	f.apply(t, args)
	ctx := context.WithValue(f.native.ctx, monitorHistoryAdmissionReadKey{}, func(string, *os.File) error { return syscall.EIO })
	owner, raw, err := openMonitorHistoryReader(ctx, plan.Archive)
	var cleanup *monitorAdmissionCleanupError
	if owner != nil || raw != nil || !errors.Is(err, syscall.EIO) || errors.As(err, &cleanup) || !monitorStartupPending(err) {
		t.Fatal("ordinary read failure invented uncertain cleanup", err)
	}
	owner, raw, err = openMonitorHistoryReader(f.native.ctx, plan.Archive)
	if err != nil || monitorReadDigest(raw) != plan.Archive.Sha256 {
		t.Fatal("same original archive could not readmit", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
}

// The real Claim checkpoint has an uncertain acknowledgement after publication.
// Its next exact archive admission fails during release, so no third owner is
// acquired while the independent validator continues to publish.
func TestMonitorClaimRecoveryNestedCleanupCannotReacquire(t *testing.T) {
	f := newMonitorStartupArchiveFixture(t, "claim")
	url, _, _ := monitorServicesBlockedChain(t)
	var reads, waits atomic.Int32
	var failed atomic.Bool
	ctx := context.WithValue(f.ctx, monitorHistoryAdmissionReadKey{}, func(path string, file *os.File) error {
		if path != f.archive || reads.Add(1) == 1 {
			return nil
		}
		if err := file.Close(); err != nil {
			return err
		}
		return syscall.EIO
	})
	ctx, cancel := context.WithCancel(ctx)
	run := &monitorServicesTestRun{cancel: cancel, done: make(chan struct{}), events: make(chan monitorServiceEvent, 8), resume: map[string]chan struct{}{"validator-a": make(chan struct{})}}
	sink := &monitorStartupStatusSink{samples: run.events, admissions: make(chan monitorAdmissionEvent, 8)}
	terminal, retry := make(chan int, 1), make(chan struct{}, 1)
	retained := make(chan []byte, 1)
	hooks := monitorServiceHooks{
		syncDirectory: func(role, kind string, file *os.File) error {
			err := file.Sync()
			if role == f.role && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
				return errors.Join(err, syscall.EIO)
			}
			return err
		},
		afterClose: func(role, kind string, file *os.File) error {
			if role == f.role && kind == "checkpoint" && waits.Load() == 0 {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("original Claim owner did not join")
				}
				raw, err := os.ReadFile(f.checkpoint)
				if err != nil {
					return err
				}
				retained <- raw
			}
			return nil
		},
		wait: func(ctx context.Context, role string, _ time.Duration) bool {
			if role == f.role {
				if waits.Add(1) == 1 {
					return true
				}
				select {
				case retry <- struct{}{}:
				default:
				}
				<-ctx.Done()
				return false
			}
			select {
			case <-run.resume[role]:
				return true
			case <-ctx.Done():
				return false
			}
		},
		afterWorker: func(role string, exit int) {
			if role == f.role {
				terminal <- exit
			}
		},
	}
	go func() {
		defer close(run.done)
		run.exit = runMonitorStorageTestWithHooks(t, ctx, f.services.args(url), sink, &run.stderr, f.services.clock.now, hooks)
	}()
	t.Cleanup(func() { cancel(); <-run.done })
	select {
	case exit := <-terminal:
		if exit != 3 || reads.Load() != 2 || waits.Load() != 1 || !failed.Load() {
			t.Fatal("Claim recovery discarded nested cleanup failure", exit, reads.Load(), waits.Load(), failed.Load())
		}
	case <-retry:
		t.Fatal("Claim recovery retried after nested archive cleanup failure")
	case <-time.After(time.Minute):
		t.Fatal("Claim archive recovery did not join")
	}
	if event := monitorStartupNext(t, run); event.Role != "validator-a" || event.Publication != "published" {
		t.Fatal("Claim recovery stopped healthy peer", event)
	}
	run.again(t, "validator-a")
	if event := monitorStartupNext(t, run); event.Role != "validator-a" || event.Publication != "published" {
		t.Fatal("Claim recovery blocked later peer publication", event)
	}
	var original []byte
	select {
	case original = <-retained:
	default:
		t.Fatal("uncertain original checkpoint was not retained")
	}
	after, err := os.ReadFile(f.checkpoint)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("failed recovery rewrote original Claim checkpoint", err)
	}
	cancel()
	<-run.done
	if run.exit != 3 {
		t.Fatal("Claim recovery cleanup was canceled away", run.exit, run.stderr.String())
	}
}

// Parent cancellation coincides with the actual old checkpoint's close. The
// failed second close remains an error even though every other worker joins.
func TestMonitorStorageRecoveryCancellationRetainsCloseFailure(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var failed, joined atomic.Bool
	var reopens atomic.Int32
	hooks := monitorServiceHooks{
		syncDirectory: func(role, kind string, file *os.File) error {
			err := file.Sync()
			if role == "alpha" && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
				return errors.Join(err, syscall.EIO)
			}
			return err
		},
		afterClose: func(role, kind string, file *os.File) error {
			if role == "alpha" && kind == "checkpoint" && joined.CompareAndSwap(false, true) {
				closeErr := file.Close()
				cancel()
				return errors.Join(closeErr, ctx.Err())
			}
			return nil
		},
		wait: func(ctx context.Context, role string, _ time.Duration) bool {
			if role == "alpha" {
				reopens.Add(1)
			}
			<-ctx.Done()
			return false
		},
	}
	run := &monitorServicesTestRun{cancel: cancel, done: make(chan struct{}), events: make(chan monitorServiceEvent, 32)}
	go func() {
		defer close(run.done)
		run.exit = runMonitorStorageTestWithHooks(t, ctx, fixture.args(url), &monitorServicesTestWriter{events: run.events}, &run.stderr, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { cancel(); <-run.done })
	select {
	case <-run.done:
	case <-time.After(time.Minute):
		t.Fatal("canceled recovery did not join")
	}
	if run.exit != 3 || !failed.Load() || !joined.Load() || reopens.Load() != 0 {
		t.Fatal("cancellation erased original close failure", run.exit, failed.Load(), joined.Load(), reopens.Load(), run.stderr.String())
	}
}

func TestMonitorStorageRecoveryCleanupMarkerDominatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	recovery := monitorStorageRecovery{}
	reopens := 0
	err := recovery.resume(ctx, "retained-role", func() error { return ctx.Err() }, func() error { reopens++; return nil }, monitorServiceHooks{})
	var cleanup *monitorAdmissionCleanupError
	if !errors.As(err, &cleanup) || !errors.Is(err, ctx.Err()) || monitorCanceledCheckpointLoad(ctx, err) || monitorStartupPending(err) || monitorStoragePending(err) || reopens != 0 || recovery.attempts != 0 {
		t.Fatal("cleanup cancellation became an ordinary canceled observation", err, reopens, recovery.attempts)
	}
	err = recovery.resume(ctx, "retained-role", func() error { return nil }, func() error { reopens++; return nil }, monitorServiceHooks{})
	if !monitorCanceledCheckpointLoad(ctx, err) || reopens != 0 {
		t.Fatal("pure joined cancellation became a custody failure", err, reopens)
	}
}
