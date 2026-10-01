//go:build linux || darwin

package validator

// A sealer owns one fresh private directory and two bounded descriptor spools.
// Descriptor-relative access never follows a swapped path. Failure closes all
// owners but preserves staged scratch bytes for the caller's later inspection.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Synchronous ownership seams observe actual I/O; they do not replace record,
// object, signature or lifecycle verification. Production leaves them nil.
type attemptCutV2SealHooks struct {
	HeadCaptured   func(AttemptLedgerHead) error
	ScratchCreated func(string) error
	Step           func(string, string) error
	BeforeClose    func(string, *os.File)
	Closed         func() error
	Replay         attemptCutV2ReplayHooks
}

// All methods are operation-local. The retained directory descriptor prevents
// inode reuse while a hook, object callback or replay is running.
type attemptCutV2SealScratch struct {
	ctx        context.Context
	path       string
	name       string
	parent     *os.Root
	root       *os.Root
	directory  *os.File
	anchor     os.FileInfo
	spools     [2]*attemptCutV2SealSpool
	hooks      attemptCutV2SealHooks
	closed     bool
	closeError error
}

// Creates only the exact fresh leaf, never repairs existing permissions or
// reuses a failed scratch namespace. Existing parents are resolved once.
func newAttemptCutV2SealScratch(ctx context.Context, path string, hooks attemptCutV2SealHooks) (result *attemptCutV2SealScratch, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return nil, errors.New("compact attempt sealer scratch path is not a clean absolute directory")
	}
	parentPath, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		return nil, err
	}
	self := &attemptCutV2SealScratch{ctx: ctx, path: filepath.Join(parentPath, filepath.Base(path)), name: filepath.Base(path), parent: parent, hooks: hooks}
	defer func() {
		if resultErr != nil || result == nil {
			result = nil
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := parent.Mkdir(self.name, 0o700); err != nil {
		return nil, fmt.Errorf("compact attempt sealer scratch must be fresh: %w", err)
	}
	self.anchor, err = parent.Lstat(self.name)
	if err != nil || !attemptStorePrivateDirectory(self.anchor) {
		return nil, errors.Join(errors.New("compact attempt sealer scratch is not private and owned"), err)
	}
	self.directory, err = parent.OpenFile(self.name, os.O_RDONLY|attemptStoreNoFollowFlag(), 0)
	if err != nil {
		return nil, err
	}
	opened, err := self.directory.Stat()
	if err != nil || !attemptStorePrivateDirectory(opened) || !os.SameFile(self.anchor, opened) {
		return nil, errors.Join(errors.New("compact attempt sealer scratch changed during creation"), err)
	}
	if hooks.ScratchCreated != nil {
		if err := hooks.ScratchCreated(self.path); err != nil {
			return nil, err
		}
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	self.root, err = parent.OpenRoot(self.name)
	if err != nil {
		return nil, err
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	return self, nil
}

// Both the caller-visible name and retained roots must still identify the
// owned inode. Renaming a parent cannot authorize writes into its replacement.
func (self *attemptCutV2SealScratch) check() error {
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if self.closed {
		return errors.New("compact attempt sealer scratch is closed")
	}
	current, currentErr := os.Lstat(self.path)
	anchored, anchoredErr := self.parent.Lstat(self.name)
	opened, openedErr := self.directory.Stat()
	if currentErr != nil || anchoredErr != nil || openedErr != nil || !attemptStorePrivateDirectory(current) || !attemptStorePrivateDirectory(anchored) || !attemptStorePrivateDirectory(opened) || !os.SameFile(self.anchor, current) || !os.SameFile(self.anchor, anchored) || !os.SameFile(self.anchor, opened) {
		return errors.New("compact attempt sealer scratch anchor changed")
	}
	if self.root != nil {
		rooted, err := self.root.Lstat(".")
		if err != nil || !attemptStorePrivateDirectory(rooted) || !os.SameFile(self.anchor, rooted) {
			return errors.New("compact attempt sealer descriptor root changed")
		}
	}
	return nil
}

// Hooks run between two actual ownership/cancellation checks, before I/O.
func (self *attemptCutV2SealScratch) step(operation, name string) error {
	if err := self.check(); err != nil {
		return err
	}
	if self.hooks.Step != nil {
		if err := self.hooks.Step(operation, name); err != nil {
			return err
		}
	}
	return self.check()
}

// Fixed typed names are exclusive, private, single-link files. The writer's
// explicit MaxChunks bounds each spool by exactly 104 bytes per descriptor.
func (self *attemptCutV2SealScratch) openSpool(index int, maxChunks uint64) (*attemptCutV2SealSpool, error) {
	if index < 0 || index >= len(self.spools) || self.spools[index] != nil || maxChunks == 0 || maxChunks > uint64(1<<63-1)/attemptStreamV2DescriptorBytes {
		return nil, errors.New("compact attempt descriptor spool request is invalid")
	}
	name := [...]string{"records.descriptors", "proofs.descriptors"}[index]
	if err := self.step("before-spool-open", name); err != nil {
		return nil, err
	}
	file, err := self.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|attemptStoreNoFollowFlag(), 0o600)
	if err != nil {
		return nil, err
	}
	spool := &attemptCutV2SealSpool{owner: self, name: name, file: file, limit: maxChunks * attemptStreamV2DescriptorBytes}
	self.spools[index] = spool
	if err := spool.check(); err != nil {
		return nil, err
	}
	if err := self.step("after-spool-open", name); err != nil {
		return nil, err
	}
	return spool, nil
}

// Close never skips real resource release because of cancellation, a hook
// error or an earlier close error. An idempotent result retains all failures.
func (self *attemptCutV2SealScratch) close() error {
	if self.closed {
		return self.closeError
	}
	var result error
	for _, spool := range self.spools {
		if spool == nil {
			continue
		}
		result = errors.Join(result, spool.check(), spool.file.Sync())
		if self.hooks.BeforeClose != nil {
			self.hooks.BeforeClose(spool.name, spool.file)
		}
		result = errors.Join(result, spool.file.Close())
	}
	if self.directory != nil {
		result = errors.Join(result, self.directory.Close())
	}
	if self.root != nil {
		result = errors.Join(result, self.root.Close())
	}
	if self.parent != nil {
		result = errors.Join(result, self.parent.Close())
	}
	self.closed = true
	if self.hooks.Closed != nil {
		result = errors.Join(result, self.hooks.Closed())
	}
	self.closeError = result
	return result
}

// Position and file size stay within the writer's fixed descriptor allowance.
// The underlying file is never exposed to external object callbacks.
type attemptCutV2SealSpool struct {
	owner    *attemptCutV2SealScratch
	name     string
	file     *os.File
	limit    uint64
	position uint64
}

// Private single-link inode identity is checked around every bounded read,
// write and seek, including the final check before releasing the descriptor.
func (self *attemptCutV2SealSpool) check() error {
	if err := self.owner.check(); err != nil {
		return err
	}
	opened, openedErr := self.file.Stat()
	current, currentErr := self.owner.root.Lstat(self.name)
	if openedErr != nil || currentErr != nil || !attemptStorePrivateFile(opened) || !attemptStorePrivateFile(current) || !os.SameFile(opened, current) || opened.Size() < 0 || uint64(opened.Size()) > self.limit {
		return errors.New("compact attempt descriptor spool inode or bound changed")
	}
	return nil
}

// Forward reads expose only the current bounded descriptor to the writer.
func (self *attemptCutV2SealSpool) Read(data []byte) (int, error) {
	if err := self.owner.step("before-spool-read", self.name); err != nil {
		return 0, err
	}
	if err := self.check(); err != nil {
		return 0, err
	}
	n, err := self.file.Read(data)
	self.position += uint64(n)
	return n, errors.Join(err, self.check())
}

// Refuses an oversized write before changing the private file, then checks
// the same actual descriptor after the write rather than trusting its path.
func (self *attemptCutV2SealSpool) Write(data []byte) (int, error) {
	if err := self.owner.step("before-spool-write", self.name); err != nil {
		return 0, err
	}
	if err := self.check(); err != nil {
		return 0, err
	}
	if self.position > self.limit || uint64(len(data)) > self.limit-self.position {
		return 0, errors.New("compact attempt descriptor spool byte bound exceeded")
	}
	n, err := self.file.Write(data)
	self.position += uint64(n)
	return n, errors.Join(err, self.check())
}

// The codec seeks only absolute positions or the exact file end; other
// relative coordinates cannot enlarge the reserved scratch namespace.
func (self *attemptCutV2SealSpool) Seek(offset int64, whence int) (int64, error) {
	if err := self.owner.step("before-spool-seek", self.name); err != nil {
		return 0, err
	}
	if err := self.check(); err != nil {
		return 0, err
	}
	if whence != io.SeekStart && whence != io.SeekEnd || whence == io.SeekEnd && offset != 0 || offset < 0 || uint64(offset) > self.limit {
		return 0, errors.New("compact attempt descriptor spool seek exceeds its bound")
	}
	position, err := self.file.Seek(offset, whence)
	if err != nil || position < 0 || uint64(position) > self.limit {
		return 0, errors.Join(errors.New("compact attempt descriptor spool returned an invalid position"), err)
	}
	self.position = uint64(position)
	return position, self.check()
}
