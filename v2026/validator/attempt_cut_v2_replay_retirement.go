//go:build linux || darwin

package validator

// A replay scratch child is a fresh private record store. Its owner mints a
// new random name, replay creates the directory, and replay returns only after
// every scratch resource is closed; nothing opens the child again. Under the
// configured replay roots a release run therefore retires each child right
// after its replay, and a slow background sweep removes the children earlier
// processes left behind. Removal needs the same custody the store had: a
// private directory owned by this user, an unchanged inode, an unlocked store
// and only private single-link files. Anything else is left in place.
//
// Cleanup is never worth a crash: scratch left behind only costs space, while
// a validator that exits can miss an epoch. A panic in either path is
// recovered, reported as one bounded line and turns cleanup off for the run.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const (
	// The record store never holds more files than its replay bounds allow.
	replayScratchDefaultMaximumFiles = 4096
	// Creation holds the store lock within microseconds of mkdir; a child
	// unchanged this long and unlocked has no live owner.
	replayScratchStaleAge = 10 * time.Minute
	// The first sweep waits until startup replay and steering own the disk.
	replayScratchSweepDelay = time.Minute
	// At most one batch of children per pause: about 64 a second.
	replayScratchSweepBatch = 64
	replayScratchSweepPause = time.Second
	// Later passes catch children whose own retirement failed.
	replayScratchSweepInterval = 10 * time.Minute
	// A long backlog pass reports progress at this many removals.
	replayScratchSweepProgress = 50000
	replayScratchLogBytes      = 400
)

// The child is not a private flat record store owned by this user.
var errReplayScratchForeign = errors.New("replay scratch child is not a private flat record store owned by this user")

// One owner per release run: the configured replay roots it retires and its
// bounded failure report.
type replayScratchRetirement struct {
	roots    []string
	maxFiles uint64
	stderr   io.Writer
	logged   atomic.Bool
	failures atomic.Uint64
	// Set by the first contained panic; both paths then do nothing more.
	stopped atomic.Bool

	// Ownership seams run at the real removal boundary. Production leaves
	// them unset; they do not replace any custody check.
	beforeRemove func(string)
	sweepDelay   time.Duration
}

type replayScratchRetirementKey struct{}

// Only the configured replay roots retire children. Seal scratch keeps its
// failed bytes for inspection and is not listed here.
func newReplayScratchRetirement(cfg *ReleaseConfig, stderr io.Writer) *replayScratchRetirement {
	if cfg == nil {
		return nil
	}
	self := &replayScratchRetirement{maxFiles: cfg.EvidenceV2.Bounds.Replay.MaxScratchFiles, stderr: stderr}
	if self.maxFiles == 0 {
		self.maxFiles = replayScratchDefaultMaximumFiles
	}
	for _, operator := range cfg.EvidenceV2.Operators {
		root := operator.ReplayScratchRoot
		if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
			continue
		}
		duplicate := false
		for _, existing := range self.roots {
			duplicate = duplicate || existing == root
		}
		if !duplicate {
			self.roots = append(self.roots, root)
		}
	}
	return self
}

// Replays below these roots retire their own children after closing them.
func withReplayScratchRetirement(ctx context.Context, owner *replayScratchRetirement) context.Context {
	if ctx == nil || owner == nil || len(owner.roots) == 0 {
		return ctx
	}
	return context.WithValue(ctx, replayScratchRetirementKey{}, owner)
}

func replayScratchRetirementFrom(ctx context.Context) *replayScratchRetirement {
	if ctx == nil {
		return nil
	}
	owner, _ := ctx.Value(replayScratchRetirementKey{}).(*replayScratchRetirement)
	return owner
}

// The child's parent must be one configured root exactly; a nested seal
// replay or any other caller-chosen directory is never retired here.
func (self *replayScratchRetirement) retires(parent string) bool {
	for _, root := range self.roots {
		if root == parent {
			return true
		}
	}
	return false
}

// Runs after the replay closed its store. A missing marker, a child this
// replay did not create or a removal failure leaves the replay result as is.
func retireAttemptCutV2ReplayScratch(ctx context.Context, path string, created os.FileInfo) {
	owner := replayScratchRetirementFrom(ctx)
	if owner == nil || owner.stopped.Load() || created == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || !owner.retires(filepath.Dir(path)) {
		return
	}
	// A panic below stops here: this runs in the replay's deferred close
	// path, and the replay's result and error are already decided.
	defer owner.contain("retirement")
	// The replay already closed; cancellation must not strand its child.
	removalCtx := context.WithoutCancel(ctx)
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		owner.report(err)
		return
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		owner.report(err)
		return
	}
	defer func() { owner.report(root.Close()) }()
	if owner.beforeRemove != nil {
		owner.beforeRemove(filepath.Base(path))
	}
	_, err = removeReplayScratchChild(removalCtx, root, parent, filepath.Base(path), created, time.Time{}, owner.maxFiles)
	owner.report(err)
}

// Removes one child when it is the exact private, unlocked, flat store that
// anchor names (any such store when anchor is nil) and, with staleBefore set,
// unchanged since then. Busy, fresh and already removed children return false
// without error. The directory stays locked until it is removed.
func removeReplayScratchChild(ctx context.Context, root *os.Root, rootPath, name string, anchor os.FileInfo, staleBefore time.Time, maxFiles uint64) (removed bool, resultErr error) {
	if root == nil || !validAttemptPrivateLeaf(name) {
		return false, errors.New("replay scratch removal needs one leaf under a retained root")
	}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !attemptStorePrivateDirectory(info) || anchor != nil && !os.SameFile(anchor, info) {
		return false, errReplayScratchForeign
	}
	if !staleBefore.IsZero() && !info.ModTime().Before(staleBefore) {
		return false, nil
	}
	// Identity only: removal frees space, so it must not need the write reserve.
	guard, err := openValidatorDurableDirectory(ctx, filepath.Join(rootPath, name), durablevolume.ReadOnly, false)
	if err != nil {
		return false, replayScratchGone(err)
	}
	if guard != nil {
		defer func() { resultErr = errors.Join(resultErr, guard.Close()) }()
	}
	directory, err := root.OpenFile(name, os.O_RDONLY|attemptStoreNoFollowFlag(), 0)
	if err != nil {
		return false, replayScratchGone(err)
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	opened, err := directory.Stat()
	if err != nil || !attemptStorePrivateDirectory(opened) || !os.SameFile(info, opened) {
		return false, errors.Join(errReplayScratchForeign, err)
	}
	if err := checkValidatorDurableDirectory(guard, directory); err != nil {
		return false, err
	}
	// The store holds this lock on its directory inode while it is open.
	if err := attemptStoreLockFile(directory); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return false, nil
		}
		return false, err
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return false, replayScratchGone(err)
	}
	defer func() { resultErr = errors.Join(resultErr, child.Close()) }()
	rooted, err := child.Lstat(".")
	if err != nil || !os.SameFile(info, rooted) {
		return false, errors.Join(errReplayScratchForeign, err)
	}
	names, err := listReplayScratchFiles(directory, child, maxFiles)
	if err != nil {
		return false, err
	}
	for _, file := range names {
		if err := child.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	if err := root.Remove(name); err != nil {
		return false, err
	}
	return true, nil
}

// Another remover holding the lock first leaves nothing to remove here.
func replayScratchGone(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Every entry must be a private single-link regular file. A subdirectory,
// symlink, alias or foreign file refuses the whole child before any removal.
func listReplayScratchFiles(directory *os.File, child *os.Root, maxFiles uint64) ([]string, error) {
	var names []string
	for {
		entries, readErr := directory.ReadDir(64)
		for _, entry := range entries {
			if uint64(len(names)) >= maxFiles {
				return nil, errors.Join(errReplayScratchForeign, errors.New("replay scratch child holds more files than its store allows"))
			}
			info, err := child.Lstat(entry.Name())
			if err != nil {
				return nil, err
			}
			if !validAttemptPrivateLeaf(entry.Name()) || !attemptStorePrivateFile(info) {
				return nil, errReplayScratchForeign
			}
			names = append(names, entry.Name())
		}
		if errors.Is(readErr, io.EOF) {
			return names, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}

// Minted names are "<purpose>-<32 lowercase hex>"; nothing else is swept.
func replayScratchMintedName(name string) bool {
	const nonce = 32
	if len(name) > 255 || len(name) < nonce+2 || !validAttemptPrivateLeaf(name) || name[len(name)-nonce-1] != '-' {
		return false
	}
	for _, c := range name[len(name)-nonce:] {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	for _, c := range name[:len(name)-nonce-1] {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// One pass over a configured root, one batch at a time with a pause between
// batches. Children that are fresh, locked or not minted are skipped; a
// refused child is counted and left in place.
func (self *replayScratchRetirement) sweepRoot(ctx context.Context, configured string, staleBefore time.Time, batch int, pause func(context.Context) error) (removed, refused uint64, resultErr error) {
	if batch <= 0 || pause == nil {
		return 0, 0, errors.New("replay scratch sweep needs a batch and a pause")
	}
	parent, err := filepath.EvalSymlinks(configured)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	guard, err := openValidatorDurableDirectory(ctx, parent, durablevolume.ReadOnly, false)
	if err != nil {
		return 0, 0, err
	}
	if guard != nil {
		defer func() { resultErr = errors.Join(resultErr, guard.Close()) }()
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return 0, 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	directory, err := root.Open(".")
	if err != nil {
		return 0, 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	if err := checkValidatorDurableDirectory(guard, directory); err != nil {
		return 0, 0, err
	}
	for {
		entries, readErr := directory.ReadDir(batch)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return removed, refused, err
			}
			if self.stopped.Load() {
				return removed, refused, nil
			}
			if !replayScratchMintedName(entry.Name()) {
				continue
			}
			if self.beforeRemove != nil {
				self.beforeRemove(entry.Name())
			}
			done, err := removeReplayScratchChild(ctx, root, parent, entry.Name(), nil, staleBefore, self.maxFiles)
			switch {
			case done:
				removed++
				if removed%replayScratchSweepProgress == 0 {
					self.printf("validator: replay scratch sweep removed %d stale children under %s so far\n", removed, configured)
				}
			case errors.Is(err, errReplayScratchForeign):
				refused++
			case err != nil:
				self.report(err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return removed, refused, nil
		}
		if readErr != nil {
			return removed, refused, readErr
		}
		if err := pause(ctx); err != nil {
			return removed, refused, err
		}
	}
}

// Runs passes until ctx ends or a panic turns cleanup off. The returned
// channel closes in either case, so the run's exit never waits on a sweep
// that is gone.
func (self *replayScratchRetirement) start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	if self == nil || len(self.roots) == 0 {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		defer self.contain("sweep")
		delay := self.sweepDelay
		if delay <= 0 {
			delay = replayScratchSweepDelay
		}
		// A summary appears only when a pass removed something or the
		// unverifiable count changed, so a lasting refusal logs once.
		lastRefused := make(map[string]uint64, len(self.roots))
		for {
			if !waitReplayScratchSweep(ctx, delay) || self.stopped.Load() {
				return
			}
			for _, root := range self.roots {
				removed, refused, err := self.sweepRoot(ctx, root, time.Now().Add(-replayScratchStaleAge), replayScratchSweepBatch, func(ctx context.Context) error {
					if !waitReplayScratchSweep(ctx, replayScratchSweepPause) {
						return ctx.Err()
					}
					return nil
				})
				if removed != 0 || refused != lastRefused[root] {
					self.printf("validator: replay scratch sweep removed %d stale children and left %d it could not verify under %s\n", removed, refused, root)
				}
				lastRefused[root] = refused
				if err != nil && ctx.Err() == nil {
					self.report(err)
				}
				if ctx.Err() != nil || self.stopped.Load() {
					return
				}
			}
			delay = replayScratchSweepInterval
		}
	}()
	return done
}

func waitReplayScratchSweep(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Logs the first failure as one bounded line and only counts the rest.
func (self *replayScratchRetirement) report(err error) {
	if self == nil || err == nil {
		return
	}
	self.failures.Add(1)
	if self.logged.Swap(true) {
		return
	}
	self.printf("validator: replay scratch cleanup failed; later failures are counted, not logged: %s\n", replayScratchBounded(err.Error()))
}

// Deferred directly by each cleanup path. It recovers that path's own panic,
// turns cleanup off for the rest of the run and logs the first one as a
// single bounded line: its innermost frames, then the value, never a stack
// dump. It does not recover a panic that began outside the cleanup path.
func (self *replayScratchRetirement) contain(where string) {
	recovered := recover()
	if recovered == nil {
		return
	}
	// Reporting itself must not raise the panic it is containing.
	defer func() { _ = recover() }()
	self.failures.Add(1)
	if self.stopped.Swap(true) {
		return
	}
	self.printf("validator: replay scratch cleanup panicked in %s and is off for the rest of this run: %s\n", where, replayScratchBounded(fmt.Sprintf("%s: %v", replayScratchPanicFrames(), recovered)))
}

// The innermost frames outside the runtime, as "function:line" joined by
// " < ". Called from contain, whose own frame is skipped.
func replayScratchPanicFrames() string {
	var counters [32]uintptr
	frames := runtime.CallersFrames(counters[:runtime.Callers(3, counters[:])])
	var names []string
	for len(names) < 3 {
		frame, more := frames.Next()
		if frame.Function != "" && !strings.HasPrefix(frame.Function, "runtime.") {
			name := frame.Function
			if index := strings.LastIndexByte(name, '/'); index >= 0 {
				name = name[index+1:]
			}
			names = append(names, fmt.Sprintf("%s:%d", name, frame.Line))
		}
		if !more {
			break
		}
	}
	if len(names) == 0 {
		return "unknown frames"
	}
	return strings.Join(names, " < ")
}

// One line of at most replayScratchLogBytes, whatever the cause contains.
func replayScratchBounded(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > replayScratchLogBytes {
		text = strings.ToValidUTF8(text[:replayScratchLogBytes], "") + "..."
	}
	return text
}

func (self *replayScratchRetirement) printf(format string, values ...any) {
	if self.stderr != nil {
		fmt.Fprintf(self.stderr, format, values...)
	}
}
