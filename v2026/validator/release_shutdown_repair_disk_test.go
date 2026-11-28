//go:build linux || darwin

package validator

// Physical-close controls explicitly select the real disk backend. The legacy
// release loader intentionally retains no directory descriptors; it is covered
// separately and must not be reconfigured merely to satisfy this fixture.

import (
	"crypto/ed25519"
	"errors"
	"os"
	"strings"
	"testing"
)

// A deliberately closed real retained descriptor makes the first sorted
// ledger Close fail. Every disk backend and outer owner must still join, and
// the exact completed physical error must remain in the prepared-owner result.
// This proves local descriptor behavior, not an external client-close failure.
func TestReleaseShutdownClosesAllPreparedLedgersDespitePhysicalCloseFailure(t *testing.T) {
	states := make(map[uint64]*releaseAttemptState)
	backends := make(map[uint64]*attemptRecordStore)
	for noID := uint64(1); noID <= 2; noID++ {
		fixture := newReleaseClientSeedContinuityFixture(t)
		legacy := fixture.state.ledger
		if legacy == nil || legacy.disk != nil || legacy.directory != nil {
			t.Fatal("physical-close fixture expected the actual legacy preparation boundary")
		}
		if err := legacy.Close(); err != nil {
			t.Fatal(err)
		}
		identity := legacy.identity
		identity.NoID = noID
		// Choose the disk identity before its first persisted byte. This new
		// private directory never re-tags the continuity fixture's legacy state.
		stateDir := newAttemptLedgerDiskTestStateDir(t)
		ledger, err := NewDiskAttemptLedger(t.Context(), stateDir, identity, strings.ToLower(fixture.cfg.Coordinator), ed25519.NewKeyFromSeed(fixture.seed[:]), attemptLedgerDiskTestLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := ledger.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Errorf("close retained disk test ledger: %v", err)
			}
		})
		backend, ok := ledger.disk.(*attemptRecordStore)
		if !ok || backend == nil || ledger.directory == nil || ledger.directory.directory == nil || ledger.directory.root == nil || ledger.directory.parent == nil {
			t.Fatal("physical-close fixture has no actual retained disk descriptors")
		}
		info, err := ledger.directory.directory.Stat()
		if err != nil || !info.IsDir() {
			t.Fatalf("physical-close fixture directory is not live: %v", err)
		}
		stats := NewStatsEngine(fixture.state.stats.cfg)
		if err := stats.Load(stateDir); err != nil {
			t.Fatal(err)
		}
		if err := stats.AttachAttemptLedgerContext(t.Context(), ledger, stateDir); err != nil {
			t.Fatal(err)
		}
		store, err := NewProofStore(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.ReconcileAttemptProofsContext(t.Context(), ledger); err != nil {
			t.Fatal(err)
		}
		states[noID] = &releaseAttemptState{stats: stats, ledger: ledger, store: store}
		backends[noID] = backend
	}
	retained := states[1].ledger.directory.directory
	retainedPath := retained.Name()
	if err := retained.Close(); err != nil {
		t.Fatal(err)
	}
	err := closeReleaseAttemptStates(states)
	firstCloseErr := states[1].ledger.Close()
	var physicalCloseErr *os.PathError
	if !errors.Is(err, firstCloseErr) || !errors.Is(firstCloseErr, os.ErrClosed) || !errors.As(firstCloseErr, &physicalCloseErr) || physicalCloseErr.Op != "close" || physicalCloseErr.Path != retainedPath {
		t.Fatalf("prepared owner lost the exact physical descriptor Close failure: joined=%v first=%v", err, firstCloseErr)
	}
	if !strings.Contains(err.Error(), "validator no_id 1 attempt ledger shutdown") {
		t.Fatalf("physical Close failure lost its prepared owner identity: %v", err)
	}
	if err := states[2].ledger.Close(); err != nil {
		t.Fatalf("healthy later disk owner did not close successfully: %v", err)
	}
	for noID, state := range states {
		select {
		case <-state.ledger.closeDone:
		default:
			t.Errorf("prepared disk ledger %d was not joined after sibling Close failure", noID)
		}
		select {
		case <-backends[noID].closeDone:
		default:
			t.Errorf("prepared disk backend %d was not joined", noID)
		}
		if state.ledger.directory.directory != nil || state.ledger.directory.root != nil || state.ledger.directory.parent != nil {
			t.Errorf("prepared disk ledger %d retained physical descriptors", noID)
		}
		if _, err := state.ledger.Head(); !errors.Is(err, errAttemptLedgerClosed) {
			t.Errorf("closed prepared disk ledger %d still admitted work: %v", noID, err)
		}
	}
	if repeated := closeReleaseAttemptStates(states); !errors.Is(repeated, firstCloseErr) || states[1].ledger.Close() != firstCloseErr {
		t.Fatalf("repeated prepared close lost its exact completed failure: %v", repeated)
	}
}
