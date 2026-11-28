//go:build linux || darwin

package validator

// Startup and ordinary cuts use the real signed M8 ledger/object fixtures,
// real codecs and actual Save/restart paths. Error observers do not replace
// proof verification, capacity admission or the exact generation transition.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The last real disk visitor changes stats.json without changing the ledger
// directory. Ledger path checks therefore cannot cover this snapshot defect.
func TestStatsSnapshotWriteRealAttachReplayLeafRetargetRefused(t *testing.T) {
	stats, ledger, fixture, dir := newStatsWriteReplayTest(t)
	path := filepath.Join(dir, "stats.json")
	before := statsSnapshotWriteTestRead(t, path)
	disk := ledger.disk
	rows, changes, writes := 0, 0, 0
	ledger.disk = &attemptStatsCallbackBackend{attemptLedgerDiskBackend: disk, walkRecords: func(ctx context.Context, first, last uint64, visit func(AttemptRecord) error) error {
		return disk.Walk(ctx, first, last, func(record AttemptRecord) error {
			rows++
			if err := visit(record); err != nil {
				return err
			}
			if record.Sequence == 8 {
				changes++
				if err := os.Rename(path, path+"-preserved"); err != nil {
					return err
				}
				return os.WriteFile(path, before, 0o600)
			}
			return nil
		})
	}}
	stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		writes++
		return writeStatsSnapshotOwned(directory, write)
	}
	err := stats.AttachAttemptLedger(ledger, dir)
	if rows != 8 || changes != 1 || writes != 0 || err == nil || stats.attemptLedger != nil || stats.attemptLastAppliedSequence != 0 || len(stats.ProviderIDs()) != 0 || !bytes.Equal(statsSnapshotWriteTestRead(t, path), before) {
		t.Fatalf("real attach replay replaced an unowned snapshot: rows=%d changes=%d writes=%d error=%v", rows, changes, writes, err)
	}
	head, headErr := ledger.Head()
	if headErr != nil || head.LastSequence != 8 || head.Root != fixture.recordTs[7].RecordHash {
		t.Fatalf("refusal changed the genuine signed prefix: %v", headErr)
	}
}

// The physical snapshot committed before a late close observer failed. A
// legitimate final Save may restore the live preimage; actual restart must
// still replay the retained ledger exactly once rather than publish half-state.
func TestStatsSnapshotWriteRealAttachCloseFailureSaveAndRestart(t *testing.T) {
	stats, ledger, fixture, dir := newStatsWriteReplayTest(t)
	before := statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json"))
	failure, closed := errors.New("late attach directory close observer"), 0
	stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
		if stage == "directory-closed" {
			closed++
			return failure
		}
		return nil
	}
	err := stats.AttachAttemptLedger(ledger, dir)
	postimage, _ := readStatsWriteSnapshotTest(t, dir)
	if closed != 1 || !errors.Is(err, failure) || stats.attemptLedger != nil || stats.attemptLastAppliedSequence != 0 || postimage.AttemptLastAppliedSequence != 8 {
		t.Fatalf("attach late Close published or hid its actual postimage: closes=%d error=%v", closed, err)
	}
	stats.writeHooks = statsWriteHooks{}
	if err := stats.Save(dir); err != nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) {
		t.Fatalf("legitimate final Save lost its owned preimage: %v", err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	restarted := NewStatsEngine(stats.cfg)
	if err := restarted.Load(dir); err != nil {
		t.Fatal(err)
	}
	if err := restarted.AttachAttemptLedger(reopened, dir); err != nil {
		t.Fatal(err)
	}
	var assignments uint64
	for _, count := range restarted.Exposure() {
		assignments += count
	}
	if assignments != 7 || restarted.attemptLastAppliedSequence != 8 || restarted.attemptLedger != reopened {
		t.Fatalf("actual attach restart refolded or lost signed M8: assignments=%d sequence=%d", assignments, restarted.attemptLastAppliedSequence)
	}
	loaded := NewStatsEngine(stats.cfg)
	if err := loaded.Load(dir); err != nil || !reflect.DeepEqual(loaded.snapshotStats(), restarted.snapshotStats()) {
		t.Fatalf("actual restarted snapshot differs: %v", err)
	}
}

// The full real proof stream closes before the snapshot writer. Same-byte
// leaf replacement is not a new authorised snapshot, even after journaling.
func TestStatsSnapshotWriteRealM8DetachProofCloseRetargetRefused(t *testing.T) {
	fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	path := filepath.Join(fixture.dir, "stats.json")
	before := statsSnapshotWriteTestRead(t, path)
	live := fixture.stats.snapshotStats()
	options := fixture.fresh(t)
	open := options.Stats.Replay.OpenData
	closes, writes := 0, 0
	options.Stats.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &ordinaryInputCommitTestReader{ReadCloser: reader, after: func() error {
			closes++
			if closes != 1 {
				return nil
			}
			if err := os.Rename(path, path+"-preserved"); err != nil {
				return err
			}
			return os.WriteFile(path, before, 0o600)
		}}, nil
	}
	fixture.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		writes++
		return writeStatsSnapshotOwned(directory, write)
	}
	var captured releaseStatsV2RuntimeTestInput
	measurement, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, options, releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured))
	if closes != 1 || writes != 0 || err == nil || cut != nil || !reflect.DeepEqual(measurement, ReleaseStatsMeasurement{}) || captured.Cut.RecordCount != 8 || captured.Cut.CompleteCount != 1 || !reflect.DeepEqual(fixture.stats.snapshotStats(), live) || !fixture.stats.attemptCutPending || !bytes.Equal(statsSnapshotWriteTestRead(t, path), before) {
		t.Fatalf("real M8 proof Close retarget reached snapshot write: closes=%d writes=%d error=%v", closes, writes, err)
	}
}

// This uses the actual journal loader and same-generation reconciliation,
// counting only the proof Close during its retained Stats replay owner.
func TestStatsSnapshotWriteRealM8ReconcileProofCloseRetargetRefused(t *testing.T) {
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	path := filepath.Join(fixture.runtime.dir, "stats.json")
	preimage := statsSnapshotWriteTestRead(t, path)
	input := fixture.detach(t)
	if input.AttemptCutV2.RecordCount != 8 || input.AttemptCutV2.CompleteCount != 1 {
		t.Fatal("genuine M8 reconciliation prerequisite missing")
	}
	if err := os.WriteFile(path, preimage, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.restart(t)
	before := fixture.runtime.stats.snapshotStats()
	options := fixture.fresh(t)
	open := options.Stats.Stats.Replay.OpenData
	closes, writes := 0, 0
	options.Stats.Stats.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &ordinaryInputCommitTestReader{ReadCloser: reader, after: func() error {
			owner := func() *statsWriteOwner {
				fixture.runtime.stats.mu.Lock()
				defer fixture.runtime.stats.mu.Unlock()
				return fixture.runtime.stats.writeOwner
			}()
			if owner == nil || owner.operation != "reconcile-native-v2" {
				return nil
			}
			closes++
			if closes != 1 {
				return nil
			}
			if err := os.Rename(path, path+"-preserved"); err != nil {
				return err
			}
			return os.WriteFile(path, preimage, 0o600)
		}}, nil
	}
	fixture.runtime.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		writes++
		return writeStatsSnapshotOwned(directory, write)
	}
	result, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, options)
	if closes != 1 || writes != 0 || err == nil || !reflect.DeepEqual(result, ReleaseMeasurementInput{}) || !reflect.DeepEqual(fixture.runtime.stats.snapshotStats(), before) || !bytes.Equal(statsSnapshotWriteTestRead(t, path), preimage) {
		t.Fatalf("real M8 reconciliation Close retarget reached snapshot mutation: closes=%d writes=%d error=%v", closes, writes, err)
	}
}

// The ordinary journal closes after the snapshot writer. Its nil-returning
// observer can alter the separate snapshot leaf, so both witnesses must finish
// before the ordinary gate is released or any input is returned.
func TestStatsSnapshotWriteRealM8PostJournalCloseRetargetKeepsGate(t *testing.T) {
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	before := fixture.runtime.stats.snapshotStats()
	path := filepath.Join(fixture.runtime.dir, "stats.json")
	journalParent := filepath.Dir(releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9))
	closed := 0
	result, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2WithReadHooks(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t), releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		if file.Name() != journalParent {
			return nil
		}
		closed++
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			return errors.New("journal observer preceded actual Close")
		}
		postimage := statsSnapshotWriteTestRead(t, path)
		if err := os.Rename(path, path+"-preserved"); err != nil {
			return err
		}
		return os.WriteFile(path, postimage, 0o600)
	}})
	if closed != 1 || err == nil || !reflect.DeepEqual(result, ReleaseMeasurementInput{}) || !reflect.DeepEqual(fixture.runtime.stats.snapshotStats(), before) || !fixture.runtime.stats.attemptCutPending {
		t.Fatalf("post-journal Close changed snapshot without retaining gate: closes=%d error=%v", closed, err)
	}
	postimage, _ := readStatsWriteSnapshotTest(t, fixture.runtime.dir)
	if postimage.EgressGeneration != before.EgressGeneration+1 || postimage.AttemptEgressFirstSequence != 9 {
		t.Fatal("control lacks the real completed snapshot postimage")
	}
}

// After an actual snapshot commit and failed close acknowledgement, the live
// gate stays shut. A real final Save and disk restart recover the immutable M8
// input, without resealing, refolding or changing any existing capacity.
func TestStatsSnapshotWriteRealM8CloseFailureSaveRetryAndRestart(t *testing.T) {
	for _, cancelAtClose := range []bool{false, true} {
		fixture := newReleaseMeasurementInputV2TestFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		path := filepath.Join(fixture.runtime.dir, "stats.json")
		preimage := statsSnapshotWriteTestRead(t, path)
		before := fixture.runtime.stats.snapshotStats()
		failure, closed := errors.New("late compact snapshot directory close observer"), 0
		fixture.runtime.stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
			if stage == "directory-closed" {
				closed++
				if cancelAtClose {
					cancel()
					return errors.Join(context.Canceled, failure)
				}
				return failure
			}
			return nil
		}
		result, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(ctx, 9, 7, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t))
		postimage, _ := readStatsWriteSnapshotTest(t, fixture.runtime.dir)
		if closed != 1 || !errors.Is(err, failure) || errors.Is(err, context.Canceled) != cancelAtClose || (ctx.Err() != nil) != cancelAtClose || !reflect.DeepEqual(result, ReleaseMeasurementInput{}) || !reflect.DeepEqual(fixture.runtime.stats.snapshotStats(), before) || !fixture.runtime.stats.attemptCutPending || postimage.EgressGeneration != before.EgressGeneration+1 {
			t.Fatalf("real M8 late snapshot Close escaped rollback/gate: closes=%d error=%v", closed, err)
		}
		journalPath := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
		original := statsSnapshotWriteTestRead(t, journalPath)
		fixture.runtime.stats.writeHooks = statsWriteHooks{}
		if err := fixture.runtime.stats.Save(fixture.runtime.dir); err != nil || !bytes.Equal(statsSnapshotWriteTestRead(t, path), preimage) {
			t.Fatalf("legitimate final Save did not preserve exact live preimage: %v", err)
		}
		writes, reads := fixture.runtime.objects.writes, fixture.runtime.objects.reads
		fixture.restart(t)
		recovered := fixture.detach(t)
		if recovered.AttemptCutV2 == nil || recovered.AttemptCutV2.RecordCount != 8 || recovered.AttemptCutV2.CompleteCount != 1 || fixture.runtime.stats.egressGeneration != before.EgressGeneration+1 || fixture.runtime.stats.attemptCutPending || fixture.runtime.objects.writes != writes || fixture.runtime.objects.reads <= reads || !bytes.Equal(statsSnapshotWriteTestRead(t, journalPath), original) {
			t.Fatal("real M8 restart after failed snapshot Close lost exact immutable input or replay")
		}
		fixture.restart(t)
		retried := fixture.detach(t)
		if !reflect.DeepEqual(retried, recovered) || fixture.runtime.stats.egressGeneration != before.EgressGeneration+1 || fixture.runtime.objects.writes != writes {
			t.Fatal("second actual restart refolded or resealed the M8 input")
		}
	}
}

// Pure cancellation at real post-write syscall boundaries still publishes
// exactly one rotation from the captured generation, including the final Close.
func TestStatsSnapshotWriteRealM8SuccessfulLatePureCancellationPublishes(t *testing.T) {
	for _, cancelStage := range []string{"temporary-closed", "directory-synced", "directory-closed"} {
		fixture := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
		before := fixture.stats.snapshotStats()
		raw := fixture.stats.currentReleaseStatsMeasurement()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		closed, canceled := 0, 0
		fixture.stats.writeHooks.snapshotIO.after = func(stage string, file *os.File) error {
			if stage == "directory-closed" {
				closed++
			}
			if stage == cancelStage {
				if stage != "directory-synced" {
					if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
						return errors.New("late cancellation observer preceded actual Close")
					}
				}
				canceled++
				cancel()
			}
			return nil
		}
		var captured releaseStatsV2RuntimeTestInput
		measurement, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(ctx, fixture.dir, fixture.source.expected.Boundary, fixture.fresh(t), releaseStatsV2RuntimeTestCapture(filepath.Join(ordinaryInputCommitTestDir(t), "input.json"), &captured))
		after := fixture.stats.snapshotStats()
		if closed != 1 || canceled != 1 || err != nil || ctx.Err() == nil || cut == nil || cut.RecordCount != 8 || cut.CompleteCount != 1 || cut.Context.EgressGeneration != before.EgressGeneration || after.EgressGeneration != before.EgressGeneration+1 || fixture.stats.attemptCutPending {
			t.Fatalf("%s successful late pure cancellation changed the commit boundary: closes=%d cancellations=%d generation=%d/%d pending=%t error=%v", cancelStage, closed, canceled, before.EgressGeneration, after.EgressGeneration, fixture.stats.attemptCutPending, err)
		}
		if !reflect.DeepEqual(measurement, raw) || !reflect.DeepEqual(captured.Stats, raw) || !reflect.DeepEqual(captured.Cut, *cut) || after.AttemptEgressFirstSequence != cut.LastSequence+1 || len(after.Egress) != 0 || after.AttemptLastAppliedSequence != before.AttemptLastAppliedSequence || !reflect.DeepEqual(after.Window, before.Window) || !reflect.DeepEqual(after.EmaPPM, before.EmaPPM) {
			t.Fatalf("%s late cancellation changed the signed input, exact cursor, or retained quality", cancelStage)
		}
		loaded := NewStatsEngine(fixture.stats.cfg)
		if err := loaded.Load(fixture.dir); err != nil || !reflect.DeepEqual(loaded.snapshotStats(), after) {
			t.Fatalf("%s late cancellation split actual disk/live state: %v", cancelStage, err)
		}
	}
}
