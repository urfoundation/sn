//go:build linux || darwin

// Optional validator facts never repeat arbitrary error traversal already
// owned by the protocol operation. Core retry/custody rules remain unchanged.
package validator

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// An arbitrary method must not run merely to produce an operational fact.
type validatorDiagnosticMethodProbe struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Uint64
}

// A negative control can release and join this exact blocked invocation.
func (self *validatorDiagnosticMethodProbe) wait() {
	self.calls.Add(1)
	select {
	case self.entered <- struct{}{}:
	default:
	}
	<-self.release
}

// Raw diagnostic conversion is forbidden along with type/cause callbacks.
func (self *validatorDiagnosticMethodProbe) Error() string {
	self.wait()
	return "synthetic opaque detail"
}

// Existing core predicates own any traversal they need; logs do not repeat it.
func (self *validatorDiagnosticMethodProbe) Unwrap() error {
	self.wait()
	return context.DeadlineExceeded
}

// Is is arbitrary application code and cannot supply a bounded log fact.
func (self *validatorDiagnosticMethodProbe) Is(error) bool { self.wait(); return false }

// As is equally unsuitable for optional runtime classification.
func (self *validatorDiagnosticMethodProbe) As(any) bool { self.wait(); return false }

// One actual serializer is observed through its owned exporter. The two
// exported test roots keep their independent causal failure membership.
func validatorDiagnosticMethodBarrierTest(t *testing.T, trail bool) {
	t.Helper()
	probe := &validatorDiagnosticMethodProbe{entered: make(chan struct{}, 1), release: make(chan struct{})}
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(t.Context())
	ctx, owner, err := newReleaseDiagnostics(ctx, output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		if err := owner.close(); err != nil {
			t.Error("validator diagnostic cause owner failed to close")
		}
	})
	original := &TrailError{Kind: TrailErrorHop, Err: probe}
	finished := make(chan struct{})
	go func() {
		if trail {
			observeTrailDiagnostic(ctx, trailDiagnosticFailed, original, nil, false)
		} else {
			releaseDiagnostic(ctx, "steering", "read_wait", 7, true, 0, releaseDiagnosticFacts{phase: productionReadReceipt, cause: releaseDiagnosticReadCause(probe)})
		}
		cancel()
		close(finished)
	}()
	select {
	case <-finished:
		close(probe.release)
	case <-probe.entered:
		close(probe.release)
		<-finished
	}
	if probe.calls.Load() != 0 || ctx.Err() != context.Canceled || original.Err != probe {
		t.Fatal("validator optional cause traversal held required continuation")
	}
	<-output.written
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "cause=unknown") || strings.Contains(output.String(), "synthetic opaque") {
		t.Fatal("validator opaque cause changed its bounded scalar wire")
	}
}

// The actual trail serializer cannot hold the caller before cancellation.
func TestTrailDiagnosticsForeignMethodsCannotHoldObservation(t *testing.T) {
	validatorDiagnosticMethodBarrierTest(t, true)
}

// Read diagnostics cannot repeat core cause traversal before cancellation.
func TestReleaseDiagnosticsForeignMethodsCannotHoldObservation(t *testing.T) {
	validatorDiagnosticMethodBarrierTest(t, false)
}

// Concrete cause fields still distinguish timeouts, unavailable receipt data
// and transport; nil/opaque/cyclic values cannot impersonate those facts.
func TestValidatorDiagnosticsKnownFieldsAndOpaqueWrappers(t *testing.T) {
	for _, candidate := range []struct {
		cause error
		want  releaseDiagnosticCause
	}{
		{cause: context.DeadlineExceeded, want: releaseDiagnosticTimeout},
		{cause: &url.Error{Op: "Get", URL: "https://synthetic.example", Err: context.DeadlineExceeded}, want: releaseDiagnosticTimeout},
		{cause: &crv4.ReceiptEvidenceUnavailableError{}, want: releaseDiagnosticUnavailable},
		{cause: io.EOF, want: releaseDiagnosticTransport},
		{cause: (*crv4.ReceiptEvidenceUnavailableError)(nil), want: releaseDiagnosticUnknown},
		{cause: (*validatorDiagnosticMethodProbe)(nil), want: releaseDiagnosticUnknown},
		{cause: errors.Join(context.DeadlineExceeded), want: releaseDiagnosticUnknown},
		{cause: errors.Join(&crv4.ReceiptEvidenceUnavailableError{}, errors.New("synthetic hard custody failure")), want: releaseDiagnosticUnknown},
	} {
		if got := releaseDiagnosticReadCause(candidate.cause); got != candidate.want {
			t.Fatalf("closed validator read fact changed: got %d want %d", got, candidate.want)
		}
	}
	cycle := &url.Error{}
	cycle.Err = cycle
	if releaseDiagnosticReadCause(cycle) != releaseDiagnosticUnknown || cycle.Err != cycle {
		t.Fatal("validator diagnostic cause changed or escaped a cycle")
	}
	for _, candidate := range []struct {
		cause error
		kind  string
		fact  string
	}{
		{cause: &TrailError{Kind: TrailErrorHop, Err: &url.Error{Err: context.DeadlineExceeded}}, kind: "hop", fact: "timeout"},
		{cause: &TrailError{Kind: TrailErrorSeed, Err: context.Canceled}, kind: "seed", fact: "canceled"},
		{cause: &TrailError{Kind: TrailErrorProtocol, Err: io.ErrUnexpectedEOF}, kind: "protocol", fact: "transport"},
		{cause: &TrailError{Kind: TrailErrorUnknownOutcome, Err: errors.Join(context.DeadlineExceeded)}, kind: "unknown_final", fact: "unknown"},
		{cause: (*TrailError)(nil), kind: "unknown", fact: "unknown"},
		{cause: &TrailError{Kind: TrailErrorHop, Err: cycle}, kind: "hop", fact: "unknown"},
	} {
		kind, fact := trailDiagnosticClassification(candidate.cause)
		if kind != candidate.kind || fact != candidate.fact {
			t.Fatalf("closed trail classification changed: got %s/%s want %s/%s", kind, fact, candidate.kind, candidate.fact)
		}
	}
}
