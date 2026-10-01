package validator

import "os"

// Parallel operators can wait for different parts of the same preparation.
// Every cause must be an exact cut marker or typed read interruption. A marker
// cannot hide an independent integrity, storage, cancellation or fatal error.
// The transport result keeps pure cut waits out of pre-intent read authority.
func classifyReleasePreparationRetry(err error) (retryable, transport bool) {
	if err == nil {
		return false, false
	}
	switch err {
	case errAttemptCutPending, errAttemptCutSnapshotStale, errAttemptSettlementSnapshotStale:
		return true, false
	}
	switch err.(type) {
	case *TrailFatalError, *os.PathError:
		return false, false
	}
	if RetryableEvidenceTransportError(err) {
		return true, true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false, false
		}
		for _, cause := range causes {
			childRetryable, childTransport := classifyReleasePreparationRetry(cause)
			if !childRetryable {
				return false, false
			}
			transport = transport || childTransport
		}
		return true, transport
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return classifyReleasePreparationRetry(wrapped.Unwrap())
	}
	return false, false
}
