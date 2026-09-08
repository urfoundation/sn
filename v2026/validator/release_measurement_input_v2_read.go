//go:build linux || darwin

package validator

// Ordinary journals retain a native directory and leaf descriptor while the
// complete bounded bytes are read. The existing private owner-bit/link grammar
// is separate from the terminal journal's stricter leaf admission policy.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Only clean absence at the first observation authorizes immutable creation.
// Late disappearance, cancellation and failed Close never carry this sentinel.
var errReleaseMeasurementInputV2InitiallyMissing = errors.New("compact input journal is initially absent")

// Observers receive only an acquisition phase or an owned descriptor. A late
// failure is injected after the real Close, never instead of that Close.
type releaseMeasurementInputV2ReadHooks struct {
	step       func(operation string, file *os.File, flags int) error
	afterClose func(*os.File) error
}

// Each observer is local to one operation, with no shared hook or state lock.
func (self releaseMeasurementInputV2ReadHooks) observe(operation string, file *os.File, flags int) error {
	if self.step != nil {
		return self.step(operation, file, flags)
	}
	return nil
}

// Always close the actual descriptor before any error-contract observer.
func (self releaseMeasurementInputV2ReadHooks) close(file *os.File) error {
	err := file.Close()
	if self.afterClose != nil {
		err = errors.Join(err, self.afterClose(file))
	}
	return err
}

// The native directory opener joins intermediate Close failures. Only its
// all-ENOENT tree is clean absence; a mixed tree cannot authorize creation.
func releaseMeasurementInputV2OnlyMissing(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !releaseMeasurementInputV2OnlyMissing(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return releaseMeasurementInputV2OnlyMissing(wrapped.Unwrap())
	}
	return err == os.ErrNotExist || err == unix.ENOENT
}

// One operation retains the selected parent through read, admission, Sync and
// the real Stats callback. Its witness is updated only by owned publication.
type releaseMeasurementInputV2Owner struct {
	ctx            context.Context
	path           string
	name           string
	limit          uint64
	directory      *attemptPrivateDirectory
	hooks          releaseMeasurementInputV2ReadHooks
	witness        *attemptPrivateFileState
	observed       bool
	initialMissing bool
	closed         bool
	closeErr       error
}

// Parent creation is explicit and native; existing directories are observed,
// never repaired. An unowned missing parent cannot hide a late occupied leaf.
func acquireReleaseMeasurementInputV2Owner(ctx context.Context, path string, limit uint64, hooks releaseMeasurementInputV2ReadHooks, create bool) (*releaseMeasurementInputV2Owner, error) {
	owner := &releaseMeasurementInputV2Owner{ctx: ctx, path: path, name: filepath.Base(path), limit: limit, hooks: hooks}
	if ctx == nil {
		return owner, errors.New("compact input journal context is nil")
	}
	if err := ctx.Err(); err != nil {
		return owner, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return owner, errors.New("compact input journal path is not canonical absolute non-root")
	}
	if err := hooks.observe("path-admitted", nil, 0); err != nil {
		return owner, err
	}
	if err := ctx.Err(); err != nil {
		return owner, err
	}
	parent := filepath.Dir(path)
	parentBefore, parentErr := os.Lstat(parent)
	if parentErr != nil && !errors.Is(parentErr, os.ErrNotExist) {
		return owner, parentErr
	}
	if parentErr == nil && !parentBefore.IsDir() {
		return owner, errors.New("compact input journal requires a canonical physical ancestor path without symlinks")
	}
	if err := hooks.observe("parent-observed", nil, 0); err != nil {
		return owner, err
	}
	if err := ctx.Err(); err != nil {
		return owner, err
	}
	var directory *attemptPrivateDirectory
	var err error
	if create {
		directory, err = openReleaseMeasurementInputV2Parents(ctx, parent)
	} else {
		directory, err = openAttemptPrivateDirectory(parent)
	}
	if err != nil {
		if !create && parentErr != nil && releaseMeasurementInputV2OnlyMissing(err) {
			owner.initialMissing = true
			return owner, err
		}
		return owner, fmt.Errorf("compact input journal cannot acquire its physical ancestor namespace (supply a canonical path without symlinks): %w", err)
	}
	owner.directory = directory
	if err := hooks.observe("parent-opened", directory.file, 0); err != nil {
		return owner, err
	}
	if err := ctx.Err(); err != nil {
		return owner, err
	}
	if parentErr == nil {
		opened, err := directory.file.Stat()
		if err != nil || !os.SameFile(parentBefore, opened) || parentBefore.Mode() != opened.Mode() {
			return owner, errors.Join(errors.New("compact input journal parent changed before acquisition"), err)
		}
	}
	return owner, directory.check()
}

// Expected absence and an occupied inode are distinct witnesses. A changed
// occupied name can never authorize another same-epoch publication.
func (self *releaseMeasurementInputV2Owner) check() error {
	if self == nil || self.directory == nil {
		return errors.New("compact input journal has no retained parent")
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if err := self.directory.check(); err != nil {
		return err
	}
	return self.checkLeaf(self.directory)
}

// This also checks a final unhooked directory witness after actual Close.
func (self *releaseMeasurementInputV2Owner) checkLeaf(directory *attemptPrivateDirectory) error {
	if !self.observed {
		return nil
	}
	current, err := directory.stat(self.name)
	if self.witness == nil {
		if releaseMeasurementInputV2OnlyMissing(err) {
			return nil
		}
		return errors.Join(errors.New("compact input journal appeared after its initial absence"), err)
	}
	if err != nil || current != *self.witness {
		return errors.Join(errors.New("compact input journal pathname changed after acquisition"), err)
	}
	return nil
}

// Close runs each user-visible callback exactly once. After that callback,
// unhooked native acquisition checks the original parent and leaf, then joins
// its own actual Close. No observer recursion or pathname-following is used.
func (self *releaseMeasurementInputV2Owner) finish() error {
	if self == nil {
		return nil
	}
	if self.closed {
		return self.closeErr
	}
	self.closed = true
	if self.directory == nil {
		return nil
	}
	directory, file := self.directory, self.directory.file
	self.closeErr = directory.close()
	if self.hooks.afterClose != nil {
		self.closeErr = errors.Join(self.closeErr, self.hooks.afterClose(file))
	}
	witness, err := openAttemptPrivateDirectory(directory.path)
	self.closeErr = errors.Join(self.closeErr, err)
	if err == nil {
		if witness.anchor.dev != directory.anchor.dev || witness.anchor.ino != directory.anchor.ino || witness.anchor.mode != directory.anchor.mode || witness.anchor.uid != directory.anchor.uid {
			self.closeErr = errors.Join(self.closeErr, errors.New("compact input parent changed after its actual Close"))
		} else {
			self.closeErr = errors.Join(self.closeErr, self.checkLeaf(witness))
		}
		self.closeErr = errors.Join(self.closeErr, witness.close())
	}
	return self.closeErr
}

// Actual no-follow/nonblocking opens precede native type/identity checks.
// Leaf Close and its observer finish while the original parent is retained.
func (self *releaseMeasurementInputV2Owner) read() (encoded []byte, resultErr error) {
	if err := validateReleaseMeasurementInputV2Limit(self.limit); err != nil {
		return nil, err
	}
	self.initialMissing = false
	if err := self.check(); err != nil {
		return nil, err
	}
	before, err := self.directory.stat(self.name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if checkErr := self.directory.check(); checkErr != nil {
				return nil, errors.Join(err, checkErr)
			}
			self.initialMissing, self.observed, self.witness = true, true, nil
		}
		return nil, err
	}
	self.observed, self.witness = true, &before
	if !before.regular() || before.mode&0o077 != 0 || before.size < 0 || uint64(before.size) > self.limit {
		return nil, errors.New("compact input journal is not a bounded private regular file")
	}
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	if err := self.hooks.observe("leaf-observed", nil, flags); err != nil {
		return nil, err
	}
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	if err := self.directory.check(); err != nil {
		return nil, err
	}
	if err := self.hooks.observe("leaf-opening", nil, flags); err != nil {
		return nil, err
	}
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := self.directory.openFile(self.name, flags, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, self.hooks.close(file), self.check(), self.ctx.Err())
		if resultErr != nil {
			encoded = nil
		}
	}()
	if err := self.hooks.observe("leaf-opened", file, flags); err != nil {
		return nil, err
	}
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	opened, err := statAttemptPrivateFile(file)
	if err != nil || opened != before {
		return nil, errors.Join(errors.New("compact input journal changed before its read"), err)
	}
	encoded, err = io.ReadAll(io.LimitReader(file, int64(self.limit)+1))
	if err != nil {
		return nil, err
	}
	if err := self.hooks.observe("leaf-read", file, flags); err != nil {
		return nil, err
	}
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	if uint64(len(encoded)) > self.limit || int64(len(encoded)) != before.size {
		return nil, errors.New("compact input journal changed its bounded size during read")
	}
	if err := self.hooks.observe("leaf-checked", file, flags); err != nil {
		return nil, err
	}
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	after, err := statAttemptPrivateFile(file)
	if err != nil || after != before {
		return nil, errors.Join(errors.New("compact input journal changed during its read"), err)
	}
	return encoded, self.check()
}

// Initial absence is granted only after the actual parent Close, its final
// native witness, and cancellation all succeed. Late ENOENT stays a hard error.
func readReleaseMeasurementInputV2Context(ctx context.Context, path string, limit uint64, hooks releaseMeasurementInputV2ReadHooks) (encoded []byte, resultErr error) {
	if ctx == nil {
		return nil, errors.New("compact input journal context is nil")
	}
	if err := validateReleaseMeasurementInputV2Limit(limit); err != nil {
		return nil, err
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, limit, hooks, false)
	defer func() {
		resultErr = errors.Join(resultErr, owner.finish(), ctx.Err())
		if owner.initialMissing && releaseMeasurementInputV2OnlyMissing(resultErr) {
			resultErr = errors.Join(errReleaseMeasurementInputV2InitiallyMissing, resultErr)
		}
		if resultErr != nil {
			encoded = nil
		}
	}()
	if err != nil {
		return nil, err
	}
	return owner.read()
}
