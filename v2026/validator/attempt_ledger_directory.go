package validator

// The retained state-directory inode is the shared migration gate for upgraded
// JSONL writers and disk import. Rollout must stop and join unpatched binaries:
// an advisory lock cannot constrain code that never acquires it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	attemptLedgerLegacyName = "attempt-ledger.jsonl"
	attemptLedgerImportName = "attempt-ledger-import.json"
	attemptLedgerReadyName  = "attempt-ledger-ready.json"
	attemptLedgerStoreName  = "attempt-ledger.records"
)

// Operations are serialized by enter/leave. Hooks expose actual inode and I/O
// boundaries for deterministic tests and are nil in production.
type attemptLedgerDirectory struct {
	stateLock sync.Mutex
	failure   error
	storage   *durablepath.Directory
	path      string
	name      string
	anchor    os.FileInfo
	parent    *os.Root
	root      *os.Root
	directory *os.File
	gate      chan struct{}
	step      func(string, string) error
	statFile  func(*os.File) (os.FileInfo, error)
	statName  func(*os.Root, string) (os.FileInfo, error)
}

// Only the final directory is created here; existing parents are resolved
// once, then all evidence access uses descriptor-relative no-follow opens.
func openAttemptLedgerDirectory(path string, step func(string, string) error, storageContexts ...context.Context) (*attemptLedgerDirectory, error) {
	path, err := filepath.Abs(path)
	if err != nil || filepath.Dir(path) == path {
		return nil, errors.New("attempt ledger state directory is invalid")
	}
	storage, err := openValidatorDurableDirectory(validatorStorageContext(storageContexts), path, durablevolume.ReadWrite, false)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = storage.Close()
		}
	}()
	// Preserve the legacy constructor's support for nested new state paths.
	// Existing directory modes are never broadened or silently repaired.
	if storage == nil {
		ancestor := filepath.Dir(path)
		for {
			if _, err := os.Stat(ancestor); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			ancestor = filepath.Dir(ancestor)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		for current := filepath.Dir(path); ; current = filepath.Dir(current) {
			file, err := os.Open(current)
			if err != nil {
				return nil, err
			}
			if err := errors.Join(file.Sync(), file.Close()); err != nil {
				return nil, err
			}
			if current == ancestor {
				break
			}
		}
	}
	parentPath, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		return nil, err
	}
	self := &attemptLedgerDirectory{storage: storage, path: filepath.Join(parentPath, filepath.Base(path)), name: filepath.Base(path), parent: parent, gate: make(chan struct{}, 1), step: step}
	transferred = true
	complete := false
	defer func() {
		if !complete {
			_ = self.Close()
		}
	}()
	if _, err := parent.Lstat(self.name); errors.Is(err, os.ErrNotExist) {
		if err := parent.Mkdir(self.name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	self.anchor, err = parent.Lstat(self.name)
	if err != nil {
		return nil, attemptLedgerDirectoryObservation("cannot observe opened attempt ledger directory", err)
	}
	if !attemptLedgerPrivateDirectory(self.anchor) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("attempt ledger state directory is not private and owned"))
	}
	self.root, err = parent.OpenRoot(self.name)
	if err != nil {
		return nil, err
	}
	self.directory, err = self.root.Open(".")
	if err != nil {
		return nil, err
	}
	opened, err := self.directory.Stat()
	if err != nil {
		return nil, attemptLedgerDirectoryObservation("cannot observe attempt ledger directory descriptor", err)
	}
	if !os.SameFile(opened, self.anchor) || !attemptLedgerPrivateDirectory(opened) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("attempt ledger state directory changed during open"))
	}
	parentFile, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	err = errors.Join(parentFile.Sync(), parentFile.Close())
	if err != nil {
		return nil, err
	}
	complete = true
	return self, nil
}

// Failed observations preserve their cause; a missing retained name proves
// custody loss. Instance hooks replace only the actual observation boundary.
func attemptLedgerDirectoryObservation(detail string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return errors.Join(durablevolume.ErrIdentity, errors.New(detail), err)
	}
	return errors.Join(&durablevolume.UnavailableError{Reason: detail}, err)
}

func (self *attemptLedgerDirectory) observeFile(file *os.File) (os.FileInfo, error) {
	if self.statFile != nil {
		return self.statFile(file)
	}
	return file.Stat()
}

func (self *attemptLedgerDirectory) observeName(name string) (os.FileInfo, error) {
	if self.statName != nil {
		return self.statName(self.root, name)
	}
	return self.root.Lstat(name)
}

// Replacing or chmodding the directory never redirects an existing owner.
func (self *attemptLedgerDirectory) check() (resultErr error) {
	self.stateLock.Lock()
	failure := self.failure
	self.stateLock.Unlock()
	if failure != nil {
		return failure
	}
	defer func() {
		if errors.Is(resultErr, durablevolume.ErrIdentity) {
			self.stateLock.Lock()
			if self.failure == nil {
				self.failure = resultErr
			}
			self.stateLock.Unlock()
		}
	}()
	if err := checkValidatorDurableDirectory(self.storage, self.directory); err != nil {
		return err
	}
	info, err := self.parent.Lstat(self.name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(&durablevolume.UnavailableError{Reason: "cannot observe attempt ledger state directory"}, err)
	}
	if err != nil || !attemptLedgerPrivateDirectory(info) || !os.SameFile(info, self.anchor) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("attempt ledger state directory changed after open"), err)
	}
	return nil
}

// The local token handles two calls sharing one flock descriptor. Contention
// uses a cancellable wait and the observed-contention seam, never busy polling.
func (self *attemptLedgerDirectory) enter(ctx context.Context) error {
	if ctx == nil {
		return errors.New("attempt ledger directory context is nil")
	}
	select {
	case self.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	complete := false
	defer func() {
		if !complete {
			<-self.gate
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		locked, err := attemptLedgerTryLock(self.directory)
		if err != nil {
			return err
		}
		if locked {
			if err := self.check(); err != nil {
				return errors.Join(err, attemptLedgerUnlock(self.directory))
			}
			complete = true
			return nil
		}
		if self.step != nil {
			if err := self.step("gate-contended", ""); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Unlock errors remain visible rather than silently transferring ownership.
func (self *attemptLedgerDirectory) leave() error {
	err := attemptLedgerUnlock(self.directory)
	<-self.gate
	return err
}

// Any migration artifact makes upgraded legacy writers refuse further writes,
// including a crash before a completed marker could be published.
func (self *attemptLedgerDirectory) requireLegacy() error {
	for _, name := range []string{attemptLedgerImportName, attemptLedgerImportName + ".tmp", attemptLedgerReadyName, attemptLedgerReadyName + ".tmp", attemptLedgerStoreName} {
		if _, err := self.root.Lstat(name); err == nil {
			return ErrAttemptLedgerDiskMigration
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Existing evidence must be owned, single-link and private at both the name
// and opened descriptor. Exclusive creates never truncate an unexpected file.
func (self *attemptLedgerDirectory) openFile(name string, flags int, create bool) (*os.File, error) {
	if err := self.check(); err != nil {
		return nil, err
	}
	if filepath.Base(name) != name || name == "." {
		return nil, errors.New("attempt ledger private filename is invalid")
	}
	info, err := self.root.Lstat(name)
	if err != nil && !(create && errors.Is(err, os.ErrNotExist)) {
		return nil, err
	}
	if err == nil && !attemptLedgerPrivateFile(info) {
		return nil, errors.New("attempt ledger evidence is not a private single-link owned file")
	}
	if self.step != nil {
		if err := self.step("before-open", name); err != nil {
			return nil, err
		}
	}
	file, err := self.root.OpenFile(name, flags|attemptLedgerNoFollowFlag(), 0o600)
	if err != nil {
		return nil, err
	}
	opened, err := self.observeFile(file)
	if err != nil {
		return nil, errors.Join(attemptLedgerDirectoryObservation("cannot observe opened attempt ledger evidence", err), file.Close())
	}
	current, err := self.observeName(name)
	if err != nil {
		return nil, errors.Join(attemptLedgerDirectoryObservation("cannot reobserve named attempt ledger evidence", err), file.Close())
	}
	if !attemptLedgerPrivateFile(opened) || !attemptLedgerPrivateFile(current) || !os.SameFile(opened, current) || info != nil && !os.SameFile(info, opened) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("attempt ledger evidence changed during open"), file.Close())
	}
	return file, nil
}

// Legacy append uses the same anchored namespace as its migration check.
func (self *attemptLedgerDirectory) appendLegacy(payload []byte) (resultErr error) {
	file, err := self.openFile(attemptLedgerLegacyName, os.O_WRONLY|os.O_APPEND, false)
	if errors.Is(err, os.ErrNotExist) {
		file, err = self.openFile(attemptLedgerLegacyName, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL, true)
	}
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if n, err := file.Write(payload); err != nil {
		return err
	} else if n != len(payload) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return self.sync("legacy")
}

// New paths and replacement acknowledgements include the directory barrier.
func (self *attemptLedgerDirectory) sync(stage string) error {
	if err := self.check(); err != nil {
		return err
	}
	if self.step != nil {
		if err := self.step("directory-sync", stage); err != nil {
			return err
		}
	}
	if err := self.directory.Sync(); err != nil {
		return err
	}
	return self.check()
}

// Bounded metadata reads reject oversize before allocating the complete value.
func (self *attemptLedgerDirectory) readSmall(name string, limit uint64) ([]byte, error) {
	file, err := self.openFile(name, os.O_RDONLY, false)
	if err != nil {
		return nil, err
	}
	info, err := self.observeFile(file)
	if err != nil {
		return nil, errors.Join(attemptLedgerDirectoryObservation("cannot observe attempt ledger metadata size", err), file.Close())
	}
	if info.Size() < 0 || uint64(info.Size()) > limit {
		return nil, errors.Join(errors.New("attempt ledger metadata exceeds its bound"), file.Close())
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	err = errors.Join(err, file.Close())
	if uint64(len(raw)) > limit {
		err = errors.Join(err, errors.New("attempt ledger metadata grew beyond its bound"))
	}
	return raw, err
}

// Immutable marker publication resumes only an exact interrupted byte prefix.
// A conflicting temporary or destination file is evidence, never overwritten.
func (self *attemptLedgerDirectory) publishMarker(name string, raw []byte) (resultErr error) {
	if existing, err := self.readSmall(name, uint64(len(raw))); err == nil {
		if !bytes.Equal(existing, raw) {
			return errors.New("attempt ledger migration marker conflicts")
		}
		file, err := self.openFile(name, os.O_RDONLY, false)
		if err != nil {
			return err
		}
		err = errors.Join(file.Sync(), file.Close())
		if err != nil {
			return err
		}
		if err := self.sync(name); err != nil {
			return err
		}
		actual, err := self.readSmall(name, uint64(len(raw)))
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, raw) {
			return errors.Join(durablevolume.ErrIdentity, errors.New("attempt ledger existing marker changed during acknowledgement"))
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary := name + ".tmp"
	file, err := self.openFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, true)
	if errors.Is(err, os.ErrExist) {
		file, err = self.openFile(temporary, os.O_RDWR, false)
	}
	if err != nil {
		return err
	}
	defer func() {
		if file != nil {
			resultErr = errors.Join(resultErr, file.Close())
		}
	}()
	info, err := self.observeFile(file)
	if err != nil {
		return attemptLedgerDirectoryObservation("cannot observe attempt ledger partial marker size", err)
	}
	if info.Size() < 0 || info.Size() > int64(len(raw)) {
		return errors.New("attempt ledger partial marker exceeds expected bytes")
	}
	prior := make([]byte, int(info.Size()))
	if _, err := io.ReadFull(file, prior); err != nil {
		return attemptLedgerDirectoryObservation("cannot read attempt ledger partial marker", err)
	}
	if !bytes.HasPrefix(raw, prior) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("attempt ledger partial marker conflicts"))
	}
	if n, err := file.Write(raw[len(prior):]); err != nil {
		return err
	} else if n != len(raw)-len(prior) {
		return io.ErrShortWrite
	}
	if self.step != nil {
		if err := self.step("marker-sync", name); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	publishedInfo, err := self.observeFile(file)
	if err != nil {
		return attemptLedgerDirectoryObservation("cannot observe synced attempt ledger marker", err)
	}
	err = file.Close()
	file = nil
	if err != nil {
		return err
	}
	current, err := self.observeName(temporary)
	if err != nil {
		return attemptLedgerDirectoryObservation("cannot reobserve attempt ledger marker before publication", err)
	}
	if !attemptLedgerPrivateFile(current) || !os.SameFile(current, publishedInfo) || current.Size() != int64(len(raw)) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("attempt ledger marker inode changed before publication"))
	}
	if err := attemptLedgerRenameNoReplace(self.directory, temporary, name); err != nil {
		return fmt.Errorf("publish attempt ledger marker: %w", err)
	}
	if err := self.sync(name); err != nil {
		return err
	}
	actual, err := self.readSmall(name, uint64(len(raw)))
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, raw) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("attempt ledger marker bytes changed during publication"))
	}
	return nil
}

// The ledger joins users before releasing its retained directory handles.
func (self *attemptLedgerDirectory) Close() error {
	if self == nil {
		return nil
	}
	var err error
	if self.directory != nil {
		err = errors.Join(err, self.directory.Close())
		self.directory = nil
	}
	if self.root != nil {
		err = errors.Join(err, self.root.Close())
		self.root = nil
	}
	if self.parent != nil {
		err = errors.Join(err, self.parent.Close())
		self.parent = nil
	}
	return errors.Join(err, self.storage.Close())
}
