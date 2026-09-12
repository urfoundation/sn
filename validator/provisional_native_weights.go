package validator

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfoundation/sn/crv4"
)

// Created only at V2's pre-intent weight assembly/preparation boundaries after
// authenticated provisional startup. No native success or custody is implied.
type provisionalNativeWeightRejection struct {
	nativeEpoch     uint64
	settlementEpoch uint64
	cause           error
}

func (err *provisionalNativeWeightRejection) Error() string {
	return fmt.Sprintf("provisional native epoch %d settlement %d weights rejected: %v; native_submission=false final_acceptance=false", err.nativeEpoch, err.settlementEpoch, err.cause)
}

func (err *provisionalNativeWeightRejection) Unwrap() error { return err.cause }

func classifyProvisionalNativeWeights(ctx context.Context, enabled bool, nativeEpoch, settlementEpoch uint64, err error) error {
	if !enabled || err == nil || ctx == nil || ctx.Err() != nil {
		return err
	}
	var infeasible *crv4.InfeasibleWeightLimitError
	if releaseOnlyErrors(err, errNoPositiveUnmaskedWeights) || (errors.As(err, &infeasible) && releaseOnlyErrors(err, infeasible)) {
		return &provisionalNativeWeightRejection{nativeEpoch: nativeEpoch, settlementEpoch: settlementEpoch, cause: err}
	}
	return err
}
