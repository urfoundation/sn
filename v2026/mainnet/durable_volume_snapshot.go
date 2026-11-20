// Mainnet snapshots preserve their existing serialized approval and intent
// bytes. An independently provisioned head retains which generation completed.
package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Prepared markers are empty until their specific original claim is retained.
// Child progress or a nonempty marker still forbids an interrupted parent from
// refreshing its own reservation. Each child later authenticates its own head.
func requireFreshSnapshotPaths(paths []string) error {
	for _, path := range paths {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errors.New("retained child journal requires resume"), err)
		}
		marker, err := os.Lstat(path + ".lock")
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errors.Join(durablevolume.ErrIdentity, errors.New("prepared child marker is absent"), err)
			}
			return mainnetDurableUnavailable("cannot observe prepared child marker", err)
		}
		stat, ok := marker.Sys().(*syscall.Stat_t)
		if !ok || !marker.Mode().IsRegular() || marker.Mode().Perm()&0077 != 0 || stat.Nlink != 1 || marker.Size() != 0 {
			return errors.New("child marker already retains a claim or is not a private prepared inode")
		}
	}
	return nil
}

// An observed missing marker is never an opportunity to create a new owner.
// Provisioning is a separate explicit operation, before runtime admission.
func (self *mainnetDurableDirectory) openSnapshotMarker(path string) (*os.File, error) {
	fd, err := self.openFile(path+".lock", os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, self.identity("preprovisioned snapshot marker is missing", err)
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), path+".lock"), nil
}

// Open asserts the original application lock before any marker or state write.
// Reconciliation is attempted once only after a failed fresh opener released
// its internal state, while this caller retains the original exclusive lock.
func (self *mainnetDurableDirectory) bindSnapshot(path, kind string, maximum int, create bool) error {
	return self.bindSnapshotMode(path, kind, maximum, create, !create, false)
}

// Passive readiness may inspect a committed head; it cannot reconcile pending
// writes or silently turn a reader into a recovery writer.
func (self *mainnetDurableDirectory) bindSnapshotRead(path, kind string, maximum int) error {
	return self.bindSnapshotMode(path, kind, maximum, false, false, true)
}

func (self *mainnetDurableDirectory) bindSnapshotMode(path, kind string, maximum int, create, reconcile, readOnly bool) error {
	if self == nil || self.marker == nil || self.head != nil || filepath.Dir(path) != self.path {
		return errors.New("snapshot requires one original marked directory owner")
	}
	name := filepath.Base(path)
	spec := durablehead.Spec{Kind: kind, Name: name, MaximumBytes: int64(maximum), LockName: name + ".lock", AuxiliaryNames: []string{name + ".lock"}}
	open := durablehead.Open
	if readOnly {
		open = durablehead.OpenReadOnly
	}
	head, err := open(self.ctx, self.directory, self.marker, spec)
	if errors.Is(err, durablehead.ErrUncertain) && !errors.Is(err, durablevolume.ErrIdentity) && reconcile {
		head, err = durablehead.Reconcile(self.ctx, self.directory, self.marker, spec)
	}
	if err != nil {
		return self.snapshotError(err)
	}
	self.head, self.headPath = head, path
	if create {
		_, present, err := head.Read()
		if err != nil {
			return self.snapshotError(err)
		}
		if present {
			return errors.New("snapshot already retains original custody; use resume")
		}
		info, err := self.marker.Stat()
		if err != nil {
			return mainnetDurableUnavailable("cannot observe fresh snapshot marker", err)
		}
		if info.Size() != 0 {
			return errors.New("snapshot marker already reserves original custody; use resume")
		}
	}
	return nil
}

func (self *mainnetDurableDirectory) snapshotError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, durablehead.ErrUncertain) {
		err = errors.Join(errMainnetDurablePublicationUncertain, err)
	}
	if errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, errMainnetDurablePublicationUncertain) {
		self.failed = errors.Join(self.failed, err)
		return self.failed
	}
	return err
}

// The application still owns its marker grammar. This operation can only
// write the original provisioned auxiliary inode and performs a real fsync.
func (self *mainnetDurableDirectory) writeMarkerAt(raw []byte, offset int64) (written int, resultErr error) {
	if self == nil || self.head == nil || self.marker == nil {
		return 0, errors.New("marker write requires its retained snapshot authority")
	}
	if offset < 0 || offset+int64(len(raw)) > 4096 {
		return 0, errors.New("marker write exceeds its fixed bound")
	}
	if err := self.checkWrite(nil); err != nil {
		return 0, err
	}
	err := self.head.WithAuxiliary(filepath.Base(self.marker.Name()), true, func(file *os.File) error {
		var err error
		written, err = file.WriteAt(raw, offset)
		if err == nil && written != len(raw) {
			err = io.ErrShortWrite
		}
		return err
	})
	return written, self.snapshotError(err)
}
