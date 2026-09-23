// Authenticated observations are read operations. An ambiguous transport result
// can be retried with its original nonce, while only a complete independently
// verified capture enters the durable decision slot. Quota remains charged.
package validator

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"
)

const clientKeyObservationRetryDelay = 250 * time.Millisecond

// Only explicit transient service statuses authorize the second reservation.
// Authentication, quota, input admission and semantic failures remain hard.
type clientKeyObservationHttpStatusError struct {
	status    int
	operation string
}

// Keep the failing boundary visible without destroying typed retry ownership.
func (self *clientKeyObservationHttpStatusError) Error() string {
	return fmt.Sprintf("client-key %s returned HTTP %d", self.operation, self.status)
}

// Every joined leaf must be transient. A timeout cannot conceal a malformed
// response, a failed file write, a changed signature or a lost parent context.
func retryableClientKeyObservationHttpError(failure error) bool {
	if failure == nil || failure == context.Canceled {
		return false
	}
	if failure == context.DeadlineExceeded || failure == io.EOF || failure == io.ErrUnexpectedEOF || failure == syscall.ECONNRESET || failure == syscall.ECONNREFUSED || failure == syscall.EPIPE {
		return true
	}
	switch failure := failure.(type) {
	case *clientKeyObservationHttpStatusError:
		return failure.status == http.StatusRequestTimeout || failure.status == http.StatusBadGateway || failure.status == http.StatusServiceUnavailable || failure.status == http.StatusGatewayTimeout
	case interface{ Unwrap() []error }:
		causes := failure.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !retryableClientKeyObservationHttpError(cause) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return retryableClientKeyObservationHttpError(failure.Unwrap())
	case net.Error:
		return failure.Timeout() || failure.Temporary()
	default:
		return false
	}
}
