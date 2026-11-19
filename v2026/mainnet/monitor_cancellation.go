// A parent cancellation can race the retained checkpoint load after successful
// admission. It joins that owner without reporting an invented custody loss.
package main

import (
	"context"
	"errors"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func monitorCanceledCheckpointLoad(ctx context.Context, err error) bool {
	if ctx.Err() == nil || !errors.Is(err, ctx.Err()) {
		return false
	}
	var ownership *monitorOutputOwnershipError
	var cleanup *monitorAdmissionCleanupError
	var refused *monitorAdmissionRefusalError
	return !errors.Is(err, durablevolume.ErrIdentity) && !errors.Is(err, errRpcIdentityMismatch) && !errors.Is(err, errRpcIntegrity) && !errors.As(err, &ownership) && !errors.As(err, &cleanup) && !errors.As(err, &refused) && monitorOnlyCancellationCauses(err, 0)
}

// Joined independent I/O/close failures are not canceled observations. Owned
// display wrappers can join a child deadline and parent cancellation without
// turning either into changed custody. Every leaf must be a cancellation.
func monitorOnlyCancellationCauses(err error, depth int) bool {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	if err == nil || depth >= 32 {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !monitorOnlyCancellationCauses(cause, depth+1) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return monitorOnlyCancellationCauses(wrapped.Unwrap(), depth+1)
	}
	return false
}
