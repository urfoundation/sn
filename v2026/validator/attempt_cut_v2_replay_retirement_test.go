//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Minted children use the runtime's "<purpose>-<32 lowercase hex>" grammar.
func replayScratchTestName(t *testing.T, purpose string) string {
	t.Helper()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return purpose + "-" + hex.EncodeToString(nonce[:])
}

func replayScratchTestOwner(roots ...string) (*replayScratchRetirement, *bytes.Buffer) {
	var log bytes.Buffer
	return &replayScratchRetirement{roots: roots, maxFiles: replayScratchDefaultMaximumFiles, stderr: &log}, &log
}

// A record-store-shaped child: private directory, private flat files, aged.
func replayScratchTestChild(t *testing.T, root, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"LOCK", "CURRENT", "MANIFEST-000000", "000001.log"} {
		if err := os.WriteFile(filepath.Join(path, file), []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	replayScratchTestAge(t, path, age)
	return path
}

func replayScratchTestAge(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func replayScratchTestExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// Both a successful and a failed replay under a retiring root remove the
// child they created, and only after it existed and was closed.
func TestReplayScratchRetiredAfterSuccessfulAndFailedReplay(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	bounds, _ := attemptReplayV2TestBounds()
	for _, failed := range []bool{false, true} {
		replayed := records
		if failed {
			replayed = records[:15]
		}
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, replayed, key, nil, nil)
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		parent := t.TempDir()
		options.ScratchDirectory = filepath.Join(parent, replayScratchTestName(t, "intent-ordinary"))
		existed := false
		options.VisitRecord = func(AttemptRecord) error {
			existed = existed || replayScratchTestExists(t, options.ScratchDirectory)
			return nil
		}
		owner, log := replayScratchTestOwner(parent)
		result, err := ReplayAttemptCutV2(withReplayScratchRetirement(context.Background(), owner), cut, cut.Context, bounds, options)
		if failed && (err == nil || !strings.Contains(err.Error(), "pending lifecycle") || result != (AttemptCutV2ReplayResult{})) {
			t.Fatalf("failed replay = %+v %v", result, err)
		}
		if !failed && (err != nil || result.CompleteCount != 2) {
			t.Fatalf("successful replay = %+v %v", result, err)
		}
		if !existed || replayScratchTestExists(t, options.ScratchDirectory) {
			t.Fatalf("failed=%t: scratch existed during replay=%t, survives after=%t", failed, existed, replayScratchTestExists(t, options.ScratchDirectory))
		}
		if log.Len() != 0 || owner.failures.Load() != 0 {
			t.Fatalf("failed=%t: unexpected cleanup report %q", failed, log.String())
		}
	}
}

// Cancellation ends the replay but never strands the child it created.
func TestReplayScratchRetiredAfterCancellation(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	parent := t.TempDir()
	options.ScratchDirectory = filepath.Join(parent, replayScratchTestName(t, "intent-ordinary"))
	owner, _ := replayScratchTestOwner(parent)
	ctx, cancel := context.WithCancel(withReplayScratchRetirement(context.Background(), owner))
	defer cancel()
	options.VisitRecord = func(AttemptRecord) error {
		cancel()
		return nil
	}
	bounds, _ := attemptReplayV2TestBounds()
	if _, err := ReplayAttemptCutV2(ctx, cut, cut.Context, bounds, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replay error = %v", err)
	}
	if replayScratchTestExists(t, options.ScratchDirectory) {
		t.Fatal("canceled replay stranded its scratch child")
	}
}

// Without the marker, or below another directory such as a sealer's lent
// root, replay keeps its child exactly as before.
func TestReplayScratchKeptOutsideRetiringRoots(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	bounds, _ := attemptReplayV2TestBounds()
	owner, _ := replayScratchTestOwner(t.TempDir())
	for _, ctx := range []context.Context{context.Background(), withReplayScratchRetirement(context.Background(), owner)} {
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		options.ScratchDirectory = filepath.Join(t.TempDir(), replayScratchTestName(t, "intent-ordinary"))
		if _, err := ReplayAttemptCutV2(ctx, cut, cut.Context, bounds, options); err != nil {
			t.Fatal(err)
		}
		if !replayScratchTestExists(t, options.ScratchDirectory) {
			t.Fatal("replay outside a retiring root removed its scratch child")
		}
	}
}

// A directory the replay did not create, already present or swapped in after
// mkdir, is never removed even under a retiring root.
func TestReplayScratchRetirementKeepsDirectoriesItDidNotCreate(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	bounds, _ := attemptReplayV2TestBounds()

	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	parent := t.TempDir()
	options.ScratchDirectory = filepath.Join(parent, replayScratchTestName(t, "intent-ordinary"))
	if err := os.Mkdir(options.ScratchDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(options.ScratchDirectory, "sentinel")
	if err := os.WriteFile(sentinel, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, _ := replayScratchTestOwner(parent)
	if _, err := ReplayAttemptCutV2(withReplayScratchRetirement(context.Background(), owner), cut, cut.Context, bounds, options); err == nil || !strings.Contains(err.Error(), "fresh directory") {
		t.Fatalf("existing scratch error = %v", err)
	}
	if !replayScratchTestExists(t, sentinel) {
		t.Fatal("retirement removed a preexisting scratch directory")
	}

	options = attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	parent = t.TempDir()
	other := filepath.Join(t.TempDir(), "unrelated-private-directory")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	options.ScratchDirectory = filepath.Join(parent, replayScratchTestName(t, "intent-ordinary"))
	hooks := attemptCutV2ReplayHooks{ScratchCreated: func(created string) error {
		if err := os.Rename(created, filepath.Join(parent, "preserved-created-scratch")); err != nil {
			return err
		}
		return os.Rename(other, options.ScratchDirectory)
	}}
	owner, log := replayScratchTestOwner(parent)
	if _, err := replayAttemptCutV2WithHooks(withReplayScratchRetirement(context.Background(), owner), cut, cut.Context, bounds, options, hooks); err == nil {
		t.Fatal("replacement replay succeeded")
	}
	if !replayScratchTestExists(t, options.ScratchDirectory) || !replayScratchTestExists(t, filepath.Join(parent, "preserved-created-scratch")) {
		t.Fatal("retirement removed a replaced or renamed directory")
	}
	if !strings.Contains(log.String(), "replay scratch cleanup failed") {
		t.Fatalf("refused retirement was not reported: %q", log.String())
	}
}

// One pass removes only stale, unlocked, minted, private flat stores; it
// refuses symlinks, foreign modes and unexpected content, and pauses between
// batches.
func TestReplayScratchSweepRemovesOnlyStaleOwnedStores(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	stale := replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), time.Hour)
	fresh := replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), 0)
	notMinted := replayScratchTestChild(t, root, "operator-notes", time.Hour)
	wrongMode := replayScratchTestChild(t, root, replayScratchTestName(t, "history-lineage-current-ordinary"), time.Hour)
	if err := os.Chmod(wrongMode, 0o755); err != nil {
		t.Fatal(err)
	}
	withDirectory := replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), time.Hour)
	if err := os.Mkdir(filepath.Join(withDirectory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	replayScratchTestAge(t, withDirectory, time.Hour)
	outsideFile := filepath.Join(outside, "target")
	if err := os.WriteFile(outsideFile, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withSymlink := replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), time.Hour)
	if err := os.Symlink(outsideFile, filepath.Join(withSymlink, "000002.ldb")); err != nil {
		t.Fatal(err)
	}
	replayScratchTestAge(t, withSymlink, time.Hour)
	outsideStore := replayScratchTestChild(t, outside, "store", time.Hour)
	symlinkChild := filepath.Join(root, replayScratchTestName(t, "intent-ordinary"))
	if err := os.Symlink(outsideStore, symlinkChild); err != nil {
		t.Fatal(err)
	}
	locked := replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), time.Hour)
	holder, err := os.Open(locked)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	owner, log := replayScratchTestOwner(root)
	pauses := 0
	pause := func(context.Context) error { pauses++; return nil }
	removed, refused, err := owner.sweepRoot(context.Background(), root, time.Now().Add(-replayScratchStaleAge), 3, pause)
	if err != nil || removed != 1 || refused != 4 || pauses != 3 {
		t.Fatalf("sweep removed=%d refused=%d pauses=%d err=%v, want 1/4/3", removed, refused, pauses, err)
	}
	if replayScratchTestExists(t, stale) {
		t.Fatal("stale store survived the sweep")
	}
	for _, kept := range []string{fresh, notMinted, wrongMode, withDirectory, withSymlink, symlinkChild, locked, outsideFile, outsideStore, filepath.Join(outsideStore, "CURRENT")} {
		if !replayScratchTestExists(t, kept) {
			t.Fatalf("sweep removed %s", kept)
		}
	}
	if info, err := os.Lstat(symlinkChild); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlinked child changed: %v", err)
	}
	if log.Len() != 0 {
		t.Fatalf("refusals are counted, not logged per child: %q", log.String())
	}

	// Once its holder closes, the formerly locked store goes on the next pass.
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	removed, _, err = owner.sweepRoot(context.Background(), root, time.Now().Add(-replayScratchStaleAge), 3, pause)
	if err != nil || removed != 1 || replayScratchTestExists(t, locked) {
		t.Fatalf("second pass removed=%d err=%v locked survives=%t", removed, err, replayScratchTestExists(t, locked))
	}
}

// The pass stops at its pause: one batch, then the ended context.
func TestReplayScratchSweepRespectsItsBudget(t *testing.T) {
	root := t.TempDir()
	var children []string
	for index := 0; index < 5; index++ {
		children = append(children, replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), time.Hour))
	}
	owner, _ := replayScratchTestOwner(root)
	removed, _, err := owner.sweepRoot(context.Background(), root, time.Now().Add(-replayScratchStaleAge), 2, func(context.Context) error { return context.Canceled })
	if !errors.Is(err, context.Canceled) || removed != 2 {
		t.Fatalf("budgeted pass removed=%d err=%v, want 2 and canceled", removed, err)
	}
	survivors := 0
	for _, child := range children {
		if replayScratchTestExists(t, child) {
			survivors++
		}
	}
	if survivors != 3 {
		t.Fatalf("budgeted pass left %d children, want 3", survivors)
	}
}

// Racing removers, such as a sweep and a replay's own retirement, remove each
// child exactly once and none of them reports a failure.
func TestReplayScratchConcurrentRemoversAgree(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for index := 0; index < 24; index++ {
		name := replayScratchTestName(t, "intent-ordinary")
		replayScratchTestChild(t, root, name, time.Hour)
		names = append(names, name)
	}
	var removed atomic.Uint64
	var failures atomic.Value
	var workers sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			handle, err := os.OpenRoot(resolved)
			if err != nil {
				failures.Store(err)
				return
			}
			defer handle.Close()
			for _, name := range names {
				done, err := removeReplayScratchChild(context.Background(), handle, resolved, name, nil, time.Time{}, replayScratchDefaultMaximumFiles)
				if err != nil {
					failures.Store(err)
				}
				if done {
					removed.Add(1)
				}
			}
		}()
	}
	workers.Wait()
	if err, _ := failures.Load().(error); err != nil || removed.Load() != uint64(len(names)) {
		t.Fatalf("concurrent removers removed %d of %d: %v", removed.Load(), len(names), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("children left after concurrent removal: %d %v", len(entries), err)
	}
}

// The sweep relies on the store's own custody predicate, which refuses a
// directory or file owned by another user even with private modes.
func TestReplayScratchCustodyRefusesForeignOwners(t *testing.T) {
	root := t.TempDir()
	path := replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), time.Hour)
	directory, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Lstat(filepath.Join(path, "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	if !attemptStorePrivateDirectory(directory) || !attemptStorePrivateFile(file) {
		t.Fatal("own private store refused")
	}
	if attemptStorePrivateDirectory(replayScratchTestForeign{directory}) || attemptStorePrivateFile(replayScratchTestForeign{file}) {
		t.Fatal("foreign-owned store admitted")
	}
}

// Reports another user's uid for an otherwise unchanged observation.
type replayScratchTestForeign struct{ os.FileInfo }

func (self replayScratchTestForeign) Sys() any {
	stat := *self.FileInfo.Sys().(*syscall.Stat_t)
	stat.Uid++
	return &stat
}

func TestReplayScratchMintedNames(t *testing.T) {
	for _, name := range []string{"intent-ordinary-0123456789abcdef0123456789abcdef", "startup-history-lineage-current-ordinary-ffffffffffffffffffffffffffffffff", "a-00000000000000000000000000000000"} {
		if !replayScratchMintedName(name) {
			t.Fatalf("minted name refused: %s", name)
		}
	}
	for _, name := range []string{"", "operator-notes", "intent-ordinary-0123456789ABCDEF0123456789abcdef", "intent-ordinary-0123456789abcdef0123456789abcde", "-0123456789abcdef0123456789abcdef", "../intent-0123456789abcdef0123456789abcdef", "intent ordinary-0123456789abcdef0123456789abcdef", "seal"} {
		if replayScratchMintedName(name) {
			t.Fatalf("unminted name admitted: %q", name)
		}
	}
}

// One contained-panic line: where it happened, bounded, with no stack dump.
func replayScratchTestPanicLine(t *testing.T, log *bytes.Buffer, where, frame, value string) {
	t.Helper()
	t.Logf("contained %s panic line: %s", where, strings.TrimSuffix(log.String(), "\n"))
	lines := strings.Split(strings.TrimSuffix(log.String(), "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "panicked in "+where) || !strings.Contains(lines[0], frame) || !strings.Contains(lines[0], value) || strings.Contains(log.String(), "goroutine ") || len(lines[0]) > replayScratchLogBytes+200 {
		t.Fatalf("contained panic report: %d lines %q", len(lines), log.String())
	}
}

// A panic inside the sweep ends only the sweep: its channel closes, the run's
// context stays live, nothing is removed, cleanup turns itself off and one
// bounded line is logged.
func TestReplayScratchSweepPanicIsContained(t *testing.T) {
	root := t.TempDir()
	children := []string{
		replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), time.Hour),
		replayScratchTestChild(t, root, replayScratchTestName(t, "intent-ordinary"), time.Hour),
	}
	owner, log := replayScratchTestOwner(root)
	owner.sweepDelay = time.Millisecond
	hooked := 0
	owner.beforeRemove = func(string) {
		hooked++
		panic(strings.Repeat("injected sweep failure\n", 400))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := owner.start(ctx)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("sweep did not end after its panic")
	}
	if ctx.Err() != nil || hooked != 1 || !owner.stopped.Load() {
		t.Fatalf("contained sweep: run context=%v hook calls=%d stopped=%t", ctx.Err(), hooked, owner.stopped.Load())
	}
	for _, child := range children {
		if !replayScratchTestExists(t, child) {
			t.Fatal("sweep removed a child after its panic")
		}
	}
	replayScratchTestPanicLine(t, log, "sweep", "sweepRoot", "injected sweep failure")

	// Cleanup stays off: a later sweep and a later retirement do nothing.
	select {
	case <-owner.start(ctx):
	case <-time.After(30 * time.Second):
		t.Fatal("stopped sweep did not end at once")
	}
	if hooked != 1 || strings.Count(log.String(), "\n") != 1 {
		t.Fatalf("stopped cleanup ran again: hook calls=%d log=%q", hooked, log.String())
	}
}

// A panic inside a replay's own retirement never reaches the replay: no
// panic escapes, its result and error stand, the child stays and one bounded
// line is logged. Later replays under the same run are not retired.
func TestReplayScratchRetirementPanicLeavesReplayResult(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	bounds, _ := attemptReplayV2TestBounds()
	for _, failed := range []bool{false, true} {
		replayed := records
		if failed {
			replayed = records[:15]
		}
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, replayed, key, nil, nil)
		want, wantErr := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
		if failed != (wantErr != nil) {
			t.Fatalf("baseline replay failed=%t: %v", failed, wantErr)
		}
		parent := t.TempDir()
		owner, log := replayScratchTestOwner(parent)
		hooked := 0
		owner.beforeRemove = func(string) {
			hooked++
			panic(errors.New("injected retirement failure"))
		}
		ctx := withReplayScratchRetirement(context.Background(), owner)
		replay := func() (result AttemptCutV2ReplayResult, err error, escaped any) {
			options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
			options.ScratchDirectory = filepath.Join(parent, replayScratchTestName(t, "intent-ordinary"))
			defer func() {
				escaped = recover()
				if escaped == nil && !replayScratchTestExists(t, options.ScratchDirectory) {
					t.Fatal("a panicking or stopped retirement removed the child")
				}
			}()
			result, err = ReplayAttemptCutV2(ctx, cut, cut.Context, bounds, options)
			return result, err, nil
		}
		got, err, escaped := replay()
		if escaped != nil || got != want || (err == nil) != (wantErr == nil) || err != nil && err.Error() != wantErr.Error() {
			t.Fatalf("failed=%t: escaped=%v result=%+v want %+v error=%v want %v", failed, escaped, got, want, err, wantErr)
		}
		if hooked != 1 || !owner.stopped.Load() {
			t.Fatalf("failed=%t: hook calls=%d stopped=%t", failed, hooked, owner.stopped.Load())
		}
		replayScratchTestPanicLine(t, log, "retirement", "retireAttemptCutV2ReplayScratch", "injected retirement failure")

		got, err, escaped = replay()
		if escaped != nil || got != want || (err == nil) != (wantErr == nil) || hooked != 1 || strings.Count(log.String(), "\n") != 1 {
			t.Fatalf("failed=%t after stop: escaped=%v error=%v hook calls=%d log=%q", failed, escaped, err, hooked, log.String())
		}
	}
}

// Containment covers cleanup only. A panic that begins in the replay itself
// still propagates, and the replay's child is retired on the way out.
func TestReplayScratchContainmentLeavesReplayPanicsAlone(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	parent := t.TempDir()
	options.ScratchDirectory = filepath.Join(parent, replayScratchTestName(t, "intent-ordinary"))
	options.VisitRecord = func(AttemptRecord) error { panic("visitor failure outside cleanup") }
	owner, log := replayScratchTestOwner(parent)
	bounds, _ := attemptReplayV2TestBounds()
	var escaped any
	func() {
		defer func() { escaped = recover() }()
		_, _ = ReplayAttemptCutV2(withReplayScratchRetirement(context.Background(), owner), cut, cut.Context, bounds, options)
	}()
	if escaped != "visitor failure outside cleanup" {
		t.Fatalf("replay panic was swallowed or changed: %v", escaped)
	}
	if owner.stopped.Load() || log.Len() != 0 || replayScratchTestExists(t, options.ScratchDirectory) {
		t.Fatalf("replay panic: cleanup stopped=%t log=%q child survives=%t", owner.stopped.Load(), log.String(), replayScratchTestExists(t, options.ScratchDirectory))
	}
}

// The first failure is one bounded line; later ones are only counted.
func TestReplayScratchReportsFailuresOnce(t *testing.T) {
	owner, log := replayScratchTestOwner(t.TempDir())
	owner.report(errors.New(strings.Repeat("x", 2*replayScratchLogBytes) + "\nsecond line"))
	owner.report(errors.New("second distinct cause"))
	lines := strings.Split(strings.TrimSuffix(log.String(), "\n"), "\n")
	if len(lines) != 1 || owner.failures.Load() != 2 || len(lines[0]) > replayScratchLogBytes+120 || strings.Contains(log.String(), "second distinct cause") {
		t.Fatalf("report lines=%d failures=%d: %q", len(lines), owner.failures.Load(), log.String())
	}
}
