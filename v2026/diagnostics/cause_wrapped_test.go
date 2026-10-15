// The wrapped classifier opens only the standard library's own wrappers and a
// caller's envelope; every other value keeps ClassifyCause's closed rule.
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"sync/atomic"
	"testing"
)

// A caller-owned transparent node and a recognized leaf for envelope tests.
type wrappedCauseEnvelopeNode struct{ children []error }

func (*wrappedCauseEnvelopeNode) Error() string { panic("envelope node was formatted") }

type wrappedCauseEnvelopeLeaf struct{}

func (*wrappedCauseEnvelopeLeaf) Error() string { panic("envelope leaf was formatted") }

func wrappedCauseTestEnvelope(err error) ([]error, Cause, bool) {
	switch value := err.(type) {
	case *wrappedCauseEnvelopeNode:
		return value.children, CauseUnknown, true
	case *wrappedCauseEnvelopeLeaf:
		return nil, CauseUnavailable, true
	}
	return nil, CauseUnknown, false
}

// Standard %w and Join wrappers reach their sentinel; disagreement, opaque
// children and empty joins stay unknown.
func TestDiagnosticWrappedCauseFollowsStandardWrappers(t *testing.T) {
	for _, candidate := range []struct {
		name  string
		cause error
		want  Cause
	}{
		{name: "nil", cause: nil, want: CauseNone},
		{name: "plain sentinel", cause: context.DeadlineExceeded, want: CauseTimeout},
		{name: "fmt wrap", cause: fmt.Errorf("step timeout after 3 attempts: %w", context.DeadlineExceeded), want: CauseTimeout},
		{name: "nested fmt and url", cause: fmt.Errorf("step failed: %w", fmt.Errorf("verify post via x: %w", &url.Error{Op: "Post", URL: "https://synthetic.example/verify", Err: context.DeadlineExceeded})), want: CauseTimeout},
		{name: "fmt multiple wraps agree", cause: fmt.Errorf("%w and %w", context.Canceled, context.Canceled), want: CauseCanceled},
		{name: "fmt multiple wraps disagree", cause: fmt.Errorf("%w and %w", context.Canceled, context.DeadlineExceeded), want: CauseUnknown},
		{name: "single join", cause: errors.Join(context.DeadlineExceeded), want: CauseTimeout},
		{name: "join agrees", cause: errors.Join(io.EOF, &url.Error{Err: io.ErrUnexpectedEOF}), want: CauseTransport},
		{name: "join disagrees", cause: errors.Join(io.EOF, fs.ErrPermission), want: CauseUnknown},
		{name: "join with opaque child", cause: errors.Join(context.DeadlineExceeded, errors.New("synthetic opaque")), want: CauseUnknown},
		{name: "wrap of nil", cause: fmt.Errorf("nothing: %w", nil), want: CauseUnknown},
		{name: "opaque leaf", cause: errors.New("synthetic opaque"), want: CauseUnknown},
		{name: "envelope node", cause: fmt.Errorf("owned: %w", &wrappedCauseEnvelopeNode{children: []error{context.Canceled}}), want: CauseCanceled},
		{name: "envelope leaf", cause: errors.Join(&wrappedCauseEnvelopeLeaf{}), want: CauseUnavailable},
		{name: "envelope empty node", cause: &wrappedCauseEnvelopeNode{}, want: CauseUnknown},
	} {
		if got := ClassifyWrappedCause(candidate.cause, wrappedCauseTestEnvelope); got != candidate.want {
			t.Fatalf("%s: got %d want %d", candidate.name, got, candidate.want)
		}
	}
	if got := ClassifyWrappedCause(&wrappedCauseEnvelopeNode{children: []error{io.EOF}}, nil); got != CauseUnknown {
		t.Fatal("a node without its envelope was opened")
	}
}

// ClassifyCause itself still refuses joins and fmt wrappers.
func TestDiagnosticCauseStillRefusesStandardWrappers(t *testing.T) {
	for _, cause := range []error{errors.Join(context.DeadlineExceeded), fmt.Errorf("x: %w", context.DeadlineExceeded)} {
		if ClassifyCause(cause) != CauseUnknown {
			t.Fatal("ClassifyCause opened a standard wrapper")
		}
	}
}

// Counts every method call without blocking. fmt.Errorf formats its %w operand
// once while it builds the wrapper, so wrapped variants compare counts taken
// after construction.
type wrappedCauseCountingProbe struct{ calls atomic.Uint64 }

func (self *wrappedCauseCountingProbe) Error() string { self.calls.Add(1); return "synthetic" }
func (self *wrappedCauseCountingProbe) Unwrap() error {
	self.calls.Add(1)
	return context.DeadlineExceeded
}
func (self *wrappedCauseCountingProbe) Is(error) bool { self.calls.Add(1); return true }
func (self *wrappedCauseCountingProbe) As(any) bool   { self.calls.Add(1); return true }
func (self *wrappedCauseCountingProbe) Timeout() bool { self.calls.Add(1); return true }

// No foreign method runs, even inside standard wrappers and joins, and a
// type that merely embeds a standard wrapper stays closed.
func TestDiagnosticWrappedCauseNeverInvokesForeignMethods(t *testing.T) {
	for _, variant := range []string{"single", "joined"} {
		probe := &diagnosticCauseProbe{entered: make(chan struct{}, 1), release: make(chan struct{})}
		var cause error = probe
		if variant == "joined" {
			cause = errors.Join(context.DeadlineExceeded, probe)
		}
		finished := make(chan Cause, 1)
		go func() { finished <- ClassifyWrappedCause(cause, nil) }()
		var result Cause
		select {
		case result = <-finished:
			close(probe.release)
		case <-probe.entered:
			close(probe.release)
			result = <-finished
		}
		if probe.calls.Load() != 0 || result != CauseUnknown {
			t.Fatalf("wrapped diagnostic cause invoked a foreign method: %s", variant)
		}
	}
	type embedded struct{ error }
	for _, variant := range []string{"wrapped", "joined-wrapped", "embedded"} {
		probe := &wrappedCauseCountingProbe{}
		var cause error
		switch variant {
		case "wrapped":
			cause = fmt.Errorf("x: %w", probe)
		case "joined-wrapped":
			cause = errors.Join(fmt.Errorf("x: %w", probe), context.DeadlineExceeded)
		case "embedded":
			cause = embedded{error: fmt.Errorf("x: %w", probe)}
		}
		before := probe.calls.Load()
		if result := ClassifyWrappedCause(cause, nil); result != CauseUnknown || probe.calls.Load() != before {
			t.Fatalf("wrapped diagnostic cause invoked a foreign method or opened it: %s", variant)
		}
	}
}

// Depth, width and node budgets end inspection as unknown.
func TestDiagnosticWrappedCauseBounds(t *testing.T) {
	for _, depth := range []int{wrappedCauseMaximumDepth - 1, wrappedCauseMaximumDepth} {
		var cause error = context.DeadlineExceeded
		for range depth {
			cause = fmt.Errorf("w: %w", cause)
		}
		got := ClassifyWrappedCause(cause, nil)
		if depth < wrappedCauseMaximumDepth && got != CauseTimeout || depth == wrappedCauseMaximumDepth && got != CauseUnknown {
			t.Fatalf("wrapped depth bound differs at %d: %d", depth, got)
		}
	}
	wide := make([]error, wrappedCauseMaximumNodes+1)
	for index := range wide {
		wide[index] = context.Canceled
	}
	if ClassifyWrappedCause(errors.Join(wide...), nil) != CauseUnknown {
		t.Fatal("a join wider than the node budget was classified")
	}
	narrow := make([]error, 16)
	for index := range narrow {
		narrow[index] = context.Canceled
	}
	if ClassifyWrappedCause(errors.Join(narrow...), nil) != CauseCanceled {
		t.Fatal("a join inside the node budget lost its agreed cause")
	}
	cycle := &wrappedCauseEnvelopeNode{}
	cycle.children = []error{cycle}
	if ClassifyWrappedCause(cycle, wrappedCauseTestEnvelope) != CauseUnknown {
		t.Fatal("an envelope cycle was not bounded")
	}
}
