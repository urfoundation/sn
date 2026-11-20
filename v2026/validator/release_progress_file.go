//go:build linux || darwin

// The optional publisher retries ordinary local I/O without canceling its
// producer. Its descriptor owner refuses path replacement and foreign content.
package validator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/protocol"
)

const releaseProgressHeartbeat = 30 * time.Second
const releaseProgressMaximumBackoff = time.Minute

// Ownership faults stop only this publisher. Other failures leave the last
// complete output aging while a finite backoff allows operational repair.
type releaseProgressOwnershipError struct{ reason string }

// The local cause is never copied into the exported status or fixed log code.
func (self *releaseProgressOwnershipError) Error() string { return self.reason }

// One worker owns one local output until close joins it. Test hooks replace
// only the wait and physical durability boundary, never protocol observations.
type releaseProgressPublisher struct {
	progress *releaseProgress
	path     string
	stateDir string
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	report   func(string)
	wait     func(context.Context, time.Duration) bool
	sync     func(*os.File) error
}

// Starting this worker cannot cancel its parent or acquire a protocol store.
func newReleaseProgressPublisher(ctx context.Context, progress *releaseProgress, path, stateDir string, report func(string)) *releaseProgressPublisher {
	ctx, cancel := context.WithCancel(ctx)
	self := &releaseProgressPublisher{progress: progress, path: path, stateDir: stateDir,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), report: report,
		wait: func(ctx context.Context, duration time.Duration) bool {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(duration):
				return true
			}
		}}
	go self.run()
	return self
}

// Cancellation joins the actual publisher before its parent releases resources.
func (self *releaseProgressPublisher) close() {
	self.cancel()
	<-self.done
}

// Successful recovery is visible once; failed attempts are rate limited by the
// same owned wait. Raw errors and arbitrary path strings are not emitted.
func (self *releaseProgressPublisher) run() {
	defer close(self.done)
	var store *releaseProgressFile
	defer func() {
		if store != nil {
			if err := store.close(); err != nil {
				self.report("close_error")
			}
		}
	}()
	backoff, failed := time.Second, false
	for self.ctx.Err() == nil {
		var err error
		if store == nil {
			var previous *protocol.ValidatorProgress
			store, previous, err = openReleaseProgressFile(self.path, self.stateDir, self.progress.value.Source)
			if err == nil && previous != nil {
				self.progress.retain(previous)
			}
		}
		if err == nil {
			var raw []byte
			raw, err = self.progress.snapshot()
			if err == nil {
				err = store.save(raw, self.sync)
			}
		}
		if self.ctx.Err() != nil {
			return
		}
		self.progress.observePublication(err == nil)
		delay := releaseProgressHeartbeat
		if err != nil {
			var ownership *releaseProgressOwnershipError
			if errors.As(err, &ownership) {
				self.report("publisher_disabled_ownership")
				return
			}
			self.report("publication_unavailable_retrying")
			failed, delay = true, backoff
			backoff = min(2*backoff, releaseProgressMaximumBackoff)
		} else {
			if failed {
				self.report("publication_recovered")
			}
			failed, backoff = false, time.Second
		}
		if !self.wait(self.ctx, delay) {
			return
		}
	}
}

// Open directory descriptors anchor all temporary and destination operations.
// A process lock, exact previous bytes and path identity form the local owner.
type releaseProgressFile struct {
	path          string
	name          string
	directory     *os.File
	directoryInfo os.FileInfo
	lock          *os.File
	lockInfo      os.FileInfo
	witness       os.FileInfo
	digest        [32]byte
}

// Source renewal within the same role can retain operational age, but never
// changes the original configuration hash inside an already observed intent.
func openReleaseProgressFile(path, stateDir string, source protocol.ValidatorProgressSource) (*releaseProgressFile, *protocol.ValidatorProgress, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.HasSuffix(path, ".json") {
		return nil, nil, &releaseProgressOwnershipError{reason: "progress path must be an absolute canonical JSON file"}
	}
	directory := filepath.Dir(path)
	if relative, err := filepath.Rel(stateDir, directory); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil, &releaseProgressOwnershipError{reason: "progress output must be outside protocol state"}
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, nil, err
	}
	if resolved != directory {
		return nil, nil, &releaseProgressOwnershipError{reason: "progress directory traverses an alias"}
	}
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	self := &releaseProgressFile{path: path, name: filepath.Base(path), directory: os.NewFile(uintptr(fd), directory)}
	fail := func(err error) (*releaseProgressFile, *protocol.ValidatorProgress, error) {
		return nil, nil, errors.Join(err, self.close())
	}
	self.directoryInfo, err = self.directory.Stat()
	if err != nil {
		return fail(err)
	}
	if self.directoryInfo.Mode().Perm()&0022 != 0 {
		return fail(&releaseProgressOwnershipError{reason: "progress directory is writable by other users"})
	}
	lockFd, err := unix.Openat(fd, self.name+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return fail(&releaseProgressOwnershipError{reason: "progress lock is an alias"})
		}
		return fail(err)
	}
	self.lock = os.NewFile(uintptr(lockFd), self.name+".lock")
	self.lockInfo, err = self.lock.Stat()
	if err != nil {
		return fail(err)
	}
	if !self.lockInfo.Mode().IsRegular() || self.lockInfo.Mode().Perm()&0077 != 0 {
		return fail(&releaseProgressOwnershipError{reason: "progress lock is not private and regular"})
	}
	if err := unix.Flock(lockFd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fail(&releaseProgressOwnershipError{reason: "progress output already has an owner"})
	}
	raw, info, err := self.read()
	if err != nil {
		return fail(err)
	}
	if info == nil {
		return self, nil, nil
	}
	previous, err := protocol.DecodeValidatorProgress(raw)
	if err != nil {
		return fail(&releaseProgressOwnershipError{reason: "progress destination contains foreign or malformed bytes"})
	}
	previousSource := previous.Source
	previousSource.ConfigHash = source.ConfigHash
	if previousSource != source {
		return fail(&releaseProgressOwnershipError{reason: "progress destination belongs to another producer"})
	}
	self.witness, self.digest = info, sha256.Sum256(raw)
	return self, previous, nil
}

// Read no more than the fixed wire allowance and never follow the leaf. A
// missing destination is different from an empty or malformed observation.
func (self *releaseProgressFile) read() ([]byte, os.FileInfo, error) {
	fd, err := unix.Openat(int(self.directory.Fd()), self.name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil, nil
	}
	if errors.Is(err, unix.ELOOP) {
		return nil, nil, &releaseProgressOwnershipError{reason: "progress destination is an alias"}
	}
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), self.name)
	info, err := file.Stat()
	if err != nil {
		return nil, nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > protocol.MaxValidatorProgressBytes {
		return nil, nil, errors.Join(&releaseProgressOwnershipError{reason: "progress destination is not bounded protected regular data"}, file.Close())
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, protocol.MaxValidatorProgressBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, nil, err
	}
	if len(raw) > protocol.MaxValidatorProgressBytes {
		return nil, nil, &releaseProgressOwnershipError{reason: "progress destination grew beyond its bound"}
	}
	return raw, info, nil
}

// Validate both descriptor names before publishing. A replacement stops this
// publisher rather than overwriting content admitted by another local owner.
func (self *releaseProgressFile) check() error {
	info, err := os.Lstat(filepath.Dir(self.path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 || !os.SameFile(info, self.directoryInfo) {
		return &releaseProgressOwnershipError{reason: "progress directory identity changed"}
	}
	info, err = os.Lstat(self.path + ".lock")
	if err != nil || !os.SameFile(info, self.lockInfo) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return &releaseProgressOwnershipError{reason: "progress lock identity changed"}
	}
	raw, current, err := self.read()
	if err != nil {
		return err
	}
	if self.witness == nil && current != nil || self.witness != nil && (current == nil || !os.SameFile(current, self.witness) || sha256.Sum256(raw) != self.digest) {
		return &releaseProgressOwnershipError{reason: "progress predecessor identity or bytes changed"}
	}
	return nil
}

// Rename is atomic and bounded. A later directory-sync failure remains visible,
// while its exact visible successor becomes the predecessor of the next retry.
func (self *releaseProgressFile) save(raw []byte, syncDirectory func(*os.File) error) error {
	if len(raw) == 0 || len(raw) > protocol.MaxValidatorProgressBytes {
		return errors.New("progress publication exceeds its byte bound")
	}
	if err := self.check(); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".validator-progress-" + hex.EncodeToString(nonce[:])
	directoryFd := int(self.directory.Fd())
	fd, err := unix.Openat(directoryFd, temporary, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(directoryFd, temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	if err := file.Chmod(0644); err != nil {
		return errors.Join(err, file.Close())
	}
	written, writeErr := file.Write(raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	witness, statErr := file.Stat()
	if err := errors.Join(writeErr, statErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	if err := unix.Renameat(directoryFd, temporary, directoryFd, self.name); err != nil {
		return err
	}
	self.witness, self.digest = witness, sha256.Sum256(raw)
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return syncDirectory(self.directory)
}

// Closing releases the worker's only file ownership; no worker is detached.
func (self *releaseProgressFile) close() error {
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
