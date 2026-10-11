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
		switch err {
		case context.Canceled:
			return CauseCanceled
		case context.DeadlineExceeded, os.ErrDeadlineExceeded, syscall.ETIMEDOUT:
			return CauseTimeout
		case fs.ErrPermission, syscall.EACCES, syscall.EPERM:
			return CausePermission
		case fs.ErrNotExist, syscall.ENOSPC, syscall.ENOENT:
			return CauseUnavailable
		case io.EOF, io.ErrUnexpectedEOF, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE, syscall.ENETUNREACH, syscall.EHOSTUNREACH:
			return CauseTransport
		}
		switch value := err.(type) {
		case *fs.PathError:
			if value == nil {
				return CauseUnknown
			}
			err = value.Err
		case *os.LinkError:
			if value == nil {
				return CauseUnknown
			}
			err = value.Err
		case *os.SyscallError:
			if value == nil {
				return CauseUnknown
			}
			err = value.Err
		case *url.Error:
			if value == nil {
				return CauseUnknown
			}
			err = value.Err
		case *net.OpError:
			if value == nil {
				return CauseUnknown
			}
			err = value.Err
		case *net.DNSError:
			if value == nil {
				return CauseUnknown
			}
			if value.IsTimeout {
				return CauseTimeout
			}
			return CauseTransport
		default:
			return CauseUnknown
		}
	}
	return CauseUnknown
}
