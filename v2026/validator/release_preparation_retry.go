// Preparation and transport share one traversal of original error edges.
package validator

// Parallel operators can wait for different parts of the same preparation.
// Exact cut leaves grant no transport authority; every other cause retains its
// reader's complete verdict and one shared finite traversal allowance.
func classifyReleasePreparationRetry(err error) (retryable, transport bool) {
	remaining := 512
	return classifyReleaseRetryBounded(err, false, false, false, true, 0, &remaining)
}
