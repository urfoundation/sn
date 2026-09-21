package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// A bounded read window may end without disproving historical evidence.
// Only the preparation controller may reopen that window; final verification
// and ordinary RPC callers retain their existing bounded retry contract.
func historicalPreparationReadIsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if _, fileError := err.(*os.PathError); fileError {
		return false
	}
	if exhausted, ok := err.(*evmReadRpcExhaustedError); ok {
		return historicalPreparationReadIsTransient(exhausted.cause)
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !historicalPreparationReadIsTransient(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return historicalPreparationReadIsTransient(cause)
		}
	}
	return evmReadRpcErrorIsTransient(err)
}

// Reconcile again after transient read exhaustion, keeping authenticated cache
// successes on disk. Each pass freshly validates current state, receipt bytes
// and canonical checkpoints; unresolved immutable groups alone need replay.
func continueHistoricalPreparation(ctx context.Context, verify func(context.Context) error) error {
	return continueHistoricalPreparationWithWait(ctx, verify, waitFinalSemanticRPCRetry)
}

func continueHistoricalPreparationWithWait(ctx context.Context, verify func(context.Context) error, wait func(context.Context, time.Duration) error) error {
	if ctx == nil || verify == nil || wait == nil {
		return errors.New("historical preparation continuation is incomplete")
	}
	delay := 5 * time.Second
	for round := uint64(1); ; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := verify(ctx)
		if parentErr := ctx.Err(); parentErr != nil {
			return errors.Join(err, parentErr)
		}
		if !historicalPreparationReadIsTransient(err) {
			return err
		}
		// A large census can report the same outage for thousands of actions.
		// Keep the log bounded without discarding the typed failure returned on
		// cancellation or the complete next-pass integrity checks.
		detail := err.Error()
		if len(detail) > 512 {
			detail = detail[:512] + "..."
		}
		fmt.Fprintf(os.Stderr, "sim-testnet: historical preparation deferred after read window %d; retained proofs will resume in %s: %s\n", round, delay, detail)
		if waitErr := wait(ctx, delay); waitErr != nil {
			return errors.Join(err, waitErr)
		}
		delay = min(2*delay, 30*time.Second)
	}
}
