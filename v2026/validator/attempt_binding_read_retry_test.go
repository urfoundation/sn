// Exercise bounded original-cause classification and the actual signed trail
// caller without repeating assigned work or discarding hard failures on cancel.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urnetwork/connect/v2026"
)

// Keeps malformed joins intact, including nil branches that errors.Join drops.
type attemptReadRetryJoinedError struct {
	causes []error
}

// Avoids asking malformed children to format themselves.
func (self *attemptReadRetryJoinedError) Error() string { return "synthetic original joined cause" }

// Exposes the original branches without filtering them.
func (self *attemptReadRetryJoinedError) Unwrap() []error { return self.causes }

// Counts traversal without recursive formatting or custom matching.
type attemptReadRetryWrappedError struct {
	cause   error
	unwraps int
}

// Formatting never traverses the cause graph under test.
func (self *attemptReadRetryWrappedError) Error() string { return "synthetic original wrapper" }

// Records each original-cause visit.
func (self *attemptReadRetryWrappedError) Unwrap() error {
	self.unwraps++
	return self.cause
}

// The escape after 64 visits makes an unbounded pre-fix walker fail finitely.
// A correct classifier refuses the cyclic graph before that escape is reached.
type attemptReadRetryCycleError struct {
	escape  error
	unwraps int
}

// A cycle must not recurse while the actual caller retains its error text.
func (self *attemptReadRetryCycleError) Error() string { return "synthetic cyclic original cause" }

// Returns the same cause until the deterministic pre-fix escape guard fires.
func (self *attemptReadRetryCycleError) Unwrap() error {
	self.unwraps++
	if self.unwraps >= 64 {
		return self.escape
	}
	return self
}

// Attempts to substitute a timeout for an opaque original error via errors.As.
type attemptReadRetryMatcherError struct {
	cause   error
	matches int
	unwraps int
}

// The diagnostic deliberately resembles a transport error but grants nothing.
func (self *attemptReadRetryMatcherError) Error() string { return "synthetic connection reset timeout" }

// A foreign matcher must never execute during classification.
func (self *attemptReadRetryMatcherError) As(target any) bool {
	self.matches++
	if netErr, ok := target.(*net.Error); ok {
		*netErr = context.DeadlineExceeded.(net.Error)
		return true
	}
	return false
}

// A custom matcher cannot gain authority by also exposing a timeout child.
func (self *attemptReadRetryMatcherError) Unwrap() error {
	self.unwraps++
	return self.cause
}

// Supplies a leaf matcher with no unwrap method, reproducing the old As call.
type attemptReadRetryLeafMatcherError struct {
	matches int
}

// Error text carries no authority.
func (self *attemptReadRetryLeafMatcherError) Error() string { return "synthetic timeout leaf" }

// Counts the foreign dispatch that the corrected classifier must not perform.
func (self *attemptReadRetryLeafMatcherError) As(target any) bool {
	self.matches++
	if netErr, ok := target.(*net.Error); ok {
		*netErr = context.DeadlineExceeded.(net.Error)
		return true
	}
	return false
}

// Exposes a real child while attempting foreign errors.Is substitution.
type attemptReadRetryIsError struct {
	cause   error
	matches int
	unwraps int
}

// The label grants no classification authority.
func (self *attemptReadRetryIsError) Error() string { return "synthetic owner-context matcher" }

// A foreign matcher must not be consulted even when it claims every target.
func (self *attemptReadRetryIsError) Is(error) bool { self.matches++; return true }

// The matching owner must be rejected before exposing its claimed child.
func (self *attemptReadRetryIsError) Unwrap() error {
	self.unwraps++
	return self.cause
}

// Reports typed-nil panic regressions as a normal failed root, not a crashed
// qualification process that conceals the remaining designated controls.
func classifyAttemptReadSafely(t *testing.T, name string, classify func() bool) (result bool) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s panicked while classifying its original cause", name)
		}
	}()
	return classify()
}

// Nil nodes and excessive width/depth/node counts are incomplete authority,
// even when every reachable leaf within a smaller prefix looks transient.
func TestAttemptBindingReadRetryRefusesMalformedAndExcessiveCauses(t *testing.T) {
	var nilRead *attemptBindingReadError
	var nilNetwork *net.OpError
	var nilHttp *gethrpc.HTTPError
	var nilWrapper *attemptReadRetryWrappedError
	deep := error(context.DeadlineExceeded)
	for index := 0; index < 40; index++ {
		deep = &attemptReadRetryWrappedError{cause: deep}
	}
	wide := &attemptReadRetryJoinedError{causes: make([]error, 129)}
	shared := &attemptReadRetryJoinedError{causes: make([]error, 16)}
	for index := range wide.causes {
		wide.causes[index] = context.DeadlineExceeded
	}
	for index := range shared.causes {
		shared.causes[index] = context.DeadlineExceeded
	}
	excessive := &attemptReadRetryJoinedError{causes: make([]error, 64)}
	for index := range excessive.causes {
		excessive.causes[index] = shared
	}
	for _, test := range []struct {
		name  string
		cause error
	}{
		{name: "nil marker", cause: nilRead},
		{name: "nil network", cause: nilNetwork},
		{name: "nil http", cause: nilHttp},
		{name: "nil wrapper", cause: nilWrapper},
		{name: "empty marker", cause: &attemptBindingReadError{}},
		{name: "empty join", cause: &attemptReadRetryJoinedError{}},
		{name: "nil joined branch", cause: &attemptReadRetryJoinedError{causes: []error{context.DeadlineExceeded, nil}}},
		{name: "excessive depth", cause: deep},
		{name: "excessive width", cause: wide},
		{name: "shared node allowance", cause: excessive},
	} {
		err := &attemptBindingReadError{cause: test.cause}
		if classifyAttemptReadSafely(t, test.name, func() bool { return retryableAttemptBindingReadError(err) }) {
			t.Fatalf("%s authorized retry without a complete bounded original cause", test.name)
		}
	}
}

// Cycles terminate by the same depth allowance in both pre-cancel and
// post-cancel classification, with no timeout or scheduler dependency.
func TestAttemptBindingReadRetryBoundsCyclicOriginalCauses(t *testing.T) {
	for _, want := range []error{context.DeadlineExceeded, context.Canceled} {
		cycle := &attemptReadRetryCycleError{escape: want}
		err := &attemptBindingReadError{cause: cycle}
		if retryableAttemptBindingReadError(err) || cycle.unwraps > 32 || cycle.unwraps == 0 {
			t.Fatalf("binding cycle escaped its allowance: visits=%d", cycle.unwraps)
		}
		cycle.unwraps = 0
		if onlyAttemptContextError(err, want) || cycle.unwraps > 32 || cycle.unwraps == 0 {
			t.Fatalf("owner-context cycle escaped its allowance: visits=%d", cycle.unwraps)
		}
	}
}

// Concrete read transport causes remain retryable, including standard errno
// whose built-in Is method must not be confused with foreign substitution.
func TestAttemptBindingReadRetryPreservesConcreteTransportCauses(t *testing.T) {
	for _, cause := range []error{
		context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF,
		syscall.ECONNABORTED, syscall.ECONNREFUSED, syscall.ECONNRESET,
		syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EPIPE, syscall.ETIMEDOUT,
		&net.DNSError{Err: "synthetic lookup timeout", Name: "rpc.example", IsTimeout: true},
		&net.DNSError{Err: "synthetic temporary lookup failure", Name: "rpc.example", IsTemporary: true},
		&url.Error{Op: "Post", URL: "https://rpc.example", Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
		gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable},
		&gethrpc.HTTPError{StatusCode: http.StatusTooEarly},
		errors.Join(context.DeadlineExceeded, io.ErrUnexpectedEOF),
	} {
		if !retryableAttemptBindingReadError(fmt.Errorf("binding response: %w", &attemptBindingReadError{cause: cause})) {
			t.Fatalf("concrete typed transport cause %T lost retry authority", cause)
		}
		if retryableAttemptBindingReadError(cause) {
			t.Fatalf("untyped cause %T gained binding provenance", cause)
		}
	}
}

// Matching hooks and diagnostic text cannot launder an opaque cause into
// binding-read authority, even when the hook also exposes a transient child.
func TestAttemptBindingReadRetryRejectsForeignMatcherSubstitution(t *testing.T) {
	leaf := &attemptReadRetryLeafMatcherError{}
	wrapped := &attemptReadRetryMatcherError{cause: context.DeadlineExceeded}
	claimed := &attemptReadRetryIsError{cause: context.DeadlineExceeded}
	for _, cause := range []error{leaf, wrapped, claimed, errors.New("connection refused: timeout")} {
		if retryableAttemptBindingReadError(&attemptBindingReadError{cause: cause}) {
			t.Fatalf("opaque cause %T acquired retry authority", cause)
		}
	}
	if leaf.matches != 0 || wrapped.matches != 0 || wrapped.unwraps != 0 {
		t.Fatalf("foreign matching or unwrap executed: leaf=%d wrapped=%d unwrap=%d", leaf.matches, wrapped.matches, wrapped.unwraps)
	}
	if claimed.matches != 0 || claimed.unwraps != 0 {
		t.Fatal("foreign Is matcher exposed claimed transport authority")
	}
}

// File, lifecycle, integrity and untyped sibling causes dominate independently
// of wrapper order or a successful transient branch elsewhere in the join.
func TestAttemptBindingReadRetryKeepsOriginalHardCauseDominant(t *testing.T) {
	hard := errors.New("synthetic canonical boundary changed")
	transient := &attemptBindingReadError{cause: context.DeadlineExceeded}
	for _, cause := range []error{
		&os.PathError{Op: "read", Path: "synthetic-checkpoint", Err: context.DeadlineExceeded},
		&os.LinkError{Op: "rename", Old: "synthetic-original", New: "synthetic-checkpoint", Err: context.DeadlineExceeded},
		&TrailFatalError{Err: context.DeadlineExceeded},
		&net.DNSError{Err: "synthetic lookup failure", Name: "rpc.example", IsTimeout: true, UnwrapErr: hard},
		&net.DNSError{Err: "synthetic name absent", Name: "rpc.example", IsNotFound: true, IsTimeout: true},
		&net.DNSError{Err: "synthetic name absent", Name: "rpc.example", IsNotFound: true, IsTemporary: true},
		&net.DNSError{Err: "synthetic name absent", Name: "rpc.example", IsNotFound: true, UnwrapErr: context.DeadlineExceeded},
		errors.Join(context.DeadlineExceeded, hard),
		errors.Join(hard, context.DeadlineExceeded),
		errors.Join(context.DeadlineExceeded, context.Canceled),
		gethrpc.HTTPError{StatusCode: http.StatusConflict},
		syscall.EPERM,
	} {
		if retryableAttemptBindingReadError(&attemptBindingReadError{cause: cause}) {
			t.Fatalf("hard original cause %T was hidden by read provenance", cause)
		}
	}
	for _, cause := range []error{
		errors.Join(transient, hard), errors.Join(hard, transient),
		errors.Join(transient, context.DeadlineExceeded),
		errors.Join(context.DeadlineExceeded, transient),
	} {
		if retryableAttemptBindingReadError(cause) {
			t.Fatal("a typed sibling promoted an independent hard or untyped cause")
		}
	}
}

// Owner cancellation can suppress a fatal result only after examining every
// original branch with the same hard-node and finite traversal constraints.
func TestAttemptContextErrorRequiresCompletePureOwnerCause(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		pure := fmt.Errorf("binding: %w", &attemptBindingReadError{cause: errors.Join(want, fmt.Errorf("closing: %w", want))})
		if !onlyAttemptContextError(pure, want) {
			t.Fatal("complete pure owner cause became fatal")
		}
		var nilRead *attemptBindingReadError
		var nilWrapper *attemptReadRetryWrappedError
		matcher := &attemptReadRetryMatcherError{cause: want}
		claimed := &attemptReadRetryIsError{cause: want}
		deep := want
		for index := 0; index < 40; index++ {
			deep = &attemptReadRetryWrappedError{cause: deep}
		}
		wide := &attemptReadRetryJoinedError{causes: make([]error, 129)}
		shared := &attemptReadRetryJoinedError{causes: make([]error, 16)}
		for index := range wide.causes {
			wide.causes[index] = want
		}
		for index := range shared.causes {
			shared.causes[index] = want
		}
		excessive := &attemptReadRetryJoinedError{causes: make([]error, 64)}
		for index := range excessive.causes {
			excessive.causes[index] = shared
		}
		for _, cause := range []error{
			nilRead, nilWrapper, matcher, claimed, deep, wide, excessive,
			&os.PathError{Op: "read", Path: "synthetic-checkpoint", Err: want},
			&os.LinkError{Op: "rename", Old: "synthetic-original", New: "synthetic-checkpoint", Err: want},
			&TrailFatalError{Err: want},
			&net.DNSError{Err: "synthetic name absent", Name: "rpc.example", IsNotFound: true, UnwrapErr: want},
			&attemptReadRetryJoinedError{causes: []error{want, nil}},
			errors.Join(want, errors.New("synthetic canonical mismatch")),
			errors.Join(context.Canceled, context.DeadlineExceeded),
		} {
			if classifyAttemptReadSafely(t, "owner cause", func() bool { return onlyAttemptContextError(cause, want) }) {
				t.Fatalf("incomplete or hard original cause %T became pure owner cancellation", cause)
			}
		}
		if matcher.matches != 0 || matcher.unwraps != 0 {
			t.Fatal("owner cancellation invoked a foreign matcher or its unwrap")
		}
		if claimed.matches != 0 || claimed.unwraps != 0 {
			t.Fatal("owner cancellation invoked a foreign Is matcher or its unwrap")
		}
	}
}

// The real trail caller must retain the first/later signed assignment and its
// durable prefix when a read returns a hard original cause. Returning a valid
// binding on a second call makes a pre-fix accidental retry finish finitely.
func TestAttemptBindingReadRetryRunTrailStopsAtOriginalHardCause(t *testing.T) {
	for _, failedBindingCall := range []int{1, 2} {
		for _, test := range []struct {
			name   string
			cancel bool
			cause  func() error
		}{
			{name: "file timeout", cause: func() error {
				return &os.PathError{Op: "read", Path: "synthetic-checkpoint", Err: context.DeadlineExceeded}
			}},
			{name: "lifecycle timeout", cause: func() error { return &TrailFatalError{Err: context.DeadlineExceeded} }},
			{name: "name absent with timeout", cause: func() error {
				return &net.DNSError{Err: "synthetic name absent", Name: "rpc.example", IsNotFound: true, UnwrapErr: context.DeadlineExceeded}
			}},
			{name: "foreign leaf matcher", cause: func() error { return &attemptReadRetryLeafMatcherError{} }},
			{name: "foreign wrapped matcher", cause: func() error { return &attemptReadRetryMatcherError{cause: context.DeadlineExceeded} }},
			{name: "cyclic timeout", cause: func() error { return &attemptReadRetryCycleError{escape: context.DeadlineExceeded} }},
			{name: "canceled file read", cancel: true, cause: func() error {
				return &os.PathError{Op: "read", Path: "synthetic-checkpoint", Err: context.Canceled}
			}},
			{name: "canceled lifecycle", cancel: true, cause: func() error { return &TrailFatalError{Err: context.Canceled} }},
			{name: "canceled name absent", cancel: true, cause: func() error {
				return &net.DNSError{Err: "synthetic name absent", Name: "rpc.example", IsNotFound: true, UnwrapErr: context.Canceled}
			}},
			{name: "canceled cycle", cancel: true, cause: func() error { return &attemptReadRetryCycleError{escape: context.Canceled} }},
		} {
			stateDir := t.TempDir()
			server, key, clientId := newMockVerifyServer(t, 12)
			engine, stats, _ := newTestEngine(t, server, key, clientId, 4, nil)
			transportCalls := 0
			engine.transport = attemptSettlementTestTransport(func(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
				transportCalls++
				return server.PostVerify(ctx, hop, body)
			})
			generation := uint64(1)
			ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			t.Cleanup(cancel)
			original := &attemptBindingReadError{cause: test.cause()}
			resolve := engine.resolve
			bindingCalls, waits := 0, 0
			engine.resolve = func(readCtx context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
				if len(ids) == 0 {
					return resolve(readCtx, pinned, ids)
				}
				bindingCalls++
				if pinned == nil || *pinned != attemptLedgerTestBoundary() || len(ids) != 1 {
					t.Fatalf("%s changed original assignment pin or coverage", test.name)
				}
				if bindingCalls == failedBindingCall {
					if test.cancel {
						cancel()
					}
					return AttemptBoundary{}, nil, original
				}
				return resolve(readCtx, pinned, ids)
			}
			engine.bindingRetryWait = func(context.Context, time.Duration) error { waits++; return nil }
			proof, err := engine.RunTrail(ctx)
			cancel()
			fatal, ok := err.(*TrailFatalError)
			if proof != nil || !ok || stats.activeAttemptCount != 0 || ledger.LastSequence() != uint64(failedBindingCall-1) || bindingCalls != failedBindingCall || waits != 0 || transportCalls != failedBindingCall {
				t.Fatalf("%s assignment %d lost hard refusal: fatal=%t proof=%t owners=%d sequence=%d reads=%d waits=%d requests=%d", test.name, failedBindingCall, ok, proof != nil, stats.activeAttemptCount, ledger.LastSequence(), bindingCalls, waits, transportCalls)
			}
			retained := false
			for cause, visits := fatal.Err, 0; cause != nil && visits < 4; cause, visits = errors.Unwrap(cause), visits+1 {
				if cause == original {
					retained = true
					break
				}
			}
			if !retained {
				t.Fatalf("%s replaced the original binding read cause", test.name)
			}
			server.mu.Lock()
			trails, extends := len(server.trails), server.extendCount
			server.mu.Unlock()
			if trails != 1 || extends != failedBindingCall-1 {
				t.Fatalf("%s replayed signed protocol work: trails=%d extends=%d", test.name, trails, extends)
			}
		}
	}
}

// Standard native errno and concrete provider status still recover through the
// actual retained assignment, with one read retry and no signed request replay.
func TestAttemptBindingReadRetryRunTrailRecoversConcreteTransport(t *testing.T) {
	for _, failure := range []error{
		&url.Error{Op: "Post", URL: "https://rpc.example", Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
		gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable},
		&net.DNSError{Err: "synthetic lookup timeout", Name: "rpc.example", IsTimeout: true},
	} {
		stateDir := t.TempDir()
		server, key, clientId := newMockVerifyServer(t, 12)
		engine, stats, _ := newTestEngine(t, server, key, clientId, 4, nil)
		transportCalls := 0
		engine.transport = attemptSettlementTestTransport(func(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
			transportCalls++
			return server.PostVerify(ctx, hop, body)
		})
		generation := uint64(1)
		ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
		resolve := engine.resolve
		bindingCalls, waits := 0, 0
		var retainedHop connect.Id
		engine.resolve = func(ctx context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			if len(ids) == 0 {
				return resolve(ctx, pinned, ids)
			}
			bindingCalls++
			if pinned == nil || *pinned != attemptLedgerTestBoundary() || len(ids) != 1 {
				t.Fatal("transport recovery changed the original assignment pin or coverage")
			}
			if bindingCalls == 2 {
				retainedHop = ids[0]
				return AttemptBoundary{}, nil, &attemptBindingReadError{cause: failure}
			}
			if bindingCalls == 3 && ids[0] != retainedHop {
				t.Fatal("transport recovery selected a new assigned hop")
			}
			return resolve(ctx, pinned, ids)
		}
		engine.bindingRetryWait = func(ctx context.Context, delay time.Duration) error {
			waits++
			if delay != 2*time.Second || ledger.LastSequence() != 1 || transportCalls != 2 || stats.activeAttemptCount != 1 || ctx.Err() != nil {
				t.Fatal("transport recovery did not retain its original assignment and prefix")
			}
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		t.Cleanup(cancel)
		proof, err := engine.RunTrail(ctx)
		cancel()
		if err != nil || proof == nil || waits != 1 || bindingCalls != 4 || transportCalls != 4 || stats.activeAttemptCount != 0 {
			t.Fatalf("concrete transport %T failed retained recovery: error=%v waits=%d reads=%d requests=%d owners=%d", failure, err, waits, bindingCalls, transportCalls, stats.activeAttemptCount)
		}
		records, err := ledger.RecordsAfter(0)
		if err != nil || len(records) != 4 {
			t.Fatalf("concrete transport %T lost its durable prefix: records=%d error=%v", failure, len(records), err)
		}
		for index, assignments := range []int{1, 2, 3, 3} {
			if len(records[index].Assignments) != assignments || records[index].Boundary != attemptLedgerTestBoundary() {
				t.Fatalf("concrete transport %T changed durable record %d", failure, index)
			}
		}
	}
}
