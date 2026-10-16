// Native read causes are admitted through finite, complete trees. These
// regressions reach the real HTTP owner as well as its composed consumers.
package crv4

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// Stable diagnostics permit a cycle without recursively formatting it. The
// guard makes an unbounded classifier mutation fail instead of hanging a test.
type substrateReadCauseNode struct {
	children []error
	visits   int
}

// Diagnostics never format child causes.
func (*substrateReadCauseNode) Error() string { return "synthetic read cause tree" }

// Expose exact child identity with an observable finite visitation guard.
func (self *substrateReadCauseNode) Unwrap() []error {
	self.visits++
	if self.visits > 1024 {
		panic("read cause classifier exceeded its traversal allowance")
	}
	return self.children
}

// Foreign matching methods must never be consulted to infer transport origin.
type substrateReadCauseMatcher struct{ cause error }

// This text has no transport authority.
func (*substrateReadCauseMatcher) Error() string { return "synthetic opaque matcher" }

// Even a retryable child cannot grant a foreign matcher authority.
func (self *substrateReadCauseMatcher) Unwrap() error { return self.cause }

// Matching is outside the classifier's trusted concrete taxonomy.
func (*substrateReadCauseMatcher) Is(error) bool { panic("foreign Is called") }

// Matching is outside the classifier's trusted concrete taxonomy.
func (*substrateReadCauseMatcher) As(any) bool { panic("foreign As called") }

// A timeout interface cannot bypass an independent wrapped contradiction.
type substrateReadCauseTimeoutWrapper struct{ cause error }

// The wrapper reports a timeout while retaining an independent hard child.
func (*substrateReadCauseTimeoutWrapper) Error() string { return "synthetic timeout wrapper" }

// This interface cannot conceal the complete cause.
func (*substrateReadCauseTimeoutWrapper) Timeout() bool { return true }

// This interface cannot conceal the complete cause.
func (*substrateReadCauseTimeoutWrapper) Temporary() bool { return true }

// Preserve the contradictory leaf for a complete decision.
func (self *substrateReadCauseTimeoutWrapper) Unwrap() error { return self.cause }

// An application response is distinct from a physical connection failure.
type substrateReadCapacityReply struct{ message string }

// Preserve the exact structured application message used by capacity pacing.
func (self *substrateReadCapacityReply) Error() string { return self.message }

// Implement the native client's structured application-error contract.
func (*substrateReadCapacityReply) ErrorCode() int { return -32000 }

// A structured code on a wrapper cannot hide a contradictory child.
type substrateReadCapacityBranch struct{ children []error }

// Match the exact capacity message while exposing an independent cause.
func (*substrateReadCapacityBranch) Error() string { return "Historical work rate limit exceeded" }

// Implement the native application-error contract on the wrapper itself.
func (*substrateReadCapacityBranch) ErrorCode() int { return -32000 }

// Classification must check every child before trusting the wrapper message.
func (self *substrateReadCapacityBranch) Unwrap() []error { return self.children }

// All public and local verdicts stop on cycles, nil members and excessive
// width without asking a custom matcher to impersonate a structured cause.
func TestSubstrateReadCauseTraversalIsBounded(t *testing.T) {
	for _, predicate := range []struct {
		name string
		read func(error) bool
	}{
		{name: "RPC", read: substrateRPCDisconnected},
		{name: "HTTP", read: RetryableSubstrateReadTransportError},
		{name: "presence", read: HasSubstrateReadTransportCause},
		{name: "capacity", read: substrateRPCHistoricalCapacity},
		{name: "receipt", read: receiptScanUnavailable},
	} {
		cycle := &substrateReadCauseNode{}
		cycle.children = []error{cycle}
		if predicate.read(cycle) || cycle.visits > 66 {
			t.Fatalf("%s accepted or excessively traversed a cycle: visits=%d", predicate.name, cycle.visits)
		}
		var typedNil *net.OpError
		wide := &substrateReadCauseNode{children: make([]error, 129)}
		for index := range wide.children {
			wide.children[index] = context.DeadlineExceeded
		}
		for _, cause := range []error{nil, typedNil, &substrateReadCauseNode{}, wide,
			&substrateReadCauseNode{children: []error{context.DeadlineExceeded, nil}},
			&substrateReadCauseMatcher{cause: errors.New("synthetic contradiction")},
		} {
			if predicate.read(cause) {
				t.Fatalf("%s accepted an incomplete or opaque cause tree", predicate.name)
			}
		}
	}
	var status *SubstrateReadHttpStatusError
	var physical *substrateReadHttpTransportError
	var release *substrateReadHttpCloseError
	for _, cause := range []error{status, physical, release} {
		if IsSubstrateReadTransportCause(cause) || HasSubstrateReadTransportCause(cause) || RetryableSubstrateReadTransportError(cause) {
			t.Fatal("typed nil acquired native transport origin")
		}
	}
}

// Standard errno implements Is, so it must be recognized by concrete type
// before foreign matcher rejection. The actual response is closed and its
// uncertain connection discarded before the same native read recovers.
func TestSubstrateReadCauseTrustedErrnoRecoversActualHttpRead(t *testing.T) {
	for _, cause := range []error{syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE, syscall.ETIMEDOUT, syscall.EAGAIN} {
		transport := &substrateReadHttpCloseFixture{t: t, cause: cause}
		client := newSubstrateReadHttpDecoratedFixture(t, 1024, transport)
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		var result string
		if err := client.CallContext(t.Context(), &result, "chain_getFinalizedHead"); err != nil || result != "0x2a00" ||
			transport.calls != 2 || transport.closes != 2 || transport.discards != 1 || budget.elapsed != 75*time.Second {
			t.Fatalf("trusted physical errno lost bounded recovery: cause=%v calls=%d closes=%d discards=%d elapsed=%s error=%v", cause, transport.calls, transport.closes, transport.discards, budget.elapsed, err)
		}
	}
}

// The configured GSRPC path invokes both capacity and transport classifiers.
// A cyclic or mixed physical close must finish at the first response with no
// successful result, retry pacing, or borrowed partial receipt evidence.
func TestSubstrateReadCauseActualHttpRejectsMalformedAndMixedTrees(t *testing.T) {
	cycle := &substrateReadCauseNode{}
	cycle.children = []error{cycle}
	var typedNil *net.OpError
	for _, cause := range []error{
		cycle, typedNil,
		&substrateReadCauseMatcher{cause: context.DeadlineExceeded},
		errors.Join(io.ErrUnexpectedEOF, context.DeadlineExceeded, errors.New("synthetic body integrity contradiction")),
		&os.PathError{Op: "close", Path: "synthetic-custody", Err: context.DeadlineExceeded},
		&substrateReadCauseTimeoutWrapper{cause: errors.Join(context.DeadlineExceeded, context.Canceled)},
	} {
		transport := &substrateReadHttpCloseFixture{t: t, cause: cause}
		client := newSubstrateReadHttpDecoratedFixture(t, 1024, transport)
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		var result string
		err := client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		if err == nil || result != "" || transport.calls != 1 || transport.closes != 1 || transport.discards != 1 || budget.elapsed != 0 {
			t.Fatalf("hard native cause reached another read: calls=%d closes=%d discards=%d elapsed=%s", transport.calls, transport.closes, transport.discards, budget.elapsed)
		}
		if RetryableSubstrateReadTransportError(err) || receiptScanUnavailable(err) {
			t.Fatal("hard native close gained retry or partial receipt authority")
		}
	}
}

// The application capacity response has a separate exact taxonomy. Transport
// timeouts, cancellation and arbitrary matching methods cannot acquire it.
func TestSubstrateReadCauseCapacityAndReceiptKeepHardDominance(t *testing.T) {
	capacity := &substrateReadCapacityReply{message: "Historical work rate limit exceeded"}
	if !substrateRPCHistoricalCapacity(errors.Join(capacity, capacity)) || substrateRPCDisconnected(capacity) {
		t.Fatal("exact capacity response lost its distinct application classification")
	}
	for _, cause := range []error{
		errors.Join(capacity, context.DeadlineExceeded),
		errors.Join(capacity, context.Canceled),
		&substrateReadCauseMatcher{cause: capacity},
		&substrateReadCapacityBranch{children: []error{capacity, context.Canceled}},
	} {
		if substrateRPCHistoricalCapacity(cause) {
			t.Fatal("mixed or opaque capacity cause acquired provider cooldown")
		}
	}
	hard := errors.New("synthetic incomplete coverage contradiction")
	if receiptScanUnavailable(&substrateReadCauseTimeoutWrapper{cause: errors.Join(context.DeadlineExceeded, hard)}) {
		t.Fatal("timeout wrapper hid a receipt coverage contradiction")
	}
	if !receiptScanUnavailable(context.Canceled) || !receiptScanUnavailable(context.DeadlineExceeded) {
		t.Fatal("caller interruption lost its existing partial-scan semantics")
	}
}
