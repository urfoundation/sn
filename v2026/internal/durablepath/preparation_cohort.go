// Cohort operations retain the existing explicit instance facts seam. Runtime
// constructors cannot select a fake host or enroll any missing declaration.
package durablepath

import (
	"context"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Borrows the fixed daemon registry for synchronous read-only cohort admission.
func CheckPreparationCohort(ctx context.Context, reference durablevolume.Reference, adapter durablevolume.PreparationAdapter) (durablevolume.PreparationCohortResult, error) {
	if ctx != nil {
		if host, present := ctx.Value(hostKey{}).(durablevolume.Host); present {
			return durablevolume.CheckPreparationCohortWithHost(ctx, reference, adapter, host)
		}
	}
	return durablevolume.CheckPreparationCohort(ctx, reference, adapter)
}

// Borrows the fixed registry; all roots are joined before returning any result.
func ApplyPreparationCohort(ctx context.Context, reference durablevolume.Reference, adapter durablevolume.PreparationAdapter) (durablevolume.PreparationCohortResult, error) {
	if ctx != nil {
		if host, present := ctx.Value(hostKey{}).(durablevolume.Host); present {
			return durablevolume.ApplyPreparationCohortWithHost(ctx, reference, adapter, host)
		}
	}
	return durablevolume.ApplyPreparationCohort(ctx, reference, adapter)
}
