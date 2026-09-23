//go:build linux || darwin

// A failed capture read retains the existing bounded retry owner. Synthetic
// observations force both incomplete reads and completed identity changes.
package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Each capture/continuation verdict must remain unreachable until its pinned
// read has completed; otherwise an ordinary deadline stops the retry owner.
func TestCaptureRpcObservationTimeoutRetriesSamePinnedValue(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		"compact companion signed transaction is absent",
		"compact companion actual receipt differs from finalized journal",
		"compact companion receipt block is not canonical",
		"compact companion terminal identity changed",
		"relay continuation independent finalized nonce differs",
		"relay continuation approved EVM snapshot is no longer canonical",
	} {
		expected := ChainHead{Number: 42, Hash: finalTestHex(0x42)}
		observed := ChainHead{}
		mismatch := errors.New(message)
		deadline := fmt.Errorf("synthetic pinned read: %w", context.DeadlineExceeded)
		attempts, waits := 0, 0
		policy := defaultFinalSemanticRPCRetryPolicy()
		policy.wait = func(ctx context.Context, _ time.Duration) error {
			waits++
			return ctx.Err()
		}
		err := retryEvmReadRpcCall(t.Context(), "synthetic capture observation", policy, func(ctx context.Context) error {
			attempts++
			if ctx.Err() != nil {
				t.Fatal("retry inherited an expired request context")
			}
			if attempts == 1 {
				err := captureRpcObservationError(deadline, observed == expected, mismatch)
				if err != deadline || errors.Is(err, mismatch) {
					t.Fatalf("incomplete %s observation acquired an identity verdict: %v", message, err)
				}
				return err
			}
			observed = expected
			return captureRpcObservationError(nil, observed == expected, mismatch)
		})
		if err != nil || attempts != 2 || waits != 1 || observed != expected {
			t.Fatalf("%s did not recover its exact pinned value: attempts=%d waits=%d observed=%+v err=%v", message, attempts, waits, observed, err)
		}
	}
}

// A completed mismatching observation must still stop after its first read,
// even though an unavailable observation at the same seam is recoverable.
func TestCaptureRpcObservationCompletedMismatchIsPermanent(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		"compact companion signed transaction is absent",
		"compact companion actual receipt differs from finalized journal",
		"compact companion receipt block is not canonical",
		"compact companion terminal identity changed",
		"relay continuation independent finalized nonce differs",
		"relay continuation approved EVM snapshot is no longer canonical",
	} {
		mismatch := errors.New(message)
		expected := ChainHead{Number: 42, Hash: finalTestHex(0x42)}
		observed := ChainHead{Number: 42, Hash: finalTestHex(0x43)}
		attempts := 0
		policy := defaultFinalSemanticRPCRetryPolicy()
		policy.wait = func(context.Context, time.Duration) error {
			t.Fatal("a completed identity mismatch entered retry backoff")
			return nil
		}
		err := retryEvmReadRpcCall(t.Context(), "synthetic capture observation", policy, func(context.Context) error {
			attempts++
			return captureRpcObservationError(nil, observed == expected, mismatch)
		})
		if err != mismatch || attempts != 1 || evmReadRpcErrorIsTransient(err) {
			t.Fatalf("%s lost its hard identity check: attempts=%d err=%v", message, attempts, err)
		}
	}
}

// Separating a read failure must not remove its independently established
// integrity cause, revive an exhausted retry budget, or retry cancellation.
func TestCaptureRpcObservationPreservesHardReadFailures(t *testing.T) {
	t.Parallel()
	integrity := errors.New("synthetic retained source identity differs")
	for _, readErr := range []error{
		errors.Join(integrity, context.DeadlineExceeded),
		&evmReadRpcExhaustedError{operation: "synthetic captured block", attempts: 4, attemptTimeout: time.Second, cause: context.DeadlineExceeded},
		context.Canceled,
	} {
		attempts := 0
		policy := defaultFinalSemanticRPCRetryPolicy()
		policy.wait = func(context.Context, time.Duration) error {
			t.Fatal("hard read failure gained another retry budget")
			return nil
		}
		err := retryEvmReadRpcCall(t.Context(), "synthetic capture observation", policy, func(context.Context) error {
			attempts++
			return captureRpcObservationError(readErr, false, errors.New("unobserved mismatch"))
		})
		if err != readErr || attempts != 1 || evmReadRpcErrorIsTransient(err) {
			t.Fatalf("capture changed read ownership: attempts=%d original=%v err=%v", attempts, readErr, err)
		}
	}
}
