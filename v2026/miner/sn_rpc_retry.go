// Read-only EVM recovery owns one operation budget and retains hard failures.
package miner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"time"
)

const (
	ethRpcOperationTimeout = 90 * time.Second
	ethRpcRetryDelay       = 2 * time.Second
	ethRpcMaximumAttempts  = 64
)

// Per-operation hooks allow deadline and cancellation tests without clock
// races. Production supplies neither hook; there is no mutable global state.
type ethRpcRetryHooks struct {
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait        func(context.Context, time.Duration) error
}

// HTTP status carries structured retry eligibility; diagnostic words do not.
type ethRpcStatusError struct {
	method string
	status int
}

// Retain the method and status for callers without inspecting response bodies.
func (self *ethRpcStatusError) Error() string {
	return fmt.Sprintf("%s: http %d", self.method, self.status)
}

// Only the request transport and bounded body reader may assign this origin.
type ethRpcTransportError struct {
	cause error
}

// Preserve the original diagnostic and structured cause.
func (self *ethRpcTransportError) Error() string { return self.cause.Error() }

// Expose timeout and cancellation while retaining transport provenance.
func (self *ethRpcTransportError) Unwrap() error { return self.cause }

// The operation has time to recover a minute-long outage, while each attempt
// retains its shorter request timeout. Cancellation always defeats recovery.
func retryEthRpcRead(ctx context.Context, hooks ethRpcRetryHooks, read func(context.Context) (string, error)) (string, error) {
	withTimeout := hooks.withTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	ctx, cancel := withTimeout(ctx, ethRpcOperationTimeout)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= ethRpcMaximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", errors.Join(lastErr, err)
		}
		value, err := read(ctx)
		if ownerErr := ctx.Err(); ownerErr != nil {
			return "", errors.Join(err, ownerErr)
		}
		if err == nil {
			return value, nil
		}
		lastErr = err
		if !retryableEthRpcError(err, false) || attempt == ethRpcMaximumAttempts {
			return "", err
		}
		delay := ethRpcRetryDelay + time.Duration(rand.Int64N(int64(ethRpcRetryDelay/2)))
		wait := hooks.wait
		if wait == nil {
			wait = func(ctx context.Context, delay time.Duration) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
					return nil
				}
			}
		}
		if err := wait(ctx, delay); err != nil {
			return "", errors.Join(lastErr, err)
		}
	}
	return "", lastErr
}

// Every joined cause must permit retry. A timeout cannot hide malformed JSON,
// an identity conflict, a contract refusal, or a response-close integrity error.
func retryableEthRpcError(err error, transportOrigin bool) bool {
	if err == nil || err == context.Canceled {
		return false
	}
	if _, fileError := err.(*os.PathError); fileError {
		return false
	}
	if err == context.DeadlineExceeded || err == net.ErrClosed || err == syscall.ECONNRESET || err == syscall.ECONNREFUSED || err == syscall.EPIPE || err == syscall.ETIMEDOUT || err == syscall.ENETUNREACH || err == syscall.EHOSTUNREACH {
		return true
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return transportOrigin
	}
	switch cause := err.(type) {
	case *ethRpcStatusError:
		return cause.status == http.StatusTooManyRequests || cause.status >= 500 && cause.status <= 599
	case *ethRpcTransportError:
		return retryableEthRpcError(cause.cause, true)
	case *url.Error:
		return retryableEthRpcError(cause.Err, true)
	case *net.OpError:
		return retryableEthRpcError(cause.Err, true)
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		hasCause := false
		for _, cause := range joined.Unwrap() {
			if cause == nil {
				continue
			}
			hasCause = true
			if !retryableEthRpcError(cause, transportOrigin) {
				return false
			}
		}
		return hasCause
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return retryableEthRpcError(wrapped.Unwrap(), transportOrigin)
	}
	if networkErr, ok := err.(net.Error); ok {
		return networkErr.Timeout() || networkErr.Temporary()
	}
	return false
}
