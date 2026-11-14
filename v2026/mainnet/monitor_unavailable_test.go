package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// The output barrier changes the route only after a complete healthy event.
// Cancellation follows the fourth event; no sleep decides the assertion.
type outageMonitorWriter struct {
	bytes.Buffer
	cancel  context.CancelFunc
	failing *atomic.Bool
	events  int
}

func (self *outageMonitorWriter) Write(value []byte) (int, error) {
	n, err := self.Buffer.Write(value)
	self.events++
	if self.events == 1 {
		self.failing.Store(true)
	}
	if self.events == 4 {
		self.cancel()
	}
	return n, err
}

// A persisted read outage escalates without terminating the monitor or
// resetting its clock at each retry. Every request remains local and synthetic.
func TestMonitorReadOutageEscalatesAndSurvivesRestart(t *testing.T) {
	upstream, _ := testRpcServerWithEvm(t, "0x3c4", "")
	upstreamUrl, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstreamUrl)
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if failing.Load() {
			http.Error(writer, "synthetic read outage", http.StatusBadRequest)
			return
		}
		proxy.ServeHTTP(writer, request)
	}))
	defer server.Close()
	base := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	sampleTimes := []time.Time{base, base.Add(time.Minute), base.Add(3*time.Minute + time.Second), base.Add(6*time.Minute + time.Second)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := &outageMonitorWriter{cancel: cancel, failing: &failing}
	// The completed event advances the fixture clock. Adding a clock read to
	// production cannot consume the next sample's intended time.
	clock := func() time.Time {
		index := stdout.events
		if index >= len(sampleTimes) {
			index = len(sampleTimes) - 1
		}
		return sampleTimes[index]
	}
	path := filepath.Join(t.TempDir(), "monitor.json")
	var stderr bytes.Buffer
	exit := runMonitorTestWithClock(t, ctx, []string{
		"monitor", "--rpc", server.URL, "--expected-chain", "Bittensor",
		"--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964",
		"--checkpoint", path, "--retry-window", "2s", "--interval", "1ms", "--stall-after", "20m",
	}, stdout, &stderr, clock)
	if exit != 0 || stdout.events != 4 {
		t.Fatalf("monitor stopped before four observations: exit=%d events=%d stderr=%s", exit, stdout.events, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	wantStatuses := []string{"ok", "rpc-error", "rpc-error", "rpc-error"}
	wantSeverities := []string{"", "", "warning", "critical"}
	for index, line := range lines {
		var event monitorEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Status != wantStatuses[index] || event.Severity != wantSeverities[index] {
			t.Fatalf("event %d: %+v %v", index, event, err)
		}
	}
	store, err := openMonitorCheckpoint(path, monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state, err := store.load()
	if err != nil || !state.unavailableSince.Equal(sampleTimes[1]) || state.lastHash != testFinalizedHash {
		t.Fatalf("restart lost the original unavailable boundary: %+v %v", state, err)
	}
	if severity, changed := state.observeUnavailable(base.Add(7*time.Minute), base.Add(7*time.Minute)); severity != "critical" || changed {
		t.Fatalf("restart reset the outage age: %q %v", severity, changed)
	}
	if !state.clearUnavailable() {
		t.Fatal("complete recovery did not clear the outage")
	}
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	restored, err := store.load()
	if err != nil || !restored.unavailableSince.IsZero() {
		t.Fatalf("recovery checkpoint retained a stale outage: %+v %v", restored, err)
	}
}

// A backwards host clock step escalates instead of concealing a retained
// outage until the wall clock catches up.
func TestMonitorReadOutageClockRollbackIsCritical(t *testing.T) {
	base := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	state := &monitorState{}
	if severity, changed := state.observeUnavailable(base, base); severity != "" || !changed {
		t.Fatalf("first failure: %q %v", severity, changed)
	}
	if severity, changed := state.observeUnavailable(base.Add(-time.Second), base.Add(-time.Second)); severity != "critical" || changed {
		t.Fatalf("clock rollback concealed outage: %q %v", severity, changed)
	}
}

// A long retry is part of the outage. The first failed sample must not start
// the warning clock only when its finite RPC budget finally expires.
func TestMonitorReadOutageCountsTheFirstRetryWindow(t *testing.T) {
	base := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	state := &monitorState{}
	if severity, changed := state.observeUnavailable(base, base.Add(3*time.Minute)); severity != "warning" || !changed || !state.unavailableSince.Equal(base) {
		t.Fatalf("first retry window was lost: %q %v %+v", severity, changed, state)
	}
	if severity, changed := state.observeUnavailable(base.Add(4*time.Minute), base.Add(5*time.Minute)); severity != "critical" || changed {
		t.Fatalf("second retry reset outage age: %q %v", severity, changed)
	}
}

// A checkpoint from the previously published schema keeps its finalized
// continuity when upgraded; the first new outage is written in the current schema.
func TestMonitorCheckpointMigratesLegacyFinalityWithoutInventingOutage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.json")
	base := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	record := monitorCheckpointRecord{Schema: monitorCheckpointLegacySchema, NativeChain: "Bittensor",
		GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId, FinalizedHash: testFinalizedHash,
		FinalizedAt: 100, LastProgressAt: base.Format(time.RFC3339Nano)}
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
	store, err := openMonitorCheckpoint(path, monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state, err := store.load()
	if err != nil || state.lastHash != testFinalizedHash || !state.unavailableSince.IsZero() {
		t.Fatalf("legacy checkpoint changed continuity: %+v %v", state, err)
	}
	if severity, changed := state.observeUnavailable(base.Add(time.Minute), base.Add(time.Minute)); severity != "" || !changed {
		t.Fatalf("first post-upgrade outage: %q %v", severity, changed)
	}
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var migrated monitorCheckpointRecord
	if err := json.Unmarshal(updated, &migrated); err != nil || migrated.Schema != monitorCheckpointSchema || migrated.UnavailableSince != base.Add(time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("checkpoint was not migrated on write: %+v %v", migrated, err)
	}
	legacyWithOutage := migrated
	legacyWithOutage.Schema = monitorCheckpointLegacySchema
	if err := store.validate(legacyWithOutage); err == nil {
		t.Fatal("legacy schema admitted a new outage field")
	}
}
