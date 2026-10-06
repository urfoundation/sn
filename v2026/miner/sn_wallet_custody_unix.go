//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

// A physical private directory owns the lease, so replacing the journal never
// replaces its lock. Every custody path component is opened without symlinks.
package miner

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const snWalletConsentJournalName = "history.json"
const snWalletConsentPendingName = ".history.pending"

// Parent and directory descriptors remain owned until the HTTP owner joins.
type snWalletConsentStore struct {
	parent     *os.File
	directory  *os.File
	parentPath string
	name       string
}

// Open only existing ancestors and the selected existing private credential.
// The sole allowed birth is its 0700 wallet-consent sibling directory.
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
	self.parent, err = openSnWalletConsentDirectory(self.parentPath)
	if err != nil {
		return nil, false, err
	}
	var parentStat unix.Stat_t
	if err := unix.Fstat(int(self.parent.Fd()), &parentStat); err != nil {
		return nil, false, err
	}
	if parentStat.Uid != uint32(os.Geteuid()) || parentStat.Mode&0o022 != 0 {
		return nil, false, errors.New("wallet consent parent must be owned and not writable by other users")
	}
	fd, err := unix.Openat(int(self.parent.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, false, err
	}
	token := os.NewFile(uintptr(fd), path)
	var tokenStat unix.Stat_t
	statErr := unix.Fstat(fd, &tokenStat)
	if statErr == nil && (tokenStat.Mode&unix.S_IFMT != unix.S_IFREG || tokenStat.Uid != uint32(os.Geteuid()) || tokenStat.Mode&0o077 != 0 || tokenStat.Nlink != 1 || tokenStat.Size <= 0) {
		statErr = errors.New("wallet consent requires an existing private regular provider credential")
	}
	if err := errors.Join(statErr, token.Close()); err != nil {
		return nil, false, err
	}
	if err := unix.Mkdirat(int(self.parent.Fd()), self.name, 0o700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, false, err
		}
	} else {
		created = true
	}
	fd, err = unix.Openat(int(self.parent.Fd()), self.name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, false, err
	}
	self.directory = os.NewFile(uintptr(fd), path+".wallet-consent")
	if err := self.check(); err != nil {
		return nil, false, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, false, fmt.Errorf("wallet consent already has an active owner: %w", err)
	}
	if err := self.check(); err != nil {
		return nil, false, err
	}
	return self, created, nil
}

// Descriptor-relative traversal refuses symlinks at every ancestor, including
// an alias of an otherwise valid private directory.
func openSnWalletConsentDirectory(path string) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), string(filepath.Separator))
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		next, openErr := unix.Openat(int(current.Fd()), component, flags, 0)
		closeErr := current.Close()
		if openErr != nil {
			return nil, errors.Join(openErr, closeErr)
		}
		current = os.NewFile(uintptr(next), path)
		if closeErr != nil {
			return nil, errors.Join(closeErr, current.Close())
		}
	}
	return current, nil
}

// Reopening authenticates names only. Actual I/O always uses the original
// directory, and this owner refuses a replaced parent or directory.
func (self *snWalletConsentStore) check() error {
	if self == nil || self.directory == nil || self.parent == nil {
		return errors.New("wallet consent custody is closed")
	}
	current, err := openSnWalletConsentDirectory(self.parentPath)
	if err != nil {
		return err
	}
	var parent, visibleParent, directory, visibleDirectory unix.Stat_t
	statErr := errors.Join(
		unix.Fstat(int(self.parent.Fd()), &parent),
		unix.Fstat(int(current.Fd()), &visibleParent),
		unix.Fstat(int(self.directory.Fd()), &directory),
		unix.Fstatat(int(self.parent.Fd()), self.name, &visibleDirectory, unix.AT_SYMLINK_NOFOLLOW),
	)
	if err := errors.Join(statErr, current.Close()); err != nil {
		return err
	}
	if parent.Dev != visibleParent.Dev || parent.Ino != visibleParent.Ino || parent.Uid != uint32(os.Geteuid()) || parent.Mode&0o022 != 0 || directory.Dev != visibleDirectory.Dev || directory.Ino != visibleDirectory.Ino || directory.Mode&unix.S_IFMT != unix.S_IFDIR || directory.Mode&0o7777 != 0o700 || directory.Uid != uint32(os.Geteuid()) {
		return errors.New("wallet consent directory identity or private ownership changed")
	}
	return nil
}

// Nonblocking admission prevents fifos from hanging a wallet invocation. A
// hardlink cannot alias the retained signed bytes outside this private owner.
func (self *snWalletConsentStore) open(name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(self.directory.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || stat.Size < 0 || stat.Size > snWalletConsentMaxBytes {
		return nil, errors.Join(errors.New("wallet consent file has unsafe ownership, permissions, links or size"), file.Close())
	}
	return file, nil
}

// A crash during an unfinished temporary write remains explicit uncertainty.
// A canonical journal is re-synced before reuse, including after a prior owner
// saw its rename succeed but could not acknowledge the directory sync.
func (self *snWalletConsentStore) read() ([]byte, error) {
	if err := self.check(); err != nil {
		return nil, err
	}
	var pending unix.Stat_t
	if err := unix.Fstatat(int(self.directory.Fd()), snWalletConsentPendingName, &pending, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return nil, errors.New("wallet consent has an incomplete publication; retained originals require reconciliation")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := self.open(snWalletConsentJournalName, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, snWalletConsentMaxBytes+1))
	if len(raw) > snWalletConsentMaxBytes {
		readErr = errors.Join(readErr, errors.New("wallet consent journal exceeds its finite byte bound"))
	}
	if err := errors.Join(readErr, file.Sync(), file.Close(), self.directory.Sync(), self.parent.Sync(), self.check()); err != nil {
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
		previous, err := self.open(snWalletConsentJournalName, unix.O_RDONLY)
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
	file, err := self.open(snWalletConsentPendingName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			err := unix.Unlinkat(int(self.directory.Fd()), snWalletConsentPendingName, 0)
			if !errors.Is(err, os.ErrNotExist) {
				returnErr = errors.Join(returnErr, err, self.directory.Sync())
			}
		}
	}()
	written, writeErr := file.Write(raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close(), self.check(), checkPrevious()); err != nil {
		return err
	}
	if err := unix.Renameat(int(self.directory.Fd()), snWalletConsentPendingName, int(self.directory.Fd()), snWalletConsentJournalName); err != nil {
		return err
	}
	return errors.Join(self.directory.Sync(), self.parent.Sync(), self.check())
}

// Closing the locked directory releases the lease even after process death.
func (self *snWalletConsentStore) close() error {
	if self == nil {
		return nil
	}
	var err error
	if self.directory != nil {
		err = self.directory.Close()
		self.directory = nil
	}
	if self.parent != nil {
		err = errors.Join(err, self.parent.Close())
		self.parent = nil
	}
	return err
}
