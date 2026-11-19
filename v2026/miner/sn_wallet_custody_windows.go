//go:build windows

// The Unix custody's guarantees with Windows primitives (internal/wincustody):
// handle-relative, reparse-point-free traversal, security descriptors in place
// of owner uid and mode bits, and a LockFileEx lease. Windows cannot lock a
// directory, so the lease is .history.lock inside the private directory; it is
// never replaced, so replacing the journal still never replaces its lock.
package miner

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/urfoundation/sn/v2026/internal/wincustody"
)

const snWalletConsentJournalName = "history.json"
const snWalletConsentPendingName = ".history.pending"
const snWalletConsentLockName = ".history.lock"

// A provider credential is a bounded token; this only bounds the stat.
const snWalletConsentTokenMaxBytes = 1 << 20

// Parent and directory handles remain owned until the HTTP owner joins.
type snWalletConsentStore struct {
	parent     *os.File
	directory  *os.File
	lock       *os.File
	parentPath string
	name       string
	// the identities and descriptors check compares
	parentIdentity    wincustody.Identity
	directoryIdentity wincustody.Identity
	parentSecurity    string
	directorySecurity string
}

// Open only existing ancestors and the selected existing private credential.
// The sole allowed birth is its private wallet-consent sibling directory.
func openSnWalletConsentStore(tokenPath string) (_ *snWalletConsentStore, created bool, returnErr error) {
	if tokenPath == "" || filepath.Clean(tokenPath) != tokenPath {
		return nil, false, errors.New("wallet consent requires the selected canonical provider credential path")
	}
	path, err := filepath.Abs(tokenPath)
	if err != nil || filepath.Base(path) == "." || filepath.Dir(path) == path {
		return nil, false, errors.Join(errors.New("wallet consent credential path is invalid"), err)
	}
	self := &snWalletConsentStore{parentPath: filepath.Dir(path), name: filepath.Base(path) + ".wallet-consent"}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.close())
		}
	}()
	parent, err := wincustody.OpenPath(self.parentPath)
	if err != nil {
		return nil, false, err
	}
	self.parent = os.NewFile(uintptr(parent), self.parentPath)
	self.parentSecurity, err = wincustody.Privacy(parent, true, false)
	if errors.Is(err, wincustody.ErrDirectoryNotPrivate) {
		return nil, false, errors.New("wallet consent parent must be owned and not writable by other users")
	}
	if err != nil {
		return nil, false, err
	}
	if self.parentIdentity, err = wincustody.HandleIdentity(parent); err != nil {
		return nil, false, err
	}
	token, err := wincustody.OpenLeaf(parent, self.parentPath, filepath.Base(path), wincustody.OpenRead, snWalletConsentTokenMaxBytes)
	var statErr error
	if errors.Is(err, wincustody.ErrFileNotPrivate) {
		return nil, false, errors.New("wallet consent requires an existing private regular provider credential")
	}
	if err != nil {
		return nil, false, err
	}
	if info, err := token.Stat(); err != nil || info.Size() <= 0 {
		statErr = errors.Join(errors.New("wallet consent requires an existing private regular provider credential"), err)
	}
	if err := errors.Join(statErr, token.Close()); err != nil {
		return nil, false, err
	}
	if err := wincustody.MakeDirectoryAt(parent, self.name); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, false, err
		}
	} else {
		created = true
	}
	directory, err := wincustody.OpenDirectory(parent, self.name)
	if err != nil {
		return nil, false, err
	}
	self.directory = os.NewFile(uintptr(directory), path+".wallet-consent")
	// the Unix directory must be exactly 0700: nobody else gets anything
	self.directorySecurity, err = wincustody.Privacy(directory, true, true)
	if err != nil {
		return nil, false, errors.New("wallet consent directory identity or private ownership changed")
	}
	if self.directoryIdentity, err = wincustody.HandleIdentity(directory); err != nil {
		return nil, false, err
	}
	if err := self.check(); err != nil {
		return nil, false, err
	}
	self.lock, err = self.open(snWalletConsentLockName, wincustody.OpenLock)
	if err != nil {
		return nil, false, err
	}
	if err := wincustody.Lock(self.lock); err != nil {
		return nil, false, fmt.Errorf("wallet consent already has an active owner: %w", err)
	}
	if err := self.check(); err != nil {
		return nil, false, err
	}
	return self, created, nil
}

// Reopening authenticates names only. Actual I/O always uses the original
// directory, and this owner refuses a replaced parent or directory or a
// changed descriptor on either.
func (self *snWalletConsentStore) check() error {
	if self == nil || self.directory == nil || self.parent == nil {
		return errors.New("wallet consent custody is closed")
	}
	visibleParent, err := wincustody.OpenPath(self.parentPath)
	if err != nil {
		return err
	}
	visibleParentIdentity, parentErr := wincustody.HandleIdentity(visibleParent)
	visibleDirectory, directoryErr := wincustody.OpenDirectory(windows.Handle(self.parent.Fd()), self.name)
	var visibleDirectoryIdentity wincustody.Identity
	if directoryErr == nil {
		visibleDirectoryIdentity, directoryErr = wincustody.HandleIdentity(visibleDirectory)
		directoryErr = errors.Join(directoryErr, windows.CloseHandle(visibleDirectory))
	}
	parentIdentity, parentOwnErr := wincustody.HandleIdentity(windows.Handle(self.parent.Fd()))
	directoryIdentity, directoryOwnErr := wincustody.HandleIdentity(windows.Handle(self.directory.Fd()))
	parentSecurity, parentSecurityErr := wincustody.Descriptor(windows.Handle(self.parent.Fd()))
	directorySecurity, directorySecurityErr := wincustody.Descriptor(windows.Handle(self.directory.Fd()))
	if err := errors.Join(parentErr, directoryErr, parentOwnErr, directoryOwnErr, parentSecurityErr, directorySecurityErr, windows.CloseHandle(visibleParent)); err != nil {
		return err
	}
	if parentIdentity != self.parentIdentity || visibleParentIdentity != self.parentIdentity || parentSecurity != self.parentSecurity || directoryIdentity != self.directoryIdentity || visibleDirectoryIdentity != self.directoryIdentity || directorySecurity != self.directorySecurity {
		return errors.New("wallet consent directory identity or private ownership changed")
	}
	return nil
}

// Handle admission refuses links, other owners, wider descriptors, extra
// hard links and oversized files, as the Unix fstat admission does.
func (self *snWalletConsentStore) open(name string, mode int) (*os.File, error) {
	file, err := wincustody.OpenLeaf(windows.Handle(self.directory.Fd()), filepath.Join(self.parentPath, self.name), name, mode, snWalletConsentMaxBytes)
	if errors.Is(err, wincustody.ErrFileNotPrivate) {
		return nil, errors.New("wallet consent file has unsafe ownership, permissions, links or size")
	}
	return file, err
}

// A crash during an unfinished temporary write remains explicit uncertainty.
// A canonical journal is re-flushed before reuse, including after a prior
// owner saw its rename succeed but could not acknowledge the flush; the
// journal is opened for writing only so it can be flushed.
func (self *snWalletConsentStore) read() ([]byte, error) {
	if err := self.check(); err != nil {
		return nil, err
	}
	pending, err := wincustody.Exists(windows.Handle(self.directory.Fd()), snWalletConsentPendingName)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, errors.New("wallet consent has an incomplete publication; retained originals require reconciliation")
	}
	file, err := self.open(snWalletConsentJournalName, wincustody.OpenReadWrite)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, snWalletConsentMaxBytes+1))
	if len(raw) > snWalletConsentMaxBytes {
		readErr = errors.Join(readErr, errors.New("wallet consent journal exceeds its finite byte bound"))
	}
	if err := errors.Join(readErr, file.Sync(), file.Close(), self.check()); err != nil {
		return nil, err
	}
	return raw, nil
}

// One fixed temporary bounds interrupted publication storage as well as the
// journal itself. Returned failures never authorize an HTTP handoff.
func (self *snWalletConsentStore) write(raw []byte, previousHash [32]byte, hasPrevious bool) (returnErr error) {
	if len(raw) == 0 || len(raw) > snWalletConsentMaxBytes {
		return errors.New("wallet consent publication exceeds its finite byte bound")
	}
	if err := self.check(); err != nil {
		return err
	}
	checkPrevious := func() error {
		previous, err := self.open(snWalletConsentJournalName, wincustody.OpenRead)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && !hasPrevious {
				return nil
			}
			return err
		}
		encoded, readErr := io.ReadAll(io.LimitReader(previous, snWalletConsentMaxBytes+1))
		if err := errors.Join(readErr, previous.Close()); err != nil {
			return err
		}
		if !hasPrevious || len(encoded) > snWalletConsentMaxBytes || sha256.Sum256(encoded) != previousHash {
			return errors.New("wallet consent publication predecessor changed")
		}
		return nil
	}
	if err := checkPrevious(); err != nil {
		return err
	}
	file, err := self.open(snWalletConsentPendingName, wincustody.OpenCreate)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			// renamed aside durably, as Unix unlinks and syncs: a pending
			// file back after a crash would refuse every later read
			aside, err := snWalletConsentAsideName()
			if err == nil {
				err = wincustody.Remove(windows.Handle(self.directory.Fd()), filepath.Join(self.parentPath, self.name), snWalletConsentPendingName, aside, self.lock)
			}
			returnErr = errors.Join(returnErr, err)
		}
	}()
	written, writeErr := file.Write(raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close(), self.check(), checkPrevious()); err != nil {
		return err
	}
	if err := wincustody.Rename(windows.Handle(self.directory.Fd()), filepath.Join(self.parentPath, self.name), snWalletConsentPendingName, snWalletConsentJournalName); err != nil {
		return err
	}
	return self.check()
}

func snWalletConsentAsideName() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return ".history.removed-" + hex.EncodeToString(random[:]), nil
}

// Closing the lock file releases the lease even after process death.
func (self *snWalletConsentStore) close() error {
	if self == nil {
		return nil
	}
	var err error
	if self.lock != nil {
		err = self.lock.Close()
		self.lock = nil
	}
	if self.directory != nil {
		err = errors.Join(err, self.directory.Close())
		self.directory = nil
	}
	if self.parent != nil {
		err = errors.Join(err, self.parent.Close())
		self.parent = nil
	}
	return err
}
