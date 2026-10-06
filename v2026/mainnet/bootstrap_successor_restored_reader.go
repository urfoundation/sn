// Only a validated restore lineage can lend a pending snapshot's passive
// preparation view. Its exact metadata descriptors remain held until join.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// This private capability is constructed after complete original-plan and
// physical restoration checks. It never permits mutation or protocol adoption.
type bootstrapSuccessorRestoredMemberView struct {
	owner     durablevolume.PreparationOwnerPlan
	inventory durablevolume.Inventory
	census    bootstrapSuccessorMemberCensus
}

type bootstrapSuccessorRestoredMetadata struct {
	name string
	file *os.File
	stat unix.Stat_t
}

// Unchanged immutable images are hashed once at admission. Every later check
// reobserves named descriptor/stat identity and exact checkpoint bytes, without
// repeating full image reads while its shared root lock excludes writers.
type bootstrapSuccessorRestoredMemberHead struct {
	storage   *mainnetDurableDirectory
	root      *os.File
	attribute string
	raw       []byte
	temporary string
	metadata  []bootstrapSuccessorRestoredMetadata
	closed    bool
}

// The caller already holds SH on the borrowed root. A completed head uses the
// ordinary reader; only an exact still-pending restored pair gets this view.
func openBootstrapSuccessorRestoredMembers(storage *mainnetDurableDirectory, root *os.File, view *bootstrapSuccessorRestoredMemberView) (_ *bootstrapSuccessorMembers, resultErr error) {
	if storage == nil || root == nil || view == nil || view.owner.Owner.Kind != "mainnet-successor-local-members" {
		return nil, errors.New("restored preparation requires its exact passive member view")
	}
	if err := storage.check(root); err != nil {
		return nil, err
	}
	spec := bootstrapSuccessorMemberSpec(false)
	attribute := durablehead.Attribute(spec.Kind, spec.Name)
	raw := make([]byte, 4097)
	n, err := unix.Fgetxattr(int(root.Fd()), attribute, raw)
	if err != nil || n < 0 || n > 4096 {
		return nil, errors.Join(errors.New("restored preparation lost original pending checkpoint"), err)
	}
	raw = raw[:n]
	var checkpoint durablehead.Checkpoint
	if err := decodePlanJson(raw, &checkpoint); err != nil {
		return nil, err
	}
	if checkpoint.Pending == nil {
		return openBootstrapSuccessorMembers(storage, root, false, true)
	}
	attributes, err := inspectStoragePreparationMembersRestore(storage.ctx, root, view.owner, view.inventory, false)
	if err != nil || len(attributes) != 1 || attributes[0].Spec.Path != "." || attributes[0].Spec.Name != attribute || !bytes.Equal(raw, attributes[0].Raw) {
		return nil, errors.Join(errors.New("restored preparation differs from its reviewed pending pair"), err)
	}
	head := &bootstrapSuccessorRestoredMemberHead{storage: storage, root: root, attribute: attribute, raw: append([]byte(nil), raw...), temporary: checkpoint.Pending.Temporary}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, head.close())
		}
	}()
	var parent unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &parent); err != nil {
		return nil, mainnetDurableUnavailable("cannot inspect restored preparation root", err)
	}
	for _, file := range view.owner.Files {
		if !storagePreparationMemberMetadataOwns(view.owner.PhysicalMetadata, file.Path) {
			continue
		}
		_, observed, err := readStoragePreparationMember(storage.ctx, root, parent, file)
		if err != nil {
			return nil, err
		}
		fd, err := unix.Openat(int(root.Fd()), file.Path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, mainnetDurableUnavailable("cannot retain restored census descriptor", err)
		}
		opened := os.NewFile(uintptr(fd), file.Path)
		head.metadata = append(head.metadata, bootstrapSuccessorRestoredMetadata{name: file.Path, file: opened, stat: observed})
	}
	if len(head.metadata) < 1 || len(head.metadata) > 2 {
		return nil, errors.New("restored preparation omitted its bounded metadata images")
	}
	censusRaw, err := json.Marshal(view.census)
	if err != nil {
		return nil, err
	}
	self := &bootstrapSuccessorMembers{storage: storage, file: root, restoredHead: head, spec: spec, readOnly: true, observedNameKVs: map[string]bootstrapSuccessorMemberObservation{}}
	if err := decodePlanJson(censusRaw, &self.census); err != nil {
		return nil, err
	}
	if err := errors.Join(self.validate(), self.check()); err != nil {
		return nil, err
	}
	return self, nil
}

// A lost/changed named image is permanent custody loss, whereas an unsuccessful
// syscall observation is unavailable. Neither grants a replacement enrollment.
func (self *bootstrapSuccessorRestoredMemberHead) check() error {
	if self == nil || self.closed {
		return errors.New("restored member view is closed")
	}
	if err := self.storage.check(self.root); err != nil {
		return err
	}
	for _, metadata := range self.metadata {
		var opened, named unix.Stat_t
		if err := unix.Fstat(int(metadata.file.Fd()), &opened); err != nil {
			return mainnetDurableUnavailable("cannot reobserve restored census descriptor", err)
		}
		if err := unix.Fstatat(int(self.root.Fd()), metadata.name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
				return self.storage.identity("restored census name was lost", err)
			}
			return mainnetDurableUnavailable("cannot reobserve restored census name", err)
		}
		if !bootstrapSuccessorMemberSameStat(metadata.stat, opened) || !bootstrapSuccessorMemberSameStat(opened, named) {
			return self.storage.identity("restored census metadata changed during passive inspection", nil)
		}
	}
	raw := make([]byte, 4097)
	n, err := unix.Fgetxattr(int(self.root.Fd()), self.attribute, raw)
	if err != nil {
		if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ERANGE) {
			return self.storage.identity("restored census checkpoint disappeared or exceeded its original bound", err)
		}
		return mainnetDurableUnavailable("cannot reobserve restored census checkpoint", err)
	}
	if n < 0 || n > 4096 || !bytes.Equal(raw[:n], self.raw) {
		return self.storage.identity("restored census checkpoint changed during passive inspection", nil)
	}
	return self.storage.ctx.Err()
}

// Only the metadata descriptors are owned here; the enclosing reader releases
// its root lock and guard after every member/head reader has joined.
func (self *bootstrapSuccessorRestoredMemberHead) close() error {
	if self == nil || self.closed {
		return nil
	}
	self.closed = true
	var err error
	for _, metadata := range self.metadata {
		err = errors.Join(err, metadata.file.Close())
	}
	self.metadata = nil
	return err
}
