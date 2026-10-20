//go:build linux || darwin

package main

// SDK outbox custody has its own complete owner profile. Its independent launch
// authority selects the original scope; physical restoration never signs a cut.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/urfoundation/sn/v2026/miner"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const storageSdkWorkKind = "sdk-original-work-outbox"
const storageSdkWorkPreparationSchema = "urnetwork-sdk-work-preparation-v1"

// The exact original public profile remains external to both old and copied
// outboxes. Selecting a slot cannot infer an approver from retained evidence.
type storageSdkWorkPreparationScope struct {
	Schema         string                  `json:"schema"`
	CaptureProfile durablevolume.Reference `json:"capture_profile"`
	Slot           string                  `json:"slot"`
}

type storageSdkWorkRestoreCensus struct {
	Schema             string                         `json:"schema"`
	Profile            storageSdkWorkPreparationScope `json:"profile"`
	OriginalCheckpoint []byte                         `json:"original_checkpoint"`
}

// Only the separately hashed launch profile selects the original request scope.
func storageSdkWorkProfile(ctx context.Context, owner durablevolume.PreparationOwner, ownerLocal bool) (storageSdkWorkPreparationScope, connect.OriginalWorkOutboxScope, string, error) {
	var inputs storageSdkWorkPreparationScope
	var scope connect.OriginalWorkOutboxScope
	if ctx == nil || ownerLocal || owner.Kind != storageSdkWorkKind || owner.Purpose != "fresh" && owner.Purpose != "restore" || owner.RelativePath != "." || owner.RestoreCoverage != "" {
		return inputs, scope, "", errors.New("SDK work custody requires its exclusive daemon outbox profile")
	}
	if err := decodePlanJson(owner.Inputs, &inputs); err != nil {
		return inputs, scope, "", err
	}
	if inputs.Schema != storageSdkWorkPreparationSchema || inputs.Slot == "" {
		return inputs, scope, "", errors.New("SDK work preparation profile is absent or unknown")
	}
	raw, _, err := readPlanFile(ctx, inputs.CaptureProfile.Path, 256*1024)
	if err != nil {
		return inputs, scope, "", err
	}
	profile, err := miner.DecodeProviderWorkCaptureProfile(ctx, raw, inputs.CaptureProfile.Sha256)
	if err != nil {
		return inputs, scope, "", err
	}
	for _, provider := range profile.Providers {
		if inputs.CaptureProfile.Path == provider.OutboxDirectory || strings.HasPrefix(inputs.CaptureProfile.Path, provider.OutboxDirectory+string(filepath.Separator)) {
			return inputs, scope, "", errors.New("SDK work capture authority overlaps original outbox custody")
		}
	}
	for _, provider := range profile.Providers {
		if provider.Slot != inputs.Slot {
			continue
		}
		domain, err := provider.Domain.Digest()
		if err != nil {
			return inputs, scope, "", err
		}
		return inputs, connect.OriginalWorkOutboxScope{DomainHash: domain, ClientId: provider.ClientId, PublicKey: provider.PublicKey, RequestPublicKey: profile.RequestPublicKey}, provider.OutboxDirectory, nil
	}
	return inputs, scope, "", errors.New("SDK work preparation omits its independently declared provider slot")
}

// The accepted fresh fence owns birth; staging contributes only the empty index.
func buildStorageSdkWork(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	inputs, _, _, err := storageSdkWorkProfile(ctx, owner, ownerLocal)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if owner.Purpose != "fresh" {
		return durablevolume.PreparationOwnerPlan{}, errors.New("SDK outbox birth requires explicit fresh preparation")
	}
	files := []durablevolume.PreparationFile{storagePreparationEmptyFile(connect.OriginalWorkOutboxIndexName)}
	if err := storagePreparationStageEmpty(ctx, staging, name, files); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, Files: files, Census: raw, Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: connect.OriginalWorkOutboxAttribute}}}, ctx.Err()
}

// Translate positive SDK loss without converting failed observations to loss.
func storageSdkWorkError(err error) error {
	if errors.Is(err, connect.ErrOriginalWorkOutboxIdentity) {
		return errors.Join(durablevolume.ErrIdentity, err)
	}
	return err
}

// Missing retained names and observed aliases prove loss; I/O failures do not.
func storageSdkWorkObservation(message string, err error) error {
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) {
		return errors.Join(durablevolume.ErrIdentity, errors.New(message), err)
	}
	return mainnetDurableUnavailable(message, err)
}

// Borrow the exact approved root without extending its namespace authority.
func storageSdkWorkRoot(ctx context.Context, root *os.File, expectedPath string) (unix.Stat_t, error) {
	return storageOriginalRoot(ctx, root, expectedPath)
}

// Whole-work records keep their existing fixed limits under shared I/O checks.
func readStorageSdkWorkFile(ctx context.Context, root *os.File, parent unix.Stat_t, member durablevolume.PreparationFile) ([]byte, unix.Stat_t, error) {
	maximum := uint64(12 * 1024 * 1024)
	if member.Path == connect.OriginalWorkOutboxIndexName {
		maximum = connect.MaximumOriginalWorkOutboxIndexBytes
	}
	return readStorageOriginalFile(ctx, root, parent, member, maximum)
}

// The pure SDK helper observes the actual published empty root before birth.
func inspectStorageSdkWork(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	inputs, _, path, err := storageSdkWorkProfile(ctx, owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	var retained storageSdkWorkPreparationScope
	if err := decodePlanJson(owner.Census, &retained); err != nil {
		return nil, err
	}
	files := []durablevolume.PreparationFile{storagePreparationEmptyFile(connect.OriginalWorkOutboxIndexName)}
	attribute := durablevolume.PreparationAttributeSpec{Path: ".", Name: connect.OriginalWorkOutboxAttribute}
	if owner.Owner.Purpose != "fresh" || retained != inputs || !reflect.DeepEqual(owner.Files, files) || len(owner.Attributes) != 1 || owner.Attributes[0] != attribute || owner.PhysicalMetadata != nil {
		return nil, errors.New("SDK work birth changed its exact empty profile")
	}
	if _, err := storageSdkWorkRoot(ctx, root, path); err != nil {
		return nil, err
	}
	raw, err := connect.BuildFreshOriginalWorkOutboxCheckpoint(ctx, root)
	if err != nil {
		return nil, storageSdkWorkError(err)
	}
	return []durablevolume.PreparedAttribute{{Spec: attribute, Raw: raw}}, ctx.Err()
}

// The original inventory owns the entire fixed outbox namespace. Unknown
// files, sibling heads and missing physical generations are never discarded.
func storageSdkWorkOriginals(report durablevolume.Inventory) ([]connect.OriginalWorkOutboxFile, []byte, error) {
	if report.Schema != durablevolume.PhysicalInventorySchema || report.RestartAuthorized || len(report.Entries) < 2 || len(report.Entries) > connect.MaximumOriginalWorkOutboxRecords+2 || report.Entries[0].Path != "" {
		return nil, nil, errors.New("SDK restore requires its complete original physical inventory")
	}
	var checkpoint []byte
	var files []connect.OriginalWorkOutboxFile
	seenKVs, inodeKVs := map[string]bool{}, map[uint64]bool{}
	var total uint64
	for _, entry := range report.Entries {
		if entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || seenKVs[entry.Path] || inodeKVs[entry.Physical.Inode] || entry.Uid != report.Entries[0].Uid {
			return nil, nil, errors.New("SDK restore original names or physical generations are missing or aliased")
		}
		seenKVs[entry.Path], inodeKVs[entry.Physical.Inode] = true, true
		file := connect.OriginalWorkOutboxFile{Name: entry.Path, Device: unix.Mkdev(entry.Physical.Device.Major, entry.Physical.Device.Minor), Inode: entry.Physical.Inode, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256}
		if entry.Path == "" {
			if entry.Kind != "directory" || entry.Mode != 0700 || *entry.Physical != report.PhysicalRoot {
				return nil, nil, errors.New("SDK restore original root differs")
			}
			file.Bytes, file.Sha256 = 0, ""
		} else {
			maximum, mode := uint64(12*1024*1024), uint32(0400)
			if entry.Path == connect.OriginalWorkOutboxIndexName {
				maximum, mode = connect.MaximumOriginalWorkOutboxIndexBytes, 0600
			}
			if entry.Kind != "file" || entry.Mode != mode || entry.Size > maximum || entry.Path != connect.OriginalWorkOutboxIndexName && (len(entry.Path) != 69 || filepath.Base(entry.Path) != entry.Path || !strings.HasSuffix(entry.Path, ".json") || !planSha256("sha256:"+strings.TrimSuffix(entry.Path, ".json"))) || !planSha256(entry.Sha256) {
				return nil, nil, errors.New("SDK restore has an unknown or unprotected original")
			}
			if entry.Size > connect.MaximumOriginalWorkOutboxBytes+connect.MaximumOriginalWorkOutboxIndexBytes-total {
				return nil, nil, errors.New("SDK restore exceeds original byte capacity")
			}
			total += entry.Size
		}
		files = append(files, file)
		for _, attribute := range entry.OwnerAttributes {
			if entry.Path == "" && attribute.Name == durablevolume.PreparationAttribute {
				continue
			}
			if entry.Path != "" || attribute.Name != connect.OriginalWorkOutboxAttribute || checkpoint != nil || len(attribute.Value) == 0 || len(attribute.Value) > 4096 || safeReleaseHash(attribute.Value) != attribute.Sha256 {
				return nil, nil, errors.New("SDK restore cannot discard another owner head or change original checkpoint")
			}
			checkpoint = bytes.Clone(attribute.Value)
		}
	}
	if checkpoint == nil || !seenKVs[connect.OriginalWorkOutboxIndexName] {
		return nil, nil, errors.Join(durablevolume.ErrIdentity, errors.New("SDK restore lost its original birth checkpoint or inventory"))
	}
	return files, checkpoint, nil
}

// Restore retains all original attributes and files before deriving an index.
func planStorageSdkWorkRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	inputs, _, path, err := storageSdkWorkProfile(ctx, owner, ownerLocal)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if owner.Purpose != "restore" || report.StateRoot.Path != path || name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return durablevolume.PreparationOwnerPlan{}, errors.New("SDK restore changed its independently selected original outbox")
	}
	originals, checkpoint, err := storageSdkWorkOriginals(report)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	var head connect.OriginalWorkOutboxCheckpoint
	if err := decodePlanJson(checkpoint, &head); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	canonical, err := json.Marshal(head)
	if err != nil || !bytes.Equal(canonical, checkpoint) || head.Schema != connect.OriginalWorkOutboxSchema || head.DirectoryDevice != unix.Mkdev(report.PhysicalRoot.Device.Major, report.PhysicalRoot.Device.Minor) || head.DirectoryInode != report.PhysicalRoot.Inode {
		return durablevolume.PreparationOwnerPlan{}, errors.Join(errors.New("SDK restore original checkpoint differs from runtime grammar"), err)
	}
	files := []durablevolume.PreparationFile{}
	var physical *durablevolume.PreparationPhysicalMetadata
	for _, file := range originals {
		if file.Name == "" {
			continue
		}
		if file.Name == connect.OriginalWorkOutboxIndexName {
			if file.Inode != head.IndexInode {
				return durablevolume.PreparationOwnerPlan{}, errors.New("SDK restore index inode differs from original checkpoint")
			}
			if file.Bytes != 0 {
				physical = &durablevolume.PreparationPhysicalMetadata{Path: file.Name, MaximumBytes: connect.MaximumOriginalWorkOutboxIndexBytes}
			}
		}
		files = append(files, durablevolume.PreparationFile{Path: file.Name, Kind: "file", Mode: file.Mode, Bytes: file.Bytes, Sha256: file.Sha256})
	}
	if physical == nil {
		if _, _, err := connect.DecodeOriginalWorkOutboxInventory(checkpoint, nil); err != nil {
			return durablevolume.PreparationOwnerPlan{}, storageSdkWorkError(err)
		}
	}
	raw, err := json.Marshal(storageSdkWorkRestoreCensus{Schema: "urnetwork-sdk-work-restore-v1", Profile: inputs, OriginalCheckpoint: checkpoint})
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: true, Files: files, Census: raw, Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: connect.OriginalWorkOutboxAttribute}}, PhysicalMetadata: physical}, ctx.Err()
}

// The core already pins each staged target. This callback borrows protected
// descriptors to verify signed originals before returning only new index bytes.
func rebindStorageSdkWorkRestore(ctx context.Context, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, raw []byte, targets []durablevolume.PreparationSource, ownerLocal bool) ([]byte, error) {
	expected, err := planStorageSdkWorkRestore(ctx, owner.StagingName, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, owner) || owner.PhysicalMetadata == nil {
		return nil, errors.New("SDK restore changed original physical derivation authority")
	}
	_, scope, _, err := storageSdkWorkProfile(ctx, owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	originals, checkpoint, err := storageSdkWorkOriginals(report)
	if err != nil {
		return nil, err
	}
	var translated []connect.OriginalWorkOutboxFile
	targetKVs := map[string]durablevolume.PreparationSource{}
	var device uint64
	for _, target := range targets {
		if _, exists := targetKVs[target.File.Path]; exists || target.File.Path == connect.OriginalWorkOutboxIndexName || target.File.Kind != "file" || target.File.Mode != 0400 || target.Identity.Mode&unix.S_IFMT != unix.S_IFREG || target.Identity.Mode&07777 != 0400 || target.Identity.Inode == 0 || len(targetKVs) != 0 && device != target.Identity.Device {
			return nil, errors.New("SDK restore target originals are changed or repeated")
		}
		targetKVs[target.File.Path], device = target, target.Identity.Device
	}
	if len(targets) != len(originals)-2 {
		return nil, errors.New("SDK restore target omits original signed submissions")
	}
	for _, file := range originals {
		if file.Name == "" || file.Name == connect.OriginalWorkOutboxIndexName {
			continue
		}
		target, ok := targetKVs[file.Name]
		if !ok || target.File.Bytes != file.Bytes || target.File.Sha256 != file.Sha256 {
			return nil, errors.New("SDK restore target bytes differ from originals")
		}
		file.Device, file.Inode = target.Identity.Device, target.Identity.Inode
		translated = append(translated, file)
	}
	read := func(ctx context.Context, name string) (_ []byte, resultErr error) {
		target := targetKVs[name]
		fd, err := unix.Open(filepath.Dir(target.Path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		root := os.NewFile(uintptr(fd), filepath.Dir(target.Path))
		defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
		var parent unix.Stat_t
		if err := unix.Fstat(fd, &parent); err != nil {
			return nil, err
		}
		raw, observed, err := readStorageSdkWorkFile(ctx, root, parent, target.File)
		if err != nil {
			return nil, err
		}
		if durablesys.StatDevice(&observed) != target.Identity.Device || observed.Ino != target.Identity.Inode || observed.Uid != target.Identity.Uid || observed.Gid != target.Identity.Gid {
			return nil, errors.Join(durablevolume.ErrIdentity, errors.New("SDK restore staged member lost its reviewed generation"))
		}
		return raw, nil
	}
	result, err := connect.RebindOriginalWorkOutboxIndex(ctx, scope, checkpoint, raw, originals, translated, read)
	if err != nil {
		return nil, storageSdkWorkError(err)
	}
	return result, nil
}

// Reversing the copied index proves its original logical ancestry. The exact
// new index and immutable leaves are then verified against borrowed root state.
func inspectStorageSdkWorkRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	expected, err := planStorageSdkWorkRestore(ctx, owner.StagingName, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	if len(expected.Files) != len(owner.Files) {
		return nil, errors.New("SDK restore changed its complete target census")
	}
	for index, file := range expected.Files {
		if file.Path == connect.OriginalWorkOutboxIndexName && expected.PhysicalMetadata != nil {
			if owner.Files[index].Path != file.Path || owner.Files[index].Kind != file.Kind || owner.Files[index].Mode != file.Mode || owner.Files[index].Bytes == 0 || owner.Files[index].Bytes > connect.MaximumOriginalWorkOutboxIndexBytes {
				return nil, errors.New("SDK restore changed index metadata bounds")
			}
			expected.Files[index] = owner.Files[index]
		}
	}
	if !reflect.DeepEqual(expected, owner) {
		return nil, errors.New("SDK restore changed original signed record authority")
	}
	_, scope, path, err := storageSdkWorkProfile(ctx, owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	parent, err := storageSdkWorkRoot(ctx, root, path)
	if err != nil {
		return nil, err
	}
	originals, checkpoint, err := storageSdkWorkOriginals(report)
	if err != nil {
		return nil, err
	}
	translated := []connect.OriginalWorkOutboxFile{{Name: "", Device: durablesys.StatDevice(&parent), Inode: parent.Ino, Mode: 0700}}
	fileKVs := map[string]durablevolume.PreparationFile{}
	var indexRaw []byte
	for _, file := range owner.Files {
		raw, observed, err := readStorageSdkWorkFile(ctx, root, parent, file)
		if err != nil {
			return nil, err
		}
		translated = append(translated, connect.OriginalWorkOutboxFile{Name: file.Path, Device: durablesys.StatDevice(&observed), Inode: observed.Ino, Mode: file.Mode, Bytes: file.Bytes, Sha256: file.Sha256})
		fileKVs[file.Path] = file
		if file.Path == connect.OriginalWorkOutboxIndexName {
			indexRaw = raw
		}
	}
	originalIndex, err := connect.OriginalWorkOutboxRestoreOriginalIndex(ctx, indexRaw, originals)
	if err != nil {
		return nil, storageSdkWorkError(err)
	}
	read := func(ctx context.Context, name string) ([]byte, error) {
		raw, _, err := readStorageSdkWorkFile(ctx, root, parent, fileKVs[name])
		return raw, err
	}
	result, err := connect.RebindOriginalWorkOutboxInventory(ctx, scope, checkpoint, originalIndex, originals, translated, read)
	if err != nil {
		return nil, storageSdkWorkError(err)
	}
	if !bytes.Equal(result.Index, indexRaw) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("restored SDK index changes more than physical member inodes"))
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &after); err != nil {
		return nil, mainnetDurableUnavailable("cannot reobserve restored SDK root", err)
	}
	if !bootstrapSuccessorMemberSameStat(parent, after) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("restored SDK root changed during inspection"))
	}
	return []durablevolume.PreparedAttribute{{Spec: owner.Attributes[0], Raw: result.Checkpoint}}, ctx.Err()
}
