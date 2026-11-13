//go:build linux || darwin

package validator

// Each source journal is produced by the actual signed M8 ordinary runtime.
// Filesystem copies below are custody controls, never substitute proof verdicts.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Only provisioning resolves a known temporary directory before authority.
func ordinaryInputCommitTestDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(newAttemptLedgerDiskTestStateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// Preserve the exact source while selecting a fresh physical custody target.
func ordinaryInputCommitTestPath(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(ordinaryInputCommitTestDir(t), "input.json")
	if data != nil {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// Test-only replacement retains the original directory as inspectable evidence.
func ordinaryInputCommitTestReplaceParent(path string) error {
	parent := filepath.Dir(path)
	if err := os.Rename(parent, parent+"-preserved"); err != nil {
		return err
	}
	return os.Mkdir(parent, 0o700)
}

// After the real leaf Close returns nil, neither disappearance nor a same-byte
// replacement may retain the original descriptor's acceptance.
func TestReleaseMeasurementInputV2CommitLeafCloseRenameRefused(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitLeafCloseRenameRefused")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	for _, replace := range []bool{false, true} {
		path := ordinaryInputCommitTestPath(t, original)
		closed := 0
		data, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(original)), releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
			if file.Name() != path {
				return nil
			}
			closed++
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("leaf callback preceded actual Close")
			}
			if err := os.Rename(path, path+"-preserved"); err != nil {
				return err
			}
			if replace {
				return os.WriteFile(path, original, 0o600)
			}
			return nil
		}})
		if closed != 1 || err == nil || data != nil || errors.Is(err, errReleaseMeasurementInputV2InitiallyMissing) {
			t.Fatalf("nil-returning leaf Close retarget escaped: replace=%t closed=%d error=%v", replace, closed, err)
		}
		if !replace && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("late actual disappearance cause lost: %v", err)
		}
		preserved, readErr := os.ReadFile(path + "-preserved")
		if readErr != nil || !bytes.Equal(preserved, original) {
			t.Fatalf("leaf custody lost real bytes: %v", readErr)
		}
	}
}

// The last user-visible parent Close is followed by an unhooked native witness;
// same bytes in a replacement directory and symlink aliases are not that owner.
func TestReleaseMeasurementInputV2CommitParentCloseRetargetRefused(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitParentCloseRetargetRefused")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	for _, alias := range []bool{false, true} {
		path := ordinaryInputCommitTestPath(t, original)
		parent, closed := filepath.Dir(path), 0
		data, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(original)), releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
			if file.Name() != parent {
				return nil
			}
			closed++
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("parent callback preceded actual Close")
			}
			if err := os.Rename(parent, parent+"-preserved"); err != nil {
				return err
			}
			if alias {
				return os.Symlink(filepath.Base(parent)+"-preserved", parent)
			}
			if err := os.Mkdir(parent, 0o700); err != nil {
				return err
			}
			return os.WriteFile(path, original, 0o600)
		}})
		if closed != 1 || err == nil || data != nil {
			t.Fatalf("nil-returning final parent Close retarget escaped: alias=%t closed=%d error=%v", alias, closed, err)
		}
	}
}

// An object appearing after real parent Close cannot inherit the earlier
// missing-name sentinel, even when the callback itself reports success.
func TestReleaseMeasurementInputV2CommitMissingAfterParentCloseIsNotAbsence(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitMissingAfterParentCloseIsNotAbsence")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	path, closed := ordinaryInputCommitTestPath(t, nil), 0
	data, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(original)), releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		closed++
		if file.Name() != filepath.Dir(path) {
			return errors.New("missing journal unexpectedly owned a leaf")
		}
		return os.WriteFile(path, original, 0o600)
	}})
	if closed != 1 || err == nil || data != nil || errors.Is(err, errReleaseMeasurementInputV2InitiallyMissing) {
		t.Fatalf("late occupied name acquired creation authority: closed=%d error=%v", closed, err)
	}
}

// An identical-byte writer must Sync its admitted inode/parent, not a second
// same-byte namespace selected by a successful read's real Close callback.
func TestReleaseMeasurementInputV2CommitExistingWriterCloseRetargetRefused(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitExistingWriterCloseRetargetRefused")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	path, closed := ordinaryInputCommitTestPath(t, original), 0
	err := writeReleaseMeasurementInputV2WithReadHooks(path, original, uint64(len(original)), releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		if file.Name() != path || closed != 0 {
			return nil
		}
		closed++
		if err := ordinaryInputCommitTestReplaceParent(path); err != nil {
			return err
		}
		return os.WriteFile(path, original, 0o600)
	}})
	if err == nil || closed != 1 {
		t.Fatalf("same-byte retry Sync lost read ownership: closed=%d error=%v", closed, err)
	}
}

// A temp Close callback creates a different directory containing an unrelated
// same-named temp. Neither Link nor cleanup may touch that replacement entry.
func TestReleaseMeasurementInputV2CommitTempCloseRetargetCannotPublish(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitTempCloseRetargetCannotPublish")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	path := ordinaryInputCommitTestPath(t, nil)
	var temporary string
	marker := []byte("unrelated temporary is not a signed journal")
	closed := 0
	err := writeReleaseMeasurementInputV2WithReadHooks(path, original, uint64(len(original)), releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		if !strings.HasPrefix(filepath.Base(file.Name()), ".compact-input-") {
			return nil
		}
		closed++
		temporary = filepath.Base(file.Name())
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			return errors.New("temporary callback preceded actual Close")
		}
		if err := ordinaryInputCommitTestReplaceParent(path); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(filepath.Dir(path), temporary), marker, 0o600)
	}})
	if closed != 1 || err == nil {
		t.Fatalf("pathname temp Close redirect published: closed=%d error=%v", closed, err)
	}
	if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("replacement namespace gained a journal: %v", statErr)
	}
	got, readErr := os.ReadFile(filepath.Join(filepath.Dir(path), temporary))
	if readErr != nil || !bytes.Equal(got, marker) {
		t.Fatalf("cleanup removed unrelated replacement temp: %v", readErr)
	}
}

// A changed temp name at the exact Link boundary is rejected before the
// syscall, and its unrelated replacement is never deleted as cleanup.
func TestReleaseMeasurementInputV2CommitLinkBoundaryLeafSwapRefused(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitLinkBoundaryLeafSwapRefused")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	path := ordinaryInputCommitTestPath(t, nil)
	var temporary string
	marker, linked := []byte("unrelated replacement temp"), 0
	err := writeReleaseMeasurementInputV2WithReadHooks(path, original, uint64(len(original)), releaseMeasurementInputV2ReadHooks{step: func(phase string, file *os.File, _ int) error {
		if phase == "temporary-opened" {
			temporary = file.Name()
		}
		if phase != "before-link" {
			return nil
		}
		linked++
		if temporary == "" {
			return errors.New("no real temporary was observed")
		}
		if err := os.Rename(temporary, temporary+"-preserved"); err != nil {
			return err
		}
		return os.WriteFile(temporary, marker, 0o600)
	}})
	if linked != 1 || err == nil {
		t.Fatalf("changed temporary source reached Link: linked=%d error=%v", linked, err)
	}
	if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("changed temp was published: %v", statErr)
	}
	got, readErr := os.ReadFile(temporary)
	if readErr != nil || !bytes.Equal(got, marker) {
		t.Fatalf("writer cleaned an unrelated temp inode: %v", readErr)
	}
}

// The actual no-replace link happens first. A later parent retarget cannot
// redirect temporary removal or the directory durability acknowledgement.
func TestReleaseMeasurementInputV2CommitPostLinkCleanupRetargetRefused(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitPostLinkCleanupRetargetRefused")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	path := ordinaryInputCommitTestPath(t, nil)
	var temporary string
	marker, linked := []byte("unrelated cleanup target"), 0
	err := writeReleaseMeasurementInputV2WithReadHooks(path, original, uint64(len(original)), releaseMeasurementInputV2ReadHooks{step: func(phase string, file *os.File, _ int) error {
		if phase == "temporary-opened" {
			temporary = filepath.Base(file.Name())
		}
		if phase != "journal-linked" {
			return nil
		}
		linked++
		if temporary == "" {
			return errors.New("missing actual linked temp")
		}
		if err := ordinaryInputCommitTestReplaceParent(path); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(filepath.Dir(path), temporary), marker, 0o600)
	}})
	if linked != 1 || err == nil {
		t.Fatalf("post-Link cleanup acknowledged a different parent: linked=%d error=%v", linked, err)
	}
	got, readErr := os.ReadFile(filepath.Join(filepath.Dir(path), temporary))
	if readErr != nil || !bytes.Equal(got, marker) {
		t.Fatalf("path cleanup deleted unrelated entry: %v", readErr)
	}
	preserved, readErr := os.ReadFile(filepath.Join(filepath.Dir(path)+"-preserved", filepath.Base(path)))
	if readErr != nil || !bytes.Equal(preserved, original) {
		t.Fatalf("retained actual link lost its genuine bytes: %v", readErr)
	}
}

// Error-contract controls inject only after real Sync/Close. Context and the
// original cause must both survive, including final parent-owner cleanup.
func TestReleaseMeasurementInputV2CommitRetainsSyncCloseAndCancellation(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitRetainsSyncCloseAndCancellation")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	for _, stage := range []string{"sync", "temporary-close", "parent-close"} {
		path := ordinaryInputCommitTestPath(t, nil)
		ctx, cancel := context.WithCancel(t.Context())
		failure, fired := errors.New("test-owned cause after actual durability operation"), 0
		inject := func() error {
			if fired == 0 {
				fired++
				cancel()
				return failure
			}
			return nil
		}
		err := writeReleaseMeasurementInputV2Context(ctx, path, original, uint64(len(original)), releaseMeasurementInputV2ReadHooks{
			step: func(phase string, _ *os.File, _ int) error {
				if stage == "sync" && phase == "directory-synced" {
					return inject()
				}
				return nil
			},
			afterClose: func(file *os.File) error {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("error observer preceded actual Close")
				}
				if stage == "temporary-close" && strings.HasPrefix(filepath.Base(file.Name()), ".compact-input-") || stage == "parent-close" && file.Name() == filepath.Dir(path) {
					return inject()
				}
				return nil
			},
		})
		cancel()
		if fired != 1 || !errors.Is(err, failure) || !errors.Is(err, context.Canceled) || errors.Is(err, errReleaseMeasurementInputV2InitiallyMissing) {
			t.Fatalf("late %s cause/cancellation lost: fired=%d error=%v", stage, fired, err)
		}
	}
}

// Creation is finite, physical and private. Existing mode/hardlink bytes are
// retained, while non-private parents and FIFO/symlink ancestors are not fixed.
func TestReleaseMeasurementInputV2CommitNativeParentsAndModesRemainExact(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitNativeParentsAndModesRemainExact")
		}
	})
	_, _, original := newReleaseMeasurementInputV2CustodyFixture(t)
	dir := ordinaryInputCommitTestDir(t)
	path := filepath.Join(dir, "measurements", "inputs", "input.json")
	if err := writeReleaseMeasurementInputV2(path, original, uint64(len(original))); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{filepath.Join(dir, "measurements"), filepath.Dir(path)} {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("native creation lost private parent: %v", err)
		}
	}
	for _, mode := range []os.FileMode{0o400, 0o600, 0o700} {
		existing := ordinaryInputCommitTestPath(t, original)
		if err := os.Chmod(existing, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(existing, existing+"-alias"); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(existing)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeReleaseMeasurementInputV2(existing, original, uint64(len(original))); err != nil {
			t.Fatal(err)
		}
		after, err := os.Lstat(existing)
		if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
			t.Fatalf("existing mode/hardlink was repaired: %v", err)
		}
	}
	for _, kind := range []string{"public", "fifo", "alias"} {
		root := ordinaryInputCommitTestDir(t)
		parent := filepath.Join(root, "input-parent")
		if kind == "fifo" {
			if err := unix.Mkfifo(parent, 0o600); err != nil {
				t.Fatal(err)
			}
		} else if kind == "alias" {
			target := filepath.Join(root, "actual")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("actual", parent); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Mkdir(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := writeReleaseMeasurementInputV2(filepath.Join(parent, "input.json"), original, uint64(len(original))); err == nil {
			t.Fatalf("%s parent was followed or silently repaired", kind)
		}
		info, err := os.Lstat(parent)
		if err != nil || kind == "public" && info.Mode().Perm() != 0o755 {
			t.Fatalf("existing %s parent changed mode: %v", kind, err)
		}
	}
}

// An actual M8 terminal survives a failed directory acknowledgement. Real
// same-epoch replay and disk restart finish the exact immutable input once.
func TestReleaseMeasurementInputV2CommitRealRuntimeRetryAndRestart(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitRealRuntimeRetryAndRestart")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	before := fixture.runtime.stats.snapshotStats()
	failure, synced := errors.New("test-owned failure after real input directory Sync"), 0
	input, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2WithReadHooks(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t), releaseMeasurementInputV2ReadHooks{step: func(phase string, _ *os.File, _ int) error {
		if phase == "directory-synced" && synced == 0 {
			synced++
			return failure
		}
		return nil
	}})
	if synced != 1 || !errors.Is(err, failure) || !reflect.DeepEqual(input, ReleaseMeasurementInput{}) || fixture.runtime.stats.egressGeneration != before.EgressGeneration || !fixture.runtime.stats.attemptCutPending {
		t.Fatalf("real failed acknowledgement escaped: synced=%d error=%v", synced, err)
	}
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeReleaseMeasurementInputV2(t.Context(), original, fixture.fresh(t))
	if err != nil || journal.MeasurementInput.AttemptCutV2.RecordCount != 8 || journal.MeasurementInput.AttemptCutV2.CompleteCount != 1 {
		t.Fatalf("retry prerequisite is not real M8: %v", err)
	}
	writes, reads := fixture.runtime.objects.writes, fixture.runtime.objects.reads
	retried := fixture.detach(t)
	if !reflect.DeepEqual(retried, journal.MeasurementInput) || fixture.runtime.stats.egressGeneration != before.EgressGeneration+1 || fixture.runtime.stats.attemptCutPending || fixture.runtime.objects.writes != writes || fixture.runtime.objects.reads <= reads {
		t.Fatal("real retry resealed, failed replay, or rotated incorrectly")
	}
	fixture.restart(t)
	restarted := fixture.detach(t)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, original) || !reflect.DeepEqual(restarted, retried) || fixture.runtime.stats.egressGeneration != before.EgressGeneration+1 {
		t.Fatalf("actual restart lost exact publication: %v", err)
	}
}

// This adapter delegates every byte and actual Close to the real object store.
// The after callback forces a namespace transition, never a replay verdict.
type ordinaryInputCommitTestReader struct {
	io.ReadCloser
	after func() error
}

// Complete reader cleanup precedes the scheduling callback.
func (self *ordinaryInputCommitTestReader) Close() error {
	return errors.Join(self.ReadCloser.Close(), self.after())
}

// A genuine old saved checkpoint and the real published M8 input require an
// actual reconciliation write. A proof callback retargets the journal parent;
// the retained guard must refuse before the first Stats snapshot write.
func TestReleaseMeasurementInputV2CommitReplayRetargetBeforeStatsWrite(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitReplayRetargetBeforeStatsWrite")
		}
	})
	for _, stage := range []string{"proof-close", "before-snapshot"} {
		fixture := newReleaseMeasurementInputV2TestFixture(t)
		statsPath := filepath.Join(fixture.runtime.dir, "stats.json")
		preimage, err := os.ReadFile(statsPath)
		if err != nil {
			t.Fatal(err)
		}
		input := fixture.detach(t)
		if input.AttemptCutV2.RecordCount != 8 || input.AttemptCutV2.CompleteCount != 1 {
			t.Fatal("reconciliation lacks real M8 prerequisite")
		}
		if err := os.WriteFile(statsPath, preimage, 0o600); err != nil {
			t.Fatal(err)
		}
		fixture.restart(t)
		if fixture.runtime.stats.egressGeneration != input.EgressGeneration {
			t.Fatal("saved real preimage does not require reconciliation")
		}
		restored, restoreErr := os.ReadFile(statsPath)
		if restoreErr != nil || !bytes.Equal(restored, preimage) {
			t.Fatalf("actual restart changed the preimage prerequisite: %v", restoreErr)
		}
		before := fixture.runtime.stats.snapshotStats()
		writes, proofCloses := 0, 0
		fixture.runtime.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
			writes++
			return writeStatsSnapshotOwned(directory, write)
		}
		path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
		options := fixture.fresh(t)
		open := options.Stats.Stats.Replay.OpenData
		options.Stats.Stats.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reader, err := open(ctx, kind, hash, size)
			if err != nil {
				return nil, err
			}
			if kind != AttemptStreamV2Proofs {
				return reader, nil
			}
			return &ordinaryInputCommitTestReader{ReadCloser: reader, after: func() error {
				proofCloses++
				if proofCloses == 1 && stage == "proof-close" {
					return ordinaryInputCommitTestReplaceParent(path)
				}
				return nil
			}}, nil
		}
		steps := 0
		if stage == "before-snapshot" {
			fixture.runtime.stats.writeHooks.step = func(operation, phase string) {
				if operation != "reconcile-native-v2" || phase != "before-snapshot" {
					return
				}
				steps++
				if err := ordinaryInputCommitTestReplaceParent(path); err != nil {
					t.Fatal(err)
				}
			}
		}
		result, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, options)
		after := fixture.runtime.stats.snapshotStats()
		disk, readErr := os.ReadFile(statsPath)
		if proofCloses == 0 || stage == "before-snapshot" && steps != 1 || writes != 0 || err == nil || !reflect.DeepEqual(result, ReleaseMeasurementInput{}) || !reflect.DeepEqual(before, after) || readErr != nil || !bytes.Equal(disk, preimage) {
			t.Fatalf("real replay retarget reached snapshot mutation: stage=%s proofs=%d writes=%d error=%v/%v", stage, proofCloses, writes, err, readErr)
		}
	}
}

// Final parent Close occurs after the actual Save but before gate/publication
// release. A nil-returning retarget preserves failed-Save live rollback, while
// the already-written exact postimage remains real restart evidence.
func TestReleaseMeasurementInputV2CommitPostSaveCloseRefusalKeepsGate(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-INPUT-COMMIT-v1 PASS TestReleaseMeasurementInputV2CommitPostSaveCloseRefusalKeepsGate")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	before := fixture.runtime.stats.snapshotStats()
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
	writes, closed := 0, 0
	fixture.runtime.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		writes++
		return writeStatsSnapshotOwned(directory, write)
	}
	result, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2WithReadHooks(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t), releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		if file.Name() != filepath.Dir(path) {
			return nil
		}
		closed++
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			return errors.New("post-Save callback preceded actual Close")
		}
		return ordinaryInputCommitTestReplaceParent(path)
	}})
	after := fixture.runtime.stats.snapshotStats()
	if closed != 1 || writes != 1 || err == nil || !reflect.DeepEqual(result, ReleaseMeasurementInput{}) || after.EgressGeneration != before.EgressGeneration || after.AttemptEgressFirstSequence != before.AttemptEgressFirstSequence || !reflect.DeepEqual(after.Egress, before.Egress) || !fixture.runtime.stats.attemptCutPending {
		t.Fatalf("post-Save Close released publication/gate: closes=%d writes=%d error=%v", closed, writes, err)
	}
	loaded := NewStatsEngine(fixture.runtime.stats.cfg)
	if err := loaded.Load(fixture.runtime.dir); err != nil {
		t.Fatal(err)
	}
	if loaded.egressGeneration != before.EgressGeneration+1 {
		t.Fatal("control did not retain its actual committed postimage")
	}
}
