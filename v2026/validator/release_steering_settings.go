// Steering's operational patience is independent of signed release authority.
package validator

import (
	"context"
	"time"
)

// Per-run timing for the complete steering operation, including preparation.
type ReleaseSteeringSettings struct {
	// Finite budget for the complete serial preparation, public replay and
	// submission operation. Thirty minutes leaves room for several full-prefix
	// consumers after sealing; it is not a network completion guarantee.
	// Values below the retained-read window are invalid. Caller cancellation
	// and an earlier caller deadline always take precedence.
	OperationTimeout time.Duration
}

// One finite default for callers that provide no operational override.
func DefaultReleaseSteeringSettings() ReleaseSteeringSettings {
	return ReleaseSteeringSettings{OperationTimeout: 30 * time.Minute}
}

// Runtime flags change operational patience, never the signed configuration.
func releaseSteeringOperationTimeout(ctx context.Context) time.Duration {
	settings := DefaultReleaseSteeringSettings()
	if ctx != nil {
		if override := SettlementV2RuntimeOptionsFrom(ctx).SteeringOperationTimeout; override != 0 {
			settings.OperationTimeout = override
		}
	}
	return settings.OperationTimeout
}

// Both production and legacy steering use the same runtime budget owner.
func runConfiguredReleaseSteeringOperation(ctx context.Context, operation func(context.Context) error) error {
	return runReleaseSteeringOperation(ctx, releaseSteeringOperationTimeout(ctx), operation)
}
