// Each progress sample owns one finite GET retry budget. Independent roles do
// not share waits, transport queues, response bodies or failure lifetimes.
package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"syscall"
	"time"
)

const defaultMonitorProgressReadBudget = 300 * time.Second
const monitorProgressAttemptBudget = 60 * time.Second

type monitorProgressReadAttempt struct {
	retryable bool
	header    http.Header
}

// Clock/wait hooks observe the real retry boundary; they cannot supply source
// records, protocol validation, identity or admission outcomes.
type monitorProgressReadClock struct {
	now  func() time.Time
	wait func(context.Context, time.Duration) error
}

func monitorProgressRetryStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func monitorProgressRetryTransport(err error) bool {
	remaining := 128
	return monitorProgressRetryCause(err, 0, &remaining)
}

// Complete physical cause graphs retain one finite inspection allowance.
// Foreign Is/As methods never grant authority; local custody stays terminal.
func monitorProgressRetryCause(err error, depth int, remaining *int) bool {
	*remaining--
	if err == nil || *remaining < 0 || depth > 32 {
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
	switch err {
	case context.Canceled:
		return false
	case context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF, syscall.EIO,
		syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE, syscall.ETIMEDOUT,
		syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ENETDOWN,
		syscall.ENETRESET, syscall.ECONNABORTED:
		return true
	}
	// Joined permanent causes must not borrow a sibling's timeout. Inspect
	// owned wrappers before classifying a leaf as a transient transport error.
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 || len(causes) > *remaining {
			return false
		}
		for _, cause := range causes {
			if !monitorProgressRetryCause(cause, depth+1, remaining) {
				return false
			}
		}
		return true
	}
	if dns, ok := err.(*net.DNSError); ok {
		if dns.IsNotFound {
			return false
		}
		if cause := dns.Unwrap(); cause != nil {
			return monitorProgressRetryCause(cause, depth+1, remaining)
		}
		return dns.IsTimeout || dns.IsTemporary
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return monitorProgressRetryCause(wrapped.Unwrap(), depth+1, remaining)
	}
	network, ok := err.(net.Error)
	return ok && network.Timeout()
}

// An observed body/close fault replaces the status-only retry permission.
// Explicit I/O-close failure remains recoverable; a joined permanent cause
// cannot borrow that permission from another error or a transient HTTP status.
func (self *monitorProgressReadAttempt) observeErrors(causes ...error) {
	present := false
	for _, cause := range causes {
		if cause == nil {
			continue
		}
		present = true
		if !monitorProgressRetryTransport(cause) {
			self.retryable = false
			return
		}
	}
	if present {
		self.retryable = true
	}
}

// Every body is already closed by read before a wait starts. Complete hard
// observations dominate a simultaneous timeout/close error and are not retried.
func readMonitorProgress[T any](ctx context.Context, budget time.Duration, clock monitorProgressReadClock, read func(context.Context, *monitorProgressReadAttempt) (*T, string)) (*T, string) {
	if ctx == nil || ctx.Err() != nil {
		return nil, "unavailable"
	}
	if budget < 60*time.Second || budget > 900*time.Second {
		return nil, "invalid"
	}
	if clock.now == nil {
		clock.now = time.Now
	}
	if clock.wait == nil {
		clock.wait = waitRpcReadRetry
	}
	operation, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	deadline, _ := operation.Deadline()
	if clockDeadline := clock.now().Add(budget); clockDeadline.Before(deadline) {
		deadline = clockDeadline
	}
	backoff := 500 * time.Millisecond
	for operation.Err() == nil && clock.now().Before(deadline) {
		attempt := monitorProgressReadAttempt{}
		value, code := read(operation, &attempt)
		if code != "unavailable" || !attempt.retryable {
			return value, code
		}
		if operation.Err() != nil || !clock.now().Before(deadline) {
			break
		}
		delay := rpcReadRetryDelay(attempt.header, nil, backoff, clock.now(), deadline)
		if err := clock.wait(operation, delay); err != nil {
			break
		}
		backoff = min(2*backoff, 10*time.Second)
	}
	return nil, "unavailable"
}
