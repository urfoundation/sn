//go:build linux || darwin

package validator

// Publication at journal removal is a durable-generation boundary, separate
// from write-token admission. Test-owned real files and explicit barriers
// expose both without assuming a blocked writer has actually entered Stats.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture ledgers retain the genuine production constructor and signer.
func newStatsSettlementPublicationTest(t *testing.T) (string, []AttemptSettlementParticipant) {
	t.Helper()
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	second, secondLedger := newAttemptSettlementTestParticipant(t, 2)
	t.Cleanup(func() {
		if err := errors.Join(firstLedger.Close(), secondLedger.Close()); err != nil {
			t.Errorf("close publication fixture ledgers: %v", err)
		}
	})
	return t.TempDir(), []AttemptSettlementParticipant{first, second}
}

// A callback may re-enter public readers. TryLock makes a reintroduced state
// mutex inversion an explicit failure before any potentially blocked reader.
func checkStatsSettlementPublication(participants []AttemptSettlementParticipant, epoch uint64, pending, durable bool) error {
	for _, participant := range participants {
		stats := participant.Stats
		if !stats.mu.TryLock() {
			return errors.New("settlement publication callback retained public Stats.mu")
		}
		valid := stats.settlementEpochKnown && stats.settlementEpoch == epoch && stats.attemptCutPending == pending && stats.writeOwner != nil
		actualEpoch, actualPending := stats.settlementEpoch, stats.attemptCutPending
		stats.mu.Unlock()
		if !valid {
			return fmt.Errorf("public settlement generation differs at callback: epoch=%d pending=%t want=%d/%t", actualEpoch, actualPending, epoch, pending)
		}
		_ = stats.ProviderIDs()
		if stats.requiresSettlementAdvance(epoch) {
			return errors.New("public settlement reader did not observe its published generation")
		}
		if durable {
			publicBytes, err := encodeStatsSnapshot(stats.snapshotStats())
			if err != nil {
				return err
			}
			diskBytes, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
			if err != nil || !bytes.Equal(publicBytes, diskBytes) {
				return errors.Join(errors.New("public generation differs from complete durable snapshots"), err)
			}
		}
	}
	return nil
}

// Fresh advancement exposes the complete durable batch with closed admission
// before calling external removal, including a remover that returns failure.
func TestStatsSettlementPublicationVisibleBeforeRemoval(t *testing.T) {
	root, participants := newStatsSettlementPublicationTest(t)
	stop := errors.New("test-owned publication removal stop")
	removals := 0
	err := advanceAttemptSettlementEpochWithIO(root, 43, attemptLedgerTestBoundary(), participants,
		func(path string, raw []byte) error { return atomicStateWrite(path, raw, 0o600) },
		func(string) error {
			removals++
			if err := checkStatsSettlementPublication(participants, 43, true, true); err != nil {
				t.Error(err)
			}
			raw, err := ReadAttemptSettlementClosure(root, 42)
			if err != nil {
				return err
			}
			closure, err := DecodeAttemptSettlementClosure(raw)
			if err != nil || len(closure.Transitions) != len(participants) {
				return errors.Join(errors.New("removal preceded complete signed closure"), err)
			}
			return stop
		})
	if !errors.Is(err, stop) || removals != 1 {
		t.Fatalf("fresh publication boundary: removals=%d error=%v", removals, err)
	}
	for _, participant := range participants {
		if participant.Stats.settlementEpoch != 43 || !participant.Stats.attemptCutPending || !participant.Stats.attemptSettlementCutPending || participant.Stats.writeOwner != nil {
			t.Fatal("removal failure lost its committed generation or retry reservation")
		}
	}
}

// An all-current retry must expose the same durable generation and closed
// admission at removal; it may not write new snapshots or change signed bytes.
func TestStatsSettlementPublicationRetryVisibleBeforeRemoval(t *testing.T) {
	root, participants := newStatsSettlementPublicationTest(t)
	stop := errors.New("test-owned first removal stop")
	if err := advanceAttemptSettlementEpochWithIO(root, 43, attemptLedgerTestBoundary(), participants,
		func(path string, raw []byte) error { return atomicStateWrite(path, raw, 0o600) },
		func(string) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("establish current-epoch retry: %v", err)
	}
	before, err := ReadAttemptSettlementClosure(root, 42)
	if err != nil {
		t.Fatal(err)
	}
	removals := 0
	err = advanceAttemptSettlementEpochWithIO(root, 43, AttemptBoundary{}, participants,
		func(string, []byte) error { return errors.New("current retry rewrote snapshots") },
		func(path string) error {
			removals++
			if err := checkStatsSettlementPublication(participants, 43, true, true); err != nil {
				return err
			}
			return removeAttemptSettlementTransaction(path)
		})
	if err != nil || removals != 1 {
		t.Fatalf("current retry removal: removals=%d error=%v", removals, err)
	}
	after, err := ReadAttemptSettlementClosure(root, 42)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("retry changed signed closure: %v", err)
	}
	for _, participant := range participants {
		if participant.Stats.attemptCutPending || participant.Stats.attemptSettlementCutPending {
			t.Fatal("successful current retry retained its completed barrier")
		}
	}
}

// No new public epoch appears after only a subset of snapshot writes, or when
// all snapshots exist but immutable closure publication refuses conflicting
// bytes. Those errors retain the old coherent generation and retry barriers.
func TestStatsSettlementPublicationDoesNotExposePartialDurability(t *testing.T) {
	for _, failureAt := range []string{"first-snapshot", "second-snapshot", "closure"} {
		root, participants := newStatsSettlementPublicationTest(t)
		stop := errors.New("test-owned snapshot publication stop")
		writes, removals := 0, 0
		err := advanceAttemptSettlementEpochWithIO(root, 43, attemptLedgerTestBoundary(), participants,
			func(path string, raw []byte) error {
				writes++
				for _, participant := range participants {
					if !participant.Stats.mu.TryLock() {
						return errors.New("snapshot callback retained public Stats.mu")
					}
					old := participant.Stats.settlementEpoch == 42 && participant.Stats.settlementTransition == nil && participant.Stats.writeOwner != nil
					participant.Stats.mu.Unlock()
					if !old {
						return errors.New("partial snapshot publication exposed a new public generation")
					}
					_ = participant.Stats.ProviderIDs()
				}
				if failureAt == "first-snapshot" && writes == 1 || failureAt == "second-snapshot" && writes == 2 {
					return stop
				}
				if err := atomicStateWrite(path, raw, 0o600); err != nil {
					return err
				}
				if failureAt == "closure" && writes == len(participants) {
					closurePath := AttemptSettlementClosurePath(root, 42)
					if err := os.MkdirAll(filepath.Dir(closurePath), 0o700); err != nil {
						return err
					}
					return os.WriteFile(closurePath, []byte("test-owned conflicting closure\n"), 0o400)
				}
				return nil
			}, func(string) error { removals++; return errors.New("partial durability reached journal removal") })
		if err == nil || removals != 0 || failureAt != "closure" && !errors.Is(err, stop) || failureAt == "closure" && !strings.Contains(err.Error(), "closure already exists with different bytes") {
			t.Fatalf("%s wrong durability refusal: writes=%d removals=%d error=%v", failureAt, writes, removals, err)
		}
		for _, participant := range participants {
			if participant.Stats.settlementEpoch != 42 || participant.Stats.settlementTransition != nil || !participant.Stats.attemptCutPending || !participant.Stats.attemptSettlementCutPending || participant.Stats.writeOwner != nil {
				t.Fatalf("%s lost coherent old-generation retry ownership", failureAt)
			}
		}
		if _, err := os.Stat(attemptSettlementTransactionPath(root)); err != nil {
			t.Fatalf("%s lost the recovery journal: %v", failureAt, err)
		}
	}
}

// Begin, ordinary cut, native reconciliation and Save each wait for the same
// real owner. A failed removal then admits only recovery-safe operations;
// public epoch visibility never releases a write token early.
func TestStatsSettlementPublicationQueuesMutatorsThroughRemovalFailure(t *testing.T) {
	root, participants := newStatsSettlementPublicationTest(t)
	first := participants[0]
	stats, ledger := first.Stats, first.Stats.attemptLedger
	waiting := make(chan string, 4)
	stats.writeHooks.step = func(operation, stage string) {
		if stage == "waiting" {
			waiting <- operation
		}
	}
	stop := errors.New("test-owned blocked removal")
	var calls []*statsWriteTestCall
	defer func() {
		for _, call := range calls {
			_ = call.join()
		}
	}()
	err := advanceAttemptSettlementEpochWithIO(root, 43, attemptLedgerTestBoundary(), participants,
		func(path string, raw []byte) error { return atomicStateWrite(path, raw, 0o600) },
		func(string) error {
			if err := checkStatsSettlementPublication(participants, 43, true, true); err != nil {
				t.Error(err)
			}
			for _, operation := range []struct {
				name string
				run  func() error
			}{
				{name: "begin-attempt", run: func() error { return stats.beginAttempt(43, ledger) }},
				{name: "detach-attempt-cut", run: func() error {
					boundary := attemptLedgerTestBoundary()
					boundary.SettlementEpoch = 43
					_, err := stats.detachReleaseStatsMeasurementWithAttemptCut(first.StateDir, boundary, func(ReleaseStatsMeasurement, uint64) error {
						return errors.New("ordinary cut bypassed settlement removal")
					})
					return err
				}},
				{name: "reconcile-native-cut", run: func() error { return stats.reconcileReleaseStatsCut(first.StateDir, 0) }},
				{name: "save", run: func() error { return stats.Save(first.StateDir) }},
			} {
				call := startStatsWriteTestCall(operation.run)
				calls = append(calls, call)
				select {
				case observed := <-waiting:
					if observed != operation.name {
						t.Errorf("queued mutation=%s want=%s", observed, operation.name)
					}
				case <-call.done:
					t.Errorf("%s completed before removal owner released: %v", operation.name, call.join())
				}
			}
			return stop
		})
	if !errors.Is(err, stop) || len(calls) != 4 {
		t.Fatalf("queued publication boundary: calls=%d error=%v", len(calls), err)
	}
	for index, call := range calls {
		err := call.join()
		if index < 3 && !errors.Is(err, errAttemptCutPending) || index == 3 && err != nil {
			t.Errorf("queued operation%d result=%v", index, err)
		}
	}
	if stats.activeAttemptCount != 0 || stats.settlementEpoch != 43 || !stats.attemptCutPending || !stats.attemptSettlementCutPending {
		t.Fatal("queued mutation cleared the failed-removal reservation")
	}
	publicBytes, err := encodeStatsSnapshot(stats.snapshotStats())
	diskBytes, readErr := os.ReadFile(filepath.Join(first.StateDir, "stats.json"))
	if err != nil || readErr != nil || !bytes.Equal(publicBytes, diskBytes) {
		t.Fatalf("queued Save wrote a stale generation: %v/%v", err, readErr)
	}
}

// A coherent old native snapshot is stale after durable generation publication;
// an unchanged finalized refresh waits for ownership without resolving the old
// boundary or clearing the pending removal on cancellation.
func TestStatsSettlementPublicationRefreshSeesCommittedGeneration(t *testing.T) {
	root, participants := newStatsSettlementPublicationTest(t)
	waiting := make(chan struct{}, 1)
	participants[0].Stats.writeHooks.step = func(operation, stage string) {
		if operation == "settlement" && stage == "waiting" {
			waiting <- struct{}{}
		}
	}
	stop := errors.New("test-owned refresh removal stop")
	err := advanceAttemptSettlementEpochWithIO(root, 43, attemptLedgerTestBoundary(), participants,
		func(path string, raw []byte) error { return atomicStateWrite(path, raw, 0o600) },
		func(string) error {
			for _, target := range []uint64{42, 43} {
				ctx, cancel := context.WithCancel(context.Background())
				boundaryCalls := 0
				call := startStatsWriteTestCall(func() error {
					return advanceReleaseSettlementSnapshotWithMode(ctx, root, &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(target)}, participants,
						func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error) {
							boundaryCalls++
							return AttemptBoundary{}, errors.New("refresh resolved an already committed boundary")
						}, false)
				})
				select {
				case <-waiting:
					cancel()
				case <-call.done:
				}
				result := call.join()
				cancel()
				want := errAttemptSettlementSnapshotStale
				if target == 43 {
					want = context.Canceled
				}
				if !errors.Is(result, want) || boundaryCalls != 0 {
					t.Errorf("refresh%d result=%v boundary-calls=%d", target, result, boundaryCalls)
				}
			}
			return stop
		})
	if !errors.Is(err, stop) {
		t.Fatalf("refresh publication boundary: %v", err)
	}
	for _, participant := range participants {
		if participant.Stats.settlementEpoch != 43 || !participant.Stats.attemptCutPending || !participant.Stats.attemptSettlementCutPending {
			t.Fatal("canceled refresh changed removal ownership")
		}
	}
}

// A successful remover releases admission only after it has returned and the
// final flags are published. The queued real attempt may then enter exactly once.
func TestStatsSettlementPublicationSuccessOpensAfterRemoval(t *testing.T) {
	root, participants := newStatsSettlementPublicationTest(t)
	stats, ledger := participants[0].Stats, participants[0].Stats.attemptLedger
	waiting := make(chan struct{})
	stats.writeHooks.step = func(operation, stage string) {
		if operation == "begin-attempt" && stage == "waiting" {
			close(waiting)
		}
	}
	var begin *statsWriteTestCall
	defer func() {
		if begin != nil && begin.join() == nil {
			stats.abortAttempt()
		}
	}()
	err := advanceAttemptSettlementEpochWithIO(root, 43, attemptLedgerTestBoundary(), participants,
		func(path string, raw []byte) error { return atomicStateWrite(path, raw, 0o600) },
		func(path string) error {
			if err := checkStatsSettlementPublication(participants, 43, true, true); err != nil {
				t.Error(err)
			}
			begin = startStatsWriteTestCall(func() error { return stats.beginAttempt(43, ledger) })
			select {
			case <-waiting:
			case <-begin.done:
				t.Errorf("attempt entered before removal returned: %v", begin.join())
			}
			return removeAttemptSettlementTransaction(path)
		})
	if err != nil || begin == nil {
		t.Fatalf("successful removal did not reach queued admission: %v", err)
	}
	if err := begin.join(); err != nil || stats.activeAttemptCount != 1 || stats.attemptCutPending || stats.attemptSettlementCutPending {
		t.Fatalf("completed removal did not admit exactly one attempt: %v", err)
	}
	if _, err := os.Stat(attemptSettlementTransactionPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful removal retained journal: %v", err)
	}
}
