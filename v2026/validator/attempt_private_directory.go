package validator

// Native metadata primitives are shared by callers with different leaf
// admission policies. They do not widen either caller's bounds or permissions.

import (
	"context"
	"errors"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"os"
	"path/filepath"
	"sync"
)

// Comparable full-width native state, with timestamps supplied by the target
// platform. Directory identity checks omit expected entry-content mutations.
type attemptPrivateFileState struct {
	dev               uint64
	ino               uint64
	mode              uint32
	uid               uint32
	links             uint64
	size              int64
	changeSeconds     int64
	changeNanoseconds int64
	modifySeconds     int64
	modifyNanoseconds int64
}

// The native file type is checked before a reader can consume any bytes.
func (self attemptPrivateFileState) regular() bool { return self.mode&0o170000 == 0o100000 }

// The directory acquisition itself supplies O_DIRECTORY, not a later guess.
func (self attemptPrivateFileState) directory() bool { return self.mode&0o170000 == 0o040000 }

// One retained descriptor is never redirected by a pathname replacement.
type attemptPrivateDirectory struct {
	stateLock   sync.Mutex
	failure     error
	statForTest func(*os.File) (os.FileInfo, error)
	storage     *durablepath.Directory
	storageCtx  context.Context
	file        *os.File
	path        string
	anchor      attemptPrivateFileState
}

// Every component is opened natively with no-follow, nonblocking directory
// flags. A FIFO or symlink replacement cannot block or redirect acquisition.
func openAttemptPrivateDirectory(path string, storageContexts ...context.Context) (*attemptPrivateDirectory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return nil, errors.New("private directory path is not canonical absolute non-root")
	}
	ctx := validatorStorageContext(storageContexts)
	storage, err := openValidatorDurableDirectory(ctx, path, durablevolume.ReadWrite, false)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = storage.Close()
		}
	}()
	file, err := openAttemptPrivateDirectoryFile(path)
	if err != nil {
		return nil, err
	}
	state, err := statAttemptPrivateFile(file)
	if err != nil || !state.directory() {
		return nil, errors.Join(errors.New("private directory descriptor differs"), err, file.Close())
	}
	if err := checkValidatorDurableDirectory(storage, file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	transferred = true
	return &attemptPrivateDirectory{storage: storage, storageCtx: ctx, file: file, path: path, anchor: state}, nil
}

// Reopening uses the same safe component acquisition; only exact directory
// identity, owner and mode are stable while its entries legitimately change.
func (self *attemptPrivateDirectory) check() (resultErr error) {
	if self == nil || self.file == nil {
		return errors.New("private directory owner is closed")
	}
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
			resultErr = self.failure
			self.stateLock.Unlock()
		}
	}()
	if err := checkValidatorDurableDirectoryWithStat(self.storage, self.file, self.statForTest); err != nil {
		return err
	}
	current, err := openAttemptPrivateDirectory(self.path)
	if err != nil {
		return err
	}
	err = current.close()
	if self.anchor.dev != current.anchor.dev || self.anchor.ino != current.anchor.ino || self.anchor.mode != current.anchor.mode || self.anchor.uid != current.anchor.uid {
		return errors.Join(durablevolume.ErrIdentity, errors.New("private directory namespace changed after acquisition"), err)
	}
	state, statErr := statAttemptPrivateFile(self.file)
	if statErr != nil {
		return errors.Join(&durablevolume.UnavailableError{Reason: "cannot observe retained private directory"}, err, statErr)
	}
	if state.dev != self.anchor.dev || state.ino != self.anchor.ino || state.mode != self.anchor.mode || state.uid != self.anchor.uid {
		return errors.Join(durablevolume.ErrIdentity, errors.New("private directory descriptor changed after acquisition"), err)
	}
	return err
}

// Closing does not suppress a late descriptor failure.
func (self *attemptPrivateDirectory) close() error {
	if self == nil || self.file == nil {
		return nil
	}
	file := self.file
	self.file = nil
	return errors.Join(file.Close(), self.storage.Close())
}

// Leaf primitives accept one name only, never a path that could traverse an
// unexamined intermediate directory or escape the retained descriptor.
func validAttemptPrivateLeaf(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name
}
