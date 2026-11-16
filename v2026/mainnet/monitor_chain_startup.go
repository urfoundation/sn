// Startup retries keep the original checkpoint owner until every observation
// and wait is joined. No retry writes a head or creates missing retained state.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// A complete checkpoint observation owns one finite read window. Regular-file
// operations remain synchronous; cancellation never abandons a kernel reader.
// The first and last failed causes survive exhaustion without an unbounded log.
func loadMonitorChainCheckpoint(ctx context.Context, checkpoint *monitorCheckpointStore, budget time.Duration, stdout, diagnostic io.Writer, now func() time.Time, hooks monitorServiceHooks) (*monitorState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	operation, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	deadline := now().Add(budget)
	backoff := time.Second
	var firstErr, lastErr error
	for operation.Err() == nil && now().Before(deadline) {
		state, err := checkpoint.load()
		// This existing caller-local observation barrier follows real reads.
		// It can withhold admission but cannot provide checkpoint contents.
		if after, ok := ctx.Value(monitorStartupObservationKey{}).(func(context.Context, string) error); err == nil && ok && after != nil {
			err = after(operation, "chain")
		}
		if err == nil && operation.Err() == nil && now().Before(deadline) {
			if firstErr != nil {
				publishMonitorAdmission("chain", "admitted", nil, 0, stdout, diagnostic, now)
			}
			return state, nil
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			lastErr = err
			if monitorCanceledCheckpointLoad(operation, err) {
				return nil, errors.Join(firstErr, lastErr, operation.Err())
			}
			if !monitorStartupPending(err) {
				return nil, &monitorAdmissionRefusalError{cause: errors.Join(firstErr, lastErr)}
			}
			fmt.Fprintln(diagnostic, "monitor chain checkpoint observation:", err)
		}
		if operation.Err() != nil || !now().Before(deadline) {
			break
		}
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			break
		}
		wait := min(backoff, remaining)
		publishMonitorAdmission("chain", "pending", lastErr, wait, stdout, diagnostic, now)
		var waitErr error
		if hooks.rpcWait != nil {
			waitErr = hooks.rpcWait(operation, "chain", wait)
		} else {
			waitErr = waitRpcReadRetry(operation, wait)
		}
		if waitErr != nil {
			return nil, errors.Join(firstErr, lastErr, waitErr)
		}
		backoff = min(2*backoff, 10*time.Second)
	}
	cause := operation.Err()
	if cause == nil {
		cause = context.DeadlineExceeded
	}
	return nil, errors.Join(firstErr, lastErr, cause)
}
