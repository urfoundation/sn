// Public get retries finish before immutable observations are recorded. The
// caller still owns framing, parsing, signatures and every returned byte.
package validator

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	releaseHttpGetAttemptTimeout = 60 * time.Second
	releaseHttpGetRetryTimeout   = 300 * time.Second
)

// Typed status and server pacing come from an actual response, never a stored
// diagnostic string. Permanent status codes retain their original refusal.
type releaseHttpGetStatusError struct {
	endpoint   string
	status     int
	retryAfter time.Duration
}

// Preserve the ordinary artifact reader's diagnostic format.
func (self *releaseHttpGetStatusError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d", self.endpoint, self.status)
}

// Tests advance only the operation deadline and pacing; real requests, bodies,
// response admission and verification still run through their ordinary owners.
type releaseHttpGetRetryHooks struct {
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait        releaseSnapshotRetryWait
}

// Each attempt must close its response before returning. The first permanent
// or mixed failure ends retry; cancellation and expiry preserve the last cause.
func retryReleaseHttpGet(ctx context.Context, read func(context.Context) error, hooks releaseHttpGetRetryHooks) error {
	if ctx == nil || read == nil {
		return errors.New("public get retry owner is incomplete")
	}
	withTimeout, wait := hooks.withTimeout, hooks.wait
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	if wait == nil {
		wait = waitReleaseSnapshotRetry
	}
	operationCtx, cancel := withTimeout(ctx, releaseHttpGetRetryTimeout)
	defer cancel()
	var lastErr error
	for {
		if err := errors.Join(ctx.Err(), operationCtx.Err()); err != nil {
			return errors.Join(lastErr, err)
		}
		lastErr = read(operationCtx)
		if lastErr == nil {
			return errors.Join(ctx.Err(), operationCtx.Err())
		}
		if !RetryableEvidenceTransportError(lastErr) {
			return lastErr
		}
		delay := releaseSnapshotStartupRetryDelay
		var status *releaseHttpGetStatusError
		if errors.As(lastErr, &status) {
			delay = max(delay, status.retryAfter)
		}
		if err := wait(operationCtx, delay); err != nil {
			return errors.Join(lastErr, err, ctx.Err())
		}
	}
}
