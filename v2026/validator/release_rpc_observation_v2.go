//go:build linux || darwin

// Rpc failures and completed semantic mismatches remain separate. A failed
// read has no observation whose signer, epoch, or identity can be judged.
package validator

// Returns the original read failure without attaching a mismatch verdict.
// Only a completed read may produce the supplied semantic error.
func releaseRpcObservationError(readErr error, matches bool, mismatchErr error) error {
	if readErr != nil {
		return readErr
	}
	if !matches {
		return mismatchErr
	}
	return nil
}
