// Production waits identify the read phase that owns an uncertain observation.
// They never grant signing, skip retained work, or hide a second hard cause.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

const (
	productionSteeringReadAttemptTimeout = 60 * time.Second
	productionSteeringReadTimeout        = 300 * time.Second
)

// This private closed phase is assigned at the actual read boundary, never
// inferred from error text, current artifact fields or monitor health.
type productionSteeringReadPhase uint8

const (
	productionReadIntent productionSteeringReadPhase = iota + 1
	productionReadReceipt
	productionReadPreparation
	productionReadApplication
)

// Pure unavailable/transport outcomes retain their original cause. Local file
// and lifecycle errors stay hard even when joined to a missing RPC response.
func retryableProductionSteeringRead(err error) bool {
	remaining := 512
	return retryableProductionSteeringReadBounded(err, false, 0, &remaining)
}

// Native markers retain their complete private verdict. Every other wrapper
// and joined reader consumes this same allowance before a leaf can authorize
// retry, so cycles, nil branches and timeout wrappers cannot hide hard causes.
func retryableProductionSteeringReadBounded(err error, transportOrigin bool, depth int, remaining *int) bool {
	err = releaseObservedValue(err)
	if err == nil || depth > 32 || *remaining <= 0 {
		return false
	}
	*remaining--
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if value.IsNil() {
			return false
		}
	}
	if crv4.IsSubstrateReadTransportCause(err) {
		return crv4.RetryableSubstrateReadTransportError(err)
	}
	if _, ok := err.(*crv4.ReceiptEvidenceUnavailableError); ok {
		return true
	}
	switch cause := err.(type) {
	case *os.PathError, *os.LinkError, *TrailFatalError, *releaseObservedHard, *releaseObservedRefusal:
		return false
	case *releaseObservedNativeRead:
		return cause.retryable
	case *releaseObservedNetworkRead:
		return cause.retryable
	case *url.Error:
		return retryableProductionSteeringReadBounded(cause.Err, true, depth+1, remaining)
	case *net.OpError:
		return retryableProductionSteeringReadBounded(cause.Err, true, depth+1, remaining)
	case *net.DNSError:
		if cause.IsNotFound {
			return false
		}
		if cause.UnwrapErr != nil {
			return retryableProductionSteeringReadBounded(cause.UnwrapErr, true, depth+1, remaining)
		}
		return cause.IsTimeout || cause.IsTemporary
	case *attemptStreamHttpReadError:
		return retryableProductionSteeringReadBounded(cause.cause, true, depth+1, remaining)
	case *chainRpcMissingResponseError:
		return retryableProductionSteeringReadBounded(cause.cause, true, depth+1, remaining)
	case *attemptStreamHTTPIncompleteError:
		return retryableProductionSteeringReadBounded(cause.cause, true, depth+1, remaining)
	case syscall.Errno:
		// Standard errno implements Is; its concrete transport taxonomy is
		// established before the foreign matcher guard below.
		return RetryableEvidenceTransportError(cause)
	case interface{ Is(error) bool }, interface{ As(any) bool }:
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 || len(causes) > 128 || len(causes) > *remaining {
			return false
		}
		for _, cause := range causes {
			if !retryableProductionSteeringReadBounded(cause, transportOrigin, depth+1, remaining) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return retryableProductionSteeringReadBounded(wrapped.Unwrap(), transportOrigin, depth+1, remaining)
	}
	// Only a leaf reaches generic evidence classification. No presence search
	// can return an incomplete negative and expose a rejected native subtree.
	leafRemaining := 1
	retryable, transient := classifyReleaseSnapshotRetryBounded(err, false, false, transportOrigin, 0, &leafRemaining)
	return retryable && transient
}

// The real owner has exhausted its bounded read budget. It can poll again;
// fresh preparation still has to authenticate all retained intent state first.
type productionSteeringReadWait struct {
	phase         productionSteeringReadPhase
	nativeEpoch   uint64
	epochKnown    bool
	extrinsicHash string
	cause         error
}

func (self *productionSteeringReadWait) Error() string {
	return fmt.Sprintf("production steering read phase %d remains unavailable; retained transaction %s is unchanged: %v", self.phase, self.extrinsicHash, self.cause)
}

func (self *productionSteeringReadWait) Unwrap() error { return self.cause }

// An operation that may already have published retained work is never repeated
// here. A pure remote read failure defers to the next normal custody read;
// filesystem/custody/contradiction leaves still force an explicit hard result.
func (self *ReleaseSteerer) productionRetainedReadFailure(ctx context.Context, phase productionSteeringReadPhase, intent *SteeringIntent, err error) error {
	err = observeReleaseError(err)
	if !isOwnerRecycleProductionConfig(self.cfg) || err == nil || errors.Is(ctx.Err(), context.Canceled) || !retryableProductionSteeringRead(err) {
		return err
	}
	if retained := releaseErrorMarker[*productionSteeringReadWait](err); retained != nil {
		return err
	}
	result := &productionSteeringReadWait{phase: phase, cause: err}
	if intent != nil {
		result.nativeEpoch, result.epochKnown = intent.SubnetEpoch, true
		if intent.Prepared != nil {
			result.extrinsicHash = intent.Prepared.ExtrinsicHash
		}
	}
	return result
}

// Read-only callers close their real file/HTTP ownership before retry. The
// finite shared deadline enforces expiry before cancellation is published.
func (self *ReleaseSteerer) productionRead(ctx context.Context, phase productionSteeringReadPhase, intent *SteeringIntent, read func(context.Context) error) error {
	if !isOwnerRecycleProductionConfig(self.cfg) {
		return read(ctx)
	}
	if ctx == nil || read == nil || phase < productionReadIntent || phase > productionReadApplication {
		return errors.New("production steering read owner is incomplete")
	}
	withTimeout, wait := self.productionReadHooks.withTimeout, self.productionReadHooks.wait
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	if wait == nil {
		wait = waitReleaseSnapshotRetry
	}
	operation := ctx
	if evidenceReadContextError(ctx) == nil {
		var cancel context.CancelFunc
		operation, cancel = withTimeout(ctx, productionSteeringReadTimeout)
		defer cancel()
	}
	operation = withRuntimeFinalityOwner(operation)
	var lastErr error
	for {
		if err := errors.Join(evidenceReadContextError(ctx), evidenceReadContextError(operation)); err != nil {
			lastErr = errors.Join(lastErr, err)
			break
		}
		attempt, attemptCancel := withTimeout(operation, productionSteeringReadAttemptTimeout)
		if err := errors.Join(evidenceReadContextError(ctx), evidenceReadContextError(operation), evidenceReadContextError(attempt)); err != nil {
			lastErr = errors.Join(lastErr, err)
		} else {
			lastErr = observeReleaseError(errors.Join(read(attempt), evidenceReadContextError(attempt)))
		}
		attemptCancel()
		ownerErr := errors.Join(evidenceReadContextError(ctx), evidenceReadContextError(operation))
		lastErr = errors.Join(lastErr, ownerErr)
		if lastErr == nil {
			return nil
		}
		if !retryableProductionSteeringRead(lastErr) || ownerErr != nil {
			break
		}
		if err := wait(operation, releaseSnapshotStartupRetryDelay); err != nil {
			lastErr = errors.Join(lastErr, err, evidenceReadContextError(ctx), evidenceReadContextError(operation))
			break
		}
	}
	if !retryableProductionSteeringRead(lastErr) || errors.Is(ctx.Err(), context.Canceled) {
		return errors.Join(lastErr, evidenceReadContextError(ctx))
	}
	result := &productionSteeringReadWait{phase: phase, cause: lastErr}
	if intent != nil {
		result.nativeEpoch, result.epochKnown = intent.SubnetEpoch, true
		if intent.Prepared != nil {
			result.extrinsicHash = intent.Prepared.ExtrinsicHash
		}
	}
	return result
}
