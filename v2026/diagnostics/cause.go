// Diagnostic cause inspection reads only concrete scalar identities and known
// standard-library wrapper fields. It is never a retry or custody predicate.
package diagnostics

import (
	"context"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"syscall"
)

// Unknown includes opaque joins and arbitrary wrappers. It does not mean a
// successful operation, a transient error, or permission to repeat an action.
type Cause uint8

const (
	CauseUnknown Cause = iota
	CauseNone
	CauseCanceled
	CauseTimeout
	CauseTransport
	CausePermission
	CauseUnavailable
)

// No Error, String, Is, As, Timeout, Temporary or Unwrap method is invoked.
// Known wrapper fields have a fixed traversal bound, including cycles; a
// foreign or opaque node stops inspection instead of lending a nested verdict.
func ClassifyCause(err error) Cause {
	if err == nil {
		return CauseNone
	}
	for remaining := 32; remaining > 0 && err != nil; remaining-- {
		cause, next, follow := classifyConcreteCause(err)
		if !follow {
			return cause
		}
		err = next
	}
	return CauseUnknown
}

// One inspection step: a sentinel's cause, a known standard-library wrapper
// field to follow next, or unknown for every other value.
func classifyConcreteCause(err error) (cause Cause, next error, follow bool) {
	switch err {
	case context.Canceled:
		return CauseCanceled, nil, false
	case context.DeadlineExceeded, os.ErrDeadlineExceeded, syscall.ETIMEDOUT:
		return CauseTimeout, nil, false
	case fs.ErrPermission, syscall.EACCES, syscall.EPERM:
		return CausePermission, nil, false
	case fs.ErrNotExist, syscall.ENOSPC, syscall.ENOENT:
		return CauseUnavailable, nil, false
	case io.EOF, io.ErrUnexpectedEOF, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE, syscall.ENETUNREACH, syscall.EHOSTUNREACH:
		return CauseTransport, nil, false
	}
	switch value := err.(type) {
	case *fs.PathError:
		if value == nil {
			return CauseUnknown, nil, false
		}
		return CauseUnknown, value.Err, true
	case *os.LinkError:
		if value == nil {
			return CauseUnknown, nil, false
		}
		return CauseUnknown, value.Err, true
	case *os.SyscallError:
		if value == nil {
			return CauseUnknown, nil, false
		}
		return CauseUnknown, value.Err, true
	case *url.Error:
		if value == nil {
			return CauseUnknown, nil, false
		}
		return CauseUnknown, value.Err, true
	case *net.OpError:
		if value == nil {
			return CauseUnknown, nil, false
		}
		return CauseUnknown, value.Err, true
	case *net.DNSError:
		if value == nil {
			return CauseUnknown, nil, false
		}
		if value.IsTimeout {
			return CauseTimeout, nil, false
		}
		return CauseTransport, nil, false
	}
	return CauseUnknown, nil, false
}
