//go:build linux || darwin

package validator

// Startup replay must not hold the statistics state lock while entering the
// real disk walk gate. Explicit barriers witness the otherwise circular wait.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Only Pending admission is observed; every successful read and write still
// uses the opened production backend, including its snapshot and walk gate.
type attemptStatsReplayObservedBackend struct {
	attemptLedgerDiskBackend
	pending func(context.Context, func(AttemptRecord) error) error
}

// The observer is installed before either concurrent owner starts.
func (self *attemptStatsReplayObservedBackend) Pending(ctx context.Context, visit func(AttemptRecord) error) error {
	return self.pending(ctx, visit)
}

// A public Walk visitor is allowed to read Stats. Observe both owners at the
// actual Pending boundary, then escape the old inversion and join them before
// asserting the causal failure; no timeout is the correctness witness.
func TestAttemptStatsReplayDoesNotHoldStateLockEnteringDiskPending(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	stats := NewStatsEngine(StatsConfig{})
	stats.settlementEpoch, stats.settlementEpochKnown = 42, true
	stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = 1, 1
	if err := stats.Save(dir); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(dir, "stats.json")
	before, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	walkEntered := make(chan struct{})
	pendingEntered := make(chan struct{})
	probeComplete := make(chan struct{})
	walkDone := make(chan error, 1)
	attachDone := make(chan error, 1)
	visitorComplete := errors.New("public statistics reader completed")
	stateLockAvailable := false
	pendingForwarded := false
	disk := ledger.disk
	ledger.disk = &attemptStatsReplayObservedBackend{
		attemptLedgerDiskBackend: disk,
		pending: func(ctx context.Context, visit func(AttemptRecord) error) error {
			close(pendingEntered)
			<-probeComplete
			if err := ctx.Err(); err != nil {
				return err
			}
			pendingForwarded = true
			return disk.Pending(ctx, visit)
		},
	}
	go func() {
		walkDone <- ledger.Walk(context.Background(), 1, 8, func(record AttemptRecord) error {
			close(walkEntered)
			<-pendingEntered
			stateLockAvailable = stats.mu.TryLock()
			if stateLockAvailable {
				stats.mu.Unlock()
				_ = stats.ProviderIDs()
			} else {
				// The old code owns mu and is about to wait for this visitor's
				// disk walk gate. Cancel it instead of actually deadlocking.
				cancel()
			}
			close(probeComplete)
			return visitorComplete
		})
	}()
	<-walkEntered
	go func() { attachDone <- stats.AttachAttemptLedgerContext(ctx, ledger, dir) }()
	walkErr, attachErr := <-walkDone, <-attachDone
	if !errors.Is(walkErr, visitorComplete) {
		t.Fatalf("public disk visitor did not join at its sentinel: %v", walkErr)
	}
	if !stateLockAvailable {
		if !errors.Is(attachErr, context.Canceled) || pendingForwarded {
			t.Fatalf("inversion cleanup did not cancel before backend forwarding: %v/%t", attachErr, pendingForwarded)
		}
		after, err := os.ReadFile(snapshotPath)
		if err != nil || !bytes.Equal(before, after) || stats.attemptLedger != nil || stats.attemptLastAppliedSequence != 0 {
			t.Fatalf("canceled inversion cleanup changed public or durable state: %v", err)
		}
		t.Fatal("attempt startup held Stats.mu while entering the disk Pending gate needed by a public Stats reader")
	}
	if attachErr != nil || !pendingForwarded {
		t.Fatalf("startup did not forward through the real pending backend: %v/%t", attachErr, pendingForwarded)
	}
	var assignments uint64
	for _, window := range stats.window {
		assignments += window.Assignments
	}
	if assignments != 7 || stats.attemptLedger != ledger || stats.attemptLastAppliedSequence != 8 {
		t.Fatalf("startup did not publish the exact complete replay: %d/%d", assignments, stats.attemptLastAppliedSequence)
	}
	head, err := ledger.Head()
	if err != nil || head.LastSequence != 8 || head.Root != fixture.recordTs[7].RecordHash {
		t.Fatalf("public-reader overlap changed the durable ledger: %+v/%v", head, err)
	}
}
