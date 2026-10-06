//go:build linux

// Local successor history shares a bootstrap root with separately retained
// role snapshots. Its restore view uses the actual runtime namespace; the core
// independently refuses any omitted or overlapping source member or head.
package main

import (
	"context"
	"errors"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Global nonce custody remains exclusive. Only local history has a fixed
// disjoint namespace; legacy omission retains the old complete-owner profile.
func storagePreparationMembersRestoreView(ctx context.Context, owner durablevolume.PreparationOwner, spec durablehead.Spec, report durablevolume.Inventory) (durablevolume.Inventory, error) {
	if owner.RestoreCoverage == "" {
		return report, nil
	}
	if owner.RestoreCoverage != durablevolume.PreparationCompleteUnion || owner.Kind != "mainnet-successor-local-members" {
		return durablevolume.Inventory{}, errors.New("member restore coverage is only defined for the local successor namespace")
	}
	if ctx == nil {
		return durablevolume.Inventory{}, errors.New("member restore coverage requires a bounded context")
	}
	head, _, err := storagePreparationMemberRestoreHead(spec, report)
	if err != nil {
		return durablevolume.Inventory{}, err
	}
	view := report
	view.Entries = nil
	for _, entry := range report.Entries {
		if err := ctx.Err(); err != nil {
			return durablevolume.Inventory{}, err
		}
		if entry.Path != "" && entry.Path != spec.Name && (head.Pending == nil || entry.Path != head.Pending.Temporary) && !bootstrapSuccessorMemberOwns(spec, false, entry.Path) {
			continue
		}
		if entry.Path == "" {
			original := entry.OwnerAttributes
			entry.OwnerAttributes = nil
			for _, attribute := range original {
				if attribute.Name == durablevolume.PreparationAttribute || attribute.Name == durablehead.Attribute(spec.Kind, spec.Name) {
					entry.OwnerAttributes = append(entry.OwnerAttributes, attribute)
				}
			}
		}
		view.Entries = append(view.Entries, entry)
	}
	return view, ctx.Err()
}
