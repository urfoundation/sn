// DNS flags describe a terminal resolver leaf. An explicit child retains its
// own transport or hard meaning inside both actual progress GET owners.
package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
)

// An unflagged concrete DNS wrapper cannot refuse its observed transient child.
func TestMonitorProgressHttpRecoversDnsChildrenWithoutFlags(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, child := range []error{context.DeadlineExceeded, syscall.ECONNRESET, errors.Join(io.ErrUnexpectedEOF, syscall.ECONNRESET)} {
			cause := &net.DNSError{Err: "synthetic resolver child", Name: "progress.example", UnwrapErr: child}
			got, code, calls, closed, waits := monitorProgressCauseRead(t, kind, cause, true)
			if !got || code != "ok" || calls != 2 || closed != 2 || waits != 1 {
				t.Fatalf("%s explicit DNS child lost recovery: got=%t code=%s calls=%d closed=%d waits=%d", kind, got, code, calls, closed, waits)
			}
		}
	}
}

// Notfound is a concrete permanent result, while absent flags alone establish
// no transport authority for an absent child. Actual hard children stay hard.
func TestMonitorProgressHttpDnsChildrenRetainHardPriority(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, cause := range []error{
			&net.DNSError{IsNotFound: true, UnwrapErr: context.DeadlineExceeded},
			&net.DNSError{IsNotFound: true, IsTimeout: true, UnwrapErr: syscall.ECONNRESET},
			&net.DNSError{Err: "synthetic terminal resolver refusal", Name: "progress.example"},
			&net.DNSError{UnwrapErr: &os.PathError{Op: "read", Path: "synthetic-dns-custody", Err: context.DeadlineExceeded}},
			&net.DNSError{IsTimeout: true, UnwrapErr: &os.LinkError{Op: "rename", Old: "synthetic-old", New: "synthetic-new", Err: syscall.EIO}},
			&net.DNSError{UnwrapErr: errors.Join(syscall.ECONNRESET, context.Canceled)},
			&net.DNSError{IsTemporary: true, UnwrapErr: errors.New("synthetic child integrity failure")},
			&net.DNSError{UnwrapErr: (*net.DNSError)(nil)},
		} {
			got, code, calls, closed, waits := monitorProgressCauseRead(t, kind, cause, false)
			if got || code != "unavailable" || calls != 1 || closed != 1 || waits != 0 {
				t.Fatalf("%s DNS child lost hard priority: got=%t code=%s calls=%d closed=%d waits=%d", kind, got, code, calls, closed, waits)
			}
		}
	}
}

// Complete DNS branches share the same total work and depth limits. Two local
// branches fitting the node limit do not create a second inspection allowance.
func TestMonitorProgressHttpDnsChildrenKeepOriginalGraphBudget(t *testing.T) {
	var deep error = syscall.ECONNRESET
	for range 40 {
		deep = &net.DNSError{Err: "synthetic nested resolver child", Name: "nested.example", UnwrapErr: deep}
	}
	leaves := make([]error, 64)
	for index := range leaves {
		leaves[index] = syscall.ECONNRESET
	}
	branch := &net.DNSError{UnwrapErr: &monitorProgressJoinedTestError{causes: leaves}}
	wide := &monitorProgressJoinedTestError{causes: []error{branch, branch}}
	for _, kind := range []string{"provider", "claim"} {
		for _, child := range []error{deep, wide} {
			cause := &net.DNSError{UnwrapErr: child}
			got, code, calls, closed, waits := monitorProgressCauseRead(t, kind, cause, false)
			if got || code != "unavailable" || calls != 1 || closed != 1 || waits != 0 {
				t.Fatalf("%s DNS child renewed its graph allowance: got=%t code=%s calls=%d closed=%d waits=%d", kind, got, code, calls, closed, waits)
			}
		}
	}
}
