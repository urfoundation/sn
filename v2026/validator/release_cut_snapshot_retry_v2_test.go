//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The live 1404 failure reached the first genuine record of settlement 306
// after a long prior-settlement close. Its finalized block was newer than the
// decision snapshot. A retry must keep every signed row and its reservation,
// then publish once at a fresh boundary without exhausting the failure budget.
func TestReleaseCutSnapshotV2RetainsPrefixAndRetriesFreshSnapshot(t *testing.T) {
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	before := fixture.runtime.stats.snapshotStats()
	head, err := fixture.runtime.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	stale := *fixture.snapshot
	stale.BlockNumber--
	stale.BlockHash = [32]byte{0x71}
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 1404, 9)
	attempts := 0
	var committed ReleaseMeasurementInput
	err = runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) { return 1404, nil }, func() error {
		attempts++
		snapshot := &stale
		if attempts == releaseSteeringFailureLimit+3 {
			snapshot = fixture.snapshot
		}
		input, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 1404, 200, fixture.nativeHash, snapshot, fixture.fresh(t))
		if snapshot == &stale {
			if !releaseOnlyErrors(err, errAttemptCutSnapshotStale) || !reflect.DeepEqual(input, ReleaseMeasurementInput{}) {
				t.Fatalf("stale snapshot admitted an input or lost its typed retry: %+v, %v", input, err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale snapshot published a journal: %v", err)
			}
			if !fixture.runtime.stats.attemptCutPending || fixture.runtime.objects.writes != 0 || !reflect.DeepEqual(fixture.runtime.stats.snapshotStats(), before) {
				t.Fatal("stale snapshot changed counters, uploaded evidence, or lost the cut reservation")
			}
			if err := fixture.runtime.stats.beginAttempt(42, fixture.runtime.ledger); !errors.Is(err, errAttemptCutPending) {
				t.Fatalf("new trail crossed the retained cut reservation: %v", err)
			}
		} else {
			committed = input
		}
		return err
	}, func() bool { return attempts < releaseSteeringFailureLimit+3 })
	if err != nil || attempts != releaseSteeringFailureLimit+3 || committed.AttemptCutV2 == nil || committed.AttemptCutV2.LastSequence != head.LastSequence || committed.AttemptCutV2.Root != head.Root || committed.AttemptCutV2.CompleteCount != 1 || fixture.runtime.stats.attemptCutPending {
		t.Fatalf("fresh retry failed to seal the unchanged genuine M8 prefix: attempts=%d input=%+v err=%v", attempts, committed, err)
	}
	encoded, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	writes := fixture.runtime.objects.writes
	later := *fixture.snapshot
	later.BlockNumber++
	later.BlockHash = [32]byte{0x72}
	reused, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 1404, 200, fixture.nativeHash, &later, fixture.fresh(t))
	if err != nil {
		t.Fatal(err)
	}
	after, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) || !bytes.Equal(encoded, after) || !reflect.DeepEqual(committed, reused) || fixture.runtime.objects.writes != writes || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration+1 {
		t.Fatalf("later operator retry changed an already signed input or repeated its rotation: %v", err)
	}
}

func TestReleaseCutSnapshotV2BoundaryContradictionsRemainFatal(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, variation := range []string{"wrong-settlement", "same-height-hash"} {
		t.Run(variation, func(t *testing.T) {
			expected := fixture.expected
			if variation == "wrong-settlement" {
				expected.Boundary.SettlementEpoch++
				expected.Boundary.EVMBlock--
			} else {
				expected.Boundary.EVMBlockHash = attemptHex32([32]byte{0x73})
			}
			options, _ := newAttemptCutV2SealTestOptions(t, fixture)
			cut, replay, err := SealAttemptCutV2(t.Context(), fixture.ledger, expected, fixture.policy, fixture.key, fixture.bounds, options)
			if err == nil || errors.Is(err, errAttemptCutSnapshotStale) || cut != nil || replay != (AttemptCutV2ReplayResult{}) {
				t.Fatalf("contradictory boundary became a retry or accepted cut: %v", err)
			}
		})
	}
	broken := errors.New("actual owned close failed")
	attempts := 0
	err := runReleaseSteeringLoopWithWait(context.Background(), func() (uint64, error) { return 1404, nil }, func() error {
		attempts++
		return errors.Join(errAttemptCutSnapshotStale, broken)
	}, func() bool { return attempts <= releaseSteeringFailureLimit+1 })
	if !errors.Is(err, broken) || attempts != releaseSteeringFailureLimit {
		t.Fatalf("stale snapshot masked a joined actual failure: attempts=%d err=%v", attempts, err)
	}
}

func TestReleaseCutSnapshotV2WaitKeepsFailureAndEpochGuards(t *testing.T) {
	broken := errors.New("actual native failure")
	attempts := 0
	const stalePolls = 12
	err := runReleaseSteeringLoopWithWait(context.Background(), func() (uint64, error) { return 1404, nil }, func() error {
		attempts++
		if attempts >= releaseSteeringFailureLimit && attempts < releaseSteeringFailureLimit+stalePolls {
			return fmt.Errorf("owned cut: %w", errAttemptCutSnapshotStale)
		}
		return broken
	}, func() bool { return attempts <= releaseSteeringFailureLimit+stalePolls+1 })
	if !errors.Is(err, broken) || attempts != releaseSteeringFailureLimit+stalePolls {
		t.Fatalf("snapshot wait changed prior real failure budget: attempts=%d err=%v", attempts, err)
	}
	reads := 0
	err = runReleaseSteeringLoopWithWait(context.Background(), func() (uint64, error) { reads++; return uint64(1403 + reads), nil }, func() error {
		return errAttemptCutSnapshotStale
	}, func() bool { return reads < 3 })
	if err == nil || !strings.Contains(err.Error(), "incomplete epoch") || reads != 2 {
		t.Fatalf("snapshot wait silently crossed the native epoch: reads=%d err=%v", reads, err)
	}
}
