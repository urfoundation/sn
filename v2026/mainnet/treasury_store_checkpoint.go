// Retained native intent keeps its physical marker, private directory and exact
// predecessor throughout local ownership. Cross-host rollback needs its own fence.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Restoring a pathname after observed custody loss cannot revive this instance.
func (self *treasuryStore) failIntegrity(err error) error {
	if mainnetDurableAdmissionPending(err) {
		return err
	}
	if errors.Is(err, errMainnetDurablePublicationUncertain) && !errors.Is(err, durablevolume.ErrIdentity) && !errors.Is(err, errRpcIntegrity) {
		return err
	}
	if self.failed == nil {
		self.failed = fmt.Errorf("%w: treasury original custody: %w", errRpcIntegrity, err)
	}
	return self.failed
}

// A flock does not follow replacement of its name or its containing directory.
func (self *treasuryStore) checkpoint() error {
	if self == nil || self.lock == nil || self.directory == nil {
		return errors.New("treasury store is closed")
	}
	if self.failed != nil {
		return self.failed
	}
	if err := self.storage.checkWrite(self.directory); err != nil {
		return err
	}
	if err := self.marker.checkpoint(); err != nil {
		return self.failIntegrity(err)
	}
	var directory unix.Stat_t
	if err := unix.Fstat(int(self.directory.Fd()), &directory); err != nil {
		return self.failIntegrity(err)
	}
	if uint64(directory.Dev) != self.marker.root.Device || directory.Ino != self.marker.root.Inode || directory.Mode&0077 != 0 {
		return self.failIntegrity(errors.New("physical directory descriptor changed"))
	}
	return nil
}

// Only an incomplete empty initial claim may lack its first reserved record.
func (self *treasuryStore) readRecord() ([]byte, error) {
	if err := self.checkpoint(); err != nil {
		return nil, err
	}
	raw, _, err := self.storage.readFile(context.Background(), self.config.Action.StatePath, treasuryStoreLimit)
	if err != nil && !(errors.Is(err, os.ErrNotExist) && !self.complete && self.expectedHash == "") {
		return nil, self.failIntegrity(err)
	}
	return raw, err
}

// A durable successor replaces only its still-retained original predecessor;
// no in-memory signature or receipt authorizes recreating a deleted journal.
func (self *treasuryStore) publishRecord(record treasuryRecord, raw []byte) error {
	checkPrevious := func() error {
		_, err := self.load()
		if errors.Is(err, os.ErrNotExist) && !self.complete && self.expectedHash == "" && record.Phase == "reserved" && record.Signature == "" {
			return self.checkpoint()
		}
		return err
	}
	if err := checkPrevious(); err != nil {
		return err
	}
	if err := self.storage.publish(self.config.Action.StatePath, raw, self.syncDirectory); err != nil {
		return err
	}
	self.expectedHash = monitorReadDigest(raw)
	_, err := self.load()
	return err
}
