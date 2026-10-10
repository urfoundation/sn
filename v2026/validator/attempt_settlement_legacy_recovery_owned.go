//go:build linux || darwin

package validator

// V1 recovery preserves stable legacy ancestor aliases but retains their
// actual native directories through every write. V2 never uses this fallback.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Logical names are rechecked against their initially selected physical owner.
type legacyAttemptSettlementDirectory struct {
	logical string
	initial os.FileInfo
	root    *attemptPrivateDirectory
}

// Only legacy ancestor resolution is compatible here; the actual final
// directory may not be a symlink or an unknown/unavailable target.
func openLegacyAttemptSettlementDirectory(path string) (*legacyAttemptSettlementDirectory, error) {
	initial, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !initial.IsDir() || initial.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("legacy settlement directory is not an actual directory")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	root, err := openAttemptPrivateDirectory(resolved)
	if err != nil {
		return nil, err
	}
	opened, err := root.file.Stat()
	if err != nil || !os.SameFile(initial, opened) || initial.Mode() != opened.Mode() {
		return nil, errors.Join(errors.New("legacy settlement directory changed during admission"), err, root.close())
	}
	return &legacyAttemptSettlementDirectory{logical: path, initial: initial, root: root}, nil
}

// Retargeting an admitted ancestor cannot redirect any later target or journal
// operation, even when the original physical directory still exists.
func (self *legacyAttemptSettlementDirectory) check() error {
	current, err := os.Lstat(self.logical)
	if err != nil || !os.SameFile(self.initial, current) || self.initial.Mode() != current.Mode() {
		return errors.Join(errors.New("legacy settlement logical directory changed after admission"), err)
	}
	return self.root.check()
}

// Canonical known-version snapshots only. This never interprets invalid,
// unreadable or compact state as an absent v1 target eligible for repair.
func admitLegacyAttemptSettlementSnapshot(encoded []byte) error {
	var snapshot statsSnapshot
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return err
	}
	if snapshot.Version < 1 || snapshot.Version > 5 || snapshot.AttemptV2 != nil {
		return errors.New("legacy settlement recovery cannot replace compact or unsupported statistics")
	}
	canonical, err := encodeStatsSnapshot(snapshot)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return errors.Join(errors.New("legacy settlement recovery target is not canonical statistics"), err)
	}
	return nil
}

// All participants, current files and the journal are admitted before the
// first write. Native targets/closure/removal retain exactly those owners.
func recoverAttemptSettlementEpochOwned(coordinator string, participants []AttemptSettlementParticipant, removeBoundary func(string) error, step func(string) error) (resultErr error) {
	ordered, err := validateAttemptSettlementParticipants(participants, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(coordinator); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	parent, err := openLegacyAttemptSettlementDirectory(coordinator)
	if err != nil {
		return err
	}
	owners := []*legacyAttemptSettlementDirectory{parent}
	defer func() {
		for index := len(owners) - 1; index >= 0; index-- {
			resultErr = errors.Join(resultErr, owners[index].root.close())
		}
	}()
	physical := attemptSettlementV2PhysicalIO()
	physical.roots = map[string]*attemptPrivateDirectory{coordinator: parent.root}
	read := func(owner *legacyAttemptSettlementDirectory, name string) ([]byte, bool, error) {
		if err := owner.check(); err != nil {
			return nil, false, err
		}
		data, exists, err := readAttemptPrivateMetadata(context.Background(), owner.root, name, 0, attemptPrivateMetadataReadIO{closeFile: physical.closeFile})
		return data, exists, errors.Join(err, owner.check())
	}
	encoded, exists, err := read(parent, "settlement-transaction.json")
	if err != nil || !exists {
		return err
	}
	transaction, err := decodeAttemptSettlementTransaction(encoded, ordered)
	if err != nil {
		return err
	}
	images := make([][]byte, len(ordered))
	present := make([]bool, len(ordered))
	for index, participant := range ordered {
		owner, err := openLegacyAttemptSettlementDirectory(participant.StateDir)
		if err != nil {
			return err
		}
		for _, prior := range owners[1:] {
			if owner.root.anchor.dev == prior.root.anchor.dev && owner.root.anchor.ino == prior.root.anchor.ino {
				return errors.Join(errors.New("legacy settlement participants alias one physical directory"), owner.root.close())
			}
		}
		owners = append(owners, owner)
		physical.roots[participant.StateDir] = owner.root
		images[index], present[index], err = read(owner, "stats.json")
		if err != nil {
			return err
		}
		if present[index] {
			if err := admitLegacyAttemptSettlementSnapshot(images[index]); err != nil {
				return err
			}
		}
	}
	check := func() error {
		for _, owner := range owners {
			if err := owner.check(); err != nil {
				return err
			}
		}
		current, exists, err := read(parent, "settlement-transaction.json")
		if err != nil || !exists || !bytes.Equal(current, encoded) {
			return errors.Join(errors.New("legacy settlement journal changed after admission"), err)
		}
		for index := range ordered {
			current, exists, err := read(owners[index+1], "stats.json")
			if err != nil || exists != present[index] || !bytes.Equal(current, images[index]) {
				return errors.Join(errors.New("legacy settlement target changed after admission"), err)
			}
		}
		return nil
	}
	if step != nil {
		if err := step("before-first-write"); err != nil {
			return err
		}
	}
	if err := check(); err != nil {
		return err
	}
	for index, snapshot := range transaction.Snapshots {
		if err := check(); err != nil {
			return err
		}
		if err := writeAttemptSettlementV2OwnedState(owners[index+1].root, "stats.json", snapshot.StatsJSON); err != nil {
			return err
		}
		images[index], present[index] = snapshot.StatsJSON, true
		if step != nil {
			if err := step("after-snapshot-write"); err != nil {
				return err
			}
		}
	}
	if err := check(); err != nil {
		return err
	}
	closure, err := attemptSettlementClosureFromTransaction(transaction)
	if err != nil {
		return err
	}
	if closure != nil {
		data, err := json.Marshal(closure)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := publishAttemptSettlementOwnedClosure(coordinator, "settlement-closures", closure.Epoch, data, uint64(len(data)), physical); err != nil {
			return err
		}
	}
	if err := check(); err != nil {
		return err
	}
	if removeBoundary != nil {
		if err := removeBoundary(attemptSettlementTransactionPath(coordinator)); err != nil {
			return err
		}
		for _, owner := range owners {
			if err := owner.check(); err != nil {
				return err
			}
		}
	}
	if err := unix.Unlinkat(int(parent.root.file.Fd()), "settlement-transaction.json", 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := parent.root.file.Sync(); err != nil {
		return err
	}
	return parent.check()
}
