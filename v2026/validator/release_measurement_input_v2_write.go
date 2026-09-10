//go:build linux || darwin

package validator

// Immutable ordinary input publication keeps its native parent through every
// temporary write, no-replace link, cleanup, directory Sync and actual Close.
// Existing leaf modes and stable hardlink observations retain their grammar.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Find an existing physical non-root ancestor, then create only the finite
// missing suffix through retained descriptors. Existing directories are never
// chmod-ed; a symlink/FIFO/unknown error cannot become permission to create.
func openReleaseMeasurementInputV2Parents(ctx context.Context, path string) (*attemptPrivateDirectory, error) {
	candidate := path
	var missing []string
	var parent *attemptPrivateDirectory
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if filepath.Dir(candidate) == candidate {
			return nil, errors.New("compact input creation requires an existing physical non-root ancestor")
		}
		opened, err := openAttemptPrivateDirectory(candidate)
		if err == nil {
			parent = opened
			break
		}
		if !releaseMeasurementInputV2OnlyMissing(err) {
			return nil, err
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = filepath.Dir(candidate)
	}
	for index := len(missing) - 1; index >= 0; index-- {
		if err := errors.Join(ctx.Err(), parent.check()); err != nil {
			return nil, errors.Join(err, parent.close())
		}
		name := missing[index]
		createErr := unix.Mkdirat(int(parent.file.Fd()), name, 0o700)
		if createErr != nil && !errors.Is(createErr, os.ErrExist) {
			return nil, errors.Join(createErr, parent.close())
		}
		before, err := parent.stat(name)
		if err != nil || !before.directory() || before.mode&0o077 != 0 || before.uid != uint32(os.Geteuid()) {
			return nil, errors.Join(errors.New("compact input creation encountered a non-private directory"), err, parent.close())
		}
		file, err := parent.openFile(name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			return nil, errors.Join(err, parent.close())
		}
		state, err := statAttemptPrivateFile(file)
		if err != nil || state.dev != before.dev || state.ino != before.ino || state.mode != before.mode || state.uid != before.uid {
			return nil, errors.Join(errors.New("compact input directory changed during creation"), err, file.Close(), parent.close())
		}
		child := &attemptPrivateDirectory{file: file, path: filepath.Join(parent.path, name), anchor: state}
		var syncErr error
		if createErr == nil {
			syncErr = parent.file.Sync()
		}
		err = errors.Join(syncErr, parent.check(), child.check(), parent.close(), ctx.Err())
		if err != nil {
			return nil, errors.Join(err, child.close())
		}
		parent = child
	}
	return parent, nil
}

// Sync belongs to the same checked journal inode and parent as its read.
// The observer follows the real syscall; late errors/cancellation stay joined.
func (self *releaseMeasurementInputV2Owner) sync() error {
	if err := self.check(); err != nil {
		return err
	}
	syncErr := self.directory.file.Sync()
	observeErr := self.hooks.observe("directory-synced", self.directory.file, 0)
	return errors.Join(syncErr, observeErr, self.check(), self.ctx.Err())
}

// Cleanup may unlink only this operation's exact temporary inode. A replaced
// temporary name is evidence, not permission to delete an unrelated file.
func (self *releaseMeasurementInputV2Owner) removeTemporary(name string, identity attemptPrivateFileState) error {
	state, err := self.directory.stat(name)
	if err != nil {
		return err
	}
	if state.dev != identity.dev || state.ino != identity.ino || state.mode != identity.mode || state.uid != identity.uid {
		return errors.New("compact input temporary name no longer belongs to this writer")
	}
	if err := unix.Unlinkat(int(self.directory.file.Fd()), name, 0); err != nil {
		return err
	}
	return nil
}

// The retained caller has either read exact existing bytes or observed true
// absence. Linkat preserves no-replace semantics and never follows a changed
// pathname to find the temporary source, destination or cleanup directory.
func (self *releaseMeasurementInputV2Owner) write(encoded []byte) (resultErr error) {
	if err := validateReleaseMeasurementInputV2Limit(self.limit); err != nil {
		return err
	}
	if len(encoded) == 0 || uint64(len(encoded)) > self.limit {
		return errors.New("compact input journal exceeds its byte bound")
	}
	if err := self.check(); err != nil {
		return err
	}
	if !self.observed {
		return errors.New("compact input publication has no initial leaf observation")
	}
	if self.witness != nil {
		existing, err := self.read()
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, encoded) {
			return errors.New("compact input journal already names different immutable bytes")
		}
		return self.sync()
	}
	if self.directory.anchor.mode&0o077 != 0 || self.directory.anchor.uid != uint32(os.Geteuid()) {
		return errors.New("compact input publication directory is not private and owned; existing directories are not repaired")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := ".compact-input-" + hex.EncodeToString(nonce[:])
	file, err := self.directory.openFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	identity, err := statAttemptPrivateFile(file)
	if err != nil {
		return errors.Join(err, self.hooks.close(file))
	}
	closed, temporaryOwned := false, true
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, self.hooks.close(file))
		}
		if temporaryOwned {
			removeErr := self.removeTemporary(name, identity)
			resultErr = errors.Join(resultErr, removeErr)
			if removeErr == nil {
				resultErr = errors.Join(resultErr, self.directory.file.Sync())
			}
		}
		resultErr = errors.Join(resultErr, self.ctx.Err())
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	identity.mode = identity.mode&^0o777 | 0o600
	if err := self.hooks.observe("temporary-opened", file, os.O_WRONLY|os.O_CREATE|os.O_EXCL); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	written, writeErr := file.Write(encoded)
	if written != len(encoded) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		return writeErr
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := self.hooks.observe("temporary-synced", file, 0); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	writtenState, err := statAttemptPrivateFile(file)
	if err != nil {
		return err
	}
	closed = true
	if err := self.hooks.close(file); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	current, err := self.directory.stat(name)
	if err != nil || current != writtenState {
		return errors.Join(errors.New("compact input temporary changed after actual Close"), err)
	}
	if err := self.hooks.observe("before-link", nil, 0); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	current, err = self.directory.stat(name)
	if err != nil || current != writtenState {
		return errors.Join(errors.New("compact input temporary changed before publication"), err)
	}
	linkErr := unix.Linkat(int(self.directory.file.Fd()), name, int(self.directory.file.Fd()), self.name, 0)
	if linkErr != nil && !errors.Is(linkErr, os.ErrExist) {
		return linkErr
	}
	// A no-replace winner is read by the same owner, under the original leaf
	// grammar. It may be accepted only as the exact requested immutable bytes.
	self.observed, self.witness, self.initialMissing = false, nil, false
	existing, readErr := self.read()
	if readErr != nil {
		return errors.Join(linkErr, readErr)
	}
	if !bytes.Equal(existing, encoded) {
		return errors.New("compact input journal publication raced different bytes")
	}
	if linkErr == nil && (self.witness.dev != identity.dev || self.witness.ino != identity.ino) {
		return errors.New("compact input publication lost its actual temporary inode")
	}
	if err := self.hooks.observe("journal-linked", nil, 0); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	if err := self.removeTemporary(name, identity); err != nil {
		return err
	}
	temporaryOwned = false
	// Removing our extra link changes ctime/link count, not the journal owner.
	prior := *self.witness
	after, err := self.directory.stat(self.name)
	if err != nil || after.dev != prior.dev || after.ino != prior.ino || after.mode != prior.mode || after.uid != prior.uid || after.size != prior.size || after.modifySeconds != prior.modifySeconds || after.modifyNanoseconds != prior.modifyNanoseconds {
		return errors.Join(errors.New("compact input journal changed during owned temporary cleanup"), err)
	}
	self.witness = &after
	if err := self.hooks.observe("temporary-removed", nil, 0); err != nil {
		return err
	}
	return self.sync()
}

// Context-aware publication is used by real runtime ownership, while old
// standalone signatures retain their Background convenience contract.
func writeReleaseMeasurementInputV2Context(ctx context.Context, path string, encoded []byte, limit uint64, hooks releaseMeasurementInputV2ReadHooks) (resultErr error) {
	if ctx == nil {
		return errors.New("compact input publication context is nil")
	}
	if err := validateReleaseMeasurementInputV2Limit(limit); err != nil {
		return err
	}
	if len(encoded) == 0 || uint64(len(encoded)) > limit {
		return errors.New("compact input journal exceeds its byte bound")
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, limit, hooks, true)
	defer func() { resultErr = errors.Join(resultErr, owner.finish(), ctx.Err()) }()
	if err != nil {
		return err
	}
	_, err = owner.read()
	if err != nil && !(owner.initialMissing && releaseMeasurementInputV2OnlyMissing(err)) {
		return err
	}
	return owner.write(encoded)
}

// An existing journal's retry Sync retains a complete metadata witness; it
// cannot silently acknowledge a different directory after a separate read.
func syncReleaseMeasurementInputV2DirectoryContext(ctx context.Context, path string, hooks releaseMeasurementInputV2ReadHooks) (resultErr error) {
	if ctx == nil {
		return errors.New("compact input sync context is nil")
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, 0, hooks, false)
	defer func() { resultErr = errors.Join(resultErr, owner.finish(), ctx.Err()) }()
	if err != nil {
		return err
	}
	state, err := owner.directory.stat(owner.name)
	if err != nil || !state.regular() || state.mode&0o077 != 0 || state.size < 0 {
		return errors.Join(errors.New("compact input sync has no private regular journal"), err)
	}
	owner.observed, owner.witness = true, &state
	return owner.sync()
}
