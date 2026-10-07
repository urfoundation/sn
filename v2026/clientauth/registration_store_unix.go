//go:build linux || darwin

package clientauth

// One descriptor-owned private directory and nonblocking advisory lock retain
// registration custody across its request and credential handoff. Every write
// syncs both the file and directory before its caller may use the new state.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const maximumRegistrationBytes = 32 * 1024

type registrationStore struct {
	directory *os.File
	lock      *os.File
	path      string
	client    string
	anchor    unix.Stat_t
}

// Refuse namespace redirection before opening secrets or a durable request.
func openRegistrationStore(clientPath string) (_ *registrationStore, returnErr error) {
	if !filepath.IsAbs(clientPath) || filepath.Clean(clientPath) != clientPath || filepath.Dir(clientPath) == clientPath {
		return nil, errors.New("registration credential path is not canonical absolute")
	}
	dir := filepath.Dir(clientPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), string(filepath.Separator))
	for _, component := range strings.Split(strings.TrimPrefix(dir, string(filepath.Separator)), string(filepath.Separator)) {
		next, openErr := unix.Openat(int(current.Fd()), component, flags, 0)
		closeErr := current.Close()
		if openErr != nil {
			return nil, errors.Join(openErr, closeErr)
		}
		current = os.NewFile(uintptr(next), dir)
		if closeErr != nil {
			return nil, errors.Join(closeErr, current.Close())
		}
	}
	self := &registrationStore{directory: current, path: dir, client: filepath.Base(clientPath)}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.close())
		}
	}()
	if err := unix.Fstat(int(current.Fd()), &self.anchor); err != nil {
		return nil, err
	}
	if self.anchor.Mode&unix.S_IFMT != unix.S_IFDIR || self.anchor.Uid != uint32(os.Geteuid()) || self.anchor.Mode&0022 != 0 {
		return nil, errors.New("registration parent must be owned and not writable by other users")
	}
	self.lock, err = self.open(self.client+".registration.lock", unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(self.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.New("registration credential already has an active owner")
	}
	return self, nil
}

// Provider slots borrow the key owner's physical directory, never a fresh
// pathname selection. The descriptor remains tied to that inode across reads,
// writes and retries; check refuses a renamed/replaced visible namespace.
func openRegistrationStoreForOwner(clientPath string, owner *registrationStore) (_ *registrationStore, returnErr error) {
	if owner == nil || owner.directory == nil || owner.lock == nil || !filepath.IsAbs(clientPath) || filepath.Clean(clientPath) != clientPath || filepath.Dir(clientPath) != owner.path {
		return nil, errors.New("provider client custody differs from its retained directory owner")
	}
	if err := owner.check(); err != nil {
		return nil, err
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fd, err := unix.Openat(int(owner.directory.Fd()), ".", flags, 0)
	if err != nil {
		return nil, err
	}
	self := &registrationStore{directory: os.NewFile(uintptr(fd), owner.path), path: owner.path, client: filepath.Base(clientPath), anchor: owner.anchor}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.close())
		}
	}()
	if err := self.check(); err != nil {
		return nil, err
	}
	self.lock, err = self.open(self.client+".registration.lock", unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(self.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.New("registration credential already has an active owner")
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	return self, nil
}

// All leaf opens are nonblocking/no-follow and then check descriptor metadata.
func (self *registrationStore) open(name string, flags int) (*os.File, error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return nil, errors.New("registration leaf is invalid")
	}
	fd, err := unix.Openat(int(self.directory.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(self.path, name))
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 || stat.Nlink != 1 || stat.Size < 0 || stat.Size > maximumRegistrationBytes {
		return nil, errors.Join(errors.New("registration file has unsafe ownership, mode, links or size"), file.Close())
	}
	return file, nil
}

// Reopen only to authenticate namespace identity; operations keep using the
// original descriptor even if a path is maliciously changed between checks.
func (self *registrationStore) check() error {
	info, err := os.Lstat(self.path)
	if err != nil {
		return err
	}
	var original unix.Stat_t
	if err := unix.Fstat(int(self.directory.Fd()), &original); err != nil {
		return err
	}
	owned, err := self.directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, owned) || original.Dev != self.anchor.Dev || original.Ino != self.anchor.Ino || original.Uid != self.anchor.Uid || original.Mode != self.anchor.Mode {
		return errors.New("registration custody directory identity changed")
	}
	return nil
}

func (self *registrationStore) read(name string) ([]byte, error) {
	if err := self.check(); err != nil {
		return nil, err
	}
	file, err := self.open(name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maximumRegistrationBytes+1))
	closeErr := file.Close()
	if len(raw) > maximumRegistrationBytes {
		readErr = errors.Join(readErr, errors.New("registration data exceeds its finite bound"))
	}
	if err := errors.Join(readErr, closeErr, self.check()); err != nil {
		return nil, err
	}
	return raw, nil
}

// A single bounded census detects old identity files before first-key creation.
func (self *registrationStore) names(maximum int) ([]string, error) {
	if maximum < 1 || maximum > 4096 {
		return nil, errors.New("registration directory census bound differs")
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	if _, err := self.directory.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	names, err := self.directory.Readdirnames(maximum + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > maximum {
		return nil, errors.New("registration directory census exceeds its bound")
	}
	return names, self.check()
}

// Deletion uses the same owned directory as publication. A missing leaf is
// already absent; a successful unlink is durable before the caller proceeds.
func (self *registrationStore) remove(name string) error {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return errors.New("registration removal leaf is invalid")
	}
	if err := self.check(); err != nil {
		return err
	}
	err := unix.Unlinkat(int(self.directory.Fd()), name, 0)
	if errors.Is(err, unix.ENOENT) {
		return self.check()
	}
	if err != nil {
		return err
	}
	return errors.Join(self.directory.Sync(), self.check())
}

// Rename publishes complete bytes; a failed directory sync remains an error
// and never licenses a second operation or a credential before durable handoff.
func (self *registrationStore) write(name string, raw []byte) (returnErr error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." || len(raw) == 0 || len(raw) > maximumRegistrationBytes {
		return errors.New("registration write exceeds its finite bound")
	}
	if err := self.check(); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := ".registration-" + hex.EncodeToString(random[:])
	file, err := self.open(temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			_ = unix.Unlinkat(int(self.directory.Fd()), temporary, 0)
		}
	}()
	_, writeErr := io.Copy(file, bytes.NewReader(raw))
	if err := errors.Join(writeErr, file.Sync(), file.Close(), self.check()); err != nil {
		return err
	}
	if err := unix.Renameat(int(self.directory.Fd()), temporary, int(self.directory.Fd()), name); err != nil {
		return err
	}
	return errors.Join(self.directory.Sync(), self.check())
}

// Releasing the lock and directory is part of the caller's completed result.
func (self *registrationStore) close() error {
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
	return err
}
