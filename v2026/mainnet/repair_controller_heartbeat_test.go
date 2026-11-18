// Explicit timer wakes exercise the real command, signed intent, checkpoint
// and metrics owners while the synthetic service manager remains blocked.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type repairHeartbeatTestTick struct {
	stamp time.Time
	err   error
}

type repairHeartbeatTestRun struct {
	f          *repairValidatorFixture
	elapsed    atomic.Int64
	readFault  atomic.Bool
	starts     atomic.Int32
	ticks      chan time.Time
	observed   chan repairHeartbeatTestTick
	joinedHost chan struct{}
	finished   chan struct{}
	cancel     context.CancelFunc
	code       int
	output     bytes.Buffer
	diagnostic bytes.Buffer
}

// The timer hook only wakes the real publisher. The host barrier is entered
// after the actual independently signed start allowance has been consumed.
func newRepairHeartbeatTestRun(t *testing.T) *repairHeartbeatTestRun {
	t.Helper()
	f := newRepairValidatorFixture(t)
	args, _ := repairControllerTestCommand(t, f, "validator", false)
	run := &repairHeartbeatTestRun{f: f, ticks: make(chan time.Time), observed: make(chan repairHeartbeatTestTick, 16), joinedHost: make(chan struct{}), finished: make(chan struct{})}
	entered := make(chan struct{})
	original := f.host.execute
	f.host.execute = func(ctx context.Context, path string, arguments []string) ([]byte, error) {
		if strings.Contains(strings.Join(arguments, " "), " start -- ") {
			run.starts.Add(1)
			close(entered)
			defer close(run.joinedHost)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return original(ctx, path, arguments)
	}
	ctx, cancel := context.WithTimeout(f.storage.Context, 30*time.Second)
	run.cancel = cancel
	ctx = context.WithValue(ctx, repairValidatorObservationKey{}, func(phase string) error {
		if run.readFault.Load() && phase == "owner-journal-read" {
			return syscall.EIO
		}
		return nil
	})
	ctx = context.WithValue(ctx, repairControllerHeartbeatKey{}, repairControllerHeartbeatHooks{ticks: run.ticks, afterTick: func(stamp time.Time, err error) { run.observed <- repairHeartbeatTestTick{stamp: stamp, err: err} }})
	now := func() time.Time { return f.now.Add(time.Duration(run.elapsed.Load()) * time.Second) }
	go func() {
		defer close(run.finished)
		run.code = runRepairControllerCommandWithHost(ctx, args, &run.output, &run.diagnostic, now, f.host)
	}()
	t.Cleanup(func() { run.cancel(); <-run.finished })
	select {
	case <-entered:
	case <-run.finished:
		t.Fatal("controller ended before the original start barrier", run.code, run.diagnostic.String())
	}
	return run
}

// All metric reads follow a completed publication callback, not a time sleep.
func (self *repairHeartbeatTestRun) pulse(t *testing.T, seconds int64) repairHeartbeatTestTick {
	t.Helper()
	self.elapsed.Add(seconds)
	select {
	case self.ticks <- self.f.now.Add(time.Duration(self.elapsed.Load()) * time.Second):
	case <-self.finished:
		t.Fatal("owned long repair lost its heartbeat publisher", self.code, self.diagnostic.String())
	}
	select {
	case result := <-self.observed:
		return result
	case <-self.finished:
		select {
		case result := <-self.observed:
			return result
		default:
			t.Fatal("heartbeat owner stopped without joining its observation", self.code, self.diagnostic.String())
		}
	}
	return repairHeartbeatTestTick{}
}

func repairHeartbeatTestMetrics(t *testing.T, path string) map[string]uint64 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "sn_mainnet_repair_controller_") {
			t.Fatal("controller heartbeat escaped its scalar contract", line)
		}
		name := strings.TrimPrefix(parts[0], "sn_mainnet_repair_controller_")
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := values[name]; exists {
			t.Fatal("duplicate controller metric", name)
		}
		values[name] = value
	}
	if len(values) != 6 || len(raw) > 2048 {
		t.Fatal("controller liveness changed the finite outcome contract", values)
	}
	return values
}

// Two real publications advance only liveness while the original signed
// operation and acknowledged checkpoint remain unchanged and incomplete.
func TestRepairControllerHeartbeatAdvancesWithoutRepairProgress(t *testing.T) {
	run := newRepairHeartbeatTestRun(t)
	checkpoint := filepath.Join(run.f.directory, "controller.json")
	before, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	for _, seconds := range []int64{30, 30} {
		if observed := run.pulse(t, seconds); observed.err != nil {
			t.Fatal("actual long action lost liveness publication", observed.err)
		}
		values := repairHeartbeatTestMetrics(t, filepath.Join(run.f.directory, "controller.prom"))
		if values["heartbeat_timestamp_seconds"] != uint64(run.f.now.Unix()+run.elapsed.Load()) || values["sample_timestamp_seconds"] != uint64(run.f.now.Unix()) || values["active"] != 1 || values["completed"] != 0 || run.starts.Load() != 1 {
			t.Fatal("heartbeat fabricated progress or repeated the original start", values)
		}
		after, err := os.ReadFile(checkpoint)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("liveness tick rewrote the original action census", err)
		}
	}
	run.cancel()
	<-run.finished
	<-run.joinedHost
	if run.code != 0 || run.output.Len() != 0 {
		t.Fatal("cancellation published completion or failed to join the action", run.code, run.diagnostic.String())
	}
	select {
	case run.ticks <- run.f.now.Add(90 * time.Second):
		t.Fatal("timer still accepted work after command and action joined")
	default:
	}
	if repairControllerHeartbeatInterval != 30*time.Second {
		t.Fatal("public heartbeat cadence differs from the reviewed operating SLO")
	}
}

// A real owner read fails after its syscall. No heartbeat is published until
// the same owner can be observed again; the pending child is not restarted.
func TestRepairControllerHeartbeatUnavailableFenceRetainsAndRecovers(t *testing.T) {
	run := newRepairHeartbeatTestRun(t)
	path := filepath.Join(run.f.directory, "controller.prom")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	run.readFault.Store(true)
	if observed := run.pulse(t, 30); !errors.Is(observed.err, syscall.EIO) {
		t.Fatal("physical owner observation fault did not reach heartbeat", observed.err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("unavailable owner acquired fresh liveness", err)
	}
	run.readFault.Store(false)
	if observed := run.pulse(t, 30); observed.err != nil {
		t.Fatal("same admitted owner could not recover observation", observed.err)
	}
	values := repairHeartbeatTestMetrics(t, path)
	if values["heartbeat_timestamp_seconds"] != uint64(run.f.now.Add(time.Minute).Unix()) || values["sample_timestamp_seconds"] != uint64(run.f.now.Unix()) || values["active"] != 1 || values["completed"] != 0 || run.starts.Load() != 1 {
		t.Fatal("heartbeat recovery lost the original outcome", values)
	}
	run.cancel()
	<-run.finished
	if run.code != 0 {
		t.Fatal("recovered heartbeat left a spurious terminal failure", run.code, run.diagnostic.String())
	}
}

// Equal marker bytes cannot transfer the actual checkpoint owner. The detected
// contradiction joins the blocked service-manager child before returning.
func TestRepairControllerHeartbeatLostCustodyCancelsAndJoins(t *testing.T) {
	run := newRepairHeartbeatTestRun(t)
	lock := filepath.Join(run.f.directory, "controller.json.lock")
	raw, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	metrics := filepath.Join(run.f.directory, "controller.prom")
	before, err := os.ReadFile(metrics)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(lock, lock+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if observed := run.pulse(t, 30); observed.err == nil {
		t.Fatal("replaced checkpoint marker acquired liveness")
	}
	<-run.finished
	<-run.joinedHost
	after, err := os.ReadFile(metrics)
	if run.code != 3 || err != nil || !bytes.Equal(before, after) || run.starts.Load() != 1 || run.output.Len() != 0 {
		t.Fatal("lost shared custody refreshed metrics or escaped joined refusal", run.code, err, run.diagnostic.String())
	}
}

func TestRepairControllerHeartbeatClockRollbackCannotRefreshLiveness(t *testing.T) {
	run := newRepairHeartbeatTestRun(t)
	path := filepath.Join(run.f.directory, "controller.prom")
	if observed := run.pulse(t, 30); observed.err != nil {
		t.Fatal(observed.err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if observed := run.pulse(t, -1); !errors.Is(observed.err, errRpcIntegrity) {
		t.Fatal("heartbeat clock rollback became current liveness", observed.err)
	}
	<-run.finished
	<-run.joinedHost
	after, err := os.ReadFile(path)
	if run.code != 3 || err != nil || !bytes.Equal(before, after) {
		t.Fatal("clock refusal failed to retain its prior metrics", run.code, err)
	}
}

// An optional output owner can fail independently of the already admitted
// repair. It publishes no new liveness, and its hard cause survives shutdown.
func TestRepairControllerHeartbeatMetricsFailureDoesNotRestartAction(t *testing.T) {
	run := newRepairHeartbeatTestRun(t)
	path := filepath.Join(run.f.directory, "controller.prom")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lock := path + ".lock"
	raw, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(lock, lock+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, raw, 0600); err != nil {
		t.Fatal(err)
	}
	observed := run.pulse(t, 30)
	var ownership *monitorOutputOwnershipError
	if !errors.As(observed.err, &ownership) {
		t.Fatal("physical metrics owner loss was not observed", observed.err)
	}
	select {
	case <-run.finished:
		t.Fatal("optional heartbeat output interrupted an admitted action", run.code)
	default:
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || run.starts.Load() != 1 {
		t.Fatal("lost metrics custody refreshed output or repeated the action", err)
	}
	// A later unreadable shared fence must not overwrite the completed output
	// refusal. It remains independent of the one already consumed start.
	run.readFault.Store(true)
	if observed := run.pulse(t, 30); !errors.Is(observed.err, syscall.EIO) {
		t.Fatal("later soft owner read did not reach its physical fence", observed.err)
	}
	run.readFault.Store(false)
	if observed := run.pulse(t, 30); !errors.Is(observed.err, errRepairControllerHeartbeatHeld) {
		t.Fatal("restored shared read erased the metrics custody hold", observed.err)
	}
	run.cancel()
	<-run.finished
	<-run.joinedHost
	if run.code != 3 {
		t.Fatal("joined cancellation hid the completed metrics ownership refusal", run.code, run.diagnostic.String())
	}
}

// An independently unavailable owner read cannot hide a completed refusal of
// the exact original manifest, including when shutdown also cancels the child.
func TestRepairControllerHeartbeatChangedManifestDominatesUnavailableFence(t *testing.T) {
	run := newRepairHeartbeatTestRun(t)
	path := filepath.Join(run.f.directory, "repair-manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	metrics := filepath.Join(run.f.directory, "controller.prom")
	before, err := os.ReadFile(metrics)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	run.readFault.Store(true)
	observed := run.pulse(t, 30)
	if !errors.Is(observed.err, syscall.EIO) || !strings.Contains(observed.err.Error(), "pin changed") {
		t.Fatal("actual independent fence failures were not retained", observed.err)
	}
	<-run.finished
	<-run.joinedHost
	after, err := os.ReadFile(metrics)
	if run.code != 3 || err != nil || !bytes.Equal(before, after) || run.starts.Load() != 1 || run.output.Len() != 0 {
		t.Fatal("manifest contradiction inherited read availability", run.code, err, run.diagnostic.String())
	}
}

// A textfile is replaceable telemetry, not an action journal. Failed directory
// sync can expose bytes without acknowledging publication; the next complete
// publication may recover the same owner, without editing repair progress.
func TestRepairControllerHeartbeatMetricsIoRecoversSameOwner(t *testing.T) {
	f := newRepairValidatorFixture(t)
	args, manifest := repairControllerTestCommand(t, f, "validator", false)
	ctx, cancel := context.WithCancel(f.storage.Context)
	defer cancel()
	store, record, err := openRepairControllerStore(ctx, args[5], manifest, args[3], f.now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	metrics, err := openMonitorMetrics(args[7], ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer metrics.close()
	stamp := f.now.Add(30 * time.Second)
	heartbeat := &repairControllerHeartbeat{store: store, metrics: metrics, manifest: manifest, hash: args[3], now: func() time.Time { return stamp }, checkManifest: func(observation context.Context) error {
		return f.host.pin(observation, planFileReference{Path: args[1], Sha256: args[3]}, 64*1024, false)
	}, cancelOwner: cancel, stderr: io.Discard}
	heartbeat.retain(record)
	before, err := os.ReadFile(args[5])
	if err != nil {
		t.Fatal(err)
	}
	metrics.syncDirectory = func(file *os.File) error { return errors.Join(file.Sync(), syscall.EIO) }
	if err := heartbeat.publish(ctx); !errors.Is(err, syscall.EIO) || heartbeat.outputHeld != nil || !heartbeat.lastHeartbeat.IsZero() || ctx.Err() != nil {
		t.Fatal("ordinary telemetry I/O acquired a permanent hold or acknowledgment", err, heartbeat.outputHeld)
	}
	metrics.syncDirectory = nil
	stamp = stamp.Add(30 * time.Second)
	if err := heartbeat.publish(ctx); err != nil {
		t.Fatal("same metrics owner could not recover", err)
	}
	values := repairHeartbeatTestMetrics(t, args[7])
	after, err := os.ReadFile(args[5])
	if err != nil || !bytes.Equal(before, after) || values["heartbeat_timestamp_seconds"] != uint64(stamp.Unix()) || values["sample_timestamp_seconds"] != uint64(f.now.Unix()) || values["completed"] != 0 || heartbeat.close() != nil {
		t.Fatal("telemetry recovery changed the retained census", err, values)
	}
}

// A real protected manifest read may finish under caller cancellation. Display
// context is a wrapper, so the strict fence classifier sees only that soft cause.
func TestRepairControllerHeartbeatManifestReadCancellationStaysSoft(t *testing.T) {
	f := newRepairValidatorFixture(t)
	args, _ := repairControllerTestCommand(t, f, "validator", false)
	ctx, cancel := context.WithCancel(f.storage.Context)
	defer cancel()
	ctx = context.WithValue(ctx, repairValidatorObservationKey{}, func(phase string) error {
		if phase == "host-pin-stat" {
			cancel()
		}
		return nil
	})
	err := f.host.pin(ctx, planFileReference{Path: args[1], Sha256: args[3]}, 64*1024, false)
	if !errors.Is(err, context.Canceled) || !rootMonitorStartupPending(err) {
		t.Fatal("completed read cancellation became a manifest contradiction", err)
	}
}
