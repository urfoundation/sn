//go:build linux || darwin

// Capture and continuation planning judge identities only after a completed
// read. Failed reads retain their original retry and cancellation ownership.
package main

// Incomplete observations cannot establish a mismatch. A joined read failure
// keeps every original cause, including any independently reported integrity
// failure; it never gains or loses retry authority here.
func captureRpcObservationError(readErr error, matches bool, mismatchErr error) error {
	if readErr != nil {
		return readErr
	}
	if !matches {
		return mismatchErr
	}
	return nil
}
