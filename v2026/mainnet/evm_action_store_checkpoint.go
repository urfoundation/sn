// Descriptor-relative original action custody refuses missing journals and
// replaced physical fences throughout an owner's lifetime.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// A held flock protects its inode only. The approved path must still name that
// private single-link marker under the same physical directory.
func (self *evmActionStore) checkpoint() error {
	if self != nil {
		if err := self.storage.checkWrite(self.directory); err != nil {
			return err
		}
	}
	if self == nil || self.lock == nil || self.directory == nil {
		return errors.New("EVM journal is closed")
	}
	physical, err := bootstrapSuccessorPhysicalRoot(self.config.Plan.RunDirectory)
	if err != nil || physical != self.root {
		return errors.Join(errors.New("EVM journal physical directory changed"), err)
	}
	var directoryStat, markerStat, namedStat unix.Stat_t
	if err := unix.Fstat(int(self.directory.Fd()), &directoryStat); err != nil {
		return err
	}
	if uint64(directoryStat.Dev) != self.root.Device || directoryStat.Ino != self.root.Inode || directoryStat.Mode&0077 != 0 {
		return errors.New("EVM journal directory descriptor changed")
	}
	if err := bootstrapSuccessorPrivateRegular(self.lock); err != nil {
		return err
	}
	if err := errors.Join(unix.Fstat(int(self.lock.Fd()), &markerStat),
		unix.Fstatat(int(self.directory.Fd()), filepath.Base(self.path)+".lock", &namedStat, unix.AT_SYMLINK_NOFOLLOW)); err != nil {
		return err
	}
	if markerStat.Dev != namedStat.Dev || markerStat.Ino != namedStat.Ino || markerStat.Size != int64(len(self.marker)) {
		return errors.New("EVM journal ownership marker changed")
	}
	raw, err := io.ReadAll(io.NewSectionReader(self.lock, 0, int64(len(self.marker))+1))
	if err != nil || string(raw) != self.marker {
		return errors.Join(errors.New("EVM journal ownership marker bytes changed"), err)
	}
	return nil
}

// Reads share the acquired directory and reject links or a changed path while
// retaining the same bounded record format used by existing approvals.
func (self *evmActionStore) readRecord() ([]byte, error) {
	if err := self.checkpoint(); err != nil {
		return nil, err
	}
	raw, _, err := self.storage.readFile(context.Background(), self.path, 512*1024)
	if errors.Is(err, os.ErrNotExist) && self.complete {
		err = self.storage.identity("completed EVM journal disappeared", err)
	}
	return raw, err
}

// Only an incomplete, never-loaded initial claim may create its first journal.
// Every later write compares the exact retained predecessor before replacement
// and reopens the published result after directory durability acknowledgement.
func (self *evmActionStore) publishRecord(record evmActionRecord, raw []byte) error {
	checkPrevious := func() error {
		_, err := self.load()
		if errors.Is(err, os.ErrNotExist) && !self.complete && self.retainedHash == "" && record.Signed == "" {
			return self.checkpoint()
		}
		return err
	}
	if err := checkPrevious(); err != nil {
		return err
	}
	if err := self.storage.publish(self.path, raw, self.syncDirectory); err != nil {
		return err
	}
	self.retainedHash = record.ContentHash
	_, err := self.load()
	return err
}
