package validator

import "fmt"

// Only the pre-intent preparation scope may create this retry marker. The
// ordinary reader error tree remains intact; no malformed evidence is accepted.
type provisionalNativeReadInterruption struct {
	nativeEpoch uint64
	cause       error
}

// The diagnostic explicitly distinguishes retry from a native submission.
func (self *provisionalNativeReadInterruption) Error() string {
	return fmt.Sprintf("provisional native epoch %d preparation interrupted: %v; native_submission=false final_acceptance=false", self.nativeEpoch, self.cause)
}

// Preserve transport identity for retry and cancellation checks.
func (self *provisionalNativeReadInterruption) Unwrap() error { return self.cause }

// Typed transport provenance and every joined cause must permit a retry.
// Bare EOF, diagnostic text, malformed rows, close failures and cancellation do
// not qualify. The caller revokes permission before opening the native intent.
func classifyProvisionalNativeRead(enabled bool, nativeEpoch uint64, err error) error {
	if !enabled {
		return err
	}
	retryable, transport := classifyReleasePreparationRetry(err)
	if !retryable || !transport {
		return err
	}
	return &provisionalNativeReadInterruption{nativeEpoch: nativeEpoch, cause: err}
}
