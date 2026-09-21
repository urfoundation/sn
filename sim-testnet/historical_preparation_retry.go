package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// Each preparation invocation owns one census. Pinned reads already retry
// within their bounded budgets, and completed immutable groups are durable.
// An exhausted census is reported exactly once; a separate audit invocation
// can retry it while explicit provisional startup uses local receipt authority.
func continueHistoricalPreparation(ctx context.Context, verify func(context.Context) error) error {
	if ctx == nil || verify == nil {
		return errors.New("historical preparation continuation is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := verify(ctx)
	if parentErr := ctx.Err(); parentErr != nil {
		return errors.Join(err, parentErr)
	}
	if historicalPreparationReadIsTransient(err) {
		fmt.Fprintln(os.Stderr, "sim-testnet: historical preparation deferred after bounded read retries; retained proofs remain reusable by audit; explicit provisional resume keeps final_acceptance=false")
	}
	return err
}
