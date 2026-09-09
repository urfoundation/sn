//go:build !linux && !darwin

package validator

// Legacy Stats API compatibility is intentionally weaker on other platforms:
// it retains the old pathname writer. New ordinary-v2 and actual v6 snapshots
// refuse before mutation; this adapter is not qualified release custody.

import (
	"errors"
	"path/filepath"
)

// The compatibility owner preserves the private callback shape only; it does
// not claim descriptor-relative protection unavailable on this platform.
type statsSnapshotDirectory struct {
	path    string
	written bool
}

// Explicit new authority is never silently routed through the legacy writer.
func acquireStatsSnapshotDirectory(path string, physical bool, _ statsSnapshotIOHooks) (*statsSnapshotDirectory, error) {
	if physical {
		return nil, errAttemptPrivateDirectoryPlatform
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &statsSnapshotDirectory{path: absolute}, nil
}

// Check the actual encoded version even when a compatibility owner was
// acquired before the snapshot version became available.
func (self *statsSnapshotDirectory) admit(write statsSnapshotWrite) error {
	if write.version >= 6 {
		return errAttemptPrivateDirectoryPlatform
	}
	absolute, err := filepath.Abs(write.path)
	if err != nil {
		return err
	}
	if self == nil || self.written || absolute != self.path {
		return errors.New("statistics compatibility snapshot target differs")
	}
	return nil
}

// Only the legacy public API retains its original pathname behavior.
func writeStatsSnapshotOwned(self *statsSnapshotDirectory, write statsSnapshotWrite) error {
	if err := self.admit(write); err != nil {
		return err
	}
	if err := atomicStateWrite(write.path, write.data, 0o600); err != nil {
		return err
	}
	self.written = true
	return nil
}

// Compatibility has no retained native descriptor to validate or close.
func (self *statsSnapshotDirectory) check() error { return nil }

// No native final-witness guarantee is claimed by this legacy-only adapter.
func (self *statsSnapshotDirectory) checkFinal() error { return nil }

// Actual pathname writer cleanup remains the old platform behavior.
func (self *statsSnapshotDirectory) finish() error { return nil }
