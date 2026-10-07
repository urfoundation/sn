// Command regressions join real output/file operations at deterministic
// barriers, keeping diagnostic failure separate from read and custody results.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The exact legacy command fixtures wait for their finite memory delivery;
// bounded Close is not a guarantee that a queued final record will flush.
func runRootMonitorTest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	output := &monitorFixtureOutput{writer: stdout, completed: make(chan struct{}, 1)}
	hooks := monitorServiceHooks{afterEvent: func(ctx context.Context, _ string) {
		select {
		case <-output.completed:
		case <-ctx.Done():
		}
	}}
	return runMainWithMonitorHooks(ctx, args, output, stderr, time.Now, hooks)
}

// A current unavailable read keeps prior finality but never borrows its ready
// flag. Recovery updates successful observation time, not unchanged progress.
func TestRootMonitorOutputReadOutageRetainsEvidence(t *testing.T) {
	_, fixture := newRootFixture(t)
	upstream := rootFixtureServer(t, fixture)
	route, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(route)
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if unavailable.Load() {
			io.Copy(io.Discard, io.LimitReader(request.Body, 8193))
			request.Body.Close()
			fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"synthetic unavailable"}}`)
			return
		}
		proxy.ServeHTTP(writer, request)
	}))
	defer server.Close()
	metrics := filepath.Join(monitorMetricsTestDir(t), "root.prom")
	clock := &monitorServicesTestClock{}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock.seconds.Store(base.Unix())
	var stdout bytes.Buffer
	output := &monitorFixtureOutput{writer: &stdout, completed: make(chan struct{}, 1)}
	samples := 0
	hooks := monitorServiceHooks{afterEvent: func(ctx context.Context, _ string) {
		select {
		case <-output.completed:
		case <-ctx.Done():
			return
		}
		values := monitorOutputTestMetrics(t, metrics)
		wantCurrent, wantSuccess := float64(1), float64(base.Unix())
		if samples == 1 {
			wantCurrent = 0
		}
		if samples == 2 {
			wantSuccess += 20
		}
		if values[`sn_mainnet_root_monitor_current_observation{role="primary"}`] != wantCurrent || values[`sn_mainnet_root_monitor_read_only_ready{role="primary"}`] != wantCurrent || values[`sn_mainnet_root_monitor_last_read_success_timestamp_seconds{role="primary"}`] != wantSuccess || values[`sn_mainnet_root_monitor_finalized_progress_timestamp_seconds{role="primary"}`] != float64(base.Unix()) {
			t.Error("unavailable root read fabricated health or erased good evidence")
		}
		unavailable.Store(samples == 0)
		samples++
		clock.seconds.Add(10)
	}, wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil }}
	args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--metrics-file", metrics, "--metrics-role", "primary", "--samples", "3"}
	if code := runMainWithMonitorHooks(t.Context(), args, output, io.Discard, clock.now, hooks); code != 0 || samples != 3 {
		t.Fatal("root read outage stopped bounded recovery", code, samples)
	}
}

// Events contain a bounded current projection. Prior delivery and publication
// acknowledgment cannot be promoted to current chain observation or acceptance.
func TestRootMonitorOutputMetricsAmbiguityAndOwnershipStayOptional(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	directory := monitorMetricsTestDir(t)
	checkpoint, metrics := filepath.Join(directory, "finalized.json"), filepath.Join(directory, "root.prom")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &monitorServicesTestClock{}
	clock.seconds.Store(base.Unix())
	var stdout bytes.Buffer
	output := &monitorFixtureOutput{writer: &stdout, completed: make(chan struct{}, 1)}
	writes, samples := 0, 0
	hooks := monitorServiceHooks{
		syncDirectory: func(_, kind string, file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			if kind == "metrics" {
				writes++
				if writes == 2 {
					return &rootOutputUnformattableError{}
				}
			}
			return nil
		},
		afterEvent: func(ctx context.Context, _ string) {
			select {
			case <-output.completed:
			case <-ctx.Done():
				return
			}
			samples++
			switch samples {
			case 1:
				values := monitorOutputTestMetrics(t, metrics)
				if values[`sn_mainnet_root_monitor_publication_last_success_timestamp_seconds{role="primary"}`] != 0 || values[`sn_mainnet_root_monitor_read_only_ready{role="primary"}`] != 1 {
					t.Error("ambiguous export acknowledged itself or lost valid read")
				}
			case 2:
				values := monitorOutputTestMetrics(t, metrics)
				if values[`sn_mainnet_root_monitor_publication_status{role="primary"}`] != 3 || values[`sn_mainnet_root_monitor_publication_last_success_timestamp_seconds{role="primary"}`] != 0 {
					t.Error("publication failed to retain previous uncertainty")
				}
				if err := os.Remove(metrics); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(metrics, 0700); err != nil {
					t.Fatal(err)
				}
			}
			clock.seconds.Add(10)
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil },
	}
	args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--checkpoint", checkpoint, "--metrics-file", metrics, "--metrics-role", "primary", "--samples", "4"}
	if code := runMainWithMonitorHooks(t.Context(), args, output, io.Discard, clock.now, hooks); code != 0 || writes != 3 || samples != 4 {
		t.Fatal("optional metrics fault stopped observation", code, writes, samples)
	}
	decoder := json.NewDecoder(&stdout)
	for index, want := range []string{"retrying", "published", "ownership-error", "ownership-error"} {
		var event rootMonitorEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Schema != rootDiagnosticEventSchema || event.Publication != want || event.Observation == nil || !event.Observation.ReadOnlyReady || event.Observation.ActivationReady || event.Snapshot != nil || event.Sample != index+1 {
			t.Fatal("root diagnostic projection differs")
		}
		if index == 0 && event.Diagnostics.Events.Delivered != 0 {
			t.Fatal("first event acknowledged itself")
		}
	}
	info, err := os.Stat(metrics)
	if err != nil || !info.IsDir() {
		t.Fatal("disabled publisher overwrote replacement")
	}
	store, err := openMonitorCheckpoint(checkpoint, identityExpectation{NativeChain: fixture.policy.NativeChain, GenesisHash: fixture.policy.GenesisHash, EvmChainId: 964})
	if err != nil {
		t.Fatal("command did not release checkpoint")
	}
	defer store.close()
	state, err := store.load()
	if err != nil || state.lastNumber != 100 || !state.lastProgressAt.Equal(base) {
		t.Fatal("optional output changed finalized evidence")
	}
}

// Missing metrics cannot be described as independently observable delivery.
// A refused regular-file logger remains untouched while chain reads complete.
func TestRootMonitorOutputUnconfiguredAndCompactWire(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	var stdout, stderr bytes.Buffer
	args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy)}
	if code := runRootMonitorTest(t.Context(), args, &stdout, &stderr); code != 0 {
		t.Fatal("root monitor read failed", code)
	}
	if stdout.Len() > 4096 {
		t.Fatal("compact root event exceeded scalar bound")
	}
	var event rootMonitorEvent
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || event.Publication != "unconfigured" || event.Snapshot != nil || event.Observation == nil || !planSha256(event.Observation.ContentHash) || !planSha256(event.Observation.PolicyHash) {
		t.Fatal("compact event lost explicit unknown delivery")
	}
	if strings.Contains(stdout.String(), "storage") || strings.Contains(stdout.String(), "hotkey") {
		t.Fatal("daemon record contains full root census")
	}
	file, err := os.CreateTemp(mainnetPrivateTestDir(t), "unsupported")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if code := runMain(t.Context(), args, file, file); code != 0 {
		t.Fatal("unsupported logger altered core result", code)
	}
	if info, err := file.Stat(); err != nil || info.Size() != 0 {
		t.Fatal("unsupported file logger was called")
	}
	// Finite preview still provides its original complete evidence contract.
	stdout.Reset()
	stderr.Reset()
	args[0] = "root-preview"
	if code := runMain(t.Context(), args, &stdout, &stderr); code != 0 {
		t.Fatal("finite preview changed", code)
	}
	event = rootMonitorEvent{}
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || event.Schema != rootEventSchema || event.Snapshot == nil || event.Observation != nil || event.Diagnostics != nil {
		t.Fatal("finite preview evidence contract changed")
	}
}

// The existing minimum read budget remains admission, while canceling an
// in-flight HTTP request joins the real handler and leaves startup unknown.
func TestRootMonitorOutputReadCancellationPreservesBudgetAndUnknown(t *testing.T) {
	_, fixture := newRootFixture(t)
	url, entered, left := monitorServicesBlockedChain(t)
	metrics := filepath.Join(monitorMetricsTestDir(t), "root.prom")
	args := []string{"root-monitor", "--rpc", url, "--policy", rootTestPolicyFile(t, fixture.policy), "--retry-window", "59s", "--metrics-file", metrics, "--metrics-role", "primary"}
	if code := runMain(t.Context(), args, io.Discard, io.Discard); code != 2 {
		t.Fatal("sub-minute retry admitted")
	}
	args[6] = "60s"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- runMain(ctx, args, io.Discard, io.Discard) }()
	<-entered
	values := monitorOutputTestMetrics(t, metrics)
	if values[`sn_mainnet_root_monitor_sample_timestamp_seconds{role="primary"}`] != 0 || values[`sn_mainnet_root_monitor_current_observation{role="primary"}`] != 0 || values[`sn_mainnet_root_monitor_read_only_ready{role="primary"}`] != 0 {
		t.Fatal("blocked initial read became known healthy")
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatal("cancellation changed existing exit semantics", code)
	}
	<-left
}

// Hard continuity checkpoint writes and cleanup stay visible even when logs
// cannot be delivered. All admitted file owners release their actual handles.
func TestRootMonitorOutputCustodyAndEarlyCleanupRemainHard(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	for _, cleanupFault := range []bool{false, true} {
		directory := monitorMetricsTestDir(t)
		checkpoint := filepath.Join(directory, "finalized.json")
		metrics := filepath.Join(directory, "root.prom")
		closed := 0
		hooks := monitorServiceHooks{
			syncDirectory: func(_, kind string, file *os.File) error {
				if err := file.Sync(); err != nil {
					return err
				}
				if kind == "checkpoint" && !cleanupFault {
					return &rootOutputUnformattableError{}
				}
				return nil
			},
			afterClose: func(_, kind string, file *os.File) error {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Error("real owner remained open")
				}
				closed++
				if kind == "checkpoint" && cleanupFault {
					return &rootOutputUnformattableError{}
				}
				return nil
			},
			wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil },
		}
		args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--checkpoint", checkpoint, "--metrics-file", metrics, "--metrics-role", "primary", "--samples", "2"}
		code := runMainWithMonitorHooks(t.Context(), args, nil, nil, time.Now, hooks)
		if closed != 2 || cleanupFault && code != 3 || !cleanupFault && code != 1 {
			t.Fatal("optional logger hid custody/cleanup failure", cleanupFault, code, closed)
		}
		store, err := openMonitorCheckpoint(checkpoint, identityExpectation{NativeChain: fixture.policy.NativeChain, GenesisHash: fixture.policy.GenesisHash, EvmChainId: 964})
		if err != nil {
			t.Fatal("checkpoint owner leaked")
		}
		store.close()
	}
}

// Optional metric admission has no observation authority. A refused target is
// untouched and the independently valid checkpoint still retains each read.
func TestRootMonitorOutputRefusedMetricsAdmissionStillSamples(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	for _, unsafeDirectory := range []bool{false, true} {
		directory := mainnetPrivateTestDir(t)
		checkpoint := filepath.Join(directory, "root.json")
		metricDirectory := filepath.Join(directory, "metrics")
		if err := os.Mkdir(metricDirectory, 0700); err != nil {
			t.Fatal(err)
		}
		metrics := filepath.Join(metricDirectory, "root.prom")
		retained := []byte("synthetic retained output\n")
		if unsafeDirectory {
			if err := os.WriteFile(metrics, retained, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(metricDirectory, 0770); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Mkdir(metrics, 0700); err != nil {
				t.Fatal(err)
			}
		}
		var stdout bytes.Buffer
		output := &monitorFixtureOutput{writer: &stdout, completed: make(chan struct{}, 1)}
		hooks := monitorServiceHooks{afterEvent: func(ctx context.Context, _ string) {
			select {
			case <-output.completed:
			case <-ctx.Done():
			}
		}, wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil }}
		args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--checkpoint", checkpoint, "--metrics-file", metrics, "--metrics-role", "primary", "--samples", "2"}
		if code := runMainWithMonitorHooks(t.Context(), args, output, io.Discard, time.Now, hooks); code != 0 {
			t.Fatal("optional metrics admission stopped read-only sampling", code)
		}
		decoder := json.NewDecoder(&stdout)
		for sample := 1; sample <= 2; sample++ {
			var event rootMonitorEvent
			if err := decoder.Decode(&event); err != nil || event.Sample != sample || event.Publication != "unavailable" || event.Observation == nil || !event.Observation.ReadOnlyReady {
				t.Fatal("refused metrics target fabricated delivery or lost sample")
			}
		}
		if unsafeDirectory {
			raw, err := os.ReadFile(metrics)
			if err != nil || !bytes.Equal(raw, retained) {
				t.Fatal("refused output target was modified")
			}
		} else {
			entries, err := os.ReadDir(metrics)
			if err != nil || len(entries) != 0 {
				t.Fatal("refused output directory was modified")
			}
		}
		store, err := openMonitorCheckpoint(checkpoint, identityExpectation{NativeChain: fixture.policy.NativeChain, GenesisHash: fixture.policy.GenesisHash, EvmChainId: 964})
		if err != nil {
			t.Fatal(err)
		}
		state, err := store.load()
		store.close()
		if err != nil || state.lastNumber != 100 {
			t.Fatal("optional admission fault prevented required continuity")
		}
	}
}
