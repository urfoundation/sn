//go:build linux || darwin

// Member restoration derives only unsigned physical coordinates. The original
// census remains in the reviewed plan; signed member and pending payload bytes
// never change, and existing execution approvals remain separately required.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

type storagePreparationMemberRestoreCensus struct {
	Schema             string          `json:"schema"`
	Profile            json.RawMessage `json:"profile"`
	OriginalCheckpoint []byte          `json:"original_checkpoint"`
}

// Legacy restoration owns the complete root. Explicit shared local custody
// selects its fixed namespace, including its exact original head exchange.
func planStoragePreparationMembersRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	spec, profile, err := storagePreparationMembersProfile(owner, ownerLocal)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	report, err = storagePreparationMembersRestoreView(ctx, owner, spec, report)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if ctx == nil || owner.Purpose != "restore" || name == "" || filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsRune(name, 0) || report.Schema != durablevolume.PhysicalInventorySchema || report.RestartAuthorized || len(report.Entries) == 0 || len(report.Entries) > maximumBootstrapSuccessorMemberCount+8 || report.Entries[0].Path != "" {
		return durablevolume.PreparationOwnerPlan{}, errors.New("member restore requires its bounded complete original physical inventory")
	}
	head, _, err := storagePreparationMemberRestoreHead(spec, report)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	isMetadata := func(path string) bool {
		return path == spec.Name || head.Pending != nil && path == head.Pending.Temporary
	}
	attribute := durablevolume.PreparationAttributeSpec{Path: ".", Name: durablehead.Attribute(spec.Kind, spec.Name)}
	files := []durablevolume.PreparationFile{}
	entries := map[string]durablevolume.InventoryEntry{}
	inodes := map[uint64]bool{}
	var original []byte
	var total uint64
	for _, entry := range report.Entries {
		if err := ctx.Err(); err != nil {
			return durablevolume.PreparationOwnerPlan{}, err
		}
		if entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || inodes[entry.Physical.Inode] {
			return durablevolume.PreparationOwnerPlan{}, errors.New("member restore original generations are missing or aliased")
		}
		inodes[entry.Physical.Inode] = true
		if _, found := entries[entry.Path]; found {
			return durablevolume.PreparationOwnerPlan{}, errors.New("member restore original names are duplicated")
		}
		entries[entry.Path] = entry
		if entry.Path == "" {
			if entry.Kind != "directory" || entry.Mode != 0700 || *entry.Physical != report.PhysicalRoot {
				return durablevolume.PreparationOwnerPlan{}, errors.New("member restore original root differs")
			}
		} else {
			maximum := uint64(maximumBootstrapSuccessorExecutionBytes)
			if isMetadata(entry.Path) {
				maximum = uint64(spec.MaximumBytes)
			}
			if entry.Kind != "file" || entry.Mode != 0600 || filepath.Base(entry.Path) != entry.Path || entry.Path == "." || entry.Path == ".." || len(entry.Path) > 255 || strings.ContainsRune(entry.Path, 0) || entry.Size > maximum || !planSha256(entry.Sha256) || !isMetadata(entry.Path) && !bootstrapSuccessorMemberOwns(spec, owner.Kind == "mainnet-successor-nonce-members", entry.Path) {
				return durablevolume.PreparationOwnerPlan{}, errors.New("member restore has an unknown name, structure or capacity")
			}
			if entry.Size > uint64(maximumBootstrapSuccessorMemberTotalBytes+2*maximumBootstrapSuccessorMemberCensusBytes)-total {
				return durablevolume.PreparationOwnerPlan{}, errors.New("member restore exceeds its finite retained byte capacity")
			}
			total += entry.Size
			files = append(files, durablevolume.PreparationFile{Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256})
		}
		for _, item := range entry.OwnerAttributes {
			if entry.Path == "" && item.Name == durablevolume.PreparationAttribute {
				continue
			}
			if entry.Path != "" || item.Name != attribute.Name || original != nil || len(item.Value) == 0 || len(item.Value) > 4096 || safeReleaseHash(item.Value) != item.Sha256 {
				return durablevolume.PreparationOwnerPlan{}, errors.New("member restore cannot discard another owner head or change checkpoint bytes")
			}
			original = append([]byte(nil), item.Value...)
		}
	}
	if original == nil {
		return durablevolume.PreparationOwnerPlan{}, errors.Join(durablevolume.ErrIdentity, errors.New("member restore original checkpoint is missing"))
	}
	if err := decodePlanJson(original, &head); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if head.Schema != durablehead.Schema || head.Kind != spec.Kind || head.Name != spec.Name || head.MaximumBytes != spec.MaximumBytes || head.DirectoryInode != report.PhysicalRoot.Inode || head.LockName != "" || len(head.Auxiliaries) != 0 {
		return durablevolume.PreparationOwnerPlan{}, errors.New("member restore original checkpoint differs from its fixed profile")
	}
	// The shared snapshot grammar authenticates both sides of a completed
	// write-ahead exchange. It never treats a partial temporary as complete.
	headView := report
	headView.Entries = nil
	for _, entry := range report.Entries {
		if entry.Path == "" || isMetadata(entry.Path) {
			headView.Entries = append(headView.Entries, entry)
		}
	}
	headOwner := owner
	headOwner.RestoreCoverage = ""
	if _, err := durablehead.PlanRestore(ctx, name, headOwner, spec, profile, headView); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	var physical *durablevolume.PreparationPhysicalMetadata
	for _, path := range []string{spec.Name, func() string {
		if head.Pending != nil {
			return head.Pending.Temporary
		}
		return ""
	}()} {
		if path == "" {
			continue
		}
		if metadata, present := entries[path]; present {
			if metadata.Size == 0 {
				return durablevolume.PreparationOwnerPlan{}, errors.New("member restore census is empty")
			}
			if physical == nil {
				physical = &durablevolume.PreparationPhysicalMetadata{Path: path, MaximumBytes: uint64(spec.MaximumBytes)}
			} else {
				physical.CompanionPath = path
			}
		}
	}
	if physical == nil && len(files) != 0 {
		return durablevolume.PreparationOwnerPlan{}, errors.Join(durablevolume.ErrIdentity, errors.New("member restore absent head cannot authorize historical members"))
	}
	census, err := json.Marshal(storagePreparationMemberRestoreCensus{Schema: "urnetwork-successor-members-restore-v1", Profile: profile, OriginalCheckpoint: original})
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: owner.RestoreCoverage == "", Files: files, Attributes: []durablevolume.PreparationAttributeSpec{attribute}, Census: census, PhysicalMetadata: physical}, ctx.Err()
}

// Selecting a temporary requires this exact retained bounded checkpoint, never
// a prefix match or a caller-nominated basename. Full grammar is checked later.
func storagePreparationMemberRestoreHead(spec durablehead.Spec, report durablevolume.Inventory) (durablehead.Checkpoint, []byte, error) {
	var head durablehead.Checkpoint
	var raw []byte
	for _, entry := range report.Entries {
		if entry.Path != "" {
			continue
		}
		for _, attribute := range entry.OwnerAttributes {
			if attribute.Name != durablehead.Attribute(spec.Kind, spec.Name) {
				continue
			}
			if raw != nil || len(attribute.Value) == 0 || len(attribute.Value) > 4096 || safeReleaseHash(attribute.Value) != attribute.Sha256 {
				return head, nil, errors.New("member restore original head is repeated or changed")
			}
			raw = attribute.Value
		}
	}
	if raw == nil {
		return head, nil, errors.Join(durablevolume.ErrIdentity, errors.New("member restore original checkpoint is missing"))
	}
	if err := decodePlanJson(raw, &head); err != nil {
		return head, nil, err
	}
	return head, raw, nil
}

// Both metadata images describe the same complete application namespace. They
// are excluded only by the fixed adapter's already authenticated pair.
func storagePreparationMemberMetadataOwns(metadata *durablevolume.PreparationPhysicalMetadata, path string) bool {
	return metadata != nil && (path == metadata.Path || metadata.CompanionPath != "" && path == metadata.CompanionPath)
}

// Every original member and pending stage must be accounted for. An unfinished
// payload is admissible only as the exact prefix of its retained approved
// payload, and its original pending phase is preserved.
func validateStoragePreparationMemberImage(census bootstrapSuccessorMemberCensus, spec durablehead.Spec, entries map[string]durablevolume.InventoryEntry) error {
	if err := validateBootstrapSuccessorMemberCensus(census, spec, spec.Kind == "mainnet-successor-nonce-members"); err != nil {
		return err
	}
	allowed := map[string]bool{"": true, spec.Name: true}
	pending := census.Pending
	for _, member := range census.Members {
		actual, found := entries[member.Name]
		if !found || actual.Physical == nil || actual.Physical.Inode != member.Inode {
			return errors.New("member restore lost an acknowledged member generation")
		}
		allowed[member.Name] = true
		if pending != nil && pending.Append && pending.Name == member.Name {
			payload, _ := base64.StdEncoding.Strict().DecodeString(pending.Payload)
			if member.Size >= pending.Size || actual.Size < uint64(member.Size) || actual.Size > uint64(pending.Size) || safeReleaseHash(payload[:member.Size]) != member.Sha256 || safeReleaseHash(payload[:actual.Size]) != actual.Sha256 {
				return errors.New("member restore append differs from its original exact prefix")
			}
		} else if actual.Size != uint64(member.Size) || actual.Sha256 != member.Sha256 {
			return errors.New("member restore acknowledged payload bytes differ")
		}
	}
	if pending != nil && !pending.Append {
		stage, hasStage := entries[pending.Stage]
		final, hasFinal := entries[pending.Name]
		if hasStage && hasFinal || pending.StageInode != 0 && !hasStage && !hasFinal || hasFinal && pending.StageInode == 0 {
			return errors.New("member restore pending stage is missing or ambiguous")
		}
		allowed[pending.Stage], allowed[pending.Name] = true, true
		actual := stage
		if hasFinal {
			actual = final
		}
		if hasStage || hasFinal {
			payload, _ := base64.StdEncoding.Strict().DecodeString(pending.Payload)
			if actual.Physical == nil || pending.StageInode != 0 && actual.Physical.Inode != pending.StageInode || actual.Size > uint64(pending.Size) || hasFinal && actual.Size != uint64(pending.Size) || safeReleaseHash(payload[:actual.Size]) != actual.Sha256 {
				return errors.New("member restore pending payload differs from its original exact prefix")
			}
		}
	}
	for name := range entries {
		if !allowed[name] {
			return errors.New("member restore contains an unacknowledged namespace member")
		}
	}
	return nil
}

// This pure transformation has no file or key access. It changes only inode
// fields after proving every original byte and target identity in the plan.
func rebindStoragePreparationMembersRestore(ctx context.Context, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, raw []byte, targets []durablevolume.PreparationSource, ownerLocal bool) ([]byte, error) {
	expected, err := planStoragePreparationMembersRestore(ctx, owner.StagingName, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(owner, expected) || expected.PhysicalMetadata == nil {
		return nil, errors.New("member restore derivation changed its exact original owner")
	}
	spec := bootstrapSuccessorMemberSpec(owner.Owner.Kind == "mainnet-successor-nonce-members")
	report, err = storagePreparationMembersRestoreView(ctx, owner.Owner, spec, report)
	if err != nil {
		return nil, err
	}
	entries := map[string]durablevolume.InventoryEntry{}
	for _, entry := range report.Entries {
		entries[entry.Path] = entry
	}
	metadataCount := 0
	matched := false
	for _, metadata := range expected.Files {
		if storagePreparationMemberMetadataOwns(expected.PhysicalMetadata, metadata.Path) {
			metadataCount++
			matched = matched || uint64(len(raw)) == metadata.Bytes && safeReleaseHash(raw) == metadata.Sha256
		}
	}
	if !matched {
		return nil, errors.New("member restore original census bytes differ")
	}
	var census bootstrapSuccessorMemberCensus
	if err := decodePlanJson(raw, &census); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(census)
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, errors.Join(errors.New("member restore original census is not canonical"), err)
	}
	memberEntries := make(map[string]durablevolume.InventoryEntry, len(entries))
	for path, entry := range entries {
		if !storagePreparationMemberMetadataOwns(expected.PhysicalMetadata, path) || path == spec.Name {
			memberEntries[path] = entry
		}
	}
	if err := validateStoragePreparationMemberImage(census, spec, memberEntries); err != nil {
		return nil, err
	}
	rebound := map[uint64]uint64{}
	seen := map[string]bool{}
	inodes := map[uint64]bool{}
	var device uint64
	for _, target := range targets {
		original, found := entries[target.File.Path]
		if !found || target.File.Path == "" || storagePreparationMemberMetadataOwns(expected.PhysicalMetadata, target.File.Path) || seen[target.File.Path] || target.File != (durablevolume.PreparationFile{Path: original.Path, Kind: original.Kind, Mode: original.Mode, Bytes: original.Size, Sha256: original.Sha256}) || target.Identity.Inode == 0 || inodes[target.Identity.Inode] || target.Identity.Mode&unix.S_IFMT != unix.S_IFREG || target.Identity.Mode&07777 != 0600 {
			return nil, errors.New("member restore target member is missing, aliased or changed")
		}
		if len(seen) != 0 && target.Identity.Device != device {
			return nil, errors.New("member restore target spans physical devices")
		}
		device = target.Identity.Device
		seen[target.File.Path], inodes[target.Identity.Inode] = true, true
		rebound[original.Physical.Inode] = target.Identity.Inode
	}
	if len(targets) != len(expected.Files)-metadataCount {
		return nil, errors.New("member restore target omits original members")
	}
	for index := range census.Members {
		census.Members[index].Inode = rebound[census.Members[index].Inode]
	}
	if census.Pending != nil && census.Pending.StageInode != 0 {
		census.Pending.StageInode = rebound[census.Pending.StageInode]
	}
	result, err := json.Marshal(census)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return result, nil
}

// All inspection is descriptor-relative, bounded and identity checked before
// admitting bytes. No partial result escapes a failed post-read check.
func readStoragePreparationMember(ctx context.Context, root *os.File, parent unix.Stat_t, member durablevolume.PreparationFile) (raw []byte, observed unix.Stat_t, resultErr error) {
	defer func() {
		if resultErr != nil {
			raw = nil
		}
	}()
	if member.Kind != "file" || member.Mode != 0600 || member.Bytes > maximumBootstrapSuccessorMemberCensusBytes || member.Path == "" || member.Path == "." || member.Path == ".." || filepath.Base(member.Path) != member.Path || strings.ContainsRune(member.Path, 0) {
		return nil, observed, errors.New("member restore read exceeds its fixed bounds")
	}
	if err := ctx.Err(); err != nil {
		return nil, observed, err
	}
	fd, err := unix.Openat(int(root.Fd()), member.Path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, observed, mainnetDurableUnavailable("cannot open restored member", err)
	}
	file := os.NewFile(uintptr(fd), member.Path)
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := unix.Fstat(fd, &observed); err != nil {
		return nil, observed, mainnetDurableUnavailable("cannot inspect restored member", err)
	}
	if observed.Dev != parent.Dev || observed.Mode&unix.S_IFMT != unix.S_IFREG || observed.Mode&07777 != 0600 || observed.Uid != parent.Uid || observed.Nlink != 1 || observed.Size < 0 || uint64(observed.Size) != member.Bytes {
		return nil, observed, errors.Join(durablevolume.ErrIdentity, errors.New("restored member generation or bounds differ"))
	}
	raw = make([]byte, int(member.Bytes))
	for offset := 0; offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return nil, observed, err
		}
		part := raw[offset:min(offset+64*1024, len(raw))]
		n, err := file.ReadAt(part, int64(offset))
		if err != nil || n != len(part) {
			return nil, observed, mainnetDurableUnavailable("cannot read restored member", errors.Join(err, io.ErrUnexpectedEOF))
		}
		offset += n
	}
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, observed, mainnetDurableUnavailable("cannot recheck restored member", err)
	}
	if err := unix.Fstatat(int(root.Fd()), member.Path, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, observed, mainnetDurableUnavailable("cannot recheck restored member name", err)
	}
	if !bootstrapSuccessorMemberSameStat(observed, after) || !bootstrapSuccessorMemberSameStat(after, named) || safeReleaseHash(raw) != member.Sha256 {
		return nil, observed, errors.Join(durablevolume.ErrIdentity, errors.New("restored member changed during exact inspection"))
	}
	return raw, observed, ctx.Err()
}

// Reverse only the permitted physical fields and require the original census
// digest. This independently prevents an accepted plan from changing a pending
// payload, signed member hash, phase, name, count or application capacity.
func inspectStoragePreparationMembersRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	expected, err := planStoragePreparationMembersRestore(ctx, owner.StagingName, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	spec := bootstrapSuccessorMemberSpec(owner.Owner.Kind == "mainnet-successor-nonce-members")
	report, err = storagePreparationMembersRestoreView(ctx, owner.Owner, spec, report)
	if err != nil {
		return nil, err
	}
	if root == nil || len(expected.Files) != len(owner.Files) {
		return nil, errors.New("member restore inspection lost its complete target")
	}
	for index, member := range expected.Files {
		if storagePreparationMemberMetadataOwns(expected.PhysicalMetadata, member.Path) {
			if owner.Files[index].Path != member.Path || owner.Files[index].Kind != member.Kind || owner.Files[index].Mode != member.Mode || owner.Files[index].Bytes == 0 || owner.Files[index].Bytes > uint64(spec.MaximumBytes) {
				return nil, errors.New("member restore changed the metadata profile")
			}
			expected.Files[index] = owner.Files[index]
		}
	}
	if !reflect.DeepEqual(expected, owner) {
		return nil, errors.New("member restore changed original signed member authority")
	}
	var parent unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &parent); err != nil {
		return nil, mainnetDurableUnavailable("cannot inspect restored member root", err)
	}
	if parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Mode&07777 != 0700 || parent.Uid != uint32(os.Geteuid()) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("restored member root is not private"))
	}
	originalEntries := map[string]durablevolume.InventoryEntry{}
	for _, entry := range report.Entries {
		originalEntries[entry.Path] = entry
	}
	entries := map[string]durablevolume.InventoryEntry{}
	inodes := map[uint64]bool{parent.Ino: true}
	metadataBytes := map[string][]byte{}
	metadataStats := map[string]unix.Stat_t{}
	for _, member := range owner.Files {
		raw, stat, err := readStoragePreparationMember(ctx, root, parent, member)
		if err != nil {
			return nil, err
		}
		if inodes[stat.Ino] {
			return nil, errors.Join(durablevolume.ErrIdentity, errors.New("restored member aliases another owner inode"))
		}
		inodes[stat.Ino] = true
		entry := originalEntries[member.Path]
		entry.Physical = &durablevolume.PhysicalRoot{Device: durablevolume.Device{Major: unix.Major(durablesys.StatDevice(&stat)), Minor: unix.Minor(durablesys.StatDevice(&stat))}, Inode: stat.Ino}
		entry.Size, entry.Sha256 = member.Bytes, member.Sha256
		entries[member.Path] = entry
		if storagePreparationMemberMetadataOwns(expected.PhysicalMetadata, member.Path) {
			metadataBytes[member.Path], metadataStats[member.Path] = raw, stat
		}
	}
	var retained storagePreparationMemberRestoreCensus
	var head durablehead.Checkpoint
	if err := decodePlanJson(owner.Census, &retained); err != nil {
		return nil, err
	}
	if err := decodePlanJson(retained.OriginalCheckpoint, &head); err != nil {
		return nil, err
	}
	committedInode, nextInode := head.Committed.Inode, uint64(0)
	if head.Pending != nil {
		nextInode = head.Pending.Next.Inode
	}
	memberEntries := make(map[string]durablevolume.InventoryEntry, len(entries))
	for path, entry := range entries {
		if !storagePreparationMemberMetadataOwns(expected.PhysicalMetadata, path) || path == spec.Name {
			memberEntries[path] = entry
		}
	}
	for path, metadataRaw := range metadataBytes {
		var census bootstrapSuccessorMemberCensus
		if err := decodePlanJson(metadataRaw, &census); err != nil {
			return nil, err
		}
		canonical, err := json.Marshal(census)
		if err != nil || !bytes.Equal(canonical, metadataRaw) {
			return nil, errors.Join(errors.New("restored member census is not canonical"), err)
		}
		if err := validateStoragePreparationMemberImage(census, spec, memberEntries); err != nil {
			return nil, err
		}
		for index := range census.Members {
			census.Members[index].Inode = originalEntries[census.Members[index].Name].Physical.Inode
		}
		if pending := census.Pending; pending != nil && pending.StageInode != 0 {
			original, present := originalEntries[pending.Stage]
			if !present {
				original = originalEntries[pending.Name]
			}
			pending.StageInode = original.Physical.Inode
		}
		originalRaw, err := json.Marshal(census)
		original := originalEntries[path]
		if err != nil || uint64(len(originalRaw)) != original.Size || safeReleaseHash(originalRaw) != original.Sha256 {
			return nil, errors.Join(errors.New("restored census changes authority beyond physical inode fields"), err)
		}
		metadataStat := metadataStats[path]
		rebound := durablehead.Member{Present: true, Inode: metadataStat.Ino, Size: int64(len(metadataRaw)), Sha256: strings.TrimPrefix(safeReleaseHash(metadataRaw), "sha256:")}
		if head.Committed.Present && committedInode == original.Physical.Inode {
			head.Committed = rebound
		} else if head.Pending != nil && nextInode == original.Physical.Inode {
			head.Pending.Next = rebound
		} else {
			return nil, errors.New("restored census is not bound by either original checkpoint member")
		}
	}
	if head.Committed.Present && head.Pending != nil {
		// The old exchanged temporary may already have been removed. Its
		// retired coordinate cannot accidentally name any new target inode.
		originalHead, _, err := storagePreparationMemberRestoreHead(spec, report)
		if err != nil {
			return nil, err
		}
		oldPresent := false
		for path := range metadataBytes {
			oldPresent = oldPresent || originalEntries[path].Physical.Inode == originalHead.Committed.Inode
		}
		if !oldPresent && inodes[originalHead.Committed.Inode] {
			return nil, errors.Join(durablevolume.ErrIdentity, errors.New("retired outer-census coordinate collides with restored live custody"))
		}
	}
	head.DirectoryInode = parent.Ino
	var after unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &after); err != nil {
		return nil, mainnetDurableUnavailable("cannot recheck restored member root", err)
	}
	if !bootstrapSuccessorMemberSameStat(parent, after) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("restored member root changed during inspection"))
	}
	raw, err := json.Marshal(head)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return []durablevolume.PreparedAttribute{{Spec: owner.Attributes[0], Raw: raw}}, nil
}
