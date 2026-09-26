// Interrupted compact replay retains no accepted projection. Its caller may
// retry the immutable inputs with fresh scratch while preserving native intent
// reconciliation, epoch continuity and every unrelated failure.
package validator

// Created only after the complete replay error tree passes transport checks.
// This marks read provenance; it cannot authorize a native write or epoch gap.
type attemptReplayReadInterruption struct{ cause error }

// Preserve the original diagnostic and all structured transport causes.
func (self *attemptReplayReadInterruption) Error() string { return self.cause.Error() }

// Keep cancellation and integrity checks visible to every enclosing owner.
func (self *attemptReplayReadInterruption) Unwrap() error { return self.cause }

// Only the failed read projection owns this marker. A later joined close,
// custody, integrity or cancellation failure is rechecked by the retry loop.
func classifyAttemptReplayRead(err error) error {
	if !RetryableEvidenceTransportError(err) {
		return err
	}
	return &attemptReplayReadInterruption{cause: err}
}
