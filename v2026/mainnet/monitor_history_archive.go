// Immutable history segments borrow the existing provisioned snapshot format.
// Readers authenticate segments at admission and retain shared named-inode
// custody; unchanged samples never reread archived payloads.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const maximumMonitorHistorySegments = 128
const maximumMonitorHistoryPath = 1024

// Instance-local observation barriers cannot supply bytes, errors or admission
// verdicts. The real guarded read always follows the callback.
func (self monitorServiceHooks) beforeHistoryRead(role, step string) {
	if self.historyRead != nil {
		self.historyRead(role, step)
	}
}

// A reference authenticates exact original bytes, not rewritten event data.
// The catalog, each segment, descriptor count and pathname are separately bound.
type monitorHistoryReference struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Bytes  uint64 `json:"bytes"`
}

func (self monitorHistoryReference) validate() error {
	return self.validateLimit(maxRpcReplyBytes)
}

func (self monitorHistoryReference) validateLimit(maximum uint64) error {
	if maximum == 0 {
		maximum = maxRpcReplyBytes
	}
	if maximum > economicConservationStorageMaximum || !monitorHistoryPath(self.Path) || self.Bytes == 0 || self.Bytes > maximum || !planSha256(self.Sha256) {
		return errors.New("monitor history reference is not an exact bounded private snapshot")
	}
	return nil
}

func monitorHistoryPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && len(path) <= maximumMonitorHistoryPath && len(filepath.Base(path)) <= 155 && !strings.ContainsAny(path, "\x00\r\n")
}

// The enclosing synchronous owner joins operations before close. Read-only
// admission never repairs a pending head or acquires writer authority.
type monitorHistorySnapshot struct {
	storage  *mainnetDurableDirectory
	lock     *os.File
	path     string
	readOnly bool
}

// A private instance barrier can withhold admission after the actual read. It
// cannot provide bytes or create authority; tests can close the borrowed real
// lock here to exercise an actual nested cleanup failure without timing races.
type monitorHistoryAdmissionReadKey struct{}

func openMonitorHistorySnapshot(ctx context.Context, path string, write bool) (*monitorHistorySnapshot, error) {
	return openMonitorHistorySnapshotProfile(ctx, path, write, "mainnet-monitor-checkpoint", maxRpcReplyBytes)
}

func openMonitorHistorySnapshotProfile(ctx context.Context, path string, write bool, kind string, maximum int) (_ *monitorHistorySnapshot, resultErr error) {
	if err := validateMonitorCheckpointProfile(kind, maximum); err != nil {
		return nil, err
	}
	if !monitorHistoryPath(path) {
		return nil, errors.New("monitor history path must be bounded canonical absolute")
	}
	access, flags := durablevolume.ReadOnly, os.O_RDONLY
	if write {
		access, flags = durablevolume.ReadWrite, os.O_RDWR
	}
	storage, err := openMainnetDurableDirectory(ctx, filepath.Dir(path), access)
	if err != nil {
		return nil, err
	}
	self := &monitorHistorySnapshot{storage: storage, path: path, readOnly: !write}
	defer func() {
		if resultErr != nil {
			resultErr = monitorAdmissionFailure(resultErr, self.close())
		}
	}()
	fd, err := storage.openFile(path+".lock", flags, 0)
	if err != nil {
		return nil, storage.snapshotError(monitorNamedObservation(err))
	}
	self.lock = os.NewFile(uintptr(fd), path+".lock")
	if err := storage.bindMarker(self.lock, false); err != nil {
		return nil, err
	}
	if err := storage.bindSnapshotMode(path, kind, maximum, false, write, !write); err != nil {
		return nil, err
	}
	return self, nil
}

func (self *monitorHistorySnapshot) read() ([]byte, bool, error) {
	if self == nil || self.storage == nil {
		return nil, false, os.ErrClosed
	}
	raw, present, err := self.storage.head.Read()
	return raw, present, self.storage.snapshotError(err)
}

func (self *monitorHistorySnapshot) check() error {
	if self == nil || self.storage == nil {
		return os.ErrClosed
	}
	return self.storage.check(nil)
}

func (self *monitorHistorySnapshot) publish(raw []byte, afterSync func(*os.File) error) error {
	if self == nil || self.storage == nil || self.readOnly {
		return errors.New("monitor history reader cannot publish")
	}
	return self.storage.publish(self.path, raw, afterSync)
}

func (self *monitorHistorySnapshot) close() error {
	if self == nil || self.storage == nil {
		return nil
	}
	err := self.storage.close()
	self.storage = nil
	if self.lock != nil {
		err = errors.Join(err, self.lock.Close())
		self.lock = nil
	}
	return err
}

// The returned raw bytes are admission-only. Callers retain the reader and
// discard decoded archive payloads after authenticating their bounded chain.
func openMonitorHistoryReader(ctx context.Context, reference monitorHistoryReference) (*monitorHistorySnapshot, []byte, error) {
	return openMonitorHistoryReaderProfile(ctx, reference, "mainnet-monitor-checkpoint", maxRpcReplyBytes)
}

func openMonitorHistoryReaderProfile(ctx context.Context, reference monitorHistoryReference, kind string, maximum int) (_ *monitorHistorySnapshot, _ []byte, resultErr error) {
	if err := reference.validateLimit(uint64(maximum)); err != nil {
		return nil, nil, err
	}
	owner, err := openMonitorHistorySnapshotProfile(ctx, reference.Path, false, kind, maximum)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = monitorAdmissionFailure(resultErr, owner.close())
		}
	}()
	raw, present, err := owner.read()
	if after, ok := ctx.Value(monitorHistoryAdmissionReadKey{}).(func(string, *os.File) error); err == nil && ok && after != nil {
		err = after(owner.path, owner.lock)
	}
	if err != nil {
		return nil, nil, err
	}
	if !present || uint64(len(raw)) != reference.Bytes || monitorReadDigest(raw) != reference.Sha256 {
		return nil, nil, errors.Join(durablevolume.ErrIdentity, errors.New("monitor archive differs from its exact retained reference"))
	}
	return owner, raw, nil
}
