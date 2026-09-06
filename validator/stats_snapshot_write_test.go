//go:build linux || darwin

package validator

// Direct Save controls force the filesystem seam that matters. Observers run
// after real syscalls; no encoded state, replay result or syscall verdict is
// substituted. Retargeted originals stay available for exact-byte assertions.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"github.com/urnetwork/connect"
)

// Two distinct real counter snapshots make a stale/redirected write visible.
func newStatsSnapshotWriteTest(t *testing.T) (*StatsEngine, string, []byte, []byte) {
	t.Helper()
	dir := ordinaryInputCommitTestDir(t)
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	clientID := connect.NewId()
	stats.RecordAssignment(clientID)
	stats.RecordConfirmation(clientID, 100)
	stats.RecordEgressHash(clientID, iphash(3))
	if err := stats.Save(dir); err != nil { t.Fatal(err) }
	before := statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json"))
	stats.RecordAssignment(clientID)
	after, err := encodeStatsSnapshot(stats.snapshotStats())
	if err != nil || bytes.Equal(before, after) { t.Fatalf("distinct actual counter snapshots: %v", err) }
	return stats, dir, before, after
}

// Do not turn a failed read into an empty-byte success assertion.
func statsSnapshotWriteTestRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	return data
}

// The replacement remains inside the same test-owned ancestor for cleanup.
func statsSnapshotWriteTestRetarget(dir string, data []byte) error {
	if err := os.Rename(dir, dir+"-preserved"); err != nil { return err }
	if err := os.Mkdir(dir, 0o700); err != nil { return err }
	if data != nil { return os.WriteFile(filepath.Join(dir, "stats.json"), data, 0o600) }
	return nil
}

// The snapshot codec and mode remain byte-compatible, including owned copies.
func TestStatsSnapshotWriteExactRoundTripAndUnlockedObservers(t *testing.T) {
	stats, dir, _, expected := newStatsSnapshotWriteTest(t)
	stages := map[string]int{}
	stats.writeHooks.snapshotIO.after = func(stage string, file *os.File) error {
		stages[stage]++
		if !stats.mu.TryLock() { return errors.New("snapshot observer retained Stats.mu") }
		stats.mu.Unlock()
		_ = stats.ProviderIDs()
		if strings.HasSuffix(stage, "-closed") { if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) { return errors.New("observer preceded actual Close") } }
		return nil
	}
	if err := stats.Save(dir); err != nil { t.Fatal(err) }
	if !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), expected) || stages["temporary-closed"] != 1 || stages["directory-closed"] != 1 || stages["directory-synced"] != 1 { t.Fatalf("native snapshot did not retain exact codec/close ownership: %+v", stages) }
	info, err := os.Stat(filepath.Join(dir, "stats.json"))
	if err != nil || info.Mode().Perm() != 0o600 { t.Fatalf("private snapshot mode: %v", err) }
	loaded := NewStatsEngine(stats.cfg)
	if err := loaded.Load(dir); err != nil || !reflect.DeepEqual(loaded.snapshotStats(), stats.snapshotStats()) { t.Fatalf("actual restart differs: %v", err) }
}

// This uses the preexisting before-snapshot callback, not a new syscall seam.
func TestStatsSnapshotWriteBeforeSnapshotParentRetargetRefused(t *testing.T) {
	stats, dir, before, _ := newStatsSnapshotWriteTest(t)
	marker := []byte("unrelated replacement snapshot\n")
	steps := 0
	stats.writeHooks.step = func(operation, stage string) {
		if operation != "save" || stage != "before-snapshot" { return }
		steps++
		if err := statsSnapshotWriteTestRetarget(dir, marker); err != nil { t.Fatal(err) }
	}
	err := stats.Save(dir)
	if steps != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), marker) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir+"-preserved", "stats.json")), before) { t.Fatalf("snapshot pre-write parent retarget escaped retained authority: steps=%d error=%v", steps, err) }
}

// Same bytes at a new inode cannot inherit the prior target observation.
func TestStatsSnapshotWriteBeforeSnapshotLeafReplacementRefused(t *testing.T) {
	stats, dir, before, _ := newStatsSnapshotWriteTest(t)
	path, steps := filepath.Join(dir, "stats.json"), 0
	stats.writeHooks.step = func(operation, stage string) {
		if operation != "save" || stage != "before-snapshot" { return }
		steps++
		if err := os.Rename(path, path+"-preserved"); err != nil { t.Fatal(err) }
		if err := os.WriteFile(path, before, 0o600); err != nil { t.Fatal(err) }
	}
	err := stats.Save(dir)
	if steps != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, path), before) || !bytes.Equal(statsSnapshotWriteTestRead(t, path+"-preserved"), before) { t.Fatalf("same-byte replacement snapshot was overwritten: steps=%d error=%v", steps, err) }
}

// Retarget after actual temporary Close must not publish or clean a foreign
// same-named temporary in the replacement directory.
func TestStatsSnapshotWriteTemporaryCloseRetargetCannotRedirectCleanup(t *testing.T) {
	stats, dir, before, _ := newStatsSnapshotWriteTest(t)
	marker, closed, temporary := []byte("foreign temporary\n"), 0, ""
	stats.writeHooks.snapshotIO.after = func(stage string, file *os.File) error {
		if stage != "temporary-closed" { return nil }
		closed++
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) { return errors.New("temporary callback preceded actual Close") }
		temporary = filepath.Base(file.Name())
		if err := statsSnapshotWriteTestRetarget(dir, before); err != nil { return err }
		return os.WriteFile(filepath.Join(dir, temporary), marker, 0o600)
	}
	err := stats.Save(dir)
	if closed != 1 || err == nil || temporary == "" || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, temporary)), marker) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir+"-preserved", "stats.json")), before) { t.Fatalf("temporary Close retarget redirected publication or cleanup: closed=%d error=%v", closed, err) }
	if _, err := os.Lstat(filepath.Join(dir+"-preserved", temporary)); !errors.Is(err, os.ErrNotExist) { t.Fatalf("owned original temporary was not cleaned: %v", err) }
}

// Exact temporary write state is checked after the last publication observer.
// A foreign regular file, symlink or FIFO is neither renamed nor unlinked.
func TestStatsSnapshotWriteChangedTemporaryCannotPublishOrCleanForeign(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "fifo"} {
		stats, dir, before, _ := newStatsSnapshotWriteTest(t)
		temporary, changes := "", 0
		marker := []byte("foreign temporary inode\n")
		stats.writeHooks.snapshotIO.after = func(stage string, file *os.File) error {
			if stage == "temporary-opened" { temporary = file.Name() }
			if stage != "before-rename" { return nil }
			changes++
			if temporary == "" { return errors.New("actual temporary was not observed") }
			if err := os.Rename(temporary, temporary+"-preserved"); err != nil { return err }
			switch kind {
			case "regular": return os.WriteFile(temporary, marker, 0o600)
			case "symlink": return os.Symlink(filepath.Base(temporary)+"-preserved", temporary)
			default: return unix.Mkfifo(temporary, 0o600)
			}
		}
		err := stats.Save(dir)
		if changes != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) { t.Fatalf("changed temporary reached snapshot rename: kind=%s changes=%d error=%v", kind, changes, err) }
		if _, err := os.Lstat(temporary); err != nil { t.Fatalf("foreign %s temporary was deleted: %v", kind, err) }
		if kind == "regular" && !bytes.Equal(statsSnapshotWriteTestRead(t, temporary), marker) { t.Fatal("foreign regular temporary changed") }
	}
}

// A rename really occurred, but an unrelated directory must not receive its
// durability acknowledgement or be reported as the published snapshot.
func TestStatsSnapshotWritePostRenameRetargetRefused(t *testing.T) {
	stats, dir, before, expected := newStatsSnapshotWriteTest(t)
	changed := 0
	stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
		if stage != "snapshot-renamed" { return nil }
		changed++
		return statsSnapshotWriteTestRetarget(dir, before)
	}
	err := stats.Save(dir)
	if changed != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir+"-preserved", "stats.json")), expected) { t.Fatalf("post-rename parent retarget was acknowledged: changed=%d error=%v", changed, err) }
}

// Sync's observer follows the actual admitted directory syscall.
func TestStatsSnapshotWriteDirectorySyncRetargetRefused(t *testing.T) {
	stats, dir, before, expected := newStatsSnapshotWriteTest(t)
	synced := 0
	stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
		if stage != "directory-synced" { return nil }
		synced++
		return statsSnapshotWriteTestRetarget(dir, before)
	}
	err := stats.Save(dir)
	if synced != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir+"-preserved", "stats.json")), expected) { t.Fatalf("directory Sync retarget escaped final custody: synced=%d error=%v", synced, err) }
}

// A nil-returning observer after real root Close is still a namespace edge.
func TestStatsSnapshotWriteFinalDirectoryCloseRetargetRefused(t *testing.T) {
	stats, dir, before, expected := newStatsSnapshotWriteTest(t)
	closed := 0
	stats.writeHooks.snapshotIO.after = func(stage string, file *os.File) error {
		if stage != "directory-closed" { return nil }
		closed++
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) { return errors.New("directory callback preceded actual Close") }
		return statsSnapshotWriteTestRetarget(dir, before)
	}
	err := stats.Save(dir)
	if closed != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir+"-preserved", "stats.json")), expected) { t.Fatalf("nil-returning final directory Close retarget escaped: closed=%d error=%v", closed, err) }
}

// The final registry includes leaf identity/write state and late absence.
func TestStatsSnapshotWriteFinalCloseLeafReplacementAndAbsenceRefused(t *testing.T) {
	for _, replace := range []bool{false, true} {
		stats, dir, _, expected := newStatsSnapshotWriteTest(t)
		path, closed := filepath.Join(dir, "stats.json"), 0
		stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
			if stage != "directory-closed" { return nil }
			closed++
			if err := os.Rename(path, path+"-preserved"); err != nil { return err }
			if replace { return os.WriteFile(path, expected, 0o600) }
			return nil
		}
		err := stats.Save(dir)
		if closed != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, path+"-preserved"), expected) { t.Fatalf("final Close changed leaf was acknowledged: replace=%t closed=%d error=%v", replace, closed, err) }
		if !replace && !errors.Is(err, os.ErrNotExist) { t.Fatalf("late missing snapshot cause lost: %v", err) }
	}
}

// The last ancestor Close can change a child whose descriptor already closed.
// This is an adjacent all-owned-roots ordering control for finite creation.
func TestStatsSnapshotWriteAllRootsCheckedAfterLastObservableClose(t *testing.T) {
	ancestor := ordinaryInputCommitTestDir(t)
	dir := filepath.Join(ancestor, "first", "second")
	stats := NewStatsEngine(StatsConfig{})
	expected, err := encodeStatsSnapshot(stats.snapshotStats())
	if err != nil { t.Fatal(err) }
	closed, retargeted := 0, 0
	stats.writeHooks.snapshotIO.after = func(stage string, file *os.File) error {
		if stage != "directory-closed" { return nil }
		closed++
		if file.Name() != ancestor { return nil }
		retargeted++
		return statsSnapshotWriteTestRetarget(dir, expected)
	}
	err = stats.Save(dir)
	if closed != 3 || retargeted != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir+"-preserved", "stats.json")), expected) { t.Fatalf("last owner Close changed an earlier closed child unnoticed: closes=%d retargeted=%d error=%v", closed, retargeted, err) }
}

// These injected after-real-syscall errors test joining, not a claim that an
// external filesystem physically returned the injected sentinel.
func TestStatsSnapshotWritePrimaryAndEverySecondaryCloseRemainJoined(t *testing.T) {
	stats, dir, before, _ := newStatsSnapshotWriteTest(t)
	primary, temporaryClose, directoryClose := errors.New("write observer failure"), errors.New("temporary close observer failure"), errors.New("directory close observer failure")
	closed := 0
	stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
		switch stage {
		case "temporary-written": return primary
		case "temporary-closed": closed++; return temporaryClose
		case "directory-closed": closed++; return directoryClose
		default: return nil
		}
	}
	err := stats.Save(dir)
	if closed != 2 || !errors.Is(err, primary) || !errors.Is(err, temporaryClose) || !errors.Is(err, directoryClose) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) { t.Fatalf("snapshot primary/secondary Close causes were dropped: closes=%d error=%v", closed, err) }
}

// Closing the real temporary descriptor early produces a physical write/Close
// failure; the later observer sentinel must remain independently discoverable.
func TestStatsSnapshotWriteActualClosedDescriptorAndCleanupErrorJoined(t *testing.T) {
	stats, dir, before, _ := newStatsSnapshotWriteTest(t)
	secondary, closed := errors.New("physical close cleanup observer"), 0
	stats.writeHooks.snapshotIO.after = func(stage string, file *os.File) error {
		if stage == "temporary-opened" { return file.Close() }
		if stage == "temporary-closed" { closed++; return secondary }
		return nil
	}
	err := stats.Save(dir)
	if closed != 1 || !errors.Is(err, os.ErrClosed) || !errors.Is(err, secondary) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) { t.Fatalf("real closed-descriptor failure lost secondary cleanup cause: closes=%d error=%v", closed, err) }
}

// Cancellation is not permission to discard a real durability/cleanup error.
func TestStatsSnapshotWriteMixedCancellationAndDurabilityRemainJoined(t *testing.T) {
	stats, dir, _, expected := newStatsSnapshotWriteTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	syncFailure, closeFailure := errors.New("directory sync observer failure"), errors.New("directory close observer failure")
	stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
		if stage == "directory-synced" { cancel(); return errors.Join(context.Canceled, syncFailure) }
		if stage == "directory-closed" { return closeFailure }
		return nil
	}
	owner, err := stats.acquireStatsWrite(ctx, "save")
	if err != nil { t.Fatal(err) }
	err = stats.saveOwned(dir, owner.persist)
	owner.release()
	if !errors.Is(err, context.Canceled) || !errors.Is(err, syncFailure) || !errors.Is(err, closeFailure) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), expected) { t.Fatalf("mixed cancellation discarded durable or cleanup cause: %v", err) }
}

// The owned missing plan is read-only until the old final context boundary.
func TestStatsSnapshotWriteCancellationBeforeCreationHasNoMutation(t *testing.T) {
	ancestor := ordinaryInputCommitTestDir(t)
	dir := filepath.Join(ancestor, "missing", "child")
	stats := NewStatsEngine(StatsConfig{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	steps := 0
	stats.writeHooks.step = func(_, stage string) { if stage == "before-snapshot" { steps++; cancel() } }
	owner, err := stats.acquireStatsWrite(ctx, "save")
	if err != nil { t.Fatal(err) }
	err = stats.saveOwned(dir, owner.persist)
	owner.release()
	if steps != 1 || !errors.Is(err, context.Canceled) { t.Fatalf("pre-write cancellation result differs: %d/%v", steps, err) }
	entries, err := os.ReadDir(ancestor)
	if err != nil || len(entries) != 0 { t.Fatalf("canceled snapshot created or repaired state: %v", err) }
}
