// Command-level publication tests use local read fixtures and output barriers;
// no timing race, production endpoint or external alert destination is used.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The event is published after metrics, allowing deterministic readback and
// route/clock changes before the next command sample starts.
type monitorMetricsTestWriter struct {
	bytes.Buffer
	onEvent    func(monitorEvent) error
	eventError error
}

// The collector directory is preprovisioned with read-only group access.
func monitorMetricsTestDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0750); err != nil {
		t.Fatal(err)
	}
	return directory
}

// Synchronous output is the barrier; cancellation never races the assertion.
func (self *monitorMetricsTestWriter) Write(raw []byte) (int, error) {
	var event monitorEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	n, err := self.Buffer.Write(raw)
	if err == nil {
		err = self.onEvent(event)
		self.eventError = err
	}
	return n, err
}

// Parses the exact unlabeled textfile contract without sharing the renderer.
func readMonitorTestGauges(t *testing.T, path string) map[string]float64 {
	t.Helper()
	values, err := readMonitorMetricsTestGauges(path)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

// Worker callbacks return evidence failures to the joined command owner. A
// testing.Goexit inside a worker cannot strand the command's completion signal.
func readMonitorMetricsTestGauges(path string) (map[string]float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]float64{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.HasPrefix(fields[0], "sn_mainnet_monitor_output_") {
			continue // The bounded stream census has independent assertions.
		}
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "sn_mainnet_monitor_") || strings.ContainsAny(fields[0], "{}\"") {
			return nil, fmt.Errorf("invalid or labeled gauge: %q", line)
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, err
		}
		if _, exists := values[fields[0]]; exists {
			return nil, fmt.Errorf("duplicate gauge %s", fields[0])
		}
		values[fields[0]] = value
	}
	if len(values) != 14 || len(raw) > 8*1024 || !bytes.HasSuffix(raw, []byte("\n")) {
		return nil, fmt.Errorf("unexpected publication shape: %d gauges, %d bytes", len(values), len(raw))
	}
	return values, nil
}

// Fresh failure samples keep the last successful read unchanged. Recovery
// clears outage state only after the real identity and continuity reads succeed.
func TestMonitorMetricsCommandRetainsSuccessThroughOutageAndRecovery(t *testing.T) {
	upstream, _ := testRpcServerWithIdentity(t, "fixture-mainnet", "0x3c4", "")
	upstreamUrl, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstreamUrl)
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if failing.Load() {
			http.Error(writer, "synthetic missing read", http.StatusBadRequest)
			return
		}
		proxy.ServeHTTP(writer, request)
	}))
	defer server.Close()
	directory := monitorMetricsTestDir(t)
	path, checkpoint := filepath.Join(directory, "monitor.prom"), filepath.Join(directory, "checkpoint.json")
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sampleTime := base
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := 0
	stdout := &monitorMetricsTestWriter{onEvent: func(event monitorEvent) error {
		gauges, err := readMonitorMetricsTestGauges(path)
		if err != nil {
			cancel()
			return err
		}
		for _, name := range []string{"sn_mainnet_monitor_rpc_comparison_status", "sn_mainnet_monitor_independent_rpc", "sn_mainnet_monitor_rpc_comparison_sample_timestamp_seconds"} {
			if value, present := gauges[name]; !present || value != 0 {
				cancel()
				return fmt.Errorf("absent comparison authority gained a metric: %s=%v present=%v", name, value, present)
			}
		}
		if event.RpcComparison == nil || event.RpcComparison.Status != "input-unknown" || event.RpcComparison.IndependentRpc || event.RpcComparison.ObservedAt != "" {
			cancel()
			return fmt.Errorf("absent comparison authority became a completed comparison: %+v", event.RpcComparison)
		}
		if gauges["sn_mainnet_monitor_sample_timestamp_seconds"] != float64(sampleTime.Unix()) {
			cancel()
			return errors.New("sample metric did not precede its event")
		}
		switch events {
		case 0:
			if event.Status != "ok" || gauges["sn_mainnet_monitor_healthy"] != 1 || gauges["sn_mainnet_monitor_has_finalized_evidence"] != 1 {
				cancel()
				return fmt.Errorf("first checked read not published: %+v %v", event, gauges)
			}
			failing.Store(true)
			sampleTime = base.Add(time.Minute)
		case 1, 2:
			if event.Status != "rpc-error" || gauges["sn_mainnet_monitor_healthy"] != 0 || gauges["sn_mainnet_monitor_last_success_timestamp_seconds"] != float64(base.Unix()) || gauges["sn_mainnet_monitor_read_outage_started_timestamp_seconds"] != float64(base.Add(time.Minute).Unix()) {
				cancel()
				return fmt.Errorf("failure became healthy or reset history: %+v %v", event, gauges)
			}
			if events == 1 {
				sampleTime = base.Add(7 * time.Minute)
			} else {
				if event.Severity != "critical" || gauges["sn_mainnet_monitor_severity"] != 2 || gauges["sn_mainnet_monitor_read_outage_age_seconds"] != 360 {
					cancel()
					return fmt.Errorf("outage was not critical: %+v %v", event, gauges)
				}
				failing.Store(false)
				sampleTime = base.Add(8 * time.Minute)
			}
		case 3:
			if event.Status != "ok" || gauges["sn_mainnet_monitor_read_outage_active"] != 0 || gauges["sn_mainnet_monitor_last_success_timestamp_seconds"] != float64(sampleTime.Unix()) {
				cancel()
				return fmt.Errorf("complete recovery did not clear outage: %+v %v", event, gauges)
			}
			cancel()
		}
		events++
		return nil
	}}
	var stderr bytes.Buffer
	exit := runMonitorTestWithClock(t, ctx, []string{"monitor", "--rpc", server.URL, "--expected-chain", "fixture-mainnet", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", checkpoint, "--metrics-file", path, "--retry-window", "1s", "--interval", "1ns", "--stall-after", "20m"}, stdout, &stderr, func() time.Time { return sampleTime })
	if stdout.eventError != nil {
		t.Fatal(stdout.eventError)
	}
	if exit != 0 || events != 4 {
		t.Fatalf("command exit=%d events=%d stderr=%s", exit, events, stderr.String())
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("collector cannot read the complete textfile: %v %v", info, err)
	}
	store, err := openMonitorCheckpoint(checkpoint, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId})
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state, err := store.load()
	if err != nil || !state.lastSuccessAt.Equal(sampleTime) || !state.unavailableSince.IsZero() {
		t.Fatalf("restart lost recovery facts: %+v %v", state, err)
	}
}

// Losing publication cannot report healthy success or modify the previous
// textfile through a symlink. The output barrier changes the path exactly once.
func TestMonitorMetricsCommandStopsOnPublicationFailure(t *testing.T) {
	server, _ := testRpcServerWithIdentity(t, "fixture-mainnet", "0x3c4", "")
	directory := monitorMetricsTestDir(t)
	path, retained := filepath.Join(directory, "monitor.prom"), filepath.Join(directory, "retained.prom")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := 0
	var retainedBytes []byte
	stdout := &monitorMetricsTestWriter{onEvent: func(event monitorEvent) error {
		events++
		if events == 1 {
			var err error
			retainedBytes, err = os.ReadFile(path)
			if err != nil {
				cancel()
				return err
			}
			if err := os.Rename(path, retained); err != nil {
				cancel()
				return err
			}
			if err := os.Symlink(retained, path); err != nil {
				cancel()
				return err
			}
		} else if event.Status != "metrics-error" || event.Severity != "critical" || !strings.Contains(event.Detail, "status=ok") {
			cancel()
			return fmt.Errorf("publication failure disappeared: %+v", event)
		}
		return nil
	}}
	var stderr bytes.Buffer
	exit := runMonitorTest(t, ctx, []string{"monitor", "--rpc", server.URL, "--expected-chain", "fixture-mainnet", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--metrics-file", path, "--interval", "1ns"}, stdout, &stderr)
	if stdout.eventError != nil {
		t.Fatal(stdout.eventError)
	}
	if exit != 3 || events != 2 {
		t.Fatalf("failed publication kept running: exit=%d events=%d stderr=%s", exit, events, stderr.String())
	}
	after, err := os.ReadFile(retained)
	if err != nil || !bytes.Equal(after, retainedBytes) {
		t.Fatal("publication failure changed old evidence")
	}
}

// A worker-side assertion failure cancels the original command and is reported
// only after its join. No testing.Goexit can consume the completion signal.
func TestMonitorMetricsWorkerFailureReturnsAfterOwnerCancellation(t *testing.T) {
	server, _ := testRpcServerWithIdentity(t, "fixture-mainnet", "0x3c4", "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	want := errors.New("synthetic worker publication assertion")
	events := 0
	stdout := &monitorMetricsTestWriter{onEvent: func(monitorEvent) error {
		events++
		cancel()
		return want
	}}
	var stderr bytes.Buffer
	exit := runMonitorTest(t, ctx, []string{"monitor", "--rpc", server.URL, "--expected-chain", "fixture-mainnet", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--interval", "1ns"}, stdout, &stderr)
	if events != 1 || !errors.Is(stdout.eventError, want) || ctx.Err() != context.Canceled || exit != 0 {
		t.Fatalf("worker failure did not return through joined owner: events=%d error=%v exit=%d stderr=%s", events, stdout.eventError, exit, stderr.String())
	}
}

// Fixed finite gauges do not encode arbitrary RPC data. A malformed future
// event cannot silently acquire status zero or a healthy metric.
func TestMonitorMetricsBoundsAndRejectsUnknownEvents(t *testing.T) {
	state := &monitorState{}
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	event := monitorEvent{Status: "rpc-error", Severity: "warning", ObservedAt: base.Format(time.RFC3339Nano), Detail: strings.Repeat("private-fixture-detail", 100000)}
	raw, err := renderMonitorMetrics(event, state)
	if err != nil || len(raw) > 8*1024 || bytes.Contains(raw, []byte("private-fixture-detail")) {
		t.Fatalf("unbounded or private metric: bytes=%d err=%v", len(raw), err)
	}
	for _, invalid := range []monitorEvent{{Status: "new-unknown"}, {Status: "ok", ObservedAt: "bad"}, {Status: "ok", ObservedAt: event.ObservedAt, Severity: "new-severity"}} {
		if _, err := renderMonitorMetrics(invalid, state); err == nil {
			t.Fatalf("unknown event was accepted: %+v", invalid)
		}
	}
}

// File locking fences simultaneous local publishers. Reopen retains the final
// textfile until an explicitly new complete sample replaces it.
func TestMonitorMetricsSingleOwnerAndPathRefusals(t *testing.T) {
	directory := monitorMetricsTestDir(t)
	path := filepath.Join(directory, "monitor.prom")
	store, err := openMonitorMetrics(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openMonitorMetrics(path); err == nil {
		t.Fatal("second publisher acquired the same metrics path")
	}
	if err := store.save(monitorEvent{Status: "starting"}, &monitorState{}); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if err := store.save(monitorEvent{Status: "starting"}, &monitorState{}); err == nil {
		t.Fatal("closed publisher wrote a new sample")
	}
	for _, invalid := range []string{"relative.prom", filepath.Join(directory, "bad.json"), filepath.Join(directory, "missing", "monitor.prom")} {
		if candidate, err := openMonitorMetrics(invalid); err == nil {
			candidate.close()
			t.Fatalf("invalid path accepted: %s", invalid)
		}
	}
	alias := filepath.Join(directory, "alias.prom")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if candidate, err := openMonitorMetrics(alias); err == nil {
		candidate.close()
		t.Fatal("symlink publication path accepted")
	}
	if err := os.WriteFile(filepath.Join(directory, "public.prom.lock"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	// A restrictive process umask must not turn this deliberately public
	// fixture into the private lock that the implementation correctly accepts.
	if err := os.Chmod(filepath.Join(directory, "public.prom.lock"), 0644); err != nil {
		t.Fatal(err)
	}
	if candidate, err := openMonitorMetrics(filepath.Join(directory, "public.prom")); err == nil {
		candidate.close()
		t.Fatal("public lock accepted")
	}
}

// A directory-sync failure occurs after rename. Treating this error as proof
// of old bytes would lose the actual last generation during restart.
func TestMonitorPublicationsRetainCompleteBytesAfterAmbiguousSync(t *testing.T) {
	directory := monitorMetricsTestDir(t)
	path := filepath.Join(directory, "monitor.prom")
	store, err := openMonitorMetrics(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	called := false
	store.syncDirectory = func(file *os.File) error {
		called = true
		readMonitorTestGauges(t, path)
		return errors.New("synthetic directory sync failure")
	}
	if err := store.save(monitorEvent{Status: "starting"}, &monitorState{}); err == nil || !called {
		t.Fatalf("ambiguous metrics publication became success: %v", err)
	}
	checkpoint, err := openMonitorCheckpoint(filepath.Join(directory, "checkpoint.json"), monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.close()
	checkpoint.syncDirectory = func(file *os.File) error { return fmt.Errorf("synthetic sync failure") }
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := checkpoint.save(&monitorState{unavailableSince: base}); err == nil {
		t.Fatal("ambiguous checkpoint publication became success")
	}
	state, err := checkpoint.load()
	if err != nil || !state.unavailableSince.Equal(base) || state.lastHash != "" {
		t.Fatalf("complete renamed checkpoint was not recoverable: %+v %v", state, err)
	}
}
