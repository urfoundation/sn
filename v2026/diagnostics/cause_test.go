// Cause inspection must finish without entering any foreign error method.
package diagnostics

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
)

// A method entry is an explicit regression barrier, not a timing inference.
type diagnosticCauseProbe struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Uint64
}

// The fixture releases and joins even when the old classifier enters here.
func (self *diagnosticCauseProbe) wait() {
	self.calls.Add(1)
	select {
	case self.entered <- struct{}{}:
	default:
	}
	<-self.release
}

// All optional interfaces are deliberately unsuitable for diagnostics.
func (self *diagnosticCauseProbe) Error() string { self.wait(); return "synthetic error" }

// String conversion must not be used as a fallback either.
func (self *diagnosticCauseProbe) String() string { self.wait(); return "synthetic string" }

// Generic identity traversal can invoke this instead of inspecting a cause.
func (self *diagnosticCauseProbe) Is(error) bool { self.wait(); return false }

// Generic type traversal can invoke this instead of inspecting a cause.
func (self *diagnosticCauseProbe) As(any) bool { self.wait(); return false }

// A count bound cannot constrain time spent inside this arbitrary method.
func (self *diagnosticCauseProbe) Unwrap() error { self.wait(); return context.DeadlineExceeded }

// A net.Error method is arbitrary application code as well.
func (self *diagnosticCauseProbe) Timeout() bool { self.wait(); return true }

// Deprecated net.Error methods receive the same no-callback treatment.
func (self *diagnosticCauseProbe) Temporary() bool { self.wait(); return true }

// Joined wrappers have a different signature but the same arbitrary-code risk.
type diagnosticJoinedCauseProbe struct{ probe *diagnosticCauseProbe }

// Formatting is counted on the same owned barrier.
func (self *diagnosticJoinedCauseProbe) Error() string { return self.probe.Error() }

// No slice supplied by a foreign wrapper may be requested or enumerated.
func (self *diagnosticJoinedCauseProbe) Unwrap() []error {
	self.probe.wait()
	return []error{context.DeadlineExceeded}
}

// Unknown non-comparable error values must not panic during sentinel matching.
type diagnosticSliceCause []byte

// Any invocation would violate the same closed observation boundary.
func (diagnosticSliceCause) Error() string { panic("slice diagnostic cause was formatted") }

// The actual classifier returns before a foreign method can hold its caller.
func TestDiagnosticCauseNeverInvokesForeignMethods(t *testing.T) {
	for _, variant := range []string{"single", "multiple", "standard-wrapper", "opaque-join"} {
		probe := &diagnosticCauseProbe{entered: make(chan struct{}, 1), release: make(chan struct{})}
		var cause error = probe
		switch variant {
		case "multiple":
			cause = &diagnosticJoinedCauseProbe{probe: probe}
		case "standard-wrapper":
			cause = &url.Error{Op: "Get", URL: "https://synthetic.example", Err: probe}
		case "opaque-join":
			cause = errors.Join(context.DeadlineExceeded, probe)
		}
		finished := make(chan Cause, 1)
		go func() { finished <- ClassifyCause(cause) }()
		var result Cause
		select {
		case result = <-finished:
			close(probe.release)
		case <-probe.entered:
			close(probe.release)
			result = <-finished
		}
		if probe.calls.Load() != 0 || result != CauseUnknown {
			t.Fatalf("diagnostic cause invoked a foreign method: %s", variant)
		}
	}
}

// Public standard-library fields preserve useful causes without callbacks.
// Nil wrappers, field cycles, opaque joins and excessive depth remain unknown.
func TestDiagnosticCauseConcreteWrappersAndBounds(t *testing.T) {
	for _, candidate := range []struct {
		cause error
		want  Cause
	}{
		{cause: nil, want: CauseNone},
		{cause: context.Canceled, want: CauseCanceled},
		{cause: context.DeadlineExceeded, want: CauseTimeout},
		{cause: os.ErrDeadlineExceeded, want: CauseTimeout},
		{cause: &os.LinkError{Err: syscall.ENOSPC}, want: CauseUnavailable},
		{cause: &fs.PathError{Err: &url.Error{Err: &net.OpError{Err: &os.SyscallError{Err: syscall.ETIMEDOUT}}}}, want: CauseTimeout},
		{cause: &fs.PathError{Err: fs.ErrPermission}, want: CausePermission},
		{cause: &url.Error{Err: io.EOF}, want: CauseTransport},
		{cause: &net.DNSError{IsTimeout: true}, want: CauseTimeout},
		{cause: &net.DNSError{}, want: CauseTransport},
		{cause: errors.Join(context.DeadlineExceeded), want: CauseUnknown},
		{cause: errors.Join(io.EOF, fs.ErrPermission), want: CauseUnknown},
		{cause: (*fs.PathError)(nil), want: CauseUnknown},
		{cause: (*os.LinkError)(nil), want: CauseUnknown},
		{cause: (*os.SyscallError)(nil), want: CauseUnknown},
		{cause: (*url.Error)(nil), want: CauseUnknown},
		{cause: (*net.OpError)(nil), want: CauseUnknown},
		{cause: (*net.DNSError)(nil), want: CauseUnknown},
		{cause: (*diagnosticCauseProbe)(nil), want: CauseUnknown},
		{cause: diagnosticSliceCause{1}, want: CauseUnknown},
		{cause: diagnosticSliceCause(nil), want: CauseUnknown},
		{cause: &fs.PathError{}, want: CauseUnknown},
	} {
		if actual := ClassifyCause(candidate.cause); actual != candidate.want {
			t.Fatalf("concrete diagnostic cause changed: got %d want %d", actual, candidate.want)
		}
	}
	cycle := &fs.PathError{}
	cycle.Err = cycle
	if ClassifyCause(cycle) != CauseUnknown || cycle.Err != cycle {
		t.Fatal("diagnostic cause did not bound an unchanged field cycle")
	}
	for _, depth := range []int{31, 32} {
		var cause error = context.DeadlineExceeded
		for range depth {
			cause = &fs.PathError{Err: cause}
		}
		actual := ClassifyCause(cause)
		if depth == 31 && actual != CauseTimeout || depth == 32 && actual != CauseUnknown {
			t.Fatalf("diagnostic cause depth bound differs at %d", depth)
		}
	}
}
