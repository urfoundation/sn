// Native custody retains the physical marker and exact preceding journal across
// publication. All file effects stay relative to the acquired private directory.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Observed custody loss permanently poisons this instance, even after restoration.
func (self *ownerTrimStore) failIntegrity(err error) error {
	if mainnetDurableAdmissionPending(err) {
		return err
	}
	if errors.Is(err, errMainnetDurablePublicationUncertain) && !errors.Is(err, durablevolume.ErrIdentity) && !errors.Is(err, errRpcIntegrity) {
		return err
	}
	if self.failed == nil {
		self.failed = fmt.Errorf("%w: owner trim original custody: %w", errRpcIntegrity, err)
	}
	return self.failed
}

// A held flock does not follow a renamed pathname or a replaced parent.
func (self *ownerTrimStore) checkpoint() error {
	if self == nil || self.lock == nil || self.directory == nil {
		return errors.New("owner trim store is closed")
	}
	if self.failed != nil {
		return self.failed
	}
	if err := self.storage.checkWrite(self.directory); err != nil {
		return err
	}
	if err := self.retained.checkpoint(context.Background()); err != nil {
		return self.failIntegrity(err)
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

// Only the original incomplete empty claim may lack its first reserved record.
// Completed or already observed custody cannot be reconstructed from memory.
func (self *ownerTrimStore) readRecord() ([]byte, error) {
	if err := self.checkpoint(); err != nil {
		return nil, err
	}
	raw, _, err := self.storage.readFile(context.Background(), self.config.Action.StatePath, ownerTrimStoreLimit)
	if err != nil && !(errors.Is(err, os.ErrNotExist) && !self.complete && self.expectedHash == "") {
		return nil, self.failIntegrity(err)
	}
	return raw, err
}

// Validate the same predecessor before rename and reread after directory sync.
// A successful write never grants permission to recreate a missing signed row.
func (self *ownerTrimStore) publishRecord(record ownerTrimRecord, raw []byte) error {
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
