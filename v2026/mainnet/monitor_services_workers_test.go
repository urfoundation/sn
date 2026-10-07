// Each bounded domain owns its work through cancellation and actual cleanup.
// Positive barriers show a blocked or failing peer cannot suppress observations.
package main

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

// A source descriptor remains owned while an unrelated role completes samples.
// Cancellation joins that read after its actual file operation is released.
func TestMonitorServicesCommandBlockedSourcePreservesPeer(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	hooks := monitorServiceHooks{read: func(role string) monitorServiceReadHooks {
		if role != "alpha" {
			return monitorServiceReadHooks{}
		}
		return monitorServiceReadHooks{afterRead: func(*os.File) error {
			close(entered)
			<-release
			return nil
		}}
	}}
	run := fixture.start(t, url, hooks)
	defer releaseOnce.Do(func() { close(release) })
	<-entered
	if event := run.next(t); event.Role != "beta" || event.Status != "observed" {
		t.Fatal("blocked source suppressed its independent peer", event)
	}
	fixture.clock.seconds.Add(1)
	monitorServicesTestWrite(t, fixture.policy.Validators[1].ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 2))
	run.again(t, "beta")
	if event := run.next(t); event.Role != "beta" || event.Status != "observed" {
		t.Fatal("blocked source suppressed the next peer observation", event)
	}
	run.cancel()
	releaseOnce.Do(func() { close(release) })
	<-run.done
	if run.exit != 0 {
		t.Fatal("owned source did not finish cancellation", run.stderr.String())
	}
	_, alphaPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	if _, err := os.Stat(alphaPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled unfinished source manufactured a sample", err)
	}
}

// Even repeated real ambiguous writes remain local to their role. The other
// publisher keeps its acknowledged success and bounded role labels.
func TestMonitorServicesCommandExporterFailurePreservesPeer(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == "alpha" && kind == "metrics" {
			return errors.Join(err, errors.New("synthetic role-local durability failure"))
		}
		return err
	}}
	run := fixture.start(t, url, hooks)
	seen := map[string]monitorServiceEvent{}
	for len(seen) < 2 {
		event := run.next(t)
		seen[event.Role] = event
	}
	if seen["alpha"].Publication != "retrying" || seen["beta"].Publication != "published" {
		t.Fatal("one failed exporter disabled another", seen)
	}
	fixture.clock.seconds.Add(1)
	run.again(t, "alpha")
	if event := run.next(t); event.Publication != "retrying" {
		t.Fatal("failed exporter did not retry", event)
	}
	run.again(t, "beta")
	if event := run.next(t); event.Role != "beta" || event.Publication != "published" {
		t.Fatal("exporter retry suppressed the healthy peer", event)
	}
	_, alphaPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	_, betaPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "beta")
	alpha, beta := monitorServicesGauges(t, alphaPath, "alpha"), monitorServicesGauges(t, betaPath, "beta")
	if alpha["export_status"] != 2 || alpha["export_last_success_timestamp_seconds"] != 0 || beta["export_status"] != 1 || beta["export_last_success_timestamp_seconds"] == 0 {
		t.Fatal("independent publication acknowledgments were collapsed", alpha, beta)
	}
}

// Both real closes happen even when the first reports a late error. The command
// joins its peers and returns a diagnostic failure rather than silent success.
func TestMonitorServicesCommandRetainsCleanupFailures(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, entered, left := monitorServicesBlockedChain(t)
	closed := map[string]bool{}
	var closedLock sync.Mutex
	hooks := monitorServiceHooks{afterClose: func(role, kind string, file *os.File) error {
		_, err := file.Stat()
		if !errors.Is(err, os.ErrClosed) {
			return errors.New("cleanup observer ran before physical close")
		}
		closedLock.Lock()
		closed[role+"/"+kind] = true
		closedLock.Unlock()
		return errors.New("synthetic " + kind + " close failure")
	}}
	run := fixture.start(t, url, hooks)
	run.next(t)
	<-entered
	run.cancel()
	<-run.done
	<-left
	if run.exit != 3 || !closed["alpha/metrics"] || !closed["alpha/checkpoint"] ||
		!strings.Contains(run.stderr.String(), "synthetic metrics close failure") || !strings.Contains(run.stderr.String(), "synthetic checkpoint close failure") {
		t.Fatal("cleanup omitted a close or its error", run.exit, closed, run.stderr.String())
	}
}

// Admission can own a checkpoint before the metric destination is refused.
// Partial admission still closes that exact owner and reports a late fault.
func TestMonitorServicesCommandRetainsAdmissionCleanupFailure(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	closed := false
	terminal := make(chan int, 1)
	hooks := monitorServiceHooks{afterClose: func(role, kind string, file *os.File) error {
		_, err := file.Stat()
		closed = role == "alpha" && kind == "checkpoint" && errors.Is(err, os.ErrClosed)
		return errors.New("synthetic partial admission close failure")
	}, afterWorker: func(role string, exit int) {
		if role == "alpha" {
			terminal <- exit
		}
	}}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, hooks)
	if exit := <-terminal; exit != 3 {
		t.Fatal("partial owner did not stop its role", exit)
	}
	run.cancel()
	<-run.done
	if run.exit != 3 || !closed || !strings.Contains(run.stderr.String(), "synthetic partial admission close failure") {
		t.Fatal("partial admission discarded cleanup failure", run.exit, run.stderr.String())
	}
}
