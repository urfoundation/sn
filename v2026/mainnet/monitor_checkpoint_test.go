package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cancelMonitorWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (self *cancelMonitorWriter) Write(value []byte) (int, error) {
	n, err := self.Buffer.Write(value)
	self.cancel()
	return n, err
}

func monitorTestExpectation() identityExpectation {
	return identityExpectation{NativeChain: "Bittensor", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId}
}

func TestMonitorCommandPersistsAndResumesCheckpoint(t *testing.T) {
	server, _ := testRpcServerWithEvm(t, "0x3c4", "")
	path := filepath.Join(t.TempDir(), "monitor.json")
	for run := 0; run < 2; run++ {
		ctx, cancel := context.WithCancel(context.Background())
		stdout := &cancelMonitorWriter{cancel: cancel}
		var stderr bytes.Buffer
		exit := runMonitorTest(t, ctx, []string{
			"monitor", "--rpc", server.URL, "--expected-chain", "Bittensor",
			"--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964",
			"--checkpoint", path, "--retry-window", "2s",
		}, stdout, &stderr)
		cancel()
		if exit != 0 {
			t.Fatalf("monitor run %d exited %d: %s", run, exit, stderr.String())
		}
		var event monitorEvent
		if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || event.Status != "ok" {
			t.Fatalf("monitor run %d event: %v %s", run, err, stdout.String())
		}
		store, err := openMonitorCheckpoint(path, monitorTestExpectation())
		if err != nil {
			t.Fatal(err)
		}
		state, loadErr := store.load()
		closeErr := store.close()
		if loadErr != nil || closeErr != nil || state.lastHash != testFinalizedHash || state.lastNumber != 100 {
			t.Fatalf("monitor run %d checkpoint: %+v %v %v", run, state, loadErr, closeErr)
		}
	}
}

// A restart retains the last finalized hash and progress clock. A later RPC
// sample cannot erase a finalized regression by presenting a fresh process.
func TestMonitorCheckpointRetainsFinalityAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.json")
	expected := monitorTestExpectation()
	store, err := openMonitorCheckpoint(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 27, 7, 0, 0, 0, time.UTC)
	state := &monitorState{}
	if status, err := state.observe(base, chainIdentity{FinalizedHash: testFinalizedHash, FinalizedNumber: 100}, time.Minute); status != "ok" || err != nil {
		t.Fatalf("first finality sample: %q %v", status, err)
	}
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := openMonitorCheckpoint(path, expected); err == nil {
		t.Fatal("second monitor acquired an owned checkpoint")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store, err = openMonitorCheckpoint(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	restored, err := store.load()
	if err != nil || restored.lastHash != testFinalizedHash || restored.lastNumber != 100 || !restored.lastProgressAt.Equal(base) {
		t.Fatalf("restored finality differs: %+v %v", restored, err)
	}
	if status, err := restored.observe(base.Add(2*time.Minute), chainIdentity{FinalizedHash: testFinalizedHash, FinalizedNumber: 100}, time.Minute); status != "finality-stalled" || err != nil {
		t.Fatalf("restart lost stall clock: %q %v", status, err)
	}
	if status, err := restored.observe(base.Add(3*time.Minute), chainIdentity{FinalizedHash: testGenesisHash, FinalizedNumber: 99}, time.Minute); status != "finality-conflict" || err == nil {
		t.Fatalf("restart accepted finalized regression: %q %v", status, err)
	}
}

// Local checksum and expectation checks detect corruption and wrong-network
// reuse before the monitor begins sampling a route.
func TestMonitorCheckpointRejectsCorruptionAndWrongNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.json")
	store, err := openMonitorCheckpoint(path, monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Now().UTC()}
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	wrong := monitorTestExpectation()
	wrong.GenesisHash = testFinalizedHash
	foreign := &monitorCheckpointStore{path: path, expected: wrong}
	if _, err := foreign.load(); err == nil {
		t.Fatal("foreign mainnet expectation reused checkpoint")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(string(raw), testFinalizedHash, testGenesisHash, 1)
	if err := os.WriteFile(path, []byte(corrupt), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("changed finalized hash escaped checkpoint checksum: %v", err)
	}
}

// A symlink cannot redirect checkpoint publication or be read as retained
// finality; a relative destination cannot become an implicit workdir write.
func TestMonitorCheckpointRejectsSymlinkAndRelativePath(t *testing.T) {
	if _, err := openMonitorCheckpoint("monitor.json", monitorTestExpectation()); err == nil {
		t.Fatal("relative checkpoint path was accepted")
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "other.json")
	path := filepath.Join(directory, "monitor.json")
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	store, err := openMonitorCheckpoint(path, monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if _, err := store.load(); err == nil {
		t.Fatal("symlink checkpoint was read")
	}
	if err := store.save(&monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Now().UTC()}); err == nil {
		t.Fatal("symlink checkpoint was replaced")
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "{}" {
		t.Fatalf("symlink target changed: %q %v", raw, err)
	}
	otherPath := filepath.Join(directory, "other-monitor.json")
	if err := os.Symlink(target, otherPath+".lock"); err != nil {
		t.Fatal(err)
	}
	if _, err := openMonitorCheckpoint(otherPath, monitorTestExpectation()); err == nil {
		t.Fatal("symlink checkpoint lock was accepted")
	}
}
