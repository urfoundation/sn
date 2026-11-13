// Initial outage evidence survives restart without inventing a finalized
// position. All read failures and wall-clock steps are controlled explicitly.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Each process emits one unavailable event and is canceled at that exact
// output boundary. The second starts beyond the original page threshold.
func TestMonitorInitialOutageSurvivesCommandRestart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "synthetic unavailable identity", http.StatusBadRequest)
	}))
	defer server.Close()
	directory := monitorMetricsTestDir(t)
	path, metricsPath := filepath.Join(directory, "checkpoint.json"), filepath.Join(directory, "monitor.prom")
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	expected := identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId}
	for index, now := range []time.Time{base, base.Add(6 * time.Minute), base.Add(-time.Second)} {
		ctx, cancel := context.WithCancel(context.Background())
		stdout := &cancelMonitorWriter{cancel: cancel}
		var stderr bytes.Buffer
		exit := runMonitorTestWithClock(t, ctx, []string{"monitor", "--rpc", server.URL, "--expected-chain", expected.NativeChain, "--expected-genesis", expected.GenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", path, "--metrics-file", metricsPath, "--retry-window", "1s"}, stdout, &stderr, func() time.Time { return now })
		cancel()
		var event monitorEvent
		if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || exit != 0 || event.Status != "rpc-error" || index > 0 && event.Severity != "critical" {
			t.Fatalf("run %d reset the initial outage: exit=%d event=%+v err=%v stderr=%s", index, exit, event, err, stderr.String())
		}
		store, err := openMonitorCheckpoint(path, expected)
		if err != nil {
			t.Fatal(err)
		}
		state, loadErr := store.load()
		closeErr := store.close()
		if loadErr != nil || closeErr != nil || !state.unavailableSince.Equal(base) || state.lastHash != "" || state.lastNumber != 0 || !state.lastProgressAt.IsZero() || !state.lastSuccessAt.IsZero() {
			t.Fatalf("initial outage became new or healthy state: %+v %v %v", state, loadErr, closeErr)
		}
		gauges := readMonitorTestGauges(t, metricsPath)
		if gauges["sn_mainnet_monitor_has_finalized_evidence"] != 0 || gauges["sn_mainnet_monitor_last_success_timestamp_seconds"] != 0 || gauges["sn_mainnet_monitor_read_outage_started_timestamp_seconds"] != float64(base.Unix()) {
			t.Fatalf("initial outage metrics invented success: %v", gauges)
		}
	}
	healthy, _ := testRpcServerWithIdentity(t, "fixture-mainnet", "0x3c4", "")
	ctx, cancel := context.WithCancel(context.Background())
	stdout := &cancelMonitorWriter{cancel: cancel}
	var stderr bytes.Buffer
	recoveredAt := base.Add(7 * time.Minute)
	exit := runMonitorTestWithClock(t, ctx, []string{"monitor", "--rpc", healthy.URL, "--expected-chain", expected.NativeChain, "--expected-genesis", expected.GenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", path, "--metrics-file", metricsPath}, stdout, &stderr, func() time.Time { return recoveredAt })
	cancel()
	if exit != 0 {
		t.Fatalf("initial outage could not recover: exit=%d %s", exit, stderr.String())
	}
	gauges := readMonitorTestGauges(t, metricsPath)
	if gauges["sn_mainnet_monitor_healthy"] != 1 || gauges["sn_mainnet_monitor_has_finalized_evidence"] != 1 || gauges["sn_mainnet_monitor_last_success_timestamp_seconds"] != float64(recoveredAt.Unix()) || gauges["sn_mainnet_monitor_read_outage_active"] != 0 {
		t.Fatalf("first complete read did not recover initial outage: %v", gauges)
	}
}

// A v2 record retains its original outage, while the v3 writer only adds facts
// that have actually been observed. No prior success timestamp is inferred.
func TestMonitorCheckpointMigratesV2WithoutInventingSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	record := monitorCheckpointRecord{Schema: monitorCheckpointOutageSchema, NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId, FinalizedHash: testFinalizedHash, FinalizedAt: 100, LastProgressAt: base.Format(time.RFC3339Nano), UnavailableSince: base.Add(time.Minute).Format(time.RFC3339Nano)}
	var err error
	record.ContentHash, err = hashMonitorCheckpoint(record)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := openMonitorCheckpoint(path, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId})
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state, err := store.load()
	if err != nil || !state.lastSuccessAt.IsZero() || !state.unavailableSince.Equal(base.Add(time.Minute)) || state.lastHash != testFinalizedHash {
		t.Fatalf("v2 history changed: %+v %v", state, err)
	}
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &record) != nil || record.Schema != monitorCheckpointSchema || record.LastSuccessAt != "" {
		t.Fatalf("v3 migration fabricated a successful read: %s %v", raw, err)
	}
}

// A checksum cannot legalize partially absent finality or a claimed healthy
// pre-baseline state; all fields must represent one actual observation phase.
func TestMonitorCheckpointRejectsPartialInitialState(t *testing.T) {
	store, err := openMonitorCheckpoint(filepath.Join(t.TempDir(), "checkpoint.json"), monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, state := range []monitorState{
		{},
		{lastHash: testFinalizedHash, unavailableSince: base},
		{lastNumber: 100, unavailableSince: base},
		{lastProgressAt: base, unavailableSince: base},
		{lastSuccessAt: base, unavailableSince: base},
	} {
		if err := store.save(&state); err == nil {
			t.Fatalf("partial checkpoint became durable authority: %+v", state)
		}
	}
}

// Terminal identity/conflict samples must carry critical severity in the actual
// event and textfile. A last checked position remains distinct from a bad read.
func TestMonitorMetricsCommandMarksIntegrityAndStallsCritical(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, test := range []struct {
		name         string
		evmChainId   string
		inconsistent bool
		stalled      bool
		conflict     bool
		status       string
		exit         int
	}{
		{name: "identity", evmChainId: "0x1", status: "identity-mismatch", exit: 3},
		{name: "integrity", evmChainId: "0x3c4", inconsistent: true, status: "rpc-integrity", exit: 3},
		{name: "conflict", evmChainId: "0x3c4", conflict: true, status: "finality-conflict", exit: 3},
		{name: "stall", evmChainId: "0x3c4", stalled: true, status: "finality-stalled", exit: 0},
	} {
		server, _ := testRpcServerWithIdentity(t, "fixture-mainnet", test.evmChainId, "", test.inconsistent)
		directory := monitorMetricsTestDir(t)
		checkpointPath, metricsPath := filepath.Join(directory, "checkpoint.json"), filepath.Join(directory, "monitor.prom")
		if test.stalled || test.conflict {
			store, err := openMonitorCheckpoint(checkpointPath, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId})
			if err != nil {
				t.Fatal(err)
			}
			hash := testFinalizedHash
			if test.conflict {
				hash = testGenesisHash
			}
			if err := store.save(&monitorState{lastHash: hash, lastNumber: 100, lastProgressAt: base.Add(-6 * time.Minute)}); err != nil {
				t.Fatal(err)
			}
			if err := store.close(); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		stdout := &cancelMonitorWriter{cancel: cancel}
		var stderr bytes.Buffer
		exit := runMonitorTestWithClock(t, ctx, []string{"monitor", "--rpc", server.URL, "--expected-chain", "fixture-mainnet", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", checkpointPath, "--metrics-file", metricsPath}, stdout, &stderr, func() time.Time { return base })
		cancel()
		var event monitorEvent
		if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || exit != test.exit || event.Status != test.status || event.Severity != "critical" {
			t.Fatalf("%s event lost critical failure: exit=%d %+v err=%v stderr=%s", test.name, exit, event, err, stderr.String())
		}
		if gauges := readMonitorTestGauges(t, metricsPath); gauges["sn_mainnet_monitor_severity"] != 2 || gauges["sn_mainnet_monitor_healthy"] != 0 {
			t.Fatalf("%s critical telemetry became healthy: %v", test.name, gauges)
		}
	}
}
