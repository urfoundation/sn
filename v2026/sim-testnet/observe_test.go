package main

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A stale state file from a prior supervisor generation cannot represent a
// healthy topology, even when every recorded child still says healthy.
func TestStatusRejectsStaleSupervisorGeneration(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: ticks + 1,
		Processes: []ProcessState{{ID: "validator-1", PID: os.Getpid(), Healthy: true}},
	}
	path := filepath.Join(dir, "supervisor.state.json")
	if err := writePublicJSON(path, state); err != nil {
		t.Fatal(err)
	}
	status, err := Status(context.Background(), cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if status.Healthy || !strings.Contains(strings.Join(status.Warnings, "\n"), "start time changed") {
		t.Fatalf("stale supervisor status = %+v", status)
	}

	state.SupervisorStartTimeTicks = ticks
	if err := writePublicJSON(path, state); err != nil {
		t.Fatal(err)
	}
	status, err = Status(context.Background(), cfg, dir)
	if err != nil || !status.Healthy || len(status.Warnings) != 0 {
		t.Fatalf("live supervisor status = %+v, %v", status, err)
	}
}

func TestCallUint64AcceptsABIUint64AndBoundedUint256(t *testing.T) {
	for _, value := range []any{uint64(100_000), big.NewInt(100_000)} {
		got, err := callUint64(value, nil)
		if err != nil || got != 100_000 {
			t.Fatalf("callUint64(%T) = %d, %v", value, got, err)
		}
	}
}

func TestCallUint64RejectsMalformedAndPropagatesCallFailure(t *testing.T) {
	for _, value := range []any{nil, int64(1), big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 65)} {
		if _, err := callUint64(value, nil); err == nil {
			t.Fatalf("callUint64 accepted %T(%v)", value, value)
		}
	}
	want := errors.New("rpc failed")
	if _, err := callUint64(uint64(1), want); !errors.Is(err, want) {
		t.Fatalf("call failure = %v, want %v", err, want)
	}
}

func TestExtractPolicyHashFromABITuple(t *testing.T) {
	tuple := struct {
		PolicyHash [32]byte
		Epoch      uint64
	}{Epoch: 1}
	for i := range tuple.PolicyHash {
		tuple.PolicyHash[i] = byte(i)
	}
	got := extractFirstBytes32([]any{tuple})
	want := "0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	if got != want {
		t.Fatalf("policy hash = %s, want %s", got, want)
	}
}

func TestJournalSummaryUsesLatestActionStage(t *testing.T) {
	entries := []JournalEntry{
		{ActionID: "a", Stage: StageIntent, EntryHash: "h1"},
		{ActionID: "b", Stage: StageVerified, EntryHash: "h2"},
		{ActionID: "a", Stage: StageVerified, EntryHash: "h3"},
	}
	summary := summarizeJournal(entries)
	if summary.Entries != 3 || summary.LastHash != "h3" || !summary.Actions["a"] || !summary.Actions["b"] || summary.LatestByStage[string(StageVerified)] != 2 {
		t.Fatalf("journal summary = %+v", summary)
	}
}

func TestDecodeHashRejectsZeroLengthAndMalformedValues(t *testing.T) {
	if _, err := decodeHash("0x" + strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "0x01", "0x" + strings.Repeat("zz", 32)} {
		if _, err := decodeHash(value); err == nil {
			t.Fatalf("malformed hash %q accepted", value)
		}
	}
}
