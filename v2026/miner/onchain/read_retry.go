// Only idempotent submission reads recover here. Signing, durable preparation
// and transaction transport remain outside this finite, caller-owned budget.
package onchain

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"reflect"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
)

const (
	onchainReadTimeout = 300 * time.Second
	onchainReadDelay   = 2 * time.Second
)

// Instance-only seams observe real reads without replacing transport or
// authority. No exported command option can shorten the production allowance.
// An additional failure can only join a completed failed read's original cause.
type onchainReadRetryHooks struct {
	withTimeout         func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait                func(context.Context, time.Duration) error
	additionalReadError func(error) error
}

// Tests attach a clock to one actual public submission, never process state.
type onchainReadRetryHooksKey struct{}

// One deadline survives every transient attempt. The read callback must join
// its request before returning; no failed or canceled value escapes this owner.
func retryOnchainRead[T any](parent context.Context, read func(context.Context) (T, error)) (T, error) {
	var zero T
	if parent == nil || read == nil {
		return zero, errors.New("onchain read owner is unavailable")
	}
	if err := parent.Err(); err != nil {
		return zero, err
	}
	hooks, _ := parent.Value(onchainReadRetryHooksKey{}).(onchainReadRetryHooks)
	withTimeout := hooks.withTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	ctx, cancel := withTimeout(parent, onchainReadTimeout)
	defer cancel()
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return zero, errors.Join(lastErr, err)
		}
		value, err := read(ctx)
		if ownerErr := ctx.Err(); ownerErr != nil {
			return zero, errors.Join(err, ownerErr)
		}
		if err == nil {
			return value, nil
		}
		if hooks.additionalReadError != nil {
			err = errors.Join(err, hooks.additionalReadError(err))
		}
		lastErr = err
		if !retryableOnchainRead(err, false) {
			return zero, err
		}
		delay := onchainReadDelay + time.Duration(rand.Int64N(int64(onchainReadDelay/2)))
		if err := waitOnchainRead(ctx, hooks, delay); err != nil {
			return zero, errors.Join(lastErr, err)
		}
	}
}

// Cancellation owns both the pacing wait and the following attempt admission.
func waitOnchainRead(ctx context.Context, hooks onchainReadRetryHooks, delay time.Duration) error {
	if hooks.wait != nil {
		return hooks.wait(ctx, delay)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// Every cause must authorize recovery. Unknown leaves, local-file errors and
// incomplete graphs cannot hide behind a simultaneous transport timeout.
func retryableOnchainRead(err error, finality bool) bool {
	remaining := 128
	var visit func(error, int) bool
	visit = func(err error, depth int) bool {
		remaining--
		if err == nil || remaining < 0 || depth > 32 || err == context.Canceled || err == rpc.ErrClientQuit {
			return false
		}
		value := reflect.ValueOf(err)
		switch value.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			if value.IsNil() {
				return false
			}
		}
		switch err.(type) {
		case *os.PathError, *os.LinkError:
			return false
		}
		if err == context.DeadlineExceeded || err == os.ErrDeadlineExceeded || err == net.ErrClosed || err == io.EOF || err == io.ErrUnexpectedEOF || err == syscall.ECONNRESET || err == syscall.ECONNREFUSED || err == syscall.EPIPE || err == syscall.ETIMEDOUT || err == syscall.ENETUNREACH || err == syscall.EHOSTUNREACH {
			return true
		}
		if err == ethereum.NotFound {
			return finality
		}
		switch cause := err.(type) {
		case rpc.HTTPError:
			return cause.StatusCode == http.StatusRequestTimeout || cause.StatusCode == http.StatusTooEarly || cause.StatusCode == http.StatusTooManyRequests || cause.StatusCode >= 500 && cause.StatusCode <= 599
		case *rpc.HTTPError:
			return cause.StatusCode == http.StatusRequestTimeout || cause.StatusCode == http.StatusTooEarly || cause.StatusCode == http.StatusTooManyRequests || cause.StatusCode >= 500 && cause.StatusCode <= 599
		case *net.DNSError:
			if cause.IsNotFound {
				return false
			}
			if underlying := cause.Unwrap(); underlying != nil {
				return visit(underlying, depth+1)
			}
			return cause.IsTimeout || cause.IsTemporary
		case *websocket.CloseError:
			switch cause.Code {
			case websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseInternalServerErr, websocket.CloseServiceRestart, websocket.CloseTryAgainLater:
				return true
			default:
				return false
			}
		case rpc.Error:
			// Only geth's decoded remote error supplies finality availability.
			// A preflight refusal or foreign ErrorCode callback is never retried.
			typeOf := reflect.TypeOf(err)
			if typeOf.Kind() == reflect.Pointer {
				typeOf = typeOf.Elem()
			}
			if !finality || typeOf.PkgPath() != "github.com/ethereum/go-ethereum/rpc" || typeOf.Name() != "jsonError" {
				return false
			}
			code := cause.ErrorCode()
			return code == -32603 || code >= -32099 && code <= -32000
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			causes := joined.Unwrap()
			if len(causes) == 0 || len(causes) > remaining {
				return false
			}
			for _, cause := range causes {
				if !visit(cause, depth+1) {
					return false
				}
			}
			return true
		}
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			return visit(wrapped.Unwrap(), depth+1)
		}
		return false
	}
	return visit(err, 0)
}
