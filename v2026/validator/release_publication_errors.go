package validator

import "errors"

// Only the joined replica owner constructs this error, after canceling and
// joining its own siblings. Parent cancellation remains outside this boundary.
type attemptReplicaPublicationError struct{ causes []error }

func (e *attemptReplicaPublicationError) Error() string   { return errors.Join(e.causes...).Error() }
func (e *attemptReplicaPublicationError) Unwrap() []error { return e.causes }

// Every joined replica reader/writer retains its sibling-cancellation owner.
// Cancellation of the caller stays outside that scope and can never be retried.
func joinReplicaPublicationErrors(parentErr error, causes []error) error {
	for _, cause := range causes {
		if cause != nil {
			return errors.Join(&attemptReplicaPublicationError{causes: causes}, parentErr)
		}
	}
	return parentErr
}

// Preserve the reason a readback could not reach authenticated EOF. An early
// close with no interruption cause is still a permanent integrity failure.
type attemptStreamHTTPIncompleteError struct{ cause error }

func (e *attemptStreamHTTPIncompleteError) Error() string {
	return "attempt stream HTTP body closed before complete authenticated EOF"
}
func (e *attemptStreamHTTPIncompleteError) Unwrap() error { return e.cause }

// A real Http body supplies these causes; local decoder eof and diagnostic
// text never acquire transport provenance through the retry classifier.
type attemptStreamHttpReadError struct{ cause error }

// Keep the original read diagnostic while carrying its physical source.
func (self *attemptStreamHttpReadError) Error() string { return self.cause.Error() }

// The complete tree still rejects independent integrity and close failures.
func (self *attemptStreamHttpReadError) Unwrap() error { return self.cause }
