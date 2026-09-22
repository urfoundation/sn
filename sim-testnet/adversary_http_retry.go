// Read-only adversarial API observations share a finite retry budget with the
// original sample. Every attempt still consumes the configured request gate.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	adversaryGetMaximumAttempts = 3
	adversaryGetRetryDelay      = 250 * time.Millisecond
	adversaryGetDefaultTimeout  = 30 * time.Second
)

// Only these statuses denote an ephemeral API failure. Response parsing and
// signature, content-address and deployment checks stay with the caller.
type adversaryGetStatusError struct{ status int }

// Preserve the exact terminal status in the sample's error evidence.
func (self *adversaryGetStatusError) Error() string {
	return fmt.Sprintf("adversarial GET transient HTTP %d", self.status)
}

// A successful read with malformed evidence remains hard during a scheduled
// outage, including when it passes through an actor's generic error handler.
type adversaryReadIntegrityError struct{ cause error }

// Keep the semantic failure visible in the actor evidence.
func (self *adversaryReadIntegrityError) Error() string { return self.cause.Error() }

// Preserve the precise validation cause for diagnostics and joined errors.
func (self *adversaryReadIntegrityError) Unwrap() error { return self.cause }

// One logical GET reports every admitted attempt, including recovered failures.
type adversaryGetResult struct {
	Status            int
	Body              []byte
	Requests          uint64
	TransientFailures uint64
	Err               error
}

// Per-sample accounting is local to its owner, never shared across actors.
type adversaryGetAccounting struct {
	requests          uint64
	retries           uint64
	transientFailures uint64
}

// Accumulate history pagination and artifact reads without hiding retries.
func (self *adversaryGetAccounting) observe(result adversaryGetResult) {
	self.requests += result.Requests
	if result.Requests > 1 {
		self.retries += result.Requests - 1
	}
	self.transientFailures += result.TransientFailures
}

// Numeric retry evidence survives the campaign's metric projection.
func (self adversaryGetAccounting) metrics() map[string]uint64 {
	return map[string]uint64{
		"http_attempts": self.requests, "http_retries": self.retries,
		"http_transient_failures": self.transientFailures,
	}
}

// A scheduled outage explains transport absence, never malformed content or
// a permanent semantic status from an otherwise responding operator.
func adversaryGetUnavailable(result adversaryGetResult) bool {
	_, status := result.Err.(*adversaryGetStatusError)
	return status || scenarioSnapshotTransportError(result.Err, true)
}

// Reserve time for remaining attempts inside the existing sample deadline;
// using the full deadline on the first request would make retry unreachable.
func adversaryGetAttemptTimeout(configured, remaining time.Duration, attempt int) time.Duration {
	if configured <= 0 {
		configured = adversaryGetDefaultTimeout
	}
	if remaining > 0 {
		configured = min(configured, remaining/time.Duration(adversaryGetMaximumAttempts-attempt))
	}
	return max(time.Nanosecond, configured)
}

// Retry only idempotent reads and typed transport failures. Parent cancellation
// never starts another attempt, and decoding errors never reach this boundary.
func (self *adversaryHTTP) get(ctx context.Context, endpoint, sourceIp string, limit int64) adversaryGetResult {
	result := adversaryGetResult{}
	if self == nil || self.gate == nil || limit < 1 {
		result.Err = errors.New("adversarial GET has an invalid request owner or bound")
		return result
	}
	wait := self.retryWait
	if wait == nil {
		wait = waitAdversaryDelay
	}
	for attempt := 0; attempt < adversaryGetMaximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			result.Err = err
			return result
		}
		if attempt != 0 {
			if err := wait(ctx, adversaryGetRetryDelay*time.Duration(attempt)); err != nil {
				result.Err = err
				return result
			}
		}
		if err := self.gate.WaitSlots(ctx, 1); err != nil {
			result.Err = err
			return result
		}
		remaining := time.Duration(0)
		if deadline, ok := ctx.Deadline(); ok {
			remaining = time.Until(deadline)
			if remaining <= 0 {
				result.Err = context.DeadlineExceeded
				return result
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, adversaryGetAttemptTimeout(self.timeout, remaining, attempt))
		result.Requests++
		result.Status, result.Body, result.Err = self.doReserved(attemptCtx, http.MethodGet, endpoint, sourceIp, nil, limit)
		cancel()
		transient := scenarioSnapshotTransportError(result.Err, true)
		if result.Err == nil {
			switch result.Status {
			case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				result.Err = &adversaryGetStatusError{status: result.Status}
				transient = true
			}
		}
		if !transient {
			return result
		}
		result.TransientFailures++
	}
	return result
}
