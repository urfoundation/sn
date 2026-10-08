// Lifecycle and capacity failures preserve their original typed causes.
// Evidence conflicts remain terminal even when joined with cancellation;
// an operational refusal cannot manufacture an integrity marker.
package main

import (
	"context"
	"errors"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func nativeExecutionDerivationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errRpcIntegrity) || errors.Is(err, errRpcIdentityMismatch) || errors.Is(err, durablevolume.ErrIdentity) {
		return err
	}
	if monitorOnlyCancellationCauses(err, 0) || errors.Is(err, errMonitorEconomicCapacity) {
		return err
	}
	return errors.Join(errRpcIntegrity, err)
}

// Hooks observe real derivation boundaries, never supply evidence or verdicts.
// They are private to an owned context and cannot be selected by CLI input.
type nativeYumaOwnerHooksKey struct{}
type nativeYumaOwnerHooks struct {
	before func(string)
}

func nativeYumaOwnerStep(ctx context.Context, phase string) {
	if ctx == nil {
		return
	}
	if hooks, ok := ctx.Value(nativeYumaOwnerHooksKey{}).(nativeYumaOwnerHooks); ok && hooks.before != nil {
		hooks.before(phase)
	}
}

// A bounded read deadline is local unavailability; a canceled parent abandons
// the candidate. Existing integrity markers dominate both operational causes.
func economicConservationAppendHeld(err error) bool {
	return monitorEconomicNativeReadCode(err) == "identity-conflict" || err != nil && !errors.Is(err, errMonitorEconomicCapacity) && !monitorOnlyCancellationCauses(err, 0)
}
