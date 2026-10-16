// Actual provider and claim GET owners classify physical body-close causes
// before returning samples or retrying. Foreign matching methods grant nothing.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// A real response remains physically owned until this wrapper closes it.
type monitorProgressCauseTestBody struct {
	io.ReadCloser
	closed *atomic.Int32
	cause  error
}

// Retain both the genuine close result and the independently injected cause.
func (self *monitorProgressCauseTestBody) Close() error {
	err := self.ReadCloser.Close()
	self.closed.Add(1)
	return errors.Join(err, self.cause)
}

// Matching callbacks must never manufacture transport identity.
type monitorProgressForeignTestError struct{ cause error }

// Keep diagnostics independent of nested graph behavior.
func (self *monitorProgressForeignTestError) Error() string {
	return "synthetic foreign progress cause"
}

// The bounded classifier may inspect this explicit edge exactly once.
func (self *monitorProgressForeignTestError) Unwrap() error { return self.cause }

// Generic errors.Is traversal is not an admission mechanism.
func (self *monitorProgressForeignTestError) Is(error) bool { panic("foreign Is must not execute") }

// Generic errors.As traversal cannot supply a network-error verdict.
func (self *monitorProgressForeignTestError) As(any) bool { panic("foreign As must not execute") }

// This complete opaque leaf advertises no physical transport origin.
type monitorProgressOpaqueTestError struct{}

// The diagnostic stays fixed even under a deliberately broken control.
func (self *monitorProgressOpaqueTestError) Error() string {
	return "synthetic opaque progress refusal"
}

// The old classifier would consult this foreign method before graph admission.
func (self *monitorProgressOpaqueTestError) Is(error) bool { panic("foreign Is must not execute") }

// A foreign matching callback cannot fabricate a timeout error.
func (self *monitorProgressOpaqueTestError) As(any) bool { panic("foreign As must not execute") }

// Incomplete joined graphs retain their actual child list for finite inspection.
type monitorProgressJoinedTestError struct{ causes []error }

// This diagnostic never walks its children.
func (self *monitorProgressJoinedTestError) Error() string { return "synthetic joined progress causes" }

// The producer owns an immutable graph for the duration of a read.
func (self *monitorProgressJoinedTestError) Unwrap() []error { return self.causes }

// A deliberately unbounded control still terminates, with an assertion failure
// rather than an indefinitely running recursive error walk.
type monitorProgressCycleTestError struct{ visits int }

// The cycle's diagnostic is deliberately finite.
func (self *monitorProgressCycleTestError) Error() string { return "synthetic cyclic progress cause" }

// Only the first32 graph levels may be inspected by the production owner.
func (self *monitorProgressCycleTestError) Unwrap() error {
	self.visits++
	if self.visits > 1024 {
		return io.EOF
	}
	return self
}

// A foreign mutable edge exposes why checking and then unwrapping again loses
// a hard first observation. A single observation must decide the read.
type monitorProgressChangingTestError struct {
	visits int
	hard   error
}

// No diagnostic callback observes or changes the edge.
func (self *monitorProgressChangingTestError) Error() string {
	return "synthetic changing progress cause"
}

// The first actual edge is hard; another call would erase it.
func (self *monitorProgressChangingTestError) Unwrap() error {
	self.visits++
	if self.visits == 1 {
		return self.hard
	}
	return io.EOF
}

// Run the production wire parser and selected GET owner on a real socket. The
// first physical close alone receives the cause; retry would see valid bytes.
func monitorProgressCauseRead(t *testing.T, kind string, cause error, permitRetry bool) (bool, string, int32, int32, int) {
	t.Helper()
	fixture := newMonitorProgressReadFixture(t, kind)
	var calls, closed atomic.Int32
	var selected atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Error("progress cause recovery changed request method")
		}
		if calls.Add(1) == 1 {
			selected.Store(request.URL.RequestURI())
		} else if selected.Load() != request.URL.RequestURI() {
			t.Error("progress cause recovery changed selected source")
		}
		_, _ = writer.Write(fixture.body)
	}))
	defer server.Close()
	client := newMonitorProviderClient()
	transport := client.Transport.(*http.Transport)
	defer transport.CloseIdleConnections()
	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if calls.Load() != closed.Load() {
			t.Error("next progress GET started before prior physical close")
		}
		if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > time.Minute || client.Timeout != time.Minute {
			t.Error("progress cause classification changed attempt allowance")
		}
		response, err := transport.RoundTrip(request)
		if err == nil {
			var physical error
			if calls.Load() == 1 {
				physical = cause
			}
			response.Body = &monitorProgressCauseTestBody{ReadCloser: response.Body, closed: &closed, cause: physical}
		}
		return response, err
	})
	waits := 0
	clock := monitorProgressReadClock{wait: func(ctx context.Context, delay time.Duration) error {
		waits++
		if calls.Load() != closed.Load() {
			t.Error("progress wait retained an actual response body")
		}
		if !permitRetry {
			return errors.New("unexpected retry of a hard progress cause")
		}
		return ctx.Err()
	}}
	got, code := fixture.read(t.Context(), client, server.URL, clock)
	return got, code, calls.Load(), closed.Load(), waits
}

// Every malformed or mixed hard graph stops both real progress consumers after
// the first closed response; no later healthy response can hide the refusal.
func TestMonitorProgressHttpCauseGraphsRemainHardAndBounded(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		hard := &os.PathError{Op: "close", Path: "synthetic-progress-custody", Err: context.DeadlineExceeded}
		cycle := &monitorProgressCycleTestError{}
		var deep error = io.EOF
		for range 64 {
			deep = fmt.Errorf("synthetic progress wrapper: %w", deep)
		}
		wide := make([]error, 129)
		for index := range wide {
			wide[index] = io.EOF
		}
		for _, cause := range []error{
			(*net.DNSError)(nil), (*url.Error)(nil), (*monitorProgressJoinedTestError)(nil),
			hard, &os.LinkError{Op: "read", Old: "synthetic-old", New: "synthetic-new", Err: syscall.EIO},
			&monitorProgressOpaqueTestError{}, &monitorProgressForeignTestError{cause: nil},
			&monitorProgressForeignTestError{cause: errors.Join(io.EOF, hard)},
			&monitorProgressJoinedTestError{causes: []error{io.EOF, nil}},
			&monitorProgressJoinedTestError{causes: wide}, deep, cycle,
			errors.Join(io.ErrUnexpectedEOF, context.Canceled),
			&net.DNSError{IsTimeout: true, IsNotFound: true},
			&net.DNSError{IsTimeout: true, UnwrapErr: hard},
		} {
			got, code, calls, closed, waits := monitorProgressCauseRead(t, kind, cause, false)
			if got || code != "unavailable" || calls != 1 || closed != 1 || waits != 0 {
				t.Fatalf("%s %T softened a hard progress graph: got=%t code=%s calls=%d closed=%d waits=%d", kind, cause, got, code, calls, closed, waits)
			}
		}
		if cycle.visits > 32 {
			t.Fatalf("%s inspected a cyclic graph beyond its depth allowance: %d", kind, cycle.visits)
		}
	}
}

// Complete physical transient leaves still recover. Even a benign foreign
// wrapper must be traversed by its explicit edge, never its matching methods.
func TestMonitorProgressHttpCauseGraphsRetainCompleteRecovery(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, cause := range []error{
			errors.Join(io.ErrUnexpectedEOF, context.DeadlineExceeded),
			&monitorProgressForeignTestError{cause: io.EOF},
			&url.Error{Op: "Read", URL: "https://synthetic.example/progress", Err: syscall.ECONNRESET},
			&net.DNSError{IsTimeout: true}, &net.DNSError{IsTemporary: true, UnwrapErr: syscall.ECONNRESET},
		} {
			got, code, calls, closed, waits := monitorProgressCauseRead(t, kind, cause, true)
			if !got || code != "ok" || calls != 2 || closed != 2 || waits != 1 {
				t.Fatalf("%s %T lost valid transport recovery: got=%t code=%s calls=%d closed=%d waits=%d", kind, cause, got, code, calls, closed, waits)
			}
		}
	}
}

// Re-reading a foreign edge must not replace a completed hard observation with
// a later transient answer from the same wrapper.
func TestMonitorProgressHttpCauseReadsEachForeignEdgeOnce(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		cause := &monitorProgressChangingTestError{hard: &os.PathError{Op: "close", Path: "synthetic-original-custody", Err: context.DeadlineExceeded}}
		got, code, calls, closed, waits := monitorProgressCauseRead(t, kind, cause, false)
		if got || code != "unavailable" || calls != 1 || closed != 1 || waits != 0 || cause.visits != 1 {
			t.Fatalf("%s repeated foreign edge replaced hard cause: got=%t code=%s calls=%d closed=%d waits=%d visits=%d", kind, got, code, calls, closed, waits, cause.visits)
		}
	}
}
