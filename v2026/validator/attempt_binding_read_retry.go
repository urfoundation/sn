// Assignment capture classifies the original read causes within one finite
// traversal. Cancellation and retry share the original hard-cause rules.
package validator

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"syscall"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Each classification owns its allowance; branches cannot reset it or use
// custom matching to substitute transport authority for their original cause.
type attemptReadCauseBudget struct {
	remaining int
}

// Refuses malformed, excessive and explicitly hard nodes before unwrapping.
// Standard errno has its own concrete taxonomy despite implementing Is.
func (self *attemptReadCauseBudget) admit(err error, depth int) bool {
	if err == nil || depth > 32 || self.remaining <= 0 {
		return false
	}
	self.remaining--
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return false
		}
	}
	switch cause := err.(type) {
	case *os.PathError, *os.LinkError, *TrailFatalError:
		return false
	case *net.DNSError:
		return !cause.IsNotFound
	case syscall.Errno:
		return true
	case interface{ Is(error) bool }, interface{ As(any) bool }:
		return false
	}
	return true
}

// Requires typed binding-read provenance on every branch and refuses any
// cancellation, integrity state, or unknown failure in the original tree.
func retryableAttemptBindingReadError(err error) bool {
	retryable, bindingRead := classifyAttemptBindingReadRetry(err, false)
	return retryable && bindingRead
}

// Owns one shared depth and node allowance for the complete original cause.
func classifyAttemptBindingReadRetry(err error, bindingRead bool) (bool, bool) {
	budget := attemptReadCauseBudget{remaining: 512}
	return classifyAttemptBindingReadRetryBounded(err, bindingRead, 0, &budget)
}

// Preserves typed provenance through ordinary wrappers without promoting an
// untyped sibling. All joined causes must independently authorize retry.
func classifyAttemptBindingReadRetryBounded(err error, bindingRead bool, depth int, budget *attemptReadCauseBudget) (bool, bool) {
	if !budget.admit(err, depth) {
		return false, bindingRead
	}
	if readErr, ok := err.(*attemptBindingReadError); ok {
		return classifyAttemptBindingReadRetryBounded(readErr.cause, true, depth+1, budget)
	}
	// A concrete dns failure may have no underlying error. If it does have
	// one, that original cause still has to pass the complete walk below.
	if dnsErr, ok := err.(*net.DNSError); ok && dnsErr.UnwrapErr == nil {
		return bindingRead && (dnsErr.Timeout() || dnsErr.Temporary()), bindingRead
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 || len(causes) > 128 || len(causes) > budget.remaining {
			return false, bindingRead
		}
		seenBindingRead := bindingRead
		for _, cause := range causes {
			retryable, causeBindingRead := classifyAttemptBindingReadRetryBounded(cause, bindingRead, depth+1, budget)
			seenBindingRead = seenBindingRead || causeBindingRead
			if !retryable {
				return false, seenBindingRead
			}
		}
		return true, seenBindingRead
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return classifyAttemptBindingReadRetryBounded(wrapped.Unwrap(), bindingRead, depth+1, budget)
	}
	if !bindingRead {
		return false, false
	}
	if err == context.DeadlineExceeded || err == io.EOF || err == io.ErrUnexpectedEOF {
		return true, true
	}
	if err == context.Canceled {
		return false, true
	}
	if netErr, ok := err.(net.Error); ok && (netErr.Timeout() || netErr.Temporary()) {
		return true, true
	}
	statusCode := 0
	switch httpErr := err.(type) {
	case gethrpc.HTTPError:
		statusCode = httpErr.StatusCode
	case *gethrpc.HTTPError:
		statusCode = httpErr.StatusCode
	}
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true, true
	}
	switch err {
	case syscall.ECONNABORTED, syscall.ECONNREFUSED, syscall.ECONNRESET,
		syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EPIPE, syscall.ETIMEDOUT:
		return true, true
	}
	return false, true
}

// Only a complete tree of the owner's exact context cause may omit the fatal
// assigned-work result. Local file and lifecycle failures remain hard.
func onlyAttemptContextError(err, want error) bool {
	if want != context.Canceled && want != context.DeadlineExceeded {
		return false
	}
	budget := attemptReadCauseBudget{remaining: 512}
	return onlyAttemptContextErrorBounded(err, want, 0, &budget)
}

// Cancellation examines the same finite original tree as retry, before any
// wrapper can hide a second hard cause or invoke a foreign matching method.
func onlyAttemptContextErrorBounded(err, want error, depth int, budget *attemptReadCauseBudget) bool {
	if !budget.admit(err, depth) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 || len(causes) > 128 || len(causes) > budget.remaining {
			return false
		}
		for _, cause := range causes {
			if !onlyAttemptContextErrorBounded(cause, want, depth+1, budget) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyAttemptContextErrorBounded(wrapped.Unwrap(), want, depth+1, budget)
	}
	return err == want
}
