//go:build linux

// Offline restoration keeps original snapshot and auxiliary bytes unchanged.
// Only a fixed application profile may rebind their physical checkpoint; the
// shared preparation stage publishes it and never grants restart authority.
package durablehead

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const snapshotRestoreSchema = "urnetwork-snapshot-restore-v1"

// The reviewed plan retains the old physical checkpoint and exact application
// capacities. Neither committed nor pending historical bytes are fabricated.
type snapshotRestoreCensus struct {
	Schema             string          `json:"schema"`
	Profile            json.RawMessage `json:"profile"`
	OriginalCheckpoint []byte          `json:"original_checkpoint"`
}

// Input must be one exact bounded format, never an unknown extension.
func decodeSnapshotRestore(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.Join(errors.New("snapshot restore metadata contains trailing data"), err)
	}
	return nil
}

// Only the known owner may consume original custody. Shared file-lock owners
// use an explicit view whose complete union is checked by the core publisher.
func PlanRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, spec Spec, profile json.RawMessage, report durablevolume.Inventory) (durablevolume.PreparationOwnerPlan, error) {
	if ctx == nil || !simpleName(name) || owner.Purpose != "restore" || owner.Kind != spec.Kind || owner.RelativePath != "." || len(profile) == 0 || len(profile) > 64*1024 || !json.Valid(profile) {
		return durablevolume.PreparationOwnerPlan{}, errors.New("snapshot restore requires a fixed bounded owner profile")
	}
	if err := errors.Join(ctx.Err(), validateSpec(spec)); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	var err error
	report, err = snapshotRestoreView(ctx, owner, spec, report)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if report.Schema != durablevolume.PhysicalInventorySchema || report.RestartAuthorized || len(report.Entries) == 0 || report.Entries[0].Path != "" {
		return durablevolume.PreparationOwnerPlan{}, errors.New("snapshot restore requires the complete original physical inventory")
	}
	spec.AuxiliaryNames = append([]string(nil), spec.AuxiliaryNames...)
	sort.Strings(spec.AuxiliaryNames)
	attribute := durablevolume.PreparationAttributeSpec{Path: ".", Name: Attribute(spec.Kind, spec.Name)}
	if spec.LockName != "" {
		attribute.Path = spec.LockName
	}
	entries := map[string]durablevolume.InventoryEntry{}
	inodes := map[uint64]bool{}
	files := make([]durablevolume.PreparationFile, 0, len(report.Entries)-1)
	var checkpointRaw []byte
	for _, entry := range report.Entries {
		if err := ctx.Err(); err != nil {
			return durablevolume.PreparationOwnerPlan{}, err
		}
		if entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || inodes[entry.Physical.Inode] {
			return durablevolume.PreparationOwnerPlan{}, errors.New("snapshot restore original physical generations are missing or aliased")
		}
		inodes[entry.Physical.Inode] = true
		if _, present := entries[entry.Path]; present {
			return durablevolume.PreparationOwnerPlan{}, errors.New("snapshot restore original names are duplicated")
		}
		entries[entry.Path] = entry
		if entry.Path == "" {
			if entry.Kind != "directory" || entry.Mode != 0700 || *entry.Physical != report.PhysicalRoot {
				return durablevolume.PreparationOwnerPlan{}, errors.New("snapshot restore original root differs")
			}
		} else {
			if entry.Kind != "file" || entry.Mode != 0600 || strings.Contains(entry.Path, "/") || entry.Size > uint64(spec.MaximumBytes) {
				return durablevolume.PreparationOwnerPlan{}, errors.New("snapshot restore has unknown member structure or capacity")
			}
			files = append(files, durablevolume.PreparationFile{Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256})
		}
		for _, old := range entry.OwnerAttributes {
			if entry.Path == "" && old.Name == durablevolume.PreparationAttribute {
				continue
			}
			path := entry.Path
			if path == "" {
				path = "."
			}
			if path != attribute.Path || old.Name != attribute.Name || checkpointRaw != nil || len(old.Value) == 0 || len(old.Value) > 4096 || old.Sha256 != "sha256:"+digest(old.Value) {
				return durablevolume.PreparationOwnerPlan{}, errors.New("snapshot restore cannot discard another owner or change original checkpoint bytes")
			}
			checkpointRaw = append([]byte(nil), old.Value...)
		}
	}
	if checkpointRaw == nil {
		return durablevolume.PreparationOwnerPlan{}, identity(errors.New("snapshot restore original checkpoint is missing"))
	}
	var checkpoint Checkpoint
	if err := decodeSnapshotRestore(checkpointRaw, &checkpoint); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	// Restoration changes physical coordinates only. It cannot normalize an
	// original checkpoint which the runtime refuses as ambiguous custody.
	canonical, err := json.Marshal(checkpoint)
	if err != nil || !bytes.Equal(checkpointRaw, canonical) {
		return durablevolume.PreparationOwnerPlan{}, identity(errors.New("snapshot restore original checkpoint is not canonical"), err)
	}
	if checkpoint.Schema != Schema || checkpoint.Kind != spec.Kind || checkpoint.Name != spec.Name || checkpoint.MaximumBytes != spec.MaximumBytes || checkpoint.DirectoryInode != report.PhysicalRoot.Inode || checkpoint.LockName != spec.LockName || len(checkpoint.Auxiliaries) != len(spec.AuxiliaryNames) || !validMember(checkpoint.Committed, spec.MaximumBytes) {
		return durablevolume.PreparationOwnerPlan{}, identity(errors.New("snapshot restore checkpoint differs from its exact original profile"))
	}
	allowed := map[string]bool{"": true, spec.Name: true}
	for index, auxiliary := range checkpoint.Auxiliaries {
		entry, present := entries[auxiliary.Name]
		if auxiliary.Name != spec.AuxiliaryNames[index] || !present || auxiliary.Inode != entry.Physical.Inode || entry.Size > 4096 {
			return durablevolume.PreparationOwnerPlan{}, identity(errors.New("snapshot restore original auxiliary generation or capacity differs"))
		}
		allowed[auxiliary.Name] = true
	}
	matches := func(name string, member Member) bool {
		entry, present := entries[name]
		if !member.Present {
			return !present
		}
		return present && entry.Physical.Inode == member.Inode && entry.Size == uint64(member.Size) && entry.Sha256 == "sha256:"+member.Sha256
	}
	if checkpoint.Pending == nil {
		if !matches(spec.Name, checkpoint.Committed) {
			return durablevolume.PreparationOwnerPlan{}, identity(errors.New("snapshot restore completed payload differs from its original head"))
		}
	} else {
		pending := checkpoint.Pending
		view := &Owner{spec: spec}
		if !view.temporaryName(pending.Temporary) || !pending.Next.Present || !validMember(pending.Next, spec.MaximumBytes) || pending.Next.Inode == checkpoint.Committed.Inode {
			return durablevolume.PreparationOwnerPlan{}, identity(errors.New("snapshot restore pending head is malformed"))
		}
		allowed[pending.Temporary] = true
		_, temporaryPresent := entries[pending.Temporary]
		beforeRename := matches(spec.Name, checkpoint.Committed) && matches(pending.Temporary, pending.Next)
		afterRename := matches(spec.Name, pending.Next) && (!temporaryPresent || checkpoint.Committed.Present && matches(pending.Temporary, checkpoint.Committed))
		if !beforeRename && !afterRename {
			return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrUncertain, errors.New("snapshot restore lacks complete original pending or exchanged bytes"))
		}
	}
	for path := range entries {
		if !allowed[path] {
			return durablevolume.PreparationOwnerPlan{}, identity(errors.New("snapshot restore contains an unreviewed owner member"))
		}
	}
	census, err := json.Marshal(snapshotRestoreCensus{Schema: snapshotRestoreSchema, Profile: append(json.RawMessage(nil), profile...), OriginalCheckpoint: checkpointRaw})
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: owner.RestoreCoverage == "", Files: files, Attributes: []durablevolume.PreparationAttributeSpec{attribute}, Census: census}, ctx.Err()
}

// Target bytes are observed only through retained no-follow descriptors. No
// constructor runs and no unknown file is converted into an approved snapshot.
func InspectRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, spec Spec, profile json.RawMessage, report durablevolume.Inventory) (result []durablevolume.PreparedAttribute, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = nil
		}
	}()
	expected, err := PlanRestore(ctx, owner.StagingName, owner.Owner, spec, profile, report)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, owner) || root == nil {
		return nil, errors.New("snapshot restore plan changed its original exact census")
	}
	var census snapshotRestoreCensus
	var checkpoint Checkpoint
	if err := decodeSnapshotRestore(owner.Census, &census); err != nil {
		return nil, err
	}
	if err := decodeSnapshotRestore(census.OriginalCheckpoint, &checkpoint); err != nil {
		return nil, err
	}
	var parent unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &parent); err != nil {
		return nil, observation(err, false)
	}
	if parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Mode&07777 != 0700 || parent.Uid != uint32(os.Geteuid()) {
		return nil, identity(errors.New("snapshot restore target root is not private"))
	}
	oldEntries := map[string]durablevolume.InventoryEntry{}
	for _, entry := range report.Entries {
		oldEntries[entry.Path] = entry
	}
	rebound := map[uint64]uint64{}
	newInodes := map[uint64]bool{parent.Ino: true}
	for _, member := range owner.Files {
		stat, err := inspectSnapshotRestoreMember(ctx, root, parent, member)
		if err != nil {
			return nil, err
		}
		if newInodes[stat.Ino] {
			return nil, identity(errors.New("snapshot restore target aliases another member"))
		}
		newInodes[stat.Ino] = true
		rebound[oldEntries[member.Path].Physical.Inode] = stat.Ino
	}
	checkpoint.DirectoryInode = parent.Ino
	for index := range checkpoint.Auxiliaries {
		checkpoint.Auxiliaries[index].Inode = rebound[checkpoint.Auxiliaries[index].Inode]
	}
	if checkpoint.Committed.Present {
		if next, present := rebound[checkpoint.Committed.Inode]; present {
			checkpoint.Committed.Inode = next
		} else if newInodes[checkpoint.Committed.Inode] {
			return nil, errors.Join(ErrUncertain, errors.New("retired predecessor coordinate collides with restored live custody"))
		}
	}
	if checkpoint.Pending != nil {
		checkpoint.Pending.Next.Inode = rebound[checkpoint.Pending.Next.Inode]
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &after); err != nil {
		return nil, observation(err, false)
	}
	if !sameStat(parent, after) {
		return nil, identity(errors.New("snapshot restore target changed during inspection"))
	}
	raw, err := json.Marshal(checkpoint)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return []durablevolume.PreparedAttribute{{Spec: owner.Attributes[0], Raw: raw}}, nil
}

// One bounded streaming pass verifies bytes; metadata is checked before and
// after the read. No payload prefix is exposed after a failed admission.
func inspectSnapshotRestoreMember(ctx context.Context, root *os.File, parent unix.Stat_t, member durablevolume.PreparationFile) (result unix.Stat_t, resultErr error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	fd, err := unix.Openat(int(root.Fd()), member.Path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return result, observation(err, true)
	}
	file := os.NewFile(uintptr(fd), member.Path)
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := unix.Fstat(fd, &result); err != nil {
		return result, observation(err, false)
	}
	if result.Dev != parent.Dev || result.Mode&unix.S_IFMT != unix.S_IFREG || result.Mode&07777 != 0600 || result.Uid != parent.Uid || result.Nlink != 1 || result.Size < 0 || uint64(result.Size) != member.Bytes {
		return result, identity(errors.New("snapshot restored member protection or size differs"))
	}
	hasher := sha256.New()
	buffer := make([]byte, 64*1024)
	for offset := uint64(0); offset < member.Bytes; {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		part := buffer[:min(uint64(len(buffer)), member.Bytes-offset)]
		n, err := file.ReadAt(part, int64(offset))
		if err != nil || n != len(part) {
			return result, observation(errors.Join(err, io.ErrUnexpectedEOF), false)
		}
		_, _ = hasher.Write(part)
		offset += uint64(n)
	}
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return result, observation(err, false)
	}
	if err := unix.Fstatat(int(root.Fd()), member.Path, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return result, observation(err, true)
	}
	if !sameStat(result, after) || !sameStat(after, named) || member.Sha256 != "sha256:"+hex.EncodeToString(hasher.Sum(nil)) {
		return result, identity(errors.New("snapshot restored member changed or differs from its original bytes"))
	}
	return result, ctx.Err()
}
