// A release decision observes error edges once. Later marker, retry and
// cancellation checks share frozen children while retaining original identity.
package validator

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"syscall"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The identity is diagnostic evidence only; classifiers enter value directly.
type releaseObservedOriginal struct {
	original error
	value    error
	hard     bool
}

// Rendering traverses the frozen value rather than a mutable original wrapper.
func (self *releaseObservedOriginal) Error() string { return self.value.Error() }

// Preserve exact source identity without dispatching a foreign matching method.
func (self *releaseObservedOriginal) Is(target error) bool {
	return releaseErrorIdentity(self.original, target)
}

// External cause inspection follows the same frozen value as the decision.
func (self *releaseObservedOriginal) Unwrap() error { return self.value }

// A transparent identity envelope does not spend another classification edge.
func releaseObservedValue(err error) error {
	if observed, ok := err.(*releaseObservedOriginal); ok && observed != nil {
		return observed.value
	}
	return err
}

// These wrappers retain only the children observed during admission.
type releaseObservedCause struct{ cause error }
type releaseObservedCauses struct{ causes []error }

// Original diagnostic methods cannot re-observe a changed edge while logging.
func (self *releaseObservedCause) Error() string {
	return fmt.Sprintf("release observed cause: %v", self.cause)
}
func (self *releaseObservedCauses) Error() string {
	return fmt.Sprintf("release observed causes: %v", self.causes)
}

// Each returned edge is immutable for the enclosing release decision.
func (self *releaseObservedCause) Unwrap() error    { return self.cause }
func (self *releaseObservedCauses) Unwrap() []error { return self.causes }

// A refused capture is terminal. No later classifier may reopen its original.
type releaseObservedRefusal struct{ original error }

// Avoid recursively formatting an incomplete or cyclic original.
func (*releaseObservedRefusal) Error() string {
	return "release original error graph is incomplete or refused"
}

// Keep the refused original identifiable without a live classification edge.
func (self *releaseObservedRefusal) Is(target error) bool {
	return releaseErrorIdentity(self.original, target)
}

// A concrete hard cause found anywhere in the capture dominates opaque markers.
type releaseObservedHard struct{ cause error }

// The retained cause is already frozen and remains available to the supervisor.
func (self *releaseObservedHard) Error() string {
	return fmt.Sprintf("release original hard cause: %v", self.cause)
}
func (self *releaseObservedHard) Unwrap() error { return self.cause }

// A native reader owns its private physical subtree. Cache that original
// complete verdict once; generic preparation cannot reinterpret its children.
type releaseObservedNativeRead struct {
	original  error
	retryable bool
}

func (*releaseObservedNativeRead) Error() string {
	return "release observed original native read result"
}

// Only external inspection follows the native original; classifiers stop here.
func (self *releaseObservedNativeRead) Unwrap() error { return self.original }

// A leaf network error's original timeout/temporary flags are observed once.
type releaseObservedNetworkRead struct {
	original  error
	retryable bool
}

func (*releaseObservedNetworkRead) Error() string      { return "release observed original network result" }
func (self *releaseObservedNetworkRead) Unwrap() error { return self.original }

// A single owner captures at most 512 occurrences, depth 32 and 128 joined
// children. Memoization preserves repeated exact marker identity; active nodes
// distinguish a cycle from an already completed shared original.
type releaseErrorObservation struct {
	remaining int
	failed    bool
	values    map[error]error
	active    map[error]bool
	// Only a production steering read sets this: its larger bound and the
	// deadline census taken during the same single capture.
	deadline *productionSteeringDeadlineCensus
}

// Once captured, a retained pending/scheduler result never opens its raw graph
// again when another poll, cancellation or worker supervisor observes it.
func observeReleaseError(err error) error {
	if err == nil {
		return nil
	}
	if observed, ok := err.(*releaseObservedOriginal); ok && observed != nil {
		return err
	}
	owner := &releaseErrorObservation{remaining: 512, values: make(map[error]error), active: make(map[error]bool)}
	value := owner.capture(err, 0)
	if owner.failed {
		return &releaseObservedOriginal{original: err, value: &releaseObservedHard{cause: value}, hard: true}
	}
	return value
}

// A production steering read joins one failure per object read, so a slow
// history replay can exceed the ordinary capture bound although every failure
// in it was a deadline. This captures the original once, under a larger
// bound. Within the ordinary bound the result is exactly observeReleaseError's.
// Beyond it, a graph whose every leaf was a deadline or timeout, or a
// cancellation caused by the bounded operation's own expiry, becomes one
// frozen deadline cause; any other graph stays hard, as before.
func observeProductionSteeringRead(err error, expired bool) error {
	if err == nil {
		return nil
	}
	if observed, ok := err.(*releaseObservedOriginal); ok && observed != nil {
		return err
	}
	census := &productionSteeringDeadlineCensus{}
	owner := &releaseErrorObservation{remaining: productionSteeringDeadlineLeaves, values: make(map[error]error), active: make(map[error]bool), deadline: census}
	value := owner.capture(err, 0)
	if !census.exceeded {
		if owner.failed {
			return &releaseObservedOriginal{original: err, value: &releaseObservedHard{cause: value}, hard: true}
		}
		return value
	}
	if !owner.failed && census.collapses(expired) {
		return &releaseObservedOriginal{original: err, value: &productionSteeringReadDeadline{leaves: census.deadlines + census.cancellations, sample: census.sample}}
	}
	return &releaseObservedOriginal{original: err, value: &releaseObservedHard{cause: value}, hard: true}
}

// The ordinary capture's own depth bound, or the production read's larger one.
func (self *releaseErrorObservation) maximumDepth() int {
	if self.deadline != nil {
		return productionSteeringDeadlineDepth
	}
	return 32
}

// Records a leaf or marker the deadline census cannot treat as a deadline.
func (self *releaseErrorObservation) notDeadline() {
	if self.deadline != nil {
		self.deadline.other = true
	}
}

// Missing optional children are valid only for trusted concrete owner markers.
func (self *releaseErrorObservation) optional(err error, depth int) error {
	if err == nil {
		return nil
	}
	return self.capture(err, depth)
}

// An incomplete edge cannot turn into a successful empty original later.
func (self *releaseErrorObservation) refuse(err error) error {
	self.failed = true
	return &releaseObservedRefusal{original: err}
}

// Copy only known wrapper fields. Arbitrary error values are never reflected
// into replacement types, and foreign Is/As methods are never invoked.
func (self *releaseErrorObservation) capture(err error, depth int) error {
	outerFailed := self.failed
	self.failed = false
	var created *releaseObservedOriginal
	defer func() {
		if created != nil {
			created.hard = self.failed
		}
		self.failed = outerFailed || self.failed
	}()
	if err == nil || depth > self.maximumDepth() || self.remaining <= 0 {
		return self.refuse(err)
	}
	self.remaining--
	if census := self.deadline; census != nil {
		census.nodes++
		if depth > 32 || census.nodes > 512 {
			census.exceeded = true
		}
	}
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if value.IsNil() {
			return self.refuse(err)
		}
	}
	if observed, ok := err.(*releaseObservedOriginal); ok {
		self.failed = observed.hard
		if self.deadline != nil {
			self.deadline.addFrozen(observed.value)
		}
		return err
	}
	comparable := value.Comparable()
	if comparable {
		if self.active[err] {
			return self.refuse(err)
		}
		if previous, found := self.values[err]; found {
			if observed, ok := previous.(*releaseObservedOriginal); ok {
				self.failed = observed.hard
			}
			// Repeated sentinel leaves are each one more failed read.
			if census := self.deadline; census != nil {
				switch err {
				case context.DeadlineExceeded:
					census.deadlines++
				case context.Canceled:
					census.cancellations++
				}
			}
			return previous
		}
		self.active[err] = true
		defer delete(self.active, err)
	}
	observed := &releaseObservedOriginal{original: err}
	created = observed
	if comparable {
		self.values[err] = observed
	}
	if crv4.IsSubstrateReadTransportCause(err) {
		retryable := crv4.RetryableSubstrateReadTransportError(err)
		observed.value = &releaseObservedNativeRead{original: err, retryable: retryable}
		self.failed = self.failed || !retryable
		self.notDeadline()
		return observed
	}
	switch cause := err.(type) {
	case *os.PathError, *os.LinkError, *TrailFatalError, *releaseObservedHard, *releaseObservedRefusal:
		self.failed = true
		self.notDeadline()
		observed.value = err
	case *net.DNSError:
		self.notDeadline()
		copy := *cause
		copy.UnwrapErr = self.optional(cause.UnwrapErr, depth+1)
		self.failed = self.failed || copy.IsNotFound
		observed.value = &copy
	case *net.OpError:
		copy := *cause
		copy.Err = self.capture(cause.Err, depth+1)
		observed.value = &copy
	case *url.Error:
		if self.deadline != nil {
			self.deadline.noteURL(cause)
		}
		copy := *cause
		copy.Err = self.capture(cause.Err, depth+1)
		observed.value = &copy
	case *artifactUnavailable:
		copy := *cause
		copy.cause = self.capture(cause.cause, depth+1)
		observed.value = &copy
	case *attemptStreamHttpReadError:
		copy := *cause
		copy.cause = self.capture(cause.cause, depth+1)
		observed.value = &copy
	case *chainRpcMissingResponseError:
		copy := *cause
		copy.cause = self.capture(cause.cause, depth+1)
		observed.value = &copy
	case *attemptStreamHTTPIncompleteError:
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *attemptReplicaPublicationError:
		self.notDeadline()
		observed.value = &attemptReplicaPublicationError{causes: self.children(cause.causes, depth+1)}
	case *productionSteeringReadWait:
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *productionPendingReconciliation:
		self.notDeadline()
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *provisionalNativeWeightRejection:
		self.notDeadline()
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *provisionalNativeReadInterruption:
		self.notDeadline()
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *attemptReplayReadInterruption:
		observed.value = &attemptReplayReadInterruption{cause: self.capture(cause.cause, depth+1)}
	case *provisionalClosedNativeInput:
		self.notDeadline()
		copy := *cause
		observed.value = &copy
	case *productionSteeringReadDeadline:
		if self.deadline != nil {
			self.deadline.deadlines += cause.leaves
		}
		copy := *cause
		observed.value = &copy
	case syscall.Errno:
		self.notDeadline()
		observed.value = err
	case interface{ Is(error) bool }, interface{ As(any) bool }:
		observed.value = self.refuse(err)
	default:
		switch {
		case err == context.Canceled, err == context.DeadlineExceeded, err == net.ErrClosed:
			if census := self.deadline; census != nil {
				switch err {
				case context.DeadlineExceeded:
					census.deadlines++
				case context.Canceled:
					census.cancellations++
				default:
					census.other = true
				}
			}
			observed.value = err
		default:
			if joined, ok := err.(interface{ Unwrap() []error }); ok {
				observed.value = &releaseObservedCauses{causes: self.children(joined.Unwrap(), depth+1)}
			} else if wrapped, ok := err.(interface{ Unwrap() error }); ok {
				observed.value = &releaseObservedCause{cause: self.capture(wrapped.Unwrap(), depth+1)}
			} else if network, ok := err.(net.Error); ok {
				// Each method is still called at most once, in the same order.
				timeout := network.Timeout()
				observed.value = &releaseObservedNetworkRead{original: err, retryable: timeout || network.Temporary()}
				if census := self.deadline; census != nil {
					if timeout {
						census.deadlines++
					} else {
						census.other = true
					}
				}
			} else {
				self.notDeadline()
				observed.value = err
			}
		}
	}
	return observed
}

// Copy the vector before traversing it so a later join observation cannot
// replace an original branch or discard an original nil child.
func (self *releaseErrorObservation) children(causes []error, depth int) []error {
	if census := self.deadline; census != nil && len(causes) > 512-census.nodes {
		census.exceeded = true
	}
	if len(causes) == 0 || len(causes) > 128 || len(causes) > self.remaining {
		return []error{self.refuse(nil)}
	}
	children := append([]error(nil), causes...)
	for index, cause := range children {
		children[index] = self.capture(cause, depth)
	}
	return children
}
