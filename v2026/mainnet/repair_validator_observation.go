// Observation failures retain their cause; only a completed contradictory read
// establishes custody loss. Private instance hooks can fail, never admit a read.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A deterministic after-syscall barrier cannot supply bytes or replace an error.
// No command, policy or environment option installs this instance-local hook.
type repairValidatorObservationKey struct{}

// A closed observation handle is unavailable evidence, not an observed clock
// or generation change. Positive custody loss and uncertain publication retain
// precedence; this does not broaden durable publication retry admission.
func repairValidatorObservationPending(err error) bool {
	if err == nil || errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, errRpcIntegrity) || errors.Is(err, errMainnetDurablePublicationUncertain) {
		return false
	}
	return mainnetDurableAdmissionPending(err) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF)
}

// Both the caller's cancellation and the actual observation error survive.
func repairValidatorObservation(ctx context.Context, operation string, cause error) error {
	if ctx == nil {
		return errors.Join(errors.New("validator repair observation context is absent"), cause)
	}
	if after, ok := ctx.Value(repairValidatorObservationKey{}).(func(string) error); ok && after != nil {
		cause = errors.Join(cause, after(operation))
	}
	return errors.Join(cause, ctx.Err())
}

// Missing retained names are evidence of loss. Failed descriptor observations,
// including a closed caller-owned descriptor, cannot establish a replacement.
func repairValidatorObservationError(message string, cause error, retainedName bool) error {
	if cause == nil {
		return nil
	}
	if errors.Is(cause, durablevolume.ErrIdentity) || retainedName && (errors.Is(cause, os.ErrNotExist) || errors.Is(cause, syscall.ENOTDIR) || errors.Is(cause, syscall.ELOOP)) {
		return errors.Join(durablevolume.ErrIdentity, errors.New(message), cause)
	}
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, os.ErrClosed) || errors.Is(cause, syscall.EBADF) {
		return fmt.Errorf("%s: %w", message, cause)
	}
	return mainnetDurableUnavailable(message, cause)
}
