// A supervisor may retry an unavailable startup after every partial owner has
// joined. Unknown or contradictory authority and uncertain publication stay held.
package main

import (
	"context"
	"os"
	"reflect"
	"syscall"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// This classifier never invokes custom Is/As and cannot accept an empty, cyclic,
// typed-nil or oversized graph as retry authority. Every actual leaf must be a
// known observation/resource error; a joined hard cause therefore dominates.
func economicConservationStartupPending(err error) bool {
	nodes := 0
	var visit func(error, int) bool
	visit = func(cause error, depth int) bool {
		nodes++
		if cause == nil || depth >= 32 || nodes > 128 {
			return false
		}
		value := reflect.ValueOf(cause)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return false
			}
		}
		if _, refused := cause.(*monitorAdmissionCleanupError); refused {
			return false
		}
		if _, refused := cause.(*monitorOutputOwnershipError); refused {
			return false
		}
		if cause == context.DeadlineExceeded || cause == durablevolume.ErrBusy || cause == durablevolume.ErrUnavailable || cause == os.ErrClosed {
			return true
		}
		if errno, ok := cause.(syscall.Errno); ok {
			switch errno {
			case syscall.EAGAIN, syscall.EBUSY, syscall.EIO, syscall.EMFILE, syscall.ENFILE, syscall.ENOSPC, syscall.EDQUOT, syscall.EINTR, syscall.ETIMEDOUT, syscall.EBADF:
				return true
			}
			return false
		}
		if many, ok := cause.(interface{ Unwrap() []error }); ok {
			children := many.Unwrap()
			if len(children) == 0 || len(children) > 128-nodes {
				return false
			}
			for _, child := range children {
				if !visit(child, depth+1) {
					return false
				}
			}
			return true
		}
		if one, ok := cause.(interface{ Unwrap() error }); ok {
			return visit(one.Unwrap(), depth+1)
		}
		return false
	}
	return visit(err, 0)
}
