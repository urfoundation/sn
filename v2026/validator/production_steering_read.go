// Production waits identify the read phase that owns an uncertain observation.
// They never grant signing, skip retained work, or hide a second hard cause.
package validator

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	if err == nil {
		return false
	}
	if crv4.IsSubstrateReadTransportCause(err) {
		return crv4.RetryableSubstrateReadTransportError(err)
	}
	if _, ok := err.(*crv4.ReceiptEvidenceUnavailableError); ok {
		return true
	}
	switch err.(type) {
	case *os.PathError, *TrailFatalError:
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !retryableProductionSteeringRead(cause) {
				return false
			}
		}
		return true
	}
	// A wrapper around a native origin must reach its opaque verdict before
	// generic transport classification. Independent joined readers above can
	// each retain their own actual native/EVM/unavailable response cause.
	if !crv4.HasSubstrateReadTransportCause(err) && RetryableEvidenceTransportError(err) {
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return retryableProductionSteeringRead(wrapped.Unwrap())
	}
	return false
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
	if !isOwnerRecycleProductionConfig(self.cfg) || err == nil || errors.Is(ctx.Err(), context.Canceled) || !retryableProductionSteeringRead(err) {
		return err
	}
	var retained *productionSteeringReadWait
	if errors.As(err, &retained) {
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
// shared deadline is finite even without an enclosing service deadline.
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
	operation, cancel := withTimeout(ctx, productionSteeringReadTimeout)
	defer cancel()
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return errors.Join(lastErr, err)
			}
			lastErr = errors.Join(lastErr, err)
			break
		}
		if err := operation.Err(); err != nil {
			lastErr = errors.Join(lastErr, err)
			break
		}
		attempt, attemptCancel := withTimeout(operation, productionSteeringReadAttemptTimeout)
		lastErr = errors.Join(read(attempt), attempt.Err())
		attemptCancel()
		if lastErr == nil {
			return ctx.Err()
		}
		if !retryableProductionSteeringRead(lastErr) || errors.Is(ctx.Err(), context.Canceled) {
			return errors.Join(lastErr, ctx.Err())
		}
		if err := wait(operation, releaseSnapshotStartupRetryDelay); err != nil {
			lastErr = errors.Join(lastErr, err, ctx.Err())
			break
		}
	}
	if !retryableProductionSteeringRead(lastErr) || errors.Is(ctx.Err(), context.Canceled) {
		return lastErr
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
