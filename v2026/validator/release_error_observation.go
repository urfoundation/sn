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
	if err == nil || depth > 32 || self.remaining <= 0 {
		return self.refuse(err)
	}
	self.remaining--
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if value.IsNil() {
			return self.refuse(err)
		}
	}
	if observed, ok := err.(*releaseObservedOriginal); ok {
		self.failed = observed.hard
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
		return observed
	}
	switch cause := err.(type) {
	case *os.PathError, *os.LinkError, *TrailFatalError, *releaseObservedHard, *releaseObservedRefusal:
		self.failed = true
		observed.value = err
	case *net.DNSError:
		copy := *cause
		copy.UnwrapErr = self.optional(cause.UnwrapErr, depth+1)
		self.failed = self.failed || copy.IsNotFound
		observed.value = &copy
	case *net.OpError:
		copy := *cause
		copy.Err = self.capture(cause.Err, depth+1)
		observed.value = &copy
	case *url.Error:
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
		observed.value = &attemptReplicaPublicationError{causes: self.children(cause.causes, depth+1)}
	case *productionSteeringReadWait:
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *productionPendingReconciliation:
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *provisionalNativeWeightRejection:
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *provisionalNativeReadInterruption:
		copy := *cause
		copy.cause = self.optional(cause.cause, depth+1)
		observed.value = &copy
	case *attemptReplayReadInterruption:
		observed.value = &attemptReplayReadInterruption{cause: self.capture(cause.cause, depth+1)}
	case *provisionalClosedNativeInput:
		copy := *cause
		observed.value = &copy
	case syscall.Errno:
		observed.value = err
	case interface{ Is(error) bool }, interface{ As(any) bool }:
		observed.value = self.refuse(err)
	default:
		switch {
		case err == context.Canceled, err == context.DeadlineExceeded, err == net.ErrClosed:
			observed.value = err
		default:
			if joined, ok := err.(interface{ Unwrap() []error }); ok {
				observed.value = &releaseObservedCauses{causes: self.children(joined.Unwrap(), depth+1)}
			} else if wrapped, ok := err.(interface{ Unwrap() error }); ok {
				observed.value = &releaseObservedCause{cause: self.capture(wrapped.Unwrap(), depth+1)}
			} else if network, ok := err.(net.Error); ok {
				observed.value = &releaseObservedNetworkRead{original: err, retryable: network.Timeout() || network.Temporary()}
			} else {
				observed.value = err
			}
		}
	}
	return observed
}

// Copy the vector before traversing it so a later join observation cannot
// replace an original branch or discard an original nil child.
func (self *releaseErrorObservation) children(causes []error, depth int) []error {
	if len(causes) == 0 || len(causes) > 128 || len(causes) > self.remaining {
		return []error{self.refuse(nil)}
	}
	children := append([]error(nil), causes...)
	for index, cause := range children {
		children[index] = self.capture(cause, depth)
	}
	return children
}
