//go:build linux || darwin

// Offline miner restoration selects its existing fleet or claim profile before
// physical checkpoint rebinding. Original JSON and marker bytes are unchanged;
// runtime authentication still belongs to the separately activated owner.
package miner

import (
	"context"
	"os"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The explicit owner-local command is mandatory for fleet recovery; a claim
// queue cannot inherit that scope by changing its request JSON.
func PlanStorageRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	spec, profile, err := minerStoragePreparationProfile(ownerLocal, owner)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablehead.PlanRestore(ctx, name, owner, spec, profile, report)
}

// This read-only adapter starts no fleet/claim runtime and publishes no state.
func InspectStorageRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	spec, profile, err := minerStoragePreparationProfile(ownerLocal, owner.Owner)
	if err != nil {
		return nil, err
	}
	return durablehead.InspectRestore(ctx, root, owner, spec, profile, report)
}
