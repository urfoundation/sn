// Command output regressions retain real source files, checkpoints, metrics and
// joined local RPC reads while deliberately making log delivery unavailable.
package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// One destination accepts the explicit cancellation contract but never drains.
type monitorBlockedOutput struct {
	entered, left  chan struct{}
	once, leftOnce sync.Once
}

// A synchronous call is a regression, not an available fallback.
func (self *monitorBlockedOutput) Write([]byte) (int, error) { panic("unbounded monitor log path") }

// Completion records the actual interrupted operation, never a detached worker.
func (self *monitorBlockedOutput) WriteContext(ctx context.Context, _ []byte) (int, error) {
	self.once.Do(func() { close(self.entered) })
	<-ctx.Done()
	self.leftOnce.Do(func() { close(self.left) })
	return 0, ctx.Err()
}

// The fixed metric key is independent of production rendering and preserves
// every label so different roles/domains cannot collapse into a single value.
func monitorOutputTestMetrics(t testing.TB, path string) map[string]float64 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]float64{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatal("invalid exposition", line)
		}
		if _, exists := values[fields[0]]; exists {
			t.Fatal("duplicate metric series", fields[0])
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		values[fields[0]] = value
	}
	return values
}

// Both source roles keep advancing while their common log sink and chain read
// are blocked. The public command still joins all real operations on cancel.
func TestMonitorOutputBlockedSinkPreservesRoleFilesAndShutdown(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	url, chainEntered, chainLeft := monitorServicesBlockedChain(t)
	sink := &monitorBlockedOutput{entered: make(chan struct{}), left: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sampled := make(chan string, 8)
	resume := map[string]chan struct{}{"alpha": make(chan struct{}), "beta": make(chan struct{})}
	hooks := monitorServiceHooks{wait: func(ctx context.Context, role string, _ time.Duration) bool {
		select {
		case sampled <- role:
		case <-ctx.Done():
			return false
		}
		select {
		case <-resume[role]:
			return true
		case <-ctx.Done():
			return false
		}
	}}
	done := make(chan int, 1)
	go func() {
		done <- runMonitorStorageTestWithHooks(t, ctx, fixture.args(url), sink, sink, fixture.clock.now, hooks)
	}()
	seen := map[string]bool{}
	for len(seen) < 2 {
		seen[<-sampled] = true
	}
	<-sink.entered
	<-chainEntered
	// More than the independent queue allowance forces drops for alpha.
	for count := 0; count < 5; count++ {
		resume["alpha"] <- struct{}{}
		if <-sampled != "alpha" {
			t.Fatal("unexpected role advanced")
		}
	}
	fixture.clock.seconds.Add(10)
	monitorServicesTestWrite(t, fixture.policy.Validators[1].ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 2))
	resume["beta"] <- struct{}{}
	if <-sampled != "beta" {
		t.Fatal("healthy role failed to advance")
	}
	_, alphaPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	_, betaPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "beta")
	alpha, beta := monitorOutputTestMetrics(t, alphaPath), monitorOutputTestMetrics(t, betaPath)
	if alpha[`sn_mainnet_validator_output_dropped_total{role="alpha",stream="events"}`] == 0 || alpha[`sn_mainnet_validator_output_delivered_total{role="alpha",stream="events"}`] != 0 {
		t.Fatal("blocked delivery was not counted separately", alpha)
	}
	if beta[`sn_mainnet_validator_sample_timestamp_seconds{role="beta"}`] != float64(fixture.clock.now().Unix()) {
		t.Fatal("log congestion starved independent role file", beta)
	}
	cancel()
	if exit := <-done; exit != 0 {
		t.Fatal("diagnostic outage changed core command result", exit)
	}
	<-sink.left
	<-chainLeft
}

// An early file admission failure closes the real exporter after cleanup even
// when the final diagnostic itself cannot be delivered to its destination.
func TestMonitorOutputEarlyAdmissionStillJoinsBlockedSink(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	sink := &monitorBlockedOutput{entered: make(chan struct{}), left: make(chan struct{})}
	closed := false
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hooks := monitorServiceHooks{afterClose: func(role, kind string, file *os.File) error {
		if role == "alpha" && kind == "checkpoint" {
			_, err := file.Stat()
			closed = errors.Is(err, os.ErrClosed)
		}
		return nil
	}, afterWorker: func(role string, exit int) {
		if role == "alpha" && exit == 3 {
			// Cleanup precedes this role's admission diagnostic. Its actual
			// blocked export is joined by explicit parent cancellation.
			<-sink.entered
			cancel()
		}
	}}
	if exit := runMonitorStorageTestWithHooks(t, ctx, fixture.args("http://rpc.example"), sink, sink, fixture.clock.now, hooks); exit != 3 || !closed {
		t.Fatal("early failure leaked admitted owners", exit, closed)
	}
	<-sink.entered
	<-sink.left
}

// Fixed producer domains have complete, independently parseable wire values.
func monitorDiagnosticTestObservation(now time.Time, count uint64) *protocol.ValidatorDiagnosticObservation {
	state := protocol.ValidatorDiagnosticState{Outcome: "starting"}
	if count != 0 {
		state.Outcome, state.Delivered, state.LastSuccessAt = "delivered", count, now.Format(time.RFC3339Nano)
	}
	return &protocol.ValidatorDiagnosticObservation{ObservedAt: now.Format(time.RFC3339Nano), Startup: state, Steering: state, Progress: state, Operator: state, Runtime: state}
}

// Optional old records stay unknown; same-instance counter rewinds are refused
// and an independently identified new instance can begin fresh counters.
func TestMonitorOutputProducerCountersRespectInstanceAndRetainedState(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	run.next(t)
	_, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	key := `sn_mainnet_validator_producer_diagnostic_known{role="alpha",domain="steering"}`
	if monitorOutputTestMetrics(t, metricsPath)[key] != 0 {
		t.Fatal("old producer became known output")
	}
	value := monitorServicesTestRecord(fixture.clock.now(), 1)
	value.Diagnostics = monitorDiagnosticTestObservation(fixture.clock.now(), 9)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run.again(t, "alpha")
	prior := run.next(t)
	if prior.State.Record.Diagnostics.Steering.Delivered != 9 {
		t.Fatal("real file diagnostics missing")
	}
	value.Diagnostics.Steering.Delivered = 8
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "invalid" || event.State.Record.Diagnostics.Steering.Delivered != 9 {
		t.Fatal("same-instance counter rewind accepted", event)
	}
	value.InstanceId = strings.Repeat("4", 32)
	value.Diagnostics = monitorDiagnosticTestObservation(fixture.clock.now(), 0)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "observed" || event.State.Record.Diagnostics.Steering.Delivered != 0 {
		t.Fatal("new producer counters could not restart", event)
	}
	if prior.State.Record.Diagnostics.Steering.Delivered != 9 {
		t.Fatal("retained event changed after later sample")
	}
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	run.again(t, "alpha")
	run.next(t)
	metrics := monitorOutputTestMetrics(t, metricsPath)
	if metrics[key] != 1 || metrics[`sn_mainnet_validator_producer_diagnostic_current{role="alpha",domain="steering"}`] != 0 {
		t.Fatal("retained diagnostics became current")
	}
}

// A fresh protocol heartbeat cannot conceal a future output acknowledgment or
// a stale independent diagnostic observation; neither changes native progress.
func TestMonitorOutputProducerClockAndFreshnessStayIndependent(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	value := monitorServicesTestRecord(fixture.clock.now(), 1)
	value.Diagnostics = monitorDiagnosticTestObservation(fixture.clock.now().Add(-time.Hour), 3)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	if event := run.next(t); event.Status != "observed" {
		t.Fatal("old logs changed protocol observation", event)
	}
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	if monitorOutputTestMetrics(t, path)[`sn_mainnet_validator_producer_diagnostic_current{role="alpha",domain="startup"}`] != 0 {
		t.Fatal("stale output refreshed from heartbeat")
	}
	value.Diagnostics = monitorDiagnosticTestObservation(fixture.clock.now().Add(time.Hour), 4)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "clock" || event.State.Record.Diagnostics.Steering.Delivered != 3 {
		t.Fatal("future acknowledgment laundered into health", event)
	}
}
