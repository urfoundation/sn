// Physical adoption authenticates every original/derived census image before
// selecting the already-retained next head. Preview never repairs that head.
package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// One signed restore-plan digest binds both images. The existing rebind fields
// select its logical continuation; historical single-image signing stays exact.
type bootstrapSuccessorRebindCensus struct {
	Census       bootstrapSuccessorMemberCensus
	Derivation   durablevolume.PreparationDerivation
	OuterPending bool
}

// All sources are checked as a complete disjoint union before either image is
// derived. Only physical inode fields of the fixed member census may change.
func buildBootstrapSuccessorRebindCensus(ctx context.Context, plan durablevolume.PreparationPlan, index int, original durablevolume.PreparationOwnerPlan, inventory durablevolume.Inventory) (result bootstrapSuccessorRebindCensus, resultErr error) {
	if ctx == nil || index < 0 || index >= len(plan.Owners) || original.PhysicalMetadata == nil {
		return result, errors.New("successor physical rebind lacks its original member owner")
	}
	paths := map[string]bool{original.PhysicalMetadata.Path: true}
	if companion := original.PhysicalMetadata.CompanionPath; companion != "" {
		if paths[companion] {
			return result, errors.New("successor physical rebind aliases its metadata pair")
		}
		paths[companion] = true
	}
	if len(plan.Derivations) != len(paths) {
		return result, errors.New("successor physical rebind omits an original metadata derivation")
	}
	expected := original
	expected.Files = append([]durablevolume.PreparationFile(nil), original.Files...)
	derivations := map[string]durablevolume.PreparationDerivation{}
	for _, derivation := range plan.Derivations {
		path := derivation.Original.File.Path
		if derivation.OwnerIndex != index || !paths[path] || derivation.Derived.Path != path {
			return result, errors.New("successor physical rebind changed its metadata owner or name")
		}
		if _, found := derivations[path]; found {
			return result, errors.New("successor physical rebind repeats an original metadata image")
		}
		found := false
		for fileIndex, file := range expected.Files {
			if file.Path != path {
				continue
			}
			if file != derivation.Original.File || found {
				return result, errors.New("successor physical rebind changed original census authority")
			}
			expected.Files[fileIndex], found = derivation.Derived, true
		}
		if !found {
			return result, errors.New("successor physical rebind invents a census image")
		}
		derivations[path] = derivation
	}
	if !reflect.DeepEqual(expected, plan.Owners[index]) {
		return result, errors.New("successor physical rebind changed original member bytes or capacities")
	}
	files := map[string]durablevolume.PreparationFile{}
	members := map[string]bool{}
	for ownerIndex, owner := range plan.Owners {
		for _, file := range owner.Files {
			if _, found := files[file.Path]; found {
				return result, errors.New("successor physical rebind overlaps target ownership")
			}
			files[file.Path] = file
			if ownerIndex == index && !paths[file.Path] {
				members[file.Path] = true
			}
		}
	}
	var targets []durablevolume.PreparationSource
	for _, source := range plan.Sources {
		if file, found := files[source.File.Path]; !found || file != source.File {
			return result, errors.New("successor physical rebind lost or repeated a reviewed target source")
		}
		delete(files, source.File.Path)
		if members[source.File.Path] {
			targets = append(targets, source)
		}
	}
	if len(files) != 0 {
		return result, errors.New("successor physical rebind omitted a reviewed target source")
	}
	spec := bootstrapSuccessorMemberSpec(original.Owner.Kind == "mainnet-successor-nonce-members")
	head, _, err := storagePreparationMemberRestoreHead(spec, inventory)
	if err != nil {
		return result, err
	}
	selected := head.Committed
	if head.Pending != nil {
		selected, result.OuterPending = head.Pending.Next, true
	}
	selectedPath := ""
	for _, entry := range inventory.Entries {
		if paths[entry.Path] && entry.Physical != nil && entry.Physical.Inode == selected.Inode {
			if selectedPath != "" || selected.Size < 0 || entry.Size != uint64(selected.Size) || strings.TrimPrefix(entry.Sha256, "sha256:") != selected.Sha256 {
				return result, errors.New("successor physical rebind has an ambiguous original next image")
			}
			selectedPath = entry.Path
		}
	}
	if selectedPath == "" {
		return result, errors.New("successor physical rebind lost its retained next image")
	}
	for _, derivation := range plan.Derivations {
		raw, hash, err := readBootstrapRootFile(ctx, derivation.Original.Path, maximumBootstrapSuccessorMemberCensusBytes)
		if err != nil || hash != derivation.Original.File.Sha256 || uint64(len(raw)) != derivation.Original.File.Bytes {
			return result, errors.Join(errors.New("successor physical rebind original census lineage is unavailable"), err)
		}
		derived, err := rebindStoragePreparationMembersRestore(ctx, original, inventory, raw, targets, false)
		if err != nil || uint64(len(derived)) != derivation.Derived.Bytes || safeReleaseHash(derived) != derivation.Derived.Sha256 {
			return result, errors.Join(errors.New("successor physical rebind derived census differs from its reviewed original"), err)
		}
		if derivation.Original.File.Path == selectedPath {
			if err := decodePlanJson(derived, &result.Census); err != nil {
				return result, err
			}
			result.Derivation = derivation
		}
	}
	return result, ctx.Err()
}

// An exact pending restore can be reviewed under SH without repair. After the
// approved writer joins/reconciles, ordinary prefix inspection applies instead.
func inspectBootstrapSuccessorRestoredCensus(ctx context.Context, path string, expected bootstrapSuccessorRebindCensus, owner durablevolume.PreparationOwnerPlan, inventory durablevolume.Inventory, global bool) (resultErr error) {
	if !expected.OuterPending {
		return inspectBootstrapSuccessorRebindTarget(ctx, path, expected.Census, global)
	}
	storage, err := openMainnetDurableDirectory(ctx, path, durablevolume.ReadOnly)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, storage.close()) }()
	root := storage.directory.File()
	if err := mainnetDurableFlock(int(root.Fd()), unix.LOCK_SH); err != nil {
		return err
	}
	spec := bootstrapSuccessorMemberSpec(global)
	name := durablehead.Attribute(spec.Kind, spec.Name)
	raw := make([]byte, 4097)
	n, err := unix.Fgetxattr(int(root.Fd()), name, raw)
	if err != nil || n < 0 || n > 4096 {
		return errors.Join(errors.New("successor physical rebind lost its reviewed outer checkpoint"), err)
	}
	raw = raw[:n]
	var head durablehead.Checkpoint
	if err := decodePlanJson(raw, &head); err != nil {
		return err
	}
	if head.Pending == nil {
		// Release this inspection before the ordinary owner takes its own SH.
		if err := storage.close(); err != nil {
			return err
		}
		return inspectBootstrapSuccessorRebindTarget(ctx, path, expected.Census, global)
	}
	attributes, err := inspectStoragePreparationMembersRestore(ctx, root, owner, inventory, false)
	if err != nil || len(attributes) != 1 || attributes[0].Spec.Path != "." || attributes[0].Spec.Name != name || !bytes.Equal(raw, attributes[0].Raw) {
		return errors.Join(errors.New("successor physical rebind pending checkpoint differs from its exact reviewed pair"), err)
	}
	// Read back the checkpoint after every physical member/image observation.
	after := make([]byte, 4097)
	n, err = unix.Fgetxattr(int(root.Fd()), name, after)
	if err != nil || n < 0 || n > 4096 || !bytes.Equal(raw, after[:n]) {
		return errors.Join(errors.New("successor physical rebind checkpoint changed during inspection"), err)
	}
	return errors.Join(ctx.Err(), storage.check(root))
}
