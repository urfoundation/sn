// Startup retries only authenticated read transports. Each failed request
// retains its exact block selector; transaction and live worker paths opt out.
package validator

import (
	"context"
	"errors"
	"time"
)

const releaseStartupRpcRetryTimeout = 300 * time.Second

type releaseStartupRpcReadKey struct{}

// Tests control only the bounded operation clock and wait, never read verdicts.
type releaseStartupRpcReadHooks struct {
	enabled     bool
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait        releaseSnapshotRetryWait
}

// The marker belongs to a startup call's derived context. Neither ChainClient
// nor the returned runtime stores it, so ordinary reads keep their old budget.
func withReleaseStartupRpcReads(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	hooks, _ := ctx.Value(releaseStartupRpcReadKey{}).(releaseStartupRpcReadHooks)
	if hooks.enabled {
		return ctx
	}
	hooks.enabled = true
	return context.WithValue(ctx, releaseStartupRpcReadKey{}, hooks)
}

// One finite budget spans all attempts. A request still has its ordinary
// per-call timeout, and cancellation or any permanent sibling ends recovery.
func retryReleaseStartupRpcRead(ctx context.Context, read func(context.Context) error) error {
	if ctx == nil || read == nil {
		return errors.New("startup Rpc read owner is incomplete")
	}
	call := func(owner context.Context) error {
		callCtx, cancel := context.WithTimeout(owner, chainReadCallTimeout(owner))
		defer cancel()
		return errors.Join(read(callCtx), callCtx.Err())
	}
	hooks, _ := ctx.Value(releaseStartupRpcReadKey{}).(releaseStartupRpcReadHooks)
	if !hooks.enabled {
		return call(ctx)
	}
	withTimeout, wait := hooks.withTimeout, hooks.wait
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	if wait == nil {
		wait = waitReleaseSnapshotRetry
	}
	operationCtx, cancel := withTimeout(ctx, releaseStartupRpcRetryTimeout)
	defer cancel()
	var lastErr error
	for {
		if err := errors.Join(ctx.Err(), operationCtx.Err()); err != nil {
			return errors.Join(lastErr, err)
		}
		lastErr = call(operationCtx)
		if err := errors.Join(ctx.Err(), operationCtx.Err()); err != nil {
			return errors.Join(lastErr, err)
		}
		if lastErr == nil || !RetryableEvidenceTransportError(lastErr) {
			return lastErr
		}
		if err := wait(operationCtx, releaseSnapshotRetryDelayForError(lastErr)); err != nil {
			return errors.Join(lastErr, err, ctx.Err())
		}
	}
}
