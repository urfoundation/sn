// Directory replacement is a deterministic stand-in for a namespace changing
// below a mounted state path. It must not turn retained finality into new state.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A held old checkpoint lock must not authorize a new directory with no lock.
func TestMonitorCheckpointReplacementCannotPublishFreshState(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "synthetic-volume")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "checkpoint.json")
	store, err := openMonitorCheckpoint(path, identityExpectation{
		NativeChain: "synthetic-chain", GenesisHash: "0x" + strings.Repeat("21", 32), EvmChainId: mainnetEvmChainId,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state := &monitorState{lastHash: "0x" + strings.Repeat("42", 32), lastNumber: 71,
		lastProgressAt: time.Unix(1700000000, 0).UTC(), lastSuccessAt: time.Unix(1700000000, 0).UTC()}
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	retained := directory + "-retained"
	if err := os.Rename(directory, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	state.lastHash, state.lastNumber = "0x"+strings.Repeat("43", 32), 72
	writeErr := store.save(state)
	entries, listErr := os.ReadDir(directory)
	after, readErr := os.ReadFile(filepath.Join(retained, "checkpoint.json"))
	if writeErr == nil || listErr != nil || len(entries) != 0 || readErr != nil || !bytes.Equal(original, after) {
		t.Fatalf("replacement directory admitted checkpoint publication: write=%v list=%v entries=%d read=%v original_retained=%v", writeErr, listErr, len(entries), readErr, bytes.Equal(original, after))
	}
}
