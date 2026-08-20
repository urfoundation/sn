package main

import (
	"strings"
	"testing"
)

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
