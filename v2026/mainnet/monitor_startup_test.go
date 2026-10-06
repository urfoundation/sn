// Public startup controls use actual private files, flock ownership and emitted
// peer samples. Waits are explicit barriers; elapsed silence is not evidence.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The sample itself proves progress. This bound only fails an old eager opener
// that remains at the held barrier; it is never used as successful evidence.
func monitorStartupNext(t *testing.T, run *monitorServicesTestRun) monitorServiceEvent {
	t.Helper()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	select {
	case event := <-run.events:
		return event
	case <-run.done:
		t.Fatal("supervisor ended before independent sample", run.exit, run.stderr.String())
	case <-timer.C:
		t.Fatal("independent role did not publish while startup was held")
	case <-t.Context().Done():
		t.Fatal("canceled before independent role sample")
	}
	return monitorServiceEvent{}
}

// These fixtures admit complete independent policies but never reach the held
// role's network/database read. Native/EVM reuse the actual metadata profiles.
func monitorStartupFixture(t *testing.T, kind string) (*monitorServicesFixture, string, string) {
	t.Helper()
	f := newMonitorServicesFixture(t, "healthy")
	role, checkpoint := "held", ""
	switch kind {
	case "validator":
		f = newMonitorServicesFixture(t, role, "healthy")
		checkpoint, _ = monitorValidatorPaths(f.checkpointPath, f.metricsPath, role)
	case "operator":
		policy := monitorOperatorTestPolicy()
		policy.Role = role
		policy.DatabaseFile = filepath.Join(f.directory, "operator.url")
		raw := []byte("postgres://observer@synthetic.invalid/test\n")
		if err := os.WriteFile(policy.DatabaseFile, raw, 0600); err != nil {
			t.Fatal(err)
		}
		policy.DatabaseSha256 = monitorReadDigest(raw)
		f.policy.Operators = []monitorOperatorPolicy{policy}
		checkpoint, _ = monitorOperatorPaths(f.checkpointPath, f.metricsPath, role)
	case "provider":
		policy := monitorProviderTestPolicy(monitorProviderTestValue(f.clock.now()), "https://synthetic.invalid/provider-progress")
		policy.Role = role
		f.policy.Providers = []monitorProviderPolicy{policy}
		checkpoint, _ = monitorProviderPaths(f.checkpointPath, f.metricsPath, role)
	case "claim":
		policy := monitorClaimTestPolicy(t, monitorClaimTestValue(f.clock.now()), "https://synthetic.invalid/claim-progress", f.clock.now())
		policy.Role = role
		f.policy.Claims = []monitorClaimPolicy{policy}
		checkpoint, _ = monitorClaimPaths(f.checkpointPath, f.metricsPath, role)
	case "native":
		native := newMonitorEconomicTestFixture(t, true)
		f, role = native.services, native.policy.Role
		f.policy.NativeEconomics = []monitorEconomicNativePolicy{native.policy}
		checkpoint, _ = monitorEconomicNativePaths(f.checkpointPath, f.metricsPath, role)
	case "evm":
		evm := newMonitorEvmFixture(t, "settlement-vault", true)
		f, role = evm.services, evm.policy.Role
		f.policy.EvmEconomics = []monitorEconomicEvmPolicy{evm.policy}
		checkpoint, _ = monitorEconomicEvmPaths(f.checkpointPath, f.metricsPath, role)
	default:
		t.Fatal("unknown startup fixture family", kind)
	}
	f.writePolicy(t)
	provisionMonitorTestCustody(t, checkpoint)
	return f, role, checkpoint
}

// A post-open barrier follows real descriptor admission and withholds only
// that role. Every other worker begins before the held role is released.
func TestMonitorStartupPublicBlockedAdmissionPreservesEveryPeerFamily(t *testing.T) {
	for _, kind := range []string{"validator", "operator", "provider", "claim", "native", "evm"} {
		f, role, checkpoint := monitorStartupFixture(t, kind)
		url, _, _ := monitorServicesBlockedChain(t)
		entered := make(chan struct{}, 1)
		closed := make(chan string, 2)
		run := f.start(t, url, monitorServiceHooks{afterCheckpointOpen: func(ctx context.Context, opened string, _ *os.File) {
			if opened == role {
				entered <- struct{}{}
				<-ctx.Done()
			}
		}, afterClose: func(closedRole, owner string, file *os.File) error {
			if closedRole == role {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("startup close observer preceded actual close")
				}
				closed <- owner
			}
			return nil
		}})
		select {
		case <-entered:
		case <-run.done:
			t.Fatal("role did not reach actual admission", kind, run.stderr.String())
		case <-t.Context().Done():
			t.Fatal("canceled before admission barrier", kind)
		}
		event := monitorStartupNext(t, run)
		if event.Role == role || event.Publication != "published" || event.State == nil {
			t.Fatal("held admission suppressed healthy initial sample", kind, event)
		}
		run.again(t, event.Role)
		if next := monitorStartupNext(t, run); next.Role != event.Role || next.Publication != "published" {
			t.Fatal("held admission suppressed healthy continuation", kind, next)
		}
		run.cancel()
		<-run.done
		if run.exit != 0 || len(closed) != 2 {
			t.Fatal("canceled admission leaked owners or failed a healthy peer", kind, run.exit, len(closed), run.stderr.String())
		}
		if _, err := os.Stat(checkpoint); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("held admission manufactured a checkpoint", kind, err)
		}
	}
}

// Another real process owner is ordinary pending admission. Releasing its
// exact lock permits the original role without restarting the healthy peer.
func TestMonitorStartupPublicBusyOwnerRecoversWithoutPeerRestart(t *testing.T) {
	f := newMonitorServicesFixture(t, "alpha", "beta")
	path, _ := monitorValidatorPaths(f.checkpointPath, f.metricsPath, "alpha")
	provisionMonitorTestCustody(t, path)
	lock, err := os.OpenFile(path+".lock", os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(errors.Join(err, lock.Close()))
	}
	defer func() {
		if lock != nil {
			if err := lock.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	url, _, _ := monitorServicesBlockedChain(t)
	run := f.start(t, url, monitorServiceHooks{})
	if event := monitorStartupNext(t, run); event.Role != "beta" || event.Publication != "published" {
		t.Fatal("busy alpha stopped beta", event)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	lock = nil
	run.again(t, "alpha")
	if event := monitorStartupNext(t, run); event.Role != "alpha" || event.Publication != "published" {
		t.Fatal("original role did not acquire released custody", event)
	}
	run.again(t, "beta")
	if event := monitorStartupNext(t, run); event.Role != "beta" || event.Publication != "published" {
		t.Fatal("admission retry restarted or blocked the healthy role", event)
	}
	run.cancel()
	<-run.done
	if run.exit != 0 {
		t.Fatal(run.exit, run.stderr.String())
	}
}

// An error after real constructor reads withholds those bytes and closes both
// owners. The same original explicit fresh checkpoint admits after recovery.
func TestMonitorStartupPublicReadFailureRetriesSameCustody(t *testing.T) {
	for _, cause := range []error{syscall.EIO, syscall.EMFILE} {
		f := newMonitorServicesFixture(t, "alpha", "beta")
		url, _, _ := monitorServicesBlockedChain(t)
		var attempts, closes atomic.Int32
		ctx := context.WithValue(t.Context(), monitorStartupObservationKey{}, func(_ context.Context, role string) error {
			if role == "alpha" && attempts.Add(1) == 1 {
				return cause
			}
			return nil
		})
		run := f.startWithContext(t, ctx, url, monitorServiceHooks{afterClose: func(role, _ string, _ *os.File) error {
			if role == "alpha" {
				closes.Add(1)
			}
			return nil
		}})
		if event := monitorStartupNext(t, run); event.Role != "beta" {
			t.Fatal("read failure stopped peer", event)
		}
		run.again(t, "alpha")
		if event := monitorStartupNext(t, run); event.Role != "alpha" || event.Publication != "published" || attempts.Load() != 2 || closes.Load() != 2 {
			t.Fatal("read retry lost original owner or published unadmitted state", cause, event, attempts.Load(), closes.Load())
		}
		run.cancel()
		<-run.done
		if run.exit != 0 || !strings.Contains(run.stderr.String(), cause.Error()) {
			t.Fatal(run.exit, run.stderr.String())
		}
	}
}

// Removing a head after actual descriptor admission is confirmed identity loss.
// The role is terminal even if the original bytes are put back; peers continue.
func TestMonitorStartupPublicIdentityLossQuarantinesOnlyRole(t *testing.T) {
	f := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	path, _ := monitorValidatorPaths(f.checkpointPath, f.metricsPath, "alpha")
	ctx := monitorTestStorageContext(t, t.Context(), f.args(url))
	name := durablehead.Attribute("mainnet-monitor-checkpoint", filepath.Base(path))
	size, err := syscall.Getxattr(path+".lock", name, nil)
	if err != nil {
		t.Fatal(err)
	}
	original := make([]byte, size)
	if _, err := syscall.Getxattr(path+".lock", name, original); err != nil {
		t.Fatal(err)
	}
	terminal := make(chan int, 1)
	var attempts atomic.Int32
	run := f.startWithContext(t, ctx, url, monitorServiceHooks{afterCheckpointOpen: func(_ context.Context, role string, _ *os.File) {
		if role == "alpha" {
			attempts.Add(1)
			if err := syscall.Removexattr(path+".lock", name); err != nil {
				panic(err)
			}
		}
	}, afterWorker: func(role string, exit int) {
		if role == "alpha" {
			terminal <- exit
		}
	}})
	if event := monitorStartupNext(t, run); event.Role != "beta" {
		t.Fatal("lost role stopped peer", event)
	}
	if exit := <-terminal; exit != 3 {
		t.Fatal("missing original head was retried", exit)
	}
	if err := syscall.Setxattr(path+".lock", name, original, 1); err != nil {
		t.Fatal(err)
	}
	run.again(t, "beta")
	if event := monitorStartupNext(t, run); event.Role != "beta" || event.Publication != "published" || attempts.Load() != 1 {
		t.Fatal("quarantined role silently restarted", event, attempts.Load())
	}
	run.cancel()
	<-run.done
	if run.exit != 3 {
		t.Fatal("terminal role outcome was lost", run.exit)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lost custody manufactured state", err)
	}
}

// A failed close after a retryable observation is terminal before another
// acquisition. The healthy role remains owned and publishes a second sample.
func TestMonitorStartupPublicCleanupFailureStopsReacquisition(t *testing.T) {
	f := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	var attempts, closes atomic.Int32
	terminal := make(chan int, 1)
	ctx := context.WithValue(t.Context(), monitorStartupObservationKey{}, func(_ context.Context, role string) error {
		if role == "alpha" {
			attempts.Add(1)
			return syscall.EIO
		}
		return nil
	})
	run := f.startWithContext(t, ctx, url, monitorServiceHooks{afterClose: func(role, _ string, file *os.File) error {
		if role == "alpha" {
			closes.Add(1)
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("close preceded physical release")
			}
			return syscall.EIO
		}
		return nil
	}, afterWorker: func(role string, exit int) {
		if role == "alpha" {
			terminal <- exit
		}
	}})
	if event := monitorStartupNext(t, run); event.Role != "beta" {
		t.Fatal("cleanup failure stopped peer", event)
	}
	if exit := <-terminal; exit != 3 || closes.Load() != 2 {
		t.Fatal("failed release acquired a second owner", exit, closes.Load())
	}
	run.again(t, "beta")
	if event := monitorStartupNext(t, run); event.Role != "beta" || attempts.Load() != 1 {
		t.Fatal("terminal admission reacquired custody", event, attempts.Load())
	}
	run.cancel()
	<-run.done
	if run.exit != 3 || !strings.Contains(run.stderr.String(), "admission cleanup failed") {
		t.Fatal(run.exit, run.stderr.String())
	}
}

// A role can remain in its owned retry wait while a peer is running. Parent
// cancellation joins both and cannot convert an unavailable read into a sample.
func TestMonitorStartupCancellationJoinsPendingAdmission(t *testing.T) {
	f := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	var attempts, closes atomic.Int32
	closed := make(chan struct{}, 1)
	ctx := context.WithValue(t.Context(), monitorStartupObservationKey{}, func(_ context.Context, role string) error {
		if role == "alpha" {
			attempts.Add(1)
			return syscall.EMFILE
		}
		return nil
	})
	run := f.startWithContext(t, ctx, url, monitorServiceHooks{afterClose: func(role, kind string, _ *os.File) error {
		if role == "alpha" {
			closes.Add(1)
			if kind == "checkpoint" {
				closed <- struct{}{}
			}
		}
		return nil
	}})
	if event := monitorStartupNext(t, run); event.Role != "beta" {
		t.Fatal("pending admission stopped healthy role", event)
	}
	select {
	case <-closed:
	case <-run.done:
		t.Fatal("partial admission did not join before waiting", run.exit)
	case <-t.Context().Done():
		t.Fatal("canceled before actual partial owner close")
	}
	run.cancel()
	<-run.done
	if run.exit != 0 || attempts.Load() != 1 || closes.Load() != 2 {
		t.Fatal("cancellation did not join each admitted owner", run.exit, attempts.Load(), closes.Load())
	}
	path, _ := monitorValidatorPaths(f.checkpointPath, f.metricsPath, "alpha")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pending admission published a sample", err)
	}
}

// Returning an owner and an error cannot leak that actual descriptor, execute
// the worker, or hide a cleanup error behind a retryable construction cause.
func TestMonitorStartupConstructorErrorClosesReturnedOwner(t *testing.T) {
	for _, closeFailure := range []bool{false, true} {
		file, err := os.CreateTemp(t.TempDir(), "owned")
		if err != nil {
			t.Fatal(err)
		}
		runs, closes, waits := 0, 0, 0
		exit := runMonitorRoleAdmission(t.Context(), "synthetic", func() (*monitorAdmittedRole, error) {
			return &monitorAdmittedRole{run: func() int { runs++; return 0 }, close: func() error {
				closes++
				err := file.Close()
				if closeFailure {
					err = errors.Join(err, syscall.EIO)
				}
				return err
			}}, syscall.EMFILE
		}, io.Discard, io.Discard, time.Now, monitorServiceHooks{wait: func(context.Context, string, time.Duration) bool { waits++; return false }})
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("constructor owner leaked", err)
		}
		if runs != 0 || closes != 1 || closeFailure && (exit != 3 || waits != 0) || !closeFailure && (exit != 0 || waits != 1) {
			t.Fatal("partial constructor lifecycle escaped", closeFailure, runs, closes, waits, exit)
		}
	}
}

// Joined cleanup/identity causes dominate temporary resource errors in both
// the new startup loop and the existing chain/storage retry callers.
func TestMonitorStartupCleanupAndIdentityDominateUnavailable(t *testing.T) {
	for _, cause := range []error{durablevolume.ErrUnavailable, durablevolume.ErrBusy, syscall.EIO, syscall.EBADF, os.ErrClosed} {
		if !monitorStartupPending(cause) {
			t.Fatal("ordinary observation no longer admits retry", cause)
		}
		for _, refusal := range []error{
			monitorAdmissionFailure(cause, syscall.EIO),
			errors.Join(cause, durablevolume.ErrIdentity),
			errors.Join(cause, errRpcIntegrity),
			errors.Join(cause, durablehead.ErrUncertain),
		} {
			if monitorStartupPending(refusal) {
				t.Fatal("confirmed refusal was retried", refusal)
			}
		}
		if monitorStoragePending(monitorAdmissionFailure(cause, syscall.EIO)) {
			t.Fatal("existing chain retry could reacquire a failed release", cause)
		}
	}
}

// Once both chain and service roles are terminal, no cancellation is required
// to collect their finite results. No unavailable RPC is used as a timer.
func TestMonitorStartupAllTerminalRolesReturnWithoutCancellation(t *testing.T) {
	f := newMonitorServicesFixture(t, "alpha")
	_, roleMetrics := monitorValidatorPaths(f.checkpointPath, f.metricsPath, "alpha")
	for _, path := range []string{f.metricsPath, roleMetrics} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var output, diagnostic bytes.Buffer
	exit := runMonitorStorageTestWithHooks(t, t.Context(), f.args("https://synthetic.invalid"), &monitorFixtureOutput{writer: &output, completed: make(chan struct{}, 1)}, &diagnostic, f.clock.now, monitorServiceHooks{})
	var admission monitorAdmissionEvent
	if err := json.Unmarshal(output.Bytes(), &admission); exit != 3 || err != nil || admission.Role != "alpha" || admission.Status != "quarantined" || admission.CheckpointCurrent || admission.MetricsCurrent {
		t.Fatal("all-terminal supervisor did not retain refusal", exit, output.String(), diagnostic.String())
	}
}

// This receiver observes the real shared exporter; it cannot turn a status
// callback into evidence of delivered output or completed role publication.
type monitorStartupStatusSink struct {
	samples    chan monitorServiceEvent
	admissions chan monitorAdmissionEvent
}

func (self *monitorStartupStatusSink) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}

func (self *monitorStartupStatusSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var head struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return 0, err
	}
	switch head.Schema {
	case "urnetwork-mainnet-role-admission-v1":
		if len(raw) > 1024 {
			return 0, errors.New("admission event exceeded its finite envelope")
		}
		var event monitorAdmissionEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return 0, err
		}
		select {
		case self.admissions <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	case "urnetwork-mainnet-validator-event-v1":
		var event monitorServiceEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return 0, err
		}
		select {
		case self.samples <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return len(raw), nil
}

// A role with no writable metrics destination is still visible in the parent's
// bounded stream. Pending and quarantine never claim fresh state; recovery only
// announces acquisition before an independently completed sample follows.
func TestMonitorStartupPublicAdmissionStatusDoesNotInventFreshMetrics(t *testing.T) {
	for _, kind := range []string{"busy", "identity", "cleanup"} {
		f := newMonitorServicesFixture(t, "alpha", "beta")
		url, _, _ := monitorServicesBlockedChain(t)
		ctx := monitorTestStorageContext(t, t.Context(), f.args(url))
		path, metrics := monitorValidatorPaths(f.checkpointPath, f.metricsPath, "alpha")
		var lock *os.File
		reason, status := "owner-busy", "pending"
		switch kind {
		case "busy":
			var err error
			lock, err = os.OpenFile(path+".lock", os.O_RDWR|syscall.O_NOFOLLOW, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(errors.Join(err, lock.Close()))
			}
		case "identity":
			name := durablehead.Attribute("mainnet-monitor-checkpoint", filepath.Base(path))
			if err := syscall.Removexattr(path+".lock", name); err != nil {
				t.Fatal(err)
			}
			reason, status = "custody-lost", "quarantined"
		case "cleanup":
			ctx = context.WithValue(ctx, monitorStartupObservationKey{}, func(_ context.Context, role string) error {
				if role == "alpha" {
					return syscall.EIO
				}
				return nil
			})
			reason, status = "cleanup-unresolved", "quarantined"
		}
		// A failed assertion still releases the real foreign owner, after the
		// command has joined its independent role goroutines below.
		t.Cleanup(func() {
			if lock != nil {
				if err := lock.Close(); err != nil {
					t.Error(err)
				}
			}
		})
		ctx, cancel := context.WithCancel(ctx)
		run := &monitorServicesTestRun{cancel: cancel, done: make(chan struct{}), events: make(chan monitorServiceEvent, 8), resume: map[string]chan struct{}{"alpha": make(chan struct{}), "beta": make(chan struct{})}}
		sink := &monitorStartupStatusSink{samples: run.events, admissions: make(chan monitorAdmissionEvent, 8)}
		hooks := monitorServiceHooks{wait: func(ctx context.Context, role string, _ time.Duration) bool {
			select {
			case <-run.resume[role]:
				return true
			case <-ctx.Done():
				return false
			}
		}, afterClose: func(role, _ string, file *os.File) error {
			if kind == "cleanup" && role == "alpha" {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("cleanup status preceded actual release")
				}
				return syscall.EIO
			}
			return nil
		}}
		go func() {
			defer close(run.done)
			run.exit = runMonitorStorageTestWithHooks(t, ctx, f.args(url), sink, &run.stderr, f.clock.now, hooks)
		}()
		t.Cleanup(func() { run.cancel(); <-run.done })
		nextStatus := func() monitorAdmissionEvent {
			t.Helper()
			select {
			case event := <-sink.admissions:
				return event
			case <-run.done:
				t.Fatal("startup status ended before delivery", kind, run.exit, run.stderr.String())
			case <-time.After(time.Minute):
				t.Fatal("startup status was not delivered", kind)
			}
			return monitorAdmissionEvent{}
		}
		admission := nextStatus()
		if admission.Role != "alpha" || admission.Status != status || admission.Reason != reason || admission.CheckpointCurrent || admission.MetricsCurrent || admission.ObservedAt != f.clock.now().Format(time.RFC3339Nano) || (kind == "busy") != (admission.RetryAfterSeconds == 1) {
			t.Fatal("startup status confused unowned files with a current sample", kind, admission)
		}
		if event := monitorStartupNext(t, run); event.Role != "beta" || event.Publication != "published" {
			t.Fatal("startup status suppressed peer sample", kind, event)
		}
		for _, absent := range []string{path, metrics} {
			if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unadmitted role manufactured fresh output", kind, absent, err)
			}
		}
		if kind == "busy" {
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			lock = nil
			run.again(t, "alpha")
			admission = nextStatus()
			if admission.Status != "admitted" || admission.Role != "alpha" || admission.Reason != "custody-acquired" || admission.RetryAfterSeconds != 0 || admission.CheckpointCurrent || admission.MetricsCurrent {
				t.Fatal("acquisition invented current sample", admission)
			}
			if event := monitorStartupNext(t, run); event.Role != "alpha" || event.Publication != "published" {
				t.Fatal("recovered custody did not reach actual publication", event)
			}
		}
		run.cancel()
		<-run.done
		expected := 3
		if kind == "busy" {
			expected = 0
		}
		if run.exit != expected {
			t.Fatal("startup status lost role outcome", kind, run.exit, run.stderr.String())
		}
	}
}

// Invalid shared declarations remain outside local role isolation. No source
// read or owned worker is admitted from a conflicting independent role census.
func TestMonitorStartupSharedPolicyFailureStartsNoRole(t *testing.T) {
	f := newMonitorServicesFixture(t, "alpha")
	f.policy.Validators = append(f.policy.Validators, f.policy.Validators[0])
	f.writePolicy(t)
	var admitted atomic.Int32
	var output, diagnostic bytes.Buffer
	exit := runMonitorStorageTestWithHooks(t, t.Context(), f.args("https://synthetic.invalid"), &output, &diagnostic, f.clock.now, monitorServiceHooks{afterCheckpointOpen: func(context.Context, string, *os.File) { admitted.Add(1) }})
	if exit != 2 || admitted.Load() != 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "unique") {
		t.Fatal("shared invalid policy started local owners", exit, admitted.Load(), diagnostic.String())
	}
}
