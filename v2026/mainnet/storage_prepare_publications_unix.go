//go:build linux || darwin

package main

// Published request windows and interrupted writer files have an explicit
// complete owner. Retained bytes never choose their own request birth or key.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const storageProviderPublicationKind = "validator-provider-attempt-publications"
const storageProviderPublicationPreparationSchema = "urnetwork-provider-attempt-publication-preparation-v1"

type storageProviderPublicationPreparationScope struct {
	Schema             string                  `json:"schema"`
	PreparationProfile durablevolume.Reference `json:"preparation_profile"`
}

type storageProviderPublicationRestoreCensus struct {
	Schema             string                                     `json:"schema"`
	Profile            storageProviderPublicationPreparationScope `json:"profile"`
	OriginalCheckpoint []byte                                     `json:"original_checkpoint"`
}

// Original allowance and full operator roster are approved outside this tree.
func storageProviderPublicationProfile(ctx context.Context, owner durablevolume.PreparationOwner, ownerLocal bool) (storageProviderPublicationPreparationScope, validator.ProviderAttemptPublicationPreparation, error) {
	var inputs storageProviderPublicationPreparationScope
	var profile validator.ProviderAttemptPublicationPreparation
	if ctx == nil || ownerLocal || owner.Kind != storageProviderPublicationKind || owner.Purpose != "fresh" && owner.Purpose != "restore" || owner.RelativePath != "." || owner.RestoreCoverage != "" {
		return inputs, profile, errors.New("provider publication custody requires its exclusive daemon profile")
	}
	if err := decodePlanJson(owner.Inputs, &inputs); err != nil {
		return inputs, profile, err
	}
	if inputs.Schema != storageProviderPublicationPreparationSchema || !planSha256(inputs.PreparationProfile.Sha256) {
		return inputs, profile, errors.New("provider publication approval reference is absent or unknown")
	}
	raw, _, err := readPlanFile(ctx, inputs.PreparationProfile.Path, 256*1024)
	if err != nil {
		return inputs, profile, err
	}
	if safeReleaseHash(raw) != inputs.PreparationProfile.Sha256 {
		return inputs, profile, errors.New("provider publication profile differs from its independent approved bytes")
	}
	if err := decodePlanJson(raw, &profile); err != nil {
		return inputs, profile, err
	}
	canonical, err := json.Marshal(profile)
	if err != nil || !bytes.Equal(raw, canonical) {
		return inputs, profile, errors.Join(errors.New("provider publication profile is not canonical"), err)
	}
	if err := profile.Validate(); err != nil {
		return inputs, profile, err
	}
	directory := profile.Directory()
	if inputs.PreparationProfile.Path == directory || strings.HasPrefix(inputs.PreparationProfile.Path, directory+string(filepath.Separator)) {
		return inputs, profile, errors.New("provider publication authority overlaps original custody")
	}
	return inputs, profile, ctx.Err()
}

// Birth is an actual empty stopped-writer preparation, never an open fallback.
func buildStorageProviderPublication(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	inputs, _, err := storageProviderPublicationProfile(ctx, owner, ownerLocal)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if owner.Purpose != "fresh" {
		return durablevolume.PreparationOwnerPlan{}, errors.New("provider publication birth requires explicit fresh preparation")
	}
	files := []durablevolume.PreparationFile{}
	if err := storagePreparationStageEmpty(ctx, staging, name, files); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: true, Files: files, Census: raw, Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: validator.ProviderAttemptPublicationNamespaceAttribute}}}, ctx.Err()
}

func inspectStorageProviderPublication(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	inputs, profile, err := storageProviderPublicationProfile(ctx, owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	var retained storageProviderPublicationPreparationScope
	if err := decodePlanJson(owner.Census, &retained); err != nil {
		return nil, err
	}
	attribute := durablevolume.PreparationAttributeSpec{Path: ".", Name: validator.ProviderAttemptPublicationNamespaceAttribute}
	if owner.Owner.Purpose != "fresh" || !owner.ExclusiveRoot || retained != inputs || len(owner.Files) != 0 || len(owner.Attributes) != 1 || owner.Attributes[0] != attribute || owner.PhysicalMetadata != nil {
		return nil, errors.New("provider publication birth changed its reviewed empty profile")
	}
	if _, err := storageOriginalRoot(ctx, root, profile.Directory()); err != nil {
		return nil, err
	}
	info, err := root.Stat()
	if err != nil {
		return nil, mainnetDurableUnavailable("cannot observe publication birth directory", err)
	}
	raw, err := validator.FreshProviderAttemptPublicationNamespaceAttribute(profile, info)
	if err != nil {
		return nil, err
	}
	return []durablevolume.PreparedAttribute{{Spec: attribute, Raw: raw}}, ctx.Err()
}

// Only the runtime's exact public names and exact crash temporary grammar fit.
// Temporary bytes are preserved as inert custody; they never advance a window.
func storageProviderPublicationName(name string) (epoch, noId uint64, temporary bool, resultErr error) {
	if strings.HasPrefix(name, ".compact-input-") {
		nonce := strings.TrimPrefix(name, ".compact-input-")
		decoded, err := hex.DecodeString(nonce)
		if err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == nonce {
			return 0, 0, true, nil
		}
		return 0, 0, false, errors.New("provider publication crash name is not canonical")
	}
	if len(name) != 57 || !strings.HasPrefix(name, "epoch-") || name[26:32] != "-noid-" || !strings.HasSuffix(name, ".json") {
		return 0, 0, false, errors.New("provider publication original name is unknown")
	}
	epoch, err := strconv.ParseUint(name[6:26], 10, 64)
	if err != nil {
		return 0, 0, false, err
	}
	noId, err = strconv.ParseUint(name[32:52], 10, 64)
	if err != nil || noId == 0 || fmt.Sprintf("epoch-%020d-noid-%020d.json", epoch, noId) != name {
		return 0, 0, false, errors.Join(errors.New("provider publication original name differs"), err)
	}
	return epoch, noId, false, nil
}

// Inventory covers every original publication and every interrupted writer.
func storageProviderPublicationOriginals(report durablevolume.Inventory, profile validator.ProviderAttemptPublicationPreparation) ([]durablevolume.PreparationFile, []byte, error) {
	if report.Schema != durablevolume.PhysicalInventorySchema || report.RestartAuthorized || len(report.Entries) == 0 || uint64(len(report.Entries)-1) > profile.MaxFiles || report.Entries[0].Path != "" {
		return nil, nil, errors.New("provider publication restore requires its complete bounded original inventory")
	}
	files := []durablevolume.PreparationFile{}
	var checkpoint []byte
	var used uint64
	seenKVs, inodeKVs, operatorKVs := map[string]bool{}, map[uint64]bool{}, map[uint64]bool{}
	for _, operator := range profile.Operators {
		operatorKVs[operator.Preparation.Identity.Ledger.NoID] = true
	}
	for _, entry := range report.Entries {
		if entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || seenKVs[entry.Path] || inodeKVs[entry.Physical.Inode] || entry.Uid != report.Entries[0].Uid {
			return nil, nil, errors.New("provider publication original names or physical generations differ")
		}
		seenKVs[entry.Path], inodeKVs[entry.Physical.Inode] = true, true
		if entry.Path == "" {
			if entry.Kind != "directory" || entry.Mode != 0700 || *entry.Physical != report.PhysicalRoot {
				return nil, nil, errors.New("provider publication original root differs")
			}
		} else {
			_, noId, temporary, err := storageProviderPublicationName(entry.Path)
			if err != nil || !temporary && !operatorKVs[noId] || entry.Kind != "file" || entry.Mode != 0600 || entry.Size > profile.MaxWindowBytes || entry.Size > profile.MaxHistoryBytes-used || !planSha256(entry.Sha256) || !temporary && entry.Size == 0 {
				return nil, nil, errors.Join(errors.New("provider publication original is unknown, unapproved or exceeds its allowance"), err)
			}
			used += entry.Size
			files = append(files, durablevolume.PreparationFile{Path: entry.Path, Kind: "file", Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256})
		}
		for _, attribute := range entry.OwnerAttributes {
			if entry.Path == "" && attribute.Name == durablevolume.PreparationAttribute {
				continue
			}
			if entry.Path != "" || attribute.Name != validator.ProviderAttemptPublicationNamespaceAttribute || checkpoint != nil || len(attribute.Value) == 0 || len(attribute.Value) > 4096 || safeReleaseHash(attribute.Value) != attribute.Sha256 {
				return nil, nil, errors.New("provider publication cannot discard another original owner attribute")
			}
			checkpoint = bytes.Clone(attribute.Value)
		}
	}
	var head validator.ProviderAttemptPublicationNamespaceCheckpoint
	if err := decodePlanJson(checkpoint, &head); err != nil {
		return nil, nil, err
	}
	canonical, err := json.Marshal(head)
	if err != nil || !bytes.Equal(canonical, checkpoint) {
		return nil, nil, errors.Join(errors.New("provider publication original birth changed runtime grammar"), err)
	}
	digest, err := profile.Digest()
	if err != nil {
		return nil, nil, err
	}
	if head.Schema != validator.ProviderAttemptPublicationNamespaceSchema || head.ProfileSha256 != digest || head.DirectoryDevice != unix.Mkdev(report.PhysicalRoot.Device.Major, report.PhysicalRoot.Device.Minor) || head.DirectoryInode != report.PhysicalRoot.Inode {
		return nil, nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider publication original birth differs from its independent profile"))
	}
	return files, checkpoint, nil
}

func planStorageProviderPublicationRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	inputs, profile, err := storageProviderPublicationProfile(ctx, owner, ownerLocal)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if owner.Purpose != "restore" || report.StateRoot.Path != profile.Directory() || name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return durablevolume.PreparationOwnerPlan{}, errors.New("provider publication restore changed its fixed approved logical path")
	}
	files, checkpoint, err := storageProviderPublicationOriginals(report, profile)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	raw, err := json.Marshal(storageProviderPublicationRestoreCensus{Schema: "urnetwork-provider-attempt-publication-restore-v1", Profile: inputs, OriginalCheckpoint: checkpoint})
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: true, Files: files, Census: raw, Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: validator.ProviderAttemptPublicationNamespaceAttribute}}}, ctx.Err()
}

// Reconstruct each approved operator's full published prefix from its original
// birth. Signed historical chain windows remain facts, not restart permission.
func verifyStorageProviderPublicationWindows(ctx context.Context, root *os.File, parent unix.Stat_t, files []durablevolume.PreparationFile, profile validator.ProviderAttemptPublicationPreparation) error {
	type cursor struct {
		operator validator.ProviderAttemptPublicationOperator
		epoch    uint64
		prior    validator.ProviderAttemptRequestHead
		hash     [32]byte
		endBlock uint64
		started  bool
	}
	cursorKVs := map[uint64]*cursor{}
	for _, operator := range profile.Operators {
		cursorKVs[operator.Preparation.Identity.Ledger.NoID] = &cursor{operator: operator, epoch: operator.Preparation.Birth.SettlementEpoch}
	}
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(first, second durablevolume.PreparationFile) int { return strings.Compare(first.Path, second.Path) })
	for _, file := range ordered {
		raw, _, err := readStorageOriginalFile(ctx, root, parent, file, profile.MaxWindowBytes)
		if err != nil {
			return err
		}
		epoch, noId, temporary, err := storageProviderPublicationName(file.Path)
		if err != nil {
			return err
		}
		if temporary {
			continue
		}
		current := cursorKVs[noId]
		if current == nil || current.epoch != epoch {
			return errors.New("provider publication original chain omits a window or changes its approved owner")
		}
		var cut validator.ProviderAttemptRequestWindow
		if err := decodePlanJson(raw, &cut); err != nil {
			return err
		}
		canonical, err := json.Marshal(cut)
		if err != nil || !bytes.Equal(canonical, raw) {
			return errors.Join(errors.New("provider publication original window is not canonical"), err)
		}
		window := cut.Header.Window
		if window.Epoch != epoch || current.started && current.endBlock != window.StartBlock || !current.started && (current.operator.Preparation.Birth.EVMBlock < window.StartBlock || current.operator.Preparation.Birth.EVMBlock >= window.EndBlock) {
			return errors.New("provider publication original chain changes its birth or window boundary")
		}
		if err := validator.VerifyProviderAttemptPublishedWindow(ctx, cut, current.operator.Preparation, current.operator.ReceiptScope, current.prior, current.hash, window, profile.MaxWindowBytes); err != nil {
			return err
		}
		hash, err := cut.Header.Hash()
		if err != nil {
			return err
		}
		if epoch == ^uint64(0) {
			return errors.New("provider publication original epoch cannot overflow")
		}
		current.epoch, current.prior, current.hash, current.endBlock, current.started = epoch+1, cut.Header.End, hash, window.EndBlock, true
	}
	return ctx.Err()
}

func inspectStorageProviderPublicationRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	expected, err := planStorageProviderPublicationRestore(ctx, owner.StagingName, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, owner) {
		return nil, errors.New("provider publication restore changed its complete original authority")
	}
	_, profile, err := storageProviderPublicationProfile(ctx, owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	parent, err := storageOriginalRoot(ctx, root, profile.Directory())
	if err != nil {
		return nil, err
	}
	if err := verifyStorageProviderPublicationWindows(ctx, root, parent, owner.Files, profile); err != nil {
		return nil, err
	}
	_, original, err := storageProviderPublicationOriginals(report, profile)
	if err != nil {
		return nil, err
	}
	var checkpoint validator.ProviderAttemptPublicationNamespaceCheckpoint
	if err := decodePlanJson(original, &checkpoint); err != nil {
		return nil, err
	}
	// Only original physical coordinates change; original approved birth remains.
	checkpoint.DirectoryDevice, checkpoint.DirectoryInode = durablesys.StatDevice(&parent), parent.Ino
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return nil, err
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &after); err != nil {
		return nil, mainnetDurableUnavailable("cannot reobserve restored publication directory", err)
	}
	if !bootstrapSuccessorMemberSameStat(parent, after) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("restored publication directory changed during complete inspection"))
	}
	return []durablevolume.PreparedAttribute{{Spec: owner.Attributes[0], Raw: raw}}, ctx.Err()
}
