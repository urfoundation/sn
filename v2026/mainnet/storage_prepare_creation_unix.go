//go:build linux || darwin

package main

// Original creation custody is an explicit complete owner. An independently
// hashed provider profile supplies birth and key authority before any leaf read.
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
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"golang.org/x/sys/unix"
)

const storageOriginalContractKind = "sdk-original-contract-store"
const storageOriginalContractPreparationSchema = "urnetwork-original-contract-preparation-v1"

type storageOriginalContractPreparationScope struct {
	Schema         string                  `json:"schema"`
	CaptureProfile durablevolume.Reference `json:"capture_profile"`
	Slot           string                  `json:"slot"`
}

type storageOriginalContractRestoreCensus struct {
	Schema             string                                  `json:"schema"`
	Profile            storageOriginalContractPreparationScope `json:"profile"`
	OriginalCheckpoint []byte                                  `json:"original_checkpoint"`
}

// This is a distinct namespace birth, never a replacement SDK generation.
func storageOriginalContractProfile(ctx context.Context, owner durablevolume.PreparationOwner, ownerLocal bool) (storageOriginalContractPreparationScope, connect.OriginalContractStoreScope, string, error) {
	var inputs storageOriginalContractPreparationScope
	var scope connect.OriginalContractStoreScope
	if ctx == nil || ownerLocal || owner.Kind != storageOriginalContractKind || owner.Purpose != "fresh" && owner.Purpose != "restore" || owner.RelativePath != "." || owner.RestoreCoverage != "" {
		return inputs, scope, "", errors.New("original creation custody requires its exclusive daemon profile")
	}
	if err := decodePlanJson(owner.Inputs, &inputs); err != nil {
		return inputs, scope, "", err
	}
	if inputs.Schema != storageOriginalContractPreparationSchema || inputs.Slot == "" {
		return inputs, scope, "", errors.New("original creation preparation profile is absent or unknown")
	}
	raw, _, err := readPlanFile(ctx, inputs.CaptureProfile.Path, 256*1024)
	if err != nil {
		return inputs, scope, "", err
	}
	profile, err := miner.DecodeProviderContractCaptureProfile(ctx, raw, inputs.CaptureProfile.Sha256)
	if err != nil {
		return inputs, scope, "", err
	}
	for _, provider := range profile.Providers {
		if inputs.CaptureProfile.Path == provider.Directory || strings.HasPrefix(inputs.CaptureProfile.Path, provider.Directory+string(filepath.Separator)) {
			return inputs, scope, "", errors.New("original creation authority overlaps original custody")
		}
	}
	for _, provider := range profile.Providers {
		if provider.Slot == inputs.Slot {
			domain, err := provider.Domain.Digest()
			if err != nil {
				return inputs, scope, "", err
			}
			scope = connect.OriginalContractStoreScope{DomainHash: domain, ClientId: provider.ClientId, PublicKey: provider.PublicKey, SourceGeneration: provider.SourceGeneration}
			return inputs, scope, provider.Directory, scope.Validate()
		}
	}
	return inputs, scope, "", errors.New("original creation preparation omits its independently declared provider slot")
}

func storageOriginalContractError(err error) error {
	if errors.Is(err, connect.ErrOriginalContractStoreIdentity) {
		return errors.Join(durablevolume.ErrIdentity, err)
	}
	return err
}

// Only the outer accepted empty fence creates a fresh lease and root birth.
func buildStorageOriginalContract(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	inputs, _, _, err := storageOriginalContractProfile(ctx, owner, ownerLocal)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if owner.Purpose != "fresh" {
		return durablevolume.PreparationOwnerPlan{}, errors.New("original creation birth requires explicit fresh preparation")
	}
	files := []durablevolume.PreparationFile{storagePreparationEmptyFile(connect.OriginalContractStoreLeaseName)}
	if err := storagePreparationStageEmpty(ctx, staging, name, files); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: true, Files: files, Census: raw, Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: connect.OriginalContractStoreAttribute}}}, ctx.Err()
}

func inspectStorageOriginalContract(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	inputs, scope, path, err := storageOriginalContractProfile(ctx, owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	var retained storageOriginalContractPreparationScope
	if err := decodePlanJson(owner.Census, &retained); err != nil {
		return nil, err
	}
	files := []durablevolume.PreparationFile{storagePreparationEmptyFile(connect.OriginalContractStoreLeaseName)}
	attribute := durablevolume.PreparationAttributeSpec{Path: ".", Name: connect.OriginalContractStoreAttribute}
	if owner.Owner.Purpose != "fresh" || !owner.ExclusiveRoot || retained != inputs || !reflect.DeepEqual(owner.Files, files) || len(owner.Attributes) != 1 || owner.Attributes[0] != attribute || owner.PhysicalMetadata != nil {
		return nil, errors.New("original creation birth changed its reviewed empty profile")
	}
	if _, err := storageOriginalRoot(ctx, root, path); err != nil {
		return nil, err
	}
	raw, err := connect.BuildFreshOriginalContractStoreCheckpoint(ctx, root, scope)
	if err != nil {
		return nil, storageOriginalContractError(err)
	}
	return []durablevolume.PreparedAttribute{{Spec: attribute, Raw: raw}}, ctx.Err()
}

// Every original name and attribute belongs to this literal fixed namespace.
func storageOriginalContractOriginals(report durablevolume.Inventory, scope connect.OriginalContractStoreScope) ([]connect.OriginalContractStoreFile, []byte, error) {
	if report.Schema != durablevolume.PhysicalInventorySchema || report.RestartAuthorized || len(report.Entries) < 2 || len(report.Entries) > connect.MaximumOriginalContractLeaves+2 || report.Entries[0].Path != "" {
		return nil, nil, errors.New("original creation restore requires its complete physical inventory")
	}
	var checkpoint []byte
	files := []connect.OriginalContractStoreFile{}
	seenKVs, inodeKVs := map[string]bool{}, map[uint64]bool{}
	var total uint64
	for _, entry := range report.Entries {
		if entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || seenKVs[entry.Path] || inodeKVs[entry.Physical.Inode] || entry.Uid != report.Entries[0].Uid {
			return nil, nil, errors.New("original creation physical generations repeat or differ")
		}
		seenKVs[entry.Path], inodeKVs[entry.Physical.Inode] = true, true
		file := connect.OriginalContractStoreFile{Name: entry.Path, Device: unix.Mkdev(entry.Physical.Device.Major, entry.Physical.Device.Minor), Inode: entry.Physical.Inode, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256}
		switch entry.Path {
		case "":
			if entry.Kind != "directory" || entry.Mode != 0700 || *entry.Physical != report.PhysicalRoot {
				return nil, nil, errors.New("original creation root differs")
			}
			file.Bytes, file.Sha256 = 0, ""
		case connect.OriginalContractStoreLeaseName:
			if entry.Kind != "file" || entry.Mode != 0600 || entry.Size != 0 || entry.Sha256 != safeReleaseHash(nil) {
				return nil, nil, errors.New("original creation empty lease differs")
			}
		default:
			name := strings.TrimPrefix(entry.Path, "request-")
			if name == entry.Path {
				name = strings.TrimPrefix(entry.Path, "admission-")
			}
			if entry.Kind != "file" || entry.Mode != 0400 || entry.Size == 0 || entry.Size > coreprotocol.MaximumOriginalContractAdmissionBytes || name == entry.Path || len(name) != 69 || !strings.HasSuffix(name, ".json") || !planSha256("sha256:"+strings.TrimSuffix(name, ".json")) || !planSha256(entry.Sha256) || entry.Size > connect.MaximumOriginalContractStoreBytes-total {
				return nil, nil, errors.New("original creation contains an unknown, partial or oversized member")
			}
			total += entry.Size
		}
		files = append(files, file)
		for _, attribute := range entry.OwnerAttributes {
			if entry.Path == "" && attribute.Name == durablevolume.PreparationAttribute {
				continue
			}
			if entry.Path != "" || attribute.Name != connect.OriginalContractStoreAttribute || checkpoint != nil || len(attribute.Value) == 0 || len(attribute.Value) > 4096 || safeReleaseHash(attribute.Value) != attribute.Sha256 {
				return nil, nil, errors.New("original creation cannot discard another original owner attribute")
			}
			checkpoint = bytes.Clone(attribute.Value)
		}
	}
	head, err := connect.DecodeOriginalContractStoreCheckpoint(checkpoint, scope)
	if err != nil {
		return nil, nil, storageOriginalContractError(err)
	}
	if !seenKVs[connect.OriginalContractStoreLeaseName] || head.DirectoryDevice != files[0].Device || head.DirectoryInode != report.PhysicalRoot.Inode {
		return nil, nil, errors.Join(durablevolume.ErrIdentity, errors.New("original creation lost its prepared physical birth"))
	}
	for _, file := range files {
		if file.Name == connect.OriginalContractStoreLeaseName && file.Inode != head.LeaseInode {
			return nil, nil, errors.Join(durablevolume.ErrIdentity, errors.New("original creation lease differs from original birth"))
		}
	}
	return files, checkpoint, nil
}

func planStorageOriginalContractRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	inputs, scope, path, err := storageOriginalContractProfile(ctx, owner, ownerLocal)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if owner.Purpose != "restore" || report.StateRoot.Path != path || name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return durablevolume.PreparationOwnerPlan{}, errors.New("original creation restore changed its approved logical path")
	}
	originals, checkpoint, err := storageOriginalContractOriginals(report, scope)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	files := []durablevolume.PreparationFile{}
	for _, file := range originals {
		if file.Name != "" {
			files = append(files, durablevolume.PreparationFile{Path: file.Name, Kind: "file", Mode: file.Mode, Bytes: file.Bytes, Sha256: file.Sha256})
		}
	}
	raw, err := json.Marshal(storageOriginalContractRestoreCensus{Schema: "urnetwork-original-contract-restore-v1", Profile: inputs, OriginalCheckpoint: checkpoint})
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: true, Files: files, Census: raw, Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: connect.OriginalContractStoreAttribute}}}, ctx.Err()
}

// No new birth is published until complete copied originals and every signed
// response/request closure pass the Core verifier under original approval.
func inspectStorageOriginalContractRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	expected, err := planStorageOriginalContractRestore(ctx, owner.StagingName, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, owner) {
		return nil, errors.New("original creation restore changed its complete original authority")
	}
	_, scope, path, err := storageOriginalContractProfile(ctx, owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	parent, err := storageOriginalRoot(ctx, root, path)
	if err != nil {
		return nil, err
	}
	originals, checkpoint, err := storageOriginalContractOriginals(report, scope)
	if err != nil {
		return nil, err
	}
	targets := []connect.OriginalContractStoreFile{{Name: "", Device: durablesys.StatDevice(&parent), Inode: parent.Ino, Mode: 0700}}
	fileKVs := map[string]durablevolume.PreparationFile{}
	for _, file := range owner.Files {
		_, observed, err := readStorageOriginalFile(ctx, root, parent, file, coreprotocol.MaximumOriginalContractAdmissionBytes)
		if err != nil {
			return nil, err
		}
		targets = append(targets, connect.OriginalContractStoreFile{Name: file.Path, Device: durablesys.StatDevice(&observed), Inode: observed.Ino, Mode: file.Mode, Bytes: file.Bytes, Sha256: file.Sha256})
		fileKVs[file.Path] = file
	}
	read := func(ctx context.Context, name string) ([]byte, error) {
		raw, _, err := readStorageOriginalFile(ctx, root, parent, fileKVs[name], coreprotocol.MaximumOriginalContractAdmissionBytes)
		return raw, err
	}
	raw, err := connect.RebindOriginalContractStoreInventory(ctx, scope, checkpoint, originals, targets, read)
	if err != nil {
		return nil, storageOriginalContractError(err)
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &after); err != nil {
		return nil, mainnetDurableUnavailable("cannot reobserve restored creation root", err)
	}
	if !bootstrapSuccessorMemberSameStat(parent, after) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("restored creation root changed during complete inspection"))
	}
	return []durablevolume.PreparedAttribute{{Spec: owner.Attributes[0], Raw: raw}}, ctx.Err()
}
