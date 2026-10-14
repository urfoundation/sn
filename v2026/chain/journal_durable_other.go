//go:build !linux && !darwin

// Production custody has no implicit weaker fallback on unsupported platforms.
package chain

import (
	"context"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Daemon volume identity currently requires the reviewed Linux implementation.
func OpenDurableJournal(context.Context, string) (*Journal, error) {
	return nil, durablevolume.ErrUnsupported
}

// Explicit owner-local policy retains the same Linux limitation.
func OpenOwnerLocalJournal(context.Context, string) (*Journal, error) {
	return nil, durablevolume.ErrUnsupported
}

// Recovery has no implicit filesystem fallback on unsupported hosts.
func ReconcileDurableJournal(context.Context, string) (*Journal, error) {
	return nil, durablevolume.ErrUnsupported
}

// Owner-local recovery preserves the same explicit platform restriction.
func ReconcileOwnerLocalJournal(context.Context, string) (*Journal, error) {
	return nil, durablevolume.ErrUnsupported
}
