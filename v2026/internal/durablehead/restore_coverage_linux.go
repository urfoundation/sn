//go:build linux

// A shared restore view selects only this fixed file-lock owner's namespace.
// It is not an exported inventory or authority: the preparation core must
// independently cover every original member and head with a disjoint union.
package durablehead

import (
	"context"
	"errors"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Legacy omission preserves the original complete-owner interpretation. A
// directory-lock owner has no declared disjoint namespace in this profile.
func snapshotRestoreView(ctx context.Context, owner durablevolume.PreparationOwner, spec Spec, report durablevolume.Inventory) (durablevolume.Inventory, error) {
	if owner.RestoreCoverage == "" {
		return report, nil
	}
	if owner.RestoreCoverage != durablevolume.PreparationCompleteUnion || spec.LockName == "" {
		return durablevolume.Inventory{}, errors.New("snapshot restore coverage requires a fixed file-lock owner")
	}
	if err := ctx.Err(); err != nil {
		return durablevolume.Inventory{}, err
	}
	allowed := map[string]bool{"": true, spec.Name: true, spec.LockName: true}
	for _, name := range spec.AuxiliaryNames {
		allowed[name] = true
	}
	var original []byte
	for _, entry := range report.Entries {
		if entry.Path != spec.LockName {
			continue
		}
		for _, attribute := range entry.OwnerAttributes {
			if attribute.Name != Attribute(spec.Kind, spec.Name) {
				continue
			}
			if original != nil || len(attribute.Value) == 0 || len(attribute.Value) > 4096 || attribute.Sha256 != "sha256:"+digest(attribute.Value) {
				return durablevolume.Inventory{}, errors.New("snapshot restore coverage checkpoint is repeated or changed")
			}
			original = attribute.Value
		}
	}
	if original == nil {
		return durablevolume.Inventory{}, identity(errors.New("snapshot restore coverage original checkpoint is missing"))
	}
	var checkpoint Checkpoint
	if err := decodeSnapshotRestore(original, &checkpoint); err != nil {
		return durablevolume.Inventory{}, err
	}
	if pending := checkpoint.Pending; pending != nil {
		view := &Owner{spec: spec}
		if !view.temporaryName(pending.Temporary) {
			return durablevolume.Inventory{}, identity(errors.New("snapshot restore coverage pending name differs from the fixed owner"))
		}
		allowed[pending.Temporary] = true
	}
	view := report
	view.Entries = nil
	for _, entry := range report.Entries {
		if err := ctx.Err(); err != nil {
			return durablevolume.Inventory{}, err
		}
		if !allowed[entry.Path] {
			continue
		}
		if entry.Path == "" {
			// The root's other heads must be owned elsewhere in the union.
			// Selected member attributes remain intact so unknown heads refuse.
			original := entry.OwnerAttributes
			entry.OwnerAttributes = nil
			for _, attribute := range original {
				if attribute.Name == durablevolume.PreparationAttribute {
					entry.OwnerAttributes = append(entry.OwnerAttributes, attribute)
				}
			}
		}
		view.Entries = append(view.Entries, entry)
	}
	return view, ctx.Err()
}
