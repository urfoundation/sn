// Artifact availability preserves all observed transport causes. Incomplete
// error graphs and joined hard causes cannot establish a retryable absence.
package validator

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"syscall"
)

func artifactObservationPendingCause(err error) bool {
	nodes := 0
	var inspect func(error, int, bool) bool
	inspect = func(cause error, depth int, transport bool) bool {
		nodes++
		if cause == nil || depth > 32 || nodes > 128 {
			return false
		}
		value := reflect.ValueOf(cause)
		switch value.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			if value.IsNil() {
				return false
			}
		}
		if many, ok := cause.(interface{ Unwrap() []error }); ok {
			children := many.Unwrap()
			if len(children) == 0 || len(children) > 128-nodes {
				return false
			}
			for _, child := range children {
				if !inspect(child, depth+1, transport) {
					return false
				}
			}
			return true
		}
		switch observed := cause.(type) {
		case *url.Error:
			return inspect(observed.Err, depth+1, true)
		case *net.OpError:
			return inspect(observed.Err, depth+1, true)
		case *releaseHttpGetStatusError:
			return observed.status == http.StatusNotFound || observed.status == http.StatusRequestTimeout || observed.status == http.StatusTooEarly || observed.status == http.StatusTooManyRequests || observed.status >= 500 && observed.status <= 599
		}
		if single, ok := cause.(interface{ Unwrap() error }); ok {
			return inspect(single.Unwrap(), depth+1, transport)
		}
		switch cause {
		case context.DeadlineExceeded, net.ErrClosed, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED, syscall.ENETDOWN, syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.EPIPE, syscall.ETIMEDOUT:
			return true
		case io.EOF, io.ErrUnexpectedEOF:
			return transport
		case context.Canceled:
			return false
		}
		if network, ok := cause.(net.Error); ok {
			return network.Timeout() || network.Temporary()
		}
		return false
	}
	return inspect(err, 0, false)
}
