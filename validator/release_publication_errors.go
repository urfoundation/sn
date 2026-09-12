package validator

import "errors"

// Only the joined replica owner constructs this error, after canceling and
// joining its own siblings. Parent cancellation remains outside this boundary.
type attemptReplicaPublicationError struct{ causes []error }

func (e *attemptReplicaPublicationError) Error() string   { return errors.Join(e.causes...).Error() }
func (e *attemptReplicaPublicationError) Unwrap() []error { return e.causes }

// Preserve the reason a readback could not reach authenticated EOF. An early
// close with no interruption cause is still a permanent integrity failure.
type attemptStreamHTTPIncompleteError struct{ cause error }

func (e *attemptStreamHTTPIncompleteError) Error() string {
	return "attempt stream HTTP body closed before complete authenticated EOF"
}
func (e *attemptStreamHTTPIncompleteError) Unwrap() error { return e.cause }
