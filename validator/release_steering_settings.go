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
	// The pause before the next attempt after an upstream answered 429 with no
	// Retry-After, such as a load balancer's per-address limit. Thirty seconds
	// lets a token bucket refill a burst of a few hundred requests, where
	// retrying at the 2-second poll would keep it empty. Non-positive leaves
	// only server hints.
	RateLimitPause time.Duration
	// The longest Retry-After honored. A longer hint is cut to this, so an
	// owner still looks again well within its 72-minute native epoch; a
	// refused retry just pauses again. Non-positive honors no hint.
	RetryAfterLimit time.Duration
}

// One finite default for callers that provide no operational override.
func DefaultReleaseSteeringSettings() ReleaseSteeringSettings {
	return ReleaseSteeringSettings{
		OperationTimeout: 30 * time.Minute,
		RateLimitPause:   30 * time.Second,
		RetryAfterLimit:  5 * time.Minute,
	}
}

// Runtime flags change operational patience, never the signed configuration.
func releaseSteeringSettingsFrom(ctx context.Context) ReleaseSteeringSettings {
	settings := DefaultReleaseSteeringSettings()
	if ctx != nil {
		if override := SettlementV2RuntimeOptionsFrom(ctx).SteeringOperationTimeout; override != 0 {
			settings.OperationTimeout = override
		}
	}
	return settings
}

func releaseSteeringOperationTimeout(ctx context.Context) time.Duration {
	return releaseSteeringSettingsFrom(ctx).OperationTimeout
}

// Both production and legacy steering use the same runtime budget owner.
func runConfiguredReleaseSteeringOperation(ctx context.Context, operation func(context.Context) error) error {
	return runReleaseSteeringOperation(ctx, releaseSteeringOperationTimeout(ctx), operation)
}
