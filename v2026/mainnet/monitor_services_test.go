// Actual command, file and metric tests use explicit read/write/wait barriers.
// Synthetic records carry no protocol signing authority or live identifiers.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Clock changes synchronize with all concurrently running command domains.
type monitorServicesTestClock struct{ seconds atomic.Int64 }

// No production deadline is accelerated; only the observation clock is supplied.
func (self *monitorServicesTestClock) now() time.Time { return time.Unix(self.seconds.Load(), 0).UTC() }

// Every declared source and role is independently supplied to the real command.
type monitorServicesFixture struct {
	directory      string
	policyPath     string
	checkpointPath string
	metricsPath    string
	policy         monitorServicesPolicy
	clock          *monitorServicesTestClock
}

// A complete current record can wait a long time for its original reveal block.
func monitorServicesTestRecord(now time.Time, id uint64) protocol.ValidatorProgress {
	stamp := now.Format(time.RFC3339Nano)
	return protocol.ValidatorProgress{Schema: protocol.ValidatorProgressSchema,
		Source:     protocol.ValidatorProgressSource{ConfigHash: "sha256:" + strings.Repeat("1", 64), DeploymentId: "synthetic-observer", ValidatorId: id, ChainId: 964, GenesisHash: testGenesisHash, Netuid: 25},
		InstanceId: strings.Repeat("2", 32), StartedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), HeartbeatAt: stamp,
		Publisher:  protocol.ValidatorPublicationObservation{Outcome: "published", LastSuccessAt: stamp},
		Intent:     &protocol.ValidatorIntentObservation{ObservedAt: stamp, LastSuccessAt: stamp, Current: true},
		Native:     &protocol.ValidatorNativeObservation{ObservedAt: stamp, LastSuccessAt: stamp, Current: true, Block: 100, Epoch: 8, Tempo: 360},
		Settlement: &protocol.ValidatorSettlementObservation{ObservedAt: stamp, LastSuccessAt: stamp, Current: true, CursorKnown: true, Epoch: 7, TargetEpoch: 7, ProgressAt: now.Add(-time.Hour).Format(time.RFC3339Nano)},
	}
}

// Source publication uses the real complete atomic replacement primitive.
func monitorServicesTestWrite(t testing.TB, path string, value protocol.ValidatorProgress) {
	t.Helper()
	raw, err := value.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := publishMonitorFile(path, raw, 0644, nil); err != nil {
		t.Fatal(err)
	}
}

// One preprovisioned directory holds independently named source and output files.
func newMonitorServicesFixture(t *testing.T, roles ...string) *monitorServicesFixture {
	t.Helper()
	directory := monitorMetricsTestDir(t)
	clock := &monitorServicesTestClock{}
	clock.seconds.Store(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Unix())
	self := &monitorServicesFixture{directory: directory, policyPath: filepath.Join(directory, "services.json"), checkpointPath: filepath.Join(directory, "monitor.json"), metricsPath: filepath.Join(directory, "monitor.prom"), clock: clock, policy: monitorServicesPolicy{Schema: monitorServicesSchema}}
	for index, role := range roles {
		value := monitorServicesTestRecord(clock.now(), uint64(index+1))
		validator := monitorValidatorPolicy{Role: role, ProgressFile: filepath.Join(directory, role+".json"), ExpectedSource: value.Source}
		self.policy.Validators = append(self.policy.Validators, validator)
		monitorServicesTestWrite(t, validator.ProgressFile, value)
	}
	self.writePolicy(t)
	return self
}

// Changing expected configuration is independent of candidate file content.
func (self *monitorServicesFixture) writePolicy(t testing.TB) {
	t.Helper()
	raw, err := json.Marshal(self.policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(self.policyPath, raw, 0644); err != nil {
		t.Fatal(err)
	}
}

// The actual public flags select the new composition and original chain path.
func (self *monitorServicesFixture) args(url string) []string {
	return []string{"monitor", "--rpc", url, "--expected-chain", "fixture-mainnet", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--services", self.policyPath, "--checkpoint", self.checkpointPath, "--metrics-file", self.metricsPath, "--retry-window", "300s", "--interval", "1h", "--stall-after", "24h"}
}

// JSON publication is a positive barrier after checkpoint and metrics writes.
type monitorServicesTestWriter struct{ events chan monitorServiceEvent }

// Discard only chain-schema events; retain every actual service event.
func (self *monitorServicesTestWriter) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}

// Cancellation reaches the actual event receiver, with no abandoned writer.
func (self *monitorServicesTestWriter) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var event monitorServiceEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	if event.Schema == "urnetwork-mainnet-validator-event-v1" {
		select {
		case self.events <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return len(raw), nil
}

// Tests own every resume and observe the real joined terminal result.
type monitorServicesTestRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	events chan monitorServiceEvent
	resume map[string]chan struct{}
	exit   int
	stderr bytes.Buffer
}

// Only waiting is controlled. Real command admission, reads and publication run.
func (self *monitorServicesFixture) start(t *testing.T, url string, hooks monitorServiceHooks) *monitorServicesTestRun {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	run := &monitorServicesTestRun{cancel: cancel, done: make(chan struct{}), events: make(chan monitorServiceEvent, 32), resume: map[string]chan struct{}{}}
	for _, role := range self.policy.Validators {
		run.resume[role.Role] = make(chan struct{})
	}
	hooks.wait = func(ctx context.Context, role string, _ time.Duration) bool {
		if resume := run.resume[role]; resume != nil {
			select {
			case <-resume:
				return true
			case <-ctx.Done():
				return false
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Hour):
			return true
		}
	}
	go func() {
		defer close(run.done)
		run.exit = runMainWithMonitorHooks(ctx, self.args(url), &monitorServicesTestWriter{events: run.events}, &run.stderr, self.clock.now, hooks)
	}()
	t.Cleanup(func() { run.cancel(); <-run.done })
	return run
}

// Cancellation is explicit; no timeout is used as evidence that a peer stopped.
func (self *monitorServicesTestRun) next(t testing.TB) monitorServiceEvent {
	t.Helper()
	select {
	case event := <-self.events:
		return event
	case <-self.done:
		select {
		case event := <-self.events:
			return event
		default:
			t.Fatalf("command stopped before service sample: exit=%d stderr=%s", self.exit, self.stderr.String())
		}
	case <-t.Context().Done():
		t.Fatal("test canceled before sample")
	}
	return monitorServiceEvent{}
}

// Advance one role without releasing any peer's independent wait.
func (self *monitorServicesTestRun) again(t testing.TB, role string) {
	t.Helper()
	select {
	case self.resume[role] <- struct{}{}:
	case <-self.done:
		t.Fatalf("command ended before next sample: %s", self.stderr.String())
	case <-t.Context().Done():
		t.Fatal("test canceled")
	}
}

// A deliberately blocked local chain request exits only on actual cancellation.
func monitorServicesBlockedChain(t *testing.T) (string, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	entered, left := make(chan struct{}), make(chan struct{})
	var once, leftOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// HTTP/1 starts disconnect detection after request-body EOF. Consume
		// the real bounded POST before advertising an interruptible read.
		raw, readErr := io.ReadAll(io.LimitReader(request.Body, 8*1024+1))
		if err := errors.Join(readErr, request.Body.Close()); err != nil {
			if request.Context().Err() == nil {
				t.Errorf("blocked RPC fixture request read: %v", err)
			}
			return
		}
		if len(raw) > 8*1024 || !json.Valid(raw) {
			t.Error("blocked RPC fixture requires a bounded complete JSON request")
			http.Error(writer, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		once.Do(func() { close(entered) })
		<-request.Context().Done()
		leftOnce.Do(func() { close(left) })
	}))
	t.Cleanup(server.Close)
	return server.URL, entered, left
}

// Parse the collector's actual labeled exposition independently of rendering.
func monitorServicesGauges(t testing.TB, path, role string) map[string]float64 {
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
		if len(fields) == 2 && (strings.HasPrefix(fields[0], "sn_mainnet_validator_output_") || strings.HasPrefix(fields[0], "sn_mainnet_validator_producer_diagnostic_")) {
			continue // Closed stream/domain families are checked independently.
		}
		if len(fields) != 2 || !strings.HasSuffix(fields[0], "{role="+strconv.Quote(role)+"}") || !strings.HasPrefix(fields[0], "sn_mainnet_validator_") {
			t.Fatalf("invalid role metric %q", line)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(fields[0], "sn_mainnet_validator_"), "{role="+strconv.Quote(role)+"}")
		if _, ok := values[name]; ok {
			t.Fatal("duplicate role gauge", name)
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		values[name] = value
	}
	if len(raw) > 32*1024 || len(values) != 74 {
		t.Fatalf("unbounded or incomplete gauge census: %d bytes %d metrics", len(raw), len(values))
	}
	return values
}

// A chain retry may use its full 300 seconds without starving either role.
// A missing role remains isolated while the other exports fresh observations.
func TestMonitorServicesCommandSeparatesBlockedChainAndRoleOutage(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, entered, left := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	seen := map[string]monitorServiceEvent{}
	for len(seen) < 2 {
		event := run.next(t)
		seen[event.Role] = event
	}
	<-entered
	if seen["alpha"].Status != "missing" || seen["beta"].Status != "observed" {
		t.Fatal("domains were not independently observed", seen)
	}
	_, betaPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "beta")
	beta := monitorServicesGauges(t, betaPath, "beta")
	if beta["source_current"] != 1 || beta["intent_known_empty"] != 1 || beta["protocol_deadline_known"] != 0 {
		t.Fatal("known empty observation lost its boundary", beta)
	}
	chain := readMonitorTestGauges(t, fixture.metricsPath)
	if chain["sn_mainnet_monitor_sample_timestamp_seconds"] != 0 {
		t.Fatal("service sample fabricated chain completion")
	}
	fixture.clock.seconds.Add(6 * 60)
	run.again(t, "alpha")
	if event := run.next(t); event.Role != "alpha" || event.Severity != "critical" {
		t.Fatal("isolated initial outage failed to age", event)
	}
	run.cancel()
	<-run.done
	<-left
	if run.exit != 0 {
		t.Fatal("cancellation did not join all command workers", run.exit)
	}
}

// A fresh first later failure does not inherit hours of healthy uptime. Each
// new outage has its own boundary; an unresolved restart keeps the prior one.
func TestMonitorServicesCommandOutageAgeRecoveryAndRestart(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := run.next(t)
	fixture.clock.seconds.Add(3 * 3600)
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	run.again(t, "alpha")
	failed := run.next(t)
	if failed.Severity != "" || !failed.State.OutageSince.Equal(fixture.clock.now()) || !failed.State.LastReadSuccessAt.Equal(first.State.LastReadSuccessAt) {
		t.Fatal("new outage inherited healthy process age", failed)
	}
	fixture.clock.seconds.Add(120)
	run.again(t, "alpha")
	if event := run.next(t); event.Severity != "warning" {
		t.Fatal("known outage missed warning boundary", event)
	}
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 1))
	run.again(t, "alpha")
	recovered := run.next(t)
	if recovered.Status != "observed" || !recovered.State.OutageSince.IsZero() {
		t.Fatal("real recovery retained an ended outage", recovered)
	}
	fixture.clock.seconds.Add(3600)
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	run.again(t, "alpha")
	second := run.next(t)
	if second.Severity != "" || !second.State.OutageSince.Equal(fixture.clock.now()) {
		t.Fatal("second outage reused the first boundary", second)
	}
	run.cancel()
	<-run.done
	fixture.clock.seconds.Add(301)
	restarted := fixture.start(t, url, monitorServiceHooks{})
	retained := restarted.next(t)
	if retained.Severity != "critical" || !retained.State.OutageSince.Equal(second.State.OutageSince) || retained.State.Record == nil {
		t.Fatal("restart reset unresolved outage or last-good evidence", retained)
	}
}

// Restart renewal reads only independently expected current config, while an
// original pending intent and its unchanged age survive a subsequent outage.
func TestMonitorServicesCommandRenewalRetainsOriginalIntentThroughOutage(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	value := monitorServicesTestRecord(fixture.clock.now(), 1)
	created := fixture.clock.now().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	value.Intent.Value = &protocol.ValidatorIntentProgress{ConfigHash: value.Source.ConfigHash, VectorHash: "0x" + strings.Repeat("3", 64), Status: "pending", CreatedAt: created, ProgressAt: created, NativeEpoch: 8, SettlementEpoch: 7, PreparedAtBlock: 100, RevealBlock: 2000}
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	if event := run.next(t); event.Status != "observed" {
		t.Fatal(event)
	}
	run.cancel()
	<-run.done
	value.Source.ConfigHash = "sha256:" + strings.Repeat("4", 64)
	value.InstanceId = strings.Repeat("5", 32)
	fixture.policy.Validators[0].ExpectedSource = value.Source
	fixture.writePolicy(t)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	restarted := fixture.start(t, url, monitorServiceHooks{})
	renewed := restarted.next(t)
	if renewed.Status != "observed" || renewed.State.Record.Intent.Value.ConfigHash == value.Source.ConfigHash {
		t.Fatal("renewal reinterpreted original intent", renewed)
	}
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	fixture.clock.seconds.Add(60)
	restarted.again(t, "alpha")
	missing := restarted.next(t)
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	metrics := monitorServicesGauges(t, path, "alpha")
	if missing.Status != "missing" || missing.State.Record.Intent.Value.CreatedAt != created || metrics["intent_original_config"] != 1 || metrics["intent_current"] != 0 || metrics["intent_known_empty"] != 0 || metrics["intent_present"] != 1 {
		t.Fatal("outage erased pending intent identity or age", missing, metrics)
	}
}

// Slow legitimate native progress is not a wall-clock protocol deadline. A
// stale producer heartbeat, however, is observable independently of its epoch.
func TestMonitorServicesCommandStaleHeartbeatAndUnknownSchedule(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	value := monitorServicesTestRecord(fixture.clock.now(), 1)
	created := fixture.clock.now().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	value.Intent.Value = &protocol.ValidatorIntentProgress{ConfigHash: value.Source.ConfigHash, VectorHash: "0x" + strings.Repeat("3", 64), Status: "pending", CreatedAt: created, ProgressAt: created, RevealBlock: 2000}
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run := fixture.start(t, url, monitorServiceHooks{})
	if event := run.next(t); event.Status != "observed" || event.Severity != "" {
		t.Fatal("long legitimate wait became a deadline alarm", event)
	}
	fixture.clock.seconds.Add(121)
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "stale" || event.Severity != "critical" {
		t.Fatal("stale heartbeat became a healthy wait", event)
	}
	value = monitorServicesTestRecord(fixture.clock.now(), 1)
	value.Native = nil
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "unknown" {
		t.Fatal("absent schedule fabricated complete observation", event)
	}
}

// Future evidence is refused, then retained as a clock incident through missing
// input. Only a new valid source observation resolves the clock condition.
func TestMonitorServicesCommandFutureClockAndRollbackRemainVisible(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := run.next(t)
	value := monitorServicesTestRecord(fixture.clock.now().Add(time.Hour), 1)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run.again(t, "alpha")
	future := run.next(t)
	if future.Status != "clock" || future.Severity != "critical" || !future.State.LastReadSuccessAt.Equal(first.State.LastReadSuccessAt) {
		t.Fatal("future source refreshed accepted evidence", future)
	}
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "clock" {
		t.Fatal("missing source cleared a clock incident", event)
	}
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 1))
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "observed" {
		t.Fatal("valid source could not recover", event)
	}
	run.cancel()
	<-run.done
	fixture.clock.seconds.Add(-60)
	restarted := fixture.start(t, url, monitorServiceHooks{})
	if event := restarted.next(t); event.Status != "clock" {
		t.Fatal("restart erased consumer clock high water", event)
	}
}

// A genuine terminal output ownership fault cancels and joins a pending chain
// read, instead of waiting for its whole retry window or detaching that worker.
func TestMonitorServicesCommandOwnershipFailureCancelsAndJoinsPeer(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, entered, left := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	run.next(t)
	<-entered
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
		t.Fatal(err)
	}
	run.again(t, "alpha")
	event := run.next(t)
	<-run.done
	<-left
	if run.exit != 3 || event.Publication != "ownership-error" {
		t.Fatal("ownership failure did not stop and join the composition", run.exit, event)
	}
}

// Actual post-sync failure leaves complete bytes visible but cannot advance
// acknowledged exporter success. Repeated failures remain explicit in metrics.
func TestMonitorServicesCommandAmbiguousPublicationRetriesAndRecovers(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	attempt := 0
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if kind == "metrics" {
			attempt++
			if attempt == 3 || attempt == 4 {
				return errors.Join(err, errors.New("synthetic post-sync failure"))
			}
		}
		return err
	}}
	run := fixture.start(t, url, hooks)
	run.next(t)
	fixture.clock.seconds.Add(1)
	run.again(t, "alpha")
	second := run.next(t)
	for range 2 {
		fixture.clock.seconds.Add(1)
		run.again(t, "alpha")
		if event := run.next(t); event.Publication != "retrying" {
			t.Fatal("ambiguous write reported success", event)
		}
	}
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	gauges := monitorServicesGauges(t, path, "alpha")
	if gauges["export_status"] != 2 || gauges["export_last_success_timestamp_seconds"] != float64(second.State.PublicationLastSuccessAt.Unix()) {
		t.Fatal("ambiguous file advanced confirmed export", gauges)
	}
	run.again(t, "alpha")
	if event := run.next(t); event.Publication != "published" {
		t.Fatal("repaired output failed to recover", event)
	}
}

// Rejected policy diagnostics retain their wrapped filesystem cause; public
// service status and metric labels still contain only the closed classification.
func TestMonitorServicesPolicyRetainsReadCauseAndRejectsDuplicateRoles(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	expected := monitorTestExpectation()
	if _, err := loadMonitorServices(t.Context(), filepath.Join(fixture.directory, "missing.json"), expected, fixture.checkpointPath, fixture.metricsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("policy diagnostic lost missing file cause", err)
	}
	fixture.policy.Validators = append(fixture.policy.Validators, fixture.policy.Validators[0])
	fixture.writePolicy(t)
	if _, err := loadMonitorServices(t.Context(), fixture.policyPath, expected, fixture.checkpointPath, fixture.metricsPath); err == nil {
		t.Fatal("colliding role family admitted")
	}
}

// Keep import-time test helpers simple when their closures are composed below.
var _ io.Writer = (*monitorServicesTestWriter)(nil)
