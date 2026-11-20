// Composed production readers preserve their own original cause trees without
// reopening rejected native subtrees or trusting foreign error matchers.
package validator

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// A finite diagnostic and defensive test guard make cyclic error regression
// failures explicit without depending on a process timeout or stack overflow.
type productionSteeringCauseNode struct {
	children []error
	visits   int
}

// Keep diagnostics independent of recursive child formatting.
func (*productionSteeringCauseNode) Error() string { return "synthetic composed read cause" }

// Expose exact child identity while bounding regression-control failure.
func (self *productionSteeringCauseNode) Unwrap() []error {
	self.visits++
	if self.visits > 1024 {
		panic("production read cause traversal escaped its finite allowance")
	}
	return self.children
}

// A matching interface cannot borrow a wrapped deadline's retry authority.
type productionSteeringCauseMatcher struct{ cause error }

// Diagnostic text never grants read authority.
func (*productionSteeringCauseMatcher) Error() string { return "synthetic foreign read matcher" }

// Keep the proposed child visible without invoking either matcher.
func (self *productionSteeringCauseMatcher) Unwrap() error { return self.cause }

// The trusted classifier must never consult foreign matching methods.
func (*productionSteeringCauseMatcher) Is(error) bool { panic("foreign Is called") }

// The trusted classifier must never consult foreign matching methods.
func (*productionSteeringCauseMatcher) As(any) bool { panic("foreign As called") }

// Actual read ownership, rather than a direct classifier result alone, must
// stop malformed or mixed observations before retry pacing and retain no wait.
func TestProductionSteeringReadCauseTraversalIsBounded(t *testing.T) {
	cycle := &productionSteeringCauseNode{}
	cycle.children = []error{cycle}
	if retryableProductionSteeringRead(cycle) || cycle.visits > 33 {
		t.Fatalf("production classifier escaped its cycle bound: visits=%d", cycle.visits)
	}
	wide := &productionSteeringCauseNode{children: make([]error, 129)}
	for index := range wide.children {
		wide.children[index] = context.DeadlineExceeded
	}
	var network *net.OpError
	var unavailable *crv4.ReceiptEvidenceUnavailableError
	for _, cause := range []error{
		cycle, wide, network, unavailable, &productionSteeringCauseNode{},
		&productionSteeringCauseNode{children: []error{context.DeadlineExceeded, nil}},
		&productionSteeringCauseMatcher{cause: context.DeadlineExceeded},
		errors.Join(context.DeadlineExceeded, errors.New("synthetic canonical contradiction")),
	} {
		self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
		reads, waits := 0, 0
		self.productionReadHooks.wait = func(context.Context, time.Duration) error {
			waits++
			return context.DeadlineExceeded
		}
		err := self.productionRead(t.Context(), productionReadReceipt, nil, func(context.Context) error {
			reads++
			return cause
		})
		if err == nil || waits != 0 || reads != 1 || retryableProductionSteeringRead(err) {
			t.Fatalf("malformed or mixed production cause gained a retry: reads=%d waits=%d", reads, waits)
		}
	}
	for _, cause := range []error{syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE, syscall.ETIMEDOUT, syscall.EAGAIN} {
		if !retryableProductionSteeringRead(cause) {
			t.Fatalf("trusted standard errno was confused with a foreign matcher: %v", cause)
		}
	}
}
