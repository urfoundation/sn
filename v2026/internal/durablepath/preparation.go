// Preparation has an explicit, separate admission path. It never makes an
// unenrolled runtime Open succeed and cannot inherit owner-local scope from
// request contents. The existing instance-only Host seam supplies facts only.
package durablepath

import (
	"context"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Default scope is always daemon, with the established instance-only facts seam.
func PlanPreparation(ctx context.Context, reference durablevolume.Reference, adapter durablevolume.PreparationAdapter) (durablevolume.PreparationPlan, error) {
	if ctx != nil {
		if host, present := ctx.Value(hostKey{}).(durablevolume.Host); present {
			return durablevolume.PlanPreparationWithHost(ctx, reference, adapter, host)
		}
	}
	return durablevolume.PlanPreparation(ctx, reference, adapter)
}

// Owner-local scope is explicit and cannot be inherited from request contents.
func PlanOwnerLocalPreparation(ctx context.Context, reference durablevolume.Reference, adapter durablevolume.PreparationAdapter) (durablevolume.PreparationPlan, error) {
	if ctx != nil {
		if host, present := ctx.Value(hostKey{}).(durablevolume.Host); present {
			return durablevolume.PlanOwnerLocalPreparationWithHost(ctx, reference, adapter, host)
		}
	}
	return durablevolume.PlanOwnerLocalPreparation(ctx, reference, adapter)
}

// Accepted daemon bytes are applied with the same physical host as the caller.
func ApplyPreparation(ctx context.Context, reference durablevolume.Reference, adapter durablevolume.PreparationAdapter) (durablevolume.PreparationResult, error) {
	if ctx != nil {
		if host, present := ctx.Value(hostKey{}).(durablevolume.Host); present {
			return durablevolume.ApplyPreparationWithHost(ctx, reference, adapter, host)
		}
	}
	return durablevolume.ApplyPreparation(ctx, reference, adapter)
}

// The owner-local entry point stays separate even when both scopes use one device.
func ApplyOwnerLocalPreparation(ctx context.Context, reference durablevolume.Reference, adapter durablevolume.PreparationAdapter) (durablevolume.PreparationResult, error) {
	if ctx != nil {
		if host, present := ctx.Value(hostKey{}).(durablevolume.Host); present {
			return durablevolume.ApplyOwnerLocalPreparationWithHost(ctx, reference, adapter, host)
		}
	}
	return durablevolume.ApplyOwnerLocalPreparation(ctx, reference, adapter)
}
