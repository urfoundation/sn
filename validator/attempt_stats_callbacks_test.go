//go:build linux || darwin

package validator

// The same lock rule applies to live commits, native persistence and the
// validator-wide settlement writer. Real backends and explicit probes avoid
// turning a deadlock timeout into the regression's primary evidence.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect"
)

// Successful branches retain the actual production disk operations.
type attemptStatsCallbackBackend struct {
	attemptLedgerDiskBackend
	appendRecord func(context.Context, AttemptRecord) error
	walkRecords  func(context.Context, uint64, uint64, func(AttemptRecord) error) error
}

// Only the admission boundary is observed; no signed record is emulated.
func (self *attemptStatsCallbackBackend) Append(ctx context.Context, record AttemptRecord) error {
	if self.appendRecord != nil {
		return self.appendRecord(ctx, record)
	}
	return self.attemptLedgerDiskBackend.Append(ctx, record)
}

// Observes the projection's second walk while another real visitor owns the
// first walk gate. An old-code branch returns a sentinel for joined cleanup.
func (self *attemptStatsCallbackBackend) Walk(ctx context.Context, first, last uint64, visit func(AttemptRecord) error) error {
	if self.walkRecords != nil {
		return self.walkRecords(ctx, first, last, visit)
	}
	return self.attemptLedgerDiskBackend.Walk(ctx, first, last, visit)
}

// Live terminal commitment must allow the public disk visitor to read Stats
// while the proof projection waits for that visitor's real walk gate.
func TestAttemptStatsCommitProjectionAllowsPublicStatsReader(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	dir := newAttemptLedgerDiskTestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, attemptLedgerLegacyName), attemptLedgerDiskTestJSONL(t, fixture.recordTs[:7]), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	store, err := NewProofStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	stats := NewStatsEngine(StatsConfig{})
	stats.attemptLedger, stats.activeAttemptCount = ledger, 1
	stats.settlementEpoch, stats.settlementEpochKnown = 42, true
	stats.attemptLastAppliedSequence = 7
	stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = 1, 1
	walkEntered, projectionEntered, probeComplete := make(chan struct{}), make(chan struct{}), make(chan struct{})
	walkDone, commitDone := make(chan error, 1), make(chan error, 1)
	visitorComplete := errors.New("public commit reader completed")
	projectionEscape := errors.New("old projection state-lock cleanup")
	stateLockAvailable, projectionForwarded := false, false
	disk := ledger.disk
	ledger.disk = &attemptStatsCallbackBackend{attemptLedgerDiskBackend: disk, walkRecords: func(ctx context.Context, first, last uint64, visit func(AttemptRecord) error) error {
		if last == 8 {
			close(projectionEntered)
			<-probeComplete
			if !stateLockAvailable {
				return projectionEscape
			}
			projectionForwarded = true
		}
		return disk.Walk(ctx, first, last, visit)
	}}
	go func() {
		walkDone <- ledger.Walk(context.Background(), 1, 7, func(AttemptRecord) error {
			close(walkEntered)
			<-projectionEntered
			stateLockAvailable = stats.mu.TryLock()
			if stateLockAvailable {
				stats.mu.Unlock()
				_ = stats.ProviderIDs()
			}
			close(probeComplete)
			return visitorComplete
		})
	}()
	<-walkEntered
	var committed *AttemptRecord
	go func() {
		var err error
		committed, err = stats.commitAttempt(ledger, store, fixture.recordTs[7])
		commitDone <- err
	}()
	walkErr, commitErr := <-walkDone, <-commitDone
	if !errors.Is(walkErr, visitorComplete) || committed == nil || committed.Sequence != 8 || stats.activeAttemptCount != 0 {
		t.Fatalf("live commit owners did not join at exact durable sequence: %v/%v", walkErr, commitErr)
	}
	if !stateLockAvailable {
		if !errors.Is(commitErr, projectionEscape) || projectionForwarded {
			t.Fatalf("projection inversion cleanup was not exact: %v", commitErr)
		}
		t.Fatal("attempt commit held Stats.mu while proof projection waited for a public Stats reader")
	}
	if commitErr != nil || !projectionForwarded || stats.attemptLastAppliedSequence != 8 {
		t.Fatalf("real commit projection did not complete: %v", commitErr)
	}
	var assignments uint64
	for _, count := range stats.Exposure() {
		assignments += count
	}
	if assignments != 7 {
		t.Fatalf("terminal commitment changed exact assignment count: %d", assignments)
	}
}

// The append callback can read Stats at an actual checkpoint without holding
// the shared state mutex; success still appends to the real bounded backend.
func TestAttemptStatsCheckpointDoesNotHoldStateLockEnteringAppend(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	ledger := openAttemptLedgerDiskTest(t, newAttemptLedgerDiskTestStateDir(t), fixture, attemptLedgerDiskHooks{})
	stats := NewStatsEngine(StatsConfig{})
	stats.attemptLedger, stats.activeAttemptCount = ledger, 1
	stats.settlementEpoch, stats.settlementEpochKnown = 42, true
	stateLockAvailable, entered := false, false
	escape := errors.New("old checkpoint state-lock cleanup")
	disk := ledger.disk
	ledger.disk = &attemptStatsCallbackBackend{attemptLedgerDiskBackend: disk, appendRecord: func(ctx context.Context, record AttemptRecord) error {
		entered = true
		stateLockAvailable = stats.mu.TryLock()
		if !stateLockAvailable {
			return escape
		}
		stats.mu.Unlock()
		_ = stats.ProviderIDs()
		return disk.Append(ctx, record)
	}}
	err := stats.checkpointAttempt(ledger, fixture.recordTs[0])
	if !entered {
		t.Fatalf("checkpoint did not reach the real append boundary: %v", err)
	}
	if !stateLockAvailable {
		if !errors.Is(err, escape) {
			t.Fatalf("checkpoint cleanup was not exact: %v", err)
		}
		t.Fatal("attempt checkpoint called the disk append backend while holding Stats.mu")
	}
	if err != nil || stats.attemptLastAppliedSequence != 1 {
		t.Fatalf("checkpoint did not forward its exact record: %v", err)
	}
}

// Native write-ahead persistence can read the public old generation; the
// detached replacement only becomes visible after the callback and save.
func TestAttemptStatsNativePersistenceAllowsStatsReads(t *testing.T) {
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	clientID := connect.NewId()
	stats.RecordAssignment(clientID)
	stats.RecordEgressHash(clientID, [32]byte{1})
	escape := errors.New("old native state-lock cleanup")
	stateLockAvailable := false
	_, err := stats.detachReleaseStatsMeasurement(t.TempDir(), func(ReleaseStatsMeasurement, uint64) error {
		stateLockAvailable = stats.mu.TryLock()
		if !stateLockAvailable {
			return escape
		}
		stats.mu.Unlock()
		if len(stats.EgressIpHashes()) != 1 {
			return errors.New("native callback did not observe the original generation")
		}
		return nil
	})
	if !stateLockAvailable {
		if !errors.Is(err, escape) {
			t.Fatalf("native callback cleanup was not exact: %v", err)
		}
		t.Fatal("native statistics persistence callback ran while holding Stats.mu")
	}
	if err != nil || len(stats.EgressIpHashes()) != 0 || stats.egressGeneration != 1 {
		t.Fatalf("native cut did not publish after persistence: %v", err)
	}
}

// All participant state locks must be absent during external snapshot writes;
// their exclusive write tokens still protect the complete settlement batch.
func TestAttemptStatsSettlementPersistenceAllowsStatsReads(t *testing.T) {
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	second, secondLedger := newAttemptSettlementTestParticipant(t, 2)
	t.Cleanup(func() { _ = firstLedger.Close(); _ = secondLedger.Close() })
	participants := []AttemptSettlementParticipant{first, second}
	escape := errors.New("old settlement state-lock cleanup")
	entered, stateLocksAvailable := false, true
	err := advanceAttemptSettlementEpochWithWrite(t.TempDir(), 43, attemptLedgerTestBoundary(), participants, func(path string, payload []byte) error {
		entered = true
		for _, participant := range participants {
			if !participant.Stats.mu.TryLock() {
				stateLocksAvailable = false
				return escape
			}
			participant.Stats.mu.Unlock()
			_ = participant.Stats.ProviderIDs()
		}
		return atomicStateWrite(path, payload, 0o600)
	})
	if !entered {
		t.Fatalf("settlement did not reach its real snapshot writer: %v", err)
	}
	if !stateLocksAvailable {
		if !errors.Is(err, escape) {
			t.Fatalf("settlement callback cleanup was not exact: %v", err)
		}
		t.Fatal("settlement snapshot callback ran while holding participant Stats.mu")
	}
	if err != nil || first.Stats.settlementEpoch != 43 || second.Stats.settlementEpoch != 43 {
		t.Fatalf("settlement did not publish the complete durable batch: %v", err)
	}
}
