//go:build linux || darwin

package validator

// The Stats writer owns its physical parent before callbacks, uses only
// descriptor-relative creation/rename/cleanup, joins actual closes, and checks
// every copied namespace witness after the last observable close. Legacy
// logical aliases are resolved once and rechecked, never followed for writes.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Ancestors created by this operation and the initial retained parent remain
// owned together. Logical names preserve deliberate legacy alias admission.
type statsSnapshotRoot struct {
	directory *attemptPrivateDirectory
	logical   string
}

// Missing path components are a finite initial-absence plan, not a retry
// policy. No creation or permission change occurs during acquisition.
type statsSnapshotDirectory struct {
	path      string
	physical  bool
	roots     []*statsSnapshotRoot
	current   *statsSnapshotRoot
	missing   []string
	leaf      *attemptPrivateFileState
	observed  bool
	hooks     statsSnapshotIOHooks
	written   bool
	finished  bool
	finishErr error
}

// Only genuine missing-name results authorize a missing suffix. In particular
// a joined close/metadata failure cannot be reclassified as absence.
func statsSnapshotOnlyMissing(err error) bool {
	if err == nil {
		return false
	}
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		found := false
		for _, child := range many.Unwrap() {
			if child != nil {
				if !statsSnapshotOnlyMissing(child) {
					return false
				}
				found = true
			}
		}
		return found
	}
	if one, ok := err.(interface{ Unwrap() error }); ok {
		return statsSnapshotOnlyMissing(one.Unwrap())
	}
	return errors.Is(err, os.ErrNotExist)
}

// The physical opener rejects every symlink ancestor, including Darwin /tmp
// and /var aliases. Only the explicit legacy branch resolves existing aliases;
// a dangling alias is occupied invalid state, never permission to create.
func acquireStatsSnapshotDirectory(path string, physical bool, hooks statsSnapshotIOHooks) (*statsSnapshotDirectory, error) {
	if filepath.Base(path) != "stats.json" {
		return nil, errors.New("statistics snapshot target must be stats.json")
	}
	if physical {
		if err := validateStatsSnapshotPhysicalDirectory(filepath.Dir(path)); err != nil {
			return nil, err
		}
		if filepath.Clean(path) != path {
			return nil, errors.New("statistics v2 snapshot path is not canonical")
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	owner := &statsSnapshotDirectory{path: absolute, physical: physical, hooks: hooks}
	candidate := filepath.Dir(absolute)
	for {
		if filepath.Dir(candidate) == candidate {
			return owner, errors.New("statistics snapshot requires an existing non-root directory ancestor")
		}
		_, err := os.Lstat(candidate)
		if err == nil {
			resolved := candidate
			if !physical {
				resolved, err = filepath.EvalSymlinks(candidate)
				if err != nil {
					return owner, err
				}
			}
			directory, err := openAttemptPrivateDirectory(resolved)
			if err != nil {
				return owner, err
			}
			root := &statsSnapshotRoot{directory: directory, logical: candidate}
			owner.roots, owner.current = append(owner.roots, root), root
			if err := root.checkLogical(); err != nil {
				return owner, err
			}
			break
		}
		if !statsSnapshotOnlyMissing(err) {
			return owner, err
		}
		owner.missing = append(owner.missing, filepath.Base(candidate))
		candidate = filepath.Dir(candidate)
	}
	if len(owner.missing) == 0 {
		state, err := owner.current.directory.stat("stats.json")
		if err != nil && !statsSnapshotOnlyMissing(err) {
			return owner, err
		}
		owner.observed = true
		if err == nil {
			owner.leaf = &state
		}
	}
	if err := owner.check(); err != nil {
		return owner, err
	}
	observeErr := hooks.observe("directory-admitted", owner.current.directory.file)
	return owner, errors.Join(observeErr, owner.check())
}

// Compare logical identity without opening a possibly replaced FIFO. Actual
// acquisition and every write use the already admitted native descriptor.
func (self *statsSnapshotRoot) checkLogical() error {
	logical, logicalErr := os.Stat(self.logical)
	actual, actualErr := self.directory.file.Stat()
	if logicalErr != nil || actualErr != nil {
		return errors.Join(logicalErr, actualErr)
	}
	if !logical.IsDir() || !os.SameFile(logical, actual) || logical.Mode() != actual.Mode() {
		return errors.New("statistics snapshot logical directory changed")
	}
	return nil
}

// Metadata checks happen without observers. Expected entry changes do not
// invalidate directory identity/mode/owner; file write state remains exact.
func (self *statsSnapshotDirectory) check() error {
	if self == nil || self.current == nil || self.finished {
		return errors.New("statistics snapshot directory owner is unavailable")
	}
	var resultErr error
	for _, root := range self.roots {
		resultErr = errors.Join(resultErr, root.directory.check(), root.checkLogical())
	}
	return errors.Join(resultErr, self.checkLeaf(self.current.directory))
}

// The first missing component or the target leaf must still match its initial
// observation. Late disappearance is an error, not a fresh missing-state case.
func (self *statsSnapshotDirectory) checkLeaf(directory *attemptPrivateDirectory) error {
	name := "stats.json"
	if len(self.missing) != 0 {
		name = self.missing[len(self.missing)-1]
	}
	state, err := directory.stat(name)
	if len(self.missing) != 0 || self.observed && self.leaf == nil {
		if !statsSnapshotOnlyMissing(err) {
			return errors.Join(errors.New("statistics snapshot initially absent name changed"), err)
		}
		return nil
	}
	if !self.observed || self.leaf == nil {
		return errors.New("statistics snapshot target has no initial observation")
	}
	if err != nil || state != *self.leaf {
		return errors.Join(errors.New("statistics snapshot leaf changed outside its writer"), err)
	}
	return nil
}

// A prepared operation cannot select another target or downgrade an actual
// v6 snapshot to legacy alias semantics. No mutation precedes this admission.
func (self *statsSnapshotDirectory) admit(write statsSnapshotWrite) error {
	if self == nil || self.finished || self.written {
		return errors.New("statistics snapshot writer is already complete")
	}
	absolute, err := filepath.Abs(write.path)
	if err != nil {
		return err
	}
	if absolute != self.path || write.version < 1 || write.version > 6 {
		return errors.New("statistics snapshot write differs from its admitted target or version")
	}
	if self.physical || write.version >= 6 {
		if err := validateStatsSnapshotPhysicalDirectory(filepath.Dir(write.path)); err != nil {
			return err
		}
		if filepath.Clean(write.path) != write.path {
			return errors.New("statistics v2 snapshot path is not canonical")
		}
		for _, root := range self.roots {
			if root.logical != root.directory.path {
				return errors.New("statistics v2 snapshot cannot use a legacy alias")
			}
		}
		self.physical = true
	}
	return self.check()
}

// Create each initially absent component through its retained physical parent.
// Existing private-v2 directories are never chmod-ed. Legacy final-directory
// permission repair uses fchmod on the admitted inode, preserving its API.
func (self *statsSnapshotDirectory) makeReady() error {
	for len(self.missing) != 0 {
		if err := self.check(); err != nil {
			return err
		}
		name := self.missing[len(self.missing)-1]
		parent := self.current
		if err := unix.Mkdirat(int(parent.directory.file.Fd()), name, 0o700); err != nil {
			return err
		}
		before, err := parent.directory.stat(name)
		if err != nil || !before.directory() || before.uid != uint32(os.Geteuid()) || before.mode&0o077 != 0 {
			return errors.Join(errors.New("statistics snapshot created directory differs"), err)
		}
		file, err := parent.directory.openFile(name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			return err
		}
		state, err := statAttemptPrivateFile(file)
		if err != nil || state.dev != before.dev || state.ino != before.ino || state.mode != before.mode || state.uid != before.uid {
			return errors.Join(errors.New("statistics snapshot directory changed during acquisition"), err, file.Close())
		}
		directory := &attemptPrivateDirectory{file: file, path: filepath.Join(parent.directory.path, name), anchor: state}
		child := &statsSnapshotRoot{directory: directory, logical: filepath.Join(parent.logical, name)}
		self.roots, self.current = append(self.roots, child), child
		self.missing = self.missing[:len(self.missing)-1]
		if len(self.missing) == 0 {
			self.observed, self.leaf = true, nil
		}
		syncErr := parent.directory.file.Sync()
		observeErr := self.hooks.observe("directory-created", file)
		if err := errors.Join(syncErr, observeErr, self.check()); err != nil {
			return err
		}
	}
	root := self.current.directory
	if self.physical {
		if root.anchor.mode&0o077 != 0 || root.anchor.uid != uint32(os.Geteuid()) {
			return errors.New("statistics v2 snapshot directory is not private and owned; existing directories are not repaired")
		}
	} else if root.anchor.mode&0o7777 != 0o700 {
		if err := root.file.Chmod(0o700); err != nil {
			return err
		}
		state, err := statAttemptPrivateFile(root.file)
		if err != nil || state.dev != root.anchor.dev || state.ino != root.anchor.ino || state.uid != root.anchor.uid || state.mode&0o7777 != 0o700 {
			return errors.Join(errors.New("statistics snapshot directory permission update differs"), err)
		}
		root.anchor = state
		if err := self.hooks.observe("directory-private", root.file); err != nil {
			return err
		}
	}
	return self.check()
}

// Cleanup refuses a foreign replacement instead of deleting it. The original
// retained parent is also used for its durability Sync and all joined errors.
func (self *statsSnapshotDirectory) removeTemporary(name string, identity attemptPrivateFileState) error {
	state, err := self.current.directory.stat(name)
	if err != nil {
		return err
	}
	if state.dev != identity.dev || state.ino != identity.ino || state.mode != identity.mode || state.uid != identity.uid {
		return errors.New("statistics snapshot temporary name no longer belongs to this writer")
	}
	if err := unix.Unlinkat(int(self.current.directory.file.Fd()), name, 0); err != nil {
		return err
	}
	return errors.Join(self.current.directory.file.Sync(), self.hooks.observe("cleanup-synced", self.current.directory.file))
}

// Start of this method is the already qualified commit boundary. Pure late
// cancellation does not turn a successful durable write into a rollback; real
// write/sync/close/namespace errors still prevent success and remain joined.
func writeStatsSnapshotOwned(self *statsSnapshotDirectory, write statsSnapshotWrite) (resultErr error) {
	if err := self.admit(write); err != nil {
		return err
	}
	if err := self.makeReady(); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := ".stats-" + hex.EncodeToString(nonce[:])
	file, err := self.current.directory.openFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	identity, err := statAttemptPrivateFile(file)
	if err != nil {
		return errors.Join(err, self.hooks.close("temporary-closed", file))
	}
	closed, temporaryOwned := false, true
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, self.hooks.close("temporary-closed", file))
		}
		if temporaryOwned {
			resultErr = errors.Join(resultErr, self.removeTemporary(name, identity))
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	identity.mode = identity.mode&^0o777 | 0o600
	if err := self.hooks.observe("temporary-opened", file); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	written, writeErr := file.Write(write.data)
	if written != len(write.data) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	observeErr := self.hooks.observe("temporary-written", file)
	if err := errors.Join(writeErr, observeErr); err != nil {
		return err
	}
	syncErr := file.Sync()
	observeErr = self.hooks.observe("temporary-synced", file)
	if err := errors.Join(syncErr, observeErr, self.check()); err != nil {
		return err
	}
	writtenState, err := statAttemptPrivateFile(file)
	if err != nil {
		return err
	}
	closed = true
	if err := self.hooks.close("temporary-closed", file); err != nil {
		return err
	}
	if err := self.hooks.observe("before-rename", nil); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	current, err := self.current.directory.stat(name)
	if err != nil || current != writtenState {
		return errors.Join(errors.New("statistics snapshot temporary changed after actual Close"), err)
	}
	directory := self.current.directory
	// The destination is only replaced, never opened/read/followed. Preserve
	// the real legacy rename refusal for an existing directory obstruction.
	if err := unix.Renameat(int(directory.file.Fd()), name, int(directory.file.Fd()), "stats.json"); err != nil {
		return &os.LinkError{Op: "rename", Old: filepath.Join(directory.path, name), New: filepath.Join(directory.path, "stats.json"), Err: err}
	}
	temporaryOwned = false
	published, err := directory.stat("stats.json")
	if err != nil || published.dev != writtenState.dev || published.ino != writtenState.ino || published.mode != writtenState.mode || published.uid != writtenState.uid || published.links != writtenState.links || published.size != writtenState.size || published.modifySeconds != writtenState.modifySeconds || published.modifyNanoseconds != writtenState.modifyNanoseconds {
		return errors.Join(errors.New("statistics snapshot publication lost its actual temporary inode"), err)
	}
	self.leaf, self.observed = &published, true
	observeErr = self.hooks.observe("snapshot-renamed", nil)
	if err := errors.Join(observeErr, self.check()); err != nil {
		return err
	}
	syncErr = directory.file.Sync()
	observeErr = self.hooks.observe("directory-synced", directory.file)
	if err := errors.Join(syncErr, observeErr, self.check()); err != nil {
		return err
	}
	self.written = true
	return nil
}

// Every observable directory close finishes before the final unhooked
// all-root/leaf witness. A later close cannot retarget an earlier checked root.
func (self *statsSnapshotDirectory) finish() error {
	if self == nil {
		return nil
	}
	if self.finished {
		return self.finishErr
	}
	self.finished = true
	for index := len(self.roots) - 1; index >= 0; index-- {
		root := self.roots[index].directory
		if root.file != nil {
			file := root.file
			root.file = nil
			self.finishErr = errors.Join(self.finishErr, self.hooks.close("directory-closed", file))
		}
	}
	self.finishErr = errors.Join(self.finishErr, self.checkFinal())
	return self.finishErr
}

// Reopens only for the copied witness, never for publication. These actual
// witness closes have no observer and are also joined into the result.
func (self *statsSnapshotDirectory) checkFinal() error {
	if self == nil {
		return nil
	}
	var resultErr error
	for _, root := range self.roots {
		current, err := openAttemptPrivateDirectory(root.directory.path)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		prior, actual := root.directory.anchor, current.anchor
		if actual.dev != prior.dev || actual.ino != prior.ino || actual.mode != prior.mode || actual.uid != prior.uid {
			resultErr = errors.Join(resultErr, errors.New("statistics snapshot directory changed after actual Close"))
		}
		logical, logicalErr := os.Stat(root.logical)
		info, infoErr := current.file.Stat()
		if logicalErr != nil || infoErr != nil {
			resultErr = errors.Join(resultErr, logicalErr, infoErr)
		} else if !os.SameFile(logical, info) || logical.Mode() != info.Mode() {
			resultErr = errors.Join(resultErr, errors.New("statistics snapshot logical directory changed after actual Close"))
		}
		if root == self.current {
			resultErr = errors.Join(resultErr, self.checkLeaf(current))
		}
		resultErr = errors.Join(resultErr, current.close())
	}
	return resultErr
}
