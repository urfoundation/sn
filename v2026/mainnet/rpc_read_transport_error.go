// Only physical read failures acquire transport recovery. Every cause must
// permit retry within a bounded inspection before another request is sent.
package main

import (
	"context"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"syscall"
)

// Called only for request, body-read and body-close failures, never a decoded
// response or RPC refusal. A hard sibling defeats a transient transport cause.
func rpcReadTransportMayRetry(err error) bool {
	remaining := 128
	var inspect func(error, int) bool
	inspect = func(cause error, depth int) bool {
		remaining--
		if cause == nil || remaining < 0 || depth > 32 {
			return false
		}
		value := reflect.ValueOf(cause)
		switch value.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			if value.IsNil() {
				return false
			}
		}
		if _, local := cause.(*os.PathError); local {
			return false
		}
		switch cause {
		case context.Canceled:
			return false
		case context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF, net.ErrClosed,
			syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED,
			syscall.EPIPE, syscall.ETIMEDOUT, syscall.ENETDOWN,
			syscall.ENETUNREACH, syscall.EHOSTUNREACH:
			return true
		}
		switch physical := cause.(type) {
		case *url.Error:
			return inspect(physical.Err, depth+1)
		case *net.OpError:
			return inspect(physical.Err, depth+1)
		}
		if joined, ok := cause.(interface{ Unwrap() []error }); ok {
			causes := joined.Unwrap()
			if len(causes) == 0 || len(causes) > remaining {
				return false
			}
			for _, child := range causes {
				if !inspect(child, depth+1) {
					return false
				}
			}
			return true
		}
		if wrapped, ok := cause.(interface{ Unwrap() error }); ok {
			return inspect(wrapped.Unwrap(), depth+1)
		}
		if network, ok := cause.(net.Error); ok {
			return network.Timeout() || network.Temporary()
		}
		return false
	}
	return inspect(err, 0)
}
