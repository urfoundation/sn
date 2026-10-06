// Restart, ownership and completion-time regressions exercise the command with
// local synthetic reads. Explicit request/output barriers determine ordering.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// A restart with a blocked first request must preserve both an existing page
// and a stale healthy sample. Startup is not new observation evidence.
func TestMonitorRestartRetainsMetricsWhileInitialReadPending(t *testing.T) {
	for _, prior := range []monitorEvent{{Status: "rpc-error", Severity: "critical"}, {Status: "ok"}} {
		func() {
			directory := monitorMetricsTestDir(t)
			path := filepath.Join(directory, "monitor.prom")
			base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			prior.ObservedAt = base.Format(time.RFC3339Nano)
			store, err := openMonitorMetrics(path)
			if err != nil {
				t.Fatal(err)
			}
			state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: base, lastSuccessAt: base}
			if err := errors.Join(store.save(prior, state), store.close()); err != nil {
				t.Fatal(err)
			}
			retained, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			url, entered, left := monitorServicesBlockedChain(t)
			ctx, cancel := context.WithCancel(context.Background())
			var stdout, stderr bytes.Buffer
			done := make(chan struct{})
			var exit int
			go func() {
				defer close(done)
				exit = runMonitorTestWithClock(t, ctx, []string{"monitor", "--rpc", url, "--expected-chain", "fixture-mainnet", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--metrics-file", path}, &stdout, &stderr, func() time.Time { return base.Add(time.Hour) })
			}()
			defer func() {
				cancel()
				<-done
			}()
			select {
			case <-entered:
			case <-done:
				t.Fatalf("monitor stopped before initial request: exit=%d stderr=%s", exit, stderr.String())
			}
			pending, readErr := os.ReadFile(path)
			cancel()
			<-done
			<-left
			if readErr != nil || !bytes.Equal(pending, retained) || exit != 0 || stdout.Len() != 0 {
				t.Fatalf("restart erased retained %s evidence before a sample: same=%v read=%v exit=%d stdout=%s stderr=%s", prior.Status, bytes.Equal(pending, retained), readErr, exit, stdout.String(), stderr.String())
			}
		}()
	}
}

// An aliased checkpoint cannot replace the metrics owner's lock inode. The
// command must reject the collision before reads, output or ownership changes.
func TestMonitorRejectsAliasedCheckpointMetricsLockCollision(t *testing.T) {
	directory := monitorMetricsTestDir(t)
	metricsPath := filepath.Join(directory, "monitor.prom")
	lockPath := metricsPath + ".lock"
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	expected := identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId}
	store, err := openMonitorCheckpoint(lockPath, expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(store.save(&monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: base}), store.close()); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "collector")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	server, getCount := testRpcServerWithIdentity(t, expected.NativeChain, "0x3c4", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := &cancelMonitorWriter{cancel: cancel}
	var stderr bytes.Buffer
	exit := runMonitorTestWithClock(t, ctx, []string{"monitor", "--rpc", server.URL, "--expected-chain", expected.NativeChain, "--expected-genesis", expected.GenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", filepath.Join(alias, "monitor.prom.lock"), "--metrics-file", metricsPath}, stdout, &stderr, func() time.Time { return base.Add(time.Minute) })
	after, statErr := os.Stat(lockPath)
	if exit != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "paths must be separate") || getCount("system_chain") != 0 || statErr != nil || !os.SameFile(before, after) {
		t.Fatalf("aliased outputs escaped separation: exit=%d reads=%d stat=%v stdout=%s stderr=%s", exit, getCount("system_chain"), statErr, stdout.String(), stderr.String())
	}
}

// Resolving a checkpoint parent once binds subsequent writes to the same
// physical directory as the lock, even when the caller's alias is retargeted.
func TestMonitorCheckpointRetainsPhysicalDirectoryWhenAliasMoves(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	alias := filepath.Join(t.TempDir(), "current")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	store, err := openMonitorCheckpoint(filepath.Join(alias, "checkpoint.json"), monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := store.save(&monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: base}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(first, "checkpoint.json")); err != nil {
		t.Fatalf("checkpoint abandoned its physical owner: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "checkpoint.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkpoint followed a changed alias: %v", err)
	}
	other, err := openMonitorCheckpoint(filepath.Join(first, "checkpoint.json"), monitorTestExpectation())
	if err == nil {
		other.close()
		t.Fatal("physical checkpoint lost its original owner")
	}
}

// Releasing the file lock ends this object's authority. A retained pointer
// cannot read or replace the next owner's checkpoint after reacquisition.
func TestMonitorCheckpointClosedOwnerCannotOverwriteSuccessor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	first, err := openMonitorCheckpoint(path, monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	original := &monitorState{lastHash: testGenesisHash, lastNumber: 99, lastProgressAt: base}
	if err := errors.Join(first.save(original), first.close()); err != nil {
		t.Fatal(err)
	}
	second, err := openMonitorCheckpoint(path, monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	if err := second.save(&monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: base.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.load(); err == nil {
		t.Error("closed checkpoint retained access to its successor")
	}
	if err := first.save(original); err == nil {
		t.Error("closed checkpoint overwrote its successor")
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(retained, current) {
		t.Errorf("successor checkpoint changed after closed-owner write: %v", err)
	}
	if err := first.close(); err != nil {
		t.Errorf("closed owner close was not idempotent: %v", err)
	}
}

// A successful historical continuity read finishes after the identity sample.
// The published success timestamp must include that final required read.
func TestMonitorSuccessfulContinuityUsesCompletionTime(t *testing.T) {
	upstream, _ := testRpcServerWithIdentity(t, "fixture-mainnet", "0x3c4", "")
	upstreamUrl, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstreamUrl)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	completedAt := base.Add(3 * time.Minute)
	var now atomic.Int64
	now.Store(base.UnixNano())
	var continuityReads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, "synthetic body read failed", http.StatusBadRequest)
			return
		}
		var call struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			http.Error(writer, "synthetic invalid request", http.StatusBadRequest)
			return
		}
		if call.Method == "chain_getBlockHash" && len(call.Params) == 1 && string(call.Params[0]) == "99" {
			continuityReads.Add(1)
			now.Store(completedAt.UnixNano())
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		proxy.ServeHTTP(writer, request)
	}))
	defer server.Close()
	directory := monitorMetricsTestDir(t)
	path, metricsPath := filepath.Join(directory, "checkpoint.json"), filepath.Join(directory, "monitor.prom")
	expected := identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId}
	store, err := openMonitorCheckpoint(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(store.save(&monitorState{lastHash: testGenesisHash, lastNumber: 99, lastProgressAt: base.Add(-time.Minute), lastSuccessAt: base.Add(-time.Minute)}), store.close()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := &cancelMonitorWriter{cancel: cancel}
	var stderr bytes.Buffer
	exit := runMonitorTestWithClock(t, ctx, []string{"monitor", "--rpc", server.URL, "--expected-chain", expected.NativeChain, "--expected-genesis", expected.GenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", path, "--metrics-file", metricsPath}, stdout, &stderr, func() time.Time { return time.Unix(0, now.Load()).UTC() })
	var event monitorEvent
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || exit != 0 || event.Status != "ok" || continuityReads.Load() != 1 || event.ObservedAt != completedAt.Format(time.RFC3339Nano) {
		t.Fatalf("continuity completion time was omitted: exit=%d event=%+v reads=%d err=%v stderr=%s", exit, event, continuityReads.Load(), err, stderr.String())
	}
	gauges := readMonitorTestGauges(t, metricsPath)
	if gauges["sn_mainnet_monitor_sample_timestamp_seconds"] != float64(completedAt.Unix()) || gauges["sn_mainnet_monitor_last_success_timestamp_seconds"] != float64(completedAt.Unix()) {
		t.Fatalf("completed continuity read published stale telemetry: %v", gauges)
	}
	store, err = openMonitorCheckpoint(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state, err := store.load()
	if err != nil || !state.lastSuccessAt.Equal(completedAt) || !state.lastProgressAt.Equal(completedAt) {
		t.Fatalf("checkpoint omitted continuity completion: %+v %v", state, err)
	}
}
