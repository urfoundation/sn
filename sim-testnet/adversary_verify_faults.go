// Verify faults can explain typed request unavailability, never successful
// responses that violate source, signature, assignment, or proof invariants.
package main

import (
	"errors"
	"fmt"
	"net/http"
)

// Only request boundaries mint this marker after checking the transport/status.
type adversaryVerifyUnavailableError struct{ cause error }

// Preserve the request branch and precise status/cause for chronology.
func (self *adversaryVerifyUnavailableError) Error() string { return self.cause.Error() }

// Preserve cancellation and transport causes through contextual wrappers.
func (self *adversaryVerifyUnavailableError) Unwrap() error { return self.cause }

// Semantic statuses and mixed integrity/transport causes remain hard even when
// another request in the same sample encountered a genuine outage.
func adversaryVerifyHttpFailure(label string, status int, cause error) error {
	failure := fmt.Errorf("%s status=%d", label, status)
	if cause != nil {
		failure = fmt.Errorf("%s status=%d: %w", label, status, cause)
	}
	var integrity *adversaryReadIntegrityError
	if errors.As(cause, &integrity) {
		return failure
	}
	transientStatus := false
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		transientStatus = true
	}
	if status != 0 && status/100 != 2 && !transientStatus {
		return failure
	}
	if (cause == nil && transientStatus) || adversaryGetUnavailable(adversaryGetResult{Err: cause}) {
		return &adversaryVerifyUnavailableError{cause: failure}
	}
	return failure
}

// Attribution requires every joined cause to originate at an unavailable
// request boundary. Matching error text or a nested timeout is insufficient.
func adversaryVerifyFaultUnavailable(err error) bool {
	if err == nil {
		return false
	}
	switch cause := err.(type) {
	case *adversaryReadIntegrityError:
		return false
	case *adversaryVerifyUnavailableError:
		return true
	case interface{ Unwrap() []error }:
		causes := cause.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, nested := range causes {
			if !adversaryVerifyFaultUnavailable(nested) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return adversaryVerifyFaultUnavailable(cause.Unwrap())
	default:
		return false
	}
}
